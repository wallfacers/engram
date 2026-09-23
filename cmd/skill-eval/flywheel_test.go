package main

// 048 US5 (T052): the dev-only data-flywheel contract tests. The `fw`
// fixtures materialize two complete fictional series roots — a core-only
// `dev-comparison` baseline and an official-dual candidate core leg — out of
// one shared sealed CoreExecutionPlanReceipt, so every digest on disk is
// genuine and every assertion below runs through the real readers.
//
// Everything is fictional: no skill, dataset, host CLI or endpoint is
// touched. No holdout material ever legitimately exists in these roots — the
// candidate deliberately binds (but never materializes) its holdout side and
// carries a *poisoned* holdout run tree, to prove the flywheel readers never
// traverse it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---------- fixtures ----------

func fwCases() map[string]*TriggerCaseV2 { return sciCasesBySplit()[SplitDevRegression] }

func fwCaseIDs() []string {
	cases := fwCases()
	ids := make([]string, 0, len(cases))
	for id := range cases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// fwPlan seals a core execution plan bound to the given frozen core manifest
// digest, with genuine freeze-before-digest self digests.
func fwPlan(t *testing.T, coreManifestDigest string) *CoreExecutionPlanReceipt {
	t.Helper()
	p := &CoreExecutionPlanReceipt{
		SchemaVersion:      1,
		PlanID:             "fw-plan",
		CoreManifestDigest: coreManifestDigest,
		RunnerRevision:     "fw-runner-rev",
		RunnerDigest:       "fw-runner-digest",
		JudgeRuleDigest:    "fw-judge-digest",
		Hosts:              rptHosts(),
		ToolIdentityDigests: map[string]string{
			HostClaude: "fw-tool-claude", HostCodex: "fw-tool-codex", HostOpenCode: "fw-tool-opencode",
		},
		TimeoutSeconds:                        240,
		Concurrency:                           2,
		CaseOrderSeeds:                        map[int]string{1: "fw-seed-1", 2: "fw-seed-2", 3: "fw-seed-3"},
		CoreBoundaryKind:                      BoundaryContainer,
		NormalizedCoreWorkerIdentitySetDigest: "fw-norm-worker",
		// The boundary template is derived the same way `series prepare`
		// derives it, from the isolation-config digest every fixture series
		// manifest carries in tool_configuration_digest.
		NormalizedCoreBoundaryTemplateDigest:  sha256Hex([]byte("boundary-template\x00" + string(BoundaryContainer) + "\x00fw-toolconfig")),
		NormalizedCoreExecutionTemplateDigest: "fw-norm-exec",
		CreatedAt:                             "2026-09-03T00:00:00Z",
	}
	d, err := corePlanReceiptDigest(p)
	if err != nil {
		t.Fatalf("plan digest: %v", err)
	}
	p.ReceiptDigest = d
	s, err := corePlanSealDigest(p)
	if err != nil {
		t.Fatalf("plan seal: %v", err)
	}
	p.SealDigest = s
	if err := ValidateCoreExecutionPlan(p); err != nil {
		t.Fatalf("sealed fixture plan must validate: %v", err)
	}
	return p
}

// fwSharedPlan seals one plan bound to the digest the frozen dev core dataset
// materializes to, so both fixture roots can import the exact same receipt.
func fwSharedPlan(t *testing.T) *CoreExecutionPlanReceipt {
	t.Helper()
	digest := sciWriteDataset(t, t.TempDir(), MembershipCore172, SplitDevRegression, fwCases())
	return fwPlan(t, digest)
}

func fwProvenance(host string) ToolProvenance {
	return ToolProvenance{
		Host: host, ResolvedModel: "fw-model-" + host, BillingClass: BillingFree,
		SourceRevision: "fw-source", CapturedAt: "2026-09-03T00:00:00Z",
		ToolIdentityDigest: "fw-tool-" + host,
	}
}

type fwRootSpec struct {
	seriesID    string
	purpose     SeriesPurpose
	skillDigest string
	snapshot    string
	plan        *CoreExecutionPlanReceipt
	// outcome decides every dev cell's terminal status; nil → all pass.
	outcome func(host string, ordinal int, caseID string) string
}

// fwCellOutcome turns a flat list of failing cells into an outcome matrix.
type fwCell struct {
	host     string
	caseID   string
	ordinals []int
	status   string // fail | runner-error
}

func fwOutcomeMatrix(cells []fwCell) func(string, int, string) string {
	return func(h string, o int, id string) string {
		for _, c := range cells {
			if c.host != h || c.caseID != id {
				continue
			}
			for _, x := range c.ordinals {
				if x == o {
					return c.status
				}
			}
		}
		return CaseStatusPass
	}
}

// fwCellArtifacts is the artifact triple + honest verdict of one fixture cell.
func fwCellArtifacts(t *testing.T, c *TriggerCaseV2, status string) (normalized, raw, dump []byte, verdict Verdict) {
	t.Helper()
	switch status {
	case CaseStatusPass:
		normalized, raw, dump = sciArtifacts(t, c)
	case CaseStatusFail:
		ev := []Event{}
		if !c.Expect.Trigger {
			op := "search"
			if strings.HasSuffix(c.Module, "-write-neg") {
				op = "write"
			}
			ev = []Event{{Kind: EventEngramCall, Op: op, Via: "mcp"}}
		}
		normalized, raw, dump = sciJSON(t, ev), sciJSON(t, ev), nil
	case CaseStatusRunnerError:
		normalized, raw, dump = sciJSON(t, []Event{}), sciJSON(t, []Event{}), nil
	default:
		t.Fatalf("unknown fixture status %q", status)
	}
	v, err := RejudgeFromRecordedCase(status, normalized, dump, c)
	if err != nil {
		t.Fatalf("fixture case %s (%s) must rejudge: %v", c.ID, status, err)
	}
	return normalized, raw, dump, v
}

// fwManifest builds the purpose-correct series manifest of one fixture root,
// with genuinely sealed package-validation and series-prepare receipts bound.
func fwManifest(spec fwRootSpec, coreManifestDigest string) *FormalSeriesManifest {
	m := &FormalSeriesManifest{
		SeriesID: spec.seriesID, Purpose: spec.purpose, State: StateSealed,
		SkillSnapshotDigest: spec.snapshot, SkillSnapshotAnchorDigest: spec.snapshot + "-anchor",
		SkillVersion: "0.2.8-fw", SkillDigest: spec.skillDigest,
		RunnerRevision: spec.plan.RunnerRevision, RunnerDigest: spec.plan.RunnerDigest,
		JudgeRuleDigest:         spec.plan.JudgeRuleDigest,
		CoreExecutionPlanDigest: spec.plan.ReceiptDigest,
		DatasetManifests:        map[string]string{MembershipCore172: coreManifestDigest},
		Hosts:                   append([]string(nil), spec.plan.Hosts...),
		RequiredOrdinals:        []int{1, 2, 3},
		TimeoutSeconds:          spec.plan.TimeoutSeconds, Concurrency: spec.plan.Concurrency,
		ExecutionEnvironmentDigest:    spec.plan.NormalizedCoreExecutionTemplateDigest,
		ToolConfigurationDigest:       "fw-toolconfig",
		CaseOrderSeeds:                copySeedMap(spec.plan.CaseOrderSeeds),
		QuestionCount:                 map[string]int{MembershipCore172: 172},
		WorkspaceCanaryReceiptDigests: map[string]map[int]string{},
	}
	if spec.purpose == PurposeOfficialDual {
		m.DatasetManifests[MembershipHoldout96] = "fw-holdout-manifest"
		m.QuestionCount[MembershipHoldout96] = 96
		m.CandidateBindingDigest = "fw-candidate-binding-" + spec.seriesID
		m.ProtectedExecutionReceiptDigest = "fw-protected-" + spec.seriesID
		m.ProtectedExecutionPolicyDigest = "fw-policy-" + spec.seriesID
	}
	rptPackage(m)
	rptGreenPrepare(m)
	return m
}

// fwWriteSeriesRoot materializes one complete fictional series root: a sealed
// dev-regression core dataset, genuinely sealed run manifests and the full
// per-case receipt/artifact tree of its 3 host × 3 ordinal core leg.
func fwWriteSeriesRoot(t *testing.T, spec fwRootSpec) (string, *FormalSeriesManifest) {
	t.Helper()
	root := t.TempDir()
	cases := fwCases()
	ids := fwCaseIDs()
	coreManifestDigest := sciWriteDataset(t, root, MembershipCore172, SplitDevRegression, cases)
	if coreManifestDigest != spec.plan.CoreManifestDigest {
		t.Fatalf("fixture core manifest digest %s != plan bound %s", coreManifestDigest, spec.plan.CoreManifestDigest)
	}
	m := fwManifest(spec, coreManifestDigest)
	pkg := rptPackage(m)
	prep := rptGreenPrepare(m)
	digest, err := seriesManifestDigest(m)
	if err != nil {
		t.Fatalf("series manifest digest: %v", err)
	}
	m.ManifestDigest = digest
	for _, w := range []struct {
		name string
		v    any
	}{
		{corePlanFile, spec.plan},
		{packageValidationFile, pkg},
		{greenSeriesPrepareFile, prep},
		{seriesManifestFile, m},
	} {
		if err := osWriteFile(filepath.Join(root, w.name), sciJSON(t, w.v)); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range rptHosts() {
		for _, o := range Ordinals {
			runRoot := PrimaryRunRoot(root, h, SplitDevRegression, o)
			for _, id := range ids {
				status := CaseStatusPass
				if spec.outcome != nil {
					status = spec.outcome(h, o, id)
				}
				normalized, raw, dump, verdict := fwCellArtifacts(t, cases[id], status)
				caseDir := filepath.Join(runRoot, casesDirName, id)
				normPath := filepath.Join(caseDir, "normalized-events.json")
				rawPath := filepath.Join(caseDir, "raw.jsonl")
				dumpPath := filepath.Join(caseDir, "store-dump.txt")
				for _, f := range []struct {
					p string
					b []byte
				}{{normPath, normalized}, {rawPath, raw}, {dumpPath, dump}} {
					if err := osWriteFile(f.p, f.b); err != nil {
						t.Fatal(err)
					}
				}
				receipt := &CaseRunReceipt{
					CaseID:                          id,
					CasePayloadDigest:               "fw-payload-" + id,
					WorkspaceDigest:                 "fw-ws-" + id,
					CaseStateIsolationReceiptDigest: "fw-iso-" + h + "-" + id + "-" + itoa(o),
					AttemptCount:                    1,
					Status:                          status,
					NormalizedEventsPath:            normPath, NormalizedEventsDigest: sha256Hex(normalized),
					RawEventsPath: rawPath, RawEventsDigest: sha256Hex(raw),
					StoreDumpPath: dumpPath, StoreDumpDigest: sha256Hex(dump),
					Verdict:      verdict,
					DurationMS:   500,
					StderrDigest: "fw-stderr-" + id,
				}
				if err := osWriteFile(filepath.Join(caseDir, caseReceiptName), sciJSON(t, receipt)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := SealPrimaryRun(PrimaryRunInput{
				Root: root, SeriesID: m.SeriesID, Host: h, Split: SplitDevRegression, Ordinal: o,
				Plan: spec.plan, ToolProvenance: fwProvenance(h),
				CaseIDs: ids, CaseOrder: ids,
				StartedAt: "2026-09-03T00:00:00Z", CompletedAt: "2026-09-03T01:00:00Z",
				OutPath: filepath.Join(runRoot, runManifestName),
			}); err != nil {
				t.Fatalf("seal core run %s/%d: %v", h, o, err)
			}
		}
	}
	return root, m
}

// fwBaselineCells is the frozen baseline failure matrix the flywheel round is
// exercised with: three median-fail cells, one conservative runner-error
// median-fail, and one single-ordinal failure whose median stays a pass.
func fwBaselineCells() []fwCell {
	return []fwCell{
		{host: HostClaude, caseID: "rpt-iwp-001", ordinals: []int{1, 2, 3}, status: CaseStatusFail},
		{host: HostCodex, caseID: "rpt-irp-002", ordinals: []int{1, 2, 3}, status: CaseStatusFail},
		{host: HostOpenCode, caseID: "rpt-reg-003", ordinals: []int{1, 3}, status: CaseStatusFail},
		{host: HostClaude, caseID: "rpt-iwp-005", ordinals: []int{2}, status: CaseStatusFail},
		{host: HostCodex, caseID: "rpt-twn-001", ordinals: []int{1, 2, 3}, status: CaseStatusRunnerError},
	}
}

// fwBaselineRoot writes the dev-comparison baseline root of the round.
func fwBaselineRoot(t *testing.T, plan *CoreExecutionPlanReceipt) (string, *FormalSeriesManifest) {
	t.Helper()
	return fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-baseline", purpose: PurposeDevComparison,
		skillDigest: "fw-skill-before", snapshot: "fw-snapshot-before",
		plan: plan, outcome: fwOutcomeMatrix(fwBaselineCells()),
	})
}

// fwRootCauses classifies exactly the four median-fail cells of the baseline
// (the single-ordinal failure of rpt-iwp-005 is a median pass and stays
// unclassified).
func fwRootCauses() map[string]DevRootCause {
	return map[string]DevRootCause{
		HostClaude + "/rpt-iwp-001":   RootCauseMissingTrigger,
		HostCodex + "/rpt-irp-002":    RootCauseNarrowDescription,
		HostOpenCode + "/rpt-reg-003": RootCauseHostSpecific,
		HostCodex + "/rpt-twn-001":    RootCauseContradictoryBody,
	}
}

// fwMedianFailKeys is the expected archive key set of the baseline above: the
// single-ordinal failure of rpt-iwp-005 stays a median pass and is absent.
func fwMedianFailKeys() map[string]bool {
	return map[string]bool{
		HostClaude + "/rpt-iwp-001":   true,
		HostCodex + "/rpt-irp-002":    true,
		HostOpenCode + "/rpt-reg-003": true,
		HostCodex + "/rpt-twn-001":    true,
	}
}

func fwCellKey(host, caseID string) string { return host + "/" + caseID }

// fwArchiveOfCells builds, seals and writes the archive of a root whose
// failure cells are exactly `cells`, classifying every median-fail cell from
// the closed dev enum (a single-ordinal failure is a median pass and stays
// unclassified).
func fwArchiveOfCells(t *testing.T, root string, cells []fwCell) (*FailureArchive, string) {
	t.Helper()
	causes := map[string]DevRootCause{}
	for _, c := range cells {
		fails := len(c.ordinals)
		if fails < 2 {
			continue // MedianBool of three: one pass out of three is still a pass
		}
		if c.status == CaseStatusRunnerError {
			causes[fwCellKey(c.host, c.caseID)] = RootCauseHostSpecific
			continue
		}
		causes[fwCellKey(c.host, c.caseID)] = RootCauseMissingTrigger
	}
	archive, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: causes})
	if err != nil {
		t.Fatalf("BuildFailureArchive: %v", err)
	}
	path := filepath.Join(t.TempDir(), "failure-archive.json")
	if err := WriteFailureArchive(path, archive); err != nil {
		t.Fatalf("WriteFailureArchive: %v", err)
	}
	return archive, path
}

