package main

// 048 US5 (T053): the dev-only data flywheel — FailureArchive construction
// and the sealed core-only before/after comparison (runner-cli.md §3.1,
// data-model.md §11).
//
// Everything here fails closed: an archive is derived only from a complete
// sealed `dev-comparison` series whose every receipt still rejudges; a
// comparison opens only the exact core172 run paths both manifests bind and
// never traverses a holdout path, never reads a holdout receipt, and never
// produces either official score family (only `score` can do that).
//
// The reader is deliberately narrower than LoadScoreInputs: an official-dual
// candidate mid-flight (core leg complete, holdout leg not yet started) is a
// legal compare input, so the protected-execution / pre-holdout / binding
// receipts are not part of the flywheel evidence bundle at all.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------- §11 dev failure taxonomy ----------

// DevRootCause is the closed dev-flywheel root-cause enum (spec US5): the
// four attribution classes a skill revision can target. Anything else is
// rejected — an unclassifiable failure is surfaced, never silently filed.
type DevRootCause string

const (
	RootCauseMissingTrigger    DevRootCause = "missing-trigger-cue"    // 触发词缺失
	RootCauseNarrowDescription DevRootCause = "narrow-description"     // description 过窄
	RootCauseContradictoryBody DevRootCause = "contradictory-body"     // 正文自相矛盾
	RootCauseHostSpecific      DevRootCause = "host-specific-behavior" // 宿主特有行为
)

var devRootCauses = map[DevRootCause]bool{
	RootCauseMissingTrigger:    true,
	RootCauseNarrowDescription: true,
	RootCauseContradictoryBody: true,
	RootCauseHostSpecific:      true,
}

// ValidDevRootCause reports whether c is one of the closed dev root causes.
func ValidDevRootCause(c DevRootCause) bool { return devRootCauses[c] }

const failureArchiveSealedBy = "skill-eval-048-dev-flywheel"

// FailureArchiveInput carries everything `failure-archive` needs: the
// complete sealed dev-comparison series root and the operator's closed-enum
// classification of every median-fail cell.
type FailureArchiveInput struct {
	SeriesRoot string
	// RootCauses maps "host/caseID" → closed root cause and must cover every
	// median-fail cell exactly. A failing cell without a classification, a
	// classification of a median-pass cell, or a value outside the enum fails
	// the build: an unclassified failure never enters a sealed archive.
	RootCauses map[string]DevRootCause
}

// flyCellKey identifies one host × case cell of the core matrix.
type flyCellKey struct {
	Host   string
	CaseID string
}

func (k flyCellKey) String() string { return k.Host + "/" + k.CaseID }

// flyCellOutcome is the terminal evidence of one cell: the three per-ordinal
// terminal statuses and the binary median over them.
type flyCellOutcome struct {
	Statuses   []string
	Outcomes   []CaseScoreOutcome
	Median     bool
	RunDigests []string
}

