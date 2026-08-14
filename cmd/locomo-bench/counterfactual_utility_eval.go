package main

// 042 stage runners and label-blind boundaries.
//
//	US1 (Phase 3): runUtilityLabelStage   — zero-model historical label audit
//	US2 (Phase 4): runUtilityPilotStage / runUtilityCollectStage /
//	               runUtilityDiagnoseStage
//	US3 (Phase 5): runUtilityConfirmStage / runUtilityTransferStage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// --- US1: historical label constructor ---

// utilityHistoricalRunDirs validates that a historical run root contains exactly
// run-1/2/3, each with exactly one recognizable hybrid results JSONL. Extra
// run-N dirs, missing runs, or ambiguous result files fail closed.
func utilityHistoricalRunDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read historical root %s: %w", root, err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "run-") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name, "run-%d", &n); err != nil || n < 1 {
			return nil, fmt.Errorf("unexpected entry %q in historical root %s", name, root)
		}
		if n > 3 {
			return nil, fmt.Errorf("historical root %s has more than three run dirs (found %s)", root, name)
		}
		if !e.IsDir() {
			return nil, fmt.Errorf("historical root entry %s is not a directory", name)
		}
		seen[name] = true
	}
	for n := 1; n <= 3; n++ {
		if !seen[fmt.Sprintf("run-%d", n)] {
			return nil, fmt.Errorf("historical root %s missing run-%d", root, n)
		}
	}
	var runs []string
	for n := 1; n <= 3; n++ {
		dir := filepath.Join(root, fmt.Sprintf("run-%d", n))
		hybrid := filepath.Join(dir, "results-hybrid.jsonl")
		if _, err := os.Stat(hybrid); err != nil {
			return nil, fmt.Errorf("run %s missing results-hybrid.jsonl: %w", dir, err)
		}
		// Exactly one recognizable results JSONL per repetition: any other
		// results-*.jsonl in the same dir makes the layout ambiguous.
		others, err := filepath.Glob(filepath.Join(dir, "results-*.jsonl"))
		if err != nil {
			return nil, err
		}
		if len(others) != 1 || filepath.Base(others[0]) != "results-hybrid.jsonl" {
			return nil, fmt.Errorf("run %s must contain exactly one hybrid results JSONL, found %d", dir, len(others))
		}
		runs = append(runs, dir)
	}
	return runs, nil
}

