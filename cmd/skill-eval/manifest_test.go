package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeTempManifestMaterializes a two-case fixture core dataset in a temp dir
// with real digests, returning (dir, manifest).
func writeTempManifest(t *testing.T) (string, *DatasetManifestV2) {
	t.Helper()
	dir := t.TempDir()
	caseA := readFixture(t, "core-case-v2.json")
	caseB := readFixture(t, "core-regression-v2.json")
	writeFile(t, dir, "cases-a.json", caseA)
	writeFile(t, dir, "cases-b.json", caseB)

	digA := LFNormalizedSHA256Bytes(caseA)
	digB := LFNormalizedSHA256Bytes(caseB)
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "fx-test-v1",
		Split: SplitDevRegression, ScoreMembership: MembershipCore172,
		CaseCount:      2,
		ModuleCounts:   map[string]int{"implicit-write-pos": 1, "regression": 1},
		LanguageCounts: map[string]int{LangZh: 1, LangUnclassified: 1},
		CaseIDs:        []string{"fx-iwp-001", "fx-reg-001"},
		PayloadFiles: []PayloadFileV1{
			{RelativePath: "cases-a.json", LFNormalizedSHA256: digA, CaseIDs: []string{"fx-iwp-001"}},
			{RelativePath: "cases-b.json", LFNormalizedSHA256: digB, CaseIDs: []string{"fx-reg-001"}},
		},
	}
	return dir, m
}

func TestDatasetPayloadDigestCaseOnly(t *testing.T) {
	dir, m := writeTempManifest(t)
	d1, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	// A manifest edit must not change the case-only payload digest.
	m.DatasetVersion = "fx-test-v2"
	d2, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("payload digest must cover case files only, not the manifest")
	}
	// Adding an unrelated file to the directory must not change it either.
	writeFile(t, dir, "evals.json", []byte(`[{"query":"legacy"}]`))
	d3, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d3 {
		t.Fatal("directory-discovered files must never enter the payload digest")
	}
	// Case content change changes the digest.
	writeFile(t, dir, "cases-a.json", append(readFixture(t, "core-case-v2.json"), []byte("\n")...))
	if d4, _ := DatasetPayloadDigest(dir, m); d4 == d1 {
		t.Fatal("payload digest must change when case bytes change")
	}
}

func TestFreezeBeforeDigestManifestAfterPayload(t *testing.T) {
	dir, m := writeTempManifest(t)
	// Freeze-before-digest: digest computed only after the payload digest lands.
	if _, err := CompleteManifestForSeal(m); err == nil {
		t.Fatal("manifest without payload_digest must be refused")
	}
	pd, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	m.PayloadDigest = pd
	cid, err := CaseIDsDigest(m.CaseIDs)
	if err != nil {
		t.Fatal(err)
	}
	m.CaseIDsDigest = cid
	d1, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatalf("completed manifest refused: %v", err)
	}
	// Seal exclusion: the digest is computed over the manifest without seal.
	m.Seal = &DatasetSeal{ManifestDigest: "x"}
	d2, err := CompleteManifestForSeal(m)
	if err == nil {
		t.Fatal("sealed manifest must be refused for a new seal computation")
	}
	mCopy := *m
	mCopy.Seal = nil
	d3, _ := CanonicalSHA256(mCopy)
	if d1 != d3 {
		t.Fatal("manifest digest must exclude only the seal object")
	}
	_ = d2
}

func TestSealTamperDetection(t *testing.T) {
	dir, m := writeTempManifest(t)
	pd, _ := DatasetPayloadDigest(dir, m)
	m.PayloadDigest = pd
	m.CaseIDsDigest, _ = CaseIDsDigest(m.CaseIDs)
	manifestDigest, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := BuildDatasetAnchor(m, manifestDigest, "git-tag", "refs/tags/fx-v1")
	if err != nil {
		t.Fatal(err)
	}
	m.Seal = seal
	if err := VerifyDatasetSeal(m, dir); err != nil {
		t.Fatalf("fresh seal must verify: %v", err)
	}
	// Tamper 1: post-seal field mutation.
	m.CaseCount = 99
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("post-seal mutation must invalidate the seal")
	}
	m.CaseCount = 2
	// Tamper 2: anchor preimage forgery.
	m.Seal.AnchorPreimageDigest = strings.Repeat("0", 64)
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("forged anchor preimage must fail closed")
	}
	seal2, _ := BuildDatasetAnchor(m, manifestDigest, "git-tag", "refs/tags/fx-v1")
	m.Seal = seal2
	// Tamper 3: payload swap after seal.
	writeFile(t, dir, "cases-a.json", []byte(`{"dataset":"x","version":9,"cases":[]}`))
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("payload swap after seal must invalidate the seal")
	}
}

func TestImmutableManifestRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "frozen.json")
	if err := WriteFrozenFile(p, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrozenFile(p, []byte(`{"x":1}`)); err == nil {
		t.Fatal("frozen outputs must never be overwritten")
	}
	_ = os.Remove(p)
}

func TestEnsureInsideContainment(t *testing.T) {
	parent := t.TempDir()
	if _, err := EnsureInside(parent, filepath.Join(parent, "sub", "f.json")); err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	if _, err := EnsureInside(parent, filepath.Join(parent, "..", "escape")); err == nil {
		t.Fatal(".. escape must be rejected")
	}
	if _, err := EnsureInside(parent, "/etc/passwd"); err == nil {
		t.Fatal("absolute outside path must be rejected")
	}
	if !UnsafePath("../x") || !UnsafePath("a\\b") || !UnsafePath("/abs") {
		t.Fatal("UnsafePath must reject traversal/backslash/absolute")
	}
	if UnsafePath("a/b/c.json") {
		t.Fatal("safe relative path rejected")
	}
}

// ---------- US4 formal-series semantics (spec 048 T040) ----------
//
// Every fixture below is fictional: no dataset, skill, or host artifact is
// read. Only the closed validators in series.go plus the fixed-suite green
// receipt layer (fixed-suite runner stubbed) are exercised. The stable
// candidate binding is the recovery key, so the series context (series id,
// series manifest digest, pre-holdout receipt, core-leg digest) must never
// enter its preimage.

func fx64(seed string) string { return sha256Hex([]byte("series-fx:" + seed)) }

func fxSeriesPlan() *CoreExecutionPlanReceipt {
	return &CoreExecutionPlanReceipt{
		SchemaVersion:      1,
		PlanID:             "plan-fx-001",
		CoreManifestDigest: fx64("core-manifest"),
		RunnerRevision:     "runner-fx-1",
		RunnerDigest:       fx64("runner"),
		JudgeRuleDigest:    fx64("judge-rules"),
		Hosts:              []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests: map[string]string{
			HostClaude: fx64("ti-claude"), HostCodex: fx64("ti-codex"), HostOpenCode: fx64("ti-opencode"),
		},
		TimeoutSeconds:                        600,
		Concurrency:                           4,
		CaseOrderSeeds:                        map[int]string{1: fx64("seed-1"), 2: fx64("seed-2"), 3: fx64("seed-3")},
		CoreBoundaryKind:                      BoundarySeparateUser,
		NormalizedCoreWorkerIdentitySetDigest: fx64("worker-idset"),
		NormalizedCoreBoundaryTemplateDigest:  fx64("boundary-tpl"),
		NormalizedCoreExecutionTemplateDigest: fx64("exec-tpl"),
		CreatedAt:                             "2026-09-01T00:00:00Z",
		ReceiptDigest:                         fx64("plan-receipt"),
		SealDigest:                            fx64("plan-seal"),
	}
}

func fxCandidateBinding(plan *CoreExecutionPlanReceipt) *CandidateBindingV1 {
	return &CandidateBindingV1{
		SchemaVersion:                       1,
		Purpose:                             PurposeOfficialDual,
		SkillSnapshotDigest:                 fx64("snapshot"),
		SkillSnapshotAnchorDigest:           fx64("snapshot-anchor"),
		SkillDigest:                         fx64("skill"),
		SkillPackageValidationReceiptDigest: fx64("pv-receipt"),
		ValidatorRevision:                   "validator-fx-1",
		ValidatorDigest:                     fx64("validator"),
		RunnerRevision:                      plan.RunnerRevision,
		RunnerDigest:                        plan.RunnerDigest,
		JudgeRuleDigest:                     plan.JudgeRuleDigest,
		DatasetIdentities: map[string]string{
			MembershipCore172: fx64("core-manifest"), MembershipHoldout96: fx64("holdout-manifest"),
		},
		CoreExecutionPlanDigest: plan.ReceiptDigest,
		ToolIdentityDigests: map[string]string{
			HostClaude: fx64("ti-claude"), HostCodex: fx64("ti-codex"), HostOpenCode: fx64("ti-opencode"),
		},
		ToolConfigurationDigest:        fx64("tool-config"),
		TimeoutSeconds:                 plan.TimeoutSeconds,
		Concurrency:                    plan.Concurrency,
		CaseOrderSeeds:                 map[int]string{1: fx64("seed-1"), 2: fx64("seed-2"), 3: fx64("seed-3")},
		ExecutionEnvironmentDigest:     fx64("exec-env"),
		ProtectedExecutionPolicyDigest: fx64("policy"),
		SeriesPrepareIdentityDigest:    fx64("series-prepare-identity"),
	}
}

func fxSeriesManifest(purpose SeriesPurpose) *FormalSeriesManifest {
	m := &FormalSeriesManifest{
		SeriesID: "series-fx-1", Purpose: purpose, State: StateSealed,
		SkillSnapshotDigest: fx64("snapshot"), SkillSnapshotAnchorDigest: fx64("snapshot-anchor"),
		SkillVersion: "v0.2.8-fx", SkillDigest: fx64("skill"),
		SkillPackageValidationReceiptDigest: fx64("pv-receipt"),
		GreenTestReceiptDigest:              fx64("green-series-prepare"),
		SeriesPrepareIdentityDigest:         fx64("series-prepare-identity"),
		RunnerRevision:                      "runner-fx-1",
		RunnerDigest:                        fx64("runner"),
		JudgeRuleDigest:                     fx64("judge-rules"),
		CoreExecutionPlanDigest:             fx64("plan-receipt"),
		DatasetManifests:                    map[string]string{},
		Hosts:                               []string{HostClaude, HostCodex, HostOpenCode},
		RequiredOrdinals:                    []int{1, 2, 3},
		TimeoutSeconds:                      600,
		Concurrency:                         4,
		ExecutionEnvironmentDigest:          fx64("exec-env"),
		ToolConfigurationDigest:             fx64("tool-config"),
		ProtectedExecutionPolicyDigest:      fx64("policy"),
		CaseOrderSeeds:                      map[int]string{1: fx64("seed-1"), 2: fx64("seed-2"), 3: fx64("seed-3")},
		QuestionCount:                       map[string]int{},
		CandidateBindingDigest:              fx64("candidate-binding"),
		ProtectedExecutionReceiptDigest:     fx64("protected-receipt"),
		WorkspaceCanaryReceiptDigests:       map[string]map[int]string{},
		ManifestDigest:                      fx64("series-manifest"),
	}
	switch purpose {
	case PurposeOfficialDual:
		m.DatasetManifests[MembershipCore172] = fx64("core-manifest")
		m.DatasetManifests[MembershipHoldout96] = fx64("holdout-manifest")
		m.QuestionCount[MembershipCore172] = 172
		m.QuestionCount[MembershipHoldout96] = 96
	case PurposeDevComparison:
		m.DatasetManifests[MembershipCore172] = fx64("core-manifest")
		m.QuestionCount[MembershipCore172] = 172
		m.CandidateBindingDigest = ""
		m.ProtectedExecutionReceiptDigest = ""
		m.ProtectedExecutionPolicyDigest = ""
	}
	return m
}

func fxHoldoutAttempt(seriesID, manifestDigest, bindingDigest, tag string) HoldoutSeriesAttempt {
	return HoldoutSeriesAttempt{
		SeriesID: seriesID, SeriesManifestDigest: manifestDigest,
		CandidateBindingDigestSelf:       bindingDigest,
		PreHoldoutGreenTestReceiptDigest: fx64("pre-holdout-" + tag),
		CoreLegCompletionDigest:          fx64("core-leg-" + tag),
		StartedAt:                        "2026-09-0" + tag + "T00:00:00Z",
		State:                            "started",
	}
}

func fxHoldoutBinding(bindingDigest string, attempts ...HoldoutSeriesAttempt) *HoldoutBindingReceipt {
	return &HoldoutBindingReceipt{
		DatasetVersion: "holdout96-fx-v1", DatasetManifestDigest: fx64("holdout-manifest"),
		CandidateBindingDigest: bindingDigest, FirstPrimaryStartedAt: "2026-09-01T00:00:00Z",
		SeriesAttempts: attempts, State: "frozen", ReceiptDigest: fx64("binding-receipt"),
	}
}

