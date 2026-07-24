package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
)

// Feature 013 US1: retrieval-only temporal recall diagnostic (adapter-only).
//
// The gate answers ONE question before any engine mechanism is built: is the
// LoCoMo temporal-category shortfall a recall problem (gold buried below the
// candidate pool cutoff), or does it live in the parser / extraction / answer
// side? It runs four layers over temporal-category questions using ONLY the
// engine's read APIs (Retriever.Search / EntriesByName / List /
// ParseTemporalIntent) — zero answer/judge/extraction LLM tokens (008 铁律:
// coverage 仅诊断). Layer 3's event∈window "oracle" pull is a pure adapter-side
// measurement of the proposed recall arm's ceiling; the real arm is US2 engine
// work, deliberately NOT built here.

const (
	// temporalCategory is the LoCoMo integer category for temporal questions
	// (categoryLabel(2) == "temporal").
	temporalCategory = 2

	temporalDiagnosticTopK = 30

	temporalDiagnosticReportFile    = "temporal_diagnostic.json"
	temporalDiagnosticQuestionsFile = "temporal_diagnostic_questions.jsonl"
)

// Verdict enum + causes. Causes mirror the four layers (parser / extraction /
// not-a-recall-bottleneck / ceiling-too-low) per FR-006.
const (
	verdictGo   = "GO"
	verdictNoGo = "NO-GO"

	causeParser     = "解析器"
	causeExtraction = "抽取侧"
	causeNotRecall  = "非召回瓶颈"
	causeCeiling    = "天花板不足"
)

// Verdict thresholds — deliberately provisional and reviewer-tunable. The real
// box run (T008) tunes these against actual numbers; the diagnostic always
// emits the raw metrics so the human judgment never hinges on these defaults.
const (
	// Layer 0: below this fraction of temporal queries parsing a window, the
	// parser (not the recall structure) is the bottleneck (Fork B).
	temporalParseCoverageFloor = 0.50
	// Layer 1: below this fraction of gold facts carrying an event date, the
	// extraction side is the bottleneck (the recall arm has nothing to query).
	temporalEventDateCoverageFloor = 0.50
	// Layer 2: below this fraction of gold-resolved questions with buried gold,
	// gold already sits in the top-30 — not a recall bottleneck.
	temporalBuriedRatioFloor = 0.20
	// Layer 3: below this fraction of buried gold facts the oracle can lift into
	// top-30, the proposed arm's ceiling is too low to justify building it.
	temporalOracleLiftFloor = 0.20
)

// temporalOracleFact is the adapter-side view of a stored fact's event bounds
// used by the Layer 3 measurement. It is NOT an engine type — Layer 3 replicates
// the intersection predicate as pure measurement (the engine method is US2).
type temporalOracleFact struct {
	Name       string
	EventStart *time.Time
	EventEnd   *time.Time
	EventDate  *time.Time
}

// temporalDiagnosticQuestion is one temporal-category question's per-layer
// evidence, persisted to a JSONL artifact for inspection.
type temporalDiagnosticQuestion struct {
	Conv       int    `json:"conv"`
	Q          int    `json:"q"`
	Category   int    `json:"category"`
	QuestionID string `json:"question_id,omitempty"`
	// Layer 0
	ParsedWindow bool `json:"parsed_window"`
	// Layer 2
	GoldResolved bool `json:"gold_resolved"`
	GoldRankPool int  `json:"gold_rank_pool"`
	GoldRankTopK int  `json:"gold_rank_topk"`
	Buried       bool `json:"buried"`
	// Layer 1 (fact-level)
	GoldFactCount       int `json:"gold_fact_count"`
	GoldFactsEventDated int `json:"gold_facts_event_dated"`
	// Layer 3 (fact-level; only meaningful for parsed questions)
	BuriedGoldFacts   int `json:"buried_gold_facts"`
	OracleLiftedFacts int `json:"oracle_lifted_facts"`
}

