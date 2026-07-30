package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/wallfacers/engram/store"
)

func newProjectionStore(t *testing.T) (*LedgerStore, *ProjectionStore, *sql.DB) {
	t.Helper()
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewLedgerStore(s.DB()), NewProjectionStore(s.DB()), s.DB()
}

func appendProjectionEvidence(t *testing.T, ledger *LedgerStore, externalID, content string) Evidence {
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

func TestProjectionSourcesBatchLookupAndStaleOnlyActiveViews(t *testing.T) {
	ctx := context.Background()
	ledger, projections, db := newProjectionStore(t)
	first := appendProjectionEvidence(t, ledger, "turn-1", "first source")
	second := appendProjectionEvidence(t, ledger, "turn-2", "second source")

	for _, projection := range []struct {
		id, key, state string
	}{
		{id: "projection-active", key: "entry-active", state: "active"},
		{id: "projection-disabled", key: "entry-disabled", state: "disabled"},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memory_projections(
				id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
			) VALUES (?, 'atomic_fact', ?, ?, 'test', '1', 'test-config', 1, 1)`,
			projection.id, projection.key, projection.state); err != nil {
			t.Fatalf("insert projection %q: %v", projection.id, err)
		}
	}
	for _, source := range []struct {
		projectionID string
		order        int
		evidenceID   string
	}{
		{projectionID: "projection-active", order: 0, evidenceID: first.ID},
		{projectionID: "projection-active", order: 1, evidenceID: second.ID},
		{projectionID: "projection-disabled", order: 0, evidenceID: first.ID},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memory_projection_sources(
				projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
			) VALUES (?, ?, ?, 1, NULL, NULL, NULL, 'supports')`,
			source.projectionID, source.order, source.evidenceID); err != nil {
			t.Fatalf("insert projection source: %v", err)
		}
	}

	sources, err := projections.SourcesByProjectionIDs(ctx, []string{"projection-disabled", "projection-active"})
	if err != nil {
		t.Fatalf("batch source lookup: %v", err)
	}
	activeSources := sources["projection-active"]
	if len(activeSources) != 2 || activeSources[0].EvidenceID != first.ID || activeSources[0].SourceOrder != 0 || !activeSources[0].FullSource || activeSources[1].EvidenceID != second.ID || activeSources[1].SourceOrder != 1 {
		t.Fatalf("active direct lineage = %+v", activeSources)
	}

	if err := projections.MarkStaleByEvidenceIDs(ctx, []string{first.ID}); err != nil {
		t.Fatalf("mark stale by source: %v", err)
	}
	states := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT id, state FROM memory_projections ORDER BY id`)
	if err != nil {
		t.Fatalf("read projection states: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatalf("scan projection state: %v", err)
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate projection states: %v", err)
	}
	if states["projection-active"] != "stale" || states["projection-disabled"] != "disabled" {
		t.Fatalf("projection states after source invalidation = %+v, want stale/disabled", states)
	}
}

func TestValidateEvidenceRefUsesUnicodeCodePointsAndExactDigest(t *testing.T) {
	content := "A你😊Z"
	span := "你😊"
	digest := sha256.Sum256([]byte(span))
	start, end := 1, 3
	ref := EvidenceRef{
		EvidenceID:  "evidence-1",
		SourceOrder: 0,
		StartChar:   &start,
		EndChar:     &end,
		SpanDigest:  fmt.Sprintf("%x", digest[:]),
	}
	if err := validateEvidenceRef(content, ref); err != nil {
		t.Fatalf("validate Unicode code-point span: %v", err)
	}

	badEnd := 4 // byte-ish endpoints must not bypass the code-point contract.
	ref.EndChar = &badEnd
	if err := validateEvidenceRef(content, ref); err == nil {
		t.Fatal("out-of-span digest unexpectedly accepted")
	}
	ref.FullSource = true
	if err := validateEvidenceRef(content, ref); err == nil {
		t.Fatal("full source with span fields unexpectedly accepted")
	}
}
