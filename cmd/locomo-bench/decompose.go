package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const queryDecompositionSystemPrompt = `Decompose the user's question into distinct, standalone search queries that can retrieve the separate facts needed to answer it.
Do not answer the question. Return only a JSON array of strings, with no markdown or prose.`

// decomposeQuery reuses the benchmark's answer-model caller. Callers in main.go
// should pass the existing modelCaller value rather than constructing a provider.
func decomposeQuery(ctx context.Context, caller modelCaller, question string, maxSubqueries int) []string {
	fallback := []string{question}
	if ctx == nil || ctx.Err() != nil || caller == nil || maxSubqueries <= 1 {
		return fallback
	}

	encodedQuestion, err := json.Marshal(question)
	if err != nil {
		return fallback
	}
	userPrompt := fmt.Sprintf(
		"Maximum total queries including the original: %d. Return at most %d derived queries.\nOriginal question: %s",
		maxSubqueries,
		maxSubqueries-1,
		encodedQuestion,
	)
	response, err := caller(ctx, queryDecompositionSystemPrompt, userPrompt)
	if err != nil || ctx.Err() != nil {
		return fallback
	}

	var candidates []string
	if err := json.Unmarshal([]byte(response), &candidates); err != nil || len(candidates) == 0 || len(candidates) > maxSubqueries {
		return fallback
	}

	originalKey := canonicalQuery(question)
	seen := make(map[string]struct{}, len(candidates))
	rewrites := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		key := canonicalQuery(candidate)
		if key == "" {
			return fallback
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if key != originalKey {
			rewrites = append(rewrites, candidate)
		}
	}

	if len(candidates) > 1 && len(seen) == 1 {
		return fallback
	}
	if len(rewrites) == 0 || len(rewrites)+1 > maxSubqueries {
		return fallback
	}
	return append(rewrites, question)
}

func canonicalQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}
