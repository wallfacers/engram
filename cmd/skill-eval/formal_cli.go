package main

// T051 command wiring for the formal series surface (runner-cli.md §1-§3):
// `core-plan create`, `series prepare`, `run --mode primary` and `score`.
// Everything here is assembly and fail-closed checking — the semantics live
// in the T045-T050 producers (manifest.go, runner_primary.go, report.go).
//
// A prepared series root is flat and reader-complete (the contract
// report.go's LoadScoreInputs reads):
//
//	<seriesRoot>/series-manifest.json
//	<seriesRoot>/core-plan.json
//	<seriesRoot>/protected-execution.json      (official-dual)
//	<seriesRoot>/package-validation.json
//	<seriesRoot>/green-series-prepare.json
//	<seriesRoot>/green-pre-holdout.json        (holdout ordinal 1 onward)
//	<seriesRoot>/holdout-binding.json          (holdout ordinal 1 onward)
//	<seriesRoot>/canaries/<host>/slot-<n>.json
//	<seriesRoot>/datasets/<membership>/...     (manifest + payload copies)
//	<seriesRoot>/runs/<host>-<split>-o<n>/...
//
// `failure-archive` and `compare` (US5 dev flywheel) are wired at the bottom
// of this file: both are dev-only, produce no official score and read no
// holdout path.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// formalLaneConfig assembles the frozen three-lane configuration from flags
// with the shared env fallbacks. A flag that is empty after the fallback
// fails the host template resolution later, never silently.
func formalLaneConfig(claudeSettings, codexProvider, codexModel, opencodeModel string) CLIReviewConfig {
	if claudeSettings == "" {
		claudeSettings = os.Getenv("ENGRAM_SKILL_EVAL_CLAUDE_SETTINGS")
	}
	if codexProvider == "" {
		codexProvider = os.Getenv("ENGRAM_SKILL_EVAL_CODEX_PROVIDER")
	}
	if codexModel == "" {
		codexModel = os.Getenv("ENGRAM_SKILL_EVAL_CODEX_MODEL")
	}
	if opencodeModel == "" {
		opencodeModel = os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL")
	}
	return CLIReviewConfig{
		Lanes:          []string{HostClaude, HostCodex, HostOpenCode},
		ClaudeSettings: claudeSettings,
		CodexProvider:  codexProvider,
		CodexModel:     codexModel,
		OpenCodeModel:  opencodeModel,
	}
}

// coreExecConfig is the closed schema of the operator-provided `--core-exec`
// boundary description: which isolation mechanism the formal children run
// under and a nonsecret description of its configuration (user, container
// image, mount policy…). Only digests of this file ever enter a receipt.
type coreExecConfig struct {
	BoundaryKind    BoundaryKind `json:"boundary_kind"`
	IsolationConfig string       `json:"isolation_config"`
}

// loadCoreExecConfig reads and validates the execution-boundary description
// and returns the boundary kind plus the file's byte digest (the
// isolation-config digest every boundary identity derives from).
func loadCoreExecConfig(path string) (BoundaryKind, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("core-exec config %s: %w", path, err)
	}
	var cfg coreExecConfig
	if err := StrictParseClosed(b, &cfg); err != nil {
		return "", "", fmt.Errorf("core-exec config %s: %w", path, err)
	}
	if !ValidBoundary(cfg.BoundaryKind) {
		return "", "", fmt.Errorf("core-exec config %s: boundary kind %q invalid", path, cfg.BoundaryKind)
	}
	if strings.TrimSpace(cfg.IsolationConfig) == "" {
		return "", "", fmt.Errorf("core-exec config %s: isolation_config must describe the operator boundary", path)
	}
	return cfg.BoundaryKind, sha256Hex(b), nil
}

// measuredToolIdentities captures the three lanes' provenance and returns
// their stable identity digests — the values a plan freezes and every later
// stage re-measures against.
func measuredToolIdentities(lane CLIReviewConfig) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		prov := buildLaneProvenance(h, lane)
		if prov.ResolvedModel == "" || prov.ResolvedModel == ResolvedUnavailable {
			return nil, fmt.Errorf("no resolved model identity for %s — a formal plan needs every lane measurable", h)
		}
		if prov.ToolIdentityDigest == "" {
			return nil, fmt.Errorf("no tool identity digest for %s", h)
		}
		out[h] = prov.ToolIdentityDigest
	}
	return out, nil
}

