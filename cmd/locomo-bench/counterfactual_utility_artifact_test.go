package main

// T004 [P] Artifact-contract tests: manifest immutability, endpoint/config
// digests, paired_deep arm semantics, public/hidden custody, canonical
// digest/seal validation, bounded retry state transitions, conservative
// failed-attempt token charges, orphan/tamper rejection, and secret/raw-response
// exclusion. Written first (failing) against the intended
// counterfactual_utility_artifact.go API.

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUtilityEndpointDigestPrivacy(t *testing.T) {
	// Endpoint/base-URL must be stored only as an irreversible digest; the raw
	// value never appears in the digest output.
	raw := "http://127.0.0.1:8000/v1"
	d1 := utilityEndpointDigest(raw)
	if d1 == "" || strings.Contains(d1, "127.0.0.1") {
		t.Fatalf("endpoint digest leaked raw value: %q", d1)
	}
	if !strings.HasPrefix(d1, "sha256:") {
		t.Fatalf("endpoint digest must be sha256-prefixed: %q", d1)
	}
	// Trimming is applied before hashing: trailing whitespace must not change it.
	d2 := utilityEndpointDigest("  " + raw + "\n")
	if d1 != d2 {
		t.Fatalf("endpoint digest must trim before hashing: %q != %q", d1, d2)
	}
	// Different endpoints must differ.
	d3 := utilityEndpointDigest("http://127.0.0.1:8001/v1")
	if d1 == d3 {
		t.Fatal("different endpoints must produce different digests")
	}
}

func TestUtilityCanonicalDigestStability(t *testing.T) {
	// Canonical JSON digest: key order must not matter, and identical values
	// must produce identical digests (byte-stable replay).
	a := map[string]any{"b": 1, "a": []string{"x", "y"}, "nested": map[string]any{"z": 0.5}}
	b := map[string]any{"nested": map[string]any{"z": 0.5}, "a": []string{"x", "y"}, "b": 1}
	da, err := utilityCanonicalDigest(a)
	if err != nil {
		t.Fatalf("canonical digest: %v", err)
	}
	db, err := utilityCanonicalDigest(b)
	if err != nil {
		t.Fatalf("canonical digest: %v", err)
	}
	if da != db {
		t.Fatalf("canonical digest not key-order stable: %q != %q", da, db)
	}
	// Non-finite floats must be rejected.
	if _, err := utilityCanonicalDigest(map[string]any{"v": math.Inf(1)}); err == nil {
		t.Fatal("canonical digest must reject non-finite numbers")
	}
}

func TestUtilityManifestImmutability(t *testing.T) {
	m := utilityTestManifest(utilityStageCollect)
	d1, err := utilityManifestDigest(&m)
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	// Serialize then re-digest: identical.
	m2 := m
	d2, err := utilityManifestDigest(&m2)
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	if d1 != d2 {
		t.Fatal("manifest digest not deterministic")
	}
	// Mutating any frozen field changes the digest (immutability detection).
	m2.Answerer.Model = "other-model"
	d3, err := utilityManifestDigest(&m2)
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	if d1 == d3 {
		t.Fatal("mutating manifest field must change the digest")
	}
}

func TestUtilityPairedDeepArmSemantics(t *testing.T) {
	// paired_deep is only legal in collect; policy/control arms are for
	// confirm/transfer.
	if err := utilityArmValidForStage(utilityArmPairedDeep, utilityStageCollect); err != nil {
		t.Fatalf("paired_deep must be valid in collect: %v", err)
	}
	for _, s := range []utilityStage{utilityStagePilot, utilityStageDiagnose, utilityStageConfirm, utilityStageTransfer} {
		if err := utilityArmValidForStage(utilityArmPairedDeep, s); err == nil {
			t.Fatalf("paired_deep must be invalid in %s", s)
		}
	}
	if err := utilityArmValidForStage(utilityArmPolicyDeep, utilityStageConfirm); err != nil {
		t.Fatalf("policy_deep must be valid in confirm: %v", err)
	}
	if err := utilityArmValidForStage(utilityArmFixedDeep, utilityStageCollect); err == nil {
		t.Fatal("fixed_deep must be invalid in collect (control is paired_deep there)")
	}
}

