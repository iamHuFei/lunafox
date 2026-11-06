// Package prober performs bounded HTTP liveness probes over harvested URLs
// so only assets that actually answer become EndpointV2 results ("存活资产").
package prober

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Options bounds one probe batch.
type Options struct {
	Concurrency     int
	RequestTimeout  time.Duration
	BodyLimitBytes  int64
	FollowRedirects bool
}

// Outcome is the probe result for one URL. It exists only for URLs that
// received an HTTP response; unreachable candidates are counted upstream.
type Outcome struct {
	URL           string
	Host          string
	Title         string
	StatusCode    *int64
	ContentLength *int64
	ContentType   string
	Webserver     string
	Location      string
}

type Prober struct {
	clientNoRedirect *http.Client
	clientFollow     *http.Client
	options          Options
}

func New(options Options) *Prober {
	if options.Concurrency < 1 {
		options.Concurrency = 1
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 8 * time.Second
	}
	if options.BodyLimitBytes <= 0 {
		options.BodyLimitBytes = 64 << 10
	}
	newClient := func(checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
		return &http.Client{Timeout: options.RequestTimeout, CheckRedirect: checkRedirect}
	}
	stopImmediately := func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	followUpToTen := func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &Prober{
		clientNoRedirect: newClient(stopImmediately),
		clientFollow:     newClient(followUpToTen),
		options:          options,
	}
}

// ProbeAll probes URLs with a bounded worker pool and returns the outcomes
// that received an HTTP response in stable input order, plus the URLs that
// did not answer (no HTTP response at all).
func (prober *Prober) ProbeAll(ctx context.Context, urls []string) ([]Outcome, []string) {
	if prober == nil || len(urls) == 0 {
		return nil, nil
	}
	results := make([]Outcome, len(urls))
	responded := make([]bool, len(urls))
	semaphore := make(chan struct{}, prober.options.Concurrency)
	var wg sync.WaitGroup
	for index, candidate := range urls {
		wg.Add(1)
		go func(index int, candidate string) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			outcome, ok := prober.probeOne(ctx, candidate)
			if ok {
				results[index] = outcome
				responded[index] = true
			}
		}(index, candidate)
	}
	wg.Wait()
	live := make([]Outcome, 0, len(urls))
	unreachable := make([]string, 0)
	for index := range urls {
		if responded[index] {
			live = append(live, results[index])
		} else {
			unreachable = append(unreachable, urls[index])
		}
	}
	return live, unreachable
}

func (prober *Prober) probeOne(ctx context.Context, rawURL string) (Outcome, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Outcome{}, false
	}
	client := prober.clientFollow
	if !prober.options.FollowRedirects {
		client = prober.clientNoRedirect
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Outcome{}, false
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	response, err := client.Do(request)
	if err != nil {
		return Outcome{}, false
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, prober.options.BodyLimitBytes))

	outcome := Outcome{
		URL:         rawURL,
		Host:        parsed.Hostname(),
		Title:       extractTitle(body),
		ContentType: response.Header.Get("Content-Type"),
		Webserver:   response.Header.Get("Server"),
	}
	status := int64(response.StatusCode)
	outcome.StatusCode = &status
	length := int64(len(body))
	if declared := response.Header.Get("Content-Length"); declared != "" {
		if parsedLength, err := strconv.ParseInt(strings.TrimSpace(declared), 10, 64); err == nil {
			length = parsedLength
		}
	}
	outcome.ContentLength = &length
	if !prober.options.FollowRedirects && response.StatusCode >= 300 && response.StatusCode < 400 {
		if location := response.Header.Get("Location"); location != "" {
			outcome.Location = location
		}
	}
	return outcome, true
}

func extractTitle(body []byte) string {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if title != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "title" {
			if text := strings.TrimSpace(nodeText(node)); text != "" {
				title = text
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if title != "" {
				return
			}
			walk(child)
		}
	}
	walk(root)
	// Titles are display strings; a hard byte cap keeps the encoded item
	// inside the platform evidence budget without UTF-8 splitting.
	if len(title) > 512 {
		cut := []byte(title)[:512]
		for len(cut) > 0 && !utf8.Valid(cut) {
			cut = cut[:len(cut)-1]
		}
		title = string(cut)
	}
	return title
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}
