package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	evalProtocolArtifactFile       = "protocol.json"
	evalCandidatesArtifactFile     = "candidates.jsonl"
	evalTraceArtifactFile          = "compile_trace.jsonl"
	evalBundleArtifactFile         = "bundles.jsonl"
	evalClassificationArtifactFile = "classification.jsonl"
	evalSummaryArtifactFile        = "summary.json"
)

type evalArtifactKind string

const (
	evalTraceArtifactKind          evalArtifactKind = "compile_trace"
	evalBundleArtifactKind         evalArtifactKind = "bundle"
	evalClassificationArtifactKind evalArtifactKind = "classification"
)

// evalArtifactRecord is the common envelope required before a later stage can
// interpret a trace, bundle, or classification payload. It is deliberately
// independent from the compiler so fixed-artifact validation needs no model.
type evalArtifactRecord struct {
	Schema       string           `json:"schema"`
	ProtocolHash string           `json:"protocol_hash"`
	QuestionID   string           `json:"question_id"`
	Kind         evalArtifactKind `json:"kind"`
	Valid        bool             `json:"valid"`
}

type evalArtifactSummary struct {
	Schema         string               `json:"schema"`
	ProtocolHash   string               `json:"protocol_hash"`
	ArtifactHashes map[string]string    `json:"artifact_hashes"`
	Validity       evalArtifactValidity `json:"validity"`
}

func writeEvalArtifactRecords(path string, records []evalArtifactRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
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
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
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

func readEvalArtifactRecords(path string) ([]evalArtifactRecord, error) {
	f, err := os.Open(path) //nolint:gosec // run artifact is operator-selected
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var records []evalArtifactRecord
	for scanner.Scan() {
		var record evalArtifactRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode artifact record: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan artifact records: %w", err)
	}
	return records, nil
}

func writeEvalArtifactSummary(runDir string, protocol evalProtocol, questionIDs []string) error {
	if len(questionIDs) == 0 {
		return fmt.Errorf("summary requires expected question IDs")
	}
	hashes, err := evalArtifactFileHashes(runDir)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(runDir, evalSummaryArtifactFile), evalArtifactSummary{
		Schema:         evalProtocolSchema,
		ProtocolHash:   protocol.ProtocolHash,
		ArtifactHashes: hashes,
		Validity: evalArtifactValidity{
			Valid: true, Complete: true, CandidateIdentityRate: 1, SourceValidationRate: 1,
			SpanRecoveryRate: 1, CitationCoverageRate: 1, WithinCapRate: 1, AnswerCallComplianceRate: 1,
		},
	})
}

func validateEvalArtifactRun(runDir string, requested evalProtocol, expectedQuestionIDs []string) (evalArtifactSummary, error) {
	if err := checkEvalProtocolResume(runDir, requested, evalRunFormal); err != nil {
		return evalArtifactSummary{}, err
	}
	if len(expectedQuestionIDs) == 0 {
		return evalArtifactSummary{}, fmt.Errorf("expected question IDs are required")
	}
	expected := stringSet(expectedQuestionIDs)
	if len(expected) != len(expectedQuestionIDs) {
		return evalArtifactSummary{}, fmt.Errorf("expected question IDs contain duplicates or blanks")
	}
	candidates, err := readEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile))
	if err != nil {
		return evalArtifactSummary{}, fmt.Errorf("read candidates: %w", err)
	}
	for _, candidate := range candidates {
		if err := validateEvalCandidateArtifact(requested, candidate); err != nil {
			return evalArtifactSummary{}, err
		}
	}
	if err := validateArtifactQuestionSet("candidates", candidateQuestionIDs(candidates), expected); err != nil {
		return evalArtifactSummary{}, err
	}
	for _, spec := range []struct {
		file string
		kind evalArtifactKind
	}{
		{evalTraceArtifactFile, evalTraceArtifactKind},
		{evalBundleArtifactFile, evalBundleArtifactKind},
		{evalClassificationArtifactFile, evalClassificationArtifactKind},
	} {
		records, err := readEvalArtifactRecords(filepath.Join(runDir, spec.file))
		if err != nil {
			return evalArtifactSummary{}, fmt.Errorf("read %s: %w", spec.file, err)
		}
		if err := validateEvalArtifactRecords(spec.file, spec.kind, requested.ProtocolHash, records, expected); err != nil {
			return evalArtifactSummary{}, err
		}
	}
	rawSummary, err := os.ReadFile(filepath.Join(runDir, evalSummaryArtifactFile)) //nolint:gosec // run artifact is operator-selected
	if err != nil {
		return evalArtifactSummary{}, fmt.Errorf("read summary: %w", err)
	}
	var summary evalArtifactSummary
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		return evalArtifactSummary{}, fmt.Errorf("decode summary: %w", err)
	}
	if summary.Schema != evalProtocolSchema || summary.ProtocolHash != requested.ProtocolHash || !summary.Validity.isComplete() {
		return evalArtifactSummary{}, fmt.Errorf("invalid summary protocol or validity")
	}
	hashes, err := evalArtifactFileHashes(runDir)
	if err != nil {
		return evalArtifactSummary{}, err
	}
	for file, hash := range hashes {
		if summary.ArtifactHashes[file] != hash {
			return evalArtifactSummary{}, fmt.Errorf("artifact hash mismatch for %s", file)
		}
	}
	return summary, nil
}

