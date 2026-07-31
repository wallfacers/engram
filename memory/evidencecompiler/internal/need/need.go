// Package need builds the deterministic EvidenceNeed for a query and the
// source-grounded relations between resolved sources. It is pure and has no
// IO: no resolver, tokenizer, or model dependency.
package need

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

var (
	needEntityRE       = regexp.MustCompile(`\b[\p{Lu}][\p{L}\p{M}'-]*\b`)
	needHanNameRE      = regexp.MustCompile(`(?:张|王|李|赵|刘|陈|杨|黄|周|吴|徐|孙|胡|朱|高|林|何|郭|马|罗|梁|宋|郑|谢|韩|唐|冯|于|董|萧|程|曹|袁|邓|许|傅|沈|曾|彭|吕|苏|卢|蒋|蔡|贾|丁|魏|薛|叶|阎|余|潘|杜|戴|夏|钟|汪|田|任|姜|范|方|石|姚|谭|廖|邹|熊|金|陆|郝|孔|白|崔|康|毛|邱|秦|江|史|顾|侯|邵|孟|龙|万|段|雷|钱|汤)[\p{Han}]`)
	needDateRE         = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	needListCountRE    = regexp.MustCompile(`(?i)\b(?:list|top|first|last|give|name|show)\s+(\d+)\b`)
	needNounCountRE    = regexp.MustCompile(`(?i)\b(\d+)\s+(?:items?|things?|cities|people|persons?|facts?|events?|places|names|reasons|examples|times)\b`)
	needChineseCountRE = regexp.MustCompile(`(?:列出|给出|前|最近)([一二三四五六七八九十0-9]+)(?:个|条|项|位|次)`)
	needSplitRE        = regexp.MustCompile(`[?？;；]+`)
	needSpaceRE        = regexp.MustCompile(`\s+`)
)

// NeedDateRE matches explicit ISO dates; extractive time evidence uses it.
var NeedDateRE = needDateRE

var needEntityStopWords = map[string]bool{
	"A": true, "An": true, "And": true, "Are": true, "Can": true, "Did": true,
	"Do": true, "Does": true, "For": true, "Give": true, "How": true, "In": true,
	"Is": true, "List": true, "Name": true, "Show": true, "The": true, "Then": true,
	"To": true, "What": true, "When": true, "Where": true, "Which": true, "Who": true,
	"Why": true,
}

// BuildNeed deterministically extracts only explicit, query-local constraints.
// It intentionally has no benchmark category, Retriever, or model dependency.
func BuildNeed(query string) contracts.EvidenceNeed {
	return deterministicNeed(query)
}

func deterministicNeed(query string) contracts.EvidenceNeed {
	need := contracts.EvidenceNeed{
		Entities:        extractEntities(query),
		TimeConstraints: extractTimeConstraints(query),
		Operands:        extractOperands(query),
		ListCardinality: extractCardinality(query),
		UpdateState:     extractUpdateState(query),
	}
	canonical, err := CanonicalizeNeed(need)
	if err != nil {
		// All fields above are generated locally. Returning the zero value here is
		// safer than manufacturing a partially valid Need should this ever drift.
		return contracts.EvidenceNeed{}
	}
	return canonical
}

func extractEntities(query string) []string {
	entities := make([]string, 0)
	for _, entity := range needEntityRE.FindAllString(query, -1) {
		if !needEntityStopWords[entity] {
			entities = append(entities, entity)
		}
	}
	entities = append(entities, needHanNameRE.FindAllString(query, -1)...)
	return CanonicalStrings(entities)
}

func extractTimeConstraints(query string) []string {
	lower := strings.ToLower(query)
	constraints := needDateRE.FindAllString(query, -1)
	for marker, canonical := range map[string]string{
		"after": "after", "before": "before", "between": "between", "since": "since",
		"until": "until", "last": "last", "next": "next", "today": "today",
		"yesterday": "yesterday", "tomorrow": "tomorrow", "之前": "before", "以后": "after",
		"之后": "after", "最近": "recent", "上个月": "last_month", "去年": "last_year",
	} {
		if strings.Contains(lower, marker) {
			constraints = append(constraints, canonical)
		}
	}
	return CanonicalStrings(constraints)
}

