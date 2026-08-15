package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func TestUnifiedPromptPairObserverCapturesActualCalls(t *testing.T) {
	observer := newUnifiedPromptPairObserver()
	answer := observer.wrapAnswer(func(_ context.Context, system, user string) (string, provider.Usage, error) {
		if system != "answer-system" || user != "exact-user-bytes" {
			t.Fatalf("answer input = %q / %q", system, user)
		}
		return "<think>draft</think> final", provider.Usage{InputTokens: 7, OutputTokens: 2}, nil
	})
	judge := observer.wrapJudge(func(_ context.Context, system, user string) (string, provider.Usage, error) {
		return `{"correct":true}`, provider.Usage{InputTokens: 5, OutputTokens: 1}, nil
	})

	if _, _, err := answer(context.Background(), "answer-system", "exact-user-bytes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := judge(context.Background(), "judge-system", "judge-user"); err != nil {
		t.Fatal(err)
	}
	audit := observer.snapshot()
	if audit.Schema != unifiedPromptPairAuditSchema || len(audit.Answer) != 1 || len(audit.Judge) != 1 {
		t.Fatalf("audit = %#v", audit)
	}
	if got := audit.Answer[0]; !got.Success || got.Status != unifiedPromptPairCallOK ||
		got.SystemDigest != evalTextDigest("answer-system") || got.UserDigest != evalTextDigest("exact-user-bytes") ||
		got.OutputDigest != evalTextDigest("<think>draft</think> final") || got.InputTokens != 7 || got.OutputTokens != 2 {
		t.Fatalf("answer audit = %#v", got)
	}
	if got := audit.Judge[0]; !got.Success || got.Status != unifiedPromptPairCallOK || got.UserDigest != evalTextDigest("judge-user") {
		t.Fatalf("judge audit = %#v", got)
	}
	if audit.Judge[0].JudgeCorrect == nil || !*audit.Judge[0].JudgeCorrect {
		t.Fatalf("judge correctness = %#v", audit.Judge[0].JudgeCorrect)
	}
}

func TestUnifiedPromptPairObserverRejectsHiddenFailures(t *testing.T) {
	tests := []struct {
		name   string
		answer bool
		call   usageModelCaller
		status string
	}{
		{name: "transport", answer: true, status: unifiedPromptPairCallTransportError, call: func(context.Context, string, string) (string, provider.Usage, error) {
			return "", provider.Usage{}, errors.New("endpoint failed")
		}},
		{name: "empty answer", answer: true, status: unifiedPromptPairCallEmptyAnswer, call: func(context.Context, string, string) (string, provider.Usage, error) {
			return "<think>nothing final</think>", provider.Usage{}, nil
		}},
		{name: "malformed judge", status: unifiedPromptPairCallInvalidJudge, call: func(context.Context, string, string) (string, provider.Usage, error) {
			return `prefix {"correct": true}`, provider.Usage{}, nil
		}},
		{name: "unknown judge field", status: unifiedPromptPairCallInvalidJudge, call: func(context.Context, string, string) (string, provider.Usage, error) {
			return `{"correct":true,"reason":"x"}`, provider.Usage{}, nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			observer := newUnifiedPromptPairObserver()
			wrapped := observer.wrapJudge(tc.call)
			if tc.answer {
				wrapped = observer.wrapAnswer(tc.call)
			}
			_, _, _ = wrapped(context.Background(), "system", "user")
			audit := observer.snapshot()
			attempts := audit.Judge
			if tc.answer {
				attempts = audit.Answer
			}
			if len(attempts) != 1 || attempts[0].Success || attempts[0].Status != tc.status {
				t.Fatalf("attempts = %#v", attempts)
			}
		})
	}
}

