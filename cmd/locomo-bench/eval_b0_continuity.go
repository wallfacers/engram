package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/wallfacers/engram/provider"
)

const evalB0ContinuitySummaryFile = "b0_continuity_summary.json"

func b0ContinuityArtifactsPresent(runDir string) bool {
	raw, err := os.ReadFile(filepath.Join(runDir, evalProtocolArtifactFile)) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return false
	}
	var protocol evalProtocol
	return json.Unmarshal(raw, &protocol) == nil && protocol.Experiment.Stage == "b0"
}

func runB0ContinuityValidateCLI(opt options) error {
	if opt.evalValidate == "" || opt.dataPath == "" {
		return fmt.Errorf("B0 --eval-validate requires the run directory and --data")
	}
	protocol, err := readEvalProtocolFileMode(
		filepath.Join(opt.evalValidate, evalProtocolArtifactFile), evalRunB0Continuity,
	)
	if err != nil {
		return err
	}
	convs, err := loadBenchmarkDataset(opt.dataPath, opt.datasetFormat, opt.imageCaptions)
	if err != nil {
		return err
	}
	if err := verifyFormalDataset(protocol, opt.dataPath, opt.datasetFormat, convs); err != nil {
		return err
	}
	runs, err := loadArmRuns(
		opt.evalValidate, protocol.Retrieval.Recipe, protocol.Aggregation.AnswerRepetitions,
	)
	if err != nil {
		return err
	}
	derived, err := deriveB0ContinuitySummary(
		opt.evalValidate, protocol, runs, formalQuestionIDs(opt.datasetFormat, convs),
	)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(opt.evalValidate, evalB0ContinuitySummaryFile)) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return fmt.Errorf("read persisted B0 continuity summary: %w", err)
	}
	var persisted evalB0ContinuitySummary
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return fmt.Errorf("decode persisted B0 continuity summary: %w", err)
	}
	if evalJSONDigest(persisted) != evalJSONDigest(derived) {
		return fmt.Errorf("persisted B0 continuity summary differs from independent journal validation")
	}
	fmt.Printf(
		"eval-validate-b0: protocol=%s majority_correct=%d/%d valid=true promotion_eligible=false\n",
		protocol.ProtocolHash, derived.MajorityCorrect, derived.Denominator,
	)
	return nil
}

func validateB0ContinuityMode(opt options) error {
	running := opt.evalB0ProtocolPath != ""
	freezing := opt.evalFreezeB0Protocol != ""
	if !running && !freezing {
		return nil
	}
	if running && freezing {
		return fmt.Errorf("--eval-b0-protocol and --eval-freeze-b0-protocol are mutually exclusive")
	}
	conflicts := []struct {
		enabled bool
		name    string
	}{
		{opt.compareSpec != "", "--compare"},
		{opt.evalValidate != "", "--eval-validate"},
		{opt.evalProtocolPath != "", "--eval-protocol"},
		{opt.evalFreezeProtocol != "", "--eval-freeze-protocol"},
		{opt.fixedGoldOracle, "--fixed-gold-oracle"},
		{opt.tokenCounterCalibrate, "--token-counter-calibrate"},
	}
	for _, conflict := range conflicts {
		if conflict.enabled {
			return fmt.Errorf("B0 continuity mode cannot be combined with %s", conflict.name)
		}
	}
	return nil
}

// evalB0ContinuityRun is deliberately smaller than evalFormalQuestionRun.
// B0 exercises the current legacy product path, including adaptive IDK retry,
// and therefore records calls rather than pretending to own a frozen
// Candidate/Trace/Bundle. It is never a treatment control.
type evalB0ContinuityRun struct {
	Schema       string `json:"schema"`
	ProtocolHash string `json:"protocol_hash"`
	RunIndex     int    `json:"run_index"`
	AnswerCalls  int    `json:"answer_calls"`
	RewriteCalls int    `json:"rewrite_calls"`
	JudgeCalls   int    `json:"judge_calls"`
	LegacyRetry  bool   `json:"legacy_retry"`
}

