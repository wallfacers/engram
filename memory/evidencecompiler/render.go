package evidencecompiler

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// BundleItem is one answer-facing, provenance-checked evidence unit.
type BundleItem struct {
	Kind         ActionKind
	Text         string
	Sources      []SourceSpan
	CandidateIDs []string
}

// Bundle is the validated input material for an answerer. EvidenceTokens is a
// descriptive field only; InputTokens is the actual hard-cap measurement.
type Bundle struct {
	Items              []BundleItem
	SourceIDs          []string
	RenderedContext    string
	EvidenceTokens     int
	InputTokens        int
	TokenCap           int
	CounterFingerprint string
	TraceDigest        string
}

// EvidenceBundle retains the contract name while Bundle is the compact public
// result name used by Compile.
type EvidenceBundle = Bundle

type DropRecord struct {
	CandidateID string
	ReasonCode  string
}

type TokenStep struct {
	Operation             string
	ItemID                string
	FullAnswerInputTokens int
	TokenCap              int
}

// Trace contains the complete, canonical audit trail for a compilation.
type Trace struct {
	Need               EvidenceNeed
	CandidateDigest    string
	CandidateIDs       []string
	CandidateSourceIDs []string
	ProposedActions    []Action
	AppliedActions     []Action
	Relations          []EvidenceRelation
	Dropped            []DropRecord
	TokenSteps         []TokenStep
	FallbackReason     string
	RemainingGap       *StructuredGap
	Valid              bool
}

// GroundedTrace retains the contract name while Trace is the public result
// name used by Compile.
type GroundedTrace = Trace

func renderBundle(items []evidenceItem) Bundle {
	bundleItems := make([]BundleItem, 0, len(items))
	for _, item := range items {
		bundleItems = append(bundleItems, BundleItem{
			Kind:         item.Kind,
			Text:         item.Text,
			Sources:      append([]SourceSpan(nil), item.Sources...),
			CandidateIDs: append([]string(nil), item.CandidateIDs...),
		})
	}
	return Bundle{
		Items:           bundleItems,
		SourceIDs:       bundleSourceIDs(bundleItems),
		RenderedContext: renderBundleItems(bundleItems),
	}
}

func renderBundleItems(items []BundleItem) string {
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

func renderSourceRefs(spans []SourceSpan) string {
	refs := make([]string, 0, len(spans))
	for _, span := range spans {
		refs = append(refs, fmt.Sprintf("%s:%d-%d", span.SourceID, span.StartChar, span.EndChar))
	}
	return strings.Join(refs, ",")
}

func bundleSourceIDs(items []BundleItem) []string {
	set := make(map[string]bool)
	for _, item := range items {
		for _, span := range item.Sources {
			set[span.SourceID] = true
		}
	}
	return sortedSourceIDs(set)
}

func traceDigest(trace Trace) (string, error) {
	return canonicalDigest(trace)
}

func validateBundle(bundle Bundle, trace Trace, allowlist map[string]bool, sources map[string]Source, expectedFingerprint string) error {
	if bundle.TokenCap <= 0 || bundle.InputTokens <= 0 || bundle.InputTokens > bundle.TokenCap {
		return fmt.Errorf("%w: input tokens %d exceed cap %d", ErrInvalidBundle, bundle.InputTokens, bundle.TokenCap)
	}
	if expectedFingerprint == "" || bundle.CounterFingerprint == "" || bundle.CounterFingerprint != expectedFingerprint {
		return fmt.Errorf("%w: got %q want %q", ErrFingerprintMismatch, bundle.CounterFingerprint, expectedFingerprint)
	}
	if !trace.Valid {
		return fmt.Errorf("%w: trace is not valid", ErrInvalidBundle)
	}
	digest, err := traceDigest(trace)
	if err != nil {
		return fmt.Errorf("%w: trace digest: %v", ErrInvalidBundle, err)
	}
	if bundle.TraceDigest == "" || bundle.TraceDigest != digest {
		return fmt.Errorf("%w: trace digest mismatch", ErrInvalidBundle)
	}
	for index, item := range bundle.Items {
		if err := validateBundleItem(item, allowlist, sources); err != nil {
			return fmt.Errorf("%w: item %d: %v", ErrInvalidBundle, index, err)
		}
	}
	if !sameStringSlice(bundle.SourceIDs, bundleSourceIDs(bundle.Items)) {
		return fmt.Errorf("%w: source union is not canonical", ErrInvalidBundle)
	}
	if bundle.RenderedContext != renderBundleItems(bundle.Items) {
		return fmt.Errorf("%w: rendered context does not match items", ErrInvalidBundle)
	}
	return nil
}

func validateBundleItem(item BundleItem, allowlist map[string]bool, sources map[string]Source) error {
	if item.Kind != ActionKeep && item.Kind != ActionExtract && item.Kind != ActionMerge {
		return fmt.Errorf("%w: bundle kind %q is not answer-facing", ErrInvalidBundle, item.Kind)
	}
	if item.Text == "" || !utf8.ValidString(item.Text) || len(item.Sources) == 0 {
		return fmt.Errorf("%w: empty or ungrounded item", ErrInvalidBundle)
	}
	if !strictlySortedUnique(item.CandidateIDs) {
		return fmt.Errorf("%w: candidate IDs are not canonical", ErrInvalidBundle)
	}
	for _, span := range item.Sources {
		if _, err := validateSourceSpan(span, allowlist, sources); err != nil {
			return err
		}
	}
	switch item.Kind {
	case ActionKeep:
		if len(item.Sources) != 1 {
			return fmt.Errorf("%w: KEEP needs exactly one complete source", ErrInvalidBundle)
		}
		span := item.Sources[0]
		source, err := validSource(span.SourceID, allowlist, sources)
		if err != nil {
			return err
		}
		if span.StartChar != 0 || span.EndChar != len([]rune(source.Content)) || item.Text != source.Content {
			return fmt.Errorf("%w: KEEP is not canonical source text", ErrInvalidBundle)
		}
	case ActionExtract:
		if len(item.Sources) != 1 {
			return fmt.Errorf("%w: EXTRACT needs exactly one source span", ErrInvalidBundle)
		}
		text, err := validateSourceSpan(item.Sources[0], allowlist, sources)
		if err != nil || item.Text != text {
			return fmt.Errorf("%w: EXTRACT text cannot be reconstructed", ErrInvalidBundle)
		}
	case ActionMerge:
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