// randomSeed returns a fresh hex seed for an ordinal. A plan freezes its
// seeds at creation; regenerating one is a new plan.
func randomSeed() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// planRunnerDigest resolves the current runner source digest for the plan's
// revision label; a failure yields the empty string (the plan constructor
// still fails closed on its own digest resolution).
func planRunnerDigest() string {
	d, err := CurrentRunnerDigest()
	if err != nil {
		return ""
	}
	return d
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

// ---------- core-plan create ----------

func cmdCorePlanCreate(argv []string) error {
	fs := newFlagSet("core-plan create")
	out := fs.String("out", "", "output plan receipt path (required)")
	coreManifest := fs.String("core-manifest", "skills/engram/evals/dev-regression-core.manifest.json", "frozen core172 manifest")
	claudeSettings := fs.String("claude-settings", "", "claude aly_qwen_w settings path")
	codexProvider := fs.String("codex-provider", "", "codex model_provider override")
	codexModel := fs.String("codex-model", "", "codex model id")
	opencodeModel := fs.String("opencode-model", "", "opencode model id")
	coreExec := fs.String("core-exec", "", "core child boundary-config JSON (required)")
	timeout := fs.Int("timeout", 0, "per-case timeout seconds (required)")
	concurrency := fs.Int("concurrency", 0, "sealed worker count (required)")
	planID := fs.String("plan-id", "", "plan id (default core-plan-<utc-now>)")
	seed1 := fs.String("seed-1", "", "ordinal 1 case-order seed (default: fresh random)")
	seed2 := fs.String("seed-2", "", "ordinal 2 case-order seed (default: fresh random)")
	seed3 := fs.String("seed-3", "", "ordinal 3 case-order seed (default: fresh random)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *out == "" || *coreExec == "" || *timeout <= 0 || *concurrency <= 0 {
		return fmt.Errorf("--out, --core-exec, a positive --timeout and a positive --concurrency are required")
	}
	lane := formalLaneConfig(*claudeSettings, *codexProvider, *codexModel, *opencodeModel)
	_, templateDigests, err := PrimaryHostTemplates(lane)
	if err != nil {
		return err
	}
	identities, err := measuredToolIdentities(lane)
	if err != nil {
		return err
	}
	boundary, isoDigest, err := loadCoreExecConfig(*coreExec)
	if err != nil {
		return err
	}
	normalizedTemplate, err := NormalizedCoreTemplateDigest(templateDigests)
	if err != nil {
		return err
	}
	workerSet, err := NormalizedCoreWorkerIdentitySetDigest([]string{HostClaude, HostCodex, HostOpenCode}, *concurrency, normalizedTemplate)
	if err != nil {
		return err
	}
	boundaryTemplate := sha256Hex([]byte("boundary-template\x00" + string(boundary) + "\x00" + isoDigest))
	seeds := map[int]string{1: *seed1, 2: *seed2, 3: *seed3}
	for o := range seeds {
		if seeds[o] == "" {
			seeds[o] = randomSeed()
		}
	}
	id := *planID
	if id == "" {
		id = "core-plan-" + nowRFC3339()
	}
	plan, err := CreateCoreExecutionPlan(CorePlanInput{
		PlanID:           id,
		CoreManifestPath: *coreManifest,
		RunnerRevision:   "runner-" + shortDigest(planRunnerDigest()),
		Hosts:            []string{HostClaude, HostCodex, HostOpenCode},
		ToolIdentityDigests:                   identities,
		TimeoutSeconds:                        *timeout,
		Concurrency:                           *concurrency,
		CaseOrderSeeds:                        seeds,
		CoreBoundaryKind:                      boundary,
		NormalizedCoreWorkerIdentitySetDigest: workerSet,
		NormalizedCoreBoundaryTemplateDigest:  boundaryTemplate,
		NormalizedCoreExecutionTemplateDigest: normalizedTemplate,
		OutPath:                               *out,
	})
	if err != nil {
		return err
	}
	fmt.Printf("core-plan: id=%s plan=%s core_manifest=%s\n", plan.PlanID, plan.ReceiptDigest[:16], plan.CoreManifestDigest[:16])
	fmt.Printf("seeds: 1=%s 2=%s 3=%s\n", plan.CaseOrderSeeds[1], plan.CaseOrderSeeds[2], plan.CaseOrderSeeds[3])
	return nil
}

// ---------- series prepare ----------

// copyFileTo copies one file to an exact destination path (parent created).
func copyFileTo(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// verifySeriesDataset applies the split-correct integrity gate: the holdout
// copy must carry its full dataset seal (protected-root provenance); the dev
// core is a frozen manifest whose payload and per-file digests are reverified
// against the tracked files (runner-cli §1: "sealed for holdout; frozen
// manifest for dev").
func verifySeriesDataset(m *DatasetManifestV2, dir, membership string) error {
	if membership == MembershipHoldout96 {
		return VerifyDatasetSeal(m, dir)
	}
	payloadDigest, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		return err
	}
	if payloadDigest != m.PayloadDigest {
		return fmt.Errorf("dev dataset payload digest mismatch: manifest %s != recomputed %s", m.PayloadDigest, payloadDigest)
	}
	return nil
}

// copyDatasetIntoSeriesRoot re-verifies the dataset at its source and
// copies the manifest plus exactly its payload files into the series root —
// the reader-complete layout the scorer reads. Nothing outside the sealed
// payload list is copied.
func copyDatasetIntoSeriesRoot(sourceDir, manifestPath, membership, seriesRoot string) error {
	m, err := LoadDatasetManifest(manifestPath)
	if err != nil {
		return err
	}
	if m.ScoreMembership != membership {
		return fmt.Errorf("dataset membership %q != requested %q", m.ScoreMembership, membership)
	}
	if err := verifySeriesDataset(m, sourceDir, membership); err != nil {
		return fmt.Errorf("dataset at %s failed its integrity gate: %w", sourceDir, err)
	}
	dstDir := filepath.Join(seriesRoot, datasetsDir, membership)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if err := copyFileTo(manifestPath, filepath.Join(dstDir, datasetManifestName)); err != nil {
		return err
	}
	for _, pf := range m.PayloadFiles {
		if !safeRelativePath(pf.RelativePath) {
			return fmt.Errorf("payload file %q is not containment-safe", pf.RelativePath)
		}
		if err := copyFileTo(filepath.Join(sourceDir, filepath.FromSlash(pf.RelativePath)),
			filepath.Join(dstDir, filepath.FromSlash(pf.RelativePath))); err != nil {
			return err
		}
	}
	return nil
}

func cmdSeriesPrepare(argv []string) error {
	fs := newFlagSet("series prepare")
	seriesID := fs.String("series", "", "series id (required)")
	seriesRoot := fs.String("series-root", "", "prepared series root, created by this command (required)")
	purpose := fs.String("purpose", "", "official-dual|dev-comparison (required)")
	corePlanPath := fs.String("core-execution-plan", "", "sealed core execution plan (required)")
	devDataset := fs.String("dev-dataset", "skills/engram/evals", "dev dataset dir")
	holdoutDataset := fs.String("holdout-dataset", "", "sealed holdout manifest (official-dual required)")
	skillSnapshot := fs.String("skill-snapshot", "", "immutable snapshot dir (required)")
	pkgReceipt := fs.String("skill-package-validation", "", "passing package-validation receipt (required)")
	greenReceipt := fs.String("green-test-receipt", "", "passing series-prepare green receipt (required)")
	validator := fs.String("validator", "scripts/validate-agent-skill.mjs", "020 validator path")
	claudeSettings := fs.String("claude-settings", "", "claude settings path")
	codexProvider := fs.String("codex-provider", "", "codex provider")
	codexModel := fs.String("codex-model", "", "codex model")
	opencodeModel := fs.String("opencode-model", "", "opencode model")
	coreExec := fs.String("core-exec", "", "core child boundary-config JSON (required)")
	timeout := fs.Int("timeout", 0, "per-case timeout seconds (must equal the plan)")
	concurrency := fs.Int("concurrency", 0, "worker count (must equal the plan)")
	protectedRoot := fs.String("protected-root", "", "protected holdout root (official-dual required)")
	auditRoot := fs.String("author-audit-root", "", "author/review audit root (official-dual required)")
	authorStateRoot := fs.String("author-state-root", "", "author/review state root (official-dual required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	switch *purpose {
	case string(PurposeOfficialDual), string(PurposeDevComparison):
	default:
		return fmt.Errorf("--purpose must be official-dual or dev-comparison")
	}
	if *seriesID == "" || *seriesRoot == "" || *corePlanPath == "" || *skillSnapshot == "" ||
		*pkgReceipt == "" || *greenReceipt == "" || *coreExec == "" || *timeout <= 0 || *concurrency <= 0 {
		return fmt.Errorf("--series, --series-root, --core-execution-plan, --skill-snapshot, --skill-package-validation, --green-test-receipt, --core-exec, --timeout and --concurrency are required")
	}
	if fi, err := os.Stat(*seriesRoot); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("series root %s is not a directory", *seriesRoot)
		}
		entries, _ := os.ReadDir(*seriesRoot)
		if len(entries) > 0 {
			return fmt.Errorf("series root %s is not empty — a prepared series root is created fresh", *seriesRoot)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	plan, err := LoadCoreExecutionPlan(*corePlanPath)
	if err != nil {
		return err
	}
	lane := formalLaneConfig(*claudeSettings, *codexProvider, *codexModel, *opencodeModel)
	_, templateDigests, err := PrimaryHostTemplates(lane)
	if err != nil {
		return err
	}
	identities, err := measuredToolIdentities(lane)
	if err != nil {
		return err
	}
	boundary, isoDigest, err := loadCoreExecConfig(*coreExec)
	if err != nil {
		return err
	}
	normalizedTemplate, err := NormalizedCoreTemplateDigest(templateDigests)
	if err != nil {
		return err
	}
	// The live template/boundary identities must normalize to exactly what
	// the plan froze — otherwise the plan is stale and a new one is required.
	if normalizedTemplate != plan.NormalizedCoreExecutionTemplateDigest {
		return fmt.Errorf("current execution template %s != plan %s — create a new core plan", normalizedTemplate[:16], plan.NormalizedCoreExecutionTemplateDigest[:16])
	}
	if workerSet, err := NormalizedCoreWorkerIdentitySetDigest(plan.Hosts, plan.Concurrency, normalizedTemplate); err != nil || workerSet != plan.NormalizedCoreWorkerIdentitySetDigest {
		return fmt.Errorf("current worker identity set drifts from the plan — create a new core plan")
	}
	if boundaryTemplate := sha256Hex([]byte("boundary-template\x00" + string(boundary) + "\x00" + isoDigest)); boundaryTemplate != plan.NormalizedCoreBoundaryTemplateDigest {
		return fmt.Errorf("current boundary template drifts from the plan — create a new core plan")
	}

	official := *purpose == string(PurposeOfficialDual)
	// Fail fast before any model-calling probe runs: a green receipt whose
	// runner/judge/snapshot bindings drifted means the caller must rebuild
	// its receipts, and the canaries/probes would only burn money first.
	if green, err := LoadGreenTestReceipt(*greenReceipt); err == nil {
		if green.RunnerDigest != plan.RunnerDigest || green.JudgeRuleDigest != plan.JudgeRuleDigest {
			return fmt.Errorf("series-prepare green receipt binds runner %s / judge %s, plan freezes %s / %s — rebuild the plan and green receipt after a runner change",
				shortDigest(green.RunnerDigest), shortDigest(green.JudgeRuleDigest), shortDigest(plan.RunnerDigest), shortDigest(plan.JudgeRuleDigest))
		}
	}
	var protected *ProtectedExecutionReceipt
	canaries := map[string]map[int]*WorkspaceCanaryReceipt{}
	if official {
		if *holdoutDataset == "" || *protectedRoot == "" || *auditRoot == "" || *authorStateRoot == "" {
			return fmt.Errorf("official-dual requires --holdout-dataset, --protected-root, --author-audit-root and --author-state-root")
		}
		for _, d := range []*string{protectedRoot, auditRoot, authorStateRoot} {
			if fi, err := os.Stat(*d); err != nil || !fi.IsDir() {
				return fmt.Errorf("root %s must be an existing directory", *d)
			}
		}
		// Fresh formal roots, disjoint from the author/review roots and from
		// each other per split (§7.2).
		probeRoot := filepath.Join(*seriesRoot, "protected-probe")
		workerRoot := filepath.Join(*seriesRoot, "workers")
		formalStateRoots := []string{
			filepath.Join(*seriesRoot, "formal-state", MembershipCore172),
			filepath.Join(*seriesRoot, "formal-state", MembershipHoldout96),
		}
		protected, err = RunProtectedProbes(ProtectedProbeOptions{
			Plan: plan, Lane: lane, Boundary: boundary, IsolationConfigDigest: isoDigest,
			Root: probeRoot, ProtectedRoot: *protectedRoot, AuditRoot: *auditRoot,
			AuthorStateRoot: *authorStateRoot, FormalStateRoots: formalStateRoots,
			SplitAllocatorRoots: SplitAllocatorRoots(*seriesRoot),
			WorkerRoot:          workerRoot, Capacity: plan.Concurrency,
		})
		if err != nil {
			return fmt.Errorf("protected execution probes: %w", err)
		}
	}
	// Both purposes canary the final invocation whenever the bound dataset
	// stages workspace files — core172's trap layer always does.
	staged, err := datasetHasStagedFiles(*devDataset, filepath.Join(*devDataset, "dev-regression-core.manifest.json"))
	if err != nil {
		return err
	}
	if staged {
		receipts, err := RunWorkspaceCanaries(CanaryOptions{
			SeriesID: *seriesID, SkillDigest: snapshotSkillDigest(*pkgReceipt),
			Plan: plan, Protected: protected, ToolIdentityDigests: identities, Lane: lane,
			Root:     filepath.Join(*seriesRoot, "canary-run"),
			Child:    HostCanaryChild(lane),
			Boundary: boundary, IsolationConfigDigest: isoDigest,
		})
		if err != nil {
			return fmt.Errorf("workspace canaries: %w", err)
		}
		for _, c := range receipts {
			if canaries[c.Host] == nil {
				canaries[c.Host] = map[int]*WorkspaceCanaryReceipt{}
			}
			canaries[c.Host][c.WorkerSlot] = &c
		}
	}

	manifest, err := PrepareSeries(*seriesRoot, SeriesPrepareInput{
		SeriesID: *seriesID, Purpose: SeriesPurpose(*purpose),
		CorePlanPath: *corePlanPath, CoreManifestPath: filepath.Join(*devDataset, "dev-regression-core.manifest.json"),
		HoldoutManifestPath: *holdoutDataset, SnapshotRoot: *skillSnapshot,
		PackageValidationReceiptPath: *pkgReceipt, GreenTestReceiptPath: *greenReceipt,
		ValidatorPath: *validator, RunnerDigest: plan.RunnerDigest, JudgeRuleDigest: plan.JudgeRuleDigest,
		ToolIdentityDigests: identities, ToolConfigurationDigest: isoDigest,
		ExecutionEnvironmentDigest: normalizedTemplate,
		CaseOrderSeeds:             plan.CaseOrderSeeds, TimeoutSeconds: *timeout, Concurrency: *concurrency,
		StagedWorkspaceFiles: staged, Protected: protected, Canaries: canaries,
		OutPath: filepath.Join(*seriesRoot, seriesManifestFile),
	})
	if err != nil {
		return err
	}
	// Reader-complete layout: copy every receipt the scorer later loads.
	if err := copyFileTo(*corePlanPath, filepath.Join(*seriesRoot, corePlanFile)); err != nil {
		return err
	}
	if err := copyFileTo(*pkgReceipt, filepath.Join(*seriesRoot, packageValidationFile)); err != nil {
		return err
	}
	if err := copyFileTo(*greenReceipt, filepath.Join(*seriesRoot, greenSeriesPrepareFile)); err != nil {
		return err
	}
	if protected != nil {
		if err := writeFrozenJSON(filepath.Join(*seriesRoot, protectedExecutionFile), protected); err != nil {
			return err
		}
	}
	for h, slots := range canaries {
		for slot, c := range slots {
			if err := writeFrozenJSON(filepath.Join(*seriesRoot, canariesDir, h, fmt.Sprintf(canaryReceiptNameFmt, slot)), c); err != nil {
				return err
			}
		}
	}
	if err := copyDatasetIntoSeriesRoot(*devDataset, filepath.Join(*devDataset, "dev-regression-core.manifest.json"), MembershipCore172, *seriesRoot); err != nil {
		return err
	}
	if official {
		holdoutDir := filepath.Dir(*holdoutDataset)
		if err := copyDatasetIntoSeriesRoot(holdoutDir, *holdoutDataset, MembershipHoldout96, *seriesRoot); err != nil {
			return err
		}
	}
	fmt.Printf("series prepared: %s purpose=%s manifest=%s\n", manifest.SeriesID, manifest.Purpose, manifest.ManifestDigest[:16])
	return nil
}

// snapshotSkillDigest reads the skill digest a package-validation receipt
// binds, for the canary receipts to carry.
func snapshotSkillDigest(pkgReceiptPath string) string {
	r, err := LoadPackageValidationReceipt(pkgReceiptPath)
	if err != nil {
		return ""
	}
	return r.SkillDigest
}

// datasetHasStagedFiles reports whether any case of the sealed dataset
// stages a workspace file (the canary trigger).
func datasetHasStagedFiles(datasetDir, manifestPath string) (bool, error) {
	core, err := LoadCoreV2(datasetDir, manifestPath)
	if err != nil {
		return false, err
	}
	for _, c := range core.Cases {
		if len(c.WorkspaceFiles) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ---------- run --mode primary ----------

// loadSeriesCases loads the exact case set of one membership from the
// prepared series root's dataset copy. The manifest is the load truth; a
// payload digest or case-id drift fails closed before any child starts.
func loadSeriesCases(seriesRoot, membership string) (map[string]*TriggerCaseV2, []string, error) {
	dir := filepath.Join(seriesRoot, datasetsDir, membership)
	manifestPath := filepath.Join(dir, datasetManifestName)
	m, err := LoadDatasetManifest(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	if err := verifySeriesDataset(m, dir, membership); err != nil {
		return nil, nil, fmt.Errorf("dataset copy in the series root failed its integrity gate: %w", err)
	}
	cases := map[string]*TriggerCaseV2{}
	for _, pf := range m.PayloadFiles {
		if !safeRelativePath(pf.RelativePath) {
			return nil, nil, fmt.Errorf("payload file %q is not containment-safe", pf.RelativePath)
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pf.RelativePath)))
		if err != nil {
			return nil, nil, err
		}
		d, err := LFNormalizedSHA256(b)
		if err != nil {
			return nil, nil, err
		}
		if d != pf.LFNormalizedSHA256 && pf.LFNormalizedSHA256 != "PLACEHOLDER_COMPUTED_AT_RUNTIME" {
			return nil, nil, fmt.Errorf("payload file %s digest drift", pf.RelativePath)
		}
		var cf CasePayloadFile
		if err := StrictParseClosed(b, &cf); err != nil {
			return nil, nil, fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		for i := range cf.Cases {
			c := &cf.Cases[i]
			if err := ValidateCaseV2(c); err != nil {
				return nil, nil, fmt.Errorf("case %s: %w", c.ID, err)
			}
			if _, dup := cases[c.ID]; dup {
				return nil, nil, fmt.Errorf("duplicate case id %s", c.ID)
			}
			cases[c.ID] = c
		}
	}
	ids := append([]string(nil), m.CaseIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		if _, ok := cases[id]; !ok {
			return nil, nil, fmt.Errorf("manifest case id %s missing from payload", id)
		}
	}
	if len(cases) != len(ids) {
		return nil, nil, fmt.Errorf("payload carries %d cases, manifest lists %d", len(cases), len(ids))
	}
	return cases, ids, nil
}

// coreLegRunPaths returns the nine sealed dev-split run manifests of a
// series — the complete core leg a holdout ordinal 1 requires.
func coreLegRunPaths(seriesRoot, seriesID string) ([]string, []*PrimaryRunManifest, error) {
	var paths []string
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		for _, o := range Ordinals {
			paths = append(paths, filepath.Join(PrimaryRunRoot(seriesRoot, h, SplitDevRegression, o), runManifestName))
		}
	}
	runs := make([]*PrimaryRunManifest, 0, len(paths))
	for _, p := range paths {
		r, err := LoadPrimaryRun(p)
		if err != nil {
			return nil, nil, fmt.Errorf("core leg incomplete: %w", err)
		}
		if r.SeriesID != seriesID {
			return nil, nil, fmt.Errorf("core-leg run %s belongs to series %q", p, r.SeriesID)
		}
		if r.State != StateComplete {
			return nil, nil, fmt.Errorf("core-leg run %s state %q is not complete", p, r.State)
		}
		runs = append(runs, r)
	}
	return paths, runs, nil
}

func cmdRunPrimary(argv []string) error {
	fs := newFlagSet("run --mode primary")
	mode := fs.String("mode", "", "primary (accepted for run-router compatibility)")
	out := fs.String("out", "", "ignored: a primary run writes only under --series-root")
	seriesID := fs.String("series", "", "prepared series id (required)")
	split := fs.String("split", "", "dev-regression|holdout (required)")
	ordinal := fs.Int("run-ordinal", 0, "1|2|3 (required)")
	tool := fs.String("tool", "", "claude|codex|opencode (required)")
	seriesRoot := fs.String("series-root", "", "prepared series root (required)")
	corePlanPath := fs.String("core-execution-plan", "", "sealed core execution plan (required)")
	scratch := fs.String("scratch", "", "scratch root (required, unique per run)")
	binDir := fs.String("bin-dir", "", "bin dir with engram/engram-mcp (required)")
	timeout := fs.Int("timeout", 0, "per-case timeout seconds (default: sealed value)")
	concurrency := fs.Int("concurrency", 0, "worker count (default: sealed value)")
	greenReceipt := fs.String("green-test-receipt", "", "fresh pre-holdout green receipt (holdout ordinal 1)")
	validator := fs.String("validator", "scripts/validate-agent-skill.mjs", "020 validator path")
	claudeSettings := fs.String("claude-settings", "", "claude settings path")
	codexProvider := fs.String("codex-provider", "", "codex provider")
	codexModel := fs.String("codex-model", "", "codex model")
	opencodeModel := fs.String("opencode-model", "", "opencode model")
	// Selector affordances exist only in diagnostic mode: a primary run
	// rejects them explicitly instead of leaving them undefined.
	only := fs.String("only", "", "rejected: diagnostic-only")
	sample := fs.Int("sample", 0, "rejected: diagnostic-only")
	limit := fs.Int("limit", 0, "rejected: diagnostic-only")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	_ = out // accepted for router compatibility; never a primary output path
	if *mode != "" && *mode != "primary" {
		return fmt.Errorf("the primary runner cannot run in %q mode", *mode)
	}
	if *only != "" || *sample != 0 || *limit != 0 {
		return fmt.Errorf("--only/--sample/--limit are diagnostic selectors: a primary run covers every case exactly once and never subsamples or retries")
	}
	if *seriesID == "" || *split == "" || *ordinal == 0 || *tool == "" || *seriesRoot == "" ||
		*corePlanPath == "" || *scratch == "" || *binDir == "" {
		return fmt.Errorf("--series, --split, --run-ordinal, --tool, --series-root, --core-execution-plan, --scratch and --bin-dir are required: a primary run never starts without a prepared series")
	}
	switch *split {
	case SplitDevRegression, SplitHoldout:
	default:
		return fmt.Errorf("split %q invalid", *split)
	}
	if *ordinal != 1 && *ordinal != 2 && *ordinal != 3 {
		return fmt.Errorf("--run-ordinal must be 1, 2 or 3")
	}
	manifest, err := LoadSeriesManifest(filepath.Join(*seriesRoot, seriesManifestFile))
	if err != nil {
		return err
	}
	if manifest.SeriesID != *seriesID {
		return fmt.Errorf("series root carries series %q, requested %q", manifest.SeriesID, *seriesID)
	}
	membership, err := MembershipOfSplit(*split)
	if err != nil {
		return err
	}
	if manifest.DatasetManifests[membership] == "" {
		return fmt.Errorf("series %s does not bind the %s split", manifest.SeriesID, membership)
	}
	plan, err := LoadCoreExecutionPlan(*corePlanPath)
	if err != nil {
		return err
	}
	if plan.ReceiptDigest != manifest.CoreExecutionPlanDigest {
		return fmt.Errorf("plan %s is not the one series %s sealed", plan.ReceiptDigest[:16], manifest.SeriesID)
	}
	// Omission means the sealed value; a mismatch is a usage error before any
	// case starts — never a silent change of execution conditions.
	if *timeout == 0 {
		*timeout = plan.TimeoutSeconds
	}
	if *concurrency == 0 {
		*concurrency = plan.Concurrency
	}
	if *timeout != plan.TimeoutSeconds || *timeout != manifest.TimeoutSeconds {
		return fmt.Errorf("--timeout %d != sealed %d: a primary run never changes execution conditions", *timeout, plan.TimeoutSeconds)
	}
	if *concurrency != plan.Concurrency || *concurrency != manifest.Concurrency {
		return fmt.Errorf("--concurrency %d != sealed %d: a primary run never changes execution conditions", *concurrency, plan.Concurrency)
	}
	lane := formalLaneConfig(*claudeSettings, *codexProvider, *codexModel, *opencodeModel)
	prov := buildLaneProvenance(*tool, lane)
	if prov.ToolIdentityDigest != plan.ToolIdentityDigests[*tool] {
		return fmt.Errorf("measured tool identity for %s drifted from the sealed plan — a primary run cannot start", *tool)
	}

	// The split's exact sealed case set, from the series root's dataset copy.
	cases, caseIDs, err := loadSeriesCases(*seriesRoot, membership)
	if err != nil {
		return err
	}

	var protected *ProtectedExecutionReceipt
	isoDigest := manifest.ToolConfigurationDigest
	if manifest.Purpose == PurposeOfficialDual {
		protected = &ProtectedExecutionReceipt{}
		if err := loadStrictFile(filepath.Join(*seriesRoot, protectedExecutionFile), protected); err != nil {
			return err
		}
		if err := ValidateProtectedExecutionReceipt(protected, plan); err != nil {
			return fmt.Errorf("protected execution receipt: %w", err)
		}
	} else if manifest.Purpose == PurposeDevComparison && *split == SplitHoldout {
		return fmt.Errorf("a dev-comparison series never runs the holdout split")
	}

	runRoot := PrimaryRunRoot(*seriesRoot, *tool, *split, *ordinal)
	// Holdout ordinal 1 binds the version — after a complete core leg and a
	// fresh, matching pre-holdout attestation, before any holdout child.
	if *split == SplitHoldout && *ordinal == 1 {
		if manifest.CandidateBindingDigest == "" {
			return fmt.Errorf("series %s carries no candidate binding digest", manifest.SeriesID)
		}
		if err := bindHoldoutOrdinal1(*seriesRoot, manifest, plan, *greenReceipt, *validator); err != nil {
			return err
		}
	}
	opts := PrimaryRunOptions{
		SeriesID: *seriesID, Host: *tool, Split: *split, Ordinal: *ordinal,
		Concurrency: *concurrency, Plan: plan, Manifest: manifest, Protected: protected,
		IsolationConfigDigest: isoDigest, Lane: lane,
		Cases: cases, CaseIDs: caseIDs, RunRoot: runRoot, BinDir: *binDir,
		EnforceSplitSize: true,
	}
	var obs PrimaryRunObservation
	opts.Observation = &obs
	runManifest, receipts, err := RunPrimary(opts)
	invalid := runManifest != nil && runManifest.State == StateInvalid
	for _, rec := range receipts {
		fmt.Printf("case %s: status=%s attempts=%d\n", rec.CaseID, rec.Status, rec.AttemptCount)
	}
	if err != nil {
		if invalid {
			return fmt.Errorf("primary run %s: %w", runRoot, err)
		}
		return err
	}
	fmt.Printf("primary run: %s/%s/o%d cases=%d max_in_flight=%d overlap=%v manifest=%s\n",
		*tool, *split, *ordinal, len(receipts), obs.MaxInFlight, obs.Overlap, runManifest.RunDigest[:16])
	// A completed holdout ordinal 3 ends the series: consume the version
	// immediately, whether the later score passes or fails.
	if *split == SplitHoldout && *ordinal == 3 {
		if err := consumeHoldoutIfComplete(*seriesRoot, manifest); err != nil {
			return err
		}
	}
	return nil
}

// bindHoldoutOrdinal1 performs the pre-execution holdout gate: complete core
// leg, fresh pre-holdout attestation verified against it, atomic first
// binding (or recovery append for a new series under the same stable digest).
func bindHoldoutOrdinal1(seriesRoot string, manifest *FormalSeriesManifest, plan *CoreExecutionPlanReceipt, greenReceiptPath, validatorPath string) error {
	if greenReceiptPath == "" {
		return fmt.Errorf("holdout ordinal 1 requires --green-test-receipt (a fresh pre-holdout attestation)")
	}
	_, runs, err := coreLegRunPaths(seriesRoot, manifest.SeriesID)
	if err != nil {
		return err
	}
	coreLeg, err := CoreLegCompletionDigest(runs)
	if err != nil {
		return err
	}
	green, err := LoadGreenTestReceipt(greenReceiptPath)
	if err != nil {
		return err
	}
	coreLegDigest := coreLeg
	manifestDigest := manifest.ManifestDigest
	bindingDigest := manifest.CandidateBindingDigest
	if err := VerifyGreenTestReceipt(green, SuitePreHoldout, validatorPath, GreenBindings{
		SeriesManifestDigest: &manifestDigest, CandidateBindingDigest: &bindingDigest,
		CoreLegCompletionDigest: &coreLegDigest,
	}); err != nil {
		return fmt.Errorf("pre-holdout green receipt: %w", err)
	}
	bindingPath := filepath.Join(seriesRoot, holdoutBindingFile)
	membership, err := MembershipOfSplit(SplitHoldout)
	if err != nil {
		return err
	}
	holdoutManifest := filepath.Join(seriesRoot, datasetsDir, membership, datasetManifestName)
	if _, statErr := os.Stat(bindingPath); statErr == nil {
		// Existing ledger: only a NEW series under the same stable digest may
		// append its recovery attempt.
		if err := appendHoldoutAttempt(seriesRoot, bindingPath, manifest, greenReceiptPath, coreLeg); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	} else {
		if _, err := BindHoldout(seriesRoot, HoldoutBindInput{
			DatasetManifestPath:     holdoutManifest,
			SeriesManifestPath:      filepath.Join(seriesRoot, seriesManifestFile),
			CoreLegRunPaths:         coreLegPaths(seriesRoot, manifest.SeriesID),
			CoreLegCompletionDigest: coreLeg,
			PreHoldoutReceiptPath:   greenReceiptPath,
			ValidatorPath:           validatorPath,
			OutPath:                 bindingPath,
		}); err != nil {
			return fmt.Errorf("holdout binding: %w", err)
		}
	}
	// Reader-complete layout: the attestation lives in the series root.
	return copyFileTo(greenReceiptPath, filepath.Join(seriesRoot, greenPreHoldoutFile))
}

// coreLegPaths is the path-only view of coreLegRunPaths (BindHoldout loads
// and re-verifies them itself).
func coreLegPaths(seriesRoot, seriesID string) []string {
	var paths []string
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		for _, o := range Ordinals {
			paths = append(paths, filepath.Join(PrimaryRunRoot(seriesRoot, h, SplitDevRegression, o), runManifestName))
		}
	}
	return paths
}

// appendHoldoutAttempt associates a new series attempt with the existing
// binding ledger: same stable candidate digest, new manifest digest, fresh
// pre-holdout receipt and fresh core-leg completion.
func appendHoldoutAttempt(seriesRoot, bindingPath string, manifest *FormalSeriesManifest, greenReceiptPath, coreLeg string) error {
	binding, err := LoadHoldoutBinding(bindingPath)
	if err != nil {
		return err
	}
	if binding.CandidateBindingDigest != manifest.CandidateBindingDigest {
		return fmt.Errorf("binding stable candidate digest %q != series %q — a changed binding requires a new holdout version",
			binding.CandidateBindingDigest[:16], manifest.CandidateBindingDigest[:16])
	}
	for _, a := range binding.SeriesAttempts {
		if a.SeriesID == manifest.SeriesID {
			return fmt.Errorf("series %s already has a binding attempt — holdout ordinal 1 is bound once per series", manifest.SeriesID)
		}
	}
	membership, err := MembershipOfSplit(SplitHoldout)
	if err != nil {
		return err
	}
	if _, err := AppendHoldoutAttempt(seriesRoot, HoldoutAppendInput{
		BindingPath:             bindingPath,
		SeriesManifestPath:      filepath.Join(seriesRoot, seriesManifestFile),
		CoreLegRunPaths:         coreLegPaths(seriesRoot, manifest.SeriesID),
		CoreLegCompletionDigest: coreLeg,
		PreHoldoutReceiptPath:   greenReceiptPath,
		ValidatorPath:           validatorOf(seriesRoot),
	}); err != nil {
		return fmt.Errorf("holdout binding append: %w", err)
	}
	_ = membership
	return nil
}

// validatorOf resolves the 020 validator path a receipt chain in the series
// root binds — the default repository path.
func validatorOf(seriesRoot string) string {
	if _, err := os.Stat("scripts/validate-agent-skill.mjs"); err == nil {
		return "scripts/validate-agent-skill.mjs"
	}
	_ = seriesRoot
	return "scripts/validate-agent-skill.mjs"
}

// consumeHoldoutIfComplete marks the holdout version consumed once this
// series finished its third holdout ordinal — the version is spent whether
// the eventual score passes or fails.
func consumeHoldoutIfComplete(seriesRoot string, manifest *FormalSeriesManifest) error {
	bindingPath := filepath.Join(seriesRoot, holdoutBindingFile)
	if _, err := os.Stat(bindingPath); err != nil {
		return fmt.Errorf("holdout binding missing at holdout completion: %w", err)
	}
	paths, runs, err := coreLegRunPaths(seriesRoot, manifest.SeriesID)
	if err != nil {
		return err
	}
	_ = paths
	coreLeg, err := CoreLegCompletionDigest(runs)
	if err != nil {
		return err
	}
	_ = coreLeg
	// All nine holdout runs must be complete sealed manifests.
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		for _, o := range Ordinals {
			r, err := LoadPrimaryRun(filepath.Join(PrimaryRunRoot(seriesRoot, h, SplitHoldout, o), runManifestName))
			if err != nil {
				return fmt.Errorf("holdout leg incomplete: %w", err)
			}
			if r.State != StateComplete {
				return fmt.Errorf("holdout leg %s/%d is %q", h, o, r.State)
			}
		}
	}
	outcome := "complete-pass"
	if _, err := ConsumeHoldoutBinding(seriesRoot, bindingPath, filepath.Join(seriesRoot, seriesManifestFile), outcome); err != nil {
		return fmt.Errorf("holdout consumption: %w", err)
	}
	fmt.Printf("holdout consumed: version spent by series %s\n", manifest.SeriesID)
	return nil
}

