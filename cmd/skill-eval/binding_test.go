package main

// T042 [P] [US4] — holdout-binding tests: no binding/consumption when the
// official core leg invalidates before holdout ordinal 1 (zero-attempt
// rejection); a fresh matching `pre-holdout` receipt plus binding-only
// creation at holdout ordinal 1; the bound-but-unconsumed frozen state while
// a holdout attempt is INVALID; append-only recovery-ledger evidence; recovery
// only with the same stable `CandidateBindingV1` digest; changed-stable-digest
// and every cross-series series-manifest/pre-holdout-receipt/core-leg reuse
// rejection; consumption only after one complete recovery/original holdout
// series; and the FailureArchive dev-regression closed split set (holdout
// inputs rejected). All fixtures are synthetic; digests are opaque tokens,
// never real artifact digests.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// ---------- fixtures ----------

// t042Binding is a fully populated stable recovery key.
func t042Binding() *CandidateBindingV1 {
	return &CandidateBindingV1{
		SchemaVersion:                       1,
		Purpose:                             PurposeOfficialDual,
		SkillSnapshotDigest:                 "digest-skill-snapshot-0001",
		SkillSnapshotAnchorDigest:           "digest-skill-snapshot-anchor-0001",
		SkillDigest:                         "digest-skill-0001",
		SkillPackageValidationReceiptDigest: "digest-skill-package-validation-0001",
		ValidatorRevision:                   "validator-r7",
		ValidatorDigest:                     "digest-validator-0001",
		RunnerRevision:                      "runner-r7",
		RunnerDigest:                        "digest-runner-0001",
		JudgeRuleDigest:                     "digest-judge-rule-0001",
		DatasetIdentities: map[string]string{
			MembershipCore172:   "digest-manifest-core172-0001",
			MembershipHoldout96: "digest-manifest-holdout96-0001",
		},
		CoreExecutionPlanDigest: "digest-core-execution-plan-0001",
		ToolIdentityDigests: map[string]string{
			HostClaude:   "digest-tool-claude-0001",
			HostCodex:    "digest-tool-codex-0001",
			HostOpenCode: "digest-tool-opencode-0001",
		},
		ToolConfigurationDigest: "digest-tool-configuration-0001",
		TimeoutSeconds:          900,
		Concurrency:             4,
		CaseOrderSeeds: map[int]string{
			1: "seed-ordinal-1",
			2: "seed-ordinal-2",
			3: "seed-ordinal-3",
		},
		ExecutionEnvironmentDigest:     "digest-execution-environment-0001",
		ProtectedExecutionPolicyDigest: "digest-protected-execution-policy-0001",
		SeriesPrepareIdentityDigest:    "digest-series-prepare-identity-0001",
	}
}

// t042Attempt is the n-th append-only attempt entry; every per-series digest
// is derived from n so distinct attempts never share artifacts.
func t042Attempt(n int, bindingDigest string) HoldoutSeriesAttempt {
	return HoldoutSeriesAttempt{
		SeriesID:                         fmt.Sprintf("series-official-%04d", n),
		SeriesManifestDigest:             fmt.Sprintf("digest-series-manifest-%04d", n),
		CandidateBindingDigestSelf:       bindingDigest,
		PreHoldoutGreenTestReceiptDigest: fmt.Sprintf("digest-preholdout-green-receipt-%04d", n),
		CoreLegCompletionDigest:          fmt.Sprintf("digest-core-leg-completion-%04d", n),
		StartedAt:                        "2026-09-01T00:00:00Z",
		State:                            "started",
	}
}

// t042Receipt binds the holdout version to the n-th series' first attempt.
func t042Receipt(t *testing.T, n int) (*HoldoutBindingReceipt, string) {
	t.Helper()
	d, err := CandidateBindingDigest(t042Binding())
	if err != nil {
		t.Fatalf("fixture binding digest: %v", err)
	}
	return &HoldoutBindingReceipt{
		DatasetVersion:         "holdout96-v1",
		DatasetManifestDigest:  "digest-dataset-manifest-holdout96-v1",
		CandidateBindingDigest: d,
		FirstPrimaryStartedAt:  "2026-09-01T00:00:00Z",
		SeriesAttempts:         []HoldoutSeriesAttempt{t042Attempt(n, d)},
		State:                  "frozen",
		ConsumedBySeries:       "",
		ReceiptDigest:          fmt.Sprintf("digest-holdout-binding-receipt-%04d", n),
	}, d
}

