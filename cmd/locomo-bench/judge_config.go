package main

import "net/url"

const (
	defaultLoCoMoProvider = "anthropic"
	defaultLoCoMoBaseURL  = "https://api.deepseek.com/anthropic"
	defaultLoCoMoModel    = "deepseek-v4-pro"
)

type judgeConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

// resolveJudgeConfig keeps each judge override independent: an empty JUDGE_*
// value falls back to its corresponding answer-side LOCOMO_* value.
func resolveJudgeConfig(getenv func(string) string) judgeConfig {
	return judgeConfig{
		Provider: envOverride(getenv, "JUDGE_PROVIDER", "LOCOMO_PROVIDER", defaultLoCoMoProvider),
		BaseURL:  envOverride(getenv, "JUDGE_BASE_URL", "LOCOMO_BASE_URL", defaultLoCoMoBaseURL),
		APIKey:   envOverride(getenv, "JUDGE_API_KEY", "LOCOMO_API_KEY", ""),
		Model:    envOverride(getenv, "JUDGE_MODEL", "LOCOMO_MODEL", defaultLoCoMoModel),
	}
}

func envOverride(getenv func(string) string, overrideKey, fallbackKey, defaultValue string) string {
	if value := getenv(overrideKey); value != "" {
		return value
	}
	if value := getenv(fallbackKey); value != "" {
		return value
	}
	return defaultValue
}

// baseURLHost returns only the endpoint host for startup diagnostics. In
// particular, it never falls back to logging the complete URL or credentials.
func baseURLHost(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}
