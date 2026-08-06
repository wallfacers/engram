package main

// 029 navigation decide caller. The sidecar (Qwen3.6-35B) is a reasoning model:
// with thinking enabled it emits chain-of-thought before the tool JSON
// (observed ~13s/step and interleaved text, so the answer path strips it), and
// reasoning dominates the per-step latency. For the navigation loop — where a
// fast, exact JSON tool call is all we need — this caller talks to the same
// vLLM OpenAI-compatible endpoint with `chat_template_kwargs.enable_thinking:
// false` (observed 0.6s/step, pure JSON). It is harness-side only and never
// touches provider/ (engine untouchable, FR-003).
//
// The answer phase keeps the provider.Provider path (thinking on); only the
// navigation agent's decide calls run through here.

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

const navDecideMaxTokens = 512

type navDecideConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxTokens  int
	HTTPClient *http.Client
}

// newNavDecideCaller builds a usageModelCaller that calls the vLLM OpenAI
// endpoint non-streaming with thinking disabled. The caller timeout is the
// passed ctx (the navigation loop wraps each step in navConfig.Timeout), so the
// caller itself does not add its own deadline.
func newNavDecideCaller(cfg navDecideConfig) (usageModelCaller, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("nav decide caller requires base URL and model")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = navDecideMaxTokens
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
			"chat_template_kwargs": map[string]bool{"enable_thinking": false},
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("nav decide encode: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("nav decide request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("nav decide call: %w", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", provider.Usage{}, fmt.Errorf("nav decide read: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(raw))
			if len(detail) > 300 {
				detail = detail[:300]
			}
			return "", provider.Usage{}, fmt.Errorf("nav decide status %d: %s", resp.StatusCode, detail)
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
			return "", provider.Usage{}, fmt.Errorf("nav decide decode: %w", err)
		}
		if len(out.Choices) == 0 {
			return "", provider.Usage{}, fmt.Errorf("nav decide: empty choices")
		}
		usage := provider.Usage{}
		if out.Usage != nil {
			usage.InputTokens = out.Usage.PromptTokens
			usage.OutputTokens = out.Usage.CompletionTokens
		}
		return out.Choices[0].Message.Content, usage, nil
	}, nil
}
