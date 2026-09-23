package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// This file owns the 051 success-criterion verdict (task T16): the machine
// readable SC-001..SC-007 result, the aggregated four-arm report, and the writer
// that lands both next to the measurement journal. Every criterion is either
// measured or reported HOLD — nothing is ever silently relaxed.

const (
	// jevArmVerdictFile is the machine-readable SC-001..SC-007 verdict.
	jevArmVerdictFile = "jev_arm_verdict.json"
	// jevArmReportFile is the aggregated four-arm measurement report.
	jevArmReportFile = "jev_arms_report.json"
	// jevArmPassGated is the pass in which the filter actually scores candidates.
	jevArmPassGated = "gated"
	// jevArmPassDegraded is the pass with no configured filter: the product's
	// degraded path, which is what SC-005 measures.
	jevArmPassDegraded = "degraded"
	// jevArmMergedVerdictFile is the combined two-pass verdict.
	jevArmMergedVerdictFile = "jev_arm_verdict_merged.json"
	// jevCriterionPass, jevCriterionDeclared and jevCriterionHold are the criterion
	// statuses. An unmeasured gate is HOLD, never pass; declared records evidence
	// that is named rather than measured and never counts as measured evidence when
	// the two passes are merged.
	jevCriterionPass     = "pass"
	jevCriterionDeclared = "declared"
	jevCriterionHold     = "hold"
)

