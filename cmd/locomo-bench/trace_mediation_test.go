package main

import (
	"testing"

	"github.com/wallfacers/engram/memory"
)

// TestCapEvidence pins the --trace-multi-evidence hard cap: statements beyond
// the cap are dropped, the head is preserved, and 0 / no-cap passes through.
func TestCapEvidence(t *testing.T) {
	ev := make([]traceEvidence, 6)
	for i := range ev {
		ev[i] = traceEvidence{Text: "e" + string(rune('0'+i))}
	}
	if got := capEvidence(ev, 0); len(got) != 6 {
		t.Fatalf("cap=0 must pass through, got %d", len(got))
	}
	if got := capEvidence(ev, 3); len(got) != 3 || got[0].Text != "e0" || got[2].Text != "e2" {
		t.Fatalf("cap=3 must keep head, got %+v", got)
	}
	if got := capEvidence(ev, 9); len(got) != 6 {
		t.Fatalf("cap above len must pass through, got %d", len(got))
	}
}

// TestEvidenceTouchesTopK pins the --trace-fallback-topk guard: evidence is
// "touching" when ANY cited id is inside the retrieval top-k set (the trace
// sidecar kept a top candidate), and only falls back when it cites none.
func TestEvidenceTouchesTopK(t *testing.T) {
	topK := []memory.Result{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	}
	cases := []struct {
		name     string
		evidence []traceEvidence
		want     bool
	}{
		{"cites-top1", []traceEvidence{{CitedIDs: []string{"a"}}}, true},
		{"cites-top3", []traceEvidence{{CitedIDs: []string{"z", "c"}}}, true},
		{"cites-none", []traceEvidence{{CitedIDs: []string{"x", "y"}}}, false},
		{"cites-none-multi", []traceEvidence{{CitedIDs: []string{"x"}}, {CitedIDs: []string{"y"}}}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := evidenceTouchesTopK(c.evidence, topK); got != c.want {
				t.Fatalf("evidenceTouchesTopK = %t, want %t", got, c.want)
			}
		})
	}
}

// TestTracePromptsDistinct guards the two sidecar prompts: the multi-evidence
// variant must carry the intent-breadth rule (and thus a non-fixed target_count),
// while the legacy prompt stays single-evidence. A regression here would silently
// change default trace behavior (SC-004).
func TestTracePromptsDistinct(t *testing.T) {
	if traceSystemPrompt == traceMultiEvidencePrompt {
		t.Fatal("single and multi-evidence prompts must differ")
	}
	for _, want := range []string{"1-2 evidence statements", "3-6 (one statement per hop", "completeness over minimalism"} {
		if !containsStr(traceMultiEvidencePrompt, want) {
			t.Fatalf("multi-evidence prompt missing %q", want)
		}
	}
	if containsStr(traceSystemPrompt, "completeness over minimalism") {
		t.Fatal("legacy prompt must stay single-evidence (SC-004)")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
