package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

type skillCLIContract struct {
	CLI struct {
		Commands []string `json:"commands"`
	} `json:"cli"`
	Intents []struct {
		Name string  `json:"name"`
		MCP  *string `json:"mcp"`
		CLI  *string `json:"cli"`
	} `json:"intents"`
}

func TestSkillContractCLICommandsMatchRuntime(t *testing.T) {
	contract := loadSkillCLIContract(t)
	got := make([]string, 0, len(knownCommands))
	for command := range knownCommands {
		got = append(got, command)
	}
	sort.Strings(got)
	want := append([]string(nil), contract.CLI.Commands...)
	if !sort.StringsAreSorted(want) {
		t.Fatalf("skill manifest CLI commands must be lexical: %v", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skill manifest CLI commands = %v, runtime knownCommands = %v", want, got)
	}

	if _, exists := knownCommands["memory_curate"]; exists {
		t.Fatal("CLI-only MCP-shaped fake command memory_curate must not be in knownCommands")
	}

	wantIntentCommands := map[string]string{
		"write":               "add",
		"search":              "search",
		"get":                 "get",
		"list":                "list",
		"delete":              "delete",
		"ingest":              "ingest",
		"curate":              "curate",
		"stats":               "stats",
		"export":              "export",
		"namespace-discovery": "namespaces",
		"version":             "version",
	}
	if len(contract.Intents) != len(wantIntentCommands) {
		t.Fatalf("skill manifest has %d intents, want %d", len(contract.Intents), len(wantIntentCommands))
	}
	seen := make(map[string]bool, len(contract.Intents))
	for _, intent := range contract.Intents {
		if seen[intent.Name] {
			t.Fatalf("duplicate skill intent %q", intent.Name)
		}
		seen[intent.Name] = true
		wantCommand, ok := wantIntentCommands[intent.Name]
		if !ok {
			t.Fatalf("unknown skill intent %q", intent.Name)
		}
		if intent.CLI == nil || *intent.CLI != wantCommand {
			t.Fatalf("intent %q CLI = %v, want %q", intent.Name, intent.CLI, wantCommand)
		}
		if _, exists := knownCommands[*intent.CLI]; !exists {
			t.Fatalf("intent %q references stale CLI command %q", intent.Name, *intent.CLI)
		}
	}
	for intent := range wantIntentCommands {
		if !seen[intent] {
			t.Fatalf("manifest is missing required intent %q", intent)
		}
	}
}

func loadSkillCLIContract(t *testing.T) skillCLIContract {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	contractPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "skills", "engram", "references", "contract.json")
	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read %s: %v", contractPath, err)
	}
	var contract skillCLIContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("decode %s: %v", contractPath, err)
	}
	return contract
}
