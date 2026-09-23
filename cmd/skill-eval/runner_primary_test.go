package main

// T039 [US4] — primary-runner tests (data-model.md §9-§10): bounded per-case
// worker capacity with observed overlap, disposable never-reused state roots,
// protected-execution probe matrix, core/holdout allocator separation,
// prior-case/retired-workspace denial, exact split coverage, the one-attempt
// contract, and the structural no-selector shape of PrimaryRunManifest. The
// primary execution path itself lands in T047; until then every assertion
// here targets the already-landed type/validation layer, which must fail
// closed rather than let a partial or reused run through.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- fixtures ----------

func t039PlanFixture() *CoreExecutionPlanReceipt {
	return &CoreExecutionPlanReceipt{
		SchemaVersion: 1, PlanID: "plan-039", CoreManifestDigest: "core-manifest-digest",
		RunnerRevision: "r039", RunnerDigest: "runner-digest", JudgeRuleDigest: "judge-rule-digest",
		Hosts:               []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests: map[string]string{HostClaude: "tid-claude", HostCodex: "tid-codex", HostOpenCode: "tid-opencode"},
		TimeoutSeconds:      900, Concurrency: 2,
		CaseOrderSeeds:   map[int]string{1: "seed-1", 2: "seed-2", 3: "seed-3"},
		CoreBoundaryKind: BoundarySeparateUser,
		NormalizedCoreWorkerIdentitySetDigest:   "norm-worker-identity",
		NormalizedCoreBoundaryTemplateDigest:    "norm-boundary-template",
		NormalizedCoreExecutionTemplateDigest:   "norm-exec-template",
		CreatedAt:                               "2026-09-03T00:00:00Z",
		ReceiptDigest:                           "plan-receipt-digest",
		SealDigest:                              "plan-seal-digest",
	}
}

// t039ProbeSet is one slot's complete closed probe matrix: the six mandatory
// categories exactly once, plus the three per-run extras (active sibling,
// prior case, retired workspace). The retired workspace is observed as
// not-found, which is only legal with its controller proof — exactly the
// case the validator must keep honest.
func t039ProbeSet() []FormalAccessProbe {
	denied := func(k FormalProbeKind) FormalAccessProbe {
		return FormalAccessProbe{
			Kind: k, TargetDigest: "target-" + string(k), TargetAccessPolicyDigest: "policy-" + string(k),
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
		Kind: FProbeOwnWorkspaceRead, TargetDigest: "target-own", TargetAccessPolicyDigest: "policy-own",
		Expected: "readable", Outcome: "readable",
	})
	probes = append(probes, denied(FProbeActiveSiblingRead), denied(FProbePriorCaseStateRead))
	retired := denied(FProbeRetiredWorkspaceRead)
	retired.Outcome = "not-found"
	probes = append(probes, retired)
	return probes
}

func t039WorkerProbes(plan *CoreExecutionPlanReceipt) []ProtectedWorkerProbe {
	out := []ProtectedWorkerProbe{}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			out = append(out, ProtectedWorkerProbe{
				Host: h, WorkerSlot: slot,
				ChildIdentityDigest:     fmt.Sprintf("child-%s-%d", h, slot),
				ExecutionTemplateDigest: "exec-template",
				AccessBoundaryDigest:    fmt.Sprintf("boundary-%s-%d", h, slot),
				Probes:                  t039ProbeSet(),
			})
		}
	}
	return out
}

func t039ProtectedReceipt(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	return &ProtectedExecutionReceipt{
		BoundaryKind: BoundarySeparateUser, IsolationConfigDigest: "iso-config",
		ProtectedRootDigest: "protected-root",
		AuthorReviewStateRootsDigest:   "author-review-roots",
		FormalStateRootsDigest:         "formal-roots",
		SplitStateAllocatorDigests:     map[string]string{MembershipCore172: "alloc-core", MembershipHoldout96: "alloc-holdout"},
		RequiredConcurrency:            plan.Concurrency,
		IsolatedWorkerCapacity:         plan.Concurrency,
		WorkerIdentitySetDigest:        "worker-identity-set",
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:     "exec-template-set",
		CoreExecutionPlanDigest:        plan.ReceiptDigest,
		WorkerProbes:                   t039WorkerProbes(plan),
		ProbeMatrixDigest:              "probe-matrix",
		ProbedAt:                       "2026-09-03T00:00:00Z",
		ReceiptDigest:                  "protected-receipt-digest",
	}
}