// BuildFailureArchive derives the immutable dev failure archive of one
// complete sealed dev-comparison series: the three-ordinal binary median for
// every host × core172 case, the closed dev failure taxonomy for the
// median-fail cells, and the seal that freezes it.
func BuildFailureArchive(in *FailureArchiveInput) (*FailureArchive, error) {
	if in == nil {
		return nil, errors.New("nil failure archive input")
	}
	ev, err := flyLoadCoreSeries(in.SeriesRoot, PurposeDevComparison)
	if err != nil {
		return nil, err
	}
	causes := in.RootCauses
	if causes == nil {
		causes = map[string]DevRootCause{}
	}
	entries := []FailureArchiveEntry{}
	for _, key := range ev.sortedCells() {
		cell := ev.Cells[key]
		if cell.Median {
			if cause, classified := causes[key.String()]; classified {
				return nil, fmt.Errorf("case %s/%s is a median pass and must not be classified (got %q)",
					key.Host, key.CaseID, cause)
			}
			continue
		}
		cause, ok := causes[key.String()]
		if !ok {
			return nil, fmt.Errorf("median-fail cell %s/%s has no root-cause classification: an unclassified failure is never archived",
				key.Host, key.CaseID)
		}
		if !ValidDevRootCause(cause) {
			return nil, fmt.Errorf("median-fail cell %s/%s root cause %q is outside the closed dev enum",
				key.Host, key.CaseID, cause)
		}
		entries = append(entries, FailureArchiveEntry{
			CaseID:               key.CaseID,
			Host:                 key.Host,
			Split:                SplitDevRegression,
			BaselineSeriesID:     ev.Manifest.SeriesID,
			BaselineRunDigests:   append([]string(nil), cell.RunDigests...),
			OrdinalStates:        append([]string(nil), cell.Statuses...),
			BinaryMedian:         0,
			FailureClass:         flyMajorityClass(cell),
			RootCause:            string(cause),
			BeforeSeriesManifest: ev.Manifest.ManifestDigest,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CaseID != entries[j].CaseID {
			return entries[i].CaseID < entries[j].CaseID
		}
		return entries[i].Host < entries[j].Host
	})
	if err := validateFailureArchiveEntries(entries, ev.Manifest.SeriesID, ev.Plan.ReceiptDigest); err != nil {
		return nil, err
	}
	archive := &FailureArchive{
		SchemaVersion:               1,
		BaselineSkillSnapshotDigest: ev.Manifest.SkillSnapshotDigest,
		CoreExecutionPlanDigest:     ev.Plan.ReceiptDigest,
		ToolIdentityDigests:         copyStringMap(ev.Plan.ToolIdentityDigests),
		Entries:                     entries,
	}
	if err := sealFailureArchive(archive); err != nil {
		return nil, err
	}
	return archive, nil
}

// flyMajorityClass picks the failure class of a median-fail cell: the class
// the majority of its failing ordinals recorded, ties broken by the
// lexicographically smallest class, so the value is deterministic.
func flyMajorityClass(cell flyCellOutcome) string {
	counts := map[string]int{}
	for i, o := range cell.Outcomes {
		if o == CaseOutcomePass {
			continue
		}
		class := cell.Statuses[i]
		if !failureClassesV2[class] {
			class = FailureRunnerError
		}
		counts[class]++
	}
	best, bestN := "", 0
	for class, n := range counts {
		if n > bestN || (n == bestN && (best == "" || class < best)) {
			best, bestN = class, n
		}
	}
	return best
}

// validateFailureArchiveEntries is the closed per-entry read of an archive:
// dev-regression split only, closed failure class and root cause, three
// ordinal states, and a median that agrees with them.
func validateFailureArchiveEntries(entries []FailureArchiveEntry, baselineSeriesID, corePlanDigest string) error {
	for _, e := range entries {
		if e.Split != SplitDevRegression {
			return fmt.Errorf("archive entry %s/%s carries split %q: holdout material never enters the dev failure archive",
				e.Host, e.CaseID, e.Split)
		}
		if !caseIDRE.MatchString(e.CaseID) {
			return fmt.Errorf("archive entry case id %q is not a safe case id", e.CaseID)
		}
		if !validHosts[e.Host] {
			return fmt.Errorf("archive entry %s names host %q, which is not a formal host", e.CaseID, e.Host)
		}
		if baselineSeriesID != "" && e.BaselineSeriesID != baselineSeriesID {
			return fmt.Errorf("archive entry %s/%s names series %q, want the archived baseline %q",
				e.Host, e.CaseID, e.BaselineSeriesID, baselineSeriesID)
		}
		if !failureClassesV2[e.FailureClass] {
			return fmt.Errorf("archive entry %s/%s failure class %q is outside the closed v2 set",
				e.Host, e.CaseID, e.FailureClass)
		}
		if !ValidDevRootCause(DevRootCause(e.RootCause)) {
			return fmt.Errorf("archive entry %s/%s root cause %q is outside the closed dev enum",
				e.Host, e.CaseID, e.RootCause)
		}
		if len(e.OrdinalStates) != len(Ordinals) {
			return fmt.Errorf("archive entry %s/%s carries %d ordinal states, want 3",
				e.Host, e.CaseID, len(e.OrdinalStates))
		}
		passes := 0
		for i, st := range e.OrdinalStates {
			switch st {
			case CaseStatusPass:
				passes++
			case CaseStatusFail, CaseStatusRunnerError:
			default:
				return fmt.Errorf("archive entry %s/%s ordinal %d state %q is outside the closed set",
					e.Host, e.CaseID, i+1, st)
			}
		}
		// A median pass is never archived: the archive is the failure set.
		if MedianBoolOrdinalStates(e.OrdinalStates) || passes == len(e.OrdinalStates) {
			return fmt.Errorf("archive entry %s/%s records a median pass; only failures are archived",
				e.Host, e.CaseID)
		}
		if e.BinaryMedian != 0 {
			return fmt.Errorf("archive entry %s/%s binary_median %d, want 0 for a failure",
				e.Host, e.CaseID, e.BinaryMedian)
		}
		if corePlanDigest == "" {
			return errors.New("archive carries no core execution plan digest")
		}
	}
	return nil
}

// MedianBoolOrdinalStates is the binary median over terminal status strings.
func MedianBoolOrdinalStates(states []string) bool {
	booleans := make([]bool, 0, len(states))
	for _, st := range states {
		booleans = append(booleans, st == CaseStatusPass)
	}
	return MedianBool(booleans)
}

// ---------- §11 failure archive sealing ----------

type failureArchiveDigestPreimage struct {
	SchemaVersion               int                   `json:"schema_version"`
	BaselineSkillSnapshotDigest string                `json:"baseline_skill_snapshot_digest"`
	CoreExecutionPlanDigest     string                `json:"core_execution_plan_digest"`
	ToolIdentityDigests         map[string]string     `json:"tool_identity_digests"`
	Entries                     []FailureArchiveEntry `json:"entries"`
	ArchiveDigest               string                `json:"archive_digest"`
}

type failureArchiveSealPreimage struct {
	SchemaVersion int    `json:"schema_version"`
	ArchiveDigest string `json:"archive_digest"`
	SealedBy      string `json:"sealed_by"`
}

func failureArchiveDigestProjection(a *FailureArchive) *failureArchiveDigestPreimage {
	return &failureArchiveDigestPreimage{
		SchemaVersion:               a.SchemaVersion,
		BaselineSkillSnapshotDigest: a.BaselineSkillSnapshotDigest,
		CoreExecutionPlanDigest:     a.CoreExecutionPlanDigest,
		ToolIdentityDigests:         a.ToolIdentityDigests,
		Entries:                     a.Entries,
		ArchiveDigest:               a.ArchiveDigest,
	}
}

// sealFailureArchive is the freeze-before-digest seal: every field is
// populated and validated first, then archive_digest is derived over the
// digest-less archive and seal_digest over that digest alone.
func sealFailureArchive(a *FailureArchive) error {
	if a == nil {
		return errors.New("nil failure archive")
	}
	if a.ArchiveDigest != "" || a.SealDigest != "" {
		return errors.New("freeze-before-digest violated: archive already carries a digest")
	}
	if a.SchemaVersion != 1 {
		return fmt.Errorf("archive schema_version %d, want 1", a.SchemaVersion)
	}
	if a.BaselineSkillSnapshotDigest == "" || a.CoreExecutionPlanDigest == "" {
		return errors.New("archive does not bind its baseline snapshot and core plan")
	}
	if len(a.ToolIdentityDigests) != len(validHosts) {
		return errors.New("archive does not bind every host tool identity")
	}
	d, err := CanonicalSHA256(failureArchiveDigestProjection(a))
	if err != nil {
		return err
	}
	a.ArchiveDigest = d
	seal, err := CanonicalSHA256(failureArchiveSealPreimage{
		SchemaVersion: 1, ArchiveDigest: d, SealedBy: failureArchiveSealedBy,
	})
	if err != nil {
		return err
	}
	a.SealDigest = seal
	return nil
}

// VerifyFailureArchiveSeal re-derives both digests of an archive read back
// from disk. Any post-seal mutation — a root cause, a class, an entry —
// drifts at least one of them.
func VerifyFailureArchiveSeal(a *FailureArchive) error {
	if a == nil || a.ArchiveDigest == "" || a.SealDigest == "" {
		return errors.New("failure archive is not sealed")
	}
	pre := failureArchiveDigestProjection(a)
	pre.ArchiveDigest = ""
	d, err := CanonicalSHA256(pre)
	if err != nil {
		return err
	}
	if d != a.ArchiveDigest {
		return errors.New("failure archive digest mismatch (post-seal mutation)")
	}
	seal, err := CanonicalSHA256(failureArchiveSealPreimage{
		SchemaVersion: 1, ArchiveDigest: a.ArchiveDigest, SealedBy: failureArchiveSealedBy,
	})
	if err != nil {
		return err
	}
	if seal != a.SealDigest {
		return errors.New("failure archive seal mismatch (post-seal mutation)")
	}
	return nil
}

// LoadFailureArchive reads and verifies a sealed failure archive from disk.
func LoadFailureArchive(path string) (*FailureArchive, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failure archive %s: %w", path, err)
	}
	a := &FailureArchive{}
	if err := StrictParseClosed(b, a); err != nil {
		return nil, fmt.Errorf("failure archive %s: %w", path, err)
	}
	if err := VerifyFailureArchiveSeal(a); err != nil {
		return nil, fmt.Errorf("failure archive %s: %w", path, err)
	}
	if err := validateFailureArchiveEntries(a.Entries, "", a.CoreExecutionPlanDigest); err != nil {
		return nil, fmt.Errorf("failure archive %s: %w", path, err)
	}
	return a, nil
}

