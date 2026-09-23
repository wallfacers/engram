package main

// T050 score-IO tests: the series-root wiring of LoadScoreInputs /
// RunOfficialScore. The `sci` prefix marks these fixtures so they cannot
// collide with the T044 `rpt` fixtures in report_test.go; this file is kept
// separate from report_test.go so the two can evolve in parallel.
//
// Everything here is fictional: no skill, dataset, host CLI or endpoint is
// touched. The series root is materialized on disk in a temp dir so the IO
// path itself is what is under test.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func sciPtr(s string) *string { return &s }

func sciContent(caseID string) string { return "marker-" + caseID }

// sciCase turns one frozen spec into a valid sealed-dataset case whose
// deterministic rules the pass artifact set of sciArtifacts satisfies.
func sciCase(s ScoreCaseSpec) *TriggerCaseV2 {
	c := &TriggerCaseV2{
		ID: s.CaseID, SchemaVersion: 2, Split: s.Split,
		ScoreMembership: MembershipCore172, Module: s.Module,
		Lang: sciPtr(LangZh), Prompt: sciPtr("prompt " + s.CaseID),
		Status: StatusActive,
		Expect: ExpectV2{Trigger: s.Trigger, Observable: "o"},
	}
	if s.Split == SplitHoldout {
		c.ScenarioBucket = sciPtr("sci-bucket")
		c.ScoreMembership = MembershipHoldout96
	}
	if s.Module == "regression" {
		c.Lang = nil
	}
	switch s.Module {
	case "implicit-write-pos":
		c.Expect.StoreInclude = []Alternation{{sciContent(s.CaseID)}}
	case "implicit-read-pos", "trap-read-pos":
		c.Expect.AnswerInclude = []Alternation{{sciContent(s.CaseID)}}
	}
	return c
}

// sciCasesBySplit mirrors the sealed case matrix of rptSpecs so the routed
// gate denominators are the real 172/96 composition.
func sciCasesBySplit() map[string]map[string]*TriggerCaseV2 {
	out := map[string]map[string]*TriggerCaseV2{
		SplitDevRegression: {}, SplitHoldout: {},
	}
	for _, s := range rptSpecs() {
		out[s.Split][s.CaseID] = sciCase(s)
	}
	return out
}

// sciArtifacts is the artifact triple of a case whose honest outcome is a
// pass: real traces for positives, silence for negatives.
func sciArtifacts(t *testing.T, c *TriggerCaseV2) (normalized, raw, dump []byte) {
	t.Helper()
	var events []Event
	switch {
	case c.Expect.Trigger && c.Module == "implicit-write-pos":
		events = []Event{
			{Kind: EventEngramCall, Op: "write", Via: "mcp"},
			{Kind: EventText, Text: "已记住 " + sciContent(c.ID)},
		}
		dump = []byte(sciContent(c.ID) + "\n")
	case c.Expect.Trigger && c.Module == "regression":
		events = []Event{
			{Kind: EventEngramCall, Op: "search", Via: "cli"},
			{Kind: EventText, Text: "好的"},
		}
	case c.Expect.Trigger:
		events = []Event{
			{Kind: EventEngramCall, Op: "search", Via: "mcp"},
			{Kind: EventText, Text: "答案是 " + sciContent(c.ID)},
		}
	}
	normalized = sciJSON(t, events)
	raw = sciJSON(t, events)
	return normalized, raw, dump
}

func sciJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sciWriteDataset materializes one sealed split dataset under
// datasets/<membership>/ and returns its manifest digest.
func sciWriteDataset(t *testing.T, seriesRoot, membership, split string, cases map[string]*TriggerCaseV2) string {
	t.Helper()
	dir := filepath.Join(seriesRoot, datasetsDir, membership)
	ids := make([]string, 0, len(cases))
	for id := range cases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]TriggerCaseV2, 0, len(cases))
	for _, id := range ids {
		ordered = append(ordered, *cases[id])
	}
	payload := sciJSON(t, &CasePayloadFile{Dataset: membership, Version: 1, Cases: ordered})
	if err := osWriteFile(filepath.Join(dir, "cases.json"), payload); err != nil {
		t.Fatal(err)
	}
	moduleCounts := map[string]int{}
	langCounts := map[string]int{}
	for _, id := range ids {
		c := cases[id]
		moduleCounts[c.Module]++
		if l := c.EffectiveLang(); l != "" {
			langCounts[l]++
		}
	}
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "sci-" + membership, DatasetVersion: "sci-v1",
		Split: split, ScoreMembership: membership,
		CaseCount: len(ids), ModuleCounts: moduleCounts, LanguageCounts: langCounts,
		CaseIDs: ids,
		PayloadFiles: []PayloadFileV1{{
			RelativePath: "cases.json", LFNormalizedSHA256: LFNormalizedSHA256Bytes(payload), CaseIDs: ids,
		}},
	}
	idsDigest, err := CaseIDsDigest(ids)
	if err != nil {
		t.Fatalf("case ids digest: %v", err)
	}
	m.CaseIDsDigest = idsDigest
	d, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatalf("payload digest: %v", err)
	}
	m.PayloadDigest = d
	digest, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatalf("manifest for seal: %v", err)
	}
	seal, err := BuildDatasetAnchor(m, digest, "git-tag", "sci-anchor-"+membership)
	if err != nil {
		t.Fatal(err)
	}
	m.Seal = seal
	if err := osWriteFile(filepath.Join(dir, datasetManifestName), sciJSON(t, m)); err != nil {
		t.Fatal(err)
	}
	return digest
}

// sciWriteRuns re-points every run at its split's sealed case set and writes
// the full case-receipt tree in the runner's layout: one receipt plus three
// artifacts per case, with the receipt citing the same paths the primary
// runner records.
func sciWriteRuns(t *testing.T, seriesRoot string, in *ScoreEligibilityInput, cases map[string]map[string]*TriggerCaseV2) {
	t.Helper()
	for _, key := range sortedRunKeys(in.Runs) {
		ids := make([]string, 0, len(cases[key.Split]))
		for id := range cases[key.Split] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		run := in.Runs[key]
		run.CaseIDs = ids
		run.CaseOrder = append([]string(nil), ids...)
		for _, id := range ids {
			c := cases[key.Split][id]
			normalized, raw, dump := sciArtifacts(t, c)
			caseDir := filepath.Join(PrimaryRunRoot(seriesRoot, key.Host, key.Split, key.Ordinal), casesDirName, id)
			normPath := filepath.Join(caseDir, "normalized-events.json")
			rawPath := filepath.Join(caseDir, "raw.jsonl")
			dumpPath := filepath.Join(caseDir, "store-dump.txt")
			for _, f := range []struct {
				p string
				b []byte
			}{
				{normPath, normalized}, {rawPath, raw}, {dumpPath, dump},
			} {
				if err := osWriteFile(f.p, f.b); err != nil {
					t.Fatal(err)
				}
			}
			verdict, err := RejudgeFromArtifacts(normalized, dump, c)
			if err != nil {
				t.Fatalf("fixture case %s must rejudge: %v", id, err)
			}
			receipt := &CaseRunReceipt{
				CaseID:                          id,
				CasePayloadDigest:               "sci-payload-" + id,
				WorkspaceDigest:                 "sci-workspace-" + id,
				CaseStateIsolationReceiptDigest: "sci-isolation-" + key.Host + "-" + id + "-" + itoa(key.Ordinal),
				AttemptCount:                    1,
				Status:                          CaseStatusPass,
				NormalizedEventsPath:            normPath, NormalizedEventsDigest: sha256Hex(normalized),
				RawEventsPath: rawPath, RawEventsDigest: sha256Hex(raw),
				StoreDumpPath: dumpPath, StoreDumpDigest: sha256Hex(dump),
				Verdict:      verdict,
				DurationMS:   900,
				StderrDigest: "sci-stderr-" + id,
			}
			if err := osWriteFile(filepath.Join(caseDir, caseReceiptName), sciJSON(t, receipt)); err != nil {
				t.Fatal(err)
			}
		}
		if err := osWriteFile(filepath.Join(PrimaryRunRoot(seriesRoot, key.Host, key.Split, key.Ordinal), runManifestName), sciJSON(t, run)); err != nil {
			t.Fatal(err)
		}
	}
}

