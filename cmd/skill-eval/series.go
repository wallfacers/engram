package main

// US4 formal-series types and closed validation semantics (data-model.md
// §7-§12): CoreExecutionPlanReceipt, FormalSeriesManifest, CandidateBindingV1
// stable recovery key, HoldoutBindingReceipt, ProtectedExecutionReceipt +
// workspace canaries, PrimaryRunManifest / CaseRunReceipt, FailureArchive /
// comparison, and OfficialScoreReport. Pure digest/validation logic lives
// here; execution (primary runs, probe execution, canary execution) is
// wired by T045-T051 and stubs fail closed.

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ErrNotWired marks a US4 execution entry point whose implementation task
// (T045-T051) has not landed yet. Callers must fail closed on it.
var ErrNotWired = errors.New("formal-series execution not wired yet (T045-T051)")

// SeriesPurpose decides which splits a formal series binds and whether it
// may ever produce an official score.
type SeriesPurpose string

const (
	PurposeOfficialDual  SeriesPurpose = "official-dual"
	PurposeDevComparison SeriesPurpose = "dev-comparison"
)

// LifecycleState for series and primary runs.
type LifecycleState string

const (
	StateDraft    LifecycleState = "draft"
	StateSealed   LifecycleState = "sealed"
	StateComplete LifecycleState = "complete"
	StateInvalid  LifecycleState = "invalid"
)

// BoundaryKind is the operator-provided isolation mechanism for a series.
type BoundaryKind string

const (
	BoundarySeparateUser   BoundaryKind = "separate-user"
	BoundaryContainer      BoundaryKind = "container"
	BoundaryMountNamespace BoundaryKind = "mount-namespace"
	BoundaryACL            BoundaryKind = "acl"
	BoundaryEquivalent     BoundaryKind = "equivalent"
)

// ValidBoundary reports whether k is one of the five closed mechanisms.
func ValidBoundary(k BoundaryKind) bool {
	switch k {
	case BoundarySeparateUser, BoundaryContainer, BoundaryMountNamespace, BoundaryACL, BoundaryEquivalent:
		return true
	}
	return false
}

// Ordinals are the three mandatory primary repetitions.
var Ordinals = [3]int{1, 2, 3}

// ---------- §7 CandidateBindingV1 ----------

// CandidateBindingV1 is the stable recovery key of one holdout version. Its
// preimage deliberately excludes series_id, the series manifest digest, the
// protected receipt digest, canary digests, unique run/case/state roots,
// timestamps, and every per-attempt pre-holdout receipt.
type CandidateBindingV1 struct {
	SchemaVersion                       int               `json:"schema_version"`
	Purpose                             SeriesPurpose     `json:"purpose"`
	SkillSnapshotDigest                 string            `json:"skill_snapshot_digest"`
	SkillSnapshotAnchorDigest           string            `json:"skill_snapshot_anchor_digest"`
	SkillDigest                         string            `json:"skill_digest"`
	SkillPackageValidationReceiptDigest string            `json:"skill_package_validation_receipt_digest"`
	ValidatorRevision                   string            `json:"validator_revision"`
	ValidatorDigest                     string            `json:"validator_digest"`
	RunnerRevision                      string            `json:"runner_revision"`
	RunnerDigest                        string            `json:"runner_digest"`
	JudgeRuleDigest                     string            `json:"judge_rule_digest"`
	DatasetIdentities                   map[string]string `json:"dataset_identities"` // membership → manifest digest (+ seal/anchor ids)
	CoreExecutionPlanDigest             string            `json:"core_execution_plan_digest"`
	ToolIdentityDigests                 map[string]string `json:"tool_identity_digests"` // host → digest
	ToolConfigurationDigest             string            `json:"tool_configuration_digest"`
	TimeoutSeconds                      int               `json:"timeout_seconds"`
	Concurrency                         int               `json:"concurrency"`
	CaseOrderSeeds                      map[int]string    `json:"case_order_seeds"`
	ExecutionEnvironmentDigest          string            `json:"execution_environment_digest"`
	ProtectedExecutionPolicyDigest      string            `json:"protected_execution_policy_digest"`
	SeriesPrepareIdentityDigest         string            `json:"series_prepare_identity_digest"`
}

// CandidateBindingDigest computes the stable recovery key over the canonical
// projection of the binding. It fails closed when a required stable input is
// empty or a map is nil.
func CandidateBindingDigest(b *CandidateBindingV1) (string, error) {
	if b == nil {
		return "", errors.New("nil candidate binding")
	}
	if b.SchemaVersion != 1 {
		return "", fmt.Errorf("candidate binding schema_version %d, want 1", b.SchemaVersion)
	}
	if b.Purpose != PurposeOfficialDual {
		return "", fmt.Errorf("candidate binding purpose %q, want official-dual", b.Purpose)
	}
	type proj struct {
		SchemaVersion                       int               `json:"schema_version"`
		Purpose                             SeriesPurpose     `json:"purpose"`
		SkillSnapshotDigest                 string            `json:"skill_snapshot_digest"`
		SkillSnapshotAnchorDigest           string            `json:"skill_snapshot_anchor_digest"`
		SkillDigest                         string            `json:"skill_digest"`
		SkillPackageValidationReceiptDigest string            `json:"skill_package_validation_receipt_digest"`
		ValidatorRevision                   string            `json:"validator_revision"`
		ValidatorDigest                     string            `json:"validator_digest"`
		RunnerRevision                      string            `json:"runner_revision"`
		RunnerDigest                        string            `json:"runner_digest"`
		JudgeRuleDigest                     string            `json:"judge_rule_digest"`
		DatasetIdentities                   map[string]string `json:"dataset_identities"`
		CoreExecutionPlanDigest             string            `json:"core_execution_plan_digest"`
		ToolIdentityDigests                 map[string]string `json:"tool_identity_digests"`
		ToolConfigurationDigest             string            `json:"tool_configuration_digest"`
		TimeoutSeconds                      int               `json:"timeout_seconds"`
		Concurrency                         int               `json:"concurrency"`
		// Canonical JSON only admits string map keys, so the ordinal keys are
		// projected as their shortest decimal form ("1","2","3").
		CaseOrderSeeds                 map[string]string `json:"case_order_seeds"`
		ExecutionEnvironmentDigest     string            `json:"execution_environment_digest"`
		ProtectedExecutionPolicyDigest string            `json:"protected_execution_policy_digest"`
		SeriesPrepareIdentityDigest    string            `json:"series_prepare_identity_digest"`
	}
	p := proj{
		SchemaVersion: b.SchemaVersion, Purpose: b.Purpose,
		SkillSnapshotDigest: b.SkillSnapshotDigest, SkillSnapshotAnchorDigest: b.SkillSnapshotAnchorDigest,
		SkillDigest: b.SkillDigest, SkillPackageValidationReceiptDigest: b.SkillPackageValidationReceiptDigest,
		ValidatorRevision: b.ValidatorRevision, ValidatorDigest: b.ValidatorDigest,
		RunnerRevision: b.RunnerRevision, RunnerDigest: b.RunnerDigest, JudgeRuleDigest: b.JudgeRuleDigest,
		DatasetIdentities: b.DatasetIdentities, CoreExecutionPlanDigest: b.CoreExecutionPlanDigest,
		ToolIdentityDigests: b.ToolIdentityDigests, ToolConfigurationDigest: b.ToolConfigurationDigest,
		TimeoutSeconds: b.TimeoutSeconds, Concurrency: b.Concurrency,
		CaseOrderSeeds: map[string]string{
			"1": b.CaseOrderSeeds[1],
			"2": b.CaseOrderSeeds[2],
			"3": b.CaseOrderSeeds[3],
		},
		ExecutionEnvironmentDigest:     b.ExecutionEnvironmentDigest,
		ProtectedExecutionPolicyDigest: b.ProtectedExecutionPolicyDigest,
		SeriesPrepareIdentityDigest:    b.SeriesPrepareIdentityDigest,
	}
	empties := []string{}
	if p.SkillSnapshotDigest == "" {
		empties = append(empties, "skill_snapshot_digest")
	}
	if p.SkillSnapshotAnchorDigest == "" {
		empties = append(empties, "skill_snapshot_anchor_digest")
	}
	if p.SkillDigest == "" {
		empties = append(empties, "skill_digest")
	}
	if p.SkillPackageValidationReceiptDigest == "" {
		empties = append(empties, "skill_package_validation_receipt_digest")
	}
	if p.ValidatorRevision == "" {
		empties = append(empties, "validator_revision")
	}
	if p.ValidatorDigest == "" {
		empties = append(empties, "validator_digest")
	}
	if p.RunnerRevision == "" {
		empties = append(empties, "runner_revision")
	}
	if p.RunnerDigest == "" {
		empties = append(empties, "runner_digest")
	}
	if p.JudgeRuleDigest == "" {
		empties = append(empties, "judge_rule_digest")
	}
	if p.CoreExecutionPlanDigest == "" {
		empties = append(empties, "core_execution_plan_digest")
	}
	if p.ToolConfigurationDigest == "" {
		empties = append(empties, "tool_configuration_digest")
	}
	if p.ExecutionEnvironmentDigest == "" {
		empties = append(empties, "execution_environment_digest")
	}
	if p.ProtectedExecutionPolicyDigest == "" {
		empties = append(empties, "protected_execution_policy_digest")
	}
	if p.SeriesPrepareIdentityDigest == "" {
		empties = append(empties, "series_prepare_identity_digest")
	}
	if p.TimeoutSeconds <= 0 {
		empties = append(empties, "timeout_seconds")
	}
	if p.Concurrency <= 0 {
		empties = append(empties, "concurrency")
	}
	if len(p.DatasetIdentities) != 2 {
		empties = append(empties, "dataset_identities(core172+holdout96)")
	}
	if len(p.ToolIdentityDigests) != 3 {
		empties = append(empties, "tool_identity_digests(3 hosts)")
	}
	// The seed map is checked on the ORIGINAL binding: the projection always
	// materializes three entries, so a nil/short map would silently pass.
	if len(b.CaseOrderSeeds) != 3 {
		empties = append(empties, "case_order_seeds(3 ordinals)")
	}
	for _, o := range Ordinals {
		if b.CaseOrderSeeds[o] == "" {
			empties = append(empties, fmt.Sprintf("case_order_seed[%d]", o))
		}
	}
	if len(empties) > 0 {
		return "", fmt.Errorf("candidate binding missing stable inputs: %s", strings.Join(empties, ", "))
	}
	return CanonicalSHA256(p)
}

// ---------- §8 CoreExecutionPlanReceipt ----------

