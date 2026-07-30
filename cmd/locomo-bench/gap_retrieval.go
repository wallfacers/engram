package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// gapTrace is the narrow, runner-owned view of the first compiler Trace. The
// current base exposes Need/StructuredGap but not the compiler's completed
// GroundedTrace implementation yet, so tests construct this fixture directly.
// The final runner bridge maps only Valid, Need, and RemainingGap from the
// public compiler Trace; it must not call Compile or invent a gap here.
type gapTrace struct {
	Valid         bool
	LowConfidence bool
	FreeTextNeed  string
	Need          evidencecompiler.EvidenceNeed
	RemainingGap  *evidencecompiler.StructuredGap
}

type gapCandidateRetriever interface {
	Retrieve(context.Context, string, int) ([]evidencecompiler.Candidate, error)
}

type gapCandidateRetrieverFunc func(context.Context, string, int) ([]evidencecompiler.Candidate, error)

func (retrieve gapCandidateRetrieverFunc) Retrieve(ctx context.Context, query string, limit int) ([]evidencecompiler.Candidate, error) {
	return retrieve(ctx, query, limit)
}

// gapBudget is a frozen per-question budget. RetrievalCalls counts only the
// optional supplemental lookup; the ordinary first retrieval is the shared
// control/treatment path and remains outside this narrow helper.
type gapBudget struct {
	CandidateLimit           int
	FirstRoundCandidateLimit int
	RefetchCandidateLimit    int
	TokenCap                 int
	RetrievalCallLimit       int
	AnswerCallLimit          int
}

type gapBudgetUsage struct {
	CandidateCount int
	TokenCount     int
	RetrievalCalls int
	AnswerCalls    int
}

func newGapControlBudget(candidateLimit, tokenCap int) (gapBudget, error) {
	budget := gapBudget{
		CandidateLimit:           candidateLimit,
		FirstRoundCandidateLimit: candidateLimit,
		RefetchCandidateLimit:    0,
		TokenCap:                 tokenCap,
		RetrievalCallLimit:       0,
		AnswerCallLimit:          1,
	}
	return budget, budget.validateShape()
}

func newGapTreatmentBudget(candidateLimit, reserve, tokenCap int) (gapBudget, error) {
	budget := gapBudget{
		CandidateLimit:           candidateLimit,
		FirstRoundCandidateLimit: candidateLimit - reserve,
		RefetchCandidateLimit:    reserve,
		TokenCap:                 tokenCap,
		RetrievalCallLimit:       1,
		AnswerCallLimit:          1,
	}
	if reserve <= 0 || reserve >= candidateLimit {
		return gapBudget{}, fmt.Errorf("gap treatment reserve %d must be within (0,%d)", reserve, candidateLimit)
	}
	if err := budget.validateShape(); err != nil {
		return gapBudget{}, err
	}
	return budget, nil
}

func (budget gapBudget) validateShape() error {
	if budget.CandidateLimit <= 0 {
		return fmt.Errorf("gap budget requires positive candidate limit")
	}
	if budget.FirstRoundCandidateLimit <= 0 || budget.RefetchCandidateLimit < 0 || budget.FirstRoundCandidateLimit+budget.RefetchCandidateLimit != budget.CandidateLimit {
		return fmt.Errorf("gap budget candidate split must equal frozen candidate limit")
	}
	if budget.TokenCap <= 0 {
		return fmt.Errorf("gap budget requires positive token cap")
	}
	if budget.RetrievalCallLimit < 0 || budget.RetrievalCallLimit > 1 {
		return fmt.Errorf("gap budget permits at most one supplemental retrieval")
	}
	if budget.AnswerCallLimit != 1 {
		return fmt.Errorf("gap budget requires exactly one answer call")
	}
	return nil
}

func (budget gapBudget) validateUsage(usage gapBudgetUsage) error {
	if err := budget.validateShape(); err != nil {
		return err
	}
	if usage.CandidateCount < 0 || usage.CandidateCount > budget.CandidateLimit {
		return fmt.Errorf("gap candidate union %d exceeds frozen limit %d", usage.CandidateCount, budget.CandidateLimit)
	}
	if usage.TokenCount < 0 || usage.TokenCount > budget.TokenCap {
		return fmt.Errorf("gap cumulative tokens %d exceed cap %d", usage.TokenCount, budget.TokenCap)
	}
	if usage.RetrievalCalls < 0 || usage.RetrievalCalls > budget.RetrievalCallLimit {
		return fmt.Errorf("gap supplemental retrieval calls %d exceed limit %d", usage.RetrievalCalls, budget.RetrievalCallLimit)
	}
	if usage.AnswerCalls < 0 || usage.AnswerCalls > budget.AnswerCallLimit {
		return fmt.Errorf("gap answer calls %d exceed limit %d", usage.AnswerCalls, budget.AnswerCallLimit)
	}
	return nil
}

