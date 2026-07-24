package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
)

const multiQueryFinalTopK = 30

type queryRetrievalMeta struct {
	finalTopK     int
	subqueryCount int
}

func multiQueryArm(enabled bool) string {
	if enabled {
		return "multi"
	}
	return "single"
}

func multiQueryRecipeFingerprint(opt options) string {
	return fmt.Sprintf(
		"multi_query=true;mq_max_subqueries=%d;top_k=%d;chunks=%t;chunk_quota=%d;cat_top_k=%s;cat_chunk_quota=%s",
		opt.mqMaxSubqueries,
		opt.topK,
		opt.chunks,
		opt.chunkQuota,
		formatCategoryOverrides(opt.catTopK),
		formatCategoryOverrides(opt.catQuota),
	)
}

func formatCategoryOverrides(overrides map[int]int) string {
	if len(overrides) == 0 {
		return "-"
	}
	categories := make([]int, 0, len(overrides))
	for category := range overrides {
		categories = append(categories, category)
	}
	sort.Ints(categories)
	parts := make([]string, 0, len(categories))
	for _, category := range categories {
		parts = append(parts, fmt.Sprintf("%d=%d", category, overrides[category]))
	}
	return strings.Join(parts, ",")
}

// unionRetryResults preserves the legacy append-only union when limit is zero.
// Multi-query retries use a bounded half-old/half-new context because scores
// from SearchMulti and a rewritten single query are not directly comparable.
func unionRetryResults(first, more []memory.Result, limit int) ([]memory.Result, int) {
	seen := make(map[string]struct{}, len(first)+len(more))
	firstNames := make(map[string]struct{}, len(first))
	for _, hit := range first {
		firstNames[hit.Name] = struct{}{}
	}
	if limit <= 0 {
		union := make([]memory.Result, 0, len(first)+len(more))
		for _, hit := range first {
			seen[hit.Name] = struct{}{}
			union = append(union, hit)
		}
		fresh := 0
		for _, hit := range more {
			if _, duplicate := seen[hit.Name]; duplicate {
				continue
			}
			union = append(union, hit)
			fresh++
		}
		return union, fresh
	}

	union := make([]memory.Result, 0, limit)
	firstSlots := (limit + 1) / 2
	for _, hit := range first {
		if len(union) >= firstSlots {
			break
		}
		if _, duplicate := seen[hit.Name]; duplicate {
			continue
		}
		seen[hit.Name] = struct{}{}
		union = append(union, hit)
	}
	fresh := 0
	for _, hit := range more {
		if len(union) >= limit {
			break
		}
		if _, existing := firstNames[hit.Name]; existing {
			continue
		}
		if _, duplicate := seen[hit.Name]; duplicate {
			continue
		}
		seen[hit.Name] = struct{}{}
		union = append(union, hit)
		fresh++
	}
	for _, hit := range first {
		if len(union) >= limit {
			break
		}
		if _, duplicate := seen[hit.Name]; duplicate {
			continue
		}
		seen[hit.Name] = struct{}{}
		union = append(union, hit)
	}
	return union, fresh
}

