package main

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
)

// --- T004: GapItem schema + <DEEPEN_META> parse ---

func TestParseDeepenMetaNoBlock(t *testing.T) {
	gaps, found, err := parseDeepenMeta("the final answer")
	if found {
		t.Fatalf("no block should not be found, got found=true gaps=%v", gaps)
	}
	if err != nil {
		t.Fatalf("no block must not error, got %v", err)
	}
}

func TestParseDeepenMetaMissingClose(t *testing.T) {
	_, found, err := parseDeepenMeta("answer\n<DEEPEN_META>{\"gaps\":[]}")
	if !found {
		t.Fatalf("open block without close must be found=true")
	}
	if err == nil {
		t.Fatalf("missing closing tag must error")
	}
}

func TestParseDeepenMetaMalformedJSON(t *testing.T) {
	_, found, err := parseDeepenMeta("a\n<DEEPEN_META>{not json</DEEPEN_META>")
	if !found {
		t.Fatalf("malformed JSON block must be found=true")
	}
	if err == nil {
		t.Fatalf("malformed JSON must error")
	}
}

func TestParseDeepenMetaValid(t *testing.T) {
	out := "final answer\n<DEEPEN_META>{\"gaps\":[{\"category\":\"bridge_entity\",\"target\":\"Alice\",\"slot\":\"spouse\"}]}</DEEPEN_META>"
	gaps, found, err := parseDeepenMeta(out)
	if !found {
		t.Fatal("valid block must be found")
	}
	if err != nil {
		t.Fatalf("valid block must not error: %v", err)
	}
	want := []deepenGapItem{{Category: "bridge_entity", Target: "Alice", Slot: "spouse"}}
	if !reflect.DeepEqual(gaps, want) {
		t.Fatalf("gaps=%+v want %+v", gaps, want)
	}
}

func TestValidateDeepenGapsRejectsBadCategory(t *testing.T) {
	if err := validateDeepenGaps([]deepenGapItem{{Category: "not_a_category"}}); err == nil {
		t.Fatal("unknown category must be rejected")
	}
}

func TestValidateDeepenGapsRejectsTooMany(t *testing.T) {
	gaps := make([]deepenGapItem, deepenMaxGapItems+1)
	for i := range gaps {
		gaps[i] = deepenGapItem{Category: "other"}
	}
	if err := validateDeepenGaps(gaps); err == nil {
		t.Fatalf("more than %d gaps must be rejected", deepenMaxGapItems)
	}
}

func TestValidateDeepenGapsRejectsOverlongTarget(t *testing.T) {
	gap := deepenGapItem{Category: "other", Target: strings.Repeat("x", deepenMaxTarget+1)}
	if err := validateDeepenGaps([]deepenGapItem{gap}); err == nil {
		t.Fatal("overlong target must be rejected")
	}
}

func TestValidateDeepenGapsAcceptsEmptyBlock(t *testing.T) {
	if err := validateDeepenGaps(nil); err != nil {
		t.Fatalf("empty gap block must be legal: %v", err)
	}
}

// --- T005: gapQueryFor deterministic mapping ---

func TestGapQueryForMappingTable(t *testing.T) {
	cases := []struct {
		name     string
		gaps     []deepenGapItem
		question string
		want     string
	}{
		{"target+slot wins", []deepenGapItem{{Category: "bridge_entity", Target: "Alice", Slot: "spouse", Description: "desc"}}, "q", "Alice spouse"},
		{"description fallback", []deepenGapItem{{Category: "attribute", Description: "  the desc  "}}, "q", "the desc"},
		{"target fallback", []deepenGapItem{{Category: "attribute", Target: "Alice"}}, "q", "Alice"},
		{"question fallback", []deepenGapItem{{Category: "other"}}, "  q  ", "q"},
		{"blank first gap falls to question, later gaps not consulted", []deepenGapItem{
			{Category: "other"},
			{Category: "bridge_entity", Target: "Bob", Slot: "employer"},
		}, "q", "q"},
		{"first gap wins even when later has stronger fields", []deepenGapItem{
			{Category: "attribute", Description: "first"},
			{Category: "bridge_entity", Target: "Bob", Slot: "employer"},
		}, "q", "first"},
		{"empty gap list falls to question", nil, "  q  ", "q"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gapQueryFor(tc.gaps, tc.question)
			if got != tc.want {
				t.Fatalf("gapQueryFor=%q want %q", got, tc.want)
			}
			// Determinism: same input twice.
			if again := gapQueryFor(tc.gaps, tc.question); again != got {
				t.Fatalf("gapQueryFor not deterministic: %q vs %q", again, got)
			}
		})
	}
}

// --- T006: appendDedup append-only union ---

func testResult(id string) memory.Result {
	return memory.Result{ID: id, Content: "content-" + id}
}

func TestAppendDedupKeepsRound0OrderAndAppends(t *testing.T) {
	round0 := []memory.Result{testResult("a"), testResult("b")}
	extra := []memory.Result{testResult("c"), testResult("a"), testResult("d")}
	union, added := appendDedup(round0, extra)
	if added != 2 {
		t.Fatalf("added=%d want 2", added)
	}
	want := []memory.Result{testResult("a"), testResult("b"), testResult("c"), testResult("d")}
	if !reflect.DeepEqual(union, want) {
		t.Fatalf("union=%+v want %+v", union, want)
	}
	// round-0 must be untouched (order preserved, no mutation of inputs).
	if !reflect.DeepEqual(round0, []memory.Result{testResult("a"), testResult("b")}) {
		t.Fatalf("round0 mutated: %+v", round0)
	}
}

