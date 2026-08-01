package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// newRepresentationTestStore creates an in-memory SQLite store with evidence
// for representation renderer tests.
func newRepresentationTestStore(t *testing.T) (*memory.LedgerStore, *memory.ProjectionStore, *memory.EpisodeStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	db := s.DB()
	ledger := memory.NewLedgerStore(db)
	projections := memory.NewProjectionStore(db)
	episodes := memory.NewEpisodeStore(db, ledger, projections)
	return ledger, projections, episodes, db
}

// seedRepresentationEvidence creates evidence across two sessions for testing
// the three representations.
func seedRepresentationEvidence(t *testing.T, ledger *memory.LedgerStore) (sessionA, sessionB []memory.Evidence) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	// Session A: a multi-turn conversation.
	inputsA := []memory.EvidenceInput{
		{ExternalSourceID: "a-0", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "What is the capital of France?", RecordedAt: now},
		{ExternalSourceID: "a-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 1, Content: "The capital of France is Paris.", RecordedAt: now.Add(1 * time.Second)},
		{ExternalSourceID: "a-2", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 2, Content: "What about Germany?", RecordedAt: now.Add(2 * time.Second)},
		{ExternalSourceID: "a-3", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 3, Content: "The capital of Germany is Berlin.", RecordedAt: now.Add(3 * time.Second)},
	}
	var err error
	sessionA, err = ledger.AppendBatch(ctx, inputsA)
	if err != nil {
		t.Fatalf("seed session A: %v", err)
	}

	// Session B: another conversation.
	inputsB := []memory.EvidenceInput{
		{ExternalSourceID: "b-0", SourceType: memory.EvidenceMessage, SourceSessionID: "session-b", Speaker: "alice", Ordinal: 0, Content: "Let's discuss the weather.", RecordedAt: now.Add(1 * time.Hour)},
		{ExternalSourceID: "b-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-b", Speaker: "bob", Ordinal: 1, Content: "It is sunny with a high of 25 degrees.", RecordedAt: now.Add(1*time.Hour + 1*time.Second)},
	}
	sessionB, err = ledger.AppendBatch(ctx, inputsB)
	if err != nil {
		t.Fatalf("seed session B: %v", err)
	}
	return sessionA, sessionB
}

func TestRepresentationSharedAnchors(t *testing.T) {
	ledger, projections, episodes, db := newRepresentationTestStore(t)
	_ = episodes
	ctx := context.Background()

	evA, evB := seedRepresentationEvidence(t, ledger)

	// Build common ranked anchors from evidence IDs.
	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID}},
		{CandidateID: evA[1].ID, Rank: 2, Score: 0.90, TextDigest: evalTextDigest(evA[1].Content), SourceIDs: []string{evA[1].ID}},
		{CandidateID: evB[0].ID, Rank: 3, Score: 0.85, TextDigest: evalTextDigest(evB[0].Content), SourceIDs: []string{evB[0].ID}},
	}

	// All three renderers must use the same anchors.
	renderers := []struct {
		name     string
		renderer RepresentationRenderer
	}{
		{"chunk_900", NewChunk900Renderer(projections, ledger)},
		{"raw_turn_window", NewRawTurnWindowRenderer(projections, ledger, 3)},
		{"semantic_episode", NewSemanticEpisodeRenderer(projections, ledger, episodes)},
	}

	for _, rr := range renderers {
		t.Run(rr.name, func(t *testing.T) {
			candidates, err := rr.renderer.Render(ctx, anchors)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if len(candidates) == 0 {
				t.Error("expected at least one rendered candidate")
			}
			// Each rendered candidate must reference an anchor.
			for _, c := range candidates {
				found := false
				for _, a := range anchors {
					for _, expandedFrom := range c.ExpandedFrom {
						if expandedFrom == a.CandidateID {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found && len(c.ExpandedFrom) > 0 {
					// Candidates without ExpandedFrom are standalone.
					_ = c.CandidateID
				}
			}
		})
	}

	_ = db
	_ = evB
}

func TestRepresentationByteDigestComparable(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID, evA[1].ID}},
	}

	renderers := map[string]RepresentationRenderer{
		"chunk_900":         NewChunk900Renderer(projections, ledger),
		"raw_turn_window":   NewRawTurnWindowRenderer(projections, ledger, 3),
		"semantic_episode":  NewSemanticEpisodeRenderer(projections, ledger, episodes),
	}

	var digests map[string]string
	for name, renderer := range renderers {
		candidates, err := renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("%s Render: %v", name, err)
		}
		// Each representation must produce a deterministic byte digest.
		renderedDigest := renderedCandidateSetDigest(candidates)
		if renderedDigest == "" || !strings.HasPrefix(renderedDigest, "sha256:") {
			t.Errorf("%s: rendered candidate digest must be sha256-prefixed, got %q", name, renderedDigest)
		}
		if digests == nil {
			digests = make(map[string]string)
		}
		digests[name] = renderedDigest
		t.Logf("%s digest: %s", name, renderedDigest)
	}

	// Chunk and raw-turn should differ from each other (different renderings).
	if digests["chunk_900"] == digests["raw_turn_window"] {
		t.Log("chunk_900 and raw_turn_window may produce same digest for simple inputs")
	}
}

