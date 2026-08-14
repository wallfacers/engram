package main

// T018 [P] [US2] collect/preflight + T020 [US2] diagnose tests. These exercise
// the offline-testable protocol core with fixture artifacts and stubbed
// model seams: label derivation, coverage, journal/cost accounting, LOCO fold
// building, leakage validation, decisions-before-labels, and the strict
// diagnostic gates (net +25, precision frontier 56c-31h>=25, quality/accuracy/
// token/category/Holm) with GO/NO-GO/INVALID precedence.

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// --- collect: label derivation and coverage ---

func TestUtilityCollectLabelDerivationAndCoverage(t *testing.T) {
	units := []utilityCollectUnit{
		{Key: utilityTestDecisionKey(0, "q0", 1), SignalAvailable: true, Features: []float64{-0.1, -0.2, 1.0}, ShallowCorrect: false, DeepCorrect: true},
		{Key: utilityTestDecisionKey(0, "q0", 2), SignalAvailable: true, Features: []float64{-0.1, -0.2, 1.0}, ShallowCorrect: true, DeepCorrect: true},
		{Key: utilityTestDecisionKey(0, "q0", 3), SignalAvailable: true, Features: []float64{-0.1, -0.2, 1.0}, ShallowCorrect: false, DeepCorrect: true},
		{Key: utilityTestDecisionKey(0, "q1", 1), SignalAvailable: false, ShallowCorrect: true, DeepCorrect: false},
		{Key: utilityTestDecisionKey(0, "q1", 2), SignalAvailable: false, ShallowCorrect: true, DeepCorrect: false},
		{Key: utilityTestDecisionKey(0, "q1", 3), SignalAvailable: false, ShallowCorrect: true, DeepCorrect: false},
	}
	dir := t.TempDir()
	if err := utilityWriteTestCollect(dir, units, []int{0}); err != nil {
		t.Fatalf("write collect: %v", err)
	}
	data, err := utilityLoadCollect(dir)
	if err != nil {
		t.Fatalf("load collect: %v", err)
	}
	if len(data.Units) != 6 {
		t.Fatalf("units = %d, want 6", len(data.Units))
	}
	// q0 repetition-1 utility: F→T = +1 BENEFIT; q1: T→F = -1 HARM.
	if data.Units[0].UtilityFromCorrectness() != 1 || data.Units[0].Label != utilityLabelBenefit {
		t.Fatalf("q0r1 = utility %d label %s, want +1 BENEFIT", data.Units[0].UtilityFromCorrectness(), data.Units[0].Label)
	}
	if data.Units[3].UtilityFromCorrectness() != -1 || data.Units[3].Label != utilityLabelHarm {
		t.Fatalf("q1r1 = utility %d label %s, want -1 HARM", data.Units[3].UtilityFromCorrectness(), data.Units[3].Label)
	}
	// Signal coverage counted.
	if data.SignalAvailable != 3 || data.SignalUnavailable != 3 {
		t.Fatalf("signal avail/unavail = %d/%d, want 3/3", data.SignalAvailable, data.SignalUnavailable)
	}
}

