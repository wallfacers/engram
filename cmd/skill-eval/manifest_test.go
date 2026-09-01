package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempManifestMaterializes a two-case fixture core dataset in a temp dir
// with real digests, returning (dir, manifest).
func writeTempManifest(t *testing.T) (string, *DatasetManifestV2) {
	t.Helper()
	dir := t.TempDir()
	caseA := readFixture(t, "core-case-v2.json")
	caseB := readFixture(t, "core-regression-v2.json")
	writeFile(t, dir, "cases-a.json", caseA)
	writeFile(t, dir, "cases-b.json", caseB)

	digA := LFNormalizedSHA256Bytes(caseA)
	digB := LFNormalizedSHA256Bytes(caseB)
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "fx-test-v1",
		Split: SplitDevRegression, ScoreMembership: MembershipCore172,
		CaseCount: 2,
		ModuleCounts: map[string]int{"implicit-write-pos": 1, "regression": 1},
		LanguageCounts: map[string]int{LangZh: 1, LangUnclassified: 1},
		CaseIDs: []string{"fx-iwp-001", "fx-reg-001"},
		PayloadFiles: []PayloadFileV1{
			{RelativePath: "cases-a.json", LFNormalizedSHA256: digA, CaseIDs: []string{"fx-iwp-001"}},
			{RelativePath: "cases-b.json", LFNormalizedSHA256: digB, CaseIDs: []string{"fx-reg-001"}},
		},
	}
	return dir, m
}

func TestDatasetPayloadDigestCaseOnly(t *testing.T) {
	dir, m := writeTempManifest(t)
	d1, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	// A manifest edit must not change the case-only payload digest.
	m.DatasetVersion = "fx-test-v2"
	d2, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("payload digest must cover case files only, not the manifest")
	}
	// Adding an unrelated file to the directory must not change it either.
	writeFile(t, dir, "evals.json", []byte(`[{"query":"legacy"}]`))
	d3, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d3 {
		t.Fatal("directory-discovered files must never enter the payload digest")
	}
	// Case content change changes the digest.
	writeFile(t, dir, "cases-a.json", append(readFixture(t, "core-case-v2.json"), []byte("\n")...))
	if d4, _ := DatasetPayloadDigest(dir, m); d4 == d1 {
		t.Fatal("payload digest must change when case bytes change")
	}
}

func TestFreezeBeforeDigestManifestAfterPayload(t *testing.T) {
	dir, m := writeTempManifest(t)
	// Freeze-before-digest: digest computed only after the payload digest lands.
	if _, err := CompleteManifestForSeal(m); err == nil {
		t.Fatal("manifest without payload_digest must be refused")
	}
	pd, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	m.PayloadDigest = pd
	cid, err := CaseIDsDigest(m.CaseIDs)
	if err != nil {
		t.Fatal(err)
	}
	m.CaseIDsDigest = cid
	d1, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatalf("completed manifest refused: %v", err)
	}
	// Seal exclusion: the digest is computed over the manifest without seal.
	m.Seal = &DatasetSeal{ManifestDigest: "x"}
	d2, err := CompleteManifestForSeal(m)
	if err == nil {
		t.Fatal("sealed manifest must be refused for a new seal computation")
	}
	mCopy := *m
	mCopy.Seal = nil
	d3, _ := CanonicalSHA256(mCopy)
	if d1 != d3 {
		t.Fatal("manifest digest must exclude only the seal object")
	}
	_ = d2
}

func TestSealTamperDetection(t *testing.T) {
	dir, m := writeTempManifest(t)
	pd, _ := DatasetPayloadDigest(dir, m)
	m.PayloadDigest = pd
	m.CaseIDsDigest, _ = CaseIDsDigest(m.CaseIDs)
	manifestDigest, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := BuildDatasetAnchor(m, manifestDigest, "git-tag", "refs/tags/fx-v1")
	if err != nil {
		t.Fatal(err)
	}
	m.Seal = seal
	if err := VerifyDatasetSeal(m, dir); err != nil {
		t.Fatalf("fresh seal must verify: %v", err)
	}
	// Tamper 1: post-seal field mutation.
	m.CaseCount = 99
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("post-seal mutation must invalidate the seal")
	}
	m.CaseCount = 2
	// Tamper 2: anchor preimage forgery.
	m.Seal.AnchorPreimageDigest = strings.Repeat("0", 64)
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("forged anchor preimage must fail closed")
	}
	seal2, _ := BuildDatasetAnchor(m, manifestDigest, "git-tag", "refs/tags/fx-v1")
	m.Seal = seal2
	// Tamper 3: payload swap after seal.
	writeFile(t, dir, "cases-a.json", []byte(`{"dataset":"x","version":9,"cases":[]}`))
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Fatal("payload swap after seal must invalidate the seal")
	}
}

func TestImmutableManifestRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "frozen.json")
	if err := WriteFrozenFile(p, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrozenFile(p, []byte(`{"x":1}`)); err == nil {
		t.Fatal("frozen outputs must never be overwritten")
	}
	_ = os.Remove(p)
}

func TestEnsureInsideContainment(t *testing.T) {
	parent := t.TempDir()
	if _, err := EnsureInside(parent, filepath.Join(parent, "sub", "f.json")); err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	if _, err := EnsureInside(parent, filepath.Join(parent, "..", "escape")); err == nil {
		t.Fatal(".. escape must be rejected")
	}
	if _, err := EnsureInside(parent, "/etc/passwd"); err == nil {
		t.Fatal("absolute outside path must be rejected")
	}
	if !UnsafePath("../x") || !UnsafePath("a\\b") || !UnsafePath("/abs") {
		t.Fatal("UnsafePath must reject traversal/backslash/absolute")
	}
	if UnsafePath("a/b/c.json") {
		t.Fatal("safe relative path rejected")
	}
}
