package main

// T047-T049 execution tests (runner_primary.go) — fully offline: every
// host/seed/dump/probe seam is injected, so no CLI, engine binary or network
// is touched. The only real filesystem work is the probe/canary/retirement
// tree itself, which is the thing under test.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- fixtures ----------

func tExecPlan() *CoreExecutionPlanReceipt {
	return &CoreExecutionPlanReceipt{
		SchemaVersion: 1, PlanID: "plan-exec", CoreManifestDigest: "core-manifest",
		RunnerRevision: "r-exec", RunnerDigest: "runner-exec", JudgeRuleDigest: "judge-exec",
		Hosts:               []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests: map[string]string{HostClaude: "tid-a", HostCodex: "tid-b", HostOpenCode: "tid-c"},
		TimeoutSeconds:      600, Concurrency: 2,
		CaseOrderSeeds:                        map[int]string{1: "seed-o1", 2: "seed-o2", 3: "seed-o3"},
		CoreBoundaryKind:                      BoundarySeparateUser,
		NormalizedCoreWorkerIdentitySetDigest: "norm-worker",
		NormalizedCoreBoundaryTemplateDigest:  "norm-boundary",
		NormalizedCoreExecutionTemplateDigest: "norm-exec",
		CreatedAt:                             "2026-09-03T00:00:00Z",
		ReceiptDigest:                         "plan-exec-digest",
		SealDigest:                            "plan-exec-seal",
	}
}

// tExecProbeSet is one slot's closed matrix: the six mandatory categories
// exactly once, own workspace readable, plus the per-run extras.
func tExecProbeSet(host string, slot int) []FormalAccessProbe {
	denied := func(k FormalProbeKind) FormalAccessProbe {
		return FormalAccessProbe{
			Kind: k, TargetDigest: "t-" + string(k), TargetAccessPolicyDigest: "p-" + string(k),
			ControllerTargetProofDigest: "proof-" + string(k), Expected: "denied", Outcome: "permission-denied",
		}
	}
	probes := []FormalAccessProbe{}
	for _, k := range []FormalProbeKind{
		FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
		FProbeAuditRead, FProbeAuthorStateRead,
	} {
		probes = append(probes, denied(k))
	}
	probes = append(probes, FormalAccessProbe{
		Kind: FProbeOwnWorkspaceRead, TargetDigest: "t-own-" + host, TargetAccessPolicyDigest: "p-own",
		Expected: "readable", Outcome: "readable",
	})
	probes = append(probes, denied(FProbeActiveSiblingRead), denied(FProbePriorCaseStateRead))
	retired := denied(FProbeRetiredWorkspaceRead)
	retired.Outcome = "not-found"
	return append(probes, retired)
}

func tExecProtected(plan *CoreExecutionPlanReceipt, templateDigests map[string]string) *ProtectedExecutionReceipt {
	probes := []ProtectedWorkerProbe{}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			probes = append(probes, ProtectedWorkerProbe{
				Host: h, WorkerSlot: slot,
				ChildIdentityDigest:     primaryChildIdentity(h, slot, templateDigests[h]),
				ExecutionTemplateDigest: templateDigests[h],
				AccessBoundaryDigest:    primaryAccessBoundary(BoundarySeparateUser, "iso-exec", h, slot),
				Probes:                  tExecProbeSet(h, slot),
			})
		}
	}
	return &ProtectedExecutionReceipt{
		BoundaryKind: BoundarySeparateUser, IsolationConfigDigest: "iso-exec",
		ProtectedRootDigest:                   "protected-root",
		AuthorReviewStateRootsDigest:          "author-roots",
		FormalStateRootsDigest:                "formal-roots",
		SplitStateAllocatorDigests:            map[string]string{MembershipCore172: "alloc-core", MembershipHoldout96: "alloc-holdout"},
		RequiredConcurrency:                   plan.Concurrency,
		IsolatedWorkerCapacity:                plan.Concurrency,
		WorkerIdentitySetDigest:               "worker-set",
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            "template-set",
		CoreExecutionPlanDigest:               plan.ReceiptDigest,
		WorkerProbes:                          probes,
		ProbeMatrixDigest:                     "probe-matrix",
		ProbedAt:                              "2026-09-03T00:00:00Z",
		ReceiptDigest:                         "protected-exec-digest",
	}
}

func tExecLane(dir string) CLIReviewConfig {
	return CLIReviewConfig{
		Lanes:          []string{HostClaude, HostCodex, HostOpenCode},
		ClaudeSettings: filepath.Join(dir, "settings.json"),
		CodexProvider:  "aq",
		CodexModel:     "qwen3.8-flash",
		OpenCodeModel:  "qwen3.8-flash",
	}
}

// tExecCase is a minimal non-trigger case: no engram call expected, so a
// silent child passes the deterministic judge.
func tExecCase(id string) *TriggerCaseV2 {
	prompt := "use the memory skill for " + id
	ed := "2026-08-01"
	return &TriggerCaseV2{
		ID: id, SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
		Module: "implicit-write-pos", Prompt: &prompt,
		SeedMemories:   []SeedMemory{{Name: "pref", Content: "prefers tea", EventDate: &ed}},
		WorkspaceFiles: []WorkspaceFile{{Path: "notes/todo.md", Content: "buy tea\n"}},
		Expect:         ExpectV2{Trigger: false},
	}
}

func tExecFixture(ids ...string) map[string]*TriggerCaseV2 {
	cases := map[string]*TriggerCaseV2{}
	for _, id := range ids {
		cases[id] = tExecCase(id)
	}
	return cases
}