// temporalDiagnosticReport is the four-layer summary + verdict.
type temporalDiagnosticReport struct {
	Mode           string `json:"mode"`
	TargetCategory string `json:"target_category"`
	TopK           int    `json:"top_k"`
	Questions      int    `json:"questions"`

	// Layer 0
	ParseWindowQuestions int     `json:"parse_window_questions"`
	ParseCoverage        float64 `json:"parse_coverage"`

	// Layer 1
	GoldFacts           int     `json:"gold_facts"`
	GoldFactsEventDated int     `json:"gold_facts_event_dated"`
	EventDateCoverage   float64 `json:"event_date_coverage"`

	// Layer 2
	GoldResolvedQuestions int     `json:"gold_resolved_questions"`
	BuriedQuestions       int     `json:"buried_questions"`
	BuriedRatio           float64 `json:"buried_ratio"`
	GoldRankPoolP50       float64 `json:"gold_rank_pool_p50"`
	GoldRankPoolP90       float64 `json:"gold_rank_pool_p90"`

	// Layer 3
	BuriedGoldFacts   int     `json:"buried_gold_facts"`
	OracleLiftedFacts int     `json:"oracle_lifted_facts"`
	OracleLiftRatio   float64 `json:"oracle_lift_ratio"`

	// Verdict
	Verdict string `json:"verdict"`
	Cause   string `json:"cause,omitempty"`
}

// --- Layer 0 ---

func temporalParseOK(question string, anchor time.Time) bool {
	_, ok := memory.ParseTemporalIntent(question, anchor)
	return ok
}

// --- Layer 1 ---

func entryHasEventTime(e *memory.Entry) bool {
	return e != nil && (e.EventStart != nil || e.EventEnd != nil || e.EventDate != nil)
}

// layer1EventDated counts, over the resolved gold fact names, how many resolve
// to a stored entry (total) and how many of those carry a usable event time
// (dated). Names absent from byName are omitted (the fact did not resolve).
func layer1EventDated(byName map[string]*memory.Entry, goldFactNames []string) (dated, total int) {
	for _, name := range goldFactNames {
		entry, ok := byName[name]
		if !ok {
			continue
		}
		total++
		if entryHasEventTime(entry) {
			dated++
		}
	}
	return dated, total
}

// --- Layer 2 ---

// temporalGoldBuried reports whether a gold-resolved question's gold fact sits
// below the top-K cutoff: not in the narrow top-K (GoldRankTopK outside [1,cutoff])
// OR never surfaced in the wide pool.
func temporalGoldBuried(goldResolved bool, rankPool, rankTopK, cutoff int) bool {
	if !goldResolved {
		return false
	}
	if rankTopK >= 1 && rankTopK <= cutoff {
		return false
	}
	return true
}

// --- Layer 3 (pure adapter-side measurement) ---

// eventInterval resolves a fact's event bounds to [start,end], mirroring the
// engine's temporalBounds fallback: both nil → event_date for both; event_date
// also nil → no interval (ok=false). A single-sided bound makes a point event.
func eventInterval(evStart, evEnd, evDate *time.Time) (start, end time.Time, ok bool) {
	s, e := evStart, evEnd
	if s == nil && e == nil {
		if evDate == nil {
			return time.Time{}, time.Time{}, false
		}
		s, e = evDate, evDate
	}
	if s != nil {
		start = s.UTC()
	}
	if e != nil {
		end = e.UTC()
	}
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	if end.Before(start) {
		end = start
	}
	return start, end, true
}

// eventIntersectsWindow applies the interval-intersection predicate
// (event_end >= window.Start AND event_start <= window.End) with half-open
// handling: a zero window bound means that side is unbounded; both zero → empty.
func eventIntersectsWindow(start, end time.Time, win memory.TimeWindow) bool {
	if win.Start.IsZero() && win.End.IsZero() {
		return false
	}
	if !win.End.IsZero() && start.After(win.End) {
		return false
	}
	if !win.Start.IsZero() && end.Before(win.Start) {
		return false
	}
	return true
}

