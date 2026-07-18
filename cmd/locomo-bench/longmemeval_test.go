package main

import (
	"path/filepath"
	"testing"
)

func TestLoadLongMemEvalSMapsAllQuestionTypes(t *testing.T) {
	items, err := loadLongMemEval(filepath.Join("..", "..", "testdata", "longmemeval", "sample.json"))
	if err != nil {
		t.Fatalf("load LongMemEval fixture: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("items = %d, want 7", len(items))
	}
	wantBuckets := map[string]bool{
		"single-session-user":      true,
		"single-session-assistant": true,
		"multi-session":            true,
		"temporal-reasoning":       true,
		"knowledge-update":         true,
		"abstention":               true,
		"preference":               true,
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !wantBuckets[item.Category] {
			t.Fatalf("unexpected category %q for type %q", item.Category, item.QuestionType)
		}
		seen[item.Category] = true
		if len(item.Conversation.Sessions) != 1 || len(item.Conversation.Sessions[0].Turns) != 1 {
			t.Fatalf("item %q sessions not parsed: %+v", item.ID, item.Conversation.Sessions)
		}
		if item.Conversation.Sessions[0].Date.IsZero() {
			t.Fatalf("item %q session timestamp missing", item.ID)
		}
		if item.QuestionType == "abstention" && !item.Adversarial {
			t.Fatal("abstention item must use adversarial scoring")
		}
	}
	if len(seen) != len(wantBuckets) {
		t.Fatalf("mapped categories = %v, want all %v", seen, wantBuckets)
	}
}
