package main

// 045: deterministic submodular evidence packing. The packing layer replaces
// applyChunkQuota's count-quota truncation with a budgeted, cost-scaled greedy
// selection over the SAME wide retrieval pool the harness already fetches
// (max(6×topK, 300), chunks.go). Objective (research.md R3, weights frozen at
// 3:1:1:1 rel:cover:fac:div, relevance dominant per the MMR counter-example):
//
//	gain(e) = W_rel·normScore(e) + W_cover·Δcover(e)/|qTerms|
//	        + W_fac·Σ_p max(0, sim(e,p) − maxSim[p]) / n
//	        − W_div·mean_{s∈S} sim(e,s)
//	pick    = argmax gain(e)/cost(e), cost = estTokens(e)
//
// sim is lexical 5-shingle Jaccard (R2: the engine does not expose stored
// embeddings; a deterministic, offline, zero-model-call surrogate — verbatim
// dialogue redundancy is predominantly literal). The mean-based diversity
// penalty realizes the plan's concave diversity (bounded marginal penalty as
// S grows). Everything here is deterministic: same input → same output, with
// stable-ID-ascending tie-breaks. Zero engine changes; zero model calls.

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/wallfacers/engram/memory"
)

const (
	packShingleK = 5
	// packTieEps is the float tolerance for score ties; within eps the
	// lower stable ID wins (frozen tie-break).
	packTieEps = 1e-12
	// packDroppedCap bounds the per-question drop audit list.
	packDroppedCap = 50
	// packItemTemplateTokens is the per-item overhead buildAnswerContextPrompt
	// adds on top of each hit's raw content (event/recorded labels, structural
	// markers, numbering, timeline grouping). packEstimateTokens is content-only
	// and otherwise accurate (probe-pack2: est content 4364 ≈ real content 4347);
	// without this the budget control picked ~192 items but the real prompt
	// shipped 2.5× the anchor budget. Calibrated from probe-pack2 real−est
	// (10875−4364) / ~192 selected ≈ 34/项.
	packItemTemplateTokens = 34
)

// packEstimateTokens replicates estimateTokens (agentic_nav.go: runes/4,
// minimum 1) so 045 stays self-contained when the 044 cleanup deletes that
// file. Selection-time budgeting only — reported parity always uses the
// answerer's real usage.InputTokens (plan R4).
func packEstimateTokens(text string) int {
	n := len([]rune(text))
	if n <= 0 {
		return 0
	}
	t := n / 4
	if t < 1 {
		return 1
	}
	return t
}

// packTokenizeWords lowercases and splits on any non-letter/non-digit rune.
// Contiguous CJK becomes one long word (known limitation; the eval corpus is
// English dialogue — documented in research.md R2).
func packTokenizeWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// packShingles returns the set of word-level k-shingle hashes (FNV-1a over
// NUL-joined words; deterministic across runs and platforms).
func packShingles(words []string, k int) map[uint64]struct{} {
	out := make(map[uint64]struct{}, len(words))
	for i := 0; i+k <= len(words); i++ {
		h := fnv.New64a()
		for j := 0; j < k; j++ {
			if j > 0 {
				h.Write([]byte{0})
			}
			h.Write([]byte(words[i+j]))
		}
		out[h.Sum64()] = struct{}{}
	}
	return out
}

// packCandidate is one pool entry precomputed for the greedy loop.
type packCandidate struct {
	ID         string
	Kind       string // "fact" | "chunk"
	Content    string
	FusedScore float64
	NormScore  float64
	Shingles   map[uint64]struct{}
	EstTokens  int
	// CoverTerms is the candidate's word set intersected with the query's
	// word set (the set-cover numerator).
	CoverTerms map[string]struct{}
}

// packKindOf classifies a hit the same way applyChunkQuota does.
func packKindOf(hit memory.Result) string {
	if strings.HasPrefix(hit.Name, "chunk-") {
		return "chunk"
	}
	return "fact"
}