// temporalOracleNames returns the pure event∈window oracle pull: facts whose
// event interval intersects the window, ordered by temporal proximity (all
// intersecting facts have gap 0), tie-broken by name ascending, truncated to
// topN. This is the ceiling of the proposed recall arm, measured in the adapter.
func temporalOracleNames(facts []temporalOracleFact, win memory.TimeWindow, topN int) []string {
	type scored struct {
		name string
		gap  time.Duration
	}
	matched := make([]scored, 0, len(facts))
	for _, fact := range facts {
		start, end, ok := eventInterval(fact.EventStart, fact.EventEnd, fact.EventDate)
		if !ok {
			continue
		}
		if !eventIntersectsWindow(start, end, win) {
			continue
		}
		matched = append(matched, scored{name: fact.Name, gap: 0})
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].gap != matched[j].gap {
			return matched[i].gap < matched[j].gap
		}
		return matched[i].name < matched[j].name
	})
	if topN > 0 && len(matched) > topN {
		matched = matched[:topN]
	}
	names := make([]string, len(matched))
	for i := range matched {
		names[i] = matched[i].name
	}
	return names
}

// temporalOracleLift counts how many buried gold fact names appear in the oracle
// top-N pull.
func temporalOracleLift(buriedGoldNames, oracleNames []string) int {
	oracle := make(map[string]struct{}, len(oracleNames))
	for _, name := range oracleNames {
		oracle[name] = struct{}{}
	}
	count := 0
	for _, name := range buriedGoldNames {
		if _, ok := oracle[name]; ok {
			count++
		}
	}
	return count
}

// --- summarize + verdict ---

func summarizeTemporalDiagnostic(questions []temporalDiagnosticQuestion) temporalDiagnosticReport {
	report := temporalDiagnosticReport{
		Mode:           "retrieval_only",
		TargetCategory: categoryLabel(temporalCategory),
		TopK:           temporalDiagnosticTopK,
		Questions:      len(questions),
	}
	rankPool := make([]int, 0, len(questions))
	for _, q := range questions {
		if q.ParsedWindow {
			report.ParseWindowQuestions++
		}
		report.GoldFacts += q.GoldFactCount
		report.GoldFactsEventDated += q.GoldFactsEventDated
		if q.GoldResolved {
			report.GoldResolvedQuestions++
			if q.Buried {
				report.BuriedQuestions++
			}
			if q.GoldRankPool > 0 {
				rankPool = append(rankPool, q.GoldRankPool)
			}
		}
		report.BuriedGoldFacts += q.BuriedGoldFacts
		report.OracleLiftedFacts += q.OracleLiftedFacts
	}
	if report.Questions > 0 {
		report.ParseCoverage = float64(report.ParseWindowQuestions) / float64(report.Questions)
	}
	if report.GoldFacts > 0 {
		report.EventDateCoverage = float64(report.GoldFactsEventDated) / float64(report.GoldFacts)
	}
	if report.GoldResolvedQuestions > 0 {
		report.BuriedRatio = float64(report.BuriedQuestions) / float64(report.GoldResolvedQuestions)
	}
	if report.BuriedGoldFacts > 0 {
		report.OracleLiftRatio = float64(report.OracleLiftedFacts) / float64(report.BuriedGoldFacts)
	}
	sort.Ints(rankPool)
	report.GoldRankPoolP50 = percentileInt(rankPool, 0.50)
	report.GoldRankPoolP90 = percentileInt(rankPool, 0.90)
	report.Verdict, report.Cause = temporalDiagnosticVerdict(report)
	return report
}

// temporalDiagnosticVerdict is GO iff all four layers pass; otherwise NO-GO with
// the first failing layer's cause. Evaluated in layer order so the reported
// cause is the earliest (most upstream) failure.
func temporalDiagnosticVerdict(r temporalDiagnosticReport) (verdict, cause string) {
	if r.ParseCoverage < temporalParseCoverageFloor {
		return verdictNoGo, causeParser
	}
	if r.EventDateCoverage < temporalEventDateCoverageFloor {
		return verdictNoGo, causeExtraction
	}
	if r.BuriedRatio < temporalBuriedRatioFloor {
		return verdictNoGo, causeNotRecall
	}
	if r.OracleLiftRatio < temporalOracleLiftFloor {
		return verdictNoGo, causeCeiling
	}
	return verdictGo, ""
}

// percentileInt returns the nearest-rank percentile of a pre-sorted slice.
func percentileInt(sorted []int, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(float64(len(sorted))*p + 0.5)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return float64(sorted[rank-1])
}

