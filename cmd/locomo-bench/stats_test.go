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
	a := metricSummary{CI95: [2]float64{0.40, 0.60}, N: 2}
	b := metricSummary{CI95: [2]float64{0.61, 0.80}, N: 2}
	if !aboveNoise(a, b, 0.20) {
		t.Fatal("non-overlapping CIs should be above noise")
	}
	if aboveNoise(a, metricSummary{CI95: [2]float64{0.55, 0.75}, N: 2}, 0.20) {
		t.Fatal("overlapping CIs with non-significant p should be within noise")
	}
	if !aboveNoise(a, metricSummary{CI95: [2]float64{0.55, 0.75}, N: 2}, 0.01) {
		t.Fatal("significant paired p should be above noise")
	}
}

func TestSingleRunDifferenceWithOneFlipStaysWithinNoise(t *testing.T) {
	aDir := t.TempDir()
	bDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(aDir, "results-fts.jsonl"), []byte(
		`{"question_id":"q1","correct":true}`+"\n"+`{"question_id":"q2","correct":false}`+"\n"+`{"question_id":"q3","correct":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bDir, "results-fts.jsonl"), []byte(
		`{"question_id":"q1","correct":true}`+"\n"+`{"question_id":"q2","correct":true}`+"\n"+`{"question_id":"q3","correct":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := compareRunDirs(aDir, bDir)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if report.Verdict != "within-noise" {
		t.Fatalf("verdict = %q, want within-noise (p=1, single runs)", report.Verdict)
	}
	if report.NA != 1 || report.NB != 1 {
		t.Fatalf("run counts = %d/%d, want 1/1", report.NA, report.NB)
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
	for _, dir := range []string{aDir, bDir} {
		for _, run := range []string{"run-1", "run-2"} {
			if err := os.Mkdir(filepath.Join(dir, run), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, run := range []string{"run-1", "run-2"} {
		if err := os.WriteFile(filepath.Join(aDir, run, "results-fts.jsonl"), []byte(a), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bDir, run, "results-fts.jsonl"), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
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

func TestLoadResultFilesRejectsMixedArmFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"results.jsonl", "results-fts.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"question_id":"q1","correct":true}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadResultFiles(dir); err == nil {
		t.Fatal("mixed canonical and arm result files should be rejected")
	}

	items, err := loadResultFiles(func() string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "results-fts.jsonl"), []byte(`{"question_id":"q1","correct":true}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}())
	if err != nil || len(items) != 1 {
		t.Fatalf("single-arm results = %d, err=%v; want one item", len(items), err)
	}

	armDir := t.TempDir()
	for _, name := range []string{"results-fts.jsonl", "results-hybrid.jsonl"} {
		if err := os.WriteFile(filepath.Join(armDir, name), []byte(`{"question_id":"q1","correct":true}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadResultFiles(armDir); err == nil {
		t.Fatal("two arm-specific result files should be rejected without an arm selector")
	}
}

func TestCompareRunDirsRejectsEmptyAndUnalignedRuns(t *testing.T) {
	if _, err := compareRunDirs(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("empty compare should fail")
	}
	aDir, bDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(aDir, "results-fts.jsonl"), []byte(`{"question_id":"a","correct":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bDir, "results-fts.jsonl"), []byte(`{"question_id":"b","correct":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compareRunDirs(aDir, bDir); err == nil {
		t.Fatal("compare without aligned questions should fail")
	}
}
