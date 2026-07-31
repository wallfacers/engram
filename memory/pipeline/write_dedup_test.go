package pipeline_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/pipeline"
)

// US1 write-time redundancy suppression (024). TDD: these tests pin the
// engine behavior of the injection point added by T004/T010 against the
// offline curation.Suppressor (T005).

func newDedupPipeline(t *testing.T, es *memory.EntryStore, db *sql.DB, suppress pipeline.RedundancySuppressor, caller pipeline.ModelCaller) *pipeline.Pipeline {
	t.Helper()
	ledger := memory.NewLedgerStore(db)
	return pipeline.New(pipeline.Config{
		Entries:    es,
		Ledger:     ledger,
		Budgets:    memory.DefaultBudgets(),
		Suppressor: suppress,
		Call:       caller,
	})
}

// oneFactCaller returns a model caller that yields exactly the given facts
// (one per call), each bound to the single source id in the prompt.
func oneFactCaller(facts ...string) pipeline.ModelCaller {
	calls := 0
	return func(_ context.Context, _, user string) (string, error) {
		if calls >= len(facts) {
			return "", fmt.Errorf("caller exhausted: %d facts for %d calls", len(facts), calls+1)
		}
		ids := sourceIDsFromPrompt(user)
		if len(ids) != 1 {
			return "", fmt.Errorf("want one source id, got %d", len(ids))
		}
		out := fmt.Sprintf(`{"facts":[{"fact":%q,"source_ids":[%q]}]}`, facts[calls], ids[0])
		calls++
		return out, nil
	}
}

// datedFactCaller is oneFactCaller with an explicit event_date per fact,
// alternating (fact, date) pairs across calls.
func datedFactCaller(factDatePairs ...string) pipeline.ModelCaller {
	if len(factDatePairs)%2 != 0 {
		panic("datedFactCaller needs (fact, date) pairs")
	}
	calls := 0
	return func(_ context.Context, _, user string) (string, error) {
		idx := calls * 2
		if idx >= len(factDatePairs) {
			return "", fmt.Errorf("caller exhausted: %d facts", calls)
		}
		fact, date := factDatePairs[idx], factDatePairs[idx+1]
		ids := sourceIDsFromPrompt(user)
		if len(ids) != 1 {
			return "", fmt.Errorf("want one source id, got %d", len(ids))
		}
		calls++
		return fmt.Sprintf(`{"facts":[{"fact":%q,"event_date":%q,"source_ids":[%q]}]}`, fact, date, ids[0]), nil
	}
}

func TestWriteDedupSuppressesNearDuplicateFact(t *testing.T) {
	// US1 scenario 1: two semantically near-identical descriptions of the same
	// event — the second produces NO new projection; both raw messages stay in
	// the evidence ledger (append-only, nothing lost).
	ctx := context.Background()
	es, db := newStore(t)
	caller := oneFactCaller("小明1月14日开家长会需要提前到校门口集合", "小明1月14日开家长会需要提前到校门口集合了")
	p := newDedupPipeline(t, es, db, curation.NewSuppressor(0), caller)

	first, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-a",
		[]pipeline.Message{{ExternalSourceID: "turn-1", Role: "user", Text: "小明1月14日开家长会需要提前到校门口集合。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if len(first.Entries) != 1 || len(first.Evidence) != 1 {
		t.Fatalf("first ingest = %+v, want 1 entry + 1 evidence", first)
	}

	second, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-b",
		[]pipeline.Message{{ExternalSourceID: "turn-2", Role: "user", Text: "小明1月14日开家长会需要提前到校门口集合(补充)。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if len(second.Entries) != 0 {
		t.Fatalf("near-duplicate second ingest must NOT create a projection, got %d entries", len(second.Entries))
	}
	if len(second.Evidence) != 1 {
		t.Fatalf("second ingest evidence must stay intact, got %d", len(second.Evidence))
	}
	if second.Suppression.Decisions < 1 {
		t.Fatalf("suppression decisions = %d, want >= 1", second.Suppression.Decisions)
	}
	if second.Suppression.Suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1", second.Suppression.Suppressed)
	}

	// Evidence ledger: BOTH sessions keep their raw messages.
	ledger := memory.NewLedgerStore(db)
	for _, sess := range []string{"session-a", "session-b"} {
		all, err := ledger.ListSession(ctx, sess, false)
		if err != nil {
			t.Fatalf("list %s evidence: %v", sess, err)
		}
		if len(all) != 1 {
			t.Fatalf("%s evidence = %d, want 1", sess, len(all))
		}
	}
	// Store holds exactly one projection.
	entries, err := es.List(ctx)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("store entries = %d, want 1 (one projection)", len(entries))
	}
}

