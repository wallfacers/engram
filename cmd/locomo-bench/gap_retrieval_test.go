package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

func TestStructuredGapRefetchAcceptsOnlyValidatedExplicitGaps(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	tests := []struct {
		name      string
		gap       evidencecompiler.StructuredGap
		wantQuery string
	}{
		{
			name:      "entity",
			gap:       evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity, Entity: "Avery", SourceNeed: "identity"},
			wantQuery: `gap:entity entity="Avery" source_need="identity"`,
		},
		{
			name:      "time range",
			gap:       evidencecompiler.StructuredGap{Kind: evidencecompiler.GapTimeRange, Start: &start, End: &end, SourceNeed: "July event"},
			wantQuery: `gap:time_range start="2026-07-01T00:00:00Z" end="2026-07-31T23:59:59Z" source_need="July event"`,
		},
		{
			name:      "second operand",
			gap:       evidencecompiler.StructuredGap{Kind: evidencecompiler.GapSecondOperand, Operand: "Bob", SourceNeed: "comparison"},
			wantQuery: `gap:second_operand operand="Bob" source_need="comparison"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var query string
			retriever := gapCandidateRetrieverFunc(func(_ context.Context, gotQuery string, limit int) ([]evidencecompiler.Candidate, error) {
				calls++
				query = gotQuery
				if limit != 1 {
					t.Fatalf("supplemental limit = %d, want 1", limit)
				}
				return []evidencecompiler.Candidate{gapSupplementCandidate()}, nil
			})
			budget, err := newGapTreatmentBudget(3, 1, 512)
			if err != nil {
				t.Fatal(err)
			}
			trace := gapTrace{
				Valid:        true,
				Need:         evidencecompiler.EvidenceNeed{Gap: &test.gap},
				RemainingGap: &test.gap,
			}
			result, err := runOneStructuredGapRefetch(context.Background(), gapRefetchRequest{
				Trace:             trace,
				InitialCandidates: projectionTestCandidates(),
				Budget:            budget,
				Usage:             gapBudgetUsage{TokenCount: 120},
			}, retriever)
			if err != nil {
				t.Fatalf("run gap refetch: %v", err)
			}
			if calls != 1 {
				t.Fatalf("retrieval calls = %d, want 1", calls)
			}
			if query != test.wantQuery {
				t.Fatalf("gap query = %q, want %q", query, test.wantQuery)
			}
			if !result.Triggered || result.Usage.RetrievalCalls != 1 {
				t.Fatalf("result = %+v, want exactly one triggered refetch", result)
			}
			if got, want := candidateIDs(result.Candidates), []string{"candidate-1", "candidate-2", "candidate-gap"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("stable union = %v, want %v", got, want)
			}
			if got, want := result.Candidates[2].SourceIDs, []string{"source-gap"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("supplemental candidate lost lineage: %v", got)
			}
		})
	}
}

func TestStructuredGapRefetchRejectsLowConfidenceAndFreeText(t *testing.T) {
	gap := evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity, Entity: "Avery"}
	tests := []struct {
		name  string
		trace gapTrace
	}{
		{name: "unvalidated trace", trace: gapTrace{Need: evidencecompiler.EvidenceNeed{Gap: &gap}, RemainingGap: &gap}},
		{name: "low confidence", trace: gapTrace{Valid: true, LowConfidence: true, Need: evidencecompiler.EvidenceNeed{Gap: &gap}, RemainingGap: &gap}},
		{name: "free text has no structured gap", trace: gapTrace{Valid: true, FreeTextNeed: "please search more context"}},
		{name: "missing entity field", trace: gapTrace{Valid: true, Need: evidencecompiler.EvidenceNeed{Gap: &evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity}}, RemainingGap: &evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity}}},
	}

	budget, err := newGapTreatmentBudget(3, 1, 512)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			retriever := gapCandidateRetrieverFunc(func(_ context.Context, _ string, _ int) ([]evidencecompiler.Candidate, error) {
				calls++
				return nil, nil
			})
			result, err := runOneStructuredGapRefetch(context.Background(), gapRefetchRequest{
				Trace:             test.trace,
				InitialCandidates: projectionTestCandidates(),
				Budget:            budget,
			}, retriever)
			if err != nil {
				t.Fatalf("ineligible trace returned error: %v", err)
			}
			if calls != 0 || result.Triggered || result.Usage.RetrievalCalls != 0 {
				t.Fatalf("ineligible trace triggered refetch: calls=%d result=%+v", calls, result)
			}
			if got, want := candidateIDs(result.Candidates), []string{"candidate-1", "candidate-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("ineligible trace changed candidates: got %v want %v", got, want)
			}
		})
	}
}

func TestIneligibleStructuredGapDoesNotRequireRetriever(t *testing.T) {
	budget, err := newGapTreatmentBudget(3, 1, 512)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runOneStructuredGapRefetch(context.Background(), gapRefetchRequest{
		Trace:             gapTrace{Valid: false},
		InitialCandidates: projectionTestCandidates(),
		Budget:            budget,
	}, nil)
	if err != nil {
		t.Fatalf("ineligible trace with no retriever returned error: %v", err)
	}
	if result.Triggered || result.Usage.RetrievalCalls != 0 {
		t.Fatalf("ineligible trace unexpectedly triggered: %+v", result)
	}
}

func TestStructuredGapRefetchStopsAfterOneRound(t *testing.T) {
	gap := evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity, Entity: "Avery"}
	budget, err := newGapTreatmentBudget(3, 1, 512)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	retriever := gapCandidateRetrieverFunc(func(_ context.Context, _ string, _ int) ([]evidencecompiler.Candidate, error) {
		calls++
		return []evidencecompiler.Candidate{gapSupplementCandidate()}, nil
	})
	_, err = runOneStructuredGapRefetch(context.Background(), gapRefetchRequest{
		Trace:             gapTrace{Valid: true, Need: evidencecompiler.EvidenceNeed{Gap: &gap}, RemainingGap: &gap},
		InitialCandidates: projectionTestCandidates(),
		Budget:            budget,
		Usage:             gapBudgetUsage{RetrievalCalls: 1},
	}, retriever)
	if err == nil {
		t.Fatal("second structured refetch succeeded, want forced stop")
	}
	if calls != 0 {
		t.Fatalf("second structured refetch called retriever %d times", calls)
	}
}

func TestStructuredGapRefetchStableUnionRejectsOverflowAndDuplicateLoss(t *testing.T) {
	gap := evidencecompiler.StructuredGap{Kind: evidencecompiler.GapEntity, Entity: "Avery"}
	budget, err := newGapTreatmentBudget(3, 1, 512)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		supplemental []evidencecompiler.Candidate
		wantErr      bool
	}{
		{
			name: "deduplicates without dropping initial lineage",
			supplemental: []evidencecompiler.Candidate{
				projectionTestCandidates()[1],
				gapSupplementCandidate(),
			},
		},
		{
			name: "retriever exceeds supplemental allocation",
			supplemental: []evidencecompiler.Candidate{
				gapSupplementCandidate(),
				{ID: "candidate-overflow", Rank: 4, SourceIDs: []string{"source-overflow"}},
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			retriever := gapCandidateRetrieverFunc(func(_ context.Context, _ string, _ int) ([]evidencecompiler.Candidate, error) {
				return test.supplemental, nil
			})
			result, err := runOneStructuredGapRefetch(context.Background(), gapRefetchRequest{
				Trace:             gapTrace{Valid: true, Need: evidencecompiler.EvidenceNeed{Gap: &gap}, RemainingGap: &gap},
				InitialCandidates: projectionTestCandidates(),
				Budget:            budget,
			}, retriever)
			if test.wantErr {
				if err == nil {
					t.Fatal("overflow supplemental set was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("run gap refetch: %v", err)
			}
			if got, want := candidateIDs(result.Candidates), []string{"candidate-1", "candidate-2", "candidate-gap"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("union = %v, want %v", got, want)
			}
			if got, want := result.Candidates[0].SourceIDs, []string{"source-1", "source-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("dedupe lost original candidate lineage: %v", got)
			}
		})
	}
}

func gapSupplementCandidate() evidencecompiler.Candidate {
	return evidencecompiler.Candidate{
		ID:         "candidate-gap",
		Kind:       evidencecompiler.CandidateRawTurn,
		Rank:       3,
		Text:       "Avery is the comparison target.",
		TextDigest: evalTextDigest("Avery is the comparison target."),
		SourceIDs:  []string{"source-gap"},
	}
}

func candidateIDs(candidates []evidencecompiler.Candidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.ID
	}
	return ids
}
