package main

// T041 (048 US4): receipt/rejudge contract tests. Every fixture here is
// fictional and no real host CLI is spawned. The per-case isolation validator
// is a test-local statement of data-model.md §10: the production validator is
// T045-T051 work (series.go fails closed via ErrNotWired until then), and
// these tests pin the closed semantics it must land — bind a valid prepared
// worker slot/probe, reject unknown slots and identity/template/boundary
// drift, keep reset disposable, keep prior/retired targets unreadable, and
// never let a core-leg case touch the protected holdout allocator.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rcptDigest(seed string) string { return sha256Hex([]byte(seed)) }

// ---------- fictional prepared-stage fixtures ----------

// receiptPlanFixture is a sealed core execution plan over the three hosts.
func receiptPlanFixture(concurrency int) *CoreExecutionPlanReceipt {
	hosts := []string{HostClaude, HostCodex, HostOpenCode}
	tools := map[string]string{}
	for _, h := range hosts {
		tools[h] = rcptDigest("tool-identity-" + h)
	}
	return &CoreExecutionPlanReceipt{
		SchemaVersion:                         1,
		PlanID:                                "plan-fx",
		CoreManifestDigest:                    rcptDigest("core172-manifest"),
		RunnerRevision:                        "runner-fx-rev",
		RunnerDigest:                          rcptDigest("runner-fx"),
		JudgeRuleDigest:                       rcptDigest("judge-rule-fx"),
		Hosts:                                 hosts,
		ToolIdentityDigests:                   tools,
		TimeoutSeconds:                        900,
		Concurrency:                           concurrency,
		CaseOrderSeeds:                        map[int]string{1: rcptDigest("seed-1"), 2: rcptDigest("seed-2"), 3: rcptDigest("seed-3")},
		CoreBoundaryKind:                      BoundarySeparateUser,
		NormalizedCoreWorkerIdentitySetDigest: rcptDigest("norm-worker-identity"),
		NormalizedCoreBoundaryTemplateDigest:  rcptDigest("norm-boundary-template"),
		NormalizedCoreExecutionTemplateDigest: rcptDigest("norm-exec-template"),
		CreatedAt:                             "2026-09-03T00:00:00Z",
		ReceiptDigest:                         rcptDigest("plan-receipt"),
		SealDigest:                            rcptDigest("plan-seal"),
	}
}

// preparedSlotProbe is one prepared host × slot probe matrix that satisfies
// the closed ValidateWorkerProbe rules: every off-limits target denied, the
// slot's own workspace readable.
func preparedSlotProbe(host string, slot int) *ProtectedWorkerProbe {
	denied := func(kind FormalProbeKind, seed string, notFound bool) FormalAccessProbe {
		p := FormalAccessProbe{
			Kind:                     kind,
			TargetDigest:             rcptDigest("target-" + seed),
			TargetAccessPolicyDigest: rcptDigest("policy-" + seed),
			Expected:                 "denied",
			Outcome:                  "permission-denied",
		}
		if notFound {
			p.Outcome = "not-found"
			p.ControllerTargetProofDigest = rcptDigest("proof-" + seed)
		}
		return p
	}
	return &ProtectedWorkerProbe{
		Host:                    host,
		WorkerSlot:              slot,
		ChildIdentityDigest:     rcptDigest(fmt.Sprintf("child-%s-%d", host, slot)),
		ExecutionTemplateDigest: rcptDigest(fmt.Sprintf("template-%s-%d", host, slot)),
		AccessBoundaryDigest:    rcptDigest(fmt.Sprintf("boundary-%s-%d", host, slot)),
		Probes: []FormalAccessProbe{
			denied(FProbeProtectedRootTraverse, "traverse", false),
			denied(FProbeProtectedRootList, "list", true),
			denied(FProbeProtectedRootRead, "read", false),
			denied(FProbeAuditRead, "audit", false),
			denied(FProbeAuthorStateRead, "state", false),
			{
				Kind:                     FProbeOwnWorkspaceRead,
				TargetDigest:             rcptDigest("target-own-" + host),
				TargetAccessPolicyDigest: rcptDigest("policy-own-" + host),
				Expected:                 "readable",
				Outcome:                  "readable",
			},
		},
	}
}

// preparedSlotProbeDigest is the canonical digest a case receipt must cite.
func preparedSlotProbeDigest(p *ProtectedWorkerProbe) string {
	d, err := CanonicalSHA256(p)
	if err != nil {
		panic(err)
	}
	return d
}

// receiptProtectedFixture returns a protected execution receipt that passes
// the closed validator for the plan: full host × slot matrix, disjoint split
// allocators, formal roots disjoint from the author/review roots.
func receiptProtectedFixture(plan *CoreExecutionPlanReceipt) *ProtectedExecutionReceipt {
	prot := &ProtectedExecutionReceipt{
		BoundaryKind:                 plan.CoreBoundaryKind,
		IsolationConfigDigest:        rcptDigest("isolation-config"),
		ProtectedRootDigest:          rcptDigest("protected-root"),
		AuthorReviewStateRootsDigest: rcptDigest("author-review-roots"),
		FormalStateRootsDigest:       rcptDigest("formal-roots"),
		SplitStateAllocatorDigests: map[string]string{
			MembershipCore172:   rcptDigest("allocator-core172"),
			MembershipHoldout96: rcptDigest("allocator-holdout96"),
		},
		RequiredConcurrency:                   plan.Concurrency,
		IsolatedWorkerCapacity:                plan.Concurrency,
		WorkerIdentitySetDigest:               rcptDigest("worker-identity-set"),
		NormalizedCoreWorkerIdentitySetDigest: plan.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            rcptDigest("execution-template-set"),
		CoreExecutionPlanDigest:               plan.ReceiptDigest,
		ProbedAt:                              "2026-09-03T00:00:00Z",
		ReceiptDigest:                         rcptDigest("protected-receipt"),
	}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			prot.WorkerProbes = append(prot.WorkerProbes, *preparedSlotProbe(h, slot))
		}
	}
	d, err := CanonicalSHA256(prot.WorkerProbes)
	if err != nil {
		panic(err)
	}
	prot.ProbeMatrixDigest = d
	return prot
}

