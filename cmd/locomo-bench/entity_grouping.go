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

// indexedEntityHit keeps the input ordinal as the final deterministic
// tie-break without changing memory.Result or the engine contract.
type indexedEntityHit struct {
	hit     memory.Result
	ordinal int
}

// sortIndexedEntityHits applies the canonical member order: score desc,
// stable source ID asc, then original ordinal for malformed duplicate IDs.
func sortIndexedEntityHits(hits []indexedEntityHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].hit.Score != hits[j].hit.Score {
			return hits[i].hit.Score > hits[j].hit.Score
		}
		if hits[i].hit.Name != hits[j].hit.Name {
			return hits[i].hit.Name < hits[j].hit.Name
		}
		return hits[i].ordinal < hits[j].ordinal
	})
}

// groupHitsByEntity constructs the canonical multi-hop flat sequence. Entity
// coverage is computed over the complete input closure (the pre-033 grouping
// semantic), but output is globally kind-layered: every chunk precedes every
// fact. Within each kind layer, groups are coverage desc then entity asc;
// members are score desc, SourceID asc, ordinal asc; ungrouped hits are last.
func groupHitsByEntity(hits []memory.Result) []memory.Result {
	type indexedGroup struct {
		entity  string
		members []indexedEntityHit
	}

	byEntity := make(map[string]*indexedGroup)
	groups := make([]*indexedGroup, 0)
	ungrouped := make([]indexedEntityHit, 0)
	for ordinal, hit := range hits {
		indexed := indexedEntityHit{hit: hit, ordinal: ordinal}
		entity := topEntity(hit.Content)
		if entity == "" {
			ungrouped = append(ungrouped, indexed)
			continue
		}
		group := byEntity[entity]
		if group == nil {
			group = &indexedGroup{entity: entity}
			byEntity[entity] = group
			groups = append(groups, group)
		}
		group.members = append(group.members, indexed)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].members) != len(groups[j].members) {
			return len(groups[i].members) > len(groups[j].members)
		}
		return groups[i].entity < groups[j].entity
	})
	for _, group := range groups {
		sortIndexedEntityHits(group.members)
	}
	sortIndexedEntityHits(ungrouped)

	out := make([]memory.Result, 0, len(hits))
	for _, kind := range []string{"chunk", "fact"} {
		for _, group := range groups {
			for _, member := range group.members {
				if kindOfEvidence(member.hit.Name) == kind {
					out = append(out, member.hit)
				}
			}
		}
		for _, member := range ungrouped {
			if kindOfEvidence(member.hit.Name) == kind {
				out = append(out, member.hit)
			}
		}
	}
	return out
}

// legacyGroupHitsByEntity is the exact pre-033 sorter retained only for the
// explicit benchmark control: group-major coverage order, score-only stable
// member order, and score-only stable ungrouped tail.
func legacyGroupHitsByEntity(hits []memory.Result) []memory.Result {
	groups, ungrouped := partitionByEntity(hits)
	for _, group := range groups {
		sort.SliceStable(group.hits, func(i, j int) bool {
			return group.hits[i].Score > group.hits[j].Score
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].hits) != len(groups[j].hits) {
			return len(groups[i].hits) > len(groups[j].hits)
		}
		return groups[i].entity < groups[j].entity
	})
	sort.SliceStable(ungrouped, func(i, j int) bool {
		return ungrouped[i].Score > ungrouped[j].Score
	})

	out := make([]memory.Result, 0, len(hits))
	for _, group := range groups {
		out = append(out, group.hits...)
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

// buildEntityAnswerPrompt streams the canonical flat sequence without
// partitioning or sorting it again. It may repeat an entity header when a kind
// layer changes; evidence lines always remain byte-for-byte in input order.
func buildEntityAnswerPrompt(question string, hits []memory.Result, currentDate string) string {
	var b strings.Builder
	writeCurrentDateHeader(&b, currentDate)
	b.WriteString("RETRIEVED MEMORIES (grouped by entity):\n")
	if len(hits) == 0 {
		b.WriteString("(none)\n")
	}
	lastKind, lastEntity := "", ""
	for position, hit := range hits {
		kind := kindOfEvidence(hit.Name)
		entity := topEntity(hit.Content)
		if kind != lastKind || entity != lastEntity {
			label := "ungrouped"
			if entity != "" {
				label = "entity: " + entity
			}
			fmt.Fprintf(&b, "[%s]\n", label)
			lastKind, lastEntity = kind, entity
		}
		mem := toMemories([]memory.Result{hit})[0]
		fmt.Fprintf(&b, "%d. %s\n", position+1, mem.Line())
	}
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nAnswer:", question)
	return b.String()
}

// buildLegacyEntityAnswerPrompt preserves the pre-033 group-major renderer
// for the benchmark-only legacy control. Production assembly never calls it
// unless that explicit control mode is selected.
func buildLegacyEntityAnswerPrompt(question string, hits []memory.Result, currentDate string) string {
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
		for _, hit := range members {
			mem := toMemories([]memory.Result{hit})[0]
			fmt.Fprintf(&b, "%d. %s\n", position, mem.Line())
			position++
		}
	}
	for _, group := range groups {
		writeGroup("entity: "+group.entity, group.hits)
	}
	writeGroup("ungrouped", ungrouped)
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nAnswer:", question)
	return b.String()
}
