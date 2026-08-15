package main

// 030 US1 exact token accounting (specs/030, research decision 1,
// contracts/evidence-assembly.md). FR-002: the assembly's TotalTokens MUST be
// the answerer's real tokenizer count, never a character/runes estimate. We
// reuse the existing chat-aware vLLM /tokenize counter (vllmTokenCounter,
// token_counter.go) and count the FULL rendered answer prompt ONCE; the count
// includes chat-template boundary tokens, so it is the exact number the
// answerer consumes. Per-unit token bookkeeping inside the assembler uses
// estimateTokens ONLY for deterministic sort/truncate ordering and is never
// summed into TotalTokens (evidencecompiler discipline: never derive a total
// by summing parts).

import (
	"context"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// assemblyTokenCounter wraps the exact chat-aware tokenizer counter for the
// assembly pipeline. counter is an interface so tests inject a stub (offline);
// the production value is *vllmTokenCounter. counter is nil when the exact
// counter is unavailable (offline, no answerer configured); callers then fall
// back to the estimate ledger and mark the assembly tokens_estimated=true
// (constitution V explicit degradation).
type assemblyTokenCounter struct {
	counter     evidencecompiler.TokenCounter // nil = exact counter unavailable
	answerModel string
}

// countPrompt returns the exact token count of the full (system, user) answer
// input as the configured answerer would render it. ok=false (nil error) means
// the exact counter is unavailable and the caller must use the estimate ledger.
func (c *assemblyTokenCounter) countPrompt(ctx context.Context, system, user string) (count int, ok bool, err error) {
	if c == nil || c.counter == nil || strings.TrimSpace(c.answerModel) == "" {
		return 0, false, nil
	}
	tokenCount, err := c.counter.CountInput(ctx, evidencecompiler.AnswerInput{
		Model:  c.answerModel,
		System: system,
		User:   user,
	})
	if err != nil {
		return 0, false, err
	}
	return tokenCount.InputTokens, true, nil
}

// estimateTokens is a local, offline approximation used for answer-context
// budget bookkeeping (008 discipline). It intentionally under-counts vs a
// tokenizer; the budget cap is validated per-run and over-cap bundles truncate.
// Moved here from agentic_nav.go (044 T007); removed with 030/031 (044 T012).
func estimateTokens(text string) int {
	n := len([]rune(text))
	if n <= 0 {
		return 0
	}
	t := n / 4
	if t < 1 {
		return 1
	}
	return t
}

// defaultAnswerContextCap bounds the assembled answer context in tokens.
// Moved here from the removed 029 nav adapter (044 T007); used by the 030/031
// assembly/relation paths (removed with them, 044 T012).
const defaultAnswerContextCap = 3600
