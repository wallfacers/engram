package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/store"
)

const (
	evalFixedGoldOracleArtifactFile = "fixed_gold_oracle.jsonl"
	evalFixedGoldOracleSummaryFile  = "fixed_gold_oracle_summary.json"
	evalFixedGoldOracleJournalFile  = "fixed_gold_oracle_calls.jsonl"
	evalFixedGoldOracleArm          = "all_gold_evidence"
	evalFixedGoldOracleJournalKind  = "fixed_gold_call_journal"
)

// validateFixedGoldOracleMode keeps the oracle as a dedicated execution mode.
// It must run before every alternate early-return path in main so no
// retrieval, annotation, calibration, or unrelated artifact write can occur
// under a command labelled fixed-gold.
func validateFixedGoldOracleMode(opt options) error {
	if !opt.fixedGoldOracle {
		return nil
	}
	conflicts := []struct {
		enabled bool
		name    string
	}{
		{strings.TrimSpace(opt.compareSpec) != "", "--compare"},
		{strings.TrimSpace(opt.evalValidate) != "", "--eval-validate"},
		{strings.TrimSpace(opt.evalB0ProtocolPath) != "", "--eval-b0-protocol"},
		{strings.TrimSpace(opt.evalFreezeB0Protocol) != "", "--eval-freeze-b0-protocol"},
		{strings.TrimSpace(opt.evalFreezeProtocol) != "", "--eval-freeze-protocol"},
		{opt.tokenCounterCalibrate, "--token-counter-calibrate"},
		{opt.estimate, "--estimate"},
		{opt.recallDiagnostic, "--recall-diagnostic"},
		{opt.doc2queryBuild, "--doc2query-build"},
		{doc2queryEnabled(opt), "--doc2query"},
		{aliasShadowEnabled(opt), "--alias-shadow"},
		{opt.temporalDiagnostic, "--temporal-diagnostic"},
		{opt.attributionTrace, "--attribution-trace"},
		{opt.pcic || opt.oracle, "--pcic/--oracle selector"},
		{opt.pcicAnnotate, "--pcic-annotate"},
		{opt.abstainProbe, "--abstain-probe"},
		{opt.coverageOnly, "--coverage-only"},
	}
	for _, conflict := range conflicts {
		if conflict.enabled {
			return fmt.Errorf("--fixed-gold-oracle is a dedicated mode and cannot be combined with %s", conflict.name)
		}
	}
	if err := validateFormalLegacyMechanismOptions(opt); err != nil {
		return fmt.Errorf("--fixed-gold-oracle requires the plain legacy control options: %w", err)
	}
	return nil
}

// validateFixedGoldControlProtocol is shared by execution and independent
// read-back. A generic valid 022 manifest is insufficient: F0 is defined only
// as the three-repetition diagnostic derived from the frozen B1 legacy
// control, with every adaptive mechanism disabled.
func validateFixedGoldControlProtocol(protocol evalProtocol) error {
	if err := validateEvalProtocol(protocol, evalRunFormal); err != nil {
		return fmt.Errorf("invalid fixed-gold control protocol: %w", err)
	}
	controlHash, err := evalProtocolFingerprint(protocol)
	if err != nil || !isDigest(protocol.ProtocolHash) || protocol.ProtocolHash != controlHash {
		return fmt.Errorf("fixed-gold control protocol hash mismatch")
	}
	if err := validateFormalLegacyRecipe(protocol.Retrieval.Recipe); err != nil {
		return fmt.Errorf("fixed-gold control protocol: %w", err)
	}
	if protocol.Experiment.Stage != "b1" ||
		protocol.Experiment.Arm != "legacy_count_packer" ||
		strings.TrimSpace(protocol.Experiment.ControlProtocolHash) != "" ||
		protocol.Experiment.PrimaryCohort != "all" ||
		protocol.Aggregation.AnswerRepetitions != 3 ||
		protocol.Budget.RetrievalCallLimit != 1 ||
		protocol.Models.Planner.Enabled {
		return fmt.Errorf("fixed-gold oracle requires the frozen three-repetition B1 legacy control")
	}
	requiredDisabled := []string{"idk_retry", "iris", "rerank"}
	if len(protocol.Experiment.MechanismFlags) != len(requiredDisabled) {
		return fmt.Errorf("fixed-gold B1 control mechanism flags differ from the registered control")
	}
	for _, mechanism := range requiredDisabled {
		enabled, exists := protocol.Experiment.MechanismFlags[mechanism]
		if !exists || enabled {
			return fmt.Errorf("fixed-gold B1 control requires %s=false", mechanism)
		}
	}
	return nil
}

// fixedGoldEvidenceReader is deliberately the oracle's only memory dependency.
// A *memory.LedgerStore satisfies it and fails the entire GetMany call when any
// requested Evidence is missing, tombstoned, or purged.
type fixedGoldEvidenceReader interface {
	GetMany(context.Context, []string) (map[string]memory.Evidence, error)
}

// fixedGoldSkippedQuestions splits skipped dataset questions by why they cannot
// be reconstructed into a complete gold-answer input.
type fixedGoldSkippedQuestions struct {
	Empty      []string // evidence annotation is empty
	Unresolved []string // annotation references a source missing from the conversation
}

func (s fixedGoldSkippedQuestions) All() []string {
	all := make([]string, 0, len(s.Empty)+len(s.Unresolved))
	all = append(all, s.Empty...)
	all = append(all, s.Unresolved...)
	return all
}

// fixedGoldConversationDiaIDs returns the set of canonical dataset source IDs
// present in a conversation's turns (normalized the same way evidence IDs are),
// used to detect evidence annotations that reference a missing/malformed source.
func fixedGoldConversationDiaIDs(conv conversation) map[string]bool {
	diaIDs := make(map[string]bool)
	for _, session := range conv.Sessions {
		for _, t := range session.Turns {
			for _, id := range fixedGoldSplitEvidenceDatasetIDs([]string{t.DiaID}) {
				diaIDs[id] = true
			}
		}
	}
	return diaIDs
}

// fixedGoldEvidenceComplete reports whether a question's gold evidence annotation
// can be fully resolved against the conversation: non-empty and every referenced
// source exists.
func fixedGoldEvidenceComplete(diaIDs map[string]bool, qa locomoQA) bool {
	ids := fixedGoldSplitEvidenceDatasetIDs(qa.Evidence)
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if !diaIDs[id] {
			return false
		}
	}
	return true
}

// fixedGoldUnanswerableQuestionIDs returns dataset question IDs whose gold
// evidence cannot be reconstructed into a complete answer input — either the
// annotation is empty or it references a source that does not exist in the
// conversation (missing turn / malformed id). These are skipped (excluded from
// the diagnostic denominator) rather than failing the whole run. B1 is
// unaffected — it retrieves evidence from the conversation store instead of
// relying on the dataset annotation.
func fixedGoldUnanswerableQuestionIDs(convs []conversation) fixedGoldSkippedQuestions {
	var skipped fixedGoldSkippedQuestions
	for _, conv := range convs {
		diaIDs := fixedGoldConversationDiaIDs(conv)
		for _, qa := range conv.QA {
			switch {
			case len(fixedGoldSplitEvidenceDatasetIDs(qa.Evidence)) == 0:
				skipped.Empty = append(skipped.Empty, qa.QuestionID)
			case !fixedGoldEvidenceComplete(diaIDs, qa):
				skipped.Unresolved = append(skipped.Unresolved, qa.QuestionID)
			}
		}
	}
	return skipped
}

// fixedGoldFilterUnanswerableQuestions drops any selected question whose gold
// evidence cannot be fully resolved, keeping the in-memory record order aligned
// with the expected set built by buildFixedGoldExpectedQuestions.
func fixedGoldFilterUnanswerableQuestions(conv conversation, selected []selectedQuestion) []selectedQuestion {
	diaIDs := fixedGoldConversationDiaIDs(conv)
	filtered := selected[:0]
	for _, sq := range selected {
		if fixedGoldEvidenceComplete(diaIDs, sq.QA) {
			filtered = append(filtered, sq)
		}
	}
	return filtered
}

func fixedGoldContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// evalFixedGoldOracleDiagnostic is intentionally not an eval metrics artifact.
// It can diagnose whether the frozen answerer stack reaches the paper target
// when given all gold sources, but cannot be consumed by formal promotion.
type evalFixedGoldOracleDiagnostic struct {
	Schema                  string                      `json:"schema"`
	Stage                   string                      `json:"stage"`
	Arm                     string                      `json:"arm"`
	DiagnosticOnly          bool                        `json:"diagnostic_only"`
	ControlProtocolHash     string                      `json:"control_protocol_hash"`
	OracleProtocolHash      string                      `json:"oracle_protocol_hash"`
	QuestionID              string                      `json:"question_id"`
	RetrievalCalls          int                         `json:"retrieval_calls"`
	DatasetSourceIDs        []string                    `json:"dataset_source_ids,omitempty"`
	EmptyEvidenceAbstention bool                        `json:"empty_evidence_abstention,omitempty"`
	AnswerInputDigest       string                      `json:"answer_input_digest,omitempty"`
	AnswerPromptDigest      string                      `json:"answer_prompt_digest,omitempty"`
	AnswerInputTokens       int                         `json:"answer_input_tokens,omitempty"`
	CounterFingerprint      string                      `json:"counter_fingerprint,omitempty"`
	AnswerCalls             int                         `json:"answer_calls"`
	JudgeCalls              int                         `json:"judge_calls"`
	Valid                   bool                        `json:"valid"`
	InvalidReasons          []string                    `json:"invalid_reasons,omitempty"`
	RepetitionResults       []evalFixedGoldOracleRun    `json:"repetition_results,omitempty"`
	OracleDiagnostic        *evalFixedGoldOracleOutcome `json:"oracle_diagnostic,omitempty"`
}

type evalFixedGoldOracleRun struct {
	RunIndex           int    `json:"run_index"`
	Answer             string `json:"answer"`
	AnswerDigest       string `json:"answer_digest"`
	JudgeInputDigest   string `json:"judge_input_digest"`
	JudgeVerdict       string `json:"judge_verdict"`
	JudgeCorrect       bool   `json:"judge_correct"`
	JudgeVerdictDigest string `json:"judge_verdict_digest"`
	InputTokens        int    `json:"input_tokens"`
	OutputTokens       int    `json:"output_tokens"`
}

// evalFixedGoldOracleOutcome uses a one-question denominator so a caller can
// aggregate valid diagnostics without conflating answer repetitions with the
// benchmark denominator.
type evalFixedGoldOracleOutcome struct {
	Correct            int  `json:"correct"`
	Denominator        int  `json:"denominator"`
	MajorityCorrect    bool `json:"majority_correct"`
	CorrectRepetitions int  `json:"correct_repetitions"`
	Repetitions        int  `json:"repetitions"`
}

type evalFixedGoldOracleAggregate struct {
	Correct       int  `json:"correct"`
	Denominator   int  `json:"denominator"`
	TargetCorrect int  `json:"target_correct"`
	TargetMet     bool `json:"target_met"`
}

// evalFixedGoldOracleSummary intentionally has no metrics, promotion, paired,
// or verdict fields. An incomplete/invalid run omits OracleDiagnostic entirely
// so a partial percentage cannot be mistaken for a formal result.
type evalFixedGoldOracleSummary struct {
	Schema              string                        `json:"schema"`
	Stage               string                        `json:"stage"`
	Arm                 string                        `json:"arm"`
	DiagnosticOnly      bool                          `json:"diagnostic_only"`
	ControlProtocolHash string                        `json:"control_protocol_hash"`
	OracleProtocolHash  string                        `json:"oracle_protocol_hash"`
	ArtifactDigest      string                        `json:"artifact_digest"`
	CallJournalDigest   string                        `json:"call_journal_digest"`
	QuestionsExpected   int                           `json:"questions_expected"`
	QuestionsComplete   int                           `json:"questions_complete"`
	ValidQuestions      int                           `json:"valid_questions"`
	InvalidQuestions    int                           `json:"invalid_questions"`
	AnswerCalls         int                           `json:"answer_calls"`
	JudgeCalls          int                           `json:"judge_calls"`
	Valid               bool                          `json:"valid"`
	EmptyEvidenceSkipped      int `json:"empty_evidence_skipped,omitempty"`
	UnresolvedEvidenceSkipped int `json:"unresolved_evidence_skipped,omitempty"`
	InvalidReasons      []string                      `json:"invalid_reasons,omitempty"`
	OracleDiagnostic    *evalFixedGoldOracleAggregate `json:"oracle_diagnostic,omitempty"`
}

type evalFixedGoldOracleIdentity struct {
	Schema              string `json:"schema"`
	Stage               string `json:"stage"`
	Arm                 string `json:"arm"`
	DiagnosticOnly      bool   `json:"diagnostic_only"`
	ControlProtocolHash string `json:"control_protocol_hash"`
}

type evalFixedGoldOracleCallAudit struct {
	Schema              string `json:"schema"`
	Kind                string `json:"kind"`
	Stage               string `json:"stage"`
	Arm                 string `json:"arm"`
	DiagnosticOnly      bool   `json:"diagnostic_only"`
	ControlProtocolHash string `json:"control_protocol_hash"`
	OracleProtocolHash  string `json:"oracle_protocol_hash"`
	QuestionID          string `json:"question_id,omitempty"`
	RunIndex            int    `json:"run_index,omitempty"`
	Role                string `json:"role,omitempty"`
	State               string `json:"state,omitempty"`
	InputDigest         string `json:"input_digest,omitempty"`
	OutputDigest        string `json:"output_digest,omitempty"`
	Success             bool   `json:"success,omitempty"`
}

func fixedGoldOracleIdentity(protocol evalProtocol) evalFixedGoldOracleIdentity {
	return evalFixedGoldOracleIdentity{
		Schema: evalProtocolSchema, Stage: evalStageFixedGoldOracle,
		Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: protocol.ProtocolHash,
	}
}

func fixedGoldOracleProtocolHash(protocol evalProtocol) string {
	return evalJSONDigest(fixedGoldOracleIdentity(protocol))
}

// ContributesToPromotion is permanently false by type contract. The diagnostic
// has no formal metrics or verdict fields that could be mistaken for B1.
func (evalFixedGoldOracleDiagnostic) ContributesToPromotion() bool {
	return false
}

type fixedGoldOracleCallJournal struct {
	mu                  sync.Mutex
	file                *os.File
	controlProtocolHash string
	oracleProtocolHash  string
	started             map[string]bool
	terminal            map[string]bool
}

func fixedGoldCallKey(questionID string, runIndex int, role string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", questionID, runIndex, role)
}

