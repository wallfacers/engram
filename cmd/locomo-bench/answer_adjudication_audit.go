package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const adjudicationAuditEntailmentSystemPrompt = `You are the entailment-first evidence auditor. Assess every supplied answer slot independently using only E01-E30. Return strict JSON with exactly assessments. Each assessment has slot, support, and contradiction; support and contradiction each have value yes, no, or unclear and evidence_ids. A yes requires direct citations. A no or unclear requires an empty citation list. Do not recommend an answer and do not write a new answer.`

const adjudicationAuditFalsificationSystemPrompt = `You are the falsification-first evidence auditor. Try to falsify and support every supplied answer slot independently using only E01-E30. Return strict JSON with exactly assessments. Each assessment has slot, support, and contradiction; support and contradiction each have value yes, no, or unclear and evidence_ids. A yes requires direct citations. A no or unclear requires an empty citation list. Do not recommend an answer and do not write a new answer.`

const adjudicationAuditResolverContract = `retain-parent-v1;switch-iff-both-valid-views-current-contradicted-unsupported-and-same-unique-alternative-supported-uncontradicted`

func adjudicationAuditPromptDigest(viewID string) string {
	switch viewID {
	case adjudicationAuditViewEntailment:
		return adjudicationTextDigest(adjudicationAuditEntailmentSystemPrompt + "\x00audit-view-json-v1")
	case adjudicationAuditViewFalsification:
		return adjudicationTextDigest(adjudicationAuditFalsificationSystemPrompt + "\x00audit-view-json-v1")
	default:
		return ""
	}
}

func adjudicationAuditResolverDigest() string {
	return adjudicationTextDigest(adjudicationAuditResolverContract)
}

