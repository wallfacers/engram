package main

import (
	"context"
	"testing"
)

type stubProbeEmbeddingClient struct {
	responses [][][]float32
	calls     int
}

func (s *stubProbeEmbeddingClient) Embed(_ context.Context, _ []string) ([][]float32, error) {
	response := s.responses[s.calls]
	s.calls++
	return response, nil
}

func (*stubProbeEmbeddingClient) Model() string { return "stub" }

func TestEmbedProbeVerdicts(t *testing.T) {
	tests := []struct {
		name        string
		responses   [][][]float32
		wantRatio   float64
		wantMaxL2   float64
		wantMeanL2  float64
		wantVerdict string
	}{
		{
			name: "deterministic",
			responses: [][][]float32{
				{{1, 2}, {3, 4}},
				{{1, 2}, {3, 4}},
			},
			wantRatio: 1, wantMaxL2: 0, wantMeanL2: 0, wantVerdict: embedVerdictDeterministic,
		},
		{
			name: "bounded",
			responses: [][][]float32{
				{{0, 0}},
				{{0.0000003, 0.0000004}},
			},
			wantRatio: 0, wantMaxL2: 0.0000005, wantMeanL2: 0.0000005, wantVerdict: embedVerdictBounded,
		},
		{
			name: "unstable",
			responses: [][][]float32{
				{{0, 0}},
				{{0.003, 0.004}},
			},
			wantRatio: 0, wantMaxL2: 0.005, wantMeanL2: 0.005, wantVerdict: embedVerdictUnstable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubProbeEmbeddingClient{responses: tt.responses}
			report, err := probeEmbeddings(context.Background(), client, []string{"same query", "second query"}[:len(tt.responses[0])], defaultEmbedBoundedL2)
			if err != nil {
				t.Fatalf("probe embeddings: %v", err)
			}
			if client.calls != 2 {
				t.Fatalf("Embed calls = %d, want 2", client.calls)
			}
			if report.BitIdenticalRatio != tt.wantRatio {
				t.Fatalf("bit_identical_ratio = %g, want %g", report.BitIdenticalRatio, tt.wantRatio)
			}
			if !closeEnough(report.MaxL2Delta, tt.wantMaxL2, 1e-9) {
				t.Fatalf("max_l2_delta = %.12g, want %.12g", report.MaxL2Delta, tt.wantMaxL2)
			}
			if !closeEnough(report.MeanL2Delta, tt.wantMeanL2, 1e-9) {
				t.Fatalf("mean_l2_delta = %.12g, want %.12g", report.MeanL2Delta, tt.wantMeanL2)
			}
			if report.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q", report.Verdict, tt.wantVerdict)
			}
		})
	}
}

func closeEnough(got, want, tolerance float64) bool {
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	return delta <= tolerance
}
