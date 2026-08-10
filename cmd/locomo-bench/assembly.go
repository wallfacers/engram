package main

// 030 US1 evidence assembly (specs/030, contracts/evidence-assembly.md,
// data-model.md). The read-side pipeline sits between retrieval and answer
// generation: it turns the retrieved candidate set into a token-exact,
// chunk-first, category-structured EvidenceAssembly that is the only source of
// the answerer context. All flags default OFF; when off the legacy path is
// byte-identical (SC-004 parity). Engine untouched (FR-001).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// kindOfEvidence reports whether an entry is a verbatim chunk or an extracted
// fact, by the store's naming convention (chunks.go:210 uses the same prefix).
func kindOfEvidence(name string) string {
	if strings.HasPrefix(name, "chunk-") {
		return "chunk"
	}
	return "fact"
}

// EvidenceUnit is the smallest assembly unit: one candidate (chunk or fact)
// with its token accounting (data-model.md).
type EvidenceUnit struct {
	SourceID   string  `json:"source_id"`
	Text       string  `json:"text"`
	Kind       string  `json:"kind"` // chunk | fact | consolidated
	TokenCount int     `json:"token_count"`
	Estimated  bool    `json:"estimated,omitempty"` // true when token_count is an estimate (per-unit sort/truncation bookkeeping)
	Score      float64 `json:"score,omitempty"`
	EventDate  string  `json:"event_date,omitempty"`
	Recorded   string  `json:"recorded,omitempty"`
}

// EvidenceAssembly is the assembler output — a token-exact, category-structured
// ordered evidence package (data-model.md). TotalTokens is the exact count of
// the full rendered answer prompt (single chat-aware /tokenize call), NOT the
// sum of per-unit estimates (evidencecompiler discipline: never derive a total
// by summing parts). Units are ordered chunk-first + category structure.
type EvidenceAssembly struct {
	QuestionID              string                  `json:"question_id"`
	Category                int                     `json:"category"`
	InputCandidateCount     int                     `json:"input_candidate_count"`
	InputClosureSHA256      string                  `json:"input_closure_sha256"`
	EntityOrder             string                  `json:"entity_order"` // kind_layered | legacy_grouped | not_applicable
	PromptOrderMatchesUnits bool                    `json:"prompt_order_matches_units"`
	Units                   []EvidenceUnit          `json:"units"`
	Structure               string                  `json:"structure"` // temporal | entity | generic
	TotalTokens             int                     `json:"total_tokens"`
	Cap                     int                     `json:"cap"`
	ChunkFraction           float64                 `json:"chunk_fraction"`
	TokensEstimated         bool                    `json:"tokens_estimated"`         // exact counter unavailable; TotalTokens is an estimate
	RelationBlock           *StructuralContextBlock `json:"relation_block,omitempty"` // 031: rendered structural-context block (nil when off / empty / non-applicable)
}

// chunkTokenFraction returns the chunk-token share of the assembled units, the
// SC-002 metric (fixes 029's ~1% fact-dominated context). Returns 0 when there
// is no text (all-chunk all-empty or empty units).
func (a *EvidenceAssembly) chunkTokenFraction() float64 {
	chunkTokens, totalTokens := 0, 0
	for _, u := range a.Units {
		if u.TokenCount < 1 {
			continue
		}
		totalTokens += u.TokenCount
		if u.Kind == "chunk" {
			chunkTokens += u.TokenCount
		}
	}
	if totalTokens == 0 {
		return 0
	}
	return float64(chunkTokens) / float64(totalTokens)
}

// LoCoMo category constants for the assembly structure router. temporal is the
// pre-existing temporalCategory (2); the others are named here for the router.
const (
	assemblyCategoryMultiHop   = 1
	assemblyCategoryOpenDomain = 3
	assemblyCategorySingleHop  = 4

	assemblyEntityOrderKindLayered   = "kind_layered"
	assemblyEntityOrderLegacyGrouped = "legacy_grouped"
	assemblyEntityOrderNotApplicable = "not_applicable"
)

// assemblyConfig carries the fixed per-question context the assembler needs.
type assemblyConfig struct {
	Cap             int    // answer-context token cap (default 3600)
	CurrentDate     string // rendered "CURRENT DATE" header
	Scaffold        bool   // --temporal-date-scaffold (gates the TIMELINE block)
	SystemPrompt    string // answerer system prompt (exact full-prompt count)
	QuestionID      string // conv-N-q-M for diagnostics
	RelationEnabled bool   // 031: append the structural-context relation block (default off; parity)
	EntityOrder     string // 033: kind_layered (default) | legacy_grouped benchmark control
}

