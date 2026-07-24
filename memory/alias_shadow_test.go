package memory_test

// TDD (feature 011, US1): failing-first tests for dual-index alias shadow
// vectors. Contract: specs/011-dual-index-alias/contracts/shadow-embedding-engine.md.
//
// Frozen conventions asserted here:
//   - shadow name = "<source fact name>#alias"
//   - write: embedOne on a shadow name embeds the fact's aliases (minus words
//     already in Content) and Puts a vector under the shadow name; empty -> no-op
//   - retrieval: vectorRankContext folds "#alias" cosine back onto the source
//     fact BEFORE truncation via max-pool; shadow name never leaks to Results
//   - no-alias facts / all chunks / source text vectors stay byte-identical
//   - degenerate (nil client / orphan shadow / empty alias) never panics
//
// RED expectation pre-impl: TestAliasShadow_EmbedOneShadowBranch and
// TestAliasShadow_MergeLiftsSource fail on assertions (behavior absent). The
// parity/dedup/no-leak/degenerate tests are invariant guards that pass now and
// MUST stay green after the merge is implemented.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// recordingClient captures the exact texts handed to Embed so a test can assert
// the shadow embedding text (aliases merged, content-words dropped).
type recordingClient struct {
	model string
	mu    sync.Mutex
	texts []string
}

func (c *recordingClient) Model() string { return c.model }

func (c *recordingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.mu.Lock()
	c.texts = append(c.texts, texts...)
	c.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 1, 0}
	}
	return out, nil
}

func (c *recordingClient) recorded() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

func keysOf(m map[string][]float32) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// T004 — embedOne shadow branch: enqueue "<fact>#alias" -> aliases embedded
// (content-contained words dropped) and stored under the shadow name; empty
// merged text -> no shadow vector.
func TestAliasShadow_EmbedOneShadowBranch(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)

	// "painting" is already in Content (must be dropped); "self-acceptance" is
	// not (must be kept in the shadow embedding text).
	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Trigger: "hobby", Content: "The user took up painting.", CharCount: 26}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := es.PutAliases(ctx, "a", []string{"painting", "self-acceptance"}); err != nil {
		t.Fatalf("put aliases a: %v", err)
	}

	rc := &recordingClient{model: "m1"}
	emb := memory.NewEmbedder(es, vs, rc, 8)
	emb.Enqueue("a#alias") // frozen shadow-name convention
	emb.Close()            // drains and waits

	vecs, err := vs.LoadAllForModel(ctx, "m1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := vecs["a#alias"]; !ok {
		t.Fatalf("expected shadow vector under a#alias, got keys %v", keysOf(vecs))
	}
	joined := strings.ToLower(strings.Join(rc.recorded(), " | "))
	if !strings.Contains(joined, "self-acceptance") {
		t.Fatalf("shadow embed text must include non-content alias, got %q", joined)
	}
	if strings.Contains(joined, "painting") {
		t.Fatalf("shadow embed text must drop content-contained alias 'painting', got %q", joined)
	}

	// Empty merged text (every alias already in Content) -> no shadow vector.
	if err := es.Upsert(ctx, &memory.Entry{Name: "b", Content: "I love painting and hiking.", CharCount: 27}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if err := es.PutAliases(ctx, "b", []string{"painting", "hiking"}); err != nil {
		t.Fatalf("put aliases b: %v", err)
	}
	rc2 := &recordingClient{model: "m1"}
	emb2 := memory.NewEmbedder(es, vs, rc2, 8)
	emb2.Enqueue("b#alias")
	emb2.Close()
	vecs2, err := vs.LoadAllForModel(ctx, "m1")
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	if _, ok := vecs2["b#alias"]; ok {
		t.Fatalf("all-aliases-in-content must yield NO shadow vector, got keys %v", keysOf(vecs2))
	}
}

// T005 — parity guard: a no-alias corpus's semantic retrieval is unchanged and
// no "#alias" name ever appears. Green now; MUST stay green post-merge.
func TestAliasShadow_NoAliasParity(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t) // no aliases seeded
	fc := &vectorFakeClient{
		model: "m1",
		vectors: map[string][]float32{
			"The user moved from Sweden four years ago.":   {1, 0, 0},
			"The user prefers Python for scripting tasks.": {0, 1, 0},
			"The user drinks black coffee every morning.":  {0, 0, 1},
			"caffeine intake":                              {0, 0, 1},
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
		t.Fatalf("no-alias parity broken: coffee-habit must be rank 0, got %+v", got)
	}
	for _, g := range got {
		if strings.Contains(g.Name, "#alias") {
			t.Fatalf("shadow name leaked into no-alias results: %+v", got)
		}
	}
}