// t042Reject asserts a fail-closed rejection that names the violated rule.
func t042Reject(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rejection mentioning %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("rejection %q does not mention %q", err, substr)
	}
}

// t042CopyAttempt snapshots an entry so the test can prove the ledger is
// append-only (earlier entries never rewritten by a later append).
func t042CopyAttempt(a HoldoutSeriesAttempt) HoldoutSeriesAttempt { return a }

// ---------- CandidateBindingV1: the stable recovery key ----------

func TestCandidateBindingDigestDeterministic(t *testing.T) {
	d1, err := CandidateBindingDigest(t042Binding())
	if err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	d2, err := CandidateBindingDigest(t042Binding())
	if err != nil {
		t.Fatalf("identical binding rejected: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest is not deterministic: %s vs %s", d1, d2)
	}
	// Map iteration order must not influence the canonical preimage.
	b := t042Binding()
	b.DatasetIdentities = map[string]string{
		MembershipHoldout96: "digest-manifest-holdout96-0001",
		MembershipCore172:   "digest-manifest-core172-0001",
	}
	b.ToolIdentityDigests = map[string]string{
		HostOpenCode: "digest-tool-opencode-0001",
		HostCodex:    "digest-tool-codex-0001",
		HostClaude:   "digest-tool-claude-0001",
	}
	b.CaseOrderSeeds = map[int]string{3: "seed-ordinal-3", 2: "seed-ordinal-2", 1: "seed-ordinal-1"}
	d3, err := CandidateBindingDigest(b)
	if err != nil {
		t.Fatalf("reordered binding rejected: %v", err)
	}
	if d3 != d1 {
		t.Fatalf("canonical key order leaked into the digest: %s vs %s", d1, d3)
	}
}

func TestCandidateBindingDigestFailsClosed(t *testing.T) {
	if d, err := CandidateBindingDigest(nil); err == nil || d != "" {
		t.Fatalf("nil binding accepted (digest %q)", d)
	}
	bad := t042Binding()
	bad.SchemaVersion = 2
	if _, err := CandidateBindingDigest(bad); err == nil {
		t.Error("schema_version 2 accepted")
	}
	dev := t042Binding()
	dev.Purpose = PurposeDevComparison
	if _, err := CandidateBindingDigest(dev); err == nil {
		t.Error("dev-comparison purpose accepted in a stable recovery key")
	}
}

