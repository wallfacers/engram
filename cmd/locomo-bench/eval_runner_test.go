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

func TestMechanismArmsRequireFormalProtocolContext(t *testing.T) {
	for name, mutate := range map[string]func(*options){
		"representation": func(opt *options) { opt.representationArm = ReprRawTurnWindow },
		"compiler":       func(opt *options) { opt.compilerArm = "extractive" },
		"compiler exact_token": func(opt *options) { opt.compilerArm = "exact_token" },
		"event gap": func(opt *options) {
			opt.eventProjection = "E1"
			opt.gapRefetch = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			diagnostic := options{representationArm: ReprChunk900}
			mutate(&diagnostic)
			if err := validateMechanismArms(diagnostic); err == nil {
				t.Fatalf("non-formal %s mechanism was silently accepted", name)
			}

			formalRun := diagnostic
			formalRun.evalProtocolPath = "protocol.json"
			if err := validateMechanismArms(formalRun); err != nil {
				t.Fatalf("formal-run %s mechanism rejected: %v", name, err)
			}

			formalFreeze := diagnostic
			formalFreeze.evalFreezeProtocol = "protocol.json"
			formalFreeze.controlProtocolPath = "control.json"
			if err := validateMechanismArms(formalFreeze); err != nil {
				t.Fatalf("formal-freeze %s mechanism rejected after T114 bound it via --control-protocol: %v", name, err)
			}
		})
	}
}

