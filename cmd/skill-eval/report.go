package main

// 048 US4 (T050): the official-score wiring. series.go owns the pure scorer
// (T044); this file owns the only two IO entries that may feed it and the
// only writer of an OfficialScoreReport. Everything here fails closed: a
// missing file, an unverifiable digest, a verdict that cannot be re-derived
// from its artifacts, or a dev-comparison series all stop the score instead
// of degrading it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Frozen series-root layout. A prepared series root is self-contained: the
// receipts it cites live inside it. The run tree is exactly the one the
// primary runner (T047/T048) writes — PrimaryRunRoot plus its per-case
// directories — so the score never guesses where a receipt landed.
//
//	<seriesRoot>/
//	  series-manifest.json                sealed FormalSeriesManifest
//	  core-plan.json                      sealed CoreExecutionPlanReceipt
//	  protected-execution.json            sealed ProtectedExecutionReceipt
//	  package-validation.json             SkillPackageValidationReceipt
//	  green-series-prepare.json           GreenTestReceipt (series-prepare)
//	  green-pre-holdout.json              GreenTestReceipt (pre-holdout)
//	  holdout-binding.json                HoldoutBindingReceipt
//	  canaries/<host>/slot-<n>.json       WorkspaceCanaryReceipt per prepared slot
//	  runs/<host>-<split>-o<n>/manifest.json      sealed PrimaryRunManifest
//	  runs/<host>-<split>-o<n>/cases/<case>/case-receipt.json   CaseRunReceipt
//	  runs/.../cases/<case>/{normalized-events.json,raw.jsonl,store-dump.txt}
//	  datasets/<membership>/manifest.json sealed DatasetManifestV2
//	  datasets/<membership>/<payload files>  case payloads named by that manifest
//	  official-score.json                 sealed OfficialScoreReport (written here)
const (
	seriesManifestFile      = "series-manifest.json"
	corePlanFile            = "core-plan.json"
	protectedExecutionFile  = "protected-execution.json"
	packageValidationFile   = "package-validation.json"
	greenSeriesPrepareFile  = "green-series-prepare.json"
	greenPreHoldoutFile     = "green-pre-holdout.json"
	holdoutBindingFile      = "holdout-binding.json"
	officialScoreReportFile = "official-score.json"

	runsDir     = "runs"
	canariesDir = "canaries"
	datasetsDir = "datasets"

	datasetManifestName = "manifest.json"
	runManifestName     = "manifest.json"
	caseReceiptName     = "case-receipt.json"
	casesDirName        = "cases"

	canaryReceiptNameFmt = "slot-%d.json"

	officialScoreSealedBy = "skill-eval-048-controller"
)

// LoadScoreInputs reads the complete evidence bundle of a prepared series
// root from disk. Every file the bundle needs must be present and parse under
// the closed-schema rules; a missing or mutated receipt is an error, never a
// silently skipped check. Structural digest verification stays in
// ValidateOfficialScoreEligibility, which every scorer must run next.
func LoadScoreInputs(seriesRoot string) (*ScoreEligibilityInput, error) {
	if strings.TrimSpace(seriesRoot) == "" {
		return nil, errors.New("series root is empty")
	}
	if fi, err := os.Stat(seriesRoot); err != nil {
		return nil, fmt.Errorf("series root %s: %w", seriesRoot, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("series root %s is not a directory", seriesRoot)
	}

	manifest := &FormalSeriesManifest{}
	if err := loadStrictFile(filepath.Join(seriesRoot, seriesManifestFile), manifest); err != nil {
		return nil, err
	}
	plan := &CoreExecutionPlanReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, corePlanFile), plan); err != nil {
		return nil, err
	}
	protected := &ProtectedExecutionReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, protectedExecutionFile), protected); err != nil {
		return nil, err
	}
	pkg := &SkillPackageValidationReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, packageValidationFile), pkg); err != nil {
		return nil, err
	}
	prepare := &GreenTestReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, greenSeriesPrepareFile), prepare); err != nil {
		return nil, err
	}
	preHoldout := &GreenTestReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, greenPreHoldoutFile), preHoldout); err != nil {
		return nil, err
	}
	binding := &HoldoutBindingReceipt{}
	if err := loadStrictFile(filepath.Join(seriesRoot, holdoutBindingFile), binding); err != nil {
		return nil, err
	}
	canaries, err := loadScoreCanaries(seriesRoot, manifest)
	if err != nil {
		return nil, err
	}
	runs, err := loadScoreRuns(seriesRoot, manifest)
	if err != nil {
		return nil, err
	}
	return &ScoreEligibilityInput{
		Manifest:      manifest,
		Plan:          plan,
		Protected:     protected,
		Canaries:      canaries,
		Package:       pkg,
		SeriesPrepare: prepare,
		PreHoldout:    preHoldout,
		Runs:          runs,
		Binding:       binding,
	}, nil
}

