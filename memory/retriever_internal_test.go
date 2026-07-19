package memory

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/store"
)

type internalVectorClient struct{}

func (internalVectorClient) Model() string { return "internal" }

func (internalVectorClient) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 0}}, nil
}

func TestAssociativeSignalFailureLogsStage(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := NewEntryStore(s.DB())
	vs := NewVectorStore(s.DB())
	if err := es.Upsert(ctx, &Entry{Name: "neighbor", Content: "neighbor fact"}); err != nil {
		t.Fatalf("entry: %v", err)
	}
	if err := es.PutEntities(ctx, "neighbor", []string{"topic", "neighbor"}); err != nil {
		t.Fatalf("entity: %v", err)
	}
	if err := vs.Put(ctx, "neighbor", "internal", []float32{1, 0}, nowForTest()); err != nil {
		t.Fatalf("vector: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE memory_entity_edges`); err != nil {
		t.Fatalf("drop edge table: %v", err)
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	r := NewRetrieverWithOptions(es, vs, internalVectorClient{}, nil, RetrieverOptions{Associative: true})
	got := r.associativeRanks(ctx, 10, []float32{1, 0}, map[string][]float32{"neighbor": {1, 0}}, []string{"topic"})
	if len(got) != 0 {
		t.Fatalf("failed associative signal = %v, want empty", got)
	}
	if !strings.Contains(logs.String(), "stage=graph_walk") {
		t.Fatalf("log = %q, want graph_walk stage", logs.String())
	}
}

func TestClusterSweepCapsCandidatesAndWarns(t *testing.T) {
	ctx := context.Background()
	es, _ := newGraphStore(t)
	const candidateCount = clusterSweepCap + 1
	edges := make([]EntityEdge, 0, candidateCount)
	fused := make([]embedding.Scored, 0, candidateCount)
	for i := 0; i < candidateCount; i++ {
		name := fmt.Sprintf("cluster-entry-%03d", i)
		entity := fmt.Sprintf("member-%03d", i)
		if err := es.Upsert(ctx, &Entry{Name: name, Content: name}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
		if err := es.PutEntities(ctx, name, []string{entity}); err != nil {
			t.Fatalf("entity %s: %v", name, err)
		}
		edges = append(edges, EntityEdge{A: "root", B: entity, Kind: "co"})
		fused = append(fused, embedding.Scored{Key: name, Score: float64(candidateCount - i)})
	}
	if err := es.UpsertEdges(ctx, edges); err != nil {
		t.Fatalf("edges: %v", err)
	}
	r := NewRetriever(es, nil, nil)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	got := r.clusterSweepCandidates(ctx, fused, []string{"root"})
	if len(got) != clusterSweepCap {
		t.Fatalf("cluster sweep len = %d, want cap %d", len(got), clusterSweepCap)
	}
	if got[0].Key != "cluster-entry-000" {
		t.Fatalf("cluster sweep order = %q first, want fused-score leader", got[0].Key)
	}
	if !strings.Contains(logs.String(), "cluster sweep cap") {
		t.Fatalf("cluster sweep log = %q, want explicit cap warning", logs.String())
	}
}

func nowForTest() (t time.Time) {
	return time.Unix(1_700_000_000, 0).UTC()
}

var _ embedding.Client = internalVectorClient{}
