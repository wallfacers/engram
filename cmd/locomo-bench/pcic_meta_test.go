package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
)

// annotateFixtureConvs is a tiny two-turn conversation: one turn carries a
// durable claim, the other is small talk the annotator must report claimless.
func annotateFixtureConvs() []conversation {
	return []conversation{{
		ID: 0,
		Sessions: []session{{
			Index: 1,
			Turns: []turn{
				{Speaker: "Alice", Text: "I just started my new job as an engineer.", DiaID: "D1:1"},
				{Speaker: "Bob", Text: "Nice, congrats!", DiaID: "D1:2"},
			},
		}},
	}}
}

// annotateFixtureCaller answers the durable turn with a typed claim and every
// other turn with a claimless object.
func annotateFixtureCaller(_ context.Context, _, user string) (string, error) {
	if strings.Contains(user, "D1:1") {
		return "```json\n{\"entity\":\"Alice\",\"slot\":\"job\",\"value\":\"engineer\"," +
			"\"polarity\":\"affirm\",\"time_state\":\"current\",\"source_turn_ids\":[\"D1:1\"]}\n```", nil
	}
	return `{"entity":""}`, nil
}

func TestPCICAnnotateSpanShape(t *testing.T) {
	meta, err := annotatePCICMeta(context.Background(), annotateFixtureConvs(),
		"gpt-5.6-luna", "sha256:fixture", annotateFixtureCaller, 1, slog.Default())
	if err != nil {
		t.Fatalf("annotatePCICMeta: %v", err)
	}
	if meta.Header.AnnotateModel != "gpt-5.6-luna" || meta.Header.DatasetFingerprint != "sha256:fixture" {
		t.Fatalf("header = %#v, want model+fingerprint set", meta.Header)
	}
	if meta.Header.Count != len(meta.Spans) || len(meta.Spans) != 1 {
		t.Fatalf("count = %d, spans = %d, want 1 claimful span", meta.Header.Count, len(meta.Spans))
	}
	key := pcicSpanKey(0, "D1:1")
	claim, ok := meta.Spans[key]
	if !ok {
		t.Fatalf("%s span missing; got %#v", key, meta.Spans)
	}
	if _, dup := meta.Spans[pcicSpanKey(0, "D1:2")]; dup {
		t.Fatalf("claimless turn D1:2 must be absent")
	}
	if claim.SpanID != key {
		t.Fatalf("span_id = %q, want %q", claim.SpanID, key)
	}
	if claim.Entity != memory.EntityNorm("Alice") {
		t.Fatalf("entity = %q, want normalized %q", claim.Entity, memory.EntityNorm("Alice"))
	}
	if claim.Slot != "job" || claim.Value != "engineer" || claim.TimeState != "current" {
		t.Fatalf("claim fields = %#v", claim)
	}
	if claim.Polarity != PolarityAffirm {
		t.Fatalf("polarity = %q, want affirm", claim.Polarity)
	}
	if len(claim.SourceTurnIDs) == 0 || claim.SourceTurnIDs[0] != "D1:1" {
		t.Fatalf("source_turn_ids = %v, want [D1:1]", claim.SourceTurnIDs)
	}
	// The sidecar must validate under its own schema check.
	if err := validatePCICMeta(meta); err != nil {
		t.Fatalf("annotated meta invalid: %v", err)
	}
}

