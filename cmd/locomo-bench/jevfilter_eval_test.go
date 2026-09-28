package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/store"
)

// This file tests the four-arm recipes, the filter-aware retrieval seam, the
// packer admission that defines "shown", and the metric layer — all offline.

// jevTestCounter counts one token per rendered candidate name plus a fixed prompt
// overhead, so a test can predict admission exactly.
type jevTestCounter struct{ overhead int }

func (c jevTestCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	count := c.overhead
	if trimmed := strings.TrimSpace(input.User); trimmed != "" {
		count += len(strings.Fields(trimmed))
	}
	return evidencecompiler.TokenCount{InputTokens: count, Fingerprint: "test-counter"}, nil
}

// jevTestRenderer renders one candidate name per token, nothing else.
func jevTestRenderer(hits []memory.Result) evidencecompiler.AnswerInput {
	names := make([]string, 0, len(hits))
	for _, hit := range hits {
		names = append(names, hit.Name)
	}
	return evidencecompiler.AnswerInput{User: strings.Join(names, " ")}
}

// jevTestFilter is a deterministic RelevanceFilter: one probability per candidate
// name, with a call counter so a test can prove the unfiltered arms never call it.
type jevTestFilter struct {
	probs       map[string]float64
	defaultProb float64
	err         error
	calls       int
	lastPool    int
}

func (f *jevTestFilter) Filter(_ context.Context, _ string, cands []filter.Candidate) ([]float64, filter.FilterMeta, error) {
	f.calls++
	f.lastPool = len(cands)
	if f.err != nil {
		return nil, filter.FilterMeta{Degraded: true}, f.err
	}
	probs := make([]float64, len(cands))
	for index, cand := range cands {
		prob, ok := f.probs[cand.Name]
		if !ok {
			prob = f.defaultProb
		}
		probs[index] = prob
	}
	return probs, filter.FilterMeta{Backend: filter.BackendJev, Theta: filter.DefaultPolicy().Theta}, nil
}

