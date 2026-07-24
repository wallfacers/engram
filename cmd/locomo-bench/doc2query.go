package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// Feature 012: doc2query pseudo-query shadow vectors on the adapter side.
//
// Architecture (decoupled build, contract A2): the expensive LLM query
// generation runs ONCE via --doc2query-build, baking pseudo-queries + #query
// shadow vectors into <run-dir>/doc2query-store. The gate arms then use THAT
// prebuilt store as --store-dir; each arm copies it run-local (方案 A two-store
// isolation) and either strips (baseline) or keeps (treatment) the #query
// vectors, so the only retrieval difference is the #query shadow. No LLM runs
// during the diagnostic/e2e arms (extractNever holds).

const (
	doc2queryOff         = "off"
	doc2queryBaseline    = "baseline"
	doc2queryTreatment   = "treatment"
	doc2queryTopK        = 30
	doc2queryTemperature = 0.2

	doc2queryStoreDirName = "doc2query-store"
	doc2queryBackfillFile = "doc2query_backfill.jsonl"
)

func doc2queryEnabled(opt options) bool {
	return opt.doc2query == doc2queryBaseline || opt.doc2query == doc2queryTreatment
}

func validateDoc2QueryOptions(opt options) error {
	switch opt.doc2query {
	case "", doc2queryOff:
	case doc2queryBaseline, doc2queryTreatment:
	default:
		return fmt.Errorf("--doc2query must be off, baseline, or treatment, got %q", opt.doc2query)
	}
	if opt.doc2queryBuild {
		if doc2queryEnabled(opt) {
			return fmt.Errorf("--doc2query-build cannot be combined with --doc2query baseline/treatment")
		}
		if aliasShadowEnabled(opt) {
			return fmt.Errorf("--doc2query-build cannot be combined with --alias-shadow")
		}
		if strings.TrimSpace(opt.storeDir) == "" {
			return fmt.Errorf("--doc2query-build requires --store-dir (canonical 009 store)")
		}
		if strings.TrimSpace(opt.runDir) == "" {
			return fmt.Errorf("--doc2query-build requires --run-dir")
		}
		return nil
	}
	if !doc2queryEnabled(opt) {
		return nil
	}
	if opt.opinionPass {
		return fmt.Errorf("--doc2query cannot be combined with --opinion-pass; gate arms must not call extraction")
	}
	if opt.conflictResolution {
		return fmt.Errorf("--doc2query cannot be combined with --conflict-resolution; gate arms must not call extraction")
	}
	if strings.TrimSpace(opt.retrieval) != "" {
		arms, err := armsFor(opt.retrieval)
		if err != nil {
			return err
		}
		for _, arm := range arms {
			spec, err := parseArm(arm)
			if err != nil {
				return err
			}
			if spec.mechanisms["conflict"] {
				return fmt.Errorf("--doc2query cannot use retrieval arm %q with conflict resolution; gate arms must not call extraction", arm)
			}
		}
	}
	if opt.topK != doc2queryTopK {
		return fmt.Errorf("--doc2query requires --top-k %d, got %d", doc2queryTopK, opt.topK)
	}
	for category, topK := range opt.catTopK {
		if topK != doc2queryTopK {
			return fmt.Errorf("--doc2query requires category %d top-k to remain %d, got %d", category, doc2queryTopK, topK)
		}
	}
	if opt.multiQuery {
		return fmt.Errorf("--doc2query and --multi-query are mutually exclusive")
	}
	if aliasShadowEnabled(opt) {
		return fmt.Errorf("--doc2query and --alias-shadow are mutually exclusive")
	}
	if strings.TrimSpace(opt.storeDir) == "" {
		return fmt.Errorf("--doc2query requires --store-dir (the prebuilt doc2query-store from --doc2query-build)")
	}
	if strings.TrimSpace(opt.runDir) == "" {
		return fmt.Errorf("--doc2query requires --run-dir for the isolated store copy")
	}
	return nil
}

