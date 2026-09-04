package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 048 command routing (runner-cli.md §1). The legacy `validate`/`run` surface
// evolves: v2 validate is split/phase-aware, `family-index build` freezes the
// CLI-reviewed index, `run --mode` distinguishes primary from diagnostic, and
// `green-test create` / `package validate` gate irreversible actions.

const usageV2 = `skill-eval — engram skill trigger evaluation runner (048)

Formal / protocol commands:
  validate      --dataset <dir> --split dev-regression|holdout
                [--phase pre-index|family-aware] [--dev-family-index <file>]
                [--manifest <file>] [--out <receipt.json>]
  family-index build --dataset <dir> --core-manifest <file> --review-prompt <file>
                --out <dev-family-index.json> --tool claude,codex,opencode
                --claude-settings ~/.claude/settings.json.aly_qwen_w
                --codex-provider aq --codex-model qwen3.8-flash --opencode-model <m>
                --concurrency <n>
  green-test create --suite holdout-pipeline|formal-tooling|series-prepare|pre-holdout
                --validator scripts/validate-agent-skill.mjs [--skill-snapshot <dir>]
                [--skill-package-validation <file>] [--series-root <dir>] --out <file>
  package validate --skill-dir <dir> --repository-root <repo> --snapshot-root <dir>
                --green-test-receipt <file> --out <file>
  core-plan create --out <plan.json> --core-exec <boundary-config.json>
                --core-manifest <file> --timeout <s> --concurrency <n>
                [--claude-settings <p>] [--codex-provider aq] [--codex-model <m>]
                [--opencode-model <m>] [--seed-1 <s>] [--seed-2 <s>] [--seed-3 <s>]
  series prepare --series <id> --series-root <dir> --purpose official-dual|dev-comparison
                --core-execution-plan <plan.json> --skill-snapshot <dir>
                --skill-package-validation <file> --green-test-receipt <file>
                --core-exec <file> --timeout <s> --concurrency <n>
                [--holdout-dataset --protected-root --author-audit-root --author-state-root]  (official-dual)
  score --series-root <dir>   # official-dual only; dev-comparison is refused
  failure-archive --series-root <dev-comparison-root> [--root-causes <file>]
                --out <failure-archive.json>   # dev-only, no official score
  compare --baseline-series-root <dev-comparison-root>
                --candidate-series-root <official-dual-core-leg>
                --failure-archive <file> [--extension-receipt <file>]
                --out <flywheel-comparison.json>   # core172 paths only, no holdout read

Dev commands (diagnostic, never score-eligible):
  run --mode diagnostic --split dev-regression --dataset <dir> --manifest <file>
                --tool <t> --out <dir> --scratch <dir> --bin-dir <dir> --concurrency <n>
                [--only ids] [--sample n] [--limit n] [--include-extension]
  run --mode primary --series <id> --split dev-regression|holdout --run-ordinal 1|2|3
                --tool <t> --series-root <dir> --core-execution-plan <plan.json>
                --scratch <dir> --bin-dir <dir> [--green-test-receipt <file>]  (holdout ordinal 1)
  run (legacy)  --dataset <dir> --out <dir> [--tool ...] (v1 dev harness)
`

