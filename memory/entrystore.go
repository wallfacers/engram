package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wallfacers/engram/internal/idgen"
	"github.com/wallfacers/engram/store"
)

// Entry is one row of the per-entry memory store (memory_entries). It mirrors
// the schema introduced by sqlite migration v7 (redesign-memory-layered-curation
// D1). LastUsedAt is nil until the entry is first loaded (NULL in the column).
type Entry struct {
	ID              string
	Name            string
	Trigger         string
	Content         string
	Pinned          bool
	Durability      string
	Category        string
	HitCount        int
	LastUsedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CharCount       int
	SourceSessionID string
	// EventDate is when the remembered fact occurred (nil when unknown),
	// distinct from CreatedAt (when it was recorded). Added by migration v8 for
	// time-aware retrieval (memory-hybrid-retrieval-locomo).
	EventDate *time.Time
	// FactSource records provenance: "" (legacy/manual write), "user", "agent",
	// or "extraction" (the ADD-only pipeline).
	FactSource string
	// EventStart and EventEnd bound when the remembered event occurred. They are
	// stored as nullable unix seconds; nil means the event time is unknown.
	EventStart *time.Time
	EventEnd   *time.Time
	// SupersededBy names the newer entry that replaces this entry. Empty means
	// the entry has not been superseded.
	SupersededBy string
	// Revision is a database-maintained, monotonically increasing
	// optimistic-concurrency token. It is deliberately independent of
	// UpdatedAt because timestamps can repeat or be supplied by callers.
	Revision int64
}

// ErrSupersedeSelf is returned when a Supersede call names the same entry as
// both loser and winner. ErrSupersedePinned protects a pinned entry from being
// non-destructively suppressed.
var (
	ErrSupersedeSelf   = errors.New("memory: cannot supersede an entry with itself")
	ErrSupersedePinned = errors.New("memory: cannot supersede a pinned entry")
)

// EntryStore is a thin SQLite-backed accessor for memory_entries. It takes the
// shared *sql.DB directly (as sessionsearch does for its FTS queries) rather
// than extending the portable store.Store interface, keeping the blast radius
// of the memory subsystem local to this package.
type EntryStore struct {
	db *sql.DB
}

// EntryRevision is the optimistic-concurrency token used by background
// curation. ID distinguishes delete/recreate; Revision distinguishes every
// mutation of the same row without relying on wall-clock resolution.
// Interactive Delete/Merge remain unconditional.
type EntryRevision struct {
	ID       string
	Revision int64
}

// NewEntryStore wraps the shared *sql.DB (obtain via store.Store.DB()).
func NewEntryStore(db *sql.DB) *EntryStore {
	return &EntryStore{db: db}
}

// Ledger returns the Evidence Ledger backed by the same namespace-local
// database as this entry store. It lets engine subsystems preserve source
// material without exposing the raw database handle to adapters.
func (s *EntryStore) Ledger() *LedgerStore {
	if s == nil {
		return nil
	}
	return NewLedgerStore(s.db)
}

// ---- time helpers (unix microseconds, consistent with internal/store/sqlite) ----

func entryToMicros(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

func entryFromMicros(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMicro(n).UTC()
}

func entryNullableMicros(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMicro(), Valid: true}
}

func entryFromNullableMicros(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := entryFromMicros(n.Int64)
	return &t
}

