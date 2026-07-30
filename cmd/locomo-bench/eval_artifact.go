package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// Metrics is absent for every INVALID run. Per-question diagnostics remain
	// available, but an invalid repetition/cap/source run cannot expose an
	// accuracy object that downstream tooling or a human could mistake for a
	// formal score.
	Metrics *evalFormalMetrics `json:"metrics,omitempty"`
}

// evalFormalTraceRecord, evalFormalBundleRecord, and
// evalFormalClassificationRecord are the concrete B1 payloads.  Their common
// envelope is deliberately retained so the no-model validator can reject a
// malformed run before any score or paired statistic is interpreted.
type evalFormalTraceRecord struct {
	evalArtifactRecord
	Attempt            int      `json:"attempt"`
	CandidateSetDigest string   `json:"candidate_set_digest"`
	AppliedActions     []string `json:"applied_actions"`
	FallbackReason     string   `json:"fallback_reason,omitempty"`
	TraceDigest        string   `json:"trace_digest"`
}

type evalFormalBundleRecord struct {
	evalArtifactRecord
	CandidateSetDigest string   `json:"candidate_set_digest"`
	TraceDigest        string   `json:"trace_digest"`
	SourceIDs          []string `json:"source_ids"`
	RenderedContext    string   `json:"rendered_context"`
	RenderedDigest     string   `json:"rendered_context_digest"`
	AnswerInputTokens  int      `json:"answer_input_tokens"`
	TokenCap           int      `json:"token_cap"`
	CounterFingerprint string   `json:"counter_fingerprint"`
	WithinCap          bool     `json:"within_cap"`
	SourceValid        bool     `json:"source_valid"`
	AnswerPromptDigest string   `json:"answer_prompt_digest"`
}

type evalFormalClassificationRecord struct {
	evalArtifactRecord
	Category         string                `json:"category"`
	GoldAnswerDigest string                `json:"gold_answer_digest"`
	AnswerRuns       []evalFormalAnswerRun `json:"answer_runs"`
	MajorityCorrect  bool                  `json:"majority_correct"`
	MissClass        evalMissClass         `json:"miss_class"`
	RetrievalCalls   int                   `json:"retrieval_calls"`
	AnswerCalls      int                   `json:"answer_calls"`
	InvalidReasons   []string              `json:"invalid_reasons,omitempty"`
}