func unionMultiRetryResults(first, more []memory.Result, topK, quota int) ([]memory.Result, int) {
	if quota <= 0 {
		return unionRetryResults(first, more, topK)
	}
	partition := func(hits []memory.Result, chunks bool) []memory.Result {
		out := make([]memory.Result, 0, len(hits))
		for _, hit := range hits {
			if strings.HasPrefix(hit.Name, "chunk-") == chunks {
				out = append(out, hit)
			}
		}
		return out
	}

	factSlots := topK - quota
	selectedFacts, _ := unionRetryResults(partition(first, false), partition(more, false), factSlots)
	selectedChunks, _ := unionRetryResults(partition(first, true), partition(more, true), quota)
	selected := make(map[string]struct{}, len(selectedFacts)+len(selectedChunks))
	for _, hit := range selectedFacts {
		selected[hit.Name] = struct{}{}
	}
	for _, hit := range selectedChunks {
		selected[hit.Name] = struct{}{}
	}

	firstNames := make(map[string]struct{}, len(first))
	for _, hit := range first {
		firstNames[hit.Name] = struct{}{}
	}
	union := make([]memory.Result, 0, topK)
	seen := make(map[string]struct{}, topK)
	appendSelected := func(hits []memory.Result, selectedOnly bool) {
		for _, hit := range hits {
			if len(union) >= topK {
				return
			}
			if selectedOnly {
				if _, ok := selected[hit.Name]; !ok {
					continue
				}
			}
			if _, duplicate := seen[hit.Name]; duplicate {
				continue
			}
			seen[hit.Name] = struct{}{}
			union = append(union, hit)
		}
	}
	appendSelected(first, true)
	appendSelected(more, true)
	if len(union) < topK {
		// Backfill when the store does not contain enough facts or chunks.
		appendSelected(first, false)
		appendSelected(more, false)
	}
	fresh := 0
	for _, hit := range union {
		if _, existed := firstNames[hit.Name]; !existed {
			fresh++
		}
	}
	return union, fresh
}

// retrieveQuestionWithDiagnostics is the initial per-question retrieval seam.
// The disabled branch deliberately delegates to the historical front door
// unchanged; the enabled branch keeps SearchMulti's final budget at topK.
func retrieveQuestionWithDiagnostics(ctx context.Context, retriever *memory.Retriever, filterCall, decomposeCall modelCaller, question string, topK, quota int, opt options) ([]memory.Result, memory.SearchDiagnostics, queryRetrievalMeta, error) {
	meta := queryRetrievalMeta{finalTopK: topK, subqueryCount: 1}
	if !opt.multiQuery {
		hits, diagnostics, err := retrieveWithDiagnostics(ctx, retriever, filterCall, question, topK, quota, opt)
		meta.finalTopK = len(hits)
		return hits, diagnostics, meta, err
	}

	subqueries := decomposeQuery(ctx, decomposeCall, question, opt.mqMaxSubqueries)
	if len(subqueries) == 0 || len(subqueries) > opt.mqMaxSubqueries {
		subqueries = []string{question}
	}
	meta.subqueryCount = len(subqueries)
	searchK := questionSearchK(topK, quota)
	hits, err := retriever.SearchMulti(ctx, subqueries, searchK)
	if err != nil {
		meta.finalTopK = 0
		return nil, memory.SearchDiagnostics{}, meta, err
	}
	hits = finalizeQuestionHits(ctx, question, hits, topK, quota, opt)
	meta.finalTopK = len(hits)
	return hits, memory.SearchDiagnostics{}, meta, nil
}

func questionSearchK(topK, quota int) int {
	if quota <= 0 {
		return topK
	}
	searchK := topK * 6
	if searchK < 300 {
		searchK = 300
	}
	return searchK
}

func finalizeQuestionHits(ctx context.Context, question string, hits []memory.Result, topK, quota int, opt options) []memory.Result {
	if quota > 0 {
		if opt.selector != nil {
			hits = applyChunkSelector(ctx, question, hits, quota, opt.selector)
		}
		return applyChunkQuota(hits, topK, quota)
	}
	if len(hits) > topK {
		return hits[:topK]
	}
	return hits
}

type contextParityRecord struct {
	Conv                int    `json:"conv"`
	Q                   int    `json:"q"`
	Category            int    `json:"category"`
	Arm                 string `json:"arm"`
	FinalTopK           int    `json:"final_top_k"`
	AnswerContextTokens int    `json:"answer_context_tokens"`
	SubqueryCount       int    `json:"subquery_count"`
}

// contextParityJournal flushes every completed question so an interrupted run
// retains the same accounting evidence as its result journals.
type contextParityJournal struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	seen map[contextParityKey]struct{}
	err  error
}

