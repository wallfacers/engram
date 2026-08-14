package main

// 042 calibration: fixed three-variable ridge utility regression, training-only
// feature scaling, deterministic threshold selection, and AUC for the
// signal-existence pilot. No third-party ML dependency, no model search, no
// held-out label access during rule construction (research.md Decisions 4/5).

import (
	"fmt"
	"math"
	"sort"
)

// utilityRidgeSolution is the deterministic least-squares result for a fold.
type utilityRidgeSolution struct {
	Intercept    float64
	Coefficients []float64
}

// utilityFitScaler computes training-only population mean/std per feature.
// A feature with zero population variance is flagged and its z/coefficient are
// fixed to 0 downstream.
func utilityFitScaler(features [][]float64) (utilityFeatureScaler, error) {
	n := len(features)
	if n == 0 {
		return utilityFeatureScaler{}, fmt.Errorf("no training rows to fit scaler")
	}
	p := len(features[0])
	if p != 3 {
		return utilityFeatureScaler{}, fmt.Errorf("expected 3 routing features, got %d", p)
	}
	sc := utilityFeatureScaler{FeatureNames: append([]string(nil), utilityRoutingFeatureNames...), TrainingAvailableRows: n}
	means := make([]float64, p)
	for _, row := range features {
		for j, v := range row {
			means[j] += v
		}
	}
	for j := range means {
		means[j] /= float64(n)
	}
	stddevs := make([]float64, p)
	for _, row := range features {
		for j, v := range row {
			d := v - means[j]
			stddevs[j] += d * d
		}
	}
	zeroVar := make([]bool, p)
	for j := range stddevs {
		stddevs[j] = math.Sqrt(stddevs[j] / float64(n))
		if stddevs[j] == 0 || math.IsNaN(stddevs[j]) || math.IsInf(stddevs[j], 0) {
			zeroVar[j] = true
			stddevs[j] = 0
		}
	}
	sc.Means = means
	sc.PopulationStddevs = stddevs
	sc.ZeroVariance = zeroVar
	return sc, nil
}

// utilityScaleFeatures standardizes a feature vector with training statistics.
// Zero-variance features map to exactly 0.
func utilityScaleFeatures(features []float64, sc utilityFeatureScaler) ([]float64, error) {
	if len(features) != 3 || len(sc.Means) != 3 || len(sc.PopulationStddevs) != 3 {
		return nil, fmt.Errorf("feature/scaler dimension mismatch")
	}
	for _, v := range features {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("non-finite feature in scaling")
		}
	}
	z := make([]float64, 3)
	for j := range features {
		if sc.ZeroVariance[j] || sc.PopulationStddevs[j] == 0 {
			z[j] = 0
			continue
		}
		z[j] = (features[j] - sc.Means[j]) / sc.PopulationStddevs[j]
	}
	return z, nil
}

// utilityGramMatrix computes Z^T Z for an n×p design matrix.
func utilityGramMatrix(Z [][]float64) [][]float64 {
	n := len(Z)
	if n == 0 {
		return nil
	}
	p := len(Z[0])
	g := make([][]float64, p)
	for i := range g {
		g[i] = make([]float64, p)
	}
	for i := range Z {
		for a := 0; a < p; a++ {
			for b := 0; b < p; b++ {
				g[a][b] += Z[i][a] * Z[i][b]
			}
		}
	}
	return g
}

// utilityGramVector computes Z^T y for an n×p design matrix and n-vector y.
func utilityGramVector(Z [][]float64, y []float64) []float64 {
	n := len(Z)
	if n == 0 {
		return nil
	}
	p := len(Z[0])
	out := make([]float64, p)
	for i := 0; i < n; i++ {
		for j := 0; j < p; j++ {
			out[j] += Z[i][j] * y[i]
		}
	}
	return out
}

// utilityMatVec multiplies an n×n matrix by an n-vector.
func utilityMatVec(A [][]float64, x []float64) []float64 {
	n := len(A)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			out[i] += A[i][j] * x[j]
		}
	}
	return out
}

