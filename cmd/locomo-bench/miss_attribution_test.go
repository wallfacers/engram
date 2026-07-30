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
