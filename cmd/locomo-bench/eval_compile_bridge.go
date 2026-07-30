package main

import (
	"context"
	"fmt"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// buildCompileCandidates converts formal expanded sources into the
// evidencecompiler.Candidate format expected by Compile. Each source maps to
// one candidate with its evidence text, rank, and score.
func buildCompileCandidates(sources []formalExpandedSource) []evidencecompiler.Candidate {
	candidates := make([]evidencecompiler.Candidate, 0, len(sources))
	for _, source := range sources {
		candidates = append(candidates, evidencecompiler.Candidate{
			ID:         source.Candidate.CandidateID,
			Kind:       evidencecompiler.CandidateKind(source.Candidate.Kind),
			Rank:       source.Candidate.Rank,
			Score:      source.Candidate.Score,
			Text:       source.Result.Content,
			TextDigest: evalTextDigest(source.Result.Content),
			SourceIDs:  append([]string(nil), source.Candidate.SourceIDs...),
		})
	}
	return candidates
}

// formalCompileRenderer adapts the formal answer prompt pipeline to the
// evidencecompiler.AnswerRenderer interface. It passes through the rendered
// evidence text so the compiler can count tokens; the actual answer input is
// rebuilt from the mapped bundle items after compilation.
type formalCompileRenderer struct {
	model  string
	system string
}

func (r formalCompileRenderer) RenderAnswerInput(query string, renderedEvidence string) evidencecompiler.AnswerInput {
	return evidencecompiler.AnswerInput{
		Model:  r.model,
		System: r.system,
		User:   renderedEvidence,
	}
}

// compileBundleItems maps the compiler's output BundleItems to eval formal
// bundle items. Each BundleItem with a single source span is mapped to an
// evalFormalBundleItem with the same text and source identity.
func compileBundleItems(protocol evalProtocol, compiledItems []evidencecompiler.BundleItem) []evalFormalBundleItem {
	items := make([]evalFormalBundleItem, 0, len(compiledItems))
	for _, item := range compiledItems {
		if len(item.Sources) == 0 || len(item.CandidateIDs) == 0 {
			continue
		}
		source := item.Sources[0]
		items = append(items, evalFormalBundleItem{
			ItemID:       formalBundleItemID(item.CandidateIDs[0]),
			Kind:         string(item.Kind),
			Text:         item.Text,
			CandidateIDs: append([]string(nil), item.CandidateIDs...),
			Sources: []evalFormalSourceSpan{{
				EvidenceID: source.SourceID,
				StartChar:  source.StartChar,
				EndChar:    source.EndChar,
				SpanDigest: formalArtifactDigest(source.SpanDigest),
			}},
		})
	}
	return items
}

// compileRenderedCandidates builds rendered candidates from the compiler's
// output bundle items. Each item becomes one rendered candidate with the
// source evidence identity preserved.
func compileRenderedCandidates(items []evidencecompiler.BundleItem) []evalRenderedCandidate {
	candidates := make([]evalRenderedCandidate, 0, len(items))
	rank := 0
	for _, item := range items {
		if len(item.CandidateIDs) == 0 || len(item.Sources) == 0 {
			continue
		}
		rank++
		candidateID := item.CandidateIDs[0]
		source := item.Sources[0]
		candidates = append(candidates, evalRenderedCandidate{
			CandidateID:    candidateID,
			Kind:           string(item.Kind),
			Rank:           rank,
			Score:          0,
			Text:           item.Text,
			TextDigest:     evalTextDigest(item.Text),
			SourceIDs:      []string{source.SourceID},
			ExpandedFrom:   nil,
			ExpansionCount: 0,
		})
	}
	return candidates
}

// buildCompileTrace maps the compiler's Trace to the formal eval trace record.
func buildCompileTrace(protocol evalProtocol, questionID string, candidate evalCandidateArtifact, compiledTrace evidencecompiler.Trace, items []evalFormalBundleItem) evalFormalTraceRecord {
	actions := make([]string, 0, len(items))
	var resolved []string
	for _, item := range items {
		actions = append(actions, item.Kind)
		for _, source := range item.Sources {
			resolved = append(resolved, source.EvidenceID)
		}
	}
	allowed := stableStrings(collectRenderedSources(candidate.RenderedCandidates))
	trace := evalFormalTraceRecord{
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalTraceArtifactKind, Valid: compiledTrace.Valid},
		Attempt:            1,
		CandidateSetDigest: candidate.CandidateSetDigest,
		AppliedActions:     actions,
		FallbackReason:     compiledTrace.FallbackReason,
		SourceValidation: evalFormalSourceValidation{
			AllowedIDsDigest: evalJSONDigest(allowed),
			ResolvedCount:    len(stableStrings(resolved)),
		},
	}
	trace.TraceDigest = formalTraceDigest(trace)
	return trace
}

