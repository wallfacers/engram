package main

import (
	"errors"
	"flag"
	"io"
	"strings"
)

var errDataDirRequired = errors.New("data directory is required")

const defaultNamespace = "default"

type Config struct {
	DataDir      string
	Namespace    string
	EmbedBaseURL string
	EmbedModel   string
	EmbedAPIKey  string
	LLMBaseURL   string
	LLMModel     string
	LLMProvider  string
	LLMAPIKey    string
}

func loadConfig(args []string, getenv func(string) string) (Config, []string, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	fs := flag.NewFlagSet("engram", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", getenv("ENGRAM_DATA_DIR"), "directory for engram data")
	namespace := fs.String("namespace", defaultNamespace, "memory namespace")
	if raw := getenv("ENGRAM_NAMESPACE"); raw != "" {
		*namespace = raw
	}
	embedBaseURL := fs.String("embed-base-url", getenv("ENGRAM_EMBED_BASE_URL"), "embedding endpoint")
	embedModel := fs.String("embed-model", getenv("ENGRAM_EMBED_MODEL"), "embedding model")
	llmBaseURL := fs.String("llm-base-url", getenv("ENGRAM_LLM_BASE_URL"), "LLM endpoint")
	llmModel := fs.String("llm-model", getenv("ENGRAM_LLM_MODEL"), "LLM model")
	llmProvider := fs.String("llm-provider", getenv("ENGRAM_LLM_PROVIDER"), "LLM provider")
	if err := fs.Parse(args); err != nil {
		return Config{}, nil, err
	}

	config := Config{
		DataDir:      strings.TrimSpace(*dataDir),
		Namespace:    strings.TrimSpace(*namespace),
		EmbedBaseURL: strings.TrimSpace(*embedBaseURL),
		EmbedModel:   strings.TrimSpace(*embedModel),
		EmbedAPIKey:  getenv("ENGRAM_EMBED_API_KEY"),
		LLMBaseURL:   strings.TrimSpace(*llmBaseURL),
		LLMModel:     strings.TrimSpace(*llmModel),
		LLMProvider:  strings.TrimSpace(*llmProvider),
		LLMAPIKey:    getenv("ENGRAM_LLM_API_KEY"),
	}
	// data-dir is required by every command that opens a store, but not by
	// `version` (a build-probe). Enforcement therefore lives in run(), after the
	// command is known, not here — so `engram version` works with no data dir.
	return config, fs.Args(), nil
}
