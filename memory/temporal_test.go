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
			start:  anchor.AddDate(-1, 0, 0),
			end:    anchor,
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
			if got.Start.IsZero() && got.End.IsZero() {
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
