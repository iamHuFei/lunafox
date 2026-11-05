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

func runPipeline(ctx context.Context, newProvider func(time.Duration) provider.SearchProvider, execution *enginecontract.Execution) error {
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

	search := newProvider(time.Duration(config.DorkSearch.RequestTimeout) * time.Second)
	urls, hosts, searchErr := harvest(ctx, search, execution.Progress, dorks, config.DorkSearch, target)
	if searchErr != nil {
		return searchErr
	}
	if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
		"harvest completed dorks=%d urls=%d hosts=%d", len(dorks), len(urls), len(hosts))); reportErr != nil {
		return fmt.Errorf("report harvest progress: %w", reportErr)
	}

	if !config.Probe.Enabled || len(urls) == 0 {
		return submitResults(ctx, execution, hosts, nil)
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
	})
	outcomes, unreachable := engine.ProbeAll(probeCtx, urls)
	if reportErr := execution.Progress.Report(ctx, fmt.Sprintf(
		"probe completed ok=%d unreachable=%d", len(outcomes), unreachable)); reportErr != nil {
		return fmt.Errorf("report probe progress: %w", reportErr)
	}
	return submitResults(ctx, execution, hosts, outcomes)
}

// harvest queries every rendered dork across the configured page budget and
// returns deduplicated in-scope URLs and hosts.
func harvest(ctx context.Context, search provider.SearchProvider, progress enginecontract.Progress, dorks []string, config enginecontract.DorkSearchConfig, target string) ([]string, []string, error) {
	urlSet := make(map[string]struct{})
	hostSet := make(map[string]struct{})
	anomalies := 0
	outOfScope := 0
	for _, dork := range dorks {
		blocked := false
		for page := 0; page < int(config.MaxPages) && !blocked; page++ {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			first := page*int(config.PageSize) + 1
			result, err := search.Search(ctx, dork, first, int(config.PageSize))
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				// A failing dork must not fail the whole task; surface it and
				// continue with the remaining templates.
				if reportErr := progress.Report(ctx, fmt.Sprintf("dork_query error dork=%q error=%v", dork, err)); reportErr != nil {
					return nil, nil, fmt.Errorf("report dork failure: %w", reportErr)
				}
				break
			}
			if result.Blocked {
				blocked = true
				if reportErr := progress.Report(ctx, fmt.Sprintf("dork_query blocked dork=%q page=%d", dork, page+1)); reportErr != nil {
					return nil, nil, fmt.Errorf("report dork block: %w", reportErr)
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
				hostSet[host] = struct{}{}
				urlSet[strings.TrimSuffix(hit.URL, "/")] = struct{}{}
			}
			anomalies += result.Anomalies
			if reportErr := progress.Report(ctx, fmt.Sprintf(
				"dork_query completed dork=%q page=%d hits=%d anomalies=%d", dork, page+1, len(result.Hits), result.Anomalies)); reportErr != nil {
				return nil, nil, fmt.Errorf("report dork progress: %w", reportErr)
			}
			if len(result.Hits) == 0 {
				break
			}
			if delay := time.Duration(config.RequestDelayMS) * time.Millisecond; delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				}
			}
		}
	}
	urls := sortedKeys(urlSet)
	hosts := sortedKeys(hostSet)
	if outOfScope > 0 || anomalies > 0 {
		if reportErr := progress.Report(ctx, fmt.Sprintf("harvest_notes outOfScope=%d anomalies=%d", outOfScope, anomalies)); reportErr != nil {
			return nil, nil, fmt.Errorf("report harvest notes: %w", reportErr)
		}
	}
	return urls, hosts, nil
}

func submitResults(ctx context.Context, execution *enginecontract.Execution, hosts []string, outcomes []prober.Outcome) error {
	if err := submitSubdomains(ctx, execution, hosts); err != nil {
		return err
	}
	if err := submitEndpoints(ctx, execution, outcomes); err != nil {
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

func submitEndpoints(ctx context.Context, execution *enginecontract.Execution, outcomes []prober.Outcome) error {
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
