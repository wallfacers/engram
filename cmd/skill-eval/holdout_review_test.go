package main

// T028 [US3] — label-blind review-envelope serialization tests: the envelope
// leaks no author/quota/batch/ordinal/provenance/private-digest/prior-review
// field and none of the author-proposed expect/module/lang/scenario/category/
// machine-rule fields; nested aliases/extensions, duplicate JSON keys and
// unknown recursive fields are rejected; identical blind projections yield
// identical canonical digests regardless of private labels/rules/slots; the
// family-summary payloads are materialized and source-bound.

import (
	"encoding/json"
	"strings"
	"testing"
)

func t028Case() *TriggerCaseV2 {
	lang := LangZh
	scen := "durable-preference"
	p := "帮我把这个项目的依赖装一下"
	max1 := 1
	return &TriggerCaseV2{
		ID: "iw-pos-9001", SchemaVersion: 2, Split: "holdout", ScoreMembership: "official",
		Module: "implicit-read-pos", Lang: &lang, ScenarioBucket: &scen,
		Category: "package-manager",
		Prompt:   &p,
		SeedMemories: []SeedMemory{
			{Name: "pkg-manager", Content: "包管理器用 pnpm，不用 npm"},
		},
		WorkspaceFiles: []WorkspaceFile{
			{Path: "notes/tools.md", Content: "team uses pnpm", SHA256: "ab"},
			{Path: "README.md", Content: "hello", SHA256: "cd"},
		},
		Expect: ExpectV2{
			Trigger: true, AllowedOps: []string{"search"}, MinCalls: &max1, MaxCalls: &max1,
			AnswerInclude: []Alternation{{"pnpm"}}, Observable: "recalled preference",
		},
		Source: "claude-authoring", Status: "candidate",
	}
}

func t028Envelope(t *testing.T) ReviewEnvelope {
	t.Helper()
	c := t028Case()
	bc := BuildBlindCandidate(c)
	d, err := BlindCandidateDigest(bc)
	if err != nil {
		t.Fatal(err)
	}
	rev := 1
	dev := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "dev-regression", Revision: 7,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: "dev-index-digest", SourceFamilyCount: 1,
		Entries: []FamilySummaryEntry{{
			FamilyID: "devfam-1", LanguageMembers: []string{LangZh},
			BlindSemanticPayloads: []string{"pnpm"},
			EntryDigest:           "e1",
		}},
		EntriesRootDigest: "e1",
	}
	return ReviewEnvelope{
		Candidate:               *bc,
		BlindCandidateDigest:    d,
		ReviewPromptDigest:      "rp",
		DevFamilySummary:        *dev,
		DevFamilySummaryDigest:  "devsum",
		AcceptedHoldoutFamilyRevision: &rev,
		EnvelopeDigest:          "env",
	}
}

func t028Raw(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(t028Envelope(t))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReviewEnvelopeCanonicalSerializationAccepted(t *testing.T) {
	if err := ValidateReviewEnvelopeJSON(t028Raw(t)); err != nil {
		t.Fatalf("canonical envelope rejected: %v", err)
	}
}

// t028Inject mutates the raw JSON by injecting an extra top-level or nested
// field and asserts rejection.
func t028Inject(t *testing.T, name, path, key string, value any) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(t028Raw(t), &m); err != nil {
			t.Fatal(err)
		}
		node := m
		for _, seg := range strings.Split(path, ".") {
			if seg == "" {
				continue
			}
			next, ok := node[seg].(map[string]any)
			if !ok {
				t.Fatalf("path segment %q not an object", seg)
			}
			node = next
		}
		node[key] = value
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReviewEnvelopeJSON(raw); err == nil {
			t.Errorf("envelope with %s=%v accepted", key, value)
		}
	})
}

