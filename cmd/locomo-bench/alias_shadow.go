package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
)

const (
	aliasShadowOff       = "off"
	aliasShadowBaseline  = "baseline"
	aliasShadowTreatment = "treatment"
	aliasShadowTopK      = 30

	aliasShadowStoreDirName = "alias-store"
)

func aliasShadowEnabled(opt options) bool {
	return opt.aliasShadow == aliasShadowBaseline || opt.aliasShadow == aliasShadowTreatment
}

func validateAliasShadowOptions(opt options) error {
	switch opt.aliasShadow {
	case "", aliasShadowOff:
		return nil
	case aliasShadowBaseline, aliasShadowTreatment:
	default:
		return fmt.Errorf("--alias-shadow must be off, baseline, or treatment, got %q", opt.aliasShadow)
	}
	if opt.topK != aliasShadowTopK {
		return fmt.Errorf("--alias-shadow requires --top-k %d, got %d", aliasShadowTopK, opt.topK)
	}
	for category, topK := range opt.catTopK {
		if topK != aliasShadowTopK {
			return fmt.Errorf("--alias-shadow requires category %d top-k to remain %d, got %d", category, aliasShadowTopK, topK)
		}
	}
	if opt.multiQuery {
		return fmt.Errorf("--alias-shadow and --multi-query are mutually exclusive")
	}
	if strings.TrimSpace(opt.storeDir) == "" {
		return fmt.Errorf("--alias-shadow requires --store-dir; in-memory stores cannot protect the canonical artifact")
	}
	if strings.TrimSpace(opt.runDir) == "" {
		return fmt.Errorf("--alias-shadow requires --run-dir for the isolated store copy")
	}
	return nil
}

// prepareAliasShadowStore copies the canonical stores before any runtime opens
// them, then redirects every downstream store access to the run-local copy.
func prepareAliasShadowStore(opt *options) error {
	if opt == nil || !aliasShadowEnabled(*opt) || opt.aliasShadowPrepared {
		return nil
	}
	source := opt.storeDir
	destination := filepath.Join(opt.runDir, aliasShadowStoreDirName)
	if err := copyStoreDir(source, destination); err != nil {
		return fmt.Errorf("prepare alias-shadow store copy: %w", err)
	}
	opt.storeDir = destination
	opt.aliasShadowPrepared = true
	return nil
}

// copyStoreDir overwrites run-local copies on every invocation. This makes a
// resumed baseline/treatment run start from the canonical bytes again instead
// of inheriting shadow rows left by the other arm.
func copyStoreDir(src, dst string) error {
	sourcePath, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve canonical store dir: %w", err)
	}
	destinationPath, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve alias store dir: %w", err)
	}
	if sourcePath == destinationPath {
		return fmt.Errorf("canonical and alias store directories must differ: %s", sourcePath)
	}
	matches, err := filepath.Glob(filepath.Join(src, "conv*.db"))
	if err != nil {
		return fmt.Errorf("list canonical stores: %w", err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("no conv*.db stores found in %s", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create alias store dir: %w", err)
	}
	for _, databasePath := range matches {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			sourcePath := databasePath + suffix
			destinationPath := filepath.Join(dst, filepath.Base(sourcePath))
			if err := copyStoreFile(sourcePath, destinationPath); err != nil {
				if os.IsNotExist(err) && suffix != "" {
					// Remove a stale sidecar from an earlier run when the canonical
					// store no longer has that sidecar.
					if removeErr := os.Remove(destinationPath); removeErr != nil && !os.IsNotExist(removeErr) {
						return fmt.Errorf("remove stale store sidecar %s: %w", destinationPath, removeErr)
					}
					continue
				}
				return fmt.Errorf("copy %s: %w", sourcePath, err)
			}
		}
	}
	return nil
}