func TestCandidateBindingDigestRejectsMissingStableInputs(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*CandidateBindingV1)
		want string
	}{
		{"skill_snapshot_digest", func(b *CandidateBindingV1) { b.SkillSnapshotDigest = "" }, "skill_snapshot_digest"},
		{"skill_snapshot_anchor_digest", func(b *CandidateBindingV1) { b.SkillSnapshotAnchorDigest = "" }, "skill_snapshot_anchor_digest"},
		{"skill_digest", func(b *CandidateBindingV1) { b.SkillDigest = "" }, "skill_digest"},
		{"package_validation", func(b *CandidateBindingV1) { b.SkillPackageValidationReceiptDigest = "" }, "skill_package_validation_receipt_digest"},
		{"validator_revision", func(b *CandidateBindingV1) { b.ValidatorRevision = "" }, "validator_revision"},
		{"validator_digest", func(b *CandidateBindingV1) { b.ValidatorDigest = "" }, "validator_digest"},
		{"runner_revision", func(b *CandidateBindingV1) { b.RunnerRevision = "" }, "runner_revision"},
		{"runner_digest", func(b *CandidateBindingV1) { b.RunnerDigest = "" }, "runner_digest"},
		{"judge_rule_digest", func(b *CandidateBindingV1) { b.JudgeRuleDigest = "" }, "judge_rule_digest"},
		{"core_execution_plan", func(b *CandidateBindingV1) { b.CoreExecutionPlanDigest = "" }, "core_execution_plan_digest"},
		{"tool_configuration", func(b *CandidateBindingV1) { b.ToolConfigurationDigest = "" }, "tool_configuration_digest"},
		{"execution_environment", func(b *CandidateBindingV1) { b.ExecutionEnvironmentDigest = "" }, "execution_environment_digest"},
		{"protected_execution_policy", func(b *CandidateBindingV1) { b.ProtectedExecutionPolicyDigest = "" }, "protected_execution_policy_digest"},
		{"series_prepare_identity", func(b *CandidateBindingV1) { b.SeriesPrepareIdentityDigest = "" }, "series_prepare_identity_digest"},
		{"timeout_zero", func(b *CandidateBindingV1) { b.TimeoutSeconds = 0 }, "timeout_seconds"},
		{"timeout_negative", func(b *CandidateBindingV1) { b.TimeoutSeconds = -1 }, "timeout_seconds"},
		{"concurrency_zero", func(b *CandidateBindingV1) { b.Concurrency = 0 }, "concurrency"},
		{"dataset_identities_one", func(b *CandidateBindingV1) { b.DatasetIdentities = map[string]string{MembershipCore172: "x"} }, "dataset_identities"},
		{"dataset_identities_three", func(b *CandidateBindingV1) {
			b.DatasetIdentities = map[string]string{MembershipCore172: "x", MembershipHoldout96: "y", MembershipDevExt: "z"}
		}, "dataset_identities"},
		{"dataset_identities_nil", func(b *CandidateBindingV1) { b.DatasetIdentities = nil }, "dataset_identities"},
		{"tool_identity_two_hosts", func(b *CandidateBindingV1) { delete(b.ToolIdentityDigests, HostCodex) }, "tool_identity_digests"},
		{"tool_identity_four_hosts", func(b *CandidateBindingV1) { b.ToolIdentityDigests["stray"] = "x" }, "tool_identity_digests"},
		{"case_order_seeds_two", func(b *CandidateBindingV1) { delete(b.CaseOrderSeeds, 3) }, "case_order_seeds"},
		{"case_order_seeds_nil", func(b *CandidateBindingV1) { b.CaseOrderSeeds = nil }, "case_order_seeds"},
	}
	for _, tc := range cases {
		b := t042Binding()
		tc.mut(b)
		_, err := CandidateBindingDigest(b)
		t042Reject(t, err, tc.want)
	}
}

