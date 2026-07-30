package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// vllmTokenCounter calls vLLM's chat-aware /tokenize endpoint. It uses the
// same system+user message shape as the OpenAI provider adapter and asks vLLM
// to include the generation prompt, so the preflight count includes chat
// template boundary tokens rather than only evidence text.
type vllmTokenCounter struct {
	baseURL     string
	apiKey      string
	fingerprint string
	httpClient  *http.Client
}

type vllmTokenCounterConfig struct {
	BaseURL     string
	APIKey      string
	Fingerprint string
	HTTPClient  *http.Client
}

// gateTokenCounter makes token-preflight requests share the benchmark's
// global remote-call limit. Formal packing counts a complete prompt for every
// candidate, so leaving it outside the answer/judge gate can otherwise create
// thousands of concurrent /tokenize calls before any answer admission occurs.
// Waiting respects the caller context and never substitutes an estimate.
func gateTokenCounter(sem chan struct{}, counter evidencecompiler.TokenCounter) evidencecompiler.TokenCounter {
	if counter == nil || sem == nil {
		return counter
	}
	return gatedTokenCounter{sem: sem, counter: counter}
}

type gatedTokenCounter struct {
	sem     chan struct{}
	counter evidencecompiler.TokenCounter
}

func (counter gatedTokenCounter) CountInput(ctx context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	select {
	case counter.sem <- struct{}{}:
		defer func() { <-counter.sem }()
	case <-ctx.Done():
		return evidencecompiler.TokenCount{}, fmt.Errorf("formal token counter admission: %w", ctx.Err())
	}
	return counter.counter.CountInput(ctx, input)
}

type vllmChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type vllmTokenizeRequest struct {
	Model               string            `json:"model"`
	Messages            []vllmChatMessage `json:"messages"`
	AddGenerationPrompt bool              `json:"add_generation_prompt"`
}

type vllmTokenizeResponse struct {
	Count  int               `json:"count"`
	Tokens []json.RawMessage `json:"tokens"`
}

func newVLLMTokenCounter(config vllmTokenCounterConfig) (*vllmTokenCounter, error) {
	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.Fingerprint) == "" {
		return nil, fmt.Errorf("vLLM token counter requires base URL and fingerprint")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	return &vllmTokenCounter{
		baseURL:     strings.TrimRight(config.BaseURL, "/"),
		apiKey:      config.APIKey,
		fingerprint: config.Fingerprint,
		httpClient:  config.HTTPClient,
	}, nil
}

func (counter *vllmTokenCounter) CountInput(ctx context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if counter == nil || strings.TrimSpace(input.Model) == "" {
		return evidencecompiler.TokenCount{}, fmt.Errorf("vLLM token count requires a configured counter and answerer model")
	}
	body, err := json.Marshal(vllmTokenizeRequest{
		Model: input.Model,
		Messages: []vllmChatMessage{
			{Role: "system", Content: input.System},
			{Role: "user", Content: input.User},
		},
		AddGenerationPrompt: true,
	})
	if err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("encode vLLM token request: %w", err)
	}
	// vLLM exposes tokenization helpers at the server root, while its OpenAI
	// chat-completions endpoint conventionally lives under /v1. Accept the
	// latter base URL because it is shared with the answer provider, then strip
	// only that terminal API prefix for the tokenizer call.
	tokenizeBase := strings.TrimSuffix(counter.baseURL, "/v1")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenizeBase+"/tokenize", bytes.NewReader(body))
	if err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("build vLLM token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if counter.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+counter.apiKey)
	}
	response, err := counter.httpClient.Do(request)
	if err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("vLLM token request: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return evidencecompiler.TokenCount{}, fmt.Errorf("vLLM token request failed with status %d", response.StatusCode)
	}
	var decoded vllmTokenizeResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024)).Decode(&decoded); err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("decode vLLM token response: %w", err)
	}
	count := decoded.Count
	if count == 0 {
		count = len(decoded.Tokens)
	}
	if count < 1 {
		return evidencecompiler.TokenCount{}, fmt.Errorf("vLLM token response has no positive count")
	}
	return evidencecompiler.TokenCount{InputTokens: count, Fingerprint: counter.fingerprint}, nil
}

type evalTokenCalibrationFixture struct {
	Name            string
	Input           evidencecompiler.AnswerInput
	WantInputTokens int
}

type evalTokenCalibrationReport struct {
	Complete           bool
	MaxDelta           int
	CounterFingerprint string
}

type evalRuntimeTokenCalibrationFixture struct {
	Name                 string `json:"name"`
	PreflightInputTokens int    `json:"preflight_input_tokens"`
	RuntimeInputTokens   int    `json:"runtime_input_tokens"`
}