// tExecChild is a deterministic silent child that records what it was handed.
func tExecChild(rec *[]string, mu *sync.Mutex) caseChildRunner {
	return func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		mu.Lock()
		*rec = append(*rec, tool+"\x00"+prompt+"\x00"+caseDir)
		mu.Unlock()
		if !strings.HasSuffix(caseDir, "workspace") {
			return nil, fmt.Errorf("child cwd %s is not the case workspace", caseDir)
		}
		return []byte("{\"type\":\"step_finish\"}\n"), nil
	}
}

type tExecSeeds struct {
	mu  sync.Mutex
	byC map[string][]SeedRenderResult
}

func (s *tExecSeeds) seed(caseDir string, c *TriggerCaseV2) ([]SeedRenderResult, error) {
	out := []SeedRenderResult{}
	for _, sm := range c.SeedMemories {
		_, rr, err := RenderSeedContent(sm)
		if err != nil {
			return nil, err
		}
		out = append(out, rr)
	}
	s.mu.Lock()
	s.byC[caseDir] = out
	s.mu.Unlock()
	return out, nil
}

func tExecDumper(caseDir string) (string, error) {
	return "store dump of " + caseDir + "\napi_key=sk-secretvaluethatislong\n", nil
}

// tExecPrimary wires one RunPrimary call with every production seam stubbed.
// child == nil selects the recording deterministic child.
func tExecPrimary(t *testing.T, series string, ids []string, cases map[string]*TriggerCaseV2, host, split string, ordinal, concurrency int, child caseChildRunner, obs *PrimaryRunObservation) (*PrimaryRunManifest, []CaseRunReceipt, error) {
	t.Helper()
	t.Cleanup(func() { _ = RestoreProbePermissions(series) })
	rec := []string{}
	var mu sync.Mutex
	seeds := &tExecSeeds{byC: map[string][]SeedRenderResult{}}
	lane := tExecLane(t.TempDir())
	opts := PrimaryRunOptions{
		SeriesID: "series-exec", Host: host, Split: split, Ordinal: ordinal, Concurrency: concurrency,
		Plan: tExecPlan(), Manifest: tExecManifest(), Protected: tExecProtected(tExecPlan(), tExecDigests(lane)),
		Lane:  lane,
		Cases: cases, CaseIDs: ids,
		RunRoot:     PrimaryRunRoot(series, host, split, ordinal),
		SeedRunner:  seeds.seed,
		StoreDumper: tExecDumper,
		ChildRunner: child,
		Observation: obs,
	}
	ours := child == nil
	if ours {
		opts.ChildRunner = tExecChild(&rec, &mu)
	}
	manifest, receipts, err := RunPrimary(opts)
	if err == nil && ours {
		if len(rec) != len(ids) {
			t.Fatalf("%d child attempts, want exactly one per case (%d)", len(rec), len(ids))
		}
		if len(seeds.byC) != len(ids) {
			t.Fatalf("seed receipts for %d case workspaces, want %d", len(seeds.byC), len(ids))
		}
	}
	return manifest, receipts, err
}

// tExecDigests resolves the frozen host template digests a fixture must
// bind. The claude template embeds its settings path, so the fixture and the
// run must derive both from the SAME lane configuration.
// tExecDigests resolves the lane's templates and projects them the way
// RunProtectedProbes/RunWorkspaceCanaries carry them downstream: one
// normalized digest per host, exactly what the prepared probes bind.
func tExecDigests(lane CLIReviewConfig) map[string]string {
	_, digests, err := PrimaryHostTemplates(lane)
	if err != nil {
		panic(err)
	}
	normalized, err := NormalizedCoreTemplateDigest(digests)
	if err != nil {
		panic(err)
	}
	return map[string]string{HostClaude: normalized, HostCodex: normalized, HostOpenCode: normalized}
}

func tExecManifest() *FormalSeriesManifest {
	return &FormalSeriesManifest{
		SeriesID: "series-exec", Purpose: PurposeOfficialDual, State: StateSealed,
		Hosts: []string{HostClaude, HostCodex, HostOpenCode}, RequiredOrdinals: []int{1, 2, 3},
		Concurrency:             2,
		CoreExecutionPlanDigest: "plan-exec-digest",
	}
}

func tExecRestore(t *testing.T, dirs ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = RestoreProbePermissions(d)
		}
	})
}

// ---------- T047: bounded workers, unique roots, full coverage ----------

