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
	"github.com/wallfacers/engram/store"
)

// EvidenceSourceType identifies what supplied an immutable Evidence record.
// Legacy entries are migration snapshots only and never claim message-level
// provenance.
type EvidenceSourceType string

const (
	EvidenceMessage     EvidenceSourceType = "message"
	EvidenceDirectWrite EvidenceSourceType = "direct_write"
	EvidenceLegacyEntry EvidenceSourceType = "legacy_entry"
)

// EvidenceState is the current lifecycle state cached in memory_evidence_heads.
type EvidenceState string

const (
	EvidenceActive     EvidenceState = "active"
	EvidenceTombstoned EvidenceState = "tombstoned"
	EvidencePurged     EvidenceState = "purged"
)

var (
	// ErrInvalidEvidence reports an invalid append input before the batch starts
	// its transaction, so callers never receive a partial append.
	ErrInvalidEvidence = errors.New("memory: invalid evidence")
	// ErrEvidenceConflict reports a reused external source ID whose immutable
	// source payload differs from the record already stored for that key.
	ErrEvidenceConflict = errors.New("memory: evidence external source conflict")
	// ErrEvidenceUnavailable reports a tombstoned Evidence record. A caller
	// must restore it through the explicit lifecycle API rather than re-append.
	ErrEvidenceUnavailable = errors.New("memory: evidence unavailable")
	// ErrEvidencePurged reports a purged Evidence record. Its content is gone
	// and cannot be restored.
	ErrEvidencePurged = errors.New("memory: evidence purged")
	// ErrEvidenceState reports an invalid lifecycle transition that is not an
	// idempotent retry of the same lifecycle request.
	ErrEvidenceState = errors.New("memory: invalid evidence lifecycle state")
	// ErrPurgeIncomplete reports a completed logical purge whose final WAL
	// checkpoint did not finish. The source remains purged and retrying Purge
	// performs only the physical checkpoint step.
	ErrPurgeIncomplete = errors.New("memory: evidence purge checkpoint incomplete")
)

// EvidenceInput is caller-provided canonical source material. AppendBatch
// assigns the immutable ID and content digest; all times are persisted as unix
// microseconds in SQLite.
type EvidenceInput struct {
	ExternalSourceID string
	SourceType       EvidenceSourceType
	SourceSessionID  string
	Speaker          string
	Ordinal          int
	Content          string
	OccurredAt       *time.Time
	RecordedAt       time.Time
}

// Evidence is an immutable source record with its current lifecycle state.
type Evidence struct {
	ID               string
	ExternalSourceID string
	SourceType       EvidenceSourceType
	SourceSessionID  string
	Speaker          string
	Ordinal          int
	Content          string
	OccurredAt       *time.Time
	RecordedAt       time.Time
	ContentDigest    string
	State            EvidenceState
	Revision         int64
}

// EvidenceRef is a direct projection-to-Evidence reference. It is defined
// here because Ledger, projection builders, and the Compiler share the same
// source/span contract.
type EvidenceRef struct {
	EvidenceID  string
	SourceOrder int
	FullSource  bool
	StartChar   *int
	EndChar     *int
	SpanDigest  string
}

// LifecycleRequest is reserved for the explicit tombstone/restore/purge API.
// Its type is frozen here so all call sites use the same audit shape.
type LifecycleRequest struct {
	EvidenceID string
	RequestID  string
	ReasonCode string
}

// PurgeResult is the result shape for the later explicit purge operation.
type PurgeResult struct {
	EvidenceID        string
	Purged            bool
	CheckpointPending bool
}

// LedgerStore is the SQLite-backed immutable Evidence Ledger. It shares the
// namespace-local database with EntryStore; namespace isolation remains a
// property of the surrounding namespace-to-database registry.
type LedgerStore struct {
	db *sql.DB
}

// NewLedgerStore wraps the shared namespace-local SQLite database.
func NewLedgerStore(db *sql.DB) *LedgerStore {
	return &LedgerStore{db: db}
}

