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

// formalFreezeJournalFile is a crash-safe staging journal, not one of the six
// immutable 022 result artifacts. A question is synced here after its sole
// retrieve/pack pass and before its first answer call. That closes the window
// where a process restart could otherwise rematerialize different candidates.
const formalFreezeJournalFile = "formal_freeze.jsonl"

// formalFrozenQuestion contains exactly the question state shared by all
// answer repetitions. Answer- or judge-specific fields deliberately do not
// live here, so a failed model call cannot mutate Trace/Bundle validity or
// contaminate another repetition.
type formalFrozenQuestion struct {
	Candidate      evalCandidateArtifact  `json:"candidate"`
	Trace          evalFormalTraceRecord  `json:"trace"`
	Bundle         evalFormalBundleRecord `json:"bundle"`
	InvalidReasons []string               `json:"invalid_reasons,omitempty"`
}

type formalFreezeRecord struct {
	Schema       string               `json:"schema"`
	ProtocolHash string               `json:"protocol_hash"`
	Conv         int                  `json:"conv"`
	Q            int                  `json:"q"`
	QuestionID   string               `json:"question_id"`
	FrozenDigest string               `json:"frozen_digest"`
	Frozen       formalFrozenQuestion `json:"frozen"`
}

func (record formalFreezeRecord) key() resultKey {
	return resultKey{Conv: record.Conv, Q: record.Q}
}

type formalReplayEntry struct {
	ready      chan struct{}
	questionID string
	frozen     formalFrozenQuestion
	err        error
}

// formalQuestionReplay is a per-key singleflight cache backed by a strict
// append-only journal in formal runs. The global mutex is held only while
// publishing/looking up entries; retrieval and packing happen outside it, so
// unrelated benchmark questions remain parallel.
type formalQuestionReplay struct {
	mu      sync.Mutex
	entries map[resultKey]*formalReplayEntry
	journal *formalFreezeJournal
}

func newFormalQuestionReplay() *formalQuestionReplay {
	return &formalQuestionReplay{entries: make(map[resultKey]*formalReplayEntry)}
}

func openFormalQuestionReplay(runDir, protocolHash string) (*formalQuestionReplay, error) {
	journal, records, err := openFormalFreezeJournal(runDir, protocolHash)
	if err != nil {
		return nil, err
	}
	replay := newFormalQuestionReplay()
	replay.journal = journal
	for _, record := range records {
		entry := &formalReplayEntry{
			ready:      make(chan struct{}),
			questionID: record.QuestionID,
			frozen:     cloneFormalFrozenQuestion(record.Frozen),
		}
		close(entry.ready)
		replay.entries[record.key()] = entry
	}
	return replay, nil
}

func (replay *formalQuestionReplay) getOrMaterialize(key resultKey, questionID string, materialize func() formalFrozenQuestion) (formalFrozenQuestion, error) {
	if replay == nil {
		return formalFrozenQuestion{}, fmt.Errorf("formal question replay is unavailable")
	}
	if strings.TrimSpace(questionID) == "" {
		return formalFrozenQuestion{}, fmt.Errorf("formal question replay requires a question ID")
	}
	if materialize == nil {
		return formalFrozenQuestion{}, fmt.Errorf("formal question replay requires a materializer")
	}

	replay.mu.Lock()
	if entry, ok := replay.entries[key]; ok {
		replay.mu.Unlock()
		<-entry.ready
		if entry.questionID != questionID {
			return formalFrozenQuestion{}, fmt.Errorf("formal replay key %+v belongs to question %q, not %q", key, entry.questionID, questionID)
		}
		return cloneFormalFrozenQuestion(entry.frozen), entry.err
	}
	entry := &formalReplayEntry{ready: make(chan struct{}), questionID: questionID}
	replay.entries[key] = entry
	replay.mu.Unlock()

	frozen := materialize()
	frozen.InvalidReasons = stableStrings(frozen.InvalidReasons)
	var persistErr error
	if replay.journal != nil {
		persistErr = replay.journal.Write(formalFreezeRecord{
			Schema:       evalProtocolSchema,
			ProtocolHash: replay.journal.protocolHash,
			Conv:         key.Conv,
			Q:            key.Q,
			QuestionID:   questionID,
			FrozenDigest: evalJSONDigest(frozen),
			Frozen:       frozen,
		})
	}

	replay.mu.Lock()
	entry.frozen = cloneFormalFrozenQuestion(frozen)
	entry.err = persistErr
	close(entry.ready)
	replay.mu.Unlock()
	return cloneFormalFrozenQuestion(frozen), persistErr
}