type evalRuntimeTokenCalibrationReport struct {
	Complete           bool                                 `json:"complete"`
	CounterFingerprint string                               `json:"counter_fingerprint"`
	Fixtures           []evalRuntimeTokenCalibrationFixture `json:"fixtures"`
}

// calibrateEvalTokenCounter checks counts on complete answer inputs, including
// system/user chat boundaries. It never derives a total by summing parts.
func calibrateEvalTokenCounter(ctx context.Context, counter evidencecompiler.TokenCounter, fixtures []evalTokenCalibrationFixture, expectedFingerprint string) (evalTokenCalibrationReport, error) {
	if counter == nil || len(fixtures) == 0 || expectedFingerprint == "" {
		return evalTokenCalibrationReport{}, fmt.Errorf("counter, fixtures, and expected fingerprint are required")
	}
	report := evalTokenCalibrationReport{Complete: true, CounterFingerprint: expectedFingerprint}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.Name == "" || fixture.WantInputTokens < 1 || seen[fixture.Name] {
			return evalTokenCalibrationReport{}, fmt.Errorf("calibration fixtures require unique names and positive expected counts")
		}
		seen[fixture.Name] = true
		count, err := counter.CountInput(ctx, fixture.Input)
		if err != nil {
			return evalTokenCalibrationReport{}, fmt.Errorf("count calibration fixture %q: %w", fixture.Name, err)
		}
		if err := validateEvalTokenCount(count, math.MaxInt, expectedFingerprint); err != nil {
			return evalTokenCalibrationReport{}, fmt.Errorf("calibration fixture %q: %w", fixture.Name, err)
		}
		delta := count.InputTokens - fixture.WantInputTokens
		if delta < 0 {
			delta = -delta
		}
		if delta > report.MaxDelta {
			report.MaxDelta = delta
		}
	}
	if report.MaxDelta != 0 {
		return evalTokenCalibrationReport{}, fmt.Errorf("token counter calibration drift: max delta %d", report.MaxDelta)
	}
	return report, nil
}

// calibrateEvalTokenCounterAgainstRuntime proves that the preflight counter
// and the configured answerer agree on the full chat-template input. Unlike a
// self-recorded fixture, this fails if a server-side template or tokenizer
// revision changes between counting and generation.
func calibrateEvalTokenCounterAgainstRuntime(ctx context.Context, counter evidencecompiler.TokenCounter, answerCall usageModelCaller, fixtures []evalTokenCalibrationFixture, expectedFingerprint string) (evalRuntimeTokenCalibrationReport, error) {
	if counter == nil || answerCall == nil || len(fixtures) == 0 || expectedFingerprint == "" {
		return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("counter, answer caller, fixtures, and expected fingerprint are required")
	}
	report := evalRuntimeTokenCalibrationReport{Complete: true, CounterFingerprint: expectedFingerprint, Fixtures: make([]evalRuntimeTokenCalibrationFixture, 0, len(fixtures))}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.Name == "" || fixture.Input.Model == "" || seen[fixture.Name] {
			return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("runtime calibration fixtures require unique names and answerer models")
		}
		seen[fixture.Name] = true
		count, err := counter.CountInput(ctx, fixture.Input)
		if err != nil {
			return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("count runtime calibration fixture %q: %w", fixture.Name, err)
		}
		if err := validateEvalTokenCount(count, math.MaxInt, expectedFingerprint); err != nil {
			return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("validate runtime calibration fixture %q: %w", fixture.Name, err)
		}
		_, usage, err := answerCall(ctx, fixture.Input.System, fixture.Input.User)
		if err != nil {
			return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("answer runtime calibration fixture %q: %w", fixture.Name, err)
		}
		report.Fixtures = append(report.Fixtures, evalRuntimeTokenCalibrationFixture{
			Name:                 fixture.Name,
			PreflightInputTokens: count.InputTokens,
			RuntimeInputTokens:   usage.InputTokens,
		})
		if usage.InputTokens != count.InputTokens {
			return evalRuntimeTokenCalibrationReport{}, fmt.Errorf("runtime input-token drift for fixture %q: preflight=%d runtime=%d", fixture.Name, count.InputTokens, usage.InputTokens)
		}
	}
	return report, nil
}

func validateEvalTokenCount(count evidencecompiler.TokenCount, cap int, expectedFingerprint string) error {
	if count.InputTokens < 1 {
		return fmt.Errorf("token counter returned non-positive input tokens")
	}
	if count.InputTokens > cap {
		return fmt.Errorf("answer input tokens %d exceed cap %d", count.InputTokens, cap)
	}
	if count.Fingerprint == "" || count.Fingerprint != expectedFingerprint {
		return fmt.Errorf("token counter fingerprint drift")
	}
	return nil
}