func TestValidateUnifiedPromptPairExperiment(t *testing.T) {
	base := options{retrieval: "hybrid,hybrid+unified", noIDKRetry: true, repeats: 3}
	arms := []string{"hybrid", "hybrid+unified"}
	singleUnifiedArm := []string{"hybrid+unified"}
	if !isExactUnifiedPromptPair(base, arms) {
		t.Fatal("expected exact unified prompt pair")
	}
	if err := validateUnifiedPromptPairExperiment(base, arms); err != nil {
		t.Fatalf("valid pair: %v", err)
	}
	even := base
	even.repeats = 2
	if err := validateUnifiedPromptPairExperiment(even, arms); err == nil || !strings.Contains(err.Error(), "positive odd") {
		t.Fatalf("even paired repetitions accepted: %v", err)
	}
	for _, invalidArms := range [][]string{
		{"hybrid+unified", "hybrid"},
		{"fts", "hybrid+unified"},
		{"fts", "hybrid", "hybrid+unified"},
	} {
		if !hasUnifiedPromptArm(base, invalidArms) {
			t.Fatalf("unified arm not detected: %v", invalidArms)
		}
		if err := validateUnifiedPromptPairExperiment(base, invalidArms); err == nil {
			t.Fatalf("invalid unified arm layout accepted: %v", invalidArms)
		}
	}
	global := base
	global.unifiedAnswerContract = true
	if !hasUnifiedPromptArm(global, arms) {
		t.Fatal("global unified mode not detected")
	}
	if err := validateUnifiedPromptPairExperiment(global, arms); err == nil {
		t.Fatal("global unified mode accepted as a prompt-only control/treatment pair")
	}
	// Standalone unified runs must not be mistaken for paired syntax, whether
	// via a global flag with one arm or via a single +unified arm.
	for _, single := range [][]string{{"hybrid"}, {"hybrid+unified"}, {"fts+unified"}} {
		if unifiedPromptPairExperimentRequested(global, single) {
			t.Fatalf("standalone unified run was mistaken for paired syntax: %v", single)
		}
	}
	if unifiedPromptPairExperimentRequested(base, singleUnifiedArm) {
		t.Fatal("single +unified arm was treated as a paired experiment")
	}
	if !unifiedPromptPairExperimentRequested(global, arms) {
		t.Fatal("ambiguous global-unified multi-arm syntax was not detected")
	}

	for name, mutate := range map[string]func(*options){
		"global unified":      func(o *options) { o.unifiedAnswerContract = true },
		"force answer":        func(o *options) { o.forceAnswer = true },
		"temporal prompt":     func(o *options) { o.temporalAnswerPrompt = true },
		"unified typed":       func(o *options) { o.unifiedTypedPrompts = true },
		"rerank":              func(o *options) { o.rerank = true },
		"write dedup":         func(o *options) { o.writeDedup = true },
	} {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if err := validateUnifiedPromptPairExperiment(got, arms); err == nil {
				t.Fatal("expected isolation error")
			}
		})
	}
}

// TestStandaloneUnifiedArmAppliesContract guards the configurable standalone
// mode: a single hybrid+unified arm (or a global unified flag with one arm) must
// run the unified contract without requiring the paired control arm. This is the
// flexible, non-contrast configuration — the pair protocol remains available via
// the exact hybrid,hybrid+unified syntax.
func TestStandaloneUnifiedArmAppliesContract(t *testing.T) {
	base := options{retrieval: "hybrid+unified", noIDKRetry: true, repeats: 2}
	armOpt := optionsForRun(base, "hybrid+unified", false)
	if !armOpt.unifiedAnswerContract {
		t.Fatal("standalone hybrid+unified arm did not apply the unified contract")
	}
	if unifiedPromptPairExperimentRequested(base, []string{"hybrid+unified"}) {
		t.Fatal("standalone unified arm was routed into the paired protocol")
	}
	if unifiedPromptPairExperimentRequested(base, []string{"hybrid"}) {
		t.Fatal("plain hybrid standalone was routed into the paired protocol")
	}

	// Even repeats are fine outside the paired protocol; the odd-repeats rule is
	// a pair-experiment isolation rule, not a standalone constraint.
	even := base
	even.repeats = 4
	if unifiedPromptPairExperimentRequested(even, []string{"hybrid+unified"}) {
		t.Fatal("standalone unified arm with even repeats was rejected by pair rules")
	}
}