// T006 — merge lifts source: gold's text vector is orthogonal to the query
// (weak) while its shadow vector aligns (strong); max-pool before truncation
// must lift gold into results. RED pre-impl (gold absent).
func TestAliasShadow_MergeLiftsSource(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	// gold shares no keyword/entity token with the query -> semantic-only.
	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "Discussed a difficult personal matter.", CharCount: 38}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{0, 1, 0}, now); err != nil { // weak: cosine 0 vs query
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#alias", model, []float32{1, 0, 0}, now); err != nil { // strong: cosine 1
		t.Fatalf("put gold shadow vec: %v", err)
	}
	// Enough medium fillers (cosine ~0.707) to push gold's weak text out of top-k.
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
		"self acceptance journey": {1, 0, 0}, // query aligns with gold's shadow
	}}
	r := memory.NewRetriever(es, vs, fc)
	got, err := r.Search(ctx, "self acceptance journey", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rankOf(got, "gold") < 0 {
		t.Fatalf("max-pool merge must lift gold into results via its shadow vector, got %+v", got)
	}
	for _, g := range got {
		if strings.Contains(g.Name, "#alias") {
			t.Fatalf("shadow name leaked into results: %+v", got)
		}
	}
}

// T007a — dedup single vote: same source hit by both text and shadow vectors
// appears exactly once. Invariant guard.
func TestAliasShadow_DedupSingleVote(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	now := time.Now()
	const model = "m1"

	if err := es.Upsert(ctx, &memory.Entry{Name: "gold", Content: "A meaningful reflection.", CharCount: 24}); err != nil {
		t.Fatalf("upsert gold: %v", err)
	}
	if err := vs.Put(ctx, "gold", model, []float32{1, 0, 0}, now); err != nil { // strong text
		t.Fatalf("put gold vec: %v", err)
	}
	if err := vs.Put(ctx, "gold#alias", model, []float32{1, 0, 0}, now); err != nil { // strong shadow
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

// T007b — shadow name never leaks: any shadow hit resolves to the source name.
// Invariant guard (shadow rows have no memory_entries row -> must not surface).
func TestAliasShadow_ShadowNameNeverLeaks(t *testing.T) {
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
	if err := vs.Put(ctx, "gold#alias", model, []float32{1, 0, 0}, now); err != nil {
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
		if strings.Contains(g.Name, "#alias") {
			t.Fatalf("shadow name must never appear in Results, got %+v", got)
		}
	}
}

// T008 — degenerate: nil client, orphan shadow (source fact missing), and empty
// alias must never panic and must degrade per-signal. Invariant guard.
func TestAliasShadow_Degenerate(t *testing.T) {
	ctx := context.Background()

	// (a) nil embedding client: semantic absent, no panic.
	es, vs := seedRetrievalCorpus(t)
	rNil := memory.NewRetriever(es, vs, nil)
	if _, err := rNil.Search(ctx, "anything at all", 3); err != nil {
		t.Fatalf("nil-client search must degrade, not error: %v", err)
	}

	// (b) orphan shadow: vector for "z#alias" with no source entry "z".
	es2, vs2 := newStores(t)
	now := time.Now()
	const model = "m1"
	if err := es2.Upsert(ctx, &memory.Entry{Name: "real", Content: "An unrelated fact.", CharCount: 18}); err != nil {
		t.Fatalf("upsert real: %v", err)
	}
	if err := vs2.Put(ctx, "real", model, []float32{0, 1, 0}, now); err != nil {
		t.Fatalf("put real vec: %v", err)
	}
	if err := vs2.Put(ctx, "z#alias", model, []float32{1, 0, 0}, now); err != nil { // orphan
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
		if g.Name == "z#alias" || g.Name == "z" {
			t.Fatalf("orphan shadow must be discarded, not surfaced, got %+v", got)
		}
	}

	// (c) empty alias set: shadow enqueue is a no-op, no panic.
	es3, vs3 := newStores(t)
	if err := es3.Upsert(ctx, &memory.Entry{Name: "c", Content: "cats and dogs", CharCount: 13}); err != nil {
		t.Fatalf("upsert c: %v", err)
	}
	if err := es3.PutAliases(ctx, "c", []string{"cats", "dogs"}); err != nil { // both in content
		t.Fatalf("put aliases c: %v", err)
	}
	rc := &recordingClient{model: model}
	emb := memory.NewEmbedder(es3, vs3, rc, 8)
	emb.Enqueue("c#alias")
	emb.Close()
	vecs, err := vs3.LoadAllForModel(ctx, model)
	if err != nil {
		t.Fatalf("load3: %v", err)
	}
	if _, ok := vecs["c#alias"]; ok {
		t.Fatalf("empty merged alias text must produce no shadow vector, got %v", keysOf(vecs))
	}
}
