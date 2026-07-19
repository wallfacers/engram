package memory

import (
	"testing"
	"time"
)

func TestParseTemporalIntentTable(t *testing.T) {
	anchor := time.Date(2024, time.June, 15, 12, 0, 0, 0, time.UTC)
	wantDay := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	tests := []struct {
		name         string
		query        string
		anchor       time.Time
		ok           bool
		start        time.Time
		end          time.Time
		intent       string
		state        string
		anchorEntity string
		fuzzy        bool
	}{
		{
			name:   "absolute month",
			query:  "What happened in May 2023?",
			ok:     true,
			start:  wantDay(2023, time.May, 1),
			end:    wantDay(2023, time.June, 1).Add(-time.Nanosecond),
			intent: "range",
			state:  "historical",
		},
		{
			name:   "last month",
			query:  "What happened last month?",
			anchor: anchor,
			ok:     true,
			start:  wantDay(2024, time.May, 1),
			end:    wantDay(2024, time.June, 1).Add(-time.Nanosecond),
			intent: "relative",
			state:  "historical",
		},
		{
			name:   "recently",
			query:  "What has happened recently?",
			anchor: anchor,
			ok:     true,
			start:  anchor.Add(-30 * 24 * time.Hour),
			end:    anchor,
			intent: "relative",
			state:  "current",
		},
		{
			name:   "last year in Chinese",
			query:  "去年发生了什么？",
			anchor: anchor,
			ok:     true,
			start:  wantDay(2023, time.January, 1),
			end:    wantDay(2024, time.January, 1).Add(-time.Nanosecond),
			intent: "relative",
			state:  "historical",
		},
		{
			name:   "last week in Chinese",
			query:  "上周的旅行是什么？",
			anchor: anchor,
			ok:     true,
			start:  wantDay(2024, time.June, 3),
			end:    wantDay(2024, time.June, 10).Add(-time.Nanosecond),
			intent: "relative",
			state:  "historical",
		},
		{
			name:         "before anchored event",
			query:        "What happened before the conference on 2023-05-07?",
			ok:           true,
			end:          wantDay(2023, time.May, 7).Add(-time.Nanosecond),
			intent:       "before",
			state:        "historical",
			anchorEntity: "conference",
		},
		{
			name:         "after anchored event",
			query:        "What happened after Alice's birthday on 7 May 2023?",
			ok:           true,
			start:        wantDay(2023, time.May, 7).Add(24 * time.Hour),
			intent:       "after",
			state:        "historical",
			anchorEntity: "alice's birthday",
		},
		{
			name:   "current state",
			query:  "What is Alice's current job?",
			anchor: anchor,
			ok:     true,
			intent: "current",
			state:  "current",
		},
		{
			name:   "historical state",
			query:  "What was Alice's job in 2023?",
			anchor: anchor,
			ok:     true,
			start:  wantDay(2023, time.January, 1),
			end:    wantDay(2024, time.January, 1).Add(-time.Nanosecond),
			intent: "range",
			state:  "historical",
		},
		{
			name:   "relative without anchor is fuzzy",
			query:  "last month",
			ok:     true,
			intent: "relative",
			state:  "historical",
			fuzzy:  true,
		},
		{
			name:  "no time intent",
			query: "What is Alice's favorite color?",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseTemporalIntent(tt.query, tt.anchor)
			if ok != tt.ok {
				t.Fatalf("ok = %t, want %t; window=%+v", ok, tt.ok, got)
			}
			if !ok {
				return
			}
			if !tt.start.IsZero() && !got.Start.Equal(tt.start) {
				t.Errorf("start = %v, want %v", got.Start, tt.start)
			}
			if !tt.end.IsZero() && !got.End.Equal(tt.end) {
				t.Errorf("end = %v, want %v", got.End, tt.end)
			}
			if got.Start.IsZero() && got.End.IsZero() && got.Intent != "current" && got.Intent != "historical" {
				t.Error("successful parse returned an empty time window")
			}
			if !got.Start.IsZero() && !got.End.IsZero() && got.Start.After(got.End) {
				t.Errorf("invalid window: start=%v end=%v", got.Start, got.End)
			}
			if got.Intent != tt.intent {
				t.Errorf("intent = %q, want %q", got.Intent, tt.intent)
			}
			if got.State != tt.state {
				t.Errorf("state = %q, want %q", got.State, tt.state)
			}
			if got.AnchorEntity != tt.anchorEntity {
				t.Errorf("anchor entity = %q, want %q", got.AnchorEntity, tt.anchorEntity)
			}
			if got.Fuzzy != tt.fuzzy {
				t.Errorf("fuzzy = %t, want %t", got.Fuzzy, tt.fuzzy)
			}
		})
	}
}

