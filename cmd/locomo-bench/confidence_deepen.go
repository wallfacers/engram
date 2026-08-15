package main

// 043 confidence-gated gap-guided deepening — pure function layer.
//
// This file holds the harness-only, offline-testable parts of the deepening
// protocol: the structured gap schema (<DEEPEN_META>), the deterministic
// gap→query mapping, the append-only evidence union, the dual-signal AUC gate
// machinery, and the textual hesitation lexicon. Nothing here calls a model or
// touches the engine; every function is deterministic for the same input. The
// engine (memory/ embedding/ provider/ store/ internal/) is untouched.

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// Closed outcome kinds (data-model.md). Exactly one terminal outcome per
// question; named outcome_kind (analyze F4) to avoid the "non-failure inside a
// failure enum" ambiguity of an earlier failure_kind name.
type deepenOutcomeKind string

const (
	deepenOutcomeNone                   deepenOutcomeKind = "none"
	deepenOutcomeSignalUnavailable      deepenOutcomeKind = "signal_unavailable"
	deepenOutcomeGapParseFailed         deepenOutcomeKind = "gap_parse_failed"
	deepenOutcomeQueryEmptyFallback     deepenOutcomeKind = "query_empty_fallback_question"
	deepenOutcomeSearchError            deepenOutcomeKind = "search_error"
	deepenOutcomeSearchEmpty            deepenOutcomeKind = "search_empty"
)

func (o deepenOutcomeKind) valid() bool {
	switch o {
	case deepenOutcomeNone, deepenOutcomeSignalUnavailable, deepenOutcomeGapParseFailed,
		deepenOutcomeQueryEmptyFallback, deepenOutcomeSearchError, deepenOutcomeSearchEmpty:
		return true
	}
	return false
}

// Closed gap categories (S2G-RAG universal schema, domain-agnostic slots).
type deepenGapCategory string

const (
	deepenGapCategoryBridgeEntity deepenGapCategory = "bridge_entity"
	deepenGapCategoryAttribute    deepenGapCategory = "attribute"
	deepenGapCategoryRelation     deepenGapCategory = "relation"
	deepenGapCategoryEvidenceSpan deepenGapCategory = "evidence_span"
	deepenGapCategoryOther        deepenGapCategory = "other"
)

func (c deepenGapCategory) valid() bool {
	switch c {
	case deepenGapCategoryBridgeEntity, deepenGapCategoryAttribute, deepenGapCategoryRelation,
		deepenGapCategoryEvidenceSpan, deepenGapCategoryOther:
		return true
	}
	return false
}

// Frozen schema limits (contracts/cli-flags.md, data-model.md).
const (
	deepenMaxGapItems = 3
	deepenMaxTarget   = 120
	deepenMaxSlot     = 80
	deepenMaxDesc     = 240
)

// deepenGapItem is one structured gap. target/slot/description are optional;
// the deterministic query mapping (gapQueryFor) resolves precedence.
type deepenGapItem struct {
	Category    string `json:"category"`
	Target      string `json:"target,omitempty"`
	Slot        string `json:"slot,omitempty"`
	Description string `json:"description,omitempty"`
}

func (g deepenGapItem) validate() error {
	if !deepenGapCategory(g.Category).valid() {
		return fmt.Errorf("gap category %q not in {bridge_entity,attribute,relation,evidence_span,other}", g.Category)
	}
	if len(g.Target) > deepenMaxTarget {
		return fmt.Errorf("gap target exceeds %d chars", deepenMaxTarget)
	}
	if len(g.Slot) > deepenMaxSlot {
		return fmt.Errorf("gap slot exceeds %d chars", deepenMaxSlot)
	}
	if len(g.Description) > deepenMaxDesc {
		return fmt.Errorf("gap description exceeds %d chars", deepenMaxDesc)
	}
	return nil
}

