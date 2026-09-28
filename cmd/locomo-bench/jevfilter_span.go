package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// E-arm lineage span recovery (051 §E, "facts ride with their verbatim
// sources"). Arm E keeps arm D's pipeline byte-identical — the same wide pool,
// the same single Jev filter call, the same degrade semantics — and then
// appends verbatim source spans traced through the v7 evidence ledger:
//
//	kept fact --memory_projection_sources--> evidence --span--> raw dialogue
//
// The spans serve the three failure modes the B/D pilot left on the table:
// enumeration completeness (one span usually names several activities),
// two-hop intersections (both legs' sources ride along), and qualifier
// survival (the qualifier lives in the source text, not the extracted fact).
// Span candidates come from two places, in priority order:
//
//  1. the kept shortlist's lineage (high confidence), then
//  2. the unfiltered RRF pool's head — facts the filter dropped but whose
//     retrieval rank says the source turn is on-topic.
//
// The one arm-level variable E introduces over D is the appended span cards,
// so a D->E contrast stays attributable. Everything here lives in the harness:
// it composes the engine's public EntryStore.SourceRefs / LedgerStore.GetMany
// reads and never reimplements engine retrieval.

const (
	// defaultJevSpanCap is the default number of verbatim span cards arm E
	// appends. Six spans (~30-60 tokens each) keep the whole bundle around the
	// "1/6 of arm B" token budget while covering a four-item enumeration plus
	// the two legs of an intersection question.
	defaultJevSpanCap = 6
	// jevSpanPoolRank is how deep into the unfiltered RRF pool the recovery
	// looks for dropped-but-ranked facts whose sources deserve a span. 30
	// matches the historical top-k anchor: anything the retriever can rank
	// that high is on-topic enough for its source turn to ride along.
	jevSpanPoolRank = 30
)

// spanRecovery carries arm E's lineage-span configuration. A nil receiver (or
// a non-positive cap) disables recovery, which makes an E arm degrade to
// exactly arm D — the knob that keeps an E-vs-D ablation one flag away.
type spanRecovery struct {
	entries *memory.EntryStore
	ledger  *memory.LedgerStore
	cap     int
}

// newSpanRecovery builds the recovery handle from the run's entry store. It
// returns nil (disabled) unless both the store and a positive cap are present,
// collapsing to "no spans" the way the engine collapses unconfigured clients.
func newSpanRecovery(entries *memory.EntryStore, cap int) *spanRecovery {
	if entries == nil || cap <= 0 {
		return nil
	}
	return &spanRecovery{entries: entries, ledger: entries.Ledger(), cap: cap}
}

// enabled reports whether recovery will produce anything.
func (sr *spanRecovery) enabled() bool {
	return sr != nil && sr.entries != nil && sr.ledger != nil && sr.cap > 0
}

// spanWindow is one evidence record's merged citation window. Multiple facts
// citing the same evidence widen the window to the union of their spans; a
// full-source citation always wins over any partial span.
type spanWindow struct {
	full    bool
	merged  bool
	start   int
	end     int
}

// merge widens w to cover ref. Code-point offsets, per the v7 contract
// (projection.go validates spans against len(runes)).
func (w *spanWindow) merge(ref memory.EvidenceRef) {
	if ref.FullSource {
		w.full = true
		return
	}
	if ref.StartChar == nil || ref.EndChar == nil {
		// A partial ref without offsets cannot be located; treat it as the
		// whole record rather than dropping the source silently.
		w.full = true
		return
	}
	if w.full {
		return
	}
	if !w.merged {
		w.start, w.end = *ref.StartChar, *ref.EndChar
		w.merged = true
		return
	}
	w.start = min(w.start, *ref.StartChar)
	w.end = max(w.end, *ref.EndChar)
}

// spanCandidate accumulates one evidence record's window plus its selection
// priority: priority 0 comes from the kept shortlist, priority 1 from the
// unfiltered pool head. rank preserves the pool order within a priority.
type spanCandidate struct {
	window     spanWindow
	priority   int
	rank       int
	ordinal    int
	occurredAt time.Time
	evidenceID string
	evidence   memory.Evidence
}

