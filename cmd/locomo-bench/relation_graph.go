package main

// 031 read-side evidence relation assembly (specs/031, contracts/
// evidence-relations.md, data-model.md). Between 030 assembly and answer
// generation this stage annotates the assembled evidence with explicit
// inter-evidence relations (related_to / temporal_next / caused_by) and renders
// them as a structural-context block appended to the answerer prompt. Pure Go,
// offline, deterministic, default OFF — when off the assembled path is
// byte-identical (SC-004). Engine untouched (FR-008).

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// RelationType is the explicit relation between two evidence units.
type RelationType string

const (
	RelationRelatedTo    RelationType = "related_to"
	RelationTemporalNext RelationType = "temporal_next"
	RelationCausedBy     RelationType = "caused_by"
)

// Per-evidence out-edge caps (data-model.md / contracts §6, FR-005).
const (
	relationCapRelatedTo    = 4
	relationCapTemporalNext = 1
	relationCapCausedBy     = 2
	relationEntityCap       = 5 // per-evidence entity cap (research R-2)
	// relationMaxEdges caps the total edges in a block. A dense
	// single-conversation candidate set (many facts sharing the participants)
	// can otherwise emit dozens of low-value edges that crowd out the evidence
	// itself — token budget discipline (FR-005 / R-4).
	relationMaxEdges = 24
)

// RelationEdge is one explicit relation between two evidence units
// (data-model.md). From ≠ To, at most one Type per (From, To), directed for
// temporal_next (early→late) and caused_by (cause→effect); related_to is
// undirected and emitted once per pair (lexicographic From side).
type RelationEdge struct {
	From     string       `json:"from"`
	To       string       `json:"to"`
	Type     RelationType `json:"type"`
	Evidence string       `json:"evidence"` // shared entity / date pair / causal word
	Rank     int          `json:"rank"`     // display order within its type (0 = first)
}

// StructuralContextBlock is the assembled, rendered relation annotation
// appended to the answerer prompt (data-model.md).
type StructuralContextBlock struct {
	Category   int            `json:"category"` // temporal | multi-hop
	Edges      []RelationEdge `json:"edges"`
	Text       string         `json:"text"`
	TokenCount int            `json:"token_count"`
}

// causalIndicatorWords is the built-in deterministic causal dictionary
// (research R-3). Fixed order → deterministic "first hit" matching. Matching is
// a lowercase substring scan; a caused_by edge additionally requires a shared
// core entity between the two evidence texts (R-3 double condition).
var causalIndicatorWords = []string{
	"because", "due to", "led to", "caused by", "resulted in", "therefore",
	"as a result", "consequently", "thus", "triggered", "in response to",
	// NOTE: "since" is deliberately absent — in LoCoMo/English it is primarily a
	// temporal preposition ("since 2023"), which fires spurious caused_by edges
	// at every dated fact (observed: a single run produced 17 since-caused_by
	// edges all pointing at one shared evidence).
}

// speakerRe matches a dialogue turn's "Name:" prefix (LoCoMo chunks carry
// speaker labels at line starts). upperTokenRe catches capitalized single
// tokens (proper nouns in dialogue). Both are deterministic (research R-2).
var (
	speakerRe    = regexp.MustCompile(`(?:^|\n)[ \t]*([A-Z][A-Za-z]+):[ \t]`)
	upperTokenRe = regexp.MustCompile(`\b[A-Z][a-z]{4,}\b`)
	// quotedRe / titleRe extract quoted spans and capitalized title phrases for
	// relation/entity grouping (moved from the removed 029 nav adapter).
	quotedRe = regexp.MustCompile(`"([^"]{2,})"`)
	titleRe  = regexp.MustCompile(`[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+`)
)

