package memory

import (
	"strings"
	"unicode"
)

// isCJK reports whether r is a CJK character within the Unicode ranges
// specified in design.md D6.
func isCJK(r rune) bool {
	switch {
	// CJK Unified Ideographs
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	// CJK Unified Ideographs Extension A
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	// CJK Unified Ideographs Extension B
	case r >= 0x20000 && r <= 0x2A6DF:
		return true
	// CJK Unified Ideographs Extension C
	case r >= 0x2A700 && r <= 0x2B73F:
		return true
	// CJK Unified Ideographs Extension D
	case r >= 0x2B740 && r <= 0x2B81F:
		return true
	// CJK Unified Ideographs Extension E
	case r >= 0x2B820 && r <= 0x2CEAF:
		return true
	// CJK Unified Ideographs Extension F
	case r >= 0x2CEB0 && r <= 0x2EBEF:
		return true
	// CJK Compatibility Ideographs
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	// CJK Radicals Supplement
	case r >= 0x2E80 && r <= 0x2EFF:
		return true
	// CJK Symbols and Punctuation
	case r >= 0x3000 && r <= 0x303F:
		return true
	// Hiragana
	case r >= 0x3040 && r <= 0x309F:
		return true
	// Katakana
	case r >= 0x30A0 && r <= 0x30FF:
		return true
	// Katakana Phonetic Extensions
	case r >= 0x31F0 && r <= 0x31FF:
		return true
	// Hangul Syllables
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	// Hangul Jamo
	case r >= 0x1100 && r <= 0x11FF:
		return true
	// Hangul Compatibility Jamo
	case r >= 0x3130 && r <= 0x318F:
		return true
	// Halfwidth Katakana
	case r >= 0xFF65 && r <= 0xFF9F:
		return true
	default:
		return false
	}
}

type runKind string

const (
	runASCII runKind = "ascii"
	runCJK   runKind = "cjk"
	runWS    runKind = "ws"
	runOther runKind = "other"
)

type tokenRun struct {
	Kind runKind
	Text string
}

// tokenize splits query into runs by character class.
func tokenize(query string) []tokenRun {
	if query == "" {
		return nil
	}

	var runs []tokenRun
	runes := []rune(query)
	i := 0

	for i < len(runes) {
		r := runes[i]
		var kind runKind
		switch {
		case isCJK(r):
			kind = runCJK
		case unicode.IsSpace(r):
			kind = runWS
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '\''):
			kind = runASCII
		default:
			kind = runOther
		}

		j := i + 1
		for j < len(runes) {
			next := classify(runes[j])
			if next != kind {
				break
			}
			j++
		}

		runs = append(runs, tokenRun{Kind: kind, Text: string(runes[i:j])})
		i = j
	}

	return runs
}

func classify(r rune) runKind {
	switch {
	case isCJK(r):
		return runCJK
	case unicode.IsSpace(r):
		return runWS
	case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '\''):
		return runASCII
	default:
		return runOther
	}
}

// trigrams produces the sliding 3-grams from a CJK run.
func trigrams(cjkRun string) []string {
	runes := []rune(cjkRun)
	if len(runes) < 3 {
		return nil
	}
	result := make([]string, 0, len(runes)-2)
	for i := 0; i <= len(runes)-3; i++ {
		result = append(result, string(runes[i:i+3]))
	}
	return result
}

// buildPlan combines tokens into an FTS5 expression or signals LIKE fallback.
func buildPlan(query string) (matchExpr string, ok bool) {
	runs := tokenize(query)
	var parts []string
	needFallback := false

	for _, r := range runs {
		switch r.Kind {
		case runASCII:
			parts = append(parts, strings.ToLower(r.Text))
		case runCJK:
			runes := []rune(r.Text)
			if len(runes) < 3 {
				needFallback = true
			} else {
				tgs := trigrams(r.Text)
				if len(tgs) > 1 {
					expr := make([]string, len(tgs))
					for i, tg := range tgs {
						expr[i] = `"` + tg + `"`
					}
					parts = append(parts, "("+strings.Join(expr, " AND ")+")")
				} else {
					parts = append(parts, `"`+tgs[0]+`"`)
				}
			}
		case runWS, runOther:
			// Skip whitespace and punctuation.
		}
	}

	if needFallback || len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, " AND "), true
}

// likeFragments extracts the searchable fragments for LIKE fallback.
func likeFragments(query string) []string {
	runs := tokenize(query)
	var fragments []string
	for _, r := range runs {
		switch r.Kind {
		case runASCII, runCJK:
			fragments = append(fragments, r.Text)
		}
	}
	return fragments
}
