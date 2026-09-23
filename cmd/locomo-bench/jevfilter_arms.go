package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// This file owns the 051 four-arm evaluation recipes (spec §23 / tasks T13-T14)
// and the retrieval plumbing they share: the arm table, the filter-aware
// retrieval seam payload, the packer admission that defines "shown", and the
// arm-B untruncated assertion. Every arm differs from its neighbour in exactly
// one variable so a paired contrast stays attributable:
//
//	A -> B  budget      (shown budget 8 -> wide pool 150)
//	B -> D  filter      (no filter -> Jev relevance filter)
//	C -> D  selection   (rank truncation -> per-query calibrated filtering)
//
// The engine is untouched: arms only call the public memory.Retriever API
// (Search / SearchFiltered) and the filter package's pure Select policy.

// jevArm names the four §23 arms plus the D-noRelax kill-switch variant.
type jevArm string

const (
	jevArmA        jevArm = "A"
	jevArmB        jevArm = "B"
	jevArmC        jevArm = "C"
	jevArmD        jevArm = "D"
	jevArmDNoRelax jevArm = "D-noRelax"
)

const (
	// jevArmPoolSize is the frozen wide-pool size shared by the B/C/D recipes
	// (spec §23: 150). Arms that widen use exactly this pool so the A->B budget
	// contrast is a single-variable change.
	jevArmPoolSize = 150
	// jevArmCShowGate and jevArmCShowReference are arm C's truncation budgets:
	// the SC-003 gate compares C@12 against D, and C@8 is the production
	// reference (spec SC-003).
	jevArmCShowGate      = 12
	jevArmCShowReference = 8
	// jevArmAnswerInputCap is the single frozen answer-input token cap for all
	// arms (research R7b). It is roomy enough for arm B's ~8.5k-token bundle, so
	// B is never silently weakened: a budget stop invalidates the run instead.
	jevArmAnswerInputCap = 32768
	// jevArmAnswerRepetitions is the frozen 3-rep majority protocol (§23).
	jevArmAnswerRepetitions = 3
	// jevArmCategoryFiveBlock names the declared LoCoMo category-5 adversarial
	// block. Its questions are reported separately and never enter the 1540
	// main denominator (spec SC-004).
	jevArmCategoryFiveBlock = "category5"
	// jevArmMainBlock names the main population.
	jevArmMainBlock = "main"
)

// filterRetrieval carries the optional wide-pool + Jev filter spec through the
// shared retrieval seam. A nil receiver — or one whose Filter is nil or a typed
// nil — preserves the legacy quota path byte-for-byte (SC-006 parity). It is a
// runtime-only value: it is never serialized into a protocol or a result row.
type filterRetrieval struct {
	Pool   int
	Show   int
	Filter filter.RelevanceFilter
	Policy filter.Policy
}

// active reports whether the spec asks for real filtering. It collapses typed
// nils at the boundary (AGENTS.md typed-nil discipline): the retriever must see
// "no filter", not a non-nil interface wrapping a nil client.
func (fr *filterRetrieval) active() bool {
	return fr != nil && !filter.IsNilRelevanceFilter(fr.Filter)
}

// jevArmRecipe is one arm's frozen retrieval recipe.
type jevArmRecipe struct {
	Arm jevArm
	// Pool is the wide-pool size retriever.Search is asked for. Several recipes
	// share a pool because the arms are contrasted on what they show, not on how
	// wide they look.
	Pool int
	// Presented is how many pool entries the arm hands to the packer: 0 means the
	// whole pool (arm B). Arm D does not use it — the filter's Select decides.
	Presented int
	// Filtered marks the arms whose shortlist comes from SearchFiltered.
	Filtered bool
	// RelaxDisabled is the ENGRAM_JEV_RELAX=0 kill-switch; only D-noRelax sets it.
	RelaxDisabled bool
	// Description documents the recipe in the run report.
	Description string
}

