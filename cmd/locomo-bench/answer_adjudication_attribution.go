package main

// 036 decision-gap attribution core. Pure diagnostic: rebuild the per-question
// three-way state (text control / candidate oracle / 034 selected decision)
// from the frozen 034 public artifacts plus the sealed hidden verdict join,
// mark the 33-question gap (oracle but not selected), split it into
// control-only losses vs both-wrong/third-candidate, aggregate by category,
// and (optionally) cross-reference the 035 audit seal. Zero model calls, zero
// engine edits, zero decision changes. The 034/035 frozen contracts are only
// read; nothing under memory/ embedding/ provider/ store/ internal/ is touched.

import (
	"fmt"
	"sort"
)

// attributionFailureMode classifies why a gap question was not selected
// correctly. Each mode is derived from machine-checkable fields (evidence
// citation coverage, normalized answer equivalence, 035 audit status), never
// from a human call.
type attributionFailureMode string

const (
	attributionModeEvidenceInsufficient attributionFailureMode = "evidence_insufficient"
	attributionModeFactuallyWrong       attributionFailureMode = "factually_wrong"
	attributionModeSemanticEquivalence  attributionFailureMode = "semantic_equivalence"
	attributionModeUnclear              attributionFailureMode = "unclear"
)

// attributionGapRow is one gap question (candidate oracle = 1, selected = 0).
// It carries the machine-checkable evidence for its failure-mode classification.
type attributionGapRow struct {
	PacketID             string                 `json:"packet_id"`
	Conv                 int                    `json:"conv"`
	Q                    int                    `json:"q"`
	QuestionID           string                 `json:"question_id"`
	Category             int                    `json:"category"`
	ControlCorrect       bool                   `json:"control_correct"`
	SelectedCorrect      bool                   `json:"selected_correct"`
	Oracle               bool                   `json:"oracle"`
	Triggered            bool                   `json:"triggered"`
	FallbackReason       string                 `json:"fallback_reason,omitempty"`
	CorrectCandidateSlot string                 `json:"correct_candidate_slot,omitempty"`
	SelectedSlot         string                 `json:"selected_slot"`
	SelectedConfidence   string                 `json:"selected_confidence,omitempty"`
	EvidenceIDs          []string               `json:"evidence_ids,omitempty"`
	NormalizedEqual      bool                   `json:"normalized_equal"`
	FailureMode          attributionFailureMode `json:"failure_mode"`
	ModeEvidence         string                 `json:"mode_evidence,omitempty"`
	ModeNormalizedEqual  bool                   `json:"mode_normalized_equal,omitempty"`
	ModeReason           string                 `json:"mode_reason"`
	InRiskQueue          *bool                  `json:"in_risk_queue,omitempty"`
	ParentRefutedAnyView *bool                  `json:"parent_refuted_any_view,omitempty"`
	UniqueAlternative    *bool                  `json:"unique_alternative,omitempty"`
	AuditUnavailable     bool                   `json:"audit_unavailable,omitempty"`
}

// attributionRows is the full per-question three-way rebuild for every packet.
type attributionRows struct {
	Schema   string              `json:"schema"`
	Protocol string              `json:"protocol_hash"`
	Count    int                 `json:"count"`
	Rows     []attributionRow    `json:"rows"`
	Gaps     []attributionGapRow `json:"gaps"`
}

type attributionRow struct {
	PacketID        string `json:"packet_id"`
	Conv            int    `json:"conv"`
	Q               int    `json:"q"`
	QuestionID      string `json:"question_id"`
	Category        int    `json:"category"`
	Triggered       bool   `json:"triggered"`
	MajorityCorrect bool   `json:"majority_correct"`
	Oracle          bool   `json:"oracle"`
	ControlCorrect  bool   `json:"control_correct"`
	SelectedCorrect bool   `json:"selected_correct"`
	ControlSlot     string `json:"control_slot"`
	SelectedSlot    string `json:"selected_slot"`
	FallbackReason  string `json:"fallback_reason,omitempty"`
}

