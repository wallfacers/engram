package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func newCoverageTestRuntime(t *testing.T, opt options) (conversation, *conversationRuntime, []string) {
	t.Helper()
	ctx := context.Background()
	conv := conversation{
		ID: 0,
		Sessions: []session{{
			Index: 1,
			Turns: []turn{
				{Speaker: "user", Text: "I adopted a golden retriever puppy named Max last weekend.", DiaID: "D1:1"},
				{Speaker: "assistant", Text: "Congratulations on the new puppy!", DiaID: "D1:2"},
			},
		}},
		QA: []locomoQA{{
			Question: "adopted golden retriever puppy",
			Answer:   []byte(`"golden retriever"`),
			Evidence: []string{"D1:1"},
			Category: 4,
		}},
	}
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		t.Fatalf("parse arms: %v", err)
	}
	extract := func(context.Context, string, string) (string, error) {
		return `{"facts":[{"fact":"The user adopted a golden retriever puppy named Max.","entities":["Max"],"category":"event"}]}`, nil
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extract, nil, arms, slog.Default())
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	return conv, runtime, arms
}

func TestComputeCoverageGradesTurnRecallOffline(t *testing.T) {
	ctx := context.Background()
	opt := options{
		datasetFormat: "locomo",
		retrieval:     "fts",
		topK:          10,
		chunks:        true,
		storeDir:      t.TempDir(),
	}
	conv, runtime, arms := newCoverageTestRuntime(t, opt)
	defer runtime.Close()

	reports, err := computeCoverage(ctx, opt, []conversation{conv}, []*conversationRuntime{runtime}, arms, slog.Default())
	if err != nil {
		t.Fatalf("compute coverage: %v", err)
	}
	if len(reports) != 1 || reports[0].Arm != "fts" {
		t.Fatalf("reports = %+v, want one fts arm", reports)
	}
	overall := reports[0].Overall
	if overall.N != 1 {
		t.Fatalf("graded n = %d, want 1", overall.N)
	}
	// The verbatim chunk covering D1:1 is keyword-retrievable, so exact-turn
	// recall must be a full 1.0 — proving the offline ruler actually resolves
	// retrieved chunks back to the gold turn.
	if overall.TurnRecall != 1 {
		t.Fatalf("turn recall = %v, want 1 (chunk covering D1:1 retrieved)", overall.TurnRecall)
	}
	if overall.SessionRecall != 1 {
		t.Fatalf("session recall = %v, want 1", overall.SessionRecall)
	}
}