// jevTestRetriever builds an in-memory retriever over the named entries.
func jevTestRetriever(t *testing.T, names ...string) *memory.Retriever {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	for _, name := range names {
		content := fmt.Sprintf("%s field note about the alpha topic", name)
		if err := entries.Upsert(ctx, &memory.Entry{Name: name, Content: content, CharCount: len(content)}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}
	return memory.NewRetriever(entries, vectors, nil)
}

// jevTestSpec is the run's filter retrieval payload for a test.
func jevTestSpec(flt filter.RelevanceFilter, policy filter.Policy) *filterRetrieval {
	return &filterRetrieval{Pool: jevArmPoolSize, Show: jevArmCShowReference, Filter: flt, Policy: policy}
}

func jevObservation(arm jevArm, shown, pool []string, tokens int) jevArmObservation {
	observation := jevArmObservation{Arm: arm, Pool: nil}
	for _, name := range shown {
		observation.Packed.Admitted = append(observation.Packed.Admitted, memory.Result{Name: name})
	}
	observation.Packed.Tokens = tokens
	for _, name := range pool {
		observation.Pool = append(observation.Pool, memory.Result{Name: name})
	}
	observation.Presented = append([]memory.Result(nil), observation.Packed.Admitted...)
	return observation
}

func TestJevArmRetrieveEconomisesUnlistedFilterArms(t *testing.T) {
	ctx := context.Background()
	retriever := jevTestRetriever(t, "keep-me", "drop-me")
	stub := &jevTestFilter{probs: map[string]float64{"keep-me": 0.9, "drop-me": 0.1}, defaultProb: 0.1}
	recipe, err := jevArmRecipeFor(jevArmDNoRelax)
	if err != nil {
		t.Fatal(err)
	}
	spec := jevTestSpec(stub, filter.DefaultPolicy())
	// D-noRelax outside --jev-filter-arms D,E: the stub must never be called and
	// the observation is flagged degraded (the no-filter composition).
	observation, err := jevArmRetrieve(ctx, retriever, "alpha", recipe, spec, 0, 8, nil, []jevArm{jevArmD, jevArmE})
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Degraded {
		t.Error("an economised filtered arm was not flagged degraded")
	}
	if stub.calls != 0 {
		t.Errorf("economised arm reached the filter: %d stub calls", stub.calls)
	}
	// The same arm inside the subset filters normally.
	observation, err = jevArmRetrieve(ctx, retriever, "alpha", recipe, spec, 0, 8, nil, []jevArm{jevArmDNoRelax})
	if err != nil {
		t.Fatal(err)
	}
	if observation.Degraded {
		t.Error("a listed arm ran degraded")
	}
	if stub.calls != 1 {
		t.Errorf("a listed arm made %d filter calls, want 1", stub.calls)
	}
}

func TestJevArmRecipesAndArmSet(t *testing.T) {
	recipes := jevArmRecipes()
	if len(recipes) != 6 {
		t.Fatalf("got %d recipes, want the four arms plus the D-noRelax variant and the E span-recovery arm", len(recipes))
	}
	want := map[jevArm]jevArmRecipe{
		jevArmA:        {Pool: 0, Presented: jevArmCShowReference},
		jevArmB:        {Pool: jevArmPoolSize, Presented: 0},
		jevArmC:        {Pool: jevArmPoolSize, Presented: jevArmCShowGate},
		jevArmD:        {Pool: jevArmPoolSize, Filtered: true},
		jevArmDNoRelax: {Pool: jevArmPoolSize, Filtered: true, RelaxDisabled: true},
		jevArmE:        {Pool: jevArmPoolSize, Filtered: true, SpanRecovery: true},
	}
	for _, recipe := range recipes {
		expected, ok := want[recipe.Arm]
		if !ok {
			t.Fatalf("unexpected arm %q", recipe.Arm)
		}
		if recipe.Pool != expected.Pool || recipe.Presented != expected.Presented || recipe.Filtered != expected.Filtered || recipe.RelaxDisabled != expected.RelaxDisabled || recipe.SpanRecovery != expected.SpanRecovery {
			t.Errorf("arm %s recipe = %+v, want pool=%d presented=%d filtered=%t relax_disabled=%t span_recovery=%t", recipe.Arm, recipe, expected.Pool, expected.Presented, expected.Filtered, expected.RelaxDisabled, expected.SpanRecovery)
		}
	}
	if names := armNamesInOrder(); strings.Join(names, ",") != "A,B,C,D,D-noRelax,E" {
		t.Errorf("arm order = %v", names)
	}
	if _, err := jevArmRecipeFor("Z"); err == nil {
		t.Error("unknown arm did not error")
	}
	if err := validateJevArmSet(armNamesInOrder()); err != nil {
		t.Errorf("frozen arm set rejected: %v", err)
	}
	if err := validateJevArmSet([]string{"A", "B"}); err == nil {
		t.Error("a partial arm set was accepted")
	}
}

func TestJevArmTopKSplit(t *testing.T) {
	opt := options{topK: 8}
	cases := []struct {
		arm   jevArm
		poolK int
		showK int
	}{
		{jevArmA, 0, 8},
		{jevArmB, jevArmPoolSize, 0},
		{jevArmC, jevArmPoolSize, jevArmCShowGate},
		{jevArmD, jevArmPoolSize, 0},
	}
	for _, testCase := range cases {
		recipe, err := jevArmRecipeFor(testCase.arm)
		if err != nil {
			t.Fatal(err)
		}
		poolK, showK := jevArmTopKSplit(opt, recipe)
		if poolK != testCase.poolK || showK != testCase.showK {
			t.Errorf("arm %s split = (%d,%d), want (%d,%d)", testCase.arm, poolK, showK, testCase.poolK, testCase.showK)
		}
	}
	// Arm A falls back to memory.Search's own default when --top-k is unset.
	recipeA, _ := jevArmRecipeFor(jevArmA)
	if _, showK := jevArmTopKSplit(options{}, recipeA); showK != jevArmCShowReference {
		t.Errorf("arm A default show budget = %d, want %d", showK, jevArmCShowReference)
	}
	// A custom --top-k bounds arm A only.
	if _, showK := jevArmTopKSplit(options{topK: 30}, recipeA); showK != 30 {
		t.Errorf("arm A show budget = %d, want the configured 30", showK)
	}
}

func TestJevArmPolicyRelaxKillSwitch(t *testing.T) {
	base := filter.DefaultPolicy()
	plain, _ := jevArmRecipeFor(jevArmD)
	noRelax, _ := jevArmRecipeFor(jevArmDNoRelax)
	if jevArmPolicy(plain, base).RelaxDisabled {
		t.Error("arm D disabled the relax stage")
	}
	if !jevArmPolicy(noRelax, base).RelaxDisabled {
		t.Error("arm D-noRelax did not disable the relax stage")
	}
	if got := jevArmPolicy(plain, filter.Policy{}); got.Theta != base.Theta {
		t.Errorf("zero policy did not normalize to the default: %+v", got)
	}
}

func TestPackJevArmAnswerInputStopsAtCap(t *testing.T) {
	hits := []memory.Result{{Name: "one"}, {Name: "two"}, {Name: "three"}}
	counter := jevTestCounter{overhead: 4} // tokens = 4 + number of admitted names
	packed, err := packJevArmAnswerInput(context.Background(), counter, 6, jevTestRenderer, hits)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed.Admitted) != 2 || packed.Tokens != 6 {
		t.Errorf("admitted %d entries with %d tokens, want 2 entries at the cap of 6", len(packed.Admitted), packed.Tokens)
	}
	if !packed.BudgetStop {
		t.Error("packer did not record that the cap ended admission")
	}
	// A cap that fits everything leaves BudgetStop false.
	packed, err = packJevArmAnswerInput(context.Background(), counter, 16, jevTestRenderer, hits)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed.Admitted) != 3 || packed.BudgetStop {
		t.Errorf("admitted %d entries with budget stop %t, want all 3 without a stop", len(packed.Admitted), packed.BudgetStop)
	}
	// The static prompt must already fit.
	if _, err := packJevArmAnswerInput(context.Background(), jevTestCounter{overhead: 9}, 8, jevTestRenderer, hits); err == nil {
		t.Error("an over-cap static prompt was accepted")
	}
	if _, err := packJevArmAnswerInput(context.Background(), nil, 8, jevTestRenderer, hits); err == nil {
		t.Error("a nil counter was accepted")
	}
	if _, err := packJevArmAnswerInput(context.Background(), counter, 0, jevTestRenderer, hits); err == nil {
		t.Error("a zero cap was accepted")
	}
}

