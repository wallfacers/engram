package main

// T044 score/report tests (US4). Every fixture here is fictional: no skill,
// dataset, host CLI or receipt file is read. The fixtures are all prefixed
// `rpt` so they cannot collide with the other US4 test files' helpers.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func rptHosts() []string {
	return []string{HostClaude, HostCodex, HostOpenCode}
}

// rptPlan is a sealed core execution plan with two worker slots per host.
func rptPlan() *CoreExecutionPlanReceipt {
	return &CoreExecutionPlanReceipt{
		SchemaVersion:      1,
		PlanID:             "rpt-plan",
		CoreManifestDigest: "rpt-core-manifest",
		RunnerRevision:     "rpt-runner-rev",
		RunnerDigest:       "rpt-runner-digest",
		JudgeRuleDigest:    "rpt-judge-digest",
		Hosts:              rptHosts(),
		ToolIdentityDigests: map[string]string{
			HostClaude: "rpt-tool-claude", HostCodex: "rpt-tool-codex", HostOpenCode: "rpt-tool-opencode",
		},
		TimeoutSeconds:                        240,
		Concurrency:                           2,
		CaseOrderSeeds:                        map[int]string{1: "rpt-seed-1", 2: "rpt-seed-2", 3: "rpt-seed-3"},
		CoreBoundaryKind:                      BoundaryContainer,
		NormalizedCoreWorkerIdentitySetDigest: "rpt-norm-worker",
		NormalizedCoreBoundaryTemplateDigest:  "rpt-norm-boundary",
		NormalizedCoreExecutionTemplateDigest: "rpt-norm-exec",
		ReceiptDigest:                         "rpt-plan-digest",
		SealDigest:                            "rpt-plan-seal",
	}
}

func rptManifest(plan *CoreExecutionPlanReceipt) *FormalSeriesManifest {
	return &FormalSeriesManifest{
		SeriesID: "rpt-series", Purpose: PurposeOfficialDual, State: StateComplete,
		SkillSnapshotDigest: "rpt-snapshot", SkillSnapshotAnchorDigest: "rpt-snapshot-anchor",
		SkillVersion: "0.2.7", SkillDigest: "rpt-skill",
		SkillPackageValidationReceiptDigest: "rpt-pv-digest",
		GreenTestReceiptDigest:              "rpt-green-prepare-digest",
		SeriesPrepareIdentityDigest:         "rpt-stable-identity",
		RunnerRevision:                      plan.RunnerRevision, RunnerDigest: plan.RunnerDigest, JudgeRuleDigest: plan.JudgeRuleDigest,
		CoreExecutionPlanDigest: plan.ReceiptDigest,
		DatasetManifests: map[string]string{
			MembershipCore172: "rpt-core-manifest", MembershipHoldout96: "rpt-holdout-manifest",
		},
		Hosts:                           append([]string(nil), plan.Hosts...),
		RequiredOrdinals:                []int{1, 2, 3},
		TimeoutSeconds:                  plan.TimeoutSeconds,
		Concurrency:                     plan.Concurrency,
		ExecutionEnvironmentDigest:      "rpt-env",
		ToolConfigurationDigest:         "rpt-toolconfig",
		ProtectedExecutionPolicyDigest:  "rpt-policy",
		CaseOrderSeeds:                  plan.CaseOrderSeeds,
		QuestionCount:                   map[string]int{MembershipCore172: 172, MembershipHoldout96: 96},
		CandidateBindingDigest:          "rpt-candidate-binding",
		ProtectedExecutionReceiptDigest: "rpt-protected-digest",
		WorkspaceCanaryReceiptDigests:   map[string]map[int]string{},
		ManifestDigest:                  "rpt-series-manifest",
	}
}

func rptCanaries(m *FormalSeriesManifest, plan *CoreExecutionPlanReceipt) map[string]map[int]*WorkspaceCanaryReceipt {
	out := map[string]map[int]*WorkspaceCanaryReceipt{}
	for _, h := range plan.Hosts {
		out[h] = map[int]*WorkspaceCanaryReceipt{}
		m.WorkspaceCanaryReceiptDigests[h] = map[int]string{}
		for slot := 1; slot <= plan.Concurrency; slot++ {
			c := &WorkspaceCanaryReceipt{
				SeriesID: m.SeriesID, Host: h, SkillDigest: m.SkillDigest,
				ToolIdentityDigest:      plan.ToolIdentityDigests[h],
				ExecutionTemplateDigest: plan.NormalizedCoreExecutionTemplateDigest,
				WorkerSlot:              slot,
				ChildIdentityDigest:     fmt.Sprintf("rpt-child-%s-%d", h, slot),
				AccessBoundaryDigest:    fmt.Sprintf("rpt-boundary-%s-%d", h, slot),
				CanaryWorkspaceDigest:   fmt.Sprintf("rpt-cwd-%s-%d", h, slot),
				ExpectedFileDigest:      fmt.Sprintf("rpt-file-%s-%d", h, slot),
				Status:                  "pass",
				ReceiptDigest:           fmt.Sprintf("rpt-canary-%s-%d", h, slot),
			}
			c.ObservedCWDDigest = c.CanaryWorkspaceDigest
			c.ObservedFileDigest = c.ExpectedFileDigest
			out[h][slot] = c
			m.WorkspaceCanaryReceiptDigests[h][slot] = c.ReceiptDigest
		}
	}
	return out
}

func rptProtected(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	r := &ProtectedExecutionReceipt{
		BoundaryKind:                          BoundaryContainer,
		IsolationConfigDigest:                 "rpt-isolation",
		ProtectedRootDigest:                   "rpt-protected-root",
		AuthorReviewStateRootsDigest:          "rpt-author-roots",
		FormalStateRootsDigest:                "rpt-formal-roots",
		SplitStateAllocatorDigests:            map[string]string{MembershipCore172: "rpt-alloc-core", MembershipHoldout96: "rpt-alloc-holdout"},
		RequiredConcurrency:                   plan.Concurrency,
		IsolatedWorkerCapacity:                plan.Concurrency,
		WorkerIdentitySetDigest:               "rpt-worker-set",
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            plan.NormalizedCoreExecutionTemplateDigest,
		CoreExecutionPlanDigest:               plan.ReceiptDigest,
		ProbeMatrixDigest:                     "rpt-probe-matrix",
		ProbedAt:                              "2026-09-03T00:00:00Z",
		ReceiptDigest:                         "rpt-protected-digest",
	}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			p := ProtectedWorkerProbe{
				Host: h, WorkerSlot: slot,
				ChildIdentityDigest:     fmt.Sprintf("rpt-child-%s-%d", h, slot),
				ExecutionTemplateDigest: plan.NormalizedCoreExecutionTemplateDigest,
				AccessBoundaryDigest:    fmt.Sprintf("rpt-boundary-%s-%d", h, slot),
			}
			for _, k := range []FormalProbeKind{
				FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
				FProbeAuditRead, FProbeAuthorStateRead, FProbePriorCaseStateRead,
				FProbeRetiredWorkspaceRead, FProbeActiveSiblingRead,
			} {
				p.Probes = append(p.Probes, FormalAccessProbe{
					Kind: k, TargetDigest: "rpt-target", TargetAccessPolicyDigest: "rpt-policy",
					Expected: "denied", Outcome: "permission-denied",
				})
			}
			p.Probes = append(p.Probes, FormalAccessProbe{
				Kind: FProbeOwnWorkspaceRead, TargetDigest: "rpt-own", TargetAccessPolicyDigest: "rpt-policy",
				Expected: "readable", Outcome: "readable",
			})
			r.WorkerProbes = append(r.WorkerProbes, p)
		}
	}
	return r
}

func rptPackage(m *FormalSeriesManifest) *SkillPackageValidationReceipt {
	r := &SkillPackageValidationReceipt{
		SnapshotID: "rpt-snapshot-id", SnapshotDigest: m.SkillSnapshotDigest, SnapshotAnchorDigest: m.SkillSnapshotAnchorDigest,
		SkillVersion: m.SkillVersion, SkillDigest: m.SkillDigest,
		FileRecordsDigest: "rpt-file-records", ValidatorRevision: "rpt-validator-rev", ValidatorDigest: "rpt-validator",
		ValidatorArgvDigest: "rpt-validator-argv", ValidatorOutputDigest: "rpt-validator-out",
		Checks: map[string]bool{"frontmatter": true}, Passed: true, ValidatedAt: "2026-09-03T00:00:00Z",
	}
	d, err := receiptDigestPV(r)
	if err != nil {
		panic(err)
	}
	r.ReceiptDigest = d
	m.SkillPackageValidationReceiptDigest = d
	return r
}

