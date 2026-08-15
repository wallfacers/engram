package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
)

func TestRunFixedGoldOracleQuestionUsesOnlyAllActiveGoldEvidence(t *testing.T) {
	protocol := fixedGoldTestProtocol()
	first := fixedGoldTestEvidence("e-1", "D1:1", "Alice moved to Paris.", 1)
	second := fixedGoldTestEvidence("e-2", "D2:3", "She moved there for a design job.", 2)
	reader := &fixedGoldTestReader{evidence: map[string]memory.Evidence{
		first.ID:  first,
		second.ID: second,
	}}
	qa := locomoQA{
		QuestionID: "locomo:0:0",
		Question:   "Where did Alice move and why?",
		Answer:     json.RawMessage(`"Paris for a design job"`),
		Evidence:   []string{"D1:1", "D2:3"},
		Category:   1,
	}
	counter := &fixedGoldTestCounter{count: 137, fingerprint: protocol.Budget.CounterFingerprint}
	answerCalls, judgeCalls := 0, 0
	answer := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		answerCalls++
		wantSystem := answerPromptForRegime(qa.Category, false, false, false, false)
		if system != wantSystem {
			t.Fatalf("oracle answer system prompt drifted")
		}
		if !strings.Contains(user, first.Content) || !strings.Contains(user, second.Content) {
			t.Fatalf("oracle prompt omitted raw gold Evidence: %q", user)
		}
		if strings.Index(user, first.Content) > strings.Index(user, second.Content) {
			t.Fatalf("oracle prompt changed dataset gold order: %q", user)
		}
		return "Paris for a design job", provider.Usage{InputTokens: 137, OutputTokens: 8}, nil
	}
	judge := func(_ context.Context, system, user string) (string, provider.Usage, error) {
		judgeCalls++
		if system != judgeSystemPromptFor("strict") ||
			user != buildJudgePrompt(qa.Question, goldFor(qa), "Paris for a design job") {
			t.Fatalf("oracle judge input drifted: system=%q user=%q", system, user)
		}
		return `{"correct":true}`, provider.Usage{}, nil
	}

	got := runFixedGoldOracleQuestion(
		context.Background(), protocol, options{formalCounter: counter},
		reader, map[string]string{"D1:1": first.ID, "D2:3": second.ID},
		qa, answer, judge,
	)

	if !got.Valid || !got.DiagnosticOnly || got.Stage != evalStageFixedGoldOracle {
		t.Fatalf("oracle diagnostic identity = %+v", got)
	}
	if got.ControlProtocolHash != protocol.ProtocolHash || got.RetrievalCalls != 0 {
		t.Fatalf("oracle control/retrieval = %+v", got)
	}
	if answerCalls != protocol.Aggregation.AnswerRepetitions ||
		judgeCalls != protocol.Aggregation.AnswerRepetitions {
		t.Fatalf("answer/judge calls=%d/%d, want %d each", answerCalls, judgeCalls, protocol.Aggregation.AnswerRepetitions)
	}
	if reader.calls != 1 || counter.calls != 1 {
		t.Fatalf("reader/counter calls=%d/%d, want one frozen materialization", reader.calls, counter.calls)
	}
	if len(reader.requested) != 2 || reader.requested[0] != first.ID || reader.requested[1] != second.ID {
		t.Fatalf("reader requested IDs=%v, want dataset-mapped gold order", reader.requested)
	}
	if got.OracleDiagnostic == nil || got.OracleDiagnostic.Correct != 1 ||
		got.OracleDiagnostic.Denominator != 1 || !got.OracleDiagnostic.MajorityCorrect ||
		got.OracleDiagnostic.CorrectRepetitions != 3 || got.OracleDiagnostic.Repetitions != 3 {
		t.Fatalf("oracle outcome = %+v", got.OracleDiagnostic)
	}
	if got.ContributesToPromotion() {
		t.Fatal("fixed-gold oracle diagnostic became promotion-eligible")
	}
}

func TestFixedGoldAnswerInputUsesUnifiedAnswerContract(t *testing.T) {
	protocol := fixedGoldTestProtocol()
	qa := locomoQA{
		QuestionID: "generic-contract-case",
		Question:   "Which subject did the user ask about?",
		Category:   8,
	}
	input := buildFixedGoldAnswerInput(
		protocol,
		options{unifiedAnswerContract: true},
		qa,
		nil,
		nil,
	)
	if input.System != unifiedAnswerContractPrompt {
		t.Fatalf("fixed-gold path bypassed unified answer contract: %q", input.System)
	}
}

func TestFixedGoldOracleModeIsExclusive(t *testing.T) {
	if err := validateFixedGoldOracleMode(options{}); err != nil {
		t.Fatalf("non-oracle options unexpectedly rejected: %v", err)
	}
	conflicts := map[string]options{
		"compare":             {compareSpec: "a,b"},
		"eval validate":       {evalValidate: "run"},
		"protocol freeze":     {evalFreezeProtocol: "protocol.json"},
		"token calibration":   {tokenCounterCalibrate: true},
		"estimate":            {estimate: true},
		"recall diagnostic":   {recallDiagnostic: true},
		"doc2query build":     {doc2queryBuild: true},
		"doc2query arm":       {doc2query: doc2queryTreatment},
		"alias shadow arm":    {aliasShadow: aliasShadowTreatment},
		"temporal diagnostic": {temporalDiagnostic: true},
		"attribution trace":   {attributionTrace: true},
		"conflict resolver":   {conflictResolution: true},
		"pcic selector":       {pcic: true},
		"pcic annotate":       {pcicAnnotate: true},
		"abstain probe":       {abstainProbe: true},
		"coverage only":       {coverageOnly: true},
	}
	for name, conflict := range conflicts {
		t.Run(name, func(t *testing.T) {
			conflict.fixedGoldOracle = true
			if err := validateFixedGoldOracleMode(conflict); err == nil {
				t.Fatal("fixed-gold oracle accepted a conflicting execution mode")
			}
		})
	}
}