// TestPCICAnnotateScopesSpansPerConversation guards the cross-conversation
// dia_id collision: LoCoMo dia_ids (e.g. "D1:1") are unique only within one
// conversation, so a global sidecar keyed by bare dia_id would let conv 1's
// claim overwrite conv 0's. Keys must be conversation-scoped.
func TestPCICAnnotateScopesSpansPerConversation(t *testing.T) {
	convs := []conversation{
		{ID: 0, Sessions: []session{{Index: 1, Turns: []turn{
			{Speaker: "Alice", Text: "Alice is an engineer.", DiaID: "D1:1"},
		}}}},
		{ID: 1, Sessions: []session{{Index: 1, Turns: []turn{
			{Speaker: "Bob", Text: "Bob is a teacher.", DiaID: "D1:1"},
		}}}},
	}
	call := func(_ context.Context, _, user string) (string, error) {
		if strings.Contains(user, "Alice") {
			return `{"entity":"Alice","slot":"job","value":"engineer","polarity":"affirm","time_state":"current"}`, nil
		}
		return `{"entity":"Bob","slot":"job","value":"teacher","polarity":"affirm","time_state":"current"}`, nil
	}
	meta, err := annotatePCICMeta(context.Background(), convs, "gpt-5.6-luna", "sha256:fixture", call, 1, slog.Default())
	if err != nil {
		t.Fatalf("annotatePCICMeta: %v", err)
	}
	if len(meta.Spans) != 2 {
		t.Fatalf("colliding dia_ids collapsed: got %d spans, want 2 (%#v)", len(meta.Spans), meta.Spans)
	}
	c0, ok0 := meta.Spans[pcicSpanKey(0, "D1:1")]
	c1, ok1 := meta.Spans[pcicSpanKey(1, "D1:1")]
	if !ok0 || !ok1 {
		t.Fatalf("both conversations' D1:1 must survive; keys = %v", spanKeys(meta))
	}
	if c0.Entity != memory.EntityNorm("Alice") || c1.Entity != memory.EntityNorm("Bob") {
		t.Fatalf("cross-conv overwrite: conv0=%q conv1=%q", c0.Entity, c1.Entity)
	}
}

// TestPCICClaimsForChunkAreConversationScoped proves the selector's lookup keys
// spans by the current conversation, so a chunk in conv 1 never picks up conv
// 0's claim for the same dia_id.
func TestPCICClaimsForChunkAreConversationScoped(t *testing.T) {
	meta := &PCICMeta{
		Header: PCICMetaHeader{Count: 2},
		Spans: map[string]SpanClaim{
			pcicSpanKey(0, "D1:1"): {SpanID: pcicSpanKey(0, "D1:1"), Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, TimeState: "current"},
			pcicSpanKey(1, "D1:1"): {SpanID: pcicSpanKey(1, "D1:1"), Entity: "bob", Slot: "job", Value: "teacher", Polarity: PolarityAffirm, TimeState: "current"},
		},
	}
	input := PCICSelectionInput{
		Meta:       meta,
		ChunkTurns: map[string][]string{"chunk-x": {"D1:1"}},
		SpanKey:    func(d string) string { return pcicSpanKey(1, d) },
	}
	claims := claimsForChunk(pcicResult("chunk-x", 1.0, "text"), input)
	if len(claims) != 1 || claims[0].Entity != "bob" {
		t.Fatalf("conv-1 lookup got %#v, want bob's claim only", claims)
	}
}

func spanKeys(meta PCICMeta) []string {
	out := make([]string, 0, len(meta.Spans))
	for k := range meta.Spans {
		out = append(out, k)
	}
	return out
}

// TestPCICAnnotateToleratesTransientTurnFailure reproduces the exact production
// failure: one turn hits a transient relay 502. The one-time pass must skip that
// span (absent = safe degradation) and keep every other claim, never aborting
// thousands of successful annotations over one blip.
func TestPCICAnnotateToleratesTransientTurnFailure(t *testing.T) {
	convs := []conversation{{ID: 0, Sessions: []session{{Index: 1, Turns: []turn{
		{Speaker: "Alice", Text: "Alice is an engineer.", DiaID: "D1:1"},
		{Speaker: "Bob", Text: "Bob teaches.", DiaID: "D1:2"},
		{Speaker: "Cara", Text: "Cara paints.", DiaID: "D1:3"},
	}}}}}
	call := func(_ context.Context, _, user string) (string, error) {
		if strings.Contains(user, "D1:2") {
			return "", fmt.Errorf("provider/openai: server_error (HTTP 502): Bad Gateway")
		}
		if strings.Contains(user, "D1:1") {
			return `{"entity":"Alice","slot":"job","value":"engineer","polarity":"affirm","time_state":"current"}`, nil
		}
		return `{"entity":"Cara","slot":"hobby","value":"painting","polarity":"affirm","time_state":"current"}`, nil
	}
	meta, err := annotatePCICMeta(context.Background(), convs, "gpt-5.6-luna", "sha256:fixture", call, 1, slog.Default())
	if err != nil {
		t.Fatalf("one transient failure must not abort the pass: %v", err)
	}
	if len(meta.Spans) != 2 {
		t.Fatalf("got %d spans, want 2 (the 502 turn skipped): %v", len(meta.Spans), spanKeys(meta))
	}
	if _, ok := meta.Spans[pcicSpanKey(0, "D1:2")]; ok {
		t.Fatalf("failed turn D1:2 must be absent")
	}
	if _, ok := meta.Spans[pcicSpanKey(0, "D1:1")]; !ok {
		t.Fatalf("successful turn D1:1 must survive")
	}
}