func TestGateUsageOnceNeverRetriesFormalProviderAttempt(t *testing.T) {
	calls := 0
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		calls++
		if calls == 1 {
			return "", provider.Usage{}, fmt.Errorf("transient failure")
		}
		return "unexpected retry", provider.Usage{}, nil
	}

	if _, _, err := gateUsageOnce(make(chan struct{}, 1), caller)(
		context.Background(), "system", "user",
	); err == nil {
		t.Fatal("exactly-once formal caller unexpectedly hid the first provider failure")
	}
	if calls != 1 {
		t.Fatalf("formal provider attempts = %d, want exactly 1", calls)
	}

	calls = 0
	if got, _, err := gateUsage(make(chan struct{}, 1), caller)(
		context.Background(), "system", "user",
	); err != nil || got != "unexpected retry" {
		t.Fatalf("legacy retrying caller = %q, %v", got, err)
	}
	if calls != 2 {
		t.Fatalf("legacy provider attempts = %d, want 2", calls)
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
	t.Setenv("EMBED_FINGERPRINT", "sha256:embedding")
	protocol := testEvalProtocol()
	protocol.Retrieval.Recipe = "hybrid"
	opt := options{repeats: 3, topK: 30, chunkQuota: 7, chunks: true, maxTokens: 8000}
	protocol.Store.SchemaVersion = 7
	protocol.Store.IngestionRecipe = "ledger_lossless_chunks_v2"
	protocol.Store.IngestionConfigDigest = evalJSONDigest(evalFreezeIngestion{
		Chunks: opt.chunks, ImageCaptions: opt.imageCaptions, OpinionPass: opt.opinionPass,
	})
	protocol.Store.ProjectionBuilderVersions = map[string]string{"atomic_fact": "entry_store_explicit_v1"}
	protocol.Retrieval.CandidateRulesDigest = evalJSONDigest(evalFreezeCandidateRules{
		TopK: opt.topK, ChunkQuota: opt.chunkQuota, Chunks: opt.chunks, Retrieval: "hybrid",
	})
	if err := validateFormalRunnerOptions(protocol, opt, []string{"hybrid"}); err != nil {
		t.Fatalf("valid formal options rejected: %v", err)
	}
	adaptive := opt
	adaptive.multiQuery = true
	if err := validateFormalRunnerOptions(protocol, adaptive, []string{"hybrid"}); err == nil {
		t.Fatal("formal options unexpectedly accepted multi-query")
	}
	if err := validateFormalRunnerOptions(protocol, opt, []string{"fts", "hybrid"}); err == nil {
		t.Fatal("formal options unexpectedly accepted multiple arms")
	}
	for _, recipe := range []string{"hybrid+rerank", "hybrid+pcic", "hybrid+assoc"} {
		t.Run("recipe "+recipe, func(t *testing.T) {
			drifted := protocol
			drifted.Retrieval.Recipe = recipe
			drifted.Retrieval.CandidateRulesDigest = evalJSONDigest(evalFreezeCandidateRules{
				TopK: opt.topK, ChunkQuota: opt.chunkQuota, Chunks: opt.chunks, Retrieval: recipe,
			})
			if err := validateFormalRunnerOptions(drifted, opt, []string{recipe}); err == nil {
				t.Fatalf("formal options unexpectedly accepted suffixed recipe %q", recipe)
			}
		})
	}
	for name, mutate := range map[string]func(*options){
		"chunks":               func(opt *options) { opt.chunks = false },
		"chunk quota":          func(opt *options) { opt.chunkQuota++ },
		"image captions":       func(opt *options) { opt.imageCaptions = true },
		"opinion pass":         func(opt *options) { opt.opinionPass = true },
		"date scaffold":        func(opt *options) { opt.temporalDateScaffold = true },
		"category top-k":       func(opt *options) { opt.catTopKSpec = "1=40" },
		"output cap":           func(opt *options) { opt.maxTokens++ },
		"filter pool":          func(opt *options) { opt.filterPool = 60 },
		"association":          func(opt *options) { opt.assoc = true },
		"cluster sweep":        func(opt *options) { opt.clusterSweep = true },
		"temporal score":       func(opt *options) { opt.temporalScore = true },
		"temporal hard filter": func(opt *options) { opt.temporalHardFilter = true },
		"conflict resolution":  func(opt *options) { opt.conflictResolution = true },
		"iris":                 func(opt *options) { opt.iris = true },
		"rerank":               func(opt *options) { opt.rerank = true },
		"pcic":                 func(opt *options) { opt.pcic = true },
		"oracle selector":      func(opt *options) { opt.oracle = true },
		"abstain hard":         func(opt *options) { opt.abstainHard = true },
		"abstain soft":         func(opt *options) { opt.abstainSoft = true },
		"alias shadow":         func(opt *options) { opt.aliasShadow = aliasShadowBaseline },
		"doc2query":            func(opt *options) { opt.doc2query = doc2queryBaseline },
		"doc2query build":      func(opt *options) { opt.doc2queryBuild = true },
		"pcic annotate":        func(opt *options) { opt.pcicAnnotate = true },
		"recall diagnostic":    func(opt *options) { opt.recallDiagnostic = true },
		"coverage diagnostic":  func(opt *options) { opt.coverageOnly = true },
		"temporal diagnostic":  func(opt *options) { opt.temporalDiagnostic = true },
		"attribution trace":    func(opt *options) { opt.attributionTrace = true },
		"abstention probe":     func(opt *options) { opt.abstainProbe = true },
		"estimate":             func(opt *options) { opt.estimate = true },
	} {
		t.Run(name, func(t *testing.T) {
			drifted := opt
			mutate(&drifted)
			if err := validateFormalRunnerOptions(protocol, drifted, []string{"hybrid"}); err == nil {
				t.Fatalf("formal options unexpectedly accepted %s drift", name)
			}
		})
	}
	t.Setenv("EMBED_FINGERPRINT", "sha256:different-embedding")
	if err := validateFormalRunnerOptions(protocol, opt, []string{"hybrid"}); err == nil {
		t.Fatal("formal options unexpectedly accepted embedding fingerprint drift")
	}
	oracleOpt := opt
	oracleOpt.fixedGoldOracle = true
	if err := validateFormalRunnerOptions(protocol, oracleOpt, []string{"hybrid"}); err != nil {
		t.Fatalf("fixed-gold oracle unexpectedly required an unused runtime embedding: %v", err)
	}
	t.Setenv("EMBED_FINGERPRINT", "sha256:embedding")

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

func TestFormalRunnerOptionsRequireLegacyControlAndRejectTreatments(t *testing.T) {
	t.Setenv("EMBED_FINGERPRINT", "sha256:embedding")
	baseProtocol := testEvalProtocol()
	baseProtocol.Retrieval.Recipe = "hybrid"
	baseProtocol.Store.SchemaVersion = 7
	baseProtocol.Store.IngestionRecipe = "ledger_lossless_chunks_v2"
	baseProtocol.Store.IngestionConfigDigest = evalJSONDigest(evalFreezeIngestion{Chunks: true})
	baseProtocol.Store.ProjectionBuilderVersions = map[string]string{"atomic_fact": "entry_store_explicit_v1"}
	baseProtocol.Retrieval.CandidateRulesDigest = evalJSONDigest(evalFreezeCandidateRules{
		TopK: 30, ChunkQuota: 7, Chunks: true, Retrieval: "hybrid",
	})
	opt := options{repeats: 3, topK: 30, chunkQuota: 7, chunks: true, maxTokens: 8000}
	if err := validateFormalRunnerOptions(baseProtocol, opt, []string{"hybrid"}); err != nil {
		t.Fatalf("formal runner rejected frozen legacy control: %v", err)
	}

	// Phase 8 supports only the frozen B1 legacy control. Treatment manifests
	// and flags remain fail-closed until T114 implements candidate replay and
	// bidirectional option/manifest binding.
	for name, mutate := range map[string]func(*options){
		"compiler arm":   func(o *options) { o.compilerArm = "extractive" },
		"representation": func(o *options) { o.representationArm = ReprRawTurnWindow },
		"event + gap": func(o *options) {
			o.eventProjection = "E1"
			o.gapRefetch = true
		},
	} {
		t.Run("unbound "+name, func(t *testing.T) {
			drifted := opt
			mutate(&drifted)
			if err := validateFormalRunnerOptions(baseProtocol, drifted, []string{"hybrid"}); err == nil {
				t.Fatalf("formal runner accepted %s against a manifest that does not bind it", name)
			}
		})
	}

	t.Run("non-legacy b1 arm without CLI flag", func(t *testing.T) {
		drifted := baseProtocol
		drifted.Experiment.Arm = "deterministic_extractive_compiler"
		if err := validateFormalRunnerOptions(drifted, opt, []string{"hybrid"}); err == nil {
			t.Fatal("formal runner accepted a non-legacy B1 arm without a treatment flag")
		}
	})
	t.Run("treatment manifest binds matching mechanism", func(t *testing.T) {
		treatment := baseProtocol
		treatment.Experiment = evalExperimentProtocol{
			Stage: "representation_navigation", Arm: string(ReprRawTurnWindow), PrimaryCohort: "all",
			ControlProtocolHash: "sha256:control",
			MechanismFlags:      map[string]bool{"representation": true},
		}
		treatmentOpt := opt
		treatmentOpt.representationArm = ReprRawTurnWindow
		if err := validateFormalRunnerOptions(treatment, treatmentOpt, []string{"hybrid"}); err != nil {
			t.Fatalf("formal runner refused a matching treatment manifest: %v", err)
		}
	})
	t.Run("combined mechanisms remain unavailable", func(t *testing.T) {
		treatment := baseProtocol
		treatment.Experiment = evalExperimentProtocol{
			Stage: "representation_rendering", Arm: string(ReprRawTurnWindow), PrimaryCohort: "all",
			ControlProtocolHash: "sha256:control",
			MechanismFlags:      map[string]bool{"idk_retry": false, "iris": false, "rerank": false},
		}
		combined := opt
		combined.representationArm = ReprRawTurnWindow
		combined.eventProjection = "E1"
		combined.gapRefetch = true
		if err := validateFormalRunnerOptions(treatment, combined, []string{"hybrid"}); err == nil {
			t.Fatal("formal runner accepted multiple treatment mechanisms before T114")
		}
	})
	t.Run("legacy manifest mechanism flag", func(t *testing.T) {
		drifted := baseProtocol
		drifted.Experiment.MechanismFlags = map[string]bool{
			"idk_retry": false, "iris": false, "rerank": false, "gap_refetch": true,
		}
		if err := validateFormalRunnerOptions(drifted, opt, []string{"hybrid"}); err == nil {
			t.Fatal("formal runner accepted a treatment mechanism flag in the legacy manifest")
		}
	})
}

func TestFreezeFormalProtocolRejectsSuffixedRecipeAndAlternateModesBeforeIO(t *testing.T) {
	base := options{
		evalBudgetProfile:   "low",
		answerInputTokenCap: 1100,
		counterFingerprint:  "sha256:counter",
		retrieval:           "hybrid",
	}
	for name, mutate := range map[string]func(*options){
		"suffixed recipe":  func(opt *options) { opt.retrieval = "hybrid+rerank" },
		"build mode":       func(opt *options) { opt.doc2queryBuild = true },
		"diagnostic mode":  func(opt *options) { opt.coverageOnly = true },
		"representation":   func(opt *options) { opt.representationArm = ReprRawTurnWindow },
		"event projection": func(opt *options) { opt.eventProjection = "E1" },
		"gap refetch":      func(opt *options) { opt.gapRefetch = true },
		"event + gap": func(opt *options) {
			opt.eventProjection = "E1"
			opt.gapRefetch = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			opt := base
			mutate(&opt)
			if err := freezeFormalProtocol(opt, nil, ""); err == nil {
				t.Fatal("formal freeze accepted an alternate recipe or execution mode")
			} else if strings.Contains(err.Error(), "read benchmark") {
				t.Fatalf("formal freeze reached dataset I/O before rejecting the mode: %v", err)
			}
		})
	}
}

