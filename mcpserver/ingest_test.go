package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory/pipeline"
)

func TestMemoryIngestIsVisibleOnlyWithConfiguredCaller(t *testing.T) {
	ctx := context.Background()
	stub := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		return `{"facts":[{"fact":"The user prefers jasmine tea.","entities":["jasmine tea"],"event_date":"","category":"preference","durability":"evergreen"}]}`, nil
	})

	withCaller, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), LLMCaller: stub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withCaller.Close() })
	withClient, _ := connectInMemory(t, ctx, NewServer(withCaller))
	withTools, err := withClient.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(withTools.Tools, "memory_ingest") {
		t.Fatal("configured LLM did not register memory_ingest")
	}
	result := structuredMap(t, callTool(t, ctx, withClient, "memory_ingest", map[string]any{
		"messages": []any{map[string]any{"role": "user", "text": "I prefer jasmine tea."}},
	}))
	if result["extracted_count"] != float64(1) {
		t.Fatalf("unexpected ingest count: %#v", result)
	}
	entries := result["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["content"] != "The user prefers jasmine tea." {
		t.Fatalf("unexpected extracted entries: %#v", result)
	}
	search := structuredMap(t, callTool(t, ctx, withClient, "memory_search", map[string]any{"query": "jasmine tea"}))
	if len(search["results"].([]any)) == 0 {
		t.Fatalf("extracted entry was not searchable: %#v", search)
	}

	withoutCaller, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withoutCaller.Close() })
	withoutClient, _ := connectInMemory(t, ctx, NewServer(withoutCaller))
	withoutTools, err := withoutClient.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasTool(withoutTools.Tools, "memory_ingest") {
		t.Fatal("memory_ingest is visible without an LLM caller")
	}
	callTool(t, ctx, withoutClient, "memory_write", map[string]any{"name": "still-offline", "content": "CRUD remains available."})
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
