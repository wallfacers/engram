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
