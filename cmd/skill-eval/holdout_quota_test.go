package main

// T027 [US3] — exact 96-case quota scheduling tests. The matrices below are
// transcribed independently from contracts/dataset-protocol.md §2 so a drift
// in either direction (implementation or this file) fails the comparison.

import (
	"fmt"
	"testing"
)

// Independent transcription of the frozen author × module × language table
// (dataset-protocol.md §2 "exact slot scheduler"). host → module → lang → n.
var t027AuthorModuleLang = map[string]map[string]map[string]int{
	HostClaude: {
		"implicit-write-pos": {LangZh: 4, LangEn: 3},
		"implicit-write-neg": {LangZh: 3, LangEn: 4},
		"implicit-read-pos":  {LangZh: 3, LangEn: 3},
		"implicit-read-neg":  {LangZh: 3, LangEn: 3},
		"trap-read-pos":      {LangZh: 1, LangEn: 2},
		"trap-write-neg":     {LangZh: 1, LangEn: 1},
		"trap-read-neg":      {LangZh: 1, LangEn: 0},
	},
	HostCodex: {
		"implicit-write-pos": {LangZh: 3, LangEn: 4},
		"implicit-write-neg": {LangZh: 3, LangEn: 3},
		"implicit-read-pos":  {LangZh: 4, LangEn: 3},
		"implicit-read-neg":  {LangZh: 3, LangEn: 3},
		"trap-read-pos":      {LangZh: 2, LangEn: 1},
		"trap-write-neg":     {LangZh: 0, LangEn: 1},
		"trap-read-neg":      {LangZh: 1, LangEn: 1},
	},
	HostOpenCode: {
		"implicit-write-pos": {LangZh: 3, LangEn: 3},
		"implicit-write-neg": {LangZh: 4, LangEn: 3},
		"implicit-read-pos":  {LangZh: 3, LangEn: 4},
		"implicit-read-neg":  {LangZh: 4, LangEn: 4},
		"trap-read-pos":      {LangZh: 1, LangEn: 1},
		"trap-write-neg":     {LangZh: 1, LangEn: 0},
		"trap-read-neg":      {LangZh: 0, LangEn: 1},
	},
}

func t027Counts(slots []QuotaSlot) (mod, author, zhEn map[string]int) {
	mod, author = map[string]int{}, map[string]int{}
	zhEn = map[string]int{}
	for _, s := range slots {
		mod[s.Module]++
		author[s.Author]++
		zhEn[s.Lang]++
	}
	return
}

func TestHoldoutQuotaSlotsExactMatrix(t *testing.T) {
	slots, err := HoldoutQuotaSlots()
	if err != nil {
		t.Fatalf("HoldoutQuotaSlots: %v", err)
	}
	if len(slots) != 96 {
		t.Fatalf("got %d slots, want 96", len(slots))
	}
	mod, author, zhEn := t027Counts(slots)
	wantMod := map[string]int{
		"implicit-write-pos": 20, "implicit-write-neg": 20,
		"implicit-read-pos": 20, "implicit-read-neg": 20,
		"trap-read-pos": 8, "trap-write-neg": 4, "trap-read-neg": 4,
	}
	for m, want := range wantMod {
		if mod[m] != want {
			t.Errorf("module %s: %d slots, want %d", m, mod[m], want)
		}
	}
	if zhEn[LangZh] != 48 || zhEn[LangEn] != 48 {
		t.Errorf("language split zh=%d en=%d, want 48/48", zhEn[LangZh], zhEn[LangEn])
	}
	for h, want := range map[string]int{HostClaude: 32, HostCodex: 32, HostOpenCode: 32} {
		if author[h] != want {
			t.Errorf("author %s: %d slots, want %d", h, author[h], want)
		}
	}
	// Exact host × module × language table.
	got := map[string]map[string]map[string]int{}
	for _, s := range slots {
		if got[s.Author] == nil {
			got[s.Author] = map[string]map[string]int{}
		}
		if got[s.Author][s.Module] == nil {
			got[s.Author][s.Module] = map[string]int{}
		}
		got[s.Author][s.Module][s.Lang]++
	}
	for h, mods := range t027AuthorModuleLang {
		for m, langs := range mods {
			for l, want := range langs {
				if got[h][m][l] != want {
					t.Errorf("%s/%s/%s: %d slots, want %d", h, m, l, got[h][m][l], want)
				}
			}
		}
	}
	// Determinism: a second solve is byte-identical.
	slots2, err := HoldoutQuotaSlots()
	if err != nil {
		t.Fatalf("second solve: %v", err)
	}
	for i := range slots {
		if slots[i] != slots2[i] {
			t.Fatalf("slot %d differs between solves: %+v vs %+v", i, slots[i], slots2[i])
		}
	}
}