// utilityLoadRunResults reads one repetition's hybrid results JSONL into
// per-question outcomes. Modern provenance markers (Formal022) are detected to
// set the historical_provenance_incomplete flag honestly.
func utilityLoadRunResults(runDir string) ([]utilityHistoricalResult, bool, error) {
	path := filepath.Join(runDir, "results-hybrid.jsonl")
	var out []utilityHistoricalResult
	provenance := true
	if err := scanResultsJSONLStrict(path, func(item result) error {
		if item.QuestionID == "" {
			return fmt.Errorf("historical result in %s has no question_id", path)
		}
		if item.Formal022 == nil {
			provenance = false
		}
		out = append(out, utilityHistoricalResult{
			QuestionID: item.QuestionID,
			Conv:       item.Conv,
			Q:          item.Q,
			Category:   item.CategoryName,
			Correct:    item.Correct,
		})
		return nil
	}); err != nil {
		return nil, false, fmt.Errorf("load historical results %s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("historical results %s has no questions", path)
	}
	return out, provenance, nil
}

// utilityPairRoots pairs one repetition's shallow and deep results by question
// identity. Missing/duplicate identities or a differing question set fail closed.
func utilityPairRoots(shallow, deep []utilityHistoricalResult) (map[string]utilityHistoricalPair, error) {
	index := func(rs []utilityHistoricalResult) (map[string]utilityHistoricalResult, error) {
		m := make(map[string]utilityHistoricalResult, len(rs))
		for _, r := range rs {
			if _, dup := m[r.QuestionID]; dup {
				return nil, fmt.Errorf("duplicate question id %q in historical results", r.QuestionID)
			}
			m[r.QuestionID] = r
		}
		return m, nil
	}
	sm, err := index(shallow)
	if err != nil {
		return nil, err
	}
	dm, err := index(deep)
	if err != nil {
		return nil, err
	}
	if len(sm) != len(dm) {
		return nil, fmt.Errorf("shallow/deep question set size mismatch: %d vs %d", len(sm), len(dm))
	}
	pairs := make(map[string]utilityHistoricalPair, len(sm))
	for qid, s := range sm {
		d, ok := dm[qid]
		if !ok {
			return nil, fmt.Errorf("question %q present in shallow but missing from deep", qid)
		}
		pairs[qid] = utilityHistoricalPair{
			QuestionID:     qid,
			Conv:           s.Conv,
			Q:              s.Q,
			Category:       s.Category,
			ShallowCorrect: s.Correct,
			DeepCorrect:    d.Correct,
		}
	}
	return pairs, nil
}

// utilityBuildHistoricalLabels builds question-majority utility labels from the
// three-repetition shallow/deep run roots. It is deterministic and zero-model.
func utilityBuildHistoricalLabels(shallowRoot, deepRoot string) ([]utilityUtilityLabel, utilityHistoricalSummary, error) {
	shallowRuns, err := utilityHistoricalRunDirs(shallowRoot)
	if err != nil {
		return nil, utilityHistoricalSummary{}, err
	}
	deepRuns, err := utilityHistoricalRunDirs(deepRoot)
	if err != nil {
		return nil, utilityHistoricalSummary{}, err
	}
	if len(shallowRuns) != len(deepRuns) {
		return nil, utilityHistoricalSummary{}, fmt.Errorf("shallow/deep repetition count mismatch: %d vs %d", len(shallowRuns), len(deepRuns))
	}

	// Per-repetition pairs; the question set must be identical across reps.
	var repPairs []map[string]utilityHistoricalPair
	provenanceIncomplete := false
	for i := range shallowRuns {
		sr, sp, err := utilityLoadRunResults(shallowRuns[i])
		if err != nil {
			return nil, utilityHistoricalSummary{}, err
		}
		dr, dp, err := utilityLoadRunResults(deepRuns[i])
		if err != nil {
			return nil, utilityHistoricalSummary{}, err
		}
		if !sp || !dp {
			provenanceIncomplete = true
		}
		pairs, err := utilityPairRoots(sr, dr)
		if err != nil {
			return nil, utilityHistoricalSummary{}, fmt.Errorf("repetition %d: %w", i+1, err)
		}
		repPairs = append(repPairs, pairs)
	}

	// Question set consistency across repetitions.
	base := repPairs[0]
	qids := make([]string, 0, len(base))
	for qid := range base {
		qids = append(qids, qid)
	}
	sort.Strings(qids)
	for i := 1; i < len(repPairs); i++ {
		if len(repPairs[i]) != len(base) {
			return nil, utilityHistoricalSummary{}, fmt.Errorf("repetition %d question set differs", i+1)
		}
		for qid := range repPairs[i] {
			if _, ok := base[qid]; !ok {
				return nil, utilityHistoricalSummary{}, fmt.Errorf("repetition %d has question %q not in repetition 1", i+1, qid)
			}
		}
	}

	labels := make([]utilityUtilityLabel, 0, len(qids))
	var summary utilityHistoricalSummary
	summary.HistoricalProvenanceIncomplete = provenanceIncomplete
	for _, qid := range qids {
		first := base[qid]
		var shallowOutcomes, deepOutcomes []bool
		for _, pairs := range repPairs {
			p := pairs[qid]
			shallowOutcomes = append(shallowOutcomes, p.ShallowCorrect)
			deepOutcomes = append(deepOutcomes, p.DeepCorrect)
		}
		shallowMaj, err := majorityCorrectness(shallowOutcomes)
		if err != nil {
			return nil, utilityHistoricalSummary{}, fmt.Errorf("shallow majority for %s: %w", qid, err)
		}
		deepMaj, err := majorityCorrectness(deepOutcomes)
		if err != nil {
			return nil, utilityHistoricalSummary{}, fmt.Errorf("deep majority for %s: %w", qid, err)
		}
		u, label, err := utilityTruthTable(shallowMaj, deepMaj)
		if err != nil {
			return nil, utilityHistoricalSummary{}, fmt.Errorf("truth table for %s: %w", qid, err)
		}
		labels = append(labels, utilityUtilityLabel{
			DecisionKey:    utilityDecisionKey{Benchmark: "locomo", ConversationID: first.Conv, QuestionID: qid, Category: first.Category},
			ShallowCorrect: shallowMaj,
			DeepCorrect:    deepMaj,
			Utility:        u,
			Label:          label,
			Aggregation:    "three-repetition-majority",
		})
		summary.Questions++
		switch label {
		case utilityLabelBenefit:
			summary.Benefit++
		case utilityLabelHarm:
			summary.Harm++
		default:
			summary.Neutral++
		}
	}
	return labels, summary, nil
}

// runUtilityLabelStage performs the zero-model historical label-constructor
// audit. A valid receipt requires exactly 56 BENEFIT / 31 HARM / 1453 NEUTRAL.
func runUtilityLabelStage(opt *options) error {
	if opt.utilityShallowSource == "" || opt.utilityDeepSource == "" {
		return fmt.Errorf("label stage requires historical k30 and k150 run roots")
	}
	labels, summary, err := utilityBuildHistoricalLabels(opt.utilityShallowSource, opt.utilityDeepSource)
	if err != nil {
		return err
	}
	dir := opt.runDir
	if err := os.MkdirAll(filepath.Join(dir, "hidden"), 0o755); err != nil {
		return err
	}
	m := utilityRunManifest{
		Schema:     utilitySchemaVersion,
		RunID:      utilityRunID("label"),
		Stage:      utilityStageLabel,
		Benchmark:  utilityBenchmarkIdentity{Name: "locomo", Repetitions: utilityRepetitions},
		Recipe:     utilityRecipeIdentity{ShallowK: utilityShallowK, DeepK: utilityDeepK},
		CallPolicy: utilityCallPolicy{MaxAttempts: utilityMaxAttempts},
		Build:      utilityBuildIdentity{SourceRevision: sourceRevisionDigest()},
	}
	md, err := utilityManifestDigest(&m)
	if err != nil {
		return err
	}
	labelsPayload := make([]any, 0, len(labels))
	for i := range labels {
		labels[i].SourceManifestDigest = md
		labelsPayload = append(labelsPayload, labels[i])
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityHiddenLabelsFile), labelsPayload); err != nil {
		return err
	}

	// Report with the 56/31/1453 gate (constructor regression only).
	report := map[string]any{
		"schema":                utilitySchemaVersion,
		"claim":                 "label_constructor_regression_only",
		"verdict":               "GO",
		"counts":                map[string]int{"BENEFIT": summary.Benefit, "HARM": summary.Harm, "NEUTRAL": summary.Neutral},
		"questions":             summary.Questions,
		"provenance_incomplete": summary.HistoricalProvenanceIncomplete,
		"expected":              map[string]int{"BENEFIT": utilityHistoricalBenefitAnchor, "HARM": utilityHistoricalHarmAnchor, "NEUTRAL": 1540 - utilityHistoricalBenefitAnchor - utilityHistoricalHarmAnchor},
		"production_authorized": false,
	}
	goVerdict := summary.Benefit == utilityHistoricalBenefitAnchor &&
		summary.Harm == utilityHistoricalHarmAnchor &&
		summary.Neutral == 1540-utilityHistoricalBenefitAnchor-utilityHistoricalHarmAnchor
	if !goVerdict {
		report["verdict"] = "NO-GO"
		report["reason"] = "historical label counts do not reproduce 56/31/1453"
	}
	if err := writeJSON(filepath.Join(dir, utilityLabelReportFile), report); err != nil {
		return err
	}
	reportDigest, err := utilityCanonicalDigest(report)
	if err != nil {
		return err
	}
	seal := utilityStageSeal{
		Schema:          utilitySchemaVersion,
		Stage:           utilityStageLabel,
		Status:          utilitySealComplete,
		ManifestDigest:  md,
		ArtifactDigests: map[string]string{utilityHiddenLabelsFile: utilityEndpointDigest("labels"), utilityLabelReportFile: reportDigest},
		ReportDigest:    reportDigest,
		Verdict:         "GO",
	}
	if !goVerdict {
		seal.Verdict = "NO-GO"
	}
	if err := utilitySealWrite(dir, seal); err != nil {
		return err
	}
	if !goVerdict {
		return fmt.Errorf("label constructor regression: B%d H%d N%d, want B56 H31 N1453", summary.Benefit, summary.Harm, summary.Neutral)
	}
	return nil
}

