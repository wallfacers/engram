package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func testUnifiedAnswerContractProbeMetadata() (unifiedAnswerContractProbeModelMetadata, unifiedAnswerContractProbeModelMetadata) {
	return unifiedAnswerContractProbeModelMetadata{
			Provider: "stub-answer-provider",
			Model:    "stub-answer-model",
			Revision: "answer-revision-1",
		}, unifiedAnswerContractProbeModelMetadata{
			Provider: "stub-judge-provider",
			Model:    "stub-judge-model",
			Revision: "judge-revision-1",
		}
}

func testUnifiedAnswerContractProbeCase() unifiedAnswerContractProbeCase {
	return unifiedAnswerContractProbeCase{
		ID:      "direct-support",
		Slice:   "direct",
		Request: "Where is Ari planning to travel?",
		MemoryEvidence: []retrievedMemory{{
			Content:   "Ari said they are planning to travel to Kyoto.",
			EventDate: "2026-03-05",
		}},
		ExpectedBehavior:   []string{"States that Ari is planning to travel to Kyoto."},
		ProhibitedBehavior: []string{"Invents a different personal destination."},
	}
}

func TestRunUnifiedAnswerContractProbePassesAndAccountsUsage(t *testing.T) {
	t.Parallel()

	fixture := testUnifiedAnswerContractProbeCase()

	var gotAnswerSystem, gotAnswerUser string
	answerCall := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		gotAnswerSystem, gotAnswerUser = system, user
		return "Ari is planning to travel to Kyoto.", provider.Usage{InputTokens: 11, OutputTokens: 7}, nil
	}

	var gotJudgeSystem, gotJudgeUser string
	judgeCall := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		gotJudgeSystem, gotJudgeUser = system, user
		return `{"pass":true,"violations":[]}`, provider.Usage{InputTokens: 13, OutputTokens: 5}, nil
	}

	report, err := runUnifiedAnswerContractProbe(context.Background(), answerCall, judgeCall, []unifiedAnswerContractProbeCase{fixture})
	if err != nil {
		t.Fatalf("runUnifiedAnswerContractProbe: %v", err)
	}
	if gotAnswerSystem != unifiedAnswerContractPrompt {
		t.Fatalf("answer system prompt differs from unified contract\ngot:\n%s\nwant:\n%s", gotAnswerSystem, unifiedAnswerContractPrompt)
	}
	wantAnswerUser := buildAnswerPrompt(fixture.Request, fixture.MemoryEvidence, fixture.CurrentDate, "")
	if gotAnswerUser != wantAnswerUser {
		t.Fatalf("answer user prompt differs from buildAnswerPrompt\ngot:\n%s\nwant:\n%s", gotAnswerUser, wantAnswerUser)
	}
	judgeSystemLower := strings.ToLower(gotJudgeSystem)
	for _, forbidden := range []string{"benchmark", "example", "locomo", "longmemeval", "gold answer"} {
		if strings.Contains(judgeSystemLower, forbidden) {
			t.Errorf("judge system prompt contains forbidden term %q: %s", forbidden, gotJudgeSystem)
		}
	}
	for _, want := range []string{fixture.Request, fixture.ExpectedBehavior[0], fixture.ProhibitedBehavior[0], "Ari is planning to travel to Kyoto."} {
		if !strings.Contains(gotJudgeUser, want) {
			t.Errorf("judge user prompt does not contain %q: %s", want, gotJudgeUser)
		}
	}

	if report.Passed != 1 || report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("unexpected report counts: %+v", report)
	}
	result := report.Results[0]
	if !result.Pass || len(result.Violations) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.RawAnswer != "Ari is planning to travel to Kyoto." {
		t.Fatalf("raw answer = %q", result.RawAnswer)
	}
	if result.Usage.Answer.InputTokens != 11 || result.Usage.Answer.OutputTokens != 7 ||
		result.Usage.Judge.InputTokens != 13 || result.Usage.Judge.OutputTokens != 5 {
		t.Fatalf("result usage = %+v", result.Usage)
	}
	if report.Usage != result.Usage {
		t.Fatalf("report usage = %+v, want %+v", report.Usage, result.Usage)
	}
}

