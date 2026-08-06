package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// newTestNavRetriever builds an in-memory chunk store with the given
// name→content entries and returns the hybrid retriever plus the persisted
// name→entry-ID map (for stop evidence_ids referencing real ids).
func newTestNavRetriever(t *testing.T, entries map[string]string) (*memory.Retriever, map[string]string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	es := memory.NewEntryStore(st.DB())
	nameToID := make(map[string]string, len(entries))
	for name, content := range entries {
		e := &memory.Entry{Name: name, Content: content}
		if err := es.Upsert(ctx, e); err != nil {
			t.Fatal(err)
		}
		nameToID[name] = e.ID
	}
	vs := memory.NewVectorStore(st.DB())
	r := memory.NewRetriever(es, vs, nil)
	return r, nameToID
}

func navCallFromStub(stub *stubPlannerProvider) usageModelCaller {
	return newUsageModelCaller(stub, "stub-model", 512, 0, "nav", nil)
}

// TestNavTrajectoryMarshalRoundTrip: a full trajectory survives JSONL
// round-tripping with every contract field intact (contracts/navigation-trajectory.md).
func TestNavTrajectoryMarshalRoundTrip(t *testing.T) {
	traj := NavigationTrajectory{
		QuestionID: "conv-2-q-42",
		Query:      "What area was hit by a flood?",
		Steps: []NavStep{
			{
				Index:    1,
				Tool:     "search",
				ToolArgs: json.RawMessage(`{"query":"area hit by a flood","k":8}`),
				ReturnedEvidence: []NavEvidence{
					{SourceID: "chunk-12", Text: "A flood hit the river area.", Score: 0.82, RetrievedBy: "hybrid"},
				},
				Rationale: "raw query first",
				LatencyMS: 340,
			},
			{
				Index:    2,
				Tool:     "stop",
				ToolArgs: json.RawMessage(`{"evidence_ids":["chunk-12"],"assembly":"first_n"}`),
				Rationale: "enough",
				LatencyMS: 0,
			},
		},
		FinalEvidence: EvidenceBundle{
			Evidence:    []NavEvidence{{SourceID: "chunk-12", Text: "A flood hit the river area."}},
			TotalTokens: 60,
			Assembly:    "first_n",
		},
		BudgetUsage: BudgetUsage{Steps: 2, NavTokens: 140, AnswerContextTokens: 60},
		Answer:      "the river area",
	}
	line, err := traj.MarshalJSONLine()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got NavigationTrajectory
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.QuestionID != traj.QuestionID || got.Query != traj.Query {
		t.Fatalf("header mismatch: %#v", got)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(got.Steps))
	}
	if got.Steps[0].Tool != "search" || got.Steps[0].ToolArgs == nil || got.Steps[0].ReturnedEvidence[0].SourceID != "chunk-12" {
		t.Fatalf("step[0]: %#v", got.Steps[0])
	}
	if got.Steps[1].Tool != "stop" {
		t.Fatalf("step[1].tool = %q, want stop", got.Steps[1].Tool)
	}
	if got.FinalEvidence.TotalTokens != 60 || got.FinalEvidence.Assembly != "first_n" {
		t.Fatalf("final_evidence: %#v", got.FinalEvidence)
	}
	if got.BudgetUsage.NavTokens != 140 || got.BudgetUsage.AnswerContextTokens != 60 {
		t.Fatalf("budget_usage: %#v", got.BudgetUsage)
	}
	if got.Answer != "the river area" {
		t.Fatalf("answer: %q", got.Answer)
	}
}

// TestNavTrajectoryAnswerOmittedWhenEmpty: answer uses omitempty so pending
// (not-yet-answered) trajectories don't carry an empty field.
func TestNavTrajectoryAnswerOmittedWhenEmpty(t *testing.T) {
	line, err := NavigationTrajectory{QuestionID: "q1"}.MarshalJSONLine()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), `"answer"`) {
		t.Fatalf("empty answer must be omitted: %s", line)
	}
}