// validateDeepenGaps enforces the per-question cap and validates every item.
func validateDeepenGaps(gaps []deepenGapItem) error {
	if len(gaps) > deepenMaxGapItems {
		return fmt.Errorf("gap block exceeds %d items, got %d", deepenMaxGapItems, len(gaps))
	}
	for i := range gaps {
		if err := gaps[i].validate(); err != nil {
			return fmt.Errorf("gap[%d]: %w", i, err)
		}
	}
	return nil
}

// deepenMetaOpen/Close delimit the structured gap block appended after the
// final answer in the mechanism arm. The block is an output-format contract,
// never part of the frozen unified answer prompt (digest 1d8a8d0f unchanged).
const (
	deepenMetaOpen  = "<DEEPEN_META>"
	deepenMetaClose = "</DEEPEN_META>"
)

// deepenMetaBlock is the JSON payload inside <DEEPEN_META>.
type deepenMetaBlock struct {
	Gaps []deepenGapItem `json:"gaps"`
}

// parseDeepenMeta extracts and validates the gap block from an answerer's raw
// output. Returns (gaps, found, err): found=false means no block (treated as
// high-confidence, no deepening); err means the block existed but was malformed
// (failure_kind gap_parse_failed) or failed schema validation.
func parseDeepenMeta(output string) ([]deepenGapItem, bool, error) {
	open := strings.Index(output, deepenMetaOpen)
	if open < 0 {
		return nil, false, nil
	}
	rest := output[open+len(deepenMetaOpen):]
	closeIdx := strings.Index(rest, deepenMetaClose)
	if closeIdx < 0 {
		return nil, true, fmt.Errorf("deepen meta block missing closing %q", deepenMetaClose)
	}
	raw := strings.TrimSpace(rest[:closeIdx])
	var block deepenMetaBlock
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		return nil, true, fmt.Errorf("deepen meta block malformed JSON: %w", err)
	}
	if err := validateDeepenGaps(block.Gaps); err != nil {
		return nil, true, err
	}
	return block.Gaps, true, nil
}

// gapQueryFor deterministically maps the first gap to a refetch query
// (contracts/cli-flags.md "确定性映射契约"). Only gaps[0] is consulted — a
// blank first gap falls straight back to the original question (subsequent
// gaps do not "rescue" it):
//
//	query(gaps[0]) =
//	  nonempty(target) && nonempty(slot) ? target + " " + slot
//	: nonempty(description)              ? description
//	: nonempty(target)                   ? target
//	: 原问题
//
// The mapping is pure string concatenation, so the same input always yields
// the same query (locked by table-driven tests).
func gapQueryFor(gaps []deepenGapItem, question string) string {
	if len(gaps) == 0 {
		return strings.TrimSpace(question)
	}
	g := gaps[0]
	switch {
	case g.Target != "" && g.Slot != "":
		return strings.TrimSpace(g.Target + " " + g.Slot)
	case g.Description != "":
		return strings.TrimSpace(g.Description)
	case g.Target != "":
		return strings.TrimSpace(g.Target)
	default:
		return strings.TrimSpace(question)
	}
}

// appendDedup appends the supplemental hits onto round0 in round0 order,
// skipping any result whose ID is already present (compare by ID). It never
// reorders, truncates, or splits the round-0 quota (021's gapBudget failure is
// forbidden by plan FR-007). Returns the union and the number of added items.
func appendDedup(round0 []memory.Result, extra []memory.Result) ([]memory.Result, int) {
	seen := make(map[string]struct{}, len(round0)+len(extra))
	for i := range round0 {
		seen[round0[i].ID] = struct{}{}
	}
	union := make([]memory.Result, len(round0))
	copy(union, round0)
	added := 0
	for i := range extra {
		if _, dup := seen[extra[i].ID]; dup {
			continue
		}
		seen[extra[i].ID] = struct{}{}
		union = append(union, extra[i])
		added++
	}
	return union, added
}