// WriteFailureArchive materializes the archive as an immutable frozen file.
func WriteFailureArchive(path string, a *FailureArchive) error {
	if err := VerifyFailureArchiveSeal(a); err != nil {
		return fmt.Errorf("refusing to write an unverifiable failure archive: %w", err)
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return WriteFrozenFile(path, b)
}

// ---------- §11 core-only series reader ----------

// flySeries is the core172-only evidence bundle the dev flywheel may read out
// of one series root. It carries no holdout artifact by construction.
type flySeries struct {
	Root     string
	Manifest *FormalSeriesManifest
	Plan     *CoreExecutionPlanReceipt
	Package  *SkillPackageValidationReceipt
	Prepare  *GreenTestReceipt
	Cases    map[string]*TriggerCaseV2
	Cells    map[flyCellKey]flyCellOutcome
	RunPaths []string
}

func (s *flySeries) sortedCells() []flyCellKey {
	keys := make([]flyCellKey, 0, len(s.Cells))
	for k := range s.Cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].CaseID != keys[j].CaseID {
			return keys[i].CaseID < keys[j].CaseID
		}
		return keys[i].Host < keys[j].Host
	})
	return keys
}

// coreOnlyRunPath resolves one flywheel input path inside a series root. The
// guard is deliberately coarse: a dev-flywheel path never legitimately
// contains the holdout substring in any shape, and containment is verified
// after symlink elimination, so a holdout-split run directory, the holdout96
// dataset directory and any escaping path are all refused before a byte of
// them is read.
func coreOnlyRunPath(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("empty flywheel path")
	}
	slash := filepath.ToSlash(filepath.Clean(rel))
	if !safeRelativePath(slash) {
		return "", fmt.Errorf("flywheel path %q is not containment-safe", rel)
	}
	lower := strings.ToLower(slash)
	if strings.Contains(lower, SplitHoldout) || strings.Contains(lower, MembershipHoldout96) {
		return "", fmt.Errorf("flywheel path %q requires holdout traversal: the dev flywheel never reads holdout material", rel)
	}
	return EnsureInside(root, filepath.Join(root, filepath.FromSlash(slash)))
}

