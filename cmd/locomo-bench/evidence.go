package main

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/wallfacers/engram/memory"
)

var (
	evidenceReferencePattern = regexp.MustCompile(`^D(\d+):(\d+)$`)
	sourceSessionPattern     = regexp.MustCompile(`(?:^|-)sess(\d+)(?:-|$)`)
)

type goldEvidenceReference struct {
	Session int `json:"session"`
	Dialog  int `json:"dialog"`
}

type sweepEvidenceDiagnostics struct {
	GoldEvidence              []string                `json:"gold_evidence"`
	GoldEvidenceReferences    []goldEvidenceReference `json:"gold_evidence_references"`
	GoldEvidenceSessions      []int                   `json:"gold_evidence_sessions"`
	RetrievedMemoryNames      []string                `json:"retrieved_memory_names"`
	RetrievedSourceSessionIDs []string                `json:"retrieved_source_session_ids"`
	RetrievedTurnIDs          []string                `json:"retrieved_turn_ids,omitempty"`
	SweepCandidatesBefore     int                     `json:"sweep_candidates_before"`
	SweepCandidatesAfter      int                     `json:"sweep_candidates_after"`
	EvidenceSessionRecall     float64                 `json:"evidence_session_recall"`
	EvidenceTurnRecall        float64                 `json:"evidence_turn_recall"`
	AnswerContextTokens       int                     `json:"answer_context_tokens"`
}

// newSweepEvidenceDiagnostics records both the (inflated) session-level recall
// and the honest exact-turn recall. chunkTurns maps a retrieved chunk entry name
// to the dialogue ids (D<session>:<turn>) its verbatim text covers; a nil map
// (e.g. a facts-only run with no chunk provenance) leaves turn recall at zero.
func newSweepEvidenceDiagnostics(qa locomoQA, hits []memory.Result, search memory.SearchDiagnostics, answerContextTokens int, chunkTurns map[string][]string) *sweepEvidenceDiagnostics {
	if !search.SweepUsed {
		return nil
	}
	diagnostic := &sweepEvidenceDiagnostics{
		GoldEvidence:           append([]string(nil), qa.Evidence...),
		GoldEvidenceReferences: evidenceReferences(qa.Evidence),
		GoldEvidenceSessions:   evidenceSessions(qa.Evidence),
		SweepCandidatesBefore:  search.SweepCandidatesBefore,
		SweepCandidatesAfter:   search.SweepCandidatesAfter,
		AnswerContextTokens:    answerContextTokens,
	}
	retrievedSessionNumbers := make(map[int]struct{})
	retrievedTurnIDs := make(map[string]struct{})
	seenSourceSessions := make(map[string]struct{}, len(hits))
	seenTurnIDs := make(map[string]struct{})
	for _, hit := range hits {
		diagnostic.RetrievedMemoryNames = append(diagnostic.RetrievedMemoryNames, hit.Name)
		for _, diaID := range chunkTurns[hit.Name] {
			retrievedTurnIDs[diaID] = struct{}{}
			if _, seen := seenTurnIDs[diaID]; !seen {
				diagnostic.RetrievedTurnIDs = append(diagnostic.RetrievedTurnIDs, diaID)
				seenTurnIDs[diaID] = struct{}{}
			}
		}
		if hit.SourceSessionID == "" {
			continue
		}
		if _, seen := seenSourceSessions[hit.SourceSessionID]; !seen {
			diagnostic.RetrievedSourceSessionIDs = append(diagnostic.RetrievedSourceSessionIDs, hit.SourceSessionID)
			seenSourceSessions[hit.SourceSessionID] = struct{}{}
		}
		if session, ok := sourceSessionNumber(hit.SourceSessionID); ok {
			retrievedSessionNumbers[session] = struct{}{}
		}
	}
	if len(diagnostic.GoldEvidenceSessions) == 0 {
		return diagnostic
	}
	matched := 0
	for _, session := range diagnostic.GoldEvidenceSessions {
		if _, ok := retrievedSessionNumbers[session]; ok {
			matched++
		}
	}
	diagnostic.EvidenceSessionRecall = float64(matched) / float64(len(diagnostic.GoldEvidenceSessions))
	diagnostic.EvidenceTurnRecall = exactTurnRecall(diagnostic.GoldEvidenceReferences, retrievedTurnIDs)
	return diagnostic
}

// exactTurnRecall is the fraction of gold evidence turns (D<session>:<dialog>)
// whose exact dialogue id was covered by a retrieved chunk. Unlike session
// recall, a hit from the right session but the wrong turn does not count.
func exactTurnRecall(gold []goldEvidenceReference, retrievedTurnIDs map[string]struct{}) float64 {
	if len(gold) == 0 {
		return 0
	}
	matched := 0
	for _, ref := range gold {
		if _, ok := retrievedTurnIDs[fmt.Sprintf("D%d:%d", ref.Session, ref.Dialog)]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(gold))
}

func evidenceSessions(evidence []string) []int {
	seen := make(map[int]struct{}, len(evidence))
	sessions := make([]int, 0, len(evidence))
	for _, reference := range evidenceReferences(evidence) {
		session := reference.Session
		if _, duplicate := seen[session]; duplicate {
			continue
		}
		seen[session] = struct{}{}
		sessions = append(sessions, session)
	}
	return sessions
}

func evidenceReferences(evidence []string) []goldEvidenceReference {
	references := make([]goldEvidenceReference, 0, len(evidence))
	for _, item := range evidence {
		match := evidenceReferencePattern.FindStringSubmatch(item)
		if len(match) != 3 {
			continue
		}
		session, sessionErr := strconv.Atoi(match[1])
		dialog, dialogErr := strconv.Atoi(match[2])
		if sessionErr != nil || dialogErr != nil {
			continue
		}
		references = append(references, goldEvidenceReference{Session: session, Dialog: dialog})
	}
	return references
}

func sourceSessionNumber(sourceSessionID string) (int, bool) {
	match := sourceSessionPattern.FindStringSubmatch(sourceSessionID)
	if len(match) != 2 {
		return 0, false
	}
	session, err := strconv.Atoi(match[1])
	return session, err == nil
}
