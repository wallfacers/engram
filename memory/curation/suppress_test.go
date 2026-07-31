package curation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

// T005 — offline default write-time redundancy suppressor (024 FR-001/FR-003).
// These tests pin the deterministic character-trigram Jaccard decision with the
// conflict guard: event-date disagreement must survive, never be suppressed.
//
// Entries model the real pipeline shape: name = shared slug + distinct ULID
// suffix (entryName), so approximate facts share the slug portion of name and
// the decision is dominated by content overlap — exactly what normalizeText
// feeds the Jaccard signal with.

// entry builds an Entry in the real pipeline shape. suffix is the unique ULID
// tail; fact entries approximating each other share the slug prefix.
func entry(name, content string, eventDate *time.Time) *memory.Entry {
	e := &memory.Entry{Name: name, Content: content}
	if eventDate != nil {
		t := eventDate.UTC()
		e.EventDate = &t
	}
	return e
}

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// approximatePairs are the "同事件近似转述" cases the offline Jaccard path is
// designed to catch: high character-level overlap including the shared slug.
var approximatePairs = []struct{ existing, incoming string }{
	{
		"小明1月14日开家长会需要提前到校门口集合",
		"小明1月14日开家长会需要提前到校门口集合了",
	},
	{
		"小明1月14日开家长会，需要提前到校门口集合",
		"小明1月14日要开家长会，需要提前到校门口集合",
	},
	{
		"Alice moved to Berlin last spring with her cat",
		"Alice moved to Berlin last spring with her cat and dog",
	},
}

func TestSuppressorSuppressesNearDuplicate(t *testing.T) {
	s := NewSuppressor(0) // 0 → default threshold 0.7
	ctx := context.Background()
	for _, tc := range approximatePairs {
		existing := entry("parent-meeting-2026-01-14-0001", tc.existing, nil)
		incoming := entry("parent-meeting-2026-01-14-0002", tc.incoming, nil)
		if !s.ShouldSuppress(ctx, existing, incoming) {
			t.Fatalf("near-duplicate facts must be suppressed (existing=%q incoming=%q)", tc.existing, tc.incoming)
		}
	}
}

func TestSuppressorSuppressesExactContent(t *testing.T) {
	s := NewSuppressor(0)
	ctx := context.Background()
	existing := entry("parent-meeting-2026-01-14-0001", "小明1月14日开家长会", nil)
	if !s.ShouldSuppress(ctx, existing, existing) {
		t.Fatalf("identical content must be suppressed")
	}
}

func TestSuppressorKeepsDistinctFacts(t *testing.T) {
	s := NewSuppressor(0)
	ctx := context.Background()
	existing := entry("parent-meeting-2026-01-14-0001", "小明1月14日开家长会", nil)
	incoming := entry("alice-moved-to-berlin-0001", "Alice moved to Berlin last spring", nil)
	if s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("unrelated facts must not be suppressed (existing=%q incoming=%q)", existing.Content, incoming.Content)
	}
}

func TestSuppressorKeepsEventConflict(t *testing.T) {
	// FR-003: a correction/conflict must survive — different event dates on
	// otherwise textually similar facts are a temporal conflict, not redundancy.
	s := NewSuppressor(0)
	ctx := context.Background()
	existing := entry("meeting-rescheduled-0001", "会议改到1月14日举行", day(2026, 1, 14))
	incoming := entry("meeting-rescheduled-0002", "会议改到1月15日举行", day(2026, 1, 15))
	if s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("event-date conflict must not be suppressed (existing=%v incoming=%v)", *existing.EventDate, *incoming.EventDate)
	}
}

func TestSuppressorKeepsDateCorrection(t *testing.T) {
	// US1 scenario 3: "conflicting fact (e.g. date correction) is not
	// suppressed, projection is built as usual."
	s := NewSuppressor(0)
	ctx := context.Background()
	existing := entry("parent-meeting-2026-01-14-0001", "家长会定在1月14日", day(2026, 1, 14))
	incoming := entry("parent-meeting-2026-01-15-0002", "家长会改到1月15日了", day(2026, 1, 15))
	if s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("date correction must survive suppression")
	}
}

