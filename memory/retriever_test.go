package memory_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
)

// seedRetrievalCorpus inserts a small labeled corpus and returns the stores.
func seedRetrievalCorpus(t *testing.T) (*memory.EntryStore, *memory.VectorStore) {
	t.Helper()
	ctx := context.Background()
	es, vs := newStores(t)
	corpus := []struct {
		name, trigger, content string
		entities               []string
	}{
		{"sweden-move", "where the user is from", "The user moved from Sweden four years ago.", []string{"Sweden"}},
		{"python-pref", "favorite language", "The user prefers Python for scripting tasks.", []string{"Python"}},
		{"coffee-habit", "morning routine", "The user drinks black coffee every morning.", []string{"coffee"}},
	}
	for _, c := range corpus {
		if err := es.Upsert(ctx, &memory.Entry{Name: c.name, Trigger: c.trigger, Content: c.content, CharCount: len([]rune(c.content))}); err != nil {
			t.Fatalf("upsert %s: %v", c.name, err)
		}
		if err := es.PutEntities(ctx, c.name, c.entities); err != nil {
			t.Fatalf("entities %s: %v", c.name, err)
		}
	}
	return es, vs
}

func TestRetriever_KeywordOnlyDegradation(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	// nil client → no semantic signal.
	r := memory.NewRetriever(es, vs, nil)
	got, err := r.Search(ctx, "Python scripting", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 || got[0].Name != "python-pref" {
		t.Fatalf("expected python-pref top, got %+v", got)
	}
}

func TestRetriever_EntitySignalContributes(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	r := memory.NewRetriever(es, vs, nil)
	// "Sweden" appears as an entity and in content; entity signal reinforces it.
	got, err := r.Search(ctx, "Sweden", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 || got[0].Name != "sweden-move" {
		t.Fatalf("expected sweden-move top, got %+v", got)
	}
}

func TestRetriever_SemanticOnlyMatch(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	// Fake client that returns a vector aligning the query with coffee-habit only.
	fc := &vectorFakeClient{
		model: "m1",
		vectors: map[string][]float32{
			"The user moved from Sweden four years ago.":   {1, 0, 0},
			"The user prefers Python for scripting tasks.": {0, 1, 0},
			"The user drinks black coffee every morning.":  {0, 0, 1},
			"caffeine intake": {0, 0, 1}, // query aligns with coffee
		},
	}
	// Embed & store vectors for the corpus.
	emb := memory.NewEmbedder(es, vs, fc, 8)
	_ = emb.Backfill(ctx)
	emb.Close()

	r := memory.NewRetriever(es, vs, fc)
	// Query shares NO keywords/entities with coffee-habit, only semantic vector.
	got, err := r.Search(ctx, "caffeine intake", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected a semantic hit, got none")
	}
	found := false
	for _, g := range got {
		if g.Name == "coffee-habit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected coffee-habit via semantic signal, got %+v", got)
	}
}

func TestRetriever_EmptyStore(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	r := memory.NewRetriever(es, vs, nil)
	got, err := r.Search(ctx, "anything", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestRetriever_TimeAwareFields(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "dated", Content: "lived in Berlin", CharCount: 15}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	r := memory.NewRetriever(es, vs, nil)
	got, err := r.Search(ctx, "Berlin", 5)
	if err != nil || len(got) == 0 {
		t.Fatalf("search: got %v err %v", got, err)
	}
	if got[0].CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt populated on result")
	}
}

func TestTemporalScoreUsesSoftExponentialGap(t *testing.T) {
	windowStart := time.Date(2024, time.May, 10, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart
	window := memory.TimeWindow{Start: windowStart, End: windowEnd}
	tau := 30 * 24 * time.Hour

	overlap := windowStart
	if got := memory.TemporalScore(&overlap, &overlap, window, tau); got != 1 {
		t.Fatalf("overlap score = %.12f, want 1", got)
	}

	outside := time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)
	got := memory.TemporalScore(&outside, &outside, window, tau)
	want := math.Exp(-9.0 / 30.0)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("outside score = %.12f, want exp(-9/30)=%.12f", got, want)
	}
	if got <= 0 || got >= 1 {
		t.Fatalf("outside score = %.12f, want a positive soft score below 1", got)
	}

	if got := memory.TemporalScore(nil, nil, window, tau); got != 1 {
		t.Fatalf("unknown event score = %.12f, want neutral 1", got)
	}
}

