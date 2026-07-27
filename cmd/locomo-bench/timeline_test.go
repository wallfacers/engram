package main

import (
	"strings"
	"testing"
)

// Contract tests for feature 017 (deterministic temporal date scaffold).
// Every case here maps to a CT-N row in
// specs/017-temporal-date-scaffold/contracts/scaffold-contract.md and runs with
// zero LLM calls, zero network and zero GPU box — the whole point of US1 is that
// correctness is provable for free, before anyone spends a token on the e2e gate.
//
// The scaffold exists because spec 014 falsified the other route: telling the
// model to reason about dates ITSELF made things worse. So these tests pin down
// a scaffold that COMPUTES instead of instructing, and — just as importantly —
// that refuses to invent anything it cannot derive.

// temporalCategory (== 2) is already defined in temporal_diagnostic.go and is
// reused here on purpose: the scaffold's category gate must agree with the
// diagnostic that motivated this feature, not carry its own copy of the number.

// --- CT-1 / CT-2 / CT-3: the three short-circuits -----------------------------

func TestTimelineBlockDisabledReturnsEmpty(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "adopted a cat", EventDate: "2023-05-08"},
		{Content: "went hiking", EventDate: "2023-07-21"},
	}
	if got := buildTimelineBlock(memories, temporalCategory, false); got != "" {
		t.Fatalf("scaffold disabled must yield no block, got %q", got)
	}
}

func TestTimelineBlockNonTemporalCategoryReturnsEmpty(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "adopted a cat", EventDate: "2023-05-08"},
		{Content: "went hiking", EventDate: "2023-07-21"},
	}
	for _, category := range []int{1, 3, 4, 5} {
		if got := buildTimelineBlock(memories, category, true); got != "" {
			t.Fatalf("category %d must not get a scaffold, got %q", category, got)
		}
	}
}

func TestTimelineBlockNoDatedMemoryReturnsEmpty(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "likes jazz"},
		{Content: "dislikes crowds", Recorded: "2023-05-10"},
	}
	if got := buildTimelineBlock(memories, temporalCategory, true); got != "" {
		t.Fatalf("no resolvable event date must yield no block, got %q", got)
	}
}

// --- CT-4 / CT-5: ordering, numbering, natural-language dates -----------------