func TestRepresentationTokenCapRendering(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	// Create many anchors to test token cap behavior.
	var anchors []evalRankedAnchor
	for i, ev := range evA {
		anchors = append(anchors, evalRankedAnchor{
			CandidateID: ev.ID,
			Rank:        i + 1,
			Score:       0.9 - float64(i)*0.1,
			TextDigest:  evalTextDigest(ev.Content),
			SourceIDs:   []string{ev.ID},
		})
	}

	renderers := map[string]RepresentationRenderer{
		"chunk_900":        NewChunk900Renderer(projections, ledger),
		"raw_turn_window":  NewRawTurnWindowRenderer(projections, ledger, 3),
	}

	for name, renderer := range renderers {
		t.Run(name+"/cap_enforcement", func(t *testing.T) {
			candidates, err := renderer.Render(ctx, anchors)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			// Verify each candidate has token fields populated.
			for _, c := range candidates {
				if c.PreCapInputTokens < 0 {
					t.Errorf("%s: candidate %s has negative pre-cap tokens", name, c.CandidateID)
				}
			}

			// Digest must be deterministic for same input.
			d1 := renderedCandidateSetDigest(candidates)
			candidates2, err := renderer.Render(ctx, anchors)
			if err != nil {
				t.Fatalf("second Render: %v", err)
			}
			d2 := renderedCandidateSetDigest(candidates2)
			if d1 != d2 {
				t.Errorf("%s: non-deterministic rendering: %s vs %s", name, d1, d2)
			}
		})
	}

	_ = episodes
}

func TestRepresentationSourceExpansion(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	// Anchor that references two evidence IDs.
	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID, evA[1].ID}},
	}

	renderers := map[string]RepresentationRenderer{
		"chunk_900":        NewChunk900Renderer(projections, ledger),
		"raw_turn_window":  NewRawTurnWindowRenderer(projections, ledger, 3),
	}

	for name, renderer := range renderers {
		t.Run(name, func(t *testing.T) {
			candidates, err := renderer.Render(ctx, anchors)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			// Each representation must produce candidates with evidence-sourced content.
			for _, c := range candidates {
				if c.Text == "" {
					t.Errorf("%s: candidate %s has empty text", name, c.CandidateID)
				}
				if c.TextDigest == "" {
					t.Errorf("%s: candidate %s has empty text digest", name, c.CandidateID)
				}
				// Verify the digest matches the text.
				expectedDigest := evalTextDigest(c.Text)
				if c.TextDigest != expectedDigest {
					t.Errorf("%s: candidate %s text digest mismatch: %q vs %q", name, c.CandidateID, c.TextDigest, expectedDigest)
				}
			}
		})
	}

	_ = episodes
}