func extractCardinality(query string) contracts.Cardinality {
	for _, expression := range []*regexp.Regexp{needListCountRE, needNounCountRE, needChineseCountRE} {
		matches := expression.FindStringSubmatch(query)
		if len(matches) != 2 {
			continue
		}
		if count := parseExplicitCount(matches[1]); count > 0 {
			return contracts.Cardinality{Known: true, Count: count}
		}
	}
	return contracts.Cardinality{}
}

func parseExplicitCount(value string) int {
	count := 0
	for _, runeValue := range value {
		if runeValue >= '0' && runeValue <= '9' {
			count = count*10 + int(runeValue-'0')
			continue
		}
		if count != 0 {
			return 0
		}
		switch runeValue {
		case '一':
			count = 1
		case '二', '两':
			count = 2
		case '三':
			count = 3
		case '四':
			count = 4
		case '五':
			count = 5
		case '六':
			count = 6
		case '七':
			count = 7
		case '八':
			count = 8
		case '九':
			count = 9
		case '十':
			count = 10
		default:
			return 0
		}
	}
	return count
}

func extractOperands(query string) []contracts.Operand {
	segments := needSplitRE.Split(query, -1)
	operands := make([]contracts.Operand, 0, len(segments))
	for _, segment := range segments {
		name := canonicalOperand(segment)
		if name != "" {
			operands = append(operands, contracts.Operand{Name: name})
		}
	}
	if len(operands) < 2 {
		lower := strings.ToLower(query)
		for _, separator := range []string{" and then ", " then ", " also ", " versus ", " vs. ", " vs "} {
			if !strings.Contains(lower, separator) {
				continue
			}
			parts := strings.Split(lower, separator)
			operands = operands[:0]
			for _, part := range parts {
				if name := canonicalOperand(part); name != "" {
					operands = append(operands, contracts.Operand{Name: name})
				}
			}
			break
		}
	}
	return canonicalOperands(operands)
}

func canonicalOperand(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ".,!?，。！？；;")
	value = needSpaceRE.ReplaceAllString(value, " ")
	return strings.ToLower(value)
}

func extractUpdateState(query string) string {
	lower := strings.ToLower(query)
	states := make([]string, 0, 2)
	for marker, canonical := range map[string]string{
		"latest": "latest", "current": "current", "currently": "current", "previous": "previous",
		"changed": "change", "change": "change", "conflict": "conflict", "最新": "latest",
		"当前": "current", "之前": "previous", "变化": "change", "冲突": "conflict",
	} {
		if strings.Contains(lower, marker) {
			states = append(states, canonical)
		}
	}
	return strings.Join(CanonicalStrings(states), "|")
}

// CanonicalizeNeed normalizes a Need: sorted unique strings, checked operands,
// validated cardinality and a cloned validated gap. It is shared by the
// deterministic builder and the planner merge path.
func CanonicalizeNeed(need contracts.EvidenceNeed) (contracts.EvidenceNeed, error) {
	need.Entities = CanonicalStrings(need.Entities)
	need.TimeConstraints = CanonicalStrings(need.TimeConstraints)
	operands, err := canonicalOperandsChecked(need.Operands)
	if err != nil {
		return contracts.EvidenceNeed{}, err
	}
	need.Operands = operands
	if need.ListCardinality.Known {
		if need.ListCardinality.Count <= 0 {
			return contracts.EvidenceNeed{}, fmt.Errorf("%w: known cardinality must be positive", contracts.ErrInvalidNeed)
		}
	} else if need.ListCardinality.Count != 0 {
		return contracts.EvidenceNeed{}, fmt.Errorf("%w: unknown cardinality cannot contain a count", contracts.ErrInvalidNeed)
	}
	need.UpdateState = strings.Join(CanonicalStrings(strings.FieldsFunc(need.UpdateState, func(r rune) bool { return r == '|' })), "|")
	if need.Gap != nil {
		gap := *need.Gap
		if err := validateStructuredGap(gap); err != nil {
			return contracts.EvidenceNeed{}, err
		}
		need.Gap = &gap
	}
	return need, nil
}