// buildPackCandidates precomputes pool stats. NormScore is pool-internal
// min–max normalization of the fused RRF score; a degenerate pool (all equal
// scores, or a single entry) maps every candidate to 1.0 (frozen).
func buildPackCandidates(hits []memory.Result, queryWordSet map[string]struct{}) []packCandidate {
	if len(hits) == 0 {
		return nil
	}
	minScore, maxScore := math.Inf(1), math.Inf(-1)
	for _, h := range hits {
		minScore = math.Min(minScore, h.Score)
		maxScore = math.Max(maxScore, h.Score)
	}
	out := make([]packCandidate, len(hits))
	for i, h := range hits {
		words := packTokenizeWords(h.Content)
		cover := make(map[string]struct{}, len(queryWordSet))
		ws := make(map[string]struct{}, len(words))
		for _, w := range words {
			ws[w] = struct{}{}
			if len(queryWordSet) > 0 {
				if _, ok := queryWordSet[w]; ok {
					cover[w] = struct{}{}
				}
			}
		}
		norm := 1.0
		if maxScore > minScore {
			norm = (h.Score - minScore) / (maxScore - minScore)
		}
		out[i] = packCandidate{
			ID:         h.Name,
			Kind:       packKindOf(h),
			Content:    h.Content,
			FusedScore: h.Score,
			NormScore:  norm,
			Shingles:   packShingles(words, packShingleK),
			EstTokens:  packEstimateTokens(h.Content) + packItemTemplateTokens,
			CoverTerms: cover,
		}
	}
	return out
}

// packJaccard is the shingle-set Jaccard similarity.
func packJaccard(a, b map[uint64]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for x := range a {
		if _, ok := b[x]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union <= 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// packWeights are the four objective term weights (rel:cover:fac:div).
type packWeights struct {
	Rel   float64
	Cover float64
	Fac   float64
	Div   float64
}

// defaultPackWeights is the frozen probe/batch/LME value (spec FR-010: zero
// retuning; ablations must set ablation:true in artifacts).
func defaultPackWeights() packWeights {
	return packWeights{Rel: 3, Cover: 1, Fac: 1, Div: 1}
}

// parsePackWeights parses "rel:cover:fac:div" (positive floats).
func parsePackWeights(spec string) (packWeights, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return defaultPackWeights(), nil
	}
	parts := strings.Split(spec, ":")
	if len(parts) != 4 {
		return packWeights{}, fmt.Errorf("--pack-weights wants rel:cover:fac:div, got %q", spec)
	}
	var vals [4]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return packWeights{}, fmt.Errorf("--pack-weights term %d not a positive number in %q", i+1, spec)
		}
		vals[i] = v
	}
	return packWeights{Rel: vals[0], Cover: vals[1], Fac: vals[2], Div: vals[3]}, nil
}

// packDrop is one dropped-pool-entry audit record.
type packDrop struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // budget-exhausted | outcompeted
}

// packSelection is the packing result for one question.
type packSelection struct {
	// Selected is the chosen hits restored to fused-score order (ties by
	// ID ascending) — the shape the answer-context renderer consumes.
	Selected []memory.Result
	// GreedyOrder lists selected IDs in pick order for audit.
	GreedyOrder      []string
	EstTokensUsed    int
	SingletonFallback bool
	// Dropped bounds the drop audit (top by NormScore, then ID).
	Dropped []packDrop
}