// loadStrictFile reads path and parses it into v under the closed-schema
// rules. A missing file is reported as such: the scorer must know that a
// receipt is absent, not merely that it was unreadable.
func loadStrictFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("prepared series root is missing %s (fail-closed)", path)
		}
		return err
	}
	if err := StrictParseClosed(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// loadScoreRuns reads the sealed primary matrix: one run root per host ×
// split × ordinal, each carrying its own manifest.json. A run manifest
// outside that matrix must never be silently ignored, so an unexpected one
// rejects the load.
func loadScoreRuns(seriesRoot string, m *FormalSeriesManifest) (map[RunKey]*PrimaryRunManifest, error) {
	runs := map[RunKey]*PrimaryRunManifest{}
	expected := map[string]bool{}
	for _, h := range m.Hosts {
		for _, split := range []string{SplitDevRegression, SplitHoldout} {
			for _, o := range Ordinals {
				rel, err := filepath.Rel(seriesRoot, PrimaryRunRoot(seriesRoot, h, split, o))
				if err != nil {
					return nil, err
				}
				expected[filepath.Join(rel, runManifestName)] = true
				r := &PrimaryRunManifest{}
				if err := loadStrictFile(filepath.Join(PrimaryRunRoot(seriesRoot, h, split, o), runManifestName), r); err != nil {
					return nil, err
				}
				runs[RunKey{h, split, o}] = r
			}
		}
	}
	root := filepath.Join(seriesRoot, runsDir)
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("prepared series root is missing the %s directory (fail-closed)", runsDir)
	}
	return runs, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// A run manifest sits directly inside its run root; everything deeper
		// (case receipts, artifacts) is read by the per-case pass.
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 2 && parts[1] == runManifestName && !expected[filepath.Join(runsDir, rel)] {
			return fmt.Errorf("unexpected primary run manifest %s outside the sealed host x split x ordinal matrix", rel)
		}
		return nil
	})
}

// loadScoreCanaries reads one workspace-canary receipt per prepared host ×
// worker slot the manifest binds. Slots come from the manifest's own map, so
// an out-of-range slot is caught by the eligibility validator.
func loadScoreCanaries(seriesRoot string, m *FormalSeriesManifest) (map[string]map[int]*WorkspaceCanaryReceipt, error) {
	out := map[string]map[int]*WorkspaceCanaryReceipt{}
	for h, slots := range m.WorkspaceCanaryReceiptDigests {
		out[h] = map[int]*WorkspaceCanaryReceipt{}
		for slot := range slots {
			c := &WorkspaceCanaryReceipt{}
			rel := filepath.Join(canariesDir, h, fmt.Sprintf(canaryReceiptNameFmt, slot))
			if err := loadStrictFile(filepath.Join(seriesRoot, rel), c); err != nil {
				return nil, err
			}
			out[h][slot] = c
		}
	}
	return out, nil
}

