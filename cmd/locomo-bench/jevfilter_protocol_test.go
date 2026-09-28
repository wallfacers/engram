package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// This file tests the 038-protocol integration surface: the pre-registration, the
// filter call-class audit, the retrieval-budget exclusion, the validity gate, the
// paired significance tests, the cost plan, and the SC-001..SC-007 verdict.

func jevTestRegistration() jevFilterRegistration {
	registration := newJevFilterRegistration("jev-pinned-2026-09-21", "jev.example.test", filter.DefaultPolicy(), jevArmPoolSize, jevArmAnswerRepetitions)
	registration.PilotGateConfirmed = true
	registration.WarmupDisposed = true
	registration.SameWindowReps = true
	return registration
}

func jevTestValidity() jevRunValidity {
	return jevRunValidity{
		Valid: true, Complete: true,
		QuestionsMeasured: 2, QuestionsExpected: 2,
		RepetitionsMeasured: 1, RepetitionsExpected: 1,
		RowsMeasured: 10, RowsExpected: 10,
		ArmRowsEqual: true, IdentityRate: 1, WithinCapRate: 1, AnswerComplianceRate: 1,
	}
}

func jevTestArtifacts() map[string]bool {
	present := make(map[string]bool, len(jevCoreValidityArtifacts))
	for _, artifact := range jevCoreValidityArtifacts {
		present[artifact] = true
	}
	return present
}

type jevTestRowSpec struct {
	Arm         jevArm
	Shown       int
	Tokens      int
	Category    int
	Gradeable   bool
	ShownRecall float64
	Empty       bool
	Degraded    bool
	JevMs       int
	JevUSD      float64
	Correct     func(question int) bool
}

// jevTestRows builds a consistent paired fixture: every arm answers the same
// question set, so the paired contrasts are well defined.
func jevTestRows(questions int, specs ...jevTestRowSpec) []jevArmQuestionRow {
	rows := make([]jevArmQuestionRow, 0, questions*len(specs))
	for _, spec := range specs {
		for question := 1; question <= questions; question++ {
			category := spec.Category
			block := jevArmMainBlock
			if category == 5 {
				block = jevArmCategoryFiveBlock
			}
			row := jevArmQuestionRow{
				Conv: 1, Q: question, Category: category, Block: block, Arm: spec.Arm,
				Shown: spec.Shown, AnswerInputTokens: spec.Tokens, Gradeable: spec.Gradeable,
				ShownRecall: spec.ShownRecall, EmptyInjection: spec.Empty, Degraded: spec.Degraded,
				JevMs: spec.JevMs, JevUSD: spec.JevUSD,
			}
			if spec.Correct != nil {
				row.CorrectMeasured = true
				row.Correct = spec.Correct(question)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func jevTestDerived(questions int, budgetRecall float64, comparable, matched bool) []jevArmQuestionDerived {
	derived := make([]jevArmQuestionDerived, 0, questions)
	for question := 1; question <= questions; question++ {
		derived = append(derived, jevArmQuestionDerived{
			Conv: 1, Q: question, Block: jevArmMainBlock,
			BudgetGradeable: budgetRecall >= 0, DShown: 3,
			CBudgetRecallGate: budgetRecall, CBudgetRecallReference: budgetRecall, CBudgetRecallEqual: budgetRecall,
			DegradedSetComparable: comparable, DegradedShownMatchesA: matched,
			DegradedC8Comparable: comparable, DegradedShownMatchesC8: matched,
		})
	}
	return derived
}

func jevCriterion(t *testing.T, verdict jevArmVerdict, id string) jevSuccessCriterion {
	t.Helper()
	for _, criterion := range verdict.Criteria {
		if criterion.ID == id {
			return criterion
		}
	}
	t.Fatalf("criterion %s is missing from the verdict", id)
	return jevSuccessCriterion{}
}

func TestValidatePinnedFilterModel(t *testing.T) {
	for _, model := range []string{"jev-2026-09-21", "typesafe/jev@2026-09-21", "Jev-1.2.3"} {
		if err := validatePinnedFilterModel(model); err != nil {
			t.Errorf("pinned model %q rejected: %v", model, err)
		}
	}
	for _, model := range []string{"", "  ", "jev-latest", "jev-LATEST", "jev/latest", "jev_stable", "jev:edge", "jev@main"} {
		if err := validatePinnedFilterModel(model); err == nil {
			t.Errorf("floating or empty model %q was accepted", model)
		}
	}
}

func TestJevFilterRegistrationValidation(t *testing.T) {
	registration := jevTestRegistration()
	if err := validateJevFilterRegistration(registration); err != nil {
		t.Fatalf("the frozen registration was rejected: %v", err)
	}
	digest, err := registration.Digest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q (%v)", digest, err)
	}
	if digest == "" {
		t.Fatal("empty digest")
	}

	cases := map[string]func(*jevFilterRegistration){
		"mechanism key":     func(r *jevFilterRegistration) { r.MechanismKey = "filter.other.v1" },
		"filter model":      func(r *jevFilterRegistration) { r.FilterModel = "jev-latest" },
		"theta":             func(r *jevFilterRegistration) { r.Theta = 1.5 },
		"relax above theta": func(r *jevFilterRegistration) { r.RelaxTheta = 0.9 },
		"relax max":         func(r *jevFilterRegistration) { r.RelaxMax = -1 },
		"k_show_max":        func(r *jevFilterRegistration) { r.KShowMax = 20 },
		"pool":              func(r *jevFilterRegistration) { r.Pool = 300 },
		"cap":               func(r *jevFilterRegistration) { r.AnswerInputCap = 8192 },
		// 1 is the legal pilot declaration (slice 13); 2 has no majority semantics.
		"repetitions":   func(r *jevFilterRegistration) { r.AnswerRepetitions = 2 },
		"lowered floor": func(r *jevFilterRegistration) { r.EmptyInjectionFloor = 0.2 },
		"arm set":       func(r *jevFilterRegistration) { r.Arms = []string{"A", "B"} },
	}
	for name, mutate := range cases {
		broken := jevTestRegistration()
		mutate(&broken)
		if err := validateJevFilterRegistration(broken); err == nil {
			t.Errorf("%s: a broken registration was accepted", name)
		}
	}
	if jevTestRegistration().EmptyInjectionFloor != jevEmptyInjectionFloor {
		t.Errorf("the registration floor %v is not the pre-registered %v", jevTestRegistration().EmptyInjectionFloor, jevEmptyInjectionFloor)
	}
}

func TestAttachJevFilterRegistrationIsAdditive(t *testing.T) {
	// A protocol without a registration must not mention the mechanism at all,
	// which is what keeps unrelated manifests hash-stable.
	plain := evalProtocol{}
	canonical, err := canonicalEvalProtocolJSON(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonical), jevFilterMechanismKey) || strings.Contains(string(canonical), `"filter"`) {
		t.Errorf("an unregistered protocol carries filter fields: %s", canonical)
	}
	if err := validateJevProtocolBinding(plain); err != nil {
		t.Errorf("an unregistered protocol failed binding validation: %v", err)
	}

	protocol := evalProtocol{Budget: evalBudgetProtocol{AnswerInputTokenCap: jevArmAnswerInputCap}, Aggregation: evalAggregationProtocol{AnswerRepetitions: jevArmAnswerRepetitions}}
	registration := jevTestRegistration()
	if err := attachJevFilterRegistration(&protocol, registration); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !protocol.Experiment.MechanismFlags[jevFilterMechanismKey] {
		t.Error("the mechanism flag was not set")
	}
	if protocol.Experiment.Filter == nil || protocol.Experiment.Filter.FilterModel != registration.FilterModel {
		t.Fatalf("registration not stored: %+v", protocol.Experiment.Filter)
	}
	canonical, err = canonicalEvalProtocolJSON(protocol)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), jevFilterMechanismKey) || !strings.Contains(string(canonical), registration.FilterModel) {
		t.Errorf("registered protocol does not carry the mechanism key and pinned model: %s", canonical)
	}
	if err := validateJevProtocolBinding(protocol); err != nil {
		t.Errorf("a registered protocol failed binding validation: %v", err)
	}
	// A manifest that claims the mechanism without the registration must fail.
	orphan := evalProtocol{Experiment: evalExperimentProtocol{MechanismFlags: map[string]bool{jevFilterMechanismKey: true}}}
	if err := validateJevProtocolBinding(orphan); err == nil {
		t.Error("a mechanism flag without a registration was accepted")
	}
}