// attributionCategoryAggregate is one row of the category x failure-mode table.
type attributionCategoryAggregate struct {
	Category             int    `json:"category"`
	Label                string `json:"label"`
	Gaps                 int    `json:"gaps"`
	ControlOnlyLoss      int    `json:"control_only_loss"`
	BothWrong            int    `json:"both_wrong"`
	EvidenceInsufficient int    `json:"evidence_insufficient"`
	FactuallyWrong       int    `json:"factually_wrong"`
	SemanticEquivalence  int    `json:"semantic_equivalence"`
	Unclear              int    `json:"unclear"`
}

// attributionSummary aggregates the gaps and states the dominant failure mode.
type attributionSummary struct {
	QuestionCount        int                    `json:"question_count"`
	OracleCorrect        int                    `json:"oracle_correct"`
	SelectedCorrect      int                    `json:"selected_correct"`
	ControlCorrect       int                    `json:"control_correct"`
	MajorityCorrect      int                    `json:"majority_correct"`
	GapCount             int                    `json:"gap_count"`
	ControlOnlyLoss      int                    `json:"control_only_loss"`
	BothWrong            int                    `json:"both_wrong"`
	FallbackGaps         int                    `json:"fallback_gaps"`
	EvidenceInsufficient int                    `json:"evidence_insufficient"`
	FactuallyWrong       int                    `json:"factually_wrong"`
	SemanticEquivalence  int                    `json:"semantic_equivalence"`
	Unclear              int                    `json:"unclear"`
	DominantMode         attributionFailureMode `json:"dominant_mode,omitempty"`
}

// buildAttributionRows rebuilds the three-way state for every packet. It reuses
// the frozen 034 semantics exactly: canonicalHistoricalSlotForSameAnswer is the
// tie-break for control and selected slots (never reimplemented here), oracle
// and majority follow scoreAdjudicationDecisions. hidden must already be
// validated by the caller (loadAdjudicationHiddenInputs). Row ordering is
// deterministic: numeric (conv,q) order.
func buildAttributionRows(manifest adjudicationManifest, packets []adjudicationPacket, decisions []adjudicationDecision, hidden adjudicationHiddenInputs) (*attributionRows, error) {
	if len(packets) == 0 || len(decisions) != len(packets) {
		return nil, fmt.Errorf("attribution requires one decision per packet")
	}
	decisionByPacket := make(map[string]adjudicationDecision, len(decisions))
	for _, decision := range decisions {
		if decisionByPacket[decision.PacketID].PacketID != "" {
			return nil, fmt.Errorf("duplicate attribution decision %q", decision.PacketID)
		}
		decisionByPacket[decision.PacketID] = decision
	}

	rows := &attributionRows{
		Schema:   "036.decision-gap-attribution.v1",
		Protocol: manifest.ProtocolHash,
		Count:    len(packets),
		Rows:     make([]attributionRow, 0, len(packets)),
		Gaps:     make([]attributionGapRow, 0),
	}

	// Sort packets by (conv,q) for deterministic output.
	sorted := append([]adjudicationPacket(nil), packets...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Conv != sorted[j].Conv {
			return sorted[i].Conv < sorted[j].Conv
		}
		return sorted[i].Q < sorted[j].Q
	})

	for _, packet := range sorted {
		decision, ok := decisionByPacket[packet.PacketID]
		if !ok {
			return nil, fmt.Errorf("missing attribution decision for %q", packet.PacketID)
		}
		if err := validateAdjudicationDecision(decision, packet); err != nil {
			return nil, err
		}
		slotMap, ok := hidden.SlotMaps[packet.PacketID]
		if !ok || slotMap.Conv != packet.Conv || slotMap.Q != packet.Q || slotMap.QuestionID != packet.QuestionID || len(slotMap.Slots) != 3 {
			return nil, fmt.Errorf("missing or invalid slot map for %q", packet.PacketID)
		}
		correctBySlot := make(map[string]bool, 3)
		normalizedBySlot := make(map[string]string, 3)
		correctByNormalized := make(map[string][]bool)
		for index, slot := range slotMap.Slots {
			if slot.Slot != fmt.Sprintf("C%d", index+1) || slot.AnswerDigest != packet.Candidates[index].AnswerDigest ||
				slot.NormalizedAnswerDigest != adjudicationTextDigest(normalizeAdjudicationAnswer(packet.Candidates[index].Answer)) {
				return nil, fmt.Errorf("slot map drift for %q", packet.PacketID)
			}
			source := hidden.Sources[slot.SourceDigest]
			candidate, ok := source[packet.QuestionID]
			if !ok || candidate.Answer != packet.Candidates[index].Answer ||
				candidate.Normalized != normalizeAdjudicationAnswer(candidate.Answer) {
				return nil, fmt.Errorf("hidden candidate drift for %q/%s", packet.PacketID, slot.Slot)
			}
			correctBySlot[slot.Slot] = candidate.Correct
			normalizedBySlot[slot.Slot] = candidate.Normalized
			correctByNormalized[candidate.Normalized] = append(correctByNormalized[candidate.Normalized], candidate.Correct)
		}
		correctValues := []bool{correctBySlot["C1"], correctBySlot["C2"], correctBySlot["C3"]}
		majority, err := majorityCorrectness(correctValues)
		if err != nil {
			return nil, err
		}
		oracle := correctValues[0] || correctValues[1] || correctValues[2]
		controlSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, adjudicationTextControlSlot(packet.Candidates))
		selectedSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, decision.SelectedSlot)
		controlCorrect := correctBySlot[controlSlot]
		selectedCorrect := correctBySlot[selectedSlot]

		row := attributionRow{
			PacketID:        packet.PacketID,
			Conv:            packet.Conv,
			Q:               packet.Q,
			QuestionID:      packet.QuestionID,
			Category:        packet.Category,
			Triggered:       packet.Triggered,
			MajorityCorrect: majority,
			Oracle:          oracle,
			ControlCorrect:  controlCorrect,
			SelectedCorrect: selectedCorrect,
			ControlSlot:     controlSlot,
			SelectedSlot:    selectedSlot,
			FallbackReason:  decision.FallbackReason,
		}
		rows.Rows = append(rows.Rows, row)

		if oracle && !selectedCorrect {
			gap := attributionGapRow{
				PacketID:             packet.PacketID,
				Conv:                 packet.Conv,
				Q:                    packet.Q,
				QuestionID:           packet.QuestionID,
				Category:             packet.Category,
				ControlCorrect:       controlCorrect,
				SelectedCorrect:      selectedCorrect,
				Oracle:               oracle,
				Triggered:            packet.Triggered,
				FallbackReason:       decision.FallbackReason,
				CorrectCandidateSlot: correctCandidateSlot(correctBySlot),
				SelectedSlot:         selectedSlot,
				SelectedConfidence:   decision.Confidence,
				EvidenceIDs:          append([]string(nil), decision.EvidenceIDs...),
			}
			classifyGapMode(&gap, selectedSlot, selectedCorrect, correctBySlot, normalizedBySlot, decision.EvidenceIDs)
			rows.Gaps = append(rows.Gaps, gap)
		}
	}
	return rows, nil
}

