package memory_test

// TDD (feature 012, US1): tests for the doc2query #query pseudo-query shadow
// vector. Contract: specs/012-doc2query-shadow/contracts/query-shadow-engine.md.
//
// Frozen conventions asserted here:
//   - shadow name = "<source fact name>#query"
//   - write: embedOne on a #query name embeds the fact's pseudo-queries joined
//     VERBATIM (no content-word drop, unlike #alias) and Puts a vector under the
//     shadow name; empty query set -> no shadow vector
//   - retrieval: vectorRankContext folds "#query" cosine back onto the source
//     fact BEFORE truncation via the same content-agnostic max-pool used for
//     #alias; shadow name never leaks to Results
//   - facts without pseudo-queries stay byte-identical (inert-by-default)
//   - degenerate (nil client / orphan shadow / empty queries) never panics
//   - a fact carrying BOTH #alias and #query shadows folds both, max-pools the
//     best, and still surfaces exactly once
//
// Reuses helpers from alias_shadow_test.go / retriever_test.go / embedder_test.go
// (recordingClient, vectorFakeClient, newStores, seedRetrievalCorpus, rankOf,
// keysOf) — all in package memory_test.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// T009 — embedOne #query branch: enqueue "<fact>#query" -> pseudo-queries
// embedded VERBATIM under the shadow name (content-word overlap kept); empty
// query set -> no shadow vector.
func TestQueryShadow_EmbedOneBranch(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)

	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "Jon lost his banking job on 2023-01-19.", CharCount: 39}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	// Queries share content words (Jon, job) — those MUST be kept, unlike #alias.
	if err := es.PutFactQueries(ctx, "a", []string{"When did Jon lose his job?", "Who lost a banking job?"}); err != nil {
		t.Fatalf("put queries a: %v", err)
	}

	rc := &recordingClient{model: "m1"}
	emb := memory.NewEmbedder(es, vs, rc, 8)
	emb.Enqueue(memory.QueryShadowName("a")) // "a#query"
	emb.Close()

	vecs, err := vs.LoadAllForModel(ctx, "m1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := vecs["a#query"]; !ok {
		t.Fatalf("expected shadow vector under a#query, got keys %v", keysOf(vecs))
	}
	joined := strings.ToLower(strings.Join(rc.recorded(), " | "))
	if !strings.Contains(joined, "when did jon lose his job?") {
		t.Fatalf("shadow embed text must include the pseudo-query verbatim, got %q", joined)
	}
	if !strings.Contains(joined, "jon") {
		t.Fatalf("#query must KEEP content-word overlap (jon), got %q", joined)
	}

	// Empty query set -> no shadow vector.
	if err := es.Upsert(ctx, &memory.Entry{Name: "b", Content: "Gina moved to Berlin.", CharCount: 21}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	rc2 := &recordingClient{model: "m1"}
	emb2 := memory.NewEmbedder(es, vs, rc2, 8)
	emb2.Enqueue(memory.QueryShadowName("b"))
	emb2.Close()
	vecs2, err := vs.LoadAllForModel(ctx, "m1")
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	if _, ok := vecs2["b#query"]; ok {
		t.Fatalf("no pseudo-queries must yield NO shadow vector, got keys %v", keysOf(vecs2))
	}
}

// T010 — parity guard: a corpus with no memory_fact_queries rows retrieves
// exactly as before and no "#query" name ever appears. Green now; MUST stay
// green post-merge (inert-by-default).
func TestQueryShadow_NoQueriesParity(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t) // no pseudo-queries seeded
	fc := &vectorFakeClient{
		model: "m1",
		vectors: map[string][]float32{
			"The user moved from Sweden four years ago.":   {1, 0, 0},
			"The user prefers Python for scripting tasks.": {0, 1, 0},
			"The user drinks black coffee every morning.":  {0, 0, 1},
			"caffeine intake": {0, 0, 1},
		},
	}
	emb := memory.NewEmbedder(es, vs, fc, 8)
	_ = emb.Backfill(ctx)
	emb.Close()

	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "caffeine intake", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rankOf(got, "coffee-habit") != 0 {
		t.Fatalf("no-queries parity broken: coffee-habit must be rank 0, got %+v", got)
	}
	for _, g := range got {
		if strings.Contains(g.Name, "#query") {
			t.Fatalf("shadow name leaked into no-queries results: %+v", got)
		}
	}
}

