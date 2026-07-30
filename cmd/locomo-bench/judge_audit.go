package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type evalJudgeAuditQuestion struct {
	QuestionID       string
	Benchmark        string
	Category         string
	ControlCorrect   bool
	TreatmentCorrect bool
}

type evalJudgeAuditPlan struct {
	AllDiscordant        bool
	ConcordantPerStratum int
	Seed                 string
}

type evalAuditSelectionReason string

const (
	evalAuditSelectionDiscordant evalAuditSelectionReason = "discordant"
	evalAuditSelectionConcordant evalAuditSelectionReason = "concordant_stratified"
)

type evalJudgeAuditSelection struct {
	QuestionID      string
	SelectionReason evalAuditSelectionReason
	Stratum         string
}

func selectJudgeAuditQuestions(plan evalJudgeAuditPlan, questions []evalJudgeAuditQuestion) ([]evalJudgeAuditSelection, error) {
	if !plan.AllDiscordant || plan.ConcordantPerStratum < 0 || strings.TrimSpace(plan.Seed) == "" {
		return nil, fmt.Errorf("invalid judge audit plan")
	}
	seen := map[string]bool{}
	byStratum := map[string][]evalJudgeAuditQuestion{}
	selected := make([]evalJudgeAuditSelection, 0, len(questions))
	for _, question := range questions {
		if strings.TrimSpace(question.QuestionID) == "" || seen[question.QuestionID] {
			return nil, fmt.Errorf("judge audit question IDs must be unique and non-empty")
		}
		seen[question.QuestionID] = true
		if question.ControlCorrect != question.TreatmentCorrect {
			selected = append(selected, evalJudgeAuditSelection{QuestionID: question.QuestionID, SelectionReason: evalAuditSelectionDiscordant, Stratum: auditStratum(question)})
			continue
		}
		key := auditStratum(question)
		byStratum[key] = append(byStratum[key], question)
	}
	keys := make([]string, 0, len(byStratum))
	for key := range byStratum {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		items := byStratum[key]
		sort.Slice(items, func(left, right int) bool {
			leftDigest := auditSelectionDigest(plan.Seed, items[left].QuestionID)
			rightDigest := auditSelectionDigest(plan.Seed, items[right].QuestionID)
			if leftDigest == rightDigest {
				return items[left].QuestionID < items[right].QuestionID
			}
			return leftDigest < rightDigest
		})
		limit := plan.ConcordantPerStratum
		if len(items) < limit {
			limit = len(items)
		}
		for _, item := range items[:limit] {
			selected = append(selected, evalJudgeAuditSelection{QuestionID: item.QuestionID, SelectionReason: evalAuditSelectionConcordant, Stratum: key})
		}
	}
	sort.Slice(selected, func(left, right int) bool {
		if selected[left].QuestionID == selected[right].QuestionID {
			return selected[left].SelectionReason < selected[right].SelectionReason
		}
		return selected[left].QuestionID < selected[right].QuestionID
	})
	return selected, nil
}

func auditStratum(question evalJudgeAuditQuestion) string {
	label := "negative"
	if question.ControlCorrect && question.TreatmentCorrect {
		label = "positive"
	}
	return question.Benchmark + "/" + question.Category + "/" + label
}

func auditSelectionDigest(seed, questionID string) string {
	sum := sha256.Sum256([]byte(seed + "\x00" + questionID))
	return hex.EncodeToString(sum[:])
}

type evalJudgeAuditSource struct {
	QuestionID      string
	Benchmark       string
	Category        string
	ControlArm      string
	TreatmentArm    string
	ModelID         string
	RawJudgeCorrect bool
	Question        string
	Gold            string
	Answer          string
}

type evalBlindedReviewPacket struct {
	QuestionID    string
	Benchmark     string
	Category      string
	Question      string
	Gold          string
	Answer        string
	Arm           string
	ModelID       string
	RawJudgeLabel *bool
}

func blindJudgeAuditPackets(sources []evalJudgeAuditSource) ([]evalBlindedReviewPacket, error) {
	packets := make([]evalBlindedReviewPacket, 0, len(sources))
	seen := map[string]bool{}
	for _, source := range sources {
		if strings.TrimSpace(source.QuestionID) == "" || seen[source.QuestionID] || strings.TrimSpace(source.Question) == "" || strings.TrimSpace(source.Gold) == "" {
			return nil, fmt.Errorf("invalid or duplicate audit source")
		}
		seen[source.QuestionID] = true
		packets = append(packets, evalBlindedReviewPacket{
			QuestionID: source.QuestionID,
			Benchmark:  source.Benchmark,
			Category:   source.Category,
			Question:   source.Question,
			Gold:       source.Gold,
			Answer:     source.Answer,
		})
	}
	return packets, nil
}

type evalJudgeAuditReview struct {
	QuestionID string
	Reviewer   string
	Correct    bool
}

type evalJudgeAuditDecision struct {
	QuestionID string
	Correct    bool
	Reason     string
}