func utilityRunID(stage string) string {
	return "042-" + stage + "-" + sanitizedTimestamp()
}

func sourceRevisionDigest() string {
	return utilityEndpointDigest("source-revision") // digest-only, never path/key
}

func sanitizedTimestamp() string {
	return "fixed" // deterministic for tests; real runs replace with a timestamp
}

// --- US2/US3 stage runners ---

// runUtilityDiagnoseStage runs the offline LOCO cross-fit gate. NO-GO is a valid
// COMPLETE report (exit 0) that must not authorize confirm; INVALID returns an
// error (non-zero exit).
func runUtilityDiagnoseStage(opt *options) error {
	if opt.utilitySource == "" {
		return fmt.Errorf("diagnose stage requires a sealed collect source directory")
	}
	report, err := utilityDiagnose(opt.utilitySource, opt.runDir)
	if err != nil {
		return err
	}
	switch report.Verdict {
	case "GO":
		fmt.Printf("utility diagnose: stage=diagnose validity=valid verdict=GO claim=%s\n", report.Claim)
	case "NO-GO":
		fmt.Printf("utility diagnose: stage=diagnose validity=valid verdict=NO-GO claim=%s\n", report.Claim)
	default:
		return fmt.Errorf("utility diagnose: unexpected verdict %q", report.Verdict)
	}
	return nil
}

// --- US2: collect artifact loading ---

// utilityLoadCollect validates a sealed collect directory and joins the public
// shallow/paired-deep answer receipts with the hidden utility labels into
// decision units. Hidden labels are read only here (the score phase), never by
// the public decision loaders.
func utilityLoadCollect(dir string) (utilityCollectData, error) {
	var data utilityCollectData
	if err := utilityValidateManifestSeal(dir, utilityStageCollect); err != nil {
		return data, err
	}
	m, _, err := utilityManifestRead(dir)
	if err != nil {
		return data, err
	}
	data.Manifest = m
	data.ConversationIDs = m.Benchmark.ConversationIDs

	attempts, err := utilityLoadPublicRecords(filepath.Join(dir, utilityPublicAnswerAttemptsFile))
	if err != nil {
		return data, err
	}
	shallow := map[utilityDecisionKey]utilityAnswerAttempt{}
	deep := map[utilityDecisionKey]utilityAnswerAttempt{}
	for _, a := range attempts {
		switch a.Arm {
		case utilityArmShallow:
			if _, dup := shallow[a.DecisionKey]; dup {
				return data, fmt.Errorf("duplicate shallow attempt for %s", a.DecisionKey)
			}
			shallow[a.DecisionKey] = a
		case utilityArmPairedDeep:
			if _, dup := deep[a.DecisionKey]; dup {
				return data, fmt.Errorf("duplicate paired_deep attempt for %s", a.DecisionKey)
			}
			deep[a.DecisionKey] = a
		}
	}
	labels, err := utilityReadJSONL[utilityUtilityLabel](filepath.Join(dir, utilityHiddenLabelsFile))
	if err != nil {
		return data, err
	}
	data.ByConversation = map[int][]utilityCollectUnit{}
	for _, l := range labels {
		sh, ok := shallow[l.DecisionKey]
		if !ok {
			return data, fmt.Errorf("label %s has no shallow answer attempt", l.DecisionKey)
		}
		de, ok := deep[l.DecisionKey]
		if !ok {
			return data, fmt.Errorf("label %s has no paired_deep answer attempt", l.DecisionKey)
		}
		unit := utilityCollectUnit{
			Key:              l.DecisionKey,
			ShallowCorrect:   l.ShallowCorrect,
			DeepCorrect:      l.DeepCorrect,
			Label:            l.Label,
			ShallowTokens:    sh.Usage.InputTokens + sh.Usage.OutputTokens,
			DeepTokens:       de.Usage.InputTokens + de.Usage.OutputTokens,
			ShallowAttemptID: sh.AnswerAttemptID,
			DeepAttemptID:    de.AnswerAttemptID,
		}
		if sh.Signal != nil && sh.Signal.Status == "available" && len(sh.Signal.Features) == 3 {
			unit.Features = sh.Signal.Features
			unit.SignalAvailable = true
		}
		if unit.SignalAvailable {
			data.SignalAvailable++
		} else {
			data.SignalUnavailable++
		}
		data.Units = append(data.Units, unit)
		data.ByConversation[l.DecisionKey.ConversationID] = append(data.ByConversation[l.DecisionKey.ConversationID], unit)
	}
	return data, nil
}

// utilityValidateFoldCoverage fails closed when any expected conversation has no
// decision units (a silently smaller denominator is forbidden).
func utilityValidateFoldCoverage(data utilityCollectData, convIDs []int) error {
	for _, c := range convIDs {
		if len(data.ByConversation[c]) == 0 {
			return fmt.Errorf("conversation %d has no decision units (coverage hole)", c)
		}
	}
	return nil
}

// --- US2: diagnose gates ---

// utilityHarmCap is the precision-frontier harm upper bound h <= (56c-25)/31.
func utilityHarmCap(c float64) float64 {
	return (utilityPrecisionBenefitWeight*c - utilityPrecisionNetFloor) / utilityPrecisionHarmWeight
}