func TestParseTemporalIntentDateThenOrderDoesNotPanic(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		intent string
	}{
		{name: "Chinese before", query: "2023年5月7日之前发生了什么？", intent: "before"},
		{name: "Chinese after", query: "2023年5月7日以后发生了什么？", intent: "after"},
		{name: "English before", query: "May 1, 2023 before the pottery class", intent: "before"},
		{name: "English after", query: "May 1, 2023 after the pottery class", intent: "after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseTemporalIntent(tt.query, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
			if !ok {
				t.Fatalf("parse failed: %+v", got)
			}
			if got.Intent != tt.intent {
				t.Fatalf("intent = %q, want %q; window=%+v", got.Intent, tt.intent, got)
			}
		})
	}
}

func TestParseTemporalIntentChineseBareOrderWords(t *testing.T) {
	tests := []struct {
		query  string
		intent string
	}{
		{query: "2023年5月7日之后发生了什么？", intent: "after"},
		{query: "2023年5月7日以前发生了什么？", intent: "before"},
	}
	for _, tt := range tests {
		got, ok := ParseTemporalIntent(tt.query, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
		if !ok || got.Intent != tt.intent {
			t.Errorf("ParseTemporalIntent(%q) = %+v, ok=%t; want intent %q", tt.query, got, ok, tt.intent)
		}
	}
}

func TestCurrentAndHistoricalIntentAreStateOnly(t *testing.T) {
	anchor := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	oldEvent := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		query string
		state string
	}{
		{query: "What is Alice's current job?", state: "current"},
		{query: "What was Alice's job?", state: "historical"},
	} {
		t.Run(tt.state, func(t *testing.T) {
			win, ok := ParseTemporalIntent(tt.query, anchor)
			if !ok || win.State != tt.state {
				t.Fatalf("window=%+v ok=%t, want state %q", win, ok, tt.state)
			}
			if !win.Start.IsZero() || !win.End.IsZero() {
				t.Fatalf("state-only intent unexpectedly has window: %+v", win)
			}
			if got := TemporalScore(&oldEvent, &oldEvent, win, 0); got != 1 {
				t.Fatalf("state-only score = %.12f, want neutral 1", got)
			}
		})
	}
}

func TestParseTemporalIntentGatesBareYearsByContext(t *testing.T) {
	anchor := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, query := range []string{
		"reach 2048 in the game",
		"the model supports 2023 tokens",
	} {
		if _, ok := ParseTemporalIntent(query, anchor); ok {
			t.Errorf("ordinary number was parsed as temporal intent: %q", query)
		}
	}
	for _, query := range []string{
		"events in 2023",
		"events during 2023",
		"events since 2023",
		"events until 2023",
		"events before 2023",
		"events after 2023",
		"events by 2023",
		"2023年发生了什么？",
	} {
		win, ok := ParseTemporalIntent(query, anchor)
		if !ok {
			t.Errorf("temporal year was not parsed: query=%q window=%+v ok=%t", query, win, ok)
			continue
		}
		if win.Intent == "before" || win.Intent == "after" {
			if win.AnchorTime.Year() != 2023 {
				t.Errorf("temporal order anchor year = %d, want 2023: query=%q window=%+v", win.AnchorTime.Year(), query, win)
			}
		} else if win.Start.Year() != 2023 {
			t.Errorf("temporal year start = %d, want 2023: query=%q window=%+v", win.Start.Year(), query, win)
		}
	}
}

func TestParseTemporalIntentUnionsMultipleAbsoluteDates(t *testing.T) {
	win, ok := ParseTemporalIntent("What happened between 2022 and 2023?", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatalf("span query was not parsed: %+v", win)
	}
	wantStart := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
	if win.Intent != "range" || !win.Start.Equal(wantStart) || !win.End.Equal(wantEnd) {
		t.Fatalf("span window = %+v, want [%v,%v] range", win, wantStart, wantEnd)
	}
}
