package main

// T003 [P] CLI contract tests: six-stage closed enum, auxiliary-flag rejection
// when disabled, mutually exclusive modes, required mem0-aligned/clean-final
// regime, early offline dispatch, and ordinary-run byte parity. Written first
// (failing) against the intended counterfactual_utility_cli.go API.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUtilityStageClosedEnum(t *testing.T) {
	cases := []struct {
		in      string
		valid   bool
		wantStr string
	}{
		{"label", true, "label"},
		{"pilot", true, "pilot"},
		{"collect", true, "collect"},
		{"diagnose", true, "diagnose"},
		{"confirm", true, "confirm"},
		{"transfer", true, "transfer"},
		{"", false, ""},
		{"pilot ", false, ""},
		{"COLLECT", false, ""},
		{"summarize", false, ""},
		{"label|pilot", false, ""},
	}
	for _, c := range cases {
		got, err := parseUtilityStage(c.in)
		if c.valid {
			if err != nil {
				t.Fatalf("parseUtilityStage(%q) unexpected error: %v", c.in, err)
			}
			if string(got) != c.wantStr {
				t.Fatalf("parseUtilityStage(%q) = %q, want %q", c.in, got, c.wantStr)
			}
			if !got.valid() {
				t.Fatalf("stage %q valid() = false, want true", got)
			}
		} else {
			if err == nil {
				t.Fatalf("parseUtilityStage(%q) expected error, got %q", c.in, got)
			}
		}
	}
}

// baseOpt returns an options value with the ordinary-run defaults set so that
// the utility validation only sees utility-relevant state.
func baseUtilityOpt() options {
	return options{
		retrieval:     "hybrid",
		datasetFormat: "locomo",
		repeats:       3,
		maxTokens:     8000,
		concurrency:   32,
		topK:          30,
		explicitFlags: map[string]bool{},
	}
}

func setUtilityFlags(opt *options, stage string, kShallow, kDeep int) {
	opt.utilityStageFlag = stage
	opt.utilityShallowK = kShallow
	opt.utilityDeepK = kDeep
	if opt.explicitFlags == nil {
		opt.explicitFlags = map[string]bool{}
	}
	opt.explicitFlags["utility-stage"] = stage != ""
	opt.explicitFlags["utility-shallow-k"] = true
	opt.explicitFlags["utility-deep-k"] = true
}

func TestUtilityAuxiliaryFlagsRejectedWhenDisabled(t *testing.T) {
	// Every --utility-* auxiliary flag must be a usage error while
	// --utility-stage is empty (set-but-ignored is forbidden).
	aux := []struct {
		name  string
		apply func(o *options)
	}{
		{"utility-source", func(o *options) { o.utilitySource = "x" }},
		{"utility-label-source", func(o *options) { o.utilityLabelSource = "x" }},
		{"utility-pilot-source", func(o *options) { o.utilityPilotSource = "x" }},
		{"utility-shallow-source", func(o *options) { o.utilityShallowSource = "x" }},
		{"utility-deep-source", func(o *options) { o.utilityDeepSource = "x" }},
		{"utility-shallow-k", func(o *options) { o.utilityShallowK = 7 }},
		{"utility-deep-k", func(o *options) { o.utilityDeepK = 9 }},
	}
	for _, a := range aux {
		opt := baseUtilityOpt()
		a.apply(&opt)
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatalf("auxiliary flag %s with --utility-stage empty: expected usage error, got nil", a.name)
		} else if !strings.Contains(err.Error(), "--utility-stage") {
			t.Fatalf("auxiliary flag %s error should mention --utility-stage, got: %v", a.name, err)
		}
	}
	// Fully empty utility state is fine (default off).
	opt := baseUtilityOpt()
	if err := validateUtilityCLIOptions(&opt); err != nil {
		t.Fatalf("empty utility state should validate: %v", err)
	}
}

func TestUtilityMutuallyExclusiveModes(t *testing.T) {
	conflicting := []struct {
		name  string
		apply func(o *options)
	}{
		{"nav", func(o *options) { o.nav = true }},
		{"iris", func(o *options) { o.iris = true }},
		{"unified-answer-contract", func(o *options) { o.unifiedAnswerContract = true }},
		{"abstain", func(o *options) { o.abstainPrompt = true }},
		{"pcic", func(o *options) { o.pcic = true }},
		{"rerank", func(o *options) { o.rerank = true }},
		{"filter-pool", func(o *options) { o.filterPool = 20 }},
		{"multi-query", func(o *options) { o.multiQuery = true }},
	}
	for _, c := range conflicting {
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, "collect", 30, 150)
		opt.utilityLabelSource = "label-dir"
		opt.utilityPilotSource = "pilot-dir"
		opt.dataPath = "locomo.json"
		opt.storeDir = "store"
		c.apply(&opt)
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatalf("conflicting mode %s with utility collect: expected error, got nil", c.name)
		}
	}
	// A plain collect without the conflicts validates.
	opt := baseUtilityOpt()
	setUtilityFlags(&opt, "collect", 30, 150)
	opt.utilityLabelSource = "label-dir"
	opt.utilityPilotSource = "pilot-dir"
	opt.dataPath = "locomo.json"
	opt.storeDir = "store"
	opt.runDir = "out"
	opt.chunks = true
	opt.judgeMem0Aligned = true
	if err := validateUtilityCLIOptions(&opt); err != nil {
		t.Fatalf("plain utility collect should validate: %v", err)
	}
}