func validateComparableGapBudgets(control, treatment gapBudget) error {
	if err := control.validateShape(); err != nil {
		return fmt.Errorf("invalid control budget: %w", err)
	}
	if err := treatment.validateShape(); err != nil {
		return fmt.Errorf("invalid treatment budget: %w", err)
	}
	if control.CandidateLimit != treatment.CandidateLimit {
		return fmt.Errorf("control/treatment candidate limits differ")
	}
	if control.TokenCap != treatment.TokenCap {
		return fmt.Errorf("control/treatment token caps differ")
	}
	if control.AnswerCallLimit != treatment.AnswerCallLimit {
		return fmt.Errorf("control/treatment answer-call limits differ")
	}
	if control.FirstRoundCandidateLimit != control.CandidateLimit || control.RefetchCandidateLimit != 0 || control.RetrievalCallLimit != 0 {
		return fmt.Errorf("control must be one pass with N candidates")
	}
	if treatment.RefetchCandidateLimit <= 0 || treatment.RetrievalCallLimit != 1 || treatment.FirstRoundCandidateLimit+treatment.RefetchCandidateLimit != treatment.CandidateLimit {
		return fmt.Errorf("treatment must be (N-r)+r with one supplemental retrieval")
	}
	return nil
}

type gapRefetchRequest struct {
	Trace             gapTrace
	InitialCandidates []evidencecompiler.Candidate
	Budget            gapBudget
	Usage             gapBudgetUsage
}

type gapRefetchResult struct {
	Triggered    bool
	Query        string
	Candidates   []evidencecompiler.Candidate
	RemainingGap *evidencecompiler.StructuredGap
	Budget       gapBudget
	Usage        gapBudgetUsage
}

var errStructuredGapAlreadyRefetched = errors.New("structured gap retrieval already used")

// runOneStructuredGapRefetch has no Compiler or answerer dependency. It is the
// single, auditable retrieval seam inserted between two Compile calls by the
// eventual runner integration.
func runOneStructuredGapRefetch(ctx context.Context, request gapRefetchRequest, retriever gapCandidateRetriever) (gapRefetchResult, error) {
	if err := request.Budget.validateShape(); err != nil {
		return gapRefetchResult{}, err
	}
	if len(request.InitialCandidates) > request.Budget.FirstRoundCandidateLimit {
		return gapRefetchResult{}, fmt.Errorf("first-round candidates %d exceed treatment allocation %d", len(request.InitialCandidates), request.Budget.FirstRoundCandidateLimit)
	}
	if err := validateShadowCandidates(request.InitialCandidates); err != nil {
		return gapRefetchResult{}, fmt.Errorf("invalid first-round candidates: %w", err)
	}
	usage := request.Usage
	if usage.CandidateCount != 0 && usage.CandidateCount != len(request.InitialCandidates) {
		return gapRefetchResult{}, fmt.Errorf("recorded first-round candidate count %d does not match %d", usage.CandidateCount, len(request.InitialCandidates))
	}
	usage.CandidateCount = len(request.InitialCandidates)
	if err := request.Budget.validateUsage(usage); err != nil {
		return gapRefetchResult{}, err
	}

	gap, eligible := eligibleStructuredGap(request.Trace)
	if !eligible {
		return gapRefetchResult{
			Candidates:   cloneCandidates(request.InitialCandidates),
			RemainingGap: cloneStructuredGap(request.Trace.RemainingGap),
			Budget:       request.Budget,
			Usage:        usage,
		}, nil
	}
	if retriever == nil {
		return gapRefetchResult{}, fmt.Errorf("structured gap retrieval requires retriever")
	}
	if usage.RetrievalCalls >= request.Budget.RetrievalCallLimit {
		return gapRefetchResult{}, errStructuredGapAlreadyRefetched
	}

	query, err := renderStructuredGapQuery(gap)
	if err != nil {
		return gapRefetchResult{}, err
	}
	supplemental, err := retriever.Retrieve(ctx, query, request.Budget.RefetchCandidateLimit)
	if err != nil {
		return gapRefetchResult{}, fmt.Errorf("structured gap retrieval: %w", err)
	}
	if len(supplemental) > 0 {
		if err := validateShadowCandidates(supplemental); err != nil {
			return gapRefetchResult{}, fmt.Errorf("invalid supplemental candidates: %w", err)
		}
	}
	union, added, err := stableGapCandidateUnion(request.InitialCandidates, supplemental, request.Budget.CandidateLimit)
	if err != nil {
		return gapRefetchResult{}, err
	}
	if added > request.Budget.RefetchCandidateLimit {
		return gapRefetchResult{}, fmt.Errorf("supplemental candidate count %d exceeds allocation %d", added, request.Budget.RefetchCandidateLimit)
	}
	usage.RetrievalCalls++
	usage.CandidateCount = len(union)
	if err := request.Budget.validateUsage(usage); err != nil {
		return gapRefetchResult{}, err
	}
	return gapRefetchResult{
		Triggered:    true,
		Query:        query,
		Candidates:   union,
		RemainingGap: cloneStructuredGap(gap),
		Budget:       request.Budget,
		Usage:        usage,
	}, nil
}

