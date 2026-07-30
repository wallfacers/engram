package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeValidEvalArtifactRun(t *testing.T) (string, evalProtocol) {
	t.Helper()
	runDir := t.TempDir()
	protocol, err := freezeEvalProtocol(runDir, testEvalProtocol(), evalRunFormal)
	if err != nil {
		t.Fatalf("freeze protocol: %v", err)
	}
	candidate := testCandidateArtifact()
	candidate.ProtocolHash = protocol.ProtocolHash
	if err := writeEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile), []evalCandidateArtifact{candidate}); err != nil {
		t.Fatalf("write candidates: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalTraceArtifactFile), []evalArtifactRecord{{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: candidate.QuestionID, Kind: evalTraceArtifactKind, Valid: true}}); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalBundleArtifactFile), []evalArtifactRecord{{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: candidate.QuestionID, Kind: evalBundleArtifactKind, Valid: true}}); err != nil {
		t.Fatalf("write bundles: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalClassificationArtifactFile), []evalArtifactRecord{{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: candidate.QuestionID, Kind: evalClassificationArtifactKind, Valid: true}}); err != nil {
		t.Fatalf("write classification: %v", err)
	}
	if err := writeEvalArtifactSummary(runDir, protocol, []string{candidate.QuestionID}); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	return runDir, protocol
}

func TestEvalArtifactRunValidationAcceptsCompleteAndRefusesMissingOrTampered(t *testing.T) {
	runDir, protocol := writeValidEvalArtifactRun(t)
	if _, err := validateEvalArtifactRun(runDir, protocol, []string{"locomo:1:2"}); err != nil {
		t.Fatalf("complete artifact run rejected: %v", err)
	}

	missingDir, missingProtocol := writeValidEvalArtifactRun(t)
	if err := os.Remove(filepath.Join(missingDir, evalBundleArtifactFile)); err != nil {
		t.Fatalf("remove bundle artifact: %v", err)
	}
	if _, err := validateEvalArtifactRun(missingDir, missingProtocol, []string{"locomo:1:2"}); err == nil {
		t.Fatal("missing required artifact unexpectedly accepted")
	}

	tamperedDir, tamperedProtocol := writeValidEvalArtifactRun(t)
	candidates, err := readEvalCandidateArtifacts(filepath.Join(tamperedDir, evalCandidatesArtifactFile))
	if err != nil {
		t.Fatalf("read candidates before tamper: %v", err)
	}
	candidates[0].QuestionID = "locomo:drifted"
	if err := writeEvalCandidateArtifacts(filepath.Join(tamperedDir, evalCandidatesArtifactFile), candidates); err != nil {
		t.Fatalf("rewrite tampered candidates: %v", err)
	}
	if _, err := validateEvalArtifactRun(tamperedDir, tamperedProtocol, []string{"locomo:1:2"}); err == nil {
		t.Fatal("tampered artifact unexpectedly accepted")
	}
}

func TestRunEvalArtifactValidateCLIUsesFrozenProtocolWithoutDataset(t *testing.T) {
	runDir, _ := writeValidEvalArtifactRun(t)
	if err := runEvalArtifactValidateCLI(runDir); err != nil {
		t.Fatalf("validate CLI helper rejected complete frozen run: %v", err)
	}
	if err := runEvalArtifactValidateCLI(filepath.Join(runDir, "missing")); err == nil {
		t.Fatal("validate CLI helper accepted missing run directory")
	}
}

func TestMaterializeFormalB1ArtifactsKeepsThreeAnswersInOneQuestion(t *testing.T) {
	runDir := t.TempDir()
	protocol, err := freezeEvalProtocol(runDir, testEvalProtocol(), evalRunFormal)
	if err != nil {
		t.Fatalf("freeze protocol: %v", err)
	}
	candidate := testCandidateArtifact()
	candidate.ProtocolHash = protocol.ProtocolHash
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: candidate.QuestionID, Kind: evalBundleArtifactKind, Valid: true},
		CandidateSetDigest: candidate.CandidateSetDigest, TraceDigest: trace.TraceDigest, SourceIDs: []string{"e-1", "e-2"},
		RenderedContext: "evidence", RenderedDigest: fixtureDigest("evidence"), AnswerInputTokens: 11, TokenCap: protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint, WithinCap: true, SourceValid: true, AnswerPromptDigest: fixtureDigest("system"),
	}
	runs := make([][]result, 0, 3)
	for index, correct := range []bool{true, false, true} {
		runs = append(runs, []result{{
			QuestionID: candidate.QuestionID, CategoryName: "temporal", Gold: "gold",
			Formal022: &evalFormalQuestionRun{Candidate: candidate, Trace: trace, Bundle: bundle, Answer: evalFormalAnswerRun{
				RunIndex: index + 1, Answer: "answer", AnswerDigest: fixtureDigest("answer"), JudgeCorrect: correct, AnswerCalls: 1, InputTokens: 11,
			}},
		}})
	}
	summary, err := materializeFormalB1Artifacts(runDir, protocol, runs)
	if err != nil {
		t.Fatalf("materialize formal artifacts: %v", err)
	}
	if !summary.Validity.isComplete() {
		t.Fatalf("formal artifact summary invalid: %+v", summary.Validity)
	}
	if _, err := validateEvalArtifactRun(runDir, protocol, []string{candidate.QuestionID}); err != nil {
		t.Fatalf("formal artifacts failed validator: %v", err)
	}
	if summary.Metrics.Questions != 1 || summary.Metrics.Correct != 1 || summary.Metrics.P95InputTokens != 11 {
		t.Fatalf("formal metrics = %+v, want one correct majority at 11 tokens", summary.Metrics)
	}
	var classifications []evalFormalClassificationRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalClassificationArtifactFile), &classifications); err != nil {
		t.Fatalf("read classifications: %v", err)
	}
	if len(classifications) != 1 || len(classifications[0].AnswerRuns) != 3 || !classifications[0].MajorityCorrect || classifications[0].AnswerCalls != 3 {
		t.Fatalf("classification = %+v, want one 3-run majority", classifications)
	}
}
