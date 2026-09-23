package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/filter"
)

// stubFilter scripts probabilities so selection, telemetry and degradation are
// observable without a network.
type stubFilter struct {
	probs []float64
	err   error
}

func (s *stubFilter) Filter(_ context.Context, _ string, cands []filter.Candidate) ([]float64, filter.FilterMeta, error) {
	if s.err != nil {
		return nil, filter.FilterMeta{Backend: filter.BackendJev, Degraded: true}, s.err
	}
	probs := s.probs
	if probs == nil {
		probs = make([]float64, len(cands))
		for i := range probs {
			probs[i] = 1
		}
	}
	return probs, filter.FilterMeta{Backend: filter.BackendJev}, nil
}

func cliPool() filterInput {
	return filterInput{
		Query: "naming",
		Candidates: []filter.Candidate{
			{ID: "m1", Name: "alpha", Text: "alpha entry", Trigger: "naming", Score: 1.0},
			{ID: "m2", Name: "beta", Text: "beta entry", Trigger: "other", Score: 0.9},
			{ID: "m3", Name: "gamma", Text: "gamma entry", Trigger: "other", Score: 0.8},
			{ID: "m4", Name: "delta", Text: "delta entry", Trigger: "naming", Pinned: true, Score: 0.1},
		},
	}
}

func emptyEnv(string) string { return "" }

func TestSelectShortlistMatchesDirectFilterPackageSelection(t *testing.T) {
	// Parity: the B path must produce exactly what the engine's own selection
	// produces for the same pool, so both integration paths are interchangeable.
	// The expectation is written out literally rather than recomputed with the same
	// call, so a regression in SelectForQuery cannot hide behind it.
	pool := cliPool()
	shortlist, err := selectShortlist(context.Background(), nil, pool, filter.DefaultPolicy(), 8, false)
	if err != nil {
		t.Fatal(err)
	}
	// m4 is pinned with a trigger that matches the query, so it leads; the rest are
	// uniform-probability entries in input order.
	want := []int{3, 0, 1, 2}
	if len(shortlist.Indices) != len(want) {
		t.Fatalf("cli indices = %v, want %v", shortlist.Indices, want)
	}
	for i, idx := range want {
		if shortlist.Indices[i] != idx {
			t.Fatalf("cli indices = %v, want %v", shortlist.Indices, want)
		}
	}
	if shortlist.Meta.Kept != 4 || shortlist.Meta.Dropped != 0 {
		t.Fatalf("telemetry = %#v", shortlist.Meta)
	}
}

