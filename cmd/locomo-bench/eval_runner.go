package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if requested.rerank {
		return options{}, fmt.Errorf("formal 022 evaluation refuses --rerank")
	}

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
		Chunks: opt.chunks, ImageCaptions: opt.imageCaptions,
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
	if err := validateFormalMechanismBinding(protocol, opt); err != nil {
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
		Chunks: opt.chunks, ImageCaptions: opt.imageCaptions,
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
	if opt.rerank || opt.pcic || opt.oracle ||
		opt.pcicAnnotate || opt.recallDiagnostic || opt.coverageOnly ||
		opt.temporalDiagnostic || opt.attributionTrace || opt.abstainProbe ||
		opt.estimate {
		return fmt.Errorf("formal B1 legacy control refuses unfrozen retrieval, store, selector, shadow, build, or diagnostic modes")
	}
	return nil
}

// densityMechanismKeys are the additive write-time/query-time mechanism flags
// (contracts/mechanism-bindings.md). They are additive to a formal B1 control
// manifest rather than a distinct treatment stage: the four-arm 024 ablation
// freezes four independent B1 manifests that differ only in these keys, so the
// protocol hash attributes each arm (mechanism-bindings rule 4). 025's
// episode_cluster follows the same shape: it is a build-time mechanism additive
// to the B1 control (the semantic_episode renderer is requested at runtime via
// --representation, but the frozen manifest difference is the episode_cluster
// key alone). They are deliberately NOT part of formalTreatmentForOptions — a
// density flag never replaces the legacy B1 control, it extends it.
var densityMechanismKeys = []string{"write_dedup", "neighbor_extend", "episode_cluster", "compiler", "temporal_resolution", "counter_refine"}

// densityMechanismFlagsForOptions returns the additive mechanism flags derived
// from the CLI. Absent flags are omitted so a run with neither mechanism set
// stays byte-identical to the legacy B1 control manifest (mechanism-bindings
// rule: new keys default to false / absent = false). 024 density keys and the
// 026 compiler arm are all b1-stage additive mechanisms: the compiler arm
// swaps the answer-bundle packer inside the same b1 retrieval→pack→answer
// path, exactly as episode_cluster swaps the representation.
func densityMechanismFlagsForOptions(opt options) map[string]bool {
	flags := make(map[string]bool)
	if opt.writeDedup {
		flags["write_dedup"] = true
	}
	if opt.neighborExtend {
		flags["neighbor_extend"] = true
	}
	if opt.episodeCluster {
		flags["episode_cluster"] = true
	}
	if opt.compilerArm != "" {
		flags["compiler"] = true
	}
	if opt.temporalResolution {
		flags["temporal_resolution"] = true
	}
	if opt.counterRefine {
		flags["counter_refine"] = true
	}
	return flags
}

// mergeMechanismFlags merges additive flags over a base set, returning a new
// map. The base is never mutated.
func mergeMechanismFlags(base, additive map[string]bool) map[string]bool {
	if len(additive) == 0 {
		return base
	}
	merged := make(map[string]bool, len(base)+len(additive))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range additive {
		merged[k] = v
	}
	return merged
}

// formalTreatmentFreeze names the single treatment mechanism a formal
// manifest is frozen for. T114 keeps treatment freezes single-mechanism:
// mixing mechanisms in one manifest would make the artifact's effect
// unattributable.
type formalTreatmentFreeze struct {
	Stage          string
	Arm            string
	MechanismFlags map[string]bool
}

