package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// TestNotebookAttributionInlineCapture verifies the --notebook inline capture end
// to end: with opt.notebook set and a turnText index supplied, the answer path
// computes gold_resolved/candidate_covered/bundle_covered against the ACTUAL
// candidate set and ACTUAL answer context. All model calls are stubbed.
func TestNotebookAttributionInlineCapture(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-c0-s1-000": "A flood hit the river area on 2023-05-08. The mayor declared an emergency.",
	})
	answerCall := navCallFromStub(&stubPlannerProvider{text: "the river area"})
	judgeCall := navCallFromStub(&stubPlannerProvider{text: `{"correct":true}`})
	noop := modelCallerFromUsage(navCallFromStub(&stubPlannerProvider{text: ""}))

	opt := options{
		topK:            30,
		chunkQuota:      12,
		forceAnswer:     true,
		noIDKRetry:      true,
		retrieval:       "hybrid",
		factCoverageTau: defaultFactCoverageTau,
		notebook:        true,
	}
	qa := locomoQA{
		Question:   "What area was hit by the flood?",
		Answer:     json.RawMessage(`"the river area"`),
		Evidence:   []string{"D1:1"},
		Category:   4,
		QuestionID: "conv-0-q-1",
	}
	chunkTurns := map[string][]string{"chunk-c0-s1-000": {"D1:1"}}
	turnText := map[string]string{"D1:1": "A flood hit the river area"}

	correct, _, _, _, _, _, att := answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(
		context.Background(), r, answerCall, noop, noop, judgeCall, opt, qa,
		chunkTurns, turnText, nil, slog.Default(),
	)
	if !correct {
		t.Fatalf("stub judge returns correct=true; got correct=%t", correct)
	}
	if att == nil {
		t.Fatalf("--notebook must return an attribution")
	}
	if !att.GoldResolved {
		t.Fatalf("gold evidence D1:1 must resolve; got %+v", att)
	}
	if att.ContextPreview == "" {
		t.Fatalf("ContextPreview should carry the actual answer context")
	}
	// Note: candidate/bundle coverage depend on FTS retrieval, which on a single
	// in-memory chunk may legitimately miss a full-sentence query (FTS trigram
	// against one entry). Coverage semantics are covered by
	// TestComputeNotebookAttribution / TestEvidenceCoversGold*; here we only pin
	// the wiring: a non-nil attribution with gold_resolved + context preview.

	// --notebook off → nil attribution (parity; results unchanged).
	opt.notebook = false
	_, _, _, _, _, _, off := answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(
		context.Background(), r, answerCall, noop, noop, judgeCall, opt, qa,
		chunkTurns, turnText, nil, slog.Default(),
	)
	if off != nil {
		t.Fatalf("--notebook off must return nil attribution, got %+v", off)
	}
}

