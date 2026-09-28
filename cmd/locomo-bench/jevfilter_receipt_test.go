package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wallfacers/engram/store"
)

// This file pins the 051 four-arm run's native validity receipt: the arms path
// never produces the 022 evidence-compiler artifacts (candidates/trace/bundle/
// classification/formal_calls) because it does not run that pipeline, so its
// fail-closed gate must instead be backed by what the run itself measured —
// question coverage, per-arm row equality, row identity, the packer token cap,
// and answer+judge completeness. T19 fix for the receipts seam (main.go
// early-return before the only receipt writers).

// jevReceiptTestRows builds a fully measured fixture: every arm answers the
// same question set, every row ran answer+judge, every row fits the frozen cap.
func jevReceiptTestRows(questions, reps int) []jevArmQuestionRow {
	rows := make([]jevArmQuestionRow, 0, questions*reps*len(jevArmNames()))
	for _, arm := range jevArmNames() {
		for rep := 0; rep < reps; rep++ {
			for q := 1; q <= questions; q++ {
				rows = append(rows, jevArmQuestionRow{
					Conv: 1, Q: q, Block: jevArmMainBlock, Arm: arm, Repetition: rep,
					AnswerInputTokens: 500, CorrectMeasured: true,
				})
			}
		}
	}
	return rows
}

func TestJevRunValidityFromCompleteMeasurements(t *testing.T) {
	protocol := &evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}, Aggregation: evalAggregationProtocol{AnswerRepetitions: 1}}
	validity := jevRunValidityFromMeasurements(protocol, jevReceiptTestRows(2, 1), nil, "")
	if !validity.isComplete() {
		t.Fatalf("a fully measured run is not complete: %+v", validity)
	}
	if validity.QuestionsMeasured != 2 || validity.RowsMeasured != 10 || validity.RepetitionsMeasured != 1 {
		t.Errorf("counts = %+v, want questions=2 rows=10 reps=1", validity)
	}
	if validity.QuestionsExpected != 2 || validity.RowsExpected != 10 || validity.RepetitionsExpected != 1 {
		t.Errorf("expectations = %+v, want questions=2 rows=10 reps=1", validity)
	}
}

func TestJevRunValidityRefusesIncompleteMeasurements(t *testing.T) {
	protocol := &evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}, Aggregation: evalAggregationProtocol{AnswerRepetitions: 1}}
	full := jevReceiptTestRows(2, 1)

	missingArm := make([]jevArmQuestionRow, 0, len(full)-2)
	for _, row := range full {
		if row.Arm != jevArmD {
			missingArm = append(missingArm, row)
		}
	}
	missingQuestion := full[:len(full)-1]
	duplicate := append(append([]jevArmQuestionRow(nil), full...), full[0])
	overCap := append([]jevArmQuestionRow(nil), full...)
	overCap[0].AnswerInputTokens = jevArmAnswerInputCap + 1
	unanswered := append([]jevArmQuestionRow(nil), full...)
	unanswered[0].CorrectMeasured = false
	declaredInvalid := append([]jevArmQuestionRow(nil), full...)

	protocolTwoReps := &evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}, Aggregation: evalAggregationProtocol{AnswerRepetitions: 3}}

	cases := []struct {
		name     string
		protocol *evalProtocol
		rows     []jevArmQuestionRow
		invalid  string
	}{
		{"missing arm", protocol, missingArm, ""},
		{"missing question", protocol, missingQuestion, ""},
		{"duplicate row", protocol, duplicate, ""},
		{"token cap breach", protocol, overCap, ""},
		{"unanswered row", protocol, unanswered, ""},
		{"declared invalid", protocol, declaredInvalid, "arm B hit the frozen cap"},
		{"repetitions not measured", protocolTwoReps, full, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if validity := jevRunValidityFromMeasurements(tc.protocol, tc.rows, nil, tc.invalid); validity.isComplete() {
				t.Fatalf("incomplete run passed the gate: %+v", validity)
			}
		})
	}
}

// TestWriteJevArmArtifactsLandsOwnReceipt pins the T19 receipts seam fix: in a
// run dir holding only what earlier stages genuinely wrote (the frozen protocol
// and the filter call journal), writeJevArmArtifacts itself materializes the
// rows file and the summary receipt, and a fully measured run passes its own
// validity gate — the pilot's rc=1 "receipts are incomplete" failure mode is
// gone without any pre-seeded summary.json.
func TestWriteJevArmArtifactsLandsOwnReceipt(t *testing.T) {
	dir := t.TempDir()
	for _, artifact := range []string{evalProtocolArtifactFile, jevFilterCallJournalFile} {
		if err := os.WriteFile(filepath.Join(dir, artifact), []byte("{}\n"), 0o644); err != nil { //nolint:gosec
			t.Fatal(err)
		}
	}
	protocol := &evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}, Aggregation: evalAggregationProtocol{AnswerRepetitions: 1}}
	opt := options{runDir: dir}
	if err := writeJevArmArtifacts(opt, jevTestRegistration(), protocol, jevReceiptTestRows(2, 1), nil, 4, 10, nil, false, ""); err != nil {
		t.Fatalf("write artifacts: %v", err)
	}
	for _, name := range []string{jevArmRowsFile, jevArmReportFile, jevArmVerdictFile, evalSummaryArtifactFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
	var verdict jevArmVerdict
	if err := readJSON(filepath.Join(dir, jevArmVerdictFile), &verdict); err != nil {
		t.Fatalf("read verdict: %v", err)
	}
	if verdict.ValidityError != "" {
		t.Errorf("a fully measured run still failed its own gate: %s", verdict.ValidityError)
	}

	// Removing an artifact the run does not itself write must fail the run
	// closed — the receipt is derived from the measurements, but the filter call
	// journal it certifies is a precondition, not a decoration.
	if err := os.Remove(filepath.Join(dir, jevFilterCallJournalFile)); err != nil {
		t.Fatal(err)
	}
	if err := writeJevArmArtifacts(opt, jevTestRegistration(), protocol, jevReceiptTestRows(2, 1), nil, 4, 10, nil, false, ""); err == nil {
		t.Error("a run missing its filter call journal exited zero")
	}
}

