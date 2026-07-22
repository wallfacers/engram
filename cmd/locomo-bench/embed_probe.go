package main

import (
	"context"
	"fmt"
	"math"

	"github.com/wallfacers/engram/embedding"
)

const (
	defaultEmbedBoundedL2 = 1e-6

	embedVerdictDeterministic = "deterministic"
	embedVerdictBounded       = "bounded"
	embedVerdictUnstable      = "unstable"
)

// EmbeddingDeterminismProbe summarizes two embedding passes over identical queries.
type EmbeddingDeterminismProbe struct {
	NQueries          int     `json:"n_queries"`
	BitIdenticalRatio float64 `json:"bit_identical_ratio"`
	MaxL2Delta        float64 `json:"max_l2_delta"`
	MeanL2Delta       float64 `json:"mean_l2_delta"`
	Verdict           string  `json:"verdict"`
}

func probeEmbeddings(ctx context.Context, client embedding.Client, queries []string, boundedL2 float64) (EmbeddingDeterminismProbe, error) {
	if client == nil {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe requires a configured client")
	}
	if len(queries) == 0 {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe requires at least one query")
	}
	if boundedL2 < 0 {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe bounded L2 must be non-negative")
	}

	first, err := client.Embed(ctx, queries)
	if err != nil {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe first pass: %w", err)
	}
	second, err := client.Embed(ctx, queries)
	if err != nil {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe second pass: %w", err)
	}
	if len(first) != len(queries) || len(second) != len(queries) {
		return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe expected %d vectors per pass, got %d and %d", len(queries), len(first), len(second))
	}

	identical := 0
	totalL2 := 0.0
	maxL2 := 0.0
	for index := range queries {
		if len(first[index]) == 0 || len(first[index]) != len(second[index]) {
			return EmbeddingDeterminismProbe{}, fmt.Errorf("embedding probe query %d dimension mismatch: %d and %d", index, len(first[index]), len(second[index]))
		}
		bitIdentical := true
		squaredDelta := 0.0
		for dimension := range first[index] {
			a, b := first[index][dimension], second[index][dimension]
			if math.Float32bits(a) != math.Float32bits(b) {
				bitIdentical = false
			}
			delta := float64(a) - float64(b)
			squaredDelta += delta * delta
		}
		if bitIdentical {
			identical++
		}
		l2 := math.Sqrt(squaredDelta)
		totalL2 += l2
		if l2 > maxL2 {
			maxL2 = l2
		}
	}

	report := EmbeddingDeterminismProbe{
		NQueries:          len(queries),
		BitIdenticalRatio: float64(identical) / float64(len(queries)),
		MaxL2Delta:        maxL2,
		MeanL2Delta:       totalL2 / float64(len(queries)),
		Verdict:           embedVerdictUnstable,
	}
	if identical == len(queries) {
		report.Verdict = embedVerdictDeterministic
	} else if maxL2 <= boundedL2 {
		report.Verdict = embedVerdictBounded
	}
	return report, nil
}
