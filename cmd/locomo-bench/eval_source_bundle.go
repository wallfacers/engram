package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// formalEvidenceReader is deliberately narrower than LedgerStore. Formal
// evaluation may resolve only an already-declared Evidence allowlist; it
// cannot search the Ledger or silently recover a different source.
type formalEvidenceReader interface {
	GetMany(context.Context, []string) (map[string]memory.Evidence, error)
}

type formalExpandedSource struct {
	Ref       memory.EvidenceRef
	Evidence  memory.Evidence
	Result    memory.Result
	Candidate evalRenderedCandidate
	Item      evalFormalBundleItem
}

type formalExpandedAnchor struct {
	Hit         memory.Result
	CandidateID string
	SourceIDs   []string
	Sources     []formalExpandedSource
}

// expandFormalEvidence resolves each navigation hit through its direct
// Projection→Evidence references, then rereads the active Ledger bytes. This
// happens before token admission: short projection text can never be used as a
// proxy for the longer answer-facing source text.
func expandFormalEvidence(
	ctx context.Context,
	projections *memory.ProjectionStore,
	reader formalEvidenceReader,
	hits []memory.Result,
) ([]formalExpandedAnchor, error) {
	if projections == nil || reader == nil {
		return nil, fmt.Errorf("formal source expansion requires projection and Evidence readers")
	}
	projectionIDs := make([]string, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.ProjectionID) == "" {
			return nil, fmt.Errorf("candidate %q has no projection identity", hit.Name)
		}
		projectionIDs = append(projectionIDs, hit.ProjectionID)
	}
	refsByProjection, err := projections.SourcesByProjectionIDs(ctx, projectionIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve formal projection sources: %w", err)
	}
	var evidenceIDs []string
	for _, hit := range hits {
		refs := refsByProjection[hit.ProjectionID]
		if len(refs) == 0 {
			return nil, fmt.Errorf("candidate %q has no direct Evidence source", hit.Name)
		}
		for _, ref := range refs {
			evidenceIDs = append(evidenceIDs, ref.EvidenceID)
		}
	}
	evidenceByID, err := reader.GetMany(ctx, evidenceIDs)
	if err != nil {
		return nil, fmt.Errorf("read active formal Evidence: %w", err)
	}

	anchors := make([]formalExpandedAnchor, 0, len(hits))
	renderedRank := 0
	for _, hit := range hits {
		anchorID := formalCandidateID(hit)
		refs := refsByProjection[hit.ProjectionID]
		anchor := formalExpandedAnchor{
			Hit:         hit,
			CandidateID: anchorID,
			SourceIDs:   orderedFormalSourceIDs(refs),
			Sources:     make([]formalExpandedSource, 0, len(refs)),
		}
		for _, ref := range refs {
			evidence, ok := evidenceByID[ref.EvidenceID]
			if !ok {
				return nil, fmt.Errorf("active Evidence %q was not resolved", ref.EvidenceID)
			}
			text, span, kind, err := formalEvidenceRefSpan(evidence, ref)
			if err != nil {
				return nil, fmt.Errorf("candidate %q source %q: %w", anchorID, ref.EvidenceID, err)
			}
			renderedRank++
			renderedID := formalRenderedSourceID(anchorID, ref.SourceOrder, ref.EvidenceID)
			result := formalEvidenceResult(renderedID, text, evidence)
			candidate := evalRenderedCandidate{
				CandidateID: renderedID,
				Kind:        "raw_turn",
				Rank:        renderedRank,
				Score:       hit.Score,
				Text:        text,
				TextDigest:  evalTextDigest(text),
				SourceIDs:   []string{evidence.ID},
				ExpandedFrom: []string{
					anchorID,
				},
				ExpansionCount: len(refs) - 1,
			}
			item := evalFormalBundleItem{
				ItemID:       formalBundleItemID(renderedID),
				Kind:         kind,
				Text:         text,
				CandidateIDs: []string{renderedID},
				Sources:      []evalFormalSourceSpan{span},
			}
			anchor.Sources = append(anchor.Sources, formalExpandedSource{
				Ref: ref, Evidence: evidence, Result: result, Candidate: candidate, Item: item,
			})
		}
		anchors = append(anchors, anchor)
	}
	return anchors, nil
}

