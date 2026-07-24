package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
)

type doc2queryTestEmbedder struct{}

func (doc2queryTestEmbedder) Model() string { return "doc2query-test" }

func (doc2queryTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i + 1), 1, 0}
	}
	return vectors, nil
}

type doc2queryTestProvider struct {
	request provider.Request
}

type doc2queryBlockingEmbedder struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *doc2queryBlockingEmbedder) Model() string { return "doc2query-bulk-test" }

func (e *doc2queryBlockingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, float32(i + 1), 0}
	}
	return vectors, nil
}

type doc2queryFailingEmbedder struct{}

func (doc2queryFailingEmbedder) Model() string { return "different-current-model" }

func (doc2queryFailingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedding unavailable")
}

func (p *doc2queryTestProvider) Name() string { return "doc2query-test" }

func (p *doc2queryTestProvider) Stream(_ context.Context, request provider.Request) (<-chan provider.ProviderEvent, error) {
	p.request = request
	events := make(chan provider.ProviderEvent, 1)
	events <- provider.ProviderEvent{Type: provider.EventTextDelta, TextDelta: `{"queries":["one","two"]}`}
	close(events)
	return events, nil
}

func TestDoc2Query_BuildCreatesQueryShadowsWithoutPollutingCanonical(t *testing.T) {
	ctx := context.Background()
	canonicalDir := t.TempDir()
	canonicalPath := filepath.Join(canonicalDir, "conv0.db")
	seedDoc2QueryCanonicalStore(t, ctx, canonicalPath)
	if got := doc2queryVectorCount(t, ctx, canonicalPath); got != 0 {
		t.Fatalf("canonical #query count before build = %d, want 0", got)
	}

	var generationCalls atomic.Int32
	genCall := func(_ context.Context, system, user string) (string, error) {
		generationCalls.Add(1)
		wantSystem := `You generate the questions a memory fact directly answers, for a
retrieval index. Given ONE self-contained fact, output 2-3 SHORT, natural
questions a user might ask that this fact answers. Each question must be
answerable by the fact alone. Vary phrasing (who/when/what/where). Return
STRICT JSON: {"queries":["...","..."]}. No prose.`
		if system != wantSystem {
			t.Errorf("system prompt = %q, want %q", system, wantSystem)
		}
		if user != "FACT: Maya found a restorative hobby.\nReturn the JSON now." {
			t.Errorf("user prompt = %q", user)
		}
		return `{"queries":["What hobby did Maya find?","What did Maya find restorative?","What restored Maya?","ignored fourth query"]}`, nil
	}
	buildDir := t.TempDir()
	opt := options{
		storeDir:    canonicalDir,
		runDir:      buildDir,
		concurrency: 2,
	}
	if err := runDoc2QueryBuild(ctx, opt, []conversation{{ID: 0}}, genCall, doc2queryTestEmbedder{}, doc2queryDiscardLogger()); err != nil {
		t.Fatalf("run doc2query build: %v", err)
	}
	if got := generationCalls.Load(); got != 1 {
		t.Fatalf("generation calls = %d, want 1 extraction fact only", got)
	}

	prebuiltDir := filepath.Join(buildDir, doc2queryStoreDirName)
	prebuiltPath := filepath.Join(prebuiltDir, "conv0.db")
	if got := doc2queryVectorCount(t, ctx, prebuiltPath); got == 0 {
		t.Fatal("prebuilt #query count = 0, want >0 from Embedder.Backfill")
	}
	if got := doc2queryVectorCount(t, ctx, canonicalPath); got != 0 {
		t.Fatalf("canonical #query count after build = %d, want 0", got)
	}
	queries := doc2queryFactQueries(t, ctx, prebuiltPath, "maya-hobby")
	wantQueries := []string{"What did Maya find restorative?", "What hobby did Maya find?", "What restored Maya?"}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("stored queries = %v, want %v", queries, wantQueries)
	}
	artifact := readJSONObject(t, filepath.Join(buildDir, doc2queryBackfillFile))
	if artifact["name"] != "maya-hobby" {
		t.Fatalf("backfill artifact name = %v, want maya-hobby", artifact["name"])
	}
	if queries, ok := artifact["queries"].([]any); !ok || len(queries) != 3 {
		t.Fatalf("backfill artifact queries = %v, want 3", artifact["queries"])
	}
	for _, forbidden := range []string{"api_key", "base_url", "provider"} {
		if _, found := artifact[forbidden]; found {
			t.Fatalf("backfill artifact contains credential/config field %q", forbidden)
		}
	}

	prebuiltCount := doc2queryVectorCount(t, ctx, prebuiltPath)
	for _, mode := range []string{doc2queryBaseline, doc2queryTreatment} {
		t.Run(mode, func(t *testing.T) {
			var extractionCalls atomic.Int32
			armOpt := options{
				doc2query:   mode,
				storeDir:    prebuiltDir,
				runDir:      t.TempDir(),
				retrieval:   "hybrid",
				topK:        doc2queryTopK,
				concurrency: 1,
			}
			if err := prepareDoc2QueryStore(&armOpt); err != nil {
				t.Fatalf("prepare %s store: %v", mode, err)
			}
			if !armOpt.noIDKRetry {
				t.Fatalf("%s prepare left IDK retries enabled; final top-k could exceed 30", mode)
			}
			runtime, err := buildConversationRuntime(ctx, armOpt, conversation{ID: 0}, func(context.Context, string, string) (string, error) {
				extractionCalls.Add(1)
				return `{"facts":[]}`, nil
			}, doc2queryTestEmbedder{}, []string{"hybrid"}, doc2queryDiscardLogger())
			if err != nil {
				t.Fatalf("build %s runtime: %v", mode, err)
			}
			runtime.Close()
			if got := extractionCalls.Load(); got != 0 {
				t.Fatalf("%s extraction calls = %d, want 0", mode, got)
			}
			copyCount := doc2queryVectorCount(t, ctx, filepath.Join(armOpt.storeDir, "conv0.db"))
			if mode == doc2queryBaseline && copyCount != 0 {
				t.Fatalf("baseline #query count = %d, want 0", copyCount)
			}
			if mode == doc2queryTreatment && copyCount == 0 {
				t.Fatal("treatment #query count = 0, want >0")
			}
			if got := doc2queryVectorCount(t, ctx, prebuiltPath); got != prebuiltCount {
				t.Fatalf("prebuilt store #query count after %s = %d, want unchanged %d", mode, got, prebuiltCount)
			}
			if got := doc2queryVectorCount(t, ctx, canonicalPath); got != 0 {
				t.Fatalf("canonical #query count after %s = %d, want 0", mode, got)
			}
		})
	}
}

