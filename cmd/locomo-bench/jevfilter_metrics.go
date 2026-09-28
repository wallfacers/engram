package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// This file owns the 051 four-arm measurement layer (tasks T14/T16): the
// per-question row, the per-arm aggregates, the equal-budget recall variants
// SC-003 needs, the B<->D flip lists, and the per-segment latency/USD/token
// accounting of SC-007. Metrics are computed from raw hit lists against the same
// gold ruler (`evidenceRecallAt`) the rest of the harness uses, so nothing here
// introduces a second definition of recall.

// jevArmOutcome is the answer+judge stage's result for one arm and question.
// Measured is false when the stage did not run (for example a retrieval-only
// measurement pass): an unmeasured arm stays out of accuracy claims instead of
// being scored as wrong.
type jevArmOutcome struct {
	Measured        bool
	Correct         bool
	Answer          string
	AnswerMs        int
	AnswerInTokens  int
	AnswerOutTokens int
	AnswerUSD       float64
	JudgeCallMade   bool
}

// jevArmQuestionInput is everything the metric layer needs for one question:
// the per-arm observations (raw hit lists plus packer admission) and the
// per-arm answer outcomes, plus the gold ruler.
type jevArmQuestionInput struct {
	Conv         int
	Q            int
	Category     int
	QuestionID   string
	Repetition   int
	QA           locomoQA
	ChunkTurns   map[string][]string
	Observations map[jevArm]jevArmObservation
	Outcomes     map[jevArm]jevArmOutcome
}

// block reports whether the question belongs to the declared category-5
// adversarial block or the main population. The block is reported separately and
// never enters the main denominator (spec SC-004).
func (in jevArmQuestionInput) block() string {
	if in.QA.Adversarial || in.Category == adversarialCategory {
		return jevArmCategoryFiveBlock
	}
	return jevArmMainBlock
}

// jevArmQuestionDerived holds the per-question metrics that need more than one
// arm's observation: arm C's recall under SC-003's three truncation budgets, and
// the arm-D shown count the equal-budget variant is cut to.
type jevArmQuestionDerived struct {
	Conv                   int     `json:"conv"`
	Q                      int     `json:"q"`
	Category               int     `json:"category"`
	QuestionID             string  `json:"question_id,omitempty"`
	Block                  string  `json:"block"`
	Replications           int     `json:"replications"`
	DShown                 int     `json:"d_shown"`
	// EShown is arm E's shown count (facts + span cards). ESpans isolates the
	// appended verbatim span cards so a D->E contrast can attribute the delta
	// to the cards rather than to a filter difference.
	EShown int `json:"e_shown,omitempty"`
	ESpans int `json:"e_spans,omitempty"`
	BudgetGradeable        bool    `json:"budget_gradeable"`
	CBudgetRecallGate      float64 `json:"c_budget_recall_at_12"`
	CBudgetRecallReference float64 `json:"c_budget_recall_at_8"`
	CBudgetRecallEqual     float64 `json:"c_budget_recall_at_d_shown"`
	// DegradedSetComparable is true when both arm A and a filtered arm produced a
	// shown set for this question, so SC-005's "the degraded path shows exactly
	// the no-filter set" claim can be checked. DegradedShownMatchesA carries the
	// answer.
	DegradedSetComparable bool `json:"degraded_set_comparable"`
	DegradedShownMatchesA bool `json:"degraded_shown_matches_a"`
	// DegradedC8Comparable and DegradedShownMatchesC8 are the same check against
	// arm C's pool truncated to the production budget (C@8), which is the
	// comparator spec SC-005 names. Both are reported: A and C@8 are the same RRF
	// top-8 by construction, and a disagreement is worth seeing rather than
	// papering over. Only names and order are compared, never recalled text.
	DegradedC8Comparable   bool `json:"degraded_c8_comparable"`
	DegradedShownMatchesC8 bool `json:"degraded_shown_matches_c8"`
}