func TestTimelineBlockOrdersNumbersAndRendersNaturalDates(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "third", EventDate: "2023-07-21"},
		{Content: "first", EventDate: "2023-05-08"},
		{Content: "second", EventDate: "2023-06-01"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	want := "TIMELINE (computed from the [event:] markers above, chronological):\n" +
		"T1. 8 May 2023 — memory 2\n" +
		"T2. 1 June 2023 — memory 3\n" +
		"T3. 21 July 2023 — memory 1\n" +
		"SPAN: T1 → T3 = 74 days\n"
	if got != want {
		t.Fatalf("timeline block =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "2023-05-08") {
		t.Fatal("dates must be natural language, never ISO (the answer prompts forbid ISO)")
	}
}

func TestTimelineBlockSkipsUndatedButKeepsMemoryNumbering(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "undated one"},
		{Content: "dated one", EventDate: "2023-05-08"},
		{Content: "undated two"},
		{Content: "dated two", EventDate: "2023-05-18"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	// The undated memories must not appear as entries, but the "memory N"
	// back-references must still point at their real position in the prompt's
	// RETRIEVED MEMORIES list — otherwise the model is sent to the wrong line.
	if !strings.Contains(got, "T1. 8 May 2023 — memory 2\n") {
		t.Fatalf("first entry must back-reference memory 2, got %q", got)
	}
	if !strings.Contains(got, "T2. 18 May 2023 — memory 4\n") {
		t.Fatalf("second entry must back-reference memory 4, got %q", got)
	}
	if strings.Contains(got, "undated") {
		t.Fatalf("undated memories must not enter the block, got %q", got)
	}
	if n := strings.Count(got, "\nT"); n != 2 {
		t.Fatalf("expected exactly 2 entries (T1 + T2), block was %q", got)
	}
}

// --- CT-6 / CT-7: relative-expression resolution, and its refusal -------------

func TestTimelineBlockResolvesRelativeExpressionAgainstOwnAnchor(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "Alice said she would move next month", EventDate: "2023-05-08"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if !strings.Contains(got, "T1. 8 May 2023 — memory 1\n") {
		t.Fatalf("the native date must still be listed, got %q", got)
	}
	if !strings.Contains(got, `June 2023`) {
		t.Fatalf("relative phrase must resolve to an absolute date, got %q", got)
	}
	if !strings.Contains(got, `(derived from "next month")`) {
		t.Fatalf("a derived date MUST be marked as derived, got %q", got)
	}
}

func TestTimelineBlockRefusesToResolveWithoutAnchor(t *testing.T) {
	// Same relative phrase, but the memory carries no event date, so there is
	// no anchor. Recorded is deliberately present: recorded time is NOT an
	// acceptable anchor (mention time != event time), so nothing may be derived.
	memories := []retrievedMemory{
		{Content: "Alice said she would move next month", Recorded: "2023-05-10"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if got != "" {
		t.Fatalf("no anchor means nothing to derive and no block at all, got %q", got)
	}
}

func TestTimelineBlockNeverInventsDatesForUnknownPhrases(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "Alice said she would move sometime soonish, maybe", EventDate: "2023-05-08"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if strings.Contains(got, "derived") {
		t.Fatalf("an unrecognised phrase must not be resolved, got %q", got)
	}
}

// --- CT-8 / CT-9 / CT-10: interval arithmetic and its precision ceiling -------

func TestTimelineBlockComputesExactSpanForDayGranularity(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "start", EventDate: "2023-05-08"},
		{Content: "end", EventDate: "2023-05-18"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if !strings.Contains(got, "SPAN: T1 → T2 = 10 days\n") {
		t.Fatalf("expected an exact 10-day span, got %q", got)
	}
}

func TestTimelineBlockOmitsSpanForSingleEntry(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "lonely", EventDate: "2023-05-08"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if strings.Contains(got, "SPAN") {
		t.Fatalf("one endpoint cannot make a span; must not invent a second, got %q", got)
	}
	if !strings.Contains(got, "T1. 8 May 2023") {
		t.Fatalf("the single entry must still be listed, got %q", got)
	}
}

func TestTimelineBlockDegradesSpanPrecisionToCoarsestEndpoint(t *testing.T) {
	// Endpoint 2 is derived at MONTH granularity ("next year" would be year;
	// "next month" lands on a month). A span touching a month-granular endpoint
	// must not claim an exact day count — that would be fake precision.
	memories := []retrievedMemory{
		{Content: "anchor event", EventDate: "2023-01-10"},
		{Content: "she moved next month", EventDate: "2023-05-08"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	spanLine := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "SPAN:") {
			spanLine = line
		}
	}
	if spanLine == "" {
		t.Fatalf("expected a SPAN line, got %q", got)
	}
	if strings.Contains(spanLine, "days") {
		t.Fatalf("a month-granular endpoint must not yield an exact day count: %q", spanLine)
	}
	if !strings.Contains(spanLine, "about") {
		t.Fatalf("degraded spans must be marked approximate: %q", spanLine)
	}
}

// --- CT-11 / CT-12 / CT-16 / CT-17: determinism, stability, robustness --------

func TestTimelineBlockIsDeterministic(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "c", EventDate: "2023-07-21"},
		{Content: "a", EventDate: "2023-05-08"},
		{Content: "b", EventDate: "2023-05-08"},
		{Content: "d moved next month", EventDate: "2023-06-01"},
	}
	first := buildTimelineBlock(memories, temporalCategory, true)
	for i := 0; i < 100; i++ {
		if got := buildTimelineBlock(memories, temporalCategory, true); got != first {
			t.Fatalf("call %d differed:\ngot  %q\nwant %q", i, got, first)
		}
	}
}

func TestTimelineBlockKeepsInputOrderWithinSameDate(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "retrieved first", EventDate: "2023-05-08"},
		{Content: "retrieved second", EventDate: "2023-05-08"},
		{Content: "retrieved third", EventDate: "2023-05-08"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	want := "T1. 8 May 2023 — memory 1\nT2. 8 May 2023 — memory 2\nT3. 8 May 2023 — memory 3\n"
	if !strings.Contains(got, want) {
		t.Fatalf("same-date entries must keep retrieval order (stable sort):\ngot %q", got)
	}
}

func TestTimelineBlockHandlesYearBoundary(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "new year", EventDate: "2024-01-05"},
		{Content: "old year", EventDate: "2023-12-28"},
	}
	got := buildTimelineBlock(memories, temporalCategory, true)

	if !strings.Contains(got, "T1. 28 December 2023 — memory 2\n") {
		t.Fatalf("earlier (previous-year) entry must sort first, got %q", got)
	}
	if !strings.Contains(got, "T2. 5 January 2024 — memory 1\n") {
		t.Fatalf("later entry must sort second, got %q", got)
	}
	if !strings.Contains(got, "= 8 days\n") {
		t.Fatalf("span must cross the year boundary correctly, got %q", got)
	}
}

