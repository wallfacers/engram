package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOfflineMemoryRoundTrip(t *testing.T) {
	setOfflineEnvironment(t)
	dataDir := t.TempDir()

	code, stdout, stderr := runCommand(t, []string{"--data-dir", dataDir, "add", "--name", "dark-mode", "--content", "The user prefers dark mode.", "--category", "preference"}, "")
	if code != exitOK {
		t.Fatalf("add exit code = %d, stderr = %q", code, stderr)
	}
	if got, want := stdout, "# added\n\n- name: dark-mode\n"; got != want {
		t.Errorf("add stdout = %q, want %q", got, want)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "search", "dark mode"}, "")
	if code != exitOK {
		t.Fatalf("search exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "## dark-mode") || !strings.Contains(stdout, "The user prefers dark mode.") {
		t.Errorf("search stdout = %q, want stored memory", stdout)
	}
	if !strings.Contains(stdout, "degraded.semantic: unavailable") {
		t.Errorf("search stdout = %q, want honest semantic degradation", stdout)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "get", "dark-mode"}, "")
	if code != exitOK {
		t.Fatalf("get exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "## dark-mode") || !strings.Contains(stdout, "The user prefers dark mode.") {
		t.Errorf("get stdout = %q, want full stored record", stdout)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "list"}, "")
	if code != exitOK {
		t.Fatalf("list exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "## dark-mode") {
		t.Errorf("list stdout = %q, want stored memory", stdout)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "delete", "dark-mode"}, "")
	if code != exitOK {
		t.Fatalf("delete exit code = %d, stderr = %q", code, stderr)
	}
	if got, want := stdout, "# deleted\n\n- name: dark-mode\n"; got != want {
		t.Errorf("delete stdout = %q, want %q", got, want)
	}

	code, stdout, stderr = runCommand(t, []string{"--data-dir", dataDir, "get", "dark-mode"}, "")
	if code != exitNotFound {
		t.Fatalf("get missing exit code = %d, want %d; stderr = %q", code, exitNotFound, stderr)
	}
	if stdout != "" {
		t.Errorf("get missing stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "memory \"dark-mode\" not found") || !strings.Contains(stderr, "run: engram list") {
		t.Errorf("get missing stderr = %q, want actionable not-found diagnostic", stderr)
	}
}

func runCommand(t *testing.T, args []string, input string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func setOfflineEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ENGRAM_DATA_DIR",
		"ENGRAM_NAMESPACE",
		"ENGRAM_EMBED_BASE_URL",
		"ENGRAM_EMBED_MODEL",
		"ENGRAM_EMBED_API_KEY",
		"ENGRAM_LLM_BASE_URL",
		"ENGRAM_LLM_MODEL",
		"ENGRAM_LLM_PROVIDER",
		"ENGRAM_LLM_API_KEY",
	} {
		t.Setenv(key, "")
	}
}
