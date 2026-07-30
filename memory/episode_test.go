package memory_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func newEpisodeStore(t *testing.T) (*memory.LedgerStore, *memory.ProjectionStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	db := s.DB()
	return memory.NewLedgerStore(db), memory.NewProjectionStore(db), db
}

// fixedSegmenter returns boundaries from a precomputed list, ignoring input.
type fixedSegmenter struct {
	boundaries []memory.EpisodeBoundary
	err        error
}

func (s *fixedSegmenter) Segment(_ context.Context, _ []memory.Evidence) ([]memory.EpisodeBoundary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.boundaries, nil
}

// singleEpisodeSegmenter wraps the entire session as one episode.
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

func seedSessionEvidence(t *testing.T, ledger *memory.LedgerStore, sessionID string, turns []struct {
	speaker string
	content string
}) []memory.Evidence {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	inputs := make([]memory.EvidenceInput, len(turns))
	for i, turn := range turns {
		inputs[i] = memory.EvidenceInput{
			ExternalSourceID: fmt.Sprintf("%s-turn-%d", sessionID, i),
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  sessionID,
			Speaker:          turn.speaker,
			Ordinal:          i,
			Content:          turn.content,
			RecordedAt:       now.Add(time.Duration(i) * time.Second),
		}
	}
	evidence, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("seed evidence for session %q: %v", sessionID, err)
	}
	return evidence
}

func TestEpisodeSegmenterSameSessionContinuousBoundary(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	evidence := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Hello, how are you?"},
		{"assistant", "I'm doing well, thanks!"},
		{"user", "What's the weather like?"},
		{"assistant", "It's sunny today."},
	})

	// Segment into two episodes: [0,1] and [2,3].
	segmenter := &fixedSegmenter{
		boundaries: []memory.EpisodeBoundary{
			{FirstEvidenceID: evidence[0].ID, LastEvidenceID: evidence[1].ID},
			{FirstEvidenceID: evidence[2].ID, LastEvidenceID: evidence[3].ID},
		},
	}

	store := memory.NewEpisodeStore(db, ledger, projections)
	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "test-config-hash")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d episodes, want 2", len(eps))
	}
	for _, ep := range eps {
		if ep.Kind != memory.ProjectionSemanticEpisode {
			t.Errorf("episode kind = %q, want %q", ep.Kind, memory.ProjectionSemanticEpisode)
		}
		if ep.State != "active" {
			t.Errorf("episode state = %q, want active", ep.State)
		}
	}

	// Verify sources were stored.
	sources, err := projections.SourcesByProjectionIDs(ctx, []string{eps[0].ID, eps[1].ID})
	if err != nil {
		t.Fatalf("SourcesByProjectionIDs: %v", err)
	}
	if len(sources[eps[0].ID]) != 2 {
		t.Errorf("episode 0 has %d sources, want 2", len(sources[eps[0].ID]))
	}
	if len(sources[eps[1].ID]) != 2 {
		t.Errorf("episode 1 has %d sources, want 2", len(sources[eps[1].ID]))
	}

	// Verify narratives are read back.
	var n0, n1 string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, eps[0].ID).Scan(&n0); err != nil {
		t.Fatalf("read episode 0 narrative: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, eps[1].ID).Scan(&n1); err != nil {
		t.Fatalf("read episode 1 narrative: %v", err)
	}
	if !strings.Contains(n0, "Hello, how are you?") || !strings.Contains(n0, "I'm doing well") {
		t.Errorf("episode 0 narrative missing expected content: %q", n0)
	}
	if !strings.Contains(n1, "What's the weather") || !strings.Contains(n1, "It's sunny") {
		t.Errorf("episode 1 narrative missing expected content: %q", n1)
	}
}