// ---------- test-local CaseStateIsolationReceipt validator ----------

// caseIsolationContext carries the prepared facts a per-case receipt must bind.
type caseIsolationContext struct {
	SeriesID    string
	Host        string
	Split       string
	Ordinal     int
	Purpose     SeriesPurpose
	Concurrency int

	// official-dual bindings; must stay null under dev-comparison.
	ProtectedReceiptDigest string
	PreparedProbe          *ProtectedWorkerProbe
	PreparedProbeDigest    string

	// dev-comparison bindings: the normalized core-plan digests.
	CoreIdentityDigest string
	CoreTemplateDigest string
	CoreBoundaryDigest string

	// The case's split allocator and the protected holdout allocator a
	// core-leg case must never claim.
	SplitAllocatorDigest   string
	HoldoutAllocatorDigest string

	// SeenStateRoots maps an already-used state root to its owner.
	SeenStateRoots map[string]string
}

func officialIsolationContext(plan *CoreExecutionPlanReceipt, prot *ProtectedExecutionReceipt, host string, slot int) *caseIsolationContext {
	probe := preparedSlotProbe(host, slot)
	return &caseIsolationContext{
		SeriesID: "series-fx", Host: host, Split: MembershipCore172, Ordinal: 1,
		Purpose:                PurposeOfficialDual,
		Concurrency:            plan.Concurrency,
		ProtectedReceiptDigest: prot.ReceiptDigest,
		PreparedProbe:          probe,
		PreparedProbeDigest:    preparedSlotProbeDigest(probe),
		SplitAllocatorDigest:   prot.SplitStateAllocatorDigests[MembershipCore172],
		HoldoutAllocatorDigest: prot.SplitStateAllocatorDigests[MembershipHoldout96],
		SeenStateRoots:         map[string]string{prot.AuthorReviewStateRootsDigest: "author/review roots"},
	}
}

func deniedCaseProbe(kind FormalProbeKind, seed string) *FormalAccessProbe {
	return &FormalAccessProbe{
		Kind:                     kind,
		TargetDigest:             rcptDigest("target-" + seed),
		TargetAccessPolicyDigest: rcptDigest("policy-" + seed),
		Expected:                 "denied",
		Outcome:                  "permission-denied",
	}
}

// validCaseIsolationReceipt builds a receipt consistent with ctx; `prior`
// marks a case that has predecessor state/workspace to prove unreadable.
func validCaseIsolationReceipt(ctx *caseIsolationContext, caseID string, prior bool) *CaseStateIsolationReceipt {
	slot, identity, template, boundary := 1, ctx.CoreIdentityDigest, ctx.CoreTemplateDigest, ctx.CoreBoundaryDigest
	if ctx.PreparedProbe != nil { // official-dual binds the prepared slot probe
		slot = ctx.PreparedProbe.WorkerSlot
		identity = ctx.PreparedProbe.ChildIdentityDigest
		template = ctx.PreparedProbe.ExecutionTemplateDigest
		boundary = ctx.PreparedProbe.AccessBoundaryDigest
	}
	r := &CaseStateIsolationReceipt{
		SeriesID:                ctx.SeriesID,
		Host:                    ctx.Host,
		Split:                   ctx.Split,
		Ordinal:                 ctx.Ordinal,
		CaseID:                  caseID,
		WorkerSlot:              slot,
		ChildIdentityDigest:     identity,
		ExecutionTemplateDigest: template,
		AccessBoundaryDigest:    boundary,
		FreshStateRootDigest:    rcptDigest("fresh-root-" + caseID),
		StateAllocatorDigest:    ctx.SplitAllocatorDigest,
		ResetMethod:             "disposable",
		ChildTeardown:           "child-destroyed",
		RetirementOrFinalDelete: "controller-final-delete",
		ReceiptDigest:           rcptDigest("isolation-receipt-" + caseID),
	}
	if ctx.Purpose == PurposeOfficialDual {
		r.ProtectedExecutionReceiptDigest = ctx.ProtectedReceiptDigest
		r.PreparedWorkerProbeDigest = ctx.PreparedProbeDigest
	}
	if prior {
		r.PriorStateProbe = deniedCaseProbe(FProbePriorCaseStateRead, "prior-"+caseID)
		r.RetiredWorkspaceProbe = deniedCaseProbe(FProbeRetiredWorkspaceRead, "retired-"+caseID)
	}
	return r
}

