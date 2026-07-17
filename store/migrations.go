package store

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Migration represents a single schema migration step. Up contains the
// statements applied in order inside a single transaction; Down contains
// statements to reverse the migration (applied in reverse order).
type Migration struct {
	Version int
	Up      []string
	Down    []string
}

// v1Memory creates the per-entry memory store (redesign-memory-layered-curation
// D1/D6): the memory_entries table, its FTS5 mirror with sync triggers, and the
// single-row curation leader-lease table.
//
// The memory FTS columns (name, trigger, content) are plain text, so the
// triggers index them directly. All timestamps are INTEGER unix microseconds,
// consistent with the rest of the schema.
var v1Memory = []string{
	`CREATE TABLE IF NOT EXISTS memory_entries (
		id                TEXT    PRIMARY KEY,
		name              TEXT    NOT NULL UNIQUE,
		trigger           TEXT    NOT NULL DEFAULT '',
		content           TEXT    NOT NULL DEFAULT '',
		pinned            INTEGER NOT NULL DEFAULT 0,
		durability        TEXT    NOT NULL DEFAULT 'volatile',
		category          TEXT    NOT NULL DEFAULT '',
		hit_count         INTEGER NOT NULL DEFAULT 0,
		last_used_at      INTEGER,
		created_at        INTEGER NOT NULL,
		updated_at        INTEGER NOT NULL,
		char_count        INTEGER NOT NULL DEFAULT 0,
		source_session_id TEXT    NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_memory_pinned ON memory_entries(pinned)`,

	`CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(
		name,
		trigger,
		content,
		tokenize='trigram'
	)`,
	`CREATE TRIGGER IF NOT EXISTS memory_entries_fts_ai AFTER INSERT ON memory_entries BEGIN
		INSERT INTO memory_entries_fts(rowid, name, trigger, content)
		VALUES (new.rowid, new.name, new.trigger, new.content);
	END`,
	`CREATE TRIGGER IF NOT EXISTS memory_entries_fts_ad AFTER DELETE ON memory_entries BEGIN
		DELETE FROM memory_entries_fts WHERE rowid = old.rowid;
	END`,
	`CREATE TRIGGER IF NOT EXISTS memory_entries_fts_au AFTER UPDATE ON memory_entries BEGIN
		DELETE FROM memory_entries_fts WHERE rowid = old.rowid;
		INSERT INTO memory_entries_fts(rowid, name, trigger, content)
		VALUES (new.rowid, new.name, new.trigger, new.content);
	END`,

	`CREATE TABLE IF NOT EXISTS memory_curation_lease (
		id           INTEGER PRIMARY KEY CHECK (id = 1),
		holder       TEXT    NOT NULL DEFAULT '',
		expires_at   INTEGER NOT NULL DEFAULT 0,
		heartbeat_at INTEGER NOT NULL DEFAULT 0
	)`,
	`INSERT OR IGNORE INTO memory_curation_lease(id, holder, expires_at, heartbeat_at)
		VALUES (1, '', 0, 0)`,
}

// v1MemoryDown reverses the v7 migration. Order is safe: drop the triggers and
// FTS mirror before the base table, then the standalone lease table.
var v1MemoryDown = []string{
	`DROP TRIGGER IF EXISTS memory_entries_fts_au`,
	`DROP TRIGGER IF EXISTS memory_entries_fts_ad`,
	`DROP TRIGGER IF EXISTS memory_entries_fts_ai`,
	`DROP TABLE IF EXISTS memory_entries_fts`,
	`DROP TABLE IF EXISTS memory_curation_lease`,
	`DROP TABLE IF EXISTS memory_entries`,
}

// v2MemoryHybrid extends the memory store for hybrid retrieval
// (memory-hybrid-retrieval-locomo). It adds provenance/temporal columns to
// memory_entries and two side tables kept out of the FTS-mirrored base table:
// memory_embeddings (one float32 vector BLOB per entry, rebuildable on model
// change) and memory_entities (normalized entity -> entry index for the
// entity-match retrieval signal). All timestamps remain INTEGER unix micros.
//
// event_date is nullable: the unix-micros instant the remembered fact occurred
// (distinct from created_at, when it was recorded). fact_source records
// provenance (” | user | agent | extraction).
var v2MemoryHybrid = []string{
	`ALTER TABLE memory_entries ADD COLUMN event_date INTEGER`,
	`ALTER TABLE memory_entries ADD COLUMN fact_source TEXT NOT NULL DEFAULT ''`,

	`CREATE TABLE IF NOT EXISTS memory_embeddings (
		entry_name TEXT    PRIMARY KEY,
		model      TEXT    NOT NULL DEFAULT '',
		dims       INTEGER NOT NULL DEFAULT 0,
		vec        BLOB    NOT NULL,
		updated_at INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS memory_entities (
		entry_name  TEXT NOT NULL,
		entity_norm TEXT NOT NULL,
		entity_raw  TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (entry_name, entity_norm)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_memory_entities_norm ON memory_entities(entity_norm)`,
}

// v2MemoryHybridDown reverses v8. SQLite (modernc) supports DROP COLUMN, so the
// added columns are removed after the side tables.
var v2MemoryHybridDown = []string{
	`DROP INDEX IF EXISTS idx_memory_entities_norm`,
	`DROP TABLE IF EXISTS memory_entities`,
	`DROP TABLE IF EXISTS memory_embeddings`,
	`ALTER TABLE memory_entries DROP COLUMN fact_source`,
	`ALTER TABLE memory_entries DROP COLUMN event_date`,
}

// migrationsByVersion is the ordered list of all migrations. Each entry is
// applied inside its own transaction; schema_version is bumped per step.
var migrationsByVersion = []Migration{
	{Version: 1, Up: v1Memory, Down: v1MemoryDown},
	{Version: 2, Up: v2MemoryHybrid, Down: v2MemoryHybridDown},
}

func (s *Store) migrate(ctx context.Context) error {
	// Apply each migration in version order, each in its own transaction.
	for _, m := range migrationsByVersion {
		if err := s.applyMigration(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration checks whether migration m has already been applied and, if
// not, executes its Up statements inside a single transaction, then bumps
// schema_version.
func (s *Store) applyMigration(ctx context.Context, m Migration) error {
	current, err := s.readSchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current >= m.Version {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migration v%d: %w", m.Version, err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, stmt := range m.Up {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlite: migration v%d step %d failed: %s\n  %w", m.Version, i+1, truncateStmt(stmt), err)
		}
	}

	if m.Version == 2 {
		slog.Info("sqlite: migration v2 memory hybrid complete")
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO schema_version(version) VALUES (?)`, m.Version); err != nil {
		return fmt.Errorf("sqlite: record schema version v%d: %w", m.Version, err)
	}

	return tx.Commit()
}

// readSchemaVersion returns the current schema version, or 0 if the table
// does not yet exist (fresh database) or is empty.
func (s *Store) readSchemaVersion(ctx context.Context) (int, error) {
	var version *int
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		// Fresh database: schema_version table doesn't exist yet, or
		// table exists but is empty (NULL → Scan to *int yields nil value, no error).
		// Any other error (corruption, I/O) should propagate.
		if isTableNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("sqlite: read schema version: %w", err)
	}
	if version == nil {
		return 0, nil
	}
	return *version, nil
}

func truncateStmt(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func isTableNotExist(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no such table")
}
