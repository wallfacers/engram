package main

import (
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
	SweepCandidatesBefore     int                     `json:"sweep_candidates_before"`
	SweepCandidatesAfter      int                     `json:"sweep_candidates_after"`
	EvidenceSessionRecall     float64                 `json:"evidence_session_recall"`
	AnswerContextTokens       int                     `json:"answer_context_tokens"`
}

func newSweepEvidenceDiagnostics(qa locomoQA, hits []memory.Result, search memory.SearchDiagnostics, answerContextTokens int) *sweepEvidenceDiagnostics {
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
	seenSourceSessions := make(map[string]struct{}, len(hits))
	for _, hit := range hits {
		diagnostic.RetrievedMemoryNames = append(diagnostic.RetrievedMemoryNames, hit.Name)
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
	return diagnostic
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