func rptGreenPrepare(m *FormalSeriesManifest) *GreenTestReceipt {
	r := &GreenTestReceipt{
		SchemaVersion: 1, Suite: SuiteSeriesPrepare, SuiteManifestDigest: "rpt-suite-manifest",
		Commands: []GreenCommand{{
			Name: "go-test-skill-eval", ArgvDigest: "rpt-argv", ExitCode: 0,
			StdoutDigest: "rpt-out", StderrDigest: "rpt-err",
		}},
		RunnerDigest: m.RunnerDigest, JudgeRuleDigest: m.JudgeRuleDigest, ValidatorDigest: "rpt-validator",
		SnapshotDigest: &m.SkillSnapshotDigest, PackageValidationReceiptDigest: &m.SkillPackageValidationReceiptDigest,
		Passed: true, CreatedAt: "2026-09-03T00:00:00Z",
	}
	id := "rpt-stable-identity"
	r.StableIdentityDigest = &id
	m.SeriesPrepareIdentityDigest = id
	d, err := receiptDigest(r)
	if err != nil {
		panic(err)
	}
	r.ReceiptDigest = d
	m.GreenTestReceiptDigest = d
	return r
}

// rptGreenPreHoldout is the fresh pre-holdout receipt of exactly this series,
// bound to its manifest, stable candidate digest and core-leg completion.
func rptGreenPreHoldout(m *FormalSeriesManifest, coreLeg string) *GreenTestReceipt {
	r := &GreenTestReceipt{
		SchemaVersion: 1, Suite: SuitePreHoldout, SuiteManifestDigest: "rpt-suite-manifest-pre",
		Commands: []GreenCommand{{
			Name: "node-validator-unit", ArgvDigest: "rpt-argv-pre", ExitCode: 0,
			StdoutDigest: "rpt-out-pre", StderrDigest: "rpt-err-pre",
		}},
		RunnerDigest: m.RunnerDigest, JudgeRuleDigest: m.JudgeRuleDigest, ValidatorDigest: "rpt-validator",
		SnapshotDigest: &m.SkillSnapshotDigest, PackageValidationReceiptDigest: &m.SkillPackageValidationReceiptDigest,
		SeriesManifestDigest: &m.ManifestDigest, CandidateBindingDigest: &m.CandidateBindingDigest,
		CoreLegCompletionDigest: &coreLeg,
		Passed:                  true, CreatedAt: "2026-09-03T00:00:00Z",
	}
	d, err := receiptDigest(r)
	if err != nil {
		panic(err)
	}
	r.ReceiptDigest = d
	return r
}

func rptCaseIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%03d", prefix, i+1)
	}
	return ids
}

func rptRun(m *FormalSeriesManifest, host, split string, ordinal, runSeq int) *PrimaryRunManifest {
	membership, err := MembershipOfSplit(split)
	if err != nil {
		panic(err)
	}
	count, err := ExpectedQuestionCount(membership)
	if err != nil {
		panic(err)
	}
	ids := rptCaseIDs(membership, count)
	return &PrimaryRunManifest{
		Mode: "primary", SeriesID: m.SeriesID, Host: host, Split: split, Ordinal: ordinal,
		ToolProvenance: ToolProvenance{
			Host: host, ResolvedModel: "rpt-model", BillingClass: BillingFree,
			SourceRevision: "rpt-source", ToolIdentityDigest: fmt.Sprintf("rpt-tool-%s", host),
		},
		CaseIDs:           ids,
		CaseSetDigest:     fmt.Sprintf("rpt-caseset-%d", runSeq),
		CaseOrder:         append([]string(nil), ids...),
		CaseOrderDigest:   fmt.Sprintf("rpt-order-%d", runSeq),
		ExpectedCaseCount: count,
		StartedAt:         "2026-09-03T00:00:00Z", CompletedAt: "2026-09-03T01:00:00Z",
		State:     StateComplete,
		RunDigest: fmt.Sprintf("rpt-run-%d", runSeq), SealDigest: fmt.Sprintf("rpt-run-seal-%d", runSeq),
	}
}

func rptRuns(m *FormalSeriesManifest) map[RunKey]*PrimaryRunManifest {
	runs := map[RunKey]*PrimaryRunManifest{}
	seq := 0
	for _, h := range m.Hosts {
		for _, split := range []string{SplitDevRegression, SplitHoldout} {
			for _, o := range Ordinals {
				seq++
				runs[RunKey{h, split, o}] = rptRun(m, h, split, o, seq)
			}
		}
	}
	return runs
}

func rptBinding(m *FormalSeriesManifest, preHoldout *GreenTestReceipt, coreLeg string, withPriorInvalid bool) *HoldoutBindingReceipt {
	attempts := []HoldoutSeriesAttempt{}
	if withPriorInvalid {
		attempts = append(attempts, HoldoutSeriesAttempt{
			SeriesID:                         "rpt-series-invalid-0",
			SeriesManifestDigest:             "rpt-invalid-manifest",
			CandidateBindingDigestSelf:       m.CandidateBindingDigest,
			PreHoldoutGreenTestReceiptDigest: "rpt-pre-holdout-invalid-attempt",
			CoreLegCompletionDigest:          "rpt-coreleg-invalid-attempt",
			StartedAt:                        "2026-09-03T00:00:00Z",
			State:                            "invalid",
			TerminalAt:                       "2026-09-03T02:00:00Z",
			RecoveryEventDigest:              "rpt-recovery-event",
		})
	}
	attempts = append(attempts, HoldoutSeriesAttempt{
		SeriesID: m.SeriesID, SeriesManifestDigest: m.ManifestDigest,
		CandidateBindingDigestSelf:       m.CandidateBindingDigest,
		PreHoldoutGreenTestReceiptDigest: preHoldout.ReceiptDigest,
		CoreLegCompletionDigest:          coreLeg,
		StartedAt:                        "2026-09-03T03:00:00Z", State: "complete-pass",
		TerminalAt: "2026-09-03T09:00:00Z",
	})
	return &HoldoutBindingReceipt{
		DatasetVersion: "rpt-holdout-v1", DatasetManifestDigest: m.DatasetManifests[MembershipHoldout96],
		CandidateBindingDigest: m.CandidateBindingDigest, FirstPrimaryStartedAt: "2026-09-03T00:00:00Z",
		SeriesAttempts: attempts, State: "consumed", ConsumedBySeries: m.SeriesID,
		ReceiptDigest: "rpt-binding-digest",
	}
}

// rptSpecs is the frozen case matrix of both splits: 172 dev cases
// (28×4 implicit + 18/6/4 trap + 32 legacy 020) and 96 holdout cases
// (20×4 implicit + 8/4/4 trap). It mirrors the sealed dataset quotas so the
// gate denominators land exactly on the 90%/10% integer boundaries.
func rptSpecs() []ScoreCaseSpec {
	var specs []ScoreCaseSpec
	add := func(split, module, prefix string, n, triggerN int) {
		for i := 1; i <= n; i++ {
			specs = append(specs, ScoreCaseSpec{
				CaseID: fmt.Sprintf("%s-%03d", prefix, i), Split: split, Module: module,
				Trigger: i <= triggerN,
			})
		}
	}
	add(SplitDevRegression, "implicit-write-pos", "rpt-iwp", 28, 28)
	add(SplitDevRegression, "implicit-write-neg", "rpt-iwn", 28, 0)
	add(SplitDevRegression, "implicit-read-pos", "rpt-irp", 28, 28)
	add(SplitDevRegression, "implicit-read-neg", "rpt-irn", 28, 0)
	add(SplitDevRegression, "trap-read-pos", "rpt-trp", 18, 18)
	add(SplitDevRegression, "trap-write-neg", "rpt-twn", 6, 0)
	add(SplitDevRegression, "trap-read-neg", "rpt-trn", 4, 0)
	add(SplitDevRegression, "regression", "rpt-reg", 32, 16)
	add(SplitHoldout, "implicit-write-pos", "rpt-h-iwp", 20, 20)
	add(SplitHoldout, "implicit-write-neg", "rpt-h-iwn", 20, 0)
	add(SplitHoldout, "implicit-read-pos", "rpt-h-irp", 20, 20)
	add(SplitHoldout, "implicit-read-neg", "rpt-h-irn", 20, 0)
	add(SplitHoldout, "trap-read-pos", "rpt-h-trp", 8, 8)
	add(SplitHoldout, "trap-write-neg", "rpt-h-twn", 4, 0)
	add(SplitHoldout, "trap-read-neg", "rpt-h-trn", 4, 0)
	return specs
}