func TestEpisodeNarrativeIsDeterministic(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	evidence := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Message one."},
		{"assistant", "Message two."},
	})

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	first, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "config-hash")
	if err != nil {
		t.Fatalf("first RebuildSession: %v", err)
	}

	// Read the first narrative before it gets deleted.
	var n1 string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, first[0].ID).Scan(&n1); err != nil {
		t.Fatalf("read first narrative: %v", err)
	}

	// Rebuild with same config (deletes old episodes inside the same tx).
	second, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "config-hash")
	if err != nil {
		t.Fatalf("second RebuildSession: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("rebuild count changed: %d → %d", len(first), len(second))
	}

	var n2 string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, second[0].ID).Scan(&n2); err != nil {
		t.Fatalf("read second narrative: %v", err)
	}
	if n1 != n2 {
		t.Errorf("narratives differ after rebuild:\nfirst:  %q\nsecond: %q", n1, n2)
	}

	// Verify the config-hash is embedded in the projection.
	if second[0].ConfigHash != "config-hash" {
		t.Errorf("config hash = %q, want config-hash", second[0].ConfigHash)
	}
	if second[0].Builder != "episode" {
		t.Errorf("builder = %q, want episode", second[0].Builder)
	}
	if second[0].BuilderVersion != "1.0.0" {
		t.Errorf("builder version = %q, want 1.0.0", second[0].BuilderVersion)
	}

	// Verify the evidence is unchanged after rebuild.
	ev, err := ledger.Get(ctx, evidence[0].ID)
	if err != nil {
		t.Fatalf("get evidence after rebuild: %v", err)
	}
	if ev.Content != "Message one." {
		t.Errorf("evidence content changed after episode rebuild: %q", ev.Content)
	}
}

func TestEpisodeDeletePreservesEvidence(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	evidence := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Important data."},
		{"assistant", "More important data."},
	})

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "delete-test-hash")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}

	// Delete the episode.
	if err := store.DeleteByConfig(ctx, "delete-test-hash"); err != nil {
		t.Fatalf("DeleteByConfig: %v", err)
	}

	// Evidence must still be active.
	for _, ev := range evidence {
		got, err := ledger.Get(ctx, ev.ID)
		if err != nil {
			t.Fatalf("evidence %q should still exist after episode delete: %v", ev.ID, err)
		}
		if got.State != memory.EvidenceActive {
			t.Errorf("evidence %q state = %q, want active", ev.ID, got.State)
		}
		if got.Content != ev.Content {
			t.Errorf("evidence %q content changed", ev.ID)
		}
	}

	// Episode payload must be gone.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_semantic_episodes`).Scan(&count); err != nil {
		t.Fatalf("count episodes: %v", err)
	}
	if count != 0 {
		t.Errorf("episodes remaining after delete: %d, want 0", count)
	}

	// Projection registry must be clean for episodes.
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_projections WHERE kind = 'semantic_episode'`).Scan(&count); err != nil {
		t.Fatalf("count episode projections: %v", err)
	}
	if count != 0 {
		t.Errorf("episode projections remaining after delete: %d, want 0", count)
	}

	// Projection sources must be gone.
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_projection_sources`).Scan(&count); err != nil {
		t.Fatalf("count projection sources: %v", err)
	}
	if count != 0 {
		t.Errorf("projection sources remaining after episode delete: %d, want 0", count)
	}
}

func TestEpisodeSegmenterFailureDegradesGracefully(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	_ = seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Some content."},
	})

	t.Run("nil segmenter", func(t *testing.T) {
		store := memory.NewEpisodeStore(db, ledger, projections)
		_, err := store.RebuildSession(ctx, "session-a", nil, "1.0.0", "nil-test")
		if err == nil {
			t.Fatal("expected error with nil segmenter")
		}
		if !errors.Is(err, memory.ErrEpisodeSegmenterRequired) {
			t.Errorf("error = %v, want ErrEpisodeSegmenterRequired", err)
		}
	})

	t.Run("segmenter returns error", func(t *testing.T) {
		store := memory.NewEpisodeStore(db, ledger, projections)
		segmenter := &fixedSegmenter{err: errors.New("segmenter unavailable")}
		_, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "err-test")
		if err == nil {
			t.Fatal("expected error from failing segmenter")
		}
	})

	t.Run("Ledger and search remain unaffected after segmenter failure", func(t *testing.T) {
		// Evidence must still be queryable.
		ev, err := ledger.ListSession(ctx, "session-a", false)
		if err != nil {
			t.Fatalf("ListSession after segmenter failure: %v", err)
		}
		if len(ev) != 1 {
			t.Errorf("expected 1 evidence, got %d", len(ev))
		}
	})
}

func TestEpisodeBoundaryMustBeContinuousWithinSession(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	// Two different sessions.
	evA := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Session A turn 0."},
		{"assistant", "Session A turn 1."},
	})
	evB := seedSessionEvidence(t, ledger, "session-b", []struct {
		speaker string
		content string
	}{
		{"user", "Session B turn 0."},
	})

	// Boundary that crosses sessions is invalid.
	segmenter := &fixedSegmenter{
		boundaries: []memory.EpisodeBoundary{
			{FirstEvidenceID: evA[0].ID, LastEvidenceID: evB[0].ID},
		},
	}
	store := memory.NewEpisodeStore(db, ledger, projections)
	_, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "cross-session")
	if err == nil {
		t.Fatal("expected error for cross-session boundary")
	}
}

