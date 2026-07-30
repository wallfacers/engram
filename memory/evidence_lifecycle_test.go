package memory_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func TestLedgerPurgeReportsAndRetriesBusyWALCheckpoint(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "evidence-lifecycle.db")
	opened, err := store.Open(ctx, store.Options{DSN: dsn, BusyTimeoutMs: 50})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	ledger := memory.NewLedgerStore(opened.DB())
	appended, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "turn-checkpoint",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-checkpoint",
		Speaker:          "user",
		Ordinal:          0,
		Content:          "source retained by a concurrent WAL reader",
		RecordedAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := reader.ExecContext(ctx, `PRAGMA busy_timeout = 50`); err != nil {
		t.Fatal(err)
	}
	readerTx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := readerTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence`).Scan(&before); err != nil {
		_ = readerTx.Rollback()
		t.Fatal(err)
	}
	if before != 1 {
		_ = readerTx.Rollback()
		t.Fatalf("reader snapshot count = %d, want 1", before)
	}

	first, err := ledger.Purge(ctx, memory.LifecycleRequest{
		EvidenceID: appended[0].ID, RequestID: "purge-checkpoint", ReasonCode: "privacy_purge",
	})
	if !errors.Is(err, memory.ErrPurgeIncomplete) {
		_ = readerTx.Rollback()
		t.Fatalf("purge error = %v, want ErrPurgeIncomplete", err)
	}
	if !first.Purged || !first.CheckpointPending {
		_ = readerTx.Rollback()
		t.Fatalf("first purge result = %+v, want logically purged checkpoint pending", first)
	}
	if _, err := ledger.Get(ctx, appended[0].ID); !errors.Is(err, memory.ErrEvidencePurged) {
		_ = readerTx.Rollback()
		t.Fatalf("logical purge state = %v, want ErrEvidencePurged", err)
	}
	if err := readerTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	second, err := ledger.Purge(ctx, memory.LifecycleRequest{
		EvidenceID: appended[0].ID, RequestID: "purge-checkpoint", ReasonCode: "privacy_purge",
	})
	if err != nil {
		t.Fatalf("retry purge checkpoint: %v", err)
	}
	if !second.Purged || second.CheckpointPending {
		t.Fatalf("retry purge result = %+v, want completed checkpoint", second)
	}
	rows, err := opened.DB().QueryContext(ctx, `PRAGMA table_info(memory_evidence_events)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"content", "speaker", "session", "digest", "span"} {
			if strings.Contains(strings.ToLower(name), forbidden) {
				t.Fatalf("audit event table exposes forbidden source field %q", name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
