// Package contracts freezes the evidencecompiler public contract types. It is
// the single source of type truth for the package: need/validate/extract/
// render/resolve all consume these types, and the evidencecompiler root
// re-exports them by alias so the frozen outward API shape is unchanged.
//
// The package intentionally has no behaviour: no validation, no IO, no
// rendering. It only declares the provider-neutral, query-time evidence
// compilation contract (candidates frozen before compile; answerer called
// only after a valid bundle has been produced).
package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/wallfacers/engram/memory"
)

// Sentinel errors are part of the frozen public contract. They are declared
// here, on the type layer, so every internal layer and the root package share
// the same error values without importing each other.
var (
	ErrInvalidCandidate    = errors.New("evidencecompiler: invalid candidate")
	ErrSourceUnavailable   = errors.New("evidencecompiler: source unavailable")
	ErrInvalidAction       = errors.New("evidencecompiler: invalid action")
	ErrInvalidSpan         = errors.New("evidencecompiler: invalid source span")
	ErrInvalidNeed         = errors.New("evidencecompiler: invalid evidence need")
	ErrInvalidBundle       = errors.New("evidencecompiler: invalid evidence bundle")
	ErrCounterUnavailable  = errors.New("evidencecompiler: token counter unavailable")
	ErrFingerprintMismatch = errors.New("evidencecompiler: counter fingerprint mismatch")
	ErrBudgetImpossible    = errors.New("evidencecompiler: evidence cannot fit token cap")
)

type CandidateKind string

const (
	CandidateChunk           CandidateKind = "chunk"
	CandidateRawTurn         CandidateKind = "raw_turn"
	CandidateSemanticEpisode CandidateKind = "semantic_episode"
	CandidateAtomicFact      CandidateKind = "atomic_fact"
)

type Candidate struct {
	ID         string
	Kind       CandidateKind
	Rank       int
	Score      float64
	Text       string
	TextDigest string
	SourceIDs  []string
	Metadata   map[string]string
}

type Source struct {
	ID              string
	SourceSessionID string
	Speaker         string
	Ordinal         int
	Content         string
	ContentDigest   string
	OccurredAt      *time.Time
}

// SourceSpan uses Unicode code-point [StartChar, EndChar) offsets.
type SourceSpan struct {
	SourceID   string
	StartChar  int
	EndChar    int
	SpanDigest string
}

type Cardinality struct {
	Known bool
	Count int
}

type Operand struct {
	Name      string
	Satisfied bool
}

type GapKind string

const (
	GapEntity        GapKind = "entity"
	GapTimeRange     GapKind = "time_range"
	GapSecondOperand GapKind = "second_operand"
)

type StructuredGap struct {
	Kind       GapKind
	Entity     string
	Start      *time.Time
	End        *time.Time
	Operand    string
	SourceNeed string
}

type RelationKind string

const (
	RelationBefore          RelationKind = "before"
	RelationAfter           RelationKind = "after"
	RelationConflicts       RelationKind = "conflicts"
	RelationSupportsOperand RelationKind = "supports_operand"
)

type EvidenceRelation struct {
	Kind          RelationKind
	LeftSourceID  string
	RightSourceID string
	Operand       string
}

type EvidenceNeed struct {
	Entities        []string
	TimeConstraints []string
	Operands        []Operand
	ListCardinality Cardinality
	UpdateState     string
	Gap             *StructuredGap
}

type ActionKind string

const (
	ActionKeep        ActionKind = "KEEP"
	ActionExtract     ActionKind = "EXTRACT"
	ActionDrop        ActionKind = "DROP"
	ActionMerge       ActionKind = "MERGE"
	ActionFetchSource ActionKind = "FETCH_SOURCE"
)

type GroundedSentence struct {
	Text    string
	Sources []SourceSpan
}

type Action struct {
	Kind        ActionKind
	CandidateID string
	SourceID    string
	Span        *SourceSpan
	Sentences   []GroundedSentence
	ReasonCode  string
}

type Proposal struct {
	Need    EvidenceNeed
	Actions []Action
}

// AnswerInput is the exact message pair passed to the configured answerer.
// Its token count, rather than a character or item estimate, is the cap gate.
type AnswerInput struct {
	Model  string
	System string
	User   string
}

type TokenCount struct {
	InputTokens int
	Fingerprint string
}

type TokenCounter interface {
	CountInput(ctx context.Context, input AnswerInput) (TokenCount, error)
}

type AnswerRenderer interface {
	RenderAnswerInput(query string, renderedEvidence string) AnswerInput
}

// Evidence is the immutable Ledger record used by the narrow resolver bridge.
// It is an alias, not a second source representation or a copied store model.
type Evidence = memory.Evidence

// SourceResolver is intentionally the only read authority available to the
// Compiler. Its finite ID list comes from frozen candidate lineage.
type SourceResolver interface {
	Resolve(ctx context.Context, sourceIDs []string) ([]Evidence, error)
}

// EvidenceBatchReader is the active-Ledger capability required by
// LedgerResolver. GetMany is already batched by LedgerStore and fails if any
// requested record is unavailable.
type EvidenceBatchReader interface {
	GetMany(ctx context.Context, evidenceIDs []string) (map[string]memory.Evidence, error)
}

// Planner is optional and proposal-only. It has neither a source reader nor
// an answer invocation capability; all grounding and budget admission remain
// inside Compiler.
type Planner interface {
	Propose(ctx context.Context, query string, candidates []Candidate) (Proposal, error)
}

// Config fixes the source boundary and tokenizer protocol for a compiler run.
type Config struct {
	TokenCap           int
	CounterFingerprint string
	MaxCandidates      int
	MaxSources         int
	Planner            Planner
	Resolver           SourceResolver
	Counter            TokenCounter
	Renderer           AnswerRenderer
}

// CompileRequest holds frozen candidates and, for the direct package function,
// its immutable compilation configuration. The direct config fields are a
// convenient equivalent to Config and take precedence when non-zero/non-nil.
type CompileRequest struct {
	Query      string
	Candidates []Candidate
	Config     Config

	TokenCap           int
	CounterFingerprint string
	MaxCandidates      int
	MaxSources         int
	Planner            Planner
	Resolver           SourceResolver
	Counter            TokenCounter
	Renderer           AnswerRenderer
}

// Result preserves the contract's aggregate result shape for callers that
// prefer a value object. Compile itself returns Bundle and Trace separately.
type Result struct {
	Bundle Bundle
	Trace  Trace
}

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
