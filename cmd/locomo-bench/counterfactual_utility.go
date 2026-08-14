package main

// 042 counterfactual evidence utility gate — v1 data model.
//
// This file defines the closed enums, identities, and artifact structs shared by
// every stage (label/pilot/collect/diagnose/confirm/transfer). It is a
// benchmark-only research capability: default off, no public provider/engine
// contract changes (Constitution II), no paid cloud reranker/recall lever
// (death rule). Everything under memory/ embedding/ provider/ store/ internal/
// is untouched.
//
// The structs mirror specs/042-counterfactual-evidence-utility/data-model.md and
// contracts/utility-artifacts.md. All float fields must be finite; canonical
// digests are computed by utilityCanonicalDigest. Schema truth lives in these
// types (tagged JSON shapes), not a separate DDL.

import (
	"errors"
	"fmt"
	"strings"
)

const utilitySchemaVersion = "counterfactual-utility/v1"

// Frozen v1 gate / protocol constants (fixed, not CLI-overridable).
const (
	utilityShallowK                = 30
	utilityDeepK                   = 150
	utilityRepetitions             = 3
	utilityRidgeLambda             = 1.0
	utilityPilotAUCGate            = 0.65
	utilityMinNetQuestions         = 25
	utilityMinAccuracy             = 0.90
	utilityMaxTokenRatio           = 0.60
	utilityHolmAlpha               = 0.05
	utilityMaxAttempts             = 3
	utilityMaxModelLen             = 32768
	utilityResponseLimit           = 64 * 1024 * 1024 // 67,108,864 bytes
	utilityHistoricalBenefitAnchor = 56               // 历史 label audit 复现锚
	utilityHistoricalHarmAnchor    = 31

	// 精度前沿 56c-31h>=25 的 harm 上限：h <= (56c-25)/31。c=0.70 时 h<=0.46。
	utilityPrecisionBenefitWeight = 56.0
	utilityPrecisionHarmWeight    = 31.0
	utilityPrecisionNetFloor      = 25.0
)

// Closed stage enum.
type utilityStage string

const (
	utilityStageLabel    utilityStage = "label"
	utilityStagePilot    utilityStage = "pilot"
	utilityStageCollect  utilityStage = "collect"
	utilityStageDiagnose utilityStage = "diagnose"
	utilityStageConfirm  utilityStage = "confirm"
	utilityStageTransfer utilityStage = "transfer"
)

func parseUtilityStage(s string) (utilityStage, error) {
	st := utilityStage(s)
	if !st.valid() {
		return "", fmt.Errorf("invalid utility stage %q: must be one of label|pilot|collect|diagnose|confirm|transfer", s)
	}
	return st, nil
}

func (s utilityStage) valid() bool {
	switch s {
	case utilityStageLabel, utilityStagePilot, utilityStageCollect, utilityStageDiagnose, utilityStageConfirm, utilityStageTransfer:
		return true
	}
	return false
}

// String keeps CLI/stdout output readable.
func (s utilityStage) String() string { return string(s) }

// utilityOfflineStage reports whether a stage makes no model calls and must be
// dispatched before any provider/env resolution (label: zero-model audit;
// diagnose: offline read of a sealed collect artifact).
func utilityOfflineStage(s utilityStage) bool {
	return s == utilityStageLabel || s == utilityStageDiagnose
}

// Closed answer-arm enum. paired_deep is only legal inside collect (offline
// calibration pairing); confirm/transfer use policy_deep + fixed_deep.
type utilityArm string

const (
	utilityArmShallow      utilityArm = "shallow"
	utilityArmPairedDeep   utilityArm = "paired_deep"
	utilityArmPolicyDeep   utilityArm = "policy_deep"
	utilityArmFixedDeep    utilityArm = "fixed_deep"
	utilityArmJudgeShallow utilityArm = "judge_shallow"
	utilityArmJudgePaired  utilityArm = "judge_paired_deep"
	utilityArmJudgePolicy  utilityArm = "judge_policy"
	utilityArmJudgeFixed   utilityArm = "judge_fixed_deep"
	utilityArmPreflight    utilityArm = "preflight"
)