func TestValidateJevProtocolBindingChecksCapAndRepetitions(t *testing.T) {
	build := func(cap int, repetitions int) evalProtocol {
		protocol := evalProtocol{Budget: evalBudgetProtocol{AnswerInputTokenCap: cap}, Aggregation: evalAggregationProtocol{AnswerRepetitions: repetitions}}
		registration := jevTestRegistration()
		if err := attachJevFilterRegistration(&protocol, registration); err != nil {
			t.Fatalf("attach: %v", err)
		}
		return protocol
	}
	if err := validateJevProtocolBinding(build(jevArmAnswerInputCap, jevArmAnswerRepetitions)); err != nil {
		t.Errorf("a bound protocol failed: %v", err)
	}
	if err := validateJevProtocolBinding(build(4096, jevArmAnswerRepetitions)); err == nil {
		t.Error("a cap mismatch was accepted; arm B would be silently truncated")
	}
	if err := validateJevProtocolBinding(build(jevArmAnswerInputCap, 1)); err == nil {
		t.Error("a repetition mismatch was accepted")
	}
}

func TestValidateJevDeclaredPrerequisites(t *testing.T) {
	if err := validateJevDeclaredPrerequisites(jevTestRegistration()); err != nil {
		t.Errorf("the declared prerequisites were rejected: %v", err)
	}
	for name, mutate := range map[string]func(*jevFilterRegistration){
		"pilot gate": func(r *jevFilterRegistration) { r.PilotGateConfirmed = false },
		"warm-up":    func(r *jevFilterRegistration) { r.WarmupDisposed = false },
		"one window": func(r *jevFilterRegistration) { r.SameWindowReps = false },
	} {
		registration := jevTestRegistration()
		mutate(&registration)
		if err := validateJevDeclaredPrerequisites(registration); err == nil {
			t.Errorf("an undeclared %s prerequisite was accepted", name)
		}
	}
}

