package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"

	"github.com/wallfacers/engram/memory"
)

// evidenceRecallAt grades one QA's retrieved hits against its gold evidence,
// returning exact-turn recall and session recall. Unlike newSweepEvidenceDiagnostics
// it does NOT gate on the cluster-sweep signal, so every retrieval arm — including
// the plain-RRF baseline — is measured on the same honest ruler. gradeable is
// false when the QA carries no parseable D<session>:<dialog> gold evidence.
func evidenceRecallAt(qa locomoQA, hits []memory.Result, chunkTurns map[string][]string) (turnRecall, sessionRecall float64, gradeable bool) {
	refs := evidenceReferences(qa.Evidence)
	if len(refs) == 0 {
		return 0, 0, false
	}
	goldSessions := evidenceSessions(qa.Evidence)

	retrievedTurnIDs := make(map[string]struct{})
	retrievedSessions := make(map[int]struct{})
	for _, hit := range hits {
		for _, diaID := range chunkTurns[hit.Name] {
			retrievedTurnIDs[diaID] = struct{}{}
		}
		if session, ok := sourceSessionNumber(hit.SourceSessionID); ok {
			retrievedSessions[session] = struct{}{}
		}
	}

	matchedSessions := 0
	for _, session := range goldSessions {
		if _, ok := retrievedSessions[session]; ok {
			matchedSessions++
		}
	}
	if len(goldSessions) > 0 {
		sessionRecall = float64(matchedSessions) / float64(len(goldSessions))
	}
	turnRecall = exactTurnRecall(refs, retrievedTurnIDs)
	return turnRecall, sessionRecall, true
}

// coverageBucket accumulates turn/session recall over a set of graded QAs. The
// exported means are populated by finalize(); the running sums stay unexported so
// a serialized report carries only the finished averages.
type coverageBucket struct {
	N                 int     `json:"n"`
	TurnRecall        float64 `json:"turn_recall"`
	SessionRecall     float64 `json:"session_recall"`
	SelectionSurvival float64 `json:"selection_survival"`
	ComplementDrop    float64 `json:"complement_drop"`
	AnchorViolation   int     `json:"anchor_violation"`
	turnSum           float64
	sessionSum        float64
	candidateGold     int
	selectedGold      int
	metricQuestions   int
	complementDrops   int
}

type selectionMetricInput struct {
	Candidates []memory.Result
	Selected   []memory.Result
	GoldTurns  []string
	ChunkTurns map[string][]string
}

func (b *coverageBucket) addSelectionMetrics(input selectionMetricInput) {
	gold := make(map[string]struct{}, len(input.GoldTurns))
	for _, turnID := range input.GoldTurns {
		gold[turnID] = struct{}{}
	}
	if len(gold) == 0 {
		return
	}
	b.metricQuestions++
	candidateGold := coveredGoldTurns(input.Candidates, input.ChunkTurns, gold)
	selectedGold := coveredGoldTurns(input.Selected, input.ChunkTurns, gold)
	b.candidateGold += len(candidateGold)
	b.selectedGold += len(selectedGold)
	if len(selectedGold) < len(candidateGold) {
		b.complementDrops++
	}

	selectedNames := make(map[string]struct{}, len(input.Selected))
	for _, selected := range input.Selected {
		selectedNames[selected.Name] = struct{}{}
	}
	anchorViolation := false
	for i := 0; i < len(input.Candidates) && i < 2; i++ {
		if _, ok := selectedNames[input.Candidates[i].Name]; !ok {
			anchorViolation = true
		}
	}
	if anchorViolation {
		b.AnchorViolation++
	}
}

func coveredGoldTurns(results []memory.Result, chunkTurns map[string][]string, gold map[string]struct{}) map[string]struct{} {
	covered := make(map[string]struct{})
	for _, result := range results {
		for _, turnID := range chunkTurns[result.Name] {
			if _, ok := gold[turnID]; ok {
				covered[turnID] = struct{}{}
			}
		}
	}
	return covered
}

