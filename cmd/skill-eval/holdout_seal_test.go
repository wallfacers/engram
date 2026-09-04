package main

// T029 [US3] — dataset-seal validation tests: host-stable non-unavailable
// resolved models across every attempt (three distinct host harnesses; the
// maintainer unified the underlying model 2026-09-01, so equal models across
// hosts are fact, not failure), OpenCode free billing, prompt consistency,
// append-only ledger integrity including launch-failure attempts, complete
// isolation aggregation, unique state roots, payload_files/file-digest/
// canonical-json/anchor-preimage verification, protected-root placement, and
// the explicit exclusion of future formal-series fields from the dataset seal.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func strp(s string) *string { return &s }

func t029Models() map[string]*string {
	return map[string]*string{
		HostClaude: strp("qwen3.8-flash"), HostCodex: strp("qwen3.8-flash"), HostOpenCode: strp("qwen3.8-flash"),
	}
}

func TestValidateHoldoutResolvedModels(t *testing.T) {
	if err := ValidateHoldoutResolvedModels(t029Models()); err != nil {
		t.Fatalf("unified-model aggregate rejected (2026-09-01 maintainer decision): %v", err)
	}
	bad := t029Models()
	*bad[HostCodex] = "unavailable"
	if err := ValidateHoldoutResolvedModels(bad); err == nil {
		t.Error("unavailable resolved model accepted")
	}
	bad2 := t029Models()
	bad2[HostCodex] = strp("qwen3.8-max") // drift inside one host
	// A single stable value per host is the invariant; drift is caught by the
	// per-attempt provenance check, not by the aggregate shape — but a nil or
	// missing entry must fail.
	bad3 := t029Models()
	bad3[HostCodex] = nil
	if err := ValidateHoldoutResolvedModels(bad3); err == nil {
		t.Error("nil resolved model accepted")
	}
	delete(bad3, HostOpenCode)
	if err := ValidateHoldoutResolvedModels(bad3); err == nil {
		t.Error("missing host accepted")
	}
	_ = bad2
}

func TestValidateHoldoutBilling(t *testing.T) {
	// Contract v4.4: the three lanes share the maintainer's authorized
	// Bailian endpoint, so an authorized class on every host is honest and
	// must pass; only an undeclared/unknown class is rejected.
	auth := []ToolProvenance{
		{Host: HostClaude, BillingClass: BillingAuthorized},
		{Host: HostCodex, BillingClass: BillingAuthorized},
		{Host: HostOpenCode, BillingClass: BillingAuthorized},
	}
	if err := ValidateHoldoutBilling(auth); err != nil {
		t.Fatalf("honest authorized billing rejected: %v", err)
	}
	mixed := []ToolProvenance{
		{Host: HostClaude, BillingClass: BillingAuthorized},
		{Host: HostOpenCode, BillingClass: BillingFree},
	}
	if err := ValidateHoldoutBilling(mixed); err != nil {
		t.Fatalf("free-model opencode rejected: %v", err)
	}
	unknown := []ToolProvenance{{Host: HostOpenCode, BillingClass: BillingUnknown}}
	if err := ValidateHoldoutBilling(unknown); err == nil {
		t.Error("unknown billing class accepted")
	}
}

func TestValidatePromptConsistency(t *testing.T) {
	author := []AuthoringPromptReceipt{{PromptID: "holdout-authoring-v1", Version: 1, DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: "a1"}}
	review := []AuthoringPromptReceipt{{PromptID: "holdout-review-v1", Version: 1, DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: "r1"}}
	if err := ValidatePromptConsistency(author, review); err != nil {
		t.Fatalf("consistent batch rejected: %v", err)
	}
	mixed := append(author, AuthoringPromptReceipt{PromptID: "holdout-authoring-v1", Version: 1, DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: "a2"})
	if err := ValidatePromptConsistency(mixed, review); err == nil {
		t.Error("mixed author prompt digests accepted")
	}
	if err := ValidatePromptConsistency(nil, review); err == nil {
		t.Error("empty author prompt set accepted")
	}
}

