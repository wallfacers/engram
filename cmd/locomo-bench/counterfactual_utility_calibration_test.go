package main

// T019 [P] [US2] deterministic scaler/ridge/threshold tests: zero-variance
// features, fixed lambda, strict score > threshold, never/always sentinels, AUC,
// lexicographic tie-breaks, class-absence errors, and numerical failure.

import (
	"math"
	"testing"
)

func TestUtilityFitScaler(t *testing.T) {
	// feature 0 constant (zero variance), features 1/2 varying.
	rows := [][]float64{
		{1.0, 0.0, 2.0},
		{1.0, 2.0, 4.0},
		{1.0, 4.0, 6.0},
	}
	sc, err := utilityFitScaler(rows)
	if err != nil {
		t.Fatalf("fit scaler: %v", err)
	}
	if !sc.ZeroVariance[0] {
		t.Fatal("feature 0 must be flagged zero-variance")
	}
	if sc.ZeroVariance[1] || sc.ZeroVariance[2] {
		t.Fatal("features 1/2 must not be zero-variance")
	}
	// mean of feature 1 = 2, population std = sqrt(((0-2)^2+(2-2)^2+(4-2)^2)/3)=sqrt(8/3)
	if math.Abs(sc.Means[1]-2.0) > 1e-9 {
		t.Fatalf("feature1 mean = %v, want 2", sc.Means[1])
	}
	if math.Abs(sc.PopulationStddevs[1]-math.Sqrt(8.0/3.0)) > 1e-9 {
		t.Fatalf("feature1 std = %v, want sqrt(8/3)", sc.PopulationStddevs[1])
	}
	// Zero variance std is treated as 0 and its z is fixed to 0; a value one
	// population std above the mean maps to exactly 1.
	std12 := math.Sqrt(8.0 / 3.0)
	z, err := utilityScaleFeatures([]float64{1.0, 2.0 + std12, 4.0 + std12}, sc)
	if err != nil {
		t.Fatal(err)
	}
	if z[0] != 0 {
		t.Fatalf("zero-variance z = %v, want 0", z[0])
	}
	if math.Abs(z[1]-1.0) > 1e-9 || math.Abs(z[2]-1.0) > 1e-9 {
		t.Fatalf("z = %v, want [0,1,1]", z)
	}
	// Non-finite input must fail.
	if _, err := utilityScaleFeatures([]float64{math.Inf(1), 0, 0}, sc); err == nil {
		t.Fatal("non-finite scaling must fail")
	}
}

func TestUtilityRidgeSolveDeterministicNormalEquations(t *testing.T) {
	Z := [][]float64{{1, 0, 0}, {0, 1, 0}, {1, 1, 0}, {0, 0, 1}, {-1, 0, 1}}
	y := []float64{1, 0, 1, -1, 1}
	lambda := 1.0

	s1, err := utilityRidgeSolve(Z, y, lambda)
	if err != nil {
		t.Fatalf("ridge solve: %v", err)
	}
	s2, err := utilityRidgeSolve(Z, y, lambda)
	if err != nil {
		t.Fatalf("ridge solve: %v", err)
	}
	// Deterministic replay.
	if s1.Intercept != s2.Intercept || !floatClose(s1.Coefficients, s2.Coefficients) {
		t.Fatal("ridge solve not deterministic")
	}
	// Intercept is the uncentered target mean.
	mean := (1.0 + 0.0 + 1.0 - 1.0 + 1.0) / 5.0
	if math.Abs(s1.Intercept-mean) > 1e-9 {
		t.Fatalf("intercept = %v, want %v", s1.Intercept, mean)
	}
	// Normal equations: (Z^T Z + I) beta = Z^T (y - mean).
	yc := make([]float64, len(y))
	for i := range y {
		yc[i] = y[i] - mean
	}
	ztz := utilityGramMatrix(Z)
	for i := range ztz {
		ztz[i][i] += lambda
	}
	ztY := utilityGramVector(Z, yc)
	// A * beta == ztY
	lhs := utilityMatVec(ztz, s1.Coefficients)
	if !floatClose(lhs, ztY) {
		t.Fatalf("normal equations violated: lhs=%v rhs=%v", lhs, ztY)
	}
}

func TestUtilityRidgeSolveZeroVarianceStability(t *testing.T) {
	// A zero column must still produce a finite deterministic solution.
	Z := [][]float64{{0, 1, 0}, {0, 0, 1}, {0, 1, 1}}
	y := []float64{1, -1, 0}
	s, err := utilityRidgeSolve(Z, y, utilityRidgeLambda)
	if err != nil {
		t.Fatalf("ridge solve with zero column: %v", err)
	}
	if math.Abs(s.Coefficients[0]) > 1e-9 {
		t.Fatalf("zero-variance coefficient = %v, want 0", s.Coefficients[0])
	}
	for _, v := range s.Coefficients {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("non-finite coefficient: %v", s.Coefficients)
		}
	}
	// Empty training rows is a numerical failure.
	if _, err := utilityRidgeSolve(nil, nil, utilityRidgeLambda); err == nil {
		t.Fatal("empty ridge input must fail")
	}
	// Singular-ish but regularized still solves.
	if _, err := utilityRidgeSolve([][]float64{{1, 1, 1}, {1, 1, 1}}, []float64{1, -1}, utilityRidgeLambda); err != nil {
		t.Fatalf("regularized singular solve must succeed: %v", err)
	}
}

