// Package extract builds the ordered raw and extractive admission plans for a
// frozen candidate pool. It never fetches or searches; it turns already
// resolved candidate lineage into alternatives. Token-cap admission is
// deliberately left to the orchestration layer, which owns the real tokenizer.
package extract

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/need"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

// EvidenceItem is an internal, fully reconstructable bundle candidate. Text is
// always either a complete active source or the exact bytes named by Sources.
type EvidenceItem struct {
	Kind         contracts.ActionKind
	Text         string
	Sources      []contracts.SourceSpan
	CandidateIDs []string
	Rank         int
	coverage     int
	lexical      int
}

// ExtractionPlan carries the ordered raw and extractive alternatives for one
// question. raw is selected when the real tokenizer proves it fits; extracts
// are the over-cap fallback.
type ExtractionPlan struct {
	Raw      []EvidenceItem
	Extracts []EvidenceItem
}

type sourceCandidate struct {
	source       contracts.Source
	candidateIDs []string
	rank         int
	coverage     int
	lexical      int
}

// BuildExtractionPlan turns resolved candidate lineage into ordered raw and
// extractive alternatives; admission is deliberately left to the orchestration
// layer because it must use the real tokenizer.
func BuildExtractionPlan(needNeed contracts.EvidenceNeed, candidates validate.ValidatedCandidates, sources map[string]contracts.Source) (ExtractionPlan, error) {
	canonical, err := need.CanonicalizeNeed(needNeed)
	if err != nil {
		return ExtractionPlan{}, err
	}
	for _, sourceID := range candidates.SourceIDs {
		if _, err := validate.ValidSource(sourceID, candidates.Allowlist, sources); err != nil {
			return ExtractionPlan{}, err
		}
	}

	bySource := make(map[string]*sourceCandidate, len(candidates.SourceIDs))
	for _, candidate := range candidates.Ordered {
		for _, sourceID := range candidate.SourceIDs {
			source := sources[sourceID]
			current, ok := bySource[sourceID]
			if !ok {
				current = &sourceCandidate{source: source, rank: candidate.Rank}
				bySource[sourceID] = current
			}
			if candidate.Rank < current.rank {
				current.rank = candidate.Rank
			}
			current.candidateIDs = append(current.candidateIDs, candidate.ID)
		}
	}

	ordered := make([]sourceCandidate, 0, len(bySource))
	for _, current := range bySource {
		current.candidateIDs = need.CanonicalStrings(current.candidateIDs)
		current.coverage = sourceNeedCoverage(canonical, current.source)
		current.lexical = sourceLexicalOverlap(canonical, current.source)
		ordered = append(ordered, *current)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].coverage != ordered[right].coverage {
			return ordered[left].coverage > ordered[right].coverage
		}
		if ordered[left].lexical != ordered[right].lexical {
			return ordered[left].lexical > ordered[right].lexical
		}
		if ordered[left].rank != ordered[right].rank {
			return ordered[left].rank < ordered[right].rank
		}
		return ordered[left].source.ID < ordered[right].source.ID
	})

	plan := ExtractionPlan{}
	for _, current := range ordered {
		full := fullSourceSpan(current.source)
		plan.Raw = append(plan.Raw, EvidenceItem{
			Kind:         contracts.ActionKeep,
			Text:         current.source.Content,
			Sources:      []contracts.SourceSpan{full},
			CandidateIDs: append([]string(nil), current.candidateIDs...),
			Rank:         current.rank,
			coverage:     current.coverage,
			lexical:      current.lexical,
		})
		for _, span := range selectExtractiveSpans(canonical, current.source) {
			text, err := validate.ValidateSourceSpan(span, candidates.Allowlist, sources)
			if err != nil {
				return ExtractionPlan{}, err
			}
			plan.Extracts = append(plan.Extracts, EvidenceItem{
				Kind:         contracts.ActionExtract,
				Text:         text,
				Sources:      []contracts.SourceSpan{span},
				CandidateIDs: append([]string(nil), current.candidateIDs...),
				Rank:         current.rank,
				coverage:     sentenceNeedCoverage(canonical, text),
				lexical:      lexicalOverlapForText(canonical, text),
			})
		}
	}
	sortEvidenceItems(plan.Extracts)
	return plan, nil
}

// SelectPackingItems returns the raw alternative when it fits, else the
// extractive fallback.
func SelectPackingItems(plan ExtractionPlan, rawFits bool) []EvidenceItem {
	if rawFits {
		return CloneEvidenceItems(plan.Raw)
	}
	return CloneEvidenceItems(plan.Extracts)
}

