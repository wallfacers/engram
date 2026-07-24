package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed.UTC()
}

func timePtr(v time.Time) *time.Time { return &v }

// --- Layer 0: parse_coverage ---

func TestTemporal_Layer0ParseOK(t *testing.T) {
	anchor := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		question string
		want     bool
	}{
		{"What did Maya do in May 2023?", true},
		{"What is Maya's favorite hobby?", false},
		{"What happened last week?", true},
		{"When did Maya start her new job?", false},
		{"What did Caroline do before the pottery class?", true},
	}
	ok := 0
	for _, tc := range cases {
		if got := temporalParseOK(tc.question, anchor); got != tc.want {
			t.Errorf("temporalParseOK(%q) = %v, want %v", tc.question, got, tc.want)
		} else if got {
			ok++
		}
	}
	if ok != 3 {
		t.Fatalf("parsed ok count = %d, want 3", ok)
	}
}

// --- Layer 1: event_date_coverage ---

func TestTemporal_Layer1EventDated(t *testing.T) {
	if entryHasEventTime(nil) {
		t.Fatal("nil entry reported as event-dated")
	}
	if entryHasEventTime(&memory.Entry{}) {
		t.Fatal("entry with no event time reported as event-dated")
	}
	if !entryHasEventTime(&memory.Entry{EventDate: timePtr(mustTime(t, "2023-05-10"))}) {
		t.Fatal("event_date entry not reported as event-dated")
	}
	if !entryHasEventTime(&memory.Entry{EventStart: timePtr(mustTime(t, "2023-05-10"))}) {
		t.Fatal("event_start entry not reported as event-dated")
	}
	byName := map[string]*memory.Entry{
		"a": {Name: "a", EventDate: timePtr(mustTime(t, "2023-05-10"))},
		"b": {Name: "b", EventStart: timePtr(mustTime(t, "2023-05-10"))},
		"c": {Name: "c"},
	}
	dated, total := layer1EventDated(byName, []string{"a", "b", "c", "missing"})
	if dated != 2 || total != 3 {
		t.Fatalf("layer1EventDated = %d/%d, want 2/3", dated, total)
	}
}

// --- Layer 2: buried ---

func TestTemporal_Layer2Buried(t *testing.T) {
	if temporalGoldBuried(false, 5, 5, 30) {
		t.Fatal("unresolved gold reported as buried")
	}
	if temporalGoldBuried(true, 5, 5, 30) {
		t.Fatal("gold in top-30 reported as buried")
	}
	if !temporalGoldBuried(true, 71, -1, 30) {
		t.Fatal("gold buried below top-30 not reported as buried")
	}
	if !temporalGoldBuried(true, -1, -1, 30) {
		t.Fatal("gold never in pool not reported as buried")
	}
}

// --- Layer 3: event interval + intersection + oracle ---

func TestTemporal_EventInterval(t *testing.T) {
	if _, _, ok := eventInterval(nil, nil, nil); ok {
		t.Fatal("no event time resolved to an interval")
	}
	date := mustTime(t, "2023-05-10")
	start, end, ok := eventInterval(nil, nil, timePtr(date))
	if !ok || !start.Equal(date) || !end.Equal(date) {
		t.Fatalf("event_date fallback = %v..%v ok=%v, want point %v", start, end, ok, date)
	}
	s1 := mustTime(t, "2023-05-10")
	start, end, ok = eventInterval(timePtr(s1), nil, nil)
	if !ok || !start.Equal(s1) || !end.Equal(s1) {
		t.Fatalf("single-sided start = %v..%v ok=%v, want point %v", start, end, ok, s1)
	}
	s2 := mustTime(t, "2023-05-20")
	start, end, ok = eventInterval(timePtr(s1), timePtr(s2), nil)
	if !ok || !start.Equal(s1) || !end.Equal(s2) {
		t.Fatalf("interval = %v..%v, want %v..%v", start, end, s1, s2)
	}
}