// CoreExecutionPlanReceipt freezes the stable core172 execution conditions
// once; the SC-5 baseline and the post-change official-dual series reference
// the same receipt digest. It deliberately does NOT bind the evaluated skill.
type CoreExecutionPlanReceipt struct {
	SchemaVersion                         int               `json:"schema_version"`
	PlanID                                string            `json:"plan_id"`
	CoreManifestDigest                    string            `json:"core_manifest_digest"`
	RunnerRevision                        string            `json:"runner_revision"`
	RunnerDigest                          string            `json:"runner_digest"`
	JudgeRuleDigest                       string            `json:"judge_rule_digest"`
	Hosts                                 []string          `json:"hosts"`
	ToolIdentityDigests                   map[string]string `json:"tool_identity_digests"`
	TimeoutSeconds                        int               `json:"timeout_seconds"`
	Concurrency                           int               `json:"concurrency"`
	CaseOrderSeeds                        map[int]string    `json:"case_order_seeds"`
	CoreBoundaryKind                      BoundaryKind      `json:"core_boundary_kind"`
	NormalizedCoreWorkerIdentitySetDigest string            `json:"normalized_core_worker_identity_set_digest"`
	NormalizedCoreBoundaryTemplateDigest  string            `json:"normalized_core_boundary_template_digest"`
	NormalizedCoreExecutionTemplateDigest string            `json:"normalized_core_execution_template_digest"`
	CreatedAt                             string            `json:"created_at"`
	ReceiptDigest                         string            `json:"receipt_digest"`
	SealDigest                            string            `json:"seal_digest"`
}