func TestFixedGoldControlRequiresB1LegacyThreeRepeatProtocol(t *testing.T) {
	valid := fixedGoldTestProtocol()
	if err := validateFixedGoldControlProtocol(valid); err != nil {
		t.Fatalf("valid fixed-gold control rejected: %v", err)
	}
	for name, mutate := range map[string]func(*evalProtocol){
		"stage": func(protocol *evalProtocol) {
			protocol.Experiment.Stage = "b0"
		},
		"arm": func(protocol *evalProtocol) {
			protocol.Experiment.Arm = "compiler"
		},
		"derived control": func(protocol *evalProtocol) {
			protocol.Experiment.ControlProtocolHash = fixtureDigest("parent")
		},
		"one repetition": func(protocol *evalProtocol) {
			protocol.Aggregation.AnswerRepetitions = 1
		},
		"missing mechanism": func(protocol *evalProtocol) {
			delete(protocol.Experiment.MechanismFlags, "rerank")
		},
		"enabled mechanism": func(protocol *evalProtocol) {
			protocol.Experiment.MechanismFlags["iris"] = true
		},
		"suffixed retrieval recipe": func(protocol *evalProtocol) {
			protocol.Retrieval.Recipe = "hybrid+rerank"
		},
	} {
		t.Run(name, func(t *testing.T) {
			protocol := fixedGoldTestProtocol()
			mutate(&protocol)
			fixedGoldSealProtocol(&protocol)
			if err := validateFixedGoldControlProtocol(protocol); err == nil {
				t.Fatal("fixed-gold oracle accepted a non-control protocol")
			}
		})
	}
}

func TestRunFixedGoldOracleQuestionFailsClosedBeforeModels(t *testing.T) {
	active := fixedGoldTestEvidence("e-1", "D1:1", "gold raw turn", 1)
	inactive := active
	inactive.State = memory.EvidenceTombstoned

	tests := []struct {
		name       string
		qa         locomoQA
		mapping    map[string]string
		reader     fixedGoldEvidenceReader
		counter    *fixedGoldTestCounter
		wantReason string
	}{
		{
			name: "unresolved dataset gold",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Evidence: []string{"D1:1"}, Category: 1},
			mapping:    map[string]string{},
			reader:     &fixedGoldTestReader{evidence: map[string]memory.Evidence{active.ID: active}},
			counter:    &fixedGoldTestCounter{count: 50, fingerprint: "sha256:counter"},
			wantReason: "gold_evidence_unresolved",
		},
		{
			name: "missing active evidence",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Evidence: []string{"D1:1"}, Category: 1},
			mapping:    map[string]string{"D1:1": active.ID},
			reader:     &fixedGoldTestReader{err: errors.New("not found")},
			counter:    &fixedGoldTestCounter{count: 50, fingerprint: "sha256:counter"},
			wantReason: "gold_evidence_unavailable",
		},
		{
			name: "reader returns inactive evidence",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Evidence: []string{"D1:1"}, Category: 1},
			mapping:    map[string]string{"D1:1": active.ID},
			reader:     &fixedGoldTestReader{evidence: map[string]memory.Evidence{inactive.ID: inactive}},
			counter:    &fixedGoldTestCounter{count: 50, fingerprint: "sha256:counter"},
			wantReason: "gold_evidence_inactive",
		},
		{
			name: "mapping points at a different raw turn",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Evidence: []string{"D9:9"}, Category: 1},
			mapping:    map[string]string{"D9:9": active.ID},
			reader:     &fixedGoldTestReader{evidence: map[string]memory.Evidence{active.ID: active}},
			counter:    &fixedGoldTestCounter{count: 50, fingerprint: "sha256:counter"},
			wantReason: "gold_evidence_mapping_mismatch",
		},
		{
			name: "all raw evidence exceeds cap",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Evidence: []string{"D1:1"}, Category: 1},
			mapping:    map[string]string{"D1:1": active.ID},
			reader:     &fixedGoldTestReader{evidence: map[string]memory.Evidence{active.ID: active}},
			counter:    &fixedGoldTestCounter{count: 1101, fingerprint: "sha256:counter"},
			wantReason: "answer_input_budget_impossible",
		},
		{
			name: "ordinary question cannot have empty gold",
			qa: locomoQA{QuestionID: "q", Question: "question", Answer: json.RawMessage(`"answer"`),
				Category: 1},
			mapping:    map[string]string{},
			reader:     &fixedGoldTestReader{},
			counter:    &fixedGoldTestCounter{count: 50, fingerprint: "sha256:counter"},
			wantReason: "gold_evidence_empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			protocol := fixedGoldTestProtocol()
			answerCalls, judgeCalls := 0, 0
			got := runFixedGoldOracleQuestion(
				context.Background(), protocol, options{formalCounter: test.counter},
				test.reader, test.mapping, test.qa,
				func(context.Context, string, string) (string, provider.Usage, error) {
					answerCalls++
					return "answer", provider.Usage{InputTokens: test.counter.count}, nil
				},
				func(context.Context, string, string) (string, provider.Usage, error) {
					judgeCalls++
					return `{"correct":true}`, provider.Usage{}, nil
				},
			)
			if got.Valid || got.OracleDiagnostic != nil || answerCalls != 0 || judgeCalls != 0 {
				t.Fatalf("fail-closed oracle = %+v, model calls=%d/%d", got, answerCalls, judgeCalls)
			}
			if !fixedGoldHasReason(got.InvalidReasons, test.wantReason) {
				t.Fatalf("invalid reasons=%v, want %q", got.InvalidReasons, test.wantReason)
			}
		})
	}
}