// formalTreatmentForOptions derives the frozen treatment mechanism from the
// CLI flags, enforcing that at most one mechanism is requested and that
// dependent flags (gap-refetch requires event-projection) are consistent.
// An empty result (no treatment flags) is the legacy B1 control.
//
// compiler-arm is NOT a treatment here: like episode_cluster / write_dedup /
// neighbor_extend it is a b1-stage additive mechanism (densityMechanismFlagsFor
// Options), so it is excluded from the treatment freeze — a b1+compiler
// manifest freezes as arm=legacy_count_packer with mechanism_flags{compiler:true}
// bound to a control protocol hash, exactly mirroring 024/025.
func formalTreatmentForOptions(opt options) (formalTreatmentFreeze, error) {
	active := make([]string, 0, 3)
	if opt.representationArm != "" && opt.representationArm != ReprChunk900 {
		active = append(active, "representation")
	}
	switch {
	case opt.eventProjection != "" && opt.gapRefetch:
		active = append(active, "gap")
	case opt.eventProjection != "":
		active = append(active, "event")
	case opt.gapRefetch:
		active = append(active, "gap")
	}
	if len(active) > 1 {
		return formalTreatmentFreeze{}, fmt.Errorf("formal treatment freeze allows exactly one mechanism, got %v", active)
	}
	if len(active) == 0 {
		return formalTreatmentFreeze{}, nil
	}
	switch active[0] {
	case "representation":
		flags := map[string]bool{"representation": true}
		if opt.episodeCluster {
			// 025 cross-session semantic episode clustering: the semantic_episode
			// arm additionally rebuilds episodes across sessions before rendering.
			// Kept as its own flag so the treatment freeze binds the mechanism.
			flags["episode_cluster"] = true
		}
		return formalTreatmentFreeze{Stage: "representation_navigation", Arm: string(opt.representationArm), MechanismFlags: flags}, nil
	case "event":
		if !validEventProjection(opt.eventProjection) {
			return formalTreatmentFreeze{}, fmt.Errorf("--event-projection must be E0 | E1 | E2 | E3, got %q", opt.eventProjection)
		}
		if opt.gapRefetch {
			return formalTreatmentFreeze{Stage: "gap", Arm: "structured_gap_refetch", MechanismFlags: map[string]bool{"event_projection": true, "gap_refetch": true}}, nil
		}
		return formalTreatmentFreeze{Stage: "event", Arm: "event_" + strings.ToLower(opt.eventProjection), MechanismFlags: map[string]bool{"event_projection": true}}, nil
	case "gap":
		if !validEventProjection(opt.eventProjection) {
			return formalTreatmentFreeze{}, fmt.Errorf("--gap-refetch requires --event-projection E0 | E1 | E2 | E3")
		}
		return formalTreatmentFreeze{Stage: "gap", Arm: "structured_gap_refetch", MechanismFlags: map[string]bool{"event_projection": true, "gap_refetch": true}}, nil
	}
	return formalTreatmentFreeze{}, nil
}

func validEventProjection(value string) bool {
	switch value {
	case "E0", "E1", "E2", "E3":
		return true
	}
	return false
}

// buildFormalExperiment derives the frozen experiment block for either the
// legacy B1 control (no treatment flags, empty control hash) or a single
// treatment mechanism bound to its B1 control protocol hash.
func buildFormalExperiment(opt options, controlHash string) (evalExperimentProtocol, error) {
	treatment, err := formalTreatmentForOptions(opt)
	if err != nil {
		return evalExperimentProtocol{}, err
	}
	if treatment.Stage == "" {
		flags := map[string]bool{"idk_retry": false, "iris": false, "rerank": false}
		flags = mergeMechanismFlags(flags, densityMechanismFlagsForOptions(opt))
		return evalExperimentProtocol{
			Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
			MechanismFlags: flags,
		}, nil
	}
	if !isDigest(controlHash) {
		return evalExperimentProtocol{}, fmt.Errorf("treatment freeze requires a frozen B1 control protocol hash, got %q", controlHash)
	}
	return evalExperimentProtocol{
		Stage: treatment.Stage, Arm: treatment.Arm, PrimaryCohort: "all",
		MechanismFlags: treatment.MechanismFlags, ControlProtocolHash: controlHash,
	}, nil
}

