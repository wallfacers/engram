package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
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

func TestCallFormalAnswerPreflightsExactInputAndFailsClosedOnDrift(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.Budget.AnswerInputTokenCap = 16
	protocol.Budget.CounterFingerprint = "sha256:counter"
	input := evidencecompiler.AnswerInput{Model: "answerer-r1", System: "system", User: "question and evidence"}
	counter := formalCounter{count: 12, fingerprint: protocol.Budget.CounterFingerprint}
	calls := 0
	answer := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		calls++
		if system != input.System || user != input.User {
			t.Fatalf("answer input = (%q, %q), want exact preflight input", system, user)
		}
		return "answer", provider.Usage{InputTokens: 12, OutputTokens: 3}, nil
	}
	got, usage, count, err := callFormalAnswer(context.Background(), protocol, counter, input, answer)
	if err != nil || got != "answer" || calls != 1 || usage.InputTokens != 12 || count.InputTokens != 12 {
		t.Fatalf("formal answer = (%q, %+v, %+v, %v), calls=%d", got, usage, count, err, calls)
	}

	calls = 0
	_, _, _, err = callFormalAnswer(context.Background(), protocol, formalCounter{count: 17, fingerprint: protocol.Budget.CounterFingerprint}, input, answer)
	if err == nil || calls != 0 {
		t.Fatalf("over-cap preflight err=%v calls=%d, want error before answer call", err, calls)
	}

	calls = 0
	_, _, _, err = callFormalAnswer(context.Background(), protocol, counter, input, func(context.Context, string, string) (string, provider.Usage, error) {
		calls++
		return "answer", provider.Usage{InputTokens: 13}, nil
	})
	if err == nil || calls != 1 {
		t.Fatalf("runtime drift err=%v calls=%d, want drift error after one call", err, calls)
	}
}

func TestFormalRunnerOptionsAndDatasetFingerprintFailClosed(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.Retrieval.Recipe = "hybrid"
	if err := validateFormalRunnerOptions(protocol, options{repeats: 3, topK: 30}, []string{"hybrid"}); err != nil {
		t.Fatalf("valid formal options rejected: %v", err)
	}
	if err := validateFormalRunnerOptions(protocol, options{repeats: 3, topK: 30, multiQuery: true}, []string{"hybrid"}); err == nil {
		t.Fatal("formal options unexpectedly accepted multi-query")
	}
	if err := validateFormalRunnerOptions(protocol, options{repeats: 3, topK: 30}, []string{"fts", "hybrid"}); err == nil {
		t.Fatal("formal options unexpectedly accepted multiple arms")
	}

	dataPath := filepath.Join(t.TempDir(), "dataset.json")
	raw := []byte(`[{"fixture":true}]`)
	if err := os.WriteFile(dataPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	convs := []conversation{{ID: 0, QA: make([]locomoQA, 1540)}}
	questionIDs := make([]string, 0, 1540)
	for index := range convs[0].QA {
		id := fmt.Sprintf("q-%d", index)
		convs[0].QA[index].QuestionID = id
		questionIDs = append(questionIDs, id)
	}
	protocol.Benchmark.DatasetDigest = evalTextDigest(string(raw))
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)
	if err := verifyFormalDataset(protocol, dataPath, "locomo", convs); err != nil {
		t.Fatalf("matching formal dataset rejected: %v", err)
	}
	convs[0].QA[1].QuestionID = "drifted"
	if err := verifyFormalDataset(protocol, dataPath, "locomo", convs); err == nil {
		t.Fatal("formal dataset unexpectedly accepted question-id drift")
	}
}

func TestPackFormalLegacyInputUsesExactCounterBeforeAnswer(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.Budget.AnswerInputTokenCap = 80
	protocol.Budget.CounterFingerprint = "sha256:length"
	qa := locomoQA{Question: "where", Category: 4}
	hits := []memory.Result{
		{Name: "one", Content: "first short memory"},
		{Name: "two", Content: "second memory that makes the complete prompt too long"},
	}
	counter := lengthCounter{fingerprint: protocol.Budget.CounterFingerprint}
	selected, input, count, err := packFormalLegacyInput(context.Background(), protocol, counter, "system", qa, hits, false)
	if err != nil {
		t.Fatalf("pack formal input: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "one" || count.InputTokens > protocol.Budget.AnswerInputTokenCap || input.User == "" {
		t.Fatalf("packed selected=%v count=%+v input=%+v", selected, count, input)
	}
	_, _, _, err = packFormalLegacyInput(context.Background(), protocol, counter, strings.Repeat("s", 100), qa, hits, false)
	if err == nil {
		t.Fatal("static over-cap prompt unexpectedly accepted")
	}
}

type formalCounter struct {
	count       int
	fingerprint string
}

type lengthCounter struct{ fingerprint string }

func (counter lengthCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{InputTokens: len([]rune(input.System + input.User)), Fingerprint: counter.fingerprint}, nil
}

func (counter formalCounter) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if counter.count < 1 {
		return evidencecompiler.TokenCount{}, fmt.Errorf("counter unavailable")
	}
	return evidencecompiler.TokenCount{InputTokens: counter.count, Fingerprint: counter.fingerprint}, nil
}