func TestRunPrimarySmallFixtureCompletes(t *testing.T) {
	series := t.TempDir()
	ids := []string{"case-a", "case-b", "case-c", "case-d"}
	manifest, receipts, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err != nil {
		t.Fatalf("RunPrimary: %v", err)
	}
	if manifest.State != StateComplete || manifest.Mode != "primary" {
		t.Fatalf("manifest state %q mode %q, want complete/primary", manifest.State, manifest.Mode)
	}
	if manifest.ExpectedCaseCount != len(ids) || len(manifest.CaseIDs) != len(ids) || len(manifest.CaseOrder) != len(ids) {
		t.Fatalf("manifest coverage %d/%d/%d, want %d", manifest.ExpectedCaseCount, len(manifest.CaseIDs), len(manifest.CaseOrder), len(ids))
	}
	if manifest.RunDigest == "" || manifest.SealDigest == "" {
		t.Fatal("completed manifest is not sealed")
	}
	if manifest.CaseOrderDigest == manifest.CaseSetDigest {
		t.Fatal("case order digest equals the set digest: the ordinal order was lost")
	}
	if manifest.ToolProvenance.Host != HostClaude || manifest.ToolProvenance.InvocationTemplateDigest == "" {
		t.Fatalf("manifest provenance incomplete: %+v", manifest.ToolProvenance)
	}
	if len(receipts) != len(ids) {
		t.Fatalf("%d case receipts, want %d", len(receipts), len(ids))
	}
	passes := 0
	for _, r := range receipts {
		if r.Status != "pass" {
			t.Errorf("case %s status %q, want pass", r.CaseID, r.Status)
		} else {
			passes++
		}
		if r.AttemptCount != 1 {
			t.Errorf("case %s recorded %d attempts", r.CaseID, r.AttemptCount)
		}
		if !r.Verdict.Pass || r.CaseStateIsolationReceiptDigest == "" {
			t.Errorf("case %s verdict pass=%v isolation=%q", r.CaseID, r.Verdict.Pass, r.CaseStateIsolationReceiptDigest)
		}
		for _, p := range []string{r.RawEventsPath, r.NormalizedEventsPath, r.StoreDumpPath} {
			if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
				t.Errorf("case %s artifact %s missing/empty", r.CaseID, p)
			}
		}
		if r.WorkspaceDigest == "" || r.CasePayloadDigest == "" || r.StderrDigest == "" {
			t.Errorf("case %s receipt digests incomplete", r.CaseID)
		}
	}
	if passes != len(ids) {
		t.Fatalf("%d/%d cases passed", passes, len(ids))
	}
	// Secret filtering happens before persistence: the dumper's credential
	// must not survive, and the digest is the digest of the filtered bytes.
	dump, err := os.ReadFile(receipts[0].StoreDumpPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dump), "sk-secretvaluethatislong") {
		t.Fatal("store dump leaked the injected credential")
	}
	if !strings.Contains(string(dump), redactedMarker) {
		t.Fatal("store dump was not filtered")
	}
	if receipts[0].StoreDumpDigest != sha256Hex(dump) {
		t.Fatal("store dump digest is not the digest of the persisted bytes")
	}
	// The event-date seed receipts were persisted per case: the frozen
	// marker entered the rendered content, and the engine was never told it
	// populated structured metadata.
	seedBytes, err := os.ReadFile(filepath.Join(filepath.Dir(receipts[0].RawEventsPath), "seeds.json"))
	if err != nil {
		t.Fatalf("seeds.json: %v", err)
	}
	var seedReceipts []SeedRenderResult
	if err := json.Unmarshal(seedBytes, &seedReceipts); err != nil {
		t.Fatal(err)
	}
	if len(seedReceipts) != 1 {
		t.Fatalf("%d seed receipts, want 1", len(seedReceipts))
	}
	if seedReceipts[0].SourceEventDate == nil || *seedReceipts[0].SourceEventDate != "2026-08-01" {
		t.Fatalf("seed receipt lost the source event date: %+v", seedReceipts[0])
	}
	if seedReceipts[0].RenderedContentDigest != sha256Hex([]byte("[event_date=2026-08-01] prefers tea")) {
		t.Fatalf("seed receipt digest %s does not bind the frozen event_date marker", seedReceipts[0].RenderedContentDigest)
	}
	if seedReceipts[0].EngineEventDateSupported {
		t.Fatal("seed receipt claims the engine ingested a structured event date")
	}
}

func TestRunPrimaryStateRootsUniqueAndRetired(t *testing.T) {
	series := t.TempDir()
	ids := []string{"case-a", "case-b", "case-c"}
	manifest, receipts, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := PrimaryRunRoot(series, manifest.Host, manifest.Split, manifest.Ordinal)
	roots := map[string]bool{}
	for _, r := range receipts {
		iso, err := loadStateIsolation(filepath.Join(runRoot, "cases", r.CaseID, "state-isolation.json"))
		if err != nil {
			t.Fatal(err)
		}
		if roots[iso.FreshStateRootDigest] {
			t.Fatalf("state root of %s reused inside one run", r.CaseID)
		}
		roots[iso.FreshStateRootDigest] = true
		if iso.RetirementOrFinalDelete == "" {
			t.Fatalf("case %s has no retirement boundary", r.CaseID)
		}
		if fi, err := os.Stat(iso.RetirementOrFinalDelete); err != nil || fi.Mode().Perm() != 0 {
			t.Fatalf("retired state root of %s is not mode-0000 on disk", r.CaseID)
		}
		if iso.PriorStateProbe == nil || iso.RetiredWorkspaceProbe == nil {
			t.Fatalf("case %s lost the prior/retired probes", r.CaseID)
		}
		if iso.PriorStateProbe.Outcome != "permission-denied" || iso.RetiredWorkspaceProbe.Outcome != "permission-denied" {
			t.Fatalf("case %s probes observed %q/%q, want closed denials", r.CaseID, iso.PriorStateProbe.Outcome, iso.RetiredWorkspaceProbe.Outcome)
		}
		if iso.PriorStateProbe.ControllerTargetProofDigest == "" || iso.RetiredWorkspaceProbe.ControllerTargetProofDigest == "" {
			t.Fatal("denial probes without controller target proofs")
		}
		if iso.ResetMethod == "" || iso.ChildTeardown == "" {
			t.Fatalf("case %s teardown contract incomplete", r.CaseID)
		}
		if iso.StateAllocatorDigest == "" || iso.StateAllocatorDigest == iso.FreshStateRootDigest {
			t.Fatalf("case %s allocator digest missing or collapsed", r.CaseID)
		}
	}
	// A second leg of the same series must not reuse any of those roots, and
	// the series ledger carries both legs.
	ids2 := []string{"case-d", "case-e", "case-f"}
	if _, _, err := tExecPrimary(t, series, ids2, tExecFixture(ids2...), HostCodex, SplitDevRegression, 1, 2, nil, nil); err != nil {
		t.Fatalf("second leg: %v", err)
	}
	ledger, err := readStateRootLedger(filepath.Join(series, "runs", "state-root-ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger) != len(ids)+len(ids2) {
		t.Fatalf("series ledger carries %d state roots, want %d", len(ledger), len(ids)+len(ids2))
	}
	seen := map[string]string{}
	for _, e := range ledger {
		if prev, dup := seen[e.StateRootDigest]; dup {
			t.Fatalf("state root shared by %s and %s across legs", prev, e.CaseID)
		}
		seen[e.StateRootDigest] = e.CaseID
	}
}

func TestRunPrimaryLedgerRejectsReusedStateRoot(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ledgerPath := filepath.Join(series, "runs", "state-root-ledger.jsonl")
	dup := stateRootLedgerEntry{SeriesID: "series-exec", Host: HostClaude, Split: SplitDevRegression, Ordinal: 1, CaseID: "earlier", StateRootDigest: "already-used"}
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeStateRootLedger(ledgerPath, []stateRootLedgerEntry{dup, dup}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"case-a", "case-b"}
	if _, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil); err == nil {
		t.Fatal("a series ledger with a reused state root was accepted")
	}
}

