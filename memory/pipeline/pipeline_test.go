package pipeline_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

func newStore(t *testing.T) (*memory.EntryStore, *sql.DB) {
	t.Helper()
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return memory.NewEntryStore(s.DB()), s.DB()
}

func staticCaller(out string) pipeline.ModelCaller {
	return func(_ context.Context, _, user string) (string, error) {
		return withPromptSourceIDs(out, user), nil
	}
}

func withPromptSourceIDs(out, user string) string {
	if strings.Contains(out, `"source_ids"`) {
		return out
	}
	ids, err := json.Marshal(sourceIDsFromPrompt(user))
	if err != nil {
		return out
	}
	return strings.ReplaceAll(out, `{"fact":`, `{"source_ids":`+string(ids)+`,"fact":`)
}

func sourceIDsFromPrompt(user string) []string {
	const marker = "source_id="
	var ids []string
	for rest := user; ; {
		start := strings.Index(rest, marker)
		if start < 0 {
			return ids
		}
		rest = rest[start+len(marker):]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ids
		}
		ids = append(ids, rest[:end])
		rest = rest[end+1:]
	}
}

func TestIngestDetailedPersistsEvidenceBeforeExtractionFailure(t *testing.T) {
	ctx := context.Background()
	es, db := newStore(t)
	ledger := memory.NewLedgerStore(db)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call: func(context.Context, string, string) (string, error) {
			return "", fmt.Errorf("model unavailable")
		},
	})
	result, err := p.IngestDetailed(ctx, time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "session-a", []pipeline.Message{
		{ExternalSourceID: "turn-1", Role: "user", Text: "Alice moved to Berlin.", Ordinal: 0},
		{ExternalSourceID: "turn-2", Role: "assistant", Text: "I recorded Alice's move.", Ordinal: 1},
	})
	if err != nil {
		t.Fatalf("ingest detailed model failure: %v", err)
	}
	if len(result.Evidence) != 2 || len(result.Entries) != 0 || len(result.Degraded) != 1 || result.Degraded[0] != "extraction_model_failure" {
		t.Fatalf("model-failure result = %+v", result)
	}
	stored, err := ledger.ListSession(ctx, "session-a", false)
	if err != nil {
		t.Fatalf("list persisted raw evidence: %v", err)
	}
	if len(stored) != 2 || stored[0].Content != "Alice moved to Berlin." || stored[1].Content != "I recorded Alice's move." {
		t.Fatalf("raw evidence after extraction failure = %+v", stored)
	}
}

func TestIngestDetailedRequiresCurrentBatchSourceIDsAndUnionsDuplicateFactLineage(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call: func(_ context.Context, _, user string) (string, error) {
			ids := sourceIDsFromPrompt(user)
			if len(ids) != 1 {
				return "", fmt.Errorf("prompt source IDs = %v, want one", ids)
			}
			return fmt.Sprintf(`{"facts":[{"fact":"Alice moved to Berlin.","source_ids":[%q]}]}`, ids[0]), nil
		},
	})
	first, err := p.IngestDetailed(ctx, time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC), "session-a", []pipeline.Message{
		{ExternalSourceID: "turn-1", Role: "user", Text: "Alice moved to Berlin.", Ordinal: 0},
	})
	if err != nil || len(first.Entries) != 1 || len(first.Evidence) != 1 {
		t.Fatalf("first detailed ingest = %+v, err=%v", first, err)
	}
	second, err := p.IngestDetailed(ctx, time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), "session-b", []pipeline.Message{
		{ExternalSourceID: "turn-2", Role: "user", Text: "Reminder: Alice moved to Berlin.", Ordinal: 0},
	})
	if err != nil {
		t.Fatalf("second detailed ingest: %v", err)
	}
	if len(second.Entries) != 0 || len(second.Evidence) != 1 {
		t.Fatalf("duplicate fact result = %+v, want evidence-only duplicate", second)
	}
	refs, err := es.SourceRefs(ctx, first.Entries[0].ID)
	if err != nil {
		t.Fatalf("read duplicate fact lineage: %v", err)
	}
	if len(refs) != 2 || refs[0].EvidenceID != first.Evidence[0].ID || refs[1].EvidenceID != second.Evidence[0].ID {
		t.Fatalf("duplicate fact lineage = %+v", refs)
	}
}

