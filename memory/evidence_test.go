package memory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func newLedgerStore(t *testing.T) (*memory.LedgerStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return memory.NewLedgerStore(s.DB()), s.DB()
}

func TestLedgerAppendBatchIsAtomicAndRejectsInvalidUTF8(t *testing.T) {
	ledger, db := newLedgerStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	_, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{
		{
			ExternalSourceID: "turn-1",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "user",
			Ordinal:          0,
			Content:          "valid first turn",
			RecordedAt:       now,
		},
		{
			ExternalSourceID: "turn-2",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "assistant",
			Ordinal:          1,
			Content:          string([]byte{0xff}),
			RecordedAt:       now,
		},
	})
	if !errors.Is(err, memory.ErrInvalidEvidence) {
		t.Fatalf("invalid UTF-8 error = %v, want ErrInvalidEvidence", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence`).Scan(&count); err != nil {
		t.Fatalf("count evidence after rejected batch: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected batch persisted %d evidence rows, want 0", count)
	}
}

func TestLedgerAppendBatchExternalIDIdempotencyAndConflict(t *testing.T) {
	ledger, db := newLedgerStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	input := memory.EvidenceInput{
		ExternalSourceID: "turn-1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-a",
		Speaker:          "user",
		Ordinal:          0,
		Content:          "你好，世界 👋",
		RecordedAt:       now,
	}

	first, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{input})
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	second, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{input})
	if err != nil {
		t.Fatalf("idempotent append: %v", err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID == "" || first[0].ID != second[0].ID {
		t.Fatalf("idempotent append IDs = first=%+v second=%+v", first, second)
	}
	if first[0].ContentDigest == "" || first[0].State != memory.EvidenceActive {
		t.Fatalf("first evidence not active/digested: %+v", first[0])
	}

	conflicting := input
	conflicting.Content = "same external source, changed payload"
	newEvidence := input
	newEvidence.ExternalSourceID = "turn-2"
	newEvidence.Ordinal = 1
	newEvidence.Content = "must roll back with the conflicting input"
	if _, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{newEvidence, conflicting}); !errors.Is(err, memory.ErrEvidenceConflict) {
		t.Fatalf("conflicting append error = %v, want ErrEvidenceConflict", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence`).Scan(&count); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if count != 1 {
		t.Fatalf("conflicting batch changed evidence count to %d, want 1", count)
	}
}

func TestLedgerGetAndListSessionUseStableMessageOrder(t *testing.T) {
	ledger, _ := newLedgerStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	inputs := []memory.EvidenceInput{
		{
			ExternalSourceID: "turn-3",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "assistant",
			Ordinal:          2,
			Content:          "third",
			RecordedAt:       now.Add(2 * time.Second),
		},
		{
			ExternalSourceID: "turn-1",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "user",
			Ordinal:          0,
			Content:          "first",
			RecordedAt:       now,
		},
		{
			ExternalSourceID: "turn-2",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "assistant",
			Ordinal:          1,
			Content:          "second",
			RecordedAt:       now.Add(time.Second),
		},
	}
	appended, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("append ordered messages: %v", err)
	}
	ordered, err := ledger.ListSession(ctx, "session-a", false)
	if err != nil {
		t.Fatalf("list session: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("session evidence count = %d, want 3", len(ordered))
	}
	for index, evidence := range ordered {
		if evidence.Ordinal != index {
			t.Fatalf("ordered[%d].Ordinal = %d, want %d", index, evidence.Ordinal, index)
		}
		if evidence.State != memory.EvidenceActive {
			t.Fatalf("ordered[%d] state = %q, want active", index, evidence.State)
		}
	}

	got, err := ledger.Get(ctx, appended[0].ID)
	if err != nil {
		t.Fatalf("get evidence: %v", err)
	}
	if got.ID != appended[0].ID || got.Content != "third" || got.State != memory.EvidenceActive {
		t.Fatalf("get evidence = %+v, want third active evidence", got)
	}
	many, err := ledger.GetMany(ctx, []string{appended[2].ID, appended[0].ID, appended[1].ID})
	if err != nil {
		t.Fatalf("get many evidence: %v", err)
	}
	if len(many) != 3 || many[appended[1].ID].Content != "first" {
		t.Fatalf("get many evidence = %+v, want all active sources", many)
	}
}

func TestLedgerRejectsInvalidMessageShape(t *testing.T) {
	ledger, _ := newLedgerStore(t)
	_, err := ledger.AppendBatch(context.Background(), []memory.EvidenceInput{{
		SourceType: memory.EvidenceMessage,
		Content:    "missing session, speaker, and timestamp",
	}})
	if !errors.Is(err, memory.ErrInvalidEvidence) {
		t.Fatalf("invalid message error = %v, want ErrInvalidEvidence", err)
	}
}