func TestUtilityCollectSealAndManifestValidation(t *testing.T) {
	units := []utilityCollectUnit{
		{Key: utilityTestDecisionKey(0, "q0", 1), SignalAvailable: true, Features: []float64{-0.1, -0.2, 1.0}, ShallowCorrect: false, DeepCorrect: true},
	}
	dir := t.TempDir()
	if err := utilityWriteTestCollect(dir, units, []int{0}); err != nil {
		t.Fatal(err)
	}
	// A valid sealed collect loads.
	if _, err := utilityLoadCollect(dir); err != nil {
		t.Fatalf("valid collect rejected: %v", err)
	}
	// Tampering with a label file invalidates the collect.
	labelsPath := filepath.Join(dir, utilityHiddenLabelsFile)
	raw, err := os.ReadFile(labelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelsPath, append(raw[:len(raw)-2], []byte("}]}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := utilityLoadCollect(dir); err == nil {
		t.Fatal("tampered collect must be rejected")
	}
}

// --- diagnose: folds, leakage, decisions, gates ---

// TestUtilityDiagnoseGates builds a synthetic collect where a feature perfectly
// separates BENEFIT (high score) from HARM/NEUTRAL, so the ridge rule should
// capture benefits without triggering harms, passing the +25 / precision /
// quality gates. Cost proportion (shallow ~4800, deep ~28800) keeps the 60%
// token gate feasible at ~42% deepen rate.
func TestUtilityDiagnoseGates(t *testing.T) {
	// 6 conversations x 10 questions x 3 reps: 25 BENEFIT / 5 HARM / 30 NEUTRAL.
	var units []utilityCollectUnit
	const convs = 6
	benefitPerConv := []int{4, 4, 4, 4, 4, 5}
	for c := 0; c < convs; c++ {
		qi := 0
		for b := 0; b < benefitPerConv[c]; b++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityCollectUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), SignalAvailable: true,
					Features: []float64{2.0, -0.1, 0.5}, ShallowCorrect: false, DeepCorrect: true,
				})
			}
			qi++
		}
		// Remaining questions: 5 HARM + NEUTRAL to reach 10 per conv.
		harmPerConv := 1
		if c == 5 {
			harmPerConv = 0
		}
		for h := 0; h < harmPerConv; h++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityCollectUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), SignalAvailable: true,
					Features: []float64{-1.0, -0.3, 0.2}, ShallowCorrect: true, DeepCorrect: false,
				})
			}
			qi++
		}
		for ; qi < 10; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityCollectUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), SignalAvailable: true,
					Features: []float64{-1.0, -0.3, 0.2}, ShallowCorrect: true, DeepCorrect: true,
				})
			}
		}
	}
	dir := t.TempDir()
	convIDs := []int{0, 1, 2, 3, 4, 5}
	if err := utilityWriteTestCollect(dir, units, convIDs); err != nil {
		t.Fatal(err)
	}
	report, err := utilityDiagnose(dir, t.TempDir())
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if report.Verdict != "GO" {
		// Dump the failing gates for diagnosis.
		t.Fatalf("expected GO, got %s; gates=%+v quality=%v utility=%v precision=%v",
			report.Verdict, report.Gates, report.Quality, report.Utility, report.Precision)
	}
	if report.Precision == nil || !report.Precision.Passed {
		t.Fatalf("precision frontier must pass: %+v", report.Precision)
	}
}

func TestUtilityDiagnoseLeakageInvalid(t *testing.T) {
	// A fold set whose held-out conversations overlap or miss the union must be
	// INVALID, never a silently smaller denominator.
	units := []utilityCollectUnit{
		{Key: utilityTestDecisionKey(0, "q0", 1), SignalAvailable: true, Features: []float64{0, 0, 0}, ShallowCorrect: false, DeepCorrect: true},
		{Key: utilityTestDecisionKey(1, "q0", 1), SignalAvailable: true, Features: []float64{0, 0, 0}, ShallowCorrect: false, DeepCorrect: true},
	}
	dir := t.TempDir()
	if err := utilityWriteTestCollect(dir, units, []int{0, 1}); err != nil {
		t.Fatal(err)
	}
	// Conversation 2 exists in the manifest but has no units: coverage invalid.
	data, err := utilityLoadCollect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := utilityValidateFoldCoverage(data, []int{0, 1, 2}); err == nil {
		t.Fatal("coverage with an empty conversation must be invalid")
	}
}

func TestUtilityDiagnosePrecisionFrontierMath(t *testing.T) {
	// 56c - 31h >= 25 with c=0.70 => h <= (56*0.7-25)/31 = 0.458...
	cap := utilityHarmCap(0.70)
	if math.Abs(cap-0.458) > 0.005 {
		t.Fatalf("harm cap at c=0.70 = %v, want ~0.458 (<=0.46)", cap)
	}
	// c=1.0 => h <= (56-25)/31 = 1.0 exactly (all benefit, harm-free bound loose).
	cap1 := utilityHarmCap(1.0)
	if math.Abs(cap1-1.0) > 1e-9 {
		t.Fatalf("harm cap at c=1.0 = %v, want 1.0", cap1)
	}
	// The frontier is an explicit harm cap, not a net-only check: a router that
	// triggers nearly every HARM fails even at high capture.
	if !utilityPrecisionPasses(0.95, 0.80) {
		t.Fatal("c=0.95 h=0.80 passes (56*.95-31*.80=28.4>=25)")
	}
	if utilityPrecisionPasses(0.95, 0.95) {
		t.Fatal("c=0.95 h=0.95 must fail (53.2-29.45=23.75<25)")
	}
	if utilityPrecisionPasses(0.70, 0.60) {
		t.Fatal("c=0.70 h=0.60 must fail (39.2-18.6=20.6<25)")
	}
	if !utilityPrecisionPasses(0.70, 0.40) {
		t.Fatal("c=0.70 h=0.40 must pass (56*.70-31*.40=27.2>=25)")
	}
}

