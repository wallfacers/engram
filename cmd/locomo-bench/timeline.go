package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Deterministic temporal date scaffold (feature 017).
//
// WHY THIS EXISTS. A free offline triage of the 55 wrong temporal answers found
// that 38 of them (69%) had the gold evidence sitting in the top-30 context and
// still got answered wrong. The three failure modes were all arithmetic, not
// retrieval: picking the wrong candidate among several dated ones (55%), echoing
// a relative phrase instead of resolving it (26%), and never subtracting the two
// endpoints of a "how long" question (18%).
//
// Spec 014 tried the obvious fix — a stronger prompt contract telling the model
// to do those steps — and it came out SIGNIFICANTLY WORSE than the old simple
// contract. So this file takes the other route: do the mechanical work in code
// and hand the model a finished timeline. The model chooses; it does not compute.
//
// THREE INVARIANTS, in priority order:
//
//  1. NEVER INVENT. Every date here is either a memory's own [event:] marker or
//     is derived from that same memory's marker. There is no third source. When
//     anything is unclear we emit less, never a guess — a confidently wrong date
//     in an authoritative-looking block is worse than no block at all, because
//     the model has no way to tell it is wrong.
//  2. DEGRADE, DON'T FAIL. Missing date, missing anchor, unparseable phrase,
//     insufficient granularity — each drops just its own contribution. The
//     memory itself stays in the prompt body either way.
//  3. DETERMINISTIC. Pure function of the input slice. No clock, no randomness,
//     no map iteration in any decision. Same input, same bytes, forever.
//
// Everything is gated behind --temporal-date-scaffold (default OFF) and behind
// category == temporal, so the canonical recipe is byte-for-byte unaffected
// until the e2e gate says otherwise.

// dateGranularity records how precise a resolved date actually is. It is the
// mechanism behind invariant 1 for arithmetic: a span can never be reported more
// precisely than its coarsest endpoint, so "about 4 months" is emitted where a
// fake "121 days" would otherwise be.
type dateGranularity int

const (
	granularityDay dateGranularity = iota
	granularityMonth
	granularityYear
)

// timelineEntry is one dated memory as presented in the scaffold.
type timelineEntry struct {
	when        time.Time
	granularity dateGranularity
	memoryIndex int    // 1-based position in the prompt's RETRIEVED MEMORIES list
	derivedFrom string // the relative phrase this was resolved from; "" when native
}

const timelineHeader = "TIMELINE (computed from the [event:] markers above, chronological):"

// buildTimelineBlock renders the deterministic date scaffold for one question.
//
// Returns "" — meaning the caller's prompt stays byte-identical to the
// pre-feature path — when the scaffold is disabled, the question is not
// temporal, or no memory carries a resolvable event date.
func buildTimelineBlock(memories []retrievedMemory, category int, enabled bool) string {
	if !enabled || category != temporalCategory {
		return ""
	}

	entries := collectTimelineEntries(memories)
	if len(entries) == 0 {
		return ""
	}

	// Stable sort: same-date memories keep their retrieval order. A secondary
	// key (content, name) would decouple presentation from ranking for no gain.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].when.Before(entries[j].when) })

	var b strings.Builder
	b.WriteString(timelineHeader)
	b.WriteString("\n")
	for i, e := range entries {
		fmt.Fprintf(&b, "T%d. %s — memory %d", i+1, formatTimelineDate(e.when, e.granularity), e.memoryIndex)
		if e.derivedFrom != "" {
			fmt.Fprintf(&b, " (derived from %q)", e.derivedFrom)
		}
		b.WriteString("\n")
	}
	if len(entries) >= 2 {
		first, last := entries[0], entries[len(entries)-1]
		fmt.Fprintf(&b, "SPAN: T1 → T%d = %s\n", len(entries), formatSpan(first, last))
	}
	return b.String()
}

// collectTimelineEntries reads each memory's structured event date and, when the
// text carries a recognised relative phrase, resolves that phrase against the
// memory's OWN date. Memories without a usable date contribute nothing and are
// silently skipped — they remain visible in the prompt body.
func collectTimelineEntries(memories []retrievedMemory) []timelineEntry {
	entries := make([]timelineEntry, 0, len(memories))
	for i, m := range memories {
		anchor, ok := parseEventDate(m.EventDate)
		if !ok {
			// No anchor: nothing to list and — critically — nothing to resolve
			// a relative phrase against. Recorded time is NOT a substitute;
			// mention time is not event time.
			continue
		}
		position := i + 1
		entries = append(entries, timelineEntry{
			when:        anchor,
			granularity: granularityDay,
			memoryIndex: position,
		})
		if derived, phrase, granularity, ok := resolveRelativePhrase(m.Content, anchor); ok {
			entries = append(entries, timelineEntry{
				when:        derived,
				granularity: granularity,
				memoryIndex: position,
				derivedFrom: phrase,
			})
		}
	}
	return entries
}

