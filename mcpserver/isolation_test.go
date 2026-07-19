package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNamespacesAreIsolatedAndDefaultIsIndependent(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"namespace": "projectA",
		"name":      "a-memory",
		"content":   "Only project A knows this fact.",
		"trigger":   "project A",
	})
	callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"namespace": "projectB",
		"name":      "b-memory",
		"content":   "Only project B knows this fact.",
		"trigger":   "project B",
	})
	callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name":    "default-memory",
		"content": "Only the default namespace knows this fact.",
	})

	assertNamespaceHasOnly(t, ctx, clientSession, "projectA", "a-memory")
	assertNamespaceHasOnly(t, ctx, clientSession, "projectB", "b-memory")
	assertNamespaceHasOnly(t, ctx, clientSession, "", "default-memory")

	callTool(t, ctx, clientSession, "memory_delete", map[string]any{
		"namespace": "projectA",
		"name":      "a-memory",
	})
	assertNamespaceHasOnly(t, ctx, clientSession, "projectB", "b-memory")
}

func assertNamespaceHasOnly(t *testing.T, ctx context.Context, session *mcp.ClientSession, namespace, wantName string) {
	t.Helper()
	list := structuredMap(t, callTool(t, ctx, session, "memory_list", map[string]any{"namespace": namespace}))
	entries := list["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != wantName {
		t.Fatalf("namespace %q list leaked or lost entries: %#v", namespace, list)
	}
	got := structuredMap(t, callTool(t, ctx, session, "memory_get", map[string]any{
		"namespace": namespace,
		"name":      wantName,
	}))
	if got["entry"].(map[string]any)["name"] != wantName {
		t.Fatalf("namespace %q get returned the wrong entry: %#v", namespace, got)
	}
	search := structuredMap(t, callTool(t, ctx, session, "memory_search", map[string]any{
		"namespace": namespace,
		"query":     "fact",
	}))
	results := search["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["name"] != wantName {
		t.Fatalf("namespace %q search leaked or lost entries: %#v", namespace, search)
	}
}