func formalEvidenceRefSpan(evidence memory.Evidence, ref memory.EvidenceRef) (string, evalFormalSourceSpan, string, error) {
	if evidence.ID == "" || evidence.ID != ref.EvidenceID || evidence.State != memory.EvidenceActive {
		return "", evalFormalSourceSpan{}, "", fmt.Errorf("Evidence identity or active state mismatch")
	}
	if evidence.SourceType != memory.EvidenceMessage {
		return "", evalFormalSourceSpan{}, "", fmt.Errorf(
			"answer-facing B1 source %q has type %q, want raw message Evidence",
			evidence.ID, evidence.SourceType,
		)
	}
	if formalArtifactDigest(evidence.ContentDigest) != evalTextDigest(evidence.Content) {
		return "", evalFormalSourceSpan{}, "", fmt.Errorf("Evidence content digest mismatch")
	}
	runes := []rune(evidence.Content)
	start, end := 0, len(runes)
	kind := "KEEP"
	if ref.FullSource {
		if ref.StartChar != nil || ref.EndChar != nil || strings.TrimSpace(ref.SpanDigest) != "" {
			return "", evalFormalSourceSpan{}, "", fmt.Errorf("full-source ref contains span fields")
		}
	} else {
		if ref.StartChar == nil || ref.EndChar == nil {
			return "", evalFormalSourceSpan{}, "", fmt.Errorf("partial source ref is missing offsets")
		}
		start, end = *ref.StartChar, *ref.EndChar
		if start < 0 || start >= end || end > len(runes) {
			return "", evalFormalSourceSpan{}, "", fmt.Errorf("source span [%d,%d) exceeds %d code points", start, end, len(runes))
		}
		kind = "EXTRACT"
	}
	text := string(runes[start:end])
	digest := evalTextDigest(text)
	if !ref.FullSource && formalArtifactDigest(ref.SpanDigest) != digest {
		return "", evalFormalSourceSpan{}, "", fmt.Errorf("source span digest mismatch")
	}
	return text, evalFormalSourceSpan{
		EvidenceID: evidence.ID,
		StartChar:  start,
		EndChar:    end,
		SpanDigest: digest,
	}, kind, nil
}

func formalArtifactDigest(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func orderedFormalSourceIDs(refs []memory.EvidenceRef) []string {
	seen := make(map[string]bool, len(refs))
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.EvidenceID != "" && !seen[ref.EvidenceID] {
			seen[ref.EvidenceID] = true
			ids = append(ids, ref.EvidenceID)
		}
	}
	return ids
}

func formalRenderedSourceID(anchorID string, sourceOrder int, evidenceID string) string {
	return anchorID + "/source:" + strconv.Itoa(sourceOrder) + ":" + evidenceID
}

func formalBundleItemID(renderedCandidateID string) string {
	return "bundle:" + renderedCandidateID
}

func formalEvidenceResult(id, text string, evidence memory.Evidence) memory.Result {
	result := memory.Result{
		ID:              id,
		Name:            id,
		Content:         text,
		SourceSessionID: evidence.SourceSessionID,
		CreatedAt:       evidence.RecordedAt,
	}
	if evidence.OccurredAt != nil {
		occurred := evidence.OccurredAt.UTC()
		result.EventDate = &occurred
	}
	return result
}

func buildExpandedFormalCandidateArtifact(
	protocol evalProtocol,
	qa locomoQA,
	anchors []formalExpandedAnchor,
	turnEvidence map[string]string,
	retrievalCalls int,
) evalCandidateArtifact {
	ranked := make([]evalRankedAnchor, 0, len(anchors))
	var rendered []evalRenderedCandidate
	for index, anchor := range anchors {
		ranked = append(ranked, evalRankedAnchor{
			CandidateID: anchor.CandidateID,
			Rank:        index + 1,
			Score:       anchor.Hit.Score,
			TextDigest:  evalTextDigest(anchor.Hit.Content),
			SourceIDs:   append([]string(nil), anchor.SourceIDs...),
		})
		for _, source := range anchor.Sources {
			rendered = append(rendered, source.Candidate)
		}
	}
	resolved, unresolved, _ := resolveDatasetSourceIDs(qa.Evidence, turnEvidence)
	artifact := evalCandidateArtifact{
		Schema:             evalProtocolSchema,
		ProtocolHash:       protocol.ProtocolHash,
		QuestionID:         qa.QuestionID,
		QueryDigest:        evalTextDigest(qa.Question),
		Mode:               evalCandidateModeAnchorRendering,
		RetrievalCalls:     retrievalCalls,
		Anchors:            ranked,
		RenderedCandidates: rendered,
		Gold: evalGoldResolution{
			DatasetSourceIDs:    stableStrings(qa.Evidence),
			ResolvedEvidenceIDs: resolved,
			UnresolvedIDs:       unresolved,
		},
	}
	artifact.AnchorDigest = rankedAnchorDigest(artifact.Anchors)
	artifact.CandidateSetDigest = renderedCandidateSetDigest(artifact.RenderedCandidates)
	artifact.Gold.AnchorSourceCoverage = sourceCoverage(artifact.Gold.ResolvedEvidenceIDs, collectAnchorSources(artifact.Anchors))
	artifact.Gold.RenderedSourceCoverage = sourceCoverage(artifact.Gold.ResolvedEvidenceIDs, collectRenderedSources(artifact.RenderedCandidates))
	artifact.CoverageStratum = coverageStratumFor(protocol.CoverageStrata.Boundaries, artifact.Gold.RenderedSourceCoverage)
	return artifact
}

