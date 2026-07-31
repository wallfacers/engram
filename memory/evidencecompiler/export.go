// Package evidencecompiler defines the provider-neutral, query-time evidence
// compilation contract. It intentionally has no retriever, store, or answerer
// dependency: callers freeze candidates before compile and call the answerer
// only after a valid bundle has been produced.
//
// Structure: the root package is the public facade. All frozen contract types
// live in internal/contracts and are re-exported here by alias, so the outward
// API shape is unchanged; the deterministic, IO-free layers live under
// internal/need, internal/validate, internal/extract, internal/render, and
// internal/resolve with a single dependency direction (contracts → validate →
// need/extract/render/resolve → root orchestration).
package evidencecompiler

import (
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/need"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/resolve"
)

// Contract types are aliased from internal/contracts so the frozen public
// shape (compiler-contract.md) remains byte-identical for callers.
type (
	CandidateKind       = contracts.CandidateKind
	Candidate           = contracts.Candidate
	Source              = contracts.Source
	SourceSpan          = contracts.SourceSpan
	Cardinality         = contracts.Cardinality
	Operand             = contracts.Operand
	GapKind             = contracts.GapKind
	StructuredGap       = contracts.StructuredGap
	RelationKind        = contracts.RelationKind
	EvidenceRelation    = contracts.EvidenceRelation
	EvidenceNeed        = contracts.EvidenceNeed
	ActionKind          = contracts.ActionKind
	GroundedSentence    = contracts.GroundedSentence
	Action              = contracts.Action
	Proposal            = contracts.Proposal
	AnswerInput         = contracts.AnswerInput
	TokenCount          = contracts.TokenCount
	TokenCounter        = contracts.TokenCounter
	AnswerRenderer      = contracts.AnswerRenderer
	Evidence            = contracts.Evidence
	SourceResolver      = contracts.SourceResolver
	EvidenceBatchReader = contracts.EvidenceBatchReader
	Planner             = contracts.Planner
	Config              = contracts.Config
	CompileRequest      = contracts.CompileRequest
	Result              = contracts.Result
	BundleItem          = contracts.BundleItem
	Bundle              = contracts.Bundle
	EvidenceBundle      = contracts.EvidenceBundle
	DropRecord          = contracts.DropRecord
	TokenStep           = contracts.TokenStep
	Trace               = contracts.Trace
	GroundedTrace       = contracts.GroundedTrace
	LedgerResolver      = resolve.LedgerResolver
)

// Contract constants are aliased from internal/contracts.
const (
	CandidateChunk           = contracts.CandidateChunk
	CandidateRawTurn         = contracts.CandidateRawTurn
	CandidateSemanticEpisode = contracts.CandidateSemanticEpisode
	CandidateAtomicFact      = contracts.CandidateAtomicFact

	GapEntity        = contracts.GapEntity
	GapTimeRange     = contracts.GapTimeRange
	GapSecondOperand = contracts.GapSecondOperand

	RelationBefore          = contracts.RelationBefore
	RelationAfter           = contracts.RelationAfter
	RelationConflicts       = contracts.RelationConflicts
	RelationSupportsOperand = contracts.RelationSupportsOperand

	ActionKeep        = contracts.ActionKeep
	ActionExtract     = contracts.ActionExtract
	ActionDrop        = contracts.ActionDrop
	ActionMerge       = contracts.ActionMerge
	ActionFetchSource = contracts.ActionFetchSource
)

// Sentinel errors are re-exported as the same values as the contract layer.
var (
	ErrInvalidCandidate    = contracts.ErrInvalidCandidate
	ErrSourceUnavailable   = contracts.ErrSourceUnavailable
	ErrInvalidAction       = contracts.ErrInvalidAction
	ErrInvalidSpan         = contracts.ErrInvalidSpan
	ErrInvalidNeed         = contracts.ErrInvalidNeed
	ErrInvalidBundle       = contracts.ErrInvalidBundle
	ErrCounterUnavailable  = contracts.ErrCounterUnavailable
	ErrFingerprintMismatch = contracts.ErrFingerprintMismatch
	ErrBudgetImpossible    = contracts.ErrBudgetImpossible
)

// BuildNeed deterministically extracts only explicit, query-local constraints.
// It intentionally has no benchmark category, Retriever, or model dependency.
func BuildNeed(query string) EvidenceNeed {
	return need.BuildNeed(query)
}