func copyStoreFile(src, dst string) (err error) {
	input, err := os.Open(src) //nolint:gosec // operator-selected benchmark store
	if err != nil {
		return err
	}
	defer input.Close() //nolint:errcheck
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.Create(dst) //nolint:gosec // destination is the run-local alias store
	if err != nil {
		return err
	}
	if err := output.Chmod(info.Mode().Perm()); err != nil {
		_ = output.Close()
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	_, err = io.Copy(output, input)
	return err
}

func enforceAliasShadowStoreMode(ctx context.Context, db *sql.DB, mode string) (int, error) {
	if mode == aliasShadowBaseline {
		if _, err := db.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE entry_name LIKE '%#alias'`); err != nil {
			return 0, fmt.Errorf("strip baseline alias-shadow vectors: %w", err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE entry_name LIKE '%#alias'`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count alias-shadow vectors: %w", err)
	}
	switch mode {
	case aliasShadowBaseline:
		if count != 0 {
			return count, fmt.Errorf("baseline alias-shadow invariant failed: count=%d, want 0", count)
		}
	case aliasShadowTreatment:
		if count == 0 {
			return 0, fmt.Errorf("treatment alias-shadow invariant failed: count=0, want >0")
		}
	}
	return count, nil
}

// attributedGoldFactNames uses the same provenance-aware mapping as
// buildAttributionTrace and returns only stored fact names, never chunk names.
func attributedGoldFactNames(qa locomoQA, hits []memory.Result, chunkTurns map[string][]string, goldTurnText map[string]string, tau float64) []string {
	goldTurns := parsedGoldTurns(qa.Evidence)
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, hit := range hits {
		if _, isChunk := chunkTurns[hit.Name]; isChunk {
			continue
		}
		if len(hitMappedGoldTurns(hit, chunkTurns, goldTurnText, goldTurns, tau)) == 0 {
			continue
		}
		if _, duplicate := seen[hit.Name]; duplicate {
			continue
		}
		seen[hit.Name] = struct{}{}
		names = append(names, hit.Name)
	}
	return names
}

func goldFactNamesHaveAlias(ctx context.Context, db *sql.DB, names []string) (bool, error) {
	for _, name := range names {
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT 1 FROM memory_event_aliases WHERE entry_name = ? LIMIT 1`, name).Scan(&exists)
		if err == nil {
			return true, nil
		}
		if err != sql.ErrNoRows {
			return false, fmt.Errorf("query gold aliases for %q: %w", name, err)
		}
	}
	return false, nil
}

type aliasShadowRecallQuestion struct {
	Conv              int     `json:"conv"`
	Q                 int     `json:"q"`
	Category          int     `json:"category"`
	QuestionID        string  `json:"question_id,omitempty"`
	Arm               string  `json:"arm"`
	GoldHasAlias      bool    `json:"gold_has_alias"`
	GoldResolved      bool    `json:"gold_resolved"`
	GoldRank          int     `json:"gold_rank"`
	GoldRankAt30      int     `json:"gold_rank_at_30"`
	Gradeable         bool    `json:"gradeable"`
	CoverageAt30      float64 `json:"coverage_at_30"`
	SessionCoverage30 float64 `json:"session_coverage_at_30"`
}

type aliasShadowRecallLayer struct {
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

type aliasShadowRecallReport struct {
	Mode            string                 `json:"mode"`
	TargetCategory  string                 `json:"target_category"`
	TopK            int                    `json:"top_k"`
	DeltaConvention string                 `json:"delta_convention"`
	Global          aliasShadowRecallLayer `json:"global"`
	GoldHasAlias    aliasShadowRecallLayer `json:"gold_has_alias"`
}

type aliasShadowRecallPair struct {
	baseline  aliasShadowRecallQuestion
	treatment aliasShadowRecallQuestion
}

func summarizeAliasShadowRecallDiagnostic(baseline, treatment []aliasShadowRecallQuestion, targetCategory int) (aliasShadowRecallReport, error) {
	report := aliasShadowRecallReport{
		Mode:            "retrieval_only",
		TargetCategory:  categoryLabel(targetCategory),
		TopK:            aliasShadowTopK,
		DeltaConvention: "rank_delta=treatment-baseline (negative improves); coverage_delta=treatment-baseline (positive improves)",
	}
	pairs, err := pairAliasShadowRecallQuestions(baseline, treatment)
	if err != nil {
		return report, err
	}
	report.Global = summarizeAliasShadowRecallLayer(pairs)
	withAlias := make([]aliasShadowRecallPair, 0, len(pairs))
	for _, pair := range pairs {
		if pair.baseline.GoldHasAlias {
			withAlias = append(withAlias, pair)
		}
	}
	report.GoldHasAlias = summarizeAliasShadowRecallLayer(withAlias)
	return report, nil
}

func pairAliasShadowRecallQuestions(baseline, treatment []aliasShadowRecallQuestion) ([]aliasShadowRecallPair, error) {
	type key struct{ conv, q int }
	baselineByKey := make(map[key]aliasShadowRecallQuestion, len(baseline))
	for _, question := range baseline {
		questionKey := key{question.Conv, question.Q}
		if _, duplicate := baselineByKey[questionKey]; duplicate {
			return nil, fmt.Errorf("duplicate baseline recall record: conv=%d q=%d", question.Conv, question.Q)
		}
		baselineByKey[questionKey] = question
	}
	pairs := make([]aliasShadowRecallPair, 0, len(treatment))
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
		if base.GoldHasAlias != question.GoldHasAlias {
			return nil, fmt.Errorf("gold_has_alias mismatch: conv=%d q=%d baseline=%t treatment=%t", question.Conv, question.Q, base.GoldHasAlias, question.GoldHasAlias)
		}
		seenTreatment[questionKey] = struct{}{}
		pairs = append(pairs, aliasShadowRecallPair{baseline: base, treatment: question})
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

func summarizeAliasShadowRecallLayer(pairs []aliasShadowRecallPair) aliasShadowRecallLayer {
	layer := aliasShadowRecallLayer{Questions: len(pairs)}
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

func contextParityArm(opt options) string {
	if aliasShadowEnabled(opt) {
		return opt.aliasShadow
	}
	if doc2queryEnabled(opt) {
		return opt.doc2query
	}
	return multiQueryArm(opt.multiQuery)
}

func validateAliasShadowContextParity(opt options, record contextParityRecord) error {
	if !aliasShadowEnabled(opt) {
		return nil
	}
	if record.FinalTopK != aliasShadowTopK {
		return fmt.Errorf("alias-shadow context parity failed for conv=%d q=%d arm=%s: final_top_k=%d, want %d", record.Conv, record.Q, record.Arm, record.FinalTopK, aliasShadowTopK)
	}
	if record.SubqueryCount != 1 {
		return fmt.Errorf("alias-shadow context parity failed for conv=%d q=%d arm=%s: subquery_count=%d, want 1", record.Conv, record.Q, record.Arm, record.SubqueryCount)
	}
	return nil
}

func runAliasShadowRecallDiagnosticWithClient(ctx context.Context, opt options, convs []conversation, arms []string, embClient embedding.Client, logger *slog.Logger) error {
	if err := validateRecallDiagnosticOptions(opt, arms); err != nil {
		return err
	}
	if !aliasShadowEnabled(opt) {
		return fmt.Errorf("alias-shadow recall diagnostic requires --alias-shadow baseline or treatment")
	}
	if embClient == nil {
		return fmt.Errorf("alias-shadow recall diagnostic requires a configured embedding client")
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create alias-shadow recall run dir: %w", err)
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
	questions := make([]aliasShadowRecallQuestion, 0)
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
		return "", fmt.Errorf("alias-shadow diagnostic must not call extraction")
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
				setErr(fmt.Errorf("prepare alias-shadow diagnostic conv=%d: %w", conv.ID, err))
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
					setErr(fmt.Errorf("alias-shadow diagnostic retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				hits := finalizeQuestionHits(diagnosticCtx, qa.Question, candidates, topK, quota, armOpt)
				wide := candidates
				wideK := attributionWidePool(topK, armOpt.widePool)
				if wideK != searchK {
					wide, err = retriever.Search(diagnosticCtx, qa.Question, wideK)
					if err != nil {
						setErr(fmt.Errorf("alias-shadow diagnostic wide retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
						return
					}
				}
				trace := buildAttributionTrace(conv.ID, selected.Index, qa, hits, wide, runtime.chunkTurns, goldTurnText, topK, 0, targetOpt.factCoverageTau, nil)
				coverage, sessionCoverage, gradeable := evidenceRecallAt(qa, hits, runtime.chunkTurns)
				goldFactNames := attributedGoldFactNames(qa, storedFacts, runtime.chunkTurns, goldTurnText, targetOpt.factCoverageTau)
				goldHasAlias, err := goldFactNamesHaveAlias(diagnosticCtx, runtime.store.DB(), goldFactNames)
				if err != nil {
					setErr(fmt.Errorf("classify gold aliases conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				record := aliasShadowRecallQuestion{
					Conv:              conv.ID,
					Q:                 selected.Index,
					Category:          qa.Category,
					QuestionID:        qa.QuestionID,
					Arm:               targetOpt.aliasShadow,
					GoldHasAlias:      goldHasAlias,
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
		return fmt.Errorf("alias-shadow diagnostic extraction calls=%d, want 0", got)
	}
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Conv != questions[j].Conv {
			return questions[i].Conv < questions[j].Conv
		}
		return questions[i].Q < questions[j].Q
	})

	armPath := aliasShadowRecallArmPath(opt.runDir, opt.aliasShadow)
	if opt.aliasShadow == aliasShadowBaseline {
		// A baseline invocation starts a fresh pair. The following treatment run
		// recreates its store copy and writes the combined delta report.
		for _, stale := range []string{
			aliasShadowRecallArmPath(opt.runDir, aliasShadowTreatment),
			filepath.Join(opt.runDir, "alias_shadow_recall.json"),
		} {
			if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale alias-shadow recall artifact %s: %w", stale, err)
			}
		}
	}
	if err := writeAliasShadowRecallQuestions(armPath, questions); err != nil {
		return fmt.Errorf("write %s alias-shadow recall records: %w", opt.aliasShadow, err)
	}
	if opt.aliasShadow == aliasShadowTreatment {
		baseline, err := readAliasShadowRecallQuestions(aliasShadowRecallArmPath(opt.runDir, aliasShadowBaseline))
		if err != nil {
			return fmt.Errorf("read baseline alias-shadow recall records (run baseline first): %w", err)
		}
		report, err := summarizeAliasShadowRecallDiagnostic(baseline, questions, targetOpt.onlyCategory)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(opt.runDir, "alias_shadow_recall.json"), report); err != nil {
			return fmt.Errorf("write alias-shadow recall report: %w", err)
		}
		fmt.Printf("alias-shadow recall: category=%s global_n=%d alias_n=%d alias coverage@30 delta=%+.3f, gold entered=%d left=%d\n",
			report.TargetCategory, report.Global.Questions, report.GoldHasAlias.Questions, report.GoldHasAlias.MeanCoverageAt30Delta,
			report.GoldHasAlias.GoldEnteredTop30, report.GoldHasAlias.GoldLeftTop30)
	}
	logger.Info("alias-shadow recall diagnostic complete", "arm", opt.aliasShadow, "questions", len(questions), "extract_callers", 0, "answer_callers", 0, "judge_callers", 0, "decomposition_callers", 0)
	return nil
}

func attributionStoredFacts(ctx context.Context, runtime *conversationRuntime) ([]memory.Result, error) {
	entries, err := runtime.entries.List(ctx)
	if err != nil {
		return nil, err
	}
	facts := make([]memory.Result, 0, len(entries))
	for _, entry := range entries {
		if _, isChunk := runtime.chunkTurns[entry.Name]; isChunk || entry.FactSource == "verbatim_chunk" {
			continue
		}
		facts = append(facts, memory.Result{Name: entry.Name, Content: entry.Content, SourceSessionID: entry.SourceSessionID})
	}
	return facts, nil
}

func aliasShadowRecallArmPath(runDir, arm string) string {
	return filepath.Join(runDir, "alias_shadow_recall_"+arm+".jsonl")
}

func writeAliasShadowRecallQuestions(path string, questions []aliasShadowRecallQuestion) error {
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

func readAliasShadowRecallQuestions(path string) ([]aliasShadowRecallQuestion, error) {
	file, err := os.Open(path) //nolint:gosec // run-local benchmark artifact
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck
	decoder := json.NewDecoder(file)
	var questions []aliasShadowRecallQuestion
	for decoder.More() {
		var question aliasShadowRecallQuestion
		if err := decoder.Decode(&question); err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	return questions, nil
}
