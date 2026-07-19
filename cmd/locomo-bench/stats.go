package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// metricSummary is a run-level accuracy summary. Mean and CI95 are fractions,
// not percentages, so the JSON report matches the bench-cli contract.
type metricSummary struct {
	Mean       float64    `json:"mean"`
	CI95       [2]float64 `json:"ci95"`
	N          int        `json:"-"`
	NQuestions int        `json:"n_questions,omitempty"`
}

type statsReport struct {
	Repeats           int                      `json:"repeats"`
	Categories        map[string]metricSummary `json:"categories"`
	Overall           metricSummary            `json:"overall"`
	OverallComparable metricSummary            `json:"overall_comparable"`
}

// summarize computes a two-sided 95% confidence interval using the sample
// standard deviation and a small built-in Student-t critical-value table.
func summarize(values []float64) metricSummary {
	if len(values) == 0 {
		return metricSummary{}
	}
	var mean float64
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	if len(values) == 1 {
		return metricSummary{Mean: mean, CI95: [2]float64{mean, mean}, N: 1}
	}
	var sumSquares float64
	for _, value := range values {
		d := value - mean
		sumSquares += d * d
	}
	sd := math.Sqrt(sumSquares / float64(len(values)-1))
	margin := tCritical95(len(values)-1) * sd / math.Sqrt(float64(len(values)))
	return metricSummary{Mean: mean, CI95: [2]float64{mean - margin, mean + margin}, N: len(values)}
}

var tCritical95Table = map[int]float64{
	1: 12.706, 2: 4.303, 3: 3.182, 4: 2.776, 5: 2.571,
	6: 2.447, 7: 2.365, 8: 2.306, 9: 2.262, 10: 2.228,
	11: 2.201, 12: 2.179, 13: 2.160, 14: 2.145, 15: 2.131,
	16: 2.120, 17: 2.110, 18: 2.101, 19: 2.093, 20: 2.086,
	21: 2.080, 22: 2.074, 23: 2.069, 24: 2.064, 25: 2.060,
	26: 2.056, 27: 2.052, 28: 2.048, 29: 2.045, 30: 2.042,
	40: 2.021, 60: 2.000, 120: 1.980,
}

func tCritical95(df int) float64 {
	if df <= 0 {
		return 0
	}
	if value, ok := tCritical95Table[df]; ok {
		return value
	}
	for _, threshold := range []int{30, 40, 60, 120} {
		if df < threshold {
			return tCritical95Table[threshold]
		}
	}
	return 1.962
}

type mcnemarResult struct {
	AToB   int     `json:"flips_a_to_b"`
	BToA   int     `json:"flips_b_to_a"`
	PValue float64 `json:"mcnemar_p"`
}

// mcnemar returns the paired two-sided McNemar p-value. Small discordant
// samples use an exact binomial test; larger samples use the continuity-
// corrected chi-square approximation.
func mcnemar(a, b []bool) mcnemarResult {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	result := mcnemarResult{}
	for i := 0; i < n; i++ {
		switch {
		case a[i] && !b[i]:
			result.AToB++
		case !a[i] && b[i]:
			result.BToA++
		}
	}
	discordant := result.AToB + result.BToA
	if discordant == 0 {
		result.PValue = 1
		return result
	}
	if discordant < 25 {
		result.PValue = exactBinomialTwoSided(discordant, result.AToB)
		return result
	}
	delta := math.Abs(float64(result.AToB-result.BToA)) - 1
	stat := delta * delta / float64(discordant)
	result.PValue = math.Erfc(math.Sqrt(stat / 2))
	return result
}

func exactBinomialTwoSided(n, observed int) float64 {
	observedProbability := binomialProbability(n, observed)
	var total float64
	for k := 0; k <= n; k++ {
		if binomialProbability(n, k) <= observedProbability+1e-15 {
			total += binomialProbability(n, k)
		}
	}
	if total > 1 {
		return 1
	}
	return total
}

func binomialProbability(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	probability := 1.0
	for i := 1; i <= k; i++ {
		probability *= float64(n-k+i) / float64(i)
	}
	return probability / math.Pow(2, float64(n))
}

func ciOverlap(a, b metricSummary) bool {
	return a.CI95[0] <= b.CI95[1] && b.CI95[0] <= a.CI95[1]
}