func TestDoc2Query_ParseRequiresTwoToThreeQueries(t *testing.T) {
	if got := parseDoc2QueryResponse(`{"queries":["only one"]}`); len(got) != 0 {
		t.Fatalf("one-query response parsed as %v, want skip", got)
	}
	got := parseDoc2QueryResponse(`{"queries":["one","two","three","four"]}`)
	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("four-query response parsed as %v, want %v", got, want)
	}
	if got := parseDoc2QueryResponse(`{"queries":["same"," same "]}`); len(got) != 0 {
		t.Fatalf("duplicate-only response parsed as %v, want skip", got)
	}
}

func TestDoc2Query_BuildEmbedsEveryQueryShadowBeyondDefaultBuffer(t *testing.T) {
	const factCount = memory.DefaultEmbedBuffer + 44
	ctx := context.Background()
	canonicalDir := t.TempDir()
	seedDoc2QueryBulkCanonicalStore(t, ctx, filepath.Join(canonicalDir, "conv0.db"), factCount)
	embClient := &doc2queryBlockingEmbedder{started: make(chan struct{}), release: make(chan struct{})}
	result := make(chan error, 1)
	buildDir := t.TempDir()
	go func() {
		result <- runDoc2QueryBuild(ctx, options{storeDir: canonicalDir, runDir: buildDir, concurrency: 32}, []conversation{{ID: 0}},
			func(context.Context, string, string) (string, error) {
				return `{"queries":["What happened?","When did it happen?"]}`, nil
			}, embClient, doc2queryDiscardLogger())
	}()
	select {
	case <-embClient.started:
	case <-time.After(10 * time.Second):
		t.Fatal("embedding backfill did not start")
	}
	close(embClient.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("build bulk store: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("embedding backfill did not finish")
	}
	path := filepath.Join(buildDir, doc2queryStoreDirName, "conv0.db")
	if got := doc2queryVectorCountForModel(t, ctx, path, embClient.Model()); got != factCount {
		t.Fatalf("current-model #query vectors = %d, want %d", got, factCount)
	}
}

func TestDoc2Query_ValidateRejectsConfoundedRuns(t *testing.T) {
	base := options{
		doc2query: doc2queryTreatment,
		topK:      doc2queryTopK,
		storeDir:  "/prebuilt",
		runDir:    "/run",
	}
	tests := []struct {
		name string
		opt  options
	}{
		{name: "top-k", opt: func() options { o := base; o.topK = 40; return o }()},
		{name: "multi-query", opt: func() options { o := base; o.multiQuery = true; return o }()},
		{name: "missing-store", opt: func() options { o := base; o.storeDir = ""; return o }()},
		{name: "missing-run-dir", opt: func() options { o := base; o.runDir = ""; return o }()},
		{name: "unknown-enum", opt: func() options { o := base; o.doc2query = "maybe"; return o }()},
		{name: "alias-conflict", opt: func() options { o := base; o.aliasShadow = aliasShadowTreatment; return o }()},
		{name: "build-conflict", opt: func() options { o := base; o.doc2queryBuild = true; return o }()},
		{name: "opinion-pass", opt: func() options { o := base; o.opinionPass = true; return o }()},
		{name: "conflict-resolution", opt: func() options { o := base; o.conflictResolution = true; return o }()},
		{name: "conflict-arm", opt: func() options { o := base; o.retrieval = "hybrid+conflict"; return o }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateDoc2QueryOptions(tt.opt); err == nil {
				t.Fatal("validation succeeded, want error")
			}
		})
	}
	if err := validateDoc2QueryOptions(base); err != nil {
		t.Fatalf("valid treatment rejected: %v", err)
	}
	base.doc2query = doc2queryBaseline
	if err := validateDoc2QueryOptions(base); err != nil {
		t.Fatalf("valid baseline rejected: %v", err)
	}
	for _, buildOpt := range []options{
		{doc2queryBuild: true, runDir: "/run"},
		{doc2queryBuild: true, storeDir: "/canonical"},
		{doc2queryBuild: true, storeDir: "/canonical", runDir: "/run", aliasShadow: aliasShadowTreatment},
	} {
		if err := validateDoc2QueryOptions(buildOpt); err == nil {
			t.Fatalf("invalid build options accepted: %+v", buildOpt)
		}
	}
	if err := validateDoc2QueryOptions(options{doc2queryBuild: true, storeDir: "/canonical", runDir: "/run"}); err != nil {
		t.Fatalf("valid build options rejected: %v", err)
	}
}

