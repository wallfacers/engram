package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wallfacers/engram/memory"
)

// Verbatim-chunk union store: alongside the extracted facts, each session's raw
// dialogue is stored as speaker-attributed chunk entries in the SAME store, so
// every retrieval signal (vector, BM25, RRF) sees both representations. This is
// the "chunks ∪ artifacts" configuration from An 2026 (arXiv:2601.00821), which
// matches verbatim-chunk accuracy while extraction alone forfeits 15-30 pp —
// extraction commits to relevance before the question exists; chunks defer that
// decision to query time.
const (
	chunkTargetChars = 900  // soft target per chunk (entry budget is 1200)
	chunkMaxChars    = 1100 // hard cap for a single oversized turn
)

// sessionChunk is one verbatim chunk plus the dialogue ids of the turns packed
// into it, so a retrieved chunk can be resolved back to exact turns for evidence
// recall.
type sessionChunk struct {
	Text   string
	DiaIDs []string
}

// buildSessionChunks splits one session's turns into speaker-attributed chunks
// of at most ~chunkTargetChars code points, never splitting a turn except when
// a single turn alone exceeds chunkMaxChars (then it is truncated). Each chunk
// records the dialogue ids of the turns it contains (blank ids are dropped).
func buildSessionChunks(s session) []sessionChunk {
	var chunks []sessionChunk
	var b strings.Builder
	var diaIDs []string
	size := 0
	flush := func() {
		chunks = append(chunks, sessionChunk{Text: b.String(), DiaIDs: diaIDs})
		b.Reset()
		diaIDs = nil
		size = 0
	}
	for _, t := range s.Turns {
		line := t.Speaker + ": " + t.Text
		if n := utf8.RuneCountInString(line); n > chunkMaxChars {
			line = string([]rune(line)[:chunkMaxChars])
		}
		n := utf8.RuneCountInString(line)
		if size > 0 && size+1+n > chunkTargetChars {
			flush()
		}
		if size > 0 {
			b.WriteByte('\n')
			size++
		}
		b.WriteString(line)
		size += n
		if t.DiaID != "" {
			diaIDs = append(diaIDs, t.DiaID)
		}
	}
	if size > 0 {
		flush()
	}
	return chunks
}

// chunkTrigger derives the single-line manifest trigger from chunk content.
func chunkTrigger(content string) string {
	line := content
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if utf8.RuneCountInString(line) > 100 {
		line = string([]rune(line)[:100])
	}
	return line
}

// retrieveWithQuota reserves `quota` of the topK slots for verbatim chunks.
// The RRF signals are biased toward extracted facts (chunks carry no entities
// and embed diffusely), so without a quota chunks fill only ~0-6% of the top-k
// even when they hold the answer verbatim. A wide fused search is partitioned
// by kind, each side keeps its fused order, and shortfall on either side is
// backfilled from the other. quota <= 0 degrades to a plain Search.
func retrieveWithQuota(ctx context.Context, r *memory.Retriever, query string, topK, quota int) ([]memory.Result, error) {
	hits, _, err := retrieveWithQuotaDiagnostics(ctx, r, query, topK, quota)
	return hits, err
}

func retrieveWithQuotaDiagnostics(ctx context.Context, r *memory.Retriever, query string, topK, quota int) ([]memory.Result, memory.SearchDiagnostics, error) {
	if quota <= 0 {
		return r.SearchWithDiagnostics(ctx, query, topK)
	}
	widePool := topK * 6
	if widePool < 300 {
		widePool = 300
	}
	wide, diagnostics, err := r.SearchWithDiagnostics(ctx, query, widePool)
	if err != nil {
		return nil, diagnostics, err
	}
	return applyChunkQuota(wide, topK, quota), diagnostics, nil
}

// applyChunkQuota partitions a fused result list into facts and chunks, keeps
// topK-quota facts + quota chunks (backfilling shortfall from the other side),
// and restores fused (score-descending) order.
func applyChunkQuota(wide []memory.Result, topK, quota int) []memory.Result {
	var facts, chunks []memory.Result
	for _, h := range wide {
		if strings.HasPrefix(h.Name, "chunk-") {
			chunks = append(chunks, h)
		} else {
			facts = append(facts, h)
		}
	}
	factSlots := topK - quota
	if len(chunks) < quota {
		factSlots = topK - len(chunks)
	}
	if factSlots > len(facts) {
		factSlots = len(facts)
	}
	chunkSlots := topK - factSlots
	if chunkSlots > len(chunks) {
		chunkSlots = len(chunks)
	}
	out := append(facts[:factSlots:factSlots], chunks[:chunkSlots]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// ingestChunks writes one conversation's verbatim chunks as entries. Upsert is
// keyed by deterministic names, so re-running over a persisted store is
// idempotent. Returns a map from each chunk entry name to the dialogue ids its
// text covers (for exact-turn evidence recall) and the number of chunks written.
func ingestChunks(ctx context.Context, es *memory.EntryStore, conv conversation) (map[string][]string, int, error) {
	chunkTurns := make(map[string][]string)
	n := 0
	for _, s := range conv.Sessions {
		var eventDate *time.Time
		if !s.Date.IsZero() {
			d := s.Date
			eventDate = &d
		}
		for i, chunk := range buildSessionChunks(s) {
			name := fmt.Sprintf("chunk-c%d-s%d-%03d", conv.ID, s.Index, i)
			e := &memory.Entry{
				Name:            name,
				Trigger:         chunkTrigger(chunk.Text),
				Content:         chunk.Text,
				Durability:      "volatile",
				Category:        "chunk",
				EventDate:       eventDate,
				FactSource:      "verbatim_chunk",
				SourceSessionID: fmt.Sprintf("conv%d-sess%d", conv.ID, s.Index),
			}
			if err := es.Upsert(ctx, e); err != nil {
				return chunkTurns, n, fmt.Errorf("chunk %s: %w", e.Name, err)
			}
			if len(chunk.DiaIDs) > 0 {
				chunkTurns[name] = chunk.DiaIDs
			}
			n++
		}
	}
	return chunkTurns, n, nil
}
