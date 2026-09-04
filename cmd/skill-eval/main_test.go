package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMainRoutingV2Commands(t *testing.T) {
	// The v2 command surface routes before the legacy harness.
	for _, cmd := range []string{"family-index", "green-test", "package"} {
		if handled, err := routeV2([]string{cmd}); !handled {
			t.Errorf("command %q must be handled by the v2 router", cmd)
		} else if err == nil {
			t.Errorf("command %q without a subcommand must error", cmd)
		}
	}
	if handled, _ := routeV2([]string{"run"}); handled {
		t.Error("run routes through the mode-aware legacy bridge, not the v2 router")
	}
}

func TestGreenTestCreateArgvDiscipline(t *testing.T) {
	// Fixed suite allowlist: unknown and arbitrary suites are rejected before
	// any command runs.
	if err := cmdGreenTestCreate([]string{"--suite", "deploy-prod", "--out", "x.json"}); err == nil {
		t.Fatal("arbitrary suite names must be rejected")
	}
	if err := cmdGreenTestCreate([]string{"--suite", "rm -rf /", "--out", "x.json"}); err == nil {
		t.Fatal("shell-text suites must be rejected")
	}
	if err := cmdGreenTestCreate([]string{"--suite", SuiteFormalTooling}); err == nil {
		t.Fatal("missing --out must be rejected")
	}
	if err := cmdGreenTestCreate(nil); err == nil {
		t.Fatal("missing required arguments must be rejected")
	}
	// The fixed argv sets never embed caller text.
	for _, suite := range fixedSuitesOrder() {
		for _, argv := range suiteArgvSets(suite) {
			if strings.Contains(argv, "x.json") || strings.Contains(argv, "deploy") {
				t.Fatalf("suite %s argv set carries caller text: %s", suite, argv)
			}
		}
	}
}

func fixedSuitesOrder() []string {
	return []string{SuiteHoldoutPipeline, SuiteFormalTooling, SuiteSeriesPrepare, SuitePreHoldout}
}

func TestPackageValidateArgvRequirements(t *testing.T) {
	if err := cmdPackageValidate(nil); err == nil {
		t.Fatal("package validate without arguments must be rejected")
	}
	if err := cmdPackageValidate([]string{"--skill-dir", "s"}); err == nil {
		t.Fatal("package validate with partial arguments must be rejected")
	}
	err := cmdPackageValidate([]string{
		"--skill-dir", "s", "--repository-root", "r", "--snapshot-root", "n",
		"--green-test-receipt", "g", "--out", "o.json",
	})
	// With all flags present the command proceeds to the green gate and must
	// fail on the missing receipt file — not on argument validation.
	if err == nil || !strings.Contains(err.Error(), "green") {
		t.Fatalf("full-argv package validate should fail at the green gate: %v", err)
	}
}

func TestUsageDocumentsV2Surface(t *testing.T) {
	// The usage text must document the pre-T018 commands so the frozen CLI is
	// discoverable before any formal run.
	for _, want := range []string{"package validate", "green-test create", "family-index build", "--mode diagnostic"} {
		if !strings.Contains(usageV2, want) {
			t.Errorf("usage missing %q", want)
		}
	}
}

func TestValidateAndRunModeRouting(t *testing.T) {
	// validate with v2 flags routes to the split/phase-aware command.
	if err := cmdValidateV2([]string{"--split", "holdout", "--phase", "pre-index"}); err == nil {
		t.Fatal("holdout pre-index must be invalid (pre-index is dev-only)")
	}
	// run --mode is required for the v2 path.
	if err := cmdRunV2(nil); err == nil || !strings.Contains(err.Error(), "--mode") {
		t.Fatalf("run without --mode must be rejected: %v", err)
	}
	// primary mode is gated on a prepared formal series (T045+).
	if err := cmdRunV2([]string{"--mode", "primary", "--split", "dev-regression"}); err == nil {
		t.Fatal("primary runs without a prepared series must be rejected")
	}
}

// ---------- T038: US4 command-surface tests ----------

// csCorePlan returns a fully populated sealed core execution plan.
func csCorePlan() *CoreExecutionPlanReceipt {
	return &CoreExecutionPlanReceipt{
		SchemaVersion:      1,
		PlanID:             "plan-fx",
		CoreManifestDigest: "core-manifest-digest",
		RunnerRevision:     "runner-rev",
		RunnerDigest:       "runner-digest",
		JudgeRuleDigest:    "judge-digest",
		Hosts:              []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests: map[string]string{
			HostClaude: "tool-digest-claude", HostCodex: "tool-digest-codex", HostOpenCode: "tool-digest-opencode",
		},
		TimeoutSeconds:                         240,
		Concurrency:                            3,
		CaseOrderSeeds:                         map[int]string{1: "seed-1", 2: "seed-2", 3: "seed-3"},
		CoreBoundaryKind:                       BoundaryContainer,
		NormalizedCoreWorkerIdentitySetDigest:  "norm-worker-identity",
		NormalizedCoreBoundaryTemplateDigest:   "norm-boundary-template",
		NormalizedCoreExecutionTemplateDigest:  "norm-execution-template",
		ReceiptDigest:                          "plan-receipt-digest",
		SealDigest:                             "plan-seal-digest",
	}
}

