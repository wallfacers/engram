package main

// 042 artifact machinery: canonical digests, immutable manifest, append-only
// call-journal validation, conservative token charges, and manifest+seal
// validation. Artifacts never persist credentials, raw endpoints, raw provider
// responses/errors, or decoded reasoning text; endpoint/base-URL values are only
// stored as irreversible sha256 digests.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Artifact file layout (relative to a stage run-dir).
const (
	utilityManifestFile             = "manifest.json"
	utilitySealFile                 = "seal.json"
	utilityPreflightFile            = "preflight.json"
	utilityCallJournalFile          = "call-journal.jsonl"
	utilityPublicAnswerAttemptsFile = "public/answer-attempts.jsonl"
	utilityPublicFoldRulesFile      = "public/fold-rules.json"
	utilityPublicCrossfitFile       = "public/crossfit-decisions.jsonl"
	utilityPublicDecisionsFile      = "public/utility-decisions.jsonl"
	utilityPublicGlobalRuleFile     = "public/global-transfer-rule.json"
	utilityHiddenJudgeFile          = "hidden/judge-outcomes.jsonl"
	utilityHiddenLabelsFile         = "hidden/utility-labels.jsonl"
	utilityHiddenScoreFile          = "hidden/diagnostic-score.json"
	utilityLabelReportFile          = "label-report.json"
	utilityCollectReportFile        = "collect-report.json"
	utilityPilotReportFile          = "pilot-report.json"
	utilityDiagnosticReportFile     = "diagnostic-report.json"
	utilityEvaluationReportFile     = "evaluation-report.json"
)

// utilityHiddenFileSet lists files that only the score/training-label phase may
// read. The public/decision loaders must refuse them.
var utilityHiddenFileSet = map[string]bool{
	utilityHiddenJudgeFile:  true,
	utilityHiddenLabelsFile: true,
	utilityHiddenScoreFile:  true,
}

