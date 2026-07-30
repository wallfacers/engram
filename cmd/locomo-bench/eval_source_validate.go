package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// formalActiveBundleValidationReceipt is the independently derived result of
// rereading the Ledger. It is intentionally separate from Bundle.SourceValid:
// callers may persist these three dimensions only after this validator
// succeeds, instead of treating a producer-owned boolean as evidence.
type formalActiveBundleValidationReceipt struct {
	AllowedIDsDigest    string
	EvidenceStateDigest string
	ResolvedCount       int
	InvalidCount        int
	SourceValid         bool
	SpanValid           bool
	CitationValid       bool
}

func (receipt formalActiveBundleValidationReceipt) Valid() bool {
	return receipt.SourceValid && receipt.SpanValid && receipt.CitationValid &&
		receipt.InvalidCount == 0
}

func persistFormalActiveValidation(receipt formalActiveBundleValidationReceipt) evalFormalActiveValidation {
	validation := evalFormalActiveValidation{
		Checked:             true,
		AllowedIDsDigest:    receipt.AllowedIDsDigest,
		EvidenceStateDigest: receipt.EvidenceStateDigest,
		ResolvedCount:       receipt.ResolvedCount,
		InvalidCount:        receipt.InvalidCount,
		SourceValid:         receipt.SourceValid,
		SpanValid:           receipt.SpanValid,
		CitationValid:       receipt.CitationValid,
	}
	validation.ReceiptDigest = formalActiveValidationDigest(validation)
	return validation
}

func formalActiveValidationDigest(validation evalFormalActiveValidation) string {
	validation.ReceiptDigest = ""
	return evalJSONDigest(validation)
}

func inspectFormalActiveValidation(validation evalFormalActiveValidation) (sourceOK, spanOK, citationOK bool) {
	if !validation.Checked ||
		!isDigest(validation.AllowedIDsDigest) ||
		!isDigest(validation.EvidenceStateDigest) ||
		validation.ResolvedCount < 0 ||
		validation.InvalidCount < 0 ||
		validation.ReceiptDigest != formalActiveValidationDigest(validation) {
		return false, false, false
	}
	return validation.SourceValid, validation.SpanValid, validation.CitationValid
}

// validateActiveFormalBundle is deliberately independent from the producer's
// SourceValid flag. It rereads the current Ledger state, recovers every
// Unicode code-point span, checks candidate/citation authority, and rebuilds
// the exact answer-facing prompt.
func validateActiveFormalBundle(
	ctx context.Context,
	reader formalEvidenceReader,
	protocol evalProtocol,
	opt options,
	qa locomoQA,
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	bundle evalFormalBundleRecord,
) error {
	receipt, err := validateActiveFormalBundleReceipt(ctx, reader, protocol, opt, qa, candidate, trace, bundle)
	if err != nil {
		return err
	}
	if !receipt.Valid() {
		return fmt.Errorf("formal Bundle failed active source/span/citation validation")
	}
	return nil
}