// routeV2 dispatches the v2 command surface. ok=false falls back to legacy.
func routeV2(args []string) (handled bool, err error) {
	switch args[0] {
	case "family-index":
		if len(args) < 2 {
			return true, fmt.Errorf("family-index requires a subcommand (build)")
		}
		if args[1] != "build" {
			return true, fmt.Errorf("unknown family-index subcommand %q", args[1])
		}
		return true, cmdFamilyIndexBuild(args[2:])
	case "green-test":
		if len(args) < 2 {
			return true, fmt.Errorf("green-test requires a subcommand (create)")
		}
		if args[1] != "create" {
			return true, fmt.Errorf("unknown green-test subcommand %q", args[1])
		}
		return true, cmdGreenTestCreate(args[2:])
	case "package":
		if len(args) < 2 {
			return true, fmt.Errorf("package requires a subcommand (validate)")
		}
		if args[1] != "validate" {
			return true, fmt.Errorf("unknown package subcommand %q", args[1])
		}
		return true, cmdPackageValidate(args[2:])
	case "rejudge":
		return true, cmdRejudge(args[1:])
	case "core-plan":
		if len(args) < 2 {
			return true, fmt.Errorf("core-plan requires a subcommand (create)")
		}
		if args[1] != "create" {
			return true, fmt.Errorf("unknown core-plan subcommand %q", args[1])
		}
		return true, cmdCorePlanCreate(args[2:])
	case "series":
		if len(args) < 2 {
			return true, fmt.Errorf("series requires a subcommand (prepare)")
		}
		if args[1] != "prepare" {
			return true, fmt.Errorf("unknown series subcommand %q", args[1])
		}
		return true, cmdSeriesPrepare(args[2:])
	case "score":
		return true, cmdScore(args[1:])
	case "failure-archive":
		return true, cmdFailureArchive(args[1:])
	case "compare":
		return true, cmdCompare(args[1:])
	case "holdout":
		if len(args) < 2 {
			return true, fmt.Errorf("holdout requires a subcommand (generate|review|seal)")
		}
		switch args[1] {
		case "generate":
			return true, cmdHoldoutGenerate(args[2:])
		case "review":
			return true, cmdHoldoutReview(args[2:])
		case "seal":
			return true, cmdHoldoutSeal(args[2:])
		}
		return true, fmt.Errorf("unknown holdout subcommand %q", args[1])
	case "validate":
		// v2 split/phase-aware validation; a bare `validate --dataset` still
		// falls through to the legacy surface unless --split is given.
		if hasFlag(args[1:], "split") {
			return true, cmdValidateV2(args[1:])
		}
		return false, nil
	}
	return false, nil
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--"+name || strings.HasPrefix(a, "--"+name+"=") {
			return true
		}
	}
	return false
}

// holdoutLaneConfig builds the frozen three-lane CLI config from the same
// env-driven settings the family-index/review lanes use.
func holdoutLaneConfig(datasetDir string) CLIReviewConfig {
	return CLIReviewConfig{
		Lanes:         []string{HostClaude, HostCodex, HostOpenCode},
		ClaudeSettings: os.Getenv("ENGRAM_SKILL_EVAL_CLAUDE_SETTINGS"),
		CodexProvider: os.Getenv("ENGRAM_SKILL_EVAL_CODEX_PROVIDER"),
		CodexModel:    os.Getenv("ENGRAM_SKILL_EVAL_CODEX_MODEL"),
		OpenCodeModel: os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL"),
	}
}

func cmdHoldoutGenerate(argv []string) error {
	fs := newFlagSet("holdout generate")
	root := fs.String("root", "", "operator-provided protected root (absolute, must exist)")
	datasetDir := fs.String("dataset", "skills/engram/evals", "dataset dir with prompts and dev index")
	concurrency := fs.Int("concurrency", 2, "bounded author/review worker capacity")
	maxAttempts := fs.Int("max-attempts-per-slot", 6, "regeneration budget per quota slot")
	only := fs.String("only", "", "comma-separated slot keys (author/module/lang/scenario) to fill alone — smoke use")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *root == "" {
		return fmt.Errorf("--root is required (operator-provided protected root)")
	}
	cfg := HoldoutBatchConfig{
		Root:               *root,
		DatasetDir:         *datasetDir,
		AuthorPromptFile:   filepath.Join(*datasetDir, "prompts", "holdout-authoring-v1.md"),
		ReviewPromptFile:   filepath.Join(*datasetDir, "prompts", "holdout-review-v2.md"),
		DevIndexFile:       filepath.Join(*datasetDir, "dev-family-index.json"),
		Concurrency:        *concurrency,
		MaxAttemptsPerSlot: *maxAttempts,
	}
	b, err := LoadOrInitHoldoutBatch(cfg)
	if err != nil {
		return err
	}
	var onlySet map[string]bool
	if *only != "" {
		onlySet = map[string]bool{}
		for _, k := range strings.Split(*only, ",") {
			onlySet[strings.TrimSpace(k)] = true
		}
	}
	if err := b.Run(holdoutLaneConfig(*datasetDir), onlySet); err != nil {
		return err
	}
	fmt.Printf("holdout generate: filled=%d/96 revision=%d attempts=%d\n",
		len(b.persist.Filled), b.persist.Accepted.Revision, len(b.persist.Ledger.Events))
	return nil
}

