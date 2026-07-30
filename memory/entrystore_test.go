package memory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func newEntryStore(t *testing.T) (*memory.EntryStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return memory.NewEntryStore(s.DB()), s.DB()
}

func mustEntry(t *testing.T, es *memory.EntryStore, name string) *memory.Entry {
	t.Helper()
	entry, err := es.GetByName(context.Background(), name)
	if err != nil {
		t.Fatalf("get %q: %v", name, err)
	}
	return entry
}

func ftsCount(t *testing.T, db *sql.DB, match string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM memory_entries_fts WHERE memory_entries_fts MATCH ?`, match).Scan(&n); err != nil {
		t.Fatalf("fts count match %q: %v", match, err)
	}
	return n
}

func TestUpsertInsertThenConflictUpdate(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	e := &memory.Entry{Name: "alpha", Trigger: "t1", Content: "hello world", Category: "user", CharCount: 11}
	if err := es.Upsert(ctx, e); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if e.ID == "" {
		t.Fatal("expected ID to be assigned")
	}
	if e.CreatedAt.IsZero() || e.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to be set")
	}

	got, err := es.GetByName(ctx, "alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	origCreated := got.CreatedAt
	if got.LastUsedAt != nil {
		t.Fatalf("expected nil LastUsedAt on fresh entry, got %v", got.LastUsedAt)
	}
	if got.Durability != "volatile" {
		t.Fatalf("expected default durability volatile, got %q", got.Durability)
	}

	// Ensure updated_at advances on conflict update.
	time.Sleep(2 * time.Millisecond)
	upd := &memory.Entry{Name: "alpha", Trigger: "t2", Content: "goodbye world", Category: "project", CharCount: 13}
	if err := es.Upsert(ctx, upd); err != nil {
		t.Fatalf("conflict upsert: %v", err)
	}

	if c, _ := es.Count(ctx); c != 1 {
		t.Fatalf("expected 1 row after conflict upsert, got %d", c)
	}
	got2, err := es.GetByName(ctx, "alpha")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got2.Trigger != "t2" || got2.Content != "goodbye world" || got2.Category != "project" {
		t.Fatalf("conflict update did not replace fields: %+v", got2)
	}
	if !got2.CreatedAt.Equal(origCreated) {
		t.Fatalf("created_at should be preserved: was %v now %v", origCreated, got2.CreatedAt)
	}
	if !got2.UpdatedAt.After(origCreated) {
		t.Fatalf("updated_at should advance past created_at: created %v updated %v", origCreated, got2.UpdatedAt)
	}
}

func TestUpsertCreatesSelfEvidenceAndKeepsPriorEvidenceAppendOnly(t *testing.T) {
	es, db := newEntryStore(t)
	ledger := memory.NewLedgerStore(db)
	ctx := context.Background()
	entry := &memory.Entry{Name: "self-evidence", Content: "first direct fact", CharCount: 17}
	if err := es.Upsert(ctx, entry); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	firstRefs, err := es.SourceRefs(ctx, entry.ID)
	if err != nil {
		t.Fatalf("first source refs: %v", err)
	}
	if len(firstRefs) != 1 || !firstRefs[0].FullSource {
		t.Fatalf("first source refs = %+v, want one full self source", firstRefs)
	}
	firstEvidence, err := ledger.Get(ctx, firstRefs[0].EvidenceID)
	if err != nil {
		t.Fatalf("get first self evidence: %v", err)
	}
	if firstEvidence.SourceType != memory.EvidenceDirectWrite || firstEvidence.Content != "first direct fact" {
		t.Fatalf("first self evidence = %+v", firstEvidence)
	}

	if err := es.Upsert(ctx, &memory.Entry{Name: "self-evidence", Content: "first direct fact", CharCount: 17}); err != nil {
		t.Fatalf("same-content upsert: %v", err)
	}
	sameRefs, err := es.SourceRefs(ctx, entry.ID)
	if err != nil {
		t.Fatalf("same-content source refs: %v", err)
	}
	if len(sameRefs) != 1 || sameRefs[0].EvidenceID != firstRefs[0].EvidenceID {
		t.Fatalf("same-content upsert changed self evidence: first=%+v same=%+v", firstRefs, sameRefs)
	}

	if err := es.Upsert(ctx, &memory.Entry{Name: "self-evidence", Content: "changed direct fact", CharCount: 19}); err != nil {
		t.Fatalf("changed-content upsert: %v", err)
	}
	changedRefs, err := es.SourceRefs(ctx, entry.ID)
	if err != nil {
		t.Fatalf("changed-content source refs: %v", err)
	}
	if len(changedRefs) != 1 || changedRefs[0].EvidenceID == firstRefs[0].EvidenceID {
		t.Fatalf("changed-content upsert did not append new self evidence: first=%+v changed=%+v", firstRefs, changedRefs)
	}
	if _, err := ledger.Get(ctx, firstRefs[0].EvidenceID); err != nil {
		t.Fatalf("prior direct evidence was not retained: %v", err)
	}

	if err := es.Delete(ctx, "self-evidence"); err != nil {
		t.Fatalf("delete fact projection: %v", err)
	}
	if _, err := ledger.Get(ctx, changedRefs[0].EvidenceID); err != nil {
		t.Fatalf("delete fact projection deleted source evidence: %v", err)
	}
	var projectionCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_projections WHERE kind = 'atomic_fact' AND object_key = ?`, entry.ID).Scan(&projectionCount); err != nil {
		t.Fatalf("count deleted fact projection: %v", err)
	}
	if projectionCount != 0 {
		t.Fatalf("deleted fact still has %d projection rows", projectionCount)
	}
}