const evidenceSelectColumns = `
	e.id, e.external_source_id, e.source_type, e.source_session_id, e.speaker,
	e.ordinal, e.content, e.occurred_at, e.recorded_at, e.content_digest,
	h.state, h.revision`

// AppendBatch validates the complete input set before writing, then appends or
// reuses every item in one transaction. A conflicting external source ID rolls
// back the entire batch, preserving append-only Evidence semantics.
func (s *LedgerStore) AppendBatch(ctx context.Context, inputs []EvidenceInput) ([]Evidence, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil ledger store", ErrInvalidEvidence)
	}
	for index, input := range inputs {
		if err := validateEvidenceInput(input); err != nil {
			return nil, fmt.Errorf("%w at batch index %d: %v", ErrInvalidEvidence, index, err)
		}
	}
	if len(inputs) == 0 {
		return []Evidence{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("memory: begin evidence append: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	appended := make([]Evidence, 0, len(inputs))
	for _, input := range inputs {
		evidence, err := appendOrReuseEvidence(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		appended = append(appended, evidence)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("memory: commit evidence append: %w", err)
	}
	return appended, nil
}

func validateEvidenceInput(input EvidenceInput) error {
	if input.SourceType != EvidenceMessage && input.SourceType != EvidenceDirectWrite && input.SourceType != EvidenceLegacyEntry {
		return fmt.Errorf("unknown source type %q", input.SourceType)
	}
	if input.Ordinal < 0 {
		return fmt.Errorf("negative ordinal %d", input.Ordinal)
	}
	if input.Content == "" {
		return errors.New("empty content")
	}
	if !utf8.ValidString(input.Content) {
		return errors.New("content is not valid UTF-8")
	}
	if input.RecordedAt.IsZero() {
		return errors.New("missing recorded_at")
	}
	if input.SourceType == EvidenceMessage {
		if input.SourceSessionID == "" {
			return errors.New("message is missing source_session_id")
		}
		if input.Speaker == "" {
			return errors.New("message is missing speaker")
		}
	}
	return nil
}

func appendOrReuseEvidence(ctx context.Context, tx *sql.Tx, input EvidenceInput) (Evidence, error) {
	if input.ExternalSourceID != "" {
		existing, err := findEvidenceByExternalID(ctx, tx, input)
		if err == nil {
			if !sameEvidencePayload(existing, input) {
				return Evidence{}, fmt.Errorf("%w: source_type=%q session=%q external_source_id=%q",
					ErrEvidenceConflict, input.SourceType, input.SourceSessionID, input.ExternalSourceID)
			}
			if existing.State != EvidenceActive {
				return Evidence{}, evidenceStateError(existing.ID, existing.State)
			}
			return existing, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return Evidence{}, err
		}
	}

	evidenceID := idgen.NewULID()
	recordedAt := input.RecordedAt.UTC()
	digest := sha256.Sum256([]byte(input.Content))
	contentDigest := fmt.Sprintf("%x", digest[:])
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_evidence(
			id, source_type, external_source_id, source_session_id, speaker, ordinal,
			content, occurred_at, recorded_at, content_digest
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evidenceID,
		string(input.SourceType),
		input.ExternalSourceID,
		input.SourceSessionID,
		input.Speaker,
		input.Ordinal,
		input.Content,
		evidenceNullableMicros(input.OccurredAt),
		recordedAt.UnixMicro(),
		contentDigest,
	); err != nil {
		return Evidence{}, fmt.Errorf("memory: insert evidence: %w", err)
	}

	eventID := idgen.NewULID()
	var eventSeq int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO memory_evidence_events(
			event_id, evidence_id, source_type, action, recorded_at, reason_code, request_id
		) VALUES (?, ?, ?, 'append', ?, 'append', ?)
		RETURNING seq`,
		eventID, evidenceID, string(input.SourceType), recordedAt.UnixMicro(), evidenceID,
	).Scan(&eventSeq); err != nil {
		return Evidence{}, fmt.Errorf("memory: append evidence lifecycle event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_evidence_heads(evidence_id, state, last_seq, revision, changed_at)
		VALUES (?, 'active', ?, 1, ?)`, evidenceID, eventSeq, recordedAt.UnixMicro()); err != nil {
		return Evidence{}, fmt.Errorf("memory: create evidence head: %w", err)
	}
	return Evidence{
		ID:               evidenceID,
		ExternalSourceID: input.ExternalSourceID,
		SourceType:       input.SourceType,
		SourceSessionID:  input.SourceSessionID,
		Speaker:          input.Speaker,
		Ordinal:          input.Ordinal,
		Content:          input.Content,
		OccurredAt:       cloneTime(input.OccurredAt),
		RecordedAt:       recordedAt,
		ContentDigest:    contentDigest,
		State:            EvidenceActive,
		Revision:         1,
	}, nil
}

func findEvidenceByExternalID(ctx context.Context, tx *sql.Tx, input EvidenceInput) (Evidence, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT `+evidenceSelectColumns+`
		FROM memory_evidence AS e
		JOIN memory_evidence_heads AS h ON h.evidence_id = e.id
		WHERE e.source_type = ? AND e.source_session_id = ? AND e.external_source_id = ?`,
		string(input.SourceType), input.SourceSessionID, input.ExternalSourceID)
	evidence, err := scanEvidence(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Evidence{}, store.ErrNotFound
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("memory: find evidence by external source ID: %w", err)
	}
	return evidence, nil
}

func sameEvidencePayload(existing Evidence, input EvidenceInput) bool {
	return existing.SourceType == input.SourceType &&
		existing.SourceSessionID == input.SourceSessionID &&
		existing.Speaker == input.Speaker &&
		existing.Ordinal == input.Ordinal &&
		existing.Content == input.Content &&
		sameOptionalTime(existing.OccurredAt, input.OccurredAt)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || left.IsZero() {
		return right == nil || right.IsZero()
	}
	if right == nil || right.IsZero() {
		return false
	}
	return left.UTC().UnixMicro() == right.UTC().UnixMicro()
}

// Get returns only active Evidence. Tombstoned and purged records return
// distinct sentinel errors so a caller cannot accidentally treat audit state
// as usable source material.
func (s *LedgerStore) Get(ctx context.Context, evidenceID string) (*Evidence, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+evidenceSelectColumns+`
		FROM memory_evidence AS e
		JOIN memory_evidence_heads AS h ON h.evidence_id = e.id
		WHERE e.id = ?`, evidenceID)
	evidence, err := scanEvidence(row)
	if err == nil {
		if evidence.State != EvidenceActive {
			return nil, evidenceStateError(evidenceID, evidence.State)
		}
		return &evidence, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("memory: get evidence %q: %w", evidenceID, err)
	}

	state, stateErr := evidenceHeadState(ctx, s.db, evidenceID)
	if stateErr == nil {
		return nil, evidenceStateError(evidenceID, state)
	}
	if errors.Is(stateErr, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return nil, fmt.Errorf("memory: get evidence head %q: %w", evidenceID, stateErr)
}

// GetMany loads active Evidence in bounded SQL batches. Any missing or
// unavailable source fails the whole request; callers cannot mistake a partial
// map for a complete provenance chain.
func (s *LedgerStore) GetMany(ctx context.Context, evidenceIDs []string) (map[string]Evidence, error) {
	unique := uniqueNonEmptyStrings(evidenceIDs)
	if len(unique) == 0 {
		return map[string]Evidence{}, nil
	}
	out := make(map[string]Evidence, len(unique))
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
		rows, err := s.db.QueryContext(ctx, `
			SELECT
				COALESCE(e.id, h.evidence_id),
				COALESCE(e.external_source_id, ''),
				COALESCE(e.source_type, ''),
				COALESCE(e.source_session_id, ''),
				COALESCE(e.speaker, ''),
				COALESCE(e.ordinal, 0),
				COALESCE(e.content, ''),
				e.occurred_at,
				COALESCE(e.recorded_at, 0),
				COALESCE(e.content_digest, ''),
				h.state,
				h.revision
			FROM memory_evidence_heads AS h
			LEFT JOIN memory_evidence AS e ON e.id = h.evidence_id
			WHERE h.evidence_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("memory: get evidence batch: %w", err)
		}
		for rows.Next() {
			evidence, err := scanEvidence(rows)
			if err != nil {
				rows.Close() //nolint:errcheck
				return nil, fmt.Errorf("memory: scan evidence batch: %w", err)
			}
			if evidence.State != EvidenceActive {
				rows.Close() //nolint:errcheck
				return nil, evidenceStateError(evidence.ID, evidence.State)
			}
			if evidence.Content == "" || evidence.ContentDigest == "" {
				rows.Close() //nolint:errcheck
				return nil, fmt.Errorf("%w: active evidence %q has no content row", ErrEvidenceUnavailable, evidence.ID)
			}
			out[evidence.ID] = evidence
		}
		if err := rows.Err(); err != nil {
			rows.Close() //nolint:errcheck
			return nil, fmt.Errorf("memory: iterate evidence batch: %w", err)
		}
		rows.Close() //nolint:errcheck
	}
	for _, evidenceID := range unique {
		if _, ok := out[evidenceID]; !ok {
			return nil, store.ErrNotFound
		}
	}
	return out, nil
}