func entryNullableSeconds(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func entryFromNullableSeconds(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// upsertTx writes e via INSERT ... ON CONFLICT(name) DO UPDATE within the given
// querier (a *sql.DB or *sql.Tx). It mutates e in place to fill ID/CreatedAt/
// UpdatedAt defaults so callers observe what was persisted. On conflict the
// existing created_at/hit_count/last_used_at and lifecycle fields are
// preserved; only the mutable fields and updated_at are refreshed.
type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type upsertContext interface {
	execContext
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *EntryStore) upsert(ctx context.Context, q upsertContext, e *Entry) error {
	if e.ID == "" {
		e.ID = idgen.NewULID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = time.Now().UTC()
	}
	if e.Durability == "" {
		e.Durability = "volatile"
	}
	var persistedID string
	var createdAt, updatedAt, revision int64
	err := q.QueryRowContext(ctx,
		`INSERT INTO memory_entries(
			id, name, trigger, content, pinned, durability, category,
			 hit_count, last_used_at, created_at, updated_at, char_count, source_session_id,
			event_date, fact_source, event_start, event_end, superseded_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET
			trigger           = excluded.trigger,
			content           = excluded.content,
			pinned            = excluded.pinned,
			durability        = excluded.durability,
			category          = excluded.category,
			char_count        = excluded.char_count,
			source_session_id = excluded.source_session_id,
			event_date        = excluded.event_date,
			fact_source       = excluded.fact_source,
			event_start       = COALESCE(excluded.event_start, event_start),
			event_end         = COALESCE(excluded.event_end, event_end),
			updated_at        = excluded.updated_at,
			revision          = memory_entries.revision + 1
		 RETURNING id, created_at, updated_at, revision`,
		e.ID, e.Name, e.Trigger, e.Content, boolToInt(e.Pinned), e.Durability, e.Category,
		e.HitCount, entryNullableMicros(e.LastUsedAt),
		entryToMicros(e.CreatedAt), entryToMicros(e.UpdatedAt), e.CharCount, e.SourceSessionID,
		entryNullableMicros(e.EventDate), e.FactSource,
		entryNullableSeconds(e.EventStart), entryNullableSeconds(e.EventEnd),
		sql.NullString{String: e.SupersededBy, Valid: e.SupersededBy != ""}).
		Scan(&persistedID, &createdAt, &updatedAt, &revision)
	if err != nil {
		return fmt.Errorf("memory: upsert entry %q: %w", e.Name, err)
	}
	e.ID = persistedID
	e.CreatedAt = entryFromMicros(createdAt)
	e.UpdatedAt = entryFromMicros(updatedAt)
	e.Revision = revision
	return nil
}

// Upsert inserts a new entry or updates the existing one keyed by name. It
// preserves the existing API while recording the call as direct-write Evidence
// and replacing only the atomic-fact projection's source lineage. char_count is
// taken verbatim from e (the caller decides the code-point count for this phase).
func (s *EntryStore) Upsert(ctx context.Context, e *Entry) error {
	return s.upsertWithSourceMode(ctx, e, nil, false)
}

// UpsertWithSources writes an atomic fact supported by explicit active Evidence
// records. It is used by extractors and builders that can name their actual
// message-level provenance; unknown, unavailable, empty, or invalid spans roll
// back the entry and projection together.
func (s *EntryStore) UpsertWithSources(ctx context.Context, e *Entry, sources []EvidenceRef) error {
	return s.upsertWithSourceMode(ctx, e, sources, true)
}

func (s *EntryStore) upsertWithSourceMode(ctx context.Context, e *Entry, sources []EvidenceRef, explicitSources bool) error {
	if e == nil {
		return errors.New("memory: upsert nil entry")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: upsert with sources begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if err := s.upsert(ctx, tx, e); err != nil {
		return err
	}
	configHash := "explicit-sources"
	builder := "entry_store_explicit"
	if !explicitSources {
		content := directEvidenceContent(e)
		digest := sha256.Sum256([]byte(content))
		selfEvidence, err := appendOrReuseEvidence(ctx, tx, EvidenceInput{
			ExternalSourceID: fmt.Sprintf("direct:%s:%x", e.ID, digest[:]),
			SourceType:       EvidenceDirectWrite,
			Content:          content,
			RecordedAt:       e.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("memory: upsert self evidence: %w", err)
		}
		sources = []EvidenceRef{{EvidenceID: selfEvidence.ID, SourceOrder: 0, FullSource: true}}
		configHash = "direct-write-self-evidence"
		builder = "entry_store_direct_write"
	}
	if err := replaceAtomicFactProjectionTx(ctx, tx, e.ID, sources, builder, configHash, e.UpdatedAt); err != nil {
		return fmt.Errorf("memory: upsert atomic-fact projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: upsert with sources commit: %w", err)
	}
	return nil
}

func directEvidenceContent(e *Entry) string {
	if e.Content != "" {
		return e.Content
	}
	if e.Name != "" {
		return e.Name
	}
	return "(empty direct write)"
}

const entrySelectCols = `id, name, trigger, content, pinned, durability, category,
	hit_count, last_used_at, created_at, updated_at, char_count, source_session_id,
	event_date, fact_source, event_start, event_end, superseded_by, revision`

func scanEntry(sc interface{ Scan(dest ...any) error }) (*Entry, error) {
	var e Entry
	var pinned int
	var lastUsedAt, eventDate, eventStart, eventEnd sql.NullInt64
	var supersededBy sql.NullString
	var createdAt, updatedAt int64
	if err := sc.Scan(&e.ID, &e.Name, &e.Trigger, &e.Content, &pinned,
		&e.Durability, &e.Category, &e.HitCount, &lastUsedAt,
		&createdAt, &updatedAt, &e.CharCount, &e.SourceSessionID,
		&eventDate, &e.FactSource, &eventStart, &eventEnd, &supersededBy,
		&e.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("memory: scan entry: %w", err)
	}
	e.Pinned = pinned != 0
	e.LastUsedAt = entryFromNullableMicros(lastUsedAt)
	e.CreatedAt = entryFromMicros(createdAt)
	e.UpdatedAt = entryFromMicros(updatedAt)
	e.EventDate = entryFromNullableMicros(eventDate)
	e.EventStart = entryFromNullableSeconds(eventStart)
	e.EventEnd = entryFromNullableSeconds(eventEnd)
	if supersededBy.Valid {
		e.SupersededBy = supersededBy.String
	}
	return &e, nil
}

// GetByName returns the entry with the given name, or store.ErrNotFound.
func (s *EntryStore) GetByName(ctx context.Context, name string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+entrySelectCols+` FROM memory_entries WHERE name = ?`, name)
	return scanEntry(row)
}

// GetByContent returns the first exact-content Atomic Fact. ADD-only pipeline
// dedup uses it only to union newly observed Evidence onto the existing fact;
// it never overwrites the fact's canonical projection text.
func (s *EntryStore) GetByContent(ctx context.Context, content string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+entrySelectCols+` FROM memory_entries WHERE content = ? ORDER BY created_at ASC, id ASC LIMIT 1`, content)
	return scanEntry(row)
}

// EntriesByName loads a set of entries in bounded batches. Missing names are
// omitted, matching Search's race-tolerant GetByName behavior.
func (s *EntryStore) EntriesByName(ctx context.Context, names []string) (map[string]*Entry, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	out := make(map[string]*Entry, len(unique))
	for start := 0; start < len(unique); start += 500 {
		end := start + 500
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, name := range batch {
			placeholders[i] = "?"
			args[i] = name
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT `+entrySelectCols+` FROM memory_entries WHERE name IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("memory: batch entries: %w", err)
		}
		for rows.Next() {
			entry, err := scanEntry(rows)
			if err != nil {
				rows.Close() //nolint:errcheck
				return nil, err
			}
			out[entry.Name] = entry
		}
		if err := rows.Err(); err != nil {
			rows.Close() //nolint:errcheck
			return nil, fmt.Errorf("memory: batch entries rows: %w", err)
		}
		rows.Close() //nolint:errcheck
	}
	return out, nil
}

// HasContent reports whether an entry with the exact stored content exists.
// Pipelines use this as their idempotency guard before creating derived indexes.
func (s *EntryStore) HasContent(ctx context.Context, content string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM memory_entries WHERE content = ? LIMIT 1`, content).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("memory: check entry content: %w", err)
	}
	return found != 0, nil
}

// List returns all entries, sorted by name ascending.
func (s *EntryStore) List(ctx context.Context) ([]*Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+entrySelectCols+` FROM memory_entries ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("memory: list entries: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SourceRefs returns the direct Evidence lineage for one atomic-fact entry.
func (s *EntryStore) SourceRefs(ctx context.Context, entryID string) ([]EvidenceRef, error) {
	var projectionID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM memory_projections
		WHERE kind = 'atomic_fact' AND object_key = ?`, entryID).Scan(&projectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("memory: find atomic-fact projection %q: %w", entryID, err)
	}
	refs, err := NewProjectionStore(s.db).SourcesByProjectionIDs(ctx, []string{projectionID})
	if err != nil {
		return nil, err
	}
	return refs[projectionID], nil
}

// Delete removes the entry by name, returning store.ErrNotFound when no row
// matched. All owned derived rows, shadow vectors, and references that would
// dangle are cleaned in the same transaction.
func (s *EntryStore) Delete(ctx context.Context, name string) error {
	_, err := s.delete(ctx, name, nil)
	return err
}

// DeleteIfUnchanged deletes name only if it is still the unpinned revision
// observed before a long-running curation judge call. false,nil means the row
// disappeared, changed, or became pinned, so the stale decision was skipped.
func (s *EntryStore) DeleteIfUnchanged(ctx context.Context, name string, expected EntryRevision) (bool, error) {
	return s.delete(ctx, name, &expected)
}

func (s *EntryStore) delete(ctx context.Context, name string, expected *EntryRevision) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("memory: delete begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	var entryID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM memory_entries WHERE name = ?`, name).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != nil {
			return false, nil
		}
		return false, store.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("memory: read entry %q before delete: %w", name, err)
	}

	query := `DELETE FROM memory_entries WHERE name = ?`
	args := []any{name}
	if expected != nil {
		query += ` AND id = ? AND revision = ? AND pinned = 0`
		args = append(args, expected.ID, expected.Revision)
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("memory: delete entry %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("memory: delete entry %q rows: %w", name, err)
	}
	if n == 0 {
		if expected != nil {
			return false, nil
		}
		return false, store.ErrNotFound
	}
	if err := deleteDerivedTx(ctx, tx, name); err != nil {
		return false, err
	}
	if err := clearReverseSupersessionTx(ctx, tx, name); err != nil {
		return false, err
	}
	if err := deleteAtomicFactProjectionTx(ctx, tx, entryID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("memory: delete commit: %w", err)
	}
	return true, nil
}

func deleteAtomicFactProjectionTx(ctx context.Context, q execContext, entryID string) error {
	if _, err := q.ExecContext(ctx,
		`DELETE FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, entryID); err != nil {
		return fmt.Errorf("memory: delete atomic-fact projection %q: %w", entryID, err)
	}
	return nil
}

// deleteDerivedTx invalidates every side row derived from one entry. Shadow
// vectors use exact synthetic names so unrelated suffixes are preserved.
func deleteDerivedTx(ctx context.Context, q execContext, name string) error {
	if _, err := q.ExecContext(ctx,
		`DELETE FROM memory_embeddings WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete embeddings %q: %w", name, err)
	}
	for _, shadowName := range []string{name + "#alias", name + "#query"} {
		if _, err := q.ExecContext(ctx,
			`DELETE FROM memory_embeddings
			 WHERE entry_name = ?
			   AND NOT EXISTS (SELECT 1 FROM memory_entries WHERE name = ?)`,
			shadowName, shadowName); err != nil {
			return fmt.Errorf("memory: delete shadow embedding %q: %w", shadowName, err)
		}
	}
	if err := pruneEntityEdgesForEntryTx(ctx, q, name); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_entities WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete entities %q: %w", name, err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_event_aliases WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete event aliases %q: %w", name, err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_fact_queries WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete fact queries %q: %w", name, err)
	}
	return nil
}

// clearReverseSupersessionTx clears references to an entry that is truly being
// deleted. It is deliberately not called for a surviving merge target.
func clearReverseSupersessionTx(ctx context.Context, q execContext, name string) error {
	if _, err := q.ExecContext(ctx,
		`UPDATE memory_entries
		 SET superseded_by = NULL, revision = revision + 1
		 WHERE superseded_by = ?`, name); err != nil {
		return fmt.Errorf("memory: clear reverse supersession %q: %w", name, err)
	}
	return nil
}

// pruneEntityEdgesForEntryTx removes edges that lose an endpoint when name's
// entity rows are deleted. The entry's rows still exist while this query runs,
// so the surviving-endpoint checks explicitly exclude name. Restricting the
// candidate set to name's entities preserves unrelated historical orphan rows;
// this lifecycle fix is not a whole-database repair sweep.
func pruneEntityEdgesForEntryTx(ctx context.Context, q execContext, name string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_entity_edges
		WHERE (
			entity_a IN (SELECT entity_norm FROM memory_entities WHERE entry_name = ?)
			OR entity_b IN (SELECT entity_norm FROM memory_entities WHERE entry_name = ?)
		) AND (
			NOT EXISTS (
				SELECT 1 FROM memory_entities
				WHERE entity_norm = memory_entity_edges.entity_a AND entry_name <> ?
			)
			OR NOT EXISTS (
				SELECT 1 FROM memory_entities
				WHERE entity_norm = memory_entity_edges.entity_b AND entry_name <> ?
			)
		)`, name, name, name, name); err != nil {
		return fmt.Errorf("memory: prune entity edges for %q: %w", name, err)
	}
	return nil
}

// Merge atomically upserts into and deletes every source name in a single
// transaction. If into.Name is itself one of names, the source delete for that
// name is skipped so the freshly written merged entry survives. A failure at any
// step rolls the whole operation back, leaving all rows in their pre-call state.
func (s *EntryStore) Merge(ctx context.Context, names []string, into *Entry) error {
	_, err := s.merge(ctx, names, into, nil)
	return err
}

// MergeIfUnchanged applies a curation merge only while every source and any
// pre-existing target still match the revisions observed before judging.
// expected must contain every source; absence of into.Name means the target did
// not exist in the snapshot and must still be absent. false,nil skips a stale
// decision without modifying any row.
func (s *EntryStore) MergeIfUnchanged(ctx context.Context, names []string, into *Entry, expected map[string]EntryRevision) (bool, error) {
	return s.merge(ctx, names, into, expected)
}

func (s *EntryStore) merge(ctx context.Context, names []string, into *Entry, expected map[string]EntryRevision) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("memory: merge begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if expected != nil {
		required := make(map[string]struct{}, len(names)+1)
		for _, name := range names {
			required[name] = struct{}{}
		}
		if _, existed := expected[into.Name]; existed {
			required[into.Name] = struct{}{}
		}
		for name := range required {
			revision, ok := expected[name]
			if !ok {
				return false, nil
			}
			var id string
			var currentRevision int64
			var pinned int
			err := tx.QueryRowContext(ctx,
				`SELECT id, revision, pinned FROM memory_entries WHERE name = ?`, name).
				Scan(&id, &currentRevision, &pinned)
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("memory: merge check revision %q: %w", name, err)
			}
			if id != revision.ID || currentRevision != revision.Revision || pinned != 0 {
				return false, nil
			}
		}
		if _, existed := expected[into.Name]; !existed {
			var found int
			err := tx.QueryRowContext(ctx,
				`SELECT 1 FROM memory_entries WHERE name = ?`, into.Name).Scan(&found)
			if err == nil {
				return false, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return false, fmt.Errorf("memory: merge check new target %q: %w", into.Name, err)
			}
		}
	}

	if err := s.upsert(ctx, tx, into); err != nil {
		return false, err
	}
	for _, name := range names {
		if name == into.Name {
			// The merged target shares a name with a source: it was just
			// (re)written above; deleting it would undo the merge.
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE name = ?`, name); err != nil {
			return false, fmt.Errorf("memory: merge delete %q: %w", name, err)
		}
		if err := deleteDerivedTx(ctx, tx, name); err != nil {
			return false, err
		}
		if err := clearReverseSupersessionTx(ctx, tx, name); err != nil {
			return false, err
		}
	}
	// The merged target's own derived rows are stale (content changed); drop
	// them so the write-behind embedder re-embeds and the caller re-indexes
	// entities from the merged content.
	if err := deleteDerivedTx(ctx, tx, into.Name); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("memory: merge commit: %w", err)
	}
	return true, nil
}