func TestAppendDedupAllDuplicates(t *testing.T) {
	round0 := []memory.Result{testResult("a")}
	extra := []memory.Result{testResult("a")}
	union, added := appendDedup(round0, extra)
	if added != 0 {
		t.Fatalf("added=%d want 0", added)
	}
	if len(union) != 1 || union[0].ID != "a" {
		t.Fatalf("union=%+v want [a]", union)
	}
}

// --- T007: AUC (rank-based, known small sample) ---

func TestDeepenAUCKnownValues(t *testing.T) {
	cases := []struct {
		name string
		pos  []float64
		neg  []float64
		want float64
	}{
		{"perfect separation", []float64{1.0, 2.0}, []float64{0.5, 0.8}, 1.0},
		{"rank 2/4 and 4/4", []float64{0.6, 0.9}, []float64{0.5, 0.8}, 0.75},
		{"random", []float64{1.0, 0.0}, []float64{1.0, 0.0}, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deepenAUC(tc.pos, tc.neg)
			if err != nil {
				t.Fatalf("deepenAUC error: %v", err)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("AUC=%v want %v", got, tc.want)
			}
		})
	}
}

func TestDeepenAUCBootstrapDeterministic(t *testing.T) {
	pos := []float64{0.8, 0.9, 0.7, 1.0, 0.6}
	neg := []float64{0.2, 0.3, 0.1, 0.4, 0.0}
	lo1, hi1, err1 := deepenAUCBootstrap(pos, neg, 42, 200)
	lo2, hi2, err2 := deepenAUCBootstrap(pos, neg, 42, 200)
	if err1 != nil || err2 != nil {
		t.Fatalf("bootstrap error: %v / %v", err1, err2)
	}
	if lo1 != lo2 || hi1 != hi2 {
		t.Fatalf("bootstrap not deterministic with fixed seed: (%v,%v) vs (%v,%v)", lo1, hi1, lo2, hi2)
	}
	if lo1 < 0 || hi1 > 1 || lo1 > hi1 {
		t.Fatalf("bootstrap CI out of range: (%v,%v)", lo1, hi1)
	}
}

func TestDeepenROCThresholdSeparatesPerfectly(t *testing.T) {
	pos := []float64{1.0, 2.0, 3.0}
	neg := []float64{0.1, 0.2, 0.3}
	thr, err := deepenROCThreshold(pos, neg)
	if err != nil {
		t.Fatalf("threshold error: %v", err)
	}
	// Any cutoff in (0.3, 1.0] yields J=1; the max-J set includes the smallest
	// such score, so the chosen threshold must fully separate.
	tps, fps := 0, 0
	for _, s := range pos {
		if s >= thr {
			tps++
		}
	}
	for _, s := range neg {
		if s >= thr {
			fps++
		}
	}
	if tps != len(pos) || fps != 0 {
		t.Fatalf("threshold %.3f not separating: tps=%d/%d fps=%d/%d", thr, tps, len(pos), fps, len(neg))
	}
}

// --- T007: textual hesitation lexicon ---

func TestTextualHesitation(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"I don't know", true},
		{"i do not know this", true},
		{"I'm not sure about it", true},
		{"not enough information provided", true},
		{"The answer is 42.", false},
		{"", true},
		{"   ", true},
	}
	for _, tc := range cases {
		if got := textualHesitation(tc.text); got != tc.want {
			t.Fatalf("textualHesitation(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}

// --- T003: artifact validate / gate verdict ---

func TestDeepenDecisionValidate(t *testing.T) {
	good := deepenDecision{DecisionID: "id", OutcomeKind: deepenOutcomeNone}
	if err := good.validate(); err != nil {
		t.Fatalf("valid decision rejected: %v", err)
	}
	if err := (&deepenDecision{DecisionID: "", OutcomeKind: deepenOutcomeNone}).validate(); err == nil {
		t.Fatal("missing decision_id must be rejected")
	}
	if err := (&deepenDecision{DecisionID: "id", OutcomeKind: "bogus"}).validate(); err == nil {
		t.Fatal("invalid outcome_kind must be rejected")
	}
}

func TestDeepenPilotGateVerdict(t *testing.T) {
	cases := []struct {
		name      string
		auc       float64
		parity    deepenChannelParity
		wantVerd  string
	}{
		{"go above gate", 0.80, deepenChannelParity{N: 10, Flips: 1, FlipRate: 0.10}, "GO"},
		{"no-go below gate", 0.60, deepenChannelParity{N: 10, Flips: 1, FlipRate: 0.10}, "NO-GO"},
		{"no-go flip too high", 0.80, deepenChannelParity{N: 10, Flips: 3, FlipRate: 0.30}, "NO-GO"},
		{"no-go no parity", 0.80, deepenChannelParity{N: 0, Flips: 0, FlipRate: 0.0}, "NO-GO"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, _ := deepenPilotGateVerdict(tc.auc, tc.parity)
			if verdict != tc.wantVerd {
				t.Fatalf("verdict=%q want %q", verdict, tc.wantVerd)
			}
		})
	}
}

func TestDeepenManifestDigestChangesWhenFieldChanges(t *testing.T) {
	base := deepenRunManifest{
		Schema: deepenSchemaVersion, RunID: "r", Stage: "pilot", QuestionCount: 2,
		Arm: "signal-pilot", Threshold: 0.7, FeatureName: "final_p10_logprob",
		ContractDigest: "c", DatasetDigest: "d", DeepenK: 30, MaxGaps: 3,
	}
	d1, err := deepenManifestDigest(&base)
	if err != nil {
		t.Fatal(err)
	}
	base.Threshold = 0.8
	d2, err := deepenManifestDigest(&base)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("manifest digest must change when threshold changes")
	}
}
