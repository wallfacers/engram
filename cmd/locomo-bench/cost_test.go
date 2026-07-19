package main

import (
	"math"
	"sync"
	"testing"
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
