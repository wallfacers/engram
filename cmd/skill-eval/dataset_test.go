package main

import (
	"path/filepath"
	"runtime"
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