// packSelect runs the cost-scaled greedy. Empty pool returns an empty
// selection (caller falls back to the current assembly per FR — pool-empty
// degradation, never a whole-question failure).
func packSelect(hits []memory.Result, query string, budget int, w packWeights) packSelection {
	cands := buildPackCandidates(hits, packQueryWordSet(query))
	n := len(cands)
	sel := packSelection{}
	if n == 0 {
		return sel
	}
	// sim is the full pairwise Jaccard matrix (pool ≤ ~300 → ≤90k entries).
	sim := make([][]float64, n)
	for i := range sim {
		sim[i] = make([]float64, n)
		sim[i][i] = 1
		for j := 0; j < i; j++ {
			s := packJaccard(cands[i].Shingles, cands[j].Shingles)
			sim[i][j] = s
			sim[j][i] = s
		}
	}
	// order iterates candidates by ID ascending so strict-> replacement
	// keeps the lowest ID on ties (frozen tie-break).
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return cands[order[a]].ID < cands[order[b]].ID })

	qTerms := packQueryWordSet(query)
	maxSim := make([]float64, n)
	picked := make([]bool, n)
	covered := make(map[string]struct{}, len(qTerms))
	type pickedRef struct {
		idx  int
		hit  memory.Result
	}
	var chosen []pickedRef
	used := 0
	for {
		best := -1
		bestScore := math.Inf(-1)
		for _, i := range order {
			if picked[i] {
				continue
			}
			cost := cands[i].EstTokens
			if cost <= 0 || cost > budget-used {
				continue
			}
			gain := w.Rel * cands[i].NormScore
			if len(qTerms) > 0 {
				fresh := 0
				for t := range cands[i].CoverTerms {
					if _, ok := covered[t]; !ok {
						fresh++
					}
				}
				gain += w.Cover * float64(fresh)/float64(len(qTerms))
			}
			fac := 0.0
			for p := 0; p < n; p++ {
				if p == i {
					continue
				}
				if d := sim[i][p] - maxSim[p]; d > 0 {
					fac += d
				}
			}
			gain += w.Fac * (fac / float64(n))
			if len(chosen) > 0 {
				div := 0.0
				for _, c := range chosen {
					div += sim[i][c.idx]
				}
				div /= float64(len(chosen))
				gain -= w.Div * div
			}
			score := gain / float64(cost)
			if score > bestScore+packTieEps {
				bestScore = score
				best = i
			}
		}
		if best < 0 {
			break
		}
		picked[best] = true
		used += cands[best].EstTokens
		for p := 0; p < n; p++ {
			if sim[best][p] > maxSim[p] {
				maxSim[p] = sim[best][p]
			}
		}
		for t := range cands[best].CoverTerms {
			covered[t] = struct{}{}
		}
		chosen = append(chosen, pickedRef{idx: best, hit: hits[best]})
	}

	// Singleton fallback: nothing fit at all → keep the highest-NormScore
	// entry (lowest ID on ties), allowed to exceed the budget (only path).
	if len(chosen) == 0 {
		best := -1
		for _, i := range order {
			if best < 0 || cands[i].NormScore > cands[best].NormScore+packTieEps {
				best = i
			}
		}
		picked[best] = true
		used = cands[best].EstTokens
		chosen = append(chosen, pickedRef{idx: best, hit: hits[best]})
		sel.SingletonFallback = true
	}

	for _, c := range chosen {
		sel.GreedyOrder = append(sel.GreedyOrder, cands[c.idx].ID)
		sel.Selected = append(sel.Selected, c.hit)
	}
	sort.SliceStable(sel.Selected, func(a, b int) bool {
		if sel.Selected[a].Score != sel.Selected[b].Score {
			return sel.Selected[a].Score > sel.Selected[b].Score
		}
		return sel.Selected[a].Name < sel.Selected[b].Name
	})
	sel.EstTokensUsed = used

	dropped := make([]packDrop, 0, packDroppedCap)
	for _, i := range order {
		if picked[i] {
			continue
		}
		reason := "outcompeted"
		if cands[i].EstTokens > budget-used {
			reason = "budget-exhausted"
		}
		dropped = append(dropped, packDrop{ID: cands[i].ID, Reason: reason})
	}
	sel.Dropped = dropped[:min(len(dropped), packDroppedCap)]
	return sel
}

// packQueryWordSet is the deduplicated query content-word set.
func packQueryWordSet(query string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range packTokenizeWords(query) {
		out[w] = struct{}{}
	}
	return out
}
