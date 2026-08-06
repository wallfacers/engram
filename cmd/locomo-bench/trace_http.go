package main

// 030 US2 trace sidecar caller (specs/030, contracts/grounded-trace.md).
// Generates the grounded-trace packet from a strong local sidecar (e.g.
// DeepSeek-flash), reusing the 029 harness-side vLLM OpenAI-compatible caller
// pattern. The caller is opt-in (default off): without it, the legacy answer
// path is byte-identical (SC-004). The packet is a single JSON object
// {plan, trace, actions, evidence}; the fail-closed gate (trace_gate.go) then
// validates it against the closed candidate boundary. Engine untouched
// (FR-001).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wallfacers/engram/provider"
)

const traceSidecarMaxTokens = 2048

// traceSidecarConfig mirrors navDecideConfig for the 030 trace mediator.
type traceSidecarConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxTokens  int
	HTTPClient *http.Client
}

// newTraceSidecarCaller builds a usageModelCaller that asks the sidecar for one
// trace packet (temperature 0, non-streaming). The caller timeout is the passed
// ctx; the mediator wraps the call with a step timeout.
func newTraceSidecarCaller(cfg traceSidecarConfig) (usageModelCaller, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("trace sidecar caller requires base URL and model")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = traceSidecarMaxTokens
	}
	client := cfg.HTTPClient
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	apiKey := cfg.APIKey
	model := cfg.Model

	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		body := map[string]any{
			"model":     model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
			"max_tokens":          maxTokens,
			"temperature":         0,
			// chat_template_kwargs is the vLLM toggle; thinking is the OpenAI-
			// compatible toggle DeepSeek honours. Unknown fields are ignored,
			// so sending both is safe across both sidecars.
			"chat_template_kwargs": map[string]bool{"enable_thinking": false},
			"thinking":             map[string]string{"type": "disabled"},
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar encode: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar call: %w", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar read: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(raw))
			if len(detail) > 300 {
				detail = detail[:300]
			}
			return "", provider.Usage{}, fmt.Errorf("trace sidecar status %d: %s", resp.StatusCode, detail)
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar decode: %w", err)
		}
		if len(out.Choices) == 0 {
			return "", provider.Usage{}, fmt.Errorf("trace sidecar: empty choices")
		}
		usage := provider.Usage{}
		if out.Usage != nil {
			usage.InputTokens = out.Usage.PromptTokens
			usage.OutputTokens = out.Usage.CompletionTokens
		}
		return out.Choices[0].Message.Content, usage, nil
	}, nil
}