// assembleEvidence builds the answer context from retrieved hits (contracts/
// evidence-assembly.md, data-model.md). Returns the assembly record and the
// rendered user prompt.
//
// Ordering is deterministic and offline: chunk units before facts, then a
// per-category key (temporal → event date asc, multi-hop → kind-layered entity
// grouping, generic → score desc). The canonical ordered slice is created once
// and every cap iteration removes only its tail before the renderer streams the
// remaining prefix. The exact token count of the full rendered prompt is
// taken once from the chat-aware tokenizer; when it exceeds the cap the last
// unit is dropped and the prompt re-rendered until it fits (bounded by the
// unit count). With no exact counter the assembly reports an estimate ledger
// and marks tokens_estimated=true (constitution V explicit degradation).
func assembleEvidence(ctx context.Context, question string, hits []memory.Result, category int, cfg assemblyConfig, counter *assemblyTokenCounter) (EvidenceAssembly, string, error) {
	inputCandidateCount := len(hits)
	inputClosureSHA256 := evidenceInputClosureSHA256(hits)
	entityOrder := normalizedAssemblyEntityOrder(category, cfg.EntityOrder)
	ordered := orderHitsForAssembly(hits, category, entityOrder)
	for {
		user := renderAssembledPrompt(question, ordered, category, cfg, entityOrder)
		var relationBlock *StructuralContextBlock
		if cfg.RelationEnabled {
			// 031 (contracts/evidence-relations.md §2): compute the inter-evidence
			// relations over the current ordered set and append the rendered block.
			// Recomputed per truncation round so the block shrinks/disappears with
			// the evidence it annotates (token budget discipline, FR-005/FR-006).
			if block, _ := computeRelationContext(ctx, ordered, category); block != nil {
				user = appendRelationBlock(user, block)
				relationBlock = block
			}
		}
		asm := finishAssembly(ordered, category, cfg, estimateTotalTokens(ordered))
		asm.InputCandidateCount = inputCandidateCount
		asm.InputClosureSHA256 = inputClosureSHA256
		asm.EntityOrder = entityOrder
		asm.PromptOrderMatchesUnits = promptEvidenceLinesMatchUnits(user, asm.Units)
		asm.RelationBlock = relationBlock
		if counter == nil {
			asm.TokensEstimated = true
			return asm, user, nil
		}
		count, ok, err := counter.countPrompt(ctx, cfg.SystemPrompt, user)
		if err != nil {
			return EvidenceAssembly{}, "", err
		}
		if !ok {
			asm.TokensEstimated = true
			return asm, user, nil
		}
		asm.TotalTokens = count
		if count <= cfg.Cap || len(ordered) == 0 {
			return asm, user, nil
		}
		// Over cap: drop the last (lowest-priority) unit and re-render.
		ordered = ordered[:len(ordered)-1]
	}
}

// promptEvidenceLinesMatchUnits independently parses the rendered numbered
// evidence lines and checks them against the assembly ledger. Headers and the
// optional relation block are ignored; missing, extra, or reordered evidence
// lines fail the audit bit.
func promptEvidenceLinesMatchUnits(user string, units []EvidenceUnit) bool {
	offset := 0
	for position, unit := range units {
		marker := fmt.Sprintf("%d. %s", position+1, renderUnitLine(unit))
		relative := strings.Index(user[offset:], marker)
		if relative < 0 {
			return false
		}
		offset += relative + len(marker)
	}
	return true
}

// orderHitsForAssembly applies the deterministic read-side ordering
// (contracts/evidence-assembly.md step 2 + 3).
func orderHitsForAssembly(hits []memory.Result, category int, entityOrder string) []memory.Result {
	ordered := make([]memory.Result, len(hits))
	copy(ordered, hits)
	if category == assemblyCategoryMultiHop {
		if entityOrder == assemblyEntityOrderLegacyGrouped {
			return legacyGroupHitsByEntity(ordered)
		}
		return groupHitsByEntity(ordered)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ik, jk := kindRank(kindOfEvidence(ordered[i].Name)), kindRank(kindOfEvidence(ordered[j].Name)); ik != jk {
			return ik < jk
		}
		if category == temporalCategory {
			di, dOk := assemblyDateRank(ordered[i])
			dj, djOk := assemblyDateRank(ordered[j])
			switch {
			case dOk && djOk:
				if di != dj {
					return di < dj
				}
			case dOk != djOk:
				return dOk // dated units before undated
			}
		}
		return ordered[i].Score > ordered[j].Score
	})
	return ordered
}

// normalizedAssemblyEntityOrder makes the default repaired mode explicit for
// multi-hop audits while keeping non-multi assembly records byte-identical
// across the ignored benchmark flag.
func normalizedAssemblyEntityOrder(category int, configured string) string {
	if category != assemblyCategoryMultiHop {
		return assemblyEntityOrderNotApplicable
	}
	if configured == assemblyEntityOrderLegacyGrouped {
		return assemblyEntityOrderLegacyGrouped
	}
	return assemblyEntityOrderKindLayered
}