func aboveNoise(a, b metricSummary, pValue float64) bool {
	if pValue < 0.05 {
		return true
	}
	return a.N >= 2 && b.N >= 2 && !ciOverlap(a, b)
}

// statsFromRuns computes per-category and overall summaries from repeated
// result sets. The ordinary overall excludes adversarial questions; the
// comparable overall includes them under the refusal-scoring convention.
func statsFromRuns(runs [][]result) statsReport {
	report := statsReport{Repeats: len(runs), Categories: map[string]metricSummary{}}
	categoryRates := map[string][]float64{}
	allRates := make([]float64, 0, len(runs))
	comparableRates := make([]float64, 0, len(runs))
	for _, run := range runs {
		byCategory := map[string][]bool{}
		var ordinary, comparable []bool
		for _, item := range run {
			category := resultCategory(item)
			byCategory[category] = append(byCategory[category], item.Correct)
			comparable = append(comparable, item.Correct)
			if !isAdversarialResult(item) {
				ordinary = append(ordinary, item.Correct)
			}
		}
		for category, outcomes := range byCategory {
			correct := 0
			for _, outcome := range outcomes {
				if outcome {
					correct++
				}
			}
			categoryRates[category] = append(categoryRates[category], ratio(correct, len(outcomes)))
		}
		allRates = append(allRates, rateCount(ordinary))
		comparableRates = append(comparableRates, rateCount(comparable))
	}
	for category, values := range categoryRates {
		summary := summarize(values)
		if len(runs) > 0 {
			for _, item := range runs[0] {
				if resultCategory(item) == category {
					summary.NQuestions++
				}
			}
		}
		report.Categories[category] = summary
	}
	report.Overall = summarize(allRates)
	report.OverallComparable = summarize(comparableRates)
	return report
}

func rateCount(outcomes []bool) float64 {
	correct := 0
	for _, outcome := range outcomes {
		if outcome {
			correct++
		}
	}
	return ratio(correct, len(outcomes))
}

func resultCategory(item result) string {
	if item.CategoryName != "" {
		return item.CategoryName
	}
	return categoryLabel(item.Category)
}

func isAdversarialResult(item result) bool {
	return item.Adversarial || item.Category == adversarialCategory || resultCategory(item) == "abstention"
}

func writeStats(path string, report statsReport) error {
	return writeJSON(path, report)
}

type pairedQuestion struct {
	QuestionID string `json:"question_id"`
	Category   string `json:"category"`
	AMajority  bool   `json:"a_majority"`
	BMajority  bool   `json:"b_majority"`
	Flip       string `json:"flip,omitempty"`
}

type compareReport struct {
	Questions []pairedQuestion `json:"questions"`
	FlipsAToB int              `json:"flips_a_to_b"`
	FlipsBToA int              `json:"flips_b_to_a"`
	McNemarP  float64          `json:"mcnemar_p"`
	CIOverlap bool             `json:"ci_overlap"`
	NA        int              `json:"n_a"`
	NB        int              `json:"n_b"`
	Verdict   string           `json:"verdict"`
}