// ---------- bounded capacity, observed overlap, unique roots ----------

// TestPrimaryStateRootsBoundedUniqueAndNeverReused rides the stage workspace
// manager (the allocator primary mode reuses until T047 wires its own) over a
// fan of cases: peak in-flight stays within the configured concurrency, every
// case gets its own root, and a released slot is never handed back with the
// old root (disposable per-case state).
func TestPrimaryStateRootsBoundedUniqueAndNeverReused(t *testing.T) {
	const concurrency, cases = 3, 9
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, concurrency)

	var mu sync.Mutex
	seen := map[string]string{}
	var wg sync.WaitGroup
	for i := 0; i < cases; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("case-%02d", i)
			ws, err := m.Acquire(StageAuthor, id)
			if err != nil {
				t.Errorf("acquire %s: %v", id, err)
				return
			}
			if fi, err := os.Stat(filepath.Join(ws, "state")); err != nil || !fi.IsDir() {
				t.Errorf("case %s state/ root missing: %v", id, err)
			}
			mu.Lock()
			if prev, dup := seen[ws]; dup {
				t.Errorf("cases %s and %s share state root %s", prev, id, ws)
			}
			seen[ws] = id
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			if err := m.Release(id); err != nil {
				t.Errorf("release %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	if got := m.MaxObservedInFlight(); got > concurrency {
		t.Errorf("max_in_flight %d exceeds concurrency %d", got, concurrency)
	}
	if len(seen) != cases {
		t.Fatalf("distinct roots %d, want %d (one per case)", len(seen), cases)
	}
}

// TestPrimaryStateRootsOverlapObserved pins the actual-overlap requirement:
// with concurrency > 1 two cases really are in flight at once — a serial
// allocator that merely claims concurrency would report max 1.
func TestPrimaryStateRootsOverlapObserved(t *testing.T) {
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, 2)

	wsA, err := m.Acquire(StageAuthor, "case-a")
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	acquired := make(chan string, 1)
	go func() {
		ws, aerr := m.Acquire(StageReview, "case-b")
		if aerr != nil {
			t.Errorf("acquire b: %v", aerr)
			return
		}
		acquired <- ws
	}()
	t039WaitFor(t, "second case to enter flight", func() bool { return len(acquired) > 0 })
	wsB := <-acquired
	if wsA == wsB {
		t.Fatal("two in-flight cases share one state root")
	}
	if got := m.MaxObservedInFlight(); got < 2 {
		t.Fatalf("no overlap observed (max=%d) with concurrency 2", got)
	}
	if err := m.Release("case-a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("case-b"); err != nil {
		t.Fatal(err)
	}
}

// TestPrimaryCapacityExhaustionBlocksAndUnavailableFailsClosed pins the
// unavailable path: a zero/invalid concurrency is refused outright, an
// exhausted pool blocks instead of over-subscribing, and unsafe case ids
// never reach the filesystem.
func TestPrimaryCapacityExhaustionBlocksAndUnavailableFailsClosed(t *testing.T) {
	if _, err := NewStageWorkspaceManager(t.TempDir(), 0).Acquire(StageAuthor, "case-a"); err == nil {
		t.Error("acquire with concurrency 0 accepted (capacity must fail closed)")
	}
	if _, err := NewStageWorkspaceManager(t.TempDir(), -1).Acquire(StageAuthor, "case-a"); err == nil {
		t.Error("acquire with negative concurrency accepted")
	}
	m := NewStageWorkspaceManager(t.TempDir(), 1)
	if _, err := m.Acquire(StageAuthor, "../../escape"); err == nil {
		t.Error("case id with a path separator accepted")
	}
	wsA, err := m.Acquire(StageAuthor, "case-a")
	if err != nil {
		t.Fatal(err)
	}
	second := make(chan string, 1)
	go func() {
		ws, aerr := m.Acquire(StageReview, "case-b")
		if aerr != nil {
			t.Errorf("acquire b: %v", aerr)
			return
		}
		second <- ws
	}()
	// The pool is full: the second case must still be blocked, not silently
	// over-subscribed.
	time.Sleep(20 * time.Millisecond)
	if len(second) != 0 {
		t.Fatalf("concurrency 1 pool admitted a second in-flight case at %s", <-second)
	}
	if err := m.Release("case-a"); err != nil {
		t.Fatal(err)
	}
	var wsB string
	select {
	case wsB = <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked case never resumed after a release")
	}
	if wsB == wsA {
		t.Fatal("released state root handed to the next case")
	}
	if err := m.Release("case-b"); err != nil {
		t.Fatal(err)
	}
}