// utilityRidgeSolve solves (Z^T Z + lambda*I) beta = Z^T (y - mean(y)) with
// deterministic Gaussian elimination (partial pivoting). intercept = mean(y) is
// not penalized. Empty input or non-finite solution is a numerical failure.
func utilityRidgeSolve(Z [][]float64, y []float64, lambda float64) (utilityRidgeSolution, error) {
	n := len(Z)
	if n == 0 || len(y) != n {
		return utilityRidgeSolution{}, fmt.Errorf("ridge solve needs matching non-empty rows")
	}
	p := len(Z[0])
	if p == 0 || len(y) == 0 {
		return utilityRidgeSolution{}, fmt.Errorf("ridge solve needs at least one feature")
	}
	mean := 0.0
	for _, v := range y {
		mean += v
	}
	mean /= float64(n)
	yc := make([]float64, n)
	for i := range y {
		yc[i] = y[i] - mean
	}
	A := utilityGramMatrix(Z)
	for i := 0; i < p; i++ {
		A[i][i] += lambda
	}
	b := utilityGramVector(Z, yc)

	// Gaussian elimination with partial pivoting.
	aug := make([][]float64, p)
	for i := range aug {
		aug[i] = make([]float64, p+1)
		copy(aug[i], A[i])
		aug[i][p] = b[i]
	}
	for col := 0; col < p; col++ {
		pivot := col
		for r := col + 1; r < p; r++ {
			if math.Abs(aug[r][col]) > math.Abs(aug[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(aug[pivot][col]) < 1e-15 {
			return utilityRidgeSolution{}, fmt.Errorf("ridge solve: singular system after regularization")
		}
		aug[col], aug[pivot] = aug[pivot], aug[col]
		for r := col + 1; r < p; r++ {
			factor := aug[r][col] / aug[col][col]
			if factor == 0 {
				continue
			}
			for c := col; c <= p; c++ {
				aug[r][c] -= factor * aug[col][c]
			}
		}
	}
	coef := make([]float64, p)
	for i := p - 1; i >= 0; i-- {
		sum := aug[i][p]
		for j := i + 1; j < p; j++ {
			sum -= aug[i][j] * coef[j]
		}
		coef[i] = sum / aug[i][i]
	}
	for _, v := range coef {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return utilityRidgeSolution{}, fmt.Errorf("ridge solve: non-finite coefficient")
		}
	}
	return utilityRidgeSolution{Intercept: mean, Coefficients: coef}, nil
}

// utilityRuleScore computes intercept + coef·z from the standardized features.
func utilityRuleScore(coef []float64, intercept float64, z []float64) float64 {
	score := intercept
	for i := range z {
		score += coef[i] * z[i]
	}
	return score
}

// utilityApplyRule is the frozen decision function: unavailable signal forces
// deep; available uses strict score > threshold (equal keeps shallow); never
// keeps all available; always deepens all available.
func utilityApplyRule(rule utilityCalibratedRule, features []float64) (utilityDecisionAction, float64, error) {
	if features == nil {
		return utilityActionForcedDeep, 0, nil
	}
	z, err := utilityScaleFeatures(features, rule.Scaler)
	if err != nil {
		return "", 0, err
	}
	score := utilityRuleScore(rule.Coefficients, rule.Intercept, z)
	switch rule.Threshold.Kind {
	case utilityThresholdNever:
		return utilityActionKeepShallow, score, nil
	case utilityThresholdAlways:
		return utilityActionDeepen, score, nil
	case utilityThresholdFinite:
		if score > rule.Threshold.Value {
			return utilityActionDeepen, score, nil
		}
		return utilityActionKeepShallow, score, nil
	}
	return "", score, fmt.Errorf("unknown threshold kind %q", rule.Threshold.Kind)
}

// utilityThresholdCandidates enumerates unique adjacent midpoints of the
// training scores plus the never (keep-all) and always (deepen-all) sentinels.
func utilityThresholdCandidates(scores []float64) []utilityRuleThreshold {
	uniq := append([]float64(nil), scores...)
	sort.Float64s(uniq)
	out := make([]float64, 0, len(uniq))
	for i, v := range uniq {
		if i == 0 || v != uniq[i-1] {
			out = append(out, v)
		}
	}
	cands := []utilityRuleThreshold{{Kind: utilityThresholdNever}}
	for i := 0; i+1 < len(out); i++ {
		mid := (out[i] + out[i+1]) / 2
		cands = append(cands, utilityRuleThreshold{Kind: utilityThresholdFinite, Value: mid})
	}
	cands = append(cands, utilityRuleThreshold{Kind: utilityThresholdAlways})
	return cands
}

// utilityAUROC computes the area under the ROC curve via the Mann-Whitney U
// statistic with average ranks for ties. Missing either class fails closed.
func utilityAUROC(scores []float64, positive []bool) (float64, error) {
	if len(scores) != len(positive) || len(scores) == 0 {
		return 0, fmt.Errorf("auc needs matching non-empty scores/labels")
	}
	nPos, nNeg := 0, 0
	for _, p := range positive {
		if p {
			nPos++
		} else {
			nNeg++
		}
	}
	if nPos == 0 || nNeg == 0 {
		return 0, fmt.Errorf("auc undefined: missing a class (pos=%d neg=%d)", nPos, nNeg)
	}
	type scored struct {
		score float64
		pos   bool
	}
	items := make([]scored, len(scores))
	for i := range scores {
		items[i] = scored{scores[i], positive[i]}
	}
	sort.Slice(items, func(a, b int) bool { return items[a].score < items[b].score })
	ranks := make([]float64, len(items))
	for i := 0; i < len(items); {
		j := i
		for j+1 < len(items) && items[j+1].score == items[i].score {
			j++
		}
		avg := float64(i+j)/2 + 1 // 1-based average rank
		for k := i; k <= j; k++ {
			ranks[k] = avg
		}
		i = j + 1
	}
	sumPosRanks := 0.0
	for i := range items {
		if items[i].pos {
			sumPosRanks += ranks[i]
		}
	}
	auc := (sumPosRanks - float64(nPos)*(float64(nPos)+1)/2) / (float64(nPos) * float64(nNeg))
	if auc < 0 {
		auc = 0
	}
	if auc > 1 {
		auc = 1
	}
	return auc, nil
}