func TestAssertJevArmBUntruncatedFailsClosed(t *testing.T) {
	if err := assertJevArmBUntruncated(jevArmPacked{Admitted: []memory.Result{{Name: "a"}, {Name: "b"}}, Tokens: 100}, 2, jevArmAnswerInputCap); err != nil {
		t.Errorf("an untruncated arm B was rejected: %v", err)
	}
	if err := assertJevArmBUntruncated(jevArmPacked{Admitted: []memory.Result{{Name: "a"}}, Tokens: 100, BudgetStop: true}, 2, jevArmAnswerInputCap); err == nil {
		t.Error("a budget-stopped arm B was accepted")
	}
	if err := assertJevArmBUntruncated(jevArmPacked{Admitted: []memory.Result{{Name: "a"}}, Tokens: 100}, 2, jevArmAnswerInputCap); err == nil {
		t.Error("a short admission without a budget stop was accepted")
	}
	if err := assertJevArmBUntruncated(jevArmPacked{Admitted: []memory.Result{{Name: "a"}}, Tokens: jevArmAnswerInputCap}, 1, jevArmAnswerInputCap); err == nil {
		t.Error("a bundle at the cap was accepted")
	}
}

func TestJevArmRetrieveArmAIsTheProductionSeam(t *testing.T) {
	ctx := context.Background()
	retriever := jevTestRetriever(t, "one", "two", "three")
	recipeA, _ := jevArmRecipeFor(jevArmA)
	observation, err := jevArmRetrieve(ctx, retriever, "alpha", recipeA, nil, 0, 8, nil, nil)
	if err != nil {
		t.Fatalf("arm A retrieve: %v", err)
	}
	plain, err := retriever.Search(ctx, "alpha", 8)
	if err != nil {
		t.Fatalf("plain search: %v", err)
	}
	if !jevArmShownSetMatches(observation.Presented, plain) {
		t.Errorf("arm A presented %d hits that differ from the production seam's %d", len(observation.Presented), len(plain))
	}
	if observation.Pool != nil {
		t.Error("arm A reported a pool, but it has none of its own")
	}
}