// Supersede non-destructively suppresses oldName in favor of newName by setting
// old.superseded_by = newName. Both entries must exist; a pinned loser and a
// self-reference are refused. The superseded entry stays retrievable — the
// retriever only downweights it (contract engine-api §4/§7).
func (s *EntryStore) Supersede(ctx context.Context, oldName, newName string) error {
	if oldName == newName {
		return ErrSupersedeSelf
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries
		 SET superseded_by = ?, revision = revision + 1
		 WHERE name = ?
		   AND pinned = 0
		   AND EXISTS (
			SELECT 1 FROM memory_entries AS winner WHERE winner.name = ?
		   )`,
		newName, oldName, newName)
	if err != nil {
		return fmt.Errorf("memory: supersede %q: %w", oldName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: supersede %q rows: %w", oldName, err)
	}
	if n != 0 {
		return nil
	}

	// Preserve the public method's specific error contract after the atomic
	// guarded update declined to mutate anything.
	old, err := s.GetByName(ctx, oldName)
	if err != nil {
		return err
	}
	if old.Pinned {
		return ErrSupersedePinned
	}
	if _, err := s.GetByName(ctx, newName); err != nil {
		return fmt.Errorf("memory: supersede winner %q: %w", newName, err)
	}
	return store.ErrNotFound
}

// SupersedeIfUnchanged applies a curation conflict only while both endpoints
// still match the exact revisions observed before the remote judge call.
// false,nil means either endpoint disappeared or changed, or the loser became
// pinned, so the stale decision was skipped without mutation.
func (s *EntryStore) SupersedeIfUnchanged(
	ctx context.Context,
	oldName, newName string,
	expectedOld, expectedNew EntryRevision,
) (bool, error) {
	if oldName == newName {
		return false, ErrSupersedeSelf
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries
		 SET superseded_by = ?, revision = revision + 1
		 WHERE name = ?
		   AND id = ?
		   AND revision = ?
		   AND pinned = 0
		   AND EXISTS (
			SELECT 1
			FROM memory_entries AS winner
			WHERE winner.name = ? AND winner.id = ? AND winner.revision = ?
		   )`,
		newName, oldName, expectedOld.ID, expectedOld.Revision,
		newName, expectedNew.ID, expectedNew.Revision)
	if err != nil {
		return false, fmt.Errorf("memory: supersede %q if unchanged: %w", oldName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("memory: supersede %q rows: %w", oldName, err)
	}
	return n == 1, nil
}

// Unsupersede clears an entry's superseded_by marker, reversing a misjudged
// suppression. Returns store.ErrNotFound when no entry matches name.
func (s *EntryStore) Unsupersede(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries
		 SET superseded_by = NULL, revision = revision + 1
		 WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("memory: unsupersede %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: unsupersede %q rows: %w", name, err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// PutEntities replaces the entity index rows for entry name with the given
// entities (normalized via EntityNorm; blanks and duplicates dropped). An empty
// list clears the entry's entities. Runs in one transaction.
func (s *EntryStore) PutEntities(ctx context.Context, name string, entities []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: put entities begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entities WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: clear entities %q: %w", name, err)
	}
	seen := make(map[string]struct{}, len(entities))
	for _, raw := range entities {
		norm := EntityNorm(raw)
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO memory_entities(entry_name, entity_norm, entity_raw) VALUES (?,?,?)`,
			name, norm, raw); err != nil {
			return fmt.Errorf("memory: insert entity %q/%q: %w", name, norm, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: put entities commit: %w", err)
	}
	return nil
}

// PutAliases replaces the event alias index for an entry. Aliases are stored
// verbatim enough for display but whitespace-normalized for stable FTS rows;
// blanks and duplicates are ignored. The FTS mirror is maintained by the
// schema trigger in the same transaction.
func (s *EntryStore) PutAliases(ctx context.Context, name string, aliases []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: put aliases begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_event_aliases WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: clear aliases %q: %w", name, err)
	}
	seen := make(map[string]struct{}, len(aliases))
	for _, raw := range aliases {
		alias := strings.Join(strings.Fields(raw), " ")
		if alias == "" {
			continue
		}
		key := strings.ToLower(alias)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO memory_event_aliases(entry_name, alias) VALUES (?,?)`, name, alias); err != nil {
			return fmt.Errorf("memory: insert alias %q/%q: %w", name, alias, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: put aliases commit: %w", err)
	}
	return nil
}

// PutFactQueries replaces the doc2query pseudo-queries for a fact (feature 012
// shadow source). Whitespace is collapsed and blank/case-insensitive duplicates
// are dropped, mirroring PutAliases.
func (s *EntryStore) PutFactQueries(ctx context.Context, name string, queries []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: put fact queries begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fact_queries WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: clear fact queries %q: %w", name, err)
	}
	seen := make(map[string]struct{}, len(queries))
	for _, raw := range queries {
		query := strings.Join(strings.Fields(raw), " ")
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO memory_fact_queries(entry_name, query) VALUES (?,?)`, name, query); err != nil {
			return fmt.Errorf("memory: insert fact query %q/%q: %w", name, query, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: put fact queries commit: %w", err)
	}
	return nil
}

// FactQueries returns a fact's stored pseudo-queries, ordered by query text.
func (s *EntryStore) FactQueries(ctx context.Context, name string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT query FROM memory_fact_queries WHERE entry_name = ? ORDER BY query`, name)
	if err != nil {
		return nil, fmt.Errorf("memory: fact queries %q: %w", name, err)
	}
	defer rows.Close() //nolint:errcheck

	var queries []string
	for rows.Next() {
		var query string
		if err := rows.Scan(&query); err != nil {
			return nil, fmt.Errorf("memory: scan fact query %q: %w", name, err)
		}
		queries = append(queries, query)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read fact queries %q: %w", name, err)
	}
	return queries, nil
}

// FactQueryEntryNames returns the distinct entry names that have at least one
// pseudo-query, ordered by name. Used to enumerate #query shadow vectors.
func (s *EntryStore) FactQueryEntryNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT entry_name FROM memory_fact_queries ORDER BY entry_name`)
	if err != nil {
		return nil, fmt.Errorf("memory: fact query entry names: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("memory: scan fact query entry name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read fact query entry names: %w", err)
	}
	return names, nil
}

// EntityMatchCounts returns, for the given normalized entity tokens, a map from
// entry name to the number of distinct query tokens that entry matches. Entries
// with zero matches are absent. Used to build the entity retrieval signal.
func (s *EntryStore) EntityMatchCounts(ctx context.Context, tokens []string) (map[string]int, error) {
	counts := make(map[string]int)
	seen := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		rows, err := s.db.QueryContext(ctx,
			`SELECT entry_name FROM memory_entities WHERE entity_norm = ?`, tok)
		if err != nil {
			return nil, fmt.Errorf("memory: entity match %q: %w", tok, err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close() //nolint:errcheck
				return nil, fmt.Errorf("memory: scan entity match: %w", err)
			}
			counts[name]++
		}
		if err := rows.Err(); err != nil {
			rows.Close() //nolint:errcheck
			return nil, err
		}
		rows.Close() //nolint:errcheck
	}
	return counts, nil
}

// EntitiesOf returns the distinct normalized entity tokens indexed for the
// given entry names. Used by the retriever's 1-hop associative expansion:
// seed hits → their entities → co-occurring entries.
func (s *EntryStore) EntitiesOf(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(names))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(names))
	for i, n := range names {
		args[i] = n
	}
	// #nosec G201 -- placeholders is a constant "?" list; values are all bound.
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT DISTINCT entity_norm FROM memory_entities WHERE entry_name IN (%s)`, placeholders),
		args...)
	if err != nil {
		return nil, fmt.Errorf("memory: entities of: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err != nil {
			return nil, fmt.Errorf("memory: scan entity token: %w", err)
		}
		out = append(out, tok)
	}
	return out, rows.Err()
}

