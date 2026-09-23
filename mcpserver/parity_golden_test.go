package mcpserver

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory"
)

// goldenCorpus is the fixed store behind the byte-golden parity tests: explicit
// IDs and creation times keep the wire bytes reproducible, so the golden can
// lock the whole response instead of only its key set.
func goldenCorpus() []memory.Entry {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []memory.Entry{
		{ID: "mem-tea", Name: "tea", Trigger: "morning drink", Content: "The user prefers jasmine tea in the morning.", CharCount: memory.CharCount("The user prefers jasmine tea in the morning."), CreatedAt: created, UpdatedAt: created},
		{ID: "mem-coffee", Name: "coffee", Trigger: "morning drink", Content: "The user sometimes drinks coffee after lunch.", CharCount: memory.CharCount("The user sometimes drinks coffee after lunch."), CreatedAt: created, UpdatedAt: created},
		{ID: "mem-travel", Name: "travel", Trigger: "next trip", Content: "The user plans a trip to Kyoto in spring.", CharCount: memory.CharCount("The user plans a trip to Kyoto in spring."), CreatedAt: created, UpdatedAt: created},
	}
}

// rawToolText returns the exact JSON text the server put on the wire for a tool
// call. Unlike result.StructuredContent (a map, whose marshaling reorders keys),
// this is the bytes the adapter produced — the surface the parity invariant is
// about.
func rawToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content block is %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

// projectionIDPattern matches the one generated field in a search response: the
// atomic-fact projection ULID the store mints per entry. Everything else in the
// response is a function of the seeded corpus, so the golden can lock all of it.
var projectionIDPattern = regexp.MustCompile(`"projection_id":"[^"]*"`)

const memorySearchLegacyGolden = `{"degraded":{"reason":"no embedding endpoint configured (offline mode)","semantic":true},"limit":8,"results":[{"content":"The user prefers jasmine tea in the morning.","created_at":"2026-01-02T03:04:05Z","event_date":null,"id":"mem-tea","name":"tea","projection_id":"<ulid>","projection_kind":"atomic_fact","score":0.01639344262295082,"snippet":"The user prefers jasmine tea in the morning.","source_session_id":"","trigger":"morning drink"},{"content":"The user sometimes drinks coffee after lunch.","created_at":"2026-01-02T03:04:05Z","event_date":null,"id":"mem-coffee","name":"coffee","projection_id":"<ulid>","projection_kind":"atomic_fact","score":0.016129032258064516,"snippet":"The user sometimes drinks coffee after lunch.","source_session_id":"","trigger":"morning drink"}],"returned":2,"scope":"ranked_subset"}`

func TestMemorySearchUnchangedCallGoldenWireBytes(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{})
	seedMemories(t, registry, ctx, goldenCorpus())
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	result := callTool(t, ctx, clientSession, "memory_search", map[string]any{
		"query": "morning drink",
		"limit": 8,
	})
	got := rawToolText(t, result)
	if strings.Contains(got, "pool_size") || strings.Contains(got, `"filter"`) {
		t.Fatalf("legacy memory_search response carries new telemetry: %s", got)
	}
	normalized := projectionIDPattern.ReplaceAllString(got, `"projection_id":"<ulid>"`)
	if normalized != memorySearchLegacyGolden {
		t.Fatalf("legacy memory_search wire bytes changed:\n got: %s\nwant: %s", normalized, memorySearchLegacyGolden)
	}
}

func TestMemoryWriteUngatedGoldenWireBytes(t *testing.T) {
	ctx := context.Background()
	registry := testRegistry(t, ctx, RegistryConfig{})
	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))

	result := callTool(t, ctx, clientSession, "memory_write", map[string]any{
		"name": "preference", "content": "The user prefers dark mode.",
	})
	const golden = `{"name":"preference","written":true}`
	if got := rawToolText(t, result); got != golden {
		t.Fatalf("ungated memory_write wire bytes changed:\n got: %s\nwant: %s", got, golden)
	}
}