// parseEventDate accepts exactly the format toMemories produces ("2006-01-02").
// Anything else — empty, partial, malformed, impossible — is rejected rather
// than coerced, so a bad value can never silently become a confident date.
func parseEventDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// relativePattern is deliberately NARROW: it covers the phrase families the
// diagnosis actually observed, and nothing else. A general-purpose temporal
// parser would be a liability here — its tolerance is exactly what produces
// confident wrong answers on ambiguous text.
var relativePattern = regexp.MustCompile(
	`(?i)\b(next|last|previous|following)\s+(day|week|month|year)\b` +
		`|\b(\d{1,3}|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|week|month|year)s?\s+(ago|later|earlier)\b` +
		`|\b(yesterday|tomorrow)\b`)

var wordNumbers = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
}

// resolveRelativePhrase turns a recognised relative expression into an absolute
// date, anchored on the memory's own event date. It returns ok=false for every
// unrecognised, ambiguous, or multi-match input — when in doubt, emit nothing.
func resolveRelativePhrase(content string, anchor time.Time) (time.Time, string, dateGranularity, bool) {
	matches := relativePattern.FindAllStringSubmatch(content, 2)
	if len(matches) != 1 {
		// Zero matches: nothing to do. More than one: genuinely ambiguous which
		// phrase the question is about, so we refuse rather than pick.
		return time.Time{}, "", granularityDay, false
	}
	m := matches[0]
	phrase := strings.ToLower(strings.TrimSpace(m[0]))

	switch {
	case m[1] != "": // "next month", "last year", ...
		direction := 1
		if strings.EqualFold(m[1], "last") || strings.EqualFold(m[1], "previous") {
			direction = -1
		}
		return shiftBy(anchor, direction, 1, strings.ToLower(m[2]), phrase)

	case m[3] != "": // "two months ago", "3 weeks later", ...
		count, ok := parseCount(m[3])
		if !ok {
			return time.Time{}, "", granularityDay, false
		}
		direction := 1
		if strings.EqualFold(m[5], "ago") || strings.EqualFold(m[5], "earlier") {
			direction = -1
		}
		return shiftBy(anchor, direction, count, strings.ToLower(m[4]), phrase)

	case m[6] != "": // "yesterday" / "tomorrow"
		direction := -1
		if strings.EqualFold(m[6], "tomorrow") {
			direction = 1
		}
		return anchor.AddDate(0, 0, direction), phrase, granularityDay, true
	}
	return time.Time{}, "", granularityDay, false
}

func parseCount(raw string) (int, bool) {
	if n, ok := wordNumbers[strings.ToLower(raw)]; ok {
		return n, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// shiftBy moves the anchor and — just as importantly — reports the granularity
// the result actually has. "next month" genuinely does not pin down a day, so it
// is month-granular and any span touching it degrades accordingly.
func shiftBy(anchor time.Time, direction, count int, unit, phrase string) (time.Time, string, dateGranularity, bool) {
	switch unit {
	case "day":
		return anchor.AddDate(0, 0, direction*count), phrase, granularityDay, true
	case "week":
		return anchor.AddDate(0, 0, direction*count*7), phrase, granularityDay, true
	case "month":
		return anchor.AddDate(0, direction*count, 0), phrase, granularityMonth, true
	case "year":
		return anchor.AddDate(direction*count, 0, 0), phrase, granularityYear, true
	}
	return time.Time{}, "", granularityDay, false
}

// formatTimelineDate renders at the precision the date actually has, in the
// natural-language form the answer prompts require ("never ISO format").
func formatTimelineDate(when time.Time, granularity dateGranularity) string {
	switch granularity {
	case granularityYear:
		return when.Format("2006")
	case granularityMonth:
		return when.Format("January 2006")
	default:
		return fmt.Sprintf("%d %s", when.Day(), when.Format("January 2006"))
	}
}

// formatSpan reports the distance between the endpoints, capped at the precision
// of the coarser one. This is where "never invent" applies to arithmetic: an
// exact day count between a day-granular and a month-granular endpoint would be
// fabricated precision, so it degrades to an explicit approximation.
func formatSpan(from, to timelineEntry) string {
	coarsest := from.granularity
	if to.granularity > coarsest {
		coarsest = to.granularity
	}
	days := int(to.when.Sub(from.when).Hours() / 24)
	if days < 0 {
		days = -days
	}

	switch coarsest {
	case granularityYear:
		return fmt.Sprintf("about %s", pluralize(days/365, "year"))
	case granularityMonth:
		return fmt.Sprintf("about %s", pluralize(days/30, "month"))
	default:
		return pluralize(days, "day")
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
