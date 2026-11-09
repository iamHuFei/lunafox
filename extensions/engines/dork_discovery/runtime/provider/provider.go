// Package provider abstracts search-engine providers behind one interface so
// additional engines (DuckDuckGo, Google behind an egress proxy) can join
// without touching the pipeline. Bing is the first provider because it is the
// only one reachable from the deployment egress and it honors Google-style
// dork operators in international mode (ensearch=1).
package provider

import "context"

// Hit is one organic result extracted from a search page. Date and Snippet
// carry the crawl-date badge and the result summary Bing renders beside each
// organic entry; they become search-engine evidence in the submitted assets.
type Hit struct {
	URL     string
	Title   string
	Date    string
	Snippet string
}

// Page is one fetched result page. Blocked reports provider-side throttling
// or challenge pages; the pipeline stops paginating that dork but keeps the
// hits already harvested.
type Page struct {
	StatusCode int
	Hits       []Hit
	Blocked    bool
	// Anomalies counts result blocks that existed but could not be parsed;
	// surfaced through progress so HTML drift stays observable.
	Anomalies int
	// NextPageURL is the pagination URL Bing itself rendered for the next
	// page. Bing ignores bare first= offsets without the session fingerprint
	// (FPIG) embedded in its own pager links, so pagination must follow this
	// URL instead of rebuilding the query.
	NextPageURL string
}

// SearchProvider queries one dork. first is the 1-based result offset and
// count the requested page size.
type SearchProvider interface {
	Name() string
	Search(ctx context.Context, query string, first, count int) (Page, error)
}

// CloseIfCloser releases providers that hold OS resources (the headless
// browser); plain HTTP providers need no teardown.
func CloseIfCloser(searchProvider SearchProvider) {
	if closer, ok := searchProvider.(interface{ Close() }); ok {
		closer.Close()
	}
}
