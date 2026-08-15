package main

// 043 confidence-gated gap-guided deepening — CLI boundary.
//
// The deepening protocol is a dedicated, default-off research mode of
// cmd/locomo-bench: --confidence-deepen false and --deepen-pilot empty keep the
// ordinary benchmark path byte-identical. --deepen-pilot dispatches early
// (before the --data gate), mirroring the 042 --utility-stage pattern. The
// signal pilot stage itself is a box-side task (T010) — this file wires the
// stage enum, validation, and dispatch; the stage runner body lives in
// confidence_deepen_pilot.go.

import (
	"fmt"
)

// Closed deepen pilot stage enum (v1 supports the signal-existence pilot only).
type deepenPilotStage string

const (
	deepenPilotStageSignal deepenPilotStage = "signal"
)

func parseDeepenPilotStage(s string) (deepenPilotStage, error) {
	st := deepenPilotStage(s)
	if st != deepenPilotStageSignal {
		return "", fmt.Errorf("invalid --deepen-pilot stage %q: must be signal", s)
	}
	return st, nil
}

// validateDeepenCLIOptions validates the deepen-related fields of opt. When
// --deepen-pilot is empty and --confidence-deepen is off it returns nil unless
// an auxiliary --deepen-* flag is set (set-but-ignored is a usage error).
func validateDeepenCLIOptions(opt *options) error {
	stage := opt.deepenPilot
	if stage == "" {
		if !opt.confidenceDeepen {
			// No mechanism, no pilot: any deepen flag is a usage error.
			if opt.deepenThreshold != 0 || opt.deepenSignalFeature != "" {
				return fmt.Errorf("--deepen-threshold/--deepen-signal-feature require --confidence-deepen or --deepen-pilot")
			}
			return nil
		}
		return validateDeepenMechanismFlags(opt)
	}
	st, err := parseDeepenPilotStage(stage)
	if err != nil {
		return err
	}
	if opt.confidenceDeepen {
		return fmt.Errorf("--deepen-pilot cannot be combined with --confidence-deepen (the pilot produces the seal the mechanism run consumes)")
	}
	if opt.deepenThreshold != 0 || opt.deepenSignalFeature != "" {
		return fmt.Errorf("--deepen-pilot must not pre-set --deepen-threshold/--deepen-signal-feature (the pilot seal freezes them)")
	}
	if opt.dataPath == "" {
		return fmt.Errorf("--deepen-pilot %s requires --data", st)
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--deepen-pilot %s requires --store-dir", st)
	}
	if opt.runDir == "" {
		return fmt.Errorf("--deepen-pilot %s requires --run-dir", st)
	}
	return nil
}

// validateDeepenMechanismFlags validates the --confidence-deepen mechanism run.
// threshold/featureName are read-only from the pilot seal: explicit non-default
// CLI values are rejected so the mechanism can never silently tune the gate
// (FR-005 / plan decision 5).
func validateDeepenMechanismFlags(opt *options) error {
	if !opt.unifiedAnswerContract {
		return fmt.Errorf("--confidence-deepen requires --unified-answer-contract")
	}
	if opt.deepenThreshold != 0 {
		return fmt.Errorf("--deepen-threshold is read-only from the pilot seal; leave 0 (unfinalized) to load the seal")
	}
	if opt.deepenSignalFeature != "" {
		return fmt.Errorf("--deepen-signal-feature is read-only from the pilot seal; leave empty to load the seal")
	}
	if opt.deepenK <= 0 {
		return fmt.Errorf("--deepen-k must be positive, got %d", opt.deepenK)
	}
	if opt.deepenMaxGaps < 1 || opt.deepenMaxGaps > 3 {
		return fmt.Errorf("--deepen-max-gaps must be in [1,3], got %d", opt.deepenMaxGaps)
	}
	return nil
}

// runDeepenPilotCLI is the early-dispatch entry: it runs the selected deepen
// stage without touching the ordinary benchmark path.
func runDeepenPilotCLI(opt *options) error {
	st, err := parseDeepenPilotStage(opt.deepenPilot)
	if err != nil {
		return err
	}
	switch st {
	case deepenPilotStageSignal:
		return runDeepenSignalPilotStage(opt)
	}
	return fmt.Errorf("unhandled deepen pilot stage %s", st)
}
