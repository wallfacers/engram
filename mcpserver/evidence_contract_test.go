package mcpserver

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory/pipeline"
)

func TestMemoryIngestV2PersistsEvidenceOffline(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	client, _ := connectInMemory(t, ctx, NewServer(registry))

	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(tools.Tools, "memory_ingest_v2") {
		t.Fatal("offline server did not register memory_ingest_v2")
	}
	if hasTool(tools.Tools, "memory_ingest") {
		t.Fatal("legacy memory_ingest must remain hidden without an LLM caller")
	}

	ingested := structuredMap(t, callTool(t, ctx, client, "memory_ingest_v2", map[string]any{
		"session_id": "session-offline",
		"messages": []any{
			map[string]any{"source_id": "turn-1", "role": "user", "text": "I prefer jasmine tea.", "ordinal": 0},
			map[string]any{"source_id": "turn-2", "role": "assistant", "text": "Noted: jasmine tea.", "ordinal": 1},
		},
	}))
	if ingested["extracted_count"] != float64(0) {
		t.Fatalf("offline extracted_count = %#v, want 0", ingested["extracted_count"])
	}
	if degraded, ok := ingested["degraded"].([]any); !ok || len(degraded) != 1 || degraded[0] != "extraction_unavailable" {
		t.Fatalf("offline degraded = %#v, want [extraction_unavailable]", ingested["degraded"])
	}
	receipts := evidenceMaps(t, ingested["evidence"])
	if len(receipts) != 2 || receipts[0]["source_id"] != "turn-1" || receipts[1]["source_id"] != "turn-2" {
		t.Fatalf("offline evidence receipts = %#v", receipts)
	}
	firstID, _ := receipts[0]["evidence_id"].(string)
	secondID, _ := receipts[1]["evidence_id"].(string)
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("invalid offline evidence IDs: %#v", receipts)
	}

	got := structuredMap(t, callTool(t, ctx, client, "memory_evidence_get", map[string]any{
		"evidence_ids": []any{secondID, firstID},
	}))
	evidence := evidenceMaps(t, got["evidence"])
	if len(evidence) != 2 || evidence[0]["evidence_id"] != secondID || evidence[0]["content"] != "Noted: jasmine tea." ||
		evidence[1]["evidence_id"] != firstID || evidence[1]["content"] != "I prefer jasmine tea." {
		t.Fatalf("evidence get did not preserve request order/content: %#v", evidence)
	}
}