func TestUpsertWithSourcesRequiresActiveSourcesAndPreservesCompleteLineage(t *testing.T) {
	es, db := newEntryStore(t)
	ledger := memory.NewLedgerStore(db)
	ctx := context.Background()
	inputs := []memory.EvidenceInput{
		{
			ExternalSourceID: "turn-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a",
			Speaker: "user", Ordinal: 0, Content: "Alice moved on Monday", RecordedAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		},
		{
			ExternalSourceID: "turn-2", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a",
			Speaker: "assistant", Ordinal: 1, Content: "The move was to Berlin", RecordedAt: time.Date(2026, 7, 30, 12, 1, 0, 0, time.UTC),
		},
	}
	sources, err := ledger.AppendBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("append supporting evidence: %v", err)
	}
	entry := &memory.Entry{Name: "alice-move", Content: "Alice moved to Berlin on Monday", CharCount: 32}
	refs := []memory.EvidenceRef{
		{EvidenceID: sources[0].ID, SourceOrder: 0, FullSource: true},
		{EvidenceID: sources[1].ID, SourceOrder: 1, FullSource: true},
	}
	if err := es.UpsertWithSources(ctx, entry, refs); err != nil {
		t.Fatalf("upsert with sources: %v", err)
	}
	got, err := es.SourceRefs(ctx, entry.ID)
	if err != nil {
		t.Fatalf("get explicit source refs: %v", err)
	}
	if len(got) != 2 || got[0].EvidenceID != sources[0].ID || got[1].EvidenceID != sources[1].ID {
		t.Fatalf("explicit direct lineage = %+v, want %+v", got, refs)
	}

	bad := &memory.Entry{Name: "must-not-exist", Content: "no active evidence"}
	err = es.UpsertWithSources(ctx, bad, []memory.EvidenceRef{{EvidenceID: "unknown-source", SourceOrder: 0, FullSource: true}})
	if err == nil {
		t.Fatal("upsert with unknown source unexpectedly succeeded")
	}
	if _, err := es.GetByName(ctx, bad.Name); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown-source write leaked an entry: %v", err)
	}
}

