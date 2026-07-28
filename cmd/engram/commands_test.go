package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
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

func TestCurateCommandRoutesAndReportsMissingCapability(t *testing.T) {
	setOfflineEnvironment(t)
	code, stdout, stderr := runCommand(t, []string{"--data-dir", t.TempDir(), "curate"}, "")
	if code != exitCapability {
		t.Fatalf("curate exit code = %d, want %d; stderr = %q", code, exitCapability, stderr)
	}
	if stdout != "" {
		t.Fatalf("curate stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "curate requires an LLM") {
		t.Fatalf("curate stderr = %q, want capability diagnostic", stderr)
	}
}

func TestCurateReportsIncompleteLLMConfigurationAsCapabilityError(t *testing.T) {
	setOfflineEnvironment(t)
	t.Setenv("ENGRAM_LLM_MODEL", "configured-without-provider")
	code, stdout, stderr := runCommand(t, []string{"--data-dir", t.TempDir(), "curate"}, "")
	if code != exitCapability {
		t.Fatalf("curate exit code = %d, want %d; stderr = %q", code, exitCapability, stderr)
	}
	if stdout != "" {
		t.Fatalf("curate stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "curate requires an LLM") || !strings.Contains(stderr, "ENGRAM_LLM_BASE_URL/MODEL/PROVIDER") {
		t.Fatalf("curate stderr = %q, want actionable capability diagnostic", stderr)
	}
}

func TestRunCurateRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCurate(context.Background(), &engineHandle{}, []string{"unexpected"}, &stdout, &stderr)
	if code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "curate takes no arguments") {
		t.Fatalf("runCurate = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestRunCurateIsSynchronousAndPrintsDeterministicStatus(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	releaseJudge := make(chan struct{})
	caller := pipeline.ModelCaller(func(callCtx context.Context, _, _ string) (string, error) {
		close(started)
		select {
		case <-releaseJudge:
			return `{"evict":[],"merge":[]}`, nil
		case <-callCtx.Done():
			return "", callCtx.Err()
		}
	})
	config := Config{DataDir: t.TempDir(), Namespace: "curate-ns"}
	handle, err := openEngineWith(ctx, config, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := handle.entries.Upsert(ctx, &memory.Entry{Name: "candidate", Content: "candidate content"}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runCurate(ctx, handle, nil, &stdout, &stderr)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("curation judge did not start")
	}
	select {
	case code := <-done:
		t.Fatalf("curate returned before judge completed with code %d", code)
	default:
	}
	close(releaseJudge)
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("curate exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("curate did not return after judge completed")
	}
	if got, want := stdout.String(), "# curated\n\n- namespace: curate-ns\n- status: completed\n"; got != want {
		t.Fatalf("curate stdout = %q, want %q", got, want)
	}
}

func TestRunCurateOnlyTouchesCurrentNamespace(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	var calls int
	caller := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		calls++
		return `{"evict":["candidate"],"merge":[]}`, nil
	})
	first, err := openEngineWith(ctx, Config{DataDir: dataDir, Namespace: "first"}, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := openEngineWith(ctx, Config{DataDir: dataDir, Namespace: "second"}, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	for _, handle := range []*engineHandle{first, second} {
		if err := handle.entries.Upsert(ctx, &memory.Entry{Name: "candidate", Content: "candidate content"}); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := runCurate(ctx, first, nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("curate code = %d, stderr = %q", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("judge calls = %d, want 1 for current namespace", calls)
	}
	if _, err := second.entries.GetByName(ctx, "candidate"); err != nil {
		t.Fatalf("other namespace candidate was changed: %v", err)
	}
}
