package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// buildCompileCandidates converts formal expanded sources into the
// evidencecompiler.Candidate format expected by Compile. Each source maps to
// one candidate with its evidence text, rank, and score.
func buildCompileCandidates(sources []formalExpandedSource) []evidencecompiler.Candidate {
	// 026: the flat source list carries one entry per navigation hit, and
	// several hits can reference the same rendered candidate (e.g. an evidence
	// span surfaced by multiple projections or anchors). Without dedup the
	// compiler receives the same candidate twice, emits duplicate bundle items,
	// and the formal 1:1 item->candidate contract fails. First occurrence wins,
	// mirroring compileSourceByCandidateID.
	seen := make(map[string]bool, len(sources))
	candidates := make([]evidencecompiler.Candidate, 0, len(sources))
	for _, source := range sources {
		// A blank rendered candidate ID (offline mocks, degenerate hits) is kept
		// as-is; only non-blank duplicates of the same candidate are collapsed.
		if source.Candidate.CandidateID != "" {
			if seen[source.Candidate.CandidateID] {
				continue
			}
			seen[source.Candidate.CandidateID] = true
		}
		candidates = append(candidates, evidencecompiler.Candidate{
			ID:    source.Candidate.CandidateID,
			Kind:  evidencecompiler.CandidateKind(source.Candidate.Kind),
			Rank:  source.Candidate.Rank,
			Score: source.Candidate.Score,
			Text:  source.Result.Content,
			// evidencecompiler.sameDigest expects a bare 64-char SHA-256 hex (no
			// "sha256:" prefix); evalTextDigest returns the prefixed artifact form.
			TextDigest: strings.TrimPrefix(evalTextDigest(source.Result.Content), "sha256:"),
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
	// 026: the compiler's budget decision must cover the same text the answer
	// provider will actually see. The formal answer input is question + rendered
	// evidence (buildAnswerContextPrompt), so counting only the evidence here
	// lets the compiler overshoot the token cap and makes every frozen
	// AnswerInputTokens disagree with the harness preflight at answer time.
	// Including the query keeps the compiler's admit/drop arithmetic on the same
	// scale as the harness preflight (the exact final count is re-derived from
	// the harness counter in buildCompileBundle).
	return evidencecompiler.AnswerInput{
		Model:  r.model,
		System: r.system,
		User:   query + "\n" + renderedEvidence,
	}
}

// compileSourceByCandidateID indexes the canonical formal expansion by rendered
// candidate ID so compiler-built bundle items can reuse the exact, formal-
// contract-compatible Evidence span (ref offsets + span text) that expansion
// already produced. The compiler selects which sources enter the bundle; it
// does not redefine their spans or answer-facing text.
func compileSourceByCandidateID(expanded []formalExpandedAnchor) map[string]formalExpandedSource {
	byID := make(map[string]formalExpandedSource)
	for _, anchor := range expanded {
		for _, source := range anchor.Sources {
			if _, exists := byID[source.Candidate.CandidateID]; !exists {
				byID[source.Candidate.CandidateID] = source
			}
		}
	}
	return byID
}

// compileBundleItems maps the compiler's selected items back to the formal
// bundle item shape. Each compiler item must reference a rendered candidate
// that expansion already materialized; we reuse expansion's evalFormalBundleItem
// (verbatim ref span + span text) so the formal source/span/citation contract
// holds. The compiler's grounded text is a selection signal only.
//
// 026: the formal contract is one item per rendered candidate. The compiler's
// admission is keyed by Evidence source, so a candidate that spans several
// sources can surface as several KEEP items carrying the same CandidateIDs[0]
// (same rendered candidate, identical grounded text). Emitting them all would
// duplicate the item identity and fail the 1:1 structural check; the first
// occurrence wins, mirroring buildCompileCandidates and compileSourceByCandidateID.
func compileBundleItems(protocol evalProtocol, compiledItems []evidencecompiler.BundleItem, sourceByCandidate map[string]formalExpandedSource) []evalFormalBundleItem {
	seen := make(map[string]bool, len(compiledItems))
	items := make([]evalFormalBundleItem, 0, len(compiledItems))
	for _, compiled := range compiledItems {
		if len(compiled.CandidateIDs) == 0 || len(compiled.Sources) == 0 {
			continue
		}
		key := compiled.CandidateIDs[0]
		if seen[key] {
			continue
		}
		seen[key] = true
		source, ok := sourceByCandidate[key]
		if !ok {
			continue
		}
		item := source.Item
		item.ItemID = formalBundleItemID(key)
		items = append(items, item)
	}
	return items
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
func buildCompileBundle(ctx context.Context, protocol evalProtocol, opt options, qa locomoQA, candidate evalCandidateArtifact, trace evalFormalTraceRecord, compiledBundle evidencecompiler.Bundle, sourceByCandidate map[string]formalExpandedSource, items []evalFormalBundleItem) (evalFormalBundleRecord, int, evidencecompiler.TokenCount, error) {
	resultHits := make([]memory.Result, 0, len(items))
	for _, item := range items {
		if len(item.Sources) == 0 || len(item.CandidateIDs) == 0 {
			continue
		}
		source, ok := sourceByCandidate[item.CandidateIDs[0]]
		if !ok {
			continue
		}
		// Match validateActiveFormalBundleReceipt's reconstruction exactly: it
		// rebuilds each hit via formalEvidenceResult(item.ItemID, item.Text, evidence),
		// so the stored RenderedContext must be built from the same Result shape
		// (ID/Name, SourceSessionID, EventDate) — not just bare ID+Content.
		resultHits = append(resultHits, formalEvidenceResult(item.ItemID, item.Text, source.Evidence))
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

	// 026: the frozen AnswerInputTokens must equal the harness counter's count
	// over the exact answer input (System+User), because prepareFrozenFormalB1Answer
	// re-derives it with the same counter at answer time and treats any mismatch
	// as answer_preflight_or_runtime_failed. The compiler engine's own count
	// (compiledBundle.InputTokens, renderer-shaped) is not comparable, so the
	// formal record is pinned to the harness preflight instead.
	finalCount, err := preflightFormalAnswer(ctx, protocol, opt.formalCounter, answerInput)
	if err != nil {
		return evalFormalBundleRecord{}, 0, evidencecompiler.TokenCount{}, err
	}
	// 026: EvidenceTokens must be on the same counter scale as AnswerInputTokens
	// (both harness preflight) or the citation gate
	// (EvidenceTokens <= AnswerInputTokens) fails on every question. The
	// compiler engine's EvidenceTokens is renderer-shaped and not comparable.
	// Evidence tokens = full answer input minus the static question/system prompt.
	staticInput := answerInput
	staticInput.User = buildAnswerContextPrompt(qa.Question, nil, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
	staticCount, err := preflightFormalAnswer(ctx, protocol, opt.formalCounter, staticInput)
	if err != nil {
		return evalFormalBundleRecord{}, 0, evidencecompiler.TokenCount{}, err
	}
	evidenceTokens := finalCount.InputTokens - staticCount.InputTokens
	if evidenceTokens < 0 {
		evidenceTokens = 0
	}

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
		EvidenceTokens:     evidenceTokens,
		AnswerInputTokens:  finalCount.InputTokens,
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		WithinCap:          finalCount.InputTokens > 0 && finalCount.InputTokens <= protocol.Budget.AnswerInputTokenCap,
		SourceValid:        sourceValid,
		AnswerPromptDigest: evalTextDigest(answerInput.System),
	}

	return bundle, finalCount.InputTokens, finalCount, nil
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
	allSources := formalCompileSourceList(expanded)
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
		Query:              qa.Question,
		Candidates:         candidates,
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		MaxCandidates:      len(candidates),
		MaxSources:         len(srcIDs),
		Planner:            opt.planner, // nil → deterministic extractive fallback (023 FR-019)
		Counter:            opt.formalCounter,
		Resolver:           resolver,
		Renderer:           renderer,
	})
}

// formalCompileSourceList flattens the expanded anchors into the flat source
// list shared by every compiler arm (extractive/planner via
// compileFormalSources, exact-token via compileExactTokenArm). One list means
// every arm scores the same candidate set — the byte-replay contract of T114.
func formalCompileSourceList(expanded []formalExpandedAnchor) []formalExpandedSource {
	var allSources []formalExpandedSource
	for _, anchor := range expanded {
		allSources = append(allSources, anchor.Sources...)
	}
	return allSources
}
