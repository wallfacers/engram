package main

import "testing"

func TestStructuredGapBudgetUsesSameCumulativeLimitsAsControl(t *testing.T) {
	control, err := newGapControlBudget(10, 4096)
	if err != nil {
		t.Fatal(err)
	}
	treatment, err := newGapTreatmentBudget(10, 3, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := control.FirstRoundCandidateLimit, 10; got != want {
		t.Fatalf("control first-round candidate limit = %d, want %d", got, want)
	}
	if got, want := treatment.FirstRoundCandidateLimit, 7; got != want {
		t.Fatalf("treatment first-round candidate limit = %d, want %d", got, want)
	}
	if got, want := treatment.RefetchCandidateLimit, 3; got != want {
		t.Fatalf("treatment refetch candidate limit = %d, want %d", got, want)
	}
	if err := validateComparableGapBudgets(control, treatment); err != nil {
		t.Fatalf("comparable budgets rejected: %v", err)
	}
	if treatment.TokenCap != control.TokenCap || treatment.AnswerCallLimit != 1 || treatment.RetrievalCallLimit != 1 {
		t.Fatalf("treatment did not retain shared cap/one-call limits: %+v", treatment)
	}
}

func TestStructuredGapBudgetRejectsCumulativeBudgetInflation(t *testing.T) {
	budget, err := newGapTreatmentBudget(5, 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		usage gapBudgetUsage
	}{
		{name: "union exceeds N", usage: gapBudgetUsage{CandidateCount: 6}},
		{name: "cumulative tokens exceed cap", usage: gapBudgetUsage{TokenCount: 101}},
		{name: "two retrievals", usage: gapBudgetUsage{RetrievalCalls: 2}},
		{name: "two answer calls", usage: gapBudgetUsage{AnswerCalls: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := budget.validateUsage(test.usage); err == nil {
				t.Fatalf("usage %+v bypassed cumulative budget %+v", test.usage, budget)
			}
		})
	}
}

func TestStructuredGapBudgetRejectsUnfairArmShapes(t *testing.T) {
	if _, err := newGapTreatmentBudget(3, 0, 100); err == nil {
		t.Fatal("zero reserve was accepted")
	}
	if _, err := newGapTreatmentBudget(3, 3, 100); err == nil {
		t.Fatal("reserve equal to candidate cap was accepted")
	}
	control, err := newGapControlBudget(4, 100)
	if err != nil {
		t.Fatal(err)
	}
	treatment, err := newGapTreatmentBudget(4, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	treatment.TokenCap = 101
	if err := validateComparableGapBudgets(control, treatment); err == nil {
		t.Fatal("different token cap was accepted as a fair comparison")
	}
}
