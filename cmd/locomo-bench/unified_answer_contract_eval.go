package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/provider"
)

const (
	unifiedPromptPairAuditSchema        = "unified-prompt-pair-call-audit/v1"
	unifiedPromptPairValidationSchema   = "unified-prompt-pair-validation/v1"
	unifiedPromptPairCallOK             = "ok"
	unifiedPromptPairCallTransportError = "transport_error"
	unifiedPromptPairCallEmptyAnswer    = "empty_final_answer"
	unifiedPromptPairCallInvalidJudge   = "invalid_judge_json"
)

// unifiedPromptPairCallAudit records the exact bytes at the provider-facing
// answer/judge seam. It intentionally stores only digests and aggregate usage,
// never prompt text, model output, errors, or credentials.
type unifiedPromptPairCallAudit struct {
	SystemDigest string `json:"system_digest"`
	UserDigest   string `json:"user_digest"`
	OutputDigest string `json:"output_digest,omitempty"`
	Success      bool   `json:"success"`
	Status       string `json:"status"`
	LatencyMS    int64  `json:"latency_ms"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	JudgeCorrect *bool  `json:"judge_correct,omitempty"`
}

type unifiedPromptPairQuestionAudit struct {
	Schema string                       `json:"schema"`
	Answer []unifiedPromptPairCallAudit `json:"answer"`
	Judge  []unifiedPromptPairCallAudit `json:"judge"`
}

type unifiedPromptPairObserver struct {
	mu     sync.Mutex
	answer []unifiedPromptPairCallAudit
	judge  []unifiedPromptPairCallAudit
}

func newUnifiedPromptPairObserver() *unifiedPromptPairObserver {
	return &unifiedPromptPairObserver{}
}

func (o *unifiedPromptPairObserver) wrapAnswer(call usageModelCaller) usageModelCaller {
	return o.wrap("answer", call)
}

func (o *unifiedPromptPairObserver) wrapJudge(call usageModelCaller) usageModelCaller {
	return o.wrap("judge", call)
}

func (o *unifiedPromptPairObserver) wrap(role string, call usageModelCaller) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		started := time.Now()
		output, usage, err := call(ctx, system, user)
		attempt := unifiedPromptPairCallAudit{
			SystemDigest: evalTextDigest(system),
			UserDigest:   evalTextDigest(user),
			OutputDigest: evalTextDigest(output),
			Success:      err == nil,
			Status:       unifiedPromptPairCallOK,
			LatencyMS:    time.Since(started).Milliseconds(),
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
		}
		if err != nil {
			attempt.Success = false
			attempt.Status = unifiedPromptPairCallTransportError
		} else if role == "answer" && strings.TrimSpace(extractFinalAnswer(output)) == "" {
			attempt.Success = false
			attempt.Status = unifiedPromptPairCallEmptyAnswer
		} else if role == "judge" {
			correct, valid := parseUnifiedPromptPairJudge(output)
			if !valid {
				attempt.Success = false
				attempt.Status = unifiedPromptPairCallInvalidJudge
			} else {
				attempt.JudgeCorrect = &correct
			}
		}

		o.mu.Lock()
		if role == "answer" {
			o.answer = append(o.answer, attempt)
		} else {
			o.judge = append(o.judge, attempt)
		}
		o.mu.Unlock()
		return output, usage, err
	}
}

func (o *unifiedPromptPairObserver) snapshot() unifiedPromptPairQuestionAudit {
	if o == nil {
		return unifiedPromptPairQuestionAudit{Schema: unifiedPromptPairAuditSchema}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return unifiedPromptPairQuestionAudit{
		Schema: unifiedPromptPairAuditSchema,
		Answer: append([]unifiedPromptPairCallAudit(nil), o.answer...),
		Judge:  append([]unifiedPromptPairCallAudit(nil), o.judge...),
	}
}

func parseUnifiedPromptPairJudge(raw string) (bool, bool) {
	var verdict struct {
		Correct *bool `json:"correct"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(extractFinalAnswer(raw))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&verdict); err != nil || verdict.Correct == nil {
		return false, false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return false, false
	}
	return *verdict.Correct, true
}