func (replay *formalQuestionReplay) seed(key resultKey, questionID string, frozen formalFrozenQuestion) error {
	if replay == nil {
		return fmt.Errorf("formal question replay is unavailable")
	}
	frozen.InvalidReasons = stableStrings(frozen.InvalidReasons)
	replay.mu.Lock()
	if entry, ok := replay.entries[key]; ok {
		replay.mu.Unlock()
		<-entry.ready
		if entry.err != nil {
			return entry.err
		}
		if entry.questionID != questionID || evalJSONDigest(entry.frozen) != evalJSONDigest(frozen) {
			return fmt.Errorf("formal replay seed drift for conv=%d q=%d", key.Conv, key.Q)
		}
		return nil
	}
	entry := &formalReplayEntry{ready: make(chan struct{}), questionID: questionID}
	replay.entries[key] = entry
	replay.mu.Unlock()

	var persistErr error
	if replay.journal != nil {
		persistErr = replay.journal.Write(formalFreezeRecord{
			Schema:       evalProtocolSchema,
			ProtocolHash: replay.journal.protocolHash,
			Conv:         key.Conv,
			Q:            key.Q,
			QuestionID:   questionID,
			FrozenDigest: evalJSONDigest(frozen),
			Frozen:       frozen,
		})
	}
	replay.mu.Lock()
	entry.frozen = cloneFormalFrozenQuestion(frozen)
	entry.err = persistErr
	close(entry.ready)
	replay.mu.Unlock()
	return persistErr
}

func (replay *formalQuestionReplay) Close() error {
	if replay == nil || replay.journal == nil {
		return nil
	}
	return replay.journal.Close()
}

func cloneFormalFrozenQuestion(frozen formalFrozenQuestion) formalFrozenQuestion {
	frozen.Candidate.Anchors = append([]evalRankedAnchor(nil), frozen.Candidate.Anchors...)
	for index := range frozen.Candidate.Anchors {
		frozen.Candidate.Anchors[index].SourceIDs = append([]string(nil), frozen.Candidate.Anchors[index].SourceIDs...)
	}
	frozen.Candidate.RenderedCandidates = append([]evalRenderedCandidate(nil), frozen.Candidate.RenderedCandidates...)
	for index := range frozen.Candidate.RenderedCandidates {
		frozen.Candidate.RenderedCandidates[index].SourceIDs = append([]string(nil), frozen.Candidate.RenderedCandidates[index].SourceIDs...)
		frozen.Candidate.RenderedCandidates[index].ExpandedFrom = append([]string(nil), frozen.Candidate.RenderedCandidates[index].ExpandedFrom...)
	}
	frozen.Candidate.Gold.DatasetSourceIDs = append([]string(nil), frozen.Candidate.Gold.DatasetSourceIDs...)
	frozen.Candidate.Gold.ResolvedEvidenceIDs = append([]string(nil), frozen.Candidate.Gold.ResolvedEvidenceIDs...)
	frozen.Candidate.Gold.UnresolvedIDs = append([]string(nil), frozen.Candidate.Gold.UnresolvedIDs...)
	frozen.Trace.AppliedActions = append([]string(nil), frozen.Trace.AppliedActions...)
	frozen.Bundle.SourceIDs = append([]string(nil), frozen.Bundle.SourceIDs...)
	frozen.InvalidReasons = append([]string(nil), frozen.InvalidReasons...)
	return frozen
}

func frozenQuestionFromRun(run evalFormalQuestionRun) formalFrozenQuestion {
	reasons := make([]string, 0, len(run.InvalidReasons))
	for _, reason := range run.InvalidReasons {
		switch reason {
		case "answer_input_drift", "answer_preflight_or_runtime_failed", "judge_failed":
			continue
		default:
			reasons = append(reasons, reason)
		}
	}
	return formalFrozenQuestion{
		Candidate:      run.Candidate,
		Trace:          run.Trace,
		Bundle:         run.Bundle,
		InvalidReasons: stableStrings(reasons),
	}
}

// seedFormalQuestionReplay imports already-completed first-repetition results.
// This is a backwards-compatible recovery source; new runs persist the same
// freeze before the answer call and therefore do not depend on that call
// having completed.
func seedFormalQuestionReplay(replay *formalQuestionReplay, source *journal) error {
	if replay == nil || source == nil {
		return fmt.Errorf("formal replay seed requires replay and source journal")
	}
	prior, err := source.formalSnapshotStrict()
	if err != nil {
		return fmt.Errorf("validate first-run formal journal: %w", err)
	}
	for key, item := range prior {
		if item.Formal022 == nil {
			return fmt.Errorf("formal replay seed conv=%d q=%d has no 022 payload", key.Conv, key.Q)
		}
		questionID := resultID(item)
		if strings.TrimSpace(questionID) == "" {
			return fmt.Errorf("formal replay seed conv=%d q=%d has no question ID", key.Conv, key.Q)
		}
		if err := replay.seed(key, questionID, frozenQuestionFromRun(*item.Formal022)); err != nil {
			return err
		}
	}
	return nil
}

type formalFreezeJournal struct {
	mu           sync.Mutex
	f            *os.File
	w            *bufio.Writer
	protocolHash string
	byKey        map[resultKey]formalFreezeRecord
	byQuestion   map[string]resultKey
	err          error
	closed       bool
}

