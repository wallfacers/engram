package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

type aliasShadowTestEmbedder struct{}

func (aliasShadowTestEmbedder) Model() string { return "alias-shadow-test" }

func (aliasShadowTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i + 1), 1, 0}
	}
	return vectors, nil
}

func TestReembed_CopyStoreDirCopiesSQLiteSidecars(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "alias-store")
	want := map[string]string{
		"conv0.db":     "database",
		"conv0.db-wal": "write-ahead-log",
		"conv0.db-shm": "shared-memory",
	}
	for name, content := range want {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o640); err != nil {
			t.Fatalf("write source %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("do not copy"), 0o644); err != nil {
		t.Fatalf("write unrelated source file: %v", err)
	}

	if err := copyStoreDir(src, dst); err != nil {
		t.Fatalf("copy store dir: %v", err)
	}
	for name, content := range want {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("read copied %s: %v", name, err)
		}
		if string(got) != content {
			t.Fatalf("copied %s = %q, want %q", name, got, content)
		}
		source, err := os.ReadFile(filepath.Join(src, name))
		if err != nil || string(source) != content {
			t.Fatalf("source %s changed: %q, err=%v", name, source, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("unrelated file copied, stat err=%v", err)
	}
}

func TestReembed_AliasShadowModesUseCopiesAndNeverReextract(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	seedAliasShadowCanonicalStore(t, ctx, filepath.Join(sourceDir, "conv0.db"))
	if got := aliasShadowVectorCount(t, ctx, filepath.Join(sourceDir, "conv0.db")); got != 0 {
		t.Fatalf("canonical shadow count before run = %d, want 0", got)
	}

	for _, mode := range []string{aliasShadowBaseline, aliasShadowTreatment} {
		t.Run(mode, func(t *testing.T) {
			var extractCalls atomic.Int32
			opt := options{
				runDir:        t.TempDir(),
				storeDir:      sourceDir,
				datasetFormat: "locomo",
				retrieval:     "hybrid",
				topK:          aliasShadowTopK,
				aliasShadow:   mode,
			}
			if err := prepareAliasShadowStore(&opt); err != nil {
				t.Fatalf("prepare %s store: %v", mode, err)
			}
			wantCopyDir := filepath.Join(opt.runDir, aliasShadowStoreDirName)
			if opt.storeDir != wantCopyDir {
				t.Fatalf("effective store dir = %q, want %q", opt.storeDir, wantCopyDir)
			}

			runtime, err := buildConversationRuntime(ctx, opt, conversation{ID: 0}, func(context.Context, string, string) (string, error) {
				extractCalls.Add(1)
				return `{"facts":[]}`, nil
			}, aliasShadowTestEmbedder{}, []string{"hybrid"}, slog.Default())
			if err != nil {
				t.Fatalf("build %s runtime: %v", mode, err)
			}
			runtime.Close()

			if got := extractCalls.Load(); got != 0 {
				t.Fatalf("%s extraction calls = %d, want 0", mode, got)
			}
			copyCount := aliasShadowVectorCount(t, ctx, filepath.Join(opt.storeDir, "conv0.db"))
			if mode == aliasShadowBaseline && copyCount != 0 {
				t.Fatalf("baseline copy shadow count = %d, want 0", copyCount)
			}
			if mode == aliasShadowTreatment && copyCount == 0 {
				t.Fatal("treatment copy shadow count = 0, want > 0")
			}
			if got := aliasShadowVectorCount(t, ctx, filepath.Join(sourceDir, "conv0.db")); got != 0 {
				t.Fatalf("canonical shadow count after %s = %d, want 0", mode, got)
			}
			t.Logf("arm=%s copy_alias_count=%d canonical_alias_count=0 extraction_calls=0", mode, copyCount)
		})
	}
}

func TestReembed_RuntimeRefusesUnpreparedCanonicalStore(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	path := filepath.Join(sourceDir, "conv0.db")
	seedAliasShadowCanonicalStore(t, ctx, path)
	opt := options{
		aliasShadow: aliasShadowTreatment,
		runDir:      t.TempDir(),
		storeDir:    sourceDir,
		retrieval:   "hybrid",
		topK:        aliasShadowTopK,
	}
	runtime, err := buildConversationRuntime(ctx, opt, conversation{ID: 0}, func(context.Context, string, string) (string, error) {
		return `{"facts":[]}`, nil
	}, aliasShadowTestEmbedder{}, []string{"hybrid"}, slog.Default())
	if runtime != nil {
		runtime.Close()
	}
	if err == nil {
		t.Fatal("unprepared alias runtime opened canonical store, want error")
	}
	if got := aliasShadowVectorCount(t, ctx, path); got != 0 {
		t.Fatalf("canonical shadow count = %d after rejected runtime, want 0", got)
	}
}

func TestAliasShadow_ValidateRejectsConfoundedRuns(t *testing.T) {
	base := options{
		aliasShadow: aliasShadowTreatment,
		topK:        aliasShadowTopK,
		storeDir:    "/canonical",
		runDir:      "/run",
	}
	tests := []struct {
		name string
		opt  options
	}{
		{name: "multi-query", opt: func() options { o := base; o.multiQuery = true; return o }()},
		{name: "top-k", opt: func() options { o := base; o.topK = aliasShadowTopK - 1; return o }()},
		{name: "missing-store", opt: func() options { o := base; o.storeDir = ""; return o }()},
		{name: "unknown-enum", opt: func() options { o := base; o.aliasShadow = "maybe"; return o }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAliasShadowOptions(tt.opt); err == nil {
				t.Fatal("validation succeeded, want error")
			}
		})
	}
	if err := validateAliasShadowOptions(base); err != nil {
		t.Fatalf("valid treatment rejected: %v", err)
	}
	base.aliasShadow = aliasShadowBaseline
	if err := validateAliasShadowOptions(base); err != nil {
		t.Fatalf("valid baseline rejected: %v", err)
	}
}