func TestJevArmRetrieveFilteredArmUsesTheFilter(t *testing.T) {
	ctx := context.Background()
	retriever := jevTestRetriever(t, "keep-me", "drop-me")
	stub := &jevTestFilter{probs: map[string]float64{"keep-me": 0.9, "drop-me": 0.1}, defaultProb: 0.1}
	recipeD, _ := jevArmRecipeFor(jevArmD)
	observation, err := jevArmRetrieve(ctx, retriever, "alpha", recipeD, jevTestSpec(stub, filter.DefaultPolicy()), 0, 8, nil, nil)
	if err != nil {
		t.Fatalf("arm D retrieve: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("filter was called %d times, want 1", stub.calls)
	}
	if stub.lastPool != 2 {
		t.Errorf("filter saw a pool of %d, want the 2 candidates", stub.lastPool)
	}
	names := jevResultNames(observation.Presented)
	if len(names) != 1 || names[0] != "keep-me" {
		t.Errorf("arm D shortlist = %v, want only the above-threshold entry", names)
	}
	if observation.Degraded {
		t.Error("a successful filter was marked degraded")
	}
	if observation.Meta.Backend != filter.BackendJev {
		t.Errorf("filter telemetry backend = %q", observation.Meta.Backend)
	}
	if len(observation.Pool) != 2 {
		t.Errorf("arm D pool snapshot = %d entries, want the wide pool for pool recall", len(observation.Pool))
	}

	// The unfiltered arms must never call the filter.
	for _, arm := range []jevArm{jevArmB, jevArmC} {
		recipe, _ := jevArmRecipeFor(arm)
		if _, err := jevArmRetrieve(ctx, retriever, "alpha", recipe, jevTestSpec(stub, filter.DefaultPolicy()), 0, 8, nil, nil); err != nil {
			t.Fatalf("arm %s retrieve: %v", arm, err)
		}
	}
	if stub.calls != 1 {
		t.Errorf("the unfiltered arms called the filter (%d calls total)", stub.calls)
	}
}

func TestJevArmRetrieveDegradedArmFallsBackToTheNoFilterSet(t *testing.T) {
	ctx := context.Background()
	retriever := jevTestRetriever(t, "one", "two", "three")
	recipeA, _ := jevArmRecipeFor(jevArmA)
	armA, err := jevArmRetrieve(ctx, retriever, "alpha", recipeA, nil, 0, 8, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	recipeD, _ := jevArmRecipeFor(jevArmD)
	degraded, err := jevArmRetrieve(ctx, retriever, "alpha", recipeD, jevTestSpec(nil, filter.DefaultPolicy()), 0, 8, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !degraded.Degraded {
		t.Error("an unconfigured filter was not reported as degraded")
	}
	// The degraded path must produce the same evidence as the production baseline
	// (SC-005), and only SearchFiltered may produce it.
	packer := func(observation jevArmObservation) []memory.Result {
		packed, packErr := packJevArmAnswerInput(ctx, jevTestCounter{overhead: 1}, jevArmAnswerInputCap, jevTestRenderer, observation.Presented)
		if packErr != nil {
			t.Fatalf("pack: %v", packErr)
		}
		return packed.Admitted
	}
	if !jevArmShownSetMatches(packer(armA), packer(degraded)) {
		t.Errorf("degraded shown set %v != arm A shown set %v", jevResultNames(packer(degraded)), jevResultNames(packer(armA)))
	}

	// A failing filter must degrade the whole call, never adopt a partial list.
	stub := &jevTestFilter{err: fmt.Errorf("endpoint down")}
	failing, err := jevArmRetrieve(ctx, retriever, "alpha", recipeD, jevTestSpec(stub, filter.DefaultPolicy()), 0, 8, nil, nil)
	if err != nil {
		t.Fatalf("a failing filter must not error the arm: %v", err)
	}
	if !failing.Degraded {
		t.Error("a failing filter did not mark the arm degraded")
	}
	if !jevArmShownSetMatches(packer(armA), packer(failing)) {
		t.Errorf("failed-filter shown set %v != arm A shown set %v", jevResultNames(packer(failing)), jevResultNames(packer(armA)))
	}
}

func jevResultNames(hits []memory.Result) []string {
	names := make([]string, 0, len(hits))
	for _, hit := range hits {
		names = append(names, hit.Name)
	}
	return names
}

func TestMeasureJevArmQuestionPopulatesMetrics(t *testing.T) {
	qa := locomoQA{Question: "where did they move?", Category: 1, Evidence: []string{"D1:2"}, QuestionID: "q-1"}
	chunkTurns := map[string][]string{"gold": {"D1:2"}, "noise": {"D1:9"}}
	observations := map[jevArm]jevArmObservation{
		jevArmA:        jevObservation(jevArmA, []string{"gold"}, nil, 10),
		jevArmB:        jevObservation(jevArmB, []string{"gold", "noise"}, []string{"gold", "noise"}, 200),
		jevArmC:        jevObservation(jevArmC, []string{"gold", "noise"}, []string{"noise", "gold", "noise"}, 20),
		jevArmD:        jevObservation(jevArmD, []string{"gold"}, []string{"gold", "noise"}, 10),
		jevArmDNoRelax: jevObservation(jevArmDNoRelax, []string{"gold", "noise"}, []string{"gold", "noise"}, 10),
		jevArmE:        jevObservation(jevArmE, []string{"gold"}, []string{"gold", "noise"}, 10),
	}
	outcomes := map[jevArm]jevArmOutcome{}
	for _, arm := range jevArmNames() {
		outcomes[arm] = jevArmOutcome{Measured: true, Correct: arm == jevArmD}
	}
	rows, derived, err := measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 1, Category: 1, QuestionID: "q-1", Repetition: 0,
		QA: qa, ChunkTurns: chunkTurns, Observations: observations, Outcomes: outcomes,
	})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want one per arm", len(rows))
	}
	byArm := map[jevArm]jevArmQuestionRow{}
	for _, row := range rows {
		byArm[row.Arm] = row
		if row.Block != jevArmMainBlock {
			t.Errorf("arm %s block = %q, want main", row.Arm, row.Block)
		}
	}
	if row := byArm[jevArmB]; row.Shown != 2 || row.AnswerInputTokens != 200 || !row.PoolMeasured || row.PoolRecall != 1 {
		t.Errorf("arm B row = %+v, want 2 shown / 200 tokens / full pool recall", row)
	}
	if row := byArm[jevArmD]; row.Shown != 1 || row.ShownRecall != 1 || row.ShortlistPrecision != 1 || !row.Correct {
		t.Errorf("arm D row = %+v, want 1 shown with full recall and precision 1", row)
	}
	if row := byArm[jevArmD]; row.EmptyInjection {
		t.Error("a non-empty shortlist was flagged as an empty injection")
	}
	if row := byArm[jevArmA]; row.PoolMeasured {
		t.Error("arm A reported a pool measurement")
	}
	if !derived.BudgetGradeable {
		t.Fatal("arm C's budget recalls were not gradeable")
	}
	if derived.DShown != 1 {
		t.Errorf("derived D shown = %d, want 1", derived.DShown)
	}
	if derived.CBudgetRecallGate != 1 {
		t.Errorf("C@12 recall = %v, want 1 (gold is second in the pool)", derived.CBudgetRecallGate)
	}
	if derived.CBudgetRecallEqual != 0 {
		t.Errorf("C@D recall = %v, want 0 (the equal budget cuts to the first noise entry)", derived.CBudgetRecallEqual)
	}
	if derived.Block != jevArmMainBlock {
		t.Errorf("derived block = %q", derived.Block)
	}
	if !derived.DegradedSetComparable {
		t.Error("the comparability flag was not set even though arm A and arm D-noRelax both ran")
	}
	if derived.DegradedShownMatchesA {
		t.Error("different shown sets were reported as matching")
	}

	// A question whose gold evidence is unparseable is not gradeable.
	rows, _, err = measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 2, Category: 5, QA: locomoQA{Question: "x", Category: 5},
		Observations: observations, Outcomes: outcomes,
	})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if rows[0].Gradeable {
		t.Error("a question without gold evidence was marked gradeable")
	}
	if rows[0].Block != jevArmCategoryFiveBlock {
		t.Errorf("category 5 block = %q, want %q", rows[0].Block, jevArmCategoryFiveBlock)
	}

	// A missing arm observation is an error, not a smaller denominator.
	if _, _, err := measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 3, QA: qa, Observations: map[jevArm]jevArmObservation{jevArmA: observations[jevArmA]},
	}); err == nil {
		t.Error("a partially measured question was accepted")
	}
}