func (a utilityArm) valid() bool {
	switch a {
	case utilityArmShallow, utilityArmPairedDeep, utilityArmPolicyDeep, utilityArmFixedDeep,
		utilityArmJudgeShallow, utilityArmJudgePaired, utilityArmJudgePolicy, utilityArmJudgeFixed, utilityArmPreflight:
		return true
	}
	return false
}

func (a utilityArm) judgeArm() bool {
	return a == utilityArmJudgeShallow || a == utilityArmJudgePaired || a == utilityArmJudgePolicy || a == utilityArmJudgeFixed
}

// utilityArmValidForStage enforces paired_deep = collect only and forbids
// policy/fixed control arms in collect (the collect control is paired_deep).
func utilityArmValidForStage(arm utilityArm, stage utilityStage) error {
	if !arm.valid() {
		return fmt.Errorf("unknown answer arm %q", arm)
	}
	switch arm {
	case utilityArmPairedDeep:
		if stage != utilityStageCollect {
			return fmt.Errorf("paired_deep is only legal in collect, got stage %s", stage)
		}
	case utilityArmPolicyDeep, utilityArmFixedDeep, utilityArmJudgePolicy, utilityArmJudgeFixed:
		if stage == utilityStageCollect {
			return fmt.Errorf("%s is not a collect arm (collect control is paired_deep)", arm)
		}
	}
	return nil
}

// Closed call-unit state machine values.
type utilityCallUnitState string

const (
	utilityCallUnitStarted   utilityCallUnitState = "STARTED"
	utilityCallUnitCompleted utilityCallUnitState = "COMPLETED"
	utilityCallUnitFailed    utilityCallUnitState = "FAILED"
)

func (s utilityCallUnitState) valid() bool {
	switch s {
	case utilityCallUnitStarted, utilityCallUnitCompleted, utilityCallUnitFailed:
		return true
	}
	return false
}

// Closed usage-status values (terminal call units only).
type utilityUsageStatus string

const (
	utilityUsageReported          utilityUsageStatus = "reported"
	utilityUsageConservativeBound utilityUsageStatus = "conservative_bound"
	utilityUsageUnavailable       utilityUsageStatus = "unavailable"
	utilityUsageNotApplicable     utilityUsageStatus = "not_applicable"
)

// Closed FAILED failure-reason codes (never raw upstream text).
type utilityFailureReason string

const (
	utilityFailureTimeout        utilityFailureReason = "timeout"
	utilityFailureContextCancel  utilityFailureReason = "context_canceled"
	utilityFailureNetwork        utilityFailureReason = "network_error"
	utilityFailureHTTP429        utilityFailureReason = "http_429"
	utilityFailureHTTP4xx        utilityFailureReason = "http_4xx"
	utilityFailureHTTP5xx        utilityFailureReason = "http_5xx"
	utilityFailureContextLength  utilityFailureReason = "context_length"
	utilityFailureResponseTooBig utilityFailureReason = "response_too_large"
	utilityFailureDecode         utilityFailureReason = "decode_error"
	utilityFailureSchema         utilityFailureReason = "schema_error"
	utilityFailureEmptyChoice    utilityFailureReason = "empty_choice"
	utilityFailureEmptyAnswer    utilityFailureReason = "empty_answer"
	utilityFailureInvalidUsage   utilityFailureReason = "invalid_usage"
	utilityFailureJudgeParse     utilityFailureReason = "judge_parse_error"
)

// retryable reports whether a failure reason may advance to a next attempt.
func (r utilityFailureReason) retryable() bool {
	switch r {
	case utilityFailureTimeout, utilityFailureNetwork, utilityFailureHTTP429, utilityFailureHTTP5xx:
		return true
	}
	return false
}

// Closed signal-unavailable reasons.
type utilitySignalUnavailableReason string

