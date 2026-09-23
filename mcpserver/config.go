package mcpserver

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wallfacers/engram/filter"
)

const (
	defaultMaxOpenNamespaces = 64
	// maxCandidatePool is the honest upper bound on an explicitly requested RRF
	// candidate pool. Evaluation pools top out at 150; a larger implicit pool is
	// refused rather than silently grown (constitution V, honest scale).
	maxCandidatePool = 500
)

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
	CurationEnabled   bool

	// Jev is the optional relevance filter and write gate (051). All three of
	// BaseURL, Model and APIKey must be set for the filter to be usable; the key
	// is read only from the environment.
	JevBaseURL    string
	JevModel      string
	JevAPIKey     string
	JevTheta      float64
	JevRelaxTheta float64
	JevKShowMax   int
	// JevRelax is the ENGRAM_JEV_RELAX kill-switch: false disables the relax
	// stage entirely, so "nothing above theta" stays an honest empty result.
	// It defaults to true.
	JevRelax bool
	// JevWriteGate is opt-in (default off): while off, memory_write is ungated.
	JevWriteGate bool
	// SearchPool and SearchFilter are server-side defaults for memory_search's
	// candidate_pool and filter parameters. MCP clients cannot set them, which is
	// what keeps the parity invariant scoped to "knobs unset".
	SearchPool   int
	SearchFilter string
}

// Config is kept as a short name for callers that construct a server directly.
type Config = ServerConfig

// SearchPolicy returns the engine threshold policy described by the Jev
// configuration. RelaxTheta > Theta is clamped to Theta as contracted, and the
// relax stage is disabled when ENGRAM_JEV_RELAX is off.
func (c ServerConfig) SearchPolicy() filter.Policy {
	policy := filter.Policy{
		Theta:         c.JevTheta,
		RelaxTheta:    c.JevRelaxTheta,
		RelaxMax:      filter.DefaultPolicy().RelaxMax,
		KShowMax:      c.JevKShowMax,
		RelaxDisabled: !c.JevRelax,
	}
	if policy.RelaxTheta > policy.Theta {
		policy.RelaxTheta = policy.Theta
	}
	return policy
}

func applyEnvJevDefaults(defaults *ServerConfig, getenv func(string) string) error {
	for _, item := range []struct {
		name string
		dst  *float64
	}{
		{"ENGRAM_JEV_THETA", &defaults.JevTheta},
		{"ENGRAM_JEV_RELAX_THETA", &defaults.JevRelaxTheta},
	} {
		raw := strings.TrimSpace(getenv(item.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 || value >= 1 {
			return fmt.Errorf("parse %s: want a number in (0,1), got %q", item.name, raw)
		}
		*item.dst = value
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_KSHOW_MAX")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("parse ENGRAM_JEV_KSHOW_MAX: want a positive integer, got %q", raw)
		}
		defaults.JevKShowMax = n
	}
	for _, item := range []struct {
		name string
		dst  *bool
	}{
		{"ENGRAM_JEV_RELAX", &defaults.JevRelax},
		{"ENGRAM_JEV_WRITE_GATE", &defaults.JevWriteGate},
	} {
		raw := strings.TrimSpace(getenv(item.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("parse %s: want a boolean, got %q", item.name, raw)
		}
		*item.dst = value
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_SEARCH_POOL")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("parse ENGRAM_SEARCH_POOL: want a positive integer, got %q", raw)
		}
		defaults.SearchPool = min(n, maxCandidatePool)
	}
	if raw := strings.ToLower(strings.TrimSpace(getenv("ENGRAM_FILTER"))); raw != "" {
		if raw != filter.BackendNone && raw != filter.BackendJev {
			return fmt.Errorf("parse ENGRAM_FILTER: want %q or %q, got %q", filter.BackendNone, filter.BackendJev, raw)
		}
		defaults.SearchFilter = raw
	}
	return nil
}

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
		JevBaseURL:        strings.TrimSpace(getenv("ENGRAM_JEV_BASE_URL")),
		JevModel:          strings.TrimSpace(getenv("ENGRAM_JEV_MODEL")),
		JevAPIKey:         getenv("ENGRAM_JEV_API_KEY"),
		JevTheta:          filter.DefaultPolicy().Theta,
		JevRelaxTheta:     filter.DefaultPolicy().RelaxTheta,
		JevKShowMax:       filter.DefaultPolicy().KShowMax,
		JevRelax:          true,
	}
	if err := applyEnvJevDefaults(&defaults, getenv); err != nil {
		return ServerConfig{}, err
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_CURATION_ENABLED")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return ServerConfig{}, fmt.Errorf("parse ENGRAM_CURATION_ENABLED: %w", err)
		}
		defaults.CurationEnabled = enabled
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
	curationEnabled := fs.Bool("curation-enabled", defaults.CurationEnabled, "enable persistent asynchronous memory curation")
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
		CurationEnabled:   *curationEnabled,

		JevBaseURL:    defaults.JevBaseURL,
		JevModel:      defaults.JevModel,
		JevAPIKey:     defaults.JevAPIKey,
		JevTheta:      defaults.JevTheta,
		JevRelaxTheta: defaults.JevRelaxTheta,
		JevKShowMax:   defaults.JevKShowMax,
		JevRelax:      defaults.JevRelax,
		JevWriteGate:  defaults.JevWriteGate,
		SearchPool:    defaults.SearchPool,
		SearchFilter:  defaults.SearchFilter,
	}
	if config.DataDir == "" {
		return ServerConfig{}, errors.New("data directory is required (use --data-dir or ENGRAM_DATA_DIR)")
	}
	if config.MaxOpenNamespaces <= 0 {
		return ServerConfig{}, errors.New("max open namespaces must be greater than zero")
	}
	return config, nil
}