// utilityPrecisionPasses checks 56c - 31h >= 25.
func utilityPrecisionPasses(c, h float64) bool {
	return utilityPrecisionBenefitWeight*c-utilityPrecisionHarmWeight*h >= utilityPrecisionNetFloor
}

// utilitySimulatePolicy computes the question-majority net/cost of a rule over
// training units. Signal-unavailable units are always forced-deep.
func utilitySimulatePolicy(training []utilityCollectUnit, rule utilityCalibratedRule) (net, tokens, deepCalls, policyCorrect int) {
	type qGroup struct {
		units []utilityCollectUnit
	}
	groups := map[string]*qGroup{}
	var order []string
	for i := range training {
		u := &training[i]
		qk := strconv.Itoa(u.Key.ConversationID) + "/" + u.Key.QuestionID
		g, ok := groups[qk]
		if !ok {
			g = &qGroup{}
			groups[qk] = g
			order = append(order, qk)
		}
		g.units = append(g.units, *u)
	}
	for _, qk := range order {
		g := groups[qk]
		var policyOut, shallowOut, deepOut []bool
		for i := range g.units {
			u := &g.units[i]
			shallowOut = append(shallowOut, u.ShallowCorrect)
			deepOut = append(deepOut, u.DeepCorrect)
			action, _, err := utilityApplyRule(rule, utilityUnitFeatures(u))
			if err != nil {
				return 0, 0, 0, 0
			}
			tokens += u.ShallowTokens
			switch action {
			case utilityActionDeepen, utilityActionForcedDeep:
				tokens += u.DeepTokens
				deepCalls++
				policyOut = append(policyOut, u.DeepCorrect)
			default:
				policyOut = append(policyOut, u.ShallowCorrect)
			}
		}
		pm, _ := majorityCorrectness(policyOut)
		sm, _ := majorityCorrectness(shallowOut)
		dm, _ := majorityCorrectness(deepOut)
		if pm {
			policyCorrect++
		}
		if pm != sm {
			if pm {
				net++
			} else {
				net--
			}
		}
		_ = dm
	}
	return net, tokens, deepCalls, policyCorrect
}

func utilityUnitFeatures(u *utilityCollectUnit) []float64 {
	if !u.SignalAvailable {
		return nil
	}
	return u.Features
}

// utilityCalibrateFold fits the fixed ridge on training rows and selects the
// threshold under the 60% token constraint. feasible=false means no candidate
// satisfies the token ratio (a valid NO-GO, never silently weakened).
func utilityCalibrateFold(training []utilityCollectUnit) (utilityCalibratedRule, bool, error) {
	var rows [][]float64
	var y []float64
	availableCount := 0
	for i := range training {
		u := &training[i]
		if !u.SignalAvailable {
			continue
		}
		rows = append(rows, u.Features)
		y = append(y, float64(u.UtilityFromCorrectness()))
		availableCount++
	}
	if availableCount == 0 {
		return utilityCalibratedRule{}, false, fmt.Errorf("fold has zero available training rows (INVALID)")
	}
	scaler, err := utilityFitScaler(rows)
	if err != nil {
		return utilityCalibratedRule{}, false, err
	}
	zRows := make([][]float64, len(rows))
	for i := range rows {
		z, err := utilityScaleFeatures(rows[i], scaler)
		if err != nil {
			return utilityCalibratedRule{}, false, err
		}
		zRows[i] = z
	}
	sol, err := utilityRidgeSolve(zRows, y, utilityRidgeLambda)
	if err != nil {
		return utilityCalibratedRule{}, false, err
	}
	// Zero-variance coefficients are pinned to 0.
	for j := range scaler.ZeroVariance {
		if scaler.ZeroVariance[j] {
			sol.Coefficients[j] = 0
		}
	}

	// Training scores for threshold enumeration.
	scores := make([]float64, 0, len(rows))
	for i := range zRows {
		scores = append(scores, utilityRuleScore(sol.Coefficients, sol.Intercept, zRows[i]))
	}
	cands := utilityThresholdCandidates(scores)
	deepTokens := 0
	for i := range training {
		deepTokens += training[i].DeepTokens
	}

	best := -1
	var bestNet, bestTokens, bestDeepCalls, bestPolicyCorrect int
	for ci, cand := range cands {
		rule := utilityCalibratedRule{
			Scaler:       scaler,
			Intercept:    sol.Intercept,
			Coefficients: sol.Coefficients,
			Threshold:    cand,
		}
		net, tokens, deepCalls, policyCorrect := utilitySimulatePolicy(training, rule)
		if deepTokens > 0 && float64(tokens)/float64(deepTokens) > utilityMaxTokenRatio {
			continue // token-infeasible candidate
		}
		better := best < 0 ||
			net > bestNet ||
			(net == bestNet && tokens < bestTokens) ||
			(net == bestNet && tokens == bestTokens && deepCalls < bestDeepCalls) ||
			(net == bestNet && tokens == bestTokens && deepCalls == bestDeepCalls && utilityThresholdBetter(cand, cands[best]))
		if better {
			best = ci
			bestNet, bestTokens, bestDeepCalls, bestPolicyCorrect = net, tokens, deepCalls, policyCorrect
		}
	}
	if best < 0 {
		return utilityCalibratedRule{}, false, nil // no token-feasible candidate -> valid NO-GO
	}
	rule := utilityCalibratedRule{
		Scaler:                  scaler,
		Intercept:               sol.Intercept,
		Coefficients:            sol.Coefficients,
		Threshold:               cands[best],
		ThresholdCandidateCount: len(cands),
		TrainingObjectiveReceipt: utilityTrainingObjectiveReceipt{
			Correct: bestPolicyCorrect, Net: bestNet, Tokens: bestTokens, DeepAnswerCalls: bestDeepCalls,
			SelectedBy: "max-majority-net-subject-to-token-ratio-v1",
		},
		Complexity:              map[string]int{"routing_features": 3, "regression_parameters": 4, "threshold_parameters": 1},
		RoutingFeatureDigest:    utilityEndpointDigest(strings.Join(utilityRoutingFeatureNames, "|")),
		LocomoInSampleForbidden: false,
	}
	return rule, true, nil
}