func TestUtilityDiagnoseNoGoOnNetFloor(t *testing.T) {
	// A signal with zero discriminating power must NOT pass the +25 net gate.
	// All questions are NEUTRAL (shallow==deep), so any keep-shallow policy nets
	// 0 < 25 => NO-GO (not INVALID).
	var units []utilityCollectUnit
	for c := 0; c < 3; c++ {
		for qi := 0; qi < 4; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityCollectUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), SignalAvailable: true,
					Features: []float64{0.0, 0.0, 0.0}, ShallowCorrect: true, DeepCorrect: true,
				})
			}
		}
	}
	dir := t.TempDir()
	if err := utilityWriteTestCollect(dir, units, []int{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	report, err := utilityDiagnose(dir, t.TempDir())
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if report.Verdict != "NO-GO" {
		t.Fatalf("zero-signal fixture must be NO-GO, got %s", report.Verdict)
	}
}

// --- helpers ---

func convQID(conv, qi int) string {
	return "conv-" + strconv.Itoa(conv) + "-q-" + strconv.Itoa(qi)
}

// utilityWriteTestCollect writes a sealed collect fixture with the given units.
func utilityWriteTestCollect(dir string, units []utilityCollectUnit, convIDs []int) error {
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "hidden"), 0o755); err != nil {
		return err
	}
	m := utilityTestManifest(utilityStageCollect)
	m.Benchmark.ConversationIDs = convIDs
	// Declare the label constructor-audit GO receipt in the collect source chain.
	m.Source = []utilitySourceRef{{Stage: "label", ManifestDigest: utilityEndpointDigest("label-m"), SealDigest: utilityEndpointDigest("label-s"), ReportDigest: utilityEndpointDigest("label-r")}}
	md, err := utilityManifestDigest(&m)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, utilityManifestFile), m); err != nil {
		return err
	}
	var attempts []any
	var labels []any
	for i := range units {
		u := &units[i]
		u.Label, _ = utilityLabelFromUtility(u.UtilityFromCorrectness())
		shID := utilityEndpointDigest("sh-" + u.Key.QuestionID + "-" + strconv.Itoa(u.Key.Repetition))
		deID := utilityEndpointDigest("de-" + u.Key.QuestionID + "-" + strconv.Itoa(u.Key.Repetition))
		attempts = append(attempts, utilityAnswerAttempt{
			AnswerAttemptID: shID, DecisionKey: u.Key, Arm: utilityArmShallow, FinalAnswer: "sh",
			Usage: utilityAnswerUsage{InputTokens: 4000, OutputTokens: 800}, LatencyMS: 3,
			Signal: &utilityProbabilitySignal{Status: "available", Features: u.Features, FinalTrace: []utilityTokenTraceEntry{{ByteLen: 1, SampledLogprob: -0.1, Top1Logprob: -0.1, Top2Logprob: -2.0}}},
		})
		attempts = append(attempts, utilityAnswerAttempt{
			AnswerAttemptID: deID, DecisionKey: u.Key, Arm: utilityArmPairedDeep, FinalAnswer: "de",
			Usage: utilityAnswerUsage{InputTokens: 28000, OutputTokens: 800}, LatencyMS: 5,
		})
		labels = append(labels, utilityUtilityLabel{
			DecisionKey: u.Key, ShallowAnswerDigest: shID, DeepAnswerDigest: deID,
			ShallowCorrect: u.ShallowCorrect, DeepCorrect: u.DeepCorrect,
			Utility: u.UtilityFromCorrectness(), Label: u.Label,
			SourceManifestDigest: md,
		})
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityPublicAnswerAttemptsFile), attempts); err != nil {
		return err
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityHiddenJudgeFile), []any{}); err != nil {
		return err
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityHiddenLabelsFile), labels); err != nil {
		return err
	}
	report := map[string]any{"schema": utilitySchemaVersion, "verdict": "GO", "claim": "fixture"}
	reportDigest, err := utilityCanonicalDigest(report)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, utilityCollectReportFile), report); err != nil {
		return err
	}
	seal := utilityStageSeal{
		Schema: utilitySchemaVersion, Stage: utilityStageCollect, Status: utilitySealComplete,
		ManifestDigest: md, Verdict: "GO", ReportDigest: reportDigest,
	}
	return utilitySealWrite(dir, seal)
}