func openFormalFreezeJournal(runDir, protocolHash string) (*formalFreezeJournal, []formalFreezeRecord, error) {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(protocolHash) == "" {
		return nil, nil, fmt.Errorf("formal freeze journal requires run directory and protocol hash")
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create formal freeze journal directory: %w", err)
	}
	path := filepath.Join(runDir, formalFreezeJournalFile)
	var records []formalFreezeRecord
	byKey := make(map[resultKey]formalFreezeRecord)
	byQuestion := make(map[string]resultKey)
	if prior, err := os.Open(path); err == nil { //nolint:gosec // operator-selected run artifact
		scanner := bufio.NewScanner(prior)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var record formalFreezeRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = prior.Close()
				return nil, nil, fmt.Errorf("decode %s line %d: %w; use a fresh --run-dir", path, line, err)
			}
			if err := validateFormalFreezeRecord(record, protocolHash); err != nil {
				_ = prior.Close()
				return nil, nil, fmt.Errorf("validate %s line %d: %w; use a fresh --run-dir", path, line, err)
			}
			key := record.key()
			if _, duplicate := byKey[key]; duplicate {
				_ = prior.Close()
				return nil, nil, fmt.Errorf("%s line %d duplicates conv=%d q=%d; use a fresh --run-dir", path, line, key.Conv, key.Q)
			}
			if previous, duplicate := byQuestion[record.QuestionID]; duplicate {
				_ = prior.Close()
				return nil, nil, fmt.Errorf("%s line %d duplicates question %q at conv=%d q=%d; use a fresh --run-dir", path, line, record.QuestionID, previous.Conv, previous.Q)
			}
			byKey[key] = record
			byQuestion[record.QuestionID] = key
			records = append(records, record)
		}
		if err := scanner.Err(); err != nil {
			_ = prior.Close()
			return nil, nil, fmt.Errorf("scan formal freeze journal: %w", err)
		}
		if err := prior.Close(); err != nil {
			return nil, nil, fmt.Errorf("close formal freeze journal after scan: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("read formal freeze journal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return nil, nil, fmt.Errorf("open formal freeze journal: %w", err)
	}
	return &formalFreezeJournal{
		f: f, w: bufio.NewWriter(f), protocolHash: protocolHash,
		byKey: byKey, byQuestion: byQuestion,
	}, records, nil
}

func validateFormalFreezeRecord(record formalFreezeRecord, protocolHash string) error {
	if record.Schema != evalProtocolSchema || record.ProtocolHash != protocolHash {
		return fmt.Errorf("schema or protocol hash mismatch")
	}
	if record.Conv < 0 || record.Q < 0 || strings.TrimSpace(record.QuestionID) == "" {
		return fmt.Errorf("invalid question identity")
	}
	if record.FrozenDigest == "" || record.FrozenDigest != evalJSONDigest(record.Frozen) {
		return fmt.Errorf("frozen digest mismatch")
	}
	for label, identity := range map[string]struct {
		questionID   string
		protocolHash string
	}{
		"candidate": {record.Frozen.Candidate.QuestionID, record.Frozen.Candidate.ProtocolHash},
		"trace":     {record.Frozen.Trace.QuestionID, record.Frozen.Trace.ProtocolHash},
		"bundle":    {record.Frozen.Bundle.QuestionID, record.Frozen.Bundle.ProtocolHash},
	} {
		if identity.questionID != "" && identity.questionID != record.QuestionID {
			return fmt.Errorf("%s question ID mismatch", label)
		}
		if identity.protocolHash != "" && identity.protocolHash != protocolHash {
			return fmt.Errorf("%s protocol hash mismatch", label)
		}
	}
	return nil
}

func (journal *formalFreezeJournal) Write(record formalFreezeRecord) error {
	if journal == nil {
		return fmt.Errorf("formal freeze journal is unavailable")
	}
	if err := validateFormalFreezeRecord(record, journal.protocolHash); err != nil {
		return err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode formal freeze record: %w", err)
	}

	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.err != nil {
		return journal.err
	}
	if journal.closed {
		return fmt.Errorf("formal freeze journal is closed")
	}
	key := record.key()
	if prior, ok := journal.byKey[key]; ok {
		if prior.QuestionID == record.QuestionID && prior.FrozenDigest == record.FrozenDigest {
			return nil
		}
		return fmt.Errorf("formal freeze drift for conv=%d q=%d", key.Conv, key.Q)
	}
	if prior, ok := journal.byQuestion[record.QuestionID]; ok {
		return fmt.Errorf("formal freeze question %q already belongs to conv=%d q=%d", record.QuestionID, prior.Conv, prior.Q)
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
	journal.byKey[key] = record
	journal.byQuestion[record.QuestionID] = key
	return nil
}

func (journal *formalFreezeJournal) Close() error {
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
		return fmt.Errorf("write %s: %w", formalFreezeJournalFile, journal.err)
	}
	return nil
}