// ---------- score ----------

func cmdScore(argv []string) error {
	fs := newFlagSet("score")
	seriesRoot := fs.String("series-root", "", "prepared official-dual series root (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *seriesRoot == "" {
		return fmt.Errorf("--series-root is required")
	}
	report, err := RunOfficialScore(*seriesRoot)
	if err != nil {
		return err
	}
	fmt.Printf("official score: series=%s verdict=%s\n", report.SeriesID, report.OverallVerdict)
	for _, fam := range []struct {
		name   string
		scores []HostScore
	}{{"dev-regression", report.DevRegression}, {"generalization", report.Generalization}} {
		for _, hs := range fam.scores {
			for _, g := range hs.Gates {
				fmt.Printf("  %s %s/%s %s: %d/%d median → %v\n",
					fam.name, hs.Host, hs.Split, g.ID, g.MedianNumerator, g.Denominator, passFail(g.Passed))
			}
		}
	}
	fmt.Printf("report: %s\n", filepath.Join(*seriesRoot, officialScoreReportFile))
	return nil
}

func passFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

// ---------- failure-archive / compare (US5 dev flywheel, T053) ----------

// flywheelForbiddenFlags are the flags that would turn a dev-only flywheel
// artifact into an official-score or holdout-reading surface. Neither command
// ever produces a score or opens a holdout path, so asking for one is a usage
// error rather than a silently ignored flag.
var flywheelForbiddenFlags = []string{"official-score", "include-holdout", "holdout-dataset", "holdout-series-root"}

