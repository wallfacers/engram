package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/eventstore"
)

type mockEvidenceReader struct{}

func (mockEvidenceReader) Get(_ context.Context, id string) (*memory.Evidence, error) {
	return &memory.Evidence{ID: id, Content: "raw:" + id}, nil
}
func (mockEvidenceReader) GetMany(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
	out := make(map[string]memory.Evidence, len(ids))
	for _, id := range ids {
		out[id] = memory.Evidence{ID: id, Content: "raw:" + id}
	}
	return out, nil
}
func (mockEvidenceReader) ListSession(context.Context, string, bool) ([]memory.Evidence, error) {
	return nil, nil
}

func sampleProject() *eventstore.Project {
	ev := eventstore.Event{
		ID:              "ev-1",
		ConversationID:  "conv-0",
		SourceLedgerIDs: []string{"e1", "e2"},
		Speaker:         "user",
		FactEntries:     []eventstore.FactEntry{{Text: "Caroline attended Pride on 2023-08-11", Grounded: true}},
		RelationEntries: []eventstore.RelationEntry{{
			RelationType: "co_participation", Subject: "Caroline", Object: "Melanie",
			Text: "Melanie is interested in Caroline's Pride experience",
		}},
		AbsoluteTS: "2023-08-17T00:00:00Z",
	}
	return eventstore.BuildProject("conv-0", "hash", []eventstore.Event{ev}, nil)
}

func TestEventRendererMatchesEvent(t *testing.T) {
	r := NewEventProjectionRenderer(sampleProject(), mockEvidenceReader{})
	anchors := []evalRankedAnchor{{CandidateID: "a1", Rank: 1, Score: 0.9, SourceIDs: []string{"e2"}}}
	cands, err := r.Render(context.Background(), anchors)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 event candidate, got %d", len(cands))
	}
	if !strings.Contains(cands[0].Text, "EVENT") || !strings.Contains(cands[0].Text, "FACT") {
		t.Fatalf("event text missing markers: %q", cands[0].Text)
	}
	if !strings.Contains(cands[0].Text, "co_participation") {
		t.Fatalf("relation missing from event text: %q", cands[0].Text)
	}
	if cands[0].Kind != string(ReprEvent) {
		t.Fatalf("kind should be event, got %q", cands[0].Kind)
	}
}

func TestEventRendererDedupAndFallback(t *testing.T) {
	r := NewEventProjectionRenderer(sampleProject(), mockEvidenceReader{})
	// Two anchors referencing the same event must produce one candidate.
	anchors := []evalRankedAnchor{
		{CandidateID: "a1", Rank: 1, Score: 0.9, SourceIDs: []string{"e1"}},
		{CandidateID: "a2", Rank: 2, Score: 0.5, SourceIDs: []string{"e2"}},
	}
	cands, err := r.Render(context.Background(), anchors)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected dedup to 1 candidate, got %d", len(cands))
	}
	// An anchor with no matching event falls back to raw source text.
	r2 := NewEventProjectionRenderer(sampleProject(), mockEvidenceReader{})
	cands2, err := r2.Render(context.Background(), []evalRankedAnchor{
		{CandidateID: "a9", Rank: 1, Score: 0.3, SourceIDs: []string{"e9"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(cands2) != 1 || !strings.Contains(cands2[0].Text, "raw:e9") {
		t.Fatalf("fallback expected raw source, got %+v", cands2)
	}
}

func TestEventRendererNilProjectDegrades(t *testing.T) {
	r := NewEventProjectionRenderer(nil, mockEvidenceReader{})
	cands, err := r.Render(context.Background(), []evalRankedAnchor{
		{CandidateID: "a9", Rank: 1, Score: 0.3, SourceIDs: []string{"e9"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(cands) != 1 || !strings.Contains(cands[0].Text, "raw:e9") {
		t.Fatalf("nil project should degrade to raw sources, got %+v", cands)
	}
}
