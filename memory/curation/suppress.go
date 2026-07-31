package curation

import (
	"context"

	"github.com/wallfacers/engram/memory"
)

// Suppressor is the offline default write-time redundancy suppressor for the
// 024 memory-density levers. It decides whether an incoming fact projection is
// a semantic duplicate of an existing fact and should be suppressed before a
// new projection is created — pure offline, no embedding endpoint and no LLM
// required (Constitution I/V, spec FR-001/FR-010).
//
// The decision signal is the same character-trigram Jaccard used by Cluster
// (dedup.go), with a configurable threshold (default DefaultJaccardThreshold,
// 0.7). A cheap upstream pre-filter limits candidate pairs so the exact
// comparison is O(pairs), never O(n²).
//
// Conflict guard (spec FR-003): facts whose event dates disagree are treated as
// a temporal correction/conflict, NOT redundancy — they are never suppressed.
type Suppressor struct {
	threshold float64
}

// NewSuppressor builds an offline suppressor. A threshold <= 0 selects the
// default (DefaultJaccardThreshold).
func NewSuppressor(threshold float64) *Suppressor {
	if threshold <= 0 {
		threshold = DefaultJaccardThreshold
	}
	return &Suppressor{threshold: threshold}
}

// ShouldSuppress reports whether incoming is redundant with existing. It is
// conservative: nil receiver or nil entries never suppress (a suppression
// error must not drop evidence).
func (s *Suppressor) ShouldSuppress(_ context.Context, existing, incoming *memory.Entry) bool {
	if s == nil || existing == nil || incoming == nil {
		return false
	}
	if eventDateConflict(existing, incoming) {
		return false // temporal correction/conflict survives (spec FR-003)
	}
	similarity := jaccard(charTrigrams(normalizeText(existing)), charTrigrams(normalizeText(incoming)))
	return similarity >= s.threshold
}

// eventDateConflict reports whether the two entries name different event dates
// (both known and unequal). Such entries carry contradictory temporal facts and
// must not be merged or suppressed — the newer date may be a correction the
// ledger is required to keep (spec Edge Cases "冲突而非冗余").
func eventDateConflict(a, b *memory.Entry) bool {
	return a.EventDate != nil && b.EventDate != nil && !a.EventDate.Equal(*b.EventDate)
}