func TestTimelineBlockSurvivesMalformedInput(t *testing.T) {
	cases := [][]retrievedMemory{
		nil,
		{},
		{{}},
		{{Content: "", EventDate: ""}},
		{{Content: "x", EventDate: "not-a-date"}},
		{{Content: "y", EventDate: "2023-13-45"}},
		{{Content: "z", EventDate: "2023-05"}},
	}
	for i, memories := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d panicked: %v", i, r)
				}
			}()
			_ = buildTimelineBlock(memories, temporalCategory, true)
		}()
	}
}

func TestTimelineBlockDoesNotMutateInput(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "b", EventDate: "2023-07-21"},
		{Content: "a", EventDate: "2023-05-08"},
	}
	_ = buildTimelineBlock(memories, temporalCategory, true)

	if memories[0].Content != "b" || memories[1].Content != "a" {
		t.Fatalf("scaffold must not reorder or mutate the caller's slice: %+v", memories)
	}
}

// --- CT-13 / CT-14: wiring into both prompt builders --------------------------

// TestAnswerPromptInjectsTimelineBetweenMemoriesAndQuestion pins the injection
// point. The timeline is an INDEX over the evidence above it, so it has to land
// after the memory list and before the question — never replacing the body.
func TestAnswerPromptInjectsTimelineBetweenMemoriesAndQuestion(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "adopted a cat", EventDate: "2023-05-08"},
		{Content: "went hiking", EventDate: "2023-05-18"},
	}
	timeline := buildTimelineBlock(memories, temporalCategory, true)
	if timeline == "" {
		t.Fatal("precondition: expected a non-empty timeline for this fixture")
	}

	got := buildAnswerPrompt("When did she adopt the cat?", memories, "", timeline)
	want := "RETRIEVED MEMORIES:\n" +
		"1. [event: 2023-05-08] adopted a cat\n" +
		"2. [event: 2023-05-18] went hiking\n" +
		"\n" +
		"TIMELINE (computed from the [event:] markers above, chronological):\n" +
		"T1. 8 May 2023 — memory 1\n" +
		"T2. 18 May 2023 — memory 2\n" +
		"SPAN: T1 → T2 = 10 days\n" +
		"\nQUESTION: When did she adopt the cat?\n\nAnswer:"
	if got != want {
		t.Fatalf("scaffolded prompt =\n%q\nwant\n%q", got, want)
	}
}

// TestSweepAnswerPromptInjectsTimeline covers the cluster-sweep path. Wiring
// only the ordinary builder would let --temporal-date-scaffold silently do
// nothing on sweep questions, which would quietly corrupt the e2e attribution.
func TestSweepAnswerPromptInjectsTimeline(t *testing.T) {
	memories := []retrievedMemory{
		{Name: "a", Content: "early event", EventDate: "2023-05-01", SourceSessionID: "conv0-sess1"},
		{Name: "b", Content: "later event", EventDate: "2023-05-11", SourceSessionID: "conv0-sess2"},
	}
	timeline := buildTimelineBlock(memories, temporalCategory, true)

	got := buildSweepAnswerPrompt("What happened?", memories, "", timeline)
	if !strings.Contains(got, "TIMELINE (computed") {
		t.Fatalf("sweep path must receive the scaffold too, got %q", got)
	}
	if !strings.Contains(got, "SPAN: T1 → T2 = 10 days\n\nQUESTION:") {
		t.Fatalf("timeline must sit directly before the question, got %q", got)
	}
}

// TestScaffoldOffLeavesBothBuildersByteIdentical is the FR-006 guarantee stated
// as a property rather than a fixture: for the SAME inputs, an empty timeline
// must reproduce exactly what the pre-feature builders produced. The golden
// baseline tests in bench_test.go pin the literal bytes; this one pins the
// equivalence for the scaffold-shaped call sites.
func TestScaffoldOffLeavesBothBuildersByteIdentical(t *testing.T) {
	memories := []retrievedMemory{
		{Content: "adopted a cat", EventDate: "2023-05-08", SourceSessionID: "conv0-sess1"},
		{Content: "went hiking", EventDate: "2023-05-18", SourceSessionID: "conv0-sess1"},
	}
	// A non-temporal category must produce no block, hence no prompt change.
	disabledByCategory := buildTimelineBlock(memories, 1, true)
	disabledBySwitch := buildTimelineBlock(memories, temporalCategory, false)
	if disabledByCategory != "" || disabledBySwitch != "" {
		t.Fatal("precondition: both gates must yield an empty timeline")
	}

	if buildAnswerPrompt("q", memories, "", disabledByCategory) != buildAnswerPrompt("q", memories, "", "") {
		t.Fatal("category gate must leave the ordinary prompt byte-identical")
	}
	if buildSweepAnswerPrompt("q", memories, "", disabledBySwitch) != buildSweepAnswerPrompt("q", memories, "", "") {
		t.Fatal("switch gate must leave the sweep prompt byte-identical")
	}
}
