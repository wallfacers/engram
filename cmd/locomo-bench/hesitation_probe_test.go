package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeResultsJSONL(t *testing.T, path string, results []result) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, r := range results {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestRunHesitationProbeCounts(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "results-hybrid.jsonl")
	// 3 wrong (2 hesitant) + 2 right (0 hesitant): recall 0.667 >= 0.60, fp 0 <= 0.30 → PASS.
	writeResultsJSONL(t, jsonl, []result{
		{Conv: 0, Q: 0, Correct: false, Predicted: "I'm not sure. Could be Berlin."},
		{Conv: 0, Q: 1, Correct: false, Predicted: "Could be Paris, not sure."},
		{Conv: 0, Q: 2, Correct: false, Predicted: "Paris"}, // confident wrong
		{Conv: 0, Q: 3, Correct: true, Predicted: "Berlin"},
		{Conv: 0, Q: 4, Correct: true, Predicted: "Berlin"},
	})
	opt := options{
		runDir:              dir,
		probeHesitation:     true,
		probeHesitationJSONL: jsonl,
		confidenceThreshold: 3.0,
	}
	if err := runHesitationProbeCLI(opt); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "hesitation-probe.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var rep hesitationProbeReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if rep.Questions != 5 || rep.WrongTotal != 3 || rep.WrongHesitant != 2 || rep.RightTotal != 2 || rep.RightHesitant != 0 {
		t.Fatalf("counts wrong: %+v", rep)
	}
	if rep.WrongRecall != 2.0/3.0 || rep.RightFalsePos != 0 {
		t.Fatalf("rates wrong: recall=%.3f fp=%.3f", rep.WrongRecall, rep.RightFalsePos)
	}
	if !rep.Pass {
		t.Fatalf("gates should pass: %+v", rep)
	}
}

func TestRunHesitationProbeFailsClosed(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "results-hybrid.jsonl")
	// 2 wrong, 0 hesitant: recall 0 < 0.60 → FAIL closed (US1 stop line).
	writeResultsJSONL(t, jsonl, []result{
		{Conv: 0, Q: 0, Correct: false, Predicted: "Paris"},
		{Conv: 0, Q: 1, Correct: false, Predicted: "Berlin"},
		{Conv: 0, Q: 2, Correct: true, Predicted: "Berlin"},
	})
	opt := options{runDir: dir, probeHesitationJSONL: jsonl, confidenceThreshold: 3.0}
	if err := runHesitationProbeCLI(opt); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "hesitation-probe.json"))
	var rep hesitationProbeReport
	_ = json.Unmarshal(raw, &rep)
	if rep.Pass {
		t.Fatalf("gates must fail closed with zero hesitant wrongs: %+v", rep)
	}
}
