package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRenderSeedContentEventDateHonesty(t *testing.T) {
	// Non-null event_date renders the deterministic prefix.
	ed := "2026-08-01"
	rendered, rr, err := RenderSeedContent(SeedMemory{Name: "s", Content: "server is Debian", EventDate: &ed})
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "[event_date=2026-08-01] server is Debian" {
		t.Fatalf("rendered content %q lacks the frozen prefix", rendered)
	}
	if rr.RenderedContentDigest == "" || rr.EngineEventDateSupported {
		t.Fatal("receipt must record the digest and honestly deny structured engine EventDate")
	}
	if rr.SourceEventDate == nil || *rr.SourceEventDate != "2026-08-01" {
		t.Fatal("receipt must record the source field")
	}
	// Null event_date passes content through untouched.
	rendered2, rr2, err := RenderSeedContent(SeedMemory{Name: "s2", Content: "plain"})
	if err != nil || rendered2 != "plain" || rr2.SourceEventDate != nil {
		t.Fatalf("null event_date handling: %q %+v %v", rendered2, rr2, err)
	}
	// Invalid format fails closed.
	bad := "08/01/2026"
	if _, _, err := RenderSeedContent(SeedMemory{Name: "s", Content: "x", EventDate: &bad}); err == nil {
		t.Fatal("non-ISO event_date must fail, never render or drop silently")
	}
}

func TestDiagnosticUniqueRootsRequired(t *testing.T) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	base := DiagnosticOptions{Split: SplitDevRegression, Tool: "claude", DatasetDir: dir,
		ManifestPath: dir + "/manifest.json", Concurrency: 2}
	defer func(orig caseChildRunner) { v2ChildRunner = orig }(v2ChildRunner)
	v2ChildRunner = func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) { return []byte{}, nil }

	// A pre-existing out root is refused (unique-root requirement).
	used := t.TempDir()
	o := base
	o.OutRoot = used
	o.ScratchRoot = t.TempDir()
	if _, err := RunDiagnostic(o); err == nil {
		t.Fatal("reused out root must be refused")
	}
	o = base
	o.OutRoot = t.TempDir()
	o.ScratchRoot = used
	if _, err := RunDiagnostic(o); err == nil {
		t.Fatal("reused scratch root must be refused")
	}
	// Holdout split is always rejected for diagnostics.
	o = base
	o.Split = SplitHoldout
	o.OutRoot, o.ScratchRoot = t.TempDir(), t.TempDir()
	if _, err := RunDiagnostic(o); err == nil {
		t.Fatal("diagnostic --split holdout must be invalid")
	}
	// Missing explicit concurrency is rejected.
	o = base
	o.Concurrency = 0
	o.OutRoot, o.ScratchRoot = t.TempDir(), t.TempDir()
	if _, err := RunDiagnostic(o); err == nil {
		t.Fatal("diagnostic mode requires explicit --concurrency")
	}
}

func diagnosticFixture(t *testing.T) (string, string) {
	dir, m := syntheticCore172(t)
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	return dir, dir + "/manifest.json"
}

func TestDiagnosticPoolBoundsOverlapAndEligibility(t *testing.T) {
	datasetDir, manifest := diagnosticFixture(t)
	defer func(orig caseChildRunner) { v2ChildRunner = orig }(v2ChildRunner)

	// Barrier child: block until `want` children are simultaneously active,
	// making concurrency behavior deterministic.
	barrier := func(want int) caseChildRunner {
		var mu sync.Mutex
		active := 0
		release := make(chan struct{})
		closed := false
		return func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
			mu.Lock()
			active++
			if active >= want && !closed {
				close(release)
				closed = true
			}
			mu.Unlock()
			<-release
			mu.Lock()
			active--
			mu.Unlock()
			return []byte(fmt.Sprintf(`{"type":"result","result":"ok %s"}`, caseDir)), nil
		}
	}

	// Concurrency 1: strictly serial — overlap must never be observed.
	v2ChildRunner = barrier(2) // never reached: with one worker, want=2 blocks!
	// Use a non-blocking child for concurrency 1.
	v2ChildRunner = func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		return []byte(`{"type":"result","result":"ok"}`), nil
	}
	opts := DiagnosticOptions{Split: SplitDevRegression, Tool: "claude", DatasetDir: datasetDir,
		ManifestPath: manifest, Concurrency: 1, Limit: 4,
		OutRoot: uniqueRoot(t), ScratchRoot: uniqueRoot(t)}
	rec, err := RunDiagnostic(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ObservedMaxInFlight != 1 || rec.ObservedOverlap {
		t.Fatalf("concurrency 1: max_in_flight=%d overlap=%v", rec.ObservedMaxInFlight, rec.ObservedOverlap)
	}
	if rec.FormalScoreEligible {
		t.Fatal("diagnostic receipts are permanently score-ineligible")
	}
	if rec.CaseCount != 4 {
		t.Fatalf("limit selector: case count %d != 4", rec.CaseCount)
	}

	// Concurrency 4 with the barrier: overlap must be observed and the bound
	// must hold.
	v2ChildRunner = barrier(4)
	opts.Concurrency = 4
	opts.Limit = 0
	opts.Sample = 8
	opts.OutRoot, opts.ScratchRoot = uniqueRoot(t), uniqueRoot(t)
	rec, err = RunDiagnostic(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ObservedMaxInFlight > 4 {
		t.Fatalf("max_in_flight %d exceeds concurrency 4", rec.ObservedMaxInFlight)
	}
	if !rec.ObservedOverlap {
		t.Fatal("concurrency > 1 must exhibit actual overlap")
	}
	if rec.CaseCount != 8 {
		t.Fatalf("sample selector: case count %d != 8", rec.CaseCount)
	}
	// Every case produced exactly one verdict.
	if len(rec.Verdicts) != rec.CaseCount {
		t.Fatalf("verdicts %d != case count %d", len(rec.Verdicts), rec.CaseCount)
	}
}

func TestDiagnosticOnlySelectorAndUnknownIDs(t *testing.T) {
	datasetDir, manifest := diagnosticFixture(t)
	defer func(orig caseChildRunner) { v2ChildRunner = orig }(v2ChildRunner)
	v2ChildRunner = func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		return []byte(`{"type":"result","result":"ok"}`), nil
	}
	opts := DiagnosticOptions{Split: SplitDevRegression, Tool: "claude", DatasetDir: datasetDir,
		ManifestPath: manifest, Concurrency: 2, Only: []string{"iwp-01", "reg-001"},
		OutRoot: uniqueRoot(t), ScratchRoot: uniqueRoot(t)}
	rec, err := RunDiagnostic(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rec.CaseCount != 2 {
		t.Fatalf("only selector: got %d cases", rec.CaseCount)
	}
	opts.Only = []string{"iwp-01", "ghost-999"}
	opts.OutRoot, opts.ScratchRoot = uniqueRoot(t), uniqueRoot(t)
	if _, err := RunDiagnostic(opts); err == nil || !strings.Contains(err.Error(), "ghost-999") {
		t.Fatalf("unknown --only ids must fail: %v", err)
	}
}

func TestDiagnosticChildFailureIsRunnerError(t *testing.T) {
	datasetDir, manifest := diagnosticFixture(t)
	defer func(orig caseChildRunner) { v2ChildRunner = orig }(v2ChildRunner)
	v2ChildRunner = func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		return nil, fmt.Errorf("spawn: fictional cli missing")
	}
	opts := DiagnosticOptions{Split: SplitDevRegression, Tool: "claude", DatasetDir: datasetDir,
		ManifestPath: manifest, Concurrency: 2, Only: []string{"iwp-01"},
		OutRoot: uniqueRoot(t), ScratchRoot: uniqueRoot(t)}
	// A terminal child failure does not abort the diagnostic batch: it is
	// classified as that case's runner-error verdict (diagnostics never stop
	// the batch; formal primary mode will invalidate instead).
	rec, err := RunDiagnostic(opts)
	if err != nil {
		t.Fatalf("child failure must not abort the batch in diagnostic mode: %v", err)
	}
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Failure != "runner-error" {
		t.Fatalf("terminal child failure must classify runner-error: %+v", rec.Verdicts)
	}
}