func TestJevFilterCallJournalForcesClassAndRejectsForeignRows(t *testing.T) {
	dir := t.TempDir()
	journal, err := openJevFilterCallJournal(dir, "sha256:protocol")
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := journal.Record(jevFilterCallRecord{Conv: 1, Q: 1, Arm: string(jevArmD), Pool: 150, Memories: 150, LatencyMs: 300, USD: 0.0004}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if journal.Count() != 1 {
		t.Errorf("count = %d, want 1", journal.Count())
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, jevFilterCallJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	var decoded jevFilterCallRecord
	if err := json.Unmarshal(raw[:len(raw)-1], &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Class != jevFilterCallClass || decoded.ProtocolHash != "sha256:protocol" || decoded.MechanismKey != jevFilterMechanismKey {
		t.Errorf("journal row is not attributable: %+v", decoded)
	}
	// Reopening appends, and a foreign row is refused instead of silently mixed in.
	journal, err = openJevFilterCallJournal(dir, "sha256:protocol")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(`{"schema":"022.v1","protocol_hash":"sha256:protocol","mechanism_key":"filter.jev.v1","class":"retrieval","conv":1,"q":1}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, jevFilterCallJournalFile), append(raw, foreign...), 0o644); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	if _, err := openJevFilterCallJournal(dir, "sha256:protocol"); err == nil {
		t.Error("a foreign call class was accepted into the filter journal")
	}
	if _, err := openJevFilterCallJournal(dir, "sha256:other"); err == nil {
		t.Error("a mismatched protocol hash was accepted")
	}
	if _, err := openJevFilterCallJournal(dir, ""); err == nil {
		t.Error("an empty protocol hash was accepted")
	}
}

func TestJevCallBudgetExcludesFilterCalls(t *testing.T) {
	budget := jevCallBudget{RetrievalCalls: 900, FilterCalls: 400, RetrievalCallLimit: 1000}
	if err := budget.Validate(); err != nil {
		t.Errorf("filter calls broke the retrieval budget: %v", err)
	}
	over := jevCallBudget{RetrievalCalls: 1100, FilterCalls: 0, RetrievalCallLimit: 1000}
	if err := over.Validate(); err == nil {
		t.Error("a real retrieval-budget breach was accepted")
	}
	if err := (jevCallBudget{RetrievalCalls: -1}).Validate(); err == nil {
		t.Error("a negative count was accepted")
	}
}

func TestValidateJevRunValidityFailsClosed(t *testing.T) {
	valid := jevValidityInput{Validity: jevTestValidity(), Present: jevTestArtifacts(), Registration: jevTestRegistration()}
	if err := validateJevRunValidity(valid); err != nil {
		t.Fatalf("the complete evidence set was rejected: %v", err)
	}
	incomplete := valid
	incomplete.Validity.WithinCapRate = 0.9
	if err := validateJevRunValidity(incomplete); err == nil {
		t.Error("incomplete per-repeat receipts were accepted")
	}
	missing := valid
	missing.Present = map[string]bool{}
	if err := validateJevRunValidity(missing); err == nil {
		t.Error("missing validity artifacts were accepted")
	}
	b0 := valid
	b0.B0ContinuityDeclared = true
	if err := validateJevRunValidity(b0); err == nil {
		t.Error("a declared B0 continuity without its receipt was accepted")
	}
	b0.Present[evalB0ContinuitySummaryFile] = true
	if err := validateJevRunValidity(b0); err != nil {
		t.Errorf("a declared B0 continuity with its receipt was rejected: %v", err)
	}
	undeclared := valid
	undeclared.Registration = jevTestRegistration()
	undeclared.Registration.WarmupDisposed = false
	if err := validateJevRunValidity(undeclared); err == nil {
		t.Error("an undeclared prerequisite was accepted by the validity gate")
	}
}

func TestJevArmContrastForUsesExactMcNemar(t *testing.T) {
	rows := jevTestRows(10,
		jevTestRowSpec{Arm: jevArmB, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Correct: func(question int) bool { return question > 3 }},
	)
	contrast, err := jevArmContrastFor(rows, jevArmB, jevArmD)
	if err != nil {
		t.Fatalf("contrast: %v", err)
	}
	if contrast.Questions != 10 {
		t.Fatalf("paired questions = %d, want 10", contrast.Questions)
	}
	if contrast.DeltaPP != -30 {
		t.Errorf("delta = %v pp, want -30 (D is right on 7 of 10)", contrast.DeltaPP)
	}
	wantP, err := exactMcNemarTwoSided(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contrast.McNemarP != wantP {
		t.Errorf("p = %v, want the exact McNemar p %v", contrast.McNemarP, wantP)
	}
	if contrast.CI.DeltaPP != -30 {
		t.Errorf("paired CI centre = %v, want -30", contrast.CI.DeltaPP)
	}
	// A mismatched question set is not a paired contrast.
	partial := rows[:len(rows)-1]
	if _, err := jevArmContrastFor(partial, jevArmB, jevArmD); err == nil {
		t.Error("a mismatched paired set was accepted")
	}
	if _, err := jevArmContrastFor(nil, jevArmB, jevArmD); err == nil {
		t.Error("an empty contrast was accepted")
	}
}

func TestJevPolicyFromEnv(t *testing.T) {
	env := map[string]string{}
	getenv := func(key string) string { return env[key] }
	policy, err := jevPolicyFromEnv(getenv)
	if err != nil || policy != filter.DefaultPolicy() {
		t.Fatalf("defaults = %+v (%v)", policy, err)
	}
	env["ENGRAM_JEV_THETA"] = "0.6"
	env["ENGRAM_JEV_RELAX_THETA"] = "0.4"
	env["ENGRAM_JEV_RELAX_MAX"] = "5"
	env["ENGRAM_JEV_KSHOW_MAX"] = "12"
	policy, err = jevPolicyFromEnv(getenv)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if policy.Theta != 0.6 || policy.RelaxTheta != 0.4 || policy.RelaxMax != 5 || policy.KShowMax != 12 {
		t.Errorf("policy = %+v", policy)
	}
	env["ENGRAM_JEV_RELAX"] = "0"
	if policy, err = jevPolicyFromEnv(getenv); err != nil || !policy.RelaxDisabled {
		t.Errorf("kill switch = %+v (%v)", policy, err)
	}
	// The switch follows the server's boolean semantics (strconv.ParseBool), not a
	// literal "0" comparison: "false" must disable the relax stage too.
	for _, off := range []string{"false", "FALSE", "False"} {
		env["ENGRAM_JEV_RELAX"] = off
		if policy, err = jevPolicyFromEnv(getenv); err != nil || !policy.RelaxDisabled {
			t.Errorf("ENGRAM_JEV_RELAX=%q = %+v (%v), want the relax stage disabled", off, policy, err)
		}
	}
	for _, on := range []string{"1", "true"} {
		env["ENGRAM_JEV_RELAX"] = on
		if policy, err = jevPolicyFromEnv(getenv); err != nil || policy.RelaxDisabled {
			t.Errorf("ENGRAM_JEV_RELAX=%q = %+v (%v), want the relax stage enabled", on, policy, err)
		}
	}
	env["ENGRAM_JEV_RELAX"] = "maybe"
	if _, err := jevPolicyFromEnv(getenv); err == nil {
		t.Error("a non-boolean ENGRAM_JEV_RELAX was accepted")
	}
	delete(env, "ENGRAM_JEV_RELAX")
	for key, value := range map[string]string{
		"ENGRAM_JEV_THETA":       "1.5",
		"ENGRAM_JEV_RELAX_THETA": "0.9",
		"ENGRAM_JEV_RELAX_MAX":   "-2",
		"ENGRAM_JEV_KSHOW_MAX":   "20",
	} {
		broken := map[string]string{key: value}
		if _, err := jevPolicyFromEnv(func(k string) string { return broken[k] }); err == nil {
			t.Errorf("%s=%s was accepted", key, value)
		}
	}
	if _, err := jevPolicyFromEnv(func(k string) string {
		if k == "ENGRAM_JEV_THETA" {
			return "not-a-number"
		}
		return ""
	}); err == nil {
		t.Error("a non-numeric theta was accepted")
	}
}

func TestPlanJevArmCostCountsFilterCallsOnlyOnFilteredArms(t *testing.T) {
	prices := priceTable{
		"answer-model": {In: 1, Out: 2},
		"filter-model": {In: 1, Out: 0},
	}
	plan := planJevArmCost(100, 3, prices, "filter-model", "answer-model", "answer-model")
	if plan.Questions != 100 || plan.Repetitions != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.ByArm) != len(jevArmNames()) {
		t.Fatalf("got %d arm lines, want one per arm", len(plan.ByArm))
	}
	for _, line := range plan.ByArm {
		if line.AnswerCalls != 300 || line.JudgeCalls != 300 {
			t.Errorf("arm %s calls = %d answer / %d judge, want 300 each", line.Arm, line.AnswerCalls, line.JudgeCalls)
		}
		wantFilterCalls := 0
		if line.Arm == string(jevArmD) || line.Arm == string(jevArmDNoRelax) || line.Arm == string(jevArmE) {
			wantFilterCalls = 300
		}
		if line.FilterCalls != wantFilterCalls {
			t.Errorf("arm %s filter calls = %d, want %d", line.Arm, line.FilterCalls, wantFilterCalls)
		}
	}
	if plan.AnswerCalls != 1800 || plan.FilterCalls != 900 {
		t.Errorf("totals = %d answer / %d filter, want 1800/900", plan.AnswerCalls, plan.FilterCalls)
	}
	if plan.FilterInTokens != 900*jevArmPoolSize*jevEstimateFilterTokensPerCandidate {
		t.Errorf("filter input tokens = %d", plan.FilterInTokens)
	}
	if plan.EstimatedUSD <= 0 {
		t.Error("the plan priced a run at zero")
	}
	if plan.AnswerInputCap != jevArmAnswerInputCap || plan.EmptyInjectionFloor != jevEmptyInjectionFloor {
		t.Errorf("the plan does not carry the frozen protocol numbers: %+v", plan)
	}
	// Unknown models are reported, never priced as free.
	unpriced := planJevArmCost(1, 1, priceTable{}, "filter-model", "answer-model", "judge-model")
	if len(unpriced.UnpricedModels) != 3 {
		t.Errorf("unpriced models = %v, want all three named", unpriced.UnpricedModels)
	}
	if unpriced.EstimatedUSD != 0 {
		t.Errorf("an unpriced plan reported %v USD", unpriced.EstimatedUSD)
	}
}

func TestJevSC001AndSC002Statuses(t *testing.T) {
	base := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
	passInput := base
	passInput.Rows = jevTestRows(10,
		jevTestRowSpec{Arm: jevArmB, Tokens: 3000, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Tokens: 20, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 500, JevMs: 300, JevUSD: 0.0004, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 3, Tokens: 500},
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
	)
	passInput.Derived = jevTestDerived(10, 0.5, true, true)
	verdict, err := buildJevArmVerdict(passInput)
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if status := jevCriterion(t, verdict, "SC-001").Status; status != jevCriterionPass {
		t.Errorf("SC-001 = %s (%s), want pass for an exactly-matched D arm", status, jevCriterion(t, verdict, "SC-001").Detail)
	}
	if status := jevCriterion(t, verdict, "SC-002").Status; status != jevCriterionPass {
		t.Errorf("SC-002 = %s (%s), want pass at 3 shown and 1/6 of B's tokens", status, jevCriterion(t, verdict, "SC-002").Detail)
	}

	regressed := passInput
	regressed.Rows = jevTestRows(10,
		jevTestRowSpec{Arm: jevArmB, Tokens: 3000, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Tokens: 20, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 2000, Correct: func(question int) bool { return question > 7 }},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 3, Tokens: 2000},
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
	)
	verdict, err = buildJevArmVerdict(regressed)
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if status := jevCriterion(t, verdict, "SC-001").Status; status != jevCriterionHold {
		t.Errorf("SC-001 = %s, want hold for a 70pp regression", status)
	}
	if status := jevCriterion(t, verdict, "SC-002").Status; status != jevCriterionHold {
		t.Errorf("SC-002 = %s, want hold for 2000/3000 tokens", status)
	}
}

func TestJevSC003Statuses(t *testing.T) {
	build := func(budgetRecall float64, dRecall float64) jevArmVerdict {
		input := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
		input.Rows = jevTestRows(10,
			jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 500, Gradeable: true, ShownRecall: dRecall},
			jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000},
		)
		input.Derived = jevTestDerived(10, budgetRecall, true, true)
		verdict, err := buildJevArmVerdict(input)
		if err != nil {
			t.Fatalf("verdict: %v", err)
		}
		return verdict
	}
	if status := jevCriterion(t, build(0.5, 1.0), "SC-003").Status; status != jevCriterionPass {
		t.Errorf("SC-003 = %s, want pass when D beats every C budget", status)
	}
	if status := jevCriterion(t, build(0.9, 0.5), "SC-003").Status; status != jevCriterionHold {
		t.Errorf("SC-003 = %s, want hold when D trails the equal budget", status)
	}
	// With nothing gradeable the criterion is unmeasured, never a pass.
	empty := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
	verdict, err := buildJevArmVerdict(empty)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-003").Status; status != jevCriterionHold {
		t.Errorf("SC-003 = %s without any gradeable question, want hold", status)
	}
}

func TestJevSC004CategoryFiveBlock(t *testing.T) {
	build := func(emptyQuestions int) jevArmVerdict {
		input := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
		rows := jevTestRows(4, jevTestRowSpec{Arm: jevArmC, Category: 5, Shown: 12, Tokens: 100})
		for question := 1; question <= 4; question++ {
			rows = append(rows, jevArmQuestionRow{
				Conv: 1, Q: question, Category: 5, Block: jevArmCategoryFiveBlock, Arm: jevArmDNoRelax,
				Shown: 0, EmptyInjection: question <= emptyQuestions,
			})
		}
		input.Rows = rows
		verdict, err := buildJevArmVerdict(input)
		if err != nil {
			t.Fatalf("verdict: %v", err)
		}
		return verdict
	}
	// The floor is what decides: C@12 never injects an empty set, so criterion ① is
	// satisfied by any empty rate.
	if status := jevCriterion(t, build(2), "SC-004").Status; status != jevCriterionPass {
		t.Errorf("SC-004 = %s, want pass at a 50%% empty rate", status)
	}
	if status := jevCriterion(t, build(1), "SC-004").Status; status != jevCriterionHold {
		t.Errorf("SC-004 = %s, want hold below the pre-registered 50%% floor", status)
	}
	unmeasured := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
	unmeasured.Rows = jevTestRows(2, jevTestRowSpec{Arm: jevArmC, Shown: 12})
	verdict, err := buildJevArmVerdict(unmeasured)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-004").Status; status != jevCriterionHold {
		t.Errorf("SC-004 = %s without the category-5 block, want hold", status)
	}
}

func TestJevSC005RequiresTheDegradedPass(t *testing.T) {
	gated := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(), Rows: jevTestRows(2, jevTestRowSpec{Arm: jevArmD, Shown: 3})}
	verdict, err := buildJevArmVerdict(gated)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-005").Status; status != jevCriterionHold {
		t.Errorf("SC-005 = %s in a gated pass, want hold", status)
	}

	degraded := jevArmVerdictInput{
		Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(),
		DegradedPass:          true,
		DefaultParityEvidence: "declared evidence",
	}
	degraded.Rows = jevTestRows(4,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 8, Tokens: 800, Degraded: true, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 8, Tokens: 800, Degraded: true},
	)
	degraded.Derived = jevTestDerived(4, 0.5, true, true)
	verdict, err = buildJevArmVerdict(degraded)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-005").Status; status != jevCriterionPass {
		t.Errorf("SC-005 = %s (%s), want pass when every degraded set matches arm A", status, jevCriterion(t, verdict, "SC-005").Detail)
	}
	if status := jevCriterion(t, verdict, "SC-006").Status; status != jevCriterionPass {
		t.Errorf("SC-006 = %s, want pass in a fully matched no-key pass", status)
	}
	for _, id := range []string{"SC-001", "SC-002", "SC-003", "SC-004"} {
		if status := jevCriterion(t, verdict, id).Status; status != jevCriterionHold {
			t.Errorf("%s = %s in a degraded pass, want hold (unmeasured)", id, status)
		}
	}
	if verdict.Verdict != string(evalVerdictHOLD) {
		t.Errorf("degraded-pass verdict = %s, want HOLD while the gated criteria are unmeasured", verdict.Verdict)
	}

	mismatched := degraded
	mismatched.Derived = jevTestDerived(4, 0.5, true, false)
	verdict, err = buildJevArmVerdict(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-005").Status; status != jevCriterionHold {
		t.Errorf("SC-005 = %s with a mismatched set, want hold", status)
	}
	if status := jevCriterion(t, verdict, "SC-006").Status; status != jevCriterionHold {
		t.Errorf("SC-006 = %s with a mismatched set, want hold", status)
	}
}

func TestJevSC005HoldsWhenTheDegradedSetMissesC8(t *testing.T) {
	degraded := jevArmVerdictInput{
		Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(),
		DegradedPass: true,
	}
	degraded.Rows = jevTestRows(2,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 8, Tokens: 800, Degraded: true, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 8, Tokens: 800, Degraded: true},
	)
	// The arm-A check passes but the spec's C@8 comparator disagrees: the criterion
	// must not paper over that.
	degraded.Derived = jevTestDerived(2, 0.5, true, true)
	for index := range degraded.Derived {
		degraded.Derived[index].DegradedShownMatchesC8 = false
	}
	verdict, err := buildJevArmVerdict(degraded)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-005").Status; status != jevCriterionHold {
		t.Errorf("SC-005 = %s with a C@8 mismatch, want hold", status)
	}
	if verdict.Report.DegradedC8Matched != 0 || verdict.Report.DegradedC8Total != 2 {
		t.Errorf("C@8 tallies = %d/%d, want 0/2", verdict.Report.DegradedC8Matched, verdict.Report.DegradedC8Total)
	}
}

func TestJevSC006GatedPassNeedsEvidence(t *testing.T) {
	without := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts()}
	verdict, err := buildJevArmVerdict(without)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-006").Status; status != jevCriterionHold {
		t.Errorf("SC-006 = %s without declared parity evidence, want hold", status)
	}
	with := without
	with.DefaultParityEvidence = "engine parity + contract parity tests"
	verdict, err = buildJevArmVerdict(with)
	if err != nil {
		t.Fatal(err)
	}
	// A gated pass can only name the engine-side evidence: it is `declared`, never
	// `pass`, so the merged verdict cannot mistake a declaration for a measurement.
	if status := jevCriterion(t, verdict, "SC-006").Status; status != jevCriterionDeclared {
		t.Errorf("SC-006 = %s with declared evidence in a gated pass, want declared", status)
	}
	if verdict.Verdict != string(evalVerdictHOLD) {
		t.Errorf("a declared criterion must not yield GO, got %q", verdict.Verdict)
	}
	if got := jevVerdictFor([]jevSuccessCriterion{{ID: "SC-006", Status: jevCriterionDeclared}}); got != string(evalVerdictHOLD) {
		t.Errorf("jevVerdictFor(declared) = %q, want hold: a declaration is not a measurement", got)
	}
}

func TestJevSC007FilterSegmentAccounting(t *testing.T) {
	build := func(jevMs int, usd float64, attribution string, degraded bool) jevArmVerdict {
		input := jevArmVerdictInput{
			Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(),
			JevCostAttribution: attribution, DegradedPass: degraded,
		}
		input.Rows = jevTestRows(2, jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 500, JevMs: jevMs, JevUSD: usd})
		verdict, err := buildJevArmVerdict(input)
		if err != nil {
			t.Fatal(err)
		}
		return verdict
	}
	if status := jevCriterion(t, build(300, 0.0004, "", false), "SC-007").Status; status != jevCriterionPass {
		t.Errorf("SC-007 = %s for an on-expectation filter segment, want pass", status)
	}
	if status := jevCriterion(t, build(5000, 0.0004, "", false), "SC-007").Status; status != jevCriterionHold {
		t.Errorf("SC-007 = %s for a 16x latency anomaly without attribution, want hold", status)
	}
	if status := jevCriterion(t, build(5000, 0.0004, "cold GPU warm-up on the first batch", false), "SC-007").Status; status != jevCriterionPass {
		t.Errorf("SC-007 = %s with an attribution, want pass", status)
	}
	if status := jevCriterion(t, build(300, 0.5, "", false), "SC-007").Status; status != jevCriterionHold {
		t.Errorf("SC-007 = %s for a >1000x cost anomaly without attribution, want hold", status)
	}
	if status := jevCriterion(t, build(0, 0, "", false), "SC-007").Status; status != jevCriterionHold {
		t.Errorf("SC-007 = %s without any filter call, want hold", status)
	}
	if status := jevCriterion(t, build(300, 0.0004, "", true), "SC-007").Status; status != jevCriterionHold {
		t.Errorf("SC-007 = %s in a degraded pass, want hold", status)
	}
}

func TestJevVerdictNeverDefaultsToPass(t *testing.T) {
	verdict, err := buildJevArmVerdict(jevArmVerdictInput{Registration: jevTestRegistration()})
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if len(verdict.Criteria) != 7 {
		t.Fatalf("got %d criteria, want SC-001..SC-007", len(verdict.Criteria))
	}
	for _, criterion := range verdict.Criteria {
		if criterion.Status != jevCriterionHold {
			t.Errorf("%s = %s with no measurements at all, want hold", criterion.ID, criterion.Status)
		}
		if criterion.Detail == "" {
			t.Errorf("%s carries no reason", criterion.ID)
		}
	}
	if verdict.Verdict != string(evalVerdictHOLD) {
		t.Errorf("verdict = %s, want HOLD", verdict.Verdict)
	}
	if verdict.ValidityError == "" {
		t.Error("an unvalidated run did not record its validity error")
	}
	if verdict.PromotionDeclared == "" {
		t.Error("the promotion input declaration is missing")
	}
	if verdict.Promotion == evalVerdictGO {
		t.Error("an all-HOLD run was promoted")
	}
	if len(verdict.Report.Main) != len(jevArmNames()) || len(verdict.Report.CategoryFive) != len(jevArmNames()) {
		t.Errorf("report arms = %d main / %d category-5", len(verdict.Report.Main), len(verdict.Report.CategoryFive))
	}
}

func TestJevArmEmptyInjectionRate(t *testing.T) {
	if rate, questions := jevArmEmptyInjectionRate(nil); rate != 0 || questions != 0 {
		t.Errorf("empty input = %v/%d", rate, questions)
	}
	rows := []jevArmQuestionRow{{EmptyInjection: true}, {EmptyInjection: false}, {EmptyInjection: false}, {EmptyInjection: false}}
	if rate, questions := jevArmEmptyInjectionRate(rows); rate != 0.25 || questions != 4 {
		t.Errorf("rate = %v over %d rows, want 0.25 over 4", rate, questions)
	}
}

// jevWriteRunDir is a run directory carrying the artifacts earlier stages
// genuinely write (the frozen protocol and the filter call journal). The rows
// file and the summary receipt are the arms run's own output and are written by
// writeJevArmArtifacts itself, so the writer tests exercise the criteria rather
// than the validity gate.
func jevWriteRunDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, artifact := range []string{evalProtocolArtifactFile, jevFilterCallJournalFile} {
		if err := os.WriteFile(filepath.Join(dir, artifact), []byte("{}\n"), 0o644); err != nil { //nolint:gosec
			t.Fatal(err)
		}
	}
	return dir
}

func TestWriteJevArmArtifactsLandsEverythingAndSurfacesValidity(t *testing.T) {
	dir := jevWriteRunDir(t)
	opt := options{runDir: dir}
	registration := jevTestRegistration()
	rows := jevTestRows(2,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 400, JevMs: 300, JevUSD: 0.0004, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 3, Tokens: 400, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmE, Shown: 6, Tokens: 700, JevMs: 300, JevUSD: 0.0004, Correct: func(int) bool { return true }},
	)
	if err := writeJevArmArtifacts(opt, registration, nil, rows, nil, 2, 8, nil, false, ""); err != nil {
		t.Fatalf("write artifacts: %v", err)
	}
	for _, name := range []string{jevArmRowsFile, jevArmReportFile, jevArmVerdictFile, evalSummaryArtifactFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
	var verdict jevArmVerdict
	if err := readJSON(filepath.Join(dir, jevArmVerdictFile), &verdict); err != nil {
		t.Fatalf("read verdict: %v", err)
	}
	if verdict.Pass != jevArmPassGated || verdict.MechanismKey != jevFilterMechanismKey {
		t.Errorf("verdict = %+v", verdict)
	}
	if verdict.RegistrationDigest != mustDigest(t, registration) {
		t.Errorf("verdict registration digest = %q", verdict.RegistrationDigest)
	}
	// Remove a required artifact the run does not itself write: the run must fail
	// closed, and it must still have written the verdict so the operator can
	// read why.
	if err := os.Remove(filepath.Join(dir, jevFilterCallJournalFile)); err != nil {
		t.Fatal(err)
	}
	if err := writeJevArmArtifacts(opt, registration, nil, rows, nil, 2, 8, nil, false, ""); err == nil {
		t.Error("a missing validity artifact did not fail the run")
	}
}

func TestJevArmVerdictReportInvalidRun(t *testing.T) {
	dir := t.TempDir()
	for _, artifact := range jevCoreValidityArtifacts {
		if err := os.WriteFile(filepath.Join(dir, artifact), []byte("{}\n"), 0o644); err != nil { //nolint:gosec
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(map[string]any{"validity": jevTestValidity()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, evalSummaryArtifactFile), raw, 0o644); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	registration := jevTestRegistration()
	rows := jevTestRows(2,
		jevTestRowSpec{Arm: jevArmB, Shown: 2, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 2, Tokens: 100, Correct: func(int) bool { return true }},
	)
	// The invalid reason must dominate a run whose rows happen to look fine.
	if err := writeJevArmArtifacts(options{runDir: dir}, registration, nil, rows, nil, 1, 4, nil, false, "arm B hit the frozen cap"); err == nil {
		t.Fatal("an invalid run exited zero")
	}
	var verdict jevArmVerdict
	if err := readJSON(filepath.Join(dir, jevArmVerdictFile), &verdict); err != nil {
		t.Fatalf("read verdict: %v", err)
	}
	if verdict.Verdict != string(evalVerdictHOLD) || verdict.Promotion != evalVerdictInvalid {
		t.Errorf("invalid run verdict = %s / %s, want HOLD / INVALID", verdict.Verdict, verdict.Promotion)
	}
	if verdict.InvalidReason == "" {
		t.Error("the invalid reason was not recorded")
	}
}

func TestWriteJevArmArtifactsMergesBothPasses(t *testing.T) {
	dir := jevWriteRunDir(t)
	opt := options{runDir: dir}
	registration := jevTestRegistration()

	// Pass 1 (gated): the filter scores, so SC-001 can be measured.
	gatedRows := jevTestRows(4,
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 400, JevMs: 300, JevUSD: 0.0004, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 3, Tokens: 400, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmE, Shown: 6, Tokens: 700, JevMs: 300, JevUSD: 0.0004, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
	)
	if err := writeJevArmArtifacts(opt, registration, nil, gatedRows, jevTestDerived(4, 0.5, true, true), 2, 8, nil, false, ""); err != nil {
		t.Fatalf("gated pass: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, jevArmMergedVerdictFile)); !os.IsNotExist(err) {
		t.Fatal("a merged verdict was written before the degraded pass ran")
	}

	// Pass 2 (degraded): the arms fall back, which is where SC-005 lives.
	degradedRows := jevTestRows(4,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmD, Shown: 8, Tokens: 800, Degraded: true, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 8, Tokens: 800, Degraded: true, Correct: func(int) bool { return true }},
		jevTestRowSpec{Arm: jevArmE, Shown: 8, Tokens: 900, Degraded: true, Correct: func(int) bool { return true }},
	)
	if err := writeJevArmArtifacts(opt, registration, nil, degradedRows, jevTestDerived(4, 0.5, true, true), 0, 8, nil, true, ""); err != nil {
		t.Fatalf("degraded pass: %v", err)
	}
	var merged jevArmVerdict
	if err := readJSON(filepath.Join(dir, jevArmMergedVerdictFile), &merged); err != nil {
		t.Fatalf("read merged verdict: %v", err)
	}
	if merged.Pass != "merged" {
		t.Errorf("merged pass = %q", merged.Pass)
	}
	statuses := map[string]string{}
	for _, criterion := range merged.Criteria {
		statuses[criterion.ID] = criterion.Status
	}
	if statuses["SC-001"] != jevCriterionPass {
		t.Errorf("merged SC-001 = %s, want the gated pass's measurement", statuses["SC-001"])
	}
	if statuses["SC-005"] != jevCriterionPass || statuses["SC-006"] != jevCriterionPass {
		t.Errorf("merged SC-005/SC-006 = %s/%s, want the degraded pass's measurements", statuses["SC-005"], statuses["SC-006"])
	}
	if statuses["SC-004"] != jevCriterionHold {
		t.Errorf("merged SC-004 = %s, want hold (the category-5 block was not run)", statuses["SC-004"])
	}
	if merged.Verdict != string(evalVerdictHOLD) {
		t.Errorf("merged verdict = %s, want HOLD while SC-004 is unmeasured", merged.Verdict)
	}
}

func mustDigest(t *testing.T, registration jevFilterRegistration) string {
	t.Helper()
	digest, err := registration.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestMergeJevArmVerdicts(t *testing.T) {
	registration := jevTestRegistration()
	gated := jevArmVerdict{Pass: jevArmPassGated, RegistrationDigest: mustDigest(t, registration), Criteria: []jevSuccessCriterion{
		{ID: "SC-001", Status: jevCriterionPass, Detail: "ok"},
		{ID: "SC-004", Status: jevCriterionHold, Detail: "needs the degraded pass"},
		{ID: "SC-005", Status: jevCriterionHold, Detail: "requires the degraded pass"},
		{ID: "SC-006", Status: jevCriterionPass, Detail: "declared"},
	}}
	degraded := jevArmVerdict{Pass: jevArmPassDegraded, RegistrationDigest: mustDigest(t, registration), Criteria: []jevSuccessCriterion{
		{ID: "SC-001", Status: jevCriterionHold, Detail: "unmeasured"},
		{ID: "SC-005", Status: jevCriterionPass, Detail: "matched"},
		{ID: "SC-006", Status: jevCriterionPass, Detail: "matched"},
		{ID: "SC-007", Status: jevCriterionHold, Detail: "degraded pass has no filter segment"},
	}}
	merged, err := mergeJevArmVerdicts(gated, degraded)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.Verdict != string(evalVerdictHOLD) {
		t.Errorf("merged verdict = %s, want HOLD while SC-004 is unmeasured", merged.Verdict)
	}
	statuses := map[string]string{}
	for _, criterion := range merged.Criteria {
		statuses[criterion.ID] = criterion.Status
	}
	if statuses["SC-001"] != jevCriterionPass || statuses["SC-005"] != jevCriterionPass {
		t.Errorf("merged statuses = %v, want each criterion taken from the pass that measured it", statuses)
	}
	if _, err := mergeJevArmVerdicts(gated, gated); err == nil {
		t.Error("two passes of the same kind were merged")
	}
	other := jevArmVerdict{Pass: jevArmPassDegraded, RegistrationDigest: "sha256:other"}
	if _, err := mergeJevArmVerdicts(gated, other); err == nil {
		t.Error("verdicts from different registrations were merged")
	}
}

func TestJevFilterCallJournalRecordsUsageTokens(t *testing.T) {
	dir := t.TempDir()
	journal, err := openJevFilterCallJournal(dir, "sha256:protocol")
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := journal.Record(jevFilterCallRecord{
		Conv: 1, Q: 2, Arm: string(jevArmD), Pool: 150, Memories: 150,
		LatencyMs: 300, USD: 0.0004, InputTokens: 1200, OutputTokens: 60,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, jevFilterCallJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	var decoded jevFilterCallRecord
	if err := json.Unmarshal(raw[:len(raw)-1], &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.InputTokens != 1200 || decoded.OutputTokens != 60 {
		t.Fatalf("journal row tokens = %d/%d, want 1200/60 (SC-007 filter-segment accounting)", decoded.InputTokens, decoded.OutputTokens)
	}
	for _, want := range []string{`"input_tokens":1200`, `"output_tokens":60`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("journal row is missing %s: %s", want, raw)
		}
	}
}

func TestJevArmCoverageViolationRefusesAPartialCohort(t *testing.T) {
	protocol := evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}}
	full := []jevArmQuestionDerived{
		{Conv: 1, Q: 1, Block: jevArmMainBlock},
		{Conv: 1, Q: 2, Block: jevArmMainBlock},
	}
	if reason := jevArmCoverageViolation(&protocol, full); reason != "" {
		t.Errorf("a full cohort was refused: %s", reason)
	}
	if reason := jevArmCoverageViolation(&protocol, full[:1]); reason == "" {
		t.Error("a run that measured half the frozen questions was accepted")
	}
	// The category-5 declared block is reported separately and must not be counted
	// as main-population coverage.
	withBlock := append(append([]jevArmQuestionDerived{}, full...), jevArmQuestionDerived{Conv: 1, Q: 3, Block: jevArmCategoryFiveBlock})
	if reason := jevArmCoverageViolation(&protocol, withBlock); reason != "" {
		t.Errorf("the declared block disturbed main-block coverage: %s", reason)
	}
	if reason := jevArmCoverageViolation(&evalProtocol{}, full); reason != "" {
		t.Error("an unset question count fabricated a violation")
	}
}

func TestValidateJevArmsOptionsPinsTheChunkQuotaRecipe(t *testing.T) {
	base := options{datasetFormat: "locomo", repeats: jevArmAnswerRepetitions, jevArmsReps: jevArmAnswerRepetitions, estimate: true}
	if err := validateJevArmsOptions(base, []string{"hybrid"}); err != nil {
		t.Fatalf("a valid estimate invocation was refused: %v", err)
	}
	quota := base
	quota.chunkQuota = 5
	if err := validateJevArmsOptions(quota, []string{"hybrid"}); err == nil {
		t.Error("--chunk-quota changes pool membership, so arm C and the filtered arms stop being single-variable; it was accepted")
	}
	categoryQuota := base
	categoryQuota.catQuotaSpec = "1:8,2:6"
	if err := validateJevArmsOptions(categoryQuota, []string{"hybrid"}); err == nil {
		t.Error("--cat-chunk-quota was accepted")
	}
}

// TestValidateJevArmsOptionsRequiresTheFrozenProtocolFlag pins the run-mode
// pre-flight to the flag that names the frozen protocol. main.go assigns
// opt.formalProtocol only when it has loaded --eval-protocol, and that load
// happens after this pre-flight — so keying the check on the pointer refuses
// every gated/degraded --jev-arms pass regardless of flags. The path stays the
// authority here; the run-time backstop in runJevArmProtocol still requires the
// bound protocol.
func TestValidateJevArmsOptionsRequiresTheFrozenProtocolFlag(t *testing.T) {
	for _, name := range []string{
		"ENGRAM_JEV_THETA", "ENGRAM_JEV_RELAX_THETA", "ENGRAM_JEV_RELAX_MAX",
		"ENGRAM_JEV_KSHOW_MAX", "ENGRAM_JEV_RELAX", "ENGRAM_JEV_MODEL",
	} {
		t.Setenv(name, "")
	}
	runMode := options{
		datasetFormat:         "locomo",
		repeats:               jevArmAnswerRepetitions,
		jevArmsReps:           jevArmAnswerRepetitions,
		runDir:                t.TempDir(),
		storeDir:              t.TempDir(),
		chunks:                true,
		noIDKRetry:            true,
		tokenCounterBaseURL:   "http://127.0.0.1:8000/v1",
		jevDegradedPass:       true,
		jevModel:              "jev-pinned-2026-09-21",
		jevPilotGateConfirmed: true,
		jevWarmupDisposed:     true,
		jevSameWindowReps:     true,
		jevB0Continuity:       true,
		evalProtocolPath:      "protocol.json",
	}
	if runMode.formalProtocol != nil {
		t.Fatal("the fixture no longer mirrors run mode: formalProtocol is bound before the pre-flight")
	}
	if err := validateJevArmsOptions(runMode, []string{"hybrid"}); err != nil {
		t.Fatalf("a gated run-mode invocation carrying --eval-protocol was refused before the protocol is bound: %v", err)
	}
	unfrozen := runMode
	unfrozen.evalProtocolPath = ""
	err := validateJevArmsOptions(unfrozen, []string{"hybrid"})
	if err == nil {
		t.Fatal("--jev-arms without --eval-protocol was accepted")
	}
	if want := "--jev-arms requires a frozen protocol via --eval-protocol"; !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name the missing flag: %v", err)
	}
	// The flag is the authority: a bound protocol cannot make an unfrozen run
	// legal, because main.go only binds it together with the flag.
	bound := unfrozen
	bound.formalProtocol = &evalProtocol{}
	if err := validateJevArmsOptions(bound, []string{"hybrid"}); err == nil {
		t.Fatal("a bound protocol without --eval-protocol was accepted")
	}
}

func TestValidateJevArmsRegistrationEnvironment(t *testing.T) {
	for _, name := range []string{
		"ENGRAM_JEV_THETA", "ENGRAM_JEV_RELAX_THETA", "ENGRAM_JEV_RELAX_MAX",
		"ENGRAM_JEV_KSHOW_MAX", "ENGRAM_JEV_RELAX", "ENGRAM_JEV_MODEL",
	} {
		t.Setenv(name, "")
	}
	declared := options{
		jevModel:              "jev-pinned-2026-09-21",
		jevPilotGateConfirmed: true,
		jevWarmupDisposed:     true,
		jevSameWindowReps:     true,
		jevB0Continuity:       true,
	}
	if err := validateJevArmsRegistrationEnvironment(declared); err != nil {
		t.Fatalf("a fully declared environment was refused: %v", err)
	}
	noB0 := declared
	noB0.jevB0Continuity = false
	if err := validateJevArmsRegistrationEnvironment(noB0); err == nil {
		t.Error("a run that does not declare its B0 continuity receipts was accepted")
	}
	floating := declared
	floating.jevModel = "jev-latest"
	if err := validateJevArmsRegistrationEnvironment(floating); err == nil {
		t.Error("a floating filter model was accepted")
	}
	undeclared := declared
	undeclared.jevPilotGateConfirmed = false
	if err := validateJevArmsRegistrationEnvironment(undeclared); err == nil {
		t.Error("an undeclared pilot gate was accepted")
	}
}

// jevProbeRecordingCounter records the exact AnswerInput the fingerprint probe
// sends and counts calls, so a test can prove both which model the probe names
// and that a refused probe never reached the wire.
type jevProbeRecordingCounter struct {
	fingerprint string
	calls       int
	lastInput   evidencecompiler.AnswerInput
}

func (counter *jevProbeRecordingCounter) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	counter.calls++
	counter.lastInput = input
	return evidencecompiler.TokenCount{InputTokens: 3, Fingerprint: counter.fingerprint}, nil
}

// TestValidateJevCounterFingerprintProbesWithTheFrozenAnswererModel pins the probe
// to the frozen answerer model id: vLLM's /tokenize rejects an unknown model with
// HTTP 404, so the placeholder id this probe used to send failed every --jev-arms
// pass before a single paid call.
func TestValidateJevCounterFingerprintProbesWithTheFrozenAnswererModel(t *testing.T) {
	const (
		model = "Qwen/Qwen3.6-35B-A3B-FP8"
		want  = "sha256:answerer-template-r1"
	)
	counter := &jevProbeRecordingCounter{fingerprint: want}
	if err := validateJevCounterFingerprint(context.Background(), counter, want, model); err != nil {
		t.Fatalf("a probe naming the frozen answerer model was refused: %v", err)
	}
	if counter.calls != 1 {
		t.Fatalf("token counter calls = %d, want 1", counter.calls)
	}
	if counter.lastInput.Model != model {
		t.Errorf("probe model = %q, want the frozen answerer %q", counter.lastInput.Model, model)
	}
	if counter.lastInput.Model == "fingerprint-probe" {
		t.Error("the probe still sends the placeholder model id")
	}
}

// TestValidateJevCounterFingerprintRefusesABlankAnswererModel pins the fail-closed
// rule: without a frozen answerer id the probe cannot name the tokenizer's model,
// so the run refuses instead of falling back to a placeholder id.
func TestValidateJevCounterFingerprintRefusesABlankAnswererModel(t *testing.T) {
	for _, model := range []string{"", "   ", "\t\n"} {
		counter := &jevProbeRecordingCounter{fingerprint: "sha256:answerer-template-r1"}
		err := validateJevCounterFingerprint(context.Background(), counter, "sha256:answerer-template-r1", model)
		if err == nil {
			t.Fatalf("a blank answerer model id (%q) was accepted", model)
		}
		if !strings.Contains(err.Error(), "answerer") {
			t.Errorf("refusal for %q does not name the missing answerer model: %v", model, err)
		}
		if counter.calls != 0 {
			t.Errorf("a blank answerer model still probed the counter %d times", counter.calls)
		}
	}
}

// TestValidateJevCounterFingerprintStillRefusesDrift pins the comparison: a counter
// whose fingerprint differs from the frozen one stops the run.
func TestValidateJevCounterFingerprintStillRefusesDrift(t *testing.T) {
	counter := &jevProbeRecordingCounter{fingerprint: "sha256:other-template"}
	err := validateJevCounterFingerprint(context.Background(), counter, "sha256:answerer-template-r1", "Qwen/Qwen3.6-35B-A3B-FP8")
	if err == nil {
		t.Fatal("a drifted counter fingerprint was accepted")
	}
	if want := `differs from the frozen "sha256:answerer-template-r1"`; !strings.Contains(err.Error(), want) {
		t.Errorf("drift refusal = %v, want it to contain %q", err, want)
	}
}

// TestValidateJevCounterFingerprintKeepsNilCounterAndBlankWant pins the two
// pre-existing behaviors the answerer-model argument must not disturb: a nil
// counter is always refused, and a blank frozen fingerprint needs no probe.
func TestValidateJevCounterFingerprintKeepsNilCounterAndBlankWant(t *testing.T) {
	err := validateJevCounterFingerprint(context.Background(), nil, "sha256:answerer-template-r1", "Qwen/Qwen3.6-35B-A3B-FP8")
	if err == nil || !strings.Contains(err.Error(), "jev arms require a token counter") {
		t.Fatalf("nil counter refusal = %v, want the pre-existing message", err)
	}
	counter := &jevProbeRecordingCounter{fingerprint: "sha256:other-template"}
	if err := validateJevCounterFingerprint(context.Background(), counter, "  ", "Qwen/Qwen3.6-35B-A3B-FP8"); err != nil {
		t.Fatalf("a blank frozen fingerprint was refused: %v", err)
	}
	if counter.calls != 0 {
		t.Errorf("a blank frozen fingerprint still probed the counter %d times", counter.calls)
	}
}

func TestJevFilterRegistrationIsCoveredByTheProtocolDigest(t *testing.T) {
	protocol := evalProtocol{
		Budget:      evalBudgetProtocol{AnswerInputTokenCap: jevArmAnswerInputCap},
		Aggregation: evalAggregationProtocol{AnswerRepetitions: jevArmAnswerRepetitions},
	}
	before, err := evalProtocolFingerprint(protocol)
	if err != nil {
		t.Fatal(err)
	}
	registration := jevTestRegistration()
	if err := attachJevFilterRegistration(&protocol, registration); err != nil {
		t.Fatalf("attach: %v", err)
	}
	after, err := evalProtocolFingerprint(protocol)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("attaching the registration did not change the protocol digest, so the digest does not cover it")
	}

	plain := evalProtocol{
		Budget:      evalBudgetProtocol{AnswerInputTokenCap: jevArmAnswerInputCap},
		Aggregation: evalAggregationProtocol{AnswerRepetitions: jevArmAnswerRepetitions},
	}
	if err := verifyJevFilterRegistrationBinding(plain, registration); err == nil {
		t.Error("a manifest frozen without the registration was accepted by the run")
	}
	if err := verifyJevFilterRegistrationBinding(protocol, registration); err != nil {
		t.Errorf("a manifest carrying the same registration was refused: %v", err)
	}
	drifted := registration
	drifted.Theta = 0.4
	if err := verifyJevFilterRegistrationBinding(protocol, drifted); err == nil {
		t.Error("a run whose registration differs from the frozen one was accepted")
	}
}

func TestJevCallBudgetRecordsBothCounts(t *testing.T) {
	rows := jevTestRows(2,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100},
		jevTestRowSpec{Arm: jevArmD, Shown: 3, Tokens: 500, JevMs: 300, JevUSD: 0.0004},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 3, Tokens: 500, JevMs: 300, JevUSD: 0.0004},
	)
	report, err := buildJevArmReport(jevArmVerdictInput{Rows: rows, RetrievalCalls: 40, FilterCalls: 8, RetrievalCallLimit: 40})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.CallBudget.RetrievalCalls != 40 || report.CallBudget.FilterCalls != 8 {
		t.Errorf("call budget = %+v, want both counts recorded", report.CallBudget)
	}
	if err := report.CallBudget.Validate(); err != nil {
		t.Errorf("a run exactly at its scaled limit failed validation: %v", err)
	}
	over := jevArmVerdictInput{Rows: rows, RetrievalCalls: 41, FilterCalls: 8, RetrievalCallLimit: 40}
	if _, err := buildJevArmReport(over); err == nil {
		t.Error("a retrieval count above the scaled limit was accepted")
	}
}

func TestJevSC005HoldsWhenTheAccuracyContrastIsUnmeasurable(t *testing.T) {
	input := jevArmVerdictInput{
		Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(),
		DegradedPass: true, DefaultParityEvidence: "declared evidence",
	}
	// The evidence sets match structurally, but no arm was answered, so the A->C
	// contrast has no questions: the accuracy leg has no evidence and must HOLD.
	input.Rows = jevTestRows(2,
		jevTestRowSpec{Arm: jevArmA, Shown: 8, Tokens: 800},
		jevTestRowSpec{Arm: jevArmC, Shown: 12, Tokens: 100},
		jevTestRowSpec{Arm: jevArmD, Shown: 8, Tokens: 800, Degraded: true},
		jevTestRowSpec{Arm: jevArmB, Shown: 150, Tokens: 3000},
		jevTestRowSpec{Arm: jevArmDNoRelax, Shown: 8, Tokens: 800, Degraded: true},
	)
	input.Derived = jevTestDerived(2, 0.5, true, true)
	verdict, err := buildJevArmVerdict(input)
	if err != nil {
		t.Fatal(err)
	}
	if status := jevCriterion(t, verdict, "SC-005").Status; status != jevCriterionHold {
		t.Errorf("SC-005 = %s (%s), want hold when the A->C contrast is unmeasurable", status, jevCriterion(t, verdict, "SC-005").Detail)
	}
}

func TestJevSC007CountsBothFilteredArmsAndGatesOnP95(t *testing.T) {
	build := func(rows []jevArmQuestionRow) jevSuccessCriterion {
		input := jevArmVerdictInput{Registration: jevTestRegistration(), Validity: jevTestValidity(), Artifacts: jevTestArtifacts(), Rows: rows}
		verdict, err := buildJevArmVerdict(input)
		if err != nil {
			t.Fatal(err)
		}
		return jevCriterion(t, verdict, "SC-007")
	}
	slowTail := func(normal, slow int) []jevArmQuestionRow {
		rows := make([]jevArmQuestionRow, 0, normal+slow)
		for i := 1; i <= normal+slow; i++ {
			jevMs := 300
			if i > normal {
				jevMs = 5000
			}
			rows = append(rows, jevArmQuestionRow{
				Conv: 1, Q: i, Category: 1, Block: jevArmMainBlock, Arm: jevArmD,
				Shown: 3, AnswerInputTokens: 500, JevMs: jevMs, JevUSD: 0.0004,
			})
		}
		return rows
	}
	// Both filtered arms are part of the segment accounting.
	both := append(slowTail(3, 0),
		jevArmQuestionRow{Conv: 1, Q: 11, Category: 1, Block: jevArmMainBlock, Arm: jevArmDNoRelax, Shown: 3, AnswerInputTokens: 500, JevMs: 300, JevUSD: 0.0004},
		jevArmQuestionRow{Conv: 1, Q: 12, Category: 1, Block: jevArmMainBlock, Arm: jevArmDNoRelax, Shown: 3, AnswerInputTokens: 500, JevMs: 300, JevUSD: 0.0004},
	)
	if criterion := build(both); criterion.Metrics["jev_calls"] != 5 {
		t.Errorf("jev_calls = %v, want both filtered arms counted (5)", criterion.Metrics["jev_calls"])
	}
	// A slow tail that leaves the median on expectation must still be caught: the
	// gate is p95, not p50.
	tail := build(slowTail(18, 2))
	if tail.Status != jevCriterionHold {
		t.Errorf("SC-007 = %s (%s), want hold for a >10x p95 anomaly without attribution", tail.Status, tail.Detail)
	}
	if tail.Metrics["jev_ms_p50"] != 300 {
		t.Errorf("p50 = %v, the fixture is wrong: the median must stay on expectation", tail.Metrics["jev_ms_p50"])
	}
}

func TestMergedJevArmVerdictRefusesRegistrationDrift(t *testing.T) {
	gated := jevArmVerdict{Pass: jevArmPassGated, RegistrationDigest: "sha256:one"}
	drifted := jevArmVerdict{Pass: jevArmPassDegraded, RegistrationDigest: "sha256:two"}
	if _, err := mergedJevArmVerdict(&gated, drifted); err == nil {
		t.Error("two passes under different registrations were skipped silently")
	}
	if merged, err := mergedJevArmVerdict(nil, gated); err != nil || merged != nil {
		t.Errorf("a missing counterpart produced %v, %v; want a silent skip", merged, err)
	}
	if merged, err := mergedJevArmVerdict(&gated, gated); err != nil || merged != nil {
		t.Errorf("two passes of the same kind produced %v, %v; want a silent skip", merged, err)
	}
}

func TestJevArmInvalidReasonNeverLetsAFailedRunLookMeasured(t *testing.T) {
	protocol := evalProtocol{Benchmark: evalBenchmarkProvenance{QuestionCount: 2}}
	full := []jevArmQuestionDerived{
		{Conv: 1, Q: 1, Block: jevArmMainBlock},
		{Conv: 1, Q: 2, Block: jevArmMainBlock},
	}
	if reason := jevArmInvalidReason("", nil, &protocol, full); reason != "" {
		t.Errorf("a complete, successful run was marked invalid: %s", reason)
	}
	// A plain call failure is an invalid reason too: otherwise a partial row set
	// could satisfy every gate over a smaller denominator and leave a GO verdict
	// behind a non-zero exit.
	aborted := jevArmInvalidReason("", fmt.Errorf("judge call failed"), &protocol, full)
	if aborted == "" {
		t.Fatal("a failed run produced no invalid reason")
	}
	if !strings.Contains(aborted, "judge call failed") {
		t.Errorf("invalid reason = %q, want the underlying failure named", aborted)
	}
	partial := jevArmInvalidReason("", nil, &protocol, full[:1])
	if partial == "" {
		t.Fatal("a partial cohort produced no invalid reason")
	}
	// The first recorded cause wins.
	if got := jevArmInvalidReason("arm B hit the frozen cap", fmt.Errorf("later"), &protocol, full[:1]); got != "arm B hit the frozen cap" {
		t.Errorf("invalid reason = %q, want the first recorded cause", got)
	}
}

// --- openjev backend wiring (P1.5) ----------------------------------------

// TestJevFilterConfigWidensTimeoutsForTheLocalShim pins the openjev deadline
// wiring: the backend is a local shim answering with a 35B model in seconds per
// shard, and the engine's 1s defaults would cut every filter call off. Both knobs
// have to move, because the per-request cap is min(remaining deadline, cap).
func TestJevFilterConfigWidensTimeoutsForTheLocalShim(t *testing.T) {
	policy := filter.DefaultPolicy()
	cfg := jevFilterConfig(options{
		jevBaseURL: "http://127.0.0.1:8020",
		jevModel:   "Qwen3.6-shim-pinned",
		jevAPIKey:  "openjev",
		jevPath:    "/answers",
	}, policy)

	if cfg.Deadline != 30*time.Second {
		t.Errorf("Deadline = %s, want 30s", cfg.Deadline)
	}
	if cfg.PerRequestTimeout != 30*time.Second {
		t.Errorf("PerRequestTimeout = %s, want 30s", cfg.PerRequestTimeout)
	}
	if cfg.BaseURL != "http://127.0.0.1:8020" || cfg.Model != "Qwen3.6-shim-pinned" || cfg.APIKey != "openjev" || cfg.Path != "/answers" {
		t.Errorf("address/per-key fields dropped: %+v", cfg)
	}
	if cfg.Policy != policy {
		t.Errorf("policy = %+v, want the run's policy %+v", cfg.Policy, policy)
	}

	// A caller that pins its own bound wins, and the per-request cap follows it.
	pinned := jevFilterConfig(options{jevDeadline: 5 * time.Second}, policy)
	if pinned.Deadline != 5*time.Second || pinned.PerRequestTimeout != 5*time.Second {
		t.Errorf("explicit bound = %s/%s, want 5s/5s", pinned.Deadline, pinned.PerRequestTimeout)
	}
}

// TestJevFilterConfigPinsTheMeasuredShardSize pins the eval-side shard-size
// wiring: the gateway 503-storms the engine's 48-candidate pointer requests, so
// the harness configures 12. The end-to-end leg proves the value survives the
// engine's clamp (150 candidates leave as 13 twelve-candidate requests) — with
// the old 32 floor they would arrive as 5 thirty-two-candidate requests.
func TestJevFilterConfigPinsTheMeasuredShardSize(t *testing.T) {
	policy := filter.DefaultPolicy()
	cfg := jevFilterConfig(options{
		jevBaseURL: "http://127.0.0.1:8020",
		jevModel:   "Qwen3.6-shim-pinned",
		jevAPIKey:  "openjev",
	}, policy)
	if cfg.ShardSize != jevFilterShardSize {
		t.Fatalf("ShardSize = %d, want %d", cfg.ShardSize, jevFilterShardSize)
	}
	// A caller-pinned deadline moves the timeout knobs only.
	pinned := jevFilterConfig(options{jevDeadline: 5 * time.Second}, policy)
	if pinned.ShardSize != jevFilterShardSize {
		t.Errorf("pinned deadline changed the shard size: %d, want %d", pinned.ShardSize, jevFilterShardSize)
	}

	var requests int32
	var maxMemories int32
	answers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State struct {
				Memories map[string]json.RawMessage `json:"memories"`
			} `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode pointer request: %v", err)
			return
		}
		atomic.AddInt32(&requests, 1)
		if n := int32(len(req.State.Memories)); n > atomic.LoadInt32(&maxMemories) {
			atomic.StoreInt32(&maxMemories, n)
		}
		probs := make(map[string]float64, len(req.Questions))
		for key := range req.Questions {
			probs[key] = 0.9
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probabilities": probs,
			"usage":         map[string]int{"prompt_tokens": 10, "completion_tokens": 2},
		})
	}))
	t.Cleanup(answers.Close)

	client, err := buildJevFilterClient(options{
		jevBaseURL: answers.URL,
		jevModel:   "Qwen3.6-shim-pinned",
		jevAPIKey:  "openjev",
	}, policy)
	if err != nil {
		t.Fatalf("build jev filter client: %v", err)
	}
	if client == nil {
		t.Fatal("a fully configured client must not collapse to nil")
	}

	cands := make([]filter.Candidate, 150)
	for i := range cands {
		cands[i] = filter.Candidate{
			ID:    fmt.Sprintf("m%d", i),
			Name:  fmt.Sprintf("memory-%d", i),
			Text:  fmt.Sprintf("body-%d", i),
			Score: float64(150 - i),
		}
	}
	probs, meta, err := client.Filter(context.Background(), "which memory matters?", cands)
	if err != nil {
		t.Fatalf("filter through the harness client: %v (degraded: %v, notes %v)", err, meta.Degraded, meta.Notes)
	}
	if len(probs) != len(cands) {
		t.Fatalf("aligned probabilities = %d, want %d", len(probs), len(cands))
	}
	if got := atomic.LoadInt32(&requests); got != 13 {
		t.Fatalf("requests = %d, want 13 (150 candidates at the configured %d per shard)", got, jevFilterShardSize)
	}
	if got := atomic.LoadInt32(&maxMemories); got != jevFilterShardSize {
		t.Fatalf("largest shard = %d memories, want the configured %d", got, jevFilterShardSize)
	}
	if meta.Degraded {
		t.Errorf("the harness wiring must not degrade: %v", meta.Notes)
	}
}