func TestLedgerTombstoneAndRestoreChangeSourceAvailability(t *testing.T) {
	ledger, _ := newLedgerStore(t)
	ctx := context.Background()
	appended, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "turn-1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-a",
		Speaker:          "user",
		Ordinal:          0,
		Content:          "a source that can be restored",
		RecordedAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("append source: %v", err)
	}
	req := memory.LifecycleRequest{EvidenceID: appended[0].ID, RequestID: "delete-1", ReasonCode: "user_delete"}
	if err := ledger.Tombstone(ctx, req); err != nil {
		t.Fatalf("tombstone source: %v", err)
	}
	if _, err := ledger.Get(ctx, appended[0].ID); !errors.Is(err, memory.ErrEvidenceUnavailable) {
		t.Fatalf("get tombstoned source error = %v, want ErrEvidenceUnavailable", err)
	}
	activeOnly, err := ledger.ListSession(ctx, "session-a", false)
	if err != nil {
		t.Fatalf("list active evidence: %v", err)
	}
	if len(activeOnly) != 0 {
		t.Fatalf("active session list includes tombstoned evidence: %+v", activeOnly)
	}
	withTombstones, err := ledger.ListSession(ctx, "session-a", true)
	if err != nil {
		t.Fatalf("list tombstoned evidence: %v", err)
	}
	if len(withTombstones) != 1 || withTombstones[0].State != memory.EvidenceTombstoned {
		t.Fatalf("tombstoned session list = %+v", withTombstones)
	}
	if err := ledger.Restore(ctx, memory.LifecycleRequest{
		EvidenceID: appended[0].ID,
		RequestID:  "restore-1",
		ReasonCode: "user_restore",
	}); err != nil {
		t.Fatalf("restore source: %v", err)
	}
	restored, err := ledger.Get(ctx, appended[0].ID)
	if err != nil {
		t.Fatalf("get restored source: %v", err)
	}
	if restored.State != memory.EvidenceActive || restored.Revision != 3 {
		t.Fatalf("restored evidence = %+v, want active revision 3", restored)
	}
}

func TestLedgerLifecycleInvalidatesProjectionAndPurgeDeletesDirectClosure(t *testing.T) {
	ledger, db := newLedgerStore(t)
	entries := memory.NewEntryStore(db)
	ctx := context.Background()
	appended, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "turn-1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-a",
		Speaker:          "user",
		Ordinal:          0,
		Content:          "Alice moved to Berlin on Monday.",
		RecordedAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("append source: %v", err)
	}
	fact := &memory.Entry{Name: "alice-move", Content: "Alice moved to Berlin on Monday."}
	if err := entries.UpsertWithSources(ctx, fact, []memory.EvidenceRef{{EvidenceID: appended[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatalf("write sourced fact: %v", err)
	}
	if err := ledger.Tombstone(ctx, memory.LifecycleRequest{EvidenceID: appended[0].ID, RequestID: "tombstone-1", ReasonCode: "user_delete"}); err != nil {
		t.Fatalf("tombstone source: %v", err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `
		SELECT state FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, fact.ID).Scan(&state); err != nil {
		t.Fatalf("read stale projection: %v", err)
	}
	if state != "stale" {
		t.Fatalf("projection state after tombstone = %q, want stale", state)
	}
	if err := ledger.Restore(ctx, memory.LifecycleRequest{EvidenceID: appended[0].ID, RequestID: "restore-1", ReasonCode: "user_restore"}); err != nil {
		t.Fatalf("restore source: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT state FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, fact.ID).Scan(&state); err != nil {
		t.Fatalf("read stale projection after restore: %v", err)
	}
	if state != "stale" {
		t.Fatalf("restore reactivated projection state %q, want stale", state)
	}

	purged, err := ledger.Purge(ctx, memory.LifecycleRequest{EvidenceID: appended[0].ID, RequestID: "purge-1", ReasonCode: "privacy_purge"})
	if err != nil {
		t.Fatalf("purge source: %v", err)
	}
	if !purged.Purged || purged.CheckpointPending {
		t.Fatalf("purge result = %+v, want complete purge", purged)
	}
	if _, err := ledger.Get(ctx, appended[0].ID); !errors.Is(err, memory.ErrEvidencePurged) {
		t.Fatalf("get purged source error = %v, want ErrEvidencePurged", err)
	}
	if _, err := entries.GetByName(ctx, fact.Name); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("purge retained direct fact projection: %v", err)
	}
	var evidenceCount, projectionCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence WHERE id = ?`, appended[0].ID).Scan(&evidenceCount); err != nil {
		t.Fatalf("count purged content: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, fact.ID).Scan(&projectionCount); err != nil {
		t.Fatalf("count purged projection: %v", err)
	}
	if evidenceCount != 0 || projectionCount != 0 {
		t.Fatalf("purge closure left evidence=%d projections=%d", evidenceCount, projectionCount)
	}
	var secureDelete int
	if err := db.QueryRowContext(ctx, `PRAGMA secure_delete`).Scan(&secureDelete); err != nil {
		t.Fatalf("read secure_delete pragma: %v", err)
	}
	if secureDelete != 1 {
		t.Fatalf("secure_delete = %d, want 1 after purge", secureDelete)
	}
}
