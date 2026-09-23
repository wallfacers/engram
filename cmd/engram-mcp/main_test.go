package main

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/mcpserver"
	"github.com/wallfacers/engram/memory/pipeline"
)

// stubWriteGate satisfies filter.WriteGate so the propagation test can observe
// the interface crossing the constructor boundary.
type stubWriteGate struct{}

func (stubWriteGate) Gate(context.Context, string, string) (filter.GateDecision, error) {
	return filter.GateDecision{Route: filter.RouteWrite}, nil
}

func (stubWriteGate) GateWithOptions(context.Context, filter.GateRequest) (filter.GateDecision, error) {
	return filter.GateDecision{Route: filter.RouteWrite}, nil
}

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
		JevBaseURL:        "http://jev.local",
		JevAPIKey:         "jev-secret",
		JevTheta:          0.5,
		JevRelaxTheta:     0.35,
		JevKShowMax:       12,
		JevRelax:          true,
		JevWriteGate:      true,
		SearchPool:        150,
		SearchFilter:      "jev",
	}
	got := buildRegistryConfig(config, nil, caller, filter.None{}, stubWriteGate{})
	if !got.CurationEnabled || got.LLMCaller == nil {
		t.Fatalf("curation dependencies were not propagated: %+v", got)
	}
	if got.DataDir != config.DataDir || got.MaxOpenNamespaces != 7 {
		t.Fatalf("registry settings were not propagated: %+v", got)
	}
	if got.SearchFilterName != "jev" || got.SearchPool != 150 || got.SearchPolicy.Theta != 0.5 {
		t.Fatalf("search knobs were not propagated: %+v", got)
	}
	if got.SearchFilter == nil || got.WriteGate == nil || !got.WriteGateEnabled {
		t.Fatalf("search filter and write-gate switch were not propagated: %+v", got)
	}
	// RegistryConfig has no API-key fields: secrets stay at provider
	// construction and cannot leak into namespace lifecycle configuration.
}
