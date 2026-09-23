package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/filter/jev"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
)

// This file owns the 038-protocol integration surface of the 051 four-arm
// evaluation (task T15): the manifest registration that pins the filter model and
// the pre-registered threshold policy, the filter call-class audit, the
// retrieval-budget exclusion proof, the validity-artifact gate, the run-internal
// paired significance tests, and the four-arm orchestration itself.

const (
	// jevFilterMechanismKey registers the read-side relevance filter as an
	// additive mechanism in the 022 protocol manifest (research R7b). It carries
	// the mechanism family and version: a future local approximation (Open-Jev)
	// registers as its own key instead of silently reusing this one.
	jevFilterMechanismKey = "filter.jev.v1"
	// jevFilterCallClass is the per-row provider-call audit class for Jev filter
	// calls. The class exists so filter traffic is auditable without being
	// mistaken for a retrieval call.
	jevFilterCallClass = "filter"
	// jevFilterCallJournalFile is the crash-auditable filter-call journal.
	jevFilterCallJournalFile = "jev_filter_calls.jsonl"
	// jevArmRowsFile is the per-question, per-arm, per-repetition measurement file.
	jevArmRowsFile = "jev_arms.jsonl"
	// jevEmptyInjectionFloor is SC-004's pre-registered absolute floor. It is
	// written into the manifest before the gated run; lowering it afterwards is
	// forbidden (spec SC-004), which is why the value is a constant here rather
	// than a flag.
	jevEmptyInjectionFloor = 0.50
	// jevFilterLatencyExpectationMS and jevFilterCostExpectationUSD are SC-007's
	// order-of-magnitude expectations for one filter call (~0.3s, ~$0.0004).
	jevFilterLatencyExpectationMS = 300
	jevFilterCostExpectationUSD   = 0.0004
	// jevFilterAnomalyFactor is SC-007's "one order of magnitude" anomaly factor.
	jevFilterAnomalyFactor = 10
	// jevFilterPoolUpperBound mirrors the memory_search contract's hard clamp:
	// the harness never pre-registers a pool the product would refuse.
	jevFilterPoolUpperBound = 500
	// jevEstimateFilterTokensPerCandidate is the nominal per-candidate token cost
	// of a pointer-mode request (name + text). It feeds --estimate only; a run
	// measures real response usage.
	jevEstimateFilterTokensPerCandidate = 120
	// The nominal per-call answer and judge estimates, matching the constants the
	// existing --estimate path already uses. They exist to be compared against the
	// frozen budget before spending, never to replace measured usage.
	jevEstimateAnswerInTokens  = 5_100
	jevEstimateAnswerOutTokens = 50
	jevEstimateJudgeInTokens   = 4_000
	jevEstimateJudgeOutTokens  = 100
)

// jevFloatingModelTags are the model-tag suffixes that must never appear as the
// pinned filter model. A floating tag would make a gated run unattributable, so
// the registration refuses it up front (research R7a: forbid a "latest" tag).
var jevFloatingModelTags = []string{"latest", "stable", "default", "dev", "main", "head", "edge"}

// jevFilterRegistration is the manifest-visible pre-registration of the 051
// filter: which model scores, which policy was frozen, and which protocol
// prerequisites the operator declared. It rides inside
// evalExperimentProtocol.Filter, a pointer field, so protocols that do not
// declare the mechanism keep byte-identical canonical bytes (and therefore
// unchanged protocol hashes).
type jevFilterRegistration struct {
	MechanismKey        string   `json:"mechanism_key"`
	FilterModel         string   `json:"filter_model"`
	FilterBaseURLHost   string   `json:"filter_base_url_host,omitempty"`
	Theta               float64  `json:"theta"`
	RelaxTheta          float64  `json:"relax_theta"`
	RelaxMax            int      `json:"relax_max"`
	KShowMax            int      `json:"k_show_max"`
	Pool                int      `json:"pool"`
	AnswerInputCap      int      `json:"answer_input_cap"`
	AnswerRepetitions   int      `json:"answer_repetitions"`
	EmptyInjectionFloor float64  `json:"empty_injection_floor"`
	Arms                []string `json:"arms"`
	// The three declared protocol prerequisites. The 038 machine records them as
	// receipts elsewhere; the registration states that the operator confirmed
	// them for this run, and validateJevRunValidity refuses a gated run without
	// them instead of assuming them.
	PilotGateConfirmed bool `json:"pilot_gate_confirmed"`
	WarmupDisposed     bool `json:"warmup_disposed"`
	SameWindowReps     bool `json:"same_window_reps"`
}

// newJevFilterRegistration builds the registration from a run's frozen policy.
func newJevFilterRegistration(model, baseURLHost string, policy filter.Policy, pool int) jevFilterRegistration {
	pol := policy.WithDefaults()
	return jevFilterRegistration{
		MechanismKey:        jevFilterMechanismKey,
		FilterModel:         strings.TrimSpace(model),
		FilterBaseURLHost:   strings.TrimSpace(baseURLHost),
		Theta:               pol.Theta,
		RelaxTheta:          pol.RelaxTheta,
		RelaxMax:            pol.RelaxMax,
		KShowMax:            pol.KShowMax,
		Pool:                pool,
		AnswerInputCap:      jevArmAnswerInputCap,
		AnswerRepetitions:   jevArmAnswerRepetitions,
		EmptyInjectionFloor: jevEmptyInjectionFloor,
		Arms:                armNamesInOrder(),
	}
}

// validateJevFilterRegistration refuses a registration that could not be
// attributed or reproduced: a floating model tag, a policy outside the contract,
// a cap that is not the frozen one, or a lowered SC-004 floor.
func validateJevFilterRegistration(reg jevFilterRegistration) error {
	if reg.MechanismKey != jevFilterMechanismKey {
		return fmt.Errorf("jev filter registration mechanism key = %q, want %q", reg.MechanismKey, jevFilterMechanismKey)
	}
	if err := validatePinnedFilterModel(reg.FilterModel); err != nil {
		return err
	}
	if !(reg.Theta > 0 && reg.Theta < 1) {
		return fmt.Errorf("jev filter theta %v must be inside (0,1)", reg.Theta)
	}
	if !(reg.RelaxTheta > 0 && reg.RelaxTheta <= reg.Theta) {
		return fmt.Errorf("jev filter relax_theta %v must be inside (0, theta=%v]", reg.RelaxTheta, reg.Theta)
	}
	if reg.RelaxMax < 0 {
		return fmt.Errorf("jev filter relax_max %d must be non-negative", reg.RelaxMax)
	}
	if reg.KShowMax != jevArmCShowGate {
		return fmt.Errorf("jev filter k_show_max %d must be the frozen gate budget %d (SC-002 measures the mean shown count against it)", reg.KShowMax, jevArmCShowGate)
	}
	if reg.Pool != jevArmPoolSize {
		return fmt.Errorf("jev filter pool %d must be the frozen evaluation pool %d", reg.Pool, jevArmPoolSize)
	}
	if reg.AnswerInputCap != jevArmAnswerInputCap {
		return fmt.Errorf("jev filter answer_input_cap %d must be the frozen cap %d", reg.AnswerInputCap, jevArmAnswerInputCap)
	}
	if reg.AnswerRepetitions != jevArmAnswerRepetitions {
		return fmt.Errorf("jev filter answer_repetitions %d must be the frozen majority protocol %d", reg.AnswerRepetitions, jevArmAnswerRepetitions)
	}
	if reg.EmptyInjectionFloor != jevEmptyInjectionFloor {
		return fmt.Errorf("jev filter empty_injection_floor %v must be the pre-registered %v (post-run edits are forbidden)", reg.EmptyInjectionFloor, jevEmptyInjectionFloor)
	}
	return validateJevArmSet(reg.Arms)
}