// CloneEvidenceItems deep-copies the source spans and candidate IDs of items.
func CloneEvidenceItems(items []EvidenceItem) []EvidenceItem {
	clone := make([]EvidenceItem, len(items))
	for index, item := range items {
		clone[index] = item
		clone[index].Sources = append([]contracts.SourceSpan(nil), item.Sources...)
		clone[index].CandidateIDs = append([]string(nil), item.CandidateIDs...)
	}
	return clone
}

func sortEvidenceItems(items []EvidenceItem) {
	sort.Slice(items, func(left, right int) bool {
		if items[left].coverage != items[right].coverage {
			return items[left].coverage > items[right].coverage
		}
		if items[left].lexical != items[right].lexical {
			return items[left].lexical > items[right].lexical
		}
		if items[left].Rank != items[right].Rank {
			return items[left].Rank < items[right].Rank
		}
		return EvidenceItemID(items[left]) < EvidenceItemID(items[right])
	})
}

// EvidenceItemID is the stable span identity used in token steps and drops.
func EvidenceItemID(item EvidenceItem) string {
	if len(item.Sources) == 0 {
		return ""
	}
	span := item.Sources[0]
	return fmt.Sprintf("%s:%d:%d", span.SourceID, span.StartChar, span.EndChar)
}

func fullSourceSpan(source contracts.Source) contracts.SourceSpan {
	return contracts.SourceSpan{
		SourceID:   source.ID,
		StartChar:  0,
		EndChar:    len([]rune(source.Content)),
		SpanDigest: sourceTextDigest(source.Content),
	}
}

func sourceTextDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

func sourceNeedCoverage(needNeed contracts.EvidenceNeed, source contracts.Source) int {
	coverage := sentenceNeedCoverage(needNeed, source.Content)
	if len(needNeed.TimeConstraints) > 0 && (source.OccurredAt != nil || HasTimeEvidence(source.Content)) {
		coverage++
	}
	return coverage
}

func sentenceNeedCoverage(needNeed contracts.EvidenceNeed, text string) int {
	coverage := 0
	lower := strings.ToLower(text)
	for _, entity := range needNeed.Entities {
		if strings.Contains(lower, strings.ToLower(entity)) {
			coverage++
		}
	}
	for _, operand := range needNeed.Operands {
		if need.SourceSupportsOperand(text, operand.Name) {
			coverage++
		}
	}
	if needNeed.UpdateState != "" && stateEvidenceMatches(needNeed.UpdateState, text) {
		coverage++
	}
	return coverage
}

func sourceLexicalOverlap(needNeed contracts.EvidenceNeed, source contracts.Source) int {
	return lexicalOverlapForText(needNeed, source.Content)
}

func lexicalOverlapForText(needNeed contracts.EvidenceNeed, text string) int {
	textWords := need.LexicalWords(text)
	queryWords := make(map[string]bool)
	for _, entity := range needNeed.Entities {
		for word := range need.LexicalWords(entity) {
			queryWords[word] = true
		}
	}
	for _, constraint := range needNeed.TimeConstraints {
		for word := range need.LexicalWords(constraint) {
			queryWords[word] = true
		}
	}
	for _, operand := range needNeed.Operands {
		for word := range need.LexicalWords(operand.Name) {
			queryWords[word] = true
		}
	}
	overlap := 0
	for word := range queryWords {
		if textWords[word] {
			overlap++
		}
	}
	return overlap
}

func selectExtractiveSpans(needNeed contracts.EvidenceNeed, source contracts.Source) []contracts.SourceSpan {
	spans := sentenceSpans(source)
	if len(spans) == 0 {
		return nil
	}
	type scoredSpan struct {
		span     contracts.SourceSpan
		coverage int
		lexical  int
	}
	scored := make([]scoredSpan, 0, len(spans))
	for _, span := range spans {
		text := string([]rune(source.Content)[span.StartChar:span.EndChar])
		scored = append(scored, scoredSpan{span: span, coverage: sentenceNeedCoverage(needNeed, text), lexical: lexicalOverlapForText(needNeed, text)})
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].coverage != scored[right].coverage {
			return scored[left].coverage > scored[right].coverage
		}
		if scored[left].lexical != scored[right].lexical {
			return scored[left].lexical > scored[right].lexical
		}
		return scored[left].span.StartChar < scored[right].span.StartChar
	})
	selected := make([]contracts.SourceSpan, 0, len(scored))
	for _, item := range scored {
		if item.coverage == 0 && item.lexical == 0 && len(selected) > 0 {
			continue
		}
		selected = append(selected, item.span)
	}
	if len(selected) == 0 {
		selected = append(selected, scored[0].span)
	}
	return selected
}

