package memory

import (
	"regexp"
	"strings"
)

// EnumerationIntent is the deterministic query classification used by the
// optional cluster sweep. It deliberately carries no model-derived state.
type EnumerationIntent struct {
	IsEnumeration bool
}

var (
	enumerationPattern = regexp.MustCompile(`(?i)\b(?:what\s+(?:thing|things|item|items|activity|activities|place|places|event|events|ways)|which\s+(?:thing|things|item|items|activity|activities|place|places|event|events|option|options|ones)|how\s+(?:many|often)|all\s+the|every\s+time|list\s+(?:of|all|the|out|every))\b|哪些|几次|多少(?:个|次|种|项|件|人|地方)|每次|分别`)
	comparisonPattern  = regexp.MustCompile(`(?i)\b(?:compare|comparison|difference\s+between)\b|\bwhich\b[^?!.]{0,80}\b(?:more|less|than)\b|比较|差异|哪个[^?。！？]{0,40}(?:更多|更少|较多|较少)`)
)

// ParseEnumerationIntent detects questions that need broad enumeration,
// counting, or comparison evidence. Ordinary single-fact what/when questions
// intentionally return false.
func ParseEnumerationIntent(query string) EnumerationIntent {
	query = strings.TrimSpace(query)
	if query == "" {
		return EnumerationIntent{}
	}
	return EnumerationIntent{IsEnumeration: enumerationPattern.MatchString(query) || comparisonPattern.MatchString(query)}
}