// RunOfficialScore produces the only official score of a series: load the
// bundle, prove eligibility, re-derive every case verdict from its recorded
// artifacts, score, seal and write the frozen report. A dev-comparison series
// has no official score by definition and is refused here as well as in the
// eligibility validator.
func RunOfficialScore(seriesRoot string) (*OfficialScoreReport, error) {
	in, err := LoadScoreInputs(seriesRoot)
	if err != nil {
		return nil, err
	}
	if in.Manifest.Purpose != PurposeOfficialDual {
		return nil, fmt.Errorf("purpose %q can never produce an official score (it is core-only diagnostic evidence)", in.Manifest.Purpose)
	}
	if err := ValidateOfficialScoreEligibility(in); err != nil {
		return nil, fmt.Errorf("score eligibility: %w", err)
	}

	datasets, err := loadScoreDatasets(seriesRoot, in.Manifest)
	if err != nil {
		return nil, err
	}
	specs, err := scoreSpecsFromDatasets(datasets)
	if err != nil {
		return nil, err
	}
	if err := checkRunCaseSets(in, datasets); err != nil {
		return nil, err
	}
	states, err := scoreStatesFromReceipts(seriesRoot, in, datasets)
	if err != nil {
		return nil, err
	}

	m, err := ComputeOfficialScore(in.Manifest.SeriesID, in.Manifest.Hosts, specs, states)
	if err != nil {
		return nil, err
	}
	// Cases no gate of their split reads (the dev split carries the trap
	// cases; only the holdout split gates the trap pair) stay out of every
	// gate denominator by construction — ComputeOfficialScore reports them in
	// ScoreMatrix.Unrouted instead of folding them anywhere.

	report := BuildOfficialScoreReport(in, m)
	// Freeze-before-digest: every public field is populated by the builder and
	// bound to the bundle above, so the seal can be derived now. Nothing is
	// written until the sealed report has passed validation, so a rejected
	// report never becomes an artifact.
	if err := sealOfficialScoreReport(report); err != nil {
		return nil, err
	}
	if err := ValidateOfficialScoreReport(report, in, m); err != nil {
		return nil, fmt.Errorf("sealed report: %w", err)
	}
	if err := VerifyOfficialScoreSeal(report); err != nil {
		return nil, err
	}
	// The on-disk form is plain encoding/json: the report carries
	// worker-slot-keyed maps that canonical JSON cannot express, and the
	// integrity anchor is the seal over the projection preimage above, not the
	// file bytes. encoding/json is deterministic here (sorted map keys, fixed
	// struct order), so the artifact is byte-stable.
	b, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if err := WriteFrozenFile(filepath.Join(seriesRoot, officialScoreReportFile), b); err != nil {
		return nil, err
	}
	return report, nil
}

// officialScoreSealPreimage is the seal's exact preimage: it binds nothing
// but the report digest, so it cannot smuggle in a field the report digest
// does not already cover.
type officialScoreSealPreimage struct {
	SchemaVersion int    `json:"schema_version"`
	ReportDigest  string `json:"report_digest"`
	SealedBy      string `json:"sealed_by"`
}

// officialScoreDigestPreimage is the report's digest preimage: the report
// itself, minus both digests. Canonical JSON only admits string map keys, so
// the worker-slot keys of the canary map are projected in their shortest
// decimal form ("1","2",...) — the same rule CandidateBindingDigest applies
// to case_order_seeds.
type officialScoreDigestPreimage struct {
	SeriesID                            string                       `json:"series_id"`
	SeriesManifestDigest                string                       `json:"series_manifest_digest"`
	CoreExecutionPlanDigest             string                       `json:"core_execution_plan_digest"`
	ProtectedExecutionReceiptDigest     string                       `json:"protected_execution_receipt_digest"`
	WorkspaceCanaryReceiptDigests       map[string]map[string]string `json:"workspace_canary_receipt_digests"`
	SkillPackageValidationReceiptDigest string                       `json:"skill_package_validation_receipt_digest"`
	SkillSnapshotDigest                 string                       `json:"skill_snapshot_digest"`
	SkillSnapshotAnchorDigest           string                       `json:"skill_snapshot_anchor_digest"`
	CandidateBindingDigest              string                       `json:"candidate_binding_digest"`
	HoldoutBindingReceiptDigest         string                       `json:"holdout_binding_receipt_digest"`
	GreenTestReceiptDigests             GreenTestDigestPair          `json:"green_test_receipt_digests"`
	DevRegression                       []HostScore                  `json:"dev_regression"`
	Generalization                      []HostScore                  `json:"generalization"`
	SupplementalCrossHost               *SupplementalCrossHost       `json:"supplemental_cross_host,omitempty"`
	OverallVerdict                      string                       `json:"overall_verdict"`
	DiagnosticArtifactsUsed             bool                         `json:"diagnostic_artifacts_used"`
	BiasDiagnostics                     []BiasDiagnosticCell         `json:"bias_diagnostics"`
	ReportDigest                        string                       `json:"report_digest"`
}