// rptStates materializes the complete 3-host × 3-ordinal terminal matrix.
// override decides each cell when non-nil; nil means every case terminates
// with its correct outcome.
func rptStates(specs []ScoreCaseSpec, override func(host string, ordinal int, s ScoreCaseSpec) CaseScoreOutcome) []CaseScoreState {
	var states []CaseScoreState
	for _, host := range rptHosts() {
		for _, o := range Ordinals {
			for _, s := range specs {
				out := CaseOutcomePass
				if override != nil {
					out = override(host, o, s)
				}
				states = append(states, CaseScoreState{
					Host: host, Split: s.Split, Ordinal: o, CaseID: s.CaseID, Outcome: out,
				})
			}
		}
	}
	return states
}

// rptFailSpecs fails `ids` for one host on the given ordinals with the given
// outcome; every other cell keeps its correct outcome.
func rptFailSpecs(ids []string, host string, ordinals map[int]bool, outcome CaseScoreOutcome) func(string, int, ScoreCaseSpec) CaseScoreOutcome {
	return func(h string, o int, s ScoreCaseSpec) CaseScoreOutcome {
		if h != host || !ordinals[o] {
			return CaseOutcomePass
		}
		for _, id := range ids {
			if s.CaseID == id {
				return outcome
			}
		}
		return CaseOutcomePass
	}
}

func rptScore(t *testing.T, specs []ScoreCaseSpec, states []CaseScoreState) *ScoreMatrix {
	t.Helper()
	m, err := ComputeOfficialScore("rpt-series", rptHosts(), specs, states)
	if err != nil {
		t.Fatalf("ComputeOfficialScore: %v", err)
	}
	return m
}

func rptGate(t *testing.T, m *ScoreMatrix, host, split string, id ScoreGateID) *GateScore {
	t.Helper()
	hs := m.HostSplit(host, split)
	if hs == nil {
		t.Fatalf("no score for %s/%s", host, split)
	}
	for i := range hs.Gates {
		if hs.Gates[i].ID == id {
			return &hs.Gates[i]
		}
	}
	t.Fatalf("gate %s missing for %s/%s", id, host, split)
	return nil
}

// rptBundle is a fully eligible official-dual bundle whose ledger carries one
// prior invalid series plus the complete scored recovery series.
func rptBundle(t *testing.T) (*ScoreEligibilityInput, *ScoreMatrix) {
	t.Helper()
	plan := rptPlan()
	m := rptManifest(plan)
	in := &ScoreEligibilityInput{
		Manifest: m, Plan: plan,
		Protected:     rptProtected(plan),
		Canaries:      rptCanaries(m, plan),
		Package:       rptPackage(m),
		SeriesPrepare: rptGreenPrepare(m),
	}
	coreLeg := "rpt-coreleg-complete"
	in.PreHoldout = rptGreenPreHoldout(m, coreLeg)
	in.Runs = rptRuns(m)
	in.Binding = rptBinding(m, in.PreHoldout, coreLeg, true)
	if err := ValidateOfficialScoreEligibility(in); err != nil {
		t.Fatalf("fixture bundle must be eligible: %v", err)
	}
	specs := rptSpecs()
	return in, rptScore(t, specs, rptStates(specs, nil))
}

func TestGateExactIntegerBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		positive bool
		num      int
		total    int
		want     bool
	}{
		{"9/10 is exactly 90% and passes", true, 9, 10, true},
		{"90/100 is exactly 90% and passes", true, 90, 100, true},
		{"89/100 fails", true, 89, 100, false},
		{"91/100 passes", true, 91, 100, true},
		{"18/20 is exactly 90% and passes", true, 18, 20, true},
		{"17/20 fails", true, 17, 20, false},
		{"8/8 passes", true, 8, 8, true},
		{"7/8 is 87.5% and fails", true, 7, 8, false},
		{"26/28 is the lowest passing dev write numerator", true, 26, 28, true},
		{"25/28 fails", true, 25, 28, false},
		{"1/10 is exactly 10% and passes the negative gate", false, 1, 10, true},
		{"0/10 passes the negative gate", false, 0, 10, true},
		{"2/10 fails the negative gate", false, 2, 10, false},
		{"11/100 fails the negative gate", false, 11, 100, false},
		{"9/100 passes the negative gate", false, 9, 100, true},
		{"4/40 is exactly 10% and passes", false, 4, 40, true},
		{"5/40 fails", false, 5, 40, false},
		{"1/8 misfire fails the holdout trap gate", false, 1, 8, false},
		{"0/8 misfire passes the holdout trap gate", false, 0, 8, true},
		{"1/16 misfire passes the 020 negative gate", false, 1, 16, true},
		{"2/16 misfire fails the 020 negative gate", false, 2, 16, false},
		{"empty positive gate never passes", true, 0, 0, false},
		{"empty negative gate never passes", false, 0, 0, false},
	}
	for _, c := range cases {
		got := PassesGateNegative(c.num, c.total)
		if c.positive {
			got = PassesGatePositive(c.num, c.total)
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestMedianBoolConservativeAndMedianInt(t *testing.T) {
	cases := []struct {
		name   string
		states []bool
		want   bool
	}{
		{"two of three ordinals pass", []bool{false, true, true}, true},
		{"one of three ordinals passes", []bool{true, false, false}, false},
		{"all three pass", []bool{true, true, true}, true},
		{"all three fail", []bool{false, false, false}, false},
		{"order independent", []bool{true, false, true}, true},
		{"even tie is conservative (fail)", []bool{true, false}, false},
		{"even tie reversed is conservative", []bool{false, true}, false},
		{"even full pass stays a pass", []bool{true, true}, true},
		{"empty is conservative", nil, false},
	}
	for _, c := range cases {
		if got := MedianBool(c.states); got != c.want {
			t.Errorf("%s: MedianBool=%v want %v", c.name, got, c.want)
		}
	}
	if got := MedianInt([]int{9, 10, 8}); got != 9 {
		t.Errorf("MedianInt of 9/10/8 = %d, want 9", got)
	}
	if got := MedianInt([]int{7, 9, 10}); got != 9 {
		t.Errorf("MedianInt of 7/9/10 = %d, want 9", got)
	}
	if got := MedianInt([]int{4}); got != 4 {
		t.Errorf("MedianInt of a single value = %d, want 4", got)
	}
	if got := MedianInt([]int{1, 2, 3, 4}); got != 2 {
		t.Errorf("MedianInt of an even slice = %d, want the conservative lower middle 2", got)
	}
	if got := MedianInt(nil); got != 0 {
		t.Errorf("MedianInt of nil = %d, want 0", got)
	}
}

func TestRouteScoreMetricsDevOnly020HoldoutOnlyTrap(t *testing.T) {
	dev, err := RouteScoreMetrics(SplitDevRegression)
	if err != nil {
		t.Fatalf("dev routing: %v", err)
	}
	hold, err := RouteScoreMetrics(SplitHoldout)
	if err != nil {
		t.Fatalf("holdout routing: %v", err)
	}
	// The 020 regression pair of SC-4 is dev-only.
	rptAssertRouted(t, dev, GateRegressionPos, true)
	rptAssertRouted(t, dev, GateRegressionNeg, false)
	rptAssertNotRouted(t, dev, GateTrapReadPos)
	rptAssertNotRouted(t, dev, GateTrapNeg)
	// The trap pair of SC-4 is holdout-only.
	rptAssertRouted(t, hold, GateTrapReadPos, true)
	rptAssertRouted(t, hold, GateTrapNeg, false)
	rptAssertNotRouted(t, hold, GateRegressionPos)
	rptAssertNotRouted(t, hold, GateRegressionNeg)
	// SC-1/SC-2/SC-3 hold on both splits, with the negative gate merged.
	for split, gates := range map[string][]ScoreMetric{"dev": dev, "holdout": hold} {
		rptAssertRouted(t, gates, GateImplicitWritePos, true)
		rptAssertRouted(t, gates, GateImplicitReadPos, true)
		rptAssertRouted(t, gates, GateImplicitNeg, false)
		for _, g := range gates {
			if g.ID != GateImplicitNeg {
				continue
			}
			if len(g.Modules) != 2 || !rptHasString(g.Modules, "implicit-write-neg") || !rptHasString(g.Modules, "implicit-read-neg") {
				t.Errorf("%s SC-3 gate modules = %v, want the merged write-neg + read-neg pair", split, g.Modules)
			}
		}
	}
	// A dev split must not leak its 28 trap cases into the holdout trap gates:
	// through the scorer they are reported as unrouted instead.
	specs := rptSpecs()
	m := rptScore(t, specs, rptStates(specs, nil))
	unrouted := m.Unrouted[SplitDevRegression]
	if len(unrouted) != 28 {
		t.Fatalf("dev unrouted cases = %d, want the 28 trap cases", len(unrouted))
	}
	for _, id := range unrouted {
		if !strings.HasPrefix(id, "rpt-trp-") && !strings.HasPrefix(id, "rpt-twn-") && !strings.HasPrefix(id, "rpt-trn-") {
			t.Fatalf("dev unrouted case %s is not a trap case", id)
		}
	}
	if got := len(m.Unrouted[SplitHoldout]); got != 0 {
		t.Fatalf("holdout unrouted cases = %d, want 0", got)
	}
	// Closed split set.
	if _, err := RouteScoreMetrics("diagnostic"); err == nil {
		t.Error("unknown split must have no score routing")
	}
}

func rptAssertRouted(t *testing.T, gates []ScoreMetric, id ScoreGateID, positive bool) {
	t.Helper()
	for _, g := range gates {
		if g.ID != id {
			continue
		}
		if g.Positive != positive {
			t.Errorf("gate %s polarity = %v, want %v", id, g.Positive, positive)
		}
		return
	}
	t.Errorf("gate %s missing from routing", id)
}

func rptAssertNotRouted(t *testing.T, gates []ScoreMetric, id ScoreGateID) {
	t.Helper()
	for _, g := range gates {
		if g.ID == id {
			t.Fatalf("gate %s must not be routed here", id)
		}
	}
}

func rptHasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestScoreThreeOrdinalMedianGate(t *testing.T) {
	specs := rptSpecs()
	ids := rptCaseIDs("rpt-iwp", 28)
	// A single-ordinal dip cannot fail the gate: numerators 26/28/28 have
	// median 28.
	m := rptScore(t, specs, rptStates(specs, rptFailSpecs(ids[:2], HostClaude, map[int]bool{1: true}, CaseOutcomeFail)))
	if got := rptGate(t, m, HostClaude, SplitDevRegression, GateImplicitWritePos); !got.Passed {
		t.Fatalf("single-ordinal dip must not fail the median gate: %+v", got)
	}
	// A permanent two-case loss sits exactly on the 90% boundary and passes.
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(ids[:2], HostClaude, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	g := rptGate(t, m, HostClaude, SplitDevRegression, GateImplicitWritePos)
	if !g.Passed || g.MedianNumerator != 26 || g.Denominator != 28 {
		t.Fatalf("26/28 is exactly 90%% and must pass, got %+v", g)
	}
	if want := []int{26, 26, 26}; !reflect.DeepEqual(g.OrdinalNumerators, want) {
		t.Fatalf("ordinal numerators = %v, want %v", g.OrdinalNumerators, want)
	}
	// A permanent three-case loss drops the median below 90% and fails, while
	// the other hosts are untouched.
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(ids[:3], HostClaude, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	g = rptGate(t, m, HostClaude, SplitDevRegression, GateImplicitWritePos)
	if g.Passed || g.MedianNumerator != 25 {
		t.Fatalf("25/28 must fail the ≥90%% gate, got %+v", g)
	}
	if other := rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitWritePos); !other.Passed || other.MedianNumerator != 28 {
		t.Fatalf("failing host must not leak into codex: %+v", other)
	}
	// A 2-of-3 median recovers a host that collapsed on one ordinal only.
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(ids[:3], HostCodex, map[int]bool{2: true}, CaseOutcomeFail)))
	g = rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitWritePos)
	if !g.Passed || g.MedianNumerator != 28 || !reflect.DeepEqual(g.OrdinalNumerators, []int{28, 25, 28}) {
		t.Fatalf("numerators 28/25/28 must median to 28 and pass, got %+v", g)
	}
}