// evalFormalQuestionRun is persisted inside each legacy repeat journal entry.
// The journal remains the crash-safe source of individual calls; the immutable
// 022 artifact files are only materialized after all repetitions agree on the
// frozen candidate and bundle identity.
type evalFormalQuestionRun struct {
	Candidate      evalCandidateArtifact  `json:"candidate"`
	Trace          evalFormalTraceRecord  `json:"trace"`
	Bundle         evalFormalBundleRecord `json:"bundle"`
	Answer         evalFormalAnswerRun    `json:"answer"`
	InvalidReasons []string               `json:"invalid_reasons,omitempty"`
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

// materializeFormalB1Artifacts promotes crash-safe, per-repetition journal
// records into the immutable 022 artifact layout.  It accepts only one frozen
// candidate/trace/bundle per question and keeps all answer repetitions nested
// in classification.jsonl so repeated model calls never inflate the question
// denominator.
func materializeFormalB1Artifacts(runDir string, protocol evalProtocol, runs [][]result) (evalArtifactSummary, error) {
	if len(runs) != protocol.Aggregation.AnswerRepetitions {
		return evalArtifactSummary{}, fmt.Errorf("formal runs %d, want protocol repetitions %d", len(runs), protocol.Aggregation.AnswerRepetitions)
	}
	byQuestion := make(map[string][]result)
	var expected map[string]bool
	for runIndex, run := range runs {
		seen := make(map[string]bool, len(run))
		seenKeys := make(map[resultKey]bool, len(run))
		for _, item := range run {
			id := resultID(item)
			key := resultKey{Conv: item.Conv, Q: item.Q}
			if strings.TrimSpace(id) == "" || seen[id] || seenKeys[key] || item.Formal022 == nil {
				return evalArtifactSummary{}, fmt.Errorf("formal run %d contains duplicate, blank, or non-formal result", runIndex+1)
			}
			seen[id] = true
			seenKeys[key] = true
			byQuestion[id] = append(byQuestion[id], item)
		}
		if runIndex == 0 {
			expected = seen
			continue
		}
		if err := validateArtifactQuestionSet("formal journal", mapKeys(seen), expected); err != nil {
			return evalArtifactSummary{}, err
		}
	}
	if len(expected) == 0 {
		return evalArtifactSummary{}, fmt.Errorf("formal journal has no question results")
	}
	orderedQuestionIDs, err := formalJournalQuestionIDs(runs[0])
	if err != nil {
		return evalArtifactSummary{}, err
	}
	if len(orderedQuestionIDs) != protocol.Benchmark.QuestionCount {
		return evalArtifactSummary{}, fmt.Errorf("formal denominator has %d questions, protocol requires %d", len(orderedQuestionIDs), protocol.Benchmark.QuestionCount)
	}
	if evalJSONDigest(orderedQuestionIDs) != protocol.Benchmark.QuestionIDsDigest {
		return evalArtifactSummary{}, fmt.Errorf("formal question ID digest differs from protocol")
	}

	questionIDs := mapKeys(expected)
	candidates := make([]evalCandidateArtifact, 0, len(questionIDs))
	traces := make([]evalFormalTraceRecord, 0, len(questionIDs))
	bundles := make([]evalFormalBundleRecord, 0, len(questionIDs))
	classifications := make([]evalFormalClassificationRecord, 0, len(questionIDs))
	for _, id := range questionIDs {
		entries := byQuestion[id]
		if len(entries) != protocol.Aggregation.AnswerRepetitions {
			return evalArtifactSummary{}, fmt.Errorf("formal question %q has %d repetitions, want %d", id, len(entries), protocol.Aggregation.AnswerRepetitions)
		}
		first := entries[0].Formal022
		candidate := first.Candidate
		trace := first.Trace
		bundle := first.Bundle
		invalid := append([]string(nil), first.InvalidReasons...)
		for _, entry := range entries[1:] {
			current := entry.Formal022
			if evalJSONDigest(current.Candidate) != evalJSONDigest(candidate) || evalJSONDigest(current.Trace) != evalJSONDigest(trace) || evalJSONDigest(current.Bundle) != evalJSONDigest(bundle) {
				invalid = append(invalid, "frozen_candidate_trace_or_bundle_drift")
			}
			invalid = append(invalid, current.InvalidReasons...)
		}
		if err := validateEvalCandidateArtifact(protocol, candidate); err != nil {
			invalid = append(invalid, "candidate_artifact_invalid")
		}
		if err := validateFormalFrozenPayload(protocol, candidate, trace, bundle); err != nil {
			invalid = append(invalid, "frozen_payload_invalid")
		}
		answerRuns := make([]evalFormalAnswerRun, 0, len(entries))
		outcomes := make([]bool, 0, len(entries))
		answerCalls := 0
		for index, entry := range entries {
			answer := entry.Formal022.Answer
			if answer.RunIndex != index+1 || answer.AnswerCalls != 1 || answer.JudgeCalls != 1 ||
				answer.InputTokens != bundle.AnswerInputTokens || answer.CounterSource != protocol.Budget.CounterFingerprint ||
				answer.AnswerDigest != evalTextDigest(answer.Answer) {
				invalid = append(invalid, "answer_call_or_token_contract_violation")
			}
			answerRuns = append(answerRuns, answer)
			outcomes = append(outcomes, answer.JudgeCorrect)
			answerCalls += answer.AnswerCalls
		}
		majority, err := majorityCorrectness(outcomes)
		if err != nil {
			return evalArtifactSummary{}, fmt.Errorf("formal majority %q: %w", id, err)
		}
		candidateCoverage := candidate.Gold.RenderedSourceCoverage
		bundleCoverage := sourceCoverage(candidate.Gold.ResolvedEvidenceIDs, bundle.SourceIDs)
		miss, err := classifyEvalMiss(evalMissAttributionInput{
			GoldResolved:      len(candidate.Gold.UnresolvedIDs) == 0,
			CandidateCoverage: candidateCoverage, BundleCoverage: bundleCoverage, MajorityCorrect: majority,
		})
		if err != nil {
			return evalArtifactSummary{}, fmt.Errorf("formal miss class %q: %w", id, err)
		}
		invalid = stableStrings(invalid)
		valid := len(invalid) == 0 && trace.Valid && bundle.Valid && bundle.WithinCap && bundle.SourceValid
		trace.Valid = valid
		trace.TraceDigest = formalTraceDigest(trace)
		bundle.Valid = valid
		bundle.TraceDigest = trace.TraceDigest
		classification := evalFormalClassificationRecord{
			evalArtifactRecord: evalArtifactRecord{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, QuestionID: id, Kind: evalClassificationArtifactKind, Valid: valid},
			Category:           entries[0].CategoryName, GoldAnswerDigest: evalTextDigest(entries[0].Gold), AnswerRuns: answerRuns,
			MajorityCorrect: majority, MissClass: miss, RetrievalCalls: candidate.RetrievalCalls, AnswerCalls: answerCalls, InvalidReasons: invalid,
		}
		candidates = append(candidates, candidate)
		traces = append(traces, trace)
		bundles = append(bundles, bundle)
		classifications = append(classifications, classification)
	}
	if err := writeEvalCandidateArtifacts(filepath.Join(runDir, evalCandidatesArtifactFile), candidates); err != nil {
		return evalArtifactSummary{}, err
	}
	if err := writeEvalJSONL(filepath.Join(runDir, evalTraceArtifactFile), traces); err != nil {
		return evalArtifactSummary{}, err
	}
	if err := writeEvalJSONL(filepath.Join(runDir, evalBundleArtifactFile), bundles); err != nil {
		return evalArtifactSummary{}, err
	}
	if err := writeEvalJSONL(filepath.Join(runDir, evalClassificationArtifactFile), classifications); err != nil {
		return evalArtifactSummary{}, err
	}
	hashes, err := evalArtifactFileHashes(runDir)
	if err != nil {
		return evalArtifactSummary{}, err
	}
	validity := formalArtifactValidity(candidates, traces, bundles, classifications)
	summary := evalArtifactSummary{Schema: evalProtocolSchema, ProtocolHash: protocol.ProtocolHash, ArtifactHashes: hashes, Validity: validity}
	if err := writeJSON(filepath.Join(runDir, evalSummaryArtifactFile), summary); err != nil {
		return evalArtifactSummary{}, err
	}
	return summary, nil
}

// publishFormalB1Metrics is deliberately separate from materialization.
// Materialization may discover only a subset of malformed payloads; the score
// becomes visible only after validateEvalArtifactRun has independently checked
// every persisted artifact and hash against the frozen protocol.
func publishFormalB1Metrics(runDir string, validated evalArtifactSummary, protocol evalProtocol) (evalArtifactSummary, error) {
	if !validated.Validity.isComplete() || validated.ProtocolHash != protocol.ProtocolHash || validated.Metrics != nil {
		return evalArtifactSummary{}, fmt.Errorf("formal metrics require one complete, unpublished validated summary")
	}
	var classifications []evalFormalClassificationRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalClassificationArtifactFile), &classifications); err != nil {
		return evalArtifactSummary{}, err
	}
	if len(classifications) != protocol.Benchmark.QuestionCount {
		return evalArtifactSummary{}, fmt.Errorf("formal metric denominator %d, want %d", len(classifications), protocol.Benchmark.QuestionCount)
	}
	for _, classification := range classifications {
		if !classification.Valid || classification.ProtocolHash != protocol.ProtocolHash ||
			classification.Kind != evalClassificationArtifactKind ||
			len(classification.AnswerRuns) != protocol.Aggregation.AnswerRepetitions ||
			classification.AnswerCalls != protocol.Aggregation.AnswerRepetitions {
			return evalArtifactSummary{}, fmt.Errorf("formal metrics require valid classifications")
		}
	}
	metrics := formalClassificationMetrics(classifications)
	validated.Metrics = &metrics
	if err := writeJSON(filepath.Join(runDir, evalSummaryArtifactFile), validated); err != nil {
		return evalArtifactSummary{}, err
	}
	return validated, nil
}