// EntitiesByEntry returns normalized entity keys grouped by entry name in one
// query. It is used by maintenance jobs that compare many entries without
// issuing one entity lookup per pair.
func (s *EntryStore) EntitiesByEntry(ctx context.Context, names []string) (map[string][]string, error) {
	out := make(map[string][]string, len(names))
	if s == nil || len(names) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT entry_name, entity_norm FROM memory_entities WHERE entry_name IN (%s) ORDER BY entry_name, entity_norm`, placeholders),
		args...)
	if err != nil {
		return nil, fmt.Errorf("memory: entities by entry: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var name, entity string
		if err := rows.Scan(&name, &entity); err != nil {
			return nil, fmt.Errorf("memory: scan entities by entry: %w", err)
		}
		out[name] = append(out[name], entity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read entities by entry: %w", err)
	}
	return out, nil
}

// BumpUsage records a usage hit: increments hit_count and stamps last_used_at.
// It is best-effort — a name that does not exist is not an error (0 rows
// affected is silently fine), matching the read-only-tool usage-log semantics.
func (s *EntryStore) BumpUsage(ctx context.Context, name string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries
		 SET hit_count = hit_count + 1, last_used_at = ?, revision = revision + 1
		 WHERE name = ?`,
		entryToMicros(at.UTC()), name)
	if err != nil {
		return fmt.Errorf("memory: bump usage %q: %w", name, err)
	}
	return nil
}