func TestDoc2Query_ModelCallerUsesFixedTemperature(t *testing.T) {
	prov := &doc2queryTestProvider{}
	call := newModelCaller(prov, "query-model", 321, doc2queryTemperature)
	if _, err := call(context.Background(), "system", "user"); err != nil {
		t.Fatalf("call model: %v", err)
	}
	if prov.request.Temperature != doc2queryTemperature {
		t.Fatalf("temperature = %v, want fixed %v", prov.request.Temperature, doc2queryTemperature)
	}
	if prov.request.Temperature <= 0 {
		t.Fatalf("temperature = %v, provider would treat it as unset", prov.request.Temperature)
	}
}

func TestDoc2Query_BuildSkipsUnparseableGeneration(t *testing.T) {
	ctx := context.Background()
	canonicalDir := t.TempDir()
	seedDoc2QueryCanonicalStore(t, ctx, filepath.Join(canonicalDir, "conv0.db"))
	buildDir := t.TempDir()
	if err := runDoc2QueryBuild(ctx, options{storeDir: canonicalDir, runDir: buildDir, concurrency: 1}, []conversation{{ID: 0}},
		func(context.Context, string, string) (string, error) { return "not json", nil },
		doc2queryTestEmbedder{}, doc2queryDiscardLogger()); err != nil {
		t.Fatalf("unparseable generation must be skipped: %v", err)
	}
	if got := doc2queryVectorCount(t, ctx, filepath.Join(buildDir, doc2queryStoreDirName, "conv0.db")); got != 0 {
		t.Fatalf("unparseable generation created %d #query vectors, want 0", got)
	}
}