func TestTemporalSearchSoftRanksWithoutDeletingCandidates(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	oldDate := time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC)
	recentDate := time.Date(2024, time.May, 20, 0, 0, 0, 0, time.UTC)
	for _, entry := range []*memory.Entry{
		{Name: "old-project", Content: "project May 2024", EventStart: &oldDate, EventEnd: &oldDate},
		{Name: "recent-project", Content: "project May 2024", EventStart: &recentDate, EventEnd: &recentDate},
	} {
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}
	r := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{
		TemporalScore: true,
		Now:           time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC),
	})
	got, err := r.Search(ctx, "project May 2024", 2)
	if err != nil {
		t.Fatalf("temporal search: %v", err)
	}
	if len(got) != 2 || got[0].Name != "recent-project" {
		t.Fatalf("soft temporal order = %+v, want recent-project first and old-project retained", got)
	}
	if got[1].Name != "old-project" {
		t.Fatalf("soft temporal search deleted or reordered candidate unexpectedly: %+v", got)
	}
}

func TestTemporalSearchUnionsAliasesAndOrderRecall(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	aliasDate := time.Date(2024, time.May, 8, 0, 0, 0, 0, time.UTC)
	if err := es.Upsert(ctx, &memory.Entry{Name: "tracker-event", Content: "The user bought equipment.", EventStart: &aliasDate, EventEnd: &aliasDate}); err != nil {
		t.Fatalf("upsert alias entry: %v", err)
	}
	if err := es.PutAliases(ctx, "tracker-event", []string{"fitness tracker"}); err != nil {
		t.Fatalf("put alias: %v", err)
	}
	beforeDate := time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)
	if err := es.Upsert(ctx, &memory.Entry{Name: "before-event", Content: "The user attended a gathering.", EventStart: &beforeDate, EventEnd: &beforeDate}); err != nil {
		t.Fatalf("upsert before entry: %v", err)
	}
	options := memory.RetrieverOptions{
		TemporalScore: true,
		Now:           time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC),
	}
	r := memory.NewRetrieverWithOptions(es, vs, nil, nil, options)
	aliasHits, err := r.Search(ctx, "tracker May 2024", 5)
	if err != nil {
		t.Fatalf("alias search: %v", err)
	}
	if !containsResult(aliasHits, "tracker-event") {
		t.Fatalf("alias result missing: %+v", aliasHits)
	}
	orderHits, err := r.Search(ctx, "What happened before the conference on 2024-05-10?", 5)
	if err != nil {
		t.Fatalf("order search: %v", err)
	}
	if !containsResult(orderHits, "before-event") {
		t.Fatalf("before supplemental result missing: %+v", orderHits)
	}

	sameDate := time.Date(2024, time.May, 10, 0, 0, 0, 0, time.UTC)
	if err := es.Upsert(ctx, &memory.Entry{Name: "same-timestamp", Content: "The user attended a gathering.", EventStart: &sameDate, EventEnd: &sameDate}); err != nil {
		t.Fatalf("upsert same timestamp: %v", err)
	}
	orderHits, err = r.Search(ctx, "What happened before the conference on 2024-05-10?", 5)
	if err != nil {
		t.Fatalf("same timestamp search: %v", err)
	}
	if containsResult(orderHits, "same-timestamp") {
		t.Fatalf("same timestamp was incorrectly inferred as before: %+v", orderHits)
	}
}

