// Package render produces the deterministic answer-facing Bundle rendering and
// validates a completed bundle against its Trace, sources, and counter
// fingerprint. It is pure: rendering never calls the answerer or counter.
package render

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/extract"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

// RenderBundle turns selected evidence items into the deterministic Bundle
// shape with a canonical source union and rendered context.
func RenderBundle(items []extract.EvidenceItem) contracts.Bundle {
	bundleItems := make([]contracts.BundleItem, 0, len(items))
	for _, item := range items {
		bundleItems = append(bundleItems, contracts.BundleItem{
			Kind:         item.Kind,
			Text:         item.Text,
			Sources:      append([]contracts.SourceSpan(nil), item.Sources...),
			CandidateIDs: append([]string(nil), item.CandidateIDs...),
		})
	}
	return contracts.Bundle{
		Items:           bundleItems,
		SourceIDs:       bundleSourceIDs(bundleItems),
		RenderedContext: renderBundleItems(bundleItems),
	}
}

func renderBundleItems(items []contracts.BundleItem) string {
	var rendered strings.Builder
	for index, item := range items {
		if index > 0 {
			rendered.WriteString("\n\n")
		}
		rendered.WriteString("<evidence kind=\"")
		rendered.WriteString(string(item.Kind))
		rendered.WriteString("\" sources=\"")
		rendered.WriteString(renderSourceRefs(item.Sources))
		rendered.WriteString("\" candidates=\"")
		rendered.WriteString(strings.Join(item.CandidateIDs, ","))
		rendered.WriteString("\">\n")
		rendered.WriteString(item.Text)
		rendered.WriteString("\n</evidence>")
	}
	return rendered.String()
}

func renderSourceRefs(spans []contracts.SourceSpan) string {
	refs := make([]string, 0, len(spans))
	for _, span := range spans {
		refs = append(refs, fmt.Sprintf("%s:%d-%d", span.SourceID, span.StartChar, span.EndChar))
	}
	return strings.Join(refs, ",")
}

func bundleSourceIDs(items []contracts.BundleItem) []string {
	set := make(map[string]bool)
	for _, item := range items {
		for _, span := range item.Sources {
			set[span.SourceID] = true
		}
	}
	return validate.SortedSourceIDs(set)
}

// TraceDigest is the canonical digest of a Trace for bundle binding.
func TraceDigest(trace contracts.Trace) (string, error) {
	return validate.CanonicalDigest(trace)
}

// ValidateBundle rejects token/fingerprint/trace/rendering drift and any
// ungrounded or non-canonical item before the bundle is answer-facing.
func ValidateBundle(bundle contracts.Bundle, trace contracts.Trace, allowlist map[string]bool, sources map[string]contracts.Source, expectedFingerprint string) error {
	if bundle.TokenCap <= 0 || bundle.InputTokens <= 0 || bundle.InputTokens > bundle.TokenCap {
		return fmt.Errorf("%w: input tokens %d exceed cap %d", contracts.ErrInvalidBundle, bundle.InputTokens, bundle.TokenCap)
	}
	if expectedFingerprint == "" || bundle.CounterFingerprint == "" || bundle.CounterFingerprint != expectedFingerprint {
		return fmt.Errorf("%w: got %q want %q", contracts.ErrFingerprintMismatch, bundle.CounterFingerprint, expectedFingerprint)
	}
	if !trace.Valid {
		return fmt.Errorf("%w: trace is not valid", contracts.ErrInvalidBundle)
	}
	digest, err := TraceDigest(trace)
	if err != nil {
		return fmt.Errorf("%w: trace digest: %v", contracts.ErrInvalidBundle, err)
	}
	if bundle.TraceDigest == "" || bundle.TraceDigest != digest {
		return fmt.Errorf("%w: trace digest mismatch", contracts.ErrInvalidBundle)
	}
	for index, item := range bundle.Items {
		if err := validateBundleItem(item, allowlist, sources); err != nil {
			return fmt.Errorf("%w: item %d: %v", contracts.ErrInvalidBundle, index, err)
		}
	}
	if !sameStringSlice(bundle.SourceIDs, bundleSourceIDs(bundle.Items)) {
		return fmt.Errorf("%w: source union is not canonical", contracts.ErrInvalidBundle)
	}
	if bundle.RenderedContext != renderBundleItems(bundle.Items) {
		return fmt.Errorf("%w: rendered context does not match items", contracts.ErrInvalidBundle)
	}
	return nil
}

func validateBundleItem(item contracts.BundleItem, allowlist map[string]bool, sources map[string]contracts.Source) error {
	if item.Kind != contracts.ActionKeep && item.Kind != contracts.ActionExtract && item.Kind != contracts.ActionMerge {
		return fmt.Errorf("%w: bundle kind %q is not answer-facing", contracts.ErrInvalidBundle, item.Kind)
	}
	if item.Text == "" || !utf8.ValidString(item.Text) || len(item.Sources) == 0 {
		return fmt.Errorf("%w: empty or ungrounded item", contracts.ErrInvalidBundle)
	}
	if !strictlySortedUnique(item.CandidateIDs) {
		return fmt.Errorf("%w: candidate IDs are not canonical", contracts.ErrInvalidBundle)
	}
	for _, span := range item.Sources {
		if _, err := validate.ValidateSourceSpan(span, allowlist, sources); err != nil {
			return err
		}
	}
	switch item.Kind {
	case contracts.ActionKeep:
		if len(item.Sources) != 1 {
			return fmt.Errorf("%w: KEEP needs exactly one complete source", contracts.ErrInvalidBundle)
		}
		span := item.Sources[0]
		source, err := validate.ValidSource(span.SourceID, allowlist, sources)
		if err != nil {
			return err
		}
		if span.StartChar != 0 || span.EndChar != len([]rune(source.Content)) || item.Text != source.Content {
			return fmt.Errorf("%w: KEEP is not canonical source text", contracts.ErrInvalidBundle)
		}
	case contracts.ActionExtract:
		if len(item.Sources) != 1 {
			return fmt.Errorf("%w: EXTRACT needs exactly one source span", contracts.ErrInvalidBundle)
		}
		text, err := validate.ValidateSourceSpan(item.Sources[0], allowlist, sources)
		if err != nil || item.Text != text {
			return fmt.Errorf("%w: EXTRACT text cannot be reconstructed", contracts.ErrInvalidBundle)
		}
	case contracts.ActionMerge:
		// Sentence-level grounding is checked when the proposal action is
		// accepted. The bundle preserves its validated source-span union.
	}
	return nil
}

func strictlySortedUnique(values []string) bool {
	if len(values) == 0 {
		return true
	}
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if value == "" || (index > 0 && value == values[index-1]) {
			return false
		}
	}
	return true
}

func sameStringSlice(left, right []string) bool {
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