// TestPCICAnnotateAbortsOnWidespreadFailure guards the other side: a real relay
// outage (every turn fails) must fail loudly, not silently write an empty
// sidecar that later degrades the selector to a no-op.
func TestPCICAnnotateAbortsOnWidespreadFailure(t *testing.T) {
	turns := make([]turn, 0, 40)
	for i := 1; i <= 40; i++ {
		turns = append(turns, turn{Speaker: "X", Text: "text", DiaID: fmt.Sprintf("D1:%d", i)})
	}
	convs := []conversation{{ID: 0, Sessions: []session{{Index: 1, Turns: turns}}}}
	call := func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("provider/openai: server_error (HTTP 502): Bad Gateway")
	}
	_, err := annotatePCICMeta(context.Background(), convs, "gpt-5.6-luna", "sha256:fixture", call, 2, slog.Default())
	if err == nil {
		t.Fatal("a total outage must fail the pass, not yield an empty sidecar")
	}
}

func TestFailoverModelCaller(t *testing.T) {
	t.Run("falls over to secondary on primary error", func(t *testing.T) {
		var seen []string
		primary := func(_ context.Context, _, _ string) (string, error) {
			seen = append(seen, "p")
			return "", fmt.Errorf("provider/openai: server_error (HTTP 502): Bad Gateway")
		}
		secondary := func(_ context.Context, _, _ string) (string, error) {
			seen = append(seen, "s")
			return "ok", nil
		}
		got, err := failoverModelCaller(primary, secondary)(context.Background(), "sys", "user")
		if err != nil || got != "ok" {
			t.Fatalf("got %q err %v, want ok/nil", got, err)
		}
		if len(seen) != 2 || seen[0] != "p" || seen[1] != "s" {
			t.Fatalf("call order = %v, want [p s]", seen)
		}
	})
	t.Run("primary success skips secondary", func(t *testing.T) {
		var seen []string
		primary := func(_ context.Context, _, _ string) (string, error) { seen = append(seen, "p"); return "ok", nil }
		secondary := func(_ context.Context, _, _ string) (string, error) { seen = append(seen, "s"); return "no", nil }
		got, err := failoverModelCaller(primary, secondary)(context.Background(), "sys", "user")
		if err != nil || got != "ok" {
			t.Fatalf("got %q err %v", got, err)
		}
		if len(seen) != 1 || seen[0] != "p" {
			t.Fatalf("secondary should not run: %v", seen)
		}
	})
	t.Run("all endpoints down returns last error", func(t *testing.T) {
		fail := func(_ context.Context, _, _ string) (string, error) { return "", fmt.Errorf("502") }
		_, err := failoverModelCaller(fail, fail)(context.Background(), "sys", "user")
		if err == nil {
			t.Fatal("want error when every endpoint fails")
		}
	})
	t.Run("context cancellation is terminal", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var seen []string
		primary := func(c context.Context, _, _ string) (string, error) { seen = append(seen, "p"); return "", c.Err() }
		secondary := func(_ context.Context, _, _ string) (string, error) { seen = append(seen, "s"); return "ok", nil }
		_, err := failoverModelCaller(primary, secondary)(ctx, "sys", "user")
		if err == nil {
			t.Fatal("cancelled context must not fall over")
		}
		if len(seen) != 1 {
			t.Fatalf("secondary must not run after cancellation: %v", seen)
		}
	})
}

