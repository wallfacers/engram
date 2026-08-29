package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ToolConfig describes one agent CLI runner and its fixed invocation contract.
// A spec may carry a @variant suffix (codex@ds, claude@opus): the variant
// names a codex profile (model-matrix backends) and namespaces the store
// container and report label so different backends never collide.
type ToolConfig struct {
	Name    string // claude | codex | opencode (base name, selects the parser)
	Base    string // == Name; kept for readability at call sites
	Variant string // optional @variant suffix
	Label   string // reporting label: name or name@variant
	DataDir string // per-tool container for per-case isolated stores
	Cwd     string // working directory for the CLI (when not per-case)
	// PerCaseCwd: the CLI runs inside its per-case dir (claude: host
	// auto-memory is keyed by cwd; opencode: project config lives there).
	PerCaseCwd bool
	// build constructs the command for one prompt against one isolated
	// per-case store dir. stdin is /dev/null; stdout is the event stream.
	build func(tc ToolConfig, binDir, settingsFile, caseDir, prompt string) *exec.Cmd
}

// parseToolSpec splits "name@variant" into its parts and the report label.
func parseToolSpec(spec string) (name, variant, label string) {
	name, variant = spec, ""
	if i := strings.Index(spec, "@"); i > 0 {
		name, variant = spec[:i], spec[i+1:]
	}
	label = name
	if variant != "" {
		label = name + "@" + variant
	}
	return name, variant, label
}

// RunConfig carries everything a full run needs.
type RunConfig struct {
	BinDir      string // holds engram + engram-mcp
	Scratch     string // scratch root (mcp configs, raw output)
	OutDir      string // reports
	Timeout     time.Duration
	CaseLimit   int
	Concurrency int
	Datasets    map[string]*Dataset
	Tools       []ToolConfig

	engramBin string
	cases     []Case
	allCases  []Case
}