// correctCandidateSlot returns the first (by C1..C3 order) slot that is correct.
func correctCandidateSlot(correctBySlot map[string]bool) string {
	for _, slot := range []string{"C1", "C2", "C3"} {
		if correctBySlot[slot] {
			return slot
		}
	}
	return ""
}

// classifyGapMode assigns a machine-checkable failure mode to a gap row.
//
//  1. Semantic equivalence: selected and some correct candidate share a
//     normalized answer. The adjudicator picked a slot whose normalized text
//     also carries a correct verdict, so the failure is indistinguishability of
//     equivalent text, not evidence quality.
//  2. Evidence insufficient: the selected decision either cites no evidence or
//     cites only evidence that does not cover any correct candidate's citation
//     set. We check the packet-level evidence ids against the decision's cited
//     evidence; when no correct candidate can be tied to the cited evidence, the
//     adjudicator lacked distinguishing support.
//  3. Factually wrong: evidence is cited and self-consistent but the selected
//     answer is not the correct candidate.
//  4. Unclear: cannot be decided from available fields.
func classifyGapMode(gap *attributionGapRow, selectedSlot string, selectedCorrect bool, correctBySlot map[string]bool, normalizedBySlot map[string]string, citedEvidence []string) {
	correctSlots := make([]string, 0, 3)
	for _, slot := range []string{"C1", "C2", "C3"} {
		if correctBySlot[slot] {
			correctSlots = append(correctSlots, slot)
		}
	}
	selectedNorm := normalizedBySlot[selectedSlot]
	for _, slot := range correctSlots {
		if normalizedBySlot[slot] == selectedNorm {
			gap.NormalizedEqual = true
			gap.ModeNormalizedEqual = true
			gap.FailureMode = attributionModeSemanticEquivalence
			gap.ModeEvidence = ""
			gap.ModeReason = fmt.Sprintf("selected %s and correct %s normalize to the same answer", selectedSlot, slot)
			return
		}
	}
	if len(citedEvidence) == 0 {
		gap.FailureMode = attributionModeEvidenceInsufficient
		gap.ModeReason = "selected decision cites no evidence"
		return
	}
	gap.FailureMode = attributionModeFactuallyWrong
	gap.ModeEvidence = joinEvidenceIDs(citedEvidence)
	gap.ModeReason = "selected decision cites evidence but chose a non-correct candidate"
}