func TestRunCoverageWritesCoverageJSON(t *testing.T) {
	ctx := context.Background()
	runDir := t.TempDir()
	opt := options{
		datasetFormat: "locomo",
		retrieval:     "fts",
		topK:          10,
		chunks:        true,
		storeDir:      t.TempDir(),
		runDir:        runDir,
	}
	conv, runtime, arms := newCoverageTestRuntime(t, opt)
	defer runtime.Close()

	if err := runCoverage(ctx, opt, []conversation{conv}, []*conversationRuntime{runtime}, arms, slog.Default()); err != nil {
		t.Fatalf("run coverage: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "coverage.json"))
	if err != nil {
		t.Fatalf("read coverage.json: %v", err)
	}
	var reports []coverageArmReport
	if err := json.Unmarshal(raw, &reports); err != nil {
		t.Fatalf("parse coverage.json: %v", err)
	}
	if len(reports) != 1 || reports[0].Overall == nil || reports[0].Overall.N != 1 {
		t.Fatalf("coverage.json = %+v, want one arm graded n=1", reports)
	}
	if reports[0].Overall.TurnRecall != 1 {
		t.Fatalf("persisted turn recall = %v, want 1", reports[0].Overall.TurnRecall)
	}
}

func TestEvidenceRecallAtGradesTurnAndSessionWithoutSweep(t *testing.T) {
	qa := locomoQA{Evidence: []string{"D1:3", "D2:1"}}
	hits := []memory.Result{
		{Name: "chunk-c0-s1-000", SourceSessionID: "conv0-sess1"},
		{Name: "chunk-c0-s2-005", SourceSessionID: "conv0-sess2"},
	}
	chunkTurns := map[string][]string{
		"chunk-c0-s1-000": {"D1:1", "D1:2", "D1:3"}, // covers gold turn D1:3
		"chunk-c0-s2-005": {"D2:8", "D2:9"},         // session 2, wrong turn
	}
	// Unlike newSweepEvidenceDiagnostics, this must grade even with no sweep.
	turn, session, gradeable := evidenceRecallAt(qa, hits, chunkTurns)
	if !gradeable {
		t.Fatal("qa with parseable evidence must be gradeable")
	}
	if session != 1 {
		t.Fatalf("session recall = %v, want 1 (both sessions hit)", session)
	}
	if turn != 0.5 {
		t.Fatalf("turn recall = %v, want 0.5 (D2:1 not covered)", turn)
	}
}

func TestEvidenceRecallAtUngradeableWhenNoParseableEvidence(t *testing.T) {
	qa := locomoQA{Evidence: []string{"", "not-a-ref"}}
	_, _, gradeable := evidenceRecallAt(qa, nil, nil)
	if gradeable {
		t.Fatal("qa without parseable D<s>:<t> evidence must be ungradeable")
	}
}

func TestCoverageAccumulatorMeansPerCategoryAndOverall(t *testing.T) {
	acc := newCoverageAccumulator("hybrid+sweep", 10)
	acc.add("multi-hop", 0.5, 1.0)
	acc.add("multi-hop", 1.0, 1.0)
	acc.add("temporal", 0.0, 0.5)

	rep := acc.report()
	if rep.Arm != "hybrid+sweep" || rep.TopK != 10 {
		t.Fatalf("report head = %+v", rep)
	}
	if rep.Overall.N != 3 {
		t.Fatalf("overall n = %d, want 3", rep.Overall.N)
	}
	if got := rep.Overall.TurnRecall; got != 0.5 {
		t.Fatalf("overall turn recall = %v, want 0.5 ((0.5+1+0)/3)", got)
	}
	mh := rep.ByCategory["multi-hop"]
	if mh == nil || mh.N != 2 || mh.TurnRecall != 0.75 {
		t.Fatalf("multi-hop bucket = %+v, want n=2 turn=0.75", mh)
	}
	if tp := rep.ByCategory["temporal"]; tp == nil || tp.SessionRecall != 0.5 {
		t.Fatalf("temporal bucket = %+v, want session=0.5", tp)
	}
}

func TestSelectorMetricsGateThresholds(t *testing.T) {
	bucket := &coverageBucket{}
	bucket.addSelectionMetrics(selectionMetricInput{
		Candidates: []memory.Result{{Name: "chunk-a"}, {Name: "chunk-b"}, {Name: "chunk-c"}},
		Selected:   []memory.Result{{Name: "chunk-a"}, {Name: "chunk-c"}},
		GoldTurns:  []string{"D1:1", "D1:2", "D1:3"},
		ChunkTurns: map[string][]string{
			"chunk-a": {"D1:1", "D1:2"},
			"chunk-b": {"D1:3"},
		},
	})
	bucket.addSelectionMetrics(selectionMetricInput{
		Candidates: []memory.Result{{Name: "chunk-d"}},
		Selected:   []memory.Result{{Name: "chunk-d"}},
		GoldTurns:  []string{"D2:1"},
		ChunkTurns: map[string][]string{"chunk-d": {"D2:1"}},
	})
	bucket.finalize()
	if bucket.SelectionSurvival != 0.75 {
		t.Fatalf("selection_survival = %v, want 0.75", bucket.SelectionSurvival)
	}
	if bucket.ComplementDrop != 0.5 {
		t.Fatalf("complement_drop = %v, want 0.5", bucket.ComplementDrop)
	}
	if bucket.AnchorViolation != 1 {
		t.Fatalf("anchor_violation = %d, want 1", bucket.AnchorViolation)
	}
}

func TestCoverageEmitsSelectorMetrics(t *testing.T) {
	bucket := &coverageBucket{N: 1, SelectionSurvival: 1, ComplementDrop: 0.25, AnchorViolation: 2}
	raw, err := json.Marshal(bucket)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"selection_survival", "complement_drop", "anchor_violation"} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("coverage JSON %s does not contain %q", raw, field)
		}
	}
}