// NewRunConfig prepares directories and the flattened case list (dataset
// order: implicit-write, implicit-read, regression last).
func NewRunConfig(binDir, scratch, outDir string, timeoutSec, caseLimit int, datasets map[string]*Dataset) (*RunConfig, error) {
	for _, p := range []string{outDir, filepath.Join(outDir, "raw")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	cfg := &RunConfig{
		BinDir: binDir, Scratch: scratch, OutDir: outDir,
		Timeout:   time.Duration(timeoutSec) * time.Second,
		CaseLimit: caseLimit, Datasets: datasets,
		engramBin: filepath.Join(binDir, "engram"),
	}
	for _, name := range []string{"implicit-write", "implicit-read", "regression"} {
		cfg.cases = append(cfg.cases, datasets[name].Cases...)
	}
	cfg.allCases = append([]Case(nil), cfg.cases...)
	if caseLimit > 0 && caseLimit < len(cfg.cases) {
		cfg.cases = cfg.cases[:caseLimit]
	}
	for _, c := range cfg.allCases {
		moduleRegistry[c.ID] = c.Module
	}
	return cfg, nil
}

// ApplyOnly filters the case list to explicit ids (single-case retry mode —
// cheap flywheel iterations never re-run the whole dataset).
func (c *RunConfig) ApplyOnly(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var kept []Case
	for _, cs := range c.cases {
		if want[cs.ID] {
			kept = append(kept, cs)
			delete(want, cs.ID)
		}
	}
	if len(want) > 0 {
		var missing []string
		for id := range want {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return fmt.Errorf("unknown case ids: %v", missing)
	}
	c.cases = kept
	return nil
}

// SampleRegression adds a deterministic stride sample of the remaining
// dataset to the selected case list, so a cheap retry still guards against
// the skill revision breaking previously-passing cases (over-fitting check)
// without a full re-run.
func (c *RunConfig) SampleRegression(n int) {
	if n <= 0 || len(c.allCases) == 0 {
		return
	}
	have := map[string]bool{}
	for _, cs := range c.cases {
		have[cs.ID] = true
	}
	stride := len(c.allCases)/n + 1
	taken := 0
	for i := 0; i < len(c.allCases) && taken < n; i++ {
		if i%stride != 0 {
			continue
		}
		cs := c.allCases[i]
		if have[cs.ID] {
			continue
		}
		c.cases = append(c.cases, cs)
		have[cs.ID] = true
		taken++
	}
}

// AddTool registers one tool runner; unknown names return an error lazily at
// run time through the unavailable marker.
func (c *RunConfig) AddTool(spec string) {
	name, variant, label := parseToolSpec(spec)
	tc := ToolConfig{Name: name, Base: name, Variant: variant, Label: label,
		DataDir: filepath.Join(c.Scratch, "data", label)}
	switch name {
	case "claude":
		// Per-case cwd: Claude Code keys its host auto-memory to the project
		// path; a fresh directory every case prevents facts written in one
		// case from satisfying (and thus suppressing) engram writes in the
		// next — the round-1 auto-memory contamination, now per-case.
		tc.PerCaseCwd = true
		tc.build = func(tc ToolConfig, binDir, settings, caseDir, prompt string) *exec.Cmd {
			mcpCfg := filepath.Join(c.Scratch, "run", "mcp-claude-"+tc.Label+".json")
			writeMCPConfig(mcpCfg, filepath.Join(binDir, "engram-mcp"), caseDir)
			args := []string{"--settings", settings, "--mcp-config", mcpCfg,
				"--allowed-tools", "mcp__engram__*"}
			// Optional host-model override (model-matrix axis): the variant
			// names a claude model slot (claude@opus → --model opus, resolved
			// through --settings); plain claude falls back to the env override.
			m := tc.Variant
			if m == "" {
				m = os.Getenv("ENGRAM_SKILL_EVAL_CLAUDE_MODEL")
			}
			if m != "" {
				args = append(args, "--model", m)
			}
			args = append(args, "-p", prompt, "--output-format", "stream-json", "--verbose")
			return exec.Command("claude", args...)
		}
	case "codex":
		tc.Cwd = c.Scratch
		profile := variant
		if profile == "" {
			profile = "tf"
		}
		tc.build = func(tc ToolConfig, binDir, settings, caseDir, prompt string) *exec.Cmd {
			cmd := exec.Command("codex", "-p", profile, "--yolo", "exec", "--json",
				"-c", fmt.Sprintf("mcp_servers.engram.command=%q", filepath.Join(binDir, "engram-mcp")),
				"-c", fmt.Sprintf("mcp_servers.engram.args=[\"--data-dir\",\"%s\"]", caseDir),
				prompt)
			return cmd
		}
	case "opencode":
		tc.PerCaseCwd = true // project opencode.json is generated per case
		tc.build = func(tc ToolConfig, binDir, settings, caseDir, prompt string) *exec.Cmd {
			// --standalone: private server per invocation — the shared
			// background service is a serialization point whose sessions
			// stall a worker pool when a client is killed on timeout.
			return exec.Command("opencode2", "run", "--standalone", "--auto", "--format", "json", prompt, caseDir)
		}
	}
	c.Tools = append(c.Tools, tc)
}

// writeWorkspaceTemplate plants a minimal project-looking directory: task-
// implied cases ("install the project's dependencies") need one, while a
// fresh empty dir suppresses them.
func writeWorkspaceTemplate(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("# workspace\n\nSmall demo workspace used for assistant sessions.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte("{\n  \"name\": \"demo-workspace\",\n  \"version\": \"0.1.0\",\n  \"scripts\": {\"test\": \"echo no tests\"}\n}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() { println(\"demo\") }\n"), 0o644)
}

// writeMCPConfig emits the claude --mcp-config JSON for one case store.
func writeMCPConfig(path, engramMCP, dataDir string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"engram": map[string]any{
				"type":    "stdio",
				"command": engramMCP,
				"args":    []string{"--data-dir", dataDir},
			},
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
}

// writeOpenCodeConfig emits the OpenCode v2 project config into a case dir:
// the engram MCP server wired to that case's isolated store, plus — when
// ENGRAM_SKILL_EVAL_OPENCODE_MODEL is set (form "provider/model") — a custom
// model provider (OpenAI-compatible; key flows through {env:} templating,
// never into this file).
func writeOpenCodeConfig(dir, binDir string) {
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{
			"servers": map[string]any{
				"engram": map[string]any{
					"type":    "local",
					"command": filepath.Join(binDir, "engram-mcp"),
					"args":    []string{"--data-dir", dir},
				},
			},
		},
	}
	if model := os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL"); model != "" {
		prov := model
		mid := model
		if i := strings.Index(model, "/"); i > 0 {
			prov, mid = model[:i], model[i+1:]
		}
		opts := map[string]any{"apiKey": "{env:MAAS_API_KEY}"}
		if b := os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_BASE"); b != "" {
			opts["baseURL"] = b
		}
		cfg["provider"] = map[string]any{
			prov: map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    prov,
				"options": opts,
				"models": map[string]any{
					mid: map[string]any{"name": mid},
				},
			},
		}
		cfg["model"] = model
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "opencode.json"), b, 0o644)
}