func buildAdjudicationAuditPrompt(packet adjudicationAuditPacket, viewID string) (string, error) {
	view, ok := findAdjudicationAuditView(packet, viewID)
	if !ok {
		return "", fmt.Errorf("unknown adjudication audit view")
	}
	input := struct {
		Role       string                           `json:"role"`
		Question   string                           `json:"question"`
		Category   int                              `json:"category"`
		Evidence   []adjudicationEvidenceItem       `json:"evidence"`
		Candidates []adjudicationAuditViewCandidate `json:"candidates"`
	}{Role: view.ViewID, Question: packet.Question, Category: packet.Category, Evidence: packet.Evidence, Candidates: view.Candidates}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(input); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

func parseAdjudicationAuditResponse(raw string, packet adjudicationAuditPacket, viewID string) (adjudicationAuditResponse, error) {
	var response adjudicationAuditResponse
	view, ok := findAdjudicationAuditView(packet, viewID)
	if !ok {
		return response, fmt.Errorf("unknown adjudication audit view")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, fmt.Errorf("decode adjudication audit response: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return response, err
	}
	if err := validateAdjudicationAuditAssessments(response.Assessments, packet, view); err != nil {
		return response, err
	}
	return response, nil
}

func validateAdjudicationAuditAssessments(assessments []adjudicationAuditCandidateAssessment, packet adjudicationAuditPacket, view adjudicationAuditView) error {
	if len(assessments) != len(view.Candidates) {
		return fmt.Errorf("audit response must assess every view slot")
	}
	validEvidence := make(map[string]bool, len(packet.Evidence))
	for _, item := range packet.Evidence {
		validEvidence[item.EvidenceID] = true
	}
	seenSlots := make(map[string]bool, len(assessments))
	validSlots := make(map[string]bool, len(view.Candidates))
	for _, candidate := range view.Candidates {
		validSlots[candidate.Slot] = true
	}
	for _, assessment := range assessments {
		if !validSlots[assessment.Slot] || seenSlots[assessment.Slot] {
			return fmt.Errorf("invalid or duplicate audit assessment slot")
		}
		seenSlots[assessment.Slot] = true
		if err := validateAdjudicationAuditAxis(assessment.Support, validEvidence); err != nil {
			return fmt.Errorf("invalid support for %s: %w", assessment.Slot, err)
		}
		if err := validateAdjudicationAuditAxis(assessment.Contradiction, validEvidence); err != nil {
			return fmt.Errorf("invalid contradiction for %s: %w", assessment.Slot, err)
		}
	}
	return nil
}

func validateAdjudicationAuditAxis(axis adjudicationAuditAxis, validEvidence map[string]bool) error {
	switch axis.Value {
	case "yes":
		if len(axis.EvidenceIDs) == 0 {
			return fmt.Errorf("yes requires evidence")
		}
	case "no", "unclear":
		if len(axis.EvidenceIDs) != 0 {
			return fmt.Errorf("no/unclear require an empty evidence list")
		}
	default:
		return fmt.Errorf("unknown closed audit value")
	}
	seen := make(map[string]bool, len(axis.EvidenceIDs))
	for _, evidenceID := range axis.EvidenceIDs {
		if !validEvidence[evidenceID] || seen[evidenceID] {
			return fmt.Errorf("invalid or duplicate audit evidence ID")
		}
		seen[evidenceID] = true
	}
	return nil
}

func adjudicationAuditRunIdentityDigest(config adjudicationRunConfig, manifest adjudicationAuditManifest) string {
	return evalJSONDigest(struct {
		ProtocolHash              string `json:"protocol_hash"`
		Provider                  string `json:"provider"`
		BaseURLDigest             string `json:"base_url_digest"`
		Model                     string `json:"model"`
		ModelRevision             string `json:"model_revision"`
		MaxTokens                 int    `json:"max_tokens"`
		BinaryDigest              string `json:"binary_digest"`
		EntailmentPromptDigest    string `json:"entailment_prompt_digest"`
		FalsificationPromptDigest string `json:"falsification_prompt_digest"`
	}{
		ProtocolHash: manifest.ProtocolHash, Provider: config.Provider, BaseURLDigest: config.BaseURLDigest,
		Model: config.Model, ModelRevision: config.ModelRevision, MaxTokens: config.MaxTokens,
		BinaryDigest: config.BinaryDigest, EntailmentPromptDigest: manifest.EntailmentPromptDigest,
		FalsificationPromptDigest: manifest.FalsificationPromptDigest,
	})
}

func adjudicationAuditInputDigest(packet adjudicationAuditPacket, view adjudicationAuditView, runIdentity string) string {
	return evalJSONDigest(struct {
		RunIdentity  string `json:"run_identity"`
		PacketDigest string `json:"packet_digest"`
		ViewDigest   string `json:"view_digest"`
	}{RunIdentity: runIdentity, PacketDigest: packet.PacketDigest, ViewDigest: view.ViewDigest})
}

func resolveAdjudicationAuditDecision(record adjudicationAuditResolverMapRecord, packet adjudicationAuditPacket, terminals []adjudicationAuditCallRecord) (adjudicationAuditDecision, error) {
	if !record.Risk || packet.PacketID != record.PacketID || len(terminals) != 2 {
		return adjudicationAuditDecision{}, fmt.Errorf("risk resolver requires one packet and two terminals")
	}
	terminalByView := make(map[string]adjudicationAuditCallRecord, 2)
	inputTokens, outputTokens := 0, 0
	for _, terminal := range terminals {
		view, ok := findAdjudicationAuditView(packet, terminal.ViewID)
		if !ok || terminal.PacketID != packet.PacketID || terminal.PacketDigest != packet.PacketDigest ||
			terminal.ViewDigest != view.ViewDigest || terminal.TerminalDigest != adjudicationAuditCallTerminalDigest(terminal) ||
			terminalByView[terminal.ViewID].PacketID != "" ||
			(terminal.State != adjudicationAuditCallCompleted && terminal.State != adjudicationAuditCallFailed) {
			return adjudicationAuditDecision{}, fmt.Errorf("invalid audit resolver terminal")
		}
		terminalByView[terminal.ViewID] = terminal
		inputTokens += terminal.InputTokens
		outputTokens += terminal.OutputTokens
	}
	ordered := []adjudicationAuditCallRecord{
		terminalByView[adjudicationAuditViewEntailment], terminalByView[adjudicationAuditViewFalsification],
	}
	if ordered[0].PacketID == "" || ordered[1].PacketID == "" {
		return adjudicationAuditDecision{}, fmt.Errorf("audit resolver lacks both view terminals")
	}
	decision := adjudicationAuditDecision{
		Schema: adjudicationAuditDecisionSchema, ProtocolHash: packet.ProtocolHash, PacketID: record.PacketID,
		ParentPacketDigest: record.ParentPacketDigest, ParentDecisionDigest: record.ParentDecisionDigest,
		Conv: record.Conv, Q: record.Q, QuestionID: record.QuestionID, FinalParentSlot: record.ParentSelectedSlot,
		FinalAnswerDigest: record.ParentSelectedAnswerDigest, FinalGroupDigest: record.ParentSelectedGroupDigest,
		AuditTerminalDigests: []string{ordered[0].TerminalDigest, ordered[1].TerminalDigest},
		Resolution:           adjudicationAuditResolutionRetained, ResolutionReason: "audit_not_dually_convergent",
		ProviderAttempts: 2, InputTokens: inputTokens, OutputTokens: outputTokens,
	}
	if ordered[0].State == adjudicationAuditCallFailed || ordered[1].State == adjudicationAuditCallFailed {
		decision.ResolutionReason = "terminal_failure"
		decision.DecisionDigest = adjudicationAuditDecisionDigest(decision)
		return decision, nil
	}
	currentRefuted := true
	var commonAlternative string
	for viewIndex, terminal := range ordered {
		viewMap := record.ViewMaps[viewIndex]
		assessmentByGroup := make(map[string]adjudicationAuditCandidateAssessment, len(terminal.Assessments))
		for _, assessment := range terminal.Assessments {
			groupDigest := viewMap.SlotToGroup[assessment.Slot]
			if groupDigest == "" || assessmentByGroup[groupDigest].Slot != "" {
				return adjudicationAuditDecision{}, fmt.Errorf("invalid assessment-to-group mapping")
			}
			assessmentByGroup[groupDigest] = assessment
		}
		current := assessmentByGroup[record.ParentSelectedGroupDigest]
		if current.Contradiction.Value != "yes" || current.Support.Value == "yes" {
			currentRefuted = false
		}
		alternatives := make([]string, 0, 2)
		for groupDigest, assessment := range assessmentByGroup {
			if groupDigest != record.ParentSelectedGroupDigest && assessment.Support.Value == "yes" &&
				assessment.Contradiction.Value != "yes" {
				alternatives = append(alternatives, groupDigest)
			}
		}
		if len(alternatives) != 1 {
			commonAlternative = ""
			currentRefuted = false
			continue
		}
		if viewIndex == 0 {
			commonAlternative = alternatives[0]
		} else if commonAlternative != alternatives[0] {
			commonAlternative = ""
			currentRefuted = false
		}
	}
	if currentRefuted && commonAlternative != "" {
		for _, group := range record.Groups {
			if group.GroupDigest == commonAlternative {
				decision.FinalParentSlot = group.RepresentativeParentSlot
				decision.FinalAnswerDigest = group.RepresentativeAnswerDigest
				decision.FinalGroupDigest = group.GroupDigest
				decision.Resolution = adjudicationAuditResolutionSwitched
				decision.ResolutionReason = "dual_convergence"
				break
			}
		}
	}
	decision.DecisionDigest = adjudicationAuditDecisionDigest(decision)
	return decision, nil
}

func retainedNonriskAdjudicationAuditDecision(record adjudicationAuditResolverMapRecord) adjudicationAuditDecision {
	decision := adjudicationAuditDecision{
		Schema: adjudicationAuditDecisionSchema, ProtocolHash: record.ProtocolHash, PacketID: record.PacketID,
		ParentPacketDigest: record.ParentPacketDigest, ParentDecisionDigest: record.ParentDecisionDigest,
		Conv: record.Conv, Q: record.Q, QuestionID: record.QuestionID, FinalParentSlot: record.ParentSelectedSlot,
		FinalAnswerDigest: record.ParentSelectedAnswerDigest, FinalGroupDigest: record.ParentSelectedGroupDigest,
		AuditTerminalDigests: []string{}, Resolution: adjudicationAuditResolutionRetainedNonrisk,
		ResolutionReason: "not_in_risk_queue",
	}
	decision.DecisionDigest = adjudicationAuditDecisionDigest(decision)
	return decision
}

type adjudicationAuditInstabilityScore struct {
	Total               int `json:"total"`
	Triggered           int `json:"triggered"`
	NewLower            int `json:"new_lower"`
	NewUpper            int `json:"new_upper"`
	TriggeredMixedLower int `json:"triggered_mixed_lower"`
	TriggeredMixedUpper int `json:"triggered_mixed_upper"`
}

type adjudicationAuditPairedScore struct {
	ParentOnly int     `json:"parent_only"`
	NewOnly    int     `json:"new_only"`
	McNemarP   float64 `json:"mcnemar_p"`
}

type adjudicationAuditStage0Score struct {
	Schema                      string                            `json:"schema"`
	ResultKind                  string                            `json:"result_kind"`
	ProtocolHash                string                            `json:"protocol_hash"`
	SealDigest                  string                            `json:"seal_digest"`
	ParentDecisionSetDigest     string                            `json:"parent_decision_set_digest"`
	DecisionSetDigest           string                            `json:"decision_set_digest"`
	QuestionCount               int                               `json:"question_count"`
	RiskCount                   int                               `json:"risk_count"`
	ViewCount                   int                               `json:"view_count"`
	ContextParityCount          int                               `json:"context_parity_count"`
	TriggeredContextParityCount int                               `json:"triggered_context_parity_count"`
	Parent                      adjudicationScoreCount            `json:"parent_034_historical_mapping"`
	New                         adjudicationScoreCount            `json:"new_historical_mapping"`
	TriggeredMixedParent        adjudicationScoreCount            `json:"triggered_mixed_parent"`
	TriggeredMixedNew           adjudicationScoreCount            `json:"triggered_mixed_new"`
	JudgeInstability            adjudicationAuditInstabilityScore `json:"judge_instability"`
	Paired                      adjudicationAuditPairedScore      `json:"new_vs_parent"`
	Categories                  []evalCategoryGateResult          `json:"categories"`
	TemporalNet                 int                               `json:"temporal_net"`
	CompletedCalls              int                               `json:"completed_calls"`
	FailedCalls                 int                               `json:"failed_calls"`
	RetainedCount               int                               `json:"retained_count"`
	SwitchedCount               int                               `json:"switched_count"`
	ProviderAttempts            int                               `json:"provider_attempts"`
	InputTokens                 int                               `json:"input_tokens"`
	OutputTokens                int                               `json:"output_tokens"`
	PricingStatus               string                            `json:"pricing_status"`
	EstimatedCNY                *float64                          `json:"estimated_cny,omitempty"`
	IntegrityValid              bool                              `json:"integrity_valid"`
	FrozenDiagnosticsValid      bool                              `json:"frozen_diagnostics_valid"`
	GateReasons                 []string                          `json:"gate_reasons"`
	Verdict                     string                            `json:"verdict"`
}

type adjudicationAuditStage0GateInput struct {
	NewCorrect             int
	NewLower               int
	QuestionCount          int
	MixedCorrect           int
	MixedLower             int
	MixedTotal             int
	NewOnly                int
	ParentOnly             int
	McNemarP               float64
	TemporalNet            int
	IntegrityValid         bool
	FrozenDiagnosticsValid bool
	Categories             map[string]evalCategoryGateResult
}

func adjudicationAuditStage0Verdict(input adjudicationAuditStage0GateInput) string {
	if !input.IntegrityValid || !input.FrozenDiagnosticsValid || input.QuestionCount != adjudicationAuditFrozenQuestionCount ||
		input.NewCorrect < 1387 || input.NewLower < 1387 || input.MixedTotal != 88 || input.MixedCorrect < 69 ||
		input.MixedLower < 69 || input.NewOnly-input.ParentOnly < 9 || input.McNemarP >= 0.05 ||
		input.TemporalNet < 0 || hasHolmNegativeRegression(input.Categories) {
		return "NO_GO"
	}
	return "GO"
}

func scoreAdjudicationAuditDecisions(parentManifest adjudicationManifest, parentPackets []adjudicationPacket, parentDecisions []adjudicationDecision, auditManifest adjudicationAuditManifest, auditDecisions []adjudicationAuditDecision, seal adjudicationAuditSeal, hidden adjudicationHiddenInputs, requireFrozen bool) (adjudicationAuditStage0Score, error) {
	report := adjudicationAuditStage0Score{
		Schema: "035.adjudication.audit.stage0-score.v1", ResultKind: "historical_verdict_mapping",
		ProtocolHash: auditManifest.ProtocolHash, SealDigest: seal.SealDigest,
		ParentDecisionSetDigest: auditManifest.Parent.DecisionSetDigest, DecisionSetDigest: seal.DecisionSetDigest,
		QuestionCount: len(parentPackets), RiskCount: auditManifest.RiskCount, ViewCount: auditManifest.ViewCount,
		ContextParityCount:          parentManifest.ContextParityCount,
		TriggeredContextParityCount: parentManifest.TriggeredContextParityCount,
		Parent:                      adjudicationScoreCount{Total: len(parentPackets)}, New: adjudicationScoreCount{Total: len(parentPackets)},
		IntegrityValid: hidden.IntegrityValid && seal.Valid,
		CompletedCalls: seal.CompletedCalls, FailedCalls: seal.FailedCalls, RetainedCount: seal.RetainedCount,
		SwitchedCount: seal.SwitchedCount, ProviderAttempts: seal.ProviderAttempts,
		InputTokens: seal.InputTokens, OutputTokens: seal.OutputTokens,
		PricingStatus: seal.PricingStatus, EstimatedCNY: seal.EstimatedCNY,
	}
	if len(parentPackets) == 0 || len(parentDecisions) != len(parentPackets) || len(auditDecisions) != len(parentPackets) ||
		auditManifest.QuestionCount != len(parentPackets) {
		return report, fmt.Errorf("audit score requires aligned complete decisions")
	}
	parentByPacket := make(map[string]adjudicationDecision, len(parentDecisions))
	auditByPacket := make(map[string]adjudicationAuditDecision, len(auditDecisions))
	for _, decision := range parentDecisions {
		if parentByPacket[decision.PacketID].PacketID != "" {
			return report, fmt.Errorf("duplicate parent score decision")
		}
		parentByPacket[decision.PacketID] = decision
	}
	for _, decision := range auditDecisions {
		if auditByPacket[decision.PacketID].PacketID != "" || decision.DecisionDigest != adjudicationAuditDecisionDigest(decision) {
			return report, fmt.Errorf("duplicate or invalid audit score decision")
		}
		auditByPacket[decision.PacketID] = decision
	}
	parentOutcomes := make([]bool, 0, len(parentPackets))
	newOutcomes := make([]bool, 0, len(parentPackets))
	categoryParent := make(map[int][]bool)
	categoryNew := make(map[int][]bool)
	for _, packet := range parentPackets {
		parentDecision, parentOK := parentByPacket[packet.PacketID]
		auditDecision, auditOK := auditByPacket[packet.PacketID]
		if !parentOK || !auditOK || auditDecision.ParentPacketDigest != packet.PacketDigest ||
			auditDecision.ParentDecisionDigest != parentDecision.DecisionDigest {
			return report, fmt.Errorf("audit score decision identity mismatch")
		}
		if err := validateAdjudicationDecision(parentDecision, packet); err != nil {
			return report, err
		}
		slotMap, ok := hidden.SlotMaps[packet.PacketID]
		if !ok || slotMap.Conv != packet.Conv || slotMap.Q != packet.Q || slotMap.QuestionID != packet.QuestionID || len(slotMap.Slots) != 3 {
			return report, fmt.Errorf("missing audit score slot map")
		}
		correctBySlot := make(map[string]bool, 3)
		normalizedBySlot := make(map[string]string, 3)
		correctByNormalized := make(map[string][]bool)
		for index, slot := range slotMap.Slots {
			if slot.Slot != fmt.Sprintf("C%d", index+1) || slot.AnswerDigest != packet.Candidates[index].AnswerDigest ||
				slot.NormalizedAnswerDigest != adjudicationTextDigest(normalizeAdjudicationAnswer(packet.Candidates[index].Answer)) {
				return report, fmt.Errorf("audit score slot-map drift")
			}
			candidate, ok := hidden.Sources[slot.SourceDigest][packet.QuestionID]
			if !ok || candidate.Answer != packet.Candidates[index].Answer || candidate.Normalized != normalizeAdjudicationAnswer(candidate.Answer) {
				return report, fmt.Errorf("audit score hidden candidate drift")
			}
			correctBySlot[slot.Slot] = candidate.Correct
			normalizedBySlot[slot.Slot] = candidate.Normalized
			correctByNormalized[candidate.Normalized] = append(correctByNormalized[candidate.Normalized], candidate.Correct)
		}
		parentSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, parentDecision.SelectedSlot)
		newSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, auditDecision.FinalParentSlot)
		_, parentSlotOK := correctBySlot[parentSlot]
		_, newSlotOK := correctBySlot[newSlot]
		if !parentSlotOK || !newSlotOK {
			return report, fmt.Errorf("audit score selected an unknown slot")
		}
		parentCorrect := correctBySlot[parentSlot]
		newCorrect := correctBySlot[newSlot]
		if parentCorrect {
			report.Parent.Correct++
		}
		if newCorrect {
			report.New.Correct++
		}
		correctValues := []bool{correctBySlot["C1"], correctBySlot["C2"], correctBySlot["C3"]}
		mixed := correctValues[0] != correctValues[1] || correctValues[0] != correctValues[2]
		if mixed && packet.Triggered {
			report.TriggeredMixedParent.Total++
			report.TriggeredMixedNew.Total++
			if parentCorrect {
				report.TriggeredMixedParent.Correct++
			}
			if newCorrect {
				report.TriggeredMixedNew.Correct++
			}
		}
		unstable := false
		for _, outcomes := range correctByNormalized {
			hasTrue, hasFalse := false, false
			for _, outcome := range outcomes {
				hasTrue = hasTrue || outcome
				hasFalse = hasFalse || !outcome
			}
			unstable = unstable || (hasTrue && hasFalse)
		}
		if unstable {
			report.JudgeInstability.Total++
			if packet.Triggered {
				report.JudgeInstability.Triggered++
			}
		}
		newLower, newUpper := newCorrect, newCorrect
		selectedGroup := correctByNormalized[normalizedBySlot[newSlot]]
		if len(selectedGroup) > 1 {
			newLower, newUpper = true, false
			for _, outcome := range selectedGroup {
				newLower = newLower && outcome
				newUpper = newUpper || outcome
			}
		}
		if newLower {
			report.JudgeInstability.NewLower++
			if mixed && packet.Triggered {
				report.JudgeInstability.TriggeredMixedLower++
			}
		}
		if newUpper {
			report.JudgeInstability.NewUpper++
			if mixed && packet.Triggered {
				report.JudgeInstability.TriggeredMixedUpper++
			}
		}
		parentOutcomes = append(parentOutcomes, parentCorrect)
		newOutcomes = append(newOutcomes, newCorrect)
		categoryParent[packet.Category] = append(categoryParent[packet.Category], parentCorrect)
		categoryNew[packet.Category] = append(categoryNew[packet.Category], newCorrect)
		if packet.Category == 2 {
			if newCorrect && !parentCorrect {
				report.TemporalNet++
			} else if parentCorrect && !newCorrect {
				report.TemporalNet--
			}
		}
	}
	for index := range parentOutcomes {
		switch {
		case parentOutcomes[index] && !newOutcomes[index]:
			report.Paired.ParentOnly++
		case !parentOutcomes[index] && newOutcomes[index]:
			report.Paired.NewOnly++
		}
	}
	pValue, err := exactMcNemarTwoSided(report.Paired.ParentOnly, report.Paired.NewOnly)
	if err != nil {
		return report, err
	}
	report.Paired.McNemarP = pValue
	categoryIDs := make([]int, 0, len(categoryParent))
	for category := range categoryParent {
		categoryIDs = append(categoryIDs, category)
	}
	sort.Ints(categoryIDs)
	comparisons := make([]evalCategoryComparison, 0, len(categoryIDs))
	for _, category := range categoryIDs {
		comparison, err := pairedCategoryComparison(categoryLabel(category), categoryParent[category], categoryNew[category])
		if err != nil {
			return report, err
		}
		comparisons = append(comparisons, comparison)
	}
	categoryGates := holmNegativeCategoryGate(comparisons, 0.05)
	for _, category := range categoryIDs {
		report.Categories = append(report.Categories, categoryGates[categoryLabel(category)])
	}
	report.FrozenDiagnosticsValid = !requireFrozen ||
		(parentManifest.QuestionCount == adjudicationFrozenQuestionCount && parentManifest.TriggeredCount == adjudicationFrozenTriggerCount &&
			parentManifest.ContextParityCount == adjudicationFrozenContextParityCount &&
			parentManifest.TriggeredContextParityCount == adjudicationFrozenTriggeredContextParityCount &&
			report.Parent.Correct == 1378 && report.TriggeredMixedParent.Total == 88 &&
			report.TriggeredMixedParent.Correct == 61 && report.JudgeInstability.Total == 13 &&
			report.JudgeInstability.Triggered == 5 && auditManifest.QuestionCount == adjudicationAuditFrozenQuestionCount &&
			auditManifest.RiskCount == adjudicationAuditFrozenRiskCount && auditManifest.ViewCount == adjudicationAuditFrozenViewCount &&
			seal.ProviderAttempts == adjudicationAuditFrozenViewCount && seal.Retries == 0)
	gate := adjudicationAuditStage0GateInput{
		NewCorrect: report.New.Correct, NewLower: report.JudgeInstability.NewLower, QuestionCount: report.QuestionCount,
		MixedCorrect: report.TriggeredMixedNew.Correct, MixedLower: report.JudgeInstability.TriggeredMixedLower,
		MixedTotal: report.TriggeredMixedNew.Total, NewOnly: report.Paired.NewOnly, ParentOnly: report.Paired.ParentOnly,
		McNemarP: report.Paired.McNemarP, TemporalNet: report.TemporalNet,
		IntegrityValid: report.IntegrityValid, FrozenDiagnosticsValid: report.FrozenDiagnosticsValid, Categories: categoryGates,
	}
	report.Verdict = adjudicationAuditStage0Verdict(gate)
	if report.New.Correct < 1387 {
		report.GateReasons = append(report.GateReasons, "new_mapping_below_1387")
	}
	if report.JudgeInstability.NewLower < 1387 {
		report.GateReasons = append(report.GateReasons, "new_instability_lower_below_1387")
	}
	if report.TriggeredMixedNew.Total != 88 || report.TriggeredMixedNew.Correct < 69 {
		report.GateReasons = append(report.GateReasons, "triggered_mixed_below_69_of_88")
	}
	if report.TriggeredMixedNew.Total != 88 || report.JudgeInstability.TriggeredMixedLower < 69 {
		report.GateReasons = append(report.GateReasons, "triggered_mixed_instability_lower_below_69_of_88")
	}
	if report.Paired.NewOnly-report.Paired.ParentOnly < 9 {
		report.GateReasons = append(report.GateReasons, "paired_net_below_9")
	}
	if report.Paired.McNemarP >= 0.05 {
		report.GateReasons = append(report.GateReasons, "overall_mcnemar_not_significant")
	}
	if report.TemporalNet < 0 {
		report.GateReasons = append(report.GateReasons, "temporal_regression")
	}
	if hasHolmNegativeRegression(categoryGates) {
		report.GateReasons = append(report.GateReasons, "holm_significant_negative_category")
	}
	if !report.IntegrityValid {
		report.GateReasons = append(report.GateReasons, "integrity_invalid")
	}
	if !report.FrozenDiagnosticsValid {
		report.GateReasons = append(report.GateReasons, "frozen_diagnostics_mismatch")
	}
	return report, nil
}