// cmdHoldoutReview re-runs the dual review for every unfilled slot's pending
// candidates (fresh sessions against the newest accepted state).
func cmdHoldoutReview(argv []string) error {
	// Review is inlined in generate (stale CAS reruns both reviews in fresh
	// sessions automatically); this command surfaces the same loop for an
	// operator-triggered retry after an interrupted batch.
	return cmdHoldoutGenerate(argv)
}

func cmdHoldoutSeal(argv []string) error {
	fs := newFlagSet("holdout seal")
	root := fs.String("root", "", "protected root of a completed batch")
	datasetDir := fs.String("dataset", "skills/engram/evals", "dataset dir with prompts and dev index")
	anchorType := fs.String("anchor-type", "immutable-object", "git-tag | detached-signature | immutable-object")
	anchorID := fs.String("anchor-id", "", "external anchor identifier")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *root == "" {
		return fmt.Errorf("--root is required")
	}
	if *anchorID == "" {
		return fmt.Errorf("--anchor-id is required (content address / tag / signature id)")
	}
	cfg := HoldoutBatchConfig{
		Root:             *root,
		DatasetDir:       *datasetDir,
		AuthorPromptFile: filepath.Join(*datasetDir, "prompts", "holdout-authoring-v1.md"),
		ReviewPromptFile: filepath.Join(*datasetDir, "prompts", "holdout-review-v2.md"),
		DevIndexFile:     filepath.Join(*datasetDir, "dev-family-index.json"),
		Concurrency:      1,
		AnchorType:       *anchorType,
		AnchorID:         *anchorID,
	}
	b, err := LoadOrInitHoldoutBatch(cfg)
	if err != nil {
		return err
	}
	m, err := b.Seal()
	if err != nil {
		return err
	}
	fmt.Printf("holdout sealed: cases=%d payload=%s manifest=%s\n",
		m.CaseCount, m.PayloadDigest[:16], m.Seal.ManifestDigest[:16])
	fmt.Printf("receipt written to %s\n", filepath.Join(*root, "sealed", "manifest.json"))
	return nil
}