// validateCaseStateIsolation is the test-local statement of the data-model.md
// §10 CaseStateIsolationReceipt rules.
func validateCaseStateIsolation(r *CaseStateIsolationReceipt, ctx *caseIsolationContext) error {
	if r == nil {
		return errors.New("nil case state isolation receipt")
	}
	if ctx == nil {
		return errors.New("nil isolation context")
	}
	if r.SeriesID != ctx.SeriesID || r.Host != ctx.Host || r.Split != ctx.Split || r.Ordinal != ctx.Ordinal {
		return fmt.Errorf("receipt identity %s/%s/%s/%d does not match the run it claims", r.SeriesID, r.Host, r.Split, r.Ordinal)
	}
	if r.CaseID == "" {
		return errors.New("case_id empty")
	}
	if r.WorkerSlot < 1 || r.WorkerSlot > ctx.Concurrency {
		return fmt.Errorf("worker_slot %d outside prepared 1..%d", r.WorkerSlot, ctx.Concurrency)
	}
	if r.ChildIdentityDigest == "" || r.ExecutionTemplateDigest == "" || r.AccessBoundaryDigest == "" {
		return errors.New("child identity/template/boundary incomplete")
	}
	switch ctx.Purpose {
	case PurposeOfficialDual:
		if r.ProtectedExecutionReceiptDigest != ctx.ProtectedReceiptDigest {
			return errors.New("protected_execution_receipt_digest must equal the series manifest reference")
		}
		if r.PreparedWorkerProbeDigest == "" {
			return errors.New("prepared_worker_probe_digest missing: the case must cite its prepared slot probe")
		}
		if r.PreparedWorkerProbeDigest != ctx.PreparedProbeDigest {
			return errors.New("prepared_worker_probe_digest does not bind the prepared probe for this host/slot")
		}
		if ctx.PreparedProbe == nil {
			return errors.New("no prepared probe recorded for this host/slot")
		}
		if r.WorkerSlot != ctx.PreparedProbe.WorkerSlot {
			return fmt.Errorf("worker_slot %d does not match the prepared probe slot %d", r.WorkerSlot, ctx.PreparedProbe.WorkerSlot)
		}
		if r.ChildIdentityDigest != ctx.PreparedProbe.ChildIdentityDigest ||
			r.ExecutionTemplateDigest != ctx.PreparedProbe.ExecutionTemplateDigest ||
			r.AccessBoundaryDigest != ctx.PreparedProbe.AccessBoundaryDigest {
			return errors.New("child identity/template/boundary drifts from the prepared slot probe")
		}
	case PurposeDevComparison:
		if r.ProtectedExecutionReceiptDigest != "" || r.PreparedWorkerProbeDigest != "" {
			return errors.New("dev-comparison must leave the protected artifacts null")
		}
		if r.ChildIdentityDigest != ctx.CoreIdentityDigest ||
			r.ExecutionTemplateDigest != ctx.CoreTemplateDigest ||
			r.AccessBoundaryDigest != ctx.CoreBoundaryDigest {
			return errors.New("dev-comparison identity/template/boundary drifts from the normalized core plan")
		}
	default:
		return fmt.Errorf("purpose %q invalid", ctx.Purpose)
	}
	if r.FreshStateRootDigest == "" {
		return errors.New("fresh_state_root_digest empty: no disposable state root proven")
	}
	if prev, dup := ctx.SeenStateRoots[r.FreshStateRootDigest]; dup {
		return fmt.Errorf("fresh_state_root_digest reuses the root of %s", prev)
	}
	if r.StateAllocatorDigest == "" {
		return errors.New("state_allocator_digest empty")
	}
	if r.StateAllocatorDigest != ctx.SplitAllocatorDigest {
		if ctx.Split != MembershipHoldout96 && r.StateAllocatorDigest == ctx.HoldoutAllocatorDigest {
			return errors.New("case claims the protected holdout state allocator outside the holdout leg")
		}
		return errors.New("state_allocator_digest does not match the split allocator")
	}
	if r.ResetMethod != "disposable" {
		return fmt.Errorf("reset_method %q, want disposable", r.ResetMethod)
	}
	for name, p := range map[string]*FormalAccessProbe{
		"prior_state_probe":       r.PriorStateProbe,
		"retired_workspace_probe": r.RetiredWorkspaceProbe,
	} {
		if p == nil {
			continue
		}
		if p.TargetDigest == "" || p.TargetAccessPolicyDigest == "" {
			return fmt.Errorf("%s lacks target/policy digests", name)
		}
		if p.Expected != "denied" {
			return fmt.Errorf("%s must expect denied, got %q", name, p.Expected)
		}
		switch p.Outcome {
		case "permission-denied":
		case "not-found":
			if p.ControllerTargetProofDigest == "" {
				return fmt.Errorf("%s observed not-found without controller proof", name)
			}
		default:
			return fmt.Errorf("%s observed %q: a prior/retired target must be unreadable", name, p.Outcome)
		}
	}
	if r.ReceiptDigest == "" {
		return errors.New("receipt_digest empty")
	}
	return nil
}

// ---------- test-local CaseRunReceipt validator ----------

// verdictSemanticClasses are the judge's closed semantic failure classes;
// `failed` (v1 chain) and `runner-error` (v2 chain) are the terminal codes.
var verdictSemanticClasses = map[string]bool{
	"false-negative": true,
	"false-positive": true,
	"wrong-op":       true,
	"wrong-report":   true,
}

var verdictTerminalCodes = map[string]bool{
	"failed":       true, // v1 judge chain
	"runner-error": true, // v2 judge chain
}