const (
	utilitySigMissingLogprobs   utilitySignalUnavailableReason = "missing_logprobs"
	utilitySigMissingTokenBytes utilitySignalUnavailableReason = "missing_token_bytes"
	utilitySigContentNotSuffix  utilitySignalUnavailableReason = "content_not_generated_suffix"
	utilitySigFinalNotSuffix    utilitySignalUnavailableReason = "final_not_content_suffix"
	utilitySigEmptyFinal        utilitySignalUnavailableReason = "empty_final"
	utilitySigBoundaryInToken   utilitySignalUnavailableReason = "final_boundary_inside_token"
	utilitySigMissingTop2       utilitySignalUnavailableReason = "missing_top2"
	utilitySigNonFinite         utilitySignalUnavailableReason = "non_finite_probability"
	utilitySigUnsupportedShape  utilitySignalUnavailableReason = "unsupported_response_shape"
)

// Closed utility-label enum.
type utilityLabelKind string

const (
	utilityLabelBenefit utilityLabelKind = "BENEFIT"
	utilityLabelNeutral utilityLabelKind = "NEUTRAL"
	utilityLabelHarm    utilityLabelKind = "HARM"
)

func utilityLabelFromUtility(u int) (utilityLabelKind, error) {
	switch u {
	case 1:
		return utilityLabelBenefit, nil
	case 0:
		return utilityLabelNeutral, nil
	case -1:
		return utilityLabelHarm, nil
	}
	return "", fmt.Errorf("utility %d outside {-1,0,+1}", u)
}

// Closed decision actions / reasons.
type utilityDecisionAction string

const (
	utilityActionKeepShallow utilityDecisionAction = "keep_shallow"
	utilityActionDeepen      utilityDecisionAction = "deepen"
	utilityActionForcedDeep  utilityDecisionAction = "forced_deep"
)

type utilityDecisionReason string

const (
	utilityReasonPredictedBenefit    utilityDecisionReason = "predicted_benefit"
	utilityReasonPredictedNonBenefit utilityDecisionReason = "predicted_non_benefit"
	utilityReasonSignalUnavailable   utilityDecisionReason = "signal_unavailable"
)

// Closed threshold kinds (infinity is encoded as an enum, never IEEE).
type utilityThresholdKind string

const (
	utilityThresholdFinite utilityThresholdKind = "finite"
	utilityThresholdAlways utilityThresholdKind = "always"
	utilityThresholdNever  utilityThresholdKind = "never"
)

// Closed seal status values.
type utilitySealStatus string

const (
	utilitySealComplete utilitySealStatus = "COMPLETE"
	utilitySealInvalid  utilitySealStatus = "INVALID"
)

// utilityDecisionKey is the canonical decision identity. It is the identity for
// every answer/judge/label/decision record.
type utilityDecisionKey struct {
	Benchmark      string `json:"benchmark"`
	ConversationID int    `json:"conversation_id"`
	QuestionID     string `json:"question_id"`
	QuestionIndex  int    `json:"question_index,omitempty"`
	Category       string `json:"category,omitempty"`
	Repetition     int    `json:"repetition"`
}

func (k utilityDecisionKey) String() string {
	return fmt.Sprintf("%s/%d/%s/%d", k.Benchmark, k.ConversationID, k.QuestionID, k.Repetition)
}

// --- Run manifest (provenance root, frozen before any model call) ---

type utilityAnswererIdentity struct {
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	Revision           string `json:"revision"`
	EndpointDigest     string `json:"endpoint_digest"`
	ServerConfigDigest string `json:"server_config_digest"`
	TemperatureRequest string `json:"temperature_request_mode"`
	MaxTokens          int    `json:"max_tokens"`
	MaxModelLen        int    `json:"max_model_len"`
	ThinkingMode       string `json:"thinking_mode,omitempty"`
}

type utilityBenchmarkIdentity struct {
	Name              string `json:"name"`
	DatasetFormat     string `json:"dataset_format"`
	DatasetDigest     string `json:"dataset_digest"`
	QuestionCount     int    `json:"question_count"`
	QuestionIDsDigest string `json:"question_ids_digest"`
	ConversationIDs   []int  `json:"conversation_ids"`
	Repetitions       int    `json:"repetitions"`
}