func TestReviewEnvelopeRejectsAuthorAndProvenanceLeak(t *testing.T) {
	// Top-level leaks: identity, slot, batch, provenance, prior state.
	t028Inject(t, "author", "", "author", "claude")
	t028Inject(t, "author-model", "", "author_model", "qwen3.8-flash")
	t028Inject(t, "quota-slot", "", "quota_slot", "claude/iw-pos/zh/durable-preference")
	t028Inject(t, "quota-slot-digest", "", "quota_slot_digest", "abc")
	t028Inject(t, "batch-id", "", "batch_id", "b1")
	t028Inject(t, "source", "", "source", "claude-authoring")
	t028Inject(t, "ordinal", "", "attempt_ordinal", 3)
	t028Inject(t, "provenance", "", "provenance", map[string]any{"cli": "1.0"})
	t028Inject(t, "private-candidate-digest", "", "private_candidate_digest", "dead")
	t028Inject(t, "authoring-receipt", "", "authoring_receipt", map[string]any{})
	t028Inject(t, "prior-review", "", "prior_review", map[string]any{})
	t028Inject(t, "id", "", "id", "iw-pos-9001")
	t028Inject(t, "family-proposal", "", "family_id", "fam-9")
}

func TestReviewEnvelopeRejectsProposedLabelsAtTopLevel(t *testing.T) {
	t028Inject(t, "expect", "", "expect", map[string]any{"trigger": true})
	t028Inject(t, "module", "", "module", "implicit-read-pos")
	t028Inject(t, "lang", "", "lang", LangZh)
	t028Inject(t, "scenario", "", "scenario_bucket", "durable-preference")
	t028Inject(t, "category", "", "category", "package-manager")
	t028Inject(t, "machine-rules", "", "machine_rules", map[string]any{"max_calls": 1})
	t028Inject(t, "translation", "", "translation_of", "iw-pos-0001")
	t028Inject(t, "status", "", "status", "candidate")
}

func TestReviewEnvelopeRejectsLabelsInsideCandidate(t *testing.T) {
	t028Inject(t, "candidate-module", "candidate", "module", "implicit-read-pos")
	t028Inject(t, "candidate-lang", "candidate", "lang", LangZh)
	t028Inject(t, "candidate-scenario", "candidate", "scenario_bucket", "durable-preference")
	t028Inject(t, "candidate-category", "candidate", "category", "package-manager")
	t028Inject(t, "candidate-expect", "candidate", "expect", map[string]any{"trigger": true})
	t028Inject(t, "candidate-id", "candidate", "id", "iw-pos-9001")
	t028Inject(t, "candidate-family", "candidate", "family_id", "fam-9")
	t028Inject(t, "candidate-safe-context", "candidate", "safe_context", "for judging only")
}

func TestReviewEnvelopeRejectsNestedAliasesAndExtensions(t *testing.T) {
	// Inject into nested arrays via a targeted rebuild.
	t.Run("turn-extra-field", func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(t028Raw(t), &m); err != nil {
			t.Fatal(err)
		}
		m["candidate"].(map[string]any)["turns"] = []any{
			map[string]any{"session": 1, "role": "user", "content": "hi", "extension": "x"},
		}
		raw, _ := json.Marshal(m)
		if err := ValidateReviewEnvelopeJSON(raw); err == nil {
			t.Error("turn extension field accepted")
		}
	})
	t.Run("seed-extra-field", func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(t028Raw(t), &m); err != nil {
			t.Fatal(err)
		}
		m["candidate"].(map[string]any)["seed_memories"] = []any{
			map[string]any{"name": "n", "content": "c", "category": "leak"},
		}
		raw, _ := json.Marshal(m)
		if err := ValidateReviewEnvelopeJSON(raw); err == nil {
			t.Error("seed alias field accepted")
		}
	})
	t.Run("workspace-extra-field", func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(t028Raw(t), &m); err != nil {
			t.Fatal(err)
		}
		m["candidate"].(map[string]any)["workspace_files"] = []any{
			map[string]any{"path": "a", "content": "b", "sha256": "c", "module": "leak"},
		}
		raw, _ := json.Marshal(m)
		if err := ValidateReviewEnvelopeJSON(raw); err == nil {
			t.Error("workspace extension field accepted")
		}
	})
	t.Run("summary-entry-extra-field", func(t *testing.T) {
		var m map[string]any
		if err := json.Unmarshal(t028Raw(t), &m); err != nil {
			t.Fatal(err)
		}
		dev := m["dev_family_summary"].(map[string]any)
		dev["entries"] = []any{
			map[string]any{"family_id": "d", "language_members": []any{LangZh},
				"blind_semantic_payloads": []any{"x"}, "entry_digest": "e", "nearest": "leak"},
		}
		raw, _ := json.Marshal(m)
		if err := ValidateReviewEnvelopeJSON(raw); err == nil {
			t.Error("summary entry extension accepted")
		}
	})
}