// flyLoadCoreSeries reads the complete core172 evidence of one series root
// for the dev flywheel. It verifies every seal it crosses: the series
// manifest, the shared core plan, the package-validation and series-prepare
// receipts, the frozen core dataset and all 3 host × 3 ordinal run manifests
// with their per-case receipts and artifacts.
func flyLoadCoreSeries(root string, want SeriesPurpose) (*flySeries, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("series root is empty")
	}
	if fi, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("series root %s: %w", root, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("series root %s is not a directory", root)
	}
	manifest, err := LoadSeriesManifest(filepath.Join(root, seriesManifestFile))
	if err != nil {
		return nil, err
	}
	if manifest.Purpose != want {
		return nil, fmt.Errorf("series %s has purpose %q; the dev flywheel accepts %q evidence only",
			manifest.SeriesID, manifest.Purpose, want)
	}
	switch manifest.State {
	case StateSealed, StateComplete:
	default:
		return nil, fmt.Errorf("series %s state %q: an incomplete series is never dev-flywheel evidence",
			manifest.SeriesID, manifest.State)
	}
	plan, err := LoadCoreExecutionPlan(filepath.Join(root, corePlanFile))
	if err != nil {
		return nil, err
	}
	if manifest.CoreExecutionPlanDigest != plan.ReceiptDigest {
		return nil, fmt.Errorf("series %s references core execution plan %q but the sealed plan is %q",
			manifest.SeriesID, manifest.CoreExecutionPlanDigest, plan.ReceiptDigest)
	}
	if manifest.DatasetManifests[MembershipCore172] != plan.CoreManifestDigest {
		return nil, fmt.Errorf("series %s binds core172 manifest %q, the shared plan freezes %q",
			manifest.SeriesID, manifest.DatasetManifests[MembershipCore172], plan.CoreManifestDigest)
	}
	pkg := &SkillPackageValidationReceipt{}
	if err := loadStrictFile(filepath.Join(root, packageValidationFile), pkg); err != nil {
		return nil, err
	}
	if err := scorePackageReceipt(&ScoreEligibilityInput{Manifest: manifest, Package: pkg}); err != nil {
		return nil, fmt.Errorf("package validation receipt: %w", err)
	}
	prepare := &GreenTestReceipt{}
	if err := loadStrictFile(filepath.Join(root, greenSeriesPrepareFile), prepare); err != nil {
		return nil, err
	}
	if err := flySeriesPrepareReceipt(prepare, manifest); err != nil {
		return nil, err
	}
	cases, err := loadScoreDataset(filepath.Join(root, datasetsDir, MembershipCore172),
		SplitDevRegression, MembershipCore172, manifest.DatasetManifests[MembershipCore172])
	if err != nil {
		return nil, err
	}
	ev := &flySeries{
		Root: root, Manifest: manifest, Plan: plan, Package: pkg, Prepare: prepare, Cases: cases,
		Cells: map[flyCellKey]flyCellOutcome{},
	}
	if err := flyLoadCoreRuns(ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// flySeriesPrepareReceipt re-checks the series-prepare green receipt bindings
// of one flywheel input (the series-prepare half of the scorer's green-receipt
// gate; the pre-holdout half belongs to the holdout leg and is never read).
func flySeriesPrepareReceipt(r *GreenTestReceipt, m *FormalSeriesManifest) error {
	if err := greenReceiptStructure(r, SuiteSeriesPrepare); err != nil {
		return err
	}
	if r.ReceiptDigest != m.GreenTestReceiptDigest {
		return fmt.Errorf("series-prepare receipt digest %q != manifest bound %q", r.ReceiptDigest, m.GreenTestReceiptDigest)
	}
	if r.StableIdentityDigest == nil || *r.StableIdentityDigest != m.SeriesPrepareIdentityDigest {
		return errors.New("series-prepare receipt stable identity mismatch")
	}
	if deref(r.SnapshotDigest) != m.SkillSnapshotDigest {
		return errors.New("series-prepare receipt snapshot binding mismatch")
	}
	if deref(r.PackageValidationReceiptDigest) != m.SkillPackageValidationReceiptDigest {
		return errors.New("series-prepare receipt package-validation binding mismatch")
	}
	if r.RunnerDigest != m.RunnerDigest || r.JudgeRuleDigest != m.JudgeRuleDigest {
		return errors.New("series-prepare receipt runner/judge digest drift")
	}
	return nil
}

// flyLoadCoreRuns reads exactly the nine sealed dev-regression run manifests
// of the series and every case receipt under them. It never walks the runs
// directory, so a holdout run tree inside the same root is never opened.
func flyLoadCoreRuns(ev *flySeries) error {
	for _, h := range ev.Manifest.Hosts {
		for _, o := range Ordinals {
			rel := filepath.Join(runsDir, fmt.Sprintf("%s-%s-o%d", h, SplitDevRegression, o), runManifestName)
			p, err := coreOnlyRunPath(ev.Root, rel)
			if err != nil {
				return err
			}
			ev.RunPaths = append(ev.RunPaths, p)
			// Identity before deeper validation: a receipt filed at a dev path
			// but labelled another host/split/ordinal (holdout above all) is
			// refused before any of its content is trusted.
			labelled := &PrimaryRunManifest{}
			if err := loadStrictFile(p, labelled); err != nil {
				return fmt.Errorf("core run %s/%s ordinal %d: %w", h, SplitDevRegression, o, err)
			}
			if labelled.Host != h || labelled.Split != SplitDevRegression || labelled.Ordinal != o {
				return fmt.Errorf("core run at %s disagrees with its own identity (%s/%s/%d): a %s receipt never enters the dev flywheel",
					p, h, SplitDevRegression, o, labelled.Split)
			}
			run, err := LoadPrimaryRun(p)
			if err != nil {
				return fmt.Errorf("core run %s/%s ordinal %d: %w", h, SplitDevRegression, o, err)
			}
			if run.SeriesID != ev.Manifest.SeriesID {
				return fmt.Errorf("core run %s/%s ordinal %d belongs to series %q: cross-series splice",
					h, SplitDevRegression, o, run.SeriesID)
			}
			if identity := run.ToolProvenance.ToolIdentityDigest; identity != ev.Plan.ToolIdentityDigests[h] {
				return fmt.Errorf("core run %s/%s ordinal %d observed tool_identity_digest %q, the shared plan freezes %q",
					h, SplitDevRegression, o, identity, ev.Plan.ToolIdentityDigests[h])
			}
			if run.ExpectedCaseCount != len(ev.Cases) {
				return fmt.Errorf("core run %s/%s ordinal %d carries %d cases, the sealed core172 dataset has %d",
					h, SplitDevRegression, o, run.ExpectedCaseCount, len(ev.Cases))
			}
			ids := append([]string(nil), run.CaseIDs...)
			sort.Strings(ids)
			for _, caseID := range ids {
				key := flyCellKey{Host: h, CaseID: caseID}
				cell, seen := ev.Cells[key]
				if !seen {
					cell = flyCellOutcome{}
					cell.RunDigests = make([]string, 0, len(Ordinals))
				}
				status, err := flyReadCase(ev, run, h, o, caseID)
				if err != nil {
					return err
				}
				cell.Statuses = append(cell.Statuses, status)
				cell.Outcomes = append(cell.Outcomes, flyOutcome(status))
				cell.RunDigests = append(cell.RunDigests, run.RunDigest)
				ev.Cells[key] = cell
			}
		}
	}
	for key, cell := range ev.Cells {
		if len(cell.Statuses) != len(Ordinals) {
			return fmt.Errorf("case %s/%s carries %d ordinals, want all three", key.Host, key.CaseID, len(cell.Statuses))
		}
		cell.Median = MedianBoolOrdinalStates(cell.Statuses)
		ev.Cells[key] = cell
	}
	if len(ev.Cells) != len(ev.Cases)*len(ev.Manifest.Hosts) {
		return fmt.Errorf("core matrix covers %d host × case cells, want %d", len(ev.Cells), len(ev.Cases)*len(ev.Manifest.Hosts))
	}
	return nil
}

func flyOutcome(status string) CaseScoreOutcome {
	switch status {
	case CaseStatusPass:
		return CaseOutcomePass
	case CaseStatusRunnerError:
		return CaseOutcomeRunnerError
	default:
		return CaseOutcomeFail
	}
}

// flyReadCase reads, digest-verifies and rejudges one primary case receipt —
// the same closed path the official scorer uses, so a flywheel median comes
// from evidence that still reproduces its verdict.
func flyReadCase(ev *flySeries, run *PrimaryRunManifest, host string, ordinal int, caseID string) (string, error) {
	receipt, err := loadCaseRunReceipt(ev.Root, RunKey{Host: host, Split: SplitDevRegression, Ordinal: ordinal}, caseID)
	if err != nil {
		return "", err
	}
	if err := validateCaseRunReceiptOnDisk(receipt, caseID); err != nil {
		return "", err
	}
	normalized, err := readScoreArtifact(ev.Root, receipt.NormalizedEventsPath, receipt.NormalizedEventsDigest)
	if err != nil {
		return "", err
	}
	if _, err := readScoreArtifact(ev.Root, receipt.RawEventsPath, receipt.RawEventsDigest); err != nil {
		return "", err
	}
	dump, err := readScoreArtifact(ev.Root, receipt.StoreDumpPath, receipt.StoreDumpDigest)
	if err != nil {
		return "", err
	}
	rejudged, err := RejudgeFromRecordedCase(receipt.Status, normalized, dump, ev.Cases[caseID])
	if err != nil {
		return "", fmt.Errorf("core run %s/%s/%d case %s: %w", host, SplitDevRegression, ordinal, caseID, err)
	}
	if err := verdictAgrees(caseID, receipt.Verdict, rejudged); err != nil {
		return "", fmt.Errorf("core run %s/%s/%d: %w", host, SplitDevRegression, ordinal, err)
	}
	return receipt.Status, nil
}

// ---------- §11 FlywheelComparisonReceipt ----------

// Closed per-cell transition labels of the binary median comparison.
const (
	TransitionStablePass = "stable-pass"
	TransitionStableFail = "stable-fail"
	TransitionFailToPass = "fail-to-pass"
	TransitionRegression = "regression"
)

// FlywheelCellRef names one host × case cell of the core matrix.
type FlywheelCellRef struct {
	CaseID string `json:"case_id"`
	Host   string `json:"host"`
}

// FlywheelCaseMedian is one compared host × case row.
type FlywheelCaseMedian struct {
	CaseID          string `json:"case_id"`
	Host            string `json:"host"`
	BaselineMedian  int    `json:"baseline_median"`
	CandidateMedian int    `json:"candidate_median"`
	Transition      string `json:"transition"`
}

// FlywheelBackfillRecord is the verified one-to-one mapping from the
// required fail-to-pass source IDs to their new extension case IDs.
type FlywheelBackfillRecord struct {
	ExtensionManifestDigest string            `json:"extension_manifest_digest"`
	ExtensionCaseCount      int               `json:"extension_case_count"`
	Lineage                 map[string]string `json:"lineage"`
	LineageDigest           string            `json:"lineage_digest"`
	Verified                bool              `json:"verified"`
}

// FlywheelExtensionDiagnostics is the single column the extension diagnostic
// run occupies in a comparison receipt. It is explicit about being neither
// comparable to core172 nor gating anything.
type FlywheelExtensionDiagnostics struct {
	ReceiptDigest string `json:"receipt_digest"`
	NonComparable bool   `json:"non_comparable"`
	NonGating     bool   `json:"non_gating"`
	Note          string `json:"note"`
}

// FlywheelComparisonReceipt is the sealed dev-only before/after receipt of
// one flywheel round. It has no field that can hold a gate, a host score or
// either official score family: the comparison is dev evidence, and only
// `score` may publish a score.
type FlywheelComparisonReceipt struct {
	SchemaVersion                          int                           `json:"schema_version"`
	BaselineSeriesID                       string                        `json:"baseline_series_id"`
	BaselineSeriesManifestDigest           string                        `json:"baseline_series_manifest_digest"`
	BaselineSkillSnapshotDigest            string                        `json:"baseline_skill_snapshot_digest"`
	BaselineSkillDigest                    string                        `json:"baseline_skill_digest"`
	CandidateSeriesID                      string                        `json:"candidate_series_id"`
	CandidateSeriesManifestDigest          string                        `json:"candidate_series_manifest_digest"`
	CandidateSkillSnapshotDigest           string                        `json:"candidate_skill_snapshot_digest"`
	CandidateSkillDigest                   string                        `json:"candidate_skill_digest"`
	CoreExecutionPlanDigest                string                        `json:"core_execution_plan_digest"`
	CoreManifestDigest                     string                        `json:"core_manifest_digest"`
	ToolIdentityDigests                    map[string]string             `json:"tool_identity_digests"`
	ComparedCoreCaseCount                  int                           `json:"compared_core_case_count"`
	FailToPassCount                        int                           `json:"fail_to_pass_count"`
	RegressionCount                        int                           `json:"regression_count"`
	StablePassCount                        int                           `json:"stable_pass_count"`
	StableFailCount                        int                           `json:"stable_fail_count"`
	FailToPassCases                        []FlywheelCellRef             `json:"fail_to_pass_cases"`
	RegressionCases                        []FlywheelCellRef             `json:"regression_cases"`
	CaseMedians                            []FlywheelCaseMedian          `json:"case_medians"`
	RequiredExtensionBackfillSourceCaseIDs []string                      `json:"required_extension_backfill_source_case_ids"`
	RequiredExtensionBackfillDigest        string                        `json:"required_extension_backfill_digest"`
	FailureArchiveDigest                   string                        `json:"failure_archive_digest"`
	ExtensionBackfill                      *FlywheelBackfillRecord       `json:"extension_backfill,omitempty"`
	ExtensionDiagnostics                   *FlywheelExtensionDiagnostics `json:"extension_diagnostics,omitempty"`
	ExtensionSeparateFromCore              bool                          `json:"extension_separate_from_core"`
	SC5Verdict                             string                        `json:"sc5_verdict"` // pass | fail
	ReportDigest                           string                        `json:"report_digest"`
	SealDigest                             string                        `json:"seal_digest"`
}

const (
	flywheelSealedBy        = "skill-eval-048-dev-flywheel"
	flywheelBackfillAlgo    = "engram-flywheel-backfill-digest-v1"
	flywheelLineageAlgo     = "engram-flywheel-lineage-digest-v1"
	flywheelExtensionMethod = "extension diagnostics are dev-only: not comparable to core172, never gating, never part of either official score family"
)

// CompareDevSeriesInput carries the four flywheel comparison inputs.
type CompareDevSeriesInput struct {
	BaselineSeriesRoot  string
	CandidateSeriesRoot string
	FailureArchivePath  string
	// ExtensionReceiptPath is the dev diagnostic extension receipt whose
	// manifest carries the append-only extension_lineage. It is optional in
	// the flag surface but becomes mandatory the moment the round requires a
	// backfill: an unverified fail-to-pass set can never be sealed.
	ExtensionReceiptPath string
}

// CompareDevSeries is the `compare` producer: a sealed dev-only before/after
// comparison of two core172 legs that share one CoreExecutionPlanReceipt,
// with the skill package digest as the single intentional variable.
func CompareDevSeries(in *CompareDevSeriesInput) (*FlywheelComparisonReceipt, error) {
	if in == nil {
		return nil, errors.New("nil flywheel comparison input")
	}
	if in.FailureArchivePath == "" {
		return nil, errors.New("compare requires the sealed failure archive of the baseline")
	}
	baseline, err := flyLoadCoreSeries(in.BaselineSeriesRoot, PurposeDevComparison)
	if err != nil {
		return nil, fmt.Errorf("baseline series: %w", err)
	}
	candidate, err := flyLoadCoreSeries(in.CandidateSeriesRoot, PurposeOfficialDual)
	if err != nil {
		return nil, fmt.Errorf("candidate series: %w", err)
	}
	archive, err := LoadFailureArchive(in.FailureArchivePath)
	if err != nil {
		return nil, err
	}
	if err := flySharedPlan(baseline, candidate); err != nil {
		return nil, err
	}
	plan := baseline.Plan
	if archive.CoreExecutionPlanDigest != plan.ReceiptDigest {
		return nil, fmt.Errorf("failure archive is bound to core execution plan %q, the shared plan is %q",
			archive.CoreExecutionPlanDigest, plan.ReceiptDigest)
	}
	if archive.BaselineSkillSnapshotDigest != baseline.Manifest.SkillSnapshotDigest {
		return nil, fmt.Errorf("failure archive is bound to baseline snapshot %q, the series froze %q",
			archive.BaselineSkillSnapshotDigest, baseline.Manifest.SkillSnapshotDigest)
	}
	// Every entry must name this exact baseline series: an archive of another
	// dev-comparison series is a foreign baseline, never this one's truth.
	if err := validateFailureArchiveEntries(archive.Entries, baseline.Manifest.SeriesID, plan.ReceiptDigest); err != nil {
		return nil, fmt.Errorf("failure archive: %w", err)
	}
	if err := flyArchiveMatchesBaseline(archive, baseline); err != nil {
		return nil, err
	}

	report := &FlywheelComparisonReceipt{
		SchemaVersion:                 1,
		BaselineSeriesID:              baseline.Manifest.SeriesID,
		BaselineSeriesManifestDigest:  baseline.Manifest.ManifestDigest,
		BaselineSkillSnapshotDigest:   baseline.Manifest.SkillSnapshotDigest,
		BaselineSkillDigest:           baseline.Manifest.SkillDigest,
		CandidateSeriesID:             candidate.Manifest.SeriesID,
		CandidateSeriesManifestDigest: candidate.Manifest.ManifestDigest,
		CandidateSkillSnapshotDigest:  candidate.Manifest.SkillSnapshotDigest,
		CandidateSkillDigest:          candidate.Manifest.SkillDigest,
		CoreExecutionPlanDigest:       plan.ReceiptDigest,
		CoreManifestDigest:            plan.CoreManifestDigest,
		ToolIdentityDigests:           copyStringMap(plan.ToolIdentityDigests),
		ComparedCoreCaseCount:         len(baseline.Cases),
		FailureArchiveDigest:          archive.ArchiveDigest,
		ExtensionSeparateFromCore:     true,
	}
	for _, key := range baseline.sortedCells() {
		before := baseline.Cells[key].Median
		after := candidate.Cells[key].Median
		transition := TransitionStablePass
		switch {
		case !before && after:
			transition = TransitionFailToPass
			report.FailToPassCount++
			report.FailToPassCases = append(report.FailToPassCases, FlywheelCellRef{CaseID: key.CaseID, Host: key.Host})
		case before && !after:
			transition = TransitionRegression
			report.RegressionCount++
			report.RegressionCases = append(report.RegressionCases, FlywheelCellRef{CaseID: key.CaseID, Host: key.Host})
		case before && after:
			report.StablePassCount++
		default:
			report.StableFailCount++
		}
		report.CaseMedians = append(report.CaseMedians, FlywheelCaseMedian{
			CaseID:          key.CaseID,
			Host:            key.Host,
			BaselineMedian:  flyBit(before),
			CandidateMedian: flyBit(after),
			Transition:      transition,
		})
	}
	// The regression list is not pre-sorted (it follows the deterministic cell
	// order above) but the required backfill set is sorted and deduplicated:
	// any host's fail-to-pass makes the core case ID a backfill source.
	sources := map[string]bool{}
	for _, ref := range report.FailToPassCases {
		sources[ref.CaseID] = true
	}
	report.RequiredExtensionBackfillSourceCaseIDs = sortedKeysOf(sources)
	digest, err := CanonicalSHA256(struct {
		Algorithm     string   `json:"algorithm"`
		SourceCaseIDs []string `json:"source_case_ids"`
	}{Algorithm: flywheelBackfillAlgo, SourceCaseIDs: report.RequiredExtensionBackfillSourceCaseIDs})
	if err != nil {
		return nil, err
	}
	report.RequiredExtensionBackfillDigest = digest

	if len(report.RequiredExtensionBackfillSourceCaseIDs) > 0 && strings.TrimSpace(in.ExtensionReceiptPath) == "" {
		return nil, fmt.Errorf("the round requires %d extension backfill source IDs and no extension receipt was given: an unverified fail-to-pass set is never sealed",
			len(report.RequiredExtensionBackfillSourceCaseIDs))
	}
	if strings.TrimSpace(in.ExtensionReceiptPath) != "" {
		backfill, diagnostics, err := flyVerifyExtensionBackfill(in.ExtensionReceiptPath, report.RequiredExtensionBackfillSourceCaseIDs)
		if err != nil {
			return nil, err
		}
		report.ExtensionBackfill = backfill
		report.ExtensionDiagnostics = diagnostics
	}
	// SC-5: the round passes only when at least one frozen-baseline median
	// failure became a median pass. Regressions are quantified above and never
	// silently folded away, but they do not flip the verdict on their own.
	report.SC5Verdict = "fail"
	if report.FailToPassCount >= 1 {
		report.SC5Verdict = "pass"
	}
	if err := sealFlywheelComparison(report); err != nil {
		return nil, err
	}
	return report, nil
}

func flyBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sortedKeysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// flySharedPlan proves both series imported the exact same core execution
// plan and that every stable execution condition it freezes still agrees with
// each series manifest — leaving the skill package digest as the only
// intentional variable between the two legs.
func flySharedPlan(baseline, candidate *flySeries) error {
	if baseline.Plan.ReceiptDigest != candidate.Plan.ReceiptDigest {
		return fmt.Errorf("baseline and candidate reference different core execution plans (%q vs %q): SC-5 compares only legs that share one sealed plan",
			baseline.Plan.ReceiptDigest, candidate.Plan.ReceiptDigest)
	}
	for _, pair := range []struct {
		name                string
		baseline, candidate string
	}{
		{"core manifest", baseline.Manifest.DatasetManifests[MembershipCore172], candidate.Manifest.DatasetManifests[MembershipCore172]},
	} {
		if pair.baseline != pair.candidate {
			return fmt.Errorf("baseline and candidate bind different %s (%q vs %q)", pair.name, pair.baseline, pair.candidate)
		}
	}
	for _, side := range []struct {
		name string
		m    *FormalSeriesManifest
	}{{"baseline", baseline.Manifest}, {"candidate", candidate.Manifest}} {
		switch {
		case side.m.RunnerDigest != baseline.Plan.RunnerDigest || side.m.RunnerRevision != baseline.Plan.RunnerRevision:
			return fmt.Errorf("%s series runner digest/revision drifts from the shared plan", side.name)
		case side.m.JudgeRuleDigest != baseline.Plan.JudgeRuleDigest:
			return fmt.Errorf("%s series judge rule digest drifts from the shared plan", side.name)
		case side.m.TimeoutSeconds != baseline.Plan.TimeoutSeconds:
			return fmt.Errorf("%s series timeout %d != the sealed plan %d", side.name, side.m.TimeoutSeconds, baseline.Plan.TimeoutSeconds)
		case side.m.Concurrency != baseline.Plan.Concurrency:
			return fmt.Errorf("%s series concurrency %d != the sealed plan %d", side.name, side.m.Concurrency, baseline.Plan.Concurrency)
		case side.m.ExecutionEnvironmentDigest != baseline.Plan.NormalizedCoreExecutionTemplateDigest:
			return fmt.Errorf("%s series execution environment %q != the plan's normalized core execution template %q",
				side.name, side.m.ExecutionEnvironmentDigest, baseline.Plan.NormalizedCoreExecutionTemplateDigest)
		case flyBoundaryTemplate(baseline.Plan.CoreBoundaryKind, side.m.ToolConfigurationDigest) != baseline.Plan.NormalizedCoreBoundaryTemplateDigest:
			return fmt.Errorf("%s series boundary template drifts from the shared plan", side.name)
		}
		if len(side.m.CaseOrderSeeds) != len(baseline.Plan.CaseOrderSeeds) {
			return fmt.Errorf("%s series carries %d case-order seeds, the plan froze %d",
				side.name, len(side.m.CaseOrderSeeds), len(baseline.Plan.CaseOrderSeeds))
		}
		for o, seed := range baseline.Plan.CaseOrderSeeds {
			if side.m.CaseOrderSeeds[o] != seed {
				return fmt.Errorf("%s series case-order seed for ordinal %d drifts from the shared plan", side.name, o)
			}
		}
	}
	if baseline.Manifest.SkillDigest == candidate.Manifest.SkillDigest {
		return fmt.Errorf("baseline and candidate carry the same skill digest %q: the skill package digest is the single intentional SC-5 variable, so this is not a flywheel round",
			baseline.Manifest.SkillDigest)
	}
	return nil
}

// flyBoundaryTemplate recomputes the normalized boundary-template digest the
// same way `series prepare` derives it, so a compare can re-check that both
// legs ran under the boundary the shared plan froze.
func flyBoundaryTemplate(kind BoundaryKind, isolationConfigDigest string) string {
	return sha256Hex([]byte("boundary-template\x00" + string(kind) + "\x00" + isolationConfigDigest))
}

// flyArchiveMatchesBaseline proves the sealed archive is the frozen truth of
// exactly this baseline: its entries must be exactly the median-fail cells
// the baseline root still rejudges to, so a stale, partial or foreign archive
// can never authorize a comparison.
func flyArchiveMatchesBaseline(archive *FailureArchive, baseline *flySeries) error {
	failing := map[string]bool{}
	for _, key := range baseline.sortedCells() {
		if !baseline.Cells[key].Median {
			failing[key.String()] = true
		}
	}
	seen := map[string]bool{}
	for _, e := range archive.Entries {
		key := flyCellKey{Host: e.Host, CaseID: e.CaseID}.String()
		if !failing[key] {
			return fmt.Errorf("archive entry %s is not a median-fail cell of baseline series %s", key, baseline.Manifest.SeriesID)
		}
		if seen[key] {
			return fmt.Errorf("archive repeats median-fail cell %s", key)
		}
		seen[key] = true
	}
	var missing []string
	for key := range failing {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("the failure archive is not the frozen truth of baseline series %s: missing median-fail cell(s) %s",
			baseline.Manifest.SeriesID, strings.Join(missing, ", "))
	}
	return nil
}

// flyVerifyExtensionBackfill enforces the closed one-to-one backfill rule:
// every emitted source ID maps to exactly one successor that is a member of
// the extension manifest, with no missing, duplicated, extra or wrong-source
// mapping, and the receipt itself stays a score-ineligible dev diagnostic.
func flyVerifyExtensionBackfill(receiptPath string, required []string) (*FlywheelBackfillRecord, *FlywheelExtensionDiagnostics, error) {
	b, err := os.ReadFile(receiptPath)
	if err != nil {
		return nil, nil, fmt.Errorf("extension receipt %s: %w", receiptPath, err)
	}
	receipt := &DiagnosticRunReceipt{}
	if err := StrictParseClosed(b, receipt); err != nil {
		return nil, nil, fmt.Errorf("extension receipt %s: %w", receiptPath, err)
	}
	if receipt.Mode != "diagnostic" {
		return nil, nil, fmt.Errorf("extension receipt mode %q is not diagnostic: extension evidence is permanently score-ineligible", receipt.Mode)
	}
	if receipt.Split != SplitDevRegression {
		return nil, nil, fmt.Errorf("extension receipt split %q is not %q: holdout material never enters the dev flywheel",
			receipt.Split, SplitDevRegression)
	}
	if receipt.FormalScoreEligible {
		return nil, nil, errors.New("extension receipt claims formal score eligibility: extension results stay separate from every score family")
	}
	if strings.TrimSpace(receipt.ManifestPath) == "" {
		return nil, nil, errors.New("extension receipt names no extension manifest")
	}
	manifestPath := receipt.ManifestPath
	if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(filepath.Dir(receiptPath), manifestPath)
	}
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("extension manifest %s: %w", manifestPath, err)
	}
	manifest := &DatasetManifestV2{}
	if err := StrictParseClosed(mb, manifest); err != nil {
		return nil, nil, fmt.Errorf("extension manifest %s: %w", manifestPath, err)
	}
	if manifest.ScoreMembership != MembershipDevExt || manifest.Split != SplitDevRegression {
		return nil, nil, fmt.Errorf("extension manifest is %s/%s, want %s/%s",
			manifest.Split, manifest.ScoreMembership, SplitDevRegression, MembershipDevExt)
	}
	membership := map[string]bool{}
	for _, id := range manifest.CaseIDs {
		membership[id] = true
	}
	lineage := manifest.ExtensionLineage
	seenSuccessor := map[string]string{}
	for source, successor := range lineage {
		if !membership[successor] {
			return nil, nil, fmt.Errorf("extension lineage maps %s to %q, which is not a member of the extension manifest", source, successor)
		}
		if prev, dup := seenSuccessor[successor]; dup {
			return nil, nil, fmt.Errorf("extension lineage maps both %s and %s to the single successor %q: the backfill must be one-to-one",
				prev, source, successor)
		}
		seenSuccessor[successor] = source
	}
	requiredSet := map[string]bool{}
	for _, id := range required {
		requiredSet[id] = true
		if lineage[id] == "" {
			return nil, nil, fmt.Errorf("extension lineage carries no successor for required fail-to-pass source %s", id)
		}
	}
	var extra []string
	for source := range lineage {
		if !requiredSet[source] {
			extra = append(extra, source)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, nil, fmt.Errorf("extension lineage carries mapping(s) outside the required backfill set: %s", strings.Join(extra, ", "))
	}
	verified := &FlywheelBackfillRecord{
		ExtensionManifestDigest: sha256Hex(mb),
		ExtensionCaseCount:      len(manifest.CaseIDs),
		Lineage:                 copyStringMap(lineage),
		Verified:                true,
	}
	lineageDigest, err := CanonicalSHA256(struct {
		Algorithm string            `json:"algorithm"`
		Lineage   map[string]string `json:"lineage"`
	}{Algorithm: flywheelLineageAlgo, Lineage: verified.Lineage})
	if err != nil {
		return nil, nil, err
	}
	verified.LineageDigest = lineageDigest
	diagnostics := &FlywheelExtensionDiagnostics{
		ReceiptDigest: sha256Hex(b),
		NonComparable: true,
		NonGating:     true,
		Note:          flywheelExtensionMethod,
	}
	return verified, diagnostics, nil
}