// --- AUC / threshold (pilot gate, plan decision 5) ---

// deepenAUC computes the rank-based (Mann–Whitney U / WMW) AUC of positive
// scores vs negative scores. AUC = P(pos_score > neg_score) + 0.5*P(equal).
// Ties receive the average of the ranks they occupy (standard WMW tie-mean),
// so a perfectly symmetric positive/negative sample gives AUC = 0.5.
// Deterministic for the same input.
func deepenAUC(posScores, negScores []float64) (float64, error) {
	type scored struct {
		score float64
		pos   bool
	}
	var all []scored
	for _, s := range posScores {
		all = append(all, scored{score: s, pos: true})
	}
	for _, s := range negScores {
		all = append(all, scored{score: s, pos: false})
	}
	if len(all) < 2 {
		return 0, fmt.Errorf("AUC requires at least two scored units, got %d", len(all))
	}
	nPos, nNeg := len(posScores), len(negScores)
	if nPos == 0 || nNeg == 0 {
		return 0, fmt.Errorf("AUC needs both classes, got pos=%d neg=%d", nPos, nNeg)
	}
	// Sort ascending by score; ties are grouped by the rank-mean pass below, so
	// the secondary order does not matter for the AUC value.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].score < all[j].score
	})
	// Assign average ranks to tied groups (1-based positions).
	ranks := make([]float64, len(all))
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].score == all[i].score {
			j++
		}
		avgRank := (float64(i) + 1 + float64(j)) / 2
		for k := i; k < j; k++ {
			ranks[k] = avgRank
		}
		i = j
	}
	rankSum := 0.0
	for i := range all {
		if all[i].pos {
			rankSum += ranks[i]
		}
	}
	// Standard WMW: AUC = (R_pos - nPos*(nPos+1)/2) / (nPos*nNeg).
	return (rankSum - float64(nPos*(nPos+1))/2) / float64(nPos*nNeg), nil
}

// deepenAUCBootstrap computes the 95% CI of deepenAUC by resampling each class
// with replacement, using a fixed rand seed so the interval is reproducible.
// Samples fewer than the pooled size when either class is tiny.
func deepenAUCBootstrap(posScores, negScores []float64, seed int64, iterations int) (lo, hi float64, err error) {
	if iterations <= 0 {
		iterations = 1000
	}
	rng := rand.New(rand.NewSource(seed))
	aucs := make([]float64, 0, iterations)
	for i := 0; i < iterations; i++ {
		pos := make([]float64, len(posScores))
		neg := make([]float64, len(negScores))
		for j := range pos {
			pos[j] = posScores[rng.Intn(len(posScores))]
		}
		for j := range neg {
			neg[j] = negScores[rng.Intn(len(negScores))]
		}
		a, err := deepenAUC(pos, neg)
		if err != nil {
			continue
		}
		aucs = append(aucs, a)
	}
	if len(aucs) < 2 {
		return 0, 0, fmt.Errorf("bootstrap produced too few AUC samples (%d)", len(aucs))
	}
	sort.Float64s(aucs)
	lo = aucs[int(0.025*float64(len(aucs)))]
	hi = aucs[int(0.975*float64(len(aucs)))]
	return lo, hi, nil
}

