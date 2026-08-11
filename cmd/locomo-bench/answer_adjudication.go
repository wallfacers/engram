package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const adjudicationVerifierSystemPrompt = `You are an evidence-grounded answer adjudicator. Choose exactly one supplied candidate answer. Use only the supplied evidence. Return strict JSON with exactly selected_slot, evidence_ids, and confidence. selected_slot must be C1, C2, or C3; evidence_ids must contain at least one supplied evidence ID; confidence must be "high" only when the cited evidence directly supports the selection. If evidence is insufficient, return confidence "low". Never write a new answer.`

// adjudicationTemporalSystemPrompt extends the frozen adjudicator contract
// with a temporal reasoning plan for category-2 (temporal) questions. It
// mirrors the answer-side tplan contract (032) that proved GO on temporal: the
// adjudicator must list [event:] markers, normalize the timeline, and reason
// the requested order/interval before selecting a candidate. Opt-in via
// --adjudication-temporal-prompt; default off keeps the run byte-identical.
const adjudicationTemporalSystemPrompt = adjudicationVerifierSystemPrompt + `

TEMPORAL REASONING PLAN (required for this temporal question):
- List every supplied evidence's [event: YYYY-MM-DD] marker before deciding.
- Normalize the candidate dates onto a common timeline, then compare dates and determine the requested order, month, or interval.
- When the question asks a duration or an inclusive date range, count the boundaries explicitly (include both endpoints).
- For sequence questions ("which city before X"), order the dated events and pick the city that immediately precedes X.
- Then choose the single candidate that matches the reasoned answer.`

// adjudicationTemporalPromptEnabled is set by --adjudication-temporal-prompt.
// When false (default) every system prompt and digest is byte-identical to the
// frozen 034 generic contract.
var adjudicationTemporalPromptEnabled bool

// adjudicationSystemPromptFor selects the system prompt for one packet. The
// temporal contract applies only when explicitly enabled AND the packet is a
// category-2 question; every other packet keeps the frozen generic prompt.
func adjudicationSystemPromptFor(packet adjudicationPacket) string {
	if adjudicationTemporalPromptEnabled && packet.Category == 2 {
		return adjudicationTemporalSystemPrompt
	}
	return adjudicationVerifierSystemPrompt
}

type adjudicationCandidate struct {
	Answer       string
	Normalized   string
	AnswerDigest string
	SourceDigest string
}

type adjudicationVerifierResponse struct {
	SelectedSlot string   `json:"selected_slot"`
	EvidenceIDs  []string `json:"evidence_ids"`
	Confidence   string   `json:"confidence"`
}

type adjudicationVerifierInput struct {
	Question   string                        `json:"question"`
	Evidence   []adjudicationEvidenceItem    `json:"evidence"`
	Candidates []adjudicationPacketCandidate `json:"candidates"`
}

type adjudicationVerifierError struct {
	Reason string
	Err    error
}

func (e *adjudicationVerifierError) Error() string {
	return e.Err.Error()
}

func (e *adjudicationVerifierError) Unwrap() error {
	return e.Err
}

func buildAdjudicationVerifierPrompt(packet adjudicationPacket) (string, error) {
	input := adjudicationVerifierInput{
		Question: packet.Question, Evidence: packet.Evidence, Candidates: packet.Candidates,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode verifier input: %w", err)
	}
	return string(raw), nil
}

func adjudicationRunIdentityDigest(providerName, baseURLDigest, model, modelRevision string, maxTokens int, binaryDigest, promptDigest string) string {
	return evalJSONDigest(struct {
		Provider      string `json:"provider"`
		BaseURLDigest string `json:"base_url_digest"`
		Model         string `json:"model"`
		ModelRevision string `json:"model_revision"`
		MaxTokens     int    `json:"max_tokens"`
		BinaryDigest  string `json:"binary_digest"`
		PromptDigest  string `json:"prompt_digest"`
	}{providerName, baseURLDigest, model, modelRevision, maxTokens, binaryDigest, promptDigest})
}

func adjudicationPacketInputDigest(packet adjudicationPacket, runIdentity string) (string, error) {
	userPrompt, err := buildAdjudicationVerifierPrompt(packet)
	if err != nil {
		return "", err
	}
	return adjudicationTextDigest(runIdentity + "\x00" + adjudicationSystemPromptFor(packet) + "\x00" + userPrompt), nil
}

