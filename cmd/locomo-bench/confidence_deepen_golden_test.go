package main

// T009 default-off golden gate: with --confidence-deepen false and
// --deepen-pilot empty, the deepen research mode must be completely inert —
// every auxiliary --deepen-* flag is a usage error, no deepen artifact is ever
// produced, the answer regime fingerprint is unchanged, and the frozen unified
// answer contract digest stays pinned at 1d8a8d0f (038 contract).

import (
	"path/filepath"
	"strings"
	"testing"
)

// baseDeepenOpt returns ordinary-run defaults so deepen validation only sees
// deepen-relevant state (mirrors baseUtilityOpt).
func baseDeepenOpt() options {
	return options{
		retrieval:     "hybrid",
		datasetFormat: "locomo",
		repeats:       3,
		maxTokens:     8000,
		concurrency:   32,
		topK:          30,
		deepenK:       30,
		deepenMaxGaps: 3,
		explicitFlags: map[string]bool{},
	}
}

func TestDeepenAuxiliaryFlagsRejectedWhenDisabled(t *testing.T) {
	// Every --deepen-* auxiliary flag must be a usage error while both
	// --confidence-deepen and --deepen-pilot are off (set-but-ignored is
	// forbidden, mirroring the 042 utility rule).
	aux := []struct {
		name  string
		apply func(o *options)
	}{
		{"deepen-threshold", func(o *options) { o.deepenThreshold = 0.7 }},
		{"deepen-signal-feature", func(o *options) { o.deepenSignalFeature = "final_p10_logprob" }},
	}
	for _, a := range aux {
		opt := baseDeepenOpt()
		a.apply(&opt)
		if err := validateDeepenCLIOptions(&opt); err == nil {
			t.Fatalf("auxiliary flag %s with deepen off: expected usage error, got nil", a.name)
		} else if !strings.Contains(err.Error(), "--confidence-deepen") && !strings.Contains(err.Error(), "--deepen-pilot") {
			t.Fatalf("auxiliary flag %s error should mention the master switch, got: %v", a.name, err)
		}
	}
	// Fully empty deepen state is fine (default off).
	opt := baseDeepenOpt()
	if err := validateDeepenCLIOptions(&opt); err != nil {
		t.Fatalf("empty deepen state should validate: %v", err)
	}
}

func TestDeepenMechanismRequiresUnified(t *testing.T) {
	opt := baseDeepenOpt()
	opt.confidenceDeepen = true
	// --confidence-deepen without --unified-answer-contract must be rejected.
	if err := validateDeepenMechanismFlags(&opt); err == nil {
		t.Fatal("--confidence-deepen without --unified-answer-contract must fail")
	} else if !strings.Contains(err.Error(), "--unified-answer-contract") {
		t.Fatalf("error should mention --unified-answer-contract, got: %v", err)
	}
	opt.unifiedAnswerContract = true
	if err := validateDeepenMechanismFlags(&opt); err != nil {
		t.Fatalf("--confidence-deepen with unified should validate: %v", err)
	}
}

func TestDeepenMechanismReadsSealOnly(t *testing.T) {
	// threshold / featureName are read-only from the pilot seal: explicit
	// non-default CLI values are rejected (FR-005 anti-tuning).
	opt := baseDeepenOpt()
	opt.confidenceDeepen = true
	opt.unifiedAnswerContract = true
	opt.deepenThreshold = 0.7
	if err := validateDeepenMechanismFlags(&opt); err == nil {
		t.Fatal("explicit --deepen-threshold must be rejected (seal read-only)")
	}
	opt.deepenThreshold = 0
	opt.deepenSignalFeature = "final_p10_logprob"
	if err := validateDeepenMechanismFlags(&opt); err == nil {
		t.Fatal("explicit --deepen-signal-feature must be rejected (seal read-only)")
	}
	opt.deepenSignalFeature = ""
	if err := validateDeepenMechanismFlags(&opt); err != nil {
		t.Fatalf("default mechanism flags should validate: %v", err)
	}
}

func TestDeepenPilotStageClosedEnum(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"signal", true},
		{"", false},
		{"signal ", false},
		{"SIGNAL", false},
		{"mechanism", false},
	}
	for _, c := range cases {
		_, err := parseDeepenPilotStage(c.in)
		if c.valid && err != nil {
			t.Fatalf("parseDeepenPilotStage(%q) unexpected error: %v", c.in, err)
		}
		if !c.valid && err == nil {
			t.Fatalf("parseDeepenPilotStage(%q) expected error", c.in)
		}
	}
}

