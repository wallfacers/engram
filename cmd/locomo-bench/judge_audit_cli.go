package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// judgeAuditArmInput is the operational bridge from persisted answer journals
// to the already-frozen selection and adjudication rules in judge_audit.go.
// Runs are independent answer repetitions for one benchmark arm.
type judgeAuditArmInput struct {
	Benchmark string
	Arm       string
	Runs      [][]result
}

type operationalJudgeAuditPacket struct {
	PacketID   string `json:"packet_id"`
	QuestionID string `json:"question_id"`
	Benchmark  string `json:"benchmark"`
	Category   string `json:"category"`
	Question   string `json:"question"`
	Gold       string `json:"gold"`
	Answer     string `json:"answer"`
}

// operationalJudgeAuditKey is kept away from reviewer packets. Arm identity
// and the raw judge label are intentionally absent from the blinded packet.
type operationalJudgeAuditKey struct {
	PacketID        string `json:"packet_id"`
	QuestionID      string `json:"question_id"`
	Arm             string `json:"arm"`
	RawJudgeCorrect *bool  `json:"raw_judge_correct"`
}

type operationalJudgeAuditPreparation struct {
	Plan       evalJudgeAuditPlan            `json:"plan"`
	Selections []evalJudgeAuditSelection     `json:"selections"`
	Packets    []operationalJudgeAuditPacket `json:"packets"`
	Key        []operationalJudgeAuditKey    `json:"key"`
}

type operationalJudgeAuditReview struct {
	PacketID string `json:"packet_id"`
	Reviewer string `json:"reviewer"`
	Correct  bool   `json:"correct"`
	Reason   string `json:"reason"`
}

type operationalJudgeAuditDecision struct {
	PacketID string `json:"packet_id"`
	Correct  bool   `json:"correct"`
	Reason   string `json:"reason"`
}

type operationalJudgeAuditResult struct {
	PacketID          string `json:"packet_id"`
	QuestionID        string `json:"question_id"`
	Arm               string `json:"arm"`
	RawJudgeCorrect   bool   `json:"raw_judge_correct"`
	Correct           bool   `json:"correct"`
	ReviewerAgreement bool   `json:"reviewer_agreement"`
	Adjudicated       bool   `json:"adjudicated"`
}

type operationalJudgeAuditCompletion struct {
	Results      []operationalJudgeAuditResult `json:"results"`
	Summary      evalJudgeAuditSummary         `json:"summary"`
	Verdict      operationalJudgeAuditVerdict  `json:"verdict"`
	ProtocolHash string                        `json:"protocol_hash,omitempty"`
	ArtifactHash string                        `json:"artifact_hash,omitempty"`
}

type judgeAuditQuestionOutcome struct {
	QuestionID string
	Category   string
	Question   string
	Gold       string
	Answer     string
	Correct    bool
}

