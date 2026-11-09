package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	enginecontract "github.com/yyhuni/lunafox/engines/dork_discovery/contract"
	"github.com/yyhuni/lunafox/engines/dork_discovery/runtime/provider"
)

// ProgressReporter is the message-only Engine API port used by Dork
// Discovery. A failed acknowledgement is terminal for the same reason as URL
// Collection: continuing after losing the required execution-state channel
// could report a falsely successful task.
type ProgressReporter interface {
	Report(context.Context, string) error
}

// Runtime owns Dork Discovery's search-and-probe pipeline. It receives only
// generated Engine inputs and ports; workflow state, Server repositories,
// and Agent materialization capabilities stay outside this boundary.
type Runtime struct {
	newProvider func(requestTimeout time.Duration, proxyURL string, useBrowser bool) provider.SearchProvider
}

func New() *Runtime {
	return &Runtime{newProvider: func(requestTimeout time.Duration, proxyURL string, useBrowser bool) provider.SearchProvider {
		if useBrowser {
			if browser := provider.NewBrowserBingProvider("", proxyURL); browser != nil {
				return browser
			}
		}
		return provider.NewBingProvider(requestTimeout, proxyURL)
	}}
}

// Execute validates the typed execution and runs the dork pipeline:
// render dorks → search pages → harvest in-scope URLs/hosts → probe → submit.
func (r *Runtime) Execute(ctx context.Context, execution *enginecontract.Execution) error {
	if ctx == nil {
		return errors.New("execution context is required")
	}
	if execution == nil {
		return errors.New("execution is required")
	}
	if execution.Progress == nil {
		return errors.New("engine progress port is required")
	}
	if execution.Results.Subdomains == nil {
		return errors.New("typed Subdomain result submitter is required")
	}
	if execution.Results.EndpointV2 == nil {
		return errors.New("typed EndpointV2 result submitter is required")
	}
	if execution.Workspace == "" {
		return errors.New("workspace is required")
	}
	if info, err := os.Stat(execution.Workspace); err != nil || !info.IsDir() {
		if err != nil {
			return fmt.Errorf("dork discovery workspace: %w", err)
		}
		return errors.New("dork discovery workspace is not a directory")
	}
	if r == nil || r.newProvider == nil {
		return errors.New("dork discovery search provider factory is required")
	}
	if execution.Target.Type != enginecontract.TargetTypeDomain && execution.Target.Type != enginecontract.TargetTypeIP {
		return errors.New("dork discovery requires a domain or ip target")
	}
	if !execution.Config.DorkSearch.Enabled {
		if err := execution.Progress.Report(ctx, "dork_search disabled; nothing to do"); err != nil {
			return fmt.Errorf("report disabled progress: %w", err)
		}
		return nil
	}
	return runPipeline(ctx, r.newProvider, execution)
}

// canonicalTargetDomain normalizes the target domain for dork rendering and
// scope matching.
func canonicalTargetDomain(target string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target), "."))
}