// jevArmRecipes returns the frozen arm table in report order. The list is
// deliberately static: recipes are evaluation configuration, so changing one is
// an eval-config change that must be committed separately from algorithm work
// (Constitution IV attribution rule).
func jevArmRecipes() []jevArmRecipe {
	return []jevArmRecipe{
		{
			Arm:         jevArmA,
			Presented:   jevArmCShowReference,
			Description: "production baseline: RRF limit=8",
		},
		{
			Arm:         jevArmB,
			Pool:        jevArmPoolSize,
			Description: "eval wide pool: RRF k=150, the whole pool is presented",
		},
		{
			Arm:         jevArmC,
			Pool:        jevArmPoolSize,
			Presented:   jevArmCShowGate,
			Description: "wide pool truncated: first 12 of the 150-pool order (C@8 is reported as the production reference)",
		},
		{
			Arm:         jevArmD,
			Pool:        jevArmPoolSize,
			Filtered:    true,
			Description: "wide pool -> Jev relevance filter; the threshold policy decides the shortlist",
		},
		{
			Arm:           jevArmDNoRelax,
			Pool:          jevArmPoolSize,
			Filtered:      true,
			RelaxDisabled: true,
			Description:   "same as D with the relax stage disabled (ENGRAM_JEV_RELAX=0); SC-004 is measured on this variant",
		},
	}
}

// jevArmNames returns every arm name in recipe order.
func jevArmNames() []jevArm {
	recipes := jevArmRecipes()
	names := make([]jevArm, 0, len(recipes))
	for _, recipe := range recipes {
		names = append(names, recipe.Arm)
	}
	return names
}

// jevArmRecipeFor resolves one arm's recipe.
func jevArmRecipeFor(arm jevArm) (jevArmRecipe, error) {
	for _, recipe := range jevArmRecipes() {
		if recipe.Arm == arm {
			return recipe, nil
		}
	}
	return jevArmRecipe{}, fmt.Errorf("unknown jev arm %q", arm)
}

// jevArmPolicy derives the threshold policy for a recipe from the run's
// pre-registered policy, applying the arm's kill-switch. Pre-registration means
// the policy is frozen before the gated run: sweeps are diagnostic only
// (research R3a).
func jevArmPolicy(recipe jevArmRecipe, base filter.Policy) filter.Policy {
	pol := base.WithDefaults()
	if recipe.RelaxDisabled {
		pol.RelaxDisabled = true
	}
	return pol
}

// jevArmTopKSplit resolves one arm's internal pool-k/show-k from the run's
// --top-k (tasks T13). Arm A keeps --top-k as its shown budget and lets the
// production front door do its own internal widening; the evaluator arms own a
// frozen pool and presentation budget, so --top-k only bounds the production
// baseline. Reporting both numbers keeps a run's log from carrying one ambiguous
// "k".
func jevArmTopKSplit(opt options, recipe jevArmRecipe) (poolK, showK int) {
	if recipe.Arm == jevArmA {
		showK = opt.topK
		if showK <= 0 {
			showK = 8 // memory.Search's own default k
		}
		return 0, showK
	}
	poolK = recipe.Pool
	if recipe.Presented > 0 {
		showK = recipe.Presented
	}
	return poolK, showK
}

// jevArmPacked is the packer's admission result for one arm and question: the
// prefix the answerer actually sees ("shown") and the exact input-token count of
// that bundle. BudgetStop records that the frozen cap — not the arm's own
// presentation list — ended admission, which is what makes arm B invalid.
type jevArmPacked struct {
	Admitted   []memory.Result
	Tokens     int
	BudgetStop bool
}

// jevArmObservation is one arm's retrieval + packing result for one question.
type jevArmObservation struct {
	Arm jevArm
	// Pool is the wide-pool snapshot used for pool recall@150 and for arm C's
	// equal-budget variants. It is nil for arm A, which has no pool of its own.
	// Taking the snapshot costs one extra local search; it is measurement only
	// and never part of the answer path.
	Pool []memory.Result
	// Presented is the candidate list handed to the packer.
	Presented []memory.Result
	// Packed is the packer result; Packed.Admitted is the arm's "shown" set.
	Packed jevArmPacked
	// Meta is the filter telemetry (zero value for unfiltered arms).
	Meta filter.FilterMeta
	// Degraded records that the filter failed and the arm fell back to RRF.
	Degraded bool
	// RetrievalMs is the wall time of this arm's retrieval, excluding packing.
	RetrievalMs int
}

// shown returns the packer-admitted set: the exact evidence list the answerer
// sees, which is the measurement point for "shown" (tasks T13).
func (o jevArmObservation) shown() []memory.Result {
	return o.Packed.Admitted
}

