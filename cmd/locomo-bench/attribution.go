package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

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
	// defaultFactCoverageTau is the directional-containment threshold: a fact
	// hit covers a gold turn when >=τ of the fact's unique content words appear
	// in the gold turn text (session-gated). Deterministic, no embeddings.
	defaultFactCoverageTau = 0.8
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
	// GoldRankTopK is the 1-indexed rank of the first gold-covering hit within
	// the narrow top-K the answerer actually consumed; -1 when absent. Drives
	// quadrant classification (was gold in front of the answerer?).
	GoldRankTopK int `json:"gold_rank_topk"`
	// GoldRankPool is the 1-indexed rank of the first gold-covering hit within
	// the wide diagnostic pool; -1 when absent. Drives outranked_by (which
	// non-gold hits sit above gold when we look wider than top-K).
	GoldRankPool int            `json:"gold_rank_pool"`
	OutrankedBy  []RetrievedHit `json:"outranked_by"`
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

func buildAttributionTrace(convID, questionIndex int, qa locomoQA, hits, wideHits []memory.Result, chunkTurns map[string][]string, goldTurnText map[string]string, topK, outrankCap int, tau float64, correct *bool) AttributionTrace {
	goldTurns := parsedGoldTurns(qa.Evidence)

	// Narrow top-K: what the answerer actually consumed → gold_rank_topk.
	retrieved := make([]RetrievedHit, 0, len(hits))
	goldRankTopK := -1
	for index, hit := range hits {
		mapped := hitMappedGoldTurns(hit, chunkTurns, goldTurnText, goldTurns, tau)
		item := newRetrievedHit(hit, index+1, mapped)
		retrieved = append(retrieved, item)
		if goldRankTopK < 0 && item.CoversGold {
			goldRankTopK = item.Rank
		}
	}

	// Wide pool: gold's true rank when we look past top-K → gold_rank_pool,
	// and the non-gold hits sitting above it → outranked_by.
	goldRankPool := -1
	outRankedBy := make([]RetrievedHit, 0)
	for index, hit := range wideHits {
		mapped := hitMappedGoldTurns(hit, chunkTurns, goldTurnText, goldTurns, tau)
		if len(mapped) > 0 {
			goldRankPool = index + 1
			break
		}
		if outrankCap > 0 && len(outRankedBy) < outrankCap {
			outRankedBy = append(outRankedBy, newRetrievedHit(hit, index+1, nil))
		}
	}
	if goldRankPool < 0 {
		// Gold never surfaced in the pool: the accumulated leaders outrank
		// nothing, so report no competitors rather than a pool prefix.
		outRankedBy = outRankedBy[:0]
	}
	goldInPool := goldRankPool > 0

	trace := AttributionTrace{
		Conv:         convID,
		Q:            questionIndex,
		Category:     qa.Category,
		CategoryName: attributionCategoryName(qa.Category, qa.CategoryName),
		GoldEvidence: append([]string(nil), qa.Evidence...),
		GoldTurns:    goldTurns,
		Retrieved:    retrieved,
		GoldInPool:   goldInPool,
		GoldRankTopK: goldRankTopK,
		GoldRankPool: goldRankPool,
		OutrankedBy:  outRankedBy,
		Quadrant:     classifyAttribution(len(goldTurns) > 0, goldInPool, goldRankTopK, topK, correct),
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

// hitMappedGoldTurns reports which gold turns a single hit covers, taking the
// path that matches the hit's provenance:
//   - chunk hits (present in chunkTurns) keep exact turn-id overlap — chunks
//     carry real DiaIDs, so coverage is turn-precise.
//   - fact hits carry only session-level provenance, so they map via
//     factCoversGoldTurn: session-gated directional lexical containment.
//
// The two never collide: probeChunkTurns keys only chunk-named entries, so a
// fact name is never in chunkTurns and takes the fact path.
func hitMappedGoldTurns(hit memory.Result, chunkTurns map[string][]string, goldTurnText map[string]string, goldTurns []string, tau float64) []string {
	mapped := make([]string, 0, len(goldTurns))
	if turns, isChunk := chunkTurns[hit.Name]; isChunk {
		turnSet := make(map[string]struct{}, len(turns))
		for _, t := range turns {
			turnSet[t] = struct{}{}
		}
		for _, gold := range goldTurns {
			if _, ok := turnSet[gold]; ok {
				mapped = append(mapped, gold)
			}
		}
		return mapped
	}
	for _, gold := range goldTurns {
		if factCoversGoldTurn(hit.Content, hit.SourceSessionID, goldTurnText[gold], goldTurnSession(gold), tau) {
			mapped = append(mapped, gold)
		}
	}
	return mapped
}

// factCoversGoldTurn reports whether a fact hit covers one gold turn via
// session-gated directional containment: the fact's source session must match
// the gold turn's session, and >=τ of the fact's unique content words must
// appear in the gold turn text. A fact is extracted from its source turn, so
// its content words should reappear there; the session gate rejects same-word
// coincidences across sessions. Deterministic (lexical only) so reruns stay
// byte-identical (SC-004) even when query embeddings are unstable.
func factCoversGoldTurn(factContent, factSessionID, goldTurnText string, goldSession int, tau float64) bool {
	if goldSession < 0 || factSessionNumber(factSessionID) != goldSession {
		return false
	}
	factWords := contentWordSet(factContent)
	if len(factWords) == 0 {
		return false
	}
	turnWords := contentWordSet(goldTurnText)
	if len(turnWords) == 0 {
		return false
	}
	present := 0
	for word := range factWords {
		if _, ok := turnWords[word]; ok {
			present++
		}
	}
	return float64(present)/float64(len(factWords)) >= tau
}

// goldTurnSession extracts the session number from a "D<session>:<dialog>" gold
// turn id; -1 when unparseable.
func goldTurnSession(turnID string) int {
	inner := strings.TrimPrefix(turnID, "D")
	sep := strings.IndexByte(inner, ':')
	if sep <= 0 {
		return -1
	}
	session, err := strconv.Atoi(inner[:sep])
	if err != nil {
		return -1
	}
	return session
}

// factSessionNumber extracts the session number from a "conv<N>-sess<M>" source
// session id; -1 when unparseable.
func factSessionNumber(sessionID string) int {
	idx := strings.LastIndex(sessionID, "sess")
	if idx < 0 {
		return -1
	}
	session, err := strconv.Atoi(sessionID[idx+len("sess"):])
	if err != nil {
		return -1
	}
	return session
}

// turnTextIndex maps each dialogue id (e.g. "D19:3") to its speaker-augmented
// turn text — the reference corpus fact hits are matched against for gold
// coverage. The speaker name is folded in because extraction resolves
// first-person pronouns to the speaker ("I ..." → "Maria ..."), so the fact's
// subject word is legitimately part of the turn's provenance even though the
// raw first-person turn text never contains it.
func turnTextIndex(conv conversation) map[string]string {
	index := make(map[string]string)
	for _, session := range conv.Sessions {
		for _, t := range session.Turns {
			if t.DiaID != "" {
				index[t.DiaID] = t.Speaker + " " + t.Text
			}
		}
	}
	return index
}

// contentWordSet lowercases text, splits on non-alphanumeric runes, and drops
// stopwords — the deterministic token basis for directional containment.
func contentWordSet(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, stop := attributionStopwords[field]; stop {
			continue
		}
		words[field] = struct{}{}
	}
	return words
}

// attributionStopwords is a small fixed English stoplist. Kept frozen so
// coverage stays deterministic and reproducible across runs.
var attributionStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "been": {},
	"but": {}, "by": {}, "for": {}, "from": {}, "had": {}, "has": {}, "have": {}, "he": {},
	"her": {}, "his": {}, "in": {}, "is": {}, "it": {}, "its": {}, "of": {}, "on": {},
	"or": {}, "she": {}, "that": {}, "the": {}, "their": {}, "them": {}, "they": {},
	"this": {}, "to": {}, "was": {}, "were": {}, "will": {}, "with": {},
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
		goldTurnText := turnTextIndex(conv)
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
			traces = append(traces, buildAttributionTrace(conv.ID, selected.Index, qa, hits, wideHits, runtime.chunkTurns, goldTurnText, topK, opt.outrankCap, opt.factCoverageTau, correct))
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
