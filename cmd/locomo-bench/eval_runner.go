package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/memory/prompt"
	"github.com/wallfacers/engram/provider"
)

// evalFormalAnswerRun is the one answerer invocation that belongs to one
// independent repetition.  Keeping the preflight count beside the runtime
// usage makes a tokenizer/template drift visible in the per-question artifact
// rather than silently accepting post-hoc usage as the budget measurement.
type evalFormalAnswerRun struct {
	RunIndex      int            `json:"run_index"`
	Answer        string         `json:"answer"`
	AnswerDigest  string         `json:"answer_digest"`
	JudgeCorrect  bool           `json:"judge_correct"`
	AnswerCalls   int            `json:"answer_calls"`
	JudgeCalls    int            `json:"judge_calls"`
	InputTokens   int            `json:"input_tokens"`
	OutputTokens  int            `json:"output_tokens"`
	LatencyMS     int64          `json:"latency_ms"`
	Cost          float64        `json:"cost"`
	CounterSource string         `json:"counter_source,omitempty"`
	Usage         provider.Usage `json:"-"`
}

// preflightFormalAnswer renders the exact input which will be passed to the
// answer provider, asks the frozen counter to count that complete input, and
// rejects the invocation before it reaches the model on every hard-budget
// failure.  It is intentionally separate from the legacy answer routine:
// legacy is allowed to record post-call usage, whereas a formal 022 arm is
// not allowed to use it as its admission check.
func preflightFormalAnswer(ctx context.Context, protocol evalProtocol, counter evidencecompiler.TokenCounter, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if counter == nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("formal 022 evaluation requires a token counter")
	}
	if strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.System) == "" || strings.TrimSpace(input.User) == "" {
		return evidencecompiler.TokenCount{}, fmt.Errorf("formal 022 evaluation requires a complete answer input")
	}
	count, err := counter.CountInput(ctx, input)
	if err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("formal answer preflight: %w", err)
	}
	if err := validateEvalTokenCount(count, protocol.Budget.AnswerInputTokenCap, protocol.Budget.CounterFingerprint); err != nil {
		return evidencecompiler.TokenCount{}, fmt.Errorf("formal answer preflight: %w", err)
	}
	return count, nil
}

// callFormalAnswer performs exactly one answer-provider call after successful
// preflight.  A mismatch between the counter and provider-reported input usage
// invalidates the repetition; it is never papered over by the observed usage.
func callFormalAnswer(ctx context.Context, protocol evalProtocol, counter evidencecompiler.TokenCounter, input evidencecompiler.AnswerInput, answerCall usageModelCaller) (string, provider.Usage, evidencecompiler.TokenCount, error) {
	count, err := preflightFormalAnswer(ctx, protocol, counter, input)
	if err != nil {
		return "", provider.Usage{}, evidencecompiler.TokenCount{}, err
	}
	answer, usage, err := callPreflightedFormalAnswer(ctx, input, count, answerCall)
	return answer, usage, count, err
}

func callPreflightedFormalAnswer(ctx context.Context, input evidencecompiler.AnswerInput, count evidencecompiler.TokenCount, answerCall usageModelCaller) (string, provider.Usage, error) {
	if answerCall == nil {
		return "", provider.Usage{}, fmt.Errorf("formal 022 evaluation requires an answer caller")
	}
	answer, usage, err := answerCall(ctx, input.System, input.User)
	if err != nil {
		return "", usage, fmt.Errorf("formal answer call: %w", err)
	}
	if usage.InputTokens != count.InputTokens {
		return "", usage, fmt.Errorf("formal answer runtime input-token drift: preflight=%d runtime=%d", count.InputTokens, usage.InputTokens)
	}
	return answer, usage, nil
}

// prepareFrozenEvalOptions returns the only option set a formal 022 run may
// pass to the legacy harness. The formal protocol has one retrieval and one
// answer path per question: no IRIS expansion, no hosted reranker, and no
// answer-dependent IDK retry. Keep this guard at the runner boundary so a
// future call site cannot accidentally make the frozen protocol adaptive.
func prepareFrozenEvalOptions(protocol evalProtocol, requested options) (options, error) {
	if err := validateEvalProtocol(protocol, evalRunFormal); err != nil {
		return options{}, fmt.Errorf("invalid formal evaluation protocol: %w", err)
	}
	if requested.iris {
		return options{}, fmt.Errorf("formal 022 evaluation refuses --iris")
	}
	if requested.rerank {
		return options{}, fmt.Errorf("formal 022 evaluation refuses --rerank")
	}

	requested.iris = false
	requested.irisDepth = 0
	requested.rerank = false
	requested.noIDKRetry = true
	return requested, nil
}

// prepareFormalEvalRun pins an already-frozen manifest into the immutable run
// directory before any model, extraction, or retrieval work begins. A resume
// must present the byte-equivalent protocol fingerprint; a changed answerer,
// cap, dataset, or candidate recipe therefore cannot silently reuse artifacts.
func prepareFormalEvalRun(manifestPath, runDir string, requested options) (evalProtocol, options, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("formal evaluation requires --eval-protocol")
	}
	if strings.TrimSpace(runDir) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("formal evaluation requires --run-dir")
	}
	protocol, err := readEvalProtocolFile(manifestPath)
	if err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("read --eval-protocol: %w", err)
	}
	prepared, err := prepareFrozenEvalOptions(protocol, requested)
	if err != nil {
		return evalProtocol{}, options{}, err
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("create formal run dir: %w", err)
	}
	pinnedPath := filepath.Join(runDir, evalProtocolArtifactFile)
	if _, err := os.Stat(pinnedPath); err == nil {
		if err := checkEvalProtocolResume(runDir, protocol, evalRunFormal); err != nil {
			return evalProtocol{}, options{}, err
		}
	} else if os.IsNotExist(err) {
		if err := writeJSON(pinnedPath, protocol); err != nil {
			return evalProtocol{}, options{}, fmt.Errorf("pin formal protocol: %w", err)
		}
	} else {
		return evalProtocol{}, options{}, fmt.Errorf("stat pinned protocol: %w", err)
	}
	return protocol, prepared, nil
}

// prepareB0ContinuityEvalRun pins a continuity manifest without turning on
// any formal B1 machinery. In particular it leaves legacy IDK retry enabled
// and never opens the Candidate/Trace/Bundle replay journals.
func prepareB0ContinuityEvalRun(manifestPath, runDir string, requested options) (evalProtocol, options, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("B0 continuity evaluation requires --eval-b0-protocol")
	}
	if strings.TrimSpace(runDir) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("B0 continuity evaluation requires --run-dir")
	}
	protocol, err := readEvalProtocolFileMode(manifestPath, evalRunB0Continuity)
	if err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("read --eval-b0-protocol: %w", err)
	}
	if err := validateEvalProtocol(protocol, evalRunB0Continuity); err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("invalid B0 continuity protocol: %w", err)
	}
	if requested.noIDKRetry {
		return evalProtocol{}, options{}, fmt.Errorf("B0 continuity requires legacy IDK retry")
	}
	arms, err := armsFor(requested.retrieval)
	if err != nil {
		return evalProtocol{}, options{}, err
	}
	if err := validateB0ContinuityRunnerOptions(protocol, requested, arms); err != nil {
		return evalProtocol{}, options{}, err
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("create B0 run dir: %w", err)
	}
	pinnedPath := filepath.Join(runDir, evalProtocolArtifactFile)
	if _, err := os.Stat(pinnedPath); err == nil {
		if err := checkEvalProtocolResume(runDir, protocol, evalRunB0Continuity); err != nil {
			return evalProtocol{}, options{}, err
		}
	} else if os.IsNotExist(err) {
		if err := writeJSON(pinnedPath, protocol); err != nil {
			return evalProtocol{}, options{}, fmt.Errorf("pin B0 continuity protocol: %w", err)
		}
	} else {
		return evalProtocol{}, options{}, fmt.Errorf("stat pinned B0 protocol: %w", err)
	}
	return protocol, requested, nil
}