func (journal *fixedGoldOracleCallJournal) append(record evalFixedGoldOracleCallAudit) error {
	if journal == nil || journal.file == nil {
		return fmt.Errorf("fixed-gold call journal is unavailable")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode fixed-gold call journal: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := journal.file.Write(raw); err != nil {
		return fmt.Errorf("append fixed-gold call journal: %w", err)
	}
	if err := journal.file.Sync(); err != nil {
		return fmt.Errorf("sync fixed-gold call journal: %w", err)
	}
	return nil
}

func (journal *fixedGoldOracleCallJournal) Intent(questionID string, runIndex int, role, inputDigest string) error {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	key := fixedGoldCallKey(questionID, runIndex, role)
	if journal.started[key] || journal.terminal[key] {
		return fmt.Errorf("duplicate fixed-gold call intent for %q run %d role %q", questionID, runIndex, role)
	}
	record := evalFixedGoldOracleCallAudit{
		Schema: evalProtocolSchema, Kind: evalFixedGoldOracleJournalKind,
		Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: journal.controlProtocolHash, OracleProtocolHash: journal.oracleProtocolHash,
		QuestionID: questionID, RunIndex: runIndex, Role: role, State: "intent",
		InputDigest: inputDigest,
	}
	if err := journal.append(record); err != nil {
		return err
	}
	journal.started[key] = true
	return nil
}

func (journal *fixedGoldOracleCallJournal) Terminal(questionID string, runIndex int, role, inputDigest, outputDigest string, success bool) error {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	key := fixedGoldCallKey(questionID, runIndex, role)
	if !journal.started[key] || journal.terminal[key] {
		return fmt.Errorf("fixed-gold call terminal lacks one intent for %q run %d role %q", questionID, runIndex, role)
	}
	record := evalFixedGoldOracleCallAudit{
		Schema: evalProtocolSchema, Kind: evalFixedGoldOracleJournalKind,
		Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: journal.controlProtocolHash, OracleProtocolHash: journal.oracleProtocolHash,
		QuestionID: questionID, RunIndex: runIndex, Role: role, State: "terminal",
		InputDigest: inputDigest, OutputDigest: outputDigest, Success: success,
	}
	if err := journal.append(record); err != nil {
		return err
	}
	journal.terminal[key] = true
	return nil
}

func (journal *fixedGoldOracleCallJournal) Close() error {
	if journal == nil || journal.file == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.file.Sync(); err != nil {
		_ = journal.file.Close()
		journal.file = nil
		return err
	}
	err := journal.file.Close()
	journal.file = nil
	return err
}

// runFixedGoldOracleQuestion executes the fixed-gold diagnostic with the
// control protocol's answerer, prompt, exact token counter, judge, and
// aggregation policy. It has no retriever parameter: RetrievalCalls therefore
// remains structurally zero.
func runFixedGoldOracleQuestion(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	evidenceReader fixedGoldEvidenceReader,
	evidenceByDatasetID map[string]string,
	qa locomoQA,
	answerCall usageModelCaller,
	judgeCall usageModelCaller,
) evalFixedGoldOracleDiagnostic {
	return runFixedGoldOracleQuestionWithJournal(
		ctx, protocol, opt, evidenceReader, evidenceByDatasetID, qa,
		answerCall, judgeCall, nil,
	)
}

func runFixedGoldOracleQuestionWithJournal(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	evidenceReader fixedGoldEvidenceReader,
	evidenceByDatasetID map[string]string,
	qa locomoQA,
	answerCall usageModelCaller,
	judgeCall usageModelCaller,
	callJournal *fixedGoldOracleCallJournal,
) evalFixedGoldOracleDiagnostic {
	oracleProtocolHash := fixedGoldOracleProtocolHash(protocol)
	diagnostic := evalFixedGoldOracleDiagnostic{
		Schema:              evalProtocolSchema,
		Stage:               evalStageFixedGoldOracle,
		Arm:                 evalFixedGoldOracleArm,
		DiagnosticOnly:      true,
		ControlProtocolHash: protocol.ProtocolHash,
		OracleProtocolHash:  oracleProtocolHash,
		QuestionID:          qa.QuestionID,
		RetrievalCalls:      0,
	}
	fail := func(reason string) evalFixedGoldOracleDiagnostic {
		diagnostic.Valid = false
		diagnostic.OracleDiagnostic = nil
		diagnostic.InvalidReasons = stableStrings(append(diagnostic.InvalidReasons, reason))
		return diagnostic
	}

	if err := validateFixedGoldControlProtocol(protocol); err != nil {
		return fail("control_protocol_invalid")
	}
	if protocol.Models.Answerer.PromptDigest != formalAnswerPromptDigest(opt) ||
		protocol.Models.Judge.PromptDigest != evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode())) {
		return fail("control_prompt_drift")
	}
	if strings.TrimSpace(qa.QuestionID) == "" || strings.TrimSpace(qa.Question) == "" {
		return fail("question_invalid")
	}
	if opt.formalCounter == nil {
		return fail("token_counter_unavailable")
	}
	if answerCall == nil {
		return fail("answerer_unavailable")
	}
	if judgeCall == nil {
		return fail("judge_unavailable")
	}

	evidenceIDs, evidence, reason := loadFixedGoldEvidence(ctx, protocol, evidenceReader, evidenceByDatasetID, qa)
	if reason != "" {
		return fail(reason)
	}
	diagnostic.DatasetSourceIDs = fixedGoldOrderedDatasetSourceIDs(fixedGoldSplitEvidenceDatasetIDs(qa.Evidence))
	diagnostic.EmptyEvidenceAbstention = len(evidenceIDs) == 0

	input := buildFixedGoldAnswerInput(protocol, opt, qa, evidenceIDs, evidence)
	diagnostic.AnswerInputDigest = evalJSONDigest(input)
	diagnostic.AnswerPromptDigest = evalTextDigest(input.System)

	// Admission happens only after every gold source has been rendered into the
	// one exact answer input. There is no truncation, prefix packing, or retry.
	count, err := preflightFormalAnswer(ctx, protocol, opt.formalCounter, input)
	if err != nil {
		if fixedGoldBudgetError(err) {
			return fail("answer_input_budget_impossible")
		}
		return fail("answer_input_preflight_failed")
	}
	diagnostic.AnswerInputTokens = count.InputTokens
	diagnostic.CounterFingerprint = count.Fingerprint

	correctRepetitions := 0
	for runIndex := 1; runIndex <= protocol.Aggregation.AnswerRepetitions; runIndex++ {
		if err := callJournal.Intent(qa.QuestionID, runIndex, "answer", diagnostic.AnswerInputDigest); err != nil {
			return fail("call_journal_failed")
		}
		answer, usage, answerErr := callPreflightedFormalAnswer(ctx, input, count, answerCall)
		diagnostic.AnswerCalls++
		answerDigest := evalTextDigest(answer)
		if err := callJournal.Terminal(
			qa.QuestionID, runIndex, "answer", diagnostic.AnswerInputDigest,
			answerDigest, answerErr == nil,
		); err != nil {
			return fail("call_journal_failed")
		}
		if answerErr != nil {
			fmt.Fprintf(os.Stderr, "fixed-gold oracle %s run %d answer failed: %v\n", qa.QuestionID, runIndex, answerErr)
			return fail("answer_failed")
		}
		judgeSystem := judgeSystemPromptFor(opt.judgeAlignmentMode())
		judgeUser := buildJudgePrompt(qa.Question, goldFor(qa), answer)
		judgeInputDigest := evalJSONDigest(evidencecompiler.AnswerInput{
			Model: protocol.Models.Judge.ID, System: judgeSystem, User: judgeUser,
		})
		if err := callJournal.Intent(qa.QuestionID, runIndex, "judge", judgeInputDigest); err != nil {
			return fail("call_journal_failed")
		}
		verdict, _, judgeErr := judgeCall(
			ctx,
			judgeSystem,
			judgeUser,
		)
		diagnostic.JudgeCalls++
		verdictDigest := evalTextDigest(verdict)
		if err := callJournal.Terminal(
			qa.QuestionID, runIndex, "judge", judgeInputDigest,
			verdictDigest, judgeErr == nil,
		); err != nil {
			return fail("call_journal_failed")
		}
		if judgeErr != nil {
			fmt.Fprintf(os.Stderr, "fixed-gold oracle %s run %d judge failed: %v\n", qa.QuestionID, runIndex, judgeErr)
			return fail("judge_failed")
		}
		correct := parseJudgeVerdict(verdict)
		if correct {
			correctRepetitions++
		}
		diagnostic.RepetitionResults = append(diagnostic.RepetitionResults, evalFixedGoldOracleRun{
			RunIndex:           runIndex,
			Answer:             answer,
			AnswerDigest:       answerDigest,
			JudgeInputDigest:   judgeInputDigest,
			JudgeVerdict:       verdict,
			JudgeCorrect:       correct,
			JudgeVerdictDigest: verdictDigest,
			InputTokens:        usage.InputTokens,
			OutputTokens:       usage.OutputTokens,
		})
	}

	majorityCorrect := correctRepetitions > protocol.Aggregation.AnswerRepetitions/2
	correct := 0
	if majorityCorrect {
		correct = 1
	}
	diagnostic.Valid = true
	diagnostic.OracleDiagnostic = &evalFixedGoldOracleOutcome{
		Correct:            correct,
		Denominator:        1,
		MajorityCorrect:    majorityCorrect,
		CorrectRepetitions: correctRepetitions,
		Repetitions:        protocol.Aggregation.AnswerRepetitions,
	}
	return diagnostic
}

