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
	B0Continuity        *evalB0ContinuityRun      `json:"b0_continuity,omitempty"`
	Formal022           *evalFormalQuestionRun    `json:"formal_022,omitempty"`
}

// loadFormalQuestionRuns reads the per-repetition journal records used to
// produce immutable 022 artifacts.  Unlike ordinary resume, a malformed or
// missing formal payload is a hard error: silently dropping it would turn a
// partial answer run into an apparently complete majority denominator.
func loadFormalQuestionRuns(runDirs []string, arm string) ([][]result, error) {
	runs := make([][]result, 0, len(runDirs))
	for _, runDir := range runDirs {
		path := filepath.Join(runDir, fmt.Sprintf("results-%s.jsonl", arm))
		var current []result
		if err := scanResultsJSONLStrict(path, func(item result) error {
			if item.Formal022 == nil {
				return fmt.Errorf("formal result %q has no 022 payload", item.QuestionID)
			}
			current = append(current, item)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("read formal journal %s: %w", path, err)
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("formal journal %s has no results", path)
		}
		runs = append(runs, current)
	}
	return runs, nil
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
	path string
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
	return &journal{f: f, w: bufio.NewWriter(f), seen: seen, path: path}, nil
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

func (j *journal) count() int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.seen)
}

// formalSnapshotStrict re-reads an existing result journal without legacy
// resume's malformed-line tolerance. Formal replay must refuse a torn,
// duplicate, or non-formal first repetition before it makes any later answer
// calls; detecting that only during final artifact materialization is too late.
func (j *journal) formalSnapshotStrict() (map[resultKey]result, error) {
	if j == nil {
		return nil, fmt.Errorf("formal result journal is unavailable")
	}
	j.mu.Lock()
	path := j.path
	fallback := make(map[resultKey]result, len(j.seen))
	for key, item := range j.seen {
		fallback[key] = item
	}
	j.mu.Unlock()
	if path == "" {
		return fallback, nil
	}

	prior := make(map[resultKey]result)
	questionKeys := make(map[string]resultKey)
	if err := scanResultsJSONLStrict(path, func(item result) error {
		key := resultKey{Conv: item.Conv, Q: item.Q}
		if _, duplicate := prior[key]; duplicate {
			return fmt.Errorf("duplicate formal result conv=%d q=%d", key.Conv, key.Q)
		}
		if item.Formal022 == nil {
			return fmt.Errorf("formal result conv=%d q=%d has no 022 payload", key.Conv, key.Q)
		}
		questionID := resultID(item)
		if questionID == "" {
			return fmt.Errorf("formal result conv=%d q=%d has no question ID", key.Conv, key.Q)
		}
		if previous, duplicate := questionKeys[questionID]; duplicate {
			return fmt.Errorf("duplicate formal question %q at conv=%d q=%d and conv=%d q=%d", questionID, previous.Conv, previous.Q, key.Conv, key.Q)
		}
		prior[key] = item
		questionKeys[questionID] = key
		return nil
	}); err != nil {
		return nil, err
	}
	return prior, nil
}

func (j *journal) writeResult(r result, syncFile bool) error {
	if j == nil {
		return fmt.Errorf("result journal is unavailable")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.w.Write(b); err != nil {
		return err
	}
	if err := j.w.WriteByte('\n'); err != nil {
		return err
	}
	if err := j.w.Flush(); err != nil {
		return err
	}
	if syncFile {
		if err := j.f.Sync(); err != nil {
			return err
		}
	}
	j.seen[resultKey{Conv: r.Conv, Q: r.Q}] = r
	return nil
}

func (j *journal) write(r result) {
	_ = j.writeResult(r, false)
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
