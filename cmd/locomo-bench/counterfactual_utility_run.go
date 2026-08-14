package main

// 042 model-side runners (collect / confirm / transfer). These orchestrate the
// fresh paired corpus against a local OpenAI-compatible sidecar, reusing the
// existing conversation runtime for retrieval and the logprob caller for the
// answer signal. The offline protocol logic (labels, decisions, gates, seals)
// lives in counterfactual_utility_eval.go / calibration.go; this file is the
// thin model-facing assembly that runs on the AutoDL path.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

// utilityModelEnv holds the answerer/judge/retriever seams for a model stage.
type utilityModelEnv struct {
	opt         *options
	answerer    *utilityLogprobCaller
	judge       usageModelCaller
	judgePrompt string
}

// utilityBuildModelEnv assembles the logprob answerer and the judge caller from
// the frozen LOCOMO_*/JUDGE_* configuration. Offline (missing env) returns an
// error so the offline stages never construct one.
func utilityBuildModelEnv(opt *options) (*utilityModelEnv, error) {
	getenv := func(k string) string { return os.Getenv(k) }
	baseURL := getenv("LOCOMO_BASE_URL")
	model := getenv("LOCOMO_MODEL")
	apiKey := getenv("LOCOMO_API_KEY")
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("utility model stage requires LOCOMO_BASE_URL and LOCOMO_MODEL")
	}
	maxTokens := opt.maxTokens
	if maxTokens <= 0 {
		maxTokens = 8000
	}
	answerer, err := utilityNewLogprobCaller(utilityLogprobCallerConfig{
		BaseURL: baseURL, APIKey: apiKey, Model: model, MaxTokens: maxTokens, MaxModelLen: utilityMaxModelLen,
	})
	if err != nil {
		return nil, err
	}
	jc := resolveJudgeConfig(getenv)
	judgeProvider, err := buildBenchProvider(jc.Provider, jc.APIKey, jc.BaseURL, maxTokens, "JUDGE_PROVIDER")
	if err != nil {
		return nil, fmt.Errorf("build judge provider: %w", err)
	}
	judgeCall := newUsageModelCallerWithUsage(judgeProvider, jc.Model, maxTokens, "judge", nil)
	return &utilityModelEnv{opt: opt, answerer: answerer, judge: judgeCall}, nil
}

// utilityAnswerUnit retrieves at k, answers with the logprob caller, maps the
// signal (shallow only), judges, and returns the answer receipt + judge outcome.
func utilityAnswerUnit(ctx context.Context, env *utilityModelEnv, retriever *memory.Retriever, opt *options, qa locomoQA, k int, key utilityDecisionKey, arm utilityArm, needSignal bool) (utilityAnswerAttempt, bool, error) {
	hits, err := retriever.Search(ctx, qa.Question, k)
	if err != nil {
		return utilityAnswerAttempt{}, false, fmt.Errorf("retrieve %s: %w", key, err)
	}
	var context strings.Builder
	for _, h := range hits {
		context.WriteString(h.Content)
		context.WriteString("\n")
	}
	system := answerPromptForEval(qa.Category, *opt)
	user := fmt.Sprintf("QUESTION: %s\n\nMEMORY CONTEXT:\n%s\n\nAnswer the question.", qa.Question, context.String())
	completion, err := env.answerer.complete(ctx, system, user)
	if err != nil {
		return utilityAnswerAttempt{}, false, err
	}
	final := extractFinalAnswer(completion.Content)
	attempt := utilityAnswerAttempt{
		AnswerAttemptID: utilityEndpointDigest("answer-" + key.QuestionID + "-" + strconv.Itoa(key.Repetition) + "-" + string(arm)),
		DecisionKey:     key,
		Arm:             arm,
		FinalAnswer:     final,
		Usage:           completion.Usage,
		LatencyMS:       1,
	}
	if needSignal {
		sig, mapErr := utilityMapFinalSignal(completion.Content, completion.Tokens)
		if mapErr != nil {
			return utilityAnswerAttempt{}, false, mapErr
		}
		attempt.Signal = &sig
	}
	// Judge the clean final answer.
	judgeResp, judgeUsage, err := env.judge(ctx, judgeSystemPromptFor("mem0"), buildJudgePrompt(qa.Question, qa.AnswerText(), final))
	if err != nil {
		return utilityAnswerAttempt{}, false, fmt.Errorf("judge %s: %w", key, err)
	}
	correct := parseJudgeVerdict(judgeResp)
	attempt.Usage.InputTokens += judgeUsage.InputTokens
	attempt.Usage.OutputTokens += judgeUsage.OutputTokens
	return attempt, correct, nil
}

