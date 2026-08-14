package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
)

// --- T003: closed-state golden (SC-003) ---

func TestValidateConfidenceGatedOffIsNoOp(t *testing.T) {
	// --confidence-gated=false (default): validation must not reject anything,
	// even flag combinations that are invalid when the mechanism is on.
	bad := options{
		confidenceGated:     false,
		confidenceDeepK:     10, // <= shallow would be invalid when on
		confidenceShallowK:  150,
		confidenceThreshold: -1,
		confidenceMaxRounds: 1,
	}
	if err := validateConfidenceGatedOptions(bad); err != nil {
		t.Fatalf("off-state must be a no-op, got %v", err)
	}
}

func TestValidateConfidenceGatedOn(t *testing.T) {
	valid := options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceThreshold: 3, confidenceMaxRounds: 2}
	if err := validateConfidenceGatedOptions(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	cases := []struct {
		name string
		opt  options
		want string
	}{
		{"deep must exceed shallow", options{confidenceGated: true, confidenceShallowK: 150, confidenceDeepK: 30, confidenceMaxRounds: 2}, "exceed"},
		{"negative threshold", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceThreshold: -1, confidenceMaxRounds: 2}, ">= 0"},
		{"rounds below 2", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceMaxRounds: 1}, "at least 2"},
		{"multi-query", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceMaxRounds: 2, multiQuery: true}, "multi-query"},
		{"cat-top-k", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceMaxRounds: 2, catTopKSpec: "1=150"}, "cat-top-k"},
		{"formal protocol", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceMaxRounds: 2, evalProtocolPath: "x.json"}, "frozen"},
		{"abstain-hard", options{confidenceGated: true, confidenceShallowK: 30, confidenceDeepK: 150, confidenceMaxRounds: 2, abstainHard: true}, "abstain-hard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfidenceGatedOptions(tc.opt)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// --- T008: iterative loop flow ---

// stubAnswerer returns the given texts per call, and counts calls.
func stubAnswerer(texts ...string) (usageModelCaller, *int) {
	calls := 0
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		idx := calls
		calls++
		if idx >= len(texts) {
			idx = len(texts) - 1
		}
		return texts[idx], provider.Usage{InputTokens: 100 + idx, OutputTokens: 10}, nil
	}, &calls
}

func stubJudge(correct bool) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		if correct {
			return `{"correct": true}`, provider.Usage{}, nil
		}
		return `{"correct": false}`, provider.Usage{}, nil
	}
}

func newEmptyRetriever(t *testing.T) *memory.Retriever {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	entries := memory.NewEntryStore(st.DB())
	return memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil)
}

func mustQA(t *testing.T, question, gold string) locomoQA {
	t.Helper()
	raw, err := json.Marshal(gold)
	if err != nil {
		t.Fatalf("marshal gold: %v", err)
	}
	return locomoQA{Question: question, Answer: raw, Category: 1, QuestionID: "conv-0-q-1"}
}

func TestRunConfidenceGatedConfidentStopsShallow(t *testing.T) {
	// A confident shallow answer must NOT deepen: FinalFromRound=1, one answer
	// call, shallow-only usage.
	answerCall, calls := stubAnswerer("Berlin")
	judgeCall := stubJudge(true)
	qa := mustQA(t, "Where does the user live?", "Berlin")
	opt := options{judgeMem0Aligned: true}

	correct, predicted, usage, rec, err := runConfidenceGatedQuestion(
		context.Background(), newEmptyRetriever(t), qa, opt,
		budgetLadder{ShallowTopK: 30, DeepTopK: 150},
		confidenceGateConfig{Threshold: 3.0, MaxRounds: 2},
		answerCall, judgeCall,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("answer calls = %d, want 1 (confident must not deepen)", *calls)
	}
	if rec.Deepened || rec.FinalFromRound != 1 {
		t.Fatalf("rec = %+v, want FinalFromRound=1 Deepened=false", rec)
	}
	if predicted != "Berlin" || !correct {
		t.Fatalf("predicted=%q correct=%v, want Berlin/true", predicted, correct)
	}
	if usage.OutputTokens != 10 {
		t.Fatalf("usage.OutputTokens = %v, want 10 (shallow round only)", usage.OutputTokens)
	}
}

