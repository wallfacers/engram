package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/wallfacers/engram/store"
)

func TestMigration_FreshDB(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open fresh db: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.DB().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 7 {
		t.Fatalf("fresh schema version = %d, want 7", version)
	}
	for _, table := range []string{
		"memory_evidence",
		"memory_evidence_events",
		"memory_evidence_heads",
		"memory_projections",
		"memory_projection_sources",
		"memory_semantic_episodes",
	} {
		var count int
		if err := s.DB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("v7 table %q missing", table)
		}
	}
}

func TestMigration_IdempotentRerun(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	var version int
	if err := s.DB().QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 7 {
		t.Errorf("expected version 7 after first open, got %d", version)
	}
	s.Close()

	s2, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}

func TestMigration_MemoryHybrid(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	db := s.DB()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_entries(id, name, content, created_at, updated_at, event_date, fact_source)
		 VALUES ('id1','alpha','hello',0,0,123456,'extraction')`); err != nil {
		t.Fatalf("insert with new columns: %v", err)
	}
	var evt sql.NullInt64
	var src string
	if err := db.QueryRowContext(ctx,
		`SELECT event_date, fact_source FROM memory_entries WHERE name='alpha'`).Scan(&evt, &src); err != nil {
		t.Fatalf("read new columns: %v", err)
	}
	if !evt.Valid || evt.Int64 != 123456 || src != "extraction" {
		t.Fatalf("new columns: got event_date=%v fact_source=%q", evt, src)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at)
		 VALUES ('alpha','m',2,x'0000',0)`); err != nil {
		t.Fatalf("insert embedding: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_entities(entry_name, entity_norm, entity_raw)
		 VALUES ('alpha','sweden','Sweden')`); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	var cnt int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_entities WHERE entity_norm='sweden'`).Scan(&cnt); err != nil {
		t.Fatalf("query entity index: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("entity index count: got %d, want 1", cnt)
	}
}

func TestMigration_V3RoundTrip(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "migration.db")
	s, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("open v2 database: %v", err)
	}

	var version int
	if err := s.DB().QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 7 {
		t.Fatalf("expected migration v7, got v%d", version)
	}

	db := s.DB()
	for _, table := range []string{"memory_entity_edges", "memory_event_aliases", "memory_event_aliases_fts"} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'view') AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %q missing", table)
		}
	}
	for _, column := range []string{"event_start", "event_end", "superseded_by", "revision"} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('memory_entries') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatalf("check column %q: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("column %q missing", column)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_event_aliases(entry_name, alias) VALUES ('alpha', 'fitness tracker')`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	var indexed string
	if err := db.QueryRowContext(ctx,
		`SELECT alias FROM memory_event_aliases_fts WHERE memory_event_aliases_fts MATCH 'fitness'`).Scan(&indexed); err != nil {
		t.Fatalf("alias FTS trigger: %v", err)
	}
	if indexed != "fitness tracker" {
		t.Fatalf("indexed alias = %q, want %q", indexed, "fitness tracker")
	}

	// Apply the v4/v3 Down contracts, then reopen so normal migration logic
	// upgrades the same v2 database back to the current schema.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS memory_semantic_episodes`,
		`DROP TABLE IF EXISTS memory_projection_sources`,
		`DROP TABLE IF EXISTS memory_projections`,
		`DROP TABLE IF EXISTS memory_evidence_heads`,
		`DROP TABLE IF EXISTS memory_evidence_events`,
		`DROP TABLE IF EXISTS memory_evidence`,
		`DELETE FROM schema_version WHERE version = 7`,
		`ALTER TABLE memory_entries DROP COLUMN revision`,
		`DELETE FROM schema_version WHERE version = 6`,
		`DROP TABLE IF EXISTS memory_fact_queries`,
		`DELETE FROM schema_version WHERE version = 5`,
		`DROP INDEX IF EXISTS idx_memory_entries_event_end`,
		`DROP INDEX IF EXISTS idx_memory_entries_event_start`,
		`DELETE FROM schema_version WHERE version = 4`,
		`DROP TRIGGER IF EXISTS memory_event_aliases_fts_au`,
		`DROP TRIGGER IF EXISTS memory_event_aliases_fts_ad`,
		`DROP TRIGGER IF EXISTS memory_event_aliases_fts_ai`,
		`DROP TABLE IF EXISTS memory_event_aliases_fts`,
		`DROP TABLE IF EXISTS memory_event_aliases`,
		`DROP INDEX IF EXISTS idx_entity_edges_b`,
		`DROP INDEX IF EXISTS idx_entity_edges_a`,
		`DROP TABLE IF EXISTS memory_entity_edges`,
		`ALTER TABLE memory_entries DROP COLUMN superseded_by`,
		`ALTER TABLE memory_entries DROP COLUMN event_end`,
		`ALTER TABLE memory_entries DROP COLUMN event_start`,
		`DELETE FROM schema_version WHERE version = 3`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply v3 down %q: %v", stmt, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close v2 database: %v", err)
	}

	s, err = store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("reopen v2 database: %v", err)
	}
	defer s.Close()
	if err := s.DB().QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("read upgraded schema version: %v", err)
	}
	if version != 7 {
		t.Fatalf("expected migration v7 after round trip, got v%d", version)
	}
}