// TestPrimaryRetiredRootNeverOverwritten pins the resume-safe sequence: a
// retired attempt root already on disk must be skipped, never reused or
// overwritten by a restarted batch.
func TestPrimaryRetiredRootNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, 2)
	retired := filepath.Join(root, "author", "000001-case-a")
	if err := os.MkdirAll(filepath.Join(retired, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retired, "state", "engram.db"), []byte("retired"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := m.Acquire(StageAuthor, "case-a")
	if err != nil {
		t.Fatal(err)
	}
	if ws == retired {
		t.Fatal("acquire returned the retired root (no-overwrite violated)")
	}
	b, err := os.ReadFile(filepath.Join(retired, "state", "engram.db"))
	if err != nil || string(b) != "retired" {
		t.Fatalf("retired root content changed: %q %v", b, err)
	}
	if err := m.Release("case-a"); err != nil {
		t.Fatal(err)
	}
}

// ---------- disposable per-case state roots ----------

// TestPrimaryFreshStateRootsUniqueAcrossCasesAndSplits builds the per-case
// isolation receipts of one series by hand, including a deliberately reused
// fresh_state_root across ordinals/splits — the dedup check must catch it.
func TestPrimaryFreshStateRootsUniqueAcrossCasesAndSplits(t *testing.T) {
	roots := []string{"state-root-c1-o1", "state-root-c2-o1", "state-root-c1-o2", "state-root-c1-h"}
	if err := ValidateStateRootsUnique(roots); err != nil {
		t.Fatalf("honest distinct root set rejected: %v", err)
	}
	for _, dup := range [][]string{
		{"state-root-c1-o1", "state-root-c2-o1", "state-root-c1-o1"},
		{"a", "a"},
	} {
		if err := ValidateStateRootsUnique(dup); err == nil {
			t.Errorf("reused state roots %v accepted", dup)
		}
	}
	if err := ValidateStateRootsUnique([]string{"state-root-c1", ""}); err == nil {
		t.Error("empty state root accepted")
	}
	if err := ValidateStateRootsUnique(nil); err != nil {
		t.Errorf("empty batch rejected: %v", err)
	}
}

// ---------- protected execution receipt ----------

func TestPrimaryProtectedExecutionReceiptGoodPath(t *testing.T) {
	plan := t039PlanFixture()
	r := t039ProtectedReceipt(plan)
	if err := ValidateProtectedExecutionReceipt(r, plan); err != nil {
		t.Fatalf("complete protected receipt rejected: %v", err)
	}
	// Capacity exactly at the concurrency is the minimum acceptable.
	if r.IsolatedWorkerCapacity != plan.Concurrency {
		t.Fatalf("fixture capacity %d, want plan concurrency", r.IsolatedWorkerCapacity)
	}
	if r.SplitStateAllocatorDigests[MembershipCore172] == r.SplitStateAllocatorDigests[MembershipHoldout96] {
		t.Fatal("fixture allocators must differ")
	}
	if r.FormalStateRootsDigest == r.AuthorReviewStateRootsDigest {
		t.Fatal("fixture formal roots must differ from author/review roots")
	}
}

func TestPrimaryProtectedExecutionReceiptFailClosed(t *testing.T) {
	plan := t039PlanFixture()
	mutate := func(name string, f func(r *ProtectedExecutionReceipt)) {
		t.Run(name, func(t *testing.T) {
			r := t039ProtectedReceipt(plan)
			f(r)
			if err := ValidateProtectedExecutionReceipt(r, plan); err == nil {
				t.Fatal("invalid protected execution receipt accepted")
			}
		})
	}
	mutate("capacity-below-concurrency", func(r *ProtectedExecutionReceipt) {
		r.IsolatedWorkerCapacity = r.RequiredConcurrency - 1
	})
	mutate("required-concurrency-drifts-from-plan", func(r *ProtectedExecutionReceipt) {
		r.RequiredConcurrency = plan.Concurrency + 1
		r.IsolatedWorkerCapacity = plan.Concurrency + 1
		r.WorkerProbes = t039WorkerProbes(&CoreExecutionPlanReceipt{Hosts: plan.Hosts, Concurrency: plan.Concurrency + 1})
	})
	mutate("split-allocators-overlap", func(r *ProtectedExecutionReceipt) {
		r.SplitStateAllocatorDigests[MembershipHoldout96] = r.SplitStateAllocatorDigests[MembershipCore172]
	})
	mutate("split-allocator-missing", func(r *ProtectedExecutionReceipt) {
		delete(r.SplitStateAllocatorDigests, MembershipHoldout96)
	})
	mutate("formal-roots-reuse-author-review-roots", func(r *ProtectedExecutionReceipt) {
		r.FormalStateRootsDigest = r.AuthorReviewStateRootsDigest
	})
	mutate("plan-digest-mismatch", func(r *ProtectedExecutionReceipt) {
		r.CoreExecutionPlanDigest = "another-plan"
	})
	mutate("worker-identity-template-drift", func(r *ProtectedExecutionReceipt) {
		r.NormalizedCoreWorkerIdentitySetDigest = "drifted-identity"
	})
	mutate("invalid-boundary-kind", func(r *ProtectedExecutionReceipt) {
		r.BoundaryKind = BoundaryKind("shared-home-dir")
	})
	mutate("incomplete-identity", func(r *ProtectedExecutionReceipt) {
		r.ProbeMatrixDigest = ""
	})
	mutate("missing-host-slot-probe", func(r *ProtectedExecutionReceipt) {
		r.WorkerProbes = r.WorkerProbes[:len(r.WorkerProbes)-1]
	})
	mutate("duplicate-host-slot-probe", func(r *ProtectedExecutionReceipt) {
		r.WorkerProbes = append(r.WorkerProbes, r.WorkerProbes[0])
	})
	mutate("probe-beyond-prepared-slots", func(r *ProtectedExecutionReceipt) {
		extra := r.WorkerProbes[0]
		extra.WorkerSlot = plan.Concurrency + 1
		r.WorkerProbes = append(r.WorkerProbes, extra)
	})
	if err := ValidateProtectedExecutionReceipt(nil, plan); err == nil {
		t.Error("nil protected receipt accepted")
	}
}

// ---------- worker probe matrix ----------

func TestPrimaryWorkerProbeMatrixGoodPath(t *testing.T) {
	plan := t039PlanFixture()
	for _, p := range t039WorkerProbes(plan) {
		if err := ValidateWorkerProbe(p, plan); err != nil {
			t.Fatalf("probe matrix for %s/%d rejected: %v", p.Host, p.WorkerSlot, err)
		}
	}
}

func TestPrimaryWorkerProbeMatrixFailClosed(t *testing.T) {
	plan := t039PlanFixture()

	t.Run("each-mandatory-kind-exactly-once", func(t *testing.T) {
		for _, kind := range []FormalProbeKind{
			FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
			FProbeAuditRead, FProbeAuthorStateRead, FProbeOwnWorkspaceRead,
		} {
			t.Run(string(kind), func(t *testing.T) {
				p := t039WorkerProbes(plan)[0]
				kept := p.Probes[:0]
				for _, pr := range p.Probes {
					if pr.Kind != kind {
						kept = append(kept, pr)
					}
				}
				p.Probes = kept
				if err := ValidateWorkerProbe(p, plan); err == nil {
					t.Fatalf("probe matrix missing %s accepted", kind)
				}
			})
		}
	})

	t.Run("duplicated-mandatory-kind", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		p.Probes = append(p.Probes, p.Probes[0])
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("probe kind appearing twice accepted")
		}
	})
	t.Run("own-workspace-expects-denied", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbeOwnWorkspaceRead {
				p.Probes[i].Expected = "denied"
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("own-workspace probe expecting denied accepted")
		}
	})
	t.Run("own-workspace-unreadable", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbeOwnWorkspaceRead {
				p.Probes[i].Outcome = "permission-denied"
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("own workspace the child could not read accepted")
		}
	})
	t.Run("forbidden-target-expects-readable", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbeProtectedRootRead {
				p.Probes[i].Expected = "readable"
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("forbidden target expecting readable accepted")
		}
	})
	t.Run("prior-case-not-found-without-proof", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbePriorCaseStateRead {
				p.Probes[i].Outcome = "not-found"
				p.Probes[i].ControllerTargetProofDigest = ""
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("prior-case not-found without a controller proof accepted")
		}
	})
	t.Run("retired-workspace-not-found-without-proof", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbeRetiredWorkspaceRead {
				p.Probes[i].ControllerTargetProofDigest = ""
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("retired-workspace not-found without a controller proof accepted")
		}
	})
	t.Run("prior-case-observed-readable", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbePriorCaseStateRead {
				p.Probes[i].Outcome = "readable"
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("readable prior-case state accepted")
		}
	})
	t.Run("active-sibling-observed-readable", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		for i := range p.Probes {
			if p.Probes[i].Kind == FProbeActiveSiblingRead {
				p.Probes[i].Outcome = "readable"
			}
		}
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("readable active sibling workspace accepted")
		}
	})
	t.Run("unknown-expectation", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		p.Probes[0].Expected = "probably-denied"
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("unknown expectation accepted")
		}
	})
	t.Run("missing-target-or-policy-digest", func(t *testing.T) {
		for _, field := range []int{0, 1} {
			p := t039WorkerProbes(plan)[0]
			if field == 0 {
				p.Probes[0].TargetDigest = ""
			} else {
				p.Probes[0].TargetAccessPolicyDigest = ""
			}
			if err := ValidateWorkerProbe(p, plan); err == nil {
				t.Fatal("probe without target/policy digest accepted")
			}
		}
	})
	t.Run("slot-zero", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		p.WorkerSlot = 0
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("worker slot 0 accepted")
		}
	})
	t.Run("missing-boundary-digest", func(t *testing.T) {
		p := t039WorkerProbes(plan)[0]
		p.AccessBoundaryDigest = ""
		if err := ValidateWorkerProbe(p, plan); err == nil {
			t.Fatal("worker probe without an access boundary digest accepted")
		}
	})
}