// ValidateCoreExecutionPlan checks the closed plan semantics: schema, three
// hosts, positive timeout/concurrency, exactly ordinals 1-3, valid boundary.
func ValidateCoreExecutionPlan(p *CoreExecutionPlanReceipt) error {
	if p == nil {
		return errors.New("nil core execution plan")
	}
	if p.SchemaVersion != 1 {
		return fmt.Errorf("plan schema_version %d, want 1", p.SchemaVersion)
	}
	if p.PlanID == "" || p.CoreManifestDigest == "" || p.RunnerRevision == "" ||
		p.RunnerDigest == "" || p.JudgeRuleDigest == "" {
		return errors.New("plan identity fields incomplete")
	}
	if len(p.Hosts) != 3 {
		return fmt.Errorf("plan hosts %d, want exactly 3", len(p.Hosts))
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		found := false
		for _, hh := range p.Hosts {
			if hh == h {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("plan missing host %s", h)
		}
		if p.ToolIdentityDigests[h] == "" {
			return fmt.Errorf("plan missing tool_identity_digest for %s", h)
		}
	}
	if p.TimeoutSeconds <= 0 || p.Concurrency <= 0 {
		return errors.New("plan timeout/concurrency must be positive")
	}
	if len(p.CaseOrderSeeds) != 3 {
		return fmt.Errorf("plan case_order_seeds %d, want exactly 3", len(p.CaseOrderSeeds))
	}
	for _, o := range Ordinals {
		if p.CaseOrderSeeds[o] == "" {
			return fmt.Errorf("plan missing case_order_seed for ordinal %d", o)
		}
	}
	if !ValidBoundary(p.CoreBoundaryKind) {
		return fmt.Errorf("plan boundary kind %q invalid", p.CoreBoundaryKind)
	}
	for _, d := range []string{p.NormalizedCoreWorkerIdentitySetDigest, p.NormalizedCoreBoundaryTemplateDigest, p.NormalizedCoreExecutionTemplateDigest} {
		if d == "" {
			return errors.New("plan normalized core digests incomplete")
		}
	}
	if p.ReceiptDigest == "" || p.SealDigest == "" {
		return errors.New("plan not sealed")
	}
	return nil
}

// ---------- §8 FormalSeriesManifest ----------

// FormalSeriesManifest binds one candidate skill/runner to 3 hosts × 3
// ordinals and the splits its purpose decides.
type FormalSeriesManifest struct {
	SeriesID                            string                    `json:"series_id"`
	Purpose                             SeriesPurpose             `json:"purpose"`
	State                               LifecycleState            `json:"state"`
	SkillSnapshotDigest                 string                    `json:"skill_snapshot_digest"`
	SkillSnapshotAnchorDigest           string                    `json:"skill_snapshot_anchor_digest"`
	SkillVersion                        string                    `json:"skill_version"`
	SkillDigest                         string                    `json:"skill_digest"`
	SkillPackageValidationReceiptDigest string                    `json:"skill_package_validation_receipt_digest"`
	GreenTestReceiptDigest              string                    `json:"green_test_receipt_digest"`
	SeriesPrepareIdentityDigest         string                    `json:"series_prepare_identity_digest"`
	RunnerRevision                      string                    `json:"runner_revision"`
	RunnerDigest                        string                    `json:"runner_digest"`
	JudgeRuleDigest                     string                    `json:"judge_rule_digest"`
	CoreExecutionPlanDigest             string                    `json:"core_execution_plan_digest"`
	DatasetManifests                    map[string]string         `json:"dataset_manifests"` // membership → manifest digest
	Hosts                               []string                  `json:"hosts"`
	RequiredOrdinals                    []int                     `json:"required_ordinals"`
	TimeoutSeconds                      int                       `json:"timeout_seconds"`
	Concurrency                         int                       `json:"concurrency"`
	ExecutionEnvironmentDigest          string                    `json:"execution_environment_digest"`
	ToolConfigurationDigest             string                    `json:"tool_configuration_digest"`
	ProtectedExecutionPolicyDigest      string                    `json:"protected_execution_policy_digest"`
	CaseOrderSeeds                      map[int]string            `json:"case_order_seeds"`
	QuestionCount                       map[string]int            `json:"question_count"`
	CandidateBindingDigest              string                    `json:"candidate_binding_digest"`
	ProtectedExecutionReceiptDigest     string                    `json:"protected_execution_receipt_digest"`
	WorkspaceCanaryReceiptDigests       map[string]map[int]string `json:"workspace_canary_receipt_digests"`
	ManifestDigest                      string                    `json:"manifest_digest"`
}

// ExpectedQuestionCount maps a bound split to its exact size.
func ExpectedQuestionCount(membership string) (int, error) {
	switch membership {
	case MembershipCore172:
		return 172, nil
	case MembershipHoldout96:
		return 96, nil
	}
	return 0, fmt.Errorf("membership %q is not a formal-series split", membership)
}

// ValidateFormalSeriesManifest enforces the closed purpose-aware semantics:
// official-dual binds exactly core172+holdout96 and must carry the stable
// candidate binding + protected receipt digests; dev-comparison binds only
// core172 and must leave them null.
func ValidateFormalSeriesManifest(m *FormalSeriesManifest) error {
	if m == nil {
		return errors.New("nil series manifest")
	}
	if m.SeriesID == "" {
		return errors.New("series_id empty")
	}
	switch m.Purpose {
	case PurposeOfficialDual:
	case PurposeDevComparison:
	default:
		return fmt.Errorf("purpose %q invalid", m.Purpose)
	}
	if len(m.Hosts) != 3 {
		return fmt.Errorf("series hosts %d, want exactly 3", len(m.Hosts))
	}
	if len(m.RequiredOrdinals) != 3 || m.RequiredOrdinals[0] != 1 || m.RequiredOrdinals[1] != 2 || m.RequiredOrdinals[2] != 3 {
		return errors.New("required_ordinals must be exactly [1,2,3]")
	}
	if m.CoreExecutionPlanDigest == "" {
		return errors.New("series must reference a sealed core execution plan")
	}
	switch m.Purpose {
	case PurposeOfficialDual:
		if len(m.DatasetManifests) != 2 || m.DatasetManifests[MembershipCore172] == "" || m.DatasetManifests[MembershipHoldout96] == "" {
			return errors.New("official-dual must bind exactly core172 and holdout96")
		}
		if m.CandidateBindingDigest == "" {
			return errors.New("official-dual candidate_binding_digest required")
		}
		if m.ProtectedExecutionReceiptDigest == "" {
			return errors.New("official-dual protected_execution_receipt_digest required")
		}
		if m.ProtectedExecutionPolicyDigest == "" {
			return errors.New("official-dual protected_execution_policy_digest required")
		}
	case PurposeDevComparison:
		if len(m.DatasetManifests) != 1 || m.DatasetManifests[MembershipCore172] == "" {
			return errors.New("dev-comparison must bind exactly core172")
		}
		if m.CandidateBindingDigest != "" {
			return errors.New("dev-comparison candidate_binding_digest must be null")
		}
		if m.ProtectedExecutionReceiptDigest != "" {
			return errors.New("dev-comparison protected_execution_receipt_digest must be null")
		}
		if m.ProtectedExecutionPolicyDigest != "" {
			return errors.New("dev-comparison protected_execution_policy_digest must be null")
		}
	}
	qcNeed, qcHave := map[string]bool{}, 0
	for mem := range m.DatasetManifests {
		qcNeed[mem] = true
		qcHave++
	}
	if len(m.QuestionCount) != qcHave {
		return fmt.Errorf("question_count covers %d splits, want %d", len(m.QuestionCount), qcHave)
	}
	for mem := range qcNeed {
		want, err := ExpectedQuestionCount(mem)
		if err != nil {
			return err
		}
		if m.QuestionCount[mem] != want {
			return fmt.Errorf("question_count[%s]=%d, want %d", mem, m.QuestionCount[mem], want)
		}
	}
	if m.SkillPackageValidationReceiptDigest == "" {
		return errors.New("series must bind a passing exact-skill package-validation receipt")
	}
	if m.GreenTestReceiptDigest == "" || m.SeriesPrepareIdentityDigest == "" {
		return errors.New("series must bind a matching series-prepare green-test receipt")
	}
	if m.ManifestDigest == "" {
		return errors.New("series manifest not sealed")
	}
	return nil
}

// ---------- §7 HoldoutBindingReceipt ----------

// HoldoutSeriesAttempt is one append-only attempt entry.
type HoldoutSeriesAttempt struct {
	SeriesID                         string `json:"series_id"`
	SeriesManifestDigest             string `json:"series_manifest_digest"`
	CandidateBindingDigestSelf       string `json:"candidate_binding_digest"`
	PreHoldoutGreenTestReceiptDigest string `json:"pre_holdout_green_test_receipt_digest"`
	CoreLegCompletionDigest          string `json:"core_leg_completion_digest"`
	StartedAt                        string `json:"started_at"`
	State                            string `json:"state"` // started | complete-pass | complete-fail | invalid
	TerminalAt                       string `json:"terminal_at,omitempty"`
	RecoveryEventDigest              string `json:"recovery_event_digest,omitempty"`
}

// HoldoutBindingReceipt is the append-only consumption log of one holdout
// version, kept independent from the sealed dataset manifest.
type HoldoutBindingReceipt struct {
	DatasetVersion         string                 `json:"dataset_version"`
	DatasetManifestDigest  string                 `json:"dataset_manifest_digest"`
	CandidateBindingDigest string                 `json:"candidate_binding_digest"`
	FirstPrimaryStartedAt  string                 `json:"first_primary_started_at"`
	SeriesAttempts         []HoldoutSeriesAttempt `json:"series_attempts"`
	State                  string                 `json:"state"` // frozen | consumed
	ConsumedBySeries       string                 `json:"consumed_by_series"`
	// PreviousReceiptDigest chains appends (the AuthorReviewAttemptLedgerV1
	// pattern): it carries the ReceiptDigest of the version this one
	// supersedes and is empty only on first creation, so a rewritten history
	// cannot present itself as the current ledger.
	PreviousReceiptDigest string `json:"previous_receipt_digest,omitempty"`
	ReceiptDigest         string `json:"receipt_digest"`
}

// ValidateHoldoutBinding checks the closed binding lifecycle semantics.
func ValidateHoldoutBinding(b *HoldoutBindingReceipt) error {
	if b == nil {
		return errors.New("nil holdout binding receipt")
	}
	if b.DatasetVersion == "" || b.DatasetManifestDigest == "" || b.CandidateBindingDigest == "" || b.FirstPrimaryStartedAt == "" {
		return errors.New("holdout binding identity incomplete")
	}
	if len(b.SeriesAttempts) == 0 {
		return errors.New("holdout binding without any attempt")
	}
	digests := map[string]bool{}
	preHoldouts := map[string]bool{}
	coreLegs := map[string]bool{}
	for i, a := range b.SeriesAttempts {
		if a.SeriesID == "" || a.SeriesManifestDigest == "" || a.PreHoldoutGreenTestReceiptDigest == "" || a.CoreLegCompletionDigest == "" {
			return fmt.Errorf("attempt %d identity incomplete", i)
		}
		switch a.State {
		case "started", "complete-pass", "complete-fail", "invalid":
		default:
			return fmt.Errorf("attempt %d state %q invalid", i, a.State)
		}
		if a.CandidateBindingDigestV() != b.CandidateBindingDigest {
			return fmt.Errorf("attempt %d carries a different candidate binding digest", i)
		}
		if digests[a.SeriesManifestDigest] {
			return fmt.Errorf("attempt %d reuses a series manifest digest", i)
		}
		digests[a.SeriesManifestDigest] = true
		// The pre-holdout receipt and the core-leg completion are per-series
		// artifacts: a recovery series re-runs core172 from zero and earns a
		// fresh receipt, so reusing either digest is cross-series reuse.
		if preHoldouts[a.PreHoldoutGreenTestReceiptDigest] {
			return fmt.Errorf("attempt %d reuses a pre-holdout green-test receipt digest", i)
		}
		preHoldouts[a.PreHoldoutGreenTestReceiptDigest] = true
		if coreLegs[a.CoreLegCompletionDigest] {
			return fmt.Errorf("attempt %d reuses a core-leg completion digest", i)
		}
		coreLegs[a.CoreLegCompletionDigest] = true
	}
	switch b.State {
	case "frozen":
		if b.ConsumedBySeries != "" {
			return errors.New("frozen binding must have null consumed_by_series")
		}
		// A complete series consumes immediately; frozen + complete-* is the
		// bound-but-unconsumed violation.
		for _, a := range b.SeriesAttempts {
			if a.State == "complete-pass" || a.State == "complete-fail" {
				return fmt.Errorf("series %s completed its holdout series but the binding is still frozen", a.SeriesID)
			}
		}
	case "consumed":
		if b.ConsumedBySeries == "" {
			return errors.New("consumed binding must name consumed_by_series")
		}
		found := false
		for _, a := range b.SeriesAttempts {
			if a.SeriesID != b.ConsumedBySeries {
				continue
			}
			found = true
			if a.State != "complete-pass" && a.State != "complete-fail" {
				return fmt.Errorf("consumed_by_series %s never completed its holdout series (state %q)", b.ConsumedBySeries, a.State)
			}
		}
		if !found {
			return fmt.Errorf("consumed_by_series %s has no attempt entry", b.ConsumedBySeries)
		}
	default:
		return fmt.Errorf("binding state %q invalid", b.State)
	}
	if b.ReceiptDigest == "" {
		return errors.New("binding receipt digest empty")
	}
	return nil
}

// CandidateBindingDigestV returns the attempt's own copy of the stable key.
func (a HoldoutSeriesAttempt) CandidateBindingDigestV() string {
	return a.CandidateBindingDigestSelf
}

// ---------- §10 ProtectedExecutionReceipt ----------

// FormalProbeKind enumerates the closed primary-stage probe matrix.
type FormalProbeKind string

const (
	FProbeProtectedRootTraverse FormalProbeKind = "protected-root-traverse"
	FProbeProtectedRootList     FormalProbeKind = "protected-root-list"
	FProbeProtectedRootRead     FormalProbeKind = "protected-root-read"
	FProbeAuditRead             FormalProbeKind = "author-review-audit-read"
	FProbeAuthorStateRead       FormalProbeKind = "author-review-state-read"
	FProbeOwnWorkspaceRead      FormalProbeKind = "own-workspace-read"
	FProbeActiveSiblingRead     FormalProbeKind = "active-sibling-workspace-read"
	FProbePriorCaseStateRead    FormalProbeKind = "prior-case-state-read"
	FProbeRetiredWorkspaceRead  FormalProbeKind = "retired-case-workspace-read"
)

// FormalAccessProbe is one recorded access observation of the primary stage
// (digests only — never a raw path).
type FormalAccessProbe struct {
	Kind                        FormalProbeKind `json:"kind"`
	TargetDigest                string          `json:"target_digest"`
	TargetAccessPolicyDigest    string          `json:"target_access_policy_digest"`
	ControllerTargetProofDigest string          `json:"controller_target_proof_digest"`
	Expected                    string          `json:"expected"` // denied | readable
	Outcome                     string          `json:"outcome"`  // permission-denied | not-found | readable
}

// ProtectedWorkerProbe is one host × worker slot's complete probe matrix.
type ProtectedWorkerProbe struct {
	Host                    string              `json:"host"`
	WorkerSlot              int                 `json:"worker_slot"`
	ChildIdentityDigest     string              `json:"child_identity_digest"`
	ExecutionTemplateDigest string              `json:"execution_template_digest"`
	AccessBoundaryDigest    string              `json:"access_boundary_digest"`
	Probes                  []FormalAccessProbe `json:"probes"`
}

// ProtectedExecutionReceipt proves the formal child can read only its own
// materialized workspace; it is created by series prepare and never by
// dataset sealing.
type ProtectedExecutionReceipt struct {
	BoundaryKind                          BoundaryKind           `json:"boundary_kind"`
	IsolationConfigDigest                 string                 `json:"isolation_config_digest"`
	ProtectedRootDigest                   string                 `json:"protected_root_digest"`
	AuthorReviewStateRootsDigest          string                 `json:"author_review_state_roots_digest"`
	FormalStateRootsDigest                string                 `json:"formal_state_roots_digest"`
	SplitStateAllocatorDigests            map[string]string      `json:"split_state_allocator_digests"`
	RequiredConcurrency                   int                    `json:"required_concurrency"`
	IsolatedWorkerCapacity                int                    `json:"isolated_worker_capacity"`
	WorkerIdentitySetDigest               string                 `json:"worker_identity_set_digest"`
	NormalizedCoreWorkerIdentitySetDigest string                 `json:"normalized_core_worker_identity_set_digest"`
	ExecutionTemplateSetDigest            string                 `json:"execution_template_set_digest"`
	CoreExecutionPlanDigest               string                 `json:"core_execution_plan_digest"`
	WorkerProbes                          []ProtectedWorkerProbe `json:"worker_probes"`
	ProbeMatrixDigest                     string                 `json:"probe_matrix_digest"`
	ProbedAt                              string                 `json:"probed_at"`
	ReceiptDigest                         string                 `json:"receipt_digest"`
}

// ValidateProtectedExecutionReceipt enforces the closed probe semantics:
// complete host × slot matrix, capacity ≥ concurrency, disjoint split
// allocators, and per-slot denied/readable outcomes.
func ValidateProtectedExecutionReceipt(r *ProtectedExecutionReceipt, plan *CoreExecutionPlanReceipt) error {
	if r == nil {
		return errors.New("nil protected execution receipt")
	}
	if !ValidBoundary(r.BoundaryKind) {
		return fmt.Errorf("boundary kind %q invalid", r.BoundaryKind)
	}
	if r.IsolationConfigDigest == "" || r.ProtectedRootDigest == "" || r.WorkerIdentitySetDigest == "" ||
		r.ExecutionTemplateSetDigest == "" || r.ProbeMatrixDigest == "" || r.ProbedAt == "" || r.ReceiptDigest == "" {
		return errors.New("protected receipt identity incomplete")
	}
	if r.CoreExecutionPlanDigest != plan.ReceiptDigest {
		return fmt.Errorf("protected receipt plan digest %s != plan %s", r.CoreExecutionPlanDigest, plan.ReceiptDigest)
	}
	if r.NormalizedCoreWorkerIdentitySetDigest != plan.NormalizedCoreWorkerIdentitySetDigest {
		return errors.New("protected receipt worker identity template drifts from the core plan")
	}
	if r.RequiredConcurrency != plan.Concurrency || r.RequiredConcurrency <= 0 {
		return fmt.Errorf("required concurrency %d, want plan concurrency %d", r.RequiredConcurrency, plan.Concurrency)
	}
	if r.IsolatedWorkerCapacity < r.RequiredConcurrency {
		return fmt.Errorf("isolated worker capacity %d < required %d", r.IsolatedWorkerCapacity, r.RequiredConcurrency)
	}
	if len(r.SplitStateAllocatorDigests) != 2 || r.SplitStateAllocatorDigests[MembershipCore172] == "" || r.SplitStateAllocatorDigests[MembershipHoldout96] == "" {
		return errors.New("protected receipt must declare disjoint core172/holdout96 allocators")
	}
	if r.SplitStateAllocatorDigests[MembershipCore172] == r.SplitStateAllocatorDigests[MembershipHoldout96] {
		return errors.New("split state allocators overlap")
	}
	if r.FormalStateRootsDigest == r.AuthorReviewStateRootsDigest {
		return errors.New("formal state roots reuse the author/review roots")
	}
	want := map[string]int{}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= r.RequiredConcurrency; slot++ {
			want[h+"\x00"+fmt.Sprint(slot)]++
		}
	}
	got := map[string][]ProtectedWorkerProbe{}
	for _, p := range r.WorkerProbes {
		key := p.Host + "\x00" + fmt.Sprint(p.WorkerSlot)
		got[key] = append(got[key], p)
	}
	for key := range want {
		if len(got[key]) != 1 {
			return fmt.Errorf("worker probe for %s appears %d times, want exactly 1", strings.ReplaceAll(key, "\x00", "/"), len(got[key]))
		}
		if err := ValidateWorkerProbe(got[key][0], plan); err != nil {
			return err
		}
	}
	for key := range got {
		if _, ok := want[key]; ok {
			continue
		}
		return fmt.Errorf("unexpected worker probe %s", strings.ReplaceAll(key, "\x00", "/"))
	}
	return nil
}

