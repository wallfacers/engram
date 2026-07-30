package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func writeValidEvalArtifactRun(t *testing.T) (string, evalProtocol, []string) {
	t.Helper()
	runDir := t.TempDir()
	requested := testEvalProtocol()
	questionIDs := make([]string, requested.Benchmark.QuestionCount)
	for index := range questionIDs {
		questionIDs[index] = fmt.Sprintf("locomo:0:%04d", index)
	}
	requested.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)
	protocol, err := freezeEvalProtocol(runDir, requested, evalRunFormal)
	if err != nil {
		t.Fatalf("freeze protocol: %v", err)
	}
	candidates := make([]evalCandidateArtifact, 0, len(questionIDs))
	traces := make([]evalArtifactRecord, 0, len(questionIDs))
	bundles := make([]evalArtifactRecord, 0, len(questionIDs))
	classifications := make([]evalArtifactRecord, 0, len(questionIDs))
	for _, questionID := range questionIDs {
		candidate := testCandidateArtifact()
		candidate.ProtocolHash = protocol.ProtocolHash
		candidate.QuestionID = questionID
		candidates = append(candidates, candidate)
		traces = append(traces, evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalTraceArtifactKind, Valid: true})
		bundles = append(bundles, evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalBundleArtifactKind, Valid: true})
		classifications = append(classifications, evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: questionID, Kind: evalClassificationArtifactKind, Valid: true})
	}
	if err := writeEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile), candidates); err != nil {
		t.Fatalf("write candidates: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalTraceArtifactFile), traces); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalBundleArtifactFile), bundles); err != nil {
		t.Fatalf("write bundles: %v", err)
	}
	if err := writeEvalArtifactRecords(filepath.Join(runDir, evalClassificationArtifactFile), classifications); err != nil {
		t.Fatalf("write classification: %v", err)
	}
	if err := writeEvalArtifactSummary(runDir, protocol, questionIDs); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	return runDir, protocol, questionIDs
}

func TestEvalArtifactRunValidationAcceptsCompleteAndRefusesMissingOrTampered(t *testing.T) {
	runDir, protocol, questionIDs := writeValidEvalArtifactRun(t)
	if _, err := validateEvalArtifactRun(runDir, protocol, questionIDs); err != nil {
		t.Fatalf("complete artifact run rejected: %v", err)
	}

	missingDir, missingProtocol, missingIDs := writeValidEvalArtifactRun(t)
	if err := os.Remove(filepath.Join(missingDir, evalBundleArtifactFile)); err != nil {
		t.Fatalf("remove bundle artifact: %v", err)
	}
	if _, err := validateEvalArtifactRun(missingDir, missingProtocol, missingIDs); err == nil {
		t.Fatal("missing required artifact unexpectedly accepted")
	}

	tamperedDir, tamperedProtocol, tamperedIDs := writeValidEvalArtifactRun(t)
	candidates, err := readEvalCandidateArtifacts(filepath.Join(tamperedDir, evalCandidatesArtifactFile))
	if err != nil {
		t.Fatalf("read candidates before tamper: %v", err)
	}
	candidates[0].QuestionID = "locomo:drifted"
	if err := writeEvalCandidateArtifacts(filepath.Join(tamperedDir, evalCandidatesArtifactFile), candidates); err != nil {
		t.Fatalf("rewrite tampered candidates: %v", err)
	}
	if _, err := validateEvalArtifactRun(tamperedDir, tamperedProtocol, tamperedIDs); err == nil {
		t.Fatal("tampered artifact unexpectedly accepted")
	}
}

func TestRunEvalArtifactValidateCLIUsesFrozenProtocolWithoutDataset(t *testing.T) {
	runDir, _, _ := writeValidEvalArtifactRun(t)
	if err := runEvalArtifactValidateCLI(runDir); err != nil {
		t.Fatalf("validate CLI helper rejected complete frozen run: %v", err)
	}
	if err := runEvalArtifactValidateCLI(filepath.Join(runDir, "missing")); err == nil {
		t.Fatal("validate CLI helper accepted missing run directory")
	}
}