// validateCaseRunReceipt pins data-model.md §10: closed terminal status bound
// to its verdict class, exactly one primary attempt, and three artifact pairs
// that are always complete so the case stays rejudgeable.
func validateCaseRunReceipt(r *CaseRunReceipt) error {
	if r == nil {
		return errors.New("nil case run receipt")
	}
	for field, v := range map[string]string{
		"case_id":                             r.CaseID,
		"case_payload_digest":                 r.CasePayloadDigest,
		"workspace_digest":                    r.WorkspaceDigest,
		"case_state_isolation_receipt_digest": r.CaseStateIsolationReceiptDigest,
	} {
		if v == "" {
			return fmt.Errorf("%s empty", field)
		}
	}
	switch r.Status {
	case "pass":
		if !r.Verdict.Pass || r.Verdict.Failure != "" {
			return errors.New("status pass with a failing verdict")
		}
	case "fail":
		if r.Verdict.Pass {
			return errors.New("status fail with a passing verdict")
		}
		if r.Verdict.Failure == "runner-error" {
			return errors.New("runner-error cases carry status runner-error, not fail")
		}
		if !verdictSemanticClasses[r.Verdict.Failure] && !verdictTerminalCodes[r.Verdict.Failure] {
			return fmt.Errorf("status fail with failure class %q", r.Verdict.Failure)
		}
	case "runner-error":
		if r.Verdict.Pass || r.Verdict.Failure != "runner-error" {
			return errors.New("status runner-error must carry the runner-error verdict class")
		}
	default:
		return fmt.Errorf("status %q outside the closed pass/fail/runner-error set", r.Status)
	}
	if r.AttemptCount != 1 {
		return fmt.Errorf("attempt_count %d, primary allows exactly 1", r.AttemptCount)
	}
	pairs := []struct{ name, path, digest string }{
		{"normalized_events", r.NormalizedEventsPath, r.NormalizedEventsDigest},
		{"raw_events", r.RawEventsPath, r.RawEventsDigest},
		{"store_dump", r.StoreDumpPath, r.StoreDumpDigest},
	}
	for _, p := range pairs {
		if (p.path == "") != (p.digest == "") {
			return fmt.Errorf("%s artifact pair incomplete: path %q digest %q", p.name, p.path, p.digest)
		}
		if p.path != "" && len(p.digest) != 64 {
			return fmt.Errorf("%s digest is not a sha-256 hex digest", p.name)
		}
	}
	if r.DurationMS < 0 {
		return errors.New("negative duration_ms")
	}
	return nil
}

// validCaseRunReceipt builds a receipt whose artifact digests are the real
// digests of the fictional artifact contents it cites.
func validCaseRunReceipt(caseID string, raw, normalized, storeDump []byte) *CaseRunReceipt {
	return &CaseRunReceipt{
		CaseID:                          caseID,
		CasePayloadDigest:               rcptDigest("payload-" + caseID),
		WorkspaceDigest:                 rcptDigest("workspace-" + caseID),
		CaseStateIsolationReceiptDigest: rcptDigest("isolation-" + caseID),
		AttemptCount:                    1,
		Status:                          "pass",
		NormalizedEventsPath:            filepath.Join("out", "normalized", caseID+".json"),
		NormalizedEventsDigest:          sha256Hex(normalized),
		RawEventsPath:                   filepath.Join("out", "raw", caseID+".jsonl"),
		RawEventsDigest:                 sha256Hex(raw),
		StoreDumpPath:                   filepath.Join("out", "store", caseID+".txt"),
		StoreDumpDigest:                 sha256Hex(storeDump),
		Verdict:                         Verdict{CaseID: caseID, Pass: true},
		DurationMS:                      1200,
		StderrDigest:                    rcptDigest("stderr-" + caseID),
	}
}

// ---------- prepared slot / probe binding ----------

func TestReceiptPreparedSlotBindingValid(t *testing.T) {
	plan := receiptPlanFixture(2)
	if err := ValidateCoreExecutionPlan(plan); err != nil {
		t.Fatalf("fictional plan must satisfy the closed plan rules: %v", err)
	}
	prot := receiptProtectedFixture(plan)
	if err := ValidateProtectedExecutionReceipt(prot, plan); err != nil {
		t.Fatalf("fictional protected receipt must satisfy the closed rules: %v", err)
	}
	if len(prot.WorkerProbes) != len(plan.Hosts)*plan.Concurrency {
		t.Fatalf("worker probes %d, want the full %d host x %d slot matrix",
			len(prot.WorkerProbes), len(plan.Hosts), plan.Concurrency)
	}
	for _, p := range prot.WorkerProbes {
		if err := ValidateWorkerProbe(p, plan); err != nil {
			t.Fatalf("prepared probe %s/%d must satisfy the closed matrix: %v", p.Host, p.WorkerSlot, err)
		}
	}
	// The probe digest is stable across recomputation and moves when the probe
	// moves — so a case receipt that cites it cannot silently rebind.
	probe := preparedSlotProbe(HostClaude, 1)
	if preparedSlotProbeDigest(probe) != preparedSlotProbeDigest(preparedSlotProbe(HostClaude, 1)) {
		t.Fatal("prepared probe digest must be stable for identical probes")
	}
	drifted := *probe
	drifted.AccessBoundaryDigest = rcptDigest("other-boundary")
	if preparedSlotProbeDigest(probe) == preparedSlotProbeDigest(&drifted) {
		t.Fatal("a boundary change must move the prepared probe digest")
	}

	ctx := officialIsolationContext(plan, prot, HostClaude, 1)
	first := validCaseIsolationReceipt(ctx, "iwp-01", false)
	if err := validateCaseStateIsolation(first, ctx); err != nil {
		t.Fatalf("first case (no predecessor) must validate: %v", err)
	}
	ctx.SeenStateRoots[first.FreshStateRootDigest] = first.CaseID
	second := validCaseIsolationReceipt(ctx, "iwp-02", true)
	if err := validateCaseStateIsolation(second, ctx); err != nil {
		t.Fatalf("later case (prior+retired denied probes) must validate: %v", err)
	}
}

func TestReceiptUnknownWorkerSlotRejected(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := officialIsolationContext(plan, prot, HostClaude, 1)

	for _, slot := range []int{0, -1, plan.Concurrency + 1} {
		r := validCaseIsolationReceipt(ctx, "iwp-01", false)
		r.WorkerSlot = slot
		if err := validateCaseStateIsolation(r, ctx); err == nil {
			t.Fatalf("worker_slot %d outside the prepared range must be rejected", slot)
		}
	}
	// A receipt that cites another slot's prepared probe is a rebinding.
	other := preparedSlotProbe(HostClaude, 2)
	r := validCaseIsolationReceipt(ctx, "iwp-01", false)
	r.PreparedWorkerProbeDigest = preparedSlotProbeDigest(other)
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("citing a different slot's prepared probe must be rejected")
	}
	// A receipt claiming an in-range slot the cited probe was not prepared for.
	r = validCaseIsolationReceipt(ctx, "iwp-01", false)
	r.WorkerSlot = 2
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("an in-range slot the cited probe was not prepared for must be rejected")
	}
	// A missing prepared-probe reference is as fatal as a wrong one.
	r = validCaseIsolationReceipt(ctx, "iwp-01", false)
	r.PreparedWorkerProbeDigest = ""
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("missing prepared_worker_probe_digest must be rejected")
	}
}

