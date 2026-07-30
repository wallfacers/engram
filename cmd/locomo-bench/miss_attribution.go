package main

import (
	"fmt"
	"strings"
)

type evalMissClass string

const (
	evalMissGoldUnresolved evalMissClass = "gold_unresolved"
	evalMissCandidate      evalMissClass = "candidate_miss"
	evalMissCompiler       evalMissClass = "compiler_miss"
	evalMissAnswerer       evalMissClass = "answerer_miss"
	evalMissSuccess        evalMissClass = "success"
)

type evalMissAttributionInput struct {
	GoldResolved      bool
	CandidateCoverage float64
	BundleCoverage    float64
	MajorityCorrect   bool
}

func classifyEvalMiss(input evalMissAttributionInput) (evalMissClass, error) {
	if !validCoverage(input.CandidateCoverage) || !validCoverage(input.BundleCoverage) {
		return "", fmt.Errorf("candidate and bundle coverage must be within [0,1]")
	}
	switch {
	case !input.GoldResolved:
		return evalMissGoldUnresolved, nil
	case input.CandidateCoverage < 1:
		return evalMissCandidate, nil
	case input.BundleCoverage < 1:
		return evalMissCompiler, nil
	case !input.MajorityCorrect:
		return evalMissAnswerer, nil
	default:
		return evalMissSuccess, nil
	}
}

func validCoverage(coverage float64) bool {
	return coverage >= 0 && coverage <= 1
}

const evalStageFixedGoldOracle = "fixed_gold_oracle"

type evalFixedGoldOracleRequest struct {
	Stage          string
	DiagnosticOnly bool
	ProtocolHash   string
	CandidateIDs   []string
}

func validateFixedGoldOracleRequest(request evalFixedGoldOracleRequest) error {
	if request.Stage != evalStageFixedGoldOracle || !request.DiagnosticOnly || !isDigest(request.ProtocolHash) || len(request.CandidateIDs) == 0 {
		return fmt.Errorf("fixed-gold oracle must be a diagnostic-only run with frozen evidence")
	}
	seen := map[string]bool{}
	for _, id := range request.CandidateIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return fmt.Errorf("fixed-gold oracle candidate IDs must be unique and non-empty")
		}
		seen[id] = true
	}
	return nil
}

func oracleContributesToPromotion(request evalFixedGoldOracleRequest) bool {
	return !request.DiagnosticOnly && request.Stage != evalStageFixedGoldOracle
}

// resolveDatasetSourceIDs maps dataset-level IDs to immutable Ledger IDs
// without dropping anything it cannot map. Callers retain unresolved IDs in
// the artifact and classify those questions as gold_unresolved for source
// survival metrics while keeping them in the answer-accuracy denominator.
func resolveDatasetSourceIDs(datasetSourceIDs []string, evidenceByDatasetID map[string]string) (resolved, unresolved []string, err error) {
	seenDataset := map[string]bool{}
	seenEvidence := map[string]bool{}
	for _, rawID := range datasetSourceIDs {
		datasetID := strings.TrimSpace(rawID)
		if datasetID == "" {
			return nil, nil, fmt.Errorf("dataset source ID is empty")
		}
		if seenDataset[datasetID] {
			continue
		}
		seenDataset[datasetID] = true
		evidenceID := strings.TrimSpace(evidenceByDatasetID[datasetID])
		if evidenceID == "" {
			unresolved = append(unresolved, datasetID)
			continue
		}
		if !seenEvidence[evidenceID] {
			resolved = append(resolved, evidenceID)
			seenEvidence[evidenceID] = true
		}
	}
	return resolved, unresolved, nil
}
