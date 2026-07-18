package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory/pipeline"
)

func TestMemoryToolsRoundTripOverInMemoryMCP(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	server := NewServer(registry)
	clientSession, serverSession := connectInMemory(t, ctx, server)

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTools := map[string]bool{
		"memory_write":  true,
		"memory_search": true,
		"memory_list":   true,
		"memory_get":    true,
		"memory_delete": true,
	}
	if len(tools.Tools) != len(wantTools) {
		t.Fatalf("tools/list returned %d tools, want %d", len(tools.Tools), len(wantTools))
	}
	for _, tool := range tools.Tools {
		if !wantTools[tool.Name] {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Description == "" || tool.InputSchema == nil {
			t.Fatalf("tool %q lacks description or input schema", tool.Name)
		}
	}

	write := callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name":     "preferences",
		"content":  "The user prefers dark mode.",
		"trigger":  "user preferences",
		"category": "preference",
	})
	writeOutput := structuredMap(t, write)
	if writeOutput["name"] != "preferences" || writeOutput["written"] != true {
		t.Fatalf("unexpected write output: %#v", writeOutput)
	}

	search := callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "dark mode",
	})
	searchOutput := structuredMap(t, search)
	results, ok := searchOutput["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("unexpected search results: %#v", searchOutput)
	}
	firstResult := results[0].(map[string]any)
	if firstResult["name"] != "preferences" || firstResult["content"] != "The user prefers dark mode." {
		t.Fatalf("unexpected search hit: %#v", firstResult)
	}
	degraded, ok := searchOutput["degraded"].(map[string]any)
	if !ok || degraded["semantic"] != true || degraded["reason"] != offlineDegradedReason {
		t.Fatalf("unexpected degraded marker: %#v", searchOutput["degraded"])
	}

	got := callTool(t, ctx, clientSession, "memory_get", map[string]any{"name": "preferences"})
	gotOutput := structuredMap(t, got)
	entry := gotOutput["entry"].(map[string]any)
	if entry["name"] != "preferences" || entry["content"] != "The user prefers dark mode." {
		t.Fatalf("unexpected get output: %#v", gotOutput)
	}

	list := callTool(t, ctx, clientSession, "memory_list", nil)
	listOutput := structuredMap(t, list)
	entries, ok := listOutput["entries"].([]any)
	if !ok || len(entries) != 1 || entries[0].(map[string]any)["name"] != "preferences" {
		t.Fatalf("unexpected list output: %#v", listOutput)
	}

	deleted := callTool(t, ctx, clientSession, "memory_delete", map[string]any{"name": "preferences"})
	if structuredMap(t, deleted)["deleted"] != true {
		t.Fatalf("unexpected delete output: %#v", structuredMap(t, deleted))
	}

	search = callTool(t, ctx, clientSession, "memory_search", map[string]any{"query": "dark mode"})
	searchOutput = structuredMap(t, search)
	if results, ok := searchOutput["results"].([]any); !ok || len(results) != 0 {
		t.Fatalf("expected empty search after delete, got %#v", searchOutput)
	}

	_ = serverSession
}

// TestToolsListExposesFullContractWithLLM is the tools/list smoke test for the
// LLM-configured server: exactly the six-tool contract (CRUD + memory_ingest),
// each with a non-empty description and a valid input schema (SC-004). The
// offline five-tool case is covered by TestMemoryToolsRoundTripOverInMemoryMCP.
func TestToolsListExposesFullContractWithLLM(t *testing.T) {
	ctx := context.Background()
	stub := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		return `{"facts":[]}`, nil
	})
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), LLMCaller: stub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"memory_write":  true,
		"memory_search": true,
		"memory_list":   true,
		"memory_get":    true,
		"memory_delete": true,
		"memory_ingest": true,
	}
	if len(tools.Tools) != len(want) {
		t.Fatalf("tools/list with LLM returned %d tools, want %d", len(tools.Tools), len(want))
	}
	seen := make(map[string]bool, len(want))
	for _, tool := range tools.Tools {
		if !want[tool.Name] {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Description == "" || tool.InputSchema == nil {
			t.Fatalf("tool %q lacks description or input schema (SC-004)", tool.Name)
		}
		seen[tool.Name] = true
	}
	for name := range want {
		if !seen[name] {
			t.Fatalf("tools/list is missing %q", name)
		}
	}
}

func connectInMemory(t *testing.T, ctx context.Context, server *mcp.Server) (*mcp.ClientSession, *mcp.ServerSession) {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession, serverSession
}

func callTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%s) returned tool error: %#v", name, result.Content)
	}
	return result
}

func structuredMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode structured content %s: %v", data, err)
	}
	return output
}

func TestToolsRejectPathLikeNamespacesWithoutCreatingOutsideFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	invalid := []string{"../outside", "a/b", `a\b`, filepath.Join(root, "absolute")}
	for _, namespace := range invalid {
		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "memory_write",
			Arguments: map[string]any{"namespace": namespace, "name": "blocked", "content": "must not be stored"},
		})
		if err != nil {
			t.Fatalf("namespace %q CallTool: %v", namespace, err)
		}
		if !result.IsError {
			t.Fatalf("namespace %q was accepted", namespace)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "data" {
			t.Fatalf("invalid namespace created outside data directory: %s", entry.Name())
		}
	}
}