// isExactUnifiedPromptPair recognizes the frozen single-variable experiment:
// the historical hybrid answer stack versus the same backend with only the
// unified prompt selector enabled. A global unified flag would make both arms
// treatment and therefore is rejected.
func isExactUnifiedPromptPair(opt options, arms []string) bool {
	if opt.unifiedAnswerContract || len(arms) != 2 {
		return false
	}
	control, err := parseArm(arms[0])
	if err != nil || control.backend != "hybrid" || control.overrides || len(control.mechanisms) != 0 {
		return false
	}
	treatment, err := parseArm(arms[1])
	return err == nil && treatment.backend == control.backend && treatment.overrides &&
		len(treatment.mechanisms) == 1 && treatment.mechanisms["unified"]
}

func hasUnifiedPromptArm(opt options, arms []string) bool {
	if opt.unifiedAnswerContract {
		return true
	}
	for _, arm := range arms {
		spec, err := parseArm(arm)
		if err == nil && spec.mechanisms["unified"] {
			return true
		}
	}
	return false
}

// unifiedPromptPairExperimentRequested reports whether the run uses the frozen
// paired-arm syntax (exactly two arms: hybrid control + hybrid+unified
// treatment). A single +unified arm, or a global --unified-answer-contract flag
// with a single arm, is a standalone unified run and intentionally does not
// request the paired protocol: the unified contract is independently runnable.
// A global unified flag with multiple arms is ambiguous — every arm would
// become treatment — and is routed into validation so it fails instead of
// silently treating each arm as treatment.
func unifiedPromptPairExperimentRequested(opt options, arms []string) bool {
	if opt.unifiedAnswerContract && len(arms) > 1 {
		return true
	}
	if len(arms) != 2 {
		return false
	}
	return hasUnifiedPromptArm(opt, arms)
}

