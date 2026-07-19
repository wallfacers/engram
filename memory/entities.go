package memory

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// EntityNorm normalizes an entity string into its index key: trimmed, folded to
// lower case, and with internal whitespace runs collapsed to a single space.
// Returns "" for entities that carry no indexable content. Both the extraction
// indexer (PutEntities) and the query tokenizer (EntityQueryTokens) route
// through this so the entity-match retrieval signal compares like with like.
func EntityNorm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// EntityQueryTokens derives candidate entity keys from a free-text query for the
// entity-match retrieval signal. It emits both single word-like runs and the
// whole normalized query, so multi-word entities ("new york") and single-word
// entities ("sweden") both have a chance to match an indexed entity_norm.
// CJK runs are emitted whole (word segmentation is out of scope); ASCII/digit
// runs are split on non-alphanumeric boundaries.
func EntityQueryTokens(query string) []string {
	norm := EntityNorm(query)
	if norm == "" {
		return nil
	}
	out := []string{norm}
	seen := map[string]struct{}{norm: {}}

	add := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		if _, dup := seen[tok]; dup {
			return
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}

	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			add(cur.String())
			cur.Reset()
		}
	}
	for _, r := range norm {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			// Each CJK character is a candidate; also accumulate runs below.
			flush()
			add(string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func entityWordTokens(s string) []string {
	norm := EntityNorm(s)
	if norm == "" {
		return nil
	}
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}
	for _, r := range norm {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			current.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func containsEntityTokenSequence(query, entity []string) bool {
	if len(entity) == 0 || len(entity) > len(query) {
		return false
	}
	for start := 0; start <= len(query)-len(entity); start++ {
		matched := true
		for i := range entity {
			if query[start+i] != entity[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// EntityMatchCountsForQuery combines exact entity-token matching with a whole
// query substring match against entity_raw. The latter links natural-language
// questions to multi-word extracted entities without changing the token API.
func (s *EntryStore) EntityMatchCountsForQuery(ctx context.Context, query string) (map[string]int, error) {
	if s == nil {
		return nil, nil
	}
	tokens := make(map[string]struct{}, len(EntityQueryTokens(query)))
	for _, token := range EntityQueryTokens(query) {
		tokens[token] = struct{}{}
	}
	queryWordTokens := entityWordTokens(query)
	rows, err := s.db.QueryContext(ctx, `SELECT entry_name, entity_norm, entity_raw FROM memory_entities`)
	if err != nil {
		return nil, fmt.Errorf("memory: entity query match: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	counts := make(map[string]int)
	seen := make(map[string]map[string]struct{})
	for rows.Next() {
		var name, entity, raw string
		if err := rows.Scan(&name, &entity, &raw); err != nil {
			return nil, fmt.Errorf("memory: scan entity query match: %w", err)
		}
		rawNorm := EntityNorm(raw)
		_, exact := tokens[entity]
		phrase := rawNorm != "" && containsEntityTokenSequence(queryWordTokens, entityWordTokens(rawNorm))
		if !exact && !phrase {
			continue
		}
		if seen[name] == nil {
			seen[name] = make(map[string]struct{})
		}
		if _, duplicate := seen[name][entity]; duplicate {
			continue
		}
		seen[name][entity] = struct{}{}
		counts[name]++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read entity query match: %w", err)
	}
	return counts, nil
}
