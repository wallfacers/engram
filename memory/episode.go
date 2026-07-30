package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wallfacers/engram/internal/idgen"
)

// EpisodeBoundary is the segmenter's only output: a closed interval of
// Evidence IDs within a single session. The segmenter must not rewrite
// content; it only declares where each episode starts and ends.
type EpisodeBoundary struct {
	FirstEvidenceID string
	LastEvidenceID  string
}

// EpisodeSegmenter inspects ordered active Evidence within one session and
// returns a contiguous, non-overlapping partition. Implementations must only
// select boundaries; content rendering and lineage are owned by EpisodeStore.
type EpisodeSegmenter interface {
	Segment(ctx context.Context, session []Evidence) ([]EpisodeBoundary, error)
}

var (
	ErrEpisodeSegmenterRequired = errors.New("memory: episode segmenter is required")
	ErrEpisodeNotContinuous     = errors.New("memory: episode evidence ordinals are not continuous")
)

// EpisodeStore builds and manages semantic episode projections. It is a
// read-only consumer of the Ledger: it never modifies Evidence content or
// lifecycle state, and it always reads active Evidence through the public
// LedgerStore API.
type EpisodeStore struct {
	db          *sql.DB
	ledger      *LedgerStore
	projections *ProjectionStore
}

// NewEpisodeStore creates an EpisodeStore backed by the namespace-local
// database. All three collaborators must share the same underlying *sql.DB.
func NewEpisodeStore(db *sql.DB, ledger *LedgerStore, projections *ProjectionStore) *EpisodeStore {
	return &EpisodeStore{db: db, ledger: ledger, projections: projections}
}

// RebuildSession reads active Evidence for sourceSessionID, partitions it
// with the given segmenter, and persists one semantic_episode projection per
// boundary. Rebuild with the same config hash deletes the old episodes first,
// making the call idempotent.
//
// Segmenter must not be nil. A nil or empty boundary list is a valid no-op
// (zero episodes). Each boundary must span a contiguous ordinal range within
// the session; non-contiguous or cross-session boundaries are rejected.
func (s *EpisodeStore) RebuildSession(
	ctx context.Context,
	sourceSessionID string,
	segmenter EpisodeSegmenter,
	builderVersion string,
	configHash string,
) ([]Projection, error) {
	if segmenter == nil {
		return nil, ErrEpisodeSegmenterRequired
	}
	if s == nil || s.db == nil || s.ledger == nil || s.projections == nil {
		return nil, fmt.Errorf("memory: nil episode store")
	}
	if sourceSessionID == "" {
		return nil, fmt.Errorf("memory: missing source session ID")
	}

	// Read active Evidence for this session in deterministic order.
	session, err := s.ledger.ListSession(ctx, sourceSessionID, false)
	if err != nil {
		return nil, fmt.Errorf("memory: episode list session %q: %w", sourceSessionID, err)
	}
	if len(session) == 0 {
		return []Projection{}, nil
	}

	boundaries, err := segmenter.Segment(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("memory: episode segmenter: %w", err)
	}
	if len(boundaries) == 0 {
		return []Projection{}, nil
	}

	// Build an index: evidence ID → position in session.
	index := make(map[string]int, len(session))
	for i, ev := range session {
		index[ev.ID] = i
	}

	// Validate every boundary and collect the source ranges.
	type episodeRange struct {
		start int
		end   int // inclusive
	}
	var ranges []episodeRange
	for _, boundary := range boundaries {
		startIdx, ok := index[boundary.FirstEvidenceID]
		if !ok {
			return nil, fmt.Errorf("memory: episode boundary first %q not found in session %q", boundary.FirstEvidenceID, sourceSessionID)
		}
		endIdx, ok := index[boundary.LastEvidenceID]
		if !ok {
			return nil, fmt.Errorf("memory: episode boundary last %q not found in session %q", boundary.LastEvidenceID, sourceSessionID)
		}
		if startIdx > endIdx {
			return nil, fmt.Errorf("memory: episode boundary first %q after last %q", boundary.FirstEvidenceID, boundary.LastEvidenceID)
		}
		if endIdx-startIdx+1 != session[endIdx].Ordinal-session[startIdx].Ordinal+1 {
			return nil, fmt.Errorf("%w: session %q ordinals %d..%d, expected %d sources",
				ErrEpisodeNotContinuous, sourceSessionID,
				session[startIdx].Ordinal, session[endIdx].Ordinal,
				endIdx-startIdx+1)
		}
		ranges = append(ranges, episodeRange{start: startIdx, end: endIdx})
	}

	// Build and persist each episode in one transaction that also deletes
	// any prior episode projections with the same config hash.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("memory: begin episode rebuild: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete old episodes with the same config hash inside the transaction.
	if err := s.deleteByConfigTx(ctx, tx, configHash); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	projections := make([]Projection, 0, len(ranges))

	for _, r := range ranges {
		ep := session[r.start : r.end+1]
		proj, err := s.buildEpisodeTx(ctx, tx, ep, builderVersion, configHash, now)
		if err != nil {
			return nil, err
		}
		projections = append(projections, proj)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("memory: commit episode rebuild: %w", err)
	}
	return projections, nil
}