// TestNavFailClosedWhenNoModel: navigation with no sidecar model must collapse
// to the single-shot retrieval path (SC-004), never an empty answer.
func TestNavFailClosedWhenNoModel(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08. The mayor declared an emergency.",
	})
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{FallbackTopK: 5})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if !traj.FallbackTriggered {
		t.Fatal("want fallback_triggered=true with no navigation model")
	}
	if len(traj.FinalEvidence.Evidence) == 0 {
		t.Fatal("fail-closed must produce non-empty single-shot evidence")
	}
	if traj.FinalEvidence.Evidence[0].SourceID == "" {
		t.Fatalf("evidence missing source_id: %#v", traj.FinalEvidence.Evidence[0])
	}
	if len(traj.Steps) != 0 {
		t.Fatalf("no navigation steps expected on immediate fallback, got %d", len(traj.Steps))
	}
}

// TestNavFailClosedOnParseFailure: an unparsable sidecar response must fail
// closed to single-shot (never a partial trajectory).
func TestNavFailClosedOnParseFailure(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08.",
	})
	stub := &stubPlannerProvider{text: "I don't know how to navigate."}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if !traj.FallbackTriggered {
		t.Fatal("want fallback on unparsable tool call")
	}
	if len(traj.FinalEvidence.Evidence) == 0 {
		t.Fatal("fail-closed evidence must be non-empty")
	}
	if stub.calls != 1 {
		t.Fatalf("expect exactly 1 sidecar call, got %d", stub.calls)
	}
}

// TestNavFailClosedOnStepCap: a model that never stops must hit the step cap
// and fail closed, recording every step for audit.
func TestNavFailClosedOnStepCap(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08.",
	})
	stub := &stubPlannerProvider{text: `{"tool":"search","tool_args":{"query":"flood river"}}`}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 3, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if !traj.FallbackTriggered {
		t.Fatal("want fallback on step-cap exhaustion")
	}
	if len(traj.Steps) != 3 {
		t.Fatalf("steps = %d, want 3 (cap reached)", len(traj.Steps))
	}
	for i, s := range traj.Steps {
		if s.Index != i+1 {
			t.Fatalf("step[%d].index = %d, want %d", i, s.Index, i+1)
		}
	}
	if traj.BudgetUsage.Steps != 3 {
		t.Fatalf("budget_usage.steps = %d, want 3", traj.BudgetUsage.Steps)
	}
	if traj.BudgetUsage.NavTokens <= 0 {
		t.Fatalf("nav_tokens must account sidecar calls, got %d", traj.BudgetUsage.NavTokens)
	}
	// The fallback must keep the navigation's accumulated seen evidence (the
	// focused multi-query search did real work), not discard it for a fresh
	// single-shot retrieval.
	stepSeen := map[string]bool{}
	for _, s := range traj.Steps {
		for _, ev := range s.ReturnedEvidence {
			stepSeen[ev.SourceID] = true
		}
	}
	for _, ev := range traj.FinalEvidence.Evidence {
		if !stepSeen[ev.SourceID] {
			t.Fatalf("final evidence %s is not from the navigation's seen set", ev.SourceID)
		}
	}
}

// TestNavStopAssemblesEvidence: search → stop with the retrieved id must
// assemble a non-fallback bundle whose evidence references the seen id, and the
// trajectory's last step must be stop (contracts validation).
func TestNavStopAssemblesEvidence(t *testing.T) {
	r, nameToID := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08. The mayor declared an emergency.",
	})
	floodID := nameToID["chunk-flood"]
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		fmt.Sprintf(`{"tool":"stop","tool_args":{"evidence_ids":["%s"],"assembly":"first_n"}}`, floodID),
	}}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if traj.FallbackTriggered {
		t.Fatal("successful stop must not be a fallback")
	}
	if len(traj.Steps) != 2 {
		t.Fatalf("steps = %d, want [search, stop]", len(traj.Steps))
	}
	if traj.Steps[len(traj.Steps)-1].Tool != "stop" {
		t.Fatalf("last step tool = %q, want stop", traj.Steps[len(traj.Steps)-1].Tool)
	}
	if len(traj.FinalEvidence.Evidence) != 1 || traj.FinalEvidence.Evidence[0].SourceID != floodID {
		t.Fatalf("final evidence: %#v", traj.FinalEvidence.Evidence)
	}
	if traj.BudgetUsage.AnswerContextTokens != traj.FinalEvidence.TotalTokens {
		t.Fatalf("budget answer_context %d != final total %d", traj.BudgetUsage.AnswerContextTokens, traj.FinalEvidence.TotalTokens)
	}
}