type utilityRecipeIdentity struct {
	Retrieval      string `json:"retrieval"`
	ShallowK       int    `json:"shallow_k"`
	DeepK          int    `json:"deep_k"`
	ChunkQuota     int    `json:"chunk_quota"`
	ForceAnswer    bool   `json:"force_answer"`
	TraceMediation bool   `json:"trace_mediation"`
	Thinking       string `json:"thinking"`
}

type utilitySignalProtocol struct {
	Mapping           string   `json:"mapping"`
	Logprobs          bool     `json:"logprobs"`
	TopLogprobs       int      `json:"top_logprobs"`
	ResponseBodyLimit int      `json:"response_body_limit_bytes"`
	Features          []string `json:"features"`
}

type utilityCalibrationProtocol struct {
	Split              string  `json:"split"`
	Rule               string  `json:"rule"`
	Lambda             float64 `json:"lambda"`
	ThresholdObjective string  `json:"threshold_objective"`
}

type utilityGateConfig struct {
	MinimumNetQuestions int     `json:"minimum_net_questions"`
	NetSemantics        string  `json:"minimum_net_semantics"`
	QualityNotBelowDeep bool    `json:"quality_not_below_same_batch_deep"`
	MinimumAccuracy     float64 `json:"minimum_accuracy"`
	MaximumTokenRatio   float64 `json:"maximum_token_ratio"`
	CategoryLoss        string  `json:"category_loss"`
	HolmAlpha           float64 `json:"holm_alpha"`
	PrecisionFrontier   string  `json:"precision_frontier"`
	BenefitAnchor       int     `json:"benefit_anchor"`
	HarmAnchor          int     `json:"harm_anchor"`
}

type utilityJudgeIdentity struct {
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	Revision           string `json:"revision"`
	EndpointDigest     string `json:"endpoint_digest"`
	PromptDigest       string `json:"prompt_digest"`
	Mem0Aligned        bool   `json:"mem0_aligned"`
	CleanFinalAnswer   string `json:"clean_final_answer"`
	TemperatureRequest string `json:"temperature_request_mode"`
}

type utilityCallPolicy struct {
	MaxAttempts              int      `json:"max_attempts"`
	Retryable                []string `json:"retryable"`
	UnknownAnswerUsageCharge string   `json:"unknown_answer_usage_charge"`
}

type utilitySourceRef struct {
	Stage          string `json:"stage"`
	ManifestDigest string `json:"manifest_digest"`
	SealDigest     string `json:"seal_digest"`
	ReportDigest   string `json:"report_digest"`
}

type utilityBuildIdentity struct {
	BinaryDigest   string `json:"binary_digest"`
	SourceRevision string `json:"source_revision"`
	SourceModified bool   `json:"source_modified"`
	GoVersion      string `json:"go_version"`
}

type utilityRunManifest struct {
	Schema              string                     `json:"schema"`
	RunID               string                     `json:"run_id"`
	Stage               utilityStage               `json:"stage"`
	CreatedAt           string                     `json:"created_at"`
	Source              []utilitySourceRef         `json:"source,omitempty"`
	Benchmark           utilityBenchmarkIdentity   `json:"benchmark"`
	Recipe              utilityRecipeIdentity      `json:"recipe"`
	RetrievalProvenance map[string]any             `json:"retrieval_provenance,omitempty"`
	Answerer            utilityAnswererIdentity    `json:"answerer"`
	SignalProtocol      utilitySignalProtocol      `json:"signal_protocol"`
	CalibrationProtocol utilityCalibrationProtocol `json:"calibration_protocol,omitempty"`
	Judge               utilityJudgeIdentity       `json:"judge,omitempty"`
	CallPolicy          utilityCallPolicy          `json:"call_policy"`
	Gates               utilityGateConfig          `json:"gates,omitempty"`
	Build               utilityBuildIdentity       `json:"build"`
	Fixture             bool                       `json:"fixture,omitempty"`
}

