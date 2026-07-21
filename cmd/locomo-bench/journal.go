package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// result is one graded question outcome, persisted as a JSONL line for resume.
// It deliberately never carries any credential — only benchmark content.
type result struct {
	Conv                int                       `json:"conv"`
	Q                   int                       `json:"q"`
	QuestionID          string                    `json:"question_id,omitempty"`
	Category            int                       `json:"category"`
	CategoryName        string                    `json:"category_name,omitempty"`
	QuestionType        string                    `json:"question_type,omitempty"`
	Adversarial         bool                      `json:"adversarial,omitempty"`
	RetrievalFlags      string                    `json:"retrieval_flags"`
	AnswerRegime        string                    `json:"answer_regime"`
	Correct             bool                      `json:"correct"`
	Question            string                    `json:"question"`
	Gold                string                    `json:"gold"`
	Predicted           string                    `json:"predicted"`
	HardGated           bool                      `json:"hard_gated,omitempty"`
	InputTokens         int                       `json:"input_tokens,omitempty"`
	OutputTokens        int                       `json:"output_tokens,omitempty"`
	AnswerContextTokens int                       `json:"answer_context_tokens,omitempty"`
	SweepUsed           bool                      `json:"sweep_used,omitempty"`
	SweepOverBudget     bool                      `json:"sweep_over_budget,omitempty"`
	EvidenceDiagnostics *sweepEvidenceDiagnostics `json:"evidence_diagnostics,omitempty"`
}

type resultKey struct {
	Conv int
	Q    int
}

// journal is an append-only JSONL writer with a prior-run index for resume.
// Safe for concurrent writers (conversations and questions run in parallel).
type journal struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	seen map[resultKey]result
}

// openJournal opens (creating if needed) the run's JSONL file for the given
// retrieval mode, preloading any prior results for resume.
func openJournal(runDir, retrieval string) (*journal, error) {
	path := filepath.Join(runDir, fmt.Sprintf("results-%s.jsonl", retrieval))
	seen, err := loadPrior(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	return &journal{f: f, w: bufio.NewWriter(f), seen: seen}, nil
}

func loadPrior(path string) (map[resultKey]result, error) {
	seen := map[resultKey]result{}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return seen, nil
		}
		return nil, fmt.Errorf("read prior journal: %w", err)
	}
	if err := scanResultsJSONL(path, func(r result) {
		seen[resultKey{Conv: r.Conv, Q: r.Q}] = r
	}); err != nil {
		return nil, fmt.Errorf("read prior journal: %w", err)
	}
	return seen, nil
}

func (j *journal) lookup(k resultKey) (result, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.seen[k]
	return r, ok
}

func (j *journal) write(r result) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.w.Write(b)
	_ = j.w.WriteByte('\n')
	_ = j.w.Flush() // flush each line so an interrupted run resumes cleanly
	j.seen[resultKey{Conv: r.Conv, Q: r.Q}] = r
}

func (j *journal) Close() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = j.w.Flush()
	_ = j.f.Close()
}