func TestCandidateBindingDigestChangesOnEveryStableInput(t *testing.T) {
	base, err := CandidateBindingDigest(t042Binding())
	if err != nil {
		t.Fatalf("baseline binding rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*CandidateBindingV1)
	}{
		{"skill_snapshot_digest", func(b *CandidateBindingV1) { b.SkillSnapshotDigest = "digest-skill-snapshot-0002" }},
		{"skill_snapshot_anchor_digest", func(b *CandidateBindingV1) { b.SkillSnapshotAnchorDigest = "digest-skill-snapshot-anchor-0002" }},
		{"skill_digest", func(b *CandidateBindingV1) { b.SkillDigest = "digest-skill-0002" }},
		{"package_validation_receipt", func(b *CandidateBindingV1) {
			b.SkillPackageValidationReceiptDigest = "digest-skill-package-validation-0002"
		}},
		{"validator_revision", func(b *CandidateBindingV1) { b.ValidatorRevision = "validator-r8" }},
		{"validator_digest", func(b *CandidateBindingV1) { b.ValidatorDigest = "digest-validator-0002" }},
		{"runner_revision", func(b *CandidateBindingV1) { b.RunnerRevision = "runner-r8" }},
		{"runner_digest", func(b *CandidateBindingV1) { b.RunnerDigest = "digest-runner-0002" }},
		{"judge_rule_digest", func(b *CandidateBindingV1) { b.JudgeRuleDigest = "digest-judge-rule-0002" }},
		{"core_dataset_identity", func(b *CandidateBindingV1) { b.DatasetIdentities[MembershipCore172] = "digest-manifest-core172-0002" }},
		{"holdout_dataset_identity", func(b *CandidateBindingV1) {
			b.DatasetIdentities[MembershipHoldout96] = "digest-manifest-holdout96-0002"
		}},
		{"core_execution_plan", func(b *CandidateBindingV1) { b.CoreExecutionPlanDigest = "digest-core-execution-plan-0002" }},
		{"tool_identity_host", func(b *CandidateBindingV1) { b.ToolIdentityDigests[HostCodex] = "digest-tool-codex-0002" }},
		{"tool_configuration", func(b *CandidateBindingV1) { b.ToolConfigurationDigest = "digest-tool-configuration-0002" }},
		{"timeout_seconds", func(b *CandidateBindingV1) { b.TimeoutSeconds = 1200 }},
		{"concurrency", func(b *CandidateBindingV1) { b.Concurrency = 6 }},
		{"case_order_seed", func(b *CandidateBindingV1) { b.CaseOrderSeeds[2] = "seed-ordinal-2b" }},
		{"execution_environment", func(b *CandidateBindingV1) { b.ExecutionEnvironmentDigest = "digest-execution-environment-0002" }},
		{"protected_execution_policy", func(b *CandidateBindingV1) {
			b.ProtectedExecutionPolicyDigest = "digest-protected-execution-policy-0002"
		}},
		{"series_prepare_identity", func(b *CandidateBindingV1) { b.SeriesPrepareIdentityDigest = "digest-series-prepare-identity-0002" }},
	}
	for _, tc := range cases {
		b := t042Binding()
		tc.mut(b)
		d, err := CandidateBindingDigest(b)
		if err != nil {
			t.Errorf("%s: mutated binding rejected: %v", tc.name, err)
			continue
		}
		if d == base {
			t.Errorf("%s: changing a stable input left the recovery key unchanged", tc.name)
		}
	}
}

func TestCandidateBindingPreimageExcludesPerAttemptContext(t *testing.T) {
	// data-model.md §7: the stable recovery key explicitly excludes series_id,
	// the series manifest digest, the protected receipt digest, canary
	// digests, run/case/state roots, timestamps, and every per-attempt
	// pre-holdout receipt. Those inputs do not exist in the preimage struct,
	// so no per-attempt value can ever reach the digest.
	typ := reflect.TypeOf(CandidateBindingV1{})
	tags := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			tags[tag] = true
		}
	}
	want := []string{
		"schema_version", "purpose",
		"skill_snapshot_digest", "skill_snapshot_anchor_digest", "skill_digest",
		"skill_package_validation_receipt_digest", "validator_revision", "validator_digest",
		"runner_revision", "runner_digest", "judge_rule_digest",
		"dataset_identities", "core_execution_plan_digest",
		"tool_identity_digests", "tool_configuration_digest",
		"timeout_seconds", "concurrency", "case_order_seeds",
		"execution_environment_digest", "protected_execution_policy_digest",
		"series_prepare_identity_digest",
	}
	if len(tags) != len(want) {
		t.Errorf("CandidateBindingV1 has %d json fields, want exactly the %d stable inputs", len(tags), len(want))
	}
	for _, k := range want {
		if !tags[k] {
			t.Errorf("CandidateBindingV1 lost stable input %q", k)
		}
	}
	excluded := []string{
		"series_id", "series_manifest_digest", "manifest_digest",
		"protected_execution_receipt_digest", "workspace_canary_receipt_digests",
		"pre_holdout_green_test_receipt_digest", "core_leg_completion_digest",
		"recovery_event_digest", "receipt_digest", "run_digest", "case_set_digest",
		"created_at", "started_at", "terminal_at", "probed_at",
		"first_primary_started_at", "consumed_by_series", "state", "seal_digest",
	}
	for _, ex := range excluded {
		if tags[ex] {
			t.Errorf("CandidateBindingV1 preimage carries per-attempt context %q", ex)
		}
	}
}

// ---------- binding lifecycle: no binding before holdout ordinal 1 ----------

func TestHoldoutBindingRequiresAttemptBeforeOrdinalOne(t *testing.T) {
	if err := ValidateHoldoutBinding(nil); err == nil {
		t.Fatal("nil binding receipt accepted")
	}
	// The core leg invalidated before holdout ordinal 1: no attempt entry may
	// exist, therefore no binding may exist and nothing may be consumed.
	empty := &HoldoutBindingReceipt{
		DatasetVersion:         "holdout96-v1",
		DatasetManifestDigest:  "digest-dataset-manifest-holdout96-v1",
		CandidateBindingDigest: "digest-candidate-binding-0001",
		FirstPrimaryStartedAt:  "2026-09-01T00:00:00Z",
		State:                  "frozen",
		ReceiptDigest:          "digest-holdout-binding-receipt-0000",
	}
	t042Reject(t, ValidateHoldoutBinding(empty), "without any attempt")
}

func TestHoldoutBindingIdentityIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*HoldoutBindingReceipt)
		want string
	}{
		{"dataset_version", func(b *HoldoutBindingReceipt) { b.DatasetVersion = "" }, "identity incomplete"},
		{"dataset_manifest", func(b *HoldoutBindingReceipt) { b.DatasetManifestDigest = "" }, "identity incomplete"},
		{"candidate_binding", func(b *HoldoutBindingReceipt) { b.CandidateBindingDigest = "" }, "identity incomplete"},
		{"first_primary_started_at", func(b *HoldoutBindingReceipt) { b.FirstPrimaryStartedAt = "" }, "identity incomplete"},
		{"receipt_digest", func(b *HoldoutBindingReceipt) { b.ReceiptDigest = "" }, "receipt digest empty"},
	} {
		b, _ := t042Receipt(t, 1)
		tc.mut(b)
		t042Reject(t, ValidateHoldoutBinding(b), tc.want)
	}
}

func TestHoldoutBindingAttemptIdentityCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*HoldoutSeriesAttempt)
		want string
	}{
		{"series_id", func(a *HoldoutSeriesAttempt) { a.SeriesID = "" }, "attempt 0 identity incomplete"},
		{"series_manifest", func(a *HoldoutSeriesAttempt) { a.SeriesManifestDigest = "" }, "attempt 0 identity incomplete"},
		{"pre_holdout_receipt", func(a *HoldoutSeriesAttempt) { a.PreHoldoutGreenTestReceiptDigest = "" }, "attempt 0 identity incomplete"},
		{"core_leg_completion", func(a *HoldoutSeriesAttempt) { a.CoreLegCompletionDigest = "" }, "attempt 0 identity incomplete"},
	} {
		b, _ := t042Receipt(t, 1)
		tc.mut(&b.SeriesAttempts[0])
		t042Reject(t, ValidateHoldoutBinding(b), tc.want)
	}
	// Same-digest invariant: every attempt carries the receipt's own stable key.
	b, d := t042Receipt(t, 1)
	b.SeriesAttempts[0].CandidateBindingDigestSelf = d + "-drift"
	t042Reject(t, ValidateHoldoutBinding(b), "different candidate binding digest")
}

// ---------- append-only ledger: no overwrite, no cross-series reuse ----------

func TestHoldoutBindingAppendOnlyRejectsSeriesManifestReuse(t *testing.T) {
	b, d := t042Receipt(t, 1)
	replay := t042Attempt(2, d)
	replay.SeriesManifestDigest = b.SeriesAttempts[0].SeriesManifestDigest // same manifest, new series id
	replay.SeriesID = "series-official-replay"
	b.SeriesAttempts = append(b.SeriesAttempts, replay)
	t042Reject(t, ValidateHoldoutBinding(b), "reuses a series manifest digest")
}

func TestHoldoutBindingRejectsCrossSeriesArtifactReuse(t *testing.T) {
	// The pre-holdout green-test receipt is per-series: a recovery series
	// re-runs core172 from zero and earns a fresh receipt.
	b, d := t042Receipt(t, 1)
	replayed := t042Attempt(2, d)
	replayed.PreHoldoutGreenTestReceiptDigest = b.SeriesAttempts[0].PreHoldoutGreenTestReceiptDigest
	b.SeriesAttempts = append(b.SeriesAttempts, replayed)
	t042Reject(t, ValidateHoldoutBinding(b), "reuses a pre-holdout green-test receipt digest")

	// Same for the core-leg completion: recovery must complete core172 again,
	// never inherit the previous series' completion.
	b2, d2 := t042Receipt(t, 1)
	inherited := t042Attempt(2, d2)
	inherited.CoreLegCompletionDigest = b2.SeriesAttempts[0].CoreLegCompletionDigest
	b2.SeriesAttempts = append(b2.SeriesAttempts, inherited)
	t042Reject(t, ValidateHoldoutBinding(b2), "reuses a core-leg completion digest")
}

func TestHoldoutBindingAttemptStateClosedSet(t *testing.T) {
	for _, state := range []string{"", "recovering", "complete", "pass", "failed"} {
		b, _ := t042Receipt(t, 1)
		b.SeriesAttempts[0].State = state
		t042Reject(t, ValidateHoldoutBinding(b), fmt.Sprintf("attempt 0 state %q invalid", state))
	}
}

// ---------- state machine: frozen vs consumed ----------