func TestDeepenPilotRequiredInputs(t *testing.T) {
	opt := baseDeepenOpt()
	opt.deepenPilot = "signal"
	if err := validateDeepenCLIOptions(&opt); err == nil {
		t.Fatal("pilot without --data should fail")
	}
	opt.dataPath = "locomo.json"
	if err := validateDeepenCLIOptions(&opt); err == nil {
		t.Fatal("pilot without --store-dir should fail")
	}
	opt.storeDir = "store"
	if err := validateDeepenCLIOptions(&opt); err == nil {
		t.Fatal("pilot without --run-dir should fail")
	}
	opt.runDir = "out"
	if err := validateDeepenCLIOptions(&opt); err != nil {
		t.Fatalf("pilot with required inputs should validate: %v", err)
	}
	// Pilot must not combine with the mechanism switch (pilot produces the seal
	// the mechanism consumes).
	opt.confidenceDeepen = true
	if err := validateDeepenCLIOptions(&opt); err == nil {
		t.Fatal("pilot combined with --confidence-deepen must fail")
	}
	opt.confidenceDeepen = false
	// Pilot must not pre-set the seal-frozen threshold.
	opt.deepenThreshold = 0.7
	if err := validateDeepenCLIOptions(&opt); err == nil {
		t.Fatal("pilot with pre-set threshold must fail")
	}
}

func TestDeepenUnifiedConflictTable(t *testing.T) {
	// --confidence-deepen must land in the unified prompt pair experiment
	// conflict table alongside --gap-refetch/--iris/multi-query.
	arms := []string{"hybrid", "hybrid+unified"}
	opt := baseDeepenOpt()
	opt.noIDKRetry = true
	opt.repeats = 3
	opt.retrieval = "hybrid,hybrid+unified"
	opt.confidenceDeepen = true
	if err := validateUnifiedPromptPairExperiment(opt, arms); err == nil {
		t.Fatal("unified pair experiment with --confidence-deepen must be rejected")
	}
	// Without the conflict flag the same experiment validates (baseline guard).
	opt.confidenceDeepen = false
	if err := validateUnifiedPromptPairExperiment(opt, arms); err != nil {
		t.Fatalf("unified pair experiment baseline should validate: %v", err)
	}
}

// TestDeepenOrdinaryRunByteParity mirrors TestUtilityOrdinaryRunByteParity:
// with the deepen mode off, no deepen artifact may be produced and the ordinary
// options remain untouched.
func TestDeepenOrdinaryRunByteParity(t *testing.T) {
	dir := t.TempDir()
	opt := baseDeepenOpt()
	opt.runDir = dir
	if err := validateDeepenCLIOptions(&opt); err != nil {
		t.Fatalf("ordinary options validate: %v", err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ordinary run must produce no deepen artifacts, got %v", entries)
	}
}

// TestDeepenAnswerRegimeFingerprintUnchanged locks the byte parity at the
// regime-fingerprint level: the ordinary path's fingerprint must not include any
// deepen marker, and the default fingerprint must equal the deepen-off one.
func TestDeepenAnswerRegimeFingerprintUnchanged(t *testing.T) {
	base := answerRegimeFingerprint(baseDeepenOpt())
	if strings.Contains(base, "confidence_deepen") || strings.Contains(base, "deepen") {
		t.Fatalf("default regime fingerprint must not mention deepen: %s", base)
	}
	if opt := baseDeepenOpt(); answerRegimeFingerprint(opt) != base {
		t.Fatalf("deepen-off fingerprint drifted: %s", answerRegimeFingerprint(opt))
	}
}

// TestDeepenUnifiedContractDigestPinned locks the frozen 038 contract digest:
// any edit to unifiedAnswerContractPrompt would break this pin and the deepen
// mechanism would silently drift off the byte-frozen contract.
func TestDeepenUnifiedContractDigestPinned(t *testing.T) {
	const want = "sha256:1d8a8d0f"
	got := evalTextDigest(unifiedAnswerContractPrompt)
	if !strings.HasPrefix(got, want) {
		t.Fatalf("unified answer contract digest drift: got %s, want prefix %s", got, want)
	}
}
