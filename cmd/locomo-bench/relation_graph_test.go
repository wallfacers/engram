package main

// 031 offline relation-computation tests (specs/031, contracts/
// evidence-relations.md §4-§6). Deterministic, zero model, zero network.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

func relTestHits() []memory.Result {
	dt := func(s string) *time.Time {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return &t
	}
	return []memory.Result{
		{Name: "fact-a", Content: `The "Project Atlas" roadmap began because Project Atlas funding was approved.`},
		{Name: "fact-b", Content: `"Project Atlas" launched due to the 2024 budget.`},
		{Name: "fact-c", Content: `A separate note about weather patterns.`},
		{Name: "fact-d", Content: "Atlas team met on Project Atlas."},
		{Name: "fact-e", Content: `"Project Atlas" resulted in wider deployment.`, EventDate: dt("2025-03-10")},
		{Name: "fact-f", Content: "The second deployment completed.", EventDate: dt("2025-05-21")},
	}
}

func TestRelationCompute_Deterministic(t *testing.T) {
	hits := relTestHits()
	ctx := context.Background()
	a, err := computeRelationContext(ctx, hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	b, err := computeRelationContext(ctx, hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute (2nd): %v", err)
	}
	if a == nil || b == nil {
		t.Fatalf("expected a non-nil block, got a=%v b=%v", a, b)
	}
	if a.Text != b.Text {
		t.Errorf("non-deterministic render:\nA=%q\nB=%q", a.Text, b.Text)
	}
	// Edge sets must be byte-identical too (ordering + evidence + rank).
	if len(a.Edges) != len(b.Edges) {
		t.Fatalf("edge count differs: %d vs %d", len(a.Edges), len(b.Edges))
	}
	for i := range a.Edges {
		if a.Edges[i] != b.Edges[i] {
			t.Errorf("edge %d differs: %+v vs %+v", i, a.Edges[i], b.Edges[i])
		}
	}
}

func TestRelationCompute_MultiHop(t *testing.T) {
	hits := relTestHits()
	block, err := computeRelationContext(context.Background(), hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected a block for multi-hop")
	}
	// Shared entity "Project Atlas" / "Atlas" appears across several facts →
	// related_to edges must exist and not be self loops.
	related := 0
	caused := 0
	for _, e := range block.Edges {
		if e.From == e.To {
			t.Errorf("self-loop edge: %+v", e)
		}
		switch e.Type {
		case RelationRelatedTo:
			related++
			if !strings.Contains(e.Evidence, "Project Atlas") && !strings.Contains(e.Evidence, "Atlas") {
				t.Errorf("related_to evidence should be the shared entity, got %q", e.Evidence)
			}
		case RelationCausedBy:
			caused++
			if e.Evidence != "due to" && e.Evidence != "because" && e.Evidence != "resulted in" {
				t.Errorf("caused_by evidence should be the causal word, got %q", e.Evidence)
			}
		}
	}
	if related == 0 {
		t.Error("expected ≥1 related_to edge for shared-entity facts")
	}
	if caused == 0 {
		t.Error("expected ≥1 caused_by edge for causal-word facts")
	}
	// Caps must hold (FR-005).
	if related > relationCapRelatedTo*len(hits) || caused > relationCapCausedBy*len(hits) {
		t.Errorf("cap overflow: related=%d caused=%d", related, caused)
	}
}

func TestRelationCompute_Temporal(t *testing.T) {
	hits := relTestHits()
	block, err := computeRelationContext(context.Background(), hits, temporalCategory)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected a temporal block (fact-e, fact-f dated)")
	}
	// Only temporal_next edges, ordered by date asc.
	var prev *RelationEdge
	for _, e := range block.Edges {
		if e.Type != RelationTemporalNext {
			t.Errorf("temporal block should only carry temporal_next, got %q", e.Type)
		}
		if e.From != "fact-e" || e.To != "fact-f" {
			t.Errorf("expected fact-e → fact-f chain, got %s → %s", e.From, e.To)
		}
		if prev != nil {
			t.Errorf("expected exactly one temporal_next edge, got extra %+v", e)
		}
		prev = &e
	}
}

func TestRelationCompute_EmptyFailSoft(t *testing.T) {
	// No shared entity, no dates, no causal words → nil block (fail-soft).
	hits := []memory.Result{
		{Name: "a", Content: "The quick brown fox jumps."},
		{Name: "b", Content: "A lazy dog slept in the sun."},
	}
	for _, cat := range []int{assemblyCategoryMultiHop, temporalCategory} {
		block, err := computeRelationContext(context.Background(), hits, cat)
		if err != nil {
			t.Fatalf("compute(%d): %v", cat, err)
		}
		if block != nil {
			t.Errorf("category %d: expected nil block for unrelated evidence, got %+v", cat, block)
		}
	}
}

func TestRelationCompute_CategoryNoOp(t *testing.T) {
	// single-hop / generic / open-domain never produce a block (R-6).
	hits := relTestHits()
	for _, cat := range []int{assemblyCategorySingleHop, assemblyCategoryOpenDomain} {
		block, err := computeRelationContext(context.Background(), hits, cat)
		if err != nil {
			t.Fatalf("compute(%d): %v", cat, err)
		}
		if block != nil {
			t.Errorf("category %d: expected nil block, got %+v", cat, block)
		}
	}
}