func TestReviewEnvelopeRejectsDuplicateKeys(t *testing.T) {
	// Hand-written raw JSON: the smuggled duplicate "blind_candidate_digest"
	// would silently override the canonical one under encoding/json.
	raw := `{
	  "candidate": {"schema_version":"blind-candidate-v1","prompt":"p","seed_memories":[],"workspace_files":[]},
	  "blind_candidate_digest": "legit",
	  "blind_candidate_digest": "smuggled",
	  "review_prompt_digest": "rp",
	  "dev_family_summary": {"schema_version":1,"scope":"dev-regression","revision":7,
	    "projection_version":"blind-family-summary-v1","source_state_digest":"d",
	    "source_family_count":0,"entries":[],"entries_root_digest":"r","payload_digest":"p"},
	  "dev_family_summary_digest": "ds",
	  "envelope_digest": "e"
	}`
	if err := ValidateReviewEnvelopeJSON([]byte(raw)); err == nil {
		t.Fatal("duplicate top-level key accepted")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate-key error, got %v", err)
	}
	// Nested duplicate inside a candidate turn.
	nested := strings.Replace(raw, `"prompt":"p"`, `"prompt":"p","prompt":"q"`, 1)
	if err := ValidateReviewEnvelopeJSON([]byte(nested)); err == nil {
		t.Fatal("nested duplicate key accepted")
	}
}

func TestReviewEnvelopeRequiresPromptXTurns(t *testing.T) {
	base := `{
	  "candidate": {"schema_version":"blind-candidate-v1","seed_memories":[],"workspace_files":[]},
	  "blind_candidate_digest": "x", "review_prompt_digest": "rp",
	  "dev_family_summary": {"schema_version":1,"scope":"dev-regression","revision":7,
	    "projection_version":"blind-family-summary-v1","source_state_digest":"d",
	    "source_family_count":0,"entries":[],"entries_root_digest":"r","payload_digest":"p"},
	  "dev_family_summary_digest": "ds", "envelope_digest": "e"
	}`
	if err := ValidateReviewEnvelopeJSON([]byte(base)); err == nil {
		t.Error("candidate with neither prompt nor turns accepted")
	}
	both := strings.Replace(base, `"seed_memories":[]`, `"prompt":"p","turns":[],"seed_memories":[]`, 1)
	if err := ValidateReviewEnvelopeJSON([]byte(both)); err == nil {
		t.Error("candidate with both prompt and turns accepted")
	}
}

// TestBlindDigestIdenticalAcrossPrivateLabels — two private candidates that
// differ only in labels/rules/slots/IDs share one reviewer-visible digest.
func TestBlindDigestIdenticalAcrossPrivateLabels(t *testing.T) {
	a := t028Case()
	b := t028Case() // same blind projection…
	// …different private labels, rules, ids, slot, status.
	otherLang, otherScen := LangEn, "identity-biography"
	b.ID, b.Module, b.Lang, b.ScenarioBucket = "different-id", "implicit-write-neg", &otherLang, &otherScen
	b.Category = "editor"
	max9 := 9
	b.Expect = ExpectV2{Trigger: false, AllowedOps: []string{"write"}, MaxCalls: &max9, Observable: "other"}
	b.Source, b.Status = "codex-authoring", "rejected"
	da, err := BlindCandidateDigest(BuildBlindCandidate(a))
	if err != nil {
		t.Fatal(err)
	}
	db, err := BlindCandidateDigest(BuildBlindCandidate(b))
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("identical blind projections produced different digests %s vs %s", da, db)
	}
	// Workspace-file order in the private case must not leak into the digest.
	c := t028Case()
	c.WorkspaceFiles = []WorkspaceFile{c.WorkspaceFiles[1], c.WorkspaceFiles[0]}
	dc, err := BlindCandidateDigest(BuildBlindCandidate(c))
	if err != nil {
		t.Fatal(err)
	}
	if da != dc {
		t.Error("workspace file order changed the blind digest (must be path-sorted)")
	}
	// A one-word content difference must change it.
	d := t028Case()
	d.Prompt = strPtr("帮我把这个项目的依赖装好")
	dd, err := BlindCandidateDigest(BuildBlindCandidate(d))
	if err != nil {
		t.Fatal(err)
	}
	if da == dd {
		t.Error("different prompt produced the same blind digest")
	}
}