func TestRunPrimaryBoundedWorkersObserveOverlap(t *testing.T) {
	series := t.TempDir()
	ids := []string{"case-a", "case-b", "case-c", "case-d"}
	var inFlight int32
	release := make(chan struct{})
	var once sync.Once
	child := func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		if atomic.AddInt32(&inFlight, 1) >= 2 {
			once.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		atomic.AddInt32(&inFlight, -1)
		return []byte("done"), nil
	}
	obs := &PrimaryRunObservation{}
	_, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, child, obs)
	if err != nil {
		t.Fatal(err)
	}
	if obs.MaxInFlight > 2 {
		t.Fatalf("observed %d cases in flight, above the sealed concurrency 2", obs.MaxInFlight)
	}
	if obs.MaxInFlight < 2 || !obs.Overlap {
		t.Fatalf("bounded pool never overlapped (max=%d overlap=%v)", obs.MaxInFlight, obs.Overlap)
	}
}

func TestRunPrimaryNeverOverwritesARunRoot(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b"}
	runRoot := PrimaryRunRoot(series, HostClaude, SplitDevRegression, 1)
	if err := os.MkdirAll(filepath.Join(runRoot, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("RunPrimary overwrote an existing run root: err=%v", err)
	}
}

func TestRunPrimaryFailsClosedOnMissingCase(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b", "case-ghost"}
	cases := tExecFixture("case-a", "case-b")
	_, receipts, err := tExecPrimary(t, series, ids, cases, HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "case-ghost") {
		t.Fatalf("missing case accepted: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(series, "runs", "claude-dev-regression-o1", "manifest.json")); err == nil {
		t.Fatal("an incomplete run wrote a primary manifest")
	}
	for _, r := range receipts {
		if r.Status == "pass" {
			t.Fatalf("case %s passed inside an incomplete run", r.CaseID)
		}
	}
}

func TestRunPrimaryRejectsWorkerProbeDrift(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b"}
	lane := tExecLane(t.TempDir())
	protected := tExecProtected(tExecPlan(), tExecDigests(lane))
	// One prepared slot's child identity no longer matches what the run
	// derives from the frozen host template.
	protected.WorkerProbes[0].ChildIdentityDigest = "drifted-child-identity"
	opts := PrimaryRunOptions{
		SeriesID: "series-exec", Host: HostClaude, Split: SplitDevRegression, Ordinal: 1, Concurrency: 2,
		Plan: tExecPlan(), Manifest: tExecManifest(), Protected: protected,
		Lane:  lane,
		Cases: tExecFixture(ids...), CaseIDs: ids,
		RunRoot:     PrimaryRunRoot(series, HostClaude, SplitDevRegression, 1),
		SeedRunner:  (&tExecSeeds{byC: map[string][]SeedRenderResult{}}).seed,
		StoreDumper: tExecDumper,
		ChildRunner: tExecChild(&[]string{}, &sync.Mutex{}),
	}
	if _, _, err := RunPrimary(opts); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("prepared-probe drift accepted: err=%v", err)
	}
}

func TestRunPrimaryRejectsConcurrencyBeyondPreparedCapacity(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b", "case-c"}
	_, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 3, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("run beyond the prepared worker capacity accepted: err=%v", err)
	}
}

func TestRunPrimaryCoreLegRefusesPreexistingHoldoutAllocator(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b"}
	runRoot := PrimaryRunRoot(series, HostClaude, SplitDevRegression, 1)
	if err := os.MkdirAll(SplitAllocatorRoots(primarySeriesBase(runRoot))[MembershipHoldout96], 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "holdout96") {
		t.Fatalf("core leg tolerated a pre-existing holdout allocator: err=%v", err)
	}
}