func validateB0ContinuityRunnerOptions(protocol evalProtocol, opt options, arms []string) error {
	if len(arms) != 1 || arms[0] != protocol.Retrieval.Recipe {
		return fmt.Errorf("B0 continuity requires exactly the frozen retrieval arm %q, got %v", protocol.Retrieval.Recipe, arms)
	}
	if err := validateFormalLegacyRecipe(arms[0]); err != nil {
		return fmt.Errorf("B0 continuity retrieval: %w", err)
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return fmt.Errorf("B0 continuity: %w", err)
	}
	if opt.noIDKRetry {
		return fmt.Errorf("B0 continuity requires legacy IDK retry")
	}
	if opt.fixedGoldOracle || strings.TrimSpace(opt.evalProtocolPath) != "" ||
		strings.TrimSpace(opt.evalFreezeProtocol) != "" {
		return fmt.Errorf("B0 continuity cannot use B1 or fixed-gold modes")
	}
	if opt.repeats != protocol.Aggregation.AnswerRepetitions ||
		opt.topK != protocol.Retrieval.CandidateLimit ||
		opt.maxTokens != protocol.Budget.MaxOutputTokens {
		return fmt.Errorf("B0 repetitions, candidate limit, or output cap differ from the manifest")
	}
	if len(opt.catTopK) != 0 || len(opt.catQuota) != 0 ||
		strings.TrimSpace(opt.catTopKSpec) != "" || strings.TrimSpace(opt.catQuotaSpec) != "" {
		return fmt.Errorf("B0 continuity refuses category-specific candidate budgets")
	}
	ingestionDigest := evalJSONDigest(evalFreezeIngestion{
		Chunks: opt.chunks, ImageCaptions: opt.imageCaptions, OpinionPass: opt.opinionPass,
	})
	if protocol.Store.SchemaVersion != 7 ||
		protocol.Store.IngestionRecipe != "ledger_lossless_chunks_v2" ||
		protocol.Store.IngestionConfigDigest != ingestionDigest ||
		evalJSONDigest(protocol.Store.ProjectionBuilderVersions) != evalJSONDigest(map[string]string{
			"atomic_fact": "entry_store_explicit_v1",
		}) {
		return fmt.Errorf("B0 store or lossless ingestion differs from the manifest")
	}
	candidateRulesDigest := evalJSONDigest(evalFreezeCandidateRules{
		TopK: opt.topK, ChunkQuota: opt.chunkQuota, Chunks: opt.chunks, Retrieval: arms[0],
	})
	if protocol.Retrieval.CandidateRulesDigest != candidateRulesDigest ||
		protocol.Retrieval.EmbeddingFingerprint != evalEmbeddingFingerprint() {
		return fmt.Errorf("B0 retrieval or embedding fingerprint differs from the manifest")
	}
	if protocol.Models.Answerer.PromptDigest != formalAnswerPromptDigest(opt) ||
		protocol.Models.Judge.PromptDigest != evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode())) {
		return fmt.Errorf("B0 answer or judge prompt differs from the manifest")
	}
	return nil
}

// validateFormalRunnerOptions refuses the legacy adaptive switches which are
// not represented by a B1 candidate artifact.  A protocol's retrieval recipe
// describes one arm, so the formal path deliberately requires one backend
// instead of silently materializing incomparable fts and hybrid journals.
func validateFormalRunnerOptions(protocol evalProtocol, opt options, arms []string) error {
	if len(arms) != 1 {
		return fmt.Errorf("formal 022 evaluation requires exactly one retrieval arm, got %v", arms)
	}
	if arms[0] != protocol.Retrieval.Recipe {
		return fmt.Errorf("formal retrieval arm %q differs from protocol recipe %q", arms[0], protocol.Retrieval.Recipe)
	}
	if err := validateFormalLegacyRecipe(protocol.Retrieval.Recipe); err != nil {
		return err
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return err
	}
	if strings.TrimSpace(opt.catTopKSpec) != "" || strings.TrimSpace(opt.catQuotaSpec) != "" ||
		len(opt.catTopK) != 0 || len(opt.catQuota) != 0 {
		return fmt.Errorf("formal 022 evaluation refuses category-specific candidate budgets")
	}
	if opt.repeats != protocol.Aggregation.AnswerRepetitions {
		return fmt.Errorf("formal answer repetitions %d differ from protocol %d", opt.repeats, protocol.Aggregation.AnswerRepetitions)
	}
	if opt.topK != protocol.Retrieval.CandidateLimit {
		return fmt.Errorf("formal --top-k %d differs from protocol candidate limit %d", opt.topK, protocol.Retrieval.CandidateLimit)
	}
	if opt.maxTokens != protocol.Budget.MaxOutputTokens {
		return fmt.Errorf("formal --max-tokens %d differs from protocol output cap %d", opt.maxTokens, protocol.Budget.MaxOutputTokens)
	}
	ingestionDigest := evalJSONDigest(evalFreezeIngestion{
		Chunks: opt.chunks, ImageCaptions: opt.imageCaptions, OpinionPass: opt.opinionPass,
	})
	if protocol.Store.SchemaVersion != 7 ||
		protocol.Store.IngestionRecipe != "ledger_lossless_chunks_v2" ||
		protocol.Store.IngestionConfigDigest != ingestionDigest ||
		evalJSONDigest(protocol.Store.ProjectionBuilderVersions) != evalJSONDigest(map[string]string{
			"atomic_fact": "entry_store_explicit_v1",
		}) {
		return fmt.Errorf("formal store or ingestion options differ from frozen protocol")
	}
	candidateRulesDigest := evalJSONDigest(evalFreezeCandidateRules{
		TopK:       opt.topK,
		ChunkQuota: opt.chunkQuota,
		Chunks:     opt.chunks,
		Retrieval:  arms[0],
	})
	if protocol.Retrieval.CandidateRulesDigest != candidateRulesDigest {
		return fmt.Errorf("formal candidate rules differ from frozen protocol")
	}
	// The fixed-gold oracle retains the control's frozen fingerprint for
	// provenance but has no embedder or retrieval dependency at runtime.
	if !opt.fixedGoldOracle && protocol.Retrieval.EmbeddingFingerprint != evalEmbeddingFingerprint() {
		return fmt.Errorf("formal embedding fingerprint differs from frozen protocol")
	}
	if protocol.Models.Answerer.PromptDigest != formalAnswerPromptDigest(opt) ||
		protocol.Models.Judge.PromptDigest != evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode())) {
		return fmt.Errorf("formal answer or judge prompt differs from frozen protocol")
	}
	return nil
}

// validateFormalLegacyRecipe keeps B1's registered legacy control from
// smuggling an unfrozen mechanism through parseArm's "+suffix" syntax. The
// recipe is deliberately narrower than the general harness grammar.
func validateFormalLegacyRecipe(recipe string) error {
	spec, err := parseArm(recipe)
	if err != nil {
		return fmt.Errorf("formal B1 retrieval recipe: %w", err)
	}
	if spec.overrides || recipe != spec.backend {
		return fmt.Errorf("formal B1 legacy control requires a plain fts or hybrid recipe, got %q", recipe)
	}
	return nil
}

// validateFormalLegacyMechanismOptions rejects retrieval/store selectors which
// are not represented in the B1 control contract. Answer and judge prompt
// variants remain allowed because their exact digests are frozen separately.
func validateFormalLegacyMechanismOptions(opt options) error {
	if opt.multiQuery || opt.filterPool > 0 || opt.assoc || opt.clusterSweep ||
		opt.temporalScore || opt.temporalHardFilter || opt.conflictResolution ||
		opt.iris || opt.rerank || opt.pcic || opt.oracle ||
		opt.abstainHard || opt.abstainSoft ||
		aliasShadowEnabled(opt) || doc2queryEnabled(opt) || opt.doc2queryBuild ||
		opt.pcicAnnotate || opt.recallDiagnostic || opt.coverageOnly ||
		opt.temporalDiagnostic || opt.attributionTrace || opt.abstainProbe ||
		opt.estimate {
		return fmt.Errorf("formal B1 legacy control refuses unfrozen retrieval, store, selector, shadow, build, or diagnostic modes")
	}
	return nil
}