// recoverSpans resolves the verbatim span cards for one question. kept is the
// filtered shortlist (arm D's shown set before packing); poolTail is the
// unfiltered wide pool in RRF order, used only for the dropped-but-ranked
// head. Entries without an atomic-fact projection — verbatim chunk entries —
// resolve to store.ErrNotFound and are skipped: their Content already IS the
// source text, so presenting them twice would spend tokens twice.
func recoverSpans(ctx context.Context, sr *spanRecovery, kept, pool []memory.Result) ([]memory.Result, error) {
	if !sr.enabled() {
		return nil, nil
	}
	keptIDs := make(map[string]bool, len(kept))
	for _, hit := range kept {
		keptIDs[hit.ID] = true
	}
	candidates := make(map[string]*spanCandidate)
	priorityRank := 0
	collect := func(hit memory.Result, priority int, rank int) error {
		refs, err := sr.entries.SourceRefs(ctx, hit.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil // no atomic-fact projection: chunk entry, already verbatim
			}
			return fmt.Errorf("span recovery: sources for entry %s: %w", hit.ID, err)
		}
		for _, ref := range refs {
			cand := candidates[ref.EvidenceID]
			if cand == nil {
				cand = &spanCandidate{evidenceID: ref.EvidenceID, priority: priority, rank: rank}
				candidates[ref.EvidenceID] = cand
			}
			// A later, higher-priority citation tightens the candidate's origin.
			if priority < cand.priority {
				cand.priority = priority
				cand.rank = rank
			}
			cand.window.merge(ref)
		}
		return nil
	}
	for _, hit := range kept {
		if err := collect(hit, 0, priorityRank); err != nil {
			return nil, err
		}
		priorityRank++
	}
	poolRank := 0
	for _, hit := range pool {
		if poolRank >= jevSpanPoolRank {
			break
		}
		if keptIDs[hit.ID] {
			poolRank++
			continue
		}
		if err := collect(hit, 1, poolRank); err != nil {
			return nil, err
		}
		poolRank++
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	evidence, err := sr.ledger.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("span recovery: evidence batch read: %w", err)
	}
	ordered := make([]*spanCandidate, 0, len(candidates))
	for _, cand := range candidates {
		ev, ok := evidence[cand.evidenceID]
		if !ok || ev.State != memory.EvidenceActive {
			continue
		}
		cand.ordinal = ev.Ordinal
		if ev.OccurredAt != nil {
			cand.occurredAt = *ev.OccurredAt
		} else {
			cand.occurredAt = ev.RecordedAt
		}
		cand.evidence = ev
		ordered = append(ordered, cand)
	}
	// Selection order: shortlist lineage first (that is what the filter vouched
	// for), then the pool head; within a priority the earlier-ranked fact's
	// source wins, and a chronological tiebreak keeps same-priority spans in
	// conversation order so adjacent turns read as a narrative.
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if !a.occurredAt.Equal(b.occurredAt) {
			return a.occurredAt.Before(b.occurredAt)
		}
		return a.ordinal < b.ordinal
	})
	if len(ordered) > sr.cap {
		ordered = ordered[:sr.cap]
	}
	spans := make([]memory.Result, 0, len(ordered))
	for _, cand := range ordered {
		ev := cand.evidence
		card := memory.Result{
			ID:              "span-" + ev.ID,
			Name:            "verbatim source",
			Content:         spanCardText(ev, cand.window),
			EventDate:       ev.OccurredAt,
			SourceSessionID: ev.SourceSessionID,
			ProjectionKind:  memory.ProjectionAtomicFact,
		}
		spans = append(spans, card)
	}
	return spans, nil
}

// spanCardText renders one evidence record as a speaker-attributed span card.
// Offsets are code points; a full-source window renders the whole record. The
// ledger's message ingest already prefixes some content with "Speaker:" — the
// attribution is added only when the span does not already carry it, so a card
// never reads "Melanie: Melanie: ...".
func spanCardText(ev memory.Evidence, w spanWindow) string {
	text := ev.Content
	if !w.full && w.end > w.start {
		if runes := []rune(text); w.end <= len(runes) {
			text = string(runes[w.start:w.end])
		}
	}
	text = strings.TrimSpace(text)
	if ev.Speaker == "" {
		return text
	}
	if strings.HasPrefix(text, ev.Speaker+":") {
		return text
	}
	return ev.Speaker + ": " + text
}
