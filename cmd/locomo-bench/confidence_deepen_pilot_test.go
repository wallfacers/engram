package main

// T014: pure-logic tests for the deepen signal pilot — score orientation,
// best-signal tie-break, degenerate label split, channel-parity gate, and the
// report builder's chosen-signal/threshold wiring. No model calls.

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDeepenHesitationScoreOrientation(t *testing.T) {
	scores := deepenHesitationScores([]float64{-1.5, -2.5, 0.25}, true, false)
	want := [deepenPilotSignals]float64{1.5, 2.5, -0.25, 0}
	for i := range want {
		if scores[i] != want[i] {
			t.Fatalf("score[%d] = %v, want %v", i, scores[i], want[i])
		}
	}
	// Unavailable logprob signal collapses to zero, textual stays independent.
	scores = deepenHesitationScores(nil, false, true)
	if scores[0] != 0 || scores[1] != 0 || scores[2] != 0 || scores[3] != 1 {
		t.Fatalf("unavailable/textual scores = %v", scores)
	}
}

func mkAttempt(feat []float64, available bool, hesitant, correct, flip bool) deepenPilotAttempt {
	c := correct
	return deepenPilotAttempt{
		DecisionKey: "k", Features: feat, LogprobStatus: map[bool]string{true: "available", false: "unavailable"}[available],
		TextualHesitant: hesitant, JudgeCorrect: &c, Flip: flip,
	}
}