func (m *utilityRunManifest) validate() error {
	if m == nil || m.Schema != utilitySchemaVersion {
		return fmt.Errorf("manifest missing %s schema", utilitySchemaVersion)
	}
	if !m.Stage.valid() {
		return fmt.Errorf("manifest has invalid stage %q", m.Stage)
	}
	if m.Benchmark.Repetitions != utilityRepetitions {
		return fmt.Errorf("manifest repetitions=%d, want %d", m.Benchmark.Repetitions, utilityRepetitions)
	}
	if m.Recipe.ShallowK != utilityShallowK || m.Recipe.DeepK != utilityDeepK {
		return fmt.Errorf("manifest recipe k=%d/%d, want %d/%d", m.Recipe.ShallowK, m.Recipe.DeepK, utilityShallowK, utilityDeepK)
	}
	if m.Answerer.MaxModelLen != utilityMaxModelLen {
		return fmt.Errorf("manifest max_model_len=%d, want %d", m.Answerer.MaxModelLen, utilityMaxModelLen)
	}
	if m.Answerer.TemperatureRequest != "omitted" {
		return fmt.Errorf("manifest temperature_request_mode=%q, want omitted", m.Answerer.TemperatureRequest)
	}
	if m.CallPolicy.MaxAttempts != utilityMaxAttempts {
		return fmt.Errorf("manifest max_attempts=%d, want %d", m.CallPolicy.MaxAttempts, utilityMaxAttempts)
	}
	return nil
}

// --- Call unit (append-only crash journal record) ---

type utilityCallUnitRecord struct {
	Schema           string               `json:"schema,omitempty"`
	LogicalCallID    string               `json:"logical_call_id"`
	UnitID           string               `json:"unit_id"`
	DecisionKey      *utilityDecisionKey  `json:"decision_key,omitempty"`
	Arm              utilityArm           `json:"arm"`
	State            utilityCallUnitState `json:"state"`
	RequestDigest    string               `json:"request_digest,omitempty"`
	Attempt          int                  `json:"attempt"`
	StartedAt        string               `json:"started_at,omitempty"`
	LatencyMS        int64                `json:"latency_ms,omitempty"`
	InputTokens      int                  `json:"input_tokens,omitempty"`
	OutputTokens     int                  `json:"output_tokens,omitempty"`
	UsageStatus      utilityUsageStatus   `json:"usage_status,omitempty"`
	RatioTokenCharge int                  `json:"ratio_token_charge,omitempty"`
	AnswerDigest     string               `json:"answer_digest,omitempty"`
	ResponseDigest   string               `json:"response_digest,omitempty"`
	FailureReason    utilityFailureReason `json:"failure_reason,omitempty"`
	Retryable        bool                 `json:"retryable,omitempty"`
}

// --- Probability signal record ---

type utilityTokenTraceEntry struct {
	ByteLen        int     `json:"byte_len"`
	SampledLogprob float64 `json:"sampled_logprob"`
	Top1Logprob    float64 `json:"top1_logprob"`
	Top2Logprob    float64 `json:"top2_logprob"`
}

type utilityThinkingDiagnostic struct {
	RoutingEligible bool    `json:"routing_eligible"`
	TokenCount      int     `json:"token_count,omitempty"`
	MeanLogprob     float64 `json:"mean_logprob,omitempty"`
}

type utilityProbabilitySignal struct {
	Status              string                     `json:"status"` // available|unavailable
	Reason              string                     `json:"reason,omitempty"`
	ContentDigest       string                     `json:"content_digest,omitempty"`
	TokenTraceDigest    string                     `json:"token_trace_digest,omitempty"`
	GeneratedTokenCount int                        `json:"generated_token_count,omitempty"`
	FinalTokenCount     int                        `json:"final_token_count,omitempty"`
	FinalByteStart      int                        `json:"final_byte_start,omitempty"`
	FinalByteEnd        int                        `json:"final_byte_end,omitempty"`
	FeatureNamesDigest  string                     `json:"feature_names_digest,omitempty"`
	Features            []float64                  `json:"features,omitempty"`
	FinalTrace          []utilityTokenTraceEntry   `json:"final_trace,omitempty"`
	FinalLengthStratum  string                     `json:"final_length_stratum,omitempty"`
	ThinkingDiagnostic  *utilityThinkingDiagnostic `json:"thinking_diagnostic,omitempty"`
}

func (s *utilityProbabilitySignal) available() bool {
	return s != nil && s.Status == "available"
}

// --- Answer attempt receipt (label-blind public record) ---

