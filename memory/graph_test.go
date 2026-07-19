package memory

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/wallfacers/engram/store"
)

func newGraphStore(t *testing.T) (*EntryStore, *sql.DB) {
	t.Helper()
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewEntryStore(s.DB()), s.DB()
}

func TestUpsertEdgesNormalizesAccumulatesAndQueriesBothDirections(t *testing.T) {
	ctx := context.Background()
	es, _ := newGraphStore(t)

	if err := es.UpsertEdges(ctx, []EntityEdge{
		{A: " Zed ", B: "ALPHA", Kind: "co", Weight: 1},
		{A: "alpha", B: " zed", Kind: "co", Weight: 2},
		{A: "Beta", B: "ALPHA", Kind: "syn", Weight: 0.84},
	}); err != nil {
		t.Fatalf("upsert edges: %v", err)
	}

	got, err := es.NeighborsOf(ctx, []string{"alpha"}, []string{"co", "syn"})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("neighbors len = %d, want 2: %+v", len(got), got)
	}
	if got[0].A != "alpha" || got[0].B != "beta" || got[0].Kind != "syn" || got[0].Weight != 0.84 {
		t.Fatalf("syn edge = %+v, want normalized alpha/beta weight .84", got[0])
	}
	if got[1].A != "alpha" || got[1].B != "zed" || got[1].Kind != "co" || got[1].Weight != 3 {
		t.Fatalf("co edge = %+v, want normalized alpha/zed weight 3", got[1])
	}

	reverse, err := es.NeighborsOf(ctx, []string{"zed"}, []string{"co"})
	if err != nil {
		t.Fatalf("reverse neighbors: %v", err)
	}
	if len(reverse) != 1 || reverse[0].A != "alpha" || reverse[0].B != "zed" {
		t.Fatalf("reverse lookup = %+v, want alpha/zed", reverse)
	}
}

func TestEntityDocFreqAndDepthTwoWalkUseIDFAndDepthLimit(t *testing.T) {
	ctx := context.Background()
	es, _ := newGraphStore(t)
	for _, e := range []struct {
		name     string
		entities []string
	}{
		{"e1", []string{"common", "rare"}},
		{"e2", []string{"common"}},
		{"e3", []string{"target"}},
		{"e4", []string{"hop-two"}},
	} {
		if err := es.Upsert(ctx, &Entry{Name: e.name, Content: e.name}); err != nil {
			t.Fatalf("upsert %s: %v", e.name, err)
		}
		if err := es.PutEntities(ctx, e.name, e.entities); err != nil {
			t.Fatalf("entities %s: %v", e.name, err)
		}
	}
	if err := es.UpsertEdges(ctx, []EntityEdge{
		{A: "common", B: "target", Kind: "co", Weight: 1},
		{A: "rare", B: "target", Kind: "co", Weight: 1},
		{A: "target", B: "hop-two", Kind: "co", Weight: 1},
	}); err != nil {
		t.Fatalf("upsert graph: %v", err)
	}

	freq, err := es.EntityDocFreq(ctx)
	if err != nil {
		t.Fatalf("doc freq: %v", err)
	}
	if freq["common"] != 2 || freq["rare"] != 1 {
		t.Fatalf("doc freq = %+v, want common=2 rare=1", freq)
	}

	one, err := es.WalkEntityGraph(ctx, []string{"common", "rare"}, 1)
	if err != nil {
		t.Fatalf("depth one: %v", err)
	}
	if _, ok := one["hop-two"]; ok {
		t.Fatalf("depth-one walk crossed into hop-two: %+v", one)
	}
	two, err := es.WalkEntityGraph(ctx, []string{"common", "rare"}, 2)
	if err != nil {
		t.Fatalf("depth two: %v", err)
	}
	if _, ok := two["hop-two"]; !ok {
		t.Fatalf("depth-two walk missed hop-two: %+v", two)
	}
	if got, want := two["target"], 1.5; math.Abs(got-want) > 1e-9 {
		t.Fatalf("target IDF score = %v, want %v", got, want)
	}
}

func TestWalkEntityGraphDoesNotEchoVisitedSeeds(t *testing.T) {
	ctx := context.Background()
	es, _ := newGraphStore(t)
	for _, item := range []struct {
		name   string
		entity string
	}{
		{"entry-a", "a"},
		{"entry-b", "b"},
	} {
		if err := es.Upsert(ctx, &Entry{Name: item.name, Content: item.name}); err != nil {
			t.Fatalf("upsert %s: %v", item.name, err)
		}
		if err := es.PutEntities(ctx, item.name, []string{item.entity}); err != nil {
			t.Fatalf("entities %s: %v", item.name, err)
		}
	}
	if err := es.UpsertEdges(ctx, []EntityEdge{{A: "a", B: "b", Kind: "co", Weight: 1}}); err != nil {
		t.Fatalf("edge: %v", err)
	}
	scores, err := es.WalkEntityGraph(ctx, []string{"a"}, 2)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if _, echoed := scores["a"]; echoed {
		t.Fatalf("walk echoed visited seed: %+v", scores)
	}
}