// CanonicalStrings returns sorted, unique, trimmed strings. It is the shared
// canonical form for entity lists and source-ID unions.
func CanonicalStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		canonical = append(canonical, value)
	}
	sort.Strings(canonical)
	return canonical
}

func canonicalOperands(values []contracts.Operand) []contracts.Operand {
	operands, _ := canonicalOperandsChecked(values)
	return operands
}

func canonicalOperandsChecked(values []contracts.Operand) ([]contracts.Operand, error) {
	byName := make(map[string]bool, len(values))
	operands := make([]contracts.Operand, 0, len(values))
	for _, operand := range values {
		operand.Name = canonicalOperand(operand.Name)
		if operand.Name == "" || !utf8.ValidString(operand.Name) {
			return nil, fmt.Errorf("%w: operand is empty or invalid UTF-8", contracts.ErrInvalidNeed)
		}
		if satisfied, exists := byName[operand.Name]; exists {
			byName[operand.Name] = satisfied || operand.Satisfied
			continue
		}
		byName[operand.Name] = operand.Satisfied
	}
	for name, satisfied := range byName {
		operands = append(operands, contracts.Operand{Name: name, Satisfied: satisfied})
	}
	sort.Slice(operands, func(left, right int) bool { return operands[left].Name < operands[right].Name })
	return operands, nil
}

func validateStructuredGap(gap contracts.StructuredGap) error {
	if strings.TrimSpace(gap.SourceNeed) == "" {
		return fmt.Errorf("%w: gap needs an auditable source requirement", contracts.ErrInvalidNeed)
	}
	switch gap.Kind {
	case contracts.GapEntity:
		if strings.TrimSpace(gap.Entity) == "" || gap.Operand != "" || gap.Start != nil || gap.End != nil {
			return fmt.Errorf("%w: entity gap fields are invalid", contracts.ErrInvalidNeed)
		}
	case contracts.GapTimeRange:
		if gap.Entity != "" || gap.Operand != "" || (gap.Start == nil && gap.End == nil) || (gap.Start != nil && gap.End != nil && gap.End.Before(*gap.Start)) {
			return fmt.Errorf("%w: time range gap fields are invalid", contracts.ErrInvalidNeed)
		}
	case contracts.GapSecondOperand:
		if strings.TrimSpace(gap.Operand) == "" || gap.Entity != "" || gap.Start != nil || gap.End != nil {
			return fmt.Errorf("%w: second operand gap fields are invalid", contracts.ErrInvalidNeed)
		}
	default:
		return fmt.Errorf("%w: unknown gap kind %q", contracts.ErrInvalidNeed, gap.Kind)
	}
	return nil
}