// admitFormalQuestion limits in-flight retrieve→pack→answer pipelines. It is
// intentionally above the token counter: without it, every question can queue
// its next exact count ahead of already-packed questions, delaying answers
// despite bounded tokenizer concurrency. The gate is runtime-only and does not
// change candidates, rendered inputs, or any frozen protocol field.
func admitFormalQuestion(ctx context.Context, gate chan struct{}) (func(), error) {
	if gate == nil {
		return func() {}, nil
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("formal question admission: %w", ctx.Err())
	}
}

// materializeFormalB1Question is the sole formal B1 path allowed to retrieve,
// resolve lineage, or pack evidence. Its output is persisted before the first
// answer call and then byte-replayed by every repetition.
func materializeFormalB1Question(ctx context.Context, protocol evalProtocol, opt options, retriever *memory.Retriever, projections *memory.ProjectionStore, qa locomoQA, chunkTurns map[string][]string, turnEvidence map[string]string) formalFrozenQuestion {
	frozen := formalFrozenQuestion{}
	if retriever == nil {
		frozen.InvalidReasons = []string{"retriever_unavailable"}
		return frozen
	}
	hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, protocol.Retrieval.CandidateLimit, opt.chunkQuota, nil)
	if err != nil {
		frozen.InvalidReasons = []string{"retrieval_failed"}
		return frozen
	}
	expanded, sourceErr := expandFormalEvidence(ctx, projections, opt.formalEvidence, hits)
	if sourceErr != nil {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "source_lineage_unavailable")
		// Retain the navigation artifact for diagnosis, but never let its
		// projection text become answer-facing evidence.
		sourceByCandidate, _ := formalCandidateSources(ctx, projections, hits)
		frozen.Candidate = buildFormalCandidateArtifact(protocol, qa, hits, chunkTurns, sourceByCandidate, turnEvidence)
	} else {
		frozen.Candidate = buildExpandedFormalCandidateArtifact(protocol, qa, expanded, turnEvidence, 1)
		// When a non-chunk_900 representation is selected, re-render the
		// anchors through the representation renderer so the candidate
		// artifact records the enriched structure (windows or episodes).
		// The bundle items remain derived from the canonical source
		// expansion to preserve formal auditability.
		if opt.representationArm != ReprChunk900 {
			renderer, rendererErr := formalRepresentationRendererWithEpisodes(opt.representationArm, projections, opt.formalEvidence, opt.formalEpisodes)
			if rendererErr == nil {
				anchors := buildFormalRankedAnchors(expanded)
				enriched, renderErr := renderer.Render(ctx, anchors)
				if renderErr == nil && len(enriched) > 0 {
					frozen.Candidate.RenderedCandidates = enriched
					frozen.Candidate.CandidateSetDigest = renderedCandidateSetDigest(enriched)
					frozen.Candidate.Mode = evalCandidateModeAnchorRendering
				}
			}
		}
	}
	if err := validateEvalCandidateArtifact(protocol, frozen.Candidate); err != nil {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "candidate_invalid")
	}

	system := withCurrentDateRule(answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt), qa.QuestionDate)
	input := evidencecompiler.AnswerInput{
		Model:  protocol.Models.Answerer.ID,
		System: system,
		User:   buildAnswerContextPrompt(qa.Question, nil, qa.QuestionDate, qa.Category, opt.temporalDateScaffold),
	}
	var packedSources []formalExpandedSource
	var preflight evidencecompiler.TokenCount
	var evidenceTokens int
	var packErr error
	if sourceErr == nil {
		if opt.compilerArm != "" {
			// Compile arm: use the evidencecompiler engine instead of the
			// legacy ranked-prefix packer. The compiler selects items under
			// the real token counter and produces an auditable trace.
			compiledBundle, compiledTrace, compileErr := compileFormalSources(ctx, protocol, opt, qa, expanded)
			if compileErr != nil {
				packErr = fmt.Errorf("compile: %w", compileErr)
			} else {
				compiledItems := compiledBundle.Items
				items := compileBundleItems(protocol, compiledItems)
				rendered := compileRenderedCandidates(compiledItems)
				// Update candidate artifact with compiler-rendered sources.
				if len(rendered) > 0 {
					frozen.Candidate.RenderedCandidates = rendered
					frozen.Candidate.CandidateSetDigest = renderedCandidateSetDigest(rendered)
				}
				frozen.Trace = buildCompileTrace(protocol, qa.QuestionID, frozen.Candidate, compiledTrace, items)
				bundle, inputTokens, count, bundleErr := buildCompileBundle(ctx, protocol, opt, qa, frozen.Candidate, frozen.Trace, compiledBundle, compiledItems, items)
				if bundleErr != nil {
					packErr = fmt.Errorf("compile bundle: %w", bundleErr)
				} else {
					frozen.Bundle = bundle
					preflight = count
					evidenceTokens = compiledBundle.EvidenceTokens
					input = evidencecompiler.AnswerInput{
						Model:  protocol.Models.Answerer.ID,
						System: system,
						User:   bundle.RenderedContext,
					}
					_ = inputTokens
				}
			}
		} else {
			packedSources, input, preflight, evidenceTokens, packErr = packExpandedFormalInput(
				ctx, protocol, opt.formalCounter, system, qa, expanded, opt.temporalDateScaffold,
			)
		}
	}
	if packErr != nil {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "answer_input_budget_impossible")
		input = evidencecompiler.AnswerInput{Model: protocol.Models.Answerer.ID, System: system, User: buildAnswerContextPrompt(qa.Question, nil, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)}
	}
	if input.Model == "" {
		input.Model = protocol.Models.Answerer.ID
	}
	if opt.compilerArm == "" {
		items := formalBundleItems(packedSources)
		frozen.Trace = buildFormalTraceForItems(protocol, qa.QuestionID, frozen.Candidate, items)
		frozen.Bundle = buildExpandedFormalBundle(protocol, qa.QuestionID, frozen.Candidate, frozen.Trace, packedSources, input)
		frozen.Bundle.EvidenceTokens = evidenceTokens
		frozen.Bundle.AnswerInputTokens = preflight.InputTokens
	}
	frozen.Bundle.WithinCap = sourceErr == nil && packErr == nil
	if sourceErr == nil && len(frozen.Bundle.SourceIDs) == 0 {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "no_evidence_fits_token_cap")
	}
	if sourceErr == nil && packErr == nil && len(frozen.Bundle.Items) > 0 {
		frozen = revalidateFrozenFormalSources(ctx, protocol, opt, qa, frozen)
	}
	frozen.InvalidReasons = stableStrings(frozen.InvalidReasons)
	if len(frozen.InvalidReasons) > 0 {
		frozen.Trace.Valid = false
		frozen.Bundle.Valid = false
	}
	return frozen
}

