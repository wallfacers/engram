package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/memory"
)

// stubFilter is a deterministic in-process RelevanceFilter: tests script the
// probabilities, so selection, telemetry and degradation are observable without
// any network.
type stubFilter struct {
	probsFor  func(cands []filter.Candidate) []float64
	meta      filter.FilterMeta
	err       error
	calls     int
	lastCands []filter.Candidate
	lastQuery string
}

func (s *stubFilter) Filter(_ context.Context, query string, cands []filter.Candidate) ([]float64, filter.FilterMeta, error) {
	s.calls++
	s.lastQuery = query
	s.lastCands = cands
	if s.err != nil {
		return nil, filter.FilterMeta{Backend: filter.BackendJev, Degraded: true}, s.err
	}
	probs := uniformProbabilitiesFor(len(cands))
	if s.probsFor != nil {
		probs = s.probsFor(cands)
	}
	meta := s.meta
	if meta.Backend == "" {
		meta.Backend = filter.BackendJev
	}
	return probs, meta, nil
}

// stubGate is a deterministic WriteGate returning one scripted decision. It
// implements the canonical interface (Gate and GateWithOptions), recording the
// request form the memory_write wiring must fill in.
type stubGate struct {
	decision    filter.GateDecision
	err         error
	calls       int
	lastTurn    string
	lastDraft   string
	lastRequest filter.GateRequest
}

func (g *stubGate) Gate(ctx context.Context, userTurn, draft string) (filter.GateDecision, error) {
	return g.GateWithOptions(ctx, filter.GateRequest{UserTurn: userTurn, Draft: draft})
}

func (g *stubGate) GateWithOptions(_ context.Context, req filter.GateRequest) (filter.GateDecision, error) {
	g.calls++
	g.lastTurn = req.UserTurn
	g.lastDraft = req.Draft
	g.lastRequest = req
	if g.err != nil {
		return filter.GateDecision{Meta: filter.FilterMeta{Backend: filter.BackendJev}}, g.err
	}
	return g.decision, nil
}

func uniformProbabilitiesFor(n int) []float64 {
	probs := make([]float64, n)
	for i := range probs {
		probs[i] = 1
	}
	return probs
}