func TestHoldoutBindingStateMachine(t *testing.T) {
	b, _ := t042Receipt(t, 1)
	b.SeriesAttempts[0].State = "invalid"
	b.SeriesAttempts[0].TerminalAt = "2026-09-01T01:00:00Z"
	if err := ValidateHoldoutBinding(b); err != nil {
		t.Fatalf("frozen binding after an INVALID holdout attempt rejected: %v", err)
	}

	bound := t042CopyAttempt(b.SeriesAttempts[0])
	b.ConsumedBySeries = bound.SeriesID
	t042Reject(t, ValidateHoldoutBinding(b), "frozen binding must have null consumed_by_series")
	b.ConsumedBySeries = ""

	b.State = "consumed"
	t042Reject(t, ValidateHoldoutBinding(b), "consumed binding must name consumed_by_series")

	for _, state := range []string{"", "frozen-consumed", "archived", "INVALID"} {
		b2, _ := t042Receipt(t, 1)
		b2.State = state
		t042Reject(t, ValidateHoldoutBinding(b2), fmt.Sprintf("binding state %q invalid", state))
	}
}

func TestHoldoutBindingConsumptionRequiresCompleteSeries(t *testing.T) {
	// Consumption only after one complete holdout series: an INVALID or still
	// running series may never be named as the consumer.
	for _, state := range []string{"started", "invalid"} {
		b, _ := t042Receipt(t, 1)
		b.SeriesAttempts[0].State = state
		b.State = "consumed"
		b.ConsumedBySeries = b.SeriesAttempts[0].SeriesID
		t042Reject(t, ValidateHoldoutBinding(b), "never completed its holdout series")
	}

	// The consumer must be one of the appended attempts.
	b, _ := t042Receipt(t, 1)
	b.SeriesAttempts[0].State = "complete-pass"
	b.SeriesAttempts[0].TerminalAt = "2026-09-01T05:00:00Z"
	b.State = "consumed"
	b.ConsumedBySeries = "series-official-ghost"
	t042Reject(t, ValidateHoldoutBinding(b), "has no attempt entry")

	// A completed series must be consumed immediately: frozen + complete-* is
	// the bound-but-unconsumed violation.
	b2, _ := t042Receipt(t, 1)
	b2.SeriesAttempts[0].State = "complete-fail"
	b2.SeriesAttempts[0].TerminalAt = "2026-09-01T05:00:00Z"
	t042Reject(t, ValidateHoldoutBinding(b2), "still frozen")

	// Both complete outcomes consume, by the series that ran the holdout.
	for _, state := range []string{"complete-pass", "complete-fail"} {
		b3, _ := t042Receipt(t, 1)
		b3.SeriesAttempts[0].State = state
		b3.SeriesAttempts[0].TerminalAt = "2026-09-01T05:00:00Z"
		b3.State = "consumed"
		b3.ConsumedBySeries = b3.SeriesAttempts[0].SeriesID
		if err := ValidateHoldoutBinding(b3); err != nil {
			t.Fatalf("consumption after a %s series rejected: %v", state, err)
		}
	}
}

// ---------- recovery: same stable digest, fresh series, append-only ----------

func TestHoldoutBindingRecoveryChainKeepsStableDigest(t *testing.T) {
	b, bindingDigest := t042Receipt(t, 1)
	b.SeriesAttempts[0].State = "invalid"
	b.SeriesAttempts[0].TerminalAt = "2026-09-01T02:00:00Z"
	b.SeriesAttempts[0].RecoveryEventDigest = "digest-recovery-event-0001"
	if err := ValidateHoldoutBinding(b); err != nil {
		t.Fatalf("INVALID attempt must keep the binding frozen, not consume it: %v", err)
	}

	// The recovery series re-derives the SAME stable key from the same
	// CandidateBindingV1 preimage; only its series_id / manifest / runtime
	// receipts differ.
	redigest, err := CandidateBindingDigest(t042Binding())
	if err != nil {
		t.Fatalf("recovery binding digest: %v", err)
	}
	if redigest != bindingDigest {
		t.Fatalf("recovery recomputed a different stable key: %s vs %s", bindingDigest, redigest)
	}

	before := t042CopyAttempt(b.SeriesAttempts[0])
	recovery := t042Attempt(2, redigest)
	b.SeriesAttempts = append(b.SeriesAttempts, recovery)
	// While the recovery series is still running the ledger stays frozen and
	// validates: one INVALID attempt plus one live attempt.
	if err := ValidateHoldoutBinding(b); err != nil {
		t.Fatalf("same-digest recovery attempt rejected: %v", err)
	}
	if !reflect.DeepEqual(before, b.SeriesAttempts[0]) {
		t.Error("append-only ledger violated: the earlier attempt entry was rewritten")
	}
	if b.SeriesAttempts[0].RecoveryEventDigest == b.SeriesAttempts[1].RecoveryEventDigest {
		t.Error("recovery attempts must record distinct recovery-event digests")
	}

	// Completion and consumption land together: the recovery series is the
	// consumer, and the ledger closes on it.
	b.SeriesAttempts[1].State = "complete-pass"
	b.SeriesAttempts[1].TerminalAt = "2026-09-03T09:00:00Z"
	b.SeriesAttempts[1].RecoveryEventDigest = "digest-recovery-event-0002"
	b.State = "consumed"
	b.ConsumedBySeries = b.SeriesAttempts[1].SeriesID
	if err := ValidateHoldoutBinding(b); err != nil {
		t.Fatalf("consumption after a complete recovery series rejected: %v", err)
	}
}

