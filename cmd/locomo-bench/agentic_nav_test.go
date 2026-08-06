package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestNavAccumulatesEvidenceAcrossSteps: evidence from successive search steps
// accumulates in first-seen order and dedups by source_id (state-machine
// core). A later stop references an id seen in an earlier step.
func TestNavAccumulatesEvidenceAcrossSteps(t *testing.T) {
	r, nameToID := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08.",
		"chunk-mayor": "The mayor declared an emergency.",
	})
	floodID := nameToID["chunk-flood"]
	mayorID := nameToID["chunk-mayor"]
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		`{"tool":"search","tool_args":{"query":"mayor emergency"}}`,
		fmt.Sprintf(`{"tool":"stop","tool_args":{"evidence_ids":["%s","%s"],"assembly":"first_n"}}`, floodID, mayorID),
	}}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if traj.FallbackTriggered {
		t.Fatal("successful multi-step stop must not fall back")
	}
	if len(traj.Steps) != 3 {
		t.Fatalf("steps = %d, want [search, search, stop]", len(traj.Steps))
	}
	if traj.Steps[0].Tool != "search" || traj.Steps[1].Tool != "search" || traj.Steps[2].Tool != "stop" {
		t.Fatalf("step sequence = %s,%s,%s", traj.Steps[0].Tool, traj.Steps[1].Tool, traj.Steps[2].Tool)
	}
	if traj.Steps[0].Index != 1 || traj.Steps[1].Index != 2 || traj.Steps[2].Index != 3 {
		t.Fatalf("step indexes = %d,%d,%d", traj.Steps[0].Index, traj.Steps[1].Index, traj.Steps[2].Index)
	}
	if len(traj.FinalEvidence.Evidence) != 2 {
		t.Fatalf("final evidence = %d, want 2", len(traj.FinalEvidence.Evidence))
	}
	if traj.FinalEvidence.Evidence[0].SourceID != floodID || traj.FinalEvidence.Evidence[1].SourceID != mayorID {
		t.Fatalf("final evidence order: %s,%s", traj.FinalEvidence.Evidence[0].SourceID, traj.FinalEvidence.Evidence[1].SourceID)
	}
}

// TestNavPromptIncludesSeenEvidence: after the first search the second step's
// prompt must carry the query plus the id of the evidence already seen
// (grounding the model's tool decisions in real evidence).
func TestNavPromptIncludesSeenEvidence(t *testing.T) {
	r, nameToID := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08.",
	})
	floodID := nameToID["chunk-flood"]
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		`{"tool":"stop","tool_args":{"evidence_ids":["%s"],"assembly":"first_n"}}`,
	}}
	// Note: the stop JSON is templated after construction via fmt.Sprintf below.
	stub.texts[1] = fmt.Sprintf(`{"tool":"stop","tool_args":{"evidence_ids":["%s"],"assembly":"first_n"}}`, floodID)
	if _, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	}); err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if !strings.Contains(stub.lastUser, "flood river") {
		t.Fatalf("prompt must contain the query; got %q", stub.lastUser)
	}
	if !strings.Contains(stub.lastUser, floodID) {
		t.Fatalf("prompt must carry the seen evidence id %s; got %q", floodID, stub.lastUser)
	}
}

// TestNavBudgetAccountsNavTokens: nav_tokens accumulate the sidecar usage and
// answer_context_tokens stay within the cap (008 discipline).
func TestNavBudgetAccountsNavTokens(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area.",
	})
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		`{"tool":"search","tool_args":{"query":"river area"}}`,
		`{"tool":"stop","tool_args":{"evidence_ids":[],"assembly":"first_n"}}`,
	}}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5, AnswerContextCap: 900,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	// 3 sidecar calls, each contributing InputTokens=10 + OutputTokens=20.
	if traj.BudgetUsage.NavTokens != 90 {
		t.Fatalf("nav_tokens = %d, want 90 (3 calls × 30)", traj.BudgetUsage.NavTokens)
	}
	if traj.BudgetUsage.AnswerContextTokens > 900 {
		t.Fatalf("answer_context_tokens = %d exceeds cap 900", traj.BudgetUsage.AnswerContextTokens)
	}
	if traj.BudgetUsage.AnswerContextTokens != traj.FinalEvidence.TotalTokens {
		t.Fatalf("budget answer_context %d != final total %d", traj.BudgetUsage.AnswerContextTokens, traj.FinalEvidence.TotalTokens)
	}
}

// TestNavStopDedupReferencedIds: duplicate ids in stop evidence_ids collapse to
// one evidence entry (dedup in assembly).
func TestNavStopDedupReferencedIds(t *testing.T) {
	r, nameToID := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area.",
	})
	floodID := nameToID["chunk-flood"]
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		fmt.Sprintf(`{"tool":"stop","tool_args":{"evidence_ids":["%s","%s"],"assembly":"first_n"}}`, floodID, floodID),
	}}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if len(traj.FinalEvidence.Evidence) != 1 {
		t.Fatalf("dedup failed: %d evidence entries for a duplicated id", len(traj.FinalEvidence.Evidence))
	}
}