// reportDigestPreimage projects a report onto its digest preimage.
func reportDigestPreimage(r *OfficialScoreReport) *officialScoreDigestPreimage {
	canaries := map[string]map[string]string{}
	for h, slots := range r.WorkspaceCanaryReceiptDigests {
		m := make(map[string]string, len(slots))
		for slot, digest := range slots {
			m[itoa(slot)] = digest
		}
		canaries[h] = m
	}
	return &officialScoreDigestPreimage{
		SeriesID:                            r.SeriesID,
		SeriesManifestDigest:                r.SeriesManifestDigest,
		CoreExecutionPlanDigest:             r.CoreExecutionPlanDigest,
		ProtectedExecutionReceiptDigest:     r.ProtectedExecutionReceiptDigest,
		WorkspaceCanaryReceiptDigests:       canaries,
		SkillPackageValidationReceiptDigest: r.SkillPackageValidationReceiptDigest,
		SkillSnapshotDigest:                 r.SkillSnapshotDigest,
		SkillSnapshotAnchorDigest:           r.SkillSnapshotAnchorDigest,
		CandidateBindingDigest:              r.CandidateBindingDigest,
		HoldoutBindingReceiptDigest:         r.HoldoutBindingReceiptDigest,
		GreenTestReceiptDigests:             r.GreenTestReceiptDigests,
		DevRegression:                       r.DevRegression,
		Generalization:                      r.Generalization,
		SupplementalCrossHost:               r.SupplementalCrossHost,
		OverallVerdict:                      r.OverallVerdict,
		DiagnosticArtifactsUsed:             r.DiagnosticArtifactsUsed,
		BiasDiagnostics:                     r.BiasDiagnostics,
		ReportDigest:                        r.ReportDigest,
	}
}

// sealOfficialScoreReport is the freeze-before-digest seal: every public
// field is populated and validated first, then report_digest is derived over
// the digest-less report and seal_digest over that digest alone.
func sealOfficialScoreReport(r *OfficialScoreReport) error {
	if r.ReportDigest != "" || r.SealDigest != "" {
		return errors.New("freeze-before-digest violated: report already carries a digest")
	}
	d, err := CanonicalSHA256(reportDigestPreimage(r))
	if err != nil {
		return err
	}
	r.ReportDigest = d
	seal, err := CanonicalSHA256(officialScoreSealPreimage{
		SchemaVersion: 1, ReportDigest: d, SealedBy: officialScoreSealedBy,
	})
	if err != nil {
		return err
	}
	r.SealDigest = seal
	return nil
}

// VerifyOfficialScoreSeal re-derives both digests of a report read back from
// disk. Any post-seal mutation — a score, a gate or a binding — drifts at
// least one of them.
func VerifyOfficialScoreSeal(r *OfficialScoreReport) error {
	if r == nil || r.ReportDigest == "" || r.SealDigest == "" {
		return errors.New("official score report is not sealed")
	}
	pre := reportDigestPreimage(r)
	// The recorded digest was computed over the digest-less report, so the
	// verification preimage must be digest-less too.
	pre.ReportDigest = ""
	d, err := CanonicalSHA256(pre)
	if err != nil {
		return err
	}
	if d != r.ReportDigest {
		return errors.New("official score report digest mismatch (post-seal mutation)")
	}
	seal, err := CanonicalSHA256(officialScoreSealPreimage{
		SchemaVersion: 1, ReportDigest: r.ReportDigest, SealedBy: officialScoreSealedBy,
	})
	if err != nil {
		return err
	}
	if seal != r.SealDigest {
		return errors.New("official score report seal mismatch (post-seal mutation)")
	}
	return nil
}

// loadScoreDatasets reads the two sealed datasets the series manifest binds,
// one directory per membership, and returns the frozen cases by split.
func loadScoreDatasets(seriesRoot string, m *FormalSeriesManifest) (map[string]map[string]*TriggerCaseV2, error) {
	out := map[string]map[string]*TriggerCaseV2{}
	for _, split := range []string{SplitDevRegression, SplitHoldout} {
		membership, err := MembershipOfSplit(split)
		if err != nil {
			return nil, err
		}
		bound := m.DatasetManifests[membership]
		if bound == "" {
			return nil, fmt.Errorf("series manifest binds no %s dataset manifest", membership)
		}
		cases, err := loadScoreDataset(filepath.Join(seriesRoot, datasetsDir, membership), split, membership, bound)
		if err != nil {
			return nil, err
		}
		out[split] = cases
	}
	return out, nil
}