func testRegistry(t *testing.T, ctx context.Context, config RegistryConfig) *Registry {
	t.Helper()
	if config.DataDir == "" {
		config.DataDir = t.TempDir()
	}
	registry, err := NewRegistry(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

// registryFromEnv builds the registry the way cmd/engram-mcp does: configuration
// from the environment, clients built from that configuration.
func registryFromEnv(t *testing.T, ctx context.Context, env map[string]string) (*Registry, ServerConfig) {
	t.Helper()
	values := map[string]string{"ENGRAM_DATA_DIR": t.TempDir()}
	for key, value := range env {
		values[key] = value
	}
	config, err := LoadConfigWithEnv(nil, func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	searchFilter, err := BuildSearchFilter(config)
	if err != nil {
		t.Fatalf("build search filter: %v", err)
	}
	writeGate, err := BuildWriteGate(config)
	if err != nil {
		t.Fatalf("build write gate: %v", err)
	}
	return testRegistry(t, ctx, RegistryConfig{
		DataDir:          config.DataDir,
		SearchFilter:     searchFilter,
		SearchFilterName: config.SearchFilter,
		SearchPool:       config.SearchPool,
		SearchPolicy:     config.SearchPolicy(),
		WriteGate:        writeGate,
		WriteGateEnabled: config.JevWriteGate,
	}), config
}

func seedMemories(t *testing.T, registry *Registry, ctx context.Context, entries []memory.Entry) {
	t.Helper()
	handle := getForTest(t, registry, ctx, "default")
	for i := range entries {
		entry := entries[i]
		if err := handle.entries.Upsert(ctx, &entry); err != nil {
			t.Fatal(err)
		}
	}
}

func searchMemoryNames(t *testing.T, output map[string]any) []string {
	t.Helper()
	raw, ok := output["results"].([]any)
	if !ok {
		t.Fatalf("search output has no results array: %#v", output)
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	return names
}

func defaultCorpus() []memory.Entry {
	return []memory.Entry{
		{Name: "tea", Trigger: "morning drink", Content: "The user prefers jasmine tea in the morning."},
		{Name: "coffee", Trigger: "morning drink", Content: "The user sometimes drinks coffee after lunch."},
		{Name: "travel", Trigger: "next trip", Content: "The user plans a trip to Kyoto in spring."},
	}
}

// ledgerQuery matches every ledgerCorpus entry, so the wide-pool tests can count
// pool positions deterministically.
const ledgerQuery = "ledger"

func ledgerCorpus() []memory.Entry {
	return []memory.Entry{
		{Name: "alpha", Trigger: "ledger alpha", Content: "The user tracks alpha in the ledger."},
		{Name: "beta", Trigger: "ledger beta", Content: "The user tracks beta in the ledger."},
		{Name: "gamma", Trigger: "ledger gamma", Content: "The user tracks gamma in the ledger."},
	}
}

func TestMemorySearchUnchangedCallKeepsLegacyResponseShape(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{})
	seedMemories(t, registry, ctx, defaultCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	result := callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "morning drink",
		"limit": 8,
	})
	output := structuredMap(t, result)
	// A byte-level lock on top of the key-set assertion: the unchanged response
	// must not even mention the new telemetry fields.
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pool_size") || strings.Contains(string(encoded), `"filter"`) {
		t.Fatalf("unchanged call carries filter telemetry: %s", encoded)
	}
	// The parity invariant: with no new parameters and no server-side knobs, the
	// response carries exactly the legacy field set — no pool_size, no filter.
	wantKeys := []string{"degraded", "limit", "results", "returned", "scope"}
	gotKeys := make([]string, 0, len(output))
	for key := range output {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("unchanged call keys = %v, want exactly %v", gotKeys, wantKeys)
	}
	for i, key := range wantKeys {
		if gotKeys[i] != key {
			t.Fatalf("unchanged call keys = %v, want exactly %v", gotKeys, wantKeys)
		}
	}

	direct, err := getForTest(t, registry, ctx, "default").retriever.Search(ctx, "morning drink", 8)
	if err != nil {
		t.Fatal(err)
	}
	gotNames := searchMemoryNames(t, output)
	if len(gotNames) != len(direct) {
		t.Fatalf("unchanged call returned %v, direct retrieval %d hits", gotNames, len(direct))
	}
	for i, result := range direct {
		if gotNames[i] != result.Name {
			t.Fatalf("unchanged call order = %v, direct order index %d = %q", gotNames, i, result.Name)
		}
	}
}

func TestMemorySearchServerKnobsDoNotChangeResults(t *testing.T) {
	ctx := context.Background()
	plain := testRegistry(t, ctx, RegistryConfig{})
	knobs := testRegistry(t, ctx, RegistryConfig{SearchFilterName: filter.BackendNone, SearchPool: 150})
	for _, registry := range []*Registry{plain, knobs} {
		seedMemories(t, registry, ctx, defaultCorpus())
	}

	plainSession, _ := connectInMemory(t, ctx, NewServer(plain))
	knobsSession, _ := connectInMemory(t, ctx, NewServer(knobs))
	plainOutput := structuredMap(t, callTool(t, ctx, plainSession, "memory_search", map[string]any{"query": "morning drink", "limit": 8}))
	knobsOutput := structuredMap(t, callTool(t, ctx, knobsSession, "memory_search", map[string]any{"query": "morning drink", "limit": 8}))

	if got, want := searchMemoryNames(t, knobsOutput), searchMemoryNames(t, plainOutput); len(got) != len(want) {
		t.Fatalf("knob-enabled results = %v, plain = %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("server knobs changed result order: %v vs %v", got, want)
			}
		}
	}
	if knobsOutput["pool_size"] != float64(150) {
		t.Fatalf("knob-enabled call must report pool_size 150: %#v", knobsOutput["pool_size"])
	}
	if _, ok := knobsOutput["filter"]; !ok {
		t.Fatalf("knob-enabled call must report filter telemetry: %#v", knobsOutput)
	}
}

