package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func TestChunkUpsertAndRetrieve(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	conv := conversation{ID: 0, Sessions: []session{{
		Index: 1,
		Date:  time.Date(2023, 5, 8, 0, 0, 0, 0, time.UTC),
		Turns: []turn{{Speaker: "Caroline", Text: "I adopted a golden retriever named Max."}},
	}}}
	_, n, err := ingestChunks(ctx, es, conv)
	if err != nil || n != 1 {
		t.Fatalf("ingestChunks = %d, %v", n, err)
	}
	vectors := memory.NewVectorStore(st.DB())
	if err := vectors.Put(ctx, "chunk-c0-s1-000", "fixture", []float32{1, 0}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// idempotent re-run
	if _, n, err = ingestChunks(ctx, es, conv); err != nil || n != 1 {
		t.Fatalf("re-run ingestChunks = %d, %v", n, err)
	}
	if got := chunkEmbeddingCount(t, ctx, st, "chunk-c0-s1-000"); got != 1 {
		t.Fatalf("unchanged chunk embedding count = %d, want 1", got)
	}
	r := memory.NewRetriever(es, vectors, nil)
	hits, err := r.Search(ctx, "golden retriever", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("Search = %d hits, %v", len(hits), err)
	}
	if hits[0].EventDate == nil || hits[0].EventDate.Day() != 8 {
		t.Errorf("chunk EventDate not surfaced: %+v", hits[0])
	}
}

func TestChunkIngestReconcilesChangedAndObsoletePersistedChunks(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	for _, entry := range []*memory.Entry{
		{Name: "chunk-c0-s1-000", Content: "legacy truncated content", Category: "chunk", SourceSessionID: "conv0-sess1"},
		{Name: "chunk-c0-s1-999", Content: "obsolete chunk", Category: "chunk", SourceSessionID: "conv0-sess1"},
	} {
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
		if err := vectors.Put(ctx, entry.Name, "fixture", []float32{1, 0}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	conv := conversation{ID: 0, Sessions: []session{{
		Index: 1,
		Turns: []turn{{
			Speaker: "assistant",
			Text:    strings.Repeat("context ", 180) + "ANSWER_AFTER_THE_OLD_CAP",
			DiaID:   "D1:1",
		}},
	}}}
	_, n, err := ingestChunks(ctx, es, conv)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("ingested chunks = %d, want at least 2 for oversized turn", n)
	}
	if got := chunkEmbeddingCount(t, ctx, st, "chunk-c0-s1-000"); got != 0 {
		t.Fatalf("changed chunk retained %d stale embedding rows, want 0", got)
	}
	if got := chunkEmbeddingCount(t, ctx, st, "chunk-c0-s1-999"); got != 0 {
		t.Fatalf("obsolete chunk retained %d stale embedding rows, want 0", got)
	}
	if _, err := es.GetByName(ctx, "chunk-c0-s1-999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("obsolete chunk lookup error = %v, want store.ErrNotFound", err)
	}
}

func chunkEmbeddingCount(t *testing.T, ctx context.Context, st *store.Store, name string) int {
	t.Helper()
	var count int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name = ?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