type b0CallRecorder struct {
	mu  sync.Mutex
	run evalB0ContinuityRun
}

func newB0CallRecorder(protocolHash string, runIndex int) *b0CallRecorder {
	return &b0CallRecorder{run: evalB0ContinuityRun{
		Schema: evalProtocolSchema, ProtocolHash: protocolHash, RunIndex: runIndex,
	}}
}

func (recorder *b0CallRecorder) wrapAnswer(call usageModelCaller) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		recorder.mu.Lock()
		recorder.run.AnswerCalls++
		recorder.mu.Unlock()
		return call(ctx, system, user)
	}
}

func (recorder *b0CallRecorder) wrapRewrite(call modelCaller) modelCaller {
	return func(ctx context.Context, system, user string) (string, error) {
		recorder.mu.Lock()
		recorder.run.RewriteCalls++
		recorder.mu.Unlock()
		return call(ctx, system, user)
	}
}

func (recorder *b0CallRecorder) wrapJudge(call usageModelCaller) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		recorder.mu.Lock()
		recorder.run.JudgeCalls++
		recorder.mu.Unlock()
		return call(ctx, system, user)
	}
}

func (recorder *b0CallRecorder) snapshot() evalB0ContinuityRun {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	run := recorder.run
	run.LegacyRetry = run.AnswerCalls > 1 || run.RewriteCalls > 0
	return run
}

type evalB0ContinuitySummary struct {
	Schema                   string   `json:"schema"`
	ProtocolHash             string   `json:"protocol_hash"`
	Valid                    bool     `json:"valid"`
	PromotionEligible        bool     `json:"promotion_eligible"`
	Denominator              int      `json:"denominator"`
	Repetitions              int      `json:"repetitions"`
	MajorityCorrect          int      `json:"majority_correct,omitempty"`
	AnswerCalls              int      `json:"answer_calls"`
	RewriteCalls             int      `json:"rewrite_calls"`
	JudgeCalls               int      `json:"judge_calls"`
	QuestionsWithLegacyRetry int      `json:"questions_with_legacy_retry"`
	InvalidReasons           []string `json:"invalid_reasons,omitempty"`
}

// materializeB0ContinuitySummary validates the independent legacy journals.
// It explicitly refuses every B1-only artifact so a continuity run cannot be
// mistaken for a source-valid causal control or become promotion eligible.
func materializeB0ContinuitySummary(runDir string, protocol evalProtocol, runs [][]result, expectedQuestionIDs []string) (evalB0ContinuitySummary, error) {
	summary, validationErr := deriveB0ContinuitySummary(runDir, protocol, runs, expectedQuestionIDs)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return summary, fmt.Errorf("create B0 summary directory: %w", err)
	}
	if err := writeJSON(filepath.Join(runDir, evalB0ContinuitySummaryFile), summary); err != nil {
		return summary, fmt.Errorf("write B0 continuity summary: %w", err)
	}
	return summary, validationErr
}

