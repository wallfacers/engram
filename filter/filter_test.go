package filter

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func testCands(n int) []Candidate {
	cands := make([]Candidate, n)
	for i := range cands {
		cands[i] = Candidate{
			ID:    fmt.Sprintf("c%d", i),
			Name:  fmt.Sprintf("name-%d", i),
			Text:  fmt.Sprintf("text-%d", i),
			Score: float64(n - i),
		}
	}
	return cands
}

func TestDefaultPolicyValues(t *testing.T) {
	pol := DefaultPolicy()
	if pol.Theta != 0.5 || pol.RelaxTheta != 0.35 || pol.RelaxMax != 3 || pol.KShowMax != 12 {
		t.Fatalf("unexpected default policy: %+v", pol)
	}
	if pol.RelaxDisabled {
		t.Fatalf("relax must be enabled by default: %+v", pol)
	}
}

func TestSelectKeepsOnlyAboveThetaAndSortsByProbability(t *testing.T) {
	got := Select([]float64{0.9, 0.4, 0.6}, testCands(3), DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("expected [0 2], got %v", got)
	}
}

func TestSelectCapsAtKShowMax(t *testing.T) {
	probs := make([]float64, 20)
	for i := range probs {
		probs[i] = 0.9
	}
	got := Select(probs, testCands(20), DefaultPolicy())
	if len(got) != 12 {
		t.Fatalf("expected 12 kept, got %d (%v)", len(got), got)
	}
	for i, idx := range got {
		if idx != i {
			t.Fatalf("ties must break by index: got %v", got)
		}
	}
}

func TestSelectRelaxesWhenNothingAboveTheta(t *testing.T) {
	got := Select([]float64{0.4, 0.3, 0.2}, testCands(3), DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("expected relaxed [0], got %v", got)
	}
}

func TestSelectRelaxCapsAtRelaxMax(t *testing.T) {
	got := Select([]float64{0.45, 0.44, 0.43, 0.42, 0.41}, testCands(5), DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("expected relaxed top 3, got %v", got)
	}
}

func TestSelectHonestEmptyWhenBelowRelaxTheta(t *testing.T) {
	got := Select([]float64{0.2, 0.1}, testCands(2), DefaultPolicy())
	if len(got) != 0 {
		t.Fatalf("expected honest empty, got %v", got)
	}
}

func TestSelectRelaxDisabledSkipsRelaxStage(t *testing.T) {
	pol := DefaultPolicy()
	pol.RelaxDisabled = true
	got := Select([]float64{0.4, 0.3}, testCands(2), pol)
	if len(got) != 0 {
		t.Fatalf("expected empty with relax disabled, got %v", got)
	}
}

func TestSelectPinnedWithTriggerMatchInjectedFirst(t *testing.T) {
	cands := testCands(3)
	cands[2].Pinned = true
	cands[2].Trigger = "pnpm"
	got := SelectForQuery("How do I install pnpm?", []float64{0.9, 0.8, 0.1}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Fatalf("expected pinned first then p-desc, got %v", got)
	}
}

func TestSelectTriggerMatchIsCaseInsensitive(t *testing.T) {
	cands := testCands(1)
	cands[0].Pinned = true
	cands[0].Trigger = "PNPM"
	got := SelectForQuery("how to install pnpm on debian", []float64{0.1}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("expected case-insensitive trigger match, got %v", got)
	}
}

func TestSelectPinnedWithoutTriggerMatchNotInjected(t *testing.T) {
	cands := testCands(2)
	cands[0].Pinned = true
	cands[0].Trigger = "docker"
	// The pinned entry does not match the query, so it is an ordinary
	// candidate: below theta it is dropped, not injected.
	got := SelectForQuery("how do I install pnpm?", []float64{0.1, 0.8}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("expected only the passing candidate, got %v", got)
	}
}

func TestSelectPinnedWithoutTriggerMatchStillKeptAboveTheta(t *testing.T) {
	cands := testCands(2)
	cands[0].Pinned = true
	cands[0].Trigger = "docker"
	got := SelectForQuery("how do I install pnpm?", []float64{0.9, 0.8}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("expected both entries in probability order, got %v", got)
	}
}

func TestSelectPinnedWithoutTriggerDoesNotBypass(t *testing.T) {
	cands := testCands(2)
	cands[0].Pinned = true // empty Trigger cannot be "found in" the query
	got := SelectForQuery("anything at all", []float64{0.9, 0.8}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("expected plain probability order, got %v", got)
	}
}

