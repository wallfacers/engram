package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Implement-identity digests: stable, source-derived identities that receipts
// bind so any drift after attestation is detectable offline.

type capturedOutput struct {
	exitCode int
	stdout   []byte
	stderr   []byte
}

// runAndCapture runs argv in the repository root and captures exit/stdout/stderr.
func runAndCapture(args []string) capturedOutput {
	return runAndCaptureIn(args, "")
}

// runAndCaptureIn runs argv with an explicit working directory ("", meaning
// inherit) and captures exit/stdout/stderr.
func runAndCaptureIn(args []string, cwd string) capturedOutput {
	cmd := exec.Command(args[0], args[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdoutWriter{b: &stdout}
	cmd.Stderr = &stderrWriter{b: &stderr}
	err := cmd.Run()
	out := capturedOutput{stdout: []byte(stdout.String()), stderr: []byte(stderr.String())}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			out.exitCode = ee.ExitCode()
		} else {
			out.exitCode = -1
		}
	}
	return out
}

type stdoutWriter struct{ b *strings.Builder }
type stderrWriter struct{ b *strings.Builder }

func (w *stdoutWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
func (w *stderrWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// repoRootFromCwd walks up to the directory containing go.mod.
func repoRootFromCwd() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// CurrentRunnerDigest is the LF-normalized source digest of the
// cmd/skill-eval production subtree (non-test .go files) — the
// ToolProvenance.source_revision scope: runner-only, never skill/dataset/docs.
func CurrentRunnerDigest() (string, error) {
	root, err := repoRootFromCwd()
	if err != nil {
		return "", err
	}
	return dirSourceDigest(filepath.Join(root, "cmd", "skill-eval"), true)
}

// CurrentJudgeRuleDigest binds the deterministic judge implementation
// (judge + events normalization). Any judge-behavior change drifts every
// receipt that references it.
func CurrentJudgeRuleDigest() (string, error) {
	root, err := repoRootFromCwd()
	if err != nil {
		return "", err
	}
	h := ""
	for _, name := range []string{"judge.go", "events.go"} {
		b, err := os.ReadFile(filepath.Join(root, "cmd", "skill-eval", name))
		if err != nil {
			return "", err
		}
		d, err := LFNormalizedSHA256(b)
		if err != nil {
			return "", err
		}
		h += d
	}
	return sha256Hex([]byte(h)), nil
}

// dirSourceDigest hashes sorted non-test (.go) files under dir with the
// dataset-payload framing (path\0len\0normalized-content\0).
func dirSourceDigest(dir string, excludeTests bool) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if excludeTests && strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", os.ErrNotExist
	}
	sort.Strings(files)
	var buf []byte
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(dir, f)
		buf = append(buf, []byte(rel)...)
		buf = append(buf, 0)
		buf = append(buf, []byte(itoa(len(b)))...)
		buf = append(buf, 0)
		buf = append(buf, normalizeLF(b)...)
		buf = append(buf, 0)
	}
	return sha256Hex(buf), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
