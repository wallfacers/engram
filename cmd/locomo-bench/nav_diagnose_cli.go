package main

// 029 US1 zero-cost navigation rescue-space diagnostic (specs/029 US1,
// quickstart.md). Retrieval must run inside the harness (hybrid search needs the
// embedding sidecar), so this mode emits the per-question retrieval diagnosis
// that nav_diagnose.py consumes for the three-way classification
// (in_pool / topk_hit / rescueable / not_in_pool) and the simulated-action
// attribution. It is fully offline for answerer/judge (zero paid calls); only
// the local embedding endpoint is used for hybrid retrieval.
//
// Rescue simulation is DETERMINISTIC and HONEST: simulated queries derive only
// from the question text and the evidence already seen in the single-shot
// top-k (never from the gold answer), mirroring the three US2 navigation tools:
//   - rewrite       ~ expand_query   : keyword/entity/temporal variants of the question
//   - follow_entity ~ follow_entity  : entity-anchored retrieval on entities in seen evidence
//   - deep (wide)   ~ deeper search  : gold's rank in the wide pool beyond top-k
//
// The engine is untouched (FR-003).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"log/slog"
)

// navDiagnoseQuestion is one question's US1 diagnosis record (JSONL line).
type navDiagnoseQuestion struct {
	Conv         int                  `json:"conv"`
	Q            int                  `json:"q"`
	QuestionID   string               `json:"question_id"`
	Question     string               `json:"question"`
	Category     int                  `json:"category"`
	GoldTurns    []string             `json:"gold_turns"`
	GoldResolved bool                 `json:"gold_resolved"`
	InPool       bool                 `json:"in_pool"`
	SingleTopK   navDiagnoseRetrieval `json:"single_topk"`
	WidePool     navDiagnoseRetrieval `json:"wide_pool"`
	Simulated    []navDiagnoseSimulated `json:"simulated"`
}

// navDiagnoseRetrieval reports whether a single retrieval path surfaced the
// gold evidence and at what 1-indexed rank (-1 = not surfaced).
type navDiagnoseRetrieval struct {
	GoldHit  bool `json:"gold_hit"`
	GoldRank int  `json:"gold_rank"`
	Hits     int  `json:"hits"`
}

// navDiagnoseSimulated is one deterministic rescue attempt.
type navDiagnoseSimulated struct {
	Action   string `json:"action"` // rewrite | follow_entity
	Query    string `json:"query"`
	GoldHit  bool   `json:"gold_hit"`
	GoldRank int    `json:"gold_rank"`
}