func TestAliasShadow_GoldHasAliasUsesAttributedStoredFactNames(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "conv0.db")
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close() //nolint:errcheck
	entries := memory.NewEntryStore(st.DB())
	for _, entry := range []*memory.Entry{
		{Name: "gold-with-alias", Content: "Maya embraced painting.", SourceSessionID: "conv0-sess1"},
		{Name: "gold-without-alias", Content: "Maya visited Oslo.", SourceSessionID: "conv0-sess2"},
	} {
		entry.CharCount = len(entry.Content)
		if err := entries.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}
	if err := entries.PutAliases(ctx, "gold-with-alias", []string{"self-acceptance"}); err != nil {
		t.Fatalf("put aliases: %v", err)
	}

	tests := []struct {
		name string
		qa   locomoQA
		hit  memory.Result
		want bool
	}{
		{
			name: "attributed fact has alias",
			qa:   locomoQA{Evidence: []string{"D1:1"}},
			hit:  memory.Result{Name: "gold-with-alias", Content: "Maya embraced painting.", SourceSessionID: "conv0-sess1"},
			want: true,
		},
		{
			name: "attributed fact has no alias",
			qa:   locomoQA{Evidence: []string{"D2:1"}},
			hit:  memory.Result{Name: "gold-without-alias", Content: "Maya visited Oslo.", SourceSessionID: "conv0-sess2"},
			want: false,
		},
	}
	goldText := map[string]string{"D1:1": "Maya embraced painting.", "D2:1": "Maya visited Oslo."}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := attributedGoldFactNames(tt.qa, []memory.Result{tt.hit}, nil, goldText, defaultFactCoverageTau)
			if !reflect.DeepEqual(names, []string{tt.hit.Name}) {
				t.Fatalf("attributed names = %v, want [%s]", names, tt.hit.Name)
			}
			got, err := goldFactNamesHaveAlias(ctx, st.DB(), names)
			if err != nil {
				t.Fatalf("query aliases: %v", err)
			}
			if got != tt.want {
				t.Fatalf("gold_has_alias = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAliasShadow_RecallSummaryStratifiesAndDefinesImprovementDeltas(t *testing.T) {
	baseline := []aliasShadowRecallQuestion{
		{Conv: 0, Q: 0, Category: 1, GoldHasAlias: true, GoldResolved: true, GoldRank: 40, GoldRankAt30: -1, Gradeable: true, CoverageAt30: 0},
		{Conv: 0, Q: 1, Category: 1, GoldHasAlias: false, GoldResolved: true, GoldRank: 5, GoldRankAt30: 5, Gradeable: true, CoverageAt30: 1},
	}
	treatment := []aliasShadowRecallQuestion{
		{Conv: 0, Q: 0, Category: 1, GoldHasAlias: true, GoldResolved: true, GoldRank: 10, GoldRankAt30: 10, Gradeable: true, CoverageAt30: 1},
		{Conv: 0, Q: 1, Category: 1, GoldHasAlias: false, GoldResolved: true, GoldRank: 6, GoldRankAt30: 6, Gradeable: true, CoverageAt30: 1},
	}

	report, err := summarizeAliasShadowRecallDiagnostic(baseline, treatment, 1)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if report.Global.Questions != 2 || report.GoldHasAlias.Questions != 1 {
		t.Fatalf("strata sizes = global:%d alias:%d, want 2/1", report.Global.Questions, report.GoldHasAlias.Questions)
	}
	if report.GoldHasAlias.GoldEnteredTop30 != 1 || report.GoldHasAlias.GoldLeftTop30 != 0 {
		t.Fatalf("alias stratum entered/left = %d/%d, want 1/0", report.GoldHasAlias.GoldEnteredTop30, report.GoldHasAlias.GoldLeftTop30)
	}
	if report.GoldHasAlias.MeanGoldRankDelta != -30 {
		t.Fatalf("alias rank delta = %v, want -30 (treatment-baseline, negative improves)", report.GoldHasAlias.MeanGoldRankDelta)
	}
	if report.GoldHasAlias.MeanGoldRankAt30Delta != -21 {
		t.Fatalf("alias rank@30 delta = %v, want -21 (31 outside -> rank 10)", report.GoldHasAlias.MeanGoldRankAt30Delta)
	}
	if report.GoldHasAlias.MeanCoverageAt30Delta != 1 {
		t.Fatalf("alias coverage delta = %v, want +1 (treatment-baseline, positive improves)", report.GoldHasAlias.MeanCoverageAt30Delta)
	}
	if report.DeltaConvention == "" {
		t.Fatal("delta convention must be explicit")
	}
}

func TestAliasShadow_ContextParityRequiresFinalTop30(t *testing.T) {
	opt := options{aliasShadow: aliasShadowTreatment}
	valid := contextParityRecord{Arm: aliasShadowTreatment, FinalTopK: aliasShadowTopK, AnswerContextTokens: 1234, SubqueryCount: 1}
	if err := validateAliasShadowContextParity(opt, valid); err != nil {
		t.Fatalf("valid parity rejected: %v", err)
	}
	invalid := valid
	invalid.FinalTopK--
	if err := validateAliasShadowContextParity(opt, invalid); err == nil {
		t.Fatal("final_top_k != 30 accepted")
	}
	if got := contextParityArm(opt); got != aliasShadowTreatment {
		t.Fatalf("parity arm = %q, want %q", got, aliasShadowTreatment)
	}
}

func TestAliasShadow_RecallDiagnosticNeedsNoMultiQueryCaller(t *testing.T) {
	opt := options{
		aliasShadow:     aliasShadowBaseline,
		runDir:          "/run",
		storeDir:        "/copied-store",
		datasetFormat:   "locomo",
		chunks:          true,
		topK:            aliasShadowTopK,
		mqMaxSubqueries: 0,
	}
	if err := validateRecallDiagnosticOptions(opt, []string{"hybrid"}); err != nil {
		t.Fatalf("alias recall diagnostic unexpectedly requires a decomposition caller: %v", err)
	}
}

func TestAliasShadow_RecallDiagnosticRunsSingleQueryWithoutModelCallers(t *testing.T) {
	ctx := context.Background()
	sourceDir := t.TempDir()
	seedAliasShadowCanonicalStore(t, ctx, filepath.Join(sourceDir, "conv0.db"))
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
	for _, mode := range []string{aliasShadowBaseline, aliasShadowTreatment} {
		opt := options{
			aliasShadow:      mode,
			runDir:           runDir,
			storeDir:         sourceDir,
			datasetFormat:    "locomo",
			retrieval:        "hybrid",
			recallDiagnostic: true,
			chunks:           true,
			topK:             aliasShadowTopK,
			concurrency:      1,
			factCoverageTau:  defaultFactCoverageTau,
		}
		if err := prepareAliasShadowStore(&opt); err != nil {
			t.Fatalf("prepare %s diagnostic store: %v", mode, err)
		}
		if err := runAliasShadowRecallDiagnosticWithClient(ctx, opt, []conversation{conv}, []string{"hybrid"}, aliasShadowTestEmbedder{}, slog.Default()); err != nil {
			t.Fatalf("run %s diagnostic without model callers: %v", mode, err)
		}
		if _, err := os.Stat(filepath.Join(runDir, "alias_shadow_recall_"+mode+".jsonl")); err != nil {
			t.Fatalf("%s arm output: %v", mode, err)
		}
		if got := aliasShadowVectorCount(t, ctx, filepath.Join(sourceDir, "conv0.db")); got != 0 {
			t.Fatalf("canonical shadow count after %s diagnostic = %d, want 0", mode, got)
		}
	}

	reportBytes, err := os.ReadFile(filepath.Join(runDir, "alias_shadow_recall.json"))
	if err != nil {
		t.Fatalf("read combined report: %v", err)
	}
	var report aliasShadowRecallReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("decode combined report: %v", err)
	}
	if report.Global.Questions != 1 || report.DeltaConvention == "" {
		t.Fatalf("combined report = %+v, want one paired question and explicit delta convention", report)
	}
}

func seedAliasShadowCanonicalStore(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open canonical store: %v", err)
	}
	entries := memory.NewEntryStore(st.DB())
	entry := &memory.Entry{
		Name:            "painting-fact",
		Content:         "The user found a restorative hobby.",
		CharCount:       len("The user found a restorative hobby."),
		SourceSessionID: "conv0-sess1",
		FactSource:      "extraction",
	}
	if err := entries.Upsert(ctx, entry); err != nil {
		_ = st.Close()
		t.Fatalf("upsert canonical fact: %v", err)
	}
	if err := entries.PutAliases(ctx, entry.Name, []string{"painting", "self-acceptance"}); err != nil {
		_ = st.Close()
		t.Fatalf("put canonical aliases: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close canonical store: %v", err)
	}
}

func aliasShadowVectorCount(t *testing.T, ctx context.Context, path string) int {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open count store %s: %v", path, err)
	}
	defer st.Close() //nolint:errcheck
	var count int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name LIKE '%#alias'`).Scan(&count); err != nil {
		t.Fatalf("count alias shadow vectors: %v", err)
	}
	return count
}
