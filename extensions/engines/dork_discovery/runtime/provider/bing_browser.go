package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserBingProvider runs the queries inside a real headless Chromium.
//
// Bing binds pagination (and clean site: results) to a JavaScript session:
// the rdrig pagination fingerprint only materializes after JS runs, and
// HTTP-only clients get the same page for every first= offset no matter which
// cookies or FPIG links they replay. A headless browser session therefore is
// the only reliable way to page through results; the plain BingProvider stays
// available as a lightweight fallback.
type BrowserBingProvider struct {
	execPath  string
	proxyURL  string
	browser   *rod.Browser
	page      *rod.Page
	lastQuery string
	// lastNextSelector targets the pager link rendered on the last page.
	// Pagination clicks it like a user: the click drives Bing's own JS
	// pagination, which is what carries the live session fingerprint (rdrig)
	// that raw URL navigation lacks.
	lastNextSelector string
}

const defaultHeadlessShellPath = "/opt/lunafox-engine/bin/chrome-headless-shell"

// NewBrowserBingProvider prepares a lazy browser session; the browser starts
// on first use. proxyURL (http://) is forwarded to Chromium.
func NewBrowserBingProvider(execPath, proxyURL string) *BrowserBingProvider {
	if strings.TrimSpace(execPath) == "" {
		execPath = defaultHeadlessShellPath
	}
	return &BrowserBingProvider{execPath: execPath, proxyURL: proxyURL}
}

func (provider *BrowserBingProvider) Name() string { return "bing-browser" }

// Close tears the browser down; safe to call multiple times.
func (provider *BrowserBingProvider) Close() {
	if provider.browser != nil {
		_ = provider.browser.Close()
		provider.browser = nil
		provider.page = nil
	}
}

func (provider *BrowserBingProvider) ensure(ctx context.Context) error {
	if provider.page != nil {
		return nil
	}
	control := launcher.New().
		Bin(provider.execPath).
		Set("headless", "new").
		NoSandbox(true).
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("no-first-run")
	if parsed, err := url.Parse(provider.proxyURL); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		control = control.Proxy(provider.proxyURL)
	}
	browser := rod.New().ControlURL(control.MustLaunch())
	if err := withDeadline(ctx, func() error { return browser.Connect() }); err != nil {
		control.Kill()
		return fmt.Errorf("start headless browser: %w", err)
	}
	provider.browser = browser
	var page *rod.Page
	err2 := withDeadline(ctx, func() (err error) {
		page, err = provider.browser.Page(proto.TargetCreateTarget{})
		if err != nil {
			return err
		}
		// headless-shell identifies as HeadlessChrome in its UA; Bing gates
		// pagination on non-bot fingerprints, so present the real Chrome UA.
		return page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
			AcceptLanguage: "zh-CN,zh;q=0.9,en;q=0.8",
		})
	})
	if err2 != nil {
		provider.Close()
		return err2
	}
	// Warm the session cookies with one homepage visit.
	if err := provider.navigate(ctx, page, "https://cn.bing.com/", 1200*time.Millisecond); err != nil {
		provider.Close()
		return err
	}
	provider.page = page
	return nil
}

// Search loads the query page in the browser and returns its hits. Pagination
// navigates the pager href inside the same live session, where it carries the
// rdrig fingerprint that makes first= offsets advance.
func (provider *BrowserBingProvider) Search(ctx context.Context, query string, first, count int) (Page, error) {
	if err := provider.ensure(ctx); err != nil {
		return Page{}, err
	}
	endpoint := "https://cn.bing.com/search?" + url.Values{
		"q":        {query},
		"ensearch": {"1"},
		"count":    {fmt.Sprintf("%d", count)},
		"first":    {fmt.Sprintf("%d", first)},
	}.Encode()
	if first > 1 && provider.lastQuery == query && provider.lastNextSelector != "" {
		body, err := provider.clickNext(ctx, provider.lastNextSelector)
		if err != nil {
			return Page{}, err
		}
		return provider.parseBody(ctx, query, body)
	}
	body, err := provider.render(ctx, provider.page, endpoint)
	if err != nil {
		return Page{}, err
	}
	return provider.parseBody(ctx, query, body)
}

// parseBody converts a rendered results page into a provider Page and records
// the pager selector for the next click.
func (provider *BrowserBingProvider) parseBody(ctx context.Context, query, body string) (Page, error) {
	_ = ctx
	page := Page{StatusCode: 200}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "b_captcha") || strings.Contains(lower, "verify you are human") || strings.Contains(body, "请输入验证码") {
		page.Blocked = true
		provider.lastNextSelector = ""
		return page, nil
	}
	hits, anomalies := ParseBingResults(body)
	page.Hits = hits
	page.Anomalies = anomalies
	provider.lastQuery = query
	if next := ParseBingNextPageURL(body); next != "" {
		provider.lastNextSelector = `a[href*="first=` + pagerOffset(next) + `"]`
		page.NextPageURL = next
	} else {
		provider.lastNextSelector = ""
	}
	return page, nil
}

// pagerOffset extracts the first= value from a pager href.
func pagerOffset(href string) string {
	if index := strings.Index(href, "first="); index >= 0 {
		rest := href[index+len("first="):]
		if end := strings.IndexAny(rest, "&"); end > 0 {
			return rest[:end]
		}
		return rest
	}
	return ""
}

// clickNext drives Bing's own pagination link as a user click.
func (provider *BrowserBingProvider) clickNext(ctx context.Context, selector string) (string, error) {
	err := withDeadline(ctx, func() error {
		el, err := provider.page.Element(selector)
		if err != nil {
			return err
		}
		return el.Click(proto.InputMouseButtonLeft, 1)
	})
	if err != nil {
		return "", fmt.Errorf("browser click next: %w", err)
	}
	select {
	case <-time.After(1500 * time.Millisecond):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	var body string
	err = withDeadline(ctx, func() (err error) {
		body, err = provider.page.HTML()
		return err
	})
	if err != nil {
		return "", fmt.Errorf("browser html: %w", err)
	}
	return body, nil
}

// render navigates and returns the settled page HTML.
func (provider *BrowserBingProvider) render(ctx context.Context, page *rod.Page, endpoint string) (string, error) {
	if err := provider.navigate(ctx, page, endpoint, 900*time.Millisecond); err != nil {
		return "", err
	}
	var body string
	err := withDeadline(ctx, func() (err error) {
		body, err = page.HTML()
		return err
	})
	if err != nil {
		return "", fmt.Errorf("browser html: %w", err)
	}
	return body, nil
}

func (provider *BrowserBingProvider) navigate(ctx context.Context, page *rod.Page, endpoint string, settle time.Duration) error {
	err := withDeadline(ctx, func() (err error) {
		err = page.Navigate(endpoint)
		if err != nil {
			return err
		}
		return page.WaitLoad()
	})
	if err != nil {
		return fmt.Errorf("browser navigate: %w", err)
	}
	select {
	case <-time.After(settle):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// withDeadline bounds a rod call with the engine's context deadline.
func withDeadline(ctx context.Context, call func() error) error {
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
