package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const formalCallJournalFile = "formal_calls.jsonl"

type formalCallState string

const (
	formalCallStarted   formalCallState = "started"
	formalCallCompleted formalCallState = "completed"
	formalCallFailed    formalCallState = "failed"
)

type formalCallKey struct {
	Conv       int
	Q          int
	Repetition int
}

type formalCallRecord struct {
	Schema       string          `json:"schema"`
	ProtocolHash string          `json:"protocol_hash"`
	Conv         int             `json:"conv"`
	Q            int             `json:"q"`
	QuestionID   string          `json:"question_id"`
	Repetition   int             `json:"repetition"`
	FrozenDigest string          `json:"frozen_digest"`
	InputDigest  string          `json:"input_digest"`
	State        formalCallState `json:"state"`
	ResultDigest string          `json:"result_digest,omitempty"`
	Result       *result         `json:"result,omitempty"`
}

func (record formalCallRecord) key() formalCallKey {
	return formalCallKey{Conv: record.Conv, Q: record.Q, Repetition: record.Repetition}
}

type formalCallStatus struct {
	started  formalCallRecord
	terminal *formalCallRecord
}

// formalCallJournal makes the external-call count crash-auditable. A valid
// answer path syncs STARTED after exact preflight and before the provider call,
// then syncs a terminal record containing the complete result before the
// ordinary per-repeat journal is derived. An orphan STARTED is ambiguous and
// permanently invalidates resume; it is never retried.
type formalCallJournal struct {
	mu           sync.Mutex
	f            *os.File
	w            *bufio.Writer
	protocolHash string
	statuses     map[formalCallKey]formalCallStatus
	err          error
	closed       bool
}

