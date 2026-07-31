package evidencecompiler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler/internal/extract"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/need"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/render"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/resolve"
	"github.com/wallfacers/engram/memory/evidencecompiler/internal/validate"
)

// plannerProposal runs the optional Planner, validates its proposal against
// the frozen candidates and resolved sources, and merges its Need with the
// deterministic base. Any invalid or failing proposal degrades to the
// deterministic path with a recorded fallback reason.
func (c *Compiler) plannerProposal(ctx context.Context, query string, candidates validate.ValidatedCandidates, sources map[string]Source, base EvidenceNeed) (EvidenceNeed, []Action, []Action, string, error) {
	if c.cfg.Planner == nil {
		return base, nil, nil, "planner_unavailable", nil
	}
	proposal, err := c.cfg.Planner.Propose(ctx, query, cloneCandidates(candidates.Ordered))
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
	merged, err := need.MergePlannerNeed(base, proposal.Need)
	if err != nil {
		return base, proposed, nil, "planner_invalid_need", nil
	}
	for _, action := range proposal.Actions {
		if err := validate.ValidateAction(action, candidates, sources); err != nil {
			return base, proposed, nil, "planner_invalid_action", nil
		}
	}
	return merged, proposed, cloneActions(proposal.Actions), "", nil
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

func (c *Compiler) countItems(ctx context.Context, query string, items []extract.EvidenceItem) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	bundle := render.RenderBundle(items)
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

func (c *Compiler) admitItems(ctx context.Context, query string, items []extract.EvidenceItem, trace *Trace, operation string) ([]extract.EvidenceItem, []DropRecord, error) {
	selected := make([]extract.EvidenceItem, 0, len(items))
	dropped := make([]DropRecord, 0)
	for _, item := range items {
		candidate := append(extract.CloneEvidenceItems(selected), item)
		bundle, err := c.countItems(ctx, query, candidate)
		if err != nil {
			return nil, dropped, err
		}
		trace.TokenSteps = append(trace.TokenSteps, tokenStep(operation, extract.EvidenceItemID(item), bundle, c.cfg.TokenCap))
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

func plannerMergeItems(actions []Action, plan extract.ExtractionPlan) []extract.EvidenceItem {
	bySource := make(map[string]extract.EvidenceItem, len(plan.Raw))
	for _, raw := range plan.Raw {
		if len(raw.Sources) == 1 {
			bySource[raw.Sources[0].SourceID] = raw
		}
	}
	items := make([]extract.EvidenceItem, 0)
	for _, action := range actions {
		for _, sentence := range action.Sentences {
			candidateSet := make(map[string]bool)
			rank := 0
			for _, span := range sentence.Sources {
				if raw, ok := bySource[span.SourceID]; ok {
					if rank == 0 || raw.Rank < rank {
						rank = raw.Rank
					}
					for _, candidateID := range raw.CandidateIDs {
						candidateSet[candidateID] = true
					}
				}
			}
			items = append(items, extract.EvidenceItem{
				Kind:         ActionMerge,
				Text:         sentence.Text,
				Sources:      append([]SourceSpan(nil), sentence.Sources...),
				CandidateIDs: resolve.CanonicalResolverIDs(candidateSet),
				Rank:         rank,
			})
		}
	}
	return items
}

func actionsForEvidenceItems(items []extract.EvidenceItem) []Action {
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

func needRequiresEvidence(needNeed EvidenceNeed) bool {
	return len(needNeed.Entities) > 0 || len(needNeed.TimeConstraints) > 0 || len(needNeed.Operands) > 0 || needNeed.ListCardinality.Known || needNeed.UpdateState != ""
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

func (c *Compiler) finalizeBundle(ctx context.Context, query string, staticTokens int, items []extract.EvidenceItem, sources map[string]Source, trace *Trace) (Bundle, error) {
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
	digest, err := render.TraceDigest(*trace)
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: trace digest: %v", ErrInvalidBundle, err)
	}
	bundle.TraceDigest = digest
	if err := render.ValidateBundle(bundle, *trace, traceAllowlist(trace), sources, c.cfg.CounterFingerprint); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func (c *Compiler) finishTrace(trace *Trace, sources map[string]Source, items []extract.EvidenceItem) {
	selectedSources := sourcesForItems(sources, items)
	trace.Relations = need.BuildRelations(trace.Need, selectedSources)
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

func sourcesForItems(sources map[string]Source, items []extract.EvidenceItem) map[string]Source {
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

func deriveRemainingGap(needNeed EvidenceNeed, items []extract.EvidenceItem, sources map[string]Source) *StructuredGap {
	if needNeed.Gap != nil {
		return need.CloneGap(needNeed.Gap)
	}
	var evidence strings.Builder
	for _, item := range items {
		if evidence.Len() > 0 {
			evidence.WriteByte('\n')
		}
		evidence.WriteString(item.Text)
	}
	text := strings.ToLower(evidence.String())
	for _, entity := range needNeed.Entities {
		if !strings.Contains(text, strings.ToLower(entity)) {
			return &StructuredGap{Kind: GapEntity, Entity: entity, SourceNeed: "entity:" + entity}
		}
	}
	if len(needNeed.TimeConstraints) > 0 && !selectedHasTimeEvidence(items, sources) {
		if start, ok := explicitNeedDate(needNeed); ok {
			return &StructuredGap{Kind: GapTimeRange, Start: &start, SourceNeed: "time:" + strings.Join(needNeed.TimeConstraints, ",")}
		}
	}
	for _, operand := range needNeed.Operands {
		if !need.SourceSupportsOperand(evidence.String(), operand.Name) {
			return &StructuredGap{Kind: GapSecondOperand, Operand: operand.Name, SourceNeed: "operand:" + operand.Name}
		}
	}
	return nil
}

func selectedHasTimeEvidence(items []extract.EvidenceItem, sources map[string]Source) bool {
	for _, item := range items {
		if extract.HasTimeEvidence(item.Text) {
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

func explicitNeedDate(needNeed EvidenceNeed) (time.Time, bool) {
	for _, constraint := range needNeed.TimeConstraints {
		if parsed, err := time.Parse("2006-01-02", constraint); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