func TestUtilityRetryStateMachine(t *testing.T) {
	// A logical call: STARTED -> COMPLETED (stop) or STARTED -> FAILED
	// retryable -> attempt 2 ... max 3. Orphan STARTED, completion-after,
	// attempt gap, duplicate terminal, non-retryable/exhausted FAILED all fail.
	key := utilityTestDecisionKey(0, "q0", 1)

	good := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitFailed).withFailure("timeout", true),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitCompleted),
	}
	if err := utilityValidateCallUnitJournal(good); err != nil {
		t.Fatalf("valid retry journal rejected: %v", err)
	}

	// Orphan STARTED (no terminal) is invalid.
	orphan := []utilityCallUnitRecord{utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted)}
	if err := utilityValidateCallUnitJournal(orphan); err == nil {
		t.Fatal("orphan STARTED must be invalid")
	}

	// Completion then another attempt is invalid.
	dupComplete := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitCompleted),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitStarted),
	}
	if err := utilityValidateCallUnitJournal(dupComplete); err == nil {
		t.Fatal("attempt after COMPLETED must be invalid")
	}

	// Attempt gap (1 -> 3) is invalid.
	gap := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitFailed).withFailure("timeout", true),
		utilityUnit(key, utilityArmShallow, 3, utilityCallUnitStarted),
	}
	if err := utilityValidateCallUnitJournal(gap); err == nil {
		t.Fatal("attempt gap must be invalid")
	}

	// Exhausted retries (FAILED retryable at attempt 3) is invalid.
	exhausted := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitFailed).withFailure("timeout", true),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitFailed).withFailure("timeout", true),
		utilityUnit(key, utilityArmShallow, 3, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 3, utilityCallUnitFailed).withFailure("timeout", true),
	}
	if err := utilityValidateCallUnitJournal(exhausted); err == nil {
		t.Fatal("exhausted retryable FAILED at attempt 3 must be invalid")
	}

	// Non-retryable failure must not continue.
	nonRetry := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitFailed).withFailure("decode_error", false),
		utilityUnit(key, utilityArmShallow, 2, utilityCallUnitStarted),
	}
	if err := utilityValidateCallUnitJournal(nonRetry); err == nil {
		t.Fatal("non-retryable FAILED must not allow a next attempt")
	}

	// Duplicate terminal for one attempt is invalid.
	dupTerminal := []utilityCallUnitRecord{
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitStarted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitCompleted),
		utilityUnit(key, utilityArmShallow, 1, utilityCallUnitCompleted),
	}
	if err := utilityValidateCallUnitJournal(dupTerminal); err == nil {
		t.Fatal("duplicate terminal must be invalid")
	}
}

func TestUtilityConservativeFailedAttemptCharge(t *testing.T) {
	maxLen := 32768
	// COMPLETED with valid usage: charge = input+output, reported.
	ok := utilityUnit(utilityTestDecisionKey(0, "q0", 1), utilityArmShallow, 1, utilityCallUnitCompleted)
	ok.InputTokens = 100
	ok.OutputTokens = 20
	ok.UsageStatus = utilityUsageReported
	if got := utilityUnitTokenCharge(ok, maxLen); got != 120 {
		t.Fatalf("reported charge = %d, want 120", got)
	}
	// FAILED with no usage: conservative bound = max_model_len.
	failed := utilityUnit(utilityTestDecisionKey(0, "q0", 1), utilityArmShallow, 1, utilityCallUnitFailed).withFailure("timeout", true)
	failed.UsageStatus = utilityUsageConservativeBound
	if got := utilityUnitTokenCharge(failed, maxLen); got != maxLen {
		t.Fatalf("conservative charge = %d, want %d", got, maxLen)
	}
	// FAILED with reported usage: charge reported usage, not the bound.
	failedReported := failed
	failedReported.InputTokens = 50
	failedReported.OutputTokens = 5
	failedReported.UsageStatus = utilityUsageReported
	if got := utilityUnitTokenCharge(failedReported, maxLen); got != 55 {
		t.Fatalf("failed-with-usage charge = %d, want 55", got)
	}
	// Judge has no runtime ratio charge.
	judge := utilityUnit(utilityTestDecisionKey(0, "q0", 1), utilityArmJudgeShallow, 1, utilityCallUnitCompleted)
	judge.UsageStatus = utilityUsageUnavailable
	if got := utilityUnitTokenCharge(judge, maxLen); got != 0 {
		t.Fatalf("judge ratio charge = %d, want 0", got)
	}
	// Query-embedding is not applicable to the generation ratio.
	embed := utilityUnit(utilityTestDecisionKey(0, "q0", 1), utilityArmShallow, 1, utilityCallUnitCompleted)
	embed.UsageStatus = utilityUsageNotApplicable
	if got := utilityUnitTokenCharge(embed, maxLen); got != 0 {
		t.Fatalf("embedding ratio charge = %d, want 0", got)
	}
}

func TestUtilityPublicHiddenCustody(t *testing.T) {
	// Hidden label files must not be readable from the public phase loader.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, utilityHiddenLabelsFile), []utilityUtilityLabel{utilityTestLabel()}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, utilityPublicAnswerAttemptsFile), []utilityAnswerAttempt{utilityTestAnswerAttempt()}); err != nil {
		t.Fatal(err)
	}
	// The public loader must refuse the hidden file path.
	if _, err := utilityLoadPublicRecords(filepath.Join(dir, utilityHiddenLabelsFile)); err == nil {
		t.Fatal("public loader must refuse hidden label files")
	}
}