func TestMaterializeFormalB1ArtifactsKeepsThreeAnswersInOneQuestion(t *testing.T) {
	runDir := t.TempDir()
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	candidate := testCandidateArtifact()
	candidate.ProtocolHash = protocol.ProtocolHash
	protocol.Benchmark.QuestionCount = 1
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest([]string{candidate.QuestionID})
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := testFormalBundle(protocol, candidate, trace, "evidence", 11, fixtureDigest("system"))
	runs := make([][]result, 0, 3)
	for index, correct := range []bool{true, false, true} {
		runs = append(runs, []result{{
			QuestionID: candidate.QuestionID, CategoryName: "temporal", Gold: "gold",
			Formal022: &evalFormalQuestionRun{Candidate: candidate, Trace: trace, Bundle: bundle, Answer: evalFormalAnswerRun{
				RunIndex: index + 1, Answer: "answer", AnswerDigest: fixtureDigest("answer"), JudgeCorrect: correct,
				AnswerCalls: 1, JudgeCalls: 1, InputTokens: 11, CounterSource: protocol.Budget.CounterFingerprint,
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
	if summary.Metrics != nil {
		t.Fatalf("materializer exposed metrics before independent validation: %+v", summary.Metrics)
	}
	var classifications []evalFormalClassificationRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalClassificationArtifactFile), &classifications); err != nil {
		t.Fatalf("read classifications: %v", err)
	}
	if len(classifications) != 1 || len(classifications[0].AnswerRuns) != 3 || !classifications[0].MajorityCorrect || classifications[0].AnswerCalls != 3 {
		t.Fatalf("classification = %+v, want one 3-run majority", classifications)
	}
	metrics := formalClassificationMetrics(classifications)
	if metrics.Questions != 1 || metrics.Correct != 1 || metrics.P95InputTokens != 11 {
		t.Fatalf("formal metrics = %+v, want one correct majority at 11 tokens", metrics)
	}

	driftedRuns := append([][]result(nil), runs...)
	driftedSecond := *driftedRuns[1][0].Formal022
	driftedSecond.Candidate.CandidateSetDigest = "sha256:repetition-drift"
	driftedRuns[1] = append([]result(nil), driftedRuns[1]...)
	driftedRuns[1][0].Formal022 = &driftedSecond
	driftedSummary, err := materializeFormalB1Artifacts(runDir, protocol, driftedRuns)
	if err != nil {
		t.Fatalf("materialize drifted formal artifacts: %v", err)
	}
	if driftedSummary.Validity.isComplete() {
		t.Fatal("repetition drift unexpectedly produced a valid summary")
	}
	rawSummary, err := os.ReadFile(filepath.Join(runDir, evalSummaryArtifactFile))
	if err != nil {
		t.Fatalf("read drifted summary: %v", err)
	}
	if strings.Contains(string(rawSummary), `"metrics"`) {
		t.Fatalf("invalid formal summary exposed score metrics: %s", rawSummary)
	}

	badMetadataRuns := make([][]result, len(runs))
	for runIndex := range runs {
		badMetadataRuns[runIndex] = append([]result(nil), runs[runIndex]...)
		cloned := *badMetadataRuns[runIndex][0].Formal022
		cloned.Bundle.TokenCap++
		badMetadataRuns[runIndex][0].Formal022 = &cloned
	}
	badMetadataSummary, err := materializeFormalB1Artifacts(runDir, protocol, badMetadataRuns)
	if err != nil {
		t.Fatalf("materialize bad-metadata artifacts: %v", err)
	}
	if badMetadataSummary.Validity.isComplete() || badMetadataSummary.Metrics != nil {
		t.Fatalf("bad frozen metadata exposed formal metrics: %+v", badMetadataSummary)
	}

	badAnswerRuns := make([][]result, len(runs))
	for runIndex := range runs {
		badAnswerRuns[runIndex] = append([]result(nil), runs[runIndex]...)
		cloned := *badAnswerRuns[runIndex][0].Formal022
		cloned.Answer.AnswerDigest = fixtureDigest("different answer")
		badAnswerRuns[runIndex][0].Formal022 = &cloned
	}
	badAnswerSummary, err := materializeFormalB1Artifacts(runDir, protocol, badAnswerRuns)
	if err != nil {
		t.Fatalf("materialize bad-answer artifacts: %v", err)
	}
	if badAnswerSummary.Validity.isComplete() || badAnswerSummary.Metrics != nil {
		t.Fatalf("malformed answer metadata exposed formal metrics: %+v", badAnswerSummary)
	}
}

func TestMaterializeFormalB1ArtifactsRefusesProtocolDenominatorMismatch(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	protocol.Benchmark.QuestionCount = 2
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest([]string{"locomo:1:2", "locomo:1:3"})
	candidate := testCandidateArtifact()
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := testFormalBundle(protocol, candidate, trace, "evidence", 11, fixtureDigest("system"))
	runs := make([][]result, 3)
	for index := range runs {
		runs[index] = []result{{
			Conv: 1, Q: 2, QuestionID: candidate.QuestionID,
			Formal022: &evalFormalQuestionRun{
				Candidate: candidate,
				Trace:     trace,
				Bundle:    bundle,
				Answer: evalFormalAnswerRun{
					RunIndex: index + 1, Answer: "answer", AnswerDigest: fixtureDigest("answer"),
					JudgeCorrect: true, AnswerCalls: 1, JudgeCalls: 1, InputTokens: 11,
				},
			},
		}}
	}
	if _, err := materializeFormalB1Artifacts(t.TempDir(), protocol, runs); err == nil {
		t.Fatal("formal materialization accepted a stable but incomplete protocol denominator")
	}
}

// TestMaterializeFormalB1ArtifactsWritesNumericQuestionOrder guards the
// independent read-back contract. The frozen protocol digest and the no-model
// validator (runEvalArtifactValidateCLI) derive expected question IDs in
// dataset numeric order, but materializeFormalB1Artifacts previously iterated
// lexical map-keys when writing the immutable artifact arrays. With real,
// zero-padding-free question IDs (conv-0-q-1 ... conv-0-q-11) the two orderings
// diverge from q=10 onward, so the read-back rejected every multi-digit
// question set with "expected question ID digest differs from protocol". This
// test materializes a multi-digit set and asserts the written candidate order
// matches the numeric protocol order that read-back requires.
func TestMaterializeFormalB1ArtifactsWritesNumericQuestionOrder(t *testing.T) {
	runDir := t.TempDir()
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"

	const questionCount = 11 // q=10 and q=11 force lexical != numeric ordering
	questionIDs := make([]string, 0, questionCount)
	for q := 1; q <= questionCount; q++ {
		questionIDs = append(questionIDs, questionID(0, q))
	}
	protocol.Benchmark.QuestionCount = questionCount
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)

	runs := make([][]result, 0, 3)
	for index := 0; index < 3; index++ {
		row := make([]result, 0, questionCount)
		for q := 1; q <= questionCount; q++ {
			id := questionID(0, q)
			candidate := testCandidateArtifact()
			candidate.ProtocolHash = protocol.ProtocolHash
			candidate.QuestionID = id
			trace := buildFormalTrace(protocol, id, candidate)
			bundle := testFormalBundle(protocol, candidate, trace, "evidence", 11, fixtureDigest("system"))
			row = append(row, result{
				Conv: 0, Q: q, QuestionID: id, CategoryName: "single-hop", Gold: "gold",
				Formal022: &evalFormalQuestionRun{
					Candidate: candidate, Trace: trace, Bundle: bundle,
					Answer: evalFormalAnswerRun{
						RunIndex:      index + 1,
						Answer:        "answer",
						AnswerDigest:  fixtureDigest("answer"),
						JudgeCorrect:  true,
						AnswerCalls:   1,
						JudgeCalls:    1,
						InputTokens:   11,
						CounterSource: protocol.Budget.CounterFingerprint,
					},
				},
			})
		}
		runs = append(runs, row)
	}

	if _, err := materializeFormalB1Artifacts(runDir, protocol, runs); err != nil {
		t.Fatalf("materialize formal artifacts: %v", err)
	}

	candidates, err := readEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile))
	if err != nil {
		t.Fatalf("read materialized candidates: %v", err)
	}
	writtenIDs := candidateQuestionIDs(candidates)
	if evalJSONDigest(writtenIDs) != protocol.Benchmark.QuestionIDsDigest {
		t.Fatalf("materialized candidate order diverges from numeric protocol order:\n  written=%v\n  numeric=%v", writtenIDs, questionIDs)
	}
}

func TestFormalRepeatScoresRemainPendingUntilFullValidation(t *testing.T) {
	protocol := testEvalProtocol()
	if formalRepeatScoresVisible(options{formalProtocol: &protocol}) {
		t.Fatal("formal repetition exposed an unvalidated percentage")
	}
	if !formalRepeatScoresVisible(options{}) {
		t.Fatal("legacy non-formal repetition unexpectedly suppressed its report")
	}
}

func TestFormalQuestionReplayMaterializesOnceAcrossThreeAnswerRuns(t *testing.T) {
	protocol := testEvalProtocol()
	candidate := testCandidateArtifact()
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := testFormalBundle(protocol, candidate, trace, "frozen evidence", 17, fixtureDigest("system"))

	replay := newFormalQuestionReplay()
	key := resultKey{Conv: 3, Q: 7}
	materializations := 0
	var digests []string
	for range 3 {
		frozen, err := replay.getOrMaterialize(key, candidate.QuestionID, func() formalFrozenQuestion {
			materializations++
			drifted := candidate
			drifted.CandidateSetDigest = fmt.Sprintf("candidate-call-%d", materializations)
			return formalFrozenQuestion{
				Candidate: drifted,
				Trace:     trace,
				Bundle:    bundle,
			}
		})
		if err != nil {
			t.Fatalf("get frozen question: %v", err)
		}
		digests = append(digests, evalJSONDigest(frozen))
	}

	if materializations != 1 {
		t.Fatalf("materializations = %d, want exactly one retrieve/pack", materializations)
	}
	for index := 1; index < len(digests); index++ {
		if digests[index] != digests[0] {
			t.Fatalf("repetition %d frozen digest = %q, want %q", index+1, digests[index], digests[0])
		}
	}
}

func TestFormalQuestionReplaySingleflightsConcurrentSameKey(t *testing.T) {
	replay := newFormalQuestionReplay()
	key := resultKey{Conv: 8, Q: 9}
	candidate := testCandidateArtifact()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var materializations atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			frozen, err := replay.getOrMaterialize(key, candidate.QuestionID, func() formalFrozenQuestion {
				materializations.Add(1)
				once.Do(func() { close(entered) })
				<-release
				return formalFrozenQuestion{Candidate: candidate}
			})
			if err == nil && evalJSONDigest(frozen.Candidate) != evalJSONDigest(candidate) {
				err = fmt.Errorf("concurrent replay returned a different candidate")
			}
			errs <- err
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := materializations.Load(); got != 1 {
		t.Fatalf("concurrent materializations=%d, want 1", got)
	}
}

func TestFormalQuestionReplayPersistsBeforeAnswerAndResumesWithoutMaterializing(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	candidate := testCandidateArtifact()
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := testFormalBundle(protocol, candidate, trace, "resume evidence", 19, fixtureDigest("system"))
	key := resultKey{Conv: 4, Q: 2}

	runDir := t.TempDir()
	replay, err := openFormalQuestionReplay(runDir, protocol.ProtocolHash)
	if err != nil {
		t.Fatalf("open replay: %v", err)
	}
	first, err := replay.getOrMaterialize(key, candidate.QuestionID, func() formalFrozenQuestion {
		return formalFrozenQuestion{Candidate: candidate, Trace: trace, Bundle: bundle}
	})
	if err != nil {
		t.Fatalf("persist frozen question: %v", err)
	}
	if err := replay.Close(); err != nil {
		t.Fatalf("close replay: %v", err)
	}

	resumed, err := openFormalQuestionReplay(runDir, protocol.ProtocolHash)
	if err != nil {
		t.Fatalf("reopen replay: %v", err)
	}
	defer resumed.Close() //nolint:errcheck

	materializations := 0
	frozen, err := resumed.getOrMaterialize(key, candidate.QuestionID, func() formalFrozenQuestion {
		materializations++
		return formalFrozenQuestion{}
	})
	if err != nil {
		t.Fatalf("resume frozen question: %v", err)
	}
	if materializations != 0 {
		t.Fatalf("resume rematerialized frozen evidence %d times", materializations)
	}
	if got := evalJSONDigest(frozen); got != evalJSONDigest(first) {
		t.Fatalf("resumed frozen digest = %q, want %q", got, evalJSONDigest(first))
	}
	if got := evalJSONDigest(frozen.Candidate); got != evalJSONDigest(candidate) {
		t.Fatalf("resumed candidate digest = %q, want %q", got, evalJSONDigest(candidate))
	}
	if got := evalJSONDigest(frozen.Trace); got != evalJSONDigest(trace) {
		t.Fatalf("resumed trace digest = %q, want %q", got, evalJSONDigest(trace))
	}
	if got := evalJSONDigest(frozen.Bundle); got != evalJSONDigest(bundle) {
		t.Fatalf("resumed bundle digest = %q, want %q", got, evalJSONDigest(bundle))
	}
}

func TestFormalQuestionReplayRefusesMalformedOrCrossProtocolResume(t *testing.T) {
	runDir := t.TempDir()
	replay, err := openFormalQuestionReplay(runDir, "sha256:protocol-a")
	if err != nil {
		t.Fatalf("open replay: %v", err)
	}
	candidate := testCandidateArtifact()
	candidate.ProtocolHash = "sha256:protocol-a"
	if _, err := replay.getOrMaterialize(resultKey{Conv: 1, Q: 1}, candidate.QuestionID, func() formalFrozenQuestion {
		return formalFrozenQuestion{Candidate: candidate}
	}); err != nil {
		t.Fatalf("persist replay: %v", err)
	}
	if err := replay.Close(); err != nil {
		t.Fatalf("close replay: %v", err)
	}
	if _, err := openFormalQuestionReplay(runDir, "sha256:protocol-b"); err == nil {
		t.Fatal("cross-protocol replay unexpectedly resumed")
	}
	f, err := os.OpenFile(filepath.Join(runDir, formalFreezeJournalFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open replay journal for corruption: %v", err)
	}
	if _, err := f.WriteString(`{"torn":`); err != nil {
		_ = f.Close()
		t.Fatalf("corrupt replay journal: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close corrupted replay journal: %v", err)
	}
	if _, err := openFormalQuestionReplay(runDir, "sha256:protocol-a"); err == nil {
		t.Fatal("malformed replay journal unexpectedly resumed")
	}
}

func TestSeedFormalQuestionReplayRefusesMalformedFirstRunJournal(t *testing.T) {
	runDir := t.TempDir()
	path := filepath.Join(runDir, "results-fts.jsonl")
	if err := os.WriteFile(path, []byte(`{"partial":`), 0o644); err != nil {
		t.Fatalf("write malformed first-run journal: %v", err)
	}
	j, err := openJournal(runDir, "fts")
	if err != nil {
		t.Fatalf("legacy journal open should retain resume compatibility: %v", err)
	}
	defer j.Close()
	if err := seedFormalQuestionReplay(newFormalQuestionReplay(), j); err == nil {
		t.Fatal("formal replay accepted a malformed first-run result journal")
	}
}

func TestFormalCallJournalRefusesAmbiguousStartedCallAndReplaysTerminalResult(t *testing.T) {
	protocolHash := "sha256:protocol"
	key := resultKey{Conv: 2, Q: 4}
	questionID := "locomo:2:4"
	frozenDigest := formalRunFrozenDigest(evalFormalQuestionRun{})
	inputDigest := fixtureDigest("input")

	ambiguousDir := t.TempDir()
	ambiguous, err := openFormalCallJournal(ambiguousDir, protocolHash)
	if err != nil {
		t.Fatalf("open ambiguous call journal: %v", err)
	}
	if err := ambiguous.Begin(key, questionID, 2, frozenDigest, inputDigest); err != nil {
		t.Fatalf("begin ambiguous call: %v", err)
	}
	if err := ambiguous.Close(); err != nil {
		t.Fatalf("close ambiguous call journal: %v", err)
	}
	ambiguous, err = openFormalCallJournal(ambiguousDir, protocolHash)
	if err != nil {
		t.Fatalf("reopen ambiguous call journal: %v", err)
	}
	ambiguousRunDir := filepath.Join(ambiguousDir, "run-2")
	if err := os.MkdirAll(ambiguousRunDir, 0o755); err != nil {
		t.Fatalf("create ambiguous result directory: %v", err)
	}
	emptyResults, err := openJournal(ambiguousRunDir, "hybrid")
	if err != nil {
		t.Fatalf("open empty result journal: %v", err)
	}
	if err := ambiguous.Reconcile(2, emptyResults); err == nil {
		t.Fatal("resume accepted STARTED without a terminal call record")
	}
	emptyResults.Close()
	_ = ambiguous.Close()

	terminalDir := t.TempDir()
	calls, err := openFormalCallJournal(terminalDir, protocolHash)
	if err != nil {
		t.Fatalf("open terminal call journal: %v", err)
	}
	if err := calls.Begin(key, questionID, 2, frozenDigest, inputDigest); err != nil {
		t.Fatalf("begin terminal call: %v", err)
	}
	terminal := result{
		Conv: key.Conv, Q: key.Q, QuestionID: questionID, Correct: true,
		Formal022: &evalFormalQuestionRun{Answer: evalFormalAnswerRun{
			RunIndex: 2, Answer: "answer", AnswerDigest: fixtureDigest("answer"),
			JudgeCorrect: true, AnswerCalls: 1, JudgeCalls: 1, InputTokens: 12,
		}},
	}
	if err := calls.Finish(key, questionID, 2, frozenDigest, inputDigest, terminal); err != nil {
		t.Fatalf("finish terminal call: %v", err)
	}
	if err := calls.Close(); err != nil {
		t.Fatalf("close terminal call journal: %v", err)
	}
	calls, err = openFormalCallJournal(terminalDir, protocolHash)
	if err != nil {
		t.Fatalf("reopen terminal call journal: %v", err)
	}
	defer calls.Close() //nolint:errcheck
	terminalRunDir := filepath.Join(terminalDir, "run-2")
	if err := os.MkdirAll(terminalRunDir, 0o755); err != nil {
		t.Fatalf("create terminal result directory: %v", err)
	}
	replayedResults, err := openJournal(terminalRunDir, "hybrid")
	if err != nil {
		t.Fatalf("open replay result journal: %v", err)
	}
	defer replayedResults.Close()
	if err := calls.Reconcile(2, replayedResults); err != nil {
		t.Fatalf("reconcile terminal result: %v", err)
	}
	replayed, ok := replayedResults.lookup(key)
	if !ok || evalJSONDigest(replayed) != evalJSONDigest(terminal) {
		t.Fatalf("terminal result was not byte-replayed: ok=%t got=%+v", ok, replayed)
	}
}

func TestFormalCallJournalRefusesProviderCallWithoutStartedIntent(t *testing.T) {
	calls, err := openFormalCallJournal(t.TempDir(), "sha256:protocol")
	if err != nil {
		t.Fatalf("open formal call journal: %v", err)
	}
	defer calls.Close() //nolint:errcheck
	key := resultKey{Conv: 1, Q: 2}
	run := evalFormalQuestionRun{
		Answer:         evalFormalAnswerRun{RunIndex: 1, AnswerCalls: 1},
		InvalidReasons: []string{"answer_preflight_or_runtime_failed"},
	}
	item := result{Conv: key.Conv, Q: key.Q, QuestionID: "locomo:1:2", Formal022: &run}
	if err := calls.FailWithoutStart(
		key, item.QuestionID, 1, formalRunFrozenDigest(run), fixtureDigest("input"), item,
	); err == nil {
		t.Fatal("failed terminal with a provider call was accepted without STARTED")
	}
}

func TestFormalCallJournalReplaysFailedProviderCallsWithoutRetry(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		answerRuns int
		judgeRuns  int
		reason     string
	}{
		{name: "answer failure", answerRuns: 1, reason: "answer_preflight_or_runtime_failed"},
		{name: "judge failure", answerRuns: 1, judgeRuns: 1, reason: "judge_failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runDir := t.TempDir()
			protocolHash := "sha256:protocol"
			key := resultKey{Conv: 1, Q: 2}
			run := evalFormalQuestionRun{
				Answer: evalFormalAnswerRun{
					RunIndex: 1, Answer: "answer", AnswerDigest: fixtureDigest("answer"),
					AnswerCalls: testCase.answerRuns, JudgeCalls: testCase.judgeRuns,
				},
				InvalidReasons: []string{testCase.reason},
			}
			frozenDigest := formalRunFrozenDigest(run)
			inputDigest := fixtureDigest("input")
			item := result{Conv: key.Conv, Q: key.Q, QuestionID: "locomo:1:2", Formal022: &run}

			calls, err := openFormalCallJournal(runDir, protocolHash)
			if err != nil {
				t.Fatalf("open formal call journal: %v", err)
			}
			if err := calls.Begin(key, item.QuestionID, 1, frozenDigest, inputDigest); err != nil {
				t.Fatalf("begin provider call: %v", err)
			}
			if err := calls.Finish(key, item.QuestionID, 1, frozenDigest, inputDigest, item); err != nil {
				t.Fatalf("finish failed provider call: %v", err)
			}
			if err := calls.Close(); err != nil {
				t.Fatalf("close formal call journal: %v", err)
			}

			calls, err = openFormalCallJournal(runDir, protocolHash)
			if err != nil {
				t.Fatalf("reopen formal call journal: %v", err)
			}
			defer calls.Close() //nolint:errcheck
			resultDir := filepath.Join(runDir, "run-1")
			if err := os.MkdirAll(resultDir, 0o755); err != nil {
				t.Fatalf("create result directory: %v", err)
			}
			results, err := openJournal(resultDir, "fts")
			if err != nil {
				t.Fatalf("open result journal: %v", err)
			}
			defer results.Close()
			if err := calls.Reconcile(1, results); err != nil {
				t.Fatalf("replay failed terminal: %v", err)
			}
			replayed, ok := results.lookup(key)
			if !ok || evalJSONDigest(replayed) != evalJSONDigest(item) {
				t.Fatalf("failed terminal was not replayed: ok=%t result=%+v", ok, replayed)
			}
			if err := calls.Begin(key, item.QuestionID, 1, frozenDigest, inputDigest); err == nil {
				t.Fatal("resume accepted a retry after a terminal provider failure")
			}
		})
	}
}

