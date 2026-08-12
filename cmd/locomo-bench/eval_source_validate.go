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
	if err := validateFormalB1AnchorPrefix(protocol, candidate, bundle); err != nil {
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
		rendered, renderedExists := renderedByID[item.CandidateIDs[0]]
		isEpisodeItem := renderedExists && isGenuineEpisodeRendered(rendered)
		isChunkVerbatimItem := renderedExists && isChunkVerbatimRendered(rendered)
		if len(item.CandidateIDs) != 1 {
			spanOK, citationOK = false, false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("B1 item %q must have exactly one candidate", item.ItemID))
			continue
		}
		if isEpisodeItem {
			// 025: an episode item aggregates a cross-message cluster. Each source
			// must be whole-source (KEEP) and recoverable; the item text is the
			// deterministic narrative of those sources (research.md R5).
			recoveredNarrative, ok := recoverEpisodeNarrative(item, evidenceByID)
			if !ok {
				spanOK, citationOK = false, false
				contextRecoverable = false
				problems = append(problems, fmt.Sprintf("episode item %q narrative/span cannot be recovered", item.ItemID))
				continue
			}
			if item.Text != recoveredNarrative || rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) ||
				!sameOrderedStrings(rendered.SourceIDs, episodeItemSourceIDs(item)) {
				citationOK = false
				problems = append(problems, fmt.Sprintf("episode item %q differs from its frozen rendered candidate", item.ItemID))
			}
			// The episode item is one aggregated narrative in the answer input,
			// so reconstruction appends exactly one result (matching the
			// one-result-per-item contract below). Its identity must match the
			// packer's episode Result (Name = the episode candidate ID, not the
			// bundle item ID); session/date identity comes from the first
			// whole-source span.
			if len(item.Sources) > 0 {
				first, ok := evidenceByID[item.Sources[0].EvidenceID]
				if !ok {
					spanOK = false
					contextRecoverable = false
					problems = append(problems, fmt.Sprintf("episode item %q first source cannot be resolved", item.ItemID))
				} else {
					result := formalEvidenceResult(item.CandidateIDs[0], item.Text, first)
					results = append(results, result)
				}
			}
			continue
		}
		if isChunkVerbatimItem {
			// 027: a chunk-verbatim item packs the projection's own text with
			// every member evidence as a whole-source span. The text equals the
			// frozen rendered candidate's text (it may be a verbatim chunk
			// concatenation or a condensed fact that no single span can recover),
			// so citation is proven against the rendered candidate while each
			// span stays independently recoverable from the Ledger.
			spanGood := true
			for _, span := range item.Sources {
				evidence, ok := evidenceByID[span.EvidenceID]
				if !ok {
					spanOK = false
					contextRecoverable = false
					problems = append(problems, fmt.Sprintf("chunk-verbatim item %q cites unavailable Evidence %q", item.ItemID, span.EvidenceID))
					spanGood = false
					continue
				}
				runes := []rune(evidence.Content)
				if span.StartChar != 0 || span.EndChar != len(runes) ||
					span.SpanDigest != evalTextDigest(evidence.Content) {
					spanOK = false
					contextRecoverable = false
					problems = append(problems, fmt.Sprintf("chunk-verbatim item %q span %q is not whole-source", item.ItemID, span.EvidenceID))
					spanGood = false
				}
			}
			if !spanGood {
				continue
			}
			if rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) ||
				!sameSet(rendered.SourceIDs, episodeItemSourceIDs(item)) {
				citationOK = false
				problems = append(problems, fmt.Sprintf("chunk-verbatim item %q differs from its frozen rendered candidate", item.ItemID))
			}
			// The item is one aggregated text in the answer input, so
			// reconstruction appends exactly one result whose identity matches
			// the packer's folded Result (Name = the folded candidate ID);
			// session/date identity comes from the first whole-source span.
			if len(item.Sources) > 0 {
				first, ok := evidenceByID[item.Sources[0].EvidenceID]
				if !ok {
					spanOK = false
					contextRecoverable = false
					problems = append(problems, fmt.Sprintf("chunk-verbatim item %q first source cannot be resolved", item.ItemID))
				} else {
					result := formalEvidenceResult(item.CandidateIDs[0], item.Text, first)
					results = append(results, result)
				}
			}
			continue
		}
		if len(item.Sources) != 1 {
			spanOK, citationOK = false, false
			contextRecoverable = false
			problems = append(problems, fmt.Sprintf("B1 item %q must have exactly one source span", item.ItemID))
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
		if !renderedExists || rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) ||
			len(rendered.SourceIDs) != 1 || rendered.SourceIDs[0] != source.EvidenceID {
			citationOK = false
			problems = append(problems, fmt.Sprintf("item %q differs from its frozen rendered candidate", item.ItemID))
		}
		results = append(results, formalEvidenceResult(item.ItemID, item.Text, evidence))
	}

	system := withCurrentDateRule(
		answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt, opt.lmeTypedPrompts),
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