func TestDoc2Query_RuntimeRefusesUnpreparedStore(t *testing.T) {
	ctx := context.Background()
	prebuiltDir := t.TempDir()
	path := filepath.Join(prebuiltDir, "conv0.db")
	seedDoc2QueryPrebuiltStore(t, ctx, path)
	runtime, err := buildConversationRuntime(ctx, options{
		doc2query: doc2queryTreatment,
		storeDir:  prebuiltDir,
		runDir:    t.TempDir(),
		retrieval: "hybrid",
		topK:      doc2queryTopK,
	}, conversation{ID: 0}, func(context.Context, string, string) (string, error) {
		return `{"facts":[]}`, nil
	}, doc2queryTestEmbedder{}, []string{"hybrid"}, doc2queryDiscardLogger())
	if runtime != nil {
		runtime.Close()
	}
	if err == nil {
		t.Fatal("unprepared doc2query runtime opened store, want error")
	}
	if got := doc2queryVectorCount(t, ctx, path); got == 0 {
		t.Fatal("rejected runtime mutated the prebuilt store")
	}
}

func TestDoc2Query_TreatmentRejectsStaleQueryShadowModel(t *testing.T) {
	ctx := context.Background()
	prebuiltDir := t.TempDir()
	seedDoc2QueryPrebuiltStore(t, ctx, filepath.Join(prebuiltDir, "conv0.db"))
	opt := options{
		doc2query: doc2queryTreatment,
		storeDir:  prebuiltDir,
		runDir:    t.TempDir(),
		retrieval: "hybrid",
		topK:      doc2queryTopK,
	}
	if err := prepareDoc2QueryStore(&opt); err != nil {
		t.Fatalf("prepare treatment store: %v", err)
	}
	runtime, err := buildConversationRuntime(ctx, opt, conversation{ID: 0}, func(context.Context, string, string) (string, error) {
		return "", errors.New("extraction must not run")
	}, doc2queryFailingEmbedder{}, []string{"hybrid"}, doc2queryDiscardLogger())
	if runtime != nil {
		runtime.Close()
	}
	if err == nil {
		t.Fatal("treatment accepted #query vectors from a stale embedding model")
	}
}

func TestDoc2Query_RecallDiagnosticUsesGoldHasQueryAndNeverExtracts(t *testing.T) {
	ctx := context.Background()
	prebuiltDir := t.TempDir()
	prebuiltPath := filepath.Join(prebuiltDir, "conv0.db")
	seedDoc2QueryPrebuiltStore(t, ctx, prebuiltPath)
	runDir := t.TempDir()
	conv := conversation{
		ID: 0,
		Sessions: []session{{
			Index: 1,
			Turns: []turn{{DiaID: "D1:1", Speaker: "Maya", Text: "Maya found a restorative hobby."}},
		}},
		QA: []locomoQA{{
			Question: "What helped Maya restore herself?",
			Category: 1,
			Evidence: []string{"D1:1"},
		}},
	}
	for _, mode := range []string{doc2queryBaseline, doc2queryTreatment} {
		opt := options{
			doc2query:        mode,
			storeDir:         prebuiltDir,
			runDir:           runDir,
			datasetFormat:    "locomo",
			retrieval:        "hybrid",
			recallDiagnostic: true,
			chunks:           true,
			topK:             doc2queryTopK,
			concurrency:      1,
			factCoverageTau:  defaultFactCoverageTau,
		}
		if err := prepareDoc2QueryStore(&opt); err != nil {
			t.Fatalf("prepare %s diagnostic store: %v", mode, err)
		}
		if err := runDoc2QueryRecallDiagnosticWithClient(ctx, opt, []conversation{conv}, []string{"hybrid"}, doc2queryTestEmbedder{}, doc2queryDiscardLogger()); err != nil {
			t.Fatalf("run %s diagnostic: %v", mode, err)
		}
		armRecord := readJSONObject(t, doc2queryRecallArmPath(runDir, mode))
		if got, ok := armRecord["gold_has_query"]; !ok || got != true {
			t.Errorf("%s gold_has_query = %v (present=%t), want true", mode, got, ok)
		}
		if _, leaked := armRecord["gold_has_alias"]; leaked {
			t.Errorf("%s record leaked gold_has_alias key: %v", mode, armRecord)
		}
	}

	report := readJSONObject(t, filepath.Join(runDir, "doc2query_recall.json"))
	layer, ok := report["gold_has_query"].(map[string]any)
	if !ok {
		t.Fatalf("gold_has_query layer missing from report: %v", report)
	}
	if got := layer["questions"]; got != float64(1) {
		t.Fatalf("gold_has_query questions = %v, want 1", got)
	}
	if _, leaked := report["gold_has_alias"]; leaked {
		t.Fatalf("report leaked gold_has_alias key: %v", report)
	}
}

