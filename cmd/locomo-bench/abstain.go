package main

import (
	"context"
	"math"

	"github.com/wallfacers/engram/memory"
)

type ClaimMatch string

const (
	ClaimNoMatch       ClaimMatch = "no-match"
	ClaimEntityOnly    ClaimMatch = "entity-only"
	ClaimEntityAndSlot ClaimMatch = "entity+slot"
)

type ConfidenceSource string

const (
	ConfidenceSourceRerank ConfidenceSource = "rerank"
	ConfidenceSourceCosine ConfidenceSource = "cosine"
)

// AbstainSignal is the threshold-independent, offline evidence available for a
// single LoCoMo question. It contains no answer or judge model output.
type AbstainSignal struct {
	QuestionID         string           `json:"question_id"`
	Category           int              `json:"category"`
	Adversarial        bool             `json:"adversarial"`
	ClaimMatch         ClaimMatch       `json:"claim_match"`
	Confidence         float64          `json:"confidence"`
	ConfidenceTop1     float64          `json:"confidence_top1"`
	ConfidenceSource   ConfidenceSource `json:"confidence_source"`
	ClaimSignalPresent bool             `json:"claim_signal_present"`
}

type AbstainThresholdConfig struct {
	UseClaim            bool
	ClaimThreshold      float64
	UseConfidence       bool
	ConfidenceThreshold float64
}

type AbstainDecision struct {
	Abstain bool   `json:"abstain"`
	Rule    string `json:"rule"`
}

type abstainSignalInput struct {
	QuestionID        string
	Category          int
	DemandAtoms       []DemandAtom
	Candidates        []memory.Result
	Meta              *PCICMeta
	ChunkTurns        map[string][]string
	SpanKey           func(string) string
	Reranked          bool
	CosineByCandidate map[string]float64
}

// computeAbstainSignal derives the query entity demand through the existing
// PCIC path, then grades only the already-retrieved candidates.
func computeAbstainSignal(ctx context.Context, entries *memory.EntryStore, question string, input abstainSignalInput) (AbstainSignal, error) {
	signals, err := derivePCICSignals(ctx, entries, question, input.Candidates)
	if err != nil {
		return AbstainSignal{}, err
	}
	input.DemandAtoms = signals.DemandAtoms
	return computeAbstainSignalForDemand(input), nil
}

func computeAbstainSignalForDemand(input abstainSignalInput) AbstainSignal {
	confidence, top1, source := abstainConfidence(input.Candidates, input.Reranked, input.CosineByCandidate)
	signal := AbstainSignal{
		QuestionID:       input.QuestionID,
		Category:         input.Category,
		Adversarial:      input.Category == adversarialCategory,
		ClaimMatch:       ClaimNoMatch,
		Confidence:       confidence,
		ConfidenceTop1:   top1,
		ConfidenceSource: source,
	}
	if input.Meta == nil {
		return signal
	}

	signal.ClaimSignalPresent = true
	claimInput := PCICSelectionInput{
		Meta:       input.Meta,
		ChunkTurns: input.ChunkTurns,
		SpanKey:    input.SpanKey,
	}
	for _, candidate := range input.Candidates {
		for _, claim := range claimsForChunk(candidate, claimInput) {
			for _, demand := range input.DemandAtoms {
				if memory.EntityNorm(claim.Entity) == "" || memory.EntityNorm(claim.Entity) != memory.EntityNorm(demand.Entity) {
					continue
				}
				if demand.Slot != "" && memory.EntityNorm(claim.Slot) == memory.EntityNorm(demand.Slot) {
					return withClaimMatch(signal, ClaimEntityAndSlot)
				}
				signal.ClaimMatch = ClaimEntityOnly
			}
		}
	}
	return signal
}

func withClaimMatch(signal AbstainSignal, match ClaimMatch) AbstainSignal {
	signal.ClaimMatch = match
	return signal
}

// abstainConfidence returns two confidence variants over the retrieved
// candidates: the mean-blended (top+mean)/2 and the peak top-1-only score. The
// probe sweeps both — top-1 avoids the mean's dilution, which drags an
// answerable question's confidence down toward an adversarial one's when both
// carry many weak candidates.
func abstainConfidence(candidates []memory.Result, reranked bool, cosineByCandidate map[string]float64) (blended, top1 float64, source ConfidenceSource) {
	source = ConfidenceSourceCosine
	if reranked {
		source = ConfidenceSourceRerank
	}
	if len(candidates) == 0 {
		return 0, 0, source
	}

	var total float64
	top := 0.0
	for i, candidate := range candidates {
		var score float64
		if reranked {
			score = clamp01(candidate.Score)
		} else {
			score = clamp01((cosineByCandidate[candidate.Name] + 1) / 2)
		}
		if i == 0 || score > top {
			top = score
		}
		total += score
	}
	return clamp01((top + total/float64(len(candidates))) / 2), clamp01(top), source
}

func clamp01(value float64) float64 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value
}

// decideAbstention applies thresholds to already-computed evidence. Confidence
// is converted to an abstention score (1-confidence), so the inclusive >= rule
// has the same direction for every threshold used by the probe.
func decideAbstention(signal AbstainSignal, config AbstainThresholdConfig) AbstainDecision {
	claimFired := config.UseClaim && signal.ClaimSignalPresent && claimAbstentionScore(signal.ClaimMatch) >= config.ClaimThreshold
	confidenceFired := config.UseConfidence && 1-signal.Confidence >= config.ConfidenceThreshold
	switch {
	case claimFired && confidenceFired:
		return AbstainDecision{Abstain: true, Rule: "combined"}
	case claimFired:
		return AbstainDecision{Abstain: true, Rule: "claim=no-match"}
	case confidenceFired:
		return AbstainDecision{Abstain: true, Rule: "confidence<tau"}
	default:
		return AbstainDecision{}
	}
}

func claimAbstentionScore(match ClaimMatch) float64 {
	switch match {
	case ClaimNoMatch:
		return 1
	case ClaimEntityOnly:
		return 0.5
	default:
		return 0
	}
}
