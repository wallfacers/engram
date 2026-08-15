package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestAbstainClaimMatchGradesSpanScopedClaims(t *testing.T) {
	tests := []struct {
		name  string
		claim SpanClaim
		want  ClaimMatch
	}{
		{
			name:  "entity absent",
			claim: SpanClaim{Entity: "Alex", Slot: "job", Value: "teacher"},
			want:  ClaimNoMatch,
		},
		{
			name:  "Caroline charity-race trap is entity only",
			claim: SpanClaim{Entity: "Caroline", Slot: "job", Value: "teacher"},
			want:  ClaimEntityOnly,
		},
		{
			name:  "entity and slot match",
			claim: SpanClaim{Entity: "Caroline", Slot: "charity-race-realization", Value: "persistence"},
			want:  ClaimEntityAndSlot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := computeAbstainSignalForDemand(abstainSignalInput{
				QuestionID: "conv-0-q-1",
				Category:   adversarialCategory,
				DemandAtoms: []DemandAtom{{
					Entity: "Caroline",
					Slot:   "charity-race-realization",
				}},
				Candidates: []memory.Result{{Name: "chunk-1", Score: 0.8}},
				Meta:       &PCICMeta{Spans: map[string]SpanClaim{"conv-0/D1:1": tt.claim}},
				ChunkTurns: map[string][]string{"chunk-1": {"D1:1"}},
				SpanKey:    func(turnID string) string { return "conv-0/" + turnID },
			})
			if signal.ClaimMatch != tt.want {
				t.Fatalf("claim match = %q, want %q", signal.ClaimMatch, tt.want)
			}
		})
	}
}

func TestAbstainConfidencePrefersRerankAndFallsBackToCosine(t *testing.T) {
	t.Run("rerank", func(t *testing.T) {
		signal := computeAbstainSignalForDemand(abstainSignalInput{
			Candidates: []memory.Result{{Name: "top", Score: 1.4}, {Name: "other", Score: 0.4}},
			Reranked:   true,
			CosineByCandidate: map[string]float64{
				"top": -1,
			},
		})
		if signal.ConfidenceSource != ConfidenceSourceRerank {
			t.Fatalf("confidence source = %q, want rerank", signal.ConfidenceSource)
		}
		if signal.Confidence < 0 || signal.Confidence > 1 {
			t.Fatalf("rerank confidence = %v, want normalized [0,1]", signal.Confidence)
		}
	})

	t.Run("cosine fallback", func(t *testing.T) {
		signal := computeAbstainSignalForDemand(abstainSignalInput{
			Candidates: []memory.Result{{Name: "top", Score: 0.01}},
			CosineByCandidate: map[string]float64{
				"top": -0.5,
			},
		})
		if signal.ConfidenceSource != ConfidenceSourceCosine {
			t.Fatalf("confidence source = %q, want cosine", signal.ConfidenceSource)
		}
		if signal.Confidence < 0 || signal.Confidence > 1 {
			t.Fatalf("cosine confidence = %v, want normalized [0,1]", signal.Confidence)
		}
	})
}

func TestAbstainConfidenceTop1UsesPeakWithoutMeanDilution(t *testing.T) {
	signal := computeAbstainSignalForDemand(abstainSignalInput{
		Candidates: []memory.Result{
			{Name: "top", Score: 0.9},
			{Name: "low1", Score: 0.1},
			{Name: "low2", Score: 0.1},
		},
		Reranked: true,
	})
	if math.Abs(signal.ConfidenceTop1-0.9) > 1e-9 {
		t.Fatalf("ConfidenceTop1 = %v, want peak score 0.9 (no mean dilution)", signal.ConfidenceTop1)
	}
	if signal.ConfidenceTop1 <= signal.Confidence {
		t.Fatalf("ConfidenceTop1 %v should exceed mean-blended Confidence %v", signal.ConfidenceTop1, signal.Confidence)
	}
}

func TestAbstainSignalDegradesWithoutPCICMeta(t *testing.T) {
	signal := computeAbstainSignalForDemand(abstainSignalInput{
		Candidates: []memory.Result{{Name: "top", Score: 0.9}},
		Reranked:   true,
	})
	if signal.ClaimSignalPresent {
		t.Fatal("claim signal present without pcic_meta")
	}
	if signal.Confidence <= 0 || signal.Confidence > 1 {
		t.Fatalf("confidence = %v, want usable normalized confidence", signal.Confidence)
	}
}