func (b *coverageBucket) add(turn, session float64) {
	b.N++
	b.turnSum += turn
	b.sessionSum += session
}

func (b *coverageBucket) finalize() {
	if b.N > 0 {
		b.TurnRecall = b.turnSum / float64(b.N)
		b.SessionRecall = b.sessionSum / float64(b.N)
	}
	b.SelectionSurvival = 1
	if b.candidateGold > 0 {
		b.SelectionSurvival = float64(b.selectedGold) / float64(b.candidateGold)
	}
	if b.metricQuestions > 0 {
		b.ComplementDrop = float64(b.complementDrops) / float64(b.metricQuestions)
	}
}

// coverageArmReport is one arm's finished coverage figures, overall and split by
// LoCoMo category (multi-hop is the target hole).
type coverageArmReport struct {
	Arm        string                     `json:"arm"`
	TopK       int                        `json:"top_k"`
	Overall    *coverageBucket            `json:"overall"`
	ByCategory map[string]*coverageBucket `json:"by_category"`
}

// coverageAccumulator collects one arm's per-category recall across conversations
// that grade concurrently, so add() is mutex-guarded.
type coverageAccumulator struct {
	arm        string
	topK       int
	mu         sync.Mutex
	overall    *coverageBucket
	byCategory map[string]*coverageBucket
	selector   bool
}

func newCoverageAccumulator(arm string, topK int) *coverageAccumulator {
	spec, _ := parseArm(arm)
	return &coverageAccumulator{
		arm:        arm,
		topK:       topK,
		overall:    &coverageBucket{},
		byCategory: map[string]*coverageBucket{},
		selector:   spec.mechanisms["pcic"] || spec.mechanisms["oracle"],
	}
}

func (a *coverageAccumulator) addSelection(category string, input selectionMetricInput) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.overall.addSelectionMetrics(input)
	bucket := a.byCategory[category]
	if bucket == nil {
		bucket = &coverageBucket{}
		a.byCategory[category] = bucket
	}
	bucket.addSelectionMetrics(input)
}

func (a *coverageAccumulator) add(category string, turn, session float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.overall.add(turn, session)
	bucket := a.byCategory[category]
	if bucket == nil {
		bucket = &coverageBucket{}
		a.byCategory[category] = bucket
	}
	bucket.add(turn, session)
}

func (a *coverageAccumulator) report() coverageArmReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.overall.finalize()
	for _, bucket := range a.byCategory {
		bucket.finalize()
	}
	if !a.selector {
		a.overall.SelectionSurvival = 1
		a.overall.AnchorViolation = 0
		for _, bucket := range a.byCategory {
			bucket.SelectionSurvival = 1
			bucket.AnchorViolation = 0
		}
	}
	return coverageArmReport{
		Arm:        a.arm,
		TopK:       a.topK,
		Overall:    a.overall,
		ByCategory: a.byCategory,
	}
}

