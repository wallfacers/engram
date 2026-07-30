package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// setupIntegrationStore creates both the production store and a run-dir
// for end-to-end representation tests.
func setupIntegrationStore(t *testing.T) (*memory.LedgerStore, *memory.ProjectionStore, *memory.EpisodeStore, *sql.DB, string) {
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

	runDir, err := os.MkdirTemp("", "representation-integration-*")
	if err != nil {
		t.Fatalf("create temp run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(runDir) })

	return ledger, projections, episodes, db, runDir
}

// seedIntegrationEvidence creates a realistic multi-session conversation.
func seedIntegrationEvidence(t *testing.T, ledger *memory.LedgerStore) []memory.Evidence {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	inputs := []memory.EvidenceInput{
		{ExternalSourceID: "int-0", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-1", Speaker: "user", Ordinal: 0, Content: "I need to book a flight to Tokyo.", RecordedAt: now},
		{ExternalSourceID: "int-1", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-1", Speaker: "assistant", Ordinal: 1, Content: "Sure! When would you like to depart?", RecordedAt: now.Add(1 * time.Second)},
		{ExternalSourceID: "int-2", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-1", Speaker: "user", Ordinal: 2, Content: "Next Monday, preferably morning.", RecordedAt: now.Add(2 * time.Second)},
		{ExternalSourceID: "int-3", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-1", Speaker: "assistant", Ordinal: 3, Content: "I found a flight at 8am. It's $450 round trip.", RecordedAt: now.Add(3 * time.Second)},
		{ExternalSourceID: "int-4", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-2", Speaker: "user", Ordinal: 0, Content: "What's the weather in Tokyo next week?", RecordedAt: now.Add(1 * time.Hour)},
		{ExternalSourceID: "int-5", SourceType: memory.EvidenceMessage, SourceSessionID: "conv-2", Speaker: "assistant", Ordinal: 1, Content: "It will be partly cloudy with highs around 15°C.", RecordedAt: now.Add(1*time.Hour + 1*time.Second)},
	}
	evidence, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("seed integration evidence: %v", err)
	}
	return evidence
}

func TestIntegrationFullBakeOffFlow(t *testing.T) {
	ledger, projections, episodes, _, runDir := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	// Step 1: Build semantic episodes for both conversations.
	segmenter := &singleEpisodeSegmenter{}
	eps1, err := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "integration-hash-1")
	if err != nil {
		t.Fatalf("RebuildSession conv-1: %v", err)
	}
	eps2, err := episodes.RebuildSession(ctx, "conv-2", segmenter, "1.0.0", "integration-hash-2")
	if err != nil {
		t.Fatalf("RebuildSession conv-2: %v", err)
	}
	_ = eps2

	// Step 2: Create shared ranked anchors from the evidence.
	anchors := []evalRankedAnchor{
		{CandidateID: evidence[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evidence[0].Content), SourceIDs: []string{evidence[0].ID}},
		{CandidateID: evidence[1].ID, Rank: 2, Score: 0.90, TextDigest: evalTextDigest(evidence[1].Content), SourceIDs: []string{evidence[1].ID}},
		{CandidateID: evidence[4].ID, Rank: 3, Score: 0.85, TextDigest: evalTextDigest(evidence[4].Content), SourceIDs: []string{evidence[4].ID}},
		{CandidateID: eps1[0].ID, Rank: 4, Score: 0.80, TextDigest: evalTextDigest(""), SourceIDs: []string{evidence[0].ID, evidence[1].ID, evidence[2].ID, evidence[3].ID}},
	}

	// Step 3: Render with all three representations.
	renderers := map[RepresentationKind]RepresentationRenderer{
		ReprChunk900:        NewChunk900Renderer(projections, ledger),
		ReprRawTurnWindow:   NewRawTurnWindowRenderer(projections, ledger, 2),
		ReprSemanticEpisode: NewSemanticEpisodeRenderer(projections, ledger, episodes),
	}

	type armResult struct {
		kind       RepresentationKind
		candidates []evalRenderedCandidate
		digest     string
	}

	var arms []armResult
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		renderer := renderers[kind]
		candidates, err := renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("%s Render: %v", kind, err)
		}
		digest := renderedCandidateSetDigest(candidates)
		arms = append(arms, armResult{kind: kind, candidates: candidates, digest: digest})
		t.Logf("%s: %d candidates, digest=%s", kind, len(candidates), digest)
	}

	// Step 4: Verify each representation produced candidates.
	for _, arm := range arms {
		if len(arm.candidates) == 0 {
			t.Errorf("%s: no candidates produced", arm.kind)
		}
		if arm.digest == "" {
			t.Errorf("%s: empty digest", arm.kind)
		}
		// Every candidate must have valid text and digest.
		for _, c := range arm.candidates {
			if c.Text == "" {
				t.Errorf("%s: candidate %s has empty text", arm.kind, c.CandidateID)
			}
			if c.TextDigest == "" {
				t.Errorf("%s: candidate %s has empty text digest", arm.kind, c.CandidateID)
			}
			if c.TextDigest != evalTextDigest(c.Text) {
				t.Errorf("%s: candidate %s text digest mismatch", arm.kind, c.CandidateID)
			}
		}
	}

	// Step 5: Store rendered candidates in shadow indexes.
	for _, arm := range arms {
		shadow := NewRepresentationShadowIndex(runDir, arm.kind)
		if err := shadow.Open(ctx); err != nil {
			t.Fatalf("%s shadow Open: %v", arm.kind, err)
		}
		if err := shadow.IndexCandidates(ctx, arm.candidates); err != nil {
			t.Fatalf("%s shadow IndexCandidates: %v", arm.kind, err)
		}

		// Verify round-trip: read back and compare.
		readBack, err := shadow.GetCandidates(ctx)
		if err != nil {
			t.Fatalf("%s shadow GetCandidates: %v", arm.kind, err)
		}
		if len(readBack) != len(arm.candidates) {
			t.Errorf("%s: round-trip count mismatch: %d in, %d out", arm.kind, len(arm.candidates), len(readBack))
		}

		// Build a map of read-back candidates by ID.
		readBackByID := make(map[string]evalRenderedCandidate, len(readBack))
		for _, c := range readBack {
			readBackByID[c.CandidateID] = c
		}

		// Each original candidate must be present in the read-back with matching content.
		for _, original := range arm.candidates {
			rb, ok := readBackByID[original.CandidateID]
			if !ok {
				t.Errorf("%s: candidate %s not found in read-back", arm.kind, original.CandidateID)
				continue
			}
			if rb.Text != original.Text {
				t.Errorf("%s: round-trip text mismatch for %s", arm.kind, original.CandidateID)
			}
			if rb.TextDigest != original.TextDigest {
				t.Errorf("%s: round-trip digest mismatch for %s", arm.kind, original.CandidateID)
			}
		}

		shadow.Close() //nolint:errcheck
	}

	// Step 6: Verify shadow index files exist and are independent.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		dbPath := filepath.Join(runDir, string(kind)+".db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Errorf("%s: shadow DB not found at %s", kind, dbPath)
		}
	}

	// Step 7: Delete all shadow indexes and verify cleanup.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		shadow := NewRepresentationShadowIndex(runDir, kind)
		if err := shadow.Delete(ctx); err != nil {
			t.Errorf("%s Delete: %v", kind, err)
		}
	}
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		dbPath := filepath.Join(runDir, string(kind)+".db")
		if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
			t.Errorf("%s: shadow DB still exists after delete: %s", kind, dbPath)
		}
	}

	// Step 8: Production Evidence must be intact after everything.
	for i, ev := range evidence {
		got, err := ledger.Get(ctx, ev.ID)
		if err != nil {
			t.Fatalf("evidence[%d] should still exist: %v", i, err)
		}
		if got.State != memory.EvidenceActive {
			t.Errorf("evidence[%d] state = %q, want active", i, got.State)
		}
	}
}