func prepareOperationalJudgeAudit(plan evalJudgeAuditPlan, control, treatment judgeAuditArmInput) (operationalJudgeAuditPreparation, error) {
	if strings.TrimSpace(control.Benchmark) == "" || control.Benchmark != treatment.Benchmark {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("judge audit arms require one non-empty benchmark")
	}
	if strings.TrimSpace(control.Arm) == "" || strings.TrimSpace(treatment.Arm) == "" || control.Arm == treatment.Arm {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("judge audit arms must be distinct and non-empty")
	}
	controlOutcomes, err := summarizeJudgeAuditArm(control)
	if err != nil {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("control arm: %w", err)
	}
	treatmentOutcomes, err := summarizeJudgeAuditArm(treatment)
	if err != nil {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("treatment arm: %w", err)
	}
	if len(controlOutcomes) != len(treatmentOutcomes) {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("judge audit arm question counts differ")
	}

	questionIDs := make([]string, 0, len(controlOutcomes))
	questions := make([]evalJudgeAuditQuestion, 0, len(controlOutcomes))
	for questionID, controlOutcome := range controlOutcomes {
		treatmentOutcome, ok := treatmentOutcomes[questionID]
		if !ok {
			return operationalJudgeAuditPreparation{}, fmt.Errorf("treatment arm is missing question %q", questionID)
		}
		if controlOutcome.Category != treatmentOutcome.Category || controlOutcome.Question != treatmentOutcome.Question || controlOutcome.Gold != treatmentOutcome.Gold {
			return operationalJudgeAuditPreparation{}, fmt.Errorf("question %q metadata differs between arms", questionID)
		}
		questionIDs = append(questionIDs, questionID)
		questions = append(questions, evalJudgeAuditQuestion{
			QuestionID: questionID, Benchmark: control.Benchmark, Category: controlOutcome.Category,
			ControlCorrect: controlOutcome.Correct, TreatmentCorrect: treatmentOutcome.Correct,
		})
	}
	sort.Strings(questionIDs)
	selections, err := selectJudgeAuditQuestions(plan, questions)
	if err != nil {
		return operationalJudgeAuditPreparation{}, err
	}

	prepared := operationalJudgeAuditPreparation{Plan: plan, Selections: selections}
	for _, selection := range selections {
		for _, arm := range []struct {
			name    string
			outcome judgeAuditQuestionOutcome
		}{
			{name: control.Arm, outcome: controlOutcomes[selection.QuestionID]},
			{name: treatment.Arm, outcome: treatmentOutcomes[selection.QuestionID]},
		} {
			packetID := "audit:" + auditSelectionDigest(plan.Seed, selection.QuestionID+"\x00"+arm.name)
			prepared.Packets = append(prepared.Packets, operationalJudgeAuditPacket{
				PacketID: packetID, QuestionID: selection.QuestionID, Benchmark: control.Benchmark,
				Category: arm.outcome.Category, Question: arm.outcome.Question, Gold: arm.outcome.Gold,
				Answer: arm.outcome.Answer,
			})
			raw := arm.outcome.Correct
			prepared.Key = append(prepared.Key, operationalJudgeAuditKey{
				PacketID: packetID, QuestionID: selection.QuestionID, Arm: arm.name, RawJudgeCorrect: &raw,
			})
		}
	}
	sort.Slice(prepared.Packets, func(i, j int) bool { return prepared.Packets[i].PacketID < prepared.Packets[j].PacketID })
	sort.Slice(prepared.Key, func(i, j int) bool { return prepared.Key[i].PacketID < prepared.Key[j].PacketID })
	return prepared, nil
}

func summarizeJudgeAuditArm(input judgeAuditArmInput) (map[string]judgeAuditQuestionOutcome, error) {
	if len(input.Runs) == 0 || len(input.Runs)%2 == 0 {
		return nil, fmt.Errorf("judge audit requires an odd non-zero repetition count")
	}
	byQuestion := make(map[string][]result)
	for runIndex, run := range input.Runs {
		seen := make(map[string]bool)
		for _, item := range run {
			if strings.TrimSpace(item.QuestionID) == "" || seen[item.QuestionID] {
				return nil, fmt.Errorf("run %d has an empty or duplicate question ID", runIndex+1)
			}
			seen[item.QuestionID] = true
			byQuestion[item.QuestionID] = append(byQuestion[item.QuestionID], item)
		}
	}
	outcomes := make(map[string]judgeAuditQuestionOutcome, len(byQuestion))
	for questionID, items := range byQuestion {
		if len(items) != len(input.Runs) {
			return nil, fmt.Errorf("question %q is incomplete across repetitions", questionID)
		}
		base := items[0]
		labels := make([]bool, len(items))
		for index, item := range items {
			if item.Question != base.Question || item.Gold != base.Gold || item.CategoryName != base.CategoryName {
				return nil, fmt.Errorf("question %q metadata drifts across repetitions", questionID)
			}
			labels[index] = item.Correct
		}
		majority, err := majorityCorrectness(labels)
		if err != nil {
			return nil, err
		}
		answers := make([]string, 0, len(items))
		for _, item := range items {
			if item.Correct == majority && strings.TrimSpace(item.Predicted) != "" {
				answers = append(answers, item.Predicted)
			}
		}
		if len(answers) == 0 {
			return nil, fmt.Errorf("question %q has no representative majority answer", questionID)
		}
		sort.Strings(answers)
		outcomes[questionID] = judgeAuditQuestionOutcome{
			QuestionID: questionID, Category: base.CategoryName, Question: base.Question,
			Gold: base.Gold, Answer: answers[0], Correct: majority,
		}
	}
	return outcomes, nil
}