func TestMeasureJevArmQuestionMeasuresDegradedComparability(t *testing.T) {
	qa := locomoQA{Question: "q", Category: 1, Evidence: []string{"D1:1"}}
	shown := []string{"one", "two"}
	observations := map[jevArm]jevArmObservation{
		jevArmA:        jevObservation(jevArmA, shown, nil, 10),
		jevArmB:        jevObservation(jevArmB, shown, shown, 100),
		jevArmC:        jevObservation(jevArmC, shown, shown, 20),
		jevArmD:        jevObservation(jevArmD, shown, shown, 10),
		jevArmDNoRelax: jevObservation(jevArmDNoRelax, []string{"one"}, shown, 10),
		jevArmE:        jevObservation(jevArmE, shown, shown, 10),
	}
	_, derived, err := measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 1, Category: 1, QA: qa,
		ChunkTurns:   map[string][]string{"one": {"D1:1"}},
		Observations: observations,
		Outcomes:     map[jevArm]jevArmOutcome{},
	})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if !derived.DegradedSetComparable {
		t.Fatal("comparability was not detected with both arm A and arm D-noRelax present")
	}
	if derived.DegradedShownMatchesA {
		t.Error("mismatched shown sets were reported as matching")
	}
	observations[jevArmDNoRelax] = jevObservation(jevArmDNoRelax, shown, shown, 10)
	_, derived, err = measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 1, Category: 1, QA: qa, ChunkTurns: map[string][]string{"one": {"D1:1"}},
		Observations: observations, Outcomes: map[jevArm]jevArmOutcome{},
	})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if !derived.DegradedShownMatchesA {
		t.Error("identical shown sets were not reported as matching")
	}
}

