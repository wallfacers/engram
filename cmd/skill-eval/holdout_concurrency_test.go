package main

// T027 [US3] — author/review stage concurrency and isolation-capacity tests:
// bounded max_in_flight ≤ N, observed overlap when N > 1, per-attempt
// stage-isolation workspaces, own-input readability, controller
// target-existence proofs, and private/audit/receipt/prior-review/
// active-sibling denial (contracts/dataset-protocol.md §4-§5).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStageManagerBoundsInFlight(t *testing.T) {
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, 2)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("att-%d", i)
			ws, err := m.Acquire(StageAuthor, id)
			if err != nil {
				t.Errorf("acquire %s: %v", id, err)
				return
			}
			if ws == "" {
				t.Errorf("acquire %s returned empty workspace", id)
			}
			time.Sleep(5 * time.Millisecond)
			if err := m.Release(id); err != nil {
				t.Errorf("release %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	if got := m.MaxObservedInFlight(); got > 2 {
		t.Errorf("max in-flight %d exceeds configured bound 2", got)
	}
}

func TestStageManagerObservesOverlap(t *testing.T) {
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("att-%d", i)
			_, err := m.Acquire(StageReview, id)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			time.Sleep(40 * time.Millisecond)
			if err := m.Release(id); err != nil {
				t.Errorf("release: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := m.MaxObservedInFlight(); got < 2 {
		t.Errorf("no overlap observed (max=%d) with concurrency 2 — the frozen-concurrency receipt requires actual overlap", got)
	}
}

func TestStageManagerPerAttemptIsolation(t *testing.T) {
	root := t.TempDir()
	m := NewStageWorkspaceManager(root, 3)
	wsA, err := m.Acquire(StageAuthor, "att-a")
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	wsB, err := m.Acquire(StageAuthor, "att-b")
	if err != nil {
		t.Fatalf("acquire b: %v", err)
	}
	if wsA == wsB {
		t.Fatalf("two attempts share one workspace %s", wsA)
	}
	for _, ws := range []string{wsA, wsB} {
		if !isSubdir(root, ws) {
			t.Errorf("workspace %s escapes manager root %s", ws, root)
		}
		for _, sub := range []string{"input", "state", "output"} {
			if fi, err := os.Stat(filepath.Join(ws, sub)); err != nil || !fi.IsDir() {
				t.Errorf("workspace %s missing %s/ layout", ws, sub)
			}
		}
	}
	// The own-input file the exact child must be able to read.
	ownInput := filepath.Join(wsA, "input", "envelope.json")
	if err := os.WriteFile(ownInput, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownInput); err != nil {
		t.Errorf("own input not readable: %v", err)
	}
	if err := m.Release("att-a"); err != nil {
		t.Fatalf("release a: %v", err)
	}
	// The released slot is reusable by a new attempt but under a NEW root.
	wsC, err := m.Acquire(StageReview, "att-c")
	if err != nil {
		t.Fatalf("acquire c: %v", err)
	}
	if wsC == wsA {
		t.Errorf("state root reused for a new attempt (ephemeral roots must never repeat)")
	}
	if err := m.Release("att-b"); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("att-c"); err != nil {
		t.Fatal(err)
	}
}

func TestStageManagerRejectsUnknownOrDuplicate(t *testing.T) {
	m := NewStageWorkspaceManager(t.TempDir(), 1)
	if err := m.Release("ghost"); err == nil {
		t.Error("release of never-acquired attempt accepted")
	}
	if _, err := m.Acquire(StageAuthor, "dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(StageAuthor, "dup"); err == nil {
		t.Error("duplicate acquire for one attempt accepted")
	}
}

func t027ProbeFixture(t *testing.T) (root string, ownInput string, deniedTargets map[ProbeKind]string) {
	t.Helper()
	root = t.TempDir()
	// The controller-side private tree the exact child must never reach. The
	// forbidden roots carry mode 000 so even the owning test user is denied
	// (only root bypasses permission bits); restore them so t.TempDir can
	// clean up afterwards.
	var locked []string
	for _, d := range []string{"private-holdout", "generation-audit", "author-receipts", "prior-reviews"} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		secret := filepath.Join(dir, "record.json")
		if err := os.WriteFile(secret, []byte(`{"x":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(secret, 0o000); err != nil {
			t.Fatal(err)
		}
		locked = append(locked, secret)
	}
	if err := os.MkdirAll(filepath.Join(root, "private-holdout"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "private-holdout"), 0o000); err != nil {
		t.Fatal(err)
	}
	locked = append(locked, filepath.Join(root, "private-holdout"))
	t.Cleanup(func() {
		for _, p := range locked {
			_ = os.Chmod(p, 0o700)
		}
	})
	ownInput = filepath.Join(root, "attempt-input", "envelope.json")
	if err := os.MkdirAll(filepath.Dir(ownInput), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownInput, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	deniedTargets = map[ProbeKind]string{
		ProbePrivateRootTraverse: filepath.Join(root, "private-holdout"),
		ProbePrivateRootList:     filepath.Join(root, "private-holdout"),
		ProbePrivateRootRead:     filepath.Join(root, "private-holdout", "record.json"),
		ProbeGenerationAuditRead: filepath.Join(root, "generation-audit", "record.json"),
		ProbeAuthorReceiptRead:   filepath.Join(root, "author-receipts", "record.json"),
		ProbePriorReviewRead:     filepath.Join(root, "prior-reviews", "record.json"),
		ProbeActiveSiblingRead:   filepath.Join(root, "sibling-attempt", "input", "envelope.json"),
	}
	return root, ownInput, deniedTargets
}

func t027GoodProbes(t *testing.T) []AccessProbe {
	t.Helper()
	_, ownInput, denied := t027ProbeFixture(t)
	probes := []AccessProbe{}
	for kind, target := range denied {
		probes = append(probes, AccessProbe{
			Kind:                        kind,
			TargetPath:                  target,
			ControllerTargetProofDigest: "controller-proof-" + string(kind),
			TargetAccessPolicyDigest:    "parent-policy-digest",
			Expected:                    ProbeDenied,
			Observed:                    ProbeDenied,
		})
	}
	probes = append(probes, AccessProbe{
		Kind:                     ProbeOwnInputRead,
		TargetPath:               ownInput,
		TargetAccessPolicyDigest: "parent-policy-digest",
		Expected:                 ProbeReadable,
		Observed:                 ProbeReadable,
	})
	return probes
}

func TestValidateIsolationProbesGoodPath(t *testing.T) {
	if err := ValidateIsolationProbes(StageAuthor, t027GoodProbes(t)); err != nil {
		t.Fatalf("complete honest probe set rejected: %v", err)
	}
}

func TestValidateIsolationProbesFailClosed(t *testing.T) {
	drop := func(name string, kinds ...ProbeKind) {
		t.Run(name, func(t *testing.T) {
			probes := t027GoodProbes(t)
			kill := map[ProbeKind]bool{}
			for _, k := range kinds {
				kill[k] = true
			}
			kept := probes[:0]
			for _, p := range probes {
				if !kill[p.Kind] {
					kept = append(kept, p)
				}
			}
			if err := ValidateIsolationProbes(StageAuthor, kept); err == nil {
				t.Errorf("probe set missing %v accepted", kinds)
			}
		})
	}
	drop("missing-private-root-read", ProbePrivateRootRead)
	drop("missing-audit", ProbeGenerationAuditRead)
	drop("missing-author-receipt", ProbeAuthorReceiptRead)
	drop("missing-prior-review", ProbePriorReviewRead)
	drop("missing-sibling", ProbeActiveSiblingRead)
	drop("missing-own-input", ProbeOwnInputRead)

	t.Run("forbidden-target-readable", func(t *testing.T) {
		probes := t027GoodProbes(t)
		for i := range probes {
			if probes[i].Kind == ProbeAuthorReceiptRead {
				probes[i].Observed = ProbeReadable
			}
		}
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("readable forbidden target accepted")
		}
	})
	t.Run("own-input-denied", func(t *testing.T) {
		probes := t027GoodProbes(t)
		for i := range probes {
			if probes[i].Kind == ProbeOwnInputRead {
				probes[i].Observed = ProbeDenied
			}
		}
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("unreadable own input accepted")
		}
	})
	t.Run("not-found-without-controller-proof", func(t *testing.T) {
		probes := t027GoodProbes(t)
		for i := range probes {
			if probes[i].Kind == ProbeGenerationAuditRead {
				probes[i].Observed = ProbeNotFound
				probes[i].ControllerTargetProofDigest = ""
			}
		}
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("not-found without controller target-existence proof accepted")
		}
	})
	t.Run("empty-policy-digest", func(t *testing.T) {
		probes := t027GoodProbes(t)
		for i := range probes {
			if probes[i].Kind == ProbePriorReviewRead {
				probes[i].TargetAccessPolicyDigest = ""
			}
		}
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("probe without parent access-policy digest accepted")
		}
	})
	t.Run("unknown-kind", func(t *testing.T) {
		probes := t027GoodProbes(t)
		probes = append(probes, AccessProbe{Kind: ProbeKind("made-up"), Expected: ProbeDenied, Observed: ProbeDenied, TargetAccessPolicyDigest: "x"})
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("unknown probe kind accepted")
		}
	})
	t.Run("expected-mismatch-denied-own-input", func(t *testing.T) {
		probes := t027GoodProbes(t)
		for i := range probes {
			if probes[i].Kind == ProbeOwnInputRead {
				probes[i].Expected = ProbeDenied
			}
		}
		if err := ValidateIsolationProbes(StageAuthor, probes); err == nil {
			t.Error("own-input probe expected=denied accepted")
		}
	})
}

// TestProbeDeniedOnRealTree runs the actual filesystem probe against the
// fixture tree: every forbidden target reports denied, the own input reads.
func TestProbeDeniedOnRealTree(t *testing.T) {
	root, ownInput, denied := t027ProbeFixture(t)
	// A sibling attempt workspace that is "active" during the probe; its
	// input is mode 000 so the exact child is denied.
	siblingFile := denied[ProbeActiveSiblingRead]
	if err := os.MkdirAll(filepath.Dir(siblingFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(siblingFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(siblingFile, 0o600) })
	for kind, target := range denied {
		if got := ProbeFilesystem(target); got != ProbeDenied {
			t.Errorf("probe %s on %s observed %s, want denied", kind, target, got)
		}
	}
	if got := ProbeFilesystem(ownInput); got != ProbeReadable {
		t.Errorf("own input observed %s, want readable", got)
	}
	if got := ProbeFilesystem(filepath.Join(root, "never-existed")); got != ProbeNotFound {
		t.Errorf("missing target observed %s, want not-found", got)
	}
}

// TestControllerTargetProofBindsPreexistingTarget pins the pre-launch
// existence/content/policy capture: a proof taken now must fail to verify
// after the target content changes (the digest binds the exact bytes).
func TestControllerTargetProofBindsPreexistingTarget(t *testing.T) {
	root, ownInput, _ := t027ProbeFixture(t)
	target := ownInput // controller-readable capture target
	_ = root
	proof, err := CaptureControllerTargetProof(target)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := VerifyControllerTargetProof(target, proof); err != nil {
		t.Fatalf("verify unchanged target: %v", err)
	}
	if err := os.WriteFile(target, []byte(`{"x":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyControllerTargetProof(target, proof); !errors.Is(err, ErrTargetChanged) {
		t.Fatalf("verify after content change: want ErrTargetChanged, got %v", err)
	}
	if _, err := CaptureControllerTargetProof(filepath.Join(root, "nope")); err == nil {
		t.Fatal("proof captured for a target that does not exist")
	}
}

// TestProbesForFreshBatchUsesSiblingFixture is the v2-full-run regression:
// the first attempt of a fresh batch has no in-flight or retired sibling,
// and probesFor must fall back to the permanent fixture so the fail-closed
// aggregation still sees exactly one sibling probe — not zero.
func TestProbesForFreshBatchUsesSiblingFixture(t *testing.T) {
	root := t.TempDir()
	// The forbidden-zone villas LoadOrInitHoldoutBatch materializes in
	// production (occupied 0000 files so probes observe denied, not-found
	// never happens without a proof).
	var locked []string
	for _, d := range []string{"private", "generation-audit", "author-receipts", "prior-reviews"} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"private", "generation-audit", "author-receipts", "prior-reviews"} {
		f := filepath.Join(root, d, map[string]string{
			"private":          "candidates.json",
			"generation-audit": "audit.json",
			"author-receipts":  "receipts.json",
			"prior-reviews":    "reviews.json",
		}[d])
		if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o000); err != nil {
			t.Fatal(err)
		}
		locked = append(locked, f)
	}
	// The forbidden directories themselves go 0000 last (production order),
	// so traverse/list probes observe denied.
	for _, d := range []string{"private", "generation-audit", "author-receipts", "prior-reviews"} {
		if err := os.Chmod(filepath.Join(root, d), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	dirs := []string{"private", "generation-audit", "author-receipts", "prior-reviews"}
	t.Cleanup(func() {
		// Directories back to traversable 0700 first (a 0000 dir's
		// children are unreachable), then files; the fixture sentinel is
		// 0000 too, or RemoveAll cannot unlink it.
		for _, d := range dirs {
			_ = os.Chmod(filepath.Join(root, d), 0o700)
		}
		for _, p := range locked {
			_ = os.Chmod(p, 0o600)
		}
		_ = os.Chmod(siblingSentinel(filepath.Join(root, "attempts", "sibling-fixture")), 0o600)
	})
	if err := ensureSiblingFixture(root); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// Idempotent on re-run (resume path).
	if err := ensureSiblingFixture(root); err != nil {
		t.Fatalf("fixture (idempotent): %v", err)
	}
	ownInput := filepath.Join(root, "attempts", "author", "000001-x", "input", "prompt.json")
	if err := os.MkdirAll(filepath.Dir(ownInput), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownInput, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &HoldoutBatch{cfg: HoldoutBatchConfig{Root: root}}
	probes := b.probesFor("att-x", ownInput, "")
	if err := ValidateIsolationProbes(StageAuthor, probes); err != nil {
		t.Fatalf("fresh-batch probe set invalid: %v", err)
	}
	fixtureTarget := siblingSentinel(filepath.Join(root, "attempts", "sibling-fixture"))
	for _, p := range probes {
		if p.Kind == ProbeActiveSiblingRead && p.TargetPath != fixtureTarget {
			t.Errorf("sibling probe target %s, want fixture %s", p.TargetPath, fixtureTarget)
		}
	}
	// A live sibling still wins over the fixture.
	liveWs := filepath.Join(root, "attempts", "author", "000002-y")
	if err := os.MkdirAll(filepath.Dir(siblingSentinel(liveWs)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingSentinel(liveWs), []byte("z"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(siblingSentinel(liveWs), 0o600) })
	probes = b.probesFor("att-x", ownInput, liveWs)
	if err := ValidateIsolationProbes(StageAuthor, probes); err != nil {
		t.Fatalf("live-sibling probe set invalid: %v", err)
	}
	for _, p := range probes {
		if p.Kind == ProbeActiveSiblingRead && p.TargetPath != siblingSentinel(liveWs) {
			t.Errorf("sibling probe target %s, want live %s", p.TargetPath, siblingSentinel(liveWs))
		}
	}
}

// TestCloseInterruptedAttempts pins the kill/restart resume path: dangling
// started events (hard runner kill) get an honest "interrupted" terminal,
// the ledger then verifies, and the closure is idempotent.
func TestCloseInterruptedAttempts(t *testing.T) {
	l := &AuthorReviewAttemptLedgerV1{}
	if err := l.AppendStarted(AttemptEvent{AttemptID: "a1", Stage: "author", Host: "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := l.AppendStarted(AttemptEvent{AttemptID: "a2", Stage: "review", Host: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := l.AppendTerminal(AttemptEvent{AttemptID: "a2", TerminalOutcome: strPtr("review-complete")}); err != nil {
		t.Fatal(err)
	}
	if err := l.VerifyLedger(); err == nil {
		t.Fatal("dangling a1 not caught before closure")
	}
	if err := l.CloseInterruptedAttempts(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := l.VerifyLedger(); err != nil {
		t.Fatalf("verify after closure: %v", err)
	}
	// An author attempt may also carry an append-only FINAL terminal
	// (admitted | rejected) after its production terminal — legal chain.
	if err := l.AppendTerminal(AttemptEvent{AttemptID: "a2", TerminalOutcome: strPtr("rejected")}); err != nil {
		t.Fatal(err)
	}
	if err := l.VerifyLedger(); err != nil {
		t.Fatalf("rejected-after-production rejected: %v", err)
	}
	// But never two production terminals…
	if err := l.AppendTerminal(AttemptEvent{AttemptID: "a2", TerminalOutcome: strPtr("candidate-ready")}); err != nil {
		t.Fatal(err)
	}
	if err := l.VerifyLedger(); err == nil {
		t.Fatal("second production terminal accepted")
	}
	// …and never two finals either.
	l2 := &AuthorReviewAttemptLedgerV1{}
	if err := l2.AppendStarted(AttemptEvent{AttemptID: "c1", Stage: "author", Host: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := l2.AppendTerminal(AttemptEvent{AttemptID: "c1", TerminalOutcome: strPtr("candidate-ready")}); err != nil {
		t.Fatal(err)
	}
	if err := l2.AppendTerminal(AttemptEvent{AttemptID: "c1", TerminalOutcome: strPtr("admitted")}); err != nil {
		t.Fatal(err)
	}
	if err := l2.AppendTerminal(AttemptEvent{AttemptID: "c1", TerminalOutcome: strPtr("rejected")}); err != nil {
		t.Fatal(err)
	}
	if err := l2.VerifyLedger(); err == nil {
		t.Fatal("second final terminal accepted")
	}
	if err := l.CloseInterruptedAttempts(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	// admitted without any production terminal is rejected outright.
	bad := &AuthorReviewAttemptLedgerV1{}
	if err := bad.AppendStarted(AttemptEvent{AttemptID: "b1", Stage: "author", Host: "claude"}); err != nil {
		t.Fatal(err)
	}
	if err := bad.AppendTerminal(AttemptEvent{AttemptID: "b1", TerminalOutcome: strPtr("admitted")}); err != nil {
		t.Fatal(err)
	}
	if err := bad.VerifyLedger(); err == nil {
		t.Fatal("admitted-before-production accepted")
	}
	term := 0
	for _, e := range l.Events {
		if e.EventKind == EventAttemptTerminal && e.AttemptID == "a1" {
			term++
			if e.TerminalOutcome == nil || *e.TerminalOutcome != "interrupted" {
				t.Errorf("a1 terminal outcome %v, want interrupted", e.TerminalOutcome)
			}
			if e.StartedEventDigest == nil {
				t.Error("a1 terminal lacks started_event_digest link")
			}
		}
	}
	if term != 1 {
		t.Errorf("a1 terminals = %d, want 1", term)
	}
}

// TestFilledSlotKeyDisambiguatesRepeatedTuples pins the instance-index rule:
// the frozen table legitimately repeats a four-tuple (count tables are the
// invariant, tuple uniqueness is not a contract requirement), so the second
// occurrence gets a "#1" key while the first keeps the bare key.
func TestFilledSlotKeyDisambiguatesRepeatedTuples(t *testing.T) {
	if got := FilledSlotKey("a/b/c/d", 0); got != "a/b/c/d" {
		t.Errorf("first occurrence = %q, want bare key", got)
	}
	if got := FilledSlotKey("a/b/c/d", 1); got != "a/b/c/d#1" {
		t.Errorf("second occurrence = %q, want a/b/c/d#1", got)
	}
	if got := FilledSlotKey("a/b/c/d", 2); got != "a/b/c/d#2" {
		t.Errorf("third occurrence = %q, want a/b/c/d#2", got)
	}
}