func normalizeAdjudicationAnswer(answer string) string {
	var b strings.Builder
	b.Grow(len(answer))
	for i := 0; i < len(answer); i++ {
		c := answer[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func adjudicationTextControlSlot(candidates []adjudicationPacketCandidate) string {
	type group struct {
		normalized string
		members    []adjudicationPacketCandidate
	}
	byNorm := make(map[string][]adjudicationPacketCandidate)
	for _, candidate := range candidates {
		norm := normalizeAdjudicationAnswer(candidate.Answer)
		byNorm[norm] = append(byNorm[norm], candidate)
	}
	groups := make([]group, 0, len(byNorm))
	for normalized, members := range byNorm {
		sort.Slice(members, func(i, j int) bool {
			if members[i].Answer != members[j].Answer {
				return members[i].Answer < members[j].Answer
			}
			return members[i].Slot < members[j].Slot
		})
		groups = append(groups, group{normalized: normalized, members: members})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].members) != len(groups[j].members) {
			return len(groups[i].members) > len(groups[j].members)
		}
		if groups[i].members[0].Answer != groups[j].members[0].Answer {
			return groups[i].members[0].Answer < groups[j].members[0].Answer
		}
		return groups[i].normalized < groups[j].normalized
	})
	if len(groups) == 0 {
		return ""
	}
	return groups[0].members[0].Slot
}

func canonicalHistoricalSlotForSameAnswer(packet adjudicationPacket, slotMap adjudicationSlotMapRecord, selectedSlot string) string {
	selectedAnswer := ""
	for _, candidate := range packet.Candidates {
		if candidate.Slot == selectedSlot {
			selectedAnswer = candidate.Answer
			break
		}
	}
	bestSlot, bestSource := selectedSlot, ""
	for index, candidate := range packet.Candidates {
		if candidate.Answer != selectedAnswer || index >= len(slotMap.Slots) || slotMap.Slots[index].Slot != candidate.Slot {
			continue
		}
		source := slotMap.Slots[index].SourceDigest
		if bestSource == "" || source < bestSource || (source == bestSource && candidate.Slot < bestSlot) {
			bestSlot, bestSource = candidate.Slot, source
		}
	}
	return bestSlot
}

func blindAdjudicationCandidates(seed, questionID string, candidates []adjudicationCandidate) []adjudicationPacketCandidate {
	ordered := adjudicationBlindedSourceOrder(seed, questionID, candidates)
	out := make([]adjudicationPacketCandidate, len(ordered))
	for i, candidate := range ordered {
		out[i] = adjudicationPacketCandidate{
			Slot: fmt.Sprintf("C%d", i+1), Answer: candidate.Answer, AnswerDigest: candidate.AnswerDigest,
		}
	}
	return out
}

func adjudicationBlindedSourceOrder(seed, questionID string, candidates []adjudicationCandidate) []adjudicationCandidate {
	canonical := append([]adjudicationCandidate(nil), candidates...)
	for i := range canonical {
		if canonical[i].Normalized == "" {
			canonical[i].Normalized = normalizeAdjudicationAnswer(canonical[i].Answer)
		}
		if canonical[i].AnswerDigest == "" {
			canonical[i].AnswerDigest = adjudicationTextDigest(canonical[i].Answer)
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Normalized != canonical[j].Normalized {
			return canonical[i].Normalized < canonical[j].Normalized
		}
		if canonical[i].Answer != canonical[j].Answer {
			return canonical[i].Answer < canonical[j].Answer
		}
		return canonical[i].SourceDigest < canonical[j].SourceDigest
	})
	type keyed struct {
		candidate adjudicationCandidate
		key       string
	}
	keyedCandidates := make([]keyed, len(canonical))
	for i, candidate := range canonical {
		material := strings.Join([]string{seed, questionID, candidate.Normalized, candidate.Answer, candidate.SourceDigest}, "\x00")
		sum := sha256.Sum256([]byte(material))
		keyedCandidates[i] = keyed{candidate: candidate, key: hex.EncodeToString(sum[:])}
	}
	sort.Slice(keyedCandidates, func(i, j int) bool {
		if keyedCandidates[i].key != keyedCandidates[j].key {
			return keyedCandidates[i].key < keyedCandidates[j].key
		}
		return keyedCandidates[i].candidate.SourceDigest < keyedCandidates[j].candidate.SourceDigest
	})
	out := make([]adjudicationCandidate, len(keyedCandidates))
	for i, item := range keyedCandidates {
		out[i] = item.candidate
	}
	return out
}