// prepareDoc2QueryStore copies the prebuilt store (already carrying pseudo-query
// rows and #query vectors) before any runtime opens it, then redirects store
// access to the run-local copy. Mirrors prepareAliasShadowStore.
func prepareDoc2QueryStore(opt *options) error {
	if opt == nil || !doc2queryEnabled(*opt) || opt.doc2queryPrepared {
		return nil
	}
	source := opt.storeDir
	destination := filepath.Join(opt.runDir, doc2queryStoreDirName)
	if err := copyStoreDir(source, destination); err != nil {
		return fmt.Errorf("prepare doc2query store copy: %w", err)
	}
	opt.storeDir = destination
	opt.doc2queryPrepared = true
	opt.noIDKRetry = true
	return nil
}

// enforceDoc2QueryStoreMode strips (baseline) or asserts (treatment) the #query
// shadow vectors on the run-local store copy. Mirrors enforceAliasShadowStoreMode.
func doc2queryShadowCoverage(ctx context.Context, db *sql.DB, model string) (expected, actual int, err error) {
	if strings.TrimSpace(model) == "" {
		return 0, 0, fmt.Errorf("doc2query shadow coverage requires an embedding model")
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT entry_name) FROM memory_fact_queries`).Scan(&expected); err != nil {
		return 0, 0, fmt.Errorf("count facts with pseudo-queries: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM memory_embeddings AS embedding
		  JOIN (SELECT DISTINCT entry_name FROM memory_fact_queries) AS fact
		    ON embedding.entry_name = fact.entry_name || '#query'
		 WHERE embedding.model = ?`, model).Scan(&actual); err != nil {
		return 0, 0, fmt.Errorf("count current-model doc2query shadow vectors: %w", err)
	}
	return expected, actual, nil
}

func enforceDoc2QueryStoreMode(ctx context.Context, db *sql.DB, mode, model string) (int, error) {
	expected, current, err := doc2queryShadowCoverage(ctx, db, model)
	if err != nil {
		return 0, err
	}
	if expected == 0 {
		return 0, fmt.Errorf("doc2query store has no pseudo-query facts; use a --doc2query-build store")
	}
	if current != expected {
		return current, fmt.Errorf("doc2query current-model shadow coverage failed: count=%d, want %d (model=%s)", current, expected, model)
	}
	if mode == doc2queryBaseline {
		if _, err := db.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE entry_name LIKE '%#query'`); err != nil {
			return 0, fmt.Errorf("strip baseline doc2query shadow vectors: %w", err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name LIKE '%#query'`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count doc2query shadow vectors: %w", err)
	}
	switch mode {
	case doc2queryBaseline:
		if count != 0 {
			return count, fmt.Errorf("baseline doc2query invariant failed: count=%d, want 0", count)
		}
	case doc2queryTreatment:
		return current, nil
	}
	return count, nil
}