// ---------- comparison sealing ----------

type flywheelDigestPreimage struct {
	SchemaVersion                          int                           `json:"schema_version"`
	BaselineSeriesID                       string                        `json:"baseline_series_id"`
	BaselineSeriesManifestDigest           string                        `json:"baseline_series_manifest_digest"`
	BaselineSkillSnapshotDigest            string                        `json:"baseline_skill_snapshot_digest"`
	BaselineSkillDigest                    string                        `json:"baseline_skill_digest"`
	CandidateSeriesID                      string                        `json:"candidate_series_id"`
	CandidateSeriesManifestDigest          string                        `json:"candidate_series_manifest_digest"`
	CandidateSkillSnapshotDigest           string                        `json:"candidate_skill_snapshot_digest"`
	CandidateSkillDigest                   string                        `json:"candidate_skill_digest"`
	CoreExecutionPlanDigest                string                        `json:"core_execution_plan_digest"`
	CoreManifestDigest                     string                        `json:"core_manifest_digest"`
	ToolIdentityDigests                    map[string]string             `json:"tool_identity_digests"`
	ComparedCoreCaseCount                  int                           `json:"compared_core_case_count"`
	FailToPassCount                        int                           `json:"fail_to_pass_count"`
	RegressionCount                        int                           `json:"regression_count"`
	StablePassCount                        int                           `json:"stable_pass_count"`
	StableFailCount                        int                           `json:"stable_fail_count"`
	FailToPassCases                        []FlywheelCellRef             `json:"fail_to_pass_cases"`
	RegressionCases                        []FlywheelCellRef             `json:"regression_cases"`
	CaseMedians                            []FlywheelCaseMedian          `json:"case_medians"`
	RequiredExtensionBackfillSourceCaseIDs []string                      `json:"required_extension_backfill_source_case_ids"`
	RequiredExtensionBackfillDigest        string                        `json:"required_extension_backfill_digest"`
	FailureArchiveDigest                   string                        `json:"failure_archive_digest"`
	ExtensionBackfill                      *FlywheelBackfillRecord       `json:"extension_backfill"`
	ExtensionDiagnostics                   *FlywheelExtensionDiagnostics `json:"extension_diagnostics"`
	ExtensionSeparateFromCore              bool                          `json:"extension_separate_from_core"`
	SC5Verdict                             string                        `json:"sc5_verdict"`
	ReportDigest                           string                        `json:"report_digest"`
}

