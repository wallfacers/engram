package mcpserver

import (
	"context"
	"testing"
)

func TestOfflineServerStartsAndCompletesCRUDWithDegradedSearch(t *testing.T) {
	ctx := context.Background()
	config, err := LoadConfigWithEnv([]string{"--data-dir", t.TempDir()}, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.EmbedBaseURL != "" || config.LLMProvider != "" {
		t.Fatalf("offline config unexpectedly has external endpoints: %#v", config)
	}
	registry, err := NewRegistry(ctx, RegistryConfig{
		DataDir: config.DataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("offline tools/list returned %d tools, want 5", len(tools.Tools))
	}

	for _, memory := range []map[string]any{
		{"name": "offline-tea", "content": "The user prefers jasmine tea every morning.", "trigger": "morning drink"},
		{"name": "offline-book", "content": "The user reads a book before bed.", "trigger": "bedtime"},
	} {
		callTool(t, ctx, clientSession, "memory_write", memory)
	}
	search := structuredMap(t, callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "jasmine tea morning",
	}))
	results, ok := search["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("offline search returned no results: %#v", search)
	}
	degraded, ok := search["degraded"].(map[string]any)
	if !ok || degraded["semantic"] != true || degraded["reason"] != offlineDegradedReason {
		t.Fatalf("offline search marker is incorrect: %#v", search["degraded"])
	}

	got := structuredMap(t, callTool(t, ctx, clientSession, "memory_get", map[string]any{"name": "offline-tea"}))
	if got["entry"].(map[string]any)["name"] != "offline-tea" {
		t.Fatalf("offline get returned unexpected entry: %#v", got)
	}
	listed := structuredMap(t, callTool(t, ctx, clientSession, "memory_list", nil))
	if len(listed["entries"].([]any)) != 2 {
		t.Fatalf("offline list returned unexpected entries: %#v", listed)
	}
	deleted := structuredMap(t, callTool(t, ctx, clientSession, "memory_delete", map[string]any{"name": "offline-tea"}))
	if deleted["deleted"] != true {
		t.Fatalf("offline delete failed: %#v", deleted)
	}
}