func TestAbstainROCSeparatesLabelsAndOrdersThresholds(t *testing.T) {
	separable := sweepSignalROC("confidence", []probeScore{
		{Adversarial: true, Score: 1},
		{Adversarial: true, Score: 0.8},
		{Adversarial: false, Score: 0.2},
		{Adversarial: false, Score: 0},
	}, defaultAbstainGate())
	if math.Abs(separable.AUC-1) > 1e-9 {
		t.Fatalf("separable AUC = %v, want 1", separable.AUC)
	}
	for i := 1; i < len(separable.Points); i++ {
		previous, current := separable.Points[i-1], separable.Points[i]
		if previous.Threshold < current.Threshold {
			t.Fatalf("thresholds must descend: %v then %v", previous.Threshold, current.Threshold)
		}
		if previous.AdversarialRecall > current.AdversarialRecall || previous.AnswerableFalseAbstain > current.AnswerableFalseAbstain {
			t.Fatalf("ROC rates regressed: %+v then %+v", previous, current)
		}
	}

	inseparable := sweepSignalROC("confidence", []probeScore{
		{Adversarial: true, Score: 1},
		{Adversarial: true, Score: 0},
		{Adversarial: false, Score: 1},
		{Adversarial: false, Score: 0},
	}, defaultAbstainGate())
	if math.Abs(inseparable.AUC-0.5) > 1e-9 {
		t.Fatalf("inseparable AUC = %v, want 0.5", inseparable.AUC)
	}
}

func TestAbstainGateVerdictRequiresAQualifyingSignalPoint(t *testing.T) {
	gate := defaultAbstainGate()
	passing := SignalROC{Signal: "confidence", Points: []ROCPoint{{
		Threshold:              0.7,
		AdversarialRecall:      gate.MinAdvRecall,
		AnswerableFalseAbstain: gate.MaxFalseAbstain,
		NetQuestions:           gate.MinNet,
	}}}
	if verdict, winning := evaluateProbeVerdict([]SignalROC{passing}, gate); verdict != "GO" || winning != "confidence" {
		t.Fatalf("passing verdict = %q/%q, want GO/confidence", verdict, winning)
	}

	failing := SignalROC{Signal: "claim", Points: []ROCPoint{{
		Threshold:              1,
		AdversarialRecall:      gate.MinAdvRecall,
		AnswerableFalseAbstain: gate.MaxFalseAbstain,
		NetQuestions:           gate.MinNet - 1,
	}}}
	if verdict, winning := evaluateProbeVerdict([]SignalROC{failing}, gate); verdict != "NO-GO" || winning != "" {
		t.Fatalf("failing verdict = %q/%q, want NO-GO/empty", verdict, winning)
	}
}

func TestAbstainProbeUsesNoAnswerOrJudgeLLM(t *testing.T) {
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
	failIfCalled := func(context.Context, string, string) (string, error) {
		t.Fatal("probe invoked an answer or judge caller")
		return "", nil
	}

	report, err := computeAbstainProbe(ctx, opt, []conversation{conv}, []*conversationRuntime{runtime}, arms, abstainProbeCallers{
		Answer: failIfCalled,
		Judge:  failIfCalled,
	}, slog.Default())
	if err != nil {
		t.Fatalf("compute abstain probe: %v", err)
	}
	if !report.ZeroLLM {
		t.Fatal("probe report must attest zero LLM calls")
	}
}

