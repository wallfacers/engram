package main

import (
	"path/filepath"
	"testing"
)

func TestPrepareFrozenEvalOptionsRejectsIRISAndForcesOneAnswerPath(t *testing.T) {
	protocol := testEvalProtocol()
	prepared, err := prepareFrozenEvalOptions(protocol, options{noIDKRetry: false})
	if err != nil {
		t.Fatalf("prepare formal options: %v", err)
	}
	if !prepared.noIDKRetry || prepared.iris {
		t.Fatalf("formal options = %+v, want legacy retry off and IRIS off", prepared)
	}
	if _, err := prepareFrozenEvalOptions(protocol, options{iris: true, noIDKRetry: true}); err == nil {
		t.Fatal("formal protocol unexpectedly accepted IRIS")
	}
	if _, err := prepareFrozenEvalOptions(protocol, options{rerank: true, noIDKRetry: true}); err == nil {
		t.Fatal("formal protocol unexpectedly accepted reranker")
	}
}

func TestPrepareFormalEvalRunPinsProtocolAndRefusesResumeDrift(t *testing.T) {
	manifestDir := t.TempDir()
	manifest, err := freezeEvalProtocol(manifestDir, testEvalProtocol(), evalRunFormal)
	if err != nil {
		t.Fatalf("freeze manifest: %v", err)
	}
	runDir := t.TempDir()
	got, prepared, err := prepareFormalEvalRun(filepath.Join(manifestDir, evalProtocolArtifactFile), runDir, options{})
	if err != nil {
		t.Fatalf("prepare formal run: %v", err)
	}
	if got.ProtocolHash != manifest.ProtocolHash || !prepared.noIDKRetry {
		t.Fatalf("prepared formal run = protocol=%q options=%+v", got.ProtocolHash, prepared)
	}
	if _, err := readFrozenEvalProtocol(runDir); err != nil {
		t.Fatalf("run protocol was not pinned: %v", err)
	}

	changed := testEvalProtocol()
	changed.Models.Answerer.PromptDigest = "sha256:different-answer-prompt"
	changedDir := t.TempDir()
	if _, err := freezeEvalProtocol(changedDir, changed, evalRunFormal); err != nil {
		t.Fatalf("freeze changed manifest: %v", err)
	}
	if _, _, err := prepareFormalEvalRun(filepath.Join(changedDir, evalProtocolArtifactFile), runDir, options{}); err == nil {
		t.Fatal("formal run unexpectedly accepted protocol drift on resume")
	}
}
