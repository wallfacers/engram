package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

const (
	quadrantOK             = "q1_ok"
	quadrantAnswerSide     = "q2_answer_side"
	quadrantUS2Target      = "q3_us2_target"
	quadrantExtractionSide = "q4_extraction_side"
	quadrantGoldUnresolved = "gold_unresolved"
	quadrantRetrievalOnly  = "retrieval_only"

	attributionCorrectSource   = "008-us4-e2e/results-hybrid.jsonl"
	defaultAttributionWidePool = 300
	defaultEmbedProbeQueries   = 32
)

// AttributionTrace is the retrieval-only attribution record for one question.
type AttributionTrace struct {
	Conv          int            `json:"conv"`
	Q             int            `json:"q"`
	Category      int            `json:"category"`
	CategoryName  string         `json:"category_name"`
	GoldEvidence  []string       `json:"gold_evidence"`
	GoldTurns     []string       `json:"gold_turns"`
	Retrieved     []RetrievedHit `json:"retrieved"`
	GoldInPool    bool           `json:"gold_in_pool"`
	GoldRank      int            `json:"gold_rank"`
	OutrankedBy   []RetrievedHit `json:"outranked_by"`
	Quadrant      string         `json:"quadrant"`
	Correct       *bool          `json:"correct,omitempty"`
	CorrectSource string         `json:"correct_source,omitempty"`
}

// RetrievedHit is the adapter-visible attribution for one fused retrieval hit.
// PerSignalRanks remains nil in US1 because the engine does not expose it.
type RetrievedHit struct {
	Name            string         `json:"name"`
	Rank            int            `json:"rank"`
	RRFScore        float64        `json:"rrf_score"`
	CoversGold      bool           `json:"covers_gold"`
	MappedGoldTurns []string       `json:"mapped_gold_turns"`
	PerSignalRanks  map[string]int `json:"per_signal_ranks,omitempty"`
}

// QuadrantDistribution is one category's mutually exclusive attribution count.
type QuadrantDistribution struct {
	Category         string `json:"category,omitempty"`
	Q1OK             int    `json:"q1_ok"`
	Q2AnswerSide     int    `json:"q2_answer_side"`
	Q3US2Target      int    `json:"q3_us2_target"`
	Q4ExtractionSide int    `json:"q4_extraction_side"`
	GoldUnresolved   int    `json:"gold_unresolved"`
	TotalGradeable   int    `json:"total_gradeable"`
}

func buildAttributionTrace(convID, questionIndex int, qa locomoQA, hits, wideHits []memory.Result, chunkTurns map[string][]string, topK, outrankCap int, correct *bool) AttributionTrace {
	goldTurns := parsedGoldTurns(qa.Evidence)
	goldSet := make(map[string]struct{}, len(goldTurns))
	for _, turnID := range goldTurns {
		goldSet[turnID] = struct{}{}
	}

	retrieved := make([]RetrievedHit, 0, len(hits))
	goldRank := -1
	for index, hit := range hits {
		mapped := mappedGoldTurns(hit, chunkTurns, goldTurns, goldSet)
		item := newRetrievedHit(hit, index+1, mapped)
		retrieved = append(retrieved, item)
		if goldRank < 0 && item.CoversGold {
			goldRank = item.Rank
		}
	}

	goldInPool := len(coveredGoldTurns(wideHits, chunkTurns, goldSet)) > 0
	if goldRank > 0 {
		goldInPool = true
	}
	outRankedBy := make([]RetrievedHit, 0)
	if goldRank > 0 && outrankCap > 0 {
		for _, hit := range retrieved[:goldRank-1] {
			if hit.CoversGold {
				continue
			}
			outRankedBy = append(outRankedBy, hit)
			if len(outRankedBy) == outrankCap {
				break
			}
		}
	}

	trace := AttributionTrace{
		Conv:         convID,
		Q:            questionIndex,
		Category:     qa.Category,
		CategoryName: attributionCategoryName(qa.Category, qa.CategoryName),
		GoldEvidence: append([]string(nil), qa.Evidence...),
		GoldTurns:    goldTurns,
		Retrieved:    retrieved,
		GoldInPool:   goldInPool,
		GoldRank:     goldRank,
		OutrankedBy:  outRankedBy,
		Quadrant:     classifyAttribution(len(goldTurns) > 0, goldInPool, goldRank, topK, correct),
	}
	if correct != nil {
		value := *correct
		trace.Correct = &value
		trace.CorrectSource = attributionCorrectSource
	}
	return trace
}

