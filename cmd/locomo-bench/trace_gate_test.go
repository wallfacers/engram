package main

// 030 US2 fail-closed gate tests (specs/030, T012). Assert the gate's pure-Go
// deterministic behaviour: closed-boundary citation, traceability, empty-
// evidence fallback, and parse-failure status. Offline (no sidecar call).

import (
	"testing"
)

const validTraceRaw = `{
  "plan": {"intent": "temporal_state_tracking", "temporal_scope": "current"},
  "trace": [
    {"role": "old_state", "cited_ids": ["candidate-123"], "statement": "old workplace"},
    {"role": "update", "cited_ids": ["candidate-456"], "statement": "new workplace"}
  ],
  "actions": [
    {"action": "DROP", "cited_ids": ["candidate-123"], "rationale": "superseded"},
    {"action": "KEEP", "cited_ids": ["candidate-456"], "rationale": "current"}
  ],
  "evidence": [
    {"text": "Daniel now works at Riverbend.", "cited_ids": ["candidate-456"]}
  ]
}`

func candidateBoundary() map[string]bool {
	return map[string]bool{"candidate-123": true, "candidate-456": true, "candidate-789": true}
}

func TestTraceGateValid(t *testing.T) {
	pkt, evidence, status, err := mediateTrace(traceMediationInput{Raw: validTraceRaw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateValid {
		t.Fatalf("status = %s, want valid", status)
	}
	if len(evidence) != 1 || evidence[0].Text == "" {
		t.Fatalf("expected one non-empty evidence, got %d", len(evidence))
	}
	if len(pkt.Trace) != 2 || len(pkt.Actions) != 2 {
		t.Fatalf("packet trace/actions mutated: %d/%d", len(pkt.Trace), len(pkt.Actions))
	}
}

func TestTraceGateInvalidCitationDropped(t *testing.T) {
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [{"action": "KEEP", "cited_ids": ["candidate-123"], "rationale": "r"}],
	  "evidence": [
	    {"text": "good", "cited_ids": ["candidate-123"]},
	    {"text": "hallucinated", "cited_ids": ["outside-id"]}
	  ]
	}`
	pkt, evidence, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateInvalidCitation {
		t.Fatalf("status = %s, want invalid_citation", status)
	}
	if len(evidence) != 1 || evidence[0].Text != "good" {
		t.Fatalf("illegal citation must be dropped, keeping good; got %+v", evidence)
	}
	if len(pkt.Evidence) != 1 {
		t.Fatalf("packet evidence = %d, want 1", len(pkt.Evidence))
	}
}

func TestTraceGateAllInvalidFallsBack(t *testing.T) {
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [],
	  "evidence": [{"text": "hallucinated", "cited_ids": ["outside-id"]}]
	}`
	_, evidence, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateFallback {
		t.Fatalf("status = %s, want fallback", status)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence = %d, want 0", len(evidence))
	}
}

func TestTraceGateParseFailed(t *testing.T) {
	_, _, status, err := mediateTrace(traceMediationInput{Raw: `{not json`, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace parse: %v", err)
	}
	if status != traceGateParseFailed {
		t.Fatalf("status = %s, want parse_failed", status)
	}
}

func TestTraceGateEmptyEvidenceFallsBack(t *testing.T) {
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [],
	  "evidence": []
	}`
	_, _, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateFallback {
		t.Fatalf("status = %s, want fallback", status)
	}
}

func TestTraceGateUntraceableDropped(t *testing.T) {
	// Evidence cites a valid candidate but no trace step cites it → untraceable.
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [],
	  "evidence": [{"text": "untraceable", "cited_ids": ["candidate-456"]}]
	}`
	_, evidence, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateFallback || len(evidence) != 0 {
		t.Fatalf("untraceable evidence must fall back: status=%s evidence=%d", status, len(evidence))
	}
}

func TestTraceGateEmptyCitationRejected(t *testing.T) {
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [],
	  "evidence": [{"text": "no citation", "cited_ids": []}]
	}`
	_, _, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateFallback {
		t.Fatalf("empty citation must fail: status=%s", status)
	}
}

func TestTraceGateActionOutsideBoundary(t *testing.T) {
	raw := `{
	  "plan": {"intent": "fact_lookup"},
	  "trace": [{"role": "seed", "cited_ids": ["candidate-123"], "statement": "s"}],
	  "actions": [{"action": "KEEP", "cited_ids": ["outside"], "rationale": "bad"}],
	  "evidence": [{"text": "good", "cited_ids": ["candidate-123"]}]
	}`
	_, evidence, status, err := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	if err != nil {
		t.Fatalf("mediateTrace: %v", err)
	}
	if status != traceGateInvalidCitation {
		t.Fatalf("status = %s, want invalid_citation", status)
	}
	if len(evidence) != 1 || len(pktActionsOf(raw)) != 0 {
		t.Fatalf("good evidence must survive and the outside-boundary action must be dropped: %+v", evidence)
	}
}

// pktActionsOf is a tiny helper that re-parses raw to check action count.
func pktActionsOf(raw string) []traceAction {
	pkt, _, _, _ := mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: candidateBoundary()})
	return pkt.Actions
}
