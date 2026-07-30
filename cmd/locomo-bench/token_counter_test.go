package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

type fixtureTokenCounter struct {
	counts      map[string]int
	fingerprint string
}

func (c fixtureTokenCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	key := input.System + "\x00" + input.User
	count, ok := c.counts[key]
	if !ok {
		return evidencecompiler.TokenCount{}, fmt.Errorf("missing fixture count for %q", key)
	}
	return evidencecompiler.TokenCount{InputTokens: count, Fingerprint: c.fingerprint}, nil
}

func TestTokenCounterCalibrationCoversUnicodeAndChatBoundaries(t *testing.T) {
	fixtures := []evalTokenCalibrationFixture{
		{Name: "cjk", Input: evidencecompiler.AnswerInput{Model: "local", System: "规则", User: "张三在上海"}, WantInputTokens: 11},
		{Name: "emoji", Input: evidencecompiler.AnswerInput{Model: "local", System: "be concise", User: "coffee ☕️ then 🧭"}, WantInputTokens: 18},
		{Name: "numbers-and-time", Input: evidencecompiler.AnswerInput{Model: "local", System: "UTC", User: "2026-07-30T12:34:56.789Z 1234567890"}, WantInputTokens: 24},
		{Name: "chat-boundary", Input: evidencecompiler.AnswerInput{Model: "local", System: "system prompt", User: "same words"}, WantInputTokens: 17},
	}
	counts := make(map[string]int, len(fixtures))
	for _, fixture := range fixtures {
		counts[fixture.Input.System+"\x00"+fixture.Input.User] = fixture.WantInputTokens
	}
	counter := fixtureTokenCounter{counts: counts, fingerprint: "sha256:local-tokenizer-template"}

	report, err := calibrateEvalTokenCounter(context.Background(), counter, fixtures, "sha256:local-tokenizer-template")
	if err != nil {
		t.Fatalf("calibrate token counter: %v", err)
	}
	if !report.Complete || report.MaxDelta != 0 || report.CounterFingerprint == "" {
		t.Fatalf("calibration report = %#v, want complete zero-delta fingerprinted result", report)
	}
}

func TestTokenCounterRejectsCapAndFingerprintDrift(t *testing.T) {
	count := evidencecompiler.TokenCount{InputTokens: 100, Fingerprint: "sha256:stable"}
	if err := validateEvalTokenCount(count, 100, "sha256:stable"); err != nil {
		t.Fatalf("at-cap count rejected: %v", err)
	}
	if err := validateEvalTokenCount(count, 99, "sha256:stable"); err == nil {
		t.Fatal("cap+1 token count unexpectedly accepted")
	}
	if err := validateEvalTokenCount(count, 100, "sha256:other"); err == nil {
		t.Fatal("counter fingerprint drift unexpectedly accepted")
	}
	if err := validateEvalTokenCount(evidencecompiler.TokenCount{InputTokens: 0, Fingerprint: "sha256:stable"}, 100, "sha256:stable"); err == nil {
		t.Fatal("non-positive token count unexpectedly accepted")
	}
}
