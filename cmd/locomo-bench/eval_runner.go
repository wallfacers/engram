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
	if answerCall == nil {
		return "", provider.Usage{}, evidencecompiler.TokenCount{}, fmt.Errorf("formal 022 evaluation requires an answer caller")
	}
	count, err := preflightFormalAnswer(ctx, protocol, counter, input)
	if err != nil {
		return "", provider.Usage{}, evidencecompiler.TokenCount{}, err
	}
	answer, usage, err := answerCall(ctx, input.System, input.User)
	if err != nil {
		return "", usage, count, fmt.Errorf("formal answer call: %w", err)
	}
	if usage.InputTokens != count.InputTokens {
		return "", usage, count, fmt.Errorf("formal answer runtime input-token drift: preflight=%d runtime=%d", count.InputTokens, usage.InputTokens)
	}
	return answer, usage, count, nil
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
	if opt.multiQuery || opt.filterPool > 0 || opt.clusterSweep || opt.iris || opt.rerank {
		return fmt.Errorf("formal 022 evaluation refuses adaptive retrieval, filter, IRIS, or rerank switches")
	}
	if opt.repeats != protocol.Aggregation.AnswerRepetitions {
		return fmt.Errorf("formal answer repetitions %d differ from protocol %d", opt.repeats, protocol.Aggregation.AnswerRepetitions)
	}
	if opt.topK != protocol.Retrieval.CandidateLimit {
		return fmt.Errorf("formal --top-k %d differs from protocol candidate limit %d", opt.topK, protocol.Retrieval.CandidateLimit)
	}
	return nil
}

// runFormalB1Question is intentionally a narrow legacy-packer bridge.  It
// retrieves exactly once, freezes the returned candidates and the legacy
// rendered context, preflights the full answer input, then makes one answer
// call and one judge call.  Later compiler stages replace only the packing
// portion; they keep this answer-facing counter and journal boundary.
func runFormalB1Question(ctx context.Context, protocol evalProtocol, opt options, retriever *memory.Retriever, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, chunkTurns map[string][]string, runIndex int) (bool, string, provider.Usage, evalFormalQuestionRun) {
	artifact := evalFormalQuestionRun{}
	if retriever == nil {
		artifact.InvalidReasons = []string{"retriever_unavailable"}
		return false, "", provider.Usage{}, artifact
	}
	hits, err := retriever.Search(ctx, qa.Question, protocol.Retrieval.CandidateLimit)
	if err != nil {
		artifact.InvalidReasons = []string{"retrieval_failed"}
		return false, "", provider.Usage{}, artifact
	}
	artifact.Candidate = buildFormalCandidateArtifact(protocol, qa, hits, chunkTurns)
	if err := validateEvalCandidateArtifact(protocol, artifact.Candidate); err != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "candidate_invalid")
	}

	system := withCurrentDateRule(answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt), qa.QuestionDate)
	packedHits, input, preflight, err := packFormalLegacyInput(ctx, protocol, opt.formalCounter, system, qa, hits, opt.temporalDateScaffold)
	if err != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_input_budget_impossible")
		input = evidencecompiler.AnswerInput{Model: opt.answerModel, System: system, User: buildAnswerContextPrompt(qa.Question, nil, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)}
	}
	if input.Model == "" {
		input.Model = opt.answerModel
	}
	artifact.Trace = buildFormalTrace(protocol, qa.QuestionID, artifact.Candidate)
	artifact.Bundle = buildFormalBundle(protocol, qa.QuestionID, artifact.Candidate, artifact.Trace, packedHits, chunkTurns, input)
	artifact.Bundle.AnswerInputTokens = preflight.InputTokens
	artifact.Bundle.WithinCap = err == nil
	if !artifact.Bundle.SourceValid {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "source_lineage_unavailable")
	}
	if len(artifact.Bundle.SourceIDs) == 0 {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "no_evidence_fits_token_cap")
	}
	if len(artifact.InvalidReasons) > 0 {
		artifact.Trace.Valid = false
		artifact.Bundle.Valid = false
		return false, "", provider.Usage{}, artifact
	}

	started := time.Now()
	answer, usage, count, err := callFormalAnswer(ctx, protocol, opt.formalCounter, input, answerCall)
	artifact.Answer = evalFormalAnswerRun{
		RunIndex:      runIndex,
		Answer:        answer,
		AnswerDigest:  evalTextDigest(answer),
		AnswerCalls:   0,
		InputTokens:   count.InputTokens,
		OutputTokens:  usage.OutputTokens,
		LatencyMS:     time.Since(started).Milliseconds(),
		CounterSource: count.Fingerprint,
		Usage:         usage,
	}
	if err != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "answer_preflight_or_runtime_failed")
		artifact.Trace.Valid = false
		artifact.Bundle.Valid = false
		return false, answer, usage, artifact
	}
	artifact.Answer.AnswerCalls = 1
	artifact.Bundle.AnswerInputTokens = count.InputTokens
	artifact.Bundle.WithinCap = true
	verdict, _, judgeErr := judgeCall(ctx, judgeSystemPromptFor(opt.judgeAlignmentMode()), buildJudgePrompt(qa.Question, goldFor(qa), answer))
	if judgeErr != nil {
		artifact.InvalidReasons = append(artifact.InvalidReasons, "judge_failed")
		artifact.Trace.Valid = false
		artifact.Bundle.Valid = false
		return false, answer, usage, artifact
	}
	correct := parseJudgeVerdict(verdict)
	artifact.Answer.JudgeCorrect = correct
	return correct, answer, usage, artifact
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