// sciWriteRoot materializes a structurally complete official-dual series root
// whose every case is an honest pass, and returns its eligibility bundle.
func sciWriteRoot(t *testing.T, seriesRoot string) *ScoreEligibilityInput {
	t.Helper()
	in, _ := rptBundle(t)
	cases := sciCasesBySplit()
	dev := sciWriteDataset(t, seriesRoot, MembershipCore172, SplitDevRegression, cases[SplitDevRegression])
	holdout := sciWriteDataset(t, seriesRoot, MembershipHoldout96, SplitHoldout, cases[SplitHoldout])
	in.Manifest.DatasetManifests[MembershipCore172] = dev
	in.Manifest.DatasetManifests[MembershipHoldout96] = holdout
	in.Binding.DatasetManifestDigest = holdout
	sciWriteRuns(t, seriesRoot, in, cases)

	write := []struct {
		name string
		v    any
	}{
		{seriesManifestFile, in.Manifest},
		{corePlanFile, in.Plan},
		{protectedExecutionFile, in.Protected},
		{packageValidationFile, in.Package},
		{greenSeriesPrepareFile, in.SeriesPrepare},
		{greenPreHoldoutFile, in.PreHoldout},
		{holdoutBindingFile, in.Binding},
	}
	for _, w := range write {
		if err := osWriteFile(filepath.Join(seriesRoot, w.name), sciJSON(t, w.v)); err != nil {
			t.Fatal(err)
		}
	}
	for h, slots := range in.Manifest.WorkspaceCanaryReceiptDigests {
		for slot := range slots {
			rel := filepath.Join(canariesDir, h, "slot-"+itoa(slot)+".json")
			if err := osWriteFile(filepath.Join(seriesRoot, rel), sciJSON(t, in.Canaries[h][slot])); err != nil {
				t.Fatal(err)
			}
		}
	}
	return in
}

