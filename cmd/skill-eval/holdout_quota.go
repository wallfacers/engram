package main

// T027/T032 quota half — the exact 96-slot holdout authoring plan.
//
// The matrices are the frozen pre-registered tables from
// contracts/dataset-protocol.md §2 (author × module × language scheduler plus
// the eight scenario buckets). HoldoutQuotaSlots deterministically solves the
// author × module × language × scenario constraints; ValidateQuotaSlots is the
// independent fail-closed re-check used at seal time. The holdout validator
// must never reuse the dev trap minima (pos≥12 / neg≥8), which are impossible
// for a 16-case trap layer.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// QuotaSlot is one canonical private authoring slot: exactly
// author × module × language × scenario_bucket.
type QuotaSlot struct {
	Author   string
	Module   string
	Lang     string
	Scenario string
}

var HoldoutScenarioBuckets = []string{
	"durable-preference", "identity-biography", "project-convention",
	"environment-tooling", "supersession-time", "transience-boundary",
	"attribution-secret-boundary", "workspace-session-conflict",
}

// holdoutModuleCounts is the exact sealed-time module split (96 total).
var holdoutModuleCounts = map[string]int{
	"implicit-write-pos": 20, "implicit-write-neg": 20,
	"implicit-read-pos": 20, "implicit-read-neg": 20,
	"trap-read-pos": 8, "trap-write-neg": 4, "trap-read-neg": 4,
}

// holdoutAuthorModuleLangTable is the frozen exact slot scheduler
// (dataset-protocol.md §2). host → module → lang → count.
var holdoutAuthorModuleLangTable = map[string]map[string]map[string]int{
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

func isHoldoutImplicitModule(m string) bool {
	switch m {
	case "implicit-write-pos", "implicit-write-neg", "implicit-read-pos", "implicit-read-neg":
		return true
	}
	return false
}

// frozenHoldoutSlots is the pre-solved 96-slot plan: the frozen
// author × module × language × scenario assignment of the pre-registered §2
// matrices. It was solved offline (deterministic constraint solve, verified
// independently) and is guarded at load time by ValidateQuotaSlots, so the
// plan is reproducible and fail-closed without shipping a live solver.
var frozenHoldoutSlots = []QuotaSlot{
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostCodex, Module: "trap-read-pos", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangZh, Scenario: "attribution-secret-boundary"},
	{Author: HostOpenCode, Module: "trap-read-neg", Lang: LangEn, Scenario: "attribution-secret-boundary"},
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostClaude, Module: "trap-read-pos", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostCodex, Module: "trap-write-neg", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangEn, Scenario: "durable-preference"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangZh, Scenario: "durable-preference"},
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostClaude, Module: "trap-write-neg", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangEn, Scenario: "environment-tooling"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostOpenCode, Module: "trap-read-pos", Lang: LangZh, Scenario: "environment-tooling"},
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostClaude, Module: "trap-write-neg", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostCodex, Module: "trap-read-pos", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangZh, Scenario: "identity-biography"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangEn, Scenario: "identity-biography"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostCodex, Module: "trap-read-neg", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostCodex, Module: "trap-read-pos", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangZh, Scenario: "project-convention"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangEn, Scenario: "project-convention"},
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostClaude, Module: "trap-read-pos", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostCodex, Module: "implicit-write-pos", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangEn, Scenario: "supersession-time"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostOpenCode, Module: "trap-write-neg", Lang: LangZh, Scenario: "supersession-time"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostClaude, Module: "implicit-write-neg", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostClaude, Module: "trap-read-pos", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostCodex, Module: "trap-read-neg", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostOpenCode, Module: "implicit-write-neg", Lang: LangEn, Scenario: "transience-boundary"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangZh, Scenario: "transience-boundary"},
	{Author: HostClaude, Module: "implicit-read-neg", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostClaude, Module: "implicit-read-pos", Lang: LangEn, Scenario: "workspace-session-conflict"},
	{Author: HostClaude, Module: "implicit-write-pos", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostClaude, Module: "trap-read-neg", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangEn, Scenario: "workspace-session-conflict"},
	{Author: HostCodex, Module: "implicit-read-neg", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostCodex, Module: "implicit-read-pos", Lang: LangEn, Scenario: "workspace-session-conflict"},
	{Author: HostCodex, Module: "implicit-write-neg", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostOpenCode, Module: "implicit-read-neg", Lang: LangZh, Scenario: "workspace-session-conflict"},
	{Author: HostOpenCode, Module: "implicit-read-pos", Lang: LangEn, Scenario: "workspace-session-conflict"},
	{Author: HostOpenCode, Module: "implicit-write-pos", Lang: LangEn, Scenario: "workspace-session-conflict"},
	{Author: HostOpenCode, Module: "trap-read-pos", Lang: LangEn, Scenario: "workspace-session-conflict"},
}

// HoldoutQuotaSlots returns the frozen 96-slot plan after fail-closed
// validation. The matrices themselves are the contract
// (contracts/dataset-protocol.md §2); this function never re-solves them.
func HoldoutQuotaSlots() ([]QuotaSlot, error) {
	slots := append([]QuotaSlot(nil), frozenHoldoutSlots...)
	if err := ValidateQuotaSlots(slots); err != nil {
		return nil, err
	}
	return slots, nil
}