// relationStopWords filters sentence-initial filler / dialogue-register words
// that are capitalized in turn text but are not content entities. The list is
// deliberately conservative: leftover noise stays bounded by the per-evidence
// entity cap and only *shared* entities ever build edges.
var relationStopWords = map[string]bool{
	"I": true, "A": true, "An": true, "The": true, "We": true, "You": true,
	"He": true, "She": true, "It": true, "They": true, "So": true, "And": true,
	"But": true, "Or": true, "Oh": true, "Hey": true, "Hi": true, "Yeah": true,
	"No": true, "Yes": true, "Ok": true, "Okay": true, "Well": true, "Look": true,
	"Good": true, "Great": true, "Thanks": true, "Bye": true, "Wait": true,
	"Wow": true, "Ugh": true, "Just": true, "Then": true, "Now": true,
	"Still": true, "Maybe": true, "Probably": true, "Also": true, "Even": true,
	"Please": true, "Sure": true, "Right": true, "Long": true, "Been": true,
	"Lots": true, "Little": true, "Last": true, "Next": true, "This": true,
	"That": true, "These": true, "Those": true, "My": true, "Your": true,
	"His": true, "Her": true, "Our": true, "Their": true, "Its": true,
	"What": true, "When": true, "Where": true, "Who": true, "How": true,
	"Why": true, "There": true, "Here": true, "Not": true, "Really": true,
	"Anyways": true, "Actually": true, "Apparently": true, "Congrats": true,
	"Congratulation": true, "Sounds": true, "Sound": true, "Glad": true,
	"Anything": true, "Something": true, "Everything": true, "Nothing": true,
	"Take": true, "Takes": true, "Taken": true, "Took": true, "Going": true,
	"Went": true, "Come": true, "Comes": true, "Coming": true, "Came": true,
	"Make": true, "Makes": true, "Made": true, "Making": true, "Need": true,
	"Needs": true, "Needed": true, "Think": true, "Thinks": true,
	"Thought": true, "Thinking": true, "Want": true, "Wants": true,
	"Wanted": true, "Get": true, "Gets": true, "Got": true, "Getting": true,
	"Start": true, "Starts": true, "Started": true, "Starting": true,
	"Hope": true, "Hopes": true, "Hoped": true, "Hoping": true,
	"Sorry": true, "Missed": true, "Miss": true, "Missing": true,
}

// extractEntities lifts content entities from a single evidence text (029
// extractEntitiesFromHits pattern): quoted phrases, title-case multi-word
// spans, and capitalized single tokens — minus the dialogue "Name:" speakers
// and sentence-initial stop words. Deduped, capped per evidence (R-2). In a
// single-conversation candidate set every chunk shares the speakers, so they
// are excluded: they would otherwise flood related_to with meaningless
// co-occurrence. Returns entities in first-occurrence order.
func extractEntities(text string) []string {
	speakers := make(map[string]bool)
	for _, m := range speakerRe.FindAllStringSubmatch(text, -1) {
		speakers[m[1]] = true
	}
	var entities []string
	seen := make(map[string]bool)
	add := func(e string) {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] || len(strings.Fields(e)) > 6 {
			return
		}
		if speakers[e] {
			return
		}
		if relationStopWords[strings.Fields(e)[0]] {
			return
		}
		seen[e] = true
		entities = append(entities, e)
	}
	for _, q := range quotedRe.FindAllString(text, -1) {
		add(strings.Trim(q, `"`))
	}
	for _, t := range titleRe.FindAllString(text, -1) {
		add(t)
	}
	for _, m := range upperTokenRe.FindAllString(text, -1) {
		add(m)
	}
	if len(entities) > relationEntityCap {
		entities = entities[:relationEntityCap]
	}
	return entities
}