type utilityRetrievalCost struct {
	K                    int    `json:"k"`
	CandidateCount       int    `json:"candidate_count"`
	Calls                int    `json:"calls"`
	LatencyMS            int    `json:"latency_ms"`
	EmbeddingCalls       int    `json:"embedding_calls"`
	EmbeddingFailures    int    `json:"embedding_failures"`
	EmbeddingLatencyMS   int    `json:"embedding_latency_ms"`
	EmbeddingTokenStatus string `json:"embedding_token_usage_status"`
}

type utilityAnswerUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type utilityAnswerAttempt struct {
	Schema            string                    `json:"schema,omitempty"`
	AnswerAttemptID   string                    `json:"answer_attempt_id"`
	LogicalCallID     string                    `json:"logical_call_id,omitempty"`
	CompletedUnitID   string                    `json:"completed_unit_id,omitempty"`
	DecisionKey       utilityDecisionKey        `json:"decision_key"`
	Arm               utilityArm                `json:"arm"`
	QuestionDigest    string                    `json:"question_digest,omitempty"`
	Retrieval         utilityRetrievalCost      `json:"retrieval"`
	FinalAnswer       string                    `json:"final_answer"`
	AnswerDigest      string                    `json:"answer_digest"`
	FinalAnswerDigest string                    `json:"final_answer_digest"`
	Usage             utilityAnswerUsage        `json:"usage"`
	LatencyMS         int                       `json:"latency_ms"`
	Signal            *utilityProbabilitySignal `json:"signal,omitempty"`
}

// --- Judge outcome (score-only custody; never a runtime input) ---

type utilityJudgeOutcome struct {
	Schema            string             `json:"schema,omitempty"`
	DecisionKey       utilityDecisionKey `json:"decision_key"`
	AnswerArm         utilityArm         `json:"answer_arm"`
	AnswerDigest      string             `json:"answer_digest"`
	Correct           bool               `json:"correct"`
	JudgePromptDigest string             `json:"judge_prompt_digest"`
	JudgeModelDigest  string             `json:"judge_model_digest"`
	Usage             utilityAnswerUsage `json:"usage"`
	LogicalCalls      int                `json:"logical_calls"`
	ProviderAttempts  int                `json:"provider_attempts"`
	LatencyMS         int                `json:"latency_ms"`
}

// --- Utility label (derived from a shallow/deep pair; hidden) ---

type utilityUtilityLabel struct {
	Schema               string             `json:"schema,omitempty"`
	DecisionKey          utilityDecisionKey `json:"decision_key"`
	ShallowAnswerDigest  string             `json:"shallow_answer_digest"`
	DeepAnswerDigest     string             `json:"deep_answer_digest"`
	ShallowCorrect       bool               `json:"shallow_correct"`
	DeepCorrect          bool               `json:"deep_correct"`
	Utility              int                `json:"utility"`
	Label                utilityLabelKind   `json:"label"`
	SourceManifestDigest string             `json:"source_manifest_digest,omitempty"`
	SourceSealDigest     string             `json:"source_seal_digest,omitempty"`
	Aggregation          string             `json:"aggregation,omitempty"`
}

// --- Calibration entities ---

type utilityFeatureScaler struct {
	FeatureNames          []string  `json:"feature_names"`
	Means                 []float64 `json:"means"`
	PopulationStddevs     []float64 `json:"population_stddevs"`
	ZeroVariance          []bool    `json:"zero_variance"`
	TrainingAvailableRows int       `json:"training_available_rows"`
}

type utilityRuleThreshold struct {
	Kind  utilityThresholdKind `json:"kind"`
	Value float64              `json:"value,omitempty"`
}

type utilityTrainingObjectiveReceipt struct {
	Correct         int    `json:"correct"`
	Net             int    `json:"net"`
	Tokens          int    `json:"tokens"`
	DeepAnswerCalls int    `json:"deep_answer_calls"`
	SelectedBy      string `json:"selected_by"`
}