// TestFamilySummaryReprojectionFailClosed pins the source-bound materialized
// payload: digest-only or dropped-family recomputation is invalid.
func TestFamilySummaryReprojectionFailClosed(t *testing.T) {
	newEntry := func(id string) FamilySummaryEntry {
		return FamilySummaryEntry{
			FamilyID: id, LanguageMembers: []string{LangZh},
			BlindSemanticPayloads: []string{"payload-" + id},
		}
	}
	build := func(ids ...string) (*FamilySummaryPayload, map[string]FamilySummaryEntry) {
		fams := map[string]FamilySummaryEntry{}
		for _, id := range ids {
			fams[id] = newEntry(id)
		}
		entries := make([]FamilySummaryEntry, 0, len(fams))
		for _, id := range ids {
			e := newEntry(id)
			e.EntryDigest = sha256Hex([]byte(strings.Join([]string{
				e.FamilyID, joinSorted(e.LanguageMembers), joinSorted(e.BlindSemanticPayloads),
			}, "\x00")))
			entries = append(entries, e)
		}
		p := &FamilySummaryPayload{
			SchemaVersion: 1, Scope: "dev-regression", Revision: 3,
			ProjectionVersion: BlindFamilySummaryProjection,
			SourceStateDigest: "state", SourceFamilyCount: len(fams),
			Entries: entries,
		}
		p.EntriesRootDigest = EntriesRootDigest(entries)
		p.PayloadDigest, _ = p.ComputePayloadDigest()
		return p, fams
	}
	p, fams := build("a", "b", "c")
	if err := ReprojectFamilySummary(p, fams, "state"); err != nil {
		t.Fatalf("faithful reprojection rejected: %v", err)
	}
	// Dropping one source family and recomputing summary-local digests is
	// still invalid: the source count binds.
	dropped, _ := build("a", "b")
	if err := ReprojectFamilySummary(dropped, fams, "state"); err == nil {
		t.Fatal("summary with a dropped source family accepted")
	}
	// A source-state digest swap is invalid.
	swapped, _ := build("a", "b", "c")
	if err := ReprojectFamilySummary(swapped, fams, "other-state"); err == nil {
		t.Fatal("mismatched source_state_digest accepted")
	}
	// Post-hoc payload mutation (digest no longer matches content).
	mutated, mf := build("a", "b", "c")
	mutated.Entries[0].BlindSemanticPayloads = []string{"tampered"}
	if err := ReprojectFamilySummary(mutated, mf, "state"); err == nil {
		t.Fatal("tampered entry accepted")
	}
}

func TestNearestFamilyReferenced(t *testing.T) {
	dev := &FamilySummaryPayload{Entries: []FamilySummaryEntry{{FamilyID: "devfam-1"}}}
	acc := &FamilySummaryPayload{Entries: []FamilySummaryEntry{{FamilyID: "hfam-2"}}}
	ok := strPtr("devfam-1")
	if err := NearestFamilyReferenced(ReviewRecord{Novel: false, NearestFamilyID: ok}, dev, acc); err != nil {
		t.Errorf("dev reference rejected: %v", err)
	}
	ok2 := strPtr("hfam-2")
	if err := NearestFamilyReferenced(ReviewRecord{Novel: false, NearestFamilyID: ok2}, dev, acc); err != nil {
		t.Errorf("accepted-holdout reference rejected: %v", err)
	}
	ghost := strPtr("hfam-404")
	if err := NearestFamilyReferenced(ReviewRecord{Novel: false, NearestFamilyID: ghost}, dev, acc); err == nil {
		t.Error("reference absent from both payloads accepted")
	}
	if err := NearestFamilyReferenced(ReviewRecord{Novel: false, NearestFamilyID: nil}, dev, acc); err == nil {
		t.Error("non-novel verdict without nearest reference accepted")
	}
	if err := NearestFamilyReferenced(ReviewRecord{Novel: true, NearestFamilyID: nil}, dev, acc); err != nil {
		t.Errorf("novel verdict without reference rejected: %v", err)
	}
}

