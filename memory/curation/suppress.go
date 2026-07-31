package curation

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
)

// Suppressor is the default write-time redundancy suppressor for the 024
// memory-density levers. It decides whether an incoming fact projection is a
// semantic duplicate of an existing fact and should be suppressed before a new
// projection is created.
//
// The primary signal is pure offline: character-trigram Jaccard (research.md
// Decision 1, spec FR-001), reusing dedup.go's signal with a configurable
// threshold (default DefaultJaccardThreshold, 0.7). An optional embedding
// overlay (default OFF) can additionally suppress when two contents are
// semantically near-identical but character-distinct — embedding computes live
// vectors for the two contents, no stored-vector dependency. The offline path
// never requires an embedding endpoint (Constitution I/V, FR-010).
//
// Conflict guard (spec FR-003): facts whose event dates disagree are treated as
// a temporal correction/conflict, NOT redundancy — they are never suppressed.
type Suppressor struct {
	threshold    float64
	embedClient  embedding.Client
	embedThresh  float64
}

// NewSuppressor builds a suppressor with the offline Jaccard signal. A
// threshold <= 0 selects the default (DefaultJaccardThreshold).
func NewSuppressor(threshold float64) *Suppressor {
	if threshold <= 0 {
		threshold = DefaultJaccardThreshold
	}
	return &Suppressor{threshold: threshold}
}

// WithEmbedding adds the optional semantic overlay: when the offline Jaccard
// signal is below the suppression threshold but the embedding cosine similarity
// of the two contents is >= embedThreshold (default 0.9, MemOS-style semantic
// near-identity, research.md Decision 1), the pair is suppressed. A nil client
// keeps the suppressor pure-offline. The overlay is OR-ed over the offline
// decision and is deliberately default-off (spec FR-010).
func (s *Suppressor) WithEmbedding(client embedding.Client, embedThreshold float64) *Suppressor {
	if client == nil {
		return s
	}
	if embedThreshold <= 0 {
		embedThreshold = 0.9
	}
	s.embedClient = client
	s.embedThresh = embedThreshold
	return s
}

// ShouldSuppress reports whether incoming is redundant with existing. It is
// conservative: nil receiver or nil entries never suppress (a suppression
// error must not drop evidence).
func (s *Suppressor) ShouldSuppress(ctx context.Context, existing, incoming *memory.Entry) bool {
	if s == nil || existing == nil || incoming == nil {
		return false
	}
	if eventDateConflict(existing, incoming) {
		return false // temporal correction/conflict survives (spec FR-003)
	}
	similarity := jaccard(charTrigrams(suppressionText(existing)), charTrigrams(suppressionText(incoming)))
	if similarity >= s.threshold {
		return true
	}
	// Optional embedding overlay (default off): semantic near-identity that the
	// character signal misses (research.md Decision 1). Any failure degrades to
	// the offline decision — an embedding hiccup must never drop evidence.
	if s.embedClient == nil {
		return false
	}
	sim, err := s.embedSimilarity(ctx, existing.Content, incoming.Content)
	if err != nil || sim < s.embedThresh {
		return false
	}
	return true
}

// embedSimilarity embeds two contents live and returns their cosine similarity.
// Errors (endpoint down, empty vectors) return 0 so the overlay degrades off.
func (s *Suppressor) embedSimilarity(ctx context.Context, a, b string) (float64, error) {
	vecs, err := s.embedClient.Embed(ctx, []string{a, b})
	if err != nil {
		return 0, err
	}
	if len(vecs) != 2 || len(vecs[0]) == 0 || len(vecs[1]) == 0 {
		return 0, fmt.Errorf("embedding overlay returned %d vectors", len(vecs))
	}
	return embedding.Cosine(vecs[0], vecs[1]), nil
}

// suppressionText builds the comparison text for write-time redundancy: the
// fact CONTENT only, lowercased with whitespace collapsed — deliberately NOT
// name/trigger (dedup.normalizeText). Entry names carry a random ULID tail
// (entryName), so folding them into the similarity would inject per-entry noise
// and drown the content signal (engram 024 US1: the redundancy is about the
// fact's meaning, which lives in Content, spec FR-001).
func suppressionText(e *memory.Entry) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range e.Content {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(unicode.ToLower(r))
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// eventDateConflict reports whether the two entries name different event dates
// (both known and unequal). Such entries carry contradictory temporal facts and
// must not be merged or suppressed — the newer date may be a correction the
// ledger is required to keep (spec Edge Cases "冲突而非冗余").
func eventDateConflict(a, b *memory.Entry) bool {
	return a.EventDate != nil && b.EventDate != nil && !a.EventDate.Equal(*b.EventDate)
}
