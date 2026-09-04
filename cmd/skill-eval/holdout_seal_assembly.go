package main

// T034 tail + T035 validation — private manifest assembly, dataset sealing,
// and the holdout-split validator (skill-eval validate --split holdout).
// The sealed payload carries only the 96 admitted cases with private lineage
// (authoring receipts, reviews) stripped; the aggregate digests (ledger,
// admission chain, isolation, state roots, resolved models) come from the
// batch state under the protected root.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Seal assembles the payload + manifest, anchors and verifies it, and writes
// the sealed manifest under <root>/sealed/manifest.json.
func (b *HoldoutBatch) Seal() (*DatasetManifestV2, error) {
	slots, err := HoldoutQuotaSlots()
	if err != nil {
		return nil, err
	}
	if len(b.persist.Filled) != len(slots) {
		return nil, fmt.Errorf("batch incomplete: %d/%d slots filled", len(b.persist.Filled), len(slots))
	}
	if err := b.persist.Ledger.VerifyLedger(); err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	if _, err := ReplayAdmissionChain(b.persist.Admissions); err != nil {
		return nil, fmt.Errorf("admission chain: %w", err)
	}
	if err := AggregateIsolationReceipts(b.persist.Ledger, b.persist.Probes); err != nil {
		return nil, fmt.Errorf("isolation aggregate: %w", err)
	}
	if err := ValidateStateRootsUnique(b.persist.StateRoots); err != nil {
		return nil, fmt.Errorf("state roots: %w", err)
	}
	// Payload: canonical case lines, private lineage stripped.
	payloadDir := filepath.Join(b.cfg.Root, "payload")
	if err := os.MkdirAll(payloadDir, 0o700); err != nil {
		return nil, err
	}
	type sealedCase struct {
		IDs    []string
		Lines  []string
	}
	byModule := map[string]*sealedCase{}
	var moduleNames []string
	seen := map[string]int{}
	for _, s := range slots {
		k := slotKey(s)
		c := b.persist.Filled[FilledSlotKey(k, seen[k])]
		seen[k]++
		if c == nil {
			return nil, fmt.Errorf("slot %s unfilled", FilledSlotKey(k, seen[k]-1))
		}
		pub := *c
		pub.Authoring = nil
		pub.Reviews = nil
		line, err := CanonicalJSON(pub)
		if err != nil {
			return nil, err
		}
		if byModule[c.Module] == nil {
			byModule[c.Module] = &sealedCase{}
			moduleNames = append(moduleNames, c.Module)
		}
		byModule[c.Module].IDs = append(byModule[c.Module].IDs, c.ID)
		byModule[c.Module].Lines = append(byModule[c.Module].Lines, string(line))
	}
	sort.Strings(moduleNames)
	var payloadFiles []PayloadFileV1
	for _, m := range moduleNames {
		mc := byModule[m]
		rel := "payload/" + m + ".jsonl"
		data := []byte(joinLines(mc.Lines))
		if err := os.WriteFile(filepath.Join(b.cfg.Root, filepath.FromSlash(rel)), data, 0o600); err != nil {
			return nil, err
		}
		d, err := LFNormalizedSHA256(data)
		if err != nil {
			return nil, err
		}
		ids := append([]string{}, mc.IDs...)
		sort.Strings(ids)
		payloadFiles = append(payloadFiles, PayloadFileV1{RelativePath: rel, LFNormalizedSHA256: d, CaseIDs: ids})
	}
	caseIDs := make([]string, 0, 96)
	for _, pf := range payloadFiles {
		caseIDs = append(caseIDs, pf.CaseIDs...)
	}
	sort.Strings(caseIDs)
	cid, err := CaseIDsDigest(caseIDs)
	if err != nil {
		return nil, err
	}
	// Manifest counters recomputed from the admitted cases.
	modCounts := map[string]int{}
	langCounts := map[string]int{}
	authorCounts := map[string]int{}
	bucketCounts := map[string]int{}
	seen2 := map[string]int{}
	for _, s := range slots {
		c := b.persist.Filled[FilledSlotKey(slotKey(s), seen2[slotKey(s)])]
		seen2[slotKey(s)]++
		modCounts[c.Module]++
		langCounts[*c.Lang]++
		authorCounts[s.Author]++
		bucketCounts[*c.ScenarioBucket]++
	}
	pd, err := DatasetPayloadDigest(b.cfg.Root, &DatasetManifestV2{PayloadFiles: payloadFiles, CaseIDs: caseIDs})
	if err != nil {
		return nil, err
	}
	// Aggregate digests.
	ledgerDigest, err := CanonicalSHA256(b.persist.Ledger)
	if err != nil {
		return nil, err
	}
	admissions := b.persist.Admissions
	admissionCount, committed := len(admissions), 0
	for _, r := range admissions {
		if r.CASResult == "committed" {
			committed++
		}
	}
	admDigest, err := CanonicalSHA256(admissions)
	if err != nil {
		return nil, err
	}
	probesDigest, err := CanonicalSHA256(b.persist.Probes)
	if err != nil {
		return nil, err
	}
	rootsDigest, err := CanonicalSHA256(b.persist.StateRoots)
	if err != nil {
		return nil, err
	}
	started, terminals, reasons := 0, 0, map[string]int{}
	for _, e := range b.persist.Ledger.Events {
		switch e.EventKind {
		case EventAttemptStarted:
			started++
		case EventAttemptTerminal:
			terminals++
			if e.ReasonCode != nil {
				reasons[*e.ReasonCode]++
			}
		}
	}
	resolved := map[string]*string{}
	for _, p := range append(append([]ToolProvenance{}, b.persist.AuthorProvs...), b.persist.ReviewProvs...) {
		if resolved[p.Host] == nil {
			resolved[p.Host] = &p.ResolvedModel
		} else if *resolved[p.Host] != p.ResolvedModel {
			return nil, fmt.Errorf("host %s model drift %s vs %s", p.Host, *resolved[p.Host], p.ResolvedModel)
		}
	}
	if err := ValidateHoldoutResolvedModels(resolved); err != nil {
		return nil, err
	}
	if err := ValidateHoldoutBilling(append(b.persist.AuthorProvs, b.persist.ReviewProvs...)); err != nil {
		return nil, err
	}
	if err := ValidatePromptConsistency(
		repeatReceipt(*b.authorP, len(b.persist.AuthorProvs)),
		repeatReceipt(*b.reviewP, len(b.persist.ReviewProvs))); err != nil {
		return nil, err
	}
	finalState, err := ReplayAdmissionChain(admissions)
	if err != nil {
		return nil, err
	}
	if finalState.Revision != b.persist.Accepted.Revision {
		return nil, errors.New("replayed final state diverges from the batch state")
	}
	stateDigest, err := b.persist.Accepted.StateDigest()
	if err != nil {
		return nil, err
	}
	sumDigest, err := AcceptedSummaryFor(b.persist.Accepted).ComputePayloadDigest()
	if err != nil {
		return nil, err
	}
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger", DatasetVersion: "holdout-96-v1",
		Split: "holdout", ScoreMembership: MembershipHoldout96,
		CaseCount: len(caseIDs), ModuleCounts: modCounts, LanguageCounts: langCounts,
		AuthorCounts: authorCounts, ScenarioBucketCounts: bucketCounts,
		CaseIDs: caseIDs, CaseIDsDigest: cid,
		PayloadFiles: payloadFiles, PayloadDigest: pd,
		AuthorReviewResolvedModels: resolved,
		AuthorPrompt: b.authorP, ReviewPrompt: b.reviewP,
		AuthorReviewStateRootsDigest: &rootsDigest,
		AuthorReviewIsolationDigest:  &probesDigest,
		AuthorReviewAttemptEventChainDigest: &ledgerDigest,
		AuthorReviewAttemptStartedCount:     &started,
		AuthorReviewAttemptTerminalCount:    &terminals,
		AuthorReviewAttemptReasonCounts:     reasons,
		AdmissionChainDigest:      &admDigest,
		AdmissionReceiptCount:     &admissionCount,
		CommittedAdmissionCount:   &committed,
		AcceptedFamilyRevision:    &b.persist.Accepted.Revision,
		AcceptedFamilyStateDigest: &stateDigest,
		AcceptedFamilySummaryDigest: &sumDigest,
	}
	md, err := CompleteManifestForSeal(m)
	if err != nil {
		return nil, err
	}
	seal, err := BuildDatasetAnchor(m, md, b.cfg.AnchorType, b.cfg.AnchorID)
	if err != nil {
		return nil, err
	}
	m.Seal = seal
	if err := VerifyDatasetSeal(m, b.cfg.Root); err != nil {
		return nil, fmt.Errorf("sealed manifest failed self-verification: %w", err)
	}
	raw, err := CanonicalJSON(m)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(b.cfg.Root, "sealed", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return nil, err
	}
	if err := WriteFrozenFile(out, raw); err != nil {
		return nil, err
	}
	return m, nil
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out + "\n"
}