type utilityCalibratedRule struct {
	RuleID                    string                          `json:"rule_id"`
	Scope                     string                          `json:"scope"` // fold|global_transfer
	HeldOutConversation       *int                            `json:"held_out_conversation"`
	TrainingConversations     []int                           `json:"training_conversations"`
	TrainingQuestionIDsDigest string                          `json:"training_question_ids_digest"`
	Scaler                    utilityFeatureScaler            `json:"scaler"`
	Intercept                 float64                         `json:"intercept"`
	Coefficients              []float64                       `json:"coefficients"`
	Threshold                 utilityRuleThreshold            `json:"threshold"`
	ThresholdCandidateCount   int                             `json:"threshold_candidate_count"`
	TrainingObjectiveReceipt  utilityTrainingObjectiveReceipt `json:"training_objective_receipt,omitempty"`
	Complexity                map[string]int                  `json:"complexity"`
	RoutingFeatureDigest      string                          `json:"routing_feature_digest"`
	LocomoInSampleForbidden   bool                            `json:"locomo_in_sample_score_forbidden"`
}

type utilityCalibrationFold struct {
	FoldID                    string                `json:"fold_id"`
	TrainingConversations     []int                 `json:"training_conversations"`
	ValidationConversation    *int                  `json:"validation_conversation"`
	Rule                      utilityCalibratedRule `json:"rule"`
	ValidationDecisionsDigest string                `json:"validation_decisions_digest"`
	UndefinedMetrics          map[string]string     `json:"undefined_metrics,omitempty"`
	StabilityWarnings         []string              `json:"stability_warnings,omitempty"`
	Valid                     bool                  `json:"valid"`
	InvalidReasons            []string              `json:"invalid_reasons,omitempty"`
}

// --- Runtime utility decision (label-blind, written before scoring) ---

type utilityRuntimeCost struct {
	RetrievalCalls       int `json:"retrieval_calls"`
	EmbeddingCalls       int `json:"embedding_calls"`
	EmbeddingLatencyMS   int `json:"embedding_latency_ms"`
	LogicalAnswerCalls   int `json:"logical_answer_calls"`
	ProviderAttempts     int `json:"provider_attempts"`
	ReportedInputTokens  int `json:"reported_input_tokens"`
	ReportedOutputTokens int `json:"reported_output_tokens"`
	RatioTokenCharge     int `json:"ratio_token_charge"`
	SerialLatencyMS      int `json:"serial_latency_ms"`
}

type utilityUtilityDecision struct {
	Schema           string                `json:"schema,omitempty"`
	DecisionKey      utilityDecisionKey    `json:"decision_key"`
	RuleID           string                `json:"rule_id"`
	SignalStatus     string                `json:"signal_status"`
	FeaturesDigest   string                `json:"features_digest,omitempty"`
	Score            *float64              `json:"score,omitempty"`
	Threshold        *utilityRuleThreshold `json:"threshold,omitempty"`
	Action           utilityDecisionAction `json:"action"`
	Reason           utilityDecisionReason `json:"reason"`
	ShallowAttemptID string                `json:"shallow_attempt_id"`
	DeepAttemptID    string                `json:"deep_attempt_id,omitempty"`
	RuntimeCost      utilityRuntimeCost    `json:"runtime_cost"`
	DecisionDigest   string                `json:"decision_digest"`
}

// --- Pilot receipt ---

type utilityPilotReceipt struct {
	Schema               string         `json:"schema"`
	Verdict              string         `json:"verdict"` // GO|NO-GO|INVALID
	Claim                string         `json:"claim"`
	ConversationIDs      []int          `json:"conversation_ids"`
	Questions            int            `json:"questions"`
	DecisionUnits        int            `json:"decision_units"`
	AUC                  *float64       `json:"auc"`
	AUCGate              float64        `json:"auc_gate"`
	AUCNullReason        string         `json:"auc_null_reason,omitempty"`
	Counts               map[string]int `json:"counts"`
	SignalAvailable      int            `json:"signal_available"`
	SignalUnavailable    int            `json:"signal_unavailable"`
	GatePassed           bool           `json:"gate_passed"`
	GateObserved         string         `json:"gate_observed"`
	Cost                 map[string]int `json:"cost"`
	ProductionAuthorized bool           `json:"production_authorized"`
}