type flywheelSealPreimage struct {
	SchemaVersion int    `json:"schema_version"`
	ReportDigest  string `json:"report_digest"`
	SealedBy      string `json:"sealed_by"`
}

func flywheelDigestProjection(r *FlywheelComparisonReceipt) *flywheelDigestPreimage {
	return &flywheelDigestPreimage{
		SchemaVersion:                          r.SchemaVersion,
		BaselineSeriesID:                       r.BaselineSeriesID,
		BaselineSeriesManifestDigest:           r.BaselineSeriesManifestDigest,
		BaselineSkillSnapshotDigest:            r.BaselineSkillSnapshotDigest,
		BaselineSkillDigest:                    r.BaselineSkillDigest,
		CandidateSeriesID:                      r.CandidateSeriesID,
		CandidateSeriesManifestDigest:          r.CandidateSeriesManifestDigest,
		CandidateSkillSnapshotDigest:           r.CandidateSkillSnapshotDigest,
		CandidateSkillDigest:                   r.CandidateSkillDigest,
		CoreExecutionPlanDigest:                r.CoreExecutionPlanDigest,
		CoreManifestDigest:                     r.CoreManifestDigest,
		ToolIdentityDigests:                    r.ToolIdentityDigests,
		ComparedCoreCaseCount:                  r.ComparedCoreCaseCount,
		FailToPassCount:                        r.FailToPassCount,
		RegressionCount:                        r.RegressionCount,
		StablePassCount:                        r.StablePassCount,
		StableFailCount:                        r.StableFailCount,
		FailToPassCases:                        r.FailToPassCases,
		RegressionCases:                        r.RegressionCases,
		CaseMedians:                            r.CaseMedians,
		RequiredExtensionBackfillSourceCaseIDs: r.RequiredExtensionBackfillSourceCaseIDs,
		RequiredExtensionBackfillDigest:        r.RequiredExtensionBackfillDigest,
		FailureArchiveDigest:                   r.FailureArchiveDigest,
		ExtensionBackfill:                      r.ExtensionBackfill,
		ExtensionDiagnostics:                   r.ExtensionDiagnostics,
		ExtensionSeparateFromCore:              r.ExtensionSeparateFromCore,
		SC5Verdict:                             r.SC5Verdict,
		ReportDigest:                           r.ReportDigest,
	}
}

