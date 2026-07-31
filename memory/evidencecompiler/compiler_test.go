package evidencecompiler

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCompileUsesRawWhenItFitsAndNeverCallsPlannerCompression(t *testing.T) {
	content := "Alice met Bob in Beijing."
	resolver := &compilerTestResolver{records: []Evidence{{ID: "src-1", Content: content, ContentDigest: sha256Hex(content), State: "active"}}}
	planner := compilerTestPlanner{proposal: Proposal{Actions: []Action{{Kind: ActionExtract, CandidateID: "candidate-1", Span: &SourceSpan{SourceID: "src-1", StartChar: 0, EndChar: 5, SpanDigest: sha256Hex("Alice")}}}}}
	bundle, trace, err := Compile(context.Background(), compilerTestRequest(content, resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, planner, 200))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(bundle.Items) != 1 || bundle.Items[0].Kind != ActionKeep || bundle.Items[0].Text != content {
		t.Fatalf("Compile() bundle = %+v, want full raw KEEP", bundle)
	}
	if len(trace.AppliedActions) != 1 || trace.AppliedActions[0].Kind != ActionKeep {
		t.Fatalf("Compile() applied actions = %+v, want KEEP only", trace.AppliedActions)
	}
	if !trace.Valid || bundle.InputTokens > bundle.TokenCap {
		t.Fatalf("Compile() validity/cap = %v/%d>%d", trace.Valid, bundle.InputTokens, bundle.TokenCap)
	}
}

func TestCompileFallsBackFromInvalidPlannerToDeterministicExtractive(t *testing.T) {
	content := "Background text that does not answer the question. Alice met Bob in Beijing. More background text."
	resolver := &compilerTestResolver{records: []Evidence{{ID: "src-1", Content: content, ContentDigest: sha256Hex(content), State: "active"}}}
	invalidPlanner := compilerTestPlanner{proposal: Proposal{Actions: []Action{{Kind: ActionKind("ADD"), SourceID: "src-1"}}}}
	bundle, trace, err := Compile(context.Background(), compilerTestRequest(content, resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, invalidPlanner, 170))
	if err != nil {
		t.Fatalf("Compile() fallback error = %v", err)
	}
	if trace.FallbackReason == "" {
		t.Fatal("Compile() did not record invalid planner fallback")
	}
	if len(bundle.Items) == 0 || bundle.Items[0].Kind != ActionExtract || bundle.Items[0].Text != "Alice met Bob in Beijing." {
		t.Fatalf("Compile() fallback bundle = %+v, want grounded EXTRACT", bundle)
	}
}

func TestCompileFailsClosedForCounterAndStaticBudgetFailures(t *testing.T) {
	content := "Alice met Bob."
	resolver := &compilerTestResolver{records: []Evidence{{ID: "src-1", Content: content, ContentDigest: sha256Hex(content), State: "active"}}}
	for name, counter := range map[string]TokenCounter{
		"counter-error":     compilerErrorCounter{},
		"fingerprint-drift": compilerLengthCounter{fingerprint: "drifted"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Compile(context.Background(), compilerTestRequest(content, resolver, counter, nil, 100))
			if name == "counter-error" && !errors.Is(err, ErrCounterUnavailable) {
				t.Fatalf("Compile() error = %v, want ErrCounterUnavailable", err)
			}
			if name == "fingerprint-drift" && !errors.Is(err, ErrFingerprintMismatch) {
				t.Fatalf("Compile() error = %v, want ErrFingerprintMismatch", err)
			}
		})
	}

	bundle, trace, err := Compile(context.Background(), compilerTestRequest(content, resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, nil, 1))
	if !errors.Is(err, ErrBudgetImpossible) {
		t.Fatalf("Compile(static prompt over cap) error = %v, want ErrBudgetImpossible", err)
	}
	if len(bundle.Items) != 0 || !trace.Valid {
		t.Fatalf("Compile(static prompt over cap) result = %+v / %+v, want empty bundle and valid trace", bundle, trace)
	}
}

