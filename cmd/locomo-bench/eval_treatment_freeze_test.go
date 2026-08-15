package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// testFormalProtocolBase builds a minimal but fully valid 022.v1 protocol
// body (everything except Experiment) so freeze/validate round trips can be
// exercised offline without a dataset, git state, or model endpoints.
func testFormalProtocolBase(t *testing.T) evalProtocol {
	t.Helper()
	commit := strings.Repeat("a", 40)
	questionIDs := []string{"q-1"}
	base := evalProtocol{
		Schema: evalProtocolSchema, ProtocolID: "test-treatment", CreatedAt: time.Now().UTC(),
		Git: evalGitProvenance{Commit: commit, Dirty: false},
		Benchmark: evalBenchmarkProvenance{
			Name: "locomo", DatasetDigest: evalTextDigest("fixture"), Split: "category_1_4",
			QuestionCount: 1540, QuestionIDsDigest: evalJSONDigest(questionIDs),
		},
		Store: evalStoreProvenance{
			SchemaVersion: 7, IngestionRecipe: "ledger_lossless_chunks_v2",
			IngestionConfigDigest:     evalJSONDigest(map[string]any{"chunks": true}),
			ProjectionBuilderVersions: map[string]string{"atomic_fact": "entry_store_explicit_v1"},
		},
		Models: evalModelProvenance{
			Extractor: evalModelFingerprint{ID: "answerer-x", Revision: "answerer-x", Provider: "openai", PromptDigest: evalTextDigest("extract")},
			Answerer:  evalModelFingerprint{ID: "answerer-x", Revision: "answerer-x", Provider: "openai", PromptDigest: evalTextDigest("answer")},
			Judge:     evalModelFingerprint{ID: "judge-x", Revision: "judge-x", Provider: "openai", PromptDigest: evalTextDigest("judge")},
			Planner:   evalPlannerFingerprint{Enabled: false},
		},
		Retrieval: evalRetrievalProvenance{
			Recipe: "ledger_lossless_chunks_v2", EmbeddingFingerprint: evalTextDigest("embed"),
			Reranker: "disabled", CandidateLimit: 20,
			CandidateRulesDigest: evalJSONDigest(map[string]any{"top_k": 20}),
		},
		Budget: evalBudgetProtocol{
			Profile: "low", AnswerInputTokenCap: 1000, MaxOutputTokens: 512, CandidateLimit: 20,
			RetrievalCallLimit: 1, AnswerCallLimit: 1, CounterFingerprint: evalTextDigest("counter"),
		},
		Aggregation: evalAggregationProtocol{
			AnswerRepetitions: 3, Rule: "majority_correctness", JudgeRepetitions: 1, SeedPolicy: "independent-recorded",
		},
		JudgeAudit: evalJudgeAuditProtocol{
			AllDiscordant: true, ConcordantSamplingDigest: evalTextDigest("concordant"),
			Reviewers: 2, BlindedToArm: true, AdjudicationRule: "independent_then_adjudicate",
		},
		CoverageStrata: evalCoverageStrataProtocol{
			Boundaries: []float64{0, 0.5, 0.9, 1}, SelectionDigest: evalTextDigest("strata"),
		},
		Experiment: evalExperimentProtocol{
			Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
			MechanismFlags: map[string]bool{"idk_retry": false, "iris": false, "rerank": false},
		},
	}
	if err := validateEvalProtocol(base, evalRunFormal); err != nil {
		t.Fatalf("test protocol base must validate: %v", err)
	}
	return base
}

func TestFormalTreatmentForOptions(t *testing.T) {
	cases := []struct {
		name      string
		opt       options
		wantStage string
		wantArm   string
		wantFlags map[string]bool
		wantErr   bool
	}{
		{"no mechanism is legacy", options{representationArm: ReprChunk900}, "", "", nil, false},
		{"compiler extractive", options{compilerArm: "extractive"}, "", "", nil, false},
		{"compiler exact_token", options{compilerArm: "exact_token"}, "", "", nil, false},
		{"representation", options{representationArm: "semantic_episode"}, "representation_navigation", "semantic_episode", map[string]bool{"representation": true}, false},
		{"bad compiler arm", options{compilerArm: "hybrid"}, "", "", nil, false},
	}
	for _, tc := range cases {
		got, err := formalTreatmentForOptions(tc.opt)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %#v", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if got.Stage != tc.wantStage || got.Arm != tc.wantArm || !reflect.DeepEqual(got.MechanismFlags, tc.wantFlags) {
			t.Errorf("%s: formalTreatmentForOptions = %#v, want stage=%q arm=%q flags=%#v", tc.name, got, tc.wantStage, tc.wantArm, tc.wantFlags)
		}
	}
}