func TestJevShortlistPrecision(t *testing.T) {
	qa := locomoQA{Evidence: []string{"D1:2"}}
	chunkTurns := map[string][]string{"gold": {"D1:2"}, "noise": {"D1:3"}}
	precision, gradeable := jevShortlistPrecision(qa, []memory.Result{{Name: "gold"}, {Name: "noise"}}, chunkTurns)
	if !gradeable || precision != 0.5 {
		t.Errorf("precision = %v (gradeable=%t), want 0.5", precision, gradeable)
	}
	precision, gradeable = jevShortlistPrecision(qa, nil, chunkTurns)
	if !gradeable || precision != 0 {
		t.Errorf("empty shortlist precision = %v (gradeable=%t), want 0/true", precision, gradeable)
	}
	if _, gradeable := jevShortlistPrecision(locomoQA{}, []memory.Result{{Name: "gold"}}, chunkTurns); gradeable {
		t.Error("a question without gold evidence was gradeable for precision")
	}
}

func TestAggregateJevArmUsesPerMetricDenominators(t *testing.T) {
	rows := []jevArmQuestionRow{
		{Arm: jevArmD, Block: jevArmMainBlock, Shown: 2, AnswerInputTokens: 100, Gradeable: true, ShownRecall: 1, PoolMeasured: true, PoolGradeable: true, PoolRecall: 1, PrecisionGradeable: true, ShortlistPrecision: 1, CorrectMeasured: true, Correct: true, Degraded: true},
		{Arm: jevArmD, Block: jevArmMainBlock, Shown: 4, AnswerInputTokens: 300, Gradeable: false, PoolMeasured: true, PoolGradeable: false, CorrectMeasured: true, Correct: false, EmptyInjection: true},
	}
	aggregate := aggregateJevArm(rows)
	if aggregate.Questions != 2 || aggregate.CorrectMeasured != 2 {
		t.Fatalf("aggregate = %+v", aggregate)
	}
	if aggregate.Accuracy != 0.5 {
		t.Errorf("accuracy = %v, want 0.5 over the measured rows", aggregate.Accuracy)
	}
	if aggregate.MeanShown != 3 || aggregate.MeanAnswerInputTokens != 200 {
		t.Errorf("means = shown %v tokens %v, want 3 and 200", aggregate.MeanShown, aggregate.MeanAnswerInputTokens)
	}
	if aggregate.Gradeable != 1 || aggregate.MeanShownRecall != 1 {
		t.Errorf("recall = %v over %d gradeable, want 1 over 1", aggregate.MeanShownRecall, aggregate.Gradeable)
	}
	if aggregate.PoolMeasured != 1 || aggregate.MeanPoolRecall != 1 {
		t.Errorf("pool recall = %v over %d, want 1 over 1", aggregate.MeanPoolRecall, aggregate.PoolMeasured)
	}
	if aggregate.EmptyInjectionRate != 0.5 || aggregate.DegradedRate != 0.5 {
		t.Errorf("rates = empty %v degraded %v, want 0.5 each", aggregate.EmptyInjectionRate, aggregate.DegradedRate)
	}
	if aggregate.MeanShortlistPrecision != 1 {
		t.Errorf("precision = %v, want 1 over the single graded row", aggregate.MeanShortlistPrecision)
	}
	if empty := aggregateJevArm(nil); empty.Arm != "" || empty.Questions != 0 {
		t.Errorf("empty aggregate = %+v", empty)
	}
}

