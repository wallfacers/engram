package main

import (
	"strings"
	"testing"

	"github.com/wallfacers/engram/internal/version"
)

func TestStoreOperations(t *testing.T) {
	setOfflineEnvironment(t)
	dataDir := t.TempDir()

	for _, args := range [][]string{
		{"--data-dir", dataDir, "add", "--name", "default-memory", "--content", "default namespace fact", "--pinned"},
		{"--data-dir", dataDir, "--namespace", "work", "add", "--name", "work-memory", "--content", "work namespace fact"},
	} {
		code, _, stderr := runCommand(t, args, "")
		if code != exitOK {
			t.Fatalf("seed add %v exit code = %d, stderr = %q", args, code, stderr)
		}
	}

	code, stdout, stderr := runCommand(t, []string{"--data-dir", dataDir, "stats"}, "")
	if code != exitOK {
		t.Fatalf("stats exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "# stats") || !strings.Contains(stdout, "- entries: 1") || !strings.Contains(stdout, "- non-pinned: 0") {
		t.Errorf("stats stdout = %q, want default namespace counts", stdout)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "namespaces"}, "")
	if code != exitOK {
		t.Fatalf("namespaces exit code = %d, stderr = %q", code, stderr)
	}
	if got := markdownListItems(stdout); strings.Join(got, ",") != "default,work" {
		t.Errorf("namespaces = %v, want [default work]", got)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "export"}, "")
	if code != exitOK {
		t.Fatalf("export exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "# workhorse-agent memory export") || !strings.Contains(stdout, "default-memory") || !strings.Contains(stdout, "default namespace fact") {
		t.Errorf("export stdout = %q, want complete default export", stdout)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "version"}, "")
	if code != exitOK {
		t.Fatalf("version exit code = %d, stderr = %q", code, stderr)
	}
	if got, want := stdout, version.Version+"\n"; got != want {
		t.Errorf("version stdout = %q, want %q", got, want)
	}
}

// TestVersionDoesNotRequireDataDir locks the frozen contract: `version` is a
// build-probe that prints and exits 0 with no data directory configured. A prior
// implementation required --data-dir for every command, which broke this probe.
func TestVersionDoesNotRequireDataDir(t *testing.T) {
	setOfflineEnvironment(t) // clears ENGRAM_DATA_DIR and all other ENGRAM_* vars
	code, stdout, stderr := runCommand(t, []string{"version"}, "")
	if code != exitOK {
		t.Fatalf("version exit code = %d, stderr = %q", code, stderr)
	}
	if got, want := stdout, version.Version+"\n"; got != want {
		t.Errorf("version stdout = %q, want %q", got, want)
	}
	if stderr != "" {
		t.Errorf("version stderr = %q, want empty", stderr)
	}
}

func markdownListItems(document string) []string {
	var items []string
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "- ") {
			items = append(items, strings.TrimPrefix(line, "- "))
		}
	}
	return items
}