func TestBuildFormalExperiment(t *testing.T) {
	controlHash := "sha256:feedface"
	legacy, err := buildFormalExperiment(options{representationArm: ReprChunk900}, "")
	if err != nil {
		t.Fatalf("build legacy experiment: %v", err)
	}
	if legacy.Stage != "b1" || legacy.Arm != "legacy_count_packer" || legacy.ControlProtocolHash != "" || !isFormalLegacyControlMechanismFlags(legacy.MechanismFlags) {
		t.Fatalf("unexpected legacy experiment: %#v", legacy)
	}
	// 026: compiler-arm is a b1-stage additive mechanism (like episode_cluster),
	// so a compiler treatment freezes as b1/legacy_count_packer with the compiler
	// flag set — the runtime picks the packer/compiler path from opt.compilerArm,
	// not from a distinct stage.
	compiler, err := buildFormalExperiment(options{compilerArm: "extractive"}, controlHash)
	if err != nil {
		t.Fatalf("build compiler experiment: %v", err)
	}
	if compiler.Stage != "b1" || compiler.Arm != "legacy_count_packer" || !compiler.MechanismFlags["compiler"] || !compiler.MechanismFlags["idk_retry"] == false {
		t.Fatalf("unexpected compiler experiment: %#v", compiler)
	}
	// A standalone treatment (representation/event) still freezes as its own
	// stage bound to the control hash.
	rep, err := buildFormalExperiment(options{representationArm: ReprRawTurnWindow}, controlHash)
	if err != nil {
		t.Fatalf("build representation experiment: %v", err)
	}
	if rep.Stage != "representation_navigation" || rep.Arm != "raw_turn_window" || rep.ControlProtocolHash != controlHash {
		t.Fatalf("unexpected representation experiment: %#v", rep)
	}
}

func TestValidateFormalMechanismBindingTreatment(t *testing.T) {
	controlHash := "sha256:feedface"
	treatmentProtocol := testFormalProtocolBase(t)
	treatmentProtocol.Experiment = evalExperimentProtocol{
		Stage: "representation_navigation", Arm: "raw_turn_window", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"representation": true}, ControlProtocolHash: controlHash,
	}
	matching := options{representationArm: ReprRawTurnWindow}
	if err := validateFormalMechanismBinding(treatmentProtocol, matching); err != nil {
		t.Fatalf("matching treatment binding refused: %v", err)
	}
	mismatched := options{representationArm: ReprSemanticEpisode}
	if err := validateFormalMechanismBinding(treatmentProtocol, mismatched); err == nil {
		t.Fatal("mismatched treatment arm was accepted")
	}
	noFlags := options{}
	if err := validateFormalMechanismBinding(treatmentProtocol, noFlags); err == nil {
		t.Fatal("treatment manifest without matching CLI flags was accepted")
	}
	noControlHash := treatmentProtocol
	noControlHash.Experiment.ControlProtocolHash = ""
	if err := validateFormalMechanismBinding(noControlHash, matching); err == nil {
		t.Fatal("treatment manifest without control protocol hash was accepted")
	}
	legacyProtocol := testFormalProtocolBase(t)
	legacyProtocol.Experiment = evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"idk_retry": false, "iris": false, "rerank": false},
	}
	if err := validateFormalMechanismBinding(legacyProtocol, matching); err == nil {
		t.Fatal("b1 control with treatment flags was accepted")
	}
	if err := validateFormalMechanismBinding(legacyProtocol, options{representationArm: ReprChunk900}); err != nil {
		t.Fatalf("b1 control without treatment flags refused: %v", err)
	}
}

// TestValidateFormalMechanismBindingCompilerAdditive: the compiler arm is a
// b1-stage additive mechanism (026), so a b1 manifest carrying the compiler
// flag must match CLI --compiler-arm via the density mechanism binding, exactly
// like episode_cluster.
func TestValidateFormalMechanismBindingCompilerAdditive(t *testing.T) {
	compilerProtocol := testFormalProtocolBase(t)
	compilerProtocol.Experiment = evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"compiler": true, "idk_retry": false, "iris": false, "rerank": false},
	}
	if err := validateFormalMechanismBinding(compilerProtocol, options{compilerArm: "extractive"}); err != nil {
		t.Fatalf("matching compiler additive binding refused: %v", err)
	}
	if err := validateFormalMechanismBinding(compilerProtocol, options{compilerArm: "exact_token"}); err != nil {
		t.Fatalf("matching compiler additive binding refused: %v", err)
	}
	if err := validateFormalMechanismBinding(compilerProtocol, options{}); err == nil {
		t.Fatal("compiler manifest without --compiler-arm was accepted")
	}
}

func TestFreezeFormalTreatmentManifestRoundTrip(t *testing.T) {
	controlHash := "sha256:feedface"
	base := testFormalProtocolBase(t)
	experiment, err := buildFormalExperiment(options{compilerArm: "exact_token"}, controlHash)
	if err != nil {
		t.Fatalf("build experiment: %v", err)
	}
	base.Experiment = experiment
	path := t.TempDir() + "/protocol.json"
	frozen, err := freezeEvalProtocolFile(path, base, evalRunFormal)
	if err != nil {
		t.Fatalf("freeze treatment manifest: %v", err)
	}
	if !isDigest(frozen.ProtocolHash) {
		t.Fatalf("frozen treatment protocol has no protocol hash: %#v", frozen)
	}
	loaded, err := readEvalProtocolFileMode(path, evalRunFormal)
	if err != nil {
		t.Fatalf("read frozen treatment manifest: %v", err)
	}
	// 026: compiler-arm is a b1 additive mechanism — the round-trip keeps
	// arm=legacy_count_packer with the compiler flag set.
	if loaded.Experiment.Stage != "b1" || loaded.Experiment.Arm != "legacy_count_packer" {
		t.Fatalf("round-trip experiment mismatch: %#v", loaded.Experiment)
	}
	if !loaded.Experiment.MechanismFlags["compiler"] {
		t.Fatalf("round-trip mechanism flags mismatch: %#v", loaded.Experiment.MechanismFlags)
	}
}