// cmdRejudge re-runs the deterministic judge offline over one completed
// diagnostic run: raw event streams and per-case stores are re-read, no CLI
// or model is invoked, and the fresh verdicts are printed plus summarized.
// This exists for harness-failure recovery (e.g. an exit-code misread) —
// the observable evidence is unchanged.
func cmdRejudge(argv []string) error {
	fs := newFlagSet("rejudge")
	datasetDir := fs.String("dataset", "skills/engram/evals", "dataset directory")
	manifest := fs.String("manifest", "", "manifest file")
	tool := fs.String("tool", "", "host tool of the completed run (claude|codex|opencode)")
	runRoot := fs.String("run", "", "completed diagnostic run root (contains raw/) (required)")
	scratchRoot := fs.String("scratch", "", "scratch root of that run (contains data/<case>/) (required)")
	binDir := fs.String("bin-dir", "", "bin dir with the engram CLI (required)")
	only := fs.String("only", "", "comma-separated case ids (default: all cases in the manifest)")
	out := fs.String("out", "", "write the rejudge verdict list JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *manifest == "" {
		*manifest = filepath.Join(*datasetDir, "dev-regression-core.manifest.json")
	}
	if *tool == "" || *runRoot == "" || *scratchRoot == "" || *binDir == "" {
		return fmt.Errorf("--tool, --run, --scratch and --bin-dir are required")
	}
	core, err := LoadCoreV2(*datasetDir, *manifest)
	if err != nil {
		return err
	}
	var parsers = map[string]func(io.Reader) []Event{HostClaude: ParseClaude, HostCodex: ParseCodex, HostOpenCode: ParseOpenCode}
	parse, ok := parsers[*tool]
	if !ok {
		return fmt.Errorf("unknown tool %q", *tool)
	}
	onlySet := map[string]bool{}
	if *only != "" {
		for _, id := range splitTools(*only) {
			onlySet[id] = true
		}
	}
	type row struct {
		CaseID  string `json:"case_id"`
		Pass    bool   `json:"pass"`
		Failure string `json:"failure"`
		Detail  string `json:"detail"`
	}
	var rows []row
	modStats := map[string][2]int{}
	for _, id := range sortedKeys(core.Cases) {
		if len(onlySet) > 0 && !onlySet[id] {
			continue
		}
		c := core.Cases[id]
		raw, err := os.ReadFile(filepath.Join(*runRoot, "raw", id+".jsonl"))
		if err != nil {
			return fmt.Errorf("case %s: %w", id, err)
		}
		// The re-read exit-code policy: a stream that carries a completion
		// event is a completed run regardless of the recorded exit code.
		runErr := error(nil)
		if len(raw) == 0 {
			runErr = fmt.Errorf("empty raw stream")
		}
		events := parse(bytes.NewReader(raw))
		caseDir := filepath.Join(*scratchRoot, "data", id)
		storeDump := ""
		if o, err := newStoreDumpCommand(filepath.Join(*binDir, "engram"), caseDir).Output(); err == nil {
			storeDump = string(o)
		}
		v := JudgeV2(c, events, storeDump, runErr)
		fmt.Printf("%s: pass=%v failure=%s detail=%s\n", v.CaseID, v.Pass, v.Failure, v.Detail)
		s := modStats[c.Module]
		if v.Pass {
			s[0]++
		}
		s[1]++
		modStats[c.Module] = s
		rows = append(rows, row{CaseID: v.CaseID, Pass: v.Pass, Failure: v.Failure, Detail: v.Detail})
	}
	total, passed := 0, 0
	for _, s := range modStats {
		total += s[1]
		passed += s[0]
	}
	fmt.Printf("rejudge: %d/%d passed (offline; no CLI/model invoked)\n", passed, total)
	if *out != "" {
		b, err := CanonicalJSON(rows)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			return err
		}
		fmt.Printf("rejudge verdicts: %s\n", *out)
	}
	return nil
}