// loadScoreDataset loads one sealed split dataset: the manifest must be
// sealed, bound to exactly the digest the series manifest names, and its
// payload files must re-hash to the sealed payload digest. Only files the
// manifest names contribute; extra files in the directory are invisible.
func loadScoreDataset(dir, split, membership, boundDigest string) (map[string]*TriggerCaseV2, error) {
	manifest := &DatasetManifestV2{}
	if err := loadStrictFile(filepath.Join(dir, datasetManifestName), manifest); err != nil {
		return nil, err
	}
	if manifest.Split != split {
		return nil, fmt.Errorf("%s dataset manifest split %q, want %q", membership, manifest.Split, split)
	}
	if manifest.ScoreMembership != membership {
		return nil, fmt.Errorf("%s dataset manifest membership %q, want %q", membership, manifest.ScoreMembership, membership)
	}
	if manifest.Seal == nil {
		return nil, fmt.Errorf("%s dataset manifest is not sealed", membership)
	}
	if manifest.Seal.ManifestDigest != boundDigest {
		return nil, fmt.Errorf("%s dataset manifest digest %q != series bound %q", membership, manifest.Seal.ManifestDigest, boundDigest)
	}
	if err := VerifyDatasetSeal(manifest, dir); err != nil {
		return nil, fmt.Errorf("%s dataset seal: %w", membership, err)
	}
	cases := map[string]*TriggerCaseV2{}
	for _, pf := range manifest.PayloadFiles {
		if !safeRelativePath(pf.RelativePath) {
			return nil, fmt.Errorf("payload path %q is not containment-safe", pf.RelativePath)
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pf.RelativePath)))
		if err != nil {
			return nil, fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		d, err := LFNormalizedSHA256(b)
		if err != nil {
			return nil, fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		if d != pf.LFNormalizedSHA256 {
			return nil, fmt.Errorf("payload file %s digest mismatch: manifest %s != actual %s", pf.RelativePath, pf.LFNormalizedSHA256, d)
		}
		payload := &CasePayloadFile{}
		if err := StrictParseClosed(b, payload); err != nil {
			return nil, fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		for i := range payload.Cases {
			c := &payload.Cases[i]
			if err := ValidateCaseV2(c); err != nil {
				return nil, fmt.Errorf("payload file %s case %s: %w", pf.RelativePath, c.ID, err)
			}
			if c.Split != split || c.ScoreMembership != membership {
				return nil, fmt.Errorf("payload case %s is %s/%s, want %s/%s", c.ID, c.Split, c.ScoreMembership, split, membership)
			}
			if _, dup := cases[c.ID]; dup {
				return nil, fmt.Errorf("duplicate case id %s across payload files", c.ID)
			}
			cases[c.ID] = c
		}
	}
	if len(cases) != len(manifest.CaseIDs) {
		return nil, fmt.Errorf("%s payload files carry %d cases, manifest lists %d", membership, len(cases), len(manifest.CaseIDs))
	}
	for _, id := range manifest.CaseIDs {
		if _, ok := cases[id]; !ok {
			return nil, fmt.Errorf("%s manifest case id %s resolved to no payload case", membership, id)
		}
	}
	return cases, nil
}

// scoreSpecsFromDatasets projects the frozen case identity the scorer reads.
// Module and expected polarity come from the sealed dataset, never from the
// run, so a run cannot re-label a case to steer a gate.
func scoreSpecsFromDatasets(datasets map[string]map[string]*TriggerCaseV2) ([]ScoreCaseSpec, error) {
	var specs []ScoreCaseSpec
	for _, split := range []string{SplitDevRegression, SplitHoldout} {
		for _, c := range datasets[split] {
			specs = append(specs, ScoreCaseSpec{CaseID: c.ID, Split: split, Module: c.Module, Trigger: c.Expect.Trigger})
		}
	}
	if len(specs) == 0 {
		return nil, errors.New("the bound datasets carry no scoreable case")
	}
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Split != specs[j].Split {
			return specs[i].Split < specs[j].Split
		}
		return specs[i].CaseID < specs[j].CaseID
	})
	return specs, nil
}

