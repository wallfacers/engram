package main

// 043 confidence-gated deepening — 2-conv dual-signal quality pilot (T010-T013).
//
// Stage shape mirrors runUtilityPilotStage: frozen manifest → pre-built
// conversation runtimes → worker-pool answering over the first two
// conversations → dual-signal AUC + channel-parity gate → GO/NO-GO seal. The
// pilot answers every question on BOTH channels with byte-identical prompts
// (system = answerSystemPromptForEval under the unified contract, user =
// buildAnswerContextPrompt over retrieveWithQuota hits — the exact 87.9%
// anchor recipe):
//
//   - logprob channel: utilityLogprobCaller (non-streaming, logprobs=true,
//     thinking-on) — source of the three frozen final-span features;
//   - streaming channel: the ordinary bench caller — source of the textual
//     hesitation lexicon signal.
//
// Correctness labels come from the standard eval judge on the logprob channel's
// clean final answer (fresh, self-contained — no cross-run label reuse). The
// 042 k30/k150 join (「k30 错 k150 对」positive class) is an offline analysis on
// the pulled artifacts, not a stage input. The gate freezes threshold/feature
// into pilot-report.json; the mechanism run reads them read-only (FR-005).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// deepenPilotChannels is the fixed dual-channel measurement set.
const (
	deepenPilotRep    = 1 // single repetition: the pilot gates signal quality, not score
	deepenAUCSeed     = 43
	deepenAUCBootIter = 1000
	deepenPilotSignals = 4 // 3 logprob features + textual hesitation
)

// deepenPilotAttempt is one question's dual-channel measurement record
// (public/answer-attempts.jsonl).
type deepenPilotAttempt struct {
	DecisionKey     string   `json:"decision_key"`
	ConvID          int      `json:"conv_id"`
	QuestionID      string   `json:"question_id"`
	Category        string   `json:"category"`
	FinalStream     string   `json:"final_stream"`
	FinalLogprob    string   `json:"final_logprob"`
	Flip            bool     `json:"flip"`
	LogprobStatus   string   `json:"logprob_status"`
	LogprobReason   string   `json:"logprob_reason,omitempty"`
	Features        []float64 `json:"features,omitempty"`
	TextualHesitant bool     `json:"textual_hesitant"`
	JudgeCorrect    *bool    `json:"judge_correct,omitempty"`
	UsageIn         int      `json:"usage_in"`
	UsageOut        int      `json:"usage_out"`
}

// deepenHesitationScores converts one attempt's raw signals into
// hesitation-oriented scores (higher = more hesitant) indexed 0..3:
// the three negated logprob features (lower logprob = less confident) and the
// binary textual signal. Unavailable logprob signals score 0 with coverage
// tracked separately by the report.
func deepenHesitationScores(features []float64, available bool, textualHesitant bool) [deepenPilotSignals]float64 {
	var scores [deepenPilotSignals]float64
	for i := 0; i < 3; i++ {
		if available && i < len(features) {
			scores[i] = -features[i]
		}
	}
	if textualHesitant {
		scores[3] = 1
	}
	return scores
}

// signalKindFor / signalFeatureFor map the fixed signal index to its report
// identity. The index order IS the deterministic best-signal tie-break.
func signalKindFor(idx int) string {
	if idx < 3 {
		return string(deepenSignalLogprob)
	}
	return string(deepenSignalTextual)
}

func signalFeatureFor(idx int) string {
	if idx < 3 {
		return utilityRoutingFeatureNames[idx]
	}
	return "textual_hesitation"
}

