package prompt

import (
	"strings"
	"testing"
)

func TestMemoryExtractionPromptCarriesStableSourceIDs(t *testing.T) {
	user := BuildMemoryExtractionUserPrompt("2026-07-30", []MemoryExtractionMessage{
		{Role: "user", Text: "Alice moved to Berlin.", SourceID: "01HXUSER"},
		{Role: "assistant", Text: "I recorded the move.", SourceID: "01HXASSIST"},
	})
	for _, want := range []string{
		"[source_id=01HXUSER] user: Alice moved to Berlin.",
		"[source_id=01HXASSIST] assistant: I recorded the move.",
	} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q: %q", want, user)
		}
	}
	if !strings.Contains(MemoryExtractionSystemPrompt, `"source_ids"`) || !strings.Contains(MemoryExtractionSystemPrompt, "never return a fact without at least one source_id") {
		t.Fatal("system prompt does not require exact non-empty source IDs")
	}
}
