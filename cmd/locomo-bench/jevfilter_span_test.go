package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// spanTestEnv is one in-memory store with a ledger so span-recovery tests can
// build real lineage: evidence records plus entries citing them.
type spanTestEnv struct {
	entries *memory.EntryStore
	ledger  *memory.LedgerStore
}

func newSpanTestEnv(t *testing.T) *spanTestEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	entries := memory.NewEntryStore(st.DB())
	return &spanTestEnv{entries: entries, ledger: entries.Ledger()}
}

// addEvidence appends one message record and returns its id.
func (e *spanTestEnv) addEvidence(t *testing.T, externalID, speaker string, ordinal int, content string, occurred time.Time) string {
	t.Helper()
	batch, err := e.ledger.AppendBatch(context.Background(), []memory.EvidenceInput{{
		ExternalSourceID: externalID,
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "conv0-sess1",
		Speaker:          speaker,
		Ordinal:          ordinal,
		Content:          content,
		OccurredAt:       &occurred,
		RecordedAt:       occurred.Add(time.Hour),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return batch[0].ID
}

// addFact upserts one fact entry citing refs through the atomic-fact projection
// and returns the retrieval-shaped hit carrying the store-assigned entry id.
func (e *spanTestEnv) addFact(t *testing.T, name string, refs ...memory.EvidenceRef) memory.Result {
	t.Helper()
	entry := &memory.Entry{Name: name, Trigger: name, Content: name, Category: "fact"}
	if err := e.entries.UpsertWithSources(context.Background(), entry, refs); err != nil {
		t.Fatal(err)
	}
	return memory.Result{ID: entry.ID, Name: name, Content: name}
}

func spanRef(t *testing.T, evidenceID, spanText string, start, end int) memory.EvidenceRef {
	t.Helper()
	return memory.EvidenceRef{EvidenceID: evidenceID, SourceOrder: 0, StartChar: &start, EndChar: &end,
		SpanDigest: strings.TrimPrefix(evalTextDigest(spanText), "sha256:")}
}

func fullRef(evidenceID string) memory.EvidenceRef {
	return memory.EvidenceRef{EvidenceID: evidenceID, SourceOrder: 0, FullSource: true}
}

func TestRecoverSpansPrefersKeptLineageAndFillsFromPoolHead(t *testing.T) {
	env := newSpanTestEnv(t)
	ctx := context.Background()
	day := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	keptEv := env.addEvidence(t, "D1:1", "Melanie", 0, "I signed up for pottery class yesterday", day)
	droppedEv := env.addEvidence(t, "D2:4", "Melanie", 1, "We went camping in the mountains last week", day.Add(24*time.Hour))

	keptFact := env.addFact(t, "fact-pottery", fullRef(keptEv))
	poolFact := env.addFact(t, "fact-camping", fullRef(droppedEv))

	spans, err := recoverSpans(ctx, newSpanRecovery(env.entries, 6), []memory.Result{keptFact}, []memory.Result{keptFact, poolFact})
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d span cards, want kept lineage plus one pool-head fill", len(spans))
	}
	if spans[0].Content != "Melanie: I signed up for pottery class yesterday" {
		t.Errorf("first card = %q, want the kept fact's verbatim source with speaker attribution", spans[0].Content)
	}
	if spans[0].ID != "span-"+keptEv {
		t.Errorf("first card id = %q", spans[0].ID)
	}
	if spans[1].Content != "Melanie: We went camping in the mountains last week" {
		t.Errorf("second card = %q, want the dropped-but-ranked pool fact's source", spans[1].Content)
	}
	if spans[0].SourceSessionID != "conv0-sess1" {
		t.Errorf("first card session = %q", spans[0].SourceSessionID)
	}
}

func TestRecoverSpansMergesWindowsAcrossFactsRuneSafe(t *testing.T) {
	env := newSpanTestEnv(t)
	ctx := context.Background()
	day := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	// 8 code points: 前缀🙂活动中文后缀 — spans [2,4) = 🙂活 and [3,6) = 活动中 must merge to [2,6) = 🙂活动中
	ev := env.addEvidence(t, "D1:2", "Caroline", 0, "前缀🙂活动中文后缀", day)
	factA := env.addFact(t, "fact-a", spanRef(t, ev, "🙂活", 2, 4))
	factB := env.addFact(t, "fact-b", spanRef(t, ev, "活动中", 3, 6))

	spans, err := recoverSpans(ctx, newSpanRecovery(env.entries, 6), []memory.Result{factA, factB}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d cards, want the shared evidence merged into one", len(spans))
	}
	if want := "Caroline: 🙂活动中"; spans[0].Content != want {
		t.Errorf("merged card = %q, want %q (code-point window union)", spans[0].Content, want)
	}
}

func TestRecoverSpansHonoursCapAndSkipsEntriesWithoutProjections(t *testing.T) {
	env := newSpanTestEnv(t)
	ctx := context.Background()
	day := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	evs := make([]string, 0, 4)
	for i, text := range []string{"one", "two", "three", "four"} {
		evs = append(evs, env.addEvidence(t, "D1:"+string(rune('1'+i)), "Melanie", i, text, day.Add(time.Duration(i)*time.Hour)))
	}
	facts := make([]memory.Result, 0, 5)
	for i, ev := range evs {
		facts = append(facts, env.addFact(t, "fact-"+string(rune('a'+i)), fullRef(ev)))
	}
	// A plain upsert (no explicit sources) is what chunk ingest uses: it has a
	// self-evidence record but no atomic-fact projection lineage to a message.
	if err := env.entries.Upsert(ctx, &memory.Entry{Name: "chunk-entry", Trigger: "chunk", Content: "verbatim chunk text", Category: "chunk"}); err != nil {
		t.Fatal(err)
	}
	chunkHit := memory.Result{ID: "chunk-entry", Name: "chunk-entry", Content: "verbatim chunk text"}

	spans, err := recoverSpans(ctx, newSpanRecovery(env.entries, 2), facts, []memory.Result{chunkHit})
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d cards, want the cap honoured", len(spans))
	}
	for _, span := range spans {
		if span.Content == "verbatim chunk text" {
			t.Error("a chunk entry produced a span card: verbatim entries must be skipped, not duplicated")
		}
	}
}

func TestRecoverSpansDisabledDegradesToNone(t *testing.T) {
	env := newSpanTestEnv(t)
	day := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	ev := env.addEvidence(t, "D1:9", "Melanie", 0, "text", day)
	fact := env.addFact(t, "fact-x", fullRef(ev))

	for _, sr := range []*spanRecovery{nil, newSpanRecovery(env.entries, 0), newSpanRecovery(nil, 4)} {
		spans, err := recoverSpans(context.Background(), sr, []memory.Result{fact}, nil)
		if err != nil {
			t.Fatalf("disabled recovery errored: %v", err)
		}
		if len(spans) != 0 {
			t.Errorf("disabled recovery produced %d cards", len(spans))
		}
	}
}

func TestSpanCardTextFallsBackToWholeRecordOnFullSource(t *testing.T) {
	day := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	ev := memory.Evidence{Speaker: "Melanie", Content: " whole record ", OccurredAt: &day}
	if got, want := spanCardText(ev, spanWindow{full: true}), "Melanie: whole record"; got != want {
		t.Errorf("full-source card = %q, want %q", got, want)
	}
	noSpeaker := memory.Evidence{Content: " bare "}
	if got, want := spanCardText(noSpeaker, spanWindow{full: true}), "bare"; got != want {
		t.Errorf("anonymous card = %q, want %q", got, want)
	}
	// Ledger message ingest prefixes some content with the speaker already; the
	// card must not double the attribution.
	prefixed := memory.Evidence{Speaker: "Melanie", Content: "Melanie: I signed up for pottery class"}
	if got, want := spanCardText(prefixed, spanWindow{full: true}), "Melanie: I signed up for pottery class"; got != want {
		t.Errorf("prefixed card = %q, want %q (no doubled speaker)", got, want)
	}
}
