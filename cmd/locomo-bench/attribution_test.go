package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestAttributionTraceGoldenRankOutrankersAndCorrectness(t *testing.T) {
	qa := locomoQA{
		Evidence:     []string{"D3:14"},
		Category:     1,
		CategoryName: "single_hop",
	}
	hits := []memory.Result{
		{Name: "chunk-aerial-yoga", Score: 0.031},
		{Name: "chunk-hot-yoga", Score: 0.029},
		{Name: "chunk-pilates", Score: 0.027},
		{Name: "chunk-kundalini-yoga", Score: 0.024},
	}
	chunkTurns := map[string][]string{
		"chunk-aerial-yoga":    {"D2:7"},
		"chunk-hot-yoga":       {"D1:9"},
		"chunk-pilates":        {"D4:2"},
		"chunk-kundalini-yoga": {"D3:14", "D3:15"},
	}

	correct := true
	trace := buildAttributionTrace(2, 111, qa, hits, hits, chunkTurns, 4, 2, &correct)
	if trace.GoldRank != 4 {
		t.Fatalf("gold_rank = %d, want 4", trace.GoldRank)
	}
	if !trace.GoldInPool {
		t.Fatal("gold_in_pool = false, want true")
	}
	gotOutrankers := []string{trace.OutrankedBy[0].Name, trace.OutrankedBy[1].Name}
	if want := []string{"chunk-aerial-yoga", "chunk-hot-yoga"}; !reflect.DeepEqual(gotOutrankers, want) {
		t.Fatalf("outranked_by = %v, want %v", gotOutrankers, want)
	}
	if trace.Quadrant != quadrantOK {
		t.Fatalf("quadrant with correct=true = %q, want %q", trace.Quadrant, quadrantOK)
	}
	if !reflect.DeepEqual(trace.Retrieved[3].MappedGoldTurns, []string{"D3:14"}) {
		t.Fatalf("mapped_gold_turns = %v, want [D3:14]", trace.Retrieved[3].MappedGoldTurns)
	}

	correct = false
	trace = buildAttributionTrace(2, 111, qa, hits, hits, chunkTurns, 4, 2, &correct)
	if trace.Quadrant != quadrantAnswerSide {
		t.Fatalf("quadrant with correct=false = %q, want %q", trace.Quadrant, quadrantAnswerSide)
	}

	raw, err := json.Marshal(trace.Retrieved[0])
	if err != nil {
		t.Fatalf("marshal retrieved hit: %v", err)
	}
	if strings.Contains(string(raw), "per_signal_ranks") {
		t.Fatalf("US1 retrieved hit unexpectedly serialized per_signal_ranks: %s", raw)
	}
}

func TestAttributionQuadrantsAreMutuallyExclusiveAndExhaustive(t *testing.T) {
	correct, wrong := true, false
	tests := []struct {
		name         string
		goldResolved bool
		goldInPool   bool
		goldRank     int
		topK         int
		correct      *bool
		want         string
	}{
		{name: "top-k correct", goldResolved: true, goldInPool: true, goldRank: 1, topK: 3, correct: &correct, want: quadrantOK},
		{name: "top-k wrong", goldResolved: true, goldInPool: true, goldRank: 3, topK: 3, correct: &wrong, want: quadrantAnswerSide},
		{name: "wide pool only", goldResolved: true, goldInPool: true, goldRank: -1, topK: 3, correct: &wrong, want: quadrantUS2Target},
		{name: "outside wide pool", goldResolved: true, goldInPool: false, goldRank: -1, topK: 3, correct: &wrong, want: quadrantExtractionSide},
		{name: "unresolved wins", goldResolved: false, goldInPool: true, goldRank: 1, topK: 3, correct: &correct, want: quadrantGoldUnresolved},
		{name: "missing join", goldResolved: true, goldInPool: true, goldRank: 1, topK: 3, correct: nil, want: quadrantRetrievalOnly},
	}

	seen := map[string]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAttribution(tt.goldResolved, tt.goldInPool, tt.goldRank, tt.topK, tt.correct)
			if got != tt.want {
				t.Fatalf("classifyAttribution() = %q, want %q", got, tt.want)
			}
			seen[got] = true
		})
	}
	for _, quadrant := range []string{quadrantOK, quadrantAnswerSide, quadrantUS2Target, quadrantExtractionSide} {
		if !seen[quadrant] {
			t.Errorf("gradeable quadrant %q was not classified", quadrant)
		}
	}
}

