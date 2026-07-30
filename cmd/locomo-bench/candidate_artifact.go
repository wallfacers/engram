package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type evalCandidateMode string

const (
	evalCandidateModeNavigation      evalCandidateMode = "navigation"
	evalCandidateModeAnchorRendering evalCandidateMode = "anchor_rendering"
	evalCandidateModeCompilerReplay  evalCandidateMode = "compiler_replay"
	evalCandidateModeGapUnion        evalCandidateMode = "gap_union"
)

type evalRankedAnchor struct {
	CandidateID string   `json:"candidate_id"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
	TextDigest  string   `json:"text_digest"`
	SourceIDs   []string `json:"source_ids"`
}

type evalRenderedCandidate struct {
	CandidateID       string   `json:"candidate_id"`
	Kind              string   `json:"kind"`
	Rank              int      `json:"rank"`
	Score             float64  `json:"score"`
	Text              string   `json:"text"`
	TextDigest        string   `json:"text_digest"`
	SourceIDs         []string `json:"source_ids"`
	ExpandedFrom      []string `json:"expanded_from"`
	ExpansionCount    int      `json:"expansion_count"`
	PreCapInputTokens int      `json:"pre_cap_input_tokens"`
	Truncated         bool     `json:"truncated"`
}

type evalGoldResolution struct {
	DatasetSourceIDs       []string `json:"dataset_source_ids"`
	ResolvedEvidenceIDs    []string `json:"resolved_evidence_ids"`
	UnresolvedIDs          []string `json:"unresolved_ids,omitempty"`
	AnchorSourceCoverage   float64  `json:"anchor_source_coverage"`
	RenderedSourceCoverage float64  `json:"rendered_source_coverage"`
}

type evalCandidateArtifact struct {
	Schema             string                  `json:"schema"`
	ProtocolHash       string                  `json:"protocol_hash"`
	QuestionID         string                  `json:"question_id"`
	QueryDigest        string                  `json:"query_digest"`
	Mode               evalCandidateMode       `json:"mode"`
	AnchorDigest       string                  `json:"anchor_digest"`
	CandidateSetDigest string                  `json:"candidate_set_digest"`
	RetrievalCalls     int                     `json:"retrieval_calls"`
	Anchors            []evalRankedAnchor      `json:"anchors"`
	RenderedCandidates []evalRenderedCandidate `json:"rendered_candidates"`
	Gold               evalGoldResolution      `json:"gold"`
	CoverageStratum    string                  `json:"coverage_stratum"`
}

func rankedAnchorDigest(anchors []evalRankedAnchor) string {
	return evalJSONDigest(anchors)
}

func renderedCandidateSetDigest(candidates []evalRenderedCandidate) string {
	return evalJSONDigest(candidates)
}

func evalJSONDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sourceCoverage(required, available []string) float64 {
	requiredSet := stringSet(required)
	if len(requiredSet) == 0 {
		return 1
	}
	availableSet := stringSet(available)
	matched := 0
	for id := range requiredSet {
		if availableSet[id] {
			matched++
		}
	}
	return float64(matched) / float64(len(requiredSet))
}

func coverageStratumFor(boundaries []float64, coverage float64) string {
	if len(boundaries) < 2 || coverage < 0 || coverage > 1 {
		return ""
	}
	for index := 1; index < len(boundaries); index++ {
		upper := boundaries[index]
		if coverage < upper || (index == len(boundaries)-1 && coverage <= upper) {
			close := ")"
			if index == len(boundaries)-1 {
				close = "]"
			}
			return fmt.Sprintf("[%.3f,%.3f%s", boundaries[index-1], upper, close)
		}
	}
	return ""
}

func validateEvalCandidateArtifact(protocol evalProtocol, artifact evalCandidateArtifact) error {
	if artifact.Schema != evalProtocolSchema || artifact.ProtocolHash != protocol.ProtocolHash {
		return fmt.Errorf("candidate artifact schema/protocol hash mismatch")
	}
	if strings.TrimSpace(artifact.QuestionID) == "" || !isDigest(artifact.QueryDigest) {
		return fmt.Errorf("candidate artifact requires question ID and query digest")
	}
	if !validCandidateMode(artifact.Mode) || artifact.RetrievalCalls < 0 {
		return fmt.Errorf("invalid candidate mode or retrieval calls")
	}
	if err := validateRankedAnchors(artifact.Anchors); err != nil {
		return err
	}
	if artifact.AnchorDigest != rankedAnchorDigest(artifact.Anchors) {
		return fmt.Errorf("anchor digest mismatch")
	}
	if err := validateRenderedCandidates(artifact.Anchors, artifact.RenderedCandidates); err != nil {
		return err
	}
	if artifact.CandidateSetDigest != renderedCandidateSetDigest(artifact.RenderedCandidates) {
		return fmt.Errorf("candidate-set digest mismatch")
	}
	required := artifact.Gold.ResolvedEvidenceIDs
	anchorCoverage := sourceCoverage(required, collectAnchorSources(artifact.Anchors))
	if !sameCoverage(artifact.Gold.AnchorSourceCoverage, anchorCoverage) {
		return fmt.Errorf("anchor source coverage mismatch")
	}
	renderedCoverage := sourceCoverage(required, collectRenderedSources(artifact.RenderedCandidates))
	if !sameCoverage(artifact.Gold.RenderedSourceCoverage, renderedCoverage) {
		return fmt.Errorf("rendered source coverage mismatch")
	}
	if want := coverageStratumFor(protocol.CoverageStrata.Boundaries, renderedCoverage); artifact.CoverageStratum != want {
		return fmt.Errorf("coverage stratum %q, want %q", artifact.CoverageStratum, want)
	}
	return nil
}

func validCandidateMode(mode evalCandidateMode) bool {
	switch mode {
	case evalCandidateModeNavigation, evalCandidateModeAnchorRendering, evalCandidateModeCompilerReplay, evalCandidateModeGapUnion:
		return true
	default:
		return false
	}
}

func validateRankedAnchors(anchors []evalRankedAnchor) error {
	if len(anchors) == 0 {
		return fmt.Errorf("candidate artifact requires at least one anchor")
	}
	seen := map[string]bool{}
	for index, anchor := range anchors {
		if strings.TrimSpace(anchor.CandidateID) == "" || anchor.Rank != index+1 || !isDigest(anchor.TextDigest) || len(anchor.SourceIDs) == 0 || seen[anchor.CandidateID] {
			return fmt.Errorf("invalid ranked anchor at index %d", index)
		}
		seen[anchor.CandidateID] = true
	}
	return nil
}

func validateRenderedCandidates(anchors []evalRankedAnchor, candidates []evalRenderedCandidate) error {
	if len(candidates) == 0 {
		return fmt.Errorf("candidate artifact requires rendered candidates")
	}
	anchorIDs := map[string]bool{}
	for _, anchor := range anchors {
		anchorIDs[anchor.CandidateID] = true
	}
	seen := map[string]bool{}
	for index, candidate := range candidates {
		if strings.TrimSpace(candidate.CandidateID) == "" || candidate.Rank != index+1 || candidate.ExpansionCount < 0 || candidate.PreCapInputTokens < 0 || len(candidate.SourceIDs) == 0 || seen[candidate.CandidateID] {
			return fmt.Errorf("invalid rendered candidate at index %d", index)
		}
		if !isDigest(candidate.TextDigest) || candidate.TextDigest != evalTextDigest(candidate.Text) {
			return fmt.Errorf("rendered candidate %q text digest mismatch", candidate.CandidateID)
		}
		for _, expanded := range candidate.ExpandedFrom {
			if !anchorIDs[expanded] {
				return fmt.Errorf("rendered candidate %q expands unknown anchor %q", candidate.CandidateID, expanded)
			}
		}
		seen[candidate.CandidateID] = true
	}
	return nil
}

func evalTextDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func collectAnchorSources(anchors []evalRankedAnchor) []string {
	var sources []string
	for _, anchor := range anchors {
		sources = append(sources, anchor.SourceIDs...)
	}
	return sources
}

func collectRenderedSources(candidates []evalRenderedCandidate) []string {
	var sources []string
	for _, candidate := range candidates {
		sources = append(sources, candidate.SourceIDs...)
	}
	return sources
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = true
		}
	}
	return set
}

func sameCoverage(left, right float64) bool {
	return math.Abs(left-right) < 1e-12
}

func writeEvalCandidateArtifacts(path string, artifacts []evalCandidateArtifact) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create candidate artifact directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	writer := bufio.NewWriter(tmp)
	encoder := json.NewEncoder(writer)
	for _, artifact := range artifacts {
		if err := encoder.Encode(artifact); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func readEvalCandidateArtifacts(path string) ([]evalCandidateArtifact, error) {
	f, err := os.Open(path) //nolint:gosec // run artifact is operator-selected
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var artifacts []evalCandidateArtifact
	for scanner.Scan() {
		var artifact evalCandidateArtifact
		if err := json.Unmarshal(scanner.Bytes(), &artifact); err != nil {
			return nil, fmt.Errorf("decode candidate artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan candidate artifacts: %w", err)
	}
	return artifacts, nil
}
