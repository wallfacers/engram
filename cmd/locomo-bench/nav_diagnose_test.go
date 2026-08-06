package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func TestQuestionRewriteVariants(t *testing.T) {
	q := "What area was hit by the flood in May 2023?"
	variants := questionRewriteVariants(q)
	if len(variants) == 0 {
		t.Fatal("want at least one variant")
	}
	joined := strings.Join(variants, " | ")
	for _, want := range []string{"2023", "may"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Fatalf("variants must contain %q: %s", want, joined)
		}
	}
	for _, v := range variants {
		if strings.EqualFold(v, q) {
			t.Fatalf("variant must differ from the raw question: %q", v)
		}
	}
}

func TestQuestionRewriteVariantsQuotedAndTitle(t *testing.T) {
	variants := questionRewriteVariants(`When was "Golden Gate Bridge" completed?`)
	joined := strings.Join(variants, " | ")
	if !strings.Contains(joined, "Golden Gate Bridge") {
		t.Fatalf("quoted/title variant missing: %s", joined)
	}
}

func TestExtractEntitiesFromHits(t *testing.T) {
	hits := []memory.Result{
		{Content: `Alice said "my birthday party" was fun. Caroline visited Golden Gate Bridge.`},
		{Content: `The mayor met with Bob on Friday.`},
	}
	entities := extractEntitiesFromHits(hits)
	if len(entities) == 0 {
		t.Fatal("want entities")
	}
	joined := strings.Join(entities, " | ")
	for _, want := range []string{"my birthday party", "Golden Gate Bridge"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("want entity %q in %s", want, joined)
		}
	}
	if len(entities) > 5 {
		t.Fatalf("cap at 5 entities, got %d", len(entities))
	}
}

// TestGoldRankInHitsChunkPath: chunk hits map gold coverage by exact turn-id
// overlap (chunkTurns), reporting the first gold-covering rank.
func TestGoldRankInHitsChunkPath(t *testing.T) {
	chunkTurns := map[string][]string{
		"chunk-c0-s1-000": {"D1:1", "D1:2"},
		"chunk-c0-s2-001": {"D2:3"},
	}
	goldTurns := []string{"D2:3"}
	goldTurnText := map[string]string{"D1:1": "text", "D1:2": "text", "D2:3": "text"}
	hits := []memory.Result{
		{ID: "e1", Name: "chunk-c0-s1-000", Content: "A"},
		{ID: "e2", Name: "chunk-c0-s2-001", Content: "B"},
	}
	res := goldRankInHits(hits, chunkTurns, goldTurnText, goldTurns, 0.8)
	if !res.GoldHit || res.GoldRank != 2 {
		t.Fatalf("want gold hit at rank 2, got %#v", res)
	}
}

func TestGoldRankInHitsMiss(t *testing.T) {
	chunkTurns := map[string][]string{"chunk-c0-s1-000": {"D1:1"}}
	goldTurns := []string{"D9:9"}
	hits := []memory.Result{{ID: "e1", Name: "chunk-c0-s1-000", Content: "A"}}
	res := goldRankInHits(hits, chunkTurns, map[string]string{}, goldTurns, 0.8)
	if res.GoldHit || res.GoldRank != -1 {
		t.Fatalf("want miss (rank -1), got %#v", res)
	}
}

// TestGoldInConversationPool: in-pool requires the covering chunk to actually
// exist in the persisted store (all-conversation oracle).
func TestGoldInConversationPool(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	if err := es.Upsert(ctx, &memory.Entry{Name: "chunk-c0-s1-000", Content: "flood hit the area"}); err != nil {
		t.Fatal(err)
	}
	chunkTurns := map[string][]string{"chunk-c0-s1-000": {"D1:1"}}
	if !goldInConversationPool(es, chunkTurns, []string{"D1:1"}, ctx) {
		t.Fatal("gold turn with persisted chunk must be in pool")
	}
	if goldInConversationPool(es, chunkTurns, []string{"D9:9"}, ctx) {
		t.Fatal("gold turn without a chunk must not be in pool")
	}
	missing := map[string][]string{"chunk-c0-s9-999": {"D1:1"}}
	if goldInConversationPool(es, missing, []string{"D1:1"}, ctx) {
		t.Fatal("chunk that is not persisted must not count as in pool")
	}
}

// TestSimulateRescueFindsGoldViaRewrite: a rewrite variant that retrieves the
// gold chunk must be reported as a rescueable hit.
func TestSimulateRescueFindsGoldViaRewrite(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	e := &memory.Entry{Name: "chunk-c0-s1-000", Content: "The mayor declared an emergency after the flood in May 2023."}
	if err := es.Upsert(ctx, e); err != nil {
		t.Fatal(err)
	}
	vs := memory.NewVectorStore(st.DB())
	r := memory.NewRetriever(es, vs, nil)
	chunkTurns := map[string][]string{"chunk-c0-s1-000": {"D1:1"}}
	goldTurnText := map[string]string{"D1:1": "The mayor declared an emergency after the flood in May 2023."}
	goldTurns := []string{"D1:1"}

	qa := locomoQA{Question: "What happened after the flood?", QuestionID: "conv-0-q-0"}
	rescue := simulateRescue(ctx, r, qa, nil, chunkTurns, goldTurnText, goldTurns, 10, 0.8)
	found := false
	for _, s := range rescue {
		if s.Action == "rewrite" && s.GoldHit {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("simulated rewrite should rescue the gold: %#v", rescue)
	}
}