// runDeepenSignalPilotStage is the --deepen-pilot signal stage entry.
func runDeepenSignalPilotStage(opt *options) error {
	// Fail-closed environment gates (analyze F1): the 87.9% anchor recipe is
	// thinking-ON, so the streaming bench channel must have thinking enabled.
	if benchNoThinking {
		return fmt.Errorf("deepen pilot requires the anchor thinking-ON regime: set LOCOMO_NO_THINKING=0 (streaming bench channel currently disables thinking)")
	}
	if !opt.unifiedAnswerContract {
		return fmt.Errorf("deepen pilot requires --unified-answer-contract (the pilot measures the anchor unified recipe)")
	}
	ctx := context.Background()
	// Deepen pilot env: the logprob answerer pins temperature=0 (matching the
	// streaming bench channel) — the 042 builder omits temperature, which
	// measured as a 95% channel flip rate in pilot run 1 (probe 2026-08-15).
	maxTokens := opt.maxTokens
	if maxTokens <= 0 {
		maxTokens = 8000
	}
	zeroTemp := 0.0
	answerer, err := utilityNewLogprobCaller(utilityLogprobCallerConfig{
		BaseURL:     os.Getenv("LOCOMO_BASE_URL"),
		APIKey:      os.Getenv("LOCOMO_API_KEY"),
		Model:       os.Getenv("LOCOMO_MODEL"),
		MaxTokens:   maxTokens,
		MaxModelLen: utilityMaxModelLen,
		Temperature: &zeroTemp,
	})
	if err != nil {
		return fmt.Errorf("deepen pilot logprob answerer: %w", err)
	}
	jc := resolveJudgeConfig(func(k string) string { return os.Getenv(k) })
	judgeProvider, err := buildBenchProvider(jc.Provider, jc.APIKey, jc.BaseURL, maxTokens, "JUDGE_PROVIDER")
	if err != nil {
		return fmt.Errorf("deepen pilot judge provider: %w", err)
	}
	env := &utilityModelEnv{
		opt:      opt,
		answerer: answerer,
		judge:    newUsageModelCallerWithUsage(judgeProvider, jc.Model, maxTokens, "judge", nil),
	}
	streamProvider, err := buildBenchProvider("openai", os.Getenv("LOCOMO_API_KEY"), os.Getenv("LOCOMO_BASE_URL"), maxTokens, "LOCOMO")
	if err != nil {
		return fmt.Errorf("deepen pilot streaming provider: %w", err)
	}
	streamCall := newUsageModelCallerWithUsage(streamProvider, os.Getenv("LOCOMO_MODEL"), maxTokens, "answer", nil)

	convs, err := loadDataset(opt.dataPath, false)
	if err != nil {
		return fmt.Errorf("load dataset: %w", err)
	}
	if len(convs) < 2 {
		return fmt.Errorf("deepen pilot requires at least two conversations, got %d", len(convs))
	}
	pilotConvs := convs[:2]
	convIDs := []int{pilotConvs[0].ID, pilotConvs[1].ID}
	sort.Ints(convIDs)

	dir := opt.runDir
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		return err
	}
	questionCount := 0
	for ci := range pilotConvs {
		questionCount += len(pilotConvs[ci].QA)
	}
	manifest := deepenRunManifest{
		Schema:         deepenSchemaVersion,
		RunID:          fmt.Sprintf("deepen-pilot-%d", time.Now().UTC().Unix()),
		Stage:          "pilot",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		QuestionCount:  questionCount,
		Arm:            "signal-pilot",
		ContractDigest: formalAnswerPromptDigest(*opt),
		DatasetDigest:  utilityEndpointDigest(opt.dataPath),
		DeepenK:        opt.deepenK,
		MaxGaps:        opt.deepenMaxGaps,
	}
	md, err := deepenManifestDigest(&manifest)
	if err != nil {
		return err
	}
	if err := deepenManifestWrite(dir, manifest); err != nil {
		return err
	}

	logger, embClient, extractNever := utilityRuntimeSeam()
	rtMap := map[int]*conversationRuntime{}
	var runtimes []*conversationRuntime
	for ci := range pilotConvs {
		conv := pilotConvs[ci]
		runtime, err := buildConversationRuntime(ctx, *opt, conv, extractNever, embClient, []string{"hybrid"}, logger)
		if err != nil {
			return fmt.Errorf("build runtime conv %d: %w", conv.ID, err)
		}
		rtMap[conv.ID] = runtime
		runtimes = append(runtimes, runtime)
	}
	defer func() {
		for _, rt := range runtimes {
			rt.Close()
		}
	}()

	attempts, err := deepenRunPilotUnits(ctx, env, streamCall, opt, rtMap, pilotConvs)
	if err != nil {
		return err
	}
	if err := utilityWriteJSONL(filepath.Join(dir, deepenAnswerAttemptsFile), attempts); err != nil {
		return err
	}

	report, err := deepenBuildPilotReport(attempts, convIDs)
	if err != nil {
		return err
	}
	reportDigest, err := deepenPilotReportWrite(dir, report)
	if err != nil {
		return err
	}
	if err := deepenSealWrite(dir, deepenStageSeal{
		Schema:         deepenSchemaVersion,
		Stage:          "pilot",
		Status:         deepenSealComplete,
		ManifestDigest: md,
		ReportDigest:   reportDigest,
		Verdict:        report.Gate.Verdict,
	}); err != nil {
		return err
	}
	fmt.Printf("deepen pilot: verdict=%s best_auc= see report chosen=%s/%s threshold=%s questions=%d flip_rate=%.3f\n",
		report.Gate.Verdict, report.Chosen.Kind, report.Chosen.Feature,
		strconv.FormatFloat(report.Chosen.Threshold, 'f', -1, 64), len(attempts), report.ChannelParity.FlipRate)
	fmt.Printf("gate reason: %s\n", report.Gate.Reason)
	return nil
}

