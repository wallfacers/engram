package main

// 045 ride-along: --reverify-042 CLI (box stage, shares the single
// consolidated boot). Re-collects the 042 counterfactual signal on the
// 2-conv slice with the FIXED measurement (temperature=0 logprob channel +
// robust final-span mapping) and scores it against the 042 keep/deepen
// labels' per-question correctness. Verdicts state measurement facts only —
// whether to reopen 042 stays with the maintainer (spec FR-013).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	rvSliceConvs    = "0,1"
	rvFeatureNames  = 3
	rvStreamBodyCap = 1 << 22
)

func rvQuestionID(convID, qIndex int) string {
	return fmt.Sprintf("c%dq%d", convID, qIndex)
}

// rvLoadLabels reads utility-labels.jsonl and majority-aggregates
// shallow_correct per unique question (labels are per-repetition).
func rvLoadLabels(dir string) (map[string]bool, int, error) {
	path := filepath.Join(dir, "hidden", "utility-labels.jsonl")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(dir, "utility-labels.jsonl")
		if _, err := os.Stat(path); err != nil {
			return nil, 0, fmt.Errorf("utility-labels.jsonl not found under %s", dir)
		}
	}
	f, err := os.Open(path) //nolint:gosec // operator-supplied artifact dir
	if err != nil {
		return nil, 0, err
	}
	defer f.Close() //nolint:errcheck
	type decisionKey struct {
		ConversationID int `json:"conversation_id"`
		QuestionIndex  int `json:"question_index"`
	}
	type row struct {
		DecisionKey     decisionKey `json:"decision_key"`
		ShallowCorrect bool        `json:"shallow_correct"`
	}
	agg := map[string][2]int{} // id → [trueCount, falseCount]
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, rvStreamBodyCap), rvStreamBodyCap)
	rows := 0
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, 0, fmt.Errorf("labels row %d: %w", rows, err)
		}
		id := rvQuestionID(r.DecisionKey.ConversationID, r.DecisionKey.QuestionIndex)
		counts := agg[id]
		if r.ShallowCorrect {
			counts[0]++
		} else {
			counts[1]++
		}
		agg[id] = counts
		rows++
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	out := make(map[string]bool, len(agg))
	for id, c := range agg {
		out[id] = c[0] >= c[1] // majority correct; ties → correct (conservative)
	}
	return out, rows, nil
}

type rvTask struct {
	QuestionID string
	Conv       conversation
	QAIndex    int
}

type rvResult struct {
	QuestionID string   `json:"question_id"`
	Features   []float64 `json:"features,omitempty"`
	Available  bool     `json:"available"`
	Reason     string   `json:"reason,omitempty"`
	Wrong      bool     `json:"wrong"`
	Labeled    bool     `json:"labeled"`
	LogprobAnswer string `json:"logprob_answer"`
	StreamAnswer  string `json:"stream_answer"`
	Flip       bool     `json:"flip"`
	Error      string   `json:"error,omitempty"`
}

// runReverify042CLI executes the ride-along and exits (flag-triggered mode).
func runReverify042CLI(ctx context.Context, opt options, convs []conversation, arms []string, logger *slog.Logger) error {
	if opt.storeDir == "" {
		return fmt.Errorf("--reverify-042 requires --store-dir")
	}
	if opt.reverifyLabels == "" {
		return fmt.Errorf("--reverify-042 requires --reverify-labels pointing at the 042 collect dir")
	}
	if err := os.MkdirAll(opt.reverifyDir, 0o755); err != nil {
		return fmt.Errorf("create reverify dir: %w", err)
	}
	labels, labelRows, err := rvLoadLabels(opt.reverifyLabels)
	if err != nil {
		return err
	}

	baseURL := os.Getenv("LOCOMO_BASE_URL")
	model := os.Getenv("LOCOMO_MODEL")
	apiKey := os.Getenv("LOCOMO_API_KEY")
	if baseURL == "" || model == "" {
		return fmt.Errorf("--reverify-042 requires LOCOMO_BASE_URL and LOCOMO_MODEL env (answerer endpoint)")
	}
	caller, err := rvNewCaller(baseURL, apiKey, model, rvDefaultMaxTok, nil)
	if err != nil {
		return err
	}
	streamer := rvNewStreamer(baseURL, apiKey, model, rvDefaultMaxTok)

	// Anchor-recipe answering config: unified contract on, k30 + quota 12.
	packOpt := opt
	packOpt.unifiedAnswerContract = true

	slice := map[int]bool{0: true, 1: true}
	var tasks []rvTask
	for _, conv := range convs {
		if !slice[conv.ID] {
			continue
		}
		for qi := range conv.QA {
			tasks = append(tasks, rvTask{
				QuestionID: rvQuestionID(conv.ID, qi),
				Conv:       conv,
				QAIndex:    qi,
			})
		}
	}

	results := make([]rvResult, len(tasks))
	sem := make(chan struct{}, max(opt.concurrency, 1))
	var wg sync.WaitGroup
	var mu sync.Mutex
	firstErr := ""
	for ti, task := range tasks {
		wg.Add(1)
		go func(ti int, task rvTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := rvRunOne(ctx, opt, packOpt, task, labels, caller, streamer, logger, arms[0])
			if err != nil {
				mu.Lock()
				if firstErr == "" {
					firstErr = err.Error()
				}
				mu.Unlock()
				return
			}
			results[ti] = res
		}(ti, task)
	}
	wg.Wait()
	if firstErr != "" {
		return fmt.Errorf("reverify worker failed (aborting, no partial report): %s", firstErr)
	}
	return rvWriteReport(opt.reverifyDir, results, labelRows)
}

