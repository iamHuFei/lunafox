package runtime

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	enginecontract "github.com/yyhuni/lunafox/engines/dork_discovery/contract"
	"github.com/yyhuni/lunafox/engines/dork_discovery/runtime/prober"
	"github.com/yyhuni/lunafox/engines/dork_discovery/runtime/provider"
)

func runPipeline(ctx context.Context, newProvider func(time.Duration, string, bool) provider.SearchProvider, execution *enginecontract.Execution) error {
	config := execution.Config
	target := canonicalTargetDomain(execution.Target.Value)
	if target == "" {
		return fmt.Errorf("dork discovery requires a non-empty domain target")
	}
	if config.DorkSearch.Provider != "bing" {
		return fmt.Errorf("unsupported dork search provider %q", config.DorkSearch.Provider)
	}
	dorks := renderDorks(config.DorkSearch.Dorks, target)
	if len(dorks) == 0 {
		return fmt.Errorf("dork discovery requires at least one rendered dork")
	}

	search := newProvider(time.Duration(config.DorkSearch.RequestTimeout)*time.Second, config.DorkSearch.ProxyURL, config.DorkSearch.UseBrowser)
	defer provider.CloseIfCloser(search)
	urls, hosts, titles, snippets, searchErr := harvest(ctx, search, execution.Progress, dorks, config.DorkSearch, target)
	if searchErr != nil {
		return searchErr
	}
	if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
		"harvest completed dorks=%d urls=%d hosts=%d", len(dorks), len(urls), len(hosts))); reportErr != nil {
		return fmt.Errorf("report harvest progress: %w", reportErr)
	}

	// The Wayback merge is the historical-depth source: one CDX dump per
	// target, gated (static assets and archived-404s dropped, recency-sorted,
	// capped) and deduped against the search-engine URLs at no extra cost.
	urls, snippets, waybackHosts, waybackErr := mergeWayback(ctx, execution, config, target, urls, snippets)
	if waybackErr != nil {
		if reportErr := execution.Progress.Report(ctx, fmt.Sprintf("wayback error=%v", waybackErr)); reportErr != nil {
			return fmt.Errorf("report wayback failure: %w", reportErr)
		}
	} else if waybackHosts != nil {
		for host := range waybackHosts {
			if !strings.HasPrefix(host, "__") && host != target {
				hosts = appendUniqueHost(hosts, host)
			}
		}
		if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
			"wayback completed kept=%s added=%s totalUrls=%d", waybackHosts["__kept__"], waybackHosts["__added__"], len(urls))); reportErr != nil {
			return fmt.Errorf("report wayback progress: %w", reportErr)
		}
	}

	// An IP target has no subdomain namespace; hosts would all equal the
	// target address, which server-side subdomain ingestion would reject.
	if IsIPTarget(target) {
		hosts = nil
	}

	if !config.Probe.Enabled || len(urls) == 0 {
		// With probing disabled the search-engine evidence is all there is:
		// record mode still submits bare endpoints so the URLs stay visible.
		bare := bareEndpoints(urls, titles)
		if !config.Probe.Enabled && recordUnreachable(config.Probe) {
			return submitResults(ctx, execution, hosts, bare, snippets)
		}
		return submitResults(ctx, execution, hosts, nil, snippets)
	}

	probeBudget := time.Duration(config.Probe.Timeout) * time.Second
	if probeBudget <= 0 {
		probeBudget = 10 * time.Minute
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()
	engine := prober.New(prober.Options{
		Concurrency:     int(config.Probe.Concurrency),
		RequestTimeout:  time.Duration(config.Probe.RequestTimeout) * time.Second,
		BodyLimitBytes:  config.Probe.BodyLimitKB * 1024,
		FollowRedirects: config.Probe.FollowRedirects,
		// Probe vantage fallback: direct first, proxy retry when the direct
		// path is blocked, so datacenter-hostile targets still verify.
		ProxyURL: config.DorkSearch.ProxyURL,
	})
	outcomes, unreachableURLs := engine.ProbeAll(probeCtx, urls)
	if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
		"probe completed ok=%d unreachable=%d", len(outcomes), len(unreachableURLs))); reportErr != nil {
		return fmt.Errorf("report probe progress: %w", reportErr)
	}
	if recordUnreachable(config.Probe) {
		// Search-engine indexing is itself asset evidence: record mode keeps
		// probe-unreachable URLs as bare endpoints (empty status code marks
		// them unverified, e.g. when the target WAF drops datacenter IPs).
		outcomes = append(outcomes, bareEndpoints(unreachableURLs, titles)...)
		if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
			"probe_unreachable recorded=%d note=statusCode empty means search-indexed but unverified", len(unreachableURLs))); reportErr != nil {
			return fmt.Errorf("report unreachable note: %w", reportErr)
		}
	}
	return submitResults(ctx, execution, hosts, outcomes, snippets)
}