// checkRunCaseSets requires every primary run to carry exactly the sealed
// dataset's case set of its split: a run cannot widen, shrink or swap it.
func checkRunCaseSets(in *ScoreEligibilityInput, datasets map[string]map[string]*TriggerCaseV2) error {
	for _, key := range sortedRunKeys(in.Runs) {
		run := in.Runs[key]
		want := datasets[key.Split]
		seen := map[string]bool{}
		for _, id := range run.CaseIDs {
			if _, ok := want[id]; !ok {
				return fmt.Errorf("primary run %s/%s/%d carries case %s, which is not in the sealed %s case set",
					key.Host, key.Split, key.Ordinal, id, key.Split)
			}
			if seen[id] {
				return fmt.Errorf("primary run %s/%s/%d repeats case %s", key.Host, key.Split, key.Ordinal, id)
			}
			seen[id] = true
		}
		if len(seen) != len(want) {
			return fmt.Errorf("primary run %s/%s/%d carries %d distinct cases, the sealed %s dataset has %d",
				key.Host, key.Split, key.Ordinal, len(seen), key.Split, len(want))
		}
	}
	return nil
}

// scoreStatesFromReceipts reads every primary case receipt of the sealed
// matrix, verifies its three artifacts byte-for-byte, re-judges the verdict
// from them, and turns the result into terminal score states. A verdict that
// cannot be re-derived fails the score: the report never inherits a verdict
// the current judge did not reproduce.
func scoreStatesFromReceipts(seriesRoot string, in *ScoreEligibilityInput, datasets map[string]map[string]*TriggerCaseV2) ([]CaseScoreState, error) {
	var states []CaseScoreState
	for _, key := range sortedRunKeys(in.Runs) {
		run := in.Runs[key]
		ids := append([]string(nil), run.CaseIDs...)
		sort.Strings(ids)
		for _, caseID := range ids {
			receipt, err := loadCaseRunReceipt(seriesRoot, key, caseID)
			if err != nil {
				return nil, err
			}
			if err := validateCaseRunReceiptOnDisk(receipt, caseID); err != nil {
				return nil, err
			}
			normalized, err := readScoreArtifact(seriesRoot, receipt.NormalizedEventsPath, receipt.NormalizedEventsDigest)
			if err != nil {
				return nil, err
			}
			// The raw stream is not judged, but its digest is still verified:
			// a receipt whose raw evidence drifted is not the receipt that ran.
			if _, err := readScoreArtifact(seriesRoot, receipt.RawEventsPath, receipt.RawEventsDigest); err != nil {
				return nil, err
			}
			dump, err := readScoreArtifact(seriesRoot, receipt.StoreDumpPath, receipt.StoreDumpDigest)
			if err != nil {
				return nil, err
			}
			rejudged, err := RejudgeFromRecordedCase(receipt.Status, normalized, dump, datasets[key.Split][caseID])
			if err != nil {
				return nil, fmt.Errorf("primary run %s/%s/%d case %s: %w", key.Host, key.Split, key.Ordinal, caseID, err)
			}
			if err := verdictAgrees(caseID, receipt.Verdict, rejudged); err != nil {
				return nil, fmt.Errorf("primary run %s/%s/%d: %w", key.Host, key.Split, key.Ordinal, err)
			}
			outcome := CaseOutcomeFail
			switch receipt.Status {
			case CaseStatusPass:
				outcome = CaseOutcomePass
			case CaseStatusRunnerError:
				outcome = CaseOutcomeRunnerError
			case CaseStatusFail:
				outcome = CaseOutcomeFail
			default:
				return nil, fmt.Errorf("case %s terminal status %q is outside the closed set", caseID, receipt.Status)
			}
			states = append(states, CaseScoreState{
				Host: key.Host, Split: key.Split, Ordinal: key.Ordinal, CaseID: caseID, Outcome: outcome,
			})
		}
	}
	return states, nil
}