// utilityEndpointDigest returns an irreversible sha256 digest of a trimmed
// endpoint/base-URL value. The raw value is never persisted.
func utilityEndpointDigest(baseURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(baseURL)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// utilityCanonicalDigest produces a deterministic digest of v. encoding/json
// already sorts map keys and rejects NaN/±Inf floats, so a compact marshal plus
// sha256 is the canonical form used for every artifact digest and seal.
func utilityCanonicalDigest(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("canonical digest encode: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// utilityManifestDigest is the canonical digest of a frozen run manifest.
func utilityManifestDigest(m *utilityRunManifest) (string, error) {
	if err := m.validate(); err != nil {
		return "", err
	}
	return utilityCanonicalDigest(m)
}

// utilitySealWrite atomically writes a seal after fsyncing records/report. The
// seal is the last artifact written in every terminal stage.
func utilitySealWrite(dir string, seal utilityStageSeal) error {
	return writeJSON(filepath.Join(dir, utilitySealFile), seal)
}

// utilityManifestWrite writes the manifest first (before any model call or label
// loading). Overwriting an existing manifest is forbidden: resume must byte-match.
func utilityManifestWrite(dir string, m utilityRunManifest) error {
	path := filepath.Join(dir, utilityManifestFile)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("manifest already exists at %s: resume requires byte-identical digest", path)
	}
	return writeJSON(path, m)
}

// utilityManifestRead reads and validates a manifest, then recomputes its digest
// so callers can compare against a seal.
func utilityManifestRead(dir string) (utilityRunManifest, string, error) {
	var m utilityRunManifest
	if err := readJSON(filepath.Join(dir, utilityManifestFile), &m); err != nil {
		return m, "", err
	}
	d, err := utilityManifestDigest(&m)
	if err != nil {
		return m, "", err
	}
	return m, d, nil
}

// utilityValidateManifestSeal validates that a sealed run-dir has an immutable
// manifest whose digest matches the seal, for the given stage. Only a COMPLETE
// seal is a valid downstream source.
func utilityValidateManifestSeal(dir string, stage utilityStage) error {
	m, md, err := utilityManifestRead(dir)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if m.Stage != stage {
		return fmt.Errorf("manifest stage %s != expected %s", m.Stage, stage)
	}
	var seal utilityStageSeal
	if err := readJSON(filepath.Join(dir, utilitySealFile), &seal); err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if seal.Schema != utilitySchemaVersion {
		return fmt.Errorf("seal schema %q", seal.Schema)
	}
	if seal.Stage != stage {
		return fmt.Errorf("seal stage %s != expected %s", seal.Stage, stage)
	}
	if seal.ManifestDigest != md {
		return fmt.Errorf("seal manifest digest %s does not match manifest %s", seal.ManifestDigest, md)
	}
	if seal.Status != utilitySealComplete {
		return fmt.Errorf("seal status %s is not COMPLETE", seal.Status)
	}
	return nil
}

// utilityLoadPublicRecords reads a JSONL of answer attempts, refusing any hidden
// (score/label) file path so the label-blind phase can never ingest them.
func utilityLoadPublicRecords(path string) ([]utilityAnswerAttempt, error) {
	base := filepath.Base(path)
	if utilityHiddenFileSet[base] {
		return nil, fmt.Errorf("refusing to load hidden custody file %q from the public phase", path)
	}
	var out []utilityAnswerAttempt
	if err := readEvalJSONL(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// utilityWriteJSONL appends one JSONL line atomically per record (crash-safe).
func utilityWriteJSONL(path string, records []any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("encode to %s: %w", path, err)
		}
	}
	return f.Sync()
}

// utilityReadJSONL reads every record from a strict JSONL file.
func utilityReadJSONL[T any](path string) ([]T, error) {
	var out []T
	if err := readEvalJSONL(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// utilityUnitTokenCharge returns the ratio-token charge of one call unit:
// reported usage when valid, the conservative max-model-len bound when a failed
// answer attempt has no usage, and 0 for judge / query-embedding (which never
// contribute to the generation-token ratio).
func utilityUnitTokenCharge(rec utilityCallUnitRecord, maxModelLen int) int {
	if rec.Arm.judgeArm() {
		return 0
	}
	switch rec.UsageStatus {
	case utilityUsageReported:
		return rec.InputTokens + rec.OutputTokens
	case utilityUsageConservativeBound:
		return maxModelLen
	default:
		return 0
	}
}

// utilityValidateCallUnitJournal validates the append-only retry state machine
// across all logical calls. Rules (data-model.md):
//   - attempts strictly 1..3 and contiguous for a logical call;
//   - one STARTED and at most one terminal per attempt;
//   - COMPLETED or non-retryable FAILED ends the logical call;
//   - retryable FAILED may continue only when attempt < 3 (exhaustion invalid);
//   - an orphan STARTED (no terminal) invalidates the whole run;
//   - a terminal FAILED must carry a closed retryable/non-retryable reason.
func utilityValidateCallUnitJournal(records []utilityCallUnitRecord) error {
	type attemptState struct {
		started  bool
		terminal bool
	}
	byCall := map[string]map[int]*attemptState{}
	var order []string
	for i := range records {
		rec := &records[i]
		if rec.LogicalCallID == "" {
			return fmt.Errorf("call-unit record %d has no logical_call_id", i)
		}
		if !rec.Arm.valid() {
			return fmt.Errorf("call-unit %s has invalid arm %q", rec.LogicalCallID, rec.Arm)
		}
		if rec.Attempt < 1 || rec.Attempt > utilityMaxAttempts {
			return fmt.Errorf("call-unit %s attempt %d outside [1,%d]", rec.LogicalCallID, rec.Attempt, utilityMaxAttempts)
		}
		if !rec.State.valid() {
			return fmt.Errorf("call-unit %s invalid state %q", rec.LogicalCallID, rec.State)
		}
		if _, ok := byCall[rec.LogicalCallID]; !ok {
			byCall[rec.LogicalCallID] = map[int]*attemptState{}
			order = append(order, rec.LogicalCallID)
		}
		as := byCall[rec.LogicalCallID][rec.Attempt]
		if as == nil {
			as = &attemptState{}
			byCall[rec.LogicalCallID][rec.Attempt] = as
		}
		switch rec.State {
		case utilityCallUnitStarted:
			if as.started {
				return fmt.Errorf("call-unit %s attempt %d has duplicate STARTED", rec.LogicalCallID, rec.Attempt)
			}
			as.started = true
		case utilityCallUnitCompleted:
			if as.terminal {
				return fmt.Errorf("call-unit %s attempt %d has duplicate terminal", rec.LogicalCallID, rec.Attempt)
			}
			as.terminal = true
		case utilityCallUnitFailed:
			if as.terminal {
				return fmt.Errorf("call-unit %s attempt %d has duplicate terminal", rec.LogicalCallID, rec.Attempt)
			}
			if rec.FailureReason == "" {
				return fmt.Errorf("call-unit %s attempt %d FAILED without a failure reason", rec.LogicalCallID, rec.Attempt)
			}
			as.terminal = true
		}
	}

	for _, id := range order {
		units := byCall[id]
		attempts := make([]int, 0, len(units))
		for a := range units {
			attempts = append(attempts, a)
		}
		sort.Ints(attempts)
		completed := false
		for i, a := range attempts {
			as := units[a]
			if !as.started {
				return fmt.Errorf("call-unit %s attempt %d has a terminal without STARTED", id, a)
			}
			if !as.terminal {
				return fmt.Errorf("call-unit %s attempt %d is an orphan STARTED", id, a)
			}
			if i > 0 && a != attempts[i-1]+1 {
				return fmt.Errorf("call-unit %s attempt gap between %d and %d", id, attempts[i-1], a)
			}
			if completed {
				return fmt.Errorf("call-unit %s has attempts after a COMPLETED terminal", id)
			}
			// Determine the terminal kind from records of this attempt.
			terminalFailedRetryable := false
			terminalFailedNonRetryable := false
			terminalCompleted := false
			for j := range records {
				r := &records[j]
				if r.LogicalCallID != id || r.Attempt != a || r.State == utilityCallUnitStarted {
					continue
				}
				switch r.State {
				case utilityCallUnitCompleted:
					terminalCompleted = true
				case utilityCallUnitFailed:
					if r.Retryable {
						terminalFailedRetryable = true
					} else {
						terminalFailedNonRetryable = true
					}
				}
			}
			switch {
			case terminalCompleted:
				completed = true
			case terminalFailedNonRetryable:
				completed = true // no further attempt allowed
			case terminalFailedRetryable:
				if a == utilityMaxAttempts {
					return fmt.Errorf("call-unit %s exhausted retryable FAILED at attempt %d", id, a)
				}
				// next attempt must exist; validated by contiguity on next loop
			}
		}
		if !completed {
			return fmt.Errorf("call-unit %s never reached a non-retryable terminal", id)
		}
	}
	return nil
}

// utilityArmTokenCharge aggregates token charges for a set of units of one arm.
func utilityArmTokenCharge(records []utilityCallUnitRecord, maxModelLen int) (reported, conservative int) {
	for i := range records {
		rec := &records[i]
		if rec.UsageStatus == utilityUsageReported {
			reported += rec.InputTokens + rec.OutputTokens
		} else if rec.UsageStatus == utilityUsageConservativeBound {
			conservative += maxModelLen
		}
	}
	return reported, conservative
}