func TestEpisodeBoundaryMustHaveContinuousOrdinals(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	evidence := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Turn 0."},
		{"assistant", "Turn 1 (will be tombstoned)."},
		{"user", "Turn 2."},
	})

	// Tombstone the middle evidence to create an ordinal gap in active session.
	if err := ledger.Tombstone(ctx, memory.LifecycleRequest{
		EvidenceID: evidence[1].ID,
		ReasonCode: "test-gap",
	}); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	// Active session now has ordinals 0 and 2 — the boundary spanning 0→2
	// has a gap (ordinal 1 is missing).
	segmenter := &fixedSegmenter{
		boundaries: []memory.EpisodeBoundary{
			{FirstEvidenceID: evidence[0].ID, LastEvidenceID: evidence[2].ID},
		},
	}
	store := memory.NewEpisodeStore(db, ledger, projections)
	_, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "skip-ordinal")
	if err == nil {
		t.Fatal("expected error for non-contiguous ordinals")
	}
	if !errors.Is(err, memory.ErrEpisodeNotContinuous) {
		t.Errorf("error = %v, want ErrEpisodeNotContinuous", err)
	}
}

func TestEpisodeAtLeastOneSource(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	_ = seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Only turn."},
	})

	// Empty boundary list is fine — no episodes to create.
	segmenter := &fixedSegmenter{boundaries: nil}
	store := memory.NewEpisodeStore(db, ledger, projections)
	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "empty-boundary")
	if err != nil {
		t.Fatalf("RebuildSession with empty boundaries: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("expected 0 episodes from empty boundary list, got %d", len(eps))
	}
}

func TestEpisodeNarrativeIncludesSpeakerLabels(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	_ = seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"alice", "Hi from Alice."},
		{"bob", "Hi from Bob."},
	})

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "speaker-test")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}

	var narrative string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, eps[0].ID).Scan(&narrative); err != nil {
		t.Fatalf("read narrative: %v", err)
	}

	if !strings.Contains(narrative, "alice:") {
		t.Errorf("narrative missing 'alice:' label: %q", narrative)
	}
	if !strings.Contains(narrative, "bob:") {
		t.Errorf("narrative missing 'bob:' label: %q", narrative)
	}
}

func TestEpisodeConfigHashIsolation(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	_ = seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Shared evidence."},
	})

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	// Build with config A.
	epsA, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "config-A")
	if err != nil {
		t.Fatalf("RebuildSession config-A: %v", err)
	}

	// Build with config B (different config hash).
	epsB, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "config-B")
	if err != nil {
		t.Fatalf("RebuildSession config-B: %v", err)
	}

	if len(epsA) != 1 || len(epsB) != 1 {
		t.Fatalf("expected 1 episode each, got %d and %d", len(epsA), len(epsB))
	}

	// Delete config A only.
	if err := store.DeleteByConfig(ctx, "config-A"); err != nil {
		t.Fatalf("DeleteByConfig config-A: %v", err)
	}

	// Config B episodes must survive.
	sources, err := projections.SourcesByProjectionIDs(ctx, []string{epsB[0].ID})
	if err != nil {
		t.Fatalf("SourcesByProjectionIDs for config-B: %v", err)
	}
	if len(sources[epsB[0].ID]) == 0 {
		t.Error("config-B episode sources deleted when config-A was removed")
	}

	// Evidence must still be intact.
	ev, err := ledger.ListSession(ctx, "session-a", false)
	if err != nil {
		t.Fatalf("ListSession: %v", err)
	}
	if len(ev) != 1 {
		t.Errorf("expected 1 evidence after config-A delete, got %d", len(ev))
	}
}