func TestRunFixedGoldOracleQuestionAllowsOnlyLongMemEvalAbstentionEmptyGold(t *testing.T) {
	protocol := fixedGoldTestProtocol()
	protocol.Benchmark.Name = "longmemeval_s"
	protocol.Benchmark.Split = "cleaned_full_500"
	protocol.Benchmark.QuestionCount = 500
	protocol.Models.Answerer.PromptDigest = formalAnswerPromptDigest(options{abstainPrompt: true})
	fixedGoldSealProtocol(&protocol)
	qa := locomoQA{
		QuestionID:   "lme-abstention-1",
		Question:     "What is the user's blood type?",
		Answer:       json.RawMessage(`"I don't know"`),
		QuestionType: "abstention",
		Category:     adversarialCategory,
		Adversarial:  true,
	}
	reader := &fixedGoldTestReader{}
	counter := &fixedGoldTestCounter{count: 91, fingerprint: protocol.Budget.CounterFingerprint}
	answerCalls, judgeCalls := 0, 0
	got := runFixedGoldOracleQuestion(
		context.Background(), protocol, options{formalCounter: counter, abstainPrompt: true},
		reader, nil, qa,
		func(_ context.Context, _, user string) (string, provider.Usage, error) {
			answerCalls++
			if !strings.Contains(user, "RETRIEVED MEMORIES:\n(none)") {
				t.Fatalf("empty abstention did not render the same empty context: %q", user)
			}
			return canonicalAbstainDecline, provider.Usage{InputTokens: 91}, nil
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			judgeCalls++
			return `{"correct":true}`, provider.Usage{}, nil
		},
	)
	if !got.Valid || got.OracleDiagnostic == nil || !got.OracleDiagnostic.MajorityCorrect {
		t.Fatalf("valid LongMemEval empty-gold abstention rejected: %+v", got)
	}
	if reader.calls != 0 || answerCalls != 3 || judgeCalls != 3 {
		t.Fatalf("reader/answer/judge calls=%d/%d/%d", reader.calls, answerCalls, judgeCalls)
	}

	protocol.Benchmark.Name = "locomo"
	protocol.Benchmark.Split = "category_1_4"
	protocol.Benchmark.QuestionCount = 1540
	fixedGoldSealProtocol(&protocol)
	got = runFixedGoldOracleQuestion(
		context.Background(), protocol, options{formalCounter: counter, abstainPrompt: true},
		reader, nil, qa,
		func(context.Context, string, string) (string, provider.Usage, error) {
			t.Fatal("LoCoMo empty gold reached answer model")
			return "", provider.Usage{}, nil
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			t.Fatal("LoCoMo empty gold reached judge")
			return "", provider.Usage{}, nil
		},
	)
	if got.Valid || !fixedGoldHasReason(got.InvalidReasons, "gold_evidence_empty") {
		t.Fatalf("non-LongMemEval empty gold accepted: %+v", got)
	}
}

func TestFixedGoldOracleFreezesFilesBeforeCallsAndRefusesOverwrite(t *testing.T) {
	protocol := fixedGoldTestProtocol()
	runDir := t.TempDir()
	files, err := createFixedGoldOracleRunFiles(runDir, protocol, protocol.Benchmark.QuestionCount)
	if err != nil {
		t.Fatalf("freeze oracle files: %v", err)
	}
	for _, name := range []string{
		evalFixedGoldOracleArtifactFile,
		evalFixedGoldOracleJournalFile,
		evalFixedGoldOracleSummaryFile,
	} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("%s was not frozen before calls: %v", name, err)
		}
	}

	evidence := fixedGoldTestEvidence("e-1", "D1:1", "gold raw turn", 1)
	counter := &fixedGoldTestCounter{count: 50, fingerprint: protocol.Budget.CounterFingerprint}
	answerCalls := 0
	record := runFixedGoldOracleQuestionWithJournal(
		context.Background(), protocol, options{formalCounter: counter},
		&fixedGoldTestReader{evidence: map[string]memory.Evidence{evidence.ID: evidence}},
		map[string]string{"D1:1": evidence.ID},
		locomoQA{
			QuestionID: "q-failure", Question: "question", Answer: json.RawMessage(`"answer"`),
			Evidence: []string{"D1:1"}, Category: 1,
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			answerCalls++
			for _, name := range []string{
				evalFixedGoldOracleArtifactFile,
				evalFixedGoldOracleJournalFile,
				evalFixedGoldOracleSummaryFile,
			} {
				if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
					t.Fatalf("model reached before %s freeze: %v", name, err)
				}
			}
			return "", provider.Usage{}, errors.New("provider failed")
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			t.Fatal("answer failure reached judge")
			return "", provider.Usage{}, nil
		},
		files.journal,
	)
	if answerCalls != 1 || record.Valid || record.OracleDiagnostic != nil ||
		!fixedGoldHasReason(record.InvalidReasons, "answer_failed") {
		t.Fatalf("failed oracle record=%+v calls=%d", record, answerCalls)
	}
	if err := writeFixedGoldOracleRecords(files.artifact, []evalFixedGoldOracleDiagnostic{record}); err != nil {
		t.Fatalf("persist failed oracle record: %v", err)
	}
	if err := files.Close(); err != nil {
		t.Fatalf("close oracle files: %v", err)
	}

	var audits []evalFixedGoldOracleCallAudit
	if err := readEvalJSONL(filepath.Join(runDir, evalFixedGoldOracleJournalFile), &audits); err != nil {
		t.Fatalf("read failure audit: %v", err)
	}
	if len(audits) != 3 || audits[1].State != "intent" || audits[2].State != "terminal" ||
		audits[2].Success || audits[1].Role != "answer" || audits[2].Role != "answer" {
		t.Fatalf("failure call audit = %+v", audits)
	}
	var pending evalFixedGoldOracleSummary
	rawPending, err := os.ReadFile(filepath.Join(runDir, evalFixedGoldOracleSummaryFile))
	if err != nil || json.Unmarshal(rawPending, &pending) != nil {
		t.Fatalf("read pending summary: %v", err)
	}
	if pending.Valid || pending.OracleDiagnostic != nil ||
		!fixedGoldHasReason(pending.InvalidReasons, "run_incomplete") {
		t.Fatalf("pending summary leaked partial score: %+v", pending)
	}
	if _, err := createFixedGoldOracleRunFiles(runDir, protocol, protocol.Benchmark.QuestionCount); err == nil {
		t.Fatal("oracle unexpectedly overwrote existing artifact/summary/journal")
	}

	for _, existing := range []string{
		evalFixedGoldOracleArtifactFile,
		evalFixedGoldOracleJournalFile,
		evalFixedGoldOracleSummaryFile,
	} {
		t.Run("existing-"+existing, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, existing), []byte("reserved"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := createFixedGoldOracleRunFiles(dir, protocol, 1540); err == nil {
				t.Fatalf("oracle accepted existing %s", existing)
			}
		})
	}
}