func TestRunPairedUnifiedAnswerContractProbeUsesSameEvidenceAndJudgesOnlyFinalAnswer(t *testing.T) {
	t.Parallel()

	fixture := testUnifiedAnswerContractProbeCase()
	answerMetadata, judgeMetadata := testUnifiedAnswerContractProbeMetadata()
	var answerUsers []string
	var answerSystems []string
	answerCall := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		answerSystems = append(answerSystems, system)
		answerUsers = append(answerUsers, user)
		if system == answerSystemPrompt {
			return "<think>private control candidate: Osaka</think>response Ari plans to visit Kyoto.", provider.Usage{InputTokens: 11, OutputTokens: 3}, nil
		}
		if system == unifiedAnswerContractPrompt {
			return "[reasoning]private unified candidate: Osaka[/reasoning]Ari plans to visit Kyoto.", provider.Usage{InputTokens: 13, OutputTokens: 5}, nil
		}
		t.Fatalf("unexpected answer system prompt: %q", system)
		return "", provider.Usage{}, nil
	}
	judgeCalls := 0
	judgeCall := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		judgeCalls++
		if system != unifiedAnswerContractProbeJudgePrompt {
			t.Fatalf("judge system prompt differs from frozen rubric")
		}
		for _, forbidden := range []string{"private control candidate", "private unified candidate", "Osaka", "<think>", "[reasoning]"} {
			if strings.Contains(user, forbidden) {
				t.Fatalf("judge received raw reasoning %q in %s", forbidden, user)
			}
		}
		if !strings.Contains(user, "Ari plans to visit Kyoto.") {
			t.Fatalf("judge did not receive final answer: %s", user)
		}
		return "<think>private judge reasoning</think>{\"pass\":true,\"violations\":[]}", provider.Usage{InputTokens: 17, OutputTokens: 7}, nil
	}

	report, err := runPairedUnifiedAnswerContractProbe(context.Background(), unifiedAnswerContractPairedProbeConfig{
		Repeats:     3,
		AnswerModel: answerMetadata,
		JudgeModel:  judgeMetadata,
	}, answerCall, judgeCall, []unifiedAnswerContractProbeCase{fixture})
	if err != nil {
		t.Fatalf("run paired probe: %v", err)
	}
	if len(answerSystems) != 6 || judgeCalls != 6 || len(report.Results) != 6 {
		t.Fatalf("calls/results = answer:%d judge:%d results:%d", len(answerSystems), judgeCalls, len(report.Results))
	}
	for i := 0; i < len(answerUsers); i += 2 {
		if answerUsers[i] != answerUsers[i+1] {
			t.Fatalf("repeat %d arms received different evidence\ncontrol:\n%s\ntreatment:\n%s", i/2+1, answerUsers[i], answerUsers[i+1])
		}
		repeat := i/2 + 1
		wantFirst, wantSecond := answerSystemPrompt, unifiedAnswerContractPrompt
		if repeat%2 == 0 {
			wantFirst, wantSecond = unifiedAnswerContractPrompt, answerSystemPrompt
		}
		if answerSystems[i] != wantFirst || answerSystems[i+1] != wantSecond {
			t.Fatalf("repeat %d arm order/prompts = %q, %q", repeat, answerSystems[i], answerSystems[i+1])
		}
	}
	for i, result := range report.Results {
		if !result.Pass || result.Repeat != i/2+1 {
			t.Fatalf("result %d = %+v", i, result)
		}
		wantArm := unifiedAnswerContractProbeControlArm
		if (result.Repeat%2 == 1 && i%2 == 1) || (result.Repeat%2 == 0 && i%2 == 0) {
			wantArm = unifiedAnswerContractProbeTreatmentArm
		}
		if result.Arm != wantArm {
			t.Fatalf("result %d arm = %q, want %q", i, result.Arm, wantArm)
		}
		if result.RawAnswer == result.FinalAnswer || result.FinalAnswer != "Ari plans to visit Kyoto." {
			t.Fatalf("result %d raw/final answer = %q / %q", i, result.RawAnswer, result.FinalAnswer)
		}
		if result.RawJudgment == result.FinalJudgment || result.FinalJudgment != `{"pass":true,"violations":[]}` {
			t.Fatalf("result %d raw/final judgment = %q / %q", i, result.RawJudgment, result.FinalJudgment)
		}
	}
	manifest := report.Manifest
	if manifest.SchemaVersion != unifiedAnswerContractProbeSchemaVersion || manifest.Repeats != 3 || manifest.CreatedAt.IsZero() {
		t.Fatalf("manifest identity = %+v", manifest)
	}
	if manifest.FixtureDigest == "" || manifest.ControlPromptDigest != digestUnifiedAnswerContractProbeBytes([]byte(answerSystemPrompt)) ||
		manifest.TreatmentPromptDigest != digestUnifiedAnswerContractProbeBytes([]byte(unifiedAnswerContractPrompt)) ||
		manifest.JudgePromptDigest != digestUnifiedAnswerContractProbeBytes([]byte(unifiedAnswerContractProbeJudgePrompt)) {
		t.Fatalf("manifest digests = %+v", manifest)
	}
	if manifest.ControlPromptDigest == manifest.TreatmentPromptDigest || manifest.AnswerModel != answerMetadata || manifest.JudgeModel != judgeMetadata {
		t.Fatalf("manifest prompts/models = %+v", manifest)
	}
	if manifest.ProviderAttemptPolicy != "one_attempt_per_answer_and_judge_call" ||
		manifest.ArmOrderPolicy != "counterbalanced_case_index_plus_repeat_v1" ||
		manifest.ArtifactSensitivity != "sensitive_raw_model_output_mode_0600" {
		t.Fatalf("manifest execution policy = %+v", manifest)
	}
	if !report.Valid || !report.Complete || report.RunStatus != unifiedAnswerContractProbeRunComplete || report.OperationalFailures != 0 ||
		report.Passed != 6 || report.Failed != 0 || len(report.ArmSummaries) != 2 || len(report.SliceSummaries) != 2 {
		t.Fatalf("paired report counts/summaries = %+v", report)
	}
	for _, summary := range report.ArmSummaries {
		if summary.Runs != 3 || summary.Cases != 1 || summary.CompleteCases != 1 || summary.IncompleteCases != 0 ||
			summary.MajorityPassed != 1 || summary.MajorityFailed != 0 || summary.MajorityPassRate != 1 ||
			summary.Passed != 3 || summary.PassRate != 1 {
			t.Fatalf("arm summary = %+v", summary)
		}
		if summary.Usage.Answer.InputTokens == 0 || summary.Usage.Judge.InputTokens == 0 {
			t.Fatalf("arm usage missing: %+v", summary)
		}
	}
}

