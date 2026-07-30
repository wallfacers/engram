package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func testB0ContinuityProtocol() evalProtocol {
	protocol := testEvalProtocol()
	opt := testB0ContinuityOptions()
	protocol.ProtocolID = "locomo-b0-continuity"
	protocol.Store = evalStoreProvenance{
		SchemaVersion:             7,
		IngestionRecipe:           "ledger_lossless_chunks_v2",
		IngestionConfigDigest:     evalJSONDigest(evalFreezeIngestion{Chunks: true}),
		ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"},
	}
	protocol.Models.Answerer.PromptDigest = formalAnswerPromptDigest(opt)
	protocol.Models.Judge.PromptDigest = evalTextDigest(judgeSystemPromptFor(opt.judgeAlignmentMode()))
	protocol.Retrieval = evalRetrievalProvenance{
		Recipe: "hybrid", EmbeddingFingerprint: evalEmbeddingFingerprint(), Reranker: "disabled",
		CandidateLimit: 30,
		CandidateRulesDigest: evalJSONDigest(evalFreezeCandidateRules{
			TopK: 30, ChunkQuota: 12, Chunks: true, Retrieval: "hybrid",
		}),
	}
	protocol.Budget = evalBudgetProtocol{
		Profile:             "continuity",
		AnswerInputTokenCap: 0,
		MaxOutputTokens:     8000,
		CandidateLimit:      protocol.Retrieval.CandidateLimit,
		RetrievalCallLimit:  3,
		AnswerCallLimit:     3,
		CounterFingerprint:  evalTextDigest("legacy-runtime-usage-only:no-preflight"),
	}
	protocol.Experiment = evalExperimentProtocol{
		Stage:          "b0",
		Arm:            "legacy_product_continuity",
		PrimaryCohort:  "all",
		MechanismFlags: map[string]bool{"idk_retry": true, "iris": false, "rerank": false},
	}
	return protocol
}

func testB0ContinuityOptions() options {
	return options{
		retrieval: "hybrid", chunks: true, chunkQuota: 12,
		topK: 30, repeats: 3, maxTokens: 8000,
	}
}

func TestB0ContinuityProtocolIsIndependentAndNeverPromotionEligible(t *testing.T) {
	protocol := testB0ContinuityProtocol()
	if err := validateEvalProtocol(protocol, evalRunB0Continuity); err != nil {
		t.Fatalf("valid B0 continuity protocol rejected: %v", err)
	}
	if err := validateEvalProtocol(protocol, evalRunFormal); err == nil {
		t.Fatal("B0 continuity protocol was accepted as a formal B1 protocol")
	}
	if isPromotionEligible(protocol, evalRunB0Continuity) {
		t.Fatal("B0 continuity must never be promotion eligible")
	}

	noRetry := protocol
	noRetry.Experiment.MechanismFlags = map[string]bool{"idk_retry": false}
	if err := validateEvalProtocol(noRetry, evalRunB0Continuity); err == nil {
		t.Fatal("B0 continuity accepted a manifest that disabled legacy retry")
	}
}

func TestPrepareB0ContinuityPinsOnlyItsManifest(t *testing.T) {
	manifestDir := t.TempDir()
	protocol, err := freezeEvalProtocol(manifestDir, testB0ContinuityProtocol(), evalRunB0Continuity)
	if err != nil {
		t.Fatalf("freeze B0 fixture: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, evalProtocolArtifactFile)
	runDir := t.TempDir()
	got, prepared, err := prepareB0ContinuityEvalRun(manifestPath, runDir, testB0ContinuityOptions())
	if err != nil {
		t.Fatalf("prepare B0 continuity run: %v", err)
	}
	if got.ProtocolHash != protocol.ProtocolHash || prepared.formalProtocol != nil ||
		prepared.formalReplay != nil || prepared.formalCalls != nil || prepared.noIDKRetry {
		t.Fatalf("prepared B0 state leaked formal machinery: protocol=%+v options=%+v", got, prepared)
	}
	if _, err := os.Stat(filepath.Join(runDir, evalProtocolArtifactFile)); err != nil {
		t.Fatalf("pinned B0 manifest missing: %v", err)
	}
	for _, name := range []string{
		evalCandidatesArtifactFile, evalTraceArtifactFile, evalBundleArtifactFile,
		formalFreezeJournalFile, formalCallJournalFile,
	} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Fatalf("B0 prepare created forbidden formal artifact %s", name)
		}
	}
}