type contextParityKey struct {
	conv int
	q    int
	arm  string
}

func (record contextParityRecord) key() contextParityKey {
	return contextParityKey{conv: record.Conv, q: record.Q, arm: record.Arm}
}

func openContextParityJournal(runDir string) (*contextParityJournal, error) {
	path := filepath.Join(runDir, "context_parity.jsonl")
	seen := make(map[contextParityKey]struct{})
	prior, err := os.ReadFile(path) //nolint:gosec // operator-selected run artifact
	if err == nil {
		if len(prior) > 0 && prior[len(prior)-1] != '\n' {
			truncateAt := bytes.LastIndexByte(prior, '\n') + 1
			if err := os.Truncate(path, int64(truncateAt)); err != nil {
				return nil, fmt.Errorf("truncate partial context parity record: %w", err)
			}
			prior = prior[:truncateAt]
		}
		scanner := bufio.NewScanner(bytes.NewReader(prior))
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var record contextParityRecord
			if json.Unmarshal(scanner.Bytes(), &record) == nil {
				seen[record.key()] = struct{}{}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan context parity journal: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read context parity journal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open context parity journal: %w", err)
	}
	return &contextParityJournal{f: f, w: bufio.NewWriter(f), seen: seen}, nil
}

func (j *contextParityJournal) Has(conv, q int, arm string) bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, ok := j.seen[contextParityKey{conv: conv, q: q, arm: arm}]
	return ok
}

func validateContextParityResume(opt options, convs []conversation, states []*armState) error {
	if opt.contextParity == nil || len(states) == 0 {
		return nil
	}
	parityState := states[len(states)-1]
	parityOpt := optionsForRun(opt, parityState.name, len(states) > 1)
	arm := contextParityArm(parityOpt)
	for _, conv := range convs {
		for _, selected := range selectQuestions(conv, parityOpt) {
			key := resultKey{Conv: conv.ID, Q: selected.Index}
			_, hasResult := parityState.journal.lookup(key)
			hasParity := opt.contextParity.Has(key.Conv, key.Q, arm)
			if hasResult != hasParity {
				return fmt.Errorf("context parity/result resume mismatch for %s conv=%d q=%d; use a fresh --run-dir", arm, key.Conv, key.Q)
			}
		}
	}
	return nil
}

func (j *contextParityJournal) Write(record contextParityRecord) error {
	if j == nil {
		return nil
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return j.err
	}
	if _, exists := j.seen[record.key()]; exists {
		return nil
	}
	if _, err := j.w.Write(line); err != nil {
		j.err = err
		return err
	}
	if err := j.w.WriteByte('\n'); err != nil {
		j.err = err
		return err
	}
	if err := j.w.Flush(); err != nil {
		j.err = err
		return err
	}
	j.seen[record.key()] = struct{}{}
	return nil
}

func (j *contextParityJournal) Fail(err error) {
	if j == nil || err == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err == nil {
		j.err = err
	}
}

func (j *contextParityJournal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.w.Flush(); j.err == nil && err != nil {
		j.err = err
	}
	if err := j.f.Close(); j.err == nil && err != nil {
		j.err = err
	}
	if j.err != nil {
		return fmt.Errorf("write context_parity.jsonl: %w", j.err)
	}
	return nil
}