func finalizeOperationalJudgeAudit(prepared operationalJudgeAuditPreparation, reviews []operationalJudgeAuditReview, decisions []operationalJudgeAuditDecision) (operationalJudgeAuditCompletion, error) {
	packetIDs := make(map[string]bool, len(prepared.Packets))
	for _, packet := range prepared.Packets {
		if strings.TrimSpace(packet.PacketID) == "" || packetIDs[packet.PacketID] {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("prepared audit has an empty or duplicate packet ID")
		}
		packetIDs[packet.PacketID] = true
	}
	keyByPacket := make(map[string]operationalJudgeAuditKey, len(prepared.Key))
	for _, key := range prepared.Key {
		if !packetIDs[key.PacketID] || key.RawJudgeCorrect == nil {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("prepared audit key is incomplete for packet %q", key.PacketID)
		}
		if _, duplicate := keyByPacket[key.PacketID]; duplicate {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("prepared audit repeats key %q", key.PacketID)
		}
		keyByPacket[key.PacketID] = key
	}
	if len(keyByPacket) != len(packetIDs) {
		return operationalJudgeAuditCompletion{}, fmt.Errorf("prepared audit packet/key counts differ")
	}

	reviewsByPacket := make(map[string][]operationalJudgeAuditReview)
	reviewerSet := make(map[string]bool)
	for _, review := range reviews {
		if !packetIDs[review.PacketID] || strings.TrimSpace(review.Reviewer) == "" || strings.TrimSpace(review.Reason) == "" {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("invalid review for packet %q", review.PacketID)
		}
		reviewerSet[review.Reviewer] = true
		reviewsByPacket[review.PacketID] = append(reviewsByPacket[review.PacketID], review)
	}
	if len(reviewerSet) != 2 {
		return operationalJudgeAuditCompletion{}, fmt.Errorf("judge audit requires exactly two independent reviewer identities")
	}
	decisionByPacket := make(map[string]operationalJudgeAuditDecision)
	for _, decision := range decisions {
		if !packetIDs[decision.PacketID] || strings.TrimSpace(decision.Reason) == "" {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("invalid adjudication for packet %q", decision.PacketID)
		}
		if _, duplicate := decisionByPacket[decision.PacketID]; duplicate {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("duplicate adjudication for packet %q", decision.PacketID)
		}
		decisionByPacket[decision.PacketID] = decision
	}

	packetOrder := make([]string, 0, len(packetIDs))
	for packetID := range packetIDs {
		packetOrder = append(packetOrder, packetID)
	}
	sort.Strings(packetOrder)
	completion := operationalJudgeAuditCompletion{}
	summaryInputs := make([]evalJudgeAuditResult, 0, len(packetOrder))
	for _, packetID := range packetOrder {
		items := reviewsByPacket[packetID]
		if len(items) != 2 || items[0].Reviewer == items[1].Reviewer {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("packet %q requires one decision from each reviewer", packetID)
		}
		agreement := items[0].Correct == items[1].Correct
		correct := items[0].Correct
		adjudicated := false
		if !agreement {
			decision, ok := decisionByPacket[packetID]
			if !ok {
				return operationalJudgeAuditCompletion{}, fmt.Errorf("packet %q requires adjudication", packetID)
			}
			correct = decision.Correct
			adjudicated = true
		} else if _, unexpected := decisionByPacket[packetID]; unexpected {
			return operationalJudgeAuditCompletion{}, fmt.Errorf("packet %q has unnecessary adjudication", packetID)
		}
		key := keyByPacket[packetID]
		completion.Results = append(completion.Results, operationalJudgeAuditResult{
			PacketID: packetID, QuestionID: key.QuestionID, Arm: key.Arm,
			RawJudgeCorrect: *key.RawJudgeCorrect, Correct: correct,
			ReviewerAgreement: agreement, Adjudicated: adjudicated,
		})
		summaryInputs = append(summaryInputs, evalJudgeAuditResult{
			QuestionID: packetID, RawJudgeCorrect: *key.RawJudgeCorrect,
			AdjudicatedCorrect: correct, ReviewerAgreement: agreement,
		})
	}
	summary, err := summarizeJudgeAuditResults(summaryInputs)
	if err != nil {
		return operationalJudgeAuditCompletion{}, err
	}
	completion.Summary = summary
	return completion, nil
}

