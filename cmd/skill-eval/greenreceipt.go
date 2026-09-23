package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fixed-suite GreenTestReceipt layer (data-model.md; runner-cli.md §1.2).
// `green-test create` only accepts the four versioned fixed suites — never
// caller-supplied shell text — and binds exact argv digests plus the current
// runner/judge/validator identities.

const (
	SuiteHoldoutPipeline = "holdout-pipeline"
	SuiteFormalTooling   = "formal-tooling"
	SuiteSeriesPrepare   = "series-prepare"
	SuitePreHoldout      = "pre-holdout"
)

var fixedSuites = map[string]bool{
	SuiteHoldoutPipeline: true, SuiteFormalTooling: true,
	SuiteSeriesPrepare: true, SuitePreHoldout: true,
}

// GreenCommand is one sanitized command record in a receipt.
type GreenCommand struct {
	Name         string `json:"name"`
	ArgvDigest   string `json:"argv_digest"`
	ExitCode     int    `json:"exit_code"`
	StdoutDigest string `json:"stdout_digest"`
	StderrDigest string `json:"stderr_digest"`
}

// GreenTestReceipt is the pre-irreversible-action attestation.
type GreenTestReceipt struct {
	SchemaVersion            int           `json:"schema_version"`
	Suite                    string        `json:"suite"`
	SuiteManifestDigest      string        `json:"suite_manifest_digest"`
	Commands                 []GreenCommand `json:"commands"`
	RunnerDigest             string        `json:"runner_digest"`
	JudgeRuleDigest          string        `json:"judge_rule_digest"`
	ValidatorDigest          string        `json:"validator_digest"`
	SnapshotDigest           *string       `json:"snapshot_digest,omitempty"`
	PackageValidationReceiptDigest *string `json:"package_validation_receipt_digest,omitempty"`
	StableIdentityDigest     *string       `json:"stable_identity_digest,omitempty"`
	SeriesManifestDigest     *string       `json:"series_manifest_digest,omitempty"`
	CandidateBindingDigest   *string       `json:"candidate_binding_digest,omitempty"`
	CoreLegCompletionDigest  *string       `json:"core_leg_completion_digest,omitempty"`
	Passed                   bool          `json:"passed"`
	CreatedAt                string        `json:"created_at"`
	ReceiptDigest            string        `json:"receipt_digest"`
}

// currentImplementDigests returns the runner/judge/validator digests a fresh
// receipt binds (and a verification rechecks).
func currentImplementDigests(validatorPath string) (runner, judge, validator string, err error) {
	runner, err = CurrentRunnerDigest()
	if err != nil {
		return "", "", "", err
	}
	judge, err = CurrentJudgeRuleDigest()
	if err != nil {
		return "", "", "", err
	}
	b, err := os.ReadFile(validatorPath)
	if err != nil {
		return "", "", "", fmt.Errorf("validator %s: %w", validatorPath, err)
	}
	validator = sha256Hex(b)
	return runner, judge, validator, nil
}

// suiteManifestDigest freezes the suite's fixed argv set + implement digests.
func suiteManifestDigest(suite string, validatorPath string) (string, error) {
	runner, judge, validator, err := currentImplementDigests(validatorPath)
	if err != nil {
		return "", err
	}
	spec := struct {
		Suite         string   `json:"suite"`
		FixedArgvSets []string `json:"fixed_argv_sets"`
		RunnerDigest  string   `json:"runner_digest"`
		JudgeDigest   string   `json:"judge_digest"`
		ValidatorDigest string `json:"validator_digest"`
	}{
		Suite: suite,
		FixedArgvSets: suiteArgvSets(suite),
		RunnerDigest: runner, JudgeDigest: judge, ValidatorDigest: validator,
	}
	return CanonicalSHA256(spec)
}

// suiteArgvSets are the frozen, read-only command sets per suite. No suite
// accepts caller-provided shell text.
func suiteArgvSets(suite string) []string {
	switch suite {
	case SuiteFormalTooling, SuiteSeriesPrepare, SuitePreHoldout:
		return []string{
			"go test -count=1 ./cmd/skill-eval",
			"node --test scripts/validate-agent-skill.test.mjs",
		}
	case SuiteHoldoutPipeline:
		return []string{
			"go test -count=1 ./cmd/skill-eval",
			"node --test scripts/validate-agent-skill.test.mjs",
		}
	default:
		return nil
	}
}

