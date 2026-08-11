package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// validateChunkRegime guards against reusing a persisted chunks store in a run
// that does not enable --chunks. The retriever never filters category="chunk",
// so stale verbatim chunks left in a reused store silently dominate the
// retrieval pool — a third regime that is neither pure-fact nor proper
// --chunks. The check must refuse that direction and allow every legitimate one.
func TestValidateChunkRegime(t *testing.T) {
	ctx := context.Background()
	newStore := func(t *testing.T) (*store.Store, *memory.EntryStore) {
		t.Helper()
		st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		return st, memory.NewEntryStore(st.DB())
	}

	t.Run("empty store reused without chunks is fine", func(t *testing.T) {
		st, _ := newStore(t)
		if err := validateChunkRegime(ctx, st.DB(), false, ":memory:"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty store built with chunks is fine", func(t *testing.T) {
		st, _ := newStore(t)
		if err := validateChunkRegime(ctx, st.DB(), true, ":memory:"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fact-only store reused without chunks is fine", func(t *testing.T) {
		st, es := newStore(t)
		if err := es.Upsert(ctx, &memory.Entry{Name: "fact-1", Content: "extracted fact", Category: "fact"}); err != nil {
			t.Fatal(err)
		}
		if err := validateChunkRegime(ctx, st.DB(), false, ":memory:"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("chunks store reused without chunks is refused", func(t *testing.T) {
		st, es := newStore(t)
		if err := es.Upsert(ctx, &memory.Entry{Name: "chunk-c0-s0-000", Content: "verbatim text", Category: "chunk"}); err != nil {
			t.Fatal(err)
		}
		err := validateChunkRegime(ctx, st.DB(), false, "/eval/stores")
		if err == nil {
			t.Fatal("expected refusal for a chunks store reused without --chunks")
		}
		if !strings.Contains(err.Error(), "/eval/stores") {
			t.Errorf("error should name the offending store path, got: %v", err)
		}
	})

	t.Run("chunks store reused WITH chunks is fine (idempotent ingest)", func(t *testing.T) {
		st, es := newStore(t)
		if err := es.Upsert(ctx, &memory.Entry{Name: "chunk-c0-s0-000", Content: "verbatim text", Category: "chunk"}); err != nil {
			t.Fatal(err)
		}
		if err := validateChunkRegime(ctx, st.DB(), true, ":memory:"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mixed fact+chunk store reused without chunks is refused", func(t *testing.T) {
		st, es := newStore(t)
		if err := es.Upsert(ctx, &memory.Entry{Name: "fact-1", Content: "extracted fact", Category: "fact"}); err != nil {
			t.Fatal(err)
		}
		if err := es.Upsert(ctx, &memory.Entry{Name: "chunk-c0-s0-000", Content: "verbatim text", Category: "chunk"}); err != nil {
			t.Fatal(err)
		}
		if err := validateChunkRegime(ctx, st.DB(), false, ":memory:"); err == nil {
			t.Fatal("expected refusal for a mixed store reused without --chunks")
		}
	})
}
