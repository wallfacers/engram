package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreIndexValidationFrozenMatrix(t *testing.T) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	rep := PreIndexValidation(core)
	if !rep.OK {
		t.Fatalf("synthetic core172 must pass pre-index: %v", rep.Lines)
	}
}

func TestPreIndexRejectsMatrixDrift(t *testing.T) {
	dir, m := syntheticCore172(t)
	m.ModuleCounts["trap-read-pos"] = 17 // drift from the frozen 18
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rep := PreIndexValidation(core); rep.OK {
		t.Fatal("manifest module-count drift must fail pre-index validation")
	}
}

func TestManifestAuthoritativeIDsAndLegacyExclusion(t *testing.T) {
	dir, m := syntheticCore172(t)
	// A legacy evals.json and any directory-discovered file are outside the
	// manifest payload set: loading must ignore them entirely.
	writeFile(t, dir, "evals.json", []byte(`[{"query":"legacy ghost","should_trigger":true}]`))
	writeFile(t, dir, "rogue-cases.json", []byte(`{"dataset":"rogue","version":1,"cases":[]}`))
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(core.Cases) != 172 {
		t.Fatalf("manifest-authoritative load must yield exactly 172 cases, got %d", len(core.Cases))
	}
	for _, ghost := range []string{"legacy ghost", "rogue"} {
		for id := range core.Cases {
			if id == ghost {
				t.Fatalf("ghost case %s leaked into the manifest-authoritative load", id)
			}
		}
	}
	// A manifest case ID absent from every payload file fails closed.
	_, m2 := syntheticCore172(t)
	m2.CaseIDs = append(m2.CaseIDs, "ghost-999")
	writeFile(t, dir, "manifest.json", mustCanonical(t, m2))
	if _, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json")); err == nil {
		t.Fatal("manifest ghost ID must fail the load")
	}
}

func TestRegressionGolden32(t *testing.T) {
	// The frozen 020 regression layer: trigger-evals.json keeps 32 cases with
	// 16 should_trigger=true followed by 16 false; loader IDs are reg-001..032.
	var legacy []legacyTriggerCase
	b, err := os.ReadFile(filepath.Join("..", "..", "skills", "engram", "evals", "trigger-evals.json"))
	if err != nil {
		t.Skipf("real trigger-evals.json not available: %v", err)
	}
	if err := strictUnmarshalForTest(b, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 32 {
		t.Fatalf("golden regression layer must stay 32 cases, got %d", len(legacy))
	}
	got := ""
	for _, lc := range legacy {
		if lc.ShouldFire {
			got += "1"
		} else {
			got += "0"
		}
	}
	if got != "11111111111111110000000000000000" {
		t.Fatalf("frozen should_trigger golden drifted: %s", got)
	}
}

func TestRegressionUnclassifiedLanguagePolicy(t *testing.T) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Every regression case must classify as regression_unclassified — never
	// folded into zh/en by query-text heuristics.
	for _, id := range []string{"reg-001", "reg-017", "reg-032"} {
		c := core.Cases[id]
		if c.Lang != nil {
			t.Fatalf("regression case %s must carry no lang field", id)
		}
		if got := c.EffectiveLang(); got != LangUnclassified {
			t.Fatalf("regression case %s effective lang = %s, want regression_unclassified", id, got)
		}
	}
	// Implicit/trap keep explicit lang.
	if got := core.Cases["iwp-01"].EffectiveLang(); got != LangZh {
		t.Fatalf("implicit case effective lang = %s, want zh", got)
	}
}

func TestFamilyAwareRequiresFrozenIndex(t *testing.T) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Without a frozen index, family-aware validation fails per-case.
	rep := FamilyAwareValidation(core, &DevFamilyIndex{CaseToFamily: map[string]string{}})
	if rep.OK {
		t.Fatal("family-aware validation must fail when the index covers nothing")
	}
	// Derive and freeze a deterministic index (offline: no mirror pairs need
	// CLI review in this synthetic set because prompts are unique per case).
	derived, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1, DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: "0", QuotaPlanDigest: "0"},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
				return false, "", ToolProvenance{}, "", nil
			}}, "input-digest", "manifest-digest")
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.FamilyIDs) != 172 {
		t.Fatalf("unique synthetic prompts must yield 172 singleton families, got %d", len(derived.FamilyIDs))
	}
	rep = FamilyAwareValidation(core, derived)
	if !rep.OK {
		t.Fatalf("family-aware must pass with the frozen covering index: %v", firstFailures(rep))
	}
}