func TestCmdCorePlanReceiptClosedSemantics(t *testing.T) {
	if err := ValidateCoreExecutionPlan(csCorePlan()); err != nil {
		t.Fatalf("sealed core plan must validate: %v", err)
	}
	if err := ValidateCoreExecutionPlan(nil); err == nil {
		t.Fatal("nil core plan must be rejected")
	}
	mutations := []struct {
		name    string
		mutate  func(p *CoreExecutionPlanReceipt)
		wantErr string
	}{
		{"schema drift", func(p *CoreExecutionPlanReceipt) { p.SchemaVersion = 2 }, "schema_version"},
		{"incomplete identity", func(p *CoreExecutionPlanReceipt) { p.PlanID = "" }, "identity"},
		{"two hosts", func(p *CoreExecutionPlanReceipt) { p.Hosts = p.Hosts[:2] }, "exactly 3"},
		{"missing tool identity", func(p *CoreExecutionPlanReceipt) { delete(p.ToolIdentityDigests, HostCodex) }, "tool_identity_digest"},
		{"non-positive concurrency", func(p *CoreExecutionPlanReceipt) { p.Concurrency = 0 }, "positive"},
		{"two seeds", func(p *CoreExecutionPlanReceipt) { p.CaseOrderSeeds = map[int]string{1: "s", 2: "s"} }, "exactly 3"},
		{"missing ordinal seed", func(p *CoreExecutionPlanReceipt) { p.CaseOrderSeeds[3] = "" }, "ordinal 3"},
		{"unknown boundary", func(p *CoreExecutionPlanReceipt) { p.CoreBoundaryKind = "chmod-777" }, "boundary"},
		{"unnormalized identity", func(p *CoreExecutionPlanReceipt) { p.NormalizedCoreWorkerIdentitySetDigest = "" }, "normalized"},
		{"unsealed", func(p *CoreExecutionPlanReceipt) { p.SealDigest = "" }, "sealed"},
	}
	for _, m := range mutations {
		p := csCorePlan()
		m.mutate(p)
		if err := ValidateCoreExecutionPlan(p); err == nil || !strings.Contains(err.Error(), m.wantErr) {
			t.Errorf("%s: want rejection containing %q, got %v", m.name, m.wantErr, err)
		}
	}
}

func csOfficialDualManifest() *FormalSeriesManifest {
	return &FormalSeriesManifest{
		SeriesID: "fx-official", Purpose: PurposeOfficialDual, State: StateSealed,
		Hosts:                   []string{HostClaude, HostCodex, HostOpenCode},
		RequiredOrdinals:        []int{1, 2, 3},
		CoreExecutionPlanDigest: "plan-receipt-digest",
		DatasetManifests: map[string]string{
			MembershipCore172: "core-manifest-digest", MembershipHoldout96: "holdout-manifest-digest",
		},
		QuestionCount:                       map[string]int{MembershipCore172: 172, MembershipHoldout96: 96},
		SkillPackageValidationReceiptDigest: "pv-receipt-digest",
		GreenTestReceiptDigest:              "series-prepare-green-digest",
		SeriesPrepareIdentityDigest:         "stable-identity-digest",
		CandidateBindingDigest:              "candidate-binding-digest",
		ProtectedExecutionReceiptDigest:     "protected-receipt-digest",
		ProtectedExecutionPolicyDigest:      "protected-policy-digest",
		ManifestDigest:                      "series-manifest-digest",
	}
}

func csDevComparisonManifest() *FormalSeriesManifest {
	m := csOfficialDualManifest()
	m.SeriesID = "fx-dev-comparison"
	m.Purpose = PurposeDevComparison
	m.DatasetManifests = map[string]string{MembershipCore172: "core-manifest-digest"}
	m.QuestionCount = map[string]int{MembershipCore172: 172}
	m.CandidateBindingDigest = ""
	m.ProtectedExecutionReceiptDigest = ""
	m.ProtectedExecutionPolicyDigest = ""
	return m
}

