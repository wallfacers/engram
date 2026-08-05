package main

import (
	"fmt"
	"strings"
)

type evalMissClass string

const (
	evalMissGoldUnresolved evalMissClass = "gold_unresolved"
	evalMissCandidate      evalMissClass = "candidate_miss"
	evalMissResolution     evalMissClass = "resolution_miss"
	evalMissCompiler       evalMissClass = "compiler_miss"
	evalMissAnswerer       evalMissClass = "answerer_miss"
	evalMissSuccess        evalMissClass = "success"
)

// evalTemporalOracleVerdict is the 027 candidate-oracle verdict (spec US3 /
// FR-008). It distinguishes a resolution miss — the superseded/current versions
// ARE in the candidate pool but the harness did not organize them — from a
// candidate miss — the required version is absent from the pool, which no
// query-time resolver can fix (LazyMem: low recall cannot be saved at query
// time). The verdict is deterministic and offline; it refines a base
// candidate_miss using pool-side version multiplicity only.
type evalTemporalOracleVerdict struct {
	ResolutionApplicable bool `json:"resolution_applicable"` // query has temporal semantics
	SupersededInPool     bool `json:"superseded_in_pool"`    // any entity has >1 time version in pool
	PoolVersionCount     int  `json:"pool_version_count"`    // max per-entity version count
	ResolutionMiss       bool `json:"resolution_miss"`       // versions in pool → resolver may help
	CandidateMiss        bool `json:"candidate_miss"`        // versions absent → resolver cannot help
}

// resolveTemporalOracle derives the verdict from the query's deterministic mode
// and each entity's pool-side version multiplicity. versionsInPool carries one
// entry per distinct entity (the number of time-distinct candidates that entity
// has inside the frozen candidate pool); an empty/absent slice means no entity
// grouping was possible.
func resolveTemporalOracle(query string, versionsInPool []int) evalTemporalOracleVerdict {
	mode := classifyQueryMode(query)
	verdict := evalTemporalOracleVerdict{
		ResolutionApplicable: mode != ResolutionDegraded,
	}
	for _, count := range versionsInPool {
		if count > verdict.PoolVersionCount {
			verdict.PoolVersionCount = count
		}
		if count > 1 {
			verdict.SupersededInPool = true
		}
	}
	if verdict.ResolutionApplicable {
		if verdict.SupersededInPool {
			verdict.ResolutionMiss = true
		} else {
			verdict.CandidateMiss = true
		}
	}
	return verdict
}

// classifyTemporalMiss refines a base miss class with the candidate oracle.
// A base candidate_miss with a resolution-applicable query and superseded
// versions present in the pool becomes resolution_miss (027 may help); anything
// else stays as-is (the resolver cannot change what is not in the pool).
func classifyTemporalMiss(base evalMissClass, verdict evalTemporalOracleVerdict) evalMissClass {
	if base == evalMissCandidate && verdict.ResolutionMiss {
		return evalMissResolution
	}
	return base
}

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
	Stage                   string
	DiagnosticOnly          bool
	ProtocolHash            string
	CandidateIDs            []string
	EmptyEvidenceAbstention bool
}

func validateFixedGoldOracleRequest(request evalFixedGoldOracleRequest) error {
	if request.Stage != evalStageFixedGoldOracle || !request.DiagnosticOnly || !isDigest(request.ProtocolHash) ||
		(len(request.CandidateIDs) == 0 && !request.EmptyEvidenceAbstention) {
		return fmt.Errorf("fixed-gold oracle must be a diagnostic-only run with frozen evidence")
	}
	if request.EmptyEvidenceAbstention && len(request.CandidateIDs) != 0 {
		return fmt.Errorf("fixed-gold oracle abstention cannot mix empty and cited evidence")
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