func parsedGoldTurns(evidence []string) []string {
	refs := evidenceReferences(evidence)
	turns := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		turnID := fmt.Sprintf("D%d:%d", ref.Session, ref.Dialog)
		if _, duplicate := seen[turnID]; duplicate {
			continue
		}
		seen[turnID] = struct{}{}
		turns = append(turns, turnID)
	}
	return turns
}

func mappedGoldTurns(hit memory.Result, chunkTurns map[string][]string, goldTurns []string, goldSet map[string]struct{}) []string {
	covered := coveredGoldTurns([]memory.Result{hit}, chunkTurns, goldSet)
	mapped := make([]string, 0, len(covered))
	for _, turnID := range goldTurns {
		if _, ok := covered[turnID]; ok {
			mapped = append(mapped, turnID)
		}
	}
	return mapped
}

func newRetrievedHit(hit memory.Result, rank int, mappedGoldTurns []string) RetrievedHit {
	return RetrievedHit{
		Name:            hit.Name,
		Rank:            rank,
		RRFScore:        hit.Score,
		CoversGold:      len(mappedGoldTurns) > 0,
		MappedGoldTurns: mappedGoldTurns,
	}
}

func classifyAttribution(goldResolved, goldInPool bool, goldRank, topK int, correct *bool) string {
	if !goldResolved {
		return quadrantGoldUnresolved
	}
	if correct == nil {
		return quadrantRetrievalOnly
	}
	if goldRank > 0 && goldRank <= topK {
		if *correct {
			return quadrantOK
		}
		return quadrantAnswerSide
	}
	if goldInPool {
		return quadrantUS2Target
	}
	return quadrantExtractionSide
}

func summarizeAttribution(traces []AttributionTrace) map[string]QuadrantDistribution {
	distribution := make(map[string]QuadrantDistribution)
	for _, trace := range traces {
		category := attributionCategoryName(trace.Category, trace.CategoryName)
		bucket := distribution[category]
		bucket.Category = category
		switch trace.Quadrant {
		case quadrantOK:
			bucket.Q1OK++
			bucket.TotalGradeable++
		case quadrantAnswerSide:
			bucket.Q2AnswerSide++
			bucket.TotalGradeable++
		case quadrantUS2Target:
			bucket.Q3US2Target++
			bucket.TotalGradeable++
		case quadrantExtractionSide:
			bucket.Q4ExtractionSide++
			bucket.TotalGradeable++
		case quadrantGoldUnresolved:
			bucket.GoldUnresolved++
		}
		distribution[category] = bucket
	}
	return distribution
}

func attributionCategoryName(category int, name string) string {
	if name == "" {
		name = categoryLabel(category)
	}
	return strings.ReplaceAll(name, "-", "_")
}

func loadAttributionCorrectness(path string) (map[resultKey]*bool, error) {
	joined := make(map[resultKey]*bool)
	if path == "" {
		return joined, nil
	}
	if err := scanResultsJSONL(path, func(item result) {
		correct := item.Correct
		joined[resultKey{Conv: item.Conv, Q: item.Q}] = &correct
	}); err != nil {
		return nil, fmt.Errorf("read attribution join %s: %w", path, err)
	}
	return joined, nil
}

func runAttributionCLI(ctx context.Context, opt options, convs []conversation, arms []string, logger *slog.Logger) error {
	if err := validateAttributionOptions(opt, arms); err != nil {
		return err
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create attribution run dir: %w", err)
	}
	joined, err := loadAttributionCorrectness(opt.joinResults)
	if err != nil {
		return err
	}
	if err := validateAttributionJoinCoverage(convs, opt, joined); err != nil {
		return err
	}

	arm := arms[0]
	var embClient embedding.Client
	if armBackend(arm) == "hybrid" || opt.embedProbe {
		embClient = buildBenchEmbeddingClient(logger, nil)
		if opt.embedProbe && embClient == nil {
			return fmt.Errorf("--embed-probe requires a configured local embedding client")
		}
	}

	traces := make([]AttributionTrace, 0, countSelectedQuestions(convs, opt))
	probeQueries := make([]string, 0, defaultEmbedProbeQueries)
	for _, conv := range convs {
		runtime, err := openAttributionRuntime(ctx, opt, conv, embClient, arm)
		if err != nil {
			return err
		}
		armOpt := optionsForRun(opt, arm, false)
		retriever := runtime.retrievers[arm]
		for _, selected := range selectQuestions(conv, opt) {
			qa := selected.QA
			topK, quota := armOpt.retrievalFor(qa.Category)
			hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, topK, quota, nil)
			if err != nil {
				runtime.Close()
				return fmt.Errorf("attribution retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err)
			}
			poolSize := attributionWidePool(topK, opt.widePool)
			wideHits, _, err := retriever.SearchWithDiagnostics(ctx, qa.Question, poolSize)
			if err != nil {
				runtime.Close()
				return fmt.Errorf("attribution wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err)
			}
			correct := joined[resultKey{Conv: conv.ID, Q: selected.Index}]
			traces = append(traces, buildAttributionTrace(conv.ID, selected.Index, qa, hits, wideHits, runtime.chunkTurns, topK, opt.outrankCap, correct))
			if opt.embedProbe && len(probeQueries) < defaultEmbedProbeQueries {
				probeQueries = append(probeQueries, qa.Question)
			}
		}
		runtime.Close()
	}

	if err := writeAttributionTraces(filepath.Join(opt.runDir, "trace.jsonl"), traces); err != nil {
		return fmt.Errorf("write trace.jsonl: %w", err)
	}
	if err := writeJSON(filepath.Join(opt.runDir, "quadrant-distribution.json"), summarizeAttribution(traces)); err != nil {
		return fmt.Errorf("write quadrant-distribution.json: %w", err)
	}
	if opt.embedProbe {
		report, err := probeEmbeddings(ctx, embClient, probeQueries, defaultEmbedBoundedL2)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(opt.runDir, "embed_probe.json"), report); err != nil {
			return fmt.Errorf("write embed_probe.json: %w", err)
		}
	}
	logger.Info("attribution trace complete", "questions", len(traces), "answer_calls", 0, "judge_calls", 0)
	return nil
}

