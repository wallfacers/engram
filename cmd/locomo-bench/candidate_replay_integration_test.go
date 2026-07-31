package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// TestFormalCandidateReplaySharedAcrossMaterializations proves the T114
// contract end to end: the first compiler-arm materialize retrieves and
// persists a candidate replay; a second materialize reads the identical
// candidates with zero retrieval calls; a tampered replay is rejected
// fail-closed.
func TestFormalCandidateReplaySharedAcrossMaterializations(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	entries := memory.NewEntryStore(st.DB())
	ledger := entries.Ledger()
	const sourceText = "Caroline confirmed the flight to Tokyo leaves at 18:30."
	evidence, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "conv0-sess1",
		Speaker:          "Caroline",
		Ordinal:          0,
		Content:          sourceText,
		OccurredAt:       ptrTime(time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)),
		RecordedAt:       time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	start, end := 0, len(sourceText)
	const spanText = "Caroline confirmed the flight to Tokyo leaves at 18:30."
	if err := entries.UpsertWithSources(ctx, &memory.Entry{
		Name: "flight-marker", Trigger: "tokyo flight", Content: "tokyo flight 18:30 departure", Category: "fact",
	}, []memory.EvidenceRef{{
		EvidenceID: evidence[0].ID, SourceOrder: 0, StartChar: &start, EndChar: &end,
		SpanDigest: strings.TrimPrefix(evalTextDigest(spanText), "sha256:"),
	}}); err != nil {
		t.Fatal(err)
	}

	protocol := sourceTestProtocol()
	qa := locomoQA{
		QuestionID: "locomo:0:0", Question: "tokyo flight departure", Category: 4,
		Evidence: []string{"D1:1"},
	}
	runDir := t.TempDir()
	opt := options{
		answerModel:    protocol.Models.Answerer.ID,
		formalCounter:  lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
		formalEvidence: ledger,
		compilerArm:    "exact_token",
		runDir:         runDir,
	}
	retriever := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil)
	projections := memory.NewProjectionStore(st.DB())
	turnEvidence := map[string]string{"D1:1": evidence[0].ID}

	// (1) First materialize: retrieves and writes the replay. The full
	// source-span/trace admission of the exact-token arm is owned by T069;
	// this test only guards the T114 replay contract (no replay-level failure,
	// one retrieval call, replay file written).
	first := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, qa, nil, turnEvidence)
	if containsString(first.InvalidReasons, "candidate_replay_drift") || containsString(first.InvalidReasons, "retrieval_failed") || containsString(first.InvalidReasons, "candidate_replay_write_failed") {
		t.Fatalf("first materialize failed at the replay layer: %v", first.InvalidReasons)
	}
	if first.Candidate.RetrievalCalls != 1 {
		t.Fatalf("first materialize retrieval calls = %d, want 1", first.Candidate.RetrievalCalls)
	}
	replayPath := candidateReplayPath(runDir, qa.QuestionID)
	if _, err := os.Stat(replayPath); err != nil {
		t.Fatalf("candidate replay not written at %s: %v", replayPath, err)
	}

	// (2) Second materialize: replay serves the identical candidates with
	// zero retrieval calls (the retriever is not invoked at all).
	second := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, qa, nil, turnEvidence)
	if containsString(second.InvalidReasons, "candidate_replay_drift") || containsString(second.InvalidReasons, "retrieval_failed") {
		t.Fatalf("replayed materialize failed at the replay layer: %v", second.InvalidReasons)
	}
	if second.Candidate.RetrievalCalls != 0 {
		t.Fatalf("replayed materialize retrieval calls = %d, want 0 (post-freeze)", second.Candidate.RetrievalCalls)
	}
	if second.Candidate.CandidateSetDigest != first.Candidate.CandidateSetDigest {
		t.Fatal("replayed materialize produced a different candidate set (byte drift)")
	}

	// (3) Tamper with the replay: the run must fail closed.
	raw, err := os.ReadFile(replayPath) //nolint:gosec // test artifact
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), "18:30", "19:45", 1)
	if err := os.WriteFile(replayPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	third := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, qa, nil, turnEvidence)
	drifted := false
	for _, reason := range third.InvalidReasons {
		if reason == "candidate_replay_drift" {
			drifted = true
		}
	}
	if !drifted {
		t.Fatalf("tampered replay was not rejected fail-closed: %v", third.InvalidReasons)
	}
}