// fwArchiveOf builds, seals and writes the baseline archive of the round.
func fwArchiveOf(t *testing.T, root string) (*FailureArchive, string) {
	t.Helper()
	return fwArchiveOfCells(t, root, fwBaselineCells())
}

// ---------- FailureArchive construction ----------

func TestFailureArchiveBuildsSealedDevArchive(t *testing.T) {
	plan := fwSharedPlan(t)
	root, manifest := fwBaselineRoot(t, plan)
	archive, path := fwArchiveOf(t, root)

	if archive.SchemaVersion != 1 {
		t.Fatalf("archive schema_version %d, want 1", archive.SchemaVersion)
	}
	if archive.BaselineSkillSnapshotDigest != manifest.SkillSnapshotDigest ||
		archive.CoreExecutionPlanDigest != plan.ReceiptDigest {
		t.Fatal("the archive must bind the baseline snapshot and the shared core plan")
	}
	for _, h := range rptHosts() {
		if archive.ToolIdentityDigests[h] != plan.ToolIdentityDigests[h] {
			t.Errorf("archive tool identity for %s drifted from the plan", h)
		}
	}
	got := map[string]bool{}
	for _, e := range archive.Entries {
		key := fwCellKey(e.Host, e.CaseID)
		if !fwMedianFailKeys()[key] {
			t.Errorf("unexpected archive entry %s (not a median-fail cell)", key)
		}
		got[key] = true
		if e.Split != SplitDevRegression {
			t.Errorf("entry %s carries split %q, want dev-regression", key, e.Split)
		}
		if e.BaselineSeriesID != manifest.SeriesID {
			t.Errorf("entry %s names series %q", key, e.BaselineSeriesID)
		}
		if e.BinaryMedian != 0 {
			t.Errorf("entry %s claims median %d; only failures are archived", key, e.BinaryMedian)
		}
		if len(e.OrdinalStates) != 3 {
			t.Fatalf("entry %s carries %d ordinal states, want 3", key, len(e.OrdinalStates))
		}
		for i, st := range e.OrdinalStates {
			if st != CaseStatusPass && st != CaseStatusFail && st != CaseStatusRunnerError {
				t.Errorf("entry %s ordinal %d state %q is outside the closed set", key, i+1, st)
			}
		}
		if !failureClassesV2[e.FailureClass] {
			t.Errorf("entry %s failure class %q is outside the closed v2 set", key, e.FailureClass)
		}
		if !ValidDevRootCause(DevRootCause(e.RootCause)) {
			t.Errorf("entry %s root cause %q is outside the closed dev enum", key, e.RootCause)
		}
		if len(e.BaselineRunDigests) != 3 {
			t.Errorf("entry %s binds %d run digests, want the three ordinals", key, len(e.BaselineRunDigests))
		}
		if e.BeforeSeriesManifest != manifest.ManifestDigest {
			t.Errorf("entry %s before-manifest %q != sealed baseline manifest", key, e.BeforeSeriesManifest)
		}
		// An archive is frozen before any fix exists: the after-side fields
		// belong to the comparison receipt, never to the baseline archive.
		if e.FixSkillVersion != "" || e.FixSkillDigest != "" || e.AfterSeriesManifest != "" {
			t.Errorf("entry %s carries after-side state in an immutable baseline archive", key)
		}
	}
	if len(got) != len(fwMedianFailKeys()) {
		t.Fatalf("archive carries %d entries, want exactly the %d median-fail cells", len(got), len(fwMedianFailKeys()))
	}
	if err := VerifyFailureArchiveSeal(archive); err != nil {
		t.Fatalf("the built archive must verify its own seal: %v", err)
	}
	loaded, err := LoadFailureArchive(path)
	if err != nil {
		t.Fatalf("LoadFailureArchive: %v", err)
	}
	if loaded.ArchiveDigest != archive.ArchiveDigest || loaded.SealDigest != archive.SealDigest {
		t.Fatal("the loaded archive must be byte-identical in its digests to the built one")
	}
	// The runner-error median-fail cell keeps its terminal class.
	for _, e := range archive.Entries {
		if e.CaseID == "rpt-twn-001" && e.FailureClass != FailureRunnerError {
			t.Fatalf("a terminal runner-error cell must archive the runner-error class, got %q", e.FailureClass)
		}
	}
	// No official score/headline vocabulary may exist in a dev archive.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"dev_regression", "generalization", "overall_verdict", "passed", "gate"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("dev archive carries the official-score vocabulary %q", forbidden)
		}
	}
}

