package mcpserver

import (
	"fmt"
	"strings"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/filter/jev"
	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/anthropic"
	"github.com/wallfacers/engram/provider/openai"
)

// BuildSearchFilter constructs the optional read-side Jev relevance filter from
// startup configuration. It returns an untyped nil when Jev is unconfigured
// (missing base URL, model or key), so the typed-nil from jev.New never leaks
// through the interface. The API key reaches the client only; it is never logged
// or serialized here.
func BuildSearchFilter(config ServerConfig) (filter.RelevanceFilter, error) {
	client, err := newJevClient(config)
	if err != nil || client == nil {
		return nil, err
	}
	return client, nil
}

// BuildWriteGate mirrors BuildSearchFilter for the write-side gate. When the
// gate is switched off the configuration still yields a usable gate; the
// adapter decides whether to consult it (ENGRAM_JEV_WRITE_GATE).
func BuildWriteGate(config ServerConfig) (filter.WriteGate, error) {
	client, err := newJevClient(config)
	if err != nil || client == nil {
		return nil, err
	}
	return client, nil
}

func newJevClient(config ServerConfig) (*jev.Client, error) {
	return jev.New(jev.Config{
		BaseURL: config.JevBaseURL,
		Model:   config.JevModel,
		APIKey:  config.JevAPIKey,
		Policy:  config.SearchPolicy(),
	})
}

// BuildLLMCaller constructs the optional extraction caller from startup
// configuration. An entirely empty LLM configuration keeps the server offline.
// API keys are passed through to the provider only; this function never logs or
// serializes them.
func BuildLLMCaller(config ServerConfig) (pipeline.ModelCaller, error) {
	providerName := strings.ToLower(strings.TrimSpace(config.LLMProvider))
	if providerName == "" {
		if strings.TrimSpace(config.LLMBaseURL) == "" && strings.TrimSpace(config.LLMModel) == "" && config.LLMAPIKey == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("LLM provider is required when LLM configuration is set")
	}
	if strings.TrimSpace(config.LLMModel) == "" {
		return nil, fmt.Errorf("LLM model is required for provider %q", providerName)
	}

	var llmProvider provider.Provider
	switch providerName {
	case "openai":
		llmProvider = openai.New(openai.Options{
			APIKey:  config.LLMAPIKey,
			BaseURL: config.LLMBaseURL,
		})
	case "anthropic":
		llmProvider = anthropic.New(anthropic.Options{
			APIKey:  config.LLMAPIKey,
			BaseURL: config.LLMBaseURL,
		})
	default:
		return nil, fmt.Errorf("unsupported LLM provider %q (want openai or anthropic)", providerName)
	}
	caller, err := curation.NewProviderCaller(
		map[string]provider.Provider{providerName: llmProvider},
		providerName+":"+config.LLMModel,
		4096,
	)
	return pipeline.ModelCaller(caller), err
}