// validatePinnedFilterModel refuses an empty or floating filter model.
func validatePinnedFilterModel(model string) error {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return fmt.Errorf("jev filter_model must pin a concrete model revision")
	}
	lowered := strings.ToLower(trimmed)
	segments := strings.FieldsFunc(lowered, func(r rune) bool {
		return r == '-' || r == '_' || r == '/' || r == ':' || r == '@'
	})
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		for _, floating := range jevFloatingModelTags {
			if last == floating {
				return fmt.Errorf("jev filter_model %q is a floating tag; pin a concrete revision", trimmed)
			}
		}
	}
	return nil
}

// validateJevArmSet requires exactly the declared arm set. A subset would let a
// run report SC gates it never measured.
func validateJevArmSet(arms []string) error {
	want := armNamesInOrder()
	if len(arms) != len(want) {
		return fmt.Errorf("jev filter registration declares %d arms, want the frozen %d (%s)", len(arms), len(want), strings.Join(want, ","))
	}
	seen := make(map[string]bool, len(want))
	for _, arm := range arms {
		seen[arm] = true
	}
	for _, arm := range want {
		if !seen[arm] {
			return fmt.Errorf("jev filter registration is missing arm %q", arm)
		}
	}
	return nil
}

func armNamesInOrder() []string {
	names := make([]string, 0, len(jevArmNames()))
	for _, arm := range jevArmNames() {
		names = append(names, string(arm))
	}
	return names
}