func fxProbeMatrix() []FormalAccessProbe {
	denied := func(kind FormalProbeKind, outcome string, proof bool) FormalAccessProbe {
		p := FormalAccessProbe{
			Kind: kind, TargetDigest: fx64("target-" + string(kind)),
			TargetAccessPolicyDigest: fx64("policy-" + string(kind)),
			Expected:                 "denied", Outcome: outcome,
		}
		if proof {
			p.ControllerTargetProofDigest = fx64("proof-" + string(kind))
		}
		return p
	}
	return []FormalAccessProbe{
		denied(FProbeProtectedRootTraverse, "permission-denied", false),
		denied(FProbeProtectedRootList, "not-found", true),
		denied(FProbeProtectedRootRead, "permission-denied", false),
		denied(FProbeAuditRead, "permission-denied", false),
		denied(FProbeAuthorStateRead, "permission-denied", false),
		denied(FProbeActiveSiblingRead, "permission-denied", false),
		denied(FProbePriorCaseStateRead, "permission-denied", false),
		denied(FProbeRetiredWorkspaceRead, "permission-denied", false),
		{Kind: FProbeOwnWorkspaceRead, TargetDigest: fx64("target-own"),
			TargetAccessPolicyDigest: fx64("policy-own"), Expected: "readable", Outcome: "readable"},
	}
}

func fxWorkerProbes(plan *CoreExecutionPlanReceipt) []ProtectedWorkerProbe {
	var probes []ProtectedWorkerProbe
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			probes = append(probes, ProtectedWorkerProbe{
				Host: h, WorkerSlot: slot,
				ChildIdentityDigest:     fx64("child-" + h),
				ExecutionTemplateDigest: plan.NormalizedCoreExecutionTemplateDigest,
				AccessBoundaryDigest:    fx64("boundary-" + h),
				Probes:                  fxProbeMatrix(),
			})
		}
	}
	return probes
}

func fxProtectedReceipt(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	return &ProtectedExecutionReceipt{
		BoundaryKind:                 plan.CoreBoundaryKind,
		IsolationConfigDigest:        fx64("isolation-config"),
		ProtectedRootDigest:          fx64("protected-root"),
		AuthorReviewStateRootsDigest: fx64("author-roots"),
		FormalStateRootsDigest:       fx64("formal-roots"),
		SplitStateAllocatorDigests: map[string]string{
			MembershipCore172: fx64("allocator-core"), MembershipHoldout96: fx64("allocator-holdout"),
		},
		RequiredConcurrency:                   plan.Concurrency,
		IsolatedWorkerCapacity:                plan.Concurrency + 2,
		WorkerIdentitySetDigest:               fx64("worker-idset-raw"),
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            plan.NormalizedCoreExecutionTemplateDigest,
		CoreExecutionPlanDigest:               plan.ReceiptDigest,
		WorkerProbes:                          fxWorkerProbes(plan),
		ProbeMatrixDigest:                     fx64("probe-matrix"),
		ProbedAt:                              "2026-09-01T00:00:00Z",
		ReceiptDigest:                         fx64("protected-receipt"),
	}
}

func fxCanary(seriesID, skillDigest, toolIdentity, templateDigest, host string, slot int) *WorkspaceCanaryReceipt {
	return &WorkspaceCanaryReceipt{
		SeriesID: seriesID, Host: host, SkillDigest: skillDigest,
		ToolIdentityDigest: toolIdentity, ExecutionTemplateDigest: templateDigest, WorkerSlot: slot,
		ChildIdentityDigest: fx64("child-" + host), AccessBoundaryDigest: fx64("boundary-" + host),
		CanaryWorkspaceDigest: fx64("canary-ws"), ExpectedFileDigest: fx64("canary-file"),
		ObservedCWDDigest: fx64("canary-ws"), ObservedFileDigest: fx64("canary-file"),
		Status: "pass", ReceiptDigest: fx64("canary-receipt"),
	}
}

func stubGreenRunner(t *testing.T) {
	t.Helper()
	t.Cleanup(func(orig func(string) ([]GreenCommand, error)) func() {
		return func() { fixedSuiteRunner = orig }
	}(fixedSuiteRunner))
	fixedSuiteRunner = okSuiteRunner
}

func fxValidatorFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "validate-agent-skill.mjs")
	writeFile(t, filepath.Dir(p), filepath.Base(p), []byte("// fictional validator"))
	return p
}

func TestCorePlanStableFieldsValidate(t *testing.T) {
	if err := ValidateCoreExecutionPlan(nil); err == nil {
		t.Fatal("nil plan must be rejected")
	}
	if err := ValidateCoreExecutionPlan(fxSeriesPlan()); err != nil {
		t.Fatalf("fixture plan must validate: %v", err)
	}
	drift := func(name string, m func(p *CoreExecutionPlanReceipt)) {
		p := fxSeriesPlan()
		m(p)
		if err := ValidateCoreExecutionPlan(p); err == nil {
			t.Fatalf("core plan must reject %s", name)
		}
	}
	drift("schema_version 2", func(p *CoreExecutionPlanReceipt) { p.SchemaVersion = 2 })
	drift("empty plan_id", func(p *CoreExecutionPlanReceipt) { p.PlanID = "" })
	drift("empty core manifest digest", func(p *CoreExecutionPlanReceipt) { p.CoreManifestDigest = "" })
	drift("empty runner revision", func(p *CoreExecutionPlanReceipt) { p.RunnerRevision = "" })
	drift("empty runner digest", func(p *CoreExecutionPlanReceipt) { p.RunnerDigest = "" })
	drift("empty judge rule digest", func(p *CoreExecutionPlanReceipt) { p.JudgeRuleDigest = "" })
	drift("two hosts", func(p *CoreExecutionPlanReceipt) { p.Hosts = p.Hosts[:2] })
	drift("four hosts", func(p *CoreExecutionPlanReceipt) { p.Hosts = append(p.Hosts, "fourth") })
	drift("unknown host substituted", func(p *CoreExecutionPlanReceipt) { p.Hosts[1] = "ghost" })
	drift("missing tool identity", func(p *CoreExecutionPlanReceipt) { delete(p.ToolIdentityDigests, HostCodex) })
	drift("blank tool identity", func(p *CoreExecutionPlanReceipt) { p.ToolIdentityDigests[HostOpenCode] = "" })
	drift("zero timeout", func(p *CoreExecutionPlanReceipt) { p.TimeoutSeconds = 0 })
	drift("negative concurrency", func(p *CoreExecutionPlanReceipt) { p.Concurrency = -1 })
	drift("two case-order seeds", func(p *CoreExecutionPlanReceipt) {
		p.CaseOrderSeeds = map[int]string{1: fx64("a"), 2: fx64("b")}
	})
	drift("missing ordinal 2 seed", func(p *CoreExecutionPlanReceipt) {
		p.CaseOrderSeeds = map[int]string{1: fx64("a"), 3: fx64("c"), 4: fx64("d")}
	})
	drift("extra ordinal seed", func(p *CoreExecutionPlanReceipt) { p.CaseOrderSeeds[4] = fx64("extra") })
	drift("invalid boundary kind", func(p *CoreExecutionPlanReceipt) { p.CoreBoundaryKind = BoundaryKind("sudo") })
	drift("missing worker identity set digest", func(p *CoreExecutionPlanReceipt) { p.NormalizedCoreWorkerIdentitySetDigest = "" })
	drift("missing boundary template digest", func(p *CoreExecutionPlanReceipt) { p.NormalizedCoreBoundaryTemplateDigest = "" })
	drift("missing execution template digest", func(p *CoreExecutionPlanReceipt) { p.NormalizedCoreExecutionTemplateDigest = "" })
	drift("unsealed (no receipt digest)", func(p *CoreExecutionPlanReceipt) { p.ReceiptDigest = "" })
	drift("unsealed (no seal digest)", func(p *CoreExecutionPlanReceipt) { p.SealDigest = "" })
}

func TestCorePlanSharedBaselineCandidateImport(t *testing.T) {
	plan := fxSeriesPlan()
	if err := ValidateCoreExecutionPlan(plan); err != nil {
		t.Fatal(err)
	}
	// The SC-5 baseline (core-only dev comparison) and the post-change
	// candidate (official-dual) import the SAME sealed plan digest.
	baseline := fxSeriesManifest(PurposeDevComparison)
	candidate := fxSeriesManifest(PurposeOfficialDual)
	baseline.CoreExecutionPlanDigest = plan.ReceiptDigest
	candidate.CoreExecutionPlanDigest = plan.ReceiptDigest
	// The plan deliberately does not bind the evaluated skill: baseline and
	// candidate drift only in skill identity yet share one plan.
	baseline.SkillDigest = fx64("skill-baseline")
	baseline.SkillSnapshotDigest = fx64("snapshot-baseline")
	baseline.SkillVersion = "v0.2.7-baseline"
	candidate.SkillDigest = fx64("skill-candidate")
	if err := ValidateFormalSeriesManifest(baseline); err != nil {
		t.Fatalf("baseline series must import the shared plan: %v", err)
	}
	if err := ValidateFormalSeriesManifest(candidate); err != nil {
		t.Fatalf("candidate series must import the shared plan: %v", err)
	}
	// The candidate binding carries the identical plan digest.
	binding := fxCandidateBinding(plan)
	if binding.CoreExecutionPlanDigest != plan.ReceiptDigest {
		t.Fatal("candidate binding must reference the same frozen plan digest")
	}
	digest, err := CandidateBindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	candidate.CandidateBindingDigest = digest
	if err := ValidateFormalSeriesManifest(candidate); err != nil {
		t.Fatalf("candidate series must carry its binding digest: %v", err)
	}
}

func TestSeriesPurposeSplitContract(t *testing.T) {
	if err := ValidateFormalSeriesManifest(nil); err == nil {
		t.Fatal("nil series manifest must be rejected")
	}
	if err := ValidateFormalSeriesManifest(fxSeriesManifest(PurposeOfficialDual)); err != nil {
		t.Fatalf("official-dual fixture must validate: %v", err)
	}
	breakDual := func(name string, m func(x *FormalSeriesManifest)) {
		x := fxSeriesManifest(PurposeOfficialDual)
		m(x)
		if err := ValidateFormalSeriesManifest(x); err == nil {
			t.Fatalf("official-dual must reject %s", name)
		}
	}
	breakDual("unbound holdout96", func(x *FormalSeriesManifest) {
		delete(x.DatasetManifests, MembershipHoldout96)
		delete(x.QuestionCount, MembershipHoldout96)
	})
	breakDual("extra dev-extension split", func(x *FormalSeriesManifest) {
		x.DatasetManifests[MembershipDevExt] = fx64("dev-ext")
		x.QuestionCount[MembershipDevExt] = 12
	})
	breakDual("null candidate binding", func(x *FormalSeriesManifest) { x.CandidateBindingDigest = "" })
	breakDual("null protected execution receipt", func(x *FormalSeriesManifest) { x.ProtectedExecutionReceiptDigest = "" })
	breakDual("null protected execution policy", func(x *FormalSeriesManifest) { x.ProtectedExecutionPolicyDigest = "" })

	if err := ValidateFormalSeriesManifest(fxSeriesManifest(PurposeDevComparison)); err != nil {
		t.Fatalf("dev-comparison fixture must validate: %v", err)
	}
	breakDev := func(name string, m func(x *FormalSeriesManifest)) {
		x := fxSeriesManifest(PurposeDevComparison)
		m(x)
		if err := ValidateFormalSeriesManifest(x); err == nil {
			t.Fatalf("dev-comparison must reject %s", name)
		}
	}
	breakDev("bound holdout96", func(x *FormalSeriesManifest) {
		x.DatasetManifests[MembershipHoldout96] = fx64("holdout-manifest")
		x.QuestionCount[MembershipHoldout96] = 96
	})
	breakDev("set candidate binding", func(x *FormalSeriesManifest) { x.CandidateBindingDigest = fx64("candidate-binding") })
	breakDev("set protected execution receipt", func(x *FormalSeriesManifest) { x.ProtectedExecutionReceiptDigest = fx64("protected-receipt") })
	breakDev("set protected execution policy", func(x *FormalSeriesManifest) { x.ProtectedExecutionPolicyDigest = fx64("policy") })

	if err := ValidateFormalSeriesManifest(fxSeriesManifest(SeriesPurpose("official-only"))); err == nil {
		t.Fatal("unknown purpose must be rejected")
	}
}