func TestCompilePropagatesCallerCancellationBeforeResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := &compilerTestResolver{}
	_, _, err := Compile(ctx, compilerTestRequest("Alice met Bob.", resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, nil, 100))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile(canceled) error = %v, want context.Canceled", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("Compile(canceled) resolver calls = %d, want 0", resolver.calls)
	}
}

func TestCompileRejectsCandidateAndSourceBoundsBeforeResolution(t *testing.T) {
	content := "Alice met Bob."
	candidate := Candidate{
		ID:         "candidate-1",
		Kind:       CandidateRawTurn,
		Rank:       1,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}

	t.Run("candidate limit", func(t *testing.T) {
		resolver := &compilerTestResolver{}
		req := compilerTestRequest(content, resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, nil, 100)
		second := candidate
		second.ID = "candidate-2"
		second.Rank = 2
		second.SourceIDs = []string{"src-2"}
		req.Candidates = []Candidate{candidate, second}
		req.MaxCandidates = 1

		_, _, err := Compile(context.Background(), req)
		if !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("Compile(candidate overflow) error = %v, want ErrInvalidCandidate", err)
		}
		if resolver.calls != 0 {
			t.Fatalf("Compile(candidate overflow) resolver calls = %d, want 0", resolver.calls)
		}
	})

	t.Run("source limit", func(t *testing.T) {
		resolver := &compilerTestResolver{}
		req := compilerTestRequest(content, resolver, compilerLengthCounter{fingerprint: "tokenizer-v1"}, nil, 100)
		candidate.SourceIDs = []string{"src-1", "src-2"}
		req.Candidates = []Candidate{candidate}
		req.MaxSources = 1

		_, _, err := Compile(context.Background(), req)
		if !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("Compile(source overflow) error = %v, want ErrInvalidCandidate", err)
		}
		if resolver.calls != 0 {
			t.Fatalf("Compile(source overflow) resolver calls = %d, want 0", resolver.calls)
		}
	})
}

func compilerTestRequest(content string, resolver SourceResolver, counter TokenCounter, planner Planner, cap int) CompileRequest {
	return CompileRequest{
		Query: "What did Alice do?",
		Candidates: []Candidate{{
			ID:         "candidate-1",
			Kind:       CandidateRawTurn,
			Rank:       1,
			Text:       content,
			TextDigest: sha256Hex(content),
			SourceIDs:  []string{"src-1"},
		}},
		TokenCap:           cap,
		CounterFingerprint: "tokenizer-v1",
		MaxCandidates:      4,
		MaxSources:         4,
		Planner:            planner,
		Resolver:           resolver,
		Counter:            counter,
		Renderer:           compilerTestRenderer{},
	}
}

type compilerTestResolver struct {
	records []Evidence
	calls   int
}

func (resolver *compilerTestResolver) Resolve(_ context.Context, _ []string) ([]Evidence, error) {
	resolver.calls++
	return append([]Evidence(nil), resolver.records...), nil
}

type compilerLengthCounter struct{ fingerprint string }

func (counter compilerLengthCounter) CountInput(_ context.Context, input AnswerInput) (TokenCount, error) {
	return TokenCount{InputTokens: len([]rune(input.System + input.User)), Fingerprint: counter.fingerprint}, nil
}

type compilerErrorCounter struct{}

func (compilerErrorCounter) CountInput(context.Context, AnswerInput) (TokenCount, error) {
	return TokenCount{}, fmt.Errorf("counter offline")
}

type compilerTestRenderer struct{}

func (compilerTestRenderer) RenderAnswerInput(query, renderedEvidence string) AnswerInput {
	return AnswerInput{Model: "local", System: "answer from evidence", User: query + "\n" + renderedEvidence}
}

type compilerTestPlanner struct {
	proposal Proposal
	err      error
}

func (planner compilerTestPlanner) Propose(context.Context, string, []Candidate) (Proposal, error) {
	return planner.proposal, planner.err
}