// validateUnifiedPromptPairExperiment excludes answer- or evidence-changing
// helpers from the score comparison. This is an experiment-isolation rule, not
// a statement that the product contract cannot coexist with those capabilities
// after they are designed and tested under the same grounding contract.
func validateUnifiedPromptPairExperiment(opt options, arms []string) error {
	if !isExactUnifiedPromptPair(opt, arms) {
		return fmt.Errorf("unified prompt paired experiment requires exactly --retrieval=hybrid,hybrid+unified with no global --unified-answer-contract")
	}
	if opt.repeats <= 0 || opt.repeats%2 == 0 {
		return fmt.Errorf("unified prompt paired experiment requires a positive odd --repeats value, got %d", opt.repeats)
	}
	var conflicts []string
	for _, conflict := range []struct {
		enabled bool
		name    string
	}{
		{!opt.noIDKRetry, "missing --no-idk-retry"},
		{opt.forceAnswer, "--force-answer"},
		{opt.temporalAnswerPrompt, "--temporal-answer-prompt"},
		{opt.unifiedTypedPrompts, "--unified-typed-prompts"},
		{opt.temporalDateScaffold, "--temporal-date-scaffold"},
		{strings.TrimSpace(opt.catTopKSpec) != "", "--cat-top-k"},
		{strings.TrimSpace(opt.catQuotaSpec) != "", "--cat-chunk-quota"},
		{opt.rerank, "--rerank"},
		{opt.pcic, "--pcic"},
		{opt.oracle, "--oracle"},
		{opt.writeDedup, "--write-dedup"},
		{opt.neighborExtend, "--neighbor-extend"},
		{opt.episodeCluster, "--episode-cluster"},
		{strings.TrimSpace(opt.compilerArm) != "", "--compiler-arm"},
		{opt.representationArm != "" && opt.representationArm != ReprChunk900, "--representation"},
	} {
		if conflict.enabled {
			conflicts = append(conflicts, conflict.name)
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("unified prompt paired experiment must isolate the system prompt; incompatible configuration: %s", strings.Join(conflicts, ", "))
	}
	return nil
}

type unifiedPromptPairValidationReceipt struct {
	Schema                  string    `json:"schema"`
	Valid                   bool      `json:"valid"`
	ValidatedAt             time.Time `json:"validated_at"`
	Repeat                  int       `json:"repeat"`
	ConfiguredRepeats       int       `json:"configured_repeats"`
	QuestionCount           int       `json:"question_count"`
	ControlArm              string    `json:"control_arm"`
	TreatmentArm            string    `json:"treatment_arm"`
	ControlPromptDigests    []string  `json:"control_prompt_digests"`
	TreatmentPromptDigest   string    `json:"treatment_prompt_digest"`
	JudgePromptDigest       string    `json:"judge_prompt_digest"`
	AnswerModel             string    `json:"answer_model"`
	AnswerModelRevision     string    `json:"answer_model_revision"`
	AnswerProvider          string    `json:"answer_provider"`
	JudgeModel              string    `json:"judge_model"`
	JudgeModelRevision      string    `json:"judge_model_revision"`
	JudgeProvider           string    `json:"judge_provider"`
	DatasetFormat           string    `json:"dataset_format"`
	DatasetDigest           string    `json:"dataset_digest"`
	SelectedQuestionsDigest string    `json:"selected_questions_digest"`
	ContextParityMethod     string    `json:"context_parity_method"`
	TopK                    int       `json:"top_k"`
	ChunkQuota              int       `json:"chunk_quota"`
	Chunks                  bool      `json:"chunks"`
	MaxTokens               int       `json:"max_tokens"`
	Concurrency             int       `json:"concurrency"`
	ThinkingDisabled        bool      `json:"thinking_disabled"`
	ProviderAttemptPolicy   string    `json:"provider_attempt_policy"`
	ArmSchedulingPolicy     string    `json:"arm_scheduling_policy"`
	Failure                 string    `json:"failure,omitempty"`
}

type unifiedPromptPairExpectedQuestion struct {
	key resultKey
	qa  locomoQA
}

// validateUnifiedPromptPairRepeat validates complete journals before any score
// is printed. It compares the actual provider-facing answer user bytes, which
// include the rendered evidence and question, rather than attempting to infer
// parity from retrieval settings.
func validateUnifiedPromptPairRepeat(runDir string, opt options, convs []conversation, arms []string) (unifiedPromptPairValidationReceipt, error) {
	receipt := unifiedPromptPairValidationReceipt{
		Schema:                unifiedPromptPairValidationSchema,
		ValidatedAt:           time.Now().UTC(),
		Repeat:                opt.formalRunIndex,
		ConfiguredRepeats:     opt.repeats,
		TreatmentPromptDigest: evalTextDigest(unifiedAnswerContractPrompt),
		JudgePromptDigest:     evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode())),
		AnswerModel:           opt.answerModel,
		AnswerModelRevision:   modelRevisionMetadata(os.Getenv, "LOCOMO_MODEL_REVISION", opt.answerModel),
		AnswerProvider:        envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider),
		JudgeModel:            opt.judgeModel,
		JudgeModelRevision:    modelRevisionMetadata(os.Getenv, "JUDGE_MODEL_REVISION", opt.judgeModel),
		JudgeProvider:         resolveJudgeConfig(os.Getenv).Provider,
		DatasetFormat:         opt.datasetFormat,
		DatasetDigest:         opt.unifiedPairDatasetDigest,
		ContextParityMethod:   "sha256_of_actual_provider_answer_user_bytes",
		TopK:                  opt.topK,
		ChunkQuota:            opt.chunkQuota,
		Chunks:                opt.chunks,
		MaxTokens:             opt.maxTokens,
		Concurrency:           opt.concurrency,
		ThinkingDisabled:      benchNoThinking,
		ProviderAttemptPolicy: "one_provider_attempt_per_answer_and_judge_call",
		ArmSchedulingPolicy:   "concurrent_question_arm_goroutines_order_unspecified",
	}
	if !validUnifiedPromptPairDigest(receipt.DatasetDigest) {
		return receipt, fmt.Errorf("unified prompt paired experiment is missing the dataset digest captured at load time")
	}
	if len(arms) == 2 {
		receipt.ControlArm, receipt.TreatmentArm = arms[0], arms[1]
	}
	if err := validateUnifiedPromptPairExperiment(opt, arms); err != nil {
		return receipt, err
	}

	expected := make(map[resultKey]unifiedPromptPairExpectedQuestion)
	questionIDs := make(map[string]resultKey)
	controlPrompts := make(map[string]bool)
	for _, conv := range convs {
		for _, selected := range selectQuestions(conv, opt) {
			key := resultKey{Conv: conv.ID, Q: selected.Index}
			qa := selected.QA
			if qa.QuestionID == "" {
				qa.QuestionID = questionID(key.Conv, key.Q)
			}
			if _, duplicate := expected[key]; duplicate {
				return receipt, fmt.Errorf("duplicate expected question conv=%d q=%d", key.Conv, key.Q)
			}
			if previous, duplicate := questionIDs[qa.QuestionID]; duplicate {
				return receipt, fmt.Errorf("duplicate expected question ID %q at conv=%d q=%d and conv=%d q=%d", qa.QuestionID, previous.Conv, previous.Q, key.Conv, key.Q)
			}
			expected[key] = unifiedPromptPairExpectedQuestion{key: key, qa: qa}
			questionIDs[qa.QuestionID] = key
			controlOpt := optionsForRun(opt, arms[0], true)
			controlPrompts[evalTextDigest(answerSystemPromptForEval(qa, controlOpt))] = true
		}
	}
	if len(expected) == 0 {
		return receipt, fmt.Errorf("unified prompt paired experiment selected zero questions")
	}
	receipt.QuestionCount = len(expected)
	selectedIDs := make([]string, 0, len(questionIDs))
	for id := range questionIDs {
		selectedIDs = append(selectedIDs, id)
	}
	sort.Strings(selectedIDs)
	receipt.SelectedQuestionsDigest = evalJSONDigest(selectedIDs)
	for digest := range controlPrompts {
		receipt.ControlPromptDigests = append(receipt.ControlPromptDigests, digest)
	}
	sort.Strings(receipt.ControlPromptDigests)

	rows := make([]map[resultKey]result, 2)
	for i, arm := range arms {
		armOpt := optionsForRun(opt, arm, true)
		path := filepath.Join(runDir, fmt.Sprintf("results-%s.jsonl", arm))
		loaded, err := loadUnifiedPromptPairJournal(path, armOpt, expected)
		if err != nil {
			return receipt, fmt.Errorf("validate arm %s: %w", arm, err)
		}
		rows[i] = loaded
	}
	for key, expectedQuestion := range expected {
		control, treatment := rows[0][key], rows[1][key]
		if control.QuestionID != treatment.QuestionID || control.Question != treatment.Question ||
			control.Gold != treatment.Gold || control.Category != treatment.Category ||
			control.RetrievalFlags != treatment.RetrievalFlags {
			return receipt, fmt.Errorf("paired identity or retrieval mismatch for question %q", expectedQuestion.qa.QuestionID)
		}
		controlUser := control.UnifiedPairAudit.Answer[0].UserDigest
		treatmentUser := treatment.UnifiedPairAudit.Answer[0].UserDigest
		if controlUser != treatmentUser {
			return receipt, fmt.Errorf("answer user context digest mismatch for question %q: control=%s treatment=%s", expectedQuestion.qa.QuestionID, controlUser, treatmentUser)
		}
	}
	receipt.Valid = true
	return receipt, nil
}