func loadFixedGoldEvidence(
	ctx context.Context,
	protocol evalProtocol,
	reader fixedGoldEvidenceReader,
	evidenceByDatasetID map[string]string,
	qa locomoQA,
) ([]string, map[string]memory.Evidence, string) {
	if len(qa.Evidence) == 0 {
		if fixedGoldAllowsEmptyEvidence(protocol, qa) {
			return nil, map[string]memory.Evidence{}, ""
		}
		return nil, nil, "gold_evidence_empty"
	}
	if reader == nil {
		return nil, nil, "gold_evidence_unavailable"
	}
	evidenceIDs := fixedGoldSplitEvidenceDatasetIDs(qa.Evidence)
	resolved, unresolved, err := resolveDatasetSourceIDs(evidenceIDs, evidenceByDatasetID)
	if err != nil || len(unresolved) > 0 || len(resolved) == 0 {
		return nil, nil, "gold_evidence_unresolved"
	}
	sources, err := reader.GetMany(ctx, resolved)
	if err != nil {
		return nil, nil, "gold_evidence_unavailable"
	}
	for _, sourceID := range resolved {
		source, ok := sources[sourceID]
		if !ok || source.ID != sourceID {
			return nil, nil, "gold_evidence_unavailable"
		}
		if source.State != memory.EvidenceActive {
			return nil, nil, "gold_evidence_inactive"
		}
		if source.SourceType != memory.EvidenceMessage {
			return nil, nil, "gold_evidence_not_raw"
		}
		if strings.TrimSpace(source.Content) == "" || source.ContentDigest != fixedGoldRawDigest(source.Content) {
			return nil, nil, "gold_evidence_corrupt"
		}
	}
	for _, datasetID := range evidenceIDs {
		sourceID := strings.TrimSpace(evidenceByDatasetID[datasetID])
		if sourceID == "" || strings.TrimSpace(sources[sourceID].ExternalSourceID) != datasetID {
			return nil, nil, "gold_evidence_mapping_mismatch"
		}
	}
	return resolved, sources, ""
}

// fixedGoldEvidenceIDSplitRE separates packed dataset source IDs. LoCoMo
// evidence elements pack multiple IDs joined by ';' (e.g. "D8:6; D9:17") or
// spaces (e.g. "D9:1 D4:4 D4:6"); some carry a leading-zero turn like "D30:05".
var (
	fixedGoldEvidenceIDSplitRE = regexp.MustCompile(`[\s;,\x00]+`)
	fixedGoldEvidenceZeroPadRE = regexp.MustCompile(`:0+([0-9]+)$`)
)

// fixedGoldSplitEvidenceDatasetIDs normalizes dataset evidence annotations into
// one canonical dataset source ID per element: packed multi-ID elements are
// split on whitespace/';'/',' and a leading-zero turn ("D30:05") is normalized
// to "D30:5". Splitting before lineage resolution lets a packed annotation
// resolve to every underlying source instead of one unresolvable ID that would
// otherwise fail the whole oracle run.
func fixedGoldSplitEvidenceDatasetIDs(sourceIDs []string) []string {
	out := make([]string, 0, len(sourceIDs))
	for _, raw := range sourceIDs {
		for _, part := range fixedGoldEvidenceIDSplitRE.Split(raw, -1) {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			id = fixedGoldEvidenceZeroPadRE.ReplaceAllString(id, ":$1")
			out = append(out, id)
		}
	}
	return out
}

func fixedGoldOrderedDatasetSourceIDs(sourceIDs []string) []string {
	seen := make(map[string]bool, len(sourceIDs))
	ordered := make([]string, 0, len(sourceIDs))
	for _, rawID := range sourceIDs {
		id := strings.TrimSpace(rawID)
		if id != "" && !seen[id] {
			seen[id] = true
			ordered = append(ordered, id)
		}
	}
	return ordered
}

func buildFixedGoldAnswerInput(
	protocol evalProtocol,
	opt options,
	qa locomoQA,
	evidenceIDs []string,
	evidence map[string]memory.Evidence,
) evidencecompiler.AnswerInput {
	hits := make([]memory.Result, 0, len(evidenceIDs))
	for _, evidenceID := range evidenceIDs {
		source := evidence[evidenceID]
		name := source.ExternalSourceID
		if strings.TrimSpace(name) == "" {
			name = source.ID
		}
		hits = append(hits, memory.Result{
			ID: source.ID, Name: name, Content: source.Content,
			EventDate: source.OccurredAt, CreatedAt: source.RecordedAt,
			SourceSessionID: source.SourceSessionID,
		})
	}
	system := withCurrentDateRule(
		answerPromptForRegime(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt, opt.abstainPrompt),
		qa.QuestionDate,
	)
	return evidencecompiler.AnswerInput{
		Model:  protocol.Models.Answerer.ID,
		System: system,
		User: buildAnswerContextPrompt(
			qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold,
		),
	}
}

// appendFixedGoldConversationEvidence deliberately uses deterministic
// recorded_at values. Ledger IDs may differ on read-back reconstruction, but
// the ordered dataset source IDs and exact prompt-facing Evidence bytes do not.
func appendFixedGoldConversationEvidence(
	ctx context.Context,
	ledger *memory.LedgerStore,
	conv conversation,
	recordedBase time.Time,
) (map[string]string, error) {
	if ledger == nil {
		return nil, fmt.Errorf("conversation %d has no fixed-gold Evidence Ledger", conv.ID)
	}
	mapping := make(map[string]string)
	if recordedBase.IsZero() {
		recordedBase = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	recordedBase = recordedBase.UTC()
	for _, session := range conv.Sessions {
		inputs := make([]memory.EvidenceInput, 0, len(session.Turns))
		keys := make([]string, 0, len(session.Turns))
		occurredAt := benchmarkSessionOccurredAt(session)
		for turnIndex, item := range session.Turns {
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			key := benchmarkTurnSourceID(conv.ID, session.Index, turnIndex, item)
			offset := time.Duration(conv.ID+1)*24*time.Hour +
				time.Duration(session.Index)*time.Hour +
				time.Duration(turnIndex)*time.Second
			inputs = append(inputs, memory.EvidenceInput{
				ExternalSourceID: key,
				SourceType:       memory.EvidenceMessage,
				SourceSessionID:  fmt.Sprintf("conv%d-sess%d", conv.ID, session.Index),
				Speaker:          benchmarkTurnSpeaker(item),
				Ordinal:          turnIndex,
				Content:          benchmarkTurnContent(item),
				OccurredAt:       occurredAt,
				RecordedAt:       recordedBase.Add(offset),
			})
			keys = append(keys, key)
		}
		if len(inputs) == 0 {
			continue
		}
		appended, err := ledger.AppendBatch(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("append fixed-gold conversation %d session %d evidence: %w", conv.ID, session.Index, err)
		}
		for index, source := range appended {
			mapping[keys[index]] = source.ID
		}
	}
	return mapping, nil
}

func fixedGoldAllowsEmptyEvidence(protocol evalProtocol, qa locomoQA) bool {
	return protocol.Benchmark.Name == "longmemeval_s" &&
		qa.Adversarial &&
		strings.EqualFold(strings.TrimSpace(qa.QuestionType), "abstention")
}

func fixedGoldRawDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func fixedGoldBudgetError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "exceed cap")
}