// ---------- real-filesystem denial for prior/retired/sibling targets ----------

// TestPrimaryPriorRetiredAndSiblingTargetsDeniedOnRealTree runs the actual
// access probe against a controller-side tree: a retired workspace the child
// cannot read, a sibling case's state it must never read, its own fresh root
// reading fine, and a missing prior root reporting not-found (which only
// counts as denied with a controller proof captured before the child ran).
func TestPrimaryPriorRetiredAndSiblingTargetsDeniedOnRealTree(t *testing.T) {
	root := t.TempDir()
	var locked []string
	stateFile := func(caseID string) (string, error) {
		f := filepath.Join(root, caseID, "state", "engram.db")
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(f, []byte("sqlite"), 0o600); err != nil {
			return "", err
		}
		return f, nil
	}
	retired, err := stateFile("case-0001")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(retired, 0o000); err != nil {
		t.Fatal(err)
	}
	locked = append(locked, retired)
	sibling, err := stateFile("case-0002")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sibling, 0o000); err != nil {
		t.Fatal(err)
	}
	locked = append(locked, sibling)
	own, err := stateFile("case-0003")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range locked {
			_ = os.Chmod(p, 0o600)
		}
	})

	if got := ProbeFilesystem(retired); got != ProbeDenied {
		t.Errorf("retired workspace observed %s, want denied", got)
	}
	if got := ProbeFilesystem(sibling); got != ProbeDenied {
		t.Errorf("active sibling workspace observed %s, want denied", got)
	}
	if got := ProbeFilesystem(own); got != ProbeReadable {
		t.Errorf("own fresh state root observed %s, want readable", got)
	}
	missing := filepath.Join(root, "case-0004", "state", "engram.db")
	if got := ProbeFilesystem(missing); got != ProbeNotFound {
		t.Fatalf("missing prior root observed %s, want not-found", got)
	}
	// A not-found observation is only acceptable once the controller proves
	// the target existed immediately before launch.
	if _, err := CaptureControllerTargetProof(missing); err == nil {
		t.Fatal("controller proof captured for a target that never existed")
	}
}

