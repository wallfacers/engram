package store_test

import (
	"context"
	"database/sql"
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
	if version != 2 {
		t.Errorf("expected version 2 after first open, got %d", version)
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
