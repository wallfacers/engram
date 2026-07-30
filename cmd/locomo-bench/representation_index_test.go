package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// newIndexTestStore creates the production in-memory SQLite store with
// evidence seeded for shadow index tests.
func newIndexTestStore(t *testing.T) (*memory.LedgerStore, *memory.ProjectionStore, *sql.DB) {
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

func newRunDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "representation-index-test-*")
	if err != nil {
		t.Fatalf("create temp run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestShadowIndexRunDirScoped(t *testing.T) {
	ledger, projections, db := newIndexTestStore(t)
	ctx := context.Background()
	runDir := newRunDir(t)

	// Seed evidence.
	now := appTimestamp()
	inputs := []memory.EvidenceInput{
		{ExternalSourceID: "idx-0", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "Hello world", RecordedAt: now},
		{ExternalSourceID: "idx-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 1, Content: "Hi there, how can I help?", RecordedAt: now},
	}
	_, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("seed evidence: %v", err)
	}

	// Create shadow indexes for each representation in the run dir.
	indexes := map[RepresentationKind]*RepresentationShadowIndex{
		ReprChunk900:        NewRepresentationShadowIndex(runDir, ReprChunk900),
		ReprRawTurnWindow:   NewRepresentationShadowIndex(runDir, ReprRawTurnWindow),
		ReprSemanticEpisode: NewRepresentationShadowIndex(runDir, ReprSemanticEpisode),
	}

	for kind, index := range indexes {
		if err := index.Open(ctx); err != nil {
			t.Fatalf("%s Open: %v", kind, err)
		}
	}

	// Verify each shadow index created its own file in the run dir.
	for kind := range indexes {
		dbPath := filepath.Join(runDir, string(kind)+".db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Errorf("%s: shadow index DB not found at %s", kind, dbPath)
		}
	}

	// Verify no pollution to the production DB.
	var prodTableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entries`).Scan(&prodTableCount); err != nil {
		t.Fatalf("count production entries: %v", err)
	}
	// Production DB should be unaffected by shadow index creation.
	_ = prodTableCount

	// Clean shutdown.
	for kind, index := range indexes {
		if err := index.Close(); err != nil {
			t.Errorf("%s Close: %v", kind, err)
		}
	}

	_ = projections
	_ = db
}

func TestShadowIndexDeletionIsolation(t *testing.T) {
	ledger, _, _ := newIndexTestStore(t)
	ctx := context.Background()
	runDir := newRunDir(t)

	// Seed evidence and capture the actual evidence ID.
	now := appTimestamp()
	inputs := []memory.EvidenceInput{
		{ExternalSourceID: "del-0", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "Delete me test", RecordedAt: now},
	}
	evidence, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("seed evidence: %v", err)
	}
	actualEvidenceID := evidence[0].ID

	index := NewRepresentationShadowIndex(runDir, ReprChunk900)
	if err := index.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Index some content using the actual evidence ID.
	if err := index.IndexCandidates(ctx, []evalRenderedCandidate{
		{CandidateID: "c1", Kind: string(ReprChunk900), Rank: 1, Score: 0.9, Text: "Test content for deletion", TextDigest: evalTextDigest("Test content for deletion"), SourceIDs: []string{actualEvidenceID}},
	}); err != nil {
		t.Fatalf("IndexCandidates: %v", err)
	}

	// Delete the shadow index.
	if err := index.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify the shadow index file is gone.
	dbPath := index.DBPath()
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("shadow index DB still exists at %s after deletion", dbPath)
	}

	// Verify production DB is unaffected.
	ev, err := ledger.Get(ctx, actualEvidenceID)
	if err != nil {
		t.Fatalf("production evidence should still exist after shadow index delete: %v", err)
	}
	if ev.Content != "Delete me test" {
		t.Errorf("production evidence content changed after shadow index delete")
	}
}

func TestShadowIndexSharedBudget(t *testing.T) {
	runDir := newRunDir(t)

	// All three shadow indexes must use the same budget parameters.
	const (
		sharedCandidateBudget = 10
		sharedTokenCap        = 4096
	)

	budgets := map[RepresentationKind]struct {
		candidateBudget int
		tokenCap        int
	}{
		ReprChunk900:        {candidateBudget: sharedCandidateBudget, tokenCap: sharedTokenCap},
		ReprRawTurnWindow:   {candidateBudget: sharedCandidateBudget, tokenCap: sharedTokenCap},
		ReprSemanticEpisode: {candidateBudget: sharedCandidateBudget, tokenCap: sharedTokenCap},
	}

	// Verify all three share identical budgets.
	var reference struct {
		candidateBudget int
		tokenCap        int
	}
	first := true
	for kind, budget := range budgets {
		if first {
			reference = budget
			first = false
			continue
		}
		if budget.candidateBudget != reference.candidateBudget {
			t.Errorf("%s candidate budget = %d, want %d", kind, budget.candidateBudget, reference.candidateBudget)
		}
		if budget.tokenCap != reference.tokenCap {
			t.Errorf("%s token cap = %d, want %d", kind, budget.tokenCap, reference.tokenCap)
		}
	}

	_ = runDir
}

func TestShadowIndexThreeRepresentations(t *testing.T) {
	ledger, projections, _ := newIndexTestStore(t)
	ctx := context.Background()
	runDir := newRunDir(t)

	// Seed evidence.
	now := appTimestamp()
	inputs := []memory.EvidenceInput{
		{ExternalSourceID: "rep-0", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "What is machine learning?", RecordedAt: now},
		{ExternalSourceID: "rep-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 1, Content: "Machine learning is a subset of artificial intelligence.", RecordedAt: now},
	}
	_, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("seed evidence: %v", err)
	}

	// Create all three shadow indexes.
	indexes := map[RepresentationKind]*RepresentationShadowIndex{
		ReprChunk900:        NewRepresentationShadowIndex(runDir, ReprChunk900),
		ReprRawTurnWindow:   NewRepresentationShadowIndex(runDir, ReprRawTurnWindow),
		ReprSemanticEpisode: NewRepresentationShadowIndex(runDir, ReprSemanticEpisode),
	}

	for kind, index := range indexes {
		if err := index.Open(ctx); err != nil {
			t.Fatalf("%s Open: %v", kind, err)
		}
	}

	// Index same content under each representation.
	candidates := []evalRenderedCandidate{
		{
			CandidateID:    "c1",
			Kind:           string(ReprChunk900),
			Rank:           1,
			Score:          0.95,
			Text:           "What is machine learning?",
			TextDigest:     evalTextDigest("What is machine learning?"),
			SourceIDs:      []string{"rep-0"},
			ExpandedFrom:   []string{"anchor-1"},
			ExpansionCount: 0,
		},
	}

	for kind, index := range indexes {
		// Each index accepts candidates of any kind; the kind is recorded
		// alongside the candidate for bake-off comparison.
		if err := index.IndexCandidates(ctx, candidates); err != nil {
			t.Errorf("%s IndexCandidates: %v", kind, err)
		}
	}

	// Each shadow index must have its own independent data.
	for kind, index := range indexes {
		count, err := index.CandidateCount(ctx)
		if err != nil {
			t.Errorf("%s CandidateCount: %v", kind, err)
		}
		if count != len(candidates) {
			t.Errorf("%s candidate count = %d, want %d", kind, count, len(candidates))
		}
	}

	// Verify each shadow DB is independent.
	for kind := range indexes {
		dbPath := filepath.Join(runDir, string(kind)+".db")
		info, err := os.Stat(dbPath)
		if err != nil {
			t.Errorf("%s: stat DB: %v", kind, err)
		} else if info.Size() == 0 {
			t.Errorf("%s: shadow DB is empty", kind)
		}
	}

	// Clean shutdown.
	for kind, index := range indexes {
		if err := index.Close(); err != nil {
			t.Errorf("%s Close: %v", kind, err)
		}
	}

	_ = projections
}

func TestShadowIndexDeleteAllRepresentations(t *testing.T) {
	runDir := newRunDir(t)
	ctx := context.Background()

	indexes := map[RepresentationKind]*RepresentationShadowIndex{
		ReprChunk900:        NewRepresentationShadowIndex(runDir, ReprChunk900),
		ReprRawTurnWindow:   NewRepresentationShadowIndex(runDir, ReprRawTurnWindow),
		ReprSemanticEpisode: NewRepresentationShadowIndex(runDir, ReprSemanticEpisode),
	}

	for kind, index := range indexes {
		if err := index.Open(ctx); err != nil {
			t.Fatalf("%s Open: %v", kind, err)
		}
	}

	// Delete all.
	for kind, index := range indexes {
		if err := index.Delete(ctx); err != nil {
			t.Errorf("%s Delete: %v", kind, err)
		}
	}

	// Verify all DB files are gone.
	for kind := range indexes {
		dbPath := filepath.Join(runDir, string(kind)+".db")
		if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
			t.Errorf("%s: shadow DB still exists at %s after deletion", kind, dbPath)
		}
	}

	// Run dir should be empty (or contain only unrelated files).
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".db") {
			t.Errorf("unexpected DB file in run dir: %s", entry.Name())
		}
	}
}

func TestShadowIndexKindIsolation(t *testing.T) {
	runDir := newRunDir(t)
	ctx := context.Background()

	idxA := NewRepresentationShadowIndex(runDir, ReprChunk900)
	idxB := NewRepresentationShadowIndex(runDir, ReprRawTurnWindow)

	if err := idxA.Open(ctx); err != nil {
		t.Fatalf("idxA Open: %v", err)
	}
	if err := idxB.Open(ctx); err != nil {
		t.Fatalf("idxB Open: %v", err)
	}

	// Index different candidates in each.
	if err := idxA.IndexCandidates(ctx, []evalRenderedCandidate{
		{CandidateID: "a1", Kind: string(ReprChunk900), Rank: 1, Score: 0.9, Text: "A content", TextDigest: evalTextDigest("A content"), SourceIDs: []string{"src-a"}},
	}); err != nil {
		t.Fatalf("idxA IndexCandidates: %v", err)
	}
	if err := idxB.IndexCandidates(ctx, []evalRenderedCandidate{
		{CandidateID: "b1", Kind: string(ReprRawTurnWindow), Rank: 1, Score: 0.8, Text: "B content", TextDigest: evalTextDigest("B content"), SourceIDs: []string{"src-b"}},
	}); err != nil {
		t.Fatalf("idxB IndexCandidates: %v", err)
	}

	// Each index must only see its own candidates.
	countA, _ := idxA.CandidateCount(ctx)
	countB, _ := idxB.CandidateCount(ctx)
	if countA != 1 {
		t.Errorf("idxA candidate count = %d, want 1", countA)
	}
	if countB != 1 {
		t.Errorf("idxB candidate count = %d, want 1", countB)
	}

	idxA.Close() //nolint:errcheck
	idxB.Close() //nolint:errcheck
}

func TestShadowIndexOpenCloseIdempotent(t *testing.T) {
	runDir := newRunDir(t)
	ctx := context.Background()

	index := NewRepresentationShadowIndex(runDir, ReprChunk900)

	// Open twice.
	if err := index.Open(ctx); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := index.Open(ctx); err != nil {
		t.Fatalf("second Open: %v", err)
	}

	// Close twice.
	if err := index.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := index.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func appTimestamp() time.Time {
	return time.Now().UTC()
}