// deepenROCThreshold returns the ROC-optimal decision threshold by Youden's J
// (maximizing sensitivity + specificity - 1) over the unique score values. The
// gate chooses the threshold at the pilot's ROC optimum (plan decision 2). Ties
// are resolved deterministically (lowest threshold among the max-J candidates).
func deepenROCThreshold(posScores, negScores []float64) (float64, error) {
	cutoffs := make(map[float64]struct{})
	for _, s := range posScores {
		cutoffs[s] = struct{}{}
	}
	for _, s := range negScores {
		cutoffs[s] = struct{}{}
	}
	if len(cutoffs) == 0 {
		return 0, fmt.Errorf("ROC threshold needs at least one scored unit")
	}
	vals := make([]float64, 0, len(cutoffs))
	for v := range cutoffs {
		vals = append(vals, v)
	}
	sort.Float64s(vals)
	best := 0.0
	bestJ := math.Inf(-1)
	for _, c := range vals {
		tp := 0
		fp := 0
		for _, s := range posScores {
			if s >= c {
				tp++
			}
		}
		for _, s := range negScores {
			if s >= c {
				fp++
			}
		}
		sens := 0.0
		if len(posScores) > 0 {
			sens = float64(tp) / float64(len(posScores))
		}
		spec := 0.0
		if len(negScores) > 0 {
			spec = float64(len(negScores)-fp) / float64(len(negScores))
		}
		j := sens + spec - 1
		if j > bestJ {
			bestJ = j
			best = c
		}
	}
	return best, nil
}

// --- Robust final-span logprob signal (043 fix for the 042 suffix failure) ---

// deepenSpecialTokens are vllm generation-end special tokens. They appear in
// the logprob token trace but never in message content, which is why the 042
// strict "content is an exact suffix of the reconstructed bytes" precondition
// fails structurally on this stack (probe 2026-08-15: 304/304
// content_not_generated_suffix with a trailing <|im_end|> token).
var deepenSpecialTokens = map[string]bool{
	"<|im_end|>":     true,
	"<|endoftext|>":  true,
	"<|im_start|>":   true,
}

