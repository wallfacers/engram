package main

import "testing"

func TestMissAttributionIsMutuallyExclusiveAndOrdered(t *testing.T) {
	cases := []struct {
		name string
		in   evalMissAttributionInput
		want evalMissClass
	}{
		{
			name: "gold unresolved wins even if answer happened to be correct",
			in:   evalMissAttributionInput{GoldResolved: false, CandidateCoverage: 1, BundleCoverage: 1, MajorityCorrect: true},
			want: evalMissGoldUnresolved,
		},
		{
			name: "candidate missed required gold",
			in:   evalMissAttributionInput{GoldResolved: true, CandidateCoverage: 0.5, BundleCoverage: 0.5, MajorityCorrect: false},
			want: evalMissCandidate,
		},
		{
			name: "compiler omitted gold already in candidate",
			in:   evalMissAttributionInput{GoldResolved: true, CandidateCoverage: 1, BundleCoverage: 0.5, MajorityCorrect: false},
			want: evalMissCompiler,
		},
		{
			name: "answerer failed with fully covered bundle",
			in:   evalMissAttributionInput{GoldResolved: true, CandidateCoverage: 1, BundleCoverage: 1, MajorityCorrect: false},
			want: evalMissAnswerer,
		},
		{
			name: "success",
			in:   evalMissAttributionInput{GoldResolved: true, CandidateCoverage: 1, BundleCoverage: 1, MajorityCorrect: true},
			want: evalMissSuccess,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyEvalMiss(tc.in)
			if err != nil {
				t.Fatalf("classify miss: %v", err)
			}
			if got != tc.want {
				t.Fatalf("miss class = %q, want %q", got, tc.want)
			}
		})
	}

	if _, err := classifyEvalMiss(evalMissAttributionInput{GoldResolved: true, CandidateCoverage: 1.1, BundleCoverage: 1, MajorityCorrect: false}); err == nil {
		t.Fatal("coverage above one unexpectedly accepted")
	}
}

// TestTemporalResolutionCandidateOracle (T021) — candidate-oracle 归因:
// 每题 superseded/current 版本在池判定, 区分 resolution miss (版本在池但未解析,
// 027 可救) 与 candidate miss (版本不在池, 027 救不了)。
func TestTemporalResolutionCandidateOracle(t *testing.T) {
	// (1) current_value query + 池内含 2 版本 → resolution applicable + superseded 在池。
	v := resolveTemporalOracle("What is Alice's current email address?", []int{2})
	if !v.ResolutionApplicable || !v.SupersededInPool || v.PoolVersionCount != 2 {
		t.Fatalf("expected applicable+superseded verdict, got %+v", v)
	}
	if !v.ResolutionMiss || v.CandidateMiss {
		t.Fatalf("expected resolution miss (versions in pool), got %+v", v)
	}
	// base candidate_miss 细化 → resolution_miss。
	if got := classifyTemporalMiss(evalMissCandidate, v); got != evalMissResolution {
		t.Fatalf("base candidate_miss refined to %q, want resolution_miss", got)
	}
	// 其它 miss 类不细化。
	if got := classifyTemporalMiss(evalMissAnswerer, v); got != evalMissAnswerer {
		t.Fatalf("answerer_miss must not be refined, got %q", got)
	}

	// (2) current_value query + 池内只有 1 版本 → 无 supersede, candidate miss。
	single := resolveTemporalOracle("What is Alice's current email address?", []int{1})
	if !single.ResolutionApplicable || single.SupersededInPool {
		t.Fatalf("expected applicable single-version verdict, got %+v", single)
	}
	if single.ResolutionMiss || !single.CandidateMiss {
		t.Fatalf("expected candidate miss (no superseded version in pool), got %+v", single)
	}
	if got := classifyTemporalMiss(evalMissCandidate, single); got != evalMissCandidate {
		t.Fatalf("single-version candidate miss must stay candidate_miss, got %q", got)
	}

	// (3) 无 temporal 语义 query → 解析不适用, 不归因到 resolution。
	degraded := resolveTemporalOracle("what happened in the meeting", []int{2})
	if degraded.ResolutionApplicable {
		t.Fatalf("degraded query must not be resolution-applicable: %+v", degraded)
	}
	if degraded.ResolutionMiss || degraded.CandidateMiss {
		t.Fatalf("degraded query must not claim a temporal miss: %+v", degraded)
	}

	// (4) 无实体分组 (空版本列表) → candidate miss (无版本可判定 supersede)。
	noGroup := resolveTemporalOracle("What is Alice's current email address?", nil)
	if noGroup.SupersededInPool || !noGroup.CandidateMiss {
		t.Fatalf("ungrouped pool must be candidate miss, got %+v", noGroup)
	}
}

func TestFixedGoldOracleIsDiagnosticOnly(t *testing.T) {
	oracle := evalFixedGoldOracleRequest{
		Stage:          evalStageFixedGoldOracle,
		DiagnosticOnly: true,
		ProtocolHash:   "sha256:control",
		CandidateIDs:   []string{"gold-evidence-1"},
	}
	if err := validateFixedGoldOracleRequest(oracle); err != nil {
		t.Fatalf("valid diagnostic oracle rejected: %v", err)
	}
	if oracleContributesToPromotion(oracle) {
		t.Fatal("diagnostic fixed-gold oracle must not contribute to promotion")
	}
	if err := validateFixedGoldOracleRequest(evalFixedGoldOracleRequest{
		Stage:                   evalStageFixedGoldOracle,
		DiagnosticOnly:          true,
		ProtocolHash:            "sha256:control",
		EmptyEvidenceAbstention: true,
	}); err != nil {
		t.Fatalf("valid empty-evidence abstention oracle rejected: %v", err)
	}

	oracle.DiagnosticOnly = false
	if err := validateFixedGoldOracleRequest(oracle); err == nil {
		t.Fatal("non-diagnostic fixed-gold oracle unexpectedly accepted")
	}
}

func TestResolveDatasetSourceIDsPreservesUnresolvedGold(t *testing.T) {
	resolved, unresolved, err := resolveDatasetSourceIDs(
		[]string{"D1:2", "D1:3", "D1:2"},
		map[string]string{"D1:2": "evidence-2"},
	)
	if err != nil {
		t.Fatalf("resolve dataset source IDs: %v", err)
	}
	if got, want := len(resolved), 1; got != want || resolved[0] != "evidence-2" {
		t.Fatalf("resolved IDs = %v, want [evidence-2]", resolved)
	}
	if got, want := len(unresolved), 1; got != want || unresolved[0] != "D1:3" {
		t.Fatalf("unresolved IDs = %v, want [D1:3]", unresolved)
	}
	if _, _, err := resolveDatasetSourceIDs([]string{"  "}, nil); err == nil {
		t.Fatal("blank dataset source ID unexpectedly accepted")
	}
}
