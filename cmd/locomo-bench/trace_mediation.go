package main

// 030 US2 grounded-evidence mediation (specs/030, contracts/grounded-trace.md,
// data-model.md). A sidecar (opt-in, default off) organises the retrieved
// candidate set into a MemChain-style packet — plan → grounded trace → explicit
// actions → final evidence E — and only E is exposed to the answerer. The
// fail-closed gate (trace_gate.go) keeps every citation inside the candidate
// boundary. Engine untouched (FR-001).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/wallfacers/engram/memory"
)

// tracePlan is the question-conditioned evidence plan (z in MemChain).
type tracePlan struct {
	Intent              string   `json:"intent"`
	MemoryTypes         []string `json:"memory_types,omitempty"`
	TemporalScope       string   `json:"temporal_scope,omitempty"`
	EvidenceRequirement string   `json:"evidence_requirement,omitempty"`
	TargetCount         int      `json:"target_count,omitempty"`
}

// traceStep is one grounded trace step: an evidence role, the cited candidate
// IDs, the grounded statement, and the relation to the next step.
type traceStep struct {
	Role         string   `json:"role"`
	CitedIDs     []string `json:"cited_ids"`
	Statement    string   `json:"statement"`
	NextRelation string   `json:"next_relation,omitempty"`
}

// traceAction is one explicit memory action from the MemChain vocabulary
// KEEP/DROP/MERGE/REFINE/ADD. KEEP/MERGE/REFINE/ADD produce evidence; DROP does
// not.
type traceAction struct {
	Action      string   `json:"action"`
	CitedIDs    []string `json:"cited_ids"`
	Rationale   string   `json:"rationale,omitempty"`
	Transformed string   `json:"transformed,omitempty"`
}

// traceEvidence is one final evidence statement exposed to the answerer, with
// the candidate IDs supporting it.
type traceEvidence struct {
	Text     string   `json:"text"`
	CitedIDs []string `json:"cited_ids"`
}

// tracePacket is the full sidecar output: plan → trace → actions → evidence.
type tracePacket struct {
	Plan     tracePlan      `json:"plan"`
	Trace    []traceStep    `json:"trace"`
	Actions  []traceAction  `json:"actions"`
	Evidence []traceEvidence `json:"evidence"`
}

// traceMediationInput is the fail-closed gate input: the raw sidecar packet and
// the closed candidate boundary it must stay inside.
type traceMediationInput struct {
	Raw          string          // sidecar JSON packet
	CandidateIDs map[string]bool // closed candidate boundary (IDs(C_q))
}

// traceGateStatus is the fail-closed gate outcome (contracts/grounded-trace.md,
// data-model.md FailClosedGate).
type traceGateStatus string

const (
	traceGateValid           traceGateStatus = "valid"
	traceGateInvalidCitation traceGateStatus = "invalid_citation"
	traceGateParseFailed     traceGateStatus = "parse_failed"
	traceGateFallback        traceGateStatus = "fallback"
)

// traceSystemPrompt instructs the sidecar to emit a single grounded-trace
// packet (contracts/grounded-trace.md schema). Strict JSON, closed boundary.
const traceSystemPrompt = `You organise retrieved memories into grounded evidence for answering one question.
Emit STRICT JSON only, one object, no prose or code fences:
{
  "plan": {"intent": "temporal_state_tracking|fact_lookup|multi_hop|preference_recall", "memory_types": [...], "temporal_scope": "current|recent|historical|any", "evidence_requirement": "...", "target_count": 1},
  "trace": [{"role": "old_state|update|support|contrast|resolution", "cited_ids": [...], "statement": "...", "next_relation": "..."}],
  "actions": [{"action": "KEEP|DROP|MERGE|REFINE|ADD", "cited_ids": [...], "rationale": "..."}],
  "evidence": [{"text": "...", "cited_ids": [...]}]
}
Rules:
- Every "cited_ids" entry MUST be one of the candidate IDs provided after CANDIDATES:. Never invent IDs.
- "trace" is an ordered grounded chain; each step cites at least one candidate.
- "evidence" is the final answer-facing text; each statement MUST cite at least one candidate AND be traceable to a trace step.
- Never introduce facts absent from the provided candidates.
- "evidence" MUST be compact and sufficient to answer the question.`

// traceUserPrompt renders the question plus the closed candidate set for the
// sidecar. Candidates are rendered in the same [event:]/[recorded:] line shape
// the answering model consumes.
func traceUserPrompt(question string, hits []memory.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "QUESTION: %s\n\nCANDIDATES:\n", question)
	for _, h := range hits {
		rm := toMemories([]memory.Result{h})[0]
		fmt.Fprintf(&b, "[%s] %s\n", h.Name, rm.Line())
	}
	return b.String()
}

// evidenceFromTrace converts the gated final evidence E into answer-context
// results (each statement keeps its first supporting candidate as its source
// ID). Used to render E through the existing prompt builders.
func evidenceFromTrace(evidence []traceEvidence) []memory.Result {
	out := make([]memory.Result, 0, len(evidence))
	for _, ev := range evidence {
		id := ""
		if len(ev.CitedIDs) > 0 {
			id = ev.CitedIDs[0]
		}
		out = append(out, memory.Result{Name: id, Content: ev.Text})
	}
	return out
}

// traceGateRecord is one question's fail-closed gate outcome (audit).
type traceGateRecord struct {
	QuestionID    string          `json:"question_id"`
	Status        traceGateStatus `json:"status"`
	EvidenceCount int             `json:"evidence_count"`
	Retried       bool            `json:"retried,omitempty"`
}

// traceGateJournal is a concurrency-safe writer for run-dir/trace-gate.jsonl.
type traceGateJournal struct {
	mu  sync.Mutex
	f   *os.File
	bw  *bufio.Writer
	enc *json.Encoder
}

func openTraceGateJournal(path string) (*traceGateJournal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	return &traceGateJournal{f: f, bw: bw, enc: json.NewEncoder(bw)}, nil
}

func (j *traceGateJournal) Write(record traceGateRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j == nil || j.enc == nil {
		return nil
	}
	if err := j.enc.Encode(record); err != nil {
		return err
	}
	return j.bw.Flush()
}

func (j *traceGateJournal) Close() error {
	if j == nil || j.f == nil {
		return nil
	}
	if j.bw != nil {
		_ = j.bw.Flush()
	}
	return j.f.Close()
}
