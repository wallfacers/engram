package main

// T047/T048/T049 [US4] formal primary execution (data-model.md §9-§10,
// plan.md Phase 2.2-2.5).
//
//	T047 — bounded workers honoring the sealed concurrency (shared with the
//	       diagnostic mode), unique series/run roots, full-case ordering,
//	       disposable per-case state allocation/teardown, prior/retired-state
//	       probes, no retry boundary.
//	T048 — secret-filtered raw/normalized/store/workspace artifact
//	       persistence, per-case binding to the prepared host × worker-slot
//	       probe (identity/template/boundary drift ⇒ INVALID), and
//	       receipt/state-root completeness at run end.
//	T049 — three-host invocation parity through the frozen templates,
//	       event-date seed receipts, automatic host × slot workspace
//	       canaries, split-disjoint state allocators, controller target
//	       proofs and the closed protected access-probe matrix.
//
// Run root layout (fixed; every run root must be fresh and is never reused):
//
//	<RunRoot>/manifest.json               PrimaryRunManifest (written only on completion)
//	<RunRoot>/cases/<caseID>/             per-case rejudgeable artifacts
//	  raw.jsonl                           secret-filtered raw child stdout
//	  normalized-events.json              parsed event stream
//	  store-dump.txt                      post-turn store dump (filtered)
//	  stderr.txt                          filtered child stderr / terminal error
//	  seeds.json                          event-date seed receipts
//	  state-isolation.json                CaseStateIsolationReceipt
//	  case-receipt.json                   CaseRunReceipt
//	  workspace/                          the child's own materialized workspace (its cwd)
//	<RunRoot>/run/                        controller area (per-case MCP configs)
//	<RunRoot>/retired/state|workspace/    teardown destination (mode 0000)
//
// Per-case state roots live under the split's disposable allocator root
// (SplitAllocatorRoots, one tree per membership under the series root); a
// core172 run refuses to start while the holdout96 allocator already exists.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// ---------- T048: secret filtering ----------

// redactPatterns is the closed secret-shape set every primary artifact is
// filtered through before it is written under a run root. High-confidence
// token shapes run first, then the credential-ish key/value assignments
// (provenance.go's IsSecretLike key family), so a key whose value is a token
// collapses into a single marker.
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/-]{8,}\b`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd|authorization|bearer)\b["']?\s*[:=]\s*["']?[^\s"',;\\]+`),
}

const redactedMarker = "[REDACTED]"

// RedactSecrets replaces every secret-shaped region of raw with [REDACTED].
// Raw child output, stderr and the store dump are filtered before they are
// persisted, and every digest is computed over the filtered bytes, so a
// receipt always re-reads exactly the bytes that exist on disk.
func RedactSecrets(raw []byte) []byte {
	out := raw
	for _, re := range redactPatterns {
		out = re.ReplaceAll(out, []byte(redactedMarker))
	}
	return out
}

// ---------- T047: the one bounded worker implementation ----------
//
// Both the diagnostic mode and the primary mode dispatch their cases through
// this pool: at most `concurrency` items are in flight, each on a stable slot
// number (1..concurrency) so a case can be bound to its prepared worker slot,
// and every item runs exactly once — there is no retry boundary.

func runBounded(concurrency, count int, work func(index, slot int) error) (maxInFlight int, overlap bool, err error) {
	if concurrency < 1 {
		return 0, false, fmt.Errorf("bounded workers require concurrency >= 1 (got %d)", concurrency)
	}
	slots := make(chan int, concurrency)
	for s := 1; s <= concurrency; s++ {
		slots <- s
	}
	var (
		current    int32
		maxInf     int32
		overlapped int32
		errMu      sync.Mutex
		firstErr   error
		wg         sync.WaitGroup
	)
	for i := 0; i < count; i++ {
		wg.Add(1)
		slot := <-slots
		go func(idx, slot int) {
			defer wg.Done()
			defer func() { slots <- slot }()
			now := atomic.AddInt32(&current, 1)
			for {
				prev := atomic.LoadInt32(&maxInf)
				if now <= prev || atomic.CompareAndSwapInt32(&maxInf, prev, now) {
					break
				}
			}
			if now > 1 {
				atomic.StoreInt32(&overlapped, 1)
			}
			defer atomic.AddInt32(&current, -1)
			if werr := work(idx, slot); werr != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = werr
				}
				errMu.Unlock()
			}
		}(i, slot)
	}
	wg.Wait()
	return int(atomic.LoadInt32(&maxInf)), atomic.LoadInt32(&overlapped) == 1, firstErr
}

// ---------- roots, allocators, ordering ----------

// PrimaryRunRoot is the unique run root of one host × split × ordinal leg
// under a series root. The three-dimensional loop stays with the caller
// (T051 wiring): RunPrimary executes exactly one leg.
func PrimaryRunRoot(seriesRoot, host, split string, ordinal int) string {
	return filepath.Join(seriesRoot, "runs", fmt.Sprintf("%s-%s-o%d", host, split, ordinal))
}

// SplitAllocatorRoots returns the two split-disjoint disposable per-case
// state allocator roots of a series (§10 split_state_allocator_digests).
func SplitAllocatorRoots(seriesRoot string) map[string]string {
	return map[string]string{
		MembershipCore172:   filepath.Join(seriesRoot, "state-allocator", MembershipCore172),
		MembershipHoldout96: filepath.Join(seriesRoot, "state-allocator", MembershipHoldout96),
	}
}

// primarySeriesBase is the directory a run's allocators and its series-level
// state-root ledger live in: the parent of the run root (the series' runs
// directory), so every leg of one series shares one allocator pair and one
// ledger.
func primarySeriesBase(runRoot string) string {
	return filepath.Dir(runRoot)
}

// ValidateSplitAllocators enforces allocator disjointness and the
// core-before-holdout rule: the allocator of the split that has NOT run yet
// must not exist.
func ValidateSplitAllocators(roots map[string]string, executingSplit string) error {
	if len(roots) != 2 {
		return fmt.Errorf("want exactly core172+holdout96 allocators, got %d", len(roots))
	}
	core := roots[MembershipCore172]
	if core == "" {
		return errors.New("core172 state allocator root is required")
	}
	hold := roots[MembershipHoldout96]
	if hold == "" {
		return errors.New("holdout96 state allocator root is required")
	}
	if filepath.Clean(core) == filepath.Clean(hold) || isSubdir(core, hold) || isSubdir(hold, core) {
		return fmt.Errorf("split state allocators overlap: %s vs %s", core, hold)
	}
	executing, err := MembershipOfSplit(executingSplit)
	if err != nil {
		return err
	}
	other := MembershipHoldout96
	if executing == MembershipHoldout96 {
		other = MembershipCore172
	}
	if _, err := os.Stat(roots[other]); err == nil {
		return fmt.Errorf("%s state allocator %s already exists before the %s leg", other, roots[other], executing)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ValidatePrimaryCaseSet is the exact-split-coverage gate the wiring (T051)
// applies before a real primary leg: a formal split is closed at 172/96 and
// never runs a subsampled slice. RunPrimary itself enforces it only when
// PrimaryRunOptions.EnforceSplitSize is set, which keeps the executor
// testable on a small injected fixture.
func ValidatePrimaryCaseSet(split string, ids []string) error {
	membership, err := MembershipOfSplit(split)
	if err != nil {
		return err
	}
	want, err := ExpectedQuestionCount(membership)
	if err != nil {
		return err
	}
	if len(ids) != want {
		return fmt.Errorf("split %s binds exactly %d cases, got %d — primary never runs a partial slice", split, want, len(ids))
	}
	return nil
}

// PrimaryCaseOrder is the deterministic case order of one ordinal: the
// sorted case set permuted by the sealed ordinal seed. Identical
// (ids, seed) inputs always produce the identical order.
func PrimaryCaseOrder(ids []string, seed string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	sort.SliceStable(out, func(i, j int) bool {
		return sha256Hex([]byte(seed+"\x00"+out[i])) < sha256Hex([]byte(seed+"\x00"+out[j]))
	})
	return out
}

// ---------- digest helpers ----------

func pathDigest(path string) string {
	return sha256Hex([]byte("path\x00" + path))
}

// accessPolicyDigest binds the actual parent-directory policy (the mode the
// target inherits), so a probe target chmod'ed only for the probe cannot
// pass as the real workspace policy.
func accessPolicyDigest(path string) string {
	fi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return ""
	}
	return sha256Hex([]byte("access-policy\x00" + fmt.Sprintf("%04o", fi.Mode().Perm())))
}

// dirInventoryDigest is the exact-byte inventory digest of a directory tree:
// sorted relative paths, each bound to its content digest.
func dirInventoryDigest(dir string) (string, error) {
	type rec struct{ rel, sum string }
	var recs []rec
	err := filepath.Walk(dir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		recs = append(recs, rec{rel, sha256Hex(b)})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].rel < recs[j].rel })
	pre := ""
	for _, r := range recs {
		pre += r.rel + "\x00" + r.sum + "\n"
	}
	return sha256Hex([]byte(pre)), nil
}

// ---------- T049: host invocation parity ----------

// PrimaryHostTemplates resolves the frozen invocation template and digest for
// every host from one lane configuration. A configuration that cannot express
// all three hosts has no primary parity and is rejected before any child runs.
func PrimaryHostTemplates(lane CLIReviewConfig) (map[string]InvocationTemplate, map[string]string, error) {
	templates := map[string]InvocationTemplate{}
	digests := map[string]string{}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		t, err := TemplateForHost(h, lane.ClaudeSettings, lane.CodexProvider, lane.CodexModel, lane.OpenCodeModel)
		if err != nil {
			return nil, nil, fmt.Errorf("host %s invocation template: %w", h, err)
		}
		d, err := t.Digest()
		if err != nil {
			return nil, nil, err
		}
		templates[h] = t
		digests[h] = d
	}
	return templates, digests, nil
}