func TestIntegrationEpisodeDeletePreservesEvidence(t *testing.T) {
	ledger, _, episodes, _, _ := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	// Build episodes.
	segmenter := &singleEpisodeSegmenter{}
	eps, err := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "delete-integration-hash")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) == 0 {
		t.Fatal("expected at least one episode")
	}

	// Delete the episode.
	if err := episodes.DeleteByConfig(ctx, "delete-integration-hash"); err != nil {
		t.Fatalf("DeleteByConfig: %v", err)
	}

	// All evidence from conv-1 must still be active.
	for i := 0; i < 4; i++ {
		ev, err := ledger.Get(ctx, evidence[i].ID)
		if err != nil {
			t.Fatalf("evidence[%d] should still exist after episode delete: %v", i, err)
		}
		if ev.State != memory.EvidenceActive {
			t.Errorf("evidence[%d] state = %q, want active", i, ev.State)
		}
	}

	// Episode must be rebuildable from the same Evidence.
	eps2, err := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "delete-integration-hash")
	if err != nil {
		t.Fatalf("rebuild after delete: %v", err)
	}
	if len(eps2) != len(eps) {
		t.Errorf("rebuild count mismatch: %d vs %d", len(eps2), len(eps))
	}
}

func TestIntegrationCandidateBudgetEquality(t *testing.T) {
	ledger, projections, episodes, _, _ := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	// Build episodes for both sessions.
	segmenter := &singleEpisodeSegmenter{}
	eps1, _ := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "budget-hash-1")
	eps2, _ := episodes.RebuildSession(ctx, "conv-2", segmenter, "1.0.0", "budget-hash-2")

	// Same anchors for all three renderers.
	anchors := []evalRankedAnchor{
		{CandidateID: evidence[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evidence[0].Content), SourceIDs: []string{evidence[0].ID}},
		{CandidateID: evidence[4].ID, Rank: 2, Score: 0.85, TextDigest: evalTextDigest(evidence[4].Content), SourceIDs: []string{evidence[4].ID}},
		{CandidateID: eps1[0].ID, Rank: 3, Score: 0.80, TextDigest: evalTextDigest(""), SourceIDs: []string{evidence[0].ID, evidence[1].ID}},
		{CandidateID: eps2[0].ID, Rank: 4, Score: 0.75, TextDigest: evalTextDigest(""), SourceIDs: []string{evidence[4].ID, evidence[5].ID}},
	}

	// All three renderers receive the same anchors (same budget in terms of
	// the number of anchors and their order).
	const sharedBudget = 4
	if len(anchors) != sharedBudget {
		t.Fatalf("anchor count = %d, want %d (shared budget)", len(anchors), sharedBudget)
	}

	renderers := map[RepresentationKind]RepresentationRenderer{
		ReprChunk900:        NewChunk900Renderer(projections, ledger),
		ReprRawTurnWindow:   NewRawTurnWindowRenderer(projections, ledger, 2),
		ReprSemanticEpisode: NewSemanticEpisodeRenderer(projections, ledger, episodes),
	}

	// Each renderer must see all anchors.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		renderer := renderers[kind]
		candidates, err := renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("%s Render: %v", kind, err)
		}

		// Count unique anchors referenced.
		anchorSet := make(map[string]bool)
		for _, c := range candidates {
			for _, ef := range c.ExpandedFrom {
				anchorSet[ef] = true
			}
		}
		// Each anchor must be referenced by at least one candidate.
		for _, a := range anchors {
			if !anchorSet[a.CandidateID] {
				t.Errorf("%s: anchor %s not referenced by any candidate", kind, a.CandidateID)
			}
		}
	}
}