// deepenRunPilotUnits answers every question of the two pilot conversations on
// both channels through a worker pool (hard rule: model-side stages must
// parallelize), judging the logprob channel's clean final answer. Output order
// follows the dataset order (slot-indexed writes).
func deepenRunPilotUnits(ctx context.Context, env *utilityModelEnv, streamCall usageModelCaller, opt *options, runtimes map[int]*conversationRuntime, convs []conversation) ([]deepenPilotAttempt, error) {
	type job struct {
		convID int
		qi     int
		qa     locomoQA
	}
	var jobs []job
	for ci := range convs {
		conv := &convs[ci]
		for qi := range conv.QA {
			jobs = append(jobs, job{convID: conv.ID, qi: qi, qa: conv.QA[qi]})
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
	attempts := make([]deepenPilotAttempt, len(jobs))
	var wg sync.WaitGroup
	for ji := range jobs {
		j := jobs[ji]
		wg.Add(1)
		go func(slot int, j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-workCtx.Done():
				return
			}
			defer func() { <-sem }()
			attempt, err := deepenPilotUnit(workCtx, env, streamCall, opt, runtimes[j.convID], j.convID, j.qi, j.qa)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("conv %d q%d: %w", j.convID, j.qi, err)
					cancel()
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			attempts[slot] = attempt
			mu.Unlock()
		}(ji, j)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	for i := range attempts {
		if attempts[i].DecisionKey == "" {
			return nil, fmt.Errorf("pilot slot %d unfilled (cancelled)", i)
		}
	}
	return attempts, nil
}

// deepenPilotUnit runs one question through the anchor retrieval + both answer
// channels + the judge.
func deepenPilotUnit(ctx context.Context, env *utilityModelEnv, streamCall usageModelCaller, opt *options, runtime *conversationRuntime, convID, qi int, qa locomoQA) (deepenPilotAttempt, error) {
	retriever := runtime.retrievers["hybrid"]
	if retriever == nil {
		return deepenPilotAttempt{}, fmt.Errorf("no hybrid retriever")
	}
	topK, quota := opt.retrievalFor(qa.Category)
	hits, err := retrieveWithQuota(ctx, retriever, qa.Question, topK, quota)
	if err != nil {
		return deepenPilotAttempt{}, fmt.Errorf("retrieve: %w", err)
	}
	system := answerSystemPromptForEval(qa, *opt)
	user := buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)

	attempt := deepenPilotAttempt{
		DecisionKey: fmt.Sprintf("locomo/conv-%d/q-%d/%d", convID, qi, deepenPilotRep),
		ConvID:      convID,
		QuestionID:  fmt.Sprintf("q-%d", qi),
		Category:    qa.CategoryName,
	}

	// Logprob channel (non-streaming, thinking-on).
	completion, err := env.answerer.complete(ctx, system, user)
	if err != nil {
		return deepenPilotAttempt{}, fmt.Errorf("logprob channel: %w", err)
	}
	attempt.FinalLogprob = extractFinalAnswer(completion.Content)
	attempt.UsageIn += completion.Usage.InputTokens
	attempt.UsageOut += completion.Usage.OutputTokens
	// Robust final-span mapping (043 fix): inline thinking + trailing special
	// tokens break the 042 strict content-suffix precondition on this stack.
	feats, available, reason := deepenFinalSpanSignal(completion.Tokens)
	if available {
		attempt.LogprobStatus = "available"
		attempt.Features = feats
	} else {
		attempt.LogprobStatus = "unavailable"
		attempt.LogprobReason = reason
	}

	// Streaming channel (bench caller, same prompt bytes).
	streamResp, usage, err := streamCall(ctx, system, user)
	if err != nil {
		return deepenPilotAttempt{}, fmt.Errorf("streaming channel: %w", err)
	}
	attempt.FinalStream = extractFinalAnswer(streamResp)
	attempt.UsageIn += usage.InputTokens
	attempt.UsageOut += usage.OutputTokens

	attempt.Flip = strings.TrimSpace(attempt.FinalStream) != strings.TrimSpace(attempt.FinalLogprob)
	attempt.TextualHesitant = textualHesitation(attempt.FinalStream)

	// Judge the logprob channel's clean final answer (fresh labels).
	judgeResp, judgeUsage, err := env.judge(ctx, judgeSystemPromptFor("mem0"), buildJudgePrompt(qa.Question, qa.AnswerText(), attempt.FinalLogprob))
	if err != nil {
		return deepenPilotAttempt{}, fmt.Errorf("judge: %w", err)
	}
	correct := parseJudgeVerdict(judgeResp)
	attempt.JudgeCorrect = &correct
	attempt.UsageIn += judgeUsage.InputTokens
	attempt.UsageOut += judgeUsage.OutputTokens
	return attempt, nil
}

// deepenBuildPilotReport computes the dual-signal AUCs, channel parity, the
// chosen signal + ROC threshold, and the gate verdict.
func deepenBuildPilotReport(attempts []deepenPilotAttempt, convIDs []int) (deepenPilotReport, error) {
	report := deepenPilotReport{
		Stage:         "signal-pilot",
		Conversations: []string{fmt.Sprintf("conv-%d", convIDs[0]), fmt.Sprintf("conv-%d", convIDs[1])},
	}
	var posScores, negScores [deepenPilotSignals][]float64
	signalAvailable := [4]int{}
	posCount, negCount := 0, 0
	for i := range attempts {
		a := &attempts[i]
		scores := deepenHesitationScores(a.Features, a.LogprobStatus == "available", a.TextualHesitant)
		for s := 0; s < deepenPilotSignals; s++ {
			if s < 3 && a.LogprobStatus == "available" {
				signalAvailable[s]++
			}
			if s == 3 {
				signalAvailable[s]++
			}
		}
		if a.JudgeCorrect == nil {
			continue // unjudged attempts cannot label
		}
		if *a.JudgeCorrect {
			negCount++
			for s := 0; s < deepenPilotSignals; s++ {
				negScores[s] = append(negScores[s], scores[s])
			}
		} else {
			posCount++
			for s := 0; s < deepenPilotSignals; s++ {
				posScores[s] = append(posScores[s], scores[s])
			}
		}
	}
	if posCount == 0 || negCount == 0 {
		report.Gate = deepenPilotGate{Rule: "auc>=0.65 AND flip_rate<=0.10", Verdict: "NO-GO",
			Reason: fmt.Sprintf("degenerate label split: wrong=%d right=%d", posCount, negCount)}
		return report, nil
	}
	total := len(attempts)
	bestAUC := -1.0
	bestIdx := -1
	var bestThreshold float64
	for s := 0; s < deepenPilotSignals; s++ {
		auc, err := deepenAUC(posScores[s], negScores[s])
		if err != nil {
			report.Signals = append(report.Signals, deepenSignalReport{
				Kind: signalKindFor(s), Feature: signalFeatureFor(s), AUC: 0,
				AUCCI95: [2]float64{0, 0}, ParseCoverage: float64(signalAvailable[s]) / float64(total),
			})
			continue
		}
		lo, hi, bootErr := deepenAUCBootstrap(posScores[s], negScores[s], deepenAUCSeed, deepenAUCBootIter)
		if bootErr != nil {
			lo, hi = 0, 0
		}
		report.Signals = append(report.Signals, deepenSignalReport{
			Kind: signalKindFor(s), Feature: signalFeatureFor(s), AUC: auc,
			AUCCI95: [2]float64{lo, hi}, ParseCoverage: float64(signalAvailable[s]) / float64(total),
		})
		if auc > bestAUC { // strict > keeps the fixed index tie-break order
			bestAUC = auc
			bestIdx = s
			if th, thErr := deepenROCThreshold(posScores[s], negScores[s]); thErr == nil {
				bestThreshold = th
			}
		}
	}
	if bestIdx < 0 {
		report.Gate = deepenPilotGate{Rule: "auc>=0.65 AND flip_rate<=0.10", Verdict: "NO-GO",
			Reason: "no signal produced an AUC"}
		return report, nil
	}
	flips := 0
	for i := range attempts {
		if attempts[i].Flip {
			flips++
		}
	}
	report.ChannelParity = deepenChannelParity{N: total, Flips: flips, FlipRate: float64(flips) / float64(total)}
	report.Chosen = deepenChosenSignal{Kind: signalKindFor(bestIdx), Feature: signalFeatureFor(bestIdx), Threshold: bestThreshold}
	verdict, reason := deepenPilotGateVerdict(bestAUC, report.ChannelParity)
	report.Gate = deepenPilotGate{Rule: "auc>=0.65 AND flip_rate<=0.10", Verdict: verdict, Reason: reason}
	return report, nil
}