// hitCausalWord returns the first causal indicator present in the lowercase
// text ("" when none). Fixed dictionary order → deterministic.
func hitCausalWord(text string) string {
	lower := strings.ToLower(text)
	for _, word := range causalIndicatorWords {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}

// relationNode is one evidence unit's relation-computation state.
type relationNode struct {
	hit      memory.Result
	entities []string
	dateKey  string
	dated    bool
	causal   string
}

// computeRelationContext computes the inter-evidence relations for the ordered
// candidate evidence and renders the structural-context block (contracts §2).
// Only multi-hop (related_to + caused_by) and temporal (temporal_next)
// categories produce a block; everything else, and any empty relation set,
// returns nil (fail-soft, FR-006). Deterministic: stable sorts + lexicographic
// tie-breaks, no randomness.
func computeRelationContext(ctx context.Context, hits []memory.Result, category int) (*StructuralContextBlock, error) {
	if category != assemblyCategoryMultiHop && category != temporalCategory {
		return nil, nil
	}
	if len(hits) < 2 {
		return nil, nil
	}
	nodes := make([]relationNode, len(hits))
	entityAt := make(map[string][]int) // entity → node indexes
	for i, h := range hits {
		es := extractEntities(h.Content)
		nodes[i] = relationNode{hit: h, entities: es, causal: hitCausalWord(h.Content)}
		for _, e := range es {
			entityAt[e] = append(entityAt[e], i)
		}
		if d, ok := assemblyDateRank(h); ok {
			nodes[i].dateKey, nodes[i].dated = d, true
		}
	}

	var edges []RelationEdge
	switch category {
	case assemblyCategoryMultiHop:
		edges = relationEdgesMultiHop(nodes, entityAt)
	case temporalCategory:
		edges = relationEdgesTemporal(nodes)
	}
	if len(edges) == 0 {
		return nil, nil
	}
	if len(edges) > relationMaxEdges {
		edges = edges[:relationMaxEdges] // global block cap (R-4 + token discipline)
	}
	for i := range edges {
		edges[i].Rank = i
	}
	block := &StructuralContextBlock{Category: category, Edges: edges}
	block.Text = renderRelationBlock(block)
	block.TokenCount = estimateTokens(block.Text)
	return block, nil
}

// sharedPair is an (i, j) evidence pair sharing at least one entity.
type sharedPair struct {
	i, j   int
	shared []string // shared entity names, sorted for determinism
}

// relationEdgesMultiHop builds related_to + caused_by edges for a multi-hop
// question: shared entities connect related_to, shared entity + causal
// indicator connects caused_by (R-3 double condition). Both edge kinds are
// sorted deterministically and capped per evidence out-edge (FR-005).
func relationEdgesMultiHop(nodes []relationNode, entityAt map[string][]int) []RelationEdge {
	sharedByPair := make(map[[2]int][]string)
	for e, idxs := range entityAt {
		for a := 0; a < len(idxs); a++ {
			for b := a + 1; b < len(idxs); b++ {
				i, j := idxs[a], idxs[b]
				if nodes[i].hit.Name > nodes[j].hit.Name {
					i, j = j, i // normalize by NAME, not index → order-independent
				}
				sharedByPair[[2]int{i, j}] = append(sharedByPair[[2]int{i, j}], e)
			}
		}
	}
	pairs := make([]sharedPair, 0, len(sharedByPair))
	for k, v := range sharedByPair {
		sorted := append([]string(nil), v...)
		sort.Strings(sorted)
		pairs = append(pairs, sharedPair{i: k[0], j: k[1], shared: sorted})
	}
	// Related pairs first (higher shared-entity count); ties by From then To
	// name (NOT input index) so the output is independent of hit ordering — the
	// assembled `ordered` set is reordered from the raw hits, and the relation
	// block must depend only on evidence content (determinism contract §5).
	sort.Slice(pairs, func(a, b int) bool {
		if len(pairs[a].shared) != len(pairs[b].shared) {
			return len(pairs[a].shared) > len(pairs[b].shared)
		}
		an, bn := nodes[pairs[a].i].hit.Name, nodes[pairs[b].i].hit.Name
		if an != bn {
			return an < bn
		}
		return nodes[pairs[a].j].hit.Name < nodes[pairs[b].j].hit.Name
	})

	// related_to: undirected single edge per pair, lexicographic From side.
	// Evidence = first shared entity (already sorted).
	var edges []RelationEdge
	relatedCount := make(map[string]int)
	for _, p := range pairs {
		from, to := p.i, p.j
		if nodes[from].hit.Name > nodes[to].hit.Name {
			from, to = to, from
		}
		if relatedCount[nodes[from].hit.Name] >= relationCapRelatedTo {
			continue
		}
		relatedCount[nodes[from].hit.Name]++
		edges = append(edges, RelationEdge{
			From:     nodes[from].hit.Name,
			To:       nodes[to].hit.Name,
			Type:     RelationRelatedTo,
			Evidence: p.shared[0],
		})
	}

	// caused_by: shared core entity AND ≥1 causal indicator (R-3). Direction:
	// the causal-bearing evidence is the effect side (To) — e.g. "B due to A" /
	// "A caused B" place B after the indicator. When both carry an indicator,
	// tie-break lexicographic From. Cap per evidence out-edge.
	causedCount := make(map[string]int)
	for _, p := range pairs {
		i, j := p.i, p.j
		ci, cj := nodes[i].causal, nodes[j].causal
		if ci == "" && cj == "" {
			continue
		}
		from, to, word := i, j, cj
		switch {
		case ci != "" && cj != "":
			if nodes[i].hit.Name > nodes[j].hit.Name {
				from, to = j, i
			}
			word = ci
		case ci != "":
			from, to, word = j, i, ci // i is the effect side
		}
		if causedCount[nodes[from].hit.Name] >= relationCapCausedBy {
			continue
		}
		causedCount[nodes[from].hit.Name]++
		edges = append(edges, RelationEdge{
			From:     nodes[from].hit.Name,
			To:       nodes[to].hit.Name,
			Type:     RelationCausedBy,
			Evidence: word,
		})
	}
	return edges
}

// relationEdgesTemporal builds temporal_next edges: dated units sorted
// ascending by date key, each connected to its nearest strict successor
// (A.date < B.date). Undated units sort last and get no temporal_next edge
// (data-model.md). Cap is inherent (chain → ≤1 out-edge per evidence).
func relationEdgesTemporal(nodes []relationNode) []RelationEdge {
	dated := make([]int, 0, len(nodes))
	for i := range nodes {
		if nodes[i].dated {
			dated = append(dated, i)
		}
	}
	sort.SliceStable(dated, func(a, b int) bool {
		if nodes[dated[a]].dateKey != nodes[dated[b]].dateKey {
			return nodes[dated[a]].dateKey < nodes[dated[b]].dateKey
		}
		return nodes[dated[a]].hit.Name < nodes[dated[b]].hit.Name
	})
	var edges []RelationEdge
	for k := 0; k+1 < len(dated); k++ {
		a, b := dated[k], dated[k+1]
		if nodes[a].dateKey == nodes[b].dateKey {
			continue // same date key is not a strict successor
		}
		edges = append(edges, RelationEdge{
			From:     nodes[a].hit.Name,
			To:       nodes[b].hit.Name,
			Type:     RelationTemporalNext,
			Evidence: nodes[a].dateKey + " → " + nodes[b].dateKey,
		})
	}
	return edges
}

// renderRelationBlock renders the edges into the [relations] structural-context
// block (contracts §2): one line per edge "From --Type(依据)--> To", in edge
// order (already deterministically sorted by the caller).
func renderRelationBlock(b *StructuralContextBlock) string {
	if len(b.Edges) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[relations]\n")
	for _, e := range b.Edges {
		fmt.Fprintf(&sb, "%s --%s(%s)--> %s\n", e.From, e.Type, e.Evidence, e.To)
	}
	sb.WriteString("[/relations]")
	return sb.String()
}

// appendRelationBlock appends a rendered relation block to a user prompt,
// separated by a single newline. An empty block leaves the prompt untouched.
func appendRelationBlock(user string, block *StructuralContextBlock) string {
	if block == nil || block.Text == "" {
		return user
	}
	return strings.TrimRight(user, "\n") + "\n" + block.Text
}

// relationBlockWithinBoundary keeps only edges whose endpoints lie inside the
// closed candidate boundary (T011 / R-5: fail-closed reuse of the trace gate).
// Returns nil when nothing survives. The caller renders/accounts a fresh block.
func relationBlockWithinBoundary(block *StructuralContextBlock, boundary map[string]bool) *StructuralContextBlock {
	if block == nil {
		return nil
	}
	kept := make([]RelationEdge, 0, len(block.Edges))
	for _, e := range block.Edges {
		if boundary[e.From] && boundary[e.To] {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	block.Edges = kept
	return block
}
