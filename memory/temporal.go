package memory

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultTemporalTau = 30 * 24 * time.Hour

// TimeWindow is the normalized temporal intent extracted from a query.
// Start and End are inclusive bounds. A zero bound means unbounded, which is
// useful for before/after queries. The metadata fields are deliberately
// optional extensions to the engine contract so callers can route current,
// historical, and order-sensitive queries without changing Search's API.
type TimeWindow struct {
	Start        time.Time
	End          time.Time
	Intent       string
	State        string
	AnchorEntity string
	AnchorTime   time.Time
	Fuzzy        bool
}

// TemporalScore returns the soft temporal relevance multiplier for an event.
// It is neutral when either side lacks usable event bounds, and uses the R3
// rule exp(-gap/tau) for non-overlapping intervals. tau <= 0 selects the
// contract default of thirty days.
func TemporalScore(eventStart, eventEnd *time.Time, window TimeWindow, tau time.Duration) float64 {
	if eventStart == nil && eventEnd == nil {
		return 1
	}
	if tau <= 0 {
		tau = defaultTemporalTau
	}
	start, end := temporalBounds(eventStart, eventEnd)
	if !window.Start.IsZero() && !window.End.IsZero() && !start.After(window.End) && !end.Before(window.Start) {
		return 1
	}
	if window.Start.IsZero() && window.End.IsZero() {
		return 1
	}
	var gap time.Duration
	switch {
	case !window.Start.IsZero() && end.Before(window.Start):
		gap = window.Start.Sub(end)
	case !window.End.IsZero() && start.After(window.End):
		gap = start.Sub(window.End)
	default:
		return 1
	}
	if gap <= 0 {
		return 1
	}
	return math.Exp(-gap.Seconds() / tau.Seconds())
}

func temporalBounds(eventStart, eventEnd *time.Time) (time.Time, time.Time) {
	var start, end time.Time
	if eventStart != nil {
		start = eventStart.UTC()
	}
	if eventEnd != nil {
		end = eventEnd.UTC()
	}
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	if end.Before(start) {
		end = start
	}
	return start, end
}

var (
	ymdDatePattern     = regexp.MustCompile(`\b(20\d{2})[-/](\d{1,2})[-/](\d{1,2})\b`)
	chineseDatePattern = regexp.MustCompile(`(20\d{2})年(\d{1,2})月(\d{1,2})日?`)
	dmyDatePattern     = regexp.MustCompile(`\b(\d{1,2})\s+([A-Za-z]+),?\s+(20\d{2})\b`)
	mdyDatePattern     = regexp.MustCompile(`\b([A-Za-z]+)\s+(\d{1,2}),?\s+(20\d{2})\b`)
	monthYearPattern   = regexp.MustCompile(`\b([A-Za-z]+)\s+(20\d{2})\b`)
	yearPattern        = regexp.MustCompile(`\b(20\d{2})\b`)
	orderPattern       = regexp.MustCompile(`(?i)\b(before|after)\b|之前|以后|之前的|之后的`)
	currentPattern     = regexp.MustCompile(`(?i)\b(current|currently|latest|today|now)\b|当前|目前|现在|如今|今天`)
	historicalPattern  = regexp.MustCompile(`(?i)\b(was|were|used to|historical|formerly|previously)\b|过去|以前|曾经|历史`)
	monthNames         = map[string]time.Month{
		"january": time.January, "jan": time.January,
		"february": time.February, "feb": time.February,
		"march": time.March, "mar": time.March,
		"april": time.April, "apr": time.April,
		"may":  time.May,
		"june": time.June, "jun": time.June,
		"july": time.July, "jul": time.July,
		"august": time.August, "aug": time.August,
		"september": time.September, "sep": time.September, "sept": time.September,
		"october": time.October, "oct": time.October,
		"november": time.November, "nov": time.November,
		"december": time.December, "dec": time.December,
	}
)