type evalJudgeAuditAdjudication struct {
	Correct           bool
	ReviewerAgreement bool
	Adjudicated       bool
}

// adjudicateJudgeAudit resolves reviewer agreement directly. Disagreement is
// deliberately rejected until a separately recorded adjudicator decision is
// supplied; guessing would defeat a blinded audit.
func adjudicateJudgeAudit(reviews []evalJudgeAuditReview) (map[string]evalJudgeAuditAdjudication, error) {
	byQuestion := map[string][]evalJudgeAuditReview{}
	for _, review := range reviews {
		if strings.TrimSpace(review.QuestionID) == "" || strings.TrimSpace(review.Reviewer) == "" {
			return nil, fmt.Errorf("reviewer and question ID are required")
		}
		byQuestion[review.QuestionID] = append(byQuestion[review.QuestionID], review)
	}
	result := make(map[string]evalJudgeAuditAdjudication, len(byQuestion))
	for questionID, items := range byQuestion {
		if len(items) != 2 || items[0].Reviewer == items[1].Reviewer {
			return nil, fmt.Errorf("question %q requires two independent reviewers", questionID)
		}
		if items[0].Correct != items[1].Correct {
			return nil, fmt.Errorf("question %q requires adjudicator decision", questionID)
		}
		result[questionID] = evalJudgeAuditAdjudication{Correct: items[0].Correct, ReviewerAgreement: true}
	}
	return result, nil
}

func applyJudgeAuditDecisions(adjudicated map[string]evalJudgeAuditAdjudication, decisions []evalJudgeAuditDecision) error {
	for _, decision := range decisions {
		if strings.TrimSpace(decision.QuestionID) == "" || strings.TrimSpace(decision.Reason) == "" {
			return fmt.Errorf("adjudicator decision requires question ID and reason")
		}
		adjudicated[decision.QuestionID] = evalJudgeAuditAdjudication{Correct: decision.Correct, Adjudicated: true}
	}
	return nil
}

type evalJudgeAuditComparison struct {
	QuestionID         string
	RawJudgeCorrect    bool
	AdjudicatedCorrect bool
}

type evalJudgeNoiseSummary struct {
	Audited       int
	FalseNegative int
	FalsePositive int
}

func summarizeJudgeNoise(comparisons []evalJudgeAuditComparison) (evalJudgeNoiseSummary, error) {
	seen := map[string]bool{}
	summary := evalJudgeNoiseSummary{Audited: len(comparisons)}
	for _, comparison := range comparisons {
		if strings.TrimSpace(comparison.QuestionID) == "" || seen[comparison.QuestionID] {
			return evalJudgeNoiseSummary{}, fmt.Errorf("judge-noise comparisons require unique question IDs")
		}
		seen[comparison.QuestionID] = true
		switch {
		case !comparison.RawJudgeCorrect && comparison.AdjudicatedCorrect:
			summary.FalseNegative++
		case comparison.RawJudgeCorrect && !comparison.AdjudicatedCorrect:
			summary.FalsePositive++
		}
	}
	return summary, nil
}

func judgeAuditChangesVerdict(raw, corrected evalVerdict) bool {
	return raw != corrected
}

type evalJudgeAuditResult struct {
	QuestionID         string
	RawJudgeCorrect    bool
	AdjudicatedCorrect bool
	ReviewerAgreement  bool
}

type evalJudgeAuditSummary struct {
	Audited           int
	FalseNegative     int
	FalsePositive     int
	RawAccuracy       float64
	CorrectedAccuracy float64
	ReviewerAgreement float64
}

func summarizeJudgeAuditResults(results []evalJudgeAuditResult) (evalJudgeAuditSummary, error) {
	comparisons := make([]evalJudgeAuditComparison, 0, len(results))
	seen := map[string]bool{}
	summary := evalJudgeAuditSummary{Audited: len(results)}
	for _, result := range results {
		if strings.TrimSpace(result.QuestionID) == "" || seen[result.QuestionID] {
			return evalJudgeAuditSummary{}, fmt.Errorf("judge audit results require unique question IDs")
		}
		seen[result.QuestionID] = true
		comparisons = append(comparisons, evalJudgeAuditComparison{
			QuestionID: result.QuestionID, RawJudgeCorrect: result.RawJudgeCorrect, AdjudicatedCorrect: result.AdjudicatedCorrect,
		})
		if result.RawJudgeCorrect {
			summary.RawAccuracy++
		}
		if result.AdjudicatedCorrect {
			summary.CorrectedAccuracy++
		}
		if result.ReviewerAgreement {
			summary.ReviewerAgreement++
		}
	}
	noise, err := summarizeJudgeNoise(comparisons)
	if err != nil {
		return evalJudgeAuditSummary{}, err
	}
	summary.FalseNegative = noise.FalseNegative
	summary.FalsePositive = noise.FalsePositive
	if summary.Audited > 0 {
		denominator := float64(summary.Audited)
		summary.RawAccuracy /= denominator
		summary.CorrectedAccuracy /= denominator
		summary.ReviewerAgreement /= denominator
	}
	return summary, nil
}