// rvRunOne performs retrieval + dual-channel answer + signal mapping. It
// opens the conversation runtime per task (store opens are cheap; SQLite
// queries serialize on the single connection anyway).
func rvRunOne(ctx context.Context, opt, packOpt options, task rvTask, labels map[string]bool,
	caller *rvCaller, streamer *rvStreamer, logger *slog.Logger, arm string) (rvResult, error) {
	res := rvResult{QuestionID: task.QuestionID}
	qa := task.Conv.QA[task.QAIndex]
	if correct, ok := labels[task.QuestionID]; ok {
		res.Labeled = true
		res.Wrong = !correct
	}
	runtime, err := openAicGateRuntime(ctx, opt, task.Conv, arm, logger)
	if err != nil {
		return res, err
	}
	defer runtime.Close() //nolint:errcheck
	retriever := runtime.retrievers[arm]
	if retriever == nil {
		return res, fmt.Errorf("reverify: no retriever for arm %q", arm)
	}
	hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, aicGateTopK, aicGateQuota, nil)
	if err != nil {
		res.Error = fmt.Sprintf("retrieve: %v", err)
		return res, nil
	}
	system := answerSystemPromptForEval(qa, packOpt)
	user := buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, false)

	completion, err := caller.complete(ctx, system, user)
	if err != nil {
		return res, fmt.Errorf("logprob channel q=%s: %w", task.QuestionID, err)
	}
	res.LogprobAnswer = completion.Content
	feats, ok, reason := rvFinalSpanSignal(completion.Tokens)
	res.Features, res.Available, res.Reason = feats, ok, reason

	streamed, err := streamer.complete(ctx, system, user)
	if err != nil {
		return res, fmt.Errorf("stream channel q=%s: %w", task.QuestionID, err)
	}
	res.StreamAnswer = streamed
	res.Flip = extractFinalAnswer(completion.Content) != extractFinalAnswer(streamed)
	return res, nil
}

func rvWriteReport(dir string, results []rvResult, labelRows int) error {
	var posScores, negScores [rvFeatureNames][]float64
	valid := 0
	labeled := 0
	flips := 0
	for _, r := range results {
		if r.Flip {
			flips++
		}
		if !r.Available || !r.Labeled {
			continue
		}
		valid++
		labeled++
		for f := 0; f < rvFeatureNames && f < len(r.Features); f++ {
			if r.Wrong {
				posScores[f] = append(posScores[f], r.Features[f])
			} else {
				negScores[f] = append(negScores[f], r.Features[f])
			}
		}
	}
	type featureStat struct {
		Feature string  `json:"feature"`
		AUC     float64 `json:"auc"`
		Lo95    float64 `json:"ci95_lo"`
		Hi95    float64 `json:"ci95_hi"`
	}
	names := []string{"final_mean_logprob", "final_p10_logprob", "final_mean_top1_top2_margin"}
	var stats []featureStat
	for f := 0; f < rvFeatureNames; f++ {
		auc, err := rvAUC(posScores[f], negScores[f])
		if err != nil {
			continue
		}
		lo, hi, _ := rvAUCBootstrap(posScores[f], negScores[f], rvBootstrapSeed, rvBootstrapIters)
		stats = append(stats, featureStat{Feature: names[f], AUC: auc, Lo95: lo, Hi95: hi})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Feature < stats[j].Feature })

	total := len(results)
	validRate := 0.0
	if total > 0 {
		validRate = float64(valid) / float64(total)
	}
	flipRate := 0.0
	if total > 0 {
		flipRate = float64(flips) / float64(total)
	}
	// Verdict states measurement facts only (FR-013): the 042 "5/5958" valid
	// rate vs the fixed collection's; reopening 042 is the maintainer's call.
	verdict := "inconclusive"
	switch {
	case validRate < 0.5:
		verdict = "signal-still-invalid"
	case len(stats) > 0 && stats[0].AUC >= 0.65:
		verdict = "measurement-artifact-confirmed" // signal now discriminates
	case len(stats) > 0:
		verdict = "measurement-artifact-confirmed" // collection fixed; AUC read is the fact
	}
	report := map[string]any{
		"feature":            "045 ride-along reverify-042",
		"slice":              rvSliceConvs,
		"questions":          total,
		"label_rows_loaded":  labelRows,
		"labeled_questions":  labeled,
		"valid_collection":   valid,
		"valid_rate":         validRate,
		"channel_flip_rate":  flipRate,
		"features":           stats,
		"verdict":            verdict,
		"verdict_note":       "measurement facts only — reopening 042 is the maintainer's decision (FR-013)",
	}
	blob, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	if err := os.WriteFile(filepath.Join(dir, "reverify_042.json"), blob, 0o644); err != nil {
		return err
	}
	perPath := filepath.Join(dir, "reverify_042_questions.jsonl")
	f, err := os.Create(perPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	ids := make([]string, len(results))
	byID := map[string]int{}
	for i, r := range results {
		ids[i] = r.QuestionID
		byID[r.QuestionID] = i
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := enc.Encode(results[byID[id]]); err != nil {
			f.Close() //nolint:errcheck
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("reverify-042: questions=%d valid_rate=%.4f flip=%.4f verdict=%s\n",
		total, validRate, flipRate, verdict)
	return nil
}
