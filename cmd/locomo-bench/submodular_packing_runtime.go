package main

// 045 T009: e2e wiring for submodular packing. retrieveCandidates is the
// single retrieval dispatch used by the formal answer path: with packing off
// it is a byte-identical passthrough to retrieveWithQuotaDiagnostics (T008
// golden locks this); with packing on it selects from the same wide pool
// under the paired budget anchor instead of quota truncation.
//
// Budget anchor (contracts v1, --pack-budget-anchor):
//   paired (default) — per-question budget = the SAME-BATCH control arm's
//     per-question answer_context_tokens, read from --anchor-run's
//     results-*.jsonl (real usage.InputTokens accounting, plan R4); a
//     missing question falls back to the control mean.
//   mean — the control run's mean for every question.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wallfacers/engram/memory"
)

// packRunConfig is the per-run packing configuration (derived from options).
type packRunConfig struct {
	Enabled    bool
	PoolSize   int // 0 = max(6×topK, 300)
	Weights    packWeights
	AnchorMode string // paired | mean
	anchor     *packAnchorData
}

type packAnchorData struct {
	PerQuestion map[string]int
	Mean        float64
}

// packAnchorCache avoids re-reading the anchor run per question (options is
// passed by value through the freeze path; the cache keys on the path).
var packAnchorCache sync.Map // string → *packAnchorData

// packConfigForRun derives the packing config; errors fail closed.
func packConfigForRun(opt options) (packRunConfig, error) {
	cfg := packRunConfig{Weights: defaultPackWeights(), AnchorMode: "paired"}
	if !opt.submodularPack {
		return cfg, nil
	}
	w, err := parsePackWeights(opt.packWeights)
	if err != nil {
		return packRunConfig{}, err
	}
	mode := strings.TrimSpace(opt.packBudgetAnchor)
	if mode == "" {
		mode = "paired"
	}
	if mode != "paired" && mode != "mean" {
		return packRunConfig{}, fmt.Errorf("--pack-budget-anchor must be paired or mean, got %q", mode)
	}
	if strings.TrimSpace(opt.anchorRun) == "" {
		return packRunConfig{}, fmt.Errorf("--submodular-pack requires --anchor-run pointing at the same-batch control run (budget anchor)")
	}
	anchor, err := loadPackAnchor(opt.anchorRun)
	if err != nil {
		return packRunConfig{}, err
	}
	cfg = packRunConfig{
		Enabled:    true,
		PoolSize:   opt.packPoolSize,
		Weights:    w,
		AnchorMode: mode,
		anchor:     anchor,
	}
	return cfg, nil
}

// loadPackAnchor reads results-*.jsonl from the control run dir and extracts
// per-question answer_context_tokens plus the mean.
func loadPackAnchor(runDir string) (*packAnchorData, error) {
	if cached, ok := packAnchorCache.Load(runDir); ok {
		return cached.(*packAnchorData), nil
	}
	matches, err := filepath.Glob(filepath.Join(runDir, "results-*.jsonl"))
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("--anchor-run %s has no results-*.jsonl", runDir)
	}
	type resultRow struct {
		QuestionID         string `json:"question_id"`
		Conv               int    `json:"conv"`
		Q                  int    `json:"q"`
		AnswerContextTokens int   `json:"answer_context_tokens"`
	}
	anchor := &packAnchorData{PerQuestion: map[string]int{}}
	sum, count := 0.0, 0
	for _, path := range matches {
		f, err := os.Open(path) //nolint:gosec // operator-supplied run dir
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var row resultRow
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				f.Close() //nolint:errcheck
				return nil, fmt.Errorf("anchor %s: %w", path, err)
			}
			id := strings.TrimSpace(row.QuestionID)
			if id == "" {
				id = fmt.Sprintf("c%dq%d", row.Conv, row.Q)
			}
			if row.AnswerContextTokens > 0 {
				anchor.PerQuestion[id] = row.AnswerContextTokens
				sum += float64(row.AnswerContextTokens)
				count++
			}
		}
		f.Close() //nolint:errcheck
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	if count == 0 {
		return nil, fmt.Errorf("--anchor-run %s recorded no answer_context_tokens", runDir)
	}
	anchor.Mean = sum / float64(count)
	packAnchorCache.Store(runDir, anchor)
	return anchor, nil
}

// budgetFor resolves the per-question packing budget.
func (c packRunConfig) budgetFor(questionID string) int {
	if c.anchor == nil || len(c.anchor.PerQuestion) == 0 {
		return 0
	}
	if c.AnchorMode == "mean" {
		return int(c.anchor.Mean)
	}
	if v, ok := c.anchor.PerQuestion[strings.TrimSpace(questionID)]; ok && v > 0 {
		return v
	}
	return int(c.anchor.Mean)
}

// packAuditMu serializes the e2e packing audit sink (FR-011).
var packAuditMu sync.Mutex

func writePackAuditRow(runDir string, row aicAuditRow) {
	if strings.TrimSpace(runDir) == "" {
		return
	}
	packAuditMu.Lock()
	defer packAuditMu.Unlock()
	f, err := os.OpenFile(filepath.Join(runDir, "packing_audit.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // run-dir artifact
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	_ = json.NewEncoder(f).Encode(row)
}

// packWeightsForDigest returns the effective weights spec for protocol
// fingerprints — empty when packing is off so default-off digests stay
// byte-identical to the pre-045 shape (sealed-manifest compatibility).
func packWeightsForDigest(opt options) string {
	if !opt.submodularPack {
		return ""
	}
	spec := strings.TrimSpace(opt.packWeights)
	if spec == "" {
		return "3:1:1:1"
	}
	return spec
}

// retrieveCandidates is the retrieval dispatch for the answer path. Packing
// disabled → identical passthrough (byte-parity, T008 golden).
func retrieveCandidates(ctx context.Context, retriever *memory.Retriever, query string, topK, quota int, opt options, questionID string) ([]memory.Result, memory.SearchDiagnostics, error) {
	if !opt.submodularPack {
		return retrieveWithQuotaDiagnostics(ctx, retriever, query, topK, quota, nil)
	}
	cfg, err := packConfigForRun(opt)
	if err != nil {
		return nil, memory.SearchDiagnostics{}, err
	}
	pool := cfg.PoolSize
	if pool <= 0 {
		pool = topK * aicGatePoolFactor
		if pool < aicGatePoolMin {
			pool = aicGatePoolMin
		}
	}
	wide, diagnostics, err := retriever.SearchWithDiagnostics(ctx, query, pool)
	if err != nil {
		return nil, diagnostics, err
	}
	budget := cfg.budgetFor(questionID)
	if budget <= 0 {
		// Degenerate anchor: fail closed rather than pack unbounded.
		return nil, diagnostics, fmt.Errorf("submodular packing has no budget anchor for %s", questionID)
	}
	sel := packSelect(wide, query, budget, cfg.Weights)
	writePackAuditRow(opt.runDir, aicAuditRow{
		QuestionID:    questionID,
		Budget:        budget,
		PackedUsed:    sel.EstTokensUsed,
		Singleton:     sel.SingletonFallback,
		SelectedCount: len(sel.Selected),
		PoolSize:      len(wide),
	})
	return sel.Selected, diagnostics, nil
}