func TestPurposeOfficialDualVsDevComparison(t *testing.T) {
	if err := ValidateFormalSeriesManifest(csOfficialDualManifest()); err != nil {
		t.Fatalf("official-dual manifest must validate: %v", err)
	}
	if err := ValidateFormalSeriesManifest(csDevComparisonManifest()); err != nil {
		t.Fatalf("dev-comparison manifest must validate: %v", err)
	}
	type mutation struct {
		name     string
		official bool
		mutate   func(m *FormalSeriesManifest)
		wantErr  string
	}
	mutations := []mutation{
		{"official-dual without holdout96", true, func(m *FormalSeriesManifest) {
			delete(m.DatasetManifests, MembershipHoldout96)
			delete(m.QuestionCount, MembershipHoldout96)
		}, "must bind exactly core172 and holdout96"},
		{"official-dual without core172", true, func(m *FormalSeriesManifest) {
			delete(m.DatasetManifests, MembershipCore172)
			delete(m.QuestionCount, MembershipCore172)
		}, "must bind exactly core172 and holdout96"},
		{"official-dual without candidate binding", true, func(m *FormalSeriesManifest) { m.CandidateBindingDigest = "" }, "candidate_binding_digest required"},
		{"official-dual without protected receipt", true, func(m *FormalSeriesManifest) { m.ProtectedExecutionReceiptDigest = "" }, "protected_execution_receipt_digest required"},
		{"official-dual without protected policy", true, func(m *FormalSeriesManifest) { m.ProtectedExecutionPolicyDigest = "" }, "protected_execution_policy_digest required"},
		{"dev-comparison binding holdout96", false, func(m *FormalSeriesManifest) {
			m.DatasetManifests[MembershipHoldout96] = "holdout-manifest-digest"
			m.QuestionCount[MembershipHoldout96] = 96
		}, "dev-comparison must bind exactly core172"},
		{"dev-comparison with candidate binding", false, func(m *FormalSeriesManifest) { m.CandidateBindingDigest = "cb" }, "must be null"},
		{"dev-comparison with protected receipt", false, func(m *FormalSeriesManifest) { m.ProtectedExecutionReceiptDigest = "pe" }, "must be null"},
		{"dev-comparison with protected policy", false, func(m *FormalSeriesManifest) { m.ProtectedExecutionPolicyDigest = "pp" }, "must be null"},
		{"unknown purpose", true, func(m *FormalSeriesManifest) { m.Purpose = "quick-look" }, "purpose"},
		{"two ordinals", true, func(m *FormalSeriesManifest) { m.RequiredOrdinals = []int{1, 2} }, "[1,2,3]"},
		{"two hosts", true, func(m *FormalSeriesManifest) { m.Hosts = m.Hosts[:2] }, "exactly 3"},
		{"no core plan", true, func(m *FormalSeriesManifest) { m.CoreExecutionPlanDigest = "" }, "sealed core execution plan"},
		{"no package-validation receipt", true, func(m *FormalSeriesManifest) { m.SkillPackageValidationReceiptDigest = "" }, "package-validation receipt"},
		{"no series-prepare green receipt", true, func(m *FormalSeriesManifest) { m.GreenTestReceiptDigest = "" }, "series-prepare green-test receipt"},
		{"unsealed manifest", true, func(m *FormalSeriesManifest) { m.ManifestDigest = "" }, "not sealed"},
		{"core question_count drift", true, func(m *FormalSeriesManifest) { m.QuestionCount[MembershipCore172] = 171 }, "question_count[core172]"},
		{"holdout question_count drift", true, func(m *FormalSeriesManifest) { m.QuestionCount[MembershipHoldout96] = 95 }, "question_count[holdout96]"},
		{"question_count missing a split", true, func(m *FormalSeriesManifest) { delete(m.QuestionCount, MembershipHoldout96) }, "covers 1 splits, want 2"},
	}
	for _, mu := range mutations {
		var m *FormalSeriesManifest
		if mu.official {
			m = csOfficialDualManifest()
		} else {
			m = csDevComparisonManifest()
		}
		mu.mutate(m)
		if err := ValidateFormalSeriesManifest(m); err == nil || !strings.Contains(err.Error(), mu.wantErr) {
			t.Errorf("%s: want rejection containing %q, got %v", mu.name, mu.wantErr, err)
		}
	}
	if err := ValidateFormalSeriesManifest(nil); err == nil {
		t.Fatal("nil series manifest must be rejected")
	}
	// Question counts are closed: 172 core, 96 holdout, nothing else is a
	// formal-series split.
	if n, err := ExpectedQuestionCount(MembershipCore172); err != nil || n != 172 {
		t.Errorf("core172 question count %d err %v", n, err)
	}
	if n, err := ExpectedQuestionCount(MembershipHoldout96); err != nil || n != 96 {
		t.Errorf("holdout96 question count %d err %v", n, err)
	}
	if _, err := ExpectedQuestionCount("dev-extension"); err == nil {
		t.Error("a non-formal split must not be accepted as a series dataset")
	}
}

// csWorkerProbe returns one slot's complete closed probe matrix.
func csWorkerProbe(host string, slot int) ProtectedWorkerProbe {
	denied := func(kind FormalProbeKind) FormalAccessProbe {
		return FormalAccessProbe{Kind: kind, TargetDigest: "target-" + string(kind),
			TargetAccessPolicyDigest: "policy-digest", Expected: "denied", Outcome: "permission-denied"}
	}
	return ProtectedWorkerProbe{
		Host: host, WorkerSlot: slot,
		ChildIdentityDigest: "child-identity-" + host, AccessBoundaryDigest: "access-boundary-" + host,
		ExecutionTemplateDigest: "execution-template-" + host,
		Probes: []FormalAccessProbe{
			denied(FProbeProtectedRootTraverse), denied(FProbeProtectedRootList), denied(FProbeProtectedRootRead),
			denied(FProbeAuditRead), denied(FProbeAuthorStateRead),
			{Kind: FProbeOwnWorkspaceRead, TargetDigest: "own-workspace", TargetAccessPolicyDigest: "policy-digest",
				Expected: "readable", Outcome: "readable"},
		},
	}
}

