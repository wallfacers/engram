package main

// 043 confidence-gated deepening — artifact machinery.
//
// DeepenDecision jsonl, pilot report, and manifest+seal, mirroring the 042
// counterfactual_utility_artifact.go digest/write/read/validate pattern. The
// manifest must have EVERY field (incl. QuestionCount / threshold / featureName
// / contract_digest / dataset_digest) populated before its digest is computed
// and the seal written (CLAUDE.md "manifest freeze-before-digest"). Seal-
// requiring loaders only run once the seal exists.

import (
	"fmt"
	"os"
	"path/filepath"
)

// deepenSchemaVersion identifies the 043 artifact schema.
const deepenSchemaVersion = "confidence-deepen/v1"

// Deepen artifact file layout (relative to a run-dir).
const (
	deepenManifestFile       = "manifest.json"
	deepenSealFile           = "seal.json"
	deepenPilotReportFile    = "pilot-report.json"
	deepenDecisionsFile      = "public/deepen-decisions.jsonl"
	deepenAnswerAttemptsFile = "public/answer-attempts.jsonl"
)

// deepenSealStatus is a closed seal-status enum.
type deepenSealStatus string

const (
	deepenSealComplete deepenSealStatus = "COMPLETE"
	deepenSealInvalid  deepenSealStatus = "INVALID"
)

// deepenRunManifest is the frozen provenance root of a deepen pilot or
// mechanism run. Every field is populated before the digest is computed.
type deepenRunManifest struct {
	Schema         string `json:"schema"`
	RunID          string `json:"run_id"`
	Stage          string `json:"stage"` // pilot | mechanism
	CreatedAt      string `json:"created_at"`
	QuestionCount  int    `json:"question_count"`
	Arm            string `json:"arm"` // hybrid+unified+deepen (mechanism) / signal-pilot
	Threshold      float64 `json:"threshold"`
	FeatureName    string `json:"feature_name"`
	ContractDigest string `json:"contract_digest"`
	DatasetDigest  string `json:"dataset_digest"`
	DeepenK        int    `json:"deepen_k"`
	MaxGaps        int    `json:"max_gaps"`
	Fixture        bool   `json:"fixture,omitempty"`
}

func (m *deepenRunManifest) validate() error {
	if m == nil || m.Schema != deepenSchemaVersion {
		return fmt.Errorf("manifest missing %s schema", deepenSchemaVersion)
	}
	if m.QuestionCount < 0 {
		return fmt.Errorf("manifest question_count must be >= 0, got %d", m.QuestionCount)
	}
	if m.DeepenK <= 0 {
		return fmt.Errorf("manifest deepen_k must be positive, got %d", m.DeepenK)
	}
	if m.MaxGaps < 1 || m.MaxGaps > deepenMaxGapItems {
		return fmt.Errorf("manifest max_gaps=%d outside [1,%d]", m.MaxGaps, deepenMaxGapItems)
	}
	return nil
}

// deepenManifestDigest is the canonical digest of a frozen run manifest.
func deepenManifestDigest(m *deepenRunManifest) (string, error) {
	if err := m.validate(); err != nil {
		return "", err
	}
	return utilityCanonicalDigest(m)
}

// deepenStageSeal is the immutable seal for a deepen run-dir.
type deepenStageSeal struct {
	Schema         string           `json:"schema"`
	Stage          string           `json:"stage"`
	Status         deepenSealStatus `json:"status"`
	ManifestDigest string           `json:"manifest_digest"`
	ReportDigest   string           `json:"report_digest,omitempty"`
	Verdict        string           `json:"verdict,omitempty"`
}

// deepenManifestWrite writes the manifest first. Overwriting an existing
// manifest is forbidden: resume must byte-match.
func deepenManifestWrite(dir string, m deepenRunManifest) error {
	path := filepath.Join(dir, deepenManifestFile)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("deepen manifest already exists at %s: resume requires byte-identical digest", path)
	}
	return writeJSON(path, m)
}

// deepenManifestRead reads and validates a manifest, recomputing its digest.
func deepenManifestRead(dir string) (deepenRunManifest, string, error) {
	var m deepenRunManifest
	if err := readJSON(filepath.Join(dir, deepenManifestFile), &m); err != nil {
		return m, "", err
	}
	d, err := deepenManifestDigest(&m)
	if err != nil {
		return m, "", err
	}
	return m, d, nil
}