// jevArmQuestionRow is one arm's measurement for one question. Every count that
// describes what the answerer saw comes from the packer (shown), never from the
// retrieval list length.
type jevArmQuestionRow struct {
	Conv               int     `json:"conv"`
	Q                  int     `json:"q"`
	Category           int     `json:"category"`
	QuestionID         string  `json:"question_id,omitempty"`
	Block              string  `json:"block"`
	Arm                jevArm  `json:"arm"`
	Repetition         int     `json:"repetition"`
	PoolSize           int     `json:"pool_size"`
	PoolMeasured       bool    `json:"pool_measured"`
	Presented          int     `json:"presented"`
	Shown              int     `json:"shown"`
	AnswerInputTokens  int     `json:"answer_input_tokens"`
	BudgetStopped      bool    `json:"budget_stopped"`
	PoolRecall         float64 `json:"pool_recall"`
	PoolSessionRecall  float64 `json:"pool_session_recall"`
	PoolGradeable      bool    `json:"pool_gradeable"`
	ShownRecall        float64 `json:"shown_recall"`
	ShownSessionRecall float64 `json:"shown_session_recall"`
	Gradeable          bool    `json:"gradeable"`
	ShortlistPrecision float64 `json:"shortlist_precision"`
	PrecisionGradeable bool    `json:"precision_gradeable"`
	EmptyInjection     bool    `json:"empty_injection"`
	Degraded           bool    `json:"degraded"`
	CorrectMeasured    bool    `json:"correct_measured"`
	Correct            bool    `json:"correct"`
	RetrievalMs        int     `json:"retrieval_ms"`
	JevMs              int     `json:"jev_ms"`
	JevUSD             float64 `json:"jev_usd"`
	AnswerMs           int     `json:"answer_ms"`
	AnswerInTokens     int     `json:"answer_in_tokens"`
	AnswerOutTokens    int     `json:"answer_out_tokens"`
	AnswerUSD          float64 `json:"answer_usd"`
}

// measureJevArmQuestion turns one question's observations into rows, one per
// arm. A missing arm observation is an error rather than a skipped row: a
// partially measured run must not silently produce a smaller denominator.
func measureJevArmQuestion(in jevArmQuestionInput) ([]jevArmQuestionRow, jevArmQuestionDerived, error) {
	derived := jevArmQuestionDerived{Conv: in.Conv, Q: in.Q, Category: in.Category, QuestionID: in.QuestionID, Block: in.block()}
	rows := make([]jevArmQuestionRow, 0, len(jevArmNames()))
	for _, arm := range jevArmNames() {
		observation, ok := in.Observations[arm]
		if !ok {
			return nil, jevArmQuestionDerived{}, fmt.Errorf("jev arm %s is missing its observation for conv=%d q=%d", arm, in.Conv, in.Q)
		}
		shown := observation.shown()
		row := jevArmQuestionRow{
			Conv:              in.Conv,
			Q:                 in.Q,
			Category:          in.Category,
			QuestionID:        in.QuestionID,
			Block:             in.block(),
			Arm:               arm,
			Repetition:        in.Repetition,
			PoolSize:          len(observation.Pool),
			PoolMeasured:      observation.Pool != nil,
			Presented:         len(observation.Presented),
			Shown:             len(shown),
			AnswerInputTokens: observation.Packed.Tokens,
			BudgetStopped:     observation.Packed.BudgetStop,
			EmptyInjection:    len(shown) == 0,
			Degraded:          observation.Degraded,
			RetrievalMs:       observation.RetrievalMs,
			JevMs:             observation.Meta.LatencyMs,
			JevUSD:            observation.Meta.CostUSD,
		}
		if observation.Pool != nil {
			turn, session, gradeable := evidenceRecallAt(in.QA, observation.Pool, in.ChunkTurns)
			row.PoolRecall = turn
			row.PoolSessionRecall = session
			row.PoolGradeable = gradeable
		}
		turn, session, gradeable := evidenceRecallAt(in.QA, shown, in.ChunkTurns)
		row.ShownRecall = turn
		row.ShownSessionRecall = session
		row.Gradeable = gradeable
		precision, precisionGradeable := jevShortlistPrecision(in.QA, shown, in.ChunkTurns)
		row.ShortlistPrecision = precision
		row.PrecisionGradeable = precisionGradeable
		if outcome, ok := in.Outcomes[arm]; ok {
			row.CorrectMeasured = outcome.Measured
			row.Correct = outcome.Correct
			row.AnswerMs = outcome.AnswerMs
			row.AnswerInTokens = outcome.AnswerInTokens
			row.AnswerOutTokens = outcome.AnswerOutTokens
			row.AnswerUSD = outcome.AnswerUSD
		}
		rows = append(rows, row)
	}
	// SC-005's structural comparability claim is measured where the hit lists
	// still exist: the production baseline (arm A) and the spec's named comparator
	// (arm C's pool truncated to the production budget) against the filtered arm's
	// shown set.
	if dNoRelax, ok := in.Observations[jevArmDNoRelax]; ok {
		if aObservation, ok := in.Observations[jevArmA]; ok {
			derived.DegradedSetComparable = true
			derived.DegradedShownMatchesA = jevArmShownSetMatches(aObservation.shown(), dNoRelax.shown())
		}
		if cObservation, ok := in.Observations[jevArmC]; ok && cObservation.Pool != nil {
			derived.DegradedC8Comparable = true
			derived.DegradedShownMatchesC8 = jevArmShownSetMatches(truncateResults(cObservation.Pool, jevArmCShowReference), dNoRelax.shown())
		}
	}
	// SC-003's equal-budget variant needs arm C's pool and arm D's shown count
	// together, so it is derived once per question rather than per row.
	if cObservation, ok := in.Observations[jevArmC]; ok {
		if dObservation, ok := in.Observations[jevArmD]; ok {
			derived.DShown = len(dObservation.shown())
			if budget, gradeable := measureJevArmCRecall(cObservation.Pool, derived.DShown, in.QA, in.ChunkTurns); gradeable {
				derived.BudgetGradeable = true
				derived.CBudgetRecallGate = budget.Gate
				derived.CBudgetRecallReference = budget.Reference
				derived.CBudgetRecallEqual = budget.PerQuestion
			}
		}
	}
	if eObservation, ok := in.Observations[jevArmE]; ok {
		shown := eObservation.shown()
		derived.EShown = len(shown)
		for _, hit := range shown {
			if strings.HasPrefix(hit.ID, "span-") {
				derived.ESpans++
			}
		}
	}
	return rows, derived, nil
}