func TestWriteDedupDisabledIsByteIdentical(t *testing.T) {
	// US1 scenario 3 / T007: suppression OFF (default, nil suppressor) — the
	// second near-duplicate creates a NEW projection exactly as before.
	ctx := context.Background()
	es, db := newStore(t)
	caller := oneFactCaller("小明1月14日开家长会需要提前到校门口集合", "小明1月14日开家长会需要提前到校门口集合了")
	p := newDedupPipeline(t, es, db, nil, caller)
	first, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-a",
		[]pipeline.Message{{ExternalSourceID: "turn-1", Role: "user", Text: "第一段。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	second, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-b",
		[]pipeline.Message{{ExternalSourceID: "turn-2", Role: "user", Text: "第二段。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if len(second.Entries) != 1 {
		t.Fatalf("suppression disabled must create a new projection, got %d entries", len(second.Entries))
	}
	if second.Suppression.Decisions != 0 || second.Suppression.Suppressed != 0 {
		t.Fatalf("suppression disabled must report zero audit, got %+v", second.Suppression)
	}
	if len(first.Entries) != 1 {
		t.Fatalf("first ingest entries = %d, want 1", len(first.Entries))
	}
	entries, err := es.List(ctx)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("store entries = %d, want 2", len(entries))
	}
}

func TestWriteDedupOfflinePathWithoutEmbedding(t *testing.T) {
	// US1 scenario 2 / T008: the offline Jaccard path works with NO embedding
	// endpoint configured — the pipeline never touches a sidecar.
	ctx := context.Background()
	es, db := newStore(t)
	caller := oneFactCaller("小明1月14日开家长会需要提前到校门口集合", "小明1月14日开家长会需要提前到校门口集合了")
	p := newDedupPipeline(t, es, db, curation.NewSuppressor(0), caller)
	if _, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-a",
		[]pipeline.Message{{ExternalSourceID: "turn-1", Role: "user", Text: "第一段。", Ordinal: 0}}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	second, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-b",
		[]pipeline.Message{{ExternalSourceID: "turn-2", Role: "user", Text: "第二段。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if len(second.Entries) != 0 {
		t.Fatalf("offline path must suppress without any embedding endpoint, got %d entries", len(second.Entries))
	}
}

func TestWriteDedupKeepsConflictingFact(t *testing.T) {
	// US1 scenario (edge) / T009: a conflict — e.g. a date correction — is NOT
	// redundant and MUST create a new projection (spec FR-003). The conflict is
	// carried by the fact's event_date: the suppressor must never collapse two
	// facts that name different event dates.
	ctx := context.Background()
	es, db := newStore(t)
	caller := datedFactCaller(
		"小明1月14日开家长会需要提前到校门口集合", "2026-01-14",
		"小明1月15日开家长会需要提前到校门口集合", "2026-01-15",
	)
	p := newDedupPipeline(t, es, db, curation.NewSuppressor(0), caller)
	first, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-a",
		[]pipeline.Message{{ExternalSourceID: "turn-1", Role: "user", Text: "第一段。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if len(first.Entries) != 1 {
		t.Fatalf("first ingest = %d entries, want 1", len(first.Entries))
	}
	second, err := p.IngestDetailed(ctx, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "session-b",
		[]pipeline.Message{{ExternalSourceID: "turn-2", Role: "user", Text: "日期改了。", Ordinal: 0}})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if len(second.Entries) != 1 {
		t.Fatalf("conflicting (date-corrected) fact must NOT be suppressed, got %d entries", len(second.Entries))
	}
	entries, err := es.List(ctx)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("store entries = %d, want 2 (both facts kept)", len(entries))
	}
}