func TestJevArmMajorityOutcomes(t *testing.T) {
	rows := []jevArmQuestionRow{
		{Conv: 1, Q: 1, Arm: jevArmD, CorrectMeasured: true, Correct: true},
		{Conv: 1, Q: 1, Arm: jevArmD, CorrectMeasured: true, Correct: true},
		{Conv: 1, Q: 1, Arm: jevArmD, CorrectMeasured: true, Correct: false},
		{Conv: 1, Q: 1, Arm: jevArmB, CorrectMeasured: true, Correct: false},
		{Conv: 1, Q: 1, Arm: jevArmB, CorrectMeasured: true, Correct: false},
		{Conv: 1, Q: 1, Arm: jevArmB, CorrectMeasured: true, Correct: true},
		{Conv: 1, Q: 2, Arm: jevArmD, CorrectMeasured: false},
	}
	majority, err := jevArmMajorityOutcomes(rows)
	if err != nil {
		t.Fatalf("majority: %v", err)
	}
	if len(majority) != 2 {
		t.Fatalf("got %d majority outcomes, want one per measured question/arm", len(majority))
	}
	if !majority[jevArmOutcomeKey{Conv: 1, Q: 1, Arm: jevArmD}] {
		t.Error("arm D did not win its 2-of-3 majority")
	}
	if majority[jevArmOutcomeKey{Conv: 1, Q: 1, Arm: jevArmB}] {
		t.Error("arm B lost its 2-of-3 majority")
	}
	// An odd count is fine (one repetition is its own majority); an even count is
	// undefined and must fail loudly.
	if majority, err := jevArmMajorityOutcomes([]jevArmQuestionRow{{Arm: jevArmD, CorrectMeasured: true, Correct: true}}); err != nil || !majority[jevArmOutcomeKey{Arm: jevArmD}] {
		t.Errorf("a single measured repetition should be its own majority (err=%v)", err)
	}
	if _, err := jevArmMajorityOutcomes([]jevArmQuestionRow{
		{Arm: jevArmD, CorrectMeasured: true, Correct: true},
		{Arm: jevArmD, CorrectMeasured: true, Correct: false},
	}); err == nil {
		t.Error("an even repetition count was accepted, but majority is undefined then")
	}
}