func TestFailureArchiveClosedRootCauseValidation(t *testing.T) {
	plan := fwSharedPlan(t)
	root, _ := fwBaselineRoot(t, plan)

	causes := fwRootCauses()
	// (a) an unclassified median-fail cell is never archived.
	incomplete := map[string]DevRootCause{}
	for k, v := range causes {
		if k != HostCodex+"/rpt-twn-001" {
			incomplete[k] = v
		}
	}
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: incomplete}); err == nil ||
		!strings.Contains(err.Error(), "rpt-twn-001") {
		t.Fatalf("an unclassified median-fail cell must fail closed naming the cell, got %v", err)
	}
	// (b) a value outside the closed dev enum is rejected.
	unknown := fwRootCauses()
	unknown[HostClaude+"/rpt-iwp-001"] = DevRootCause("flaky-host")
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: unknown}); err == nil ||
		!strings.Contains(err.Error(), "flaky-host") {
		t.Fatalf("an unknown root cause must fail closed, got %v", err)
	}
	// (c) classifying a median-pass cell is an error, not a silent extra.
	extra := fwRootCauses()
	extra[HostClaude+"/rpt-iwp-007"] = RootCauseMissingTrigger
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: extra}); err == nil ||
		!strings.Contains(err.Error(), "rpt-iwp-007") {
		t.Fatalf("an extraneous classification must fail closed naming the cell, got %v", err)
	}
	// (d) no classification at all is rejected.
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root}); err == nil {
		t.Fatal("a failure archive without any root-cause classification must fail closed")
	}
	// (e) tampering with a sealed archive drifts its seal.
	_, path := fwArchiveOf(t, root)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var loaded FailureArchive
	if err := StrictParseClosed(b, &loaded); err != nil {
		t.Fatal(err)
	}
	loaded.Entries[0].RootCause = string(RootCauseNarrowDescription)
	if err := VerifyFailureArchiveSeal(&loaded); err == nil {
		t.Fatal("a post-seal root-cause edit must drift the archive seal")
	}
}