func TestUtilityRuleScoreStrictThreshold(t *testing.T) {
	rule := utilityCalibratedRule{
		Scaler: utilityFeatureScaler{
			FeatureNames:      []string{"f0", "f1", "f2"},
			Means:             []float64{0, 0, 0},
			PopulationStddevs: []float64{1, 1, 1},
			ZeroVariance:      []bool{false, false, false},
		},
		Intercept:    0.0,
		Coefficients: []float64{1, 0, 0},
		Threshold:    utilityRuleThreshold{Kind: utilityThresholdFinite, Value: 0.5},
	}
	// score > threshold => deepen; score == threshold => keep (strict).
	if action, _, err := utilityApplyRule(rule, []float64{1.0, 0, 0}); err != nil || action != utilityActionDeepen {
		t.Fatalf("score 1.0 > 0.5 should deepen, got %s err=%v", action, err)
	}
	if action, _, err := utilityApplyRule(rule, []float64{0.5, 0, 0}); err != nil || action != utilityActionKeepShallow {
		t.Fatalf("score 0.5 == threshold must keep (strict >), got %s err=%v", action, err)
	}
	// Signal unavailable => forced deep.
	if action, _, err := utilityApplyRule(rule, nil); err != nil || action != utilityActionForcedDeep {
		t.Fatalf("unavailable signal must force deep, got %s err=%v", action, err)
	}
	// never => keep all available; always => deepen all available.
	never := rule
	never.Threshold = utilityRuleThreshold{Kind: utilityThresholdNever}
	if action, _, err := utilityApplyRule(never, []float64{1.0, 0, 0}); err != nil || action != utilityActionKeepShallow {
		t.Fatalf("never must keep, got %s err=%v", action, err)
	}
	always := rule
	always.Threshold = utilityRuleThreshold{Kind: utilityThresholdAlways}
	if action, _, err := utilityApplyRule(always, []float64{-100, 0, 0}); err != nil || action != utilityActionDeepen {
		t.Fatalf("always must deepen, got %s err=%v", action, err)
	}
}

func TestUtilityThresholdCandidates(t *testing.T) {
	scores := []float64{0.1, 0.4, 0.4, 0.9}
	cands := utilityThresholdCandidates(scores)
	// adjacent midpoints (unique sorted scores) + never + always.
	uniq := []float64{0.1, 0.4, 0.9}
	mid := (0.1 + 0.4) / 2         // 0.25
	mid2 := (0.4 + 0.9) / 2        // 0.65
	if len(cands) != len(uniq)+1 { // uniq-1 midpoints + never + always
		t.Fatalf("candidate count = %d, want %d", len(cands), 3)
	}
	got := map[float64]bool{}
	for _, c := range cands {
		got[c.Value] = true
	}
	if !got[mid] || !got[mid2] {
		t.Fatalf("midpoints missing: %v", cands)
	}
	hasNever, hasAlways := false, false
	for _, c := range cands {
		if c.Kind == utilityThresholdNever {
			hasNever = true
		}
		if c.Kind == utilityThresholdAlways {
			hasAlways = true
		}
	}
	if !hasNever || !hasAlways {
		t.Fatalf("never/always sentinels missing")
	}
}

func TestUtilityAUROC(t *testing.T) {
	// Perfect separation: positives score higher than negatives.
	scores := []float64{0.1, 0.2, 0.8, 0.9}
	pos := []bool{false, false, true, true}
	auc, err := utilityAUROC(scores, pos)
	if err != nil {
		t.Fatalf("auc: %v", err)
	}
	if math.Abs(auc-1.0) > 1e-9 {
		t.Fatalf("perfect AUC = %v, want 1", auc)
	}
	// Interleaved scores give exactly 0.5 (no ordering information).
	scoresR := []float64{1, 2, 3, 4}
	posR := []bool{false, true, true, false}
	aucR, err := utilityAUROC(scoresR, posR)
	if err != nil {
		t.Fatalf("auc: %v", err)
	}
	if math.Abs(aucR-0.5) > 1e-9 {
		t.Fatalf("interleaved AUC = %v, want 0.5", aucR)
	}
	// Inverted ordering gives 0.0.
	scoresI := []float64{1, 2, 3, 4}
	posI := []bool{true, false, false, false}
	aucI, err := utilityAUROC(scoresI, posI)
	if err != nil {
		t.Fatalf("auc: %v", err)
	}
	if math.Abs(aucI) > 1e-9 {
		t.Fatalf("inverted AUC = %v, want 0", aucI)
	}
	// Missing a class fails closed (pilot_class_missing).
	if _, err := utilityAUROC([]float64{1, 2}, []bool{true, true}); err == nil {
		t.Fatal("missing negatives must error")
	}
	if _, err := utilityAUROC([]float64{1, 2}, []bool{false, false}); err == nil {
		t.Fatal("missing positives must error")
	}
}

func floatClose(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-6 {
			return false
		}
	}
	return true
}