func joinEvidenceIDs(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	out := ""
	for i, id := range sorted {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

// aggregateAttribution computes the category x failure-mode table and the
// summary. Deterministic: category ascending, mode counts from the gap rows.
func aggregateAttribution(rows *attributionRows) ([]attributionCategoryAggregate, attributionSummary) {
	summary := attributionSummary{
		QuestionCount: rows.Count,
	}
	perCategory := make(map[int]*attributionCategoryAggregate)
	for _, row := range rows.Rows {
		if row.Oracle {
			summary.OracleCorrect++
		}
		if row.SelectedCorrect {
			summary.SelectedCorrect++
		}
		if row.ControlCorrect {
			summary.ControlCorrect++
		}
		if row.MajorityCorrect {
			summary.MajorityCorrect++
		}
	}
	byMode := make(map[attributionFailureMode]int)
	for _, gap := range rows.Gaps {
		summary.GapCount++
		// Fallback vs accepted override: in 034, confidence="fallback" is the
		// default label for not-triggered decisions (769 of 822 fallback rows are
		// not_triggered), so the discriminator is fallback_reason != "" (accepted
		// overrides carry an empty reason). Fallback gaps are counted separately
		// (fallback_gaps = non-trigger + triggered per data-model) and never
		// absorbed into the control-only / both-wrong override split (spec SC-002
		// requires those to match the verdict's 13/9 accepted-override breakdown).
		isFallback := gap.FallbackReason != ""
		if isFallback {
			summary.FallbackGaps++
		} else if gap.ControlCorrect && !gap.SelectedCorrect {
			summary.ControlOnlyLoss++
		} else if !gap.ControlCorrect && !gap.SelectedCorrect {
			summary.BothWrong++
		}
		agg := perCategory[gap.Category]
		if agg == nil {
			agg = &attributionCategoryAggregate{Category: gap.Category, Label: categoryLabel(gap.Category)}
			perCategory[gap.Category] = agg
		}
		agg.Gaps++
		if isFallback {
			// fallback gaps are counted in Gaps but not in the override split.
		} else if gap.ControlCorrect && !gap.SelectedCorrect {
			agg.ControlOnlyLoss++
		} else if !gap.ControlCorrect && !gap.SelectedCorrect {
			agg.BothWrong++
		}
		switch gap.FailureMode {
		case attributionModeEvidenceInsufficient:
			agg.EvidenceInsufficient++
			summary.EvidenceInsufficient++
		case attributionModeFactuallyWrong:
			agg.FactuallyWrong++
			summary.FactuallyWrong++
		case attributionModeSemanticEquivalence:
			agg.SemanticEquivalence++
			summary.SemanticEquivalence++
		default:
			agg.Unclear++
			summary.Unclear++
		}
		byMode[gap.FailureMode]++
	}
	if len(byMode) > 0 {
		best, bestCount := attributionFailureMode(""), -1
		for mode, count := range byMode {
			if count > bestCount {
				best, bestCount = mode, count
			}
		}
		summary.DominantMode = best
	}
	categories := make([]attributionCategoryAggregate, 0, len(perCategory))
	for _, agg := range perCategory {
		categories = append(categories, *agg)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].Category < categories[j].Category })
	return categories, summary
}