// jevArmRetrieve runs one arm's retrieval for one question. spec is the run's
// filter retrieval payload (the single owner of pool/filter/policy); the arm's own
// relax kill-switch is applied to a copy of its policy. quota mirrors the
// production chunk-quota configuration: when it is positive the pool snapshot is
// partitioned exactly like retrieveWithQuotaDiagnostics does, so pool-based
// metrics are measured on the same list the production pipeline would have used.
// The four-arm protocol pins quota to 0 (validateJevArmsOptions), because a
// positive quota partitions arm C's pool while the filtered arms filter the raw
// pool — selection would no longer be the only variable between them.
//
// productionLimit is the caller-facing limit (arm A's shown budget, --top-k with
// memory.Search's default of 8). The filtered arms pass it to SearchFiltered as
// the degrade truncation width, which is what makes SC-005's "degraded shown set
// == C@8" comparison true by construction.
func jevArmRetrieve(ctx context.Context, r *memory.Retriever, query string, recipe jevArmRecipe, spec *filterRetrieval, quota, productionLimit int) (observation jevArmObservation, err error) {
	if r == nil {
		return jevArmObservation{}, fmt.Errorf("jev arm %s requires a retriever", recipe.Arm)
	}
	if productionLimit <= 0 {
		productionLimit = jevArmCShowReference // memory.Search's own default k
	}
	pol := filter.DefaultPolicy()
	var flt filter.RelevanceFilter
	poolSize := 0
	if spec != nil {
		pol = spec.Policy
		flt = spec.Filter
		poolSize = spec.Pool
	}
	pol = jevArmPolicy(recipe, pol)
	// observation is a named result so the deferred stamp below genuinely lands in
	// the value the caller receives: with unnamed results Go copies the return
	// values before the deferred function runs, and the retrieval segment would be
	// silently reported as 0ms (SC-007 accounting).
	observation = jevArmObservation{Arm: recipe.Arm}
	started := time.Now()
	defer func() {
		observation.RetrievalMs = int(time.Since(started).Milliseconds())
	}()

	if recipe.Arm == jevArmA {
		// The production front door decides arm A: the seam owns the widening, so
		// arm A stays byte-identical to a plain --top-k run.
		hits, _, err := retrieveWithQuotaDiagnostics(ctx, r, query, productionLimit, quota, nil, nil)
		if err != nil {
			return jevArmObservation{}, fmt.Errorf("jev arm %s retrieve: %w", recipe.Arm, err)
		}
		observation.Presented = hits
		return observation, nil
	}

	if poolSize <= 0 {
		poolSize = recipe.Pool
	}
	pool, err := r.Search(ctx, query, poolSize)
	if err != nil {
		return jevArmObservation{}, fmt.Errorf("jev arm %s pool retrieve: %w", recipe.Arm, err)
	}
	if quota > 0 {
		pool = applyChunkQuota(pool, poolSize, quota)
	}
	observation.Pool = pool

	if !recipe.Filtered {
		presented := pool
		if recipe.Presented > 0 && len(presented) > recipe.Presented {
			presented = presented[:recipe.Presented]
		}
		observation.Presented = presented
		return observation, nil
	}

	if !filter.IsNilRelevanceFilter(flt) {
		// The product path: one wide search plus one filter call. SearchFiltered
		// never surfaces a filter failure as an error — it degrades — so an error
		// here means the underlying search failed.
		shortlist, meta, err := r.SearchFiltered(ctx, query, poolSize, productionLimit, flt, pol)
		if err != nil {
			return jevArmObservation{}, fmt.Errorf("jev arm %s filtered retrieve: %w", recipe.Arm, err)
		}
		observation.Meta = meta
		observation.Degraded = meta.Degraded
		observation.Presented = shortlist
		return observation, nil
	}

	// No configured filter: the arm is on the production degraded path, so the
	// shortlist is produced by the very same composition the product calls with a
	// nil filter (SearchFiltered's none semantics = plain truncation). The
	// observation is flagged degraded so the report can never mistake a missing
	// filter for a working one, and the shown set is what SC-005 compares against
	// arm A / C@8.
	shortlist, meta, err := r.SearchFiltered(ctx, query, poolSize, productionLimit, nil, pol)
	if err != nil {
		return jevArmObservation{}, fmt.Errorf("jev arm %s degraded retrieve: %w", recipe.Arm, err)
	}
	observation.Degraded = true
	observation.Meta = meta
	observation.Presented = shortlist
	return observation, nil
}