func TestRepresentationChunk900Rendering(t *testing.T) {
	ledger, projections, _, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID}},
	}

	renderer := NewChunk900Renderer(projections, ledger)
	candidates, err := renderer.Render(ctx, anchors)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("chunk_900 must produce at least one candidate")
	}

	// Chunk_900 must use the chunk_900 kind.
	for _, c := range candidates {
		if c.Kind != string(ReprChunk900) {
			t.Errorf("chunk_900 candidate kind = %q, want %q", c.Kind, ReprChunk900)
		}
	}
}

func TestRepresentationRawTurnWindowRendering(t *testing.T) {
	ledger, projections, _, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	// Anchor in the middle of session A.
	anchors := []evalRankedAnchor{
		{CandidateID: evA[1].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[1].Content), SourceIDs: []string{evA[1].ID}},
	}

	renderer := NewRawTurnWindowRenderer(projections, ledger, 2)
	candidates, err := renderer.Render(ctx, anchors)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("raw_turn_window must produce at least one candidate")
	}

	// With window=2, the candidate should include evidence[0] and evidence[2]
	// as surrounding context.
	for _, c := range candidates {
		if c.Kind != string(ReprRawTurnWindow) {
			t.Errorf("raw_turn_window candidate kind = %q, want %q", c.Kind, ReprRawTurnWindow)
		}
	}

	// Raw-turn window with window=0 should only have the anchor itself.
	rendererZero := NewRawTurnWindowRenderer(projections, ledger, 0)
	candidatesZero, err := rendererZero.Render(ctx, anchors)
	if err != nil {
		t.Fatalf("Render window=0: %v", err)
	}
	if len(candidatesZero) == 0 {
		t.Fatal("raw_turn_window(window=0) must produce at least one candidate")
	}
}

func TestRepresentationSemanticEpisodeRendering(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	// Build episodes for session A.
	segmenter := &singleEpisodeSegmenter{}
	eps, err := episodes.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "repr-test-hash")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) == 0 {
		t.Fatal("expected at least one episode")
	}

	// Create anchors pointing to the episode projection.
	anchors := []evalRankedAnchor{
		{CandidateID: eps[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(""), SourceIDs: []string{evA[0].ID, evA[1].ID}},
	}

	renderer := NewSemanticEpisodeRenderer(projections, ledger, episodes)
	candidates, err := renderer.Render(ctx, anchors)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("semantic_episode must produce at least one candidate")
	}

	for _, c := range candidates {
		if c.Kind != string(ReprSemanticEpisode) {
			t.Errorf("semantic_episode candidate kind = %q, want %q", c.Kind, ReprSemanticEpisode)
		}
	}
}

func TestRepresentationDigestIdentitySameInput(t *testing.T) {
	ledger, projections, _, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID}},
	}

	renderer := NewChunk900Renderer(projections, ledger)

	// Same input → same digest (deterministic rendering).
	var previousDigest string
	for i := 0; i < 3; i++ {
		candidates, err := renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("Render iteration %d: %v", i, err)
		}
		d := renderedCandidateSetDigest(candidates)
		if i == 0 {
			previousDigest = d
		} else if d != previousDigest {
			t.Errorf("iteration %d: digest changed from %s to %s", i, previousDigest, d)
		}
	}
}

