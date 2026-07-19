package main

import (
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func TestPriceTableAndCostLedger(t *testing.T) {
	prices, err := parsePriceTable(`{"gpt-test":{"in":1.25,"out":10.0}}`)
	if err != nil {
		t.Fatalf("parse prices: %v", err)
	}
	price, ok := prices.Lookup("gpt-test")
	if !ok || price.In != 1.25 || price.Out != 10.0 {
		t.Fatalf("price lookup = %+v, priced=%v", price, ok)
	}
	if _, ok := prices.Lookup("local-model"); ok {
		t.Fatal("missing model should be marked unpriced")
	}

	ledger := newCostLedger(prices)
	ledger.Add("answer", "gpt-test", 1_000, 200)
	ledger.Add("answer", "local-model", 500, 100)
	ledger.Add("extract", "gpt-test", 2_000, 300)
	if got := ledger.ByRole["answer"].Calls; got != 2 {
		t.Fatalf("answer calls = %d, want 2", got)
	}
	if got := ledger.ByRole["answer"].InTokens; got != 1_500 {
		t.Fatalf("answer input tokens = %d, want 1500", got)
	}
	wantUSD := (1_000*1.25 + 200*10.0 + 2_000*1.25 + 300*10.0) / 1_000_000
	if math.Abs(ledger.ActualUSD()-wantUSD) > 1e-12 {
		t.Fatalf("actual usd = %.12f, want %.12f", ledger.ActualUSD(), wantUSD)
	}
}

func TestEstimateAndContextBudgetWarning(t *testing.T) {
	prices, err := parsePriceTable(`{"gpt-test":{"in":1.25,"out":10.0}}`)
	if err != nil {
		t.Fatalf("parse prices: %v", err)
	}
	got, priced := estimateCost(prices, "gpt-test", 10, 1_000, 200)
	want := float64(10*(1_000*1.25+200*10.0)) / 1_000_000
	if !priced || math.Abs(got-want) > 1e-12 {
		t.Fatalf("estimate = %.12f priced=%v, want %.12f/true", got, priced, want)
	}
	if got, priced := estimateCost(prices, "local-model", 10, 1_000, 200); got != 0 || priced {
		t.Fatalf("unpriced estimate = %.2f/%v, want 0/false", got, priced)
	}

	ledger := newCostLedger(prices)
	ledger.AddContextTokens(1_500)
	ledger.AddContextTokens(2_500)
	if got := ledger.AnswerContextTokensMean(); got != 2_000 {
		t.Fatalf("context mean = %.0f, want 2000", got)
	}
	if !ledger.BudgetWarning(1_000) {
		t.Fatal("context mean above 1.5x baseline should warn")
	}
	ledger = newCostLedger(prices)
	ledger.AddContextTokens(1_500)
	if ledger.BudgetWarning(1_000) {
		t.Fatal("context mean at exactly 1.5x baseline should not warn")
	}
}

func TestCostLedgerConcurrentUpdatesAndReports(t *testing.T) {
	ledger := newCostLedger(priceTable{"model": {In: 1, Out: 1}})
	const workers = 32
	const updates = 200
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < updates; i++ {
				ledger.Add("answer", "model", 1, 2)
				ledger.AddContextTokens(3)
				_ = ledger.Report()
			}
		}()
	}
	wg.Wait()

	report := ledger.Report()
	wantCalls := workers * updates
	if report.ByRole["answer"].Calls != wantCalls {
		t.Fatalf("answer calls = %d, want %d", report.ByRole["answer"].Calls, wantCalls)
	}
	if report.ByRole["answer"].InTokens != wantCalls || report.ByRole["answer"].OutTokens != wantCalls*2 {
		t.Fatalf("answer tokens = %d/%d, want %d/%d", report.ByRole["answer"].InTokens, report.ByRole["answer"].OutTokens, wantCalls, wantCalls*2)
	}
	if report.AnswerContextTokensMean != 3 {
		t.Fatalf("context mean = %.0f, want 3", report.AnswerContextTokensMean)
	}
}