// jevArmAnswerRenderer returns the packer's exact-input renderer for one
// question: the same system prompt and context builder the unified answer
// contract uses, so the tokens the packer counts are the real answer input.
func jevArmAnswerRenderer(qa locomoQA, opt options, model string) func([]memory.Result) evidencecompiler.AnswerInput {
	return func(hits []memory.Result) evidencecompiler.AnswerInput {
		return evidencecompiler.AnswerInput{
			Model:  model,
			System: answerSystemPromptForEval(qa, opt),
			User:   buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold),
		}
	}
}

// packJevArmAnswerInput admits candidates in presented order, rendering and
// counting the exact complete answer input before each admission. It mirrors the
// formal ranked-prefix packer (packFormalLegacyInput) so every arm's "shown" set
// is the packer injection count rather than the retrieval-list length. The first
// candidate that does not fit ends admission and sets BudgetStop.
func packJevArmAnswerInput(ctx context.Context, counter evidencecompiler.TokenCounter, cap int, render func([]memory.Result) evidencecompiler.AnswerInput, presented []memory.Result) (jevArmPacked, error) {
	if counter == nil {
		return jevArmPacked{}, fmt.Errorf("jev arm packing requires a token counter")
	}
	if render == nil {
		return jevArmPacked{}, fmt.Errorf("jev arm packing requires an answer-input renderer")
	}
	if cap <= 0 {
		return jevArmPacked{}, fmt.Errorf("jev arm packing requires a positive answer-input cap, got %d", cap)
	}
	admitted := make([]memory.Result, 0, len(presented))
	count, fits, err := countJevArmInput(ctx, counter, render(admitted), cap)
	if err != nil {
		return jevArmPacked{}, err
	}
	if !fits {
		return jevArmPacked{}, fmt.Errorf("jev arm answer prompt uses %d tokens before any candidate, over the frozen cap %d", count.InputTokens, cap)
	}
	packed := jevArmPacked{Admitted: admitted, Tokens: count.InputTokens}
	for _, hit := range presented {
		trial := append(append([]memory.Result(nil), admitted...), hit)
		trialCount, trialFits, err := countJevArmInput(ctx, counter, render(trial), cap)
		if err != nil {
			return jevArmPacked{}, err
		}
		if !trialFits {
			packed.BudgetStop = true
			return packed, nil
		}
		admitted = trial
		packed.Admitted = admitted
		packed.Tokens = trialCount.InputTokens
	}
	return packed, nil
}

func countJevArmInput(ctx context.Context, counter evidencecompiler.TokenCounter, input evidencecompiler.AnswerInput, cap int) (evidencecompiler.TokenCount, bool, error) {
	count, err := counter.CountInput(ctx, input)
	if err != nil {
		return evidencecompiler.TokenCount{}, false, fmt.Errorf("jev arm answer pack preflight: %w", err)
	}
	if count.InputTokens < 1 {
		return count, false, fmt.Errorf("jev arm token counter returned %d input tokens", count.InputTokens)
	}
	return count, count.InputTokens <= cap, nil
}

// assertJevArmBUntruncated is research R7b's hard assertion: arm B's bundle must
// be the whole pool, admitted without ever touching the frozen cap. A budget
// stop, a shorter admission, or a bundle at/over the cap invalidates the run —
// the evidential baseline is never silently weakened and SC-002's token ratio
// stays honest. Callers must report HOLD instead of comparing a truncated B.
func assertJevArmBUntruncated(packed jevArmPacked, poolSize, cap int) error {
	if packed.BudgetStop {
		return fmt.Errorf("arm B hit the frozen answer-input cap %d: the evidential baseline would be silently truncated (run INVALID, report HOLD)", cap)
	}
	if len(packed.Admitted) != poolSize {
		return fmt.Errorf("arm B admitted %d of %d pool candidates without a budget stop (run INVALID, report HOLD)", len(packed.Admitted), poolSize)
	}
	if packed.Tokens >= cap {
		return fmt.Errorf("arm B bundle uses %d tokens, at or over the frozen cap %d (run INVALID, report HOLD)", packed.Tokens, cap)
	}
	return nil
}
