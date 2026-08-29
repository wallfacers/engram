package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseToolSpec(t *testing.T) {
	for _, tc := range []struct {
		spec          string
		name, variant string
		label         string
	}{{"claude", "claude", "", "claude"},
		{"codex@ds", "codex", "ds", "codex@ds"},
		{"opencode@qwen", "opencode", "qwen", "opencode@qwen"},
		{"claude@opus", "claude", "opus", "claude@opus"}} {
		name, variant, label := parseToolSpec(tc.spec)
		if name != tc.name || variant != tc.variant || label != tc.label {
			t.Errorf("parseToolSpec(%q) = %q,%q,%q; want %q,%q,%q",
				tc.spec, name, variant, label, tc.name, tc.variant, tc.label)
		}
	}
}

func TestAddToolVariantIsolation(t *testing.T) {
	cfg := &RunConfig{Scratch: t.TempDir()}
	cfg.AddTool("codex@ds")
	cfg.AddTool("codex")
	if len(cfg.Tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(cfg.Tools))
	}
	a, b := cfg.Tools[0], cfg.Tools[1]
	if a.Label != "codex@ds" || b.Label != "codex" {
		t.Fatalf("labels = %q, %q", a.Label, b.Label)
	}
	if a.DataDir == b.DataDir {
		t.Errorf("variants must not share a store container: both %q", a.DataDir)
	}
	if a.Base != "codex" || b.Base != "codex" {
		t.Errorf("base parser key must be the tool name, got %q/%q", a.Base, b.Base)
	}
}

func TestWriteMCPConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPConfig(p, "/bin/engram-mcp", "/store/case")
	var cfg map[string]any
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	srv := cfg["mcpServers"].(map[string]any)["engram"].(map[string]any)
	if srv["command"] != "/bin/engram-mcp" {
		t.Errorf("command = %v", srv["command"])
	}
	args := srv["args"].([]any)
	if args[1] != "/store/case" {
		t.Errorf("args = %v; want case store dir as --data-dir value", args)
	}
}

func TestClaudeMCPConfigPathPerCase(t *testing.T) {
	// The claude --mcp-config file must be per-case: one shared path per label
	// is a write race under concurrency — parallel claude processes overwrite
	// the same file and every write funnels into whichever caseDir the last
	// writer encoded (f1: six cases' entries landed in iw-pos-004's db).
	cfg := &RunConfig{Scratch: t.TempDir()}
	cfg.AddTool("claude")
	build := cfg.Tools[0].build
	cmds := []*exec.Cmd{
		build(cfg.Tools[0], "/bin", "settings.json", filepath.Join(cfg.Scratch, "data", "claude", "iw-pos-001"), "p1"),
		build(cfg.Tools[0], "/bin", "settings.json", filepath.Join(cfg.Scratch, "data", "claude", "iw-pos-002"), "p2"),
	}
	var paths []string
	for _, cmd := range cmds {
		args := cmd.Args
		i := -1
		for j, a := range args {
			if a == "--mcp-config" {
				i = j + 1
				break
			}
		}
		if i < 0 {
			t.Fatalf("--mcp-config missing in %v", args)
		}
		paths = append(paths, args[i])
	}
	if paths[0] == paths[1] {
		t.Fatalf("mcp-config path must differ per case, both %q", paths[0])
	}
	// Each written config must encode its own caseDir.
	for i, p := range paths {
		var mc map[string]any
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("config %q not written: %v", p, err)
		}
		if err := json.Unmarshal(b, &mc); err != nil {
			t.Fatal(err)
		}
		args := mc["mcpServers"].(map[string]any)["engram"].(map[string]any)["args"].([]any)
		want := []string{"iw-pos-001", "iw-pos-002"}[i]
		if !strings.HasSuffix(fmt.Sprint(args[1]), want) {
			t.Errorf("config %d data-dir = %v, want suffix %q", i, args[1], want)
		}
	}
}

func TestWriteOpenCodeConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL", "maas/deepseek-v4-flash-0731")
	t.Setenv("ENGRAM_SKILL_EVAL_OPENCODE_BASE", "https://example.test/v1")
	writeOpenCodeConfig(dir, "/bin")
	b, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	srv := cfg["mcp"].(map[string]any)["servers"].(map[string]any)["engram"].(map[string]any)
	if srv["args"].([]any)[1] != dir {
		t.Errorf("mcp --data-dir must point at this case dir, got %v", srv["args"])
	}
	prov := cfg["provider"].(map[string]any)["maas"].(map[string]any)
	opts := prov["options"].(map[string]any)
	if opts["apiKey"] != "{env:MAAS_API_KEY}" {
		t.Errorf("apiKey must template from env, got %v", opts["apiKey"])
	}
	if opts["baseURL"] != "https://example.test/v1" {
		t.Errorf("baseURL = %v", opts["baseURL"])
	}
	if cfg["model"] != "maas/deepseek-v4-flash-0731" {
		t.Errorf("model = %v", cfg["model"])
	}
}

func TestWriteOpenCodeConfigNoProviderWithoutEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL", "")
	writeOpenCodeConfig(dir, "/bin")
	var cfg map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["provider"]; ok {
		t.Error("provider block must be omitted when no model env is set")
	}
	if _, ok := cfg["model"]; ok {
		t.Error("model must be omitted when no model env is set")
	}
	if _, ok := cfg["mcp"]; !ok {
		t.Error("mcp server block must always be present")
	}
}

func TestSweepHostArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	scratch := filepath.Join(home, "scratch-run")
	projDir := filepath.Join(home, ".claude", "projects")
	// Eval claude instances key auto-memory to their case cwd:
	// <home>/scratch-run/data/claude/iw-pos-001 → "-home-...-data-claude-iw-pos-001".
	encoded := "-" + strings.ReplaceAll(scratch, string(filepath.Separator), "-")
	junk := filepath.Join(projDir, encoded+"-data-claude-iw-pos-001", "memory")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatal(err)
	}
	junkOld := filepath.Join(projDir, encoded+"-cwd-claude-run-123", "memory")
	if err := os.MkdirAll(junkOld, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(projDir, "-home-real-project", "memory")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	// A user store with one leaked seed entry (marker file stands in for the db).
	engramHome := filepath.Join(home, ".engram")
	if err := os.MkdirAll(engramHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engramHome, "default.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	calls := filepath.Join(binDir, "calls")
	bin := filepath.Join(binDir, "engram")
	script := "#!/bin/sh\necho \"$@\" >> " + calls + "\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs, seeds, err := sweepHostArtifacts(scratch, []string{"pkg-manager", "diet-peanut"}, bin)
	if err != nil {
		t.Fatal(err)
	}
	if dirs != 2 {
		t.Errorf("removed dirs = %d, want 2 (per-case + cwd-run pattern)", dirs)
	}
	if _, err := os.Stat(junk); !os.IsNotExist(err) {
		t.Errorf("eval project dir not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated project dir must survive: %v", err)
	}
	if seeds != 2 {
		t.Errorf("removed seeds = %d, want 2", seeds)
	}
	got, _ := os.ReadFile(calls)
	for _, want := range []string{"pkg-manager", "diet-peanut"} {
		if !strings.Contains(string(got), "delete "+want) {
			t.Errorf("delete for seed %q not issued; calls=%q", want, got)
		}
	}
}

func TestSweepHostArtifactsNoUserStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	scratch := filepath.Join(home, "scratch-run")
	bin := filepath.Join(t.TempDir(), "engram")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs, seeds, err := sweepHostArtifacts(scratch, []string{"pkg-manager"}, bin)
	if err != nil {
		t.Fatal(err)
	}
	if dirs != 0 || seeds != 0 {
		t.Errorf("empty home: dirs=%d seeds=%d, want 0/0", dirs, seeds)
	}
}