// TestBuildJevFilterClientSurvivesALocalShimCall drives the harness's own client
// against a /answers endpoint that takes 1.5s to answer: with the 1s defaults this
// call degrades, with the harness wiring it must return probabilities.
func TestBuildJevFilterClientSurvivesALocalShimCall(t *testing.T) {
	const latency = 1500 * time.Millisecond
	answers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/answers" {
			t.Errorf("shim path = %q, want /answers", r.URL.Path)
		}
		select {
		case <-time.After(latency):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"probabilities":{"need_m0":0.9},"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
	}))
	t.Cleanup(answers.Close)

	client, err := buildJevFilterClient(options{
		jevBaseURL: answers.URL,
		jevModel:   "Qwen3.6-shim-pinned",
		jevAPIKey:  "openjev",
	}, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("build jev filter client: %v", err)
	}
	if client == nil {
		t.Fatal("a fully configured client must not collapse to nil")
	}

	probs, meta, err := client.Filter(context.Background(), "which memory matters?", []filter.Candidate{
		{ID: "m0", Name: "memory-m0", Text: "body-m0", Score: 1},
	})
	if err != nil {
		t.Fatalf("filter through a %s-latency shim: %v (degraded: %v, notes %v)", latency, err, meta.Degraded, meta.Notes)
	}
	if want := []float64{0.9}; !reflect.DeepEqual(probs, want) {
		t.Errorf("probabilities = %v, want %v", probs, want)
	}
	if meta.Degraded {
		t.Errorf("the widened wiring must not degrade: %v", meta.Notes)
	}
}