// csProtectedReceipt returns a protected execution receipt prepared against
// the exact plan.
func csProtectedReceipt(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	var probes []ProtectedWorkerProbe
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			probes = append(probes, csWorkerProbe(h, slot))
		}
	}
	return &ProtectedExecutionReceipt{
		BoundaryKind:           BoundaryContainer,
		IsolationConfigDigest:  "isolation-config",
		ProtectedRootDigest:    "protected-root",
		AuthorReviewStateRootsDigest: "author-review-roots",
		FormalStateRootsDigest:       "formal-roots",
		SplitStateAllocatorDigests: map[string]string{
			MembershipCore172: "core-allocator", MembershipHoldout96: "holdout-allocator",
		},
		RequiredConcurrency:                   plan.Concurrency,
		IsolatedWorkerCapacity:                plan.Concurrency,
		WorkerIdentitySetDigest:               "worker-identity-set",
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            "execution-template-set",
		CoreExecutionPlanDigest:               plan.ReceiptDigest,
		WorkerProbes:                          probes,
		ProbeMatrixDigest:                     "probe-matrix",
		ProbedAt:                              "2026-09-01T00:00:00Z",
		ReceiptDigest:                         "protected-receipt-digest",
	}
}

func TestSeriesSharedCorePlanImport(t *testing.T) {
	plan := csCorePlan()
	if err := ValidateProtectedExecutionReceipt(csProtectedReceipt(plan), plan); err != nil {
		t.Fatalf("a receipt prepared against the plan must validate: %v", err)
	}
	// A comparable series imports the exact plan — never a reconstruction.
	other := csCorePlan()
	other.ReceiptDigest = "another-plan-receipt-digest"
	if err := ValidateProtectedExecutionReceipt(csProtectedReceipt(plan), other); err == nil || !strings.Contains(err.Error(), "plan digest") {
		t.Errorf("a receipt prepared against another plan must be rejected: %v", err)
	}
	drifted := csProtectedReceipt(plan)
	drifted.NormalizedCoreWorkerIdentitySetDigest = "drifted-worker-identity"
	if err := ValidateProtectedExecutionReceipt(drifted, plan); err == nil || !strings.Contains(err.Error(), "drifts from the core plan") {
		t.Errorf("worker identity template drift must be rejected: %v", err)
	}
	capacity := csProtectedReceipt(plan)
	capacity.IsolatedWorkerCapacity = plan.Concurrency - 1
	if err := ValidateProtectedExecutionReceipt(capacity, plan); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Errorf("insufficient isolated capacity must be rejected: %v", err)
	}
	overlap := csProtectedReceipt(plan)
	overlap.SplitStateAllocatorDigests[MembershipHoldout96] = overlap.SplitStateAllocatorDigests[MembershipCore172]
	if err := ValidateProtectedExecutionReceipt(overlap, plan); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("overlapping split allocators must be rejected: %v", err)
	}
	missingSlot := csProtectedReceipt(plan)
	missingSlot.WorkerProbes = missingSlot.WorkerProbes[1:]
	if err := ValidateProtectedExecutionReceipt(missingSlot, plan); err == nil || !strings.Contains(err.Error(), "exactly 1") {
		t.Errorf("an incomplete host × slot probe matrix must be rejected: %v", err)
	}
	extraSlot := csProtectedReceipt(plan)
	extraSlot.WorkerProbes = append(extraSlot.WorkerProbes, csWorkerProbe(HostClaude, plan.Concurrency+7))
	if err := ValidateProtectedExecutionReceipt(extraSlot, plan); err == nil || !strings.Contains(err.Error(), "unexpected worker probe") {
		t.Errorf("an out-of-matrix worker slot must be rejected: %v", err)
	}
	// The manifest side: a series must import a sealed core plan digest.
	m := csDevComparisonManifest()
	m.CoreExecutionPlanDigest = ""
	if err := ValidateFormalSeriesManifest(m); err == nil || !strings.Contains(err.Error(), "sealed core execution plan") {
		t.Errorf("a series without a core plan must be rejected: %v", err)
	}
}