// prepareFrozenFormalB1Answer is the last no-call boundary in the formal
// answer path. It reconstructs the immutable provider input and performs every
// deterministic identity/token check before the call journal records STARTED.
// Any invalid result returned here is therefore safe to persist as a terminal
// pre-call failure without claiming that a provider was invoked.
func prepareFrozenFormalB1Answer(ctx context.Context, protocol evalProtocol, opt options, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, frozen formalFrozenQuestion, runIndex int) (evidencecompiler.AnswerInput, evidencecompiler.TokenCount, evalFormalQuestionRun) {
	artifact := evalFormalQuestionRun{
		Candidate:      frozen.Candidate,
		Trace:          frozen.Trace,
		Bundle:         frozen.Bundle,
		InvalidReasons: append([]string(nil), frozen.InvalidReasons...),
		Answer:         evalFormalAnswerRun{RunIndex: runIndex},
	}

	system := withCurrentDateRule(answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt), qa.QuestionDate)
	input := evidencecompiler.AnswerInput{
		Model:  protocol.Models.Answerer.ID,
		System: system,
		User:   frozen.Bundle.RenderedContext,
	}
	if len(artifact.InvalidReasons) > 0 {
		return input, evidencecompiler.TokenCount{}, artifact
	}
	if err := validateFormalFrozenPayload(protocol, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "source_span_or_citation_invalid")
		return input, evidencecompiler.TokenCount{}, artifact
	}
	if frozen.Candidate.QuestionID != qa.QuestionID ||
		frozen.Trace.QuestionID != qa.QuestionID ||
		frozen.Bundle.QuestionID != qa.QuestionID ||
		frozen.Trace.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.TraceDigest != frozen.Trace.TraceDigest ||
		frozen.Bundle.RenderedDigest != evalTextDigest(input.User) ||
		frozen.Bundle.AnswerPromptDigest != evalTextDigest(input.System) {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_input_drift")
		return input, evidencecompiler.TokenCount{}, artifact
	}

	count, err := preflightFormalAnswer(ctx, protocol, opt.formalCounter, input)
	artifact.Answer.InputTokens = count.InputTokens
	artifact.Answer.CounterSource = count.Fingerprint
	if err != nil || count.InputTokens != frozen.Bundle.AnswerInputTokens {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_preflight_or_runtime_failed")
		return input, count, artifact
	}
	if answerCall == nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_preflight_or_runtime_failed")
	}
	if judgeCall == nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "judge_failed")
	}
	artifact.InvalidReasons = stableStrings(artifact.InvalidReasons)
	return input, count, artifact
}

// callPreparedFrozenFormalB1Answer is entered only after STARTED is durable.
// It performs at most one answer call and, after a successful answer, at most
// one judge call. Call counters record attempts even when the provider returns
// an error, making terminal failures auditable without permitting a retry.
func callPreparedFrozenFormalB1Answer(ctx context.Context, opt options, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, input evidencecompiler.AnswerInput, count evidencecompiler.TokenCount, artifact evalFormalQuestionRun) (bool, string, provider.Usage, evalFormalQuestionRun) {
	if len(artifact.InvalidReasons) > 0 {
		return false, "", provider.Usage{}, artifact
	}
	started := time.Now()
	answer, usage, err := callPreflightedFormalAnswer(ctx, input, count, answerCall)
	artifact.Answer.AnswerCalls = 1
	artifact.Answer.Answer = answer
	artifact.Answer.AnswerDigest = evalTextDigest(answer)
	artifact.Answer.OutputTokens = usage.OutputTokens
	artifact.Answer.LatencyMS = time.Since(started).Milliseconds()
	artifact.Answer.Usage = usage
	if err != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_preflight_or_runtime_failed")
		return false, answer, usage, artifact
	}
	verdict, _, judgeErr := judgeCall(ctx, judgeSystemPromptFor(opt.judgeAlignmentMode()), buildJudgePrompt(qa.Question, goldFor(qa), answer))
	artifact.Answer.JudgeCalls = 1
	if judgeErr != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "judge_failed")
		return false, answer, usage, artifact
	}
	correct := parseJudgeVerdict(verdict)
	artifact.Answer.JudgeCorrect = correct
	return correct, answer, usage, artifact
}

// answerFrozenFormalB1Question is intentionally unable to access a retriever,
// projection store, source resolver, or packer. It is retained for focused
// unit tests and one-shot callers; production wraps the prepared call with the
// durable formal-call state machine.
func answerFrozenFormalB1Question(ctx context.Context, protocol evalProtocol, opt options, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, frozen formalFrozenQuestion, runIndex int) (bool, string, provider.Usage, evalFormalQuestionRun) {
	input, count, artifact := prepareFrozenFormalB1Answer(ctx, protocol, opt, answerCall, judgeCall, qa, frozen, runIndex)
	return callPreparedFrozenFormalB1Answer(ctx, opt, answerCall, judgeCall, qa, input, count, artifact)
}

// runFormalB1Question remains a narrow compatibility helper for unit tests and
// one-shot callers. Production repetitions go through formalQuestionReplay.
func runFormalB1Question(ctx context.Context, protocol evalProtocol, opt options, retriever *memory.Retriever, projections *memory.ProjectionStore, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, chunkTurns map[string][]string, turnEvidence map[string]string, runIndex int) (bool, string, provider.Usage, evalFormalQuestionRun) {
	frozen := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, qa, chunkTurns, turnEvidence)
	return answerFrozenFormalB1Question(ctx, protocol, opt, answerCall, judgeCall, qa, frozen, runIndex)
}

// packFormalLegacyInput preserves the legacy rank-order packer while making
// its admission decision on the exact complete answer input.  It never uses a
// character or per-item estimate: each candidate is rendered through the
// production prompt and counted before it enters the Bundle.  The first
// candidate that does not fit terminates the legacy prefix pack; later stages
// compare their own packers against the same frozen candidate array.
func packFormalLegacyInput(ctx context.Context, protocol evalProtocol, counter evidencecompiler.TokenCounter, system string, qa locomoQA, hits []memory.Result, scaffold bool) ([]memory.Result, evidencecompiler.AnswerInput, evidencecompiler.TokenCount, error) {
	if counter == nil {
		return nil, evidencecompiler.AnswerInput{}, evidencecompiler.TokenCount{}, fmt.Errorf("formal 022 evaluation requires a token counter")
	}
	render := func(selected []memory.Result) evidencecompiler.AnswerInput {
		return evidencecompiler.AnswerInput{
			Model: protocol.Models.Answerer.ID, System: system,
			User: buildAnswerContextPrompt(qa.Question, selected, qa.QuestionDate, qa.Category, scaffold),
		}
	}
	selected := make([]memory.Result, 0, len(hits))
	input := render(selected)
	count, fits, err := countFormalPackInput(ctx, protocol, counter, input)
	if err != nil {
		return nil, input, evidencecompiler.TokenCount{}, err
	}
	if !fits {
		return nil, input, count, fmt.Errorf("formal static answer prompt exceeds token cap %d", protocol.Budget.AnswerInputTokenCap)
	}
	for _, hit := range hits {
		trial := append(append([]memory.Result(nil), selected...), hit)
		trialInput := render(trial)
		trialCount, trialFits, err := countFormalPackInput(ctx, protocol, counter, trialInput)
		if err != nil {
			return nil, input, count, err
		}
		if !trialFits {
			break
		}
		selected = trial
		input = trialInput
		count = trialCount
	}
	return selected, input, count, nil
}

func countFormalPackInput(ctx context.Context, protocol evalProtocol, counter evidencecompiler.TokenCounter, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, bool, error) {
	count, err := counter.CountInput(ctx, input)
	if err != nil {
		return evidencecompiler.TokenCount{}, false, fmt.Errorf("formal answer pack preflight: %w", err)
	}
	if count.InputTokens < 1 || count.Fingerprint == "" || count.Fingerprint != protocol.Budget.CounterFingerprint {
		return evidencecompiler.TokenCount{}, false, fmt.Errorf("formal answer pack counter fingerprint drift")
	}
	return count, count.InputTokens <= protocol.Budget.AnswerInputTokenCap, nil
}

