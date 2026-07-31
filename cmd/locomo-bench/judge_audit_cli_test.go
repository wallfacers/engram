package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// ---- T115: operational judge-audit workflow (CLI + file round-trip) ----

func TestJudgeAuditVerdictGate(t *testing.T) {
	gate := judgeAuditAccuracyGate{Accuracy: 0.9}
	cases := []struct {
		name     string
		accuracy float64
		gate     judgeAuditAccuracyGate
		want     evalVerdict
	}{
		{"meets gate", 0.95, gate, evalVerdictGO},
		{"exact gate", 0.9, gate, evalVerdictGO},
		{"below gate", 0.85, gate, evalVerdictHOLD},
		{"zero gate", 0.95, judgeAuditAccuracyGate{}, evalVerdictInvalid},
		{"gate above one", 0.95, judgeAuditAccuracyGate{Accuracy: 1.5}, evalVerdictInvalid},
		{"gate below zero", 0.95, judgeAuditAccuracyGate{Accuracy: -0.1}, evalVerdictInvalid},
	}
	for _, tc := range cases {
		if got := judgeAuditVerdictFor(tc.accuracy, tc.gate); got != tc.want {
			t.Errorf("%s: judgeAuditVerdictFor(%v, %#v) = %s, want %s", tc.name, tc.accuracy, tc.gate, got, tc.want)
		}
	}
	if !judgeAuditChangesVerdict(judgeAuditVerdictFor(0.5, gate), judgeAuditVerdictFor(1.0, gate)) {
		t.Fatal("HOLD→GO correction was not detected as a verdict change")
	}
	if judgeAuditChangesVerdict(judgeAuditVerdictFor(0.95, gate), judgeAuditVerdictFor(1.0, gate)) {
		t.Fatal("GO→GO correction reported as a verdict change")
	}
}

func TestJudgeAuditPreparationFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prepared := operationalJudgeAuditPreparation{
		Plan: evalJudgeAuditPlan{AllDiscordant: true, ConcordantPerStratum: 1, Seed: "frozen-seed"},
		Selections: []evalJudgeAuditSelection{{
			QuestionID: "q-1", SelectionReason: evalAuditSelectionDiscordant, Stratum: "temporal",
		}},
		Packets: []operationalJudgeAuditPacket{{
			PacketID: "p-1", QuestionID: "q-1", Benchmark: "locomo", Category: "temporal",
			Question: "When?", Gold: "2024", Answer: "2024",
		}},
		Key: []operationalJudgeAuditKey{{
			PacketID: "p-1", QuestionID: "q-1", Arm: "control", RawJudgeCorrect: boolPointer(false),
		}},
	}
	packetsPath, keyPath, preparedPath, err := writeJudgeAuditPreparation(dir, prepared)
	if err != nil {
		t.Fatalf("write judge-audit preparation: %v", err)
	}
	for _, path := range []string{packetsPath, keyPath, preparedPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected output file %s: %v", path, err)
		}
	}
	packetsRaw, err := os.ReadFile(packetsPath)
	if err != nil {
		t.Fatalf("read packets: %v", err)
	}
	for _, leak := range []string{`"arm"`, `"raw_judge_correct"`} {
		if bytes.Contains(packetsRaw, []byte(leak)) {
			t.Fatalf("blinded reviewer packets leak private key field %s", leak)
		}
	}
	loaded, err := loadJudgeAuditPreparation(dir)
	if err != nil {
		t.Fatalf("load judge-audit preparation: %v", err)
	}
	if !reflect.DeepEqual(loaded, prepared) {
		t.Fatalf("judge-audit preparation round trip mismatch:\nloaded:   %#v\noriginal: %#v", loaded, prepared)
	}
}