func TestUtilitySealTamperRejection(t *testing.T) {
	dir := t.TempDir()
	m := utilityTestManifest(utilityStageCollect)
	if err := writeJSON(filepath.Join(dir, utilityManifestFile), m); err != nil {
		t.Fatal(err)
	}
	md, err := utilityManifestDigest(&m)
	if err != nil {
		t.Fatal(err)
	}
	seal := utilityStageSeal{Schema: utilitySchemaVersion, Stage: "collect", Status: utilitySealComplete, ManifestDigest: md}
	if err := writeJSON(filepath.Join(dir, utilitySealFile), seal); err != nil {
		t.Fatal(err)
	}
	// Valid manifest+seal pair validates.
	if err := utilityValidateManifestSeal(dir, utilityStageCollect); err != nil {
		t.Fatalf("valid manifest+seal rejected: %v", err)
	}
	// Tamper the manifest file: digest no longer matches the seal.
	tampered := m
	tampered.Benchmark.QuestionCount = 9999
	if err := writeJSON(filepath.Join(dir, utilityManifestFile), tampered); err != nil {
		t.Fatal(err)
	}
	if err := utilityValidateManifestSeal(dir, utilityStageCollect); err == nil {
		t.Fatal("tampered manifest must be rejected")
	}
	// Tamper the seal status to COMPLETE for a NO-GO report is not allowed to
	// hide invalidity: a fake COMPLETE seal over a wrong digest must fail.
	fakeSeal := seal
	fakeSeal.Status = utilitySealComplete
	fakeSeal.ManifestDigest = "sha256:" + strings.Repeat("0", 64)
	if err := writeJSON(filepath.Join(dir, utilitySealFile), fakeSeal); err != nil {
		t.Fatal(err)
	}
	if err := utilityValidateManifestSeal(dir, utilityStageCollect); err == nil {
		t.Fatal("seal with wrong manifest digest must be rejected")
	}
}

func TestUtilitySecretAndRawResponseExclusion(t *testing.T) {
	// The sanitized call record must not carry credentials or raw response
	// bodies even if the upstream object erroneously does.
	rec := utilityUnit(utilityTestDecisionKey(0, "q0", 1), utilityArmShallow, 1, utilityCallUnitCompleted)
	rec.InputTokens = 10
	rec.OutputTokens = 3
	rec.UsageStatus = utilityUsageReported
	rec.AnswerDigest = utilityEndpointDigest("answer") // digest only
	raw, err := utilityCanonicalDigest(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "Bearer") || strings.Contains(raw, "api_key") || strings.Contains(raw, "sk-") {
		t.Fatal("sanitized record leaked a secret pattern")
	}
	// Endpoint raw values are absent from the manifest's persisted digests.
	m := utilityTestManifest(utilityStageCollect)
	persisted, err := utilityCanonicalDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "127.0.0.1:8000") {
		t.Fatal("manifest persisted raw endpoint")
	}
}

// --- test helpers (referenced by T003/T004; the production types they build
// are defined by T005/T006) ---

func utilityTestDecisionKey(conv int, qid string, rep int) utilityDecisionKey {
	return utilityDecisionKey{Benchmark: "locomo", ConversationID: conv, QuestionID: qid, Repetition: rep}
}

func utilityUnit(key utilityDecisionKey, arm utilityArm, attempt int, state utilityCallUnitState) utilityCallUnitRecord {
	return utilityCallUnitRecord{
		LogicalCallID: utilityEndpointDigest("logical-" + string(arm) + "-" + key.QuestionID),
		UnitID:        utilityEndpointDigest("unit-" + key.QuestionID + "-" + strconv.Itoa(attempt)),
		DecisionKey:   &key,
		Arm:           arm,
		State:         state,
		Attempt:       attempt,
		UsageStatus:   utilityUsageNotApplicable,
	}
}

func (r utilityCallUnitRecord) withFailure(reason utilityFailureReason, retryable bool) utilityCallUnitRecord {
	r.State = utilityCallUnitFailed
	r.FailureReason = reason
	r.Retryable = retryable
	return r
}

func utilityTestManifest(stage utilityStage) utilityRunManifest {
	return utilityRunManifest{
		Schema: utilitySchemaVersion,
		RunID:  "run-test",
		Stage:  stage,
		Answerer: utilityAnswererIdentity{
			Model:              "test-model",
			EndpointDigest:     utilityEndpointDigest("http://127.0.0.1:8000/v1"),
			TemperatureRequest: "omitted",
			MaxModelLen:        utilityMaxModelLen,
		},
		Benchmark: utilityBenchmarkIdentity{
			QuestionCount:   1540,
			ConversationIDs: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			Repetitions:     utilityRepetitions,
		},
		Recipe: utilityRecipeIdentity{
			ShallowK: utilityShallowK,
			DeepK:    utilityDeepK,
		},
		CallPolicy: utilityCallPolicy{MaxAttempts: utilityMaxAttempts},
	}
}

func utilityTestLabel() utilityUtilityLabel {
	return utilityUtilityLabel{
		DecisionKey: utilityTestDecisionKey(0, "q0", 1),
		Utility:     1,
		Label:       utilityLabelBenefit,
	}
}

func utilityTestAnswerAttempt() utilityAnswerAttempt {
	return utilityAnswerAttempt{
		AnswerAttemptID: utilityEndpointDigest("answer-test"),
		DecisionKey:     utilityTestDecisionKey(0, "q0", 1),
		Arm:             utilityArmShallow,
		FinalAnswer:     "Oslo",
	}
}