// harvest queries every rendered dork across the configured page budget and
// returns deduplicated in-scope URLs and hosts.
func harvest(ctx context.Context, search provider.SearchProvider, progress enginecontract.Progress, dorks []string, config enginecontract.DorkSearchConfig, target string) ([]string, []string, map[string]string, map[string]string, error) {
	urlSet := make(map[string]struct{})
	hostSet := make(map[string]struct{})
	titles := make(map[string]string)
	snippets := make(map[string]string)
	anomalies := 0
	outOfScope := 0
	for _, dork := range dorks {
		blocked := false
		previousPageKey := ""
		for page := 0; page < int(config.MaxPages) && !blocked; page++ {
			pageInScope := 0
			pageHits := make([]string, 0, 16)
			if ctx.Err() != nil {
				return nil, nil, nil, nil, ctx.Err()
			}
			first := page*int(config.PageSize) + 1
			result, err := search.Search(ctx, dork, first, int(config.PageSize))
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil, nil, nil, ctx.Err()
				}
				// A failing dork must not fail the whole task; surface it and
				// continue with the remaining templates.
				if reportErr := progress.Report(ctx, fmt.Sprintf("dork_query error dork=%q error=%v", dork, err)); reportErr != nil {
					return nil, nil, nil, nil, fmt.Errorf("report dork failure: %w", reportErr)
				}
				break
			}
			if result.Blocked {
				blocked = true
				if reportErr := progress.Report(ctx, fmt.Sprintf("dork_query blocked dork=%q page=%d", dork, page+1)); reportErr != nil {
					return nil, nil, nil, nil, fmt.Errorf("report dork block: %w", reportErr)
				}
				break
			}
			for _, hit := range result.Hits {
				parsed, err := url.Parse(hit.URL)
				if err != nil || parsed.Host == "" {
					continue
				}
				host := strings.ToLower(parsed.Hostname())
				if !HostInScope(host, target) {
					outOfScope++
					continue
				}
				// The server-side subdomain ingestion excludes the apex
				// target itself and escalates that item to a whole-batch
				// PermissionDenied, so the apex never enters the host set.
				if host != target {
					hostSet[host] = struct{}{}
				}
				pageInScope++
				normalized := strings.TrimSuffix(hit.URL, "/")
				urlSet[normalized] = struct{}{}
				pageHits = append(pageHits, normalized)
				if hit.Title != "" {
					titles[normalized] = hit.Title
				}
				if evidence := searchEvidence(hit); evidence != "" {
					snippets[normalized] = evidence
				}
			}
			anomalies += result.Anomalies
			if reportErr := progress.Report(ctx, fmt.Sprintf(
				"dork_query completed dork=%q page=%d hits=%d anomalies=%d", dork, page+1, len(result.Hits), result.Anomalies)); reportErr != nil {
				return nil, nil, nil, nil, fmt.Errorf("report dork progress: %w", reportErr)
			}
			// A page with zero in-scope hits means the dork is exhausted (or
			// generalized); fetching further pages only burns quota.
			if pageInScope == 0 {
				break
			}
			// Bing serves cookie-less JS-less sessions the same page for every
			// first= offset; repeating content means pagination is exhausted.
			sort.Strings(pageHits)
			pageKey := strings.Join(pageHits, "\x00")
			if pageKey == previousPageKey {
				if reportErr := progress.Report(ctx, fmt.Sprintf(
					"dork_query repeated dork=%q page=%d; pagination exhausted", dork, page+1)); reportErr != nil {
					return nil, nil, nil, nil, fmt.Errorf("report repeat note: %w", reportErr)
				}
				break
			}
			previousPageKey = pageKey
			if delay := time.Duration(config.RequestDelayMS) * time.Millisecond; delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, nil, nil, nil, ctx.Err()
				}
			}
		}
	}
	urls := sortedKeys(urlSet)
	hosts := sortedKeys(hostSet)
	if outOfScope > 0 || anomalies > 0 {
		if reportErr := progress.Report(ctx, fmt.Sprintf("harvest_notes outOfScope=%d anomalies=%d", outOfScope, anomalies)); reportErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("report harvest notes: %w", reportErr)
		}
	}
	return urls, hosts, titles, snippets, nil
}

// searchEvidence renders the crawl-date badge and result summary Bing showed
// beside the hit, e.g. "[bing 2024/5/12] 描述文本".
func searchEvidence(hit provider.Hit) string {
	parts := make([]string, 0, 2)
	if hit.Date != "" {
		parts = append(parts, hit.Date)
	}
	if hit.Snippet != "" {
		parts = append(parts, hit.Snippet)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[bing " + strings.Join(parts, "] [") + "]"
}

// recordUnreachable reports whether probe-unreachable URLs should still be
// submitted as bare search-indexed endpoints.
func recordUnreachable(probe enginecontract.ProbeConfig) bool {
	return probe.UnreachablePolicy != "skip"
}

// bareEndpoints builds endpoint items that carry only search-engine
// evidence: URL, host, and the search-result title when present. The empty
// status code is the "unverified" marker in the asset tables.
func bareEndpoints(urls []string, titles map[string]string) []prober.Outcome {
	outcomes := make([]prober.Outcome, 0, len(urls))
	for _, rawURL := range urls {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" {
			continue
		}
		outcomes = append(outcomes, prober.Outcome{
			URL:   rawURL,
			Host:  parsed.Hostname(),
			Title: titles[rawURL],
		})
	}
	return outcomes
}