// deepenValidateManifestSeal validates that a sealed deepen run-dir has an
// immutable manifest whose digest matches the seal, for the given stage. Only
// a COMPLETE seal is a valid downstream source.
func deepenValidateManifestSeal(dir, stage string) error {
	m, md, err := deepenManifestRead(dir)
	if err != nil {
		return fmt.Errorf("deepen manifest: %w", err)
	}
	if m.Stage != stage {
		return fmt.Errorf("deepen manifest stage %s != expected %s", m.Stage, stage)
	}
	var seal deepenStageSeal
	if err := readJSON(filepath.Join(dir, deepenSealFile), &seal); err != nil {
		return fmt.Errorf("deepen seal: %w", err)
	}
	if seal.Schema != deepenSchemaVersion {
		return fmt.Errorf("deepen seal schema %q", seal.Schema)
	}
	if seal.Stage != stage {
		return fmt.Errorf("deepen seal stage %s != expected %s", seal.Stage, stage)
	}
	if seal.ManifestDigest != md {
		return fmt.Errorf("deepen seal manifest digest %s does not match manifest %s", seal.ManifestDigest, md)
	}
	if seal.Status != deepenSealComplete {
		return fmt.Errorf("deepen seal status %s is not COMPLETE", seal.Status)
	}
	return nil
}

// deepenSealWrite atomically writes a seal. The seal is the last artifact
// written in a terminal deepen stage.
func deepenSealWrite(dir string, seal deepenStageSeal) error {
	return writeJSON(filepath.Join(dir, deepenSealFile), seal)
}

// --- DeepenDecision jsonl ---

// deepenWriteDecisions appends one DeepenDecision per line (crash-safe).
func deepenWriteDecisions(dir string, decisions []deepenDecision) error {
	return utilityWriteJSONL(filepath.Join(dir, deepenDecisionsFile), decisions)
}

// deepenLoadDecisions reads the public decision records, refusing hidden paths.
func deepenLoadDecisions(dir string) ([]deepenDecision, error) {
	return readEvalJSONLWithValidate(filepath.Join(dir, deepenDecisionsFile), func(d deepenDecision) error {
		return d.validate()
	})
}

// readEvalJSONLWithValidate reads strict JSONL and validates every record.
func readEvalJSONLWithValidate[T any](path string, validate func(T) error) ([]T, error) {
	var out []T
	if err := readEvalJSONL(path, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if err := validate(out[i]); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
	}
	return out, nil
}

// --- Pilot report ---

// deepenSignalReport is one signal's AUC result in the pilot report.
type deepenSignalReport struct {
	Kind           string    `json:"kind"`
	Feature        string    `json:"feature"`
	AUC            float64   `json:"auc"`
	AUCCI95        [2]float64 `json:"auc_ci95"`
	ParseCoverage  float64   `json:"parse_coverage"`
}

// deepenChannelParity is the streaming-vs-logprob answer flip audit.
type deepenChannelParity struct {
	N        int     `json:"n"`
	Flips    int     `json:"flips"`
	FlipRate float64 `json:"flip_rate"`
}

// deepenChosenSignal is the pilot's selected signal + threshold.
type deepenChosenSignal struct {
	Kind      string  `json:"kind"`
	Feature   string  `json:"feature"`
	Threshold float64 `json:"threshold"`
}

// deepenPilotGate is the GO/NO-GO gate verdict.
type deepenPilotGate struct {
	Rule    string `json:"rule"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// deepenPilotReport is pilot-report.json (contracts/artifacts.md).
type deepenPilotReport struct {
	Stage         string               `json:"stage"`
	Conversations []string             `json:"conversations"`
	Signals       []deepenSignalReport `json:"signals"`
	ChannelParity deepenChannelParity  `json:"channel_parity"`
	Chosen        deepenChosenSignal   `json:"chosen"`
	Gate          deepenPilotGate      `json:"gate"`
}

// deepenPilotReportWrite writes the pilot report and returns its digest.
func deepenPilotReportWrite(dir string, report deepenPilotReport) (string, error) {
	if err := writeJSON(filepath.Join(dir, deepenPilotReportFile), report); err != nil {
		return "", err
	}
	return utilityCanonicalDigest(report)
}

// deepenPilotGateVerdict applies the frozen gate rule: AUC >= 0.65 AND the
// channel flip_rate stays within the noise band. The channel-parity noise band
// is defined as flip_rate <= 0.10 (10%) for the 2-conv pilot; a higher flip
// rate means the logprob channel's answers diverge from the streaming control
// and the two channels are not comparable (plan decision 2).
const (
	deepenPilotAUCGate   = 0.65
	deepenFlipRateNoise  = 0.10
)

func deepenPilotGateVerdict(bestAUC float64, parity deepenChannelParity) (string, string) {
	reason := ""
	switch {
	case bestAUC < deepenPilotAUCGate:
		reason = fmt.Sprintf("best signal AUC %.3f below gate %.2f", bestAUC, deepenPilotAUCGate)
		return "NO-GO", reason
	case parity.N > 0 && parity.FlipRate > deepenFlipRateNoise:
		reason = fmt.Sprintf("channel flip_rate %.3f above noise band %.2f", parity.FlipRate, deepenFlipRateNoise)
		return "NO-GO", reason
	case parity.N == 0:
		reason = "no channel-parity pairs measured; GO requires at least one comparison"
		return "NO-GO", reason
	default:
		reason = fmt.Sprintf("AUC %.3f >= %.2f and flip_rate %.3f within noise band", bestAUC, deepenPilotAUCGate, parity.FlipRate)
		return "GO", reason
	}
}