// validateFormalMechanismBinding keeps the formal runner honest about what
// its manifest claims: a B1 control manifest must run without treatment
// flags, and a treatment manifest must run with exactly the mechanism it was
// frozen for, bound to a real control protocol hash. Any mismatch fails
// closed before a single model call.
func validateFormalMechanismBinding(protocol evalProtocol, opt options) error {
	exp := protocol.Experiment
	if exp.Stage == "b1" {
		if exp.Arm != "legacy_count_packer" || exp.ControlProtocolHash != "" {
			return fmt.Errorf("formal B1 control manifest must be arm=legacy_count_packer without control hash, got %q/%q", exp.Stage, exp.Arm)
		}
		if !isFormalControlMechanismFlags(exp.MechanismFlags) {
			return fmt.Errorf("formal b1/legacy_count_packer manifest contains non-control mechanism flags")
		}
		// 025: --representation semantic_episode alongside --episode-cluster is a
		// renderer selection, not a treatment — the mechanism difference is fully
		// expressed by the frozen episode_cluster key. It is therefore exempt from
		// the treatment-flag rejection so a b1+episode_cluster manifest can run
		// its semantic_episode arm. Any other treatment flag still fails closed.
		if formalTreatmentMechanismRequested(opt) &&
			!(opt.episodeCluster && opt.representationArm == ReprSemanticEpisode) {
			return fmt.Errorf("formal b1/legacy_count_packer run cannot bind --compiler-arm/--representation/--event-projection/--gap-refetch")
		}
		// 024 density mechanisms are additive to the B1 control: the manifest's
		// density keys must exactly match the requested CLI flags so a frozen
		// write_dedup/neighbor_extend arm cannot silently change mechanism.
		want := densityMechanismFlagsForOptions(opt)
		for _, key := range densityMechanismKeys {
			if exp.MechanismFlags[key] != want[key] {
				return fmt.Errorf("formal b1 manifest density mechanism %s=%v differs from requested %s=%v", key, exp.MechanismFlags[key], key, want[key])
			}
		}
		return nil
	}
	if !isDigest(exp.ControlProtocolHash) {
		return fmt.Errorf("treatment manifest %s/%s must bind a B1 control protocol hash", exp.Stage, exp.Arm)
	}
	requested, err := formalTreatmentForOptions(opt)
	if err != nil {
		return err
	}
	if requested.Stage == "" {
		return fmt.Errorf("treatment manifest %s/%s requires the matching treatment CLI flags", exp.Stage, exp.Arm)
	}
	if exp.Stage != requested.Stage || exp.Arm != requested.Arm || !reflect.DeepEqual(exp.MechanismFlags, requested.MechanismFlags) {
		return fmt.Errorf("treatment manifest %s/%s does not match requested mechanism %s/%s", exp.Stage, exp.Arm, requested.Stage, requested.Arm)
	}
	return nil
}

func isFormalLegacyControlMechanismFlags(flags map[string]bool) bool {
	if len(flags) != 3 {
		return false
	}
	for _, name := range []string{"idk_retry", "iris", "rerank"} {
		value, ok := flags[name]
		if !ok || value {
			return false
		}
	}
	return true
}

// isFormalControlMechanismFlags accepts the legacy 3-key B1 control, optionally
// extended with the 024 density additive keys (write_dedup / neighbor_extend,
// contracts/mechanism-bindings.md) and the 027 temporal_resolution additive key.
// Legacy keys must be present and false; density keys may be true or false; no
// unknown key is allowed. A pure legacy control (3 keys) satisfies this —
// backward compatible with frozen 022 assets.
func isFormalControlMechanismFlags(flags map[string]bool) bool {
	if len(flags) < 3 {
		return false
	}
	for _, name := range []string{"idk_retry", "iris", "rerank"} {
		value, ok := flags[name]
		if !ok || value {
			return false
		}
	}
	for name := range flags {
		switch name {
		case "idk_retry", "iris", "rerank", "write_dedup", "neighbor_extend", "episode_cluster", "compiler", "temporal_resolution", "counter_refine":
			continue
		default:
			return false
		}
	}
	return true
}

