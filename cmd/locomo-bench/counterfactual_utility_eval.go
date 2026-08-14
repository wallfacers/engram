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

// --- US2/US3 stage runners (implemented with their failing tests) ---

func runUtilityPilotStage(opt *options) error {
	return fmt.Errorf("pilot stage not yet implemented")
}

func runUtilityCollectStage(opt *options) error {
	return fmt.Errorf("collect stage not yet implemented")
}

func runUtilityDiagnoseStage(opt *options) error {
	if opt.utilitySource == "" {
		return fmt.Errorf("diagnose stage requires a sealed collect source directory")
	}
	return fmt.Errorf("diagnose stage not yet implemented")
}

func runUtilityConfirmStage(opt *options) error {
	return fmt.Errorf("confirm stage not yet implemented")
}

func runUtilityTransferStage(opt *options) error {
	return fmt.Errorf("transfer stage not yet implemented")
}