// --- Evaluation receipt (terminal report) ---

type utilityGateRecord struct {
	Name      string `json:"name"`
	Observed  any    `json:"observed"`
	Required  string `json:"required"`
	Passed    bool   `json:"passed"`
	Authority string `json:"authority"`
}

type utilityPrecisionFrontier struct {
	BenefitCapture  float64 `json:"benefit_capture"`
	HarmTrigger     float64 `json:"harm_trigger"`
	FrontierValue   float64 `json:"precision_frontier_56c_minus_31h"`
	RequiredHarmCap float64 `json:"required_harm_cap"`
	Passed          bool    `json:"passed"`
}

type utilityEvaluationReceipt struct {
	Schema               string                    `json:"schema"`
	Verdict              string                    `json:"verdict"`
	Claim                string                    `json:"claim"`
	Validity             map[string]any            `json:"validity"`
	Population           map[string]any            `json:"population"`
	Labels               map[string]any            `json:"labels"`
	Quality              map[string]any            `json:"quality"`
	Utility              map[string]any            `json:"utility"`
	Cost                 map[string]any            `json:"cost"`
	Calibration          map[string]any            `json:"calibration,omitempty"`
	Precision            *utilityPrecisionFrontier `json:"precision_frontier,omitempty"`
	Gates                []utilityGateRecord       `json:"gates"`
	ClaimBoundary        string                    `json:"claim_boundary"`
	ProductionAuthorized bool                      `json:"production_authorized"`
}

// --- Stage seal ---

type utilityStageSeal struct {
	Schema                   string            `json:"schema"`
	Stage                    utilityStage      `json:"stage"`
	Status                   utilitySealStatus `json:"status"`
	ManifestDigest           string            `json:"manifest_digest"`
	SourceSealDigest         string            `json:"source_seal_digest,omitempty"`
	ArtifactDigests          map[string]string `json:"artifact_digests,omitempty"`
	Counts                   map[string]any    `json:"counts,omitempty"`
	UsageByArm               map[string]any    `json:"usage_by_arm,omitempty"`
	DecisionDigest           string            `json:"decision_digest,omitempty"`
	ReportDigest             string            `json:"report_digest,omitempty"`
	Verdict                  string            `json:"verdict,omitempty"`
	GlobalTransferRuleDigest string            `json:"global_transfer_rule_digest,omitempty"`
}

// --- Finite-number guard ---

// utilityFiniteFloats reports the first non-finite float in a slice, if any.
func utilityFiniteFloats(values []float64) error {
	for _, v := range values {
		if v != v || v > 1.7976931348623157e308 || v < -1.7976931348623157e308 {
			return errors.New("non-finite float")
		}
	}
	return nil
}

// utilityParseStageAlias trims the enclosing type name for error messages.
func utilityTrimSpaceLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// --- US1: historical label constructor ---

// utilityHistoricalResult is the per-repetition question outcome read from an
// old hybrid results JSONL (no model call, no modern provenance guaranteed).
type utilityHistoricalResult struct {
	QuestionID string
	Conv       int
	Q          int
	Category   string
	Correct    bool
}

// utilityHistoricalPair is one repetition's shallow/deep outcome for a question.
type utilityHistoricalPair struct {
	QuestionID     string
	Conv, Q        int
	Category       string
	ShallowCorrect bool
	DeepCorrect    bool
}

// utilityHistoricalSummary aggregates question-majority utility counts.
type utilityHistoricalSummary struct {
	Benefit                        int
	Harm                           int
	Neutral                        int
	Questions                      int
	HistoricalProvenanceIncomplete bool
}

// utilityTruthTable maps a (shallow, deep) correctness pair to the counterfactual
// utility label: F→T BENEFIT(+1), T→F HARM(-1), otherwise NEUTRAL(0).
func utilityTruthTable(shallow, deep bool) (int, utilityLabelKind, error) {
	u := 0
	switch {
	case !shallow && deep:
		u = 1
	case shallow && !deep:
		u = -1
	}
	label, err := utilityLabelFromUtility(u)
	if err != nil {
		return 0, "", err
	}
	return u, label, nil
}
