package main

import (
	"fmt"
	"math"
	"math/big"
	"sort"
)

type evalConfidenceInterval struct {
	Lower   float64 `json:"lower"`
	Upper   float64 `json:"upper"`
	DeltaPP float64 `json:"delta_pp"`
}

func majorityCorrectness(outcomes []bool) (bool, error) {
	if len(outcomes) == 0 || len(outcomes)%2 == 0 {
		return false, fmt.Errorf("majority requires a non-empty odd number of outcomes")
	}
	correct := 0
	for _, outcome := range outcomes {
		if outcome {
			correct++
		}
	}
	return correct > len(outcomes)/2, nil
}

// exactMcNemarTwoSided uses the exact conditional binomial distribution for
// every discordant-pair count. It intentionally never switches to chi-square.
func exactMcNemarTwoSided(controlCorrectTreatmentWrong, controlWrongTreatmentCorrect int) (float64, error) {
	if controlCorrectTreatmentWrong < 0 || controlWrongTreatmentCorrect < 0 {
		return 0, fmt.Errorf("discordant counts must be non-negative")
	}
	n := controlCorrectTreatmentWrong + controlWrongTreatmentCorrect
	if n == 0 || controlCorrectTreatmentWrong == controlWrongTreatmentCorrect {
		return 1, nil
	}
	limit := controlCorrectTreatmentWrong
	if controlWrongTreatmentCorrect < limit {
		limit = controlWrongTreatmentCorrect
	}
	// Sum C(n, k), k=0..limit, using exact integers so underflow cannot turn a
	// meaningful small p-value into zero before the final conversion.
	combination := big.NewInt(1)
	numerator := big.NewInt(1) // k=0
	for k := 0; k < limit; k++ {
		combination.Mul(combination, big.NewInt(int64(n-k)))
		combination.Quo(combination, big.NewInt(int64(k+1)))
		numerator.Add(numerator, combination)
	}
	numerator.Mul(numerator, big.NewInt(2))
	denominator := new(big.Int).Lsh(big.NewInt(1), uint(n))
	if numerator.Cmp(denominator) >= 0 {
		return 1, nil
	}
	p, _ := new(big.Rat).SetFrac(numerator, denominator).Float64()
	return p, nil
}

// pairedDeltaConfidenceInterval is a fixed 95% normal interval over paired
// per-question differences (-1, 0, +1). Promotion uses exact McNemar; this
// interval is descriptive and its method is fixed before any treatment run.
func pairedDeltaConfidenceInterval(control, treatment []bool) (evalConfidenceInterval, error) {
	if len(control) == 0 || len(control) != len(treatment) {
		return evalConfidenceInterval{}, fmt.Errorf("paired confidence interval requires equal non-empty outcomes")
	}
	differences := make([]float64, len(control))
	var mean float64
	for index := range control {
		switch {
		case !control[index] && treatment[index]:
			differences[index] = 1
		case control[index] && !treatment[index]:
			differences[index] = -1
		}
		mean += differences[index]
	}
	mean /= float64(len(differences))
	result := evalConfidenceInterval{DeltaPP: mean * 100, Lower: mean * 100, Upper: mean * 100}
	if len(differences) == 1 {
		return result, nil
	}
	var squared float64
	for _, difference := range differences {
		delta := difference - mean
		squared += delta * delta
	}
	standardError := math.Sqrt(squared/float64(len(differences)-1)) / math.Sqrt(float64(len(differences)))
	margin := 1.96 * standardError * 100
	result.Lower -= margin
	result.Upper += margin
	return result, nil
}

type evalCategoryComparison struct {
	Category string
	DeltaPP  float64
	PValue   float64
}

type evalCategoryGateResult struct {
	Category                string  `json:"category"`
	DeltaPP                 float64 `json:"delta_pp"`
	PValue                  float64 `json:"p_value"`
	HolmThreshold           float64 `json:"holm_threshold"`
	HolmSignificantNegative bool    `json:"holm_significant_negative"`
}