func sentenceSpans(source contracts.Source) []contracts.SourceSpan {
	runes := []rune(source.Content)
	if len(runes) == 0 {
		return nil
	}
	spans := make([]contracts.SourceSpan, 0, 1)
	start := 0
	for index, runeValue := range runes {
		if !sentenceBoundary(runeValue) {
			continue
		}
		if span, ok := trimmedSpan(source.ID, runes, start, index+1); ok {
			spans = append(spans, span)
		}
		start = index + 1
	}
	if span, ok := trimmedSpan(source.ID, runes, start, len(runes)); ok {
		spans = append(spans, span)
	}
	return spans
}

func sentenceBoundary(value rune) bool {
	switch value {
	case '.', '!', '?', '。', '！', '？', '\n':
		return true
	default:
		return false
	}
}

func trimmedSpan(sourceID string, runes []rune, start, end int) (contracts.SourceSpan, bool) {
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	if start == end {
		return contracts.SourceSpan{}, false
	}
	text := string(runes[start:end])
	return contracts.SourceSpan{SourceID: sourceID, StartChar: start, EndChar: end, SpanDigest: sourceTextDigest(text)}, true
}

// MergePermitted is the two-condition safety gate. A planner may only propose
// MERGE after raw evidence is proven over the real cap and the independently
// grounded extractive alternative still cannot satisfy the Need.
func MergePermitted(rawOverCap bool, needNeed contracts.EvidenceNeed, extracts []EvidenceItem) bool {
	return rawOverCap && !ExtractiveSatisfiesNeed(needNeed, extracts)
}

// ExtractiveSatisfiesNeed reports whether the selected items fully ground the
// Need's explicit constraints.
func ExtractiveSatisfiesNeed(needNeed contracts.EvidenceNeed, items []EvidenceItem) bool {
	if len(items) == 0 {
		return len(needNeed.Entities) == 0 && len(needNeed.TimeConstraints) == 0 && len(needNeed.Operands) == 0 && !needNeed.ListCardinality.Known && needNeed.UpdateState == ""
	}
	var builder strings.Builder
	sourceIDs := make(map[string]bool)
	for _, item := range items {
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(item.Text)
		for _, span := range item.Sources {
			sourceIDs[span.SourceID] = true
		}
	}
	text := builder.String()
	lower := strings.ToLower(text)
	for _, entity := range needNeed.Entities {
		if !strings.Contains(lower, strings.ToLower(entity)) {
			return false
		}
	}
	if len(needNeed.TimeConstraints) > 0 {
		for _, constraint := range needNeed.TimeConstraints {
			if timeOperator(constraint) {
				if !HasTimeEvidence(text) {
					return false
				}
				continue
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(constraint)) {
				return false
			}
		}
	}
	for _, operand := range needNeed.Operands {
		if !need.SourceSupportsOperand(text, operand.Name) {
			return false
		}
	}
	if needNeed.ListCardinality.Known && len(sourceIDs) < needNeed.ListCardinality.Count {
		return false
	}
	return needNeed.UpdateState == "" || stateEvidenceMatches(needNeed.UpdateState, text)
}

func timeOperator(value string) bool {
	switch value {
	case "after", "before", "between", "since", "until", "last", "next", "recent", "last_month", "last_year":
		return true
	default:
		return false
	}
}

// HasTimeEvidence reports whether the text carries a date, relative marker, or
// CJK calendar character.
func HasTimeEvidence(value string) bool {
	lower := strings.ToLower(value)
	return need.NeedDateRE.MatchString(value) || strings.Contains(lower, "today") || strings.Contains(lower, "yesterday") || strings.Contains(lower, "tomorrow") || strings.Contains(value, "年") || strings.Contains(value, "月") || strings.Contains(value, "日")
}

func stateEvidenceMatches(updateState, text string) bool {
	lower := strings.ToLower(text)
	for _, state := range need.SplitStates(updateState) {
		switch state {
		case "current":
			if !strings.Contains(lower, "current") && !strings.Contains(text, "当前") {
				return false
			}
		case "latest":
			if !strings.Contains(lower, "latest") && !strings.Contains(text, "最新") {
				return false
			}
		case "previous":
			if !strings.Contains(lower, "previous") && !strings.Contains(text, "之前") {
				return false
			}
		case "change":
			if !strings.Contains(lower, "chang") && !strings.Contains(text, "变化") {
				return false
			}
		case "conflict":
			if !strings.Contains(lower, "conflict") && !strings.Contains(text, "冲突") && !need.HasNegation(text) {
				return false
			}
		}
	}
	return true
}
