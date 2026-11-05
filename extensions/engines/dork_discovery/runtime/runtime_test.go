package runtime

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	enginecontract "github.com/yyhuni/lunafox/engines/dork_discovery/contract"
	"github.com/yyhuni/lunafox/engines/dork_discovery/runtime/provider"
)

func TestHostInScope(t *testing.T) {
	cases := []struct {
		host   string
		target string
		want   bool
	}{
		{"example.com", "example.com", true},
		{"www.example.com", "example.com", true},
		{"a.b.example.com", "example.com", true},
		{"EXAMPLE.com.", "example.com", true},
		{"notexample.com", "example.com", false},
		{"example.com.evil.net", "example.com", false},
		{"", "example.com", false},
		{"www.example.com", "", false},
		{"112.13.167.113", "112.13.167.113", true},
		{"112.13.167.113", "112.13.167.114", false},
		{"sub.112.13.167.113", "112.13.167.113", false},
	}
	for _, item := range cases {
		if got := HostInScope(item.host, item.target); got != item.want {
			t.Fatalf("HostInScope(%q, %q) = %v, want %v", item.host, item.target, got, item.want)
		}
	}
}

func TestRenderDorks(t *testing.T) {
	got := renderDorks([]string{"site:{target}", " site:{target} inurl:login ", "", "{target}"}, "Example.COM")
	want := []string{"site:example.com", "site:example.com inurl:login"}
	// {target} alone renders to the bare domain, which is a valid dork.
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != "example.com" {
		t.Fatalf("renderDorks = %v", got)
	}
}

type recordingProgress struct {
	messages []string
}

func (progress *recordingProgress) Report(_ context.Context, message string) error {
	progress.messages = append(progress.messages, message)
	return nil
}

type recordingSubmitters struct {
	subdomains []enginecontract.Subdomain
	endpoints  []enginecontract.EndpointV2
}

func (recorder *recordingSubmitters) SubmitSubdomains(_ context.Context, items <-chan enginecontract.Subdomain) error {
	for item := range items {
		recorder.subdomains = append(recorder.subdomains, item)
	}
	return nil
}

func (recorder *recordingSubmitters) SubmitEndpoints(_ context.Context, items <-chan enginecontract.EndpointV2) error {
	for item := range items {
		recorder.endpoints = append(recorder.endpoints, item)
	}
	return nil
}

type fakeProvider struct {
	pages map[string]provider.Page
	calls []string
}

func (fake *fakeProvider) Name() string { return "fake" }

func (fake *fakeProvider) Search(_ context.Context, query string, first, count int) (provider.Page, error) {
	fake.calls = append(fake.calls, query)
	if page, ok := fake.pages[query]; ok {
		return page, nil
	}
	return provider.Page{StatusCode: 200}, nil
}

type failingProvider struct{ calls int }

func (fake *failingProvider) Name() string { return "failing" }

func (fake *failingProvider) Search(_ context.Context, _ string, _, _ int) (provider.Page, error) {
	fake.calls++
	return provider.Page{}, errors.New("network down")
}

// pipelineHarness wires a full execution with injected provider and probe
// targets so the whole flow runs without Bing or real assets.
type pipelineHarness struct {
	execution *enginecontract.Execution
	progress  *recordingProgress
	recorder  *recordingSubmitters
	provider  *fakeProvider
}

func newPipelineHarness(t *testing.T, dorks []string) *pipelineHarness {
	t.Helper()
	workspace := t.TempDir()
	harness := &pipelineHarness{
		progress: &recordingProgress{},
		recorder: &recordingSubmitters{},
		provider: &fakeProvider{pages: map[string]provider.Page{}},
	}
	harness.execution = &enginecontract.Execution{
		Target:    enginecontract.Target{Type: enginecontract.TargetTypeDomain, Value: "example.com"},
		Workspace: workspace,
		Progress:  harness.progress,
		Config: enginecontract.Config{
			DorkSearch: enginecontract.DorkSearchConfig{
				Enabled:        true,
				Dorks:          dorks,
				Provider:       "bing",
				MaxPages:       1,
				PageSize:       20,
				RequestTimeout: 5,
				RequestDelayMS: 0,
			},
			Probe: enginecontract.ProbeConfig{
				Enabled:         true,
				RequestTimeout:  5,
				Concurrency:     2,
				BodyLimitKB:     64,
				FollowRedirects: true,
				Timeout:         60,
			},
		},
	}
	harness.execution.Results.Subdomains = submitterFunc(harness.recorder.SubmitSubdomains)
	harness.execution.Results.EndpointV2 = endpointSubmitterFunc(harness.recorder.SubmitEndpoints)
	return harness
}

type submitterFunc func(context.Context, <-chan enginecontract.Subdomain) error

func (fn submitterFunc) Submit(ctx context.Context, items <-chan enginecontract.Subdomain) error {
	return fn(ctx, items)
}

