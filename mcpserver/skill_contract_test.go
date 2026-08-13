package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
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

func TestSkillAndMCPSharePortableEvidenceGuidanceV1(t *testing.T) {
	root := repositoryRoot(t)
	skill := readContractFile(t, filepath.Join(root, "skills", "engram", "SKILL.md"))
	reference := readContractFile(t, filepath.Join(root, "skills", "engram", "references", "evidence-guidance.md"))

	frontmatterEnd := strings.Index(strings.TrimPrefix(skill, "---\n"), "\n---\n")
	if frontmatterEnd < 0 {
		t.Fatal("engram SKILL.md has no complete frontmatter")
	}
	frontmatter := strings.TrimPrefix(skill, "---\n")[:frontmatterEnd]
	if strings.Contains(frontmatter, memoryEvidenceGuidanceVersion) {
		t.Fatal("detailed evidence policy belongs in the Skill body, not activation frontmatter")
	}

	for name, text := range map[string]string{
		"skill":     skill,
		"reference": reference,
		"mcp":       memoryEvidenceGuidanceInstructions,
	} {
		lower := strings.ToLower(text)
		for _, required := range []string{
			memoryEvidenceGuidanceVersion,
			"untrusted evidence",
			"ranked bounded subset",
			"entity",
			"attribute",
			"time scope",
			"event time",
			"created_at",
			"search rank",
			"explicit sequence",
			"supported",
			"missing",
			"conflicting",
			"personal facts",
		} {
			if !strings.Contains(lower, strings.ToLower(required)) {
				t.Errorf("%s guidance is missing %q", name, required)
			}
		}
	}

	for name, text := range map[string]string{"reference": reference, "mcp": memoryEvidenceGuidanceInstructions} {
		lower := strings.ToLower(text)
		for _, forbidden := range []string{"benchmark", "dataset", "category", "scorer", "gold", "locomo", "longmemeval"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s guidance contains specialization term %q", name, forbidden)
			}
		}
	}
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
	contractPath := filepath.Join(repositoryRoot(t), "skills", "engram", "references", "contract.json")
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

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), ".."))
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