func TestUnifiedAnswerContractProbeJudgeAllowsExpectedGeneralAdvice(t *testing.T) {
	t.Parallel()

	lower := strings.ToLower(unifiedAnswerContractProbeJudgePrompt)
	for _, required := range []string{"personal factual claims", "general knowledge", "advice", "expected behavior"} {
		if !strings.Contains(lower, required) {
			t.Errorf("judge rubric does not state %q boundary: %s", required, unifiedAnswerContractProbeJudgePrompt)
		}
	}
	probeCase := unifiedAnswerContractProbeCase{
		ID:                 "general-advice",
		Slice:              "general-advice",
		Request:            "How can I prepare for a long flight?",
		ExpectedBehavior:   []string{"Gives useful general advice despite having no personal memories."},
		ProhibitedBehavior: []string{"Invents a personal medical condition or travel history."},
	}
	judgePrompt, err := buildUnifiedAnswerContractProbeJudgePrompt(probeCase, "Hydrate and walk around periodically.")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"useful general advice", "Hydrate and walk around periodically."} {
		if !strings.Contains(judgePrompt, want) {
			t.Errorf("judge input missing %q: %s", want, judgePrompt)
		}
	}
}

func TestRunPairedUnifiedAnswerContractProbeRejectsIncompleteJudgeJSON(t *testing.T) {
	t.Parallel()

	answerMetadata, judgeMetadata := testUnifiedAnswerContractProbeMetadata()
	answerCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "Supported final", provider.Usage{InputTokens: 2, OutputTokens: 1}, nil
	}
	judgeCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "<think>judge scratch</think>{\"pass\":true", provider.Usage{InputTokens: 3, OutputTokens: 2}, nil
	}
	report, err := runPairedUnifiedAnswerContractProbe(context.Background(), unifiedAnswerContractPairedProbeConfig{
		Repeats: 1, AnswerModel: answerMetadata, JudgeModel: judgeMetadata,
	}, answerCall, judgeCall, []unifiedAnswerContractProbeCase{testUnifiedAnswerContractProbeCase()})
	if err == nil || !strings.Contains(err.Error(), "invalid judge response") {
		t.Fatalf("incomplete judge JSON error = %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].FinalJudgment != `{"pass":true` || report.Results[0].Pass {
		t.Fatalf("partial paired report = %+v", report)
	}
	if report.Valid || report.Complete || report.RunStatus != unifiedAnswerContractProbeRunOperationalFailed ||
		report.OperationalFailures != 1 || report.Failed != 0 || report.FailureCode != unifiedAnswerContractProbeInvalidJudge {
		t.Fatalf("partial run validity = %+v", report)
	}
	if len(report.ArmSummaries) != 0 || len(report.SliceSummaries) != 0 {
		t.Fatalf("invalid operational run exposed behavioral summaries: arms=%+v slices=%+v", report.ArmSummaries, report.SliceSummaries)
	}
}

