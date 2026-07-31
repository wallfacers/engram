package main

import (
	"fmt"
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
	PacketID  string `json:"packet_id"`
	QuestionID string `json:"question_id"`
	Benchmark string `json:"benchmark"`
	Category  string `json:"category"`
	Question  string `json:"question"`
	Gold      string `json:"gold"`
	Answer    string `json:"answer"`
}

// operationalJudgeAuditKey is kept away from reviewer packets. Arm identity
// and the raw judge label are intentionally absent from the blinded packet.
type operationalJudgeAuditKey struct {
	PacketID       string `json:"packet_id"`
	QuestionID     string `json:"question_id"`
	Arm            string `json:"arm"`
	RawJudgeCorrect *bool  `json:"raw_judge_correct"`
}

type operationalJudgeAuditPreparation struct {
	Plan       evalJudgeAuditPlan        `json:"plan"`
	Selections []evalJudgeAuditSelection `json:"selections"`
	Packets    []operationalJudgeAuditPacket `json:"packets"`
	Key        []operationalJudgeAuditKey `json:"key"`
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
	Results []operationalJudgeAuditResult `json:"results"`
	Summary evalJudgeAuditSummary         `json:"summary"`
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