type fixedGoldOracleRunFiles struct {
	artifact     *os.File
	journal      *fixedGoldOracleCallJournal
	artifactPath string
	journalPath  string
	summaryPath  string
}

func createFixedGoldOracleRunFiles(
	runDir string,
	protocol evalProtocol,
	questionsExpected int,
) (*fixedGoldOracleRunFiles, error) {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create fixed-gold run dir: %w", err)
	}
	artifactPath := filepath.Join(runDir, evalFixedGoldOracleArtifactFile)
	journalPath := filepath.Join(runDir, evalFixedGoldOracleJournalFile)
	summaryPath := filepath.Join(runDir, evalFixedGoldOracleSummaryFile)
	for _, path := range []string{artifactPath, journalPath, summaryPath} {
		if _, err := os.Lstat(path); err == nil {
			return nil, fmt.Errorf("fixed-gold oracle refuses to overwrite %s", path)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect fixed-gold oracle path %s: %w", path, err)
		}
	}

	artifact, err := os.OpenFile(artifactPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("freeze fixed-gold artifact: %w", err)
	}
	if err := artifact.Sync(); err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("sync frozen fixed-gold artifact: %w", err)
	}

	oracleHash := fixedGoldOracleProtocolHash(protocol)
	journalFile, err := os.OpenFile(journalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("freeze fixed-gold call journal: %w", err)
	}
	journal := &fixedGoldOracleCallJournal{
		file: journalFile, controlProtocolHash: protocol.ProtocolHash,
		oracleProtocolHash: oracleHash, started: map[string]bool{}, terminal: map[string]bool{},
	}
	header := evalFixedGoldOracleCallAudit{
		Schema: evalProtocolSchema, Kind: evalFixedGoldOracleJournalKind,
		Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: protocol.ProtocolHash, OracleProtocolHash: oracleHash,
		State: "header",
	}
	if err := journal.append(header); err != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, err
	}

	pending := evalFixedGoldOracleSummary{
		Schema: evalProtocolSchema, Stage: evalStageFixedGoldOracle,
		Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: protocol.ProtocolHash, OracleProtocolHash: oracleHash,
		QuestionsExpected: questionsExpected, Valid: false,
		InvalidReasons: []string{"run_incomplete"},
	}
	summaryFile, err := os.OpenFile(summaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("freeze fixed-gold summary: %w", err)
	}
	rawPending, err := json.MarshalIndent(pending, "", "  ")
	if err == nil {
		rawPending = append(rawPending, '\n')
		_, err = summaryFile.Write(rawPending)
	}
	if err == nil {
		err = summaryFile.Sync()
	}
	closeErr := summaryFile.Close()
	if err != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("write pending fixed-gold summary: %w", err)
	}
	if closeErr != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("close pending fixed-gold summary: %w", closeErr)
	}
	dir, err := os.Open(runDir)
	if err != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("open fixed-gold run directory for sync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("sync fixed-gold run directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		_ = journal.Close()
		_ = artifact.Close()
		return nil, fmt.Errorf("close fixed-gold run directory: %w", err)
	}
	return &fixedGoldOracleRunFiles{
		artifact: artifact, journal: journal,
		artifactPath: artifactPath, journalPath: journalPath, summaryPath: summaryPath,
	}, nil
}

func (files *fixedGoldOracleRunFiles) Close() error {
	if files == nil {
		return nil
	}
	var errs []string
	if files.artifact != nil {
		if err := files.artifact.Close(); err != nil {
			errs = append(errs, err.Error())
		}
		files.artifact = nil
	}
	if files.journal != nil {
		if err := files.journal.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close fixed-gold run files: %s", strings.Join(errs, "; "))
	}
	return nil
}

func writeFixedGoldOracleRecords(file *os.File, records []evalFixedGoldOracleDiagnostic) error {
	if file == nil {
		return fmt.Errorf("fixed-gold artifact is unavailable")
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("encode fixed-gold artifact: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush fixed-gold artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync fixed-gold artifact: %w", err)
	}
	return nil
}

// runFixedGoldOracleDataset is the executable dataset path. It constructs only
// a local Evidence Ledger from the supplied benchmark turns: no extractor,
// embedder, retriever, projection, filter, rewrite, or reranker is reachable.
// Questions may execute concurrently, while each question freezes one complete
// gold input and replays it for the protocol's answer repetitions.
func runFixedGoldOracleDataset(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	convs []conversation,
	answerCall usageModelCaller,
	judgeCall usageModelCaller,
) (evalFixedGoldOracleSummary, error) {
	if err := validateFixedGoldControlProtocol(protocol); err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	expectedIDs := formalQuestionIDs(opt.datasetFormat, convs)
	if len(expectedIDs) != protocol.Benchmark.QuestionCount ||
		evalJSONDigest(expectedIDs) != protocol.Benchmark.QuestionIDsDigest {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("fixed-gold oracle denominator differs from control protocol")
	}
	// The oracle is defined over gold evidence. Dataset questions that carry no
	// evidence annotation (LoCoMo has a few) cannot be reconstructed as answer
	// inputs, so they are skipped and excluded from the diagnostic denominator.
	skipped := fixedGoldUnanswerableQuestionIDs(convs)
	skippedIDs := skipped.All()
	questionsExpected := len(expectedIDs) - len(skippedIDs)
	if questionsExpected <= 0 {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("fixed-gold oracle has no questions after skipping unanswerable-evidence questions")
	}
	if len(fixedGoldOrderedDatasetSourceIDs(expectedIDs)) != len(expectedIDs) {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("fixed-gold oracle question IDs are blank or duplicated")
	}
	if strings.TrimSpace(opt.runDir) == "" {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("fixed-gold oracle requires --run-dir")
	}
	files, err := createFixedGoldOracleRunFiles(opt.runDir, protocol, questionsExpected)
	if err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = files.Close()
		}
	}()
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	records := make([]evalFixedGoldOracleDiagnostic, 0, questionsExpected)
	stopAfterConversation := false
	for _, conv := range convs {
		selected := fixedGoldFilterUnanswerableQuestions(conv, selectQuestions(conv, opt))
		if len(selected) == 0 {
			continue
		}
		st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
		if err != nil {
			return evalFixedGoldOracleSummary{}, fmt.Errorf("open fixed-gold Evidence Ledger for conversation %d: %w", conv.ID, err)
		}
		entries := memory.NewEntryStore(st.DB())
		evidenceByDatasetID, err := appendFixedGoldConversationEvidence(
			ctx, entries.Ledger(), conv, protocol.CreatedAt,
		)
		if err != nil {
			_ = st.Close()
			return evalFixedGoldOracleSummary{}, err
		}

		conversationRecords := make([]evalFixedGoldOracleDiagnostic, len(selected))
		workers := opt.concurrency
		if workers < 1 {
			workers = 1
		}
		if workers > len(selected) {
			workers = len(selected)
		}
		type fixedGoldJobResult struct {
			index  int
			record evalFixedGoldOracleDiagnostic
		}
		jobs := make(chan int, workers)
		results := make(chan fixedGoldJobResult, workers)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for index := range jobs {
					record := runFixedGoldOracleQuestionWithJournal(
						runCtx, protocol, opt, entries.Ledger(), evidenceByDatasetID,
						selected[index].QA, answerCall, judgeCall, files.journal,
					)
					results <- fixedGoldJobResult{index: index, record: record}
				}
			}()
		}

		next, inFlight := 0, 0
		schedulingFailed := false
		for next < len(selected) && inFlight < workers {
			jobs <- next
			next++
			inFlight++
		}
		for inFlight > 0 {
			result := <-results
			inFlight--
			conversationRecords[result.index] = result.record
			if !result.record.Valid && !schedulingFailed {
				schedulingFailed = true
				cancelRun()
			}
			if next < len(selected) && !schedulingFailed {
				jobs <- next
				next++
				inFlight++
			}
		}
		close(jobs)
		wg.Wait()
		scheduled := next
		if err := st.Close(); err != nil {
			return evalFixedGoldOracleSummary{}, fmt.Errorf("close fixed-gold Evidence Ledger for conversation %d: %w", conv.ID, err)
		}
		conversationRecords = conversationRecords[:scheduled]
		records = append(records, conversationRecords...)
		if scheduled != len(selected) {
			stopAfterConversation = true
		}
		for _, record := range conversationRecords {
			if !record.Valid {
				stopAfterConversation = true
				break
			}
		}
		if stopAfterConversation {
			break
		}
	}

	if err := writeFixedGoldOracleRecords(files.artifact, records); err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	if err := files.Close(); err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	closed = true

	_, summary, err := validateFixedGoldOracleReadback(
		ctx, protocol, opt, convs, files.artifactPath, files.journalPath,
	)
	if err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	if err := writeJSON(files.summaryPath, summary); err != nil {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("write fixed-gold oracle summary: %w", err)
	}
	if !summary.Valid {
		return summary, fmt.Errorf("fixed-gold oracle invalid; diagnostic score suppressed")
	}
	return summary, nil
}

