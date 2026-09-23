package main

import (
	"bytes"
	"os"
	"os/exec"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

// 048 v2 runner additions (runner-cli.md; data-model.md §10): seed rendering
// with event_date honesty, safe workspace materialization, and the minimum
// dev-only diagnostic runner with unique roots, a bounded worker pool and
// permanently false formal-score eligibility.

var eventDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// SeedRenderResult records how one SeedMemory was materialized.
type SeedRenderResult struct {
	Name                     string `json:"name"`
	SourceEventDate          *string `json:"source_event_date"`
	RenderedContentDigest    string `json:"rendered_content_digest"`
	EngineEventDateSupported bool   `json:"engine_event_date_supported"`
}

// RenderSeedContent applies the frozen contract (dataset-protocol §6): a
// non-null SeedMemory.event_date is validated as YYYY-MM-DD and rendered as a
// deterministic `[event_date=YYYY-MM-DD]` content prefix through the existing
// CLI path. It is never silently dropped and never claimed to populate
// structured engine EventDate.
func RenderSeedContent(s SeedMemory) (string, SeedRenderResult, error) {
	rendered := s.Content
	res := SeedRenderResult{Name: s.Name, SourceEventDate: s.EventDate}
	if s.EventDate != nil {
		if !eventDateRE.MatchString(*s.EventDate) {
			return "", res, fmt.Errorf("seed %s event_date %q is not YYYY-MM-DD", s.Name, *s.EventDate)
		}
		rendered = fmt.Sprintf("[event_date=%s] %s", *s.EventDate, s.Content)
	}
	res.RenderedContentDigest = sha256Hex([]byte(rendered))
	res.EngineEventDateSupported = false // public CLI/MCP accept no structured event date
	return rendered, res, nil
}

// MaterializeWorkspace stages workspace_files under caseDir with containment
// and digest verification (data-model.md WorkspaceFile rules).
func MaterializeWorkspace(caseDir string, files []WorkspaceFile) error {
	for _, f := range files {
		if UnsafePath(f.Path) {
			return fmt.Errorf("workspace file %q is not containment-safe", f.Path)
		}
		dst, err := EnsureInside(caseDir, filepath.Join(caseDir, filepath.FromSlash(f.Path)))
		if err != nil {
			return err
		}
		if f.SHA256 != "" && sha256Hex([]byte(f.Content)) != f.SHA256 {
			return fmt.Errorf("workspace file %s sha256 mismatch", f.Path)
		}
		if err := osWriteFile(dst, []byte(f.Content)); err != nil {
			return err
		}
	}
	return nil
}

// DiagnosticRunReceipt is the diagnostic artifact manifest. It is permanently
// score-ineligible (FormalScoreEligible=false) — a structural fact, not a
// promise.
type DiagnosticRunReceipt struct {
	SchemaVersion        int      `json:"schema_version"`
	Mode                 string   `json:"mode"`
	Split                string   `json:"split"`
	Tool                 string   `json:"tool"`
	DatasetDir           string   `json:"dataset_dir"`
	ManifestPath         string   `json:"manifest_path"`
	OutRoot              string   `json:"out_root"`
	ScratchRoot          string   `json:"scratch_root"`
	Concurrency          int      `json:"concurrency"`
	ObservedMaxInFlight  int      `json:"observed_max_in_flight"`
	ObservedOverlap      bool     `json:"observed_overlap"`
	CaseCount            int      `json:"case_count"`
	Verdicts             []Verdict `json:"verdicts"`
	FormalScoreEligible  bool     `json:"formal_score_eligible"`
	RunnerDigest         string   `json:"runner_digest"`
	CreatedAt            string   `json:"created_at"`
}

// DiagnosticOptions configures one dev-only diagnostic run.
type DiagnosticOptions struct {
	Split          string // dev-regression only
	Tool           string
	DatasetDir     string
	ManifestPath   string // dev-regression-core.manifest.json
	BinDir         string
	OutRoot        string // must not exist (unique root)
	ScratchRoot    string // must not exist (unique root)
	Concurrency    int    // required explicit
	Only           []string
	Sample         int
	Limit          int
	IncludeExtension bool
}

// caseChildRunner executes one v2 case against one prepared case dir and
// returns (raw stdout, run error). Production spawns the real host CLI; tests
// inject deterministic children.
type caseChildRunner func(tool string, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error)

var v2ChildRunner caseChildRunner = runV2Child

// RunDiagnostic executes the minimum dev-only diagnostic run. It refuses
// reused roots, requires explicit positive concurrency, honors it with a
// bounded worker pool, and writes a permanently score-ineligible receipt.
func RunDiagnostic(opts DiagnosticOptions) (*DiagnosticRunReceipt, error) {
	if opts.Split != SplitDevRegression {
		return nil, fmt.Errorf("diagnostic mode is dev-only: split %q is rejected (holdout diagnostics are always invalid)", opts.Split)
	}
	if opts.Concurrency < 1 {
		return nil, fmt.Errorf("diagnostic mode requires an explicit --concurrency >= 1")
	}
	if _, err := os.Stat(opts.OutRoot); err == nil {
		return nil, fmt.Errorf("out root %s already exists — diagnostic runs require unique roots", opts.OutRoot)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := os.Stat(opts.ScratchRoot); err == nil {
		return nil, fmt.Errorf("scratch root %s already exists — diagnostic runs require unique roots", opts.ScratchRoot)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	core, err := LoadCoreV2(opts.DatasetDir, opts.ManifestPath)
	if err != nil {
		return nil, err
	}
	// The child-runner injection point has no options struct; binDir flows
	// through the process env (same ENGRAM_SKILL_EVAL_* family).
	os.Setenv("ENGRAM_SKILL_EVAL_BIN_DIR", opts.BinDir)
	// Selector: only / sample / limit (diagnostic affordances).
	ids := sortedKeys(core.Cases)
	if len(opts.Only) > 0 {
		want := map[string]bool{}
		for _, id := range opts.Only {
			want[id] = true
		}
		var kept []string
		for _, id := range ids {
			if want[id] {
				kept = append(kept, id)
				delete(want, id)
			}
		}
		if len(want) > 0 {
			return nil, fmt.Errorf("unknown case ids in --only: %v", sortedKeys(want))
		}
		ids = kept
	}
	if opts.Sample > 0 && opts.Sample < len(ids) {
		stride := len(ids) / opts.Sample
		var sampled []string
		for i := 0; i < len(ids) && len(sampled) < opts.Sample; i += stride {
			sampled = append(sampled, ids[i])
		}
		ids = sampled
	}
	if opts.Limit > 0 && opts.Limit < len(ids) {
		ids = ids[:opts.Limit]
	}

	for _, d := range []string{opts.OutRoot, filepath.Join(opts.OutRoot, "raw"), opts.ScratchRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	// Bounded worker pool with observed max-in-flight/overlap: the same
	// runBounded implementation the formal primary mode uses, so the
	// diagnostic mode cannot drift from the sealed-concurrency contract.
	verdicts := make([]Verdict, len(ids))
	maxInFlight, overlap, firstErr := runBounded(opts.Concurrency, len(ids), func(idx int, _ int) error {
		caseID := ids[idx]
		v, err := runV2Case(opts, core.Cases[caseID])
		if err != nil {
			v = Verdict{CaseID: caseID, Failure: "runner-error", Detail: err.Error()}
		}
		verdicts[idx] = v
		return err
	})

	receipt := &DiagnosticRunReceipt{
		SchemaVersion: 1, Mode: "diagnostic", Split: opts.Split, Tool: opts.Tool,
		DatasetDir: opts.DatasetDir, ManifestPath: opts.ManifestPath,
		OutRoot: opts.OutRoot, ScratchRoot: opts.ScratchRoot,
		Concurrency: opts.Concurrency, ObservedMaxInFlight: maxInFlight,
		ObservedOverlap: overlap,
		CaseCount:       len(ids),
		FormalScoreEligible: false, // structural: diagnostics never score
	}
	runnerDigest, err := CurrentRunnerDigest()
	if err == nil {
		receipt.RunnerDigest = runnerDigest
	}
	receipt.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	for _, v := range verdicts {
		if v.CaseID != "" {
			receipt.Verdicts = append(receipt.Verdicts, v)
		}
	}
	if firstErr != nil {
		return receipt, firstErr
	}
	return receipt, nil
}

// runV2Case executes one case: workspace, seeds, child, events, store dump,
// deterministic judge.
func runV2Case(opts DiagnosticOptions, c *TriggerCaseV2) (Verdict, error) {
	caseDir := filepath.Join(opts.ScratchRoot, "data", c.ID)
	// Absolute from here on: downstream consumers treat caseDir as absolute
	// (claude resolves --mcp-config against its own cwd, opencode2 resolves
	// its directory argument against process.cwd, engram-mcp needs an
	// absolute --data-dir). A relative --scratch broke all three at once.
	if abs, aerr := filepath.Abs(caseDir); aerr == nil {
		caseDir = abs
	}
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		return Verdict{}, err
	}
	writeWorkspaceTemplate(caseDir)
	if err := MaterializeWorkspace(caseDir, c.WorkspaceFiles); err != nil {
		return Verdict{}, err
	}
	if opts.Tool == HostOpenCode {
		writeOpenCodeConfig(caseDir, opts.BinDir) // custom provider via env; key stays in {env:}
	}
	engramBin := filepath.Join(opts.BinDir, "engram")
	seedReceipts := []SeedRenderResult{}
	for _, s := range c.SeedMemories {
		content, rr, err := RenderSeedContent(s)
		if err != nil {
			return Verdict{}, err
		}
		cmd := newSeedCommand(engramBin, caseDir, s.Name, content)
		if out, err := cmd.CombinedOutput(); err != nil {
			return Verdict{}, fmt.Errorf("seed %s failed: %v: %s", s.Name, err, truncate(string(out), 200))
		}
		seedReceipts = append(seedReceipts, rr)
	}
	userTurn := *c.Prompt
	if c.Prompt == nil {
		for _, t := range c.Turns {
			if t.Role == "user" && !t.SetupOnly {
				userTurn = t.Content
			}
		}
	}
	raw, runErr := v2ChildRunner(opts.Tool, caseDir, userTurn, c)
	if runErr != nil && raw == nil {
		// Terminal child failure is judged as runner-error below.
		raw = []byte{}
	}
	if err := osWriteFile(filepath.Join(opts.OutRoot, "raw", c.ID+".jsonl"), raw); err != nil {
		return Verdict{}, err
	}
	var events []Event
	switch opts.Tool {
	case HostClaude:
		events = ParseClaude(bytes.NewReader(raw))
	case HostCodex:
		events = ParseCodex(bytes.NewReader(raw))
	case HostOpenCode:
		events = ParseOpenCode(bytes.NewReader(raw))
	default:
		return Verdict{}, fmt.Errorf("unknown tool %q", opts.Tool)
	}
	_ = seedReceipts // recorded in verbose seed receipts (diagnostic artifact)
	storeDump := ""
	if out, err := newStoreDumpCommand(engramBin, caseDir).Output(); err == nil {
		storeDump = string(out)
	}
	return JudgeV2(c, events, storeDump, runErr), nil
}

func newSeedCommand(engramBin, dataDir, name, content string) *exec.Cmd {
	cmd := exec.Command(engramBin, "--data-dir", dataDir, "add", "--name", name, "--content", content)
	cmd.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+dataDir)
	return cmd
}

func newStoreDumpCommand(engramBin, dataDir string) *exec.Cmd {
	cmd := exec.Command(engramBin, "--data-dir", dataDir, "list")
	cmd.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+dataDir)
	return cmd
}

// runV2Child spawns the real host CLI for one diagnostic case (production
// implementation; tests replace v2ChildRunner).
func runV2Child(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
	binDir := os.Getenv("ENGRAM_SKILL_EVAL_BIN_DIR") // set by RunDiagnostic; CLI resolution also via PATH
	var cmd *exec.Cmd
	switch tool {
	case HostClaude:
		settings := os.Getenv("ENGRAM_SKILL_EVAL_CLAUDE_SETTINGS")
		if settings == "" {
			settings = filepath.Join(os.Getenv("HOME"), ".claude", "settings.json.aly_qwen_w")
		}
		// Per-case MCP config (never a shared path — parallel claude runs
		// would race on one file and funnel every store into the last
		// writer's caseDir). Without --mcp-config the claude lane cannot
		// reach engram MCP at all.
		mcpCfg := filepath.Join(filepath.Dir(filepath.Dir(caseDir)), "run", "mcp-claude-"+filepath.Base(caseDir)+".json")
		writeMCPConfig(mcpCfg, filepath.Join(binDir, "engram-mcp"), caseDir)
		args := []string{"claude", "--settings", settings, "--mcp-config", mcpCfg, "--allowed-tools", "mcp__engram__*", "-p", prompt, "--output-format", "stream-json", "--verbose"}
		cmd = exec.Command(args[0], args[1:]...)
	case HostCodex:
		provider := os.Getenv("ENGRAM_SKILL_EVAL_CODEX_PROVIDER")
		model := os.Getenv("ENGRAM_SKILL_EVAL_CODEX_MODEL")
		if provider == "" || model == "" {
			return nil, fmt.Errorf("codex lane requires ENGRAM_SKILL_EVAL_CODEX_PROVIDER and ENGRAM_SKILL_EVAL_CODEX_MODEL (aq / qwen3.8-flash)")
		}
		// MCP server override per case (the v1 runner's wiring): without it
		// the codex lane has no engram MCP at all.
		cmd = exec.Command("codex", "-c", "model_provider="+provider, "-c", "model="+model,
			"-c", fmt.Sprintf("mcp_servers.engram.command=%q", filepath.Join(binDir, "engram-mcp")),
			"-c", fmt.Sprintf("mcp_servers.engram.args=[\"--data-dir\",\"%s\"]", caseDir),
			"--yolo", "exec", "--json", prompt)
	case HostOpenCode:
		model := os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL")
		// opencode2 resolves its provider from the config in its RUN
		// directory (bisected 2026-09-02; provider.no-route otherwise).
		// Link the lane config into the case workspace so the per-case cwd
		// carries it — the same fix the canary lane runner got.
		if cfgWs, err := materializeOpenCodeLaneWorkspace(); err == nil {
			link := filepath.Join(caseDir, "opencode.json")
			if err := os.Symlink(filepath.Join(cfgWs, "opencode.json"), link); err != nil && !os.IsExist(err) {
				return nil, err
			}
		} else {
			return nil, err
		}
		args := []string{"opencode2", "run", "--standalone", "--auto", "--format", "json"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt, caseDir)
		cmd = exec.Command(args[0], args[1:]...)
		// Whitelist env: the inherited shell environment (leftover
		// ANTHROPIC_*/model-provider vars, agent markers) breaks opencode2's
		// provider routing with provider.no-route even when the project
		// opencode.json is correct (2026-09-01 minimal-env bisect). Only the
		// variables the lane genuinely needs survive.
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"LANG=" + os.Getenv("LANG"),
			"TMPDIR=" + os.Getenv("TMPDIR"),
			"MAAS_API_KEY=" + os.Getenv("MAAS_API_KEY"),
			"ENGRAM_DATA_DIR=" + caseDir,
		}
	default:
		return nil, fmt.Errorf("unknown tool %q", tool)
	}
	cmd.Dir = caseDir
	if cmd.Env == nil { // a lane may pin a whitelist env (opencode); never clobber it
		cmd.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+caseDir)
	}
	out, err := cmd.Output()
	if err != nil && tool == HostOpenCode {
		// opencode2 exits 1 after a COMPLETED run: the model's final answer
		// and step_finish are already on stdout, then the session teardown
		// emits {"type":"error","aborted":"Session interrupted: shutdown"}
		// and the process exit code becomes 1 (2026-09-01: 32/172 cases).
		// A completed event stream outranks the exit code; a stream without
		// step_finish stays a runner error.
		if bytes.Contains(out, []byte(`"type":"step_finish"`)) {
			return out, nil
		}
	}
	return out, err
}
