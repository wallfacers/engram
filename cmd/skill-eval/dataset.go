package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case is one trigger-evaluation scenario.
type Case struct {
	ID       string     `json:"id"`
	Module   string     `json:"module"`
	Lang     string     `json:"lang"`
	Category string     `json:"category"`
	Prompt   string     `json:"prompt"`
	Expect   Expect     `json:"expect"`
	Seed     []SeedItem `json:"seed"`
	Source   string     `json:"source"`
}

// Expect holds the machine-readable judgement rules. Observable is the
// human-readable contract; the other fields drive the deterministic judge.
type Expect struct {
	Trigger       bool     `json:"trigger"`
	Observable    string   `json:"observable"`
	StoreInclude  []string `json:"store_include,omitempty"`  // each item: |-separated alternations, every item must match the store dump
	AnswerInclude []string `json:"answer_include,omitempty"` // each item: |-separated alternations, every item must match the answer text
	Acknowledge   bool     `json:"acknowledge,omitempty"`    // write cases: same-turn acknowledgment required
	NotFound      bool     `json:"notfound,omitempty"`       // read cases: honest not-found statement required
}

// SeedItem is a memory entry planted deterministically via the CLI before the
// prompt runs.
type SeedItem struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Dataset is one JSON dataset file.
type Dataset struct {
	Dataset string `json:"dataset"`
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

// legacyTriggerCase matches 020's trigger-evals.json shape.
type legacyTriggerCase struct {
	Query      string `json:"query"`
	ShouldFire bool   `json:"should_trigger"`
}

var requiredModules = []string{
	"implicit-write-pos",
	"implicit-write-neg",
	"implicit-read-pos",
	"implicit-read-neg",
}

// LoadDatasets loads the implicit-write/implicit-read datasets plus the 020
// legacy trigger set (mapped into the regression module) from dir.
func LoadDatasets(dir string) (map[string]*Dataset, error) {
	out := map[string]*Dataset{}
	for _, name := range []string{"implicit-write.json", "implicit-read.json"} {
		var d Dataset
		if err := loadJSON(filepath.Join(dir, name), &d); err != nil {
			return nil, fmt.Errorf("load %s: %w", name, err)
		}
		out[d.Dataset] = &d
	}
	// Legacy 020 trigger set → regression module (content is never modified).
	var legacy []legacyTriggerCase
	if err := loadJSON(filepath.Join(dir, "trigger-evals.json"), &legacy); err != nil {
		return nil, fmt.Errorf("load trigger-evals.json: %w", err)
	}
	reg := &Dataset{Dataset: "regression", Version: 1}
	for i, lc := range legacy {
		reg.Cases = append(reg.Cases, Case{
			ID:     fmt.Sprintf("reg-%03d", i+1),
			Module: "regression",
			Lang:   langOf(lc.Query),
			Prompt: lc.Query,
			Expect: Expect{Trigger: lc.ShouldFire, Observable: "legacy 020 trigger case: any engram call expected iff should_trigger"},
			Seed:   nil,
			Source: "020-trigger-evals",
		})
	}
	out["regression"] = reg
	return out, nil
}

func langOf(q string) string {
	for _, r := range q {
		if r > 127 {
			return "zh"
		}
	}
	return "en"
}

func loadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// ValidationReport summarizes dataset structure checks.
type ValidationReport struct {
	OK    bool
	Lines []string
}

func (v *ValidationReport) addf(ok bool, format string, args ...any) {
	mark := "PASS"
	if !ok {
		mark = "FAIL"
		v.OK = false
	}
	v.Lines = append(v.Lines, fmt.Sprintf("[%s] "+format, append([]any{mark}, args...)...))
}

// ValidateDatasets enforces the frozen structure gates: module coverage,
// positive/negative balance, unique ids, language coverage, and per-module
// machine-rule completeness (pos write cases need store rules; read pos cases
// need answer rules or notfound).
func ValidateDatasets(datasets map[string]*Dataset) ValidationReport {
	rep := ValidationReport{OK: true}
	seen := map[string]string{}
	modCount := map[string]int{}
	modLang := map[string]map[string]bool{}
	var ids []string

	addCases := func(d *Dataset) {
		for _, c := range d.Cases {
			if prev, dup := seen[c.ID]; dup {
				rep.addf(false, "duplicate case id %s (also in %s)", c.ID, prev)
				continue
			}
			seen[c.ID] = d.Dataset
			ids = append(ids, c.ID)
			modCount[c.Module]++
			if modLang[c.Module] == nil {
				modLang[c.Module] = map[string]bool{}
			}
			modLang[c.Module][c.Lang] = true
		}
	}
	for _, name := range []string{"implicit-write", "implicit-read", "regression"} {
		d, ok := datasets[name]
		if !ok {
			rep.addf(false, "dataset %s missing", name)
			continue
		}
		addCases(d)
	}

	for _, m := range requiredModules {
		rep.addf(modCount[m] >= 20, "module %s has %d cases (gate: >=20)", m, modCount[m])
		rep.addf(len(modLang[m]) >= 2, "module %s language coverage: %v (gate: zh+en)", m, keysOf(modLang[m]))
	}
	rep.addf(modCount["regression"] >= 20, "module regression has %d cases (gate: >=20)", modCount["regression"])

	// Balance gates: pos:neg within each implicit dataset at 40/40 minimum share.
	for _, name := range []string{"implicit-write", "implicit-read"} {
		d := datasets[name]
		pos, neg := 0, 0
		for _, c := range d.Cases {
			if c.Expect.Trigger {
				pos++
			} else {
				neg++
			}
		}
		total := pos + neg
		rep.addf(pos*100 >= 40*total && neg*100 >= 40*total,
			"%s balance pos=%d neg=%d (gate: each >=40%% of %d)", name, pos, neg, total)
	}

	// Machine-rule completeness.
	for _, name := range []string{"implicit-write", "implicit-read"} {
		for _, c := range datasets[name].Cases {
			if !c.Expect.Trigger {
				continue
			}
			switch c.Module {
			case "implicit-write-pos":
				if len(c.Expect.StoreInclude) == 0 || !c.Expect.Acknowledge {
					rep.addf(false, "%s: write-pos missing store_include/acknowledge rules", c.ID)
				}
			case "implicit-read-pos":
				if len(c.Expect.AnswerInclude) == 0 && !c.Expect.NotFound {
					rep.addf(false, "%s: read-pos missing answer_include/notfound rules", c.ID)
				}
			}
		}
	}

	sort.Strings(ids)
	rep.addf(true, "total cases: %d (ids unique, modules covered)", len(ids))
	return rep
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ = strings.TrimSpace // placeholder to keep imports stable during edits