func TestFailureArchiveRejectsHoldoutAndOfficialDual(t *testing.T) {
	plan := fwSharedPlan(t)
	// (a) an official-dual series can never be the dev baseline archive.
	dualRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-dual", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-before", snapshot: "fw-snapshot-before", plan: plan,
	})
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: dualRoot, RootCauses: fwRootCauses()}); err == nil ||
		!strings.Contains(err.Error(), string(PurposeOfficialDual)) {
		t.Fatalf("an official-dual series must be refused as a dev archive input, got %v", err)
	}
	// (b) a dev-comparison root whose core run manifest was relabelled
	// split=holdout is rejected — the archive never reads holdout splits.
	root, _ := fwBaselineRoot(t, plan)
	runManifest := filepath.Join(PrimaryRunRoot(root, HostClaude, SplitDevRegression, 1), runManifestName)
	r := &PrimaryRunManifest{}
	b, err := os.ReadFile(runManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := StrictParseClosed(b, r); err != nil {
		t.Fatal(err)
	}
	r.Split = SplitHoldout
	if err := osWriteFile(runManifest, sciJSON(t, r)); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: fwRootCauses()}); err == nil ||
		!strings.Contains(err.Error(), SplitHoldout) {
		t.Fatalf("a holdout-relabeled core run must be refused by the archive, got %v", err)
	}
	// (c) a drafted archive entry with a holdout split is rejected outright.
	entries := []FailureArchiveEntry{{
		CaseID: "rpt-h-iwp-001", Host: HostClaude, Split: SplitHoldout, BinaryMedian: 0,
		FailureClass: FailureFalseNegative, RootCause: string(RootCauseMissingTrigger),
	}}
	if err := validateFailureArchiveEntries(entries, "fw-baseline", "fw-plan"); err == nil ||
		!strings.Contains(err.Error(), "rpt-h-iwp-001") {
		t.Fatalf("holdout entries must never enter the dev archive, got %v", err)
	}
}

func TestFailureArchiveRejectsIncompleteOrDriftedSeries(t *testing.T) {
	plan := fwSharedPlan(t)
	root, _ := fwBaselineRoot(t, plan)
	expectErr := func(name string, err error, want ...string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected a fail-closed error", name)
		}
		for _, w := range want {
			if !strings.Contains(err.Error(), w) {
				t.Fatalf("%s: error %q does not mention %q", name, err, w)
			}
		}
	}
	// (a) one ordinal run missing → the series is incomplete, never archivable.
	runManifest := filepath.Join(PrimaryRunRoot(root, HostCodex, SplitDevRegression, 3), runManifestName)
	if err := os.Rename(runManifest, runManifest+".away"); err != nil {
		t.Fatal(err)
	}
	_, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: fwRootCauses()})
	expectErr("missing ordinal", err, "ordinal 3")
	if err := os.Rename(runManifest+".away", runManifest); err != nil {
		t.Fatal(err)
	}
	// (b) a drifted case artifact stops the archive: the medians must come
	// from receipts whose evidence still rejudges.
	target := filepath.Join(PrimaryRunRoot(root, HostClaude, SplitDevRegression, 1), casesDirName, "rpt-iwp-001", "store-dump.txt")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(original, []byte("rogue\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: fwRootCauses()})
	expectErr("drifted artifact", err, "post-run mutation")
	// (c) an empty root fails closed.
	if _, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: filepath.Join(root, "absent")}); err == nil {
		t.Fatal("an absent series root must be rejected")
	}
	// (d) a rewritten (post-seal mutated) series manifest is rejected.
	manifestPath := filepath.Join(root, seriesManifestFile)
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m := &FormalSeriesManifest{}
	if err := StrictParseClosed(mb, m); err != nil {
		t.Fatal(err)
	}
	m.SkillVersion = "0.9.9-rewritten"
	if err := osWriteFile(manifestPath, sciJSON(t, m)); err != nil {
		t.Fatal(err)
	}
	_, err = BuildFailureArchive(&FailureArchiveInput{SeriesRoot: root, RootCauses: fwRootCauses()})
	expectErr("rewritten manifest", err, "post-seal mutation")
}

// ---------- compare ----------

// fwCandidateCells is the post-revision core leg: every baseline median-fail
// cell turns into a pass, the single-ordinal failure stays a median pass, and
// one previously stable case regresses.
func fwCandidateCells() []fwCell {
	return []fwCell{
		{host: HostClaude, caseID: "rpt-iwp-005", ordinals: []int{2}, status: CaseStatusFail},
		{host: HostClaude, caseID: "rpt-iwp-010", ordinals: []int{1, 2, 3}, status: CaseStatusFail},
	}
}

