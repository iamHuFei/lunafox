package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// WaybackEntry is one historical URL from the Internet Archive CDX index.
type WaybackEntry struct {
	URL            string
	LastSeen       string // YYYYMMDDhhmmss as archived
	ArchivedStatus string // HTTP status the archiver recorded, "-" when absent
}

// LastSeenHuman renders the archive timestamp as 2006-01-02.
func (entry WaybackEntry) LastSeenHuman() string {
	if len(entry.LastSeen) >= 8 && isDigits(entry.LastSeen[:8]) {
		return entry.LastSeen[0:4] + "-" + entry.LastSeen[4:6] + "-" + entry.LastSeen[6:8]
	}
	return entry.LastSeen
}

func isDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// waybackStaticExtensions never represent attack surface; the archive is full
// of them and they would bury real paths.
var waybackStaticExtensions = []string{
	".css", ".js", ".jpg", ".jpeg", ".png", ".gif", ".ico", ".woff", ".woff2",
	".ttf", ".svg", ".eot", ".mp4", ".mp3", ".swf", ".webp", ".map",
}

// FetchWaybackURLs queries the Wayback CDX index for the target domain
// (including subdomains), applies the ingestion gates, and returns entries
// ordered most-recently-archived first:
//
//  1. static-asset extensions dropped;
//  2. URLs whose last archived status was 404 dropped (dead even then);
//  3. optional max-age filter in years against the last-seen timestamp;
//  4. optional limit (0 keeps everything).
//
// proxyURL routes the CDX API call itself (web.archive.org is unreachable
// from some networks without one).
// waybackBaseURL allows tests to point the CDX client at a stub server.
var waybackBaseURL = "https://web.archive.org/cdx/search/cdx"

// FetchWaybackURLsVia is FetchWaybackURLs with an explicit CDX endpoint.
func FetchWaybackURLsVia(base, target, proxyURL string, limit, maxAgeYears int, now time.Time) ([]WaybackEntry, error) {
	saved := waybackBaseURL
	waybackBaseURL = base
	defer func() { waybackBaseURL = saved }()
	return FetchWaybackURLs(context.Background(), target, proxyURL, limit, maxAgeYears, now)
}

func FetchWaybackURLs(ctx context.Context, target, proxyURL string, limit, maxAgeYears int, now time.Time) ([]WaybackEntry, error) {
	endpoint := waybackBaseURL + "?" + url.Values{
		"url":       {target},
		"matchType": {"domain"},
		"output":    {"text"},
		"collapse":  {"urlkey"},
		"fl":        {"original,timestamp,statuscode"},
		"limit":     {"5000"},
		"filter":    {"statuscode:^[23]"},
	}.Encode()
	transport := &http.Transport{}
	if parsed, err := url.Parse(proxyURL); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "socks5") {
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Timeout: 120 * time.Second, Transport: transport}
	// The CDX endpoint answers 503 under load fairly often; it is transient,
	// so retry with backoff instead of failing the whole historical source.
	var body []byte
	var lastStatus int
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 5 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build wayback request: %w", err)
		}
		request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		response, err := client.Do(request)
		if err != nil {
			lastStatus = 0
			continue
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		response.Body.Close()
		if response.StatusCode == http.StatusOK && readErr == nil {
			body = payload
			break
		}
		lastStatus = response.StatusCode
		if response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusTooManyRequests {
			return nil, fmt.Errorf("wayback cdx status %d", response.StatusCode)
		}
	}
	if body == nil {
		if lastStatus == 0 {
			return nil, fmt.Errorf("wayback request failed after retries")
		}
		return nil, fmt.Errorf("wayback cdx status %d after retries", lastStatus)
	}

	cutoff := ""
	if maxAgeYears > 0 {
		cutoff = now.AddDate(-maxAgeYears, 0, 0).Format("20060102")
	}
	seen := make(map[string]struct{})
	entries := make([]WaybackEntry, 0, 256)
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rawURL := fields[0]
		lastSeen := fields[1]
		status := "-"
		if len(fields) >= 3 {
			status = fields[2]
		}
		normalized := normalizeWaybackURL(rawURL)
		if normalized == "" {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		if isWaybackStaticAsset(normalized) {
			continue
		}
		if status == "404" || status == "410" {
			continue
		}
		if cutoff != "" && isDigits(lastSeen) && lastSeen < cutoff {
			continue
		}
		seen[normalized] = struct{}{}
		entries = append(entries, WaybackEntry{URL: normalized, LastSeen: lastSeen, ArchivedStatus: status})
	}
	// Most recently archived first: fresh paths surface at the top and the
	// limit keeps the most relevant slice when the archive is deep.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].LastSeen > entries[j].LastSeen })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// normalizeWaybackURL strips default ports and trims trailing slashes so
// archive URLs dedupe cleanly against search-engine URLs.
func normalizeWaybackURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "http") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	if parsed.Port() == "80" && parsed.Scheme == "http" {
		parsed.Host = parsed.Hostname()
	}
	if parsed.Port() == "443" && parsed.Scheme == "https" {
		parsed.Host = parsed.Hostname()
	}
	return strings.TrimSuffix(parsed.String(), "/")
}

func isWaybackStaticAsset(normalized string) bool {
	lower := strings.ToLower(normalized)
	query := strings.IndexByte(lower, '?')
	path := lower
	if query >= 0 {
		path = lower[:query]
	}
	for _, extension := range waybackStaticExtensions {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}
