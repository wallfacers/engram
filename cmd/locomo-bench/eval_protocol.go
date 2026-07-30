package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const evalProtocolSchema = "022.v1"

type evalRunMode string

const (
	evalRunFormal       evalRunMode = "formal"
	evalRunB0Continuity evalRunMode = "b0_continuity"
	evalRunExploratory  evalRunMode = "exploratory"
)

// evalProtocol freezes every input that can otherwise turn an apparent A/B
// comparison into a changed experiment. ProtocolHash is deliberately omitted
// from canonical bytes, so it is a digest of the remaining fields.
type evalProtocol struct {
	Schema         string                     `json:"schema"`
	ProtocolID     string                     `json:"protocol_id"`
	ProtocolHash   string                     `json:"protocol_hash,omitempty"`
	CreatedAt      time.Time                  `json:"created_at"`
	Git            evalGitProvenance          `json:"git"`
	Benchmark      evalBenchmarkProvenance    `json:"benchmark"`
	Store          evalStoreProvenance        `json:"store"`
	Models         evalModelProvenance        `json:"models"`
	Retrieval      evalRetrievalProvenance    `json:"retrieval"`
	Budget         evalBudgetProtocol         `json:"budget"`
	Aggregation    evalAggregationProtocol    `json:"aggregation"`
	JudgeAudit     evalJudgeAuditProtocol     `json:"judge_audit"`
	CoverageStrata evalCoverageStrataProtocol `json:"coverage_strata"`
	Experiment     evalExperimentProtocol     `json:"experiment"`
}

type evalGitProvenance struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

type evalBenchmarkProvenance struct {
	Name              string `json:"name"`
	DatasetDigest     string `json:"dataset_digest"`
	Split             string `json:"split"`
	QuestionCount     int    `json:"question_count"`
	QuestionIDsDigest string `json:"question_ids_digest"`
}

type evalStoreProvenance struct {
	SchemaVersion             int               `json:"schema_version"`
	IngestionRecipe           string            `json:"ingestion_recipe"`
	IngestionConfigDigest     string            `json:"ingestion_config_digest"`
	ProjectionBuilderVersions map[string]string `json:"projection_builder_versions"`
}

type evalModelFingerprint struct {
	ID           string `json:"id"`
	Revision     string `json:"revision"`
	Provider     string `json:"provider"`
	PromptDigest string `json:"prompt_digest"`
}

type evalPlannerFingerprint struct {
	Enabled      bool   `json:"enabled"`
	ID           string `json:"id"`
	Revision     string `json:"revision"`
	Provider     string `json:"provider"`
	PromptDigest string `json:"prompt_digest"`
}

type evalModelProvenance struct {
	Extractor evalModelFingerprint   `json:"extractor"`
	Answerer  evalModelFingerprint   `json:"answerer"`
	Judge     evalModelFingerprint   `json:"judge"`
	Planner   evalPlannerFingerprint `json:"planner"`
}

type evalRetrievalProvenance struct {
	Recipe               string `json:"recipe"`
	EmbeddingFingerprint string `json:"embedding_fingerprint"`
	Reranker             string `json:"reranker"`
	CandidateLimit       int    `json:"candidate_limit"`
	CandidateRulesDigest string `json:"candidate_rules_digest"`
}

type evalBudgetProtocol struct {
	Profile             string `json:"profile"`
	AnswerInputTokenCap int    `json:"answer_input_token_cap"`
	MaxOutputTokens     int    `json:"max_output_tokens"`
	CandidateLimit      int    `json:"candidate_limit"`
	RetrievalCallLimit  int    `json:"retrieval_call_limit"`
	AnswerCallLimit     int    `json:"answer_call_limit"`
	CounterFingerprint  string `json:"counter_fingerprint"`
}

type evalAggregationProtocol struct {
	AnswerRepetitions int    `json:"answer_repetitions"`
	Rule              string `json:"rule"`
	JudgeRepetitions  int    `json:"judge_repetitions"`
	SeedPolicy        string `json:"seed_policy"`
}