type endpointSubmitterFunc func(context.Context, <-chan enginecontract.EndpointV2) error

func (fn endpointSubmitterFunc) Submit(ctx context.Context, items <-chan enginecontract.EndpointV2) error {
	return fn(ctx, items)
}

func TestExecuteRejectsInvalidExecutions(t *testing.T) {
	engine := New()
	if err := engine.Execute(nil, &enginecontract.Execution{}); err == nil {
		t.Fatal("nil context must fail")
	}
	if err := engine.Execute(context.Background(), nil); err == nil {
		t.Fatal("nil execution must fail")
	}
	workspace := t.TempDir()
	base := func() *enginecontract.Execution {
		execution := &enginecontract.Execution{
			Target:    enginecontract.Target{Type: enginecontract.TargetTypeDomain, Value: "example.com"},
			Workspace: workspace,
			Progress:  &recordingProgress{},
		}
		execution.Results.Subdomains = submitterFunc(func(context.Context, <-chan enginecontract.Subdomain) error { return nil })
		execution.Results.EndpointV2 = endpointSubmitterFunc(func(context.Context, <-chan enginecontract.EndpointV2) error { return nil })
		return execution
	}
	if err := engine.Execute(context.Background(), &enginecontract.Execution{Workspace: workspace}); err == nil {
		t.Fatal("missing progress and submitters must fail")
	}
	cidrTarget := base()
	cidrTarget.Target.Type = enginecontract.TargetTypeCIDR
	if err := engine.Execute(context.Background(), cidrTarget); err == nil {
		t.Fatal("cidr target must fail")
	}
	notADir := base()
	notADir.Workspace = "/nonexistent-directory-xyz"
	if err := engine.Execute(context.Background(), notADir); err == nil {
		t.Fatal("missing workspace must fail")
	}
	disabled := base()
	disabled.Config.DorkSearch = enginecontract.DorkSearchConfig{Enabled: false}
	disabled.Config.Probe = enginecontract.ProbeConfig{Enabled: true}
	if err := engine.Execute(context.Background(), disabled); err != nil {
		t.Fatalf("disabled search should no-op: %v", err)
	}
}

func TestExecuteDisabledProbeStillSubmitsSubdomains(t *testing.T) {
	harness := newPipelineHarness(t, []string{"site:example.com"})
	harness.execution.Config.Probe.Enabled = false
	harness.provider.pages["site:example.com"] = provider.Page{
		StatusCode: 200,
		Hits: []provider.Hit{
			{URL: "https://api.example.com/login"},
			{URL: "https://evil.net/x"},
		},
	}
	engine := &Runtime{newProvider: func(_ time.Duration) provider.SearchProvider { return harness.provider }}
	if err := engine.Execute(context.Background(), harness.execution); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(harness.recorder.subdomains) != 1 || harness.recorder.subdomains[0].DNSName != "api.example.com" {
		t.Fatalf("subdomains = %+v", harness.recorder.subdomains)
	}
	if len(harness.recorder.endpoints) != 0 {
		t.Fatalf("disabled probe must not submit endpoints: %+v", harness.recorder.endpoints)
	}
	if !progressContains(harness.progress.messages, "result_submission completed subdomains=1 endpoints=0") {
		t.Fatalf("progress missing submission summary: %v", harness.progress.messages)
	}
}

func progressContains(messages []string, fragment string) bool {
	for _, message := range messages {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func TestExecuteFailingProviderContinuesAndReports(t *testing.T) {
	harness := newPipelineHarness(t, []string{"site:example.com"})
	failing := &failingProvider{}
	execution := harness.execution
	if err := harvestAndSubmit(context.Background(), failing, execution); err != nil {
		t.Fatalf("a failing provider must not fail the run: %v", err)
	}
	if !progressContains(harness.progress.messages, "dork_query error") {
		t.Fatalf("dork failure must be reported: %v", harness.progress.messages)
	}
	if len(harness.recorder.subdomains) != 0 || len(harness.recorder.endpoints) != 0 {
		t.Fatalf("nothing should be submitted: %+v %+v", harness.recorder.subdomains, harness.recorder.endpoints)
	}
}

func harvestAndSubmit(ctx context.Context, search provider.SearchProvider, execution *enginecontract.Execution) error {
	dorks := renderDorks(execution.Config.DorkSearch.Dorks, canonicalTargetDomain(execution.Target.Value))
	_, hosts, err := harvest(ctx, search, execution.Progress, dorks, execution.Config.DorkSearch, canonicalTargetDomain(execution.Target.Value))
	if err != nil {
		return err
	}
	return submitResults(ctx, execution, hosts, nil)
}

func TestWorkspaceValidationCoversFileWorkspace(t *testing.T) {
	file := t.TempDir() + "/file"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	execution := &enginecontract.Execution{Workspace: file}
	if err := New().Execute(context.Background(), execution); err == nil {
		t.Fatal("file workspace must fail")
	}
}