func validateActiveFormalBundleReceipt(
	ctx context.Context,
	reader formalEvidenceReader,
	protocol evalProtocol,
	opt options,
	qa locomoQA,
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	bundle evalFormalBundleRecord,
) (formalActiveBundleValidationReceipt, error) {
	allowedSourceIDs := stableStrings(collectRenderedSources(candidate.RenderedCandidates))
	receipt := formalActiveBundleValidationReceipt{
		AllowedIDsDigest:    evalJSONDigest(allowedSourceIDs),
		EvidenceStateDigest: evalJSONDigest([]string{}),
	}
	sourceOK, spanOK, citationOK := inspectFormalBundleStructure(candidate, trace, bundle)
	var problems []string
	invalidateAll := func(problem string) {
		sourceOK, spanOK, citationOK = false, false, false
		problems = append(problems, problem)
	}
	if err := validateEvalCandidateArtifact(protocol, candidate); err != nil {
		invalidateAll("candidate artifact: " + err.Error())
	}
	if candidate.QuestionID != qa.QuestionID || candidate.QueryDigest != evalTextDigest(qa.Question) {
		invalidateAll("candidate question identity differs from the active question")
	}
	if err := validateFormalActiveBundleEnvelope(protocol, candidate, trace, bundle); err != nil {
		invalidateAll("formal Bundle envelope: " + err.Error())
	}
	if err := validateFormalB1AnchorPrefix(candidate, bundle); err != nil {
		citationOK = false
		problems = append(problems, "ranked anchor prefix: "+err.Error())
	}

	var evidenceByID map[string]memory.Evidence
	if reader == nil {
		sourceOK, spanOK = false, false
		problems = append(problems, "active Evidence reader is unavailable")
	} else {
		resolved, err := reader.GetMany(ctx, bundle.SourceIDs)
		if err != nil {
			sourceOK, spanOK = false, false
			problems = append(problems, "reread active formal Evidence: "+err.Error())
		} else {
			evidenceByID = resolved
			if len(evidenceByID) != len(stringSet(bundle.SourceIDs)) {
				sourceOK, spanOK = false, false
				problems = append(problems, fmt.Sprintf(
					"active Evidence resolution returned %d of %d unique sources",
					len(evidenceByID), len(stringSet(bundle.SourceIDs)),
				))
			}
		}
	}

	type evidenceStateReceipt struct {
		EvidenceID       string                    `json:"evidence_id"`
		ExternalSourceID string                    `json:"external_source_id,omitempty"`
		SourceType       memory.EvidenceSourceType `json:"source_type,omitempty"`
		State            memory.EvidenceState      `json:"state,omitempty"`
		Revision         int64                     `json:"revision,omitempty"`
		ContentDigest    string                    `json:"content_digest,omitempty"`
		Missing          bool                      `json:"missing,omitempty"`
	}
	invalidSources := make(map[string]bool)
	evidenceStates := make([]evidenceStateReceipt, 0, len(stableStrings(bundle.SourceIDs)))
	for _, sourceID := range stableStrings(bundle.SourceIDs) {
		evidence, ok := evidenceByID[sourceID]
		if !ok {
			evidenceStates = append(evidenceStates, evidenceStateReceipt{EvidenceID: sourceID, Missing: true})
			invalidSources[sourceID] = true
			sourceOK, spanOK = false, false
			continue
		}
		evidenceStates = append(evidenceStates, evidenceStateReceipt{
			EvidenceID:       evidence.ID,
			ExternalSourceID: evidence.ExternalSourceID,
			SourceType:       evidence.SourceType,
			State:            evidence.State,
			Revision:         evidence.Revision,
			ContentDigest:    formalArtifactDigest(evidence.ContentDigest),
		})
		if err := validateFormalRawMessageEvidence(evidence, sourceID); err != nil {
			invalidSources[sourceID] = true
			sourceOK, spanOK = false, false
			problems = append(problems, err.Error())
			continue
		}
		receipt.ResolvedCount++
	}
	receipt.EvidenceStateDigest = evalJSONDigest(evidenceStates)

	renderedByID := make(map[string]evalRenderedCandidate, len(candidate.RenderedCandidates))
	for _, rendered := range candidate.RenderedCandidates {
		renderedByID[rendered.CandidateID] = rendered
	}
	results := make([]memory.Result, 0, len(bundle.Items))
	contextRecoverable := true
	for _, item := range bundle.Items {
		if len(item.Sources) != 1 || len(item.CandidateIDs) != 1 {
			spanOK, citationOK = false, false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("B1 item %q must have exactly one candidate and one source span", item.ItemID))
			continue
		}
		source := item.Sources[0]
		evidence, ok := evidenceByID[source.EvidenceID]
		if !ok {
			spanOK = false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("item %q cites unavailable Evidence %q", item.ItemID, source.EvidenceID))
			continue
		}
		runes := []rune(evidence.Content)
		if source.StartChar < 0 || source.StartChar >= source.EndChar || source.EndChar > len(runes) {
			spanOK = false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("item %q has invalid source offsets", item.ItemID))
			continue
		}
		recovered := string(runes[source.StartChar:source.EndChar])
		if source.SpanDigest != evalTextDigest(recovered) || item.Text != recovered {
			spanOK = false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("item %q text/span digest cannot be recovered", item.ItemID))
			continue
		}
		switch item.Kind {
		case "KEEP":
			if source.StartChar != 0 || source.EndChar != len(runes) {
				spanOK = false
				contextRecoverable = false
				problems = append(problems, fmt.Sprintf("KEEP item %q is not the complete source", item.ItemID))
				continue
			}
		case "EXTRACT":
			if source.StartChar == 0 && source.EndChar == len(runes) {
				spanOK = false
				contextRecoverable = false
				problems = append(problems, fmt.Sprintf("EXTRACT item %q unexpectedly cites the complete source", item.ItemID))
				continue
			}
		default:
			spanOK = false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("B1 item %q uses unsupported action %q", item.ItemID, item.Kind))
			continue
		}
		rendered, exists := renderedByID[item.CandidateIDs[0]]
		if !exists || rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) ||
			len(rendered.SourceIDs) != 1 || rendered.SourceIDs[0] != source.EvidenceID {
			citationOK = false
			problems = append(problems, fmt.Sprintf("item %q differs from its frozen rendered candidate", item.ItemID))
		}
		results = append(results, formalEvidenceResult(item.ItemID, item.Text, evidence))
	}

	system := withCurrentDateRule(
		answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		qa.QuestionDate,
	)
	if contextRecoverable && len(results) == len(bundle.Items) {
		renderedContext := buildAnswerContextPrompt(qa.Question, results, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
		if bundle.RenderedContext != renderedContext ||
			bundle.RenderedDigest != evalTextDigest(renderedContext) ||
			bundle.AnswerPromptDigest != evalTextDigest(system) {
			citationOK = false
			problems = append(problems, "formal Bundle answer input cannot be reconstructed")
		}
	} else {
		citationOK = false
		problems = append(problems, "formal Bundle answer input could not be reconstructed")
	}
	if bundle.TokenCap != protocol.Budget.AnswerInputTokenCap ||
		bundle.CounterFingerprint != protocol.Budget.CounterFingerprint ||
		bundle.AnswerInputTokens < 1 || bundle.AnswerInputTokens > bundle.TokenCap ||
		bundle.EvidenceTokens < 0 || bundle.EvidenceTokens > bundle.AnswerInputTokens ||
		!bundle.WithinCap {
		citationOK = false
		problems = append(problems, "formal Bundle token admission is invalid")
	}

	receipt.SourceValid = sourceOK
	receipt.SpanValid = spanOK
	receipt.CitationValid = citationOK
	receipt.InvalidCount = len(invalidSources)
	if receipt.Valid() {
		return receipt, nil
	}
	problems = stableStrings(problems)
	if len(problems) == 0 {
		problems = []string{"active source/span/citation contract failed"}
	}
	return receipt, fmt.Errorf("formal Bundle active validation failed: %s", strings.Join(problems, "; "))
}