func buildFormalCandidateArtifact(protocol evalProtocol, qa locomoQA, hits []memory.Result, chunkTurns map[string][]string, sourceByCandidate map[string][]string, turnEvidence map[string]string) evalCandidateArtifact {
	anchors := make([]evalRankedAnchor, 0, len(hits))
	rendered := make([]evalRenderedCandidate, 0, len(hits))
	for index, hit := range hits {
		candidateID := formalCandidateID(hit)
		sourceIDs := formalCandidateSourceIDs(hit, sourceByCandidate)
		textDigest := evalTextDigest(hit.Content)
		anchor := evalRankedAnchor{CandidateID: candidateID, Rank: index + 1, Score: hit.Score, TextDigest: textDigest, SourceIDs: sourceIDs}
		anchors = append(anchors, anchor)
		kind := "atomic_fact"
		if _, ok := chunkTurns[hit.Name]; ok {
			kind = "chunk"
		}
		rendered = append(rendered, evalRenderedCandidate{
			CandidateID: candidateID, Kind: kind, Rank: index + 1, Score: hit.Score, Text: hit.Content, TextDigest: textDigest,
			SourceIDs: sourceIDs, ExpandedFrom: []string{candidateID}, ExpansionCount: 0,
		})
	}
	resolved, unresolved, _ := resolveDatasetSourceIDs(qa.Evidence, turnEvidence)
	artifact := evalCandidateArtifact{
		Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: qa.QuestionID,
		QueryDigest: evalTextDigest(qa.Question), Mode: evalCandidateModeAnchorRendering, RetrievalCalls: 1,
		Anchors: anchors, RenderedCandidates: rendered,
		Gold: evalGoldResolution{DatasetSourceIDs: stableStrings(qa.Evidence), ResolvedEvidenceIDs: resolved, UnresolvedIDs: unresolved},
	}
	artifact.AnchorDigest = rankedAnchorDigest(artifact.Anchors)
	artifact.CandidateSetDigest = renderedCandidateSetDigest(artifact.RenderedCandidates)
	artifact.Gold.AnchorSourceCoverage = sourceCoverage(artifact.Gold.ResolvedEvidenceIDs, collectAnchorSources(artifact.Anchors))
	artifact.Gold.RenderedSourceCoverage = sourceCoverage(artifact.Gold.ResolvedEvidenceIDs, collectRenderedSources(artifact.RenderedCandidates))
	artifact.CoverageStratum = coverageStratumFor(protocol.CoverageStrata.Boundaries, artifact.Gold.RenderedSourceCoverage)
	return artifact
}

func buildFormalTrace(protocol evalProtocol, questionID string, candidate evalCandidateArtifact) evalFormalTraceRecord {
	items := make([]evalFormalBundleItem, 0, len(candidate.RenderedCandidates))
	for _, rendered := range candidate.RenderedCandidates {
		if len(rendered.SourceIDs) == 0 {
			continue
		}
		items = append(items, evalFormalBundleItem{
			ItemID:       formalBundleItemID(rendered.CandidateID),
			Kind:         "KEEP",
			Text:         rendered.Text,
			CandidateIDs: []string{rendered.CandidateID},
			Sources: []evalFormalSourceSpan{{
				EvidenceID: rendered.SourceIDs[0],
				StartChar:  0,
				EndChar:    len([]rune(rendered.Text)),
				SpanDigest: evalTextDigest(rendered.Text),
			}},
		})
	}
	return buildFormalTraceForItems(protocol, questionID, candidate, items)
}

// formalRepresentationRenderer selects and creates the appropriate representation
// renderer for the formal B1 pipeline. Returns nil renderer + non-nil error
// when the required dependencies are unavailable; callers should fall back to
// the chunk_900 path silently.
func formalRepresentationRenderer(arm RepresentationKind, projections *memory.ProjectionStore, reader formalEvidenceReader) (RepresentationRenderer, error) {
	fullReader, ok := reader.(evidenceReader)
	if !ok {
		return nil, fmt.Errorf("formal evidence reader does not satisfy evidenceReader")
	}
	switch arm {
	case ReprChunk900:
		return NewChunk900Renderer(projections, fullReader), nil
	case ReprRawTurnWindow:
		return NewRawTurnWindowRenderer(projections, fullReader, 0), nil
	case ReprSemanticEpisode:
		// The episode store is wired through options; the caller should
		// ensure it is non-nil before calling this function.
		return nil, fmt.Errorf("semantic_episode renderer requires an episode store; use formalEpisodes")
	default:
		return nil, fmt.Errorf("unsupported representation %q", arm)
	}
}

// formalRepresentationRendererWithEpisodes extends formalRepresentationRenderer
// for renderers that require an EpisodeStore (semantic_episode).
func formalRepresentationRendererWithEpisodes(arm RepresentationKind, projections *memory.ProjectionStore, reader formalEvidenceReader, episodes *memory.EpisodeStore) (RepresentationRenderer, error) {
	if arm != ReprSemanticEpisode {
		return formalRepresentationRenderer(arm, projections, reader)
	}
	fullReader, ok := reader.(evidenceReader)
	if !ok {
		return nil, fmt.Errorf("formal evidence reader does not satisfy evidenceReader")
	}
	return NewSemanticEpisodeRenderer(projections, fullReader, episodes), nil
}

// buildFormalRankedAnchors converts expanded formal anchors into ranked anchors
// suitable for representation renderers.
func buildFormalRankedAnchors(expanded []formalExpandedAnchor) []evalRankedAnchor {
	anchors := make([]evalRankedAnchor, 0, len(expanded))
	for index, anchor := range expanded {
		anchors = append(anchors, evalRankedAnchor{
			CandidateID: anchor.CandidateID,
			Rank:        index + 1,
			Score:       anchor.Hit.Score,
			TextDigest:  evalTextDigest(anchor.Hit.Content),
			SourceIDs:   append([]string(nil), anchor.SourceIDs...),
		})
	}
	return anchors
}

func buildFormalTraceForItems(protocol evalProtocol, questionID string, candidate evalCandidateArtifact, items []evalFormalBundleItem) evalFormalTraceRecord {
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
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalTraceArtifactKind, Valid: true},
		Attempt:            1,
		CandidateSetDigest: candidate.CandidateSetDigest,
		AppliedActions:     actions,
		SourceValidation: evalFormalSourceValidation{
			AllowedIDsDigest: evalJSONDigest(allowed),
			ResolvedCount:    len(stableStrings(resolved)),
		},
	}
	trace.TraceDigest = formalTraceDigest(trace)
	return trace
}

func buildFormalBundle(protocol evalProtocol, questionID string, candidate evalCandidateArtifact, trace evalFormalTraceRecord, hits []memory.Result, sourceByCandidate map[string][]string, input evidencecompiler.AnswerInput) evalFormalBundleRecord {
	var sourceValues []string
	var items []evalFormalBundleItem
	for _, hit := range hits {
		candidateID := formalCandidateID(hit)
		for index, sourceID := range formalCandidateSourceIDs(hit, sourceByCandidate) {
			sourceValues = append(sourceValues, sourceID)
			items = append(items, evalFormalBundleItem{
				ItemID:       formalBundleItemID(candidateID) + ":" + fmt.Sprint(index),
				Kind:         "KEEP",
				Text:         hit.Content,
				CandidateIDs: []string{candidateID},
				Sources: []evalFormalSourceSpan{{
					EvidenceID: sourceID,
					StartChar:  0,
					EndChar:    len([]rune(hit.Content)),
					SpanDigest: evalTextDigest(hit.Content),
				}},
			})
		}
	}
	sources := stableStrings(sourceValues)
	sourceValid := len(sources) > 0 && len(items) > 0
	for _, sourceID := range sources {
		if strings.HasPrefix(sourceID, "legacy-entry:") {
			sourceValid = false
			break
		}
	}
	return evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalBundleArtifactKind, Valid: sourceValid},
		CandidateSetDigest: candidate.CandidateSetDigest, TraceDigest: trace.TraceDigest, Items: items, SourceIDs: sources,
		RenderedContext: input.User, RenderedDigest: evalTextDigest(input.User), TokenCap: protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint, SourceValid: sourceValid,
		AnswerPromptDigest: evalTextDigest(input.System),
	}
}

func formalTraceDigest(trace evalFormalTraceRecord) string {
	trace.TraceDigest = ""
	return evalJSONDigest(trace)
}

