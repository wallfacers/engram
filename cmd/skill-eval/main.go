// Command skill-eval is the trigger-evaluation runner for the engram agent
// skill. It drives real agent CLIs (claude / codex / opencode) in
// non-interactive mode against a versioned trigger dataset, judges each case
// deterministically from the runner's observable output (engram operation
// traces plus store-side verification), and emits per-tool reports that feed
// the skill data flywheel.
//
// It is an adapter-side harness: it never imports engine internals beyond the
// public CLI/MCP surfaces and spawns external binaries only.
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `skill-eval — engram skill trigger evaluation runner (specs/048-implicit-memory-flywheel)

Usage:
  skill-eval validate --dataset <dir>          validate dataset structure and balance gates
  skill-eval run     --dataset <dir> --out <dir> [--tool name[@variant][,...]] [--timeout 240s]
                     [--bin-dir <dir>] [--scratch <dir>]

Tools are driven through fixed per-tool invocation contracts (see runner.go).
A @variant suffix namespaces a backend: codex@ds selects the codex profile
"ds" (model-matrix axis), claude@opus labels a claude run using the host
model from ENGRAM_SKILL_EVAL_CLAUDE_MODEL. Every case runs against its own
isolated store under <scratch>/data/<label>/<case-id>.
The judge is deterministic: re-judging the same raw output yields the same verdict.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	datasetDir := fs.String("dataset", "skills/engram/evals", "directory containing the trigger dataset JSON files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	datasets, err := LoadDatasets(*datasetDir)
	if err != nil {
		return err
	}
	report := ValidateDatasets(datasets)
	for _, l := range report.Lines {
		fmt.Println(l)
	}
	if !report.OK {
		return fmt.Errorf("dataset validation failed")
	}
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	datasetDir := fs.String("dataset", "skills/engram/evals", "directory containing the trigger dataset JSON files")
	outDir := fs.String("out", "", "output directory for reports (required)")
	tools := fs.String("tool", "claude,codex,opencode", "comma-separated tool list")
	timeoutSec := fs.Int("timeout", 240, "per-case timeout in seconds")
	binDir := fs.String("bin-dir", "", "directory containing the engram and engram-mcp binaries (required)")
	scratch := fs.String("scratch", "", "scratch root for per-tool data dirs and MCP configs (required)")
	caseLimit := fs.Int("limit", 0, "run only the first N cases per tool (0 = all, smoke use)")
	concurrency := fs.Int("concurrency", 3, "parallel cases per tool")
	only := fs.String("only", "", "comma-separated case ids to (re-)run alone — single-case retry mode for cheap flywheel iterations")
	sample := fs.Int("sample", 0, "add a deterministic stride sample of N additional dataset cases as an over-fitting guard alongside --only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outDir == "" || *binDir == "" || *scratch == "" {
		return fmt.Errorf("--out, --bin-dir and --scratch are required")
	}
	datasets, err := LoadDatasets(*datasetDir)
	if err != nil {
		return err
	}
	if v := ValidateDatasets(datasets); !v.OK {
		for _, l := range v.Lines {
			fmt.Println(l)
		}
		return fmt.Errorf("dataset validation failed; refusing to run")
	}
	cfg, err := NewRunConfig(*binDir, *scratch, *outDir, *timeoutSec, *caseLimit, datasets)
	if err != nil {
		return err
	}
	cfg.Concurrency = *concurrency
	if *only != "" {
		if err := cfg.ApplyOnly(splitTools(*only)); err != nil {
			return err
		}
	}
	if *sample > 0 {
		cfg.SampleRegression(*sample)
	}
	if err != nil {
		return err
	}
	for _, name := range splitTools(*tools) {
		cfg.AddTool(name)
	}
	return Run(cfg)
}

func splitTools(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' || r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