// ListSession returns Evidence in deterministic message order. Tombstoned
// source material is included only when explicitly requested; purged material
// has no content row and is never returned.
func (s *LedgerStore) ListSession(ctx context.Context, sourceSessionID string, includeTombstoned bool) ([]Evidence, error) {
	stateClause := `h.state = 'active'`
	if includeTombstoned {
		stateClause = `h.state IN ('active', 'tombstoned')`
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+evidenceSelectColumns+`
		FROM memory_evidence AS e
		JOIN memory_evidence_heads AS h ON h.evidence_id = e.id
		WHERE e.source_session_id = ? AND `+stateClause+`
		ORDER BY e.ordinal ASC, e.recorded_at ASC, e.id ASC`, sourceSessionID)
	if err != nil {
		return nil, fmt.Errorf("memory: list evidence session %q: %w", sourceSessionID, err)
	}
	defer rows.Close() //nolint:errcheck

	var evidence []Evidence
	for rows.Next() {
		item, err := scanEvidence(rows)
		if err != nil {
			return nil, fmt.Errorf("memory: scan session evidence: %w", err)
		}
		evidence = append(evidence, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: list evidence session rows: %w", err)
	}
	return evidence, nil
}

// Tombstone immediately removes Evidence from active source recovery while
// retaining its canonical content for an explicit Restore. Projection
// invalidation is added with the projection store; this method owns the
// append-only lifecycle transition itself.
func (s *LedgerStore) Tombstone(ctx context.Context, req LifecycleRequest) error {
	return s.transition(ctx, req, "tombstone")
}

// Restore makes tombstoned Evidence active again. It does not reactivate a
// derived projection: builders must revalidate and rebuild those views.
func (s *LedgerStore) Restore(ctx context.Context, req LifecycleRequest) error {
	return s.transition(ctx, req, "restore")
}

func (s *LedgerStore) transition(ctx context.Context, req LifecycleRequest, action string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: nil ledger store", ErrEvidenceState)
	}
	if req.EvidenceID == "" {
		return fmt.Errorf("%w: missing evidence ID", ErrEvidenceState)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory: begin evidence %s: %w", action, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	state, revision, sourceType, err := lifecycleHead(ctx, tx, req.EvidenceID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("memory: load evidence %q for %s: %w", req.EvidenceID, action, err)
	}
	if state == EvidencePurged {
		return fmt.Errorf("%w: %s", ErrEvidencePurged, req.EvidenceID)
	}
	if req.RequestID != "" {
		previousAction, err := lifecycleActionByRequestID(ctx, tx, req.EvidenceID, req.RequestID)
		if err == nil {
			if previousAction == action {
				return nil
			}
			return fmt.Errorf("%w: request %q already recorded as %q", ErrEvidenceState, req.RequestID, previousAction)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("memory: read lifecycle request %q: %w", req.RequestID, err)
		}
	}

	var wanted EvidenceState
	switch action {
	case "tombstone":
		if state != EvidenceActive {
			return fmt.Errorf("%w: tombstone requires active evidence %q, got %q", ErrEvidenceState, req.EvidenceID, state)
		}
		wanted = EvidenceTombstoned
	case "restore":
		if state != EvidenceTombstoned {
			return fmt.Errorf("%w: restore requires tombstoned evidence %q, got %q", ErrEvidenceState, req.EvidenceID, state)
		}
		wanted = EvidenceActive
	default:
		return fmt.Errorf("%w: unknown action %q", ErrEvidenceState, action)
	}

	now := time.Now().UTC().UnixMicro()
	eventID := idgen.NewULID()
	var eventSeq int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO memory_evidence_events(
			event_id, evidence_id, source_type, action, recorded_at, reason_code, request_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING seq`,
		eventID, req.EvidenceID, string(sourceType), action, now, req.ReasonCode, req.RequestID,
	).Scan(&eventSeq); err != nil {
		return fmt.Errorf("memory: append evidence %s event: %w", action, err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_evidence_heads
		SET state = ?, last_seq = ?, revision = revision + 1, changed_at = ?
		WHERE evidence_id = ? AND state = ? AND revision = ?`,
		string(wanted), eventSeq, now, req.EvidenceID, string(state), revision,
	)
	if err != nil {
		return fmt.Errorf("memory: update evidence %s head: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: evidence %s rows affected: %w", action, err)
	}
	if changed != 1 {
		return fmt.Errorf("%w: concurrent transition for evidence %q", ErrEvidenceState, req.EvidenceID)
	}
	if action == "tombstone" {
		if err := markProjectionsStaleWithoutActiveSourcesTx(ctx, tx, []string{req.EvidenceID}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory: commit evidence %s: %w", action, err)
	}
	return nil
}

// Purge removes canonical content and every projection directly derived from
// it. Unlike tombstone, a purge is irreversible: its Evidence head remains as
// content-free audit state solely to prevent ID reuse and explain the result.
func (s *LedgerStore) Purge(ctx context.Context, req LifecycleRequest) (PurgeResult, error) {
	if s == nil || s.db == nil {
		return PurgeResult{}, fmt.Errorf("%w: nil ledger store", ErrEvidenceState)
	}
	if req.EvidenceID == "" {
		return PurgeResult{}, fmt.Errorf("%w: missing evidence ID", ErrEvidenceState)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		return PurgeResult{}, fmt.Errorf("memory: enable secure delete: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("memory: begin evidence purge: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	state, revision, sourceType, err := lifecycleHead(ctx, tx, req.EvidenceID)
	if errors.Is(err, sql.ErrNoRows) {
		return PurgeResult{}, store.ErrNotFound
	}
	if err != nil {
		return PurgeResult{}, fmt.Errorf("memory: load evidence %q for purge: %w", req.EvidenceID, err)
	}
	if state == EvidencePurged {
		if err := tx.Commit(); err != nil {
			return PurgeResult{}, fmt.Errorf("memory: commit idempotent purge: %w", err)
		}
		return s.finishPurgeCheckpoint(ctx, req.EvidenceID)
	}
	if req.RequestID != "" {
		previousAction, err := lifecycleActionByRequestID(ctx, tx, req.EvidenceID, req.RequestID)
		if err == nil {
			if previousAction == "purge" {
				if err := tx.Commit(); err != nil {
					return PurgeResult{}, fmt.Errorf("memory: commit idempotent purge: %w", err)
				}
				return s.finishPurgeCheckpoint(ctx, req.EvidenceID)
			}
			return PurgeResult{}, fmt.Errorf("%w: request %q already recorded as %q", ErrEvidenceState, req.RequestID, previousAction)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PurgeResult{}, fmt.Errorf("memory: read purge request %q: %w", req.RequestID, err)
		}
	}
	if sourceType == "" {
		return PurgeResult{}, fmt.Errorf("%w: active evidence %q has no source type", ErrEvidenceState, req.EvidenceID)
	}
	if err := purgeProjectionClosureTx(ctx, tx, req.EvidenceID); err != nil {
		return PurgeResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_evidence WHERE id = ?`, req.EvidenceID); err != nil {
		return PurgeResult{}, fmt.Errorf("memory: delete purged evidence content: %w", err)
	}
	now := time.Now().UTC().UnixMicro()
	var eventSeq int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO memory_evidence_events(
			event_id, evidence_id, source_type, action, recorded_at, reason_code, request_id
		) VALUES (?, ?, ?, 'purge', ?, ?, ?)
		RETURNING seq`,
		idgen.NewULID(), req.EvidenceID, string(sourceType), now, req.ReasonCode, req.RequestID,
	).Scan(&eventSeq); err != nil {
		return PurgeResult{}, fmt.Errorf("memory: append purge event: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_evidence_heads
		SET state = 'purged', last_seq = ?, revision = revision + 1, changed_at = ?
		WHERE evidence_id = ? AND state = ? AND revision = ?`,
		eventSeq, now, req.EvidenceID, string(state), revision,
	)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("memory: update purged evidence head: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return PurgeResult{}, fmt.Errorf("memory: purge rows affected: %w", err)
	}
	if changed != 1 {
		return PurgeResult{}, fmt.Errorf("%w: concurrent purge for evidence %q", ErrEvidenceState, req.EvidenceID)
	}
	if err := tx.Commit(); err != nil {
		return PurgeResult{}, fmt.Errorf("memory: commit evidence purge: %w", err)
	}
	return s.finishPurgeCheckpoint(ctx, req.EvidenceID)
}

func (s *LedgerStore) finishPurgeCheckpoint(ctx context.Context, evidenceID string) (PurgeResult, error) {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return PurgeResult{EvidenceID: evidenceID, Purged: true, CheckpointPending: true},
			fmt.Errorf("%w: %v", ErrPurgeIncomplete, err)
	}
	return PurgeResult{EvidenceID: evidenceID, Purged: true}, nil
}

func markProjectionsStaleWithoutActiveSourcesTx(ctx context.Context, tx *sql.Tx, evidenceIDs []string) error {
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
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_projections AS p
			SET state = 'stale', revision = revision + 1
			WHERE p.state = 'active'
			  AND p.id IN (
				SELECT DISTINCT projection_id
				FROM memory_projection_sources
				WHERE evidence_id IN (`+strings.Join(placeholders, ",")+`)
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM memory_projection_sources AS ps
				JOIN memory_evidence_heads AS h ON h.evidence_id = ps.evidence_id
				WHERE ps.projection_id = p.id AND h.state = 'active'
			  )`, args...); err != nil {
			return fmt.Errorf("memory: stale projections after source tombstone: %w", err)
		}
	}
	return nil
}

func purgeProjectionClosureTx(ctx context.Context, tx *sql.Tx, evidenceID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT p.id, p.kind, p.object_key
		FROM memory_projections AS p
		JOIN memory_projection_sources AS ps ON ps.projection_id = p.id
		WHERE ps.evidence_id = ?
		ORDER BY p.id`, evidenceID)
	if err != nil {
		return fmt.Errorf("memory: read purge projection closure: %w", err)
	}
	type projectionTarget struct {
		id, kind, objectKey string
	}
	var targets []projectionTarget
	for rows.Next() {
		var target projectionTarget
		if err := rows.Scan(&target.id, &target.kind, &target.objectKey); err != nil {
			rows.Close() //nolint:errcheck
			return fmt.Errorf("memory: scan purge projection closure: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //nolint:errcheck
		return fmt.Errorf("memory: iterate purge projection closure: %w", err)
	}
	rows.Close() //nolint:errcheck

	for _, target := range targets {
		if target.kind == string(ProjectionAtomicFact) {
			var entryName string
			err := tx.QueryRowContext(ctx, `SELECT name FROM memory_entries WHERE id = ?`, target.objectKey).Scan(&entryName)
			if err == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE id = ?`, target.objectKey); err != nil {
					return fmt.Errorf("memory: delete purged fact %q: %w", entryName, err)
				}
				if err := deleteDerivedTx(ctx, tx, entryName); err != nil {
					return err
				}
				if err := clearReverseSupersessionTx(ctx, tx, entryName); err != nil {
					return err
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("memory: read purged fact %q: %w", target.objectKey, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_projections WHERE id = ?`, target.id); err != nil {
			return fmt.Errorf("memory: delete purged projection %q: %w", target.id, err)
		}
	}
	return nil
}

func lifecycleHead(ctx context.Context, tx *sql.Tx, evidenceID string) (EvidenceState, int64, EvidenceSourceType, error) {
	var state, sourceType string
	var revision int64
	err := tx.QueryRowContext(ctx, `
		SELECT h.state, h.revision, COALESCE(e.source_type, '')
		FROM memory_evidence_heads AS h
		LEFT JOIN memory_evidence AS e ON e.id = h.evidence_id
		WHERE h.evidence_id = ?`, evidenceID).Scan(&state, &revision, &sourceType)
	if err != nil {
		return "", 0, "", err
	}
	return EvidenceState(state), revision, EvidenceSourceType(sourceType), nil
}

func lifecycleActionByRequestID(ctx context.Context, tx *sql.Tx, evidenceID, requestID string) (string, error) {
	var action string
	err := tx.QueryRowContext(ctx, `
		SELECT action FROM memory_evidence_events
		WHERE evidence_id = ? AND request_id = ?
		ORDER BY seq DESC LIMIT 1`, evidenceID, requestID).Scan(&action)
	return action, err
}

func scanEvidence(scanner interface{ Scan(...any) error }) (Evidence, error) {
	var evidence Evidence
	var sourceType, state string
	var occurredAt sql.NullInt64
	var recordedAt int64
	if err := scanner.Scan(
		&evidence.ID,
		&evidence.ExternalSourceID,
		&sourceType,
		&evidence.SourceSessionID,
		&evidence.Speaker,
		&evidence.Ordinal,
		&evidence.Content,
		&occurredAt,
		&recordedAt,
		&evidence.ContentDigest,
		&state,
		&evidence.Revision,
	); err != nil {
		return Evidence{}, err
	}
	evidence.SourceType = EvidenceSourceType(sourceType)
	evidence.State = EvidenceState(state)
	evidence.OccurredAt = evidenceFromNullableMicros(occurredAt)
	evidence.RecordedAt = time.UnixMicro(recordedAt).UTC()
	return evidence, nil
}

func evidenceHeadState(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, evidenceID string) (EvidenceState, error) {
	var state string
	err := q.QueryRowContext(ctx, `SELECT state FROM memory_evidence_heads WHERE evidence_id = ?`, evidenceID).Scan(&state)
	if err != nil {
		return "", err
	}
	return EvidenceState(state), nil
}

func evidenceStateError(evidenceID string, state EvidenceState) error {
	switch state {
	case EvidenceTombstoned:
		return fmt.Errorf("%w: %s", ErrEvidenceUnavailable, evidenceID)
	case EvidencePurged:
		return fmt.Errorf("%w: %s", ErrEvidencePurged, evidenceID)
	default:
		return fmt.Errorf("%w: evidence %s has invalid state %q", ErrEvidenceUnavailable, evidenceID, state)
	}
}

func evidenceNullableMicros(value *time.Time) sql.NullInt64 {
	if value == nil || value.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value.UTC().UnixMicro(), Valid: true}
}

func evidenceFromNullableMicros(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := time.UnixMicro(value.Int64).UTC()
	return &timestamp
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
