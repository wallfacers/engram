package mcpserver

import (
	"context"
	"testing"
)

func TestRegistryLazilyOpensAndReusesNamespace(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	if got := registry.handleCount(); got != 0 {
		t.Fatalf("new registry has %d handles, want 0", got)
	}

	first, err := registry.Get(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if first.entries == nil || first.retriever == nil {
		t.Fatalf("lazily opened handle is incomplete: entries=%v retriever=%v", first.entries, first.retriever)
	}
	if got := registry.handleCount(); got != 1 {
		t.Fatalf("after first Get has %d handles, want 1", got)
	}

	second, err := registry.Get(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("repeated default namespace Get did not reuse the handle")
	}
	if got := registry.handleCount(); got != 1 {
		t.Fatalf("after repeated Get has %d handles, want 1", got)
	}
}

func TestRegistryCloseClosesAllHandles(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, "other"); err != nil {
		t.Fatal(err)
	}

	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := registry.handleCount(); got != 0 {
		t.Fatalf("closed registry has %d handles, want 0", got)
	}
}