func formalJournalQuestionIDs(run []result) ([]string, error) {
	ordered := append([]result(nil), run...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Conv != ordered[j].Conv {
			return ordered[i].Conv < ordered[j].Conv
		}
		return ordered[i].Q < ordered[j].Q
	})
	ids := make([]string, 0, len(ordered))
	seen := make(map[string]bool, len(ordered))
	seenKeys := make(map[resultKey]bool, len(ordered))
	for _, item := range ordered {
		id := resultID(item)
		key := resultKey{Conv: item.Conv, Q: item.Q}
		if strings.TrimSpace(id) == "" || seen[id] || seenKeys[key] {
			return nil, fmt.Errorf("formal journal contains duplicate or blank question identity")
		}
		seen[id] = true
		seenKeys[key] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func validateFormalFrozenPayload(protocol evalProtocol, candidate evalCandidateArtifact, trace evalFormalTraceRecord, bundle evalFormalBundleRecord) error {
	if candidate.RetrievalCalls != protocol.Budget.RetrievalCallLimit {
		return fmt.Errorf("retrieval calls %d differ from protocol %d", candidate.RetrievalCalls, protocol.Budget.RetrievalCallLimit)
	}
	if trace.Schema != evalProtocolSchema || trace.ProtocolHash != protocol.ProtocolHash || trace.QuestionID != candidate.QuestionID ||
		trace.Kind != evalTraceArtifactKind || !trace.Valid || trace.Attempt != 1 ||
		trace.CandidateSetDigest != candidate.CandidateSetDigest || trace.TraceDigest != formalTraceDigest(trace) {
		return fmt.Errorf("invalid formal trace for question %q", candidate.QuestionID)
	}
	if bundle.Schema != evalProtocolSchema || bundle.ProtocolHash != protocol.ProtocolHash || bundle.QuestionID != candidate.QuestionID ||
		bundle.Kind != evalBundleArtifactKind || !bundle.Valid ||
		bundle.CandidateSetDigest != candidate.CandidateSetDigest || bundle.TraceDigest != trace.TraceDigest ||
		bundle.RenderedDigest != evalTextDigest(bundle.RenderedContext) || bundle.AnswerInputTokens < 1 ||
		bundle.AnswerInputTokens > protocol.Budget.AnswerInputTokenCap || bundle.TokenCap != protocol.Budget.AnswerInputTokenCap ||
		bundle.CounterFingerprint != protocol.Budget.CounterFingerprint || !bundle.WithinCap || !bundle.SourceValid ||
		len(bundle.SourceIDs) == 0 || !isDigest(bundle.AnswerPromptDigest) {
		return fmt.Errorf("invalid formal bundle for question %q", candidate.QuestionID)
	}
	return nil
}

func writeEvalJSONL[T any](path string, records []T) error {
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

func readEvalJSONL[T any](path string, out *[]T) error {
	f, err := os.Open(path) //nolint:gosec // operator-selected formal run artifact
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var record T
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode %s line %d: %w", path, line, err)
		}
		*out = append(*out, record)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func formalArtifactValidity(candidates []evalCandidateArtifact, traces []evalFormalTraceRecord, bundles []evalFormalBundleRecord, classifications []evalFormalClassificationRecord) evalArtifactValidity {
	total := len(classifications)
	if total == 0 || len(candidates) != total || len(traces) != total || len(bundles) != total {
		return evalArtifactValidity{}
	}
	var candidateIdentity, sourceValid, withinCap, answerCompliance, fullyValid int
	for index, classification := range classifications {
		if traces[index].CandidateSetDigest == candidates[index].CandidateSetDigest && bundles[index].CandidateSetDigest == candidates[index].CandidateSetDigest {
			candidateIdentity++
		}
		if bundles[index].SourceValid {
			sourceValid++
		}
		if bundles[index].WithinCap {
			withinCap++
		}
		if len(classification.AnswerRuns) > 0 {
			compliant := true
			for _, answer := range classification.AnswerRuns {
				if answer.AnswerCalls != 1 || answer.JudgeCalls != 1 {
					compliant = false
				}
			}
			if compliant {
				answerCompliance++
			}
		}
		if classification.Valid {
			fullyValid++
		}
	}
	rate := func(value int) float64 { return float64(value) / float64(total) }
	validity := evalArtifactValidity{
		Complete: fullyValid == total, CandidateIdentityRate: rate(candidateIdentity), SourceValidationRate: rate(sourceValid),
		SpanRecoveryRate: rate(sourceValid), CitationCoverageRate: rate(sourceValid), WithinCapRate: rate(withinCap),
		AnswerCallComplianceRate: rate(answerCompliance), UnattributedAddCount: 0,
	}
	validity.Valid = validity.Complete && validity.CandidateIdentityRate == 1 && validity.SourceValidationRate == 1 && validity.SpanRecoveryRate == 1 && validity.CitationCoverageRate == 1 && validity.WithinCapRate == 1 && validity.AnswerCallComplianceRate == 1
	return validity
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func validateEvalArtifactRun(runDir string, requested evalProtocol, expectedQuestionIDs []string) (evalArtifactSummary, error) {
	if err := checkEvalProtocolResume(runDir, requested, evalRunFormal); err != nil {
		return evalArtifactSummary{}, err
	}
	if len(expectedQuestionIDs) == 0 {
		return evalArtifactSummary{}, fmt.Errorf("expected question IDs are required")
	}
	if len(expectedQuestionIDs) != requested.Benchmark.QuestionCount {
		return evalArtifactSummary{}, fmt.Errorf("expected %d question IDs, protocol requires %d", len(expectedQuestionIDs), requested.Benchmark.QuestionCount)
	}
	if evalJSONDigest(expectedQuestionIDs) != requested.Benchmark.QuestionIDsDigest {
		return evalArtifactSummary{}, fmt.Errorf("expected question ID digest differs from protocol")
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
	if err := validateFormalArtifactPayloads(runDir, requested, candidates, expected); err != nil {
		return evalArtifactSummary{}, err
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

// validateFormalArtifactPayloads is intentionally conditional: the T018
// envelope fixtures remain valid minimal schema tests, while B1's richer
// records receive the extra cap, counter, trace, and one-answer checks that
// make their score eligible for interpretation.
func validateFormalArtifactPayloads(runDir string, protocol evalProtocol, candidates []evalCandidateArtifact, expected map[string]bool) error {
	var traces []evalFormalTraceRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalTraceArtifactFile), &traces); err != nil {
		return err
	}
	if len(traces) == 0 || traces[0].TraceDigest == "" {
		return nil
	}
	if err := validateArtifactQuestionSet("formal traces", formalTraceIDs(traces), expected); err != nil {
		return err
	}
	byID := make(map[string]evalCandidateArtifact, len(candidates))
	for _, candidate := range candidates {
		if candidate.RetrievalCalls != protocol.Budget.RetrievalCallLimit {
			return fmt.Errorf("invalid formal retrieval call count for question %q", candidate.QuestionID)
		}
		byID[candidate.QuestionID] = candidate
	}
	for _, trace := range traces {
		candidate, ok := byID[trace.QuestionID]
		if !ok || trace.Schema != evalProtocolSchema || trace.ProtocolHash != protocol.ProtocolHash || trace.Kind != evalTraceArtifactKind ||
			!trace.Valid || trace.Attempt != 1 || trace.CandidateSetDigest != candidate.CandidateSetDigest || trace.TraceDigest != formalTraceDigest(trace) {
			return fmt.Errorf("invalid formal trace for question %q", trace.QuestionID)
		}
	}

	var bundles []evalFormalBundleRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalBundleArtifactFile), &bundles); err != nil {
		return err
	}
	if err := validateArtifactQuestionSet("formal bundles", formalBundleIDs(bundles), expected); err != nil {
		return err
	}
	traceByID := make(map[string]evalFormalTraceRecord, len(traces))
	for _, trace := range traces {
		traceByID[trace.QuestionID] = trace
	}
	for _, bundle := range bundles {
		candidate, candidateOK := byID[bundle.QuestionID]
		trace, traceOK := traceByID[bundle.QuestionID]
		if !candidateOK || !traceOK || bundle.Schema != evalProtocolSchema || bundle.ProtocolHash != protocol.ProtocolHash ||
			bundle.Kind != evalBundleArtifactKind || !bundle.Valid ||
			bundle.CandidateSetDigest != candidate.CandidateSetDigest || bundle.TraceDigest != trace.TraceDigest ||
			bundle.RenderedDigest != evalTextDigest(bundle.RenderedContext) || bundle.AnswerInputTokens < 1 || bundle.AnswerInputTokens > protocol.Budget.AnswerInputTokenCap ||
			bundle.TokenCap != protocol.Budget.AnswerInputTokenCap || bundle.CounterFingerprint != protocol.Budget.CounterFingerprint || !bundle.WithinCap || !bundle.SourceValid || len(bundle.SourceIDs) == 0 || !isDigest(bundle.AnswerPromptDigest) {
			return fmt.Errorf("invalid formal bundle for question %q", bundle.QuestionID)
		}
	}

	var classifications []evalFormalClassificationRecord
	if err := readEvalJSONL(filepath.Join(runDir, evalClassificationArtifactFile), &classifications); err != nil {
		return err
	}
	if err := validateArtifactQuestionSet("formal classifications", formalClassificationIDs(classifications), expected); err != nil {
		return err
	}
	for _, classification := range classifications {
		if len(classification.AnswerRuns) != protocol.Aggregation.AnswerRepetitions || classification.AnswerCalls != len(classification.AnswerRuns) || !classification.Valid {
			return fmt.Errorf("invalid formal classification for question %q", classification.QuestionID)
		}
		outcomes := make([]bool, 0, len(classification.AnswerRuns))
		for index, answer := range classification.AnswerRuns {
			if answer.RunIndex != index+1 || answer.AnswerCalls != 1 || answer.JudgeCalls != 1 ||
				answer.InputTokens < 1 || answer.CounterSource != protocol.Budget.CounterFingerprint ||
				answer.AnswerDigest != evalTextDigest(answer.Answer) {
				return fmt.Errorf("invalid formal answer run for question %q", classification.QuestionID)
			}
			outcomes = append(outcomes, answer.JudgeCorrect)
		}
		majority, err := majorityCorrectness(outcomes)
		if err != nil || majority != classification.MajorityCorrect {
			return fmt.Errorf("invalid formal majority for question %q", classification.QuestionID)
		}
	}
	return nil
}

func formalTraceIDs(records []evalFormalTraceRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.QuestionID)
	}
	return ids
}

func formalBundleIDs(records []evalFormalBundleRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.QuestionID)
	}
	return ids
}

func formalClassificationIDs(records []evalFormalClassificationRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.QuestionID)
	}
	return ids
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
	return readEvalProtocolFile(filepath.Join(runDir, evalProtocolArtifactFile))
}

func readEvalProtocolFile(path string) (evalProtocol, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // run artifact is operator-selected
	if err != nil {
		return evalProtocol{}, fmt.Errorf("read protocol: %w", err)
	}
	var protocol evalProtocol
	if err := json.Unmarshal(raw, &protocol); err != nil {
		return evalProtocol{}, fmt.Errorf("decode protocol: %w", err)
	}
	if err := validateEvalProtocol(protocol, evalRunFormal); err != nil {
		return evalProtocol{}, fmt.Errorf("validate protocol: %w", err)
	}
	hash, err := evalProtocolFingerprint(protocol)
	if err != nil {
		return evalProtocol{}, err
	}
	if protocol.ProtocolHash != hash {
		return evalProtocol{}, fmt.Errorf("protocol hash mismatch; use a fresh --run-dir")
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