func TestCompareDevSeriesFullFlywheelRound(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, baseline := fwBaselineRoot(t, plan)
	archive, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, candidate := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after",
		plan: plan, outcome: fwOutcomeMatrix(fwCandidateCells()),
	})

	in := &CompareDevSeriesInput{
		BaselineSeriesRoot:  baselineRoot,
		CandidateSeriesRoot: candidateRoot,
		FailureArchivePath:  archivePath,
		ExtensionReceiptPath: fwWriteExtensionReceipt(t, map[string]string{
			"rpt-iwp-001": "fw-ext-001", "rpt-irp-002": "fw-ext-002",
			"rpt-reg-003": "fw-ext-003", "rpt-twn-001": "fw-ext-004",
		}),
	}
	report, err := CompareDevSeries(in)
	if err != nil {
		t.Fatalf("CompareDevSeries: %v", err)
	}
	if report.CoreExecutionPlanDigest != plan.ReceiptDigest {
		t.Fatal("the comparison must be bound to the shared core plan")
	}
	if report.BaselineSeriesManifestDigest != baseline.ManifestDigest ||
		report.CandidateSeriesManifestDigest != candidate.ManifestDigest {
		t.Fatal("the comparison must bind both sealed series manifest digests")
	}
	if report.BaselineSkillDigest == report.CandidateSkillDigest {
		t.Fatal("the skill package digest is the single intentional variable")
	}
	if report.FailureArchiveDigest != archive.ArchiveDigest {
		t.Fatal("the comparison must bind the frozen baseline archive digest")
	}
	// Per-case binary medians: 172 core cases × 3 hosts, extension never adds
	// rows.
	if report.ComparedCoreCaseCount != 172 {
		t.Fatalf("compared core case count %d, want 172", report.ComparedCoreCaseCount)
	}
	if len(report.CaseMedians) != 172*3 {
		t.Fatalf("case medians carry %d rows, want 516", len(report.CaseMedians))
	}
	transitions := map[string]int{}
	for _, row := range report.CaseMedians {
		transitions[row.Transition]++
		if row.Transition == TransitionFailToPass && (row.BaselineMedian != 0 || row.CandidateMedian != 1) {
			t.Errorf("fail-to-pass row %s/%s carries medians %d→%d", row.Host, row.CaseID, row.BaselineMedian, row.CandidateMedian)
		}
		if row.Transition == TransitionRegression && (row.BaselineMedian != 1 || row.CandidateMedian != 0) {
			t.Errorf("regression row %s/%s carries medians %d→%d", row.Host, row.CaseID, row.BaselineMedian, row.CandidateMedian)
		}
	}
	if transitions[TransitionFailToPass] != 4 || transitions[TransitionRegression] != 1 {
		t.Fatalf("transitions %v, want 4 fail-to-pass and 1 regression", transitions)
	}
	if report.FailToPassCount != 4 || len(report.FailToPassCases) != 4 {
		t.Fatalf("fail-to-pass accounting is %d/%d, want 4/4", report.FailToPassCount, len(report.FailToPassCases))
	}
	// Every regression is counted, and the single one is named.
	if report.RegressionCount != 1 || len(report.RegressionCases) != 1 ||
		report.RegressionCases[0].CaseID != "rpt-iwp-010" || report.RegressionCases[0].Host != HostClaude {
		t.Fatalf("regressions must be counted and named, got %d/%v", report.RegressionCount, report.RegressionCases)
	}
	if report.FailToPassCount+report.RegressionCount+report.StablePassCount+report.StableFailCount != 516 {
		t.Fatal("the four transitions must exactly partition the compared cells")
	}
	// At least one frozen-baseline failure became a pass → SC-5 pass.
	if report.SC5Verdict != "pass" {
		t.Fatalf("a round with fail-to-pass improvements must record SC-5 pass, got %q", report.SC5Verdict)
	}
	// Sorted, deduplicated required backfill set with a stable digest. The
	// regressed rpt-iwp-010 is deliberately absent: only fail-to-pass cells
	// are backfilled.
	expected := []string{"rpt-irp-002", "rpt-iwp-001", "rpt-reg-003", "rpt-twn-001"}
	if strings.Join(report.RequiredExtensionBackfillSourceCaseIDs, ",") != strings.Join(expected, ",") {
		t.Fatalf("required backfill set %v, want %v", report.RequiredExtensionBackfillSourceCaseIDs, expected)
	}
	if report.RequiredExtensionBackfillDigest == "" {
		t.Fatal("the required backfill set must carry a digest")
	}
	if report.ExtensionBackfill == nil || !report.ExtensionBackfill.Verified {
		t.Fatal("a satisfied one-to-one backfill must be verified")
	}
	if len(report.ExtensionBackfill.Lineage) != 4 {
		t.Fatalf("backfill lineage %v, want exactly the four required mappings", report.ExtensionBackfill.Lineage)
	}
	// Extension diagnostics stay explicit and non-gating.
	if report.ExtensionDiagnostics == nil || !report.ExtensionDiagnostics.NonComparable || !report.ExtensionDiagnostics.NonGating {
		t.Fatal("extension diagnostics must be marked non-comparable and non-gating")
	}
	if !report.ExtensionSeparateFromCore {
		t.Fatal("extension results must be recorded as separate from the core172 denominator")
	}
	if err := VerifyFlywheelComparisonSeal(report); err != nil {
		t.Fatalf("the comparison must verify its own seal: %v", err)
	}
	// No official score family may be expressible in the receipt.
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"dev_regression", "generalization", "overall_verdict", "gate", "official_score"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("flywheel comparison carries the official-score vocabulary %q", forbidden)
		}
	}
	// Writing is frozen: a second write must refuse to overwrite.
	path := filepath.Join(t.TempDir(), "flywheel-comparison.json")
	if err := WriteFlywheelComparison(path, report); err != nil {
		t.Fatalf("write comparison: %v", err)
	}
	if err := WriteFlywheelComparison(path, report); err == nil {
		t.Fatal("a sealed comparison receipt is never rewritten")
	}
}

func TestCompareDevSeriesPerCellMedianRules(t *testing.T) {
	plan := fwSharedPlan(t)
	cells := []fwCell{
		// Fails only ordinal 2 → the binary median stays a pass.
		{host: HostClaude, caseID: "rpt-iwp-001", ordinals: []int{2}, status: CaseStatusFail},
		// Two of three fail → the binary median is a fail.
		{host: HostCodex, caseID: "rpt-irp-001", ordinals: []int{1, 3}, status: CaseStatusFail},
	}
	baselineRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-baseline", purpose: PurposeDevComparison,
		skillDigest: "fw-skill-before", snapshot: "fw-snapshot-before", plan: plan,
		outcome: fwOutcomeMatrix(cells),
	})
	archive, archivePath := fwArchiveOfCells(t, baselineRoot, cells)
	keys := map[string]bool{}
	for _, e := range archive.Entries {
		keys[fwCellKey(e.Host, e.CaseID)] = true
	}
	if keys[fwCellKey(HostClaude, "rpt-iwp-001")] {
		t.Fatal("a single-ordinal failure must stay a median pass and never be archived")
	}
	if !keys[fwCellKey(HostCodex, "rpt-irp-001")] {
		t.Fatal("a two-of-three failure must archive as a median fail")
	}
	// A candidate that reproduces the same median-fail (no improvement)
	// records SC-5 FAIL, not an error.
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-unchanged-behavior", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(cells),
	})
	report, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: candidateRoot, FailureArchivePath: archivePath,
	})
	if err != nil {
		t.Fatalf("a no-improvement round must still be comparable: %v", err)
	}
	if report.SC5Verdict != "fail" {
		t.Fatalf("a round without any fail-to-pass must record SC-5 FAIL, got %q", report.SC5Verdict)
	}
	if len(report.RequiredExtensionBackfillSourceCaseIDs) != 0 || report.ExtensionBackfill != nil {
		t.Fatal("a failed round must require no backfill")
	}
	if report.FailToPassCount != 0 || report.RegressionCount != 0 {
		t.Fatalf("an unchanged candidate must report zero transitions, got %d/%d", report.FailToPassCount, report.RegressionCount)
	}
	if report.StableFailCount != 1 {
		t.Fatalf("the reproduced median-fail must be counted stable-fail, got %d", report.StableFailCount)
	}
}