func TestSeriesQuestionCountExact(t *testing.T) {
	if n, err := ExpectedQuestionCount(MembershipCore172); err != nil || n != 172 {
		t.Fatalf("core172 = %d, %v; want 172, nil", n, err)
	}
	if n, err := ExpectedQuestionCount(MembershipHoldout96); err != nil || n != 96 {
		t.Fatalf("holdout96 = %d, %v; want 96, nil", n, err)
	}
	for _, bad := range []string{MembershipDevExt, SplitHoldout, "", "core172 "} {
		if _, err := ExpectedQuestionCount(bad); err == nil {
			t.Fatalf("membership %q must not be a formal-series split", bad)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(m *FormalSeriesManifest)
	}{
		{"core 171", func(m *FormalSeriesManifest) { m.QuestionCount[MembershipCore172] = 171 }},
		{"holdout 97", func(m *FormalSeriesManifest) { m.QuestionCount[MembershipHoldout96] = 97 }},
		{"zero core", func(m *FormalSeriesManifest) { m.QuestionCount[MembershipCore172] = 0 }},
		{"missing core", func(m *FormalSeriesManifest) { delete(m.QuestionCount, MembershipCore172) }},
		{"missing holdout", func(m *FormalSeriesManifest) { delete(m.QuestionCount, MembershipHoldout96) }},
		{"extra coverage", func(m *FormalSeriesManifest) { m.QuestionCount[MembershipDevExt] = 12 }},
	} {
		m := fxSeriesManifest(PurposeOfficialDual)
		tc.edit(m)
		if err := ValidateFormalSeriesManifest(m); err == nil {
			t.Fatalf("official-dual must reject question_count with %s", tc.name)
		}
	}
}

func TestSeriesPreSealPrerequisites(t *testing.T) {
	drop := func(name string, m func(x *FormalSeriesManifest)) {
		x := fxSeriesManifest(PurposeOfficialDual)
		m(x)
		if err := ValidateFormalSeriesManifest(x); err == nil {
			t.Fatalf("series seal must require %s", name)
		}
	}
	drop("series_id", func(x *FormalSeriesManifest) { x.SeriesID = "" })
	drop("a core execution plan", func(x *FormalSeriesManifest) { x.CoreExecutionPlanDigest = "" })
	drop("three hosts", func(x *FormalSeriesManifest) { x.Hosts = x.Hosts[:2] })
	drop("all three ordinals", func(x *FormalSeriesManifest) { x.RequiredOrdinals = []int{1, 2} })
	drop("ordinal order [1,2,3]", func(x *FormalSeriesManifest) { x.RequiredOrdinals = []int{1, 3, 2} })
	drop("exact-skill package-validation receipt", func(x *FormalSeriesManifest) { x.SkillPackageValidationReceiptDigest = "" })
	drop("series-prepare green-test receipt", func(x *FormalSeriesManifest) { x.GreenTestReceiptDigest = "" })
	drop("series-prepare identity digest", func(x *FormalSeriesManifest) { x.SeriesPrepareIdentityDigest = "" })
	drop("the seal (manifest digest)", func(x *FormalSeriesManifest) { x.ManifestDigest = "" })

	// The series-prepare green receipt is the pre-seal attestation: it must
	// bind the exact skill snapshot AND the exact-skill package-validation
	// receipt the series is about to seal.
	validator := fxValidatorFile(t)
	stubGreenRunner(t)
	snapshot, pv := fx64("snapshot"), fx64("pv-receipt")
	prepare, err := CreateGreenTestReceipt(SuiteSeriesPrepare, validator, "", GreenBindings{
		SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &pv,
	})
	if err != nil {
		t.Fatalf("series-prepare receipt refused: %v", err)
	}
	if prepare.StableIdentityDigest == nil || *prepare.StableIdentityDigest == "" {
		t.Fatal("series-prepare receipt must carry a stable identity digest")
	}
	ok := GreenBindings{SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &pv}
	if err := VerifyGreenTestReceipt(prepare, SuiteSeriesPrepare, validator, ok); err != nil {
		t.Fatalf("matching series-prepare receipt must verify: %v", err)
	}
	otherPV := fx64("other-pv-receipt")
	if err := VerifyGreenTestReceipt(prepare, SuiteSeriesPrepare, validator, GreenBindings{
		SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &otherPV,
	}); err == nil {
		t.Fatal("seal must reject a series-prepare receipt bound to another skill's package-validation receipt")
	}
	otherSnapshot := fx64("other-snapshot")
	if err := VerifyGreenTestReceipt(prepare, SuiteSeriesPrepare, validator, GreenBindings{
		SnapshotDigest: &otherSnapshot, PackageValidationReceiptDigest: &pv,
	}); err == nil {
		t.Fatal("seal must reject a series-prepare receipt bound to another snapshot")
	}
	// A series-prepare identity that the receipt never produced is not a
	// pre-seal prerequisite.
	if *prepare.StableIdentityDigest == fx64("series-prepare-identity") {
		t.Fatal("fixture identity digest must not collide with a real stable identity")
	}
}

func TestCandidateBindingDigestStableKey(t *testing.T) {
	plan := fxSeriesPlan()
	binding := fxCandidateBinding(plan)
	d1, err := CandidateBindingDigest(binding)
	if err != nil {
		t.Fatalf("complete binding must digest: %v", err)
	}
	d2, err := CandidateBindingDigest(binding)
	if err != nil || d1 != d2 {
		t.Fatalf("digest must be deterministic: %q vs %q (%v)", d1, d2, err)
	}
	if again, _ := CandidateBindingDigest(fxCandidateBinding(plan)); again != d1 {
		t.Fatal("equal bindings must digest identically")
	}
	if d, err := CandidateBindingDigest(nil); err == nil || d != "" {
		t.Fatal("nil binding must fail closed")
	}
	for _, f := range []struct {
		name   string
		mutate func(x *CandidateBindingV1)
	}{
		{"schema_version 2", func(x *CandidateBindingV1) { x.SchemaVersion = 2 }},
		{"dev-comparison purpose", func(x *CandidateBindingV1) { x.Purpose = PurposeDevComparison }},
		{"unknown purpose", func(x *CandidateBindingV1) { x.Purpose = SeriesPurpose("official") }},
		{"skill snapshot digest", func(x *CandidateBindingV1) { x.SkillSnapshotDigest = "" }},
		{"skill snapshot anchor digest", func(x *CandidateBindingV1) { x.SkillSnapshotAnchorDigest = "" }},
		{"skill digest", func(x *CandidateBindingV1) { x.SkillDigest = "" }},
		{"package-validation receipt digest", func(x *CandidateBindingV1) { x.SkillPackageValidationReceiptDigest = "" }},
		{"validator revision", func(x *CandidateBindingV1) { x.ValidatorRevision = "" }},
		{"validator digest", func(x *CandidateBindingV1) { x.ValidatorDigest = "" }},
		{"runner revision", func(x *CandidateBindingV1) { x.RunnerRevision = "" }},
		{"runner digest", func(x *CandidateBindingV1) { x.RunnerDigest = "" }},
		{"judge rule digest", func(x *CandidateBindingV1) { x.JudgeRuleDigest = "" }},
		{"core execution plan digest", func(x *CandidateBindingV1) { x.CoreExecutionPlanDigest = "" }},
		{"tool configuration digest", func(x *CandidateBindingV1) { x.ToolConfigurationDigest = "" }},
		{"execution environment digest", func(x *CandidateBindingV1) { x.ExecutionEnvironmentDigest = "" }},
		{"protected execution policy digest", func(x *CandidateBindingV1) { x.ProtectedExecutionPolicyDigest = "" }},
		{"series-prepare identity digest", func(x *CandidateBindingV1) { x.SeriesPrepareIdentityDigest = "" }},
		{"nil dataset identities", func(x *CandidateBindingV1) { x.DatasetIdentities = nil }},
		{"single dataset identity", func(x *CandidateBindingV1) { delete(x.DatasetIdentities, MembershipHoldout96) }},
		{"two tool identities", func(x *CandidateBindingV1) { delete(x.ToolIdentityDigests, HostCodex) }},
		{"two case-order seeds", func(x *CandidateBindingV1) { delete(x.CaseOrderSeeds, 3) }},
		{"zero timeout", func(x *CandidateBindingV1) { x.TimeoutSeconds = 0 }},
		{"zero concurrency", func(x *CandidateBindingV1) { x.Concurrency = 0 }},
	} {
		x := fxCandidateBinding(plan)
		f.mutate(x)
		if _, err := CandidateBindingDigest(x); err == nil {
			t.Fatalf("candidate binding digest must fail closed on %s", f.name)
		}
	}
}

func TestCandidateBindingDigestIgnoresSeriesContext(t *testing.T) {
	plan := fxSeriesPlan()
	digest, err := CandidateBindingDigest(fxCandidateBinding(plan))
	if err != nil {
		t.Fatal(err)
	}
	// Two attempts from two DIFFERENT series (own series id, own series
	// manifest digest, own pre-holdout receipt, own core-leg digest) built
	// from one candidate skill share one stable digest.
	first := fxHoldoutAttempt("series-fx-1", fx64("series-manifest-1"), digest, "1")
	second := fxHoldoutAttempt("series-fx-2", fx64("series-manifest-2"), digest, "2")
	first.State = "complete-pass"
	second.State = "complete-pass"
	receipt := fxHoldoutBinding(digest, first, second)
	receipt.State = "consumed"
	receipt.ConsumedBySeries = "series-fx-2"
	if err := ValidateHoldoutBinding(receipt); err != nil {
		t.Fatalf("two series sharing one stable candidate digest must coexist: %v", err)
	}
}

func TestCandidateBindingDigestDriftsOnPlanInputs(t *testing.T) {
	plan := fxSeriesPlan()
	base, err := CandidateBindingDigest(fxCandidateBinding(plan))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		name  string
		drift func(x *CandidateBindingV1)
	}{
		{"core execution plan digest", func(x *CandidateBindingV1) { x.CoreExecutionPlanDigest = fx64("plan-receipt-2") }},
		{"one tool identity", func(x *CandidateBindingV1) { x.ToolIdentityDigests[HostClaude] = fx64("ti-claude-v2") }},
		{"timeout", func(x *CandidateBindingV1) { x.TimeoutSeconds = plan.TimeoutSeconds + 1 }},
		{"concurrency", func(x *CandidateBindingV1) { x.Concurrency = plan.Concurrency + 1 }},
		{"one case-order seed", func(x *CandidateBindingV1) { x.CaseOrderSeeds[2] = fx64("seed-2-v2") }},
		{"skill digest", func(x *CandidateBindingV1) { x.SkillDigest = fx64("skill-v2") }},
		{"dataset identity", func(x *CandidateBindingV1) { x.DatasetIdentities[MembershipHoldout96] = fx64("holdout-manifest-v2") }},
		{"tool configuration", func(x *CandidateBindingV1) { x.ToolConfigurationDigest = fx64("tool-config-v2") }},
		{"execution environment", func(x *CandidateBindingV1) { x.ExecutionEnvironmentDigest = fx64("exec-env-v2") }},
		{"protected execution policy", func(x *CandidateBindingV1) { x.ProtectedExecutionPolicyDigest = fx64("policy-v2") }},
		{"series-prepare identity", func(x *CandidateBindingV1) { x.SeriesPrepareIdentityDigest = fx64("series-prepare-identity-v2") }},
		{"package-validation receipt", func(x *CandidateBindingV1) { x.SkillPackageValidationReceiptDigest = fx64("pv-v2") }},
		{"judge rule", func(x *CandidateBindingV1) { x.JudgeRuleDigest = fx64("judge-rules-v2") }},
	} {
		x := fxCandidateBinding(plan)
		f.drift(x)
		d, err := CandidateBindingDigest(x)
		if err != nil {
			t.Fatalf("%s: drifted binding must still digest: %v", f.name, err)
		}
		if d == base {
			t.Fatalf("candidate digest must drift on %s", f.name)
		}
	}
}

func TestHoldoutBindingLifecycle(t *testing.T) {
	plan := fxSeriesPlan()
	digest, err := CandidateBindingDigest(fxCandidateBinding(plan))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateHoldoutBinding(nil); err == nil {
		t.Fatal("nil binding receipt must be rejected")
	}
	drop := func(name string, m func(b *HoldoutBindingReceipt)) {
		b := fxHoldoutBinding(digest, fxHoldoutAttempt("series-fx-1", fx64("series-manifest-1"), digest, "1"))
		m(b)
		if err := ValidateHoldoutBinding(b); err == nil {
			t.Fatalf("holdout binding must reject %s", name)
		}
	}
	drop("empty dataset version", func(b *HoldoutBindingReceipt) { b.DatasetVersion = "" })
	drop("empty dataset manifest digest", func(b *HoldoutBindingReceipt) { b.DatasetManifestDigest = "" })
	drop("empty candidate binding digest", func(b *HoldoutBindingReceipt) { b.CandidateBindingDigest = "" })
	drop("empty first primary start", func(b *HoldoutBindingReceipt) { b.FirstPrimaryStartedAt = "" })
	drop("no attempts", func(b *HoldoutBindingReceipt) { b.SeriesAttempts = nil })
	drop("incomplete attempt identity", func(b *HoldoutBindingReceipt) { b.SeriesAttempts[0].CoreLegCompletionDigest = "" })
	drop("attempt without a pre-holdout receipt", func(b *HoldoutBindingReceipt) { b.SeriesAttempts[0].PreHoldoutGreenTestReceiptDigest = "" })
	drop("attempt with a different candidate digest", func(b *HoldoutBindingReceipt) { b.SeriesAttempts[0].CandidateBindingDigestSelf = fx64("other-binding") })
	drop("unknown attempt state", func(b *HoldoutBindingReceipt) { b.SeriesAttempts[0].State = "sealed" })
	drop("duplicate series manifest digest", func(b *HoldoutBindingReceipt) {
		b.SeriesAttempts = append(b.SeriesAttempts, fxHoldoutAttempt("series-fx-2", fx64("series-manifest-1"), digest, "2"))
	})
	// The pre-holdout receipt and the core-leg completion are per-series: a
	// recovery series re-runs core172 from zero and earns fresh ones.
	drop("reused pre-holdout receipt digest", func(b *HoldoutBindingReceipt) {
		reused := fxHoldoutAttempt("series-fx-2", fx64("series-manifest-2"), digest, "2")
		reused.PreHoldoutGreenTestReceiptDigest = b.SeriesAttempts[0].PreHoldoutGreenTestReceiptDigest
		b.SeriesAttempts = append(b.SeriesAttempts, reused)
	})
	drop("reused core-leg completion digest", func(b *HoldoutBindingReceipt) {
		reused := fxHoldoutAttempt("series-fx-2", fx64("series-manifest-2"), digest, "2")
		reused.CoreLegCompletionDigest = b.SeriesAttempts[0].CoreLegCompletionDigest
		b.SeriesAttempts = append(b.SeriesAttempts, reused)
	})
	drop("frozen binding with a completed series", func(b *HoldoutBindingReceipt) { b.SeriesAttempts[0].State = "complete-pass" })
	drop("consumed_by_series that never completed", func(b *HoldoutBindingReceipt) {
		b.State = "consumed"
		b.ConsumedBySeries = "series-fx-1"
	})
	drop("consumed_by_series without an attempt entry", func(b *HoldoutBindingReceipt) {
		b.State = "consumed"
		b.SeriesAttempts[0].State = "complete-pass"
		b.ConsumedBySeries = "series-fx-9"
	})
	drop("frozen with consumed_by_series", func(b *HoldoutBindingReceipt) { b.ConsumedBySeries = "series-fx-1" })
	drop("consumed without consumed_by_series", func(b *HoldoutBindingReceipt) { b.State = "consumed" })
	drop("unknown state", func(b *HoldoutBindingReceipt) { b.State = "sealed" })
	drop("empty receipt digest", func(b *HoldoutBindingReceipt) { b.ReceiptDigest = "" })

	frozen := fxHoldoutBinding(digest, fxHoldoutAttempt("series-fx-1", fx64("series-manifest-1"), digest, "1"))
	if err := ValidateHoldoutBinding(frozen); err != nil {
		t.Fatalf("frozen binding must validate: %v", err)
	}
	consumed := *frozen
	consumed.SeriesAttempts[0].State = "complete-pass"
	consumed.State = "consumed"
	consumed.ConsumedBySeries = "series-fx-1"
	if err := ValidateHoldoutBinding(&consumed); err != nil {
		t.Fatalf("consumed binding must validate: %v", err)
	}
}