// judgeAuditDirName holds the judge-audit workflow artifacts of one run
// directory. packets.json is the blinded reviewer deliverable; key.json is
// the private arm/raw-label ledger kept apart from it; prepared.json carries
// the full plan+selections+packets+key state for the finalize step.
const judgeAuditDirName = "judge-audit"

const (
	judgeAuditPacketsFile    = "packets.json"
	judgeAuditKeyFile        = "key.json"
	judgeAuditPreparedFile   = "prepared.json"
	judgeAuditCompletionFile = "completion.json"
)

// judgeAuditAccuracyGate is the audit-scoped promotion threshold: an arm's
// (raw or corrected) accuracy at or above Accuracy maps to GO, below to HOLD.
// The gate is declared per run and recorded in the completion artifact so the
// verdict-change check is reproducible.
type judgeAuditAccuracyGate struct {
	Accuracy float64 `json:"accuracy"`
}

// operationalJudgeAuditVerdict reports whether judge-audit correction moved
// the arm across the accuracy gate (raw judge accuracy vs adjudicated
// accuracy). Changed is evaluated with judgeAuditChangesVerdict so identical
// verdicts never report a change.
type operationalJudgeAuditVerdict struct {
	Raw       evalVerdict `json:"raw_verdict"`
	Corrected evalVerdict `json:"corrected_verdict"`
	Changed   bool        `json:"verdict_changed"`
}

func judgeAuditVerdictFor(accuracy float64, gate judgeAuditAccuracyGate) evalVerdict {
	if gate.Accuracy <= 0 || gate.Accuracy > 1 {
		return evalVerdictInvalid
	}
	if accuracy >= gate.Accuracy {
		return evalVerdictGO
	}
	return evalVerdictHOLD
}

func judgeAuditVerdictForSummary(summary evalJudgeAuditSummary, gate judgeAuditAccuracyGate) operationalJudgeAuditVerdict {
	raw := judgeAuditVerdictFor(summary.RawAccuracy, gate)
	corrected := judgeAuditVerdictFor(summary.CorrectedAccuracy, gate)
	return operationalJudgeAuditVerdict{
		Raw:       raw,
		Corrected: corrected,
		Changed:   judgeAuditChangesVerdict(raw, corrected),
	}
}

// writeJudgeAuditPreparation persists a prepared audit as three separate
// files under runDir/judge-audit: blinded packets (reviewer-facing), private
// key (arm/raw labels), and the full preparation for the finalize step.
// It returns the three file paths.
func writeJudgeAuditPreparation(runDir string, prepared operationalJudgeAuditPreparation) (string, string, string, error) {
	auditDir := filepath.Join(runDir, judgeAuditDirName)
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create judge-audit dir: %w", err)
	}
	packetsPath := filepath.Join(auditDir, judgeAuditPacketsFile)
	keyPath := filepath.Join(auditDir, judgeAuditKeyFile)
	preparedPath := filepath.Join(auditDir, judgeAuditPreparedFile)
	if err := writeJSON(packetsPath, prepared.Packets); err != nil {
		return "", "", "", fmt.Errorf("write blinded packets: %w", err)
	}
	if err := writeJSON(keyPath, prepared.Key); err != nil {
		return "", "", "", fmt.Errorf("write private key: %w", err)
	}
	if err := writeJSON(preparedPath, prepared); err != nil {
		return "", "", "", fmt.Errorf("write prepared audit: %w", err)
	}
	return packetsPath, keyPath, preparedPath, nil
}