func TestRunPrimaryRejectsBadIdentityAndMismatchedSeries(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	ids := []string{"case-a", "case-b"}
	cases := tExecFixture(ids...)
	base := func() PrimaryRunOptions {
		lane := tExecLane(t.TempDir())
		return PrimaryRunOptions{
			SeriesID: "series-exec", Host: HostClaude, Split: SplitDevRegression, Ordinal: 1, Concurrency: 2,
			Plan: tExecPlan(), Manifest: tExecManifest(), Protected: tExecProtected(tExecPlan(), tExecDigests(lane)),
			Lane:  lane,
			Cases: cases, CaseIDs: ids,
			RunRoot:     PrimaryRunRoot(series, HostClaude, SplitDevRegression, 1),
			SeedRunner:  (&tExecSeeds{byC: map[string][]SeedRenderResult{}}).seed,
			StoreDumper: tExecDumper,
			ChildRunner: tExecChild(&[]string{}, &sync.Mutex{}),
		}
	}
	badOrdinal := base()
	badOrdinal.Ordinal = 4
	if _, _, err := RunPrimary(badOrdinal); err == nil {
		t.Fatal("ordinal 4 accepted")
	}
	badHost := base()
	badHost.Host = "cursor"
	if _, _, err := RunPrimary(badHost); err == nil {
		t.Fatal("unknown host accepted")
	}
	noManifest := base()
	noManifest.Manifest = nil
	if _, _, err := RunPrimary(noManifest); err == nil {
		t.Fatal("run without the prepared series manifest accepted")
	}
	wrongSeries := base()
	wrongSeries.Manifest = &FormalSeriesManifest{
		SeriesID: "other-series", Hosts: []string{HostClaude}, Concurrency: 2,
		CoreExecutionPlanDigest: "plan-exec-digest",
	}
	if _, _, err := RunPrimary(wrongSeries); err == nil {
		t.Fatal("series manifest of another series accepted")
	}
	noProtected := base()
	noProtected.Protected = nil
	if _, _, err := RunPrimary(noProtected); err == nil {
		t.Fatal("run without a prepared protected receipt accepted")
	}
	emptySet := base()
	emptySet.CaseIDs = nil
	if _, _, err := RunPrimary(emptySet); err == nil {
		t.Fatal("run with no required case set accepted")
	}
}

func TestRunPrimaryTerminalChildErrorIsRecordedNotFatal(t *testing.T) {
	series := t.TempDir()
	ids := []string{"case-a", "case-b"}
	child := func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		return nil, fmt.Errorf("cli crashed: api_key=sk-reallysecretvalue123 leaked")
	}
	manifest, receipts, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, child, nil)
	if err != nil {
		t.Fatalf("a terminal child error must be a case status, not a run failure: %v", err)
	}
	if manifest.State != StateComplete {
		t.Fatalf("manifest state %q, want complete", manifest.State)
	}
	for _, r := range receipts {
		if r.Status != "runner-error" {
			t.Fatalf("case %s status %q, want runner-error", r.CaseID, r.Status)
		}
		if r.AttemptCount != 1 {
			t.Fatalf("case %s retried (%d attempts)", r.CaseID, r.AttemptCount)
		}
		raw, err := os.ReadFile(r.RawEventsPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "sk-reallysecretvalue123") {
			t.Fatalf("case %s raw events leaked the credential", r.CaseID)
		}
		stderr, err := os.ReadFile(filepath.Join(filepath.Dir(r.RawEventsPath), "stderr.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(stderr), "sk-reallysecretvalue123") {
			t.Fatalf("case %s stderr leaked the credential", r.CaseID)
		}
	}
}

// ---------- T048/T049: redaction, allocators, ordering, coverage ----------

func TestRedactSecretsFiltersClosedShapes(t *testing.T) {
	in := []byte("ok line\n" +
		"OPENAI_API_KEY=sk-abc123def456ghi7\n" +
		"\"api_key\": \"sk-plainsecret99\"\n" +
		"Authorization: Bearer abc.def.ghi12345\n" +
		"github token: ghp_" + strings.Repeat("a", 30) + "\n" +
		"password=hunter2supersecret\n")
	s := string(RedactSecrets(in))
	for _, leaked := range []string{
		"sk-abc123def456ghi7", "sk-plainsecret99", "abc.def.ghi12345",
		"ghp_" + strings.Repeat("a", 30), "hunter2supersecret",
	} {
		if strings.Contains(s, leaked) {
			t.Fatalf("secret survived redaction (%s): %s", leaked, s)
		}
	}
	if !strings.HasPrefix(s, "ok line\n") {
		t.Fatalf("benign content was damaged: %s", s)
	}
	if strings.Count(s, redactedMarker) < 5 {
		t.Fatalf("expected every secret shape to be replaced, got %s", s)
	}
	if n := len(RedactSecrets(nil)); n != 0 {
		t.Fatalf("empty input changed: %d bytes", n)
	}
}

func TestValidateSplitAllocatorsDisjointAndCoreBeforeHoldout(t *testing.T) {
	series := t.TempDir()
	tExecRestore(t, series)
	roots := SplitAllocatorRoots(series)
	if err := ValidateSplitAllocators(roots, SplitDevRegression); err != nil {
		t.Fatalf("core leg rejected: %v", err)
	}
	if err := ValidateSplitAllocators(roots, SplitHoldout); err != nil {
		t.Fatalf("holdout leg rejected: %v", err)
	}
	// Once the holdout allocator exists, no further core leg may start.
	if err := os.MkdirAll(roots[MembershipHoldout96], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSplitAllocators(roots, SplitDevRegression); err == nil {
		t.Fatal("core leg started after the holdout allocator existed")
	}
	if err := ValidateSplitAllocators(roots, SplitHoldout); err != nil {
		t.Fatalf("holdout leg must tolerate its own allocator: %v", err)
	}
	overlap := map[string]string{
		MembershipCore172:   filepath.Join(series, "a"),
		MembershipHoldout96: filepath.Join(series, "a", "nested"),
	}
	if err := ValidateSplitAllocators(overlap, SplitDevRegression); err == nil {
		t.Fatal("nested allocators accepted as disjoint")
	}
	single := map[string]string{MembershipCore172: filepath.Join(series, "a")}
	if err := ValidateSplitAllocators(single, SplitDevRegression); err == nil {
		t.Fatal("a single allocator accepted for a two-split series")
	}
	if err := ValidateSplitAllocators(roots, MembershipDevExt); err == nil {
		t.Fatal("a non-scored split accepted as an executing split")
	}
}

func TestPrimaryCaseOrderIsDeterministicPerSeed(t *testing.T) {
	ids := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	a := PrimaryCaseOrder(ids, "seed-1")
	b := PrimaryCaseOrder(ids, "seed-1")
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("same seed produced different orders")
	}
	if len(a) != len(ids) {
		t.Fatalf("order lost cases: %d", len(a))
	}
	seen := map[string]bool{}
	for _, id := range a {
		seen[id] = true
	}
	if len(seen) != len(ids) {
		t.Fatal("order duplicated or dropped a case")
	}
	if fmt.Sprint(a) == fmt.Sprint(PrimaryCaseOrder(ids, "seed-2")) {
		t.Fatal("different ordinals share one order — the seed is decorative")
	}
}