func TestScoreIOMissingFileFailsClosed(t *testing.T) {
	root := t.TempDir()
	sciWriteRoot(t, root)
	if _, err := LoadScoreInputs(root); err != nil {
		t.Fatalf("a complete series root must load: %v", err)
	}
	missing := []string{
		seriesManifestFile, corePlanFile, protectedExecutionFile, packageValidationFile,
		greenSeriesPrepareFile, greenPreHoldoutFile, holdoutBindingFile,
		filepath.Join(runsDir, HostClaude+"-"+SplitHoldout+"-o3", runManifestName),
		filepath.Join(runsDir, HostOpenCode+"-"+SplitDevRegression+"-o1", runManifestName),
		filepath.Join(canariesDir, HostCodex, "slot-2.json"),
	}
	for _, rel := range missing {
		p := filepath.Join(root, rel)
		if err := os.Rename(p, p+".away"); err != nil {
			t.Fatal(err)
		}
		_, err := LoadScoreInputs(root)
		if rerr := os.Rename(p+".away", p); rerr != nil {
			t.Fatal(rerr)
		}
		if err == nil {
			t.Fatalf("removing %s must fail the load", rel)
		}
		if !strings.Contains(err.Error(), rel) {
			t.Fatalf("the error must name the missing file %s, got %v", rel, err)
		}
	}
	if _, err := LoadScoreInputs(""); err == nil {
		t.Fatal("an empty series root must be rejected")
	}
	if _, err := LoadScoreInputs(filepath.Join(root, seriesManifestFile)); err == nil {
		t.Fatal("a file as series root must be rejected")
	}
	// A missing case receipt loads fine (it is not part of the bundle) but the
	// score itself must refuse: a case with no terminal receipt is never an
	// implicit pass.
	caseReceipt := filepath.Join(PrimaryRunRoot(root, HostCodex, SplitHoldout, 2), casesDirName, sciFirstHoldoutCase(), caseReceiptName)
	if err := os.Rename(caseReceipt, caseReceipt+".away"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(caseReceipt+".away", caseReceipt)
	if _, err := RunOfficialScore(root); err == nil || !strings.Contains(err.Error(), sciFirstHoldoutCase()) {
		t.Fatalf("scoring with a missing case receipt must fail closed naming the case, got %v", err)
	}
}

func TestScoreIODatasetMissingFailsClosed(t *testing.T) {
	root := t.TempDir()
	sciWriteRoot(t, root)
	// The bundle loads without the datasets; the score itself may not.
	if _, err := LoadScoreInputs(root); err != nil {
		t.Fatalf("bundle load: %v", err)
	}
	p := filepath.Join(root, datasetsDir, MembershipCore172, datasetManifestName)
	if err := os.Rename(p, p+".away"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(p+".away", p)
	if _, err := RunOfficialScore(root); err == nil {
		t.Fatal("scoring without the sealed core dataset must fail closed")
	} else if !strings.Contains(err.Error(), MembershipCore172) {
		t.Fatalf("the error should name the missing dataset, got %v", err)
	}
}

func TestScoreIODevComparisonRejected(t *testing.T) {
	root := t.TempDir()
	in := sciWriteRoot(t, root)
	// A dev-comparison series is core-only diagnostic evidence: it can never
	// produce an official score, whatever its receipts look like.
	in.Manifest.Purpose = PurposeDevComparison
	if err := osWriteFile(filepath.Join(root, seriesManifestFile), sciJSON(t, in.Manifest)); err != nil {
		t.Fatal(err)
	}
	_, err := RunOfficialScore(root)
	if err == nil {
		t.Fatal("a dev-comparison series must never produce an official score")
	}
	if !strings.Contains(err.Error(), "dev-comparison") {
		t.Fatalf("the rejection should name the purpose, got %v", err)
	}
}

func TestScoreIOEndToEndSealedReport(t *testing.T) {
	root := t.TempDir()
	sciWriteRoot(t, root)
	report, err := RunOfficialScore(root)
	if err != nil {
		t.Fatalf("RunOfficialScore: %v", err)
	}
	if report.OverallVerdict != "pass" {
		t.Fatalf("an all-pass series must score pass, got %q", report.OverallVerdict)
	}
	if report.DiagnosticArtifactsUsed {
		t.Fatal("an official report must never flag diagnostic artifacts")
	}
	if len(report.DevRegression) != 3 || len(report.Generalization) != 3 {
		t.Fatalf("both families must cover three hosts: %d dev / %d holdout",
			len(report.DevRegression), len(report.Generalization))
	}
	// Five routed gates per host on both splits: the implicit pair, the merged
	// negative gate, plus the dev-only 020 pair and the holdout-only trap pair.
	for _, family := range []struct {
		name    string
		hosts   []HostScore
		hostIdx int
		gateID  ScoreGateID
	}{
		{"dev", report.DevRegression, 0, GateRegressionPos},
		{"holdout", report.Generalization, 0, GateTrapReadPos},
	} {
		if len(family.hosts[0].Gates) != 5 {
			t.Fatalf("%s family carries %d gates, want 5", family.name, len(family.hosts[0].Gates))
		}
		if family.hosts[0].Split == SplitDevRegression {
			for _, g := range family.hosts[0].Gates {
				if g.ID == GateTrapReadPos {
					t.Fatal("the trap pair must never route into the dev family")
				}
			}
		}
		found := false
		for _, g := range family.hosts[0].Gates {
			if g.ID == family.gateID {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s family is missing gate %s", family.name, family.gateID)
		}
	}
	if err := VerifyOfficialScoreSeal(report); err != nil {
		t.Fatalf("the returned report must verify its own seal: %v", err)
	}
	// The written artifact is canonical, byte-identical to what was returned,
	// and re-verifies after a round trip through disk.
	b, err := os.ReadFile(filepath.Join(root, officialScoreReportFile))
	if err != nil {
		t.Fatal(err)
	}
	loaded := &OfficialScoreReport{}
	if err := StrictParseClosed(b, loaded); err != nil {
		t.Fatalf("the sealed report must reparse under the closed schema: %v", err)
	}
	if !reflect.DeepEqual(loaded, report) {
		t.Fatal("the sealed report on disk must be exactly the returned report")
	}
	if err := VerifyOfficialScoreSeal(loaded); err != nil {
		t.Fatalf("the reloaded report must verify: %v", err)
	}
	// A sealed report is frozen: scoring again must refuse to overwrite it.
	if _, err := RunOfficialScore(root); err == nil {
		t.Fatal("a second score run must not overwrite the frozen report")
	}
}

func TestScoreIOTamperedArtifactsFailClosed(t *testing.T) {
	root := t.TempDir()
	sciWriteRoot(t, root)
	// (a) A store dump edited after the run drifts from its recorded digest.
	target := filepath.Join(PrimaryRunRoot(root, HostClaude, SplitDevRegression, 1), casesDirName, sciFirstDevCase(), "store-dump.txt")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(original, []byte("rogue\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOfficialScore(root); err == nil || !strings.Contains(err.Error(), "post-run mutation") {
		t.Fatalf("a tampered store dump must stop the score, got %v", err)
	}
	root = t.TempDir()
	sciWriteRoot(t, root)
	// (b) A receipt that claims a failure its artifacts do not show cannot
	// survive the rejudge, in either direction.
	receiptPath := filepath.Join(PrimaryRunRoot(root, HostClaude, SplitDevRegression, 1), casesDirName, sciFirstDevCase(), caseReceiptName)
	b, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	r := &CaseRunReceipt{}
	if err := StrictParseClosed(b, r); err != nil {
		t.Fatal(err)
	}
	r.Status = CaseStatusFail
	r.Verdict = Verdict{CaseID: r.CaseID, Failure: FailureWrongReport, Detail: "fabricated"}
	if err := osWriteFile(receiptPath, sciJSON(t, r)); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOfficialScore(root); err == nil ||
		!strings.Contains(err.Error(), "rejudge to") {
		t.Fatalf("a fabricated verdict must be caught by the rejudge, got %v", err)
	}
	root = t.TempDir()
	sciWriteRoot(t, root)
	// (c) A failure class outside the closed v2 set fails closed instead of
	// being read as a pass.
	receiptPath = filepath.Join(PrimaryRunRoot(root, HostCodex, SplitHoldout, 2), casesDirName, sciFirstHoldoutCase(), caseReceiptName)
	b, err = os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	r = &CaseRunReceipt{}
	if err := StrictParseClosed(b, r); err != nil {
		t.Fatal(err)
	}
	r.Status = CaseStatusFail
	r.Verdict = Verdict{CaseID: r.CaseID, Failure: "failed", Detail: "legacy v1 chain"}
	if err := osWriteFile(receiptPath, sciJSON(t, r)); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOfficialScore(root); err == nil ||
		!strings.Contains(err.Error(), "outside the closed v2 set") {
		t.Fatalf("a legacy failure class must fail closed, got %v", err)
	}
}

// sciFirstDevCase / sciFirstHoldoutCase name a deterministic case to tamper
// with, so the failures above are reproducible.
func sciFirstDevCase() string { return "rpt-iwp-001" }

func sciFirstHoldoutCase() string { return "rpt-h-iwp-001" }
