package render

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/extract"
)

func TestRenderBundleIsDeterministicAndCarriesCompleteSourceUnion(t *testing.T) {
	first := "Alice met Bob."
	second := "Bob chose tea."
	items := []extract.EvidenceItem{
		{Kind: contracts.ActionKeep, Text: first, Sources: []contracts.SourceSpan{{SourceID: "src-b", StartChar: 0, EndChar: len([]rune(first)), SpanDigest: sha256Hex(first)}}, CandidateIDs: []string{"candidate-2"}},
		{Kind: contracts.ActionExtract, Text: second, Sources: []contracts.SourceSpan{{SourceID: "src-a", StartChar: 0, EndChar: len([]rune(second)), SpanDigest: sha256Hex(second)}}, CandidateIDs: []string{"candidate-1"}},
	}

	bundle := RenderBundle(items)
	if got, want := bundle.SourceIDs, []string{"src-a", "src-b"}; !sameStrings(got, want) {
		t.Fatalf("RenderBundle().SourceIDs = %v, want %v", got, want)
	}
	if !strings.Contains(bundle.RenderedContext, first) || !strings.Contains(bundle.RenderedContext, second) {
		t.Fatalf("RenderBundle().RenderedContext = %q, want both item texts", bundle.RenderedContext)
	}
	if again := RenderBundle(items); again.RenderedContext != bundle.RenderedContext || !sameStrings(again.SourceIDs, bundle.SourceIDs) {
		t.Fatalf("RenderBundle() was non-deterministic:\nfirst=%+v\nnext=%+v", bundle, again)
	}
}

func TestValidateBundleRejectsTamperingAndRequiresCounterTruth(t *testing.T) {
	content := "Alice met Bob."
	sources := map[string]contracts.Source{"src-1": {ID: "src-1", Content: content, ContentDigest: sha256Hex(content)}}
	allowlist := map[string]bool{"src-1": true}
	bundle := RenderBundle([]extract.EvidenceItem{{
		Kind:         contracts.ActionKeep,
		Text:         content,
		Sources:      []contracts.SourceSpan{{SourceID: "src-1", StartChar: 0, EndChar: len([]rune(content)), SpanDigest: sha256Hex(content)}},
		CandidateIDs: []string{"candidate-1"},
	}})
	trace := contracts.Trace{Valid: true}
	digest, err := TraceDigest(trace)
	if err != nil {
		t.Fatal(err)
	}
	bundle.TraceDigest = digest
	bundle.InputTokens = 12
	bundle.TokenCap = 12
	bundle.CounterFingerprint = "tokenizer-v1"
	if err := ValidateBundle(bundle, trace, allowlist, sources, "tokenizer-v1"); err != nil {
		t.Fatalf("ValidateBundle(valid) error = %v", err)
	}

	tampered := bundle
	tampered.RenderedContext = "invented"
	if err := ValidateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, contracts.ErrInvalidBundle) {
		t.Fatalf("ValidateBundle(tampered context) error = %v, want ErrInvalidBundle", err)
	}
	tampered = bundle
	tampered.InputTokens = 13
	if err := ValidateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, contracts.ErrInvalidBundle) {
		t.Fatalf("ValidateBundle(over cap) error = %v, want ErrInvalidBundle", err)
	}
	tampered = bundle
	tampered.CounterFingerprint = "drifted"
	if err := ValidateBundle(tampered, trace, allowlist, sources, "tokenizer-v1"); !errors.Is(err, contracts.ErrFingerprintMismatch) {
		t.Fatalf("ValidateBundle(fingerprint drift) error = %v, want ErrFingerprintMismatch", err)
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

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