// MergePlannerNeed allows additions but never lets a proposal remove or alter
// an explicit deterministic constraint. Unknown cardinality is intentionally
// sticky: a planner cannot invent a count from an unconstrained question.
func MergePlannerNeed(base, proposal contracts.EvidenceNeed) (contracts.EvidenceNeed, error) {
	base, err := CanonicalizeNeed(base)
	if err != nil {
		return contracts.EvidenceNeed{}, err
	}
	proposal, err = CanonicalizeNeed(proposal)
	if err != nil {
		return contracts.EvidenceNeed{}, err
	}
	if !containsAll(proposal.Entities, base.Entities) || !containsAll(proposal.TimeConstraints, base.TimeConstraints) || !containsAll(operandNames(proposal.Operands), operandNames(base.Operands)) {
		return contracts.EvidenceNeed{}, fmt.Errorf("%w: planner proposal removed an explicit constraint", contracts.ErrInvalidNeed)
	}
	if base.ListCardinality.Known {
		if !proposal.ListCardinality.Known || proposal.ListCardinality.Count != base.ListCardinality.Count {
			return contracts.EvidenceNeed{}, fmt.Errorf("%w: planner proposal changed explicit cardinality", contracts.ErrInvalidNeed)
		}
	} else if proposal.ListCardinality.Known {
		return contracts.EvidenceNeed{}, fmt.Errorf("%w: planner proposal invented cardinality", contracts.ErrInvalidNeed)
	}
	if !containsAll(SplitStates(proposal.UpdateState), SplitStates(base.UpdateState)) {
		return contracts.EvidenceNeed{}, fmt.Errorf("%w: planner proposal removed explicit update state", contracts.ErrInvalidNeed)
	}
	if base.Gap != nil && !equalGap(base.Gap, proposal.Gap) {
		return contracts.EvidenceNeed{}, fmt.Errorf("%w: planner proposal removed or changed explicit gap", contracts.ErrInvalidNeed)
	}

	merged := contracts.EvidenceNeed{
		Entities:        CanonicalStrings(append(append([]string(nil), base.Entities...), proposal.Entities...)),
		TimeConstraints: CanonicalStrings(append(append([]string(nil), base.TimeConstraints...), proposal.TimeConstraints...)),
		Operands:        mergeOperands(base.Operands, proposal.Operands),
		ListCardinality: base.ListCardinality,
		UpdateState:     strings.Join(CanonicalStrings(append(SplitStates(base.UpdateState), SplitStates(proposal.UpdateState)...)), "|"),
		Gap:             CloneGap(base.Gap),
	}
	if merged.Gap == nil {
		merged.Gap = CloneGap(proposal.Gap)
	}
	return CanonicalizeNeed(merged)
}

func containsAll(values, required []string) bool {
	available := make(map[string]bool, len(values))
	for _, value := range values {
		available[value] = true
	}
	for _, value := range required {
		if !available[value] {
			return false
		}
	}
	return true
}

func operandNames(operands []contracts.Operand) []string {
	names := make([]string, 0, len(operands))
	for _, operand := range operands {
		names = append(names, operand.Name)
	}
	return names
}

func mergeOperands(left, right []contracts.Operand) []contracts.Operand {
	combined := append(append([]contracts.Operand(nil), left...), right...)
	return canonicalOperands(combined)
}

// SplitStates splits a pipe-joined update-state value.
func SplitStates(value string) []string {
	if value == "" {
		return nil
	}
	return strings.FieldsFunc(value, func(r rune) bool { return r == '|' })
}

// CloneGap deep-copies a StructuredGap, normalizing timestamps to UTC.
func CloneGap(gap *contracts.StructuredGap) *contracts.StructuredGap {
	if gap == nil {
		return nil
	}
	clone := *gap
	if gap.Start != nil {
		value := gap.Start.UTC()
		clone.Start = &value
	}
	if gap.End != nil {
		value := gap.End.UTC()
		clone.End = &value
	}
	return &clone
}

func equalGap(left, right *contracts.StructuredGap) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind || left.Entity != right.Entity || left.Operand != right.Operand || left.SourceNeed != right.SourceNeed {
		return false
	}
	return equalTime(left.Start, right.Start) && equalTime(left.End, right.End)
}

func equalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