func TestMeasureJevArmFlipsBothDirections(t *testing.T) {
	var rows []jevArmQuestionRow
	add := func(conv, q int, arm jevArm, correct bool) {
		rows = append(rows, jevArmQuestionRow{Conv: conv, Q: q, Category: 2, QuestionID: fmt.Sprintf("q-%d", q), Arm: arm, CorrectMeasured: true, Correct: correct})
	}
	add(1, 1, jevArmB, true)
	add(1, 1, jevArmD, false) // B right, D wrong
	add(1, 2, jevArmB, false)
	add(1, 2, jevArmD, true) // D right, B wrong
	add(1, 3, jevArmB, true)
	add(1, 3, jevArmD, true)  // unchanged
	add(1, 4, jevArmB, false) // arm D unmeasured: excluded
	flips, err := measureJevArmFlips(rows)
	if err != nil {
		t.Fatalf("flips: %v", err)
	}
	if len(flips.BCorrectDWrong) != 1 || flips.BCorrectDWrong[0].Q != 1 || flips.BCorrectDWrong[0].QuestionID != "q-1" {
		t.Errorf("B-correct/D-wrong flips = %+v", flips.BCorrectDWrong)
	}
	if len(flips.DCorrectBWrong) != 1 || flips.DCorrectBWrong[0].Q != 2 {
		t.Errorf("D-correct/B-wrong flips = %+v", flips.DCorrectBWrong)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	values := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if got := percentile(values, 50); got != 50 {
		t.Errorf("p50 = %v, want 50", got)
	}
	if got := percentile(values, 95); got != 100 {
		t.Errorf("p95 = %v, want 100", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Errorf("empty p95 = %v, want 0", got)
	}
	if got := percentile(values, 100); got != 100 {
		t.Errorf("max = %v, want 100", got)
	}
}

func TestMeasureJevArmSegmentsSeparatesFilterSegment(t *testing.T) {
	rows := []jevArmQuestionRow{
		{Arm: jevArmD, RetrievalMs: 10, JevMs: 300, JevUSD: 0.0004, AnswerMs: 900, AnswerInTokens: 100, AnswerOutTokens: 10, PoolSize: 150},
		{Arm: jevArmD, RetrievalMs: 20, JevMs: 500, JevUSD: 0.0008, AnswerMs: 1100, AnswerInTokens: 200, AnswerOutTokens: 20, PoolSize: 150},
		{Arm: jevArmD, RetrievalMs: 30, JevMs: 0, JevUSD: 0, AnswerMs: 1000, AnswerInTokens: 300, AnswerOutTokens: 30, PoolSize: 150, Degraded: true},
	}
	segments := measureJevArmSegments(rows)
	if segments.JevCalls != 2 {
		t.Errorf("filter calls = %d, want only the rows that reported filter latency", segments.JevCalls)
	}
	if segments.JevMsP50 != 300 || segments.JevMsP95 != 500 {
		t.Errorf("filter latency p50/p95 = %v/%v, want 300/500", segments.JevMsP50, segments.JevMsP95)
	}
	if segments.RetrievalMsP50 != 20 || segments.RetrievalMsP95 != 30 {
		t.Errorf("retrieval latency p50/p95 = %v/%v", segments.RetrievalMsP50, segments.RetrievalMsP95)
	}
	if segments.WidePoolCandidates != 450 || segments.AnswerInTokens != 600 {
		t.Errorf("segments = %+v", segments)
	}
	if diff := segments.JevUSDPerCall - 0.0006; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost per filter call = %v, want 0.0006", segments.JevUSDPerCall)
	}
}

func TestJevArmShownSetMatches(t *testing.T) {
	if !jevArmShownSetMatches(nil, nil) {
		t.Error("two empty sets did not match")
	}
	if jevArmShownSetMatches([]memory.Result{{Name: "a"}}, nil) {
		t.Error("a non-empty set matched an empty one")
	}
	if jevArmShownSetMatches([]memory.Result{{Name: "a"}}, []memory.Result{{Name: "b"}}) {
		t.Error("different names matched")
	}
	if !jevArmShownSetMatches([]memory.Result{{Name: "a"}, {Name: "b"}}, []memory.Result{{Name: "a"}, {Name: "b"}}) {
		t.Error("identical sets did not match")
	}
	if jevArmShownSetMatches([]memory.Result{{Name: "a"}, {Name: "b"}}, []memory.Result{{Name: "b"}, {Name: "a"}}) {
		t.Error("reordered sets matched; the order is part of the shown evidence")
	}
}

// jevSlowEmbedder is a vector client whose only job is to burn time: the semantic
// signal degrades (it returns no vectors) while the retrieval segment becomes
// measurably slow, which is what makes the timing assertion below deterministic.
type jevSlowEmbedder struct{ delay time.Duration }

func (e jevSlowEmbedder) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	select {
	case <-time.After(e.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}

func (e jevSlowEmbedder) Model() string { return "slow-embedder-stub" }

// jevTestSlowRetriever builds the same in-memory retriever as jevTestRetriever
// with a client that makes every Search take at least delay.
func jevTestSlowRetriever(t *testing.T, delay time.Duration, names ...string) *memory.Retriever {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	for _, name := range names {
		content := fmt.Sprintf("%s field note about the alpha topic", name)
		if err := entries.Upsert(ctx, &memory.Entry{Name: name, Content: content, CharCount: len(content)}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}
	return memory.NewRetrieverWithOptions(entries, vectors, jevSlowEmbedder{delay: delay}, nil, memory.RetrieverOptions{})
}

// TestJevArmRetrieveRecordsTheRetrievalSegment is the regression test for the
// defer-after-return bug: with unnamed results the deferred stamp mutated a local
// copy and every row carried retrieval_ms=0, which SC-007 published as a zero
// wide-pool latency distribution.
func TestJevArmRetrieveRecordsTheRetrievalSegment(t *testing.T) {
	ctx := context.Background()
	retriever := jevTestSlowRetriever(t, 25*time.Millisecond, "one", "two")
	for _, arm := range []jevArm{jevArmA, jevArmD} {
		recipe, err := jevArmRecipeFor(arm)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := jevArmRetrieve(ctx, retriever, "alpha", recipe, nil, 0, 8, nil, nil)
		if err != nil {
			t.Fatalf("arm %s retrieve: %v", arm, err)
		}
		if observation.RetrievalMs < 10 {
			t.Errorf("arm %s reported retrieval_ms=%d after a 25ms retrieval; the segment timing did not reach the caller", arm, observation.RetrievalMs)
		}
	}
}

// TestJevArmQuestionRowsPlumbRetrievalMs checks the second half of that
// regression: the observation's retrieval segment must survive into the frozen
// measurement row.
func TestJevArmQuestionRowsPlumbRetrievalMs(t *testing.T) {
	observations := make(map[jevArm]jevArmObservation, len(jevArmNames()))
	for _, arm := range jevArmNames() {
		observations[arm] = jevArmObservation{Arm: arm, RetrievalMs: 123}
	}
	rows, _, err := measureJevArmQuestion(jevArmQuestionInput{
		Conv: 1, Q: 1, Category: 1,
		QA:           locomoQA{Question: "q", Evidence: []string{"D1:1"}},
		Observations: observations,
	})
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(rows) != len(jevArmNames()) {
		t.Fatalf("rows = %d, want one per arm", len(rows))
	}
	for _, row := range rows {
		if row.RetrievalMs != 123 {
			t.Errorf("arm %s row retrieval_ms = %d, want the observation's 123", row.Arm, row.RetrievalMs)
		}
	}
}