func TestHoldoutBindingRecoveryAfterInvalid(t *testing.T) {
	plan := fxSeriesPlan()
	digest, err := CandidateBindingDigest(fxCandidateBinding(plan))
	if err != nil {
		t.Fatal(err)
	}
	oldManifest, newManifest := fx64("series-manifest-1"), fx64("series-manifest-2")

	// The first series went INVALID before holdout ordinal 1: the holdout
	// binding must stay frozen with the attempt on record and must not name
	// a consuming series.
	invalid := fxHoldoutBinding(digest, fxHoldoutAttempt("series-fx-1", oldManifest, digest, "1"))
	invalid.SeriesAttempts[0].State = "invalid"
	if err := ValidateHoldoutBinding(invalid); err != nil {
		t.Fatalf("invalid-before-holdout attempt must stay on a frozen binding: %v", err)
	}
	if invalid.State != "frozen" || invalid.ConsumedBySeries != "" {
		t.Fatal("a binding after core-invalid must stay frozen with null consumed_by_series")
	}

	// Recovery appends a NEW series: new series id + manifest digest, the
	// SAME stable candidate digest, fresh pre-holdout receipt and fresh
	// core-leg receipt-set digest.
	recovered := *invalid
	recovered.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, invalid.SeriesAttempts...),
		fxHoldoutAttempt("series-fx-2", newManifest, digest, "2"))
	if err := ValidateHoldoutBinding(&recovered); err != nil {
		t.Fatalf("binding-after-INVALID recovery must append cleanly: %v", err)
	}

	// Reusing the OLD series manifest digest is not a recovery.
	reusedManifest := *invalid
	reusedManifest.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, invalid.SeriesAttempts...),
		fxHoldoutAttempt("series-fx-2", oldManifest, digest, "2"))
	if err := ValidateHoldoutBinding(&reusedManifest); err == nil {
		t.Fatal("recovery must not reuse the previous series manifest digest")
	}

	// Any drift in a stable input yields a different candidate, which is not
	// a recovery of this holdout version.
	driftedPlan := fxSeriesPlan()
	driftedPlan.TimeoutSeconds = plan.TimeoutSeconds + 1
	otherDigest, err := CandidateBindingDigest(fxCandidateBinding(driftedPlan))
	if err != nil {
		t.Fatal(err)
	}
	if otherDigest == digest {
		t.Fatal("a drifted plan must produce a different stable digest")
	}
	reusedKey := *invalid
	reusedKey.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, invalid.SeriesAttempts...),
		fxHoldoutAttempt("series-fx-2", newManifest, otherDigest, "2"))
	if err := ValidateHoldoutBinding(&reusedKey); err == nil {
		t.Fatal("recovery must bind the SAME stable candidate digest")
	}

	// A recovery series re-runs core172 from zero: neither the previous
	// pre-holdout receipt nor the previous core-leg completion may be reused.
	reusedPreHoldout := *invalid
	stalePre := fxHoldoutAttempt("series-fx-2", newManifest, digest, "2")
	stalePre.PreHoldoutGreenTestReceiptDigest = invalid.SeriesAttempts[0].PreHoldoutGreenTestReceiptDigest
	reusedPreHoldout.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, invalid.SeriesAttempts...), stalePre)
	if err := ValidateHoldoutBinding(&reusedPreHoldout); err == nil {
		t.Fatal("recovery must not reuse the previous series' pre-holdout receipt digest")
	}
	reusedCoreLeg := *invalid
	staleLeg := fxHoldoutAttempt("series-fx-2", newManifest, digest, "2")
	staleLeg.CoreLegCompletionDigest = invalid.SeriesAttempts[0].CoreLegCompletionDigest
	reusedCoreLeg.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, invalid.SeriesAttempts...), staleLeg)
	if err := ValidateHoldoutBinding(&reusedCoreLeg); err == nil {
		t.Fatal("recovery must not reuse the previous series' core-leg completion digest")
	}

	// A holdout that was already CONSUMED and later went INVALID recovers the
	// same way: the new attempt joins a consumed binding whose consuming
	// series completed earlier, on the same stable digest.
	consumed := fxHoldoutBinding(digest, fxHoldoutAttempt("series-fx-1", oldManifest, digest, "1"))
	consumed.SeriesAttempts[0].State = "complete-pass"
	consumed.State = "consumed"
	consumed.ConsumedBySeries = "series-fx-1"
	if err := ValidateHoldoutBinding(consumed); err != nil {
		t.Fatalf("consumed binding must validate: %v", err)
	}
	recoveredConsumed := *consumed
	recoveredConsumed.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, consumed.SeriesAttempts...),
		fxHoldoutAttempt("series-fx-2", newManifest, digest, "2"))
	if err := ValidateHoldoutBinding(&recoveredConsumed); err != nil {
		t.Fatalf("binding-after-INVALID recovery on a consumed holdout must append cleanly: %v", err)
	}
}