func openFormalCallJournal(runDir, protocolHash string) (*formalCallJournal, error) {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(protocolHash) == "" {
		return nil, fmt.Errorf("formal call journal requires run directory and protocol hash")
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create formal call journal directory: %w", err)
	}
	path := filepath.Join(runDir, formalCallJournalFile)
	statuses := make(map[formalCallKey]formalCallStatus)
	if prior, err := os.Open(path); err == nil { //nolint:gosec // operator-selected formal artifact
		scanner := bufio.NewScanner(prior)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var record formalCallRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = prior.Close()
				return nil, fmt.Errorf("decode %s line %d: %w; use a fresh --run-dir", path, line, err)
			}
			if err := validateFormalCallRecord(record, protocolHash); err != nil {
				_ = prior.Close()
				return nil, fmt.Errorf("validate %s line %d: %w; use a fresh --run-dir", path, line, err)
			}
			key := record.key()
			status := statuses[key]
			switch record.State {
			case formalCallStarted:
				if status.started.State != "" || status.terminal != nil {
					_ = prior.Close()
					return nil, fmt.Errorf("%s line %d duplicates or reorders STARTED; use a fresh --run-dir", path, line)
				}
				status.started = record
			case formalCallCompleted:
				if status.started.State == "" || status.terminal != nil || !sameFormalCallIdentity(status.started, record) {
					_ = prior.Close()
					return nil, fmt.Errorf("%s line %d has COMPLETED without its matching STARTED; use a fresh --run-dir", path, line)
				}
				status.terminal = &record
			case formalCallFailed:
				if status.terminal != nil || (status.started.State != "" && !sameFormalCallIdentity(status.started, record)) {
					_ = prior.Close()
					return nil, fmt.Errorf("%s line %d conflicts with prior call state; use a fresh --run-dir", path, line)
				}
				if status.started.State == "" &&
					(record.Result.Formal022.Answer.AnswerCalls != 0 || record.Result.Formal022.Answer.JudgeCalls != 0) {
					_ = prior.Close()
					return nil, fmt.Errorf("%s line %d has provider calls without STARTED; use a fresh --run-dir", path, line)
				}
				status.terminal = &record
			}
			statuses[key] = status
		}
		if err := scanner.Err(); err != nil {
			_ = prior.Close()
			return nil, fmt.Errorf("scan formal call journal: %w", err)
		}
		if err := prior.Close(); err != nil {
			return nil, fmt.Errorf("close formal call journal after scan: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read formal call journal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // operator-selected formal artifact
	if err != nil {
		return nil, fmt.Errorf("open formal call journal: %w", err)
	}
	return &formalCallJournal{
		f: f, w: bufio.NewWriter(f), protocolHash: protocolHash, statuses: statuses,
	}, nil
}

func validateFormalCallRecord(record formalCallRecord, protocolHash string) error {
	if record.Schema != evalProtocolSchema || record.ProtocolHash != protocolHash {
		return fmt.Errorf("schema or protocol hash mismatch")
	}
	if record.Conv < 0 || record.Q < 0 || record.Repetition < 1 || strings.TrimSpace(record.QuestionID) == "" {
		return fmt.Errorf("invalid formal call identity")
	}
	if !isDigest(record.FrozenDigest) || !isDigest(record.InputDigest) {
		return fmt.Errorf("formal call requires frozen/input digests")
	}
	switch record.State {
	case formalCallStarted:
		if record.Result != nil || record.ResultDigest != "" {
			return fmt.Errorf("STARTED must not contain a result")
		}
	case formalCallCompleted, formalCallFailed:
		if record.Result == nil || !isDigest(record.ResultDigest) || record.ResultDigest != evalJSONDigest(*record.Result) {
			return fmt.Errorf("terminal call record has invalid result")
		}
		if record.Result.Conv != record.Conv || record.Result.Q != record.Q || resultID(*record.Result) != record.QuestionID ||
			record.Result.Formal022 == nil || record.Result.Formal022.Answer.RunIndex != record.Repetition ||
			formalRunFrozenDigest(*record.Result.Formal022) != record.FrozenDigest {
			return fmt.Errorf("terminal call result identity mismatch")
		}
		validCalls := len(record.Result.Formal022.InvalidReasons) == 0 &&
			record.Result.Formal022.Answer.AnswerCalls == 1 && record.Result.Formal022.Answer.JudgeCalls == 1
		if (record.State == formalCallCompleted) != validCalls {
			return fmt.Errorf("terminal call state does not match result validity")
		}
	default:
		return fmt.Errorf("unknown formal call state %q", record.State)
	}
	return nil
}

func sameFormalCallIdentity(left, right formalCallRecord) bool {
	return left.Schema == right.Schema && left.ProtocolHash == right.ProtocolHash &&
		left.Conv == right.Conv && left.Q == right.Q && left.QuestionID == right.QuestionID &&
		left.Repetition == right.Repetition && left.FrozenDigest == right.FrozenDigest &&
		left.InputDigest == right.InputDigest
}

func formalRunFrozenDigest(run evalFormalQuestionRun) string {
	return evalJSONDigest(struct {
		Candidate evalCandidateArtifact
		Trace     evalFormalTraceRecord
		Bundle    evalFormalBundleRecord
	}{Candidate: run.Candidate, Trace: run.Trace, Bundle: run.Bundle})
}

func formalFrozenPayloadDigest(frozen formalFrozenQuestion) string {
	return evalJSONDigest(struct {
		Candidate evalCandidateArtifact
		Trace     evalFormalTraceRecord
		Bundle    evalFormalBundleRecord
	}{Candidate: frozen.Candidate, Trace: frozen.Trace, Bundle: frozen.Bundle})
}

func (journal *formalCallJournal) Begin(key resultKey, questionID string, repetition int, frozenDigest, inputDigest string) error {
	record := formalCallRecord{
		Schema:       evalProtocolSchema,
		ProtocolHash: journal.protocolHash,
		Conv:         key.Conv,
		Q:            key.Q,
		QuestionID:   questionID,
		Repetition:   repetition,
		FrozenDigest: frozenDigest,
		InputDigest:  inputDigest,
		State:        formalCallStarted,
	}
	if err := validateFormalCallRecord(record, journal.protocolHash); err != nil {
		return err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	callKey := record.key()
	if status, exists := journal.statuses[callKey]; exists && (status.started.State != "" || status.terminal != nil) {
		return fmt.Errorf("formal call already recorded for conv=%d q=%d repetition=%d", key.Conv, key.Q, repetition)
	}
	if err := journal.appendLocked(record); err != nil {
		return err
	}
	journal.statuses[callKey] = formalCallStatus{started: record}
	return nil
}

// FailWithoutStart records a deterministic pre-call failure. Since no external
// call was made, it is safe to resume by replaying the terminal result.
func (journal *formalCallJournal) FailWithoutStart(key resultKey, questionID string, repetition int, frozenDigest, inputDigest string, item result) error {
	return journal.finish(key, questionID, repetition, frozenDigest, inputDigest, item, true)
}

func (journal *formalCallJournal) Finish(key resultKey, questionID string, repetition int, frozenDigest, inputDigest string, item result) error {
	return journal.finish(key, questionID, repetition, frozenDigest, inputDigest, item, false)
}

func (journal *formalCallJournal) finish(key resultKey, questionID string, repetition int, frozenDigest, inputDigest string, item result, allowWithoutStart bool) error {
	state := formalCallFailed
	if item.Formal022 != nil && len(item.Formal022.InvalidReasons) == 0 &&
		item.Formal022.Answer.AnswerCalls == 1 && item.Formal022.Answer.JudgeCalls == 1 {
		state = formalCallCompleted
	}
	record := formalCallRecord{
		Schema:       evalProtocolSchema,
		ProtocolHash: journal.protocolHash,
		Conv:         key.Conv,
		Q:            key.Q,
		QuestionID:   questionID,
		Repetition:   repetition,
		FrozenDigest: frozenDigest,
		InputDigest:  inputDigest,
		State:        state,
		ResultDigest: evalJSONDigest(item),
		Result:       &item,
	}
	if err := validateFormalCallRecord(record, journal.protocolHash); err != nil {
		return err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	callKey := record.key()
	status := journal.statuses[callKey]
	if status.terminal != nil {
		return fmt.Errorf("formal call terminal already recorded for conv=%d q=%d repetition=%d", key.Conv, key.Q, repetition)
	}
	if status.started.State == "" {
		if !allowWithoutStart || state != formalCallFailed ||
			item.Formal022.Answer.AnswerCalls != 0 || item.Formal022.Answer.JudgeCalls != 0 {
			return fmt.Errorf("formal call has no STARTED record for conv=%d q=%d repetition=%d", key.Conv, key.Q, repetition)
		}
	} else if !sameFormalCallIdentity(status.started, record) {
		return fmt.Errorf("formal call terminal identity drift")
	}
	if err := journal.appendLocked(record); err != nil {
		return err
	}
	status.terminal = &record
	journal.statuses[callKey] = status
	return nil
}

func (journal *formalCallJournal) appendLocked(record formalCallRecord) error {
	if journal.err != nil {
		return journal.err
	}
	if journal.closed {
		return fmt.Errorf("formal call journal is closed")
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := journal.w.Write(line); err != nil {
		journal.err = err
		return err
	}
	if err := journal.w.WriteByte('\n'); err != nil {
		journal.err = err
		return err
	}
	if err := journal.w.Flush(); err != nil {
		journal.err = err
		return err
	}
	if err := journal.f.Sync(); err != nil {
		journal.err = err
		return err
	}
	return nil
}

// Reconcile validates one repetition before or after execution. Terminal
// records are the crash-safe source and may recreate a missing ordinary result
// journal line. An orphan STARTED or an ordinary result without a terminal is
// a hard refusal: either could conceal an extra provider call.
func (journal *formalCallJournal) Reconcile(repetition int, results *journal) error {
	if journal == nil || results == nil {
		return fmt.Errorf("formal call reconciliation requires call and result journals")
	}
	prior, err := results.formalSnapshotStrict()
	if err != nil {
		return fmt.Errorf("validate repetition %d result journal: %w", repetition, err)
	}

	journal.mu.Lock()
	statuses := make(map[formalCallKey]formalCallStatus)
	for key, status := range journal.statuses {
		if key.Repetition == repetition {
			statuses[key] = status
		}
	}
	journal.mu.Unlock()

	for key, status := range statuses {
		ordinaryKey := resultKey{Conv: key.Conv, Q: key.Q}
		item, hasResult := prior[ordinaryKey]
		if status.terminal == nil {
			return fmt.Errorf("ambiguous STARTED formal call conv=%d q=%d repetition=%d; use a fresh --run-dir", key.Conv, key.Q, repetition)
		}
		terminal := *status.terminal
		if hasResult {
			if evalJSONDigest(item) != terminal.ResultDigest {
				return fmt.Errorf("formal call/result digest drift conv=%d q=%d repetition=%d", key.Conv, key.Q, repetition)
			}
			delete(prior, ordinaryKey)
			continue
		}
		if err := results.writeResult(*terminal.Result, true); err != nil {
			return fmt.Errorf("replay terminal formal result: %w", err)
		}
	}
	if len(prior) > 0 {
		return fmt.Errorf("repetition %d result journal contains %d results without formal call terminals", repetition, len(prior))
	}
	return nil
}

func (journal *formalCallJournal) Close() error {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return journal.err
	}
	journal.closed = true
	if err := journal.w.Flush(); journal.err == nil && err != nil {
		journal.err = err
	}
	if err := journal.f.Sync(); journal.err == nil && err != nil {
		journal.err = err
	}
	if err := journal.f.Close(); journal.err == nil && err != nil {
		journal.err = err
	}
	if journal.err != nil {
		return fmt.Errorf("write %s: %w", formalCallJournalFile, journal.err)
	}
	return nil
}