// NormalizedCoreTemplateDigest binds the three hosts' frozen invocation
// templates as one normalized execution-template digest. It is the value the
// core plan freezes, every prepared probe/canary carries and the protected
// receipt reports as its ExecutionTemplateSetDigest — one shared normalized
// identity, with each host's concrete template still distinguishable through
// its own digest inside the same canonical map.
func NormalizedCoreTemplateDigest(templateDigests map[string]string) (string, error) {
	if len(templateDigests) != 3 {
		return "", fmt.Errorf("normalized execution template needs exactly 3 host digests, got %d", len(templateDigests))
	}
	return CanonicalSHA256(templateDigests)
}

// NormalizedCoreWorkerIdentitySetDigest derives the normalized worker
// identity set a core plan freezes: the sorted host × slot × child-identity
// set over the normalized execution template — the same preimage
// RunProtectedProbes reports as its WorkerIdentitySetDigest.
func NormalizedCoreWorkerIdentitySetDigest(hosts []string, concurrency int, normalizedTemplateDigest string) (string, error) {
	if concurrency < 1 {
		return "", errors.New("worker identity set needs a positive concurrency")
	}
	sorted := append([]string(nil), hosts...)
	sort.Strings(sorted)
	identities := make([]string, 0, len(sorted)*concurrency)
	for _, h := range sorted {
		for slot := 1; slot <= concurrency; slot++ {
			identities = append(identities, h+"\x00"+fmt.Sprint(slot)+"\x00"+primaryChildIdentity(h, slot, normalizedTemplateDigest))
		}
	}
	return CanonicalSHA256(identities)
}

// primaryChildIdentity is the effective exact-child identity of one prepared
// host × worker slot: host, slot and the normalized execution template.
func primaryChildIdentity(host string, slot int, templateDigest string) string {
	return sha256Hex([]byte("primary-child\x00" + host + "\x00" + fmt.Sprint(slot) + "\x00" + templateDigest))
}

// primaryAccessBoundary is the boundary policy digest of one slot under the
// operator-provided isolation mechanism. It is never a raw UID/container id.
func primaryAccessBoundary(boundary BoundaryKind, isolationConfigDigest, host string, slot int) string {
	return sha256Hex([]byte("access-boundary\x00" + string(boundary) + "\x00" + isolationConfigDigest + "\x00" + host + "\x00" + fmt.Sprint(slot)))
}

