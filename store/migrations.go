package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// Migration represents a single schema migration step. Up contains the
// statements applied in order inside a single transaction; Down contains
// statements to reverse the migration (applied in reverse order).
type Migration struct {
	Version  int
	Up       []string
	Down     []string
	Backfill func(context.Context, *sql.Tx) error
}

// v1Memory creates the per-entry memory store (redesign-memory-layered-curation
// D1/D6): the memory_entries table, its FTS5 mirror with sync triggers, and the
// single-row curation leader-lease table.
//
// The memory FTS columns (name, trigger, content) are plain text, so the
// triggers index them directly. All timestamps are INTEGER unix microseconds,
// consistent with the rest of the schema.
var v1Memory = []string{
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	)`,
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

// v3BioRetrieval adds the local-only indexes used by the optional associative,
// temporal, and conflict-resolution retrieval signals.
var v3BioRetrieval = []string{
	`CREATE TABLE IF NOT EXISTS memory_entity_edges (
		entity_a   TEXT NOT NULL,
		entity_b   TEXT NOT NULL,
		kind       TEXT NOT NULL DEFAULT 'co',
		weight     REAL NOT NULL DEFAULT 1.0,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (entity_a, entity_b, kind)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_entity_edges_a ON memory_entity_edges(entity_a)`,
	`CREATE INDEX IF NOT EXISTS idx_entity_edges_b ON memory_entity_edges(entity_b)`,

	`CREATE TABLE IF NOT EXISTS memory_event_aliases (
		entry_name TEXT NOT NULL,
		alias      TEXT NOT NULL,
		PRIMARY KEY (entry_name, alias)
	)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS memory_event_aliases_fts USING fts5(
		alias,
		entry_name UNINDEXED,
		tokenize='trigram'
	)`,
	`CREATE TRIGGER IF NOT EXISTS memory_event_aliases_fts_ai AFTER INSERT ON memory_event_aliases BEGIN
		INSERT INTO memory_event_aliases_fts(rowid, alias, entry_name)
		VALUES (new.rowid, new.alias, new.entry_name);
	END`,
	`CREATE TRIGGER IF NOT EXISTS memory_event_aliases_fts_ad AFTER DELETE ON memory_event_aliases BEGIN
		DELETE FROM memory_event_aliases_fts WHERE rowid = old.rowid;
	END`,
	`CREATE TRIGGER IF NOT EXISTS memory_event_aliases_fts_au AFTER UPDATE ON memory_event_aliases BEGIN
		DELETE FROM memory_event_aliases_fts WHERE rowid = old.rowid;
		INSERT INTO memory_event_aliases_fts(rowid, alias, entry_name)
		VALUES (new.rowid, new.alias, new.entry_name);
	END`,

	`ALTER TABLE memory_entries ADD COLUMN event_start INTEGER`,
	`ALTER TABLE memory_entries ADD COLUMN event_end INTEGER`,
	`ALTER TABLE memory_entries ADD COLUMN superseded_by TEXT`,
}

var v3BioRetrievalDown = []string{
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
}

var v4TemporalIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_memory_entries_event_start ON memory_entries(event_start)`,
	`CREATE INDEX IF NOT EXISTS idx_memory_entries_event_end ON memory_entries(event_end)`,
}

var v4TemporalIndexesDown = []string{
	`DROP INDEX IF EXISTS idx_memory_entries_event_end`,
	`DROP INDEX IF EXISTS idx_memory_entries_event_start`,
}

// v5FactQueries adds the doc2query pseudo-query side table (feature 012). Each
// row is one LLM-generated question a fact answers; the embedder turns a fact's
// queries into a "<name>#query" shadow vector that the retriever max-pools back
// onto the source fact. Kept out of the FTS-mirrored base table and separate
// from memory_event_aliases (011) so the two shadow sources stay independent.
var v5FactQueries = []string{
	`CREATE TABLE IF NOT EXISTS memory_fact_queries (
		entry_name TEXT NOT NULL,
		query      TEXT NOT NULL,
		PRIMARY KEY (entry_name, query)
	)`,
}

var v5FactQueriesDown = []string{
	`DROP TABLE IF EXISTS memory_fact_queries`,
}

