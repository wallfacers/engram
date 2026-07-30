package memory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

func TestEvidenceLifecycleSurvivesIngestMergeAndPurgeClosure(t *testing.T) {
	ctx := context.Background()
	opened, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	entries := memory.NewEntryStore(opened.DB())
	ledger := entries.Ledger()
	p := pipeline.New(pipeline.Config{
		Entries: entries,
		Budgets: memory.DefaultBudgets(),
		Call: func(_ context.Context, _, user string) (string, error) {
			ids := integrationPromptSourceIDs(user)
			if len(ids) != 2 {
				return "", fmt.Errorf("prompt source IDs = %v, want two", ids)
			}
			return fmt.Sprintf(`{"facts":[
				{"fact":"Alice moved to Berlin.","source_ids":[%q]},
				{"fact":"Alice moved on Monday.","source_ids":[%q]}
			]}`, ids[0], ids[1]), nil
		},
	})
	result, err := p.IngestDetailed(ctx, time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC), "session-integration", []pipeline.Message{
		{ExternalSourceID: "turn-1", Role: "user", Text: "Alice moved to Berlin.", Ordinal: 0},
		{ExternalSourceID: "turn-2", Role: "assistant", Text: "The move happened on Monday.", Ordinal: 1},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(result.Evidence) != 2 || len(result.Entries) != 2 {
		t.Fatalf("ingest result = %+v, want two Evidence and two facts", result)
	}
	merged := &memory.Entry{Name: "alice-move", Content: "Alice moved to Berlin on Monday."}
	if err := entries.Merge(ctx, []string{result.Entries[0].Name, result.Entries[1].Name}, merged); err != nil {
		t.Fatalf("merge: %v", err)
	}
	refs, err := entries.SourceRefs(ctx, merged.ID)
	if err != nil {
		t.Fatalf("merged source refs: %v", err)
	}
	if len(refs) != 2 || refs[0].EvidenceID != result.Evidence[0].ID || refs[1].EvidenceID != result.Evidence[1].ID {
		t.Fatalf("merged source refs = %+v", refs)
	}

	for index, source := range result.Evidence {
		if err := ledger.Tombstone(ctx, memory.LifecycleRequest{
			EvidenceID: source.ID, RequestID: fmt.Sprintf("tombstone-%d", index), ReasonCode: "user_delete",
		}); err != nil {
			t.Fatalf("tombstone %d: %v", index, err)
		}
	}
	retriever := memory.NewRetriever(entries, memory.NewVectorStore(opened.DB()), nil)
	hits, err := retriever.Search(ctx, "Alice Berlin", 5)
	if err != nil {
		t.Fatalf("search after both tombstones: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("tombstoned projection remained retrievable: %+v", hits)
	}
	if err := ledger.Restore(ctx, memory.LifecycleRequest{
		EvidenceID: result.Evidence[0].ID, RequestID: "restore-0", ReasonCode: "user_restore",
	}); err != nil {
		t.Fatalf("restore source: %v", err)
	}
	if _, err := ledger.Get(ctx, result.Evidence[0].ID); err != nil {
		t.Fatalf("restored source unavailable: %v", err)
	}
	hits, err = retriever.Search(ctx, "Alice Berlin", 5)
	if err != nil {
		t.Fatalf("search after restore: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("restore revived stale projection without a rebuild: %+v", hits)
	}

	purged, err := ledger.Purge(ctx, memory.LifecycleRequest{
		EvidenceID: result.Evidence[0].ID, RequestID: "purge-0", ReasonCode: "privacy_purge",
	})
	if err != nil || !purged.Purged || purged.CheckpointPending {
		t.Fatalf("purge result = %+v, err=%v", purged, err)
	}
	if _, err := entries.GetByName(ctx, merged.Name); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("purge retained merged projection: %v", err)
	}
	if _, err := ledger.Get(ctx, result.Evidence[0].ID); !errors.Is(err, memory.ErrEvidencePurged) {
		t.Fatalf("purged source state = %v, want ErrEvidencePurged", err)
	}
	remaining, err := ledger.Get(ctx, result.Evidence[1].ID)
	if !errors.Is(err, memory.ErrEvidenceUnavailable) {
		t.Fatalf("unrestored second source state = %v, want ErrEvidenceUnavailable", err)
	}
	if remaining != nil {
		t.Fatalf("tombstoned source unexpectedly returned content: %+v", remaining)
	}
}

func integrationPromptSourceIDs(user string) []string {
	const marker = "source_id="
	var ids []string
	for rest := user; ; {
		start := strings.Index(rest, marker)
		if start < 0 {
			return ids
		}
		rest = rest[start+len(marker):]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ids
		}
		ids = append(ids, rest[:end])
		rest = rest[end+1:]
	}
}