func TestDoc2Query_RecallSummaryStratifiesGoldHasQuery(t *testing.T) {
	baseline := []doc2queryRecallQuestion{
		{Conv: 0, Q: 0, Category: 1, GoldHasQuery: true, GoldResolved: true, GoldRank: 40, GoldRankAt30: -1, Gradeable: true},
		{Conv: 0, Q: 1, Category: 1, GoldHasQuery: false, GoldResolved: true, GoldRank: 5, GoldRankAt30: 5, Gradeable: true, CoverageAt30: 1},
	}
	treatment := []doc2queryRecallQuestion{
		{Conv: 0, Q: 0, Category: 1, GoldHasQuery: true, GoldResolved: true, GoldRank: 10, GoldRankAt30: 10, Gradeable: true, CoverageAt30: 1},
		{Conv: 0, Q: 1, Category: 1, GoldHasQuery: false, GoldResolved: true, GoldRank: 6, GoldRankAt30: 6, Gradeable: true, CoverageAt30: 1},
	}
	report, err := summarizeDoc2QueryRecallDiagnostic(baseline, treatment, 1)
	if err != nil {
		t.Fatalf("summarize recall: %v", err)
	}
	if report.Global.Questions != 2 || report.GoldHasQuery.Questions != 1 {
		t.Fatalf("strata sizes = global:%d query:%d, want 2/1", report.Global.Questions, report.GoldHasQuery.Questions)
	}
	if report.GoldHasQuery.GoldEnteredTop30 != 1 || report.GoldHasQuery.GoldLeftTop30 != 0 {
		t.Fatalf("query stratum entered/left = %d/%d, want 1/0", report.GoldHasQuery.GoldEnteredTop30, report.GoldHasQuery.GoldLeftTop30)
	}
	if report.GoldHasQuery.MeanGoldRankDelta != -30 || report.GoldHasQuery.MeanCoverageAt30Delta != 1 {
		t.Fatalf("query deltas = rank:%v coverage:%v, want -30/+1", report.GoldHasQuery.MeanGoldRankDelta, report.GoldHasQuery.MeanCoverageAt30Delta)
	}
}

func TestDoc2Query_ContextParityRequiresFinalTop30(t *testing.T) {
	opt := options{doc2query: doc2queryTreatment}
	valid := contextParityRecord{Arm: doc2queryTreatment, FinalTopK: doc2queryTopK, SubqueryCount: 1}
	if err := validateDoc2QueryContextParity(opt, valid); err != nil {
		t.Fatalf("valid parity rejected: %v", err)
	}
	invalid := valid
	invalid.FinalTopK = 29
	if err := validateDoc2QueryContextParity(opt, invalid); err == nil {
		t.Fatal("final_top_k != 30 accepted")
	}
	if got := contextParityArm(opt); got != doc2queryTreatment {
		t.Fatalf("parity arm = %q, want %q", got, doc2queryTreatment)
	}
}

