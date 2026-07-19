package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

type tokenPrice struct {
	In  float64 `json:"in"`
	Out float64 `json:"out"`
}

type priceTable map[string]tokenPrice

func parsePriceTable(raw string) (priceTable, error) {
	table := priceTable{}
	if strings.TrimSpace(raw) == "" {
		return table, nil
	}
	if err := json.Unmarshal([]byte(raw), &table); err != nil {
		return nil, fmt.Errorf("parse LOCOMO_PRICE_TABLE: %w", err)
	}
	for model, price := range table {
		if price.In < 0 || price.Out < 0 || math.IsNaN(price.In) || math.IsNaN(price.Out) {
			return nil, fmt.Errorf("price for model %q must be non-negative", model)
		}
	}
	return table, nil
}

func (p priceTable) Lookup(model string) (tokenPrice, bool) {
	price, ok := p[model]
	return price, ok
}

type roleCost struct {
	Calls     int     `json:"calls"`
	InTokens  int     `json:"in_tokens"`
	OutTokens int     `json:"out_tokens"`
	USD       float64 `json:"usd"`
}

type costLedger struct {
	mu             sync.Mutex
	Prices         priceTable
	ByRole         map[string]*roleCost
	UnpricedModels map[string]bool
	contextSum     int64
	contextCount   int
	EstimatedUSD   float64
}

func newCostLedger(prices priceTable) *costLedger {
	byRole := map[string]*roleCost{}
	for _, role := range []string{"extract", "answer", "filter", "rewrite", "judge", "embed"} {
		byRole[role] = &roleCost{}
	}
	return &costLedger{
		Prices:         prices,
		ByRole:         byRole,
		UnpricedModels: map[string]bool{},
	}
}

func (c *costLedger) Add(role, model string, inTokens, outTokens int) {
	if inTokens < 0 {
		inTokens = 0
	}
	if outTokens < 0 {
		outTokens = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	bucket := c.ByRole[role]
	if bucket == nil {
		bucket = &roleCost{}
		c.ByRole[role] = bucket
	}
	bucket.Calls++
	bucket.InTokens += inTokens
	bucket.OutTokens += outTokens
	if price, ok := c.Prices.Lookup(model); ok {
		bucket.USD += tokenUSD(price, inTokens, outTokens)
	} else {
		c.UnpricedModels[model] = true
	}
}

func (c *costLedger) ActualUSD() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.actualUSDLocked()
}

func (c *costLedger) actualUSDLocked() float64 {
	var total float64
	for _, bucket := range c.ByRole {
		total += bucket.USD
	}
	return total
}

func (c *costLedger) AddContextTokens(tokens int) {
	if tokens >= 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.contextSum += int64(tokens)
		c.contextCount++
	}
}

func (c *costLedger) AnswerContextTokensMean() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answerContextTokensMeanLocked()
}

func (c *costLedger) answerContextTokensMeanLocked() float64 {
	if c.contextCount == 0 {
		return 0
	}
	return float64(c.contextSum) / float64(c.contextCount)
}

func (c *costLedger) BudgetWarning(baseline float64) bool {
	mean := c.AnswerContextTokensMean()
	return baseline > 0 && mean > baseline*1.5
}

func estimateCost(prices priceTable, model string, calls, inTokens, outTokens int) (float64, bool) {
	price, ok := prices.Lookup(model)
	if !ok {
		return 0, false
	}
	return float64(calls) * tokenUSD(price, inTokens, outTokens), true
}

func tokenUSD(price tokenPrice, inTokens, outTokens int) float64 {
	return (float64(inTokens)*price.In + float64(outTokens)*price.Out) / 1_000_000
}

type costReport struct {
	EstimatedUSD            float64              `json:"estimated_usd"`
	ActualUSD               float64              `json:"actual_usd"`
	ByRole                  map[string]*roleCost `json:"by_role"`
	AnswerContextTokensMean float64              `json:"answer_context_tokens_mean"`
	UnpricedModels          []string             `json:"unpriced_models,omitempty"`
}

func (c *costLedger) Report() costReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	roles := make(map[string]*roleCost, len(c.ByRole))
	for role, bucket := range c.ByRole {
		copyBucket := *bucket
		roles[role] = &copyBucket
	}
	unpriced := make([]string, 0, len(c.UnpricedModels))
	for model := range c.UnpricedModels {
		unpriced = append(unpriced, model)
	}
	sort.Strings(unpriced)
	return costReport{
		EstimatedUSD:            c.EstimatedUSD,
		ActualUSD:               c.actualUSDLocked(),
		ByRole:                  roles,
		AnswerContextTokensMean: c.answerContextTokensMeanLocked(),
		UnpricedModels:          unpriced,
	}
}

func writeCost(path string, report costReport) error {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644) //nolint:gosec // operator-selected run directory
}