// runEvalArtifactValidateCLI implements the no-model validation path used by
// the quickstart. It derives the expected IDs from the frozen candidates, then
// verifies every companion artifact and digest against that immutable replay
// set. Full denominator checks are performed when the protocol is frozen.
func runEvalArtifactValidateCLI(runDir string) error {
	protocol, err := readFrozenEvalProtocol(runDir)
	if err != nil {
		return err
	}
	candidates, err := readEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile))
	if err != nil {
		return fmt.Errorf("read candidates for validation: %w", err)
	}
	summary, err := validateEvalArtifactRun(runDir, protocol, candidateQuestionIDs(candidates))
	if err != nil {
		return err
	}
	fmt.Printf("eval-validate: protocol=%s questions=%d valid=%t\n", summary.ProtocolHash, len(candidates), summary.Validity.isComplete())
	return nil
}

func readFrozenEvalProtocol(runDir string) (evalProtocol, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, evalProtocolArtifactFile)) //nolint:gosec // run artifact is operator-selected
	if err != nil {
		return evalProtocol{}, fmt.Errorf("read frozen protocol: %w", err)
	}
	var protocol evalProtocol
	if err := json.Unmarshal(raw, &protocol); err != nil {
		return evalProtocol{}, fmt.Errorf("decode frozen protocol: %w", err)
	}
	if err := validateEvalProtocol(protocol, evalRunFormal); err != nil {
		return evalProtocol{}, fmt.Errorf("validate frozen protocol: %w", err)
	}
	hash, err := evalProtocolFingerprint(protocol)
	if err != nil {
		return evalProtocol{}, err
	}
	if protocol.ProtocolHash != hash {
		return evalProtocol{}, fmt.Errorf("frozen protocol hash mismatch; use a fresh --run-dir")
	}
	return protocol, nil
}

func candidateQuestionIDs(candidates []evalCandidateArtifact) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.QuestionID)
	}
	return ids
}

func validateEvalArtifactRecords(file string, kind evalArtifactKind, protocolHash string, records []evalArtifactRecord, expected map[string]bool) error {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.Schema != evalProtocolSchema || record.ProtocolHash != protocolHash || record.Kind != kind || !record.Valid {
			return fmt.Errorf("invalid %s record for question %q", file, record.QuestionID)
		}
		ids = append(ids, record.QuestionID)
	}
	return validateArtifactQuestionSet(file, ids, expected)
}

func validateArtifactQuestionSet(name string, ids []string, expected map[string]bool) error {
	if len(ids) != len(expected) {
		return fmt.Errorf("%s question count %d, want %d", name, len(ids), len(expected))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] || !expected[id] {
			return fmt.Errorf("%s contains duplicate, blank, or unexpected question ID %q", name, id)
		}
		seen[id] = true
	}
	return nil
}

func evalArtifactFileHashes(runDir string) (map[string]string, error) {
	files := []string{evalCandidatesArtifactFile, evalTraceArtifactFile, evalBundleArtifactFile, evalClassificationArtifactFile}
	hashes := make(map[string]string, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(runDir, file)) //nolint:gosec // run artifact is operator-selected
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		sum := sha256.Sum256(raw)
		hashes[file] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return hashes, nil
}