// T011 — merge lifts source: gold's text vector is orthogonal to the query
// (weak) while its #query shadow aligns (strong); max-pool before truncation
// must lift gold into results. RED pre-impl (gold absent).
func TestQueryShadow_MergeLiftsSource(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "Discussed a difficult personal matter.", CharCount: 38}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{0, 1, 0}, now); err != nil { // weak: cosine 0 vs query
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#query", model, []float32{1, 0, 0}, now); err != nil { // strong: cosine 1
		t.Fatalf("put gold shadow vec: %v", err)
	}
	for i := 0; i < 6; i++ {
		name := "filler-" + string(rune('a'+i))
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: "Weather note number " + name, CharCount: 20}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
		if err := vs.Put(ctx, name, model, []float32{1, 1, 0}, now); err != nil { // cosine ~0.707
			t.Fatalf("put %s vec: %v", name, err)
		}
	}

	fc := &vectorFakeClient{model: model, vectors: map[string][]float32{
		"when did the difficult matter happen": {1, 0, 0}, // aligns with gold's #query shadow
	}}
	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "when did the difficult matter happen", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rankOf(got, "gold") < 0 {
		t.Fatalf("max-pool merge must lift gold into results via its #query vector, got %+v", got)
	}
	for _, g := range got {
		if strings.Contains(g.Name, "#query") {
			t.Fatalf("shadow name leaked into results: %+v", got)
		}
	}
}

// T012a — dedup single vote: same source hit by both text and #query vectors
// appears exactly once.
func TestQueryShadow_DedupSingleVote(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "A meaningful reflection.", CharCount: 24}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{1, 0, 0}, now); err != nil {
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#query", model, []float32{1, 0, 0}, now); err != nil {
		t.Fatalf("put gold shadow vec: %v", err)
	}

	fc := &vectorFakeClient{model: model, vectors: map[string][]float32{
		"meaningful reflection": {1, 0, 0},
	}}
	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "meaningful reflection", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	count := 0
	for _, g := range got {
		if g.Name == "gold" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("source fact double-hit must dedup to exactly one result, got %d in %+v", count, got)
	}
}

// T012b — shadow name never leaks: any #query hit resolves to the source name.
func TestQueryShadow_ShadowNameNeverLeaks(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "Some private reflection.", CharCount: 24}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{0, 1, 0}, now); err != nil {
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#query", model, []float32{1, 0, 0}, now); err != nil {
		t.Fatalf("put gold shadow vec: %v", err)
	}

	fc := &vectorFakeClient{model: model, vectors: map[string][]float32{
		"private reflection": {1, 0, 0},
	}}
	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "private reflection", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, g := range got {
		if strings.Contains(g.Name, "#query") {
			t.Fatalf("shadow name must never appear in Results, got %+v", got)
		}
	}
}