func TestSelectPinnedCountsTowardKShowMax(t *testing.T) {
	pol := DefaultPolicy()
	pol.KShowMax = 2
	cands := testCands(5)
	cands[0].Pinned = true
	cands[0].Trigger = "pnpm"
	probs := []float64{0.1, 0.95, 0.94, 0.93, 0.92}
	got := SelectForQuery("pnpm", probs, cands, pol)
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("expected pinned plus one kept under the hard cap, got %v", got)
	}
}

func TestSelectPinnedOverridesKShowMaxWhenPathological(t *testing.T) {
	pol := DefaultPolicy()
	pol.KShowMax = 2
	cands := testCands(3)
	for i := range cands {
		cands[i].Pinned = true
		cands[i].Trigger = "pnpm"
	}
	got := SelectForQuery("pnpm", []float64{0.1, 0.2, 0.3}, cands, pol)
	if len(got) != 3 {
		t.Fatalf("pathological pinned entries must all show, got %v", got)
	}
}

func TestSelectProbsShorterThanCandsDoesNotPanic(t *testing.T) {
	cands := testCands(3)
	cands[0].Pinned = true
	cands[0].Trigger = "pnpm"
	got := SelectForQuery("pnpm", []float64{0.9}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("expected only the pinned entry, got %v", got)
	}
}

func TestSelectEmptyInputs(t *testing.T) {
	if got := Select(nil, nil, DefaultPolicy()); len(got) != 0 {
		t.Fatalf("expected empty selection, got %v", got)
	}
	if got := SelectForQuery("q", nil, testCands(2), DefaultPolicy()); len(got) != 0 {
		t.Fatalf("expected empty selection for no probabilities, got %v", got)
	}
}

func TestZeroPolicyBehavesLikeDefault(t *testing.T) {
	zero := Select([]float64{0.9, 0.4}, testCands(2), Policy{})
	def := Select([]float64{0.9, 0.4}, testCands(2), DefaultPolicy())
	if !reflect.DeepEqual(zero, def) {
		t.Fatalf("zero-value policy must normalize to defaults: %v vs %v", zero, def)
	}
}

func TestSelectWithoutQueryNeverBypassesPinned(t *testing.T) {
	cands := testCands(2)
	cands[1].Pinned = true
	cands[1].Trigger = "pnpm"
	got := Select([]float64{0.9, 0.1}, cands, DefaultPolicy())
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("expected no trigger bypass without a query, got %v", got)
	}
}

func TestNoneFilterIsPassthrough(t *testing.T) {
	var f RelevanceFilter = None{}
	cands := testCands(3)
	probs, meta, err := f.Filter(context.Background(), "q", cands)
	if err != nil {
		t.Fatalf("none filter must not fail: %v", err)
	}
	if len(probs) != 3 {
		t.Fatalf("expected aligned probabilities, got %v", probs)
	}
	for i, p := range probs {
		if p != 1 {
			t.Fatalf("none probabilities must be 1.0 (threshold no-op), got %v at %d", p, i)
		}
	}
	if meta.Backend != BackendNone || meta.Kept != 3 || meta.Dropped != 0 || meta.Degraded {
		t.Fatalf("unexpected none meta: %+v", meta)
	}
	// The composition layer truncates the none path to `show`; Select alone must
	// still keep everything the cap allows.
	if got := SelectForQuery("q", probs, cands, DefaultPolicy()); len(got) != 3 {
		t.Fatalf("expected all candidates kept, got %v", got)
	}
}

func TestNoneFilterEmptyInput(t *testing.T) {
	var f RelevanceFilter = None{}
	probs, meta, err := f.Filter(context.Background(), "q", nil)
	if err != nil || len(probs) != 0 {
		t.Fatalf("expected empty passthrough, got %v %v %v", probs, meta, err)
	}
	if meta.Backend != BackendNone {
		t.Fatalf("expected none backend, got %+v", meta)
	}
}

func TestNoneDoesNotInventUsageTokens(t *testing.T) {
	probs, meta, err := None{}.Filter(context.Background(), "q", []Candidate{{ID: "a"}})
	if err != nil || len(probs) != 1 {
		t.Fatalf("none filter: probs=%v err=%v", probs, err)
	}
	if meta.InputTokens != 0 || meta.OutputTokens != 0 {
		t.Fatalf("none must not report usage, got %d/%d", meta.InputTokens, meta.OutputTokens)
	}
	if meta.CostUSD != 0 {
		t.Fatalf("none must not report cost, got %v", meta.CostUSD)
	}
}