// TestFreezeFormalProtocolAcceptsCompilerArm: the compiler arm is a b1-stage
// additive mechanism (026), so a formal freeze with --compiler-arm must be
// accepted (mirrors episode_cluster), producing a b1 manifest carrying the
// compiler flag — not rejected as an alternate execution mode.
func TestFreezeFormalProtocolAcceptsCompilerArm(t *testing.T) {
	base := options{
		evalBudgetProfile:   "low",
		answerInputTokenCap: 1100,
		counterFingerprint:  "sha256:counter",
		retrieval:           "hybrid",
		compilerArm:         "extractive",
	}
	if err := freezeFormalProtocol(base, nil, ""); err == nil {
		return // accepted, as intended
	} else if strings.Contains(err.Error(), "read benchmark") {
		// reached dataset I/O — the recipe/mode gate passed; compiler arm is accepted
		return
	} else {
		t.Fatalf("formal freeze rejected compiler arm before recipe gate: %v", err)
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
	candidate := buildFormalCandidateArtifact(protocol, qa, hits, map[string][]string{hits[0].Name: {"D1:1"}}, sources, map[string]string{"D1:1": evidence[0].ID}, 1)
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
		options{
			answerModel:    protocol.Models.Answerer.ID,
			formalCounter:  formalCounter{count: 12, fingerprint: protocol.Budget.CounterFingerprint},
			formalEvidence: entries.Ledger(),
		},
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
		options{
			answerModel:    protocol.Models.Answerer.ID,
			formalCounter:  evidenceFailCounter{fingerprint: protocol.Budget.CounterFingerprint},
			formalEvidence: entries.Ledger(),
		},
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
	system := withCurrentDateRule(answerPromptForRegime(qa.Category, false, false, false, false), qa.QuestionDate)
	candidate := testCandidateArtifact()
	trace := buildFormalTrace(protocol, qa.QuestionID, candidate)
	bundle := testFormalBundle(
		protocol, candidate, trace,
		"QUESTION:\nWhen did Alice move?\n\nMEMORIES:\nAlice moved in 2023.",
		12, evalTextDigest(system),
	)
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

func TestDensityMechanismFlagsFailClosedOutsideFormalContext(t *testing.T) {
	// contracts/mechanism-bindings.md rule 1: a density mechanism flag must be
	// rejected outside a formal context (no --eval-protocol / --eval-freeze-protocol).
	for name, mutate := range map[string]func(*options){
		"write_dedup":     func(opt *options) { opt.writeDedup = true },
		"neighbor_extend": func(opt *options) { opt.neighborExtend = true },
		"both": func(opt *options) {
			opt.writeDedup = true
			opt.neighborExtend = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			diagnostic := options{representationArm: ReprChunk900}
			mutate(&diagnostic)
			if err := validateMechanismArms(diagnostic); err == nil {
				t.Fatalf("non-formal %s mechanism was silently accepted", name)
			}

			formalRun := diagnostic
			formalRun.evalProtocolPath = "protocol.json"
			if err := validateMechanismArms(formalRun); err != nil {
				t.Fatalf("formal-run %s mechanism rejected: %v", name, err)
			}
		})
	}
}

func TestDensityMechanismFlagsMergeIntoB1Control(t *testing.T) {
	// mechanism-bindings rule 4: density keys participate in the frozen
	// manifest, so each arm gets a distinct protocol hash; and rule
	// (backward compat): no density flag → pure legacy 3-key control.
	control, err := buildFormalExperiment(options{}, "")
	if err != nil {
		t.Fatalf("build control: %v", err)
	}
	if control.Stage != "b1" || control.Arm != "legacy_count_packer" {
		t.Fatalf("control manifest = %s/%s", control.Stage, control.Arm)
	}
	if !isFormalLegacyControlMechanismFlags(control.MechanismFlags) {
		t.Fatalf("no-density control must stay legacy 3-key, got %v", control.MechanismFlags)
	}

	dedup, err := buildFormalExperiment(options{writeDedup: true}, "")
	if err != nil {
		t.Fatalf("build dedup arm: %v", err)
	}
	if !dedup.MechanismFlags["write_dedup"] || dedup.MechanismFlags["neighbor_extend"] {
		t.Fatalf("dedup arm flags = %v, want write_dedup=true neighbor_extend=false", dedup.MechanismFlags)
	}
	if !isFormalControlMechanismFlags(dedup.MechanismFlags) {
		t.Fatalf("dedup arm must pass isFormalControlMechanismFlags, got %v", dedup.MechanismFlags)
	}
	if isFormalLegacyControlMechanismFlags(dedup.MechanismFlags) {
		t.Fatalf("dedup arm must NOT pass legacy 3-key check")
	}

	// Distinct protocol hashes per arm (attribution).
	controlProto := evalProtocol{Schema: evalProtocolSchema, ProtocolID: "b1", Experiment: control}
	dedupProto := evalProtocol{Schema: evalProtocolSchema, ProtocolID: "b1", Experiment: dedup}
	hControl, _ := evalProtocolFingerprint(controlProto)
	hDedup, _ := evalProtocolFingerprint(dedupProto)
	if hControl == hDedup {
		t.Fatal("density arm must produce a different protocol hash than the control")
	}
}

func TestValidateFormalMechanismBindingDensityArms(t *testing.T) {
	// A frozen density arm must bind the exact requested density flags; a
	// mismatched request (or a density arm requested against a plain control)
	// must fail closed before any model call.
	dedupProto := evalProtocol{Experiment: evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{
			"idk_retry": false, "iris": false, "rerank": false,
			"write_dedup": true, "neighbor_extend": false,
		},
	}}
	matching := options{writeDedup: true}
	if err := validateFormalMechanismBinding(dedupProto, matching); err != nil {
		t.Fatalf("matching density arm rejected: %v", err)
	}
	if err := validateFormalMechanismBinding(dedupProto, options{}); err == nil {
		t.Fatal("density arm accepted with no requested density flag")
	}
	if err := validateFormalMechanismBinding(dedupProto, options{neighborExtend: true}); err == nil {
		t.Fatal("density arm accepted with a different requested density flag")
	}

	// A plain legacy control (3 keys) still validates with no density request.
	legacyProto := evalProtocol{Experiment: evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"idk_retry": false, "iris": false, "rerank": false},
	}}
	if err := validateFormalMechanismBinding(legacyProto, options{}); err != nil {
		t.Fatalf("legacy control rejected: %v", err)
	}
	if err := validateFormalMechanismBinding(legacyProto, options{writeDedup: true}); err == nil {
		t.Fatal("legacy control accepted with a density flag request")
	}

	// Unknown mechanism key must be rejected.
	badProto := evalProtocol{Experiment: evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"idk_retry": false, "iris": false, "rerank": false, "mystery": true},
	}}
	if err := validateFormalMechanismBinding(badProto, options{}); err == nil {
		t.Fatal("unknown mechanism key accepted")
	}
}