func TestIngestDetailedRejectsUnknownOrEmptyFactSources(t *testing.T) {
	for _, sourceIDs := range []string{`[]`, `["unknown-source"]`} {
		t.Run(sourceIDs, func(t *testing.T) {
			es, _ := newStore(t)
			p := pipeline.New(pipeline.Config{
				Entries: es,
				Budgets: memory.DefaultBudgets(),
				Call:    staticCaller(`{"facts":[{"fact":"This must not be stored.","source_ids":` + sourceIDs + `}]}`),
			})
			result, err := p.IngestDetailed(context.Background(), time.Now().UTC(), "session-a", []pipeline.Message{
				{ExternalSourceID: "turn-1", Role: "user", Text: "A source exists.", Ordinal: 0},
			})
			if err != nil {
				t.Fatalf("ingest detailed invalid source IDs: %v", err)
			}
			if len(result.Evidence) != 1 || len(result.Entries) != 0 {
				t.Fatalf("invalid source IDs result = %+v", result)
			}
			entries, err := es.List(context.Background())
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid source IDs stored entries=%+v err=%v", entries, err)
			}
		})
	}
}

func TestIngest_ExtractsFactsAndEntities(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	onWriteFired := false
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		OnWrite: func() { onWriteFired = true },
		Call: staticCaller(`Here you go:
{"facts":[
 {"fact":"The user moved from Sweden in 2019.","entities":["Sweden"],"event_date":"2019-01-01","category":"user","durability":"evergreen"},
 {"fact":"The user prefers Python.","entities":["Python"],"category":"preference","durability":"evergreen"}
]}`),
	})
	n, err := p.Ingest(ctx, time.Date(2023, 5, 20, 0, 0, 0, 0, time.UTC), "sess1",
		[]pipeline.Message{{Role: "user", Text: "I moved from Sweden four years ago and I love Python."}})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 facts, got %d", n)
	}
	if !onWriteFired {
		t.Fatal("expected onWrite to fire")
	}
	entries, _ := es.List(ctx)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Provenance + event date recorded.
	var sawEvent bool
	for _, e := range entries {
		if e.FactSource != "extraction" {
			t.Fatalf("fact_source: got %q", e.FactSource)
		}
		if e.EventDate != nil {
			sawEvent = true
		}
	}
	if !sawEvent {
		t.Fatal("expected at least one entry with an event date")
	}
	// Entity index populated.
	counts, _ := es.EntityMatchCounts(ctx, memory.EntityQueryTokens("Sweden"))
	if len(counts) == 0 {
		t.Fatal("expected an entity match for Sweden")
	}
}

func TestIngestStoresEventRangeAndAliases(t *testing.T) {
	ctx := context.Background()
	es, db := newStore(t)
	var systemPrompt string
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call: func(_ context.Context, system, user string) (string, error) {
			systemPrompt = system
			return withPromptSourceIDs(`{"facts":[{"fact":"The user bought a fitness tracker.","entities":["fitness tracker"],"event_date":"2023-05-02","event_start":"2023-05-01","event_end":"2023-05-03","aliases":["step counter","activity band"],"category":"event","durability":"volatile"}]}`, user), nil
		},
	})

	if n, err := p.Ingest(ctx, time.Date(2023, 5, 20, 0, 0, 0, 0, time.UTC), "session-range", []pipeline.Message{{Role: "user", Text: "I bought a fitness tracker."}}); err != nil || n != 1 {
		t.Fatalf("ingest: n=%d err=%v", n, err)
	}
	if !strings.Contains(systemPrompt, "event_start") || !strings.Contains(systemPrompt, "event_end") || !strings.Contains(systemPrompt, "aliases") {
		t.Fatalf("extraction prompt omits temporal/alias fields: %q", systemPrompt)
	}
	entries, err := es.List(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: got %d err=%v", len(entries), err)
	}
	start := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 5, 3, 0, 0, 0, 0, time.UTC)
	if entries[0].EventStart == nil || !entries[0].EventStart.Equal(start) || entries[0].EventEnd == nil || !entries[0].EventEnd.Equal(end) {
		t.Fatalf("event range: start=%v end=%v", entries[0].EventStart, entries[0].EventEnd)
	}
	var aliases int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM memory_event_aliases WHERE entry_name = ?`, entries[0].Name).Scan(&aliases); err != nil {
		t.Fatalf("alias count: %v", err)
	}
	if aliases != 2 {
		t.Fatalf("alias count = %d, want 2", aliases)
	}
}

func TestIngestIgnoresMalformedEventRange(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"The user attended a meeting.","event_start":"not-a-date","event_end":"2023-xx-99","aliases":["sync"],"category":"event"}]}`),
	})
	if n, err := p.Ingest(ctx, time.Now().UTC(), "session-bad-range", []pipeline.Message{{Role: "user", Text: "I attended a meeting."}}); err != nil || n != 1 {
		t.Fatalf("malformed range should not reject fact: n=%d err=%v", n, err)
	}
	entries, err := es.List(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: got %d err=%v", len(entries), err)
	}
	if entries[0].EventStart != nil || entries[0].EventEnd != nil {
		t.Fatalf("malformed range should remain empty: %+v", entries[0])
	}
}

