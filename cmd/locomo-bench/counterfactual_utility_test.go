package main

// T009 [US1] truth-table, three-repetition majority, exact run-1/2/3 discovery,
// duplicate/missing/identity/judge-regime rejection, historical-provenance
// warning, deterministic replay, and the 56/31/1453 fixture regression. Written
// first (failing) against the intended label-constructor API.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestUtilityTruthTable(t *testing.T) {
	cases := []struct {
		shallow, deep bool
		wantU         int
		wantLabel     utilityLabelKind
	}{
		{false, false, 0, utilityLabelNeutral}, // 错→错
		{true, true, 0, utilityLabelNeutral},   // 对→对
		{false, true, 1, utilityLabelBenefit},  // 错→对
		{true, false, -1, utilityLabelHarm},    // 对→错
	}
	for _, c := range cases {
		u, label, err := utilityTruthTable(c.shallow, c.deep)
		if err != nil {
			t.Fatalf("truth table %v/%v error: %v", c.shallow, c.deep, err)
		}
		if u != c.wantU || label != c.wantLabel {
			t.Fatalf("truth table %v/%v = (%d,%s), want (%d,%s)", c.shallow, c.deep, u, label, c.wantU, c.wantLabel)
		}
	}
}

func TestUtilityHistoricalRunDiscovery(t *testing.T) {
	root := t.TempDir()
	// Missing run dirs must fail closed.
	if _, err := utilityHistoricalRunDirs(root); err == nil {
		t.Fatal("empty root must fail closed")
	}
	// Correct run-1/2/3 layout passes.
	for i := 1; i <= 3; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("run-%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
		// One recognizable hybrid results JSONL per repetition.
		if err := utilityWriteFixtureResults(filepath.Join(root, fmt.Sprintf("run-%d", i), "results-hybrid.jsonl"),
			[]utilityHistoricalResult{{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: true}}); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := utilityHistoricalRunDirs(root)
	if err != nil {
		t.Fatalf("valid root rejected: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	// A fourth repetition dir makes the layout invalid.
	if err := os.MkdirAll(filepath.Join(root, "run-4"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := utilityHistoricalRunDirs(root); err == nil {
		t.Fatal("a fourth run dir must be rejected")
	}
	// Duplicate recognizable results JSONL in one run dir is rejected.
	if err := os.Remove(filepath.Join(root, "run-4")); err != nil {
		t.Fatal(err)
	}
	if err := utilityWriteFixtureResults(filepath.Join(root, "run-1", "results-fixed.jsonl"),
		[]utilityHistoricalResult{{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := utilityHistoricalRunDirs(root); err == nil {
		t.Fatal("duplicate results JSONL in one run dir must be rejected")
	}
}

func TestUtilityHistoricalPairingRejections(t *testing.T) {
	// Question sets must match between shallow and deep roots.
	shallow := []utilityHistoricalResult{
		{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: false},
		{QuestionID: "conv-0-q-1", Conv: 0, Q: 1, Correct: true},
	}
	deep := []utilityHistoricalResult{
		{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: true},
		{QuestionID: "conv-0-q-2", Conv: 0, Q: 2, Correct: false}, // missing q-1, extra q-2
	}
	if _, err := utilityPairRoots(shallow, deep); err == nil {
		t.Fatal("mismatched question sets must be rejected")
	}
	// Duplicate question IDs in one side are rejected.
	dup := []utilityHistoricalResult{
		{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: true},
		{QuestionID: "conv-0-q-0", Conv: 0, Q: 1, Correct: true},
	}
	if _, err := utilityPairRoots(dup, dup); err == nil {
		t.Fatal("duplicate question IDs must be rejected")
	}
	// Matching sets pair correctly.
	deepOK := []utilityHistoricalResult{
		{QuestionID: "conv-0-q-0", Conv: 0, Q: 0, Correct: true},
		{QuestionID: "conv-0-q-1", Conv: 0, Q: 1, Correct: true},
	}
	pairs, err := utilityPairRoots(shallow, deepOK)
	if err != nil {
		t.Fatalf("matching sets rejected: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(pairs))
	}
	// q-1 shallow true + deep true => NEUTRAL; q-0 shallow false + deep true => BENEFIT.
	if pairs["conv-0-q-0"].DeepCorrect != true || pairs["conv-0-q-0"].ShallowCorrect != false {
		t.Fatal("q-0 pairing wrong")
	}
}

func TestUtilityLabelDeterministicReplay(t *testing.T) {
	shallowRoot, deepRoot := utilityWriteHistoricalFixture(t, "conv-0-q-0", utilityFixtureQuestion{
		Shallow: []bool{false, false, false},
		Deep:    []bool{true, true, true},
	})
	labels1, summary1, err := utilityBuildHistoricalLabels(shallowRoot, deepRoot)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	labels2, summary2, err := utilityBuildHistoricalLabels(shallowRoot, deepRoot)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if summary1.Benefit != summary2.Benefit || summary1.Harm != summary2.Harm || summary1.Neutral != summary2.Neutral {
		t.Fatal("replay changed counts")
	}
	if len(labels1) != len(labels2) {
		t.Fatal("replay changed label count")
	}
	for i := range labels1 {
		if labels1[i].Label != labels2[i].Label || labels1[i].Utility != labels2[i].Utility {
			t.Fatalf("replay changed label %d: %+v vs %+v", i, labels1[i], labels2[i])
		}
	}
}

func TestUtilityThreeRepetitionMajority(t *testing.T) {
	// 2-of-3 correct => question correct; 1-of-3 => incorrect.
	for _, c := range []struct {
		reps []bool
		want bool
	}{
		{[]bool{true, true, true}, true},
		{[]bool{true, true, false}, true},
		{[]bool{true, false, false}, false},
		{[]bool{false, false, false}, false},
	} {
		got, err := majorityCorrectness(c.reps)
		if err != nil {
			t.Fatalf("majority %v: %v", c.reps, err)
		}
		if got != c.want {
			t.Fatalf("majority %v = %v, want %v", c.reps, got, c.want)
		}
	}
}

// utilityFixtureHistoricalRoots builds a three-repetition shallow/deep fixture
// shaped like the historical audit: 56 BENEFIT + 31 HARM + 1453 NEUTRAL.
func utilityFixtureHistoricalRoots(t *testing.T) (shallowRoot, deepRoot string) {
	t.Helper()
	root := t.TempDir()
	shallowRoot = filepath.Join(root, "shallow")
	deepRoot = filepath.Join(root, "deep")

	type qSpec struct {
		shallow, deep []bool
	}
	var questions []qSpec
	for i := 0; i < 56; i++ {
		questions = append(questions, qSpec{[]bool{false, false, false}, []bool{true, true, true}}) // BENEFIT
	}
	for i := 0; i < 31; i++ {
		questions = append(questions, qSpec{[]bool{true, true, true}, []bool{false, false, false}}) // HARM
	}
	for i := 0; i < 1000; i++ {
		questions = append(questions, qSpec{[]bool{true, true, true}, []bool{true, true, true}}) // NEUTRAL (对→对)
	}
	for i := 0; i < 453; i++ {
		questions = append(questions, qSpec{[]bool{false, false, false}, []bool{false, false, false}}) // NEUTRAL (错→错)
	}

	convCount := 10
	for qi, spec := range questions {
		qid := fmt.Sprintf("conv-%d-q-%d", qi%convCount, qi/convCount)
		conv := qi % convCount
		for rep := 0; rep < 3; rep++ {
			sh := utilityHistoricalResult{QuestionID: qid, Conv: conv, Q: qi / convCount, Correct: spec.shallow[rep]}
			de := utilityHistoricalResult{QuestionID: qid, Conv: conv, Q: qi / convCount, Correct: spec.deep[rep]}
			runSh := filepath.Join(shallowRoot, fmt.Sprintf("run-%d", rep+1))
			runDe := filepath.Join(deepRoot, fmt.Sprintf("run-%d", rep+1))
			if err := os.MkdirAll(runSh, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(runDe, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := utilityAppendFixtureResult(filepath.Join(runSh, "results-hybrid.jsonl"), sh); err != nil {
				t.Fatal(err)
			}
			if err := utilityAppendFixtureResult(filepath.Join(runDe, "results-hybrid.jsonl"), de); err != nil {
				t.Fatal(err)
			}
		}
	}
	return shallowRoot, deepRoot
}

func TestUtilityHistorical561311453Fixture(t *testing.T) {
	// A synthetic three-repetition fixture shaped like the historical audit
	// must reproduce exactly 56 BENEFIT / 31 HARM / 1453 NEUTRAL.
	shallowRoot, deepRoot := utilityFixtureHistoricalRoots(t)

	labels, summary, err := utilityBuildHistoricalLabels(shallowRoot, deepRoot)
	if err != nil {
		t.Fatalf("build historical labels: %v", err)
	}
	if len(labels) != 1540 {
		t.Fatalf("got %d labels, want 1540", len(labels))
	}
	if summary.Benefit != 56 || summary.Harm != 31 || summary.Neutral != 1453 {
		t.Fatalf("counts = B%d H%d N%d, want B56 H31 N1453", summary.Benefit, summary.Harm, summary.Neutral)
	}
}

// TestUtilityLabelStageEndToEnd runs the full zero-model label stage (manifest
// digest + seal + report) on the 56/31/1453 fixture. Regression for the
// manifest validate() gap where the label manifest omitted the frozen answerer
// identity and could never produce a GO receipt.
func TestUtilityLabelStageEndToEnd(t *testing.T) {
	shallowRoot, deepRoot := utilityFixtureHistoricalRoots(t)
	dir := t.TempDir()
	opt := &options{
		utilityShallowSource: shallowRoot,
		utilityDeepSource:    deepRoot,
		runDir:               dir,
		maxTokens:            8000,
	}
	if err := runUtilityLabelStage(opt); err != nil {
		t.Fatalf("label stage: %v", err)
	}
	// The report must be a sealed GO with the exact historical counts.
	raw, err := os.ReadFile(filepath.Join(dir, "label-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Verdict string         `json:"verdict"`
		Counts  map[string]int `json:"counts"`
		Claim   string         `json:"claim"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "GO" || report.Claim != "label_constructor_regression_only" {
		t.Fatalf("verdict/claim = %s/%s, want GO/label_constructor_regression_only", report.Verdict, report.Claim)
	}
	if report.Counts["BENEFIT"] != 56 || report.Counts["HARM"] != 31 || report.Counts["NEUTRAL"] != 1453 {
		t.Fatalf("counts = %v, want B56 H31 N1453", report.Counts)
	}
	// The manifest must validate (answerer identity frozen) and the seal must be
	// readable as a COMPLETE label GO.
	m, _, err := utilityManifestRead(dir)
	if err != nil {
		t.Fatalf("manifest read: %v", err)
	}
	if m.Answerer.MaxModelLen != utilityMaxModelLen || m.Answerer.TemperatureRequest != "omitted" {
		t.Fatalf("manifest answerer = maxlen %d temp %q, want %d/omitted", m.Answerer.MaxModelLen, m.Answerer.TemperatureRequest, utilityMaxModelLen)
	}
	if err := utilityValidateManifestSeal(dir, utilityStageLabel); err != nil {
		t.Fatalf("label seal invalid: %v", err)
	}
}

func TestUtilityLabelStageDoesNotReadProviderEnv(t *testing.T) {
	// The label stage must not consult provider/judge env (zero-model audit).
	// At minimum, building labels must not require LOCOMO_* / JUDGE_* vars,
	// which is guaranteed because the loader reads only the run roots.
	shallowRoot, deepRoot := utilityWriteHistoricalFixture(t, "conv-0-q-0", utilityFixtureQuestion{
		Shallow: []bool{false, false, false},
		Deep:    []bool{true, true, true},
	})
	labels, summary, err := utilityBuildHistoricalLabels(shallowRoot, deepRoot)
	if err != nil {
		t.Fatalf("label build must not require provider env: %v", err)
	}
	if len(labels) != 1 || summary.Benefit != 1 {
		t.Fatalf("unexpected label build result: labels=%d summary=%+v", len(labels), summary)
	}
}

// --- US1 label-constructor test helpers ---

type utilityFixtureQuestion struct {
	Shallow []bool
	Deep    []bool
}

// utilityWriteFixtureResults writes a strict results JSONL for one repetition.
func utilityWriteFixtureResults(path string, results []utilityHistoricalResult) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(path) //nolint:gosec // fixture path
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	for _, r := range results {
		rec := result{Conv: r.Conv, Q: r.Q, QuestionID: r.QuestionID, Correct: r.Correct}
		b, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return f.Sync()
}

func utilityAppendFixtureResult(path string, r utilityHistoricalResult) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	rec := result{Conv: r.Conv, Q: r.Q, QuestionID: r.QuestionID, Correct: r.Correct}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// utilityWriteHistoricalFixture writes a one-question fixture across both roots
// and returns (shallowRoot, deepRoot).
func utilityWriteHistoricalFixture(t *testing.T, qid string, q utilityFixtureQuestion) (string, string) {
	t.Helper()
	root := t.TempDir()
	shallowRoot := filepath.Join(root, "shallow")
	deepRoot := filepath.Join(root, "deep")
	for rep := 0; rep < 3; rep++ {
		sh := utilityHistoricalResult{QuestionID: qid, Conv: 0, Q: 0, Correct: q.Shallow[rep]}
		de := utilityHistoricalResult{QuestionID: qid, Conv: 0, Q: 0, Correct: q.Deep[rep]}
		if err := utilityWriteFixtureResults(filepath.Join(shallowRoot, fmt.Sprintf("run-%d", rep+1), "results-hybrid.jsonl"), []utilityHistoricalResult{sh}); err != nil {
			t.Fatal(err)
		}
		if err := utilityWriteFixtureResults(filepath.Join(deepRoot, fmt.Sprintf("run-%d", rep+1), "results-hybrid.jsonl"), []utilityHistoricalResult{de}); err != nil {
			t.Fatal(err)
		}
	}
	return shallowRoot, deepRoot
}