func TestJudgeV2FailureClasses(t *testing.T) {
	lang := "zh"
	c := &TriggerCaseV2{ID: "fx", Module: "implicit-write-pos", Lang: &lang,
		Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}},
			AnswerInclude: []Alternation{{"已记"}}, Observable: "o"}}
	// False-negative: no write.
	v := JudgeV2(c, []Event{}, "", nil)
	if v.Pass || v.Failure != "false-negative" {
		t.Fatalf("no-write verdict: %+v", v)
	}
	// Wrong-report: write happened, store ok, no acknowledgment.
	writeEvent := []Event{{Kind: EventEngramCall, Op: "write"}}
	v = JudgeV2(c, writeEvent, "pnpm saved", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("no-ack verdict: %+v", v)
	}
	// Pass.
	v = JudgeV2(c, writeEvent, "pnpm saved", nil)
	v = JudgeV2(c, append(writeEvent, Event{Kind: EventText, Text: "已记住 pnpm"}), "pnpm saved", nil)
	if !v.Pass {
		t.Fatalf("clean pass verdict: %+v", v)
	}
	// Wrong-op: store missing content.
	v = JudgeV2(c, append(writeEvent, Event{Kind: EventText, Text: "已记"}), "unrelated", nil)
	if v.Pass || v.Failure != "wrong-op" {
		t.Fatalf("wrong store verdict: %+v", v)
	}
	// runner-error is terminal and conservative.
	v = JudgeV2(c, nil, "", fmt.Errorf("timeout"))
	if v.Pass || v.Failure != "runner-error" {
		t.Fatalf("runner-error verdict: %+v", v)
	}
	// Read-negative: any call is a false-positive.
	neg := &TriggerCaseV2{ID: "fxneg", Module: "implicit-read-neg",
		Expect: ExpectV2{Trigger: false, Observable: "o"}}
	v = JudgeV2(neg, []Event{{Kind: EventEngramCall, Op: "search"}}, "", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("read-neg verdict: %+v", v)
	}
	// Write-negative: a careful search alone is not a misfire; a write is.
	wneg := &TriggerCaseV2{ID: "fxwneg", Module: "implicit-write-neg",
		Expect: ExpectV2{Trigger: false, Observable: "o"}}
	if v := JudgeV2(wneg, []Event{{Kind: EventEngramCall, Op: "search"}}, "", nil); !v.Pass {
		t.Fatalf("write-neg search-only must pass: %+v", v)
	}
	if v := JudgeV2(wneg, []Event{{Kind: EventEngramCall, Op: "write"}}, "", nil); v.Pass || v.Failure != "false-positive" {
		t.Fatalf("write-neg write verdict: %+v", v)
	}
}

func uniqueRoot(t *testing.T) string {
	t.Helper()
	// Always under t.TempDir(): diagnostic runs require non-existent unique
	// roots, and throwaway run artifacts must never land in the source tree.
	return filepath.Join(t.TempDir(), "root-"+fmt.Sprint(nextUnique()))
}

var uniqueCounter int64

func nextUnique() int64 {
	uniqueCounter++
	return uniqueCounter
}
