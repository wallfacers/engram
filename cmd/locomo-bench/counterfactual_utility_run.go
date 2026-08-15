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
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
)

// utilityRuntimeSeam builds the logger, hybrid embedding client, and a
// fail-closed extraction guard shared by the 042 model-side stages. The frozen
// recipe reuses a pre-built store, so extraction must never be re-paid; an
// empty store (missing facts) must fail closed rather than extract ad-hoc.
func utilityRuntimeSeam() (*slog.Logger, embedding.Client, pipeline.ModelCaller) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	embClient := buildBenchEmbeddingClient(logger, nil)
	extractNever := func(context.Context, string, string) (string, error) {
		return "", fmt.Errorf("042 stage must not call extraction (store must be pre-built)")
	}
	return logger, embClient, extractNever
}

// utilityRunPairedUnits answers every (conversation, question, repetition) unit
// through a worker pool sized to opt.concurrency, returning the public shallow/
// paired-deep answer attempts and the hidden utility labels. The engine
// retriever is concurrency-safe (vecMu guards the vector memo) and the logprob
// answerer / judge are stateless HTTP callers, so per-question parallelism is
// safe and uses the answer sidecar's batch slots fully. The first error is
// returned and the remaining in-flight calls are cancelled via the derived
// context.
func utilityRunPairedUnits(ctx context.Context, env *utilityModelEnv, opt *options, runtimes map[int]*conversationRuntime, convs []conversation, sourceMD string) (attempts []utilityAnswerAttempt, labels []utilityUtilityLabel, err error) {
	type job struct {
		convID int
		qi     int
		qid    string
		qa     locomoQA
		rep    int
	}
	var jobs []job
	for ci := range convs {
		conv := &convs[ci]
		for qi := range conv.QA {
			qa := conv.QA[qi]
			qid := qa.QuestionID
			if qid == "" {
				qid = fmt.Sprintf("conv-%d-q-%d", conv.ID, qi)
			}
			for rep := 1; rep <= utilityRepetitions; rep++ {
				jobs = append(jobs, job{convID: conv.ID, qi: qi, qid: qid, qa: qa, rep: rep})
			}
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	n := opt.concurrency
	if n < 1 {
		n = 1
	}
	if n > len(jobs) {
		n = len(jobs)
	}
	sem := make(chan struct{}, n)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-workCtx.Done():
				return
			}
			defer func() { <-sem }()
			retriever := runtimes[j.convID].retrievers["hybrid"]
			if retriever == nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("conv %d has no hybrid retriever", j.convID)
					cancel()
				}
				mu.Unlock()
				return
			}
			key := utilityDecisionKey{Benchmark: "locomo", ConversationID: j.convID, QuestionID: j.qid, QuestionIndex: j.qi, Category: j.qa.CategoryName, Repetition: j.rep}
			sh, shCorrect, err := utilityAnswerUnit(workCtx, env, retriever, opt, j.qa, utilityShallowK, key, utilityArmShallow, true)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("shallow %s: %w", key, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			de, deCorrect, err := utilityAnswerUnit(workCtx, env, retriever, opt, j.qa, utilityDeepK, key, utilityArmPairedDeep, false)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("paired_deep %s: %w", key, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			u, label, err := utilityTruthTable(shCorrect, deCorrect)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("truth table %s: %w", key, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			attempts = append(attempts, sh, de)
			labels = append(labels, utilityUtilityLabel{
				DecisionKey: key, ShallowAnswerDigest: sh.AnswerAttemptID, DeepAnswerDigest: de.AnswerAttemptID,
				ShallowCorrect: shCorrect, DeepCorrect: deCorrect, Utility: u, Label: label,
				SourceManifestDigest: sourceMD,
			})
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return attempts, labels, firstErr
}

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
// utilityValidateCollectSources enforces the collect authorization gates:
// the label constructor-audit GO seal and the pilot GO kill-gate (SC-012).
func utilityValidateCollectSources(labelDir, pilotDir string) error {
	if err := utilityValidateManifestSeal(labelDir, utilityStageLabel); err != nil {
		return fmt.Errorf("label source: %w", err)
	}
	if err := utilityValidateManifestSeal(pilotDir, utilityStagePilot); err != nil {
		return fmt.Errorf("pilot source: %w", err)
	}
	pilotVerdict, err := utilityReadPilotVerdict(pilotDir)
	if err != nil {
		return fmt.Errorf("pilot verdict: %w", err)
	}
	if pilotVerdict != "GO" {
		return fmt.Errorf("collect requires pilot GO, got %s (SC-012 kill-gate)", pilotVerdict)
	}
	return nil
}

func runUtilityCollectStage(opt *options) error {
	if err := utilityValidateCollectSources(opt.utilityLabelSource, opt.utilityPilotSource); err != nil {
		return err
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
	questionCount := 0
	for i := range convs {
		convIDs = append(convIDs, convs[i].ID)
		questionCount += len(convs[i].QA)
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
		Benchmark: utilityBenchmarkIdentity{Name: "locomo", QuestionCount: questionCount, ConversationIDs: convIDs, Repetitions: utilityRepetitions},
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

	// Build every conversation runtime up front so the answer worker pool can
	// use them concurrently; the manifest digest already covers the full
	// question count, so the seal stays consistent with the on-disk manifest.
	logger, embClient, extractNever := utilityRuntimeSeam()
	runtimes := make([]*conversationRuntime, 0, len(convs))
	rtMap := map[int]*conversationRuntime{}
	for ci := range convs {
		conv := &convs[ci]
		runtime, err := buildConversationRuntime(ctx, *opt, *conv, extractNever, embClient, []string{"hybrid"}, logger)
		if err != nil {
			return fmt.Errorf("build runtime conv %d: %w", conv.ID, err)
		}
		runtimes = append(runtimes, runtime)
		rtMap[conv.ID] = runtime
	}
	defer func() {
		for _, rt := range runtimes {
			rt.Close()
		}
	}()

	attempts, labels, err := utilityRunPairedUnits(ctx, env, opt, rtMap, convs, md)
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
		ManifestDigest: md, ReportDigest: reportDigest, Verdict: "GO",
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

// utilityReadPilotVerdict reads the sealed pilot report verdict (SC-012 gate).
func utilityReadPilotVerdict(dir string) (string, error) {
	var receipt utilityPilotReceipt
	if err := readJSON(filepath.Join(dir, utilityPilotReportFile), &receipt); err != nil {
		return "", err
	}
	return receipt.Verdict, nil
}

var _ provider.Usage // keep provider import for the caller seam
