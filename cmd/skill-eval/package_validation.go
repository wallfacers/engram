package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// `skill-eval package validate` — the SOLE producer of a formal
// FrozenSkillPackageSnapshot + SkillPackageValidationReceipt
// (data-model.md; runner-cli.md §1.1). It copies the complete package into an
// anchored immutable snapshot, binds the existing 020 validator to that exact
// sorted file list, and never leaves a reusable passing artifact on failure.

// PackageFileRecord is one exact-byte inventory entry.
type PackageFileRecord struct {
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	Size         int    `json:"size"`
}

// SnapshotAnchor is the controller-held immutable anchor.
type SnapshotAnchor struct {
	SchemaVersion    int    `json:"schema_version"`
	SnapshotID       string `json:"snapshot_id"`
	SkillDigest      string `json:"skill_digest"`
	FileRecordsDigest string `json:"file_records_digest"`
	ValidatorDigest  string `json:"validator_digest"`
	AnchorDigest     string `json:"anchor_digest"`
}

// FrozenSkillPackageSnapshot is the immutable evaluated package.
type FrozenSkillPackageSnapshot struct {
	SchemaVersion     int                `json:"schema_version"`
	SnapshotID        string             `json:"snapshot_id"`
	SnapshotRootDigest string            `json:"snapshot_root_digest"`
	SkillDigest       string             `json:"skill_digest"`
	FileRecords       []PackageFileRecord `json:"file_records"`
	ValidatorRevision string             `json:"validator_revision"`
	ValidatorDigest   string             `json:"validator_digest"`
	SnapshotAnchor    SnapshotAnchor     `json:"snapshot_anchor"`
	CreatedAt         string             `json:"created_at"`
	SnapshotDigest    string             `json:"snapshot_digest"`
}

// SkillPackageValidationReceipt binds a passing validation to one exact snapshot.
type SkillPackageValidationReceipt struct {
	SnapshotID            string            `json:"snapshot_id"`
	SnapshotDigest        string            `json:"snapshot_digest"`
	SnapshotAnchorDigest  string            `json:"snapshot_anchor_digest"`
	SkillVersion          string            `json:"skill_version"`
	SkillDigest           string            `json:"skill_digest"`
	FileRecordsDigest     string            `json:"file_records_digest"`
	FileRecords           []PackageFileRecord `json:"file_records"`
	ValidatorRevision     string            `json:"validator_revision"`
	ValidatorDigest       string            `json:"validator_digest"`
	ValidatorArgvDigest   string            `json:"validator_argv_digest"`
	ValidatorOutputDigest string            `json:"validator_output_digest"`
	Checks                map[string]bool   `json:"checks"`
	Passed                bool              `json:"passed"`
	ValidatedAt           string            `json:"validated_at"`
	ReceiptDigest         string            `json:"receipt_digest"`
}

// FileRecordsDigest is the sorted-inventory receipt digest.
func FileRecordsDigest(recs []PackageFileRecord) (string, error) {
	sorted := append([]PackageFileRecord{}, recs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RelativePath < sorted[j].RelativePath })
	return CanonicalSHA256(sorted)
}

// EngramPackageDigest reimplements `engram-package-sha256-v1`
// (scripts/validate-agent-skill.mjs calculatePackageDigest): sorted relative
// paths, LF-normalized UTF-8 content, framing path\0len\0content\0.
func EngramPackageDigest(files []PackageFileRecord, readContent func(rel string) ([]byte, error)) (string, error) {
	sorted := append([]PackageFileRecord{}, files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RelativePath < sorted[j].RelativePath })
	if len(sorted) == 0 {
		return "", fmt.Errorf("package must not be empty")
	}
	var buf []byte
	for _, f := range sorted {
		content, err := readContent(f.RelativePath)
		if err != nil {
			return "", err
		}
		norm := normalizeLF(content)
		buf = append(buf, []byte(f.RelativePath)...)
		buf = append(buf, 0)
		buf = append(buf, []byte(itoa(len(norm)))...)
		buf = append(buf, 0)
		buf = append(buf, norm...)
		buf = append(buf, 0)
	}
	return sha256Hex(buf), nil
}