// ---------- independent fail-closed validation ----------

// ValidateQuotaSlots re-checks every frozen constraint from scratch. It never
// trusts the solver: the same function guards the sealed slot plan.
func ValidateQuotaSlots(slots []QuotaSlot) error {
	if len(slots) != 96 {
		return fmt.Errorf("quota plan has %d slots, want 96", len(slots))
	}
	// NOTE: identical (author, module, lang, scenario) tuples may legitimately
	// appear more than once — a bucket's ten implicit slots can assign the
	// same combination twice. The exact count tables are the real invariant,
	// so there is no tuple-uniqueness check.
	modCount := map[string]int{}
	langCount := map[string]int{}
	authorCount := map[string]int{}
	aml := map[string]map[string]map[string]int{}
	buckets := map[string][]QuotaSlot{}
	for _, s := range slots {
		if !validHosts[s.Author] {
			return fmt.Errorf("unknown author host %q", s.Author)
		}
		if !isHoldoutImplicitModule(s.Module) && s.Module != "trap-read-pos" &&
			s.Module != "trap-write-neg" && s.Module != "trap-read-neg" {
			return fmt.Errorf("unknown module %q", s.Module)
		}
		if s.Lang != LangZh && s.Lang != LangEn {
			return fmt.Errorf("unknown language %q", s.Lang)
		}
		if !holdoutScenarioKnown(s.Scenario) {
			return fmt.Errorf("unknown scenario bucket %q", s.Scenario)
		}
		modCount[s.Module]++
		langCount[s.Lang]++
		authorCount[s.Author]++
		if aml[s.Author] == nil {
			aml[s.Author] = map[string]map[string]int{}
		}
		if aml[s.Author][s.Module] == nil {
			aml[s.Author][s.Module] = map[string]int{}
		}
		aml[s.Author][s.Module][s.Lang]++
		buckets[s.Scenario] = append(buckets[s.Scenario], s)
	}
	for m, want := range holdoutModuleCounts {
		if modCount[m] != want {
			return fmt.Errorf("module %s: %d slots, want %d", m, modCount[m], want)
		}
	}
	if langCount[LangZh] != 48 || langCount[LangEn] != 48 {
		return fmt.Errorf("language split zh=%d en=%d, want 48/48", langCount[LangZh], langCount[LangEn])
	}
	for h := range holdoutAuthorModuleLangTable {
		if authorCount[h] != 32 {
			return fmt.Errorf("author %s: %d slots, want 32", h, authorCount[h])
		}
		for m, langs := range holdoutAuthorModuleLangTable[h] {
			for l, want := range langs {
				if aml[h][m][l] != want {
					return fmt.Errorf("%s/%s/%s: %d slots, want %d", h, m, l, aml[h][m][l], want)
				}
			}
		}
	}
	if len(buckets) != len(HoldoutScenarioBuckets) {
		return fmt.Errorf("%d scenario buckets populated, want %d", len(buckets), len(HoldoutScenarioBuckets))
	}
	var bucketErrs []string
	for _, b := range HoldoutScenarioBuckets {
		if err := validateHoldoutBucket(buckets[b]); err != nil {
			bucketErrs = append(bucketErrs, fmt.Sprintf("bucket %s: %v", b, err))
		}
	}
	if len(bucketErrs) > 0 {
		return errors.New(strings.Join(bucketErrs, "; "))
	}
	return nil
}

func holdoutScenarioKnown(b string) bool {
	for _, x := range HoldoutScenarioBuckets {
		if x == b {
			return true
		}
	}
	return false
}

func validateHoldoutBucket(slots []QuotaSlot) error {
	if len(slots) != 12 {
		return fmt.Errorf("%d cases, want 12", len(slots))
	}
	author := map[string]int{}
	lang := map[string]int{}
	impl := map[string]int{}
	trp, negTrp := 0, 0
	for _, s := range slots {
		author[s.Author]++
		lang[s.Lang]++
		if isHoldoutImplicitModule(s.Module) {
			impl[s.Module]++
		} else if s.Module == "trap-read-pos" {
			trp++
		} else {
			negTrp++
		}
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		if author[h] != 4 {
			return fmt.Errorf("author %s has %d, want 4", h, author[h])
		}
	}
	if lang[LangZh] != 6 || lang[LangEn] != 6 {
		return fmt.Errorf("zh/en = %d/%d, want 6/6", lang[LangZh], lang[LangEn])
	}
	if len(impl) != 4 {
		return fmt.Errorf("%d distinct implicit modules, want all 4", len(impl))
	}
	if trp != 1 || negTrp != 1 {
		return fmt.Errorf("trap pair = %d pos + %d neg, want 1+1", trp, negTrp)
	}
	return nil
}

// sortQuotaSlots orders slots deterministically (bucket, author, module, lang)
// for canonical serialization in receipts.
func sortQuotaSlots(slots []QuotaSlot) {
	sort.Slice(slots, func(i, j int) bool {
		a, b := slots[i], slots[j]
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		if a.Author != b.Author {
			return a.Author < b.Author
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Lang < b.Lang
	})
}