// v6EntryRevision adds a database-maintained optimistic-concurrency token.
// Wall-clock timestamps are presentation/audit data and may repeat; curation
// and write-behind jobs use revision to reject decisions made from stale rows.
var v6EntryRevision = []string{
	`ALTER TABLE memory_entries ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
}

var v6EntryRevisionDown = []string{
	`ALTER TABLE memory_entries DROP COLUMN revision`,
}

// v7EvidenceLedger separates immutable source material from memory_entries,
// which remains the mutable atomic-fact retrieval projection. All v7 objects
// are additive: existing FTS, embedding, entity, fact-query, and graph data
// are left untouched. The Go backfill is part of the same migration
// transaction because SQLite has no built-in SHA-256 function for the required
// content digest.
var v7EvidenceLedger = []string{
	`CREATE TABLE IF NOT EXISTS memory_evidence (
		id                TEXT PRIMARY KEY,
		source_type       TEXT NOT NULL CHECK (source_type IN ('message', 'direct_write', 'legacy_entry')),
		external_source_id TEXT NOT NULL DEFAULT '',
		source_session_id TEXT NOT NULL DEFAULT '',
		speaker           TEXT NOT NULL DEFAULT '',
		ordinal           INTEGER NOT NULL DEFAULT 0 CHECK (ordinal >= 0),
		content           TEXT NOT NULL CHECK (length(content) > 0),
		occurred_at       INTEGER,
		recorded_at       INTEGER NOT NULL,
		content_digest    TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS memory_evidence_events (
		seq         INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id    TEXT NOT NULL UNIQUE,
		evidence_id TEXT NOT NULL,
		source_type TEXT NOT NULL,
		action      TEXT NOT NULL CHECK (action IN ('append', 'tombstone', 'restore', 'purge')),
		recorded_at INTEGER NOT NULL,
		reason_code TEXT NOT NULL DEFAULT '',
		request_id  TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS memory_evidence_heads (
		evidence_id TEXT PRIMARY KEY,
		state       TEXT NOT NULL CHECK (state IN ('active', 'tombstoned', 'purged')),
		last_seq    INTEGER NOT NULL,
		revision    INTEGER NOT NULL CHECK (revision >= 1),
		changed_at  INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS memory_projections (
		id              TEXT PRIMARY KEY,
		kind            TEXT NOT NULL CHECK (kind IN ('atomic_fact', 'semantic_episode')),
		object_key      TEXT NOT NULL,
		state           TEXT NOT NULL CHECK (state IN ('active', 'stale', 'disabled')),
		builder         TEXT NOT NULL,
		builder_version TEXT NOT NULL,
		config_hash     TEXT NOT NULL,
		built_at        INTEGER NOT NULL,
		revision        INTEGER NOT NULL CHECK (revision >= 1),
		UNIQUE(kind, object_key)
	)`,
	`CREATE TABLE IF NOT EXISTS memory_projection_sources (
		projection_id TEXT NOT NULL REFERENCES memory_projections(id) ON DELETE CASCADE,
		source_order  INTEGER NOT NULL CHECK (source_order >= 0),
		evidence_id   TEXT NOT NULL,
		full_source   INTEGER NOT NULL CHECK (full_source IN (0, 1)),
		start_char    INTEGER,
		end_char      INTEGER,
		span_digest   TEXT,
		relation      TEXT NOT NULL CHECK (relation IN ('supports', 'derived_from')),
		PRIMARY KEY (projection_id, source_order, evidence_id),
		UNIQUE(projection_id, source_order),
		CHECK (
			(full_source = 1 AND start_char IS NULL AND end_char IS NULL AND span_digest IS NULL)
			OR
			(full_source = 0 AND start_char IS NOT NULL AND end_char IS NOT NULL AND span_digest IS NOT NULL AND start_char >= 0 AND end_char > start_char)
		)
	)`,
	`CREATE TABLE IF NOT EXISTS memory_semantic_episodes (
		projection_id TEXT PRIMARY KEY REFERENCES memory_projections(id) ON DELETE CASCADE,
		narrative     TEXT NOT NULL,
		started_at    INTEGER,
		ended_at      INTEGER,
		char_count    INTEGER NOT NULL CHECK (char_count >= 0)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_evidence_session_ordinal ON memory_evidence(source_session_id, ordinal)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_evidence_external ON memory_evidence(source_type, source_session_id, external_source_id) WHERE external_source_id <> ''`,
	`CREATE INDEX IF NOT EXISTS idx_evidence_heads_state ON memory_evidence_heads(state)`,
	`CREATE INDEX IF NOT EXISTS idx_projection_kind_state ON memory_projections(kind, state)`,
	`CREATE INDEX IF NOT EXISTS idx_projection_sources_evidence ON memory_projection_sources(evidence_id)`,
}

var v7EvidenceLedgerDown = []string{
	`DROP INDEX IF EXISTS idx_projection_sources_evidence`,
	`DROP INDEX IF EXISTS idx_projection_kind_state`,
	`DROP INDEX IF EXISTS idx_evidence_heads_state`,
	`DROP INDEX IF EXISTS uq_evidence_external`,
	`DROP INDEX IF EXISTS idx_evidence_session_ordinal`,
	`DROP TABLE IF EXISTS memory_semantic_episodes`,
	`DROP TABLE IF EXISTS memory_projection_sources`,
	`DROP TABLE IF EXISTS memory_projections`,
	`DROP TABLE IF EXISTS memory_evidence_heads`,
	`DROP TABLE IF EXISTS memory_evidence_events`,
	`DROP TABLE IF EXISTS memory_evidence`,
}

// migrationsByVersion is the ordered list of all migrations. Each entry is
// applied inside its own transaction; schema_version is bumped per step.
var migrationsByVersion = []Migration{
	{Version: 1, Up: v1Memory, Down: v1MemoryDown},
	{Version: 2, Up: v2MemoryHybrid, Down: v2MemoryHybridDown},
	{Version: 3, Up: v3BioRetrieval, Down: v3BioRetrievalDown},
	{Version: 4, Up: v4TemporalIndexes, Down: v4TemporalIndexesDown},
	{Version: 5, Up: v5FactQueries, Down: v5FactQueriesDown},
	{Version: 6, Up: v6EntryRevision, Down: v6EntryRevisionDown},
	{Version: 7, Up: v7EvidenceLedger, Down: v7EvidenceLedgerDown, Backfill: backfillV7EvidenceLedger},
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
	if m.Backfill != nil {
		if err := m.Backfill(ctx, tx); err != nil {
			return fmt.Errorf("sqlite: migration v%d backfill failed: %w", m.Version, err)
		}
	}

	if m.Version == 2 {
		slog.Info("sqlite: migration v2 memory hybrid complete")
	}
	if m.Version == 3 {
		slog.Info("sqlite: migration v3 bio retrieval complete")
	}
	if m.Version == 7 {
		slog.Info("sqlite: migration v7 evidence ledger complete")
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO schema_version(version) VALUES (?)`, m.Version); err != nil {
		return fmt.Errorf("sqlite: record schema version v%d: %w", m.Version, err)
	}

	return tx.Commit()
}

type legacyEntrySnapshot struct {
	ID              string
	Name            string
	Content         string
	SourceSessionID string
	CreatedAt       int64
	UpdatedAt       int64
}

func backfillV7EvidenceLedger(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, content, source_session_id, created_at, updated_at
		FROM memory_entries
		ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read legacy entries: %w", err)
	}
	defer rows.Close()

	ordinals := make(map[string]int64)
	for rows.Next() {
		var entry legacyEntrySnapshot
		if err := rows.Scan(
			&entry.ID,
			&entry.Name,
			&entry.Content,
			&entry.SourceSessionID,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return fmt.Errorf("scan legacy entry: %w", err)
		}
		if entry.ID == "" {
			return fmt.Errorf("legacy entry has empty ID")
		}

		content := entry.Content
		if content == "" {
			content = entry.Name
		}
		if content == "" {
			return fmt.Errorf("legacy entry %q has no content or name", entry.ID)
		}
		recordedAt := entry.CreatedAt
		if recordedAt == 0 {
			recordedAt = entry.UpdatedAt
		}
		ordinal := ordinals[entry.SourceSessionID]
		ordinals[entry.SourceSessionID] = ordinal + 1

		evidenceID := "legacy:" + entry.ID
		eventID := "legacy:event:" + entry.ID
		projectionID := "legacy:projection:" + entry.ID
		digest := sha256.Sum256([]byte(content))
		contentDigest := fmt.Sprintf("%x", digest[:])

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_evidence(
				id, source_type, external_source_id, source_session_id, speaker, ordinal,
				content, occurred_at, recorded_at, content_digest
			) VALUES (?, 'legacy_entry', '', ?, '', ?, ?, NULL, ?, ?)
			ON CONFLICT(id) DO NOTHING`,
			evidenceID, entry.SourceSessionID, ordinal, content, recordedAt, contentDigest,
		); err != nil {
			return fmt.Errorf("append legacy evidence %q: %w", entry.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_evidence_events(
				event_id, evidence_id, source_type, action, recorded_at, reason_code, request_id
			) VALUES (?, ?, 'legacy_entry', 'append', ?, 'migration_backfill', 'v7-backfill')
			ON CONFLICT(event_id) DO NOTHING`, eventID, evidenceID, recordedAt); err != nil {
			return fmt.Errorf("append legacy event %q: %w", entry.ID, err)
		}
		var eventSeq int64
		if err := tx.QueryRowContext(ctx,
			`SELECT seq FROM memory_evidence_events WHERE event_id = ?`, eventID,
		).Scan(&eventSeq); err != nil {
			return fmt.Errorf("read legacy event %q: %w", entry.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_evidence_heads(evidence_id, state, last_seq, revision, changed_at)
			VALUES (?, 'active', ?, 1, ?)
			ON CONFLICT(evidence_id) DO NOTHING`, evidenceID, eventSeq, recordedAt); err != nil {
			return fmt.Errorf("append legacy head %q: %w", entry.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_projections(
				id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
			) VALUES (?, 'atomic_fact', ?, 'active', 'v7-backfill', '1', 'v7-legacy-entry', ?, 1)
			ON CONFLICT(kind, object_key) DO NOTHING`, projectionID, entry.ID, recordedAt); err != nil {
			return fmt.Errorf("append legacy projection %q: %w", entry.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_projection_sources(
				projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
			) VALUES (?, 0, ?, 1, NULL, NULL, NULL, 'supports')
			ON CONFLICT(projection_id, source_order, evidence_id) DO NOTHING`, projectionID, evidenceID); err != nil {
			return fmt.Errorf("append legacy lineage %q: %w", entry.ID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy entries: %w", err)
	}
	return nil
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
