package main

import (
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// TestBuildCompileCandidatesDedupsByCandidateID: the flat source list carries
// one entry per navigation hit; several hits may reference the same rendered
// candidate. buildCompileCandidates must dedup by CandidateID (first wins) or
// the compiler emits duplicate bundle items and the formal 1:1 item->candidate
// contract fails (026).
func TestBuildCompileCandidatesDedupsByCandidateID(t *testing.T) {
	dup := evalRenderedCandidate{CandidateID: "cand:1", Kind: "episode", Rank: 1, Score: 0.9, SourceIDs: []string{"ev:1"}}
	sources := []formalExpandedSource{
		{Candidate: dup, Result: memory.Result{ID: "a", Content: "alpha"}},
		{Candidate: dup, Result: memory.Result{ID: "b", Content: "beta"}}, // same CandidateID, second hit
		{Candidate: evalRenderedCandidate{CandidateID: "cand:2", Kind: "episode", Rank: 2, Score: 0.8, SourceIDs: []string{"ev:2"}}, Result: memory.Result{ID: "c", Content: "gamma"}},
	}
	candidates := buildCompileCandidates(sources)
	if len(candidates) != 2 {
		t.Fatalf("buildCompileCandidates returned %d candidates, want 2", len(candidates))
	}
	if candidates[0].ID != "cand:1" || candidates[0].Text != "alpha" {
		t.Fatalf("first-occurrence dedup broken: %#v", candidates[0])
	}
	if candidates[1].ID != "cand:2" {
		t.Fatalf("second candidate wrong: %#v", candidates[1])
	}
}

// TestFormalCompileRendererIncludesQueryInUser: the compiler's budget decision
// must count the question text too — the formal answer input is question +
// rendered evidence. Counting evidence alone let the compiler overshoot the cap
// and made every frozen AnswerInputTokens disagree with the harness preflight
// (026).
func TestFormalCompileRendererIncludesQueryInUser(t *testing.T) {
	renderer := formalCompileRenderer{model: "answerer-x", system: "sys"}
	input := renderer.RenderAnswerInput("Who wrote the bowl?", "Caroline: the bowls are amazing")
	if !strings.HasPrefix(input.User, "Who wrote the bowl?") {
		t.Fatalf("RenderAnswerInput.User must lead with the query, got %q", input.User)
	}
	if !strings.Contains(input.User, "Caroline: the bowls are amazing") {
		t.Fatalf("RenderAnswerInput.User must contain the rendered evidence, got %q", input.User)
	}
	if input.Model != "answerer-x" || input.System != "sys" {
		t.Fatalf("RenderAnswerInput passthrough broken: %#v", input)
	}
}

// TestCompileBundleItemsDedupsCandidate: the compiler admits by Evidence
// source, so one rendered candidate spanning several sources can surface as
// several KEEP items with the same CandidateIDs[0]. The formal contract is one
// item per rendered candidate; the bridge must collapse them (first wins) or
// the duplicate item identity fails the 1:1 structural check.
func TestCompileBundleItemsDedupsCandidate(t *testing.T) {
	sourceByCandidate := map[string]formalExpandedSource{
		"cand:X": {Item: evalFormalBundleItem{Text: "shared text", Sources: []evalFormalSourceSpan{{EvidenceID: "ev:1"}}}},
	}
	compiled := []evidencecompiler.BundleItem{
		{CandidateIDs: []string{"cand:X"}, Sources: []evidencecompiler.SourceSpan{{SourceID: "ev:1"}}},
		{CandidateIDs: []string{"cand:X"}, Sources: []evidencecompiler.SourceSpan{{SourceID: "ev:2"}}}, // same candidate, second source
		{CandidateIDs: []string{"cand:Y"}, Sources: []evidencecompiler.SourceSpan{{SourceID: "ev:3"}}},
	}
	protocol := testFormalProtocolBase(t)
	items := compileBundleItems(protocol, compiled, sourceByCandidate)
	if len(items) != 1 {
		t.Fatalf("compileBundleItems returned %d items, want 1 (dup candidate collapsed)", len(items))
	}
	if items[0].ItemID != formalBundleItemID("cand:X") || items[0].Text != "shared text" {
		t.Fatalf("kept item wrong: %#v", items[0])
	}
}
