package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoEvalsDir resolves the repo's skill evals directory relative to this test
// file so the real shipped dataset is validated in CI.
func repoEvalsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "skills", "engram", "evals")
}

func TestLoadAndValidateShippedDatasets(t *testing.T) {
	ds, err := LoadDatasets(repoEvalsDir(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(ds["regression"].Cases) < 20 {
		t.Errorf("regression module too small: %d", len(ds["regression"].Cases))
	}
	rep := ValidateDatasets(ds)
	if !rep.OK {
		for _, l := range rep.Lines {
			if len(l) > 6 && l[1] == 'F' {
				t.Error(l)
			}
		}
		t.Fatal("shipped datasets failed validation gates")
	}
}

func TestLegacyTriggerMapping(t *testing.T) {
	ds, err := LoadDatasets(repoEvalsDir(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	pos, neg := 0, 0
	for _, c := range ds["regression"].Cases {
		if c.Source != "020-trigger-evals" {
			t.Errorf("unexpected source %q", c.Source)
		}
		if c.Expect.Trigger {
			pos++
		} else {
			neg++
		}
	}
	if pos != 16 || neg != 16 {
		t.Errorf("legacy balance drifted: pos=%d neg=%d (020 ships 16/16)", pos, neg)
	}
}

func TestAlternationMatching(t *testing.T) {
	if !matchAlternation("User does not eat 香菜 today", "香菜|cilantro") {
		t.Error("zh alternative must match")
	}
	if !matchAlternation("no cilantro please", "香菜|CILANTRO") {
		t.Error("case-insensitive en alternative must match")
	}
	if matchAlternation("something else", "香菜|cilantro") {
		t.Error("no match expected")
	}
}

func TestTrapDatasetShipped(t *testing.T) {
	ds, err := LoadDatasets(repoEvalsDir(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	trap, ok := ds["trap"]
	if !ok {
		t.Fatal("trap dataset missing from LoadDatasets output")
	}
	mod := map[string]int{}
	for _, c := range trap.Cases {
		mod[c.Module]++
		if c.Files != nil && c.Seed == nil {
			t.Errorf("%s: file-backed trap should pair files with a seed", c.ID)
		}
	}
	for _, m := range []string{"trap-read-pos", "trap-write-neg", "trap-read-neg"} {
		if mod[m] < 4 {
			t.Errorf("module %s too small: %d", m, mod[m])
		}
	}
}

func TestTrapRuleCompletenessGate(t *testing.T) {
	// A trap read-pos case with no machine rule at all must fail validation.
	ds, err := LoadDatasets(repoEvalsDir(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	broken := *ds["trap"]
	broken.Cases = append([]Case(nil), broken.Cases...)
	broken.Cases = append(broken.Cases, Case{
		ID: "tr-pos-999", Module: "trap-read-pos", Lang: "zh",
		Category: "trap-injection", Prompt: "x",
		Expect: Expect{Trigger: true}, Source: "test",
	})
	ds["trap"] = &broken
	rep := ValidateDatasets(ds)
	if rep.OK {
		t.Fatal("ruleless trap read-pos case passed validation")
	}
	found := false
	for _, l := range rep.Lines {
		if strings.Contains(l, "tr-pos-999") {
			found = true
		}
	}
	if !found {
		t.Errorf("gate did not name the offending case; lines: %v", rep.Lines)
	}
}
