package validate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
)

func TestValidateCandidatesAndActionsRejectUngroundedOrUnknownInput(t *testing.T) {
	content := "Alice 在北京见到了 Bob 🧭。"
	source := contracts.Source{
		ID:            "src-1",
		Content:       content,
		ContentDigest: sha256Hex(content),
	}
	candidate := contracts.Candidate{
		ID:         "candidate-1",
		Kind:       contracts.CandidateRawTurn,
		Rank:       1,
		Score:      0.9,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}

	validated, err := ValidateCandidates([]contracts.Candidate{candidate}, 1)
	if err != nil {
		t.Fatalf("ValidateCandidates() error = %v", err)
	}
	if !validated.Allowlist["src-1"] {
		t.Fatal("validated candidate did not retain its frozen lineage allowlist")
	}

	sources := map[string]contracts.Source{"src-1": source}
	validSpan := contracts.SourceSpan{
		SourceID:   "src-1",
		StartChar:  13,
		EndChar:    18,
		SpanDigest: sha256Hex("Bob 🧭"),
	}
	if err := ValidateAction(contracts.Action{Kind: contracts.ActionExtract, CandidateID: "candidate-1", Span: &validSpan}, validated, sources); err != nil {
		t.Fatalf("ValidateAction(valid EXTRACT) error = %v", err)
	}

	for _, action := range []contracts.Action{
		{Kind: contracts.ActionKind("ADD"), SourceID: "src-1"},
		{Kind: contracts.ActionKeep, CandidateID: "candidate-1", SourceID: "src-1"},
		{Kind: contracts.ActionExtract, CandidateID: "candidate-1", Span: &contracts.SourceSpan{SourceID: "src-1", StartChar: 13, EndChar: 18, SpanDigest: "wrong"}},
		{Kind: contracts.ActionFetchSource, SourceID: "outside-lineage"},
	} {
		if err := ValidateAction(action, validated, sources); err == nil {
			t.Fatalf("ValidateAction(%+v) succeeded, want rejection", action)
		}
	}

	bad := candidate
	bad.Kind = contracts.CandidateKind("invented")
	if _, err := ValidateCandidates([]contracts.Candidate{bad}, 1); !errors.Is(err, contracts.ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidates(unknown kind) error = %v, want ErrInvalidCandidate", err)
	}
}

func TestValidateSpanUsesUnicodeCodePointOffsetsAndDigest(t *testing.T) {
	content := "a中🧭z"
	sources := map[string]contracts.Source{
		"src-unicode": {
			ID:            "src-unicode",
			Content:       content,
			ContentDigest: sha256Hex(content),
		},
	}
	allowlist := map[string]bool{"src-unicode": true}

	span := contracts.SourceSpan{
		SourceID:   "src-unicode",
		StartChar:  1,
		EndChar:    3,
		SpanDigest: sha256Hex("中🧭"),
	}
	if got, err := ValidateSourceSpan(span, allowlist, sources); err != nil || got != "中🧭" {
		t.Fatalf("ValidateSourceSpan() = (%q, %v), want (中🧭, nil)", got, err)
	}

	span.EndChar = 5
	if _, err := ValidateSourceSpan(span, allowlist, sources); !errors.Is(err, contracts.ErrInvalidSpan) {
		t.Fatalf("ValidateSourceSpan(out of range) error = %v, want ErrInvalidSpan", err)
	}
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