// rejectFlywheelForbiddenFlags refuses any invocation that asks a dev-only
// flywheel command for official-score or holdout material.
func rejectFlywheelForbiddenFlags(argv []string, cmd string) error {
	for _, a := range argv {
		for _, f := range flywheelForbiddenFlags {
			if a == "--"+f || strings.HasPrefix(a, "--"+f+"=") {
				return fmt.Errorf("%s is dev-only: it never produces an official score/headline and never reads holdout material (--%s is not part of its surface)", cmd, f)
			}
		}
	}
	return nil
}

// loadRootCauseFile reads the closed root-cause classification of the
// baseline's median-fail cells: a JSON map "host/case-id" → closed dev root
// cause. Any value outside the enum fails before an archive is built.
func loadRootCauseFile(path string) (map[string]DevRootCause, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("root-cause file %s: %w", path, err)
	}
	raw := map[string]string{}
	if err := StrictParseClosed(b, &raw); err != nil {
		return nil, fmt.Errorf("root-cause file %s: %w", path, err)
	}
	causes := make(map[string]DevRootCause, len(raw))
	for cell, cause := range raw {
		c := DevRootCause(cause)
		if !ValidDevRootCause(c) {
			return nil, fmt.Errorf("root-cause file %s: cell %s cause %q is outside the closed dev enum", path, cell, cause)
		}
		causes[cell] = c
	}
	return causes, nil
}