func TestScoreTrapGateEightOfEightAndZeroOfEight(t *testing.T) {
	specs := rptSpecs()
	// Baseline: the holdout trap gates sit exactly on their boundaries.
	m := rptScore(t, specs, rptStates(specs, nil))
	pos := rptGate(t, m, HostOpenCode, SplitHoldout, GateTrapReadPos)
	if !pos.Passed || pos.Denominator != 8 || pos.MedianNumerator != 8 {
		t.Fatalf("trap-read-pos must be 8/8 and pass, got %+v", pos)
	}
	neg := rptGate(t, m, HostOpenCode, SplitHoldout, GateTrapNeg)
	if !neg.Passed || neg.Denominator != 8 || neg.MedianNumerator != 0 {
		t.Fatalf("trap-neg must be 0/8 misfires and pass, got %+v", neg)
	}
	// 7/8 (87.5%) fails the ≥90% trap-read-pos gate.
	trapPos := rptCaseIDs("rpt-h-trp", 8)
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(trapPos[:1], HostOpenCode, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	if g := rptGate(t, m, HostOpenCode, SplitHoldout, GateTrapReadPos); g.Passed || g.MedianNumerator != 7 {
		t.Fatalf("7/8 must fail the trap-read-pos gate, got %+v", g)
	}
	// A single-ordinal 7/8 leaves the 8/8 median intact.
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(trapPos[:1], HostOpenCode, map[int]bool{1: true}, CaseOutcomeFail)))
	if g := rptGate(t, m, HostOpenCode, SplitHoldout, GateTrapReadPos); !g.Passed {
		t.Fatalf("7/8/8 must median to 8/8 and pass, got %+v", g)
	}
	// A single trap misfire out of 8 (12.5%) fails the ≤10% trap-neg gate.
	trapNeg := append(rptCaseIDs("rpt-h-twn", 4), rptCaseIDs("rpt-h-trn", 4)...)
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(trapNeg[:1], HostOpenCode, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	if g := rptGate(t, m, HostOpenCode, SplitHoldout, GateTrapNeg); g.Passed || g.MedianNumerator != 1 {
		t.Fatalf("1/8 misfire must fail the trap-neg gate, got %+v", g)
	}
}

func TestScoreDevComparisonRejected(t *testing.T) {
	plan := rptPlan()
	in := &ScoreEligibilityInput{Manifest: rptManifest(plan), Plan: plan}
	in.Manifest.Purpose = PurposeDevComparison
	in.Manifest.DatasetManifests = map[string]string{MembershipCore172: "rpt-core-manifest"}
	in.Manifest.QuestionCount = map[string]int{MembershipCore172: 172}
	in.Manifest.CandidateBindingDigest = ""
	in.Manifest.ProtectedExecutionReceiptDigest = ""
	in.Manifest.ProtectedExecutionPolicyDigest = ""
	if err := ValidateFormalSeriesManifest(in.Manifest); err != nil {
		t.Fatalf("dev-comparison fixture manifest must still be a valid series: %v", err)
	}
	err := ValidateOfficialScoreEligibility(in)
	if err == nil {
		t.Fatal("dev-comparison series must be rejected by the official scorer")
	}
	if !strings.Contains(err.Error(), "can never enter the official scorer") {
		t.Fatalf("dev-comparison rejection must name the purpose rule, got %q", err)
	}
	if got := ComputeOverallVerdict(nil); got != "invalid" {
		t.Fatalf("verdict of a nil matrix = %q, want invalid", got)
	}
	if got := ComputeOverallVerdict(&ScoreMatrix{}); got != "invalid" {
		t.Fatalf("verdict of an empty matrix = %q, want invalid", got)
	}
}

