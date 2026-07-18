package mcpserver

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestMemorySearchMatchesDirectRetrieverOrder(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	handle, err := registry.Get(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	corpus := []struct {
		name    string
		trigger string
		content string
	}{
		{name: "tea", trigger: "morning drink", content: "The user prefers jasmine tea in the morning."},
		{name: "coffee", trigger: "morning drink", content: "The user sometimes drinks coffee after lunch."},
		{name: "travel", trigger: "next trip", content: "The user plans a trip to Kyoto in spring."},
	}
	for _, item := range corpus {
		if err := handle.entries.Upsert(ctx, entryForTest(item.name, item.trigger, item.content)); err != nil {
			t.Fatal(err)
		}
	}

	want, err := handle.retriever.Search(ctx, "morning drink", 8)
	if err != nil {
		t.Fatal(err)
	}

	server := NewServer(registry)
	clientSession, _ := connectInMemory(t, ctx, server)
	result := callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "morning drink",
		"limit": 8,
	})
	output := structuredMap(t, result)
	gotResults, ok := output["results"].([]any)
	if !ok {
		t.Fatalf("search output has no results array: %#v", output)
	}
	if len(gotResults) != len(want) {
		t.Fatalf("search result count = %d, direct count = %d", len(gotResults), len(want))
	}
	for i, direct := range want {
		got := gotResults[i].(map[string]any)
		if got["name"] != direct.Name {
			t.Fatalf("result %d name = %v, direct = %q; full output %#v", i, got["name"], direct.Name, output)
		}
	}
}

func entryForTest(name, trigger, content string) *memory.Entry {
	return &memory.Entry{Name: name, Trigger: trigger, Content: content, CharCount: memory.CharCount(content)}
}