func TestEvidenceToolsAreNamespaceIsolatedAndLifecycleSafe(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	client, _ := connectInMemory(t, ctx, NewServer(registry))

	ingest := func(namespace, text string) string {
		t.Helper()
		output := structuredMap(t, callTool(t, ctx, client, "memory_ingest_v2", map[string]any{
			"namespace":  namespace,
			"session_id": "shared-session",
			"extract":    false,
			"messages": []any{
				map[string]any{"source_id": "same-source-id", "role": "user", "text": text, "ordinal": 0},
			},
		}))
		receipts := evidenceMaps(t, output["evidence"])
		if len(receipts) != 1 {
			t.Fatalf("%s receipts = %#v", namespace, receipts)
		}
		id, _ := receipts[0]["evidence_id"].(string)
		if id == "" {
			t.Fatalf("%s empty evidence ID", namespace)
		}
		return id
	}
	alphaID := ingest("alpha", "alpha private evidence")
	betaID := ingest("beta", "beta independent evidence")
	if alphaID == betaID {
		t.Fatalf("namespace-local evidence IDs unexpectedly match: %q", alphaID)
	}
	callToolError(t, ctx, client, "memory_evidence_get", map[string]any{
		"namespace": "alpha", "evidence_ids": []any{betaID},
	})

	tombstoned := structuredMap(t, callTool(t, ctx, client, "memory_evidence_tombstone", map[string]any{
		"namespace": "alpha", "evidence_id": alphaID, "request_id": "delete-alpha", "reason_code": "user_delete",
	}))
	if tombstoned["evidence_id"] != alphaID || tombstoned["state"] != "tombstoned" {
		t.Fatalf("tombstone output = %#v", tombstoned)
	}
	callToolError(t, ctx, client, "memory_evidence_get", map[string]any{
		"namespace": "alpha", "evidence_ids": []any{alphaID},
	})
	restored := structuredMap(t, callTool(t, ctx, client, "memory_evidence_restore", map[string]any{
		"namespace": "alpha", "evidence_id": alphaID, "request_id": "restore-alpha", "reason_code": "user_restore",
	}))
	if restored["state"] != "active" {
		t.Fatalf("restore output = %#v", restored)
	}
	purged := structuredMap(t, callTool(t, ctx, client, "memory_evidence_purge", map[string]any{
		"namespace": "alpha", "evidence_id": alphaID, "request_id": "purge-alpha", "reason_code": "privacy_purge",
	}))
	if purged["evidence_id"] != alphaID || purged["state"] != "purged" {
		t.Fatalf("purge output = %#v", purged)
	}
	result := callToolError(t, ctx, client, "memory_evidence_get", map[string]any{
		"namespace": "alpha", "evidence_ids": []any{alphaID},
	})
	if strings.Contains(strings.ToLower(toolResultText(result)), "alpha private evidence") {
		t.Fatalf("purged source content leaked through tool error: %#v", result.Content)
	}
	beta := structuredMap(t, callTool(t, ctx, client, "memory_evidence_get", map[string]any{
		"namespace": "beta", "evidence_ids": []any{betaID},
	}))
	if got := evidenceMaps(t, beta["evidence"]); len(got) != 1 || got[0]["content"] != "beta independent evidence" {
		t.Fatalf("alpha lifecycle affected beta evidence: %#v", got)
	}
}

func TestMemoryIngestV2ConflictRollsBackWithoutExtraction(t *testing.T) {
	ctx := context.Background()
	var extractionCalls atomic.Int32
	registry, err := NewRegistry(ctx, RegistryConfig{
		DataDir: t.TempDir(),
		LLMCaller: pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
			extractionCalls.Add(1)
			return `{"facts":[]}`, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	client, _ := connectInMemory(t, ctx, NewServer(registry))

	callTool(t, ctx, client, "memory_ingest_v2", map[string]any{
		"session_id": "session-conflict",
		"messages": []any{map[string]any{
			"source_id": "stable-source", "role": "user", "text": "original evidence", "ordinal": 0,
		}},
	})
	if got := extractionCalls.Load(); got != 1 {
		t.Fatalf("initial extraction calls = %d, want 1", got)
	}
	callToolError(t, ctx, client, "memory_ingest_v2", map[string]any{
		"session_id": "session-conflict",
		"messages": []any{
			map[string]any{"source_id": "fresh-source", "role": "assistant", "text": "must roll back", "ordinal": 1},
			map[string]any{"source_id": "stable-source", "role": "user", "text": "changed evidence", "ordinal": 0},
		},
	})
	if got := extractionCalls.Load(); got != 1 {
		t.Fatalf("conflicting append called extractor %d times, want 1", got)
	}
	handle, release, err := registry.Acquire(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	evidence, err := handle.ledger.ListSession(ctx, "session-conflict", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].ExternalSourceID != "stable-source" || evidence[0].Content != "original evidence" {
		t.Fatalf("conflicting v2 append changed evidence session: %#v", evidence)
	}
}

func evidenceMaps(t *testing.T, value any) []map[string]any {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("evidence value = %#v, want array", value)
	}
	out := make([]map[string]any, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("evidence[%d] = %#v, want object", index, value)
		}
		out[index] = item
	}
	return out
}

func callToolError(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("CallTool(%s) unexpectedly succeeded: %#v", name, result.StructuredContent)
	}
	return result
}

func toolResultText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if item, ok := content.(*mcp.TextContent); ok {
			text.WriteString(item.Text)
		}
	}
	return text.String()
}