func TestReceiptIdentityTemplateBoundaryDriftRejected(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := officialIsolationContext(plan, prot, HostClaude, 1)
	drifts := map[string]func(r *CaseStateIsolationReceipt){
		"child_identity_digest":     func(r *CaseStateIsolationReceipt) { r.ChildIdentityDigest = rcptDigest("rogue-child") },
		"execution_template_digest": func(r *CaseStateIsolationReceipt) { r.ExecutionTemplateDigest = rcptDigest("rogue-template") },
		"access_boundary_digest":    func(r *CaseStateIsolationReceipt) { r.AccessBoundaryDigest = rcptDigest("rogue-boundary") },
	}
	for name, mutate := range drifts {
		r := validCaseIsolationReceipt(ctx, "iwp-01", false)
		mutate(r)
		if err := validateCaseStateIsolation(r, ctx); err == nil {
			t.Fatalf("%s drift from the prepared slot probe must be rejected", name)
		}
	}
}

func TestReceiptFreshRootAndAllocatorContract(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := officialIsolationContext(plan, prot, HostClaude, 1)

	// Empty fresh root: no disposable state root proven.
	r := validCaseIsolationReceipt(ctx, "iwp-01", false)
	r.FreshStateRootDigest = ""
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("empty fresh_state_root_digest must be rejected")
	}
	// Root reuse across cases of the same series (any ordinal/split) is fatal.
	first := validCaseIsolationReceipt(ctx, "iwp-01", false)
	if err := validateCaseStateIsolation(first, ctx); err != nil {
		t.Fatal(err)
	}
	ctx.SeenStateRoots[first.FreshStateRootDigest] = first.CaseID
	reuser := validCaseIsolationReceipt(ctx, "iwp-02", true)
	reuser.FreshStateRootDigest = first.FreshStateRootDigest
	if err := validateCaseStateIsolation(reuser, ctx); err == nil {
		t.Fatal("a reused fresh state root must be rejected")
	}
	// The author/review roots are not case state roots.
	r = validCaseIsolationReceipt(ctx, "iwp-09", false)
	r.FreshStateRootDigest = prot.AuthorReviewStateRootsDigest
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("reusing the author/review roots as a case state root must be rejected")
	}
	// Allocator must be present and be the split's own.
	r = validCaseIsolationReceipt(ctx, "iwp-03", false)
	r.StateAllocatorDigest = ""
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("empty state_allocator_digest must be rejected")
	}
	r = validCaseIsolationReceipt(ctx, "iwp-04", false)
	r.StateAllocatorDigest = rcptDigest("unrelated-allocator")
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("a foreign allocator must be rejected")
	}
	// Protected holdout artifacts: a core-leg case must never claim the
	// holdout allocator (holdout roots/allocators do not exist before the
	// holdout leg runs).
	r = validCaseIsolationReceipt(ctx, "iwp-05", false)
	r.StateAllocatorDigest = ctx.HoldoutAllocatorDigest
	err := validateCaseStateIsolation(r, ctx)
	if err == nil {
		t.Fatal("a core172 case claiming the holdout allocator must be rejected")
	}
	if !strings.Contains(err.Error(), "holdout") {
		t.Fatalf("holdout misuse must be named: %v", err)
	}
}

func TestReceiptResetMethodDisposableOnly(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := officialIsolationContext(plan, prot, HostClaude, 1)
	for _, method := range []string{"", "recreate-root", "truncate", "DISPOSABLE"} {
		r := validCaseIsolationReceipt(ctx, "iwp-01", false)
		r.ResetMethod = method
		if err := validateCaseStateIsolation(r, ctx); err == nil {
			t.Fatalf("reset_method %q must be rejected: only disposable resets count", method)
		}
	}
}

func TestReceiptPriorRetiredProbesClosedRules(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := officialIsolationContext(plan, prot, HostClaude, 1)

	// No predecessor: both probes stay null.
	first := validCaseIsolationReceipt(ctx, "iwp-01", false)
	if first.PriorStateProbe != nil || first.RetiredWorkspaceProbe != nil {
		t.Fatal("the first case has no prior/retired target to probe")
	}
	if err := validateCaseStateIsolation(first, ctx); err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(p *FormalAccessProbe){
		"expected readable":     func(p *FormalAccessProbe) { p.Expected = "readable" },
		"outcome readable":      func(p *FormalAccessProbe) { p.Outcome = "readable" },
		"unknown outcome":       func(p *FormalAccessProbe) { p.Outcome = "granted" },
		"not-found no proof":    func(p *FormalAccessProbe) { p.Outcome = "not-found"; p.ControllerTargetProofDigest = "" },
		"missing target digest": func(p *FormalAccessProbe) { p.TargetDigest = "" },
		"missing policy digest": func(p *FormalAccessProbe) { p.TargetAccessPolicyDigest = "" },
	}
	for name, mutate := range cases {
		r := validCaseIsolationReceipt(ctx, "iwp-02", true)
		mutate(r.PriorStateProbe)
		if err := validateCaseStateIsolation(r, ctx); err == nil {
			t.Fatalf("prior_state_probe %s must be rejected", name)
		}
		r = validCaseIsolationReceipt(ctx, "iwp-02", true)
		mutate(r.RetiredWorkspaceProbe)
		if err := validateCaseStateIsolation(r, ctx); err == nil {
			t.Fatalf("retired_workspace_probe %s must be rejected", name)
		}
	}
	// A not-found observation is acceptable only with the controller proof.
	r := validCaseIsolationReceipt(ctx, "iwp-02", true)
	r.PriorStateProbe.Outcome = "not-found"
	r.PriorStateProbe.ControllerTargetProofDigest = rcptDigest("controller-proof")
	if err := validateCaseStateIsolation(r, ctx); err != nil {
		t.Fatalf("not-found with controller proof must validate: %v", err)
	}
}

