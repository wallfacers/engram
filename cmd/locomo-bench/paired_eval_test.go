package main

import (
	"math"
	"testing"
)

func TestPairedEvaluationMajorityExactMcNemarAndConfidenceInterval(t *testing.T) {
	if got, err := majorityCorrectness([]bool{true, false, true}); err != nil || !got {
		t.Fatalf("majority = %t, %v; want true, nil", got, err)
	}
	if _, err := majorityCorrectness([]bool{true, false}); err == nil {
		t.Fatal("tied repetition count unexpectedly accepted")
	}

	p, err := exactMcNemarTwoSided(0, 30)
	if err != nil {
		t.Fatalf("exact McNemar: %v", err)
	}
	if want := 2 / math.Pow(2, 30); math.Abs(p-want) > 1e-15 {
		t.Fatalf("exact p = %.18f, want %.18f", p, want)
	}
	if p, err := exactMcNemarTwoSided(11, 11); err != nil || p != 1 {
		t.Fatalf("balanced discordance p = %v, %v; want 1, nil", p, err)
	}
	if _, err := exactMcNemarTwoSided(-1, 0); err == nil {
		t.Fatal("negative discordant count unexpectedly accepted")
	}

	ci, err := pairedDeltaConfidenceInterval(
		[]bool{true, true, false, false, true},
		[]bool{true, false, true, true, true},
	)
	if err != nil {
		t.Fatalf("paired delta CI: %v", err)
	}
	if ci.Lower > ci.DeltaPP || ci.DeltaPP > ci.Upper {
		t.Fatalf("CI %+v does not contain delta", ci)
	}
}

func TestPairedEvaluationHolmCategoryGateAndPromotionVerdict(t *testing.T) {
	category := holmNegativeCategoryGate([]evalCategoryComparison{
		{Category: "temporal", DeltaPP: -3.0, PValue: 0.010},
		{Category: "multi_hop", DeltaPP: -1.0, PValue: 0.060},
		{Category: "single_hop", DeltaPP: 2.0, PValue: 0.001},
	}, 0.05)
	if !category["temporal"].HolmSignificantNegative {
		t.Fatalf("temporal negative regression not Holm-rejected: %#v", category)
	}
	if category["multi_hop"].HolmSignificantNegative {
		t.Fatalf("multi-hop negative regression should survive Holm correction: %#v", category)
	}
	if category["single_hop"].HolmSignificantNegative {
		t.Fatalf("positive category marked as harmful: %#v", category)
	}

	base := evalPromotionInput{
		Validity:                       evalArtifactValidity{Valid: true, Complete: true, CandidateIdentityRate: 1, SourceValidationRate: 1, SpanRecoveryRate: 1, CitationCoverageRate: 1, WithinCapRate: 1, AnswerCallComplianceRate: 1},
		PrimaryDeltaPP:                 2.1,
		PrimaryMcNemarP:                0.01,
		OtherBenchmarkDeltaPP:          -0.4,
		CandidateCoverageNonRegression: true,
		JudgeAuditComplete:             true,
		JudgeAuditVerdictStable:        true,
		OfflineCompatible:              true,
		CategoryResults:                holmNegativeCategoryGate([]evalCategoryComparison{{Category: "temporal", DeltaPP: -0.1, PValue: 0.8}}, 0.05),
	}
	if got := promotionVerdictFor(base); got != evalVerdictGO {
		t.Fatalf("promotion verdict = %q, want GO", got)
	}

	hold := base
	hold.PrimaryDeltaPP = 1.0
	hold.PrimaryMcNemarP = 0.2
	if got := promotionVerdictFor(hold); got != evalVerdictHOLD {
		t.Fatalf("weak positive verdict = %q, want HOLD", got)
	}

	stop := base
	stop.PrimaryDeltaPP = 0
	if got := promotionVerdictFor(stop); got != evalVerdictSTOP {
		t.Fatalf("non-positive verdict = %q, want STOP", got)
	}

	invalid := base
	invalid.Validity.WithinCapRate = 0.99
	if got := promotionVerdictFor(invalid); got != evalVerdictInvalid {
		t.Fatalf("invalid artifact verdict = %q, want INVALID", got)
	}
}
