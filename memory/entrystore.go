package memory

import (
	"context"
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

// NewEntryStore wraps the shared *sql.DB (obtain via store.Store.DB()).
func NewEntryStore(db *sql.DB) *EntryStore {
	return &EntryStore{db: db}
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

func (s *EntryStore) upsert(ctx context.Context, q execContext, e *Entry) error {
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
	_, err := q.ExecContext(ctx,
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
			updated_at        = excluded.updated_at`,
		e.ID, e.Name, e.Trigger, e.Content, boolToInt(e.Pinned), e.Durability, e.Category,
		e.HitCount, entryNullableMicros(e.LastUsedAt),
		entryToMicros(e.CreatedAt), entryToMicros(e.UpdatedAt), e.CharCount, e.SourceSessionID,
		entryNullableMicros(e.EventDate), e.FactSource,
		entryNullableSeconds(e.EventStart), entryNullableSeconds(e.EventEnd),
		sql.NullString{String: e.SupersededBy, Valid: e.SupersededBy != ""})
	if err != nil {
		return fmt.Errorf("memory: upsert entry %q: %w", e.Name, err)
	}
	return nil
}

// Upsert inserts a new entry or updates the existing one keyed by name. char_count
// is taken verbatim from e (the caller decides the code-point count for this phase).
func (s *EntryStore) Upsert(ctx context.Context, e *Entry) error {
	return s.upsert(ctx, s.db, e)
}

const entrySelectCols = `id, name, trigger, content, pinned, durability, category,
	hit_count, last_used_at, created_at, updated_at, char_count, source_session_id,
	event_date, fact_source, event_start, event_end, superseded_by`

func scanEntry(sc interface{ Scan(dest ...any) error }) (*Entry, error) {
	var e Entry
	var pinned int
	var lastUsedAt, eventDate, eventStart, eventEnd sql.NullInt64
	var supersededBy sql.NullString
	var createdAt, updatedAt int64
	if err := sc.Scan(&e.ID, &e.Name, &e.Trigger, &e.Content, &pinned,
		&e.Durability, &e.Category, &e.HitCount, &lastUsedAt,
		&createdAt, &updatedAt, &e.CharCount, &e.SourceSessionID,
		&eventDate, &e.FactSource, &eventStart, &eventEnd, &supersededBy); err != nil {
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

// Delete removes the entry by name, returning store.ErrNotFound when no row
// matched. Derived rows in memory_embeddings and memory_entities are cascaded
// away in the same transaction (migration v8 has no FK cascade because the
// side tables key on entry_name, not rowid).
func (s *EntryStore) Delete(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: delete begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	res, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("memory: delete entry %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: delete entry %q rows: %w", name, err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	if err := deleteDerivedTx(ctx, tx, name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: delete commit: %w", err)
	}
	return nil
}

// deleteDerivedTx removes the embedding and entity rows for one entry name.
func deleteDerivedTx(ctx context.Context, q execContext, name string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete embedding %q: %w", name, err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM memory_entities WHERE entry_name = ?`, name); err != nil {
		return fmt.Errorf("memory: delete entities %q: %w", name, err)
	}
	return nil
}

// Merge atomically upserts into and deletes every source name in a single
// transaction. If into.Name is itself one of names, the source delete for that
// name is skipped so the freshly written merged entry survives. A failure at any
// step rolls the whole operation back, leaving all rows in their pre-call state.
func (s *EntryStore) Merge(ctx context.Context, names []string, into *Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: merge begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if err := s.upsert(ctx, tx, into); err != nil {
		return err
	}
	for _, name := range names {
		if name == into.Name {
			// The merged target shares a name with a source: it was just
			// (re)written above; deleting it would undo the merge.
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE name = ?`, name); err != nil {
			return fmt.Errorf("memory: merge delete %q: %w", name, err)
		}
		if err := deleteDerivedTx(ctx, tx, name); err != nil {
			return err
		}
	}
	// The merged target's own derived rows are stale (content changed); drop
	// them so the write-behind embedder re-embeds and the caller re-indexes
	// entities from the merged content.
	if err := deleteDerivedTx(ctx, tx, into.Name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: merge commit: %w", err)
	}
	return nil
}

// Supersede non-destructively suppresses oldName in favor of newName by setting
// old.superseded_by = newName. Both entries must exist; a pinned loser and a
// self-reference are refused. The superseded entry stays retrievable — the
// retriever only downweights it (contract engine-api §4/§7).
func (s *EntryStore) Supersede(ctx context.Context, oldName, newName string) error {
	if oldName == newName {
		return ErrSupersedeSelf
	}
	old, err := s.GetByName(ctx, oldName)
	if err != nil {
		return err // store.ErrNotFound when the loser is unknown
	}
	if old.Pinned {
		return ErrSupersedePinned
	}
	if _, err := s.GetByName(ctx, newName); err != nil {
		return fmt.Errorf("memory: supersede winner %q: %w", newName, err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries SET superseded_by = ? WHERE name = ?`, newName, oldName)
	if err != nil {
		return fmt.Errorf("memory: supersede %q: %w", oldName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: supersede %q rows: %w", oldName, err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Unsupersede clears an entry's superseded_by marker, reversing a misjudged
// suppression. Returns store.ErrNotFound when no entry matches name.
func (s *EntryStore) Unsupersede(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_entries SET superseded_by = NULL WHERE name = ?`, name)
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
		`UPDATE memory_entries SET hit_count = hit_count + 1, last_used_at = ? WHERE name = ?`,
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