func TestSelectionAndEstimateShareQuestionAndCallPlan(t *testing.T) {
	conv := conversation{ID: 1, Sessions: []session{{Index: 1}}, QA: []locomoQA{
		{Question: "normal 1", Answer: []byte(`"one"`), Category: 4},
		{Question: "unknown", Answer: []byte(`"trap"`), Category: adversarialCategory},
		{Question: "normal 2", Answer: []byte(`"two"`), Category: 4},
	}}
	opt := options{
		datasetFormat: "locomo",
		maxQuestions:  1,
		adversarial:   1,
		repeats:       3,
		topK:          5,
		filterPool:    10,
	}
	selected := selectQuestions(conv, opt)
	if len(selected) != 2 || selected[0].QA.Question != "normal 1" || selected[1].QA.Question != "unknown" {
		t.Fatalf("selected questions = %+v, want normal 1 plus tail adversarial", selected)
	}
	plan := buildCallPlan([]conversation{conv}, opt)
	if plan.Questions != len(selected) || plan.ExtractionCalls != 1 {
		t.Fatalf("call plan = %+v, want questions=2 extraction=1", plan)
	}
	opt.retrieval = "hybrid,hybrid+assoc"
	pairedPlan := buildCallPlan([]conversation{conv}, opt)
	if pairedPlan.AnswerCalls != 12 || pairedPlan.JudgeCalls != 12 || pairedPlan.FilterCalls != 12 {
		t.Fatalf("paired call plan = %+v, want answer/filter/judge=12", pairedPlan)
	}
	if pairedPlan.AnswerInTokens != 12*4000 || pairedPlan.AnswerOutTokens != 12*50 || pairedPlan.JudgeInTokens != 12*1600 {
		t.Fatalf("paired calibrated tokens = %+v", pairedPlan)
	}
	opt.retrieval = ""
	opt.opinionPass = true
	if got := buildCallPlan([]conversation{conv}, opt).ExtractionCalls; got != 2 {
		t.Fatalf("opinion call plan extraction calls = %d, want 2", got)
	}
	opt.opinionPass = false
	report := estimateReport([]conversation{conv}, opt, priceTable{
		"answer-model":  {In: 1, Out: 2},
		"extract-model": {In: 3, Out: 4},
	}, "answer-model", "extract-model")
	if report.ByRole["answer"].Calls != plan.AnswerCalls || report.ByRole["extract"].Calls != plan.ExtractionCalls {
		t.Fatalf("report roles = %+v, plan = %+v", report.ByRole, plan)
	}
	var byRoleUSD float64
	for _, role := range report.ByRole {
		byRoleUSD += role.USD
	}
	if math.Abs(byRoleUSD-report.EstimatedUSD) > 1e-12 {
		t.Fatalf("by_role usd = %.12f, estimate = %.12f", byRoleUSD, report.EstimatedUSD)
	}
}

func TestAnswerContextBudgetExcludesFilterAndPrintsWarning(t *testing.T) {
	ledger := newCostLedger(nil)
	recordBenchUsage(ledger, "filter", "filter-model", provider.Usage{InputTokens: 9_000})
	recordBenchUsage(ledger, "rewrite", "rewrite-model", provider.Usage{InputTokens: 8_000})
	recordBenchUsage(ledger, "answer", "answer-model", provider.Usage{InputTokens: 2_000})
	if got := ledger.AnswerContextTokensMean(); got != 2_000 {
		t.Fatalf("answer context mean = %.0f, want 2000", got)
	}
	if warning := formatBudgetSummary(2_000, 1_000); !strings.Contains(warning, "WARNING") {
		t.Fatalf("budget summary = %q, want WARNING", warning)
	}
	if warning := formatBudgetSummary(1_500, 1_000); strings.Contains(warning, "WARNING") {
		t.Fatalf("budget summary at 1.5x = %q, want no WARNING", warning)
	}
}
