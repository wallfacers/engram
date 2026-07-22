package main

import (
	"strings"
	"testing"
)

func TestResolveJudgeConfigFallbackMatrix(t *testing.T) {
	base := map[string]string{
		"LOCOMO_PROVIDER": "openai",
		"LOCOMO_BASE_URL": "http://127.0.0.1:8000/v1",
		"LOCOMO_API_KEY":  "answer-key",
		"LOCOMO_MODEL":    "answer-model",
	}

	tests := []struct {
		name  string
		judge map[string]string
		want  judgeConfig
	}{
		{
			name: "all empty falls back item by item",
			want: judgeConfig{
				Provider: "openai",
				BaseURL:  "http://127.0.0.1:8000/v1",
				APIKey:   "answer-key",
				Model:    "answer-model",
			},
		},
		{
			name:  "model only",
			judge: map[string]string{"JUDGE_MODEL": "judge-model"},
			want: judgeConfig{
				Provider: "openai",
				BaseURL:  "http://127.0.0.1:8000/v1",
				APIKey:   "answer-key",
				Model:    "judge-model",
			},
		},
		{
			name:  "base url only",
			judge: map[string]string{"JUDGE_BASE_URL": "https://judge.example/v1"},
			want: judgeConfig{
				Provider: "openai",
				BaseURL:  "https://judge.example/v1",
				APIKey:   "answer-key",
				Model:    "answer-model",
			},
		},
		{
			name:  "provider only",
			judge: map[string]string{"JUDGE_PROVIDER": "anthropic"},
			want: judgeConfig{
				Provider: "anthropic",
				BaseURL:  "http://127.0.0.1:8000/v1",
				APIKey:   "answer-key",
				Model:    "answer-model",
			},
		},
		{
			name: "all judge settings",
			judge: map[string]string{
				"JUDGE_PROVIDER": "anthropic",
				"JUDGE_BASE_URL": "https://judge.example/anthropic",
				"JUDGE_API_KEY":  "judge-key",
				"JUDGE_MODEL":    "deepseek-v4-flash",
			},
			want: judgeConfig{
				Provider: "anthropic",
				BaseURL:  "https://judge.example/anthropic",
				APIKey:   "judge-key",
				Model:    "deepseek-v4-flash",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if value, ok := tt.judge[key]; ok {
					return value
				}
				return base[key]
			}
			if got := resolveJudgeConfig(getenv); got != tt.want {
				t.Fatalf("resolveJudgeConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAnswerRegimeFingerprintSeparatesJudgeModel(t *testing.T) {
	const answerModel = "local-vllm-model"

	withDifferentJudge := answerRegimeFingerprint(options{
		answerModel: answerModel,
		judgeModel:  "deepseek-v4-flash",
	})
	wantDifferent := "force_answer=false;abstain_prompt=false;no_idk_retry=false;judge_model=deepseek-v4-flash"
	if withDifferentJudge != wantDifferent {
		t.Fatalf("different judge model fingerprint = %q, want %q", withDifferentJudge, wantDifferent)
	}

	withSameJudge := answerRegimeFingerprint(options{
		answerModel: answerModel,
		judgeModel:  answerModel,
	})
	if strings.Contains(withSameJudge, "judge_model=") {
		t.Fatalf("same judge model fingerprint = %q, must omit judge model segment", withSameJudge)
	}
}