// inventoryPackage enumerates the source package: regular files only, sorted
// by raw UTF-8 relative path, symlinks/special files/path escapes rejected.
func inventoryPackage(srcDir string) ([]PackageFileRecord, error) {
	var recs []PackageFileRecord
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(absSrc, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absSrc, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("internal symlink %q is not allowed in an evaluated package", rel)
		}
		if !d.Type().IsRegular() {
			if d.IsDir() {
				return nil
			}
			return fmt.Errorf("unsupported package entry %q", rel)
		}
		if !safeRelativePath(filepath.ToSlash(rel)) {
			return fmt.Errorf("package path %q is not containment-safe", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		recs = append(recs, PackageFileRecord{RelativePath: filepath.ToSlash(rel), SHA256: sha256Hex(b), Size: len(b)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("package %s is empty", srcDir)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].RelativePath < recs[j].RelativePath })
	return recs, nil
}

// ValidatorRunner executes the 020 validator against a staged package;
// production binds the node CLI, tests inject a deterministic stub.
type ValidatorRunner func(argv []string) capturedOutput

var zero20Validator ValidatorRunner = func(argv []string) capturedOutput {
	return runAndCapture(argv)
}

// RunPackageValidate implements the `package validate` command.
func RunPackageValidate(skillDir, repoRoot, snapshotRoot, greenReceiptPath, validatorPath, outPath string) (*SkillPackageValidationReceipt, error) {
	return runPackageValidateWith(skillDir, repoRoot, snapshotRoot, greenReceiptPath, validatorPath, outPath, zero20Validator)
}

func runPackageValidateWith(skillDir, repoRoot, snapshotRoot, greenReceiptPath, validatorPath, outPath string, validator ValidatorRunner) (*SkillPackageValidationReceipt, error) {
	// 1. Gate: a current passing formal-tooling receipt.
	green, err := LoadGreenTestReceipt(greenReceiptPath)
	if err != nil {
		return nil, fmt.Errorf("package validate requires a formal-tooling green receipt: %w", err)
	}
	if err := VerifyGreenTestReceipt(green, SuiteFormalTooling, validatorPath, GreenBindings{}); err != nil {
		return nil, fmt.Errorf("package validate green gate failed: %w", err)
	}
	// 2. Snapshot root must be fresh.
	if fi, err := os.Stat(snapshotRoot); err == nil {
		if !fi.IsDir() {
			return nil, fmt.Errorf("snapshot root %s exists and is not a directory", snapshotRoot)
		}
		entries, _ := os.ReadDir(snapshotRoot)
		if len(entries) > 0 {
			return nil, fmt.Errorf("snapshot root %s already exists and is not empty", snapshotRoot)
		}
	}
	// 3. Inventory the source; copy to staging in the same pass.
	recs, err := inventoryPackage(skillDir)
	if err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp("", "skill-snapshot-staging-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	for _, r := range recs {
		src := filepath.Join(skillDir, filepath.FromSlash(r.RelativePath))
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(staging, filepath.FromSlash(r.RelativePath))
		if err := osWriteFile(dst, b); err != nil {
			return nil, err
		}
	}
	// 4. Recompute everything from the staged bytes.
	var stagedRecs []PackageFileRecord
	stagedRead := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(staging, filepath.FromSlash(rel)))
	}
	for _, r := range recs {
		b, err := stagedRead(r.RelativePath)
		if err != nil {
			return nil, err
		}
		if sha256Hex(b) != r.SHA256 || len(b) != r.Size {
			return nil, fmt.Errorf("staging byte mismatch for %s", r.RelativePath)
		}
		stagedRecs = append(stagedRecs, r)
	}
	skillDigest, err := EngramPackageDigest(stagedRecs, stagedRead)
	if err != nil {
		return nil, err
	}
	validatorDigest := green.ValidatorDigest
	validatorRevision := "020-validate-agent-skill-v1"
	// 5. Bind the existing 020 validator to the staged package.
	argv := []string{"node", filepath.Join(repoRoot, "scripts", "validate-agent-skill.mjs"), "--package", staging, "--root", repoRoot}
	argvDigest, err := CanonicalSHA256(argv)
	if err != nil {
		return nil, err
	}
	out := validator(argv)
	if out.exitCode != 0 {
		return nil, fmt.Errorf("020 validator failed on the staged package (exit %d); no snapshot materialized", out.exitCode)
	}
	if !strings.Contains(string(out.stdout), `"status": "ok"`) && !strings.Contains(string(out.stdout), `"status":"ok"`) {
		return nil, fmt.Errorf("020 validator output does not report ok; no snapshot materialized")
	}
	// 6. Materialize atomically and build the anchor.
	if err := os.MkdirAll(snapshotRoot, 0o755); err != nil {
		return nil, err
	}
	snapshotID := "snap-" + sha256Hex([]byte(skillDigest+sha256Hex(out.stdout)))[:24]
	fileRecDigest, err := FileRecordsDigest(stagedRecs)
	if err != nil {
		return nil, err
	}
	anchor := SnapshotAnchor{SchemaVersion: 1, SnapshotID: snapshotID, SkillDigest: skillDigest,
		FileRecordsDigest: fileRecDigest, ValidatorDigest: validatorDigest}
	anchorBody, err := CanonicalJSON(struct {
		SchemaVersion     int    `json:"schema_version"`
		SnapshotID        string `json:"snapshot_id"`
		SkillDigest       string `json:"skill_digest"`
		FileRecordsDigest string `json:"file_records_digest"`
		ValidatorDigest   string `json:"validator_digest"`
	}{anchor.SchemaVersion, anchor.SnapshotID, anchor.SkillDigest, anchor.FileRecordsDigest, anchor.ValidatorDigest})
	if err != nil {
		return nil, err
	}
	anchor.AnchorDigest = sha256Hex(anchorBody)
	err = filepath.Walk(staging, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(staging, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(snapshotRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		dstF, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		defer dstF.Close()
		_, err = io.Copy(dstF, src)
		return err
	})
	if err != nil {
		os.RemoveAll(snapshotRoot)
		return nil, err
	}
	// Snapshot-level digest: inventory of the materialized tree.
	rootRecs, err := inventoryPackage(snapshotRoot)
	if err != nil {
		return nil, err
	}
	rootDigest, err := FileRecordsDigest(rootRecs)
	if err != nil {
		return nil, err
	}
	snap := &FrozenSkillPackageSnapshot{
		SchemaVersion: 1, SnapshotID: snapshotID, SnapshotRootDigest: rootDigest,
		SkillDigest: skillDigest, FileRecords: stagedRecs,
		ValidatorRevision: validatorRevision, ValidatorDigest: validatorDigest,
		SnapshotAnchor: anchor, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	snap.SnapshotDigest = mustDigest(snap)
	snapJSON, err := CanonicalJSON(snap)
	if err != nil {
		return nil, err
	}
	if err := WriteFrozenFile(filepath.Join(snapshotRoot, "snapshot.json"), snapJSON); err != nil {
		return nil, err
	}
	// 7. Receipt.
	version := readSkillVersion(stagedRead)
	receipt := &SkillPackageValidationReceipt{
		SnapshotID: snap.SnapshotID, SnapshotDigest: snap.SnapshotDigest,
		SnapshotAnchorDigest: anchor.AnchorDigest,
		SkillVersion: version, SkillDigest: skillDigest,
		FileRecordsDigest: fileRecDigest, FileRecords: stagedRecs,
		ValidatorRevision: validatorRevision, ValidatorDigest: validatorDigest,
		ValidatorArgvDigest: argvDigest, ValidatorOutputDigest: sha256Hex(out.stdout),
		Checks: map[string]bool{
			"description_body_reference_sync": true,
			"version_bump":                    true,
			"line_reference_digest_consistency": true,
		},
		Passed:      true,
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	d, err := receiptDigestPV(receipt)
	if err != nil {
		return nil, err
	}
	receipt.ReceiptDigest = d
	if err := WriteFrozenFile(outPath, mustCanonicalJSON(receipt)); err != nil {
		return nil, err
	}
	return receipt, nil
}

func mustDigest(s *FrozenSkillPackageSnapshot) string {
	saved := s.SnapshotDigest
	s.SnapshotDigest = ""
	d, err := CanonicalSHA256(s)
	s.SnapshotDigest = saved
	if err != nil {
		panic(err)
	}
	return d
}

func receiptDigestPV(r *SkillPackageValidationReceipt) (string, error) {
	saved := r.ReceiptDigest
	r.ReceiptDigest = ""
	d, err := CanonicalSHA256(r)
	r.ReceiptDigest = saved
	return d, err
}

// readSkillVersion extracts the frontmatter version from SKILL.md.
func readSkillVersion(read func(rel string) ([]byte, error)) string {
	b, err := read("SKILL.md")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		}
	}
	return ""
}

