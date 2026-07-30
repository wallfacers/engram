package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wallfacers/engram/store"
)

// RepresentationShadowIndex is a run-dir-scoped, deletable shadow SQLite
// database that holds representation-specific navigation indexes. Each
// representation arm gets its own shadow DB so that the three bake-off arms
// can use the same query, embedding, pool, and candidate budget without
// polluting the production DB.
//
// The shadow index can be safely deleted at any time; it is a pure cache
// derived from the production Evidence Ledger.
type RepresentationShadowIndex struct {
	runDir string
	kind   RepresentationKind
	db     *sql.DB
	store  *store.Store
}

// NewRepresentationShadowIndex creates a shadow index handle. Call Open before use.
func NewRepresentationShadowIndex(runDir string, kind RepresentationKind) *RepresentationShadowIndex {
	return &RepresentationShadowIndex{runDir: runDir, kind: kind}
}

// DBPath returns the path to the shadow index SQLite file.
func (s *RepresentationShadowIndex) DBPath() string {
	return filepath.Join(s.runDir, string(s.kind)+".db")
}

// Open creates or opens the shadow index database. It is idempotent:
// calling Open on an already-open index is a no-op.
func (s *RepresentationShadowIndex) Open(ctx context.Context) error {
	if s.db != nil {
		return nil
	}
	dbPath := s.DBPath()
	// Ensure the run directory exists.
	if err := os.MkdirAll(s.runDir, 0o755); err != nil {
		return fmt.Errorf("representation shadow index: create run dir %q: %w", s.runDir, err)
	}
	st, err := store.Open(ctx, store.Options{DSN: dbPath})
	if err != nil {
		return fmt.Errorf("representation shadow index: open %s %q: %w", s.kind, dbPath, err)
	}
	s.store = st
	s.db = st.DB()

	// Create shadow tables for candidate storage. These are lightweight
	// tables that mirror the candidate structure but are scoped to the
	// shadow DB only.
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS shadow_candidates (
			candidate_id   TEXT PRIMARY KEY,
			kind           TEXT NOT NULL,
			rank           INTEGER NOT NULL,
			score          REAL NOT NULL,
			text           TEXT NOT NULL,
			text_digest    TEXT NOT NULL,
			source_ids     TEXT NOT NULL,
			expanded_from  TEXT NOT NULL,
			expansion_count INTEGER NOT NULL DEFAULT 0,
			pre_cap_tokens  INTEGER NOT NULL DEFAULT 0,
			truncated      INTEGER NOT NULL DEFAULT 0,
			indexed_at     INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("representation shadow index: create table %s: %w", s.kind, err)
	}
	return nil
}

// Close releases the shadow index database connection. It is idempotent:
// calling Close on an already-closed index is a no-op.
func (s *RepresentationShadowIndex) Close() error {
	if s.store == nil {
		return nil
	}
	err := s.store.Close()
	s.db = nil
	s.store = nil
	if err != nil {
		return fmt.Errorf("representation shadow index: close %s: %w", s.kind, err)
	}
	return nil
}

// Delete removes the shadow index database file from disk. It closes the
// connection first if open. This does NOT affect the production DB.
func (s *RepresentationShadowIndex) Delete(ctx context.Context) error {
	_ = ctx
	if s.store != nil {
		_ = s.Close()
	}
	dbPath := s.DBPath()
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("representation shadow index: delete %s %q: %w", s.kind, dbPath, err)
	}
	return nil
}

// IndexCandidates stores rendered candidates in the shadow index. Each
// candidate's text, digest, and metadata are persisted for later bake-off
// comparison and retrieval.
func (s *RepresentationShadowIndex) IndexCandidates(ctx context.Context, candidates []evalRenderedCandidate) error {
	if s.db == nil {
		return fmt.Errorf("representation shadow index: %s not open", s.kind)
	}
	if len(candidates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("representation shadow index: begin tx %s: %w", s.kind, err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC().UnixMicro()
	for _, c := range candidates {
		sourceIDs := joinStrings(c.SourceIDs, ",")
		expandedFrom := joinStrings(c.ExpandedFrom, ",")
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO shadow_candidates(
				candidate_id, kind, rank, score, text, text_digest,
				source_ids, expanded_from, expansion_count,
				pre_cap_tokens, truncated, indexed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.CandidateID, c.Kind, c.Rank, c.Score, c.Text, c.TextDigest,
			sourceIDs, expandedFrom, c.ExpansionCount,
			c.PreCapInputTokens, boolToInt(c.Truncated), now,
		); err != nil {
			return fmt.Errorf("representation shadow index: insert %s candidate %q: %w", s.kind, c.CandidateID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("representation shadow index: commit %s: %w", s.kind, err)
	}
	return nil
}

// CandidateCount returns the number of candidates stored in the shadow index.
func (s *RepresentationShadowIndex) CandidateCount(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("representation shadow index: %s not open", s.kind)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shadow_candidates`).Scan(&count); err != nil {
		return 0, fmt.Errorf("representation shadow index: count %s: %w", s.kind, err)
	}
	return count, nil
}

// GetCandidates retrieves all candidates from the shadow index ordered by rank.
func (s *RepresentationShadowIndex) GetCandidates(ctx context.Context) ([]evalRenderedCandidate, error) {
	if s.db == nil {
		return nil, fmt.Errorf("representation shadow index: %s not open", s.kind)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate_id, kind, rank, score, text, text_digest,
		       source_ids, expanded_from, expansion_count,
		       pre_cap_tokens, truncated
		FROM shadow_candidates
		ORDER BY rank ASC`)
	if err != nil {
		return nil, fmt.Errorf("representation shadow index: query %s: %w", s.kind, err)
	}
	defer rows.Close() //nolint:errcheck

	var candidates []evalRenderedCandidate
	for rows.Next() {
		var c evalRenderedCandidate
		var sourceIDs, expandedFrom string
		var truncated int
		if err := rows.Scan(
			&c.CandidateID, &c.Kind, &c.Rank, &c.Score, &c.Text, &c.TextDigest,
			&sourceIDs, &expandedFrom, &c.ExpansionCount,
			&c.PreCapInputTokens, &truncated,
		); err != nil {
			return nil, fmt.Errorf("representation shadow index: scan %s: %w", s.kind, err)
		}
		c.SourceIDs = splitStrings(sourceIDs, ",")
		c.ExpandedFrom = splitStrings(expandedFrom, ",")
		c.Truncated = truncated != 0
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("representation shadow index: iterate %s: %w", s.kind, err)
	}
	return candidates, nil
}

// joinStrings joins non-empty strings with a separator.
func joinStrings(values []string, sep string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for i := 1; i < len(values); i++ {
		result += sep + values[i]
	}
	return result
}

// splitStrings splits a string by separator, filtering empty parts.
func splitStrings(value string, sep string) []string {
	if value == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i < len(value); i++ {
		if string(value[i]) == sep {
			if i > start {
				parts = append(parts, value[start:i])
			}
			start = i + 1
		}
	}
	if start < len(value) {
		parts = append(parts, value[start:])
	}
	return parts
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