// Count returns the total number of entries.
func (s *EntryStore) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entries`).Scan(&n); err != nil {
		return 0, fmt.Errorf("memory: count entries: %w", err)
	}
	return n, nil
}

// CountNonPinned returns the number of non-pinned entries (curation scope).
func (s *EntryStore) CountNonPinned(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_entries WHERE pinned = 0`).Scan(&n); err != nil {
		return 0, fmt.Errorf("memory: count non-pinned entries: %w", err)
	}
	return n, nil
}

// ManifestSizeEstimate returns an approximate code-point size of the INDEX
// manifest region: the sum over non-pinned entries of the rendered line
// `- {name} — {trigger}` plus a per-line overhead for the markers and newline.
// It is a cheap estimate (SQLite LENGTH counts characters for TEXT) used by the
// curation pressure trigger's manifest-size water line (design D5), avoiding a
// full snapshot assembly. The overhead constant mirrors manifestLine's fixed
// glyphs ("- " + " — " + joining "\n").
func (s *EntryStore) ManifestSizeEstimate(ctx context.Context) (int, error) {
	const perLineOverhead = 6
	var n sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(LENGTH(name) + LENGTH(trigger) + ?), 0)
		   FROM memory_entries WHERE pinned = 0`, perLineOverhead).Scan(&n); err != nil {
		return 0, fmt.Errorf("memory: estimate manifest size: %w", err)
	}
	return int(n.Int64), nil
}

// PinnedCharTotal returns the sum of char_count over all pinned entries,
// excluding the entry named excludeName (pass "" to exclude nothing). This lets
// memory_write compute the incremental pinned total for a budget check before an
// upsert: total = PinnedCharTotal(ctx, name) + newContentCharCount.
func (s *EntryStore) PinnedCharTotal(ctx context.Context, excludeName string) (int, error) {
	var n sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(char_count), 0) FROM memory_entries WHERE pinned = 1 AND name <> ?`,
		excludeName).Scan(&n); err != nil {
		return 0, fmt.Errorf("memory: sum pinned char_count: %w", err)
	}
	return int(n.Int64), nil
}
