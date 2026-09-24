package main

import (
	"strings"
	"testing"

	"github.com/wallfacers/engram/filter"
)

// T19 slice 13: --jev-arms-reps parameterization. The flag declares the answer
// repetition protocol for --jev-arms runs: 3 (default) is the canonical
// majority-of-3 protocol and stays byte-identical; 1 is the gated single-answer
// pilot (majority-of-1). Any other count has no majority semantics and must be
// refused at every seam.

// clearJevEnv neutralizes the policy environment so a test sees exactly the
// frozen defaults regardless of the developer shell.
func clearJevEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ENGRAM_JEV_THETA", "ENGRAM_JEV_RELAX_THETA", "ENGRAM_JEV_RELAX_MAX",
		"ENGRAM_JEV_KSHOW_MAX", "ENGRAM_JEV_RELAX", "ENGRAM_JEV_MODEL",
		"ENGRAM_JEV_BASE_URL", "ENGRAM_JEV_API_KEY",
	} {
		t.Setenv(name, "")
	}
}

// jevRepsEstimateOptions builds the smallest pre-flight input that reaches the
// repetitions checks: --estimate is exempt from the run-only requirements but
// never from the protocol pinning, so the repetitions refuse fires first.
func jevRepsEstimateOptions(reps, repeats int) options {
	return options{
		datasetFormat: "locomo",
		repeats:       repeats,
		jevArmsReps:   reps,
		estimate:      true,
	}
}

// (t1) the pilot case that was previously refused unconditionally:
// --jev-arms-reps 1 --repeats 1 must pass the pre-flight.
func TestJevArmsRepsOnePilotPassesPreFlight(t *testing.T) {
	clearJevEnv(t)
	if err := validateJevArmsOptions(jevRepsEstimateOptions(1, 1), []string{"hybrid"}); err != nil {
		t.Fatalf("the 1-repetition pilot was refused: %v", err)
	}
}

// (t2) the canonical default is unchanged: --jev-arms-reps 3 (the flag default)
// with --repeats 3 passes, and a --repeats drift is still refused.
func TestJevArmsRepsCanonicalDefaultUnchanged(t *testing.T) {
	clearJevEnv(t)
	if err := validateJevArmsOptions(jevRepsEstimateOptions(jevArmAnswerRepetitions, 3), []string{"hybrid"}); err != nil {
		t.Fatalf("the canonical 3-repetition invocation was refused: %v", err)
	}
	for _, repeats := range []int{1, 2, 5} {
		err := validateJevArmsOptions(jevRepsEstimateOptions(jevArmAnswerRepetitions, repeats), []string{"hybrid"})
		if err == nil {
			t.Fatalf("--repeats %d against --jev-arms-reps 3 was accepted", repeats)
		}
		if !strings.Contains(err.Error(), "--repeats") {
			t.Errorf("the %d-repeats refusal does not name --repeats: %v", repeats, err)
		}
	}
}

// (t3) only {1, 3} are allowed: any other declared count is refused by name.
func TestJevArmsRepsRefusesCountsWithoutMajoritySemantics(t *testing.T) {
	clearJevEnv(t)
	for _, reps := range []int{0, 2, 4, 5, -1} {
		err := validateJevArmsOptions(jevRepsEstimateOptions(reps, reps), []string{"hybrid"})
		if err == nil {
			t.Fatalf("--jev-arms-reps %d was accepted", reps)
		}
		for _, want := range []string{"--jev-arms-reps", "1 (pilot", "3 (canonical", "has no majority semantics"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("--jev-arms-reps %d refusal %q is missing %q", reps, err, want)
			}
		}
	}
}

// (t4) the declared reps and --repeats must agree in both directions, and the
// refusal names both flags.
func TestJevArmsRepsMustMatchRepeats(t *testing.T) {
	clearJevEnv(t)
	cases := []struct{ reps, repeats int }{{1, 3}, {3, 1}}
	for _, c := range cases {
		err := validateJevArmsOptions(jevRepsEstimateOptions(c.reps, c.repeats), []string{"hybrid"})
		if err == nil {
			t.Fatalf("--jev-arms-reps %d with --repeats %d was accepted", c.reps, c.repeats)
		}
		for _, want := range []string{"--jev-arms-reps 1", "--repeats 3"} {
			if !strings.Contains(err.Error(), want) && c.reps == 1 && c.repeats == 3 {
				t.Errorf("pilot-mismatch refusal %q is missing %q", err, want)
			}
		}
	}
}

// (t5a) the registration derived from a pilot run records answer_repetitions 1
// and validates; the canonical registration still records 3.
func TestJevRegistrationRecordsDeclaredReps(t *testing.T) {
	clearJevEnv(t)
	opt := options{
		jevModel:              "jev-pinned-2026-09-21",
		jevArmsReps:           1,
		repeats:               1,
		jevPilotGateConfirmed: true,
		jevWarmupDisposed:     true,
		jevSameWindowReps:     true,
	}
	registration, err := jevRegistrationForRun(opt, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("a pilot registration was refused: %v", err)
	}
	if registration.AnswerRepetitions != 1 {
		t.Errorf("pilot registration answer_repetitions = %d, want 1", registration.AnswerRepetitions)
	}
	canonical := opt
	canonical.jevArmsReps = jevArmAnswerRepetitions
	canonical.repeats = jevArmAnswerRepetitions
	if cr, err := jevRegistrationForRun(canonical, filter.DefaultPolicy()); err != nil || cr.AnswerRepetitions != 3 {
		t.Errorf("canonical registration = %+v (%v), want answer_repetitions 3", cr, err)
	}
}

