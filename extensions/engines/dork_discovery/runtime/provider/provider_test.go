package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(body)
}

func TestParseBingResultsExtractsWrappedResultsFromLivePage(t *testing.T) {
	hits, anomalies := ParseBingResults(loadFixture(t, "bing-site-results.html"))
	if len(hits) == 0 {
		t.Fatalf("live results page must yield hits, anomalies=%d", anomalies)
	}
	matched := false
	for _, hit := range hits {
		if strings.Contains(hit.URL, "52pojie.cn") {
			matched = true
		}
		if strings.Contains(hit.Title, "52pojie") && hit.URL == "" {
			t.Fatalf("hit with title but no url: %+v", hit)
		}
	}
	if !matched {
		t.Fatalf("expected at least one 52pojie.cn hit, got %+v", hits)
	}
}

func TestParseBingResultsHandlesGeneralizedPageWithoutFailure(t *testing.T) {
	// A dork whose site: has no indexed results silently degrades to a
	// recommendation page; the parser must still extract its (out-of-scope)
	// links so scope filtering upstream decides, not the parser.
	hits, anomalies := ParseBingResults(loadFixture(t, "bing-generalized.html"))
	if len(hits) == 0 {
		t.Fatalf("generalized page should still parse into hits, anomalies=%d", anomalies)
	}
	for _, hit := range hits {
		if strings.Contains(strings.ToLower(hit.URL), "bing.com") && !strings.Contains(hit.URL, "/ck/") {
			t.Fatalf("bing self link leaked: %s", hit.URL)
		}
	}
}

func TestParseBingResultsOnEmptyPageNeverYieldsTargetHits(t *testing.T) {
	// A b_no page (no indexed results for the dork) still carries Bing's
	// generalized recommendation blocks. The parser must surface them — the
	// pipeline's target-scope filter is the actual safety net — but none of
	// them may belong to the (nonexistent) dorked domain.
	hits, anomalies := ParseBingResults(loadFixture(t, "bing-empty.html"))
	if anomalies != 0 {
		t.Fatalf("well-formed page should parse without anomalies: %d", anomalies)
	}
	for _, hit := range hits {
		if strings.Contains(hit.URL, "nonexistent-domain-xyz123.invalid") {
			t.Fatalf("b_no page must not carry hits for the dorked domain: %+v", hit)
		}
	}
}

func TestParseBingResultsDecodesCKRedirectAndCiteFallback(t *testing.T) {
	hits, anomalies := ParseBingResults(loadFixture(t, "bing-ck-redirect.html"))
	// Block 1: ck/a wrapped docs.example.com URL decoded. Block 2: bing self
	// link dropped. Block 3: no anchor, cite host hint repaired with https.
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits (ck decoded + cite repaired), got %+v anomalies=%d", hits, anomalies)
	}
	if hits[0].URL != "https://docs.example.com/admin/login.html" || hits[0].Title != "Admin Login" {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}
	if hits[1].URL != "https://plain.example.com" {
		t.Fatalf("cite fallback must repair to https url: %+v", hits[1])
	}
}

func TestResolveBingURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a":           "https://example.com/a",
		"//example.com/b":                 "https://example.com/b",
		"https://www.bing.com/search?q=x": "",
		"javascript:alert(1)":             "",
		"":                                "",
		"ftp://example.com/x":             "",
	}
	for input, expected := range cases {
		if got := resolveBingURL(input); got != expected {
			t.Fatalf("resolveBingURL(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestBingProviderDetectsBlockAndServesHits(t *testing.T) {
	resultsBody := loadFixture(t, "bing-site-results.html")
	captchaBody := loadFixture(t, "bing-captcha.html")
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Query().Get("q") == "blocked query" {
			w.Write([]byte(captchaBody))
			return
		}
		if r.URL.Query().Get("q") != "" && r.URL.Query().Get("ensearch") != "1" {
			t.Errorf("international mode flag missing: %v", r.URL.RawQuery)
		}
		w.Write([]byte(resultsBody))
	}))
	defer server.Close()

	provider := NewBingProvider(5*time.Second, "")
	provider.BaseURL = server.URL
	// Keep the warmup inside the test server; it accounts for one request.
	provider.HomeURL = server.URL + "/"

	page, err := provider.Search(context.Background(), "site:52pojie.cn", 1, 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if page.Blocked {
		t.Fatalf("normal page flagged blocked")
	}
	if len(page.Hits) == 0 {
		t.Fatalf("hits missing on normal page")
	}

	page, err = provider.Search(context.Background(), "blocked query", 1, 20)
	if err != nil {
		t.Fatalf("blocked search: %v", err)
	}
	if !page.Blocked {
		t.Fatalf("captcha page must set Blocked")
	}
	if requestCount != 3 {
		t.Fatalf("unexpected request count %d (warmup + 2 searches)", requestCount)
	}
}

func TestBingProviderWarmsUpCookiesAndSendsThem(t *testing.T) {
	resultsBody := loadFixture(t, "bing-site-results.html")
	searchSawCookie := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// The homepage warmup hands out a session cookie.
			http.SetCookie(w, &http.Cookie{Name: "MUID", Value: "B84FD3A1E2C5"})
			w.WriteHeader(http.StatusOK)
			return
		}
		if cookie, err := r.Cookie("MUID"); err == nil {
			searchSawCookie = cookie.Value
		}
		w.Write([]byte(resultsBody))
	}))
	defer server.Close()

	provider := NewBingProvider(5*time.Second, "")
	provider.BaseURL = server.URL + "/search"
	provider.HomeURL = server.URL + "/"

	page, err := provider.Search(context.Background(), "site:example.com", 1, 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if searchSawCookie != "B84FD3A1E2C5" {
		t.Fatalf("search request must carry the warmup cookie, got %q", searchSawCookie)
	}
	if len(page.Hits) == 0 {
		t.Fatalf("hits missing")
	}
}

func TestParseBingNextPageURLFromLivePage(t *testing.T) {
	next := ParseBingNextPageURL(loadFixture(t, "bing-site-results.html"))
	if next == "" {
		t.Fatalf("live results page should expose a next-page link (FPIG fingerprint)")
	}
	if !strings.Contains(next, "first=") {
		t.Fatalf("next-page link must carry the first offset: %q", next)
	}
	if !strings.Contains(next, "FPIG=") {
		t.Fatalf("next-page link should carry the session fingerprint: %q", next)
	}
}

func TestParseBingNextPageURLEmptyOnNoPager(t *testing.T) {
	if next := ParseBingNextPageURL(loadFixture(t, "bing-captcha.html")); next != "" {
		t.Fatalf("captcha page must not expose pagination: %q", next)
	}
	if next := ParseBingNextPageURL(loadFixture(t, "bing-ck-redirect.html")); next != "" {
		t.Fatalf("synthetic page has no pager: %q", next)
	}
}