func TestRevisionAdvancesWhenTimestampRepeatsAndRejectsStaleDelete(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	fixed := time.Unix(1_700_000_000, 123_000).UTC()

	if err := es.Upsert(ctx, &memory.Entry{
		Name: "same-clock", Content: "old", CreatedAt: fixed, UpdatedAt: fixed,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	before, err := es.GetByName(ctx, "same-clock")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if before.Revision < 1 {
		t.Fatalf("initial revision = %d, want >= 1", before.Revision)
	}

	// A caller-supplied timestamp can repeat exactly. The persisted revision
	// must still advance so background work cannot mistake this rewrite for the
	// snapshot it judged.
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "same-clock", Content: "new", UpdatedAt: fixed,
	}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	after, err := es.GetByName(ctx, "same-clock")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Revision <= before.Revision {
		t.Fatalf("revision did not advance: before=%d after=%d", before.Revision, after.Revision)
	}
	applied, err := es.DeleteIfUnchanged(ctx, "same-clock", memory.EntryRevision{
		ID: before.ID, Revision: before.Revision,
	})
	if err != nil {
		t.Fatalf("conditional delete: %v", err)
	}
	if applied {
		t.Fatal("stale delete applied after a same-timestamp rewrite")
	}
	if got, err := es.GetByName(ctx, "same-clock"); err != nil || got.Content != "new" {
		t.Fatalf("rewritten entry changed: got=%+v err=%v", got, err)
	}
}

func TestSupersedeIfUnchangedValidatesBothRevisions(t *testing.T) {
	for _, changed := range []string{"loser", "winner"} {
		t.Run(changed, func(t *testing.T) {
			es, _ := newEntryStore(t)
			ctx := context.Background()
			fixed := time.Unix(1_700_000_000, 123_000).UTC()
			for _, entry := range []*memory.Entry{
				{Name: "loser", Content: "old fact", UpdatedAt: fixed},
				{Name: "winner", Content: "new fact", UpdatedAt: fixed},
			} {
				if err := es.Upsert(ctx, entry); err != nil {
					t.Fatalf("seed %s: %v", entry.Name, err)
				}
			}
			loser := mustEntry(t, es, "loser")
			winner := mustEntry(t, es, "winner")

			if err := es.Upsert(ctx, &memory.Entry{
				Name: changed, Content: "interactive rewrite", UpdatedAt: fixed,
			}); err != nil {
				t.Fatalf("rewrite %s: %v", changed, err)
			}
			applied, err := es.SupersedeIfUnchanged(
				ctx,
				"loser",
				"winner",
				memory.EntryRevision{ID: loser.ID, Revision: loser.Revision},
				memory.EntryRevision{ID: winner.ID, Revision: winner.Revision},
			)
			if err != nil {
				t.Fatalf("conditional supersede: %v", err)
			}
			if applied {
				t.Fatalf("stale supersede applied after %s rewrite", changed)
			}
			if got := mustEntry(t, es, "loser"); got.SupersededBy != "" {
				t.Fatalf("loser superseded after %s rewrite: %+v", changed, got)
			}
		})
	}
}

func TestGetByNameNotFound(t *testing.T) {
	es, _ := newEntryStore(t)
	_, err := es.GetByName(context.Background(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListOrdering(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		if err := es.Upsert(ctx, &memory.Entry{Name: n, Content: n}); err != nil {
			t.Fatalf("upsert %q: %v", n, err)
		}
	}
	list, err := es.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if len(list) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(list))
	}
	for i, e := range list {
		if e.Name != want[i] {
			t.Fatalf("list[%d] = %q, want %q", i, e.Name, want[i])
		}
	}
}

func TestDelete(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{Name: "gone", Content: "ephemeral content here"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.Delete(ctx, "gone"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := es.GetByName(ctx, "gone"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if n := ftsCount(t, db, "ephemeral"); n != 0 {
		t.Fatalf("expected FTS row removed after delete, got %d", n)
	}
	// Deleting a missing entry returns ErrNotFound.
	if err := es.Delete(ctx, "never"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting missing, got %v", err)
	}
}

func TestMerge(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "from a"}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "b", Content: "from b"}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	into := &memory.Entry{Name: "into", Trigger: "merged", Content: "from a + from b"}
	if err := es.Merge(ctx, []string{"a", "b"}, into); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if _, err := es.GetByName(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected a gone, got %v", err)
	}
	if _, err := es.GetByName(ctx, "b"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected b gone, got %v", err)
	}
	got, err := es.GetByName(ctx, "into")
	if err != nil {
		t.Fatalf("expected into present: %v", err)
	}
	if got.Content != "from a + from b" {
		t.Fatalf("unexpected merged content: %q", got.Content)
	}
}

func TestMergeIntoNameInSources(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "old a"}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "b", Content: "old b"}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	// into.Name == "a" which is also in names: must survive with new content.
	into := &memory.Entry{Name: "a", Content: "merged into a"}
	if err := es.Merge(ctx, []string{"a", "b"}, into); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := es.GetByName(ctx, "a")
	if err != nil {
		t.Fatalf("expected a to survive merge: %v", err)
	}
	if got.Content != "merged into a" {
		t.Fatalf("expected merged content, got %q", got.Content)
	}
	if _, err := es.GetByName(ctx, "b"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected b gone, got %v", err)
	}
}