func formalCandidateID(hit memory.Result) string {
	if hit.ProjectionID != "" {
		return "projection:" + hit.ProjectionID
	}
	if hit.ID != "" {
		return "entry:" + hit.ID
	}
	return "legacy-candidate:" + strings.TrimPrefix(evalTextDigest(hit.Name+"\n"+hit.SourceSessionID+"\n"+hit.Content), "sha256:")
}

func formalCandidateSourceIDs(hit memory.Result, sourceByCandidate map[string][]string) []string {
	return stableStrings(sourceByCandidate[hit.Name])
}

// formalCandidateSources resolves all search-hit lineage in bounded engine
// batches. A formal candidate never invents a source from its name, session, or
// chunk bookkeeping: a missing projection ID or direct Evidence ref is a hard
// validity failure before an answer call.
func formalCandidateSources(ctx context.Context, projections *memory.ProjectionStore, hits []memory.Result) (map[string][]string, error) {
	if projections == nil {
		return nil, fmt.Errorf("formal candidate projections unavailable")
	}
	projectionIDs := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.ProjectionID == "" || hit.ProjectionKind != memory.ProjectionAtomicFact {
			return nil, fmt.Errorf("candidate %q has no atomic-fact projection", hit.Name)
		}
		projectionIDs = append(projectionIDs, hit.ProjectionID)
	}
	refsByProjection, err := projections.SourcesByProjectionIDs(ctx, projectionIDs)
	if err != nil {
		return nil, err
	}
	byCandidate := make(map[string][]string, len(hits))
	for _, hit := range hits {
		refs := refsByProjection[hit.ProjectionID]
		if len(refs) == 0 {
			return nil, fmt.Errorf("candidate %q has no direct Evidence source", hit.Name)
		}
		ids := make([]string, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.EvidenceID)
		}
		byCandidate[hit.Name] = stableStrings(ids)
	}
	return byCandidate, nil
}

func stableStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	stable := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			stable = append(stable, value)
		}
	}
	sort.Strings(stable)
	return stable
}

func formalRepeatRunDirs(runDir string, repeats int) []string {
	if repeats == 1 {
		return []string{runDir}
	}
	dirs := make([]string, 0, repeats)
	for repeat := 1; repeat <= repeats; repeat++ {
		dirs = append(dirs, filepath.Join(runDir, fmt.Sprintf("run-%d", repeat)))
	}
	return dirs
}

// verifyFormalDataset makes the protocol's denominator an executable check.
// The raw dataset digest is intentionally separate from the ordered question
// ID digest: either can change while the other remains coincidentally stable.
func verifyFormalDataset(protocol evalProtocol, dataPath, format string, convs []conversation) error {
	if protocol.Benchmark.Name == "locomo" && format != "locomo" {
		return fmt.Errorf("formal protocol benchmark locomo requires --dataset-format locomo")
	}
	if protocol.Benchmark.Name == "longmemeval_s" && format != "longmemeval" {
		return fmt.Errorf("formal protocol benchmark longmemeval_s requires --dataset-format longmemeval")
	}
	raw, err := os.ReadFile(dataPath) //nolint:gosec // operator-selected dataset is protocol-pinned
	if err != nil {
		return fmt.Errorf("read formal dataset: %w", err)
	}
	if evalTextDigest(string(raw)) != protocol.Benchmark.DatasetDigest {
		return fmt.Errorf("formal dataset digest differs from protocol")
	}
	questionIDs := formalQuestionIDs(format, convs)
	if len(questionIDs) != protocol.Benchmark.QuestionCount {
		return fmt.Errorf("formal question count %d differs from protocol %d", len(questionIDs), protocol.Benchmark.QuestionCount)
	}
	if evalJSONDigest(questionIDs) != protocol.Benchmark.QuestionIDsDigest {
		return fmt.Errorf("formal question ID digest differs from protocol")
	}
	return nil
}

func formalQuestionIDs(format string, convs []conversation) []string {
	questionIDs := make([]string, 0)
	for _, conv := range convs {
		for _, qa := range conv.QA {
			if format == "locomo" && qa.Category == adversarialCategory {
				continue
			}
			questionIDs = append(questionIDs, qa.QuestionID)
		}
	}
	return questionIDs
}

// verifyFormalGitProvenance checks the worktree, not merely a hand-written
// manifest field.  A code or config edit between freeze and resume therefore
// fails before any model call instead of being mislabeled as the frozen run.
func verifyFormalGitProvenance(protocol evalProtocol) error {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output() //nolint:gosec // fixed git subcommand
	if err != nil {
		return fmt.Errorf("read formal git commit: %w", err)
	}
	if strings.TrimSpace(string(head)) != protocol.Git.Commit {
		return fmt.Errorf("formal git commit differs from protocol")
	}
	status, err := exec.Command("git", "status", "--porcelain").Output() //nolint:gosec // fixed git subcommand
	if err != nil {
		return fmt.Errorf("read formal git status: %w", err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return fmt.Errorf("formal evaluation refuses dirty worktree")
	}
	return nil
}

// freezeFormalB1Protocol is the no-model half of T020.  It derives the two
// dataset fingerprints and all current harness knobs from the actual command
// line/environment, then refuses to write a manifest from a dirty worktree.
// It deliberately writes only the manifest: scores and per-question artifacts
// belong to a later immutable run directory.
func freezeFormalB1Protocol(opt options, convs []conversation) error {
	if strings.TrimSpace(opt.evalProtocolPath) != "" {
		return fmt.Errorf("--eval-freeze-protocol cannot be combined with --eval-protocol")
	}
	if opt.evalBudgetProfile != "low" && opt.evalBudgetProfile != "high" {
		return fmt.Errorf("--eval-budget-profile must be low or high")
	}
	if opt.answerInputTokenCap < 1 || !isDigest(opt.counterFingerprint) {
		return fmt.Errorf("--answer-input-cap and --counter-fingerprint are required for formal protocol freeze")
	}
	if opt.maxConvs != 0 || opt.maxQuestions != 0 || opt.onlyCategory != 0 || opt.onlyEnumeration || opt.adversarial != 0 {
		return fmt.Errorf("formal protocol freeze refuses dataset/question sampling")
	}
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	if len(arms) != 1 {
		return fmt.Errorf("formal B1 freeze requires exactly one retrieval arm")
	}
	if err := validateFormalLegacyRecipe(arms[0]); err != nil {
		return err
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return err
	}
	if len(opt.catTopK) != 0 || len(opt.catQuota) != 0 ||
		strings.TrimSpace(opt.catTopKSpec) != "" || strings.TrimSpace(opt.catQuotaSpec) != "" {
		return fmt.Errorf("formal B1 freeze requires one non-adaptive retrieval arm with IRIS and rerank disabled")
	}
	raw, err := os.ReadFile(opt.dataPath) //nolint:gosec // operator-selected benchmark is frozen by digest
	if err != nil {
		return fmt.Errorf("read benchmark for protocol freeze: %w", err)
	}
	questionIDs := formalQuestionIDs(opt.datasetFormat, convs)
	benchmarkName, split := "", ""
	switch opt.datasetFormat {
	case "locomo":
		benchmarkName, split = "locomo", "category_1_4"
	case "longmemeval":
		benchmarkName, split = "longmemeval_s", "cleaned_full_500"
	default:
		return fmt.Errorf("unsupported formal benchmark format %q", opt.datasetFormat)
	}
	git, err := currentCleanGitProvenance()
	if err != nil {
		return err
	}
	answerModel := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	extractModel := envOr("EXTRACT_MODEL", answerModel)
	answerProvider := envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider)
	judge := resolveJudgeConfig(os.Getenv)
	answerPromptDigest := formalAnswerPromptDigest(opt)
	protocol := evalProtocol{
		Schema: evalProtocolSchema, ProtocolID: fmt.Sprintf("%s-b1-%s", benchmarkName, opt.evalBudgetProfile), CreatedAt: time.Now().UTC(), Git: git,
		Benchmark: evalBenchmarkProvenance{Name: benchmarkName, DatasetDigest: evalTextDigest(string(raw)), Split: split, QuestionCount: len(questionIDs), QuestionIDsDigest: evalJSONDigest(questionIDs)},
		Store:     evalStoreProvenance{SchemaVersion: 7, IngestionRecipe: "ledger_lossless_chunks_v2", IngestionConfigDigest: evalJSONDigest(evalFreezeIngestion{Chunks: opt.chunks, ImageCaptions: opt.imageCaptions, OpinionPass: opt.opinionPass}), ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"}},
		Models: evalModelProvenance{
			Extractor: evalModelFingerprint{ID: extractModel, Revision: envOr("EXTRACT_MODEL_REVISION", extractModel), Provider: answerProvider, PromptDigest: evalTextDigest(prompt.MemoryExtractionSystemPrompt)},
			Answerer:  evalModelFingerprint{ID: answerModel, Revision: envOr("LOCOMO_MODEL_REVISION", answerModel), Provider: answerProvider, PromptDigest: answerPromptDigest},
			Judge:     evalModelFingerprint{ID: judge.Model, Revision: envOr("JUDGE_MODEL_REVISION", judge.Model), Provider: judge.Provider, PromptDigest: evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode()))},
			Planner:   evalPlannerFingerprint{Enabled: false},
		},
		Retrieval:      evalRetrievalProvenance{Recipe: arms[0], EmbeddingFingerprint: evalEmbeddingFingerprint(), Reranker: "disabled", CandidateLimit: opt.topK, CandidateRulesDigest: evalJSONDigest(evalFreezeCandidateRules{TopK: opt.topK, ChunkQuota: opt.chunkQuota, Chunks: opt.chunks, Retrieval: arms[0]})},
		Budget:         evalBudgetProtocol{Profile: opt.evalBudgetProfile, AnswerInputTokenCap: opt.answerInputTokenCap, MaxOutputTokens: opt.maxTokens, CandidateLimit: opt.topK, RetrievalCallLimit: 1, AnswerCallLimit: 1, CounterFingerprint: opt.counterFingerprint},
		Aggregation:    evalAggregationProtocol{AnswerRepetitions: 3, Rule: "majority_correctness", JudgeRepetitions: 1, SeedPolicy: "independent-recorded"},
		JudgeAudit:     evalJudgeAuditProtocol{AllDiscordant: true, ConcordantSamplingDigest: evalTextDigest("022.v1:concordant-stratified-plan:freeze-before-treatment"), Reviewers: 2, BlindedToArm: true, AdjudicationRule: "independent_then_adjudicate"},
		CoverageStrata: evalCoverageStrataProtocol{Boundaries: []float64{0, 0.5, 0.9, 1}, SelectionDigest: evalTextDigest("022.v1:coverage-strata:0,0.5,0.9,1")},
		Experiment:     evalExperimentProtocol{Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all", MechanismFlags: map[string]bool{"idk_retry": false, "iris": false, "rerank": false}},
	}
	if !opt.chunks {
		return fmt.Errorf("formal B1 freeze requires --chunks for lossless source identity")
	}
	if _, err := freezeEvalProtocolFile(opt.evalFreezeProtocol, protocol, evalRunFormal); err != nil {
		return err
	}
	fmt.Printf("eval-freeze: protocol=%s output=%s questions=%d cap=%d\n", protocol.ProtocolID, opt.evalFreezeProtocol, len(questionIDs), opt.answerInputTokenCap)
	return nil
}