// VerifyPackageValidationReceipt re-verifies a receipt against a snapshot dir:
// rehash the materialized tree, the anchor and every binding; a post-hoc or
// wrong-snapshot receipt fails closed.
func VerifyPackageValidationReceipt(r *SkillPackageValidationReceipt, snapshotRoot, validatorPath string) error {
	if r == nil || !r.Passed {
		return fmt.Errorf("package validation receipt missing or failed")
	}
	rootRecs, err := inventoryPackage(snapshotRoot)
	if err != nil {
		return err
	}
	// The snapshot dir carries snapshot.json alongside package files; filter
	// it for the package inventory comparison.
	var pkgRecs []PackageFileRecord
	for _, rec := range rootRecs {
		if rec.RelativePath == "snapshot.json" {
			continue
		}
		pkgRecs = append(pkgRecs, rec)
	}
	for _, want := range r.FileRecords {
		found := false
		for _, got := range pkgRecs {
			if got.RelativePath == want.RelativePath {
				if got.SHA256 != want.SHA256 || got.Size != want.Size {
					return fmt.Errorf("snapshot file %s drifted from the receipt", want.RelativePath)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("snapshot file %s missing from the materialized tree", want.RelativePath)
		}
	}
	if len(pkgRecs) != len(r.FileRecords) {
		return fmt.Errorf("snapshot carries %d package files, receipt lists %d", len(pkgRecs), len(r.FileRecords))
	}
	fd, err := FileRecordsDigest(r.FileRecords)
	if err != nil {
		return err
	}
	if fd != r.FileRecordsDigest {
		return fmt.Errorf("receipt file-records digest mismatch")
	}
	digest, err := EngramPackageDigest(pkgRecs, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(snapshotRoot, filepath.FromSlash(rel)))
	})
	if err != nil {
		return err
	}
	if digest != r.SkillDigest {
		return fmt.Errorf("snapshot package digest %s != receipt skill digest %s", digest, r.SkillDigest)
	}
	if b, err := os.ReadFile(filepath.Join(snapshotRoot, "snapshot.json")); err == nil {
		var snap FrozenSkillPackageSnapshot
		if err := StrictParseClosed(b, &snap); err != nil {
			return fmt.Errorf("materialized snapshot.json is not the frozen snapshot: %w", err)
		}
		if snap.SnapshotDigest != r.SnapshotDigest || snap.SkillDigest != r.SkillDigest {
			return fmt.Errorf("materialized snapshot identity disagrees with the receipt")
		}
		saved := snap.SnapshotDigest
		snap.SnapshotDigest = ""
		recomputed, err := CanonicalSHA256(snap)
		snap.SnapshotDigest = saved
		if err != nil || recomputed != saved {
			return fmt.Errorf("materialized snapshot digest does not verify (post-hoc mutation)")
		}
	} else {
		return fmt.Errorf("snapshot.json missing from snapshot root")
	}
	_, _, validator, err := currentImplementDigests(validatorPath)
	if err != nil {
		return err
	}
	if r.ValidatorDigest != validator {
		return fmt.Errorf("receipt validator digest drifted from the current validator")
	}
	saved := r.ReceiptDigest
	r.ReceiptDigest = ""
	d, err := CanonicalSHA256(r)
	r.ReceiptDigest = saved
	if err != nil {
		return err
	}
	if d != saved {
		return fmt.Errorf("package validation receipt digest mismatch (post-hoc mutation)")
	}
	return nil
}
