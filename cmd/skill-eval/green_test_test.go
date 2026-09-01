package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func okSuiteRunner(suite string) ([]GreenCommand, error) {
	return []GreenCommand{
		{Name: "go-test-skill-eval", ArgvDigest: strings.Repeat("a", 64), ExitCode: 0,
			StdoutDigest: strings.Repeat("b", 64), StderrDigest: strings.Repeat("c", 64)},
	}, nil
}

func failingSuiteRunner(suite string) ([]GreenCommand, error) {
	cmds, _ := okSuiteRunner(suite)
	cmds[0].ExitCode = 1
	return cmds, nil
}

func TestGreenTestReceiptLifecycle(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	validator := filepath.Join(t.TempDir(), "validate-agent-skill.mjs")
	writeFile(t, filepath.Dir(validator), filepath.Base(validator), []byte("// fictional validator"))

	// Fixed suites only: arbitrary shell text is refused outright.
	if _, err := CreateGreenTestReceipt("rm -rf /", validator, "", GreenBindings{}); err == nil {
		t.Fatal("arbitrary command suites must be rejected")
	}
	if len(suiteArgvSets("not-a-suite")) != 0 {
		t.Fatal("unknown suites have no argv set")
	}

	r, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, "", GreenBindings{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed || r.ReceiptDigest == "" {
		t.Fatal("passing run must produce a receipt with a digest")
	}
	// Verification against the current digests succeeds.
	if err := VerifyGreenTestReceipt(r, SuiteFormalTooling, validator, GreenBindings{}); err != nil {
		t.Fatalf("fresh receipt must verify: %v", err)
	}
	// Wrong suite.
	if err := VerifyGreenTestReceipt(r, SuitePreHoldout, validator, GreenBindings{}); err == nil {
		t.Fatal("wrong-suite verification must fail")
	}
	// Failed evidence.
	fixedSuiteRunner = failingSuiteRunner
	if _, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, "", GreenBindings{}); err == nil {
		t.Fatal("a failed fixed run must not produce a receipt")
	}
	fixedSuiteRunner = okSuiteRunner
	// Failed receipt never verifies.
	bad := *r
	bad.Passed = false
	if err := VerifyGreenTestReceipt(&bad, SuiteFormalTooling, validator, GreenBindings{}); err == nil {
		t.Fatal("failed receipt must be rejected")
	}
	// Digest drift (tamper with a bound digest).
	drifted := *r
	drifted.RunnerDigest = strings.Repeat("f", 64)
	if err := VerifyGreenTestReceipt(&drifted, SuiteFormalTooling, validator, GreenBindings{}); err == nil {
		t.Fatal("digest-drifted receipt must be rejected")
	}
	// Post-hoc mutation breaks the self digest.
	mutated := *r
	mutated.Commands[0].ExitCode = 0
	mutated.CreatedAt = "2031-01-01T00:00:00Z"
	if err := VerifyGreenTestReceipt(&mutated, SuiteFormalTooling, validator, GreenBindings{}); err == nil {
		t.Fatal("post-hoc timestamp mutation must be rejected")
	}
}

func TestGreenSuiteScopeRules(t *testing.T) {
	validator := filepath.Join(t.TempDir(), "validate-agent-skill.mjs")
	writeFile(t, filepath.Dir(validator), filepath.Base(validator), []byte("// v"))
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner

	// series-prepare must bind snapshot + package-validation and derive a
	// stable identity digest.
	snap := "snap-digest"
	pv := "pv-receipt-digest"
	r, err := CreateGreenTestReceipt(SuiteSeriesPrepare, validator, "", GreenBindings{
		SnapshotDigest: &snap, PackageValidationReceiptDigest: &pv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.StableIdentityDigest == nil || *r.StableIdentityDigest == "" {
		t.Fatal("series-prepare receipts must carry stable_identity_digest")
	}
	if _, err := CreateGreenTestReceipt(SuiteSeriesPrepare, validator, "", GreenBindings{}); err == nil {
		t.Fatal("series-prepare without snapshot/package bindings must be refused")
	}
	// pre-holdout additionally requires series manifest + candidate binding +
	// core-leg completion.
	if _, err := CreateGreenTestReceipt(SuitePreHoldout, validator, "", GreenBindings{
		SnapshotDigest: &snap, PackageValidationReceiptDigest: &pv,
	}); err == nil {
		t.Fatal("pre-holdout without series/core-leg bindings must be refused")
	}
	sm, cb, cl := "series-manifest", "candidate-binding", "core-leg-set"
	rp, err := CreateGreenTestReceipt(SuitePreHoldout, validator, "", GreenBindings{
		SnapshotDigest: &snap, PackageValidationReceiptDigest: &pv,
		SeriesManifestDigest: &sm, CandidateBindingDigest: &cb, CoreLegCompletionDigest: &cl,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verification binds the exact series: another series' binding fails.
	if err := VerifyGreenTestReceipt(rp, SuitePreHoldout, validator, GreenBindings{
		SeriesManifestDigest: strPtr("other-series"), CandidateBindingDigest: &cb, CoreLegCompletionDigest: &cl,
	}); err == nil {
		t.Fatal("pre-holdout receipt must be one-series-only")
	}
	if err := VerifyGreenTestReceipt(rp, SuitePreHoldout, validator, GreenBindings{
		SeriesManifestDigest: &sm, CandidateBindingDigest: &cb, CoreLegCompletionDigest: &cl,
	}); err != nil {
		t.Fatalf("matching pre-holdout binding must verify: %v", err)
	}
	// The stable identity is reproducible and excludes output/time/receipts.
	r2, err := CreateGreenTestReceipt(SuiteSeriesPrepare, validator, "", GreenBindings{
		SnapshotDigest: &snap, PackageValidationReceiptDigest: &pv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *r2.StableIdentityDigest != *r_Stable(rp) && rp.StableIdentityDigest != nil {
		// Different suites may differ; same suite must be identical.
	}
	r3, err := CreateGreenTestReceipt(SuiteSeriesPrepare, validator, "", GreenBindings{
		SnapshotDigest: &snap, PackageValidationReceiptDigest: &pv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *r2.StableIdentityDigest != *r3.StableIdentityDigest {
		t.Fatal("stable_identity_digest must be reproducible for identical bindings")
	}
}

func r_Stable(r *GreenTestReceipt) *string { return r.StableIdentityDigest }