func TestSeriesPackageValidationReceiptRejection(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	repo := t.TempDir()
	validator := filepath.Join(repo, "scripts", "validate-agent-skill.mjs")
	writeFile(t, filepath.Join(repo, "scripts"), "validate-agent-skill.mjs", []byte("// fictional validator"))
	skillA := filepath.Join(repo, "skills", "a")
	fakeSkillPackage(t, skillA)
	skillB := filepath.Join(repo, "skills", "b")
	fakeSkillPackage(t, skillB)
	writeFile(t, skillB, "SKILL.md", []byte("---\nname: engram\nversion: 9.9.9-other\n---\n\nA different evaluated body.\n"))
	greenPath := filepath.Join(repo, "receipts", "ft-green.json")
	if _, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, greenPath, GreenBindings{}); err != nil {
		t.Fatal(err)
	}
	snapA := filepath.Join(repo, "snap-a")
	recA, err := runPackageValidateWith(skillA, repo, snapA, greenPath, validator, filepath.Join(repo, "receipts", "pv-a.json"), okValidator)
	if err != nil {
		t.Fatalf("package validate A failed: %v", err)
	}
	snapB := filepath.Join(repo, "snap-b")
	recB, err := runPackageValidateWith(skillB, repo, snapB, greenPath, validator, filepath.Join(repo, "receipts", "pv-b.json"), okValidator)
	if err != nil {
		t.Fatalf("package validate B failed: %v", err)
	}
	if recA.SkillDigest == recB.SkillDigest {
		t.Fatal("the two fictional skills must have different digests")
	}
	// Missing.
	if err := VerifyPackageValidationReceipt(nil, snapA, validator); err == nil {
		t.Error("a missing package-validation receipt must be rejected")
	}
	// Failed.
	failed := *recA
	failed.Passed = false
	if err := VerifyPackageValidationReceipt(&failed, snapA, validator); err == nil {
		t.Error("a failed package-validation receipt must be rejected")
	}
	// Wrong skill: the receipt names another package than the materialized one.
	if err := VerifyPackageValidationReceipt(recB, snapA, validator); err == nil {
		t.Error("a wrong-skill receipt must be rejected")
	}
	// Digest drift inside the receipt.
	drifted := *recA
	drifted.SkillDigest = strings.Repeat("0", 64)
	if err := VerifyPackageValidationReceipt(&drifted, snapA, validator); err == nil {
		t.Error("a digest-drifted receipt must be rejected")
	}
	// Byte drift inside the materialized snapshot: no mutable substitute.
	b, err := os.ReadFile(filepath.Join(snapA, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, snapA, "SKILL.md", append(b, []byte("\n<!-- drifted -->\n")...))
	if err := VerifyPackageValidationReceipt(recA, snapA, validator); err == nil {
		t.Error("snapshot byte drift must be rejected")
	}
	// The series manifest must bind a passing exact-skill receipt digest.
	m := csDevComparisonManifest()
	m.SkillPackageValidationReceiptDigest = ""
	if err := ValidateFormalSeriesManifest(m); err == nil || !strings.Contains(err.Error(), "package-validation receipt") {
		t.Errorf("a series without a package-validation receipt must be rejected: %v", err)
	}
}

