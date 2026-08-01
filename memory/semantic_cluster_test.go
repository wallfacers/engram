package memory_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// seedClusterEvidence seeds evidence across multiple sessions and returns them
// in seed order. Each turn carries an ordinal within its own session.
func seedClusterEvidence(t *testing.T, ledger *memory.LedgerStore, sessions map[string][]string) []memory.Evidence {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	var out []memory.Evidence
	for sessionID, contents := range sessions {
		inputs := make([]memory.EvidenceInput, len(contents))
		for i, content := range contents {
			inputs[i] = memory.EvidenceInput{
				ExternalSourceID: fmt.Sprintf("%s-turn-%d", sessionID, i),
				SourceType:       memory.EvidenceMessage,
				SourceSessionID:  sessionID,
				Speaker:          "alice",
				Ordinal:          i,
				Content:          content,
				RecordedAt:       now.Add(time.Duration(i) * time.Second),
			}
		}
		ev, err := ledger.AppendBatch(ctx, inputs)
		if err != nil {
			t.Fatalf("seed evidence for session %q: %v", sessionID, err)
		}
		out = append(out, ev...)
	}
	return out
}

func newClusterStore(t *testing.T) (*memory.LedgerStore, *memory.ProjectionStore, *memory.EpisodeStore, *sql.DB) {
	t.Helper()
	ledger, projections, db := newEpisodeStore(t)
	return ledger, projections, memory.NewEpisodeStore(db, ledger, projections), db
}

func evidenceIDs(evidence []memory.Evidence) []string {
	out := make([]string, len(evidence))
	for i, ev := range evidence {
		out[i] = ev.ID
	}
	return out
}

// TestSemanticClusterCrossSessionGroupsRelatedEvidence is the core US1 test:
// evidence from different sessions that share entities/topics must be clustered
// into a single episode (research.md R1/R3). It must FAIL before SemanticClusterer
// is implemented.
func TestSemanticClusterCrossSessionGroupsRelatedEvidence(t *testing.T) {
	ledger, _, _, _ := newClusterStore(t)
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Project launch postponed to next week", "Alpha confirmed the launch date"},
		"session-b": {"The launch is now next week per Alpha"},
		"session-c": {"What should we eat for lunch"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	clusters, err := clusterer.Cluster(context.Background(), ev)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}

	// The three launch-related evidence (across session-a and session-b) must
	// land together; the unrelated lunch turn must not join them.
	var found []memory.EpisodeCluster
	for _, c := range clusters {
		if len(c.EvidenceIDs) >= 3 {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one episode of >=3 related evidence across sessions, got %d clusters: %+v", len(found), clusters)
	}
	ids := found[0].EvidenceIDs
	if !containsEvidence(ids, ev[2].ID) { // session-b launch turn
		t.Fatalf("cross-session related evidence not grouped: %v", ids)
	}
	if containsEvidence(ids, ev[3].ID) { // lunch turn
		t.Fatalf("unrelated evidence leaked into episode: %v", ids)
	}
}

// TestSemanticClusterUnrelatedNotGrouped: evidence with no shared entity/keyword
// must not be clustered together.
func TestSemanticClusterUnrelatedNotGrouped(t *testing.T) {
	ledger, _, _, _ := newClusterStore(t)
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Alpha booked a flight to Berlin"},
		"session-b": {"What time is the standup tomorrow"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	clusters, err := clusterer.Cluster(context.Background(), ev)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected no cluster for unrelated evidence, got %d: %+v", len(clusters), clusters)
	}
}

// TestSemanticClusterBoundConstrained: when related evidence exceeds the per-episode
// cap, the cluster must be deterministically truncated (research.md R4).
func TestSemanticClusterBoundConstrained(t *testing.T) {
	ledger, _, _, _ := newClusterStore(t)
	contents := make([]string, 5)
	for i := range contents {
		contents[i] = fmt.Sprintf("Alpha project status update iteration %d", i)
	}
	ev := seedClusterEvidence(t, ledger, map[string][]string{"session-a": contents})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{MaxEvidencePerEpisode: 2})
	clusters, err := clusterer.Cluster(context.Background(), ev)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	for _, c := range clusters {
		if len(c.EvidenceIDs) > 2 {
			t.Fatalf("episode exceeded cap: got %d evidence", len(c.EvidenceIDs))
		}
	}
	// Deterministic: the first two evidence (by seed order/ordinal) survive.
	if len(clusters) > 0 && len(clusters[0].EvidenceIDs) == 2 {
		if clusters[0].EvidenceIDs[0] != ev[0].ID || clusters[0].EvidenceIDs[1] != ev[1].ID {
			t.Fatalf("truncation not deterministic by ordinal: %+v", clusters[0].EvidenceIDs)
		}
	}
}