func loadUnifiedPromptPairJournal(path string, opt options, expected map[resultKey]unifiedPromptPairExpectedQuestion) (map[resultKey]result, error) {
	rows := make(map[resultKey]result, len(expected))
	questionIDs := make(map[string]resultKey, len(expected))
	err := scanResultsJSONLStrict(path, func(item result) error {
		key := resultKey{Conv: item.Conv, Q: item.Q}
		want, ok := expected[key]
		if !ok {
			return fmt.Errorf("unexpected row conv=%d q=%d", key.Conv, key.Q)
		}
		if _, duplicate := rows[key]; duplicate {
			return fmt.Errorf("duplicate row conv=%d q=%d", key.Conv, key.Q)
		}
		id := resultID(item)
		if id != want.qa.QuestionID {
			return fmt.Errorf("question ID mismatch conv=%d q=%d: got %q want %q", key.Conv, key.Q, id, want.qa.QuestionID)
		}
		if previous, duplicate := questionIDs[id]; duplicate {
			return fmt.Errorf("duplicate question ID %q at conv=%d q=%d and conv=%d q=%d", id, previous.Conv, previous.Q, key.Conv, key.Q)
		}
		if item.Question != want.qa.Question || item.Gold != goldFor(want.qa) || item.Category != want.qa.Category {
			return fmt.Errorf("question payload mismatch for %q", id)
		}
		if item.RetrievalFlags != retrievalFingerprint(opt) || item.AnswerRegime != answerRegimeFingerprint(opt) {
			return fmt.Errorf("configuration fingerprint mismatch for %q", id)
		}
		if err := validateUnifiedPromptPairQuestionAudit(item, want.qa, opt); err != nil {
			return fmt.Errorf("question %q: %w", id, err)
		}
		rows[key] = item
		questionIDs[id] = key
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(expected) {
		return nil, fmt.Errorf("missing expected rows: got %d want %d", len(rows), len(expected))
	}
	return rows, nil
}

func validateUnifiedPromptPairQuestionAudit(item result, qa locomoQA, opt options) error {
	audit := item.UnifiedPairAudit
	if audit == nil || audit.Schema != unifiedPromptPairAuditSchema {
		return fmt.Errorf("missing or invalid provider call audit")
	}
	if len(audit.Answer) != 1 {
		return fmt.Errorf("answer calls=%d, want exactly 1", len(audit.Answer))
	}
	if len(audit.Judge) != 1 {
		return fmt.Errorf("judge calls=%d, want exactly 1", len(audit.Judge))
	}
	for role, attempt := range map[string]unifiedPromptPairCallAudit{"answer": audit.Answer[0], "judge": audit.Judge[0]} {
		if !attempt.Success || attempt.Status != unifiedPromptPairCallOK {
			return fmt.Errorf("%s call status=%q success=%t", role, attempt.Status, attempt.Success)
		}
		for field, digest := range map[string]string{"system": attempt.SystemDigest, "user": attempt.UserDigest, "output": attempt.OutputDigest} {
			if !validUnifiedPromptPairDigest(digest) {
				return fmt.Errorf("%s %s digest is invalid", role, field)
			}
		}
	}
	wantAnswerSystem := evalTextDigest(answerSystemPromptForEval(qa, opt))
	if audit.Answer[0].SystemDigest != wantAnswerSystem {
		return fmt.Errorf("answer system digest mismatch: got %s want %s", audit.Answer[0].SystemDigest, wantAnswerSystem)
	}
	wantJudgeSystem := evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode()))
	if audit.Judge[0].SystemDigest != wantJudgeSystem {
		return fmt.Errorf("judge system digest mismatch: got %s want %s", audit.Judge[0].SystemDigest, wantJudgeSystem)
	}
	if audit.Answer[0].OutputDigest != evalTextDigest(item.Predicted) {
		return fmt.Errorf("answer output digest does not bind predicted result")
	}
	wantJudgeUser := evalTextDigest(buildJudgePrompt(item.Question, item.Gold, item.Predicted))
	if audit.Judge[0].UserDigest != wantJudgeUser {
		return fmt.Errorf("judge user digest does not bind question/gold/predicted result")
	}
	if audit.Judge[0].JudgeCorrect == nil || *audit.Judge[0].JudgeCorrect != item.Correct {
		return fmt.Errorf("strict judge verdict does not bind scored correctness")
	}
	return nil
}