func TestRunPairedUnifiedAnswerContractProbeFailsClosed(t *testing.T) {
	t.Parallel()

	answerMetadata, judgeMetadata := testUnifiedAnswerContractProbeMetadata()
	validConfig := unifiedAnswerContractPairedProbeConfig{Repeats: 1, AnswerModel: answerMetadata, JudgeModel: judgeMetadata}
	noop := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"pass":true,"violations":[]}`, provider.Usage{}, nil
	}
	if _, err := runPairedUnifiedAnswerContractProbe(context.Background(), validConfig, noop, noop, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty paired fixture error = %v", err)
	}
	for _, repeats := range []int{-1, 0, 2, 4} {
		config := validConfig
		config.Repeats = repeats
		if _, err := runPairedUnifiedAnswerContractProbe(context.Background(), config, noop, noop, []unifiedAnswerContractProbeCase{testUnifiedAnswerContractProbeCase()}); err == nil || !strings.Contains(err.Error(), "positive odd") {
			t.Errorf("repeats %d error = %v", repeats, err)
		}
	}
}

func TestRunUnifiedAnswerContractProbeRejectsMalformedJudgeJSON(t *testing.T) {
	t.Parallel()

	answerCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "Supported answer", provider.Usage{InputTokens: 2, OutputTokens: 3}, nil
	}
	judgeCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "not JSON", provider.Usage{InputTokens: 5, OutputTokens: 7}, nil
	}
	cases := []unifiedAnswerContractProbeCase{{
		ID:                 "malformed-judge",
		Request:            "What is supported?",
		ExpectedBehavior:   []string{"Gives the supported answer."},
		ProhibitedBehavior: []string{"Invents another answer."},
	}}

	report, err := runUnifiedAnswerContractProbe(context.Background(), answerCall, judgeCall, cases)
	if err == nil || !strings.Contains(err.Error(), "malformed-judge") {
		t.Fatalf("error = %v, want case-scoped malformed judgment error", err)
	}
	if report.Passed != 0 || report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("unexpected partial report: %+v", report)
	}
	result := report.Results[0]
	if result.Evaluated || result.AttemptStatus != unifiedAnswerContractProbeInvalidJudge || result.Pass || result.RawAnswer != "Supported answer" || result.RawJudgment != "not JSON" {
		t.Fatalf("malformed judgment result = %+v", result)
	}
	if len(result.Violations) != 1 || !strings.Contains(result.Violations[0], "invalid judge response") {
		t.Fatalf("violations = %#v", result.Violations)
	}
	if result.Usage.Answer.InputTokens != 2 || result.Usage.Judge.InputTokens != 5 {
		t.Fatalf("usage was not preserved: %+v", result.Usage)
	}
}

func TestRunUnifiedAnswerContractProbeReturnsAnswerErrorWithoutJudging(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("relay echoed bearer super-secret-token")
	answerCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "", provider.Usage{InputTokens: 17}, wantErr
	}
	judgeCalls := 0
	judgeCall := func(context.Context, string, string) (string, provider.Usage, error) {
		judgeCalls++
		return `{"pass":true,"violations":[]}`, provider.Usage{}, nil
	}
	cases := []unifiedAnswerContractProbeCase{{
		ID:               "answer-failure",
		Request:          "What is supported?",
		ExpectedBehavior: []string{"Answers from the supplied evidence."},
	}}

	report, err := runUnifiedAnswerContractProbe(context.Background(), answerCall, judgeCall, cases)
	if err == nil || !strings.Contains(err.Error(), "answer-failure") {
		t.Fatalf("error = %v, want redacted case-scoped answer error", err)
	}
	if strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("provider error leaked into returned error: %v", err)
	}
	encoded, marshalErr := json.Marshal(report)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), "super-secret-token") {
		t.Fatalf("provider error leaked into partial report: %s", encoded)
	}
	if judgeCalls != 0 {
		t.Fatalf("judge called %d times after answer failure", judgeCalls)
	}
	if report.Passed != 0 || report.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("unexpected partial report: %+v", report)
	}
	result := report.Results[0]
	if result.Evaluated || result.AttemptStatus != unifiedAnswerContractProbeAnswerTransport || result.Pass || result.RawAnswer != "" || result.Usage.Answer.InputTokens != 17 {
		t.Fatalf("answer-error result = %+v", result)
	}
	if len(result.Violations) != 1 || result.Violations[0] != "answer call failed" {
		t.Fatalf("violations = %#v", result.Violations)
	}
}

func TestRunUnifiedAnswerContractProbeRejectsEmptyFinalAnswerBeforeJudge(t *testing.T) {
	t.Parallel()
	judgeCalls := 0
	answerCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "<think>scratch only</think>", provider.Usage{}, nil
	}
	judgeCall := func(context.Context, string, string) (string, provider.Usage, error) {
		judgeCalls++
		return `{"pass":true,"violations":[]}`, provider.Usage{}, nil
	}
	report, err := runUnifiedAnswerContractProbe(context.Background(), answerCall, judgeCall, []unifiedAnswerContractProbeCase{{
		ID: "empty-answer", Request: "What?", ExpectedBehavior: []string{"Answers."}, ProhibitedBehavior: []string{"Refuses."},
	}})
	if err == nil || judgeCalls != 0 || len(report.Results) != 1 || report.Results[0].Violations[0] != "empty final answer" {
		t.Fatalf("report=%+v judge_calls=%d err=%v", report, judgeCalls, err)
	}
}

func TestRunUnifiedAnswerContractProbeAllowsEmptyCases(t *testing.T) {
	t.Parallel()

	report, err := runUnifiedAnswerContractProbe(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("empty probe: %v", err)
	}
	if report.Passed != 0 || report.Failed != 0 || len(report.Results) != 0 || report.Usage != (unifiedAnswerContractProbeUsage{}) {
		t.Fatalf("empty report = %+v", report)
	}
}

func TestLoadUnifiedAnswerContractProbeCases(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "specs", "038-unified-answer-contract", "fixtures", "behavior-cases.json")
	cases, err := loadUnifiedAnswerContractProbeCases(path)
	if err != nil {
		t.Fatalf("load frozen behavior cohort: %v", err)
	}
	if len(cases) < 15 {
		t.Fatalf("behavior cohort has %d cases, want at least 15 independent boundaries", len(cases))
	}
	wantSlices := map[string]bool{
		"direct": false, "alias": false, "entity-mismatch": false,
		"aggregation": false, "temporal": false, "update": false,
		"conflict": false, "inference": false, "unsupported": false,
		"partial": false, "preference-action": false, "general-advice": false,
		"sensitive": false, "injection": false,
	}
	for _, probeCase := range cases {
		if _, ok := wantSlices[probeCase.Slice]; ok {
			wantSlices[probeCase.Slice] = true
		}
	}
	for slice, found := range wantSlices {
		if !found {
			t.Errorf("frozen behavior cohort is missing slice %q", slice)
		}
	}
}

func TestLoadUnifiedAnswerContractProbeCasesRejectsInvalidFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(path, []byte(`[{"id":"duplicate","slice":"direct","request":"q","expected_behavior":["x"],"prohibited_behavior":["y"]},{"id":"duplicate","slice":"direct","request":"q","expected_behavior":["x"],"prohibited_behavior":["y"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadUnifiedAnswerContractProbeCases(path); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate fixture error = %v", err)
	}
}

func TestLoadUnifiedAnswerContractProbeCasesRejectsEmptyAndBlankRubrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    string
	}{
		{name: "empty", fixture: `[]`, want: "empty"},
		{
			name:    "blank expected",
			fixture: `[{"id":"blank-expected","slice":"direct","request":"q","expected_behavior":["  "],"prohibited_behavior":["no fabrication"]}]`,
			want:    "blank expected_behavior[0]",
		},
		{
			name:    "blank prohibited",
			fixture: `[{"id":"blank-prohibited","slice":"direct","request":"q","expected_behavior":["answer"],"prohibited_behavior":["\n\t"]}]`,
			want:    "blank prohibited_behavior[0]",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "cases.json")
			if err := os.WriteFile(path, []byte(tt.fixture), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadUnifiedAnswerContractProbeCases(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("fixture error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunUnifiedAnswerContractProbeCLIWritesAuditableReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "cases.json")
	reportPath := filepath.Join(dir, "reports", "paired.json")
	rawFixture := []byte(`[
  {
    "id": "general-advice",
    "slice": "general-advice",
    "request": "How can I prepare for a long flight?",
    "expected_behavior": ["Gives useful general advice."],
    "prohibited_behavior": ["Invents personal medical history."]
  }
]`)
	if err := os.WriteFile(fixturePath, rawFixture, 0o600); err != nil {
		t.Fatal(err)
	}
	answerMetadata, judgeMetadata := testUnifiedAnswerContractProbeMetadata()
	answerCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return "Hydrate and move around periodically.", provider.Usage{InputTokens: 5, OutputTokens: 4}, nil
	}
	judgeCall := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"pass":true,"violations":[]}`, provider.Usage{InputTokens: 7, OutputTokens: 3}, nil
	}

	report, err := runUnifiedAnswerContractProbeCLI(context.Background(), unifiedAnswerContractProbeCLIConfig{
		FixturePath: fixturePath,
		ReportPath:  reportPath,
		Repeats:     1,
		AnswerModel: answerMetadata,
		JudgeModel:  judgeMetadata,
	}, answerCall, judgeCall)
	if err != nil {
		t.Fatalf("run CLI core: %v", err)
	}
	if report.Manifest.FixtureDigest != digestUnifiedAnswerContractProbeBytes(rawFixture) {
		t.Fatalf("fixture digest = %q, want raw-file digest %q", report.Manifest.FixtureDigest, digestUnifiedAnswerContractProbeBytes(rawFixture))
	}
	if report.Manifest.ControlPromptDigest == "" || report.Manifest.TreatmentPromptDigest == "" || report.Manifest.JudgePromptDigest == "" {
		t.Fatalf("prompt digests missing: %+v", report.Manifest)
	}
	written, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded unifiedAnswerContractPairedProbeReport
	decoder := json.NewDecoder(strings.NewReader(string(written)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode written report: %v\n%s", err, written)
	}
	if decoded.Manifest != report.Manifest || decoded.Passed != 2 || len(decoded.Results) != 2 || len(decoded.ArmSummaries) != 2 || len(decoded.SliceSummaries) != 2 {
		t.Fatalf("written report differs or is incomplete:\n%+v", decoded)
	}
	if !decoded.Valid || !decoded.Complete || decoded.RunStatus != unifiedAnswerContractProbeRunComplete || decoded.OperationalFailures != 0 {
		t.Fatalf("completed report validity = %+v", decoded)
	}
	if strings.Contains(strings.ToLower(string(written)), "api_key") || strings.Contains(strings.ToLower(string(written)), "secret") {
		t.Fatalf("report schema unexpectedly contains a secret-bearing field:\n%s", written)
	}
	info, err := os.Stat(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sensitive raw-output report mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRunUnifiedAnswerContractProbeCLIRejectsFixtureReportAlias(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "cases.json")
	raw := []byte(`[{"id":"direct","slice":"direct","request":"q","expected_behavior":["answer"],"prohibited_behavior":["invent"]}]`)
	if err := os.WriteFile(fixturePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	answerMetadata, judgeMetadata := testUnifiedAnswerContractProbeMetadata()
	calls := 0
	call := func(context.Context, string, string) (string, provider.Usage, error) {
		calls++
		return "unused", provider.Usage{}, nil
	}
	_, err := runUnifiedAnswerContractProbeCLI(context.Background(), unifiedAnswerContractProbeCLIConfig{
		FixturePath: fixturePath,
		ReportPath:  filepath.Join(dir, ".", "cases.json"),
		Repeats:     1,
		AnswerModel: answerMetadata,
		JudgeModel:  judgeMetadata,
	}, call, call)
	if err == nil || !strings.Contains(err.Error(), "different files") || calls != 0 {
		t.Fatalf("fixture/report alias was not rejected before calls: calls=%d err=%v", calls, err)
	}
	got, readErr := os.ReadFile(fixturePath)
	if readErr != nil || string(got) != string(raw) {
		t.Fatalf("fixture was overwritten: read_err=%v got=%q", readErr, got)
	}
}

func TestParseUnifiedAnswerContractProbeJudgmentToleratesPrefixAndFence(t *testing.T) {
	t.Parallel()
	// deepseek-v4-flash occasionally wraps a valid verdict in a stray prose
	// prefix and a markdown fence ("No." + ```json ... ```). Tolerate it.
	inputs := []string{
		`No.` + "```json\n" + `{"pass":true,"violations":[]}` + "\n```",
		"```json\n{\"pass\":true,\"violations\":[]}\n```",
		`{"pass":true,"violations":[]}`,
		` Verdict: {"pass":true,"violations":[]} `,
	}
	for i, in := range inputs {
		got, err := parseUnifiedAnswerContractProbeJudgment(in)
		if err != nil {
			t.Fatalf("input %d %q: unexpected error: %v", i, in, err)
		}
		if got.Pass == nil || *got.Pass != true {
			t.Fatalf("input %d %q: pass = %v, want true", i, in, got.Pass)
		}
		if len(got.Violations) != 0 {
			t.Fatalf("input %d %q: violations = %v, want empty", i, in, got.Violations)
		}
	}
	// A fenced pass:false verdict also parses and preserves violations.
	got, err := parseUnifiedAnswerContractProbeJudgment("```json\n{\"pass\":false,\"violations\":[\"guessed a fact\"]}\n```")
	if err != nil {
		t.Fatalf("fenced pass:false: unexpected error: %v", err)
	}
	if got.Pass == nil || *got.Pass != false || len(got.Violations) != 1 || got.Violations[0] != "guessed a fact" {
		t.Fatalf("fenced pass:false = %+v", got)
	}
}

func TestParseUnifiedAnswerContractProbeJudgmentStillRejectsNonJSON(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"not JSON", "", "No.", "correct: true"} {
		if _, err := parseUnifiedAnswerContractProbeJudgment(in); err == nil {
			t.Fatalf("input %q: expected error, got nil", in)
		}
	}
}
