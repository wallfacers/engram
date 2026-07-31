package extract

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

func TestExtractionPlanKeepsRawCanonicalEvidenceWhenRawFits(t *testing.T) {
	content := "Alice met Bob in Beijing. They chose tea."
	candidates := []contracts.Candidate{{
		ID:         "candidate-1",
		Kind:       contracts.CandidateRawTurn,
		Rank:       1,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}}
	sources := map[string]contracts.Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	validated, err := validate.ValidateCandidates(candidates, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildExtractionPlan(contracts.EvidenceNeed{Entities: []string{"Alice"}}, validated, sources)
	if err != nil {
		t.Fatal(err)
	}

	selected := SelectPackingItems(plan, true)
	if len(selected) != 1 || selected[0].Kind != contracts.ActionKeep || selected[0].Text != content {
		t.Fatalf("SelectPackingItems(raw fits) = %+v, want one uncompressed KEEP", selected)
	}
	if got := selected[0].Sources[0]; got.StartChar != 0 || got.EndChar != len([]rune(content)) || got.SpanDigest != sha256Hex(content) {
		t.Fatalf("raw KEEP span = %+v, want complete canonical source", got)
	}
}

func TestExtractionPlanUsesExactSentenceSpansOnlyWhenRawDoesNotFit(t *testing.T) {
	content := "Background material that is not needed. Alice met Bob in Beijing. More unrelated details."
	candidates := []contracts.Candidate{{
		ID:         "candidate-1",
		Kind:       contracts.CandidateRawTurn,
		Rank:       1,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}}
	sources := map[string]contracts.Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	validated, err := validate.ValidateCandidates(candidates, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildExtractionPlan(contracts.EvidenceNeed{Entities: []string{"Alice", "Bob"}}, validated, sources)
	if err != nil {
		t.Fatal(err)
	}

	selected := SelectPackingItems(plan, false)
	if len(selected) != 1 || selected[0].Kind != contracts.ActionExtract || selected[0].Text != "Alice met Bob in Beijing." {
		t.Fatalf("SelectPackingItems(raw over cap) = %+v, want exact relevant EXTRACT", selected)
	}
	if reconstructed, err := validate.ValidateSourceSpan(selected[0].Sources[0], validated.Allowlist, sources); err != nil || reconstructed != selected[0].Text {
		t.Fatalf("extract span reconstruction = (%q, %v), want (%q, nil)", reconstructed, err, selected[0].Text)
	}
}

func TestMergeGateRequiresRawOverCapAndExtractiveInsufficiency(t *testing.T) {
	need := contracts.EvidenceNeed{Entities: []string{"Alice", "Bob"}}
	fullyGrounded := []EvidenceItem{{
		Kind:    contracts.ActionExtract,
		Text:    "Alice met Bob.",
		Sources: []contracts.SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune("Alice met Bob.")), SpanDigest: sha256Hex("Alice met Bob.")}},
	}}
	if MergePermitted(false, need, fullyGrounded) {
		t.Fatal("MergePermitted() allowed MERGE while raw evidence fits")
	}
	if MergePermitted(true, need, fullyGrounded) {
		t.Fatal("MergePermitted() allowed MERGE although EXTRACT fully satisfies Need")
	}

	insufficient := []EvidenceItem{{
		Kind:    contracts.ActionExtract,
		Text:    "Alice arrived.",
		Sources: []contracts.SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune("Alice arrived.")), SpanDigest: sha256Hex("Alice arrived.")}},
	}}
	if !MergePermitted(true, need, insufficient) {
		t.Fatal("MergePermitted() rejected MERGE after raw over-cap and insufficient EXTRACT")
	}
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