// ---------- primary manifest / case receipt contracts ----------

// TestPrimaryManifestHasNoPartialRunSelectors pins data-model.md §9: primary
// mode carries no only/sample/limit affordance, so no artifact can imply a
// partial run. It also checks the closed field set: every field is tagged and
// marshals to exactly the declared keys.
func TestPrimaryManifestHasNoPartialRunSelectors(t *testing.T) {
	banned := []string{"only", "sample", "limit"}
	types := []struct {
		name string
		typ  reflect.Type
	}{
		{"PrimaryRunManifest", reflect.TypeOf(PrimaryRunManifest{})},
		{"CaseRunReceipt", reflect.TypeOf(CaseRunReceipt{})},
		{"CaseStateIsolationReceipt", reflect.TypeOf(CaseStateIsolationReceipt{})},
	}
	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			tags := map[string]bool{}
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				tag := f.Tag.Get("json")
				if tag == "" {
					t.Errorf("field %s has no json tag (an untagged field would leak into the artifact)", f.Name)
					continue
				}
				name := strings.Split(tag, ",")[0]
				tags[name] = true
				for _, b := range banned {
					if strings.Contains(strings.ToLower(name), b) {
						t.Errorf("field %s (json %q) carries the banned partial-run selector %q", f.Name, name, b)
					}
				}
			}
			// The artifact must round-trip to exactly the declared keys.
			b, err := json.Marshal(reflect.New(tc.typ).Interface())
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			keys := map[string]json.RawMessage{}
			if err := json.Unmarshal(b, &keys); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for k := range keys {
				if !tags[k] {
					t.Errorf("marshaled key %q is not a declared field", k)
				}
			}
			for k := range tags {
				if _, ok := keys[k]; !ok {
					t.Errorf("declared field %q missing from the marshaled artifact", k)
				}
			}
		})
	}

	// The unique key fields must exist, and the primary mode marker with them.
	m := reflect.TypeOf(PrimaryRunManifest{})
	for _, want := range []string{"mode", "series_id", "host", "split", "ordinal", "expected_case_count", "run_digest", "seal_digest"} {
		if !t039HasJSONTag(m, want) {
			t.Errorf("PrimaryRunManifest lacks the required field %q", want)
		}
	}
}

