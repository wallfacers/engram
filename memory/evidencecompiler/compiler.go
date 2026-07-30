package evidencecompiler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

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
	compiler, err := New(req.compileConfig())
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

	candidates, err := validateCandidates(req.Candidates, c.cfg.MaxCandidates)
	if err != nil {
		return Bundle{}, Trace{}, err
	}
	if len(candidates.sourceIDs) > c.cfg.MaxSources {
		return Bundle{}, Trace{}, fmt.Errorf("%w: %d sources exceeds limit %d", ErrInvalidCandidate, len(candidates.sourceIDs), c.cfg.MaxSources)
	}
	trace := Trace{
		Need:               BuildNeed(req.Query),
		CandidateDigest:    candidates.digest,
		CandidateIDs:       candidateIDs(candidates.ordered),
		CandidateSourceIDs: append([]string(nil), candidates.sourceIDs...),
	}
	sources, err := resolveSources(ctx, c.cfg.Resolver, candidates.allowlist, canonicalResolverIDs(candidates.allowlist))
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

	need, proposed, plannerActions, fallbackReason, err := c.plannerProposal(ctx, req.Query, candidates, sources, trace.Need)
	trace.ProposedActions = proposed
	trace.FallbackReason = fallbackReason
	if err != nil {
		return Bundle{}, trace, err
	}
	trace.Need = need
	plan, err := buildExtractionPlan(need, candidates, sources)
	if err != nil {
		return Bundle{}, trace, err
	}

	rawBundle, err := c.countItems(ctx, req.Query, plan.raw)
	if err != nil {
		return Bundle{}, trace, err
	}
	trace.TokenSteps = append(trace.TokenSteps, tokenStep("raw_admission", "all", rawBundle, c.cfg.TokenCap))
	selected := make([]evidenceItem, 0)
	if rawBundle.InputTokens <= c.cfg.TokenCap {
		selected = cloneEvidenceItems(plan.raw)
		trace.AppliedActions = actionsForEvidenceItems(selected)
	} else {
		selected, trace.Dropped, err = c.admitItems(ctx, req.Query, plan.extracts, &trace, "extract_add")
		if err != nil {
			return Bundle{}, trace, err
		}
		if len(selected) == 0 && needRequiresEvidence(need) {
			c.finishTrace(&trace, sources, selected)
			return Bundle{}, trace, ErrBudgetImpossible
		}
		if mergeActions := onlyMergeActions(plannerActions); len(mergeActions) > 0 {
			if !mergePermitted(true, need, selected) {
				trace.FallbackReason = appendFallbackReason(trace.FallbackReason, "planner_merge_not_permitted")
			} else if merged := plannerMergeItems(mergeActions, plan); len(merged) > 0 {
				mergedSelected, mergedDrops, mergeErr := c.admitItems(ctx, req.Query, merged, &trace, "merge_add")
				if mergeErr != nil {
					return Bundle{}, trace, mergeErr
				}
				if len(mergedSelected) > 0 && extractiveSatisfiesNeed(need, mergedSelected) {
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

func (req CompileRequest) compileConfig() Config {
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

func (c *Compiler) plannerProposal(ctx context.Context, query string, candidates validatedCandidates, sources map[string]Source, base EvidenceNeed) (EvidenceNeed, []Action, []Action, string, error) {
	if c.cfg.Planner == nil {
		return base, nil, nil, "planner_unavailable", nil
	}
	proposal, err := c.cfg.Planner.Propose(ctx, query, cloneCandidates(candidates.ordered))
	proposed := cloneActions(proposal.Actions)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return EvidenceNeed{}, proposed, nil, "", ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return EvidenceNeed{}, proposed, nil, "", err
		}
		return base, proposed, nil, "planner_error", nil
	}
	if err := ctx.Err(); err != nil {
		return EvidenceNeed{}, proposed, nil, "", err
	}
	need, err := mergePlannerNeed(base, proposal.Need)
	if err != nil {
		return base, proposed, nil, "planner_invalid_need", nil
	}
	for _, action := range proposal.Actions {
		if err := validateAction(action, candidates, sources); err != nil {
			return base, proposed, nil, "planner_invalid_action", nil
		}
	}
	return need, proposed, cloneActions(proposal.Actions), "", nil
}

func cloneCandidates(candidates []Candidate) []Candidate {
	clone := make([]Candidate, len(candidates))
	for index, candidate := range candidates {
		clone[index] = candidate
		clone[index].SourceIDs = append([]string(nil), candidate.SourceIDs...)
		if candidate.Metadata != nil {
			clone[index].Metadata = make(map[string]string, len(candidate.Metadata))
			for key, value := range candidate.Metadata {
				clone[index].Metadata[key] = value
			}
		}
	}
	return clone
}

func cloneActions(actions []Action) []Action {
	clone := make([]Action, len(actions))
	for index, action := range actions {
		clone[index] = action
		if action.Span != nil {
			span := *action.Span
			clone[index].Span = &span
		}
		clone[index].Sentences = make([]GroundedSentence, len(action.Sentences))
		for sentenceIndex, sentence := range action.Sentences {
			clone[index].Sentences[sentenceIndex] = GroundedSentence{Text: sentence.Text, Sources: append([]SourceSpan(nil), sentence.Sources...)}
		}
	}
	return clone
}

func candidateIDs(candidates []Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func (c *Compiler) countItems(ctx context.Context, query string, items []evidenceItem) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	bundle := renderBundle(items)
	input := c.cfg.Renderer.RenderAnswerInput(query, bundle.RenderedContext)
	count, err := c.cfg.Counter.CountInput(ctx, input)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Bundle{}, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Bundle{}, err
		}
		return Bundle{}, fmt.Errorf("%w: %v", ErrCounterUnavailable, err)
	}
	if count.InputTokens <= 0 {
		return Bundle{}, fmt.Errorf("%w: non-positive token count", ErrCounterUnavailable)
	}
	if count.Fingerprint != c.cfg.CounterFingerprint {
		return Bundle{}, fmt.Errorf("%w: got %q want %q", ErrFingerprintMismatch, count.Fingerprint, c.cfg.CounterFingerprint)
	}
	bundle.InputTokens = count.InputTokens
	bundle.TokenCap = c.cfg.TokenCap
	bundle.CounterFingerprint = count.Fingerprint
	return bundle, nil
}

func tokenStep(operation, itemID string, bundle Bundle, cap int) TokenStep {
	return TokenStep{Operation: operation, ItemID: itemID, FullAnswerInputTokens: bundle.InputTokens, TokenCap: cap}
}

func (c *Compiler) admitItems(ctx context.Context, query string, items []evidenceItem, trace *Trace, operation string) ([]evidenceItem, []DropRecord, error) {
	selected := make([]evidenceItem, 0, len(items))
	dropped := make([]DropRecord, 0)
	for _, item := range items {
		candidate := append(cloneEvidenceItems(selected), item)
		bundle, err := c.countItems(ctx, query, candidate)
		if err != nil {
			return nil, dropped, err
		}
		trace.TokenSteps = append(trace.TokenSteps, tokenStep(operation, evidenceItemID(item), bundle, c.cfg.TokenCap))
		if bundle.InputTokens <= c.cfg.TokenCap {
			selected = candidate
			continue
		}
		for _, candidateID := range item.CandidateIDs {
			dropped = append(dropped, DropRecord{CandidateID: candidateID, ReasonCode: "token_cap"})
		}
	}
	return selected, canonicalDrops(dropped), nil
}

func canonicalDrops(drops []DropRecord) []DropRecord {
	seen := make(map[string]bool, len(drops))
	canonical := make([]DropRecord, 0, len(drops))
	for _, drop := range drops {
		key := drop.CandidateID + "\x00" + drop.ReasonCode
		if drop.CandidateID == "" || drop.ReasonCode == "" || seen[key] {
			continue
		}
		seen[key] = true
		canonical = append(canonical, drop)
	}
	return canonical
}

func onlyMergeActions(actions []Action) []Action {
	merges := make([]Action, 0)
	for _, action := range actions {
		if action.Kind == ActionMerge {
			merges = append(merges, action)
		}
	}
	return merges
}

func plannerMergeItems(actions []Action, plan extractionPlan) []evidenceItem {
	bySource := make(map[string]evidenceItem, len(plan.raw))
	for _, raw := range plan.raw {
		if len(raw.Sources) == 1 {
			bySource[raw.Sources[0].SourceID] = raw
		}
	}
	items := make([]evidenceItem, 0)
	for _, action := range actions {
		for _, sentence := range action.Sentences {
			candidateSet := make(map[string]bool)
			rank := 0
			for _, span := range sentence.Sources {
				if raw, ok := bySource[span.SourceID]; ok {
					if rank == 0 || raw.rank < rank {
						rank = raw.rank
					}
					for _, candidateID := range raw.CandidateIDs {
						candidateSet[candidateID] = true
					}
				}
			}
			items = append(items, evidenceItem{
				Kind:         ActionMerge,
				Text:         sentence.Text,
				Sources:      append([]SourceSpan(nil), sentence.Sources...),
				CandidateIDs: canonicalResolverIDs(candidateSet),
				rank:         rank,
			})
		}
	}
	return items
}

func actionsForEvidenceItems(items []evidenceItem) []Action {
	actions := make([]Action, 0, len(items))
	for _, item := range items {
		if len(item.Sources) == 0 {
			continue
		}
		switch item.Kind {
		case ActionKeep:
			actions = append(actions, Action{Kind: ActionKeep, SourceID: item.Sources[0].SourceID})
		case ActionExtract:
			span := item.Sources[0]
			actions = append(actions, Action{Kind: ActionExtract, Span: &span})
		case ActionMerge:
			actions = append(actions, Action{Kind: ActionMerge, Sentences: []GroundedSentence{{Text: item.Text, Sources: append([]SourceSpan(nil), item.Sources...)}}})
		}
	}
	return actions
}

func needRequiresEvidence(need EvidenceNeed) bool {
	return len(need.Entities) > 0 || len(need.TimeConstraints) > 0 || len(need.Operands) > 0 || need.ListCardinality.Known || need.UpdateState != ""
}

func appendFallbackReason(existing, reason string) string {
	if existing == "" {
		return reason
	}
	for _, part := range strings.Split(existing, "+") {
		if part == reason {
			return existing
		}
	}
	return existing + "+" + reason
}

func (c *Compiler) finalizeBundle(ctx context.Context, query string, staticTokens int, items []evidenceItem, sources map[string]Source, trace *Trace) (Bundle, error) {
	bundle, err := c.countItems(ctx, query, items)
	if err != nil {
		return Bundle{}, err
	}
	trace.TokenSteps = append(trace.TokenSteps, tokenStep("final_validation", "all", bundle, c.cfg.TokenCap))
	if bundle.InputTokens > c.cfg.TokenCap {
		return Bundle{}, ErrBudgetImpossible
	}
	bundle.EvidenceTokens = bundle.InputTokens - staticTokens
	if bundle.EvidenceTokens < 0 {
		bundle.EvidenceTokens = 0
	}
	c.finishTrace(trace, sources, items)
	digest, err := traceDigest(*trace)
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: trace digest: %v", ErrInvalidBundle, err)
	}
	bundle.TraceDigest = digest
	if err := validateBundle(bundle, *trace, traceAllowlist(trace), sources, c.cfg.CounterFingerprint); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func (c *Compiler) finishTrace(trace *Trace, sources map[string]Source, items []evidenceItem) {
	selectedSources := sourcesForItems(sources, items)
	trace.Relations = buildRelations(trace.Need, selectedSources)
	trace.RemainingGap = deriveRemainingGap(trace.Need, items, selectedSources)
	trace.Valid = true
}

func traceAllowlist(trace *Trace) map[string]bool {
	allowlist := make(map[string]bool, len(trace.CandidateSourceIDs))
	for _, sourceID := range trace.CandidateSourceIDs {
		allowlist[sourceID] = true
	}
	return allowlist
}

func sourcesForItems(sources map[string]Source, items []evidenceItem) map[string]Source {
	selected := make(map[string]Source)
	for _, item := range items {
		for _, span := range item.Sources {
			if source, ok := sources[span.SourceID]; ok {
				selected[span.SourceID] = source
			}
		}
	}
	return selected
}

func deriveRemainingGap(need EvidenceNeed, items []evidenceItem, sources map[string]Source) *StructuredGap {
	if need.Gap != nil {
		return cloneGap(need.Gap)
	}
	var evidence strings.Builder
	for _, item := range items {
		if evidence.Len() > 0 {
			evidence.WriteByte('\n')
		}
		evidence.WriteString(item.Text)
	}
	text := strings.ToLower(evidence.String())
	for _, entity := range need.Entities {
		if !strings.Contains(text, strings.ToLower(entity)) {
			return &StructuredGap{Kind: GapEntity, Entity: entity, SourceNeed: "entity:" + entity}
		}
	}
	if len(need.TimeConstraints) > 0 && !selectedHasTimeEvidence(items, sources) {
		if start, ok := explicitNeedDate(need); ok {
			return &StructuredGap{Kind: GapTimeRange, Start: &start, SourceNeed: "time:" + strings.Join(need.TimeConstraints, ",")}
		}
	}
	for _, operand := range need.Operands {
		if !sourceSupportsOperand(evidence.String(), operand.Name) {
			return &StructuredGap{Kind: GapSecondOperand, Operand: operand.Name, SourceNeed: "operand:" + operand.Name}
		}
	}
	return nil
}

func selectedHasTimeEvidence(items []evidenceItem, sources map[string]Source) bool {
	for _, item := range items {
		if hasTimeEvidence(item.Text) {
			return true
		}
		for _, span := range item.Sources {
			if source, ok := sources[span.SourceID]; ok && source.OccurredAt != nil {
				return true
			}
		}
	}
	return false
}

func explicitNeedDate(need EvidenceNeed) (time.Time, bool) {
	for _, constraint := range need.TimeConstraints {
		if parsed, err := time.Parse("2006-01-02", constraint); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
