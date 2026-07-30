package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory/pipeline"
)

func TestMemoryIngestIsVisibleOnlyWithConfiguredCaller(t *testing.T) {
	ctx := context.Background()
	stub := pipeline.ModelCaller(func(_ context.Context, _, user string) (string, error) {
		return responseWithPromptSources(`{"facts":[{"fact":"The user prefers jasmine tea.","entities":["jasmine tea"],"event_date":"","category":"preference","durability":"evergreen"}]}`, user), nil
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

func TestMemoryIngestNotifiesCuratorOncePerSuccessfulBatch(t *testing.T) {
	ctx := context.Background()
	curationStarted := make(chan struct{})
	releaseJudge := make(chan struct{})
	var calls atomic.Int32
	caller := pipeline.ModelCaller(func(callCtx context.Context, _, user string) (string, error) {
		switch calls.Add(1) {
		case 1:
			return responseWithPromptSources(`{"facts":[
				{"fact":"The user likes jasmine tea.","entities":["jasmine tea"],"category":"preference","durability":"evergreen"},
				{"fact":"The user likes dark mode.","entities":["dark mode"],"category":"preference","durability":"evergreen"}
			]}`, user), nil
		case 2:
			close(curationStarted)
			select {
			case <-releaseJudge:
				return `{"evict":[],"merge":[]}`, nil
			case <-callCtx.Done():
				return "", callCtx.Err()
			}
		default:
			return `{"evict":[],"merge":[]}`, nil
		}
	})
	registry, err := NewRegistry(ctx, RegistryConfig{
		DataDir:         t.TempDir(),
		LLMCaller:       caller,
		CurationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	adapter := &toolAdapter{registry: registry}
	_, output, err := adapter.memoryIngest(ctx, nil, memoryIngestInput{
		Messages: []memoryIngestMessage{{Role: "user", Text: "I like jasmine tea and dark mode."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.ExtractedCount != 2 {
		t.Fatalf("ExtractedCount = %d, want 2", output.ExtractedCount)
	}
	select {
	case <-curationStarted:
	case <-time.After(time.Second):
		t.Fatal("successful ingest batch did not notify curator")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("model calls = %d, want one extraction + one curation", got)
	}
	close(releaseJudge)
}

func TestMemoryIngestDoesNotNotifyCuratorByDefault(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	caller := pipeline.ModelCaller(func(_ context.Context, _, user string) (string, error) {
		calls.Add(1)
		return responseWithPromptSources(`{"facts":[{"fact":"The user likes jasmine tea.","entities":["jasmine tea"],"category":"preference","durability":"evergreen"}]}`, user), nil
	})
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), LLMCaller: caller})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	adapter := &toolAdapter{registry: registry}
	if _, output, err := adapter.memoryIngest(ctx, nil, memoryIngestInput{
		Messages: []memoryIngestMessage{{Role: "user", Text: "I like jasmine tea."}},
	}); err != nil {
		t.Fatal(err)
	} else if output.ExtractedCount != 1 {
		t.Fatalf("ExtractedCount = %d, want 1", output.ExtractedCount)
	}
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("default mode model calls = %d, want extraction only", got)
	}
}

func responseWithPromptSources(response, user string) string {
	if strings.Contains(response, `"source_ids"`) {
		return response
	}
	sources, err := json.Marshal(promptSourceIDs(user))
	if err != nil {
		return response
	}
	return strings.ReplaceAll(response, `{"fact":`, `{"source_ids":`+string(sources)+`,"fact":`)
}

func promptSourceIDs(user string) []string {
	const marker = "source_id="
	var ids []string
	for rest := user; ; {
		start := strings.Index(rest, marker)
		if start < 0 {
			return ids
		}
		rest = rest[start+len(marker):]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ids
		}
		ids = append(ids, rest[:end])
		rest = rest[end+1:]
	}
}