func parseAdjudicationVerifierResponse(raw string, packet adjudicationPacket) (adjudicationVerifierResponse, error) {
	var response adjudicationVerifierResponse
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, &adjudicationVerifierError{Reason: adjudicationFallbackInvalidResponse, Err: fmt.Errorf("decode verifier response: %w", err)}
	}
	if err := requireJSONEOF(decoder); err != nil {
		return response, &adjudicationVerifierError{Reason: adjudicationFallbackInvalidResponse, Err: err}
	}
	validSlots := make(map[string]bool, len(packet.Candidates))
	for _, candidate := range packet.Candidates {
		validSlots[candidate.Slot] = true
	}
	if !validSlots[response.SelectedSlot] {
		return response, &adjudicationVerifierError{Reason: adjudicationFallbackInvalidResponse, Err: fmt.Errorf("invalid selected slot")}
	}
	if response.Confidence != "high" {
		return response, &adjudicationVerifierError{Reason: adjudicationFallbackLowConfidence, Err: fmt.Errorf("confidence is not high")}
	}
	if len(response.EvidenceIDs) == 0 {
		return response, &adjudicationVerifierError{Reason: adjudicationFallbackInvalidResponse, Err: fmt.Errorf("at least one evidence citation is required")}
	}
	validEvidence := make(map[string]bool, len(packet.Evidence))
	for _, evidence := range packet.Evidence {
		validEvidence[evidence.EvidenceID] = true
	}
	seen := make(map[string]bool, len(response.EvidenceIDs))
	for _, evidenceID := range response.EvidenceIDs {
		if !validEvidence[evidenceID] || seen[evidenceID] {
			return response, &adjudicationVerifierError{Reason: adjudicationFallbackInvalidResponse, Err: fmt.Errorf("invalid or duplicate evidence citation")}
		}
		seen[evidenceID] = true
	}
	return response, nil
}

func adjudicationVerifierFallbackReason(err error) string {
	var verifierErr *adjudicationVerifierError
	if errors.As(err, &verifierErr) {
		return verifierErr.Reason
	}
	return adjudicationFallbackInvalidResponse
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return fmt.Errorf("invalid trailing response: %w", err)
}

func adjudicationTextDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// adjudicationPromptDigest returns the digest of the frozen generic prompt.
// The 034 manifest freezes this digest at build time; the temporal contract is
// a run-time prompt variant that does NOT rewrite the manifest, so this stays
// the generic digest in every mode (byte-identical default and opt-in alike).
// The temporal arm is distinguished by adjudicationPacketInputDigest, which
// folds the actually-used system prompt into each decision's input identity.
func adjudicationPromptDigest() string {
	return adjudicationTextDigest(adjudicationVerifierSystemPrompt + "\x00packet-json-v1")
}

// adjudicationTemporalPromptDigest is the digest the temporal contract would
// produce; used only to document the arm in the run summary, never to rewrite
// the frozen manifest.
func adjudicationTemporalPromptDigest() string {
	return adjudicationTextDigest(adjudicationTemporalSystemPrompt + "\x00packet-json-v1")
}

type adjudicationHiddenCandidate struct {
	Answer     string
	Normalized string
	Correct    bool
}

type adjudicationHiddenInputs struct {
	Sources        map[string]map[string]adjudicationHiddenCandidate
	SlotMaps       map[string]adjudicationSlotMapRecord
	IntegrityValid bool
}

type adjudicationScoreCount struct {
	Correct int `json:"correct"`
	Total   int `json:"total"`
}

type adjudicationInstabilityScore struct {
	Total          int `json:"total"`
	Triggered      int `json:"triggered"`
	SelectedLower  int `json:"selected_lower"`
	SelectedUpper  int `json:"selected_upper"`
	TriggeredLower int `json:"triggered_lower"`
	TriggeredUpper int `json:"triggered_upper"`
}

type adjudicationPairedScore struct {
	ControlOnly  int     `json:"control_only"`
	SelectedOnly int     `json:"selected_only"`
	McNemarP     float64 `json:"mcnemar_p"`
}

