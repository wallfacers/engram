package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory/pipeline"
)

func TestIngestStoresFactsFromStubCaller(t *testing.T) {
	config := Config{DataDir: t.TempDir(), Namespace: defaultNamespace}
	caller := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		return `{"facts":[{"fact":"The user moved to Berlin.","category":"profile","durability":"durable"}]}`, nil
	})
	handle, err := openEngineWith(context.Background(), config, nil, caller)
	if err != nil {
		t.Fatalf("openEngineWith: %v", err)
	}
	defer handle.Close() //nolint:errcheck

	var stdout, stderr bytes.Buffer
	code := runIngest(context.Background(), handle, strings.NewReader("user: I moved to Berlin last month.\nassistant: Noted!\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("ingest exit code = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "# ingested") || !strings.Contains(got, "- extracted: 1") {
		t.Errorf("ingest stdout = %q, want extracted count", got)
	}
	entries, err := handle.entries.List(context.Background())
	if err != nil {
		t.Fatalf("list ingested entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "The user moved to Berlin." {
		t.Errorf("ingested entries = %#v, want one extracted fact", entries)
	}
}

func TestIngestWithoutLLMReportsCapabilityDiagnostic(t *testing.T) {
	setOfflineEnvironment(t)
	code, stdout, stderr := runCommand(t, []string{"--data-dir", t.TempDir(), "ingest"}, "user: hello\n")
	if code != exitCapability {
		t.Fatalf("ingest exit code = %d, want %d; stderr = %q", code, exitCapability, stderr)
	}
	if stdout != "" {
		t.Errorf("ingest stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "ingest requires an LLM") || !strings.Contains(stderr, "ENGRAM_LLM_BASE_URL/MODEL/PROVIDER") {
		t.Errorf("ingest stderr = %q, want LLM configuration diagnostic", stderr)
	}
}
