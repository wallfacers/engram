package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
)

func renderSearch(results []memory.Result, semanticUnavailable bool) string {
	var b strings.Builder
	b.WriteString("# search\n\n")
	if semanticUnavailable {
		b.WriteString("- degraded.semantic: unavailable (no embedding endpoint configured)\n")
	}
	if len(results) == 0 {
		b.WriteString("_0 results_\n")
		return b.String()
	}
	for _, result := range results {
		fmt.Fprintf(&b, "\n## %s\n\n", result.Name)
		fmt.Fprintf(&b, "- score: %.6f\n", result.Score)
		if result.Trigger != "" {
			fmt.Fprintf(&b, "- trigger: %s\n", result.Trigger)
		}
		fmt.Fprintf(&b, "- snippet: %s\n\n", makeSnippet(result.Content))
		b.WriteString(strings.TrimRight(result.Content, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func renderEntry(entry *memory.Entry) string {
	if entry == nil {
		return "# memory\n\n"
	}
	var b strings.Builder
	writeEntry(&b, entry)
	return b.String()
}

func renderList(entries []*memory.Entry) string {
	sorted := sortedEntries(entries)
	var b strings.Builder
	b.WriteString("# memories\n\n")
	fmt.Fprintf(&b, "_%d entries_\n", len(sorted))
	for _, entry := range sorted {
		b.WriteString("\n---\n\n")
		writeEntry(&b, entry)
	}
	return b.String()
}

func renderExport(entries []*memory.Entry) string {
	base := memory.RenderExport(entries)
	withEventRange := make([]*memory.Entry, 0)
	for _, entry := range sortedEntries(entries) {
		if entry.EventStart != nil || entry.EventEnd != nil {
			withEventRange = append(withEventRange, entry)
		}
	}
	if len(withEventRange) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n## temporal metadata\n")
	for _, entry := range withEventRange {
		fmt.Fprintf(&b, "\n### %s\n\n", entry.Name)
		writeEventRange(&b, entry)
	}
	return b.String()
}

func sortedEntries(entries []*memory.Entry) []*memory.Entry {
	sorted := append([]*memory.Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Pinned != sorted[j].Pinned {
			return sorted[i].Pinned
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func writeEntry(b *strings.Builder, entry *memory.Entry) {
	fmt.Fprintf(b, "## %s\n\n", entry.Name)
	fmt.Fprintf(b, "- pinned: %t | durability: %s | category: %s\n", entry.Pinned, renderValue(entry.Durability), renderValue(entry.Category))
	fmt.Fprintf(b, "- hits: %d | last used: %s | created: %s | updated: %s\n", entry.HitCount, renderTime(entry.LastUsedAt), renderTimeValue(entry.CreatedAt), renderTimeValue(entry.UpdatedAt))
	if entry.Trigger != "" {
		fmt.Fprintf(b, "- trigger: %s\n", entry.Trigger)
	}
	writeEventRange(b, entry)
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(entry.Content, "\n"))
	b.WriteString("\n")
}

func writeEventRange(b *strings.Builder, entry *memory.Entry) {
	if entry.EventStart != nil {
		fmt.Fprintf(b, "- event start: %s\n", renderTimeValue(*entry.EventStart))
	}
	if entry.EventEnd != nil {
		fmt.Fprintf(b, "- event end: %s\n", renderTimeValue(*entry.EventEnd))
	}
}

func makeSnippet(content string) string {
	const maxRunes = 200
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes]) + "..."
}

func renderValue(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func renderTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "never"
	}
	return renderTimeValue(*value)
}

func renderTimeValue(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format("2006-01-02")
}