// EqualNeed reports canonical Need equality without mutating its arguments.
func EqualNeed(left, right contracts.EvidenceNeed) bool {
	left, leftErr := CanonicalizeNeed(left)
	right, rightErr := CanonicalizeNeed(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if !containsAll(left.Entities, right.Entities) || !containsAll(right.Entities, left.Entities) ||
		!containsAll(left.TimeConstraints, right.TimeConstraints) || !containsAll(right.TimeConstraints, left.TimeConstraints) ||
		!containsAll(operandNames(left.Operands), operandNames(right.Operands)) || !containsAll(operandNames(right.Operands), operandNames(left.Operands)) ||
		left.ListCardinality != right.ListCardinality || left.UpdateState != right.UpdateState || !equalGap(left.Gap, right.Gap) {
		return false
	}
	for index := range left.Operands {
		if left.Operands[index] != right.Operands[index] {
			return false
		}
	}
	return true
}

// BuildRelations only emits relationships that can be independently checked
// against resolved source content or timestamps. It never invents an edge from
// a candidate score, planner assertion, or benchmark category.
func BuildRelations(need contracts.EvidenceNeed, sources map[string]contracts.Source) []contracts.EvidenceRelation {
	ids := make([]string, 0, len(sources))
	for id, source := range sources {
		if source.ID == id && source.Content != "" && utf8.ValidString(source.Content) && validate.SameDigest(source.ContentDigest, source.Content) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	relations := make([]contracts.EvidenceRelation, 0)
	for leftIndex := 0; leftIndex < len(ids); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(ids); rightIndex++ {
			left, right := sources[ids[leftIndex]], sources[ids[rightIndex]]
			if left.OccurredAt != nil && right.OccurredAt != nil && !left.OccurredAt.Equal(*right.OccurredAt) {
				if left.OccurredAt.Before(*right.OccurredAt) {
					relations = append(relations, contracts.EvidenceRelation{Kind: contracts.RelationBefore, LeftSourceID: left.ID, RightSourceID: right.ID})
				} else {
					relations = append(relations, contracts.EvidenceRelation{Kind: contracts.RelationBefore, LeftSourceID: right.ID, RightSourceID: left.ID})
				}
			}
			if sourcesConflict(left.Content, right.Content) {
				relations = append(relations, contracts.EvidenceRelation{Kind: contracts.RelationConflicts, LeftSourceID: left.ID, RightSourceID: right.ID})
			}
			for _, operand := range need.Operands {
				if SourceSupportsOperand(left.Content, operand.Name) && SourceSupportsOperand(right.Content, operand.Name) {
					relations = append(relations, contracts.EvidenceRelation{Kind: contracts.RelationSupportsOperand, LeftSourceID: left.ID, RightSourceID: right.ID, Operand: operand.Name})
				}
			}
		}
	}
	sort.Slice(relations, func(left, right int) bool {
		if relations[left].Kind != relations[right].Kind {
			return relations[left].Kind < relations[right].Kind
		}
		if relations[left].LeftSourceID != relations[right].LeftSourceID {
			return relations[left].LeftSourceID < relations[right].LeftSourceID
		}
		if relations[left].RightSourceID != relations[right].RightSourceID {
			return relations[left].RightSourceID < relations[right].RightSourceID
		}
		return relations[left].Operand < relations[right].Operand
	})
	return relations
}

func sourcesConflict(left, right string) bool {
	if HasNegation(left) == HasNegation(right) {
		return false
	}
	leftWords, rightWords := LexicalWords(left), LexicalWords(right)
	overlap := 0
	for word := range leftWords {
		if rightWords[word] {
			overlap++
		}
	}
	return overlap >= 2
}

// HasNegation reports whether the text carries an explicit negation marker.
func HasNegation(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{" not ", "n't", " no ", " never ", "没有", "不是", "并非", "未"} {
		if strings.Contains(" "+lower+" ", marker) {
			return true
		}
	}
	return false
}

// SourceSupportsOperand reports whether the content contains any lexical word
// of the operand name.
func SourceSupportsOperand(content, operand string) bool {
	words := LexicalWords(operand)
	if len(words) == 0 {
		return false
	}
	contentWords := LexicalWords(content)
	for word := range words {
		if contentWords[word] {
			return true
		}
	}
	return false
}

// LexicalWords tokenizes text into the lowercase word set used by operand
// support, conflict, and lexical-overlap scoring.
func LexicalWords(value string) map[string]bool {
	words := make(map[string]bool)
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !(r >= '一' && r <= '龥')
	}) {
		if len([]rune(token)) > 1 && !needEntityStopWords[strings.Title(token)] {
			words[token] = true
		}
	}
	return words
}