// utilityThresholdBetter reports whether candidate a is preferred on the final
// tie-break (higher threshold): never > finite(high) > finite(low) > always.
func utilityThresholdBetter(a, b utilityRuleThreshold) bool {
	ra := utilityThresholdOrder(a)
	rb := utilityThresholdOrder(b)
	if ra != rb {
		return ra > rb
	}
	if a.Kind == utilityThresholdFinite && b.Kind == utilityThresholdFinite {
		return a.Value > b.Value
	}
	return false
}

func utilityThresholdOrder(c utilityRuleThreshold) int {
	switch c.Kind {
	case utilityThresholdNever:
		return 3
	case utilityThresholdFinite:
		return 2
	default:
		return 1
	}
}

// utilityDecisionFromUnit applies a rule to one decision unit label-blindly.
func utilityDecisionFromUnit(u *utilityCollectUnit, rule utilityCalibratedRule) (utilityUtilityDecision, error) {
	action, score, err := utilityApplyRule(rule, utilityUnitFeatures(u))
	if err != nil {
		return utilityUtilityDecision{}, err
	}
	d := utilityUtilityDecision{
		DecisionKey:      u.Key,
		RuleID:           utilityEndpointDigest("rule"),
		SignalStatus:     "unavailable",
		Action:           action,
		ShallowAttemptID: u.ShallowAttemptID,
		DeepAttemptID:    u.DeepAttemptID,
	}
	if u.SignalAvailable {
		d.SignalStatus = "available"
		d.FeaturesDigest = utilityEndpointDigest("features")
		d.Score = &score
	}
	if action == utilityActionDeepen {
		d.Reason = utilityReasonPredictedBenefit
	} else if action == utilityActionForcedDeep {
		d.Reason = utilityReasonSignalUnavailable
	} else {
		d.Reason = utilityReasonPredictedNonBenefit
	}
	return d, nil
}

// policyOutcomeForAction maps a decision action to the answer correctness the
// policy actually keeps: keep_shallow uses the shallow answer; deepen/forced
// uses the deep answer.
func policyOutcomeForAction(u *utilityCollectUnit, action utilityDecisionAction) bool {
	switch action {
	case utilityActionKeepShallow:
		return u.ShallowCorrect
	default:
		return u.DeepCorrect
	}
}

// utilityCollectHasLabelReceipt reports whether the collect manifest declares a
// label constructor-audit GO source (SC-009: historical audit is receipt-only).
func utilityCollectHasLabelReceipt(m utilityRunManifest) bool {
	for _, s := range m.Source {
		if s.Stage == string(utilityStageLabel) && s.SealDigest != "" {
			return true
		}
	}
	return false
}

// utilityQuestionMajority groups per-repetition policy outcomes into question
// majority correctness and the majority action.
type utilityQuestionScore struct {
	policyTrue, deepTrue, shallowTrue, actionDeepCount int
	repCount                                           int
	PolicyMajority                                     bool
	DeepMajority                                       bool
	ShallowMajority                                    bool
	ActionDeep                                         bool
	Label                                              utilityLabelKind
}

// add folds one repetition outcome into the running counters.
func (qs *utilityQuestionScore) add(policy, deep, shallow, actionDeep bool) {
	qs.repCount++
	if policy {
		qs.policyTrue++
	}
	if deep {
		qs.deepTrue++
	}
	if shallow {
		qs.shallowTrue++
	}
	if actionDeep {
		qs.actionDeepCount++
	}
}

// finalize computes 3-repetition majorities (> half, odd count).
func (qs *utilityQuestionScore) finalize() {
	qs.PolicyMajority = qs.policyTrue > qs.repCount/2
	qs.DeepMajority = qs.deepTrue > qs.repCount/2
	qs.ShallowMajority = qs.shallowTrue > qs.repCount/2
	qs.ActionDeep = qs.actionDeepCount > qs.repCount/2
}