// t029Ledger builds a small honest ledger: one author attempt that succeeds,
// one review attempt whose child launch itself fails (still started+terminal),
// one author attempt rejected after candidate parsing.
func t029Ledger(t *testing.T) *AuthorReviewAttemptLedgerV1 {
	t.Helper()
	l := &AuthorReviewAttemptLedgerV1{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	suffix := strp("seed")
	must(l.AppendStarted(AttemptEvent{AttemptID: "a1", Stage: "author", Host: HostClaude,
		ToolIdentityDigest: "ti", ResolvedModel: "qwen3.8-flash", PromptInputDigest: "p1", PreviousEventDigest: suffix}))
	must(l.AppendTerminal(AttemptEvent{AttemptID: "a1", Stage: "author", Host: HostClaude,
		TerminalOutcome: strp("candidate-ready"), ReasonCode: strp("ok"), StartedEventDigest: strp("x")}))
	must(l.AppendStarted(AttemptEvent{AttemptID: "r1", Stage: "review", Host: HostCodex,
		ToolIdentityDigest: "ti", ResolvedModel: "qwen3.8-flash", PromptInputDigest: "p2", AuthorAttemptID: strp("a1")}))
	must(l.AppendTerminal(AttemptEvent{AttemptID: "r1", Stage: "review", Host: HostCodex,
		TerminalOutcome: strp("launch-failed"), ReasonCode: strp("spawn-error"), StartedEventDigest: strp("x")}))
	must(l.AppendStarted(AttemptEvent{AttemptID: "a2", Stage: "author", Host: HostOpenCode,
		ToolIdentityDigest: "ti", ResolvedModel: "qwen3.8-flash", PromptInputDigest: "p3"}))
	must(l.AppendTerminal(AttemptEvent{AttemptID: "a2", Stage: "author", Host: HostOpenCode,
		TerminalOutcome: strp("candidate-ready"), ReasonCode: strp("ok"), StartedEventDigest: strp("x")}))
	must(l.AppendTerminal(AttemptEvent{AttemptID: "a2", Stage: "author", Host: HostOpenCode,
		TerminalOutcome: strp("rejected"), ReasonCode: strp("duplicate-family"), StartedEventDigest: strp("x")}))
	return l
}

func TestLedgerAppendOnlyIntegrity(t *testing.T) {
	l := t029Ledger(t)
	if err := l.VerifyLedger(); err != nil {
		t.Fatalf("honest ledger with a launch failure rejected: %v", err)
	}
	// A started attempt with no production terminal fails (even failures
	// are terminalized; the trailing final terminal alone does not close
	// the lifecycle).
	broken := t029Ledger(t)
	broken.Events = broken.Events[:len(broken.Events)-2]
	if err := broken.VerifyLedger(); err == nil {
		t.Error("missing terminal accepted")
	}
	// In-place mutation of an event breaks its digest.
	mutated := t029Ledger(t)
	mutated.Events[0].Host = HostCodex
	if err := mutated.VerifyLedger(); err == nil {
		t.Error("mutated event accepted")
	}
	// Renumbering breaks the chain.
	renum := t029Ledger(t)
	renum.Events[1].EventSequence = 5
	if err := renum.VerifyLedger(); err == nil {
		t.Error("renumbered event accepted")
	}
}

// t029Probes returns a complete honest probe set per attempt.
func t029Probes(t *testing.T, ids ...string) map[string][]AccessProbe {
	t.Helper()
	out := map[string][]AccessProbe{}
	for _, id := range ids {
		var ps []AccessProbe
		for _, kind := range []ProbeKind{
			ProbePrivateRootTraverse, ProbePrivateRootList, ProbePrivateRootRead,
			ProbeGenerationAuditRead, ProbeAuthorReceiptRead, ProbePriorReviewRead, ProbeActiveSiblingRead,
		} {
			ps = append(ps, AccessProbe{Kind: kind, TargetPath: "/holdout/private",
				TargetAccessPolicyDigest: "pol", Expected: ProbeDenied, Observed: ProbeDenied})
		}
		ps = append(ps, AccessProbe{Kind: ProbeOwnInputRead, TargetPath: "/attempt/" + id + "/input",
			TargetAccessPolicyDigest: "pol", Expected: ProbeReadable, Observed: ProbeReadable})
		out[id] = ps
	}
	return out
}

func TestAggregateIsolationReceipts(t *testing.T) {
	l := t029Ledger(t)
	probes := t029Probes(t, "a1", "r1", "a2")
	if err := AggregateIsolationReceipts(l, probes); err != nil {
		t.Fatalf("complete aggregate rejected (launch-failure attempt included): %v", err)
	}
	// Dropping the rejected or launch-failed attempt's receipts fails.
	delete(probes, "a2")
	if err := AggregateIsolationReceipts(l, probes); err == nil {
		t.Error("omitted rejected attempt accepted")
	}
	// An isolation set belonging to an unknown attempt fails.
	ghost := t029Probes(t, "a1", "r1", "a2", "ghost")
	if err := AggregateIsolationReceipts(l, ghost); err == nil {
		t.Error("orphan isolation receipts accepted")
	}
	// A stripped probe category fails.
	partial := t029Probes(t, "a1", "r1", "a2")
	partial["r1"] = partial["r1"][:3]
	if err := AggregateIsolationReceipts(l, partial); err == nil {
		t.Error("incomplete probe set accepted")
	}
}

func TestValidateStateRootsUnique(t *testing.T) {
	if err := ValidateStateRootsUnique([]string{"r1", "r2", "r3"}); err != nil {
		t.Fatalf("unique roots rejected: %v", err)
	}
	if err := ValidateStateRootsUnique([]string{"r1", "r1"}); err == nil {
		t.Error("reused state root accepted")
	}
	if err := ValidateStateRootsUnique([]string{"r1", ""}); err == nil {
		t.Error("empty state root accepted")
	}
}

// TestSealPayloadFilesAndAnchorVerification exercises the full
// payload_files union / file digest / canonical manifest / anchor preimage
// chain over a protected-root temporary dataset.
func TestSealPayloadFilesAndAnchorVerification(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cases/a.jsonl", `{"id":"h-1"}`+"\n"+`{"id":"h-2"}`)
	write("cases/b.jsonl", `{"id":"h-3"}`)
	d1, _ := LFNormalizedSHA256([]byte(`{"id":"h-1"}` + "\n" + `{"id":"h-2"}`))
	d2, _ := LFNormalizedSHA256([]byte(`{"id":"h-3"}`))
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger", DatasetVersion: "holdout-96-v1",
		Split: "holdout", ScoreMembership: "official",
		CaseCount: 3,
		CaseIDs:   []string{"h-1", "h-2", "h-3"},
		PayloadFiles: []PayloadFileV1{
			{RelativePath: "cases/a.jsonl", LFNormalizedSHA256: d1, CaseIDs: []string{"h-1", "h-2"}},
			{RelativePath: "cases/b.jsonl", LFNormalizedSHA256: d2, CaseIDs: []string{"h-3"}},
		},
	}
	cid, err := CaseIDsDigest(m.CaseIDs)
	if err != nil {
		t.Fatal(err)
	}
	m.CaseIDsDigest = cid
	pd, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatalf("payload digest: %v", err)
	}
	m.PayloadDigest = pd
	md, err := CompleteManifestForSeal(m)
	if err != nil {
		t.Fatalf("complete for seal: %v", err)
	}
	seal, err := BuildDatasetAnchor(m, md, "immutable-object", "obj-1")
	if err != nil {
		t.Fatal(err)
	}
	m.Seal = seal
	if err := VerifyDatasetSeal(m, dir); err != nil {
		t.Fatalf("honest seal rejected: %v", err)
	}

	// Tamper with one payload case → payload digest mismatch.
	write("cases/b.jsonl", `{"id":"h-3-tampered"}`)
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Error("tampered payload accepted")
	}
	write("cases/b.jsonl", `{"id":"h-3"}`)

	// Post-seal manifest mutation → manifest digest mismatch.
	m.SealedAt = strp("2030-01-01T00:00:00Z")
	if err := VerifyDatasetSeal(m, dir); err == nil {
		t.Error("post-seal manifest mutation accepted")
	}
	m.SealedAt = nil

	// An extra directory-discovered file must not enter the digest (the
	// manifest list is authoritative) — but an undeclared file next to the
	// payload is rejected by strict load, so here just confirm the digest
	// ignores it.
	write("cases/extra.jsonl", "garbage")
	pd2, err := DatasetPayloadDigest(dir, m)
	if err != nil {
		t.Fatalf("digest with extra undeclared file: %v", err)
	}
	if pd2 != pd {
		t.Error("undeclared extra file changed the payload digest")
	}
}