// --- driver ---

// runTemporalDiagnostic runs the four-layer retrieval-only diagnostic over the
// temporal-category questions of a prebuilt store. It spends zero answer/judge/
// extraction tokens (an extractNever guard asserts extraction is never called),
// writes a per-question JSONL + a report JSON, and prints the four-layer table
// plus a final GO / NO-GO(cause=…) line.
func runTemporalDiagnostic(ctx context.Context, opt options, convs []conversation, embClient embedding.Client, logger *slog.Logger) error {
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	if len(arms) == 0 {
		return fmt.Errorf("temporal diagnostic requires a retrieval backend")
	}
	arm := arms[0]
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create temporal diagnostic run dir: %w", err)
	}

	targetOpt := opt
	if targetOpt.onlyCategory == 0 {
		targetOpt.onlyCategory = temporalCategory
	}
	if targetOpt.factCoverageTau <= 0 {
		targetOpt.factCoverageTau = defaultFactCoverageTau
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
	questions := make([]temporalDiagnosticQuestion, 0)
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
		return "", fmt.Errorf("temporal diagnostic must not call extraction")
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
				setErr(fmt.Errorf("prepare temporal diagnostic conv=%d: %w", conv.ID, err))
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[arm]
			goldTurnText := turnTextIndex(conv)
			anchor := temporalNowForConversation(conv)
			storedFacts, err := attributionStoredFacts(diagnosticCtx, runtime)
			if err != nil {
				setErr(fmt.Errorf("load temporal diagnostic facts conv=%d: %w", conv.ID, err))
				return
			}
			oracleFacts, err := temporalOracleFactsFromRuntime(diagnosticCtx, runtime)
			if err != nil {
				setErr(fmt.Errorf("load temporal oracle facts conv=%d: %w", conv.ID, err))
				return
			}
			for _, selected := range selectQuestions(conv, targetOpt) {
				if diagnosticCtx.Err() != nil {
					return
				}
				qa := selected.QA
				window, parsed := memory.ParseTemporalIntent(qa.Question, anchor)
				armOpt := optionsForRun(targetOpt, arm, false)
				topK, quota := armOpt.retrievalFor(qa.Category)
				searchK := questionSearchK(topK, quota)
				candidates, err := retriever.Search(diagnosticCtx, qa.Question, searchK)
				if err != nil {
					setErr(fmt.Errorf("temporal diagnostic retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				hits := finalizeQuestionHits(diagnosticCtx, qa.Question, candidates, topK, quota, armOpt)
				wide := candidates
				wideK := attributionWidePool(topK, armOpt.widePool)
				if wideK != searchK {
					wide, err = retriever.Search(diagnosticCtx, qa.Question, wideK)
					if err != nil {
						setErr(fmt.Errorf("temporal diagnostic wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
				}
				trace := buildAttributionTrace(conv.ID, selected.Index, qa, hits, wide, runtime.chunkTurns, goldTurnText, topK, 0, targetOpt.factCoverageTau, nil)
				goldFactNames := attributedGoldFactNames(qa, storedFacts, runtime.chunkTurns, goldTurnText, targetOpt.factCoverageTau)

				// Layer 1: event-date coverage over the resolved gold facts.
				entriesByName, err := runtime.entries.EntriesByName(diagnosticCtx, goldFactNames)
				if err != nil {
					setErr(fmt.Errorf("load gold entries conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				dated, total := layer1EventDated(entriesByName, goldFactNames)

				// Layer 2: is the gold buried below the top-K cutoff?
				goldResolved := len(trace.GoldTurns) > 0
				buried := temporalGoldBuried(goldResolved, trace.GoldRankPool, trace.GoldRankTopK, topK)

				// Layer 3: of the gold facts buried out of the current top-30,
				// how many does the pure event∈window oracle lift back in?
				var buriedGoldFacts, oracleLifted int
				if parsed {
					currentNames := make(map[string]struct{}, len(hits))
					for _, hit := range hits {
						currentNames[hit.Name] = struct{}{}
					}
					buriedGoldNames := make([]string, 0, len(goldFactNames))
					for _, name := range goldFactNames {
						if _, ok := currentNames[name]; !ok {
							buriedGoldNames = append(buriedGoldNames, name)
						}
					}
					buriedGoldFacts = len(buriedGoldNames)
					oracleNames := temporalOracleNames(oracleFacts, window, temporalDiagnosticTopK)
					oracleLifted = temporalOracleLift(buriedGoldNames, oracleNames)
				}

				record := temporalDiagnosticQuestion{
					Conv:                conv.ID,
					Q:                   selected.Index,
					Category:            qa.Category,
					QuestionID:          qa.QuestionID,
					ParsedWindow:        parsed,
					GoldResolved:        goldResolved,
					GoldRankPool:        trace.GoldRankPool,
					GoldRankTopK:        trace.GoldRankTopK,
					Buried:              buried,
					GoldFactCount:       total,
					GoldFactsEventDated: dated,
					BuriedGoldFacts:     buriedGoldFacts,
					OracleLiftedFacts:   oracleLifted,
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
		return fmt.Errorf("temporal diagnostic extraction calls=%d, want 0", got)
	}
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Conv != questions[j].Conv {
			return questions[i].Conv < questions[j].Conv
		}
		return questions[i].Q < questions[j].Q
	})

	if err := writeTemporalDiagnosticQuestions(filepath.Join(opt.runDir, temporalDiagnosticQuestionsFile), questions); err != nil {
		return fmt.Errorf("write temporal diagnostic records: %w", err)
	}
	report := summarizeTemporalDiagnostic(questions)
	if err := writeJSON(filepath.Join(opt.runDir, temporalDiagnosticReportFile), report); err != nil {
		return fmt.Errorf("write temporal diagnostic report: %w", err)
	}
	printTemporalDiagnosticReport(report)
	logger.Info("temporal diagnostic complete", "questions", report.Questions, "verdict", report.Verdict,
		"cause", report.Cause, "extract_callers", 0, "answer_callers", 0, "judge_callers", 0)
	return nil
}

func temporalOracleFactsFromRuntime(ctx context.Context, runtime *conversationRuntime) ([]temporalOracleFact, error) {
	entries, err := runtime.entries.List(ctx)
	if err != nil {
		return nil, err
	}
	facts := make([]temporalOracleFact, 0, len(entries))
	for _, entry := range entries {
		if _, isChunk := runtime.chunkTurns[entry.Name]; isChunk || entry.FactSource == "verbatim_chunk" {
			continue
		}
		facts = append(facts, temporalOracleFact{
			Name:       entry.Name,
			EventStart: entry.EventStart,
			EventEnd:   entry.EventEnd,
			EventDate:  entry.EventDate,
		})
	}
	return facts, nil
}

func printTemporalDiagnosticReport(r temporalDiagnosticReport) {
	fmt.Printf("temporal diagnostic (retrieval-only, category=%s, top_k=%d)\n", r.TargetCategory, r.TopK)
	fmt.Printf("  Layer 0 parse_coverage:      %.3f (%d/%d)\n", r.ParseCoverage, r.ParseWindowQuestions, r.Questions)
	fmt.Printf("  Layer 1 event_date_coverage: %.3f (%d/%d gold facts)\n", r.EventDateCoverage, r.GoldFactsEventDated, r.GoldFacts)
	fmt.Printf("  Layer 2 buried_ratio:        %.3f (%d/%d gold-resolved) gold_rank_pool p50=%.0f p90=%.0f\n",
		r.BuriedRatio, r.BuriedQuestions, r.GoldResolvedQuestions, r.GoldRankPoolP50, r.GoldRankPoolP90)
	fmt.Printf("  Layer 3 oracle_lift@%d:       %.3f (%d/%d buried gold facts)\n",
		r.TopK, r.OracleLiftRatio, r.OracleLiftedFacts, r.BuriedGoldFacts)
	if r.Verdict == verdictGo {
		fmt.Println(verdictGo)
	} else {
		fmt.Printf("%s(cause=%s)\n", verdictNoGo, r.Cause)
	}
}

func writeTemporalDiagnosticQuestions(path string, questions []temporalDiagnosticQuestion) error {
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