func TestTemporalOrderResolvesAnchorEntityWithoutDate(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	anchorDate := time.Date(2024, time.May, 10, 0, 0, 0, 0, time.UTC)
	priorDate := time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)
	if err := es.Upsert(ctx, &memory.Entry{Name: "pottery-class", Content: "The user attended a pottery class.", EventStart: &anchorDate, EventEnd: &anchorDate}); err != nil {
		t.Fatalf("upsert anchor: %v", err)
	}
	if err := es.PutAliases(ctx, "pottery-class", []string{"pottery class"}); err != nil {
		t.Fatalf("put anchor alias: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "prior-event", Content: "The user visited a museum.", EventStart: &priorDate, EventEnd: &priorDate}); err != nil {
		t.Fatalf("upsert prior event: %v", err)
	}
	r := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{
		TemporalScore: true,
		Now:           time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC),
	})
	got, err := r.Search(ctx, "What happened before the pottery class?", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !containsResult(got, "prior-event") {
		t.Fatalf("anchor-entity directional result missing: %+v", got)
	}
}

func containsResult(results []memory.Result, name string) bool {
	for _, result := range results {
		if result.Name == name {
			return true
		}
	}
	return false
}

func TestRetriever_RerankReorders(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	// BM25 alone would rank python-pref first for this query; the fake
	// reranker forces coffee-habit to the top.
	rr := &fakeReranker{scores: map[string]float64{
		"The user drinks black coffee every morning.":  0.9,
		"The user prefers Python for scripting tasks.": 0.2,
	}}
	r := memory.NewRetriever(es, vs, nil).WithReranker(rr)
	got, err := r.Search(ctx, "Python coffee", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 || got[0].Name != "coffee-habit" {
		t.Fatalf("expected reranker to put coffee-habit top, got %+v", got)
	}
}

func TestRetriever_RerankErrorDegradesToFused(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	r := memory.NewRetriever(es, vs, nil).WithReranker(&fakeReranker{fail: true})
	got, err := r.Search(ctx, "Python scripting", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 || got[0].Name != "python-pref" {
		t.Fatalf("expected fused order to survive reranker failure, got %+v", got)
	}
}

func TestRetriever_EntityExpansionSurfacesNeighbor(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	// A neighbor entry that shares the "Sweden" entity but matches the query on
	// neither keywords nor entities: only 1-hop expansion can surface it.
	if err := es.Upsert(ctx, &memory.Entry{Name: "midsummer-party", Trigger: "holiday plans",
		Content: "The user hosts a midsummer party each June.", CharCount: 43}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "midsummer-party", []string{"Sweden"}); err != nil {
		t.Fatalf("entities: %v", err)
	}
	rr := &fakeReranker{scores: map[string]float64{
		"The user hosts a midsummer party each June.": 0.9,
		"The user moved from Sweden four years ago.":  0.5,
	}}
	r := memory.NewRetriever(es, vs, nil).WithReranker(rr)
	got, err := r.Search(ctx, "Sweden", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) == 0 || got[0].Name != "midsummer-party" {
		t.Fatalf("expected entity-expanded neighbor on top, got %+v", got)
	}
}

func TestAssociativeNoRegression(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "midsummer-party", Trigger: "holiday plans",
		Content: "The user hosts a midsummer party each June.", CharCount: 43,
	}); err != nil {
		t.Fatalf("upsert neighbor: %v", err)
	}
	if err := es.PutEntities(ctx, "midsummer-party", []string{"midsummer"}); err != nil {
		t.Fatalf("neighbor entities: %v", err)
	}
	if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "Sweden", B: "midsummer", Kind: "co", Weight: 1}}); err != nil {
		t.Fatalf("upsert edge: %v", err)
	}
	client := &vectorFakeClient{model: "assoc", vectors: map[string][]float32{
		"Sweden": {1, 0, 0},
	}}
	if err := vs.Put(ctx, "sweden-move", client.model, []float32{1, 0, 0}, time.Now()); err != nil {
		t.Fatalf("sweden vector: %v", err)
	}
	if err := vs.Put(ctx, "midsummer-party", client.model, []float32{0, 0.1, 0}, time.Now()); err != nil {
		t.Fatalf("neighbor vector: %v", err)
	}

	baseline, err := memory.NewRetriever(es, vs, client).Search(ctx, "Sweden", 2)
	if err != nil || len(baseline) == 0 {
		t.Fatalf("baseline search: got %v err %v", baseline, err)
	}
	assoc, err := memory.NewRetrieverWithOptions(es, vs, client, nil, memory.RetrieverOptions{
		Associative: true,
	}).Search(ctx, "Sweden", 2)
	if err != nil || len(assoc) == 0 {
		t.Fatalf("associative search: got %v err %v", assoc, err)
	}
	if assoc[0].Name != baseline[0].Name {
		t.Fatalf("associative changed top-1: baseline=%+v assoc=%+v", baseline, assoc)
	}
}