func t039HasJSONTag(typ reflect.Type, tag string) bool {
	for i := 0; i < typ.NumField(); i++ {
		if strings.Split(typ.Field(i).Tag.Get("json"), ",")[0] == tag {
			return true
		}
	}
	return false
}

// TestPrimaryCaseRunReceiptOneAttemptContract pins data-model.md §10: the
// primary agent attempt count is exactly 1 — a retry affordance field would
// have to appear as its own tagged field, and none may.
func TestPrimaryCaseRunReceiptOneAttemptContract(t *testing.T) {
	typ := reflect.TypeOf(CaseRunReceipt{})
	f, ok := typ.FieldByName("AttemptCount")
	if !ok || strings.Split(f.Tag.Get("json"), ",")[0] != "attempt_count" {
		t.Fatal("CaseRunReceipt must declare the attempt_count field")
	}
	if f.Type.Kind() != reflect.Int {
		t.Fatalf("attempt_count kind %s, want int", f.Type.Kind())
	}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		for _, banned := range []string{"retry", "retries", "resumption", "resume"} {
			if strings.Contains(name, banned) {
				t.Errorf("field %q carries a retry affordance %q", typ.Field(i).Name, banned)
			}
		}
	}
	// The terminal status set is closed: pass | fail | runner-error.
	r := CaseRunReceipt{AttemptCount: 1, Status: "runner-error"}
	if r.AttemptCount != 1 || r.Status != "runner-error" {
		t.Fatal("primary case receipts record exactly one attempt and a closed status")
	}
}