type recallDiagnosticQuestion struct {
	Conv                      int     `json:"conv"`
	Q                         int     `json:"q"`
	Category                  int     `json:"category"`
	QuestionID                string  `json:"question_id,omitempty"`
	SubqueryCount             int     `json:"subquery_count"`
	GoldResolved              bool    `json:"gold_resolved"`
	SingleGoldRank            int     `json:"single_gold_rank"`
	MultiGoldRank             int     `json:"multi_gold_rank"`
	GoldRankDelta             *int    `json:"gold_rank_delta,omitempty"`
	SingleGoldRankAt30        int     `json:"single_gold_rank_at_30"`
	MultiGoldRankAt30         int     `json:"multi_gold_rank_at_30"`
	GoldRankAt30Delta         int     `json:"gold_rank_at_30_delta"`
	Gradeable                 bool    `json:"gradeable"`
	SingleCoverageAt30        float64 `json:"single_coverage_at_30"`
	MultiCoverageAt30         float64 `json:"multi_coverage_at_30"`
	CoverageAt30Delta         float64 `json:"coverage_at_30_delta"`
	SingleSessionCoverageAt30 float64 `json:"single_session_coverage_at_30"`
	MultiSessionCoverageAt30  float64 `json:"multi_session_coverage_at_30"`
}

type recallDiagnosticReport struct {
	Mode                      string  `json:"mode"`
	TargetCategory            string  `json:"target_category"`
	TopK                      int     `json:"top_k"`
	Questions                 int     `json:"questions"`
	GoldResolved              int     `json:"gold_resolved"`
	GoldRankComparable        int     `json:"gold_rank_comparable"`
	Gradeable                 int     `json:"gradeable"`
	SingleMeanGoldRank        float64 `json:"single_mean_gold_rank"`
	MultiMeanGoldRank         float64 `json:"multi_mean_gold_rank"`
	MeanGoldRankDelta         float64 `json:"mean_gold_rank_delta"`
	SingleMeanGoldRankAt30    float64 `json:"single_mean_gold_rank_at_30"`
	MultiMeanGoldRankAt30     float64 `json:"multi_mean_gold_rank_at_30"`
	MeanGoldRankAt30Delta     float64 `json:"mean_gold_rank_at_30_delta"`
	GoldEnteredTop30          int     `json:"gold_entered_top_30"`
	GoldLeftTop30             int     `json:"gold_left_top_30"`
	SingleMeanCoverageAt30    float64 `json:"single_mean_coverage_at_30"`
	MultiMeanCoverageAt30     float64 `json:"multi_mean_coverage_at_30"`
	MeanCoverageAt30Delta     float64 `json:"mean_coverage_at_30_delta"`
	SingleMeanSessionCoverage float64 `json:"single_mean_session_coverage_at_30"`
	MultiMeanSessionCoverage  float64 `json:"multi_mean_session_coverage_at_30"`
}

