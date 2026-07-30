package evidencecompiler

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// evidenceItem is an internal, fully reconstructable bundle candidate. Text is
// always either a complete active source or the exact bytes named by Sources.
type evidenceItem struct {
	Kind         ActionKind
	Text         string
	Sources      []SourceSpan
	CandidateIDs []string
	rank         int
	coverage     int
	lexical      int
}

type extractionPlan struct {
	raw      []evidenceItem
	extracts []evidenceItem
}

type sourceCandidate struct {
	source       Source
	candidateIDs []string
	rank         int
	coverage     int
	lexical      int
}

// buildExtractionPlan never fetches or searches. It turns already-resolved
// candidate lineage into ordered raw and extractive alternatives; admission is
// deliberately left to compiler.go because it must use the real tokenizer.
func buildExtractionPlan(need EvidenceNeed, candidates validatedCandidates, sources map[string]Source) (extractionPlan, error) {
	need, err := canonicalizeNeed(need)
	if err != nil {
		return extractionPlan{}, err
	}
	for _, sourceID := range candidates.sourceIDs {
		if _, err := validSource(sourceID, candidates.allowlist, sources); err != nil {
			return extractionPlan{}, err
		}
	}

	bySource := make(map[string]*sourceCandidate, len(candidates.sourceIDs))
	for _, candidate := range candidates.ordered {
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
		current.candidateIDs = canonicalStrings(current.candidateIDs)
		current.coverage = sourceNeedCoverage(need, current.source)
		current.lexical = sourceLexicalOverlap(need, current.source)
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

	plan := extractionPlan{}
	for _, current := range ordered {
		full := fullSourceSpan(current.source)
		plan.raw = append(plan.raw, evidenceItem{
			Kind:         ActionKeep,
			Text:         current.source.Content,
			Sources:      []SourceSpan{full},
			CandidateIDs: append([]string(nil), current.candidateIDs...),
			rank:         current.rank,
			coverage:     current.coverage,
			lexical:      current.lexical,
		})
		for _, span := range selectExtractiveSpans(need, current.source) {
			text, err := validateSourceSpan(span, candidates.allowlist, sources)
			if err != nil {
				return extractionPlan{}, err
			}
			plan.extracts = append(plan.extracts, evidenceItem{
				Kind:         ActionExtract,
				Text:         text,
				Sources:      []SourceSpan{span},
				CandidateIDs: append([]string(nil), current.candidateIDs...),
				rank:         current.rank,
				coverage:     sentenceNeedCoverage(need, text),
				lexical:      lexicalOverlapForText(need, text),
			})
		}
	}
	sortEvidenceItems(plan.extracts)
	return plan, nil
}

func selectPackingItems(plan extractionPlan, rawFits bool) []evidenceItem {
	if rawFits {
		return cloneEvidenceItems(plan.raw)
	}
	return cloneEvidenceItems(plan.extracts)
}

func cloneEvidenceItems(items []evidenceItem) []evidenceItem {
	clone := make([]evidenceItem, len(items))
	for index, item := range items {
		clone[index] = item
		clone[index].Sources = append([]SourceSpan(nil), item.Sources...)
		clone[index].CandidateIDs = append([]string(nil), item.CandidateIDs...)
	}
	return clone
}

func sortEvidenceItems(items []evidenceItem) {
	sort.Slice(items, func(left, right int) bool {
		if items[left].coverage != items[right].coverage {
			return items[left].coverage > items[right].coverage
		}
		if items[left].lexical != items[right].lexical {
			return items[left].lexical > items[right].lexical
		}
		if items[left].rank != items[right].rank {
			return items[left].rank < items[right].rank
		}
		return evidenceItemID(items[left]) < evidenceItemID(items[right])
	})
}

func evidenceItemID(item evidenceItem) string {
	if len(item.Sources) == 0 {
		return ""
	}
	span := item.Sources[0]
	return fmt.Sprintf("%s:%d:%d", span.SourceID, span.StartChar, span.EndChar)
}

func fullSourceSpan(source Source) SourceSpan {
	return SourceSpan{
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

func sourceNeedCoverage(need EvidenceNeed, source Source) int {
	coverage := sentenceNeedCoverage(need, source.Content)
	if len(need.TimeConstraints) > 0 && (source.OccurredAt != nil || hasTimeEvidence(source.Content)) {
		coverage++
	}
	return coverage
}

func sentenceNeedCoverage(need EvidenceNeed, text string) int {
	coverage := 0
	lower := strings.ToLower(text)
	for _, entity := range need.Entities {
		if strings.Contains(lower, strings.ToLower(entity)) {
			coverage++
		}
	}
	for _, operand := range need.Operands {
		if sourceSupportsOperand(text, operand.Name) {
			coverage++
		}
	}
	if need.UpdateState != "" && stateEvidenceMatches(need.UpdateState, text) {
		coverage++
	}
	return coverage
}

func sourceLexicalOverlap(need EvidenceNeed, source Source) int {
	return lexicalOverlapForText(need, source.Content)
}

func lexicalOverlapForText(need EvidenceNeed, text string) int {
	textWords := lexicalWords(text)
	queryWords := make(map[string]bool)
	for _, entity := range need.Entities {
		for word := range lexicalWords(entity) {
			queryWords[word] = true
		}
	}
	for _, constraint := range need.TimeConstraints {
		for word := range lexicalWords(constraint) {
			queryWords[word] = true
		}
	}
	for _, operand := range need.Operands {
		for word := range lexicalWords(operand.Name) {
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

func selectExtractiveSpans(need EvidenceNeed, source Source) []SourceSpan {
	spans := sentenceSpans(source)
	if len(spans) == 0 {
		return nil
	}
	type scoredSpan struct {
		span     SourceSpan
		coverage int
		lexical  int
	}
	scored := make([]scoredSpan, 0, len(spans))
	for _, span := range spans {
		text := string([]rune(source.Content)[span.StartChar:span.EndChar])
		scored = append(scored, scoredSpan{span: span, coverage: sentenceNeedCoverage(need, text), lexical: lexicalOverlapForText(need, text)})
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
	selected := make([]SourceSpan, 0, len(scored))
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

func sentenceSpans(source Source) []SourceSpan {
	runes := []rune(source.Content)
	if len(runes) == 0 {
		return nil
	}
	spans := make([]SourceSpan, 0, 1)
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

func trimmedSpan(sourceID string, runes []rune, start, end int) (SourceSpan, bool) {
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	if start == end {
		return SourceSpan{}, false
	}
	text := string(runes[start:end])
	return SourceSpan{SourceID: sourceID, StartChar: start, EndChar: end, SpanDigest: sourceTextDigest(text)}, true
}

// mergePermitted is the two-condition safety gate. A planner may only propose
// MERGE after raw evidence is proven over the real cap and the independently
// grounded extractive alternative still cannot satisfy the Need.
func mergePermitted(rawOverCap bool, need EvidenceNeed, extracts []evidenceItem) bool {
	return rawOverCap && !extractiveSatisfiesNeed(need, extracts)
}

func extractiveSatisfiesNeed(need EvidenceNeed, items []evidenceItem) bool {
	if len(items) == 0 {
		return len(need.Entities) == 0 && len(need.TimeConstraints) == 0 && len(need.Operands) == 0 && !need.ListCardinality.Known && need.UpdateState == ""
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
	for _, entity := range need.Entities {
		if !strings.Contains(lower, strings.ToLower(entity)) {
			return false
		}
	}
	if len(need.TimeConstraints) > 0 {
		for _, constraint := range need.TimeConstraints {
			if timeOperator(constraint) {
				if !hasTimeEvidence(text) {
					return false
				}
				continue
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(constraint)) {
				return false
			}
		}
	}
	for _, operand := range need.Operands {
		if !sourceSupportsOperand(text, operand.Name) {
			return false
		}
	}
	if need.ListCardinality.Known && len(sourceIDs) < need.ListCardinality.Count {
		return false
	}
	return need.UpdateState == "" || stateEvidenceMatches(need.UpdateState, text)
}

func timeOperator(value string) bool {
	switch value {
	case "after", "before", "between", "since", "until", "last", "next", "recent", "last_month", "last_year":
		return true
	default:
		return false
	}
}

func hasTimeEvidence(value string) bool {
	lower := strings.ToLower(value)
	return needDateRE.MatchString(value) || strings.Contains(lower, "today") || strings.Contains(lower, "yesterday") || strings.Contains(lower, "tomorrow") || strings.Contains(value, "年") || strings.Contains(value, "月") || strings.Contains(value, "日")
}

func stateEvidenceMatches(updateState, text string) bool {
	lower := strings.ToLower(text)
	for _, state := range splitStates(updateState) {
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
			if !strings.Contains(lower, "conflict") && !strings.Contains(text, "冲突") && !hasNegation(text) {
				return false
			}
		}
	}
	return true
}
