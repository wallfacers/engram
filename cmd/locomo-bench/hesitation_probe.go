package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --probe-hesitation: the 041 US1 life-or-death diagnostic. It reads an
// existing run's results-hybrid.jsonl (correct/gold/predicted) and runs the
// deterministic hesitation detector over each prediction, producing a
// 2x2 confusion matrix and the discrimination gates (research Decision 2):
//
//   - wrong-recall  = fraction of WRONG answers whose generation is hesitant
//     (must be >= 0.60 — 040 found 89% human-judged; automatic rules are
//     intentionally stricter, leaving headroom)
//   - right-false-pos = fraction of CORRECT answers whose generation is
//     hesitant (must be <= 0.30 — else we over-deepen and burn the budget)
//
// Zero model cost: reuses existing run artifacts, no new retrieval/answer/judge.
// Fails the whole feature closed when either gate misses (spec US1 Acceptance 3).

const (
	hesitationWrongRecallGate = 0.60
	hesitationRightFpGate     = 0.30
)

// hesitationProbeReport is the US1 discrimination summary (written to
// run-dir/hesitation-probe.json).
type hesitationProbeReport struct {
	Source        string  `json:"source"`
	Questions     int     `json:"questions"`
	WrongTotal    int     `json:"wrong_total"`
	WrongHesitant int     `json:"wrong_hesitant"`
	RightTotal    int     `json:"right_total"`
	RightHesitant int     `json:"right_hesitant"`
	WrongRecall   float64 `json:"wrong_recall"`
	RightFalsePos float64 `json:"right_false_pos"`
	// Gates (research Decision 2). Pass requires BOTH.
	WrongRecallGate float64 `json:"wrong_recall_gate"`
	RightFpGate     float64 `json:"right_fp_gate"`
	Pass            bool    `json:"pass"`
	Threshold       float64 `json:"threshold"`
}

func runHesitationProbeCLI(opt options) error {
	path := opt.probeHesitationJSONL
	if strings.TrimSpace(path) == "" {
		if strings.TrimSpace(opt.runDir) == "" {
			return fmt.Errorf("--probe-hesitation requires --probe-hesitation-jsonl <results-hybrid.jsonl> or --run-dir")
		}
		path = filepath.Join(opt.runDir, "results-hybrid.jsonl")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open results jsonl: %w", err)
	}
	defer f.Close()

	report := hesitationProbeReport{Source: path, Threshold: opt.confidenceThreshold}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r result
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("malformed results line: %w", err)
		}
		_, deepened := detectHesitation(r.Predicted, opt.confidenceThreshold)
		if r.Correct {
			report.RightTotal++
			if deepened {
				report.RightHesitant++
			}
		} else {
			report.WrongTotal++
			if deepened {
				report.WrongHesitant++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan results jsonl: %w", err)
	}
	report.Questions = report.WrongTotal + report.RightTotal
	if report.WrongTotal > 0 {
		report.WrongRecall = float64(report.WrongHesitant) / float64(report.WrongTotal)
	}
	if report.RightTotal > 0 {
		report.RightFalsePos = float64(report.RightHesitant) / float64(report.RightTotal)
	}
	report.WrongRecallGate = hesitationWrongRecallGate
	report.RightFpGate = hesitationRightFpGate
	report.Pass = report.WrongRecall >= hesitationWrongRecallGate && report.RightFalsePos <= hesitationRightFpGate

	out := filepath.Join(opt.runDir, "hesitation-probe.json")
	if strings.TrimSpace(opt.runDir) == "" {
		out = "hesitation-probe.json"
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return fmt.Errorf("write hesitation probe: %w", err)
	}

	fmt.Printf("hesitation probe: n=%d wrong=%d (hesitant %.1f%%) right=%d (false-pos %.1f%%) gates=%.0f%%/%.0f%% pass=%t report=%s\n",
		report.Questions, report.WrongTotal, report.WrongRecall*100, report.RightTotal, report.RightFalsePos*100,
		hesitationWrongRecallGate*100, hesitationRightFpGate*100, report.Pass, out)
	return nil
}
