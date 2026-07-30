package evidencecompiler

import "testing"

func TestExtractionPlanKeepsRawCanonicalEvidenceWhenRawFits(t *testing.T) {
	content := "Alice met Bob in Beijing. They chose tea."
	candidates := []Candidate{{
		ID:         "candidate-1",
		Kind:       CandidateRawTurn,
		Rank:       1,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}}
	sources := map[string]Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	validated, err := validateCandidates(candidates, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildExtractionPlan(EvidenceNeed{Entities: []string{"Alice"}}, validated, sources)
	if err != nil {
		t.Fatal(err)
	}

	selected := selectPackingItems(plan, true)
	if len(selected) != 1 || selected[0].Kind != ActionKeep || selected[0].Text != content {
		t.Fatalf("selectPackingItems(raw fits) = %+v, want one uncompressed KEEP", selected)
	}
	if got := selected[0].Sources[0]; got.StartChar != 0 || got.EndChar != len([]rune(content)) || got.SpanDigest != sha256Hex(content) {
		t.Fatalf("raw KEEP span = %+v, want complete canonical source", got)
	}
}

func TestExtractionPlanUsesExactSentenceSpansOnlyWhenRawDoesNotFit(t *testing.T) {
	content := "Background material that is not needed. Alice met Bob in Beijing. More unrelated details."
	candidates := []Candidate{{
		ID:         "candidate-1",
		Kind:       CandidateRawTurn,
		Rank:       1,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}}
	sources := map[string]Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	validated, err := validateCandidates(candidates, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildExtractionPlan(EvidenceNeed{Entities: []string{"Alice", "Bob"}}, validated, sources)
	if err != nil {
		t.Fatal(err)
	}

	selected := selectPackingItems(plan, false)
	if len(selected) != 1 || selected[0].Kind != ActionExtract || selected[0].Text != "Alice met Bob in Beijing." {
		t.Fatalf("selectPackingItems(raw over cap) = %+v, want exact relevant EXTRACT", selected)
	}
	if reconstructed, err := validateSourceSpan(selected[0].Sources[0], validated.allowlist, sources); err != nil || reconstructed != selected[0].Text {
		t.Fatalf("extract span reconstruction = (%q, %v), want (%q, nil)", reconstructed, err, selected[0].Text)
	}
}

func TestMergeGateRequiresRawOverCapAndExtractiveInsufficiency(t *testing.T) {
	need := EvidenceNeed{Entities: []string{"Alice", "Bob"}}
	fullyGrounded := []evidenceItem{{
		Kind:    ActionExtract,
		Text:    "Alice met Bob.",
		Sources: []SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune("Alice met Bob.")), SpanDigest: sha256Hex("Alice met Bob.")}},
	}}
	if mergePermitted(false, need, fullyGrounded) {
		t.Fatal("mergePermitted() allowed MERGE while raw evidence fits")
	}
	if mergePermitted(true, need, fullyGrounded) {
		t.Fatal("mergePermitted() allowed MERGE although EXTRACT fully satisfies Need")
	}

	insufficient := []evidenceItem{{
		Kind:    ActionExtract,
		Text:    "Alice arrived.",
		Sources: []SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune("Alice arrived.")), SpanDigest: sha256Hex("Alice arrived.")}},
	}}
	if !mergePermitted(true, need, insufficient) {
		t.Fatal("mergePermitted() rejected MERGE after raw over-cap and insufficient EXTRACT")
	}
}