func TestSuppressorNilSafety(t *testing.T) {
	ctx := context.Background()
	if s := (*Suppressor)(nil); s.ShouldSuppress(ctx, entry("a-0001", "a", nil), entry("a-0002", "a", nil)) {
		t.Fatalf("nil suppressor must never suppress")
	}
	s := NewSuppressor(0)
	if s.ShouldSuppress(ctx, nil, nil) {
		t.Fatalf("nil entries must never suppress")
	}
	if s.ShouldSuppress(ctx, entry("a-0001", "a", nil), nil) {
		t.Fatalf("nil incoming must never suppress")
	}
	if s.ShouldSuppress(ctx, nil, entry("a-0001", "a", nil)) {
		t.Fatalf("nil existing must never suppress")
	}
}

// fixedEmbed maps the semantic-twin sentences to one vector and everything
// else to an orthogonal vector, so cosine is 1 only for the twin pair.
type fixedEmbed struct{}

func (fixedEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch text {
		case "小明明天下午三点开家长会", "Xiaoming has a parent-teacher meeting at 3pm tomorrow":
			out[i] = []float32{1, 1}
		default:
			out[i] = []float32{1, 0}
		}
	}
	return out, nil
}

func (fixedEmbed) Model() string { return "stub" }

const (
	twinExisting = "小明明天下午三点开家长会"
	twinIncoming = "Xiaoming has a parent-teacher meeting at 3pm tomorrow"
)

func TestSuppressorEmbeddingOverlayIsDefaultOff(t *testing.T) {
	// T012: the embedding overlay is default-off — without WithEmbedding, a
	// character-distinct (Jaccard ≈ 0) but semantically-identical pair is NOT
	// suppressed by the offline Jaccard signal alone.
	s := NewSuppressor(0)
	ctx := context.Background()
	existing := entry("twin-0001", twinExisting, nil)
	incoming := entry("twin-0002", twinIncoming, nil)
	if s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("character-distinct pair must NOT suppress without embedding overlay")
	}
}

func TestSuppressorEmbeddingOverlaySuppressesSemanticTwin(t *testing.T) {
	// T012: with the overlay on (OR combination), a pair whose embedding cosine
	// clears the overlay threshold is suppressed even when Jaccard is ~0.
	s := NewSuppressor(0).WithEmbedding(fixedEmbed{}, 0.9)
	ctx := context.Background()
	existing := entry("twin-0001", twinExisting, nil)
	incoming := entry("twin-0002", twinIncoming, nil)
	if !s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("semantic twin must suppress with embedding overlay on")
	}
	// Orthogonal pair must NOT suppress via the overlay.
	other := entry("other-0002", "totally unrelated fact", nil)
	if s.ShouldSuppress(ctx, existing, other) {
		t.Fatalf("orthogonal pair must not suppress via embedding overlay")
	}
	// Event-date conflict still survives the overlay.
	a := entry("conflict-0001", twinExisting, day(2026, 1, 14))
	b := entry("conflict-0002", twinIncoming, day(2026, 1, 15))
	if s.ShouldSuppress(ctx, a, b) {
		t.Fatalf("event-date conflict must survive even with embedding overlay")
	}
}

func TestSuppressorEmbeddingOverlayDegradesOnError(t *testing.T) {
	// T012: an embedding endpoint failure must degrade to the offline decision,
	// never drop evidence.
	s := NewSuppressor(0).WithEmbedding(erringEmbed{}, 0.9)
	ctx := context.Background()
	existing := entry("twin-0001", twinExisting, nil)
	incoming := entry("twin-0002", twinIncoming, nil)
	if s.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("embedding overlay error must degrade to offline decision (no suppression)")
	}
}

type erringEmbed struct{}

func (erringEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, fmt.Errorf("endpoint down")
}

func (erringEmbed) Model() string { return "stub" }

func TestSuppressorCustomThreshold(t *testing.T) {
	ctx := context.Background()
	existing := entry("parent-meeting-0001", "小明1月14日开家长会需要提前到校门口集合", nil)
	incoming := entry("parent-meeting-0002", "小明1月14日开家长会需要提前到校门口集合了", nil)
	// A high threshold must not be met by a near-duplicate that clears 0.7.
	strict := NewSuppressor(0.99)
	if strict.ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("near-duplicate must not suppress at threshold 0.99")
	}
	// Default threshold suppresses the same pair.
	if !NewSuppressor(0).ShouldSuppress(ctx, existing, incoming) {
		t.Fatalf("near-duplicate must suppress at default threshold")
	}
}