// buildCompileBundle maps the compiler's output Bundle to the formal eval
// bundle record. It rebuilds the answer input through buildAnswerContextPrompt
// to maintain byte compatibility with the legacy format.
func buildCompileBundle(ctx context.Context, protocol evalProtocol, opt options, qa locomoQA, candidate evalCandidateArtifact, trace evalFormalTraceRecord, compiledBundle evidencecompiler.Bundle, compiledItems []evidencecompiler.BundleItem, items []evalFormalBundleItem) (evalFormalBundleRecord, int, evidencecompiler.TokenCount, error) {
	resultHits := make([]memory.Result, 0, len(compiledItems))
	for _, compiledItem := range compiledItems {
		if len(compiledItem.Sources) == 0 {
			continue
		}
		sourceID := compiledItem.Sources[0].SourceID
		resultHits = append(resultHits, memory.Result{
			ID:      sourceID,
			Name:    sourceID,
			Content: compiledItem.Text,
		})
	}

	system := withCurrentDateRule(
		answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		qa.QuestionDate,
	)
	answerInput := evidencecompiler.AnswerInput{
		Model:  protocol.Models.Answerer.ID,
		System: system,
		User:   buildAnswerContextPrompt(qa.Question, resultHits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold),
	}

	var sourceIDs []string
	for _, item := range items {
		for _, source := range item.Sources {
			sourceIDs = append(sourceIDs, source.EvidenceID)
		}
	}
	sourceIDs = stableStrings(sourceIDs)
	sourceValid := len(items) > 0 && len(sourceIDs) > 0

	bundle := evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{
			Schema:       evalProtocolSchema,
			ProtocolHash: protocol.ProtocolHash,
			QuestionID:   qa.QuestionID,
			Kind:         evalBundleArtifactKind,
			Valid:        sourceValid,
		},
		CandidateSetDigest: candidate.CandidateSetDigest,
		TraceDigest:        trace.TraceDigest,
		Items:              items,
		SourceIDs:          sourceIDs,
		RenderedContext:    answerInput.User,
		RenderedDigest:     evalTextDigest(answerInput.User),
		EvidenceTokens:     compiledBundle.EvidenceTokens,
		AnswerInputTokens:  compiledBundle.InputTokens,
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		WithinCap:          compiledBundle.InputTokens > 0 && compiledBundle.InputTokens <= compiledBundle.TokenCap,
		SourceValid:        sourceValid,
		AnswerPromptDigest: evalTextDigest(answerInput.System),
	}

	return bundle, compiledBundle.InputTokens, evidencecompiler.TokenCount{InputTokens: compiledBundle.InputTokens, Fingerprint: compiledBundle.CounterFingerprint}, nil
}

// compileFormalSources converts expanded sources to compiler Candidates and
// configures the compile request with the formal pipeline dependencies.
func compileFormalSources(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	qa locomoQA,
	expanded []formalExpandedAnchor,
) (evidencecompiler.Bundle, evidencecompiler.Trace, error) {
	// Collect all expanded sources into a flat candidate list.
	var allSources []formalExpandedSource
	for _, anchor := range expanded {
		allSources = append(allSources, anchor.Sources...)
	}
	if len(allSources) == 0 {
		return evidencecompiler.Bundle{}, evidencecompiler.Trace{}, fmt.Errorf("no expanded sources for compilation")
	}

	candidates := buildCompileCandidates(allSources)

	// Count unique source IDs for MaxSources.
	srcIDs := make(map[string]bool)
	for _, source := range allSources {
		for _, id := range source.Candidate.SourceIDs {
			srcIDs[id] = true
		}
	}

	resolver := evidencecompiler.LedgerResolver{Reader: opt.formalEvidence}
	renderer := formalCompileRenderer{
		model:  protocol.Models.Answerer.ID,
		system: withCurrentDateRule(answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt), qa.QuestionDate),
	}

	return evidencecompiler.Compile(ctx, evidencecompiler.CompileRequest{
		Query:      qa.Question,
		Candidates: candidates,
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		MaxCandidates:      len(candidates),
		MaxSources:         len(srcIDs),
		Counter:            opt.formalCounter,
		Resolver:           resolver,
		Renderer:           renderer,
	})
}
