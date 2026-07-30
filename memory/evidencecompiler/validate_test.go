package evidencecompiler

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestValidateCandidatesAndActionsRejectUngroundedOrUnknownInput(t *testing.T) {
	content := "Alice 在北京见到了 Bob 🧭。"
	source := Source{
		ID:            "src-1",
		Content:       content,
		ContentDigest: sha256Hex(content),
	}
	candidate := Candidate{
		ID:         "candidate-1",
		Kind:       CandidateRawTurn,
		Rank:       1,
		Score:      0.9,
		Text:       content,
		TextDigest: sha256Hex(content),
		SourceIDs:  []string{"src-1"},
	}

	validated, err := validateCandidates([]Candidate{candidate}, 1)
	if err != nil {
		t.Fatalf("validateCandidates() error = %v", err)
	}
	if !validated.allowlist["src-1"] {
		t.Fatal("validated candidate did not retain its frozen lineage allowlist")
	}

	sources := map[string]Source{"src-1": source}
	validSpan := SourceSpan{
		SourceID:   "src-1",
		StartChar:  13,
		EndChar:    18,
		SpanDigest: sha256Hex("Bob 🧭"),
	}
	if err := validateAction(Action{Kind: ActionExtract, CandidateID: "candidate-1", Span: &validSpan}, validated, sources); err != nil {
		t.Fatalf("validateAction(valid EXTRACT) error = %v", err)
	}

	for _, action := range []Action{
		{Kind: ActionKind("ADD"), SourceID: "src-1"},
		{Kind: ActionKeep, CandidateID: "candidate-1", SourceID: "src-1"},
		{Kind: ActionExtract, CandidateID: "candidate-1", Span: &SourceSpan{SourceID: "src-1", StartChar: 13, EndChar: 18, SpanDigest: "wrong"}},
		{Kind: ActionFetchSource, SourceID: "outside-lineage"},
	} {
		if err := validateAction(action, validated, sources); err == nil {
			t.Fatalf("validateAction(%+v) succeeded, want rejection", action)
		}
	}

	bad := candidate
	bad.Kind = CandidateKind("invented")
	if _, err := validateCandidates([]Candidate{bad}, 1); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("validateCandidates(unknown kind) error = %v, want ErrInvalidCandidate", err)
	}
}

func TestValidateSpanUsesUnicodeCodePointOffsetsAndDigest(t *testing.T) {
	content := "a中🧭z"
	sources := map[string]Source{
		"src-unicode": {
			ID:            "src-unicode",
			Content:       content,
			ContentDigest: sha256Hex(content),
		},
	}
	allowlist := map[string]bool{"src-unicode": true}

	span := SourceSpan{
		SourceID:   "src-unicode",
		StartChar:  1,
		EndChar:    3,
		SpanDigest: sha256Hex("中🧭"),
	}
	if got, err := validateSourceSpan(span, allowlist, sources); err != nil || got != "中🧭" {
		t.Fatalf("validateSourceSpan() = (%q, %v), want (中🧭, nil)", got, err)
	}

	span.EndChar = 5
	if _, err := validateSourceSpan(span, allowlist, sources); !errors.Is(err, ErrInvalidSpan) {
		t.Fatalf("validateSourceSpan(out of range) error = %v, want ErrInvalidSpan", err)
	}
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