func TestTemporal_EventIntersectsWindow(t *testing.T) {
	win := memory.TimeWindow{Start: mustTime(t, "2023-05-01"), End: mustTime(t, "2023-05-31")}
	inside := func(a, b string) bool {
		return eventIntersectsWindow(mustTime(t, a), mustTime(t, b), win)
	}
	if !inside("2023-05-10", "2023-05-20") {
		t.Error("fully-in event excluded")
	}
	if inside("2023-04-01", "2023-04-20") {
		t.Error("event before window included")
	}
	if inside("2023-06-01", "2023-06-20") {
		t.Error("event after window included")
	}
	if !inside("2023-04-20", "2023-05-10") {
		t.Error("partial overlap excluded")
	}
	// both-zero window is empty
	empty := memory.TimeWindow{}
	if eventIntersectsWindow(mustTime(t, "2023-05-10"), mustTime(t, "2023-05-10"), empty) {
		t.Error("both-zero window matched")
	}
	// lower-unbounded window (Start zero)
	upper := memory.TimeWindow{End: mustTime(t, "2023-05-31")}
	if !eventIntersectsWindow(mustTime(t, "2020-01-01"), mustTime(t, "2020-01-01"), upper) {
		t.Error("lower-unbounded window excluded an early event")
	}
	if eventIntersectsWindow(mustTime(t, "2023-06-10"), mustTime(t, "2023-06-10"), upper) {
		t.Error("lower-unbounded window matched an event past its end")
	}
	// upper-unbounded window (End zero)
	lower := memory.TimeWindow{Start: mustTime(t, "2023-05-01")}
	if !eventIntersectsWindow(mustTime(t, "2025-01-01"), mustTime(t, "2025-01-01"), lower) {
		t.Error("upper-unbounded window excluded a late event")
	}
	if eventIntersectsWindow(mustTime(t, "2023-04-10"), mustTime(t, "2023-04-10"), lower) {
		t.Error("upper-unbounded window matched an event before its start")
	}
}

func TestTemporal_OracleNames(t *testing.T) {
	win := memory.TimeWindow{Start: mustTime(t, "2023-05-01"), End: mustTime(t, "2023-05-31")}
	facts := []temporalOracleFact{
		{Name: "z", EventDate: timePtr(mustTime(t, "2023-05-10"))},
		{Name: "a", EventStart: timePtr(mustTime(t, "2023-05-05")), EventEnd: timePtr(mustTime(t, "2023-05-12"))},
		{Name: "m", EventDate: timePtr(mustTime(t, "2023-05-20"))},
		{Name: "outside", EventDate: timePtr(mustTime(t, "2023-08-01"))},
		{Name: "undated", EventDate: nil},
	}
	names := temporalOracleNames(facts, win, 30)
	want := []string{"a", "m", "z"}
	if len(names) != len(want) {
		t.Fatalf("oracle names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("oracle names = %v, want %v (name order)", names, want)
		}
	}
	// topN truncation
	if got := temporalOracleNames(facts, win, 2); len(got) != 2 || got[0] != "a" || got[1] != "m" {
		t.Fatalf("oracle topN=2 = %v, want [a m]", got)
	}
	// both-zero window yields nothing
	if got := temporalOracleNames(facts, memory.TimeWindow{}, 30); len(got) != 0 {
		t.Fatalf("both-zero window oracle = %v, want empty", got)
	}
}

func TestTemporal_OracleLift(t *testing.T) {
	buried := []string{"g1", "g2", "g3"}
	oracle := []string{"g2", "x", "g3"}
	if got := temporalOracleLift(buried, oracle); got != 2 {
		t.Fatalf("oracle lift = %d, want 2", got)
	}
	if got := temporalOracleLift(nil, oracle); got != 0 {
		t.Fatalf("empty buried lift = %d, want 0", got)
	}
}

// --- summarize + verdict ---

func passingTemporalQuestions() []temporalDiagnosticQuestion {
	return []temporalDiagnosticQuestion{
		{Conv: 0, Q: 0, Category: 2, ParsedWindow: true, GoldResolved: true, GoldRankPool: 71, GoldRankTopK: -1,
			Buried: true, GoldFactCount: 2, GoldFactsEventDated: 2, BuriedGoldFacts: 2, OracleLiftedFacts: 2},
		{Conv: 0, Q: 1, Category: 2, ParsedWindow: true, GoldResolved: true, GoldRankPool: 5, GoldRankTopK: 5,
			Buried: false, GoldFactCount: 1, GoldFactsEventDated: 1, BuriedGoldFacts: 0, OracleLiftedFacts: 0},
		{Conv: 0, Q: 2, Category: 2, ParsedWindow: false, GoldResolved: false, GoldRankPool: -1, GoldRankTopK: -1,
			Buried: false, GoldFactCount: 0, GoldFactsEventDated: 0, BuriedGoldFacts: 0, OracleLiftedFacts: 0},
	}
}

func TestTemporal_Summarize(t *testing.T) {
	report := summarizeTemporalDiagnostic(passingTemporalQuestions())
	if report.Questions != 3 {
		t.Fatalf("questions = %d, want 3", report.Questions)
	}
	if report.ParseWindowQuestions != 2 || !floatEq(report.ParseCoverage, 2.0/3.0) {
		t.Fatalf("parse_coverage = %v (%d), want 0.667 (2)", report.ParseCoverage, report.ParseWindowQuestions)
	}
	if report.GoldFacts != 3 || report.GoldFactsEventDated != 3 || !floatEq(report.EventDateCoverage, 1.0) {
		t.Fatalf("event_date_coverage = %v (%d/%d), want 1.0 (3/3)", report.EventDateCoverage, report.GoldFactsEventDated, report.GoldFacts)
	}
	if report.GoldResolvedQuestions != 2 || report.BuriedQuestions != 1 || !floatEq(report.BuriedRatio, 0.5) {
		t.Fatalf("buried_ratio = %v (%d/%d), want 0.5 (1/2)", report.BuriedRatio, report.BuriedQuestions, report.GoldResolvedQuestions)
	}
	if report.BuriedGoldFacts != 2 || report.OracleLiftedFacts != 2 || !floatEq(report.OracleLiftRatio, 1.0) {
		t.Fatalf("oracle_lift = %d/%d (%v), want 2/2 (1.0)", report.OracleLiftedFacts, report.BuriedGoldFacts, report.OracleLiftRatio)
	}
}