func TestEpisodeRebuildIdempotentWithSameConfig(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	_ = seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Idempotent test."},
	})

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	first, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "idempotent-hash")
	if err != nil {
		t.Fatalf("first rebuild: %v", err)
	}

	// Capture first narrative before rebuild deletes it.
	var n1 string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, first[0].ID).Scan(&n1); err != nil {
		t.Fatalf("read first narrative: %v", err)
	}

	// Rebuild with same config (deletes old episodes inside same tx).
	second, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "idempotent-hash")
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("rebuild count changed: %d → %d", len(first), len(second))
	}

	var n2 string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, second[0].ID).Scan(&n2); err != nil {
		t.Fatalf("read second narrative: %v", err)
	}

	h1 := fmt.Sprintf("%x", sha256.Sum256([]byte(n1)))
	h2 := fmt.Sprintf("%x", sha256.Sum256([]byte(n2)))
	if h1 != h2 {
		t.Errorf("narrative digest changed across idempotent rebuild: %s → %s", h1, h2)
	}

	// Should only have one set of episode projections.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_projections WHERE kind = 'semantic_episode' AND config_hash = 'idempotent-hash'`).Scan(&count); err != nil {
		t.Fatalf("count episodes: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 episode for config, got %d", count)
	}
}

func TestEpisodeCharCountAndTimestamps(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	occurredEarly := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	occurredLate := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	inputs := []memory.EvidenceInput{
		{
			ExternalSourceID: "early",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "user",
			Ordinal:          0,
			Content:          "Early message.",
			OccurredAt:       &occurredEarly,
			RecordedAt:       now,
		},
		{
			ExternalSourceID: "late",
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  "session-a",
			Speaker:          "assistant",
			Ordinal:          1,
			Content:          "Late message here.",
			OccurredAt:       &occurredLate,
			RecordedAt:       now,
		},
	}
	evidence, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	segmenter := &singleEpisodeSegmenter{}
	store := memory.NewEpisodeStore(db, ledger, projections)

	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "timestamp-test")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}

	var narrative string
	var startedAt, endedAt sql.NullInt64
	var charCount int
	if err := db.QueryRowContext(ctx, `
		SELECT narrative, started_at, ended_at, char_count
		FROM memory_semantic_episodes WHERE projection_id = ?`, eps[0].ID).Scan(&narrative, &startedAt, &endedAt, &charCount); err != nil {
		t.Fatalf("read episode payload: %v", err)
	}

	if !startedAt.Valid || !endedAt.Valid {
		t.Error("started_at and ended_at must be set")
	}
	if startedAt.Valid && endedAt.Valid && startedAt.Int64 > endedAt.Int64 {
		t.Error("started_at must be <= ended_at")
	}
	// Char count should be at least the sum of source content lengths.
	if charCount < len("Early message.")+len("Late message here.") {
		t.Errorf("char_count = %d, too small for combined source content", charCount)
	}

	_ = evidence // used for assertions above
}

func TestEpisodeEmptySessionReturnsNoEpisodes(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	store := memory.NewEpisodeStore(db, ledger, projections)
	segmenter := &singleEpisodeSegmenter{}
	eps, err := store.RebuildSession(ctx, "nonexistent-session", segmenter, "1.0.0", "empty-test")
	if err != nil {
		t.Fatalf("RebuildSession on empty session: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("expected 0 episodes for empty session, got %d", len(eps))
	}
}

func TestEpisodeTombstonedEvidenceExcluded(t *testing.T) {
	ledger, projections, db := newEpisodeStore(t)
	ctx := context.Background()

	evidence := seedSessionEvidence(t, ledger, "session-a", []struct {
		speaker string
		content string
	}{
		{"user", "Active turn."},
		{"assistant", "Tombstoned turn."},
	})

	// Tombstone the second piece of evidence.
	if err := ledger.Tombstone(ctx, memory.LifecycleRequest{
		EvidenceID: evidence[1].ID,
		ReasonCode: "test",
	}); err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	// EpisodeSegmenter only receives active evidence from ListSession.
	// The segmenter should only see evidence[0].
	store := memory.NewEpisodeStore(db, ledger, projections)
	segmenter := &singleEpisodeSegmenter{}
	eps, err := store.RebuildSession(ctx, "session-a", segmenter, "1.0.0", "tombstone-test")
	if err != nil {
		t.Fatalf("RebuildSession: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}

	var narrative string
	if err := db.QueryRowContext(ctx, `SELECT narrative FROM memory_semantic_episodes WHERE projection_id = ?`, eps[0].ID).Scan(&narrative); err != nil {
		t.Fatalf("read narrative: %v", err)
	}
	if strings.Contains(narrative, "Tombstoned turn") {
		t.Errorf("narrative contains tombstoned evidence: %q", narrative)
	}
	if !strings.Contains(narrative, "Active turn") {
		t.Errorf("narrative missing active evidence: %q", narrative)
	}
}