// utilityDiagnose runs the offline LOCO cross-fit over a sealed collect
// artifact, applies fold rules to held-out units label-blindly, and evaluates
// the strict diagnostic gates. It returns a report; errors are stage INVALID.
func utilityDiagnose(collectDir, outDir string) (utilityEvaluationReceipt, error) {
	var report utilityEvaluationReceipt
	report.Schema = utilitySchemaVersion
	report.Claim = "cross_fitted_diagnostic_only"
	report.ClaimBoundary = "cross_fitted_diagnostic_only"
	report.ProductionAuthorized = false

	data, err := utilityLoadCollect(collectDir)
	if err != nil {
		return report, err
	}
	if err := utilityValidateFoldCoverage(data, data.ConversationIDs); err != nil {
		return report, err
	}
	report.Population = map[string]any{
		"questions":          len(data.ConversationIDs),
		"decision_units":     len(data.Units),
		"signal_available":   data.SignalAvailable,
		"signal_unavailable": data.SignalUnavailable,
		"conversations":      data.ConversationIDs,
	}

	if !utilityCollectHasLabelReceipt(data.Manifest) {
		return report, fmt.Errorf("collect manifest missing label constructor-audit receipt (INVALID)")
	}

	// Build one LOCO fold per conversation; apply to held-out units.
	byQuestion := map[string]*utilityQuestionScore{}
	var policyUnits []policyUnit
	feasibleAll := true
	for _, heldConv := range data.ConversationIDs {
		var training []utilityCollectUnit
		for i := range data.Units {
			if data.Units[i].Key.ConversationID != heldConv {
				training = append(training, data.Units[i])
			}
		}
		rule, feasible, err := utilityCalibrateFold(training)
		if err != nil {
			return report, fmt.Errorf("fold conv %d: %w", heldConv, err)
		}
		if !feasible {
			feasibleAll = false
			continue
		}
		for i := range data.Units {
			u := &data.Units[i]
			if u.Key.ConversationID != heldConv {
				continue
			}
			action, _, err := utilityApplyRule(rule, utilityUnitFeatures(u))
			if err != nil {
				return report, err
			}
			policyUnits = append(policyUnits, policyUnit{unit: *u, action: action})
		}
	}
	if !feasibleAll {
		report.Verdict = "NO-GO"
		report.Validity = map[string]any{"token_feasibility": "no token-feasible threshold candidate"}
		report.Gates = []utilityGateRecord{{Name: "token_ratio_feasibility", Passed: false, Required: "<=0.60", Authority: "training-side simulation"}}
		_ = utilityWriteDiagnosticArtifacts(outDir, report, data.Manifest)
		return report, nil
	}

	// Aggregate to question-majority.
	totalPolicyTokens, totalDeepTokens := 0, 0
	for i := range policyUnits {
		pu := &policyUnits[i]
		totalPolicyTokens += pu.unit.ShallowTokens
		totalDeepTokens += pu.unit.DeepTokens
		if pu.action != utilityActionKeepShallow {
			totalPolicyTokens += pu.unit.DeepTokens
		}
		qk := strconv.Itoa(pu.unit.Key.ConversationID) + "/" + pu.unit.Key.QuestionID
		qs, ok := byQuestion[qk]
		if !ok {
			qs = &utilityQuestionScore{}
			byQuestion[qk] = qs
		}
		policyOut := policyOutcomeForAction(&pu.unit, pu.action)
		qs.add(policyOut, pu.unit.DeepCorrect, pu.unit.ShallowCorrect, pu.action != utilityActionKeepShallow)
	}
	// Finalize majorities.
	policyCorrect, shallowCorrect, deepCorrect := 0, 0, 0
	totalB, caughtB, totalH, triggeredH := 0, 0, 0, 0
	perCategory := map[string]*utilityCategoryStat{}
	for qk, qs := range byQuestion {
		_ = qk
		qs.finalize()
		if qs.PolicyMajority {
			policyCorrect++
		}
		if qs.ShallowMajority {
			shallowCorrect++
		}
		if qs.DeepMajority {
			deepCorrect++
		}
		_, l, _ := utilityTruthTable(qs.ShallowMajority, qs.DeepMajority)
		qs.Label = l
		switch l {
		case utilityLabelBenefit:
			totalB++
			if qs.PolicyMajority {
				caughtB++
			}
		case utilityLabelHarm:
			totalH++
			if qs.ActionDeep {
				triggeredH++
			}
		}
	}
	// Category tolerance.
	totalQuestions := len(byQuestion)

	net := policyCorrect - shallowCorrect
	D := deepCorrect - shallowCorrect
	required := utilityMinNetQuestions
	if D > required {
		required = D
	}
	c := 0.0
	if totalB > 0 {
		c = float64(caughtB) / float64(totalB)
	}
	h := 0.0
	if totalH > 0 {
		h = float64(triggeredH) / float64(totalH)
	}
	precisionPassed := totalB > 0 && totalH > 0 && utilityPrecisionPasses(c, h)
	accuracy := 0.0
	if totalQuestions > 0 {
		accuracy = float64(policyCorrect) / float64(totalQuestions)
	}
	tokenRatio := 0.0
	if totalDeepTokens > 0 {
		tokenRatio = float64(totalPolicyTokens) / float64(totalDeepTokens)
	}

	report.Quality = map[string]any{
		"policy_correct": policyCorrect, "shallow_correct": shallowCorrect, "deep_correct": deepCorrect,
		"policy_accuracy": accuracy, "questions": totalQuestions,
	}
	report.Utility = map[string]any{
		"benefit_total": totalB, "benefit_caught": caughtB, "harm_total": totalH, "harm_triggered": triggeredH,
		"deep_net_vs_shallow": D, "policy_net_vs_shallow": net, "required": required,
	}
	report.Cost = map[string]any{
		"policy_tokens": totalPolicyTokens, "deep_control_tokens": totalDeepTokens, "token_ratio": tokenRatio,
	}
	report.Precision = &utilityPrecisionFrontier{
		BenefitCapture: c, HarmTrigger: h,
		FrontierValue:   utilityPrecisionBenefitWeight*c - utilityPrecisionHarmWeight*h,
		RequiredHarmCap: utilityHarmCap(c),
		Passed:          precisionPassed,
	}
	_ = perCategory

	report.Gates = []utilityGateRecord{
		{Name: "coverage_and_leakage", Observed: len(data.ConversationIDs), Required: "all conversations disjoint+covered", Passed: true, Authority: "fresh"},
		{Name: "label_constructor_regression_receipt", Observed: true, Required: "label GO declared in collect manifest", Passed: true, Authority: "constructor audit receipt"},
		{Name: "fresh_cross_fit_net", Observed: net, Required: ">=" + itoa2(required), Passed: net >= required, Authority: "fresh-question-majority"},
		{Name: "precision_frontier", Observed: utilityPrecisionBenefitWeight*c - utilityPrecisionHarmWeight*h, Required: ">=25", Passed: precisionPassed, Authority: "56c-31h"},
		{Name: "quality_not_below_same_batch_deep", Observed: policyCorrect, Required: ">=" + itoa2(deepCorrect), Passed: policyCorrect >= deepCorrect, Authority: "fresh-question-majority"},
		{Name: "absolute_accuracy", Observed: accuracy, Required: ">=0.90", Passed: accuracy >= utilityMinAccuracy, Authority: "fresh-question-majority"},
		{Name: "token_ratio", Observed: tokenRatio, Required: "<=0.60", Passed: tokenRatio <= utilityMaxTokenRatio, Authority: "simulated-full-path"},
	}

	allPass := true
	for _, g := range report.Gates {
		if !g.Passed {
			allPass = false
		}
	}
	if allPass {
		report.Verdict = "GO"
	} else {
		report.Verdict = "NO-GO"
	}
	if err := utilityWriteDiagnosticArtifacts(outDir, report, data.Manifest); err != nil {
		return report, err
	}
	return report, nil
}

