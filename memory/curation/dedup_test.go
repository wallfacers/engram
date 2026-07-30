package curation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func TestNormalizeTextCollapsesWhitespaceAndLowercases(t *testing.T) {
	e := &memory.Entry{Name: "My-Note", Trigger: "  When   X\thappens ", Content: "Do\n\nThe Thing"}
	got := normalizeText(e)
	want := "my-note when x happens do the thing"
	if got != want {
		t.Fatalf("normalizeText = %q, want %q", got, want)
	}
}

func TestCharTrigrams(t *testing.T) {
	got := charTrigrams("abcd")
	for _, want := range []string{"abc", "bcd"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing trigram %q in %v", want, got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 trigrams, got %d (%v)", len(got), got)
	}
	// Short strings (<3 runes) collapse to a single whole-string token.
	short := charTrigrams("ab")
	if len(short) != 1 {
		t.Fatalf("expected 1 token for short string, got %d", len(short))
	}
	if _, ok := short["ab"]; !ok {
		t.Fatalf("short string token missing")
	}
	// CJK counted by code point, not byte.
	cjk := charTrigrams("数据中台")
	for _, want := range []string{"数据中", "据中台"} {
		if _, ok := cjk[want]; !ok {
			t.Fatalf("missing CJK trigram %q", want)
		}
	}
}

func TestJaccard(t *testing.T) {
	a := charTrigrams("abcd") // {abc, bcd}
	b := charTrigrams("abcd")
	approx(t, jaccard(a, b), 1.0)

	// Disjoint.
	approx(t, jaccard(charTrigrams("aaaa"), charTrigrams("zzzz")), 0)

	// Two empty sets → 0.
	approx(t, jaccard(map[string]struct{}{}, map[string]struct{}{}), 0)
}

func TestClusterGroupsNearDuplicates(t *testing.T) {
	entries := []*memory.Entry{
		{Name: "a", Content: "the quick brown fox jumps over the lazy dog"},
		{Name: "b", Content: "the quick brown fox jumps over the lazy dog!"}, // ~identical
		{Name: "c", Content: "completely unrelated content about databases"},
	}
	clusters := Cluster(entries, DefaultJaccardThreshold, nil)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %v", len(clusters), clusters)
	}
	got := clusters[0]
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("expected cluster {a,b}, got %v", names(got))
	}
}

func TestClusterTransitiveUnion(t *testing.T) {
	// a~b and b~c but a not directly ~c; union-find still groups all three.
	base := "shared phrase one two three four five six seven"
	entries := []*memory.Entry{
		{Name: "a", Content: base + " alpha alpha alpha"},
		{Name: "b", Content: base + " alpha beta"},
		{Name: "c", Content: base + " beta beta beta"},
	}
	clusters := Cluster(entries, 0.5, nil)
	if len(clusters) != 1 || len(clusters[0]) != 3 {
		t.Fatalf("expected one cluster of 3 (transitive), got %v", clusters)
	}
}

func TestClusterExcludesPinned(t *testing.T) {
	entries := []*memory.Entry{
		{Name: "pinned", Pinned: true, Content: "the quick brown fox jumps over the lazy dog"},
		{Name: "dup", Content: "the quick brown fox jumps over the lazy dog"},
	}
	clusters := Cluster(entries, DefaultJaccardThreshold, nil)
	if len(clusters) != 0 {
		t.Fatalf("pinned entry must not be clustered, got %v", clusters)
	}
}

func TestClusterSingletonsOmitted(t *testing.T) {
	entries := []*memory.Entry{
		{Name: "a", Content: "alpha content unique"},
		{Name: "b", Content: "totally different beta"},
	}
	if clusters := Cluster(entries, DefaultJaccardThreshold, nil); len(clusters) != 0 {
		t.Fatalf("expected no clusters for dissimilar entries, got %v", clusters)
	}
}

func TestClusterHonoursCandidatePairs(t *testing.T) {
	entries := []*memory.Entry{
		{Name: "a", Content: "the quick brown fox jumps over the lazy dog"},
		{Name: "b", Content: "the quick brown fox jumps over the lazy dog"},
		{Name: "c", Content: "the quick brown fox jumps over the lazy dog"},
	}
	// Only pre-filter (a,b); c is identical but never offered as a candidate.
	clusters := Cluster(entries, DefaultJaccardThreshold, [][2]int{{0, 1}})
	if len(clusters) != 1 || len(clusters[0]) != 2 {
		t.Fatalf("expected only {a,b} from candidate pairs, got %v", clusters)
	}
	if clusters[0][0].Name != "a" || clusters[0][1].Name != "b" {
		t.Fatalf("expected {a,b}, got %v", names(clusters[0]))
	}
	// Out-of-range / pinned-endpoint pairs are skipped without panic.
	safe := Cluster(entries, DefaultJaccardThreshold, [][2]int{{0, 99}, {-1, 1}, {2, 2}})
	if len(safe) != 0 {
		t.Fatalf("expected no clusters from invalid pairs, got %v", safe)
	}
}