func TestScorePerHostGatesIndependent(t *testing.T) {
	specs := rptSpecs()
	// All three hosts hold every gate: verdict pass, six host × split scores.
	m := rptScore(t, specs, rptStates(specs, nil))
	if got := len(m.Scores); got != 6 {
		t.Fatalf("score entries = %d, want 3 hosts × 2 splits = 6", got)
	}
	if got := ComputeOverallVerdict(m); got != "pass" {
		t.Fatalf("verdict = %q, want pass", got)
	}
	// One host missing one holdout gate is enough to fail the whole series,
	// while every other host × split keeps its own passing record.
	trapPos := rptCaseIDs("rpt-h-trp", 8)
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(trapPos[:1], HostOpenCode, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	if got := ComputeOverallVerdict(m); got != "fail" {
		t.Fatalf("verdict = %q, want fail when any single host gate fails", got)
	}
	if m.HostSplit(HostOpenCode, SplitHoldout).Passed() {
		t.Fatal("opencode holdout must fail")
	}
	for _, host := range []string{HostClaude, HostCodex} {
		if !m.HostSplit(host, SplitHoldout).Passed() {
			t.Fatalf("%s holdout must still pass on its own", host)
		}
		if !m.HostSplit(host, SplitDevRegression).Passed() {
			t.Fatalf("%s dev family must still pass on its own", host)
		}
	}
	if !m.HostSplit(HostOpenCode, SplitDevRegression).Passed() {
		t.Fatal("opencode dev family is unaffected and must pass")
	}
	// The dev family stays separate from the holdout family: no host score
	// may borrow a gate from the other split.
	for _, hs := range m.Scores {
		for _, g := range hs.Gates {
			if hs.Split == SplitDevRegression && (g.ID == GateTrapReadPos || g.ID == GateTrapNeg) {
				t.Errorf("dev family carries the holdout-only gate %s", g.ID)
			}
			if hs.Split == SplitHoldout && (g.ID == GateRegressionPos || g.ID == GateRegressionNeg) {
				t.Errorf("holdout family carries the dev-only gate %s", g.ID)
			}
		}
	}
}

func TestScoreNegativeRunnerErrorConservative(t *testing.T) {
	specs := rptSpecs()
	// The holdout implicit negative gate merges write-neg + read-neg (40
	// cases, ≤4 misfires). Four terminal runner-errors are exactly 10%:
	// counted conservatively they still pass, and they are reported
	// separately.
	negIDs := append(rptCaseIDs("rpt-h-iwn", 20), rptCaseIDs("rpt-h-irn", 20)...)
	m := rptScore(t, specs, rptStates(specs, rptFailSpecs(negIDs[:4], HostClaude, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	g := rptGate(t, m, HostClaude, SplitHoldout, GateImplicitNeg)
	if !g.Passed || g.MedianNumerator != 4 {
		t.Fatalf("4/40 runner-error misfires are exactly 10%% and must pass, got %+v", g)
	}
	if g.RunnerErrors != 12 {
		t.Fatalf("runner errors = %d, want 12 (4 cases × 3 ordinals)", g.RunnerErrors)
	}
	// A fifth pushes past 10% and fails the gate.
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(negIDs[:5], HostClaude, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	if g := rptGate(t, m, HostClaude, SplitHoldout, GateImplicitNeg); g.Passed || g.MedianNumerator != 5 {
		t.Fatalf("5/40 runner-error misfires must fail the negative gate, got %+v", g)
	}
	// The merged dev gate (56 cases) treats them the same way, off the
	// boundary: 4 of 56 still passes, 6 of 56 (10.7%) fails.
	devNeg := append(rptCaseIDs("rpt-iwn", 28), rptCaseIDs("rpt-irn", 28)...)
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(devNeg[:4], HostCodex, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	if g := rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitNeg); !g.Passed || g.MedianNumerator != 4 {
		t.Fatalf("4/56 runner-error misfires must pass the merged dev gate, got %+v", g)
	}
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(devNeg[:6], HostCodex, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	if g := rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitNeg); g.Passed || g.MedianNumerator != 6 {
		t.Fatalf("6/56 runner-error misfires must fail the merged dev gate, got %+v", g)
	}
	// A runner-error on a positive case is not a pass either.
	posIDs := rptCaseIDs("rpt-iwp", 28)
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(posIDs[:2], HostCodex, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	if g := rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitWritePos); !g.Passed || g.MedianNumerator != 26 || g.RunnerErrors != 6 {
		t.Fatalf("two positive runner-errors sit on the 90%% boundary: got %+v", g)
	}
	m = rptScore(t, specs, rptStates(specs, rptFailSpecs(posIDs[:3], HostCodex, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeRunnerError)))
	if g := rptGate(t, m, HostCodex, SplitDevRegression, GateImplicitWritePos); g.Passed {
		t.Fatalf("three positive runner-errors must fail the ≥90%% gate, got %+v", g)
	}
}

func TestScoreLowNDiagnosticCells(t *testing.T) {
	// A small synthetic holdout slice: only 4 independent trap cases per gate,
	// far below LowNThreshold, so the cells must be flagged low-N.
	var specs []ScoreCaseSpec
	add := func(module, prefix string, n int) {
		for i := 1; i <= n; i++ {
			specs = append(specs, ScoreCaseSpec{
				CaseID: fmt.Sprintf("%s-%02d", prefix, i), Split: SplitHoldout,
				Module: module, Trigger: strings.HasSuffix(module, "-pos"),
			})
		}
	}
	add("trap-read-pos", "rpt-low-trp", 4)
	add("trap-write-neg", "rpt-low-twn", 4)
	m := rptScore(t, specs, rptStates(specs, nil))
	pos := rptGate(t, m, HostClaude, SplitHoldout, GateTrapReadPos)
	if pos.Bias.Label != string(GateTrapReadPos) || pos.Bias.IndependentCaseCount != 4 || !pos.Bias.LowN {
		t.Fatalf("4 independent trap-read-pos cases must be flagged low-N: %+v", pos.Bias)
	}
	// The pooled cell carries three ordinal observations over those cases.
	if pos.Bias.Denominator != 12 || pos.Bias.Numerator != 12 {
		t.Fatalf("pooled bias cell = %+v, want 12/12 over 3 ordinals", pos.Bias)
	}
	// The full 172/96 fixture has no low-N gate cell.
	full := rptSpecs()
	fm := rptScore(t, full, rptStates(full, nil))
	for _, hs := range fm.Scores {
		for _, g := range hs.Gates {
			if g.Bias.LowN {
				t.Errorf("gate %s (%d independent cases) must not be flagged low-N", g.ID, g.Bias.IndependentCaseCount)
			}
			if g.Bias.IndependentCaseCount != g.Denominator {
				t.Errorf("gate %s independent case count %d != denominator %d", g.ID, g.Bias.IndependentCaseCount, g.Denominator)
			}
		}
	}
	// The scorer fails closed on an incomplete matrix: one missing ordinal
	// cell is an error, never a silently smaller denominator.
	states := rptStates(specs, nil)
	states = states[:len(states)-1]
	if _, err := ComputeOfficialScore("rpt-series", rptHosts(), specs, states); err == nil {
		t.Fatal("a missing terminal state must fail the scorer closed")
	}
	// And on any unknown host, ordinal or outcome.
	if _, err := ComputeOfficialScore("rpt-series", rptHosts()[:2], specs, rptStates(specs, nil)); err == nil {
		t.Fatal("two hosts must be rejected: SC-9 has no partial-host mode")
	}
	bad := rptStates(specs, nil)
	bad[0].Outcome = "crashed"
	if _, err := ComputeOfficialScore("rpt-series", rptHosts(), specs, bad); err == nil {
		t.Fatal("an unknown terminal outcome must fail the scorer closed")
	}
}

// rptReseal recomputes a green receipt's self-digest after a binding
// mutation, so the row exercises the binding check instead of the earlier
// post-hoc-mutation check.
func rptReseal(r *GreenTestReceipt) {
	d, err := receiptDigest(r)
	if err != nil {
		panic(err)
	}
	r.ReceiptDigest = d
}

func TestScoreEligibilityFailuresTable(t *testing.T) {
	rows := []struct {
		name   string
		mutate func(in *ScoreEligibilityInput)
		want   string
	}{
		{"missing host run receipt", func(in *ScoreEligibilityInput) {
			delete(in.Runs, RunKey{HostOpenCode, SplitDevRegression, 3})
		}, "missing primary run receipt"},
		{"missing split run receipt", func(in *ScoreEligibilityInput) {
			for _, h := range in.Manifest.Hosts {
				delete(in.Runs, RunKey{h, SplitHoldout, 1})
			}
		}, "missing primary run receipt"},
		{"missing ordinal run receipt", func(in *ScoreEligibilityInput) {
			delete(in.Runs, RunKey{HostClaude, SplitHoldout, 3})
		}, "missing primary run receipt"},
		{"case receipt missing from a run", func(in *ScoreEligibilityInput) {
			r := in.Runs[RunKey{HostClaude, SplitDevRegression, 1}]
			r.CaseIDs = r.CaseIDs[:171]
			r.CaseOrder = r.CaseOrder[:171]
		}, "cases, want 172"},
		{"case receipt repeats a case id", func(in *ScoreEligibilityInput) {
			r := in.Runs[RunKey{HostClaude, SplitDevRegression, 1}]
			r.CaseIDs[171] = r.CaseIDs[0]
		}, "repeats case"},
		{"run receipt from an unexpected split", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostClaude, "diagnostic", 1}] = rptRun(in.Manifest, HostClaude, SplitDevRegression, 1, 99)
		}, "unexpected primary run"},
		{"package receipt missing", func(in *ScoreEligibilityInput) { in.Package = nil }, "package validation receipt missing"},
		{"package receipt failed", func(in *ScoreEligibilityInput) { in.Package.Passed = false }, "missing or failed"},
		{"package receipt digest mismatch", func(in *ScoreEligibilityInput) {
			in.Package.ReceiptDigest = "rpt-other-pv"
		}, "!= series bound"},
		{"package receipt bound to another snapshot", func(in *ScoreEligibilityInput) {
			in.Package.SnapshotDigest = "rpt-other-snapshot"
		}, "not bound to the scored skill snapshot"},
		{"package receipt skill digest drift", func(in *ScoreEligibilityInput) {
			in.Package.SkillDigest = "rpt-other-skill"
		}, "skill digest drift"},
		{"package receipt post-hoc mutation", func(in *ScoreEligibilityInput) {
			in.Package.ValidatedAt = "2020-01-01T00:00:00Z"
		}, "post-hoc mutation"},
		{"protected receipt missing", func(in *ScoreEligibilityInput) { in.Protected = nil }, "protected execution receipt missing"},
		{"protected receipt digest mismatch", func(in *ScoreEligibilityInput) {
			in.Protected.ReceiptDigest = "rpt-other-protected"
		}, "!= series bound"},
		{"protected receipt overlapping allocators", func(in *ScoreEligibilityInput) {
			in.Protected.SplitStateAllocatorDigests[MembershipHoldout96] = in.Protected.SplitStateAllocatorDigests[MembershipCore172]
		}, "allocators overlap"},
		{"protected receipt plan drift", func(in *ScoreEligibilityInput) {
			in.Protected.CoreExecutionPlanDigest = "rpt-other-plan"
		}, "plan digest"},
		{"canary receipt missing for a host", func(in *ScoreEligibilityInput) {
			delete(in.Canaries, HostCodex)
			delete(in.Manifest.WorkspaceCanaryReceiptDigests, HostCodex)
		}, "workspace canary receipt missing"},
		{"canary receipt missing for a worker slot", func(in *ScoreEligibilityInput) {
			delete(in.Canaries[HostClaude], 2)
		}, "workspace canary receipt missing"},
		{"canary receipt failed", func(in *ScoreEligibilityInput) {
			in.Canaries[HostClaude][1].Status = "fail"
		}, "canary status"},
		{"canary observation mismatch", func(in *ScoreEligibilityInput) {
			in.Canaries[HostClaude][2].ObservedFileDigest = "rpt-other-file"
		}, "observation mismatch"},
		{"canary receipt for an unknown worker slot", func(in *ScoreEligibilityInput) {
			slot9 := *in.Canaries[HostClaude][1]
			slot9.WorkerSlot = 9
			in.Canaries[HostClaude][9] = &slot9
		}, "unknown worker slot"},
		{"canary digest mismatch in the manifest", func(in *ScoreEligibilityInput) {
			in.Manifest.WorkspaceCanaryReceiptDigests[HostClaude][1] = "rpt-other-canary"
		}, "receipt says"},
		{"series-prepare receipt missing", func(in *ScoreEligibilityInput) { in.SeriesPrepare = nil }, "missing series-prepare green test receipt"},
		{"series-prepare receipt failed", func(in *ScoreEligibilityInput) { in.SeriesPrepare.Passed = false }, "is failed"},
		{"series-prepare wrong-suite receipt", func(in *ScoreEligibilityInput) {
			in.SeriesPrepare.Suite = SuitePreHoldout
		}, "wrong-suite receipt"},
		{"series-prepare receipt without command evidence", func(in *ScoreEligibilityInput) {
			in.SeriesPrepare.Commands = nil
		}, "command evidence"},
		{"series-prepare post-hoc receipt", func(in *ScoreEligibilityInput) {
			in.SeriesPrepare.SuiteManifestDigest = "rpt-mutated-suite"
		}, "post-hoc mutation"},
		{"series-prepare digest drifted from the manifest", func(in *ScoreEligibilityInput) {
			in.Manifest.GreenTestReceiptDigest = "rpt-other-green"
		}, "!= manifest bound"},
		{"series-prepare stable identity mismatch", func(in *ScoreEligibilityInput) {
			in.Manifest.SeriesPrepareIdentityDigest = "rpt-other-identity"
		}, "stable identity mismatch"},
		{"series-prepare snapshot binding mismatch", func(in *ScoreEligibilityInput) {
			other := "rpt-other-snapshot" // independent: the manifest keeps its own digest
			in.SeriesPrepare.SnapshotDigest = &other
			rptReseal(in.SeriesPrepare)
			in.Manifest.GreenTestReceiptDigest = in.SeriesPrepare.ReceiptDigest
		}, "snapshot binding mismatch"},
		{"series-prepare package binding mismatch", func(in *ScoreEligibilityInput) {
			other := "rpt-other-pv"
			in.SeriesPrepare.PackageValidationReceiptDigest = &other
			rptReseal(in.SeriesPrepare)
			in.Manifest.GreenTestReceiptDigest = in.SeriesPrepare.ReceiptDigest
		}, "package-validation binding mismatch"},
		{"series-prepare runner digest drift", func(in *ScoreEligibilityInput) {
			in.SeriesPrepare.RunnerDigest = "rpt-other-runner"
			rptReseal(in.SeriesPrepare)
			in.Manifest.GreenTestReceiptDigest = in.SeriesPrepare.ReceiptDigest
		}, "runner/judge digest drift"},
		{"pre-holdout receipt missing", func(in *ScoreEligibilityInput) { in.PreHoldout = nil }, "missing pre-holdout green test receipt"},
		{"pre-holdout wrong-suite receipt", func(in *ScoreEligibilityInput) {
			in.PreHoldout.Suite = SuiteFormalTooling
		}, "wrong-suite receipt"},
		{"pre-holdout post-hoc receipt", func(in *ScoreEligibilityInput) {
			in.PreHoldout.SuiteManifestDigest = "rpt-mutated-suite"
		}, "post-hoc mutation"},
		{"pre-holdout receipt bound to another series manifest", func(in *ScoreEligibilityInput) {
			other := "rpt-other-manifest"
			in.PreHoldout.SeriesManifestDigest = &other
			rptReseal(in.PreHoldout)
		}, "not bound to this series manifest"},
		{"pre-holdout candidate binding mismatch", func(in *ScoreEligibilityInput) {
			other := "rpt-other-binding"
			in.PreHoldout.CandidateBindingDigest = &other
			rptReseal(in.PreHoldout)
		}, "candidate binding mismatch"},
		{"holdout binding missing", func(in *ScoreEligibilityInput) { in.Binding = nil }, "holdout binding receipt missing"},
		{"holdout binding for another candidate digest", func(in *ScoreEligibilityInput) {
			in.Binding.CandidateBindingDigest = "rpt-other-binding"
			for i := range in.Binding.SeriesAttempts {
				in.Binding.SeriesAttempts[i].CandidateBindingDigestSelf = "rpt-other-binding"
			}
		}, "stable candidate digest"},
		{"holdout binding dataset manifest mismatch", func(in *ScoreEligibilityInput) {
			in.Binding.DatasetManifestDigest = "rpt-other-holdout-manifest"
		}, "dataset manifest mismatch"},
		{"holdout binding not consumed by this series", func(in *ScoreEligibilityInput) {
			in.Binding.State = "frozen"
			in.Binding.ConsumedBySeries = ""
		}, "binding is still frozen"},
		{"recovery-series green test mismatch (receipt)", func(in *ScoreEligibilityInput) {
			in.Binding.SeriesAttempts[1].PreHoldoutGreenTestReceiptDigest = "rpt-other-pre-holdout"
		}, "recovery-series green test mismatch"},
		{"recovery-series green test mismatch (core leg)", func(in *ScoreEligibilityInput) {
			other := "rpt-other-coreleg"
			in.PreHoldout.CoreLegCompletionDigest = &other
			rptReseal(in.PreHoldout)
			// The attempt still records the new receipt, so only the core-leg
			// completion the receipt attests has drifted.
			in.Binding.SeriesAttempts[1].PreHoldoutGreenTestReceiptDigest = in.PreHoldout.ReceiptDigest
		}, "core-leg completion digest drift"},
		{"scored attempt manifest digest mismatch", func(in *ScoreEligibilityInput) {
			in.Binding.SeriesAttempts[1].SeriesManifestDigest = "rpt-other-manifest"
		}, "series manifest digest mismatch"},
		{"core plan digest mismatch", func(in *ScoreEligibilityInput) {
			in.Manifest.CoreExecutionPlanDigest = "rpt-other-plan"
		}, "!= series bound"},
		{"invalid-series scoring receipt", func(in *ScoreEligibilityInput) {
			in.Manifest.State = StateInvalid
		}, "cannot be scored"},
		{"draft-series scoring receipt", func(in *ScoreEligibilityInput) {
			in.Manifest.State = StateDraft
		}, "cannot be scored"},
		{"cross-series splice (run of another series)", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostCodex, SplitHoldout, 2}].SeriesID = "rpt-series-other"
		}, "cross-series splice"},
		{"cross-series splice (reused run digest)", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostCodex, SplitHoldout, 2}].RunDigest = in.Runs[RunKey{HostCodex, SplitHoldout, 1}].RunDigest
		}, "cross-series splice"},
		{"run receipt not complete", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostClaude, SplitDevRegression, 2}].State = StateSealed
		}, "is not complete"},
		{"unsealed run receipt", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostClaude, SplitHoldout, 2}].SealDigest = ""
		}, "is not sealed"},
		{"non-primary run receipt", func(in *ScoreEligibilityInput) {
			in.Runs[RunKey{HostOpenCode, SplitHoldout, 2}].Mode = "diagnostic"
		}, "is not primary"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			in, _ := rptBundle(t)
			row.mutate(in)
			err := ValidateOfficialScoreEligibility(in)
			if err == nil {
				t.Fatalf("expected a fail-closed rejection mentioning %q", row.want)
			}
			if !strings.Contains(err.Error(), row.want) {
				t.Fatalf("error %q does not mention %q", err, row.want)
			}
		})
	}
	// The untouched bundle stays eligible.
	intact, _ := rptBundle(t)
	if err := ValidateOfficialScoreEligibility(intact); err != nil {
		t.Fatalf("baseline bundle must stay eligible: %v", err)
	}
}