func TestRunFixedGoldOracleDatasetWritesCompleteAuditableRunAndRefusesReuse(t *testing.T) {
	protocol, opt, convs := fixedGoldDatasetFixture(t)
	// Use the smaller registered full benchmark here; the executor still has
	// to complete every one of its 500 questions and all three repetitions.
	convs[0].QA = convs[0].QA[:500]
	protocol.Benchmark.Name = "longmemeval_s"
	protocol.Benchmark.Split = "cleaned_full_500"
	protocol.Benchmark.QuestionCount = 500
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(formalQuestionIDs("longmemeval", convs))
	opt.datasetFormat = "longmemeval"
	fixedGoldSealProtocol(&protocol)
	runDir := t.TempDir()
	opt.runDir = runDir
	counter := &fixedGoldTestCounter{count: 50, fingerprint: protocol.Budget.CounterFingerprint}
	opt.formalCounter = counter
	if err := writeJSON(filepath.Join(runDir, evalProtocolArtifactFile), protocol); err != nil {
		t.Fatal(err)
	}
	answerCalls, judgeCalls := 0, 0
	answer := func(context.Context, string, string) (string, provider.Usage, error) {
		answerCalls++
		return "answer", provider.Usage{InputTokens: 50, OutputTokens: 1}, nil
	}
	judge := func(context.Context, string, string) (string, provider.Usage, error) {
		judgeCalls++
		return `{"correct":true}`, provider.Usage{InputTokens: 1, OutputTokens: 1}, nil
	}
	summary, err := runFixedGoldOracleDataset(
		context.Background(), protocol, opt, convs, answer, judge,
	)
	if err != nil {
		t.Fatalf("run fixed-gold dataset: %v", err)
	}
	wantCalls := protocol.Benchmark.QuestionCount * protocol.Aggregation.AnswerRepetitions
	if !summary.Valid || summary.OracleDiagnostic == nil ||
		summary.OracleDiagnostic.Correct != protocol.Benchmark.QuestionCount ||
		summary.AnswerCalls != wantCalls || summary.JudgeCalls != wantCalls ||
		answerCalls != wantCalls || judgeCalls != wantCalls ||
		counter.calls != protocol.Benchmark.QuestionCount {
		t.Fatalf(
			"fixed-gold dataset summary=%+v answer=%d judge=%d counter=%d",
			summary, answerCalls, judgeCalls, counter.calls,
		)
	}
	for _, name := range []string{
		evalProtocolArtifactFile,
		evalFixedGoldOracleArtifactFile,
		evalFixedGoldOracleJournalFile,
		evalFixedGoldOracleSummaryFile,
	} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("missing completed oracle artifact %s: %v", name, err)
		}
	}
	if _, err := validateFixedGoldOracleRunDirectory(
		context.Background(), runDir, protocol, opt, convs,
	); err != nil {
		t.Fatalf("independent read-back rejected completed oracle: %v", err)
	}
	if _, err := runFixedGoldOracleDataset(
		context.Background(), protocol, opt, convs, answer, judge,
	); err == nil {
		t.Fatal("fixed-gold dataset runner reused an existing run directory")
	}
	if answerCalls != wantCalls || judgeCalls != wantCalls {
		t.Fatal("overwrite refusal happened after additional provider calls")
	}

	failureOpt := opt
	failureOpt.runDir = t.TempDir()
	failureOpt.formalCounter = &fixedGoldTestCounter{
		count: 50, fingerprint: protocol.Budget.CounterFingerprint,
	}
	failedAnswerCalls := 0
	failed, err := runFixedGoldOracleDataset(
		context.Background(), protocol, failureOpt, convs,
		func(context.Context, string, string) (string, provider.Usage, error) {
			failedAnswerCalls++
			return "", provider.Usage{}, errors.New("provider unavailable")
		},
		judge,
	)
	if err == nil || failed.Valid || failed.OracleDiagnostic != nil ||
		failed.QuestionsComplete != 1 ||
		failed.AnswerCalls != 1 || failed.JudgeCalls != 0 || failedAnswerCalls != 1 {
		t.Fatalf("fail-fast fixed-gold summary=%+v calls=%d err=%v", failed, failedAnswerCalls, err)
	}
}

func TestFixedGoldOracleConcurrentFailureDoesNotDispatchReplacement(t *testing.T) {
	protocol, opt, convs := fixedGoldDatasetFixture(t)
	opt.concurrency = 3
	opt.runDir = t.TempDir()
	opt.formalCounter = fixedGoldConcurrentCounter{
		count: 50, fingerprint: protocol.Budget.CounterFingerprint,
	}
	var answerCalls atomic.Int64
	var judgeCalls atomic.Int64
	summary, err := runFixedGoldOracleDataset(
		context.Background(),
		protocol,
		opt,
		convs,
		func(context.Context, string, string) (string, provider.Usage, error) {
			answerCalls.Add(1)
			return "", provider.Usage{}, errors.New("provider unavailable")
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			judgeCalls.Add(1)
			return `{"correct":true}`, provider.Usage{}, nil
		},
	)
	if err == nil || summary.Valid || summary.OracleDiagnostic != nil {
		t.Fatalf("concurrent provider failure did not invalidate oracle: summary=%+v err=%v", summary, err)
	}
	if summary.QuestionsComplete != opt.concurrency {
		t.Fatalf(
			"scheduled questions=%d, want only initial in-flight=%d",
			summary.QuestionsComplete,
			opt.concurrency,
		)
	}
	if got := int(answerCalls.Load()); got < 1 || got > opt.concurrency ||
		summary.AnswerCalls != got || summary.JudgeCalls != 0 || judgeCalls.Load() != 0 {
		t.Fatalf("provider totals answer=%d judge=%d summary=%+v", got, judgeCalls.Load(), summary)
	}
}