func TestValidatePrimaryCaseSetExactSplitCoverage(t *testing.T) {
	if err := ValidatePrimaryCaseSet(SplitDevRegression, make([]string, 172)); err != nil {
		t.Fatalf("172-case core rejected: %v", err)
	}
	if err := ValidatePrimaryCaseSet(SplitHoldout, make([]string, 96)); err != nil {
		t.Fatalf("96-case holdout rejected: %v", err)
	}
	for _, bad := range [][]string{make([]string, 171), make([]string, 97), {}} {
		if err := ValidatePrimaryCaseSet(SplitDevRegression, bad); err == nil {
			t.Fatalf("partial slice of %d cases accepted", len(bad))
		}
	}
	if err := ValidatePrimaryCaseSet(MembershipDevExt, make([]string, 10)); err == nil {
		t.Fatal("a non-scored split accepted as a formal split")
	}
}

// ---------- T049: protected probe matrix ----------

func TestRunProtectedProbesRealFilesystemValidReceipt(t *testing.T) {
	root := t.TempDir()
	tExecRestore(t, root)
	plan := tExecPlan()
	receipt, err := RunProtectedProbes(tExecProbeOptions(t, root, plan, nil))
	if err != nil {
		t.Fatalf("RunProtectedProbes: %v", err)
	}
	if err := ValidateProtectedExecutionReceipt(receipt, plan); err != nil {
		t.Fatalf("produced receipt fails its own validation: %v", err)
	}
	if receipt.ReceiptDigest == "" || receipt.ProbeMatrixDigest == "" || receipt.ProbedAt == "" {
		t.Fatal("receipt identity incomplete")
	}
	if receipt.CoreExecutionPlanDigest != plan.ReceiptDigest {
		t.Fatal("receipt does not bind the plan")
	}
	if receipt.NormalizedCoreWorkerIdentitySetDigest != plan.NormalizedCoreWorkerIdentitySetDigest {
		t.Fatal("receipt worker identity template drifts from the plan")
	}
	if receipt.SplitStateAllocatorDigests[MembershipCore172] == receipt.SplitStateAllocatorDigests[MembershipHoldout96] {
		t.Fatal("split allocators collapsed into one digest")
	}
	if receipt.AuthorReviewStateRootsDigest == receipt.FormalStateRootsDigest {
		t.Fatal("formal state roots reuse the author/review roots")
	}
	slots := map[string]int{}
	kindHits := map[FormalProbeKind]int{}
	for _, wp := range receipt.WorkerProbes {
		if wp.ExecutionTemplateDigest == "" || wp.ChildIdentityDigest == "" || wp.AccessBoundaryDigest == "" {
			t.Fatalf("%s/%d probe identity incomplete", wp.Host, wp.WorkerSlot)
		}
		slots[wp.Host+"\x00"+fmt.Sprint(wp.WorkerSlot)]++
		for _, p := range wp.Probes {
			kindHits[p.Kind]++
			if p.Outcome == "" || p.TargetDigest == "" || p.TargetAccessPolicyDigest == "" {
				t.Fatalf("%s/%d probe %s is not closed", wp.Host, wp.WorkerSlot, p.Kind)
			}
			if p.Expected == "denied" && p.Outcome != "permission-denied" && p.Outcome != "not-found" {
				t.Fatalf("%s/%d probe %s observed %q on a forbidden target", wp.Host, wp.WorkerSlot, p.Kind, p.Outcome)
			}
			if p.Kind == FProbeOwnWorkspaceRead && p.Outcome != "readable" {
				t.Fatalf("%s/%d cannot read its own workspace", wp.Host, wp.WorkerSlot)
			}
		}
	}
	for _, k := range []FormalProbeKind{
		FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
		FProbeAuditRead, FProbeAuthorStateRead, FProbeOwnWorkspaceRead,
		FProbeActiveSiblingRead, FProbePriorCaseStateRead, FProbeRetiredWorkspaceRead,
	} {
		if kindHits[k] != plan.Concurrency*len(plan.Hosts) {
			t.Fatalf("probe %s appears %d times, want one per slot (%d)", k, kindHits[k], plan.Concurrency*len(plan.Hosts))
		}
	}
	for key, n := range slots {
		if n != 1 {
			t.Fatalf("worker probe %s appears %d times", key, n)
		}
	}
}