func TestGreenTestSeriesPrepareAndPreHoldoutArgv(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	repo := t.TempDir()
	validator := filepath.Join(repo, "scripts", "validate-agent-skill.mjs")
	writeFile(t, filepath.Join(repo, "scripts"), "validate-agent-skill.mjs", []byte("// fictional validator"))
	pkg := filepath.Join(repo, "skills", "engram")
	fakeSkillPackage(t, pkg)
	// A complete, self-consistent snapshot + package-receipt chain: the
	// series-grade green create rehash-verifies the snapshot against the
	// exact-skill receipt before binding it, so the fixture must satisfy
	// VerifyPackageValidationReceipt end to end.
	recs, err := inventoryPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := FileRecordsDigest(recs)
	if err != nil {
		t.Fatal(err)
	}
	skillDigest, err := EngramPackageDigest(recs, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(pkg, filepath.FromSlash(rel)))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, vDigest, err := currentImplementDigests(validator)
	if err != nil {
		t.Fatal(err)
	}
	snap := &FrozenSkillPackageSnapshot{
		SchemaVersion: 1, SnapshotID: "snap-fx", SkillDigest: skillDigest,
		FileRecords: recs, ValidatorRevision: "020-fx", ValidatorDigest: vDigest,
	}
	snap.SnapshotDigest = mustDigest(snap)
	writeFile(t, pkg, "snapshot.json", mustCanonicalJSON(snap))
	pv := &SkillPackageValidationReceipt{
		SnapshotID: "snap-fx", Passed: true,
		SnapshotDigest: snap.SnapshotDigest, SkillDigest: skillDigest,
		FileRecords: recs, FileRecordsDigest: fd,
		ValidatorRevision: "020-fx", ValidatorDigest: vDigest,
	}
	pvDigest, err := CanonicalSHA256(pv)
	if err != nil {
		t.Fatal(err)
	}
	pv.ReceiptDigest = pvDigest
	receipts := filepath.Join(repo, "receipts")
	pvPath := filepath.Join(receipts, "pv.json")
	writeFile(t, receipts, "pv.json", mustCanonicalJSON(pv))
	spOut := filepath.Join(receipts, "series-prepare-green.json")
	spArgv := func(extra ...string) []string {
		argv := []string{"--suite", SuiteSeriesPrepare, "--skill-snapshot", pkg,
			"--skill-package-validation", pvPath, "--validator", validator, "--out", spOut}
		return append(argv, extra...)
	}
	// series-prepare without the package-validation binding is refused.
	if err := cmdGreenTestCreate([]string{"--suite", SuiteSeriesPrepare, "--skill-snapshot", pkg,
		"--validator", validator, "--out", spOut}); err == nil {
		t.Error("series-prepare without --skill-package-validation must be refused")
	}
	// A missing package-validation receipt file is refused.
	if err := cmdGreenTestCreate(spArgv("--skill-package-validation", filepath.Join(receipts, "ghost.json"))); err == nil {
		t.Error("a missing package-validation receipt must be refused")
	}
	if err := cmdGreenTestCreate(spArgv()); err != nil {
		t.Fatalf("series-prepare green-test create failed: %v", err)
	}
	// The bound snapshot digest is the receipt's own snapshot identity.
	snapDigest := snap.SnapshotDigest
	green, err := LoadGreenTestReceipt(spOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyGreenTestReceipt(green, SuiteSeriesPrepare, validator, GreenBindings{
		SnapshotDigest: &snapDigest, PackageValidationReceiptDigest: &pvDigest,
	}); err != nil {
		t.Fatalf("the produced series-prepare receipt must verify: %v", err)
	}
	other := "another-skill-snapshot-digest"
	if err := VerifyGreenTestReceipt(green, SuiteSeriesPrepare, validator, GreenBindings{
		SnapshotDigest: &other, PackageValidationReceiptDigest: &pvDigest,
	}); err == nil {
		t.Error("a series-prepare receipt must be bound to the exact snapshot")
	}

	// pre-holdout argv: the three holdout bindings are mandatory.
	phOut := filepath.Join(receipts, "pre-holdout-green.json")
	phBase := []string{"--suite", SuitePreHoldout, "--skill-snapshot", pkg,
		"--skill-package-validation", pvPath, "--validator", validator, "--out", phOut}
	if err := cmdGreenTestCreate(phBase); err == nil || !strings.Contains(err.Error(), "pre-holdout requires") {
		t.Errorf("pre-holdout without its series bindings must be refused: %v", err)
	}
	unprepared := filepath.Join(repo, "unprepared-series")
	if err := os.MkdirAll(unprepared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cmdGreenTestCreate(append(append([]string{}, phBase...), "--series-root", unprepared,
		"--candidate-binding", "cb-fx", "--core-leg-completion", "cl-fx")); err == nil || !strings.Contains(err.Error(), "series-manifest.json") {
		t.Errorf("pre-holdout against an unprepared series root must be refused: %v", err)
	}
	seriesRoot := filepath.Join(repo, "series")
	writeFile(t, seriesRoot, "series-manifest.json", []byte(`{"series_id":"fx-series"}`))
	if err := cmdGreenTestCreate(append(append([]string{}, phBase...), "--series-root", seriesRoot,
		"--candidate-binding", "cb-fx", "--core-leg-completion", "cl-fx")); err != nil {
		t.Fatalf("pre-holdout green-test create failed: %v", err)
	}
	pre, err := LoadGreenTestReceipt(phOut)
	if err != nil {
		t.Fatal(err)
	}
	smDigest, err := seriesManifestDigestAt(seriesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyGreenTestReceipt(pre, SuitePreHoldout, validator, GreenBindings{
		SnapshotDigest: &snapDigest, PackageValidationReceiptDigest: &pvDigest,
		SeriesManifestDigest: &smDigest, CandidateBindingDigest: strPtr("cb-fx"), CoreLegCompletionDigest: strPtr("cl-fx"),
	}); err != nil {
		t.Fatalf("the produced pre-holdout receipt must verify against its series: %v", err)
	}
	// It is one-series-only: it cannot authorize another series' holdout
	// ordinal 1, nor another candidate binding or core-leg completion.
	otherManifest := "another-series-manifest-digest"
	if err := VerifyGreenTestReceipt(pre, SuitePreHoldout, validator, GreenBindings{
		SeriesManifestDigest: &otherManifest, CandidateBindingDigest: strPtr("cb-fx"), CoreLegCompletionDigest: strPtr("cl-fx"),
	}); err == nil {
		t.Error("a pre-holdout receipt must not authorize another series")
	}
	for name, bindings := range map[string]GreenBindings{
		"candidate binding":    {SeriesManifestDigest: &smDigest, CandidateBindingDigest: strPtr("other-cb"), CoreLegCompletionDigest: strPtr("cl-fx")},
		"core-leg completion": {SeriesManifestDigest: &smDigest, CandidateBindingDigest: strPtr("cb-fx"), CoreLegCompletionDigest: strPtr("other-cl")},
	} {
		if err := VerifyGreenTestReceipt(pre, SuitePreHoldout, validator, bindings); err == nil {
			t.Errorf("a pre-holdout receipt must bind the exact %s", name)
		}
	}
}

func TestGreenTestPreconditionRejectionSurface(t *testing.T) {
	validator := filepath.Join(t.TempDir(), "validate-agent-skill.mjs")
	writeFile(t, filepath.Dir(validator), filepath.Base(validator), []byte("// fictional validator"))
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	r, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, "", GreenBindings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyGreenTestReceipt(r, SuiteSeriesPrepare, validator, GreenBindings{}); err == nil {
		t.Error("a wrong-suite receipt must not satisfy a series-prepare precondition")
	}
	// Argv evidence inside a "passed" receipt: a non-zero exit is rejected on
	// its own, before the digest rule can mask it. (Deep-copy Commands: a
	// struct copy shares the backing array.)
	badExit := *r
	badExit.Commands = append([]GreenCommand(nil), r.Commands...)
	badExit.Commands[0].ExitCode = 1
	d, err := receiptDigest(&badExit)
	if err != nil {
		t.Fatal(err)
	}
	badExit.ReceiptDigest = d
	if err := VerifyGreenTestReceipt(&badExit, SuiteFormalTooling, validator, GreenBindings{}); err == nil || !strings.Contains(err.Error(), "exit") {
		t.Errorf("a passed receipt with a failing command must be rejected on argv evidence: %v", err)
	}
	// No command evidence at all.
	empty := *r
	empty.Commands = nil
	if d, err = receiptDigest(&empty); err != nil {
		t.Fatal(err)
	}
	empty.ReceiptDigest = d
	if err := VerifyGreenTestReceipt(&empty, SuiteFormalTooling, validator, GreenBindings{}); err == nil || !strings.Contains(err.Error(), "no command evidence") {
		t.Errorf("a receipt without command evidence must be rejected: %v", err)
	}
	// A receipt stamped in the future is post-hoc, never a precondition.
	future := *r
	future.Commands = append([]GreenCommand(nil), r.Commands...)
	future.CreatedAt = "2031-01-01T00:00:00Z"
	if d, err = receiptDigest(&future); err != nil {
		t.Fatal(err)
	}
	future.ReceiptDigest = d
	if err := VerifyGreenTestReceipt(&future, SuiteFormalTooling, validator, GreenBindings{}); err == nil || !strings.Contains(err.Error(), "pre-action") {
		t.Errorf("a post-hoc receipt must be rejected: %v", err)
	}
	if err := VerifyGreenTestReceipt(nil, SuiteFormalTooling, validator, GreenBindings{}); err == nil {
		t.Error("a missing receipt must be rejected")
	}
}

func TestSelectorRejectedInPrimarySurface(t *testing.T) {
	// --only / --sample / --limit are diagnostic affordances: the primary run
	// manifest has no field for them, so a primary run cannot carry them.
	primary := reflect.TypeOf(PrimaryRunManifest{})
	for _, f := range []string{"Only", "Sample", "Limit", "IncludeExtension"} {
		if _, ok := primary.FieldByName(f); ok {
			t.Errorf("PrimaryRunManifest must not carry the diagnostic selector %q", f)
		}
	}
	diagnostic := reflect.TypeOf(DiagnosticOptions{})
	for _, f := range []string{"Only", "Sample", "Limit"} {
		if _, ok := diagnostic.FieldByName(f); !ok {
			t.Errorf("DiagnosticOptions must carry the diagnostic selector %q", f)
		}
	}
	// The primary gate rejects before anything executes, selector flags or not.
	for _, argv := range [][]string{
		{"--mode", "primary"},
		{"--mode", "primary", "--only", "iwp-01"},
		{"--mode", "primary", "--sample", "3"},
		{"--mode", "primary", "--limit", "1"},
	} {
		if err := cmdRunV2(argv); err == nil || !strings.Contains(err.Error(), "primary") {
			t.Errorf("run %v must be rejected at the primary gate: %v", argv, err)
		}
	}
}

func TestPrimaryModeNeverDowngrades(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	scratch := filepath.Join(root, "scratch")
	err := cmdRunV2([]string{"--mode", "primary", "--split", SplitHoldout,
		"--out", out, "--scratch", scratch, "--bin-dir", "bin"})
	if err == nil || !strings.Contains(err.Error(), "primary") {
		t.Fatalf("primary without a prepared series must fail closed: %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a rejected primary run must not materialize an output root")
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Error("a rejected primary run must not materialize a scratch root")
	}
	// --mode routes to the v2 command, never the legacy harness.
	if err := routeOrLegacy([]string{"run", "--mode", "primary"}); err == nil {
		t.Error("a routed primary invocation must still fail closed")
	}
	// Structural separation: a diagnostic artifact cannot be relabeled as a
	// primary run.
	dt := reflect.TypeOf(DiagnosticRunReceipt{})
	for _, f := range []string{"SeriesID", "Ordinal", "SealDigest"} {
		if _, ok := dt.FieldByName(f); ok {
			t.Errorf("a diagnostic receipt must not carry the primary identity field %q", f)
		}
	}
	pt := reflect.TypeOf(PrimaryRunManifest{})
	for _, f := range []string{"Mode", "SeriesID", "Split", "Ordinal", "SealDigest"} {
		if _, ok := pt.FieldByName(f); !ok {
			t.Errorf("a primary run manifest must carry %q", f)
		}
	}
}

func TestPrimaryExplicitConcurrencyRules(t *testing.T) {
	diagArgv := func(extra ...string) []string {
		argv := []string{"--mode", "diagnostic", "--split", SplitDevRegression,
			"--dataset", "dataset", "--out", filepath.Join(t.TempDir(), "out"),
			"--scratch", filepath.Join(t.TempDir(), "scratch"), "--bin-dir", "bin"}
		return append(argv, extra...)
	}
	// A diagnostic run without an explicit --concurrency is a usage error
	// before any case starts.
	if err := cmdRunV2(diagArgv()); err == nil || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("diagnostic without explicit --concurrency must be rejected: %v", err)
	}
	// Selector affordances do not relax that requirement.
	if err := cmdRunV2(diagArgv("--limit", "1")); err == nil || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("selector flags must not relax the explicit-concurrency rule: %v", err)
	}
	// Holdout diagnostics are invalid outright, at the CLI layer too. The
	// later --split overrides the fixture's dev-regression default.
	if err := cmdRunV2(diagArgv("--split", SplitHoldout, "--concurrency", "2")); err == nil || !strings.Contains(err.Error(), "dev-only") {
		t.Fatalf("a holdout diagnostic must be rejected: %v", err)
	}
	// family-index build freezes an explicit concurrency as well.
	if err := cmdFamilyIndexBuild([]string{"--dataset", "d", "--core-manifest", "m",
		"--review-prompt", "p", "--out", "o"}); err == nil || !strings.Contains(err.Error(), "--concurrency") {
		t.Fatalf("family-index build without explicit --concurrency must be rejected: %v", err)
	}
}

func TestSeriesTraceUnobservableVerdicts(t *testing.T) {
	// SC: a tool that cannot expose the traces formal judging needs makes its
	// formal series INVALID — never silently excluded, never a pass. At the
	// verdict layer that means: no engram-call trace ⇒ a classified non-pass,
	// whatever the answer text says.
	read := &TriggerCaseV2{ID: "fx-read", Module: "implicit-read-pos",
		Expect: ExpectV2{Trigger: true, AnswerInclude: []Alternation{{"记得"}}, Observable: "o"}}
	answerOnly := []Event{{Kind: EventText, Text: "我记得 pnpm 是一个包管理器"}}
	if v := JudgeV2(read, answerOnly, "", nil); v.Pass || v.Failure != "false-negative" {
		t.Fatalf("an answer without a trace must be a false-negative, got %+v", v)
	}
	if v := JudgeV2(read, nil, "", nil); v.Pass || v.Failure != "false-negative" {
		t.Fatalf("an empty trace must be a false-negative, got %+v", v)
	}
	// Store-side evidence is equally mandatory: a write trace with no store
	// dump cannot pass.
	write := &TriggerCaseV2{ID: "fx-write", Module: "implicit-write-pos",
		Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "o"}}
	if v := JudgeV2(write, []Event{{Kind: EventEngramCall, Op: "write"}}, "", nil); v.Pass || v.Failure == "" {
		t.Fatalf("missing store evidence must be a classified non-pass, got %+v", v)
	}
	// An explicit engram request with no trace at all.
	reg := &TriggerCaseV2{ID: "fx-reg", Module: "regression",
		Expect: ExpectV2{Trigger: true, Observable: "o"}}
	if v := JudgeV2(reg, []Event{{Kind: EventText, Text: "好的"}}, "", nil); v.Pass || v.Failure != "false-negative" {
		t.Fatalf("an explicit request without a trace must be a false-negative, got %+v", v)
	}
	// A tool that cannot even run is terminal, never a pass.
	if v := JudgeV2(read, nil, "", fmt.Errorf("fictional cli missing")); v.Pass || v.Failure != "runner-error" {
		t.Fatalf("a terminal child failure must be runner-error, got %+v", v)
	}
	// Determinism: identical evidence yields an identical verdict, so an
	// unobservable trace cannot be quietly re-counted as an observable one.
	if !reflect.DeepEqual(JudgeV2(read, answerOnly, "", nil), JudgeV2(read, answerOnly, "", nil)) {
		t.Fatal("the judge must be deterministic over identical evidence")
	}
	// The closed lifecycle set carries an explicit invalid state, so an
	// unobservable or incomplete series has somewhere to go other than
	// silence.
	states := map[LifecycleState]bool{}
	for _, s := range []LifecycleState{StateDraft, StateSealed, StateComplete, StateInvalid} {
		if s == "" {
			t.Fatal("empty lifecycle state in the closed set")
		}
		if states[s] {
			t.Fatalf("duplicate lifecycle state %q", s)
		}
		states[s] = true
	}
	if !states[StateInvalid] {
		t.Fatal("the lifecycle set must carry an explicit invalid state")
	}
}