// ValidateWorkerProbe checks one slot's probe matrix against the closed
// outcome rules.
func ValidateWorkerProbe(p ProtectedWorkerProbe, plan *CoreExecutionPlanReceipt) error {
	if p.WorkerSlot < 1 || p.ChildIdentityDigest == "" || p.AccessBoundaryDigest == "" {
		return fmt.Errorf("worker probe %s/%d identity incomplete", p.Host, p.WorkerSlot)
	}
	kindSeen := map[FormalProbeKind]int{}
	for _, pr := range p.Probes {
		kindSeen[pr.Kind]++
		if pr.TargetDigest == "" || pr.TargetAccessPolicyDigest == "" {
			return fmt.Errorf("probe %s on %s/%d lacks target/policy digests", pr.Kind, p.Host, p.WorkerSlot)
		}
		switch pr.Expected {
		case "denied":
			switch pr.Outcome {
			case "permission-denied", "not-found":
				if pr.Outcome == "not-found" && pr.ControllerTargetProofDigest == "" {
					return fmt.Errorf("probe %s on %s/%d not-found without controller proof", pr.Kind, p.Host, p.WorkerSlot)
				}
			default:
				return fmt.Errorf("probe %s on %s/%d forbidden outcome %q", pr.Kind, p.Host, p.WorkerSlot, pr.Outcome)
			}
		case "readable":
			if pr.Kind != FProbeOwnWorkspaceRead {
				return fmt.Errorf("probe %s on %s/%d must not expect readable", pr.Kind, p.Host, p.WorkerSlot)
			}
			if pr.Outcome != "readable" {
				return fmt.Errorf("own-workspace probe on %s/%d observed %q", p.Host, p.WorkerSlot, pr.Outcome)
			}
		default:
			return fmt.Errorf("probe %s on %s/%d unknown expectation %q", pr.Kind, p.Host, p.WorkerSlot, pr.Expected)
		}
	}
	for _, k := range []FormalProbeKind{
		FProbeProtectedRootTraverse, FProbeProtectedRootList, FProbeProtectedRootRead,
		FProbeAuditRead, FProbeAuthorStateRead, FProbeOwnWorkspaceRead,
	} {
		if kindSeen[k] != 1 {
			return fmt.Errorf("worker probe %s/%d: %s appears %d times, want exactly 1", p.Host, p.WorkerSlot, k, kindSeen[k])
		}
	}
	_ = plan
	return nil
}

// ProbeMatrixDigestV1 is the canonical digest of the ordered probe matrix
// (data-model.md §10: "canonical digest of all ordered probes and closed
// outcomes"). It is the only accepted preimage of
// ProtectedExecutionReceipt.ProbeMatrixDigest, so a receipt cannot claim
// probe evidence it does not carry.
func ProbeMatrixDigestV1(probes []ProtectedWorkerProbe) (string, error) {
	if len(probes) == 0 {
		return "", errors.New("probe matrix empty")
	}
	return CanonicalSHA256(probes)
}

// ---------- Workspace canaries ----------

// WorkspaceCanaryReceipt proves one prepared host × worker slot materializes
// and observes its own staged canary workspace with the final invocation.
type WorkspaceCanaryReceipt struct {
	SeriesID                string `json:"series_id"`
	Host                    string `json:"host"`
	SkillDigest             string `json:"skill_digest"`
	ToolIdentityDigest      string `json:"tool_identity_digest"`
	ExecutionTemplateDigest string `json:"execution_template_digest"`
	WorkerSlot              int    `json:"worker_slot"`
	ChildIdentityDigest     string `json:"child_identity_digest"`
	AccessBoundaryDigest    string `json:"access_boundary_digest"`
	CanaryWorkspaceDigest   string `json:"canary_workspace_digest"`
	ExpectedFileDigest      string `json:"expected_file_digest"`
	ObservedCWDDigest       string `json:"observed_cwd_digest"`
	ObservedFileDigest      string `json:"observed_file_digest"`
	Status                  string `json:"status"` // pass | fail
	ReceiptDigest           string `json:"receipt_digest"`
}

// ValidateWorkspaceCanary checks one canary against the series/plan identity.
func ValidateWorkspaceCanary(c *WorkspaceCanaryReceipt, seriesID, skillDigest, toolIdentity, templateDigest string, slot int) error {
	if c == nil {
		return errors.New("nil canary receipt")
	}
	if c.SeriesID != seriesID {
		return fmt.Errorf("canary series %q, want %q", c.SeriesID, seriesID)
	}
	if c.SkillDigest != skillDigest {
		return errors.New("canary skill digest drift")
	}
	if c.ToolIdentityDigest != toolIdentity {
		return errors.New("canary tool identity drift")
	}
	if c.ExecutionTemplateDigest != templateDigest {
		return errors.New("canary execution template drift")
	}
	if c.WorkerSlot != slot {
		return fmt.Errorf("canary slot %d, want %d", c.WorkerSlot, slot)
	}
	if c.ChildIdentityDigest == "" || c.AccessBoundaryDigest == "" {
		return errors.New("canary identity incomplete")
	}
	if c.ObservedCWDDigest != c.CanaryWorkspaceDigest || c.ObservedFileDigest != c.ExpectedFileDigest {
		return errors.New("canary observation mismatch")
	}
	if c.Status != "pass" {
		return fmt.Errorf("canary status %q", c.Status)
	}
	if c.ReceiptDigest == "" {
		return errors.New("canary receipt digest empty")
	}
	return nil
}

// ---------- §9 PrimaryRunManifest / §10 CaseRunReceipt ----------

// PrimaryRunManifest is keyed by series_id + host + split + ordinal.
type PrimaryRunManifest struct {
	Mode              string         `json:"mode"` // always "primary"
	SeriesID          string         `json:"series_id"`
	Host              string         `json:"host"`
	Split             string         `json:"split"`
	Ordinal           int            `json:"ordinal"`
	ToolProvenance    ToolProvenance `json:"tool_provenance"`
	CaseIDs           []string       `json:"case_ids"`
	CaseSetDigest     string         `json:"case_set_digest"`
	CaseOrder         []string       `json:"case_order"`
	CaseOrderDigest   string         `json:"case_order_digest"`
	ExpectedCaseCount int            `json:"expected_case_count"`
	StartedAt         string         `json:"started_at"`
	CompletedAt       string         `json:"completed_at"`
	State             LifecycleState `json:"state"`
	RunDigest         string         `json:"run_digest"`
	SealDigest        string         `json:"seal_digest"`
}

// CaseStateIsolationReceipt is the private per-case isolation proof.
type CaseStateIsolationReceipt struct {
	SeriesID                        string             `json:"series_id"`
	Host                            string             `json:"host"`
	Split                           string             `json:"split"`
	Ordinal                         int                `json:"ordinal"`
	CaseID                          string             `json:"case_id"`
	WorkerSlot                      int                `json:"worker_slot"`
	ProtectedExecutionReceiptDigest string             `json:"protected_execution_receipt_digest"`
	PreparedWorkerProbeDigest       string             `json:"prepared_worker_probe_digest"`
	ChildIdentityDigest             string             `json:"child_identity_digest"`
	ExecutionTemplateDigest         string             `json:"execution_template_digest"`
	AccessBoundaryDigest            string             `json:"access_boundary_digest"`
	FreshStateRootDigest            string             `json:"fresh_state_root_digest"`
	StateAllocatorDigest            string             `json:"state_allocator_digest"`
	PriorStateProbe                 *FormalAccessProbe `json:"prior_state_probe"`
	RetiredWorkspaceProbe           *FormalAccessProbe `json:"retired_workspace_probe"`
	ResetMethod                     string             `json:"reset_method"`
	ChildTeardown                   string             `json:"child_teardown"`
	RetirementOrFinalDelete         string             `json:"retirement_or_final_delete"`
	ReceiptDigest                   string             `json:"receipt_digest"`
}

// CaseRunReceipt binds one case's primary outcome and its rejudgeable
// artifacts.
type CaseRunReceipt struct {
	CaseID                          string  `json:"case_id"`
	CasePayloadDigest               string  `json:"case_payload_digest"`
	WorkspaceDigest                 string  `json:"workspace_digest"`
	CaseStateIsolationReceiptDigest string  `json:"case_state_isolation_receipt_digest"`
	AttemptCount                    int     `json:"attempt_count"`
	Status                          string  `json:"status"` // pass | fail | runner-error
	NormalizedEventsPath            string  `json:"normalized_events_path"`
	NormalizedEventsDigest          string  `json:"normalized_events_digest"`
	RawEventsPath                   string  `json:"raw_events_path"`
	RawEventsDigest                 string  `json:"raw_events_digest"`
	StoreDumpPath                   string  `json:"store_dump_path"`
	StoreDumpDigest                 string  `json:"store_dump_digest"`
	Verdict                         Verdict `json:"verdict"`
	DurationMS                      int64   `json:"duration_ms"`
	StderrDigest                    string  `json:"stderr_digest"`
}

// ---------- §11 FailureArchive / comparison ----------

// FailureArchiveEntry is one dev-flywheel failure record.
type FailureArchiveEntry struct {
	CaseID               string   `json:"case_id"`
	Host                 string   `json:"host"`
	Split                string   `json:"split"`
	BaselineSeriesID     string   `json:"baseline_series_id"`
	BaselineRunDigests   []string `json:"baseline_run_digests"`
	OrdinalStates        []string `json:"ordinal_states"`
	BinaryMedian         int      `json:"binary_median"`
	FailureClass         string   `json:"failure_class"`
	RootCause            string   `json:"root_cause"`
	FixSkillVersion      string   `json:"fix_skill_version"`
	FixSkillDigest       string   `json:"fix_skill_digest"`
	BeforeSeriesManifest string   `json:"before_series_manifest"`
	AfterSeriesManifest  string   `json:"after_series_manifest"`
}

