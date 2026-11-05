package prober

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestProbeAllKeepsOnlyRespondingURLs(t *testing.T) {
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			body := []byte("<html><head><title>Admin Console</title></head><body>x</body></html>")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Server", "nginx")
			// An explicit matching Content-Length exercises the declared-header
			// priority in the outcome (Go refuses to serve mismatched lengths).
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/slow":
			time.Sleep(2 * time.Second)
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer alive.Close()
	deadURL := "http://127.0.0.1:1/connection-refused"

	engine := New(Options{
		Concurrency:     4,
		RequestTimeout:  1 * time.Second,
		BodyLimitBytes:  64 << 10,
		FollowRedirects: true,
	})
	live, unreachable := engine.ProbeAll(context.Background(), []string{
		alive.URL + "/ok",
		alive.URL + "/redirect",
		alive.URL + "/missing",
		deadURL,
		"not-a-url",
	})
	if len(live) != 3 {
		t.Fatalf("expected 3 responding outcomes, got %+v", live)
	}
	if unreachable != 2 {
		t.Fatalf("expected 2 unreachable, got %d", unreachable)
	}
	first := live[0]
	if first.Title != "Admin Console" || first.Webserver != "nginx" || first.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("metadata not captured: %+v", first)
	}
	if first.StatusCode == nil || *first.StatusCode != 200 {
		t.Fatalf("status missing: %+v", first)
	}
	if first.ContentLength == nil || *first.ContentLength != 68 {
		t.Fatalf("content length should honor the declared header: %+v", first)
	}
	for _, outcome := range live {
		if outcome.Host != "127.0.0.1" {
			t.Fatalf("unexpected host: %+v", outcome)
		}
	}
}

func TestProbeAllWithoutRedirectsRecordsLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	engine := New(Options{
		Concurrency:     2,
		RequestTimeout:  2 * time.Second,
		BodyLimitBytes:  4 << 10,
		FollowRedirects: false,
	})
	live, _ := engine.ProbeAll(context.Background(), []string{server.URL + "/hop"})
	if len(live) != 1 {
		t.Fatalf("redirect response is still a live outcome: %+v", live)
	}
	if live[0].Location != "/ok" {
		t.Fatalf("location not recorded: %+v", live[0])
	}
}

func TestProbeAllRespectsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(200)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	engine := New(Options{Concurrency: 2, RequestTimeout: 5 * time.Second, BodyLimitBytes: 1024, FollowRedirects: true})
	live, _ := engine.ProbeAll(ctx, []string{server.URL + "/a", server.URL + "/b"})
	if len(live) != 0 {
		t.Fatalf("cancelled probes must not report outcomes: %+v", live)
	}
}