func TestTemporal_Verdict(t *testing.T) {
	pass := summarizeTemporalDiagnostic(passingTemporalQuestions())
	if verdict, cause := temporalDiagnosticVerdict(pass); verdict != verdictGo || cause != "" {
		t.Fatalf("passing report verdict = %s(%s), want GO", verdict, cause)
	}

	cases := []struct {
		name      string
		mutate    func(r *temporalDiagnosticReport)
		wantCause string
	}{
		{"parser", func(r *temporalDiagnosticReport) { r.ParseCoverage = 0 }, causeParser},
		{"extraction", func(r *temporalDiagnosticReport) { r.EventDateCoverage = 0 }, causeExtraction},
		{"not-recall", func(r *temporalDiagnosticReport) { r.BuriedRatio = 0 }, causeNotRecall},
		{"ceiling", func(r *temporalDiagnosticReport) { r.OracleLiftRatio = 0 }, causeCeiling},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := summarizeTemporalDiagnostic(passingTemporalQuestions())
			tc.mutate(&report)
			verdict, cause := temporalDiagnosticVerdict(report)
			if verdict != verdictNoGo {
				t.Fatalf("verdict = %s, want NO-GO", verdict)
			}
			if cause != tc.wantCause {
				t.Fatalf("cause = %q, want %q", cause, tc.wantCause)
			}
		})
	}
}

func floatEq(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}

// --- end-to-end: retrieval-only, never extracts ---

func TestTemporal_RunNeverExtracts(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	seedTemporalDiagnosticStore(t, ctx, filepath.Join(storeDir, "conv0.db"))
	runDir := t.TempDir()
	conv := conversation{
		ID: 0,
		Sessions: []session{{
			Index: 1,
			Date:  mustTime(t, "2023-06-01"),
			Turns: []turn{{DiaID: "D1:1", Speaker: "Maya", Text: "Maya started pottery in May 2023."}},
		}},
		QA: []locomoQA{
			{Question: "What did Maya do in May 2023?", Category: 2, Evidence: []string{"D1:1"}},
			{Question: "What is Maya's favorite color?", Category: 3, Evidence: []string{"D1:1"}},
		},
	}
	opt := options{
		storeDir:        storeDir,
		runDir:          runDir,
		datasetFormat:   "locomo",
		retrieval:       "hybrid",
		chunks:          true,
		topK:            temporalDiagnosticTopK,
		concurrency:     1,
		factCoverageTau: defaultFactCoverageTau,
	}
	if err := runTemporalDiagnostic(ctx, opt, []conversation{conv}, temporalTestEmbedder{}, doc2queryDiscardLogger()); err != nil {
		t.Fatalf("run temporal diagnostic: %v", err)
	}
	report := readJSONObject(t, filepath.Join(runDir, temporalDiagnosticReportFile))
	// Only the category-2 question is scored; it parses a window.
	if got := report["questions"]; got != float64(1) {
		t.Fatalf("questions = %v, want 1 (temporal category only)", got)
	}
	if got := report["parse_coverage"]; got != float64(1) {
		t.Fatalf("parse_coverage = %v, want 1", got)
	}
	if got := report["event_date_coverage"]; got != float64(1) {
		t.Fatalf("event_date_coverage = %v, want 1", got)
	}
	if _, ok := report["verdict"]; !ok {
		t.Fatalf("report missing verdict: %v", report)
	}
}

type temporalTestEmbedder struct{}

func (temporalTestEmbedder) Model() string { return "temporal-test" }

func (temporalTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i + 1), 1, 0}
	}
	return vectors, nil
}

func seedTemporalDiagnosticStore(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		t.Fatalf("open temporal store: %v", err)
	}
	entries := memory.NewEntryStore(st.DB())
	start := mustTime(t, "2023-05-01")
	end := mustTime(t, "2023-05-31")
	entry := &memory.Entry{
		Name:            "maya-pottery",
		Content:         "Maya started pottery in May 2023.",
		SourceSessionID: "conv0-sess1",
		FactSource:      "extraction",
		EventStart:      &start,
		EventEnd:        &end,
		EventDate:       &start,
	}
	entry.CharCount = len(entry.Content)
	if err := entries.Upsert(ctx, entry); err != nil {
		_ = st.Close()
		t.Fatalf("upsert temporal fact: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close temporal store: %v", err)
	}
}