func TestSeriesPreHoldoutAttestationFreshness(t *testing.T) {
	validator := fxValidatorFile(t)
	stubGreenRunner(t)
	plan := fxSeriesPlan()
	digest, err := CandidateBindingDigest(fxCandidateBinding(plan))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, pv := fx64("snapshot"), fx64("pv-receipt")
	oldManifest, newManifest := fx64("series-manifest-1"), fx64("series-manifest-2")
	oldCoreLeg, newCoreLeg := fx64("core-leg-1"), fx64("core-leg-2")

	attest := func(manifestDigest, coreLeg string) *GreenTestReceipt {
		t.Helper()
		r, err := CreateGreenTestReceipt(SuitePreHoldout, validator, "", GreenBindings{
			SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &pv,
			SeriesManifestDigest: &manifestDigest, CandidateBindingDigest: &digest,
			CoreLegCompletionDigest: &coreLeg,
		})
		if err != nil {
			t.Fatalf("pre-holdout attestation refused: %v", err)
		}
		return r
	}
	want := func(manifestDigest, coreLeg string) GreenBindings {
		return GreenBindings{
			SeriesManifestDigest: &manifestDigest, CandidateBindingDigest: &digest,
			CoreLegCompletionDigest: &coreLeg,
		}
	}
	// Creation fails closed: a pre-holdout attestation cannot exist without
	// the series manifest, the stable digest and the complete core-leg set.
	if _, err := CreateGreenTestReceipt(SuitePreHoldout, validator, "", GreenBindings{
		SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &pv,
	}); err == nil {
		t.Fatal("pre-holdout attestation must bind series manifest, stable digest and core-leg completion")
	}

	// The previous series' attestation verifies only against its own series.
	oldAttestation := attest(oldManifest, oldCoreLeg)
	if err := VerifyGreenTestReceipt(oldAttestation, SuitePreHoldout, validator, want(oldManifest, oldCoreLeg)); err != nil {
		t.Fatalf("old pre-holdout attestation must verify against its own series: %v", err)
	}
	// Reusing it for the recovery series is refused on both the manifest and
	// the core-leg binding.
	if err := VerifyGreenTestReceipt(oldAttestation, SuitePreHoldout, validator, want(newManifest, newCoreLeg)); err == nil {
		t.Fatal("recovery must not reuse the previous series' pre-holdout receipt")
	}
	if err := VerifyGreenTestReceipt(oldAttestation, SuitePreHoldout, validator, want(newManifest, oldCoreLeg)); err == nil {
		t.Fatal("a stale pre-holdout receipt must not satisfy the new series' manifest binding")
	}
	// The fresh attestation binds the new manifest + the SAME stable digest +
	// the complete new core-leg receipt set.
	fresh := attest(newManifest, newCoreLeg)
	if err := VerifyGreenTestReceipt(fresh, SuitePreHoldout, validator, want(newManifest, newCoreLeg)); err != nil {
		t.Fatalf("fresh pre-holdout attestation must verify: %v", err)
	}
	if err := VerifyGreenTestReceipt(fresh, SuitePreHoldout, validator, want(newManifest, oldCoreLeg)); err == nil {
		t.Fatal("fresh attestation must bind the new core-leg receipt-set digest")
	}

	// An attestation computed for a DIFFERENT candidate (drifted stable
	// input) never verifies against this holdout version.
	driftedPlan := fxSeriesPlan()
	driftedPlan.TimeoutSeconds = plan.TimeoutSeconds + 1
	otherDigest, err := CandidateBindingDigest(fxCandidateBinding(driftedPlan))
	if err != nil {
		t.Fatal(err)
	}
	other := otherDigest
	otherAttestation, err := CreateGreenTestReceipt(SuitePreHoldout, validator, "", GreenBindings{
		SnapshotDigest: &snapshot, PackageValidationReceiptDigest: &pv,
		SeriesManifestDigest: &newManifest, CandidateBindingDigest: &other,
		CoreLegCompletionDigest: &newCoreLeg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyGreenTestReceipt(otherAttestation, SuitePreHoldout, validator, want(newManifest, newCoreLeg)); err == nil {
		t.Fatal("pre-holdout attestation must bind the exact stable candidate digest")
	}
}

func TestProtectedExecutionReceiptSemantics(t *testing.T) {
	plan := fxSeriesPlan()
	if err := ValidateProtectedExecutionReceipt(nil, plan); err == nil {
		t.Fatal("nil protected receipt must be rejected")
	}
	if err := ValidateProtectedExecutionReceipt(fxProtectedReceipt(plan), plan); err != nil {
		t.Fatalf("fixture protected receipt must validate: %v", err)
	}
	drop := func(name string, m func(r *ProtectedExecutionReceipt)) {
		r := fxProtectedReceipt(plan)
		m(r)
		if err := ValidateProtectedExecutionReceipt(r, plan); err == nil {
			t.Fatalf("protected receipt must reject %s", name)
		}
	}
	drop("invalid boundary kind", func(r *ProtectedExecutionReceipt) { r.BoundaryKind = BoundaryKind("chroot") })
	for _, f := range []struct {
		name string
		zero func(r *ProtectedExecutionReceipt)
	}{
		{"isolation config digest", func(r *ProtectedExecutionReceipt) { r.IsolationConfigDigest = "" }},
		{"protected root digest", func(r *ProtectedExecutionReceipt) { r.ProtectedRootDigest = "" }},
		{"worker identity set digest", func(r *ProtectedExecutionReceipt) { r.WorkerIdentitySetDigest = "" }},
		{"execution template set digest", func(r *ProtectedExecutionReceipt) { r.ExecutionTemplateSetDigest = "" }},
		{"probe matrix digest", func(r *ProtectedExecutionReceipt) { r.ProbeMatrixDigest = "" }},
		{"probe timestamp", func(r *ProtectedExecutionReceipt) { r.ProbedAt = "" }},
		{"receipt digest", func(r *ProtectedExecutionReceipt) { r.ReceiptDigest = "" }},
	} {
		drop(f.name, f.zero)
	}
	// Candidate-specific: the receipt binds the exact sealed core plan.
	drop("foreign core plan digest", func(r *ProtectedExecutionReceipt) { r.CoreExecutionPlanDigest = fx64("plan-receipt-2") })
	drop("worker identity template drift", func(r *ProtectedExecutionReceipt) { r.NormalizedCoreWorkerIdentitySetDigest = fx64("worker-idset-v2") })
	drop("concurrency drift", func(r *ProtectedExecutionReceipt) { r.RequiredConcurrency = plan.Concurrency + 1 })
	drop("zero required concurrency", func(r *ProtectedExecutionReceipt) { r.RequiredConcurrency = 0 })
	drop("capacity below concurrency", func(r *ProtectedExecutionReceipt) { r.IsolatedWorkerCapacity = plan.Concurrency - 1 })
	drop("missing holdout allocator", func(r *ProtectedExecutionReceipt) { delete(r.SplitStateAllocatorDigests, MembershipHoldout96) })
	drop("single allocator", func(r *ProtectedExecutionReceipt) {
		r.SplitStateAllocatorDigests = map[string]string{MembershipCore172: fx64("allocator-core")}
	})
	drop("overlapping allocators", func(r *ProtectedExecutionReceipt) {
		r.SplitStateAllocatorDigests[MembershipHoldout96] = r.SplitStateAllocatorDigests[MembershipCore172]
	})
	drop("formal roots reusing author/review roots", func(r *ProtectedExecutionReceipt) {
		r.FormalStateRootsDigest = r.AuthorReviewStateRootsDigest
	})
	drop("missing worker probe slot", func(r *ProtectedExecutionReceipt) { r.WorkerProbes = r.WorkerProbes[1:] })
	drop("duplicate worker probe", func(r *ProtectedExecutionReceipt) {
		r.WorkerProbes = append(r.WorkerProbes, r.WorkerProbes[0])
	})
	drop("unexpected worker probe", func(r *ProtectedExecutionReceipt) {
		r.WorkerProbes = append(r.WorkerProbes, ProtectedWorkerProbe{
			Host: "ghost", WorkerSlot: 1, ChildIdentityDigest: fx64("child"),
			AccessBoundaryDigest: fx64("boundary"), Probes: fxProbeMatrix(),
		})
	})
}

func TestProtectedWorkerProbeMatrix(t *testing.T) {
	plan := fxSeriesPlan()
	good := ProtectedWorkerProbe{
		Host: HostClaude, WorkerSlot: 2, ChildIdentityDigest: fx64("child"),
		ExecutionTemplateDigest: plan.NormalizedCoreExecutionTemplateDigest,
		AccessBoundaryDigest:    fx64("boundary"), Probes: fxProbeMatrix(),
	}
	if err := ValidateWorkerProbe(good, plan); err != nil {
		t.Fatalf("fixture probe matrix must validate: %v", err)
	}
	edit := func(kind FormalProbeKind, f func(p *FormalAccessProbe)) ProtectedWorkerProbe {
		p := good
		probes := make([]FormalAccessProbe, len(good.Probes))
		copy(probes, good.Probes)
		for i := range probes {
			if probes[i].Kind == kind {
				f(&probes[i])
			}
		}
		p.Probes = probes
		return p
	}
	bad := func(name string, p ProtectedWorkerProbe) {
		t.Helper()
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatalf("worker probe must reject %s", name)
		}
	}
	noChild, noBoundary := good, good
	noChild.ChildIdentityDigest = ""
	noBoundary.AccessBoundaryDigest = ""
	bad("empty child identity", noChild)
	bad("empty access boundary", noBoundary)
	below := good
	below.WorkerSlot = 0
	bad("slot below 1", below)

	// Every mandatory kind is required exactly once.
	mandatory := []FormalProbeKind{
		FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
		FProbeAuditRead, FProbeAuthorStateRead, FProbeOwnWorkspaceRead,
	}
	for _, k := range mandatory {
		// Missing: drop the kind entirely.
		missing := good
		var kept []FormalAccessProbe
		for _, pr := range good.Probes {
			if pr.Kind != k {
				kept = append(kept, pr)
			}
		}
		missing.Probes = kept
		bad("missing "+string(k), missing)
		// Duplicated: a second observation of the same kind.
		duplicated := good
		twice := make([]FormalAccessProbe, 0, len(good.Probes)+1)
		for _, pr := range good.Probes {
			twice = append(twice, pr)
			if pr.Kind == k {
				twice = append(twice, pr)
			}
		}
		duplicated.Probes = twice
		bad("duplicated "+string(k), duplicated)
	}
	// Denied probes may only observe denied outcomes.
	bad("denied probe observed readable", edit(FProbeProtectedRootRead, func(pr *FormalAccessProbe) { pr.Outcome = "readable" }))
	bad("denied probe observed partial", edit(FProbeAuditRead, func(pr *FormalAccessProbe) { pr.Outcome = "partial" }))
	bad("not-found without controller proof", edit(FProbeProtectedRootList, func(pr *FormalAccessProbe) { pr.ControllerTargetProofDigest = "" }))
	bad("non-own-workspace probe expecting readable", edit(FProbeAuthorStateRead, func(pr *FormalAccessProbe) { pr.Expected = "readable" }))
	bad("own-workspace probe denied", edit(FProbeOwnWorkspaceRead, func(pr *FormalAccessProbe) { pr.Outcome = "permission-denied" }))
	bad("unknown expectation", edit(FProbeActiveSiblingRead, func(pr *FormalAccessProbe) { pr.Expected = "maybe" }))
	bad("probe without target digest", edit(FProbeProtectedRootTraverse, func(pr *FormalAccessProbe) { pr.TargetDigest = "" }))
	bad("probe without policy digest", edit(FProbePriorCaseStateRead, func(pr *FormalAccessProbe) { pr.TargetAccessPolicyDigest = "" }))
}

func TestSeriesWorkspaceCanaryBinding(t *testing.T) {
	plan := fxSeriesPlan()
	manifest := fxSeriesManifest(PurposeOfficialDual)
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			c := fxCanary(manifest.SeriesID, manifest.SkillDigest, plan.ToolIdentityDigests[h],
				plan.NormalizedCoreExecutionTemplateDigest, h, slot)
			if err := ValidateWorkspaceCanary(c, manifest.SeriesID, manifest.SkillDigest,
				plan.ToolIdentityDigests[h], plan.NormalizedCoreExecutionTemplateDigest, slot); err != nil {
				t.Fatalf("canary %s/%d must validate: %v", h, slot, err)
			}
		}
	}
	c := fxCanary(manifest.SeriesID, manifest.SkillDigest, plan.ToolIdentityDigests[HostClaude],
		plan.NormalizedCoreExecutionTemplateDigest, HostClaude, 1)
	want := func(series, skill, toolIdentity, template string, slot int) error {
		return ValidateWorkspaceCanary(c, series, skill, toolIdentity, template, slot)
	}
	for _, tc := range []struct {
		name  string
		drift func() error
	}{
		{"series drift", func() error {
			return want("series-other", manifest.SkillDigest, plan.ToolIdentityDigests[HostClaude], plan.NormalizedCoreExecutionTemplateDigest, 1)
		}},
		{"skill drift", func() error {
			return want(manifest.SeriesID, fx64("skill-v2"), plan.ToolIdentityDigests[HostClaude], plan.NormalizedCoreExecutionTemplateDigest, 1)
		}},
		{"tool identity drift", func() error {
			return want(manifest.SeriesID, manifest.SkillDigest, fx64("ti-other"), plan.NormalizedCoreExecutionTemplateDigest, 1)
		}},
		{"execution template drift", func() error {
			return want(manifest.SeriesID, manifest.SkillDigest, plan.ToolIdentityDigests[HostClaude], fx64("tpl-other"), 1)
		}},
		{"worker slot mismatch", func() error {
			return want(manifest.SeriesID, manifest.SkillDigest, plan.ToolIdentityDigests[HostClaude], plan.NormalizedCoreExecutionTemplateDigest, 2)
		}},
	} {
		if err := tc.drift(); err == nil {
			t.Fatalf("canary must reject %s", tc.name)
		}
	}
	drop := func(name string, m func(x *WorkspaceCanaryReceipt)) {
		x := fxCanary(manifest.SeriesID, manifest.SkillDigest, plan.ToolIdentityDigests[HostClaude],
			plan.NormalizedCoreExecutionTemplateDigest, HostClaude, 1)
		m(x)
		if err := ValidateWorkspaceCanary(x, manifest.SeriesID, manifest.SkillDigest,
			plan.ToolIdentityDigests[HostClaude], plan.NormalizedCoreExecutionTemplateDigest, 1); err == nil {
			t.Fatalf("canary must reject %s", name)
		}
	}
	drop("missing child identity", func(x *WorkspaceCanaryReceipt) { x.ChildIdentityDigest = "" })
	drop("missing access boundary", func(x *WorkspaceCanaryReceipt) { x.AccessBoundaryDigest = "" })
	drop("cwd observation mismatch", func(x *WorkspaceCanaryReceipt) { x.ObservedCWDDigest = fx64("elsewhere") })
	drop("file observation mismatch", func(x *WorkspaceCanaryReceipt) { x.ObservedFileDigest = fx64("other-file") })
	drop("failed status", func(x *WorkspaceCanaryReceipt) { x.Status = "fail" })
	drop("empty receipt digest", func(x *WorkspaceCanaryReceipt) { x.ReceiptDigest = "" })
}

// ---------- US4 T045 implementation tests (manifest.go lifecycle face) ----------
//
// These exercise the IO/lifecycle surface: sealed plan create/load, the
// purpose-aware series prepare gate with its injected protected-execution and
// canary receipts, the append-only holdout binding and the sealed primary run
// manifest. Everything else (probes, canaries, runs) is fabricated here the
// way the runner side (T049/T051) will produce it.

// implWriteJSON stores v with plain encoding/json. Digest preimages are
// computed through the canonical projections; storage only needs closed-schema
// JSON that StrictParseClosed accepts.
func implWriteJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, name, b)
}

func implTS(offsetMinutes int) string {
	return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).
		Add(time.Duration(offsetMinutes) * time.Minute).UTC().Format(time.RFC3339)
}

func implRunOrder(ids []string, rotate int) []string {
	out := append([]string{}, ids...)
	rotate %= len(out)
	if rotate < 0 {
		rotate += len(out)
	}
	return append(out[rotate:], out[:rotate]...)
}

// implCoreManifest materializes the synthetic 172-case core manifest on disk
// with real payload digests.
func implCoreManifest(t *testing.T) string {
	t.Helper()
	dir, m := syntheticCore172(t)
	digest, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	m.PayloadDigest = digest
	writeFile(t, dir, "manifest.json", mustCanonicalJSON(m))
	return filepath.Join(dir, "manifest.json")
}

// implHoldoutManifest materializes a synthetic 96-case holdout manifest.
func implHoldoutManifest(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	var cases []TriggerCaseV2
	var ids []string
	for i := 0; i < 96; i++ {
		id := fmt.Sprintf("hld-%03d", i+1)
		ids = append(ids, id)
		cases = append(cases, TriggerCaseV2{
			ID: id, SchemaVersion: 2, Split: SplitHoldout, ScoreMembership: MembershipHoldout96,
			Module: "trap-read-pos", Category: "synthetic", Prompt: strPtr("holdout prompt " + id),
			Expect: ExpectV2{Trigger: true, Observable: "synthetic holdout case"},
			Source: "synthetic", Status: StatusActive,
		})
	}
	sort.Strings(ids)
	b, err := CanonicalJSON(CasePayloadFile{Dataset: "synthetic", Version: 2, Cases: cases})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "cases-holdout.json", b)
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "holdout96-fx-v1",
		Split: SplitHoldout, ScoreMembership: MembershipHoldout96, CaseCount: 96,
		CaseIDs:      ids,
		PayloadFiles: []PayloadFileV1{{RelativePath: "cases-holdout.json", LFNormalizedSHA256: LFNormalizedSHA256Bytes(b), CaseIDs: ids}},
	}
	digest, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	m.PayloadDigest = digest
	writeFile(t, dir, "manifest.json", mustCanonicalJSON(m))
	return filepath.Join(dir, "manifest.json"), ids
}

// implSnapshot materializes an immutable snapshot root plus its passing
// package-validation receipt, the way `package validate` produces them.
func implSnapshot(t *testing.T, validatorPath string) (string, *SkillPackageValidationReceipt) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "snapshot")
	writeFile(t, root, "SKILL.md", []byte("---\nname: engram\nversion: 0.0.0-impl\n---\n\nimpl skill body\n"))
	writeFile(t, root, "references/a.md", []byte("impl reference body\n"))
	recs, err := inventoryPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	}
	skillDigest, err := EngramPackageDigest(recs, read)
	if err != nil {
		t.Fatal(err)
	}
	fileRecordsDigest, err := FileRecordsDigest(recs)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := os.ReadFile(validatorPath)
	if err != nil {
		t.Fatal(err)
	}
	validatorDigest := sha256Hex(vb)
	anchor := SnapshotAnchor{SchemaVersion: 1, SnapshotID: "snap-impl-001", SkillDigest: skillDigest,
		FileRecordsDigest: fileRecordsDigest, ValidatorDigest: validatorDigest}
	anchorBody, err := CanonicalJSON(struct {
		SchemaVersion     int    `json:"schema_version"`
		SnapshotID        string `json:"snapshot_id"`
		SkillDigest       string `json:"skill_digest"`
		FileRecordsDigest string `json:"file_records_digest"`
		ValidatorDigest   string `json:"validator_digest"`
	}{anchor.SchemaVersion, anchor.SnapshotID, anchor.SkillDigest, anchor.FileRecordsDigest, anchor.ValidatorDigest})
	if err != nil {
		t.Fatal(err)
	}
	anchor.AnchorDigest = sha256Hex(anchorBody)
	snap := &FrozenSkillPackageSnapshot{
		SchemaVersion: 1, SnapshotID: anchor.SnapshotID, SnapshotRootDigest: fileRecordsDigest,
		SkillDigest: skillDigest, FileRecords: recs,
		ValidatorRevision: "020-validate-agent-skill-v1", ValidatorDigest: validatorDigest,
		SnapshotAnchor: anchor, CreatedAt: implTS(0),
	}
	snap.SnapshotDigest = mustDigest(snap)
	writeFile(t, root, "snapshot.json", mustCanonicalJSON(snap))
	pv := &SkillPackageValidationReceipt{
		SnapshotID: snap.SnapshotID, SnapshotDigest: snap.SnapshotDigest, SnapshotAnchorDigest: anchor.AnchorDigest,
		SkillVersion: "0.0.0-impl", SkillDigest: skillDigest,
		FileRecordsDigest: fileRecordsDigest, FileRecords: recs,
		ValidatorRevision: snap.ValidatorRevision, ValidatorDigest: validatorDigest,
		ValidatorArgvDigest: fx64("impl-argv"), ValidatorOutputDigest: fx64("impl-validator-out"),
		Checks: map[string]bool{
			"description_body_reference_sync":   true,
			"version_bump":                      true,
			"line_reference_digest_consistency": true,
		},
		Passed:      true,
		ValidatedAt: implTS(0),
	}
	digest, err := receiptDigestPV(pv)
	if err != nil {
		t.Fatal(err)
	}
	pv.ReceiptDigest = digest
	return root, pv
}