type fixedGoldExpectedQuestion struct {
	QA                 locomoQA
	DatasetSourceIDs   []string
	EmptyAbstention    bool
	AnswerInputDigest  string
	AnswerPromptDigest string
	InvalidReason      string
}

func buildFixedGoldExpectedQuestions(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	convs []conversation,
) ([]fixedGoldExpectedQuestion, error) {
	expected := make([]fixedGoldExpectedQuestion, 0, protocol.Benchmark.QuestionCount)
	for _, conv := range convs {
		selected := fixedGoldFilterUnanswerableQuestions(conv, selectQuestions(conv, opt))
		if len(selected) == 0 {
			continue
		}
		st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
		if err != nil {
			return nil, fmt.Errorf("open fixed-gold validation Ledger for conversation %d: %w", conv.ID, err)
		}
		entries := memory.NewEntryStore(st.DB())
		mapping, err := appendFixedGoldConversationEvidence(
			ctx, entries.Ledger(), conv, protocol.CreatedAt,
		)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		for _, selectedQuestion := range selected {
			qa := selectedQuestion.QA
			item := fixedGoldExpectedQuestion{
				// The expected set must canonicalize dataset evidence the same way
				// the runtime record does (split packed annotations, normalize
				// leading-zero turns); otherwise the final reconstruction check
				// flags a phantom source drift on every packed annotation.
				QA: qa, DatasetSourceIDs: fixedGoldOrderedDatasetSourceIDs(fixedGoldSplitEvidenceDatasetIDs(qa.Evidence)),
				EmptyAbstention: len(qa.Evidence) == 0 && fixedGoldAllowsEmptyEvidence(protocol, qa),
			}
			evidenceIDs, evidence, reason := loadFixedGoldEvidence(
				ctx, protocol, entries.Ledger(), mapping, qa,
			)
			if reason != "" {
				item.InvalidReason = reason
			} else {
				input := buildFixedGoldAnswerInput(protocol, opt, qa, evidenceIDs, evidence)
				item.AnswerInputDigest = evalJSONDigest(input)
				item.AnswerPromptDigest = evalTextDigest(input.System)
			}
			expected = append(expected, item)
		}
		if err := st.Close(); err != nil {
			return nil, fmt.Errorf("close fixed-gold validation Ledger for conversation %d: %w", conv.ID, err)
		}
	}
	return expected, nil
}

