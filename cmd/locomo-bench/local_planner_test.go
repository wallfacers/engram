package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
)

// stubPlannerProvider stands in for a local vllm/ollama sidecar. It is a
// provider.Provider so local_planner exercises the real Stream-consumption
// path (usageModelCaller) without needing a live endpoint.
type stubPlannerProvider struct {
	text  string
	texts []string // per-call text queue; last entry repeats
	err   error
	delay time.Duration
	calls int
	// lastUser captures the rendered user prompt from the last request, so
	// tests can assert the query and frozen candidates reached the sidecar.
	lastUser string
}

func (s *stubPlannerProvider) Name() string { return "stub" }

func (s *stubPlannerProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.ProviderEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			for _, b := range m.Content {
				if b.Type == provider.BlockText {
					s.lastUser += b.Text
				}
			}
		}
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan provider.ProviderEvent, 4)
	if s.err != nil {
		ch <- provider.ProviderEvent{Type: provider.EventError, Error: &provider.ProviderError{Provider: s.Name(), Message: "boom"}}
		close(ch)
		return ch, nil
	}
	text := s.text
	if len(s.texts) > 0 {
		idx := s.calls
		if idx >= len(s.texts) {
			idx = len(s.texts) - 1
		}
		text = s.texts[idx]
	}
	s.calls++
	ch <- provider.ProviderEvent{Type: provider.EventTextDelta, TextDelta: text}
	ch <- provider.ProviderEvent{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 20}}
	ch <- provider.ProviderEvent{Type: provider.EventStop, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

func newStubPlanner(stub *stubPlannerProvider) *localPlanner {
	p, err := newLocalPlanner(localPlannerConfig{
		Provider:  stub,
		Model:     "stub-model",
		MaxTokens: 512,
	})
	if err != nil {
		panic(err)
	}
	return p
}

func isFallback(err error) bool {
	return errors.Is(err, errPlannerUnavailable) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

// TestPlannerProposeParsesProposal: a valid sidecar proposal JSON maps to the
// frozen Proposal contract (Need + Actions).
func TestPlannerProposeParsesProposal(t *testing.T) {
	const out = `{"need":{"entities":["Alice"],"time_constraints":["2024-05-01"],"operands":[{"name":"count","satisfied":false}],"list_cardinality":{"known":true,"count":3},"update_state":"updated"},"actions":[{"kind":"KEEP","candidate_id":"c1","source_id":"s1"},{"kind":"EXTRACT","candidate_id":"c2","source_id":"s2","span":{"source_id":"s2","start_char":4,"end_char":10,"span_digest":"abc"}},{"kind":"MERGE","sentences":[{"text":"sum","sources":[{"source_id":"s3","start_char":0,"end_char":3}]}]}]}`
	p := newStubPlanner(&stubPlannerProvider{text: out})

	proposal, err := p.Propose(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(proposal.Need.Entities) != 1 || proposal.Need.Entities[0] != "Alice" {
		t.Fatalf("entities: %#v", proposal.Need.Entities)
	}
	if len(proposal.Need.TimeConstraints) != 1 || proposal.Need.TimeConstraints[0] != "2024-05-01" {
		t.Fatalf("time constraints: %#v", proposal.Need.TimeConstraints)
	}
	if !proposal.Need.ListCardinality.Known || proposal.Need.ListCardinality.Count != 3 {
		t.Fatalf("cardinality: %#v", proposal.Need.ListCardinality)
	}
	if len(proposal.Actions) != 3 {
		t.Fatalf("got %d actions, want 3", len(proposal.Actions))
	}
	if proposal.Actions[0].Kind != evidencecompiler.ActionKeep || proposal.Actions[0].CandidateID != "c1" || proposal.Actions[0].SourceID != "s1" {
		t.Fatalf("action[0]: %#v", proposal.Actions[0])
	}
	if proposal.Actions[1].Kind != evidencecompiler.ActionExtract || proposal.Actions[1].Span == nil ||
		proposal.Actions[1].Span.SourceID != "s2" || proposal.Actions[1].Span.StartChar != 4 || proposal.Actions[1].Span.EndChar != 10 {
		t.Fatalf("action[1]: %#v", proposal.Actions[1])
	}
	if proposal.Actions[2].Kind != evidencecompiler.ActionMerge || len(proposal.Actions[2].Sentences) != 1 ||
		proposal.Actions[2].Sentences[0].Text != "sum" || len(proposal.Actions[2].Sentences[0].Sources) != 1 {
		t.Fatalf("action[2]: %#v", proposal.Actions[2])
	}
}

// TestPlannerProposeStripsCodeFence: a model that wraps JSON in a ```json fence
// must still parse (common Qwen/chat-template behavior).
func TestPlannerProposeStripsCodeFence(t *testing.T) {
	const out = "```json\n{\"need\":{\"entities\":[\"Bob\"]},\"actions\":[{\"kind\":\"KEEP\",\"candidate_id\":\"c1\",\"source_id\":\"s1\"}]}\n```"
	p := newStubPlanner(&stubPlannerProvider{text: out})
	proposal, err := p.Propose(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Propose with code fence: %v", err)
	}
	if len(proposal.Need.Entities) != 1 || proposal.Need.Entities[0] != "Bob" {
		t.Fatalf("entities: %#v", proposal.Need.Entities)
	}
}

// TestPlannerProposeProviderErrorFallbacks: a sidecar failure must fall back
// (plain error, not a propagated context error).
func TestPlannerProposeProviderErrorFallbacks(t *testing.T) {
	p := newStubPlanner(&stubPlannerProvider{err: errors.New("connection refused")})
	_, err := p.Propose(context.Background(), "q", nil)
	if err == nil {
		t.Fatal("want error on provider failure")
	}
	if !isFallback(err) {
		t.Fatalf("want fallback error, got %v (canceled=%v deadline=%v)", err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
	}
}

// TestPlannerProposeUnparsableFallbacks: non-JSON output must fall back.
func TestPlannerProposeUnparsableFallbacks(t *testing.T) {
	p := newStubPlanner(&stubPlannerProvider{text: "I don't know."})
	_, err := p.Propose(context.Background(), "q", nil)
	if err == nil {
		t.Fatal("want error on unparsable output")
	}
	if !isFallback(err) {
		t.Fatalf("want fallback error, got %v", err)
	}
}

// TestPlannerProposeInvalidActionKindFallbacks: a planner proposing an action
// outside the frozen union must fall back (never partially admitted).
func TestPlannerProposeInvalidActionKindFallbacks(t *testing.T) {
	p := newStubPlanner(&stubPlannerProvider{text: `{"need":{},"actions":[{"kind":"HACK","candidate_id":"c1"}]}`})
	_, err := p.Propose(context.Background(), "q", nil)
	if err == nil {
		t.Fatal("want error on invalid action kind")
	}
	if !isFallback(err) {
		t.Fatalf("want fallback error, got %v", err)
	}
}

// TestPlannerProposeCancelledPropagates: caller cancellation must propagate
// unchanged (FR-019), not be swallowed as a fallback.
func TestPlannerProposeCancelledPropagates(t *testing.T) {
	p := newStubPlanner(&stubPlannerProvider{text: `{"need":{},"actions":[]}`})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Propose(ctx, "q", nil)
	if err == nil {
		t.Fatal("want error on cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestPlannerProposeTimeoutFallbacks: the planner's own planning timeout must
// fall back (plain error), distinct from caller cancellation propagation.
func TestPlannerProposeTimeoutFallbacks(t *testing.T) {
	p, err := newLocalPlanner(localPlannerConfig{
		Provider:  &stubPlannerProvider{delay: 200 * time.Millisecond},
		Model:     "stub-model",
		MaxTokens: 512,
		Timeout:   20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("newLocalPlanner: %v", err)
	}
	_, err = p.Propose(context.Background(), "q", nil)
	if err == nil {
		t.Fatal("want error on planning timeout")
	}
	if !isFallback(err) {
		t.Fatalf("want fallback error on internal timeout, got %v (canceled=%v deadline=%v)", err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
	}
}

// TestNewLocalPlannerRejectsEmptyConfig: unconfigured planner must not silently
// construct a working instance; the caller maps this to deterministic fallback.
func TestNewLocalPlannerRejectsEmptyConfig(t *testing.T) {
	if _, err := newLocalPlanner(localPlannerConfig{}); err == nil {
		t.Fatal("want error on empty config")
	}
	if _, err := newLocalPlanner(localPlannerConfig{Provider: &stubPlannerProvider{}, Model: ""}); err == nil {
		t.Fatal("want error on empty model")
	}
}

// TestPlannerProposeIncludesQueryAndCandidates: the rendered user prompt must
// carry the query and every frozen candidate id/text so the sidecar can ground
// Need/actions in the lineage (contract-shape guard).
func TestPlannerProposeIncludesQueryAndCandidates(t *testing.T) {
	stub := &stubPlannerProvider{text: `{"need":{},"actions":[]}`}
	p := newStubPlanner(stub)
	cands := []evidencecompiler.Candidate{
		{ID: "c1", Kind: evidencecompiler.CandidateChunk, Rank: 0, Text: "first"},
		{ID: "c2", Kind: evidencecompiler.CandidateRawTurn, Rank: 1, Text: "second"},
	}
	if _, err := p.Propose(context.Background(), "what did Alice buy?", cands); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	for _, want := range []string{"what did Alice buy?", "c1", "c2", "second"} {
		if !strings.Contains(stub.lastUser, want) {
			t.Fatalf("rendered prompt must contain %q; got: %q", want, stub.lastUser)
		}
	}
}

// TestPlannerGapToContractKindCasing: planner gap kinds come in any casing; the
// contract constants are lower-case, so "entity"/"ENTITY" must both map onto
// GapEntity and unknown kinds must be rejected (the ToUpper bug rejected every
// legal gap).
func TestPlannerGapToContractKindCasing(t *testing.T) {
	for _, kind := range []string{"entity", "ENTITY", "Entity"} {
		g, err := plannerGapToContract(plannerGap{Kind: kind, SourceNeed: "entity:X"})
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if g.Kind != evidencecompiler.GapEntity {
			t.Fatalf("kind %q mapped to %q, want %q", kind, g.Kind, evidencecompiler.GapEntity)
		}
	}
	for _, kind := range []string{"time_range", "TIME_RANGE"} {
		g, err := plannerGapToContract(plannerGap{Kind: kind, Start: "2026-01-01", SourceNeed: "time:2026-01-01"})
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if g.Kind != evidencecompiler.GapTimeRange {
			t.Fatalf("kind %q mapped to %q, want %q", kind, g.Kind, evidencecompiler.GapTimeRange)
		}
	}
	if _, err := plannerGapToContract(plannerGap{Kind: "nonsense"}); err == nil {
		t.Fatal("unknown gap kind must be rejected")
	}
}