func implPlanInput(t *testing.T, coreManifestPath, outPath string) CorePlanInput {
	t.Helper()
	runner, err := CurrentRunnerDigest()
	if err != nil {
		t.Fatal(err)
	}
	judge, err := CurrentJudgeRuleDigest()
	if err != nil {
		t.Fatal(err)
	}
	in := CorePlanInput{
		PlanID:           "plan-impl-001",
		CoreManifestPath: coreManifestPath,
		RunnerRevision:   "runner-impl-1", RunnerDigest: runner, JudgeRuleDigest: judge,
		Hosts: []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests: map[string]string{
			HostClaude: fx64("ti-claude"), HostCodex: fx64("ti-codex"), HostOpenCode: fx64("ti-opencode"),
		},
		TimeoutSeconds: 600, Concurrency: 2,
		CaseOrderSeeds:                        map[int]string{1: fx64("seed-1"), 2: fx64("seed-2"), 3: fx64("seed-3")},
		CoreBoundaryKind:                      BoundarySeparateUser,
		NormalizedCoreWorkerIdentitySetDigest: fx64("worker-idset"),
		NormalizedCoreBoundaryTemplateDigest:  fx64("boundary-tpl"),
		NormalizedCoreExecutionTemplateDigest: fx64("exec-tpl"),
		CreatedAt:                             implTS(0),
		OutPath:                               outPath,
	}
	return in
}