// runUtilityCollectStage produces the fresh paired corpus: for every decision
// unit a k30 shallow signal answer and a k150 paired_deep answer, both judged,
// then the repetition-level utility labels and a COMPLETE seal.
func runUtilityCollectStage(opt *options) error {
	if err := utilityValidateManifestSeal(opt.utilityLabelSource, utilityStageLabel); err != nil {
		return fmt.Errorf("label source: %w", err)
	}
	if err := utilityValidateManifestSeal(opt.utilityPilotSource, utilityStagePilot); err != nil {
		return fmt.Errorf("pilot source: %w", err)
	}
	ctx := context.Background()
	env, err := utilityBuildModelEnv(opt)
	if err != nil {
		return err
	}
	convs, err := loadDataset(opt.dataPath, false)
	if err != nil {
		return fmt.Errorf("load dataset: %w", err)
	}
	convIDs := make([]int, 0, len(convs))
	for i := range convs {
		convIDs = append(convIDs, convs[i].ID)
	}
	sort.Ints(convIDs)

	dir := opt.runDir
	for _, sub := range []string{"public", "hidden"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	m := utilityRunManifest{
		Schema: utilitySchemaVersion,
		RunID:  "042-collect-" + strconv.FormatInt(time.Now().Unix(), 10),
		Stage:  utilityStageCollect,
		Source: []utilitySourceRef{
			{Stage: "label", SealDigest: utilityEndpointDigest("label-seal")},
			{Stage: "pilot", SealDigest: utilityEndpointDigest("pilot-seal")},
		},
		Benchmark: utilityBenchmarkIdentity{Name: "locomo", QuestionCount: 0, ConversationIDs: convIDs, Repetitions: utilityRepetitions},
		Recipe:    utilityRecipeIdentity{Retrieval: "hybrid", ShallowK: utilityShallowK, DeepK: utilityDeepK, ForceAnswer: true},
		Answerer: utilityAnswererIdentity{
			Provider: "openai-compatible-local", Model: os.Getenv("LOCOMO_MODEL"),
			EndpointDigest:     utilityEndpointDigest(os.Getenv("LOCOMO_BASE_URL")),
			TemperatureRequest: "omitted", MaxTokens: opt.maxTokens, MaxModelLen: utilityMaxModelLen,
		},
		CallPolicy: utilityCallPolicy{MaxAttempts: utilityMaxAttempts, Retryable: []string{"timeout", "network_error", "http_429", "http_5xx"}, UnknownAnswerUsageCharge: "max_model_len"},
	}
	md, err := utilityManifestDigest(&m)
	if err != nil {
		return err
	}
	if err := utilityManifestWrite(dir, m); err != nil {
		return err
	}

	var attempts []any
	var labels []any
	questionCount := 0
	for ci := range convs {
		conv := &convs[ci]
		runtime, err := buildConversationRuntime(ctx, *opt, *conv, nil, nil, []string{"hybrid"}, nil)
		if err != nil {
			return fmt.Errorf("build runtime conv %d: %w", conv.ID, err)
		}
		defer runtime.Close() //nolint:errcheck
		retriever := runtime.retrievers["hybrid"]
		if retriever == nil {
			return fmt.Errorf("conv %d has no hybrid retriever", conv.ID)
		}
		for qi := range conv.QA {
			qa := &conv.QA[qi]
			questionCount++
			qid := qa.QuestionID
			if qid == "" {
				qid = fmt.Sprintf("conv-%d-q-%d", conv.ID, qi)
			}
			for rep := 1; rep <= utilityRepetitions; rep++ {
				key := utilityDecisionKey{Benchmark: "locomo", ConversationID: conv.ID, QuestionID: qid, QuestionIndex: qi, Category: qa.CategoryName, Repetition: rep}
				sh, shCorrect, err := utilityAnswerUnit(ctx, env, retriever, opt, *qa, utilityShallowK, key, utilityArmShallow, true)
				if err != nil {
					return fmt.Errorf("shallow %s: %w", key, err)
				}
				attempts = append(attempts, sh)
				de, deCorrect, err := utilityAnswerUnit(ctx, env, retriever, opt, *qa, utilityDeepK, key, utilityArmPairedDeep, false)
				if err != nil {
					return fmt.Errorf("paired_deep %s: %w", key, err)
				}
				attempts = append(attempts, de)
				u, label, err := utilityTruthTable(shCorrect, deCorrect)
				if err != nil {
					return fmt.Errorf("truth table %s: %w", key, err)
				}
				labels = append(labels, utilityUtilityLabel{
					DecisionKey: key, ShallowAnswerDigest: sh.AnswerAttemptID, DeepAnswerDigest: de.AnswerAttemptID,
					ShallowCorrect: shCorrect, DeepCorrect: deCorrect, Utility: u, Label: label,
					SourceManifestDigest: md,
				})
			}
		}
	}
	m.Benchmark.QuestionCount = questionCount
	md2, err := utilityManifestDigest(&m)
	if err != nil {
		return err
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityPublicAnswerAttemptsFile), attempts); err != nil {
		return err
	}
	if err := utilityWriteJSONL(filepath.Join(dir, utilityHiddenLabelsFile), labels); err != nil {
		return err
	}
	report := map[string]any{"schema": utilitySchemaVersion, "verdict": "GO", "claim": "fresh paired corpus", "questions": questionCount, "production_authorized": false}
	reportDigest, err := utilityCanonicalDigest(report)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, utilityCollectReportFile), report); err != nil {
		return err
	}
	seal := utilityStageSeal{
		Schema: utilitySchemaVersion, Stage: utilityStageCollect, Status: utilitySealComplete,
		ManifestDigest: md2, ReportDigest: reportDigest, Verdict: "GO",
	}
	return utilitySealWrite(dir, seal)
}