func TestFixedGoldOracleReadbackReconstructsInputsAndRejectsTampering(t *testing.T) {
	protocol, opt, convs, records, audits := fixedGoldReadbackFixture(t)
	runDir := t.TempDir()
	artifactPath := filepath.Join(runDir, evalFixedGoldOracleArtifactFile)
	journalPath := filepath.Join(runDir, evalFixedGoldOracleJournalFile)
	if err := writeEvalJSONL(artifactPath, records); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalJSONL(journalPath, audits); err != nil {
		t.Fatal(err)
	}
	_, summary, err := validateFixedGoldOracleReadback(
		context.Background(), protocol, opt, convs, artifactPath, journalPath,
	)
	if err != nil || !summary.Valid || summary.OracleDiagnostic == nil ||
		summary.OracleDiagnostic.Denominator != 1540 ||
		summary.OracleDiagnostic.Correct != 1540 ||
		summary.OracleDiagnostic.TargetCorrect != 1425 ||
		!isDigest(summary.ArtifactDigest) || !isDigest(summary.CallJournalDigest) {
		t.Fatalf("valid read-back summary=%+v err=%v", summary, err)
	}
	if err := writeJSON(filepath.Join(runDir, evalFixedGoldOracleSummaryFile), summary); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFixedGoldOracleRunDirectory(
		context.Background(), runDir, protocol, opt, convs,
	); err != nil {
		t.Fatalf("independent run-directory validator rejected matching persisted summary: %v", err)
	}
	tamperedSummary := summary
	tamperedAggregate := *tamperedSummary.OracleDiagnostic
	tamperedAggregate.TargetCorrect++
	tamperedSummary.OracleDiagnostic = &tamperedAggregate
	if err := writeJSON(filepath.Join(runDir, evalFixedGoldOracleSummaryFile), tamperedSummary); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFixedGoldOracleRunDirectory(
		context.Background(), runDir, protocol, opt, convs,
	); err == nil {
		t.Fatal("independent run-directory validator accepted a tampered persisted summary")
	}
	if err := writeJSON(filepath.Join(runDir, evalFixedGoldOracleSummaryFile), summary); err != nil {
		t.Fatal(err)
	}

	records[0].RepetitionResults[0].JudgeCorrect = false
	records[0].OracleDiagnostic.Correct = 0
	records[0].OracleDiagnostic.MajorityCorrect = false
	records[0].OracleDiagnostic.CorrectRepetitions = 0
	if err := writeEvalJSONL(artifactPath, records); err != nil {
		t.Fatal(err)
	}
	_, tampered, err := validateFixedGoldOracleReadback(
		context.Background(), protocol, opt, convs, artifactPath, journalPath,
	)
	if err != nil {
		t.Fatalf("tampered artifact should produce INVALID summary, got read error: %v", err)
	}
	if tampered.Valid || tampered.OracleDiagnostic != nil ||
		!fixedGoldHasReason(tampered.InvalidReasons, "question_artifact_invalid") {
		t.Fatalf("raw-verdict tamper leaked score: %+v", tampered)
	}

	records, audits = fixedGoldReadbackRecords(t, protocol, opt, convs)
	records[0].AnswerInputDigest = evalTextDigest("plausible-but-wrong-input")
	for index := range audits {
		if audits[index].QuestionID == records[0].QuestionID && audits[index].Role == "answer" {
			audits[index].InputDigest = records[0].AnswerInputDigest
		}
	}
	if err := writeEvalJSONL(artifactPath, records); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalJSONL(journalPath, audits); err != nil {
		t.Fatal(err)
	}
	_, reconstructed, err := validateFixedGoldOracleReadback(
		context.Background(), protocol, opt, convs, artifactPath, journalPath,
	)
	if err != nil {
		t.Fatalf("input tamper should produce INVALID summary, got read error: %v", err)
	}
	if reconstructed.Valid || reconstructed.OracleDiagnostic != nil ||
		!fixedGoldHasReason(reconstructed.InvalidReasons, "question_reconstruction_invalid") {
		t.Fatalf("reconstruction tamper leaked score: %+v", reconstructed)
	}

	records, audits = fixedGoldReadbackRecords(t, protocol, opt, convs)
	if err := writeEvalJSONL(artifactPath, records); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalJSONL(journalPath, audits[:len(audits)-1]); err != nil {
		t.Fatal(err)
	}
	_, orphaned, err := validateFixedGoldOracleReadback(
		context.Background(), protocol, opt, convs, artifactPath, journalPath,
	)
	if err != nil {
		t.Fatalf("orphan journal should produce INVALID summary, got read error: %v", err)
	}
	if orphaned.Valid || orphaned.OracleDiagnostic != nil ||
		!fixedGoldHasReason(orphaned.InvalidReasons, "call_journal_invalid") {
		t.Fatalf("orphan call intent leaked score: %+v", orphaned)
	}
}

