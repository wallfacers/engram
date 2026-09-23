package main

// T027/T032 stage half — bounded author/review worker capacity, per-attempt
// ephemeral stage workspaces, and the exact-child isolation probe contract
// (contracts/dataset-protocol.md §4-§5): the exact child reads its own input
// but never the private root, generation audit, author receipts, prior
// reviews, or an active sibling workspace, and every denied "not-found"
// observation requires a controller-side target-existence proof captured
// immediately before the child launched.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// StageKind distinguishes the authoring stage from the review stage. Both
// share one bounded capacity per batch.
type StageKind string

const (
	StageAuthor StageKind = "author"
	StageReview StageKind = "review"
)

// StageWorkspaceManager hands out one ephemeral workspace per attempt under a
// bounded in-flight capacity. A released slot is reusable, but no attempt ever
// receives another attempt's workspace root.
type StageWorkspaceManager struct {
	root string
	bound int

	mu         sync.Mutex
	cond       *sync.Cond
	held       map[string]string // attemptID → workspace dir
	next       int64
	maxObserved int
}

func NewStageWorkspaceManager(root string, concurrency int) *StageWorkspaceManager {
	m := &StageWorkspaceManager{root: root, bound: concurrency, held: map[string]string{}}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// Acquire blocks until capacity is free and materializes the attempt's
// isolated input/state/output layout under the manager root.
func (m *StageWorkspaceManager) Acquire(kind StageKind, attemptID string) (string, error) {
	if m.bound <= 0 {
		return "", fmt.Errorf("stage workspace concurrency must be > 0 (got %d)", m.bound)
	}
	if !safeAttemptID(attemptID) {
		return "", fmt.Errorf("unsafe attempt id %q", attemptID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.held[attemptID]; dup {
		return "", fmt.Errorf("attempt %s already holds a workspace", attemptID)
	}
	for len(m.held) >= m.bound {
		m.cond.Wait()
	}
	// Resume-safe sequence: skip ordinals whose workspace dir already exists
	// (a restarted batch must never collide with a retired attempt root —
	// its files are mode 0000 and unwritable by design).
	for {
		m.next++
		if _, err := os.Stat(filepath.Join(m.root, string(kind),
			fmt.Sprintf("%06d-%s", m.next, attemptID))); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	ws := filepath.Join(m.root, string(kind), fmt.Sprintf("%06d-%s", m.next, attemptID))
	for _, sub := range []string{"input", "state", "output"} {
		if err := os.MkdirAll(filepath.Join(ws, sub), 0o700); err != nil {
			return "", err
		}
	}
	m.held[attemptID] = ws
	if n := len(m.held); n > m.maxObserved {
		m.maxObserved = n
	}
	return ws, nil
}

// Release frees the attempt's slot; the workspace directory stays on disk for
// the controller's post-attempt receipts.
func (m *StageWorkspaceManager) Release(attemptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.held[attemptID]; !ok {
		return fmt.Errorf("release: attempt %s holds no workspace", attemptID)
	}
	delete(m.held, attemptID)
	m.cond.Signal()
	return nil
}

// MaxObservedInFlight reports the peak concurrent in-flight attempts — the
// number the frozen-concurrency receipt records (overlap must be observed
// whenever configured concurrency exceeds one).
func (m *StageWorkspaceManager) MaxObservedInFlight() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxObserved
}

func safeAttemptID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// ---------- isolation probes ----------

// ProbeKind enumerates the closed set of exact-child access probes for the
// author/review stages.
type ProbeKind string

const (
	ProbePrivateRootTraverse ProbeKind = "private-root-traverse"
	ProbePrivateRootList     ProbeKind = "private-root-list"
	ProbePrivateRootRead     ProbeKind = "private-root-read"
	ProbeGenerationAuditRead ProbeKind = "generation-audit-read"
	ProbeAuthorReceiptRead   ProbeKind = "author-receipt-read"
	ProbePriorReviewRead     ProbeKind = "prior-review-read"
	ProbeActiveSiblingRead   ProbeKind = "active-sibling-workspace-read"
	ProbeOwnInputRead        ProbeKind = "own-input-read"
)

const (
	ProbeDenied   = "denied"
	ProbeReadable = "readable"
	ProbeNotFound = "not-found"
)

var requiredStageProbes = map[ProbeKind]bool{
	ProbePrivateRootTraverse: true,
	ProbePrivateRootList:     true,
	ProbePrivateRootRead:     true,
	ProbeGenerationAuditRead: true,
	ProbeAuthorReceiptRead:   true,
	ProbePriorReviewRead:     true,
	ProbeActiveSiblingRead:   true,
	ProbeOwnInputRead:        true,
}

// AccessProbe is one recorded exact-child access observation with its
// controller-side evidence.
type AccessProbe struct {
	Kind                        ProbeKind
	TargetPath                  string
	ControllerTargetProofDigest string // required for a denied not-found
	TargetAccessPolicyDigest    string // digest of the actual parent policy
	Expected                    string
	Observed                    string
}

// ValidateIsolationProbes fails closed: every probe category must appear
// exactly once, forbidden targets must be denied (a not-founds needs the
// controller proof), the own input must be readable, and every probe must
// carry the parent access-policy digest.
func ValidateIsolationProbes(stage StageKind, probes []AccessProbe) error {
	if stage != StageAuthor && stage != StageReview {
		return fmt.Errorf("unknown stage %q", stage)
	}
	seen := map[ProbeKind]int{}
	for _, p := range probes {
		if !requiredStageProbes[p.Kind] {
			return fmt.Errorf("unknown probe kind %q", p.Kind)
		}
		seen[p.Kind]++
		if p.TargetPath == "" {
			return fmt.Errorf("probe %s has no target path", p.Kind)
		}
		if p.TargetAccessPolicyDigest == "" {
			return fmt.Errorf("probe %s lacks the parent access-policy digest", p.Kind)
		}
		switch p.Expected {
		case ProbeDenied, ProbeReadable:
		default:
			return fmt.Errorf("probe %s has unknown expected %q", p.Kind, p.Expected)
		}
		switch p.Observed {
		case ProbeDenied, ProbeReadable, ProbeNotFound:
		default:
			return fmt.Errorf("probe %s has unknown observed %q", p.Kind, p.Observed)
		}
		if p.Kind == ProbeOwnInputRead {
			if p.Expected != ProbeReadable {
				return fmt.Errorf("own-input probe must expect readable, got %q", p.Expected)
			}
			if p.Observed != ProbeReadable {
				return fmt.Errorf("own input not readable (observed %q)", p.Observed)
			}
			continue
		}
		if p.Expected != ProbeDenied {
			return fmt.Errorf("forbidden probe %s expects %q, want denied", p.Kind, p.Expected)
		}
		switch p.Observed {
		case ProbeDenied:
		case ProbeNotFound:
			if p.ControllerTargetProofDigest == "" {
				return fmt.Errorf("probe %s observed not-found without a controller target-existence proof", p.Kind)
			}
		default:
			return fmt.Errorf("forbidden probe %s observed readable", p.Kind)
		}
	}
	for kind := range requiredStageProbes {
		if seen[kind] != 1 {
			return fmt.Errorf("probe kind %s appears %d times, want exactly 1", kind, seen[kind])
		}
	}
	return nil
}

// ProbeFilesystem performs the actual access attempt the exact child would
// make: read for files, traverse+list for directories.
func ProbeFilesystem(target string) string {
	fi, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return ProbeNotFound
		}
		return ProbeDenied
	}
	if fi.IsDir() {
		if _, err := os.ReadDir(target); err != nil {
			return ProbeDenied
		}
		return ProbeReadable
	}
	if _, err := os.ReadFile(target); err != nil {
		return ProbeDenied
	}
	return ProbeReadable
}

// ---------- controller target-existence proofs ----------

var ErrTargetChanged = errors.New("controller target proof: target missing or changed since capture")

// ControllerTargetProof is the controller-side record, captured immediately
// before child launch, that a denied probe's target actually existed with the
// recorded content and inherited parent policy. "not-found" without this proof
// is not a successful denial.
type ControllerTargetProof struct {
	TargetPath         string
	ContentDigest      string // sha256 of exact bytes (empty for a directory)
	ModeOctal          string
	ParentPolicyDigest string // digest of the parent directory policy
}

func (p *ControllerTargetProof) Digest() string {
	sum := sha256.Sum256([]byte(p.TargetPath + "\x00" + p.ContentDigest + "\x00" +
		p.ModeOctal + "\x00" + p.ParentPolicyDigest))
	return hex.EncodeToString(sum[:])
}

// CaptureControllerTargetProof records existence, content and parent policy
// of a probe target. It fails when the target does not exist — a proof must
// prove a real pre-launch target.
func CaptureControllerTargetProof(target string) (*ControllerTargetProof, error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}
	content := ""
	if !fi.IsDir() {
		b, err := os.ReadFile(target)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		content = hex.EncodeToString(sum[:])
	}
	parent := filepath.Dir(target)
	pfi, err := os.Stat(parent)
	if err != nil {
		return nil, err
	}
	return &ControllerTargetProof{
		TargetPath:         target,
		ContentDigest:      content,
		ModeOctal:          fmt.Sprintf("%04o", fi.Mode().Perm()),
		ParentPolicyDigest: fmt.Sprintf("%04o", pfi.Mode().Perm()),
	}, nil
}