func TestScoreRecoveredReportSingleCompleteSeries(t *testing.T) {
	in, m := rptBundle(t)
	if len(in.Binding.SeriesAttempts) != 2 {
		t.Fatalf("fixture ledger carries %d attempts, want the invalid prior + the scored recovery", len(in.Binding.SeriesAttempts))
	}
	prior, scored := in.Binding.SeriesAttempts[0], in.Binding.SeriesAttempts[1]
	if prior.State != "invalid" || scored.SeriesID != in.Manifest.SeriesID {
		t.Fatalf("fixture ledger = %+v / %+v", prior, scored)
	}
	// The prior invalid series survives only as non-scoring ledger evidence:
	// nothing in the report or the bundle references its artifacts.
	if in.Binding.ConsumedBySeries != in.Manifest.SeriesID {
		t.Fatalf("consumed_by_series = %q, want the scored recovery series", in.Binding.ConsumedBySeries)
	}
	if err := ValidateOfficialScoreEligibility(in); err != nil {
		t.Fatalf("a recovered series with prior invalid evidence must stay scoreable: %v", err)
	}
	// The report binds exactly this series and this binding receipt.
	report := BuildOfficialScoreReport(in, m)
	report.ReportDigest, report.SealDigest = "rpt-report-digest", "rpt-report-seal"
	if report.SeriesID != in.Manifest.SeriesID || report.HoldoutBindingReceiptDigest != in.Binding.ReceiptDigest {
		t.Fatalf("recovered report binds the wrong series/binding: %+v", report)
	}
	if err := ValidateOfficialScoreReport(report, in, m); err != nil {
		t.Fatalf("recovered report must validate: %v", err)
	}
	// A ledger with two complete series can never back a report: only one
	// complete recovery series may be referenced.
	extra := HoldoutSeriesAttempt{
		SeriesID:                         "rpt-series-complete-earlier",
		SeriesManifestDigest:             "rpt-earlier-manifest",
		CandidateBindingDigestSelf:       in.Manifest.CandidateBindingDigest,
		PreHoldoutGreenTestReceiptDigest: "rpt-earlier-pre-holdout",
		CoreLegCompletionDigest:          "rpt-earlier-coreleg",
		StartedAt:                        "2026-09-02T00:00:00Z", State: "complete-fail",
		TerminalAt: "2026-09-02T09:00:00Z",
	}
	in.Binding.SeriesAttempts = append(in.Binding.SeriesAttempts, extra)
	err := ValidateOfficialScoreEligibility(in)
	if err == nil {
		t.Fatal("a ledger with two complete series must be rejected")
	}
	if !strings.Contains(err.Error(), "exactly one complete recovery series") {
		t.Fatalf("error %q must name the single-complete-series rule", err)
	}
}