func TestMemorySearchExplicitPoolWithFilterNoneReportsTelemetry(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{})
	seedMemories(t, registry, ctx, defaultCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query":          "morning drink",
		"limit":          2,
		"candidate_pool": 150,
		"filter":         "none",
	}))
	if output["pool_size"] != float64(150) {
		t.Fatalf("pool_size = %v, want 150", output["pool_size"])
	}
	telemetry := output["filter"].(map[string]any)
	if telemetry["backend"] != filter.BackendNone || telemetry["degraded"] != false {
		t.Fatalf("filter telemetry = %#v", telemetry)
	}
	if telemetry["theta"] != filter.DefaultPolicy().Theta {
		t.Fatalf("theta = %v, want the configured default", telemetry["theta"])
	}
	if telemetry["kept"] != float64(len(searchMemoryNames(t, output))) {
		t.Fatalf("kept = %v does not match returned results %#v", telemetry["kept"], output["results"])
	}
	if _, ok := telemetry["latency_ms"]; !ok {
		t.Fatalf("filter telemetry must carry latency_ms: %#v", telemetry)
	}
	if _, ok := telemetry["cost_usd"]; !ok {
		t.Fatalf("filter telemetry must carry cost_usd: %#v", telemetry)
	}
}

func TestMemorySearchJevUnconfiguredDegradesWithoutNetwork(t *testing.T) {
	ctx := context.Background()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"probabilities":{}}`))
	}))
	defer server.Close()

	// Base URL and model are configured but the API key is missing, which is the
	// documented "Jev disabled" state: requesting jev must degrade honestly and
	// must not call out.
	registry, _ := registryFromEnv(t, ctx, map[string]string{
		"ENGRAM_JEV_BASE_URL": server.URL,
		"ENGRAM_JEV_MODEL":    "jev-2026-09-r3",
		"ENGRAM_FILTER":       filter.BackendJev,
		"ENGRAM_SEARCH_POOL":  "150",
	})
	seedMemories(t, registry, ctx, defaultCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "morning drink",
		"limit": 2,
	}))
	telemetry := output["filter"].(map[string]any)
	if telemetry["degraded"] != true {
		t.Fatalf("unconfigured Jev must report degraded=true: %#v", telemetry)
	}
	if telemetry["backend"] != filter.BackendNone {
		t.Fatalf("unconfigured Jev must not claim a backend it does not have: %#v", telemetry)
	}
	if requests.Load() != 0 {
		t.Fatalf("unconfigured Jev issued %d network requests, want 0", requests.Load())
	}
	direct, err := getForTest(t, registry, ctx, "default").retriever.Search(ctx, "morning drink", 2)
	if err != nil {
		t.Fatal(err)
	}
	got := searchMemoryNames(t, output)
	if len(got) != len(direct) {
		t.Fatalf("degraded shortlist = %v, RRF top-2 = %d hits", got, len(direct))
	}
	for i, result := range direct {
		if got[i] != result.Name {
			t.Fatalf("degraded shortlist = %v, RRF top-2 index %d = %q", got, i, result.Name)
		}
	}
}

func TestMemorySearchFilterAppliesThresholdPolicyOnWidePool(t *testing.T) {
	ctx := context.Background()
	stub := &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
		probs := make([]float64, len(cands))
		for i := range probs {
			// Keep the first and third pool entries only.
			if i == 0 || i == 2 {
				probs[i] = 0.9
			} else {
				probs[i] = 0.2
			}
		}
		return probs
	}}
	registry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter:     stub,
		SearchFilterName: filter.BackendJev,
		SearchPool:       3,
		SearchPolicy:     filter.DefaultPolicy(),
	})
	seedMemories(t, registry, ctx, ledgerCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{"query": ledgerQuery, "limit": 1}))
	wide, err := getForTest(t, registry, ctx, "default").retriever.Search(ctx, ledgerQuery, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide) != 3 {
		t.Fatalf("wide pool = %d hits, want the 3 seeded entries", len(wide))
	}
	if stub.calls != 1 {
		t.Fatalf("filter calls = %d, want 1", stub.calls)
	}
	if len(stub.lastCands) != len(wide) {
		t.Fatalf("filter saw %d candidates, wide pool = %d", len(stub.lastCands), len(wide))
	}
	if stub.lastQuery != ledgerQuery {
		t.Fatalf("filter query = %q", stub.lastQuery)
	}
	got := searchMemoryNames(t, output)
	want := []string{wide[0].Name, wide[2].Name}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("filtered shortlist = %v, want %v", got, want)
	}
	telemetry := output["filter"].(map[string]any)
	if telemetry["backend"] != filter.BackendJev || telemetry["kept"] != float64(2) {
		t.Fatalf("filter telemetry = %#v", telemetry)
	}
}

func TestMemorySearchHonestEmptyShortlistWhenNothingClearsTheta(t *testing.T) {
	ctx := context.Background()
	stub := &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
		probs := make([]float64, len(cands))
		for i := range probs {
			probs[i] = 0.1
		}
		return probs
	}}
	policy := filter.DefaultPolicy()
	policy.RelaxDisabled = true
	registry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter:     stub,
		SearchFilterName: filter.BackendJev,
		SearchPool:       3,
		SearchPolicy:     policy,
	})
	seedMemories(t, registry, ctx, ledgerCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{"query": ledgerQuery, "limit": 8}))
	results, ok := output["results"].([]any)
	if !ok {
		t.Fatalf("search output has no results array: %#v", output)
	}
	if len(results) != 0 {
		t.Fatalf("honest empty shortlist expected, got %#v", results)
	}
	if output["returned"] != float64(0) {
		t.Fatalf("returned = %v, want 0", output["returned"])
	}
	telemetry := output["filter"].(map[string]any)
	if telemetry["kept"] != float64(0) || telemetry["dropped"] != float64(3) {
		t.Fatalf("honest empty telemetry = %#v", telemetry)
	}
}

func TestMemorySearchRelaxStageKillSwitchChangesOutcome(t *testing.T) {
	ctx := context.Background()
	// 0.4 clears the relax threshold (0.35) but not theta (0.5).
	newStub := func() *stubFilter {
		return &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
			probs := make([]float64, len(cands))
			for i := range probs {
				probs[i] = 0.4
			}
			return probs
		}}
	}
	relaxed := filter.DefaultPolicy()
	strict := filter.DefaultPolicy()
	strict.RelaxDisabled = true

	relaxRegistry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter: newStub(), SearchFilterName: filter.BackendJev, SearchPool: 3, SearchPolicy: relaxed,
	})
	strictRegistry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter: newStub(), SearchFilterName: filter.BackendJev, SearchPool: 3, SearchPolicy: strict,
	})
	for _, registry := range []*Registry{relaxRegistry, strictRegistry} {
		seedMemories(t, registry, ctx, ledgerCorpus())
	}
	relaxSession, _ := connectInMemory(t, ctx, NewServer(relaxRegistry))
	strictSession, _ := connectInMemory(t, ctx, NewServer(strictRegistry))

	relaxOutput := structuredMap(t, callTool(t, ctx, relaxSession, "memory_search", map[string]any{"query": ledgerQuery, "limit": 8}))
	strictOutput := structuredMap(t, callTool(t, ctx, strictSession, "memory_search", map[string]any{"query": ledgerQuery, "limit": 8}))
	if got := len(searchMemoryNames(t, relaxOutput)); got != 3 {
		t.Fatalf("relax stage kept %d entries, want the 3 relaxed entries", got)
	}
	if got := searchMemoryNames(t, strictOutput); len(got) != 0 {
		t.Fatalf("RelaxDisabled must yield an honest empty shortlist, got %v", got)
	}
}

func TestMemorySearchPinnedTriggerBypassesThreshold(t *testing.T) {
	ctx := context.Background()
	stub := &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
		probs := make([]float64, len(cands))
		for i := range probs {
			probs[i] = 0.05
		}
		return probs
	}}
	policy := filter.DefaultPolicy()
	policy.RelaxDisabled = true
	registry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter: stub, SearchFilterName: filter.BackendJev, SearchPool: 3, SearchPolicy: policy,
	})
	seedMemories(t, registry, ctx, []memory.Entry{
		{Name: "pinned-rule", Trigger: "naming", Content: "The user pins this naming rule.", Pinned: true},
		{Name: "other", Trigger: "unrelated", Content: "Nothing relevant here."},
	})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{"query": "naming", "limit": 8}))
	got := searchMemoryNames(t, output)
	if len(got) != 1 || got[0] != "pinned-rule" {
		t.Fatalf("pinned trigger match must be injected first, got %v", got)
	}
}

func TestMemorySearchFilterFailureDegradesToFusedOrder(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		stub *stubFilter
	}{
		{name: "filter error", stub: &stubFilter{err: errors.New("jev down")}},
		{
			name: "probability count mismatch",
			stub: &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
				return []float64{0.9}
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := testRegistry(t, ctx, RegistryConfig{
				SearchFilter: test.stub, SearchFilterName: filter.BackendJev, SearchPool: 3, SearchPolicy: filter.DefaultPolicy(),
			})
			seedMemories(t, registry, ctx, defaultCorpus())
			clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

			output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
				"query": "morning drink", "limit": 2,
			}))
			telemetry := output["filter"].(map[string]any)
			if telemetry["degraded"] != true {
				t.Fatalf("filter failure must report degraded=true: %#v", telemetry)
			}
			direct, err := getForTest(t, registry, ctx, "default").retriever.Search(ctx, "morning drink", 2)
			if err != nil {
				t.Fatal(err)
			}
			got := searchMemoryNames(t, output)
			if len(got) != len(direct) {
				t.Fatalf("degraded shortlist = %v, RRF top-2 = %d hits", got, len(direct))
			}
			for i, result := range direct {
				if got[i] != result.Name {
					t.Fatalf("degraded shortlist = %v, RRF top-2 index %d = %q", got, i, result.Name)
				}
			}
		})
	}
}

func TestMemorySearchValidationAndClampRules(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{SearchFilterName: filter.BackendJev, SearchPool: 4, SearchPolicy: filter.DefaultPolicy()})
	seedMemories(t, registry, ctx, defaultCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	t.Run("pool above the honest scale is hard clamped", func(t *testing.T) {
		output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
			"query": "morning drink", "limit": 2, "candidate_pool": 600,
		}))
		if output["pool_size"] != float64(maxCandidatePool) {
			t.Fatalf("pool_size = %v, want the %d clamp", output["pool_size"], maxCandidatePool)
		}
	})
	t.Run("pool below limit is raised to limit", func(t *testing.T) {
		output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
			"query": "morning drink", "limit": 5, "candidate_pool": 2,
		}))
		if output["pool_size"] != float64(5) {
			t.Fatalf("pool_size = %v, want limit 5", output["pool_size"])
		}
	})
	t.Run("theta outside (0,1) is rejected", func(t *testing.T) {
		for _, theta := range []float64{0, 1, 1.5, -0.1} {
			result := callToolError(t, ctx, clientSession, "memory_search", map[string]any{
				"query": "morning drink", "theta": theta,
			})
			if result == nil {
				t.Fatalf("theta %v must be rejected", theta)
			}
		}
	})
	t.Run("unknown filter name is rejected", func(t *testing.T) {
		callToolError(t, ctx, clientSession, "memory_search", map[string]any{"query": "morning drink", "filter": "opaque"})
	})
	t.Run("theta override is reported in telemetry", func(t *testing.T) {
		output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
			"query": "morning drink", "limit": 2, "theta": 0.9,
		}))
		if got := output["filter"].(map[string]any)["theta"]; got != 0.9 {
			t.Fatalf("theta telemetry = %v, want the override 0.9", got)
		}
	})
}

func TestMemorySearchFilterRunsOnTopLimitWhenPoolEqualsLimit(t *testing.T) {
	ctx := context.Background()
	stub := &stubFilter{probsFor: func(cands []filter.Candidate) []float64 {
		probs := make([]float64, len(cands))
		for i := range probs {
			probs[i] = 0.1
		}
		if len(probs) > 0 {
			probs[0] = 0.9
		}
		return probs
	}}
	registry := testRegistry(t, ctx, RegistryConfig{
		SearchFilter: stub, SearchFilterName: filter.BackendJev, SearchPool: 3, SearchPolicy: filter.DefaultPolicy(),
	})
	seedMemories(t, registry, ctx, ledgerCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	// limit equals the configured pool: filtering still runs, on the top-limit
	// candidates, instead of being skipped as a no-op.
	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": ledgerQuery, "limit": 3, "candidate_pool": 3,
	}))
	if stub.calls != 1 || len(stub.lastCands) != 3 {
		t.Fatalf("filter calls = %d on %d candidates, want 1 on 3", stub.calls, len(stub.lastCands))
	}
	if got := searchMemoryNames(t, output); len(got) != 1 {
		t.Fatalf("shortlist = %v, want the single entry above theta", got)
	}
	if output["pool_size"] != float64(3) {
		t.Fatalf("pool_size = %v, want 3", output["pool_size"])
	}
}

func TestMemoryWriteUngatedResponseIsUnchanged(t *testing.T) {
	ctx := context.Background()
	gate := &stubGate{decision: filter.GateDecision{Route: filter.RouteSkip}}
	registry := testRegistry(t, ctx, RegistryConfig{WriteGate: gate})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	result := callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "preference", "content": "The user prefers dark mode.",
	})
	output := structuredMap(t, result)
	if len(output) != 2 || output["name"] != "preference" || output["written"] != true {
		t.Fatalf("ungated write output = %#v, want exactly {name, written}", output)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"gate"`) {
		t.Fatalf("ungated write response carries gate telemetry: %s", encoded)
	}
	if gate.calls != 0 {
		t.Fatalf("gate must not be consulted while disabled, calls = %d", gate.calls)
	}
}

