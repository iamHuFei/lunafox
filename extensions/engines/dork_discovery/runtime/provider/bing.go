package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// bingBlockedMarkers appear on Bing challenge, consent-wall, and
// rate-limitation pages. Any hit stops pagination for the current dork
// instead of hammering the endpoint.
var bingBlockedMarkers = []string{
	"b_captcha",
	"challenge",
	"verify you are human",
	"请输入验证码",
}

// BingProvider searches cn.bing.com in international mode. ensearch=1 keeps
// Google-style operators (site:, inurl:, ...) effective instead of the
// CN-market query rewriting that ignores them.
//
// Bing degrades cookie-less automated queries to a generalized
// recommendation page that looks like results but contains none for the
// dorked host, so the client carries a cookie jar warmed up once against the
// Bing homepage before the first search.
type BingProvider struct {
	Client  *http.Client
	BaseURL string
	HomeURL string
	warmed  bool
	// blockedOnce trips the circuit breaker: once Bing answers with a
	// challenge, every further query in this run returns Blocked without
	// hitting the endpoint, because hammering through a challenge deepens
	// the throttle instead of clearing it.
	blockedOnce bool
	// Pagination state: the last query and the pager URL Bing rendered for
	// its next page (FPIG-fingerprinted). Sequential per-dork paging only.
	lastQuery   string
	lastNextURL string
}

// NewBingProvider builds the cookie-carrying client. A non-empty proxyURL
// (http:// or socks5://) routes both the warmup and the searches through an
// egress proxy, which rotates the IP Bing sees when the direct address gets
// throttled.
func NewBingProvider(timeout time.Duration, proxyURL string) *BingProvider {
	jar, err := cookiejar.New(nil)
	if err != nil {
		jar = nil
	}
	transport := &http.Transport{}
	if parsed, err := url.Parse(proxyURL); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "socks5") {
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &BingProvider{
		Client: &http.Client{
			Timeout:   timeout,
			Jar:       jar,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		BaseURL: "https://cn.bing.com/search",
		HomeURL: "https://cn.bing.com/",
	}
}

func (provider *BingProvider) Name() string { return "bing" }

// warmup establishes the Bing session cookies (MUID & friends) that keep
// search results exact instead of generalized. A failed warmup is not fatal:
// the first search proceeds cookie-less.
func (provider *BingProvider) warmup(ctx context.Context) {
	if provider.warmed || provider.Client == nil || provider.HomeURL == "" {
		return
	}
	provider.warmed = true
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.HomeURL, nil)
	if err != nil {
		return
	}
	provider.decorateHeaders(request)
	response, err := provider.Client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
}

// decorateHeaders mirrors a real browser top-level navigation. Bing's bot
// defense degrades plain HTTP-client requests (even cookie-carrying ones) to
// a generalized recommendation page; the Sec-Fetch navigation metadata and a
// zh-first Accept-Language are what restore exact site: results.
func (provider *BingProvider) decorateHeaders(request *http.Request) {
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	request.Header.Set("Sec-Fetch-Dest", "document")
	request.Header.Set("Sec-Fetch-Mode", "navigate")
	request.Header.Set("Sec-Fetch-Site", "none")
	request.Header.Set("Sec-Fetch-User", "?1")
	request.Header.Set("Upgrade-Insecure-Requests", "1")
}

func (provider *BingProvider) Search(ctx context.Context, query string, first, count int) (Page, error) {
	if provider == nil || provider.Client == nil {
		return Page{}, fmt.Errorf("bing provider client is required")
	}
	provider.warmup(ctx)
	if provider.blockedOnce {
		return Page{Blocked: true}, nil
	}
	endpoint := provider.BaseURL + "?" + url.Values{
		"q":        {query},
		"ensearch": {"1"},
		"count":    {fmt.Sprintf("%d", count)},
		"first":    {fmt.Sprintf("%d", first)},
	}.Encode()
	// Pagination follows Bing's own pager link (with the FPIG fingerprint):
	// a rebuilt first= offset silently repeats page one. The pager href is
	// relative (/search?...), so it resolves against the Bing host.
	if first > 1 && provider.lastQuery == query && provider.lastNextURL != "" {
		if base, baseErr := url.Parse("https://www.bing.com"); baseErr == nil {
			if absolute, absErr := base.Parse(provider.lastNextURL); absErr == nil && absolute.IsAbs() {
				endpoint = absolute.String()
			}
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Page{}, fmt.Errorf("build bing request: %w", err)
	}
	provider.decorateHeaders(request)

	response, err := provider.Client.Do(request)
	if err != nil {
		return Page{}, fmt.Errorf("bing request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return Page{}, fmt.Errorf("read bing response: %w", err)
	}
	page := Page{StatusCode: response.StatusCode}
	bodyText := string(body)
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		page.Blocked = true
		provider.blockedOnce = true
		return page, nil
	}
	hits, anomalies := ParseBingResults(bodyText)
	// Challenge markers also appear as innocent substrings (script names,
	// consent plumbing) inside fully rendered result pages, so a marker only
	// counts as a block when the page carries no organic results at all.
	if len(hits) == 0 && !strings.Contains(bodyText, "b_algo") {
		for _, marker := range bingBlockedMarkers {
			if strings.Contains(bodyText, marker) {
				page.Blocked = true
				page.Anomalies = anomalies
				provider.blockedOnce = true
				return page, nil
			}
		}
	}
	page.Hits = hits
	page.Anomalies = anomalies
	if nextURL := ParseBingNextPageURL(bodyText); nextURL != "" {
		page.NextPageURL = nextURL
		provider.lastQuery = query
		provider.lastNextURL = nextURL
	} else {
		provider.lastQuery = ""
		provider.lastNextURL = ""
	}
	return page, nil
}
