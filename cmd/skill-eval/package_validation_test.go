package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSkillPackage materializes a minimal but validator-passing package.
func fakeSkillPackage(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "SKILL.md", []byte("---\nname: engram\nversion: 9.9.9-fx\n---\n\n# engram\n\nFictional test skill body.\n"))
	writeFile(t, dir, filepath.Join("references", "contract.json"), []byte("{\"contract\":\"fictional\"}\n"))
}

func okValidator(argv []string) capturedOutput {
	return capturedOutput{exitCode: 0, stdout: []byte("{\"status\": \"ok\", \"mode\": \"release\"}")}
}

func failingValidator(argv []string) capturedOutput {
	return capturedOutput{exitCode: 1, stdout: nil, stderr: []byte("error: fictional validation failure")}
}

func TestPackageValidateProducerLifecycle(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	repo := t.TempDir()
	validator := filepath.Join(repo, "scripts", "validate-agent-skill.mjs")
	writeFile(t, filepath.Join(repo, "scripts"), "validate-agent-skill.mjs", []byte("// fictional 020 validator"))
	src := filepath.Join(repo, "skills", "engram")
	fakeSkillPackage(t, src)

	greenPath := filepath.Join(repo, "protected", "receipts", "formal-tooling-green.json")
	if _, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, greenPath, GreenBindings{}); err != nil {
		t.Fatal(err)
	}
	snapRoot := filepath.Join(repo, "protected", "snapshots", "pre-revision")
	out := filepath.Join(repo, "protected", "receipts", "pv.json")
	rec, err := runPackageValidateWith(src, repo, snapRoot, greenPath, validator, out, okValidator)
	if err != nil {
		t.Fatalf("package validate failed: %v", err)
	}
	if !rec.Passed || rec.ReceiptDigest == "" {
		t.Fatal("passing validation must emit a digested receipt")
	}
	// Source/staging/materialized byte equality: every source file exists in
	// the snapshot with identical bytes.
	srcRecs, err := inventoryPackage(src)
	if err != nil {
		t.Fatal(err)
	}
	snapRecs, err := inventoryPackage(snapRoot)
	if err != nil {
		t.Fatal(err)
	}
	// snapshot.json is controller metadata, not part of the package payload.
	var pkgRecs []PackageFileRecord
	for _, r := range snapRecs {
		if r.RelativePath != "snapshot.json" {
			pkgRecs = append(pkgRecs, r)
		}
	}
	if len(srcRecs) != len(pkgRecs) {
		t.Fatalf("snapshot inventory %d != source %d", len(pkgRecs), len(srcRecs))
	}
	for i, r := range srcRecs {
		if pkgRecs[i].RelativePath != r.RelativePath || pkgRecs[i].SHA256 != r.SHA256 {
			t.Fatalf("snapshot file %d drifted: %+v vs %+v", i, pkgRecs[i], r)
		}
	}
	// The materialized snapshot is not a symlink/substitute for the source.
	if fi, err := os.Lstat(snapRoot); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("snapshot root must be a real directory")
	}
	// Sorted recursive inventory order is bytewise.
	for i := 1; i < len(snapRecs); i++ {
		if snapRecs[i-1].RelativePath >= snapRecs[i].RelativePath {
			t.Fatal("snapshot inventory must be sorted by raw relative path")
		}
	}
	// Immutable anchor present and verifiable inside snapshot.json.
	b, err := os.ReadFile(filepath.Join(snapRoot, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap FrozenSkillPackageSnapshot
	if err := StrictParseClosed(b, &snap); err != nil {
		t.Fatal(err)
	}
	anchorBody, err := CanonicalJSON(struct {
		SchemaVersion     int    `json:"schema_version"`
		SnapshotID        string `json:"snapshot_id"`
		SkillDigest       string `json:"skill_digest"`
		FileRecordsDigest string `json:"file_records_digest"`
		ValidatorDigest   string `json:"validator_digest"`
	}{snap.SnapshotAnchor.SchemaVersion, snap.SnapshotAnchor.SnapshotID, snap.SnapshotAnchor.SkillDigest,
		snap.SnapshotAnchor.FileRecordsDigest, snap.SnapshotAnchor.ValidatorDigest})
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(anchorBody) != snap.SnapshotAnchor.AnchorDigest {
		t.Fatal("snapshot anchor digest mismatch")
	}
	// Verify helper passes on the pristine snapshot.
	if err := VerifyPackageValidationReceipt(rec, snapRoot, validator); err != nil {
		t.Fatalf("pristine snapshot verification failed: %v", err)
	}
}

func TestPackageValidateRejections(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	repo := t.TempDir()
	validator := filepath.Join(repo, "scripts", "validate-agent-skill.mjs")
	writeFile(t, filepath.Join(repo, "scripts"), "validate-agent-skill.mjs", []byte("// v"))
	src := filepath.Join(repo, "skills", "engram")
	fakeSkillPackage(t, src)
	snapRoot := filepath.Join(repo, "snap")

	// Missing/failed/wrong-suite green receipt.
	if _, err := runPackageValidateWith(src, repo, snapRoot, "", validator, "", okValidator); err == nil {
		t.Fatal("missing green receipt must fail the gate")
	}
	greenPath := filepath.Join(repo, "receipts", "holdout-pipeline-green.json")
	if _, err := CreateGreenTestReceipt(SuiteHoldoutPipeline, validator, greenPath, GreenBindings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := runPackageValidateWith(src, repo, snapRoot, greenPath, validator, "", okValidator); err == nil {
		t.Fatal("wrong-suite green receipt must fail the gate")
	}
	// Validator failure leaves no reusable snapshot or receipt.
	greenPath = filepath.Join(repo, "receipts", "ft-green.json")
	if _, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, greenPath, GreenBindings{}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(repo, "pv-fail.json")
	if _, err := runPackageValidateWith(src, repo, snapRoot, greenPath, validator, out, failingValidator); err == nil {
		t.Fatal("validator failure must fail package validate")
	}
	if _, err := os.Stat(snapRoot); err == nil {
		entries, _ := os.ReadDir(snapRoot)
		if len(entries) > 0 {
			t.Fatal("validator failure must not materialize a snapshot")
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("validator failure must not leave a passing receipt")
	}
	// Internal symlink is rejected.
	fakeSkillPackage(t, src)
	if err := os.Symlink(filepath.Join(src, "SKILL.md"), filepath.Join(src, "link.md")); err == nil {
		if _, err := runPackageValidateWith(src, repo, filepath.Join(repo, "snap2"), greenPath, validator, "", okValidator); err == nil {
			t.Fatal("internal symlink must be rejected")
		}
	}
	// Non-empty snapshot root is refused.
	nonEmpty := filepath.Join(repo, "snap3")
	writeFile(t, nonEmpty, "stale.txt", []byte("x"))
	if _, err := runPackageValidateWith(src, repo, nonEmpty, greenPath, validator, "", okValidator); err == nil {
		t.Fatal("non-empty snapshot root must be refused")
	}
}

func TestPackageValidatePrimaryRejectionOnDrift(t *testing.T) {
	defer func(orig func(string) ([]GreenCommand, error)) { fixedSuiteRunner = orig }(fixedSuiteRunner)
	fixedSuiteRunner = okSuiteRunner
	repo := t.TempDir()
	validator := filepath.Join(repo, "scripts", "validate-agent-skill.mjs")
	writeFile(t, filepath.Join(repo, "scripts"), "validate-agent-skill.mjs", []byte("// v"))
	src := filepath.Join(repo, "skills", "engram")
	fakeSkillPackage(t, src)
	snapRoot := filepath.Join(repo, "snap-drift")
	greenPath := filepath.Join(repo, "receipts", "ft-green.json")
	if _, err := CreateGreenTestReceipt(SuiteFormalTooling, validator, greenPath, GreenBindings{}); err != nil {
		t.Fatal(err)
	}
	rec, err := runPackageValidateWith(src, repo, snapRoot, greenPath, validator, filepath.Join(repo, "receipts", "pv-drift.json"), okValidator)
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot byte drift → primary usage must be rejected.
	p := filepath.Join(snapRoot, "SKILL.md")
	orig, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, snapRoot, "SKILL.md", append(orig, []byte("\n<!-- drift -->\n")...))
	if err := VerifyPackageValidationReceipt(rec, snapRoot, validator); err == nil {
		t.Fatal("snapshot byte drift must fail verification (no mutable substitute)")
	}
	// File-list drift: delete a file.
	writeFile(t, snapRoot, "SKILL.md", orig)
	if err := os.Remove(filepath.Join(snapRoot, "references", "contract.json")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPackageValidationReceipt(rec, snapRoot, validator); err == nil {
		t.Fatal("file-list drift must fail verification")
	}
	// Package-digest drift via receipt tampering.
	fakeSkillPackage(t, src)
	if err := os.MkdirAll(filepath.Join(snapRoot, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(snapRoot, "references"), "contract.json", []byte("{\"contract\":\"fictional\"}\n"))
	tampered := *rec
	tampered.SkillDigest = strings.Repeat("9", 64)
	if err := VerifyPackageValidationReceipt(&tampered, snapRoot, validator); err == nil {
		t.Fatal("package-digest drift must fail verification")
	}
}