func repeatReceipt(r AuthoringPromptReceipt, n int) []AuthoringPromptReceipt {
	if n == 0 {
		n = 1
	}
	out := make([]AuthoringPromptReceipt, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// HoldoutValidation loads a sealed holdout manifest + payload and fails
// closed on the exact 96 matrix, the author/language/bucket quotas, the
// seals, and the deterministic machine rules.
func HoldoutValidation(root string, manifestPath string) ValidationReport {
	rep := ValidationReport{OK: true}
	m, err := LoadDatasetManifest(manifestPath)
	if err != nil {
		rep.addf(false, "manifest load: %v", err)
		return rep
	}
	if err := VerifyDatasetSeal(m, root); err != nil {
		rep.addf(false, "seal: %v", err)
	}
	// Load every payload case.
	cases := map[string]*TriggerCaseV2{}
	for _, pf := range m.PayloadFiles {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pf.RelativePath)))
		if err != nil {
			rep.addf(false, "payload %s: %v", pf.RelativePath, err)
			continue
		}
		if err := jsonLinesEach(raw, func(line []byte) {
			var c TriggerCaseV2
			if err := StrictParseClosed(line, &c); err != nil {
				rep.addf(false, "payload case parse: %v", err)
				return
			}
			if c.Authoring != nil || c.Reviews != nil {
				rep.addf(false, "case %s carries private lineage in the sealed payload", c.ID)
			}
			cases[c.ID] = &c
		}); err != nil {
			rep.addf(false, "payload %s: %v", pf.RelativePath, err)
		}
	}
	rep.addf(len(cases) == 96, "case count %d, want 96", len(cases))
	// Rebuild the slot view and check the exact frozen quota. The author
	// dimension is sealed in the manifest author_counts (the payload itself
	// is author-anonymous); module/lang/scenario are recomputed per case.
	slots, err := HoldoutQuotaSlots()
	if err != nil {
		rep.addf(false, "quota table: %v", err)
		return rep
	}
	if err := ValidateQuotaSlots(slots); err != nil {
		rep.addf(false, "quota table: %v", err)
	}
	bySlot := map[string]int{}
	for _, c := range cases {
		if c.Split != "holdout" || c.ScoreMembership != MembershipHoldout96 {
			rep.addf(false, "case %s split/membership %s/%s invalid", c.ID, c.Split, c.ScoreMembership)
		}
		if err := ValidateCaseV2(c); err != nil {
			rep.addf(false, "case %s: %v", c.ID, err)
			continue
		}
		key := fmt.Sprintf("*/%s/%s/%s", c.Module, *c.Lang, *c.ScenarioBucket)
		bySlot[key]++
	}
	want := map[string]int{}
	for _, s := range slots {
		want[fmt.Sprintf("*/%s/%s/%s", s.Module, s.Lang, s.Scenario)]++
	}
	for k, n := range want {
		rep.addf(bySlot[k] == n, "module/lang/scenario %s: %d cases (frozen: %d)", k, bySlot[k], n)
	}
	for mod, n := range m.ModuleCounts {
		rep.addf(n == holdoutModuleCounts[mod], "manifest module_counts[%s]=%d (frozen: %d)", mod, n, holdoutModuleCounts[mod])
	}
	rep.addf(m.LanguageCounts[LangZh] == 48 && m.LanguageCounts[LangEn] == 48,
		"manifest language zh/en = %d/%d, want 48/48", m.LanguageCounts[LangZh], m.LanguageCounts[LangEn])
	for h, n := range m.AuthorCounts {
		rep.addf(n == 32, "manifest author_counts[%s]=%d, want 32", h, n)
	}
	for _, b := range HoldoutScenarioBuckets {
		rep.addf(m.ScenarioBucketCounts[b] == 12, "bucket %s count %d, want 12", b, m.ScenarioBucketCounts[b])
	}
	return rep
}

func jsonLinesEach(raw []byte, fn func(line []byte)) error {
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		fn([]byte(line))
	}
	return nil
}