func TestPCICSpanKeyRoundTrip(t *testing.T) {
	convID, diaID, ok := parsePCICSpanKey(pcicSpanKey(3, "D15:1"))
	if !ok || convID != 3 || diaID != "D15:1" {
		t.Fatalf("parse = (%d,%q,%v), want (3,D15:1,true)", convID, diaID, ok)
	}
	if _, _, ok := parsePCICSpanKey("garbage"); ok {
		t.Fatalf("malformed key must not parse")
	}
}

// TestFillPCICMetaTargetsOnlyRequestedTurns proves the gap-fill re-annotates
// exactly the requested turns — paying for only those, never the whole dataset —
// and preserves every existing span.
func TestFillPCICMetaTargetsOnlyRequestedTurns(t *testing.T) {
	convs := []conversation{{ID: 0, Sessions: []session{{Index: 1, Turns: []turn{
		{Speaker: "Alice", Text: "Alice is an engineer.", DiaID: "D15:1"},
		{Speaker: "Bob", Text: "unrelated small talk", DiaID: "D9:9"},
	}}}}}
	existing := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:fixture", Count: 1},
		Spans: map[string]SpanClaim{
			pcicSpanKey(0, "D9:9"): {SpanID: pcicSpanKey(0, "D9:9"), Entity: "bob", Slot: "hobby", Value: "chess", Polarity: PolarityAffirm, TimeState: "current"},
		},
	}
	called := 0
	call := func(_ context.Context, _, user string) (string, error) {
		called++
		if !strings.Contains(user, "D15:1") {
			t.Errorf("fill called on an unrequested turn: %q", user)
		}
		return `{"entity":"Alice","slot":"job","value":"engineer","polarity":"affirm","time_state":"current"}`, nil
	}
	meta, filled, missing, err := fillPCICMeta(context.Background(), convs, existing, []string{pcicSpanKey(0, "D15:1")}, call, slog.Default())
	if err != nil {
		t.Fatalf("fillPCICMeta: %v", err)
	}
	if called != 1 {
		t.Fatalf("LLM called %d times, want exactly 1 (only the requested turn)", called)
	}
	if filled != 1 || len(missing) != 0 {
		t.Fatalf("filled=%d missing=%v, want 1/none", filled, missing)
	}
	if len(meta.Spans) != 2 || meta.Header.Count != 2 {
		t.Fatalf("merged span count = %d/%d, want 2", len(meta.Spans), meta.Header.Count)
	}
	if got := meta.Spans[pcicSpanKey(0, "D9:9")]; got.Entity != "bob" {
		t.Fatalf("existing span mutated: %#v", got)
	}
	if got := meta.Spans[pcicSpanKey(0, "D15:1")]; got.Entity != memory.EntityNorm("Alice") {
		t.Fatalf("filled span wrong: %#v", got)
	}
}

// TestFillPCICMetaReportsUnfillable ensures a turn that still fails (or is
// claimless) is reported as missing, not silently dropped, and existing spans
// are preserved.
func TestFillPCICMetaReportsUnfillable(t *testing.T) {
	convs := []conversation{{ID: 0, Sessions: []session{{Index: 1, Turns: []turn{
		{Speaker: "A", Text: "text", DiaID: "D15:1"},
	}}}}}
	existing := PCICMeta{Header: PCICMetaHeader{Count: 0}, Spans: map[string]SpanClaim{}}
	call := func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("provider/openai: server_error (HTTP 502): Bad Gateway")
	}
	_, filled, missing, err := fillPCICMeta(context.Background(), convs, existing, []string{pcicSpanKey(0, "D15:1")}, call, slog.Default())
	if err != nil {
		t.Fatalf("a single fill failure should report missing, not error: %v", err)
	}
	if filled != 0 || len(missing) != 1 || missing[0] != pcicSpanKey(0, "D15:1") {
		t.Fatalf("filled=%d missing=%v, want 0 filled / D15:1 missing", filled, missing)
	}
}

func TestPCICAnnotateWritesNoEngineState(t *testing.T) {
	dir := t.TempDir()
	meta, err := annotatePCICMeta(context.Background(), annotateFixtureConvs(),
		"gpt-5.6-luna", "sha256:fixture", annotateFixtureCaller, 2, slog.Default())
	if err != nil {
		t.Fatalf("annotatePCICMeta: %v", err)
	}
	if err := savePCICMeta(filepath.Join(dir, "pcic_meta.json"), meta); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}
	// Annotation is a pure sidecar pass: it opens no store and writes no engine
	// state. The only artifact under the output dir is the sidecar JSON.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "pcic_meta.json" {
			t.Fatalf("unexpected artifact %q — annotation must touch no engine state", e.Name())
		}
	}
}

