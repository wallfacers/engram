package evidencecompiler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/extract"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/resolve"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

// Compiler has no retriever, store, or answerer dependency. Its only external
// capabilities are the bounded resolver, actual token counter, and renderer.
type Compiler struct {
	cfg Config
}

func New(cfg Config) (*Compiler, error) {
	if cfg.TokenCap <= 0 || cfg.MaxCandidates <= 0 || cfg.MaxSources <= 0 || strings.TrimSpace(cfg.CounterFingerprint) == "" {
		return nil, fmt.Errorf("%w: positive cap/limits and counter fingerprint are required", ErrInvalidBundle)
	}
	if nilCapability(cfg.Resolver) {
		return nil, fmt.Errorf("%w: nil source resolver", ErrSourceUnavailable)
	}
	if nilCapability(cfg.Counter) {
		return nil, fmt.Errorf("%w: nil token counter", ErrCounterUnavailable)
	}
	if nilCapability(cfg.Renderer) {
		return nil, fmt.Errorf("%w: nil answer renderer", ErrInvalidBundle)
	}
	if nilCapability(cfg.Planner) {
		cfg.Planner = nil
	}
	return &Compiler{cfg: cfg}, nil
}

// Compile creates a compiler from the request's fixed configuration and
// compiles exactly the candidate bytes supplied by the caller.
func Compile(ctx context.Context, req CompileRequest) (Bundle, Trace, error) {
	compiler, err := New(compileConfig(req))
	if err != nil {
		return Bundle{}, Trace{}, err
	}
	return compiler.Compile(ctx, req)
}

// Compile performs one source-bounded, answerer-free compilation.
func (c *Compiler) Compile(ctx context.Context, req CompileRequest) (Bundle, Trace, error) {
	if c == nil {
		return Bundle{}, Trace{}, fmt.Errorf("%w: nil compiler", ErrInvalidBundle)
	}
	if ctx == nil {
		return Bundle{}, Trace{}, fmt.Errorf("%w: nil context", ErrInvalidBundle)
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, Trace{}, err
	}
	if strings.TrimSpace(req.Query) == "" {
		return Bundle{}, Trace{}, fmt.Errorf("%w: empty query", ErrInvalidNeed)
	}

	candidates, err := validate.ValidateCandidates(req.Candidates, c.cfg.MaxCandidates)
	if err != nil {
		return Bundle{}, Trace{}, err
	}
	if len(candidates.SourceIDs) > c.cfg.MaxSources {
		return Bundle{}, Trace{}, fmt.Errorf("%w: %d sources exceeds limit %d", ErrInvalidCandidate, len(candidates.SourceIDs), c.cfg.MaxSources)
	}
	trace := Trace{
		Need:               BuildNeed(req.Query),
		CandidateDigest:    candidates.Digest,
		CandidateIDs:       candidateIDs(candidates.Ordered),
		CandidateSourceIDs: append([]string(nil), candidates.SourceIDs...),
	}
	sources, err := resolve.ResolveSources(ctx, c.cfg.Resolver, candidates.Allowlist, resolve.CanonicalResolverIDs(candidates.Allowlist))
	if err != nil {
		return Bundle{}, trace, err
	}

	staticBundle, err := c.countItems(ctx, req.Query, nil)
	if err != nil {
		return Bundle{}, trace, err
	}
	trace.TokenSteps = append(trace.TokenSteps, tokenStep("static_prompt", "", staticBundle, c.cfg.TokenCap))
	if staticBundle.InputTokens > c.cfg.TokenCap {
		c.finishTrace(&trace, sources, nil)
		return Bundle{}, trace, ErrBudgetImpossible
	}

	needNeed, proposed, plannerActions, fallbackReason, err := c.plannerProposal(ctx, req.Query, candidates, sources, trace.Need)
	trace.ProposedActions = proposed
	trace.FallbackReason = fallbackReason
	if err != nil {
		return Bundle{}, trace, err
	}
	trace.Need = needNeed
	plan, err := extract.BuildExtractionPlan(needNeed, candidates, sources)
	if err != nil {
		return Bundle{}, trace, err
	}

	rawBundle, err := c.countItems(ctx, req.Query, plan.Raw)
	if err != nil {
		return Bundle{}, trace, err
	}
	trace.TokenSteps = append(trace.TokenSteps, tokenStep("raw_admission", "all", rawBundle, c.cfg.TokenCap))
	selected := make([]extract.EvidenceItem, 0)
	if rawBundle.InputTokens <= c.cfg.TokenCap {
		selected = extract.CloneEvidenceItems(plan.Raw)
		trace.AppliedActions = actionsForEvidenceItems(selected)
	} else {
		selected, trace.Dropped, err = c.admitItems(ctx, req.Query, plan.Extracts, &trace, "extract_add")
		if err != nil {
			return Bundle{}, trace, err
		}
		if len(selected) == 0 && needRequiresEvidence(needNeed) {
			c.finishTrace(&trace, sources, selected)
			return Bundle{}, trace, ErrBudgetImpossible
		}
		if mergeActions := onlyMergeActions(plannerActions); len(mergeActions) > 0 {
			if !extract.MergePermitted(true, needNeed, selected) {
				trace.FallbackReason = appendFallbackReason(trace.FallbackReason, "planner_merge_not_permitted")
			} else if merged := plannerMergeItems(mergeActions, plan); len(merged) > 0 {
				mergedSelected, mergedDrops, mergeErr := c.admitItems(ctx, req.Query, merged, &trace, "merge_add")
				if mergeErr != nil {
					return Bundle{}, trace, mergeErr
				}
				if len(mergedSelected) > 0 && extract.ExtractiveSatisfiesNeed(needNeed, mergedSelected) {
					selected = mergedSelected
					trace.Dropped = append(trace.Dropped, mergedDrops...)
				} else {
					trace.FallbackReason = appendFallbackReason(trace.FallbackReason, "planner_merge_insufficient")
				}
			}
		}
		trace.AppliedActions = actionsForEvidenceItems(selected)
	}

	bundle, err := c.finalizeBundle(ctx, req.Query, staticBundle.InputTokens, selected, sources, &trace)
	if err != nil {
		if errors.Is(err, ErrBudgetImpossible) {
			c.finishTrace(&trace, sources, selected)
		}
		return Bundle{}, trace, err
	}
	return bundle, trace, nil
}

// compileConfig folds the direct request fields into the immutable Config,
// preferring the direct fields when they are set.
func compileConfig(req CompileRequest) Config {
	cfg := req.Config
	if req.TokenCap != 0 {
		cfg.TokenCap = req.TokenCap
	}
	if req.CounterFingerprint != "" {
		cfg.CounterFingerprint = req.CounterFingerprint
	}
	if req.MaxCandidates != 0 {
		cfg.MaxCandidates = req.MaxCandidates
	}
	if req.MaxSources != 0 {
		cfg.MaxSources = req.MaxSources
	}
	if !nilCapability(req.Planner) {
		cfg.Planner = req.Planner
	}
	if !nilCapability(req.Resolver) {
		cfg.Resolver = req.Resolver
	}
	if !nilCapability(req.Counter) {
		cfg.Counter = req.Counter
	}
	if !nilCapability(req.Renderer) {
		cfg.Renderer = req.Renderer
	}
	return cfg
}

func nilCapability(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
