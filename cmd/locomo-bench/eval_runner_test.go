package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
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

func TestAdmitFormalQuestionBoundsPackAdmissionAndRespectsCancellation(t *testing.T) {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := admitFormalQuestion(canceled, gate); err == nil {
		t.Fatal("full formal question gate ignored canceled context")
	}

	<-gate
	release, err := admitFormalQuestion(context.Background(), gate)
	if err != nil {
		t.Fatalf("admit available formal question gate: %v", err)
	}
	release()
	if len(gate) != 0 {
		t.Fatalf("formal question gate leaked admission: len=%d", len(gate))
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
		convs[0].QA[index].Category = 1
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

func TestFormalCandidateSourcesUseLedgerEvidenceIDs(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	entries := memory.NewEntryStore(st.DB())
	evidence, err := entries.Ledger().AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "conv0-sess1",
		Speaker:          "Caroline",
		Ordinal:          0,
		Content:          "Caroline: the ledger-backed answer is retrievable.",
		RecordedAt:       time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	entry := &memory.Entry{
		Name:            "ledger-backed-fact",
		Trigger:         "ledger-backed retrievable",
		Content:         "The ledger-backed answer is retrievable.",
		Category:        "fact",
		SourceSessionID: "conv0-sess1",
	}
	if err := entries.UpsertWithSources(ctx, entry, []memory.EvidenceRef{{EvidenceID: evidence[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatal(err)
	}
	hits, err := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil).Search(ctx, "retrievable", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("Search = %#v, %v", hits, err)
	}

	sources, err := formalCandidateSources(ctx, memory.NewProjectionStore(st.DB()), hits)
	if err != nil {
		t.Fatal(err)
	}
	if got := sources[hits[0].Name]; len(got) != 1 || got[0] != evidence[0].ID {
		t.Fatalf("candidate source IDs = %#v, want Ledger Evidence %q", got, evidence[0].ID)
	}

	protocol := testEvalProtocol()
	qa := locomoQA{QuestionID: "q-source", Question: "retrievable", Evidence: []string{"D1:1"}}
	candidate := buildFormalCandidateArtifact(protocol, qa, hits, map[string][]string{hits[0].Name: {"D1:1"}}, sources, map[string]string{"D1:1": evidence[0].ID})
	if got := candidate.Gold.ResolvedEvidenceIDs; len(got) != 1 || got[0] != evidence[0].ID {
		t.Fatalf("resolved gold evidence = %#v, want Ledger Evidence %q", got, evidence[0].ID)
	}
	trace := buildFormalTrace(protocol, qa.QuestionID, candidate)
	bundle := buildFormalBundle(protocol, qa.QuestionID, candidate, trace, hits, sources, evidencecompiler.AnswerInput{System: "system", User: "question and evidence"})
	if !bundle.SourceValid || len(bundle.SourceIDs) != 1 || bundle.SourceIDs[0] != evidence[0].ID {
		t.Fatalf("formal bundle = %+v, want valid Ledger Evidence lineage", bundle)
	}

	answerCalls, judgeCalls := 0, 0
	correct, predicted, _, run := runFormalB1Question(
		ctx, protocol,
		options{answerModel: protocol.Models.Answerer.ID, formalCounter: formalCounter{count: 12, fingerprint: protocol.Budget.CounterFingerprint}},
		memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil), memory.NewProjectionStore(st.DB()),
		func(context.Context, string, string) (string, provider.Usage, error) {
			answerCalls++
			return "ledger-backed answer", provider.Usage{InputTokens: 12, OutputTokens: 2}, nil
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			judgeCalls++
			return `{"correct":true}`, provider.Usage{}, nil
		},
		qa, map[string][]string{hits[0].Name: {"D1:1"}}, map[string]string{"D1:1": evidence[0].ID}, 0,
	)
	if !correct || predicted != "ledger-backed answer" || answerCalls != 1 || judgeCalls != 1 || len(run.InvalidReasons) != 0 || !run.Bundle.SourceValid {
		t.Fatalf("formal Ledger run = correct=%t predicted=%q answers=%d judges=%d artifact=%+v", correct, predicted, answerCalls, judgeCalls, run)
	}

	// A counter failure while testing the rendered hit must fail the budget
	// admission, but it must not be relabeled as missing candidate lineage.
	// The candidate above has already proved its direct Evidence source.
	answerCalls, judgeCalls = 0, 0
	_, _, _, failed := runFormalB1Question(
		ctx, protocol,
		options{answerModel: protocol.Models.Answerer.ID, formalCounter: evidenceFailCounter{fingerprint: protocol.Budget.CounterFingerprint}},
		memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil), memory.NewProjectionStore(st.DB()),
		func(context.Context, string, string) (string, provider.Usage, error) {
			answerCalls++
			return "unexpected", provider.Usage{}, nil
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			judgeCalls++
			return `{"correct":true}`, provider.Usage{}, nil
		},
		qa, map[string][]string{hits[0].Name: {"D1:1"}}, map[string]string{"D1:1": evidence[0].ID}, 0,
	)
	if answerCalls != 0 || judgeCalls != 0 {
		t.Fatalf("budget preflight failure made model calls: answers=%d judges=%d", answerCalls, judgeCalls)
	}
	if !hasInvalidReason(failed.InvalidReasons, "answer_input_budget_impossible") || !hasInvalidReason(failed.InvalidReasons, "no_evidence_fits_token_cap") {
		t.Fatalf("budget preflight failure reasons = %v", failed.InvalidReasons)
	}
	if hasInvalidReason(failed.InvalidReasons, "source_lineage_unavailable") {
		t.Fatalf("valid candidate lineage was mislabeled after budget failure: %v", failed.InvalidReasons)
	}
}

func TestAnswerFrozenFormalB1QuestionReplaysExactBytesAndDoesNotMutateFreeze(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	qa := locomoQA{QuestionID: "locomo:1:2", Question: "When did Alice move?", Category: 2}
	system := withCurrentDateRule(answerPromptForRegime(qa.Category, false, false, false), qa.QuestionDate)
	candidate := testCandidateArtifact()
	trace := buildFormalTrace(protocol, qa.QuestionID, candidate)
	bundle := evalFormalBundleRecord{
		evalArtifactRecord: evalArtifactRecord{
			Schema:       evalProtocolSchema,
			ProtocolHash: protocol.ProtocolHash,
			QuestionID:   qa.QuestionID,
			Kind:         evalBundleArtifactKind,
			Valid:        true,
		},
		CandidateSetDigest: candidate.CandidateSetDigest,
		TraceDigest:        trace.TraceDigest,
		SourceIDs:          []string{"e-1"},
		RenderedContext:    "QUESTION:\nWhen did Alice move?\n\nMEMORIES:\nAlice moved in 2023.",
		RenderedDigest:     evalTextDigest("QUESTION:\nWhen did Alice move?\n\nMEMORIES:\nAlice moved in 2023."),
		AnswerInputTokens:  12,
		TokenCap:           protocol.Budget.AnswerInputTokenCap,
		CounterFingerprint: protocol.Budget.CounterFingerprint,
		WithinCap:          true,
		SourceValid:        true,
		AnswerPromptDigest: evalTextDigest(system),
	}
	frozen := formalFrozenQuestion{Candidate: candidate, Trace: trace, Bundle: bundle}
	frozenDigest := evalJSONDigest(frozen)
	answerCalls, judgeCalls := 0, 0
	var runDigests []string
	for runIndex := 1; runIndex <= 3; runIndex++ {
		correct, _, _, run := answerFrozenFormalB1Question(
			context.Background(), protocol,
			options{formalCounter: formalCounter{count: 12, fingerprint: protocol.Budget.CounterFingerprint}},
			func(_ context.Context, gotSystem, gotUser string) (string, provider.Usage, error) {
				answerCalls++
				if gotSystem != system || gotUser != bundle.RenderedContext {
					t.Fatalf("answer input drifted: system=%q user=%q", gotSystem, gotUser)
				}
				return fmt.Sprintf("answer-%d", runIndex), provider.Usage{InputTokens: 12, OutputTokens: 2}, nil
			},
			func(context.Context, string, string) (string, provider.Usage, error) {
				judgeCalls++
				return `{"correct":true}`, provider.Usage{}, nil
			},
			qa, frozen, runIndex,
		)
		if !correct || run.Answer.RunIndex != runIndex || run.Answer.AnswerCalls != 1 {
			t.Fatalf("run %d = %+v, correct=%t", runIndex, run, correct)
		}
		runDigests = append(runDigests, evalJSONDigest(struct {
			Candidate evalCandidateArtifact
			Trace     evalFormalTraceRecord
			Bundle    evalFormalBundleRecord
		}{run.Candidate, run.Trace, run.Bundle}))
	}
	if answerCalls != 3 || judgeCalls != 3 {
		t.Fatalf("answer calls=%d judge calls=%d, want 3 each", answerCalls, judgeCalls)
	}
	if evalJSONDigest(frozen) != frozenDigest {
		t.Fatal("answer repetition mutated the frozen question")
	}
	for index := 1; index < len(runDigests); index++ {
		if runDigests[index] != runDigests[0] {
			t.Fatalf("run %d freeze digest drifted: %q != %q", index+1, runDigests[index], runDigests[0])
		}
	}

	_, _, _, failed := answerFrozenFormalB1Question(
		context.Background(), protocol,
		options{formalCounter: formalCounter{count: 12, fingerprint: protocol.Budget.CounterFingerprint}},
		func(context.Context, string, string) (string, provider.Usage, error) {
			return "", provider.Usage{}, fmt.Errorf("answer unavailable")
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			t.Fatal("judge called after answer failure")
			return "", provider.Usage{}, nil
		},
		qa, frozen, 1,
	)
	if failed.Trace.Valid != trace.Valid || failed.Bundle.Valid != bundle.Valid || evalJSONDigest(frozen) != frozenDigest {
		t.Fatalf("answer failure mutated frozen validity: trace=%t bundle=%t", failed.Trace.Valid, failed.Bundle.Valid)
	}
}

func hasInvalidReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

type formalCounter struct {
	count       int
	fingerprint string
}

type lengthCounter struct{ fingerprint string }

type evidenceFailCounter struct{ fingerprint string }

func (counter lengthCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{InputTokens: len([]rune(input.System + input.User)), Fingerprint: counter.fingerprint}, nil
}

func (counter formalCounter) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if counter.count < 1 {
		return evidencecompiler.TokenCount{}, fmt.Errorf("counter unavailable")
	}
	return evidencecompiler.TokenCount{InputTokens: counter.count, Fingerprint: counter.fingerprint}, nil
}

func (counter evidenceFailCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if strings.Contains(input.User, "ledger-backed answer") {
		return evidencecompiler.TokenCount{}, fmt.Errorf("temporary counter failure")
	}
	return evidencecompiler.TokenCount{InputTokens: 12, Fingerprint: counter.fingerprint}, nil
}