func TestPCICAnnotateCacheHitSkipsWork(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "dataset.json")
	if err := os.WriteFile(dataPath, []byte(`[{"qa":[],"conversation":{}}]`), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	fingerprint, err := pcicDatasetFingerprint(dataPath)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	metaPath := filepath.Join(dir, "pcic_meta.json")
	prior := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: fingerprint, Count: 1},
		Spans: map[string]SpanClaim{
			"D1:1": {SpanID: "D1:1", Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, SourceTurnIDs: []string{"D1:1"}},
		},
	}
	if err := savePCICMeta(metaPath, prior); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	opt := options{dataPath: dataPath, pcicMetaPath: metaPath, concurrency: 1}
	// A matching header is a cache hit: runPCICAnnotate must return before ever
	// constructing a provider, so a bogus base URL can never be dialed.
	if err := runPCICAnnotate(opt, annotateFixtureConvs(), "unused-key", "http://127.0.0.1:1/never-dialed", logger); err != nil {
		t.Fatalf("cache-hit annotate must not error: %v", err)
	}
	if !strings.Contains(logs.String(), "cache hit") {
		t.Fatalf("expected cache-hit log, got %q", logs.String())
	}
	// The sidecar is untouched (still the seeded single span).
	got, err := loadPCICMeta(metaPath, prior.Header, logger)
	if err != nil || got == nil || len(got.Spans) != 1 {
		t.Fatalf("sidecar mutated by cache hit: got=%#v err=%v", got, err)
	}
}

func TestPCICAnnotateSidecarHasNoSecret(t *testing.T) {
	const secret = "sk-annotate-SECRET-should-never-appear"
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	// The caller closes over a credential (as the real provider does) but must
	// never surface it into claims, the sidecar, or logs.
	call := func(_ context.Context, _, user string) (string, error) {
		_ = secret // credential lives only in the caller's closure
		return annotateFixtureCaller(context.Background(), "", user)
	}
	meta, err := annotatePCICMeta(context.Background(), annotateFixtureConvs(),
		"gpt-5.6-luna", "sha256:fixture", call, 1, logger)
	if err != nil {
		t.Fatalf("annotatePCICMeta: %v", err)
	}
	path := filepath.Join(t.TempDir(), "pcic_meta.json")
	if err := savePCICMeta(path, meta); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("sidecar leaked the API key")
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("logs leaked the API key")
	}
}

func TestPCICMetaRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pcic_meta.json")
	want := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:fixture", Count: 2},
		Spans: map[string]SpanClaim{
			"D1:1": {SpanID: "D1:1", Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, TimeState: "current", SourceTurnIDs: []string{"D1:1"}},
			"D1:2": {SpanID: "D1:2", Entity: "alice", Slot: "location", Value: "paris", Polarity: PolarityAffirm, TimeState: "past", SourceTurnIDs: []string{"D1:2"}},
		},
	}
	if err := savePCICMeta(path, want); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}
	got, err := loadPCICMeta(path, want.Header, slog.Default())
	if err != nil {
		t.Fatalf("loadPCICMeta: %v", err)
	}
	if got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestPCICMetaHeaderMismatchDegrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pcic_meta.json")
	meta := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:old", Count: 1},
		Spans: map[string]SpanClaim{
			"D1:1": {SpanID: "D1:1", Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, SourceTurnIDs: []string{"D1:1"}},
		},
	}
	if err := savePCICMeta(path, meta); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	got, err := loadPCICMeta(path, PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:new"}, logger)
	if err != nil {
		t.Fatalf("mismatch must not fail the run: %v", err)
	}
	if got != nil {
		t.Fatalf("mismatch loaded stale metadata: %#v", got)
	}
	if !strings.Contains(logs.String(), "header mismatch") {
		t.Fatalf("mismatch warning missing: %q", logs.String())
	}
}