func TestClassifyNotebookMiss(t *testing.T) {
	cases := []struct {
		name                                 string
		goldResolved, cand, bundle, majority bool
		want                                 evalMissClass
	}{
		{"unresolved", false, false, false, false, evalMissGoldUnresolved},
		{"unresolved-even-if-covered", false, true, true, false, evalMissGoldUnresolved},
		{"candidate-miss", true, false, false, false, evalMissCandidate},
		{"compiler-miss", true, true, false, false, evalMissCompiler},
		{"answerer-miss", true, true, true, false, evalMissAnswerer},
		{"success", true, true, true, true, evalMissSuccess},
		// answered questions are never a miss, even when the pipeline gap is real
		// (weak retrieval). The gap stays visible in the record booleans, but a
		// correct answer must not pollute the mistake book's miss_class counts.
		{"correct-but-weak-retrieval", true, false, false, true, evalMissSuccess},
		{"correct-but-gold-unresolved", false, false, false, true, evalMissSuccess},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyNotebookMiss(c.goldResolved, c.cand, c.bundle, c.majority)
			if got != c.want {
				t.Fatalf("classifyNotebookMiss = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEvidenceCoversGoldChunk(t *testing.T) {
	// chunk path: exact turn-id overlap via chunkTurns.
	chunkTurns := map[string][]string{
		"chunk-c0-s1-000": {"D1:1", "D1:2"},
		"chunk-c0-s1-001": {"D1:3"},
	}
	hit := memory.Result{Name: "chunk-c0-s1-001", Content: "irrelevant"}
	got := evidenceCoversGold([]memory.Result{hit}, chunkTurns, nil, []string{"D1:3"}, defaultFactCoverageTau)
	if !got {
		t.Fatalf("chunk covering gold turn D1:3 should be a hit")
	}
	miss := memory.Result{Name: "chunk-c0-s1-000", Content: "irrelevant"}
	if evidenceCoversGold([]memory.Result{miss}, chunkTurns, nil, []string{"D1:3"}, defaultFactCoverageTau) {
		t.Fatalf("chunk not covering D1:3 must not be a hit")
	}
}

func TestEvidenceCoversGoldFact(t *testing.T) {
	// fact path: session-gated directional lexical containment (τ=0.8).
	chunkTurns := map[string][]string{}
	turnText := map[string]string{"D1:3": "Maria moved to Paris in June"}
	goldTurns := []string{"D1:3"}
	// SourceSessionID "conv0sess1" → factSessionNumber extracts 1; gold D1 → session 1.
	factHit := memory.Result{
		Name:            "fact-xyz",
		Content:         "Maria moved to Paris",
		SourceSessionID: "conv0sess1",
	}
	if !evidenceCoversGold([]memory.Result{factHit}, chunkTurns, turnText, goldTurns, defaultFactCoverageTau) {
		t.Fatalf("fact covering gold words in same session should be a hit")
	}
	// different session → rejected by the session gate.
	wrongSession := memory.Result{Name: "fact-xyz", Content: "Maria moved to Paris", SourceSessionID: "conv0sess2"}
	if evidenceCoversGold([]memory.Result{wrongSession}, chunkTurns, turnText, goldTurns, defaultFactCoverageTau) {
		t.Fatalf("fact from a different session must not cover the gold turn")
	}
}

func TestComputeNotebookAttribution(t *testing.T) {
	chunkTurns := map[string][]string{"chunk-c0-s1-000": {"D1:1"}}
	turnText := map[string]string{"D1:1": "Jon started a dance studio"}
	goldTurns := []string{"D1:1"}
	present := memory.Result{Name: "chunk-c0-s1-000", Content: "x"}
	absent := memory.Result{Name: "chunk-c0-s1-999", Content: "y"}

	// candidate covers, bundle (context) does not → compiler miss territory.
	att := computeNotebookAttribution([]memory.Result{present}, []memory.Result{absent}, chunkTurns, turnText, goldTurns, defaultFactCoverageTau)
	if !att.GoldResolved || !att.CandidateCovered || att.BundleCovered {
		t.Fatalf("want resolved+candidate-covered+not-bundle: %+v", att)
	}
	if att.CandidateCount != 1 {
		t.Fatalf("CandidateCount = %d, want 1", att.CandidateCount)
	}
	// evidence-ID audit trail is persisted for offline re-attribution.
	if len(att.CandidateIDs) != 1 || att.CandidateIDs[0] != "chunk-c0-s1-000" {
		t.Fatalf("CandidateIDs = %v, want [chunk-c0-s1-000]", att.CandidateIDs)
	}
	if len(att.BundleEvidenceIDs) != 1 || att.BundleEvidenceIDs[0] != "chunk-c0-s1-999" {
		t.Fatalf("BundleEvidenceIDs = %v, want [chunk-c0-s1-999]", att.BundleEvidenceIDs)
	}

	// empty gold evidence → unresolved.
	att = computeNotebookAttribution([]memory.Result{present}, []memory.Result{present}, chunkTurns, turnText, nil, defaultFactCoverageTau)
	if att.GoldResolved {
		t.Fatalf("empty gold turns must be unresolved")
	}
}

func TestAppendNotebookRecordsDedupe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notebook.jsonl")
	base := notebookRecord{RunID: "run1", QuestionID: "conv-0-q-1", RetrievalArm: "fts"}
	if _, err := appendNotebookRecords(path, []notebookRecord{base}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// same run+question+arm must dedupe.
	if n, err := appendNotebookRecords(path, []notebookRecord{base}); err != nil || n != 0 {
		t.Fatalf("dedupe: n=%d err=%v, want n=0", n, err)
	}
	// different arm must append.
	other := base
	other.RetrievalArm = "hybrid"
	if n, err := appendNotebookRecords(path, []notebookRecord{other}); err != nil || n != 1 {
		t.Fatalf("new arm append: n=%d err=%v, want n=1", n, err)
	}
	recs, err := loadNotebookRecords(path)
	if err != nil || len(recs) != 2 {
		t.Fatalf("loaded %d recs err=%v, want 2", len(recs), err)
	}
}

func TestCollectNotebookResultsMajority(t *testing.T) {
	dir := t.TempDir()
	// run-1 and run-2 each hold a result for the same question.
	for i, rep := range []bool{true, false, true} {
		sub := dir
		if i > 0 {
			sub = filepath.Join(dir, "run-"+string(rune('1'+i)))
		}
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		j, err := openJournal(sub, "fts")
		if err != nil {
			t.Fatal(err)
		}
		j.write(result{
			Conv: 0, Q: 3, QuestionID: "conv-0-q-3", Correct: rep,
			Question: "q", Gold: "g", Predicted: "p",
			Notebook: &evalNotebookAttribution{GoldResolved: true, CandidateCovered: true, BundleCovered: rep, CandidateCount: 5, BundleEvidenceIDs: []string{"e0"}},
		})
		j.Close()
	}
	results, err := collectNotebookResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	nr := results[0]
	if nr.repCount != 3 {
		t.Fatalf("repCount = %d, want 3", nr.repCount)
	}
	// 2/3 correct → majority true.
	if !nr.item.Correct {
		t.Fatalf("majority should be correct (2/3)")
	}
	// bundle covered by any rep (rep0 & rep2).
	if !nr.attribution.BundleCovered {
		t.Fatalf("bundle_covered should merge across reps")
	}
	if nr.attribution.CandidateCount != 5 {
		t.Fatalf("CandidateCount = %d, want 5", nr.attribution.CandidateCount)
	}
	// evidence-ID audit trail must survive the rep merge (first non-empty wins).
	if len(nr.attribution.BundleEvidenceIDs) != 1 || nr.attribution.BundleEvidenceIDs[0] != "e0" {
		t.Fatalf("BundleEvidenceIDs = %v, want [e0]", nr.attribution.BundleEvidenceIDs)
	}
}

func TestWriteMistakeBookAndIndex(t *testing.T) {
	dir := t.TempDir()
	imported := time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC)
	recs := []notebookRecord{
		{
			RunID: "r1", RetrievalArm: "fts", QuestionID: "conv-0-q-1",
			CategoryName: "multi-hop", Question: "why?", Gold: "g", Predicted: "p",
			MajorityCorrect: false, MissClass: string(evalMissAnswerer),
			GoldResolved: true, CandidateCovered: true, BundleCovered: true,
			AnswerContextTok: 100, ImportedAt: imported.Format(time.RFC3339),
			ContextPreview: "context…",
		},
		{
			RunID: "r1", RetrievalArm: "fts", QuestionID: "conv-0-q-2",
			CategoryName: "temporal", Question: "when?", Gold: "g2", Predicted: "p2",
			MajorityCorrect: false, MissClass: string(evalMissCandidate),
			GoldResolved: true, CandidateCovered: false, BundleCovered: false,
		},
	}
	if err := writeMistakeBook(dir, "r1", "--retrieval fts", imported, recs); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "mistakes-r1.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(md)
	for _, want := range []string{"错题本", "answerer_miss", "candidate_miss", "multi-hop", "temporal", "conv-0-q-1", "解法笔记"} {
		if !strings.Contains(text, want) {
			t.Fatalf("mistake book missing %q:\n%s", want, text)
		}
	}

	// refreshNotebookIndex needs the accumulator.
	notebookJSONL := filepath.Join(dir, "notebook.jsonl")
	if _, err := appendNotebookRecords(notebookJSONL, recs); err != nil {
		t.Fatal(err)
	}
	if err := refreshNotebookIndex(dir); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"r1", "answerer_miss", "candidate_miss"} {
		if !strings.Contains(string(idx), want) {
			t.Fatalf("index missing %q:\n%s", want, string(idx))
		}
	}
}

// TestWriteNotebookEndToEnd drives the full writeNotebook pipeline: a synthetic
// two-rep run (one correct, one wrong with attribution) produces notebook.jsonl,
// mistakes-<run_id>.md and index.md with the expected miss class.
func TestWriteNotebookEndToEnd(t *testing.T) {
	runDir := t.TempDir()
	// 3-rep majority: run-1/run-3 correct, run-2 wrong (attribution says the
	// bundle dropped the gold on the wrong rep — compiler_miss per rep, but the
	// coalesced row is majority-correct → success).
	for i, sub := range []string{filepath.Join("run-1"), filepath.Join("run-2"), filepath.Join("run-3")} {
		dir := filepath.Join(runDir, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		j, err := openJournal(dir, "fts")
		if err != nil {
			t.Fatal(err)
		}
		wrong := i == 1
		j.write(result{
			Conv: 0, Q: 4, QuestionID: "conv-0-q-4", Category: 4, CategoryName: "multi-hop",
			Correct: !wrong, Question: "q", Gold: "g", Predicted: "p",
			AnswerContextTokens: 200,
			Notebook: &evalNotebookAttribution{
				GoldResolved: true, CandidateCovered: true, BundleCovered: !wrong,
				CandidateCount: 30, ContextPreview: "ctx…",
			},
		})
		j.Close()
	}

	opt := options{runDir: runDir, retrieval: "fts", topK: 30, chunkQuota: 12, chunks: true}
	notebookDir := t.TempDir()
	imported := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)
	sum, err := writeNotebook(context.Background(), opt, "e2e-1", imported, notebookDir, nil)
	if err != nil {
		t.Fatalf("writeNotebook: %v", err)
	}
	if sum.Total != 1 || sum.Correct != 1 {
		t.Fatalf("summary = %+v, want 1 question 1 correct", sum)
	}

	recs, err := loadNotebookRecords(filepath.Join(notebookDir, "notebook.jsonl"))
	if err != nil || len(recs) != 1 {
		t.Fatalf("accumulator has %d recs err=%v, want 1", len(recs), err)
	}
	if recs[0].MissClass != string(evalMissSuccess) {
		t.Fatalf("correct question should be success, got %q", recs[0].MissClass)
	}
	if recs[0].QuestionID != "conv-0-q-4" || recs[0].CandidateCount != 30 {
		t.Fatalf("record fields wrong: %+v", recs[0])
	}
	for _, name := range []string{"notebook.jsonl", "mistakes-e2e-1.md", "index.md"} {
		if _, err := os.Stat(filepath.Join(notebookDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	md, err := os.ReadFile(filepath.Join(notebookDir, "mistakes-e2e-1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "e2e-1") || !strings.Contains(string(md), "解法笔记") {
		t.Fatalf("mistake book missing headers:\n%s", string(md))
	}
}

func TestNotebookRunIDDistinct(t *testing.T) {
	a := notebookRunID("/tmp/runA")
	b := notebookRunID("/tmp/runB")
	if a == b {
		t.Fatalf("different run dirs must produce different ids: %q", a)
	}
	for _, id := range []string{a, b} {
		if !strings.Contains(id, "-runA") && !strings.Contains(id, "-runB") {
			t.Fatalf("id %q must embed the run dir basename", id)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("short", 10); got != "short" {
		t.Fatalf("short string must be unchanged, got %q", got)
	}
	got := truncateRunes(strings.Repeat("x", 100), 10)
	want := strings.Repeat("x", 10) + "…"
	if got != want {
		t.Fatalf("truncated = %q, want %q", got, want)
	}
}