// Run executes every tool in parallel; within a tool, cases run serially
// against that tool's isolated store. Failures never abort the batch.
func Run(cfg *RunConfig) error {
	settings := os.Getenv("ENGRAM_SKILL_EVAL_SETTINGS")
	if settings == "" {
		settings = filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	}
	var wg sync.WaitGroup
	reports := make([]*ToolReport, len(cfg.Tools))
	for i := range cfg.Tools {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i] = runTool(cfg, cfg.Tools[i], settings)
		}(i)
	}
	wg.Wait()

	// Aggregate.
	agg := AggregateReport{StartedAt: time.Now().UTC().Format(time.RFC3339), CaseLimit: cfg.CaseLimit}
	for _, r := range reports {
		if r == nil {
			continue
		}
		agg.Tools = append(agg.Tools, r)
	}
	writeJSON(filepath.Join(cfg.OutDir, "run-report.json"), agg)
	writeFailures(cfg.OutDir, agg)
	fmt.Print(agg.Summary())
	return nil
}

// ToolReport is one tool's full result.
type ToolReport struct {
	Tool              string    `json:"tool"`
	Available         bool      `json:"available"`
	UnavailableReason string    `json:"unavailable_reason,omitempty"`
	Verdicts          []Verdict `json:"verdicts"`
	Duration          string    `json:"duration"`
}

// CaseResult pairs a verdict with diagnostic event counts.
type caseDiag struct {
	id  string
	ops []string
}

func runTool(cfg *RunConfig, tc ToolConfig, settings string) *ToolReport {
	rep := &ToolReport{Tool: tc.Label, Available: true}
	start := time.Now()
	defer func() { rep.Duration = time.Since(start).Round(time.Second).String() }()

	if tc.build == nil {
		rep.Available = false
		rep.UnavailableReason = "unknown tool name (expected claude|codex|opencode[@variant])"
		return rep
	}
	if !tc.PerCaseCwd {
		if err := os.MkdirAll(tc.Cwd, 0o755); err != nil {
			rep.Available = false
			rep.UnavailableReason = "cwd missing: " + err.Error()
			return rep
		}
	}

	rawDir := filepath.Join(cfg.OutDir, "raw", tc.Label)
	_ = os.MkdirAll(rawDir, 0o755)

	// Worker pool over cases. Every case runs against its own isolated store
	// (<data>/<label>/<case-id>): no cross-case seed contamination of write
	// judgements (round-3 failbook) and no single-writer SQLite contention.
	type job struct {
		idx int
		c   Case
	}
	jobs := make(chan job)
	verdicts := make([]Verdict, len(cfg.cases))
	var mu sync.Mutex
	var wg sync.WaitGroup
	failStreak := 0
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				v, _ := runCase(cfg, tc, settings, j.c, rawDir)
				verdicts[j.idx] = v
				mu.Lock()
				if v.Failure == "failed" {
					failStreak++
				} else {
					failStreak = 0
				}
				mu.Unlock()
			}
		}()
	}
	for i, c := range cfg.cases {
		mu.Lock()
		stop := failStreak >= 3
		mu.Unlock()
		if stop {
			rep.Available = false
			rep.UnavailableReason = "3 consecutive runner failures"
			break
		}
		jobs <- job{idx: i, c: c}
	}
	close(jobs)
	wg.Wait()
	// Keep every verdict collected before an unavailable break — failed
	// diagnostics must not be dropped with the report.
	rep.Verdicts = verdicts
	return rep
}