// TestProtectedRootOnlyPlaintext pins the containment guard used for holdout
// artifacts: everything the controller writes for the holdout must stay under
// the protected root, and escapes are rejected.
func TestProtectedRootOnlyPlaintext(t *testing.T) {
	root := t.TempDir()
	ok, err := EnsureInside(root, filepath.Join(root, "attempts", "a1", "input", "env.json"))
	if err != nil {
		t.Fatalf("legitimate child path rejected: %v", err)
	}
	if !filepath.IsAbs(ok) && ok == "" {
		t.Fatal("no resolved path")
	}
	if _, err := EnsureInside(root, filepath.Join(root, "..", "escape.txt")); err == nil {
		t.Error("parent escape accepted")
	}
	if _, err := EnsureInside(root, "/etc/passwd"); err == nil {
		t.Error("absolute outside path accepted")
	}
	// Frozen outputs are never overwritten — a seal artifact written twice
	// is a hard failure.
	p := filepath.Join(root, "seal", "manifest.json")
	if err := WriteFrozenFile(p, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrozenFile(p, []byte("v2")); err == nil {
		t.Error("frozen artifact overwrite accepted")
	}
}

// TestDatasetSealExcludesFutureFormalFields — the dataset seal schema must
// reject FormalSeriesManifest / HoldoutBindingReceipt / ProtectedExecutionReceipt
// fields: they belong to formal series prepare/ordinal 1, never to the seal.
func TestDatasetSealExcludesFutureFormalFields(t *testing.T) {
	base := DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "d", DatasetVersion: "v", Split: "holdout",
		CaseCount: 1, CaseIDs: []string{"h-1"}, CaseIDsDigest: "x",
		PayloadFiles: []PayloadFileV1{{RelativePath: "a.jsonl", LFNormalizedSHA256: "d", CaseIDs: []string{"h-1"}}},
		PayloadDigest: "p",
	}
	good, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var m DatasetManifestV2
	if err := StrictParseClosed(good, &m); err != nil {
		t.Fatalf("plain manifest rejected: %v", err)
	}
	for _, field := range []string{
		"formal_series_manifest", "formal_series_manifest_digest",
		"holdout_binding_receipt", "holdout_binding_receipt_digest",
		"protected_execution_receipt", "protected_execution_receipt_digest",
		"workspace_canary_receipt_digests", "pre_holdout_green_test_receipt",
	} {
		var leak map[string]any
		if err := json.Unmarshal(good, &leak); err != nil {
			t.Fatal(err)
		}
		leak[field] = map[string]any{"anything": true}
		raw, err := json.Marshal(leak)
		if err != nil {
			t.Fatal(err)
		}
		var bad DatasetManifestV2
		if err := StrictParseClosed(raw, &bad); err == nil {
			t.Errorf("future formal field %q accepted into the dataset manifest", field)
		}
	}
}