// deepenFinalSpanSignal maps the three frozen features
// (utilityRoutingFeatureNames) from a token trace whose content carries INLINE
// thinking (vllm without a reasoning parser) and trailing special tokens. The
// final answer span is everything after the LAST thinking closing delimiter in
// the visible (special-stripped) reconstruction — the same region
// extractFinalAnswer returns — and the aggregation formulas replicate the 042
// frozen mapper exactly (mean logprob, ceil-index p10 logprob, mean
// top1−top2 margin over final-span tokens).
func deepenFinalSpanSignal(tokens []utilityLogprobToken) (features []float64, available bool, reason string) {
	unavail := func(r string) ([]float64, bool, string) { return nil, false, r }
	var visIdx []int
	var starts []int
	var recon []byte
	for i := range tokens {
		if deepenSpecialTokens[tokens[i].Token] {
			continue
		}
		visIdx = append(visIdx, i)
		starts = append(starts, len(recon))
		recon = append(recon, tokens[i].Bytes...)
	}
	visible := string(recon)
	finalStart := 0
	for _, d := range thinkingCloseDelims {
		if idx := strings.LastIndex(visible, d); idx >= 0 && idx+len(d) > finalStart {
			finalStart = idx + len(d)
		}
	}
	// First visible token starting at/after finalStart (a delimiter straddling
	// a token boundary falls back to the next whole token).
	lo, hi := 0, len(visIdx)
	for lo < hi {
		mid := (lo + hi) / 2
		if starts[mid] < finalStart {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(visIdx) {
		return unavail("empty_final_span")
	}
	var sampled []float64
	marginSum := 0.0
	for v := lo; v < len(visIdx); v++ {
		tok := tokens[visIdx[v]]
		if math.IsNaN(tok.Logprob) || math.IsInf(tok.Logprob, 0) ||
			math.IsNaN(tok.Top1) || math.IsInf(tok.Top1, 0) ||
			math.IsNaN(tok.Top2) || math.IsInf(tok.Top2, 0) {
			return unavail("non_finite")
		}
		if tok.Top2 == 0.0 {
			return unavail("missing_top2")
		}
		sampled = append(sampled, tok.Logprob)
		marginSum += tok.Top1 - tok.Top2
	}
	if len(sampled) == 0 {
		return unavail("empty_final_span")
	}
	mean := 0.0
	for _, v := range sampled {
		mean += v
	}
	mean /= float64(len(sampled))
	sorted := append([]float64(nil), sampled...)
	sort.Float64s(sorted)
	p10Idx := int(math.Ceil(0.10*float64(len(sorted)))) - 1
	if p10Idx < 0 {
		p10Idx = 0
	}
	feats := []float64{mean, sorted[p10Idx], marginSum / float64(len(sampled))}
	for _, f := range feats {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return unavail("non_finite")
		}
	}
	return feats, true, ""
}

// --- Textual hesitation lexicon (runner.go isIDK companion, T007) ---

// deepenHesitationLexicon lists canonical hesitation phrasings the answerer
// uses when evidence is insufficient (040 diagnostic: ~93% of information-
// deficient answers express hesitation). Matching is case-insensitive
// substring, exactly like isIDK. A hit marks the textual signal available.
var deepenHesitationLexicon = []string{
	"i don't know",
	"i do not know",
	"not mentioned",
	"no information",
	"not enough information",
	"insufficient information",
	"i'm not sure",
	"i am not sure",
	"cannot determine",
	"can't determine",
	"unable to determine",
	"no way to know",
	"not stated",
	"not provided",
}

// textualHesitation reports whether a predicted answer expresses uncertainty
// per the frozen lexicon. Empty answers count as hesitant (an empty final
// answer carries no evidence-backed content).
func textualHesitation(predicted string) bool {
	p := strings.ToLower(strings.TrimSpace(predicted))
	if p == "" {
		return true
	}
	for _, phrase := range deepenHesitationLexicon {
		if strings.Contains(p, phrase) {
			return true
		}
	}
	return false
}

// --- HesitationSignal / DeepenDecision (data-model.md entities) ---

// deepenSignalKind is the closed signal-form enum.
type deepenSignalKind string

const (
	deepenSignalLogprob deepenSignalKind = "logprob"
	deepenSignalTextual deepenSignalKind = "textual"
)

func (k deepenSignalKind) valid() bool {
	return k == deepenSignalLogprob || k == deepenSignalTextual
}

// deepenHesitationSignal is one observed hesitation signal (data-model.md).
type deepenHesitationSignal struct {
	Kind         deepenSignalKind `json:"kind"`
	Available    bool             `json:"available"`
	Value        float64          `json:"value"`
	FeatureName  string           `json:"feature_name,omitempty"`
	ClosedReason string           `json:"closed_reason,omitempty"`
}

// deepenDecision is the per-question audit record (data-model.md).
type deepenDecision struct {
	DecisionID           string                 `json:"decision_id"`
	Triggered            bool                   `json:"triggered"`
	Signal               *deepenHesitationSignal `json:"signal,omitempty"`
	Threshold            float64                `json:"threshold"`
	GapItems             []deepenGapItem        `json:"gap_items,omitempty"`
	GapQuery             string                 `json:"gap_query,omitempty"`
	AddedCount           int                    `json:"added_count"`
	Round0AnswerDigest   string                 `json:"round0_answer_digest"`
	DeepenedAnswerDigest string                 `json:"deepened_answer_digest,omitempty"`
	FinalFromDeepen      bool                   `json:"final_from_deepen"`
	Round0ContextDigest  string                 `json:"round0_context_digest"`
	OutcomeKind          deepenOutcomeKind      `json:"outcome_kind"`
}

// validate enforces the one-terminal-outcome invariant and field sanity.
func (d *deepenDecision) validate() error {
	if d == nil {
		return fmt.Errorf("nil deepen decision")
	}
	if d.DecisionID == "" {
		return fmt.Errorf("deepen decision missing decision_id")
	}
	if !d.OutcomeKind.valid() {
		return fmt.Errorf("deepen decision %s has invalid outcome_kind %q", d.DecisionID, d.OutcomeKind)
	}
	if d.Signal != nil && !d.Signal.Kind.valid() {
		return fmt.Errorf("deepen decision %s has invalid signal kind %q", d.DecisionID, d.Signal.Kind)
	}
	return nil
}