func eligibleStructuredGap(trace gapTrace) (*evidencecompiler.StructuredGap, bool) {
	if !trace.Valid || trace.LowConfidence || strings.TrimSpace(trace.FreeTextNeed) != "" || trace.Need.Gap == nil || trace.RemainingGap == nil {
		return nil, false
	}
	if !sameStructuredGap(*trace.Need.Gap, *trace.RemainingGap) {
		return nil, false
	}
	gap := cloneStructuredGap(trace.RemainingGap)
	if err := validateStructuredGap(*gap); err != nil {
		return nil, false
	}
	return gap, true
}

func validateStructuredGap(gap evidencecompiler.StructuredGap) error {
	switch gap.Kind {
	case evidencecompiler.GapEntity:
		if strings.TrimSpace(gap.Entity) == "" || gap.Start != nil || gap.End != nil || strings.TrimSpace(gap.Operand) != "" {
			return fmt.Errorf("entity gap requires entity only")
		}
	case evidencecompiler.GapTimeRange:
		if gap.Start == nil || gap.End == nil || gap.End.Before(*gap.Start) || strings.TrimSpace(gap.Entity) != "" || strings.TrimSpace(gap.Operand) != "" {
			return fmt.Errorf("time-range gap requires ordered bounds only")
		}
	case evidencecompiler.GapSecondOperand:
		if strings.TrimSpace(gap.Operand) == "" || gap.Start != nil || gap.End != nil || strings.TrimSpace(gap.Entity) != "" {
			return fmt.Errorf("second-operand gap requires operand only")
		}
	default:
		return fmt.Errorf("unsupported structured gap %q", gap.Kind)
	}
	return nil
}

func renderStructuredGapQuery(gap *evidencecompiler.StructuredGap) (string, error) {
	if gap == nil {
		return "", fmt.Errorf("missing structured gap")
	}
	if err := validateStructuredGap(*gap); err != nil {
		return "", err
	}
	sourceNeed := strconv.Quote(strings.TrimSpace(gap.SourceNeed))
	switch gap.Kind {
	case evidencecompiler.GapEntity:
		return "gap:entity entity=" + strconv.Quote(strings.TrimSpace(gap.Entity)) + " source_need=" + sourceNeed, nil
	case evidencecompiler.GapTimeRange:
		return "gap:time_range start=" + strconv.Quote(gap.Start.UTC().Format(time.RFC3339)) + " end=" + strconv.Quote(gap.End.UTC().Format(time.RFC3339)) + " source_need=" + sourceNeed, nil
	case evidencecompiler.GapSecondOperand:
		return "gap:second_operand operand=" + strconv.Quote(strings.TrimSpace(gap.Operand)) + " source_need=" + sourceNeed, nil
	default:
		return "", fmt.Errorf("unsupported structured gap %q", gap.Kind)
	}
}

func stableGapCandidateUnion(initial, supplemental []evidencecompiler.Candidate, candidateLimit int) ([]evidencecompiler.Candidate, int, error) {
	if candidateLimit <= 0 {
		return nil, 0, fmt.Errorf("candidate limit must be positive")
	}
	union := cloneCandidates(initial)
	byID := make(map[string]evidencecompiler.Candidate, len(initial)+len(supplemental))
	for _, candidate := range initial {
		byID[candidate.ID] = candidate
	}
	added := 0
	for _, candidate := range supplemental {
		if existing, exists := byID[candidate.ID]; exists {
			if !sameCandidateLineage(existing, candidate) {
				return nil, 0, fmt.Errorf("supplemental candidate %q conflicts with frozen candidate lineage", candidate.ID)
			}
			continue
		}
		if len(union) >= candidateLimit {
			return nil, 0, fmt.Errorf("candidate union exceeds frozen limit %d", candidateLimit)
		}
		byID[candidate.ID] = candidate
		union = append(union, cloneCandidates([]evidencecompiler.Candidate{candidate})[0])
		added++
	}
	return union, added, nil
}

func sameCandidateLineage(left, right evidencecompiler.Candidate) bool {
	if len(left.SourceIDs) != len(right.SourceIDs) {
		return false
	}
	for index := range left.SourceIDs {
		if left.SourceIDs[index] != right.SourceIDs[index] {
			return false
		}
	}
	return true
}

func cloneStructuredGap(gap *evidencecompiler.StructuredGap) *evidencecompiler.StructuredGap {
	if gap == nil {
		return nil
	}
	cloned := *gap
	if gap.Start != nil {
		start := gap.Start.UTC()
		cloned.Start = &start
	}
	if gap.End != nil {
		end := gap.End.UTC()
		cloned.End = &end
	}
	return &cloned
}

func sameStructuredGap(left, right evidencecompiler.StructuredGap) bool {
	if left.Kind != right.Kind || left.Entity != right.Entity || left.Operand != right.Operand || left.SourceNeed != right.SourceNeed {
		return false
	}
	return sameGapTime(left.Start, right.Start) && sameGapTime(left.End, right.End)
}

func sameGapTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