func TestValidateUnifiedPromptPairRepeatIsStrictAndOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	qa := locomoQA{
		Question: "Where did Ana go?", Answer: json.RawMessage(`"Oslo"`), Category: 4,
		QuestionID: "conv-0-q-0", CategoryName: "single-hop",
	}
	convs := []conversation{{ID: 0, QA: []locomoQA{qa}}}
	opt := options{datasetFormat: "locomo", noIDKRetry: true, repeats: 1, unifiedPairDatasetDigest: evalTextDigest("dataset")}
	arms := []string{"hybrid", "hybrid+unified"}

	control := makeUnifiedPromptPairTestResult(qa, opt, arms[0], 0, 0, "same-user")
	treatment := makeUnifiedPromptPairTestResult(qa, opt, arms[1], 0, 0, "same-user")
	writeUnifiedPromptPairTestJournal(t, dir, arms[1], []result{treatment})
	writeUnifiedPromptPairTestJournal(t, dir, arms[0], []result{control})

	receipt, err := validateUnifiedPromptPairRepeat(dir, opt, convs, arms)
	if err != nil {
		t.Fatalf("validate pair: %v", err)
	}
	if !receipt.Valid || receipt.QuestionCount != 1 || len(receipt.ControlPromptDigests) != 1 || receipt.ControlPromptDigests[0] != evalTextDigest(answerSystemPrompt) || receipt.TreatmentPromptDigest != evalTextDigest(unifiedAnswerContractPrompt) {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.DatasetDigest != opt.unifiedPairDatasetDigest || receipt.ProviderAttemptPolicy == "" || receipt.ArmSchedulingPolicy == "" {
		t.Fatalf("receipt provenance = %#v", receipt)
	}
}

func TestValidateUnifiedPromptPairRepeatRequiresDatasetDigest(t *testing.T) {
	opt := options{datasetFormat: "locomo", noIDKRetry: true, repeats: 1}
	receipt, err := validateUnifiedPromptPairRepeat(t.TempDir(), opt, nil, []string{"hybrid", "hybrid+unified"})
	if err == nil || !strings.Contains(err.Error(), "dataset digest") || receipt.Valid {
		t.Fatalf("missing dataset digest validated: receipt=%#v err=%v", receipt, err)
	}
}

func TestValidateUnifiedPromptPairRepeatRejectsMismatchMissingAndDuplicate(t *testing.T) {
	qaA := locomoQA{Question: "A?", Answer: json.RawMessage(`"A"`), Category: 4, QuestionID: "conv-0-q-0"}
	qaB := locomoQA{Question: "B?", Answer: json.RawMessage(`"B"`), Category: 4, QuestionID: "conv-0-q-1"}
	convs := []conversation{{ID: 0, QA: []locomoQA{qaA, qaB}}}
	opt := options{datasetFormat: "locomo", noIDKRetry: true, repeats: 1, unifiedPairDatasetDigest: evalTextDigest("dataset")}
	arms := []string{"hybrid", "hybrid+unified"}

	tests := []struct {
		name       string
		controls   []result
		treatments []result
		want       string
	}{
		{
			name:       "context mismatch",
			controls:   []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[0], 0, 0, "control"), makeUnifiedPromptPairTestResult(qaB, opt, arms[0], 0, 1, "same")},
			treatments: []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[1], 0, 0, "treatment"), makeUnifiedPromptPairTestResult(qaB, opt, arms[1], 0, 1, "same")},
			want:       "answer user context digest",
		},
		{
			name:       "both arms omit expected row",
			controls:   []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[0], 0, 0, "same")},
			treatments: []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[1], 0, 0, "same")},
			want:       "missing expected",
		},
		{
			name:       "duplicate row",
			controls:   []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[0], 0, 0, "same"), makeUnifiedPromptPairTestResult(qaA, opt, arms[0], 0, 0, "same"), makeUnifiedPromptPairTestResult(qaB, opt, arms[0], 0, 1, "same")},
			treatments: []result{makeUnifiedPromptPairTestResult(qaA, opt, arms[1], 0, 0, "same"), makeUnifiedPromptPairTestResult(qaB, opt, arms[1], 0, 1, "same")},
			want:       "duplicate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeUnifiedPromptPairTestJournal(t, dir, arms[0], tc.controls)
			writeUnifiedPromptPairTestJournal(t, dir, arms[1], tc.treatments)
			_, err := validateUnifiedPromptPairRepeat(dir, opt, convs, arms)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateUnifiedPromptPairQuestionAuditBindsScoredRow(t *testing.T) {
	qa := locomoQA{Question: "Where?", Answer: json.RawMessage(`"Oslo"`), Category: 4, QuestionID: "conv-0-q-0"}
	opt := options{noIDKRetry: true}
	base := makeUnifiedPromptPairTestResult(qa, opt, "hybrid", 0, 0, "same-user")
	if err := validateUnifiedPromptPairQuestionAudit(base, qa, optionsForRun(opt, "hybrid", true)); err != nil {
		t.Fatalf("valid audit: %v", err)
	}
	for name, mutate := range map[string]func(*result){
		"predicted":   func(item *result) { item.Predicted = "tampered" },
		"judge input": func(item *result) { item.UnifiedPairAudit.Judge[0].UserDigest = evalTextDigest("tampered") },
		"correct":     func(item *result) { item.Correct = false },
	} {
		t.Run(name, func(t *testing.T) {
			item := base
			auditCopy := *base.UnifiedPairAudit
			auditCopy.Answer = append([]unifiedPromptPairCallAudit(nil), auditCopy.Answer...)
			auditCopy.Judge = append([]unifiedPromptPairCallAudit(nil), auditCopy.Judge...)
			item.UnifiedPairAudit = &auditCopy
			mutate(&item)
			if err := validateUnifiedPromptPairQuestionAudit(item, qa, optionsForRun(opt, "hybrid", true)); err == nil {
				t.Fatal("tampered scored row validated")
			}
		})
	}
}