// TestNavEmptyStopFailsClosed: a stop that references no seen ids would produce
// an empty answer bundle, which must collapse to fail-closed (never empty).
func TestNavEmptyStopFailsClosed(t *testing.T) {
	r, _ := newTestNavRetriever(t, map[string]string{
		"chunk-flood": "A flood hit the river area on 2023-05-08.",
	})
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"flood river"}}`,
		`{"tool":"stop","tool_args":{"evidence_ids":["no-such-id"],"assembly":"first_n"}}`,
	}}
	traj, err := runNavigation(context.Background(), "q1", "flood river", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if !traj.FallbackTriggered {
		t.Fatal("empty stop must fail closed")
	}
	if len(traj.FinalEvidence.Evidence) == 0 {
		t.Fatal("fail-closed evidence must be non-empty")
	}
}

// TestNavEnsureMinEvidenceChunkFirst: the final bundle must lead with
// chunk-level evidence (full contexts) ahead of short extracted facts, so the
// answerer gets a comparable context to the single-shot baseline (observed
// fact-only bundles carry ~440 tokens vs ~2700 for 12 chunks).
func TestNavEnsureMinEvidenceChunkFirst(t *testing.T) {
	fact := []NavEvidence{
		{SourceID: "f1", Text: "Evan plans to ski in Canada."},
		{SourceID: "f2", Text: "Andrew adopted a dog named Scout."},
	}
	chunk := []NavEvidence{
		{SourceID: "c1", Text: strings.Repeat("A", 900)},
		{SourceID: "c2", Text: strings.Repeat("B", 900)},
	}
	bundle := ensureMinEvidence(EvidenceBundle{Evidence: fact}, chunk, nil, 12, 3600)
	if len(bundle.Evidence) < 3 {
		t.Fatalf("want chunks padded in, got %d", len(bundle.Evidence))
	}
	for i, ev := range bundle.Evidence {
		if i < 2 && len(ev.Text) <= 300 {
			t.Fatalf("chunk-first violated at %d: %q", i, ev.Text[:min(len(ev.Text), 30)])
		}
	}
	if bundle.Evidence[0].SourceID != "c1" || bundle.Evidence[1].SourceID != "c2" {
		t.Fatalf("chunk order = %s,%s; want c1,c2", bundle.Evidence[0].SourceID, bundle.Evidence[1].SourceID)
	}
	if bundle.TotalTokens > 3600 {
		t.Fatalf("tokens %d exceed cap", bundle.TotalTokens)
	}
}

// TestNavBudgetCapTruncates: an over-cap stop bundle truncates to the
// answer-context cap (008 discipline).
func TestNavBudgetCapTruncates(t *testing.T) {
	long := strings.Repeat("x", 4000) // ≈1000 estimate tokens > cap
	r, nameToID := newTestNavRetriever(t, map[string]string{"big": long})
	bigID := nameToID["big"]
	stub := &stubPlannerProvider{texts: []string{
		`{"tool":"search","tool_args":{"query":"xxx"}}`,
		fmt.Sprintf(`{"tool":"stop","tool_args":{"evidence_ids":["%s"],"assembly":"first_n"}}`, bigID),
	}}
	cap := 200
	traj, err := runNavigation(context.Background(), "q1", "xxx", r, navConfig{
		Call: navCallFromStub(stub), MaxSteps: 4, NavK: 3, FallbackTopK: 5, AnswerContextCap: cap,
	})
	if err != nil {
		t.Fatalf("runNavigation: %v", err)
	}
	if traj.FinalEvidence.TotalTokens > cap {
		t.Fatalf("final tokens %d exceed cap %d", traj.FinalEvidence.TotalTokens, cap)
	}
	if traj.BudgetUsage.AnswerContextTokens > cap {
		t.Fatalf("budget answer_context %d exceeds cap %d", traj.BudgetUsage.AnswerContextTokens, cap)
	}
}
