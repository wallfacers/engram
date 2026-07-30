package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func testEvalProtocol() evalProtocol {
	return evalProtocol{
		Schema:     evalProtocolSchema,
		ProtocolID: "locomo-b1-low",
		CreatedAt:  time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC),
		Git: evalGitProvenance{
			Commit: "0123456789012345678901234567890123456789",
			Dirty:  false,
		},
		Benchmark: evalBenchmarkProvenance{
			Name:              "locomo",
			DatasetDigest:     "sha256:dataset",
			Split:             "category_1_4",
			QuestionCount:     1540,
			QuestionIDsDigest: "sha256:questions",
		},
		Store: evalStoreProvenance{
			SchemaVersion:             7,
			IngestionRecipe:           "lossless",
			IngestionConfigDigest:     "sha256:ingestion",
			ProjectionBuilderVersions: map[string]string{"episode": "v1", "fact": "v1"},
		},
		Models: evalModelProvenance{
			Extractor: evalModelFingerprint{ID: "extractor", Revision: "r1", PromptDigest: "sha256:extract"},
			Answerer:  evalModelFingerprint{ID: "answerer", Revision: "r1", PromptDigest: "sha256:answer"},
			Judge:     evalModelFingerprint{ID: "judge", Revision: "r1", PromptDigest: "sha256:judge"},
			Planner:   evalPlannerFingerprint{Enabled: false},
		},
		Retrieval: evalRetrievalProvenance{
			Recipe:               "both",
			EmbeddingFingerprint: "sha256:embedding",
			Reranker:             "disabled",
			CandidateLimit:       30,
			CandidateRulesDigest: "sha256:candidate-rules",
		},
		Budget: evalBudgetProtocol{
			Profile:             "low",
			AnswerInputTokenCap: 1100,
			CandidateLimit:      30,
			RetrievalCallLimit:  1,
			AnswerCallLimit:     1,
			CounterFingerprint:  "sha256:counter",
		},
		Aggregation: evalAggregationProtocol{
			AnswerRepetitions: 3,
			Rule:              "majority_correctness",
			JudgeRepetitions:  1,
			SeedPolicy:        "independent-recorded",
		},
		JudgeAudit: evalJudgeAuditProtocol{
			AllDiscordant:            true,
			ConcordantSamplingDigest: "sha256:audit-sample",
			Reviewers:                2,
			BlindedToArm:             true,
			AdjudicationRule:         "independent_then_adjudicate",
		},
		CoverageStrata: evalCoverageStrataProtocol{
			Boundaries:      []float64{0, 0.5, 0.9, 1},
			SelectionDigest: "sha256:coverage-strata",
		},
		Experiment: evalExperimentProtocol{
			Stage:               "b1",
			Arm:                 "legacy_count_packer",
			ControlProtocolHash: "",
			PrimaryCohort:       "all",
			MechanismFlags:      map[string]bool{"idk_retry": false, "iris": false},
		},
	}
}

func TestEvalProtocolCanonicalJSONAndFingerprint(t *testing.T) {
	protocol := testEvalProtocol()
	canonical, err := canonicalEvalProtocolJSON(protocol)
	if err != nil {
		t.Fatalf("canonical protocol: %v", err)
	}
	if bytes.Contains(canonical, []byte("\"protocol_hash\"")) {
		t.Fatalf("canonical JSON must exclude self-referential protocol_hash: %s", canonical)
	}

	protocol.ProtocolHash = "sha256:stale"
	canonicalWithStaleHash, err := canonicalEvalProtocolJSON(protocol)
	if err != nil {
		t.Fatalf("canonical protocol with stale hash: %v", err)
	}
	if !bytes.Equal(canonical, canonicalWithStaleHash) {
		t.Fatalf("self hash changed canonical bytes:\n%s\n!=\n%s", canonical, canonicalWithStaleHash)
	}

	fingerprint, err := evalProtocolFingerprint(protocol)
	if err != nil {
		t.Fatalf("protocol fingerprint: %v", err)
	}
	if !strings.HasPrefix(fingerprint, "sha256:") {
		t.Fatalf("fingerprint = %q, want sha256 prefix", fingerprint)
	}

	reordered := testEvalProtocol()
	reordered.Store.ProjectionBuilderVersions = map[string]string{"fact": "v1", "episode": "v1"}
	reordered.Experiment.MechanismFlags = map[string]bool{"iris": false, "idk_retry": false}
	reorderedFingerprint, err := evalProtocolFingerprint(reordered)
	if err != nil {
		t.Fatalf("reordered protocol fingerprint: %v", err)
	}
	if fingerprint != reorderedFingerprint {
		t.Fatalf("map insertion order changed fingerprint: %q != %q", fingerprint, reorderedFingerprint)
	}

	changedCap := testEvalProtocol()
	changedCap.Budget.AnswerInputTokenCap++
	changedFingerprint, err := evalProtocolFingerprint(changedCap)
	if err != nil {
		t.Fatalf("changed-cap fingerprint: %v", err)
	}
	if fingerprint == changedFingerprint {
		t.Fatal("answer-input cap drift must change protocol fingerprint")
	}
}

func TestEvalProtocolDirtyRunPolicy(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.Git.Dirty = true

	if err := validateEvalProtocol(protocol, evalRunFormal); err == nil {
		t.Fatal("formal dirty protocol unexpectedly accepted")
	}
	if err := validateEvalProtocol(protocol, evalRunExploratory); err != nil {
		t.Fatalf("exploratory dirty protocol rejected: %v", err)
	}
	if isPromotionEligible(protocol, evalRunExploratory) {
		t.Fatal("dirty exploratory protocol must not be promotion eligible")
	}
}

func TestEvalProtocolResumeRefusesFingerprintDrift(t *testing.T) {
	runDir := t.TempDir()
	frozen, err := freezeEvalProtocol(runDir, testEvalProtocol(), evalRunFormal)
	if err != nil {
		t.Fatalf("freeze protocol: %v", err)
	}
	if frozen.ProtocolHash == "" {
		t.Fatal("frozen protocol has empty hash")
	}
	if err := checkEvalProtocolResume(runDir, testEvalProtocol(), evalRunFormal); err != nil {
		t.Fatalf("matching protocol refused resume: %v", err)
	}

	changed := testEvalProtocol()
	changed.Models.Answerer.PromptDigest = "sha256:changed-answer-prompt"
	err = checkEvalProtocolResume(runDir, changed, evalRunFormal)
	if err == nil {
		t.Fatal("answerer prompt drift unexpectedly allowed resume")
	}
	if !strings.Contains(err.Error(), "fresh --run-dir") {
		t.Fatalf("resume refusal = %v, want fresh run-dir guidance", err)
	}
}