// loadCaseRunReceipt reads one case receipt from its fixed place in the run
// tree. The case id is part of the path, so a receipt filed under another
// case's name cannot be mistaken for this one.
func loadCaseRunReceipt(seriesRoot string, key RunKey, caseID string) (*CaseRunReceipt, error) {
	if !caseIDRE.MatchString(caseID) {
		return nil, fmt.Errorf("case id %q is not a safe file name", caseID)
	}
	p := filepath.Join(PrimaryRunRoot(seriesRoot, key.Host, key.Split, key.Ordinal), casesDirName, caseID, caseReceiptName)
	r := &CaseRunReceipt{}
	if err := loadStrictFile(p, r); err != nil {
		return nil, err
	}
	return r, nil
}

// validateCaseRunReceiptOnDisk is the score-side read of §10: a closed
// terminal status bound to its verdict class, exactly one primary attempt,
// three complete artifact pairs, and a non-negative duration.
func validateCaseRunReceiptOnDisk(r *CaseRunReceipt, caseID string) error {
	if r.CaseID != caseID {
		return fmt.Errorf("case receipt names case %q, want %q", r.CaseID, caseID)
	}
	if r.AttemptCount != 1 {
		return fmt.Errorf("case %s attempt_count %d, primary allows exactly 1", caseID, r.AttemptCount)
	}
	if r.DurationMS < 0 {
		return fmt.Errorf("case %s has a negative duration_ms", caseID)
	}
	if err := ValidateVerdict(&r.Verdict); err != nil {
		return err
	}
	switch r.Status {
	case CaseStatusPass:
		if !r.Verdict.Pass {
			return fmt.Errorf("case %s: status pass with a failing verdict", caseID)
		}
	case CaseStatusFail:
		if r.Verdict.Pass {
			return fmt.Errorf("case %s: status fail with a passing verdict", caseID)
		}
		if IsTerminalRunnerClass(r.Verdict.Failure) {
			return fmt.Errorf("case %s: runner-error cases carry status runner-error, not fail", caseID)
		}
	case CaseStatusRunnerError:
		if r.Verdict.Pass || !IsTerminalRunnerClass(r.Verdict.Failure) {
			return fmt.Errorf("case %s: status runner-error must carry the terminal runner-error class", caseID)
		}
	default:
		return fmt.Errorf("case %s terminal status %q is outside the closed pass/fail/runner-error set", caseID, r.Status)
	}
	for _, p := range []struct{ name, path, digest string }{
		{"normalized_events", r.NormalizedEventsPath, r.NormalizedEventsDigest},
		{"raw_events", r.RawEventsPath, r.RawEventsDigest},
		{"store_dump", r.StoreDumpPath, r.StoreDumpDigest},
	} {
		if (p.path == "") != (p.digest == "") {
			return fmt.Errorf("case %s %s artifact pair incomplete: path %q digest %q", caseID, p.name, p.path, p.digest)
		}
	}
	return nil
}

// readScoreArtifact resolves one recorded artifact path against the series
// root (a relative path must be containment-safe, an absolute one must stay
// inside the root; symlink escapes are rejected either way), verifies its
// recorded digest and returns the bytes. Artifact digests are plain sha-256
// over the exact file bytes, which is how the runner wrote them.
func readScoreArtifact(seriesRoot, recordedPath, wantDigest string) ([]byte, error) {
	if recordedPath == "" || wantDigest == "" {
		return nil, errors.New("case receipt artifact pair is empty")
	}
	clean := filepath.Clean(recordedPath)
	if !filepath.IsAbs(clean) && !safeRelativePath(filepath.ToSlash(clean)) {
		return nil, fmt.Errorf("artifact path %q is not containment-safe", recordedPath)
	}
	p, err := EnsureInside(seriesRoot, clean)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("artifact %s: %w", recordedPath, err)
	}
	if got := sha256Hex(b); got != wantDigest {
		return nil, fmt.Errorf("artifact %s digest %s != recorded %s (post-run mutation)", recordedPath, got, wantDigest)
	}
	return b, nil
}

// sortedRunKeys fixes a deterministic pass over the primary matrix.
func sortedRunKeys(runs map[RunKey]*PrimaryRunManifest) []RunKey {
	keys := make([]RunKey, 0, len(runs))
	for k := range runs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Host != keys[j].Host {
			return keys[i].Host < keys[j].Host
		}
		if keys[i].Split != keys[j].Split {
			return keys[i].Split < keys[j].Split
		}
		return keys[i].Ordinal < keys[j].Ordinal
	})
	return keys
}