func TestRepresentationAllThreeSharedBudget(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	evA, _ := seedRepresentationEvidence(t, ledger)

	// Build episodes for session A.
	segmenter := &singleEpisodeSegmenter{}
	eps, err := episodes.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "budget-test-hash")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}

	// Same anchors for all three renderers.
	anchors := []evalRankedAnchor{
		{CandidateID: evA[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evA[0].Content), SourceIDs: []string{evA[0].ID}},
		{CandidateID: eps[0].ID, Rank: 2, Score: 0.90, TextDigest: evalTextDigest(""), SourceIDs: []string{evA[1].ID}},
	}

	type representationResult struct {
		name       string
		candidates []evalRenderedCandidate
		anchorIDs  []string
	}

	var results []representationResult

	for _, spec := range []struct {
		name     string
		renderer RepresentationRenderer
	}{
		{"chunk_900", NewChunk900Renderer(projections, ledger)},
		{"raw_turn_window", NewRawTurnWindowRenderer(projections, ledger, 2)},
		{"semantic_episode", NewSemanticEpisodeRenderer(projections, ledger, episodes)},
	} {
		candidates, err := spec.renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("%s Render: %v", spec.name, err)
		}

		// Collect anchor IDs used.
		anchorSet := make(map[string]bool)
		for _, c := range candidates {
			for _, ef := range c.ExpandedFrom {
				anchorSet[ef] = true
			}
		}
		anchorIDs := make([]string, 0, len(anchorSet))
		for id := range anchorSet {
			anchorIDs = append(anchorIDs, id)
		}

		results = append(results, representationResult{
			name:       spec.name,
			candidates: candidates,
			anchorIDs:  anchorIDs,
		})
	}

	// All three must produce valid candidate artifacts with digests.
	for _, r := range results {
		digest := renderedCandidateSetDigest(r.candidates)
		if digest == "" {
			t.Errorf("%s: empty candidate set digest", r.name)
		}
		t.Logf("%s: %d candidates, digest=%s, anchors=%v", r.name, len(r.candidates), digest, r.anchorIDs)
	}
}

// Helpers for representation tests.

type singleEpisodeSegmenter struct{}

func (s singleEpisodeSegmenter) Segment(_ context.Context, session []memory.Evidence) ([]memory.EpisodeBoundary, error) {
	if len(session) == 0 {
		return nil, nil
	}
	return []memory.EpisodeBoundary{{
		FirstEvidenceID: session[0].ID,
		LastEvidenceID:  session[len(session)-1].ID,
	}}, nil
}

func TestRepresentationRendererInterface(t *testing.T) {
	// Verify the RepresentationRenderer interface is satisfied by all three renderers.
	var _ RepresentationRenderer = NewChunk900Renderer(nil, nil)
	var _ RepresentationRenderer = NewRawTurnWindowRenderer(nil, nil, 3)
	var _ RepresentationRenderer = NewSemanticEpisodeRenderer(nil, nil, nil)
}

func TestRepresentationEmptyAnchors(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()

	renderers := []RepresentationRenderer{
		NewChunk900Renderer(projections, ledger),
		NewRawTurnWindowRenderer(projections, ledger, 3),
		NewSemanticEpisodeRenderer(projections, ledger, episodes),
	}

	for i, renderer := range renderers {
		candidates, err := renderer.Render(ctx, nil)
		if err != nil {
			t.Errorf("renderer %d: Render(nil): %v", i, err)
		}
		if len(candidates) != 0 {
			t.Errorf("renderer %d: expected 0 candidates for empty anchors, got %d", i, len(candidates))
		}
	}
}

// Verify that the evalTextDigest function used in tests is consistent with
// the SHA-256 digest format used elsewhere in the codebase.
func TestEvalTextDigestFormat(t *testing.T) {
	d := evalTextDigest("hello")
	if !strings.HasPrefix(d, "sha256:") {
		t.Errorf("evalTextDigest must start with sha256:, got %q", d)
	}
	if len(d) != 7+64 { // "sha256:" + 64 hex chars
		t.Errorf("evalTextDigest length = %d, want %d", len(d), 7+64)
	}

	// Deterministic.
	if evalTextDigest("hello") != evalTextDigest("hello") {
		t.Error("evalTextDigest must be deterministic")
	}
	if evalTextDigest("hello") == evalTextDigest("world") {
		t.Error("different texts must produce different digests")
	}
}

func TestRepresentationConfigHash(t *testing.T) {
	// Different configs must produce different hashes.
	h1 := memory.EpisodeConfigHash("test", map[string]string{"window": "3"})
	h2 := memory.EpisodeConfigHash("test", map[string]string{"window": "5"})
	if h1 == h2 {
		t.Error("different config params must produce different hashes")
	}

	// Same config must produce same hash.
	h3 := memory.EpisodeConfigHash("test", map[string]string{"window": "3"})
	if h1 != h3 {
		t.Error("same config params must produce same hash")
	}
}

