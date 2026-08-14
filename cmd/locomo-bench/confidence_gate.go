package main

import "strings"

// Confidence-gated iterative retrieval (specs/041).
//
// The retrieval-depth decision is driven by the answerer's own generation
// (thinking + final answer), not by retrieval-score knee points (040 proved
// those only reach r=20% < the 45% gate needed to hold 90pp). 040's post-hoc
// analysis found 89% of the 30→150 incremental misses are "hesitant" (only 7%
// confidently wrong), which makes hesitation a usable trigger for "read more".
// All signals here are deterministic text rules (research Decision 1): no
// model calls, no logits — repeatable, unit-testable, and lifted from the
// answerer's normal generation (FR-002/FR-003). Engine untouched.

// hesitancy is the deterministic hesitation level of one generation.
// 0 = confident, higher = more hesitant. Derived from a weighted score.
type hesitancy int

const (
	hesitConfident hesitancy = iota // 0
	hesitWeak                       // 1
	hesitStrong                     // 2
)

// signalHit records one matched hesitation signal, for audit and threshold
// tuning (data-model entity 1).
type signalHit struct {
	Signal  string `json:"signal"`
	Weight  int    `json:"weight"`
	Snippet string `json:"snippet,omitempty"`
}

// HesitationSignal is the per-generation hesitation verdict (data-model 1).
// Score is the weighted sum; Decision is the level banded from Score; Hits are
// the matched signals for audit. Serialized into conf_gate_decisions.jsonl.
type HesitationSignal struct {
	Decision hesitancy   `json:"decision"`
	Score    float64     `json:"score"`
	Hits     []signalHit `json:"hits,omitempty"`
}

// budgetLadder defines the "how much to read" tiers (data-model 2).
type budgetLadder struct {
	ShallowTopK int
	DeepTopK    int
	ChunkQuota  int
}

// iterationDecisionRecord is the per-question iteration audit (data-model 3),
// written to run-dir/conf_gate_decisions.jsonl.
type iterationDecisionRecord struct {
	QuestionID     string           `json:"question_id"`
	Question       string           `json:"question"`
	ShallowHits    int              `json:"shallow_hits"`
	ShallowAnswer  string           `json:"shallow_answer"`
	ShallowSignal  HesitationSignal `json:"shallow_signal"`
	DeepHits       int              `json:"deep_hits,omitempty"`
	DeepAnswer     string           `json:"deep_answer,omitempty"`
	DeepSignal     *HesitationSignal `json:"deep_signal,omitempty"`
	FinalAnswer    string           `json:"final_answer"`
	FinalFromRound int              `json:"final_from_round"`
	Deepened       bool             `json:"deepened"`
}

// confidenceGateConfig is the runtime calibration of the hesitation→deepen
// decision (data-model 4). Threshold default 3.0 (research Decision 1 banding:
// a refusal or explicit uncertainty, or a guess+hedge combination, deepens).
type confidenceGateConfig struct {
	Threshold float64
	MaxRounds int
}

// signal phrase sets, one weight per category (any single phrase in a category
// adds that weight once — a list of hedges is not stronger than one).
var (
	strongHesitationPhrases = []string{"not sure", "uncertain", "not confident", "unsure", "not certain"}
	midGuessPhrases         = []string{"could be", "might be", "may be", "possibly", "maybe", "perhaps"}
	weakHedgePhrases        = []string{"i think", "i guess", "i believe", "probably", "likely", "approximately"}
)

// thinkingCloseDelimsForDetect are the closing markers of a reasoning preamble;
// the thinking segment is everything before the LAST such marker. Reuses the
// same marker set the judge uses to strip thinking (runner.go thinkingCloseDelims).
var thinkingCloseDelimsForDetect = append([]string(nil), thinkingCloseDelims...)