func TestRunFixedGoldOracleValidateCLIUsesDatasetAndMakesNoModelCall(t *testing.T) {
	questions := make([]locomoQA, 1540)
	for index := range questions {
		questions[index] = locomoQA{
			Question: "What is the gold answer?", Answer: json.RawMessage(`"answer"`),
			Evidence: []string{"D1:1"}, Category: 1,
		}
	}
	rawDataset, err := json.Marshal([]map[string]any{{
		"qa": questions,
		"conversation": map[string]any{
			"session_1_date_time": "1 May, 2023",
			"session_1": []map[string]string{{
				"speaker": "Alice", "text": "The gold answer is answer.", "dia_id": "D1:1",
			}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(t.TempDir(), "locomo.json")
	if err := os.WriteFile(dataPath, rawDataset, 0o600); err != nil {
		t.Fatal(err)
	}
	convs, err := loadBenchmarkDataset(dataPath, "locomo", false)
	if err != nil {
		t.Fatal(err)
	}
	opt := options{
		dataPath: dataPath, datasetFormat: "locomo", retrieval: "hybrid",
		chunks: true, chunkQuota: 7, topK: 30, repeats: 3, maxTokens: 8000,
	}
	protocol := fixedGoldTestProtocol()
	protocol.Benchmark.DatasetDigest = evalTextDigest(string(rawDataset))
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(formalQuestionIDs("locomo", convs))
	protocol.Retrieval.Recipe = "hybrid"
	protocol.Store.SchemaVersion = 7
	protocol.Store.IngestionRecipe = "ledger_lossless_chunks_v2"
	protocol.Store.IngestionConfigDigest = evalJSONDigest(evalFreezeIngestion{Chunks: true})
	protocol.Store.ProjectionBuilderVersions = map[string]string{"atomic_fact": "entry_store_explicit_v1"}
	protocol.Retrieval.CandidateRulesDigest = evalJSONDigest(evalFreezeCandidateRules{
		TopK: 30, ChunkQuota: 7, Chunks: true, Retrieval: "hybrid",
	})
	fixedGoldSealProtocol(&protocol)
	records, audits := fixedGoldReadbackRecords(t, protocol, opt, convs)

	runDir := t.TempDir()
	opt.evalValidate = runDir
	if err := writeJSON(filepath.Join(runDir, evalProtocolArtifactFile), protocol); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalJSONL(filepath.Join(runDir, evalFixedGoldOracleArtifactFile), records); err != nil {
		t.Fatal(err)
	}
	if err := writeEvalJSONL(filepath.Join(runDir, evalFixedGoldOracleJournalFile), audits); err != nil {
		t.Fatal(err)
	}
	_, summary, err := validateFixedGoldOracleReadback(
		context.Background(), protocol, opt, convs,
		filepath.Join(runDir, evalFixedGoldOracleArtifactFile),
		filepath.Join(runDir, evalFixedGoldOracleJournalFile),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(runDir, evalFixedGoldOracleSummaryFile), summary); err != nil {
		t.Fatal(err)
	}
	if err := runFixedGoldOracleValidateCLI(context.Background(), opt); err != nil {
		t.Fatalf("no-model fixed-gold CLI read-back rejected valid artifacts: %v", err)
	}
	drifted := opt
	drifted.chunkQuota++
	if err := runFixedGoldOracleValidateCLI(context.Background(), drifted); err == nil {
		t.Fatal("fixed-gold CLI read-back accepted option drift")
	}
}

func TestSummarizeFixedGoldOraclePublishesOnlyCompleteValidDiagnostic(t *testing.T) {
	protocol := fixedGoldTestProtocol()
	expected := make([]string, protocol.Benchmark.QuestionCount)
	records := make([]evalFixedGoldOracleDiagnostic, protocol.Benchmark.QuestionCount)
	for index := range expected {
		expected[index] = fmt.Sprintf("q-%04d", index)
		records[index] = fixedGoldValidDiagnostic(protocol, expected[index], index < 1425)
	}
	summary := summarizeFixedGoldOracle(protocol, expected, records)
	if !summary.Valid || summary.OracleDiagnostic == nil ||
		summary.OracleDiagnostic.Correct != 1425 ||
		summary.OracleDiagnostic.Denominator != 1540 ||
		summary.OracleDiagnostic.TargetCorrect != 1425 ||
		summary.AnswerCalls != 1540*3 ||
		summary.JudgeCalls != 1540*3 ||
		!summary.OracleDiagnostic.TargetMet {
		t.Fatalf("complete oracle summary = %+v", summary)
	}

	incomplete := summarizeFixedGoldOracle(protocol, expected, records[:len(records)-1])
	if incomplete.Valid || incomplete.OracleDiagnostic != nil ||
		incomplete.AnswerCalls != (1540-1)*3 ||
		incomplete.JudgeCalls != (1540-1)*3 ||
		!fixedGoldHasReason(incomplete.InvalidReasons, "denominator_incomplete") {
		t.Fatalf("incomplete oracle exposed a diagnostic score: %+v", incomplete)
	}

	tampered := append([]evalFixedGoldOracleDiagnostic(nil), records...)
	tampered[0].RetrievalCalls = 1
	invalid := summarizeFixedGoldOracle(protocol, expected, tampered)
	if invalid.Valid || invalid.OracleDiagnostic != nil ||
		!fixedGoldHasReason(invalid.InvalidReasons, "question_artifact_invalid") {
		t.Fatalf("tampered oracle exposed a diagnostic score: %+v", invalid)
	}

	longMemEval := fixedGoldTestProtocol()
	longMemEval.Benchmark.Name = "longmemeval_s"
	longMemEval.Benchmark.Split = "cleaned_full_500"
	longMemEval.Benchmark.QuestionCount = 500
	fixedGoldSealProtocol(&longMemEval)
	lmeIDs := make([]string, 500)
	lmeRecords := make([]evalFixedGoldOracleDiagnostic, 500)
	for index := range lmeIDs {
		lmeIDs[index] = fmt.Sprintf("lme-%03d", index)
		lmeRecords[index] = fixedGoldValidDiagnostic(longMemEval, lmeIDs[index], index < 473)
	}
	lmeSummary := summarizeFixedGoldOracle(longMemEval, lmeIDs, lmeRecords)
	if !lmeSummary.Valid || lmeSummary.OracleDiagnostic == nil ||
		lmeSummary.OracleDiagnostic.Correct != 473 ||
		lmeSummary.OracleDiagnostic.Denominator != 500 ||
		lmeSummary.OracleDiagnostic.TargetCorrect != 473 ||
		!lmeSummary.OracleDiagnostic.TargetMet {
		t.Fatalf("LongMemEval oracle target summary = %+v", lmeSummary)
	}
}

func fixedGoldReadbackFixture(t *testing.T) (
	evalProtocol,
	options,
	[]conversation,
	[]evalFixedGoldOracleDiagnostic,
	[]evalFixedGoldOracleCallAudit,
) {
	t.Helper()
	protocol, opt, convs := fixedGoldDatasetFixture(t)
	records, audits := fixedGoldReadbackRecords(t, protocol, opt, convs)
	return protocol, opt, convs, records, audits
}

func fixedGoldDatasetFixture(t *testing.T) (evalProtocol, options, []conversation) {
	t.Helper()
	questions := make([]locomoQA, 1540)
	for index := range questions {
		questions[index] = locomoQA{
			QuestionID: fmt.Sprintf("q-%04d", index),
			Question:   "What is the gold answer?",
			Answer:     json.RawMessage(`"answer"`),
			Evidence:   []string{"D1:1"},
			Category:   1,
		}
	}
	convs := []conversation{{
		ID: 0,
		Sessions: []session{{
			Index: 1,
			Date:  time.Date(2023, time.May, 1, 0, 0, 0, 0, time.UTC),
			Turns: []turn{{Speaker: "Alice", Text: "The gold answer is answer.", DiaID: "D1:1"}},
		}},
		QA: questions,
	}}
	protocol := fixedGoldTestProtocol()
	protocol.Benchmark.QuestionIDsDigest = evalJSONDigest(formalQuestionIDs("locomo", convs))
	fixedGoldSealProtocol(&protocol)
	opt := options{datasetFormat: "locomo", concurrency: 1}
	return protocol, opt, convs
}

func fixedGoldReadbackRecords(
	t *testing.T,
	protocol evalProtocol,
	opt options,
	convs []conversation,
) ([]evalFixedGoldOracleDiagnostic, []evalFixedGoldOracleCallAudit) {
	t.Helper()
	expected, err := buildFixedGoldExpectedQuestions(context.Background(), protocol, opt, convs)
	if err != nil {
		t.Fatalf("build fixed-gold expected questions: %v", err)
	}
	oracleHash := fixedGoldOracleProtocolHash(protocol)
	audits := []evalFixedGoldOracleCallAudit{{
		Schema: evalProtocolSchema, Kind: evalFixedGoldOracleJournalKind,
		Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: protocol.ProtocolHash, OracleProtocolHash: oracleHash,
		State: "header",
	}}
	records := make([]evalFixedGoldOracleDiagnostic, 0, len(expected))
	for _, item := range expected {
		if item.InvalidReason != "" {
			t.Fatalf("fixture gold invalid for %q: %s", item.QA.QuestionID, item.InvalidReason)
		}
		answer := "answer"
		answerDigest := evalTextDigest(answer)
		judgeInput := evidencecompiler.AnswerInput{
			Model:  protocol.Models.Judge.ID,
			System: judgeSystemPromptFor(opt.judgeAlignmentMode()),
			User:   buildJudgePrompt(item.QA.Question, goldFor(item.QA), answer),
		}
		judgeInputDigest := evalJSONDigest(judgeInput)
		verdict := `{"correct":true}`
		verdictDigest := evalTextDigest(verdict)
		runs := make([]evalFixedGoldOracleRun, 0, protocol.Aggregation.AnswerRepetitions)
		for runIndex := 1; runIndex <= protocol.Aggregation.AnswerRepetitions; runIndex++ {
			runs = append(runs, evalFixedGoldOracleRun{
				RunIndex: runIndex, Answer: answer, AnswerDigest: answerDigest,
				JudgeInputDigest: judgeInputDigest, JudgeVerdict: verdict,
				JudgeCorrect: true, JudgeVerdictDigest: verdictDigest,
				InputTokens: 50, OutputTokens: 1,
			})
			audits = append(audits,
				fixedGoldTestAudit(protocol, item.QA.QuestionID, runIndex, "answer", "intent", item.AnswerInputDigest, "", false),
				fixedGoldTestAudit(protocol, item.QA.QuestionID, runIndex, "answer", "terminal", item.AnswerInputDigest, answerDigest, true),
				fixedGoldTestAudit(protocol, item.QA.QuestionID, runIndex, "judge", "intent", judgeInputDigest, "", false),
				fixedGoldTestAudit(protocol, item.QA.QuestionID, runIndex, "judge", "terminal", judgeInputDigest, verdictDigest, true),
			)
		}
		records = append(records, evalFixedGoldOracleDiagnostic{
			Schema: evalProtocolSchema, Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm,
			DiagnosticOnly: true, ControlProtocolHash: protocol.ProtocolHash,
			OracleProtocolHash: oracleHash, QuestionID: item.QA.QuestionID,
			RetrievalCalls: 0, DatasetSourceIDs: append([]string(nil), item.DatasetSourceIDs...),
			EmptyEvidenceAbstention: item.EmptyAbstention,
			AnswerInputDigest:       item.AnswerInputDigest, AnswerPromptDigest: item.AnswerPromptDigest,
			AnswerInputTokens: 50, CounterFingerprint: protocol.Budget.CounterFingerprint,
			AnswerCalls: protocol.Aggregation.AnswerRepetitions,
			JudgeCalls:  protocol.Aggregation.AnswerRepetitions,
			Valid:       true, RepetitionResults: runs,
			OracleDiagnostic: &evalFixedGoldOracleOutcome{
				Correct: 1, Denominator: 1, MajorityCorrect: true,
				CorrectRepetitions: protocol.Aggregation.AnswerRepetitions,
				Repetitions:        protocol.Aggregation.AnswerRepetitions,
			},
		})
	}
	return records, audits
}

func fixedGoldTestAudit(
	protocol evalProtocol,
	questionID string,
	runIndex int,
	role string,
	state string,
	inputDigest string,
	outputDigest string,
	success bool,
) evalFixedGoldOracleCallAudit {
	return evalFixedGoldOracleCallAudit{
		Schema: evalProtocolSchema, Kind: evalFixedGoldOracleJournalKind,
		Stage: evalStageFixedGoldOracle, Arm: evalFixedGoldOracleArm, DiagnosticOnly: true,
		ControlProtocolHash: protocol.ProtocolHash,
		OracleProtocolHash:  fixedGoldOracleProtocolHash(protocol),
		QuestionID:          questionID, RunIndex: runIndex, Role: role, State: state,
		InputDigest: inputDigest, OutputDigest: outputDigest, Success: success,
	}
}

func fixedGoldValidDiagnostic(protocol evalProtocol, questionID string, correct bool) evalFixedGoldOracleDiagnostic {
	correctRepetitions := 0
	if correct {
		correctRepetitions = protocol.Aggregation.AnswerRepetitions
	}
	runs := make([]evalFixedGoldOracleRun, protocol.Aggregation.AnswerRepetitions)
	for index := range runs {
		verdict := `{"correct":false}`
		if correct {
			verdict = `{"correct":true}`
		}
		runs[index] = evalFixedGoldOracleRun{
			RunIndex:           index + 1,
			Answer:             "answer",
			AnswerDigest:       evalTextDigest("answer"),
			JudgeInputDigest:   evalTextDigest("judge-input"),
			JudgeVerdict:       verdict,
			JudgeCorrect:       correct,
			JudgeVerdictDigest: evalTextDigest(verdict),
			InputTokens:        50,
			OutputTokens:       1,
		}
	}
	return evalFixedGoldOracleDiagnostic{
		Schema:              evalProtocolSchema,
		Stage:               evalStageFixedGoldOracle,
		Arm:                 evalFixedGoldOracleArm,
		DiagnosticOnly:      true,
		ControlProtocolHash: protocol.ProtocolHash,
		OracleProtocolHash:  fixedGoldOracleProtocolHash(protocol),
		QuestionID:          questionID,
		RetrievalCalls:      0,
		DatasetSourceIDs:    []string{"dataset-" + questionID},
		AnswerInputDigest:   evalTextDigest("input"),
		AnswerPromptDigest:  evalTextDigest("system"),
		AnswerInputTokens:   50,
		CounterFingerprint:  protocol.Budget.CounterFingerprint,
		AnswerCalls:         protocol.Aggregation.AnswerRepetitions,
		JudgeCalls:          protocol.Aggregation.AnswerRepetitions,
		Valid:               true,
		RepetitionResults:   runs,
		OracleDiagnostic: &evalFixedGoldOracleOutcome{
			Correct:            boolInt(correct),
			Denominator:        1,
			MajorityCorrect:    correct,
			CorrectRepetitions: correctRepetitions,
			Repetitions:        protocol.Aggregation.AnswerRepetitions,
		},
	}
}

type fixedGoldTestReader struct {
	evidence  map[string]memory.Evidence
	err       error
	calls     int
	requested []string
}

func (reader *fixedGoldTestReader) GetMany(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
	reader.calls++
	reader.requested = append([]string(nil), ids...)
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.evidence, nil
}

type fixedGoldTestCounter struct {
	count       int
	fingerprint string
	calls       int
}

func (counter *fixedGoldTestCounter) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	counter.calls++
	return evidencecompiler.TokenCount{InputTokens: counter.count, Fingerprint: counter.fingerprint}, nil
}

type fixedGoldConcurrentCounter struct {
	count       int
	fingerprint string
}

func (counter fixedGoldConcurrentCounter) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{InputTokens: counter.count, Fingerprint: counter.fingerprint}, nil
}

func fixedGoldTestProtocol() evalProtocol {
	protocol := testEvalProtocol()
	protocol.Retrieval.Recipe = "hybrid"
	fixedGoldSealProtocol(&protocol)
	return protocol
}

func fixedGoldSealProtocol(protocol *evalProtocol) {
	hash, err := evalProtocolFingerprint(*protocol)
	if err != nil {
		panic(err)
	}
	protocol.ProtocolHash = hash
}

func fixedGoldTestEvidence(id, externalID, content string, ordinal int) memory.Evidence {
	sum := sha256.Sum256([]byte(content))
	return memory.Evidence{
		ID:               id,
		ExternalSourceID: externalID,
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "session-1",
		Speaker:          "Alice",
		Ordinal:          ordinal,
		Content:          content,
		OccurredAt:       fixedGoldTimePtr(time.Date(2023, time.May, ordinal, 0, 0, 0, 0, time.UTC)),
		RecordedAt:       time.Date(2023, time.May, ordinal, 1, 0, 0, 0, time.UTC),
		ContentDigest:    hex.EncodeToString(sum[:]),
		State:            memory.EvidenceActive,
		Revision:         1,
	}
}

func fixedGoldTimePtr(value time.Time) *time.Time {
	return &value
}

func fixedGoldHasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestFixedGoldSplitEvidenceDatasetIDs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"single", []string{"D8:6"}, []string{"D8:6"}},
		{"packed semicolon", []string{"D8:6; D9:17"}, []string{"D8:6", "D9:17"}},
		{"packed comma", []string{"A1, B2"}, []string{"A1", "B2"}},
		{"mixed with blanks", []string{"X1 ; Y2,", " Z3 "}, []string{"X1", "Y2", "Z3"}},
		{"blank only", []string{"  "}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fixedGoldSplitEvidenceDatasetIDs(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v want %v", got, c.want)
				}
			}
		})
	}
}