// ParseTemporalIntent extracts a time window from query using deterministic
// lexical rules. The anchor is normally the session date supplied by the
// caller; an absent anchor falls back to the local clock and marks the result
// fuzzy instead of dropping a relative query.
func ParseTemporalIntent(query string, anchor time.Time) (win TimeWindow, ok bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return TimeWindow{}, false
	}
	base, fuzzy := temporalAnchor(anchor)
	lower := strings.ToLower(query)

	if date, start, end, found := findAbsoluteDate(query); found {
		if order, orderStart, orderEnd := temporalOrder(query); order != "" {
			left, right := date.end, orderStart
			if orderStart < date.start {
				left, right = orderEnd, date.start
			}
			entity := cleanAnchorEntity(clampedSlice(query, left, right))
			if order == "before" {
				return TimeWindow{End: start.Add(-time.Nanosecond), Intent: order, State: "historical", AnchorEntity: entity, AnchorTime: start}, true
			}
			return TimeWindow{Start: end.Add(time.Nanosecond), Intent: order, State: "historical", AnchorEntity: entity, AnchorTime: end}, true
		}
		state := temporalState(lower, end, base, false)
		return TimeWindow{Start: start, End: end, Intent: "range", State: state}, true
	}

	if strings.Contains(lower, "last month") || strings.Contains(lower, "previous month") || strings.Contains(query, "上个月") || strings.Contains(query, "上月") {
		start := time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		return relativeWindow(start, endOfDay(start.AddDate(0, 1, -1)), "historical", fuzzy), true
	}
	if strings.Contains(lower, "last week") || strings.Contains(lower, "previous week") || strings.Contains(query, "上周") {
		monday := startOfWeek(base).AddDate(0, 0, -7)
		return relativeWindow(monday, endOfDay(monday.AddDate(0, 0, 6)), "historical", fuzzy), true
	}
	if strings.Contains(lower, "last year") || strings.Contains(lower, "previous year") || strings.Contains(query, "去年") || strings.Contains(query, "上年") {
		start := time.Date(base.Year()-1, time.January, 1, 0, 0, 0, 0, time.UTC)
		return relativeWindow(start, endOfDay(time.Date(base.Year()-1, time.December, 31, 0, 0, 0, 0, time.UTC)), "historical", fuzzy), true
	}
	if strings.Contains(lower, "yesterday") || strings.Contains(query, "昨天") {
		start := dayStart(base.AddDate(0, 0, -1))
		return relativeWindow(start, endOfDay(start), "historical", fuzzy), true
	}
	if strings.Contains(lower, "today") || strings.Contains(query, "今天") {
		start := dayStart(base)
		return relativeWindow(start, endOfDay(start), "current", fuzzy), true
	}
	if strings.Contains(lower, "recent") || strings.Contains(lower, "lately") || strings.Contains(query, "最近") || strings.Contains(query, "近期") {
		return relativeWindow(base.Add(-30*24*time.Hour), base, "current", fuzzy), true
	}
	if currentPattern.MatchString(query) {
		return TimeWindow{Start: base.AddDate(-1, 0, 0), End: base, Intent: "current", State: "current", Fuzzy: fuzzy}, true
	}
	if historicalPattern.MatchString(query) {
		return relativeWindow(base.AddDate(-10, 0, 0), base, "historical", fuzzy), true
	}
	return TimeWindow{}, false
}

func temporalAnchor(anchor time.Time) (time.Time, bool) {
	if anchor.IsZero() {
		return time.Now().UTC(), true
	}
	return anchor.UTC(), false
}

func relativeWindow(start, end time.Time, state string, fuzzy bool) TimeWindow {
	return TimeWindow{Start: start, End: end, Intent: "relative", State: state, Fuzzy: fuzzy}
}

func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func endOfDay(t time.Time) time.Time {
	return dayStart(t).Add(24*time.Hour - time.Nanosecond)
}

func startOfWeek(t time.Time) time.Time {
	t = dayStart(t)
	daysSinceMonday := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -daysSinceMonday)
}

func temporalState(query string, eventEnd, anchor time.Time, order bool) string {
	if order {
		return "historical"
	}
	if currentPattern.MatchString(query) || (!eventEnd.Before(anchor) && !historicalPattern.MatchString(query)) {
		return "current"
	}
	return "historical"
}