func tExecProbeOptions(t *testing.T, root string, plan *CoreExecutionPlanReceipt, probe ProbeFunc) ProtectedProbeOptions {
	return ProtectedProbeOptions{
		Plan: plan, Lane: tExecLane(t.TempDir()), Boundary: BoundarySeparateUser,
		IsolationConfigDigest: "iso-real",
		Root:                  filepath.Join(root, "probe"),
		ProtectedRoot:         filepath.Join(root, "probe", "protected"),
		AuditRoot:             filepath.Join(root, "probe", "audit"),
		AuthorStateRoot:       filepath.Join(root, "probe", "author-state"),
		FormalStateRoots:      []string{filepath.Join(root, "probe", "formal-home"), filepath.Join(root, "probe", "formal-cache")},
		SplitAllocatorRoots:   SplitAllocatorRoots(root),
		WorkerRoot:            filepath.Join(root, "workers"),
		Capacity:              plan.Concurrency,
		Probe:                 probe,
	}
}

func TestRunProtectedProbesFailsClosed(t *testing.T) {
	root := t.TempDir()
	tExecRestore(t, root)
	plan := tExecPlan()
	first := tExecProbeOptions(t, root, plan, nil)
	if _, err := RunProtectedProbes(first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	again := tExecProbeOptions(t, root, plan, nil)
	if _, err := RunProtectedProbes(again); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("probe root reuse accepted: %v", err)
	}
	small := tExecProbeOptions(t, root, plan, nil)
	small.Capacity = plan.Concurrency - 1
	small.Root = filepath.Join(root, "probe-small")
	if _, err := RunProtectedProbes(small); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("insufficient worker capacity accepted: %v", err)
	}
	badBoundary := tExecProbeOptions(t, root, plan, nil)
	badBoundary.Boundary = "vibes"
	badBoundary.Root = filepath.Join(root, "probe-boundary")
	if _, err := RunProtectedProbes(badBoundary); err == nil {
		t.Fatal("invalid boundary accepted")
	}
	badAlloc := tExecProbeOptions(t, root, plan, nil)
	badAlloc.SplitAllocatorRoots = map[string]string{MembershipCore172: filepath.Join(root, "x"), MembershipHoldout96: filepath.Join(root, "x")}
	badAlloc.Root = filepath.Join(root, "probe-alloc")
	if _, err := RunProtectedProbes(badAlloc); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping allocators accepted: %v", err)
	}
	// A probe that observes a forbidden target as readable fails the matrix.
	leaky := tExecProbeOptions(t, root, plan, func(string) string { return ProbeReadable })
	leaky.Root = filepath.Join(root, "probe-leaky")
	if _, err := RunProtectedProbes(leaky); err == nil {
		t.Fatal("a readable forbidden target was accepted")
	}
	// A probe that reports the own workspace unreadable fails too.
	blind := tExecProbeOptions(t, root, plan, func(target string) string {
		if strings.Contains(target, "own-workspace") {
			return ProbeDenied
		}
		return ProbeFilesystem(target)
	})
	blind.Root = filepath.Join(root, "probe-blind")
	if _, err := RunProtectedProbes(blind); err == nil {
		t.Fatal("an unreadable own workspace was accepted")
	}
}

// ---------- T049: workspace canaries ----------

func TestRunWorkspaceCanariesPass(t *testing.T) {
	root := t.TempDir()
	plan := tExecPlan()
	lane := tExecLane(t.TempDir())
	protected := tExecProtected(plan, tExecDigests(lane))
	child := func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		if !strings.HasPrefix(canaryDir, root) {
			return "", nil, fmt.Errorf("canary dir %s outside the canary root", canaryDir)
		}
		b, err := os.ReadFile(filepath.Join(canaryDir, stagedRel))
		if err != nil {
			return "", nil, err
		}
		return canaryDir, b, nil
	}
	receipts, err := RunWorkspaceCanaries(CanaryOptions{
		SeriesID: "series-exec", SkillDigest: "skill-digest", Plan: plan, Protected: protected,
		ToolIdentityDigests: plan.ToolIdentityDigests, Lane: lane,
		Root: filepath.Join(root, "canaries"), Child: child,
	})
	if err != nil {
		t.Fatalf("RunWorkspaceCanaries: %v", err)
	}
	want := plan.Concurrency * len(plan.Hosts)
	if len(receipts) != want {
		t.Fatalf("%d canary receipts, want one per host × slot (%d)", len(receipts), want)
	}
	seen := map[string]bool{}
	for _, r := range receipts {
		if r.Status != "pass" {
			t.Fatalf("canary %s/%d status %q", r.Host, r.WorkerSlot, r.Status)
		}
		if r.CanaryWorkspaceDigest != r.ObservedCWDDigest || r.ExpectedFileDigest != r.ObservedFileDigest {
			t.Fatalf("canary %s/%d observation mismatch", r.Host, r.WorkerSlot)
		}
		if r.ReceiptDigest == "" || r.ChildIdentityDigest == "" || r.AccessBoundaryDigest == "" {
			t.Fatalf("canary %s/%d identity incomplete", r.Host, r.WorkerSlot)
		}
		if err := ValidateWorkspaceCanary(&r, "series-exec", "skill-digest",
			plan.ToolIdentityDigests[r.Host], r.ExecutionTemplateDigest, r.WorkerSlot); err != nil {
			t.Fatalf("canary %s/%d fails validation: %v", r.Host, r.WorkerSlot, err)
		}
		key := fmt.Sprintf("%s/%d", r.Host, r.WorkerSlot)
		if seen[key] {
			t.Fatalf("duplicate canary for %s", key)
		}
		seen[key] = true
	}
}