func TestMemoryWriteGateSkipBlocksPersist(t *testing.T) {
	ctx := context.Background()
	gate := &stubGate{decision: filter.GateDecision{
		Route:   filter.RouteSkip,
		Reasons: map[string]float64{filter.ReasonDurable: 0.1, filter.ReasonPreference: 0.2},
	}}
	registry := testRegistry(t, ctx, RegistryConfig{WriteGate: gate, WriteGateEnabled: true})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "this-week-demo", "content": "This week I am rushing a demo.",
	}))
	if output["written"] != false || output["name"] != "this-week-demo" {
		t.Fatalf("skipped write output = %#v", output)
	}
	gateOutput := output["gate"].(map[string]any)
	if gateOutput["route"] != filter.RouteSkip {
		t.Fatalf("gate route = %v, want skip", gateOutput["route"])
	}
	if gate.calls != 1 {
		t.Fatalf("gate calls = %d, want 1", gate.calls)
	}

	// A skipped draft must not be persisted, and the tool must not require a
	// confirmation round-trip to say so.
	_, err := getForTest(t, registry, ctx, "default").entries.GetByName(ctx, "this-week-demo")
	if err == nil {
		t.Fatal("skipped draft was persisted")
	}
}

func TestMemoryWriteGateWriteProceedsAndReportsDecision(t *testing.T) {
	ctx := context.Background()
	gate := &stubGate{decision: filter.GateDecision{
		Route:   filter.RouteWrite,
		Reasons: map[string]float64{filter.ReasonDurable: 0.9},
		Meta:    filter.FilterMeta{Backend: filter.BackendJev, LatencyMs: 120, CostUSD: 0.0002},
	}}
	registry := testRegistry(t, ctx, RegistryConfig{WriteGate: gate, WriteGateEnabled: true})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "shell", "content": "The user's shell is zsh.",
	}))
	if output["written"] != true {
		t.Fatalf("gated write output = %#v", output)
	}
	gateOutput := output["gate"].(map[string]any)
	if gateOutput["route"] != filter.RouteWrite || gateOutput["backend"] != filter.BackendJev {
		t.Fatalf("gate telemetry = %#v", gateOutput)
	}
	if gateOutput["latency_ms"] != float64(120) {
		t.Fatalf("gate latency telemetry = %#v", gateOutput)
	}
	if _, err := getForTest(t, registry, ctx, "default").entries.GetByName(ctx, "shell"); err != nil {
		t.Fatalf("gate-approved write must persist: %v", err)
	}
}