// cmdFailureArchive implements `skill-eval failure-archive`: derive the
// sealed dev failure archive of one complete sealed dev-comparison series.
func cmdFailureArchive(argv []string) error {
	fs := newFlagSet("failure-archive")
	seriesRoot := fs.String("series-root", "", "complete sealed dev-comparison series root (required)")
	rootCauses := fs.String("root-causes", "", "JSON map host/case-id → closed dev root cause for every median-fail cell")
	out := fs.String("out", "", "output failure-archive.json path (required, never overwritten)")
	if err := rejectFlywheelForbiddenFlags(argv, "failure-archive"); err != nil {
		return err
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *seriesRoot == "" || *out == "" {
		return fmt.Errorf("--series-root and --out are required")
	}
	causes := map[string]DevRootCause{}
	if *rootCauses != "" {
		loaded, err := loadRootCauseFile(*rootCauses)
		if err != nil {
			return err
		}
		causes = loaded
	}
	archive, err := BuildFailureArchive(&FailureArchiveInput{SeriesRoot: *seriesRoot, RootCauses: causes})
	if err != nil {
		return err
	}
	if err := WriteFailureArchive(*out, archive); err != nil {
		return err
	}
	fmt.Printf("dev failure archive: entries=%d plan=%s\n  archive: %s\n",
		len(archive.Entries), archive.CoreExecutionPlanDigest[:16], *out)
	return nil
}

// cmdCompare implements `skill-eval compare`: the sealed dev-only before/after
// comparison of two core172 legs sharing one CoreExecutionPlanReceipt. Only
// the candidate's core172 run paths are opened — never a holdout path — and
// neither official score family is ever written here.
func cmdCompare(argv []string) error {
	fs := newFlagSet("compare")
	baseline := fs.String("baseline-series-root", "", "complete sealed dev-comparison baseline root (required)")
	candidate := fs.String("candidate-series-root", "", "official-dual series root; only its core172 leg is read (required)")
	archive := fs.String("failure-archive", "", "sealed dev failure archive of the baseline (required)")
	extension := fs.String("extension-receipt", "", "dev diagnostic extension receipt (required once the round requires backfill)")
	out := fs.String("out", "", "output flywheel-comparison.json path (required, never overwritten)")
	if err := rejectFlywheelForbiddenFlags(argv, "compare"); err != nil {
		return err
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *baseline == "" || *candidate == "" || *archive == "" || *out == "" {
		return fmt.Errorf("--baseline-series-root, --candidate-series-root, --failure-archive and --out are required")
	}
	report, err := CompareDevSeries(&CompareDevSeriesInput{
		BaselineSeriesRoot:   *baseline,
		CandidateSeriesRoot:  *candidate,
		FailureArchivePath:   *archive,
		ExtensionReceiptPath: *extension,
	})
	if err != nil {
		return err
	}
	if err := WriteFlywheelComparison(*out, report); err != nil {
		return err
	}
	fmt.Printf("flywheel comparison: baseline=%s candidate=%s fail-to-pass=%d regression=%d sc5=%s\n  receipt: %s\n",
		report.BaselineSeriesID, report.CandidateSeriesID,
		report.FailToPassCount, report.RegressionCount, strings.ToUpper(report.SC5Verdict), *out)
	return nil
}