func TestDeepenPilotReportPerfectSeparationGO(t *testing.T) {
	var attempts []deepenPilotAttempt
	// 3 wrong: hesitant (high score); 3 right: confident (low score); no flips.
	for i := 0; i < 3; i++ {
		attempts = append(attempts, mkAttempt([]float64{-3.0, -3.0, -3.0}, true, true, false, false))
	}
	for i := 0; i < 3; i++ {
		attempts = append(attempts, mkAttempt([]float64{-0.1, -0.1, -0.1}, true, false, true, false))
	}
	report, err := deepenBuildPilotReport(attempts, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.Gate.Verdict != "GO" {
		t.Fatalf("verdict = %s (%s), want GO", report.Gate.Verdict, report.Gate.Reason)
	}
	if report.Chosen.Feature != utilityRoutingFeatureNames[0] {
		t.Fatalf("chosen feature = %s, want first logprob feature (tie-break order)", report.Chosen.Feature)
	}
	if report.Chosen.Threshold <= 0 {
		t.Fatalf("ROC threshold %v must sit between the classes (positive)", report.Chosen.Threshold)
	}
	if report.ChannelParity.FlipRate != 0 {
		t.Fatalf("flip rate %v, want 0", report.ChannelParity.FlipRate)
	}
	if len(report.Signals) != deepenPilotSignals {
		t.Fatalf("signals reported = %d, want %d", len(report.Signals), deepenPilotSignals)
	}
}

func TestDeepenPilotReportDegenerateLabelsNOGO(t *testing.T) {
	var attempts []deepenPilotAttempt
	for i := 0; i < 4; i++ {
		attempts = append(attempts, mkAttempt([]float64{-1.0, -1.0, -1.0}, true, false, true, false))
	}
	report, err := deepenBuildPilotReport(attempts, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.Gate.Verdict != "NO-GO" || report.Chosen.Feature != "" {
		t.Fatalf("all-correct labels must NO-GO with empty chosen, got %s/%s", report.Gate.Verdict, report.Chosen.Feature)
	}
}

func TestDeepenPilotReportFlipRateKillsGO(t *testing.T) {
	var attempts []deepenPilotAttempt
	for i := 0; i < 5; i++ {
		attempts = append(attempts, mkAttempt([]float64{-3.0, -3.0, -3.0}, true, true, false, false))
	}
	for i := 0; i < 5; i++ {
		// Perfect AUC but half the answers flip between channels.
		attempts = append(attempts, mkAttempt([]float64{-0.1, -0.1, -0.1}, true, false, true, i < 3))
	}
	report, err := deepenBuildPilotReport(attempts, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChannelParity.FlipRate <= deepenFlipRateNoise {
		t.Fatalf("flip rate %v must exceed noise band here", report.ChannelParity.FlipRate)
	}
	if report.Gate.Verdict != "NO-GO" {
		t.Fatalf("verdict = %s, want NO-GO despite AUC (channel parity gate)", report.Gate.Verdict)
	}
}

func TestDeepenPilotReportUnjudgedSkipped(t *testing.T) {
	attempts := []deepenPilotAttempt{
		mkAttempt([]float64{-3.0, -3.0, -3.0}, true, true, false, false),
		mkAttempt([]float64{-0.1, -0.1, -0.1}, true, false, true, false),
		{DecisionKey: "unjudged", Features: []float64{-99, -99, -99}, LogprobStatus: "available"}, // JudgeCorrect nil
	}
	report, err := deepenBuildPilotReport(attempts, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	// The unjudged outlier must not enter either class: AUC stays perfect.
	if report.Gate.Verdict != "GO" {
		t.Fatalf("verdict = %s (%s), want GO with unjudged skipped", report.Gate.Verdict, report.Gate.Reason)
	}
}

func TestDeepenPilotReportJSONShape(t *testing.T) {
	report := deepenPilotReport{
		Stage: "signal-pilot", Conversations: []string{"conv-1", "conv-2"},
		Signals: []deepenSignalReport{{Kind: "logprob", Feature: "final_mean_logprob", AUC: 0.7, AUCCI95: [2]float64{0.6, 0.8}, ParseCoverage: 1.0}},
		ChannelParity: deepenChannelParity{N: 10, Flips: 1, FlipRate: 0.1},
		Chosen:        deepenChosenSignal{Kind: "logprob", Feature: "final_mean_logprob", Threshold: -1.5},
		Gate:          deepenPilotGate{Rule: "auc>=0.65 AND flip_rate<=0.10", Verdict: "GO", Reason: "ok"},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"\"stage\"", "\"signals\"", "\"channel_parity\"", "\"chosen\"", "\"gate\"", "\"auc_ci95\"", "\"parse_coverage\""} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("report JSON missing %s: %s", key, raw)
		}
	}
}

func TestDeepenROCThresholdSitsBetweenClasses(t *testing.T) {
	pos := []float64{2.0, 3.0}
	neg := []float64{0.0, 0.5}
	th, err := deepenROCThreshold(pos, neg)
	if err != nil {
		t.Fatal(err)
	}
	if th <= 0.5 || th > 2.0 {
		t.Fatalf("threshold %v outside (max(neg), min(pos)]", th)
	}
	if math.IsNaN(th) {
		t.Fatal("NaN threshold")
	}
}

// --- deepenFinalSpanSignal: robust mapping over inline thinking + special end tokens ---

func mkTok(token string, logprob, top1, top2 float64) utilityLogprobToken {
	return utilityLogprobToken{Token: token, Bytes: []byte(token), Logprob: logprob, Top1: top1, Top2: top2}
}

func TestDeepenFinalSpanSignalSkipsThinkingAndSpecial(t *testing.T) {
	// Probe-derived shape (2026-08-15): inline "<think>...</think>" + trailing <|im_end|>.
	tokens := []utilityLogprobToken{
		mkTok("Here's ", -0.9, -0.9, -1.4),
		mkTok("thinking", -1.1, -1.1, -2.0),
		mkTok("</think>", -0.05, -0.05, -3.0),
		mkTok("\n\n4", -0.2, -0.2, -1.0),
		{Token: "<|im_end|>", Bytes: []byte("<|im_end|>"), Logprob: -0.01, Top1: -0.01, Top2: -0.02},
	}
	feats, available, reason := deepenFinalSpanSignal(tokens)
	if !available {
		t.Fatalf("unavailable: %s", reason)
	}
	// Only the final-span token counts: mean=-0.2, p10=-0.2, margin=0.8.
	want := []float64{-0.2, -0.2, 0.8}
	for i := range want {
		if math.Abs(feats[i]-want[i]) > 1e-9 {
			t.Fatalf("feature[%d] = %v, want %v (features %v)", i, feats[i], want[i], feats)
		}
	}
}

func TestDeepenFinalSpanSignalNoThinkingWholeText(t *testing.T) {
	tokens := []utilityLogprobToken{
		mkTok("Paris", -0.3, -0.3, -1.2),
		{Token: "<|im_end|>", Bytes: []byte("<|im_end|>"), Logprob: -0.01, Top1: -0.01, Top2: -0.5},
	}
	feats, available, _ := deepenFinalSpanSignal(tokens)
	if !available {
		t.Fatal("must be available without thinking delimiters")
	}
	if feats[0] != -0.3 {
		t.Fatalf("mean = %v, want -0.3", feats[0])
	}
}

func TestDeepenFinalSpanSignalEmptySpan(t *testing.T) {
	tokens := []utilityLogprobToken{
		mkTok("</think>", -0.05, -0.05, -3.0),
		{Token: "<|im_end|>", Bytes: []byte("<|im_end|>"), Logprob: -0.01, Top1: -0.01, Top2: -0.02},
	}
	_, available, reason := deepenFinalSpanSignal(tokens)
	if available || reason != "empty_final_span" {
		t.Fatalf("want empty_final_span, got available=%v reason=%s", available, reason)
	}
}

func TestDeepenFinalSpanSignalMissingTop2(t *testing.T) {
	tokens := []utilityLogprobToken{
		mkTok("</think>", -0.05, -0.05, -3.0),
		mkTok("4", -0.2, -0.2, 0.0),
	}
	_, available, reason := deepenFinalSpanSignal(tokens)
	if available || reason != "missing_top2" {
		t.Fatalf("want missing_top2, got available=%v reason=%s", available, reason)
	}
}

// Regression: on a trace WITHOUT special tokens and clean content, the robust
// mapper must agree with the 042 strict mapper (frozen feature semantics).
func TestDeepenFinalSpanSignalEquivalenceWith042(t *testing.T) {
	tokens := []utilityLogprobToken{
		mkTok("Caro", -0.4, -0.35, -1.9),
		mkTok("line", -0.6, -0.55, -2.1),
		mkTok(" won", -0.2, -0.18, -1.2),
	}
	strict, err := utilityMapFinalSignal("Caroline won", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if strict.Status != "available" {
		t.Fatalf("strict mapper unexpectedly unavailable: %s", strict.Reason)
	}
	robust, available, reason := deepenFinalSpanSignal(tokens)
	if !available {
		t.Fatalf("robust unavailable: %s", reason)
	}
	for i := range strict.Features {
		if math.Abs(strict.Features[i]-robust[i]) > 1e-12 {
			t.Fatalf("feature[%d] diverged: strict %v robust %v", i, strict.Features[i], robust[i])
		}
	}
}
