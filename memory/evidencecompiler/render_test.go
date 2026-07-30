package evidencecompiler

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderBundleIsDeterministicAndCarriesCompleteSourceUnion(t *testing.T) {
	first := "Alice met Bob."
	second := "Bob chose tea."
	items := []evidenceItem{
		{Kind: ActionKeep, Text: first, Sources: []SourceSpan{{SourceID: "src-b", StartChar: 0, EndChar: len([]rune(first)), SpanDigest: sha256Hex(first)}}, CandidateIDs: []string{"candidate-2"}},
		{Kind: ActionExtract, Text: second, Sources: []SourceSpan{{SourceID: "src-a", StartChar: 0, EndChar: len([]rune(second)), SpanDigest: sha256Hex(second)}}, CandidateIDs: []string{"candidate-1"}},
	}

	bundle := renderBundle(items)
	if got, want := bundle.SourceIDs, []string{"src-a", "src-b"}; !sameStrings(got, want) {
		t.Fatalf("renderBundle().SourceIDs = %v, want %v", got, want)
	}
	if !strings.Contains(bundle.RenderedContext, first) || !strings.Contains(bundle.RenderedContext, second) {
		t.Fatalf("renderBundle().RenderedContext = %q, want both item texts", bundle.RenderedContext)
	}
	if again := renderBundle(items); again.RenderedContext != bundle.RenderedContext || !sameStrings(again.SourceIDs, bundle.SourceIDs) {
		t.Fatalf("renderBundle() was non-deterministic:\nfirst=%+v\nnext=%+v", bundle, again)
	}
}

func TestValidateBundleRejectsTamperingAndRequiresCounterTruth(t *testing.T) {
	content := "Alice met Bob."
	sources := map[string]Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	allowlist := map[string]bool{"src-1": true}
	bundle := renderBundle([]evidenceItem{{
		Kind:         ActionKeep,
		Text:         content,
		Sources:      []SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune(content)), SpanDigest: sha256Hex(content)}},
		CandidateIDs: []string{"candidate-1"},
	}})
	trace := Trace{Valid: true}
	digest, err := traceDigest(trace)
	if err != nil {
		t.Fatal(err)
	}
	bundle.TraceDigest = digest
	bundle.InputTokens = 12
	bundle.TokenCap = 12
	bundle.CounterFingerprint = "tokenizer-v1"
	if err := validateBundle(bundle, trace, allowlist, sources, "tokenizer-v1"); err != nil {
		t.Fatalf("validateBundle(valid) error = %v", err)
	}

	tampered := bundle
	tampered.RenderedContext = "invented"
	if err := validateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("validateBundle(tampered context) error = %v, want ErrInvalidBundle", err)
	}
	tampered = bundle
	tampered.InputTokens = 13
	if err := validateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("validateBundle(over cap) error = %v, want ErrInvalidBundle", err)
	}
	tampered = bundle
	tampered.CounterFingerprint = "drifted"
	if err := validateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("validateBundle(fingerprint drift) error = %v, want ErrFingerprintMismatch", err)
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