// loadJudgeAuditPreparation restores the full preparation previously written
// by writeJudgeAuditPreparation. A missing or corrupt prepared.json is a hard
// error: finalize must never run against a partial state.
func loadJudgeAuditPreparation(runDir string) (operationalJudgeAuditPreparation, error) {
	path := filepath.Join(runDir, judgeAuditDirName, judgeAuditPreparedFile)
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("read prepared audit: %w", err)
	}
	var prepared operationalJudgeAuditPreparation
	if err := json.Unmarshal(raw, &prepared); err != nil {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("decode prepared audit: %w", err)
	}
	if len(prepared.Packets) == 0 || len(prepared.Key) == 0 {
		return operationalJudgeAuditPreparation{}, fmt.Errorf("prepared audit is empty or incomplete")
	}
	return prepared, nil
}

// loadJudgeAuditReviews imports reviewer decisions from a JSON file. Any
// decode or identity error is fail-closed.
func loadJudgeAuditReviews(path string) ([]operationalJudgeAuditReview, error) {
	var reviews []operationalJudgeAuditReview
	if err := readJSON(path, &reviews); err != nil {
		return nil, fmt.Errorf("read reviews %s: %w", path, err)
	}
	return reviews, nil
}

// loadJudgeAuditDecisions imports adjudications from a JSON file. The file is
// optional (agreement on every packet needs no adjudication); an empty path
// yields an empty decision list.
func loadJudgeAuditDecisions(path string) ([]operationalJudgeAuditDecision, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	var decisions []operationalJudgeAuditDecision
	if err := readJSON(path, &decisions); err != nil {
		return nil, fmt.Errorf("read decisions %s: %w", path, err)
	}
	return decisions, nil
}

// runJudgeAuditPrepareCLI implements --judge-audit-prepare: it loads the
// control/treatment answer journals (auditRepeats odd repetitions), generates
// the blinded reviewer packets plus private key from the frozen selection
// rules, and persists them under runDir/judge-audit. No model calls.
func runJudgeAuditPrepareCLI(opt options) error {
	if strings.TrimSpace(opt.auditControlArm) == "" || strings.TrimSpace(opt.auditTreatmentArm) == "" {
		return fmt.Errorf("judge-audit prepare requires --audit-control-arm and --audit-treatment-arm")
	}
	if opt.auditRepeats < 1 || opt.auditRepeats%2 == 0 {
		return fmt.Errorf("--audit-repeats must be an odd positive repetition count")
	}
	if strings.TrimSpace(opt.auditPlanSeed) == "" {
		return fmt.Errorf("--audit-plan-seed is required for deterministic sampling")
	}
	benchmark := strings.TrimSpace(opt.auditBenchmark)
	if benchmark == "" {
		benchmark = "locomo"
	}
	controlRuns, err := loadArmRuns(opt.runDir, opt.auditControlArm, opt.auditRepeats)
	if err != nil {
		return fmt.Errorf("load control arm: %w", err)
	}
	treatmentRuns, err := loadArmRuns(opt.runDir, opt.auditTreatmentArm, opt.auditRepeats)
	if err != nil {
		return fmt.Errorf("load treatment arm: %w", err)
	}
	plan := evalJudgeAuditPlan{
		AllDiscordant:        true,
		ConcordantPerStratum: opt.auditConcordantPerStratum,
		Seed:                 opt.auditPlanSeed,
	}
	prepared, err := prepareOperationalJudgeAudit(plan, judgeAuditArmInput{
		Benchmark: benchmark, Arm: opt.auditControlArm, Runs: controlRuns,
	}, judgeAuditArmInput{
		Benchmark: benchmark, Arm: opt.auditTreatmentArm, Runs: treatmentRuns,
	})
	if err != nil {
		return fmt.Errorf("prepare judge audit: %w", err)
	}
	packetsPath, keyPath, preparedPath, err := writeJudgeAuditPreparation(opt.runDir, prepared)
	if err != nil {
		return err
	}
	fmt.Printf("judge-audit-prepare: packets=%d key=%d selections=%d\n", len(prepared.Packets), len(prepared.Key), len(prepared.Selections))
	fmt.Printf("judge-audit-prepare: %s\n%s\n%s\n", packetsPath, keyPath, preparedPath)
	return nil
}