// buildEpisodeTx writes one episode projection, payload, and full lineage
// inside the given transaction.
func (s *EpisodeStore) buildEpisodeTx(
	ctx context.Context,
	tx *sql.Tx,
	sources []Evidence,
	builderVersion string,
	configHash string,
	builtAt time.Time,
) (Projection, error) {
	if len(sources) == 0 {
		return Projection{}, fmt.Errorf("memory: episode requires at least one active source")
	}

	// Render narrative: speaker: content\n for each source.
	var sb strings.Builder
	for _, ev := range sources {
		sb.WriteString(ev.Speaker)
		sb.WriteString(": ")
		sb.WriteString(ev.Content)
		sb.WriteString("\n")
	}
	narrative := sb.String()
	charCount := utf8.RuneCountInString(narrative)

	// Compute started_at / ended_at from source occurred_at fields.
	var startedAt, endedAt *int64
	for _, ev := range sources {
		if ev.OccurredAt != nil && !ev.OccurredAt.IsZero() {
			micros := ev.OccurredAt.UTC().UnixMicro()
			if startedAt == nil || micros < *startedAt {
				startedAt = &micros
			}
			if endedAt == nil || micros > *endedAt {
				endedAt = &micros
			}
		}
	}

	projectionID := idgen.NewULID()

	// Insert projection registry row.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_projections(
			id, kind, object_key, state, builder, builder_version, config_hash, built_at, revision
		) VALUES (?, 'semantic_episode', ?, 'active', 'episode', ?, ?, ?, 1)`,
		projectionID, projectionID, builderVersion, configHash, builtAt.UTC().UnixMicro(),
	); err != nil {
		return Projection{}, fmt.Errorf("memory: insert episode projection: %w", err)
	}

	// Insert episode payload.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_semantic_episodes(
			projection_id, narrative, started_at, ended_at, char_count
		) VALUES (?, ?, ?, ?, ?)`,
		projectionID, narrative, startedAt, endedAt, charCount,
	); err != nil {
		return Projection{}, fmt.Errorf("memory: insert episode payload: %w", err)
	}

	// Insert full-source lineage for every source in order.
	for order, ev := range sources {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_projection_sources(
				projection_id, source_order, evidence_id, full_source, start_char, end_char, span_digest, relation
			) VALUES (?, ?, ?, 1, NULL, NULL, NULL, 'supports')`,
			projectionID, order, ev.ID,
		); err != nil {
			return Projection{}, fmt.Errorf("memory: insert episode source %q: %w", ev.ID, err)
		}
	}

	return Projection{
		ID:             projectionID,
		Kind:           ProjectionSemanticEpisode,
		ObjectKey:      projectionID,
		State:          "active",
		Builder:        "episode",
		BuilderVersion: builderVersion,
		ConfigHash:     configHash,
		BuiltAt:        builtAt,
		Revision:       1,
	}, nil
}

// DeleteByConfig removes all episode projections and their payloads that were
// built with the given config hash. It does NOT delete any Evidence records.
// Projections with a different config hash are left untouched.
func (s *EpisodeStore) DeleteByConfig(ctx context.Context, configHash string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memory: nil episode store")
	}
	if configHash == "" {
		return fmt.Errorf("memory: missing config hash for episode delete")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: begin episode delete: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.deleteByConfigTx(ctx, tx, configHash); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: commit episode delete: %w", err)
	}
	return nil
}

// episodeDB is a narrow interface satisfied by both *sql.DB and *sql.Tx for
// the delete operations used by deleteByConfigTx.
type episodeDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// deleteByConfigTx deletes episodes for a config hash within a transaction
// or on the bare DB connection.
func (s *EpisodeStore) deleteByConfigTx(ctx context.Context, conn episodeDB, configHash string) error {
	// Find all episode projections for this config hash.
	rows, err := conn.QueryContext(ctx, `
		SELECT id FROM memory_projections
		WHERE kind = 'semantic_episode' AND config_hash = ?`, configHash)
	if err != nil {
		return fmt.Errorf("memory: query episode projections for delete: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close() //nolint:errcheck
			return fmt.Errorf("memory: scan episode projection id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //nolint:errcheck
		return fmt.Errorf("memory: iterate episode projections: %w", err)
	}
	rows.Close() //nolint:errcheck

	for _, id := range ids {
		// Delete lineage first (FK to projection).
		if _, err := conn.ExecContext(ctx, `DELETE FROM memory_projection_sources WHERE projection_id = ?`, id); err != nil {
			return fmt.Errorf("memory: delete episode sources %q: %w", id, err)
		}
		// Delete payload (FK to projection).
		if _, err := conn.ExecContext(ctx, `DELETE FROM memory_semantic_episodes WHERE projection_id = ?`, id); err != nil {
			return fmt.Errorf("memory: delete episode payload %q: %w", id, err)
		}
		// Delete projection registry row.
		if _, err := conn.ExecContext(ctx, `DELETE FROM memory_projections WHERE id = ?`, id); err != nil {
			return fmt.Errorf("memory: delete episode projection %q: %w", id, err)
		}
	}
	return nil
}

// episodeConfigHash returns a deterministic hash of the segmenter config for
// caller-side fingerprinting.
func EpisodeConfigHash(segmenterName string, params map[string]string) string {
	var sb strings.Builder
	sb.WriteString(segmenterName)
	sb.WriteString("|")
	// Sort params for determinism — we use a simple approach since params
	// are expected to be small and caller-controlled.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	// Sort by key for determinism.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params[k])
		sb.WriteString(";")
	}
	digest := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("episode:%x", digest[:8])
}