func implPlan(t *testing.T, coreManifestPath, outPath string) *CoreExecutionPlanReceipt {
	t.Helper()
	p, err := CreateCoreExecutionPlan(implPlanInput(t, coreManifestPath, outPath))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// implProtected assembles the runner-side protected execution receipt for the
// fixture plan, including the canonical probe matrix digest.
func implProtected(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	r := fxProtectedReceipt(plan)
	r.ExecutionTemplateSetDigest = plan.NormalizedCoreExecutionTemplateDigest
	r.IsolatedWorkerCapacity = plan.Concurrency + 1
	digest, err := ProbeMatrixDigestV1(r.WorkerProbes)
	if err != nil {
		panic(err)
	}
	r.ProbeMatrixDigest = digest
	return r
}

func implCanaries(plan *CoreExecutionPlanReceipt, seriesID, skillDigest string) map[string]map[int]*WorkspaceCanaryReceipt {
	out := map[string]map[int]*WorkspaceCanaryReceipt{}
	for _, h := range plan.Hosts {
		out[h] = map[int]*WorkspaceCanaryReceipt{}
		for slot := 1; slot <= plan.Concurrency; slot++ {
			c := fxCanary(seriesID, skillDigest, plan.ToolIdentityDigests[h],
				plan.NormalizedCoreExecutionTemplateDigest, h, slot)
			c.ReceiptDigest = fx64(fmt.Sprintf("canary-%s-%d", h, slot))
			out[h][slot] = c
		}
	}
	return out
}

type implSeriesFixture struct {
	root                string
	validatorPath       string
	coreManifestPath    string
	holdoutManifestPath string
	holdoutIDs          []string
	coreIDs             []string
	plan                *CoreExecutionPlanReceipt
	planPath            string
	snapshotRoot        string
	pvPath              string
	greenPath           string
	protected           *ProtectedExecutionReceipt
	canaries            map[string]map[int]*WorkspaceCanaryReceipt
}

func implSeries(t *testing.T) *implSeriesFixture {
	t.Helper()
	stubGreenRunner(t)
	fx := &implSeriesFixture{root: t.TempDir()}
	fx.validatorPath = fxValidatorFile(t)
	fx.coreManifestPath = implCoreManifest(t)
	fx.holdoutManifestPath, fx.holdoutIDs = implHoldoutManifest(t)
	_, core := syntheticCore172(t)
	fx.coreIDs = core.CaseIDs
	fx.planPath = filepath.Join(fx.root, "core-plan.json")
	fx.plan = implPlan(t, fx.coreManifestPath, fx.planPath)
	snapshotRoot, pv := implSnapshot(t, fx.validatorPath)
	fx.snapshotRoot = snapshotRoot
	fx.pvPath = filepath.Join(fx.root, "pv.json")
	writeFile(t, fx.root, "pv.json", mustCanonicalJSON(pv))
	green, err := CreateGreenTestReceipt(SuiteSeriesPrepare, fx.validatorPath,
		filepath.Join(fx.root, "green-series-prepare.json"), GreenBindings{
			SnapshotDigest: &pv.SnapshotDigest, PackageValidationReceiptDigest: &pv.ReceiptDigest,
		})
	if err != nil {
		t.Fatal(err)
	}
	if green.ReceiptDigest == "" || green.StableIdentityDigest == nil {
		t.Fatal("the fixture series-prepare receipt must be self-digested")
	}
	fx.greenPath = filepath.Join(fx.root, "green-series-prepare.json")
	fx.protected = implProtected(fx.plan)
	fx.canaries = implCanaries(fx.plan, "series-impl-1", pv.SkillDigest)
	return fx
}

func (fx *implSeriesFixture) prepareInput(t *testing.T, seriesID string, purpose SeriesPurpose) SeriesPrepareInput {
	t.Helper()
	in := SeriesPrepareInput{
		SeriesID: seriesID, Purpose: purpose,
		CorePlanPath: fx.planPath, CoreManifestPath: fx.coreManifestPath,
		HoldoutManifestPath: fx.holdoutManifestPath,
		SnapshotRoot:        fx.snapshotRoot, PackageValidationReceiptPath: fx.pvPath,
		GreenTestReceiptPath: fx.greenPath, ValidatorPath: fx.validatorPath,
		RunnerDigest: fx.plan.RunnerDigest, JudgeRuleDigest: fx.plan.JudgeRuleDigest,
		ToolIdentityDigests:        copyStringMap(fx.plan.ToolIdentityDigests),
		ToolConfigurationDigest:    fx64("tool-config"),
		ExecutionEnvironmentDigest: fx.plan.NormalizedCoreExecutionTemplateDigest,
		CaseOrderSeeds:             copySeedMap(fx.plan.CaseOrderSeeds),
		TimeoutSeconds:             fx.plan.TimeoutSeconds, Concurrency: fx.plan.Concurrency,
	}
	if purpose == PurposeOfficialDual {
		in.Protected = fx.protected
	}
	return in
}

func implSeriesManifestPath(root, seriesID string) string {
	return filepath.Join(root, "series", seriesID, "series-manifest.json")
}

// implRewrittenManifest re-seals a copy of a sealed manifest under a different
// lifecycle state (the state transitions themselves are T046-T050's surface).
func implRewrittenManifest(t *testing.T, m *FormalSeriesManifest, dir, name string, state LifecycleState) string {
	t.Helper()
	x := *m
	x.State = state
	x.ManifestDigest = ""
	digest, err := seriesManifestDigest(&x)
	if err != nil {
		t.Fatal(err)
	}
	x.ManifestDigest = digest
	implWriteJSON(t, dir, name, &x)
	return filepath.Join(dir, name)
}

func (fx *implSeriesFixture) coreLeg(t *testing.T, seriesID string) ([]string, string) {
	t.Helper()
	var paths []string
	var runs []*PrimaryRunManifest
	minute := 1
	for _, h := range fx.plan.Hosts {
		for _, o := range Ordinals {
			p := filepath.Join(fx.root, "primary", seriesID, fmt.Sprintf("%s-dev-regression-o%d.json", h, o))
			r, err := SealPrimaryRun(PrimaryRunInput{
				Root: fx.root, SeriesID: seriesID, Host: h, Split: SplitDevRegression, Ordinal: o,
				Plan: fx.plan,
				ToolProvenance: ToolProvenance{Host: h, ToolIdentityDigest: fx.plan.ToolIdentityDigests[h],
					CapturedAt: implTS(minute), SourceRevision: fx.plan.RunnerDigest},
				CaseIDs:   fx.coreIDs,
				CaseOrder: implRunOrder(fx.coreIDs, o*7+len(h)),
				StartedAt: implTS(minute), CompletedAt: implTS(minute + 30),
				OutPath: p,
			})
			if err != nil {
				t.Fatal(err)
			}
			paths = append(paths, p)
			runs = append(runs, r)
			minute++
		}
	}
	digest, err := CoreLegCompletionDigest(runs)
	if err != nil {
		t.Fatal(err)
	}
	return paths, digest
}

func (fx *implSeriesFixture) preHoldout(t *testing.T, m *FormalSeriesManifest, coreLeg, outPath string) *GreenTestReceipt {
	t.Helper()
	r, err := CreateGreenTestReceipt(SuitePreHoldout, fx.validatorPath, outPath, GreenBindings{
		SnapshotDigest:                 &m.SkillSnapshotDigest,
		PackageValidationReceiptDigest: &m.SkillPackageValidationReceiptDigest,
		SeriesManifestDigest:           &m.ManifestDigest,
		CandidateBindingDigest:         &m.CandidateBindingDigest,
		CoreLegCompletionDigest:        &coreLeg,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestImplCorePlanCreateLoadRoundTrip(t *testing.T) {
	stubGreenRunner(t)
	coreManifest := implCoreManifest(t)
	planPath := filepath.Join(t.TempDir(), "core-plan.json")
	plan := implPlan(t, coreManifest, planPath)
	if err := ValidateCoreExecutionPlan(plan); err != nil {
		t.Fatalf("created plan must validate: %v", err)
	}
	if err := VerifyCoreExecutionPlan(plan); err != nil {
		t.Fatalf("created plan must self-verify: %v", err)
	}
	if plan.CoreManifestDigest == "" {
		t.Fatal("plan must bind the core manifest digest read from disk")
	}
	loaded, err := LoadCoreExecutionPlan(planPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(loaded, plan) {
		t.Fatal("load/create round trip drifted")
	}
	// A frozen plan is never rewritten.
	if _, err := CreateCoreExecutionPlan(implPlanInput(t, coreManifest, planPath)); err == nil {
		t.Fatal("re-creating a plan over an existing file must be refused")
	}
	// The sealed plan binds exactly the 172-case core172 manifest.
	if _, m, err := DatasetManifestDigest(coreManifest); err != nil || m.CaseCount != 172 {
		t.Fatalf("core manifest fixture: %v", err)
	}
}

func TestImplCorePlanSealTamperRejected(t *testing.T) {
	stubGreenRunner(t)
	dir := t.TempDir()
	planPath := filepath.Join(dir, "core-plan.json")
	plan := implPlan(t, implCoreManifest(t), planPath)

	tamper := func(name string, m func(p *CoreExecutionPlanReceipt)) {
		t.Helper()
		x := *plan
		m(&x)
		p := filepath.Join(dir, name+".json")
		implWriteJSON(t, dir, name+".json", &x)
		if _, err := LoadCoreExecutionPlan(p); err == nil {
			t.Fatalf("load must reject %s", name)
		}
	}
	tamper("drifted-condition", func(p *CoreExecutionPlanReceipt) { p.TimeoutSeconds = 599 })
	tamper("drifted-seed", func(p *CoreExecutionPlanReceipt) { p.CaseOrderSeeds[3] = fx64("regenerated") })
	tamper("drifted-tool", func(p *CoreExecutionPlanReceipt) { p.ToolIdentityDigests[HostCodex] = fx64("new-tool") })
	tamper("drifted-receipt-digest", func(p *CoreExecutionPlanReceipt) { p.ReceiptDigest = fx64("forged") })
	tamper("dropped-seal", func(p *CoreExecutionPlanReceipt) { p.SealDigest = "" })
	// The untouched plan still loads.
	if _, err := LoadCoreExecutionPlan(planPath); err != nil {
		t.Fatalf("untouched plan must load: %v", err)
	}
}

func TestImplPrepareSeriesSealedOfficialDual(t *testing.T) {
	fx := implSeries(t)
	in := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	in.Canaries = fx.canaries
	in.StagedWorkspaceFiles = true
	m, err := PrepareSeries(fx.root, in)
	if err != nil {
		t.Fatalf("series prepare: %v", err)
	}
	if m.State != StateSealed {
		t.Fatalf("prepared series state %q, want sealed", m.State)
	}
	if m.QuestionCount[MembershipCore172] != 172 || m.QuestionCount[MembershipHoldout96] != 96 {
		t.Fatalf("question_count %v, want core172=172 holdout96=96", m.QuestionCount)
	}
	if m.CandidateBindingDigest == "" || m.ProtectedExecutionReceiptDigest != fx.protected.ReceiptDigest {
		t.Fatal("official-dual manifest must bind the candidate digest and the protected receipt")
	}
	if len(m.WorkspaceCanaryReceiptDigests) != len(fx.plan.Hosts) {
		t.Fatal("canary coverage must be bound in the manifest")
	}
	if err := ValidateFormalSeriesManifest(m); err != nil {
		t.Fatalf("prepared manifest must validate: %v", err)
	}
	loaded, err := LoadSeriesManifest(implSeriesManifestPath(fx.root, "series-impl-1"))
	if err != nil {
		t.Fatalf("load prepared manifest: %v", err)
	}
	if !reflect.DeepEqual(loaded, m) {
		t.Fatal("series manifest round trip drifted")
	}
	// The manifest file is frozen: preparing the same series again is refused.
	if _, err := PrepareSeries(fx.root, in); err == nil {
		t.Fatal("re-preparing over an existing series manifest must be refused")
	}
}

func TestImplPrepareSeriesCandidateDigestStableAcrossSeries(t *testing.T) {
	fx := implSeries(t)
	first := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	first.Canaries, first.StagedWorkspaceFiles = fx.canaries, true
	first.OutPath = filepath.Join(fx.root, "m1.json")
	a, err := PrepareSeries(fx.root, first)
	if err != nil {
		t.Fatal(err)
	}
	// A recovery series has a new id and a fresh protected receipt (new roots),
	// yet must recompute the same stable candidate digest.
	second := fx.prepareInput(t, "series-impl-2-recovery", PurposeOfficialDual)
	second.Canaries = implCanaries(fx.plan, "series-impl-2-recovery", a.SkillDigest)
	second.StagedWorkspaceFiles = true
	second.Protected = implProtected(fx.plan)
	second.OutPath = filepath.Join(fx.root, "m2.json")
	b, err := PrepareSeries(fx.root, second)
	if err != nil {
		t.Fatal(err)
	}
	if a.SeriesID == b.SeriesID || a.ManifestDigest == b.ManifestDigest {
		t.Fatal("recovery series must carry a new identity")
	}
	if a.CandidateBindingDigest != b.CandidateBindingDigest {
		t.Fatalf("stable candidate digest drifted across series: %s != %s", a.CandidateBindingDigest, b.CandidateBindingDigest)
	}
	if a.SeriesPrepareIdentityDigest != b.SeriesPrepareIdentityDigest {
		t.Fatal("series-prepare identity must be stable across recovery")
	}
}

func TestImplPrepareSeriesTimeoutConcurrencyMismatch(t *testing.T) {
	fx := implSeries(t)
	base := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	base.Canaries, base.StagedWorkspaceFiles = fx.canaries, true
	base.OutPath = filepath.Join(fx.root, "m.json")

	drift := base
	drift.TimeoutSeconds = base.TimeoutSeconds + 1
	if _, err := PrepareSeries(fx.root, drift); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("timeout drift must be rejected: %v", err)
	}
	drift = base
	drift.TimeoutSeconds = 0
	if _, err := PrepareSeries(fx.root, drift); err == nil {
		t.Fatal("unset timeout must be rejected")
	}
	drift = base
	drift.Concurrency = base.Concurrency + 1
	if _, err := PrepareSeries(fx.root, drift); err == nil || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("concurrency drift must be rejected: %v", err)
	}
	drift = base
	drift.Concurrency = 0
	if _, err := PrepareSeries(fx.root, drift); err == nil {
		t.Fatal("unset concurrency must be rejected")
	}
}

func TestImplPrepareSeriesSeedRegenerationRejected(t *testing.T) {
	fx := implSeries(t)
	base := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	base.Canaries, base.StagedWorkspaceFiles = fx.canaries, true
	base.OutPath = filepath.Join(fx.root, "m.json")

	regenerated := base
	regenerated.CaseOrderSeeds = map[int]string{1: fx64("fresh-seed-1"), 2: fx64("fresh-seed-2"), 3: fx64("fresh-seed-3")}
	if _, err := PrepareSeries(fx.root, regenerated); err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("regenerated seeds must be rejected: %v", err)
	}
	partial := base
	partial.CaseOrderSeeds = map[int]string{1: fx.plan.CaseOrderSeeds[1]}
	if _, err := PrepareSeries(fx.root, partial); err == nil {
		t.Fatal("a partial seed copy must be rejected")
	}
	nil_ := base
	nil_.CaseOrderSeeds = nil
	if _, err := PrepareSeries(fx.root, nil_); err == nil {
		t.Fatal("a nil seed copy must be rejected")
	}
	// One drifted ordinal out of three is still a regeneration.
	drifted := base
	drifted.CaseOrderSeeds = copySeedMap(fx.plan.CaseOrderSeeds)
	drifted.CaseOrderSeeds[2] = fx64("regenerated-2")
	if _, err := PrepareSeries(fx.root, drifted); err == nil {
		t.Fatal("a single drifted seed must be rejected")
	}
}

func TestImplPrepareSeriesPackageReceiptGates(t *testing.T) {
	fx := implSeries(t)
	base := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	base.Canaries, base.StagedWorkspaceFiles = fx.canaries, true
	base.OutPath = filepath.Join(fx.root, "m.json")

	missing := base
	missing.PackageValidationReceiptPath = filepath.Join(fx.root, "absent.json")
	if _, err := PrepareSeries(fx.root, missing); err == nil {
		t.Fatal("a missing package validation receipt must be rejected")
	}

	// A failed receipt is not a passing receipt.
	failed := base
	p := filepath.Join(fx.root, "pv-failed.json")
	b, err := os.ReadFile(fx.pvPath)
	if err != nil {
		t.Fatal(err)
	}
	var bad SkillPackageValidationReceipt
	if err := StrictParseClosed(b, &bad); err != nil {
		t.Fatal(err)
	}
	bad.Passed = false
	digest, err := receiptDigestPV(&bad)
	if err != nil {
		t.Fatal(err)
	}
	bad.ReceiptDigest = digest
	writeFile(t, fx.root, "pv-failed.json", mustCanonicalJSON(&bad))
	failed.PackageValidationReceiptPath = p
	if _, err := PrepareSeries(fx.root, failed); err == nil {
		t.Fatal("a failed package validation receipt must be rejected")
	}

	// A receipt bound to another snapshot (different evaluated skill) is a
	// wrong-skill receipt for this series.
	otherRoot, otherPV := implSnapshot(t, fx.validatorPath)
	// A different evaluated revision: any byte change is another skill.
	writeFile(t, otherRoot, "SKILL.md", []byte("---\nname: engram\nversion: 9.9.9-other\n---\n\ndifferent body\n"))
	wrongSkill := base
	wrongSkill.SnapshotRoot = otherRoot
	wrongSkill.PackageValidationReceiptPath = filepath.Join(fx.root, "pv-other.json")
	writeFile(t, fx.root, "pv-other.json", mustCanonicalJSON(otherPV))
	if _, err := PrepareSeries(fx.root, wrongSkill); err == nil {
		t.Fatal("a wrong-skill package receipt must be rejected")
	}

	// A wrong-suite green receipt is not a series-prepare receipt.
	wrongSuite := base
	other, err := CreateGreenTestReceipt(SuiteFormalTooling, fx.validatorPath, "", GreenBindings{})
	if err != nil {
		t.Fatal(err)
	}
	wrongSuite.GreenTestReceiptPath = filepath.Join(fx.root, "green-wrong.json")
	writeFile(t, fx.root, "green-wrong.json", mustCanonicalJSON(other))
	if _, err := PrepareSeries(fx.root, wrongSuite); err == nil {
		t.Fatal("a wrong-suite green receipt must be rejected")
	}
}

func TestImplPrepareSeriesCanaryCoverage(t *testing.T) {
	fx := implSeries(t)
	base := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	base.OutPath = filepath.Join(fx.root, "m.json")
	base.Canaries = fx.canaries
	base.StagedWorkspaceFiles = true

	missing := base
	missing.Canaries = implCanaries(fx.plan, "series-impl-1", baseGreenSkillDigest(t, fx))
	delete(missing.Canaries[HostCodex], 2)
	if _, err := PrepareSeries(fx.root, missing); err == nil || !strings.Contains(err.Error(), "canary") {
		t.Fatalf("a missing host × slot canary must be rejected: %v", err)
	}

	failed := base
	broken := implCanaries(fx.plan, "series-impl-1", baseGreenSkillDigest(t, fx))
	broken[HostClaude][1].Status = "fail"
	failed.Canaries = broken
	if _, err := PrepareSeries(fx.root, failed); err == nil {
		t.Fatal("a failed canary must be rejected")
	}

	extra := base
	spacious := implCanaries(fx.plan, "series-impl-1", baseGreenSkillDigest(t, fx))
	spacious[HostClaude][fx.plan.Concurrency+1] = fxCanary("series-impl-1", baseGreenSkillDigest(t, fx),
		fx.plan.ToolIdentityDigests[HostClaude], fx.plan.NormalizedCoreExecutionTemplateDigest, HostClaude, fx.plan.Concurrency+1)
	extra.Canaries = spacious
	if _, err := PrepareSeries(fx.root, extra); err == nil {
		t.Fatal("a canary outside the prepared slot set must be rejected")
	}

	// No staged files → the canary map must be empty.
	none := base
	none.StagedWorkspaceFiles = false
	if _, err := PrepareSeries(fx.root, none); err == nil {
		t.Fatal("canaries without staged workspace files must be rejected")
	}
}

// baseGreenSkillDigest reads the skill digest the fixture's green receipt and
// package receipt bind (the series skill identity).
func baseGreenSkillDigest(t *testing.T, fx *implSeriesFixture) string {
	t.Helper()
	pv, err := LoadPackageValidationReceipt(fx.pvPath)
	if err != nil {
		t.Fatal(err)
	}
	return pv.SkillDigest
}

func TestImplPrepareSeriesProtectedFailClosed(t *testing.T) {
	fx := implSeries(t)
	official := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	official.Canaries, official.StagedWorkspaceFiles = fx.canaries, true
	official.OutPath = filepath.Join(fx.root, "m.json")
	official.Protected = nil
	if _, err := PrepareSeries(fx.root, official); err == nil {
		t.Fatal("official-dual without a protected execution receipt must fail closed")
	}

	dev := fx.prepareInput(t, "series-impl-dev", PurposeDevComparison)
	dev.OutPath = filepath.Join(fx.root, "m-dev.json")
	dev.Protected = implProtected(fx.plan)
	if _, err := PrepareSeries(fx.root, dev); err == nil {
		t.Fatal("dev-comparison must not carry a protected execution receipt")
	}
	dev.Protected = nil
	m, err := PrepareSeries(fx.root, dev)
	if err != nil {
		t.Fatalf("dev-comparison series prepare: %v", err)
	}
	if m.CandidateBindingDigest != "" || m.ProtectedExecutionPolicyDigest != "" || m.ProtectedExecutionReceiptDigest != "" {
		t.Fatal("dev-comparison must leave the protected identity fields null")
	}
	if m.DatasetManifests[MembershipHoldout96] != "" || m.QuestionCount[MembershipHoldout96] != 0 {
		t.Fatal("dev-comparison must bind only core172")
	}
}

func TestImplSealPrimaryRunContract(t *testing.T) {
	fx := implSeries(t)
	ids := fx.coreIDs
	base := PrimaryRunInput{
		Root: fx.root, SeriesID: "series-impl-1", Host: HostClaude, Split: SplitDevRegression, Ordinal: 1,
		Plan: fx.plan,
		ToolProvenance: ToolProvenance{Host: HostClaude, ToolIdentityDigest: fx.plan.ToolIdentityDigests[HostClaude],
			CapturedAt: implTS(1), SourceRevision: fx.plan.RunnerDigest},
		CaseIDs: ids, CaseOrder: implRunOrder(ids, 3),
		StartedAt: implTS(1), CompletedAt: implTS(40),
		OutPath: filepath.Join(fx.root, "primary", "series-impl-1", "claude-dev-regression-o1.json"),
	}
	r, err := SealPrimaryRun(base)
	if err != nil {
		t.Fatalf("seal primary run: %v", err)
	}
	if r.ExpectedCaseCount != 172 || len(r.CaseIDs) != 172 || r.Mode != "primary" {
		t.Fatal("sealed run must carry the exact core172 case count")
	}
	if r.CaseSetDigest == r.CaseOrderDigest {
		t.Fatal("case set digest and ordered digest are different receipts")
	}
	loaded, err := LoadPrimaryRun(base.OutPath)
	if err != nil {
		t.Fatalf("load primary run: %v", err)
	}
	if !reflect.DeepEqual(loaded, r) {
		t.Fatal("primary run round trip drifted")
	}

	short := base
	short.CaseIDs, short.CaseOrder = ids[:171], implRunOrder(ids[:171], 1)
	if _, err := SealPrimaryRun(short); err == nil || !strings.Contains(err.Error(), "172") {
		t.Fatalf("a 171-case core run must be rejected: %v", err)
	}
	wrongSplit := base
	wrongSplit.Split = SplitHoldout
	if _, err := SealPrimaryRun(wrongSplit); err == nil || !strings.Contains(err.Error(), "96") {
		t.Fatalf("a holdout run must carry 96 cases: %v", err)
	}
	driftedTool := base
	driftedTool.ToolProvenance.ToolIdentityDigest = fx64("new-tool")
	if _, err := SealPrimaryRun(driftedTool); err == nil || !strings.Contains(err.Error(), "tool identity") {
		t.Fatalf("a drifted tool identity must be rejected: %v", err)
	}
	foreignHost := base
	foreignHost.ToolProvenance.Host = HostCodex
	if _, err := SealPrimaryRun(foreignHost); err == nil {
		t.Fatal("provenance host must match the run host")
	}
	badOrder := base
	badOrder.CaseOrder = implRunOrder(fx.holdoutIDs, 1)
	if _, err := SealPrimaryRun(badOrder); err == nil {
		t.Fatal("case_order must be a permutation of case_ids")
	}
}

func TestImplHoldoutBindingLifecycle(t *testing.T) {
	fx := implSeries(t)
	in := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	in.Canaries, in.StagedWorkspaceFiles = fx.canaries, true
	manifest, err := PrepareSeries(fx.root, in)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := implSeriesManifestPath(fx.root, "series-impl-1")
	runPaths, coreLeg := fx.coreLeg(t, "series-impl-1")
	preHoldoutPath := filepath.Join(fx.root, "green-pre-holdout.json")
	fx.preHoldout(t, manifest, coreLeg, preHoldoutPath)
	// A receipt bound to a different core leg is not this series'.
	stalePath := filepath.Join(fx.root, "green-pre-holdout-stale.json")
	fx.preHoldout(t, manifest, fx64("other-core-leg"), stalePath)

	binding, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      manifestPath,
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
	})
	if err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if binding.State != "frozen" || len(binding.SeriesAttempts) != 1 || binding.SeriesAttempts[0].State != "started" {
		t.Fatalf("first binding must be frozen with one started attempt: %+v", binding)
	}
	if binding.DatasetManifestDigest == "" || binding.DatasetVersion != "holdout96-fx-v1" {
		t.Fatalf("binding must record the holdout dataset identity: %+v", binding)
	}
	loaded, err := LoadHoldoutBinding(filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json"))
	if err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if !reflect.DeepEqual(loaded, binding) {
		t.Fatal("binding round trip drifted")
	}
	// First binding only.
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      manifestPath,
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
	}); err == nil {
		t.Fatal("a second first-binding must be refused")
	}
	// A wrong core-leg digest is refused.
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      manifestPath,
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: fx64("other-core-leg"),
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
		OutPath:                 filepath.Join(fx.root, "binding-b.json"),
	}); err == nil {
		t.Fatal("a wrong core-leg digest must be refused")
	}
	// An incomplete core leg (one ordinal missing) is refused.
	incomplete := append([]string{}, runPaths[:len(runPaths)-1]...)
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      manifestPath,
		CoreLegRunPaths:         incomplete,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
		OutPath:                 filepath.Join(fx.root, "binding-c.json"),
	}); err == nil {
		t.Fatal("an incomplete core leg must be refused")
	}
	// A stale pre-holdout receipt (bound to another core leg) is refused.
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      manifestPath,
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   stalePath,
		ValidatorPath:           fx.validatorPath,
		OutPath:                 filepath.Join(fx.root, "binding-d.json"),
	}); err == nil {
		t.Fatal("a pre-holdout receipt bound to another core leg must be refused")
	}

	// Consuming before the holdout series is complete is refused.
	if _, err := ConsumeHoldoutBinding(fx.root, filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json"),
		manifestPath, "complete-pass"); err == nil || !strings.Contains(err.Error(), "complete") {
		t.Fatalf("an incomplete series must not consume: %v", err)
	}

	// Complete the series, then consume.
	completePath := implRewrittenManifest(t, manifest, filepath.Dir(manifestPath), "series-manifest-complete.json", StateComplete)
	consumed, err := ConsumeHoldoutBinding(fx.root, filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json"),
		completePath, "complete-pass")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.State != "consumed" || consumed.ConsumedBySeries != "series-impl-1" {
		t.Fatalf("consumed binding mismatch: %+v", consumed)
	}
	if consumed.SeriesAttempts[0].State != "complete-pass" || consumed.PreviousReceiptDigest != binding.ReceiptDigest {
		t.Fatal("consumption must settle the attempt and chain to the previous digest")
	}
	if _, err := ConsumeHoldoutBinding(fx.root, filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json"),
		completePath, "complete-pass"); err == nil {
		t.Fatal("a consumed binding cannot be consumed twice")
	}
	// A non-complete outcome is not a consumption.
	if _, err := ConsumeHoldoutBinding(fx.root, filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json"),
		completePath, "invalid"); err == nil {
		t.Fatal("invalid is not a complete-series outcome")
	}
}