func TestAnswerConversationFormalReplaySurvivesSourceDeletionAfterFirstRun(t *testing.T) {
	ctx := context.Background()
	conv := conversation{
		ID:       0,
		Sessions: []session{{Index: 1, Turns: []turn{{Speaker: "user", Text: "I live in Oslo."}}}},
		QA: []locomoQA{{
			QuestionID: "locomo:0:0",
			Question:   "The user lives in Oslo.",
			Answer:     []byte(`"Oslo"`),
			Category:   4,
		}},
	}
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	protocol.Retrieval.Recipe = "fts"
	protocol.Retrieval.CandidateLimit = 5
	protocol.Budget.CandidateLimit = 5
	protocol.Budget.AnswerInputTokenCap = 100_000
	protocol.Budget.CounterFingerprint = "sha256:length"
	formalDir := t.TempDir()
	replay, err := openFormalQuestionReplay(formalDir, protocol.ProtocolHash)
	if err != nil {
		t.Fatalf("open formal replay: %v", err)
	}
	callJournal, err := openFormalCallJournal(formalDir, protocol.ProtocolHash)
	if err != nil {
		t.Fatalf("open formal call journal: %v", err)
	}
	defer func() {
		_ = callJournal.Close()
		_ = replay.Close()
	}()
	opt := options{
		datasetFormat:      "locomo",
		retrieval:          "fts",
		topK:               5,
		storeDir:           t.TempDir(),
		noIDKRetry:         true,
		formalProtocol:     &protocol,
		formalCounter:      lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
		formalQuestionGate: make(chan struct{}, 1),
		formalReplay:       replay,
		formalCalls:        callJournal,
		answerModel:        protocol.Models.Answerer.ID,
	}
	extract := func(_ context.Context, _, user string) (string, error) {
		const marker = "[source_id="
		start := strings.Index(user, marker)
		if start < 0 {
			return "", fmt.Errorf("extraction prompt has no source ID")
		}
		start += len(marker)
		end := strings.IndexByte(user[start:], ']')
		if end < 0 {
			return "", fmt.Errorf("extraction prompt has malformed source ID")
		}
		sourceID := user[start : start+end]
		return fmt.Sprintf(`{"facts":[{"fact":"The user lives in Oslo.","source_ids":[%q],"entities":["Oslo"],"category":"user"}]}`, sourceID), nil
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extract, nil, []string{"fts"}, slog.Default())
	if err != nil {
		t.Fatalf("build conversation runtime: %v", err)
	}
	defer runtime.Close()

	var mu sync.Mutex
	var answerInputs []string
	answerCalls, judgeCalls := 0, 0
	answer := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		mu.Lock()
		answerCalls++
		answerInputs = append(answerInputs, system+"\x00"+user)
		mu.Unlock()
		return "Oslo", provider.Usage{InputTokens: len([]rune(system + user)), OutputTokens: 1}, nil
	}
	judge := func(context.Context, string, string) (string, provider.Usage, error) {
		mu.Lock()
		judgeCalls++
		mu.Unlock()
		return `{"correct":true}`, provider.Usage{}, nil
	}
	filter := func(context.Context, string, string) (string, error) { return "", nil }
	var runs []evalFormalQuestionRun
	for runIndex := 1; runIndex <= 3; runIndex++ {
		runDir := t.TempDir()
		j, err := openJournal(runDir, "fts")
		if err != nil {
			t.Fatalf("open run %d journal: %v", runIndex, err)
		}
		state := &armState{name: "fts", agg: newAggregator(), journal: j}
		runOpt := opt
		runOpt.formalRunIndex = runIndex
		if err := callJournal.Reconcile(runIndex, j); err != nil {
			j.Close()
			t.Fatalf("pre-run reconcile %d: %v", runIndex, err)
		}
		if err := answerConversationWithUsage(ctx, runOpt, conv, runtime, answer, filter, filter, judge, []*armState{state}, slog.Default()); err != nil {
			j.Close()
			t.Fatalf("answer run %d: %v", runIndex, err)
		}
		if err := callJournal.Reconcile(runIndex, j); err != nil {
			j.Close()
			t.Fatalf("reconcile run %d: %v", runIndex, err)
		}
		j.Close()
		items, err := readResultsJSONL(filepath.Join(runDir, "results-fts.jsonl"))
		if err != nil || len(items) != 1 || items[0].Formal022 == nil {
			t.Fatalf("run %d journal items=%d err=%v", runIndex, len(items), err)
		}
		runs = append(runs, *items[0].Formal022)

		if runIndex == 1 {
			entries, err := runtime.entries.List(ctx)
			if err != nil || len(entries) == 0 {
				t.Fatalf("list entries after first freeze: entries=%d err=%v", len(entries), err)
			}
			for _, entry := range entries {
				if err := runtime.entries.Delete(ctx, entry.Name); err != nil {
					t.Fatalf("delete %q after first freeze: %v", entry.Name, err)
				}
			}

			if err := callJournal.Close(); err != nil {
				t.Fatalf("close call journal before resume: %v", err)
			}
			if err := replay.Close(); err != nil {
				t.Fatalf("close replay before resume: %v", err)
			}
			replay, err = openFormalQuestionReplay(formalDir, protocol.ProtocolHash)
			if err != nil {
				t.Fatalf("reopen formal replay: %v", err)
			}
			callJournal, err = openFormalCallJournal(formalDir, protocol.ProtocolHash)
			if err != nil {
				t.Fatalf("reopen formal call journal: %v", err)
			}
			opt.formalReplay = replay
			opt.formalCalls = callJournal

			resumedJournal, err := openJournal(runDir, "fts")
			if err != nil {
				t.Fatalf("reopen first result journal: %v", err)
			}
			resumedState := &armState{name: "fts", agg: newAggregator(), journal: resumedJournal}
			parity, err := openContextParityJournal(runDir)
			if err != nil {
				resumedJournal.Close()
				t.Fatalf("open empty legacy parity journal: %v", err)
			}
			resumeOpt := opt
			resumeOpt.formalRunIndex = 1
			resumeOpt.contextParity = parity
			if err := callJournal.Reconcile(1, resumedJournal); err != nil {
				_ = parity.Close()
				resumedJournal.Close()
				t.Fatalf("reconcile first result after reopen: %v", err)
			}
			if err := validateContextParityResume(resumeOpt, []conversation{conv}, []*armState{resumedState}); err != nil {
				_ = parity.Close()
				resumedJournal.Close()
				t.Fatalf("formal resume incorrectly required legacy context parity: %v", err)
			}
			beforeAnswers, beforeJudges := answerCalls, judgeCalls
			if err := answerConversationWithUsage(ctx, resumeOpt, conv, runtime, answer, filter, filter, judge, []*armState{resumedState}, slog.Default()); err != nil {
				_ = parity.Close()
				resumedJournal.Close()
				t.Fatalf("resume first run: %v", err)
			}
			if answerCalls != beforeAnswers || judgeCalls != beforeJudges {
				t.Fatalf("resume repeated provider calls: answer %d→%d judge %d→%d", beforeAnswers, answerCalls, beforeJudges, judgeCalls)
			}
			if err := parity.Close(); err != nil {
				resumedJournal.Close()
				t.Fatalf("close parity journal: %v", err)
			}
			resumedJournal.Close()
		}
	}

	if answerCalls != 3 || judgeCalls != 3 {
		t.Fatalf("answer/judge calls=%d/%d, want 3/3; runs=%+v", answerCalls, judgeCalls, runs)
	}
	if len(answerInputs) != 3 || answerInputs[1] != answerInputs[0] || answerInputs[2] != answerInputs[0] {
		t.Fatalf("answer inputs were not byte-identical: %#v", answerInputs)
	}
	firstFrozenDigest := evalJSONDigest(formalFrozenQuestion{Candidate: runs[0].Candidate, Trace: runs[0].Trace, Bundle: runs[0].Bundle})
	for index, run := range runs[1:] {
		digest := evalJSONDigest(formalFrozenQuestion{Candidate: run.Candidate, Trace: run.Trace, Bundle: run.Bundle})
		if digest != firstFrozenDigest {
			t.Fatalf("run %d frozen digest=%q, want %q", index+2, digest, firstFrozenDigest)
		}
	}
}