func validateFormalRawMessageEvidence(evidence memory.Evidence, requestedID string) error {
	if evidence.ID == "" || evidence.ID != requestedID || evidence.State != memory.EvidenceActive {
		return fmt.Errorf("Evidence %q identity or active state mismatch", requestedID)
	}
	if evidence.SourceType != memory.EvidenceMessage {
		return fmt.Errorf(
			"answer-facing B1 source %q has type %q, want raw message Evidence",
			requestedID, evidence.SourceType,
		)
	}
	if strings.TrimSpace(evidence.SourceSessionID) == "" || strings.TrimSpace(evidence.Speaker) == "" {
		return fmt.Errorf("raw message Evidence %q lacks session or speaker identity", requestedID)
	}
	if formalArtifactDigest(evidence.ContentDigest) != evalTextDigest(evidence.Content) {
		return fmt.Errorf("Evidence %q content digest mismatch", requestedID)
	}
	return nil
}

func validateFormalActiveBundleEnvelope(
	protocol evalProtocol,
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	bundle evalFormalBundleRecord,
) error {
	if candidate.RetrievalCalls != protocol.Budget.RetrievalCallLimit {
		return fmt.Errorf("retrieval calls %d differ from protocol %d", candidate.RetrievalCalls, protocol.Budget.RetrievalCallLimit)
	}
	if trace.Schema != evalProtocolSchema || trace.ProtocolHash != protocol.ProtocolHash ||
		trace.QuestionID != candidate.QuestionID || trace.Kind != evalTraceArtifactKind ||
		trace.Attempt != 1 || trace.CandidateSetDigest != candidate.CandidateSetDigest ||
		trace.TraceDigest != formalTraceDigest(trace) {
		return fmt.Errorf("trace identity or digest mismatch")
	}
	if bundle.Schema != evalProtocolSchema || bundle.ProtocolHash != protocol.ProtocolHash ||
		bundle.QuestionID != candidate.QuestionID || bundle.Kind != evalBundleArtifactKind ||
		bundle.CandidateSetDigest != candidate.CandidateSetDigest ||
		bundle.TraceDigest != trace.TraceDigest ||
		bundle.RenderedDigest != evalTextDigest(bundle.RenderedContext) ||
		bundle.AnswerInputTokens < 1 ||
		bundle.AnswerInputTokens > protocol.Budget.AnswerInputTokenCap ||
		bundle.TokenCap != protocol.Budget.AnswerInputTokenCap ||
		bundle.CounterFingerprint != protocol.Budget.CounterFingerprint ||
		!bundle.WithinCap || len(bundle.Items) == 0 || len(bundle.SourceIDs) == 0 ||
		!isDigest(bundle.AnswerPromptDigest) {
		return fmt.Errorf("Bundle identity, digest, or budget envelope mismatch")
	}
	return nil
}

