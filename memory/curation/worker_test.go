package curation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func seed(t *testing.T, es *memory.EntryStore, e *memory.Entry) {
	t.Helper()
	if e.CharCount == 0 {
		e.CharCount = memory.CharCount(e.Content)
	}
	if err := es.Upsert(context.Background(), e); err != nil {
		t.Fatalf("seed %q: %v", e.Name, err)
	}
}

func TestWorkerAppliesEvictAndMerge(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())

	seed(t, es, &memory.Entry{Name: "keep-me", Trigger: "useful", Content: "keep this", Durability: "evergreen", HitCount: 9})
	seed(t, es, &memory.Entry{Name: "evict-me", Trigger: "stale", Content: "obsolete", Durability: "volatile"})
	seed(t, es, &memory.Entry{Name: "dup-a", Trigger: "dupe", Content: "same fact one", Durability: "volatile"})
	seed(t, es, &memory.Entry{Name: "dup-b", Trigger: "dupe", Content: "same fact two", Durability: "volatile"})
	seed(t, es, &memory.Entry{Name: "pinned-user", Trigger: "id", Content: "the user", Pinned: true, Durability: "evergreen"})

	call := func(ctx context.Context, system, user string) (string, error) {
		return `{"evict":["evict-me"],"merge":[{"names":["dup-a","dup-b"],"into":{"name":"dup-a","trigger":"merged","content":"same fact one and two","durability":"volatile","category":"project"}}]}`, nil
	}
	w := NewWorker(es, s.DB(), call, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

	w.RunPass(ctx)

	assertGone(t, es, "evict-me")
	assertGone(t, es, "dup-b")
	assertExists(t, es, "keep-me")
	assertExists(t, es, "pinned-user")
	got := mustGet(t, es, "dup-a")
	if got.Content != "same fact one and two" || got.Trigger != "merged" {
		t.Fatalf("dup-a not merged: %+v", got)
	}
}