func TestIntegrationEpisodeFaultDegradation(t *testing.T) {
	ledger, projections, episodes, _, _ := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	// Don't build episodes — the semantic episode renderer should degrade
	// gracefully by falling back to direct evidence rendering.

	anchors := []evalRankedAnchor{
		{CandidateID: evidence[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evidence[0].Content), SourceIDs: []string{evidence[0].ID}},
	}

	// Semantic episode renderer with no episodes built.
	renderer := NewSemanticEpisodeRenderer(projections, ledger, episodes)
	candidates, err := renderer.Render(ctx, anchors)
	if err != nil {
		t.Fatalf("semantic episode renderer must not fail when no episodes exist: %v", err)
	}
	// Should still produce candidates via fallback.
	if len(candidates) == 0 {
		t.Error("semantic episode renderer must degrade to fallback, not return empty")
	}
	for _, c := range candidates {
		if c.Text == "" {
			t.Error("fallback candidate has empty text")
		}
	}
}

func TestIntegrationShadowIndexDoesNotPolluteProductDB(t *testing.T) {
	ledger, projections, episodes, db, runDir := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	// Count evidence in production DB before shadow index operations.
	var evidenceCountBefore int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence`).Scan(&evidenceCountBefore); err != nil {
		t.Fatalf("count evidence before: %v", err)
	}

	// Build episodes.
	segmenter := &singleEpisodeSegmenter{}
	eps, err := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "pollute-test")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}

	anchors := []evalRankedAnchor{
		{CandidateID: evidence[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evidence[0].Content), SourceIDs: []string{evidence[0].ID}},
		{CandidateID: eps[0].ID, Rank: 2, Score: 0.80, TextDigest: evalTextDigest(""), SourceIDs: []string{evidence[0].ID, evidence[1].ID}},
	}

	// Render and index in shadow DBs.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		var renderer RepresentationRenderer
		switch kind {
		case ReprChunk900:
			renderer = NewChunk900Renderer(projections, ledger)
		case ReprRawTurnWindow:
			renderer = NewRawTurnWindowRenderer(projections, ledger, 2)
		case ReprSemanticEpisode:
			renderer = NewSemanticEpisodeRenderer(projections, ledger, episodes)
		}

		candidates, err := renderer.Render(ctx, anchors)
		if err != nil {
			t.Fatalf("%s Render: %v", kind, err)
		}

		shadow := NewRepresentationShadowIndex(runDir, kind)
		if err := shadow.Open(ctx); err != nil {
			t.Fatalf("%s shadow Open: %v", kind, err)
		}
		if err := shadow.IndexCandidates(ctx, candidates); err != nil {
			t.Fatalf("%s shadow IndexCandidates: %v", kind, err)
		}
		shadow.Close() //nolint:errcheck
	}

	// Delete shadow indexes.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		shadow := NewRepresentationShadowIndex(runDir, kind)
		shadow.Delete(ctx) //nolint:errcheck
	}

	// Production DB must be unchanged.
	var evidenceCountAfter int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_evidence`).Scan(&evidenceCountAfter); err != nil {
		t.Fatalf("count evidence after: %v", err)
	}
	if evidenceCountAfter != evidenceCountBefore {
		t.Errorf("evidence count changed: %d → %d", evidenceCountBefore, evidenceCountAfter)
	}

	// The Ledger must still serve all evidence.
	for i, ev := range evidence {
		got, err := ledger.Get(ctx, ev.ID)
		if err != nil {
			t.Fatalf("evidence[%d] missing after shadow index operations: %v", i, err)
		}
		if got.Content != ev.Content {
			t.Errorf("evidence[%d] content changed", i)
		}
	}

	// Episode projections must still exist in the product DB.
	for _, ep := range eps {
		var state string
		if err := db.QueryRowContext(ctx, `SELECT state FROM memory_projections WHERE id = ?`, ep.ID).Scan(&state); err != nil {
			t.Errorf("episode projection %s missing: %v", ep.ID, err)
		}
	}
}

