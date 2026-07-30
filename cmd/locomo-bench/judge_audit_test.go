package main

import (
	"reflect"
	"testing"
)

func TestJudgeAuditSelectsAllDiscordantAndDeterministicConcordantStrata(t *testing.T) {
	questions := []evalJudgeAuditQuestion{
		{QuestionID: "q-discordant-a", Benchmark: "locomo", Category: "temporal", ControlCorrect: true, TreatmentCorrect: false},
		{QuestionID: "q-discordant-b", Benchmark: "longmemeval_s", Category: "single_hop", ControlCorrect: false, TreatmentCorrect: true},
		{QuestionID: "q-positive-a", Benchmark: "locomo", Category: "temporal", ControlCorrect: true, TreatmentCorrect: true},
		{QuestionID: "q-positive-b", Benchmark: "locomo", Category: "temporal", ControlCorrect: true, TreatmentCorrect: true},
		{QuestionID: "q-negative-a", Benchmark: "longmemeval_s", Category: "single_hop", ControlCorrect: false, TreatmentCorrect: false},
		{QuestionID: "q-negative-b", Benchmark: "longmemeval_s", Category: "single_hop", ControlCorrect: false, TreatmentCorrect: false},
	}
	plan := evalJudgeAuditPlan{AllDiscordant: true, ConcordantPerStratum: 1, Seed: "sha256:pre-registered-sample"}
	first, err := selectJudgeAuditQuestions(plan, questions)
	if err != nil {
		t.Fatalf("select judge audit: %v", err)
	}
	second, err := selectJudgeAuditQuestions(plan, questions)
	if err != nil {
		t.Fatalf("repeat select judge audit: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("judge audit selection is nondeterministic:\n%#v\n%#v", first, second)
	}

	selected := map[string]evalJudgeAuditSelection{}
	for _, item := range first {
		selected[item.QuestionID] = item
	}
	for _, id := range []string{"q-discordant-a", "q-discordant-b"} {
		item, ok := selected[id]
		if !ok || item.SelectionReason != evalAuditSelectionDiscordant {
			t.Fatalf("discordant question %q not selected as mandatory: %#v", id, item)
		}
	}
	if len(first) != 4 {
		t.Fatalf("selection count = %d, want 2 mandatory + 2 strata samples", len(first))
	}
}

func TestJudgeAuditPacketsAreBlindAndAdjudicationTracksNoise(t *testing.T) {
	packets, err := blindJudgeAuditPackets([]evalJudgeAuditSource{{
		QuestionID:      "q-1",
		Benchmark:       "locomo",
		Category:        "temporal",
		ControlArm:      "legacy_count_packer",
		TreatmentArm:    "deterministic_extractive_compiler",
		ModelID:         "local-answerer",
		RawJudgeCorrect: false,
		Question:        "When did Alice move?",
		Gold:            "Alice moved in 2023.",
		Answer:          "She moved in 2023.",
	}})
	if err != nil {
		t.Fatalf("blind audit packets: %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(packets))
	}
	if packets[0].Arm != "" || packets[0].ModelID != "" || packets[0].RawJudgeLabel != nil {
		t.Fatalf("review packet leaks arm/model/raw label: %#v", packets[0])
	}

	adjudicated, err := adjudicateJudgeAudit([]evalJudgeAuditReview{
		{QuestionID: "q-fn", Reviewer: "r1", Correct: true},
		{QuestionID: "q-fn", Reviewer: "r2", Correct: true},
		{QuestionID: "q-fp", Reviewer: "r1", Correct: false},
		{QuestionID: "q-fp", Reviewer: "r2", Correct: false},
	})
	if err != nil {
		t.Fatalf("adjudicate reviews: %v", err)
	}
	noise, err := summarizeJudgeNoise([]evalJudgeAuditComparison{
		{QuestionID: "q-fn", RawJudgeCorrect: false, AdjudicatedCorrect: adjudicated["q-fn"].Correct},
		{QuestionID: "q-fp", RawJudgeCorrect: true, AdjudicatedCorrect: adjudicated["q-fp"].Correct},
	})
	if err != nil {
		t.Fatalf("summarize judge noise: %v", err)
	}
	if noise.FalseNegative != 1 || noise.FalsePositive != 1 {
		t.Fatalf("judge noise = %#v, want one FN and one FP", noise)
	}
	if !judgeAuditChangesVerdict(evalVerdictGO, evalVerdictHOLD) {
		t.Fatal("GO→HOLD correction was not detected as verdict change")
	}
	if judgeAuditChangesVerdict(evalVerdictHOLD, evalVerdictHOLD) {
		t.Fatal("unchanged verdict reported as changed")
	}
}

func TestJudgeAuditSummaryKeepsRawAndCorrectedScoresSeparate(t *testing.T) {
	summary, err := summarizeJudgeAuditResults([]evalJudgeAuditResult{
		{QuestionID: "q-1", RawJudgeCorrect: false, AdjudicatedCorrect: true, ReviewerAgreement: true},
		{QuestionID: "q-2", RawJudgeCorrect: false, AdjudicatedCorrect: true, ReviewerAgreement: false},
	})
	if err != nil {
		t.Fatalf("summarize judge audit results: %v", err)
	}
	if summary.RawAccuracy != 0 || summary.CorrectedAccuracy != 1 || summary.FalseNegative != 2 {
		t.Fatalf("raw/corrected audit summary = %#v", summary)
	}
	if summary.ReviewerAgreement != 0.5 {
		t.Fatalf("reviewer agreement = %v, want 0.5", summary.ReviewerAgreement)
	}
}