// validateFormalB1AnchorPrefix proves that B1 packed exactly a ranked prefix
// of complete navigation anchors. Candidate membership alone is insufficient:
// it would permit skipping a large first anchor, reordering sources, or
// admitting only part of a multi-source anchor while still citing valid IDs.
func validateFormalB1AnchorPrefix(candidate evalCandidateArtifact, bundle evalFormalBundleRecord) error {
	if len(candidate.Anchors) == 0 || len(candidate.RenderedCandidates) == 0 {
		return fmt.Errorf("candidate has no ranked anchors or rendered sources")
	}
	renderedIndex := 0
	validBoundaries := map[int]bool{0: true}
	for anchorIndex, anchor := range candidate.Anchors {
		if anchor.Rank != anchorIndex+1 || strings.TrimSpace(anchor.CandidateID) == "" {
			return fmt.Errorf("anchor %d has invalid identity or rank", anchorIndex)
		}
		start := renderedIndex
		var renderedSourceIDs []string
		for renderedIndex < len(candidate.RenderedCandidates) {
			rendered := candidate.RenderedCandidates[renderedIndex]
			if len(rendered.ExpandedFrom) != 1 || rendered.ExpandedFrom[0] != anchor.CandidateID {
				break
			}
			localOrder := renderedIndex - start
			if rendered.Rank != renderedIndex+1 || len(rendered.SourceIDs) != 1 {
				return fmt.Errorf("rendered source %d has invalid rank or source cardinality", renderedIndex)
			}
			sourceID := rendered.SourceIDs[0]
			if rendered.CandidateID != formalRenderedSourceID(anchor.CandidateID, localOrder, sourceID) {
				return fmt.Errorf("rendered source %d does not encode source_order %d", renderedIndex, localOrder)
			}
			renderedSourceIDs = append(renderedSourceIDs, sourceID)
			renderedIndex++
		}
		if renderedIndex == start {
			return fmt.Errorf("anchor %q has no rendered source group", anchor.CandidateID)
		}
		if !sameOrderedStrings(anchor.SourceIDs, orderedFormalSourceValues(renderedSourceIDs)) {
			return fmt.Errorf("anchor %q source order/union differs from rendered sources", anchor.CandidateID)
		}
		validBoundaries[renderedIndex] = true
	}
	if renderedIndex != len(candidate.RenderedCandidates) {
		return fmt.Errorf("rendered candidates are not grouped in ranked anchor order")
	}
	if len(bundle.Items) == 0 || len(bundle.Items) > len(candidate.RenderedCandidates) ||
		!validBoundaries[len(bundle.Items)] {
		return fmt.Errorf("Bundle item count %d is not a complete-anchor prefix boundary", len(bundle.Items))
	}
	for index, item := range bundle.Items {
		rendered := candidate.RenderedCandidates[index]
		if item.ItemID != formalBundleItemID(rendered.CandidateID) ||
			len(item.CandidateIDs) != 1 || item.CandidateIDs[0] != rendered.CandidateID ||
			len(item.Sources) != 1 || len(rendered.SourceIDs) != 1 ||
			item.Sources[0].EvidenceID != rendered.SourceIDs[0] ||
			item.Text != rendered.Text {
			return fmt.Errorf("Bundle item %d is not rendered candidate %q in source order", index, rendered.CandidateID)
		}
	}
	return nil
}

func orderedFormalSourceValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	ordered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" && !seen[value] {
			seen[value] = true
			ordered = append(ordered, value)
		}
	}
	return ordered
}

func sameOrderedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// inspectFormalBundle performs the source/span/citation checks available from
// immutable artifacts alone. The active validator above adds the independent
// Ledger reread; keeping the three booleans separate prevents SourceValid from
// being reported as three different measurements.
func inspectFormalBundle(
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	bundle evalFormalBundleRecord,
) (sourceOK, spanOK, citationOK bool) {
	sourceOK, spanOK, citationOK = inspectFormalBundleStructure(candidate, trace, bundle)
	if !bundle.SourceValid {
		sourceOK = false
	}
	return sourceOK, spanOK, citationOK
}