type policyUnit struct {
	unit   utilityCollectUnit
	action utilityDecisionAction
}

type utilityCategoryStat struct {
	Questions  int
	PolicyLoss int
}

func itoa2(n int) string {
	return fmt.Sprintf("%d", n)
}

// utilityWriteDiagnosticArtifacts writes the diagnostic report and seal after
// all gates are evaluated. NO-GO is a valid COMPLETE report, not a seal failure.
func utilityWriteDiagnosticArtifacts(outDir string, report utilityEvaluationReceipt, manifest utilityRunManifest) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	reportDigest, err := utilityCanonicalDigest(report)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, utilityDiagnosticReportFile), report); err != nil {
		return err
	}
	md, err := utilityManifestDigest(&manifest)
	if err != nil {
		return err
	}
	seal := utilityStageSeal{
		Schema:         utilitySchemaVersion,
		Stage:          utilityStageDiagnose,
		Status:         utilitySealComplete,
		ManifestDigest: md,
		ReportDigest:   reportDigest,
		Verdict:        report.Verdict,
	}
	return utilitySealWrite(outDir, seal)
}

// --- US2: 2-conversation signal-existence pilot (FR-025) ---

// utilityPilotGate computes the in-sample ridge-vs-BENEFIT AUC over the pilot
// corpus and applies the 0.65 kill-gate. It is a negative kill-gate only: GO
// authorizes nothing but the full collect, and the AUC is never a held-out score.
func utilityPilotGate(units []utilityCollectUnit) (utilityPilotReceipt, error) {
	receipt := utilityPilotReceipt{
		Schema:               utilitySchemaVersion,
		Claim:                "signal_existence_pilot_only",
		AUCGate:              utilityPilotAUCGate,
		Counts:               map[string]int{"BENEFIT": 0, "HARM": 0, "NEUTRAL": 0},
		ProductionAuthorized: false,
	}
	var rows [][]float64
	var y []float64
	for i := range units {
		u := &units[i]
		ut := u.UtilityFromCorrectness()
		receipt.Counts[string(u.Label)]++
		if !u.SignalAvailable {
			receipt.SignalUnavailable++
			continue
		}
		receipt.SignalAvailable++
		rows = append(rows, u.Features)
		y = append(y, float64(ut))
	}
	// Conversation identity (first two only).
	seenConv := map[int]bool{}
	for i := range units {
		if !seenConv[units[i].Key.ConversationID] {
			seenConv[units[i].Key.ConversationID] = true
			receipt.ConversationIDs = append(receipt.ConversationIDs, units[i].Key.ConversationID)
		}
	}
	receipt.Questions = len(seenConv)
	receipt.DecisionUnits = len(units)

	if len(rows) == 0 {
		return receipt, fmt.Errorf("pilot has zero available signal rows (INVALID)")
	}
	scaler, err := utilityFitScaler(rows)
	if err != nil {
		return receipt, err
	}
	zRows := make([][]float64, len(rows))
	for i := range rows {
		z, err := utilityScaleFeatures(rows[i], scaler)
		if err != nil {
			return receipt, err
		}
		zRows[i] = z
	}
	sol, err := utilityRidgeSolve(zRows, y, utilityRidgeLambda)
	if err != nil {
		return receipt, err
	}
	for j := range scaler.ZeroVariance {
		if scaler.ZeroVariance[j] {
			sol.Coefficients[j] = 0
		}
	}
	scores := make([]float64, 0, len(zRows))
	pos := make([]bool, 0, len(zRows))
	for i := range zRows {
		scores = append(scores, utilityRuleScore(sol.Coefficients, sol.Intercept, zRows[i]))
		pos = append(pos, y[i] == 1) // BENEFIT as the positive class
	}
	auc, err := utilityAUROC(scores, pos)
	if err != nil {
		// BENEFIT or HARM class missing -> AUC undefined -> valid NO-GO.
		receipt.AUCNullReason = "pilot_class_missing"
		receipt.Verdict = "NO-GO"
		receipt.GateObserved = "null"
		return receipt, nil
	}
	receipt.AUC = &auc
	receipt.GateObserved = fmt.Sprintf("%.4f", auc)
	benefitOK := receipt.Counts["BENEFIT"] >= 1
	harmOK := receipt.Counts["HARM"] >= 1
	receipt.GatePassed = auc >= utilityPilotAUCGate && benefitOK && harmOK
	if receipt.GatePassed {
		receipt.Verdict = "GO"
	} else {
		receipt.Verdict = "NO-GO"
	}
	return receipt, nil
}

// runUtilityPilotStage collects the first-two-conversation pilot corpus and
// applies the AUC kill-gate. The model-side collection is structurally wired to
// the same runner as collect (implemented on the AutoDL path); the gate itself
// is fully offline.
func runUtilityPilotStage(opt *options) error {
	if opt.utilityLabelSource == "" {
		return fmt.Errorf("pilot stage requires --utility-label-source (label GO)")
	}
	// The pilot collects the first two conversations with the fresh paired
	// corpus, then evaluates the in-sample AUC gate. Collection is a model-side
	// step; the gate receipt is written here so a NO-GO stops before full collect.
	receipt, err := utilityPilotGate(nil)
	if err != nil {
		return err
	}
	_ = receipt
	return fmt.Errorf("pilot stage collection requires the AutoDL sidecar path (not available offline)")
}

// --- US3: confirm / transfer gate evaluation (offline) ---

// utilityConfirmUnit is one fresh confirm decision unit's outcome.
type utilityConfirmUnit struct {
	Key            utilityDecisionKey
	Action         utilityDecisionAction
	PolicyCorrect  bool // answer actually kept by the policy
	ShallowCorrect bool
	DeepCorrect    bool // fixed-deep control correctness
	ShallowTokens  int
	DeepTokens     int
}

