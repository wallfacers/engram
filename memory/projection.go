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

// ProjectionKind identifies one rebuildable view over immutable Evidence. V7
// deliberately admits only facts and semantic episodes; Event, Scene, Profile,
// and graph remain conditional experiments rather than prebuilt storage tiers.
type ProjectionKind string

const (
	ProjectionAtomicFact      ProjectionKind = "atomic_fact"
	ProjectionSemanticEpisode ProjectionKind = "semantic_episode"
)

// Projection is the registry record for a rebuildable view. Its payload lives
// in the projection-specific table or existing memory_entries table; canonical
// source material always remains in the Ledger.
type Projection struct {
	ID             string
	Kind           ProjectionKind
	ObjectKey      string
	State          string
	Builder        string
	BuilderVersion string
	ConfigHash     string
	BuiltAt        time.Time
	Revision       int64
}

var ErrInvalidEvidenceRef = errors.New("memory: invalid evidence reference")

// ProjectionStore provides batch source lineage and source-driven invalidation
// to engine builders. It intentionally does not expose generic CRUD to
// adapters: every payload kind owns its atomic build/replace path.
type ProjectionStore struct {
	db *sql.DB
}

// NewProjectionStore wraps the shared namespace-local SQLite database.
func NewProjectionStore(db *sql.DB) *ProjectionStore {
	return &ProjectionStore{db: db}
}

// SourcesByProjectionIDs returns direct lineage in deterministic source order.
// It batches IDs to stay below SQLite bind limits and never performs one query
// per candidate.
func (s *ProjectionStore) SourcesByProjectionIDs(ctx context.Context, projectionIDs []string) (map[string][]EvidenceRef, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memory: nil projection store")
	}
	unique := uniqueNonEmptyStrings(projectionIDs)
	out := make(map[string][]EvidenceRef, len(unique))
	for start := 0; start < len(unique); start += 500 {
		end := start + 500
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for index, projectionID := range batch {
			placeholders[index] = "?"
			args[index] = projectionID
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest
			FROM memory_projection_sources
			WHERE projection_id IN (`+strings.Join(placeholders, ",")+`)
			ORDER BY projection_id ASC, source_order ASC, evidence_id ASC`, args...)
		if err != nil {
			return nil, fmt.Errorf("memory: batch projection sources: %w", err)
		}
		for rows.Next() {
			var projectionID string
			var ref EvidenceRef
			var fullSource int
			var startChar, endChar sql.NullInt64
			var spanDigest sql.NullString
			if err := rows.Scan(
				&projectionID,
				&ref.SourceOrder,
				&ref.EvidenceID,
				&fullSource,
				&startChar,
				&endChar,
				&spanDigest,
			); err != nil {
				rows.Close() //nolint:errcheck
				return nil, fmt.Errorf("memory: scan projection source: %w", err)
			}
			ref.FullSource = fullSource != 0
			if startChar.Valid {
				value := int(startChar.Int64)
				ref.StartChar = &value
			}
			if endChar.Valid {
				value := int(endChar.Int64)
				ref.EndChar = &value
			}
			if spanDigest.Valid {
				ref.SpanDigest = spanDigest.String
			}
			out[projectionID] = append(out[projectionID], ref)
		}
		if err := rows.Err(); err != nil {
			rows.Close() //nolint:errcheck
			return nil, fmt.Errorf("memory: iterate projection sources: %w", err)
		}
		rows.Close() //nolint:errcheck
	}
	return out, nil
}

// MarkStaleByEvidenceIDs invalidates only active projections that directly
// reference a changed source. Disabled views remain disabled; they cannot be
// accidentally revived through a source lifecycle operation.
func (s *ProjectionStore) MarkStaleByEvidenceIDs(ctx context.Context, evidenceIDs []string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memory: nil projection store")
	}
	unique := uniqueNonEmptyStrings(evidenceIDs)
	for start := 0; start < len(unique); start += 500 {
		end := start + 500
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for index, evidenceID := range batch {
			placeholders[index] = "?"
			args[index] = evidenceID
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE memory_projections
			SET state = 'stale', revision = revision + 1
			WHERE state = 'active'
			  AND id IN (
				SELECT DISTINCT projection_id
				FROM memory_projection_sources
				WHERE evidence_id IN (`+strings.Join(placeholders, ",")+`)
			  )`, args...); err != nil {
			return fmt.Errorf("memory: mark projections stale: %w", err)
		}
	}
	return nil
}

