package main

import (
	"strings"
	"testing"
)

func TestValidateUnifiedAnswerContractProbeMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opt       options
		wantMode  bool
		wantError string
	}{
		{name: "inactive", opt: options{}, wantMode: false},
		{
			name:      "fixture requires output",
			opt:       options{unifiedProbeFixture: "cases.json", unifiedProbeRepeats: 3, maxTokens: 32},
			wantMode:  true,
			wantError: "--unified-answer-probe-out is required",
		},
		{
			name:      "output requires fixture",
			opt:       options{unifiedProbeOut: "report.json", unifiedProbeRepeats: 3, maxTokens: 32},
			wantMode:  true,
			wantError: "--unified-answer-probe is required",
		},
		{
			name: "repeats must be positive and odd",
			opt: options{
				unifiedProbeFixture: "cases.json",
				unifiedProbeOut:     "report.json",
				unifiedProbeRepeats: 2,
				maxTokens:           32,
			},
			wantMode:  true,
			wantError: "positive odd number",
		},
		{
			name: "benchmark mode cannot be mixed in",
			opt: options{
				unifiedProbeFixture: "cases.json",
				unifiedProbeOut:     "report.json",
				unifiedProbeRepeats: 3,
				maxTokens:           32,
				dataPath:            "locomo.json",
			},
			wantMode:  true,
			wantError: "dedicated mode",
		},
		{
			name: "explicit ordinary flags cannot be silently ignored",
			opt: options{
				unifiedProbeFixture: "cases.json",
				unifiedProbeOut:     "report.json",
				unifiedProbeRepeats: 3,
				maxTokens:           32,
				explicitFlags:       map[string]bool{"unified-answer-probe": true, "unified-answer-probe-out": true, "force-answer": true},
			},
			wantMode:  true,
			wantError: "--force-answer",
		},
		{
			name: "valid",
			opt: options{
				unifiedProbeFixture: "cases.json",
				unifiedProbeOut:     "report.json",
				unifiedProbeRepeats: 3,
				maxTokens:           32,
			},
			wantMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMode, err := validateUnifiedAnswerContractProbeMode(tt.opt)
			if gotMode != tt.wantMode {
				t.Fatalf("mode = %t, want %t", gotMode, tt.wantMode)
			}
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestResolveUnifiedAnswerContractProbeRuntimeConfig(t *testing.T) {
	t.Parallel()

	t.Run("answer settings and independent judge overrides are frozen", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"LOCOMO_PROVIDER":       "openai",
			"LOCOMO_BASE_URL":       "http://answer.invalid/v1",
			"LOCOMO_API_KEY":        "answer-secret",
			"LOCOMO_MODEL":          "answer-model",
			"LOCOMO_MODEL_REVISION": "answer-revision",
			"JUDGE_PROVIDER":        "anthropic",
			"JUDGE_BASE_URL":        "http://judge.invalid/anthropic",
			"JUDGE_API_KEY":         "judge-secret",
			"JUDGE_MODEL":           "judge-model",
			"JUDGE_MODEL_REVISION":  "judge-revision",
		}
		got, err := resolveUnifiedAnswerContractProbeRuntimeConfig(func(key string) string { return env[key] })
		if err != nil {
			t.Fatalf("resolve config: %v", err)
		}
		if got.answerProvider != "openai" || got.answerBaseURL != env["LOCOMO_BASE_URL"] || got.answerAPIKey != env["LOCOMO_API_KEY"] {
			t.Fatal("answer endpoint config mismatch")
		}
		if got.answerMetadata != (unifiedAnswerContractProbeModelMetadata{Provider: "openai", Model: "answer-model", Revision: "answer-revision"}) {
			t.Fatalf("answer metadata = %+v", got.answerMetadata)
		}
		if got.judge != (judgeConfig{Provider: "anthropic", BaseURL: env["JUDGE_BASE_URL"], APIKey: env["JUDGE_API_KEY"], Model: "judge-model"}) {
			t.Fatal("judge endpoint config mismatch")
		}
		if got.judgeMetadata != (unifiedAnswerContractProbeModelMetadata{Provider: "anthropic", Model: "judge-model", Revision: "judge-revision"}) {
			t.Fatalf("judge metadata = %+v", got.judgeMetadata)
		}
	})

	t.Run("judge fields fall back independently and missing revisions are marked unverified", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"LOCOMO_PROVIDER": "openai",
			"LOCOMO_BASE_URL": "http://local.invalid/v1",
			"LOCOMO_API_KEY":  "local-secret",
			"LOCOMO_MODEL":    "local-model",
			"JUDGE_MODEL":     "judge-only-model",
		}
		got, err := resolveUnifiedAnswerContractProbeRuntimeConfig(func(key string) string { return env[key] })
		if err != nil {
			t.Fatalf("resolve config: %v", err)
		}
		if got.answerMetadata.Revision != "unverified:local-model" {
			t.Fatalf("answer revision = %q", got.answerMetadata.Revision)
		}
		if got.judge.Provider != "openai" || got.judge.BaseURL != env["LOCOMO_BASE_URL"] || got.judge.APIKey != env["LOCOMO_API_KEY"] {
			t.Fatal("judge fallback mismatch")
		}
		if got.judgeMetadata.Model != "judge-only-model" || got.judgeMetadata.Revision != "unverified:judge-only-model" {
			t.Fatalf("judge metadata = %+v", got.judgeMetadata)
		}
	})

	t.Run("missing key fails without echoing any other environment value", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"LOCOMO_BASE_URL": "https://user:password@example.invalid/v1",
		}
		_, err := resolveUnifiedAnswerContractProbeRuntimeConfig(func(key string) string { return env[key] })
		if err == nil || !strings.Contains(err.Error(), "LOCOMO_API_KEY is required") {
			t.Fatalf("error = %v", err)
		}
		if strings.Contains(err.Error(), env["LOCOMO_BASE_URL"]) {
			t.Fatalf("error leaks unrelated environment value: %v", err)
		}
	})
}
