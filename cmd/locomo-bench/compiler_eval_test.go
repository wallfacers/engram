package main

import (
	"testing"

	"github.com/wallfacers/engram/memory"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

func TestSelectExactTokenCandidates(t *testing.T) {
	candidates := []evidencecompiler.Candidate{
		{ID: "c-full", Rank: 0, Text: "Alice met Bob in Tokyo on 2024."},
		{ID: "c-half", Rank: 1, Text: "Bob went to Osaka."},
		{ID: "c-none", Rank: 2, Text: "The weather is nice."},
	}
	selected := selectExactTokenCandidates("who met Bob in Tokyo?", candidates, 2)
	if len(selected) != 2 {
		t.Fatalf("selection length = %d, want 2", len(selected))
	}
	if selected[0].CandidateID != "c-full" {
		t.Fatalf("top candidate = %q, want c-full (highest query-token recall)", selected[0].CandidateID)
	}
	if selected[0].Score <= selected[1].Score {
		t.Fatalf("selection not sorted by score desc: %#v", selected)
	}
	// limit truncation
	limited := selectExactTokenCandidates("who met Bob in Tokyo?", candidates, 1)
	if len(limited) != 1 || limited[0].CandidateID != "c-full" {
		t.Fatalf("limit=1 selection = %#v, want [c-full]", limited)
	}
	// no overlap: zero-recall candidates are excluded (arm falls back)
	none := selectExactTokenCandidates("zzz qqq", candidates, 3)
	if len(none) != 0 {
		t.Fatalf("no-overlap selection = %#v, want empty (fallback)", none)
	}
	// empty query tokens: empty selection
	empty := selectExactTokenCandidates("!!!", candidates, 3)
	if len(empty) != 0 {
		t.Fatalf("empty-query selection = %#v, want empty", empty)
	}
	// tie-break by rank
	tie := []evidencecompiler.Candidate{
		{ID: "a", Rank: 3, Text: "alpha beta gamma"},
		{ID: "b", Rank: 1, Text: "alpha beta gamma"},
	}
	tied := selectExactTokenCandidates("alpha beta", tie, 2)
	if tied[0].CandidateID != "b" {
		t.Fatalf("tie not broken by rank: %#v", tied)
	}
}

func TestCompileExactTokenArm(t *testing.T) {
	candidates := []evidencecompiler.Candidate{
		{ID: "c-1", Rank: 0, Text: "Alice met Bob in Tokyo on 2024.", SourceIDs: []string{"s-1", "s-2"}},
		{ID: "c-2", Rank: 1, Text: "Unrelated weather note.", SourceIDs: []string{"s-3"}},
	}
	bundle, trace, err := compileExactTokenArm("who met Bob in Tokyo?", candidates, 1)
	if err != nil {
		t.Fatalf("compile exact-token arm: %v", err)
	}
	if len(bundle.Items) != 1 {
		t.Fatalf("bundle items = %d, want 1", len(bundle.Items))
	}
	item := bundle.Items[0]
	if item.CandidateIDs[0] != "c-1" || item.Text != "Alice met Bob in Tokyo on 2024." {
		t.Fatalf("unexpected top item: %#v", item)
	}
	if len(item.Sources) != 2 || item.Sources[0].SourceID != "s-1" {
		t.Fatalf("item sources not carried through: %#v", item.Sources)
	}
	if len(bundle.SourceIDs) != 2 || !trace.Valid || len(trace.CandidateIDs) != 1 {
		t.Fatalf("unexpected bundle/trace: bundle=%#v trace=%#v", bundle, trace)
	}
	if trace.FallbackReason != "" {
		t.Fatalf("unexpected fallback on overlap: %q", trace.FallbackReason)
	}
	// no overlap: empty items with fallback recorded, still valid trace
	empty, emptyTrace, err := compileExactTokenArm("zzz qqq", candidates, 2)
	if err != nil {
		t.Fatalf("compile empty overlap: %v", err)
	}
	if len(empty.Items) != 0 || emptyTrace.FallbackReason != exactTokenArmFallback || !emptyTrace.Valid {
		t.Fatalf("no-overlap arm must degrade with fallback: bundle=%#v trace=%#v", empty, emptyTrace)
	}
	if _, _, err := compileExactTokenArm("q", nil, 1); err == nil {
		t.Fatal("compile with no candidates must fail")
	}
}

// TestExactTokenArmSharesCandidateList proves the arm scores the same flat
// source list as compileFormalSources (the byte-replay contract).
func TestExactTokenArmSharesCandidateList(t *testing.T) {
	expanded := []formalExpandedAnchor{
		{Sources: []formalExpandedSource{
			{Result: mockMemoryResult("Alice met Bob in Tokyo.")},
			{Result: mockMemoryResult("Bob left for Osaka.")},
		}},
		{Sources: []formalExpandedSource{
			{Result: mockMemoryResult("Weather was sunny.")},
		}},
	}
	flat := formalCompileSourceList(expanded)
	if len(flat) != 3 {
		t.Fatalf("flat source list length = %d, want 3", len(flat))
	}
	bundle, _, err := compileExactTokenArm("where did Bob go?", buildCompileCandidates(flat), 2)
	if err != nil {
		t.Fatalf("compile from shared candidate list: %v", err)
	}
	if len(bundle.Items) == 0 {
		t.Fatal("exact-token arm produced no items from the shared candidate list")
	}
}

func mockMemoryResult(content string) memory.Result {
	return memory.Result{Content: content}
}