func TestOfficialScoreReportNoMergedHeadline(t *testing.T) {
	banned := []string{
		"combined", "aggregate", "headline", "merged", "average", "blended",
		"fused", "pooled", "overallrate", "overallscore", "crosshostscore", "totalscore",
	}
	seen := map[reflect.Type]bool{}
	var walk func(ty reflect.Type, path string)
	walk = func(ty reflect.Type, path string) {
		if ty == nil || seen[ty] {
			return
		}
		seen[ty] = true
		switch ty.Kind() {
		case reflect.Struct:
			for i := 0; i < ty.NumField(); i++ {
				f := ty.Field(i)
				name := strings.ToLower(f.Name)
				tag := strings.ToLower(f.Tag.Get("json"))
				for _, b := range banned {
					if strings.Contains(name, b) || strings.Contains(tag, b) {
						t.Errorf("%s.%s carries a banned merged-headline name (%q)", path, f.Name, b)
					}
				}
				switch f.Type.Kind() {
				case reflect.Float32, reflect.Float64:
					t.Errorf("%s.%s is a float: no averaged/weighted rate may live in the report", path, f.Name)
				case reflect.Map:
					if k := f.Type.Elem().Kind(); k == reflect.Float32 || k == reflect.Float64 {
						t.Errorf("%s.%s is a float map: no averaged/weighted rate may live in the report", path, f.Name)
					}
				}
				walk(f.Type, path+"."+f.Name)
			}
		case reflect.Ptr, reflect.Slice, reflect.Array:
			walk(ty.Elem(), path+"[]")
		case reflect.Map:
			walk(ty.Elem(), path+"[v]")
		}
	}
	walk(reflect.TypeOf(OfficialScoreReport{}), "OfficialScoreReport")
	// Exactly two score families exist, both explicit; no third combined
	// family field may appear.
	ty := reflect.TypeOf(OfficialScoreReport{})
	familyFields := 0
	for i := 0; i < ty.NumField(); i++ {
		if ty.Field(i).Type == reflect.TypeOf([]HostScore{}) {
			familyFields++
		}
	}
	if familyFields != 2 {
		t.Fatalf("OfficialScoreReport carries %d []HostScore family fields, want exactly dev_regression + generalization", familyFields)
	}
}