// isGenuineEpisodeRendered reports whether a rendered candidate is a genuine
// cross-message episode (candidate ID "…/episode", many sources) rather than the
// renderer's single-source fallback ("…/episode-fallback:<id>"). The 022 renderer
// tags both with Kind=semantic_episode, so the candidate ID suffix — not the
// Kind — is the disambiguator. (025 regression: fallback items treated as
// episodes failed recoverEpisodeNarrative and killed B1 citation validation.)
func isGenuineEpisodeRendered(rendered evalRenderedCandidate) bool {
	return rendered.Kind == string(ReprSemanticEpisode) && strings.HasSuffix(rendered.CandidateID, "/episode")
}

// isChunkVerbatimRendered reports whether a rendered candidate was folded by
// rebuildExpandedForChunkVerbatim: one candidate carrying the projection's own
// text (verbatim chunk concatenation or condensed fact) with every member
// evidence as a whole-source span. It is the chunk_900 analogue of the 025
// episode shape.
func isChunkVerbatimRendered(rendered evalRenderedCandidate) bool {
	return rendered.Kind == chunkVerbatimKind && strings.HasSuffix(rendered.CandidateID, "/verbatim")
}

// validateFormalB1AnchorPrefix proves that B1 packed exactly a ranked prefix
// of complete navigation anchors. Candidate membership alone is insufficient:
// it would permit skipping a large first anchor, reordering sources, or
// admitting only part of a multi-source anchor while still citing valid IDs.
func validateFormalB1AnchorPrefix(protocol evalProtocol, candidate evalCandidateArtifact, bundle evalFormalBundleRecord) error {
	if len(candidate.Anchors) == 0 || len(candidate.RenderedCandidates) == 0 {
		return fmt.Errorf("candidate has no ranked anchors or rendered sources")
	}
	// 025: a genuine semantic_episode rendered candidate aggregates a whole
	// cross-message cluster into one candidate (many SourceIDs), so the legacy
	// per-source 1:1 mapping below does not apply. Detect episode mode from the
	// candidate ID suffix (a genuine episode is "…/episode"), not from the Kind
	// alone: the renderer's single-source fallback also carries
	// Kind=semantic_episode but maps 1:1 per source like a raw turn.
	isEpisode := false
	for _, rendered := range candidate.RenderedCandidates {
		if isGenuineEpisodeRendered(rendered) {
			isEpisode = true
			break
		}
	}
	if isEpisode {
		return validateFormalB1EpisodeAnchorPrefix(candidate, bundle)
	}
	// 027: a chunk-verbatim rendered candidate folds each whole-source anchor
	// into one multi-source item (projection text + every member span), the
	// same one-candidate-per-anchor shape as a genuine episode. The episode
	// prefix branch already validates exactly that invariant (bundle is a
	// ranked prefix of anchors, each selected anchor contributes one item whose
	// text equals its rendered candidate and whose spans cover the rendered
	// source set), so it is reused rather than duplicated.
	isChunkVerbatim := false
	for _, rendered := range candidate.RenderedCandidates {
		if isChunkVerbatimRendered(rendered) {
			isChunkVerbatim = true
			break
		}
	}
	if isChunkVerbatim {
		return validateFormalB1EpisodeAnchorPrefix(candidate, bundle)
	}
	// 026: the query-time compiler arm (--compiler-arm) deliberately re-orders
	// and re-selects evidence by query relevance inside the frozen candidate
	// pool, so its bundle is not a mechanical ranked-anchor prefix. It is still
	// fully auditable: every bundle item must map 1:1 to a rendered candidate
	// (its candidate IDs and whole-source spans must resolve against the
	// candidate artifact), but item count/order need not be a prefix boundary.
	// This is the contract increment approved for 026 (density-mechanism-hypothesis
	// closed → post-hit verbatim coverage), mirroring how 025 relaxed the
	// per-source cardinality for episodes. 027's temporal-resolution arm
	// re-organizes the same candidate pool by query-time time semantics
	// (current-value / evolution-chain / temporal-window), so its bundle is also
	// not a mechanical ranked-anchor prefix — it maps 1:1 to rendered candidates
	// inside the pool and shares the compiler audit contract.
	if protocol.Experiment.MechanismFlags["compiler"] || protocol.Experiment.MechanismFlags["temporal_resolution"] {
		return validateFormalB1CompilerAnchorPrefix(candidate, bundle)
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

// validateFormalB1EpisodeAnchorPrefix is the 025 semantic_episode branch of the
// ranked-anchor prefix contract. A genuine episode renders exactly one candidate
// per anchor (a cross-message cluster with many SourceIDs), so the bundle is a
// ranked prefix of anchors where each selected anchor contributes one
// multi-source item. The bundle item for that anchor carries the same text and
// the same source set (whole-source spans), so answer context is auditable
// against the rendered candidate. This relaxes the legacy single-source
// cardinality without changing the chunk/raw-turn path (research.md R5,
// direction A).
func validateFormalB1EpisodeAnchorPrefix(candidate evalCandidateArtifact, bundle evalFormalBundleRecord) error {
	if len(candidate.Anchors) == 0 || len(candidate.RenderedCandidates) == 0 {
		return fmt.Errorf("episode candidate has no ranked anchors or rendered candidates")
	}
	if len(bundle.Items) == 0 || len(bundle.Items) > len(candidate.RenderedCandidates) {
		return fmt.Errorf("episode bundle has no items or exceeds rendered candidate count")
	}
	// The packer admits whole anchors only, so the bundle must end on a
	// complete-anchor boundary. With one episode rendered candidate per anchor,
	// the boundary index equals the number of selected anchors. (The fallback
	// renderer may emit one candidate per source instead, so a fallback item
	// keeps legacy 1:1 cardinality; the boundary below counts rendered
	// candidates per anchor accordingly.)
	renderedIndex := 0
	validBoundaries := map[int]bool{0: true}
	for anchorIndex, anchor := range candidate.Anchors {
		if anchor.Rank != anchorIndex+1 || strings.TrimSpace(anchor.CandidateID) == "" {
			return fmt.Errorf("episode anchor %d has invalid identity or rank", anchorIndex)
		}
		start := renderedIndex
		for renderedIndex < len(candidate.RenderedCandidates) {
			rendered := candidate.RenderedCandidates[renderedIndex]
			if len(rendered.ExpandedFrom) != 1 || rendered.ExpandedFrom[0] != anchor.CandidateID {
				break
			}
			if rendered.Rank != renderedIndex+1 {
				return fmt.Errorf("episode rendered candidate %d has invalid rank", renderedIndex)
			}
			renderedIndex++
		}
		if renderedIndex == start {
			return fmt.Errorf("episode anchor %q has no rendered candidate group", anchor.CandidateID)
		}
		validBoundaries[renderedIndex] = true
	}
	if renderedIndex != len(candidate.RenderedCandidates) {
		return fmt.Errorf("episode rendered candidates are not grouped in ranked anchor order")
	}
	if len(bundle.Items) == 0 || len(bundle.Items) > len(candidate.RenderedCandidates) ||
		!validBoundaries[len(bundle.Items)] {
		return fmt.Errorf("episode Bundle item count %d is not a complete-anchor prefix boundary", len(bundle.Items))
	}
	for index, item := range bundle.Items {
		rendered := candidate.RenderedCandidates[index]
		if item.ItemID != formalBundleItemID(rendered.CandidateID) ||
			len(item.CandidateIDs) != 1 || item.CandidateIDs[0] != rendered.CandidateID ||
			item.Text != rendered.Text {
			return fmt.Errorf("episode bundle item %d is not its rendered candidate %q", index, rendered.CandidateID)
		}
		// Every rendered source must be present as a span in the item. Genuine
		// episodes carry many whole-source spans; a single-source fallback item
		// carries exactly one.
		if len(rendered.SourceIDs) == 0 || len(item.Sources) != len(rendered.SourceIDs) {
			return fmt.Errorf("episode item %d source cardinality mismatch", index)
		}
		renderedSet := stringSet(rendered.SourceIDs)
		itemSet := make(map[string]bool, len(item.Sources))
		for _, span := range item.Sources {
			itemSet[span.EvidenceID] = true
		}
		for _, id := range rendered.SourceIDs {
			if !itemSet[id] {
				return fmt.Errorf("episode item %d misses rendered source %q", index, id)
			}
		}
		for _, span := range item.Sources {
			if !renderedSet[span.EvidenceID] {
				return fmt.Errorf("episode item %d has unrendered source %q", index, span.EvidenceID)
			}
		}
	}
	return nil
}

// validateFormalB1CompilerAnchorPrefix is the 026 query-time compiler branch of
// the ranked-anchor contract. The compiler arm re-orders and re-selects evidence
// by query relevance inside the frozen candidate pool (extractive / exact-token /
// verbatim-first), so the bundle is not a mechanical ranked-anchor prefix.
// Auditable guarantees kept: every bundle item maps 1:1 to a rendered candidate
// (candidate ID resolves; item text equals the rendered source text; the item's
// whole-source span is allowed and resolvable). Item count/order may differ from
// the prefix boundary because the compiler chooses by relevance — that is the
// mechanism under test (026), not a packing defect.
func validateFormalB1CompilerAnchorPrefix(candidate evalCandidateArtifact, bundle evalFormalBundleRecord) error {
	if len(candidate.RenderedCandidates) == 0 {
		return fmt.Errorf("compiler candidate has no rendered sources")
	}
	if len(bundle.Items) == 0 {
		return fmt.Errorf("compiler bundle has no items")
	}
	renderedByID := make(map[string]evalRenderedCandidate, len(candidate.RenderedCandidates))
	allowedSourceIDs := make([]string, 0, len(candidate.RenderedCandidates))
	for _, rendered := range candidate.RenderedCandidates {
		if rendered.CandidateID == "" {
			return fmt.Errorf("compiler rendered candidate has empty ID")
		}
		renderedByID[rendered.CandidateID] = rendered
		allowedSourceIDs = append(allowedSourceIDs, rendered.SourceIDs...)
	}
	allowed := stringSet(stableStrings(allowedSourceIDs))

	seen := make(map[string]bool, len(bundle.Items))
	for index, item := range bundle.Items {
		if strings.TrimSpace(item.ItemID) == "" || strings.TrimSpace(item.Text) == "" || seen[item.ItemID] {
			return fmt.Errorf("compiler bundle item %d has empty/duplicate identity or text", index)
		}
		seen[item.ItemID] = true
		if item.Kind != "KEEP" && item.Kind != "EXTRACT" {
			return fmt.Errorf("compiler bundle item %d has non-verbatim kind %q (only KEEP/EXTRACT allowed)", index, item.Kind)
		}
		if len(item.CandidateIDs) != 1 {
			return fmt.Errorf("compiler bundle item %d must reference exactly one candidate", index)
		}
		rendered, ok := renderedByID[item.CandidateIDs[0]]
		if !ok {
			return fmt.Errorf("compiler bundle item %d references unrendered candidate %q", index, item.CandidateIDs[0])
		}
		if item.Text != rendered.Text {
			return fmt.Errorf("compiler bundle item %d text differs from rendered candidate %q", index, rendered.CandidateID)
		}
		if len(item.Sources) != 1 {
			return fmt.Errorf("compiler bundle item %d must carry exactly one source span", index)
		}
		span := item.Sources[0]
		if strings.TrimSpace(span.EvidenceID) == "" || !allowed[span.EvidenceID] ||
			strings.HasPrefix(span.EvidenceID, "legacy-entry:") {
			return fmt.Errorf("compiler bundle item %d has disallowed source %q", index, span.EvidenceID)
		}
		if span.StartChar < 0 || span.StartChar >= span.EndChar || !isDigest(span.SpanDigest) {
			return fmt.Errorf("compiler bundle item %d has invalid span", index)
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

// sameSet compares two string slices as unordered sets.
func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := stringSet(left)
	for _, value := range right {
		if !leftSet[value] {
			return false
		}
	}
	return true
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

// recoverEpisodeNarrative rebuilds the deterministic episode narrative from the
// item's whole-source spans, matching semanticEpisodeRenderer's rendering
// (`content + "\n"` in source order, representation_eval.go). Every span must be
// whole-source (KEEP, offsets [0,len)) and resolvable, else the episode cannot
// be independently recovered from the Ledger.
func recoverEpisodeNarrative(item evalFormalBundleItem, evidenceByID map[string]memory.Evidence) (string, bool) {
	if len(item.Sources) == 0 {
		return "", false
	}
	var sb strings.Builder
	for _, span := range item.Sources {
		evidence, ok := evidenceByID[span.EvidenceID]
		if !ok {
			return "", false
		}
		runes := []rune(evidence.Content)
		if span.StartChar != 0 || span.EndChar != len(runes) {
			return "", false // whole-source only
		}
		content := string(runes)
		if span.SpanDigest != evalTextDigest(content) {
			return "", false
		}
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	return sb.String(), true
}

// episodeItemSourceIDs extracts the ordered evidence IDs from an episode item's
// spans for comparison against the rendered candidate's SourceIDs.
func episodeItemSourceIDs(item evalFormalBundleItem) []string {
	ids := make([]string, len(item.Sources))
	for i, span := range item.Sources {
		ids[i] = span.EvidenceID
	}
	return ids
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
		if len(item.CandidateIDs) != 1 {
			spanOK, citationOK = false, false
			continue
		}
		rendered, exists := renderedByID[item.CandidateIDs[0]]
		if !exists {
			citationOK = false
		}
		if exists && (isGenuineEpisodeRendered(rendered) || isChunkVerbatimRendered(rendered)) {
			// 025 episode / 027 chunk-verbatim multi-source item: every span must
			// be allowed and resolvable; the rendered SourceIDs set must equal the
			// item's set; the item text must equal the rendered candidate's text.
			if len(item.Sources) == 0 || len(rendered.SourceIDs) != len(item.Sources) {
				spanOK, citationOK = false, false
				continue
			}
			itemSet := make(map[string]bool, len(item.Sources))
			for _, source := range item.Sources {
				itemSet[source.EvidenceID] = true
				citedSourceIDs = append(citedSourceIDs, source.EvidenceID)
				if strings.TrimSpace(source.EvidenceID) == "" || !allowed[source.EvidenceID] ||
					strings.HasPrefix(source.EvidenceID, "legacy-entry:") {
					sourceOK, citationOK = false, false
				}
				if source.StartChar < 0 || source.StartChar >= source.EndChar ||
					!isDigest(source.SpanDigest) {
					spanOK = false
				}
			}
			for _, id := range rendered.SourceIDs {
				if !itemSet[id] {
					citationOK = false
				}
			}
			if rendered.Text != item.Text || rendered.TextDigest != evalTextDigest(item.Text) {
				citationOK = false
			}
			continue
		}
		if len(item.Sources) != 1 {
			spanOK, citationOK = false, false
			continue
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
	// 026: Valid was just mutated from the receipt, but TraceDigest was computed
	// before this mutation. Without a re-derive, a later envelope check
	// (trace.TraceDigest == formalTraceDigest(trace)) fails and invalidates the
	// whole bundle on the second pass, surfacing as source_state_drift.
	revalidated.Trace.TraceDigest = formalTraceDigest(revalidated.Trace)
	revalidated.Bundle.TraceDigest = revalidated.Trace.TraceDigest
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
