package memory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/wallfacers/engram/store"
)

// US2 hit-time neighbor extension (024). TDD: these tests pin the bounded
// depth-1 sibling-fact query that extends answer context with shared-evidence
// facts after candidate freeze (spec FR-006/FR-007).

func newSiblingStore(t *testing.T) (*LedgerStore, *ProjectionStore, *sql.DB) {
	t.Helper()
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewLedgerStore(s.DB()), NewProjectionStore(s.DB()), s.DB()
}

// seedFactProjection writes one atomic-fact projection supported by the given
// evidence IDs, returning its projection ID.
func seedFactProjection(t *testing.T, db *sql.DB, id, objectKey string, evidenceIDs ...string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO memory_projections(
			id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
		) VALUES (?, 'atomic_fact', ?, 'active', 'test', '1', 'test-config', 1, 1)`,
		id, objectKey); err != nil {
		t.Fatalf("insert projection %q: %v", id, err)
	}
	for order, evidenceID := range evidenceIDs {
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO memory_projection_sources(
				projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
			) VALUES (?, ?, ?, 1, NULL, NULL, NULL, 'supports')`,
			id, order, evidenceID); err != nil {
			t.Fatalf("insert projection source %q/%q: %v", id, evidenceID, err)
		}
	}
}

func appendSiblingEvidence(t *testing.T, ledger *LedgerStore, externalID, content string) Evidence {
	t.Helper()
	evidence, err := ledger.AppendBatch(context.Background(), []EvidenceInput{{
		ExternalSourceID: externalID,
		SourceType:       EvidenceMessage,
		SourceSessionID:  "session-a",
		Speaker:          "user",
		Ordinal:          0,
		Content:          content,
		RecordedAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("append evidence: %v", err)
	}
	return evidence[0]
}

func TestSiblingFactsSharedEvidence(t *testing.T) {
	// T013: two facts share evidence → hitting one surfaces the sibling.
	ctx := context.Background()
	ledger, projections, db := newSiblingStore(t)
	evidence := appendSiblingEvidence(t, ledger, "turn-1", "meeting moved to Monday")
	other := appendSiblingEvidence(t, ledger, "turn-2", "unrelated grocery list")
	seedFactProjection(t, db, "p-meeting", "entry-meeting", evidence.ID)
	seedFactProjection(t, db, "p-moved", "entry-moved", evidence.ID)
	seedFactProjection(t, db, "p-grocery", "entry-grocery", other.ID)

	siblings, err := projections.SiblingFacts(ctx, []string{"p-meeting"}, 8)
	if err != nil {
		t.Fatalf("sibling lookup: %v", err)
	}
	// Only the shared-evidence fact is a sibling; the unrelated one is not.
	if len(siblings) != 1 || siblings[0].ID != "p-moved" {
		t.Fatalf("siblings of p-meeting = %+v, want [p-moved]", siblings)
	}
}

func TestSiblingFactsNoNeighborIsEmpty(t *testing.T) {
	// T014: no shared-evidence sibling → empty result (zero change vs off).
	ctx := context.Background()
	ledger, projections, db := newSiblingStore(t)
	a := appendSiblingEvidence(t, ledger, "turn-1", "alpha")
	b := appendSiblingEvidence(t, ledger, "turn-2", "beta")
	seedFactProjection(t, db, "p-alpha", "entry-alpha", a.ID)
	seedFactProjection(t, db, "p-beta", "entry-beta", b.ID)

	siblings, err := projections.SiblingFacts(ctx, []string{"p-alpha"}, 8)
	if err != nil {
		t.Fatalf("sibling lookup: %v", err)
	}
	if len(siblings) != 0 {
		t.Fatalf("siblings of p-alpha = %+v, want empty (no shared evidence)", siblings)
	}
}

func TestSiblingFactsBoundedAndDeterministic(t *testing.T) {
	// T015: sibling count is bounded (no unbounded candidate/token growth) and
	// the ordering is deterministic (evidence order → projection id).
	ctx := context.Background()
	ledger, projections, db := newSiblingStore(t)
	shared := appendSiblingEvidence(t, ledger, "turn-1", "shared source")
	// Five siblings all sharing the same evidence with p-root.
	for i := 0; i < 5; i++ {
		seedFactProjection(t, db, "p-sib-"+string(rune('a'+i)), "entry-sib-"+string(rune('a'+i)), shared.ID)
	}
	seedFactProjection(t, db, "p-root", "entry-root", shared.ID)

	bounded, err := projections.SiblingFacts(ctx, []string{"p-root"}, 3)
	if err != nil {
		t.Fatalf("bounded sibling lookup: %v", err)
	}
	if len(bounded) != 3 {
		t.Fatalf("bounded siblings = %d, want 3 (max limit)", len(bounded))
	}

	// Deterministic: repeated calls return the same order for the same window.
	again, err := projections.SiblingFacts(ctx, []string{"p-root"}, 3)
	if err != nil {
		t.Fatalf("repeat sibling lookup: %v", err)
	}
	if len(again) != len(bounded) {
		t.Fatalf("repeat sibling count = %d, want %d", len(again), len(bounded))
	}
	for i := range bounded {
		if bounded[i].ID != again[i].ID {
			t.Fatalf("sibling order unstable: call1[%d]=%q call2[%d]=%q", i, bounded[i].ID, i, again[i].ID)
		}
	}
}

func TestSiblingFactsExcludesSelfAndDisabled(t *testing.T) {
	// Siblings must exclude the queried projections themselves and must not
	// surface disabled/stale views.
	ctx := context.Background()
	ledger, projections, db := newSiblingStore(t)
	shared := appendSiblingEvidence(t, ledger, "turn-1", "shared")
	seedFactProjection(t, db, "p-self", "entry-self", shared.ID)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memory_projections(
			id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
		) VALUES ('p-disabled', 'atomic_fact', 'entry-disabled', 'disabled', 'test', '1', 'c', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memory_projection_sources(
			projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
		) VALUES ('p-disabled', 0, ?, 1, NULL, NULL, NULL, 'supports')`, shared.ID); err != nil {
		t.Fatal(err)
	}

	siblings, err := projections.SiblingFacts(ctx, []string{"p-self"}, 8)
	if err != nil {
		t.Fatalf("sibling lookup: %v", err)
	}
	if len(siblings) != 0 {
		t.Fatalf("siblings = %+v, want empty (self + disabled excluded)", siblings)
	}
}
