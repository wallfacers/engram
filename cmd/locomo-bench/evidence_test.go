package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wallfacers/engram/memory"
)

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
	}, 321)
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