func temporalOrder(query string) (string, int, int) {
	if m := orderPattern.FindStringIndex(query); m != nil {
		matched := strings.ToLower(query[m[0]:m[1]])
		if strings.Contains(matched, "before") || strings.Contains(matched, "之前") {
			return "before", m[0], m[1]
		}
		return "after", m[0], m[1]
	}
	return "", 0, 0
}

func clampedSlice(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > len(s) {
		start = len(s)
	}
	if end > len(s) {
		end = len(s)
	}
	if start > end {
		start, end = end, start
	}
	return s[start:end]
}

type temporalDate struct {
	start int
	end   int
}

func findAbsoluteDate(query string) (temporalDate, time.Time, time.Time, bool) {
	if match := ymdDatePattern.FindStringSubmatchIndex(query); match != nil {
		if start, ok := dateFromYMD(match, query); ok {
			return temporalDate{start: match[0], end: match[1]}, start, endOfDay(start), true
		}
	}
	if match := chineseDatePattern.FindStringSubmatchIndex(query); match != nil {
		if start, ok := dateFromChinese(match, query); ok {
			return temporalDate{start: match[0], end: match[1]}, start, endOfDay(start), true
		}
	}
	if match := dmyDatePattern.FindStringSubmatchIndex(query); match != nil {
		if start, ok := dateFromDMY(match, query); ok {
			return temporalDate{start: match[0], end: match[1]}, start, endOfDay(start), true
		}
	}
	if match := mdyDatePattern.FindStringSubmatchIndex(query); match != nil {
		if start, ok := dateFromMDY(match, query); ok {
			return temporalDate{start: match[0], end: match[1]}, start, endOfDay(start), true
		}
	}
	if match := monthYearPattern.FindStringSubmatchIndex(query); match != nil {
		month, ok := monthFromString(query[match[2]:match[3]])
		if ok {
			year, err := strconv.Atoi(query[match[4]:match[5]])
			if err == nil {
				start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
				return temporalDate{start: match[0], end: match[1]}, start, endOfDay(start.AddDate(0, 1, -1)), true
			}
		}
	}
	if match := yearPattern.FindStringSubmatchIndex(query); match != nil {
		year, err := strconv.Atoi(query[match[2]:match[3]])
		if err == nil {
			start := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
			return temporalDate{start: match[0], end: match[1]}, start, endOfDay(time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)), true
		}
	}
	return temporalDate{}, time.Time{}, time.Time{}, false
}

func dateFromYMD(match []int, query string) (time.Time, bool) {
	year, _ := strconv.Atoi(query[match[2]:match[3]])
	month, _ := strconv.Atoi(query[match[4]:match[5]])
	day, _ := strconv.Atoi(query[match[6]:match[7]])
	return validDate(year, time.Month(month), day)
}

func dateFromChinese(match []int, query string) (time.Time, bool) {
	year, _ := strconv.Atoi(query[match[2]:match[3]])
	month, _ := strconv.Atoi(query[match[4]:match[5]])
	day, _ := strconv.Atoi(query[match[6]:match[7]])
	return validDate(year, time.Month(month), day)
}

func dateFromDMY(match []int, query string) (time.Time, bool) {
	day, _ := strconv.Atoi(query[match[2]:match[3]])
	month, ok := monthFromString(query[match[4]:match[5]])
	if !ok {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(query[match[6]:match[7]])
	return validDate(year, month, day)
}

func dateFromMDY(match []int, query string) (time.Time, bool) {
	month, ok := monthFromString(query[match[2]:match[3]])
	if !ok {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(query[match[4]:match[5]])
	year, _ := strconv.Atoi(query[match[6]:match[7]])
	return validDate(year, month, day)
}

func validDate(year int, month time.Month, day int) (time.Time, bool) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return time.Time{}, false
	}
	return t, true
}

func monthFromString(s string) (time.Month, bool) {
	month, ok := monthNames[strings.ToLower(strings.TrimSpace(s))]
	return month, ok
}

func cleanAnchorEntity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSpace(strings.TrimPrefix(s, "the "))
	s = strings.TrimSpace(strings.TrimPrefix(s, "a "))
	s = strings.TrimSpace(strings.TrimSuffix(s, " on"))
	s = strings.TrimSpace(strings.TrimSuffix(s, " at"))
	s = strings.Trim(s, " '.,?!")
	return s
}