func validUnifiedPromptPairDigest(value string) bool {
	const prefix = "sha256:"
	hex := strings.TrimPrefix(value, prefix)
	return strings.HasPrefix(value, prefix) && len(hex) == 64 && isLowerHex(hex)
}

func writeUnifiedPromptPairValidationReceipt(runDir string, receipt unifiedPromptPairValidationReceipt, validationErr error) error {
	if validationErr != nil {
		receipt.Valid = false
		receipt.Failure = validationErr.Error()
	}
	return writeJSON(filepath.Join(runDir, "unified-pair-validation.json"), receipt)
}

// requireFreshUnifiedPromptPairRunDir deliberately disables journal resume for
// the score-bearing prompt-only experiment. Ordinary resume fingerprints do not
// bind every causal input (store bytes, cohort, generation configuration), so a
// partial old journal cannot be made promotion-safe merely by matching flags.
func requireFreshUnifiedPromptPairRunDir(runDir string) error {
	patterns := []string{
		filepath.Join(runDir, "results-*.jsonl"),
		filepath.Join(runDir, "run-*", "results-*.jsonl"),
		filepath.Join(runDir, "paired.json"),
		filepath.Join(runDir, "stats*.json"),
		filepath.Join(runDir, "cost.json"),
		filepath.Join(runDir, "unified-pair-validation.json"),
		filepath.Join(runDir, "run-*", "unified-pair-validation.json"),
		filepath.Join(runDir, "regime.json"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("inspect unified prompt paired run directory: %w", err)
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return fmt.Errorf("unified prompt paired experiment refuses journal resume from %s; use a fresh --run-dir", matches[0])
		}
	}
	return nil
}