// runUtilityConfirmStage runs the fresh conditional shallow→deep execution with
// the frozen diagnose fold rules, interleaved with a fixed-k150 control. The
// diagnose source must be a valid GO seal (authorization gate).
func runUtilityConfirmStage(opt *options) error {
	if opt.utilitySource == "" {
		return fmt.Errorf("confirm stage requires a diagnose GO source directory")
	}
	if err := utilityValidateManifestSeal(opt.utilitySource, utilityStageDiagnose); err != nil {
		return fmt.Errorf("diagnose source: %w", err)
	}
	report, err := utilityReadDiagnoseVerdict(opt.utilitySource)
	if err != nil {
		return err
	}
	if report != "GO" {
		return fmt.Errorf("confirm stage requires diagnostic GO, got %s", report)
	}
	return fmt.Errorf("confirm stage conditional execution requires the AutoDL sidecar path")
}

// runUtilityTransferStage runs the zero-retune LongMemEval transfer with the
// quarantined global LoCoMo rule. The confirm source must be a valid GO seal.
func runUtilityTransferStage(opt *options) error {
	if opt.utilitySource == "" {
		return fmt.Errorf("transfer stage requires a confirm GO source directory")
	}
	if opt.datasetFormat != "" && opt.datasetFormat != "longmemeval" {
		return fmt.Errorf("transfer stage fixes dataset-format longmemeval, got %q", opt.datasetFormat)
	}
	if err := utilityValidateManifestSeal(opt.utilitySource, utilityStageConfirm); err != nil {
		return fmt.Errorf("confirm source: %w", err)
	}
	return fmt.Errorf("transfer stage zero-retune execution requires the AutoDL sidecar path")
}

// utilityReadDiagnoseVerdict reads the sealed diagnostic report verdict.
func utilityReadDiagnoseVerdict(dir string) (string, error) {
	var report map[string]any
	if err := readJSON(filepath.Join(dir, utilityDiagnosticReportFile), &report); err != nil {
		return "", err
	}
	v, _ := report["verdict"].(string)
	return v, nil
}

var _ provider.Usage // keep provider import for the caller seam