func TestWorkerAppliesConflictsAsSupersede(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())

	seed(t, es, &memory.Entry{Name: "old-job", Content: "works at Acme"})
	seed(t, es, &memory.Entry{Name: "new-job", Content: "works at Globex"})
	seed(t, es, &memory.Entry{Name: "merged-a", Content: "fact one"})
	seed(t, es, &memory.Entry{Name: "merged-b", Content: "fact two"})
	seed(t, es, &memory.Entry{Name: "pinned-fact", Content: "protected", Pinned: true})

	w := NewWorker(es, s.DB(), func(context.Context, string, string) (string, error) { return "", nil }, Config{
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	entries := []*memory.Entry{
		mustGet(t, es, "old-job"), mustGet(t, es, "new-job"),
		mustGet(t, es, "merged-a"), mustGet(t, es, "merged-b"),
		mustGet(t, es, "pinned-fact"),
	}
	d := &JudgeDecision{
		Merge: []MergeDecision{{
			Names: []string{"merged-a", "merged-b"},
			Into:  MergedEntry{Name: "merged-a", Trigger: "t", Content: "fact one and two", Durability: "volatile", Category: "project"},
		}},
		Conflicts: []ConflictDecision{
			{Loser: "old-job", Winner: "new-job"},
			{Loser: "pinned-fact", Winner: "new-job"}, // pinned loser must be refused
			{Loser: "merged-b", Winner: "new-job"},    // consumed by the merge → skipped
		},
	}
	if err := w.apply(ctx, d, entries); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Non-destructive suppression: the loser survives, marked superseded.
	got := mustGet(t, es, "old-job")
	if got.SupersededBy != "new-job" {
		t.Fatalf("old-job superseded_by = %q, want new-job", got.SupersededBy)
	}
	assertExists(t, es, "new-job")
	// Pinned loser is protected from suppression.
	if p := mustGet(t, es, "pinned-fact"); p.SupersededBy != "" {
		t.Fatalf("pinned-fact superseded_by = %q, want empty", p.SupersededBy)
	}
	// A name already consumed by the merge is gone; the conflict referencing it
	// is skipped without error.
	assertGone(t, es, "merged-b")
}

func TestBuildJudgeClustersCarriesContentAndTime(t *testing.T) {
	when := time.Date(2023, time.August, 1, 0, 0, 0, 0, time.UTC)
	clusters := [][]*memory.Entry{{
		{Name: "a", Content: "the user lives in Paris"},
		{Name: "b", Content: "the user lives in Berlin", EventStart: &when},
	}}
	got := buildJudgeClusters(clusters)
	if len(got) != 1 || len(got[0].Members) != 2 {
		t.Fatalf("clusters = %+v, want one cluster with two members", got)
	}
	if got[0].Members[0].Name != "a" || got[0].Members[0].Content != "the user lives in Paris" {
		t.Fatalf("member content not carried: %+v", got[0].Members[0])
	}
	if !strings.Contains(got[0].Members[1].When, "2023-08") {
		t.Fatalf("member event time not carried: %+v", got[0].Members[1])
	}
	// Names stay populated for backward compatibility.
	if len(got[0].Names) != 2 {
		t.Fatalf("names not carried: %+v", got[0].Names)
	}
}

func TestResolveConflictsPassAppliesOnlyConflicts(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())

	// A contradictory near-duplicate pair (clusters on trigram similarity).
	seed(t, es, &memory.Entry{Name: "paris-fact", Content: "the user home city location is presently Paris"})
	seed(t, es, &memory.Entry{Name: "berlin-fact", Content: "the user home city location is presently Berlin"})
	// Extra entries the judge also names for evict/merge — these must be ignored
	// by a conflict-only pass.
	seed(t, es, &memory.Entry{Name: "evict-target", Content: "an unrelated trivial note about nothing here"})
	seed(t, es, &memory.Entry{Name: "merge-x", Content: "a distinct duplicated statement about kappa alpha"})
	seed(t, es, &memory.Entry{Name: "merge-y", Content: "a distinct duplicated statement about kappa beta"})

	call := func(context.Context, string, string) (string, error) {
		return `{"evict":["evict-target"],"merge":[{"names":["merge-x","merge-y"],"into":{"name":"merge-x","trigger":"t","content":"merged","durability":"volatile","category":"project"}}],"conflicts":[{"loser":"paris-fact","winner":"berlin-fact"}]}`, nil
	}
	w := NewWorker(es, s.DB(), call, Config{Budgets: memory.DefaultBudgets()}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := w.ResolveConflictsPass(ctx); err != nil {
		t.Fatalf("resolve conflicts: %v", err)
	}

	// The conflict is applied non-destructively.
	if got := mustGet(t, es, "paris-fact"); got.SupersededBy != "berlin-fact" {
		t.Fatalf("paris-fact superseded_by = %q, want berlin-fact", got.SupersededBy)
	}
	// Evict and merge decisions are NOT applied by a conflict-only pass.
	assertExists(t, es, "evict-target")
	assertExists(t, es, "merge-y")
}

func TestWorkerBuildsSynonymEdgesFromStoredEmbeddings(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "a", Trigger: "a", Content: "alpha fact"})
	seed(t, es, &memory.Entry{Name: "b", Trigger: "b", Content: "beta fact"})
	if err := es.PutEntities(ctx, "a", []string{"Alpha"}); err != nil {
		t.Fatalf("entities a: %v", err)
	}
	if err := es.PutEntities(ctx, "b", []string{"Alpha project"}); err != nil {
		t.Fatalf("entities b: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES
			(?,?,?,?,?), (?,?,?,?,?)`,
		"a", "m", 2, embedding.EncodeVector([]float32{1, 0}), 1,
		"b", "m", 2, embedding.EncodeVector([]float32{0.99, 0.01}), 1); err != nil {
		t.Fatalf("insert embeddings: %v", err)
	}
	w := NewWorker(es, s.DB(), func(context.Context, string, string) (string, error) {
		return `{"evict":[],"merge":[]}`, nil
	}, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: time.Minute,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights,
		Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx)

	edges, err := es.NeighborsOf(ctx, []string{"alpha", "alpha project"}, []string{"syn"})
	if err != nil {
		t.Fatalf("syn neighbors: %v", err)
	}
	if len(edges) != 1 || edges[0].A != "alpha" || edges[0].B != "alpha project" || edges[0].Weight < 0.8 {
		t.Fatalf("synonym edges = %+v, want alpha/alpha project cosine > .8", edges)
	}
}

func TestWorkerSynonymEdgesRequireAliasEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "a", Content: "alpha fact"})
	seed(t, es, &memory.Entry{Name: "b", Content: "beta fact"})
	if err := es.PutEntities(ctx, "a", []string{"Alice"}); err != nil {
		t.Fatalf("entities a: %v", err)
	}
	if err := es.PutEntities(ctx, "b", []string{"Camping"}); err != nil {
		t.Fatalf("entities b: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES
			(?,?,?,?,?), (?,?,?,?,?)`,
		"a", "m", 2, embedding.EncodeVector([]float32{1, 0}), 1,
		"b", "m", 2, embedding.EncodeVector([]float32{0.99, 0.01}), 1); err != nil {
		t.Fatalf("insert embeddings: %v", err)
	}
	w := NewWorker(es, s.DB(), func(context.Context, string, string) (string, error) {
		return `{"evict":[],"merge":[]}`, nil
	}, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: time.Minute,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights,
		Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx)

	edges, err := es.NeighborsOf(ctx, []string{"alice", "camping"}, []string{"syn"})
	if err != nil {
		t.Fatalf("syn neighbors: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("unrelated entities became synonym edges: %+v", edges)
	}
}

func TestWorkerRefusesToEvictPinned(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "pinned-user", Content: "the user", Pinned: true, Durability: "evergreen"})
	seed(t, es, &memory.Entry{Name: "filler", Content: "x", Durability: "volatile"})

	call := func(ctx context.Context, system, user string) (string, error) {
		// Judge maliciously/erroneously targets the pinned entry and an unknown name.
		return `{"evict":["pinned-user","ghost"],"merge":[]}`, nil
	}
	w := NewWorker(es, s.DB(), call, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx)

	assertExists(t, es, "pinned-user") // never evicted
}