// runJudgeAuditFinalizeCLI implements --judge-audit-finalize: it imports the
// two independent reviewer files (plus optional adjudications), computes the
// raw/corrected summary and verdict change against the accuracy gate, binds
// the run's protocol and artifact hashes, and writes completion.json. No
// model calls.
func runJudgeAuditFinalizeCLI(opt options) error {
	if strings.TrimSpace(opt.auditReviews) == "" {
		return fmt.Errorf("judge-audit finalize requires --audit-reviews")
	}
	if opt.auditAccuracyGate <= 0 || opt.auditAccuracyGate > 1 {
		return fmt.Errorf("--audit-accuracy-gate must be in (0,1]")
	}
	prepared, err := loadJudgeAuditPreparation(opt.runDir)
	if err != nil {
		return err
	}
	reviews, err := loadJudgeAuditReviews(opt.auditReviews)
	if err != nil {
		return err
	}
	decisions, err := loadJudgeAuditDecisions(opt.auditDecisions)
	if err != nil {
		return err
	}
	completion, err := finalizeOperationalJudgeAudit(prepared, reviews, decisions)
	if err != nil {
		return fmt.Errorf("finalize judge audit: %w", err)
	}
	completion.Verdict = judgeAuditVerdictForSummary(completion.Summary, judgeAuditAccuracyGate{Accuracy: opt.auditAccuracyGate})
	if completion.ProtocolHash, err = readEvalProtocolHash(opt.runDir); err != nil {
		return err
	}
	artifactHashes, err := evalArtifactFileHashes(opt.runDir)
	if err != nil {
		return fmt.Errorf("hash run artifacts: %w", err)
	}
	if completion.ArtifactHash, err = aggregateEvalArtifactHash(artifactHashes); err != nil {
		return err
	}
	completionPath := filepath.Join(opt.runDir, judgeAuditDirName, judgeAuditCompletionFile)
	if err := writeJSON(completionPath, completion); err != nil {
		return fmt.Errorf("write completion: %w", err)
	}
	fmt.Printf("judge-audit-finalize: audited=%d fn=%d fp=%d agreement=%.3f verdict=%s→%s changed=%t\n",
		completion.Summary.Audited, completion.Summary.FalseNegative, completion.Summary.FalsePositive,
		completion.Summary.ReviewerAgreement, completion.Verdict.Raw, completion.Verdict.Corrected, completion.Verdict.Changed)
	fmt.Printf("judge-audit-finalize: protocol=%s artifacts=%s\n", completion.ProtocolHash, completion.ArtifactHash)
	return nil
}

// readEvalProtocolHash reads the frozen protocol hash bound to a run
// directory (runDir/protocol.json). A missing or empty protocol_hash is a
// hard error: a judge-audit completion must never be bound to an unprovenanced
// run.
func readEvalProtocolHash(runDir string) (string, error) {
	var protocol struct {
		ProtocolHash string `json:"protocol_hash"`
	}
	if err := readJSON(filepath.Join(runDir, "protocol.json"), &protocol); err != nil {
		return "", fmt.Errorf("read protocol for hash binding: %w", err)
	}
	if !isDigest(protocol.ProtocolHash) {
		return "", fmt.Errorf("run protocol has no protocol_hash to bind")
	}
	return protocol.ProtocolHash, nil
}
