package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

const parityModel = "fixture-v1"

type parityEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Trigger   string    `json:"trigger"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	Entities  []string  `json:"entities"`
	EventDate string    `json:"event_date"`
	Vector    []float32 `json:"vector"`
	UpdatedAt string    `json:"updated_at"`
}

type parityQuery struct {
	ID          string    `json:"id"`
	Query       string    `json:"query"`
	K           int       `json:"k"`
	Vector      []float32 `json:"vector"`
	Degradation bool      `json:"degradation"`
}

type parityResult struct {
	QueryID    string   `json:"query_id"`
	EntryIDs   []string `json:"entry_ids"`
	NoSemantic []string `json:"no_semantic"`
	NoKeyword  []string `json:"no_keyword"`
	NoEntity   []string `json:"no_entity"`
}

type parityClient struct {
	model   string
	vectors map[string][]float32
	fail    bool
}

func (c *parityClient) Model() string { return c.model }

func (c *parityClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if c.fail {
		return nil, errors.New("fixture embedding failure")
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vector, ok := c.vectors[text]
		if !ok {
			return nil, fmt.Errorf("fixture vector missing for %q", text)
		}
		out[i] = vector
	}
	return out, nil
}

func parityFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate parity fixture directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "parity")
}

func loadParityJSON[T any](t *testing.T, name string, out *T) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parityFixtureDir(t), name))
	if err != nil {
		t.Fatalf("read parity fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode parity fixture %s: %v", name, err)
	}
}

func loadParityFixtures(t *testing.T) ([]parityEntry, []parityQuery, []parityResult) {
	t.Helper()
	var entries []parityEntry
	var queries []parityQuery
	var golden struct {
		Model   string         `json:"model"`
		Results []parityResult `json:"results"`
	}
	loadParityJSON(t, "corpus.json", &entries)
	loadParityJSON(t, "queries.json", &queries)
	loadParityJSON(t, "golden.json", &golden)
	if golden.Model != parityModel {
		t.Fatalf("golden model = %q, want %q", golden.Model, parityModel)
	}
	if len(entries) == 0 || len(queries) == 0 || len(golden.Results) == 0 {
		t.Fatal("parity fixtures must contain corpus, queries, and golden results")
	}
	return entries, queries, golden.Results
}

func newParityStores(t *testing.T, entries []parityEntry) (*store.Store, *memory.EntryStore, *memory.VectorStore) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open parity sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	es := memory.NewEntryStore(s.DB())
	vs := memory.NewVectorStore(s.DB())
	for i, fixture := range entries {
		updatedAt, err := time.Parse(time.RFC3339, fixture.UpdatedAt)
		if err != nil {
			t.Fatalf("parse updated_at for %s: %v", fixture.Name, err)
		}
		eventDate, err := time.Parse(time.RFC3339, fixture.EventDate)
		if err != nil {
			t.Fatalf("parse event_date for %s: %v", fixture.Name, err)
		}
		entry := &memory.Entry{
			ID: fixture.ID, Name: fixture.Name, Trigger: fixture.Trigger,
			Content: fixture.Content, Category: fixture.Category,
			CharCount: len([]rune(fixture.Content)), EventDate: &eventDate,
			CreatedAt: updatedAt.Add(-time.Duration(i+1) * time.Hour), UpdatedAt: updatedAt,
		}
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", fixture.Name, err)
		}
		if err := es.PutEntities(ctx, fixture.Name, fixture.Entities); err != nil {
			t.Fatalf("put entities %s: %v", fixture.Name, err)
		}
		if err := vs.Put(ctx, fixture.Name, parityModel, fixture.Vector, updatedAt); err != nil {
			t.Fatalf("put vector %s: %v", fixture.Name, err)
		}
	}
	return s, es, vs
}

func parityQueryVectors(queries []parityQuery) map[string][]float32 {
	vectors := make(map[string][]float32, len(queries))
	for _, query := range queries {
		vectors[query.Query] = query.Vector
	}
	return vectors
}

func resultEntryIDs(t *testing.T, results []memory.Result, entries []parityEntry) []string {
	t.Helper()
	ids := make(map[string]string, len(entries))
	for _, entry := range entries {
		ids[entry.Name] = entry.ID
	}
	out := make([]string, len(results))
	for i, result := range results {
		id, ok := ids[result.Name]
		if !ok {
			t.Fatalf("retriever returned unknown entry %q", result.Name)
		}
		out[i] = id
	}
	return out
}

func parityResultByQuery(t *testing.T, results []parityResult) map[string]parityResult {
	t.Helper()
	out := make(map[string]parityResult, len(results))
	for _, result := range results {
		if _, exists := out[result.QueryID]; exists {
			t.Fatalf("duplicate golden query %q", result.QueryID)
		}
		out[result.QueryID] = result
	}
	return out
}

func searchParityQuery(t *testing.T, entries []parityEntry, query parityQuery, client embedding.Client) []string {
	t.Helper()
	_, es, vs := newParityStores(t, entries)
	r := memory.NewRetriever(es, vs, client)
	got, err := r.Search(context.Background(), query.Query, query.K)
	if err != nil {
		t.Fatalf("search query %q: %v", query.ID, err)
	}
	return resultEntryIDs(t, got, entries)
}

func TestRetrievalParity(t *testing.T) {
	entries, queries, golden := loadParityFixtures(t)
	goldenByQuery := parityResultByQuery(t, golden)
	client := &parityClient{model: parityModel, vectors: parityQueryVectors(queries)}
	matched := 0
	for _, query := range queries {
		want, ok := goldenByQuery[query.ID]
		if !ok {
			t.Fatalf("missing golden result for query %q", query.ID)
		}
		got := searchParityQuery(t, entries, query, client)
		if !reflect.DeepEqual(got, want.EntryIDs) {
			t.Errorf("query %q (%s): got %v, want %v", query.ID, query.Query, got, want.EntryIDs)
			continue
		}
		matched++
	}
	t.Logf("retrieval parity: %d/%d queries matched", matched, len(queries))
	if len(golden) != len(queries) {
		t.Fatalf("golden entries = %d, queries = %d", len(golden), len(queries))
	}
}

func TestSignalDegradation(t *testing.T) {
	entries, queries, golden := loadParityFixtures(t)
	goldenByQuery := parityResultByQuery(t, golden)
	queryVectors := parityQueryVectors(queries)
	var degradationQueries []parityQuery
	for _, query := range queries {
		if query.Degradation {
			degradationQueries = append(degradationQueries, query)
		}
	}
	if len(degradationQueries) == 0 {
		t.Fatal("fixture must include degradation queries")
	}

	for _, query := range degradationQueries {
		want := goldenByQuery[query.ID]

		got := searchParityQuery(t, entries, query, &parityClient{model: parityModel, vectors: queryVectors, fail: true})
		if !reflect.DeepEqual(got, want.NoSemantic) {
			t.Errorf("query %q semantic failure: got %v, want %v", query.ID, got, want.NoSemantic)
		}
		if len(got) == 0 {
			t.Errorf("query %q semantic failure returned no remaining-signal result", query.ID)
		}

		s, es, vs := newParityStores(t, entries)
		if _, err := s.DB().ExecContext(context.Background(), "DROP TABLE memory_entries_fts"); err != nil {
			t.Fatalf("drop keyword index: %v", err)
		}
		keywordClient := &parityClient{model: parityModel, vectors: queryVectors}
		gotResults, err := memory.NewRetriever(es, vs, keywordClient).Search(context.Background(), query.Query, query.K)
		if err != nil {
			t.Fatalf("query %q keyword failure: %v", query.ID, err)
		}
		got = resultEntryIDs(t, gotResults, entries)
		if !reflect.DeepEqual(got, want.NoKeyword) {
			t.Errorf("query %q keyword failure: got %v, want %v", query.ID, got, want.NoKeyword)
		}
		if len(got) == 0 {
			t.Errorf("query %q keyword failure returned no remaining-signal result", query.ID)
		}

		s, es, vs = newParityStores(t, entries)
		if _, err := s.DB().ExecContext(context.Background(), "DELETE FROM memory_entities"); err != nil {
			t.Fatalf("clear entity index: %v", err)
		}
		gotResults, err = memory.NewRetriever(es, vs, &parityClient{model: parityModel, vectors: queryVectors}).Search(context.Background(), query.Query, query.K)
		if err != nil {
			t.Fatalf("query %q entity failure: %v", query.ID, err)
		}
		got = resultEntryIDs(t, gotResults, entries)
		if !reflect.DeepEqual(got, want.NoEntity) {
			t.Errorf("query %q entity failure: got %v, want %v", query.ID, got, want.NoEntity)
		}
		if len(got) == 0 {
			t.Errorf("query %q entity failure returned no remaining-signal result", query.ID)
		}
	}
	t.Logf("signal degradation: %d queries covered semantic, keyword, and entity failures", len(degradationQueries))
}