func formalTreatmentMechanismRequested(opt options) bool {
	return (opt.representationArm != "" && opt.representationArm != ReprChunk900) ||
		opt.eventProjection != "" || opt.gapRefetch
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
func materializeFormalB1Question(ctx context.Context, protocol evalProtocol, opt options, retriever *memory.Retriever, projections *memory.ProjectionStore, entries *memory.EntryStore, qa locomoQA, chunkTurns map[string][]string, turnEvidence map[string]string) formalFrozenQuestion {
	frozen := formalFrozenQuestion{}
	// 027: temporal resolution engages ONLY when the query carries temporal
	// semantics. Degraded queries (no time meaning, no explicit entity) fall
	// through to the legacy chunk-verbatim packer — byte-identical to the
	// control arm — so the mechanism never perturbs non-temporal questions
	// (spec: ResolutionDegraded = baseline behavior, zero extra tokens).
	temporalActive := opt.temporalResolution && classifyQueryMode(qa.Question) != ResolutionDegraded
	if retriever == nil {
		frozen.InvalidReasons = []string{"retriever_unavailable"}
		return frozen
	}
	// T114 candidate-replay: compiler arms must consume the byte-identical
	// retrieval product of the same protocol/question/query. The first
	// materialize retrieves and persists the replay; every later arm reads
	// it with zero retrieval calls. Identity or digest drift fails closed.
	var hits []memory.Result
	retrievalCalls := 0
	var retrieveErr error
	if (opt.compilerArm != "" || temporalActive) && strings.TrimSpace(opt.runDir) != "" {
		replay, replayErr := loadFormalCandidateReplay(opt.runDir, protocol.ProtocolHash, qa.QuestionID, qa.Question)
		if replayErr == nil {
			hits = replay.Hits
		} else if os.IsNotExist(replayErr) {
			hits, _, retrieveErr = retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, protocol.Retrieval.CandidateLimit, opt.chunkQuota, nil)
			if retrieveErr != nil {
				frozen.InvalidReasons = []string{"retrieval_failed"}
				return frozen
			}
			retrievalCalls = 1
			if replayWriteErr := writeFormalCandidateReplay(opt.runDir, protocol, qa.QuestionID, qa.Question, hits); replayWriteErr != nil {
				frozen.InvalidReasons = append(frozen.InvalidReasons, "candidate_replay_write_failed")
			}
		} else {
			frozen.InvalidReasons = []string{"candidate_replay_drift"}
			return frozen
		}
	} else {
		hits, _, retrieveErr = retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, protocol.Retrieval.CandidateLimit, opt.chunkQuota, nil)
		if retrieveErr != nil {
			frozen.InvalidReasons = []string{"retrieval_failed"}
			return frozen
		}
		retrievalCalls = 1
		// 024 hit-time neighbor extension (US2): after candidate freeze and
		// before answerer assembly, add bounded shared-evidence sibling facts to
		// the answer context (spec FR-006/FR-007). This runs only on the legacy
		// packer path — the compiler arm's candidate-replay must stay
		// byte-identical, so siblings never enter a replay product.
		if opt.neighborExtend {
			hits = extendCandidatesWithSiblings(ctx, projections, entries, hits)
		}
	}
	expanded, sourceErr := expandFormalEvidence(ctx, projections, opt.formalEvidence, hits)
	if sourceErr != nil {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "source_lineage_unavailable")
		// Retain the navigation artifact for diagnosis, but never let its
		// projection text become answer-facing evidence.
		sourceByCandidate, _ := formalCandidateSources(ctx, projections, hits)
		frozen.Candidate = buildFormalCandidateArtifact(protocol, qa, hits, chunkTurns, sourceByCandidate, turnEvidence, retrievalCalls)
	} else {
		// B1 control (chunk_900): pack the projection原文 instead of the
		// source-expanded raw messages. expandFormalEvidence resolves each hit
		// to its active Ledger spans, but for a verbatim chunk (or condensed
		// fact) the projection's own text is the higher-density packing that the
		// legacy B0 product path used. Folding whole-source anchors back to
		// hit.Content keeps every member message as a whole-source span, so
		// auditability holds while the bundle stops paying the expansion tax.
		// The compiler arm (026) and the temporal-resolution arm (027) are
		// excluded: their frozen protocol and validator operate on the per-source
		// expansion, and the fold would break the byte-identical candidate replay
		// across arms (and would collapse multi-version evidence into one
		// /verbatim candidate, defeating 027's version separation).
		if opt.representationArm == ReprChunk900 && opt.compilerArm == "" && !temporalActive {
			expanded = rebuildExpandedForChunkVerbatim(expanded)
		}
		frozen.Candidate = buildExpandedFormalCandidateArtifact(protocol, qa, expanded, turnEvidence, retrievalCalls)
		// When a non-chunk_900 representation is selected, re-render the
		// anchors through the representation renderer so the candidate
		// artifact records the enriched structure (windows or episodes).
		// The bundle items remain derived from the canonical source
		// expansion to preserve formal auditability.
		if opt.representationArm != ReprChunk900 {
			var renderer RepresentationRenderer
			var rendererErr error
			if opt.representationArm == ReprEvent {
				fullReader, ok := opt.formalEvidence.(evidenceReader)
				if !ok {
					rendererErr = fmt.Errorf("formal evidence reader does not satisfy evidenceReader")
				} else if opt.eventProject == nil {
					rendererErr = fmt.Errorf("event representation requires --event-project")
				} else {
					renderer = NewEventProjectionRenderer(opt.eventProject, fullReader)
				}
			} else {
				renderer, rendererErr = formalRepresentationRendererWithEpisodes(opt.representationArm, projections, opt.formalEvidence, opt.formalEpisodes)
			}
			if rendererErr == nil {
				anchors := buildFormalRankedAnchors(expanded)
				enriched, renderErr := renderer.Render(ctx, anchors)
				if renderErr == nil && len(enriched) > 0 {
					if opt.representationArm == ReprSemanticEpisode {
						// 025 semantic_episode: the renderer aggregates a cross-message
						// cluster into one candidate. Rebuild the expanded source set so
						// the answer bundle carries the episode narrative (multi-source)
						// instead of the per-source expansion, then rebuild the candidate
						// artifact from the folded expansion. This keeps RenderedCandidates
						// byte-identical to the bundle items (genuine episode anchors →
						// one episode candidate; fallback anchors → canonical per-source
						// candidates), which the episode anchor-prefix contract requires.
						expanded = rebuildExpandedForEpisodes(expanded, enriched, opt.formalEvidence)
						frozen.Candidate = buildExpandedFormalCandidateArtifact(protocol, qa, expanded, turnEvidence, retrievalCalls)
					} else {
						frozen.Candidate.RenderedCandidates = enriched
						frozen.Candidate.CandidateSetDigest = renderedCandidateSetDigest(enriched)
						frozen.Candidate.Mode = evalCandidateModeAnchorRendering
					}
				}
			}
		}
	}
	if err := validateEvalCandidateArtifact(protocol, frozen.Candidate); err != nil {
		frozen.InvalidReasons = append(frozen.InvalidReasons, "candidate_invalid")
	}

	system := answerSystemPromptForEval(qa, opt)
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
		if opt.compilerArm != "" || temporalActive {
			// Compile arm: use the evidencecompiler engine instead of the
			// legacy ranked-prefix packer. The compiler selects items under
			// the real token counter and produces an auditable trace. The
			// exact-token arm uses the same candidate list but a local,
			// token-level relevance selection. 027's temporal-resolution arm
			// (mutually exclusive with --compiler-arm) shares the same
			// candidate list and bundle/trace builders, adding deterministic
			// query-time time organization plus a per-question audit.
			var compiledBundle evidencecompiler.Bundle
			var compiledTrace evidencecompiler.Trace
			var resolutionAudit ResolutionAudit
			var compileErr error
			if opt.compilerArm == "exact_token" {
				compiledBundle, compiledTrace, compileErr = compileExactTokenArm(qa.Question, buildCompileCandidates(formalCompileSourceList(expanded)), protocol.Retrieval.CandidateLimit)
			} else if temporalActive {
				compiledBundle, compiledTrace, resolutionAudit, compileErr = compileTemporalResolutionArm(qa.Question, expanded, protocol.Retrieval.CandidateLimit)
			} else {
				compiledBundle, compiledTrace, compileErr = compileFormalSources(ctx, protocol, opt, qa, expanded)
			}
			if compileErr != nil {
				packErr = fmt.Errorf("compile: %w", compileErr)
			} else {
				compiledItems := compiledBundle.Items
				sourceByCandidate := compileSourceByCandidateID(expanded)
				items := compileBundleItems(protocol, compiledItems, sourceByCandidate)
				frozen.Trace = buildCompileTrace(protocol, qa.QuestionID, frozen.Candidate, compiledTrace, items)
				bundle, inputTokens, count, bundleErr := buildCompileBundle(ctx, protocol, opt, qa, frozen.Candidate, frozen.Trace, compiledBundle, sourceByCandidate, items)
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
					if temporalActive {
						// 027 FR-008/contract Rule 7: 记录 per-question 解析审计供
						// US2/US3 归因。写入失败 fail closed (审计产物不完整)。
						if auditErr := appendResolutionAudit(opt.runDir, qa.QuestionID, resolutionAudit); auditErr != nil {
							packErr = fmt.Errorf("resolution audit: %w", auditErr)
						}
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
	if opt.compilerArm == "" && !temporalActive {
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

	system := answerSystemPromptForEval(qa, opt)
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
	frozen := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, nil, qa, chunkTurns, turnEvidence)
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

func buildFormalCandidateArtifact(protocol evalProtocol, qa locomoQA, hits []memory.Result, chunkTurns map[string][]string, sourceByCandidate map[string][]string, turnEvidence map[string]string, retrievalCalls int) evalCandidateArtifact {
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
		QueryDigest: evalTextDigest(qa.Question), Mode: evalCandidateModeAnchorRendering, RetrievalCalls: retrievalCalls,
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
// extendCandidatesWithSiblings appends bounded shared-evidence sibling facts to
// the hit candidates (024 US2 neighbor extension). It queries depth-1 siblings
// over memory_projection_sources — no extra retrieval call, no graph store
// (spec FR-007). Siblings are appended only when they are not already present
// as hits; any failure degrades to the unextended candidate set so an extension
// error never loses a valid hit.
func extendCandidatesWithSiblings(ctx context.Context, projections *memory.ProjectionStore, entries *memory.EntryStore, hits []memory.Result) []memory.Result {
	if projections == nil || entries == nil {
		return hits
	}
	projectionIDs := make([]string, 0, len(hits))
	seenNames := make(map[string]struct{}, len(hits))
	for _, hit := range hits {
		seenNames[hit.Name] = struct{}{}
		if hit.ProjectionID != "" {
			projectionIDs = append(projectionIDs, hit.ProjectionID)
		}
	}
	if len(projectionIDs) == 0 {
		return hits
	}
	siblings, err := projections.SiblingFacts(ctx, projectionIDs, 8)
	if err != nil || len(siblings) == 0 {
		return hits // degrade gracefully (FR-008: no neighbors → zero change)
	}
	objectKeys := make([]string, 0, len(siblings))
	for _, s := range siblings {
		objectKeys = append(objectKeys, s.ObjectKey) // object_key == entry id
	}
	byID, err := entries.EntriesByID(ctx, objectKeys)
	if err != nil {
		return hits
	}
	extended := append([]memory.Result(nil), hits...)
	for _, sibling := range siblings {
		entry, ok := byID[sibling.ObjectKey]
		if !ok {
			continue
		}
		if _, already := seenNames[entry.Name]; already {
			continue
		}
		seenNames[entry.Name] = struct{}{}
		extended = append(extended, memory.Result{
			ProjectionID:   sibling.ID,
			ProjectionKind: sibling.Kind,
			Name:           entry.Name,
			Trigger:        entry.Trigger,
			Content:        entry.Content,
			EventDate:      entry.EventDate,
			CreatedAt:      entry.CreatedAt,
		})
	}
	return extended
}

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

// freezeFormalProtocol is the no-model half of T020/T114. It derives the
// dataset fingerprints and all current harness knobs from the actual command
// line/environment, then refuses to write a manifest from a dirty worktree.
// Without treatment flags it writes the frozen B1 legacy control; with
// exactly one treatment mechanism it writes that treatment's manifest bound
// to the B1 control protocol hash (controlHash, required for treatment).
// It deliberately writes only the manifest: scores and per-question artifacts
// belong to a later immutable run directory.
func freezeFormalProtocol(opt options, convs []conversation, controlHash string) error {
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
		return fmt.Errorf("formal freeze requires exactly one retrieval arm")
	}
	if err := validateFormalLegacyRecipe(arms[0]); err != nil {
		return err
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return err
	}
	experiment, err := buildFormalExperiment(opt, controlHash)
	if err != nil {
		return err
	}
	if experiment.Stage == "b1" && formalTreatmentMechanismRequested(opt) {
		return fmt.Errorf("formal B1 freeze cannot bind treatment mechanisms; pass no --compiler-arm/--representation/--event-projection/--gap-refetch")
	}
	if experiment.Stage != "b1" && !isDigest(controlHash) {
		return fmt.Errorf("treatment freeze requires a frozen B1 control protocol hash (--control-protocol)")
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
		Store:     evalStoreProvenance{SchemaVersion: 7, IngestionRecipe: "ledger_lossless_chunks_v2", IngestionConfigDigest: evalJSONDigest(evalFreezeIngestion{Chunks: opt.chunks, ImageCaptions: opt.imageCaptions}), ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"}},
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
		Experiment:     experiment,
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
		Store:     evalStoreProvenance{SchemaVersion: 7, IngestionRecipe: "ledger_lossless_chunks_v2", IngestionConfigDigest: evalJSONDigest(evalFreezeIngestion{Chunks: opt.chunks, ImageCaptions: opt.imageCaptions}), ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"}},
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
	if opt.unifiedAnswerContract {
		// Preserve the pure-unified digest byte-for-byte when the typed
		// combination is off. When enabled, bind the two LoCoMo typed
		// contracts (and the current-date rule they carry) so replay cannot
		// cross regimes.
		prompts := []string{
			unifiedAnswerContractPrompt,
			"unified_answer_contract=true",
			"runtime_current_date=user_context_only",
		}
		if opt.unifiedTypedPrompts {
			prompts = append(prompts,
				multiHopAnswerPrompt,
				openDomainAnswerPrompt,
				currentDateRule,
				"unified_typed_prompts=true",
			)
		}
		return evalJSONDigest(prompts)
	}
	prompts := []string{
		answerPromptForRegime(1, opt.forceAnswer, opt.temporalAnswerPrompt, false),
		answerPromptForRegime(2, opt.forceAnswer, opt.temporalAnswerPrompt, false),
		answerPromptForRegime(3, opt.forceAnswer, opt.temporalAnswerPrompt, false),
		answerPromptForRegime(4, opt.forceAnswer, opt.temporalAnswerPrompt, false),
		currentDateRule,
		fmt.Sprintf("temporal_date_scaffold=%t", opt.temporalDateScaffold),
	}
	// Preserve the historical LoCoMo digest byte-for-byte when the LME typed
	// mode is off. When enabled, bind every LongMemEval category prompt and the
	// switch so replay cannot cross regimes.
	if opt.lmeTypedPrompts {
		for _, category := range []int{6, 7, 8, 9, 10, 11, 12} {
			prompts = append(prompts, answerPromptForEval(category, opt))
		}
		prompts = append(prompts,
			fmt.Sprintf("lme_typed_prompts=%t", opt.lmeTypedPrompts),
		)
	}
	return evalJSONDigest(prompts)
}

type evalFreezeIngestion struct {
	Chunks        bool `json:"chunks"`
	ImageCaptions bool `json:"image_captions"`
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