func TestMigration_V5FactQueries(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	db := s.DB()

	var version int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 7 {
		t.Fatalf("expected migration v7, got v%d", version)
	}

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memory_fact_queries'`).Scan(&count); err != nil {
		t.Fatalf("check memory_fact_queries: %v", err)
	}
	if count != 1 {
		t.Fatalf("memory_fact_queries table missing")
	}

	// Composite PK (entry_name, query): same fact + distinct queries coexist;
	// duplicate (entry_name, query) is ignored.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_fact_queries(entry_name, query) VALUES ('alpha','when did it happen?')`); err != nil {
		t.Fatalf("insert query: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_fact_queries(entry_name, query) VALUES ('alpha','who did it?')`); err != nil {
		t.Fatalf("insert second query: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO memory_fact_queries(entry_name, query) VALUES ('alpha','when did it happen?')`); err != nil {
		t.Fatalf("insert dup query: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_fact_queries WHERE entry_name = 'alpha'`).Scan(&count); err != nil {
		t.Fatalf("count queries: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 distinct queries for alpha, got %d", count)
	}
}

func TestMigration_V6EntryRevision(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO memory_entries(id, name, content, created_at, updated_at)
		 VALUES ('id-revision','revision-entry','hello',1,1)`); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	var revision int64
	if err := s.DB().QueryRowContext(ctx,
		`SELECT revision FROM memory_entries WHERE name = 'revision-entry'`).Scan(&revision); err != nil {
		t.Fatalf("read revision: %v", err)
	}
	if revision != 1 {
		t.Fatalf("revision = %d, want default 1", revision)
	}
}

func TestMigration_TemporalEventIndexes(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	for _, index := range []string{"idx_memory_entries_event_start", "idx_memory_entries_event_end"} {
		var count int
		if err := s.DB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatalf("check index %q: %v", index, err)
		}
		if count != 1 {
			t.Fatalf("index %q missing", index)
		}
	}
}

func TestMigration_V7BackfillsV6EntriesWithoutChangingExistingIndexes(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "v6-to-v7.db")
	s, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("open current schema: %v", err)
	}
	downgradeV7ForMigrationTest(t, ctx, s.DB())
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO memory_entries(
			id, name, trigger, content, created_at, updated_at, source_session_id
		) VALUES ('entry-v6', 'legacy-name', 'legacy trigger', 'legacy evidence', 11, 12, 'session-v6')`); err != nil {
		t.Fatalf("insert v6 entry: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO memory_entity_edges(entity_a, entity_b, kind, weight, updated_at)
		VALUES ('alice', 'bob', 'co', 1, 13)`); err != nil {
		t.Fatalf("insert 003 edge: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close v6 fixture: %v", err)
	}

	s, err = store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("upgrade v6 fixture: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.DB().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 7 {
		t.Fatalf("schema version = %d, want 7", version)
	}

	var evidenceID, sourceType, sessionID, content string
	if err := s.DB().QueryRowContext(ctx, `
		SELECT id, source_type, source_session_id, content
		FROM memory_evidence WHERE id = 'legacy:entry-v6'`).Scan(
		&evidenceID, &sourceType, &sessionID, &content,
	); err != nil {
		t.Fatalf("read backfilled evidence: %v", err)
	}
	if evidenceID != "legacy:entry-v6" || sourceType != "legacy_entry" || sessionID != "session-v6" || content != "legacy evidence" {
		t.Fatalf("unexpected backfilled evidence: id=%q type=%q session=%q content=%q", evidenceID, sourceType, sessionID, content)
	}

	var state, action string
	if err := s.DB().QueryRowContext(ctx, `
		SELECT h.state, e.action
		FROM memory_evidence_heads AS h
		JOIN memory_evidence_events AS e ON e.evidence_id = h.evidence_id
		WHERE h.evidence_id = 'legacy:entry-v6'`).Scan(&state, &action); err != nil {
		t.Fatalf("read backfill lifecycle: %v", err)
	}
	if state != "active" || action != "append" {
		t.Fatalf("backfill lifecycle = state %q action %q, want active/append", state, action)
	}

	var projectionID, projectionKind, objectKey string
	if err := s.DB().QueryRowContext(ctx, `
		SELECT p.id, p.kind, p.object_key
		FROM memory_projections AS p
		JOIN memory_projection_sources AS ps ON ps.projection_id = p.id
		WHERE ps.evidence_id = 'legacy:entry-v6'`).Scan(&projectionID, &projectionKind, &objectKey); err != nil {
		t.Fatalf("read backfill projection: %v", err)
	}
	if projectionID == "" || projectionKind != "atomic_fact" || objectKey != "entry-v6" {
		t.Fatalf("unexpected backfill projection: id=%q kind=%q key=%q", projectionID, projectionKind, objectKey)
	}

	var fullSource int
	var startChar, endChar, spanDigest sql.NullString
	if err := s.DB().QueryRowContext(ctx, `
		SELECT full_source, start_char, end_char, span_digest
		FROM memory_projection_sources
		WHERE projection_id = ? AND evidence_id = 'legacy:entry-v6'`, projectionID).Scan(
		&fullSource, &startChar, &endChar, &spanDigest,
	); err != nil {
		t.Fatalf("read backfill lineage: %v", err)
	}
	if fullSource != 1 || startChar.Valid || endChar.Valid || spanDigest.Valid {
		t.Fatalf("backfill lineage must be full source without span: full=%d start=%v end=%v digest=%v", fullSource, startChar, endChar, spanDigest)
	}

	var edgeCount int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entity_edges WHERE entity_a = 'alice' AND entity_b = 'bob'`).Scan(&edgeCount); err != nil {
		t.Fatalf("read 003 edge: %v", err)
	}
	if edgeCount != 1 {
		t.Fatalf("003 edge count = %d, want 1", edgeCount)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close upgraded fixture: %v", err)
	}
	s, err = store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("reopen upgraded fixture: %v", err)
	}
	defer s.Close()
	var evidenceCount int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence WHERE id = 'legacy:entry-v6'`).Scan(&evidenceCount); err != nil {
		t.Fatalf("count idempotent evidence: %v", err)
	}
	if evidenceCount != 1 {
		t.Fatalf("backfill must be idempotent: got %d evidence rows", evidenceCount)
	}
}

func TestMigration_V7BackfillFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "v7-rollback.db")
	s, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		t.Fatalf("open current schema: %v", err)
	}
	downgradeV7ForMigrationTest(t, ctx, s.DB())
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE memory_evidence (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create malformed v7 table: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close malformed v6 fixture: %v", err)
	}

	if _, err := store.Open(ctx, store.Options{DSN: dsn}); err == nil {
		t.Fatal("v7 migration unexpectedly succeeded with malformed memory_evidence table")
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open fixture without migrations: %v", err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read rollback version: %v", err)
	}
	if version != 6 {
		t.Fatalf("failed migration recorded version %d, want 6", version)
	}
	var eventTableCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memory_evidence_events'`).Scan(&eventTableCount); err != nil {
		t.Fatalf("check rolled-back v7 table: %v", err)
	}
	if eventTableCount != 0 {
		t.Fatalf("v7 table from failed transaction survived rollback")
	}
}

func downgradeV7ForMigrationTest(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS memory_semantic_episodes`,
		`DROP TABLE IF EXISTS memory_projection_sources`,
		`DROP TABLE IF EXISTS memory_projections`,
		`DROP TABLE IF EXISTS memory_evidence_heads`,
		`DROP TABLE IF EXISTS memory_evidence_events`,
		`DROP TABLE IF EXISTS memory_evidence`,
		`DELETE FROM schema_version WHERE version = 7`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("downgrade v7 fixture with %q: %v", stmt, err)
		}
	}
}
