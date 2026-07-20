package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func TestSearchMatchesDirectRetriever(t *testing.T) {
	setOfflineEnvironment(t)
	config := Config{DataDir: t.TempDir(), Namespace: defaultNamespace}
	handle, err := openEngine(context.Background(), config)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	for _, entry := range []*memory.Entry{
		{Name: "alpha", Content: "shared query terms appear in alpha", CharCount: 34},
		{Name: "bravo", Content: "shared query terms appear in bravo", CharCount: 34},
		{Name: "charlie", Content: "unrelated memory", CharCount: 16},
	} {
		if err := handle.entries.Upsert(context.Background(), entry); err != nil {
			handle.Close() //nolint:errcheck
			t.Fatalf("seed %q: %v", entry.Name, err)
		}
	}
	direct, err := handle.retriever.Search(context.Background(), "shared query", 8)
	if err != nil {
		handle.Close() //nolint:errcheck
		t.Fatalf("direct Search: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close seeded handle: %v", err)
	}

	code, stdout, stderr := runCommand(t, []string{"--data-dir", config.DataDir, "search", "shared query"}, "")
	if code != exitOK {
		t.Fatalf("CLI search exit code = %d, stderr = %q", code, stderr)
	}
	got := markdownSearchNames(stdout)
	if len(got) != len(direct) {
		t.Fatalf("CLI result names = %v, direct results = %#v", got, direct)
	}
	for i, result := range direct {
		if got[i] != result.Name {
			t.Errorf("result %d = %q, want direct Retriever.Search name %q", i, got[i], result.Name)
		}
	}
}

func markdownSearchNames(document string) []string {
	var names []string
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "## ") {
			names = append(names, strings.TrimPrefix(line, "## "))
		}
	}
	return names
}
