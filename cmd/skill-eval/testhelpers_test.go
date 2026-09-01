package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func sortStrings(s []string) { sort.Strings(s) }

// readFixture loads a fictional v2 fixture from testdata/v2.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "v2", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// writeFile writes (or overwrites) a file under dir in one step, creating
// any intermediate directories.
func writeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// syntheticCore172 generates a full 172-case dataset matching the frozen
// matrix (28/28/28/28/18/6/4 + 32 regression; zh=72/en=68/unclassified=32)
// in a temp dir and returns (dir, manifest).
func syntheticCore172(t *testing.T) (string, *DatasetManifestV2) {
	t.Helper()
	dir := t.TempDir()
	type spec struct {
		module string
		n      int
		zh     int
		rule   string
	}
	specs := []spec{
		{"implicit-write-pos", 28, 15, "store"},
		{"implicit-write-neg", 28, 15, "none"},
		{"implicit-read-pos", 28, 14, "answer"},
		{"implicit-read-neg", 28, 14, "none"},
		{"trap-read-pos", 18, 9, "answer"},
		{"trap-write-neg", 6, 3, "none"},
		{"trap-read-neg", 4, 2, "none"},
	}
	var ids []string
	files := map[string][]TriggerCaseV2{}
	for _, s := range specs {
		for i := 0; i < s.n; i++ {
			lang := "en"
			if i < s.zh {
				lang = "zh"
			}
			id := moduleAbbrev(s.module) + "-" + twoDigits(i+1)
			ids = append(ids, id)
			category := "synthetic"
			if strings.HasPrefix(s.module, "trap-") {
				category = "trap-synthetic"
			}
			e := ExpectV2{Trigger: !hasSuffix(s.module, "-neg"), Observable: "synthetic case"}
			switch s.rule {
			case "store":
				e.StoreInclude = []Alternation{{"marker"}}
			case "answer":
				e.AnswerInclude = []Alternation{{"marker"}}
			}
			files["cases-"+moduleAbbrev(s.module)+".json"] = append(files["cases-"+moduleAbbrev(s.module)+".json"], TriggerCaseV2{
				ID: id, SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
				Module: s.module, Lang: &lang, Category: category,
				Prompt: strPtr("synthetic prompt " + id),
				Expect: e, Source: "synthetic", Status: StatusActive,
			})
		}
	}
	for i := 0; i < 32; i++ {
		// Mirror the real v1 loader IDs: reg-001..reg-032.
		id := fmt.Sprintf("reg-%03d", i+1)
		ids = append(ids, id)
		files["cases-regression.json"] = append(files["cases-regression.json"], TriggerCaseV2{
			ID: id, SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
			Module: "regression", Category: "legacy-020",
			Prompt: strPtr("legacy trigger " + id), Expect: ExpectV2{Trigger: i < 16, Observable: "legacy"},
			Source: "020-trigger-evals", Status: StatusActive,
		})
	}
	var payload []PayloadFileV1
	for name, cases := range files {
		b, err := CanonicalJSON(CasePayloadFile{Dataset: "synthetic", Version: 2, Cases: cases})
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, name, b)
		var cids []string
		for _, c := range cases {
			cids = append(cids, c.ID)
		}
		payload = append(payload, PayloadFileV1{RelativePath: name, LFNormalizedSHA256: LFNormalizedSHA256Bytes(b), CaseIDs: cids})
	}
	sortStrings(ids)
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "synthetic-core-v1",
		Split: SplitDevRegression, ScoreMembership: MembershipCore172,
		CaseCount: 172,
		ModuleCounts: map[string]int{
			"implicit-write-pos": 28, "implicit-write-neg": 28,
			"implicit-read-pos": 28, "implicit-read-neg": 28,
			"trap-read-pos": 18, "trap-write-neg": 6, "trap-read-neg": 4,
			"regression": 32,
		},
		LanguageCounts: map[string]int{LangZh: 72, LangEn: 68, LangUnclassified: 32},
		CaseIDs:        ids,
		PayloadFiles:   payload,
	}
	return dir, m
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }

func moduleAbbrev(m string) string {
	switch m {
	case "implicit-write-pos":
		return "iwp"
	case "implicit-write-neg":
		return "iwn"
	case "implicit-read-pos":
		return "irp"
	case "implicit-read-neg":
		return "irn"
	case "trap-read-pos":
		return "trp"
	case "trap-write-neg":
		return "twn"
	case "trap-read-neg":
		return "trn"
	}
	return m
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func strPtr(s string) *string { return &s }