// FailureArchive is the immutable dev-only failure archive receipt.
type FailureArchive struct {
	SchemaVersion               int                   `json:"schema_version"`
	BaselineSkillSnapshotDigest string                `json:"baseline_skill_snapshot_digest"`
	CoreExecutionPlanDigest     string                `json:"core_execution_plan_digest"`
	ToolIdentityDigests         map[string]string     `json:"tool_identity_digests"`
	Entries                     []FailureArchiveEntry `json:"entries"`
	ArchiveDigest               string                `json:"archive_digest"`
	SealDigest                  string                `json:"seal_digest"`
}

// ---------- §12 official scoring ----------
//
// Gate routing, exact integer boundaries, medians, eligibility and the
// public report all live here as pure, deterministic logic (T044 scorer, the
// core of T050). The scorer fails closed on any missing matrix/receipt and
// never merges hosts or splits into one headline number (SC-9, state
// invariant 6).

// LowNThreshold marks a non-gating bias cell low-N below this many
// independent cases (§12 bias_diagnostics).
const LowNThreshold = 5

// ScoreGateID enumerates the closed set of named official gates. The two
// named score families are `dev_regression` (SC-1/2/3 plus the 020 pair of
// SC-4) and `generalization` (SC-1/2/3 plus the trap pair of SC-4).
type ScoreGateID string

const (
	GateImplicitWritePos ScoreGateID = "implicit-write-pos" // SC-1 ≥90%
	GateImplicitReadPos  ScoreGateID = "implicit-read-pos"  // SC-2 ≥90%
	GateImplicitNeg      ScoreGateID = "implicit-neg"       // SC-3 ≤10% (write-neg + read-neg merged)
	GateRegressionPos    ScoreGateID = "regression-020-pos" // SC-4 ≥90%, dev-only
	GateRegressionNeg    ScoreGateID = "regression-020-neg" // SC-4 ≤10%, dev-only
	GateTrapReadPos      ScoreGateID = "trap-read-pos"      // SC-4 ≥90%, holdout-only
	GateTrapNeg          ScoreGateID = "trap-neg"           // SC-4 ≤10%, holdout-only
)

// ScoreMetric is one routed gate: the closed module set it reads and which
// boundary applies. SelectTrigger filters a mixed module (the 020 regression
// module carries both polarities) on the frozen expected-trigger bit; nil
// keeps the whole module set.
type ScoreMetric struct {
	ID            ScoreGateID
	Positive      bool // true → ≥90% pass-rate gate; false → ≤10% misfire-rate gate
	Modules       []string
	SelectTrigger *bool
}

var (
	gateTriggerTrue  = true
	gateTriggerFalse = false
)

// RouteScoreMetrics returns the ordered gate list of one split. The 020
// regression pair is dev-only and the trap pair is holdout-only; a case
// module the split does not gate is reported as unrouted by the scorer and
// is never folded into the other family.
func RouteScoreMetrics(split string) ([]ScoreMetric, error) {
	switch split {
	case SplitDevRegression:
		return []ScoreMetric{
			{ID: GateImplicitWritePos, Positive: true, Modules: []string{"implicit-write-pos"}},
			{ID: GateImplicitReadPos, Positive: true, Modules: []string{"implicit-read-pos"}},
			{ID: GateImplicitNeg, Positive: false, Modules: []string{"implicit-write-neg", "implicit-read-neg"}},
			{ID: GateRegressionPos, Positive: true, Modules: []string{"regression"}, SelectTrigger: &gateTriggerTrue},
			{ID: GateRegressionNeg, Positive: false, Modules: []string{"regression"}, SelectTrigger: &gateTriggerFalse},
		}, nil
	case SplitHoldout:
		return []ScoreMetric{
			{ID: GateImplicitWritePos, Positive: true, Modules: []string{"implicit-write-pos"}},
			{ID: GateImplicitReadPos, Positive: true, Modules: []string{"implicit-read-pos"}},
			{ID: GateImplicitNeg, Positive: false, Modules: []string{"implicit-write-neg", "implicit-read-neg"}},
			{ID: GateTrapReadPos, Positive: true, Modules: []string{"trap-read-pos"}},
			{ID: GateTrapNeg, Positive: false, Modules: []string{"trap-write-neg", "trap-read-neg"}},
		}, nil
	}
	return nil, fmt.Errorf("split %q has no official score routing", split)
}

// PassesGatePositive applies the SC-1/SC-2/SC-4 ≥90% boundary as an exact
// integer comparison (pass*100 >= total*90): 90% exactly passes and no
// floating-point rounding can move the boundary.
func PassesGatePositive(pass, total int) bool {
	return total > 0 && pass*100 >= total*90
}

// PassesGateNegative applies the SC-3/SC-4 ≤10% misfire boundary as an exact
// integer comparison (misfire*100 <= total*10): 10% exactly passes.
func PassesGateNegative(misfire, total int) bool {
	return total > 0 && misfire*100 <= total*10
}

// MedianInt returns the median of the values. Formally a series carries
// exactly three ordinals, so the value is the middle of three per-run
// numerators and the gate boundary stays an exact integer comparison.
func MedianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	return sorted[(len(sorted)-1)/2]
}

// MedianBool returns the binary median of terminal states. Formal series
// always carry three ordinals, where this is the plain majority; an
// even-length tie deliberately resolves to the conservative false (fail)
// side rather than rounding a pass up.
func MedianBool(states []bool) bool {
	if len(states) == 0 {
		return false
	}
	passes := 0
	for _, s := range states {
		if s {
			passes++
		}
	}
	fails := len(states) - passes
	return passes > fails
}

// CaseScoreOutcome is the terminal per-case outcome the scorer accepts.
type CaseScoreOutcome string

const (
	CaseOutcomePass        CaseScoreOutcome = "pass"
	CaseOutcomeFail        CaseScoreOutcome = "fail"
	CaseOutcomeRunnerError CaseScoreOutcome = "runner-error"
)

// ScoreCaseSpec is the frozen identity of one scored case. Module and
// expected polarity come from the sealed dataset, never from the run.
type ScoreCaseSpec struct {
	CaseID  string
	Split   string
	Module  string
	Trigger bool
}

// CaseScoreState is one host × ordinal × case terminal outcome.
type CaseScoreState struct {
	Host    string
	Split   string
	Ordinal int
	CaseID  string
	Outcome CaseScoreOutcome
}

// BiasDiagnosticCell is one non-gating diagnostic cell (§12
// bias_diagnostics): numerator/denominator pooled over the three ordinals,
// the count of independent cases behind them, and the low-N marker.
type BiasDiagnosticCell struct {
	Label                string `json:"label"`
	Numerator            int    `json:"numerator"`
	Denominator          int    `json:"denominator"`
	IndependentCaseCount int    `json:"independent_case_count"`
	LowN                 bool   `json:"low_n"`
}

// GateScore is one host × split gate across the three ordinals.
type GateScore struct {
	ID                ScoreGateID        `json:"id"`
	Positive          bool               `json:"positive"`
	OrdinalNumerators []int              `json:"ordinal_numerators"` // index 0 → ordinal 1
	Denominator       int                `json:"denominator"`
	MedianNumerator   int                `json:"median_numerator"`
	Passed            bool               `json:"passed"`
	RunnerErrors      int                `json:"runner_errors"` // conservative numerator contributions, reported separately
	Bias              BiasDiagnosticCell `json:"bias"`
}

// HostScore is one host's official score on one split (§12 HostScore).
type HostScore struct {
	Host     string      `json:"host"`
	Split    string      `json:"split"`
	Official bool        `json:"official"`
	Gates    []GateScore `json:"gates"`
}

// Passed reports whether every routed gate of this host × split held. There
// is no partial-host mode: a host with no gate result cannot pass.
func (h HostScore) Passed() bool {
	if len(h.Gates) == 0 {
		return false
	}
	for i := range h.Gates {
		if !h.Gates[i].Passed {
			return false
		}
	}
	return true
}

// ScoreMatrix is the complete per-host × per-split score of one series. It
// deliberately carries no combined/aggregate/headline field: dev and holdout
// are never weighted, averaged or spliced into one number (state invariant
// 6), and hosts are never merged (SC-9).
type ScoreMatrix struct {
	SeriesID string              `json:"series_id"`
	Hosts    []string            `json:"hosts"`
	Scores   []HostScore         `json:"scores"`
	Unrouted map[string][]string `json:"unrouted"` // split → case ids no gate reads
}

// HostSplit returns the computed score of one host on one split, or nil.
func (m *ScoreMatrix) HostSplit(host, split string) *HostScore {
	if m == nil {
		return nil
	}
	for i := range m.Scores {
		if m.Scores[i].Host == host && m.Scores[i].Split == split {
			return &m.Scores[i]
		}
	}
	return nil
}

// ComputeOverallVerdict implements §12 overall_verdict: pass only when every
// host holds every applicable gate on both splits; any failed applicable gate
// → fail; an unusable matrix → invalid.
func ComputeOverallVerdict(m *ScoreMatrix) string {
	if m == nil || len(m.Scores) == 0 {
		return "invalid"
	}
	for i := range m.Scores {
		if !m.Scores[i].Passed() {
			return "fail"
		}
	}
	return "pass"
}

type scoreSpecKey struct{ split, caseID string }

type scoreCellKey struct {
	host, split string
	ordinal     int
	caseID      string
}

