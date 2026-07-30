// Package evidencecompiler defines the provider-neutral, query-time evidence
// compilation contract. It intentionally has no retriever, store, or answerer
// dependency: callers freeze candidates before compile and call the answerer
// only after a valid bundle has been produced.
package evidencecompiler

import (
	"context"
	"time"
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
