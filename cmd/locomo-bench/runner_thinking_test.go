package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

// thinkingStreamProvider emits reasoning deltas followed by text deltas, the
// shape DeepSeek's Anthropic-compatible API produces (a separate thinking block
// ahead of the answer).
type thinkingStreamProvider struct {
	thinking string
	text     string
	noThink  bool // emit only text deltas (Qwen-on-vllm shape)
}

func (p *thinkingStreamProvider) Name() string { return "thinking-stub" }

func (p *thinkingStreamProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.ProviderEvent, error) {
	ch := make(chan provider.ProviderEvent, 8)
	if !p.noThink {
		ch <- provider.ProviderEvent{Type: provider.EventReasoningDelta, ReasoningDelta: "Let me consider. I'm not sure which candidate "}
		ch <- provider.ProviderEvent{Type: provider.EventReasoningDelta, ReasoningDelta: "fits the evidence."}
	}
	ch <- provider.ProviderEvent{Type: provider.EventTextDelta, TextDelta: p.text}
	ch <- provider.ProviderEvent{Type: provider.EventStop, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

// TestNewUsageModelCallerWithThinking: the confidence-gated answerer must see
// the provider's reasoning preamble so the hesitation detector has a signal.
// Without the merge (plain caller) the DeepSeek API thinking block is dropped
// and the gated loop never deepens (041D-iter: deepened=0).
func TestNewUsageModelCallerWithThinking(t *testing.T) {
	ctx := context.Background()

	t.Run("merges reasoning into thinking preamble", func(t *testing.T) {
		p := &thinkingStreamProvider{thinking: "x", text: "The answer is 5."}
		call := newUsageModelCallerWithThinking(p, "deepseek-v4-flash", 1000, "answer", nil)
		got, _, err := call(ctx, "sys", "user")
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		want := "<thinking>\nLet me consider. I'm not sure which candidate fits the evidence.\n</thinking>\nThe answer is 5."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		// extractFinalAnswer must strip the preamble for the judge.
		if final := extractFinalAnswer(got); final != "The answer is 5." {
			t.Errorf("extractFinalAnswer = %q, want %q", final, "The answer is 5.")
		}
		// the detector must now see the uncertainty in the thinking.
		_, deepen := detectHesitation(got, 3.0)
		if !deepen {
			t.Error("detectHesitation should deepen on thinking-segment uncertainty")
		}
	})

	t.Run("no thinking stays byte-identical to plain caller", func(t *testing.T) {
		p := &thinkingStreamProvider{noThink: true, text: "Berlin"}
		got, _, err := newUsageModelCallerWithThinking(p, "deepseek-v4-flash", 1000, "answer", nil)(ctx, "sys", "user")
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if got != "Berlin" {
			t.Errorf("got %q, want %q", got, "Berlin")
		}
	})

	t.Run("plain caller drops reasoning (regression guard)", func(t *testing.T) {
		p := &thinkingStreamProvider{thinking: "x", text: "The answer is 5."}
		got, _, err := newUsageModelCaller(p, "deepseek-v4-flash", 1000, 0, "answer", nil)(ctx, "sys", "user")
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if got != "The answer is 5." || strings.Contains(got, "thinking") {
			t.Errorf("plain caller must keep text-only, got %q", got)
		}
	})
}