func TestHoldoutBindingRecoveryRejectsChangedStableDigest(t *testing.T) {
	b, bindingDigest := t042Receipt(t, 1)
	b.SeriesAttempts[0].State = "invalid"
	b.SeriesAttempts[0].TerminalAt = "2026-09-01T02:00:00Z"

	// Any changed stable input (here the runner) yields a NEW recovery key and
	// therefore a new holdout version — it must never append to this ledger.
	changed := t042Binding()
	changed.RunnerDigest = "digest-runner-0002"
	newDigest, err := CandidateBindingDigest(changed)
	if err != nil {
		t.Fatalf("changed binding digest: %v", err)
	}
	if newDigest == bindingDigest {
		t.Fatal("changed stable input did not change the recovery key")
	}
	b.SeriesAttempts = append(b.SeriesAttempts, t042Attempt(2, newDigest))
	t042Reject(t, ValidateHoldoutBinding(b), "different candidate binding digest")
}

// ---------- FailureArchive: dev-regression closed split set ----------

// failureArchiveSplitAllowed is the T042 test-local stand-in for the not-yet-
// wired FailureArchive builder (T045-T051): the dev failure archive is a
// dev-flywheel artifact, so holdout inputs are rejected, never archived.
func failureArchiveSplitAllowed(split string) error {
	if split != SplitDevRegression {
		return fmt.Errorf("failure archive split %q is outside the dev-regression closed set", split)
	}
	return nil
}

// rejectHoldoutFailureArchiveInput scans a drafted archive and fails on the
// first entry whose split is not dev-regression.
func rejectHoldoutFailureArchiveInput(entries []FailureArchiveEntry) error {
	for _, e := range entries {
		if err := failureArchiveSplitAllowed(e.Split); err != nil {
			return fmt.Errorf("entry %s: %w", e.CaseID, err)
		}
	}
	return nil
}

func TestFailureArchiveRejectsHoldoutInput(t *testing.T) {
	dev := []FailureArchiveEntry{
		{CaseID: "case-dev-0001", Host: HostClaude, Split: SplitDevRegression},
		{CaseID: "case-dev-0002", Host: HostCodex, Split: SplitDevRegression},
	}
	if err := rejectHoldoutFailureArchiveInput(dev); err != nil {
		t.Fatalf("dev-regression entries rejected: %v", err)
	}
	for _, split := range []string{SplitHoldout, MembershipCore172, MembershipHoldout96, "", "DEV-REGRESSION", "dev-regression/holdout"} {
		if err := failureArchiveSplitAllowed(split); err == nil {
			t.Errorf("failure archive split %q accepted", split)
		}
	}
	leaked := append(append([]FailureArchiveEntry{}, dev...), FailureArchiveEntry{CaseID: "case-holdout-0001", Host: HostOpenCode, Split: SplitHoldout})
	err := rejectHoldoutFailureArchiveInput(leaked)
	if err == nil {
		t.Fatal("holdout input accepted into the dev failure archive")
	}
	if !strings.Contains(err.Error(), "case-holdout-0001") || !strings.Contains(err.Error(), SplitHoldout) {
		t.Fatalf("rejection %q does not identify the offending holdout entry", err)
	}
}