func TestWorkerFailSafeOnBadJudgeOutput(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "a", Content: "one", Durability: "volatile"})
	seed(t, es, &memory.Entry{Name: "b", Content: "two", Durability: "volatile"})

	call := func(ctx context.Context, system, user string) (string, error) {
		return "the model rambled and produced no json", nil
	}
	w := NewWorker(es, s.DB(), call, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx) // must not panic, must not mutate

	assertExists(t, es, "a")
	assertExists(t, es, "b")
}

func TestWorkerCallErrorIsFailSafe(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "a", Content: "one", Durability: "volatile"})

	call := func(ctx context.Context, system, user string) (string, error) {
		return "", errors.New("model overloaded")
	}
	w := NewWorker(es, s.DB(), call, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx)
	assertExists(t, es, "a")
}

func TestWorkerSkipsMergeOverBudget(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "dup-a", Content: "fact one", Durability: "volatile"})
	seed(t, es, &memory.Entry{Name: "dup-b", Content: "fact two", Durability: "volatile"})

	big := make([]byte, 0)
	for i := 0; i < 5000; i++ {
		big = append(big, 'x')
	}
	call := func(ctx context.Context, system, user string) (string, error) {
		return `{"evict":[],"merge":[{"names":["dup-a","dup-b"],"into":{"name":"dup-a","trigger":"t","content":"` + string(big) + `","durability":"volatile"}}]}`, nil
	}
	budgets := memory.DefaultBudgets() // EntryContentChars=1200
	w := NewWorker(es, s.DB(), call, Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		MaxCandidatesPerPass: 20, ContentSnippetChars: 200, Weights: defaultWeights, Budgets: budgets,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	w.RunPass(ctx)

	// Over-budget merge skipped: both sources survive unchanged.
	assertExists(t, es, "dup-a")
	assertExists(t, es, "dup-b")
}