// (t5b) the freeze attaches a pilot registration and the manifest binding
// consumes answer_repetitions 1 consistently; a manifest mixing 1 and 3 fails.
func TestJevFreezeManifestCarriesPilotReps(t *testing.T) {
	clearJevEnv(t)
	opt := options{
		jevArms:               true,
		jevModel:              "jev-pinned-2026-09-21",
		jevArmsReps:           1,
		repeats:               1,
		jevPilotGateConfirmed: true,
		jevWarmupDisposed:     true,
		jevSameWindowReps:     true,
	}
	pilot := evalProtocol{
		Budget:      evalBudgetProtocol{AnswerInputTokenCap: jevArmAnswerInputCap},
		Aggregation: evalAggregationProtocol{AnswerRepetitions: 1, Rule: "majority_correctness", JudgeRepetitions: 1, SeedPolicy: "independent-recorded"},
	}
	if err := attachJevArmsRegistrationForFreeze(opt, &pilot); err != nil {
		t.Fatalf("pilot freeze refused: %v", err)
	}
	if pilot.Experiment.Filter == nil || pilot.Experiment.Filter.AnswerRepetitions != 1 {
		t.Fatalf("the frozen manifest did not record answer_repetitions 1: %+v", pilot.Experiment.Filter)
	}
	if err := validateJevProtocolBinding(pilot); err != nil {
		t.Errorf("the consistent pilot manifest was refused: %v", err)
	}
	// The freeze-time helper must derive the repetitions from the declaration,
	// never silently keep the canonical 3 on a pilot freeze.
	drifted := pilot
	drifted.Aggregation.AnswerRepetitions = 3
	if err := validateJevProtocolBinding(drifted); err == nil {
		t.Error("a manifest whose aggregation (3) differs from the sealed registration (1) was accepted")
	}
	if jevFreezeAnswerRepetitions(opt) != 1 {
		t.Error("jevFreezeAnswerRepetitions did not follow --jev-arms-reps")
	}
	if jevFreezeAnswerRepetitions(options{}) != jevArmAnswerRepetitions {
		t.Error("a non-jev freeze must keep the canonical 3-repetition aggregation")
	}
}

// (t6) majority aggregation with a single measured repetition is that
// repetition's own verdict (majority-of-1), and unmeasured rows stay excluded.
func TestJevArmMajorityOutcomesSingleRepetition(t *testing.T) {
	rows := []jevArmQuestionRow{
		{Conv: 1, Q: 2, Arm: jevArmD, Repetition: 0, CorrectMeasured: true, Correct: true},
		{Conv: 1, Q: 3, Arm: jevArmD, Repetition: 0, CorrectMeasured: true, Correct: false},
		{Conv: 1, Q: 4, Arm: jevArmD, Repetition: 0, CorrectMeasured: false},
	}
	outcomes, err := jevArmMajorityOutcomes(rows)
	if err != nil {
		t.Fatalf("1-repetition majority failed: %v", err)
	}
	if got, want := outcomes[jevArmOutcomeKey{Conv: 1, Q: 2, Arm: jevArmD}], true; got != want {
		t.Errorf("single correct repetition majority = %v, want %v", got, want)
	}
	if got, want := outcomes[jevArmOutcomeKey{Conv: 1, Q: 3, Arm: jevArmD}], false; got != want {
		t.Errorf("single wrong repetition majority = %v, want %v", got, want)
	}
	if _, ok := outcomes[jevArmOutcomeKey{Conv: 1, Q: 4, Arm: jevArmD}]; ok {
		t.Error("an unmeasured repetition produced a majority outcome")
	}
	// A canonical 3-rep group still collapses by majority, unchanged.
	three := append([]jevArmQuestionRow{}, rows[1])
	three = append(three,
		jevArmQuestionRow{Conv: 1, Q: 3, Arm: jevArmD, Repetition: 1, CorrectMeasured: true, Correct: true},
		jevArmQuestionRow{Conv: 1, Q: 3, Arm: jevArmD, Repetition: 2, CorrectMeasured: true, Correct: true},
	)
	outcomes, err = jevArmMajorityOutcomes(three)
	if err != nil {
		t.Fatalf("3-repetition majority failed: %v", err)
	}
	if got := outcomes[jevArmOutcomeKey{Conv: 1, Q: 3, Arm: jevArmD}]; !got {
		t.Error("the canonical 2-of-3 majority flipped")
	}
}

// (t7) the same-window declaration keeps its exact shape at reps=1: it is still
// required (the declaration cost nothing at one repetition, so there is no
// reason to drop the receipt), and a fully declared pilot passes.
func TestJevSameWindowRepsStillRequiredForPilot(t *testing.T) {
	pilot := jevFilterRegistration{AnswerRepetitions: 1, PilotGateConfirmed: true, WarmupDisposed: true}
	err := validateJevDeclaredPrerequisites(pilot)
	if err == nil {
		t.Fatal("a pilot run without --jev-same-window-reps was accepted")
	}
	if !strings.Contains(err.Error(), "--jev-same-window-reps") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
	pilot.SameWindowReps = true
	if err := validateJevDeclaredPrerequisites(pilot); err != nil {
		t.Errorf("a fully declared pilot was refused: %v", err)
	}
}
