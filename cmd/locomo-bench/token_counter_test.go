package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
)

func TestVLLMTokenCounterUsesChatTemplateEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenize" {
			t.Fatalf("tokenizer path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer local-only" {
			t.Fatalf("authorization = %q", got)
		}
		var request vllmTokenizeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode tokenizer request: %v", err)
		}
		if request.Model != "answerer-r1" || !request.AddGenerationPrompt || len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Content != "question plus evidence" {
			t.Fatalf("tokenizer request = %+v", request)
		}
		_, _ = w.Write([]byte(`{"count":17}`))
	}))
	defer server.Close()

	counter, err := newVLLMTokenCounter(vllmTokenCounterConfig{
		BaseURL:     server.URL + "/v1",
		APIKey:      "local-only",
		Fingerprint: "sha256:answerer-template-r1",
		HTTPClient:  server.Client(),
	})
	if err != nil {
		t.Fatalf("new counter: %v", err)
	}
	got, err := counter.CountInput(context.Background(), evidencecompiler.AnswerInput{
		Model: "answerer-r1", System: "system rules", User: "question plus evidence",
	})
	if err != nil {
		t.Fatalf("count input: %v", err)
	}
	if got.InputTokens != 17 || got.Fingerprint != "sha256:answerer-template-r1" {
		t.Fatalf("token count = %+v", got)
	}
}

func TestVLLMTokenCounterRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "temporary tokenizer overload", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"count":17}`))
	}))
	defer server.Close()

	counter, err := newVLLMTokenCounter(vllmTokenCounterConfig{
		BaseURL:     server.URL,
		Fingerprint: "sha256:answerer-template-r1",
		HTTPClient:  server.Client(),
	})
	if err != nil {
		t.Fatalf("new counter: %v", err)
	}
	got, err := counter.CountInput(context.Background(), evidencecompiler.AnswerInput{
		Model: "answerer-r1", System: "system rules", User: "question plus evidence",
	})
	if err != nil || got.InputTokens != 17 || attempts.Load() != 3 {
		t.Fatalf("transient token count = (%+v, %v), attempts=%d", got, err, attempts.Load())
	}
}

func TestGateTokenCounterSharesAdmissionLimit(t *testing.T) {
	upstream := &blockingTokenCounter{
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		fingerprint: "sha256:counter",
	}
	counter := gateTokenCounter(make(chan struct{}, 1), upstream)
	input := evidencecompiler.AnswerInput{Model: "answerer-r1", System: "system", User: "question"}

	firstDone := make(chan error, 1)
	go func() {
		_, err := counter.CountInput(context.Background(), input)
		firstDone <- err
	}()
	<-upstream.started

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := counter.CountInput(canceled, input); err == nil {
		t.Fatal("admission-full token count unexpectedly bypassed canceled context")
	}
	if got := upstream.calls.Load(); got != 1 {
		t.Fatalf("upstream calls while gate was full = %d, want 1", got)
	}

	close(upstream.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first gated token count: %v", err)
	}
}

type blockingTokenCounter struct {
	started     chan struct{}
	release     chan struct{}
	fingerprint string
	calls       atomic.Int32
}

func (counter *blockingTokenCounter) CountInput(ctx context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if counter.calls.Add(1) == 1 {
		close(counter.started)
	}
	select {
	case <-counter.release:
		return evidencecompiler.TokenCount{InputTokens: 7, Fingerprint: counter.fingerprint}, nil
	case <-ctx.Done():
		return evidencecompiler.TokenCount{}, ctx.Err()
	}
}

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

func TestTokenCounterRuntimeCalibrationRejectsProviderUsageDrift(t *testing.T) {
	fixture := evalTokenCalibrationFixture{
		Name:  "chat-boundary",
		Input: evidencecompiler.AnswerInput{Model: "local", System: "system", User: "user"},
	}
	counter := fixtureTokenCounter{
		counts:      map[string]int{"system\x00user": 17},
		fingerprint: "sha256:counter",
	}
	matching := func(_ context.Context, _, _ string) (string, provider.Usage, error) {
		return "ok", provider.Usage{InputTokens: 17}, nil
	}
	report, err := calibrateEvalTokenCounterAgainstRuntime(context.Background(), counter, matching, []evalTokenCalibrationFixture{fixture}, "sha256:counter")
	if err != nil {
		t.Fatalf("matching runtime calibration: %v", err)
	}
	if !report.Complete || len(report.Fixtures) != 1 || report.Fixtures[0].PreflightInputTokens != 17 || report.Fixtures[0].RuntimeInputTokens != 17 {
		t.Fatalf("runtime calibration report = %+v", report)
	}

	drifting := func(_ context.Context, _, _ string) (string, provider.Usage, error) {
		return "ok", provider.Usage{InputTokens: 18}, nil
	}
	if _, err := calibrateEvalTokenCounterAgainstRuntime(context.Background(), counter, drifting, []evalTokenCalibrationFixture{fixture}, "sha256:counter"); err == nil {
		t.Fatal("runtime input-token drift unexpectedly accepted")
	}
}