// conv-0-q-37 的真实数据集标注把两个 source 打包进一个元素（"D8:6; D9:17"）。
// 拆分后必须能解析出两个 evidence；拆分前会 unresolvable 并让整个 oracle run 失效。
func TestFixedGoldSplitThenResolvePackedEvidenceAnnotation(t *testing.T) {
	packed := []string{"D8:6; D9:17"}
	split := fixedGoldSplitEvidenceDatasetIDs(packed)
	if len(split) != 2 || split[0] != "D8:6" || split[1] != "D9:17" {
		t.Fatalf("split = %v, want [D8:6 D9:17]", split)
	}
	evidenceByDatasetID := map[string]string{
		"D8:6":  "evidence-a",
		"D9:17": "evidence-b",
	}
	resolved, unresolved, err := resolveDatasetSourceIDs(split, evidenceByDatasetID)
	if err != nil {
		t.Fatalf("resolve err: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want empty (packed annotation must resolve both sources)", unresolved)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %v, want 2", resolved)
	}
	// 不拆分的对照：打包元素直接进 resolve 必然 unresolved。
	rawResolved, rawUnresolved, err := resolveDatasetSourceIDs(packed, evidenceByDatasetID)
	if err != nil {
		t.Fatalf("raw resolve err: %v", err)
	}
	if len(rawUnresolved) != 1 || rawUnresolved[0] != "D8:6; D9:17" {
		t.Fatalf("raw unresolved = %v, want [D8:6; D9:17] (demonstrates the bug split fixes)", rawUnresolved)
	}
	if len(rawResolved) != 0 {
		t.Fatalf("raw resolved = %v, want 0", rawResolved)
	}
}