func TestExtendCandidatesWithSiblingsAppendsSharedEvidenceFacts(t *testing.T) {
	// T017: neighbor extension (US2) — two facts sharing evidence; hitting one
	// must append the sibling to the answer context, and the extension must not
	// duplicate already-present candidates or add unrelated facts.
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	entries := memory.NewEntryStore(st.DB())
	evidence, err := entries.Ledger().AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:1", SourceType: memory.EvidenceMessage,
		SourceSessionID: "conv0-sess1", Speaker: "user", Ordinal: 0,
		Content: "the meeting was moved to Monday", RecordedAt: time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Two facts share the SAME evidence (siblings).
	sharedFact := &memory.Entry{Name: "meeting-moved-monday", Content: "The meeting was moved to Monday."}
	if err := entries.UpsertWithSources(ctx, sharedFact, []memory.EvidenceRef{{EvidenceID: evidence[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatal(err)
	}
	sharedFact2 := &memory.Entry{Name: "rescheduled-monday", Content: "The gathering is rescheduled to Monday."}
	if err := entries.UpsertWithSources(ctx, sharedFact2, []memory.EvidenceRef{{EvidenceID: evidence[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatal(err)
	}
	// Unrelated evidence + fact.
	otherEvidence, err := entries.Ledger().AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:2", SourceType: memory.EvidenceMessage,
		SourceSessionID: "conv0-sess1", Speaker: "user", Ordinal: 1,
		Content: "grocery list", RecordedAt: time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	unrelated := &memory.Entry{Name: "buy-groceries", Content: "Buy milk and eggs."}
	if err := entries.UpsertWithSources(ctx, unrelated, []memory.EvidenceRef{{EvidenceID: otherEvidence[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatal(err)
	}

	hits, err := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil).Search(ctx, "meeting Monday moved", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("retrieval = %#v, %v (need at least the meeting fact)", hits, err)
	}
	projections := memory.NewProjectionStore(st.DB())

	// Extension: sibling is appended, unrelated is not, no duplicates.
	extended := extendCandidatesWithSiblings(ctx, projections, entries, hits)
	seen := map[string]bool{}
	for _, hit := range extended {
		seen[hit.Name] = true
	}
	if !seen["rescheduled-monday"] {
		t.Fatalf("shared-evidence sibling not appended; candidates = %v", extended)
	}
	if seen["buy-groceries"] {
		t.Fatalf("unrelated fact must not be extended; candidates = %v", extended)
	}
	if len(seen) != len(extended) {
		t.Fatalf("extension produced duplicate candidates")
	}
	_ = unrelated
}