func validateAttributionOptions(opt options, arms []string) error {
	if opt.runDir == "" {
		return fmt.Errorf("--run-dir is required with --attribution-trace")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--store-dir is required with --attribution-trace (retrieval-only mode never builds a store)")
	}
	if opt.datasetFormat != "locomo" {
		return fmt.Errorf("--attribution-trace supports --dataset-format locomo only")
	}
	if len(arms) != 1 || (arms[0] != "fts" && arms[0] != "hybrid") {
		return fmt.Errorf("--attribution-trace requires exactly one retrieval backend: fts or hybrid")
	}
	if opt.filterPool > 0 {
		return fmt.Errorf("--filter-pool is not allowed with --attribution-trace because it initializes an LLM caller")
	}
	if opt.rerank {
		return fmt.Errorf("--rerank is not allowed with --attribution-trace")
	}
	if opt.topK < 1 {
		return fmt.Errorf("--top-k must be at least 1 with --attribution-trace")
	}
	if opt.widePool < 0 {
		return fmt.Errorf("--wide-pool must be non-negative")
	}
	if opt.outrankCap < 0 {
		return fmt.Errorf("--outrank-cap must be non-negative")
	}
	return nil
}

func validateAttributionJoinCoverage(convs []conversation, opt options, joined map[resultKey]*bool) error {
	if opt.joinResults == "" {
		return nil
	}
	for _, conv := range convs {
		for _, selected := range selectQuestions(conv, opt) {
			key := resultKey{Conv: conv.ID, Q: selected.Index}
			if joined[key] == nil {
				return fmt.Errorf("--join-results %s missing conv=%d question=%d; refusing a partial quadrant distribution", opt.joinResults, key.Conv, key.Q)
			}
		}
	}
	return nil
}

func attributionWidePool(topK, configured int) int {
	if configured > topK {
		return configured
	}
	widePool := topK * 6
	if widePool < defaultAttributionWidePool {
		widePool = defaultAttributionWidePool
	}
	return widePool
}

func openAttributionRuntime(ctx context.Context, opt options, conv conversation, embClient embedding.Client, arm string) (*conversationRuntime, error) {
	path := filepath.Join(opt.storeDir, fmt.Sprintf("conv%d.db", conv.ID))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("attribution trace needs persisted store %s", path)
		}
		return nil, fmt.Errorf("stat attribution store %s: %w", path, err)
	}
	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		return nil, fmt.Errorf("open attribution store %s: %w", path, err)
	}
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	client := embClient
	if armBackend(arm) != "hybrid" {
		client = nil
	}
	retriever := memory.NewRetrieverWithOptions(entries, vectors, client, nil, retrieverOptionsForAt(optionsForRun(opt, arm, false), temporalNowForConversation(conv)))
	var chunkTurns map[string][]string
	if opt.chunks {
		chunkTurns = probeChunkTurns(conv)
	}
	return &conversationRuntime{
		store:       st,
		entries:     entries,
		vectors:     vectors,
		embedClient: embClient,
		retrievers:  map[string]*memory.Retriever{arm: retriever},
		reranked:    map[string]bool{arm: false},
		chunkTurns:  chunkTurns,
	}, nil
}

func writeAttributionTraces(path string, traces []AttributionTrace) error {
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
	for _, trace := range traces {
		if err := encoder.Encode(trace); err != nil {
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