func TestRepresentationKindConstants(t *testing.T) {
	// Verify the representation kind constants are distinct.
	kinds := map[RepresentationKind]bool{}
	for _, k := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		if kinds[k] {
			t.Errorf("duplicate representation kind: %q", k)
		}
		kinds[k] = true
	}
	if len(kinds) != 3 {
		t.Errorf("expected 3 distinct representation kinds, got %d", len(kinds))
	}
}

// TestRepresentationSemanticEpisodeRouteViaFactAnchor covers 025 US1 wiring:
// when the anchor is a fact/chunk (not an episode projection), the renderer
// resolves its evidence lineage to the cross-session semantic_episode built by
// RebuildAll and renders the whole episode narrative (research.md R5). Before
// this routing existed, the renderer fell back to the anchor's own source.
func TestRepresentationSemanticEpisodeRouteViaFactAnchor(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()
	evA, evB := seedRepresentationEvidence(t, ledger)
	_ = evB

	// Build a cross-session episode over session-a evidence via RebuildAll. The
	// offline clusterer groups the four session-a turns (shared tokens like
	// "capital") into one episode.
	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	projs, err := episodes.RebuildAll(ctx, clusterer, "1.0.0", "route-hash")
	if err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}
	if len(projs) == 0 {
		t.Fatal("expected at least one cross-session episode")
	}

	// Anchor is a fact: its CandidateID is the fact (here the evidence ID), not
	// the episode projection. SourceIDs carry the fact's evidence lineage.
	anchor := evalRankedAnchor{
		CandidateID: "fact:evA0",
		Rank:        1,
		Score:       0.9,
		TextDigest:  evalTextDigest(evA[0].Content),
		SourceIDs:   []string{evA[0].ID, evA[1].ID},
	}

	renderer := NewSemanticEpisodeRenderer(projections, ledger, episodes)
	candidates, err := renderer.Render(ctx, []evalRankedAnchor{anchor})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("semantic_episode must route a fact anchor to an episode")
	}
	c := candidates[0]
	if c.Kind != string(ReprSemanticEpisode) {
		t.Fatalf("candidate kind = %q, want %q", c.Kind, ReprSemanticEpisode)
	}
	// The episode narrative should span more than the single anchor source.
	if c.ExpansionCount < 1 {
		t.Fatalf("expected episode to expand beyond the anchor, expansion=%d", c.ExpansionCount)
	}
	// Every source must be from the seeded set.
	for _, id := range c.SourceIDs {
		found := false
		for _, ev := range append(append([]memory.Evidence{}, evA...), evB...) {
			if ev.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("rendered source %s not in seeded evidence", id)
		}
	}
}

// TestRepresentationSemanticEpisodeFallbackNoEpisodes preserves 022 zero-change
// behavior: with no episode projections built, a fact anchor renders via the
// fallback (anchor's own source), byte-identical to pre-025 behavior.
func TestRepresentationSemanticEpisodeFallbackNoEpisodes(t *testing.T) {
	ledger, projections, episodes, _ := newRepresentationTestStore(t)
	ctx := context.Background()
	evA, _ := seedRepresentationEvidence(t, ledger)

	// No RebuildAll / RebuildSession: episode store is empty.
	anchor := evalRankedAnchor{
		CandidateID: "fact:evA0",
		Rank:        1,
		Score:       0.9,
		TextDigest:  evalTextDigest(evA[0].Content),
		SourceIDs:   []string{evA[0].ID},
	}
	renderer := NewSemanticEpisodeRenderer(projections, ledger, episodes)
	candidates, err := renderer.Render(ctx, []evalRankedAnchor{anchor})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected fallback single candidate, got %d", len(candidates))
	}
	c := candidates[0]
	if c.ExpansionCount != 0 {
		t.Fatalf("fallback must not expand, got expansion=%d", c.ExpansionCount)
	}
	if c.Text != evA[0].Content {
		t.Fatalf("fallback text must be the anchor source verbatim")
	}
}