func TestCompareRequiresSharedPlanIdentityAndRevision(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, _ := fwBaselineRoot(t, plan)
	_, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
	})

	cases := []struct {
		name     string
		mutate   func(root string)
		wantErrs []string
	}{
		{
			name: "different core plan digest",
			mutate: func(root string) {
				other := fwPlan(t, plan.CoreManifestDigest)
				other.PlanID = "fw-plan-other"
				d, err := corePlanReceiptDigest(other)
				if err != nil {
					t.Fatal(err)
				}
				other.ReceiptDigest = d
				s, err := corePlanSealDigest(other)
				if err != nil {
					t.Fatal(err)
				}
				other.SealDigest = s
				if err := osWriteFile(filepath.Join(root, corePlanFile), sciJSON(t, other)); err != nil {
					t.Fatal(err)
				}
			},
			wantErrs: []string{"core execution plan"},
		},
		{
			name: "observed tool identity drifts from the plan",
			mutate: func(root string) {
				runPath := filepath.Join(PrimaryRunRoot(root, HostCodex, SplitDevRegression, 2), runManifestName)
				r, err := LoadPrimaryRun(runPath)
				if err != nil {
					t.Fatal(err)
				}
				r.ToolProvenance.ToolIdentityDigest = "fw-tool-drifted"
				d, err := primaryRunDigest(r)
				if err != nil {
					t.Fatal(err)
				}
				r.RunDigest = d
				s, err := primaryRunSealDigest(r)
				if err != nil {
					t.Fatal(err)
				}
				r.SealDigest = s
				if err := osWriteFile(runPath, sciJSON(t, r)); err != nil {
					t.Fatal(err)
				}
			},
			wantErrs: []string{"tool_identity_digest"},
		},
		{
			name: "series manifest disagrees with the shared plan timeout",
			mutate: func(root string) {
				p := filepath.Join(root, seriesManifestFile)
				m, err := LoadSeriesManifest(p)
				if err != nil {
					t.Fatal(err)
				}
				m.TimeoutSeconds = plan.TimeoutSeconds + 60
				d, err := seriesManifestDigest(m)
				if err != nil {
					t.Fatal(err)
				}
				m.ManifestDigest = d
				if err := osWriteFile(p, sciJSON(t, m)); err != nil {
					t.Fatal(err)
				}
			},
			wantErrs: []string{"timeout"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := copyRoot(t, candidateRoot, root); err != nil {
				t.Fatal(err)
			}
			c.mutate(root)
			_, err := CompareDevSeries(&CompareDevSeriesInput{
				BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: root, FailureArchivePath: archivePath,
			})
			if err == nil {
				t.Fatalf("expected a fail-closed error")
			}
			for _, w := range c.wantErrs {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error %q does not mention %q", err, w)
				}
			}
		})
	}
	// No-revision comparison: the skill package digest is the single
	// intentional variable, so an identical package is not a flywheel round.
	sameRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate-same", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-before", snapshot: "fw-snapshot-after", plan: plan,
	})
	if _, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: sameRoot, FailureArchivePath: archivePath,
	}); err == nil || !strings.Contains(err.Error(), "skill") {
		t.Fatalf("a comparison without a skill revision must be refused, got %v", err)
	}
}

func TestCompareReadsOnlyCandidateCorePaths(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, _ := fwBaselineRoot(t, plan)
	_, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(fwCandidateCells()),
	})
	// Poison the whole holdout side of the candidate: garbage run manifests,
	// a garbage holdout dataset manifest, no protected receipt, no pre-holdout
	// receipt, no binding. `compare` must never notice.
	for _, h := range rptHosts() {
		for _, o := range Ordinals {
			if err := osWriteFile(filepath.Join(PrimaryRunRoot(candidateRoot, h, SplitHoldout, o), runManifestName), []byte("{not json")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := osWriteFile(filepath.Join(candidateRoot, datasetsDir, MembershipHoldout96, datasetManifestName), []byte("\x00")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{protectedExecutionFile, greenPreHoldoutFile, holdoutBindingFile, officialScoreReportFile} {
		if err := osWriteFile(filepath.Join(candidateRoot, name), []byte("{poison")); err != nil {
			t.Fatal(err)
		}
	}
	report, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot:  baselineRoot,
		CandidateSeriesRoot: candidateRoot,
		FailureArchivePath:  archivePath,
		ExtensionReceiptPath: fwWriteExtensionReceipt(t, map[string]string{
			"rpt-iwp-001": "fw-ext-001", "rpt-irp-002": "fw-ext-002",
			"rpt-reg-003": "fw-ext-003", "rpt-twn-001": "fw-ext-004",
		}),
	})
	if err != nil {
		t.Fatalf("compare must not traverse the holdout side of the candidate: %v", err)
	}
	if report.SC5Verdict != "pass" {
		t.Fatalf("the poisoned holdout side must not affect the core comparison, got %q", report.SC5Verdict)
	}
	// Symmetrically, a holdout-poisoned baseline side is irrelevant: the
	// dev-comparison root never carries one, and the archive never asks.
	// A holdout-shaped path is refused by the core-only path guard directly.
	if _, err := coreOnlyRunPath(candidateRoot, filepath.Join(runsDir, "claude-holdout-o1", runManifestName)); err == nil {
		t.Fatal("a holdout run path must be refused by the core-only path guard")
	}
	if _, err := coreOnlyRunPath(candidateRoot, filepath.Join(datasetsDir, MembershipHoldout96, datasetManifestName)); err == nil {
		t.Fatal("a holdout dataset path must be refused by the core-only path guard")
	}
	if _, err := coreOnlyRunPath(candidateRoot, filepath.Join("..", "outside.json")); err == nil {
		t.Fatal("an escaping path must be refused by the core-only path guard")
	}
}

func TestCompareArchiveMustBeTheFrozenBaselineTruth(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, baseline := fwBaselineRoot(t, plan)
	_, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(fwCandidateCells()),
	})
	// (a) an archive of a different baseline series cannot authorize this
	// comparison.
	otherRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-other-baseline", purpose: PurposeDevComparison,
		skillDigest: "fw-skill-before", snapshot: "fw-snapshot-before", plan: plan,
		outcome: fwOutcomeMatrix(fwBaselineCells()),
	})
	_, otherArchive := fwArchiveOf(t, otherRoot)
	if _, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: candidateRoot, FailureArchivePath: otherArchive,
	}); err == nil || !strings.Contains(err.Error(), baseline.SeriesID) {
		t.Fatalf("an archive of another baseline must be refused, got %v", err)
	}
	// (b) a tampered archive (post-seal root-cause edit) drifts its seal.
	b, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	a := &FailureArchive{}
	if err := StrictParseClosed(b, a); err != nil {
		t.Fatal(err)
	}
	a.Entries[0].RootCause = string(RootCauseContradictoryBody)
	tampered := filepath.Join(t.TempDir(), "tampered-archive.json")
	if err := osWriteFile(tampered, sciJSON(t, a)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: candidateRoot, FailureArchivePath: tampered,
	}); err == nil || !strings.Contains(err.Error(), "seal") {
		t.Fatalf("a tampered archive must be refused by its seal, got %v", err)
	}
	// (c) an archive that omits one of the baseline's median-fail cells is not
	// the frozen truth of this baseline.
	partial := *a
	partial.Entries = append([]FailureArchiveEntry{}, a.Entries[1:]...)
	partial.ArchiveDigest = ""
	partial.SealDigest = ""
	if err := sealFailureArchive(&partial); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(t.TempDir(), "partial-archive.json")
	if err := osWriteFile(partialPath, sciJSON(t, &partial)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: candidateRoot, FailureArchivePath: partialPath,
	}); err == nil || !strings.Contains(err.Error(), "codex/rpt-irp-002") {
		t.Fatalf("an archive that is not the baseline's complete failure set must be refused, got %v", err)
	}
}