func TestClusterDeterministicOrdering(t *testing.T) {
	entries := []*memory.Entry{
		{Name: "zebra", Content: "shared duplicate content here now"},
		{Name: "apple", Content: "shared duplicate content here now"},
		{Name: "mango", Content: "another duplicate pair of text values"},
		{Name: "lemon", Content: "another duplicate pair of text values"},
	}
	for i := 0; i < 20; i++ {
		clusters := Cluster(entries, DefaultJaccardThreshold, nil)
		if len(clusters) != 2 {
			t.Fatalf("expected 2 clusters, got %d", len(clusters))
		}
		// Clusters ordered by first member name: apple-cluster before lemon-cluster.
		if clusters[0][0].Name != "apple" || clusters[1][0].Name != "lemon" {
			t.Fatalf("nondeterministic cluster order: %v / %v", names(clusters[0]), names(clusters[1]))
		}
	}
}

func TestCurationMergeUnionsDirectEvidenceSources(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	entries := memory.NewEntryStore(s.DB())
	ledger := memory.NewLedgerStore(s.DB())
	sources, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{
		{ExternalSourceID: "turn-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "Alice moved to Berlin.", RecordedAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)},
		{ExternalSourceID: "turn-2", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 1, Content: "The move happened on Monday.", RecordedAt: time.Date(2026, 7, 30, 0, 1, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("append sources: %v", err)
	}
	for index, entry := range []*memory.Entry{
		{Name: "move-place", Content: "Alice moved to Berlin."},
		{Name: "move-time", Content: "Alice moved on Monday."},
	} {
		if err := entries.UpsertWithSources(ctx, entry, []memory.EvidenceRef{{EvidenceID: sources[index].ID, SourceOrder: 0, FullSource: true}}); err != nil {
			t.Fatalf("write sourced fact %q: %v", entry.Name, err)
		}
	}
	merged := &memory.Entry{Name: "alice-move", Content: "Alice moved to Berlin on Monday."}
	if err := entries.Merge(ctx, []string{"move-place", "move-time"}, merged); err != nil {
		t.Fatalf("merge sourced facts: %v", err)
	}
	refs, err := entries.SourceRefs(ctx, merged.ID)
	if err != nil {
		t.Fatalf("read merged lineage: %v", err)
	}
	if len(refs) != 2 || refs[0].EvidenceID != sources[0].ID || refs[1].EvidenceID != sources[1].ID {
		t.Fatalf("merged lineage = %+v", refs)
	}
	for _, source := range sources {
		if _, err := ledger.Get(ctx, source.ID); err != nil {
			t.Fatalf("merge removed source evidence %q: %v", source.ID, err)
		}
	}
}

func TestCurationMergeRejectsUnattributedSourceFact(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	entries := memory.NewEntryStore(s.DB())
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO memory_entries(id, name, content, created_at, updated_at)
		VALUES ('unattributed-id', 'unattributed', 'legacy raw fact', 1, 1)`); err != nil {
		t.Fatalf("seed unattributed fact: %v", err)
	}
	if err := entries.Merge(ctx, []string{"unattributed"}, &memory.Entry{Name: "merged", Content: "must not persist"}); err == nil {
		t.Fatal("merge with unattributed source unexpectedly succeeded")
	}
	if _, err := entries.GetByName(ctx, "merged"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unattributed merge leaked output entry: %v", err)
	}
}

func TestCurationMergeRollsBackSourceUnionWithFactDeletion(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	entries := memory.NewEntryStore(s.DB())
	ledger := memory.NewLedgerStore(s.DB())
	sources, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{
		{ExternalSourceID: "turn-1", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "user", Ordinal: 0, Content: "first source", RecordedAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)},
		{ExternalSourceID: "turn-2", SourceType: memory.EvidenceMessage, SourceSessionID: "session-a", Speaker: "assistant", Ordinal: 1, Content: "second source", RecordedAt: time.Date(2026, 7, 30, 0, 1, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("append sources: %v", err)
	}
	for index, entry := range []*memory.Entry{
		{Name: "first", Content: "first fact"},
		{Name: "second", Content: "second fact"},
	} {
		if err := entries.UpsertWithSources(ctx, entry, []memory.EvidenceRef{{EvidenceID: sources[index].ID, SourceOrder: 0, FullSource: true}}); err != nil {
			t.Fatalf("write sourced fact %q: %v", entry.Name, err)
		}
	}
	if _, err := s.DB().ExecContext(ctx, `
		CREATE TEMP TRIGGER fail_curation_merge
		BEFORE DELETE ON memory_entries
		WHEN OLD.name = 'second'
		BEGIN SELECT RAISE(ABORT, 'forced merge rollback'); END`); err != nil {
		t.Fatalf("install merge failure trigger: %v", err)
	}
	if err := entries.Merge(ctx, []string{"first", "second"}, &memory.Entry{Name: "merged", Content: "merged fact"}); err == nil {
		t.Fatal("merge unexpectedly succeeded despite forced delete failure")
	}
	if _, err := entries.GetByName(ctx, "merged"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed merge leaked target: %v", err)
	}
	for index, name := range []string{"first", "second"} {
		entry, err := entries.GetByName(ctx, name)
		if err != nil {
			t.Fatalf("failed merge removed source %q: %v", name, err)
		}
		refs, err := entries.SourceRefs(ctx, entry.ID)
		if err != nil || len(refs) != 1 || refs[0].EvidenceID != sources[index].ID {
			t.Fatalf("failed merge changed source lineage %q: refs=%+v err=%v", name, refs, err)
		}
	}
}

func names(es []*memory.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}