func TestUtilityStageRequiredInputsAndForbiddenInputs(t *testing.T) {
	// label: shallow+deep sources required, data/store/provider forbidden.
	{
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, "label", 30, 150)
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("label without shallow/deep sources should fail")
		}
		opt.utilityShallowSource = "shallow"
		opt.utilityDeepSource = "deep"
		opt.runDir = "out"
		if err := validateUtilityCLIOptions(&opt); err != nil {
			t.Fatalf("label with sources should validate: %v", err)
		}
		opt.dataPath = "locomo.json"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("label must reject --data")
		}
	}
	// diagnose: source+run-dir required, data/store forbidden.
	{
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, "diagnose", 30, 150)
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("diagnose without source should fail")
		}
		opt.utilitySource = "collect-dir"
		opt.runDir = "out"
		if err := validateUtilityCLIOptions(&opt); err != nil {
			t.Fatalf("diagnose with source should validate: %v", err)
		}
		opt.dataPath = "locomo.json"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("diagnose must reject --data")
		}
	}
	// collect: label+pilot sources + data + store required; judge regime required.
	{
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, "collect", 30, 150)
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect without label source should fail")
		}
		opt.utilityLabelSource = "label-dir"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect without pilot source should fail")
		}
		opt.utilityPilotSource = "pilot-dir"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect without data should fail")
		}
		opt.dataPath = "locomo.json"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect without store-dir should fail")
		}
		opt.storeDir = "store"
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect without judge-mem0-aligned should fail")
		}
		opt.judgeMem0Aligned = true
		opt.runDir = "out"
		opt.chunks = true
		if err := validateUtilityCLIOptions(&opt); err != nil {
			t.Fatalf("collect with all inputs should validate: %v", err)
		}
	}
	// k values are frozen for formal stages.
	{
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, "collect", 20, 150)
		opt.utilityLabelSource = "label-dir"
		opt.utilityPilotSource = "pilot-dir"
		opt.dataPath = "locomo.json"
		opt.storeDir = "store"
		opt.judgeMem0Aligned = true
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatal("collect with shallow-k=20 should fail (v1 fixed 30)")
		}
	}
}

func TestUtilityFormalRunRejectsSubsetSelectors(t *testing.T) {
	for _, stage := range []string{"collect", "pilot", "confirm"} {
		opt := baseUtilityOpt()
		setUtilityFlags(&opt, stage, 30, 150)
		opt.utilityLabelSource = "label-dir"
		opt.utilityPilotSource = "pilot-dir"
		opt.dataPath = "locomo.json"
		opt.storeDir = "store"
		opt.judgeMem0Aligned = true
		opt.maxConvs = 2
		if err := validateUtilityCLIOptions(&opt); err == nil {
			t.Fatalf("%s with --conversations must be rejected", stage)
		}
	}
}

func TestUtilityOfflineEarlyDispatch(t *testing.T) {
	if !utilityOfflineStage(utilityStageLabel) {
		t.Fatal("label must be offline")
	}
	if !utilityOfflineStage(utilityStageDiagnose) {
		t.Fatal("diagnose must be offline")
	}
	for _, s := range []utilityStage{utilityStagePilot, utilityStageCollect, utilityStageConfirm, utilityStageTransfer} {
		if utilityOfflineStage(s) {
			t.Fatalf("%s must NOT be offline", s)
		}
	}
	// Early dispatch routes offline stages before the "--data is required" gate:
	// the returned error must come from the stage (missing sources here), never
	// from the ordinary data requirement.
	opt := baseUtilityOpt()
	setUtilityFlags(&opt, "label", 30, 150)
	opt.utilityShallowSource = filepath.Join(t.TempDir(), "missing-shallow")
	opt.utilityDeepSource = filepath.Join(t.TempDir(), "missing-deep")
	opt.runDir = filepath.Join(t.TempDir(), "label-out")
	if err := runUtilityCLI(&opt); err == nil {
		t.Fatal("label with missing historical sources should fail")
	} else if strings.Contains(err.Error(), "--data") {
		t.Fatalf("offline label must dispatch before the --data gate, got: %v", err)
	}
}

func TestUtilityOrdinaryRunByteParity(t *testing.T) {
	// With --utility-stage empty, no utility artifact may be produced and the
	// ordinary options must remain untouched.
	dir := t.TempDir()
	opt := baseUtilityOpt()
	opt.runDir = dir
	if err := validateUtilityCLIOptions(&opt); err != nil {
		t.Fatalf("ordinary options validate: %v", err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ordinary run must produce no utility artifacts, got %v", entries)
	}
}