func TestCompareBackfillOneToOneVerification(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, _ := fwBaselineRoot(t, plan)
	_, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(fwCandidateCells()),
	})
	run := func(t *testing.T, lineage map[string]string, caseIDs []string) error {
		t.Helper()
		receipt := fwWriteExtensionReceiptWith(t, lineage, caseIDs)
		_, err := CompareDevSeries(&CompareDevSeriesInput{
			BaselineSeriesRoot:   baselineRoot,
			CandidateSeriesRoot:  candidateRoot,
			FailureArchivePath:   archivePath,
			ExtensionReceiptPath: receipt,
		})
		return err
	}
	full := map[string]string{
		"rpt-iwp-001": "fw-ext-001", "rpt-irp-002": "fw-ext-002",
		"rpt-reg-003": "fw-ext-003", "rpt-twn-001": "fw-ext-004",
	}
	membership := append([]string{}, "rpt-iwp-010") // a core case that regressed is not backfilled
	for id := range full {
		membership = append(membership, full[id])
	}
	// (a) the exact one-to-one mapping verifies.
	if err := run(t, full, membership); err != nil {
		t.Fatalf("the exact one-to-one backfill must verify: %v", err)
	}
	// (b) a missing mapping fails, naming the source ID.
	missing := copyStringMap(full)
	delete(missing, "rpt-reg-003")
	if err := run(t, missing, membership); err == nil || !strings.Contains(err.Error(), "rpt-reg-003") {
		t.Fatalf("a missing backfill mapping must fail closed naming the source, got %v", err)
	}
	// (c) two sources mapping to one successor is rejected.
	duplicate := copyStringMap(full)
	duplicate["rpt-reg-003"] = "fw-ext-004"
	if err := run(t, duplicate, membership); err == nil || !strings.Contains(err.Error(), "fw-ext-004") {
		t.Fatalf("a duplicated successor must be rejected, got %v", err)
	}
	// (d) an extra mapping beyond the required set is rejected.
	extra := copyStringMap(full)
	extra["rpt-iwn-001"] = "fw-ext-009"
	extraMembership := append(append([]string{}, membership...), "fw-ext-009")
	if err := run(t, extra, extraMembership); err == nil || !strings.Contains(err.Error(), "rpt-iwn-001") {
		t.Fatalf("an extra backfill mapping must be rejected, got %v", err)
	}
	// (e) a successor outside the extension manifest membership is rejected.
	orphan := copyStringMap(full)
	orphan["rpt-reg-003"] = "fw-ext-orphan"
	if err := run(t, orphan, membership); err == nil || !strings.Contains(err.Error(), "fw-ext-orphan") {
		t.Fatalf("a successor outside the manifest membership must be rejected, got %v", err)
	}
	// (f) a required backfill with no extension receipt at all fails closed.
	if _, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot: baselineRoot, CandidateSeriesRoot: candidateRoot, FailureArchivePath: archivePath,
	}); err == nil || !strings.Contains(err.Error(), "extension") {
		t.Fatalf("a required backfill without the extension receipt must fail closed, got %v", err)
	}
	// (g) a non-diagnostic or holdout extension receipt is rejected.
	for _, mutate := range []func(*DiagnosticRunReceipt){
		func(r *DiagnosticRunReceipt) { r.Mode = "primary" },
		func(r *DiagnosticRunReceipt) { r.Split = SplitHoldout },
		func(r *DiagnosticRunReceipt) { r.FormalScoreEligible = true },
	} {
		r := &DiagnosticRunReceipt{
			SchemaVersion: 1, Mode: "diagnostic", Split: SplitDevRegression, Tool: HostClaude,
			ManifestPath:        fwExtensionManifestPath(t, full, membership),
			FormalScoreEligible: false,
		}
		mutate(r)
		path := filepath.Join(t.TempDir(), "bad-extension-receipt.json")
		if err := osWriteFile(path, sciJSON(t, r)); err != nil {
			t.Fatal(err)
		}
		if _, err := CompareDevSeries(&CompareDevSeriesInput{
			BaselineSeriesRoot:   baselineRoot,
			CandidateSeriesRoot:  candidateRoot,
			FailureArchivePath:   archivePath,
			ExtensionReceiptPath: path,
		}); err == nil {
			t.Fatal("an unusable extension diagnostic receipt must be rejected")
		}
	}
}

func TestCompareExtensionStaysOutOfTheCoreDenominator(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, _ := fwBaselineRoot(t, plan)
	_, archivePath := fwArchiveOf(t, baselineRoot)
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(fwCandidateCells()),
	})
	lineage := map[string]string{
		"rpt-iwp-001": "fw-ext-001", "rpt-irp-002": "fw-ext-002",
		"rpt-reg-003": "fw-ext-003", "rpt-twn-001": "fw-ext-004",
	}
	membership := []string{"rpt-iwp-010", "fw-ext-001", "fw-ext-002", "fw-ext-003", "fw-ext-004"}
	report, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot:   baselineRoot,
		CandidateSeriesRoot:  candidateRoot,
		FailureArchivePath:   archivePath,
		ExtensionReceiptPath: fwWriteExtensionReceiptWith(t, lineage, membership),
	})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if report.ComparedCoreCaseCount != 172 {
		t.Fatalf("extension cases must not widen the core denominator, got %d", report.ComparedCoreCaseCount)
	}
	for _, row := range report.CaseMedians {
		if strings.HasPrefix(row.CaseID, "fw-ext-") {
			t.Fatalf("extension case %s entered the core median comparison", row.CaseID)
		}
	}
	for _, id := range report.RequiredExtensionBackfillSourceCaseIDs {
		if strings.HasPrefix(id, "fw-ext-") {
			t.Fatalf("extension case %s is not a core172 source ID", id)
		}
	}
}

// ---------- CLI wiring ----------

func TestFlywheelCommandsWiredAndFailClosed(t *testing.T) {
	for _, cmd := range []string{"failure-archive", "compare"} {
		if handled, _ := routeV2([]string{cmd}); !handled {
			t.Errorf("wired command %q must be claimed by the v2 router", cmd)
		}
	}
	if err := cmdFailureArchive([]string{}); err == nil {
		t.Error("failure-archive without --series-root/--out must fail closed")
	}
	if err := cmdCompare([]string{}); err == nil {
		t.Error("compare without its series roots must fail closed")
	}
	// No invocation may ask either command for an official score/headline or
	// for holdout material.
	forbidden := [][]string{
		{"--series-root", t.TempDir(), "--out", filepath.Join(t.TempDir(), "a.json"), "--official-score"},
		{"--series-root", t.TempDir(), "--out", filepath.Join(t.TempDir(), "a.json"), "--include-holdout"},
	}
	for _, argv := range forbidden {
		if err := cmdFailureArchive(argv); err == nil || !strings.Contains(err.Error(), "official score") {
			t.Errorf("failure-archive must reject %v, got %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"--baseline-series-root", t.TempDir(), "--candidate-series-root", t.TempDir(),
			"--failure-archive", "x.json", "--out", filepath.Join(t.TempDir(), "c.json"), "--official-score"},
	} {
		if err := cmdCompare(argv); err == nil || !strings.Contains(err.Error(), "official score") {
			t.Errorf("compare must reject %v, got %v", argv, err)
		}
	}
	// The usage text documents both commands.
	documented := false
	for _, line := range strings.Split(usageV2, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "failure-archive") || strings.HasPrefix(strings.TrimSpace(line), "compare ") {
			documented = true
		}
	}
	if !documented {
		t.Error("usage must document the wired flywheel commands")
	}
}

