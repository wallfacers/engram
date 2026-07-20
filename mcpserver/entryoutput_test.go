package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// TestMemoryGetSurfacesEventRange asserts the MCP serialization layer does not
// drop the engine Entry's additive event-range fields (EventStart/EventEnd),
// honoring the 002 contract that engine Entry fields are not lost. superseded_by
// is intentionally NOT surfaced yet: the behavior that populates it (US5
// conflict resolution) is unlanded, so the field is inert; its outward contract
// is deferred until US5 lands rather than exposing an always-empty field now.
func TestMemoryGetSurfacesEventRange(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	start := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 5, 3, 0, 0, 0, 0, time.UTC)
	handle := getForTest(t, registry, ctx, "default")
	if err := handle.entries.Upsert(ctx, &memory.Entry{
		Name:       "trip",
		Content:    "A trip to Kyoto.",
		CharCount:  memory.CharCount("A trip to Kyoto."),
		EventStart: &start,
		EventEnd:   &end,
	}); err != nil {
		t.Fatal(err)
	}

	clientSession, _ := connectInMemory(t, ctx, NewServer(registry))
	got := structuredMap(t, callTool(t, ctx, clientSession, "memory_get", map[string]any{"name": "trip"}))
	entry, ok := got["entry"].(map[string]any)
	if !ok {
		t.Fatalf("memory_get output missing entry: %#v", got)
	}
	if entry["event_start"] != start.Format(time.RFC3339) {
		t.Fatalf("event_start = %v, want %s (full entry %#v)", entry["event_start"], start.Format(time.RFC3339), entry)
	}
	if entry["event_end"] != end.Format(time.RFC3339) {
		t.Fatalf("event_end = %v, want %s", entry["event_end"], end.Format(time.RFC3339))
	}
	if _, present := entry["superseded_by"]; present {
		t.Fatalf("superseded_by must not be surfaced until US5 lands, got %#v", entry["superseded_by"])
	}
}
