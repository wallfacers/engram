package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// --confidence-calibrate: 041 US3 threshold sweep over an existing
// results-hybrid.jsonl. For each threshold in [0, 6] (step 0.5) it recomputes
// the discrimination gates (wrong-recall, right-false-pos, and the implied
// average evidence budget under the 30→150 ladder). Reports the threshold
// bands that satisfy both gates and the point of best recall/budget balance,
// so the default --confidence-threshold can be frozen on real data rather than
// an a priori guess (research Decision 2; calibration-failure warning from
// When Should Active RAG Retrieve 2607.24010).
//
// Zero model cost: reuses the existing run's predicted + correct labels.
type calibratePoint struct {
	Threshold   float64 `json:"threshold"`
	WrongRecall float64 `json:"wrong_recall"`
	RightFP     float64 `json:"right_false_pos"`
	// AvgEvidence is the implied mean evidence items under the 30→150 ladder:
	// (1-fp)*30 + fp*180. 150 is the fixed-budget comparator.
	AvgEvidence float64 `json:"avg_evidence"`
	Pass        bool    `json:"pass"`
}

type calibrateReport struct {
	Source          string           `json:"source"`
	Points          []calibratePoint `json:"points"`
	BestThreshold   float64          `json:"best_threshold"`
	BestWrongRecall float64          `json:"best_wrong_recall"`
	BestRightFP     float64          `json:"best_right_fp"`
	BestAvgEvidence float64          `json:"best_avg_evidence"`
	AnyPass         bool             `json:"any_pass"`
}

func runConfidenceCalibrateCLI(opt options) error {
	path := opt.probeHesitationJSONL
	if path == "" {
		if opt.runDir == "" {
			return fmt.Errorf("--confidence-calibrate requires --probe-hesitation-jsonl <results-hybrid.jsonl> or --run-dir")
		}
		path = filepath.Join(opt.runDir, "results-hybrid.jsonl")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open results jsonl: %w", err)
	}
	defer f.Close()

	// Load predictions once, then sweep thresholds deterministically.
	var wrong, right []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r result
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("malformed results line: %w", err)
		}
		if r.Correct {
			right = append(right, r.Predicted)
		} else {
			wrong = append(wrong, r.Predicted)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan results jsonl: %w", err)
	}
	if len(wrong) == 0 || len(right) == 0 {
		return fmt.Errorf("results file needs both wrong and right predictions (got wrong=%d right=%d)", len(wrong), len(right))
	}

	report := calibrateReport{Source: path}
	for t := 0.0; t <= 6.0; t += 0.5 {
		// Score each prediction once per threshold.
		wr := 0
		for _, p := range wrong {
			if _, d := detectHesitation(p, t); d {
				wr++
			}
		}
		fp := 0
		for _, p := range right {
			if _, d := detectHesitation(p, t); d {
				fp++
			}
		}
		recall := float64(wr) / float64(len(wrong))
		rightFp := float64(fp) / float64(len(right))
		pass := recall >= hesitationWrongRecallGate && rightFp <= hesitationRightFpGate
		pt := calibratePoint{
			Threshold:   t,
			WrongRecall: recall,
			RightFP:     rightFp,
			AvgEvidence: (1-rightFp)*30 + rightFp*180,
			Pass:        pass,
		}
		report.Points = append(report.Points, pt)
		if pass && (!report.AnyPass || recall > report.BestWrongRecall) {
			report.AnyPass = true
			report.BestThreshold = t
			report.BestWrongRecall = recall
			report.BestRightFP = rightFp
			report.BestAvgEvidence = pt.AvgEvidence
		}
	}

	out := filepath.Join(opt.runDir, "confidence-calibrate.json")
	if opt.runDir == "" {
		out = "confidence-calibrate.json"
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return fmt.Errorf("write calibrate report: %w", err)
	}

	fmt.Printf("calibrate: n=%d wrong=%d right=%d best=%.1f (recall=%.3f fp=%.3f avg_ev=%.1f) any_pass=%t report=%s\n",
		len(wrong)+len(right), len(wrong), len(right),
		report.BestThreshold, report.BestWrongRecall, report.BestRightFP, report.BestAvgEvidence, report.AnyPass, out)
	for _, pt := range report.Points {
		fmt.Printf("  t=%.1f recall=%.3f fp=%.3f avg_ev=%.1f pass=%t\n", pt.Threshold, pt.WrongRecall, pt.RightFP, pt.AvgEvidence, pt.Pass)
	}
	return nil
}
