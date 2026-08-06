package main

// 030 US3 conditional consolidation (specs/030, contracts/consolidation.md,
// data-model.md ConsolidationOperator). Retain or Consolidate (2607.17545)
// shows the operator's value flips sign with budget pressure: consolidation
// helps when raw evidence does NOT fit, retention wins when it does. engram's
// cap (3600) is loose, so consolidation is DEFAULT OFF and only applies when
// the evidence provably exceeds the cap AND --consolidate is set. Within
// budget this is a byte-identical no-op (SC-004). Engine untouched (FR-001).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// consolidateConfig is the compression decision context.
type consolidateConfig struct {
	Cap  int               // answer-context cap (default 3600)
	Call usageModelCaller  // compression generator (opt-in sidecar); nil → deterministic truncation
}

// consolidationSystemPrompt asks the sidecar for a compact evidence replacement
// that stays within the budget while preserving every fact needed to answer.
const consolidationSystemPrompt = `You compress retrieved evidence into a compact form that still supports answering the question.
Emit STRICT JSON only, no prose: {"compact_evidence": [{"text": "..."}, ...]}
Rules:
- Preserve every fact needed to answer the question; drop redundancy.
- Never invent facts absent from the provided evidence.
- Keep it as short as possible while remaining sufficient.`

// consolidationUserPrompt renders the question and the over-cap evidence units.
func consolidationUserPrompt(question string, units []EvidenceUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "QUESTION: %s\n\nEVIDENCE:\n", question)
	for _, u := range units {
		fmt.Fprintf(&b, "[%s] %s\n", u.SourceID, renderUnitLine(u))
	}
	return b.String()
}

// consolidateUnits compresses units that exceed the cap (contracts/
// consolidation.md when/which). Within budget it is a byte-identical no-op
// (retain raw — the default). Over budget: with a Call it asks the sidecar for
// a compact replacement; without one it deterministically truncates to the cap
// (the legacy behaviour). Returns the resulting units, the replaced unit IDs
// (audit), and whether compression was applied.
func consolidateUnits(ctx context.Context, question string, units []EvidenceUnit, cfg consolidateConfig) ([]EvidenceUnit, []string, bool, error) {
	if totalUnitTokens(units) <= cfg.Cap {
		return units, nil, false, nil
	}
	if cfg.Call == nil {
		truncated, replaced := truncateUnits(units, cfg.Cap)
		return truncated, replaced, true, nil
	}
	raw, _, err := cfg.Call(ctx, consolidationSystemPrompt, consolidationUserPrompt(question, units))
	if err != nil {
		truncated, replaced := truncateUnits(units, cfg.Cap)
		return truncated, replaced, true, nil
	}
	compact := parseCompactEvidence(raw, units)
	if len(compact) == 0 {
		truncated, replaced := truncateUnits(units, cfg.Cap)
		return truncated, replaced, true, nil
	}
	return compact, allUnitIDs(units), true, nil
}

// totalUnitTokens sums the per-unit estimate ledger (deterministic ordering
// and the consolidation trigger). The exact full-prompt count is the assembly
// TotalTokens; this per-unit sum is the local, offline proxy for the trigger.
func totalUnitTokens(units []EvidenceUnit) int {
	total := 0
	for _, u := range units {
		total += u.TokenCount
	}
	return total
}

// truncateUnits drops the lowest-priority tail units until the estimate sum
// fits the cap (deterministic fallback when no sidecar is configured).
func truncateUnits(units []EvidenceUnit, cap int) ([]EvidenceUnit, []string) {
	total := 0
	for i, u := range units {
		if total+u.TokenCount > cap {
			return units[:i], allUnitIDs(units[i:])
		}
		total += u.TokenCount
	}
	return units, nil
}

// parseCompactEvidence reads the sidecar's {"compact_evidence": [...]} output
// into consolidated EvidenceUnit values (kind=consolidated, estimated ledger).
// Empty/invalid output returns nil → caller falls back to truncation.
func parseCompactEvidence(raw string, units []EvidenceUnit) []EvidenceUnit {
	var out struct {
		CompactEvidence []struct {
			Text string `json:"text"`
		} `json:"compact_evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	if len(out.CompactEvidence) == 0 {
		return nil
	}
	compact := make([]EvidenceUnit, 0, len(out.CompactEvidence))
	for _, ce := range out.CompactEvidence {
		if strings.TrimSpace(ce.Text) == "" {
			continue
		}
		u := EvidenceUnit{SourceID: "consolidated", Text: ce.Text, Kind: "consolidated", Estimated: true}
		u.TokenCount = estimateTokens(renderUnitLine(u))
		compact = append(compact, u)
	}
	return compact
}

// allUnitIDs collects every unit's source ID.
func allUnitIDs(units []EvidenceUnit) []string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.SourceID)
	}
	return ids
}