// packExpandedFormalInput preserves B1's legacy ranked-prefix policy while
// admitting whole navigation anchors. Every trial renders the exact active
// Evidence bytes and counts the complete answer input.
func packExpandedFormalInput(
	ctx context.Context,
	protocol evalProtocol,
	counter evidencecompiler.TokenCounter,
	system string,
	qa locomoQA,
	anchors []formalExpandedAnchor,
	scaffold bool,
) ([]formalExpandedSource, evidencecompiler.AnswerInput, evidencecompiler.TokenCount, int, error) {
	if counter == nil {
		return nil, evidencecompiler.AnswerInput{}, evidencecompiler.TokenCount{}, 0, fmt.Errorf("formal 022 evaluation requires a token counter")
	}
	render := func(selected []formalExpandedSource) evidencecompiler.AnswerInput {
		hits := make([]memory.Result, 0, len(selected))
		for _, source := range selected {
			hits = append(hits, source.Result)
		}
		return evidencecompiler.AnswerInput{
			Model:  protocol.Models.Answerer.ID,
			System: system,
			User:   buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, scaffold),
		}
	}
	var selected []formalExpandedSource
	input := render(selected)
	count, fits, err := countFormalPackInput(ctx, protocol, counter, input)
	if err != nil {
		return nil, input, evidencecompiler.TokenCount{}, 0, err
	}
	if !fits {
		return nil, input, count, 0, fmt.Errorf("formal static answer prompt exceeds token cap %d", protocol.Budget.AnswerInputTokenCap)
	}
	staticInputTokens := count.InputTokens
	for _, anchor := range anchors {
		trial := append(append([]formalExpandedSource(nil), selected...), anchor.Sources...)
		trialInput := render(trial)
		trialCount, trialFits, err := countFormalPackInput(ctx, protocol, counter, trialInput)
		if err != nil {
			return nil, input, count, 0, err
		}
		if !trialFits {
			if len(selected) == 0 && len(anchor.Sources) > 0 {
				return nil, input, count, 0, fmt.Errorf("no complete source-expanded anchor fits token cap %d", protocol.Budget.AnswerInputTokenCap)
			}
			break
		}
		selected = trial
		input = trialInput
		count = trialCount
	}
	evidenceTokens := count.InputTokens - staticInputTokens
	if evidenceTokens < 0 {
		evidenceTokens = 0
	}
	return selected, input, count, evidenceTokens, nil
}

func formalBundleItems(sources []formalExpandedSource) []evalFormalBundleItem {
	items := make([]evalFormalBundleItem, 0, len(sources))
	for _, source := range sources {
		items = append(items, source.Item)
	}
	return items
}

func buildExpandedFormalBundle(
	protocol evalProtocol,
	questionID string,
	candidate evalCandidateArtifact,
	trace evalFormalTraceRecord,
	sources []formalExpandedSource,
	input evidencecompiler.AnswerInput,
) evalFormalBundleRecord {
	items := formalBundleItems(sources)
	var sourceIDs []string
	for _, item := range items {
		for _, source := range item.Sources {
			sourceIDs = append(sourceIDs, source.EvidenceID)
		}
	}
	sourceIDs = stableStrings(sourceIDs)
	sourceValid := len(items) > 0 && len(sourceIDs) > 0
	return evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{
			Schema:       evalProtocolSchema,
			ProtocolHash: protocol.ProtocolHash,
			QuestionID:   questionID,
			Kind:         evalBundleArtifactKind,
			Valid:        sourceValid,
		},
		CandidateSetDigest: candidate.CandidateSetDigest,
		TraceDigest:        trace.TraceDigest,
		Items:              items,
		SourceIDs:          sourceIDs,
		RenderedContext:    input.User,
		RenderedDigest:     evalTextDigest(input.User),
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		SourceValid:        sourceValid,
		AnswerPromptDigest: evalTextDigest(input.System),
	}
}
