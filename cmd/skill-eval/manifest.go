package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 048 manifest/digest/seal primitives (data-model.md §7, dataset-protocol §7.1).

func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func osWriteFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// WriteFrozenFile materializes an immutable artifact: it refuses to overwrite
// an existing file (frozen outputs are never rewritten).
func WriteFrozenFile(p string, data []byte) error {
	if _, err := osStat(p); err == nil {
		return fmt.Errorf("frozen output %s already exists and is never overwritten", p)
	}
	return osWriteFile(p, data)
}

// EnsureInside resolves candidate under parent and asserts containment after
// symlink elimination. Returns the resolved path or an error.
func EnsureInside(parent, candidate string) (string, error) {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	// Eliminate symlinks along both paths where they exist.
	if rp, err := filepath.EvalSymlinks(absParent); err == nil {
		absParent = rp
	}
	if rp, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(rp, filepath.Base(abs))
	}
	rel, err := filepath.Rel(absParent, abs)
	if err != nil {
		return "", fmt.Errorf("path containment check failed: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes parent %q", candidate, parent)
	}
	return filepath.Join(absParent, rel), nil
}

// CaseIDsDigest is the sorted-list receipt for a manifest case ID set.
func CaseIDsDigest(ids []string) (string, error) {
	c := append([]string{}, ids...)
	sort.Strings(c)
	return CanonicalSHA256(c)
}