// computeCoverage runs retrieval-only across every arm and grades each QA on
// exact-turn and session recall — NO answer or judge LLM call is made, so the
// whole bake-off costs only the one-time store build (and query embeddings from
// the local sidecar for hybrid arms). The IDK-tail escalation and the LLM
// listwise filter are deliberately skipped: this measures raw first-round
// retrieval coverage, the lever a later paid answer eval would depend on.
func computeCoverage(ctx context.Context, opt options, convs []conversation, runtimes []*conversationRuntime, arms []string, logger *slog.Logger) ([]coverageArmReport, error) {
	multiArm := len(arms) > 1
	accs := make(map[string]*coverageAccumulator, len(arms))
	for _, arm := range arms {
		accs[arm] = newCoverageAccumulator(arm, optionsForRun(opt, arm, multiArm).topK)
	}

	concurrency := opt.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for ci := range convs {
		wg.Add(1)
		go func(conv conversation) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			index := conv.ID
			if index < 0 || index >= len(runtimes) || runtimes[index] == nil {
				logger.Warn("conversation runtime unavailable", "conversation", conv.ID)
				return
			}
			rt := runtimes[index]
			for _, sq := range selectQuestions(conv, opt) {
				for _, arm := range arms {
					retriever := rt.retrievers[arm]
					if retriever == nil {
						continue
					}
					armOpt := optionsForRun(opt, arm, multiArm)
					topK, quota := armOpt.retrievalFor(sq.QA.Category)
					selector, trace := selectorForArm(rt, conv.ID, arm, armOpt, sq.QA.Evidence, true)
					hits, _, err := retrieveWithQuotaDiagnostics(ctx, retriever, sq.QA.Question, topK, quota, selector)
					if err != nil {
						logger.Warn("coverage retrieve failed", "conversation", conv.ID, "arm", arm, "err", err)
						continue
					}
					turn, session, gradeable := evidenceRecallAt(sq.QA, hits, rt.chunkTurns)
					if !gradeable {
						continue
					}
					accs[arm].add(sq.QA.CategoryName, turn, session)
					accs[arm].addSelection(sq.QA.CategoryName, selectionMetricInput{
						Candidates: trace.Candidates,
						Selected:   trace.Selected,
						GoldTurns:  sq.QA.Evidence,
						ChunkTurns: rt.chunkTurns,
					})
				}
			}
		}(convs[ci])
	}
	wg.Wait()

	reports := make([]coverageArmReport, 0, len(arms))
	for _, arm := range arms {
		reports = append(reports, accs[arm].report())
	}
	return reports, nil
}

// runCoverage is the --coverage-only entry point: compute, persist, and print.
func runCoverage(ctx context.Context, opt options, convs []conversation, runtimes []*conversationRuntime, arms []string, logger *slog.Logger) error {
	if !opt.chunks {
		logger.Warn("coverage-only without --chunks: exact-turn recall will be zero (facts carry no turn provenance); only session recall is meaningful")
	}
	reports, err := computeCoverage(ctx, opt, convs, runtimes, arms, logger)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(opt.runDir, "coverage.json"), reports); err != nil {
		return fmt.Errorf("write coverage.json: %w", err)
	}
	printCoverageSummary(reports)
	return nil
}

// printCoverageSummary renders a category-major turn@k matrix across arms — the
// view that answers "which lever raises evidence coverage on multi-hop?". The
// full session-recall figures live in coverage.json.
func printCoverageSummary(reports []coverageArmReport) {
	if len(reports) == 0 {
		return
	}
	fmt.Println("coverage bake-off (retrieval-only, exact-turn recall; no answer/judge)")
	fmt.Printf("  arms: ")
	for i, r := range reports {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%s (top_k=%d, n=%d)", r.Arm, r.TopK, r.Overall.N)
	}
	fmt.Println()

	categories := unionCoverageCategories(reports)
	fmt.Printf("  %-24s", "turn@k by category")
	for _, r := range reports {
		fmt.Printf(" %14s", r.Arm)
	}
	fmt.Println()
	for _, cat := range categories {
		fmt.Printf("  %-24s", cat)
		for _, r := range reports {
			fmt.Printf(" %14s", formatCoverageCell(r.ByCategory[cat]))
		}
		fmt.Println()
	}
	fmt.Printf("  %-24s", "OVERALL")
	for _, r := range reports {
		fmt.Printf(" %14.3f", r.Overall.TurnRecall)
	}
	fmt.Println()
}

func formatCoverageCell(b *coverageBucket) string {
	if b == nil || b.N == 0 {
		return "-"
	}
	return fmt.Sprintf("%.3f (%d)", b.TurnRecall, b.N)
}

// unionCoverageCategories is the sorted union of category keys seen by any arm,
// so the matrix has a stable, complete row set even when arms grade disjoint
// category subsets.
func unionCoverageCategories(reports []coverageArmReport) []string {
	seen := map[string]struct{}{}
	for _, r := range reports {
		for cat := range r.ByCategory {
			seen[cat] = struct{}{}
		}
	}
	cats := make([]string, 0, len(seen))
	for cat := range seen {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return cats
}
