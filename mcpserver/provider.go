package mcpserver

import (
	"fmt"
	"strings"

	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/anthropic"
	"github.com/wallfacers/engram/provider/openai"
)

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
