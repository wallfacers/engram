package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/anthropic"
	"github.com/wallfacers/engram/provider/openai"
	"github.com/wallfacers/engram/store"
)

type engineHandle struct {
	store     *store.Store
	entries   *memory.EntryStore
	vectors   *memory.VectorStore
	embedder  *memory.Embedder
	retriever *memory.Retriever
	pipe      *pipeline.Pipeline
	embClient embedding.Client
}

func openEngine(ctx context.Context, config Config) (*engineHandle, error) {
	embClient, err := buildEmbeddingClient(config)
	if err != nil {
		return nil, err
	}
	llmCaller, err := buildLLMCaller(config)
	if err != nil {
		return nil, err
	}
	return openEngineWith(ctx, config, embClient, llmCaller)
}

func openEngineWith(ctx context.Context, config Config, embClient embedding.Client, llmCaller pipeline.ModelCaller) (*engineHandle, error) {
	path, err := namespaceDatabasePath(config.DataDir, config.Namespace)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		return nil, fmt.Errorf("open namespace %q: %w", config.Namespace, err)
	}
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(entries, vectors, embClient, memory.DefaultEmbedBuffer)
	return &engineHandle{
		store:     st,
		entries:   entries,
		vectors:   vectors,
		embedder:  embedder,
		retriever: memory.NewRetriever(entries, vectors, embClient),
		pipe: pipeline.New(pipeline.Config{
			Entries:  entries,
			Embedder: embedder,
			Call:     llmCaller,
			Budgets:  memory.DefaultBudgets(),
		}),
		embClient: embClient,
	}, nil
}

func (h *engineHandle) Close() error {
	if h == nil {
		return nil
	}
	if h.embedder != nil {
		h.embedder.Close()
	}
	if h.store == nil {
		return nil
	}
	return h.store.Close()
}

func buildEmbeddingClient(config Config) (embedding.Client, error) {
	client, err := embedding.New(embedding.Config{
		BaseURL: config.EmbedBaseURL,
		Model:   config.EmbedModel,
		APIKey:  config.EmbedAPIKey,
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}
	return client, nil
}

func buildLLMCaller(config Config) (pipeline.ModelCaller, error) {
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
		llmProvider = openai.New(openai.Options{APIKey: config.LLMAPIKey, BaseURL: config.LLMBaseURL})
	case "anthropic":
		llmProvider = anthropic.New(anthropic.Options{APIKey: config.LLMAPIKey, BaseURL: config.LLMBaseURL})
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
