package main

import (
	"context"
	"fmt"
	"math"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

type evalTokenCalibrationFixture struct {
	Name            string
	Input           evidencecompiler.AnswerInput
	WantInputTokens int
}

type evalTokenCalibrationReport struct {
	Complete           bool
	MaxDelta           int
	CounterFingerprint string
}

// calibrateEvalTokenCounter checks counts on complete answer inputs, including
// system/user chat boundaries. It never derives a total by summing parts.
func calibrateEvalTokenCounter(ctx context.Context, counter evidencecompiler.TokenCounter, fixtures []evalTokenCalibrationFixture, expectedFingerprint string) (evalTokenCalibrationReport, error) {
	if counter == nil || len(fixtures) == 0 || expectedFingerprint == "" {
		return evalTokenCalibrationReport{}, fmt.Errorf("counter, fixtures, and expected fingerprint are required")
	}
	report := evalTokenCalibrationReport{Complete: true, CounterFingerprint: expectedFingerprint}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.Name == "" || fixture.WantInputTokens < 1 || seen[fixture.Name] {
			return evalTokenCalibrationReport{}, fmt.Errorf("calibration fixtures require unique names and positive expected counts")
		}
		seen[fixture.Name] = true
		count, err := counter.CountInput(ctx, fixture.Input)
		if err != nil {
			return evalTokenCalibrationReport{}, fmt.Errorf("count calibration fixture %q: %w", fixture.Name, err)
		}
		if err := validateEvalTokenCount(count, math.MaxInt, expectedFingerprint); err != nil {
			return evalTokenCalibrationReport{}, fmt.Errorf("calibration fixture %q: %w", fixture.Name, err)
		}
		delta := count.InputTokens - fixture.WantInputTokens
		if delta < 0 {
			delta = -delta
		}
		if delta > report.MaxDelta {
			report.MaxDelta = delta
		}
	}
	if report.MaxDelta != 0 {
		return evalTokenCalibrationReport{}, fmt.Errorf("token counter calibration drift: max delta %d", report.MaxDelta)
	}
	return report, nil
}

func validateEvalTokenCount(count evidencecompiler.TokenCount, cap int, expectedFingerprint string) error {
	if count.InputTokens < 1 {
		return fmt.Errorf("token counter returned non-positive input tokens")
	}
	if count.InputTokens > cap {
		return fmt.Errorf("answer input tokens %d exceed cap %d", count.InputTokens, cap)
	}
	if count.Fingerprint == "" || count.Fingerprint != expectedFingerprint {
		return fmt.Errorf("token counter fingerprint drift")
	}
	return nil
}
