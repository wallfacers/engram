package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

// runConfidenceGatedQuestion runs the two-tier iterative retrieval for one
// question (specs/041 US2):
//
//  1. shallow retrieve (ShallowTopK) → answerer → deterministic hesitation check
//  2. hesitant → deep retrieve (DeepTopK) → answer again, use the deep answer
//     confident → stop at the shallow answer (avoids the fixed-150 tax AND the
//     31 questions top-k 150 actively hurts — 040 verdict).
//
// FR-004: max two rounds (the loop has exactly one deepen opportunity); the
// second-round answer is final regardless of remaining hesitation.
// FR-005: a nil retriever or a generation with no usable signal never errors —
// it falls back to the single shallow round, the default top-k semantics.
//
// The decision record is returned for run-dir audit (conf_gate_decisions.jsonl).
func runConfidenceGatedQuestion(ctx context.Context, retriever *memory.Retriever, qa locomoQA, opt options, ladder budgetLadder, cfg confidenceGateConfig, answerCall usageModelCaller, judgeCall usageModelCaller) (bool, string, provider.Usage, iterationDecisionRecord, error) {
	var rec iterationDecisionRecord
	rec.QuestionID = qa.QuestionID
	rec.Question = qa.Question

	var totalUsage provider.Usage
	answerer := func(topK int) ([]memory.Result, string, provider.Usage, error) {
		if retriever == nil {
			return nil, "", provider.Usage{}, fmt.Errorf("retriever unavailable")
		}
		hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, topK, ladder.ChunkQuota, nil)
		if err != nil {
			return nil, "", provider.Usage{}, err
		}
		input := buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
		answer, usage, err := answerCall(ctx, answerSystemPromptForEval(qa, opt), input)
		return hits, answer, usage, err
	}

	// Shallow round.
	shallowHits, answer1, usage1, err := answerer(ladder.ShallowTopK)
	if err != nil {
		return false, "", totalUsage, rec, err
	}
	totalUsage.InputTokens += usage1.InputTokens
	totalUsage.OutputTokens += usage1.OutputTokens
	rec.ShallowHits = len(shallowHits)
	rec.ShallowAnswer = answer1

	sig1, deepen := detectHesitation(answer1, cfg.Threshold)
	rec.ShallowSignal = sig1

	if !deepen || cfg.MaxRounds < 2 {
		// Confident (or iteration disabled to one round): stop at the shallow
		// answer. This is the budget win — most questions never read the deep pool.
		final := extractFinalAnswer(answer1)
		rec.FinalAnswer = final
		rec.FinalFromRound = 1
		rec.Deepened = false
		correct, err := judgeConfidenceGated(ctx, qa, final, opt, judgeCall)
		return correct, final, totalUsage, rec, err
	}

	// Deep round: the answerer was hesitant with only shallow evidence.
	deepHits, answer2, usage2, err := answerer(ladder.DeepTopK)
	if err != nil {
		// Fall back to the shallow answer on a deep-retrieval failure rather than
		// failing the question (graceful degradation, constitution V).
		final := extractFinalAnswer(answer1)
		rec.FinalAnswer = final
		rec.FinalFromRound = 1
		rec.Deepened = false
		correct, judgeErr := judgeConfidenceGated(ctx, qa, final, opt, judgeCall)
		if judgeErr != nil {
			return false, final, totalUsage, rec, judgeErr
		}
		return correct, final, totalUsage, rec, nil
	}
	totalUsage.InputTokens += usage2.InputTokens
	totalUsage.OutputTokens += usage2.OutputTokens
	rec.DeepHits = len(deepHits)
	rec.DeepAnswer = answer2
	sig2, _ := detectHesitation(answer2, cfg.Threshold)
	rec.DeepSignal = &sig2
	rec.Deepened = true

	final := extractFinalAnswer(answer2)
	rec.FinalAnswer = final
	rec.FinalFromRound = 2
	correct, err := judgeConfidenceGated(ctx, qa, final, opt, judgeCall)
	return correct, final, totalUsage, rec, err
}

// judgeConfidenceGated grades the final answer with the standard judge call.
func judgeConfidenceGated(ctx context.Context, qa locomoQA, final string, opt options, judgeCall usageModelCaller) (bool, error) {
	if judgeCall == nil {
		return false, fmt.Errorf("judge caller unavailable")
	}
	raw, _, err := judgeCall(ctx, judgeSystemPromptFor(opt.judgeAlignmentMode()), buildJudgePrompt(qa.Question, qa.AnswerText(), final))
	if err != nil {
		return false, err
	}
	return parseJudgeVerdict(raw), nil
}

// confidenceGateJournal is the concurrency-safe writer for the per-question
// iteration audit (run-dir/conf_gate_decisions.jsonl). Modeled on the existing
// nav/trace journals.
type confidenceGateJournal struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	path string
}

func openConfidenceGateJournal(path string) (*confidenceGateJournal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open confidence gate journal: %w", err)
	}
	return &confidenceGateJournal{f: f, w: bufio.NewWriter(f), path: path}, nil
}

func (j *confidenceGateJournal) Write(rec iterationDecisionRecord) error {
	if j == nil {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.w.Write(append(line, '\n')); err != nil {
		return err
	}
	return j.w.Flush()
}

func (j *confidenceGateJournal) Close() error {
	if j == nil || j.f == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	flushErr := j.w.Flush()
	closeErr := j.f.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