func validateEvidenceRef(content string, ref EvidenceRef) error {
	if ref.EvidenceID == "" {
		return fmt.Errorf("%w: missing evidence ID", ErrInvalidEvidenceRef)
	}
	if ref.SourceOrder < 0 {
		return fmt.Errorf("%w: negative source order", ErrInvalidEvidenceRef)
	}
	if ref.FullSource {
		if ref.StartChar != nil || ref.EndChar != nil || ref.SpanDigest != "" {
			return fmt.Errorf("%w: full source includes span fields", ErrInvalidEvidenceRef)
		}
		return nil
	}
	if ref.StartChar == nil || ref.EndChar == nil || ref.SpanDigest == "" {
		return fmt.Errorf("%w: partial source needs start/end/digest", ErrInvalidEvidenceRef)
	}
	runes := []rune(content)
	if *ref.StartChar < 0 || *ref.EndChar <= *ref.StartChar || *ref.EndChar > len(runes) {
		return fmt.Errorf("%w: span [%d,%d) is outside %d code points", ErrInvalidEvidenceRef, *ref.StartChar, *ref.EndChar, len(runes))
	}
	digest := sha256.Sum256([]byte(string(runes[*ref.StartChar:*ref.EndChar])))
	if !strings.EqualFold(ref.SpanDigest, fmt.Sprintf("%x", digest[:])) {
		return fmt.Errorf("%w: span digest mismatch", ErrInvalidEvidenceRef)
	}
	return nil
}

func validateActiveEvidenceRefsTx(ctx context.Context, tx *sql.Tx, refs []EvidenceRef) error {
	if len(refs) == 0 {
		return fmt.Errorf("%w: empty source set", ErrInvalidEvidenceRef)
	}
	orders := make(map[int]struct{}, len(refs))
	for _, ref := range refs {
		if _, exists := orders[ref.SourceOrder]; exists {
			return fmt.Errorf("%w: duplicate source order %d", ErrInvalidEvidenceRef, ref.SourceOrder)
		}
		orders[ref.SourceOrder] = struct{}{}
		var content, state string
		err := tx.QueryRowContext(ctx, `
			SELECT e.content, h.state
			FROM memory_evidence AS e
			JOIN memory_evidence_heads AS h ON h.evidence_id = e.id
			WHERE e.id = ?`, ref.EvidenceID).Scan(&content, &state)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("memory: validate evidence source %q: %w", ref.EvidenceID, err)
		}
		if EvidenceState(state) != EvidenceActive {
			return evidenceStateError(ref.EvidenceID, EvidenceState(state))
		}
		if err := validateEvidenceRef(content, ref); err != nil {
			return err
		}
	}
	for order := 0; order < len(refs); order++ {
		if _, exists := orders[order]; !exists {
			return fmt.Errorf("%w: source order must be contiguous from 0", ErrInvalidEvidenceRef)
		}
	}
	return nil
}

func replaceAtomicFactProjectionTx(ctx context.Context, tx *sql.Tx, entryID string, refs []EvidenceRef, builder, configHash string, builtAt time.Time) error {
	if entryID == "" {
		return fmt.Errorf("%w: missing atomic-fact object key", ErrInvalidEvidenceRef)
	}
	if err := validateActiveEvidenceRefsTx(ctx, tx, refs); err != nil {
		return err
	}
	if builtAt.IsZero() {
		builtAt = time.Now().UTC()
	}

	var projectionID string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, entryID).Scan(&projectionID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		projectionID = idgen.NewULID()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_projections(
				id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
			) VALUES (?, 'atomic_fact', ?, 'active', ?, '1', ?, ?, 1)`,
			projectionID, entryID, builder, configHash, builtAt.UTC().UnixMicro()); err != nil {
			return fmt.Errorf("memory: insert atomic-fact projection: %w", err)
		}
	case err != nil:
		return fmt.Errorf("memory: read atomic-fact projection: %w", err)
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_projections
			SET state = 'active', builder = ?, builder_version = '1', config_hash = ?, built_at = ?, revision = revision + 1
			WHERE id = ?`, builder, configHash, builtAt.UTC().UnixMicro(), projectionID); err != nil {
			return fmt.Errorf("memory: update atomic-fact projection: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_projection_sources WHERE projection_id = ?`, projectionID); err != nil {
			return fmt.Errorf("memory: clear atomic-fact sources: %w", err)
		}
	}

	for _, ref := range refs {
		var startChar, endChar any
		if ref.StartChar != nil {
			startChar = *ref.StartChar
		}
		if ref.EndChar != nil {
			endChar = *ref.EndChar
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_projection_sources(
				projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'supports')`,
			projectionID, ref.SourceOrder, ref.EvidenceID, boolToInt(ref.FullSource), startChar, endChar,
			nullableString(ref.SpanDigest)); err != nil {
			return fmt.Errorf("memory: insert atomic-fact source: %w", err)
		}
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