// jevShortlistPrecision is the fraction of shown entries that carry gold
// evidence for this question: the precision half of SC-003's argument that a
// calibrated filter shows fewer, more relevant entries than rank truncation. An
// empty shortlist scores 0 — the honest reading of "nothing relevant shown" —
// and a question without parseable gold evidence is not gradeable.
func jevShortlistPrecision(qa locomoQA, shown []memory.Result, chunkTurns map[string][]string) (float64, bool) {
	refs := evidenceReferences(qa.Evidence)
	if len(refs) == 0 {
		return 0, false
	}
	if len(shown) == 0 {
		return 0, true
	}
	gold := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		gold[fmt.Sprintf("D%d:%d", ref.Session, ref.Dialog)] = struct{}{}
	}
	relevant := 0
	for _, hit := range shown {
		for _, turnID := range chunkTurns[hit.Name] {
			if _, ok := gold[turnID]; ok {
				relevant++
				break
			}
		}
	}
	return float64(relevant) / float64(len(shown)), true
}

// jevArmOutcomeKey identifies one question on one arm.
type jevArmOutcomeKey struct {
	Conv int
	Q    int
	Arm  jevArm
}

// jevArmMajorityOutcomes collapses the per-repetition rows into one majority
// outcome per question and arm (majority-of-3 canonical, majority-of-1 pilot).
// It fails loudly on an even measured
// repetition count, because majority is undefined then — the same rule
// majorityCorrectness enforces for the rest of the harness.
func jevArmMajorityOutcomes(rows []jevArmQuestionRow) (map[jevArmOutcomeKey]bool, error) {
	grouped := map[jevArmOutcomeKey][]bool{}
	for _, row := range rows {
		if !row.CorrectMeasured {
			continue
		}
		key := jevArmOutcomeKey{Conv: row.Conv, Q: row.Q, Arm: row.Arm}
		grouped[key] = append(grouped[key], row.Correct)
	}
	outcomes := make(map[jevArmOutcomeKey]bool, len(grouped))
	for key, correctness := range grouped {
		majority, err := majorityCorrectness(correctness)
		if err != nil {
			return nil, fmt.Errorf("arm %s conv=%d q=%d: %w", key.Arm, key.Conv, key.Q, err)
		}
		outcomes[key] = majority
	}
	return outcomes, nil
}