func TestFixedGoldSplitEvidenceDatasetIDsSpaceAndZeroPad(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"space separated multi", []string{"D9:1 D4:4 D4:6"}, []string{"D9:1", "D4:4", "D4:6"}},
		{"leading zero turn", []string{"D30:05"}, []string{"D30:5"}},
		{"mixed separators", []string{"A1;B2 C3", "D4"}, []string{"A1", "B2", "C3", "D4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fixedGoldSplitEvidenceDatasetIDs(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v want %v", got, c.want)
				}
			}
		})
	}
}

// 数据集证据标注的真实变体：空格打包、前导零、缺失/破损 source。
// 空格打包与前导零必须解析成功；缺失/破损 source 的题必须归入 Unresolved 跳过。
func TestFixedGoldUnanswerableQuestionIDsClassifiesDatasetAnnotations(t *testing.T) {
	conv := conversation{
		ID: 0,
		Sessions: []session{{Turns: []turn{
			{DiaID: "D8:6"},
			{DiaID: "D9:17"},
			{DiaID: "D30:5"},
		}}},
		QA: []locomoQA{
			{QuestionID: "q-empty", Evidence: nil},
			{QuestionID: "q-packed", Evidence: []string{"D8:6; D9:17"}},
			{QuestionID: "q-space", Evidence: []string{"D8:6 D9:17"}},
			{QuestionID: "q-zeropad", Evidence: []string{"D30:05"}},
			{QuestionID: "q-missing", Evidence: []string{"D8:6", "D10:19"}},
			{QuestionID: "q-malformed", Evidence: []string{"D"}},
		},
	}
	skipped := fixedGoldUnanswerableQuestionIDs([]conversation{conv})
	gotEmpty := map[string]bool{}
	for _, id := range skipped.Empty {
		gotEmpty[id] = true
	}
	gotUnresolved := map[string]bool{}
	for _, id := range skipped.Unresolved {
		gotUnresolved[id] = true
	}
	if len(gotEmpty) != 1 || !gotEmpty["q-empty"] {
		t.Fatalf("Empty = %v, want [q-empty]", skipped.Empty)
	}
	if len(gotUnresolved) != 2 || !gotUnresolved["q-missing"] || !gotUnresolved["q-malformed"] {
		t.Fatalf("Unresolved = %v, want [q-missing q-malformed]", skipped.Unresolved)
	}
	for _, id := range []string{"q-packed", "q-space", "q-zeropad"} {
		if gotEmpty[id] || gotUnresolved[id] {
			t.Fatalf("%s must resolve, Empty=%v Unresolved=%v", id, skipped.Empty, skipped.Unresolved)
		}
	}
}