func TestIngest_ADDOnlyDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"The user lives in Berlin.","category":"user","durability":"volatile"}]}`),
	})
	if _, err := p.Ingest(ctx, time.Now(), "s", []pipeline.Message{{Role: "user", Text: "I live in Berlin"}}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	// A contradicting later fact is stored as a NEW entry; the first survives.
	p2 := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"The user lives in Munich.","category":"user","durability":"volatile"}]}`),
	})
	if _, err := p2.Ingest(ctx, time.Now(), "s", []pipeline.Message{{Role: "user", Text: "Actually Munich now"}}); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	entries, _ := es.List(ctx)
	if len(entries) != 2 {
		t.Fatalf("ADD-only should keep both, got %d entries", len(entries))
	}
}

func TestIngest_MalformedOutputIsNoOp(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`this is not json at all`),
	})
	n, err := p.Ingest(ctx, time.Now(), "s", []pipeline.Message{{Role: "user", Text: "hello"}})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 written, got %d", n)
	}
	entries, _ := es.List(ctx)
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
}

func TestIngest_TrivialBatchNoCall(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	called := false
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call: func(_ context.Context, _, _ string) (string, error) {
			called = true
			return `{"facts":[]}`, nil
		},
	})
	if _, err := p.Ingest(ctx, time.Now(), "s", []pipeline.Message{{Role: "user", Text: "   "}}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if called {
		t.Fatal("expected no model call for a trivial batch")
	}
}

func TestNew_InertWithoutCaller(t *testing.T) {
	es, _ := newStore(t)
	if p := pipeline.New(pipeline.Config{Entries: es, Call: nil}); p != nil {
		t.Fatal("expected nil pipeline without a caller")
	}
	// nil pipeline Ingest is a safe no-op.
	var nilP *pipeline.Pipeline
	if n, err := nilP.Ingest(context.Background(), time.Now(), "s", nil); n != 0 || err != nil {
		t.Fatalf("nil ingest: n=%d err=%v", n, err)
	}
}

func TestIngest_AgentFactsFirstClass(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"The assistant booked a flight to Tokyo for the user.","entities":["Tokyo"],"category":"agent","durability":"volatile"}]}`),
	})
	n, err := p.Ingest(ctx, time.Now(), "s",
		[]pipeline.Message{{Role: "assistant", Text: "I've booked your flight to Tokyo."}})
	if err != nil || n != 1 {
		t.Fatalf("expected 1 agent fact, got n=%d err=%v", n, err)
	}
}

func TestIngestBuildsCooccurrenceEdges(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"Alpha and Beta are related.","entities":[" Alpha ","BETA"]}]}`),
	})
	if _, err := p.Ingest(ctx, time.Now(), "s", []pipeline.Message{{Role: "user", Text: "Alpha and Beta"}}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	edges, err := es.NeighborsOf(ctx, []string{"alpha"}, []string{"co"})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(edges) != 1 || edges[0].A != "alpha" || edges[0].B != "beta" || edges[0].Weight != 1 {
		t.Fatalf("co-occurrence edges = %+v, want alpha/beta weight 1", edges)
	}
}

func TestIngestDuplicateFactDoesNotIncrementCooccurrenceEdge(t *testing.T) {
	ctx := context.Background()
	es, _ := newStore(t)
	p := pipeline.New(pipeline.Config{
		Entries: es,
		Budgets: memory.DefaultBudgets(),
		Call:    staticCaller(`{"facts":[{"fact":"Alpha and Beta are related.","entities":["Alpha","Beta"]}]}`),
	})
	messages := []pipeline.Message{{Role: "user", Text: "Alpha and Beta"}}
	if _, err := p.Ingest(ctx, time.Now(), "s", messages); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if _, err := p.Ingest(ctx, time.Now(), "s", messages); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	edges, err := es.NeighborsOf(ctx, []string{"alpha"}, []string{"co"})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(edges) != 1 || edges[0].Weight != 1 {
		t.Fatalf("duplicate fact incremented co-occurrence edge: %+v", edges)
	}
}