func holmNegativeCategoryGate(comparisons []evalCategoryComparison, alpha float64) map[string]evalCategoryGateResult {
	results := make(map[string]evalCategoryGateResult, len(comparisons))
	negative := make([]evalCategoryComparison, 0, len(comparisons))
	for _, comparison := range comparisons {
		results[comparison.Category] = evalCategoryGateResult{Category: comparison.Category, DeltaPP: comparison.DeltaPP, PValue: comparison.PValue}
		if comparison.DeltaPP < 0 {
			negative = append(negative, comparison)
		}
	}
	sort.Slice(negative, func(left, right int) bool {
		if negative[left].PValue == negative[right].PValue {
			return negative[left].Category < negative[right].Category
		}
		return negative[left].PValue < negative[right].PValue
	})
	canReject := true
	for index, comparison := range negative {
		threshold := alpha / float64(len(negative)-index)
		result := results[comparison.Category]
		result.HolmThreshold = threshold
		if canReject && comparison.PValue <= threshold {
			result.HolmSignificantNegative = true
		} else {
			canReject = false
		}
		results[comparison.Category] = result
	}
	return results
}

type evalArtifactValidity struct {
	Valid                    bool    `json:"valid"`
	Complete                 bool    `json:"complete"`
	CandidateIdentityRate    float64 `json:"candidate_identity_rate"`
	SourceValidationRate     float64 `json:"source_validation_rate"`
	SpanRecoveryRate         float64 `json:"span_recovery_rate"`
	CitationCoverageRate     float64 `json:"citation_coverage_rate"`
	WithinCapRate            float64 `json:"within_cap_rate"`
	AnswerCallComplianceRate float64 `json:"per_instance_answer_call_compliance"`
	UnattributedAddCount     int     `json:"unattributed_add_count"`
}

func (validity evalArtifactValidity) isComplete() bool {
	return validity.Valid && validity.Complete && validity.CandidateIdentityRate == 1 && validity.SourceValidationRate == 1 && validity.SpanRecoveryRate == 1 && validity.CitationCoverageRate == 1 && validity.WithinCapRate == 1 && validity.AnswerCallComplianceRate == 1 && validity.UnattributedAddCount == 0
}

type evalVerdict string

const (
	evalVerdictGO      evalVerdict = "GO"
	evalVerdictHOLD    evalVerdict = "HOLD"
	evalVerdictSTOP    evalVerdict = "STOP"
	evalVerdictInvalid evalVerdict = "INVALID"
)

type evalPromotionInput struct {
	Validity                          evalArtifactValidity
	PrimaryDeltaPP                    float64
	PrimaryMcNemarP                   float64
	OtherBenchmarkDeltaPP             float64
	OtherBenchmarkNegativeSignificant bool
	CandidateCoverageNonRegression    bool
	JudgeAuditComplete                bool
	JudgeAuditVerdictStable           bool
	OfflineCompatible                 bool
	CategoryResults                   map[string]evalCategoryGateResult
}

func promotionVerdictFor(input evalPromotionInput) evalVerdict {
	if !input.Validity.isComplete() || !input.JudgeAuditComplete || !input.JudgeAuditVerdictStable {
		return evalVerdictInvalid
	}
	if !input.OfflineCompatible || !input.CandidateCoverageNonRegression || input.PrimaryDeltaPP <= 0 || input.OtherBenchmarkDeltaPP < -0.5 || input.OtherBenchmarkNegativeSignificant || hasHolmNegativeRegression(input.CategoryResults) {
		return evalVerdictSTOP
	}
	if input.PrimaryDeltaPP >= 2 && input.PrimaryMcNemarP < 0.05 {
		return evalVerdictGO
	}
	return evalVerdictHOLD
}

func hasHolmNegativeRegression(results map[string]evalCategoryGateResult) bool {
	for _, result := range results {
		if result.HolmSignificantNegative {
			return true
		}
	}
	return false
}