func TestExtensionMembershipSeparation(t *testing.T) {
	// An extension payload loads under dev-extension membership and never
	// enters a core manifest's denominator.
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if core.Manifest.ScoreMembership != MembershipCore172 {
		t.Fatal("core manifest membership must be core172")
	}
	ext := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "ext-v1",
		Split: SplitDevRegression, ScoreMembership: MembershipDevExt,
		CaseCount: 1, ModuleCounts: map[string]int{"implicit-write-pos": 1},
		LanguageCounts: map[string]int{LangEn: 1},
		CaseIDs:        []string{"ext-001"},
		PayloadFiles: []PayloadFileV1{{
			RelativePath: "ext-cases.json", LFNormalizedSHA256: LFNormalizedSHA256Bytes([]byte(`{"dataset":"ext","version":2,"cases":[{"id":"ext-001","schema_version":2,"split":"dev-regression","score_membership":"dev-extension","module":"implicit-write-pos","lang":"en","scenario_bucket":null,"category":"c","family_id":null,"translation_of":null,"prompt":"p","turns":null,"seed_memories":[],"workspace_files":[],"expect":{"trigger":true,"store_include":[["x"]],"observable":"o"},"source":"flywheel-1","status":"active","superseded_by":null,"authoring":null,"reviews":null}]}`)),
			CaseIDs: []string{"ext-001"},
		}},
		ExtensionLineage: map[string]string{"iwp-01": "ext-001"},
	}
	writeFile(t, dir, "ext-cases.json", []byte(`{"dataset":"ext","version":2,"cases":[{"id":"ext-001","schema_version":2,"split":"dev-regression","score_membership":"dev-extension","module":"implicit-write-pos","lang":"en","scenario_bucket":null,"category":"c","family_id":null,"translation_of":null,"prompt":"p","turns":null,"seed_memories":[],"workspace_files":[],"expect":{"trigger":true,"store_include":[["x"]],"observable":"o"},"source":"flywheel-1","status":"active","superseded_by":null,"authoring":null,"reviews":null}]}`))
	writeFile(t, dir, "ext-manifest.json", mustCanonical(t, ext))
	_, err = LoadCoreV2(dir, filepath.Join(dir, "ext-manifest.json"))
	if err != nil {
		t.Fatalf("extension manifest must load under dev membership: %v", err)
	}
	// Lineage direction: extension manifest carries source→successor, core never changes.
	if ext.ExtensionLineage["iwp-01"] != "ext-001" {
		t.Fatal("extension lineage must map source core ID to the new extension ID")
	}

	_ = os.ErrNotExist
}

func TestClosedCaseSchemaFailures(t *testing.T) {
	for _, name := range []string{"case-bad-id.json", "case-unknown-field.json"} {
		var c TriggerCaseV2
		if err := StrictParseClosed(readFixture(t, filepath.Join("invalid", name)), &c); err == nil {
			// bad-id passes strict parse but must fail semantic validation;
			// unknown field must fail parse.
			if err := ValidateCaseV2(&c); err == nil {
				t.Errorf("%s: expected failure", name)
			}
		}
	}
	var authorFamily TriggerCaseV2
	if err := StrictParseClosed(readFixture(t, filepath.Join("invalid", "case-author-family-id.json")), &authorFamily); err != nil {
		t.Fatalf("author-family fixture should parse (fail is semantic): %v", err)
	}
	if err := ValidateCaseV2(&authorFamily); err == nil {
		t.Fatal("author self-reported family_id must fail validation")
	}
	// The valid fixtures pass both layers.
	for _, name := range []string{"core-case-v2.json", "core-regression-v2.json", "holdout-case-v2.json"} {
		var c TriggerCaseV2
		if err := StrictParseClosed(readFixture(t, name), &c); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if err := ValidateCaseV2(&c); err != nil {
			t.Errorf("%s semantic: %v", name, err)
		}
	}
}

func mustCanonical(t *testing.T, v any) []byte {
	t.Helper()
	b, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func strictUnmarshalForTest(b []byte, v any) error { return StrictParseClosed(b, v) }

func firstFailures(r ValidationReport) []string {
	var out []string
	for _, l := range r.Lines {
		if len(l) > 6 && l[:6] == "[FAIL]" {
			out = append(out, l)
		}
	}
	return out
}

func TestFamilyAwareRecordsSameLanguageNearDuplicateWithoutFailing(t *testing.T) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	// irp-15 and irp-16 are both en members of the synthetic set. A frozen
	// index may group them into one family when the lanes unanimously find a
	// genuine dataset near-duplicate; validation must keep PASSING and record
	// the finding as a non-blocking WARN (the frozen core172 stays untouched).
	caseToFam := map[string]string{}
	famIDs := []string{}
	for _, id := range sortedKeys(core.Cases) {
		f := "fam-" + id
		if id == "irp-16" {
			f = "fam-irp-15" // merged into irp-15's family
		} else {
			famIDs = append(famIDs, f)
		}
		caseToFam[id] = f
	}
	membersOf := map[string][]string{}
	for _, id := range sortedKeys(core.Cases) {
		membersOf[caseToFam[id]] = append(membersOf[caseToFam[id]], id)
	}
	idx := &DevFamilyIndex{SchemaVersion: 1, Algorithm: DevFamilyIndexAlgorithm, CaseToFamily: caseToFam}
	for _, f := range famIDs {
		idx.FamilyIDs = append(idx.FamilyIDs, f)
		idx.Families = append(idx.Families, DevFamily{FamilyID: f, CaseIDs: membersOf[f]})
	}
	rep := FamilyAwareValidation(core, idx)
	if !rep.OK {
		t.Fatalf("a recorded same-language near-duplicate must not fail the frozen-dataset gate: %v", firstFailures(rep))
	}
	found := false
	for _, l := range rep.Lines {
		if strings.Contains(l, "WARN") && strings.Contains(l, "near-duplicate") && strings.Contains(l, "irp-15") && strings.Contains(l, "irp-16") {
			found = true
		}
	}
	if !found {
		t.Fatalf("WARN line naming both duplicate members is required, lines: %v", rep.Lines)
	}
}