func TestRunWorkspaceCanariesRejectMismatch(t *testing.T) {
	root := t.TempDir()
	plan := tExecPlan()
	lane := tExecLane(t.TempDir())
	protected := tExecProtected(plan, tExecDigests(lane))
	canaryOpts := func(child CanaryChildRunner, dir string) CanaryOptions {
		return CanaryOptions{
			SeriesID: "series-exec", SkillDigest: "skill-digest", Plan: plan, Protected: protected,
			ToolIdentityDigests: plan.ToolIdentityDigests, Lane: lane,
			Root: filepath.Join(dir, "canaries"), Child: child,
		}
	}
	// The child reads the staged file but reports a foreign cwd.
	lying := func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		b, err := os.ReadFile(filepath.Join(canaryDir, stagedRel))
		if err != nil {
			return "", nil, err
		}
		return filepath.Join(root, "somewhere-else"), b, nil
	}
	receipts, err := RunWorkspaceCanaries(canaryOpts(lying, root))
	if err == nil {
		t.Fatal("a canary cwd mismatch was accepted")
	}
	if len(receipts) == 0 || receipts[0].Status != "fail" {
		t.Fatalf("expected a recorded failing receipt, got %+v", receipts)
	}
	if fi, err := os.Stat(filepath.Join(root, "canaries")); err != nil || !fi.IsDir() {
		t.Fatal("the failed canary run left no artifacts behind")
	}
	// A child that returns the wrong bytes fails too.
	tampered := func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		return canaryDir, []byte("tampered"), nil
	}
	root2 := t.TempDir()
	if _, err := RunWorkspaceCanaries(canaryOpts(tampered, root2)); err == nil {
		t.Fatal("a canary content mismatch was accepted")
	}
	// No child runner at all fails closed.
	root3 := t.TempDir()
	opts := canaryOpts(nil, root3)
	if _, err := RunWorkspaceCanaries(opts); err == nil {
		t.Fatal("canaries without a child runner accepted")
	}
	// A reused canary root fails closed.
	root4 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root4, "canaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RunWorkspaceCanaries(canaryOpts(func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		b, _ := os.ReadFile(filepath.Join(canaryDir, stagedRel))
		return canaryDir, b, nil
	}, root4)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("reused canary root accepted: %v", err)
	}
}

// ---------- host invocation parity ----------

func TestPrimaryHostTemplatesAllThreeHosts(t *testing.T) {
	templates, digests, err := PrimaryHostTemplates(tExecLane(t.TempDir()))
	if err != nil {
		t.Fatalf("parity: %v", err)
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		if templates[h].ID == "" || len(templates[h].Args) == 0 {
			t.Errorf("host %s lost its invocation template", h)
		}
		if digests[h] == "" {
			t.Errorf("host %s lost its template digest", h)
		}
	}
	if _, _, err := PrimaryHostTemplates(CLIReviewConfig{ClaudeSettings: "x", CodexProvider: "p", CodexModel: "m"}); err == nil ||
		!strings.Contains(err.Error(), HostOpenCode) {
		t.Fatalf("incomplete lane configuration accepted: %v", err)
	}
}

// ---------- isolation receipt binds the prepared slot probe ----------

func TestPrimaryIsolationReceiptBindsPreparedSlotProbe(t *testing.T) {
	series := t.TempDir()
	ids := []string{"case-a", "case-b", "case-c", "case-d"}
	manifest, _, err := tExecPrimary(t, series, ids, tExecFixture(ids...), HostClaude, SplitDevRegression, 1, 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	protected := tExecProtected(tExecPlan(), tExecDigests(tExecLane(t.TempDir())))
	boundary := map[string]string{}
	for _, p := range protected.WorkerProbes {
		boundary[p.Host+"\x00"+fmt.Sprint(p.WorkerSlot)] = p.AccessBoundaryDigest
	}
	runRoot := PrimaryRunRoot(series, manifest.Host, manifest.Split, manifest.Ordinal)
	slots := map[int]int{}
	for _, id := range manifest.CaseIDs {
		raw, err := os.ReadFile(filepath.Join(runRoot, "cases", id, "state-isolation.json"))
		if err != nil {
			t.Fatal(err)
		}
		var iso CaseStateIsolationReceipt
		if err := json.Unmarshal(raw, &iso); err != nil {
			t.Fatal(err)
		}
		if iso.WorkerSlot < 1 || iso.WorkerSlot > 2 {
			t.Fatalf("case %s ran on slot %d", id, iso.WorkerSlot)
		}
		slots[iso.WorkerSlot]++
		if iso.ExecutionTemplateDigest == "" {
			t.Fatalf("case %s lost its execution template digest", id)
		}
		// The child identity is a pure function of (host, slot, template):
		// it must re-derive from the receipt itself.
		if want := primaryChildIdentity(iso.Host, iso.WorkerSlot, iso.ExecutionTemplateDigest); iso.ChildIdentityDigest != want {
			t.Fatalf("case %s child identity %s does not re-derive from its own template (%s)", id, iso.ChildIdentityDigest, want)
		}
		if want := boundary[iso.Host+"\x00"+fmt.Sprint(iso.WorkerSlot)]; iso.AccessBoundaryDigest != want {
			t.Fatalf("case %s access boundary drifts from the prepared %s/%d probe", id, iso.Host, iso.WorkerSlot)
		}
		if iso.PreparedWorkerProbeDigest == "" || iso.ProtectedExecutionReceiptDigest != protected.ReceiptDigest {
			t.Fatalf("case %s does not reference its prepared probe", id)
		}
	}
	if slots[1] == 0 || slots[2] == 0 {
		t.Fatalf("both prepared slots must carry cases, saw %v", slots)
	}
}