// sealFlywheelComparison is the freeze-before-digest seal of the comparison
// receipt: every field is populated first, then report_digest is derived over
// the digest-less receipt and seal_digest over that digest alone.
func sealFlywheelComparison(r *FlywheelComparisonReceipt) error {
	if r == nil {
		return errors.New("nil flywheel comparison receipt")
	}
	if r.ReportDigest != "" || r.SealDigest != "" {
		return errors.New("freeze-before-digest violated: comparison already carries a digest")
	}
	if r.SchemaVersion != 1 {
		return fmt.Errorf("comparison schema_version %d, want 1", r.SchemaVersion)
	}
	switch r.SC5Verdict {
	case "pass", "fail":
	default:
		return fmt.Errorf("sc5_verdict %q is outside the closed pass/fail set", r.SC5Verdict)
	}
	if !r.ExtensionSeparateFromCore {
		return errors.New("extension results must be recorded as separate from the core172 denominator")
	}
	if r.FailToPassCount != len(r.FailToPassCases) || r.RegressionCount != len(r.RegressionCases) {
		return errors.New("transition counts disagree with the named cells")
	}
	if r.FailToPassCount+r.RegressionCount+r.StablePassCount+r.StableFailCount != len(r.CaseMedians) {
		return errors.New("the four transitions must exactly partition the compared cells")
	}
	if r.RequiredExtensionBackfillDigest == "" || r.FailureArchiveDigest == "" {
		return errors.New("comparison does not bind its backfill set and failure archive")
	}
	d, err := CanonicalSHA256(flywheelDigestProjection(r))
	if err != nil {
		return err
	}
	r.ReportDigest = d
	seal, err := CanonicalSHA256(flywheelSealPreimage{
		SchemaVersion: 1, ReportDigest: d, SealedBy: flywheelSealedBy,
	})
	if err != nil {
		return err
	}
	r.SealDigest = seal
	return nil
}