func TestReceiptDevComparisonProtectedArtifactsNull(t *testing.T) {
	plan := receiptPlanFixture(2)
	prot := receiptProtectedFixture(plan)
	ctx := &caseIsolationContext{
		SeriesID: "series-dev", Host: HostCodex, Split: MembershipCore172, Ordinal: 2,
		Purpose: PurposeDevComparison, Concurrency: plan.Concurrency,
		CoreIdentityDigest:     plan.NormalizedCoreWorkerIdentitySetDigest,
		CoreTemplateDigest:     plan.NormalizedCoreExecutionTemplateDigest,
		CoreBoundaryDigest:     plan.NormalizedCoreBoundaryTemplateDigest,
		SplitAllocatorDigest:   prot.SplitStateAllocatorDigests[MembershipCore172],
		HoldoutAllocatorDigest: prot.SplitStateAllocatorDigests[MembershipHoldout96],
		SeenStateRoots:         map[string]string{},
	}
	r := validCaseIsolationReceipt(ctx, "irp-01", false)
	if r.ProtectedExecutionReceiptDigest != "" || r.PreparedWorkerProbeDigest != "" {
		t.Fatal("dev-comparison fixtures must carry no protected artifacts")
	}
	if err := validateCaseStateIsolation(r, ctx); err != nil {
		t.Fatalf("dev-comparison case with null protected artifacts must validate: %v", err)
	}
	// Official-dual artifacts in a dev-comparison series are a lie.
	r.ProtectedExecutionReceiptDigest = prot.ReceiptDigest
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("dev-comparison must reject a protected execution receipt reference")
	}
	r = validCaseIsolationReceipt(ctx, "irp-01", false)
	r.PreparedWorkerProbeDigest = preparedSlotProbeDigest(preparedSlotProbe(HostCodex, 1))
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("dev-comparison must reject a prepared-probe reference")
	}
	// Identity/template/boundary must still match the normalized core plan.
	r = validCaseIsolationReceipt(ctx, "irp-01", false)
	r.ExecutionTemplateDigest = rcptDigest("rogue-template")
	if err := validateCaseStateIsolation(r, ctx); err == nil {
		t.Fatal("dev-comparison template drift from the core plan must be rejected")
	}
	// Unknown purpose fails closed.
	ctx.Purpose = "mystery"
	if err := validateCaseStateIsolation(validCaseIsolationReceipt(ctx, "irp-01", false), ctx); err == nil {
		t.Fatal("an unknown purpose must fail closed")
	}
}

// ---------- CaseRunReceipt: closed status, single attempt, artifact pairs ----------