// T013a — degenerate: nil client, orphan shadow (source fact missing), and empty
// query set must never panic and must degrade per-signal.
func TestQueryShadow_Degenerate(t *testing.T) {
	ctx := context.Background()

	// (a) nil embedding client: semantic absent, no panic.
	es, vs := seedRetrievalCorpus(t)
	rNil := memory.NewRetriever(es, vs, nil)
	if _, err := rNil.Search(ctx, "anything at all", 3); err != nil {
		t.Fatalf("nil-client search must degrade, not error: %v", err)
	}

	// (b) orphan shadow: vector for "z#query" with no source entry "z".
	es2, vs2 := newStores(t)
	now := time.Now()
	const model = "m1"
	if err := es2.Upsert(ctx, &memory.Entry{Name: "real", Content: "An unrelated fact.", CharCount: 18}); err != nil {
		t.Fatalf("upsert real: %v", err)
	}
	if err := vs2.Put(ctx, "real", model, []float32{0, 1, 0}, now); err != nil {
		t.Fatalf("put real vec: %v", err)
	}
	if err := vs2.Put(ctx, "z#query", model, []float32{1, 0, 0}, now); err != nil { // orphan
		t.Fatalf("put orphan shadow: %v", err)
	}
	fc := &vectorFakeClient{model: model, vectors: map[string][]float32{
		"orphan probe": {1, 0, 0},
	}}
	r2 := memory.NewRetriever(es2, vs2, fc)
	got, err := r2.Search(ctx, "orphan probe", 5)
	if err != nil {
		t.Fatalf("orphan-shadow search must not error: %v", err)
	}
	for _, g := range got {
		if g.Name == "z#query" || g.Name == "z" {
			t.Fatalf("orphan shadow must be discarded, not surfaced, got %+v", got)
		}
	}

	// (c) empty query set: shadow enqueue is a no-op, no panic.
	es3, vs3 := newStores(t)
	if err := es3.Upsert(ctx, &memory.Entry{Name: "c", Content: "cats and dogs", CharCount: 13}); err != nil {
		t.Fatalf("upsert c: %v", err)
	}
	if err := es3.PutFactQueries(ctx, "c", []string{"", "   "}); err != nil { // all blank
		t.Fatalf("put queries c: %v", err)
	}
	rc := &recordingClient{model: model}
	emb := memory.NewEmbedder(es3, vs3, rc, 8)
	emb.Enqueue(memory.QueryShadowName("c"))
	emb.Close()
	vecs, err := vs3.LoadAllForModel(ctx, model)
	if err != nil {
		t.Fatalf("load3: %v", err)
	}
	if _, ok := vecs["c#query"]; ok {
		t.Fatalf("empty query set must produce no shadow vector, got %v", keysOf(vecs))
	}
}

// T013b — coexistence with #alias: a fact carrying BOTH shadows folds both back
// onto the source, max-pools the best, and surfaces exactly once. Here the
// #alias vector is the strong one; the #query vector is weak.
func TestQueryShadow_CoexistWithAlias(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "Discussed a difficult personal matter.", CharCount: 38}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{0, 1, 0}, now); err != nil { // weak text
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#query", model, []float32{0, 1, 0}, now); err != nil { // weak query
		t.Fatalf("put gold query vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#alias", model, []float32{1, 0, 0}, now); err != nil { // strong alias
		t.Fatalf("put gold alias vec: %v", err)
	}
	for i := 0; i < 6; i++ {
		name := "filler-" + string(rune('a'+i))
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: "Weather note number " + name, CharCount: 20}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
		if err := vs.Put(ctx, name, model, []float32{1, 1, 0}, now); err != nil {
			t.Fatalf("put %s vec: %v", name, err)
		}
	}

	fc := &vectorFakeClient{model: model, vectors: map[string][]float32{
		"self acceptance journey": {1, 0, 0}, // aligns with gold's #alias shadow
	}}
	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "self acceptance journey", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rankOf(got, "gold") < 0 {
		t.Fatalf("coexisting shadows must max-pool the strongest and lift gold, got %+v", got)
	}
	count := 0
	for _, g := range got {
		if g.Name == "gold" {
			count++
		}
		if strings.Contains(g.Name, "#alias") || strings.Contains(g.Name, "#query") {
			t.Fatalf("shadow name leaked into results: %+v", got)
		}
	}
	if count != 1 {
		t.Fatalf("gold must surface exactly once across two shadows, got %d in %+v", count, got)
	}
}