func TestAttributionDistributionExcludesGoldUnresolvedFromDenominator(t *testing.T) {
	traces := []AttributionTrace{
		{CategoryName: "single_hop", Quadrant: quadrantOK},
		{CategoryName: "single_hop", Quadrant: quadrantAnswerSide},
		{CategoryName: "single_hop", Quadrant: quadrantUS2Target},
		{CategoryName: "single_hop", Quadrant: quadrantExtractionSide},
		{CategoryName: "single_hop", Quadrant: quadrantGoldUnresolved},
	}

	distribution := summarizeAttribution(traces)["single_hop"]
	if distribution.TotalGradeable != 4 {
		t.Fatalf("total_gradeable = %d, want 4", distribution.TotalGradeable)
	}
	if distribution.GoldUnresolved != 1 {
		t.Fatalf("gold_unresolved = %d, want 1", distribution.GoldUnresolved)
	}
	if distribution.Q1OK != 1 || distribution.Q2AnswerSide != 1 || distribution.Q3US2Target != 1 || distribution.Q4ExtractionSide != 1 {
		t.Fatalf("quadrant counts = %+v, want one in each gradeable quadrant", distribution)
	}
}

func TestAttributionCorrectnessJoinAndMissingJoinFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results-hybrid.jsonl")
	contents := "{\"conv\":2,\"q\":111,\"correct\":true}\n{\"conv\":2,\"q\":112,\"correct\":false}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write join fixture: %v", err)
	}

	joined, err := loadAttributionCorrectness(path)
	if err != nil {
		t.Fatalf("load correctness join: %v", err)
	}
	if got := joined[resultKey{Conv: 2, Q: 111}]; got == nil || !*got {
		t.Fatalf("joined correct value = %v, want true", got)
	}
	if got := classifyAttribution(true, true, 1, 3, nil); got != quadrantRetrievalOnly {
		t.Fatalf("missing join quadrant = %q, want %q", got, quadrantRetrievalOnly)
	}
	trace := buildAttributionTrace(2, 111, locomoQA{Evidence: []string{"D3:14"}}, nil, nil, nil, 3, 5, nil)
	raw, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal retrieval-only trace: %v", err)
	}
	if strings.Contains(string(raw), "\"correct\"") || strings.Contains(string(raw), "correct_source") {
		t.Fatalf("retrieval-only trace serialized absent join fields: %s", raw)
	}
}

func TestAttributionGoldOnlyInWidePoolTargetsUS2(t *testing.T) {
	qa := locomoQA{Evidence: []string{"D2:8"}, Category: 1, CategoryName: "multi_hop"}
	topHits := []memory.Result{{Name: "competitor-a", Score: 0.03}, {Name: "competitor-b", Score: 0.02}}
	wideHits := append(append([]memory.Result(nil), topHits...), memory.Result{Name: "chunk-wide-gold", Score: 0.01})
	chunkTurns := map[string][]string{"chunk-wide-gold": {"D2:8"}}
	wrong := false

	trace := buildAttributionTrace(0, 0, qa, topHits, wideHits, chunkTurns, 2, 5, &wrong)
	if !trace.GoldInPool || trace.GoldRank != -1 || trace.Quadrant != quadrantUS2Target {
		t.Fatalf("wide-pool trace = %+v, want gold_in_pool=true gold_rank=-1 quadrant=%q", trace, quadrantUS2Target)
	}
}

func TestAttributionJoinMustCoverEverySelectedQuestionWhenProvided(t *testing.T) {
	convs := []conversation{{
		ID: 0,
		QA: []locomoQA{
			{Question: "first", Category: 1},
			{Question: "second", Category: 2},
		},
	}}
	value := true
	partial := map[resultKey]*bool{{Conv: 0, Q: 0}: &value}
	opt := options{joinResults: "results-hybrid.jsonl"}

	err := validateAttributionJoinCoverage(convs, opt, partial)
	if err == nil || !strings.Contains(err.Error(), "conv=0 question=1") {
		t.Fatalf("partial join error = %v, want missing conv=0 question=1", err)
	}
	if err := validateAttributionJoinCoverage(convs, options{}, partial); err != nil {
		t.Fatalf("absent join should degrade to retrieval_only, got %v", err)
	}
}

