package mcpserver

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/memory"
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

func TestRegistryEvictsLeastRecentlyUsedNamespaceAndPreservesData(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), MaxOpenNamespaces: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	first, err := registry.Get(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.entries.Upsert(ctx, &memory.Entry{Name: "first-memory", Content: "persisted first namespace", CharCount: 26}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	second, err := registry.Get(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.entries.Upsert(ctx, &memory.Entry{Name: "second-memory", Content: "persisted second namespace", CharCount: 27}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, "third"); err != nil {
		t.Fatal(err)
	}
	if got := registry.handleCount(); got != 2 {
		t.Fatalf("registry has %d open handles after eviction, want 2", got)
	}
	if _, ok := registry.handles["second"]; ok {
		t.Fatal("least recently used second namespace was not evicted")
	}

	reopened, err := registry.Get(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.entries.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "second-memory" {
		t.Fatalf("evicted second namespace lost persisted data: %#v", entries)
	}
	firstAgain, err := registry.Get(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	entries, err = firstAgain.entries.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "first-memory" {
		t.Fatalf("evicted first namespace lost persisted data: %#v", entries)
	}
}