// DatasetPayloadDigest implements `agent-memory-trigger-dataset-sha256-v1`
// (dataset-protocol §7.1): over the manifest-named case payload files only —
// never the manifest, seal, directory-discovered or legacy extra files.
func DatasetPayloadDigest(dir string, m *DatasetManifestV2) (string, error) {
	files := append([]PayloadFileV1{}, m.PayloadFiles...)
	sort.Slice(files, func(i, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	var buf bytes.Buffer
	covered := map[string]int{}
	for _, pf := range files {
		if !safeRelativePath(pf.RelativePath) {
			return "", fmt.Errorf("payload path %q is not containment-safe", pf.RelativePath)
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pf.RelativePath)))
		if err != nil {
			return "", fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		norm, err := LFNormalizedSHA256(b)
		if err != nil {
			return "", fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		if pf.LFNormalizedSHA256 != "PLACEHOLDER_COMPUTED_AT_RUNTIME" && norm != pf.LFNormalizedSHA256 {
			return "", fmt.Errorf("payload file %s digest mismatch", pf.RelativePath)
		}
		buf.WriteString(pf.RelativePath)
		buf.WriteByte(0)
		fmt.Fprintf(&buf, "%d", len(b))
		buf.WriteByte(0)
		buf.Write(normalizeLF(b))
		buf.WriteByte(0)
		for _, id := range pf.CaseIDs {
			covered[id]++
		}
	}
	for _, id := range m.CaseIDs {
		if covered[id] != 1 {
			return "", fmt.Errorf("case %s appears %d times across payload_files", id, covered[id])
		}
	}
	if len(covered) != len(m.CaseIDs) {
		return "", errors.New("payload_files cover a different case set than the manifest")
	}
	return sha256Hex(buf.Bytes()), nil
}

// CompleteManifestForSeal verifies freeze-before-digest preconditions and
// returns the canonical manifest digest excluding only the `seal` object.
func CompleteManifestForSeal(m *DatasetManifestV2) (string, error) {
	if m.CaseCount == 0 || len(m.CaseIDs) == 0 {
		return "", errors.New("manifest is not complete: case set empty")
	}
	if m.PayloadDigest == "" {
		return "", errors.New("manifest is not complete: payload_digest missing (freeze before digest)")
	}
	if m.CaseIDsDigest == "" {
		return "", errors.New("manifest is not complete: case_ids_digest missing")
	}
	if m.Seal != nil {
		return "", errors.New("manifest already carries a seal")
	}
	return CanonicalSHA256(m)
}

// BuildDatasetAnchor assembles the exact DatasetAnchorV1 preimage and its
// digests for the chosen anchor type.
func BuildDatasetAnchor(m *DatasetManifestV2, manifestDigest, anchorType, anchorID string) (*DatasetSeal, error) {
	switch anchorType {
	case "git-tag", "detached-signature", "immutable-object":
	default:
		return nil, fmt.Errorf("anchor_type %q invalid", anchorType)
	}
	anchor := DatasetAnchorV1{
		SchemaVersion: 1, Canonicalization: CanonicalizationName,
		DatasetID: m.DatasetID, DatasetVersion: m.DatasetVersion,
		ManifestDigest: manifestDigest, DatasetPayloadDigest: m.PayloadDigest,
	}
	preimage, err := CanonicalJSON(anchor)
	if err != nil {
		return nil, err
	}
	seal := &DatasetSeal{
		ManifestDigest: manifestDigest, DatasetPayloadDigest: m.PayloadDigest,
		AnchorType: anchorType, AnchorID: anchorID,
		AnchorPreimageDigest: sha256Hex(preimage),
		AnchorContentDigest:  sha256Hex(preimage), // git-tag/immutable-object: exact anchor bytes
		SealedBy:             "skill-eval-048-controller",
	}
	if anchorType == "detached-signature" {
		seal.AnchorContentDigest = "" // bound to signature bytes at verification time
	}
	return seal, nil
}

// VerifyDatasetSeal re-derives the manifest digest, payload digest and anchor
// preimage from the completed manifest and fails closed on any mismatch,
// self-reference or post-seal mutation.
func VerifyDatasetSeal(m *DatasetManifestV2, dir string) error {
	if m.Seal == nil {
		return errors.New("manifest is not sealed")
	}
	seal := m.Seal
	mCopy := *m
	mCopy.Seal = nil
	digest, err := CanonicalSHA256(mCopy)
	if err != nil {
		return err
	}
	if digest != seal.ManifestDigest {
		return fmt.Errorf("manifest digest mismatch: seal %s != recomputed %s (post-seal mutation)", seal.ManifestDigest, digest)
	}
	payloadDigest, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		return err
	}
	if payloadDigest != seal.DatasetPayloadDigest || payloadDigest != m.PayloadDigest {
		return errors.New("dataset payload digest mismatch")
	}
	anchor := DatasetAnchorV1{
		SchemaVersion: 1, Canonicalization: CanonicalizationName,
		DatasetID: m.DatasetID, DatasetVersion: m.DatasetVersion,
		ManifestDigest: seal.ManifestDigest, DatasetPayloadDigest: seal.DatasetPayloadDigest,
	}
	preimage, err := CanonicalJSON(anchor)
	if err != nil {
		return err
	}
	if sha256Hex(preimage) != seal.AnchorPreimageDigest {
		return errors.New("anchor preimage digest mismatch")
	}
	if seal.AnchorType == "git-tag" || seal.AnchorType == "immutable-object" {
		if seal.AnchorContentDigest != sha256Hex(preimage) {
			return errors.New("anchor content digest does not match the exact DatasetAnchorV1 bytes")
		}
	}
	return nil
}

// FreezeBeforeDigest is the shared guard for any receipt whose own digest is
// computed last: digest must be empty during preimage computation.
func FreezeBeforeDigest(digest *string) error {
	if digest == nil || *digest != "" {
		return errors.New("freeze-before-digest violated: self digest already set")
	}
	return nil
}

// ================= US4 T045: formal-series lifecycle =================
//
// The IO/lifecycle face of the sealed core execution plan, the purpose-aware
// formal series manifest with its protected-execution / workspace-canary
// bindings, the append-only holdout binding receipt and the sealed primary
// run manifest (data-model.md §7-§9).
//
// Worker probes, workspace canaries and primary runs are EXECUTED by the
// runner side (T049/T051) and injected here as receipts: this layer validates
// and binds them and fails closed whenever a required receipt is missing,
// failed or drifted. Every self-digested artifact is sealed
// freeze-before-digest into a frozen file that is never rewritten.

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copySeedMap(in map[int]string) map[int]string {
	out := make(map[int]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ---------- bound dataset manifests ----------

// datasetBinding is the identity slice of a manifest a formal artifact binds.
type datasetBinding struct {
	Digest    string
	Version   string
	CaseCount int
}

// DatasetManifestDigest loads a manifest, requires a complete case set with a
// frozen payload digest and returns its canonical digest (the manifest as
// sealed: the seal object is excluded from the preimage and, when present,
// must agree with it).
func DatasetManifestDigest(path string) (string, *DatasetManifestV2, error) {
	m, err := LoadDatasetManifest(path)
	if err != nil {
		return "", nil, err
	}
	if m.CaseCount == 0 || len(m.CaseIDs) != m.CaseCount {
		return "", nil, fmt.Errorf("manifest %s case set is incomplete", path)
	}
	if m.PayloadDigest == "" {
		return "", nil, fmt.Errorf("manifest %s has no payload_digest (not frozen)", path)
	}
	saved := m.Seal
	m.Seal = nil
	digest, err := CanonicalSHA256(m)
	m.Seal = saved
	if err != nil {
		return "", nil, err
	}
	if saved != nil && saved.ManifestDigest != digest {
		return "", nil, fmt.Errorf("manifest %s seal digest %s != recomputed %s (post-seal mutation)", path, saved.ManifestDigest, digest)
	}
	return digest, m, nil
}

// formalDatasetBinding additionally pins the membership and its exact size.
func formalDatasetBinding(path, membership string) (*datasetBinding, error) {
	digest, m, err := DatasetManifestDigest(path)
	if err != nil {
		return nil, err
	}
	if m.ScoreMembership != membership {
		return nil, fmt.Errorf("manifest %s membership %q, want %q", path, m.ScoreMembership, membership)
	}
	want, err := ExpectedQuestionCount(membership)
	if err != nil {
		return nil, err
	}
	if m.CaseCount != want {
		return nil, fmt.Errorf("manifest %s carries %d cases, want %d", path, m.CaseCount, want)
	}
	return &datasetBinding{Digest: digest, Version: m.DatasetVersion, CaseCount: m.CaseCount}, nil
}

// coreManifestBinding binds the frozen core172 manifest a plan or series
// references (`skills/engram/evals/dev-regression-core.manifest.json` in
// production).
func coreManifestBinding(path string) (string, error) {
	b, err := formalDatasetBinding(path, MembershipCore172)
	if err != nil {
		return "", err
	}
	return b.Digest, nil
}

// ---------- §8 sealed core execution plan ----------

// CorePlanInput is everything `core-plan create` freezes. The plan binds the
// frozen core172 manifest and deliberately not the evaluated skill: the skill
// package stays the single intended SC-5 variable.
type CorePlanInput struct {
	PlanID                                string
	CoreManifestPath                      string
	RunnerRevision                        string
	RunnerDigest                          string // empty → current runner digest
	JudgeRuleDigest                       string // empty → current judge rule digest
	Hosts                                 []string
	ToolIdentityDigests                   map[string]string
	TimeoutSeconds                        int
	Concurrency                           int
	CaseOrderSeeds                        map[int]string
	CoreBoundaryKind                      BoundaryKind
	NormalizedCoreWorkerIdentitySetDigest string
	NormalizedCoreBoundaryTemplateDigest  string
	NormalizedCoreExecutionTemplateDigest string
	CreatedAt                             string // empty → now
	OutPath                               string // empty → nothing written
}

// validateUnsealedPlan runs the closed plan semantics with placeholder self
// digests, so plan creation is checked by exactly the validator a loader uses.
func validateUnsealedPlan(p *CoreExecutionPlanReceipt) error {
	probe := *p
	probe.ReceiptDigest, probe.SealDigest = "unsealed", "unsealed"
	return ValidateCoreExecutionPlan(&probe)
}

// corePlanReceiptDigest is the plan's self digest (both self digests empty);
// corePlanSealDigest is sealed over the receipt-bearing plan.
// planDigestProjection mirrors CoreExecutionPlanReceipt with canonical-JSON
// map keys (ordinal keys as their shortest decimal form, the same projection
// CandidateBindingV1 uses). Every field is carried, so the digest still
// detects a change to any of them.
type planDigestProjection struct {
	SchemaVersion                         int               `json:"schema_version"`
	PlanID                                string            `json:"plan_id"`
	CoreManifestDigest                    string            `json:"core_manifest_digest"`
	RunnerRevision                        string            `json:"runner_revision"`
	RunnerDigest                          string            `json:"runner_digest"`
	JudgeRuleDigest                       string            `json:"judge_rule_digest"`
	Hosts                                 []string          `json:"hosts"`
	ToolIdentityDigests                   map[string]string `json:"tool_identity_digests"`
	TimeoutSeconds                        int               `json:"timeout_seconds"`
	Concurrency                           int               `json:"concurrency"`
	CaseOrderSeeds                        map[string]string `json:"case_order_seeds"`
	CoreBoundaryKind                      BoundaryKind      `json:"core_boundary_kind"`
	NormalizedCoreWorkerIdentitySetDigest string            `json:"normalized_core_worker_identity_set_digest"`
	NormalizedCoreBoundaryTemplateDigest  string            `json:"normalized_core_boundary_template_digest"`
	NormalizedCoreExecutionTemplateDigest string            `json:"normalized_core_execution_template_digest"`
	CreatedAt                             string            `json:"created_at"`
	ReceiptDigest                         string            `json:"receipt_digest"`
	SealDigest                            string            `json:"seal_digest"`
}

func seedMapToStrings(in map[int]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strconv.Itoa(k)] = v
	}
	return out
}

func canaryMapToStrings(in map[string]map[int]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for h, slots := range in {
		m := make(map[string]string, len(slots))
		for slot, digest := range slots {
			m[strconv.Itoa(slot)] = digest
		}
		out[h] = m
	}
	return out
}

func planDigestPreimage(p *CoreExecutionPlanReceipt) planDigestProjection {
	return planDigestProjection{
		SchemaVersion: p.SchemaVersion, PlanID: p.PlanID, CoreManifestDigest: p.CoreManifestDigest,
		RunnerRevision: p.RunnerRevision, RunnerDigest: p.RunnerDigest, JudgeRuleDigest: p.JudgeRuleDigest,
		Hosts: p.Hosts, ToolIdentityDigests: p.ToolIdentityDigests,
		TimeoutSeconds: p.TimeoutSeconds, Concurrency: p.Concurrency,
		CaseOrderSeeds:                        seedMapToStrings(p.CaseOrderSeeds),
		CoreBoundaryKind:                      p.CoreBoundaryKind,
		NormalizedCoreWorkerIdentitySetDigest: p.NormalizedCoreWorkerIdentitySetDigest,
		NormalizedCoreBoundaryTemplateDigest:  p.NormalizedCoreBoundaryTemplateDigest,
		NormalizedCoreExecutionTemplateDigest: p.NormalizedCoreExecutionTemplateDigest,
		CreatedAt:                             p.CreatedAt,
		ReceiptDigest:                         p.ReceiptDigest, SealDigest: p.SealDigest,
	}
}

func corePlanReceiptDigest(p *CoreExecutionPlanReceipt) (string, error) {
	savedR, savedS := p.ReceiptDigest, p.SealDigest
	p.ReceiptDigest, p.SealDigest = "", ""
	d, err := CanonicalSHA256(planDigestPreimage(p))
	p.ReceiptDigest, p.SealDigest = savedR, savedS
	return d, err
}

func corePlanSealDigest(p *CoreExecutionPlanReceipt) (string, error) {
	saved := p.SealDigest
	p.SealDigest = ""
	d, err := CanonicalSHA256(planDigestPreimage(p))
	p.SealDigest = saved
	return d, err
}

// VerifyCoreExecutionPlan re-derives both self digests; any post-seal
// mutation of any field fails closed.
func VerifyCoreExecutionPlan(p *CoreExecutionPlanReceipt) error {
	if p == nil {
		return errors.New("nil core execution plan")
	}
	wantReceipt, err := corePlanReceiptDigest(p)
	if err != nil {
		return err
	}
	if wantReceipt != p.ReceiptDigest {
		return fmt.Errorf("core plan receipt digest %s != recomputed %s (post-seal mutation)", p.ReceiptDigest, wantReceipt)
	}
	wantSeal, err := corePlanSealDigest(p)
	if err != nil {
		return err
	}
	if wantSeal != p.SealDigest {
		return errors.New("core plan seal digest mismatch (post-seal mutation)")
	}
	return nil
}

// CreateCoreExecutionPlan seals the frozen core execution conditions once.
// It fails closed on an incomplete identity, a core manifest that is not the
// frozen core172, or an already-existing output file.
func CreateCoreExecutionPlan(in CorePlanInput) (*CoreExecutionPlanReceipt, error) {
	if in.PlanID == "" {
		return nil, errors.New("plan_id empty")
	}
	coreDigest, err := coreManifestBinding(in.CoreManifestPath)
	if err != nil {
		return nil, err
	}
	runner, judge := in.RunnerDigest, in.JudgeRuleDigest
	if runner == "" {
		if runner, err = CurrentRunnerDigest(); err != nil {
			return nil, err
		}
	}
	if judge == "" {
		if judge, err = CurrentJudgeRuleDigest(); err != nil {
			return nil, err
		}
	}
	if in.RunnerRevision == "" {
		return nil, errors.New("runner_revision empty")
	}
	createdAt := in.CreatedAt
	if createdAt == "" {
		createdAt = nowRFC3339()
	}
	p := &CoreExecutionPlanReceipt{
		SchemaVersion:                         1,
		PlanID:                                in.PlanID,
		CoreManifestDigest:                    coreDigest,
		RunnerRevision:                        in.RunnerRevision,
		RunnerDigest:                          runner,
		JudgeRuleDigest:                       judge,
		Hosts:                                 append([]string(nil), in.Hosts...),
		ToolIdentityDigests:                   copyStringMap(in.ToolIdentityDigests),
		TimeoutSeconds:                        in.TimeoutSeconds,
		Concurrency:                           in.Concurrency,
		CaseOrderSeeds:                        copySeedMap(in.CaseOrderSeeds),
		CoreBoundaryKind:                      in.CoreBoundaryKind,
		NormalizedCoreWorkerIdentitySetDigest: in.NormalizedCoreWorkerIdentitySetDigest,
		NormalizedCoreBoundaryTemplateDigest:  in.NormalizedCoreBoundaryTemplateDigest,
		NormalizedCoreExecutionTemplateDigest: in.NormalizedCoreExecutionTemplateDigest,
		CreatedAt:                             createdAt,
	}
	if err := validateUnsealedPlan(p); err != nil {
		return nil, fmt.Errorf("core plan is not complete: %w", err)
	}
	if err := FreezeBeforeDigest(&p.ReceiptDigest); err != nil {
		return nil, err
	}
	receiptDigest, err := corePlanReceiptDigest(p)
	if err != nil {
		return nil, err
	}
	p.ReceiptDigest = receiptDigest
	sealDigest, err := corePlanSealDigest(p)
	if err != nil {
		return nil, err
	}
	p.SealDigest = sealDigest
	if in.OutPath != "" {
		// Canonical JSON admits string map keys only, so the file is the
		// canonical form of the digest projection (identical field names and
		// values, ordinals as decimal strings).
		if err := WriteFrozenFile(in.OutPath, mustCanonicalJSON(planDigestPreimage(p))); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// LoadCoreExecutionPlan strictly parses and re-verifies a sealed plan.
func LoadCoreExecutionPlan(path string) (*CoreExecutionPlanReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p CoreExecutionPlanReceipt
	if err := StrictParseClosed(b, &p); err != nil {
		return nil, fmt.Errorf("core execution plan %s: %w", path, err)
	}
	if err := ValidateCoreExecutionPlan(&p); err != nil {
		return nil, fmt.Errorf("core execution plan %s: %w", path, err)
	}
	if err := VerifyCoreExecutionPlan(&p); err != nil {
		return nil, fmt.Errorf("core execution plan %s: %w", path, err)
	}
	return &p, nil
}

// ---------- §8 formal series manifest (`series prepare`) ----------

// SeriesPrepareInput carries everything `series prepare` binds. The protected
// execution receipt and the workspace canaries are runner-side products
// (T049/T051) injected here and validated — never fabricated, never optional
// for an official-dual series.
type SeriesPrepareInput struct {
	SeriesID                     string
	Purpose                      SeriesPurpose
	CorePlanPath                 string
	CoreManifestPath             string // must be the exact manifest the plan binds
	HoldoutManifestPath          string // official-dual only
	SnapshotRoot                 string
	PackageValidationReceiptPath string
	GreenTestReceiptPath         string
	ValidatorPath                string
	RunnerDigest                 string // must equal the plan's
	JudgeRuleDigest              string // must equal the plan's
	ToolIdentityDigests          map[string]string
	ToolConfigurationDigest      string
	ExecutionEnvironmentDigest   string // must equal the plan's core template digest
	CaseOrderSeeds               map[int]string
	TimeoutSeconds               int
	Concurrency                  int
	StagedWorkspaceFiles         bool
	Protected                    *ProtectedExecutionReceipt
	Canaries                     map[string]map[int]*WorkspaceCanaryReceipt
	OutPath                      string // empty → <root>/series/<series_id>/series-manifest.json
}

func validateUnsealedSeries(m *FormalSeriesManifest) error {
	probe := *m
	probe.ManifestDigest = "unsealed"
	return ValidateFormalSeriesManifest(&probe)
}

// seriesDigestProjection mirrors FormalSeriesManifest with canonical-JSON map
// keys; every field is carried.
type seriesDigestProjection struct {
	SeriesID                            string                       `json:"series_id"`
	Purpose                             SeriesPurpose                `json:"purpose"`
	State                               LifecycleState               `json:"state"`
	SkillSnapshotDigest                 string                       `json:"skill_snapshot_digest"`
	SkillSnapshotAnchorDigest           string                       `json:"skill_snapshot_anchor_digest"`
	SkillVersion                        string                       `json:"skill_version"`
	SkillDigest                         string                       `json:"skill_digest"`
	SkillPackageValidationReceiptDigest string                       `json:"skill_package_validation_receipt_digest"`
	GreenTestReceiptDigest              string                       `json:"green_test_receipt_digest"`
	SeriesPrepareIdentityDigest         string                       `json:"series_prepare_identity_digest"`
	RunnerRevision                      string                       `json:"runner_revision"`
	RunnerDigest                        string                       `json:"runner_digest"`
	JudgeRuleDigest                     string                       `json:"judge_rule_digest"`
	CoreExecutionPlanDigest             string                       `json:"core_execution_plan_digest"`
	DatasetManifests                    map[string]string            `json:"dataset_manifests"`
	Hosts                               []string                     `json:"hosts"`
	RequiredOrdinals                    []int                        `json:"required_ordinals"`
	TimeoutSeconds                      int                          `json:"timeout_seconds"`
	Concurrency                         int                          `json:"concurrency"`
	ExecutionEnvironmentDigest          string                       `json:"execution_environment_digest"`
	ToolConfigurationDigest             string                       `json:"tool_configuration_digest"`
	ProtectedExecutionPolicyDigest      string                       `json:"protected_execution_policy_digest"`
	CaseOrderSeeds                      map[string]string            `json:"case_order_seeds"`
	QuestionCount                       map[string]int               `json:"question_count"`
	CandidateBindingDigest              string                       `json:"candidate_binding_digest"`
	ProtectedExecutionReceiptDigest     string                       `json:"protected_execution_receipt_digest"`
	WorkspaceCanaryReceiptDigests       map[string]map[string]string `json:"workspace_canary_receipt_digests"`
	ManifestDigest                      string                       `json:"manifest_digest"`
}

func seriesDigestPreimage(m *FormalSeriesManifest) seriesDigestProjection {
	return seriesDigestProjection{
		SeriesID: m.SeriesID, Purpose: m.Purpose, State: m.State,
		SkillSnapshotDigest: m.SkillSnapshotDigest, SkillSnapshotAnchorDigest: m.SkillSnapshotAnchorDigest,
		SkillVersion: m.SkillVersion, SkillDigest: m.SkillDigest,
		SkillPackageValidationReceiptDigest: m.SkillPackageValidationReceiptDigest,
		GreenTestReceiptDigest:              m.GreenTestReceiptDigest,
		SeriesPrepareIdentityDigest:         m.SeriesPrepareIdentityDigest,
		RunnerRevision:                      m.RunnerRevision, RunnerDigest: m.RunnerDigest, JudgeRuleDigest: m.JudgeRuleDigest,
		CoreExecutionPlanDigest: m.CoreExecutionPlanDigest,
		DatasetManifests:        m.DatasetManifests,
		Hosts:                   m.Hosts,
		RequiredOrdinals:        m.RequiredOrdinals,
		TimeoutSeconds:          m.TimeoutSeconds, Concurrency: m.Concurrency,
		ExecutionEnvironmentDigest:      m.ExecutionEnvironmentDigest,
		ToolConfigurationDigest:         m.ToolConfigurationDigest,
		ProtectedExecutionPolicyDigest:  m.ProtectedExecutionPolicyDigest,
		CaseOrderSeeds:                  seedMapToStrings(m.CaseOrderSeeds),
		QuestionCount:                   m.QuestionCount,
		CandidateBindingDigest:          m.CandidateBindingDigest,
		ProtectedExecutionReceiptDigest: m.ProtectedExecutionReceiptDigest,
		WorkspaceCanaryReceiptDigests:   canaryMapToStrings(m.WorkspaceCanaryReceiptDigests),
		ManifestDigest:                  m.ManifestDigest,
	}
}

func seriesManifestDigest(m *FormalSeriesManifest) (string, error) {
	saved := m.ManifestDigest
	m.ManifestDigest = ""
	d, err := CanonicalSHA256(seriesDigestPreimage(m))
	m.ManifestDigest = saved
	return d, err
}

// VerifySeriesManifest re-derives the manifest digest; post-seal mutation
// fails closed.
func VerifySeriesManifest(m *FormalSeriesManifest) error {
	if m == nil {
		return errors.New("nil series manifest")
	}
	want, err := seriesManifestDigest(m)
	if err != nil {
		return err
	}
	if want != m.ManifestDigest {
		return fmt.Errorf("series manifest digest %s != recomputed %s (post-seal mutation)", m.ManifestDigest, want)
	}
	return nil
}

// LoadSeriesManifest strictly parses, validates and re-verifies a sealed
// series manifest.
func LoadSeriesManifest(path string) (*FormalSeriesManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m FormalSeriesManifest
	if err := StrictParseClosed(b, &m); err != nil {
		return nil, fmt.Errorf("series manifest %s: %w", path, err)
	}
	if err := ValidateFormalSeriesManifest(&m); err != nil {
		return nil, fmt.Errorf("series manifest %s: %w", path, err)
	}
	if err := VerifySeriesManifest(&m); err != nil {
		return nil, fmt.Errorf("series manifest %s: %w", path, err)
	}
	return &m, nil
}

// ProtectedExecutionPolicyDigest is the stable official-dual policy identity
// projected from a protected execution receipt: boundary/config/worker/
// template only. Unique roots, state roots, split allocators, the probe
// matrix, the probe instant and the receipt digest are excluded, so a
// recovery series with fresh roots recomputes the same policy digest and
// therefore the same candidate binding.
func ProtectedExecutionPolicyDigest(r *ProtectedExecutionReceipt) (string, error) {
	if r == nil {
		return "", errors.New("nil protected execution receipt")
	}
	proj := struct {
		BoundaryKind                          BoundaryKind `json:"boundary_kind"`
		IsolationConfigDigest                 string       `json:"isolation_config_digest"`
		WorkerIdentitySetDigest               string       `json:"worker_identity_set_digest"`
		NormalizedCoreWorkerIdentitySetDigest string       `json:"normalized_core_worker_identity_set_digest"`
		ExecutionTemplateSetDigest            string       `json:"execution_template_set_digest"`
		RequiredConcurrency                   int          `json:"required_concurrency"`
		IsolatedWorkerCapacity                int          `json:"isolated_worker_capacity"`
	}{
		BoundaryKind:                          r.BoundaryKind,
		IsolationConfigDigest:                 r.IsolationConfigDigest,
		WorkerIdentitySetDigest:               r.WorkerIdentitySetDigest,
		NormalizedCoreWorkerIdentitySetDigest: r.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            r.ExecutionTemplateSetDigest,
		RequiredConcurrency:                   r.RequiredConcurrency,
		IsolatedWorkerCapacity:                r.IsolatedWorkerCapacity,
	}
	return CanonicalSHA256(proj)
}

// prepareProbeMatrix binds the carried probes to the plan's execution
// template and to their own canonical digest.
func prepareProbeMatrix(r *ProtectedExecutionReceipt, plan *CoreExecutionPlanReceipt) error {
	for _, p := range r.WorkerProbes {
		if p.ExecutionTemplateDigest != plan.NormalizedCoreExecutionTemplateDigest {
			return fmt.Errorf("worker probe %s/%d execution template drifts from the core plan", p.Host, p.WorkerSlot)
		}
	}
	digest, err := ProbeMatrixDigestV1(r.WorkerProbes)
	if err != nil {
		return err
	}
	if digest != r.ProbeMatrixDigest {
		return errors.New("probe matrix digest does not match the carried probes")
	}
	return nil
}

// prepareCanaryCoverage requires one passing, identity-matching canary per
// executable host × prepared worker slot and rejects receipts naming slots
// outside the prepared set.
func prepareCanaryCoverage(in map[string]map[int]*WorkspaceCanaryReceipt, m *FormalSeriesManifest, plan *CoreExecutionPlanReceipt) (map[string]map[int]string, error) {
	out := map[string]map[int]string{}
	for _, h := range plan.Hosts {
		for slot := 1; slot <= plan.Concurrency; slot++ {
			c := in[h][slot]
			if c == nil {
				return nil, fmt.Errorf("workspace canary receipt missing for %s worker slot %d", h, slot)
			}
			if err := ValidateWorkspaceCanary(c, m.SeriesID, m.SkillDigest,
				plan.ToolIdentityDigests[h], plan.NormalizedCoreExecutionTemplateDigest, slot); err != nil {
				return nil, fmt.Errorf("workspace canary %s/%d: %w", h, slot, err)
			}
			if out[h] == nil {
				out[h] = map[int]string{}
			}
			out[h][slot] = c.ReceiptDigest
		}
	}
	for h, slots := range in {
		if !stringInList(plan.Hosts, h) {
			return nil, fmt.Errorf("workspace canary names unknown host %q", h)
		}
		for slot := range slots {
			if slot < 1 || slot > plan.Concurrency {
				return nil, fmt.Errorf("workspace canary for %s names unprepared worker slot %d", h, slot)
			}
		}
	}
	return out, nil
}

// PrepareSeries runs the `series prepare` gate: it re-verifies the immutable
// snapshot, the exact-skill package receipt and the matching series-prepare
// green receipt, then checks the current runner/judge, per-host tool
// identities, timeout/concurrency, core manifest and case-order seeds against
// the referenced plan, binds the protected execution + canary receipts for an
// official-dual series, recomputes the stable candidate binding and seals the
// manifest freeze-before-digest into a frozen file.
func PrepareSeries(root string, in SeriesPrepareInput) (*FormalSeriesManifest, error) {
	if in.SeriesID == "" {
		return nil, errors.New("series_id empty")
	}
	switch in.Purpose {
	case PurposeOfficialDual, PurposeDevComparison:
	default:
		return nil, fmt.Errorf("purpose %q invalid", in.Purpose)
	}
	plan, err := LoadCoreExecutionPlan(in.CorePlanPath)
	if err != nil {
		return nil, fmt.Errorf("core execution plan: %w", err)
	}
	// Execution conditions are copied from the plan, never re-decided.
	if in.TimeoutSeconds <= 0 || in.Concurrency <= 0 {
		return nil, errors.New("series prepare must state timeout and concurrency")
	}
	if in.TimeoutSeconds != plan.TimeoutSeconds {
		return nil, fmt.Errorf("series timeout %ds != sealed core plan %ds: a new plan is required", in.TimeoutSeconds, plan.TimeoutSeconds)
	}
	if in.Concurrency != plan.Concurrency {
		return nil, fmt.Errorf("series concurrency %d != sealed core plan %d: a new plan is required", in.Concurrency, plan.Concurrency)
	}
	if len(in.CaseOrderSeeds) != 3 {
		return nil, errors.New("case_order_seeds must be copied from the core plan, not regenerated")
	}
	for _, o := range Ordinals {
		if in.CaseOrderSeeds[o] == "" || in.CaseOrderSeeds[o] != plan.CaseOrderSeeds[o] {
			return nil, fmt.Errorf("case_order_seed[%d] is not the core plan's registered seed", o)
		}
	}
	if in.RunnerDigest == "" || in.JudgeRuleDigest == "" {
		return nil, errors.New("series prepare must bind runner and judge digests")
	}
	if in.RunnerDigest != plan.RunnerDigest || in.JudgeRuleDigest != plan.JudgeRuleDigest {
		return nil, errors.New("runner/judge drifted from the sealed core plan: a new plan is required")
	}
	if in.ToolConfigurationDigest == "" {
		return nil, errors.New("tool_configuration_digest empty")
	}
	if in.ExecutionEnvironmentDigest != plan.NormalizedCoreExecutionTemplateDigest {
		return nil, errors.New("execution_environment_digest must equal the plan's normalized core execution template")
	}
	for _, h := range plan.Hosts {
		got := in.ToolIdentityDigests[h]
		if got == "" {
			return nil, fmt.Errorf("no measured tool identity for %s", h)
		}
		if got != plan.ToolIdentityDigests[h] {
			return nil, fmt.Errorf("measured tool identity for %s drifted from the sealed core plan", h)
		}
	}
	// Exact-skill package validation receipt (unique producer, exact snapshot).
	pv, err := LoadPackageValidationReceipt(in.PackageValidationReceiptPath)
	if err != nil {
		return nil, fmt.Errorf("package validation receipt: %w", err)
	}
	if err := VerifyPackageValidationReceipt(pv, in.SnapshotRoot, in.ValidatorPath); err != nil {
		return nil, fmt.Errorf("package validation receipt: %w", err)
	}
	// Matching series-prepare green receipt (digest-current, stable identity).
	green, err := LoadGreenTestReceipt(in.GreenTestReceiptPath)
	if err != nil {
		return nil, fmt.Errorf("series-prepare green receipt: %w", err)
	}
	snapshotDigest, pvDigest := pv.SnapshotDigest, pv.ReceiptDigest
	if err := VerifyGreenTestReceipt(green, SuiteSeriesPrepare, in.ValidatorPath, GreenBindings{
		SnapshotDigest: &snapshotDigest, PackageValidationReceiptDigest: &pvDigest,
	}); err != nil {
		return nil, fmt.Errorf("series-prepare green receipt: %w", err)
	}
	if green.RunnerDigest != in.RunnerDigest || green.JudgeRuleDigest != in.JudgeRuleDigest {
		return nil, errors.New("series-prepare green receipt does not bind this runner/judge")
	}
	if green.StableIdentityDigest == nil || *green.StableIdentityDigest == "" {
		return nil, errors.New("series-prepare green receipt carries no stable identity")
	}
	// Datasets: the core manifest is exactly the plan's.
	coreDigest, err := coreManifestBinding(in.CoreManifestPath)
	if err != nil {
		return nil, err
	}
	if coreDigest != plan.CoreManifestDigest {
		return nil, errors.New("core manifest is not the one the sealed core plan binds")
	}
	datasets := map[string]string{MembershipCore172: coreDigest}
	if in.Purpose == PurposeOfficialDual {
		hold, err := formalDatasetBinding(in.HoldoutManifestPath, MembershipHoldout96)
		if err != nil {
			return nil, err
		}
		datasets[MembershipHoldout96] = hold.Digest
	}
	m := &FormalSeriesManifest{
		SeriesID:                            in.SeriesID,
		Purpose:                             in.Purpose,
		State:                               StateSealed,
		SkillSnapshotDigest:                 pv.SnapshotDigest,
		SkillSnapshotAnchorDigest:           pv.SnapshotAnchorDigest,
		SkillVersion:                        pv.SkillVersion,
		SkillDigest:                         pv.SkillDigest,
		SkillPackageValidationReceiptDigest: pv.ReceiptDigest,
		GreenTestReceiptDigest:              green.ReceiptDigest,
		SeriesPrepareIdentityDigest:         *green.StableIdentityDigest,
		RunnerRevision:                      plan.RunnerRevision,
		RunnerDigest:                        in.RunnerDigest,
		JudgeRuleDigest:                     in.JudgeRuleDigest,
		CoreExecutionPlanDigest:             plan.ReceiptDigest,
		DatasetManifests:                    datasets,
		Hosts:                               append([]string(nil), plan.Hosts...),
		RequiredOrdinals:                    []int{1, 2, 3},
		TimeoutSeconds:                      in.TimeoutSeconds,
		Concurrency:                         in.Concurrency,
		ExecutionEnvironmentDigest:          in.ExecutionEnvironmentDigest,
		ToolConfigurationDigest:             in.ToolConfigurationDigest,
		CaseOrderSeeds:                      copySeedMap(in.CaseOrderSeeds),
		QuestionCount:                       map[string]int{},
		WorkspaceCanaryReceiptDigests:       map[string]map[int]string{},
	}
	for mem := range datasets {
		n, err := ExpectedQuestionCount(mem)
		if err != nil {
			return nil, err
		}
		m.QuestionCount[mem] = n
	}
	// Protected execution + stable candidate binding (official-dual only).
	if in.Purpose == PurposeOfficialDual {
		if in.Protected == nil {
			return nil, errors.New("official-dual series prepare requires the protected execution receipt: fail-closed")
		}
		if err := ValidateProtectedExecutionReceipt(in.Protected, plan); err != nil {
			return nil, fmt.Errorf("protected execution receipt: %w", err)
		}
		if in.Protected.ExecutionTemplateSetDigest != plan.NormalizedCoreExecutionTemplateDigest {
			return nil, errors.New("protected receipt execution template set drifts from the core plan")
		}
		if err := prepareProbeMatrix(in.Protected, plan); err != nil {
			return nil, fmt.Errorf("protected execution receipt: %w", err)
		}
		policy, err := ProtectedExecutionPolicyDigest(in.Protected)
		if err != nil {
			return nil, err
		}
		m.ProtectedExecutionPolicyDigest = policy
		m.ProtectedExecutionReceiptDigest = in.Protected.ReceiptDigest
		cbDigest, err := CandidateBindingDigest(&CandidateBindingV1{
			SchemaVersion:                       1,
			Purpose:                             in.Purpose,
			SkillSnapshotDigest:                 pv.SnapshotDigest,
			SkillSnapshotAnchorDigest:           pv.SnapshotAnchorDigest,
			SkillDigest:                         pv.SkillDigest,
			SkillPackageValidationReceiptDigest: pv.ReceiptDigest,
			ValidatorRevision:                   pv.ValidatorRevision,
			ValidatorDigest:                     pv.ValidatorDigest,
			RunnerRevision:                      plan.RunnerRevision,
			RunnerDigest:                        in.RunnerDigest,
			JudgeRuleDigest:                     in.JudgeRuleDigest,
			DatasetIdentities:                   datasets,
			CoreExecutionPlanDigest:             plan.ReceiptDigest,
			ToolIdentityDigests:                 copyStringMap(plan.ToolIdentityDigests),
			ToolConfigurationDigest:             in.ToolConfigurationDigest,
			TimeoutSeconds:                      in.TimeoutSeconds,
			Concurrency:                         in.Concurrency,
			CaseOrderSeeds:                      copySeedMap(in.CaseOrderSeeds),
			ExecutionEnvironmentDigest:          in.ExecutionEnvironmentDigest,
			ProtectedExecutionPolicyDigest:      policy,
			SeriesPrepareIdentityDigest:         *green.StableIdentityDigest,
		})
		if err != nil {
			return nil, err
		}
		m.CandidateBindingDigest = cbDigest
	} else if in.Protected != nil {
		return nil, errors.New("dev-comparison series prepare must not carry a protected execution receipt")
	}
	// Workspace canaries: mandatory exactly when a bound split stages files.
	if in.StagedWorkspaceFiles {
		canaries, err := prepareCanaryCoverage(in.Canaries, m, plan)
		if err != nil {
			return nil, err
		}
		m.WorkspaceCanaryReceiptDigests = canaries
	} else if len(in.Canaries) > 0 {
		return nil, errors.New("no bound split stages workspace files: the canary map must be empty")
	}
	if err := validateUnsealedSeries(m); err != nil {
		return nil, fmt.Errorf("series manifest is not complete: %w", err)
	}
	if err := FreezeBeforeDigest(&m.ManifestDigest); err != nil {
		return nil, err
	}
	digest, err := seriesManifestDigest(m)
	if err != nil {
		return nil, err
	}
	m.ManifestDigest = digest
	out := in.OutPath
	if out == "" {
		out = filepath.Join(root, "series", in.SeriesID, "series-manifest.json")
	}
	if err := WriteFrozenFile(out, mustCanonicalJSON(seriesDigestPreimage(m))); err != nil {
		return nil, err
	}
	return m, nil
}

// ---------- §9 sealed primary run manifest ----------

// caseOrderDigest receipts the ordered case sequence (order kept), as opposed
// to CaseIDsDigest's sorted set receipt.
func caseOrderDigest(ids []string) (string, error) {
	return CanonicalSHA256(ids)
}

// validatePrimaryRun is the closed primary-run semantics. Sealed runs must
// carry both self digests; an in-progress run is validated identically before
// they are computed.
func validatePrimaryRun(r *PrimaryRunManifest, sealed bool) error {
	if r == nil {
		return errors.New("nil primary run manifest")
	}
	if r.Mode != "primary" {
		return fmt.Errorf("run mode %q is not primary", r.Mode)
	}
	if r.SeriesID == "" {
		return errors.New("primary run series_id empty")
	}
	if !validHosts[r.Host] {
		return fmt.Errorf("primary run host %q is not a formal host", r.Host)
	}
	membership, err := MembershipOfSplit(r.Split)
	if err != nil {
		return err
	}
	if !intInList(Ordinals[:], r.Ordinal) {
		return fmt.Errorf("primary run ordinal %d invalid", r.Ordinal)
	}
	if r.ToolProvenance.Host != r.Host {
		return fmt.Errorf("tool provenance host %q != run host %q", r.ToolProvenance.Host, r.Host)
	}
	if r.ToolProvenance.ToolIdentityDigest == "" || r.ToolProvenance.CapturedAt == "" {
		return errors.New("primary run tool provenance incomplete")
	}
	expect, err := ExpectedQuestionCount(membership)
	if err != nil {
		return err
	}
	if r.ExpectedCaseCount != expect {
		return fmt.Errorf("primary run expected_case_count %d, want %d", r.ExpectedCaseCount, expect)
	}
	if len(r.CaseIDs) != expect || len(r.CaseOrder) != expect {
		return fmt.Errorf("primary run carries %d case ids / %d ordered, want %d", len(r.CaseIDs), len(r.CaseOrder), expect)
	}
	set := map[string]bool{}
	for _, id := range r.CaseIDs {
		if id == "" {
			return errors.New("primary run carries an empty case id")
		}
		if set[id] {
			return fmt.Errorf("primary run repeats case %s", id)
		}
		set[id] = true
	}
	for _, id := range r.CaseOrder {
		if !set[id] {
			return fmt.Errorf("case_order names unknown case %s", id)
		}
	}
	if r.CaseSetDigest == "" || r.CaseOrderDigest == "" {
		return errors.New("primary run case digests missing")
	}
	if setDigest, err := CaseIDsDigest(r.CaseIDs); err != nil || setDigest != r.CaseSetDigest {
		return fmt.Errorf("primary run case_set_digest mismatch")
	}
	if orderDigest, err := caseOrderDigest(r.CaseOrder); err != nil || orderDigest != r.CaseOrderDigest {
		return fmt.Errorf("primary run case_order_digest mismatch")
	}
	started, err := time.Parse(time.RFC3339, r.StartedAt)
	if err != nil {
		return fmt.Errorf("primary run started_at %q is not RFC3339 UTC", r.StartedAt)
	}
	completed, err := time.Parse(time.RFC3339, r.CompletedAt)
	if err != nil {
		return fmt.Errorf("primary run completed_at %q is not RFC3339 UTC", r.CompletedAt)
	}
	if completed.Before(started) {
		return errors.New("primary run completed before it started")
	}
	if r.State != StateComplete {
		return fmt.Errorf("primary run state %q is not complete: a partial run is never materialized", r.State)
	}
	if !sealed {
		return nil
	}
	if r.RunDigest == "" || r.SealDigest == "" {
		return errors.New("primary run is not sealed")
	}
	return nil
}

func primaryRunDigest(r *PrimaryRunManifest) (string, error) {
	savedR, savedS := r.RunDigest, r.SealDigest
	r.RunDigest, r.SealDigest = "", ""
	d, err := CanonicalSHA256(r)
	r.RunDigest, r.SealDigest = savedR, savedS
	return d, err
}

func primaryRunSealDigest(r *PrimaryRunManifest) (string, error) {
	saved := r.SealDigest
	r.SealDigest = ""
	d, err := CanonicalSHA256(r)
	r.SealDigest = saved
	return d, err
}

// VerifyPrimaryRun re-derives both self digests of a sealed run.
func VerifyPrimaryRun(r *PrimaryRunManifest) error {
	if r == nil {
		return errors.New("nil primary run manifest")
	}
	wantRun, err := primaryRunDigest(r)
	if err != nil {
		return err
	}
	if wantRun != r.RunDigest {
		return fmt.Errorf("primary run digest %s != recomputed %s (post-seal mutation)", r.RunDigest, wantRun)
	}
	wantSeal, err := primaryRunSealDigest(r)
	if err != nil {
		return err
	}
	if wantSeal != r.SealDigest {
		return errors.New("primary run seal digest mismatch (post-seal mutation)")
	}
	return nil
}

// PrimaryRunInput carries one sealed primary run of a series. only/sample/
// limit have no field here by construction: a partial run is never
// materialized.
type PrimaryRunInput struct {
	Root           string
	SeriesID       string
	Host           string
	Split          string
	Ordinal        int
	Plan           *CoreExecutionPlanReceipt
	ToolProvenance ToolProvenance
	CaseIDs        []string
	CaseOrder      []string
	StartedAt      string // empty → now
	CompletedAt    string // empty → now
	OutPath        string // empty → <root>/primary/<series_id>/<host>-<split>-o<ordinal>.json
}

// SealPrimaryRun seals one primary run receipt: identity checks against the
// referenced plan (unique series × host × split × ordinal key, tool identity
// equality, exact case count), then run/seal digests computed
// freeze-before-digest into a frozen file.
func SealPrimaryRun(in PrimaryRunInput) (*PrimaryRunManifest, error) {
	if err := ValidateCoreExecutionPlan(in.Plan); err != nil {
		return nil, fmt.Errorf("core execution plan: %w", err)
	}
	if in.ToolProvenance.Host != in.Host {
		return nil, fmt.Errorf("tool provenance host %q != run host %q", in.ToolProvenance.Host, in.Host)
	}
	wantIdentity := in.Plan.ToolIdentityDigests[in.Host]
	if wantIdentity == "" {
		return nil, fmt.Errorf("core plan binds no tool identity for %s", in.Host)
	}
	if in.ToolProvenance.ToolIdentityDigest != wantIdentity {
		return nil, fmt.Errorf("tool identity for %s drifted from the sealed core plan: a new plan is required", in.Host)
	}
	membership, err := MembershipOfSplit(in.Split)
	if err != nil {
		return nil, err
	}
	expect, err := ExpectedQuestionCount(membership)
	if err != nil {
		return nil, err
	}
	if len(in.CaseIDs) != expect || len(in.CaseOrder) != expect {
		return nil, fmt.Errorf("primary run carries %d case ids / %d ordered, want %d", len(in.CaseIDs), len(in.CaseOrder), expect)
	}
	caseSetDigest, err := CaseIDsDigest(in.CaseIDs)
	if err != nil {
		return nil, err
	}
	caseOrderDigestV, err := caseOrderDigest(in.CaseOrder)
	if err != nil {
		return nil, err
	}
	started, completed := in.StartedAt, in.CompletedAt
	if started == "" {
		started = nowRFC3339()
	}
	if completed == "" {
		completed = nowRFC3339()
	}
	r := &PrimaryRunManifest{
		Mode:              "primary",
		SeriesID:          in.SeriesID,
		Host:              in.Host,
		Split:             in.Split,
		Ordinal:           in.Ordinal,
		ToolProvenance:    in.ToolProvenance,
		CaseIDs:           append([]string(nil), in.CaseIDs...),
		CaseSetDigest:     caseSetDigest,
		CaseOrder:         append([]string(nil), in.CaseOrder...),
		CaseOrderDigest:   caseOrderDigestV,
		ExpectedCaseCount: expect,
		StartedAt:         started,
		CompletedAt:       completed,
		State:             StateComplete,
	}
	if err := validatePrimaryRun(r, false); err != nil {
		return nil, fmt.Errorf("primary run is not complete: %w", err)
	}
	if err := FreezeBeforeDigest(&r.RunDigest); err != nil {
		return nil, err
	}
	runDigest, err := primaryRunDigest(r)
	if err != nil {
		return nil, err
	}
	r.RunDigest = runDigest
	sealDigest, err := primaryRunSealDigest(r)
	if err != nil {
		return nil, err
	}
	r.SealDigest = sealDigest
	out := in.OutPath
	if out == "" {
		out = filepath.Join(in.Root, "primary", in.SeriesID,
			fmt.Sprintf("%s-%s-o%d.json", in.Host, in.Split, in.Ordinal))
	}
	if err := WriteFrozenFile(out, mustCanonicalJSON(r)); err != nil {
		return nil, err
	}
	return r, nil
}

// LoadPrimaryRun strictly parses, validates and re-verifies a sealed run.
func LoadPrimaryRun(path string) (*PrimaryRunManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r PrimaryRunManifest
	if err := StrictParseClosed(b, &r); err != nil {
		return nil, fmt.Errorf("primary run %s: %w", path, err)
	}
	if err := validatePrimaryRun(&r, true); err != nil {
		return nil, fmt.Errorf("primary run %s: %w", path, err)
	}
	if err := VerifyPrimaryRun(&r); err != nil {
		return nil, fmt.Errorf("primary run %s: %w", path, err)
	}
	return &r, nil
}

// CoreLegCompletionDigest is the receipt-set digest of a complete core172
// leg: exactly three hosts × three ordinals of sealed, complete
// dev-regression runs of one series, sorted for a canonical preimage.
func CoreLegCompletionDigest(runs []*PrimaryRunManifest) (string, error) {
	if len(runs) == 0 {
		return "", errors.New("core leg carries no primary run receipt")
	}
	type entry struct {
		Host       string `json:"host"`
		Ordinal    int    `json:"ordinal"`
		RunDigest  string `json:"run_digest"`
		SealDigest string `json:"seal_digest"`
	}
	proj := struct {
		Algorithm string  `json:"algorithm"`
		SeriesID  string  `json:"series_id"`
		Runs      []entry `json:"runs"`
	}{Algorithm: "engram-core-leg-completion-v1", SeriesID: runs[0].SeriesID}
	seen := map[string]bool{}
	for _, r := range runs {
		if r == nil {
			return "", errors.New("core leg carries a nil primary run receipt")
		}
		if err := validatePrimaryRun(r, true); err != nil {
			return "", err
		}
		if r.Split != SplitDevRegression {
			return "", fmt.Errorf("core leg run %s/%d is not core172", r.Host, r.Ordinal)
		}
		if r.SeriesID != proj.SeriesID {
			return "", fmt.Errorf("core leg mixes series %q and %q", proj.SeriesID, r.SeriesID)
		}
		key := r.Host + "/" + strconv.Itoa(r.Ordinal)
		if seen[key] {
			return "", fmt.Errorf("core leg repeats %s", key)
		}
		seen[key] = true
		proj.Runs = append(proj.Runs, entry{Host: r.Host, Ordinal: r.Ordinal, RunDigest: r.RunDigest, SealDigest: r.SealDigest})
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		for _, o := range Ordinals {
			if !seen[h+"/"+strconv.Itoa(o)] {
				return "", fmt.Errorf("core leg is missing %s ordinal %d", h, o)
			}
		}
	}
	sort.Slice(proj.Runs, func(i, j int) bool {
		if proj.Runs[i].Host != proj.Runs[j].Host {
			return proj.Runs[i].Host < proj.Runs[j].Host
		}
		return proj.Runs[i].Ordinal < proj.Runs[j].Ordinal
	})
	return CanonicalSHA256(proj)
}

func loadCoreLeg(paths []string, seriesID string) ([]*PrimaryRunManifest, string, error) {
	if len(paths) == 0 {
		return nil, "", errors.New("no core-leg primary run receipt given")
	}
	runs := make([]*PrimaryRunManifest, 0, len(paths))
	for _, p := range paths {
		r, err := LoadPrimaryRun(p)
		if err != nil {
			return nil, "", err
		}
		if r.SeriesID != seriesID {
			return nil, "", fmt.Errorf("core-leg run %s belongs to series %q, want %q: cross-series splice", p, r.SeriesID, seriesID)
		}
		runs = append(runs, r)
	}
	digest, err := CoreLegCompletionDigest(runs)
	if err != nil {
		return nil, "", err
	}
	return runs, digest, nil
}

// ---------- §7 holdout binding receipt ----------

// holdoutBindingMu serializes binding mutations: read-then-append from two
// concurrent slots would interleave and break the chain.
var holdoutBindingMu sync.Mutex

func holdoutBindingDigest(b *HoldoutBindingReceipt) (string, error) {
	saved := b.ReceiptDigest
	b.ReceiptDigest = ""
	d, err := CanonicalSHA256(b)
	b.ReceiptDigest = saved
	return d, err
}

// sealHoldoutBinding computes the chained receipt digest freeze-before-digest.
func sealHoldoutBinding(b *HoldoutBindingReceipt) error {
	if err := FreezeBeforeDigest(&b.ReceiptDigest); err != nil {
		return err
	}
	digest, err := holdoutBindingDigest(b)
	if err != nil {
		return err
	}
	b.ReceiptDigest = digest
	return nil
}

// LoadHoldoutBinding strictly parses, validates and re-verifies a binding.
func LoadHoldoutBinding(path string) (*HoldoutBindingReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r HoldoutBindingReceipt
	if err := StrictParseClosed(b, &r); err != nil {
		return nil, fmt.Errorf("holdout binding %s: %w", path, err)
	}
	if err := ValidateHoldoutBinding(&r); err != nil {
		return nil, fmt.Errorf("holdout binding %s: %w", path, err)
	}
	want, err := holdoutBindingDigest(&r)
	if err != nil {
		return nil, err
	}
	if want != r.ReceiptDigest {
		return nil, fmt.Errorf("holdout binding %s digest mismatch (post-hoc mutation)", path)
	}
	return &r, nil
}

// writeBindingAtomic keeps every superseded version on disk (the current
// receipt is frozen as v<N> before the replacement lands) and installs the
// next receipt by rename, so an append is never a silent rewrite.
func writeBindingAtomic(path string, cur, next *HoldoutBindingReceipt) error {
	nextJSON, err := CanonicalJSON(next)
	if err != nil {
		return err
	}
	history := fmt.Sprintf("%s.v%d.json", strings.TrimSuffix(path, ".json"), len(cur.SeriesAttempts))
	if err := WriteFrozenFile(history, mustCanonicalJSON(cur)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, nextJSON, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// verifyPreHoldoutAttestation re-verifies the fresh pre-holdout green receipt
// of exactly this series: suite, current implement digests, snapshot/package
// bindings, exact series manifest, the stable candidate digest, the complete
// core-leg receipt set, and a creation instant after every core-leg run
// completed (it is re-run after a complete core leg, never before).
func verifyPreHoldoutAttestation(path, validatorPath string, m *FormalSeriesManifest, coreLeg string, coreRuns []*PrimaryRunManifest) (*GreenTestReceipt, error) {
	green, err := LoadGreenTestReceipt(path)
	if err != nil {
		return nil, fmt.Errorf("pre-holdout green receipt: %w", err)
	}
	snapshotDigest, pvDigest := m.SkillSnapshotDigest, m.SkillPackageValidationReceiptDigest
	manifestDigest, candidateDigest := m.ManifestDigest, m.CandidateBindingDigest
	if err := VerifyGreenTestReceipt(green, SuitePreHoldout, validatorPath, GreenBindings{
		SnapshotDigest:                 &snapshotDigest,
		PackageValidationReceiptDigest: &pvDigest,
		SeriesManifestDigest:           &manifestDigest,
		CandidateBindingDigest:         &candidateDigest,
		CoreLegCompletionDigest:        &coreLeg,
	}); err != nil {
		return nil, fmt.Errorf("pre-holdout green receipt: %w", err)
	}
	if green.RunnerDigest != m.RunnerDigest || green.JudgeRuleDigest != m.JudgeRuleDigest {
		return nil, errors.New("pre-holdout green receipt does not bind this series' runner/judge")
	}
	created, err := time.Parse(time.RFC3339, green.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("pre-holdout green receipt created_at %q is not RFC3339", green.CreatedAt)
	}
	for _, r := range coreRuns {
		done, err := time.Parse(time.RFC3339, r.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("core-leg run %s/%d completed_at %q is not RFC3339", r.Host, r.Ordinal, r.CompletedAt)
		}
		if created.Before(done) {
			return nil, fmt.Errorf("pre-holdout receipt predates the core-leg completion of %s ordinal %d", r.Host, r.Ordinal)
		}
	}
	return green, nil
}

// HoldoutBindInput binds the first attempt of one holdout version.
type HoldoutBindInput struct {
	DatasetManifestPath     string
	SeriesManifestPath      string
	CoreLegRunPaths         []string
	CoreLegCompletionDigest string
	PreHoldoutReceiptPath   string
	ValidatorPath           string
	StartedAt               string // empty → now
	OutPath                 string // empty → alongside the series manifest
}

// BindHoldout performs the atomic first binding: the core leg must be
// complete, the fresh pre-holdout attestation must bind the exact manifest,
// the same stable candidate digest and the complete core-leg receipt set, and
// no binding may already exist for this holdout version. A series whose core
// leg is invalid never creates a binding (core-invalid-before-holdout).
func BindHoldout(root string, in HoldoutBindInput) (*HoldoutBindingReceipt, error) {
	holdoutBindingMu.Lock()
	defer holdoutBindingMu.Unlock()
	m, err := LoadSeriesManifest(in.SeriesManifestPath)
	if err != nil {
		return nil, err
	}
	if m.Purpose != PurposeOfficialDual {
		return nil, fmt.Errorf("purpose %q never binds the holdout", m.Purpose)
	}
	switch m.State {
	case StateSealed, StateComplete:
	default:
		return nil, fmt.Errorf("series state %q: core-invalid-before-holdout forbids creating a binding", m.State)
	}
	if m.CandidateBindingDigest == "" {
		return nil, errors.New("series carries no stable candidate binding digest")
	}
	coreRuns, coreLeg, err := loadCoreLeg(in.CoreLegRunPaths, m.SeriesID)
	if err != nil {
		return nil, err
	}
	if in.CoreLegCompletionDigest != coreLeg {
		return nil, fmt.Errorf("core-leg completion digest %q != recomputed %q", in.CoreLegCompletionDigest, coreLeg)
	}
	green, err := verifyPreHoldoutAttestation(in.PreHoldoutReceiptPath, in.ValidatorPath, m, coreLeg, coreRuns)
	if err != nil {
		return nil, err
	}
	hold, err := formalDatasetBinding(in.DatasetManifestPath, MembershipHoldout96)
	if err != nil {
		return nil, err
	}
	out := in.OutPath
	if out == "" {
		out = filepath.Join(filepath.Dir(in.SeriesManifestPath), "holdout-binding.json")
	}
	if _, err := osStat(out); err == nil {
		return nil, fmt.Errorf("holdout binding %s already exists: first binding only (use AppendHoldoutAttempt)", out)
	}
	started := in.StartedAt
	if started == "" {
		started = nowRFC3339()
	}
	b := &HoldoutBindingReceipt{
		DatasetVersion:         hold.Version,
		DatasetManifestDigest:  hold.Digest,
		CandidateBindingDigest: m.CandidateBindingDigest,
		FirstPrimaryStartedAt:  started,
		SeriesAttempts: []HoldoutSeriesAttempt{{
			SeriesID:                         m.SeriesID,
			SeriesManifestDigest:             m.ManifestDigest,
			CandidateBindingDigestSelf:       m.CandidateBindingDigest,
			PreHoldoutGreenTestReceiptDigest: green.ReceiptDigest,
			CoreLegCompletionDigest:          coreLeg,
			StartedAt:                        started,
			State:                            "started",
		}},
		State: "frozen",
	}
	if err := sealHoldoutBinding(b); err != nil {
		return nil, err
	}
	if err := ValidateHoldoutBinding(b); err != nil {
		return nil, err
	}
	if err := WriteFrozenFile(out, mustCanonicalJSON(b)); err != nil {
		return nil, err
	}
	return b, nil
}

// HoldoutAppendInput appends a recovery attempt to an existing binding.
type HoldoutAppendInput struct {
	BindingPath             string
	SeriesManifestPath      string
	CoreLegRunPaths         []string
	CoreLegCompletionDigest string
	PreHoldoutReceiptPath   string
	ValidatorPath           string
	StartedAt               string // empty → now
}

// AppendHoldoutAttempt appends one recovery attempt to an existing frozen
// binding. The recovery series must recompute the SAME stable candidate
// digest with a NEW series manifest digest, a fresh pre-holdout receipt and a
// fresh core-leg completion; any cross-digest reuse is refused.
func AppendHoldoutAttempt(root string, in HoldoutAppendInput) (*HoldoutBindingReceipt, error) {
	holdoutBindingMu.Lock()
	defer holdoutBindingMu.Unlock()
	cur, err := LoadHoldoutBinding(in.BindingPath)
	if err != nil {
		return nil, err
	}
	if cur.State != "frozen" {
		return nil, fmt.Errorf("holdout binding is %q: no further attempt may be appended", cur.State)
	}
	m, err := LoadSeriesManifest(in.SeriesManifestPath)
	if err != nil {
		return nil, err
	}
	if m.Purpose != PurposeOfficialDual {
		return nil, fmt.Errorf("purpose %q never binds the holdout", m.Purpose)
	}
	switch m.State {
	case StateSealed, StateComplete:
	default:
		return nil, fmt.Errorf("series state %q: recovery binds only a live series", m.State)
	}
	if m.CandidateBindingDigest != cur.CandidateBindingDigest {
		return nil, fmt.Errorf("recovery series recomputed candidate binding %q, binding holds %q: a new holdout version is required",
			m.CandidateBindingDigest, cur.CandidateBindingDigest)
	}
	coreRuns, coreLeg, err := loadCoreLeg(in.CoreLegRunPaths, m.SeriesID)
	if err != nil {
		return nil, err
	}
	if in.CoreLegCompletionDigest != coreLeg {
		return nil, fmt.Errorf("core-leg completion digest %q != recomputed %q", in.CoreLegCompletionDigest, coreLeg)
	}
	green, err := verifyPreHoldoutAttestation(in.PreHoldoutReceiptPath, in.ValidatorPath, m, coreLeg, coreRuns)
	if err != nil {
		return nil, err
	}
	for i, a := range cur.SeriesAttempts {
		if a.SeriesManifestDigest == m.ManifestDigest {
			return nil, fmt.Errorf("attempt %d already carries this series manifest digest", i)
		}
		if a.PreHoldoutGreenTestReceiptDigest == green.ReceiptDigest {
			return nil, fmt.Errorf("attempt %d already carries this pre-holdout receipt: cross-series reuse", i)
		}
		if a.CoreLegCompletionDigest == coreLeg {
			return nil, fmt.Errorf("attempt %d already carries this core-leg completion digest: cross-series reuse", i)
		}
	}
	started := in.StartedAt
	if started == "" {
		started = nowRFC3339()
	}
	next := *cur
	next.ReceiptDigest = ""
	next.SeriesAttempts = append(append([]HoldoutSeriesAttempt{}, cur.SeriesAttempts...), HoldoutSeriesAttempt{
		SeriesID:                         m.SeriesID,
		SeriesManifestDigest:             m.ManifestDigest,
		CandidateBindingDigestSelf:       cur.CandidateBindingDigest,
		PreHoldoutGreenTestReceiptDigest: green.ReceiptDigest,
		CoreLegCompletionDigest:          coreLeg,
		StartedAt:                        started,
		State:                            "started",
	})
	next.PreviousReceiptDigest = cur.ReceiptDigest
	next.ConsumedBySeries = ""
	if err := sealHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := ValidateHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := writeBindingAtomic(in.BindingPath, cur, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

// ConsumeHoldoutBinding consumes the holdout version on behalf of a series
// whose holdout leg is complete (either polarity). Only a complete series may
// consume; the binding leaves `frozen` exactly once.
func ConsumeHoldoutBinding(root string, bindingPath, seriesManifestPath, outcome string) (*HoldoutBindingReceipt, error) {
	holdoutBindingMu.Lock()
	defer holdoutBindingMu.Unlock()
	cur, err := LoadHoldoutBinding(bindingPath)
	if err != nil {
		return nil, err
	}
	if cur.State != "frozen" {
		return nil, fmt.Errorf("holdout binding is already %q by series %q", cur.State, cur.ConsumedBySeries)
	}
	m, err := LoadSeriesManifest(seriesManifestPath)
	if err != nil {
		return nil, err
	}
	if m.State != StateComplete {
		return nil, fmt.Errorf("series state %q: only a complete series may consume the holdout", m.State)
	}
	// The manifest re-seals when it leaves `sealed` for `complete`, so the
	// attempt entry may still carry the digest it was bound under. Identity is
	// pinned by the series id, the stable candidate digest and the holdout
	// dataset binding instead.
	if m.CandidateBindingDigest != cur.CandidateBindingDigest {
		return nil, errors.New("series carries a different stable candidate digest: not this holdout version")
	}
	if m.DatasetManifests[MembershipHoldout96] != cur.DatasetManifestDigest {
		return nil, errors.New("series binds a different holdout96 manifest: not this holdout version")
	}
	switch outcome {
	case "complete-pass", "complete-fail":
	default:
		return nil, fmt.Errorf("outcome %q is not a complete-series outcome", outcome)
	}
	idx := -1
	for i := range cur.SeriesAttempts {
		a := &cur.SeriesAttempts[i]
		if a.SeriesID != m.SeriesID {
			continue
		}
		if idx >= 0 {
			return nil, fmt.Errorf("series %s has more than one attempt entry", m.SeriesID)
		}
		if a.State != "started" {
			return nil, fmt.Errorf("series %s attempt is already %q", m.SeriesID, a.State)
		}
		idx = i
	}
	if idx < 0 {
		return nil, fmt.Errorf("binding carries no attempt entry for series %s", m.SeriesID)
	}
	next := *cur
	next.ReceiptDigest = ""
	next.SeriesAttempts = append([]HoldoutSeriesAttempt{}, cur.SeriesAttempts...)
	next.SeriesAttempts[idx].State = outcome
	next.SeriesAttempts[idx].TerminalAt = nowRFC3339()
	// The consumption event records the series' final manifest digest: the
	// manifest re-seals when it leaves `sealed` for `complete`, and the official
	// scorer later matches the scored attempt against exactly that digest.
	next.SeriesAttempts[idx].SeriesManifestDigest = m.ManifestDigest
	next.State = "consumed"
	next.ConsumedBySeries = m.SeriesID
	next.PreviousReceiptDigest = cur.ReceiptDigest
	if err := sealHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := ValidateHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := writeBindingAtomic(bindingPath, cur, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

// InvalidateHoldoutAttempt records a series whose core or holdout leg went
// invalid. The binding stays frozen: only the same stable candidate digest may
// recover it, through a brand-new series and a fresh pre-holdout attestation.
func InvalidateHoldoutAttempt(root string, bindingPath, seriesManifestPath string) (*HoldoutBindingReceipt, error) {
	holdoutBindingMu.Lock()
	defer holdoutBindingMu.Unlock()
	cur, err := LoadHoldoutBinding(bindingPath)
	if err != nil {
		return nil, err
	}
	if cur.State != "frozen" {
		return nil, fmt.Errorf("holdout binding is %q: an invalid series cannot be recorded", cur.State)
	}
	m, err := LoadSeriesManifest(seriesManifestPath)
	if err != nil {
		return nil, err
	}
	if m.State != StateInvalid {
		return nil, fmt.Errorf("series state %q is not invalid", m.State)
	}
	idx := -1
	for i := range cur.SeriesAttempts {
		if cur.SeriesAttempts[i].SeriesID != m.SeriesID {
			continue
		}
		if idx >= 0 {
			return nil, fmt.Errorf("series %s has more than one attempt entry", m.SeriesID)
		}
		if cur.SeriesAttempts[i].State != "started" {
			return nil, fmt.Errorf("series %s attempt is already %q", m.SeriesID, cur.SeriesAttempts[i].State)
		}
		idx = i
	}
	if idx < 0 {
		return nil, fmt.Errorf("binding carries no attempt entry for series %s", m.SeriesID)
	}
	next := *cur
	next.ReceiptDigest = ""
	next.SeriesAttempts = append([]HoldoutSeriesAttempt{}, cur.SeriesAttempts...)
	next.SeriesAttempts[idx].State = "invalid"
	next.SeriesAttempts[idx].TerminalAt = nowRFC3339()
	next.PreviousReceiptDigest = cur.ReceiptDigest
	if err := sealHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := ValidateHoldoutBinding(&next); err != nil {
		return nil, err
	}
	if err := writeBindingAtomic(bindingPath, cur, &next); err != nil {
		return nil, err
	}
	return &next, nil
}