type adjudicationStage0Score struct {
	Schema                      string                       `json:"schema"`
	ResultKind                  string                       `json:"result_kind"`
	ProtocolHash                string                       `json:"protocol_hash"`
	DecisionSetDigest           string                       `json:"decision_set_digest"`
	QuestionCount               int                          `json:"question_count"`
	TriggeredCount              int                          `json:"triggered_count"`
	ContextParityCount          int                          `json:"context_parity_count"`
	TriggeredContextParityCount int                          `json:"triggered_context_parity_count"`
	VerdictMajority             adjudicationScoreCount       `json:"historical_verdict_majority"`
	TextControl                 adjudicationScoreCount       `json:"deterministic_text_control"`
	CandidateOracle             adjudicationScoreCount       `json:"candidate_oracle"`
	Selected                    adjudicationScoreCount       `json:"selected_historical_mapping"`
	Mixed                       adjudicationScoreCount       `json:"mixed_verdict"`
	TriggeredMixed              adjudicationScoreCount       `json:"triggered_mixed_verdict"`
	JudgeInstability            adjudicationInstabilityScore `json:"judge_instability"`
	Paired                      adjudicationPairedScore      `json:"selected_vs_text_control"`
	Categories                  []evalCategoryGateResult     `json:"categories"`
	IntegrityValid              bool                         `json:"integrity_valid"`
	FrozenDiagnosticsValid      bool                         `json:"frozen_diagnostics_valid"`
	GateReasons                 []string                     `json:"gate_reasons"`
	Verdict                     string                       `json:"verdict"`
	FallbackCounts              map[string]int               `json:"fallback_counts,omitempty"`
	ProviderAttempts            int                          `json:"provider_attempts,omitempty"`
	InputTokens                 int                          `json:"input_tokens,omitempty"`
	OutputTokens                int                          `json:"output_tokens,omitempty"`
	PricingStatus               string                       `json:"pricing_status,omitempty"`
	EstimatedCNY                *float64                     `json:"estimated_cny,omitempty"`
}

type adjudicationStage0GateInput struct {
	SelectedCorrect        int
	QuestionCount          int
	MixedCorrect           int
	MixedTotal             int
	IntegrityValid         bool
	FrozenDiagnosticsValid bool
	Categories             map[string]evalCategoryGateResult
}

func adjudicationStage0Verdict(input adjudicationStage0GateInput) string {
	if !input.IntegrityValid || !input.FrozenDiagnosticsValid || input.QuestionCount != adjudicationFrozenQuestionCount ||
		input.MixedTotal != 88 || input.SelectedCorrect < 1387 || input.MixedCorrect < 69 ||
		hasHolmNegativeRegression(input.Categories) {
		return "NO_GO"
	}
	return "GO"
}