func sameFixedGoldStrings(left, right []string) bool {
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

func validateFixedGoldExpectedRecord(
	protocol evalProtocol,
	opt options,
	expected fixedGoldExpectedQuestion,
	record evalFixedGoldOracleDiagnostic,
) error {
	if record.QuestionID != expected.QA.QuestionID {
		return fmt.Errorf("question identity drift")
	}
	if expected.InvalidReason != "" {
		return fmt.Errorf("expected gold evidence is invalid: %s", expected.InvalidReason)
	}
	if !record.Valid ||
		!sameFixedGoldStrings(record.DatasetSourceIDs, expected.DatasetSourceIDs) ||
		record.EmptyEvidenceAbstention != expected.EmptyAbstention ||
		record.AnswerInputDigest != expected.AnswerInputDigest ||
		record.AnswerPromptDigest != expected.AnswerPromptDigest {
		return fmt.Errorf("fixed-gold source or answer input drift")
	}
	for _, run := range record.RepetitionResults {
		judgeInput := evidencecompiler.AnswerInput{
			Model:  protocol.Models.Judge.ID,
			System: judgeSystemPromptFor(opt.judgeAlignmentMode()),
			User:   buildJudgePrompt(expected.QA.Question, goldFor(expected.QA), run.Answer),
		}
		if run.JudgeInputDigest != evalJSONDigest(judgeInput) {
			return fmt.Errorf("fixed-gold judge input drift")
		}
	}
	return nil
}

type fixedGoldAuditPair struct {
	intent      evalFixedGoldOracleCallAudit
	terminal    evalFixedGoldOracleCallAudit
	hasIntent   bool
	hasTerminal bool
}

func validateFixedGoldCallJournal(
	protocol evalProtocol,
	records []evalFixedGoldOracleDiagnostic,
	audits []evalFixedGoldOracleCallAudit,
) error {
	if len(audits) == 0 {
		return fmt.Errorf("fixed-gold call journal is empty")
	}
	oracleHash := fixedGoldOracleProtocolHash(protocol)
	header := audits[0]
	if header.Schema != evalProtocolSchema || header.Kind != evalFixedGoldOracleJournalKind ||
		header.Stage != evalStageFixedGoldOracle || header.Arm != evalFixedGoldOracleArm ||
		!header.DiagnosticOnly || header.ControlProtocolHash != protocol.ProtocolHash ||
		header.OracleProtocolHash != oracleHash || header.State != "header" ||
		header.QuestionID != "" || header.RunIndex != 0 || header.Role != "" {
		return fmt.Errorf("invalid fixed-gold call journal header")
	}
	byQuestion := make(map[string]evalFixedGoldOracleDiagnostic, len(records))
	for _, record := range records {
		if _, exists := byQuestion[record.QuestionID]; exists {
			return fmt.Errorf("duplicate fixed-gold question record %q", record.QuestionID)
		}
		byQuestion[record.QuestionID] = record
	}
	pairs := make(map[string]fixedGoldAuditPair)
	answerCounts := make(map[string]int)
	judgeCounts := make(map[string]int)
	for _, audit := range audits[1:] {
		if audit.Schema != evalProtocolSchema || audit.Kind != evalFixedGoldOracleJournalKind ||
			audit.Stage != evalStageFixedGoldOracle || audit.Arm != evalFixedGoldOracleArm ||
			!audit.DiagnosticOnly || audit.ControlProtocolHash != protocol.ProtocolHash ||
			audit.OracleProtocolHash != oracleHash || !isDigest(audit.InputDigest) ||
			(audit.Role != "answer" && audit.Role != "judge") ||
			audit.RunIndex < 1 || audit.RunIndex > protocol.Aggregation.AnswerRepetitions {
			return fmt.Errorf("invalid fixed-gold call audit")
		}
		if _, ok := byQuestion[audit.QuestionID]; !ok {
			return fmt.Errorf("fixed-gold call audit references unknown question %q", audit.QuestionID)
		}
		key := fixedGoldCallKey(audit.QuestionID, audit.RunIndex, audit.Role)
		pair := pairs[key]
		switch audit.State {
		case "intent":
			if pair.hasIntent || pair.hasTerminal || audit.OutputDigest != "" || audit.Success {
				return fmt.Errorf("duplicate or malformed fixed-gold call intent")
			}
			pair.intent, pair.hasIntent = audit, true
			if audit.Role == "answer" {
				answerCounts[audit.QuestionID]++
			} else {
				judgeCounts[audit.QuestionID]++
			}
		case "terminal":
			if !pair.hasIntent || pair.hasTerminal || audit.InputDigest != pair.intent.InputDigest ||
				!isDigest(audit.OutputDigest) {
				return fmt.Errorf("orphan or malformed fixed-gold call terminal")
			}
			pair.terminal, pair.hasTerminal = audit, true
		default:
			return fmt.Errorf("unknown fixed-gold call audit state %q", audit.State)
		}
		pairs[key] = pair
	}
	for _, pair := range pairs {
		if !pair.hasIntent || !pair.hasTerminal {
			return fmt.Errorf("incomplete fixed-gold call journal")
		}
	}
	for questionID, record := range byQuestion {
		if answerCounts[questionID] != record.AnswerCalls || judgeCounts[questionID] != record.JudgeCalls {
			return fmt.Errorf("fixed-gold audited call count mismatch for %q", questionID)
		}
		for _, run := range record.RepetitionResults {
			answerPair := pairs[fixedGoldCallKey(questionID, run.RunIndex, "answer")]
			judgePair := pairs[fixedGoldCallKey(questionID, run.RunIndex, "judge")]
			if !answerPair.hasTerminal || !answerPair.terminal.Success ||
				answerPair.intent.InputDigest != record.AnswerInputDigest ||
				answerPair.terminal.OutputDigest != run.AnswerDigest ||
				!judgePair.hasTerminal || !judgePair.terminal.Success ||
				judgePair.intent.InputDigest != run.JudgeInputDigest ||
				judgePair.terminal.OutputDigest != run.JudgeVerdictDigest {
				return fmt.Errorf("fixed-gold successful repetition lacks matching call audit")
			}
		}
	}
	return nil
}

func invalidateFixedGoldSummary(summary *evalFixedGoldOracleSummary, reason string) {
	summary.Valid = false
	summary.OracleDiagnostic = nil
	summary.InvalidReasons = stableStrings(append(summary.InvalidReasons, reason))
}

// validateFixedGoldOracleReadback is deliberately dataset-aware. It never
// trusts the in-memory results used by the executor: it rereads JSONL, rebuilds
// a fresh local Ledger from benchmark turns, reconstructs every answer/judge
// input, and then validates the crash-safe call journal.
func validateFixedGoldOracleReadback(
	ctx context.Context,
	protocol evalProtocol,
	opt options,
	convs []conversation,
	artifactPath string,
	journalPath string,
) ([]evalFixedGoldOracleDiagnostic, evalFixedGoldOracleSummary, error) {
	if err := validateFixedGoldControlProtocol(protocol); err != nil {
		return nil, evalFixedGoldOracleSummary{}, err
	}
	var records []evalFixedGoldOracleDiagnostic
	if err := readEvalJSONL(artifactPath, &records); err != nil {
		return nil, evalFixedGoldOracleSummary{}, fmt.Errorf("read fixed-gold oracle artifact: %w", err)
	}
	var audits []evalFixedGoldOracleCallAudit
	if err := readEvalJSONL(journalPath, &audits); err != nil {
		return nil, evalFixedGoldOracleSummary{}, fmt.Errorf("read fixed-gold call journal: %w", err)
	}
	expected, err := buildFixedGoldExpectedQuestions(ctx, protocol, opt, convs)
	if err != nil {
		return nil, evalFixedGoldOracleSummary{}, err
	}
	expectedIDs := make([]string, 0, len(expected))
	for _, item := range expected {
		expectedIDs = append(expectedIDs, item.QA.QuestionID)
	}
	summary := summarizeFixedGoldOracle(protocol, expectedIDs, records)
	rawArtifact, err := os.ReadFile(artifactPath) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return nil, evalFixedGoldOracleSummary{}, fmt.Errorf("read fixed-gold artifact digest: %w", err)
	}
	summary.ArtifactDigest = evalTextDigest(string(rawArtifact))
	rawJournal, err := os.ReadFile(journalPath) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return nil, evalFixedGoldOracleSummary{}, fmt.Errorf("read fixed-gold call journal digest: %w", err)
	}
	summary.CallJournalDigest = evalTextDigest(string(rawJournal))
	fullIDs := formalQuestionIDs(opt.datasetFormat, convs)
	if len(fullIDs) != protocol.Benchmark.QuestionCount ||
		evalJSONDigest(fullIDs) != protocol.Benchmark.QuestionIDsDigest {
		invalidateFixedGoldSummary(&summary, "denominator_or_order_drift")
	}
	skipped := fixedGoldUnanswerableQuestionIDs(convs)
	skippedIDs := skipped.All()
	summary.EmptyEvidenceSkipped = len(skipped.Empty)
	summary.UnresolvedEvidenceSkipped = len(skipped.Unresolved)
	if len(expectedIDs)+len(skippedIDs) != len(fullIDs) {
		invalidateFixedGoldSummary(&summary, "denominator_or_order_drift")
	}
	for _, skippedID := range skippedIDs {
		if !fixedGoldContainsString(fullIDs, skippedID) {
			invalidateFixedGoldSummary(&summary, "denominator_or_order_drift")
		}
	}
	for _, expectedID := range expectedIDs {
		if fixedGoldContainsString(skippedIDs, expectedID) {
			invalidateFixedGoldSummary(&summary, "denominator_or_order_drift")
		}
	}
	if protocol.Models.Answerer.PromptDigest != formalAnswerPromptDigest(opt) ||
		protocol.Models.Judge.PromptDigest != evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode())) {
		invalidateFixedGoldSummary(&summary, "control_prompt_drift")
	}
	for index, record := range records {
		if index >= len(expected) {
			invalidateFixedGoldSummary(&summary, "question_identity_drift")
			break
		}
		if err := validateFixedGoldExpectedRecord(protocol, opt, expected[index], record); err != nil {
			invalidateFixedGoldSummary(&summary, "question_reconstruction_invalid")
		}
	}
	if err := validateFixedGoldCallJournal(protocol, records, audits); err != nil {
		invalidateFixedGoldSummary(&summary, "call_journal_invalid")
	}
	return records, summary, nil
}

func fixedGoldOracleArtifactsPresent(runDir string) bool {
	for _, name := range []string{
		evalFixedGoldOracleArtifactFile,
		evalFixedGoldOracleJournalFile,
		evalFixedGoldOracleSummaryFile,
	} {
		if _, err := os.Lstat(filepath.Join(runDir, name)); err == nil {
			return true
		}
	}
	return false
}