// Digest is the canonical registration digest. It is what a report cites when it
// claims which policy was frozen.
func (reg jevFilterRegistration) Digest() (string, error) {
	canonical, err := json.Marshal(reg)
	if err != nil {
		return "", fmt.Errorf("canonical jev filter registration: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// attachJevFilterRegistration writes the registration into a protocol manifest
// and records the mechanism key. It is additive: the registration is only ever
// present when a run explicitly declares it, so unrelated protocols keep
// unchanged canonical bytes and hashes. It is the freeze-time half of the
// binding: freezeFormalProtocol calls it before the manifest is hashed, and the
// four-arm run then refuses a manifest whose registration differs from its own.
func attachJevFilterRegistration(protocol *evalProtocol, reg jevFilterRegistration) error {
	if protocol == nil {
		return fmt.Errorf("jev filter registration requires a protocol")
	}
	if err := validateJevFilterRegistration(reg); err != nil {
		return err
	}
	if protocol.Experiment.MechanismFlags == nil {
		protocol.Experiment.MechanismFlags = map[string]bool{}
	}
	protocol.Experiment.MechanismFlags[jevFilterMechanismKey] = true
	stored := reg
	protocol.Experiment.Filter = &stored
	return nil
}

// validateJevProtocolBinding checks that a manifest declaring the filter
// mechanism is internally consistent: the frozen cap and the majority protocol
// must be the ones the arms actually used.
func validateJevProtocolBinding(protocol evalProtocol) error {
	if protocol.Experiment.Filter == nil {
		if protocol.Experiment.MechanismFlags[jevFilterMechanismKey] {
			return fmt.Errorf("protocol declares mechanism %q without a filter registration", jevFilterMechanismKey)
		}
		return nil
	}
	if !protocol.Experiment.MechanismFlags[jevFilterMechanismKey] {
		return fmt.Errorf("protocol carries a filter registration without mechanism flag %q", jevFilterMechanismKey)
	}
	if err := validateJevFilterRegistration(*protocol.Experiment.Filter); err != nil {
		return err
	}
	if protocol.Budget.AnswerInputTokenCap != protocol.Experiment.Filter.AnswerInputCap {
		return fmt.Errorf("protocol answer-input cap %d differs from the frozen arm cap %d", protocol.Budget.AnswerInputTokenCap, protocol.Experiment.Filter.AnswerInputCap)
	}
	if protocol.Aggregation.AnswerRepetitions != protocol.Experiment.Filter.AnswerRepetitions {
		return fmt.Errorf("protocol answer repetitions %d differ from the frozen arm repetitions %d", protocol.Aggregation.AnswerRepetitions, protocol.Experiment.Filter.AnswerRepetitions)
	}
	return nil
}

// jevFilterCallRecord is one Jev filter call in the per-row provider-call audit.
// Filter calls are their own call class: they are not retrieval calls, so they
// are never charged against the protocol's RetrievalCallLimit, but they are
// counted and priced so SC-007 can attribute cost to the filter segment.
type jevFilterCallRecord struct {
	Schema       string   `json:"schema"`
	ProtocolHash string   `json:"protocol_hash"`
	MechanismKey string   `json:"mechanism_key"`
	Class        string   `json:"class"`
	Conv         int      `json:"conv"`
	Q            int      `json:"q"`
	Repetition   int      `json:"repetition"`
	Arm          string   `json:"arm"`
	Pool         int      `json:"pool"`
	Memories     int      `json:"memories"`
	Kept         int      `json:"kept"`
	Dropped      int      `json:"dropped"`
	Theta        float64  `json:"theta"`
	LatencyMs    int      `json:"latency_ms"`
	USD          float64  `json:"usd"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Degraded     bool     `json:"degraded"`
	Notes        []string `json:"notes,omitempty"`
}

// jevFilterCallJournal appends filter-call records durably. It is deliberately
// separate from formal_calls.jsonl: the formal journal's state machine governs
// answer/judge calls, and mixing a second call class into it would change the
// meaning of its orphan-STARTED invariant.
type jevFilterCallJournal struct {
	mu           sync.Mutex
	f            *os.File
	w            *bufio.Writer
	protocolHash string
	count        int
	err          error
	closed       bool
}

func openJevFilterCallJournal(runDir, protocolHash string) (*jevFilterCallJournal, error) {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(protocolHash) == "" {
		return nil, fmt.Errorf("jev filter call journal requires a run directory and a protocol hash")
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create jev filter call journal directory: %w", err)
	}
	path := filepath.Join(runDir, jevFilterCallJournalFile)
	if prior, err := os.Open(path); err == nil { //nolint:gosec // operator-selected run artifact
		scanner := bufio.NewScanner(prior)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var record jevFilterCallRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = prior.Close()
				return nil, fmt.Errorf("decode %s line %d: %w; use a fresh --run-dir", path, line, err)
			}
			if record.Class != jevFilterCallClass || record.ProtocolHash != protocolHash || record.MechanismKey != jevFilterMechanismKey {
				_ = prior.Close()
				return nil, fmt.Errorf("%s line %d does not belong to this protocol's %s call class; use a fresh --run-dir", path, line, jevFilterMechanismKey)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = prior.Close()
			return nil, fmt.Errorf("scan %s: %w", path, err)
		}
		if err := prior.Close(); err != nil {
			return nil, fmt.Errorf("close %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open jev filter call journal: %w", err)
	}
	return &jevFilterCallJournal{f: f, w: bufio.NewWriter(f), protocolHash: protocolHash}, nil
}

// Record appends one filter call. The class, mechanism key, and protocol hash are
// forced here so a caller cannot write an unattributable row.
func (j *jevFilterCallJournal) Record(record jevFilterCallRecord) error {
	if j == nil {
		return nil
	}
	record.Schema = evalProtocolSchema
	record.ProtocolHash = j.protocolHash
	record.MechanismKey = jevFilterMechanismKey
	record.Class = jevFilterCallClass
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return fmt.Errorf("write %s: journal is closed", jevFilterCallJournalFile)
	}
	if j.err != nil {
		return j.err
	}
	if _, err := j.w.Write(line); err != nil {
		j.err = err
		return err
	}
	if err := j.w.WriteByte('\n'); err != nil {
		j.err = err
		return err
	}
	if err := j.w.Flush(); err != nil {
		j.err = err
		return err
	}
	j.count++
	return nil
}

// Count reports how many filter calls were journaled.
func (j *jevFilterCallJournal) Count() int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.count
}

func (j *jevFilterCallJournal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	if err := j.w.Flush(); j.err == nil && err != nil {
		j.err = err
	}
	if err := j.f.Close(); j.err == nil && err != nil {
		j.err = err
	}
	if j.err != nil {
		return fmt.Errorf("write %s: %w", jevFilterCallJournalFile, j.err)
	}
	return nil
}

// verifyJevFilterRegistrationBinding refuses a run whose frozen manifest does not
// carry the registration the run is about to use. The registration is baked in at
// freeze time, before the protocol digest, so the run never mutates the manifest:
// a manifest frozen without --jev-arms must be re-frozen instead of silently
// gaining a registration that its digest does not cover (AGENTS.md
// freeze-before-digest).
func verifyJevFilterRegistrationBinding(protocol evalProtocol, registration jevFilterRegistration) error {
	if err := validateJevProtocolBinding(protocol); err != nil {
		return err
	}
	if protocol.Experiment.Filter == nil {
		return fmt.Errorf("the frozen protocol declares no %s registration; re-freeze it with --jev-arms so the registration and the answer-input cap are bound to one manifest", jevFilterMechanismKey)
	}
	frozen, err := protocol.Experiment.Filter.Digest()
	if err != nil {
		return err
	}
	current, err := registration.Digest()
	if err != nil {
		return err
	}
	if frozen != current {
		return fmt.Errorf("the run's filter registration %s differs from the frozen %s; freeze the manifest with this environment (ENGRAM_JEV_MODEL/ENGRAM_JEV_THETA/...) or run the frozen configuration", current, frozen)
	}
	return nil
}

// jevCallBudget separates retrieval calls from filter calls. The 051 filter is
// not a retrieval call and consumes no RetrievalCallLimit slot; the harness
// records both counts so an audit can confirm the exclusion held. For the
// four-arm protocol the recorded limit is the frozen per-question limit scaled by
// the run's own multiplicity (one retrieval per arm per measured
// question-repetition), which is what makes the check meaningful instead of
// vacuous.
type jevCallBudget struct {
	RetrievalCalls     int `json:"retrieval_calls"`
	FilterCalls        int `json:"filter_calls"`
	RetrievalCallLimit int `json:"retrieval_call_limit"`
}

// Validate fails only on a real retrieval-budget breach. Filter calls are
// excluded by construction, so they can never be part of the failure.
func (b jevCallBudget) Validate() error {
	if b.RetrievalCalls < 0 || b.FilterCalls < 0 {
		return fmt.Errorf("jev call budget counts must be non-negative (retrieval=%d filter=%d)", b.RetrievalCalls, b.FilterCalls)
	}
	if b.RetrievalCallLimit > 0 && b.RetrievalCalls > b.RetrievalCallLimit {
		return fmt.Errorf("retrieval calls %d exceed the protocol limit %d", b.RetrievalCalls, b.RetrievalCallLimit)
	}
	return nil
}

// jevCoreValidityArtifacts are the artifacts every gated four-arm run must leave
// behind. The verdict file is written last and validated by its own schema, so it
// is not part of its own precondition.
var jevCoreValidityArtifacts = []string{
	evalProtocolArtifactFile,
	evalCandidatesArtifactFile,
	evalTraceArtifactFile,
	evalBundleArtifactFile,
	evalClassificationArtifactFile,
	formalCallJournalFile,
	jevFilterCallJournalFile,
	jevArmRowsFile,
}

// jevValidityInput is the evidence a gated four-arm run must present before any
// SC verdict may be called a result.
type jevValidityInput struct {
	Validity             evalArtifactValidity
	Present              map[string]bool
	B0ContinuityDeclared bool
	Registration         jevFilterRegistration
}

// validateJevRunValidity is the fail-closed gate: incomplete per-repeat
// validation receipts, a missing artifact, or an undeclared protocol prerequisite
// all refuse the run instead of producing a promotable verdict.
func validateJevRunValidity(input jevValidityInput) error {
	if !input.Validity.isComplete() {
		return fmt.Errorf("per-repeat validation receipts are incomplete (valid=%t complete=%t); the four-arm verdict requires evalArtifactValidity.isComplete", input.Validity.Valid, input.Validity.Complete)
	}
	for _, artifact := range jevCoreValidityArtifacts {
		if !input.Present[artifact] {
			return fmt.Errorf("required validity artifact %s is missing", artifact)
		}
	}
	if input.B0ContinuityDeclared && !input.Present[evalB0ContinuitySummaryFile] {
		return fmt.Errorf("B0 continuity was declared but %s is missing", evalB0ContinuitySummaryFile)
	}
	return validateJevDeclaredPrerequisites(input.Registration)
}

// jevArmContrast is one run-internal paired contrast (control vs treatment) over
// the same question set. Pairing is internal to the run: the exact McNemar test
// and the paired interval only mean anything over the same questions (SC-001).
type jevArmContrast struct {
	Control           jevArm                 `json:"control"`
	Treatment         jevArm                 `json:"treatment"`
	Questions         int                    `json:"questions"`
	ControlAccuracy   float64                `json:"control_accuracy"`
	TreatmentAccuracy float64                `json:"treatment_accuracy"`
	DeltaPP           float64                `json:"delta_pp"`
	McNemarP          float64                `json:"mcnemar_p"`
	CI                evalConfidenceInterval `json:"paired_ci"`
}

// jevArmContrastFor pairs two arms question by question on their three-repetition
// majorities and runs the shared exact McNemar test plus the fixed paired
// interval. Mismatched question sets are an error: a contrast over different
// populations is not a paired contrast.
func jevArmContrastFor(rows []jevArmQuestionRow, control, treatment jevArm) (jevArmContrast, error) {
	majority, err := jevArmMajorityOutcomes(rows)
	if err != nil {
		return jevArmContrast{}, err
	}
	type key struct{ conv, q int }
	controlOutcomes := map[key]bool{}
	treatmentOutcomes := map[key]bool{}
	for outcomeKey, correct := range majority {
		switch outcomeKey.Arm {
		case control:
			controlOutcomes[key{conv: outcomeKey.Conv, q: outcomeKey.Q}] = correct
		case treatment:
			treatmentOutcomes[key{conv: outcomeKey.Conv, q: outcomeKey.Q}] = correct
		}
	}
	if len(controlOutcomes) == 0 || len(treatmentOutcomes) == 0 {
		return jevArmContrast{}, fmt.Errorf("paired contrast %s->%s requires measured outcomes on both arms", control, treatment)
	}
	if len(controlOutcomes) != len(treatmentOutcomes) {
		return jevArmContrast{}, fmt.Errorf("paired contrast %s->%s has %d vs %d measured questions; pairing requires the same set", control, treatment, len(controlOutcomes), len(treatmentOutcomes))
	}
	keys := make([]key, 0, len(controlOutcomes))
	for outcomeKey := range controlOutcomes {
		if _, ok := treatmentOutcomes[outcomeKey]; !ok {
			return jevArmContrast{}, fmt.Errorf("paired contrast %s->%s: question conv=%d q=%d is missing from the treatment arm", control, treatment, outcomeKey.conv, outcomeKey.q)
		}
		keys = append(keys, outcomeKey)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].conv != keys[right].conv {
			return keys[left].conv < keys[right].conv
		}
		return keys[left].q < keys[right].q
	})
	controlPaired := make([]bool, 0, len(keys))
	treatmentPaired := make([]bool, 0, len(keys))
	controlCorrect, treatmentCorrect := 0, 0
	controlRightTreatmentWrong, controlWrongTreatmentRight := 0, 0
	for _, outcomeKey := range keys {
		left := controlOutcomes[outcomeKey]
		right := treatmentOutcomes[outcomeKey]
		controlPaired = append(controlPaired, left)
		treatmentPaired = append(treatmentPaired, right)
		if left {
			controlCorrect++
		}
		if right {
			treatmentCorrect++
		}
		switch {
		case left && !right:
			controlRightTreatmentWrong++
		case !left && right:
			controlWrongTreatmentRight++
		}
	}
	p, err := exactMcNemarTwoSided(controlRightTreatmentWrong, controlWrongTreatmentRight)
	if err != nil {
		return jevArmContrast{}, err
	}
	interval, err := pairedDeltaConfidenceInterval(controlPaired, treatmentPaired)
	if err != nil {
		return jevArmContrast{}, err
	}
	questions := float64(len(keys))
	return jevArmContrast{
		Control:           control,
		Treatment:         treatment,
		Questions:         len(keys),
		ControlAccuracy:   float64(controlCorrect) / questions,
		TreatmentAccuracy: float64(treatmentCorrect) / questions,
		DeltaPP:           (float64(treatmentCorrect) - float64(controlCorrect)) / questions * 100,
		McNemarP:          p,
		CI:                interval,
	}, nil
}

// jevArmEmptyInjectionRate is the fraction of rows whose shown set is empty. It is
// SC-004's metric; the spec's operational definition is the single criterion "the
// shown set is empty", so no gold-overlap alternative is offered here.
func jevArmEmptyInjectionRate(rows []jevArmQuestionRow) (float64, int) {
	if len(rows) == 0 {
		return 0, 0
	}
	empty := 0
	for _, row := range rows {
		if row.EmptyInjection {
			empty++
		}
	}
	return float64(empty) / float64(len(rows)), len(rows)
}

// jevArmCostLine is one arm's slice of the pre-run cost plan.
type jevArmCostLine struct {
	Arm         string  `json:"arm"`
	AnswerCalls int     `json:"answer_calls"`
	JudgeCalls  int     `json:"judge_calls"`
	FilterCalls int     `json:"filter_calls"`
	AnswerUSD   float64 `json:"answer_usd"`
	JudgeUSD    float64 `json:"judge_usd"`
	FilterUSD   float64 `json:"filter_usd"`
	USD         float64 `json:"usd"`
}

// jevArmCostPlan is the --estimate output for the four-arm protocol: expected
// provider calls, tokens, and USD for arms x repetitions x questions, printed
// before any spending happens (research R7b).
type jevArmCostPlan struct {
	Questions           int              `json:"questions"`
	Repetitions         int              `json:"repetitions"`
	Arms                []string         `json:"arms"`
	AnswerCalls         int              `json:"answer_calls"`
	JudgeCalls          int              `json:"judge_calls"`
	FilterCalls         int              `json:"filter_calls"`
	FilterInTokens      int              `json:"filter_in_tokens"`
	EstimatedUSD        float64          `json:"estimated_usd"`
	ByArm               []jevArmCostLine `json:"by_arm"`
	UnpricedModels      []string         `json:"unpriced_models,omitempty"`
	AnswerInputCap      int              `json:"answer_input_cap"`
	AnswerRepetitions   int              `json:"answer_repetitions"`
	EmptyInjectionFloor float64          `json:"empty_injection_floor"`
}

// planJevArmCost estimates the four-arm protocol's spend. Only the two filtered
// arms issue filter calls; every arm answers and judges every question for every
// repetition. The numbers are nominal: they exist to be checked against the frozen
// budget before the run, never to replace the run's measured usage.
func planJevArmCost(questions int, repetitions int, prices priceTable, filterModel, answerModel, judgeModel string) jevArmCostPlan {
	if repetitions < 1 {
		repetitions = 1
	}
	if strings.TrimSpace(judgeModel) == "" {
		judgeModel = answerModel
	}
	plan := jevArmCostPlan{
		Questions:           questions,
		Repetitions:         repetitions,
		Arms:                armNamesInOrder(),
		AnswerInputCap:      jevArmAnswerInputCap,
		AnswerRepetitions:   jevArmAnswerRepetitions,
		EmptyInjectionFloor: jevEmptyInjectionFloor,
	}
	filterInTokens := jevArmPoolSize * jevEstimateFilterTokensPerCandidate
	unpriced := map[string]bool{}
	for _, recipe := range jevArmRecipes() {
		line := jevArmCostLine{Arm: string(recipe.Arm)}
		line.AnswerCalls = questions * repetitions
		line.JudgeCalls = line.AnswerCalls
		if recipe.Filtered {
			line.FilterCalls = line.AnswerCalls
		}
		if price, ok := prices.Lookup(answerModel); ok {
			line.AnswerUSD = float64(line.AnswerCalls) * tokenUSD(price, jevEstimateAnswerInTokens, jevEstimateAnswerOutTokens)
		} else if strings.TrimSpace(answerModel) != "" {
			unpriced[answerModel] = true
		}
		if price, ok := prices.Lookup(judgeModel); ok {
			line.JudgeUSD = float64(line.JudgeCalls) * tokenUSD(price, jevEstimateJudgeInTokens, jevEstimateJudgeOutTokens)
		} else if strings.TrimSpace(judgeModel) != "" {
			unpriced[judgeModel] = true
		}
		if line.FilterCalls > 0 {
			if price, ok := prices.Lookup(filterModel); ok {
				line.FilterUSD = float64(line.FilterCalls) * tokenUSD(price, filterInTokens, 0)
			} else if strings.TrimSpace(filterModel) != "" {
				unpriced[filterModel] = true
			}
		}
		line.USD = line.AnswerUSD + line.JudgeUSD + line.FilterUSD
		plan.ByArm = append(plan.ByArm, line)
		plan.AnswerCalls += line.AnswerCalls
		plan.JudgeCalls += line.JudgeCalls
		plan.FilterCalls += line.FilterCalls
		plan.EstimatedUSD += line.USD
	}
	plan.FilterInTokens = plan.FilterCalls * filterInTokens
	for model := range unpriced {
		plan.UnpricedModels = append(plan.UnpricedModels, model)
	}
	sort.Strings(plan.UnpricedModels)
	return plan
}

// printJevArmEstimate reports the four-arm plan without spending anything.
func printJevArmEstimate(convs []conversation, opt options, prices priceTable, filterModel, answerModel, judgeModel string) {
	plan := planJevArmCost(countSelectedQuestions(convs, opt), opt.repeats, prices, filterModel, answerModel, judgeModel)
	fmt.Printf("estimate jev-arms: questions=%d repetitions=%d arms=%s\n", plan.Questions, plan.Repetitions, strings.Join(plan.Arms, ","))
	fmt.Printf("estimate jev-arms: answer_calls=%d judge_calls=%d filter_calls=%d filter_in_tokens=%d estimated_usd=%.6f\n",
		plan.AnswerCalls, plan.JudgeCalls, plan.FilterCalls, plan.FilterInTokens, plan.EstimatedUSD)
	for _, line := range plan.ByArm {
		fmt.Printf("estimate jev-arms: arm=%-9s answer=%d judge=%d filter=%d usd=%.6f\n", line.Arm, line.AnswerCalls, line.JudgeCalls, line.FilterCalls, line.USD)
	}
	for _, model := range plan.UnpricedModels {
		fmt.Printf("estimate jev-arms: unpriced model=%s\n", model)
	}
	fmt.Printf("estimate jev-arms: frozen answer_input_cap=%d repetitions=%d empty_injection_floor=%.2f\n", plan.AnswerInputCap, plan.AnswerRepetitions, plan.EmptyInjectionFloor)
}

// buildJevFilterClient constructs the Jev filter from the run's configuration. An
// unconfigured client collapses to a nil interface here (typed-nil discipline),
// which is what the arms read as "degraded".
func buildJevFilterClient(opt options, pol filter.Policy) (filter.RelevanceFilter, error) {
	client, err := jev.New(jev.Config{
		BaseURL:                    opt.jevBaseURL,
		Model:                      opt.jevModel,
		APIKey:                     opt.jevAPIKey,
		Path:                       opt.jevPath,
		Deadline:                   opt.jevDeadline,
		PricePerMillionInputTokens: opt.jevPricePerMillion,
		Policy:                     pol,
	})
	if err != nil {
		return nil, fmt.Errorf("configure jev filter: %w", err)
	}
	if client == nil {
		return nil, nil
	}
	return client, nil
}

// validateJevCounterFingerprint proves the packer's tokenizer is the one the
// protocol froze. A drift would silently change every "shown" count, so a
// mismatch refuses the run instead of surfacing after the fact.
func validateJevCounterFingerprint(ctx context.Context, counter evidencecompiler.TokenCounter, want string) error {
	if counter == nil {
		return fmt.Errorf("jev arms require a token counter")
	}
	if strings.TrimSpace(want) == "" {
		return nil
	}
	count, err := counter.CountInput(ctx, evidencecompiler.AnswerInput{Model: "fingerprint-probe", System: "s", User: "u"})
	if err != nil {
		return fmt.Errorf("probe jev token counter: %w", err)
	}
	if count.Fingerprint != want {
		return fmt.Errorf("jev token counter fingerprint %q differs from the frozen %q", count.Fingerprint, want)
	}
	return nil
}

// answerJevArmQuestion runs one arm's answer+judge stage on the exact packed
// evidence list. It reuses the unified contract's prompts and the shared judge
// helpers, so the only thing that differs between arms is the evidence they
// packed. The IDK rewrite retry is deliberately not applied: the arms contrast
// first-round evidence sets, and a rewrite would change the arm's own pool.
// runJevArmProtocol therefore requires --no-idk-retry so that setting is explicit
// rather than implicit.
func answerJevArmQuestion(ctx context.Context, answerCall usageModelCaller, judgeCall usageModelCaller, qa locomoQA, opt options, shown []memory.Result) (jevArmOutcome, error) {
	if answerCall == nil || judgeCall == nil {
		return jevArmOutcome{}, fmt.Errorf("jev arm answering requires answer and judge callers")
	}
	prompt := answerSystemPromptForEval(qa, opt)
	userPrompt := buildAnswerContextPrompt(qa.Question, shown, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
	started := time.Now()
	answer, usage, err := answerCall(ctx, prompt, userPrompt)
	outcome := jevArmOutcome{
		Answer:          answer,
		AnswerMs:        int(time.Since(started).Milliseconds()),
		AnswerInTokens:  usage.InputTokens,
		AnswerOutTokens: usage.OutputTokens,
	}
	if err != nil {
		return outcome, fmt.Errorf("jev arm answer call: %w", err)
	}
	verdict, _, judgeErr := judgeCall(ctx, judgeSystemPromptFor(opt.judgeAlignmentMode()), buildJudgePrompt(qa.Question, goldFor(qa), answer))
	if judgeErr != nil {
		return outcome, fmt.Errorf("jev arm judge call: %w", judgeErr)
	}
	outcome.Measured = true
	outcome.JudgeCallMade = true
	outcome.Correct = parseJudgeVerdict(verdict)
	return outcome, nil
}

// jevRegistrationForRun derives the pre-registration from the run's configuration.
// The policy is frozen here — before any retrieval — so no diagnostic sweep can
// become the reported gate (research R3a).
func jevRegistrationForRun(opt options, pol filter.Policy) (jevFilterRegistration, error) {
	registration := newJevFilterRegistration(opt.jevModel, opt.jevBaseURLHost, pol, jevArmPoolSize)
	registration.PilotGateConfirmed = opt.jevPilotGateConfirmed
	registration.WarmupDisposed = opt.jevWarmupDisposed
	registration.SameWindowReps = opt.jevSameWindowReps
	if err := validateJevFilterRegistration(registration); err != nil {
		return jevFilterRegistration{}, err
	}
	return registration, nil
}

// validateJevDeclaredPrerequisites refuses a run before it spends anything when a
// declared protocol prerequisite is missing. The validity gate calls the same
// function, so the pre-flight and the post-run audit cannot disagree.
func validateJevDeclaredPrerequisites(reg jevFilterRegistration) error {
	if !reg.PilotGateConfirmed {
		return fmt.Errorf("the 038 pilot gate must be confirmed for a four-arm run (--jev-pilot-gate-confirmed)")
	}
	if !reg.WarmupDisposed {
		return fmt.Errorf("warm-up disposal must be declared for a four-arm run (--jev-warmup-disposed)")
	}
	if !reg.SameWindowReps {
		return fmt.Errorf("the repetitions must be declared to run in one window (--jev-same-window-reps)")
	}
	return nil
}

// jevPolicyFromEnv freezes the threshold policy from the same environment names
// the product's memory_search knobs use, so the harness and the server cannot
// drift: ENGRAM_JEV_RELAX is read with the same boolean semantics as
// mcpserver/config.go ("0"/"false"/"FALSE" disable the relax stage).
// ENGRAM_JEV_RELAX_MAX and ENGRAM_JEV_KSHOW_MAX are harness-side knobs: the
// server pins both (RelaxMax 3, KShowMax 12). KShowMax is validated against the
// frozen gate budget below; a non-default RelaxMax is recorded in the sealed
// registration instead, so a diagnostic sweep stays attributable.
// Invalid values are refused rather than clamped: a clamped policy would
// make the manifest describe something the run did not do.
func jevPolicyFromEnv(getenv func(string) string) (filter.Policy, error) {
	pol := filter.DefaultPolicy()
	parse := func(name string, target *float64) error {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return nil
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("parse %s=%q: %w", name, raw, err)
		}
		*target = value
		return nil
	}
	if err := parse("ENGRAM_JEV_THETA", &pol.Theta); err != nil {
		return filter.Policy{}, err
	}
	if err := parse("ENGRAM_JEV_RELAX_THETA", &pol.RelaxTheta); err != nil {
		return filter.Policy{}, err
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_RELAX_MAX")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return filter.Policy{}, fmt.Errorf("parse ENGRAM_JEV_RELAX_MAX=%q: %w", raw, err)
		}
		pol.RelaxMax = value
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_KSHOW_MAX")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return filter.Policy{}, fmt.Errorf("parse ENGRAM_JEV_KSHOW_MAX=%q: %w", raw, err)
		}
		pol.KShowMax = value
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_RELAX")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return filter.Policy{}, fmt.Errorf("parse ENGRAM_JEV_RELAX=%q: want a boolean (the server reads it as a boolean too)", raw)
		}
		pol.RelaxDisabled = !enabled
	}
	if !(pol.Theta > 0 && pol.Theta < 1) {
		return filter.Policy{}, fmt.Errorf("ENGRAM_JEV_THETA %v must be inside (0,1)", pol.Theta)
	}
	if !(pol.RelaxTheta > 0 && pol.RelaxTheta <= pol.Theta) {
		return filter.Policy{}, fmt.Errorf("ENGRAM_JEV_RELAX_THETA %v must be inside (0, theta=%v]", pol.RelaxTheta, pol.Theta)
	}
	if pol.RelaxMax < 0 {
		return filter.Policy{}, fmt.Errorf("ENGRAM_JEV_RELAX_MAX %d must be non-negative", pol.RelaxMax)
	}
	if pol.KShowMax != jevArmCShowGate {
		return filter.Policy{}, fmt.Errorf("ENGRAM_JEV_KSHOW_MAX %d must be the frozen gate budget %d", pol.KShowMax, jevArmCShowGate)
	}
	return pol, nil
}

// validateJevArmsOptions is the four-arm pre-flight. It refuses an underspecified
// run before any store is opened, any extraction is paid, or any call is made —
// a misconfigured four-arm run is expensive to discover late. --estimate is exempt
// from the run-only requirements because it never spends; the freeze invocation
// (--eval-freeze-protocol) needs the registration environment instead, because
// that is the step that bakes it into the manifest.
func validateJevArmsOptions(opt options, arms []string) error {
	if opt.datasetFormat != "locomo" {
		return fmt.Errorf("--jev-arms is a LoCoMo-口径 protocol (LongMemEval-S is deferred); got --dataset-format %q", opt.datasetFormat)
	}
	if opt.repeats != jevArmAnswerRepetitions {
		return fmt.Errorf("--jev-arms is frozen at the %d-repetition majority protocol, got --repeats %d", jevArmAnswerRepetitions, opt.repeats)
	}
	if len(arms) != 1 {
		return fmt.Errorf("--jev-arms requires exactly one retrieval backend so every arm recalls identically (--retrieval fts|hybrid), got %d backends", len(arms))
	}
	if opt.chunkQuota != 0 || strings.TrimSpace(opt.catQuotaSpec) != "" {
		return fmt.Errorf("--jev-arms pins the frozen retrieval recipe to zero chunk quota (--chunk-quota=0 and no --cat-chunk-quota): quota partitioning changes pool membership, so arm C (rank truncation) and the filtered arms (which filter the raw pool) would differ in more than selection")
	}
	if opt.estimate {
		return nil
	}
	if opt.evalFreezeProtocol != "" {
		// The freeze invocation is the pre-registration step itself: it seals the
		// registration (model, policy, declarations) into the manifest before the
		// protocol digest is computed.
		return validateJevArmsRegistrationEnvironment(opt)
	}
	if opt.runDir == "" {
		return fmt.Errorf("--jev-arms requires --run-dir")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--jev-arms requires --store-dir: the arms read persisted per-conversation stores instead of re-ingesting")
	}
	if !opt.chunks {
		return fmt.Errorf("--jev-arms requires --chunks so exact-turn recall can be graded from the stored chunks")
	}
	if !opt.noIDKRetry {
		return fmt.Errorf("--jev-arms measures first-round evidence sets; pass --no-idk-retry so every arm shares one answer contract")
	}
	if opt.formalProtocol == nil {
		return fmt.Errorf("--jev-arms requires a frozen protocol (--eval-freeze-protocol) whose manifest carries the filter registration and the answer-input cap")
	}
	if strings.TrimSpace(opt.tokenCounterBaseURL) == "" {
		return fmt.Errorf("--jev-arms requires --token-counter-base-url: the packer counts the exact answer input it admits")
	}
	if err := validateJevArmsRegistrationEnvironment(opt); err != nil {
		return err
	}
	if !opt.jevDegradedPass && strings.TrimSpace(os.Getenv("ENGRAM_JEV_API_KEY")) == "" {
		return fmt.Errorf("--jev-arms requires ENGRAM_JEV_API_KEY, or --jev-degraded-pass to measure the fallback path")
	}
	return nil
}

// validateJevArmsRegistrationEnvironment is the pre-registration pre-flight: the
// pinned model, the threshold policy and the three declared prerequisites are all
// part of the registration the manifest seals, so an environment that cannot
// produce that registration must be refused before anything is frozen or spent.
func validateJevArmsRegistrationEnvironment(opt options) error {
	model := strings.TrimSpace(opt.jevModel)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("ENGRAM_JEV_MODEL"))
	}
	if model == "" {
		return fmt.Errorf("--jev-arms requires a pinned filter model (--jev-filter-model or ENGRAM_JEV_MODEL)")
	}
	if err := validatePinnedFilterModel(model); err != nil {
		return err
	}
	if _, err := jevPolicyFromEnv(os.Getenv); err != nil {
		return err
	}
	if !opt.jevB0Continuity {
		return fmt.Errorf("a four-arm run must declare its B0 continuity receipts (--jev-b0-continuity-declared), so the validity gate can require the summary artifact")
	}
	return validateJevDeclaredPrerequisites(jevFilterRegistration{
		PilotGateConfirmed: opt.jevPilotGateConfirmed,
		WarmupDisposed:     opt.jevWarmupDisposed,
		SameWindowReps:     opt.jevSameWindowReps,
	})
}

// attachJevArmsRegistrationForFreeze derives the run's registration from the
// freeze-time environment and bakes it into the manifest. Reading the environment
// at freeze time is what makes the pre-registration real: the digest the run and
// the journal cite covers the model, the policy and the cap the arms then use.
func attachJevArmsRegistrationForFreeze(opt options, protocol *evalProtocol) error {
	policy, err := jevPolicyFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	opt.jevPolicy = policy
	opt.jevBaseURL = os.Getenv("ENGRAM_JEV_BASE_URL")
	opt.jevBaseURLHost = baseURLHost(opt.jevBaseURL)
	if strings.TrimSpace(opt.jevModel) == "" {
		opt.jevModel = os.Getenv("ENGRAM_JEV_MODEL")
	}
	registration, err := jevRegistrationForRun(opt, policy)
	if err != nil {
		return err
	}
	return attachJevFilterRegistration(protocol, registration)
}

// runJevArms is the --jev-arms entry point: it assembles the run's filter
// configuration, applies the pre-registration, refuses an underspecified run
// before it spends anything, and dispatches the gated or degraded pass.
func runJevArms(ctx context.Context, opt options, convs []conversation, prices priceTable, logger *slog.Logger) error {
	policy, err := jevPolicyFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	opt.jevPolicy = policy
	opt.jevBaseURL = os.Getenv("ENGRAM_JEV_BASE_URL")
	opt.jevAPIKey = os.Getenv("ENGRAM_JEV_API_KEY")
	opt.jevPath = os.Getenv("ENGRAM_JEV_PATH")
	if strings.TrimSpace(opt.jevModel) == "" {
		opt.jevModel = os.Getenv("ENGRAM_JEV_MODEL")
	}
	if raw := strings.TrimSpace(os.Getenv("ENGRAM_JEV_PRICE_PER_MILLION")); raw != "" && opt.jevPricePerMillion == 0 {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("parse ENGRAM_JEV_PRICE_PER_MILLION=%q: %w", raw, err)
		}
		opt.jevPricePerMillion = value
	}
	opt.jevBaseURLHost = baseURLHost(opt.jevBaseURL)
	if opt.repeats != jevArmAnswerRepetitions {
		return fmt.Errorf("--jev-arms is frozen at the %d-repetition majority protocol, got --repeats %d", jevArmAnswerRepetitions, opt.repeats)
	}
	if opt.jevDegradedPass {
		// The degraded pass is the product's no-key path: the configured client is
		// dropped deliberately so every filtered arm can only fall back.
		opt.jevAPIKey = ""
	} else if strings.TrimSpace(opt.jevAPIKey) == "" {
		return fmt.Errorf("--jev-arms requires ENGRAM_JEV_API_KEY, or --jev-degraded-pass to measure the fallback path")
	}
	registration, err := jevRegistrationForRun(opt, policy)
	if err != nil {
		return err
	}
	if err := validateJevDeclaredPrerequisites(registration); err != nil {
		return err
	}
	opt.jevFilter = &filterRetrieval{
		Pool:   jevArmPoolSize,
		Show:   opt.topK,
		Policy: policy,
	}
	client, err := buildJevFilterClient(opt, policy)
	if err != nil {
		return err
	}
	opt.jevFilter.Filter = client
	logger.Info("jev four-arm run",
		"pass", map[bool]string{true: jevArmPassDegraded, false: jevArmPassGated}[opt.jevDegradedPass],
		"filter_model", registration.FilterModel,
		"theta", policy.Theta,
		"pool", registration.Pool,
		"concurrency", opt.concurrency,
	)
	return runJevArmProtocol(ctx, opt, convs, policy, opt.formalProtocol, prices, logger)
}

// primaryRetrievalArm is the retrieval backend all four arms share. The arms
// differ in what they show, never in how they recall, so a run pins one arm name.
func primaryRetrievalArm(opt options) string {
	arms, err := armsFor(opt.retrieval)
	if err != nil || len(arms) == 0 {
		return "hybrid"
	}
	return arms[0]
}

// runJevArmProtocol executes the four-arm protocol over persisted conversation
// stores and writes the measurement artifacts, the filter-call audit, and the SC
// verdict. Conversations run through a worker pool that honors --concurrency
// (AGENTS.md: model-side stages never run sequentially); within a conversation the
// arms run in recipe order, and a failure fails the run closed instead of
// producing a partial verdict.
func runJevArmProtocol(ctx context.Context, opt options, convs []conversation, pol filter.Policy, protocol *evalProtocol, prices priceTable, logger *slog.Logger) error {
	if opt.runDir == "" {
		return fmt.Errorf("--jev-arms requires --run-dir")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--jev-arms requires --store-dir: the arms read persisted per-conversation stores instead of re-ingesting")
	}
	if !opt.noIDKRetry {
		return fmt.Errorf("--jev-arms measures first-round evidence sets; pass --no-idk-retry so every arm shares one answer contract")
	}
	if protocol == nil {
		return fmt.Errorf("--jev-arms requires a frozen protocol (see --eval-freeze-protocol) whose manifest carries the filter registration and the answer-input cap")
	}
	if err := validateJevDeclaredPrerequisites(jevFilterRegistration{
		PilotGateConfirmed: opt.jevPilotGateConfirmed,
		WarmupDisposed:     opt.jevWarmupDisposed,
		SameWindowReps:     opt.jevSameWindowReps,
	}); err != nil {
		return err
	}
	registration, err := jevRegistrationForRun(opt, pol)
	if err != nil {
		return err
	}
	if err := verifyJevFilterRegistrationBinding(*protocol, registration); err != nil {
		return err
	}
	if opt.jevFilter == nil {
		return fmt.Errorf("--jev-arms requires the run's filter retrieval spec (pool/show/filter/policy)")
	}
	if !opt.jevDegradedPass && filter.IsNilRelevanceFilter(opt.jevFilter.Filter) {
		return fmt.Errorf("a gated four-arm run requires a configured filter; use --jev-degraded-pass to measure the fallback path")
	}
	apiKey := os.Getenv("LOCOMO_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LOCOMO_API_KEY is required for the four-arm protocol (never passed as a flag so it stays out of process listings)")
	}
	if strings.TrimSpace(opt.tokenCounterBaseURL) == "" {
		return fmt.Errorf("--jev-arms requires --token-counter-base-url: the packer counts the exact answer input it admits")
	}
	counter, err := newVLLMTokenCounter(vllmTokenCounterConfig{
		BaseURL: opt.tokenCounterBaseURL, APIKey: apiKey, Fingerprint: protocol.Budget.CounterFingerprint,
	})
	if err != nil {
		return fmt.Errorf("configure jev arm token counter: %w", err)
	}
	if err := validateJevCounterFingerprint(ctx, counter, protocol.Budget.CounterFingerprint); err != nil {
		return err
	}
	model := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	prov, err := buildBenchProvider(envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider), apiKey, envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic"), opt.maxTokens, "LOCOMO_PROVIDER")
	if err != nil {
		return err
	}
	judge := resolveJudgeConfig(os.Getenv)
	judgeProv, err := buildBenchProvider(judge.Provider, judge.APIKey, judge.BaseURL, opt.maxTokens, "JUDGE_PROVIDER")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create jev arm run dir: %w", err)
	}
	journal, err := openJevFilterCallJournal(opt.runDir, protocol.ProtocolHash)
	if err != nil {
		return err
	}
	defer func() { _ = journal.Close() }()

	sem := make(chan struct{}, max(1, opt.concurrency))
	ledger := newCostLedger(prices)
	recordUsage := func(role, model string, usage provider.Usage) {
		recordBenchUsage(ledger, role, model, usage)
	}
	answerCall := gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "answer", recordUsage))
	judgeCall := gateUsage(sem, newUsageModelCallerWithUsage(judgeProv, judge.Model, opt.maxTokens, "judge", recordUsage))
	armName := primaryRetrievalArm(opt)
	repetitions := opt.repeats
	if repetitions < 1 {
		repetitions = 1
	}

	// The --top-k budget splits per arm into an internal pool-k and a show-k: arm A
	// keeps --top-k as its shown budget while the evaluator arms own a frozen pool,
	// so the log states both numbers instead of one ambiguous k (task T13).
	for _, recipe := range jevArmRecipes() {
		poolK, showK := jevArmTopKSplit(opt, recipe)
		logger.Info("jev arm split", "arm", string(recipe.Arm), "pool_k", poolK, "show_k", showK)
	}

	var mu sync.Mutex
	var rows []jevArmQuestionRow
	var derived []jevArmQuestionDerived
	var retrievalCalls int
	var firstErr error
	var invalidReason string
	var wg sync.WaitGroup
	for ci := range convs {
		wg.Add(1)
		go func(conv conversation) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			fail := func(err error) {
				mu.Lock()
				defer mu.Unlock()
				if firstErr == nil {
					firstErr = err
				}
			}
			// invalidate records a reason that makes the whole run unpromotable: a
			// silently weakened arm B, a call failure, a counter drift.
			invalidate := func(reason string) {
				mu.Lock()
				defer mu.Unlock()
				if invalidReason == "" {
					invalidReason = reason
				}
			}
			runtime, err := openAttributionRuntime(ctx, opt, conv, nil, armName)
			if err != nil {
				fail(err)
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[armName]
			if retriever == nil {
				fail(fmt.Errorf("conversation %d has no retriever for arm %s", conv.ID, armName))
				return
			}
			for repetition := 0; repetition < repetitions; repetition++ {
				for _, selected := range selectQuestions(conv, opt) {
					if ctx.Err() != nil {
						return
					}
					qa := selected.QA
					topK, quota := opt.retrievalFor(qa.Category)
					productionLimit := topK
					if productionLimit <= 0 {
						productionLimit = jevArmCShowReference
					}
					render := jevArmAnswerRenderer(qa, opt, model)
					observations := make(map[jevArm]jevArmObservation, len(jevArmNames()))
					outcomes := make(map[jevArm]jevArmOutcome, len(jevArmNames()))
					aborted := false
					for _, recipe := range jevArmRecipes() {
						observation, err := jevArmRetrieve(ctx, retriever, qa.Question, recipe, opt.jevFilter, quota, productionLimit)
						if err != nil {
							fail(err)
							aborted = true
							break
						}
						packed, err := packJevArmAnswerInput(ctx, counter, jevArmAnswerInputCap, render, observation.Presented)
						if err != nil {
							fail(err)
							aborted = true
							break
						}
						observation.Packed = packed
						if recipe.Arm == jevArmB {
							if err := assertJevArmBUntruncated(packed, len(observation.Pool), jevArmAnswerInputCap); err != nil {
								reason := fmt.Sprintf("conv=%d q=%d: %v", conv.ID, selected.Index, err)
								invalidate(reason)
								fail(fmt.Errorf("%s", reason))
								aborted = true
								break
							}
						}
						if recipe.Filtered {
							if err := journal.Record(jevFilterCallRecord{
								Conv: conv.ID, Q: selected.Index, Repetition: repetition, Arm: string(recipe.Arm),
								Pool: len(observation.Pool), Memories: len(observation.Presented),
								Kept: observation.Meta.Kept, Dropped: observation.Meta.Dropped, Theta: observation.Meta.Theta,
								LatencyMs: observation.Meta.LatencyMs, USD: observation.Meta.CostUSD,
								InputTokens: observation.Meta.InputTokens, OutputTokens: observation.Meta.OutputTokens,
								Degraded: observation.Degraded, Notes: observation.Meta.Notes,
							}); err != nil {
								fail(err)
								aborted = true
								break
							}
						}
						observations[recipe.Arm] = observation
						mu.Lock()
						retrievalCalls++
						mu.Unlock()
					}
					if aborted {
						return
					}
					for _, recipe := range jevArmRecipes() {
						outcome, err := answerJevArmQuestion(ctx, answerCall, judgeCall, qa, opt, observations[recipe.Arm].shown())
						if err != nil {
							fail(fmt.Errorf("conv=%d q=%d arm=%s: %w", conv.ID, selected.Index, recipe.Arm, err))
							aborted = true
							break
						}
						outcomes[recipe.Arm] = outcome
					}
					if aborted {
						return
					}
					measured, questionDerived, err := measureJevArmQuestion(jevArmQuestionInput{
						Conv: conv.ID, Q: selected.Index, Category: qa.Category, QuestionID: qa.QuestionID,
						Repetition: repetition, QA: qa, ChunkTurns: runtime.chunkTurns,
						Observations: observations, Outcomes: outcomes,
					})
					if err != nil {
						fail(err)
						return
					}
					questionDerived.Replications = repetitions
					mu.Lock()
					rows = append(rows, measured...)
					derived = append(derived, questionDerived)
					mu.Unlock()
				}
			}
		}(convs[ci])
	}
	wg.Wait()
	if len(rows) == 0 {
		if firstErr != nil {
			return firstErr
		}
		return fmt.Errorf("--jev-arms produced no measurements; check the question selection and the persisted store")
	}
	// A run that failed part way through, or that covered only part of the frozen
	// cohort, must never leave a promotable verdict on disk: the invalid reason is
	// what forces HOLD/INVALID whatever the partial rows say.
	mu.Lock()
	invalidReason = jevArmInvalidReason(invalidReason, firstErr, protocol, derived)
	measured := retrievalCalls
	mu.Unlock()
	logger.Info("jev four-arm measurement complete", "rows", len(rows), "filter_calls", journal.Count(), "retrieval_calls", measured, "invalid", invalidReason != "")
	writeErr := writeJevArmArtifacts(opt, registration, protocol, rows, derived, journal.Count(), measured, ledger, opt.jevDegradedPass, invalidReason)
	if firstErr != nil {
		return firstErr
	}
	return writeErr
}

// jevArmInvalidReason combines the run's failure and coverage facts into the
// reason that makes a verdict unpromotable. An already-recorded reason wins: the
// first cause is the one worth reading.
func jevArmInvalidReason(existing string, firstErr error, protocol *evalProtocol, derived []jevArmQuestionDerived) string {
	if existing != "" {
		return existing
	}
	if firstErr != nil {
		return fmt.Sprintf("the run aborted: %v", firstErr)
	}
	return jevArmCoverageViolation(protocol, derived)
}

// jevArmCoverageViolation reports why the measured question set cannot support a
// promotable verdict. The four-arm protocol is a full-cohort measurement, so a run
// that covered only part of the frozen question set must be INVALID rather than
// scored over a smaller denominator (the formal runner has the same full-cohort
// rule; --only-questions is its research-subset escape and cannot promote).
func jevArmCoverageViolation(protocol *evalProtocol, derived []jevArmQuestionDerived) string {
	if protocol == nil || protocol.Benchmark.QuestionCount <= 0 {
		return ""
	}
	seen := make(map[[2]int]bool, len(derived))
	for _, question := range derived {
		if question.Block != jevArmMainBlock {
			continue
		}
		seen[[2]int{question.Conv, question.Q}] = true
	}
	if len(seen) != protocol.Benchmark.QuestionCount {
		return fmt.Sprintf("measured %d main-block questions, but the frozen protocol declares %d; a partial cohort is not promotable", len(seen), protocol.Benchmark.QuestionCount)
	}
	return ""
}
