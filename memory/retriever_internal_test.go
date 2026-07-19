package memory

import (
	"bytes"
	"context"
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

func nowForTest() (t time.Time) {
	return time.Unix(1_700_000_000, 0).UTC()
}

var _ embedding.Client = internalVectorClient{}