func cmdGreenTestCreate(argv []string) error {
	fs := newFlagSet("green-test create")
	suite := fs.String("suite", "", "fixed suite name")
	validator := fs.String("validator", "scripts/validate-agent-skill.mjs", "path to the 020 validator")
	out := fs.String("out", "", "output receipt path (required)")
	skillSnapshot := fs.String("skill-snapshot", "", "frozen snapshot dir (series-prepare/pre-holdout)")
	skillPV := fs.String("skill-package-validation", "", "package validation receipt (series-prepare/pre-holdout)")
	seriesRoot := fs.String("series-root", "", "prepared series root (pre-holdout)")
	candidateBinding := fs.String("candidate-binding", "", "stable candidate binding digest (pre-holdout)")
	coreLegCompletion := fs.String("core-leg-completion", "", "core-leg receipt set digest (pre-holdout)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *suite == "" || *out == "" {
		return fmt.Errorf("--suite and --out are required")
	}
	if !fixedSuites[*suite] {
		return fmt.Errorf("suite %q is not one of the fixed suites %v", *suite, sortedStringsOf(fixedSuites))
	}
	bindings := GreenBindings{}
	if *skillSnapshot != "" && *skillPV != "" {
		// The series-grade pairing: the snapshot must rehash-verify against
		// the exact-skill package receipt, and the bound snapshot digest is
		// the receipt's own (the snapshot-root rehash includes controller
		// metadata, which is not the skill-package digest).
		pv, err := LoadPackageValidationReceipt(*skillPV)
		if err != nil {
			return err
		}
		if err := VerifyPackageValidationReceipt(pv, *skillSnapshot, *validator); err != nil {
			return fmt.Errorf("skill snapshot does not verify against the package-validation receipt: %w", err)
		}
		bindings.SnapshotDigest = &pv.SnapshotDigest
		bindings.PackageValidationReceiptDigest = &pv.ReceiptDigest
	} else {
		if *skillSnapshot != "" {
			// Bind the materialized snapshot digest.
			recs, err := inventoryPackage(*skillSnapshot)
			if err != nil {
				return fmt.Errorf("skill snapshot: %w", err)
			}
			d, err := FileRecordsDigest(recs)
			if err != nil {
				return err
			}
			bindings.SnapshotDigest = &d
		}
		if *skillPV != "" {
			pv, err := LoadPackageValidationReceipt(*skillPV)
			if err != nil {
				return err
			}
			bindings.PackageValidationReceiptDigest = &pv.ReceiptDigest
		}
	}
	if *suite == SuitePreHoldout {
		if *seriesRoot == "" || *candidateBinding == "" || *coreLegCompletion == "" {
			return fmt.Errorf("pre-holdout requires --series-root, --candidate-binding and --core-leg-completion")
		}
		// The series manifest digest is read from the prepared series root.
		smDigest, err := seriesManifestDigestAt(*seriesRoot)
		if err != nil {
			return err
		}
		bindings.SeriesManifestDigest = &smDigest
		cb := *candidateBinding
		cl := *coreLegCompletion
		bindings.CandidateBindingDigest = &cb
		bindings.CoreLegCompletionDigest = &cl
	}
	r, err := CreateGreenTestReceipt(*suite, *validator, *out, bindings)
	if err != nil {
		return err
	}
	fmt.Printf("green-test %s: passed=%v receipt=%s\n", r.Suite, r.Passed, *out)
	return nil
}

func cmdPackageValidate(argv []string) error {
	fs := newFlagSet("package validate")
	skillDir := fs.String("skill-dir", "", "mutable skill source dir (required)")
	repoRoot := fs.String("repository-root", "", "repository root (required)")
	snapshotRoot := fs.String("snapshot-root", "", "new immutable snapshot dir (required)")
	green := fs.String("green-test-receipt", "", "formal-tooling green receipt (required)")
	validator := fs.String("validator", "scripts/validate-agent-skill.mjs", "path to the 020 validator")
	out := fs.String("out", "", "output validation receipt (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *skillDir == "" || *repoRoot == "" || *snapshotRoot == "" || *green == "" || *out == "" {
		return fmt.Errorf("--skill-dir, --repository-root, --snapshot-root, --green-test-receipt and --out are required")
	}
	rec, err := RunPackageValidate(*skillDir, *repoRoot, *snapshotRoot, *green, *validator, *out)
	if err != nil {
		return err
	}
	fmt.Printf("package validate: snapshot=%s skill_digest=%s receipt=%s\n", rec.SnapshotID, rec.SkillDigest, *out)
	return nil
}

func sortedStringsOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cmdValidateV2 is the split/phase-aware validate.
func cmdValidateV2(argv []string) error {
	fs := newFlagSet("validate")
	datasetDir := fs.String("dataset", "skills/engram/evals", "dataset directory")
	split := fs.String("split", "dev-regression", "dev-regression|holdout")
	phase := fs.String("phase", "pre-index", "pre-index|family-aware")
	familyIndex := fs.String("dev-family-index", "", "frozen family index (family-aware)")
	manifest := fs.String("manifest", "", "manifest file (default <split> manifest in dataset dir)")
	out := fs.String("out", "", "write a sealed validation receipt JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *manifest == "" {
		switch *split {
		case SplitDevRegression:
			*manifest = filepath.Join(*datasetDir, "dev-regression-core.manifest.json")
		case SplitHoldout:
			*manifest = filepath.Join(*datasetDir, "manifest.json")
		default:
			return fmt.Errorf("split %q invalid", *split)
		}
	}
	if *split == SplitHoldout {
		if *phase != "" {
			return fmt.Errorf("phase %q applies to the dev split only (holdout validates its sealed matrix)", *phase)
		}
		// The sealed holdout dataset lives under the protected root; its
		// payload/manifest pair is validated by the holdout matrix/seal pass.
		root := filepath.Dir(filepath.Dir(*manifest))
		rep := HoldoutValidation(root, *manifest)
		for _, l := range rep.Lines {
			fmt.Println(l)
		}
		if !rep.OK {
			return fmt.Errorf("holdout validation failed")
		}
		return nil
	}
	core, err := LoadCoreV2(*datasetDir, *manifest)
	if err != nil {
		return err
	}
	var rep ValidationReport
	switch *phase {
	case "pre-index":
		if *split != SplitDevRegression {
			return fmt.Errorf("pre-index phase applies to the dev split")
		}
		rep = PreIndexValidation(core)
	case "family-aware":
		if *familyIndex == "" {
			return fmt.Errorf("family-aware validation requires --dev-family-index")
		}
		idx, err := LoadDevFamilyIndex(*familyIndex)
		if err != nil {
			return err
		}
		rep = FamilyAwareValidation(core, idx)
	default:
		return fmt.Errorf("phase %q invalid", *phase)
	}
	for _, l := range rep.Lines {
		fmt.Println(l)
	}
	if *out != "" {
		receipt := ValidationReceipt{
			SchemaVersion: 1, Split: *split, Phase: *phase,
			ManifestPath: *manifest, ManifestDigest: sha256Hex([]byte(*manifest)),
			Passed: rep.OK, Lines: rep.Lines,
			CreatedAt: nowRFC3339(), ReceiptDigest: "",
		}
		d, err := CanonicalSHA256(receipt)
		if err != nil {
			return err
		}
		receipt.ReceiptDigest = d
		if err := WriteFrozenFile(*out, mustCanonicalJSON(receipt)); err != nil {
			return err
		}
		fmt.Printf("validation receipt: %s\n", *out)
	}
	if !rep.OK {
		return fmt.Errorf("dataset validation failed")
	}
	return nil
}

// ValidationReceipt is the sealed validation outcome.
type ValidationReceipt struct {
	SchemaVersion  int      `json:"schema_version"`
	Split          string   `json:"split"`
	Phase          string   `json:"phase"`
	ManifestPath   string   `json:"manifest_path"`
	ManifestDigest string   `json:"manifest_digest"`
	Passed         bool     `json:"passed"`
	Lines          []string `json:"lines"`
	CreatedAt      string   `json:"created_at"`
	ReceiptDigest  string   `json:"receipt_digest"`
}