func TestRelationEdgeCap_RelatedTo(t *testing.T) {
	// Five facts all sharing one entity → related_to out-edges per evidence ≤ cap.
	hits := make([]memory.Result, 0, 6)
	for i := 0; i < 6; i++ {
		hits = append(hits, memory.Result{Name: "fact-" + string(rune('a'+i)), Content: `Shared Entity "Core" mention.`})
	}
	block, err := computeRelationContext(context.Background(), hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected a block over 6 shared-entity facts")
	}
	out := make(map[string]int)
	for _, e := range block.Edges {
		out[e.From]++
	}
	for src, n := range out {
		if n > relationCapRelatedTo {
			t.Errorf("evidence %s has %d related_to out-edges, cap %d", src, n, relationCapRelatedTo)
		}
	}
}

func TestRelationRender_Shape(t *testing.T) {
	hits := relTestHits()
	block, err := computeRelationContext(context.Background(), hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected block")
	}
	if !strings.HasPrefix(block.Text, "[relations]\n") || !strings.HasSuffix(block.Text, "\n[/relations]") {
		t.Errorf("block should be wrapped in [relations] markers, got %q", block.Text)
	}
	for _, line := range strings.Split(strings.TrimSuffix(block.Text, "\n[/relations]")[len("[relations]\n"):], "\n") {
		if !strings.Contains(line, "--") || !strings.Contains(line, "-->") {
			t.Errorf("edge line should be From --Type(依据)--> To, got %q", line)
		}
	}
	// Appending must keep the prompt intact plus the block.
	user := appendRelationBlock("question + evidence", block)
	if !strings.HasPrefix(user, "question + evidence\n") || !strings.HasSuffix(user, "\n"+block.Text) {
		t.Errorf("appendRelationBlock shape wrong: %q", user)
	}
}

func TestRelationCompute_OrderIndependent(t *testing.T) {
	// The block must depend only on evidence content, not on hit order
	// (determinism contract §5): assembleEvidence reorders hits for assembly, so
	// a shuffled input must yield the identical edge set.
	hits := relTestHits()
	shuffled := []memory.Result{hits[4], hits[0], hits[5], hits[2], hits[1], hits[3]}
	a, err := computeRelationContext(context.Background(), hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	b, err := computeRelationContext(context.Background(), shuffled, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute (shuffled): %v", err)
	}
	if a == nil || b == nil {
		t.Fatalf("expected blocks, got %v / %v", a, b)
	}
	if a.Text != b.Text {
		t.Errorf("relation block must be order-independent:\nA=%q\nB=%q", a.Text, b.Text)
	}
}

// TestRelationContextParity is the SC-004 hard gate: with --relation-context
// off (default) the assembled prompt is byte-identical to 030; with it on the
// only difference is the appended [relations] block.
func TestRelationContextParity(t *testing.T) {
	ctx := context.Background()
	cfg := assemblyConfig{Cap: 3600, CurrentDate: "2026-08-06", QuestionID: "conv-0-q-0"}
	onCfg := cfg
	onCfg.RelationEnabled = true

	hits := relTestHits()
	baseAsm, baseUser, err := assembleEvidence(ctx, "Q", hits, assemblyCategoryMultiHop, cfg, nil)
	if err != nil {
		t.Fatalf("assemble (off): %v", err)
	}
	if strings.Contains(baseUser, "[relations]") {
		t.Errorf("off path must not carry the relation block, got %q", baseUser)
	}
	_, onUser, err := assembleEvidence(ctx, "Q", hits, assemblyCategoryMultiHop, onCfg, nil)
	if err != nil {
		t.Fatalf("assemble (on): %v", err)
	}
	block, err := computeRelationContext(ctx, hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected a block for shared-entity hits")
	}
	want := appendRelationBlock(baseUser, block)
	if onUser != want {
		t.Errorf("on-path must equal off-path + block:\n got=%q\nwant=%q", onUser, want)
	}
	// The off-path assembly record is untouched (parity beyond the prompt).
	if strings.Contains(baseAsm.Structure, "relations") {
		t.Errorf("assembly record must not mention relations when off")
	}
}

func TestRelationBlockWithinBoundary(t *testing.T) {
	hits := relTestHits()
	block, err := computeRelationContext(context.Background(), hits, assemblyCategoryMultiHop)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if block == nil {
		t.Fatal("expected block")
	}
	// Boundary excluding every endpoint → nil (fail-closed).
	empty := map[string]bool{}
	if kept := relationBlockWithinBoundary(block, empty); kept != nil {
		t.Errorf("empty boundary should drop everything, got %+v", kept)
	}
	// Boundary containing all → unchanged edge set.
	all := map[string]bool{}
	for _, h := range hits {
		all[h.Name] = true
	}
	kept := relationBlockWithinBoundary(block, all)
	if kept == nil || len(kept.Edges) != len(block.Edges) {
		t.Errorf("full boundary should keep all %d edges, got %+v", len(block.Edges), kept)
	}
}