// runCase executes one case: seed the store, run the CLI, capture, parse,
// verify the store, judge.
func runCase(cfg *RunConfig, tc ToolConfig, settings string, c Case, rawDir string) (Verdict, caseDiag) {
	diag := caseDiag{id: c.ID}
	// 0. Fresh isolated store + workspace for this case alone.
	caseDir := filepath.Join(tc.DataDir, c.ID)
	_ = os.RemoveAll(caseDir)
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		return Verdict{CaseID: c.ID, Failure: "failed",
			Detail: "case dir: " + err.Error()}, diag
	}
	writeWorkspaceTemplate(caseDir)
	if tc.Base == "opencode" {
		writeOpenCodeConfig(caseDir, cfg.BinDir)
	}

	// 1. Seed deterministically via the CLI (store is per-case, so this is
	// contention-free; the retry stays as belt-and-braces).
	for _, s := range c.Seed {
		var lastErr error
		var lastOut string
		for attempt := 0; attempt < 4; attempt++ {
			cmd := exec.Command(cfg.engramBin, "--data-dir", caseDir, "add",
				"--name", s.Name, "--content", s.Content)
			cmd.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+caseDir)
			out, err := cmd.CombinedOutput()
			if err == nil {
				lastErr = nil
				break
			}
			lastErr, lastOut = err, string(out)
			time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
		}
		if lastErr != nil {
			return Verdict{CaseID: c.ID, Failure: "failed",
				Detail: fmt.Sprintf("seed %s failed: %v: %s", s.Name, lastErr, truncate(lastOut, 200))}, diag
		}
	}

	// 2. Run the agent CLI.
	cmd := tc.build(tc, cfg.BinDir, settings, caseDir, c.Prompt)
	if tc.PerCaseCwd {
		cmd.Dir = caseDir
	} else {
		cmd.Dir = tc.Cwd
	}
	cmd.Env = append(os.Environ(), "PATH="+cfg.BinDir+":"+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := runWithTimeout(cmd, cfg.Timeout)
	raw := stdout.Bytes()
	_ = os.WriteFile(filepath.Join(rawDir, c.ID+".jsonl"), raw, 0o644)

	var events []Event
	switch tc.Base {
	case "claude":
		events = ParseClaude(bytes.NewReader(raw))
	case "codex":
		events = ParseCodex(bytes.NewReader(raw))
	case "opencode":
		events = ParseOpenCode(bytes.NewReader(raw))
	}
	for _, e := range events {
		if e.Kind == EventEngramCall {
			diag.ops = append(diag.ops, e.Op)
		}
	}
	if runErr != nil {
		return Verdict{CaseID: c.ID, Failure: "failed",
			Detail: fmt.Sprintf("runner: %v (events captured: %d; stderr: %s)", runErr, len(events), truncate(stderr.String(), 300))}, diag
	}

	// 3. Store-side verification dump (post-turn).
	storeDump := ""
	if out, err := exec.Command(cfg.engramBin, "--data-dir", caseDir, "list").Output(); err == nil {
		storeDump = string(out)
	}

	// 4. Judge (deterministic).
	v := Judge(c, events, storeDump)
	v.Tool = tc.Label
	return v, diag
}

func runWithTimeout(cmd *exec.Cmd, d time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("timeout after %s", d)
	}
}

// AggregateReport is the run's top-level output.
type AggregateReport struct {
	StartedAt string        `json:"started_at"`
	CaseLimit int           `json:"case_limit"`
	Tools     []*ToolReport `json:"tools"`
}

func (a AggregateReport) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "skill-eval run %s\n", a.StartedAt)
	for _, t := range a.Tools {
		if !t.Available {
			fmt.Fprintf(&b, "[%s] RUNNER-UNAVAILABLE: %s (partial verdicts below)\n", t.Tool, t.UnavailableReason)
			if len(t.Verdicts) == 0 {
				continue
			}
		}
		mod := map[string][2]int{} // module -> {pass, total}
		for _, v := range t.Verdicts {
			m := moduleOf(a, v)
			p := mod[m]
			p[1]++
			if v.Pass {
				p[0]++
			}
			mod[m] = p
		}
		fmt.Fprintf(&b, "[%s] %s — ", t.Tool, t.Duration)
		var names []string
		for m := range mod {
			names = append(names, m)
		}
		sort.Strings(names)
		for _, m := range names {
			p := mod[m]
			fmt.Fprintf(&b, "%s %d/%d  ", m, p[0], p[1])
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

func moduleOf(_ AggregateReport, v Verdict) string {
	if m, ok := moduleRegistry[v.CaseID]; ok {
		return m
	}
	return "unknown"
}

// writeFailures emits failures.jsonl for the flywheel archive step.
func writeFailures(outDir string, agg AggregateReport) {
	// Module lookup across datasets is injected via global registry set by Run.
	var b strings.Builder
	for _, t := range agg.Tools {
		if !t.Available && len(t.Verdicts) == 0 {
			continue
		}
		for _, v := range t.Verdicts {
			if v.Pass {
				continue
			}
			f := map[string]any{
				"case_id": v.CaseID, "tool": t.Tool, "failure": v.Failure,
				"detail": v.Detail, "module": moduleRegistry[v.CaseID],
				"root_cause": "",
			}
			jb, _ := json.Marshal(f)
			b.Write(jb)
			b.WriteByte('\n')
		}
	}
	_ = os.WriteFile(filepath.Join(outDir, "failures.jsonl"), []byte(b.String()), 0o644)
}

// moduleRegistry maps case id → module (populated by Run from the datasets).
var moduleRegistry = map[string]string{}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
