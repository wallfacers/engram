package mcpserver

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultMaxOpenNamespaces = 64

// ServerConfig contains the adapter's startup configuration. API keys are
// intentionally not accepted as command-line flags; callers must provide them
// through environment variables.
type ServerConfig struct {
	DataDir string

	EmbedBaseURL string
	EmbedModel   string
	EmbedAPIKey  string

	LLMBaseURL  string
	LLMModel    string
	LLMAPIKey   string
	LLMProvider string

	MaxOpenNamespaces int
}

// Config is kept as a short name for callers that construct a server directly.
type Config = ServerConfig

// LoadConfig loads configuration from flags and ENGRAM_* environment variables.
// Non-secret flags override their environment defaults. Secret values are read
// only from the environment.
func LoadConfig(args []string) (ServerConfig, error) {
	return LoadConfigWithEnv(args, os.Getenv)
}

// LoadConfigWithEnv is LoadConfig with an injectable environment reader for
// deterministic tests.
func LoadConfigWithEnv(args []string, getenv func(string) string) (ServerConfig, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	defaults := ServerConfig{
		DataDir:           getenv("ENGRAM_DATA_DIR"),
		EmbedBaseURL:      getenv("ENGRAM_EMBED_BASE_URL"),
		EmbedModel:        getenv("ENGRAM_EMBED_MODEL"),
		EmbedAPIKey:       getenv("ENGRAM_EMBED_API_KEY"),
		LLMBaseURL:        getenv("ENGRAM_LLM_BASE_URL"),
		LLMModel:          getenv("ENGRAM_LLM_MODEL"),
		LLMAPIKey:         getenv("ENGRAM_LLM_API_KEY"),
		LLMProvider:       getenv("ENGRAM_LLM_PROVIDER"),
		MaxOpenNamespaces: defaultMaxOpenNamespaces,
	}
	if raw := getenv("ENGRAM_MAX_OPEN_NAMESPACES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return ServerConfig{}, fmt.Errorf("parse ENGRAM_MAX_OPEN_NAMESPACES: %w", err)
		}
		defaults.MaxOpenNamespaces = n
	}

	fs := flag.NewFlagSet("engram-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", defaults.DataDir, "directory for engram data")
	embedBaseURL := fs.String("embed-base-url", defaults.EmbedBaseURL, "OpenAI-compatible embedding endpoint")
	embedModel := fs.String("embed-model", defaults.EmbedModel, "embedding model")
	llmBaseURL := fs.String("llm-base-url", defaults.LLMBaseURL, "LLM endpoint")
	llmModel := fs.String("llm-model", defaults.LLMModel, "LLM model")
	llmProvider := fs.String("llm-provider", defaults.LLMProvider, "LLM provider name")
	maxOpen := fs.Int("max-open-namespaces", defaults.MaxOpenNamespaces, "maximum cached namespaces")
	if err := fs.Parse(args); err != nil {
		return ServerConfig{}, err
	}

	config := ServerConfig{
		DataDir:           strings.TrimSpace(*dataDir),
		EmbedBaseURL:      strings.TrimSpace(*embedBaseURL),
		EmbedModel:        strings.TrimSpace(*embedModel),
		EmbedAPIKey:       defaults.EmbedAPIKey,
		LLMBaseURL:        strings.TrimSpace(*llmBaseURL),
		LLMModel:          strings.TrimSpace(*llmModel),
		LLMAPIKey:         defaults.LLMAPIKey,
		LLMProvider:       strings.TrimSpace(*llmProvider),
		MaxOpenNamespaces: *maxOpen,
	}
	if config.DataDir == "" {
		return ServerConfig{}, errors.New("data directory is required (use --data-dir or ENGRAM_DATA_DIR)")
	}
	if config.MaxOpenNamespaces <= 0 {
		return ServerConfig{}, errors.New("max open namespaces must be greater than zero")
	}
	return config, nil
}
