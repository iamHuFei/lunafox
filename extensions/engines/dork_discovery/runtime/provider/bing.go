package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
type BingProvider struct {
	Client  *http.Client
	BaseURL string
}

func NewBingProvider(timeout time.Duration) *BingProvider {
	return &BingProvider{
		Client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		BaseURL: "https://cn.bing.com/search",
	}
}

func (provider *BingProvider) Name() string { return "bing" }

func (provider *BingProvider) Search(ctx context.Context, query string, first, count int) (Page, error) {
	if provider == nil || provider.Client == nil {
		return Page{}, fmt.Errorf("bing provider client is required")
	}
	endpoint := provider.BaseURL + "?" + url.Values{
		"q":        {query},
		"ensearch": {"1"},
		"count":    {fmt.Sprintf("%d", count)},
		"first":    {fmt.Sprintf("%d", first)},
	}.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Page{}, fmt.Errorf("build bing request: %w", err)
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8")

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
				return page, nil
			}
		}
	}
	page.Hits = hits
	page.Anomalies = anomalies
	return page, nil
}