// freezeB0ContinuityProtocol records the current lossless legacy product path
// separately from B1. It deliberately has no exact counter/cap contract:
// runtime usage and adaptive IDK retry calls are recorded in B0 receipts.
func freezeB0ContinuityProtocol(opt options, convs []conversation) error {
	if strings.TrimSpace(opt.evalB0ProtocolPath) != "" ||
		strings.TrimSpace(opt.evalProtocolPath) != "" ||
		strings.TrimSpace(opt.evalFreezeProtocol) != "" {
		return fmt.Errorf("--eval-freeze-b0-protocol cannot be combined with another eval protocol mode")
	}
	if opt.evalBudgetProfile != "" || opt.answerInputTokenCap != 0 || opt.counterFingerprint != "" {
		return fmt.Errorf("B0 continuity freeze does not accept B1 profile, exact input cap, or counter fingerprint")
	}
	if opt.noIDKRetry {
		return fmt.Errorf("B0 continuity freeze requires legacy IDK retry")
	}
	if opt.maxConvs != 0 || opt.maxQuestions != 0 || opt.onlyCategory != 0 || opt.onlyEnumeration || opt.adversarial != 0 {
		return fmt.Errorf("B0 continuity freeze refuses dataset/question sampling")
	}
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	if len(arms) != 1 {
		return fmt.Errorf("B0 continuity freeze requires exactly one retrieval arm")
	}
	if err := validateFormalLegacyRecipe(arms[0]); err != nil {
		return err
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return fmt.Errorf("B0 continuity: %w", err)
	}
	if len(opt.catTopK) != 0 || len(opt.catQuota) != 0 ||
		strings.TrimSpace(opt.catTopKSpec) != "" || strings.TrimSpace(opt.catQuotaSpec) != "" {
		return fmt.Errorf("B0 continuity freeze refuses category-specific candidate budgets")
	}
	if !opt.chunks {
		return fmt.Errorf("B0 continuity freeze requires --chunks for lossless ingestion")
	}
	raw, err := os.ReadFile(opt.dataPath) //nolint:gosec // operator-selected benchmark is frozen by digest
	if err != nil {
		return fmt.Errorf("read benchmark for B0 continuity freeze: %w", err)
	}
	questionIDs := formalQuestionIDs(opt.datasetFormat, convs)
	benchmarkName, split := "", ""
	switch opt.datasetFormat {
	case "locomo":
		benchmarkName, split = "locomo", "category_1_4"
	case "longmemeval":
		benchmarkName, split = "longmemeval_s", "cleaned_full_500"
	default:
		return fmt.Errorf("unsupported B0 benchmark format %q", opt.datasetFormat)
	}
	git, err := currentCleanGitProvenance()
	if err != nil {
		return err
	}
	answerModel := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	extractModel := envOr("EXTRACT_MODEL", answerModel)
	answerProvider := envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider)
	judge := resolveJudgeConfig(os.Getenv)
	protocol := evalProtocol{
		Schema: evalProtocolSchema, ProtocolID: fmt.Sprintf("%s-b0-continuity", benchmarkName), CreatedAt: time.Now().UTC(), Git: git,
		Benchmark: evalBenchmarkProvenance{Name: benchmarkName, DatasetDigest: evalTextDigest(string(raw)), Split: split, QuestionCount: len(questionIDs), QuestionIDsDigest: evalJSONDigest(questionIDs)},
		Store:     evalStoreProvenance{SchemaVersion: 7, IngestionRecipe: "ledger_lossless_chunks_v2", IngestionConfigDigest: evalJSONDigest(evalFreezeIngestion{Chunks: opt.chunks, ImageCaptions: opt.imageCaptions, OpinionPass: opt.opinionPass}), ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"}},
		Models: evalModelProvenance{
			Extractor: evalModelFingerprint{ID: extractModel, Revision: envOr("EXTRACT_MODEL_REVISION", extractModel), Provider: answerProvider, PromptDigest: evalTextDigest(prompt.MemoryExtractionSystemPrompt)},
			Answerer:  evalModelFingerprint{ID: answerModel, Revision: envOr("LOCOMO_MODEL_REVISION", answerModel), Provider: answerProvider, PromptDigest: formalAnswerPromptDigest(opt)},
			Judge:     evalModelFingerprint{ID: judge.Model, Revision: envOr("JUDGE_MODEL_REVISION", judge.Model), Provider: judge.Provider, PromptDigest: evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode()))},
			Planner:   evalPlannerFingerprint{Enabled: false},
		},
		Retrieval: evalRetrievalProvenance{
			Recipe: arms[0], EmbeddingFingerprint: evalEmbeddingFingerprint(), Reranker: "disabled",
			CandidateLimit: opt.topK, CandidateRulesDigest: evalJSONDigest(evalFreezeCandidateRules{TopK: opt.topK, ChunkQuota: opt.chunkQuota, Chunks: opt.chunks, Retrieval: arms[0]}),
		},
		Budget: evalBudgetProtocol{
			Profile: "continuity", MaxOutputTokens: opt.maxTokens, CandidateLimit: opt.topK,
			RetrievalCallLimit: 3, AnswerCallLimit: 3,
			CounterFingerprint: evalTextDigest("legacy-runtime-usage-only:no-preflight"),
		},
		Aggregation:    evalAggregationProtocol{AnswerRepetitions: 3, Rule: "majority_correctness", JudgeRepetitions: 1, SeedPolicy: "independent-recorded"},
		JudgeAudit:     evalJudgeAuditProtocol{AllDiscordant: true, ConcordantSamplingDigest: evalTextDigest("022.v1:b0-continuity-audit"), Reviewers: 2, BlindedToArm: true, AdjudicationRule: "independent_then_adjudicate"},
		CoverageStrata: evalCoverageStrataProtocol{Boundaries: []float64{0, 0.5, 0.9, 1}, SelectionDigest: evalTextDigest("022.v1:coverage-strata:0,0.5,0.9,1")},
		Experiment: evalExperimentProtocol{
			Stage: "b0", Arm: "legacy_product_continuity", PrimaryCohort: "all",
			MechanismFlags: map[string]bool{"idk_retry": true, "iris": false, "rerank": false},
		},
	}
	if _, err := freezeEvalProtocolFile(opt.evalFreezeB0Protocol, protocol, evalRunB0Continuity); err != nil {
		return err
	}
	fmt.Printf("eval-freeze-b0: protocol=%s output=%s questions=%d\n", protocol.ProtocolID, opt.evalFreezeB0Protocol, len(questionIDs))
	return nil
}

