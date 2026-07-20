package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestExactTurnRecallDistinguishesWrongTurnSameSession(t *testing.T) {
	qa := locomoQA{Evidence: []string{"D1:3", "D2:1"}}
	hits := []memory.Result{
		{Name: "chunk-c0-s1-000", SourceSessionID: "conv0-sess1"},
		{Name: "chunk-c0-s2-005", SourceSessionID: "conv0-sess2"},
	}
	chunkTurns := map[string][]string{
		"chunk-c0-s1-000": {"D1:1", "D1:2", "D1:3"}, // covers the gold turn D1:3
		"chunk-c0-s2-005": {"D2:8", "D2:9"},         // session 2, but NOT the gold turn D2:1
	}
	diag := newSweepEvidenceDiagnostics(qa, hits, memory.SearchDiagnostics{SweepUsed: true}, 100, chunkTurns)
	// The old session-level metric is inflated: both gold sessions are present.
	if diag.EvidenceSessionRecall != 1 {
		t.Fatalf("session recall = %v, want 1 (both sessions hit)", diag.EvidenceSessionRecall)
	}
	// Exact-turn recall is the honest metric: D1:3 covered, D2:1 not (only D2:8/9).
	if diag.EvidenceTurnRecall != 0.5 {
		t.Fatalf("exact-turn recall = %v, want 0.5 (wrong turn in session 2 must not count)", diag.EvidenceTurnRecall)
	}
}

func TestSweepEvidenceDiagnosticsRecordsRecallAndJournal(t *testing.T) {
	qa := locomoQA{Evidence: []string{"D1:3", "D2:1"}}
	hits := []memory.Result{
		{Name: "session-one", SourceSessionID: "conv0-sess1"},
		{Name: "session-two-opinion", SourceSessionID: "conv0-sess2-op"},
		{Name: "duplicate-session-two", SourceSessionID: "conv0-sess2"},
	}
	diag := newSweepEvidenceDiagnostics(qa, hits, memory.SearchDiagnostics{
		SweepUsed:             true,
		SweepCandidatesBefore: 7,
		SweepCandidatesAfter:  11,
	}, 321, nil)
	if !reflect.DeepEqual(diag.GoldEvidenceSessions, []int{1, 2}) {
		t.Fatalf("gold evidence sessions = %v, want [1 2]", diag.GoldEvidenceSessions)
	}
	if !reflect.DeepEqual(diag.GoldEvidenceReferences, []goldEvidenceReference{{Session: 1, Dialog: 3}, {Session: 2, Dialog: 1}}) {
		t.Fatalf("gold evidence references = %v", diag.GoldEvidenceReferences)
	}
	if diag.EvidenceSessionRecall != 1 {
		t.Fatalf("evidence session recall = %v, want 1", diag.EvidenceSessionRecall)
	}
	if !reflect.DeepEqual(diag.RetrievedMemoryNames, []string{"session-one", "session-two-opinion", "duplicate-session-two"}) {
		t.Fatalf("retrieved memory names = %v", diag.RetrievedMemoryNames)
	}
	if diag.SweepCandidatesBefore != 7 || diag.SweepCandidatesAfter != 11 || diag.AnswerContextTokens != 321 {
		t.Fatalf("diagnostic counts = %+v", diag)
	}

	dir := t.TempDir()
	j, err := openJournal(dir, "hybrid+sweep")
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	j.write(result{Conv: 0, Q: 1, SweepUsed: true, EvidenceDiagnostics: diag})
	j.Close()
	items, err := readResultsJSONL(filepath.Join(dir, "results-hybrid+sweep.jsonl"))
	if err != nil || len(items) != 1 {
		t.Fatalf("read journal = %d items, err=%v", len(items), err)
	}
	if !reflect.DeepEqual(items[0].EvidenceDiagnostics, diag) {
		t.Fatalf("persisted diagnostic = %+v, want %+v", items[0].EvidenceDiagnostics, diag)
	}
}