func TestIntegrationDigestStabilityAcrossRenders(t *testing.T) {
	ledger, projections, episodes, _, _ := setupIntegrationStore(t)
	ctx := context.Background()

	evidence := seedIntegrationEvidence(t, ledger)

	segmenter := &singleEpisodeSegmenter{}
	eps, _ := episodes.RebuildSession(ctx, "conv-1", segmenter, "1.0.0", "digest-stability")

	anchors := []evalRankedAnchor{
		{CandidateID: evidence[0].ID, Rank: 1, Score: 0.95, TextDigest: evalTextDigest(evidence[0].Content), SourceIDs: []string{evidence[0].ID}},
		{CandidateID: eps[0].ID, Rank: 2, Score: 0.80, TextDigest: evalTextDigest(""), SourceIDs: []string{evidence[0].ID, evidence[1].ID}},
	}

	// Run each renderer three times — digest must be stable.
	for _, kind := range []RepresentationKind{ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode} {
		var renderer RepresentationRenderer
		switch kind {
		case ReprChunk900:
			renderer = NewChunk900Renderer(projections, ledger)
		case ReprRawTurnWindow:
			renderer = NewRawTurnWindowRenderer(projections, ledger, 2)
		case ReprSemanticEpisode:
			renderer = NewSemanticEpisodeRenderer(projections, ledger, episodes)
		}

		var firstDigest string
		for i := 0; i < 3; i++ {
			candidates, err := renderer.Render(ctx, anchors)
			if err != nil {
				t.Fatalf("%s Render iteration %d: %v", kind, i, err)
			}
			digest := renderedCandidateSetDigest(candidates)
			if i == 0 {
				firstDigest = digest
			} else if digest != firstDigest {
				t.Errorf("%s: digest changed on iteration %d: %s → %s", kind, i, firstDigest, digest)
			}
		}
	}
}
