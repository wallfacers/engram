package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfiguredSecretsDoNotAppearInLogsOrToolResponses(t *testing.T) {
	secret := "test-only-" + strings.Repeat("x", 32)
	env := map[string]string{
		"ENGRAM_DATA_DIR":      t.TempDir(),
		"ENGRAM_EMBED_API_KEY": secret,
		"ENGRAM_LLM_API_KEY":   secret,
	}
	config, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	registry, err := NewRegistry(context.Background(), RegistryConfig{DataDir: config.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	clientSession, _ := connectInMemory(t, context.Background(), NewServer(registry))
	for _, result := range []*mcp.CallToolResult{
		callTool(t, context.Background(), clientSession, "memory_write", map[string]any{"name": "safe", "content": "safe response"}),
		callTool(t, context.Background(), clientSession, "memory_search", map[string]any{"query": "safe"}),
		callTool(t, context.Background(), clientSession, "memory_get", map[string]any{"name": "safe"}),
		callTool(t, context.Background(), clientSession, "memory_list", nil),
		callTool(t, context.Background(), clientSession, "memory_delete", map[string]any{"name": "safe"}),
	} {
		payload, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), secret) {
			t.Fatalf("secret appeared in tool response: %s", payload)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("secret appeared in logs: %s", logs.String())
	}
}