// preparedWorkerProbe returns the prepared probe of one host × slot from the
// protected execution receipt. A missing prepared probe (or a slot beyond the
// prepared capacity) is an INVALID run, never an improvised probe.
func preparedWorkerProbe(r *ProtectedExecutionReceipt, host string, slot int) (*ProtectedWorkerProbe, error) {
	if r == nil {
		return nil, errors.New("nil protected execution receipt")
	}
	if slot < 1 || slot > r.RequiredConcurrency {
		return nil, fmt.Errorf("worker slot %d outside the prepared capacity %d", slot, r.RequiredConcurrency)
	}
	for i := range r.WorkerProbes {
		p := &r.WorkerProbes[i]
		if p.Host == host && p.WorkerSlot == slot {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no prepared worker probe for %s slot %d", host, slot)
}

// preparedProbeFor resolves the prepared slot identity a case binds to. An
// official-dual series reads it from the protected execution receipt; a
// dev-comparison series (no receipt by construction) derives it from the same
// pure functions series prepare froze — the normalized template and the
// plan's boundary + isolation-config digest. The derived probe carries no
// access-probe matrix: its Probes stay empty and nothing may treat it as
// protected-execution evidence.
func preparedProbeFor(opts PrimaryRunOptions, host string, slot int) (*ProtectedWorkerProbe, error) {
	if opts.Protected != nil {
		return preparedWorkerProbe(opts.Protected, host, slot)
	}
	if opts.IsolationConfigDigest == "" {
		return nil, errors.New("a dev-comparison primary run requires the frozen isolation config digest")
	}
	_, digests, err := PrimaryHostTemplates(opts.Lane)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizedCoreTemplateDigest(digests)
	if err != nil {
		return nil, err
	}
	return &ProtectedWorkerProbe{
		Host:                    host,
		WorkerSlot:              slot,
		ChildIdentityDigest:     primaryChildIdentity(host, slot, normalized),
		ExecutionTemplateDigest: normalized,
		AccessBoundaryDigest:    primaryAccessBoundary(opts.Plan.CoreBoundaryKind, opts.IsolationConfigDigest, host, slot),
	}, nil
}

// ---------- T047: RunPrimary ----------

// PrimaryRunObservation reports the bounded-pool facts a caller needs to
// prove the sealed concurrency was honored: the hard bound (max in flight ≤
// concurrency) and the observed overlap. A fast child can finish before a
// sibling starts, so overlap is reported rather than enforced; the bound is
// enforced.
type PrimaryRunObservation struct {
	MaxInFlight int
	Overlap     bool
}

// PrimaryRunOptions configures exactly one primary leg: one series, one host,
// one split, one ordinal. The host × split × ordinal loop is the caller's.
type PrimaryRunOptions struct {
	SeriesID    string
	Host        string
	Split       string // dev-regression (core172) | holdout (holdout96)
	Ordinal     int
	Concurrency int

	Plan      *CoreExecutionPlanReceipt
	Manifest  *FormalSeriesManifest
	Protected *ProtectedExecutionReceipt
	Lane      CLIReviewConfig

	// IsolationConfigDigest backs the per-slot boundary digest of a
	// dev-comparison series, which carries no protected execution receipt by
	// construction; the same digest series prepare froze.
	IsolationConfigDigest string

	Cases   map[string]*TriggerCaseV2
	CaseIDs []string // exact required coverage; every id runs exactly once

	RunRoot string
	BinDir  string

	// EnforceSplitSize applies the closed 172/96 coverage gate. T051 wiring
	// must set it; it stays a flag so a small injected fixture can exercise
	// the executor offline without faking a 172-case dataset.
	EnforceSplitSize bool

	// Execution seams: nil selects the production implementation; tests
	// inject deterministic children/seeders/dumpers/probes (all offline).
	ChildRunner caseChildRunner
	SeedRunner  func(caseDir string, c *TriggerCaseV2) ([]SeedRenderResult, error)
	StoreDumper func(caseDir string) (string, error)
	Probe       ProbeFunc

	Observation *PrimaryRunObservation
}

type primaryLayout struct {
	run              string
	cases            string
	controller       string
	retiredState     string
	retiredWorkspace string
	allocator        string
}

const primaryBootstrapID = "000000-run-bootstrap"

type primaryRunner struct {
	opts       PrimaryRunOptions
	layout     primaryLayout
	membership string
	order      []string
	mgr        *StageWorkspaceManager
	probe      ProbeFunc

	mu       sync.Mutex
	receipts []CaseRunReceipt
}

// RunPrimary executes one formal primary leg end to end: full-case coverage
// on bounded workers, disposable per-case state with prior/retired-state
// probes, secret-filtered artifact persistence bound to the prepared host ×
// worker-slot probe, and a sealed PrimaryRunManifest written only when the
// leg is complete. Any structural failure (missing case, probe failure,
// identity drift, incomplete coverage) leaves the run INVALID and writes no
// manifest.
func RunPrimary(opts PrimaryRunOptions) (*PrimaryRunManifest, []CaseRunReceipt, error) {
	startedAt := nowRFC3339()
	r, err := newPrimaryRunner(opts)
	if err != nil {
		return nil, nil, err
	}
	maxInFlight, overlap, poolErr := runBounded(opts.Concurrency, len(r.order), r.runCase)
	if opts.Observation != nil {
		opts.Observation.MaxInFlight = maxInFlight
		opts.Observation.Overlap = overlap
	}
	if maxInFlight > opts.Concurrency {
		return r.invalidManifest(startedAt, nowRFC3339()), r.receipts,
			fmt.Errorf("observed %d cases in flight, above the sealed concurrency %d", maxInFlight, opts.Concurrency)
	}
	if poolErr != nil {
		return r.invalidManifest(startedAt, nowRFC3339()), r.receipts, poolErr
	}
	if err := r.verifyCompleteness(); err != nil {
		return r.invalidManifest(startedAt, nowRFC3339()), r.receipts, err
	}
	manifest, err := r.sealManifest(startedAt)
	if err != nil {
		return nil, r.receipts, err
	}
	return manifest, r.receipts, nil
}

func newPrimaryRunner(opts PrimaryRunOptions) (*primaryRunner, error) {
	if opts.SeriesID == "" {
		return nil, errors.New("primary run requires a series id")
	}
	if !validHosts[opts.Host] {
		return nil, fmt.Errorf("unknown host %q", opts.Host)
	}
	membership, err := MembershipOfSplit(opts.Split)
	if err != nil {
		return nil, err
	}
	ordinalOK := false
	for _, o := range Ordinals {
		if opts.Ordinal == o {
			ordinalOK = true
		}
	}
	if !ordinalOK {
		return nil, fmt.Errorf("ordinal %d is not one of the three mandatory repetitions", opts.Ordinal)
	}
	if opts.Concurrency < 1 {
		return nil, errors.New("primary mode requires the sealed --concurrency >= 1")
	}
	if opts.Plan == nil {
		return nil, errors.New("primary mode requires the sealed core execution plan")
	}
	if err := ValidateCoreExecutionPlan(opts.Plan); err != nil {
		return nil, fmt.Errorf("core execution plan: %w", err)
	}
	// A protected execution receipt is the official-dual precondition. A
	// dev-comparison series has none by construction — its per-slot boundary
	// identity derives from the plan and the frozen isolation config instead.
	if opts.Protected == nil {
		if opts.Manifest.Purpose != PurposeDevComparison {
			return nil, errors.New("primary mode requires the prepared protected execution receipt")
		}
		if opts.IsolationConfigDigest == "" {
			return nil, errors.New("a dev-comparison primary run requires the frozen isolation config digest")
		}
	} else {
		if err := ValidateProtectedExecutionReceipt(opts.Protected, opts.Plan); err != nil {
			return nil, fmt.Errorf("protected execution receipt: %w", err)
		}
		if opts.Protected.RequiredConcurrency != opts.Concurrency {
			return nil, fmt.Errorf("protected receipt concurrency %d, sealed run concurrency %d", opts.Protected.RequiredConcurrency, opts.Concurrency)
		}
	}
	if opts.Manifest == nil {
		return nil, errors.New("primary mode requires the prepared formal series manifest")
	}
	if opts.Manifest.SeriesID != opts.SeriesID {
		return nil, fmt.Errorf("series manifest %q does not match run series %q", opts.Manifest.SeriesID, opts.SeriesID)
	}
	if !stringInList(opts.Manifest.Hosts, opts.Host) {
		return nil, fmt.Errorf("series %s does not bind host %s", opts.SeriesID, opts.Host)
	}
	if opts.Manifest.CoreExecutionPlanDigest != opts.Plan.ReceiptDigest {
		return nil, errors.New("series manifest references a different core execution plan")
	}
	if opts.Manifest.Concurrency != opts.Concurrency {
		return nil, fmt.Errorf("series concurrency %d, run concurrency %d", opts.Manifest.Concurrency, opts.Concurrency)
	}
	hostPlanned := false
	for _, h := range opts.Plan.Hosts {
		if h == opts.Host {
			hostPlanned = true
		}
	}
	if !hostPlanned {
		return nil, fmt.Errorf("host %s is not part of the sealed plan", opts.Host)
	}
	if opts.EnforceSplitSize {
		if err := ValidatePrimaryCaseSet(opts.Split, opts.CaseIDs); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(opts.RunRoot); err == nil {
		return nil, fmt.Errorf("run root %s already exists — primary runs require unique roots and never overwrite a run", opts.RunRoot)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range opts.CaseIDs {
		if id == "" || seen[id] {
			return nil, fmt.Errorf("required case id %q is empty or repeated", id)
		}
		seen[id] = true
		c := opts.Cases[id]
		if c == nil {
			return nil, fmt.Errorf("required case %s is missing from the split case set — the run cannot be complete", id)
		}
		if !safeAttemptID(id) {
			return nil, fmt.Errorf("case id %q is not containment-safe", id)
		}
	}
	if len(opts.Cases) != len(opts.CaseIDs) {
		var extra []string
		for id := range opts.Cases {
			if !seen[id] {
				extra = append(extra, id)
			}
		}
		return nil, fmt.Errorf("case set has %d entries for %d required case ids (outside coverage: %v)",
			len(opts.Cases), len(opts.CaseIDs), sortedCopy(extra))
	}
	for slot := 1; slot <= opts.Concurrency; slot++ {
		if _, err := preparedProbeFor(opts, opts.Host, slot); err != nil {
			return nil, err
		}
	}
	// Invocation parity (T049): all three host templates must resolve from
	// one lane configuration before any child is spawned.
	if _, _, err := PrimaryHostTemplates(opts.Lane); err != nil {
		return nil, err
	}

	seriesRoot := primarySeriesBase(opts.RunRoot)
	allocators := SplitAllocatorRoots(seriesRoot)
	if err := ValidateSplitAllocators(allocators, opts.Split); err != nil {
		return nil, err
	}
	layout := primaryLayout{
		run:              opts.RunRoot,
		cases:            filepath.Join(opts.RunRoot, "cases"),
		controller:       filepath.Join(opts.RunRoot, "run"),
		retiredState:     filepath.Join(opts.RunRoot, "retired", "state"),
		retiredWorkspace: filepath.Join(opts.RunRoot, "retired", "workspace"),
		allocator:        allocators[membership],
	}
	for _, d := range []string{layout.run, layout.cases, layout.controller, layout.retiredState, layout.retiredWorkspace} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	seed := opts.Plan.CaseOrderSeeds[opts.Ordinal]
	if seed == "" {
		return nil, fmt.Errorf("plan carries no case order seed for ordinal %d", opts.Ordinal)
	}
	if opts.ChildRunner == nil {
		opts.ChildRunner = envPrimaryChild(opts)
	}
	if opts.SeedRunner == nil {
		opts.SeedRunner = defaultSeedRunner(opts.BinDir)
	}
	if opts.StoreDumper == nil {
		opts.StoreDumper = defaultStoreDumper(opts.BinDir)
	}
	r := &primaryRunner{
		opts:       opts,
		layout:     layout,
		membership: membership,
		order:      PrimaryCaseOrder(opts.CaseIDs, seed),
		mgr:        NewStageWorkspaceManager(layout.allocator, opts.Concurrency),
		probe:      opts.Probe,
	}
	if r.probe == nil {
		r.probe = ProbeFilesystem
	}
	// The bootstrap anchors are this run's genuinely retired prior state:
	// the first scheduled case (and any case racing a sibling's teardown)
	// proves its denial against them instead of improvising a target.
	if err := r.writeRetiredAnchor(filepath.Join(layout.retiredState, primaryBootstrapID), "prior-case-state anchor"); err != nil {
		return nil, err
	}
	if err := r.writeRetiredAnchor(filepath.Join(layout.retiredWorkspace, primaryBootstrapID), "retired-workspace anchor"); err != nil {
		return nil, err
	}
	return r, nil
}

// writeRetiredAnchor materializes one mode-0000 retired root.
func (r *primaryRunner) writeRetiredAnchor(dir, label string) error {
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o700); err != nil {
		return err
	}
	if err := osWriteFile(filepath.Join(dir, "state", "prior-state.txt"), []byte(label+"\n")); err != nil {
		return err
	}
	return os.Chmod(dir, 0o000)
}

// RestoreProbePermissions restores 0755 on every directory under root so a
// probe/retirement tree can be removed again (mode-0000 directories are
// untraversable, which is the point of the probe and the nuisance of the
// cleanup).
func RestoreProbePermissions(root string) error {
	stack := []string{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := os.Chmod(cur, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				stack = append(stack, filepath.Join(cur, e.Name()))
			}
		}
	}
	return nil
}

// prior/retired probe targets: the previously scheduled case's retired roots
// once they exist, else the run's bootstrap anchors.
func (r *primaryRunner) priorStateTarget(idx int) string {
	if idx > 0 {
		p := filepath.Join(r.layout.retiredState, r.order[idx-1])
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(r.layout.retiredState, primaryBootstrapID)
}

func (r *primaryRunner) retiredWorkspaceTarget(idx int) string {
	if idx > 0 {
		p := filepath.Join(r.layout.retiredWorkspace, r.order[idx-1])
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(r.layout.retiredWorkspace, primaryBootstrapID)
}

// runCase is one bounded-worker unit: acquire a fresh disposable state root,
// prove the prior/retired denials, run exactly one agent attempt, persist the
// filtered artifacts bound to the prepared slot probe, then retire the state.
func (r *primaryRunner) runCase(idx, slot int) error {
	caseID := r.order[idx]
	c := r.opts.Cases[caseID]
	prepared, err := preparedProbeFor(r.opts, r.opts.Host, slot)
	if err != nil {
		return err
	}
	ws, err := r.mgr.Acquire(StageAuthor, caseID)
	if err != nil {
		return err
	}
	defer func() { _ = r.mgr.Release(caseID) }()
	stateRoot := filepath.Join(ws, "state")

	caseDir := filepath.Join(r.layout.cases, caseID)
	workspaceDir := filepath.Join(caseDir, "workspace")
	for _, d := range []string{caseDir, workspaceDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	priorProbe, err := r.runFormalProbe(FProbePriorCaseStateRead, r.priorStateTarget(idx), probeDenied)
	if err != nil {
		return err
	}
	retiredProbe, err := r.runFormalProbe(FProbeRetiredWorkspaceRead, r.retiredWorkspaceTarget(idx), probeDenied)
	if err != nil {
		return err
	}
	// The own disposable state root must be fresh: present, empty, readable.
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		return fmt.Errorf("case %s state root is unreadable: %w", caseID, err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("case %s state root is not fresh (%d leftover entries)", caseID, len(entries))
	}
	if got := r.probe(stateRoot); got != ProbeReadable {
		return fmt.Errorf("case %s cannot read its own fresh state root (%s)", caseID, got)
	}

	// Own workspace: materialize, digest, then hand exactly this directory to
	// the child as its cwd and store location.
	writeWorkspaceTemplate(workspaceDir)
	if err := MaterializeWorkspace(workspaceDir, c.WorkspaceFiles); err != nil {
		return err
	}
	workspaceDigest, err := dirInventoryDigest(workspaceDir)
	if err != nil {
		return err
	}
	seedReceipts, err := r.opts.SeedRunner(workspaceDir, c)
	if err != nil {
		return fmt.Errorf("case %s seeding: %w", caseID, err)
	}
	if err := writeFrozenJSON(filepath.Join(caseDir, "seeds.json"), seedReceipts); err != nil {
		return err
	}

	userTurn := ""
	if c.Prompt != nil {
		userTurn = *c.Prompt
	} else {
		for _, t := range c.Turns {
			if t.Role == "user" && !t.SetupOnly {
				userTurn = t.Content
			}
		}
	}
	raw, runErr := r.opts.ChildRunner(r.opts.Host, workspaceDir, userTurn, c)
	if runErr != nil && raw == nil {
		raw = []byte{}
	}
	filtered := RedactSecrets(raw)
	if err := osWriteFile(filepath.Join(caseDir, "raw.jsonl"), filtered); err != nil {
		return err
	}
	var events []Event
	switch r.opts.Host {
	case HostClaude:
		events = ParseClaude(bytes.NewReader(filtered))
	case HostCodex:
		events = ParseCodex(bytes.NewReader(filtered))
	case HostOpenCode:
		events = ParseOpenCode(bytes.NewReader(filtered))
	default:
		return fmt.Errorf("unknown host %q", r.opts.Host)
	}
	if err := writeFrozenJSON(filepath.Join(caseDir, "normalized-events.json"), events); err != nil {
		return err
	}
	dump, err := r.opts.StoreDumper(workspaceDir)
	if err != nil {
		return fmt.Errorf("case %s store dump: %w", caseID, err)
	}
	storeDump := string(RedactSecrets([]byte(dump)))
	if err := osWriteFile(filepath.Join(caseDir, "store-dump.txt"), []byte(storeDump)); err != nil {
		return err
	}
	stderr := ""
	if runErr != nil {
		stderr = runErr.Error()
	}
	stderr = string(RedactSecrets([]byte(stderr)))
	if err := osWriteFile(filepath.Join(caseDir, "stderr.txt"), []byte(stderr)); err != nil {
		return err
	}

	verdict := JudgeV2(c, events, storeDump, runErr)
	verdict.Tool = r.opts.Host
	status := "fail"
	switch {
	case verdict.Pass:
		status = "pass"
	case verdict.Failure == "runner-error":
		status = "runner-error"
	}

	payloadDigest, err := CanonicalSHA256(c)
	if err != nil {
		return err
	}
	preparedDigest, err := CanonicalSHA256(prepared)
	if err != nil {
		return err
	}

	// T048: the case is bound to its prepared host × worker slot probe. Any
	// drift between what series prepare proved and what this attempt runs is
	// an INVALID run, never a re-attestation.
	if prepared.ExecutionTemplateDigest != r.templateDigest() {
		return fmt.Errorf("case %s execution template drifts from the prepared %s/%d probe", caseID, r.opts.Host, slot)
	}
	if prepared.ChildIdentityDigest != primaryChildIdentity(r.opts.Host, slot, prepared.ExecutionTemplateDigest) {
		return fmt.Errorf("case %s child identity drifts from the prepared %s/%d probe", caseID, r.opts.Host, slot)
	}
	boundaryKind, boundaryIso := r.boundaryInputs()
	if prepared.AccessBoundaryDigest != primaryAccessBoundary(boundaryKind, boundaryIso, r.opts.Host, slot) {
		return fmt.Errorf("case %s access boundary drifts from the prepared %s/%d probe", caseID, r.opts.Host, slot)
	}

	// Retirement boundary first: the state root and the workspace move into
	// the run's retired area and go mode-0000, so the next case's
	// prior/retired probes have a real, existing, unreadable target and the
	// receipt records the boundary it was retired at.
	retiredTo, err := r.retireCase(stateRoot, workspaceDir, caseID)
	if err != nil {
		return err
	}
	iso := CaseStateIsolationReceipt{
		SeriesID:                        r.opts.SeriesID,
		Host:                            r.opts.Host,
		Split:                           r.opts.Split,
		Ordinal:                         r.opts.Ordinal,
		CaseID:                          caseID,
		WorkerSlot:                      slot,
		ProtectedExecutionReceiptDigest: r.protectedReceiptDigest(),
		PreparedWorkerProbeDigest:       preparedDigest,
		ChildIdentityDigest:             prepared.ChildIdentityDigest,
		ExecutionTemplateDigest:         prepared.ExecutionTemplateDigest,
		AccessBoundaryDigest:            prepared.AccessBoundaryDigest,
		FreshStateRootDigest:            pathDigest(stateRoot),
		StateAllocatorDigest:            pathDigest(r.layout.allocator),
		PriorStateProbe:                 &priorProbe,
		RetiredWorkspaceProbe:           &retiredProbe,
		ResetMethod:                     "dispose-and-recreate",
		ChildTeardown:                   "child-process-wait-then-state-retire",
		RetirementOrFinalDelete:         retiredTo,
	}
	isoDigest, err := CanonicalSHA256(iso)
	if err != nil {
		return err
	}
	receipt := CaseRunReceipt{
		CaseID:                          caseID,
		CasePayloadDigest:               payloadDigest,
		WorkspaceDigest:                 workspaceDigest,
		CaseStateIsolationReceiptDigest: isoDigest,
		AttemptCount:                    1, // exactly one attempt; no retry boundary
		Status:                          status,
		NormalizedEventsPath:            filepath.Join(caseDir, "normalized-events.json"),
		RawEventsPath:                   filepath.Join(caseDir, "raw.jsonl"),
		StoreDumpPath:                   filepath.Join(caseDir, "store-dump.txt"),
		Verdict:                         verdict,
		StderrDigest:                    sha256Hex([]byte(stderr)),
	}
	receipt.NormalizedEventsDigest = fileDigest(receipt.NormalizedEventsPath)
	receipt.RawEventsDigest = fileDigest(receipt.RawEventsPath)
	receipt.StoreDumpDigest = fileDigest(receipt.StoreDumpPath)
	if err := writeFrozenJSON(filepath.Join(caseDir, "state-isolation.json"), iso); err != nil {
		return err
	}
	if err := writeFrozenJSON(filepath.Join(caseDir, "case-receipt.json"), receipt); err != nil {
		return err
	}

	r.mu.Lock()
	r.receipts = append(r.receipts, receipt)
	r.mu.Unlock()
	return nil
}

// templateDigest is the frozen host execution template this run is bound to.
// templateDigest returns the normalized execution template this lane resolves
// to — the same digest the plan freezes and every prepared probe carries.
func (r *primaryRunner) templateDigest() string {
	_, digests, err := PrimaryHostTemplates(r.opts.Lane)
	if err != nil {
		return ""
	}
	d, err := NormalizedCoreTemplateDigest(digests)
	if err != nil {
		return ""
	}
	return d
}

// boundaryInputs are the two values a slot's access-boundary digest derives
// from: the protected receipt freezes them for official-dual; a
// dev-comparison series takes them from the plan and the frozen isolation
// config.
func (r *primaryRunner) boundaryInputs() (BoundaryKind, string) {
	if r.opts.Protected != nil {
		return r.opts.Protected.BoundaryKind, r.opts.Protected.IsolationConfigDigest
	}
	return r.opts.Plan.CoreBoundaryKind, r.opts.IsolationConfigDigest
}

// protectedReceiptDigest is the protected receipt this run binds; a
// dev-comparison series has none and carries the empty digest.
func (r *primaryRunner) protectedReceiptDigest() string {
	if r.opts.Protected == nil {
		return ""
	}
	return r.opts.Protected.ReceiptDigest
}

// runFormalProbe captures the controller target proof, then performs the
// real access attempt and closes the outcome.
func (r *primaryRunner) runFormalProbe(kind FormalProbeKind, target, expected string) (FormalAccessProbe, error) {
	return runFormalProbeProven(r.probe, kind, target, expected, captureProof, nil)
}

const (
	probeDenied   = "denied"
	probeReadable = "readable"
)

// captureProof is the default controller-proof capture (used when the
// controller can still see the target).
func captureProof(target string) (string, error) {
	proof, err := CaptureControllerTargetProof(target)
	if err != nil {
		return "", err
	}
	return proof.Digest(), nil
}

// runFormalProbeProven performs one access attempt against a target whose
// controller evidence is already known. Proof and policy are captured
// *before* the boundary locks: once a root is mode-0000 the controller
// itself cannot stat into it, so anything recorded afterwards would be an
// invention. policyOf may be nil (the live parent policy is used, which is
// correct when the parent is still traversable).
func runFormalProbeProven(probe ProbeFunc, kind FormalProbeKind, target, expected string, proof captureFunc, policyOf func(string) string) (FormalAccessProbe, error) {
	proofDigest := ""
	if expected == probeDenied {
		if proof == nil {
			proof = captureProof
		}
		d, err := proof(target)
		if err != nil {
			return FormalAccessProbe{}, fmt.Errorf("probe %s: controller target proof: %w", kind, err)
		}
		proofDigest = d
	}
	policyDigest := ""
	if policyOf != nil {
		policyDigest = policyOf(target)
	} else {
		policyDigest = accessPolicyDigest(target)
	}
	observed := probe(target)
	p := FormalAccessProbe{
		Kind:                        kind,
		TargetDigest:                pathDigest(target),
		TargetAccessPolicyDigest:    policyDigest,
		ControllerTargetProofDigest: proofDigest,
		Expected:                    expected,
		Outcome:                     probeOutcome(observed),
	}
	if expected == probeDenied && observed != ProbeDenied && observed != ProbeNotFound {
		return p, fmt.Errorf("probe %s observed %q on the forbidden target, want a closed denial", kind, p.Outcome)
	}
	if expected == probeReadable && observed != ProbeReadable {
		return p, fmt.Errorf("probe %s observed %q, want readable", kind, p.Outcome)
	}
	return p, nil
}

type captureFunc func(target string) (string, error)

func probeOutcome(observed string) string {
	switch observed {
	case ProbeDenied:
		return "permission-denied"
	case ProbeNotFound:
		return "not-found"
	case ProbeReadable:
		return "readable"
	}
	return "unknown:" + observed
}

func (r *primaryRunner) retireCase(stateRoot, workspaceDir, caseID string) (string, error) {
	dst := filepath.Join(r.layout.retiredState, caseID)
	if err := os.Rename(stateRoot, dst); err != nil {
		return "", err
	}
	wdst := filepath.Join(r.layout.retiredWorkspace, caseID)
	if err := os.Rename(workspaceDir, wdst); err != nil {
		return "", err
	}
	for _, d := range []string{dst, wdst} {
		if err := os.Chmod(d, 0o000); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// verifyCompleteness is the T048 run-end gate: every scheduled case ran
// exactly once with its own isolation receipt, artifacts and a state root
// that no other case, ordinal or split of this series has used.
func (r *primaryRunner) verifyCompleteness() error {
	seen := map[string]bool{}
	byIso := map[string]string{}
	for _, rec := range r.receipts {
		if seen[rec.CaseID] {
			return fmt.Errorf("case %s ran more than once", rec.CaseID)
		}
		seen[rec.CaseID] = true
		if rec.AttemptCount != 1 {
			return fmt.Errorf("case %s records %d attempts, want exactly 1", rec.CaseID, rec.AttemptCount)
		}
		if rec.CaseStateIsolationReceiptDigest == "" {
			return fmt.Errorf("case %s has no state isolation receipt", rec.CaseID)
		}
		if prev := byIso[rec.CaseStateIsolationReceiptDigest]; prev != "" {
			return fmt.Errorf("cases %s and %s share one state isolation receipt", prev, rec.CaseID)
		}
		byIso[rec.CaseStateIsolationReceiptDigest] = rec.CaseID
		for _, p := range []string{rec.RawEventsPath, rec.NormalizedEventsPath, rec.StoreDumpPath} {
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("case %s artifact %s is missing: %v", rec.CaseID, p, err)
			}
		}
	}
	if len(seen) != len(r.order) {
		var missing []string
		for _, id := range r.order {
			if !seen[id] {
				missing = append(missing, id)
			}
		}
		return fmt.Errorf("incomplete primary run: %d/%d cases ran, missing %v", len(seen), len(r.order), missing)
	}

	ledgerPath := filepath.Join(primarySeriesBase(r.opts.RunRoot), "state-root-ledger.jsonl")
	entries, err := readStateRootLedger(ledgerPath)
	if err != nil {
		return err
	}
	existing := len(entries)
	for _, rec := range r.receipts {
		iso, err := loadStateIsolation(filepath.Join(r.layout.cases, rec.CaseID, "state-isolation.json"))
		if err != nil {
			return err
		}
		if iso.SeriesID != r.opts.SeriesID || iso.Host != r.opts.Host || iso.Split != r.opts.Split || iso.Ordinal != r.opts.Ordinal {
			return fmt.Errorf("case %s isolation receipt identity drifts from this run", rec.CaseID)
		}
		if iso.WorkerSlot < 1 || iso.WorkerSlot > r.opts.Concurrency {
			return fmt.Errorf("case %s ran on worker slot %d, outside the sealed capacity", rec.CaseID, iso.WorkerSlot)
		}
		if iso.FreshStateRootDigest == "" || iso.StateAllocatorDigest != pathDigest(r.layout.allocator) {
			return fmt.Errorf("case %s isolation receipt has no fresh state root in this allocator", rec.CaseID)
		}
		entries = append(entries, stateRootLedgerEntry{
			SeriesID: iso.SeriesID, Host: iso.Host, Split: iso.Split, Ordinal: iso.Ordinal,
			CaseID: iso.CaseID, StateRootDigest: iso.FreshStateRootDigest,
		})
	}
	seenRoot := map[string]string{}
	for i, e := range entries {
		if prev, dup := seenRoot[e.StateRootDigest]; dup {
			if i < existing {
				return fmt.Errorf("this series already used the state root of %s: reuse by %s/%s", prev, r.opts.Host, e.CaseID)
			}
			return fmt.Errorf("cases %s and %s share one fresh state root", prev, e.CaseID)
		}
		seenRoot[e.StateRootDigest] = e.CaseID
	}
	return writeStateRootLedger(ledgerPath, entries)
}

func (r *primaryRunner) invalidManifest(startedAt, completedAt string) *PrimaryRunManifest {
	return &PrimaryRunManifest{
		Mode: "primary", SeriesID: r.opts.SeriesID, Host: r.opts.Host, Split: r.opts.Split,
		Ordinal: r.opts.Ordinal, CaseIDs: sortedCopy(r.opts.CaseIDs), ExpectedCaseCount: len(r.opts.CaseIDs),
		StartedAt: startedAt, CompletedAt: completedAt, State: StateInvalid,
	}
}

func (r *primaryRunner) sealManifest(startedAt string) (*PrimaryRunManifest, error) {
	prov := buildLaneProvenance(r.opts.Host, r.opts.Lane)
	caseIDs := sortedCopy(r.opts.CaseIDs)
	setDigest, err := CaseIDsDigest(caseIDs)
	if err != nil {
		return nil, err
	}
	orderDigest, err := CanonicalSHA256(r.order)
	if err != nil {
		return nil, err
	}
	isolationDigests := make([]string, 0, len(r.receipts))
	statuses := map[string]string{}
	for _, rec := range r.receipts {
		isolationDigests = append(isolationDigests, rec.CaseStateIsolationReceiptDigest)
		statuses[rec.CaseID] = rec.Status
	}
	sort.Strings(isolationDigests)
	m := &PrimaryRunManifest{
		Mode: "primary", SeriesID: r.opts.SeriesID, Host: r.opts.Host, Split: r.opts.Split,
		Ordinal: r.opts.Ordinal, ToolProvenance: prov,
		CaseIDs: caseIDs, CaseSetDigest: setDigest,
		CaseOrder: append([]string(nil), r.order...), CaseOrderDigest: orderDigest,
		ExpectedCaseCount: len(caseIDs),
		StartedAt:         startedAt, CompletedAt: nowRFC3339(), State: StateComplete,
	}
	runDigest, err := CanonicalSHA256(struct {
		Mode               string   `json:"mode"`
		SeriesID           string   `json:"series_id"`
		Host               string   `json:"host"`
		Split              string   `json:"split"`
		Ordinal            int      `json:"ordinal"`
		ToolIdentityDigest string   `json:"tool_identity_digest"`
		CaseSetDigest      string   `json:"case_set_digest"`
		CaseOrderDigest    string   `json:"case_order_digest"`
		ExpectedCaseCount  int      `json:"expected_case_count"`
		StartedAt          string   `json:"started_at"`
		CaseReceiptDigests []string `json:"case_receipt_digests"`
	}{Mode: m.Mode, SeriesID: m.SeriesID, Host: m.Host, Split: m.Split, Ordinal: m.Ordinal,
		ToolIdentityDigest: prov.ToolIdentityDigest, CaseSetDigest: m.CaseSetDigest,
		CaseOrderDigest: m.CaseOrderDigest, ExpectedCaseCount: m.ExpectedCaseCount,
		StartedAt: m.StartedAt, CaseReceiptDigests: isolationDigests})
	if err != nil {
		return nil, err
	}
	m.RunDigest = runDigest
	sealDigest, err := CanonicalSHA256(struct {
		RunDigest                       string            `json:"run_digest"`
		ProtectedExecutionReceiptDigest string            `json:"protected_execution_receipt_digest"`
		IsolationReceiptDigests         []string          `json:"case_state_isolation_receipt_digests"`
		CaseStatuses                    map[string]string `json:"case_statuses"`
	}{RunDigest: m.RunDigest, ProtectedExecutionReceiptDigest: r.protectedReceiptDigest(),
		IsolationReceiptDigests: isolationDigests, CaseStatuses: statuses})
	if err != nil {
		return nil, err
	}
	m.SealDigest = sealDigest
	if err := writeFrozenJSON(filepath.Join(r.layout.run, "manifest.json"), m); err != nil {
		return nil, err
	}
	return m, nil
}

// ---------- case artifacts ----------

func fileDigest(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256Hex(b)
}

func writeFrozenJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrozenFile(path, b)
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

type stateRootLedgerEntry struct {
	SeriesID        string `json:"series_id"`
	Host            string `json:"host"`
	Split           string `json:"split"`
	Ordinal         int    `json:"ordinal"`
	CaseID          string `json:"case_id"`
	StateRootDigest string `json:"state_root_digest"`
}

// The series-level state-root ledger makes fresh-state-root uniqueness a
// cross-run property: a later leg (other host, other ordinal, other split)
// cannot silently reuse a state root this series already retired.
func readStateRootLedger(path string) ([]stateRootLedgerEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries := []stateRootLedgerEntry{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e stateRootLedgerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("state root ledger %s: %w", path, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func writeStateRootLedger(path string, entries []stateRootLedgerEntry) error {
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return osWriteFile(path, []byte(buf.String()))
}

func loadStateIsolation(path string) (CaseStateIsolationReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return CaseStateIsolationReceipt{}, err
	}
	var iso CaseStateIsolationReceipt
	if err := json.Unmarshal(b, &iso); err != nil {
		return CaseStateIsolationReceipt{}, err
	}
	return iso, nil
}

// ---------- production seams ----------

// defaultSeedRunner seeds the case store through the public engram CLI and
// records the frozen event-date render receipt for every seed (T049).
func defaultSeedRunner(binDir string) func(caseDir string, c *TriggerCaseV2) ([]SeedRenderResult, error) {
	return func(caseDir string, c *TriggerCaseV2) ([]SeedRenderResult, error) {
		engramBin := filepath.Join(binDir, "engram")
		receipts := []SeedRenderResult{}
		for _, s := range c.SeedMemories {
			content, rr, err := RenderSeedContent(s)
			if err != nil {
				return nil, err
			}
			cmd := newSeedCommand(engramBin, caseDir, s.Name, content)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("seed %s failed: %v: %s", s.Name, err, truncate(string(out), 200))
			}
			receipts = append(receipts, rr)
		}
		return receipts, nil
	}
}

func defaultStoreDumper(binDir string) func(caseDir string) (string, error) {
	return func(caseDir string) (string, error) {
		out, err := newStoreDumpCommand(filepath.Join(binDir, "engram"), caseDir).Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

// envPrimaryChild adapts the lane configuration to the proven diagnostic
// child wiring (per-case MCP config for claude, per-case engram-mcp override
// for codex, whitelist env for opencode) and pins the child's cwd to its own
// materialized workspace. The frozen host template is still resolved first,
// so a lane configuration that cannot express the host fails before any
// child is spawned.
//
// T051 stub point: the frozen TemplateForHost argv and this proven wiring
// differ in a few isolation flags; reconciling them belongs to the CLI
// wiring task. Both sides already share the cwd/data-dir materialization
// contract, and the canary below is what catches a real divergence.
func envPrimaryChild(opts PrimaryRunOptions) caseChildRunner {
	return func(tool, caseDir, prompt string, c *TriggerCaseV2) ([]byte, error) {
		if _, err := TemplateForHost(tool, opts.Lane.ClaudeSettings, opts.Lane.CodexProvider, opts.Lane.CodexModel, opts.Lane.OpenCodeModel); err != nil {
			return nil, err
		}
		os.Setenv("ENGRAM_SKILL_EVAL_BIN_DIR", opts.BinDir)
		switch tool {
		case HostClaude:
			if opts.Lane.ClaudeSettings != "" {
				os.Setenv("ENGRAM_SKILL_EVAL_CLAUDE_SETTINGS", opts.Lane.ClaudeSettings)
			}
		case HostCodex:
			os.Setenv("ENGRAM_SKILL_EVAL_CODEX_PROVIDER", opts.Lane.CodexProvider)
			os.Setenv("ENGRAM_SKILL_EVAL_CODEX_MODEL", opts.Lane.CodexModel)
		case HostOpenCode:
			os.Setenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL", opts.Lane.OpenCodeModel)
		}
		return runV2Child(tool, caseDir, prompt, c)
	}
}

// ---------- T049: protected access-probe matrix ----------

// ProbeFunc is one access attempt against a target: denied | not-found |
// readable (the stage-probe vocabulary; ProbeFilesystem is the real one).
type ProbeFunc func(target string) string

// ProtectedProbeOptions configures the series-prepare probe matrix run.
type ProtectedProbeOptions struct {
	Plan                  *CoreExecutionPlanReceipt
	Lane                  CLIReviewConfig
	Boundary              BoundaryKind
	IsolationConfigDigest string
	Root                  string // unique probe root; must not exist
	ProtectedRoot         string // created under Root, mode 0000, with an inner sentinel
	AuditRoot             string // author/review audit root (mode 0000)
	AuthorStateRoot       string // author/review state root (mode 0000)
	FormalStateRoots      []string
	SplitAllocatorRoots   map[string]string
	WorkerRoot            string // prepared per-slot worker workspaces
	Capacity              int    // isolated worker capacity, >= sealed concurrency
	Probe                 ProbeFunc

	templateDigests map[string]string // filled by RunProtectedProbes (host → template digest)
}

// RunProtectedProbes executes the closed protected access-probe matrix for
// every host × worker slot on the real filesystem and returns the sealed
// ProtectedExecutionReceipt. Slots run on the shared bounded pool, so with
// concurrency > 1 the probes execute while other workers are in flight.
//
// The operator-provided boundary (separate user, container, mount namespace,
// ACL) is what makes the denials real for the actual child; the in-process
// tree materializes the same physical policy as mode-0000 roots — the
// package-wide convention (holdout_stage.go) — and binds every probe to the
// real parent policy digest, so a target chmod'ed only for the probe cannot
// pass.
func RunProtectedProbes(opts ProtectedProbeOptions) (*ProtectedExecutionReceipt, error) {
	if opts.Plan == nil {
		return nil, errors.New("protected probes require the core execution plan")
	}
	if err := ValidateCoreExecutionPlan(opts.Plan); err != nil {
		return nil, err
	}
	if !ValidBoundary(opts.Boundary) {
		return nil, fmt.Errorf("boundary kind %q invalid", opts.Boundary)
	}
	if opts.IsolationConfigDigest == "" {
		return nil, errors.New("protected probes require the isolation config digest")
	}
	if opts.Capacity < opts.Plan.Concurrency {
		return nil, fmt.Errorf("isolated worker capacity %d < sealed concurrency %d", opts.Capacity, opts.Plan.Concurrency)
	}
	if err := ValidateSplitAllocators(opts.SplitAllocatorRoots, SplitDevRegression); err != nil {
		return nil, err
	}
	if len(opts.FormalStateRoots) == 0 {
		return nil, errors.New("protected probes require fresh formal state roots")
	}
	if _, err := os.Stat(opts.Root); err == nil {
		return nil, fmt.Errorf("probe root %s already exists — protected probes require unique roots", opts.Root)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	probe := opts.Probe
	if probe == nil {
		probe = ProbeFilesystem
	}
	// Invocation parity (T049): all three host templates must resolve, and
	// their digests are what every worker probe binds.
	_, templateDigests, err := PrimaryHostTemplates(opts.Lane)
	if err != nil {
		return nil, err
	}
	opts.templateDigests = templateDigests
	normalizedTemplate, err := NormalizedCoreTemplateDigest(templateDigests)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.WorkerRoot, 0o755); err != nil {
		return nil, err
	}

	// Own workspaces stay readable; every other worker's workspace is
	// materialized as the foreign side of the boundary (mode 0000).
	ownDirs := map[string]string{}
	siblingDirs := map[string]string{}
	for _, h := range opts.Plan.Hosts {
		for slot := 1; slot <= opts.Plan.Concurrency; slot++ {
			own := filepath.Join(opts.WorkerRoot, "own", fmt.Sprintf("%s-%d", h, slot))
			if err := os.MkdirAll(own, 0o755); err != nil {
				return nil, err
			}
			if err := osWriteFile(filepath.Join(own, "own-workspace.txt"), []byte(fmt.Sprintf("own workspace of %s slot %d\n", h, slot))); err != nil {
				return nil, err
			}
			ownDirs[workerKey(h, slot)] = own
			sib := filepath.Join(opts.WorkerRoot, "sibling", fmt.Sprintf("%s-%d", h, slot))
			if err := os.MkdirAll(filepath.Join(sib, "state"), 0o700); err != nil {
				return nil, err
			}
			if err := osWriteFile(filepath.Join(sib, "state", "sibling-state.txt"), []byte("another worker's state\n")); err != nil {
				return nil, err
			}
			// Locked only after the controller proofs are captured (below):
			// a proof taken after the boundary closed would be an invention.
			siblingDirs[workerKey(h, slot)] = sib
		}
	}
	// Protected and author/review roots: sentinels first, then mode 0000.
	protectedSentinel := filepath.Join(opts.ProtectedRoot, "holdout-sentinel.txt")
	auditSentinel := filepath.Join(opts.AuditRoot, "audit-sentinel.txt")
	stateSentinel := filepath.Join(opts.AuthorStateRoot, "author-state-sentinel.txt")
	for _, d := range []string{opts.ProtectedRoot, opts.AuditRoot, opts.AuthorStateRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := osWriteFile(protectedSentinel, []byte("private holdout payload\n")); err != nil {
		return nil, err
	}
	if err := osWriteFile(auditSentinel, []byte("generation audit\n")); err != nil {
		return nil, err
	}
	if err := osWriteFile(stateSentinel, []byte("author/review state\n")); err != nil {
		return nil, err
	}
	// Retired anchors: real, existing, unreadable prior-state and
	// retired-workspace targets.
	retiredState := filepath.Join(opts.Root, "retired", "state", "probe-anchor")
	retiredWorkspace := filepath.Join(opts.Root, "retired", "workspace", "probe-anchor")
	for _, d := range []string{retiredState, retiredWorkspace} {
		if err := os.MkdirAll(filepath.Join(d, "state"), 0o700); err != nil {
			return nil, err
		}
		if err := osWriteFile(filepath.Join(d, "state", "retired.txt"), []byte("retired case state\n")); err != nil {
			return nil, err
		}
	}
	// Controller target proofs are captured NOW — while the controller can
	// still see every target. After the boundary locks, a proof would be an
	// invention, and the probe would have nothing honest to cite.
	proofs := map[string]string{}
	policies := map[string]string{}
	capture := func(target string) error {
		p, err := captureProof(target)
		if err != nil {
			return fmt.Errorf("controller proof for %s: %w", target, err)
		}
		proofs[target] = p
		policies[target] = accessPolicyDigest(target)
		return nil
	}
	for _, target := range []string{
		protectedSentinel, opts.ProtectedRoot, auditSentinel, stateSentinel,
		filepath.Join(retiredState, "state", "retired.txt"),
		filepath.Join(retiredWorkspace, "state", "retired.txt"),
	} {
		if err := capture(target); err != nil {
			return nil, err
		}
	}
	for _, d := range siblingDirs {
		if err := capture(filepath.Join(d, "state", "sibling-state.txt")); err != nil {
			return nil, err
		}
	}
	// The own workspace needs no proof (it is expected readable) but its
	// policy digest is part of the closed probe record.
	for _, d := range ownDirs {
		if err := capture(filepath.Join(d, "own-workspace.txt")); err != nil {
			return nil, err
		}
	}
	for _, d := range []string{opts.ProtectedRoot, opts.AuditRoot, opts.AuthorStateRoot} {
		if err := os.Chmod(d, 0o000); err != nil {
			return nil, err
		}
	}
	for _, d := range []string{retiredState, retiredWorkspace} {
		if err := os.Chmod(d, 0o000); err != nil {
			return nil, err
		}
	}
	for _, d := range siblingDirs {
		if err := os.Chmod(d, 0o000); err != nil {
			return nil, err
		}
	}

	concurrency := opts.Plan.Concurrency
	formalDigest := ""
	for _, r := range opts.FormalStateRoots {
		formalDigest += r + "\x00"
	}
	receipt := &ProtectedExecutionReceipt{
		BoundaryKind:           opts.Boundary,
		IsolationConfigDigest:  opts.IsolationConfigDigest,
		ProtectedRootDigest:    pathDigest(opts.ProtectedRoot),
		RequiredConcurrency:    concurrency,
		IsolatedWorkerCapacity: opts.Capacity,
		SplitStateAllocatorDigests: map[string]string{
			MembershipCore172:   pathDigest(opts.SplitAllocatorRoots[MembershipCore172]),
			MembershipHoldout96: pathDigest(opts.SplitAllocatorRoots[MembershipHoldout96]),
		},
		AuthorReviewStateRootsDigest: pathDigest(opts.AuditRoot + "\x00" + opts.AuthorStateRoot),
		FormalStateRootsDigest:       pathDigest(formalDigest),
		CoreExecutionPlanDigest:      opts.Plan.ReceiptDigest,
		ProbedAt:                     nowRFC3339(),
	}

	var (
		probeMu      sync.Mutex
		workerProbes []ProtectedWorkerProbe
		identities   []string
	)
	items := concurrency * len(opts.Plan.Hosts)
	_, _, poolErr := runBounded(concurrency, items, func(i, _ int) error {
		host := opts.Plan.Hosts[i%len(opts.Plan.Hosts)]
		slotIdx := i/len(opts.Plan.Hosts) + 1
		templateDigest := normalizedTemplate

		probes := []FormalAccessProbe{}
		appendProbe := func(kind FormalProbeKind, target, expected string) error {
			p, err := runFormalProbeProven(probe, kind, target, expected,
				func(t string) (string, error) {
					d, ok := proofs[t]
					if !ok {
						return "", fmt.Errorf("no controller proof was captured for %s", t)
					}
					return d, nil
				},
				func(t string) string { return policies[t] })
			if err != nil {
				return err
			}
			probes = append(probes, p)
			return nil
		}
		// Protected root: traverse (the sentinel inside it), list, read.
		if err := appendProbe(FProbeProtectedRootTraverse, protectedSentinel, probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeProtectedRootList, opts.ProtectedRoot, probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeProtectedRootRead, protectedSentinel, probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeAuditRead, auditSentinel, probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeAuthorStateRead, stateSentinel, probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeOwnWorkspaceRead, filepath.Join(ownDirs[workerKey(host, slotIdx)], "own-workspace.txt"), probeReadable); err != nil {
			return err
		}
		// Active sibling: pairwise whenever concurrency > 1, and the bounded
		// pool runs the slots concurrently while they probe each other.
		if concurrency > 1 {
			nextSlot := slotIdx%concurrency + 1
			nextHost := opts.Plan.Hosts[(i+1)%len(opts.Plan.Hosts)]
			if err := appendProbe(FProbeActiveSiblingRead,
				filepath.Join(siblingDirs[workerKey(nextHost, nextSlot)], "state", "sibling-state.txt"), probeDenied); err != nil {
				return err
			}
		}
		if err := appendProbe(FProbePriorCaseStateRead, filepath.Join(retiredState, "state", "retired.txt"), probeDenied); err != nil {
			return err
		}
		if err := appendProbe(FProbeRetiredWorkspaceRead, filepath.Join(retiredWorkspace, "state", "retired.txt"), probeDenied); err != nil {
			return err
		}

		wp := ProtectedWorkerProbe{
			Host:                    host,
			WorkerSlot:              slotIdx,
			ChildIdentityDigest:     primaryChildIdentity(host, slotIdx, templateDigest),
			ExecutionTemplateDigest: templateDigest,
			AccessBoundaryDigest:    primaryAccessBoundary(opts.Boundary, opts.IsolationConfigDigest, host, slotIdx),
			Probes:                  probes,
		}
		probeMu.Lock()
		workerProbes = append(workerProbes, wp)
		identities = append(identities, wp.Host+"\x00"+fmt.Sprint(wp.WorkerSlot)+"\x00"+wp.ChildIdentityDigest)
		probeMu.Unlock()
		return nil
	})
	if poolErr != nil {
		return nil, poolErr
	}
	receipt.WorkerProbes = workerProbes
	sort.Slice(receipt.WorkerProbes, func(i, j int) bool {
		if receipt.WorkerProbes[i].Host != receipt.WorkerProbes[j].Host {
			return receipt.WorkerProbes[i].Host < receipt.WorkerProbes[j].Host
		}
		return receipt.WorkerProbes[i].WorkerSlot < receipt.WorkerProbes[j].WorkerSlot
	})
	sort.Strings(identities)
	setDigest, err := CanonicalSHA256(identities)
	if err != nil {
		return nil, err
	}
	receipt.WorkerIdentitySetDigest = setDigest
	receipt.NormalizedCoreWorkerIdentitySetDigest = opts.Plan.NormalizedCoreWorkerIdentitySetDigest
	receipt.ExecutionTemplateSetDigest = normalizedTemplate
	if receipt.ProbeMatrixDigest, err = CanonicalSHA256(receipt.WorkerProbes); err != nil {
		return nil, err
	}
	digest, err := CanonicalSHA256(protectedProjection(receipt))
	if err != nil {
		return nil, err
	}
	receipt.ReceiptDigest = digest
	if err := ValidateProtectedExecutionReceipt(receipt, opts.Plan); err != nil {
		return nil, fmt.Errorf("probe matrix failed its own validation: %w", err)
	}
	return receipt, nil

}

func workerKey(host string, slot int) string { return host + "\x00" + fmt.Sprint(slot) }

type protectedReceiptProjection struct {
	BoundaryKind                          BoundaryKind           `json:"boundary_kind"`
	IsolationConfigDigest                 string                 `json:"isolation_config_digest"`
	ProtectedRootDigest                   string                 `json:"protected_root_digest"`
	AuthorReviewStateRootsDigest          string                 `json:"author_review_state_roots_digest"`
	FormalStateRootsDigest                string                 `json:"formal_state_roots_digest"`
	SplitStateAllocatorDigests            map[string]string      `json:"split_state_allocator_digests"`
	RequiredConcurrency                   int                    `json:"required_concurrency"`
	IsolatedWorkerCapacity                int                    `json:"isolated_worker_capacity"`
	WorkerIdentitySetDigest               string                 `json:"worker_identity_set_digest"`
	NormalizedCoreWorkerIdentitySetDigest string                 `json:"normalized_core_worker_identity_set_digest"`
	ExecutionTemplateSetDigest            string                 `json:"execution_template_set_digest"`
	CoreExecutionPlanDigest               string                 `json:"core_execution_plan_digest"`
	WorkerProbes                          []ProtectedWorkerProbe `json:"worker_probes"`
	ProbeMatrixDigest                     string                 `json:"probe_matrix_digest"`
	ProbedAt                              string                 `json:"probed_at"`
}

func protectedProjection(r *ProtectedExecutionReceipt) protectedReceiptProjection {
	return protectedReceiptProjection{
		BoundaryKind: r.BoundaryKind, IsolationConfigDigest: r.IsolationConfigDigest,
		ProtectedRootDigest: r.ProtectedRootDigest, AuthorReviewStateRootsDigest: r.AuthorReviewStateRootsDigest,
		FormalStateRootsDigest: r.FormalStateRootsDigest, SplitStateAllocatorDigests: r.SplitStateAllocatorDigests,
		RequiredConcurrency: r.RequiredConcurrency, IsolatedWorkerCapacity: r.IsolatedWorkerCapacity,
		WorkerIdentitySetDigest:               r.WorkerIdentitySetDigest,
		NormalizedCoreWorkerIdentitySetDigest: r.NormalizedCoreWorkerIdentitySetDigest,
		ExecutionTemplateSetDigest:            r.ExecutionTemplateSetDigest,
		CoreExecutionPlanDigest:               r.CoreExecutionPlanDigest,
		WorkerProbes:                          r.WorkerProbes, ProbeMatrixDigest: r.ProbeMatrixDigest,
		ProbedAt: r.ProbedAt,
	}
}

// ---------- T049: workspace canaries ----------

// CanaryChildRunner executes one canary through one prepared host × worker
// slot and reports the cwd the child observed plus the exact bytes it read
// back from the staged canary file.
type CanaryChildRunner func(host string, slot int, canaryDir, stagedRel string) (cwd string, fileBytes []byte, err error)

// CanaryOptions configures the automatic series-prepare canary run.
type CanaryOptions struct {
	SeriesID            string
	SkillDigest         string
	Plan                *CoreExecutionPlanReceipt
	Protected           *ProtectedExecutionReceipt // nil only for a dev-comparison series
	ToolIdentityDigests map[string]string          // host → digest
	Lane                CLIReviewConfig
	Root                string // unique canary root; must not exist
	Child               CanaryChildRunner
	// Boundary/IsolationConfigDigest back the prepared slot identity of a
	// dev-comparison canary, whose series carries no protected receipt; the
	// same pure functions the protected receipt's probes use.
	Boundary              BoundaryKind
	IsolationConfigDigest string
}

// RunWorkspaceCanaries stages one fictional, nonsecret canary workspace per
// prepared host × worker slot, runs the final invocation boundary against it
// and returns one receipt per slot. Any observation mismatch fails closed:
// the failed receipt is still returned, but the series cannot seal.
func RunWorkspaceCanaries(opts CanaryOptions) ([]WorkspaceCanaryReceipt, error) {
	if opts.Plan == nil {
		return nil, errors.New("canaries require the core execution plan")
	}
	if err := ValidateCoreExecutionPlan(opts.Plan); err != nil {
		return nil, err
	}
	if opts.Protected != nil {
		if err := ValidateProtectedExecutionReceipt(opts.Protected, opts.Plan); err != nil {
			return nil, err
		}
	} else if opts.Boundary == "" || opts.IsolationConfigDigest == "" {
		return nil, errors.New("a canary run without a protected execution receipt (dev-comparison) requires the boundary kind and isolation config digest")
	}
	if opts.Child == nil {
		return nil, errors.New("canaries require a child runner (T051 wires the real host invocation)")
	}
	if _, err := os.Stat(opts.Root); err == nil {
		return nil, fmt.Errorf("canary root %s already exists — canaries require unique roots", opts.Root)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	_, templateDigests, err := PrimaryHostTemplates(opts.Lane)
	if err != nil {
		return nil, err
	}
	normalizedTemplate, err := NormalizedCoreTemplateDigest(templateDigests)
	if err != nil {
		return nil, err
	}
	out := []WorkspaceCanaryReceipt{}
	for _, host := range opts.Plan.Hosts {
		for slot := 1; slot <= opts.Plan.Concurrency; slot++ {
			var childIdentity, boundaryDigest string
			if opts.Protected != nil {
				prepared, err := preparedWorkerProbe(opts.Protected, host, slot)
				if err != nil {
					return nil, err
				}
				childIdentity = prepared.ChildIdentityDigest
				boundaryDigest = prepared.AccessBoundaryDigest
			} else {
				// dev-comparison: the prepared slot identity is the same pure
				// function of (host, slot, template, boundary) the protected
				// receipt freezes — no receipt, no invented values.
				childIdentity = primaryChildIdentity(host, slot, normalizedTemplate)
				boundaryDigest = primaryAccessBoundary(opts.Boundary, opts.IsolationConfigDigest, host, slot)
			}
			canaryDir := filepath.Join(opts.Root, fmt.Sprintf("%s-slot%d", host, slot))
			stagedRel := filepath.Join("canary", "staged-file.txt")
			if err := os.MkdirAll(filepath.Join(canaryDir, "canary"), 0o755); err != nil {
				return nil, err
			}
			content := fmt.Sprintf("engram workspace canary\nseries=%s\nhost=%s\nslot=%d\nnonce=%s\n",
				opts.SeriesID, host, slot, sha256Hex([]byte(opts.SeriesID + host + fmt.Sprint(slot)))[:16])
			if err := osWriteFile(filepath.Join(canaryDir, stagedRel), []byte(content)); err != nil {
				return nil, err
			}
			if abs, err := filepath.Abs(canaryDir); err == nil {
				canaryDir = abs
			}
			cwd, fileBytes, cerr := opts.Child(host, slot, canaryDir, stagedRel)
			receipt := WorkspaceCanaryReceipt{
				SeriesID:                opts.SeriesID,
				Host:                    host,
				SkillDigest:             opts.SkillDigest,
				ToolIdentityDigest:      opts.ToolIdentityDigests[host],
				ExecutionTemplateDigest: normalizedTemplate,
				WorkerSlot:              slot,
				ChildIdentityDigest:     childIdentity,
				AccessBoundaryDigest:    boundaryDigest,
				CanaryWorkspaceDigest:   pathDigest(canaryDir),
				ExpectedFileDigest:      sha256Hex([]byte(content)),
			}
			if cerr == nil {
				receipt.ObservedCWDDigest = pathDigest(cwd)
				receipt.ObservedFileDigest = sha256Hex(fileBytes)
				if receipt.ObservedCWDDigest == receipt.CanaryWorkspaceDigest && receipt.ObservedFileDigest == receipt.ExpectedFileDigest {
					receipt.Status = "pass"
				} else {
					receipt.Status = "fail"
				}
			} else {
				receipt.Status = "fail"
			}
			digest, derr := CanonicalSHA256(canaryProjection(&receipt))
			if derr != nil {
				return nil, derr
			}
			receipt.ReceiptDigest = digest
			out = append(out, receipt)
			if err := ValidateWorkspaceCanary(&receipt, opts.SeriesID, opts.SkillDigest,
				opts.ToolIdentityDigests[host], normalizedTemplate, slot); err != nil {
				return out, fmt.Errorf("workspace canary for %s slot %d: %w", host, slot, err)
			}
		}
	}
	return out, nil
}

type canaryReceiptProjection struct {
	SeriesID                string `json:"series_id"`
	Host                    string `json:"host"`
	SkillDigest             string `json:"skill_digest"`
	ToolIdentityDigest      string `json:"tool_identity_digest"`
	ExecutionTemplateDigest string `json:"execution_template_digest"`
	WorkerSlot              int    `json:"worker_slot"`
	ChildIdentityDigest     string `json:"child_identity_digest"`
	AccessBoundaryDigest    string `json:"access_boundary_digest"`
	CanaryWorkspaceDigest   string `json:"canary_workspace_digest"`
	ExpectedFileDigest      string `json:"expected_file_digest"`
	ObservedCWDDigest       string `json:"observed_cwd_digest"`
	ObservedFileDigest      string `json:"observed_file_digest"`
	Status                  string `json:"status"`
}

func canaryProjection(c *WorkspaceCanaryReceipt) canaryReceiptProjection {
	return canaryReceiptProjection{
		SeriesID: c.SeriesID, Host: c.Host, SkillDigest: c.SkillDigest,
		ToolIdentityDigest: c.ToolIdentityDigest, ExecutionTemplateDigest: c.ExecutionTemplateDigest,
		WorkerSlot: c.WorkerSlot, ChildIdentityDigest: c.ChildIdentityDigest,
		AccessBoundaryDigest: c.AccessBoundaryDigest, CanaryWorkspaceDigest: c.CanaryWorkspaceDigest,
		ExpectedFileDigest: c.ExpectedFileDigest, ObservedCWDDigest: c.ObservedCWDDigest,
		ObservedFileDigest: c.ObservedFileDigest, Status: c.Status,
	}
}

// HostCanaryChild is the production canary child: it drives the frozen host
// invocation inside the canary workspace and parses the frozen canary
// protocol markers (the child prints its cwd and the digest of the staged
// file it read back).
//
// Stub point for T051: the opencode lane pins its own config workspace as
// cwd (cli_review.go), which cannot satisfy the canary cwd contract — that
// divergence is exactly what this canary exists to catch, and the wiring
// task must give the opencode canary a per-canary workspace.
func HostCanaryChild(lane CLIReviewConfig) CanaryChildRunner {
	run := func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		prompt := fmt.Sprintf(
			"Canary check. Print exactly two lines and nothing else:\n"+
				"ENGRAM-CANARY-CWD=<your absolute working directory>\n"+
				"ENGRAM-CANARY-SHA256=<the lowercase sha256 of the file %s>\n", stagedRel)
		raw, err := runLaneCLIIn(host, lane, prompt, canaryDir)
		if err != nil {
			return "", nil, err
		}
		cwd, digest, perr := parseCanaryMarkers(string(raw))
		if perr != nil {
			return "", nil, perr
		}
		b, err := os.ReadFile(filepath.Join(canaryDir, stagedRel))
		if err != nil {
			return "", nil, err
		}
		if sha256Hex(b) != digest {
			return "", nil, fmt.Errorf("canary child reported digest %s, staged file is %s", digest, sha256Hex(b))
		}
		return cwd, b, nil
	}
	return func(host string, slot int, canaryDir, stagedRel string) (string, []byte, error) {
		cwd, b, err := run(host, slot, canaryDir, stagedRel)
		if err != nil {
			// One fresh invocation retry: a canary is infrastructure
			// probing, not scoring, and a transient model/CLI hiccup under
			// full concurrency must not invalidate a series on its own.
			cwd, b, err = run(host, slot, canaryDir, stagedRel)
		}
		return cwd, b, err
	}
}

// parseCanaryMarkers extracts the two frozen markers from a canary child's
// output. The frozen claude/codex templates run with structured output, so
// the markers may live inside a JSON event's text/result field rather than
// on a bare line; both shapes are scanned, and a bare non-JSON line still
// matches directly.
func parseCanaryMarkers(s string) (cwd, digest string, err error) {
	scan := func(line string) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ENGRAM-CANARY-CWD="):
			cwd = strings.Trim(strings.TrimPrefix(line, "ENGRAM-CANARY-CWD="), `"'`)
		case strings.HasPrefix(line, "ENGRAM-CANARY-SHA256="):
			digest = strings.TrimSpace(strings.TrimPrefix(line, "ENGRAM-CANARY-SHA256="))
		}
	}
	for _, line := range strings.Split(s, "\n") {
		scan(line)
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		var ev struct {
			Type   string `json:"type"`
			Result string `json:"result"`
			Message *struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			// Codex exec --json shape: the final agent message rides an
			// item.completed event's item.text.
			Item *struct {
				Text string `json:"text"`
			} `json:"item"`
			// opencode2 --format json shape: text parts ride part.text.
			Part *struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(trimmed), &ev) != nil {
			continue
		}
		for _, l := range strings.Split(ev.Result, "\n") {
			scan(l)
		}
		if ev.Item != nil {
			for _, l := range strings.Split(ev.Item.Text, "\n") {
				scan(l)
			}
		}
		if ev.Part != nil {
			for _, l := range strings.Split(ev.Part.Text, "\n") {
				scan(l)
			}
		}
		if ev.Message != nil {
			for _, c := range ev.Message.Content {
				for _, l := range strings.Split(c.Text, "\n") {
					scan(l)
				}
			}
		}
	}
	if cwd == "" || digest == "" {
		return "", "", fmt.Errorf("canary output missing the frozen markers (cwd=%q digest=%q)", cwd, digest)
	}
	return cwd, digest, nil
}