// evidenceInputClosureSHA256 fingerprints the unordered candidate ID/text
// multiset. Sorting includes duplicates, so reorderings preserve the digest
// while additions, removals, or rewrites change it.
func evidenceInputClosureSHA256(hits []memory.Result) string {
	items := make([]string, 0, len(hits))
	for _, hit := range hits {
		items = append(items, hit.Name+"\x00"+hit.Content)
	}
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\x00")))
	return hex.EncodeToString(sum[:])
}

// kindRank gives chunk units priority over facts (FR-003).
func kindRank(kind string) int {
	switch kind {
	case "chunk":
		return 0
	case "fact":
		return 1
	default: // consolidated and anything else sort last
		return 2
	}
}

// assemblyDateRank returns the unit's event date key ("" when undated) and
// whether a usable date is present.
func assemblyDateRank(m memory.Result) (string, bool) {
	if m.EventDate == nil || m.EventDate.IsZero() {
		return "", false
	}
	return m.EventDate.Format("2006-01-02"), true
}

// classifyHits converts ordered hits into token-ledgered units (per-unit
// estimate for sort/truncate bookkeeping; the assembly TotalTokens is the exact
// full-prompt count, never this sum).
func classifyHits(hits []memory.Result) []EvidenceUnit {
	units := make([]EvidenceUnit, 0, len(hits))
	for _, h := range hits {
		u := EvidenceUnit{
			SourceID:  h.Name,
			Text:      h.Content,
			Kind:      kindOfEvidence(h.Name),
			Score:     h.Score,
			Estimated: true, // per-unit ledger is always an estimate; TotalTokens is the exact count
		}
		if d, ok := assemblyDateRank(h); ok {
			u.EventDate = d
		}
		if !h.CreatedAt.IsZero() {
			u.Recorded = h.CreatedAt.Format("2006-01-02")
		}
		u.TokenCount = estimateTokens(renderUnitLine(u))
		units = append(units, u)
	}
	return units
}

// renderUnitLine renders one unit in the same [event:]/[recorded:] line shape
// the answering model already consumes (retrievedMemory.Line).
func renderUnitLine(u EvidenceUnit) string {
	return (retrievedMemory{
		Name: u.SourceID, Content: u.Text, EventDate: u.EventDate, Recorded: u.Recorded,
	}).Line()
}

// estimateTotalTokens sums the per-unit estimates for the fallback ledger.
func estimateTotalTokens(hits []memory.Result) int {
	total := 0
	for _, u := range classifyHits(hits) {
		total += u.TokenCount
	}
	return total
}

// renderAssembledPrompt renders the ordered hits into the answerer user prompt.
// multi-hop streams the canonical entity-grouped sequence without repartition
// or sort; all other categories reuse the legacy prompt builder
// (buildAnswerContextPrompt), which keeps the format byte-compatible and lets
// the temporal TIMELINE block apply when scaffolded.
func renderAssembledPrompt(question string, ordered []memory.Result, category int, cfg assemblyConfig, entityOrder string) string {
	if category == assemblyCategoryMultiHop {
		if entityOrder == assemblyEntityOrderLegacyGrouped {
			return buildLegacyEntityAnswerPrompt(question, ordered, cfg.CurrentDate)
		}
		return buildEntityAnswerPrompt(question, ordered, cfg.CurrentDate)
	}
	return buildAnswerContextPrompt(question, ordered, cfg.CurrentDate, category, cfg.Scaffold)
}

// finishAssembly records the non-token fields of an assembly from ordered hits.
func finishAssembly(ordered []memory.Result, category int, cfg assemblyConfig, totalTokens int) EvidenceAssembly {
	asm := EvidenceAssembly{
		QuestionID:  cfg.QuestionID,
		Category:    category,
		Units:       classifyHits(ordered),
		Structure:   structureForCategory(category),
		TotalTokens: totalTokens,
		Cap:         cfg.Cap,
	}
	asm.ChunkFraction = asm.chunkTokenFraction()
	return asm
}

// structureForCategory maps a LoCoMo category to its assembly structure
// (contracts/evidence-assembly.md, FR-004).
func structureForCategory(category int) string {
	switch category {
	case temporalCategory:
		return "temporal"
	case assemblyCategoryMultiHop:
		return "entity"
	default:
		return "generic"
	}
}

// unitsToResults converts assembled units back into answer-context results
// (used after consolidation to render the compact replacement through the
// existing prompt builders).
func unitsToResults(units []EvidenceUnit) []memory.Result {
	out := make([]memory.Result, 0, len(units))
	for _, u := range units {
		out = append(out, memory.Result{Name: u.SourceID, Content: u.Text, Score: u.Score})
	}
	return out
}
