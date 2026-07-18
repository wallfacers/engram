package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/mcpserver"
)

func main() {
	config, err := mcpserver.LoadConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	embClient, err := embedding.New(embedding.Config{
		BaseURL: config.EmbedBaseURL,
		Model:   config.EmbedModel,
		APIKey:  config.EmbedAPIKey,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx := context.Background()
	registry, err := mcpserver.NewRegistry(ctx, mcpserver.RegistryConfig{
		DataDir:           config.DataDir,
		EmbClient:         embClient,
		MaxOpenNamespaces: config.MaxOpenNamespaces,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer registry.Close() //nolint:errcheck
	if err := mcpserver.Run(ctx, registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