func TestHoldoutScenarioBucketCoverage(t *testing.T) {
	slots, err := HoldoutQuotaSlots()
	if err != nil {
		t.Fatalf("HoldoutQuotaSlots: %v", err)
	}
	perBucket := map[string][]QuotaSlot{}
	for _, s := range slots {
		perBucket[s.Scenario] = append(perBucket[s.Scenario], s)
	}
	if len(perBucket) != len(HoldoutScenarioBuckets) {
		t.Fatalf("%d scenario buckets populated, want %d", len(perBucket), len(HoldoutScenarioBuckets))
	}
	for _, bucket := range HoldoutScenarioBuckets {
		got := perBucket[bucket]
		if len(got) != 12 {
			t.Errorf("bucket %s: %d cases, want 12", bucket, len(got))
			continue
		}
		perAuthor := map[string]int{}
		lang := map[string]int{}
		implicitMods, trapPos, trapNeg := map[string]int{}, 0, 0
		for _, s := range got {
			perAuthor[s.Author]++
			lang[s.Lang]++
			switch {
			case isHoldoutImplicitModule(s.Module):
				implicitMods[s.Module]++
			case s.Module == "trap-read-pos":
				trapPos++
			case s.Module == "trap-write-neg" || s.Module == "trap-read-neg":
				trapNeg++
			}
		}
		for h, want := range map[string]int{HostClaude: 4, HostCodex: 4, HostOpenCode: 4} {
			if perAuthor[h] != want {
				t.Errorf("bucket %s: author %s has %d, want %d", bucket, h, perAuthor[h], want)
			}
		}
		if lang[LangZh] != 6 || lang[LangEn] != 6 {
			t.Errorf("bucket %s: zh/en = %d/%d, want 6/6", bucket, lang[LangZh], lang[LangEn])
		}
		if len(implicitMods) != 4 {
			t.Errorf("bucket %s: %d distinct implicit modules (%v), want all 4", bucket, len(implicitMods), implicitMods)
		}
		if trapPos != 1 || trapNeg != 1 {
			t.Errorf("bucket %s: trap pair = %d pos + %d neg, want exactly 1+1", bucket, trapPos, trapNeg)
		}
		if n := len(implicitMods) + trapPos + trapNeg; n != 0 && countImplicit(got) != 10 {
			t.Errorf("bucket %s: %d implicit cases, want 10", bucket, countImplicit(got))
		}
	}
}

func countImplicit(slots []QuotaSlot) int {
	n := 0
	for _, s := range slots {
		if isHoldoutImplicitModule(s.Module) {
			n++
		}
	}
	return n
}

func TestValidateQuotaSlotsFailClosed(t *testing.T) {
	base, err := HoldoutQuotaSlots()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if err := ValidateQuotaSlots(base); err != nil {
		t.Fatalf("canonical plan rejected: %v", err)
	}
	mutate := func(name string, f func(slots []QuotaSlot) []QuotaSlot) {
		t.Run(name, func(t *testing.T) {
			drop := append([]QuotaSlot(nil), base...)
			if err := ValidateQuotaSlots(f(drop)); err == nil {
				t.Errorf("mutated plan %s accepted", name)
			}
		})
	}
	mutate("drop-one", func(s []QuotaSlot) []QuotaSlot { return s[:95] })
	mutate("duplicate-one", func(s []QuotaSlot) []QuotaSlot { return append(s, s[0]) })
	mutate("flip-lang", func(s []QuotaSlot) []QuotaSlot {
		if s[3].Lang == LangZh {
			s[3].Lang = LangEn
		} else {
			s[3].Lang = LangZh
		}
		return s
	})
	mutate("swap-author", func(s []QuotaSlot) []QuotaSlot {
		s[3].Author = otherHost(s[3].Author)
		return s
	})
	mutate("unknown-scenario", func(s []QuotaSlot) []QuotaSlot {
		s[3].Scenario = "invented-bucket"
		return s
	})
	mutate("unknown-module", func(s []QuotaSlot) []QuotaSlot {
		s[3].Module = "implicit-write-mixed"
		return s
	})
	mutate("unknown-lang", func(s []QuotaSlot) []QuotaSlot {
		s[3].Lang = "mixed"
		return s
	})
}

func otherHost(h string) string {
	switch h {
	case HostClaude:
		return HostCodex
	case HostCodex:
		return HostOpenCode
	}
	return HostClaude
}

// TestHoldoutMatrixIndependentFromDevMinima guards the contract's explicit
// rule that the holdout validator uses the split-specific 96 matrix and never
// the dev trap minima (pos≥12 / neg≥8), which are impossible for 16 traps.
func TestHoldoutMatrixIndependentFromDevMinima(t *testing.T) {
	if got := holdoutModuleCounts["trap-read-pos"]; got != 8 {
		t.Errorf("holdout trap-read-pos = %d, want 8 (dev minima 12 must never be reused)", got)
	}
	if got := holdoutModuleCounts["trap-write-neg"] + holdoutModuleCounts["trap-read-neg"]; got != 8 {
		t.Errorf("holdout negative traps = %d, want 8 (dev minima 8 cannot map onto 16-case layer)", got)
	}
	// The two frozen matrices are distinct tables: a holdout case count can
	// never be validated by core172 numbers and vice versa. (Individual
	// modules may coincide — trap-read-neg is 4 in both — so the guards are
	// the trap-layer scale, the total, and the table identity.)
	if fmt.Sprintf("%v", holdoutModuleCounts) == fmt.Sprintf("%v", core172ModuleCounts) {
		t.Error("holdout matrix equals core172 matrix")
	}
	if h, c := holdoutModuleCounts["trap-read-pos"], core172ModuleCounts["trap-read-pos"]; h == c {
		t.Errorf("trap-read-pos identical in both matrices (%d) — the dev minima leaked into holdout", h)
	}
	total := 0
	for _, n := range holdoutModuleCounts {
		total += n
	}
	if total != 96 {
		t.Errorf("holdout matrix total = %d, want 96", total)
	}
}
