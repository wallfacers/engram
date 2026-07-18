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
	registry, err := mcpserver.NewRegistry(ctx, mcpserver.RegistryConfig{
		DataDir:           config.DataDir,
		EmbClient:         embClient,
		LLMCaller:         llmCaller,
		MaxOpenNamespaces: config.MaxOpenNamespaces,
	})
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
		"max_open_namespaces", config.MaxOpenNamespaces,
	)
	if err := mcpserver.Run(ctx, registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
