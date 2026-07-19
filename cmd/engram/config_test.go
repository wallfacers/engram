package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoadConfigFlagsOverrideEnvironment(t *testing.T) {
	env := map[string]string{
		"ENGRAM_DATA_DIR":       "/env/data",
		"ENGRAM_NAMESPACE":      "from-env",
		"ENGRAM_EMBED_BASE_URL": "http://embed.env",
		"ENGRAM_EMBED_MODEL":    "embed-env",
		"ENGRAM_EMBED_API_KEY":  "embed-secret",
		"ENGRAM_LLM_BASE_URL":   "http://llm.env",
		"ENGRAM_LLM_MODEL":      "llm-env",
		"ENGRAM_LLM_PROVIDER":   "openai",
		"ENGRAM_LLM_API_KEY":    "llm-secret",
	}
	config, rest, err := loadConfig([]string{
		"--data-dir", "/flag/data",
		"--namespace", "from-flag",
		"--embed-base-url", "http://embed.flag",
		"--embed-model", "embed-flag",
		"--llm-base-url", "http://llm.flag",
		"--llm-model", "llm-flag",
		"--llm-provider", "anthropic",
		"search", "query",
	}, envLookup(env))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got, want := config.DataDir, "/flag/data"; got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
	if got, want := config.Namespace, "from-flag"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}
	if got, want := config.EmbedBaseURL, "http://embed.flag"; got != want {
		t.Errorf("EmbedBaseURL = %q, want %q", got, want)
	}
	if got, want := config.EmbedModel, "embed-flag"; got != want {
		t.Errorf("EmbedModel = %q, want %q", got, want)
	}
	if got, want := config.LLMBaseURL, "http://llm.flag"; got != want {
		t.Errorf("LLMBaseURL = %q, want %q", got, want)
	}
	if got, want := config.LLMModel, "llm-flag"; got != want {
		t.Errorf("LLMModel = %q, want %q", got, want)
	}
	if got, want := config.LLMProvider, "anthropic"; got != want {
		t.Errorf("LLMProvider = %q, want %q", got, want)
	}
	if got, want := config.EmbedAPIKey, "embed-secret"; got != want {
		t.Errorf("EmbedAPIKey = %q, want env value", got)
	}
	if got, want := config.LLMAPIKey, "llm-secret"; got != want {
		t.Errorf("LLMAPIKey = %q, want env value", got)
	}
	if got, want := strings.Join(rest, " "), "search query"; got != want {
		t.Errorf("remaining args = %q, want %q", got, want)
	}
}

func TestRunRequiresDataDirWithoutLeakingKeys(t *testing.T) {
	t.Setenv("ENGRAM_EMBED_API_KEY", "embed-secret")
	t.Setenv("ENGRAM_LLM_API_KEY", "llm-secret")

	var stdout, stderr bytes.Buffer
	if got := run([]string{"search", "anything"}, strings.NewReader(""), &stdout, &stderr); got != exitUsage {
		t.Fatalf("run exit code = %d, want %d", got, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "data directory is required") || !strings.Contains(got, "--data-dir") {
		t.Errorf("stderr = %q, want data-dir diagnostic", got)
	}
	for _, secret := range []string{"embed-secret", "llm-secret"} {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Errorf("secret %q leaked into command output", secret)
		}
	}
}

func envLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