func TestSelectShortlistDegradesHonestlyWhenJevUnconfigured(t *testing.T) {
	pool := cliPool()
	shortlist, err := selectShortlist(context.Background(), nil, pool, filter.DefaultPolicy(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if !shortlist.Meta.Degraded {
		t.Fatal("an unconfigured filter must report degraded=true")
	}
	if shortlist.Meta.Backend != filter.BackendNone {
		t.Fatalf("backend = %q, want none without a client", shortlist.Meta.Backend)
	}
	if len(shortlist.Indices) != 2 {
		t.Fatalf("degraded shortlist = %v, want the first 2 pool entries", shortlist.Indices)
	}
	if shortlist.Meta.Kept != 2 || shortlist.Meta.Dropped != len(pool.Candidates)-2 {
		t.Fatalf("degraded telemetry = %#v", shortlist.Meta)
	}
	if shortlist.Meta.Theta != filter.DefaultPolicy().Theta {
		t.Fatalf("theta telemetry = %v", shortlist.Meta.Theta)
	}
}

func TestSelectShortlistHonestEmptyAndRelaxKillSwitch(t *testing.T) {
	pool := cliPool()
	probs := make([]float64, len(pool.Candidates))
	for i := range probs {
		probs[i] = 0.4 // clears RelaxTheta (0.35), not Theta (0.5)
	}
	relaxed := filter.DefaultPolicy()
	strict := filter.DefaultPolicy()
	strict.RelaxDisabled = true

	withRelax, err := selectShortlist(context.Background(), &stubFilter{probs: probs}, pool, relaxed, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(withRelax.Indices) <= 1 {
		t.Fatalf("the relax stage must rescue entries above RelaxTheta, got %v", withRelax.Indices)
	}
	without, err := selectShortlist(context.Background(), &stubFilter{probs: probs}, pool, strict, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	// With the relax stage disabled nothing clears theta; the only survivor is
	// the pinned entry whose trigger matches the query.
	if len(without.Indices) != 1 || without.Indices[0] != 3 {
		t.Fatalf("RelaxDisabled must keep only the pinned bypass, got %v", without.Indices)
	}

	// A pool with no pinned trigger match and nothing above theta is an honest
	// empty shortlist, which must still serialize as [] rather than null.
	emptyPool := filterInput{Query: "naming", Candidates: []filter.Candidate{{ID: "m1", Name: "alpha", Text: "alpha", Trigger: "other"}}}
	empty, err := selectShortlist(context.Background(), &stubFilter{probs: []float64{0.4}}, emptyPool, strict, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Indices) != 0 || empty.Indices == nil {
		t.Fatalf("honest empty shortlist must be an empty slice, got %#v", empty.Indices)
	}
	if empty.Meta.Degraded {
		t.Fatalf("a configured filter that keeps nothing must not report degraded: %#v", empty.Meta)
	}
}

func TestSelectShortlistReportsFilterFailureAsDegraded(t *testing.T) {
	pool := cliPool()
	tests := []struct {
		name string
		flt  filter.RelevanceFilter
	}{
		{name: "filter error", flt: &stubFilter{err: errors.New("jev down")}},
		{name: "probability count mismatch", flt: &stubFilter{probs: []float64{0.9}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shortlist, err := selectShortlist(context.Background(), test.flt, pool, filter.DefaultPolicy(), 3, true)
			if err != nil {
				t.Fatal(err)
			}
			if !shortlist.Meta.Degraded {
				t.Fatal("a failed filter must degrade, never return a partial shortlist")
			}
			if len(shortlist.Indices) != 3 {
				t.Fatalf("degraded shortlist = %v, want the first 3 pool entries", shortlist.Indices)
			}
			for i, prob := range shortlist.Probs {
				if prob != 1 {
					t.Fatalf("degraded probabilities must be uniform, got %v at %d", prob, i)
				}
			}
		})
	}
}

func TestSelectShortlistKeepsOnlyEntriesAboveTheta(t *testing.T) {
	pool := cliPool()
	shortlist, err := selectShortlist(context.Background(), &stubFilter{probs: []float64{0.9, 0.1, 0.95, 0.2}}, pool, filter.DefaultPolicy(), 8, true)
	if err != nil {
		t.Fatal(err)
	}
	// Literal expectation: the pinned m4 leads, then the two above-theta entries by
	// probability (m3 0.95 before m1 0.9); m2 (0.1) is dropped.
	want := []int{3, 2, 0}
	if len(shortlist.Indices) != len(want) {
		t.Fatalf("kept = %v, want %v", shortlist.Indices, want)
	}
	for i, idx := range want {
		if shortlist.Indices[i] != idx {
			t.Fatalf("kept = %v, want %v", shortlist.Indices, want)
		}
	}
	if len(shortlist.Probs) != len(shortlist.Indices) {
		t.Fatalf("probs must align with indices: %v vs %v", shortlist.Probs, shortlist.Indices)
	}
	wantProbs := []float64{0.2, 0.95, 0.9}
	for i, p := range wantProbs {
		if shortlist.Probs[i] != p {
			t.Fatalf("probs = %v, want %v", shortlist.Probs, wantProbs)
		}
	}
	if shortlist.Meta.Backend != filter.BackendJev || shortlist.Meta.Degraded {
		t.Fatalf("configured filter telemetry = %#v", shortlist.Meta)
	}
}

// TestSelectShortlistKeepsPathologicalPinnedSetBeyondShow pins the B path's half
// of relevance-filter.md §3.4: a pinned set larger than the display budget is
// shown in full (the user asked for those entries), so --show truncates the
// non-pinned remainder only.
func TestSelectShortlistKeepsPathologicalPinnedSetBeyondShow(t *testing.T) {
	pool := filterInput{
		Query: "naming",
		Candidates: []filter.Candidate{
			{ID: "p1", Name: "pinned-1", Trigger: "naming", Pinned: true, Score: 1.0},
			{ID: "p2", Name: "pinned-2", Trigger: "naming", Pinned: true, Score: 0.9},
			{ID: "p3", Name: "pinned-3", Trigger: "naming", Pinned: true, Score: 0.8},
			{ID: "f1", Name: "free-1", Text: "free entry", Score: 0.7},
			{ID: "f2", Name: "free-2", Text: "free entry", Score: 0.6},
		},
	}
	shortlist, err := selectShortlist(context.Background(), nil, pool, filter.DefaultPolicy(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2}
	if len(shortlist.Indices) != len(want) {
		t.Fatalf("indices = %v, want the three pinned entries %v", shortlist.Indices, want)
	}
	for i, idx := range want {
		if shortlist.Indices[i] != idx {
			t.Fatalf("indices = %v, want %v", shortlist.Indices, want)
		}
	}
	if len(shortlist.Notes) == 0 || !strings.Contains(shortlist.Notes[0], "pinned override") {
		t.Fatalf("the override must be reported to the caller: %v", shortlist.Notes)
	}
}

func TestRunPipelineProducesShortlistJSON(t *testing.T) {
	pool := cliPool()
	raw, err := json.Marshal(pool)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--show", "2", "--probs"}, bytes.NewReader(raw), &stdout, &stderr, emptyEnv); err != nil {
		t.Fatalf("run: %v (stderr %s)", err, stderr.String())
	}
	var output filterOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode CLI output %q: %v", stdout.String(), err)
	}
	if output.Query != pool.Query || len(output.Indices) != 2 {
		t.Fatalf("CLI output = %#v", output)
	}
	if !output.Meta.Degraded || len(output.Probs) != 2 {
		t.Fatalf("CLI output telemetry = %#v", output)
	}
}

func TestRunRejectsInvalidInput(t *testing.T) {
	valid, err := json.Marshal(cliPool())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		env  map[string]string
		in   string
	}{
		{name: "theta out of range", args: []string{"--theta", "1.5"}, in: string(valid)},
		{name: "theta zero", args: []string{"--theta", "0"}, in: string(valid)},
		{name: "show below one", args: []string{"--show", "0"}, in: string(valid)},
		{name: "malformed pool", in: "{not json"},
		{name: "invalid env theta", env: map[string]string{"ENGRAM_JEV_THETA": "nope"}, in: string(valid)},
		{name: "invalid env relax", env: map[string]string{"ENGRAM_JEV_RELAX": "maybe"}, in: string(valid)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(key string) string { return test.env[key] }
			var stdout, stderr bytes.Buffer
			if err := run(test.args, strings.NewReader(test.in), &stdout, &stderr, getenv); err == nil {
				t.Fatalf("run(%v) unexpectedly succeeded: %s", test.args, stdout.String())
			}
		})
	}
}

func TestRunDoesNotCallUnconfiguredJev(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// Base URL and model without a key is the documented "Jev disabled" state.
	env := map[string]string{"ENGRAM_JEV_BASE_URL": server.URL, "ENGRAM_JEV_MODEL": "jev-2026-09-r3"}
	raw, err := json.Marshal(cliPool())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run(nil, bytes.NewReader(raw), &stdout, &stderr, func(key string) string { return env[key] }); err != nil {
		t.Fatalf("run: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("unconfigured Jev issued %d requests, want 0", requests.Load())
	}
	var output filterOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Meta.Degraded {
		t.Fatalf("unconfigured Jev must degrade: %#v", output.Meta)
	}
}