func deriveB0ContinuitySummary(runDir string, protocol evalProtocol, runs [][]result, expectedQuestionIDs []string) (evalB0ContinuitySummary, error) {
	summary := evalB0ContinuitySummary{
		Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash,
		Denominator: len(expectedQuestionIDs), Repetitions: len(runs),
		PromotionEligible: false,
	}
	var invalid []string
	if protocol.Experiment.Stage != "b0" {
		invalid = append(invalid, "protocol_not_b0")
	}
	if protocol.ProtocolHash == "" {
		invalid = append(invalid, "protocol_hash_missing")
	}
	if protocol.Benchmark.QuestionCount != len(expectedQuestionIDs) ||
		protocol.Benchmark.QuestionIDsDigest != evalJSONDigest(expectedQuestionIDs) {
		invalid = append(invalid, "denominator_mismatch")
	}
	if len(runs) != protocol.Aggregation.AnswerRepetitions {
		invalid = append(invalid, "repetition_count_mismatch")
	}
	forbidden := []string{
		evalCandidatesArtifactFile,
		evalTraceArtifactFile,
		evalBundleArtifactFile,
		evalClassificationArtifactFile,
		formalFreezeJournalFile,
		formalCallJournalFile,
		evalFixedGoldOracleArtifactFile,
		evalFixedGoldOracleJournalFile,
	}
	for _, name := range forbidden {
		if _, err := os.Stat(filepath.Join(runDir, name)); err == nil {
			invalid = append(invalid, "b1_artifact_present:"+name)
		} else if !os.IsNotExist(err) {
			invalid = append(invalid, "artifact_stat_failed:"+name)
		}
	}

	expected := make(map[string]struct{}, len(expectedQuestionIDs))
	for _, questionID := range expectedQuestionIDs {
		if _, duplicate := expected[questionID]; duplicate || questionID == "" {
			invalid = append(invalid, "invalid_expected_question_ids")
			continue
		}
		expected[questionID] = struct{}{}
	}
	correctByQuestion := make(map[string]int, len(expected))
	retriedQuestions := make(map[string]struct{})
	for runIndex, current := range runs {
		seen := make(map[string]struct{}, len(current))
		if len(current) != len(expected) {
			invalid = append(invalid, fmt.Sprintf("run_%d_denominator_mismatch", runIndex+1))
		}
		for _, item := range current {
			if _, ok := expected[item.QuestionID]; !ok {
				invalid = append(invalid, fmt.Sprintf("run_%d_unknown_question", runIndex+1))
				continue
			}
			if _, duplicate := seen[item.QuestionID]; duplicate {
				invalid = append(invalid, fmt.Sprintf("run_%d_duplicate_question", runIndex+1))
				continue
			}
			seen[item.QuestionID] = struct{}{}
			if item.Formal022 != nil {
				invalid = append(invalid, fmt.Sprintf("run_%d_b1_payload_present", runIndex+1))
			}
			receipt := item.B0Continuity
			if receipt == nil {
				invalid = append(invalid, fmt.Sprintf("run_%d_b0_receipt_missing", runIndex+1))
				continue
			}
			if receipt.Schema != evalProtocolSchema ||
				receipt.ProtocolHash != protocol.ProtocolHash ||
				receipt.RunIndex != runIndex+1 {
				invalid = append(invalid, fmt.Sprintf("run_%d_b0_provenance_invalid", runIndex+1))
			}
			if receipt.AnswerCalls < 1 ||
				receipt.AnswerCalls > protocol.Budget.AnswerCallLimit ||
				receipt.RewriteCalls < 0 || receipt.RewriteCalls > 1 ||
				receipt.JudgeCalls != protocol.Aggregation.JudgeRepetitions ||
				receipt.LegacyRetry != (receipt.AnswerCalls > 1 || receipt.RewriteCalls > 0) {
				invalid = append(invalid, fmt.Sprintf("run_%d_b0_call_receipt_invalid", runIndex+1))
			}
			summary.AnswerCalls += receipt.AnswerCalls
			summary.RewriteCalls += receipt.RewriteCalls
			summary.JudgeCalls += receipt.JudgeCalls
			if receipt.LegacyRetry {
				retriedQuestions[item.QuestionID] = struct{}{}
			}
			if item.Correct {
				correctByQuestion[item.QuestionID]++
			}
		}
	}
	for _, questionID := range expectedQuestionIDs {
		if correctByQuestion[questionID] > len(runs)/2 {
			summary.MajorityCorrect++
		}
	}
	summary.QuestionsWithLegacyRetry = len(retriedQuestions)
	sort.Strings(invalid)
	summary.InvalidReasons = stableStrings(invalid)
	summary.Valid = len(summary.InvalidReasons) == 0
	if !summary.Valid {
		return summary, fmt.Errorf("B0 continuity validation failed: %v", summary.InvalidReasons)
	}
	return summary, nil
}