type evalJudgeAuditProtocol struct {
	AllDiscordant            bool   `json:"all_discordant"`
	ConcordantSamplingDigest string `json:"concordant_sampling_digest"`
	Reviewers                int    `json:"reviewers"`
	BlindedToArm             bool   `json:"blinded_to_arm"`
	AdjudicationRule         string `json:"adjudication_rule"`
}

type evalCoverageStrataProtocol struct {
	Boundaries      []float64 `json:"boundaries"`
	SelectionDigest string    `json:"selection_digest"`
}

type evalExperimentProtocol struct {
	Stage               string          `json:"stage"`
	Arm                 string          `json:"arm"`
	ControlProtocolHash string          `json:"control_protocol_hash"`
	PrimaryCohort       string          `json:"primary_cohort"`
	MechanismFlags      map[string]bool `json:"mechanism_flags"`
}

func canonicalEvalProtocolJSON(protocol evalProtocol) ([]byte, error) {
	protocol.ProtocolHash = ""
	return json.Marshal(protocol)
}

func evalProtocolFingerprint(protocol evalProtocol) (string, error) {
	canonical, err := canonicalEvalProtocolJSON(protocol)
	if err != nil {
		return "", fmt.Errorf("canonical protocol: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateEvalProtocol(protocol evalProtocol, mode evalRunMode) error {
	if protocol.Schema != evalProtocolSchema {
		return fmt.Errorf("protocol schema = %q, want %q", protocol.Schema, evalProtocolSchema)
	}
	if strings.TrimSpace(protocol.ProtocolID) == "" || protocol.CreatedAt.IsZero() {
		return fmt.Errorf("protocol_id and created_at are required")
	}
	if mode != evalRunFormal && mode != evalRunB0Continuity && mode != evalRunExploratory {
		return fmt.Errorf("unknown run mode %q", mode)
	}
	if (mode == evalRunFormal || mode == evalRunB0Continuity) && protocol.Git.Dirty {
		return fmt.Errorf("%s protocol refuses dirty git state", mode)
	}
	if mode == evalRunB0Continuity && protocol.Experiment.Stage != "b0" {
		return fmt.Errorf("B0 continuity mode requires experiment stage b0")
	}
	if mode == evalRunFormal && protocol.Experiment.Stage == "b0" {
		return fmt.Errorf("formal promotion mode refuses B0 continuity protocol")
	}
	if len(protocol.Git.Commit) != 40 || !isLowerHex(protocol.Git.Commit) {
		return fmt.Errorf("git commit must be a full lowercase SHA-1")
	}
	if err := validateEvalBenchmark(protocol.Benchmark); err != nil {
		return err
	}
	if protocol.Store.SchemaVersion < 1 || strings.TrimSpace(protocol.Store.IngestionRecipe) == "" || !isDigest(protocol.Store.IngestionConfigDigest) {
		return fmt.Errorf("invalid store provenance")
	}
	if err := validateModelFingerprint("extractor", protocol.Models.Extractor); err != nil {
		return err
	}
	if err := validateModelFingerprint("answerer", protocol.Models.Answerer); err != nil {
		return err
	}
	if err := validateModelFingerprint("judge", protocol.Models.Judge); err != nil {
		return err
	}
	if protocol.Models.Planner.Enabled {
		if err := validateModelFingerprint("planner", evalModelFingerprint{
			ID: protocol.Models.Planner.ID, Revision: protocol.Models.Planner.Revision,
			Provider: protocol.Models.Planner.Provider, PromptDigest: protocol.Models.Planner.PromptDigest,
		}); err != nil {
			return err
		}
	}
	if protocol.Retrieval.Reranker != "disabled" {
		return fmt.Errorf("reranker must be disabled in a formal 022 protocol")
	}
	if strings.TrimSpace(protocol.Retrieval.Recipe) == "" || protocol.Retrieval.CandidateLimit < 1 || !isDigest(protocol.Retrieval.EmbeddingFingerprint) || !isDigest(protocol.Retrieval.CandidateRulesDigest) {
		return fmt.Errorf("invalid retrieval provenance")
	}
	if err := validateEvalBudget(protocol.Budget, protocol.Retrieval.CandidateLimit, protocol.Experiment.Stage); err != nil {
		return err
	}
	if protocol.Aggregation.AnswerRepetitions < 1 || protocol.Aggregation.AnswerRepetitions%2 == 0 || protocol.Aggregation.Rule != "majority_correctness" || protocol.Aggregation.JudgeRepetitions != 1 || protocol.Aggregation.SeedPolicy != "independent-recorded" {
		return fmt.Errorf("invalid aggregation policy")
	}
	if !protocol.JudgeAudit.AllDiscordant || protocol.JudgeAudit.Reviewers != 2 || !protocol.JudgeAudit.BlindedToArm || protocol.JudgeAudit.AdjudicationRule != "independent_then_adjudicate" || !isDigest(protocol.JudgeAudit.ConcordantSamplingDigest) {
		return fmt.Errorf("invalid judge audit policy")
	}
	if err := validateCoverageStrata(protocol.CoverageStrata); err != nil {
		return err
	}
	if err := validateEvalExperiment(protocol.Experiment); err != nil {
		return err
	}
	return nil
}

func validateEvalBenchmark(benchmark evalBenchmarkProvenance) error {
	if !isDigest(benchmark.DatasetDigest) || !isDigest(benchmark.QuestionIDsDigest) {
		return fmt.Errorf("benchmark digests are required")
	}
	switch benchmark.Name {
	case "locomo":
		if benchmark.Split != "category_1_4" || benchmark.QuestionCount != 1540 {
			return fmt.Errorf("locomo requires category_1_4 with 1540 questions")
		}
	case "longmemeval_s":
		if benchmark.Split != "cleaned_full_500" || benchmark.QuestionCount != 500 {
			return fmt.Errorf("longmemeval_s requires cleaned_full_500 with 500 questions")
		}
	default:
		return fmt.Errorf("unsupported benchmark %q", benchmark.Name)
	}
	return nil
}

func validateModelFingerprint(role string, model evalModelFingerprint) error {
	if strings.TrimSpace(model.ID) == "" || strings.TrimSpace(model.Revision) == "" ||
		strings.TrimSpace(model.Provider) == "" || !isDigest(model.PromptDigest) {
		return fmt.Errorf("invalid %s model fingerprint", role)
	}
	return nil
}

func validateEvalBudget(budget evalBudgetProtocol, retrievalLimit int, stage string) error {
	if budget.CandidateLimit != retrievalLimit {
		return fmt.Errorf("budget candidate limit %d differs from retrieval limit %d", budget.CandidateLimit, retrievalLimit)
	}
	if budget.MaxOutputTokens < 1 || budget.CandidateLimit < 1 || !isDigest(budget.CounterFingerprint) {
		return fmt.Errorf("invalid token/call budget")
	}
	if stage == "b0" {
		if budget.Profile != "continuity" || budget.AnswerInputTokenCap != 0 ||
			budget.RetrievalCallLimit != 3 || budget.AnswerCallLimit != 3 {
			return fmt.Errorf("invalid B0 continuity budget")
		}
		return nil
	}
	if (budget.Profile != "low" && budget.Profile != "high") ||
		budget.AnswerInputTokenCap < 1 || budget.RetrievalCallLimit < 0 ||
		budget.AnswerCallLimit != 1 {
		return fmt.Errorf("invalid token/call budget")
	}
	return nil
}

func validateCoverageStrata(strata evalCoverageStrataProtocol) error {
	if len(strata.Boundaries) < 2 || !isDigest(strata.SelectionDigest) {
		return fmt.Errorf("coverage strata require boundaries and selection digest")
	}
	if strata.Boundaries[0] != 0 || strata.Boundaries[len(strata.Boundaries)-1] != 1 {
		return fmt.Errorf("coverage strata must start at 0 and end at 1")
	}
	for i := 1; i < len(strata.Boundaries); i++ {
		if strata.Boundaries[i] <= strata.Boundaries[i-1] {
			return fmt.Errorf("coverage strata boundaries must be strictly ascending")
		}
	}
	return nil
}

func validateEvalExperiment(experiment evalExperimentProtocol) error {
	validStages := map[string]bool{
		"b0": true, "b1": true, "representation_navigation": true,
		"representation_rendering": true, "compiler": true, "event": true,
		"gap": true, "projection": true,
	}
	if !validStages[experiment.Stage] || strings.TrimSpace(experiment.Arm) == "" || strings.TrimSpace(experiment.PrimaryCohort) == "" {
		return fmt.Errorf("invalid experiment stage, arm, or primary cohort")
	}
	if experiment.Stage != "b0" && experiment.Stage != "b1" && !isDigest(experiment.ControlProtocolHash) {
		return fmt.Errorf("experiment stage %s requires control protocol hash", experiment.Stage)
	}
	if experiment.Stage == "b0" {
		if experiment.Arm != "legacy_product_continuity" || !experiment.MechanismFlags["idk_retry"] {
			return fmt.Errorf("B0 continuity requires the legacy product arm with IDK retry")
		}
	}
	if experiment.Stage == "b1" && experiment.MechanismFlags["idk_retry"] {
		return fmt.Errorf("B1 control must disable legacy IDK retry")
	}
	return nil
}

func isPromotionEligible(protocol evalProtocol, mode evalRunMode) bool {
	return mode == evalRunFormal && !protocol.Git.Dirty
}

func freezeEvalProtocol(runDir string, protocol evalProtocol, mode evalRunMode) (evalProtocol, error) {
	if err := validateEvalProtocol(protocol, mode); err != nil {
		return evalProtocol{}, err
	}
	hash, err := evalProtocolFingerprint(protocol)
	if err != nil {
		return evalProtocol{}, err
	}
	protocol.ProtocolHash = hash
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return evalProtocol{}, fmt.Errorf("create protocol run dir: %w", err)
	}
	if err := writeJSON(filepath.Join(runDir, "protocol.json"), protocol); err != nil {
		return evalProtocol{}, fmt.Errorf("write protocol: %w", err)
	}
	return protocol, nil
}

func checkEvalProtocolResume(runDir string, requested evalProtocol, mode evalRunMode) error {
	path := filepath.Join(runDir, "protocol.json")
	raw, err := os.ReadFile(path) //nolint:gosec // run dir is operator-selected
	if err != nil {
		return fmt.Errorf("read frozen protocol: %w", err)
	}
	var frozen evalProtocol
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return fmt.Errorf("decode frozen protocol: %w", err)
	}
	if err := validateEvalProtocol(frozen, mode); err != nil {
		return fmt.Errorf("invalid frozen protocol: %w", err)
	}
	frozenHash, err := evalProtocolFingerprint(frozen)
	if err != nil {
		return err
	}
	if frozen.ProtocolHash != frozenHash {
		return fmt.Errorf("frozen protocol hash mismatch; use a fresh --run-dir")
	}
	if err := validateEvalProtocol(requested, mode); err != nil {
		return err
	}
	requestedHash, err := evalProtocolFingerprint(requested)
	if err != nil {
		return err
	}
	if requestedHash != frozenHash {
		return fmt.Errorf("protocol fingerprint changed (%s != %s); use a fresh --run-dir", requestedHash, frozenHash)
	}
	return nil
}

func isDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && len(strings.TrimPrefix(value, "sha256:")) > 0
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
