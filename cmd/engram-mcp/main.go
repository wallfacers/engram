package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/mcpserver"
	"github.com/wallfacers/engram/memory/pipeline"
)

func main() {
	config, err := mcpserver.LoadConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	embClient, err := buildEmbeddingClient(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	llmCaller, err := mcpserver.BuildLLMCaller(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx := context.Background()
	registry, err := mcpserver.NewRegistry(ctx, buildRegistryConfig(config, embClient, llmCaller))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer registry.Close() //nolint:errcheck
	slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).Info(
		"engram MCP server starting",
		"data_dir", config.DataDir,
		"embedding", embClient != nil,
		"memory_ingest", llmCaller != nil,
		"curation", config.CurationEnabled,
		"max_open_namespaces", config.MaxOpenNamespaces,
	)
	if err := mcpserver.Run(ctx, registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildRegistryConfig(config mcpserver.ServerConfig, embClient embedding.Client, llmCaller pipeline.ModelCaller) mcpserver.RegistryConfig {
	return mcpserver.RegistryConfig{
		DataDir:           config.DataDir,
		EmbClient:         embClient,
		LLMCaller:         llmCaller,
		MaxOpenNamespaces: config.MaxOpenNamespaces,
		CurationEnabled:   config.CurationEnabled,
	}
}

func buildEmbeddingClient(config mcpserver.ServerConfig) (embedding.Client, error) {
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