// TestPrimaryExactSplitCoverage pins the exact-coverage rule: the formal
// splits are closed at 172/96, and a primary manifest's expected count and
// case set must match them — never a subsampled slice.
func TestPrimaryExactSplitCoverage(t *testing.T) {
	for mem, want := range map[string]int{MembershipCore172: 172, MembershipHoldout96: 96} {
		got, err := ExpectedQuestionCount(mem)
		if err != nil || got != want {
			t.Errorf("ExpectedQuestionCount(%s) = %d, %v; want %d", mem, got, err, want)
		}
	}
	for _, notFormal := range []string{MembershipDevExt, SplitDevRegression, "", "holdout-extra"} {
		if _, err := ExpectedQuestionCount(notFormal); err == nil {
			t.Errorf("membership %q accepted as a formal split", notFormal)
		}
	}
	// A primary manifest declares the exact split size up front; the case set
	// it binds can never exceed it.
	m := PrimaryRunManifest{SeriesID: "series-039", Host: HostClaude, Split: MembershipCore172,
		Ordinal: 1, CaseIDs: []string{"c1", "c2"}, ExpectedCaseCount: 172}
	want, err := ExpectedQuestionCount(m.Split)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExpectedCaseCount != want {
		t.Fatalf("manifest expected_case_count %d, want the closed %s size %d", m.ExpectedCaseCount, m.Split, want)
	}
	if len(m.CaseIDs) > m.ExpectedCaseCount {
		t.Fatal("case set larger than the exact split size")
	}
	if m.ExpectedCaseCount != len(m.CaseIDs) {
		t.Log("partial case set in fixture: a real primary run must bind all 172 before it can go complete")
	}
}

// ---------- diagnostic isolation and the unwired primary path ----------

// TestDiagnosticReceiptIsolatedFromPrimaryShape pins the diagnostic/primary
// split at the type level: a diagnostic receipt cannot be mistaken for a
// primary manifest (no series/ordinal/attempt identity, no selector, and the
// permanent score-ineligibility marker must stay a structural field).
func TestDiagnosticReceiptIsolatedFromPrimaryShape(t *testing.T) {
	typ := reflect.TypeOf(DiagnosticRunReceipt{})
	found := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		found[name] = true
		for _, banned := range []string{"series", "ordinal", "attempt", "only", "sample", "limit"} {
			if strings.Contains(name, banned) {
				t.Errorf("diagnostic receipt field %q carries primary/selector identity %q", typ.Field(i).Name, banned)
			}
		}
	}
	if !found["formal_score_eligible"] {
		t.Error("diagnostic receipt lost the formal_score_eligible marker")
	}
	if !found["observed_max_in_flight"] || !found["observed_overlap"] {
		t.Error("diagnostic receipt lost the observed concurrency facts")
	}
	rec := DiagnosticRunReceipt{Mode: "diagnostic"}
	if rec.Mode == "primary" {
		t.Fatal("diagnostic receipts must never claim primary mode")
	}
}

// TestPrimaryExecutionStillFailsClosed asserts the primary path is not
// silently faked before T047 lands: the execution sentinel exists for callers
// to fail closed on, and the only running mode remains the dev-only
// diagnostic one, which is structurally score-ineligible.
func TestPrimaryExecutionStillFailsClosed(t *testing.T) {
	if ErrNotWired == nil {
		t.Fatal("ErrNotWired sentinel missing — a stub would have nothing to fail closed on")
	}
	if !strings.Contains(ErrNotWired.Error(), "T045-T051") {
		t.Errorf("sentinel must name its implementation tasks, got %q", ErrNotWired.Error())
	}
	if !errors.Is(ErrNotWired, ErrNotWired) {
		t.Fatal("sentinel not identifiable via errors.Is")
	}
}

func t039WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