func scoreAdjudicationDecisions(manifest adjudicationManifest, packets []adjudicationPacket, decisions []adjudicationDecision, hidden adjudicationHiddenInputs, requireFrozen bool) (adjudicationStage0Score, error) {
	report := adjudicationStage0Score{
		Schema: "034.adjudication.stage0-score.v1", ResultKind: "historical_verdict_mapping",
		ProtocolHash: manifest.ProtocolHash, QuestionCount: len(packets), TriggeredCount: manifest.TriggeredCount,
		ContextParityCount: manifest.ContextParityCount, TriggeredContextParityCount: manifest.TriggeredContextParityCount,
		VerdictMajority: adjudicationScoreCount{Total: len(packets)}, TextControl: adjudicationScoreCount{Total: len(packets)},
		CandidateOracle: adjudicationScoreCount{Total: len(packets)}, Selected: adjudicationScoreCount{Total: len(packets)},
		IntegrityValid: hidden.IntegrityValid,
	}
	if len(packets) == 0 || len(decisions) != len(packets) {
		return report, fmt.Errorf("score requires one decision per packet")
	}
	decisionByPacket := make(map[string]adjudicationDecision, len(decisions))
	for _, decision := range decisions {
		if decisionByPacket[decision.PacketID].PacketID != "" {
			return report, fmt.Errorf("duplicate score decision %q", decision.PacketID)
		}
		decisionByPacket[decision.PacketID] = decision
	}
	categoryControl := make(map[int][]bool)
	categorySelected := make(map[int][]bool)
	controlOutcomes := make([]bool, 0, len(packets))
	selectedOutcomes := make([]bool, 0, len(packets))
	for _, packet := range packets {
		decision, ok := decisionByPacket[packet.PacketID]
		if !ok {
			return report, fmt.Errorf("missing score decision for %q", packet.PacketID)
		}
		if err := validateAdjudicationDecision(decision, packet); err != nil {
			return report, err
		}
		slotMap, ok := hidden.SlotMaps[packet.PacketID]
		if !ok || slotMap.Conv != packet.Conv || slotMap.Q != packet.Q || slotMap.QuestionID != packet.QuestionID || len(slotMap.Slots) != 3 {
			return report, fmt.Errorf("missing or invalid slot map for %q", packet.PacketID)
		}
		correctBySlot := make(map[string]bool, 3)
		normalizedBySlot := make(map[string]string, 3)
		correctByNormalized := make(map[string][]bool)
		for index, slot := range slotMap.Slots {
			if slot.Slot != fmt.Sprintf("C%d", index+1) || slot.AnswerDigest != packet.Candidates[index].AnswerDigest ||
				slot.NormalizedAnswerDigest != adjudicationTextDigest(normalizeAdjudicationAnswer(packet.Candidates[index].Answer)) {
				return report, fmt.Errorf("slot map drift for %q", packet.PacketID)
			}
			source := hidden.Sources[slot.SourceDigest]
			candidate, ok := source[packet.QuestionID]
			if !ok || candidate.Answer != packet.Candidates[index].Answer ||
				candidate.Normalized != normalizeAdjudicationAnswer(candidate.Answer) {
				return report, fmt.Errorf("hidden candidate drift for %q/%s", packet.PacketID, slot.Slot)
			}
			correctBySlot[slot.Slot] = candidate.Correct
			normalizedBySlot[slot.Slot] = candidate.Normalized
			correctByNormalized[candidate.Normalized] = append(correctByNormalized[candidate.Normalized], candidate.Correct)
		}
		correctValues := []bool{correctBySlot["C1"], correctBySlot["C2"], correctBySlot["C3"]}
		majority, err := majorityCorrectness(correctValues)
		if err != nil {
			return report, err
		}
		if majority {
			report.VerdictMajority.Correct++
		}
		oracle := correctValues[0] || correctValues[1] || correctValues[2]
		if oracle {
			report.CandidateOracle.Correct++
		}
		controlSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, adjudicationTextControlSlot(packet.Candidates))
		selectedSlot := canonicalHistoricalSlotForSameAnswer(packet, slotMap, decision.SelectedSlot)
		controlCorrect := correctBySlot[controlSlot]
		selectedCorrect := correctBySlot[selectedSlot]
		if controlCorrect {
			report.TextControl.Correct++
		}
		if selectedCorrect {
			report.Selected.Correct++
		}
		mixed := correctValues[0] != correctValues[1] || correctValues[0] != correctValues[2]
		if mixed {
			report.Mixed.Total++
			if selectedCorrect {
				report.Mixed.Correct++
			}
			if packet.Triggered {
				report.TriggeredMixed.Total++
				if selectedCorrect {
					report.TriggeredMixed.Correct++
				}
			}
		}
		unstable := false
		selectedGroup := correctByNormalized[normalizedBySlot[selectedSlot]]
		selectedLower, selectedUpper := selectedCorrect, selectedCorrect
		for _, group := range correctByNormalized {
			hasTrue, hasFalse := false, false
			for _, outcome := range group {
				hasTrue = hasTrue || outcome
				hasFalse = hasFalse || !outcome
			}
			unstable = unstable || (hasTrue && hasFalse)
		}
		if len(selectedGroup) > 1 {
			selectedLower, selectedUpper = true, false
			for _, outcome := range selectedGroup {
				selectedLower = selectedLower && outcome
				selectedUpper = selectedUpper || outcome
			}
		}
		if unstable {
			report.JudgeInstability.Total++
			if packet.Triggered {
				report.JudgeInstability.Triggered++
			}
		}
		if selectedLower {
			report.JudgeInstability.SelectedLower++
			if packet.Triggered {
				report.JudgeInstability.TriggeredLower++
			}
		}
		if selectedUpper {
			report.JudgeInstability.SelectedUpper++
			if packet.Triggered {
				report.JudgeInstability.TriggeredUpper++
			}
		}
		controlOutcomes = append(controlOutcomes, controlCorrect)
		selectedOutcomes = append(selectedOutcomes, selectedCorrect)
		categoryControl[packet.Category] = append(categoryControl[packet.Category], controlCorrect)
		categorySelected[packet.Category] = append(categorySelected[packet.Category], selectedCorrect)
	}
	for index := range controlOutcomes {
		switch {
		case controlOutcomes[index] && !selectedOutcomes[index]:
			report.Paired.ControlOnly++
		case !controlOutcomes[index] && selectedOutcomes[index]:
			report.Paired.SelectedOnly++
		}
	}
	pValue, err := exactMcNemarTwoSided(report.Paired.ControlOnly, report.Paired.SelectedOnly)
	if err != nil {
		return report, err
	}
	report.Paired.McNemarP = pValue
	categoryIDs := make([]int, 0, len(categoryControl))
	for category := range categoryControl {
		categoryIDs = append(categoryIDs, category)
	}
	sort.Ints(categoryIDs)
	comparisons := make([]evalCategoryComparison, 0, len(categoryIDs))
	for _, category := range categoryIDs {
		comparison, err := pairedCategoryComparison(categoryLabel(category), categoryControl[category], categorySelected[category])
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
		(manifest.QuestionCount == adjudicationFrozenQuestionCount && manifest.TriggeredCount == adjudicationFrozenTriggerCount &&
			manifest.ContextParityCount == adjudicationFrozenContextParityCount &&
			manifest.TriggeredContextParityCount == adjudicationFrozenTriggeredContextParityCount &&
			report.VerdictMajority.Correct == 1371 && report.TextControl.Correct == 1368 &&
			report.CandidateOracle.Correct == 1411 && report.Mixed.Total == 96 && report.TriggeredMixed.Total == 88 &&
			report.JudgeInstability.Total == 13 && report.JudgeInstability.Triggered == 5)
	gateInput := adjudicationStage0GateInput{
		SelectedCorrect: report.Selected.Correct, QuestionCount: report.QuestionCount,
		MixedCorrect: report.TriggeredMixed.Correct, MixedTotal: report.TriggeredMixed.Total,
		IntegrityValid: report.IntegrityValid, FrozenDiagnosticsValid: report.FrozenDiagnosticsValid,
		Categories: categoryGates,
	}
	report.Verdict = adjudicationStage0Verdict(gateInput)
	if report.Selected.Correct < 1387 {
		report.GateReasons = append(report.GateReasons, "selected_below_1387")
	}
	if report.TriggeredMixed.Total != 88 || report.TriggeredMixed.Correct < 69 {
		report.GateReasons = append(report.GateReasons, "triggered_mixed_below_69_of_88")
	}
	if hasHolmNegativeRegression(categoryGates) {
		report.GateReasons = append(report.GateReasons, "category_regression")
	}
	if !report.IntegrityValid {
		report.GateReasons = append(report.GateReasons, "integrity_invalid")
	}
	if !report.FrozenDiagnosticsValid {
		report.GateReasons = append(report.GateReasons, "frozen_diagnostics_mismatch")
	}
	return report, nil
}

func scoreSealedAdjudication(dir string, requireFrozen bool, hiddenLoader func() (adjudicationHiddenInputs, error)) (adjudicationStage0Score, error) {
	manifest, packets, err := loadAndValidateAdjudicationPublic(dir, requireFrozen)
	if err != nil {
		return adjudicationStage0Score{}, err
	}
	seal, decisions, err := validateAdjudicationSeal(dir, manifest, packets)
	if err != nil {
		return adjudicationStage0Score{}, err
	}
	if hiddenLoader == nil {
		return adjudicationStage0Score{}, fmt.Errorf("hidden score loader is required")
	}
	hidden, err := hiddenLoader()
	if err != nil {
		return adjudicationStage0Score{}, err
	}
	report, err := scoreAdjudicationDecisions(manifest, packets, decisions, hidden, requireFrozen)
	if err != nil {
		return report, err
	}
	report.DecisionSetDigest = seal.DecisionSetDigest
	report.FallbackCounts = seal.FallbackCounts
	report.ProviderAttempts = seal.ProviderAttempts
	report.InputTokens, report.OutputTokens = seal.InputTokens, seal.OutputTokens
	report.PricingStatus, report.EstimatedCNY = seal.PricingStatus, seal.EstimatedCNY
	return report, nil
}