func TestImplHoldoutBindingCoreInvalidBeforeHoldout(t *testing.T) {
	fx := implSeries(t)
	in := fx.prepareInput(t, "series-impl-1", PurposeOfficialDual)
	in.Canaries, in.StagedWorkspaceFiles = fx.canaries, true
	manifest, err := PrepareSeries(fx.root, in)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := implSeriesManifestPath(fx.root, "series-impl-1")
	runPaths, coreLeg := fx.coreLeg(t, "series-impl-1")
	preHoldoutPath := filepath.Join(fx.root, "green-pre-holdout.json")
	fx.preHoldout(t, manifest, coreLeg, preHoldoutPath)
	// A pre-holdout receipt bound to a different core leg is not this series'.
	stalePath := filepath.Join(fx.root, "green-pre-holdout-stale.json")
	fx.preHoldout(t, manifest, fx64("other-core-leg"), stalePath)

	invalidPath := implRewrittenManifest(t, manifest, filepath.Dir(manifestPath), "series-manifest-invalid.json", StateInvalid)
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      invalidPath,
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
		OutPath:                 filepath.Join(fx.root, "binding-invalid.json"),
	}); err == nil || !strings.Contains(err.Error(), "core-invalid-before-holdout") {
		t.Fatalf("an invalid core leg must not create a binding: %v", err)
	}
	// The dev-comparison purpose never binds the holdout either.
	dev := fx.prepareInput(t, "series-impl-dev", PurposeDevComparison)
	dev.OutPath = filepath.Join(fx.root, "m-dev.json")
	if _, err := PrepareSeries(fx.root, dev); err != nil {
		t.Fatal(err)
	}
	if _, err := BindHoldout(fx.root, HoldoutBindInput{
		DatasetManifestPath:     fx.holdoutManifestPath,
		SeriesManifestPath:      implSeriesManifestPath(fx.root, "series-impl-dev"),
		CoreLegRunPaths:         runPaths,
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   preHoldoutPath,
		ValidatorPath:           fx.validatorPath,
		OutPath:                 filepath.Join(fx.root, "binding-dev.json"),
	}); err == nil {
		t.Fatal("a dev-comparison series must never bind the holdout")
	}
}

func TestImplAppendHoldoutAttemptRecovery(t *testing.T) {
	fx := implSeries(t)
	prepare := func(seriesID string, mutate func(*SeriesPrepareInput)) (*FormalSeriesManifest, string, []string, string, string) {
		t.Helper()
		in := fx.prepareInput(t, seriesID, PurposeOfficialDual)
		in.Canaries = implCanaries(fx.plan, seriesID, baseGreenSkillDigest(t, fx))
		in.StagedWorkspaceFiles = true
		in.OutPath = implSeriesManifestPath(fx.root, seriesID)
		if mutate != nil {
			mutate(&in)
		}
		m, err := PrepareSeries(fx.root, in)
		if err != nil {
			t.Fatal(err)
		}
		runPaths, coreLeg := fx.coreLeg(t, seriesID)
		ph := filepath.Join(fx.root, "green-pre-holdout-"+seriesID+".json")
		fx.preHoldout(t, m, coreLeg, ph)
		return m, implSeriesManifestPath(fx.root, seriesID), runPaths, coreLeg, ph
	}
	bindInput := func(manifestPath string, runPaths []string, coreLeg, preHoldoutPath string) HoldoutBindInput {
		return HoldoutBindInput{
			DatasetManifestPath: fx.holdoutManifestPath, SeriesManifestPath: manifestPath,
			CoreLegRunPaths: runPaths, CoreLegCompletionDigest: coreLeg,
			PreHoldoutReceiptPath: preHoldoutPath, ValidatorPath: fx.validatorPath,
		}
	}
	var bindingPath string
	appendInput := func(manifestPath string, runPaths []string, coreLeg, preHoldoutPath string) HoldoutAppendInput {
		return HoldoutAppendInput{
			BindingPath: bindingPath, SeriesManifestPath: manifestPath,
			CoreLegRunPaths: runPaths, CoreLegCompletionDigest: coreLeg,
			PreHoldoutReceiptPath: preHoldoutPath, ValidatorPath: fx.validatorPath,
		}
	}

	m1, path1, runs1, leg1, ph1 := prepare("series-impl-1", nil)
	if m1.CandidateBindingDigest == "" {
		t.Fatal("fixture series must carry a stable candidate digest")
	}
	bindingPath = filepath.Join(fx.root, "series", "series-impl-1", "holdout-binding.json")
	first, err := BindHoldout(fx.root, bindInput(path1, runs1, leg1, ph1))
	if err != nil {
		t.Fatal(err)
	}

	// A series whose stable inputs drifted (here: the tool configuration)
	// recomputes a DIFFERENT candidate digest: that is a new holdout version,
	// never an append to this binding.
	_, pathX, runsX, legX, phX := prepare("series-impl-drift", func(in *SeriesPrepareInput) {
		in.ToolConfigurationDigest = fx64("drifted-tool-config")
	})
	if _, err := AppendHoldoutAttempt(fx.root, appendInput(pathX, runsX, legX, phX)); err == nil ||
		!strings.Contains(err.Error(), "new holdout version") {
		t.Fatalf("a different candidate digest must be refused: %v", err)
	}

	// Same stable inputs, new series id: the recovery append is legal.
	m2, path2, runs2, leg2, ph2 := prepare("series-impl-2", nil)
	if m2.CandidateBindingDigest != first.CandidateBindingDigest {
		t.Fatalf("recovery series must recompute the same stable digest: %s != %s", m2.CandidateBindingDigest, first.CandidateBindingDigest)
	}
	// ... but reusing the previous series' pre-holdout receipt is not.
	if _, err := AppendHoldoutAttempt(fx.root, appendInput(path2, runs2, leg2, ph1)); err == nil {
		t.Fatal("reusing the previous pre-holdout receipt must be refused")
	}
	// ... and neither is reusing the previous core-leg completion.
	if _, err := AppendHoldoutAttempt(fx.root, appendInput(path2, runs1, leg1, ph2)); err == nil {
		t.Fatal("reusing the previous core-leg completion must be refused")
	}
	second, err := AppendHoldoutAttempt(fx.root, appendInput(path2, runs2, leg2, ph2))
	if err != nil {
		t.Fatalf("recovery append: %v", err)
	}
	if len(second.SeriesAttempts) != 2 || second.PreviousReceiptDigest != first.ReceiptDigest {
		t.Fatal("the append must chain to the previous receipt digest")
	}
	if second.SeriesAttempts[1].SeriesManifestDigest == second.SeriesAttempts[0].SeriesManifestDigest {
		t.Fatal("recovery must carry a new series manifest digest")
	}
	if second.SeriesAttempts[1].CandidateBindingDigestV() != first.CandidateBindingDigest {
		t.Fatal("the appended attempt must carry the binding's stable candidate digest")
	}
	if _, err := LoadHoldoutBinding(bindingPath); err != nil {
		t.Fatalf("reloaded binding: %v", err)
	}
	// The superseded version stays on disk.
	if _, err := os.Stat(strings.TrimSuffix(bindingPath, ".json") + ".v1.json"); err != nil {
		t.Fatalf("the superseded binding version must be preserved: %v", err)
	}

	// Consume by the recovery series, then no further append is possible.
	completePath := implRewrittenManifest(t, m2, filepath.Dir(path2), "series-manifest-complete.json", StateComplete)
	if _, err := ConsumeHoldoutBinding(fx.root, bindingPath, completePath, "complete-fail"); err != nil {
		t.Fatalf("consume by recovery series: %v", err)
	}
	if _, err := AppendHoldoutAttempt(fx.root, appendInput(path2, runs2, leg2, ph2)); err == nil {
		t.Fatal("no attempt may be appended to a consumed binding")
	}
}

func TestImplCoreLegCompletionDigestMatrix(t *testing.T) {
	fx := implSeries(t)
	runPaths, digest := fx.coreLeg(t, "series-impl-1")
	var runs []*PrimaryRunManifest
	for _, p := range runPaths {
		r, err := LoadPrimaryRun(p)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	again, err := CoreLegCompletionDigest(runs)
	if err != nil || again != digest {
		t.Fatalf("core-leg digest must be reproducible: %v", err)
	}
	// A different core leg (fresh runs) yields a different digest.
	_, other := fx.coreLeg(t, "series-impl-2")
	if other == digest {
		t.Fatal("a fresh core leg must produce a different receipt-set digest")
	}
	// Reordering the input does not change the digest.
	reversed := append([]*PrimaryRunManifest{}, runs[8], runs[7], runs[6], runs[5], runs[4], runs[3], runs[2], runs[1], runs[0])
	reordered, err := CoreLegCompletionDigest(reversed)
	if err != nil || reordered != digest {
		t.Fatalf("core-leg digest must be order-independent: %v", err)
	}
	// Missing an ordinal is not a complete leg.
	if _, err := CoreLegCompletionDigest(runs[:8]); err == nil {
		t.Fatal("an incomplete core leg must be refused")
	}
	// A duplicated host × ordinal is refused.
	if _, err := CoreLegCompletionDigest(append(append([]*PrimaryRunManifest{}, runs...), runs[0])); err == nil {
		t.Fatal("a duplicated run must be refused")
	}
	// A run of another series is refused.
	foreign := append([]*PrimaryRunManifest{}, runs...)
	foreign[0].SeriesID = "series-impl-other"
	if _, err := CoreLegCompletionDigest(foreign); err == nil {
		t.Fatal("a cross-series core leg must be refused")
	}
}