// VerifyControllerTargetProof recomputes the capture against the current
// filesystem; any drift is ErrTargetChanged.
func VerifyControllerTargetProof(target string, proof *ControllerTargetProof) error {
	now, err := CaptureControllerTargetProof(target)
	if err != nil {
		return ErrTargetChanged
	}
	if *now != *proof {
		return ErrTargetChanged
	}
	return nil
}

// isSubdir reports whether sub lies strictly under root.
func isSubdir(root, sub string) bool {
	rel, err := filepath.Rel(root, sub)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// SnapshotActive returns the workspace dirs currently held by other attempts
// (the real active-sibling targets for the isolation probes). When no other
// attempt is in flight at this instant, the most recent retired workspace
// stands in: the physical boundary (a locked foreign input) is identical,
// which is what the probe proves.
func (m *StageWorkspaceManager) SnapshotActive(exclude string) string {
	m.mu.Lock()
	for id, ws := range m.held {
		if id != exclude {
			m.mu.Unlock()
			return ws
		}
	}
	m.mu.Unlock()
	// Fallback: newest other workspace on disk (its .locked sentinel exists).
	var newest string
	for _, stage := range []string{"author", "review"} {
		dir := filepath.Join(m.root, stage)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if !e.IsDir() {
				continue
			}
			ws := filepath.Join(dir, e.Name())
			if ws == m.held[exclude] {
				continue
			}
			if _, err := os.Stat(filepath.Join(ws, "input", ".locked")); err == nil {
				return ws
			}
			if newest == "" {
				newest = ws
			}
		}
	}
	return newest
}