// TestSemanticClusterOfflineNoEmbeddingRequired: clustering must work with no
// embedding endpoint (constitution V / SC-005). It exercises NewOfflineClusterer
// which must not need any embedder.
func TestSemanticClusterOfflineNoEmbeddingRequired(t *testing.T) {
	ledger, _, _, _ := newClusterStore(t)
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Release v2.0 deployed to production"},
		"session-b": {"v2.0 is live in production"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	clusters, err := clusterer.Cluster(context.Background(), ev)
	if err != nil {
		t.Fatalf("offline cluster: %v", err)
	}
	if len(clusters) == 0 {
		t.Fatal("expected offline clustering to succeed on related evidence")
	}
}

// TestSemanticClusterSignalAttributed: each cluster records the signal that
// triggered it (entity or keyword) for audit (FR-006 / research.md R3).
func TestSemanticClusterSignalAttributed(t *testing.T) {
	ledger, _, _, _ := newClusterStore(t)
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Alpha approved the budget"},
		"session-b": {"Alpha signed off the budget"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	clusters, err := clusterer.Cluster(context.Background(), ev)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected one cluster, got %d", len(clusters))
	}
	if clusters[0].Signal == "" {
		t.Fatal("cluster signal not recorded")
	}
}

// TestSemanticClusterEmptyInput: no evidence yields no clusters.
func TestSemanticClusterEmptyInput(t *testing.T) {
	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	clusters, err := clusterer.Cluster(context.Background(), nil)
	if err != nil {
		t.Fatalf("Cluster: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected no clusters for empty input, got %d", len(clusters))
	}
}

func containsEvidence(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// TestEpisodeRebuildAllCrossSession constructs a semantic_episode projection from
// evidence across two sessions via EpisodeStore.RebuildAll and asserts lineage
// source_order is contiguous (research.md R2, FR-005).
func TestEpisodeRebuildAllCrossSession(t *testing.T) {
	ledger, projections, episodes, _ := newClusterStore(t)
	_ = projections
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Project launch postponed to next week"},
		"session-b": {"Launch is now next week"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	projs, err := episodes.RebuildAll(context.Background(), clusterer, "1.0.0", "cross-session-hash")
	if err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}
	if len(projs) != 1 {
		t.Fatalf("expected 1 episode projection, got %d: %+v", len(projs), projs)
	}
	if projs[0].Kind != memory.ProjectionSemanticEpisode {
		t.Fatalf("expected semantic_episode projection, got %s", projs[0].Kind)
	}
	// Both cross-session evidence must be in lineage.
	sources, err := projections.SourcesByProjectionIDs(context.Background(), []string{projs[0].ID})
	if err != nil {
		t.Fatalf("SourcesByProjectionIDs: %v", err)
	}
	refs := sources[projs[0].ID]
	if len(refs) != 2 {
		t.Fatalf("expected 2 lineage sources, got %d: %+v", len(refs), refs)
	}
	for i, ref := range refs {
		if ref.SourceOrder != i {
			t.Fatalf("source_order not contiguous: got %d at position %d", ref.SourceOrder, i)
		}
	}
	if !containsEvidence(evidenceIDs(ev), refs[0].EvidenceID) || !containsEvidence(evidenceIDs(ev), refs[1].EvidenceID) {
		t.Fatalf("lineage evidence not from seeded set: %v", refs)
	}
}

// TestEpisodeRebuildAllIdempotentSameConfig: rebuilding with the same config hash
// deletes old episodes and rebuilds; projection set is identical (research.md R2).
func TestEpisodeRebuildAllIdempotentSameConfig(t *testing.T) {
	ledger, projections, episodes, _ := newClusterStore(t)
	seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Alpha project status update"},
		"session-b": {"Alpha project status update again"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	first, err := episodes.RebuildAll(context.Background(), clusterer, "1.0.0", "idem-hash")
	if err != nil {
		t.Fatalf("first RebuildAll: %v", err)
	}
	second, err := episodes.RebuildAll(context.Background(), clusterer, "1.0.0", "idem-hash")
	if err != nil {
		t.Fatalf("second RebuildAll: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("idempotent rebuild changed count: %d vs %d", len(first), len(second))
	}
	// Rebuild deletes old projections and assigns fresh ULIDs, so IDs differ.
	// Idempotence means the second rebuild produces the same number of projections
	// whose current lineage covers all seeded evidence (research.md R2). The first
	// projection set is gone after the second rebuild — do not query it.
	sources, err := projections.SourcesByProjectionIDs(context.Background(), projIDs(second))
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	var rebuilt []string
	for _, refs := range sources {
		for _, ref := range refs {
			rebuilt = append(rebuilt, ref.EvidenceID)
		}
	}
	if len(rebuilt) != 2 {
		t.Fatalf("expected rebuilt lineage to cover 2 evidence, got %v", rebuilt)
	}
}

// TestEpisodeRebuildAllEmptyEvidenceNoOp: no active evidence yields no episodes
// and no error (no-op, research.md R2).
func TestEpisodeRebuildAllEmptyEvidenceNoOp(t *testing.T) {
	ledger, _, episodes, _ := newClusterStore(t)
	_ = ledger
	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	projs, err := episodes.RebuildAll(context.Background(), clusterer, "1.0.0", "empty-hash")
	if err != nil {
		t.Fatalf("RebuildAll on empty store: %v", err)
	}
	if len(projs) != 0 {
		t.Fatalf("expected no projections for empty store, got %d", len(projs))
	}
}

// TestEpisodeRebuildAllDeletePreservesEvidence: deleting an episode projection
// must not delete the underlying Evidence (FR-005).
func TestEpisodeRebuildAllDeletePreservesEvidence(t *testing.T) {
	ledger, _, episodes, _ := newClusterStore(t)
	ev := seedClusterEvidence(t, ledger, map[string][]string{
		"session-a": {"Alpha project status update"},
		"session-b": {"Alpha project status update again"},
	})

	clusterer := memory.NewOfflineClusterer(memory.ClusterOptions{})
	projs, err := episodes.RebuildAll(context.Background(), clusterer, "1.0.0", "del-hash")
	if err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}
	if err := episodes.DeleteByConfig(context.Background(), "del-hash"); err != nil {
		t.Fatalf("DeleteByConfig: %v", err)
	}
	for _, id := range evidenceIDs(ev) {
		if _, err := ledger.Get(context.Background(), id); err != nil {
			t.Fatalf("evidence %s lost after episode delete: %v", id, err)
		}
	}
	_ = projs
}

func projIDs(projs []memory.Projection) []string {
	out := make([]string, len(projs))
	for i, p := range projs {
		out[i] = p.ID
	}
	return out
}
