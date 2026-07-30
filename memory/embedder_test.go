package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// fakeClient returns a deterministic vector derived from input length; it records
// how many texts it was asked to embed.
type fakeClient struct {
	model string
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakeClient) Model() string { return f.model }

func (f *fakeClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls += len(texts)
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return nil, errors.New("embed boom")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 1, 0}
	}
	return out, nil
}

func newStores(t *testing.T) (*memory.EntryStore, *memory.VectorStore) {
	t.Helper()
	es, db := newEntryStore(t)
	return es, memory.NewVectorStore(db)
}

func TestEmbedder_WriteBehindPersistsVector(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "hello", CharCount: 5}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	fc := &fakeClient{model: "m1"}
	emb := memory.NewEmbedder(es, vs, fc, 8)
	emb.Enqueue("a")
	emb.Close() // drains and waits

	vecs, err := vs.LoadAllForModel(ctx, "m1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := vecs["a"]; !ok {
		t.Fatalf("expected vector for a, got %v", vecs)
	}
}

func TestEmbedder_NilWhenClientNil(t *testing.T) {
	es, vs := newStores(t)
	if emb := memory.NewEmbedder(es, vs, nil, 8); emb != nil {
		t.Fatal("expected nil embedder for nil client")
	}
	// nil embedder methods must not panic
	var nilEmb *memory.Embedder
	nilEmb.Enqueue("x")
	_ = nilEmb.Backfill(context.Background())
	nilEmb.Close()
}

func TestEmbedder_BackfillEnqueuesMissing(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	for _, n := range []string{"a", "b", "c"} {
		if err := es.Upsert(ctx, &memory.Entry{Name: n, Content: n, CharCount: 1}); err != nil {
			t.Fatalf("upsert %s: %v", n, err)
		}
	}
	fc := &fakeClient{model: "m1"}
	emb := memory.NewEmbedder(es, vs, fc, 16)
	if err := emb.Backfill(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	emb.Close()

	vecs, _ := vs.LoadAllForModel(ctx, "m1")
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors after backfill, got %d", len(vecs))
	}
}

func TestEmbedder_ModelChangeReembeds(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "x", CharCount: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Embed with model m1.
	emb1 := memory.NewEmbedder(es, vs, &fakeClient{model: "m1"}, 8)
	_ = emb1.Backfill(ctx)
	emb1.Close()

	// New model m2: NamesMissingModel should report "a" as missing for m2.
	missing, err := vs.NamesMissingModel(ctx, "m2")
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if len(missing) != 1 || missing[0] != "a" {
		t.Fatalf("expected [a] missing for m2, got %v", missing)
	}
	emb2 := memory.NewEmbedder(es, vs, &fakeClient{model: "m2"}, 8)
	_ = emb2.Backfill(ctx)
	emb2.Close()
	if v, _ := vs.LoadAllForModel(ctx, "m2"); len(v) != 1 {
		t.Fatalf("expected re-embed under m2, got %d", len(v))
	}
}

func TestEmbedder_FailureIsNonFatal(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "x", CharCount: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	emb := memory.NewEmbedder(es, vs, &fakeClient{model: "m1", fail: true}, 8)
	emb.Enqueue("a")
	emb.Close() // must not panic despite embed error
	if v, _ := vs.LoadAllForModel(ctx, "m1"); len(v) != 0 {
		t.Fatalf("expected no vector on failure, got %d", len(v))
	}
}

type blockingEmbeddingClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingEmbeddingClient) Model() string { return "blocking-model" }

func (c *blockingEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	close(c.started)
	select {
	case <-c.release:
		return [][]float32{{1, 2, 3}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestEmbedderDoesNotRecreateVectorAfterConcurrentDelete(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "race", Content: "delete during embedding"}); err != nil {
		t.Fatal(err)
	}
	client := &blockingEmbeddingClient{started: make(chan struct{}), release: make(chan struct{})}
	emb := memory.NewEmbedder(es, vs, client, 1)
	emb.Enqueue("race")
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("embedding did not start")
	}
	if err := es.Delete(ctx, "race"); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	emb.Close()

	vectors, err := vs.LoadAllForModel(ctx, client.Model())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := vectors["race"]; exists {
		t.Fatal("in-flight embedder recreated an orphan vector after Delete returned")
	}
}

func TestEmbedderDoesNotPublishVectorAfterEvidenceTombstone(t *testing.T) {
	ctx := context.Background()
	es, db := newEntryStore(t)
	vs := memory.NewVectorStore(db)
	evidence, err := es.Ledger().AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "turn-race-evidence",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-race",
		Speaker:          "user",
		Ordinal:          0,
		Content:          "Alice moved to Berlin on Monday.",
		RecordedAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("append evidence: %v", err)
	}
	entry := &memory.Entry{Name: "race-evidence", Content: evidence[0].Content}
	if err := es.UpsertWithSources(ctx, entry, []memory.EvidenceRef{{
		EvidenceID: evidence[0].ID, SourceOrder: 0, FullSource: true,
	}}); err != nil {
		t.Fatalf("write sourced entry: %v", err)
	}
	if err := vs.Put(ctx, entry.Name, "blocking-model", []float32{9, 9, 9}, time.Now()); err != nil {
		t.Fatalf("seed prior vector: %v", err)
	}

	client := &blockingEmbeddingClient{started: make(chan struct{}), release: make(chan struct{})}
	emb := memory.NewEmbedder(es, vs, client, 1)
	emb.Enqueue(entry.Name)
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("embedding did not start")
	}
	if err := es.Ledger().Tombstone(ctx, memory.LifecycleRequest{
		EvidenceID: evidence[0].ID,
		RequestID:  "tombstone-race-evidence",
		ReasonCode: "user_delete",
	}); err != nil {
		t.Fatalf("tombstone evidence: %v", err)
	}
	close(client.release)
	emb.Close()

	vectors, err := vs.LoadAllForModel(ctx, client.Model())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := vectors[entry.Name]; exists {
		t.Fatal("in-flight embedder published a vector for a stale evidence projection")
	}
	var stored int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name = ?`, entry.Name).Scan(&stored); err != nil {
		t.Fatalf("count stale raw vector: %v", err)
	}
	if stored != 0 {
		t.Fatalf("tombstoned projection retained %d raw vector rows", stored)
	}
	hits, err := memory.NewRetriever(es, vs, nil).Search(ctx, "Alice Berlin", 1)
	if err != nil {
		t.Fatalf("search after tombstone: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("stale evidence projection remained answer-retrievable: %+v", hits)
	}
}

func TestEmbedderDoesNotPublishVectorAfterSameTimestampRewrite(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	fixed := time.Unix(1_700_000_000, 123_000).UTC()
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "race-rewrite", Content: "old content", UpdatedAt: fixed,
	}); err != nil {
		t.Fatal(err)
	}
	client := &blockingEmbeddingClient{started: make(chan struct{}), release: make(chan struct{})}
	emb := memory.NewEmbedder(es, vs, client, 1)
	emb.Enqueue("race-rewrite")
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("embedding did not start")
	}
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "race-rewrite", Content: "new content", UpdatedAt: fixed,
	}); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	emb.Close()

	vectors, err := vs.LoadAllForModel(ctx, client.Model())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := vectors["race-rewrite"]; exists {
		t.Fatal("in-flight embedder published a stale vector after same-timestamp rewrite")
	}
}