func goldFactNamesHaveQuery(ctx context.Context, db *sql.DB, names []string) (bool, error) {
	for _, name := range names {
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT 1 FROM memory_fact_queries WHERE entry_name = ? LIMIT 1`, name).Scan(&exists)
		if err == nil {
			return true, nil
		}
		if err != sql.ErrNoRows {
			return false, fmt.Errorf("query gold pseudo-queries for %q: %w", name, err)
		}
	}
	return false, nil
}

type doc2queryRecallQuestion struct {
	Conv              int     `json:"conv"`
	Q                 int     `json:"q"`
	Category          int     `json:"category"`
	QuestionID        string  `json:"question_id,omitempty"`
	Arm               string  `json:"arm"`
	GoldHasQuery      bool    `json:"gold_has_query"`
	GoldResolved      bool    `json:"gold_resolved"`
	GoldRank          int     `json:"gold_rank"`
	GoldRankAt30      int     `json:"gold_rank_at_30"`
	Gradeable         bool    `json:"gradeable"`
	CoverageAt30      float64 `json:"coverage_at_30"`
	SessionCoverage30 float64 `json:"session_coverage_at_30"`
}

type doc2queryRecallLayer struct {
	Questions                    int     `json:"questions"`
	GoldResolved                 int     `json:"gold_resolved"`
	GoldRankComparable           int     `json:"gold_rank_comparable"`
	Gradeable                    int     `json:"gradeable"`
	BaselineMeanGoldRank         float64 `json:"baseline_mean_gold_rank"`
	TreatmentMeanGoldRank        float64 `json:"treatment_mean_gold_rank"`
	MeanGoldRankDelta            float64 `json:"mean_gold_rank_delta"`
	BaselineMeanGoldRankAt30     float64 `json:"baseline_mean_gold_rank_at_30"`
	TreatmentMeanGoldRankAt30    float64 `json:"treatment_mean_gold_rank_at_30"`
	MeanGoldRankAt30Delta        float64 `json:"mean_gold_rank_at_30_delta"`
	GoldEnteredTop30             int     `json:"gold_entered_top_30"`
	GoldLeftTop30                int     `json:"gold_left_top_30"`
	BaselineMeanCoverageAt30     float64 `json:"baseline_mean_coverage_at_30"`
	TreatmentMeanCoverageAt30    float64 `json:"treatment_mean_coverage_at_30"`
	MeanCoverageAt30Delta        float64 `json:"mean_coverage_at_30_delta"`
	BaselineMeanSessionCoverage  float64 `json:"baseline_mean_session_coverage_at_30"`
	TreatmentMeanSessionCoverage float64 `json:"treatment_mean_session_coverage_at_30"`
}

type doc2queryRecallReport struct {
	Mode            string               `json:"mode"`
	TargetCategory  string               `json:"target_category"`
	TopK            int                  `json:"top_k"`
	DeltaConvention string               `json:"delta_convention"`
	Global          doc2queryRecallLayer `json:"global"`
	GoldHasQuery    doc2queryRecallLayer `json:"gold_has_query"`
}

type doc2queryRecallPair struct {
	baseline  doc2queryRecallQuestion
	treatment doc2queryRecallQuestion
}

func summarizeDoc2QueryRecallDiagnostic(baseline, treatment []doc2queryRecallQuestion, targetCategory int) (doc2queryRecallReport, error) {
	report := doc2queryRecallReport{
		Mode:            "retrieval_only",
		TargetCategory:  categoryLabel(targetCategory),
		TopK:            doc2queryTopK,
		DeltaConvention: "rank_delta=treatment-baseline (negative improves); coverage_delta=treatment-baseline (positive improves)",
	}
	pairs, err := pairDoc2QueryRecallQuestions(baseline, treatment)
	if err != nil {
		return report, err
	}
	report.Global = summarizeDoc2QueryRecallLayer(pairs)
	withQuery := make([]doc2queryRecallPair, 0, len(pairs))
	for _, pair := range pairs {
		if pair.baseline.GoldHasQuery {
			withQuery = append(withQuery, pair)
		}
	}
	report.GoldHasQuery = summarizeDoc2QueryRecallLayer(withQuery)
	return report, nil
}

func pairDoc2QueryRecallQuestions(baseline, treatment []doc2queryRecallQuestion) ([]doc2queryRecallPair, error) {
	type key struct{ conv, q int }
	baselineByKey := make(map[key]doc2queryRecallQuestion, len(baseline))
	for _, question := range baseline {
		questionKey := key{question.Conv, question.Q}
		if _, duplicate := baselineByKey[questionKey]; duplicate {
			return nil, fmt.Errorf("duplicate baseline recall record: conv=%d q=%d", question.Conv, question.Q)
		}
		baselineByKey[questionKey] = question
	}
	pairs := make([]doc2queryRecallPair, 0, len(treatment))
	seenTreatment := make(map[key]struct{}, len(treatment))
	for _, question := range treatment {
		questionKey := key{question.Conv, question.Q}
		base, ok := baselineByKey[questionKey]
		if !ok {
			return nil, fmt.Errorf("treatment recall record has no baseline pair: conv=%d q=%d", question.Conv, question.Q)
		}
		if _, duplicate := seenTreatment[questionKey]; duplicate {
			return nil, fmt.Errorf("duplicate treatment recall record: conv=%d q=%d", question.Conv, question.Q)
		}
		if base.Category != question.Category || base.QuestionID != question.QuestionID {
			return nil, fmt.Errorf("baseline/treatment question mismatch: conv=%d q=%d", question.Conv, question.Q)
		}
		if base.GoldHasQuery != question.GoldHasQuery {
			return nil, fmt.Errorf("gold_has_query mismatch: conv=%d q=%d baseline=%t treatment=%t", question.Conv, question.Q, base.GoldHasQuery, question.GoldHasQuery)
		}
		seenTreatment[questionKey] = struct{}{}
		pairs = append(pairs, doc2queryRecallPair{baseline: base, treatment: question})
	}
	if len(seenTreatment) != len(baselineByKey) {
		return nil, fmt.Errorf("baseline/treatment recall records are not a complete pair: baseline=%d treatment=%d", len(baselineByKey), len(seenTreatment))
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].baseline.Conv != pairs[j].baseline.Conv {
			return pairs[i].baseline.Conv < pairs[j].baseline.Conv
		}
		return pairs[i].baseline.Q < pairs[j].baseline.Q
	})
	return pairs, nil
}

func summarizeDoc2QueryRecallLayer(pairs []doc2queryRecallPair) doc2queryRecallLayer {
	layer := doc2queryRecallLayer{Questions: len(pairs)}
	var baselineRankSum, treatmentRankSum float64
	var baselineRankAt30Sum, treatmentRankAt30Sum float64
	var baselineCoverageSum, treatmentCoverageSum float64
	var baselineSessionSum, treatmentSessionSum float64
	for _, pair := range pairs {
		baseline, treatment := pair.baseline, pair.treatment
		if baseline.GoldResolved && treatment.GoldResolved {
			layer.GoldResolved++
			baselineRankAt30Sum += float64(diagnosticRank(baseline.GoldRankAt30))
			treatmentRankAt30Sum += float64(diagnosticRank(treatment.GoldRankAt30))
			if baseline.GoldRankAt30 < 1 && treatment.GoldRankAt30 > 0 {
				layer.GoldEnteredTop30++
			}
			if baseline.GoldRankAt30 > 0 && treatment.GoldRankAt30 < 1 {
				layer.GoldLeftTop30++
			}
		}
		if baseline.GoldRank > 0 && treatment.GoldRank > 0 {
			layer.GoldRankComparable++
			baselineRankSum += float64(baseline.GoldRank)
			treatmentRankSum += float64(treatment.GoldRank)
		}
		if baseline.Gradeable && treatment.Gradeable {
			layer.Gradeable++
			baselineCoverageSum += baseline.CoverageAt30
			treatmentCoverageSum += treatment.CoverageAt30
			baselineSessionSum += baseline.SessionCoverage30
			treatmentSessionSum += treatment.SessionCoverage30
		}
	}
	if layer.GoldRankComparable > 0 {
		denominator := float64(layer.GoldRankComparable)
		layer.BaselineMeanGoldRank = baselineRankSum / denominator
		layer.TreatmentMeanGoldRank = treatmentRankSum / denominator
		layer.MeanGoldRankDelta = layer.TreatmentMeanGoldRank - layer.BaselineMeanGoldRank
	}
	if layer.GoldResolved > 0 {
		denominator := float64(layer.GoldResolved)
		layer.BaselineMeanGoldRankAt30 = baselineRankAt30Sum / denominator
		layer.TreatmentMeanGoldRankAt30 = treatmentRankAt30Sum / denominator
		layer.MeanGoldRankAt30Delta = layer.TreatmentMeanGoldRankAt30 - layer.BaselineMeanGoldRankAt30
	}
	if layer.Gradeable > 0 {
		denominator := float64(layer.Gradeable)
		layer.BaselineMeanCoverageAt30 = baselineCoverageSum / denominator
		layer.TreatmentMeanCoverageAt30 = treatmentCoverageSum / denominator
		layer.MeanCoverageAt30Delta = layer.TreatmentMeanCoverageAt30 - layer.BaselineMeanCoverageAt30
		layer.BaselineMeanSessionCoverage = baselineSessionSum / denominator
		layer.TreatmentMeanSessionCoverage = treatmentSessionSum / denominator
	}
	return layer
}

func validateDoc2QueryContextParity(opt options, record contextParityRecord) error {
	if !doc2queryEnabled(opt) {
		return nil
	}
	if record.FinalTopK != doc2queryTopK {
		return fmt.Errorf("doc2query context parity failed for conv=%d q=%d arm=%s: final_top_k=%d, want %d", record.Conv, record.Q, record.Arm, record.FinalTopK, doc2queryTopK)
	}
	if record.SubqueryCount != 1 {
		return fmt.Errorf("doc2query context parity failed for conv=%d q=%d arm=%s: subquery_count=%d, want 1", record.Conv, record.Q, record.Arm, record.SubqueryCount)
	}
	return nil
}

// --- Build mode: one-time LLM pseudo-query generation + #query embedding ---

const doc2queryGenSystemPrompt = `You generate the questions a memory fact directly answers, for a
retrieval index. Given ONE self-contained fact, output 2-3 SHORT, natural
questions a user might ask that this fact answers. Each question must be
answerable by the fact alone. Vary phrasing (who/when/what/where). Return
STRICT JSON: {"queries":["...","..."]}. No prose.`

func buildDoc2QueryUserPrompt(fact string) string {
	return "FACT: " + oneLineFact(fact) + "\nReturn the JSON now."
}

func oneLineFact(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type doc2queryGenResult struct {
	Queries []string `json:"queries"`
}

func parseDoc2QueryResponse(raw string) []string {
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end < 0 || end < start {
		return nil
	}
	var result doc2queryGenResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return nil
	}
	cleaned := make([]string, 0, len(result.Queries))
	seen := make(map[string]struct{}, len(result.Queries))
	for _, q := range result.Queries {
		q = strings.TrimSpace(q)
		key := strings.ToLower(q)
		if q != "" {
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			cleaned = append(cleaned, q)
		}
		if len(cleaned) == 3 {
			break
		}
	}
	if len(cleaned) < 2 {
		return nil
	}
	return cleaned
}

type doc2queryBackfillRecord struct {
	Conv    int      `json:"conv"`
	Name    string   `json:"name"`
	Queries []string `json:"queries"`
}

func writeDoc2QueryBackfillRecords(path string, records []doc2queryBackfillRecord) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	writer := bufio.NewWriter(tmp)
	encoder := json.NewEncoder(writer)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

// runDoc2QueryBuild copies the canonical store, generates 2-3 pseudo-queries per
// extraction fact via the answer/extract model, persists them, and embeds the
// #query shadow vectors. Paid once; the gate arms reuse the resulting store.
func runDoc2QueryBuild(ctx context.Context, opt options, convs []conversation, genCall modelCaller, embClient embedding.Client, logger *slog.Logger) error {
	if strings.TrimSpace(opt.storeDir) == "" {
		return fmt.Errorf("--doc2query-build requires --store-dir (canonical 009 store)")
	}
	if strings.TrimSpace(opt.runDir) == "" {
		return fmt.Errorf("--doc2query-build requires --run-dir")
	}
	if genCall == nil {
		return fmt.Errorf("--doc2query-build requires an LLM caller for query generation")
	}
	if embClient == nil {
		return fmt.Errorf("--doc2query-build requires an embedding client to build #query shadow vectors")
	}
	destination := filepath.Join(opt.runDir, doc2queryStoreDirName)
	if err := copyStoreDir(opt.storeDir, destination); err != nil {
		return fmt.Errorf("copy canonical store for doc2query build: %w", err)
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create doc2query build run dir: %w", err)
	}

	concurrency := opt.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	backfillPath := filepath.Join(opt.runDir, doc2queryBackfillFile)
	records := make([]doc2queryBackfillRecord, 0)
	var recordsMu sync.Mutex
	var totalFacts, totalQueries atomic.Int64
	for _, conv := range convs {
		dsn := filepath.Join(destination, fmt.Sprintf("conv%d.db", conv.ID))
		if _, err := os.Stat(dsn); err != nil {
			return fmt.Errorf("locate doc2query build store conv=%d: %w", conv.ID, err)
		}
		st, err := store.Open(ctx, store.Options{DSN: dsn})
		if err != nil {
			return fmt.Errorf("open doc2query build store conv=%d: %w", conv.ID, err)
		}
		es := memory.NewEntryStore(st.DB())
		vectors := memory.NewVectorStore(st.DB())

		entries, err := es.List(ctx)
		if err != nil {
			_ = st.Close()
			return fmt.Errorf("list facts conv=%d: %w", conv.ID, err)
		}
		facts := make([]memory.Entry, 0, len(entries))
		for _, entry := range entries {
			if entry.FactSource != "extraction" {
				continue
			}
			existing, err := es.FactQueries(ctx, entry.Name)
			if err != nil {
				_ = st.Close()
				return fmt.Errorf("check existing queries conv=%d name=%q: %w", conv.ID, entry.Name, err)
			}
			if len(existing) > 0 {
				continue
			}
			facts = append(facts, *entry)
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var firstErr error
		genCtx, cancel := context.WithCancel(ctx)
		for _, fact := range facts {
			fact := fact
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-genCtx.Done():
					return
				}
				defer func() { <-sem }()
				if genCtx.Err() != nil {
					return
				}
				raw, err := genCall(genCtx, doc2queryGenSystemPrompt, buildDoc2QueryUserPrompt(fact.Content))
				if err != nil {
					logger.Warn("doc2query gen call failed; skipping fact", "conversation", conv.ID, "name", fact.Name, "err", err)
					return
				}
				queries := parseDoc2QueryResponse(raw)
				if len(queries) == 0 {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if err := es.PutFactQueries(genCtx, fact.Name, queries); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("put queries conv=%d name=%q: %w", conv.ID, fact.Name, err)
						cancel()
					}
					return
				}
				totalFacts.Add(1)
				totalQueries.Add(int64(len(queries)))
				recordsMu.Lock()
				records = append(records, doc2queryBackfillRecord{Conv: conv.ID, Name: fact.Name, Queries: queries})
				recordsMu.Unlock()
			}()
		}
		wg.Wait()
		cancel()
		if firstErr != nil {
			_ = st.Close()
			return firstErr
		}
		if err := ctx.Err(); err != nil {
			_ = st.Close()
			return err
		}
		embedBuffer := len(entries) * 3
		if embedBuffer < memory.DefaultEmbedBuffer {
			embedBuffer = memory.DefaultEmbedBuffer
		}
		embedder := memory.NewEmbedder(es, vectors, embClient, embedBuffer)
		// Embed the #query shadow vectors for the freshly written pseudo-queries.
		if err := embedder.Backfill(ctx); err != nil {
			embedder.Close()
			_ = st.Close()
			return fmt.Errorf("embed #query shadows conv=%d: %w", conv.ID, err)
		}
		embedder.Close()
		expectedShadows, shadowCount, err := doc2queryShadowCoverage(ctx, st.DB(), embClient.Model())
		if err != nil {
			_ = st.Close()
			return fmt.Errorf("verify #query shadows conv=%d: %w", conv.ID, err)
		}
		if shadowCount != expectedShadows {
			_ = st.Close()
			return fmt.Errorf("verify #query shadows conv=%d: current-model count=%d, want %d", conv.ID, shadowCount, expectedShadows)
		}
		logger.Info("doc2query build conversation done", "conversation", conv.ID, "facts", len(facts), "shadow_vectors", shadowCount)
		if err := st.Close(); err != nil {
			return fmt.Errorf("close doc2query build store conv=%d: %w", conv.ID, err)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Conv != records[j].Conv {
			return records[i].Conv < records[j].Conv
		}
		return records[i].Name < records[j].Name
	})
	if err := writeDoc2QueryBackfillRecords(backfillPath, records); err != nil {
		return fmt.Errorf("write doc2query backfill log: %w", err)
	}
	logger.Info("doc2query build complete", "store", destination, "facts_with_queries", totalFacts.Load(), "total_queries", totalQueries.Load(), "backfill_log", backfillPath)
	fmt.Printf("doc2query build: store=%s facts_with_queries=%d total_queries=%d\n", destination, totalFacts.Load(), totalQueries.Load())
	return nil
}

// --- Recall diagnostic (門②): retrieval-only, no LLM ---

func doc2queryRecallArmPath(runDir, arm string) string {
	return filepath.Join(runDir, "doc2query_recall_"+arm+".jsonl")
}

func runDoc2QueryRecallDiagnosticWithClient(ctx context.Context, opt options, convs []conversation, arms []string, embClient embedding.Client, logger *slog.Logger) error {
	if err := validateRecallDiagnosticOptions(opt, arms); err != nil {
		return err
	}
	if !doc2queryEnabled(opt) {
		return fmt.Errorf("doc2query recall diagnostic requires --doc2query baseline or treatment")
	}
	if embClient == nil {
		return fmt.Errorf("doc2query recall diagnostic requires a configured embedding client")
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create doc2query recall run dir: %w", err)
	}

	targetOpt := opt
	if targetOpt.onlyCategory == 0 {
		targetOpt.onlyCategory = 1
	}
	diagnosticCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	concurrency := targetOpt.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var extractCalls atomic.Int32
	questions := make([]doc2queryRecallQuestion, 0)
	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	extractNever := func(context.Context, string, string) (string, error) {
		extractCalls.Add(1)
		return "", fmt.Errorf("doc2query diagnostic must not call extraction")
	}
	for _, conv := range convs {
		conv := conv
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if diagnosticCtx.Err() != nil {
				return
			}
			runtime, err := buildConversationRuntime(diagnosticCtx, targetOpt, conv, extractNever, embClient, arms, logger)
			if err != nil {
				setErr(fmt.Errorf("prepare doc2query diagnostic conv=%d: %w", conv.ID, err))
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[arms[0]]
			goldTurnText := turnTextIndex(conv)
			storedFacts, err := attributionStoredFacts(diagnosticCtx, runtime)
			if err != nil {
				setErr(fmt.Errorf("load diagnostic facts conv=%d: %w", conv.ID, err))
				return
			}
			for _, selected := range selectQuestions(conv, targetOpt) {
				if diagnosticCtx.Err() != nil {
					return
				}
				qa := selected.QA
				armOpt := optionsForRun(targetOpt, arms[0], false)
				topK, quota := armOpt.retrievalFor(qa.Category)
				searchK := questionSearchK(topK, quota)
				candidates, err := retriever.Search(diagnosticCtx, qa.Question, searchK)
				if err != nil {
					setErr(fmt.Errorf("doc2query diagnostic retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				hits := finalizeQuestionHits(diagnosticCtx, qa.Question, candidates, topK, quota, armOpt)
				wide := candidates
				wideK := attributionWidePool(topK, armOpt.widePool)
				if wideK != searchK {
					wide, err = retriever.Search(diagnosticCtx, qa.Question, wideK)
					if err != nil {
						setErr(fmt.Errorf("doc2query diagnostic wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
				}
				trace := buildAttributionTrace(conv.ID, selected.Index, qa, hits, wide, runtime.chunkTurns, goldTurnText, topK, 0, targetOpt.factCoverageTau, nil)
				coverage, sessionCoverage, gradeable := evidenceRecallAt(qa, hits, runtime.chunkTurns)
				goldFactNames := attributedGoldFactNames(qa, storedFacts, runtime.chunkTurns, goldTurnText, targetOpt.factCoverageTau)
				goldHasQuery, err := goldFactNamesHaveQuery(diagnosticCtx, runtime.store.DB(), goldFactNames)
				if err != nil {
					setErr(fmt.Errorf("classify gold queries conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				record := doc2queryRecallQuestion{
					Conv:              conv.ID,
					Q:                 selected.Index,
					Category:          qa.Category,
					QuestionID:        qa.QuestionID,
					Arm:               targetOpt.doc2query,
					GoldHasQuery:      goldHasQuery,
					GoldResolved:      len(trace.GoldTurns) > 0,
					GoldRank:          trace.GoldRankPool,
					GoldRankAt30:      trace.GoldRankTopK,
					Gradeable:         gradeable,
					CoverageAt30:      coverage,
					SessionCoverage30: sessionCoverage,
				}
				mu.Lock()
				questions = append(questions, record)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if got := extractCalls.Load(); got != 0 {
		return fmt.Errorf("doc2query diagnostic extraction calls=%d, want 0", got)
	}
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Conv != questions[j].Conv {
			return questions[i].Conv < questions[j].Conv
		}
		return questions[i].Q < questions[j].Q
	})

	armPath := doc2queryRecallArmPath(opt.runDir, opt.doc2query)
	if opt.doc2query == doc2queryBaseline {
		for _, stale := range []string{
			doc2queryRecallArmPath(opt.runDir, doc2queryTreatment),
			filepath.Join(opt.runDir, "doc2query_recall.json"),
		} {
			if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale doc2query recall artifact %s: %w", stale, err)
			}
		}
	}
	if err := writeDoc2QueryRecallQuestions(armPath, questions); err != nil {
		return fmt.Errorf("write %s doc2query recall records: %w", opt.doc2query, err)
	}
	if opt.doc2query == doc2queryTreatment {
		baseline, err := readDoc2QueryRecallQuestions(doc2queryRecallArmPath(opt.runDir, doc2queryBaseline))
		if err != nil {
			return fmt.Errorf("read baseline doc2query recall records (run baseline first): %w", err)
		}
		report, err := summarizeDoc2QueryRecallDiagnostic(baseline, questions, targetOpt.onlyCategory)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(opt.runDir, "doc2query_recall.json"), report); err != nil {
			return fmt.Errorf("write doc2query recall report: %w", err)
		}
		fmt.Printf("doc2query recall: category=%s global_n=%d query_n=%d query coverage@30 delta=%+.3f, gold entered=%d left=%d\n",
			report.TargetCategory, report.Global.Questions, report.GoldHasQuery.Questions, report.GoldHasQuery.MeanCoverageAt30Delta,
			report.GoldHasQuery.GoldEnteredTop30, report.GoldHasQuery.GoldLeftTop30)
	}
	logger.Info("doc2query recall diagnostic complete", "arm", opt.doc2query, "questions", len(questions), "extract_callers", 0, "answer_callers", 0, "judge_callers", 0)
	return nil
}

func writeDoc2QueryRecallQuestions(path string, questions []doc2queryRecallQuestion) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	writer := bufio.NewWriter(tmp)
	encoder := json.NewEncoder(writer)
	for _, question := range questions {
		if err := encoder.Encode(question); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func readDoc2QueryRecallQuestions(path string) ([]doc2queryRecallQuestion, error) {
	file, err := os.Open(path) //nolint:gosec // run-local benchmark artifact
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck
	decoder := json.NewDecoder(file)
	var questions []doc2queryRecallQuestion
	for decoder.More() {
		var question doc2queryRecallQuestion
		if err := decoder.Decode(&question); err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	return questions, nil
}