func TestCoverageThreeArmPCICIntegrationUsesNoAnswerOrJudgeLLM(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	chunkTurns := make(map[string][]string)
	for i := 1; i <= 18; i++ {
		name := fmt.Sprintf("fact-%02d", i)
		content := fmt.Sprintf("Alice fact-%02d evidence", i)
		if err := entries.Upsert(ctx, &memory.Entry{Name: name, Trigger: "Alice", Content: content, CharCount: len(content)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 14; i++ {
		name := fmt.Sprintf("chunk-%02d", i)
		content := fmt.Sprintf("Alice chunk-%02d evidence", i)
		if err := entries.Upsert(ctx, &memory.Entry{Name: name, Trigger: "Alice", Content: content, Category: "chunk", CharCount: len(content)}); err != nil {
			t.Fatal(err)
		}
		turnID := fmt.Sprintf("D1:%d", i)
		chunkTurns[name] = []string{turnID}
	}
	if err := entries.PutEntities(ctx, "chunk-13", []string{"Alice"}); err != nil {
		t.Fatal(err)
	}

	rr := &coverageFixtureReranker{}
	arms := []string{"hybrid+rerank", "hybrid+rerank+pcic", "hybrid+rerank+oracle"}
	retrievers := make(map[string]*memory.Retriever, len(arms))
	rereranked := make(map[string]bool, len(arms))
	for _, arm := range arms {
		retrievers[arm] = memory.NewRetrieverWithOptions(entries, vectors, nil, rr, memory.RetrieverOptions{})
		rereranked[arm] = true
	}
	meta := &PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "fixture", DatasetFingerprint: "fixture", Count: 1},
		Spans: map[string]SpanClaim{
			// conv-scoped key: the selector looks spans up by conversation id.
			pcicSpanKey(0, "D1:13"): claim(pcicSpanKey(0, "D1:13"), "alice", "job", "acme", "current"),
		},
	}
	runtime := &conversationRuntime{
		store:      st,
		entries:    entries,
		retrievers: retrievers,
		reranked:   rereranked,
		chunkTurns: chunkTurns,
	}
	conv := conversation{ID: 0, QA: []locomoQA{{
		Question:     "Alice",
		Evidence:     []string{"D1:1", "D1:13"},
		Category:     3,
		CategoryName: "multi-hop",
	}}}
	opt := options{
		datasetFormat: "locomo",
		retrieval:     strings.Join(arms, ","),
		topK:          30,
		chunkQuota:    12,
		chunks:        true,
		coverageOnly:  true,
		concurrency:   1,
		pcicMeta:      meta,
	}
	reports, err := computeCoverage(ctx, opt, []conversation{conv}, []*conversationRuntime{runtime}, arms, slog.Default())
	if err != nil {
		t.Fatalf("computeCoverage: %v", err)
	}
	if rr.calls != len(arms) {
		t.Fatalf("reranker calls = %d, want one retrieval-only call per arm (%d)", rr.calls, len(arms))
	}
	if len(reports) != 3 {
		t.Fatalf("reports = %d, want 3", len(reports))
	}
	byArm := make(map[string]coverageArmReport, len(reports))
	for _, report := range reports {
		byArm[report.Arm] = report
	}
	assertCoverageMetrics(t, byArm["hybrid+rerank"], 0.5, 1, 1, 0)
	assertCoverageMetrics(t, byArm["hybrid+rerank+pcic"], 1, 1, 0, 0)
	assertCoverageMetrics(t, byArm["hybrid+rerank+oracle"], 1, 1, 0, 1)
}

func assertCoverageMetrics(t *testing.T, report coverageArmReport, turnRecall, survival, complementDrop float64, anchorViolation int) {
	t.Helper()
	if report.Overall == nil || report.Overall.N != 1 {
		t.Fatalf("arm %q report = %+v", report.Arm, report)
	}
	got := report.Overall
	if got.TurnRecall != turnRecall || got.SelectionSurvival != survival || got.ComplementDrop != complementDrop || got.AnchorViolation != anchorViolation {
		t.Fatalf("arm %q metrics = turn:%v survival:%v drop:%v anchor:%d, want %v/%v/%v/%d",
			report.Arm, got.TurnRecall, got.SelectionSurvival, got.ComplementDrop, got.AnchorViolation,
			turnRecall, survival, complementDrop, anchorViolation)
	}
	category := report.ByCategory["multi-hop"]
	if category == nil || category.TurnRecall != turnRecall || category.SelectionSurvival != survival || category.ComplementDrop != complementDrop || category.AnchorViolation != anchorViolation {
		t.Fatalf("arm %q category metrics = %+v", report.Arm, category)
	}
}

type coverageFixtureReranker struct {
	calls int
}

func (*coverageFixtureReranker) Model() string { return "fixture" }

func (r *coverageFixtureReranker) Rerank(_ context.Context, _ string, documents []string, topN int) ([]embedding.RankedDoc, error) {
	r.calls++
	ranked := make([]embedding.RankedDoc, len(documents))
	for i, document := range documents {
		score := 0.0
		var number int
		switch {
		case strings.Contains(document, "chunk-"):
			_, _ = fmt.Sscanf(document, "Alice chunk-%d evidence", &number)
			score = 100 - float64(number)
		case strings.Contains(document, "fact-"):
			_, _ = fmt.Sscanf(document, "Alice fact-%d evidence", &number)
			score = 200 - float64(number)
		}
		ranked[i] = embedding.RankedDoc{Index: i, Score: score}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	if topN > 0 && topN < len(ranked) {
		ranked = ranked[:topN]
	}
	return ranked, nil
}