func TestAssociativeReusesQueryEmbedding(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "midsummer-party", Trigger: "holiday plans",
		Content: "The user hosts a midsummer party each June.", CharCount: 43,
	}); err != nil {
		t.Fatalf("upsert neighbor: %v", err)
	}
	if err := es.PutEntities(ctx, "midsummer-party", []string{"midsummer"}); err != nil {
		t.Fatalf("neighbor entities: %v", err)
	}
	if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "Sweden", B: "midsummer", Kind: "co", Weight: 1}}); err != nil {
		t.Fatalf("upsert edge: %v", err)
	}
	client := &countingVectorClient{vectorFakeClient: vectorFakeClient{
		model:   "assoc",
		vectors: map[string][]float32{"Sweden": {1, 0, 0}},
	}}
	if err := vs.Put(ctx, "sweden-move", client.model, []float32{1, 0, 0}, time.Now()); err != nil {
		t.Fatalf("sweden vector: %v", err)
	}
	if err := vs.Put(ctx, "midsummer-party", client.model, []float32{0, 1, 0}, time.Now()); err != nil {
		t.Fatalf("neighbor vector: %v", err)
	}

	if _, err := memory.NewRetrieverWithOptions(es, vs, client, nil, memory.RetrieverOptions{
		Associative: true,
	}).Search(ctx, "Sweden", 2); err != nil {
		t.Fatalf("associative search: %v", err)
	}
	if client.embedCalls != 1 {
		t.Fatalf("query embedding calls = %d, want 1", client.embedCalls)
	}
}

func TestAssociativeSearchSurfacesGraphOnlyEntry(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "seed-entry", Trigger: "Seed", Content: "anchor fact"}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if err := es.PutEntities(ctx, "seed-entry", []string{"Seed"}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "a-graph-only", Content: "unrelated fact"}); err != nil {
		t.Fatalf("graph entry: %v", err)
	}
	if err := es.PutEntities(ctx, "a-graph-only", []string{"Target"}); err != nil {
		t.Fatalf("graph entity: %v", err)
	}
	if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "Seed", B: "Target", Kind: "co", Weight: 1}}); err != nil {
		t.Fatalf("graph edge: %v", err)
	}
	client := &vectorFakeClient{model: "graph-only", vectors: map[string][]float32{"Seed": {1, 0, 0}}}
	if err := vs.Put(ctx, "seed-entry", client.model, []float32{1, 0, 0}, time.Now()); err != nil {
		t.Fatalf("seed vector: %v", err)
	}
	if err := vs.Put(ctx, "a-graph-only", client.model, []float32{0, 1, 0}, time.Now()); err != nil {
		t.Fatalf("graph vector: %v", err)
	}
	for i := 0; i < 105; i++ {
		name := fmt.Sprintf("distractor-%03d", i)
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: "distractor"}); err != nil {
			t.Fatalf("distractor entry: %v", err)
		}
		if err := vs.Put(ctx, name, client.model, []float32{1, 0, 0}, time.Now()); err != nil {
			t.Fatalf("distractor vector: %v", err)
		}
	}

	got, err := memory.NewRetrieverWithOptions(es, vs, client, nil, memory.RetrieverOptions{
		Associative: true,
	}).Search(ctx, "Seed", 5)
	if err != nil {
		t.Fatalf("associative search: %v", err)
	}
	for _, result := range got {
		if result.Name == "a-graph-only" {
			return
		}
	}
	t.Fatalf("graph-only entry missing from results: %+v", got)
}

func TestEntityQueryWholeSentenceMatchesRawEntity(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "alice-profile", Trigger: "colleague",
		Content: "The user has a trusted colleague.", CharCount: 34,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "alice-profile", []string{"Alice Smith"}); err != nil {
		t.Fatalf("entities: %v", err)
	}
	got, err := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{
		Associative: true,
	}).Search(ctx, "What did Alice Smith do?", 3)
	if err != nil || len(got) == 0 || got[0].Name != "alice-profile" {
		t.Fatalf("whole-sentence entity match: got %v err %v", got, err)
	}
}

