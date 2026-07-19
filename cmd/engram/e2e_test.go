package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryOfflineAddAndSearch(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	binary := filepath.Join(t.TempDir(), "engram")
	build := exec.Command("go", "build", "-o", binary, "./cmd/engram")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI binary: %v\n%s", err, output)
	}

	dataDir := t.TempDir()
	add := exec.Command(binary, "--data-dir", dataDir, "add", "--name", "binary-memory", "--content", "the binary runs offline")
	add.Env = offlineCommandEnv()
	addOutput, addStderr, err := runBinary(add)
	if err != nil {
		t.Fatalf("binary add: %v\nstdout: %s\nstderr: %s", err, addOutput, addStderr)
	}
	if got := string(addOutput); got != "# added\n\n- name: binary-memory\n" {
		t.Errorf("binary add output = %q", got)
	}

	search := exec.Command(binary, "--data-dir", dataDir, "search", "binary")
	search.Env = offlineCommandEnv()
	searchOutput, searchStderr, err := runBinary(search)
	if err != nil {
		t.Fatalf("binary search: %v\nstdout: %s\nstderr: %s", err, searchOutput, searchStderr)
	}
	if got := string(searchOutput); !strings.Contains(got, "# search") || !strings.Contains(got, "## binary-memory") {
		t.Errorf("binary search output = %q, want markdown hit", got)
	}
}

func runBinary(command *exec.Cmd) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func offlineCommandEnv() []string {
	return append(os.Environ(),
		"ENGRAM_EMBED_BASE_URL=",
		"ENGRAM_EMBED_MODEL=",
		"ENGRAM_EMBED_API_KEY=",
		"ENGRAM_LLM_BASE_URL=",
		"ENGRAM_LLM_MODEL=",
		"ENGRAM_LLM_PROVIDER=",
		"ENGRAM_LLM_API_KEY=",
	)
}