func groupAdjudicationAuditAnswers(packet adjudicationPacket) ([]adjudicationAuditAnswerGroup, error) {
	if len(packet.Candidates) != 3 {
		return nil, fmt.Errorf("audit grouping requires exactly three parent candidates")
	}
	type member struct {
		slot   string
		answer string
		digest string
	}
	byNormalized := make(map[string][]member, 3)
	for index, candidate := range packet.Candidates {
		if candidate.Slot != fmt.Sprintf("C%d", index+1) || strings.TrimSpace(candidate.Answer) == "" ||
			candidate.AnswerDigest != adjudicationTextDigest(candidate.Answer) {
			return nil, fmt.Errorf("invalid parent candidate for audit grouping")
		}
		normalized := normalizeAdjudicationAnswer(candidate.Answer)
		if normalized == "" {
			return nil, fmt.Errorf("empty normalized parent candidate")
		}
		byNormalized[normalized] = append(byNormalized[normalized], member{
			slot: candidate.Slot, answer: candidate.Answer, digest: candidate.AnswerDigest,
		})
	}
	if len(byNormalized) < 1 || len(byNormalized) > 3 {
		return nil, fmt.Errorf("invalid audit answer-group count")
	}
	groups := make([]adjudicationAuditAnswerGroup, 0, len(byNormalized))
	for normalized, members := range byNormalized {
		sort.Slice(members, func(i, j int) bool {
			if members[i].answer != members[j].answer {
				return members[i].answer < members[j].answer
			}
			return members[i].slot < members[j].slot
		})
		memberDigests := make([]string, 0, len(members))
		seenMemberDigests := make(map[string]bool, len(members))
		for _, item := range members {
			if !seenMemberDigests[item.digest] {
				memberDigests = append(memberDigests, item.digest)
				seenMemberDigests[item.digest] = true
			}
		}
		sort.Strings(memberDigests)
		normalizedDigest := adjudicationTextDigest(normalized)
		groupDigest := evalJSONDigest(struct {
			NormalizedDigest    string   `json:"normalized_digest"`
			MemberAnswerDigests []string `json:"member_answer_digests"`
		}{NormalizedDigest: normalizedDigest, MemberAnswerDigests: memberDigests})
		groups = append(groups, adjudicationAuditAnswerGroup{
			GroupDigest: groupDigest, NormalizedDigest: normalizedDigest, MemberAnswerDigests: memberDigests,
			RepresentativeParentSlot: members[0].slot, RepresentativeAnswerDigest: members[0].digest,
			representativeAnswer: members[0].answer,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupDigest < groups[j].GroupDigest })
	return groups, nil
}

func buildAdjudicationAuditViews(seed string, packet adjudicationPacket, groups []adjudicationAuditAnswerGroup) ([]adjudicationAuditView, []adjudicationAuditViewMap, error) {
	if strings.TrimSpace(seed) == "" || len(groups) < 2 || len(groups) > 3 {
		return nil, nil, fmt.Errorf("audit views require a seed and two or three answer groups")
	}
	ordered := append([]adjudicationAuditAnswerGroup(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		left := adjudicationTextDigest(seed + "\x00" + packet.PacketID + "\x00" + ordered[i].GroupDigest)
		right := adjudicationTextDigest(seed + "\x00" + packet.PacketID + "\x00" + ordered[j].GroupDigest)
		if left != right {
			return left < right
		}
		return ordered[i].GroupDigest < ordered[j].GroupDigest
	})
	rotated := append([]adjudicationAuditAnswerGroup(nil), ordered[1:]...)
	rotated = append(rotated, ordered[0])
	orders := [][]adjudicationAuditAnswerGroup{ordered, rotated}
	viewIDs := []string{adjudicationAuditViewEntailment, adjudicationAuditViewFalsification}
	views := make([]adjudicationAuditView, 0, 2)
	viewMaps := make([]adjudicationAuditViewMap, 0, 2)
	for viewIndex, order := range orders {
		view := adjudicationAuditView{ViewID: viewIDs[viewIndex], Candidates: make([]adjudicationAuditViewCandidate, 0, len(order))}
		viewMap := adjudicationAuditViewMap{ViewID: viewIDs[viewIndex], SlotToGroup: make(map[string]string, len(order))}
		for index, group := range order {
			if strings.TrimSpace(group.representativeAnswer) == "" ||
				group.RepresentativeAnswerDigest != adjudicationTextDigest(group.representativeAnswer) {
				return nil, nil, fmt.Errorf("audit answer group lacks representative text")
			}
			slot := fmt.Sprintf("A%d", index+1)
			view.Candidates = append(view.Candidates, adjudicationAuditViewCandidate{Slot: slot, RepresentativeAnswer: group.representativeAnswer})
			viewMap.SlotToGroup[slot] = group.GroupDigest
		}
		view.ViewDigest = adjudicationAuditViewDigest(view)
		views = append(views, view)
		viewMaps = append(viewMaps, viewMap)
	}
	return views, viewMaps, nil
}

func deriveAdjudicationAuditArtifacts(parent adjudicationAuditParentReceipt, parentPackets []adjudicationPacket, parentDecisions []adjudicationDecision, seed string) (adjudicationAuditManifest, []adjudicationAuditPacket, []adjudicationAuditResolverMapRecord, error) {
	var manifest adjudicationAuditManifest
	if strings.TrimSpace(seed) == "" || len(parentPackets) == 0 || len(parentPackets) != len(parentDecisions) {
		return manifest, nil, nil, fmt.Errorf("audit derivation requires aligned parent inputs and seed")
	}
	manifest = adjudicationAuditManifest{
		Schema: adjudicationAuditManifestSchema, ProtocolID: "035-risk-controlled-adjudication-stage0-v1",
		Parent: parent, Normalizer: "ascii-alnum-lower-v1",
		QueueRule: "accepted-semantic-override-or-triggered-fallback-v1", ViewSeedDigest: adjudicationTextDigest(seed),
		EntailmentPromptDigest:    adjudicationAuditPromptDigest(adjudicationAuditViewEntailment),
		FalsificationPromptDigest: adjudicationAuditPromptDigest(adjudicationAuditViewFalsification),
		ResolverDigest:            adjudicationAuditResolverDigest(), QuestionCount: len(parentPackets),
	}
	auditPackets := make([]adjudicationAuditPacket, 0)
	resolver := make([]adjudicationAuditResolverMapRecord, 0, len(parentPackets))
	for index, packet := range parentPackets {
		decision := parentDecisions[index]
		if index > 0 && !adjudicationIdentityLess(parentPackets[index-1].Conv, parentPackets[index-1].Q, packet.Conv, packet.Q) {
			return manifest, nil, nil, fmt.Errorf("parent packets are not numeric identity sorted")
		}
		if decision.PacketID != packet.PacketID || decision.Conv != packet.Conv || decision.Q != packet.Q ||
			decision.QuestionID != packet.QuestionID {
			return manifest, nil, nil, fmt.Errorf("parent packet/decision alignment mismatch")
		}
		if err := validateAdjudicationDecision(decision, packet); err != nil {
			return manifest, nil, nil, fmt.Errorf("validate parent decision %d: %w", index, err)
		}
		groups, err := groupAdjudicationAuditAnswers(packet)
		if err != nil {
			return manifest, nil, nil, err
		}
		groupByMember := make(map[string]string, 3)
		for _, group := range groups {
			for _, digest := range group.MemberAnswerDigests {
				groupByMember[digest] = group.GroupDigest
			}
		}
		controlSlot := adjudicationTextControlSlot(packet.Candidates)
		controlDigest := ""
		for _, candidate := range packet.Candidates {
			if candidate.Slot == controlSlot {
				controlDigest = candidate.AnswerDigest
				break
			}
		}
		selectedGroup := groupByMember[decision.SelectedAnswerDigest]
		controlGroup := groupByMember[controlDigest]
		if selectedGroup == "" || controlGroup == "" {
			return manifest, nil, nil, fmt.Errorf("parent decision/control group missing")
		}
		overrideRisk := decision.State == adjudicationDecisionSelected && selectedGroup != controlGroup
		fallbackRisk := packet.Triggered && decision.State == adjudicationDecisionFallback &&
			decision.FallbackReason != adjudicationFallbackNotTriggered
		risk := overrideRisk || fallbackRisk
		record := adjudicationAuditResolverMapRecord{
			PacketID: packet.PacketID, ParentPacketDigest: packet.PacketDigest, ParentDecisionDigest: decision.DecisionDigest,
			Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID, ParentSelectedSlot: decision.SelectedSlot,
			ParentSelectedAnswerDigest: decision.SelectedAnswerDigest, ParentSelectedGroupDigest: selectedGroup,
			TextControlGroupDigest: controlGroup, Groups: append([]adjudicationAuditAnswerGroup(nil), groups...), Risk: risk,
		}
		if risk {
			views, viewMaps, err := buildAdjudicationAuditViews(seed, packet, groups)
			if err != nil {
				return manifest, nil, nil, err
			}
			record.RiskPacketID = packet.PacketID
			record.ViewMaps = viewMaps
			auditPackets = append(auditPackets, adjudicationAuditPacket{
				PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
				Category: packet.Category, Question: packet.Question, ContextParity: packet.ContextParity,
				Evidence: append([]adjudicationEvidenceItem(nil), packet.Evidence...), Views: views,
			})
			manifest.RiskCount++
			if overrideRisk {
				manifest.OverrideCount++
			} else {
				manifest.FallbackCount++
			}
		} else {
			manifest.RetainCount++
		}
		for groupIndex := range record.Groups {
			record.Groups[groupIndex].representativeAnswer = ""
		}
		resolver = append(resolver, record)
	}
	manifest.ViewCount = manifest.RiskCount * 2
	manifest.PlannedCalls = manifest.ViewCount
	manifest.ProtocolHash = adjudicationAuditManifestProtocolHash(manifest)
	for index := range auditPackets {
		auditPackets[index].Schema = adjudicationAuditPacketSchema
		auditPackets[index].ProtocolHash = manifest.ProtocolHash
		auditPackets[index].PacketDigest = adjudicationAuditPacketDigest(auditPackets[index])
	}
	for index := range resolver {
		resolver[index].Schema = adjudicationAuditResolverSchema
		resolver[index].ProtocolHash = manifest.ProtocolHash
	}
	var err error
	manifest.AuditPacketSetDigest, _, err = adjudicationJSONLDigest(auditPackets)
	if err != nil {
		return manifest, nil, nil, err
	}
	manifest.ResolverMapSetDigest, _, err = adjudicationJSONLDigest(resolver)
	if err != nil {
		return manifest, nil, nil, err
	}
	return manifest, auditPackets, resolver, nil
}
