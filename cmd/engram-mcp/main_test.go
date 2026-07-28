package main

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/mcpserver"
	"github.com/wallfacers/engram/memory/pipeline"
)

func TestBuildEmbeddingClientKeepsOfflineClientNil(t *testing.T) {
	client, err := buildEmbeddingClient(mcpserver.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if client != nil {
		t.Fatalf("offline embedding client has dynamic type %T, want nil", client)
	}
}

func TestBuildRegistryConfigPropagatesCurationWithoutSecrets(t *testing.T) {
	caller := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		return "", nil
	})
	config := mcpserver.ServerConfig{
		DataDir:           t.TempDir(),
		EmbedAPIKey:       "embed-secret",
		LLMAPIKey:         "llm-secret",
		MaxOpenNamespaces: 7,
		CurationEnabled:   true,
	}
	got := buildRegistryConfig(config, nil, caller)
	if !got.CurationEnabled || got.LLMCaller == nil {
		t.Fatalf("curation dependencies were not propagated: %+v", got)
	}
	if got.DataDir != config.DataDir || got.MaxOpenNamespaces != 7 {
		t.Fatalf("registry settings were not propagated: %+v", got)
	}
	// RegistryConfig has no API-key fields: secrets stay at provider
	// construction and cannot leak into namespace lifecycle configuration.
}