func TestBumpUsage(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{Name: "hot", Content: "popular"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	now := time.Now().UTC()
	if err := es.BumpUsage(ctx, "hot", now); err != nil {
		t.Fatalf("bump: %v", err)
	}
	got, err := es.GetByName(ctx, "hot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HitCount != 1 {
		t.Fatalf("expected hit_count 1, got %d", got.HitCount)
	}
	if got.LastUsedAt == nil {
		t.Fatal("expected last_used_at set after bump")
	}
	// Bumping a missing name is best-effort: no error.
	if err := es.BumpUsage(ctx, "nonexistent", now); err != nil {
		t.Fatalf("expected no error bumping missing name, got %v", err)
	}
}

func TestFTSSyncOnUpsert(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{
		Name:    "doc",
		Trigger: "when searching",
		Content: "the quick brown fox jumps",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if n := ftsCount(t, db, "brown"); n != 1 {
		t.Fatalf("expected FTS match for 'brown' after upsert, got %d", n)
	}

	// Conflict update should keep FTS in sync via the AFTER UPDATE trigger.
	if err := es.Upsert(ctx, &memory.Entry{
		Name:    "doc",
		Trigger: "when searching",
		Content: "a totally different sentence",
	}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	if n := ftsCount(t, db, "brown"); n != 0 {
		t.Fatalf("expected no FTS match for stale 'brown', got %d", n)
	}
	if n := ftsCount(t, db, "different"); n != 1 {
		t.Fatalf("expected FTS match for 'different', got %d", n)
	}
}

func TestCountAndCountNonPinned(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	if err := es.Upsert(ctx, &memory.Entry{Name: "p1", Content: "x", Pinned: true}); err != nil {
		t.Fatalf("upsert p1: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "n1", Content: "y"}); err != nil {
		t.Fatalf("upsert n1: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "n2", Content: "z"}); err != nil {
		t.Fatalf("upsert n2: %v", err)
	}

	if c, err := es.Count(ctx); err != nil || c != 3 {
		t.Fatalf("Count = %d, err %v; want 3", c, err)
	}
	if c, err := es.CountNonPinned(ctx); err != nil || c != 2 {
		t.Fatalf("CountNonPinned = %d, err %v; want 2", c, err)
	}
}

func TestEventDateAndFactSourceRoundTrip(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	ev := time.UnixMicro(1_600_000_000_000_000).UTC()
	e := &memory.Entry{Name: "moved", Content: "moved from sweden", CharCount: 17, EventDate: &ev, FactSource: "extraction"}
	if err := es.Upsert(ctx, e); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := es.GetByName(ctx, "moved")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FactSource != "extraction" {
		t.Fatalf("fact_source: got %q", got.FactSource)
	}
	if got.EventDate == nil || !got.EventDate.Equal(ev) {
		t.Fatalf("event_date: got %v want %v", got.EventDate, ev)
	}
}

func TestEventTimeRangeAndSupersededRoundTrip(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	start := time.Date(2023, time.May, 7, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	e := &memory.Entry{
		Name:         "trip",
		Content:      "visited the coast",
		EventStart:   &start,
		EventEnd:     &end,
		SupersededBy: "trip-updated",
	}
	if err := es.Upsert(ctx, e); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := es.GetByName(ctx, "trip")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EventStart == nil || !got.EventStart.Equal(start) {
		t.Fatalf("event_start: got %v want %v", got.EventStart, start)
	}
	if got.EventEnd == nil || !got.EventEnd.Equal(end) {
		t.Fatalf("event_end: got %v want %v", got.EventEnd, end)
	}
	if got.SupersededBy != "trip-updated" {
		t.Fatalf("superseded_by: got %q", got.SupersededBy)
	}
}

func TestUpsertPreservesSupersededFieldOnConflict(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	start := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	initial := &memory.Entry{
		Name:         "lifecycle",
		Content:      "original",
		EventStart:   &start,
		EventEnd:     &end,
		SupersededBy: "newer-entry",
	}
	if err := es.Upsert(ctx, initial); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "lifecycle", Content: "updated"}); err != nil {
		t.Fatalf("conflict upsert: %v", err)
	}
	got, err := es.GetByName(ctx, "lifecycle")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// A rangeless upsert (legacy migrate, chunk ingest, content-only edits)
	// must NOT wipe previously extracted event ranges — nil means "no new
	// information", not "clear".
	if got.EventStart == nil || !got.EventStart.Equal(start) || got.EventEnd == nil || !got.EventEnd.Equal(end) {
		t.Fatalf("rangeless refresh should preserve event range: start=%v end=%v", got.EventStart, got.EventEnd)
	}
	if got.SupersededBy != "newer-entry" {
		t.Fatalf("superseded_by = %q, want newer-entry", got.SupersededBy)
	}
}

func TestUpsertUpdatesEventRangeOnConflict(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	oldStart := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	oldEnd := oldStart.Add(24 * time.Hour)
	newStart := time.Date(2024, time.March, 4, 0, 0, 0, 0, time.UTC)
	newEnd := newStart.Add(72 * time.Hour)
	if err := es.Upsert(ctx, &memory.Entry{Name: "range-refresh", Content: "old", EventStart: &oldStart, EventEnd: &oldEnd}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "range-refresh", Content: "new", EventStart: &newStart, EventEnd: &newEnd}); err != nil {
		t.Fatalf("refresh upsert: %v", err)
	}
	got, err := es.GetByName(ctx, "range-refresh")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EventStart == nil || !got.EventStart.Equal(newStart) || got.EventEnd == nil || !got.EventEnd.Equal(newEnd) {
		t.Fatalf("event range was not refreshed: start=%v end=%v", got.EventStart, got.EventEnd)
	}
}

func TestSupersedeLifecycle(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()

	mustUpsert := func(e *memory.Entry) {
		t.Helper()
		if err := es.Upsert(ctx, e); err != nil {
			t.Fatalf("upsert %s: %v", e.Name, err)
		}
	}
	mustUpsert(&memory.Entry{Name: "old-job", Content: "I work at Acme"})
	mustUpsert(&memory.Entry{Name: "new-job", Content: "I work at Globex"})
	mustUpsert(&memory.Entry{Name: "pinned-fact", Content: "birthday is May 7", Pinned: true})

	// Validation: unknown loser → ErrNotFound.
	if err := es.Supersede(ctx, "ghost", "new-job"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("supersede unknown loser: got %v, want ErrNotFound", err)
	}
	// Validation: unknown winner → error (not silently accepted).
	if err := es.Supersede(ctx, "old-job", "ghost"); err == nil {
		t.Fatalf("supersede unknown winner: got nil error, want failure")
	}
	// Validation: self-supersede refused.
	if err := es.Supersede(ctx, "old-job", "old-job"); err == nil {
		t.Fatalf("self-supersede: got nil error, want failure")
	}
	// Validation: a pinned entry cannot be superseded.
	if err := es.Supersede(ctx, "pinned-fact", "new-job"); err == nil {
		t.Fatalf("supersede pinned: got nil error, want failure")
	}
	if got, _ := es.GetByName(ctx, "pinned-fact"); got.SupersededBy != "" {
		t.Fatalf("pinned entry was superseded: superseded_by=%q", got.SupersededBy)
	}

	// Happy path: old-job is superseded by new-job.
	if err := es.Supersede(ctx, "old-job", "new-job"); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	got, err := es.GetByName(ctx, "old-job")
	if err != nil {
		t.Fatalf("get after supersede: %v", err)
	}
	if got.SupersededBy != "new-job" {
		t.Fatalf("superseded_by = %q, want new-job", got.SupersededBy)
	}
	// The superseded entry is still retrievable (non-destructive suppression).
	if _, err := es.GetByName(ctx, "old-job"); err != nil {
		t.Fatalf("superseded entry must still exist: %v", err)
	}

	// Unsupersede rolls the misjudgment back.
	if err := es.Unsupersede(ctx, "old-job"); err != nil {
		t.Fatalf("unsupersede: %v", err)
	}
	got, err = es.GetByName(ctx, "old-job")
	if err != nil {
		t.Fatalf("get after unsupersede: %v", err)
	}
	if got.SupersededBy != "" {
		t.Fatalf("superseded_by = %q after unsupersede, want empty", got.SupersededBy)
	}
	// Unsupersede on an unknown entry → ErrNotFound.
	if err := es.Unsupersede(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unsupersede unknown: got %v, want ErrNotFound", err)
	}
}

func TestEntriesByNameBatch(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	for _, name := range []string{"batch-a", "batch-b"} {
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: name}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}
	got, err := es.EntriesByName(ctx, []string{"batch-a", "missing", "batch-b"})
	if err != nil {
		t.Fatalf("batch lookup: %v", err)
	}
	if len(got) != 2 || got["batch-a"].Content != "batch-a" || got["batch-b"].Content != "batch-b" {
		t.Fatalf("batch entries = %+v, want both stored entries", got)
	}
}

func TestDeleteCascadesDerived(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()
	if err := es.Upsert(ctx, &memory.Entry{Name: "alpha", Content: "hi", CharCount: 2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "alpha", []string{"Sweden", "Quicksort"}); err != nil {
		t.Fatalf("put entities: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES ('alpha','m',1,x'00',0)`); err != nil {
		t.Fatalf("insert embedding: %v", err)
	}
	if err := es.Delete(ctx, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, tbl := range []string{"memory_embeddings", "memory_entities"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl+` WHERE entry_name='alpha'`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("%s not cascaded: %d rows remain", tbl, n)
		}
	}
}

func TestDeleteCleansAllOwnedSideDataAndDanglingReferences(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()
	for _, entry := range []*memory.Entry{
		{Name: "victim", Content: "remove me"},
		{Name: "keeper", Content: "keep me"},
		{Name: "referrer", Content: "points at victim", SupersededBy: "victim"},
	} {
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES
			('victim','m',1,x'00',0), ('victim#alias','m',1,x'00',0),
			('victim#query','m',1,x'00',0), ('victim#other','m',1,x'00',0),
			('keeper','m',1,x'00',0)`,
		`INSERT INTO memory_entities(entry_name, entity_norm, entity_raw) VALUES
			('victim','victim-only','Victim Only'), ('victim','shared','Shared'),
			('keeper','shared','Shared'), ('keeper','keeper-only','Keeper Only')`,
		`INSERT INTO memory_entity_edges(entity_a, entity_b, kind, weight, updated_at) VALUES
			('victim-only','shared','co',1,0), ('shared','keeper-only','co',1,0),
			('historical-orphan-a','historical-orphan-b','co',1,0)`,
		`INSERT INTO memory_event_aliases(entry_name, alias) VALUES
			('victim','obsolete alias'), ('keeper','durable alias')`,
		`INSERT INTO memory_fact_queries(entry_name, query) VALUES
			('victim','obsolete query'), ('keeper','durable query')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed side data: %v", err)
		}
	}

	if err := es.Delete(ctx, "victim"); err != nil {
		t.Fatalf("delete victim: %v", err)
	}

	assertEntryMissing(t, es, "victim")
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name IN ('victim','victim#alias','victim#query')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name IN ('victim#other','keeper')`, 2)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entities WHERE entry_name='victim'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases WHERE entry_name='victim'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases_fts WHERE entry_name='victim'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_fact_queries WHERE entry_name='victim'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entity_edges WHERE entity_a='victim-only' OR entity_b='victim-only'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entity_edges WHERE entity_a='shared' AND entity_b='keeper-only'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entity_edges WHERE entity_a='historical-orphan-a'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases WHERE entry_name='keeper'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_fact_queries WHERE entry_name='keeper'`, 1)
	referrer, err := es.GetByName(ctx, "referrer")
	if err != nil {
		t.Fatalf("get referrer: %v", err)
	}
	if referrer.SupersededBy != "" {
		t.Fatalf("reverse supersession survived delete: %q", referrer.SupersededBy)
	}
}

func TestDeletePreservesShadowKeyThatIsAlsoALiveEntryName(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()
	for _, name := range []string{"owner", "owner#alias", "owner#query"} {
		if err := es.Upsert(ctx, &memory.Entry{Name: name, Content: "content for " + name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES
		('owner','m',1,x'00',0), ('owner#alias','m',1,x'00',0), ('owner#query','m',1,x'00',0)`); err != nil {
		t.Fatal(err)
	}

	if err := es.Delete(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	assertEntryMissing(t, es, "owner")
	if _, err := es.GetByName(ctx, "owner#alias"); err != nil {
		t.Fatalf("live suffix-name entry was deleted: %v", err)
	}
	if _, err := es.GetByName(ctx, "owner#query"); err != nil {
		t.Fatalf("live suffix-name entry was deleted: %v", err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name='owner'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name IN ('owner#alias','owner#query')`, 2)
}

func TestMergeCleansConsumedAndInvalidatesSurvivingTargetSideData(t *testing.T) {
	es, db := newEntryStore(t)
	ctx := context.Background()
	for _, entry := range []*memory.Entry{
		{Name: "source-a", Content: "old a"},
		{Name: "source-b", Content: "old b"},
		{Name: "target", Content: "old target"},
		{Name: "source-ref", Content: "points at source", SupersededBy: "source-a"},
		{Name: "target-ref", Content: "points at target", SupersededBy: "target"},
		{Name: "keeper", Content: "keep me"},
	} {
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at) VALUES
			('source-a','m',1,x'00',0), ('source-a#alias','m',1,x'00',0),
			('source-a#query','m',1,x'00',0), ('source-b','m',1,x'00',0),
			('target','m',1,x'00',0), ('target#alias','m',1,x'00',0),
			('target#query','m',1,x'00',0), ('keeper','m',1,x'00',0)`,
		`INSERT INTO memory_entities(entry_name, entity_norm, entity_raw) VALUES
			('source-a','source-only','Source Only'), ('source-b','shared','Shared'),
			('target','target-only','Target Only'), ('keeper','shared','Shared'),
			('keeper','keeper-only','Keeper Only')`,
		`INSERT INTO memory_entity_edges(entity_a, entity_b, kind, weight, updated_at) VALUES
			('source-only','shared','co',1,0), ('target-only','shared','co',1,0),
			('shared','keeper-only','co',1,0)`,
		`INSERT INTO memory_event_aliases(entry_name, alias) VALUES
			('source-a','source alias'), ('target','target alias'), ('keeper','keeper alias')`,
		`INSERT INTO memory_fact_queries(entry_name, query) VALUES
			('source-a','source query'), ('target','target query'), ('keeper','keeper query')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed side data: %v", err)
		}
	}

	if err := es.Merge(ctx, []string{"source-a", "source-b"}, &memory.Entry{
		Name: "target", Content: "merged content",
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	assertEntryMissing(t, es, "source-a")
	assertEntryMissing(t, es, "source-b")
	target, err := es.GetByName(ctx, "target")
	if err != nil || target.Content != "merged content" {
		t.Fatalf("target after merge = %+v, err %v", target, err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name IN
		('source-a','source-a#alias','source-a#query','source-b','source-b#alias','source-b#query',
		 'target','target#alias','target#query')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entities WHERE entry_name IN ('source-a','source-b','target')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases WHERE entry_name IN ('source-a','source-b','target')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases_fts WHERE entry_name IN ('source-a','source-b','target')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_fact_queries WHERE entry_name IN ('source-a','source-b','target')`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entity_edges`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name='keeper'`, 1)

	sourceRef, err := es.GetByName(ctx, "source-ref")
	if err != nil || sourceRef.SupersededBy != "" {
		t.Fatalf("source reverse reference = %+v, err %v; want cleared", sourceRef, err)
	}
	targetRef, err := es.GetByName(ctx, "target-ref")
	if err != nil || targetRef.SupersededBy != "target" {
		t.Fatalf("live target reverse reference = %+v, err %v; want preserved", targetRef, err)
	}
}

func TestDeleteAndMergeRollbackAtEveryCleanupStage(t *testing.T) {
	stages := []struct {
		name    string
		trigger string
	}{
		{"embeddings", `CREATE TEMP TRIGGER fail_cleanup BEFORE DELETE ON memory_embeddings
			WHEN OLD.entry_name='source' BEGIN SELECT RAISE(ABORT, 'embeddings'); END`},
		{"entities", `CREATE TEMP TRIGGER fail_cleanup BEFORE DELETE ON memory_entities
			WHEN OLD.entry_name='source' BEGIN SELECT RAISE(ABORT, 'entities'); END`},
		{"aliases", `CREATE TEMP TRIGGER fail_cleanup BEFORE DELETE ON memory_event_aliases
			WHEN OLD.entry_name='source' BEGIN SELECT RAISE(ABORT, 'aliases'); END`},
		{"fact_queries", `CREATE TEMP TRIGGER fail_cleanup BEFORE DELETE ON memory_fact_queries
			WHEN OLD.entry_name='source' BEGIN SELECT RAISE(ABORT, 'fact queries'); END`},
		{"reverse_reference", `CREATE TEMP TRIGGER fail_cleanup BEFORE UPDATE OF superseded_by ON memory_entries
			WHEN OLD.superseded_by='source' BEGIN SELECT RAISE(ABORT, 'reverse reference'); END`},
		{"entity_edge_prune", `CREATE TEMP TRIGGER fail_cleanup BEFORE DELETE ON memory_entity_edges
			WHEN OLD.entity_a='source-only' BEGIN SELECT RAISE(ABORT, 'entity edge'); END`},
	}

	for _, operation := range []string{"delete", "merge"} {
		for _, stage := range stages {
			t.Run(operation+"/"+stage.name, func(t *testing.T) {
				es, db := newEntryStore(t)
				ctx := context.Background()
				for _, entry := range []*memory.Entry{
					{Name: "source", Content: "original source"},
					{Name: "referrer", Content: "points at source", SupersededBy: "source"},
					{Name: "keeper", Content: "keep me"},
					{Name: "target", Content: "old target"},
				} {
					if err := es.Upsert(ctx, entry); err != nil {
						t.Fatalf("upsert %s: %v", entry.Name, err)
					}
				}
				for _, statement := range []string{
					`INSERT INTO memory_embeddings(entry_name, model, dims, vec, updated_at)
					 VALUES ('source','m',1,x'00',0)`,
					`INSERT INTO memory_entities(entry_name, entity_norm, entity_raw) VALUES
					 ('source','source-only','Source Only'), ('keeper','shared','Shared')`,
					`INSERT INTO memory_entity_edges(entity_a, entity_b, kind, weight, updated_at)
					 VALUES ('source-only','shared','co',1,0)`,
					`INSERT INTO memory_event_aliases(entry_name, alias) VALUES ('source','source alias')`,
					`INSERT INTO memory_fact_queries(entry_name, query) VALUES ('source','source query')`,
				} {
					if _, err := db.ExecContext(ctx, statement); err != nil {
						t.Fatalf("seed side data: %v", err)
					}
				}
				if _, err := db.ExecContext(ctx, stage.trigger); err != nil {
					t.Fatalf("create failure trigger: %v", err)
				}

				var err error
				if operation == "delete" {
					err = es.Delete(ctx, "source")
				} else {
					err = es.Merge(ctx, []string{"source"}, &memory.Entry{Name: "target", Content: "new target"})
				}
				if err == nil {
					t.Fatal("operation succeeded despite injected cleanup failure")
				}

				source, getErr := es.GetByName(ctx, "source")
				if getErr != nil || source.Content != "original source" {
					t.Fatalf("source was not rolled back: %+v, err %v", source, getErr)
				}
				target, getErr := es.GetByName(ctx, "target")
				if getErr != nil || target.Content != "old target" {
					t.Fatalf("target upsert was not rolled back: %+v, err %v", target, getErr)
				}
				referrer, getErr := es.GetByName(ctx, "referrer")
				if getErr != nil || referrer.SupersededBy != "source" {
					t.Fatalf("reverse reference was not rolled back: %+v, err %v", referrer, getErr)
				}
				assertRowCount(t, db, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name='source'`, 1)
				assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entities WHERE entry_name='source'`, 1)
				assertRowCount(t, db, `SELECT COUNT(*) FROM memory_event_aliases WHERE entry_name='source'`, 1)
				assertRowCount(t, db, `SELECT COUNT(*) FROM memory_fact_queries WHERE entry_name='source'`, 1)
				assertRowCount(t, db, `SELECT COUNT(*) FROM memory_entity_edges WHERE entity_a='source-only'`, 1)
			})
		}
	}
}

func assertEntryMissing(t *testing.T, es *memory.EntryStore, name string) {
	t.Helper()
	if _, err := es.GetByName(context.Background(), name); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("entry %q still exists: %v", name, err)
	}
}

func assertRowCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("count rows: %v\nquery: %s", err, query)
	}
	if got != want {
		t.Fatalf("row count = %d, want %d\nquery: %s", got, want, query)
	}
}

func TestEntityMatchCounts(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	for _, e := range []*memory.Entry{
		{Name: "a", Content: "x", CharCount: 1},
		{Name: "b", Content: "y", CharCount: 1},
	} {
		if err := es.Upsert(ctx, e); err != nil {
			t.Fatalf("upsert %s: %v", e.Name, err)
		}
	}
	if err := es.PutEntities(ctx, "a", []string{"Sweden", "Python"}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := es.PutEntities(ctx, "b", []string{"Python"}); err != nil {
		t.Fatalf("put b: %v", err)
	}
	counts, err := es.EntityMatchCounts(ctx, memory.EntityQueryTokens("Tell me about python and sweden"))
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if counts["a"] != 2 {
		t.Fatalf("entry a: got %d want 2", counts["a"])
	}
	if counts["b"] != 1 {
		t.Fatalf("entry b: got %d want 1", counts["b"])
	}
}

func TestEntitySignalsForQueryCombinesCuesAndCounts(t *testing.T) {
	ctx := context.Background()
	es, _ := newEntryStore(t)
	if err := es.Upsert(ctx, &memory.Entry{Name: "alice", Content: "profile"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "alice", []string{"Alice Smith", "Berlin"}); err != nil {
		t.Fatalf("entities: %v", err)
	}
	cues, counts, err := es.EntitySignalsForQuery(ctx, "What did Alice Smith do?")
	if err != nil {
		t.Fatalf("entity signals: %v", err)
	}
	if len(cues) != 1 || cues[0] != "alice smith" {
		t.Fatalf("cues = %v, want [alice smith]", cues)
	}
	if counts["alice"] != 1 {
		t.Fatalf("counts = %v, want alice=1", counts)
	}
}

func TestPutEntitiesReplaces(t *testing.T) {
	es, _ := newEntryStore(t)
	ctx := context.Background()
	if err := es.Upsert(ctx, &memory.Entry{Name: "a", Content: "x", CharCount: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := es.PutEntities(ctx, "a", []string{"Sweden"}); err != nil {
		t.Fatalf("put1: %v", err)
	}
	if err := es.PutEntities(ctx, "a", []string{"Norway"}); err != nil {
		t.Fatalf("put2: %v", err)
	}
	counts, err := es.EntityMatchCounts(ctx, []string{"sweden", "norway"})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if counts["a"] != 1 {
		t.Fatalf("expected only norway to match after replace, got %d", counts["a"])
	}
}

// TestPutFactQueries covers the doc2query pseudo-query accessors (feature 012):
// round-trip, case/whitespace-insensitive dedup, replace-on-rewrite, and that
// FactQueryEntryNames lists only facts with >=1 query.
func TestPutFactQueries(t *testing.T) {
	ctx := context.Background()
	es, _ := newEntryStore(t)

	if err := es.Upsert(ctx, &memory.Entry{Name: "fact-a", Content: "Jon lost his banking job on 2023-01-19."}); err != nil {
		t.Fatalf("upsert fact-a: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "fact-b", Content: "Gina moved to Berlin."}); err != nil {
		t.Fatalf("upsert fact-b: %v", err)
	}

	// Dedup: "  When did Jon lose his job? " and "when did jon lose his job?"
	// collapse to one; blanks dropped.
	if err := es.PutFactQueries(ctx, "fact-a", []string{
		"  When did Jon   lose his job? ",
		"when did jon lose his job?",
		"",
		"Who lost a banking job?",
	}); err != nil {
		t.Fatalf("put queries fact-a: %v", err)
	}
	got, err := es.FactQueries(ctx, "fact-a")
	if err != nil {
		t.Fatalf("get queries fact-a: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduped queries, got %d: %v", len(got), got)
	}

	// Rewrite replaces the prior set (not append).
	if err := es.PutFactQueries(ctx, "fact-a", []string{"What happened to Jon?"}); err != nil {
		t.Fatalf("rewrite queries fact-a: %v", err)
	}
	got, err = es.FactQueries(ctx, "fact-a")
	if err != nil {
		t.Fatalf("re-get queries fact-a: %v", err)
	}
	if len(got) != 1 || got[0] != "What happened to Jon?" {
		t.Fatalf("rewrite must replace, got %v", got)
	}

	// FactQueryEntryNames lists only facts with queries (fact-b has none).
	names, err := es.FactQueryEntryNames(ctx)
	if err != nil {
		t.Fatalf("entry names: %v", err)
	}
	if len(names) != 1 || names[0] != "fact-a" {
		t.Fatalf("expected only fact-a to have queries, got %v", names)
	}
}
