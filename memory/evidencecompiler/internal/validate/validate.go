// Package validate implements the canonical, fail-closed validation of
// frozen candidates, planner actions, and source spans. It is pure: no IO,
// no tokenizer, no resolver. It consumes only contract types.
package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
)

// ValidatedCandidates preserves the caller's frozen candidate order while
// carrying the only source IDs the Compiler may ask its resolver to read.
// Neither validation nor compilation mutates a Candidate.
type ValidatedCandidates struct {
	Ordered   []contracts.Candidate
	ByID      map[string]contracts.Candidate
	Allowlist map[string]bool
	SourceIDs []string
	Digest    string
}

// ValidateCandidates rejects unknown kinds, empty/duplicate IDs, non-strict
// ranks, non-finite scores, invalid UTF-8, digest mismatches, and non-canonical
// source-ID lists before any resolver access.
func ValidateCandidates(candidates []contracts.Candidate, maxCandidates int) (ValidatedCandidates, error) {
	if maxCandidates <= 0 {
		return ValidatedCandidates{}, fmt.Errorf("%w: max candidates must be positive", contracts.ErrInvalidCandidate)
	}
	if len(candidates) > maxCandidates {
		return ValidatedCandidates{}, fmt.Errorf("%w: %d candidates exceeds limit %d", contracts.ErrInvalidCandidate, len(candidates), maxCandidates)
	}

	validated := ValidatedCandidates{
		Ordered:   append([]contracts.Candidate(nil), candidates...),
		ByID:      make(map[string]contracts.Candidate, len(candidates)),
		Allowlist: make(map[string]bool),
	}
	lastRank := 0
	for index, candidate := range candidates {
		if !validCandidateKind(candidate.Kind) {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %d has unknown kind %q", contracts.ErrInvalidCandidate, index, candidate.Kind)
		}
		if candidate.ID == "" {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %d has no ID", contracts.ErrInvalidCandidate, index)
		}
		if _, exists := validated.ByID[candidate.ID]; exists {
			return ValidatedCandidates{}, fmt.Errorf("%w: duplicate candidate ID %q", contracts.ErrInvalidCandidate, candidate.ID)
		}
		if candidate.Rank <= 0 || candidate.Rank <= lastRank {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q rank %d is not strictly increasing", contracts.ErrInvalidCandidate, candidate.ID, candidate.Rank)
		}
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q has non-finite score", contracts.ErrInvalidCandidate, candidate.ID)
		}
		if candidate.Text == "" || !utf8.ValidString(candidate.Text) {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q has empty or invalid UTF-8 text", contracts.ErrInvalidCandidate, candidate.ID)
		}
		if !SameDigest(candidate.TextDigest, candidate.Text) {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q text digest mismatch", contracts.ErrInvalidCandidate, candidate.ID)
		}
		if len(candidate.SourceIDs) == 0 || !sort.StringsAreSorted(candidate.SourceIDs) {
			return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q source IDs are empty or not canonically sorted", contracts.ErrInvalidCandidate, candidate.ID)
		}
		for sourceIndex, sourceID := range candidate.SourceIDs {
			if sourceID == "" {
				return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q has empty source ID", contracts.ErrInvalidCandidate, candidate.ID)
			}
			if sourceIndex > 0 && sourceID == candidate.SourceIDs[sourceIndex-1] {
				return ValidatedCandidates{}, fmt.Errorf("%w: candidate %q repeats source ID %q", contracts.ErrInvalidCandidate, candidate.ID, sourceID)
			}
			validated.Allowlist[sourceID] = true
		}
		validated.ByID[candidate.ID] = candidate
		lastRank = candidate.Rank
	}
	validated.SourceIDs = SortedSourceIDs(validated.Allowlist)
	var err error
	validated.Digest, err = CanonicalDigest(candidates)
	if err != nil {
		return ValidatedCandidates{}, fmt.Errorf("%w: canonical candidate digest: %v", contracts.ErrInvalidCandidate, err)
	}
	return validated, nil
}

func validCandidateKind(kind contracts.CandidateKind) bool {
	switch kind {
	case contracts.CandidateChunk, contracts.CandidateRawTurn, contracts.CandidateSemanticEpisode, contracts.CandidateAtomicFact:
		return true
	default:
		return false
	}
}

func validActionKind(kind contracts.ActionKind) bool {
	switch kind {
	case contracts.ActionKeep, contracts.ActionExtract, contracts.ActionDrop, contracts.ActionMerge, contracts.ActionFetchSource:
		return true
	default:
		return false
	}
}