func TestB0CallRecorderReportsActualLegacyRetryInvocations(t *testing.T) {
	recorder := newB0CallRecorder("sha256:protocol", 2)
	answer := recorder.wrapAnswer(func(context.Context, string, string) (string, provider.Usage, error) {
		return "answer", provider.Usage{InputTokens: 10, OutputTokens: 1}, nil
	})
	rewrite := recorder.wrapRewrite(func(context.Context, string, string) (string, error) {
		return "rewritten", nil
	})
	judge := recorder.wrapJudge(func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"correct":true}`, provider.Usage{}, nil
	})

	if _, _, err := answer(context.Background(), "system", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := rewrite(context.Background(), "system", "user"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := answer(context.Background(), "system", "retry"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := judge(context.Background(), "system", "judge"); err != nil {
		t.Fatal(err)
	}

	run := recorder.snapshot()
	if run.ProtocolHash != "sha256:protocol" || run.RunIndex != 2 {
		t.Fatalf("B0 recorder provenance = %+v", run)
	}
	if run.AnswerCalls != 2 || run.RewriteCalls != 1 || run.JudgeCalls != 1 || !run.LegacyRetry {
		t.Fatalf("B0 retry accounting = %+v, want answer=2 rewrite=1 judge=1 retry=true", run)
	}
}

func TestB0ContinuitySummaryUsesMajorityAndRefusesB1Artifacts(t *testing.T) {
	runDir := t.TempDir()
	protocol := testB0ContinuityProtocol()
	protocol.ProtocolHash = "sha256:b0"
	questionIDs := []string{"locomo:0:0", "locomo:0:1"}
	protocol.Benchmark.QuestionCount = len(questionIDs)
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)

	outcomes := [][]bool{{true, false}, {true, true}, {false, true}}
	runs := make([][]result, 0, len(outcomes))
	for runIndex, current := range outcomes {
		items := make([]result, 0, len(current))
		for questionIndex, correct := range current {
			answerCalls, rewriteCalls := 1, 0
			if runIndex == 1 && questionIndex == 0 {
				answerCalls, rewriteCalls = 2, 1
			}
			items = append(items, result{
				Conv: 0, Q: questionIndex, QuestionID: questionIDs[questionIndex], Correct: correct,
				B0Continuity: &evalB0ContinuityRun{
					Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, RunIndex: runIndex + 1,
					AnswerCalls: answerCalls, RewriteCalls: rewriteCalls, JudgeCalls: 1,
					LegacyRetry: answerCalls > 1 || rewriteCalls > 0,
				},
			})
		}
		runs = append(runs, items)
	}

	summary, err := materializeB0ContinuitySummary(runDir, protocol, runs, questionIDs)
	if err != nil {
		t.Fatalf("materialize B0 continuity summary: %v", err)
	}
	if !summary.Valid || summary.PromotionEligible || summary.Denominator != 2 ||
		summary.MajorityCorrect != 2 || summary.AnswerCalls != 7 ||
		summary.RewriteCalls != 1 || summary.JudgeCalls != 6 ||
		summary.QuestionsWithLegacyRetry != 1 {
		t.Fatalf("B0 continuity summary = %+v", summary)
	}
	for _, name := range []string{evalCandidatesArtifactFile, evalTraceArtifactFile, evalBundleArtifactFile} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Fatalf("B0 continuity generated forbidden B1 artifact %s", name)
		}
	}

	if err := os.WriteFile(filepath.Join(runDir, evalCandidatesArtifactFile), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeB0ContinuitySummary(runDir, protocol, runs, questionIDs); err == nil {
		t.Fatal("B0 continuity accepted a run directory containing B1 candidate artifacts")
	}
}

func TestB0ContinuitySummaryRefusesFormalPayloadOrMissingCallReceipt(t *testing.T) {
	protocol := testB0ContinuityProtocol()
	protocol.ProtocolHash = "sha256:b0"
	protocol.Benchmark.QuestionCount = 1
	questionIDs := []string{"locomo:0:0"}
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)
	runs := make([][]result, 3)
	for index := range runs {
		runs[index] = []result{{
			QuestionID: questionIDs[0],
			B0Continuity: &evalB0ContinuityRun{
				Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, RunIndex: index + 1,
				AnswerCalls: 1, JudgeCalls: 1,
			},
		}}
	}
	runs[0][0].Formal022 = &evalFormalQuestionRun{}
	if _, err := materializeB0ContinuitySummary(t.TempDir(), protocol, runs, questionIDs); err == nil {
		t.Fatal("B0 continuity accepted a B1 formal payload")
	}

	runs[0][0].Formal022 = nil
	runs[1][0].B0Continuity = nil
	if _, err := materializeB0ContinuitySummary(t.TempDir(), protocol, runs, questionIDs); err == nil {
		t.Fatal("B0 continuity accepted a missing per-question call receipt")
	}
}

func TestB0ContinuityValidateCLIIndependentlyRebuildsSummary(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "locomo.json")
	item := locomoItem{Conversation: json.RawMessage(`{}`), QA: make([]locomoQA, 1540)}
	for index := range item.QA {
		item.QA[index] = locomoQA{
			Question: fmt.Sprintf("question-%d", index), Answer: json.RawMessage(`"answer"`), Category: 1,
		}
	}
	if err := writeJSON(dataPath, []locomoItem{item}); err != nil {
		t.Fatal(err)
	}
	convs, err := loadBenchmarkDataset(dataPath, "locomo", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	questionIDs := formalQuestionIDs("locomo", convs)
	protocol := testB0ContinuityProtocol()
	protocol.Benchmark.DatasetDigest = evalTextDigest(string(raw))
	protocol.Benchmark.QuestionCount = len(questionIDs)
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(questionIDs)
	runDir := t.TempDir()
	protocol, err = freezeEvalProtocolFile(
		filepath.Join(runDir, evalProtocolArtifactFile), protocol, evalRunB0Continuity,
	)
	if err != nil {
		t.Fatal(err)
	}
	runs := make([][]result, protocol.Aggregation.AnswerRepetitions)
	for runIndex := range runs {
		runs[runIndex] = make([]result, 0, len(questionIDs))
		for questionIndex, questionID := range questionIDs {
			runs[runIndex] = append(runs[runIndex], result{
				Conv: 0, Q: questionIndex, QuestionID: questionID, Correct: true,
				B0Continuity: &evalB0ContinuityRun{
					Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, RunIndex: runIndex + 1,
					AnswerCalls: 1, JudgeCalls: 1,
				},
			})
		}
		resultDir := filepath.Join(runDir, fmt.Sprintf("run-%d", runIndex+1))
		if err := writeEvalJSONL(filepath.Join(resultDir, "results-hybrid.jsonl"), runs[runIndex]); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := materializeB0ContinuitySummary(runDir, protocol, runs, questionIDs)
	if err != nil {
		t.Fatal(err)
	}
	opt := options{evalValidate: runDir, dataPath: dataPath, datasetFormat: "locomo"}
	if err := runB0ContinuityValidateCLI(opt); err != nil {
		t.Fatalf("independent B0 read-back rejected valid run: %v", err)
	}

	summary.MajorityCorrect--
	if err := writeJSON(filepath.Join(runDir, evalB0ContinuitySummaryFile), summary); err != nil {
		t.Fatal(err)
	}
	if err := runB0ContinuityValidateCLI(opt); err == nil {
		t.Fatal("independent B0 read-back accepted a tampered persisted summary")
	}
}

func TestAnswerConversationB0UsesLegacyPathWithoutFormalPayload(t *testing.T) {
	ctx := context.Background()
	conv := conversation{
		ID:       0,
		Sessions: []session{{Index: 1, Turns: []turn{{Speaker: "user", Text: "I live in Oslo."}}}},
		QA: []locomoQA{{
			QuestionID: "locomo:0:0", Question: "Where does the user live?",
			Answer: []byte(`"Oslo"`), Category: 4,
		}},
	}
	protocol := testB0ContinuityProtocol()
	protocol.ProtocolHash = "sha256:b0"
	protocol.Retrieval.Recipe = "fts"
	protocol.Retrieval.CandidateLimit = 5
	protocol.Budget.CandidateLimit = 5
	opt := options{
		datasetFormat: "locomo", retrieval: "fts", topK: 5, storeDir: t.TempDir(),
		b0Protocol: &protocol, formalRunIndex: 1, answerModel: protocol.Models.Answerer.ID,
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
			return "", fmt.Errorf("malformed source ID")
		}
		sourceID := user[start : start+end]
		return fmt.Sprintf(`{"facts":[{"fact":"The user lives in Oslo.","source_ids":[%q],"entities":["Oslo"],"category":"user"}]}`, sourceID), nil
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extract, nil, []string{"fts"}, slog.Default())
	if err != nil {
		t.Fatalf("build B0 conversation runtime: %v", err)
	}
	defer runtime.Close()

	answerCalls, rewriteCalls, judgeCalls := 0, 0, 0
	answer := func(context.Context, string, string) (string, provider.Usage, error) {
		answerCalls++
		return "Oslo", provider.Usage{InputTokens: 12, OutputTokens: 1}, nil
	}
	rewrite := func(context.Context, string, string) (string, error) {
		rewriteCalls++
		return "unused", nil
	}
	judge := func(context.Context, string, string) (string, provider.Usage, error) {
		judgeCalls++
		return `{"correct":true}`, provider.Usage{}, nil
	}
	runDir := t.TempDir()
	j, err := openJournal(runDir, "fts")
	if err != nil {
		t.Fatal(err)
	}
	state := &armState{name: "fts", agg: newAggregator(), journal: j}
	if err := answerConversationWithUsage(ctx, opt, conv, runtime, answer, rewrite, rewrite, judge, []*armState{state}, slog.Default()); err != nil {
		j.Close()
		t.Fatalf("answer B0 conversation: %v", err)
	}
	j.Close()
	items, err := readResultsJSONL(filepath.Join(runDir, "results-fts.jsonl"))
	if err != nil || len(items) != 1 {
		t.Fatalf("read B0 results: items=%d err=%v", len(items), err)
	}
	if items[0].B0Continuity == nil || items[0].Formal022 != nil {
		t.Fatalf("B0 result payload = %+v", items[0])
	}
	receipt := items[0].B0Continuity
	if receipt.AnswerCalls != 1 || receipt.RewriteCalls != 0 || receipt.JudgeCalls != 1 || receipt.LegacyRetry {
		t.Fatalf("B0 result receipt = %+v", receipt)
	}
	if answerCalls != 1 || rewriteCalls != 0 || judgeCalls != 1 {
		t.Fatalf("actual calls answer=%d rewrite=%d judge=%d", answerCalls, rewriteCalls, judgeCalls)
	}
	for _, name := range []string{evalCandidatesArtifactFile, evalTraceArtifactFile, evalBundleArtifactFile} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Fatalf("B0 answer path generated forbidden artifact %s", name)
		}
	}
}