func TestOfficialScoreReportSecurityAndDiagnostics(t *testing.T) {
	build := func(t *testing.T, m *ScoreMatrix) (*ScoreEligibilityInput, *OfficialScoreReport) {
		t.Helper()
		in, computed := rptBundle(t)
		if m != nil {
			computed = m
		}
		r := BuildOfficialScoreReport(in, computed)
		r.ReportDigest, r.SealDigest = "rpt-report-digest", "rpt-report-seal"
		return in, r
	}
	in, report := build(t, nil)
	if err := ValidateOfficialScoreReport(report, in, nil); err == nil ||
		!strings.Contains(fmt.Sprint(err), "no score matrix") {
		t.Fatalf("a report without its matrix must be rejected, got %v", err)
	}
	// Both green-test receipt digests are bound, alongside the series, plan,
	// protected, package, canary, snapshot, candidate and binding digests.
	if report.GreenTestReceiptDigests.SeriesPrepare != in.SeriesPrepare.ReceiptDigest {
		t.Error("report must bind the series-prepare green receipt digest")
	}
	if report.GreenTestReceiptDigests.PreHoldout != in.PreHoldout.ReceiptDigest {
		t.Error("report must bind the pre-holdout green receipt digest")
	}
	checks := map[string]string{
		"series_manifest_digest":             report.SeriesManifestDigest,
		"core_execution_plan_digest":         report.CoreExecutionPlanDigest,
		"protected_execution_receipt_digest": report.ProtectedExecutionReceiptDigest,
		"skill_package_validation_receipt":   report.SkillPackageValidationReceiptDigest,
		"skill_snapshot_digest":              report.SkillSnapshotDigest,
		"skill_snapshot_anchor_digest":       report.SkillSnapshotAnchorDigest,
		"candidate_binding_digest":           report.CandidateBindingDigest,
		"holdout_binding_receipt_digest":     report.HoldoutBindingReceiptDigest,
	}
	for name, got := range checks {
		if got == "" {
			t.Errorf("report field %s is empty", name)
		}
	}
	if len(report.WorkspaceCanaryReceiptDigests) != 3 {
		t.Errorf("report binds %d hosts' canaries, want 3", len(report.WorkspaceCanaryReceiptDigests))
	}
	// Families: three hosts each, all marked official, all gates present.
	if len(report.DevRegression) != 3 || len(report.Generalization) != 3 {
		t.Fatalf("families = %d dev / %d holdout hosts, want 3/3", len(report.DevRegression), len(report.Generalization))
	}
	for _, hs := range append(append([]HostScore{}, report.DevRegression...), report.Generalization...) {
		if !hs.Official {
			t.Errorf("%s/%s is not marked official", hs.Host, hs.Split)
		}
		if len(hs.Gates) != 5 {
			t.Errorf("%s/%s carries %d gates, want 5", hs.Host, hs.Split, len(hs.Gates))
		}
	}
	// Non-gating diagnostic cells with numerator/denominator/independent
	// case count/low-N, one per routed gate.
	if len(report.BiasDiagnostics) != 30 {
		t.Fatalf("bias cells = %d, want 6 host splits × 5 gates = 30", len(report.BiasDiagnostics))
	}
	for _, c := range report.BiasDiagnostics {
		if c.Label == "" || c.Denominator <= 0 || c.IndependentCaseCount <= 0 {
			t.Fatalf("malformed diagnostic cell %+v", c)
		}
		if c.LowN != (c.IndependentCaseCount < LowNThreshold) {
			t.Fatalf("cell %s low_n marker is wrong (%d cases)", c.Label, c.IndependentCaseCount)
		}
	}
	// The unmutated happy-path report validates.
	in2, report2 := build(t, nil)
	if err := ValidateOfficialScoreReport(report2, in2, rptMatrixFor(t, in2)); err != nil {
		t.Fatalf("happy-path report must validate: %v", err)
	}
	// Tamper table: every binding and every gating rule fails closed.
	rows := []struct {
		name   string
		mutate func(r *OfficialScoreReport)
		want   string
	}{
		{"series id tampered", func(r *OfficialScoreReport) { r.SeriesID = "rpt-other" }, "series_id"},
		{"series manifest digest tampered", func(r *OfficialScoreReport) { r.SeriesManifestDigest = "rpt-other" }, "series_manifest_digest"},
		{"core plan digest tampered", func(r *OfficialScoreReport) { r.CoreExecutionPlanDigest = "rpt-other" }, "core_execution_plan_digest"},
		{"protected receipt digest tampered", func(r *OfficialScoreReport) { r.ProtectedExecutionReceiptDigest = "rpt-other" }, "protected_execution_receipt_digest"},
		{"package receipt digest tampered", func(r *OfficialScoreReport) { r.SkillPackageValidationReceiptDigest = "rpt-other" }, "skill_package_validation_receipt_digest"},
		{"snapshot digest tampered", func(r *OfficialScoreReport) { r.SkillSnapshotDigest = "rpt-other" }, "skill_snapshot_digest"},
		{"candidate binding digest tampered", func(r *OfficialScoreReport) { r.CandidateBindingDigest = "rpt-other" }, "candidate_binding_digest"},
		{"holdout binding digest tampered", func(r *OfficialScoreReport) { r.HoldoutBindingReceiptDigest = "rpt-other" }, "holdout_binding_receipt_digest"},
		{"series-prepare green digest tampered", func(r *OfficialScoreReport) {
			r.GreenTestReceiptDigests.SeriesPrepare = "rpt-other"
		}, "green_test_receipt_digests.series_prepare"},
		{"pre-holdout green digest tampered", func(r *OfficialScoreReport) {
			r.GreenTestReceiptDigests.PreHoldout = "rpt-other"
		}, "green_test_receipt_digests.pre_holdout"},
		{"canary digest tampered", func(r *OfficialScoreReport) {
			// Copy the map first: mutating the shared manifest map would trip
			// the eligibility gate instead of the report binding check.
			cp := map[string]map[int]string{}
			for h, slots := range r.WorkspaceCanaryReceiptDigests {
				cp[h] = map[int]string{}
				for slot, d := range slots {
					cp[h][slot] = d
				}
			}
			cp[HostClaude][1] = "rpt-other"
			r.WorkspaceCanaryReceiptDigests = cp
		}, "workspace canary digest"},
		{"unsealed report", func(r *OfficialScoreReport) { r.SealDigest = "" }, "not sealed"},
		{"gating supplemental summary", func(r *OfficialScoreReport) {
			r.SupplementalCrossHost = &SupplementalCrossHost{NonGating: false, Note: "merged"}
		}, "non-gating"},
		{"diagnostic artifacts used", func(r *OfficialScoreReport) { r.DiagnosticArtifactsUsed = true }, "diagnostic_artifacts_used"},
		{"unknown verdict", func(r *OfficialScoreReport) { r.OverallVerdict = "mostly-pass" }, "overall_verdict"},
		{"missing host in a family", func(r *OfficialScoreReport) { r.DevRegression = r.DevRegression[:2] }, "family covers 2 hosts"},
		{"repeated host in a family", func(r *OfficialScoreReport) {
			r.DevRegression[2].Host = r.DevRegression[0].Host
		}, "repeats host"},
		{"gate disagreeing with the computed score", func(r *OfficialScoreReport) {
			r.Generalization[0].Gates[0].Passed = !r.Generalization[0].Gates[0].Passed
		}, "disagrees with the computed score"},
		{"dropped gate in a family", func(r *OfficialScoreReport) {
			r.Generalization[1].Gates = r.Generalization[1].Gates[:4]
		}, "carries 4 gates, want 5"},
		{"non-official family entry", func(r *OfficialScoreReport) {
			r.Generalization[2].Official = false
		}, "is not marked official"},
		{"wrong low-N marker", func(r *OfficialScoreReport) { r.BiasDiagnostics[0].LowN = true }, "low_n marker"},
		{"empty diagnostic cells", func(r *OfficialScoreReport) { r.BiasDiagnostics = nil }, "no non-gating diagnostic cells"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			inRow, _ := rptBundle(t)
			r := BuildOfficialScoreReport(inRow, rptMatrixFor(t, inRow))
			r.ReportDigest, r.SealDigest = "rpt-report-digest", "rpt-report-seal"
			row.mutate(r)
			err := ValidateOfficialScoreReport(r, inRow, rptMatrixFor(t, inRow))
			if err == nil {
				t.Fatalf("expected a rejection mentioning %q", row.want)
			}
			if !strings.Contains(err.Error(), row.want) {
				t.Fatalf("error %q does not mention %q", err, row.want)
			}
		})
	}
	// A genuinely failing host yields verdict fail, and the report may not
	// claim pass over it.
	specs := rptSpecs()
	trapPos := rptCaseIDs("rpt-h-trp", 8)
	failing := rptScore(t, specs, rptStates(specs, rptFailSpecs(trapPos[:1], HostOpenCode, map[int]bool{1: true, 2: true, 3: true}, CaseOutcomeFail)))
	inF, rF := build(t, failing)
	if rF.OverallVerdict != "fail" {
		t.Fatalf("verdict = %q, want fail", rF.OverallVerdict)
	}
	if err := ValidateOfficialScoreReport(rF, inF, failing); err != nil {
		t.Fatalf("an honest failing report must validate: %v", err)
	}
	rF.OverallVerdict = "pass"
	if err := ValidateOfficialScoreReport(rF, inF, failing); err == nil ||
		!strings.Contains(err.Error(), "does not follow from the score matrix") {
		t.Fatalf("a pass verdict over a failing host must be rejected, got %v", err)
	}
}

// rptMatrixFor recomputes the score matrix of a bundle's series.
func rptMatrixFor(t *testing.T, in *ScoreEligibilityInput) *ScoreMatrix {
	t.Helper()
	specs := rptSpecs()
	return rptScore(t, specs, rptStates(specs, nil))
}