func TestEntityQueryRawMatchUsesTokenBoundaries(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "sam-profile", Trigger: "contact",
		Content: "The user has a contact.", CharCount: 24,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "sam-profile", []string{"Sam"}); err != nil {
		t.Fatalf("entities: %v", err)
	}
	got, err := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{
		Associative: true,
	}).Search(ctx, "Did they watch the same movie?", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, result := range got {
		if result.Name == "sam-profile" {
			t.Fatalf("entity Sam matched same: %+v", got)
		}
	}
}

func TestClusterSweepEnumerationsReplaceTopKWithEntityCluster(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "seed", Content: "root fact"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := es.PutEntities(ctx, "seed", []string{"root"}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("cluster-%d", i)
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: fmt.Sprintf("unrelated fact %d", i)}); err != nil {
			t.Fatalf("cluster entry: %v", err)
		}
		entity := fmt.Sprintf("member-%d", i)
		if err := es.PutEntities(ctx, name, []string{entity}); err != nil {
			t.Fatalf("cluster entity: %v", err)
		}
		if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "root", B: entity, Kind: "co"}}); err != nil {
			t.Fatalf("cluster edge: %v", err)
		}
	}
	r := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{ClusterSweep: true})
	got, err := r.Search(ctx, "What things did root do?", 3)
	if err != nil {
		t.Fatalf("sweep search: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("sweep search = %+v, want all four cluster candidates", got)
	}
	if got[0].Name != "seed" {
		t.Fatalf("sweep seed order = %+v, want seed first", got)
	}
	if got[1].Name != "cluster-0" || got[2].Name != "cluster-1" || got[3].Name != "cluster-2" {
		t.Fatalf("sweep candidates = %+v, want cluster entries after seed", got)
	}
}

func TestClusterSweepKeepsCandidatesPastRequestedK(t *testing.T) {
	ctx := context.Background()
	es, vs := seedClusterSweepCorpus(t, 31)
	r := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{ClusterSweep: true})

	got, err := r.Search(ctx, "What things did root do?", 30)
	if err != nil {
		t.Fatalf("sweep search: %v", err)
	}
	if len(got) != 31 {
		t.Fatalf("sweep search returned %d candidates, want all 31 cluster candidates", len(got))
	}
}

func TestClusterSweepRerankerKeepsCandidatesPastRequestedK(t *testing.T) {
	ctx := context.Background()
	es, vs := seedClusterSweepCorpus(t, 31)
	r := memory.NewRetrieverWithOptions(es, vs, nil, &fakeReranker{}, memory.RetrieverOptions{ClusterSweep: true})

	got, err := r.Search(ctx, "What things did root do?", 30)
	if err != nil {
		t.Fatalf("reranked sweep search: %v", err)
	}
	if len(got) != 31 {
		t.Fatalf("reranked sweep search returned %d candidates, want all 31 cluster candidates", len(got))
	}
}

func TestClusterSweepRetainsTopFusedFallbackOutsideEntityCluster(t *testing.T) {
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "seed", Content: "root fact"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := es.PutEntities(ctx, "seed", []string{"root"}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "cluster", Content: "cluster fact"}); err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if err := es.PutEntities(ctx, "cluster", []string{"member"}); err != nil {
		t.Fatalf("cluster entity: %v", err)
	}
	if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "root", B: "member", Kind: "co"}}); err != nil {
		t.Fatalf("cluster edge: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "fallback", Content: "list all root values root values"}); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if err := es.PutEntities(ctx, "fallback", []string{"outside"}); err != nil {
		t.Fatalf("fallback entity: %v", err)
	}

	client := &vectorFakeClient{model: "cluster-fallback", vectors: map[string][]float32{
		"list all root values root values": {1, 0},
		"List all root values":             {1, 0},
		"root fact":                        {0, 1},
		"cluster fact":                     {0, 1},
	}}
	for name, vector := range map[string][]float32{
		"fallback": {1, 0},
		"seed":     {0, 1},
		"cluster":  {0, 1},
	} {
		if err := vs.Put(ctx, name, client.Model(), vector, time.Now()); err != nil {
			t.Fatalf("vector %s: %v", name, err)
		}
	}

	baseline, err := memory.NewRetriever(es, vs, client).Search(ctx, "List all root values", 3)
	if err != nil || len(baseline) == 0 {
		t.Fatalf("baseline search = %+v, %v", baseline, err)
	}
	if baseline[0].Name != "fallback" {
		t.Fatalf("baseline results = %+v, want fallback first", baseline)
	}
	got, err := memory.NewRetrieverWithOptions(es, vs, client, nil, memory.RetrieverOptions{ClusterSweep: true}).Search(ctx, "List all root values", 3)
	if err != nil {
		t.Fatalf("sweep search: %v", err)
	}
	if !containsResult(got, "fallback") {
		t.Fatalf("sweep results = %+v, want top fused fallback outside entity cluster", got)
	}
}