// ComputeOfficialScore turns the complete terminal case matrix into per-host
// gate results (SC-1..SC-4). It fails closed on any missing or duplicated
// matrix cell, unknown host/ordinal/outcome, unscorable split, or case the
// routing cannot place. A terminal negative-case runner-error is counted
// conservatively in the negative gate numerator and reported separately in
// GateScore.RunnerErrors.
func ComputeOfficialScore(seriesID string, hosts []string, specs []ScoreCaseSpec, states []CaseScoreState) (*ScoreMatrix, error) {
	if seriesID == "" {
		return nil, errors.New("score series_id empty")
	}
	if err := validateScoreHosts(hosts); err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, errors.New("official score case matrix empty")
	}
	specsByID := map[scoreSpecKey]ScoreCaseSpec{}
	unrouted := map[string][]string{}
	for _, s := range specs {
		if s.CaseID == "" {
			return nil, errors.New("score case spec without case id")
		}
		gates, err := RouteScoreMetrics(s.Split)
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", s.CaseID, err)
		}
		if !validModules[s.Module] {
			return nil, fmt.Errorf("case %s module %q unknown", s.CaseID, s.Module)
		}
		key := scoreSpecKey{s.Split, s.CaseID}
		if _, dup := specsByID[key]; dup {
			return nil, fmt.Errorf("case %s duplicated in split %s", s.CaseID, s.Split)
		}
		specsByID[key] = s
		if gateSelectsFor(gates, s.Module, s.Trigger) == nil {
			unrouted[s.Split] = append(unrouted[s.Split], s.CaseID)
		}
	}
	cells := map[scoreCellKey]CaseScoreOutcome{}
	for _, st := range states {
		if st.CaseID == "" || st.Split == "" {
			return nil, errors.New("score state without case/split identity")
		}
		if _, ok := specsByID[scoreSpecKey{st.Split, st.CaseID}]; !ok {
			return nil, fmt.Errorf("score state for unknown case %s/%s", st.Split, st.CaseID)
		}
		if !stringInList(hosts, st.Host) {
			return nil, fmt.Errorf("score state host %q is not part of the series", st.Host)
		}
		if !intInList(Ordinals[:], st.Ordinal) {
			return nil, fmt.Errorf("score state ordinal %d invalid", st.Ordinal)
		}
		switch st.Outcome {
		case CaseOutcomePass, CaseOutcomeFail, CaseOutcomeRunnerError:
		default:
			return nil, fmt.Errorf("case %s outcome %q unknown", st.CaseID, st.Outcome)
		}
		key := scoreCellKey{st.Host, st.Split, st.Ordinal, st.CaseID}
		if _, dup := cells[key]; dup {
			return nil, fmt.Errorf("case %s has duplicate %s ordinal %d states", st.CaseID, st.Host, st.Ordinal)
		}
		cells[key] = st.Outcome
	}
	for _, s := range specs {
		for _, h := range hosts {
			for _, o := range Ordinals {
				if _, ok := cells[scoreCellKey{h, s.Split, o, s.CaseID}]; !ok {
					return nil, fmt.Errorf("case %s missing a terminal state for %s ordinal %d", s.CaseID, h, o)
				}
			}
		}
	}
	out := &ScoreMatrix{SeriesID: seriesID, Hosts: append([]string(nil), hosts...), Unrouted: map[string][]string{}}
	for split, ids := range unrouted {
		sorted := append([]string(nil), ids...)
		sort.Strings(sorted)
		out.Unrouted[split] = sorted
	}
	for _, split := range []string{SplitDevRegression, SplitHoldout} {
		gates, err := RouteScoreMetrics(split)
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			hs := HostScore{Host: h, Split: split, Official: true}
			for _, g := range gates {
				gs := scoreGate(h, split, g, specs, cells)
				if gs == nil {
					continue // the split carries no case for this gate
				}
				hs.Gates = append(hs.Gates, *gs)
			}
			if len(hs.Gates) > 0 {
				out.Scores = append(out.Scores, hs)
			}
		}
	}
	if len(out.Scores) == 0 {
		return nil, errors.New("official score produced no gate results")
	}
	return out, nil
}

// gateSelectsFor returns the first gate of the list that reads this case, or
// nil when the split routes the module nowhere.
func gateSelectsFor(gates []ScoreMetric, module string, trigger bool) *ScoreMetric {
	for i := range gates {
		g := &gates[i]
		if !stringInList(g.Modules, module) {
			continue
		}
		if g.SelectTrigger != nil && *g.SelectTrigger != trigger {
			continue
		}
		return g
	}
	return nil
}

