package evidencecompiler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidCandidate    = errors.New("evidencecompiler: invalid candidate")
	ErrSourceUnavailable   = errors.New("evidencecompiler: source unavailable")
	ErrInvalidAction       = errors.New("evidencecompiler: invalid action")
	ErrInvalidSpan         = errors.New("evidencecompiler: invalid source span")
	ErrInvalidNeed         = errors.New("evidencecompiler: invalid evidence need")
	ErrInvalidBundle       = errors.New("evidencecompiler: invalid evidence bundle")
	ErrCounterUnavailable  = errors.New("evidencecompiler: token counter unavailable")
	ErrFingerprintMismatch = errors.New("evidencecompiler: counter fingerprint mismatch")
	ErrBudgetImpossible    = errors.New("evidencecompiler: evidence cannot fit token cap")
)

// validatedCandidates preserves the caller's frozen candidate order while
// carrying the only source IDs the Compiler may ask its resolver to read.
// Neither validation nor compilation mutates a Candidate.
type validatedCandidates struct {
	ordered   []Candidate
	byID      map[string]Candidate
	allowlist map[string]bool
	sourceIDs []string
	digest    string
}

func validateCandidates(candidates []Candidate, maxCandidates int) (validatedCandidates, error) {
	if maxCandidates <= 0 {
		return validatedCandidates{}, fmt.Errorf("%w: max candidates must be positive", ErrInvalidCandidate)
	}
	if len(candidates) > maxCandidates {
		return validatedCandidates{}, fmt.Errorf("%w: %d candidates exceeds limit %d", ErrInvalidCandidate, len(candidates), maxCandidates)
	}

	validated := validatedCandidates{
		ordered:   append([]Candidate(nil), candidates...),
		byID:      make(map[string]Candidate, len(candidates)),
		allowlist: make(map[string]bool),
	}
	lastRank := 0
	for index, candidate := range candidates {
		if !validCandidateKind(candidate.Kind) {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %d has unknown kind %q", ErrInvalidCandidate, index, candidate.Kind)
		}
		if candidate.ID == "" {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %d has no ID", ErrInvalidCandidate, index)
		}
		if _, exists := validated.byID[candidate.ID]; exists {
			return validatedCandidates{}, fmt.Errorf("%w: duplicate candidate ID %q", ErrInvalidCandidate, candidate.ID)
		}
		if candidate.Rank <= 0 || candidate.Rank <= lastRank {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %q rank %d is not strictly increasing", ErrInvalidCandidate, candidate.ID, candidate.Rank)
		}
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %q has non-finite score", ErrInvalidCandidate, candidate.ID)
		}
		if candidate.Text == "" || !utf8.ValidString(candidate.Text) {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %q has empty or invalid UTF-8 text", ErrInvalidCandidate, candidate.ID)
		}
		if !sameDigest(candidate.TextDigest, candidate.Text) {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %q text digest mismatch", ErrInvalidCandidate, candidate.ID)
		}
		if len(candidate.SourceIDs) == 0 || !sort.StringsAreSorted(candidate.SourceIDs) {
			return validatedCandidates{}, fmt.Errorf("%w: candidate %q source IDs are empty or not canonically sorted", ErrInvalidCandidate, candidate.ID)
		}
		for sourceIndex, sourceID := range candidate.SourceIDs {
			if sourceID == "" {
				return validatedCandidates{}, fmt.Errorf("%w: candidate %q has empty source ID", ErrInvalidCandidate, candidate.ID)
			}
			if sourceIndex > 0 && sourceID == candidate.SourceIDs[sourceIndex-1] {
				return validatedCandidates{}, fmt.Errorf("%w: candidate %q repeats source ID %q", ErrInvalidCandidate, candidate.ID, sourceID)
			}
			validated.allowlist[sourceID] = true
		}
		validated.byID[candidate.ID] = candidate
		lastRank = candidate.Rank
	}
	validated.sourceIDs = sortedSourceIDs(validated.allowlist)
	var err error
	validated.digest, err = canonicalDigest(candidates)
	if err != nil {
		return validatedCandidates{}, fmt.Errorf("%w: canonical candidate digest: %v", ErrInvalidCandidate, err)
	}
	return validated, nil
}

func validCandidateKind(kind CandidateKind) bool {
	switch kind {
	case CandidateChunk, CandidateRawTurn, CandidateSemanticEpisode, CandidateAtomicFact:
		return true
	default:
		return false
	}
}

func validActionKind(kind ActionKind) bool {
	switch kind {
	case ActionKeep, ActionExtract, ActionDrop, ActionMerge, ActionFetchSource:
		return true
	default:
		return false
	}
}