// jevArmAggregate is one arm's aggregate measurement over a population.
type jevArmAggregate struct {
	Arm                    jevArm  `json:"arm"`
	Questions              int     `json:"questions"`
	CorrectMeasured        int     `json:"correct_measured"`
	Accuracy               float64 `json:"accuracy"`
	MeanShown              float64 `json:"mean_shown"`
	MeanAnswerInputTokens  float64 `json:"mean_answer_input_tokens"`
	PoolMeasured           int     `json:"pool_measured"`
	MeanPoolRecall         float64 `json:"mean_pool_recall_at_150"`
	Gradeable              int     `json:"gradeable"`
	MeanShownRecall        float64 `json:"mean_shown_recall"`
	MeanShortlistPrecision float64 `json:"mean_shortlist_precision"`
	EmptyInjectionRate     float64 `json:"empty_injection_rate"`
	DegradedRate           float64 `json:"degraded_rate"`
	BudgetStopped          int     `json:"budget_stopped"`
}

// aggregateJevArm aggregates one arm's rows. Rates use each metric's own
// gradeable denominator so a question without gold evidence cannot depress a
// recall figure it was never part of.
func aggregateJevArm(rows []jevArmQuestionRow) jevArmAggregate {
	aggregate := jevArmAggregate{Arm: firstJevArm(rows)}
	if len(rows) == 0 {
		return aggregate
	}
	aggregate.Arm = rows[0].Arm
	var shown, tokens, poolRecall, shownRecall, precision float64
	for _, row := range rows {
		aggregate.Questions++
		shown += float64(row.Shown)
		tokens += float64(row.AnswerInputTokens)
		if row.CorrectMeasured {
			aggregate.CorrectMeasured++
			if row.Correct {
				aggregate.Accuracy++
			}
		}
		if row.PoolMeasured && row.PoolGradeable {
			aggregate.PoolMeasured++
			poolRecall += row.PoolRecall
		}
		if row.Gradeable {
			aggregate.Gradeable++
			shownRecall += row.ShownRecall
		}
		if row.PrecisionGradeable {
			precision += row.ShortlistPrecision
		}
		if row.EmptyInjection {
			aggregate.EmptyInjectionRate++
		}
		if row.Degraded {
			aggregate.DegradedRate++
		}
		if row.BudgetStopped {
			aggregate.BudgetStopped++
		}
	}
	denominator := float64(aggregate.Questions)
	aggregate.MeanShown = shown / denominator
	aggregate.MeanAnswerInputTokens = tokens / denominator
	aggregate.EmptyInjectionRate /= denominator
	aggregate.DegradedRate /= denominator
	if aggregate.CorrectMeasured > 0 {
		aggregate.Accuracy /= float64(aggregate.CorrectMeasured)
	}
	if aggregate.PoolMeasured > 0 {
		aggregate.MeanPoolRecall = poolRecall / float64(aggregate.PoolMeasured)
	}
	if aggregate.Gradeable > 0 {
		aggregate.MeanShownRecall = shownRecall / float64(aggregate.Gradeable)
	}
	if aggregate.Gradeable > 0 {
		aggregate.MeanShortlistPrecision = precision / float64(aggregate.Gradeable)
	}
	return aggregate
}

func firstJevArm(rows []jevArmQuestionRow) jevArm {
	if len(rows) == 0 {
		return ""
	}
	return rows[0].Arm
}

