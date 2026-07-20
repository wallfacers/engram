package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/wallfacers/engram/memory"
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