func validateAction(action Action, candidates validatedCandidates, sources map[string]Source) error {
	if !validActionKind(action.Kind) {
		return fmt.Errorf("%w: unknown action %q", ErrInvalidAction, action.Kind)
	}

	switch action.Kind {
	case ActionKeep:
		if (action.CandidateID == "") == (action.SourceID == "") || action.Span != nil || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: KEEP requires exactly one candidate or source ID", ErrInvalidAction)
		}
		if action.CandidateID != "" {
			candidate, ok := candidates.byID[action.CandidateID]
			if !ok {
				return fmt.Errorf("%w: KEEP candidate %q is not frozen", ErrInvalidAction, action.CandidateID)
			}
			if len(candidate.SourceIDs) != 1 {
				return fmt.Errorf("%w: KEEP candidate %q is not one verified source", ErrInvalidAction, candidate.ID)
			}
			source, err := validSource(candidate.SourceIDs[0], candidates.allowlist, sources)
			if err != nil {
				return err
			}
			if candidate.Text != source.Content || !sameDigest(candidate.TextDigest, source.Content) {
				return fmt.Errorf("%w: KEEP candidate %q is not canonical source text", ErrInvalidAction, candidate.ID)
			}
			return nil
		}
		_, err := validSource(action.SourceID, candidates.allowlist, sources)
		return err

	case ActionExtract:
		if action.Span == nil || action.SourceID != "" || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: EXTRACT requires only a source span", ErrInvalidAction)
		}
		if action.CandidateID != "" {
			candidate, ok := candidates.byID[action.CandidateID]
			if !ok || !containsSourceID(candidate.SourceIDs, action.Span.SourceID) {
				return fmt.Errorf("%w: EXTRACT span %q is outside candidate lineage", ErrInvalidAction, action.Span.SourceID)
			}
		}
		_, err := validateSourceSpan(*action.Span, candidates.allowlist, sources)
		return err

	case ActionDrop:
		if action.CandidateID == "" || action.SourceID != "" || action.Span != nil || len(action.Sentences) != 0 || strings.TrimSpace(action.ReasonCode) == "" {
			return fmt.Errorf("%w: DROP requires a candidate ID and reason only", ErrInvalidAction)
		}
		if _, ok := candidates.byID[action.CandidateID]; !ok {
			return fmt.Errorf("%w: DROP candidate %q is not frozen", ErrInvalidAction, action.CandidateID)
		}
		return nil

	case ActionMerge:
		if action.CandidateID != "" || action.SourceID != "" || action.Span != nil || action.ReasonCode != "" || len(action.Sentences) == 0 {
			return fmt.Errorf("%w: MERGE requires grounded sentences only", ErrInvalidAction)
		}
		for index, sentence := range action.Sentences {
			if strings.TrimSpace(sentence.Text) == "" || !utf8.ValidString(sentence.Text) || len(sentence.Sources) == 0 {
				return fmt.Errorf("%w: MERGE sentence %d is empty or ungrounded", ErrInvalidAction, index)
			}
			for _, span := range sentence.Sources {
				if _, err := validateSourceSpan(span, candidates.allowlist, sources); err != nil {
					return fmt.Errorf("%w: MERGE sentence %d: %v", ErrInvalidAction, index, err)
				}
			}
		}
		return nil

	case ActionFetchSource:
		if action.CandidateID != "" || action.SourceID == "" || action.Span != nil || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: FETCH_SOURCE requires a source ID only", ErrInvalidAction)
		}
		_, err := validSource(action.SourceID, candidates.allowlist, sources)
		return err
	}
	return fmt.Errorf("%w: unknown action %q", ErrInvalidAction, action.Kind)
}

func validateSourceSpan(span SourceSpan, allowlist map[string]bool, sources map[string]Source) (string, error) {
	source, err := validSource(span.SourceID, allowlist, sources)
	if err != nil {
		return "", err
	}
	codePoints := []rune(source.Content)
	if span.StartChar < 0 || span.EndChar <= span.StartChar || span.EndChar > len(codePoints) {
		return "", fmt.Errorf("%w: %q span [%d,%d) is outside %d code points", ErrInvalidSpan, span.SourceID, span.StartChar, span.EndChar, len(codePoints))
	}
	text := string(codePoints[span.StartChar:span.EndChar])
	if !sameDigest(span.SpanDigest, text) {
		return "", fmt.Errorf("%w: %q span digest mismatch", ErrInvalidSpan, span.SourceID)
	}
	return text, nil
}

func validSource(sourceID string, allowlist map[string]bool, sources map[string]Source) (Source, error) {
	if sourceID == "" || !allowlist[sourceID] {
		return Source{}, fmt.Errorf("%w: source %q is not in frozen candidate lineage", ErrSourceUnavailable, sourceID)
	}
	source, ok := sources[sourceID]
	if !ok {
		return Source{}, fmt.Errorf("%w: source %q was not resolved", ErrSourceUnavailable, sourceID)
	}
	if source.ID != sourceID || source.Content == "" || !utf8.ValidString(source.Content) || !sameDigest(source.ContentDigest, source.Content) {
		return Source{}, fmt.Errorf("%w: source %q is not active canonical evidence", ErrSourceUnavailable, sourceID)
	}
	return source, nil
}

func containsSourceID(sourceIDs []string, sourceID string) bool {
	index := sort.SearchStrings(sourceIDs, sourceID)
	return index < len(sourceIDs) && sourceIDs[index] == sourceID
}

func sortedSourceIDs(allowlist map[string]bool) []string {
	ids := make([]string, 0, len(allowlist))
	for sourceID := range allowlist {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	return ids
}

func sameDigest(digest, content string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	computed := sha256.Sum256([]byte(content))
	return strings.EqualFold(digest, fmt.Sprintf("%x", computed[:]))
}

func canonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:]), nil
}