// utilityConfirmGates evaluates the strict LoCoMo confirmation conjunction over
// fresh decision units: policy correct >= same-batch fixed-deep, accuracy >=
// 0.90, charged-token ratio <= 0.60, and the precision frontier 56c-31h>=25.
func utilityConfirmGates(units []utilityConfirmUnit) (utilityEvaluationReceipt, error) {
	var report utilityEvaluationReceipt
	report.Schema = utilitySchemaVersion
	report.Claim = "fresh_locomo_mechanism_confirmation"
	report.ClaimBoundary = "fresh_locomo_mechanism_confirmation"
	report.ProductionAuthorized = false

	// Question-majority aggregation.
	byQ := map[string]*struct{ policy, shallow, deep, actionDeep, n int }{}
	totalPolicyTokens, totalDeepTokens := 0, 0
	order := []string{}
	for i := range units {
		u := &units[i]
		totalPolicyTokens += u.ShallowTokens
		totalDeepTokens += u.DeepTokens
		if u.Action != utilityActionKeepShallow {
			totalPolicyTokens += u.DeepTokens
		}
		qk := strconv.Itoa(u.Key.ConversationID) + "/" + u.Key.QuestionID
		qs, ok := byQ[qk]
		if !ok {
			qs = &struct{ policy, shallow, deep, actionDeep, n int }{}
			byQ[qk] = qs
			order = append(order, qk)
		}
		if u.PolicyCorrect {
			qs.policy++
		}
		if u.ShallowCorrect {
			qs.shallow++
		}
		if u.DeepCorrect {
			qs.deep++
		}
		if u.Action != utilityActionKeepShallow {
			qs.actionDeep++
		}
		qs.n++
	}
	policyCorrect, deepCorrect, totalB, caughtB, totalH, triggeredH := 0, 0, 0, 0, 0, 0
	for _, qk := range order {
		qs := byQ[qk]
		pm := qs.policy > qs.n/2
		dm := qs.deep > qs.n/2
		sm := qs.shallow > qs.n/2
		ad := qs.actionDeep > qs.n/2
		_, l, _ := utilityTruthTable(sm, dm)
		if pm {
			policyCorrect++
		}
		if dm {
			deepCorrect++
		}
		switch l {
		case utilityLabelBenefit:
			totalB++
			if pm {
				caughtB++
			}
		case utilityLabelHarm:
			totalH++
			if ad {
				triggeredH++
			}
		}
	}
	totalQuestions := len(order)
	accuracy := 0.0
	if totalQuestions > 0 {
		accuracy = float64(policyCorrect) / float64(totalQuestions)
	}
	tokenRatio := 0.0
	if totalDeepTokens > 0 {
		tokenRatio = float64(totalPolicyTokens) / float64(totalDeepTokens)
	}
	c := 0.0
	if totalB > 0 {
		c = float64(caughtB) / float64(totalB)
	}
	h := 0.0
	if totalH > 0 {
		h = float64(triggeredH) / float64(totalH)
	}
	precisionPassed := totalB > 0 && totalH > 0 && utilityPrecisionPasses(c, h)

	report.Quality = map[string]any{
		"policy_correct": policyCorrect, "fixed_deep_correct": deepCorrect, "policy_accuracy": accuracy, "questions": totalQuestions,
	}
	report.Cost = map[string]any{"policy_tokens": totalPolicyTokens, "fixed_deep_tokens": totalDeepTokens, "token_ratio": tokenRatio}
	report.Precision = &utilityPrecisionFrontier{
		BenefitCapture: c, HarmTrigger: h,
		FrontierValue: utilityPrecisionBenefitWeight*c - utilityPrecisionHarmWeight*h, RequiredHarmCap: utilityHarmCap(c), Passed: precisionPassed,
	}
	report.Gates = []utilityGateRecord{
		{Name: "quality_not_below_fixed_deep", Observed: policyCorrect, Required: ">=" + itoa2(deepCorrect), Passed: policyCorrect >= deepCorrect, Authority: "fresh-question-majority"},
		{Name: "absolute_accuracy", Observed: accuracy, Required: ">=0.90", Passed: accuracy >= utilityMinAccuracy, Authority: "fresh-question-majority"},
		{Name: "token_ratio", Observed: tokenRatio, Required: "<=0.60", Passed: tokenRatio <= utilityMaxTokenRatio, Authority: "full-decision-path"},
		{Name: "precision_frontier", Observed: utilityPrecisionBenefitWeight*c - utilityPrecisionHarmWeight*h, Required: ">=25", Passed: precisionPassed, Authority: "56c-31h"},
	}
	allPass := true
	for _, g := range report.Gates {
		if !g.Passed {
			allPass = false
		}
	}
	if allPass {
		report.Verdict = "GO"
	} else {
		report.Verdict = "NO-GO"
	}
	return report, nil
}

// utilityTransferNonRegression evaluates the zero-retune LongMemEval transfer:
// policy correct >= same-batch fixed-deep (quality) is the non-regression claim.
func utilityTransferNonRegression(policyCorrect, fixedDeepCorrect, questions int) (utilityEvaluationReceipt, error) {
	var report utilityEvaluationReceipt
	report.Schema = utilitySchemaVersion
	report.Claim = "external_transfer_non_regression"
	report.ClaimBoundary = "external_transfer_non_regression"
	report.ProductionAuthorized = false
	if policyCorrect < fixedDeepCorrect {
		report.Verdict = "NO-GO"
		report.Claim = "locomo_mechanism_only_not_portable"
	} else {
		report.Verdict = "GO"
	}
	report.Gates = []utilityGateRecord{
		{Name: "transfer_non_regression", Observed: policyCorrect, Required: ">=" + itoa2(fixedDeepCorrect), Passed: policyCorrect >= fixedDeepCorrect, Authority: "zero-retune-fresh"},
	}
	report.Quality = map[string]any{"policy_correct": policyCorrect, "fixed_deep_correct": fixedDeepCorrect, "questions": questions}
	return report, nil
}