func TestCaseRunReceiptClosedStatusAndPairs(t *testing.T) {
	raw := []byte(`{"type":"result","result":"已记住 pnpm"}`)
	normalized := []byte(`[{"Kind":"engram_call","Op":"write","Via":"mcp"},{"Kind":"assistant_text","Text":"已记住 pnpm"}]`)
	store := []byte("pnpm\n")

	pass := validCaseRunReceipt("iwp-01", raw, normalized, store)
	if err := validateCaseRunReceipt(pass); err != nil {
		t.Fatalf("valid pass receipt must validate: %v", err)
	}
	fail := validCaseRunReceipt("iwp-02", raw, normalized, store)
	fail.Status, fail.Verdict = "fail", Verdict{CaseID: "iwp-02", Failure: "false-negative", Detail: "no engram write call observed"}
	if err := validateCaseRunReceipt(fail); err != nil {
		t.Fatalf("valid fail receipt must validate: %v", err)
	}
	runErr := validCaseRunReceipt("iwp-03", raw, normalized, store)
	runErr.Status = "runner-error"
	runErr.Verdict = Verdict{CaseID: "iwp-03", Failure: "runner-error", Detail: "terminal runner error: spawn failed"}
	if err := validateCaseRunReceipt(runErr); err != nil {
		t.Fatalf("valid runner-error receipt must validate: %v", err)
	}

	// Status is a closed set and must agree with its verdict.
	bad := validCaseRunReceipt("iwp-04", raw, normalized, store)
	bad.Status = "error"
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("status outside pass/fail/runner-error must be rejected")
	}
	bad = validCaseRunReceipt("iwp-04", raw, normalized, store)
	bad.Status, bad.Verdict = "pass", Verdict{CaseID: "iwp-04", Failure: "false-negative"}
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("status pass with a failing verdict must be rejected")
	}
	bad = validCaseRunReceipt("iwp-04", raw, normalized, store)
	bad.Status, bad.Verdict = "runner-error", Verdict{CaseID: "iwp-04", Failure: "wrong-op"}
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("status runner-error with a judge failure class must be rejected")
	}
	bad = validCaseRunReceipt("iwp-04", raw, normalized, store)
	bad.Status, bad.Verdict = "fail", Verdict{CaseID: "iwp-04", Failure: "runner-error"}
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("status fail must not absorb runner-error")
	}
	bad = validCaseRunReceipt("iwp-04", raw, normalized, store)
	bad.Status, bad.Verdict = "fail", Verdict{CaseID: "iwp-04", Failure: "made-up-class"}
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("an undocumented failure class must be rejected")
	}

	// Exactly one primary attempt.
	bad = validCaseRunReceipt("iwp-05", raw, normalized, store)
	bad.AttemptCount = 2
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("attempt_count > 1 must be rejected in primary mode")
	}
	bad = validCaseRunReceipt("iwp-05", raw, normalized, store)
	bad.AttemptCount = 0
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("attempt_count 0 must be rejected")
	}

	// Every rejudge artifact is a complete path+digest pair.
	for _, drop := range []struct {
		name   string
		mutate func(r *CaseRunReceipt)
	}{
		{"normalized digest only", func(r *CaseRunReceipt) { r.NormalizedEventsPath = "" }},
		{"normalized path only", func(r *CaseRunReceipt) { r.NormalizedEventsDigest = "" }},
		{"raw digest only", func(r *CaseRunReceipt) { r.RawEventsPath = "" }},
		{"raw path only", func(r *CaseRunReceipt) { r.RawEventsDigest = "" }},
		{"store digest only", func(r *CaseRunReceipt) { r.StoreDumpPath = "" }},
		{"store path only", func(r *CaseRunReceipt) { r.StoreDumpDigest = "" }},
	} {
		bad = validCaseRunReceipt("iwp-06", raw, normalized, store)
		drop.mutate(bad)
		if err := validateCaseRunReceipt(bad); err == nil {
			t.Fatalf("%s: a half-recorded artifact pair must be rejected", drop.name)
		}
	}
	bad = validCaseRunReceipt("iwp-06", raw, normalized, store)
	bad.StoreDumpDigest = "not-a-digest"
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("a non-sha256 artifact digest must be rejected")
	}

	// Identity fields and duration.
	for _, drop := range []struct {
		name   string
		mutate func(r *CaseRunReceipt)
	}{
		{"case_id", func(r *CaseRunReceipt) { r.CaseID = "" }},
		{"case_payload_digest", func(r *CaseRunReceipt) { r.CasePayloadDigest = "" }},
		{"workspace_digest", func(r *CaseRunReceipt) { r.WorkspaceDigest = "" }},
		{"isolation receipt digest", func(r *CaseRunReceipt) { r.CaseStateIsolationReceiptDigest = "" }},
	} {
		bad = validCaseRunReceipt("iwp-07", raw, normalized, store)
		drop.mutate(bad)
		if err := validateCaseRunReceipt(bad); err == nil {
			t.Fatalf("%s must be required", drop.name)
		}
	}
	bad = validCaseRunReceipt("iwp-07", raw, normalized, store)
	bad.DurationMS = -1
	if err := validateCaseRunReceipt(bad); err == nil {
		t.Fatal("a negative duration must be rejected")
	}
	if err := validateCaseRunReceipt(nil); err == nil {
		t.Fatal("nil receipt must be rejected")
	}
}

// TestCaseRunReceiptDigestBindsArtifactContent pins the rejudge promise: the
// recorded digest is the digest of the artifact on disk, so any content drift
// is detectable instead of silently re-judged from different bytes.
func TestCaseRunReceiptDigestBindsArtifactContent(t *testing.T) {
	raw := []byte(`{"type":"result","result":"已记住 pnpm"}` + "\n")
	normalized, err := CanonicalJSON([]Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已记住 pnpm 配置"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := []byte("pnpm\n")

	r := validCaseRunReceipt("irp-01", raw, normalized, store)
	if r.RawEventsDigest != sha256Hex(raw) || r.NormalizedEventsDigest != sha256Hex(normalized) || r.StoreDumpDigest != sha256Hex(store) {
		t.Fatal("artifact digests must be the digests of the cited contents")
	}
	if err := validateCaseRunReceipt(r); err != nil {
		t.Fatal(err)
	}
	// Rewriting any artifact behind the receipt's back is detectable.
	tampered := append([]byte{}, store...)
	tampered = append(tampered, []byte("rogue entry")...)
	if sha256Hex(tampered) == r.StoreDumpDigest {
		t.Fatal("a mutated store dump must no longer match the recorded digest")
	}
}

// ---------- verdict closed set ----------

// TestVerdictFailureClosedSet drives the real v2 judge through every closed
// failure class and pins the terminal codes.
func TestVerdictFailureClosedSet(t *testing.T) {
	c := &TriggerCaseV2{ID: "fx", Module: "implicit-write-pos",
		Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "synthetic"}}
	// A negative case is the only honest producer of false-positive.
	neg := &TriggerCaseV2{ID: "fxneg", Module: "implicit-read-neg",
		Expect: ExpectV2{Trigger: false, Observable: "synthetic"}}
	write := []Event{{Kind: EventEngramCall, Op: "write", Via: "mcp"}}
	acked := append(append([]Event{}, write...), Event{Kind: EventText, Text: "已记住 pnpm 配置"})

	samples := []struct {
		name   string
		caseID string
		want   string
		v      Verdict
	}{
		{"pass", c.ID, "", JudgeV2(c, acked, "pnpm\n", nil)},
		{"false-negative", c.ID, "false-negative", JudgeV2(c, []Event{}, "pnpm\n", nil)},
		{"false-positive", neg.ID, "false-positive", JudgeV2(neg, []Event{{Kind: EventEngramCall, Op: "search"}}, "", nil)},
		{"wrong-op", c.ID, "wrong-op", JudgeV2(c, write, "unrelated store\n", nil)},
		{"wrong-report", c.ID, "wrong-report", JudgeV2(c, write, "pnpm\n", nil)},
		{"runner-error", c.ID, "runner-error", JudgeV2(c, acked, "pnpm\n", errors.New("fictional terminal runner failure"))},
	}
	for _, s := range samples {
		if s.v.CaseID != s.caseID {
			t.Fatalf("%s: verdict must carry its case id: %+v", s.name, s.v)
		}
		if s.v.Failure != s.want {
			t.Fatalf("%s: failure class %q, want %q (detail %q)", s.name, s.v.Failure, s.want, s.v.Detail)
		}
		if s.want == "" {
			if !s.v.Pass {
				t.Fatalf("pass sample must pass: %+v", s.v)
			}
			continue
		}
		if s.v.Pass {
			t.Fatalf("%s sample must not pass: %+v", s.name, s.v)
		}
		if !verdictSemanticClasses[s.v.Failure] && !verdictTerminalCodes[s.v.Failure] {
			t.Fatalf("%s: class %q is outside the closed set", s.name, s.v.Failure)
		}
		if s.v.Detail == "" {
			t.Fatalf("%s: a failure verdict must carry a machine-readable detail code", s.name)
		}
	}
}

