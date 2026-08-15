package main

// 045 packing-layer unit tests: determinism, budget hard bound, singleton
// fallback, tie-breaks, and the frozen normalizations. These encode the
// contract in specs/045-submodular-packing/contracts/cli-flags.md — change
// behavior here only with a spec contract bump.

import (
	"math"
	"reflect"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestPackEstimateTokensTable(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 1},
		{"abcdefgh", 2},
		{"你好世界测试", 1}, // 6 runes → 1 (6/4=1)
		{"你好世界测试你好世界测试你好世界测试", 4}, // 18 runes → 4 (18/4=4)
	}
	for _, c := range cases {
		if got := packEstimateTokens(c.in); got != c.want {
			t.Errorf("packEstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPackTokenizeWords(t *testing.T) {
	got := packTokenizeWords("  The-User, asked: WHAT?!  ")
	want := []string{"the", "user", "asked", "what"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokenize = %v, want %v", got, want)
	}
}

func TestPackShingles(t *testing.T) {
	words := []string{"a", "b", "c", "d", "e", "f"}
	sh := packShingles(words, 5)
	if len(sh) != 2 { // [a..e], [b..f]
		t.Errorf("shingle count = %d, want 2", len(sh))
	}
	// Deterministic across calls.
	if !reflect.DeepEqual(packShingles(words, 5), sh) {
		t.Error("shingles not deterministic")
	}
	// Order-insensitive construction: same set regardless of build order of
	// the input string is NOT required (shingles are positional), but the
	// empty and short inputs must be empty sets.
	if len(packShingles([]string{"a", "b"}, 5)) != 0 {
		t.Error("short input must produce no shingles")
	}
}

func packTestHit(name, content string, score float64) memory.Result {
	return memory.Result{Name: name, Content: content, Score: score}
}

func TestBuildPackCandidatesNorm(t *testing.T) {
	hits := []memory.Result{
		packTestHit("chunk-c0-001", "alpha beta", 10),
		packTestHit("fact-2", "gamma", 6),
		packTestHit("chunk-c0-003", "delta", 2),
	}
	cands := buildPackCandidates(hits, packQueryWordSet("alpha gamma"))
	if len(cands) != 3 {
		t.Fatalf("candidates = %d", len(cands))
	}
	// min-max: 10→1.0, 6→0.5, 2→0.0
	want := []float64{1.0, 0.5, 0.0}
	for i, w := range want {
		if math.Abs(cands[i].NormScore-w) > 1e-12 {
			t.Errorf("cand[%d].NormScore = %v, want %v", i, cands[i].NormScore, w)
		}
	}
	if cands[0].Kind != "chunk" || cands[1].Kind != "fact" {
		t.Errorf("kinds = %v/%v", cands[0].Kind, cands[1].Kind)
	}
	// Cover terms: query {alpha, gamma}; cand0 covers {alpha}, cand1 {gamma}.
	if _, ok := cands[0].CoverTerms["alpha"]; !ok {
		t.Error("cand0 should cover 'alpha'")
	}
	if _, ok := cands[1].CoverTerms["gamma"]; !ok {
		t.Error("cand1 should cover 'gamma'")
	}
	// Degenerate pool (all equal scores) → all 1.0.
	deg := buildPackCandidates(hits[:1], nil)
	if deg[0].NormScore != 1.0 {
		t.Errorf("degenerate NormScore = %v, want 1.0", deg[0].NormScore)
	}
}

func TestPackJaccard(t *testing.T) {
	a := map[uint64]struct{}{1: {}, 2: {}, 3: {}}
	b := map[uint64]struct{}{2: {}, 3: {}, 4: {}}
	// inter 2, union 4 → 0.5
	if got := packJaccard(a, b); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("jaccard = %v, want 0.5", got)
	}
	if got := packJaccard(a, nil); got != 0 {
		t.Errorf("jaccard with empty = %v, want 0", got)
	}
}

func packSelEquals(a, b packSelection) bool {
	if a.EstTokensUsed != b.EstTokensUsed || a.SingletonFallback != b.SingletonFallback {
		return false
	}
	if !reflect.DeepEqual(a.GreedyOrder, b.GreedyOrder) {
		return false
	}
	if !reflect.DeepEqual(a.Dropped, b.Dropped) {
		return false
	}
	if len(a.Selected) != len(b.Selected) {
		return false
	}
	for i := range a.Selected {
		if a.Selected[i].Name != b.Selected[i].Name {
			return false
		}
	}
	return true
}

func TestPackSelectDeterministic(t *testing.T) {
	hits := []memory.Result{
		packTestHit("e1", "the quick brown fox jumps over the lazy dog again and again", 9),
		packTestHit("e2", "the quick brown fox jumps over the lazy dog again and again", 8),
		packTestHit("e3", "a completely different story about Paris and rivers", 7),
		packTestHit("e4", "unrelated content entirely", 6),
		packTestHit("e5", "more unrelated words here totally new", 5),
	}
	w := defaultPackWeights()
	a := packSelect(hits, "quick brown fox Paris", 64, w)
	b := packSelect(hits, "quick brown fox Paris", 64, w)
	if !packSelEquals(a, b) {
		t.Errorf("packSelect not deterministic:\n%+v\nvs\n%+v", a, b)
	}
}

func TestPackSelectBudgetHardBound(t *testing.T) {
	hits := []memory.Result{
		packTestHit("e1", "one two three four five six seven eight nine ten eleven twelve thirteen fourteen", 9),
		packTestHit("e2", "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu", 8),
		packTestHit("e3", "short", 7),
		packTestHit("e4", "tiny", 6),
	}
	sel := packSelect(hits, "query words here", 20, defaultPackWeights())
	if !sel.SingletonFallback && sel.EstTokensUsed > 20 {
		t.Errorf("EstTokensUsed = %d exceeds budget 20 without singleton flag", sel.EstTokensUsed)
	}
}

func TestPackSelectSingletonFallback(t *testing.T) {
	// Budget smaller than every candidate → singleton keeps highest NormScore.
	hits := []memory.Result{
		packTestHit("big1", "a very long content that costs many many tokens indeed yes", 5),
		packTestHit("big2", "another very long content that costs many tokens too", 9),
	}
	sel := packSelect(hits, "q", 1, defaultPackWeights())
	if !sel.SingletonFallback {
		t.Fatalf("expected singleton fallback, got %+v", sel)
	}
	if len(sel.Selected) != 1 || sel.Selected[0].Name != "big2" {
		t.Errorf("singleton pick = %v, want big2 (highest NormScore)", sel.Selected)
	}
	if sel.EstTokensUsed > 0 && sel.EstTokensUsed <= 1 {
		t.Errorf("singleton should be allowed to exceed budget, used=%d", sel.EstTokensUsed)
	}
}

func TestPackSelectEmptyPool(t *testing.T) {
	sel := packSelect(nil, "q", 100, defaultPackWeights())
	if len(sel.Selected) != 0 || sel.SingletonFallback {
		t.Errorf("empty pool must give empty selection (caller falls back), got %+v", sel)
	}
}

func TestPackSelectPrefersHighRelevance(t *testing.T) {
	// With relevance weight dominant, the top-scored entry must be selected
	// even under diversity pressure from near-duplicates.
	hits := []memory.Result{
		packTestHit("top", "unique relevant content about the answer itself directly", 10),
		packTestHit("dup1", "unique relevant content about the answer itself directly too", 9),
		packTestHit("dup2", "unique relevant content about the answer itself directly also", 8),
	}
	sel := packSelect(hits, "answer relevant", 400, defaultPackWeights())
	found := false
	for _, s := range sel.Selected {
		if s.Name == "top" {
			found = true
		}
	}
	if !found {
		t.Errorf("relevance-dominant weights must keep the top entry: %+v", sel.GreedyOrder)
	}
}

func TestPackSelectTieBreakLowestID(t *testing.T) {
	// Identical content and score → every gain equal; the lower ID must win.
	hits := []memory.Result{
		packTestHit("b", "same same same same same same", 5),
		packTestHit("a", "same same same same same same", 5),
		packTestHit("c", "same same same same same same", 5),
	}
	sel := packSelect(hits, "same", 4, defaultPackWeights())
	// Budget 4 with ~1-2 token entries — at least the first pick must be "a".
	if len(sel.GreedyOrder) == 0 || sel.GreedyOrder[0] != "a" {
		t.Errorf("tie-break should pick lowest ID first, got %v", sel.GreedyOrder)
	}
}

func TestParsePackWeights(t *testing.T) {
	if w := defaultPackWeights(); w.Rel != 3 || w.Cover != 1 || w.Fac != 1 || w.Div != 1 {
		t.Errorf("default weights = %+v, want 3:1:1:1", w)
	}
	w, err := parsePackWeights("3:1:1:1")
	if err != nil || w.Rel != 3 {
		t.Errorf("parse 3:1:1:1 → %+v, %v", w, err)
	}
	if _, err := parsePackWeights("3:1:1"); err == nil {
		t.Error("3 terms must fail")
	}
	if _, err := parsePackWeights("3:1:1:-1"); err == nil {
		t.Error("negative term must fail")
	}
	if _, err := parsePackWeights(""); err != nil {
		t.Errorf("empty must default, got %v", err)
	}
}

func TestPackSelectSelectedOrderFused(t *testing.T) {
	// Selected must come back in fused-score descending order for the
	// renderer, regardless of greedy pick order.
	hits := []memory.Result{
		packTestHit("low", "totally distinct vocabulary set words alpha", 1),
		packTestHit("high", "another distinct vocabulary set words beta", 10),
	}
	sel := packSelect(hits, "vocabulary distinct", 200, defaultPackWeights())
	if len(sel.Selected) < 2 {
		t.Fatalf("expected both selected, got %+v", sel.GreedyOrder)
	}
	if sel.Selected[0].Name != "high" || sel.Selected[0].Score < sel.Selected[1].Score {
		t.Errorf("Selected not in fused order: %+v", sel.Selected)
	}
}
