package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// compiler_eval.go hosts the byte-replay compiler arms (T069/T114). The
// exact-token relevance arm selects evidence with a deterministic, offline,
// token-level exact match against the query — no embedding, no LLM, no
// reranker — so it degrades to pure client-side scoring by construction.

// exactTokenArmFallback is recorded in the trace when no candidate shares a
// single query token with the exact-token selection.
const exactTokenArmFallback = "exact_token_no_overlap"

// exactTokenSelection is one candidate chosen by the exact-token relevance
// arm together with its deterministic score and original rank (for stable
// tie-breaking).
type exactTokenSelection struct {
	CandidateID string
	Rank        int
	Score       float64
}

// tokenizeExactToken splits text into lowercase alphanumeric tokens. It is
// deliberately byte-local and deterministic so the arm is reproducible
// without any external tokenizer.
func tokenizeExactToken(text string) map[string]bool {
	tokens := make(map[string]bool)
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens[current.String()] = true
			current.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// selectExactTokenCandidates ranks candidates by exact query-token recall
// (|queryTokens ∩ candidateTokens| / |queryTokens|), ties broken by original
// rank then stable order. Candidates with zero overlap are excluded: an arm
// with no relevant evidence must fall back (recorded in the trace) instead of
// forcing unrelated text into the bundle.
func selectExactTokenCandidates(query string, candidates []evidencecompiler.Candidate, limit int) []exactTokenSelection {
	queryTokens := tokenizeExactToken(query)
	selected := make([]exactTokenSelection, 0, len(candidates))
	for index := range candidates {
		candidate := &candidates[index]
		if len(queryTokens) == 0 {
			continue
		}
		candidateTokens := tokenizeExactToken(candidate.Text)
		overlap := 0
		for token := range candidateTokens {
			if queryTokens[token] {
				overlap++
			}
		}
		score := float64(overlap) / float64(len(queryTokens))
		if score <= 0 {
			continue
		}
		selected = append(selected, exactTokenSelection{
			CandidateID: candidate.ID,
			Rank:        candidate.Rank,
			Score:       score,
		})
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Score != selected[j].Score {
			return selected[i].Score > selected[j].Score
		}
		return selected[i].Rank < selected[j].Rank
	})
	if limit > 0 && len(selected) > limit {
		selected = selected[:limit]
	}
	return selected
}

// compileExactTokenArm builds an evidencecompiler.Bundle (plus a minimal
// valid trace) from the exact-token relevance selection over the same
// candidate list the extractive/planner arms receive. The Bundle shape is
// identical to the compiler engine's output so the shared downstream bundle
// and trace builders work unchanged.
func compileExactTokenArm(query string, candidates []evidencecompiler.Candidate, limit int) (evidencecompiler.Bundle, evidencecompiler.Trace, error) {
	if len(candidates) == 0 {
		return evidencecompiler.Bundle{}, evidencecompiler.Trace{}, fmt.Errorf("no candidates for exact-token compilation")
	}
	selections := selectExactTokenCandidates(query, candidates, limit)
	items := make([]evidencecompiler.BundleItem, 0, len(selections))
	candidateIDs := make([]string, 0, len(selections))
	var sourceIDs []string
	seenSources := make(map[string]bool)
	for _, selection := range selections {
		var candidate *evidencecompiler.Candidate
		for index := range candidates {
			if candidates[index].ID == selection.CandidateID {
				candidate = &candidates[index]
				break
			}
		}
		if candidate == nil {
			continue
		}
		sources := make([]evidencecompiler.SourceSpan, 0, len(candidate.SourceIDs))
		for _, sourceID := range candidate.SourceIDs {
			sources = append(sources, evidencecompiler.SourceSpan{SourceID: sourceID})
			if !seenSources[sourceID] {
				seenSources[sourceID] = true
				sourceIDs = append(sourceIDs, sourceID)
			}
		}
		items = append(items, evidencecompiler.BundleItem{
			Kind:         evidencecompiler.ActionExtract,
			Text:         candidate.Text,
			Sources:      sources,
			CandidateIDs: []string{candidate.ID},
		})
		candidateIDs = append(candidateIDs, candidate.ID)
	}
	fallback := ""
	if len(items) == 0 {
		fallback = exactTokenArmFallback
	}
	trace := evidencecompiler.Trace{
		CandidateDigest:    evalJSONDigest(candidateIDs),
		CandidateIDs:       candidateIDs,
		CandidateSourceIDs: sourceIDs,
		FallbackReason:     fallback,
		Valid:              true,
	}
	bundle := evidencecompiler.Bundle{
		Items:     items,
		SourceIDs: sourceIDs,
	}
	return bundle, trace, nil
}