// cmdRunV2 is the mode-aware run router.
func cmdRunV2(argv []string) error {
	// A primary leg carries flags of its own (--series, --run-ordinal, …);
	// forward the raw argv before this router's diagnostic flag set sees it.
	for _, a := range argv {
		if a == "--mode" || strings.HasPrefix(a, "--mode=") {
			if strings.Contains(a, "primary") {
				return cmdRunPrimary(argv)
			}
			break
		}
	}
	// "--mode primary <rest>" (separate argv words) also routes here.
	for i, a := range argv {
		if a == "--mode" && i+1 < len(argv) && argv[i+1] == "primary" {
			return cmdRunPrimary(argv)
		}
	}
	fs := newFlagSet("run")
	mode := fs.String("mode", "", "primary|diagnostic (required)")
	split := fs.String("split", "", "dev-regression|holdout (required for v2 modes)")
	datasetDir := fs.String("dataset", "", "dataset directory")
	manifest := fs.String("manifest", "", "manifest file (diagnostic dev runs)")
	tool := fs.String("tool", "claude", "host tool")
	out := fs.String("out", "", "output root (required, unique)")
	scratch := fs.String("scratch", "", "scratch root (required, unique)")
	binDir := fs.String("bin-dir", "", "bin dir with engram/engram-mcp")
	concurrency := fs.Int("concurrency", 0, "explicit worker count (required)")
	only := fs.String("only", "", "comma-separated case ids (diagnostic)")
	sample := fs.Int("sample", 0, "stride sample size (diagnostic)")
	limit := fs.Int("limit", 0, "case limit (diagnostic)")
	includeExtension := fs.Bool("include-extension", false, "include the append-only extension (diagnostic)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *mode == "" {
		return fmt.Errorf("--mode primary|diagnostic is required")
	}
	switch *mode {
	case "diagnostic":
		if *datasetDir == "" || *out == "" || *scratch == "" || *binDir == "" {
			return fmt.Errorf("--dataset, --out, --scratch and --bin-dir are required")
		}
		if *manifest == "" {
			*manifest = filepath.Join(*datasetDir, "dev-regression-core.manifest.json")
		}
		opts := DiagnosticOptions{
			Split: *split, Tool: *tool, DatasetDir: *datasetDir, ManifestPath: *manifest,
			BinDir: *binDir, OutRoot: *out, ScratchRoot: *scratch, Concurrency: *concurrency,
			Only: splitTools(*only), Sample: *sample, Limit: *limit,
			IncludeExtension: *includeExtension,
		}
		rec, err := RunDiagnostic(opts)
		if err != nil {
			if rec != nil {
				for _, v := range rec.Verdicts {
					fmt.Printf("%s: pass=%v failure=%s detail=%s\n", v.CaseID, v.Pass, v.Failure, v.Detail)
				}
			}
			return err
		}
		for _, v := range rec.Verdicts {
			fmt.Printf("%s: pass=%v failure=%s detail=%s\n", v.CaseID, v.Pass, v.Failure, v.Detail)
		}
		fmt.Printf("diagnostic run: cases=%d max_in_flight=%d overlap=%v score_eligible=%v\n",
			rec.CaseCount, rec.ObservedMaxInFlight, rec.ObservedOverlap, rec.FormalScoreEligible)
		return nil
	case "primary":
		return cmdRunPrimary(fs.Args())
	default:
		return fmt.Errorf("mode %q invalid", *mode)
	}
}