// ValidateAction rejects unknown actions and every ungrounded or ambiguous
// action shape before a Planner proposal can reach admission.
func ValidateAction(action contracts.Action, candidates ValidatedCandidates, sources map[string]contracts.Source) error {
	if !validActionKind(action.Kind) {
		return fmt.Errorf("%w: unknown action %q", contracts.ErrInvalidAction, action.Kind)
	}

	switch action.Kind {
	case contracts.ActionKeep:
		if (action.CandidateID == "") == (action.SourceID == "") || action.Span != nil || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: KEEP requires exactly one candidate or source ID", contracts.ErrInvalidAction)
		}
		if action.CandidateID != "" {
			candidate, ok := candidates.ByID[action.CandidateID]
			if !ok {
				return fmt.Errorf("%w: KEEP candidate %q is not frozen", contracts.ErrInvalidAction, action.CandidateID)
			}
			if len(candidate.SourceIDs) != 1 {
				return fmt.Errorf("%w: KEEP candidate %q is not one verified source", contracts.ErrInvalidAction, candidate.ID)
			}
			source, err := ValidSource(candidate.SourceIDs[0], candidates.Allowlist, sources)
			if err != nil {
				return err
			}
			if candidate.Text != source.Content || !SameDigest(candidate.TextDigest, source.Content) {
				return fmt.Errorf("%w: KEEP candidate %q is not canonical source text", contracts.ErrInvalidAction, candidate.ID)
			}
			return nil
		}
		_, err := ValidSource(action.SourceID, candidates.Allowlist, sources)
		return err

	case contracts.ActionExtract:
		if action.Span == nil || action.SourceID != "" || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: EXTRACT requires only a source span", contracts.ErrInvalidAction)
		}
		if action.CandidateID != "" {
			candidate, ok := candidates.ByID[action.CandidateID]
			if !ok || !containsSourceID(candidate.SourceIDs, action.Span.SourceID) {
				return fmt.Errorf("%w: EXTRACT span %q is outside candidate lineage", contracts.ErrInvalidAction, action.Span.SourceID)
			}
		}
		_, err := ValidateSourceSpan(*action.Span, candidates.Allowlist, sources)
		return err

	case contracts.ActionDrop:
		if action.CandidateID == "" || action.SourceID != "" || action.Span != nil || len(action.Sentences) != 0 || strings.TrimSpace(action.ReasonCode) == "" {
			return fmt.Errorf("%w: DROP requires a candidate ID and reason only", contracts.ErrInvalidAction)
		}
		if _, ok := candidates.ByID[action.CandidateID]; !ok {
			return fmt.Errorf("%w: DROP candidate %q is not frozen", contracts.ErrInvalidAction, action.CandidateID)
		}
		return nil

	case contracts.ActionMerge:
		if action.CandidateID != "" || action.SourceID != "" || action.Span != nil || action.ReasonCode != "" || len(action.Sentences) == 0 {
			return fmt.Errorf("%w: MERGE requires grounded sentences only", contracts.ErrInvalidAction)
		}
		for index, sentence := range action.Sentences {
			if strings.TrimSpace(sentence.Text) == "" || !utf8.ValidString(sentence.Text) || len(sentence.Sources) == 0 {
				return fmt.Errorf("%w: MERGE sentence %d is empty or ungrounded", contracts.ErrInvalidAction, index)
			}
			for _, span := range sentence.Sources {
				if _, err := ValidateSourceSpan(span, candidates.Allowlist, sources); err != nil {
					return fmt.Errorf("%w: MERGE sentence %d: %v", contracts.ErrInvalidAction, index, err)
				}
			}
		}
		return nil

	case contracts.ActionFetchSource:
		if action.CandidateID != "" || action.SourceID == "" || action.Span != nil || len(action.Sentences) != 0 || action.ReasonCode != "" {
			return fmt.Errorf("%w: FETCH_SOURCE requires a source ID only", contracts.ErrInvalidAction)
		}
		_, err := ValidSource(action.SourceID, candidates.Allowlist, sources)
		return err
	}
	return fmt.Errorf("%w: unknown action %q", contracts.ErrInvalidAction, action.Kind)
}

// ValidateSourceSpan reconstructs the exact code-point text of a span and
// verifies its digest against the resolved source.
func ValidateSourceSpan(span contracts.SourceSpan, allowlist map[string]bool, sources map[string]contracts.Source) (string, error) {
	source, err := ValidSource(span.SourceID, allowlist, sources)
	if err != nil {
		return "", err
	}
	codePoints := []rune(source.Content)
	if span.StartChar < 0 || span.EndChar <= span.StartChar || span.EndChar > len(codePoints) {
		return "", fmt.Errorf("%w: %q span [%d,%d) is outside %d code points", contracts.ErrInvalidSpan, span.SourceID, span.StartChar, span.EndChar, len(codePoints))
	}
	text := string(codePoints[span.StartChar:span.EndChar])
	if !SameDigest(span.SpanDigest, text) {
		return "", fmt.Errorf("%w: %q span digest mismatch", contracts.ErrInvalidSpan, span.SourceID)
	}
	return text, nil
}

// ValidSource requires the source to be inside the frozen allowlist, resolved,
// and byte-identical to its digest (active canonical evidence).
func ValidSource(sourceID string, allowlist map[string]bool, sources map[string]contracts.Source) (contracts.Source, error) {
	if sourceID == "" || !allowlist[sourceID] {
		return contracts.Source{}, fmt.Errorf("%w: source %q is not in frozen candidate lineage", contracts.ErrSourceUnavailable, sourceID)
	}
	source, ok := sources[sourceID]
	if !ok {
		return contracts.Source{}, fmt.Errorf("%w: source %q was not resolved", contracts.ErrSourceUnavailable, sourceID)
	}
	if source.ID != sourceID || source.Content == "" || !utf8.ValidString(source.Content) || !SameDigest(source.ContentDigest, source.Content) {
		return contracts.Source{}, fmt.Errorf("%w: source %q is not active canonical evidence", contracts.ErrSourceUnavailable, sourceID)
	}
	return source, nil
}

func containsSourceID(sourceIDs []string, sourceID string) bool {
	index := sort.SearchStrings(sourceIDs, sourceID)
	return index < len(sourceIDs) && sourceIDs[index] == sourceID
}

// SortedSourceIDs returns the canonical sorted form of an allowlist.
func SortedSourceIDs(allowlist map[string]bool) []string {
	ids := make([]string, 0, len(allowlist))
	for sourceID := range allowlist {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	return ids
}

// SameDigest reports whether the hex digest matches the content's SHA-256.
func SameDigest(digest, content string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	computed := sha256.Sum256([]byte(content))
	return strings.EqualFold(digest, fmt.Sprintf("%x", computed[:]))
}

// CanonicalDigest is the deterministic JSON-marshal digest used for candidate
// and trace identity.
func CanonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:]), nil
}