func inspectFormalBundleStructure(
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	bundle evalFormalBundleRecord,
) (sourceOK, spanOK, citationOK bool) {
	if len(bundle.Items) == 0 || len(bundle.SourceIDs) == 0 {
		return false, false, false
	}
	renderedByID := make(map[string]evalRenderedCandidate, len(candidate.RenderedCandidates))
	var allowedSourceIDs []string
	for _, rendered := range candidate.RenderedCandidates {
		if rendered.CandidateID == "" {
			return false, false, false
		}
		renderedByID[rendered.CandidateID] = rendered
		allowedSourceIDs = append(allowedSourceIDs, rendered.SourceIDs...)
	}
	allowedSourceIDs = stableStrings(allowedSourceIDs)
	allowed := stringSet(allowedSourceIDs)

	sourceOK, spanOK, citationOK = true, true, true
	seenItems := make(map[string]bool, len(bundle.Items))
	var citedSourceIDs []string
	actions := make([]string, 0, len(bundle.Items))
	for _, item := range bundle.Items {
		if strings.TrimSpace(item.ItemID) == "" || seenItems[item.ItemID] || strings.TrimSpace(item.Text) == "" {
			sourceOK, spanOK, citationOK = false, false, false
			continue
		}
		seenItems[item.ItemID] = true
		if item.Kind != "KEEP" && item.Kind != "EXTRACT" {
			spanOK = false
		}
		actions = append(actions, item.Kind)
		if len(item.CandidateIDs) != 1 || len(item.Sources) != 1 {
			spanOK, citationOK = false, false
			continue
		}
		rendered, exists := renderedByID[item.CandidateIDs[0]]
		if !exists {
			citationOK = false
		}
		source := item.Sources[0]
		citedSourceIDs = append(citedSourceIDs, source.EvidenceID)
		if strings.TrimSpace(source.EvidenceID) == "" || !allowed[source.EvidenceID] ||
			strings.HasPrefix(source.EvidenceID, "legacy-entry:") {
			sourceOK, citationOK = false, false
		}
		if source.StartChar < 0 || source.StartChar >= source.EndChar ||
			!isDigest(source.SpanDigest) || source.SpanDigest != evalTextDigest(item.Text) {
			spanOK = false
		}
		if exists {
			if rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) ||
				!stringSet(rendered.SourceIDs)[source.EvidenceID] {
				citationOK = false
			}
		}
	}
	citedSourceIDs = stableStrings(citedSourceIDs)
	if evalJSONDigest(citedSourceIDs) != evalJSONDigest(stableStrings(bundle.SourceIDs)) ||
		len(citedSourceIDs) != len(bundle.SourceIDs) {
		sourceOK = false
	}
	if trace.SourceValidation.AllowedIDsDigest != evalJSONDigest(allowedSourceIDs) ||
		trace.SourceValidation.ResolvedCount != len(citedSourceIDs) ||
		trace.SourceValidation.InvalidCount != 0 {
		sourceOK = false
	}
	if len(trace.AppliedActions) != len(actions) {
		citationOK = false
	} else {
		for index := range actions {
			if trace.AppliedActions[index] != actions[index] {
				citationOK = false
			}
		}
	}
	return sourceOK, spanOK, citationOK
}

func revalidateFrozenFormalSources(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	qa locomoQA,
	frozen formalFrozenQuestion,
) formalFrozenQuestion {
	revalidated := cloneFormalFrozenQuestion(frozen)
	frozenValidation := revalidated.Bundle.ActiveValidation
	receipt, err := validateActiveFormalBundleReceipt(
		ctx, opt.formalEvidence, protocol, opt, qa,
		revalidated.Candidate, revalidated.Trace, revalidated.Bundle,
	)
	activeValidation := persistFormalActiveValidation(receipt)
	sourceStateDrift := frozenValidation.Checked && frozenValidation != activeValidation
	revalidated.Bundle.ActiveValidation = activeValidation
	revalidated.Bundle.SourceValid = receipt.SourceValid && receipt.InvalidCount == 0
	revalidated.Bundle.Valid = revalidated.Bundle.Valid && receipt.Valid() && !sourceStateDrift
	revalidated.Trace.Valid = revalidated.Trace.Valid && receipt.Valid() && !sourceStateDrift
	if sourceStateDrift {
		revalidated.InvalidReasons = stableStrings(append(
			revalidated.InvalidReasons,
			"source_state_drift",
		))
	}
	if err != nil {
		revalidated.InvalidReasons = stableStrings(append(
			revalidated.InvalidReasons,
			"source_span_or_citation_invalid",
		))
	}
	return revalidated
}