func formalAnswerPromptDigest(opt options) string {
	return evalJSONDigest([]string{
		answerPromptForRegime(1, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		answerPromptForRegime(2, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		answerPromptForRegime(3, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		answerPromptForRegime(4, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		currentDateRule,
		fmt.Sprintf("temporal_date_scaffold=%t", opt.temporalDateScaffold),
	})
}

type evalFreezeIngestion struct {
	Chunks        bool `json:"chunks"`
	ImageCaptions bool `json:"image_captions"`
	OpinionPass   bool `json:"opinion_pass"`
}

type evalFreezeCandidateRules struct {
	TopK       int    `json:"top_k"`
	ChunkQuota int    `json:"chunk_quota"`
	Chunks     bool   `json:"chunks"`
	Retrieval  string `json:"retrieval"`
}

func evalEmbeddingFingerprint() string {
	if fingerprint := strings.TrimSpace(os.Getenv("EMBED_FINGERPRINT")); isDigest(fingerprint) {
		return fingerprint
	}
	return evalTextDigest("embedding-model=" + envOr("EMBED_MODEL", "qwen3-embedding:0.6b"))
}

func currentCleanGitProvenance() (evalGitProvenance, error) {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output() //nolint:gosec // fixed git subcommand
	if err != nil {
		return evalGitProvenance{}, fmt.Errorf("read git commit for protocol freeze: %w", err)
	}
	status, err := exec.Command("git", "status", "--porcelain").Output() //nolint:gosec // fixed git subcommand
	if err != nil {
		return evalGitProvenance{}, fmt.Errorf("read git status for protocol freeze: %w", err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return evalGitProvenance{}, fmt.Errorf("formal protocol freeze refuses dirty worktree")
	}
	return evalGitProvenance{Commit: strings.TrimSpace(string(head)), Dirty: false}, nil
}

func freezeEvalProtocolFile(path string, protocol evalProtocol, mode evalRunMode) (evalProtocol, error) {
	if err := validateEvalProtocol(protocol, mode); err != nil {
		return evalProtocol{}, err
	}
	hash, err := evalProtocolFingerprint(protocol)
	if err != nil {
		return evalProtocol{}, err
	}
	protocol.ProtocolHash = hash
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return evalProtocol{}, fmt.Errorf("create protocol directory: %w", err)
	}
	if err := writeJSON(path, protocol); err != nil {
		return evalProtocol{}, fmt.Errorf("write protocol: %w", err)
	}
	return protocol, nil
}

// runFormalTokenCalibrationCLI proves that the preflight counter and the
// actual answer runtime see identical complete chat inputs before a protocol
// is frozen. It needs no benchmark data and writes no credentials.
func runFormalTokenCalibrationCLI(opt options) error {
	if strings.TrimSpace(opt.runDir) == "" || strings.TrimSpace(opt.tokenCounterBaseURL) == "" || !isDigest(opt.counterFingerprint) {
		return fmt.Errorf("--token-counter-calibrate requires --run-dir, --token-counter-base-url, and --counter-fingerprint")
	}
	apiKey := os.Getenv("LOCOMO_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LOCOMO_API_KEY is required for token calibration")
	}
	model := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	baseURL := envOr("LOCOMO_BASE_URL", "")
	if strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("LOCOMO_BASE_URL is required for token calibration")
	}
	prov, err := buildBenchProvider(envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider), apiKey, baseURL, 32, "LOCOMO_PROVIDER")
	if err != nil {
		return err
	}
	counter, err := newVLLMTokenCounter(vllmTokenCounterConfig{BaseURL: opt.tokenCounterBaseURL, APIKey: apiKey, Fingerprint: opt.counterFingerprint})
	if err != nil {
		return err
	}
	report, err := calibrateEvalTokenCounterAgainstRuntime(context.Background(), counter, newUsageModelCaller(prov, model, 32, 0, "calibration", nil), formalCalibrationFixtures(model), opt.counterFingerprint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(opt.runDir, "counter-calibration.json"), report); err != nil {
		return err
	}
	fmt.Printf("token-counter-calibrate: fixtures=%d max_delta=0 fingerprint=%s\n", len(report.Fixtures), report.CounterFingerprint)
	return nil
}

func formalCalibrationFixtures(model string) []evalTokenCalibrationFixture {
	return []evalTokenCalibrationFixture{
		{Name: "cjk-ja-en", Input: evidencecompiler.AnswerInput{Model: model, System: "Use only the evidence.", User: "中文、日本語、English: 张三 met 花子."}, WantInputTokens: 1},
		{Name: "emoji-codepoints", Input: evidencecompiler.AnswerInput{Model: model, System: "Be concise.", User: "coffee ☕️ then family 👨‍👩‍👧‍👦."}, WantInputTokens: 1},
		{Name: "numbers-time", Input: evidencecompiler.AnswerInput{Model: model, System: "UTC.", User: "1234567890 -0.125 2026-07-30T12:34:56.789Z."}, WantInputTokens: 1},
		{Name: "empty-evidence", Input: evidencecompiler.AnswerInput{Model: model, System: "SYSTEM\n<rules/>", User: "EVIDENCE:\n(none)\nQUESTION: why?"}, WantInputTokens: 1},
		{Name: "multi-source-boundary", Input: evidencecompiler.AnswerInput{Model: model, System: "SYSTEM\n<rules>no guessing</rules>", User: "<evidence id=\"a\">one</evidence>\n<evidence id=\"b\">two</evidence>\nQUESTION: combine?"}, WantInputTokens: 1},
		{Name: "cap-minus-one", Input: evidencecompiler.AnswerInput{Model: model, System: "cap test", User: strings.Repeat("x", 255)}, WantInputTokens: 1},
		{Name: "cap-exact", Input: evidencecompiler.AnswerInput{Model: model, System: "cap test", User: strings.Repeat("x", 256)}, WantInputTokens: 1},
		{Name: "cap-plus-one", Input: evidencecompiler.AnswerInput{Model: model, System: "cap test", User: strings.Repeat("x", 257)}, WantInputTokens: 1},
	}
}