func TestMemoryWriteGateFailsOpenOnError(t *testing.T) {
	ctx := context.Background()
	gate := &stubGate{err: errors.New("gate unavailable")}
	registry := testRegistry(t, ctx, RegistryConfig{WriteGate: gate, WriteGateEnabled: true})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "timezone", "content": "The user is in Asia/Shanghai.",
	}))
	if output["written"] != true {
		t.Fatalf("fail-open must not block the write: %#v", output)
	}
	gateOutput := output["gate"].(map[string]any)
	if gateOutput["degraded"] != true {
		t.Fatalf("unavailable gate must be reported as degraded: %#v", gateOutput)
	}
	if _, ok := gateOutput["route"]; ok {
		t.Fatalf("a gate that did not decide must not report a route: %#v", gateOutput)
	}
	if _, err := getForTest(t, registry, ctx, "default").entries.GetByName(ctx, "timezone"); err != nil {
		t.Fatalf("fail-open write must persist: %v", err)
	}
}

func TestMemoryWriteGateWithoutClientFailsOpen(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{WriteGateEnabled: true})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "editor", "content": "The user edits in vim.",
	}))
	if output["written"] != true {
		t.Fatalf("enabled gate without a client must fail open: %#v", output)
	}
	gateOutput := output["gate"].(map[string]any)
	if gateOutput["degraded"] != true || gateOutput["backend"] != filter.BackendNone {
		t.Fatalf("missing gate client telemetry = %#v", gateOutput)
	}
}

func TestMemoryWriteGateReceivesCanonicalRequest(t *testing.T) {
	ctx := context.Background()
	gate := &stubGate{decision: filter.GateDecision{Route: filter.RouteWrite}}
	registry := testRegistry(t, ctx, RegistryConfig{WriteGate: gate, WriteGateEnabled: true})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	output := structuredMap(t, callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name":           "hf-token",
		"content":        "HF_TOKEN=hf_abcdefghijklmnop",
		"user_turn":      "please save my HF token",
		"user_requested": true,
	}))
	if output["written"] != true {
		t.Fatalf("explicitly requested write must persist: %#v", output)
	}
	if gate.lastRequest.Draft != "HF_TOKEN=hf_abcdefghijklmnop" {
		t.Fatalf("gate draft = %q", gate.lastRequest.Draft)
	}
	if gate.lastRequest.UserTurn != "please save my HF token" {
		t.Fatalf("gate user turn = %q", gate.lastRequest.UserTurn)
	}
	if !gate.lastRequest.UserRequested {
		t.Fatal("explicit user request must reach the gate")
	}
}
