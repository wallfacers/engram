package main

// 029 navigation trajectory types (data-model.md + contracts/navigation-trajectory.md).
// One NavigationTrajectory is the auditable record of a single query's
// multi-step navigation (FR-007); nav_analyze.py consumes them from
// run-dir/nav-trajectories.jsonl (one JSON object per line).

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"

	"github.com/wallfacers/engram/memory"
)

// navEvidenceToResults converts a navigation evidence bundle into the
// memory.Result shape the answer context expects. Content flows verbatim (the
// answerer only reads text); ID/Score are carried for diagnostics.
func navEvidenceToResults(evidence []NavEvidence) []memory.Result {
	results := make([]memory.Result, 0, len(evidence))
	for _, ev := range evidence {
		results = append(results, memory.Result{
			ID:      ev.SourceID,
			Content: ev.Text,
			Score:   ev.Score,
		})
	}
	return results
}

// navTrajectoryJournal is a concurrency-safe writer for
// run-dir/nav-trajectories.jsonl. Conversations answer in parallel, so writes
// are mutex-guarded; one journal per repeat run.
type navTrajectoryJournal struct {
	mu  sync.Mutex
	f   *os.File
	bw  *bufio.Writer
	enc *json.Encoder
}

func openNavTrajectoryJournal(path string) (*navTrajectoryJournal, error) {
	// Owner-only (0o600): the journal carries raw question text and retrieved
	// memory content — sensitive data — so it must not be world/group readable.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	return &navTrajectoryJournal{f: f, bw: bw, enc: json.NewEncoder(bw)}, nil
}

// Write appends one trajectory line and flushes, so every completed trajectory
// is durable on disk even if a later write or the process fails.
func (j *navTrajectoryJournal) Write(t NavigationTrajectory) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j == nil || j.enc == nil {
		return nil
	}
	if err := j.enc.Encode(t); err != nil {
		return err
	}
	return j.bw.Flush()
}

func (j *navTrajectoryJournal) Close() error {
	if j == nil || j.f == nil {
		return nil
	}
	if j.bw != nil {
		_ = j.bw.Flush()
	}
	return j.f.Close()
}

// NavEvidence is one retrieved evidence unit (data-model.md Evidence).
type NavEvidence struct {
	SourceID    string  `json:"source_id"`
	Text        string  `json:"text"`
	Score       float64 `json:"score,omitempty"`
	RetrievedBy string  `json:"retrieved_by,omitempty"` // semantic | keyword | entity | hybrid
}

// NavStep is a single navigation action and its decision basis
// (data-model.md NavStep).
type NavStep struct {
	Index            int             `json:"index"`
	Tool             string          `json:"tool"` // search | expand_query | follow_entity | stop
	ToolArgs         json.RawMessage `json:"tool_args"`
	ReturnedEvidence []NavEvidence   `json:"returned_evidence"`
	Rationale        string          `json:"rationale"`
	LatencyMS        int64           `json:"latency_ms"`
}

// EvidenceBundle is the final evidence package handed to the answerer; its
// total_tokens MUST stay within the answer-context cap (008 discipline).
type EvidenceBundle struct {
	Evidence    []NavEvidence `json:"evidence"`
	TotalTokens int           `json:"total_tokens"`
	Assembly    string        `json:"assembly"` // first_n | dedup
}

// BudgetUsage keeps navigation cost separate from answer-context cost
// (contracts/navigation-trajectory.md budget_usage).
type BudgetUsage struct {
	Steps              int `json:"steps"`
	NavTokens          int `json:"nav_tokens"`
	AnswerContextTokens int `json:"answer_context_tokens"`
}

// NavigationTrajectory is the full record of one query's navigation
// (data-model.md NavigationTrajectory).
type NavigationTrajectory struct {
	QuestionID        string          `json:"question_id"`
	Query             string          `json:"query"`
	Steps             []NavStep       `json:"steps"`
	FinalEvidence     EvidenceBundle  `json:"final_evidence"`
	BudgetUsage       BudgetUsage     `json:"budget_usage"`
	FallbackTriggered bool            `json:"fallback_triggered"`
	Answer            string          `json:"answer,omitempty"`
}

// MarshalJSONLine serialises the trajectory as one JSONL line (no trailing
// newline). Invalid for nav_analyze.py if fields are missing.
func (t NavigationTrajectory) MarshalJSONLine() ([]byte, error) {
	return json.Marshal(t)
}