// VerifyFlywheelComparisonSeal re-derives both digests of a comparison read
// back from disk.
func VerifyFlywheelComparisonSeal(r *FlywheelComparisonReceipt) error {
	if r == nil || r.ReportDigest == "" || r.SealDigest == "" {
		return errors.New("flywheel comparison receipt is not sealed")
	}
	pre := flywheelDigestProjection(r)
	pre.ReportDigest = ""
	d, err := CanonicalSHA256(pre)
	if err != nil {
		return err
	}
	if d != r.ReportDigest {
		return errors.New("flywheel comparison digest mismatch (post-seal mutation)")
	}
	seal, err := CanonicalSHA256(flywheelSealPreimage{
		SchemaVersion: 1, ReportDigest: r.ReportDigest, SealedBy: flywheelSealedBy,
	})
	if err != nil {
		return err
	}
	if seal != r.SealDigest {
		return errors.New("flywheel comparison seal mismatch (post-seal mutation)")
	}
	return nil
}

// WriteFlywheelComparison materializes the receipt as an immutable frozen
// file. It is the only writer of this receipt and never writes either
// official score family.
func WriteFlywheelComparison(path string, r *FlywheelComparisonReceipt) error {
	if err := VerifyFlywheelComparisonSeal(r); err != nil {
		return fmt.Errorf("refusing to write an unverifiable comparison receipt: %w", err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return WriteFrozenFile(path, b)
}