// --- US2 pilot gate tests (T014) ---

func utilityPilotUnit(conv, qi, rep int, feats []float64, sh, deep bool) utilityCollectUnit {
	u := utilityCollectUnit{
		Key:            utilityTestDecisionKey(conv, convQID(conv, qi), rep),
		ShallowCorrect: sh, DeepCorrect: deep,
	}
	if feats != nil {
		u.Features = feats
		u.SignalAvailable = true
	}
	l, _ := utilityLabelFromUtility(u.UtilityFromCorrectness())
	u.Label = l
	return u
}

func TestUtilityPilotGateGO(t *testing.T) {
	// BENEFIT scores high, HARM/NEUTRAL score low: in-sample AUC = 1.0 -> GO.
	var units []utilityCollectUnit
	for c := 0; c < 2; c++ {
		for qi := 0; qi < 2; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityPilotUnit(c, qi, r, []float64{2.0, -0.1, 0.5}, false, true)) // BENEFIT
			}
		}
		for qi := 2; qi < 3; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityPilotUnit(c, qi, r, []float64{-2.0, -0.3, 0.2}, true, false)) // HARM
			}
		}
		for qi := 3; qi < 5; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityPilotUnit(c, qi, r, []float64{-2.0, -0.3, 0.2}, true, true)) // NEUTRAL
			}
		}
	}
	receipt, err := utilityPilotGate(units)
	if err != nil {
		t.Fatalf("pilot gate: %v", err)
	}
	if receipt.Verdict != "GO" {
		t.Fatalf("pilot verdict = %s, want GO (auc=%v)", receipt.Verdict, receipt.AUC)
	}
	if !receipt.GatePassed || receipt.Claim != "signal_existence_pilot_only" {
		t.Fatalf("pilot receipt incomplete: %+v", receipt)
	}
	if len(receipt.ConversationIDs) != 2 {
		t.Fatalf("pilot must cover exactly two conversations, got %v", receipt.ConversationIDs)
	}
	if receipt.AUC == nil || *receipt.AUC < 0.99 {
		t.Fatalf("perfect-separation AUC = %v, want ~1.0", receipt.AUC)
	}
}

func TestUtilityPilotGateNoSignal(t *testing.T) {
	// Random/no-signal scores give AUC ~0.5 -> NO-GO (both classes present).
	var units []utilityCollectUnit
	for c := 0; c < 2; c++ {
		for qi := 0; qi < 3; qi++ {
			for r := 1; r <= 3; r++ {
				// All units share identical features: the ridge can't separate.
				units = append(units, utilityPilotUnit(c, qi, r, []float64{0.0, 0.0, 0.0}, qi%2 == 0, qi%2 != 0))
			}
		}
	}
	receipt, err := utilityPilotGate(units)
	if err != nil {
		t.Fatalf("pilot gate: %v", err)
	}
	if receipt.Verdict != "NO-GO" {
		t.Fatalf("zero-signal pilot must be NO-GO, got %s (auc=%v)", receipt.Verdict, receipt.AUC)
	}
}