func TestFlywheelCommandsEndToEnd(t *testing.T) {
	plan := fwSharedPlan(t)
	baselineRoot, _ := fwBaselineRoot(t, plan)
	archivePath := filepath.Join(t.TempDir(), "failure-archive.json")
	causesPath := filepath.Join(t.TempDir(), "root-causes.json")
	causes := map[string]string{}
	for k, v := range fwRootCauses() {
		causes[k] = string(v)
	}
	if err := osWriteFile(causesPath, sciJSON(t, causes)); err != nil {
		t.Fatal(err)
	}
	if err := cmdFailureArchive([]string{
		"--series-root", baselineRoot, "--root-causes", causesPath, "--out", archivePath,
	}); err != nil {
		t.Fatalf("failure-archive CLI: %v", err)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("the archive must be written: %v", err)
	}
	if err := cmdFailureArchive([]string{
		"--series-root", baselineRoot, "--root-causes", causesPath, "--out", archivePath,
	}); err == nil {
		t.Fatal("a frozen archive is never rewritten")
	}
	candidateRoot, _ := fwWriteSeriesRoot(t, fwRootSpec{
		seriesID: "fw-candidate", purpose: PurposeOfficialDual,
		skillDigest: "fw-skill-after", snapshot: "fw-snapshot-after", plan: plan,
		outcome: fwOutcomeMatrix(fwCandidateCells()),
	})
	extReceipt := fwWriteExtensionReceipt(t, map[string]string{
		"rpt-iwp-001": "fw-ext-001", "rpt-irp-002": "fw-ext-002",
		"rpt-reg-003": "fw-ext-003", "rpt-twn-001": "fw-ext-004",
	})
	out := filepath.Join(t.TempDir(), "flywheel-comparison.json")
	if err := cmdCompare([]string{
		"--baseline-series-root", baselineRoot,
		"--candidate-series-root", candidateRoot,
		"--failure-archive", archivePath,
		"--extension-receipt", extReceipt,
		"--out", out,
	}); err != nil {
		t.Fatalf("compare CLI: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	report := &FlywheelComparisonReceipt{}
	if err := StrictParseClosed(b, report); err != nil {
		t.Fatalf("the written comparison must reparse: %v", err)
	}
	if err := VerifyFlywheelComparisonSeal(report); err != nil {
		t.Fatalf("the written comparison must verify: %v", err)
	}
	if report.SC5Verdict != "pass" {
		t.Fatalf("CLI comparison verdict %q, want pass", report.SC5Verdict)
	}
}

// ---------- fixture plumbing ----------

// fwExtensionManifestPath writes a fictional sealed dev-extension manifest
// carrying the given lineage and membership, and returns its path.
func fwExtensionManifestPath(t *testing.T, lineage map[string]string, caseIDs []string) string {
	t.Helper()
	if caseIDs == nil {
		caseIDs = []string{}
	}
	sorted := append([]string(nil), caseIDs...)
	sort.Strings(sorted)
	payloadIDs := sorted
	m := &DatasetManifestV2{
		SchemaVersion:    2,
		Canonicalization: CanonicalizationName,
		DatasetID:        "fw-extension",
		DatasetVersion:   "fw-ext-v1",
		Split:            SplitDevRegression,
		ScoreMembership:  MembershipDevExt,
		CaseCount:        len(payloadIDs),
		ModuleCounts:     map[string]int{},
		LanguageCounts:   map[string]int{},
		CaseIDs:          payloadIDs,
		PayloadFiles:     []PayloadFileV1{},
		PayloadDigest:    "fw-ext-empty-payload",
		ExtensionLineage: lineage,
	}
	idsDigest, err := CaseIDsDigest(payloadIDs)
	if err != nil {
		t.Fatal(err)
	}
	m.CaseIDsDigest = idsDigest
	path := filepath.Join(t.TempDir(), "dev-extension.manifest.json")
	if err := osWriteFile(path, sciJSON(t, m)); err != nil {
		t.Fatal(err)
	}
	return path
}

// fwWriteExtensionReceiptWith writes a diagnostic extension receipt whose
// manifest carries the given lineage/membership.
func fwWriteExtensionReceiptWith(t *testing.T, lineage map[string]string, caseIDs []string) string {
	t.Helper()
	r := &DiagnosticRunReceipt{
		SchemaVersion: 1, Mode: "diagnostic", Split: SplitDevRegression, Tool: HostClaude,
		DatasetDir:   t.TempDir(),
		ManifestPath: fwExtensionManifestPath(t, lineage, caseIDs),
		OutRoot:      t.TempDir(), ScratchRoot: t.TempDir(),
		Concurrency: 2, ObservedMaxInFlight: 2, ObservedOverlap: true,
		CaseCount:           len(caseIDs),
		FormalScoreEligible: false,
		RunnerDigest:        "fw-runner-digest", CreatedAt: "2026-09-03T00:00:00Z",
	}
	path := filepath.Join(t.TempDir(), "extension-diagnostic.json")
	if err := osWriteFile(path, sciJSON(t, r)); err != nil {
		t.Fatal(err)
	}
	return path
}

// fwWriteExtensionReceipt is the satisfied-backfill variant used by the
// end-to-end round.
func fwWriteExtensionReceipt(t *testing.T, lineage map[string]string) string {
	t.Helper()
	ids := []string{"rpt-iwp-010"}
	for source := range lineage {
		ids = append(ids, source, lineage[source])
	}
	return fwWriteExtensionReceiptWith(t, lineage, ids)
}

// copyRoot clones a materialized series root so a test can mutate one copy.
// Case receipts cite absolute artifact paths, so they are rewritten to point
// inside the clone; their artifact digests are unchanged.
func copyRoot(t *testing.T, src, dst string) error {
	t.Helper()
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if filepath.Base(p) == caseReceiptName {
			r := &CaseRunReceipt{}
			if err := StrictParseClosed(b, r); err != nil {
				return err
			}
			rewrite := func(old string) (string, error) {
				if old == "" {
					return "", nil
				}
				relArtifact, err := filepath.Rel(src, old)
				if err != nil {
					return "", err
				}
				return filepath.Join(dst, relArtifact), nil
			}
			if r.NormalizedEventsPath, err = rewrite(r.NormalizedEventsPath); err != nil {
				return err
			}
			if r.RawEventsPath, err = rewrite(r.RawEventsPath); err != nil {
				return err
			}
			if r.StoreDumpPath, err = rewrite(r.StoreDumpPath); err != nil {
				return err
			}
			b = sciJSON(t, r)
		}
		return osWriteFile(target, b)
	})
}