func submitResults(ctx context.Context, execution *enginecontract.Execution, hosts []string, outcomes []prober.Outcome, snippets map[string]string) error {
	if err := submitSubdomains(ctx, execution, hosts); err != nil {
		return err
	}
	if err := submitEndpoints(ctx, execution, outcomes, snippets); err != nil {
		return err
	}
	return execution.Progress.Report(ctx, fmt.Sprintf(
		"result_submission completed subdomains=%d endpoints=%d", len(hosts), len(outcomes)))
}

func submitSubdomains(ctx context.Context, execution *enginecontract.Execution, hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	items := make(chan enginecontract.Subdomain)
	submitDone := make(chan error, 1)
	go func() {
		submitDone <- execution.Results.Subdomains.Submit(ctx, items)
	}()
	for _, host := range hosts {
		select {
		case items <- enginecontract.Subdomain{DNSName: host}:
		case <-ctx.Done():
			close(items)
			return fmt.Errorf("submit subdomains: %w", ctx.Err())
		}
	}
	close(items)
	if err := <-submitDone; err != nil {
		return fmt.Errorf("submit subdomains: %w", err)
	}
	return nil
}

func submitEndpoints(ctx context.Context, execution *enginecontract.Execution, outcomes []prober.Outcome, snippets map[string]string) error {
	if len(outcomes) == 0 {
		return nil
	}
	items := make(chan enginecontract.EndpointV2)
	submitDone := make(chan error, 1)
	go func() {
		submitDone <- execution.Results.EndpointV2.Submit(ctx, items)
	}()
	for _, outcome := range outcomes {
		item := enginecontract.EndpointV2{
			URL:           outcome.URL,
			Host:          outcome.Host,
			StatusCode:    outcome.StatusCode,
			ContentLength: outcome.ContentLength,
			ContentType:   outcome.ContentType,
			Webserver:     outcome.Webserver,
			Location:      outcome.Location,
		}
		if outcome.Title != "" {
			item.Title = outcome.Title
		}
		if snippets[outcome.URL] != "" {
			item.ResponseBody = snippets[outcome.URL]
		}
		select {
		case items <- item:
		case <-ctx.Done():
			close(items)
			return fmt.Errorf("submit endpoints: %w", ctx.Err())
		}
	}
	close(items)
	if err := <-submitDone; err != nil {
		return fmt.Errorf("submit endpoints: %w", err)
	}
	return nil
}

// mergeWayback fetches the gated Wayback CDX entries for the target and
// merges them into the URL and snippet maps. Search-engine URLs win: an
// archive URL already present keeps its live snippet.
func mergeWayback(ctx context.Context, execution *enginecontract.Execution, config enginecontract.Config, target string, urls []string, snippets map[string]string) ([]string, map[string]string, map[string]string, error) {
	if !config.DorkSearch.Wayback {
		return urls, snippets, nil, nil
	}
	entries, err := provider.FetchWaybackURLs(ctx, target, config.DorkSearch.ProxyURL,
		int(config.DorkSearch.WaybackLimit), int(config.DorkSearch.WaybackMaxAgeYears), time.Now())
	if err != nil {
		return urls, snippets, nil, err
	}
	known := make(map[string]struct{}, len(urls))
	for _, existing := range urls {
		known[existing] = struct{}{}
	}
	hosts := make(map[string]string)
	merged := make([]string, 0, len(urls)+len(entries))
	merged = append(merged, urls...)
	added := 0
	for _, entry := range entries {
		if _, duplicate := known[entry.URL]; duplicate {
			continue
		}
		known[entry.URL] = struct{}{}
		merged = append(merged, entry.URL)
		if _, hasSnippet := snippets[entry.URL]; !hasSnippet {
			snippets[entry.URL] = fmt.Sprintf("[wayback %s archived %s]", entry.LastSeenHuman(), entry.ArchivedStatus)
		}
		if parsed, parseErr := url.Parse(entry.URL); parseErr == nil && parsed.Host != "" {
			hosts[parsed.Hostname()] = entry.URL
		}
		added++
	}
	hosts["__added__"] = fmt.Sprintf("%d", added)
	hosts["__kept__"] = fmt.Sprintf("%d", len(entries))
	return merged, snippets, hosts, nil
}

func appendUniqueHost(hosts []string, host string) []string {
	for _, existing := range hosts {
		if existing == host {
			return hosts
		}
	}
	return append(hosts, host)
}

func renderDorks(templates []string, target string) []string {
	normalized := canonicalTargetDomain(target)
	rendered := make([]string, 0, len(templates))
	for _, template := range templates {
		value := strings.TrimSpace(strings.ReplaceAll(template, "{target}", normalized))
		if value != "" {
			rendered = append(rendered, value)
		}
	}
	return rendered
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