func TestCmdUnwiredFormalCommandsFailClosed(t *testing.T) {
	// `core-plan create`, `series prepare`, `score` and the primary run are
	// US4 execution entries wired by T051; `failure-archive` and `compare`
	// joined them in T053 (US5 dev flywheel). The router must claim all of
	// them: a claim is what makes a wrong invocation an argument error
	// instead of a silent legacy fallback.
	for _, cmd := range []string{"core-plan", "series", "score", "failure-archive", "compare"} {
		if handled, _ := routeV2([]string{cmd}); !handled {
			t.Errorf("wired command %q must be claimed by the v2 router", cmd)
		}
	}
	// A primary run still refuses to start without a prepared series: the
	// fail-closed path must reject an unusable root, never improvise one.
	if err := cmdRunPrimary([]string{"--series", "s", "--split", "dev-regression",
		"--run-ordinal", "1", "--tool", "claude", "--series-root", t.TempDir(),
		"--core-execution-plan", filepath.Join(t.TempDir(), "absent-plan.json"),
		"--scratch", t.TempDir(), "--bin-dir", t.TempDir()}); err == nil {
		t.Error("a primary run without a prepared series root must fail closed")
	}
	if ErrNotWired == nil || !strings.Contains(ErrNotWired.Error(), "not wired") {
		t.Errorf("ErrNotWired must be the declared fail-closed sentinel: %v", ErrNotWired)
	}
	// The usage text must document both run modes and every wired formal
	// command.
	if !strings.Contains(usageV2, "--mode diagnostic") {
		t.Error("usage must document the diagnostic surface")
	}
	if !strings.Contains(usageV2, "--mode primary") {
		t.Error("usage must document the primary surface")
	}
	if !strings.Contains(usageV2, "core-plan create") || !strings.Contains(usageV2, "series prepare") || !strings.Contains(usageV2, "score --series-root") {
		t.Error("usage must document the wired formal commands")
	}
}
