package main

import (
	"reflect"
	"testing"
)

func TestPrepareOperationalJudgeAuditProducesDeterministicBlindedPackets(t *testing.T) {
	control := judgeAuditArmInput{
		Benchmark: "locomo",
		Arm:       "legacy_count_packer",
		Runs: [][]result{
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: true, Question: "When?", Gold: "2024", Predicted: "2024"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: false, Question: "Where?", Gold: "B", Predicted: "C"},
			),
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: true, Question: "When?", Gold: "2024", Predicted: "2024"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: false, Question: "Where?", Gold: "B", Predicted: "C"},
			),
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: false, Question: "When?", Gold: "2024", Predicted: "2023"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: false, Question: "Where?", Gold: "B", Predicted: "D"},
			),
		},
	}
	treatment := judgeAuditArmInput{
		Benchmark: "locomo",
		Arm:       "deterministic_extractive_compiler",
		Runs: [][]result{
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: false, Question: "When?", Gold: "2024", Predicted: "2023"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: false, Question: "Where?", Gold: "B", Predicted: "D"},
			),
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: false, Question: "When?", Gold: "2024", Predicted: "2022"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: true, Question: "Where?", Gold: "B", Predicted: "B"},
			),
			judgeAuditRun(
				result{QuestionID: "q-discordant", CategoryName: "temporal", Correct: true, Question: "When?", Gold: "2024", Predicted: "2024"},
				result{QuestionID: "q-positive", CategoryName: "single_hop", Correct: true, Question: "Who?", Gold: "A", Predicted: "A"},
				result{QuestionID: "q-negative", CategoryName: "multi_hop", Correct: false, Question: "Where?", Gold: "B", Predicted: "C"},
			),
		},
	}
	plan := evalJudgeAuditPlan{AllDiscordant: true, ConcordantPerStratum: 1, Seed: "frozen-seed"}

	first, err := prepareOperationalJudgeAudit(plan, control, treatment)
	if err != nil {
		t.Fatalf("prepare operational audit: %v", err)
	}
	second, err := prepareOperationalJudgeAudit(plan, control, treatment)
	if err != nil {
		t.Fatalf("repeat operational audit: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("operational judge-audit preparation is not deterministic")
	}
	if got, want := len(first.Selections), 3; got != want {
		t.Fatalf("selection count = %d, want %d", got, want)
	}
	if got, want := len(first.Packets), 6; got != want {
		t.Fatalf("packet count = %d, want %d", got, want)
	}
	if got, want := len(first.Key), len(first.Packets); got != want {
		t.Fatalf("key count = %d, want %d", got, want)
	}
	seenPacket := map[string]bool{}
	for _, packet := range first.Packets {
		if packet.PacketID == "" || seenPacket[packet.PacketID] {
			t.Fatalf("packet ID is empty or duplicated: %#v", packet)
		}
		seenPacket[packet.PacketID] = true
		if packet.Question == "" || packet.Gold == "" || packet.Answer == "" {
			t.Fatalf("blinded packet is incomplete: %#v", packet)
		}
	}
	for _, key := range first.Key {
		if key.Arm == "" || key.RawJudgeCorrect == nil {
			t.Fatalf("private key is incomplete: %#v", key)
		}
	}
}

func TestFinalizeOperationalJudgeAuditRequiresTwoIndependentCompleteReviews(t *testing.T) {
	prepared := operationalJudgeAuditPreparation{
		Packets: []operationalJudgeAuditPacket{
			{PacketID: "p-1", QuestionID: "q-1", Question: "Q1", Gold: "G1", Answer: "A1"},
			{PacketID: "p-2", QuestionID: "q-1", Question: "Q1", Gold: "G1", Answer: "A2"},
		},
		Key: []operationalJudgeAuditKey{
			{PacketID: "p-1", QuestionID: "q-1", Arm: "control", RawJudgeCorrect: boolPointer(false)},
			{PacketID: "p-2", QuestionID: "q-1", Arm: "treatment", RawJudgeCorrect: boolPointer(true)},
		},
	}
	reviews := []operationalJudgeAuditReview{
		{PacketID: "p-1", Reviewer: "reviewer-a", Correct: true, Reason: "equivalent"},
		{PacketID: "p-1", Reviewer: "reviewer-b", Correct: true, Reason: "equivalent"},
		{PacketID: "p-2", Reviewer: "reviewer-a", Correct: false, Reason: "wrong"},
		{PacketID: "p-2", Reviewer: "reviewer-b", Correct: true, Reason: "acceptable"},
	}
	if _, err := finalizeOperationalJudgeAudit(prepared, reviews, nil); err == nil {
		t.Fatal("reviewer disagreement was accepted without adjudication")
	}
	completed, err := finalizeOperationalJudgeAudit(prepared, reviews, []operationalJudgeAuditDecision{{
		PacketID: "p-2", Correct: false, Reason: "adjudicated against rubric",
	}})
	if err != nil {
		t.Fatalf("finalize operational audit: %v", err)
	}
	if completed.Summary.Audited != 2 || completed.Summary.FalseNegative != 1 || completed.Summary.FalsePositive != 1 {
		t.Fatalf("unexpected audit summary: %#v", completed.Summary)
	}
	if completed.Summary.ReviewerAgreement != 0.5 {
		t.Fatalf("reviewer agreement = %v, want 0.5", completed.Summary.ReviewerAgreement)
	}
	if len(completed.Results) != 2 || !completed.Results[0].Correct || completed.Results[1].Correct {
		t.Fatalf("unexpected corrected results: %#v", completed.Results)
	}

	incomplete := reviews[:3]
	if _, err := finalizeOperationalJudgeAudit(prepared, incomplete, nil); err == nil {
		t.Fatal("incomplete reviewer coverage was accepted")
	}
}

func judgeAuditRun(items ...result) []result { return items }

func boolPointer(value bool) *bool { return &value }
