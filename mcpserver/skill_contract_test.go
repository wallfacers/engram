package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wallfacers/engram/memory/pipeline"
)

type skillMCPContract struct {
	MCP struct {
		Always      []string          `json:"always"`
		Conditional map[string]string `json:"conditional"`
	} `json:"mcp"`
}

func TestSkillContractMCPToolsMatchRuntime(t *testing.T) {
	contract := loadSkillMCPContract(t)
	ctx := context.Background()

	offlineRegistry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = offlineRegistry.Close() })
	offlineClient, _ := connectInMemory(t, ctx, NewServer(offlineRegistry))
	offlineTools, err := offlineClient.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedToolNames(offlineTools.Tools); !reflect.DeepEqual(got, contract.MCP.Always) {
		t.Fatalf("offline tools/list = %v, manifest always tools = %v", got, contract.MCP.Always)
	}
	for _, forbidden := range []string{"memory_curate", "memory_stats", "memory_export", "memory_namespaces", "memory_version"} {
		if contains(sortedToolNames(offlineTools.Tools), forbidden) {
			t.Fatalf("offline tools/list exposes CLI-only fake tool %q", forbidden)
		}
	}

	llmRegistry, err := NewRegistry(ctx, RegistryConfig{
		DataDir: t.TempDir(),
		LLMCaller: pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
			return `{"facts":[]}`, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = llmRegistry.Close() })
	llmClient, _ := connectInMemory(t, ctx, NewServer(llmRegistry))
	llmTools, err := llmClient.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantWithLLM := append(append([]string(nil), contract.MCP.Always...), "memory_ingest")
	sort.Strings(wantWithLLM)
	if got := sortedToolNames(llmTools.Tools); !reflect.DeepEqual(got, wantWithLLM) {
		t.Fatalf("LLM tools/list = %v, manifest contract = %v", got, wantWithLLM)
	}
	if contract.MCP.Conditional["memory_ingest"] != "llm" || len(contract.MCP.Conditional) != 1 {
		t.Fatalf("manifest conditional tools = %#v, want only memory_ingest: llm", contract.MCP.Conditional)
	}
}

func sortedToolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func contains(names []string, wanted string) bool {
	return sort.SearchStrings(names, wanted) < len(names) && names[sort.SearchStrings(names, wanted)] == wanted
}

func loadSkillMCPContract(t *testing.T) skillMCPContract {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	contractPath := filepath.Join(filepath.Dir(thisFile), "..", "skills", "engram", "references", "contract.json")
	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read %s: %v", contractPath, err)
	}
	var contract skillMCPContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("decode %s: %v", contractPath, err)
	}
	sort.Strings(contract.MCP.Always)
	return contract
}