// runFixedGoldOracleValidateCLI is the independently executable, no-model
// read-back path. The dataset is required because validating only artifact
// self-consistency cannot prove that the oracle used every and only gold turn.
func runFixedGoldOracleValidateCLI(ctx context.Context, opt options) error {
	if strings.TrimSpace(opt.evalValidate) == "" || strings.TrimSpace(opt.dataPath) == "" {
		return fmt.Errorf("fixed-gold --eval-validate requires a run directory and --data")
	}
	if opt.datasetFormat != "locomo" && opt.datasetFormat != "longmemeval" {
		return fmt.Errorf("--dataset-format must be locomo or longmemeval, got %q", opt.datasetFormat)
	}
	protocol, err := readFrozenEvalProtocol(opt.evalValidate)
	if err != nil {
		return err
	}
	if err := validateFixedGoldControlProtocol(protocol); err != nil {
		return err
	}
	prepared, err := prepareFrozenEvalOptions(protocol, opt)
	if err != nil {
		return err
	}
	prepared.fixedGoldOracle = true
	arms, err := armsFor(prepared.retrieval)
	if err != nil {
		return err
	}
	if err := validateFormalRunnerOptions(protocol, prepared, arms); err != nil {
		return err
	}
	convs, err := loadBenchmarkDataset(prepared.dataPath, prepared.datasetFormat, prepared.imageCaptions)
	if err != nil {
		return err
	}
	if err := verifyFormalDataset(protocol, prepared.dataPath, prepared.datasetFormat, convs); err != nil {
		return err
	}
	derived, err := validateFixedGoldOracleRunDirectory(ctx, prepared.evalValidate, protocol, prepared, convs)
	if err != nil {
		return err
	}
	fmt.Printf(
		"eval-validate: oracle_protocol=%s questions=%d valid=true diagnostic_only=true\n",
		derived.OracleProtocolHash,
		derived.QuestionsComplete,
	)
	return nil
}

func validateFixedGoldOracleRunDirectory(
	ctx context.Context,
	runDir string,
	protocol evalProtocol,
	opt options,
	convs []conversation,
) (evalFixedGoldOracleSummary, error) {
	_, derived, err := validateFixedGoldOracleReadback(
		ctx,
		protocol,
		opt,
		convs,
		filepath.Join(runDir, evalFixedGoldOracleArtifactFile),
		filepath.Join(runDir, evalFixedGoldOracleJournalFile),
	)
	if err != nil {
		return evalFixedGoldOracleSummary{}, err
	}
	rawSummary, err := os.ReadFile(filepath.Join(runDir, evalFixedGoldOracleSummaryFile)) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("read fixed-gold oracle summary: %w", err)
	}
	var persisted evalFixedGoldOracleSummary
	if err := json.Unmarshal(rawSummary, &persisted); err != nil {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("decode fixed-gold oracle summary: %w", err)
	}
	if evalJSONDigest(persisted) != evalJSONDigest(derived) {
		return evalFixedGoldOracleSummary{}, fmt.Errorf("fixed-gold oracle persisted summary differs from independent read-back")
	}
	if !derived.Valid || derived.OracleDiagnostic == nil {
		return derived, fmt.Errorf("fixed-gold oracle artifacts are INVALID; diagnostic score suppressed")
	}
	return derived, nil
}

func summarizeFixedGoldOracle(
	protocol evalProtocol,
	expectedIDs []string,
	records []evalFixedGoldOracleDiagnostic,
) evalFixedGoldOracleSummary {
	summary := evalFixedGoldOracleSummary{
		Schema:              evalProtocolSchema,
		Stage:               evalStageFixedGoldOracle,
		Arm:                 evalFixedGoldOracleArm,
		DiagnosticOnly:      true,
		ControlProtocolHash: protocol.ProtocolHash,
		OracleProtocolHash:  fixedGoldOracleProtocolHash(protocol),
		QuestionsExpected:   len(expectedIDs),
		QuestionsComplete:   len(records),
	}
	if len(records) != len(expectedIDs) {
		summary.InvalidReasons = append(summary.InvalidReasons, "denominator_incomplete")
	}
	correct := 0
	for index, record := range records {
		summary.AnswerCalls += record.AnswerCalls
		summary.JudgeCalls += record.JudgeCalls
		if index >= len(expectedIDs) || record.QuestionID != expectedIDs[index] {
			summary.InvalidQuestions++
			summary.InvalidReasons = append(summary.InvalidReasons, "question_identity_drift")
			continue
		}
		if err := validateFixedGoldOracleDiagnostic(protocol, record); err != nil {
			summary.InvalidQuestions++
			summary.InvalidReasons = append(summary.InvalidReasons, "question_artifact_invalid")
			continue
		}
		summary.ValidQuestions++
		if record.OracleDiagnostic.MajorityCorrect {
			correct++
		}
	}
	summary.InvalidReasons = stableStrings(summary.InvalidReasons)
	summary.Valid = len(summary.InvalidReasons) == 0 &&
		summary.QuestionsComplete == summary.QuestionsExpected &&
		summary.ValidQuestions == summary.QuestionsExpected &&
		summary.InvalidQuestions == 0
	if summary.Valid {
		target := fixedGoldTargetCorrect(protocol.Benchmark.Name)
		summary.OracleDiagnostic = &evalFixedGoldOracleAggregate{
			Correct:       correct,
			Denominator:   summary.QuestionsExpected,
			TargetCorrect: target,
			TargetMet:     correct >= target,
		}
	}
	return summary
}

func validateFixedGoldOracleDiagnostic(protocol evalProtocol, record evalFixedGoldOracleDiagnostic) error {
	if record.Schema != evalProtocolSchema ||
		record.Stage != evalStageFixedGoldOracle ||
		record.Arm != evalFixedGoldOracleArm ||
		!record.DiagnosticOnly ||
		record.ControlProtocolHash != protocol.ProtocolHash ||
		record.OracleProtocolHash != fixedGoldOracleProtocolHash(protocol) ||
		record.RetrievalCalls != 0 ||
		!record.Valid ||
		len(record.InvalidReasons) != 0 ||
		record.OracleDiagnostic == nil ||
		record.OracleDiagnostic.Denominator != 1 ||
		record.OracleDiagnostic.Repetitions != protocol.Aggregation.AnswerRepetitions ||
		record.AnswerCalls != protocol.Aggregation.AnswerRepetitions ||
		record.JudgeCalls != protocol.Aggregation.AnswerRepetitions ||
		len(record.RepetitionResults) != protocol.Aggregation.AnswerRepetitions ||
		record.AnswerInputTokens < 1 ||
		record.AnswerInputTokens > protocol.Budget.AnswerInputTokenCap ||
		record.CounterFingerprint != protocol.Budget.CounterFingerprint ||
		!isDigest(record.AnswerInputDigest) ||
		!isDigest(record.AnswerPromptDigest) {
		return fmt.Errorf("invalid fixed-gold oracle question artifact")
	}
	if record.EmptyEvidenceAbstention {
		if protocol.Benchmark.Name != "longmemeval_s" || len(record.DatasetSourceIDs) != 0 {
			return fmt.Errorf("invalid fixed-gold empty-evidence abstention artifact")
		}
	} else if len(record.DatasetSourceIDs) == 0 {
		return fmt.Errorf("fixed-gold oracle question has no source IDs")
	}
	if len(stableStrings(record.DatasetSourceIDs)) != len(record.DatasetSourceIDs) {
		return fmt.Errorf("fixed-gold oracle source IDs are blank or duplicated")
	}
	correct := 0
	for index, run := range record.RepetitionResults {
		if run.RunIndex != index+1 || run.AnswerDigest != evalTextDigest(run.Answer) ||
			!isDigest(run.JudgeInputDigest) ||
			run.JudgeVerdictDigest != evalTextDigest(run.JudgeVerdict) ||
			run.JudgeCorrect != parseJudgeVerdict(run.JudgeVerdict) ||
			run.InputTokens != record.AnswerInputTokens || run.OutputTokens < 0 {
			return fmt.Errorf("invalid fixed-gold oracle repetition")
		}
		if run.JudgeCorrect {
			correct++
		}
	}
	majority := correct > protocol.Aggregation.AnswerRepetitions/2
	if correct != record.OracleDiagnostic.CorrectRepetitions ||
		majority != record.OracleDiagnostic.MajorityCorrect ||
		record.OracleDiagnostic.Correct != boolInt(majority) {
		return fmt.Errorf("fixed-gold oracle majority mismatch")
	}
	return nil
}

func fixedGoldTargetCorrect(benchmark string) int {
	switch benchmark {
	case "locomo":
		return 1425
	case "longmemeval_s":
		return 473
	default:
		return 0
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