// ---------- workspace digest + rejudge determinism ----------

// materializedWorkspaceDigest digests the staged file set of a prepared case
// workspace with the frozen engram-package digest (sorted relative paths,
// LF-normalized content).
func materializedWorkspaceDigest(t *testing.T, caseDir string) string {
	t.Helper()
	var recs []PackageFileRecord
	err := filepath.WalkDir(caseDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(caseDir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		recs = append(recs, PackageFileRecord{RelativePath: filepath.ToSlash(rel), Size: int(info.Size())})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := EngramPackageDigest(recs, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(caseDir, filepath.FromSlash(rel)))
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func sampleWorkspaceFiles() []WorkspaceFile {
	skill := "---\nname: engram\n---\n\ntrigger contract body\n"
	notes := "workspace note: pnpm store lives here\n"
	return []WorkspaceFile{
		{Path: "SKILL.md", Content: skill, SHA256: sha256Hex([]byte(skill))},
		{Path: "references/notes.md", Content: notes},
	}
}

func TestWorkspaceDigestStableAndContentBound(t *testing.T) {
	files := sampleWorkspaceFiles()
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := MaterializeWorkspace(dirA, files); err != nil {
		t.Fatal(err)
	}
	if err := MaterializeWorkspace(dirB, files); err != nil {
		t.Fatal(err)
	}
	digestA, digestB := materializedWorkspaceDigest(t, dirA), materializedWorkspaceDigest(t, dirB)
	if digestA == "" || digestA != digestB {
		t.Fatalf("identical staged workspaces must share one digest: %q vs %q", digestA, digestB)
	}

	// Any content change moves the digest.
	changed := append([]WorkspaceFile{}, files...)
	changed[1] = WorkspaceFile{Path: "references/notes.md", Content: "workspace note: changed\n"}
	dirC := t.TempDir()
	if err := MaterializeWorkspace(dirC, changed); err != nil {
		t.Fatal(err)
	}
	if d := materializedWorkspaceDigest(t, dirC); d == digestA {
		t.Fatal("a changed workspace file must move the workspace digest")
	}
	// An added file moves it too.
	extra := append(append([]WorkspaceFile{}, files...), WorkspaceFile{Path: "extra.txt", Content: "more\n"})
	dirD := t.TempDir()
	if err := MaterializeWorkspace(dirD, extra); err != nil {
		t.Fatal(err)
	}
	if d := materializedWorkspaceDigest(t, dirD); d == digestA {
		t.Fatal("an added workspace file must move the workspace digest")
	}

	// Staging stays contained and digest-verified.
	if err := MaterializeWorkspace(t.TempDir(), []WorkspaceFile{{Path: "../escape.txt", Content: "x"}}); err == nil {
		t.Fatal("a non-containment-safe workspace path must be rejected")
	}
	badSum := sampleWorkspaceFiles()
	badSum[0].SHA256 = rcptDigest("not-the-content-digest")
	if err := MaterializeWorkspace(t.TempDir(), badSum); err == nil {
		t.Fatal("a sha256 mismatch must be rejected instead of staged silently")
	}
}

// TestRejudgeFromRecordedArtifactsIsDeterministic pins why the three artifact
// pairs exist: re-judging from the recorded normalized events + store dump
// reproduces the verdict byte-for-byte, and tampered artifacts change it.
func TestRejudgeFromRecordedArtifactsIsDeterministic(t *testing.T) {
	c := &TriggerCaseV2{ID: "irp-09", Module: "implicit-write-pos",
		Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "synthetic"}}
	events := []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已记住 pnpm 配置"},
	}
	storeDump := "pnpm\n"

	first := JudgeV2(c, events, storeDump, nil)
	if !first.Pass {
		t.Fatalf("reference verdict must pass: %+v", first)
	}

	// Round-trip the normalized events through their recorded artifact form.
	recorded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	var replayed []Event
	if err := json.Unmarshal(recorded, &replayed); err != nil {
		t.Fatal(err)
	}
	second := JudgeV2(c, replayed, storeDump, nil)
	if second != first {
		t.Fatalf("rejudge from the recorded normalized events must be identical: %+v vs %+v", second, first)
	}

	// A tampered store dump is not silently accepted as the same case.
	if v := JudgeV2(c, replayed, "rogue store\n", nil); v.Pass || v.Failure != "wrong-op" {
		t.Fatalf("a mutated store dump must change the verdict: %+v", v)
	}
	// A tampered answer stream is not silently accepted either.
	edited := append([]Event{}, replayed...)
	edited[1].Text = "已经处理好了"
	if v := JudgeV2(c, edited, storeDump, nil); v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("a mutated answer must change the verdict: %+v", v)
	}
	// A terminal runner error overrides the artifacts and stays conservative.
	if v := JudgeV2(c, replayed, storeDump, errors.New("timeout")); v.Pass || v.Failure != "runner-error" {
		t.Fatalf("runner-error must dominate a rejudge: %+v", v)
	}
}