func TestAbstainProbeWritesOnlySecretFreeArtifact(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	outputDir := t.TempDir()
	opt := options{
		datasetFormat: "locomo",
		retrieval:     "fts",
		topK:          10,
		chunks:        true,
		storeDir:      storeDir,
	}
	conv, runtime, arms := newCoverageTestRuntime(t, opt)
	defer runtime.Close()
	before, err := runtime.entries.List(ctx)
	if err != nil {
		t.Fatalf("list entries before probe: %v", err)
	}
	const secret = "sk-abstain-probe-SECRET-should-never-appear"
	failIfCalled := func(context.Context, string, string) (string, error) {
		_ = secret
		t.Fatal("probe invoked an answer or judge caller")
		return "", nil
	}
	report, err := computeAbstainProbe(ctx, opt, []conversation{conv}, []*conversationRuntime{runtime}, arms, abstainProbeCallers{
		Answer: failIfCalled,
		Judge:  failIfCalled,
	}, slog.Default())
	if err != nil {
		t.Fatalf("compute abstain probe: %v", err)
	}
	after, err := runtime.entries.List(ctx)
	if err != nil {
		t.Fatalf("list entries after probe: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("engine entries changed from %d to %d", len(before), len(after))
	}
	path := filepath.Join(outputDir, "abstain-probe.json")
	if err := writeAbstainProbe(path, report); err != nil {
		t.Fatalf("write probe artifact: %v", err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read artifact directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "abstain-probe.json" {
		t.Fatalf("probe artifacts = %v, want only abstain-probe.json", entries)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read probe artifact: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("probe artifact leaked credential")
	}
}

func TestAbstainPopulationCountsCategoryFiveAsAdversarial(t *testing.T) {
	convs := []conversation{{ID: 0, QA: make([]locomoQA, 0, 1986)}}
	for i := 0; i < 446; i++ {
		convs[0].QA = append(convs[0].QA, locomoQA{Category: adversarialCategory})
	}
	for i := 0; i < 1540; i++ {
		convs[0].QA = append(convs[0].QA, locomoQA{Category: 4})
	}
	counts := abstainPopulationCounts(convs)
	if counts.Adversarial != 446 || counts.Answerable != 1540 || counts.Total != 1986 {
		t.Fatalf("counts = %+v, want adversarial=446 answerable=1540 total=1986", counts)
	}
}

func TestAbstainProbeFlagDefaultsAndGateOverride(t *testing.T) {
	gate, err := parseAbstainGate("advrecall=0.55,falseabstain=0.02,net=123")
	if err != nil {
		t.Fatalf("parse gate: %v", err)
	}
	if gate != (AbstainGate{MinAdvRecall: 0.55, MaxFalseAbstain: 0.02, MinNet: 123}) {
		t.Fatalf("gate = %+v", gate)
	}
	if got := abstainProbeOutputPath(options{storeDir: "/store", runDir: "/run"}); got != filepath.Join("/store", "abstain-probe.json") {
		t.Fatalf("store output path = %q", got)
	}
	if got := abstainProbeOutputPath(options{runDir: "/run"}); got != filepath.Join("/run", "abstain-probe.json") {
		t.Fatalf("run output path = %q", got)
	}
}

func TestAbstainProbeArtifactEnforcesFrozenContract(t *testing.T) {
	gate := defaultAbstainGate()
	point := ROCPoint{Threshold: 0.7, AdversarialRecall: 0.5, AnswerableFalseAbstain: 0.01, NetQuestions: 120}
	winner := "confidence"
	report := ProbeReport{
		Store:            "persisted-store",
		Counts:           abstainPopulation{Adversarial: 1, Answerable: 1, Total: 2},
		Signals:          []SignalROC{{Signal: "confidence", MeetsGate: true, BestPoint: point, Points: []ROCPoint{point}}},
		Gate:             gate,
		Verdict:          "GO",
		WinningSignal:    &winner,
		ConfidenceSource: ConfidenceSourceCosine,
		ZeroLLM:          true,
	}
	path := filepath.Join(t.TempDir(), "abstain-probe.json")
	if err := writeAbstainProbe(path, report); err != nil {
		t.Fatalf("write valid artifact: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var artifact map[string]json.RawMessage
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	for _, field := range []string{"store", "counts", "signals", "gate", "verdict", "winning_signal", "confidence_source", "zero_llm"} {
		if _, ok := artifact[field]; !ok {
			t.Fatalf("artifact missing %q: %s", field, raw)
		}
	}

	invalid := []struct {
		name   string
		mutate func(*ProbeReport)
	}{
		{"count mismatch", func(r *ProbeReport) { r.Counts.Total++ }},
		{"best point absent", func(r *ProbeReport) { r.Signals[0].BestPoint.Threshold = 0.1 }},
		{"verdict disagrees", func(r *ProbeReport) { r.Verdict = "NO-GO" }},
		{"zero llm false", func(r *ProbeReport) { r.ZeroLLM = false }},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			bad := report
			bad.Signals = append([]SignalROC(nil), report.Signals...)
			bad.Signals[0].Points = append([]ROCPoint(nil), report.Signals[0].Points...)
			tt.mutate(&bad)
			if err := writeAbstainProbe(filepath.Join(t.TempDir(), "bad.json"), bad); err == nil {
				t.Fatal("write accepted an artifact that violates the contract")
			}
		})
	}
}

