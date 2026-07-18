package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSummarizeRunsUsesTDistributionCI(t *testing.T) {
	got := summarize([]float64{0.5, 0.75, 1.0, 0.75, 0.5})
	if math.Abs(got.Mean-0.7) > 1e-12 {
		t.Fatalf("mean = %.12f, want 0.7", got.Mean)
	}
	if math.Abs(got.CI95[0]-0.4403) > 0.002 || math.Abs(got.CI95[1]-0.9597) > 0.002 {
		t.Fatalf("ci95 = [%.4f, %.4f], want about [0.4403, 0.9597]", got.CI95[0], got.CI95[1])
	}
	if got.N != 5 {
		t.Fatalf("n = %d, want 5", got.N)
	}
}

func TestMcNemarUsesExactSmallSamplePath(t *testing.T) {
	a := []bool{true, false, false, false, false}
	b := []bool{false, true, true, true, false}
	got := mcnemar(a, b)
	if got.AToB != 1 || got.BToA != 3 {
		t.Fatalf("discordant counts = %d/%d, want 1/3", got.AToB, got.BToA)
	}
	if math.Abs(got.PValue-0.625) > 1e-12 {
		t.Fatalf("exact p = %.12f, want 0.625", got.PValue)
	}
}

func TestCompareVerdictUsesEitherNoiseCriterion(t *testing.T) {
	a := metricSummary{CI95: [2]float64{0.40, 0.60}}
	b := metricSummary{CI95: [2]float64{0.61, 0.80}}
	if !aboveNoise(a, b, 0.20) {
		t.Fatal("non-overlapping CIs should be above noise")
	}
	if aboveNoise(a, metricSummary{CI95: [2]float64{0.55, 0.75}}, 0.20) {
		t.Fatal("overlapping CIs with non-significant p should be within noise")
	}
	if !aboveNoise(a, metricSummary{CI95: [2]float64{0.55, 0.75}}, 0.01) {
		t.Fatal("significant paired p should be above noise")
	}
}

func TestCompareRunDirsAlignsQuestionsAndWritesFlipCounts(t *testing.T) {
	aDir := t.TempDir()
	bDir := t.TempDir()
	a := "{" + `"question_id":"q1","category_name":"single-hop","correct":true` + "}\n" +
		"{" + `"question_id":"q2","category_name":"single-hop","correct":false` + "}\n" +
		"{" + `"question_id":"q3","category_name":"single-hop","correct":false` + "}\n"
	b := "{" + `"question_id":"q1","category_name":"single-hop","correct":false` + "}\n" +
		"{" + `"question_id":"q2","category_name":"single-hop","correct":true` + "}\n" +
		"{" + `"question_id":"q3","category_name":"single-hop","correct":true` + "}\n"
	if err := os.WriteFile(filepath.Join(aDir, "results-fts.jsonl"), []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bDir, "results-fts.jsonl"), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := compareRunDirs(aDir, bDir)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if report.FlipsAToB != 2 || report.FlipsBToA != 1 || len(report.Questions) != 3 {
		t.Fatalf("compare report = %+v", report)
	}
	if report.Verdict != "above-noise" {
		t.Fatalf("verdict = %q, want above-noise", report.Verdict)
	}
}