// rowsForArm filters rows down to one arm.
func rowsForArm(rows []jevArmQuestionRow, arm jevArm) []jevArmQuestionRow {
	filtered := make([]jevArmQuestionRow, 0, len(rows))
	for _, row := range rows {
		if row.Arm == arm {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// jevFilteredRows returns the rows of both filtered arms (D and D-noRelax):
// SC-007's segmented accounting covers every row that can carry a filter call, so
// the per-call columns add up to the call journal instead of to one arm's share
// of it.
func jevFilteredRows(rows []jevArmQuestionRow) []jevArmQuestionRow {
	filtered := make([]jevArmQuestionRow, 0, len(rows))
	for _, row := range rows {
		if row.Arm == jevArmD || row.Arm == jevArmDNoRelax {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// rowsForBlock filters rows down to one population block.
func rowsForBlock(rows []jevArmQuestionRow, block string) []jevArmQuestionRow {
	filtered := make([]jevArmQuestionRow, 0, len(rows))
	for _, row := range rows {
		if row.Block == block {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// jevArmBudgetRecall reports arm C's recall@shown under the three truncation
// budgets SC-003 asks for. Gate is the C@12 gate comparison, Reference is the
// production C@8 reference, and PerQuestion truncates C's pool to the D arm's own
// shown count for that question — the equal-budget comparison that keeps the
// metric from rewarding whichever arm happens to show more.
type jevArmBudgetRecall struct {
	Gate        float64 `json:"c_at_12"`
	Reference   float64 `json:"c_at_8"`
	PerQuestion float64 `json:"c_at_d_shown"`
}

// measureJevArmCRecall truncates arm C's pool snapshot three ways and grades each
// truncation with the shared gold ruler. It returns the three recalls and whether
// the question is gradeable at all.
func measureJevArmCRecall(cPool []memory.Result, dShown int, qa locomoQA, chunkTurns map[string][]string) (jevArmBudgetRecall, bool) {
	var budget jevArmBudgetRecall
	if len(cPool) == 0 {
		return budget, false
	}
	gate := truncateResults(cPool, jevArmCShowGate)
	reference := truncateResults(cPool, jevArmCShowReference)
	equal := truncateResults(cPool, dShown)
	gateRecall, _, gateGradeable := evidenceRecallAt(qa, gate, chunkTurns)
	referenceRecall, _, _ := evidenceRecallAt(qa, reference, chunkTurns)
	equalRecall, _, _ := evidenceRecallAt(qa, equal, chunkTurns)
	if !gateGradeable {
		return budget, false
	}
	return jevArmBudgetRecall{Gate: gateRecall, Reference: referenceRecall, PerQuestion: equalRecall}, true
}

func truncateResults(hits []memory.Result, limit int) []memory.Result {
	if limit <= 0 || len(hits) <= limit {
		return hits
	}
	return hits[:limit]
}

// jevArmQuestionRef identifies one question inside a flip list.
type jevArmQuestionRef struct {
	Conv       int    `json:"conv"`
	Q          int    `json:"q"`
	Category   int    `json:"category"`
	QuestionID string `json:"question_id,omitempty"`
}

// jevArmFlips lists the questions each contrast flipped, in both directions.
// Reporting only the net delta would hide the two-way churn §23 forbids.
type jevArmFlips struct {
	BCorrectDWrong []jevArmQuestionRef `json:"b_correct_d_wrong"`
	DCorrectBWrong []jevArmQuestionRef `json:"d_correct_b_wrong"`
}

// measureJevArmFlips pairs the B and D rows per question on their declared-reps
// majorities. Questions whose answer stage did not run are excluded, so the lists
// contain only measured flips.
func measureJevArmFlips(rows []jevArmQuestionRow) (jevArmFlips, error) {
	majority, err := jevArmMajorityOutcomes(rows)
	if err != nil {
		return jevArmFlips{}, err
	}
	type key struct{ conv, q int }
	refsByKey := map[key]jevArmQuestionRef{}
	bByKey := map[key]bool{}
	dByKey := map[key]bool{}
	for outcomeKey, correct := range majority {
		flipKey := key{conv: outcomeKey.Conv, q: outcomeKey.Q}
		if _, ok := refsByKey[flipKey]; !ok {
			refsByKey[flipKey] = jevArmQuestionRef{
				Conv:       outcomeKey.Conv,
				Q:          outcomeKey.Q,
				Category:   jevArmCategoryForRef(rows, outcomeKey),
				QuestionID: jevArmQuestionIDForRef(rows, outcomeKey),
			}
		}
		switch outcomeKey.Arm {
		case jevArmB:
			bByKey[flipKey] = correct
		case jevArmD:
			dByKey[flipKey] = correct
		}
	}
	flips := jevArmFlips{}
	for flipKey, bCorrect := range bByKey {
		dCorrect, ok := dByKey[flipKey]
		if !ok {
			continue
		}
		ref := refsByKey[flipKey]
		switch {
		case bCorrect && !dCorrect:
			flips.BCorrectDWrong = append(flips.BCorrectDWrong, ref)
		case !bCorrect && dCorrect:
			flips.DCorrectBWrong = append(flips.DCorrectBWrong, ref)
		}
	}
	sortJevArmRefs(flips.BCorrectDWrong)
	sortJevArmRefs(flips.DCorrectBWrong)
	return flips, nil
}

// jevArmQuestionIDForRef and jevArmCategoryForRef recover a question's dataset ID
// and category from any of its rows, so a flip list names the question the way the
// dataset does.
func jevArmQuestionIDForRef(rows []jevArmQuestionRow, key jevArmOutcomeKey) string {
	for _, row := range rows {
		if row.Conv == key.Conv && row.Q == key.Q {
			return row.QuestionID
		}
	}
	return ""
}

func jevArmCategoryForRef(rows []jevArmQuestionRow, key jevArmOutcomeKey) int {
	for _, row := range rows {
		if row.Conv == key.Conv && row.Q == key.Q {
			return row.Category
		}
	}
	return 0
}

func sortJevArmRefs(refs []jevArmQuestionRef) {
	sort.Slice(refs, func(left, right int) bool {
		if refs[left].Conv != refs[right].Conv {
			return refs[left].Conv < refs[right].Conv
		}
		return refs[left].Q < refs[right].Q
	})
}

// jevArmSegments is SC-007's segmented accounting: wide-pool retrieval, Jev, and
// answering each get their own latency distribution and their own money/token
// columns, so a cost anomaly can be attributed to one segment instead of the run.
type jevArmSegments struct {
	RetrievalMsP50     float64 `json:"retrieval_ms_p50"`
	RetrievalMsP95     float64 `json:"retrieval_ms_p95"`
	JevMsP50           float64 `json:"jev_ms_p50"`
	JevMsP95           float64 `json:"jev_ms_p95"`
	AnswerMsP50        float64 `json:"answer_ms_p50"`
	AnswerMsP95        float64 `json:"answer_ms_p95"`
	JevCalls           int     `json:"jev_calls"`
	JevUSD             float64 `json:"jev_usd"`
	JevUSDPerCall      float64 `json:"jev_usd_per_call"`
	WidePoolCandidates int     `json:"wide_pool_candidates"`
	AnswerInTokens     int     `json:"answer_in_tokens"`
	AnswerOutTokens    int     `json:"answer_out_tokens"`
	AnswerUSD          float64 `json:"answer_usd"`
}

// measureJevArmSegments aggregates the segmented accounting. JevCalls counts the
// rows that reported filter latency: a degraded arm issues no filter call, so it
// contributes nothing to the per-call cost.
func measureJevArmSegments(rows []jevArmQuestionRow) jevArmSegments {
	var retrieval, jev, answer []int
	segments := jevArmSegments{}
	for _, row := range rows {
		retrieval = append(retrieval, row.RetrievalMs)
		answer = append(answer, row.AnswerMs)
		if row.JevMs > 0 || row.JevUSD > 0 {
			jev = append(jev, row.JevMs)
			segments.JevCalls++
			segments.JevUSD += row.JevUSD
		}
		segments.WidePoolCandidates += row.PoolSize
		segments.AnswerInTokens += row.AnswerInTokens
		segments.AnswerOutTokens += row.AnswerOutTokens
		segments.AnswerUSD += row.AnswerUSD
	}
	segments.RetrievalMsP50 = percentile(retrieval, 50)
	segments.RetrievalMsP95 = percentile(retrieval, 95)
	segments.JevMsP50 = percentile(jev, 50)
	segments.JevMsP95 = percentile(jev, 95)
	segments.AnswerMsP50 = percentile(answer, 50)
	segments.AnswerMsP95 = percentile(answer, 95)
	if segments.JevCalls > 0 {
		segments.JevUSDPerCall = segments.JevUSD / float64(segments.JevCalls)
	}
	return segments
}

// percentile returns the nearest-rank percentile of values. It is descriptive:
// SC-007 asks for p50/p95 ordering, not for an interpolated estimate.
func percentile(values []int, percent float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	if percent <= 0 {
		return float64(sorted[0])
	}
	if percent >= 100 {
		return float64(sorted[len(sorted)-1])
	}
	rank := int((percent / 100) * float64(len(sorted)))
	if rank < len(sorted) {
		// ceil: the first index whose cumulative share reaches percent
		if float64(rank)/float64(len(sorted))*100 < percent {
			rank++
		}
	}
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return float64(sorted[rank-1])
}

// jevArmShownSetMatches reports whether two arms show the same evidence in the
// same order. SC-005 compares the degraded Jev path against arm A / C@8, and a
// same-set check is what makes that accuracy comparison an
// evidence-identical one rather than a hopeful one.
func jevArmShownSetMatches(left, right []memory.Result) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name {
			return false
		}
	}
	return true
}