// cmdFamilyIndexBuild freezes the CLI-reviewed dev family index.
func cmdFamilyIndexBuild(argv []string) error {
	fs := newFlagSet("family-index build")
	datasetDir := fs.String("dataset", "", "dev dataset dir (required)")
	coreManifest := fs.String("core-manifest", "", "dev-regression-core.manifest.json (required)")
	reviewPrompt := fs.String("review-prompt", "", "dev-family-index-review-v1 prompt file (required)")
	out := fs.String("out", "", "output dev-family-index.json (required)")
	tools := fs.String("tool", "claude,codex,opencode", "three review lanes")
	claudeSettings := fs.String("claude-settings", "", "claude aly_qwen_w settings path")
	codexProvider := fs.String("codex-provider", "", "codex model_provider override (required; e.g. current maintainer config: aq)")
	codexModel := fs.String("codex-model", "", "codex model id (required; e.g. current maintainer config: qwen3.8-flash)")
	opencodeModel := fs.String("opencode-model", "", "confirmed free model id")
	concurrency := fs.Int("concurrency", 0, "frozen worker count (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *datasetDir == "" || *coreManifest == "" || *reviewPrompt == "" || *out == "" || *concurrency < 1 {
		return fmt.Errorf("--dataset, --core-manifest, --review-prompt, --out and explicit --concurrency >= 1 are required")
	}
	lanes := splitTools(*tools)
	if len(lanes) != 3 {
		return fmt.Errorf("--tool must name exactly three review lanes")
	}
	core, err := LoadCoreV2(*datasetDir, *coreManifest)
	if err != nil {
		return err
	}
	promptBytes, err := os.ReadFile(*reviewPrompt)
	if err != nil {
		return err
	}
	promptDigest, err := LFNormalizedSHA256(promptBytes)
	if err != nil {
		return err
	}
	promptReceipt := AuthoringPromptReceipt{
		PromptID: DevFamilyIndexReviewPromptID, Version: 1,
		DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: promptDigest,
		QuotaPlanDigest: "n/a-family-index",
	}
	manifestDigest, err := CanonicalSHA256(core.Manifest)
	if err != nil {
		return err
	}
	payloadDigest, err := DatasetPayloadDigest(*datasetDir, core.Manifest)
	if err != nil {
		return err
	}
	review := CLIMirrorReview(CLIReviewConfig{
		Lanes: lanes, ClaudeSettings: *claudeSettings,
		CodexProvider: *codexProvider, CodexModel: *codexModel,
		OpenCodeModel: *opencodeModel, PromptFile: *reviewPrompt,
	}, BlindCaseProjection(core))
	idx, err := DeriveDevFamilyIndex(core, promptReceipt,
		FamilyDerivationOptions{Concurrency: *concurrency, Lanes: lanes, Review: review,
			Progress: func(done, total int, _ string, joined bool) {
				fmt.Fprintf(os.Stderr, "progress: %d/%d pairs joined=%v\n", done, total, joined)
			}},
		payloadDigest, manifestDigest)
	if err != nil {
		return err
	}
	if err := SaveDevFamilyIndex(*out, idx); err != nil {
		return err
	}
	fmt.Printf("family-index: %d families over %d cases; pairs=%d max_in_flight=%d overlap=%v digest=%s\n",
		len(idx.FamilyIDs), len(idx.CaseToFamily), idx.DerivationReceipt.MirrorPairCount,
		idx.DerivationReceipt.ObservedMaxInFlight, idx.DerivationReceipt.ObservedOverlap,
		idx.DerivationReceipt.IndexDigest)
	return nil
}

// routeOrLegacy decides between v2 and legacy command surfaces.
func routeOrLegacy(args []string) error {
	if len(args) > 0 {
		if handled, err := routeV2(args); handled {
			return err
		}
		switch args[0] {
		case "validate":
			// v2 flags present → v2 path; plain legacy invocation stays.
			rest := args[1:]
			for _, a := range rest {
				if a == "--split" || a == "--phase" || a == "--dev-family-index" || a == "--manifest" || a == "--out" {
					return cmdValidateV2(rest)
				}
			}
			return cmdValidate(rest)
		case "run":
			rest := args[1:]
			for _, a := range rest {
				if a == "--mode" {
					return cmdRunV2(rest)
				}
			}
			return cmdRun(rest)
		}
	}
	fmt.Print(usageV2)
	return nil
}

// seriesManifestDigestAt reads the prepared series manifest digest from a
// series root (placeholder until T045 wires FormalSeriesManifest).
func seriesManifestDigestAt(seriesRoot string) (string, error) {
	p := filepath.Join(seriesRoot, "series-manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("series root %s has no prepared series-manifest.json: %w", seriesRoot, err)
	}
	return sha256Hex(b), nil
}

var _ = strings.TrimSpace