func runRecallDiagnosticCLI(ctx context.Context, opt options, convs []conversation, arms []string, logger *slog.Logger) error {
	if err := validateRecallDiagnosticOptions(opt, arms); err != nil {
		return err
	}
	if aliasShadowEnabled(opt) && !opt.aliasShadowPrepared {
		if err := prepareAliasShadowStore(&opt); err != nil {
			return err
		}
	}
	if doc2queryEnabled(opt) && !opt.doc2queryPrepared {
		if err := prepareDoc2QueryStore(&opt); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create recall diagnostic run dir: %w", err)
	}
	if aliasShadowEnabled(opt) {
		var embClient embedding.Client
		if armBackend(arms[0]) == "hybrid" {
			embClient = buildBenchEmbeddingClient(logger, nil)
		}
		return runAliasShadowRecallDiagnosticWithClient(ctx, opt, convs, arms, embClient, logger)
	}
	if doc2queryEnabled(opt) {
		var embClient embedding.Client
		if armBackend(arms[0]) == "hybrid" {
			embClient = buildBenchEmbeddingClient(logger, nil)
		}
		return runDoc2QueryRecallDiagnosticWithClient(ctx, opt, convs, arms, embClient, logger)
	}

	apiKey := os.Getenv("LOCOMO_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LOCOMO_API_KEY is required for query decomposition (never passed as a flag so it stays out of process listings)")
	}
	model := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	prov, err := buildBenchProvider(envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider), apiKey, envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic"), opt.maxTokens, "LOCOMO_PROVIDER")
	if err != nil {
		return err
	}
	decomposeCall := gate(make(chan struct{}, opt.concurrency), newModelCaller(prov, model, opt.maxTokens))

	arm := arms[0]
	var embClient embedding.Client
	if armBackend(arm) == "hybrid" {
		embClient = buildBenchEmbeddingClient(logger, nil)
	}

	targetOpt := opt
	if targetOpt.onlyCategory == 0 {
		targetOpt.onlyCategory = 1
	}

	diagnosticCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	questions := make([]recallDiagnosticQuestion, 0)
	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	for _, conv := range convs {
		conv := conv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if diagnosticCtx.Err() != nil {
				return
			}
			runtime, err := openAttributionRuntime(diagnosticCtx, targetOpt, conv, embClient, arm)
			if err != nil {
				setErr(err)
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[arm]
			goldTurnText := turnTextIndex(conv)
			for _, selected := range selectQuestions(conv, targetOpt) {
				if diagnosticCtx.Err() != nil {
					return
				}
				qa := selected.QA
				armOpt := optionsForRun(targetOpt, arm, false)
				topK, quota := armOpt.retrievalFor(qa.Category)
				searchK := questionSearchK(topK, quota)
				singleCandidates, err := retriever.Search(diagnosticCtx, qa.Question, searchK)
				if err != nil {
					setErr(fmt.Errorf("recall diagnostic single retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				subqueries := decomposeQuery(diagnosticCtx, decomposeCall, qa.Question, armOpt.mqMaxSubqueries)
				if len(subqueries) == 0 || len(subqueries) > armOpt.mqMaxSubqueries {
					subqueries = []string{qa.Question}
				}
				multiCandidates, err := retriever.SearchMulti(diagnosticCtx, subqueries, searchK)
				if err != nil {
					setErr(fmt.Errorf("recall diagnostic multi retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				singleHits := finalizeQuestionHits(diagnosticCtx, qa.Question, singleCandidates, topK, quota, armOpt)
				multiHits := finalizeQuestionHits(diagnosticCtx, qa.Question, multiCandidates, topK, quota, armOpt)

				wideK := attributionWidePool(topK, armOpt.widePool)
				singleWide := singleCandidates
				multiWide := multiCandidates
				if wideK != searchK {
					singleWide, err = retriever.Search(diagnosticCtx, qa.Question, wideK)
					if err != nil {
						setErr(fmt.Errorf("recall diagnostic single wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
					multiWide, err = retriever.SearchMulti(diagnosticCtx, subqueries, wideK)
					if err != nil {
						setErr(fmt.Errorf("recall diagnostic multi wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
				}

				singleTrace := buildAttributionTrace(conv.ID, selected.Index, qa, singleHits, singleWide, runtime.chunkTurns, goldTurnText, topK, 0, opt.factCoverageTau, nil)
				multiTrace := buildAttributionTrace(conv.ID, selected.Index, qa, multiHits, multiWide, runtime.chunkTurns, goldTurnText, topK, 0, opt.factCoverageTau, nil)
				singleCoverage, singleSession, singleGradeable := evidenceRecallAt(qa, singleHits, runtime.chunkTurns)
				multiCoverage, multiSession, multiGradeable := evidenceRecallAt(qa, multiHits, runtime.chunkTurns)
				goldResolved := len(singleTrace.GoldTurns) > 0
				var goldRankDelta *int
				if singleTrace.GoldRankPool > 0 && multiTrace.GoldRankPool > 0 {
					delta := singleTrace.GoldRankPool - multiTrace.GoldRankPool
					goldRankDelta = &delta
				}
				record := recallDiagnosticQuestion{
					Conv:                      conv.ID,
					Q:                         selected.Index,
					Category:                  qa.Category,
					QuestionID:                qa.QuestionID,
					SubqueryCount:             len(subqueries),
					GoldResolved:              goldResolved,
					SingleGoldRank:            singleTrace.GoldRankPool,
					MultiGoldRank:             multiTrace.GoldRankPool,
					GoldRankDelta:             goldRankDelta,
					SingleGoldRankAt30:        singleTrace.GoldRankTopK,
					MultiGoldRankAt30:         multiTrace.GoldRankTopK,
					GoldRankAt30Delta:         diagnosticRank(singleTrace.GoldRankTopK) - diagnosticRank(multiTrace.GoldRankTopK),
					Gradeable:                 singleGradeable && multiGradeable,
					SingleCoverageAt30:        singleCoverage,
					MultiCoverageAt30:         multiCoverage,
					CoverageAt30Delta:         multiCoverage - singleCoverage,
					SingleSessionCoverageAt30: singleSession,
					MultiSessionCoverageAt30:  multiSession,
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
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Conv != questions[j].Conv {
			return questions[i].Conv < questions[j].Conv
		}
		return questions[i].Q < questions[j].Q
	})

	if err := writeRecallDiagnosticQuestions(filepath.Join(opt.runDir, "recall_diagnostic.jsonl"), questions); err != nil {
		return fmt.Errorf("write recall_diagnostic.jsonl: %w", err)
	}
	report := summarizeRecallDiagnostic(questions, targetOpt.onlyCategory)
	if err := writeJSON(filepath.Join(opt.runDir, "recall_diagnostic.json"), report); err != nil {
		return fmt.Errorf("write recall_diagnostic.json: %w", err)
	}
	fmt.Printf("recall diagnostic: category=%s n=%d coverage@30 %.3f -> %.3f (delta=%+.3f), gold entered=%d left=%d\n",
		report.TargetCategory, report.Questions, report.SingleMeanCoverageAt30, report.MultiMeanCoverageAt30, report.MeanCoverageAt30Delta, report.GoldEnteredTop30, report.GoldLeftTop30)
	logger.Info("recall diagnostic complete", "questions", len(questions), "answer_callers", 0, "judge_callers", 0)
	return nil
}

func validateRecallDiagnosticOptions(opt options, arms []string) error {
	if opt.runDir == "" {
		return fmt.Errorf("--run-dir is required with --recall-diagnostic")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--store-dir is required with --recall-diagnostic (retrieval-only mode never builds a store)")
	}
	if opt.datasetFormat != "locomo" {
		return fmt.Errorf("--recall-diagnostic supports --dataset-format locomo only")
	}
	if !opt.chunks {
		return fmt.Errorf("--recall-diagnostic requires --chunks so gold evidence can be mapped to retrieved context")
	}
	if opt.widePool < 0 {
		return fmt.Errorf("--wide-pool must be non-negative")
	}
	if len(arms) != 1 {
		return fmt.Errorf("--recall-diagnostic requires exactly one retrieval backend")
	}
	if aliasShadowEnabled(opt) && armBackend(arms[0]) != "hybrid" {
		return fmt.Errorf("--recall-diagnostic with --alias-shadow requires the hybrid retrieval backend")
	}
	if doc2queryEnabled(opt) && armBackend(arms[0]) != "hybrid" {
		return fmt.Errorf("--recall-diagnostic with --doc2query requires the hybrid retrieval backend")
	}
	if opt.topK != multiQueryFinalTopK {
		return fmt.Errorf("--recall-diagnostic is fixed at --top-k %d", multiQueryFinalTopK)
	}
	for category, topK := range opt.catTopK {
		if topK != multiQueryFinalTopK {
			return fmt.Errorf("--recall-diagnostic requires category %d top-k to remain %d, got %d", category, multiQueryFinalTopK, topK)
		}
	}
	if opt.filterPool > 0 {
		return fmt.Errorf("--filter-pool is not allowed with --recall-diagnostic")
	}
	if opt.chunkQuota > multiQueryFinalTopK {
		return fmt.Errorf("--recall-diagnostic requires --chunk-quota at most %d", multiQueryFinalTopK)
	}
	for category, quota := range opt.catQuota {
		if quota > multiQueryFinalTopK {
			return fmt.Errorf("--recall-diagnostic requires category %d chunk quota at most %d, got %d", category, multiQueryFinalTopK, quota)
		}
	}
	spec, _ := parseArm(arms[0])
	if opt.rerank || opt.pcic || opt.oracle || spec.mechanisms["rerank"] || spec.mechanisms["pcic"] || spec.mechanisms["oracle"] {
		return fmt.Errorf("--recall-diagnostic does not support rerank, pcic, or oracle modifiers")
	}
	return nil
}

func diagnosticRank(rank int) int {
	if rank < 1 || rank > multiQueryFinalTopK {
		return multiQueryFinalTopK + 1
	}
	return rank
}

func summarizeRecallDiagnostic(questions []recallDiagnosticQuestion, targetCategory int) recallDiagnosticReport {
	report := recallDiagnosticReport{
		Mode:           "retrieval_only",
		TargetCategory: categoryLabel(targetCategory),
		TopK:           multiQueryFinalTopK,
		Questions:      len(questions),
	}
	var singleRankSum, multiRankSum, rankDeltaSum float64
	var singleRankAt30Sum, multiRankAt30Sum, rankAt30DeltaSum float64
	var singleCoverageSum, multiCoverageSum float64
	var singleSessionSum, multiSessionSum float64
	for _, question := range questions {
		if question.GoldResolved {
			report.GoldResolved++
			singleRankAt30Sum += float64(diagnosticRank(question.SingleGoldRankAt30))
			multiRankAt30Sum += float64(diagnosticRank(question.MultiGoldRankAt30))
			rankAt30DeltaSum += float64(question.GoldRankAt30Delta)
			if question.SingleGoldRankAt30 < 1 && question.MultiGoldRankAt30 > 0 {
				report.GoldEnteredTop30++
			}
			if question.SingleGoldRankAt30 > 0 && question.MultiGoldRankAt30 < 1 {
				report.GoldLeftTop30++
			}
		}
		if question.GoldRankDelta != nil {
			report.GoldRankComparable++
			singleRankSum += float64(question.SingleGoldRank)
			multiRankSum += float64(question.MultiGoldRank)
			rankDeltaSum += float64(*question.GoldRankDelta)
		}
		if question.Gradeable {
			report.Gradeable++
			singleCoverageSum += question.SingleCoverageAt30
			multiCoverageSum += question.MultiCoverageAt30
			singleSessionSum += question.SingleSessionCoverageAt30
			multiSessionSum += question.MultiSessionCoverageAt30
		}
	}
	if report.GoldResolved > 0 {
		denominator := float64(report.GoldResolved)
		report.SingleMeanGoldRankAt30 = singleRankAt30Sum / denominator
		report.MultiMeanGoldRankAt30 = multiRankAt30Sum / denominator
		report.MeanGoldRankAt30Delta = rankAt30DeltaSum / denominator
	}
	if report.GoldRankComparable > 0 {
		denominator := float64(report.GoldRankComparable)
		report.SingleMeanGoldRank = singleRankSum / denominator
		report.MultiMeanGoldRank = multiRankSum / denominator
		report.MeanGoldRankDelta = rankDeltaSum / denominator
	}
	if report.Gradeable > 0 {
		denominator := float64(report.Gradeable)
		report.SingleMeanCoverageAt30 = singleCoverageSum / denominator
		report.MultiMeanCoverageAt30 = multiCoverageSum / denominator
		report.MeanCoverageAt30Delta = report.MultiMeanCoverageAt30 - report.SingleMeanCoverageAt30
		report.SingleMeanSessionCoverage = singleSessionSum / denominator
		report.MultiMeanSessionCoverage = multiSessionSum / denominator
	}
	return report
}

func writeRecallDiagnosticQuestions(path string, questions []recallDiagnosticQuestion) error {
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
