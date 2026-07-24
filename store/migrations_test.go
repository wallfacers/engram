package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/wallfacers/engram/store"
)

func TestMigration_FreshDB(t *testing.T) {
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open fresh db: %v", err)
	}
	defer s.Close()
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
	if version != 5 {
		t.Errorf("expected version 5 after first open, got %d", version)
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
	if version != 5 {
		t.Fatalf("expected migration v5, got v%d", version)
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
	for _, column := range []string{"event_start", "event_end", "superseded_by"} {
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
	// upgrades the same v2 database back to v4.
	for _, stmt := range []string{
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
	if version != 5 {
		t.Fatalf("expected migration v5 after round trip, got v%d", version)
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
	if version != 5 {
		t.Fatalf("expected migration v5, got v%d", version)
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