// compareRunDirs aligns repeated results by stable question_id, takes a
// majority outcome per question, then applies the contract's McNemar/CI rule.
func compareRunDirs(dirA, dirB string) (compareReport, error) {
	runsA, err := loadRunResults(dirA)
	if err != nil {
		return compareReport{}, fmt.Errorf("load compare A: %w", err)
	}
	runsB, err := loadRunResults(dirB)
	if err != nil {
		return compareReport{}, fmt.Errorf("load compare B: %w", err)
	}
	if len(runsA) == 0 || len(runsB) == 0 {
		return compareReport{}, fmt.Errorf("compare requires at least one non-empty run per side (got A=%d B=%d)", len(runsA), len(runsB))
	}
	majA := majorityResults(runsA)
	majB := majorityResults(runsB)
	ids := make([]string, 0, len(majA))
	for id := range majA {
		if _, ok := majB[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return compareReport{}, fmt.Errorf("compare requires at least one aligned question")
	}
	report := compareReport{Questions: make([]pairedQuestion, 0, len(ids))}
	report.NA = len(runsA)
	report.NB = len(runsB)
	aOutcomes := make([]bool, 0, len(ids))
	bOutcomes := make([]bool, 0, len(ids))
	for _, id := range ids {
		a := majA[id]
		b := majB[id]
		question := pairedQuestion{
			QuestionID: id,
			Category:   resultCategory(a),
			AMajority:  a.Correct,
			BMajority:  b.Correct,
		}
		switch {
		case !a.Correct && b.Correct:
			question.Flip = "a-to-b"
			report.FlipsAToB++
		case a.Correct && !b.Correct:
			question.Flip = "b-to-a"
			report.FlipsBToA++
		}
		report.Questions = append(report.Questions, question)
		aOutcomes = append(aOutcomes, a.Correct)
		bOutcomes = append(bOutcomes, b.Correct)
	}
	mcnemarResult := mcnemar(aOutcomes, bOutcomes)
	report.McNemarP = mcnemarResult.PValue
	statsA := statsFromRuns(runsA)
	statsB := statsFromRuns(runsB)
	report.CIOverlap = ciOverlap(statsA.OverallComparable, statsB.OverallComparable)
	if aboveNoise(statsA.OverallComparable, statsB.OverallComparable, report.McNemarP) {
		report.Verdict = "above-noise"
	} else {
		report.Verdict = "within-noise"
	}
	return report, nil
}

func writeCompare(path string, report compareReport) error {
	return writeJSON(path, report)
}

func loadRunResults(dir string) ([][]result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type runEntry struct {
		name string
		path string
	}
	var runDirs []runEntry
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "run-") {
			runDirs = append(runDirs, runEntry{name: entry.Name(), path: filepath.Join(dir, entry.Name())})
		}
	}
	if len(runDirs) == 0 {
		results, err := loadResultFiles(dir)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("no results found in %s", dir)
		}
		return [][]result{results}, nil
	}
	sort.Slice(runDirs, func(i, j int) bool {
		return runNumber(runDirs[i].name) < runNumber(runDirs[j].name)
	})
	runs := make([][]result, 0, len(runDirs))
	for _, run := range runDirs {
		results, err := loadResultFiles(run.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", run.name, err)
		}
		if len(results) > 0 {
			runs = append(runs, results)
		}
	}
	return runs, nil
}

func loadResultFiles(dir string) ([]result, error) {
	canonical := filepath.Join(dir, "results.jsonl")
	_, canonicalErr := os.Stat(canonical)
	if canonicalErr != nil && !os.IsNotExist(canonicalErr) {
		return nil, canonicalErr
	}
	legacy, err := filepath.Glob(filepath.Join(dir, "results-*.jsonl"))
	if err != nil {
		return nil, err
	}
	if canonicalErr == nil && len(legacy) > 0 {
		return nil, fmt.Errorf("ambiguous run directory %s: results.jsonl and arm-specific result files both exist", dir)
	}
	if canonicalErr == nil {
		return readResultsJSONL(canonical)
	}
	if len(legacy) > 1 {
		return nil, fmt.Errorf("ambiguous run directory %s: multiple arm-specific result files exist", dir)
	}
	if len(legacy) == 0 {
		return nil, nil
	}
	return readResultsJSONL(legacy[0])
}

func readResultsJSONL(path string) ([]result, error) {
	var out []result
	if err := scanResultsJSONL(path, func(item result) {
		out = append(out, item)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func majorityResults(runs [][]result) map[string]result {
	type tally struct {
		result
		trueCount int
		total     int
	}
	tallies := map[string]*tally{}
	for _, run := range runs {
		seen := map[string]bool{}
		for _, item := range run {
			id := resultID(item)
			if seen[id] {
				continue
			}
			seen[id] = true
			entry := tallies[id]
			if entry == nil {
				entry = &tally{result: item}
				tallies[id] = entry
			}
			entry.total++
			if item.Correct {
				entry.trueCount++
			}
		}
	}
	out := make(map[string]result, len(tallies))
	for id, tally := range tallies {
		tally.result.Correct = tally.trueCount*2 >= tally.total
		out[id] = tally.result
	}
	return out
}

func resultID(item result) string {
	if item.QuestionID != "" {
		return item.QuestionID
	}
	return questionID(item.Conv, item.Q)
}

func runNumber(name string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(name, "run-"))
	if err != nil {
		return 0
	}
	return n
}