// scoreGate computes one gate's three ordinal numerators. It returns nil when
// the split carries no case for the gate: an absent gate is never reported as
// passing.
func scoreGate(host, split string, g ScoreMetric, specs []ScoreCaseSpec, cells map[scoreCellKey]CaseScoreOutcome) *GateScore {
	var ids []string
	for _, s := range specs {
		if s.Split == split && gateSelectsFor([]ScoreMetric{g}, s.Module, s.Trigger) != nil {
			ids = append(ids, s.CaseID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	nums := make([]int, len(Ordinals))
	pooledNum, pooledDen, runners := 0, 0, 0
	for _, o := range Ordinals {
		for _, id := range ids {
			out := cells[scoreCellKey{host, split, o, id}]
			// Positive gate: a pass is a hit. Negative gate: anything that is
			// not a pass is a misfire — including a terminal runner-error,
			// which is the conservative reading of an unknown outcome.
			hit := (out == CaseOutcomePass) == g.Positive
			if out == CaseOutcomeRunnerError {
				runners++
			}
			if hit {
				nums[o-1]++
				pooledNum++
			}
			pooledDen++
		}
	}
	median := MedianInt(nums)
	passed := PassesGateNegative(median, len(ids))
	if g.Positive {
		passed = PassesGatePositive(median, len(ids))
	}
	return &GateScore{
		ID: g.ID, Positive: g.Positive,
		OrdinalNumerators: nums, Denominator: len(ids),
		MedianNumerator: median, Passed: passed, RunnerErrors: runners,
		Bias: BiasDiagnosticCell{
			Label: string(g.ID), Numerator: pooledNum, Denominator: pooledDen,
			IndependentCaseCount: len(ids), LowN: len(ids) < LowNThreshold,
		},
	}
}

// validateScoreHosts requires exactly the three formal hosts with no
// duplicates: SC-9's host-specific gates have no partial-host mode.
func validateScoreHosts(hosts []string) error {
	if len(hosts) != 3 {
		return fmt.Errorf("score hosts %d, want exactly 3", len(hosts))
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		if !validHosts[h] {
			return fmt.Errorf("score host %q is not a formal host", h)
		}
		if seen[h] {
			return fmt.Errorf("score host %q duplicated", h)
		}
		seen[h] = true
	}
	return nil
}

func stringInList(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func intInList(list []int, want int) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// MembershipOfSplit maps a scored split to the membership it carries.
func MembershipOfSplit(split string) (string, error) {
	switch split {
	case SplitDevRegression:
		return MembershipCore172, nil
	case SplitHoldout:
		return MembershipHoldout96, nil
	}
	return "", fmt.Errorf("split %q is not a scored split", split)
}

// ---------- §12 OfficialScoreReport ----------

// GreenTestDigestPair binds the two mandatory green-test receipts of a scored
// series (§12 green_test_receipt_digests).
type GreenTestDigestPair struct {
	SeriesPrepare string `json:"series_prepare"`
	PreHoldout    string `json:"pre_holdout"`
}

// SupplementalCrossHost is the only permitted cross-host summary. It may
// never gate (SC-9) and its field set deliberately carries no score.
type SupplementalCrossHost struct {
	NonGating bool                 `json:"non_gating"`
	Note      string               `json:"note"`
	Cells     []BiasDiagnosticCell `json:"cells"`
}

// OfficialScoreReport is the only artifact allowed to carry an official
// score, and only for a complete official-dual series. It binds the scored
// series, core plan, package, protected execution, workspace canaries, the
// holdout binding and BOTH green-test receipt digests; it has no field that
// can hold a merged cross-host or cross-split headline number.
type OfficialScoreReport struct {
	SeriesID                            string                    `json:"series_id"`
	SeriesManifestDigest                string                    `json:"series_manifest_digest"`
	CoreExecutionPlanDigest             string                    `json:"core_execution_plan_digest"`
	ProtectedExecutionReceiptDigest     string                    `json:"protected_execution_receipt_digest"`
	WorkspaceCanaryReceiptDigests       map[string]map[int]string `json:"workspace_canary_receipt_digests"`
	SkillPackageValidationReceiptDigest string                    `json:"skill_package_validation_receipt_digest"`
	SkillSnapshotDigest                 string                    `json:"skill_snapshot_digest"`
	SkillSnapshotAnchorDigest           string                    `json:"skill_snapshot_anchor_digest"`
	CandidateBindingDigest              string                    `json:"candidate_binding_digest"`
	HoldoutBindingReceiptDigest         string                    `json:"holdout_binding_receipt_digest"`
	GreenTestReceiptDigests             GreenTestDigestPair       `json:"green_test_receipt_digests"`
	DevRegression                       []HostScore               `json:"dev_regression"`
	Generalization                      []HostScore               `json:"generalization"`
	SupplementalCrossHost               *SupplementalCrossHost    `json:"supplemental_cross_host,omitempty"`
	OverallVerdict                      string                    `json:"overall_verdict"` // pass | fail | invalid
	DiagnosticArtifactsUsed             bool                      `json:"diagnostic_artifacts_used"`
	BiasDiagnostics                     []BiasDiagnosticCell      `json:"bias_diagnostics"`
	ReportDigest                        string                    `json:"report_digest"`
	SealDigest                          string                    `json:"seal_digest"`
}

// BuildOfficialScoreReport assembles the digest-bound public report from the
// eligibility bundle and its computed matrix. Callers must still run
// ValidateOfficialScoreReport before publishing it.
func BuildOfficialScoreReport(in *ScoreEligibilityInput, m *ScoreMatrix) *OfficialScoreReport {
	r := &OfficialScoreReport{
		SeriesID:                            in.Manifest.SeriesID,
		SeriesManifestDigest:                in.Manifest.ManifestDigest,
		CoreExecutionPlanDigest:             in.Manifest.CoreExecutionPlanDigest,
		ProtectedExecutionReceiptDigest:     in.Manifest.ProtectedExecutionReceiptDigest,
		WorkspaceCanaryReceiptDigests:       in.Manifest.WorkspaceCanaryReceiptDigests,
		SkillPackageValidationReceiptDigest: in.Manifest.SkillPackageValidationReceiptDigest,
		SkillSnapshotDigest:                 in.Manifest.SkillSnapshotDigest,
		SkillSnapshotAnchorDigest:           in.Manifest.SkillSnapshotAnchorDigest,
		CandidateBindingDigest:              in.Manifest.CandidateBindingDigest,
		HoldoutBindingReceiptDigest:         in.Binding.ReceiptDigest,
		GreenTestReceiptDigests: GreenTestDigestPair{
			SeriesPrepare: in.SeriesPrepare.ReceiptDigest,
			PreHoldout:    in.PreHoldout.ReceiptDigest,
		},
		OverallVerdict: ComputeOverallVerdict(m),
	}
	for _, hs := range m.Scores {
		switch hs.Split {
		case SplitDevRegression:
			r.DevRegression = append(r.DevRegression, hs)
		case SplitHoldout:
			r.Generalization = append(r.Generalization, hs)
		}
		for _, g := range hs.Gates {
			r.BiasDiagnostics = append(r.BiasDiagnostics, g.Bias)
		}
	}
	return r
}

// ---------- §12 score eligibility (fail-closed) ----------

// RunKey identifies one primary run inside a series.
type RunKey struct {
	Host    string
	Split   string
	Ordinal int
}

// ScoreEligibilityInput is the complete evidence bundle the official scorer
// requires before it may read a single case state. Every check here is
// structural; the execution surfaces re-verify digests against disk when
// they load the artifacts (T045-T051).
type ScoreEligibilityInput struct {
	Manifest      *FormalSeriesManifest
	Plan          *CoreExecutionPlanReceipt
	Protected     *ProtectedExecutionReceipt
	Canaries      map[string]map[int]*WorkspaceCanaryReceipt // host → worker slot → receipt
	Package       *SkillPackageValidationReceipt
	SeriesPrepare *GreenTestReceipt
	PreHoldout    *GreenTestReceipt
	Runs          map[RunKey]*PrimaryRunManifest
	Binding       *HoldoutBindingReceipt
}

// ValidateOfficialScoreEligibility is the scorer's fail-closed gate: a
// missing host/split/ordinal/case receipt, a bad package/protected/canary/
// worker-slot receipt, a missing/failed/wrong-suite/post-hoc/drifted
// series-prepare or pre-holdout green receipt, a recovery-series green-test
// mismatch, any other digest mismatch, an invalid-series receipt, or any
// cross-series splice all reject the score.
func ValidateOfficialScoreEligibility(in *ScoreEligibilityInput) error {
	if in == nil {
		return errors.New("nil score eligibility input")
	}
	if in.Manifest == nil {
		return errors.New("score eligibility requires the sealed series manifest")
	}
	if in.Manifest.Purpose != PurposeOfficialDual {
		return fmt.Errorf("purpose %q can never enter the official scorer", in.Manifest.Purpose)
	}
	switch in.Manifest.State {
	case StateSealed, StateComplete:
	default:
		return fmt.Errorf("series state %q cannot be scored", in.Manifest.State)
	}
	if err := ValidateFormalSeriesManifest(in.Manifest); err != nil {
		return err
	}
	if err := ValidateCoreExecutionPlan(in.Plan); err != nil {
		return fmt.Errorf("core execution plan unusable: %w", err)
	}
	if in.Plan.ReceiptDigest != in.Manifest.CoreExecutionPlanDigest {
		return fmt.Errorf("plan digest %s != series bound %s", in.Plan.ReceiptDigest, in.Manifest.CoreExecutionPlanDigest)
	}
	if err := scorePackageReceipt(in); err != nil {
		return err
	}
	if err := scoreProtectedReceipt(in); err != nil {
		return err
	}
	if err := scoreWorkspaceCanaries(in); err != nil {
		return err
	}
	if err := scoreGreenReceipts(in); err != nil {
		return err
	}
	if err := scoreRuns(in); err != nil {
		return err
	}
	return scoreHoldoutBinding(in)
}

// scorePackageReceipt binds the exact-skill package validation receipt: it
// must be passing, self-verifying and bound to the scored snapshot.
func scorePackageReceipt(in *ScoreEligibilityInput) error {
	r := in.Package
	if r == nil || !r.Passed {
		return errors.New("package validation receipt missing or failed")
	}
	if r.ReceiptDigest != in.Manifest.SkillPackageValidationReceiptDigest {
		return fmt.Errorf("package receipt digest %q != series bound %q", r.ReceiptDigest, in.Manifest.SkillPackageValidationReceiptDigest)
	}
	if r.SnapshotDigest != in.Manifest.SkillSnapshotDigest || r.SnapshotAnchorDigest != in.Manifest.SkillSnapshotAnchorDigest {
		return errors.New("package receipt is not bound to the scored skill snapshot")
	}
	if r.SkillDigest != in.Manifest.SkillDigest {
		return errors.New("package receipt skill digest drift")
	}
	d, err := receiptDigestPV(r)
	if err != nil {
		return err
	}
	if d != r.ReceiptDigest {
		return errors.New("package validation receipt digest mismatch (post-hoc mutation)")
	}
	return nil
}

// scoreProtectedReceipt requires the sealed protected-execution receipt of
// exactly this series.
func scoreProtectedReceipt(in *ScoreEligibilityInput) error {
	if in.Protected == nil {
		return errors.New("protected execution receipt missing")
	}
	if in.Protected.ReceiptDigest != in.Manifest.ProtectedExecutionReceiptDigest {
		return fmt.Errorf("protected receipt digest %q != series bound %q", in.Protected.ReceiptDigest, in.Manifest.ProtectedExecutionReceiptDigest)
	}
	return ValidateProtectedExecutionReceipt(in.Protected, in.Plan)
}

// scoreWorkspaceCanaries requires every usable host × worker slot the plan
// declares to carry a passing, identity-matching canary bound in the
// manifest, and rejects canaries naming slots outside the prepared set.
func scoreWorkspaceCanaries(in *ScoreEligibilityInput) error {
	if len(in.Manifest.WorkspaceCanaryReceiptDigests) == 0 {
		return errors.New("series manifest binds no workspace canary receipts")
	}
	for _, h := range in.Plan.Hosts {
		for slot, c := range in.Canaries[h] {
			if c == nil {
				return fmt.Errorf("workspace canary receipt for %s slot %d is nil", h, slot)
			}
			if slot < 1 || slot > in.Plan.Concurrency {
				return fmt.Errorf("workspace canary for %s names unknown worker slot %d", h, slot)
			}
		}
		for slot := 1; slot <= in.Plan.Concurrency; slot++ {
			c := in.Canaries[h][slot]
			if c == nil {
				return fmt.Errorf("workspace canary receipt missing for %s worker slot %d", h, slot)
			}
			if err := ValidateWorkspaceCanary(c, in.Manifest.SeriesID, in.Manifest.SkillDigest,
				in.Plan.ToolIdentityDigests[h], in.Plan.NormalizedCoreExecutionTemplateDigest, slot); err != nil {
				return err
			}
			if got := in.Manifest.WorkspaceCanaryReceiptDigests[h][slot]; got != c.ReceiptDigest {
				return fmt.Errorf("manifest canary digest for %s slot %d is %q, receipt says %q", h, slot, got, c.ReceiptDigest)
			}
		}
	}
	return nil
}

// greenSelfDigest recomputes a receipt's digest; drift means the receipt was
// mutated after it was sealed (post-hoc).
func greenSelfDigest(r *GreenTestReceipt) error {
	d, err := receiptDigest(r)
	if err != nil {
		return err
	}
	if d != r.ReceiptDigest {
		return fmt.Errorf("green test receipt %s digest mismatch (post-hoc mutation)", r.Suite)
	}
	return nil
}

// greenReceiptStructure is the closed structural read of one green receipt.
func greenReceiptStructure(r *GreenTestReceipt, wantSuite string) error {
	if r == nil {
		return fmt.Errorf("missing %s green test receipt", wantSuite)
	}
	if !fixedSuites[r.Suite] {
		return fmt.Errorf("green receipt suite %q is not a fixed suite", r.Suite)
	}
	if r.Suite != wantSuite {
		return fmt.Errorf("wrong-suite receipt: got %q want %q", r.Suite, wantSuite)
	}
	if !r.Passed {
		return fmt.Errorf("%s green test receipt is failed", wantSuite)
	}
	if len(r.Commands) == 0 {
		return fmt.Errorf("%s green test receipt carries no command evidence", wantSuite)
	}
	for _, c := range r.Commands {
		if c.ExitCode != 0 {
			return fmt.Errorf("command %s exit=%d in a passed receipt", c.Name, c.ExitCode)
		}
	}
	if r.CreatedAt == "" {
		return fmt.Errorf("%s green test receipt carries no creation instant", wantSuite)
	}
	return greenSelfDigest(r)
}

// scoreGreenReceipts requires the series-prepare receipt bound by the
// manifest and the fresh pre-holdout receipt of exactly this series.
func scoreGreenReceipts(in *ScoreEligibilityInput) error {
	sp := in.SeriesPrepare
	if err := greenReceiptStructure(sp, SuiteSeriesPrepare); err != nil {
		return err
	}
	if sp.ReceiptDigest != in.Manifest.GreenTestReceiptDigest {
		return fmt.Errorf("series-prepare receipt digest %q != manifest bound %q", sp.ReceiptDigest, in.Manifest.GreenTestReceiptDigest)
	}
	if sp.StableIdentityDigest == nil || *sp.StableIdentityDigest != in.Manifest.SeriesPrepareIdentityDigest {
		return errors.New("series-prepare receipt stable identity mismatch")
	}
	if deref(sp.SnapshotDigest) != in.Manifest.SkillSnapshotDigest {
		return errors.New("series-prepare receipt snapshot binding mismatch")
	}
	if deref(sp.PackageValidationReceiptDigest) != in.Manifest.SkillPackageValidationReceiptDigest {
		return errors.New("series-prepare receipt package-validation binding mismatch")
	}
	if sp.RunnerDigest != in.Manifest.RunnerDigest || sp.JudgeRuleDigest != in.Manifest.JudgeRuleDigest {
		return errors.New("series-prepare receipt runner/judge digest drift")
	}
	ph := in.PreHoldout
	if err := greenReceiptStructure(ph, SuitePreHoldout); err != nil {
		return err
	}
	if deref(ph.SeriesManifestDigest) != in.Manifest.ManifestDigest {
		return errors.New("pre-holdout receipt is not bound to this series manifest")
	}
	if deref(ph.CandidateBindingDigest) != in.Manifest.CandidateBindingDigest {
		return errors.New("pre-holdout receipt candidate binding mismatch")
	}
	if ph.RunnerDigest != in.Manifest.RunnerDigest || ph.JudgeRuleDigest != in.Manifest.JudgeRuleDigest {
		return errors.New("pre-holdout receipt runner/judge digest drift")
	}
	return nil
}

// scoreRuns requires the complete primary matrix: both splits × three hosts ×
// three ordinals, complete non-repeating case sets, and no run from any other
// series or any duplicate run digest.
func scoreRuns(in *ScoreEligibilityInput) error {
	want := map[RunKey]bool{}
	for _, h := range in.Manifest.Hosts {
		for _, o := range Ordinals {
			want[RunKey{h, SplitDevRegression, o}] = true
			want[RunKey{h, SplitHoldout, o}] = true
		}
	}
	for key := range in.Runs {
		if !want[key] {
			return fmt.Errorf("unexpected primary run %s/%s/ordinal %d", key.Host, key.Split, key.Ordinal)
		}
	}
	seenRunDigest := map[string]string{}
	for key := range want {
		r := in.Runs[key]
		if r == nil {
			return fmt.Errorf("missing primary run receipt %s/%s/ordinal %d", key.Host, key.Split, key.Ordinal)
		}
		if r.Mode != "primary" {
			return fmt.Errorf("run %s/%s/%d mode %q is not primary", key.Host, key.Split, key.Ordinal, r.Mode)
		}
		if r.SeriesID != in.Manifest.SeriesID {
			return fmt.Errorf("primary run %s/%s/%d belongs to series %q: cross-series splice", key.Host, key.Split, key.Ordinal, r.SeriesID)
		}
		if r.Host != key.Host || r.Split != key.Split || r.Ordinal != key.Ordinal {
			return fmt.Errorf("primary run %s/%s/%d disagrees with its own identity", key.Host, key.Split, key.Ordinal)
		}
		if r.State != StateComplete {
			return fmt.Errorf("primary run %s/%s/%d state %q is not complete", key.Host, key.Split, key.Ordinal, r.State)
		}
		if r.RunDigest == "" || r.SealDigest == "" {
			return fmt.Errorf("primary run %s/%s/%d is not sealed", key.Host, key.Split, key.Ordinal)
		}
		if prev, dup := seenRunDigest[r.RunDigest]; dup {
			return fmt.Errorf("run digest of %s reused by %s: cross-series splice", prev, key.Host)
		}
		seenRunDigest[r.RunDigest] = key.Host
		membership, err := MembershipOfSplit(key.Split)
		if err != nil {
			return err
		}
		expect, err := ExpectedQuestionCount(membership)
		if err != nil {
			return err
		}
		if r.ExpectedCaseCount != expect || len(r.CaseIDs) != expect || len(r.CaseOrder) != expect {
			return fmt.Errorf("primary run %s/%s/%d carries %d cases, want %d", key.Host, key.Split, key.Ordinal, len(r.CaseIDs), expect)
		}
		seenCase := map[string]bool{}
		for _, id := range r.CaseIDs {
			if id == "" {
				return fmt.Errorf("primary run %s/%s/%d has an empty case id", key.Host, key.Split, key.Ordinal)
			}
			if seenCase[id] {
				return fmt.Errorf("primary run %s/%s/%d repeats case %s", key.Host, key.Split, key.Ordinal, id)
			}
			seenCase[id] = true
		}
		if len(seenCase) != expect {
			return fmt.Errorf("primary run %s/%s/%d repeats case ids", key.Host, key.Split, key.Ordinal)
		}
	}
	return nil
}

// scoreHoldoutBinding enforces state invariant 27: only the one complete
// recovery series this report references may have produced a score. Prior
// invalid series stay in the ledger as non-scoring evidence; their runs,
// manifests and pre-holdout receipts are never reusable.
func scoreHoldoutBinding(in *ScoreEligibilityInput) error {
	b := in.Binding
	if b == nil {
		return errors.New("holdout binding receipt missing")
	}
	if err := ValidateHoldoutBinding(b); err != nil {
		return err
	}
	if b.CandidateBindingDigest != in.Manifest.CandidateBindingDigest {
		return errors.New("holdout binding is not bound to this series' stable candidate digest")
	}
	if b.DatasetManifestDigest != in.Manifest.DatasetManifests[MembershipHoldout96] {
		return errors.New("holdout binding dataset manifest mismatch")
	}
	if b.State != "consumed" || b.ConsumedBySeries != in.Manifest.SeriesID {
		return fmt.Errorf("holdout binding is %q by series %q; series %s may not be scored", b.State, b.ConsumedBySeries, in.Manifest.SeriesID)
	}
	var scored *HoldoutSeriesAttempt
	complete := 0
	for i := range b.SeriesAttempts {
		a := &b.SeriesAttempts[i]
		if a.State != "complete-pass" && a.State != "complete-fail" {
			continue
		}
		complete++
		if a.SeriesID == in.Manifest.SeriesID {
			scored = a
		}
	}
	if complete != 1 || scored == nil {
		return fmt.Errorf("binding ledger carries %d complete series; a report may reference exactly one complete recovery series", complete)
	}
	if scored.CandidateBindingDigestV() != in.Manifest.CandidateBindingDigest {
		return errors.New("scored attempt stable candidate digest mismatch")
	}
	if scored.SeriesManifestDigest != in.Manifest.ManifestDigest {
		return errors.New("scored attempt series manifest digest mismatch")
	}
	if scored.PreHoldoutGreenTestReceiptDigest != in.PreHoldout.ReceiptDigest {
		return errors.New("recovery-series green test mismatch: pre-holdout receipt is not the scored attempt's")
	}
	if scored.CoreLegCompletionDigest == "" || deref(in.PreHoldout.CoreLegCompletionDigest) != scored.CoreLegCompletionDigest {
		return errors.New("recovery-series green test mismatch: core-leg completion digest drift")
	}
	return nil
}

// ValidateOfficialScoreReport binds every public digest field of a report to
// its eligibility bundle and re-derives the verdict from the matrix. It fails
// closed on any digest disagreement, a missing host in a family, a gate that
// disagrees with the computed score, a gating supplemental summary, or a
// malformed diagnostic cell.
func ValidateOfficialScoreReport(r *OfficialScoreReport, in *ScoreEligibilityInput, m *ScoreMatrix) error {
	if r == nil {
		return errors.New("nil official score report")
	}
	if err := ValidateOfficialScoreEligibility(in); err != nil {
		return err
	}
	if m == nil || m.SeriesID != in.Manifest.SeriesID {
		return errors.New("official report has no score matrix for this series")
	}
	switch r.OverallVerdict {
	case "pass", "fail", "invalid":
	default:
		return fmt.Errorf("overall_verdict %q invalid", r.OverallVerdict)
	}
	if want := ComputeOverallVerdict(m); r.OverallVerdict != want {
		return fmt.Errorf("overall_verdict %q does not follow from the score matrix (%q)", r.OverallVerdict, want)
	}
	if r.DiagnosticArtifactsUsed {
		return errors.New("diagnostic_artifacts_used must be false (state invariant 5)")
	}
	binds := []struct{ got, want, name string }{
		{r.SeriesID, in.Manifest.SeriesID, "series_id"},
		{r.SeriesManifestDigest, in.Manifest.ManifestDigest, "series_manifest_digest"},
		{r.CoreExecutionPlanDigest, in.Plan.ReceiptDigest, "core_execution_plan_digest"},
		{r.ProtectedExecutionReceiptDigest, in.Protected.ReceiptDigest, "protected_execution_receipt_digest"},
		{r.SkillPackageValidationReceiptDigest, in.Manifest.SkillPackageValidationReceiptDigest, "skill_package_validation_receipt_digest"},
		{r.SkillSnapshotDigest, in.Manifest.SkillSnapshotDigest, "skill_snapshot_digest"},
		{r.SkillSnapshotAnchorDigest, in.Manifest.SkillSnapshotAnchorDigest, "skill_snapshot_anchor_digest"},
		{r.CandidateBindingDigest, in.Manifest.CandidateBindingDigest, "candidate_binding_digest"},
		{r.HoldoutBindingReceiptDigest, in.Binding.ReceiptDigest, "holdout_binding_receipt_digest"},
		{r.GreenTestReceiptDigests.SeriesPrepare, in.SeriesPrepare.ReceiptDigest, "green_test_receipt_digests.series_prepare"},
		{r.GreenTestReceiptDigests.PreHoldout, in.PreHoldout.ReceiptDigest, "green_test_receipt_digests.pre_holdout"},
	}
	for _, b := range binds {
		if b.got != b.want {
			return fmt.Errorf("%s %q != sealed %q", b.name, b.got, b.want)
		}
	}
	for h, slots := range in.Manifest.WorkspaceCanaryReceiptDigests {
		for slot, want := range slots {
			if r.WorkspaceCanaryReceiptDigests[h][slot] != want {
				return fmt.Errorf("workspace canary digest %s slot %d mismatch", h, slot)
			}
		}
	}
	if err := scoreFamilyCheck(r.DevRegression, SplitDevRegression, m, in.Manifest.Hosts); err != nil {
		return err
	}
	if err := scoreFamilyCheck(r.Generalization, SplitHoldout, m, in.Manifest.Hosts); err != nil {
		return err
	}
	if r.SupplementalCrossHost != nil && !r.SupplementalCrossHost.NonGating {
		return errors.New("supplemental_cross_host must be marked non-gating")
	}
	if len(r.BiasDiagnostics) == 0 {
		return errors.New("official report carries no non-gating diagnostic cells")
	}
	for i, c := range r.BiasDiagnostics {
		if c.Denominator <= 0 || c.IndependentCaseCount <= 0 {
			return fmt.Errorf("bias cell %d (%s) has no denominator or independent case count", i, c.Label)
		}
		if c.LowN != (c.IndependentCaseCount < LowNThreshold) {
			return fmt.Errorf("bias cell %s low_n marker is wrong", c.Label)
		}
	}
	if r.ReportDigest == "" || r.SealDigest == "" {
		return errors.New("official score report is not sealed")
	}
	return nil
}

// scoreFamilyCheck requires exactly the series' three hosts, each carrying
// the routed gate set of the family's split as computed. A missing host is a
// missing score, never an implicit pass (SC-9).
func scoreFamilyCheck(family []HostScore, split string, m *ScoreMatrix, hosts []string) error {
	if len(family) != len(hosts) {
		return fmt.Errorf("%s family covers %d hosts, want %d", split, len(family), len(hosts))
	}
	seen := map[string]bool{}
	for _, hs := range family {
		if seen[hs.Host] {
			return fmt.Errorf("%s family repeats host %s", split, hs.Host)
		}
		seen[hs.Host] = true
		if hs.Split != split {
			return fmt.Errorf("%s family carries split %q", split, hs.Split)
		}
		if !hs.Official {
			return fmt.Errorf("%s/%s is not marked official", hs.Host, split)
		}
		want := m.HostSplit(hs.Host, split)
		if want == nil {
			return fmt.Errorf("%s/%s has no computed score", hs.Host, split)
		}
		if len(hs.Gates) != len(want.Gates) {
			return fmt.Errorf("%s/%s carries %d gates, want %d", hs.Host, split, len(hs.Gates), len(want.Gates))
		}
		for i := range hs.Gates {
			if !reflect.DeepEqual(hs.Gates[i], want.Gates[i]) {
				return fmt.Errorf("%s/%s gate %s disagrees with the computed score", hs.Host, split, hs.Gates[i].ID)
			}
		}
	}
	return nil
}