func TestDoc2Query_RecallDiagnosticNeedsNoMultiQueryCaller(t *testing.T) {
	opt := options{
		doc2query:       doc2queryBaseline,
		runDir:          "/run",
		storeDir:        "/prebuilt",
		datasetFormat:   "locomo",
		chunks:          true,
		topK:            doc2queryTopK,
		mqMaxSubqueries: 0,
	}
	if err := validateRecallDiagnosticOptions(opt, []string{"hybrid"}); err != nil {
		t.Fatalf("doc2query diagnostic unexpectedly requires decomposition: %v", err)
	}
}

func seedDoc2QueryCanonicalStore(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open canonical store: %v", err)
	}
	entries := memory.NewEntryStore(st.DB())
	for _, entry := range []*memory.Entry{
		{Name: "maya-hobby", Content: "Maya found a restorative hobby.", SourceSessionID: "conv0-sess1", FactSource: "extraction"},
		{Name: "verbatim", Content: "raw transcript", SourceSessionID: "conv0-sess1", FactSource: "verbatim_chunk"},
	} {
		entry.CharCount = len(entry.Content)
		if err := entries.Upsert(ctx, entry); err != nil {
			_ = st.Close()
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close canonical store: %v", err)
	}
}

func seedDoc2QueryBulkCanonicalStore(t *testing.T, ctx context.Context, path string, count int) {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open bulk canonical store: %v", err)
	}
	entries := memory.NewEntryStore(st.DB())
	for i := range count {
		entry := &memory.Entry{
			Name:            fmt.Sprintf("fact-%03d", i),
			Content:         fmt.Sprintf("Fact number %d happened.", i),
			SourceSessionID: "conv0-sess1",
			FactSource:      "extraction",
		}
		entry.CharCount = len(entry.Content)
		if err := entries.Upsert(ctx, entry); err != nil {
			_ = st.Close()
			t.Fatalf("upsert bulk fact %d: %v", i, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close bulk canonical store: %v", err)
	}
}

func seedDoc2QueryPrebuiltStore(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	seedDoc2QueryCanonicalStore(t, ctx, path)
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open prebuilt store: %v", err)
	}
	entries := memory.NewEntryStore(st.DB())
	if err := entries.PutFactQueries(ctx, "maya-hobby", []string{"What hobby restored Maya?", "What did Maya find?"}); err != nil {
		_ = st.Close()
		t.Fatalf("put fact queries: %v", err)
	}
	embedder := memory.NewEmbedder(entries, memory.NewVectorStore(st.DB()), doc2queryTestEmbedder{}, memory.DefaultEmbedBuffer)
	if err := embedder.Backfill(ctx); err != nil {
		embedder.Close()
		_ = st.Close()
		t.Fatalf("backfill prebuilt store: %v", err)
	}
	embedder.Close()
	if err := st.Close(); err != nil {
		t.Fatalf("close prebuilt store: %v", err)
	}
}

func doc2queryVectorCount(t *testing.T, ctx context.Context, path string) int {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open count store %s: %v", path, err)
	}
	defer st.Close() //nolint:errcheck
	var count int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name LIKE '%#query'`).Scan(&count); err != nil {
		t.Fatalf("count #query vectors: %v", err)
	}
	return count
}

func doc2queryVectorCountForModel(t *testing.T, ctx context.Context, path, model string) int {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open count store %s: %v", path, err)
	}
	defer st.Close() //nolint:errcheck
	var count int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name LIKE '%#query' AND model = ?`, model).Scan(&count); err != nil {
		t.Fatalf("count current-model #query vectors: %v", err)
	}
	return count
}

func doc2queryFactQueries(t *testing.T, ctx context.Context, path, name string) []string {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open query store: %v", err)
	}
	defer st.Close() //nolint:errcheck
	queries, err := memory.NewEntryStore(st.DB()).FactQueries(ctx, name)
	if err != nil {
		t.Fatalf("fact queries: %v", err)
	}
	return queries
}

func readJSONObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return result
}

func doc2queryDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