// jevSuccessCriterion is one success criterion's verdict.
type jevSuccessCriterion struct {
	ID      string             `json:"id"`
	Status  string             `json:"status"`
	Detail  string             `json:"detail"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// jevArmBudgetSummary aggregates SC-003's three arm-C truncation budgets and the
// paired D-minus-equal-budget delta over the same questions.
type jevArmBudgetSummary struct {
	Questions      int     `json:"questions"`
	CGateMean      float64 `json:"c_at_12_mean"`
	CReferenceMean float64 `json:"c_at_8_mean"`
	CEqualMean     float64 `json:"c_at_d_shown_mean"`
	DMean          float64 `json:"d_shown_recall_mean"`
	DMinusEqualPP  float64 `json:"d_minus_c_equal_pp"`
}

// jevArmRecallComparison is SC-003's paired comparison of arm D's recall@shown
// against arm C's recall under each truncation budget.
type jevArmRecallComparison struct {
	jevArmBudgetSummary
	DGteGate  bool `json:"d_gte_c_at_12"`
	DGteEqual bool `json:"d_gte_c_at_d_shown"`
}

// jevArmReport is the aggregated four-arm measurement report.
type jevArmReport struct {
	Rows              int                    `json:"rows"`
	Replications      int                    `json:"replications"`
	Main              []jevArmAggregate      `json:"main_block"`
	CategoryFive      []jevArmAggregate      `json:"category_five_block"`
	Contrasts         []jevArmContrast       `json:"contrasts"`
	Flips             jevArmFlips            `json:"flips"`
	Segments          jevArmSegments         `json:"segments"`
	RecallComparison  jevArmRecallComparison `json:"recall_comparison"`
	DegradedMatched   int                    `json:"degraded_set_matched_questions"`
	DegradedTotal     int                    `json:"degraded_set_comparable_questions"`
	DegradedC8Matched int                    `json:"degraded_c8_matched_questions"`
	DegradedC8Total   int                    `json:"degraded_c8_comparable_questions"`
	FilterCalls       int                    `json:"filter_calls"`
	CallBudget        jevCallBudget          `json:"call_budget"`
	Cost              *costReport            `json:"cost,omitempty"`
	Notes             []string               `json:"notes,omitempty"`
}

// jevArmVerdict is the machine-readable four-arm verdict for one pass.
type jevArmVerdict struct {
	Schema             string                `json:"schema"`
	Pass               string                `json:"pass"`
	ProtocolHash       string                `json:"protocol_hash,omitempty"`
	MechanismKey       string                `json:"mechanism_key"`
	FilterModel        string                `json:"filter_model"`
	RegistrationDigest string                `json:"registration_digest"`
	GeneratedAt        time.Time             `json:"generated_at"`
	Arms               []string              `json:"arms"`
	Report             jevArmReport          `json:"report"`
	Criteria           []jevSuccessCriterion `json:"criteria"`
	Verdict            string                `json:"verdict"`
	Promotion          evalVerdict           `json:"promotion_verdict"`
	PromotionDeclared  string                `json:"promotion_input_declared"`
	ValidityError      string                `json:"validity_error,omitempty"`
	InvalidReason      string                `json:"invalid_reason,omitempty"`
	Notes              []string              `json:"notes,omitempty"`
}

// jevArmVerdictInput is everything the verdict builder needs. Validity and
// artifact presence stay separate from the measurements so the criteria can be
// tested on synthetic rows while the writer still fails closed.
type jevArmVerdictInput struct {
	Registration          jevFilterRegistration
	ProtocolHash          string
	Rows                  []jevArmQuestionRow
	Derived               []jevArmQuestionDerived
	FilterCalls           int
	RetrievalCalls        int
	RetrievalCallLimit    int
	Validity              evalArtifactValidity
	Artifacts             map[string]bool
	B0ContinuityDeclared  bool
	DegradedPass          bool
	DefaultParityEvidence string
	JevCostAttribution    string
	Cost                  *costReport
	// InvalidReason marks a run that must not produce a promotable verdict at all
	// (arm B was truncated by the frozen cap, a call failed, a counter drifted).
	// It forces HOLD and an INVALID promotion whatever the partial rows say.
	InvalidReason string
}

// buildJevArmReport aggregates the rows into the report the verdict cites.
func buildJevArmReport(input jevArmVerdictInput) (jevArmReport, error) {
	report := jevArmReport{
		Rows:        len(input.Rows),
		FilterCalls: input.FilterCalls,
		CallBudget:  jevCallBudget{RetrievalCalls: input.RetrievalCalls, FilterCalls: input.FilterCalls, RetrievalCallLimit: input.RetrievalCallLimit},
		Segments:    measureJevArmSegments(jevFilteredRows(input.Rows)),
		Cost:        input.Cost,
	}
	for _, arm := range jevArmNames() {
		report.Main = append(report.Main, aggregateJevArm(rowsForArm(rowsForBlock(input.Rows, jevArmMainBlock), arm)))
		report.CategoryFive = append(report.CategoryFive, aggregateJevArm(rowsForArm(rowsForBlock(input.Rows, jevArmCategoryFiveBlock), arm)))
	}
	for _, pair := range [][2]jevArm{{jevArmB, jevArmD}, {jevArmA, jevArmC}, {jevArmA, jevArmD}} {
		contrast, err := jevArmContrastFor(rowsForBlock(input.Rows, jevArmMainBlock), pair[0], pair[1])
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("contrast %s->%s not reported: %v", pair[0], pair[1], err))
			continue
		}
		report.Contrasts = append(report.Contrasts, contrast)
	}
	flips, err := measureJevArmFlips(rowsForBlock(input.Rows, jevArmMainBlock))
	if err != nil {
		return report, err
	}
	report.Flips = flips
	comparison, err := measureJevArmRecallComparison(input.Rows, input.Derived)
	if err != nil {
		return report, err
	}
	report.RecallComparison = comparison
	for _, derived := range input.Derived {
		if derived.Block != jevArmMainBlock {
			continue
		}
		if derived.DegradedSetComparable {
			report.DegradedTotal++
			if derived.DegradedShownMatchesA {
				report.DegradedMatched++
			}
		}
		if derived.DegradedC8Comparable {
			report.DegradedC8Total++
			if derived.DegradedShownMatchesC8 {
				report.DegradedC8Matched++
			}
		}
	}
	if len(input.Derived) > 0 {
		report.Replications = input.Derived[0].Replications
	}
	if err := report.CallBudget.Validate(); err != nil {
		return report, err
	}
	return report, nil
}

// measureJevArmRecallComparison pairs arm D's recall@shown with arm C's recall at
// each of SC-003's budgets, over the questions where both are gradeable.
func measureJevArmRecallComparison(rows []jevArmQuestionRow, derived []jevArmQuestionDerived) (jevArmRecallComparison, error) {
	type key struct{ conv, q int }
	dRows := map[key]jevArmQuestionRow{}
	for _, row := range rowsForBlock(rows, jevArmMainBlock) {
		if row.Arm == jevArmD {
			dRows[key{conv: row.Conv, q: row.Q}] = row
		}
	}
	comparison := jevArmRecallComparison{}
	var dSum, gateSum, referenceSum, equalSum, deltaSumPP float64
	for _, question := range derived {
		if question.Block != jevArmMainBlock || !question.BudgetGradeable {
			continue
		}
		dRow, ok := dRows[key{conv: question.Conv, q: question.Q}]
		if !ok || !dRow.Gradeable {
			continue
		}
		comparison.Questions++
		dSum += dRow.ShownRecall
		gateSum += question.CBudgetRecallGate
		referenceSum += question.CBudgetRecallReference
		equalSum += question.CBudgetRecallEqual
		deltaSumPP += (dRow.ShownRecall - question.CBudgetRecallEqual) * 100
	}
	if comparison.Questions == 0 {
		return comparison, nil
	}
	denominator := float64(comparison.Questions)
	comparison.DMean = dSum / denominator
	comparison.CGateMean = gateSum / denominator
	comparison.CReferenceMean = referenceSum / denominator
	comparison.CEqualMean = equalSum / denominator
	comparison.DMinusEqualPP = deltaSumPP / denominator
	comparison.DGteGate = comparison.DMean >= comparison.CGateMean
	comparison.DGteEqual = comparison.DMean >= comparison.CEqualMean
	return comparison, nil
}

// buildJevArmVerdict computes SC-001..SC-007 for one pass. A criterion that this
// pass cannot measure is HOLD with the reason spelled out; nothing defaults to
// pass.
func buildJevArmVerdict(input jevArmVerdictInput) (jevArmVerdict, error) {
	pass := jevArmPassGated
	if input.DegradedPass {
		pass = jevArmPassDegraded
	}
	digest, err := input.Registration.Digest()
	if err != nil {
		return jevArmVerdict{}, err
	}
	report, err := buildJevArmReport(input)
	if err != nil {
		return jevArmVerdict{}, err
	}
	verdict := jevArmVerdict{
		Schema:             evalProtocolSchema,
		Pass:               pass,
		ProtocolHash:       input.ProtocolHash,
		MechanismKey:       jevFilterMechanismKey,
		FilterModel:        input.Registration.FilterModel,
		RegistrationDigest: digest,
		GeneratedAt:        time.Now().UTC(),
		Arms:               armNamesInOrder(),
		Report:             report,
		PromotionDeclared:  jevPromotionInputDeclaration(),
	}
	if !input.DegradedPass {
		verdict.Criteria = append(verdict.Criteria, jevSC001(input), jevSC002(input), jevSC003(input), jevSC004(input))
	} else {
		for _, id := range []string{"SC-001", "SC-002", "SC-003", "SC-004"} {
			verdict.Criteria = append(verdict.Criteria, jevSuccessCriterion{
				ID:     id,
				Status: jevCriterionHold,
				Detail: "not measured in the degraded pass: the filter issued no scoring call, so the gated contrast is undefined",
			})
		}
	}
	verdict.Criteria = append(verdict.Criteria, jevSC005(input), jevSC006(input), jevSC007(input))
	if validityErr := validateJevRunValidity(jevValidityInput{
		Validity:             input.Validity,
		Present:              input.Artifacts,
		B0ContinuityDeclared: input.B0ContinuityDeclared,
		Registration:         input.Registration,
	}); validityErr != nil {
		verdict.ValidityError = validityErr.Error()
		verdict.Notes = append(verdict.Notes, "validity gate failed: the criteria below are not promotable evidence")
	}
	verdict.Verdict = jevVerdictFor(verdict.Criteria)
	if input.InvalidReason != "" {
		// A partially measured run could otherwise satisfy a gate over a smaller
		// denominator; an invalid run is never promotable.
		verdict.InvalidReason = input.InvalidReason
		verdict.Verdict = string(evalVerdictHOLD)
		verdict.Notes = append(verdict.Notes, "run INVALID: "+input.InvalidReason)
	}
	verdict.Promotion = jevPromotionFor(input, report, verdict)
	if input.InvalidReason != "" {
		verdict.Promotion = evalVerdictInvalid
	}
	return verdict, nil
}

// jevVerdictFor collapses criterion statuses into the pass verdict. HOLD wins if
// any criterion is HOLD or merely declared: a declaration is not a measurement, so
// it can never yield GO on its own.
func jevVerdictFor(criteria []jevSuccessCriterion) string {
	for _, criterion := range criteria {
		if criterion.Status != jevCriterionPass {
			return string(evalVerdictHOLD)
		}
	}
	return string(evalVerdictGO)
}

// jevPromotionInputDeclaration names where every promotionVerdictFor input comes
// from, so the linkage from SC-001..SC-007 to the 038 promotion gate is explicit
// rather than implied (research R7b).
func jevPromotionInputDeclaration() string {
	return "primary_delta=B->D LoCoMo 口径 (paired, run-internal); other_benchmark_delta=0 declared (LongMemEval-S deferred, not measured); " +
		"candidate_coverage_non_regression=arm B pool recall@150 vs arm C; judge_audit=per-run declared; offline_compatible=engine keeps no required endpoint; " +
		"category_results=not wired (per-category Holm gate stays with the 038 runner)"
}

// jevPromotionFor feeds the SC aggregate into the 038 promotion gate through
// HOLD: a criterion that is not met can never yield GO, while the gate stays free
// to return something stricter (INVALID/STOP).
func jevPromotionFor(input jevArmVerdictInput, report jevArmReport, verdict jevArmVerdict) evalVerdict {
	promotion := promotionVerdictFor(evalPromotionInput{
		Validity:                          input.Validity,
		PrimaryDeltaPP:                    jevContrastByArms(report.Contrasts, jevArmB, jevArmD).DeltaPP,
		PrimaryMcNemarP:                   jevContrastByArms(report.Contrasts, jevArmB, jevArmD).McNemarP,
		OtherBenchmarkDeltaPP:             0, // LongMemEval-S deferred: declared, never measured
		OtherBenchmarkNegativeSignificant: false,
		CandidateCoverageNonRegression:    jevArmCoverageNonRegression(report),
		JudgeAuditComplete:                input.Validity.isComplete(),
		JudgeAuditVerdictStable:           input.Validity.isComplete(),
		OfflineCompatible:                 input.Validity.isComplete() || !input.DegradedPass,
	})
	if verdict.Verdict == string(evalVerdictHOLD) && promotion == evalVerdictGO {
		return evalVerdictHOLD
	}
	return promotion
}

// jevArmCoverageNonRegression is the declared candidate-coverage input for the
// promotion gate: the wide arms must retrieve at least as much gold as the
// truncated arm, which is the pool-coverage reason the wide pool exists.
func jevArmCoverageNonRegression(report jevArmReport) bool {
	b := jevArmAggregateByArm(report.Main, jevArmB)
	c := jevArmAggregateByArm(report.Main, jevArmC)
	if b.PoolMeasured == 0 || c.PoolMeasured == 0 {
		return false
	}
	return b.MeanPoolRecall+1e-9 >= c.MeanPoolRecall
}

func jevArmAggregateByArm(aggregates []jevArmAggregate, arm jevArm) jevArmAggregate {
	for _, aggregate := range aggregates {
		if aggregate.Arm == arm {
			return aggregate
		}
	}
	return jevArmAggregate{Arm: arm}
}

func jevContrastByArms(contrasts []jevArmContrast, control, treatment jevArm) jevArmContrast {
	for _, contrast := range contrasts {
		if contrast.Control == control && contrast.Treatment == treatment {
			return contrast
		}
	}
	return jevArmContrast{Control: control, Treatment: treatment}
}

// jevSC001 checks the primary accuracy gate: D must stay within 0.5pp of B and
// must not regress significantly (run-internal paired exact McNemar).
func jevSC001(input jevArmVerdictInput) jevSuccessCriterion {
	report, err := buildJevArmReport(input)
	if err != nil {
		return jevSuccessCriterion{ID: "SC-001", Status: jevCriterionHold, Detail: fmt.Sprintf("report failed: %v", err)}
	}
	contrast := jevContrastByArms(report.Contrasts, jevArmB, jevArmD)
	if contrast.Questions == 0 {
		return jevSuccessCriterion{ID: "SC-001", Status: jevCriterionHold, Detail: "no paired B->D accuracy outcomes were measured"}
	}
	metrics := map[string]float64{
		"delta_pp": contrast.DeltaPP, "mcnemar_p": contrast.McNemarP,
		"b_accuracy": contrast.ControlAccuracy, "d_accuracy": contrast.TreatmentAccuracy,
		"paired_ci_lower": contrast.CI.Lower, "paired_ci_upper": contrast.CI.Upper,
		"questions": float64(contrast.Questions),
	}
	regressed := contrast.McNemarP < 0.05 && contrast.DeltaPP < 0
	if contrast.DeltaPP >= -0.5 && !regressed {
		return jevSuccessCriterion{ID: "SC-001", Status: jevCriterionPass, Detail: fmt.Sprintf("D %+0.2fpp vs B (p=%.4f, paired CI [%+0.2f,%+0.2f]pp)", contrast.DeltaPP, contrast.McNemarP, contrast.CI.Lower, contrast.CI.Upper), Metrics: metrics}
	}
	return jevSuccessCriterion{ID: "SC-001", Status: jevCriterionHold, Detail: fmt.Sprintf("D %+0.2fpp vs B (p=%.4f) fails the >= -0.5pp / no-significant-regression gate", contrast.DeltaPP, contrast.McNemarP), Metrics: metrics}
}

// jevSC002 checks the budget gate: at most 12 shown entries and at most a third
// of B's packer-admitted answer-input tokens.
func jevSC002(input jevArmVerdictInput) jevSuccessCriterion {
	report, err := buildJevArmReport(input)
	if err != nil {
		return jevSuccessCriterion{ID: "SC-002", Status: jevCriterionHold, Detail: fmt.Sprintf("report failed: %v", err)}
	}
	b := jevArmAggregateByArm(report.Main, jevArmB)
	d := jevArmAggregateByArm(report.Main, jevArmD)
	if d.Questions == 0 || b.MeanAnswerInputTokens <= 0 {
		return jevSuccessCriterion{ID: "SC-002", Status: jevCriterionHold, Detail: "arm D has no measured rows or arm B admitted zero tokens"}
	}
	ratio := d.MeanAnswerInputTokens / b.MeanAnswerInputTokens
	metrics := map[string]float64{"d_mean_shown": d.MeanShown, "d_mean_tokens": d.MeanAnswerInputTokens, "b_mean_tokens": b.MeanAnswerInputTokens, "token_ratio": ratio}
	if d.MeanShown <= jevArmCShowGate && ratio <= 1.0/3.0 {
		return jevSuccessCriterion{ID: "SC-002", Status: jevCriterionPass, Detail: fmt.Sprintf("D shows %.2f entries and %.1f%% of B's tokens", d.MeanShown, ratio*100), Metrics: metrics}
	}
	return jevSuccessCriterion{ID: "SC-002", Status: jevCriterionHold, Detail: fmt.Sprintf("D shows %.2f entries (gate %d) and %.1f%% of B's tokens (gate 33.3%%)", d.MeanShown, jevArmCShowGate, ratio*100), Metrics: metrics}
}

// jevSC003 checks the equal-budget recall gate: D's recall@shown must not trail
// arm C at the 12-entry gate budget or at D's own per-question budget, with C@8
// reported as the production reference.
func jevSC003(input jevArmVerdictInput) jevSuccessCriterion {
	report, err := buildJevArmReport(input)
	if err != nil {
		return jevSuccessCriterion{ID: "SC-003", Status: jevCriterionHold, Detail: fmt.Sprintf("report failed: %v", err)}
	}
	comparison := report.RecallComparison
	if comparison.Questions == 0 {
		return jevSuccessCriterion{ID: "SC-003", Status: jevCriterionHold, Detail: "no question had both arm C and arm D recall gradeable"}
	}
	metrics := map[string]float64{
		"questions": float64(comparison.Questions),
		"d_mean":    comparison.DMean, "c_at_12": comparison.CGateMean, "c_at_8": comparison.CReferenceMean,
		"c_at_d_shown": comparison.CEqualMean, "d_minus_c_equal_pp": comparison.DMinusEqualPP,
	}
	if comparison.DGteGate && comparison.DGteEqual {
		return jevSuccessCriterion{ID: "SC-003", Status: jevCriterionPass, Detail: fmt.Sprintf("D %.3f >= C@12 %.3f and >= C@D %.3f (C@8 %.3f)", comparison.DMean, comparison.CGateMean, comparison.CEqualMean, comparison.CReferenceMean), Metrics: metrics}
	}
	return jevSuccessCriterion{ID: "SC-003", Status: jevCriterionHold, Detail: fmt.Sprintf("D %.3f vs C@12 %.3f (gate) and C@D %.3f (equal budget)", comparison.DMean, comparison.CGateMean, comparison.CEqualMean), Metrics: metrics}
}

// jevSC004 checks the category-5 adversarial block on the D-noRelax variant: the
// empty-injection rate must not trail arm C and must clear the pre-registered
// absolute floor.
func jevSC004(input jevArmVerdictInput) jevSuccessCriterion {
	noRelaxRows := rowsForBlock(rowsForArm(input.Rows, jevArmDNoRelax), jevArmCategoryFiveBlock)
	cRows := rowsForBlock(rowsForArm(input.Rows, jevArmC), jevArmCategoryFiveBlock)
	noRelaxRate, noRelaxQuestions := jevArmEmptyInjectionRate(noRelaxRows)
	if noRelaxQuestions == 0 || len(cRows) == 0 {
		return jevSuccessCriterion{ID: "SC-004", Status: jevCriterionHold, Detail: "the category-5 declared block was not run (or arm C has no block rows)"}
	}
	cRate, _ := jevArmEmptyInjectionRate(cRows)
	metrics := map[string]float64{
		"questions": float64(noRelaxQuestions), "d_no_relax_empty_rate": noRelaxRate,
		"c_empty_rate": cRate, "floor": jevEmptyInjectionFloor,
	}
	if noRelaxRate >= cRate && noRelaxRate >= jevEmptyInjectionFloor {
		return jevSuccessCriterion{ID: "SC-004", Status: jevCriterionPass, Detail: fmt.Sprintf("D-noRelax empty %.3f >= C %.3f and >= floor %.2f", noRelaxRate, cRate, jevEmptyInjectionFloor), Metrics: metrics}
	}
	return jevSuccessCriterion{ID: "SC-004", Status: jevCriterionHold, Detail: fmt.Sprintf("D-noRelax empty %.3f vs C %.3f (floor %.2f)", noRelaxRate, cRate, jevEmptyInjectionFloor), Metrics: metrics}
}

// jevSC005 checks the degraded path. Only the degraded pass can measure it: the
// filtered arms must show exactly the no-filter set, and the paired A->C accuracy
// contrast (arm A and C@8 are the same RRF top-8 evidence) must not regress.
func jevSC005(input jevArmVerdictInput) jevSuccessCriterion {
	if !input.DegradedPass {
		return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionHold, Detail: "requires the degraded pass (no configured filter); a gated pass cannot measure the fallback path"}
	}
	report, err := buildJevArmReport(input)
	if err != nil {
		return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionHold, Detail: fmt.Sprintf("report failed: %v", err)}
	}
	if report.DegradedTotal == 0 {
		return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionHold, Detail: "no degraded question had both arm A and arm D observations"}
	}
	contrast := jevContrastByArms(report.Contrasts, jevArmA, jevArmC)
	metrics := map[string]float64{
		"matched_a": float64(report.DegradedMatched), "comparable_a": float64(report.DegradedTotal),
		"matched_c8": float64(report.DegradedC8Matched), "comparable_c8": float64(report.DegradedC8Total),
		"a_vs_c_delta_pp": contrast.DeltaPP, "a_vs_c_mcnemar_p": contrast.McNemarP,
	}
	allMatched := report.DegradedMatched == report.DegradedTotal
	// The spec's comparator is C@8. It is reported alongside arm A rather than
	// replacing it: arm A and C@8 are the same RRF top-8 by construction, so a
	// disagreement is a finding, not a rounding detail. When arm C produced no pool
	// the A check stands in for it and the detail says which counts were used.
	c8OK := report.DegradedC8Total == 0 || report.DegradedC8Matched == report.DegradedC8Total
	// The accuracy leg needs a measured A->C contrast: an unmeasurable contrast is
	// HOLD, never a pass by default (the same rule SC-001 follows).
	accuracyMeasured := contrast.Questions > 0
	accuracyOK := accuracyMeasured && contrast.DeltaPP >= 0
	if !accuracyMeasured {
		return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionHold, Detail: fmt.Sprintf("the degraded A->C accuracy contrast is unmeasurable (%d questions), so the fallback path's accuracy leg has no evidence", contrast.Questions), Metrics: metrics}
	}
	if allMatched && c8OK && accuracyOK {
		return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionPass, Detail: fmt.Sprintf("degraded shown set == arm A on %d/%d and == C@8 on %d/%d questions; A->C %+0.2fpp", report.DegradedMatched, report.DegradedTotal, report.DegradedC8Matched, report.DegradedC8Total, contrast.DeltaPP), Metrics: metrics}
	}
	return jevSuccessCriterion{ID: "SC-005", Status: jevCriterionHold, Detail: fmt.Sprintf("degraded set matched arm A %d/%d and C@8 %d/%d; A->C %+0.2fpp (p=%.4f)", report.DegradedMatched, report.DegradedTotal, report.DegradedC8Matched, report.DegradedC8Total, contrast.DeltaPP, contrast.McNemarP), Metrics: metrics}
}

// jevSC006 checks the no-key default path. The degraded pass measures it (every
// filtered arm must equal the no-filter set); a gated pass can only name the
// engine-side evidence, which is reported as `declared` so the merged verdict
// never mistakes a declaration for a measurement.
func jevSC006(input jevArmVerdictInput) jevSuccessCriterion {
	if input.DegradedPass {
		report, err := buildJevArmReport(input)
		if err != nil {
			return jevSuccessCriterion{ID: "SC-006", Status: jevCriterionHold, Detail: fmt.Sprintf("report failed: %v", err)}
		}
		if report.DegradedTotal > 0 && report.DegradedMatched == report.DegradedTotal {
			return jevSuccessCriterion{ID: "SC-006", Status: jevCriterionPass, Detail: fmt.Sprintf("no-key run: %d/%d questions fell back to the no-filter evidence set; %s", report.DegradedMatched, report.DegradedTotal, input.DefaultParityEvidence), Metrics: map[string]float64{"matched": float64(report.DegradedMatched), "comparable": float64(report.DegradedTotal)}}
		}
		return jevSuccessCriterion{ID: "SC-006", Status: jevCriterionHold, Detail: "no-key run did not fall back to the no-filter evidence set on every question"}
	}
	if input.DefaultParityEvidence == "" {
		return jevSuccessCriterion{ID: "SC-006", Status: jevCriterionHold, Detail: "no default-parity evidence was supplied for this gated pass"}
	}
	return jevSuccessCriterion{ID: "SC-006", Status: jevCriterionDeclared, Detail: "declared, not measured: " + input.DefaultParityEvidence}
}

// jevSC007 checks the segmented accounting: a gated pass must have issued filter
// calls, and a per-call latency or cost more than an order of magnitude off the
// expectation needs an explicit attribution.
func jevSC007(input jevArmVerdictInput) jevSuccessCriterion {
	segments := measureJevArmSegments(jevFilteredRows(input.Rows))
	metrics := map[string]float64{
		"jev_calls": float64(segments.JevCalls), "jev_ms_p50": segments.JevMsP50, "jev_ms_p95": segments.JevMsP95,
		"jev_usd_per_call": segments.JevUSDPerCall, "expected_ms": jevFilterLatencyExpectationMS, "expected_usd": jevFilterCostExpectationUSD,
	}
	if input.DegradedPass {
		return jevSuccessCriterion{ID: "SC-007", Status: jevCriterionHold, Detail: "the degraded pass issues no filter calls, so the filter segment is unmeasured", Metrics: metrics}
	}
	if segments.JevCalls == 0 {
		return jevSuccessCriterion{ID: "SC-007", Status: jevCriterionHold, Detail: "no filter call was journaled, so the segmented accounting has no filter segment", Metrics: metrics}
	}
	// The spec's anomaly rule is per-call ("single call more than an order of
	// magnitude off"): gating on p95 rather than p50 means up to 5% of calls can be
	// that far off before an attribution is demanded, instead of half of them.
	latencyAnomalous := float64(segments.JevMsP95) > jevFilterAnomalyFactor*float64(jevFilterLatencyExpectationMS)
	costAnomalous := segments.JevUSDPerCall > jevFilterAnomalyFactor*jevFilterCostExpectationUSD
	if (latencyAnomalous || costAnomalous) && input.JevCostAttribution == "" {
		return jevSuccessCriterion{ID: "SC-007", Status: jevCriterionHold, Detail: fmt.Sprintf("filter segment is more than %dx off expectation (p95 %dms, $%.6f/call) and no attribution was supplied", jevFilterAnomalyFactor, int(segments.JevMsP95), segments.JevUSDPerCall), Metrics: metrics}
	}
	detail := fmt.Sprintf("filter p50 %dms p95 %dms, $%.6f/call over %d calls", int(segments.JevMsP50), int(segments.JevMsP95), segments.JevUSDPerCall, segments.JevCalls)
	if latencyAnomalous || costAnomalous {
		detail += "; anomaly attributed: " + input.JevCostAttribution
	}
	return jevSuccessCriterion{ID: "SC-007", Status: jevCriterionPass, Detail: detail, Metrics: metrics}
}

// writeJevArmArtifacts lands the measurement journal, the report, and the
// verdict. A failed validity gate is still written (so the operator can read why)
// and then returned as an error, so the run exits non-zero rather than presenting
// an unpromotable verdict as a result.
func writeJevArmArtifacts(opt options, registration jevFilterRegistration, protocol *evalProtocol, rows []jevArmQuestionRow, derived []jevArmQuestionDerived, filterCalls, retrievalCalls int, ledger *costLedger, degradedPass bool, invalidReason string) error {
	if opt.runDir == "" {
		return fmt.Errorf("writing jev arm artifacts requires --run-dir")
	}
	var cost *costReport
	if ledger != nil {
		report := ledger.Report()
		cost = &report
	}
	input := jevArmVerdictInput{
		Registration:          registration,
		Rows:                  rows,
		Derived:               derived,
		FilterCalls:           filterCalls,
		RetrievalCalls:        retrievalCalls,
		Artifacts:             jevArtifactPresence(opt.runDir),
		DegradedPass:          degradedPass,
		JevCostAttribution:    opt.jevCostAttribution,
		DefaultParityEvidence: jevDefaultParityEvidence(),
		InvalidReason:         invalidReason,
		Cost:                  cost,
	}
	if protocol != nil {
		input.ProtocolHash = protocol.ProtocolHash
		// The four-arm run issues one retrieval per arm per measured
		// question-repetition, so the budget it is audited against is the frozen
		// per-question limit scaled by that multiplicity.
		input.RetrievalCallLimit = protocol.Budget.RetrievalCallLimit * len(jevArmNames()) * len(derived)
	}
	input.B0ContinuityDeclared = opt.jevB0Continuity
	input.Validity = jevArtifactValidityFromDir(opt.runDir)
	verdict, err := buildJevArmVerdict(input)
	if err != nil {
		return err
	}
	prior := readJevArmVerdict(filepath.Join(opt.runDir, jevArmVerdictFile))
	if err := writeJevArmRows(filepath.Join(opt.runDir, jevArmRowsFile), rows); err != nil {
		return fmt.Errorf("write %s: %w", jevArmRowsFile, err)
	}
	if err := writeJSON(filepath.Join(opt.runDir, jevArmReportFile), verdict.Report); err != nil {
		return fmt.Errorf("write %s: %w", jevArmReportFile, err)
	}
	if err := writeJSON(filepath.Join(opt.runDir, jevArmVerdictFile), verdict); err != nil {
		return fmt.Errorf("write %s: %w", jevArmVerdictFile, err)
	}
	// A second pass of the other kind in the same run directory completes the
	// protocol: the gated pass cannot measure SC-005 and the degraded pass cannot
	// measure SC-001..SC-004, so the merged verdict is what a promotion decision
	// cites. A missing counterpart is not an error — the operator simply has not
	// run it yet.
	if merged, mergeErr := mergedJevArmVerdict(prior, verdict); mergeErr != nil {
		return fmt.Errorf("merge jev arm verdicts: %w", mergeErr)
	} else if merged != nil {
		if err := writeJSON(filepath.Join(opt.runDir, jevArmMergedVerdictFile), merged); err != nil {
			return fmt.Errorf("write %s: %w", jevArmMergedVerdictFile, err)
		}
	}
	if verdict.ValidityError != "" {
		return fmt.Errorf("jev four-arm run failed the validity gate: %s", verdict.ValidityError)
	}
	if invalidReason != "" {
		return fmt.Errorf("jev four-arm run is INVALID: %s", invalidReason)
	}
	return nil
}

// jevDefaultParityEvidence names the evidence behind SC-006's declared half. The
// measured half is the degraded pass's per-question set equality.
func jevDefaultParityEvidence() string {
	return "engine parity locked by the 051 test suite (memory.SearchFiltered none/degraded path and the mcpserver contract parity tests) and the arm A production seam"
}

// jevArtifactPresence reports which validity artifacts exist in the run dir.
func jevArtifactPresence(runDir string) map[string]bool {
	present := make(map[string]bool, len(jevCoreValidityArtifacts))
	for _, artifact := range jevCoreValidityArtifacts {
		if _, err := os.Stat(filepath.Join(runDir, artifact)); err == nil {
			present[artifact] = true
		}
	}
	return present
}

// jevArtifactValidityFromDir derives the artifact validity from the per-repeat
// validation receipts the run actually wrote. A missing receipt set is incomplete,
// which the validity gate then refuses.
func jevArtifactValidityFromDir(runDir string) evalArtifactValidity {
	var summary struct {
		Validity evalArtifactValidity `json:"validity"`
	}
	path := filepath.Join(runDir, evalSummaryArtifactFile)
	if err := readJSON(path, &summary); err != nil {
		return evalArtifactValidity{}
	}
	return summary.Validity
}

// writeJevArmRows writes the measurement journal atomically.
func writeJevArmRows(path string, rows []jevArmQuestionRow) error {
	sort.SliceStable(rows, func(left, right int) bool {
		if rows[left].Conv != rows[right].Conv {
			return rows[left].Conv < rows[right].Conv
		}
		if rows[left].Q != rows[right].Q {
			return rows[left].Q < rows[right].Q
		}
		if rows[left].Repetition != rows[right].Repetition {
			return rows[left].Repetition < rows[right].Repetition
		}
		return rows[left].Arm < rows[right].Arm
	})
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	encoder := json.NewEncoder(tmp)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

// mergedJevArmVerdict merges the pass just written with the complementary pass's
// verdict when both exist in the run directory. It returns (nil, nil) when the
// counterpart has not been run yet; two passes under different registrations are
// an error rather than a silent skip, because the merged verdict is what a
// promotion decision cites and a drifted pair measures nothing.
func mergedJevArmVerdict(prior *jevArmVerdict, current jevArmVerdict) (*jevArmVerdict, error) {
	if prior == nil || prior.Pass == current.Pass {
		return nil, nil
	}
	if prior.RegistrationDigest != current.RegistrationDigest {
		return nil, fmt.Errorf("refusing to merge the %s and %s passes: their filter registrations differ (%s vs %s), so they were not run under one pre-registration", prior.Pass, current.Pass, prior.RegistrationDigest, current.RegistrationDigest)
	}
	gated, degraded := *prior, current
	if gated.Pass != jevArmPassGated {
		gated, degraded = current, *prior
	}
	if gated.Pass != jevArmPassGated || degraded.Pass != jevArmPassDegraded {
		return nil, nil
	}
	merged, err := mergeJevArmVerdicts(gated, degraded)
	if err != nil {
		return nil, err
	}
	return &merged, nil
}

// readJevArmVerdict reads a previously written verdict, tolerating absence: the
// operator may be running the first pass.
func readJevArmVerdict(path string) *jevArmVerdict {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return nil
	}
	var verdict jevArmVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return nil
	}
	return &verdict
}

// mergeJevArmVerdicts combines the gated and degraded passes into the verdict a
// promotion decision cites. Each criterion is taken from the pass that measured
// it; the merged verdict is HOLD if any criterion is unmet.
func mergeJevArmVerdicts(gated, degraded jevArmVerdict) (jevArmVerdict, error) {
	if gated.RegistrationDigest != degraded.RegistrationDigest {
		return jevArmVerdict{}, fmt.Errorf("cannot merge verdicts from different filter registrations (%s vs %s)", gated.RegistrationDigest, degraded.RegistrationDigest)
	}
	if gated.Pass == degraded.Pass {
		return jevArmVerdict{}, fmt.Errorf("cannot merge two %s passes", gated.Pass)
	}
	// "declared" is deliberately not collected as measured evidence: only a pass
	// from the pass that actually measured the criterion is.
	measured := map[string]jevSuccessCriterion{}
	for _, criterion := range gated.Criteria {
		if criterion.Status == jevCriterionPass {
			measured[criterion.ID] = criterion
		}
	}
	for _, criterion := range degraded.Criteria {
		if criterion.Status == jevCriterionPass {
			measured[criterion.ID] = criterion
		}
	}
	merged := gated
	merged.Pass = "merged"
	merged.Criteria = nil
	unmeasured := make([]string, 0, len(gated.Criteria))
	for _, criterion := range gated.Criteria {
		if candidate, ok := measured[criterion.ID]; ok {
			merged.Criteria = append(merged.Criteria, candidate)
			continue
		}
		unmeasured = append(unmeasured, criterion.ID)
		merged.Criteria = append(merged.Criteria, jevSuccessCriterion{
			ID:     criterion.ID,
			Status: jevCriterionHold,
			Detail: "unmeasured in both passes: run the gated pass and the degraded pass under the same registration",
		})
	}
	sort.Slice(merged.Criteria, func(left, right int) bool { return merged.Criteria[left].ID < merged.Criteria[right].ID })
	merged.Verdict = jevVerdictFor(merged.Criteria)
	merged.Notes = append(merged.Notes, fmt.Sprintf("merged gated+degraded passes; unmeasured criteria: %v", unmeasured))
	if gated.ValidityError != "" {
		merged.ValidityError = gated.ValidityError
	}
	return merged, nil
}