func TestUtilityPilotGateClassMissing(t *testing.T) {
	// No BENEFIT in the pilot corpus: AUC undefined -> NO-GO with reason.
	var units []utilityCollectUnit
	for c := 0; c < 2; c++ {
		for qi := 0; qi < 3; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityPilotUnit(c, qi, r, []float64{0.0, 0.0, 0.0}, true, true)) // all NEUTRAL
			}
		}
	}
	receipt, err := utilityPilotGate(units)
	if err != nil {
		t.Fatalf("pilot gate: %v", err)
	}
	if receipt.Verdict != "NO-GO" || receipt.AUCNullReason != "pilot_class_missing" {
		t.Fatalf("class-missing pilot must be NO-GO(pilot_class_missing), got verdict=%s reason=%s", receipt.Verdict, receipt.AUCNullReason)
	}
}

// --- US3 confirm/transfer gate tests (T029/T031) ---

func TestUtilityConfirmGatesGO(t *testing.T) {
	// A policy that keeps shallow correct answers but deepens only the
	// shallow-wrong/deep-right BENEFITs passes quality/accuracy/token/precision.
	var units []utilityConfirmUnit
	// 40 questions x 3 reps. 10 BENEFIT (shallow wrong, deep right, deepened),
	// 8 HARM (kept shallow), 22 NEUTRAL (kept).
	for c := 0; c < 4; c++ {
		qi := 0
		for b := 0; b < 3; b++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityConfirmUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), Action: utilityActionDeepen,
					PolicyCorrect: true, ShallowCorrect: false, DeepCorrect: true,
					ShallowTokens: 4000, DeepTokens: 28000,
				})
			}
			qi++
		}
		for h := 0; h < 2; h++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityConfirmUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), Action: utilityActionKeepShallow,
					PolicyCorrect: true, ShallowCorrect: true, DeepCorrect: false,
					ShallowTokens: 4000, DeepTokens: 28000,
				})
			}
			qi++
		}
		for ; qi < 10; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityConfirmUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), Action: utilityActionKeepShallow,
					PolicyCorrect: true, ShallowCorrect: true, DeepCorrect: true,
					ShallowTokens: 4000, DeepTokens: 28000,
				})
			}
		}
	}
	report, err := utilityConfirmGates(units)
	if err != nil {
		t.Fatalf("confirm gates: %v", err)
	}
	if report.Verdict != "GO" {
		t.Fatalf("expected GO, got %s; gates=%+v quality=%v cost=%v precision=%v", report.Verdict, report.Gates, report.Quality, report.Cost, report.Precision)
	}
}

func TestUtilityConfirmGatesRegression(t *testing.T) {
	// A policy that deepens nothing keeps shallow answers; when deep control
	// outperforms shallow (benefits exist), the policy regresses -> NO-GO.
	var units []utilityConfirmUnit
	for c := 0; c < 2; c++ {
		for qi := 0; qi < 5; qi++ {
			for r := 1; r <= 3; r++ {
				units = append(units, utilityConfirmUnit{
					Key: utilityTestDecisionKey(c, convQID(c, qi), r), Action: utilityActionKeepShallow,
					PolicyCorrect: false, ShallowCorrect: false, DeepCorrect: true, // benefit missed
					ShallowTokens: 4000, DeepTokens: 28000,
				})
			}
		}
	}
	report, err := utilityConfirmGates(units)
	if err != nil {
		t.Fatalf("confirm gates: %v", err)
	}
	if report.Verdict != "NO-GO" {
		t.Fatalf("missed-benefits policy must be NO-GO, got %s", report.Verdict)
	}
}

func TestUtilityTransferNonRegression(t *testing.T) {
	goReceipt, err := utilityTransferNonRegression(120, 115, 150)
	if err != nil {
		t.Fatal(err)
	}
	if goReceipt.Verdict != "GO" || goReceipt.Claim != "external_transfer_non_regression" {
		t.Fatalf("policy>=deep must transfer GO, got %s %s", goReceipt.Verdict, goReceipt.Claim)
	}
	noGoReceipt, err := utilityTransferNonRegression(110, 115, 150)
	if err != nil {
		t.Fatal(err)
	}
	if noGoReceipt.Verdict != "NO-GO" || noGoReceipt.Claim != "locomo_mechanism_only_not_portable" {
		t.Fatalf("policy<deep must transfer NO-GO with portability boundary, got %s %s", noGoReceipt.Verdict, noGoReceipt.Claim)
	}
	if noGoReceipt.ProductionAuthorized {
		t.Fatal("transfer NO-GO must not authorize production")
	}
}