func TestRunConfidenceGatedHesitantDeepens(t *testing.T) {
	// A hesitant shallow answer must deepen: second retrieve+answer, the deep
	// answer wins, usage accumulates both rounds.
	answerCall, calls := stubAnswerer(
		"I'm not sure. Could be Berlin or Paris.",
		"Paris",
	)
	judgeCall := stubJudge(true)
	qa := mustQA(t, "Where does the user live?", "Paris")
	opt := options{judgeMem0Aligned: true}

	correct, predicted, usage, rec, err := runConfidenceGatedQuestion(
		context.Background(), newEmptyRetriever(t), qa, opt,
		budgetLadder{ShallowTopK: 30, DeepTopK: 150},
		confidenceGateConfig{Threshold: 3.0, MaxRounds: 2},
		answerCall, judgeCall,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("answer calls = %d, want 2 (hesitant must deepen)", *calls)
	}
	if !rec.Deepened || rec.FinalFromRound != 2 {
		t.Fatalf("rec = %+v, want FinalFromRound=2 Deepened=true", rec)
	}
	if rec.DeepSignal == nil {
		t.Fatal("deep signal should be recorded")
	}
	if predicted != "Paris" || !correct {
		t.Fatalf("predicted=%q correct=%v, want Paris/true", predicted, correct)
	}
	// Usage accumulates: round1 (100 in / 10 out) + round2 (101 in / 10 out).
	if usage.InputTokens != 201 {
		t.Fatalf("usage.InputTokens = %v, want 201 (both rounds)", usage.InputTokens)
	}
	if rec.ShallowAnswer != "I'm not sure. Could be Berlin or Paris." {
		t.Fatalf("shallow answer not recorded verbatim: %q", rec.ShallowAnswer)
	}
}

func TestRunConfidenceGatedDeepRetrievalFailureFallsBack(t *testing.T) {
	// FR-005 / constitution V: a deep-retrieval failure must fall back to the
	// shallow answer, not fail the question. Simulate by making the second
	// answer call error.
	var calls int
	answerCall := func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		calls++
		if calls == 1 {
			return "I'm not sure.", provider.Usage{InputTokens: 100, OutputTokens: 10}, nil
		}
		return "", provider.Usage{}, context.DeadlineExceeded
	}
	judgeCall := stubJudge(true)
	qa := mustQA(t, "Where does the user live?", "Berlin")
	opt := options{judgeMem0Aligned: true}

	correct, predicted, _, rec, err := runConfidenceGatedQuestion(
		context.Background(), newEmptyRetriever(t), qa, opt,
		budgetLadder{ShallowTopK: 30, DeepTopK: 150},
		confidenceGateConfig{Threshold: 3.0, MaxRounds: 2},
		answerCall, judgeCall,
	)
	if err != nil {
		t.Fatalf("deep failure should degrade to the shallow answer, got %v", err)
	}
	if rec.FinalFromRound != 1 || rec.Deepened {
		t.Fatalf("rec = %+v, want FinalFromRound=1 Deepened=false", rec)
	}
	if predicted != "I'm not sure." || !correct {
		t.Fatalf("predicted=%q correct=%v, want shallow fallback", predicted, correct)
	}
}

func TestRunConfidenceGatedJudgesFinalAnswer(t *testing.T) {
	// The judge must grade the thinking-stripped final answer, not the raw
	// generation (judge-noise guard, runner.go extractFinalAnswer contract).
	answerCall, _ := stubAnswerer(
		"<thinking>I'm not sure which.</thinking>\nParis",
	)
	judgeCall := stubJudge(false)
	qa := mustQA(t, "Where does the user live?", "Berlin")
	opt := options{judgeMem0Aligned: true}

	correct, predicted, _, _, err := runConfidenceGatedQuestion(
		context.Background(), newEmptyRetriever(t), qa, opt,
		budgetLadder{ShallowTopK: 30, DeepTopK: 150},
		confidenceGateConfig{Threshold: 3.0, MaxRounds: 2},
		answerCall, judgeCall,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if predicted != "Paris" {
		t.Fatalf("predicted = %q, want the thinking-stripped 'Paris'", predicted)
	}
	if correct {
		t.Fatal("judge stub returned false; correct must be false")
	}
}