func TestWorkerWaterLines(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	seed(t, es, &memory.Entry{Name: "a", Trigger: "t", Content: "one", Durability: "volatile"})

	now := time.Unix(1_700_000_000, 0).UTC()
	mk := func(high, manifestBudget int, minInterval time.Duration) *Worker {
		w := NewWorker(es, s.DB(), failCall(t), Config{
			EntryCountHigh: high, MinInterval: minInterval, LeaseTTL: 60 * time.Second,
			ManifestBudgetChars: manifestBudget, MaxCandidatesPerPass: 20, ContentSnippetChars: 200,
			Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		w.nowFn = func() time.Time { return now }
		return w
	}

	// Count water line: count(1) > high(0).
	wc := mk(0, 100000, time.Minute)
	wc.setLastPass(now) // recent, so time-fallback does not mask the count trigger
	if !wc.shouldRun(ctx, now) {
		t.Fatal("count > high should trigger")
	}

	// Below all water lines (count ≤ high, recent pass, manifest under budget) → no run.
	wq := mk(5, 100000, time.Minute)
	wq.setLastPass(now)
	if wq.shouldRun(ctx, now.Add(30*time.Second)) {
		t.Fatal("should not run below every water line")
	}

	// Time-based fallback: count ≤ high but min_interval elapsed since the last pass.
	if !wq.shouldRun(ctx, now.Add(2*time.Minute)) {
		t.Fatal("time-based fallback should trigger once min_interval elapsed")
	}

	// First-ever pass (last is zero) is a time-based trigger even under the count.
	wf := mk(5, 100000, time.Minute)
	if !wf.shouldRun(ctx, now) {
		t.Fatal("a never-run worker should trigger its first pass")
	}

	// Manifest-size water line: count ≤ high, recent pass, but estimated manifest
	// size (name+trigger+overhead) exceeds the tiny budget.
	wm := mk(5, 1, time.Minute)
	wm.setLastPass(now)
	if !wm.shouldRun(ctx, now.Add(30*time.Second)) {
		t.Fatal("manifest-size water line should trigger")
	}
}

func TestWorkerEmptyStoreNeverRuns(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(ctx, store.Options{DSN: ":memory:"})
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	now := time.Unix(1_700_000_000, 0).UTC()
	w := NewWorker(es, s.DB(), failCall(t), Config{
		EntryCountHigh: 0, MinInterval: time.Minute, LeaseTTL: 60 * time.Second,
		ManifestBudgetChars: 1, MaxCandidatesPerPass: 20, ContentSnippetChars: 200,
		Weights: defaultWeights, Budgets: memory.DefaultBudgets(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.nowFn = func() time.Time { return now }
	// Even with last zero (time fallback) and tiny manifest budget, an empty
	// non-pinned set short-circuits to no-run.
	if w.shouldRun(ctx, now.Add(2*time.Minute)) {
		t.Fatal("empty store must never trigger a pass")
	}
}

func failCall(t *testing.T) ModelCaller {
	return func(ctx context.Context, system, user string) (string, error) {
		t.Fatal("model caller must not be invoked")
		return "", nil
	}
}

func assertExists(t *testing.T, es *memory.EntryStore, name string) {
	t.Helper()
	if _, err := es.GetByName(context.Background(), name); err != nil {
		t.Fatalf("expected %q to exist: %v", name, err)
	}
}

func assertGone(t *testing.T, es *memory.EntryStore, name string) {
	t.Helper()
	_, err := es.GetByName(context.Background(), name)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected %q gone, got err=%v", name, err)
	}
}

func mustGet(t *testing.T, es *memory.EntryStore, name string) *memory.Entry {
	t.Helper()
	e, err := es.GetByName(context.Background(), name)
	if err != nil {
		t.Fatalf("get %q: %v", name, err)
	}
	return e
}