func TestRequireFreshUnifiedPromptPairRunDirRejectsResume(t *testing.T) {
	dir := t.TempDir()
	if err := requireFreshUnifiedPromptPairRunDir(dir); err != nil {
		t.Fatalf("empty run dir: %v", err)
	}
	repeatDir := filepath.Join(dir, "run-1")
	if err := os.MkdirAll(repeatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repeatDir, "results-hybrid.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireFreshUnifiedPromptPairRunDir(dir); err == nil || !strings.Contains(err.Error(), "fresh --run-dir") {
		t.Fatalf("resume journal accepted: %v", err)
	}
}

func TestRequireFreshUnifiedPromptPairRunDirRejectsStaleScoreArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paired.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireFreshUnifiedPromptPairRunDir(dir); err == nil || !strings.Contains(err.Error(), "fresh --run-dir") {
		t.Fatalf("stale score artifact accepted: %v", err)
	}
}

func makeUnifiedPromptPairTestResult(qa locomoQA, opt options, arm string, conv, q int, answerUser string) result {
	armOpt := optionsForRun(opt, arm, true)
	return result{
		Conv: conv, Q: q, QuestionID: qa.QuestionID, Category: qa.Category, CategoryName: qa.CategoryName,
		Question: qa.Question, Gold: goldFor(qa), Predicted: "answer", Correct: true,
		RetrievalFlags: retrievalFingerprint(armOpt), AnswerRegime: answerRegimeFingerprint(armOpt),
		UnifiedPairAudit: &unifiedPromptPairQuestionAudit{
			Schema: unifiedPromptPairAuditSchema,
			Answer: []unifiedPromptPairCallAudit{{
				SystemDigest: evalTextDigest(answerSystemPromptForEval(qa, armOpt)), UserDigest: evalTextDigest(answerUser),
				OutputDigest: evalTextDigest("answer"), Success: true, Status: unifiedPromptPairCallOK,
			}},
			Judge: []unifiedPromptPairCallAudit{{
				SystemDigest: evalTextDigest(judgeSystemPromptFor(armOpt.judgeAlignmentMode())), UserDigest: evalTextDigest(buildJudgePrompt(qa.Question, goldFor(qa), "answer")),
				OutputDigest: evalTextDigest(`{"correct":true}`), Success: true, Status: unifiedPromptPairCallOK,
				JudgeCorrect: unifiedPairBoolPointer(true),
			}},
		},
	}
}

func unifiedPairBoolPointer(value bool) *bool { return &value }

func writeUnifiedPromptPairTestJournal(t *testing.T, dir, arm string, items []result) {
	t.Helper()
	path := filepath.Join(dir, "results-"+arm+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
