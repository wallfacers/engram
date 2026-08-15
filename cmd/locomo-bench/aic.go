package main

// 045: answer-in-context (AIC) offline diagnostic. A question is
// answer-in-context when ANY adjudicated gold alias appears as a contiguous
// substring of the normalized final answer context. This is the US1
// packing-fidelity gate metric and a harness diagnostic — per FR-007 it is a
// necessary stop-loss condition, NEVER the sole e2e ship gate (008 lesson:
// coverage alone is not a ship basis).
//
// Normalization is FROZEN (plan R5): lowercase + collapse whitespace. No
// tokenizer, no fuzzy matching — nothing tunable that could be fit to the
// dataset.

import "strings"

// aicNormalize lowercases, strips, and collapses all whitespace runs to one
// space (frozen).
func aicNormalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// aicMatch returns the first alias (input order) that occurs as a contiguous
// substring of the normalized context, and whether any matched.
func aicMatch(context string, goldAliases []string) (string, bool) {
	nc := aicNormalize(context)
	for _, a := range goldAliases {
		na := aicNormalize(a)
		if na == "" {
			continue
		}
		if strings.Contains(nc, na) {
			return a, true
		}
	}
	return "", false
}

// aicArm aggregates one assembly arm (current-k30 / packed / top150-full).
type aicArm struct {
	InContext  int     `json:"in_context"`
	Total      int     `json:"total"`
	AIC        float64 `json:"aic"`
	TokensMean float64 `json:"tokens_mean"`
}

func aicArmFrom(rows []aicRow, tokensOf func(id string) float64) aicArm {
	arm := aicArm{Total: len(rows)}
	var tokSum float64
	for _, r := range rows {
		if r.InContext {
			arm.InContext++
		}
		tokSum += tokensOf(r.QuestionID)
	}
	if arm.Total > 0 {
		arm.AIC = float64(arm.InContext) / float64(arm.Total)
		arm.TokensMean = tokSum / float64(arm.Total)
	}
	return arm
}

// aicRow is the per-question diagnostic row shared by all arms.
type aicRow struct {
	QuestionID        string
	GoldAliases       []string
	MatchedAlias      string
	InContext         bool
	UnmatchableInPool bool // no pool entry contains any gold alias (audit column; stays in the denominator)
}