func buildFormalCandidateArtifact(protocol evalProtocol, qa locomoQA, hits []memory.Result, chunkTurns map[string][]string) evalCandidateArtifact {
	anchors := make([]evalRankedAnchor, 0, len(hits))
	rendered := make([]evalRenderedCandidate, 0, len(hits))
	for index, hit := range hits {
		candidateID := formalCandidateID(hit)
		sourceIDs := formalCandidateSourceIDs(hit, chunkTurns)
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
	resolved, unresolved, _ := resolveDatasetSourceIDs(qa.Evidence, datasetEvidenceIdentityMap(qa.Evidence))
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
	trace := evalFormalTraceRecord{
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalTraceArtifactKind, Valid: true},
		Attempt:            1, CandidateSetDigest: candidate.CandidateSetDigest, AppliedActions: []string{"KEEP"},
	}
	trace.TraceDigest = formalTraceDigest(trace)
	return trace
}

func buildFormalBundle(protocol evalProtocol, questionID string, candidate evalCandidateArtifact, trace evalFormalTraceRecord, hits []memory.Result, chunkTurns map[string][]string, input evidencecompiler.AnswerInput) evalFormalBundleRecord {
	var sourceValues []string
	for _, hit := range hits {
		sourceValues = append(sourceValues, formalCandidateSourceIDs(hit, chunkTurns)...)
	}
	sources := stableStrings(sourceValues)
	sourceValid := true
	for _, sourceID := range sources {
		if strings.HasPrefix(sourceID, "legacy-entry:") {
			sourceValid = false
			break
		}
	}
	return evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalBundleArtifactKind, Valid: sourceValid},
		CandidateSetDigest: candidate.CandidateSetDigest, TraceDigest: trace.TraceDigest, SourceIDs: sources,
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
	return "legacy-candidate:" + strings.TrimPrefix(evalTextDigest(hit.Name+"\n"+hit.SourceSessionID+"\n"+hit.Content), "sha256:")
}

func formalCandidateSourceIDs(hit memory.Result, chunkTurns map[string][]string) []string {
	if ids := stableStrings(chunkTurns[hit.Name]); len(ids) > 0 {
		return ids
	}
	return []string{"legacy-entry:" + strings.TrimPrefix(evalTextDigest(hit.Name+"\n"+hit.Content), "sha256:")}
}

func datasetEvidenceIdentityMap(ids []string) map[string]string {
	identity := make(map[string]string, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			identity[id] = id
		}
	}
	return identity
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
	questionIDs := make([]string, 0)
	for _, conv := range convs {
		for _, qa := range conv.QA {
			questionIDs = append(questionIDs, qa.QuestionID)
		}
	}
	if len(questionIDs) != protocol.Benchmark.QuestionCount {
		return fmt.Errorf("formal question count %d differs from protocol %d", len(questionIDs), protocol.Benchmark.QuestionCount)
	}
	if evalJSONDigest(questionIDs) != protocol.Benchmark.QuestionIDsDigest {
		return fmt.Errorf("formal question ID digest differs from protocol")
	}
	return nil
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
