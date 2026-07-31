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
		{"compiler extractive", options{compilerArm: "extractive"}, "compiler", "extractive", map[string]bool{"compiler": true}, false},
		{"compiler exact_token", options{compilerArm: "exact_token"}, "compiler", "exact_token", map[string]bool{"compiler": true}, false},
		{"representation", options{representationArm: "semantic_episode"}, "representation_navigation", "semantic_episode", map[string]bool{"representation": true}, false},
		{"event projection", options{eventProjection: "E1"}, "event", "event_e1", map[string]bool{"event_projection": true}, false},
		{"gap refetch with projection", options{eventProjection: "E1", gapRefetch: true}, "gap", "structured_gap_refetch", map[string]bool{"event_projection": true, "gap_refetch": true}, false},
		{"gap without projection", options{gapRefetch: true}, "", "", nil, true},
		{"two mechanisms", options{compilerArm: "extractive", eventProjection: "E1"}, "", "", nil, true},
		{"bad compiler arm", options{compilerArm: "hybrid"}, "", "", nil, true},
		{"bad event projection", options{eventProjection: "E9"}, "", "", nil, true},
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
	treatment, err := buildFormalExperiment(options{compilerArm: "extractive"}, controlHash)
	if err != nil {
		t.Fatalf("build treatment experiment: %v", err)
	}
	if treatment.Stage != "compiler" || treatment.Arm != "extractive" || treatment.ControlProtocolHash != controlHash || !treatment.MechanismFlags["compiler"] {
		t.Fatalf("unexpected treatment experiment: %#v", treatment)
	}
}

func TestValidateFormalMechanismBindingTreatment(t *testing.T) {
	controlHash := "sha256:feedface"
	treatmentProtocol := testFormalProtocolBase(t)
	treatmentProtocol.Experiment = evalExperimentProtocol{
		Stage: "compiler", Arm: "extractive", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"compiler": true}, ControlProtocolHash: controlHash,
	}
	matching := options{compilerArm: "extractive"}
	if err := validateFormalMechanismBinding(treatmentProtocol, matching); err != nil {
		t.Fatalf("matching treatment binding refused: %v", err)
	}
	mismatched := options{compilerArm: "planner"}
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
	if loaded.Experiment.Stage != "compiler" || loaded.Experiment.Arm != "exact_token" {
		t.Fatalf("round-trip experiment mismatch: %#v", loaded.Experiment)
	}
	if loaded.Experiment.ControlProtocolHash != controlHash {
		t.Fatalf("round-trip control hash mismatch: %q", loaded.Experiment.ControlProtocolHash)
	}
	if !loaded.Experiment.MechanismFlags["compiler"] {
		t.Fatalf("round-trip mechanism flags mismatch: %#v", loaded.Experiment.MechanismFlags)
	}
}