func runNavDiagnoseCLI(ctx context.Context, opt options, convs []conversation, arms []string, embClient embedding.Client, logger *slog.Logger) error {
	if err := validateNavDiagnoseOptions(opt); err != nil {
		return err
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create nav diagnose run dir: %w", err)
	}
	arm := arms[0]
	// hybrid (and only hybrid) needs the embedding sidecar.
	client := embClient
	if armBackend(arm) != "hybrid" {
		client = nil
	}

	diagnosticCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	records := make([]navDiagnoseQuestion, 0)
	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	start := time.Now()
	for _, conv := range convs {
		conv := conv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if diagnosticCtx.Err() != nil {
				return
			}
			runtime, err := openAttributionRuntime(diagnosticCtx, opt, conv, client, arm)
			if err != nil {
				setErr(err)
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[arm]
			goldTurnText := turnTextIndex(conv)
			for _, selected := range selectQuestions(conv, opt) {
				if diagnosticCtx.Err() != nil {
					return
				}
				qa := selected.QA
				armOpt := optionsForRun(opt, arm, false)
				topK, quota := armOpt.retrievalFor(qa.Category)
				searchK := questionSearchK(topK, quota)

				goldTurns := parsedGoldTurns(qa.Evidence)
				inPool := goldInConversationPool(runtime.entries, runtime.chunkTurns, goldTurns, diagnosticCtx)

				singleCandidates, err := retriever.Search(diagnosticCtx, qa.Question, searchK)
				if err != nil {
					setErr(fmt.Errorf("nav diagnose single retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				singleHits := finalizeQuestionHits(diagnosticCtx, qa.Question, singleCandidates, topK, quota, armOpt)
				single := goldRankInHits(singleHits, runtime.chunkTurns, goldTurnText, goldTurns, opt.factCoverageTau)

				wideK := attributionWidePool(topK, armOpt.widePool)
				wideCandidates := singleCandidates
				if wideK != searchK {
					wideCandidates, err = retriever.Search(diagnosticCtx, qa.Question, wideK)
					if err != nil {
						setErr(fmt.Errorf("nav diagnose wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
				}
				wide := goldRankInHits(wideCandidates, runtime.chunkTurns, goldTurnText, goldTurns, opt.factCoverageTau)

				// Deterministic rescue simulation — only when single-shot missed.
				var simulated []navDiagnoseSimulated
				if !single.GoldHit {
					simulated = simulateRescue(diagnosticCtx, retriever, qa, singleCandidates, runtime.chunkTurns, goldTurnText, goldTurns, searchK, opt.factCoverageTau)
				}

				record := navDiagnoseQuestion{
					Conv:         conv.ID,
					Q:            selected.Index,
					QuestionID:   qa.QuestionID,
					Question:     qa.Question,
					Category:     qa.Category,
					GoldTurns:    goldTurns,
					GoldResolved: len(goldTurns) > 0,
					InPool:       inPool,
					SingleTopK:   single,
					WidePool:     wide,
					Simulated:    simulated,
				}
				mu.Lock()
				records = append(records, record)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Conv != records[j].Conv {
			return records[i].Conv < records[j].Conv
		}
		return records[i].Q < records[j].Q
	})

	path := filepath.Join(opt.runDir, "nav-diagnose.jsonl")
	// Owner-only (0o600): the diagnostic carries raw question text and retrieved
	// memory content — sensitive data.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create nav-diagnose.jsonl: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("write nav-diagnose.jsonl: %w", err)
		}
	}
	logger.Info("nav diagnose complete", "questions", len(records), "elapsed_ms", time.Since(start).Milliseconds(), "out", path)
	return nil
}

func validateNavDiagnoseOptions(opt options) error {
	if opt.runDir == "" {
		return fmt.Errorf("--run-dir is required with --nav-diagnose")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--store-dir is required with --nav-diagnose (retrieval-only mode never builds a store)")
	}
	if !opt.chunks {
		return fmt.Errorf("--nav-diagnose requires --chunks so gold evidence can be mapped to retrieved chunks")
	}
	return nil
}

// goldInConversationPool reports whether any gold turn is covered by a chunk
// entry that actually exists in the persisted store (all-conversation oracle).
func goldInConversationPool(entries *memory.EntryStore, chunkTurns map[string][]string, goldTurns []string, ctx context.Context) bool {
	if len(goldTurns) == 0 {
		return false
	}
	goldSet := make(map[string]bool, len(goldTurns))
	for _, g := range goldTurns {
		goldSet[g] = true
	}
	for chunkName, turns := range chunkTurns {
		for _, t := range turns {
			if !goldSet[t] {
				continue
			}
			_, err := entries.GetByName(ctx, chunkName)
			if err == nil {
				return true
			}
		}
	}
	return false
}

// goldRankInHits reports the first gold-covering hit's 1-indexed rank and
// whether the gold surfaced at all. Rank -1 means the gold was not in the hits.
func goldRankInHits(hits []memory.Result, chunkTurns map[string][]string, goldTurnText map[string]string, goldTurns []string, tau float64) navDiagnoseRetrieval {
	for i, hit := range hits {
		if len(hitMappedGoldTurns(hit, chunkTurns, goldTurnText, goldTurns, tau)) > 0 {
			return navDiagnoseRetrieval{GoldHit: true, GoldRank: i + 1, Hits: len(hits)}
		}
	}
	return navDiagnoseRetrieval{GoldHit: false, GoldRank: -1, Hits: len(hits)}
}

// simulateRescue deterministically probes the three rescue mechanisms, in a
// fixed priority order (rewrite → follow_entity). Every simulated query derives
// only from the question text or the single-shot evidence — never from gold.
// The deep (wide-pool) mechanism is reported separately by WidePool.
func simulateRescue(ctx context.Context, retriever *memory.Retriever, qa locomoQA, singleCandidates []memory.Result, chunkTurns map[string][]string, goldTurnText map[string]string, goldTurns []string, searchK int, tau float64) []navDiagnoseSimulated {
	var out []navDiagnoseSimulated

	// rewrite: keyword / entity / temporal variants of the question itself.
	for _, variant := range questionRewriteVariants(qa.Question) {
		hits, err := retriever.Search(ctx, variant, searchK)
		if err != nil {
			continue
		}
		res := goldRankInHits(hits, chunkTurns, goldTurnText, goldTurns, tau)
		out = append(out, navDiagnoseSimulated{Action: "rewrite", Query: variant, GoldHit: res.GoldHit, GoldRank: res.GoldRank})
		if res.GoldHit {
			return out // first hit in priority order wins the attribution
		}
	}

	// follow_entity: entities lifted from evidence already seen in the
	// single-shot top-k.
	for _, entity := range extractEntitiesFromHits(singleCandidates) {
		hits, err := retriever.Search(ctx, entity, searchK)
		if err != nil {
			continue
		}
		res := goldRankInHits(hits, chunkTurns, goldTurnText, goldTurns, tau)
		out = append(out, navDiagnoseSimulated{Action: "follow_entity", Query: entity, GoldHit: res.GoldHit, GoldRank: res.GoldRank})
		if res.GoldHit {
			return out
		}
	}
	return out
}

var (
	navYearRe = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	monthRe   = regexp.MustCompile(`\b(january|february|march|april|may|june|july|august|september|october|november|december)\b`)
	quotedRe  = regexp.MustCompile(`"([^"]{2,})"`)
	titleRe   = regexp.MustCompile(`[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+`)
	stopWords = map[string]bool{
		"what": true, "which": true, "where": true, "when": true, "who": true,
		"how": true, "did": true, "does": true, "was": true, "were": true,
		"the": true, "a": true, "an": true, "is": true, "are": true, "of": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "by": true,
		"with": true, "from": true, "and": true, "or": true, "that": true, "this": true,
		"do": true, "have": true, "has": true, "had": true, "about": true,
		"after": true, "before": true, "during": true, "since": true, "until": true,
		"into": true, "onto": true, "than": true, "then": true, "there": true,
	}
)

// questionRewriteVariants builds deterministic query variants from the question
// text: (1) non-stopword content words joined in original order, (2) any quoted
// phrase, (3) any title-case entity, (4) temporal tokens (year/month). Variants
// are unique and non-empty.
func questionRewriteVariants(question string) []string {
	var variants []string
	seen := make(map[string]bool)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] || strings.EqualFold(v, strings.TrimSpace(question)) {
			return
		}
		seen[v] = true
		variants = append(variants, v)
	}

	var content []string
	for _, w := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') }) {
		if !stopWords[w] && len(w) > 2 {
			content = append(content, w)
		}
	}
	if len(content) > 0 {
		join := strings.Join(content, " ")
		if len(join) > 120 {
			join = join[:120]
		}
		add(join)
		// Most salient content words (by length, i.e. information density):
		// a focused query can beat the full question when keyword signals
		// (FTS trigram) favor exact terms.
		sort.SliceStable(content, func(i, j int) bool { return len(content[i]) > len(content[j]) })
		top := content
		if len(top) > 3 {
			top = top[:3]
		}
		for _, w := range top {
			add(w)
		}
	}
	for _, q := range quotedRe.FindAllString(question, -1) {
		add(strings.Trim(q, `"`))
	}
	for _, t := range titleRe.FindAllString(question, -1) {
		add(t)
	}
	var temporal []string
	for _, t := range navYearRe.FindAllString(question, -1) {
		temporal = append(temporal, t)
	}
	for _, m := range monthRe.FindAllString(question, -1) {
		temporal = append(temporal, m)
	}
	if len(temporal) > 0 {
		add(strings.Join(temporal, " "))
	}
	return variants
}

// extractEntitiesFromHits lifts candidate follow-entity targets from the
// single-shot evidence: quoted phrases and title-case multi-word sequences,
// deduplicated, capped at 5. Entities that also appear verbatim in the question
// are skipped (they are already covered by the rewrite variants).
func extractEntitiesFromHits(hits []memory.Result) []string {
	var entities []string
	seen := make(map[string]bool)
	add := func(e string) {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] || len(strings.Fields(e)) > 6 {
			return
		}
		seen[e] = true
		entities = append(entities, e)
	}
	for _, hit := range hits {
		for _, q := range quotedRe.FindAllString(hit.Content, -1) {
			add(strings.Trim(q, `"`))
		}
		for _, t := range titleRe.FindAllString(hit.Content, -1) {
			add(t)
		}
		if len(entities) >= 5 {
			break
		}
	}
	return entities
}