func TestJudgeAuditCLIWorkflow(t *testing.T) {
	runDir := t.TempDir()
	copyJudgeAuditFixture(t, runDir)
	if err := writeJSON(filepath.Join(runDir, "protocol.json"), map[string]string{
		"schema": evalProtocolSchema, "protocol_hash": "sha256:feedface",
	}); err != nil {
		t.Fatalf("write protocol.json: %v", err)
	}
	for _, artifact := range []string{evalCandidatesArtifactFile, evalTraceArtifactFile, evalBundleArtifactFile, evalClassificationArtifactFile} {
		if err := os.WriteFile(filepath.Join(runDir, artifact), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", artifact, err)
		}
	}
	opt := options{
		runDir: runDir, auditControlArm: "control", auditTreatmentArm: "treatment",
		auditRepeats: 3, auditPlanSeed: "frozen-seed", auditConcordantPerStratum: 1,
		auditAccuracyGate: 0.9, auditBenchmark: "locomo",
	}
	if err := runJudgeAuditPrepareCLI(opt); err != nil {
		t.Fatalf("judge-audit prepare CLI: %v", err)
	}
	auditDir := filepath.Join(runDir, judgeAuditDirName)
	for _, file := range []string{"packets.json", "key.json", "prepared.json"} {
		if _, err := os.Stat(filepath.Join(auditDir, file)); err != nil {
			t.Fatalf("prepare did not emit %s: %v", file, err)
		}
	}
	prepared, err := loadJudgeAuditPreparation(runDir)
	if err != nil {
		t.Fatalf("load prepared audit for reviews: %v", err)
	}
	reviews := make([]operationalJudgeAuditReview, 0, 2*len(prepared.Packets))
	for _, packet := range prepared.Packets {
		reviews = append(reviews,
			operationalJudgeAuditReview{PacketID: packet.PacketID, Reviewer: "reviewer-a", Correct: true, Reason: "matches gold"},
			operationalJudgeAuditReview{PacketID: packet.PacketID, Reviewer: "reviewer-b", Correct: true, Reason: "matches gold"},
		)
	}
	reviewsPath := filepath.Join(runDir, "reviews.json")
	if err := writeJSON(reviewsPath, reviews); err != nil {
		t.Fatalf("write reviews: %v", err)
	}
	opt.auditReviews = reviewsPath
	if err := runJudgeAuditFinalizeCLI(opt); err != nil {
		t.Fatalf("judge-audit finalize CLI: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(auditDir, "completion.json"))
	if err != nil {
		t.Fatalf("read completion: %v", err)
	}
	var completion operationalJudgeAuditCompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		t.Fatalf("decode completion: %v", err)
	}
	if completion.ProtocolHash != "sha256:feedface" {
		t.Fatalf("protocol hash not bound: %q", completion.ProtocolHash)
	}
	if !strings.HasPrefix(completion.ArtifactHash, "sha256:") {
		t.Fatalf("artifact hash not bound: %q", completion.ArtifactHash)
	}
	if completion.Verdict.Raw != evalVerdictHOLD || completion.Verdict.Corrected != evalVerdictGO || !completion.Verdict.Changed {
		t.Fatalf("expected HOLD→GO verdict change, got %#v", completion.Verdict)
	}
	if completion.Summary.FalseNegative+completion.Summary.FalsePositive == 0 {
		t.Fatal("audit summary reports no judge noise on fully-wrong treatment arm")
	}
}

func writeJudgeAuditRuns(t *testing.T, runDir string, control, treatment []result) {
	t.Helper()
	for repeat := 1; repeat <= 3; repeat++ {
		dir := filepath.Join(runDir, fmt.Sprintf("run-%d", repeat))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir run dir: %v", err)
		}
		for arm, items := range map[string][]result{"control": control, "treatment": treatment} {
			var builder strings.Builder
			for _, item := range items {
				raw, err := json.Marshal(item)
				if err != nil {
					t.Fatalf("marshal result: %v", err)
				}
				builder.Write(raw)
				builder.WriteByte('\n')
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("results-%s.jsonl", arm)), []byte(builder.String()), 0o644); err != nil {
				t.Fatalf("write results: %v", err)
			}
		}
	}
}

// copyJudgeAuditFixture copies the committed offline fixture under
// testdata/022/judge-audit into a writable scratch directory so the CLI
// workflow can run against real fixture files without polluting testdata.
func copyJudgeAuditFixture(t *testing.T, runDir string) {
	t.Helper()
	for repeat := 1; repeat <= 3; repeat++ {
		src := filepath.Join("testdata", "022", "judge-audit", fmt.Sprintf("run-%d", repeat))
		dst := filepath.Join(runDir, fmt.Sprintf("run-%d", repeat))
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dst, err)
		}
		for _, arm := range []string{"control", "treatment"} {
			raw, err := os.ReadFile(filepath.Join(src, fmt.Sprintf("results-%s.jsonl", arm)))
			if err != nil {
				t.Fatalf("read fixture %s: %v", src, err)
			}
			if err := os.WriteFile(filepath.Join(dst, fmt.Sprintf("results-%s.jsonl", arm)), raw, 0o644); err != nil {
				t.Fatalf("write fixture copy: %v", err)
			}
		}
	}
}
