package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func waybackTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != "sd.189.cn" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, body)
	}))
	return server
}

func TestFetchWaybackURLsGatesAndOrder(t *testing.T) {
	body := "" +
		"http://sd.189.cn:80/biz/index.jsp 20140501120000 200\n" +
		"http://sd.189.cn/static/app.css 20240501120000 200\n" +
		"http://sd.189.cn/gone.jsp 20240501120000 404\n" +
		"https://sd.189.cn/fresh/app 20240601120000 200\n" +
		"http://sd.189.cn:80/legacy/api 20120501120000 200\n" +
		"http://sd.189.cn:80/biz/index.jsp 20160501120000 200\n" + // duplicate after normalization, later ts wins ordering
		"not-a-url 20200101000000 200\n"
	server := waybackTestServer(t, body)
	defer server.Close()

	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	entries, err := FetchWaybackURLsVia(server.URL, "sd.189.cn", "", 0, 0, now)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 kept (static/404/bad dropped, dup collapsed), got %+v", entries)
	}
	if entries[0].URL != "https://sd.189.cn/fresh/app" {
		t.Fatalf("most recent first, got %+v", entries[0])
	}
	if entries[2].LastSeenHuman() != "2012-05-01" {
		t.Fatalf("human date wrong: %+v", entries[2])
	}

	aged, _ := FetchWaybackURLsVia(server.URL, "sd.189.cn", "", 0, 3, now)
	if len(aged) != 1 || aged[0].URL != "https://sd.189.cn/fresh/app" {
		t.Fatalf("max-age 3y keeps only fresh, got %+v", aged)
	}

	limited, _ := FetchWaybackURLsVia(server.URL, "sd.189.cn", "", 1, 0, now)
	if len(limited) != 1 {
		t.Fatalf("limit 1 keeps 1, got %+v", limited)
	}
}

func TestNormalizeWaybackURL(t *testing.T) {
	cases := map[string]string{
		"http://sd.189.cn:80/biz/":    "http://sd.189.cn/biz",
		"https://sd.189.cn:443/a?b=1": "https://sd.189.cn/a?b=1",
		"ftp://sd.189.cn/x":           "",
		"javascript:alert(1)":         "",
	}
	for raw, want := range cases {
		if got := normalizeWaybackURL(raw); got != want {
			t.Fatalf("normalize(%q)=%q want %q", raw, got, want)
		}
	}
}
