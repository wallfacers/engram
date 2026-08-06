package main

// 030 US1 category-conditional entity grouping for multi-hop questions
// (specs/030, contracts/evidence-assembly.md structure=entity, research
// decision 4). Multi-hop answers depend on a bridge entity connecting two
// evidence records, so grouping candidate hits by a lifted entity makes that
// bridge visible to the answering model instead of burying it in a flat list.
// Deterministic: groups sorted by coverage (member count) then name; units
// without a liftable entity sort last under an explicit [ungrouped] block.
// Engine untouched (FR-001).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// entityGroup is one lifted entity and its member hits.
type entityGroup struct {
	entity string
	hits   []memory.Result
}

// partitionByEntity splits hits into per-entity groups (first-encounter order)
// and an ungrouped remainder. Ordering within groups and across groups is left
// to the caller so the two render paths stay consistent.
func partitionByEntity(hits []memory.Result) (groups []*entityGroup, ungrouped []memory.Result) {
	byEntity := make(map[string]*entityGroup)
	for _, h := range hits {
		e := topEntity(h.Content)
		if e == "" {
			ungrouped = append(ungrouped, h)
			continue
		}
		grp := byEntity[e]
		if grp == nil {
			grp = &entityGroup{entity: e}
			byEntity[e] = grp
			groups = append(groups, grp)
		}
		grp.hits = append(grp.hits, h)
	}
	return groups, ungrouped
}

// groupHitsByEntity reorders hits for multi-hop questions: entities with more
// evidence first (coverage desc), then name asc; within a group score desc;
// ungrouped units last, score desc.
func groupHitsByEntity(hits []memory.Result) []memory.Result {
	groups, ungrouped := partitionByEntity(hits)
	for _, grp := range groups {
		sort.SliceStable(grp.hits, func(i, j int) bool { return grp.hits[i].Score > grp.hits[j].Score })
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].hits) != len(groups[j].hits) {
			return len(groups[i].hits) > len(groups[j].hits)
		}
		return groups[i].entity < groups[j].entity
	})
	sort.SliceStable(ungrouped, func(i, j int) bool { return ungrouped[i].Score > ungrouped[j].Score })

	out := make([]memory.Result, 0, len(hits))
	for _, grp := range groups {
		out = append(out, grp.hits...)
	}
	out = append(out, ungrouped...)
	return out
}

// topEntity lifts the most salient liftable entity from a hit's content,
// returning "" when none is liftable (the unit is ungrouped). It reuses the
// title-case detector from the 029 navigation diagnosis, so lift behaviour is
// identical across read-side diagnostics.
func topEntity(content string) string {
	for _, m := range titleRe.FindAllString(content, -1) {
		return m
	}
	return ""
}

// buildEntityAnswerPrompt renders entity-grouped hits with group headers so the
// answering model sees which evidence belongs to which entity. Format mirrors
// buildSweepAnswerPrompt's group-render pattern (headers + numbered lines) and
// keeps the legacy [event:]/[recorded:] line shape via toMemories.
func buildEntityAnswerPrompt(question string, hits []memory.Result, currentDate string) string {
	groups, ungrouped := partitionByEntity(hits)

	var b strings.Builder
	writeCurrentDateHeader(&b, currentDate)
	b.WriteString("RETRIEVED MEMORIES (grouped by entity):\n")
	if len(hits) == 0 {
		b.WriteString("(none)\n")
	}
	position := 1
	writeGroup := func(label string, members []memory.Result) {
		if len(members) == 0 {
			return
		}
		fmt.Fprintf(&b, "[%s]\n", label)
		for _, h := range members {
			mem := toMemories([]memory.Result{h})[0]
			fmt.Fprintf(&b, "%d. %s\n", position, mem.Line())
			position++
		}
	}
	for _, grp := range groups {
		writeGroup("entity: "+grp.entity, grp.hits)
	}
	writeGroup("ungrouped", ungrouped)
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nAnswer:", question)
	return b.String()
}
