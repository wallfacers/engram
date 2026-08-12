package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

// stubAnswerCaller is a deterministic usageModelCaller for counter-refine tests.
type stubAnswerCaller struct {
	text  string
	err   error
	calls int
}

func (s *stubAnswerCaller) call(_ context.Context, _, _ string) (string, provider.Usage, error) {
	s.calls++
	if s.err != nil {
		return "", provider.Usage{}, s.err
	}
	return s.text, provider.Usage{InputTokens: 10, OutputTokens: 5}, nil
}

func memResult(id, content string) memory.Result {
	return memory.Result{ID: id, Content: content, SourceSessionID: "sess"}
}

func TestCounterRefineKeys(t *testing.T) {
	if got := counterRefineKeys("Fixing cars"); len(got) != 2 || got[0] != "fixing" || got[1] != "cars" {
		t.Fatalf("Fixing cars → %v, want [fixing cars]", got)
	}
	// stop/generic words dropped
	if got := counterRefineKeys("The that with cars"); len(got) != 1 || got[0] != "cars" {
		t.Fatalf("stop-word draft → %v, want [cars]", got)
	}
	// short tokens dropped
	if got := counterRefineKeys("I am"); len(got) != 0 {
		t.Fatalf("short draft → %v, want empty", got)
	}
}

func TestCounterRefineHit(t *testing.T) {
	keys := []string{"kayaking"}
	if !counterRefineHit(keys, "Sam planned to go kayaking on the lake") {
		t.Fatal("content mentions key → want hit")
	}
	if counterRefineHit(keys, "Sam started eating healthier") {
		t.Fatal("content lacks key → want miss")
	}
}

func TestSelectCounterEvidence(t *testing.T) {
	hits := []memory.Result{
		memResult("1", "Sam started eating healthier in October"),
		memResult("2", "Sam and his mate planned to go kayaking"),
		memResult("3", "Sam attended a Weight Watchers meeting"),
	}
	// draft key "kayaking" selects the matched memory first
	sel := selectCounterEvidence("Kayaking", "What activity did Sam take up?", hits, 0)
	if len(sel) != 1 || sel[0].ID != "2" {
		t.Fatalf("draft-key selection → %+v, want only [2]", ids(sel))
	}
	// no draft-key hits → fallback to head of hits
	sel = selectCounterEvidence("zzzznone", "question", hits, 0)
	if len(sel) == 0 || sel[0].ID != "1" {
		t.Fatalf("fallback → %+v, want head [1]", ids(sel))
	}
	// empty hits → nil
	if sel := selectCounterEvidence("x", "q", nil, 0); sel != nil {
		t.Fatalf("empty hits → want nil, got %+v", sel)
	}
	// char cap truncates
	big := []memory.Result{}
	for i := 0; i < 50; i++ {
		big = append(big, memResult(strings.Repeat("x", 10), strings.Repeat("content", 30)))
	}
	sel = selectCounterEvidence("", "q", big, 100) // cap*3 = 300 chars
	if len(sel) > 10 {
		t.Fatalf("cap truncation → %d hits, want ≤10", len(sel))
	}
}

func ids(hits []memory.Result) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func TestCounterRefineMechanismFlag(t *testing.T) {
	// off → absent (legacy B1 control stays byte-identical)
	off := densityMechanismFlagsForOptions(options{})
	if off["counter_refine"] {
		t.Fatal("default off must not record counter_refine")
	}
	// on → recorded in mechanism flags
	on := densityMechanismFlagsForOptions(options{counterRefine: true})
	if !on["counter_refine"] {
		t.Fatal("--counter-refine must record counter_refine=true")
	}
	// accepted by the formal control flag gate
	if !isFormalControlMechanismFlags(map[string]bool{
		"idk_retry": false, "iris": false, "rerank": false, "counter_refine": true,
	}) {
		t.Fatal("counter_refine must be a legal formal mechanism flag")
	}
}

func TestCounterRefineAnswer(t *testing.T) {
	qa := locomoQA{Question: "What hobby did Calvin take up recently?", Category: 3}
	opt := options{}
	hits := []memory.Result{
		memResult("1", "Calvin enjoys fixing cars to calm down"),
		memResult("2", "Dave has taken up photography recently"),
	}

	t.Run("revise to better answer", func(t *testing.T) {
		stub := &stubAnswerCaller{text: "Photography"}
		a1, usage, err := counterRefineAnswer(context.Background(), stub.call, opt, qa, "Fixing cars", hits)
		if err != nil || a1 != "Photography" {
			t.Fatalf("revise → %q err=%v, want Photography", a1, err)
		}
		if stub.calls != 1 {
			t.Fatalf("answer calls = %d, want 1", stub.calls)
		}
		if usage.InputTokens != 10 || usage.OutputTokens != 5 {
			t.Fatalf("usage = %+v, want {10 5}", usage)
		}
	})

	t.Run("empty revise keeps draft", func(t *testing.T) {
		stub := &stubAnswerCaller{text: "  "}
		a1, _, err := counterRefineAnswer(context.Background(), stub.call, opt, qa, "Fixing cars", hits)
		if err != nil || a1 != "Fixing cars" {
			t.Fatalf("empty revise → %q err=%v, want keep draft", a1, err)
		}
	})

	t.Run("IDK revise keeps draft", func(t *testing.T) {
		stub := &stubAnswerCaller{text: "I don't know"}
		a1, _, err := counterRefineAnswer(context.Background(), stub.call, opt, qa, "Fixing cars", hits)
		if err != nil || a1 != "Fixing cars" {
			t.Fatalf("IDK revise → %q err=%v, want keep draft", a1, err)
		}
	})

	t.Run("no hits skips revise call", func(t *testing.T) {
		stub := &stubAnswerCaller{text: "Photography"}
		a1, _, err := counterRefineAnswer(context.Background(), stub.call, opt, qa, "Fixing cars", nil)
		if err != nil || a1 != "Fixing cars" {
			t.Fatalf("no hits → %q err=%v, want keep draft", a1, err)
		}
		if stub.calls != 0 {
			t.Fatalf("answer calls = %d, want 0 (skipped)", stub.calls)
		}
	})

	t.Run("call error keeps draft and surfaces err", func(t *testing.T) {
		stub := &stubAnswerCaller{err: context.Canceled}
		a1, _, err := counterRefineAnswer(context.Background(), stub.call, opt, qa, "Fixing cars", hits)
		if err == nil || a1 != "Fixing cars" {
			t.Fatalf("call error → %q err=%v, want keep draft + surfaced err", a1, err)
		}
	})
}
