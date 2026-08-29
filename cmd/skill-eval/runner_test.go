package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