// TestReviewersAgreeOnMachineFields exercises the controller-side unanimity
// check including the recomputed label digest preimage.
func TestReviewersAgreeOnMachineFields(t *testing.T) {
	base := func() ReviewRecord {
		d, err := NormalizedLabelDigest("implicit-read-pos", LangZh, "durable-preference",
			"package-manager", ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
		if err != nil {
			t.Fatal(err)
		}
		return ReviewRecord{
			InferredModule: "implicit-read-pos", InferredLang: LangZh,
			InferredScenarioBucket: "durable-preference", InferredCategory: "package-manager",
			NormalizedLabelDigest: d,
		}
	}
	a, b := base(), base()
	if err := ReviewersAgree(a, b); err != nil {
		t.Fatalf("agreeing reviewers rejected: %v", err)
	}
	// The human-only observable must not affect the machine label digest.
	c := base()
	c.InferredExpect.Observable = "different human prose"
	if err := ReviewersAgree(a, c); err != nil {
		t.Fatalf("observable-only difference rejected (observable is excluded from the digest): %v", err)
	}
	// Any machine-field difference must break agreement.
	d := base()
	d2, _ := NormalizedLabelDigest("implicit-write-pos", LangZh, "durable-preference",
		"package-manager", ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
	d.NormalizedLabelDigest = d2
	if err := ReviewersAgree(a, d); err == nil {
		t.Error("module disagreement accepted")
	}
	// Contract v4: inferred_category is a free-form diagnostic with no closed
	// vocabulary — two blind reviewers wording it differently must NOT fail
	// the unanimity gate.
	e := base()
	e.InferredCategory = "editor"
	if err := ReviewersAgree(a, e); err != nil {
		t.Errorf("category-only wording difference rejected (v4: diagnostic, not gated): %v", err)
	}
	// A different expect machine field must produce a different digest.
	x, _ := NormalizedLabelDigest("implicit-read-pos", LangZh, "durable-preference", "package-manager",
		ExpectV2{Trigger: true, AllowedOps: []string{"search"}, NotFound: true})
	y, _ := NormalizedLabelDigest("implicit-read-pos", LangZh, "durable-preference", "package-manager",
		ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
	if x == y {
		t.Error("expect.not_found difference did not change the normalized label digest")
	}
}

// TestBehavioralModuleProjection pins the v4.2 gate semantics: the trap-
// prefix is a difficulty annotation projected away before the label digest,
// while the four behavioral classes stay distinct. An unknown module string
// must pass through unchanged so a mismatch stays visible.
func TestBehavioralModuleProjection(t *testing.T) {
	for m, want := range map[string]string{
		"implicit-write-pos": "write-pos",
		"implicit-write-neg": "write-neg",
		"implicit-read-pos":  "read-pos",
		"implicit-read-neg":  "read-neg",
		"trap-read-pos":      "read-pos",
		"trap-write-neg":     "write-neg",
		"trap-read-neg":      "read-neg",
		"  Implicit-Read-Pos ": "read-pos",
		"unknown-module":     "unknown-module",
	} {
		if got := BehavioralModule(m); got != want {
			t.Errorf("BehavioralModule(%q) = %q, want %q", m, got, want)
		}
	}
}

// TestTrapPrefixFlipPassesGate is the v4.2 regression case from the first
// full run: two reviewers disagreeing only on the trap/implicit prefix of
// the same behavioral class must NOT fail the unanimity gate, while the
// author↔reviewer comparison follows the same projection.
func TestTrapPrefixFlipPassesGate(t *testing.T) {
	mk := func(module string) ReviewRecord {
		d, err := NormalizedLabelDigest(module, LangEn, "supersession-time", "x",
			ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
		if err != nil {
			t.Fatal(err)
		}
		return ReviewRecord{InferredModule: module, InferredLang: LangEn, NormalizedLabelDigest: d}
	}
	// Same digest value either way: both prefixes project onto read-pos.
	a, b := mk("implicit-read-pos"), mk("trap-read-pos")
	if a.NormalizedLabelDigest != b.NormalizedLabelDigest {
		t.Fatal("trap/implicit prefix changed the normalized label digest (v4.2 violation)")
	}
	if err := ReviewersAgree(a, b); err != nil {
		t.Errorf("trap-prefix-only disagreement rejected (v4.2: diagnostic, not gated): %v", err)
	}
	// A genuine behavioral-class flip must still fail.
	c := mk("implicit-write-pos")
	if err := ReviewersAgree(a, c); err == nil {
		t.Error("behavioral-class disagreement accepted")
	}
}
