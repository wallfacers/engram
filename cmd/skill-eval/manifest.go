package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 048 manifest/digest/seal primitives (data-model.md §7, dataset-protocol §7.1).

func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func osWriteFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// WriteFrozenFile materializes an immutable artifact: it refuses to overwrite
// an existing file (frozen outputs are never rewritten).
func WriteFrozenFile(p string, data []byte) error {
	if _, err := osStat(p); err == nil {
		return fmt.Errorf("frozen output %s already exists and is never overwritten", p)
	}
	return osWriteFile(p, data)
}

// EnsureInside resolves candidate under parent and asserts containment after
// symlink elimination. Returns the resolved path or an error.
func EnsureInside(parent, candidate string) (string, error) {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	// Eliminate symlinks along both paths where they exist.
	if rp, err := filepath.EvalSymlinks(absParent); err == nil {
		absParent = rp
	}
	if rp, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(rp, filepath.Base(abs))
	}
	rel, err := filepath.Rel(absParent, abs)
	if err != nil {
		return "", fmt.Errorf("path containment check failed: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes parent %q", candidate, parent)
	}
	return filepath.Join(absParent, rel), nil
}

// CaseIDsDigest is the sorted-list receipt for a manifest case ID set.
func CaseIDsDigest(ids []string) (string, error) {
	c := append([]string{}, ids...)
	sort.Strings(c)
	return CanonicalSHA256(c)
}

// DatasetPayloadDigest implements `agent-memory-trigger-dataset-sha256-v1`
// (dataset-protocol §7.1): over the manifest-named case payload files only —
// never the manifest, seal, directory-discovered or legacy extra files.
func DatasetPayloadDigest(dir string, m *DatasetManifestV2) (string, error) {
	files := append([]PayloadFileV1{}, m.PayloadFiles...)
	sort.Slice(files, func(i, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	var buf bytes.Buffer
	covered := map[string]int{}
	for _, pf := range files {
		if !safeRelativePath(pf.RelativePath) {
			return "", fmt.Errorf("payload path %q is not containment-safe", pf.RelativePath)
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pf.RelativePath)))
		if err != nil {
			return "", fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		norm, err := LFNormalizedSHA256(b)
		if err != nil {
			return "", fmt.Errorf("payload file %s: %w", pf.RelativePath, err)
		}
		if pf.LFNormalizedSHA256 != "PLACEHOLDER_COMPUTED_AT_RUNTIME" && norm != pf.LFNormalizedSHA256 {
			return "", fmt.Errorf("payload file %s digest mismatch", pf.RelativePath)
		}
		buf.WriteString(pf.RelativePath)
		buf.WriteByte(0)
		fmt.Fprintf(&buf, "%d", len(b))
		buf.WriteByte(0)
		buf.Write(normalizeLF(b))
		buf.WriteByte(0)
		for _, id := range pf.CaseIDs {
			covered[id]++
		}
	}
	for _, id := range m.CaseIDs {
		if covered[id] != 1 {
			return "", fmt.Errorf("case %s appears %d times across payload_files", id, covered[id])
		}
	}
	if len(covered) != len(m.CaseIDs) {
		return "", errors.New("payload_files cover a different case set than the manifest")
	}
	return sha256Hex(buf.Bytes()), nil
}

// CompleteManifestForSeal verifies freeze-before-digest preconditions and
// returns the canonical manifest digest excluding only the `seal` object.
func CompleteManifestForSeal(m *DatasetManifestV2) (string, error) {
	if m.CaseCount == 0 || len(m.CaseIDs) == 0 {
		return "", errors.New("manifest is not complete: case set empty")
	}
	if m.PayloadDigest == "" {
		return "", errors.New("manifest is not complete: payload_digest missing (freeze before digest)")
	}
	if m.CaseIDsDigest == "" {
		return "", errors.New("manifest is not complete: case_ids_digest missing")
	}
	if m.Seal != nil {
		return "", errors.New("manifest already carries a seal")
	}
	return CanonicalSHA256(m)
}

// BuildDatasetAnchor assembles the exact DatasetAnchorV1 preimage and its
// digests for the chosen anchor type.
func BuildDatasetAnchor(m *DatasetManifestV2, manifestDigest, anchorType, anchorID string) (*DatasetSeal, error) {
	switch anchorType {
	case "git-tag", "detached-signature", "immutable-object":
	default:
		return nil, fmt.Errorf("anchor_type %q invalid", anchorType)
	}
	anchor := DatasetAnchorV1{
		SchemaVersion: 1, Canonicalization: CanonicalizationName,
		DatasetID: m.DatasetID, DatasetVersion: m.DatasetVersion,
		ManifestDigest: manifestDigest, DatasetPayloadDigest: m.PayloadDigest,
	}
	preimage, err := CanonicalJSON(anchor)
	if err != nil {
		return nil, err
	}
	seal := &DatasetSeal{
		ManifestDigest: manifestDigest, DatasetPayloadDigest: m.PayloadDigest,
		AnchorType: anchorType, AnchorID: anchorID,
		AnchorPreimageDigest: sha256Hex(preimage),
		AnchorContentDigest:  sha256Hex(preimage), // git-tag/immutable-object: exact anchor bytes
		SealedBy:             "skill-eval-048-controller",
	}
	if anchorType == "detached-signature" {
		seal.AnchorContentDigest = "" // bound to signature bytes at verification time
	}
	return seal, nil
}

// VerifyDatasetSeal re-derives the manifest digest, payload digest and anchor
// preimage from the completed manifest and fails closed on any mismatch,
// self-reference or post-seal mutation.
func VerifyDatasetSeal(m *DatasetManifestV2, dir string) error {
	if m.Seal == nil {
		return errors.New("manifest is not sealed")
	}
	seal := m.Seal
	mCopy := *m
	mCopy.Seal = nil
	digest, err := CanonicalSHA256(mCopy)
	if err != nil {
		return err
	}
	if digest != seal.ManifestDigest {
		return fmt.Errorf("manifest digest mismatch: seal %s != recomputed %s (post-seal mutation)", seal.ManifestDigest, digest)
	}
	payloadDigest, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		return err
	}
	if payloadDigest != seal.DatasetPayloadDigest || payloadDigest != m.PayloadDigest {
		return errors.New("dataset payload digest mismatch")
	}
	anchor := DatasetAnchorV1{
		SchemaVersion: 1, Canonicalization: CanonicalizationName,
		DatasetID: m.DatasetID, DatasetVersion: m.DatasetVersion,
		ManifestDigest: seal.ManifestDigest, DatasetPayloadDigest: seal.DatasetPayloadDigest,
	}
	preimage, err := CanonicalJSON(anchor)
	if err != nil {
		return err
	}
	if sha256Hex(preimage) != seal.AnchorPreimageDigest {
		return errors.New("anchor preimage digest mismatch")
	}
	if seal.AnchorType == "git-tag" || seal.AnchorType == "immutable-object" {
		if seal.AnchorContentDigest != sha256Hex(preimage) {
			return errors.New("anchor content digest does not match the exact DatasetAnchorV1 bytes")
		}
	}
	return nil
}

// FreezeBeforeDigest is the shared guard for any receipt whose own digest is
// computed last: digest must be empty during preimage computation.
func FreezeBeforeDigest(digest *string) error {
	if digest == nil || *digest != "" {
		return errors.New("freeze-before-digest violated: self digest already set")
	}
	return nil
}
