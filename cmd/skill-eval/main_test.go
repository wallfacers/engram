package main

import (
	"strings"
	"testing"
)

func TestMainRoutingV2Commands(t *testing.T) {
	// The v2 command surface routes before the legacy harness.
	for _, cmd := range []string{"family-index", "green-test", "package"} {
		if handled, err := routeV2([]string{cmd}); !handled {
			t.Errorf("command %q must be handled by the v2 router", cmd)
		} else if err == nil {
			t.Errorf("command %q without a subcommand must error", cmd)
		}
	}
	if handled, _ := routeV2([]string{"run"}); handled {
		t.Error("run routes through the mode-aware legacy bridge, not the v2 router")
	}
}

func TestGreenTestCreateArgvDiscipline(t *testing.T) {
	// Fixed suite allowlist: unknown and arbitrary suites are rejected before
	// any command runs.
	if err := cmdGreenTestCreate([]string{"--suite", "deploy-prod", "--out", "x.json"}); err == nil {
		t.Fatal("arbitrary suite names must be rejected")
	}
	if err := cmdGreenTestCreate([]string{"--suite", "rm -rf /", "--out", "x.json"}); err == nil {
		t.Fatal("shell-text suites must be rejected")
	}
	if err := cmdGreenTestCreate([]string{"--suite", SuiteFormalTooling}); err == nil {
		t.Fatal("missing --out must be rejected")
	}
	if err := cmdGreenTestCreate(nil); err == nil {
		t.Fatal("missing required arguments must be rejected")
	}
	// The fixed argv sets never embed caller text.
	for _, suite := range fixedSuitesOrder() {
		for _, argv := range suiteArgvSets(suite) {
			if strings.Contains(argv, "x.json") || strings.Contains(argv, "deploy") {
				t.Fatalf("suite %s argv set carries caller text: %s", suite, argv)
			}
		}
	}
}

func fixedSuitesOrder() []string {
	return []string{SuiteHoldoutPipeline, SuiteFormalTooling, SuiteSeriesPrepare, SuitePreHoldout}
}

func TestPackageValidateArgvRequirements(t *testing.T) {
	if err := cmdPackageValidate(nil); err == nil {
		t.Fatal("package validate without arguments must be rejected")
	}
	if err := cmdPackageValidate([]string{"--skill-dir", "s"}); err == nil {
		t.Fatal("package validate with partial arguments must be rejected")
	}
	err := cmdPackageValidate([]string{
		"--skill-dir", "s", "--repository-root", "r", "--snapshot-root", "n",
		"--green-test-receipt", "g", "--out", "o.json",
	})
	// With all flags present the command proceeds to the green gate and must
	// fail on the missing receipt file — not on argument validation.
	if err == nil || !strings.Contains(err.Error(), "green") {
		t.Fatalf("full-argv package validate should fail at the green gate: %v", err)
	}
}

func TestUsageDocumentsV2Surface(t *testing.T) {
	// The usage text must document the pre-T018 commands so the frozen CLI is
	// discoverable before any formal run.
	for _, want := range []string{"package validate", "green-test create", "family-index build", "--mode diagnostic"} {
		if !strings.Contains(usageV2, want) {
			t.Errorf("usage missing %q", want)
		}
	}
}

func TestValidateAndRunModeRouting(t *testing.T) {
	// validate with v2 flags routes to the split/phase-aware command.
	if err := cmdValidateV2([]string{"--split", "holdout", "--phase", "pre-index"}); err == nil {
		t.Fatal("holdout pre-index must be invalid (pre-index is dev-only)")
	}
	// run --mode is required for the v2 path.
	if err := cmdRunV2(nil); err == nil || !strings.Contains(err.Error(), "--mode") {
		t.Fatalf("run without --mode must be rejected: %v", err)
	}
	// primary mode is gated on a prepared formal series (T045+).
	if err := cmdRunV2([]string{"--mode", "primary", "--split", "dev-regression"}); err == nil {
		t.Fatal("primary runs without a prepared series must be rejected")
	}
}