// CreateGreenTestReceipt executes the fixed suite for `suite` and materializes
// a passing receipt at outPath. Implement digests are captured before the run.
func CreateGreenTestReceipt(suite, validatorPath, outPath string, bindings GreenBindings) (*GreenTestReceipt, error) {
	if !fixedSuites[suite] {
		return nil, fmt.Errorf("suite %q is not a fixed suite (refusing arbitrary commands)", suite)
	}
	runner, judge, validator, err := currentImplementDigests(validatorPath)
	if err != nil {
		return nil, err
	}
	smd, err := suiteManifestDigest(suite, validatorPath)
	if err != nil {
		return nil, err
	}
	commands, err := fixedSuiteRunner(suite)
	if err != nil {
		return nil, err
	}
	passed := true
	for _, c := range commands {
		if c.ExitCode != 0 {
			passed = false
		}
	}
	r := &GreenTestReceipt{
		SchemaVersion: 1, Suite: suite, SuiteManifestDigest: smd,
		Commands: commands, RunnerDigest: runner, JudgeRuleDigest: judge, ValidatorDigest: validator,
		SnapshotDigest: bindings.SnapshotDigest,
		PackageValidationReceiptDigest: bindings.PackageValidationReceiptDigest,
		SeriesManifestDigest: bindings.SeriesManifestDigest,
		CandidateBindingDigest: bindings.CandidateBindingDigest,
		CoreLegCompletionDigest: bindings.CoreLegCompletionDigest,
		Passed: passed, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// series-prepare derives its stable identity digest (suite manifest +
	// fixed argv set + stable bindings; no output/time/receipt digests).
	if suite == SuiteSeriesPrepare || suite == SuitePreHoldout {
		if bindings.SnapshotDigest == nil || bindings.PackageValidationReceiptDigest == nil {
			return nil, fmt.Errorf("%s receipts must bind snapshot and package-validation digests", suite)
		}
		id, err := greenStableIdentity(suite, smd, runner, judge, validator, bindings)
		if err != nil {
			return nil, err
		}
		r.StableIdentityDigest = &id
	}
	if suite == SuitePreHoldout {
		if bindings.SeriesManifestDigest == nil || bindings.CandidateBindingDigest == nil || bindings.CoreLegCompletionDigest == nil {
			return nil, fmt.Errorf("pre-holdout receipts must bind series manifest, candidate binding and core-leg completion")
		}
	}
	if !passed {
		// A failed run never materializes a receipt file.
		return r, fmt.Errorf("fixed suite %s failed: not writing a receipt", suite)
	}
	d, err := receiptDigest(r)
	if err != nil {
		return nil, err
	}
	r.ReceiptDigest = d
	if outPath != "" {
		if err := WriteFrozenFile(outPath, mustCanonicalJSON(r)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// GreenBindings carries the per-suite bound identities.
type GreenBindings struct {
	SnapshotDigest                 *string
	PackageValidationReceiptDigest *string
	SeriesManifestDigest           *string
	CandidateBindingDigest         *string
	CoreLegCompletionDigest        *string
}

func greenStableIdentity(suite, smd, runner, judge, validator string, b GreenBindings) (string, error) {
	proj := struct {
		Suite                    string  `json:"suite"`
		SuiteManifestDigest      string  `json:"suite_manifest_digest"`
		RunnerDigest             string  `json:"runner_digest"`
		JudgeDigest              string  `json:"judge_digest"`
		ValidatorDigest          string  `json:"validator_digest"`
		SnapshotDigest           string  `json:"snapshot_digest"`
		PackageValidationReceipt string  `json:"package_validation_receipt"`
	}{
		Suite: suite, SuiteManifestDigest: smd,
		RunnerDigest: runner, JudgeDigest: judge, ValidatorDigest: validator,
		SnapshotDigest: deref(b.SnapshotDigest), PackageValidationReceipt: deref(b.PackageValidationReceiptDigest),
	}
	return CanonicalSHA256(proj)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func receiptDigest(r *GreenTestReceipt) (string, error) {
	saved := r.ReceiptDigest
	r.ReceiptDigest = ""
	d, err := CanonicalSHA256(r)
	r.ReceiptDigest = saved
	return d, err
}

func mustCanonicalJSON(v any) []byte {
	b, err := CanonicalJSON(v)
	if err != nil {
		panic(err)
	}
	return b
}

// fixedSuiteRunner executes a fixed suite; production binds runFixedSuite and
// tests inject a deterministic stub. It always returns sanitized digests only.
var fixedSuiteRunner = runFixedSuite

// runFixedSuite executes the suite's fixed argv entries and records sanitized
// digests only. Implemented as the real go test + validator unit-suite runs.
func runFixedSuite(suite string) ([]GreenCommand, error) {
	var cmds []GreenCommand
	for _, argv := range suiteArgvSets(suite) {
		var name string
		var args []string
		switch {
		case strings.HasPrefix(argv, "go test"):
			name = "go-test-skill-eval"
			args = []string{"go", "test", "-count=1", "./cmd/skill-eval"}
		case strings.HasPrefix(argv, "node --test"):
			name = "node-validator-unit"
			args = []string{"node", "--test", "scripts/validate-agent-skill.test.mjs"}
		default:
			return nil, fmt.Errorf("unknown fixed command %q", argv)
		}
		argvDigest, err := CanonicalSHA256(args)
		if err != nil {
			return nil, err
		}
		out := runAndCapture(args)
		cmds = append(cmds, GreenCommand{
			Name: name, ArgvDigest: argvDigest, ExitCode: out.exitCode,
			StdoutDigest: sha256Hex(out.stdout), StderrDigest: sha256Hex(out.stderr),
		})
	}
	return cmds, nil
}

// VerifyGreenTestReceipt re-verifies a loaded receipt against the required
// suite and the CURRENT implement digests: missing/failed/wrong-suite/post-hoc
// semantics (created_at in the future) and any digest drift fail closed.
func VerifyGreenTestReceipt(r *GreenTestReceipt, wantSuite, validatorPath string, wantBindings GreenBindings) error {
	if r == nil {
		return errors.New("green test receipt missing")
	}
	if !fixedSuites[r.Suite] {
		return fmt.Errorf("receipt suite %q is not a fixed suite", r.Suite)
	}
	if r.Suite != wantSuite {
		return fmt.Errorf("wrong-suite receipt: got %q want %q", r.Suite, wantSuite)
	}
	if !r.Passed {
		return fmt.Errorf("green test receipt %s is failed", r.Suite)
	}
	if len(r.Commands) == 0 {
		return errors.New("green test receipt has no command evidence")
	}
	for _, c := range r.Commands {
		if c.ExitCode != 0 {
			return fmt.Errorf("command %s exit=%d in a passed receipt", c.Name, c.ExitCode)
		}
	}
	runner, judge, validator, err := currentImplementDigests(validatorPath)
	if err != nil {
		return err
	}
	if r.RunnerDigest != runner {
		return fmt.Errorf("green receipt runner digest drifted: %s != current %s", r.RunnerDigest, runner)
	}
	if r.JudgeRuleDigest != judge {
		return fmt.Errorf("green receipt judge digest drifted")
	}
	if r.ValidatorDigest != validator {
		return fmt.Errorf("green receipt validator digest drifted")
	}
	smd, err := suiteManifestDigest(r.Suite, validatorPath)
	if err != nil {
		return err
	}
	if r.SuiteManifestDigest != smd {
		return errors.New("green receipt suite manifest drifted")
	}
	if wantBindings.SnapshotDigest != nil && deref(r.SnapshotDigest) != *wantBindings.SnapshotDigest {
		return errors.New("green receipt snapshot binding mismatch")
	}
	if wantBindings.PackageValidationReceiptDigest != nil && deref(r.PackageValidationReceiptDigest) != *wantBindings.PackageValidationReceiptDigest {
		return errors.New("green receipt package-validation binding mismatch")
	}
	if wantSuite == SuiteSeriesPrepare {
		if r.StableIdentityDigest == nil || *r.StableIdentityDigest == "" {
			return errors.New("series-prepare receipt must carry stable_identity_digest")
		}
	}
	if wantSuite == SuitePreHoldout {
		if r.SeriesManifestDigest == nil || deref(r.SeriesManifestDigest) != deref(wantBindings.SeriesManifestDigest) {
			return errors.New("pre-holdout receipt must bind the exact prepared series manifest")
		}
		if r.CandidateBindingDigest == nil || deref(r.CandidateBindingDigest) != deref(wantBindings.CandidateBindingDigest) {
			return errors.New("pre-holdout receipt must bind the exact stable candidate digest")
		}
		if r.CoreLegCompletionDigest == nil || deref(r.CoreLegCompletionDigest) != deref(wantBindings.CoreLegCompletionDigest) {
			return errors.New("pre-holdout receipt must bind the complete core-leg receipt set digest")
		}
	}
	// Post-hoc guard: the receipt cannot be stamped in the future.
	if ts, err := time.Parse(time.RFC3339, r.CreatedAt); err != nil || ts.After(time.Now().UTC().Add(time.Hour)) {
		return errors.New("green receipt timestamp is not a plausible pre-action instant")
	}
	saved := r.ReceiptDigest
	r.ReceiptDigest = ""
	d, err := CanonicalSHA256(r)
	r.ReceiptDigest = saved
	if err != nil {
		return err
	}
	if d != saved {
		return errors.New("green receipt digest mismatch (post-hoc mutation)")
	}
	return nil
}

// LoadGreenTestReceipt strictly parses a receipt file.
func LoadGreenTestReceipt(path string) (*GreenTestReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r GreenTestReceipt
	if err := StrictParseClosed(b, &r); err != nil {
		return nil, fmt.Errorf("green receipt %s: %w", path, err)
	}
	return &r, nil
}

// Ensure green receipts land in fresh per-suite files under formal dirs.
var _ = filepath.Join