func TestAttributionCLIUsesPersistedStoreWithoutAnswerOrJudgeCredentials(t *testing.T) {
	datasetPath := filepath.Join(t.TempDir(), "locomo.json")
	dataset := `[{
  "conversation": {
    "session_1": [
      {"speaker":"Alex","text":"I practice kundalini yoga every morning.","dia_id":"D1:1"}
    ]
  },
  "qa": [{
    "question":"kundalini yoga",
    "answer":"kundalini yoga",
    "evidence":["D1:1"],
    "category":4
  }]
}]`
	if err := os.WriteFile(datasetPath, []byte(dataset), 0o644); err != nil {
		t.Fatalf("write dataset fixture: %v", err)
	}
	convs, err := loadDataset(datasetPath)
	if err != nil {
		t.Fatalf("load dataset fixture: %v", err)
	}

	storeDir := t.TempDir()
	buildOpt := options{datasetFormat: "locomo", retrieval: "fts", topK: 5, chunks: true, storeDir: storeDir}
	extract := func(context.Context, string, string) (string, error) {
		return `{"facts":[{"fact":"Alex practices kundalini yoga every morning.","entities":["kundalini yoga"],"category":"event"}]}`, nil
	}
	runtime, err := buildConversationRuntime(context.Background(), buildOpt, convs[0], extract, nil, []string{"fts"}, slog.Default())
	if err != nil {
		t.Fatalf("build persisted fixture store: %v", err)
	}
	runtime.Close()

	joinPath := filepath.Join(t.TempDir(), "results-hybrid.jsonl")
	if err := os.WriteFile(joinPath, []byte("{\"conv\":0,\"q\":0,\"correct\":true}\n"), 0o644); err != nil {
		t.Fatalf("write join fixture: %v", err)
	}
	runDir := t.TempDir()
	t.Setenv("LOCOMO_API_KEY", "")

	originalArgs := os.Args
	originalFlags := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("locomo-bench-test", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{
		"locomo-bench",
		"--attribution-trace",
		"--data", datasetPath,
		"--run-dir", runDir,
		"--store-dir", storeDir,
		"--retrieval", "fts",
		"--top-k", "5",
		"--chunks",
		"--join-results", joinPath,
		"--outrank-cap", "2",
		"--wide-pool", "20",
	}
	t.Cleanup(func() {
		os.Args = originalArgs
		flag.CommandLine = originalFlags
	})

	if err := run(); err != nil {
		t.Fatalf("run attribution CLI without answer/judge credentials: %v", err)
	}

	traceRaw, err := os.ReadFile(filepath.Join(runDir, "trace.jsonl"))
	if err != nil {
		t.Fatalf("read trace.jsonl: %v", err)
	}
	var trace AttributionTrace
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(traceRaw))), &trace); err != nil {
		t.Fatalf("parse trace.jsonl: %v", err)
	}
	if trace.Conv != 0 || trace.Q != 0 || trace.GoldRank < 1 || trace.Correct == nil || !*trace.Correct || trace.Quadrant != quadrantOK {
		t.Fatalf("trace = %+v, want joined q1 attribution with a gold hit", trace)
	}

	distributionRaw, err := os.ReadFile(filepath.Join(runDir, "quadrant-distribution.json"))
	if err != nil {
		t.Fatalf("read quadrant distribution: %v", err)
	}
	var distribution map[string]QuadrantDistribution
	if err := json.Unmarshal(distributionRaw, &distribution); err != nil {
		t.Fatalf("parse quadrant distribution: %v", err)
	}
	if distribution["single_hop"].Q1OK != 1 || distribution["single_hop"].TotalGradeable != 1 {
		t.Fatalf("distribution = %+v, want one gradeable q1 item", distribution)
	}
	if _, err := os.Stat(filepath.Join(runDir, "cost.json")); !os.IsNotExist(err) {
		t.Fatalf("cost.json exists in retrieval-only run; answer machinery was not bypassed")
	}
}