func seedClusterSweepCorpus(t *testing.T, count int) (*memory.EntryStore, *memory.VectorStore) {
	t.Helper()
	ctx := context.Background()
	es, vs := newStores(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "seed", Content: "root fact"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := es.PutEntities(ctx, "seed", []string{"root"}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	for i := 1; i < count; i++ {
		name := fmt.Sprintf("cluster-%02d", i)
		entity := fmt.Sprintf("member-%02d", i)
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: "cluster fact"}); err != nil {
			t.Fatalf("cluster entry %d: %v", i, err)
		}
		if err := es.PutEntities(ctx, name, []string{entity}); err != nil {
			t.Fatalf("cluster entity %d: %v", i, err)
		}
		if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "root", B: entity, Kind: "co"}}); err != nil {
			t.Fatalf("cluster edge %d: %v", i, err)
		}
	}
	return es, vs
}

func TestClusterSweepDisabledOrNonEnumerationPreservesResults(t *testing.T) {
	ctx := context.Background()
	es, vs := seedRetrievalCorpus(t)
	baseline := memory.NewRetriever(es, vs, nil)
	withSweep := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{ClusterSweep: true})
	for _, query := range []string{"Sweden", "What is the user's favorite language?"} {
		want, err := baseline.Search(ctx, query, 5)
		if err != nil {
			t.Fatalf("baseline %q: %v", query, err)
		}
		got, err := withSweep.Search(ctx, query, 5)
		if err != nil {
			t.Fatalf("sweep %q: %v", query, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("non-enumeration %q changed results:\n got=%+v\nwant=%+v", query, got, want)
		}
	}
}

// fakeReranker scores documents from a fixed map (unknown docs score 0);
// fail=true simulates an endpoint failure.
type fakeReranker struct {
	scores map[string]float64
	fail   bool
}

func (f *fakeReranker) Model() string { return "fake-reranker" }

func (f *fakeReranker) Rerank(_ context.Context, _ string, docs []string, topN int) ([]embedding.RankedDoc, error) {
	if f.fail {
		return nil, context.DeadlineExceeded
	}
	out := make([]embedding.RankedDoc, len(docs))
	for i, d := range docs {
		out[i] = embedding.RankedDoc{Index: i, Score: f.scores[d]}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out, nil
}

// vectorFakeClient returns a stored vector by exact input text; unknown inputs
// map to a zero vector (cosine 0).
type vectorFakeClient struct {
	model   string
	vectors map[string][]float32
}

type countingVectorClient struct {
	vectorFakeClient
	embedCalls int
}

func (f *countingVectorClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.embedCalls++
	return f.vectorFakeClient.Embed(ctx, texts)
}

func (f *vectorFakeClient) Model() string { return f.model }

func (f *vectorFakeClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := f.vectors[strings.TrimSpace(t)]; ok {
			out[i] = v
			continue
		}
		// embedText joins trigger+content; match on the content suffix.
		matched := []float32{0, 0, 0}
		for key, v := range f.vectors {
			if strings.Contains(t, key) {
				matched = v
				break
			}
		}
		out[i] = matched
	}
	return out, nil
}