// jevCountingEmbedClient counts Embed calls so a wiring test can prove the
// semantic signal actually reached the retriever (T19 BUG ①: the arms path
// passed a literal nil and the engine silently disabled semantic retrieval).
type jevCountingEmbedClient struct{ calls int }

func (c *jevCountingEmbedClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.calls++
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, 8)
	}
	return vectors, nil
}

func (c *jevCountingEmbedClient) Model() string { return "jev-test-embed" }

func TestValidateJevEmbeddingWiring(t *testing.T) {
	if err := validateJevEmbeddingWiring("hybrid", nil); err == nil {
		t.Error("a hybrid arm with no embedding client was accepted; the engine would silently drop the semantic signal")
	}
	if err := validateJevEmbeddingWiring("hybrid", &jevCountingEmbedClient{}); err != nil {
		t.Errorf("a hybrid arm with a live client was refused: %v", err)
	}
	if err := validateJevEmbeddingWiring("bm25", nil); err != nil {
		t.Errorf("a non-hybrid arm does not need an embedding client: %v", err)
	}
}

// TestJevArmsRuntimeKeepsSemanticSignal proves the fix end to end at the seam
// where the pilot lost it: openAttributionRuntime must hand the arms' retriever
// the live embedding client, so a search embeds the query (the engine embeds the
// query before consulting stored vectors, so even an empty store proves wiring).
func TestJevArmsRuntimeKeepsSemanticSignal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv1.db")
	st, err := store.Open(context.Background(), store.Options{DSN: path})
	if err != nil {
		t.Fatalf("seed persisted store: %v", err)
	}
	st.Close()
	client := &jevCountingEmbedClient{}
	runtime, err := openAttributionRuntime(context.Background(), options{storeDir: dir}, conversation{ID: 1}, client, "hybrid")
	if err != nil {
		t.Fatalf("open arms runtime: %v", err)
	}
	defer runtime.Close()
	retriever := runtime.retrievers["hybrid"]
	if retriever == nil {
		t.Fatal("the hybrid retriever is missing from the arms runtime")
	}
	if _, err := retriever.Search(context.Background(), "any question", 5); err != nil {
		t.Fatalf("search: %v", err)
	}
	if client.calls == 0 {
		t.Error("the query was never embedded; the semantic signal is dead in the arms runtime (T19 BUG ①)")
	}
}

// --- --jev-answer-arms subset (maintainer order 2026-09-28: B/D-only answering) ---

func TestParseJevAnswerArms(t *testing.T) {
	if arms, err := parseJevAnswerArms(""); err != nil || arms != nil {
		t.Errorf("empty = %v, %v; want nil, nil (all arms answer)", arms, err)
	}
	arms, err := parseJevAnswerArms("B,D")
	if err != nil || len(arms) != 2 || arms[0] != jevArmB || arms[1] != jevArmD {
		t.Errorf("B,D = %v, %v", arms, err)
	}
	for _, raw := range []string{"B,X", "B,B", "B,", "d"} {
		if _, err := parseJevAnswerArms(raw); err == nil {
			t.Errorf("subset %q was accepted", raw)
		}
	}
}

func TestJevArmAnswered(t *testing.T) {
	if !jevArmAnswered(nil, jevArmA) || !jevArmAnswered(nil, jevArmDNoRelax) {
		t.Error("a nil subset must mean every arm answers")
	}
	subset := []jevArm{jevArmB, jevArmD}
	if !jevArmAnswered(subset, jevArmB) || !jevArmAnswered(subset, jevArmD) {
		t.Error("B and D must answer under the B,D subset")
	}
	if jevArmAnswered(subset, jevArmA) || jevArmAnswered(subset, jevArmC) || jevArmAnswered(subset, jevArmDNoRelax) {
		t.Error("A/C/D-noRelax must not answer under the B,D subset")
	}
}

// TestJevRunValidityComplianceOverAnswerArms pins the receipt's answer-compliance
// semantics under a B/D-only answering subset: the denominator is the subset's
// rows, so a fully answered subset on a five-arm measurement is complete while
// the same rows under all-arms answering are not.
func TestJevRunValidityComplianceOverAnswerArms(t *testing.T) {
	protocol := &evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}, Aggregation: evalAggregationProtocol{AnswerRepetitions: 1}}
	rows := jevReceiptTestRows(2, 1) // all rows CorrectMeasured=true
	answerArms := []jevArm{jevArmB, jevArmD}
	for i := range rows {
		if !answerArmsContains(answerArms, rows[i].Arm) {
			rows[i].CorrectMeasured = false
		}
	}
	if validity := jevRunValidityFromMeasurements(protocol, rows, answerArms, ""); !validity.isComplete() {
		t.Fatalf("a fully answered B/D subset is not complete: %+v", validity)
	}
	if validity := jevRunValidityFromMeasurements(protocol, rows, nil, ""); validity.isComplete() {
		t.Fatal("unanswered arms under the all-arms protocol passed the gate")
	}
}

func answerArmsContains(arms []jevArm, arm jevArm) bool {
	for _, candidate := range arms {
		if candidate == arm {
			return true
		}
	}
	return false
}
