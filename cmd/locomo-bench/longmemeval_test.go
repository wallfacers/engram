package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
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

func TestLoadLongMemEvalAcceptsSingleSessionPreference(t *testing.T) {
	items, err := loadLongMemEval(filepath.Join("..", "..", "testdata", "longmemeval", "sample_array.json"))
	if err != nil {
		t.Fatalf("load array-shaped LongMemEval fixture: %v", err)
	}

	for _, item := range items {
		if item.QuestionType != "single-session-preference" {
			continue
		}
		if item.Category != "single-session-preference" {
			t.Fatalf("preference category = %q, want single-session-preference", item.Category)
		}
		if got := longMemEvalCategoryID(item.Category); got != 12 {
			t.Fatalf("preference category id = %d, want 12", got)
		}
		return
	}
	t.Fatal("single-session-preference item not loaded")
}

func TestLoadLongMemEvalUsesIndexedHaystackDates(t *testing.T) {
	items := loadLongMemEvalFixtureSubset(t, 0)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	sessions := items[0].Conversation.Sessions
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	want := []time.Time{
		time.Date(2023, time.April, 10, 17, 50, 0, 0, time.UTC),
		time.Date(2023, time.April, 11, 8, 15, 0, 0, time.UTC),
	}
	for i := range sessions {
		if !sessions[i].Date.Equal(want[i]) {
			t.Errorf("session %d date = %s, want %s", i+1, sessions[i].Date, want[i])
		}
	}
	if sessions[0].Date.Equal(sessions[1].Date) {
		t.Fatalf("session dates collapsed to %s", sessions[0].Date)
	}
}

func TestLoadLongMemEvalRejectsMismatchedHaystackDates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mismatched-dates.json")
	data := []byte(`[
  {
    "question_id": "mismatched-dates",
    "question_type": "multi-session",
    "question": "What happened?",
    "answer": "two events",
    "question_date": "2023-07-03 12:00",
    "haystack_dates": ["2023/07/01 (Sat) 09:00"],
    "haystack_session_ids": ["mismatch_1", "mismatch_2"],
    "haystack_sessions": [
      [{"role": "user", "content": "The first event happened."}],
      [{"role": "assistant", "content": "The second event happened."}]
    ]
  }
]`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write mismatched fixture: %v", err)
	}
	if _, err := loadLongMemEval(path); err == nil {
		t.Fatal("loadLongMemEval succeeded with mismatched haystack_dates and haystack_sessions")
	}
}

func TestLoadLongMemEvalSynthesizesParseableTurnDiaIDs(t *testing.T) {
	items := loadLongMemEvalFixtureSubset(t, 0)
	sessions := items[0].Conversation.Sessions
	want := [][]string{
		{"D1:1", "D1:2"},
		{"D2:1", "D2:2"},
	}
	if len(sessions) != len(want) {
		t.Fatalf("sessions = %d, want %d", len(sessions), len(want))
	}
	for sessionIndex, s := range sessions {
		if len(s.Turns) != len(want[sessionIndex]) {
			t.Fatalf("session %d turns = %d, want %d", sessionIndex+1, len(s.Turns), len(want[sessionIndex]))
		}
		for turnIndex, turn := range s.Turns {
			if !evidenceReferencePattern.MatchString(turn.DiaID) {
				t.Errorf("session %d turn %d DiaID %q does not match %s", sessionIndex+1, turnIndex+1, turn.DiaID, evidenceReferencePattern)
			}
			if turn.DiaID != want[sessionIndex][turnIndex] {
				t.Errorf("session %d turn %d DiaID = %q, want %q", sessionIndex+1, turnIndex+1, turn.DiaID, want[sessionIndex][turnIndex])
			}
		}
	}
}

func TestLoadLongMemEvalCollectsOnlyAnswerTurnEvidence(t *testing.T) {
	path := longMemEvalFixtureSubsetPath(t, 0)
	convs, err := loadBenchmarkDataset(path, "longmemeval", false)
	if err != nil {
		t.Fatalf("load benchmark fixture subset: %v", err)
	}
	if len(convs) != 1 || len(convs[0].QA) != 1 {
		t.Fatalf("conversations = %+v, want one conversation with one QA", convs)
	}
	want := []string{"D1:1", "D2:2"}
	if got := convs[0].QA[0].Evidence; !slices.Equal(got, want) {
		t.Fatalf("evidence = %v, want only has_answer turns %v", got, want)
	}
}

func TestLoadLongMemEvalLeavesQuestionsWithoutAnswerEvidenceUngradeable(t *testing.T) {
	path := longMemEvalFixtureSubsetPath(t, 0, 2)
	convs, err := loadBenchmarkDataset(path, "longmemeval", false)
	if err != nil {
		t.Fatalf("load benchmark fixture subset: %v", err)
	}
	if len(convs) != 2 {
		t.Fatalf("conversations = %d, want 2", len(convs))
	}

	if _, _, gradeable := evidenceRecallAt(convs[0].QA[0], nil, nil); !gradeable {
		t.Fatal("question with has_answer turns is ungradeable")
	}
	qa := convs[1].QA[0]
	if len(qa.Evidence) != 0 {
		t.Fatalf("question without has_answer turns has evidence %v", qa.Evidence)
	}
	if _, _, gradeable := evidenceRecallAt(qa, nil, nil); gradeable {
		t.Fatal("question without has_answer turns is gradeable")
	}
}

func TestLongMemEvalSyntheticEvidenceUsesExistingRecallRuler(t *testing.T) {
	path := longMemEvalFixtureSubsetPath(t, 0)
	convs, err := loadBenchmarkDataset(path, "longmemeval", false)
	if err != nil {
		t.Fatalf("load benchmark fixture subset: %v", err)
	}
	conv := convs[0]
	chunks := buildSessionChunks(conv.Sessions[0])
	if len(chunks) != 1 {
		t.Fatalf("session 1 chunks = %d, want 1", len(chunks))
	}
	const chunkName = "chunk-c0-s1-000"
	hits := []memory.Result{{Name: chunkName, SourceSessionID: "conv0-sess1"}}
	chunkTurns := map[string][]string{chunkName: chunks[0].DiaIDs}

	turnRecall, sessionRecall, gradeable := evidenceRecallAt(conv.QA[0], hits, chunkTurns)
	if !gradeable {
		t.Fatal("synthetic LongMemEval evidence is ungradeable")
	}
	if turnRecall != 0.5 {
		t.Fatalf("turn recall = %v, want 0.5", turnRecall)
	}
	if sessionRecall != 0.5 {
		t.Fatalf("session recall = %v, want 0.5", sessionRecall)
	}
}

func loadLongMemEvalFixtureSubset(t *testing.T, indexes ...int) []longMemEvalItem {
	t.Helper()
	items, err := loadLongMemEval(longMemEvalFixtureSubsetPath(t, indexes...))
	if err != nil {
		t.Fatalf("load fixture subset: %v", err)
	}
	return items
}

func longMemEvalFixtureSubsetPath(t *testing.T, indexes ...int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "longmemeval", "sample_array.json"))
	if err != nil {
		t.Fatalf("read array-shaped fixture: %v", err)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatalf("parse array-shaped fixture: %v", err)
	}
	selected := make([]json.RawMessage, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(records) {
			t.Fatalf("fixture record index %d out of range", index)
		}
		selected = append(selected, records[index])
	}
	path := filepath.Join(t.TempDir(), "longmemeval.json")
	data, err := json.Marshal(selected)
	if err != nil {
		t.Fatalf("encode fixture subset: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture subset: %v", err)
	}
	return path
}