// thinkingPart returns the reasoning preamble (everything before the last
// thinking close delimiter), or "" when the completion has no thinking block.
func thinkingPart(pred string) string {
	best := -1
	for _, d := range thinkingCloseDelimsForDetect {
		if i := strings.LastIndex(pred, d); i > best {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return pred[:best]
}

// snippet bounds a matched phrase to ~80 chars for the audit record.
func snippet(s string) string {
	const max = 80
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// snippetAround bounds the context around a match (for thinking-segment hits).
func snippetAround(s string, idx, width int) string {
	if idx < 0 {
		return ""
	}
	lo := idx - 20
	if lo < 0 {
		lo = 0
	}
	hi := idx + width + 20
	if hi > len(s) {
		hi = len(s)
	}
	return snippet(s[lo:hi])
}

// detectHesitation deterministically scores one answerer generation (FR-002).
// pred is the raw generation including any thinking preamble; final is the
// judge-facing answer (thinking stripped). Returns the full signal (for audit
// and threshold tuning) plus whether the score meets the deepen threshold.
//
// FR-005 note: this is a pure text function — if the answerer emits no thinking
// structure it still runs on the final text only (isIDK / hedges / empty), and
// the caller decides the fallback policy. It never errors.
func detectHesitation(pred string, threshold float64) (HesitationSignal, bool) {
	lower := strings.ToLower(pred)
	sig := HesitationSignal{}
	addOnce := func(category string, weight int, phrases []string) {
		for _, phrase := range phrases {
			if i := strings.Index(lower, phrase); i >= 0 {
				sig.Score += float64(weight)
				sig.Hits = append(sig.Hits, signalHit{Signal: category, Weight: weight, Snippet: snippetAround(pred, i, len(phrase))})
				break // one hit per category
			}
		}
	}
	final := extractFinalAnswer(pred)

	// Strong (+3): explicit refusal. isIDK already covers empty and "don't
	// know"/"no information"/"not mentioned".
	if isIDK(pred) {
		sig.Score += 3
		sig.Hits = append(sig.Hits, signalHit{Signal: "idk_refusal", Weight: 3, Snippet: snippet(final)})
	}
	// Strong (+3): explicit uncertainty / undecided multi-candidate thinking.
	addOnce("strong_uncertainty", 3, strongHesitationPhrases)
	if strings.Contains(lower, "either ") || strings.Contains(lower, "not sure which") {
		if !containsHit(sig, "strong_uncertainty") {
			sig.Score += 3
		}
		sig.Hits = append(sig.Hits, signalHit{Signal: "multi_candidate", Weight: 3, Snippet: snippetAround(pred, strings.Index(lower, "either "), 7)})
	}
	// Mid (+2): guessing modality.
	addOnce("mid_guess", 2, midGuessPhrases)
	// Weak (+1): low-confidence hedging.
	addOnce("weak_hedge", 1, weakHedgePhrases)
	// Weak (+1): empty final (also counts as idk_refusal above) or a question
	// mark ending — a "what is it?" style restatement rather than an answer.
	if strings.TrimSpace(final) == "" {
		sig.Score += 1
		sig.Hits = append(sig.Hits, signalHit{Signal: "empty_final", Weight: 1})
	} else if strings.HasSuffix(strings.TrimSpace(final), "?") {
		sig.Score += 1
		sig.Hits = append(sig.Hits, signalHit{Signal: "question_mark", Weight: 1})
	}

	sig.Decision = bandHesitancy(sig.Score)
	return sig, sig.Score >= threshold
}

// bandHesitancy maps a weighted score to a decision level. 3.0 is the default
// deepen threshold, so the bands sit on the same grid: <3 confident, 3-5 weak,
// >=6 strong (research Decision 1).
func bandHesitancy(score float64) hesitancy {
	switch {
	case score >= 6:
		return hesitStrong
	case score >= 3:
		return hesitWeak
	default:
		return hesitConfident
	}
}

func containsHit(sig HesitationSignal, name string) bool {
	for _, h := range sig.Hits {
		if h.Signal == name {
			return true
		}
	}
	return false
}
