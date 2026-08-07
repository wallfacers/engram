package main

// Notebook (--notebook): per-run mistake attribution + cross-run accumulation.
//
// The harness already persists per-question results (question/gold/predicted/
// correct/token) in results-<arm>.jsonl, and the formal protocol (--eval-protocol)
// derives a three-way miss class via classifyEvalMiss. But everyday eval runs use
// the legacy path where that attribution is missing — the exact "which of the
// three layers dropped the gold" signal the maintainer needs to stop blind-firing
// new mechanisms. This file adds:
//
//  1. inline attribution capture (gold_resolved / candidate_covered /
//     bundle_covered) computed against the ACTUAL candidate set and ACTUAL answer
//     context, persisted on each result row under --notebook;
//  2. a cross-run notebook.jsonl accumulator (dedupe by run_id+question_id+arm);
//  3. a markdown mistake book (per-run mistakes-<run_id>.md + index.md);
//  4. an optional LLM "how to solve this class next time" draft (--notebook-advise),
//     written to advice-<run_id>.md for human review.
//
// All attribution is deterministic and offline (lexical/set matching only, mirroring
// attribution.go). When --notebook is off every change here is a no-op and the
// persisted results stay byte-identical (SC-004).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
)

// evalNotebookAttribution is the per-repetition gold attribution captured inline
// during a --notebook run. Only small scalars are persisted (not candidate texts)
// so the result row stays lean; the notebook aggregate derives the three-way miss
// class from these plus majority correctness. The candidate and bundle evidence
// ID lists ARE persisted so the coverage verdicts can be re-derived / audited
// offline (the lexical fact coverage at tau is deliberately conservative — the
// raw IDs let the maintainer re-judge with a different threshold without a rerun).
type evalNotebookAttribution struct {
	GoldResolved     bool     `json:"gold_resolved"`             // gold evidence parses to >=1 turn ID
	CandidateCovered bool     `json:"candidate_covered"`         // any candidate in the final pool covers a gold turn
	BundleCovered    bool     `json:"bundle_covered"`            // evidence actually placed in the answer context covers a gold turn
	BundleApprox     bool     `json:"bundle_approx,omitempty"`   // trace sidecar selects the bundle → may vary per rep
	CandidateCount   int      `json:"candidate_count,omitempty"` // len of the final candidate pool
	CandidateIDs     []string `json:"candidate_ids,omitempty"`   // final candidate pool source IDs (for offline re-attribution)
	BundleEvidenceIDs []string `json:"bundle_evidence_ids,omitempty"` // evidence actually in the answer context (source IDs)
	ContextPreview   string   `json:"context_preview,omitempty"` // first N chars of the actual answer context
}

// computeNotebookAttribution derives a question's gold attribution from the final
// candidate set (hits) and the evidence actually placed in the answer context
// (contextEvidence). goldTurns is parsedGoldTurns(qa.Evidence). The candidate /
// bundle ID lists are captured verbatim for offline re-derivation.
func computeNotebookAttribution(hits, contextEvidence []memory.Result, chunkTurns map[string][]string, turnText map[string]string, goldTurns []string, tau float64) evalNotebookAttribution {
	return evalNotebookAttribution{
		GoldResolved:      len(goldTurns) > 0,
		CandidateCovered:  evidenceCoversGold(hits, chunkTurns, turnText, goldTurns, tau),
		BundleCovered:     evidenceCoversGold(contextEvidence, chunkTurns, turnText, goldTurns, tau),
		CandidateCount:    len(hits),
		CandidateIDs:      resultNames(hits),
		BundleEvidenceIDs: resultNames(contextEvidence),
	}
}

// defaultNotebookFactTau is the notebook attribution's fact-coverage threshold.
// Deliberately below defaultFactCoverageTau (0.8): the notebook's job is to flag
// "the gold is plausibly in the candidate/bundle" so the maintainer can audit the
// evidence IDs offline, not to prove lexical containment. Verified against p0-diag2:
// at 0.8 the fact path under-reports bundle coverage badly (contexts that plainly
// carried the gold answer were flagged uncovered); 0.4 keeps the chunk path exact
// while letting compressed facts through. Tune per run with --notebook-fact-tau.
const defaultNotebookFactTau = 0.4

// resultNames extracts the source IDs of a result set, preserving order.
func resultNames(results []memory.Result) []string {
	if len(results) == 0 {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Name)
	}
	return out
}

// evidenceCoversGold reports whether any evidence unit covers any gold turn,
// reusing hitMappedGoldTurns (chunk: exact turn-id overlap; fact: session-gated
// lexical containment). Deterministic — reruns stay byte-identical.
func evidenceCoversGold(evidence []memory.Result, chunkTurns map[string][]string, turnText map[string]string, goldTurns []string, tau float64) bool {
	for _, hit := range evidence {
		if len(hitMappedGoldTurns(hit, chunkTurns, turnText, goldTurns, tau)) > 0 {
			return true
		}
	}
	return false
}

// classifyNotebookMiss labels WHY a question was wrong. Unlike
// classifyEvalMiss (weakest-link ordering: a correct question with weak
// retrieval is still classified by its pipeline gap), this gates on
// majorityCorrect FIRST: an answered question is always success — the
// mistake book must contain only genuine misses, or its per-class counts and
// "next time, solve it like this" notes get polluted by correct answers. The
// pipeline gaps stay visible per-record via the gold_resolved /
// candidate_covered / bundle_covered booleans (e.g. "correct despite
// candidate miss" fragility is queryable in notebook.jsonl), just not
// conflated into miss_class. The boolean attribution is captured inline
// (coverage is all-or-nothing here: any gold turn covered by any unit counts
// as covered).
func classifyNotebookMiss(goldResolved, candidateCovered, bundleCovered, majorityCorrect bool) evalMissClass {
	if majorityCorrect {
		return evalMissSuccess
	}
	switch {
	case !goldResolved:
		return evalMissGoldUnresolved
	case !candidateCovered:
		return evalMissCandidate
	case !bundleCovered:
		return evalMissCompiler
	default:
		return evalMissAnswerer
	}
}

// notebookRecord is one row of the cross-run accumulator.
type notebookRecord struct {
	RunID            string `json:"run_id"`
	RunFlags         string `json:"run_flags"`
	RetrievalArm     string `json:"retrieval_arm"`
	QuestionID       string `json:"question_id"`
	Conv             int    `json:"conv"`
	Q                int    `json:"q"`
	Category         int    `json:"category"`
	CategoryName     string `json:"category_name,omitempty"`
	Question         string `json:"question"`
	Gold             string `json:"gold"`
	Predicted        string `json:"predicted"`
	MajorityCorrect  bool   `json:"majority_correct"`
	MissClass        string `json:"miss_class"`
	GoldResolved     bool   `json:"gold_resolved"`
	CandidateCovered bool   `json:"candidate_covered"`
	BundleCovered    bool     `json:"bundle_covered"`
	BundleApprox     bool     `json:"bundle_approx"`
	AnswerContextTok int      `json:"answer_context_tokens"`
	CandidateCount   int      `json:"candidate_count"`
	BundleEvidenceIDs []string `json:"bundle_evidence_ids,omitempty"` // source IDs actually in the answer context (audit / offline re-attribution)
	ContextPreview   string   `json:"context_preview,omitempty"`
	Notes            string `json:"notes,omitempty"` // human-revised solution note; LLM draft goes to advice-<run_id>.md
	ImportedAt       string `json:"imported_at"`
}

const notebookContextPreviewLen = 600

// buildNotebookRecord renders one accumulated row from a coalesced question.
func buildNotebookRecord(nr notebookResult, runID, runFlags string, importedAt time.Time) notebookRecord {
	item := nr.item
	att := nr.attribution
	rec := notebookRecord{
		RunID:            runID,
		RunFlags:         runFlags,
		RetrievalArm:     nr.arm,
		QuestionID:       item.QuestionID,
		Conv:             item.Conv,
		Q:                item.Q,
		Category:         item.Category,
		CategoryName:     categoryNameFor(item.Category, item.CategoryName),
		Question:         item.Question,
		Gold:             item.Gold,
		Predicted:        item.Predicted,
		MajorityCorrect:  item.Correct,
		GoldResolved:     att.GoldResolved,
		CandidateCovered: att.CandidateCovered,
		BundleCovered:    att.BundleCovered,
		BundleApprox:     att.BundleApprox,
		AnswerContextTok: item.AnswerContextTokens,
		CandidateCount:   att.CandidateCount,
		BundleEvidenceIDs: att.BundleEvidenceIDs,
		ContextPreview:   truncateRunes(att.ContextPreview, notebookContextPreviewLen),
		ImportedAt:       importedAt.UTC().Format(time.RFC3339),
	}
	rec.MissClass = string(classifyNotebookMiss(
		rec.GoldResolved, rec.CandidateCovered, rec.BundleCovered, rec.MajorityCorrect))
	return rec
}

func truncateRunes(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func categoryNameFor(category int, catName string) string {
	if catName != "" {
		return catName
	}
	return fmt.Sprintf("category-%d", category)
}

// notebookResult coalesces one question across all repetitions of a run.
type notebookResult struct {
	arm         string
	item        result
	repCount    int
	attribution evalNotebookAttribution
}

// collectNotebookResults globs every results-<arm>.jsonl repetition (flat and
// run-N subdirs) and coalesces per-question rows. Attribution is merged across
// reps: resolved/candidate/bundle = covered by any rep; bundle rows under trace
// are marked approximate. Majority correctness uses strict > half.
func collectNotebookResults(runDir string) ([]notebookResult, error) {
	paths, err := resultsGlob(runDir)
	if err != nil {
		return nil, err
	}
	type key struct{ conv, q int }
	byArm := map[string]map[key][]result{}
	for _, p := range paths {
		arm := resultsArmFromPath(p)
		rows, err := readResultsJSONL(p)
		if err != nil {
			return nil, fmt.Errorf("notebook: read %s: %w", p, err)
		}
		if byArm[arm] == nil {
			byArm[arm] = map[key][]result{}
		}
		for _, r := range rows {
			byArm[arm][key{r.Conv, r.Q}] = append(byArm[arm][key{r.Conv, r.Q}], r)
		}
	}
	var out []notebookResult
	for arm, m := range byArm {
		for k, reps := range m {
			nr := notebookResult{arm: arm, item: reps[0], repCount: len(reps)}
			for _, r := range reps {
				if r.Notebook == nil {
					continue
				}
				nr.attribution.GoldResolved = nr.attribution.GoldResolved || r.Notebook.GoldResolved
				nr.attribution.CandidateCovered = nr.attribution.CandidateCovered || r.Notebook.CandidateCovered
				nr.attribution.BundleCovered = nr.attribution.BundleCovered || r.Notebook.BundleCovered
				nr.attribution.BundleApprox = nr.attribution.BundleApprox || r.Notebook.BundleApprox
				if r.Notebook.CandidateCount > nr.attribution.CandidateCount {
					nr.attribution.CandidateCount = r.Notebook.CandidateCount
				}
				if nr.attribution.ContextPreview == "" {
					nr.attribution.ContextPreview = r.Notebook.ContextPreview
				}
				if len(nr.attribution.BundleEvidenceIDs) == 0 && len(r.Notebook.BundleEvidenceIDs) > 0 {
					nr.attribution.BundleEvidenceIDs = r.Notebook.BundleEvidenceIDs
				}
			}
			majority := 0
			for _, r := range reps {
				if r.Correct {
					majority++
				}
			}
			nr.item.Correct = majority*2 > len(reps)
			out = append(out, nr)
			_ = k
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].arm != out[j].arm {
			return out[i].arm < out[j].arm
		}
		if out[i].item.Conv != out[j].item.Conv {
			return out[i].item.Conv < out[j].item.Conv
		}
		return out[i].item.Q < out[j].item.Q
	})
	return out, nil
}

// resultsGlob returns every results-*.jsonl path under runDir, including run-N
// repetition subdirectories, sorted.
func resultsGlob(runDir string) ([]string, error) {
	flat, err := filepath.Glob(filepath.Join(runDir, "results-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sub, err := filepath.Glob(filepath.Join(runDir, "run-*", "results-*.jsonl"))
	if err != nil {
		return nil, err
	}
	all := append(flat, sub...)
	sort.Strings(all)
	if len(all) == 0 {
		return nil, fmt.Errorf("notebook: no results-*.jsonl under %s", runDir)
	}
	return all, nil
}

// resultsArmFromPath extracts the retrieval arm from "results-<arm>.jsonl".
func resultsArmFromPath(p string) string {
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimPrefix(base, "results-")
	return base
}

// digest uniquely identifies one accumulated row for dedupe.
func (r notebookRecord) digest() string {
	h := sha256.New()
	h.Write([]byte(r.RunID))
	h.Write([]byte("\x00"))
	h.Write([]byte(r.QuestionID))
	h.Write([]byte("\x00"))
	h.Write([]byte(r.RetrievalArm))
	return hex.EncodeToString(h.Sum(nil))
}

func loadNotebookRecords(path string) ([]notebookRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []notebookRecord
	dec := json.NewDecoder(f)
	for {
		var rec notebookRecord
		if err := dec.Decode(&rec); err != nil {
			if err.Error() == "EOF" {
				break
			}
			// tolerate a torn tail line but not mid-file corruption
			if len(out) > 0 {
				break
			}
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// appendNotebookRecords dedupes by run_id+question_id+arm and appends.
func appendNotebookRecords(path string, recs []notebookRecord) (added int, err error) {
	existing, err := loadNotebookRecords(path)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		seen[r.digest()] = true
	}
	var fresh []notebookRecord
	for _, r := range recs {
		if seen[r.digest()] {
			continue
		}
		seen[r.digest()] = true
		fresh = append(fresh, r)
	}
	if len(fresh) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range fresh {
		if err := enc.Encode(r); err != nil {
			return 0, err
		}
	}
	return len(fresh), nil
}

// notebookSummary is the per-run headline rendered into index.md.
type notebookSummary struct {
	RunID       string         `json:"run_id"`
	RunFlags    string         `json:"run_flags"`
	ImportedAt  string         `json:"imported_at"`
	Total       int            `json:"total"`
	Correct     int            `json:"correct"`
	MissClasses map[string]int `json:"miss_classes"`
	CategoryAcc map[string]int `json:"category_acc"` // category → correct
	Counts      map[string]int `json:"counts"`       // category → total
}

// summarizeNotebook aggregates one run's records.
func summarizeNotebook(recs []notebookRecord) notebookSummary {
	sum := notebookSummary{
		MissClasses: map[string]int{}, CategoryAcc: map[string]int{}, Counts: map[string]int{},
	}
	for _, r := range recs {
		sum.Total++
		if r.MajorityCorrect {
			sum.Correct++
		}
		sum.MissClasses[r.MissClass]++
		sum.Counts[r.CategoryName]++
		if r.MajorityCorrect {
			sum.CategoryAcc[r.CategoryName]++
		}
	}
	return sum
}

// writeNotebook accumulates one run into notebook.jsonl, writes the mistake book
// and refreshes the index. Called after stats are finalized.
func writeNotebook(ctx context.Context, opt options, runID string, importedAt time.Time, notebookDir string, advise func(context.Context, string) (string, error)) (notebookSummary, error) {
	results, err := collectNotebookResults(opt.runDir)
	if err != nil {
		return notebookSummary{}, fmt.Errorf("notebook: collect results: %w", err)
	}
	runFlags := notebookRunFlags(opt)
	var recs []notebookRecord
	for _, nr := range results {
		recs = append(recs, buildNotebookRecord(nr, runID, runFlags, importedAt))
	}
	if err := os.MkdirAll(notebookDir, 0o755); err != nil {
		return notebookSummary{}, fmt.Errorf("notebook: mkdir %s: %w", notebookDir, err)
	}
	if _, err := appendNotebookRecords(filepath.Join(notebookDir, "notebook.jsonl"), recs); err != nil {
		return notebookSummary{}, fmt.Errorf("notebook: append accumulator: %w", err)
	}
	summary := summarizeNotebook(recs)
	summary.RunID, summary.RunFlags, summary.ImportedAt = runID, runFlags, importedAt.UTC().Format(time.RFC3339)
	if err := writeMistakeBook(notebookDir, runID, runFlags, importedAt, recs); err != nil {
		return notebookSummary{}, fmt.Errorf("notebook: write mistake book: %w", err)
	}
	if err := refreshNotebookIndex(notebookDir); err != nil {
		return notebookSummary{}, fmt.Errorf("notebook: refresh index: %w", err)
	}
	if advise != nil {
		if advice, adviseErr := advise(ctx, notebookAdvisePrompt(recs)); adviseErr != nil {
			return summary, fmt.Errorf("notebook: advise failed: %w", adviseErr)
		} else if advice != "" {
			if err := os.WriteFile(filepath.Join(notebookDir, "advice-"+runID+".md"), []byte(advice), 0o600); err != nil {
				return summary, fmt.Errorf("notebook: write advice: %w", err)
			}
		}
	}
	return summary, nil
}

func notebookRunFlags(opt options) string {
	return strings.Join([]string{
		"--retrieval " + opt.retrieval,
		fmt.Sprintf("--top-k %d", opt.topK),
		fmt.Sprintf("--chunk-quota %d", opt.chunkQuota),
		fmt.Sprintf("--chunks %v", opt.chunks),
	}, " ")
}

// notebookGroup groups mistakes by miss class + category for the advice draft.
type notebookGroup struct {
	missClass string
	cat       string
}

// notebookAdvisePrompt asks the LLM for a per-class "how to solve this next time"
// draft. Mistakes are grouped by miss class and category; the answerer never sees
// gold answers (only the miss class + the wrong predicted), so the advice is
// constrained to retrieval/assembly/prompt levers, not answer memorization.
func notebookAdvisePrompt(recs []notebookRecord) string {
	var b strings.Builder
	b.WriteString("You are reviewing a batch of failed long-conversation memory questions from a LoCoMo eval run.\n")
	b.WriteString("For each failure class below, explain in Chinese (简体中文) WHY that class of mistake happens and give ONE concrete, actionable fix for next time (retrieval / evidence-assembly / answer-prompt / evaluation levers only — never 'use a stronger model' as the only answer).\n")
	b.WriteString("Keep each class to 3-6 sentences. These drafts will be reviewed by a human, so be specific and honest; mark anything you are unsure about as [需要验证].\n\n")
	type group = notebookGroup
	order := []evalMissClass{evalMissCandidate, evalMissCompiler, evalMissAnswerer, evalMissGoldUnresolved}
	byGroup := map[group][]notebookRecord{}
	for _, r := range recs {
		if r.MajorityCorrect {
			continue
		}
		byGroup[group{r.MissClass, r.CategoryName}] = append(byGroup[group{r.MissClass, r.CategoryName}], r)
	}
	for _, mc := range order {
		for _, cat := range sortedCategoryNames(byGroup, string(mc)) {
			g := group{string(mc), cat}
			rows := byGroup[g]
			if len(rows) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s / %s (%d 题)\n", g.missClass, g.cat, len(rows)))
			shown := rows
			if len(shown) > 5 {
				shown = shown[:5]
			}
			for _, r := range shown {
				b.WriteString(fmt.Sprintf("- Q: %s\n  gold: %s\n  predicted: %s\n", r.Question, r.Gold, r.Predicted))
			}
			if len(rows) > len(shown) {
				b.WriteString(fmt.Sprintf("- … 另有 %d 题同类\n", len(rows)-len(shown)))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func sortedCategoryNames(byGroup map[notebookGroup][]notebookRecord, missClass string) []string {
	var out []string
	seen := map[string]bool{}
	for g := range byGroup {
		if g.missClass == missClass && !seen[g.cat] {
			seen[g.cat] = true
			out = append(out, g.cat)
		}
	}
	sort.Strings(out)
	return out
}

// writeMistakeBook renders the per-run markdown mistake book.
func writeMistakeBook(dir, runID, runFlags string, importedAt time.Time, recs []notebookRecord) error {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# 错题本 %s\n\n", runID))
	b.WriteString(fmt.Sprintf("> flags: `%s` · imported: %s\n\n", runFlags, importedAt.UTC().Format(time.RFC3339)))

	summary := summarizeNotebook(recs)
	acc := 0.0
	if summary.Total > 0 {
		acc = 100 * float64(summary.Correct) / float64(summary.Total)
	}
	b.WriteString(fmt.Sprintf("总分 **%d/%d (%.2f%%)**\n\n", summary.Correct, summary.Total, acc))
	b.WriteString("| miss_class | 题数 |\n|---|---|\n")
	for _, mc := range []evalMissClass{evalMissCandidate, evalMissCompiler, evalMissAnswerer, evalMissGoldUnresolved, evalMissSuccess} {
		n := summary.MissClasses[string(mc)]
		if n == 0 {
			continue
		}
		label := map[string]string{
			string(evalMissCandidate): "candidate_miss(检索漏了)", string(evalMissCompiler): "compiler_miss(在池未进bundle)",
			string(evalMissAnswerer): "answerer_miss(模型答错)", string(evalMissGoldUnresolved): "gold_unresolved",
			string(evalMissSuccess): "success", string(evalMissResolution): "resolution_miss",
		}[string(mc)]
		b.WriteString(fmt.Sprintf("| %s | %d |\n", label, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b.WriteString("\n## 逐题(按归因分组)\n\n")

	order := []evalMissClass{evalMissCandidate, evalMissCompiler, evalMissAnswerer, evalMissGoldUnresolved, evalMissSuccess}
	grouped := map[string][]notebookRecord{}
	for _, r := range recs {
		grouped[r.MissClass] = append(grouped[r.MissClass], r)
	}
	for _, mc := range order {
		rows := grouped[string(mc)]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].CategoryName != rows[j].CategoryName {
				return rows[i].CategoryName < rows[j].CategoryName
			}
			return rows[i].QuestionID < rows[j].QuestionID
		})
		label := map[string]string{
			string(evalMissCandidate):      "candidate_miss — 检索漏了(gold 不在候选池)",
			string(evalMissCompiler):       "compiler_miss — 在池未进 bundle(组装丢了)",
			string(evalMissAnswerer):       "answerer_miss — gold 已进 context 仍答错(模型/提示)",
			string(evalMissGoldUnresolved): "gold_unresolved — gold 无法映射",
			string(evalMissSuccess):        "success(答对)",
		}[string(mc)]
		b.WriteString(fmt.Sprintf("## %s (%d)\n\n", label, len(rows)))
		b.WriteString("| 类别 | 题 | 问题 | gold | predicted | tok | 备注 |\n|---|---|---|---|---|---|---|\n")
		for _, r := range rows {
			approx := ""
			if r.BundleApprox {
				approx = "bundle≈"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %d | %s |\n",
				r.CategoryName, r.QuestionID, cell(r.Question), cell(r.Gold), cell(r.Predicted), r.AnswerContextTok, approx))
			if r.ContextPreview != "" {
				b.WriteString(fmt.Sprintf("\n<details><summary>answer context 预览</summary>\n\n```\n%s\n```\n\n</details>\n", r.ContextPreview))
			}
			b.WriteString(fmt.Sprintf("\n**解法笔记(人工填写):**\n\n\n"))
		}
	}
	path := filepath.Join(dir, "mistakes-"+runID+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return nil
}

// refreshNotebookIndex re-renders index.md from the accumulated notebook.jsonl.
func refreshNotebookIndex(notebookDir string) error {
	recs, err := loadNotebookRecords(filepath.Join(notebookDir, "notebook.jsonl"))
	if err != nil {
		return err
	}
	byRun := map[string][]notebookRecord{}
	for _, r := range recs {
		byRun[r.RunID] = append(byRun[r.RunID], r)
	}
	var runIDs []string
	for id := range byRun {
		runIDs = append(runIDs, id)
	}
	sort.Strings(runIDs)

	var b strings.Builder
	b.WriteString("# 错题笔记本索引\n\n")
	b.WriteString("> 累积文件 `notebook.jsonl`;每 run 错题本 `mistakes-<run_id>.md`;解法草案 `advice-<run_id>.md`。\n\n")
	b.WriteString("| run | 题数 | 正确率 | candidate_miss | compiler_miss | answerer_miss | gold_unresolved |\n|---|---|---|---|---|---|---|\n")
	for _, id := range runIDs {
		s := summarizeNotebook(byRun[id])
		acc := 0.0
		if s.Total > 0 {
			acc = 100 * float64(s.Correct) / float64(s.Total)
		}
		b.WriteString(fmt.Sprintf("| %s | %d | %.2f%% | %d | %d | %d | %d |\n",
			id, s.Total, acc,
			s.MissClasses[string(evalMissCandidate)], s.MissClasses[string(evalMissCompiler)],
			s.MissClasses[string(evalMissAnswerer)], s.MissClasses[string(evalMissGoldUnresolved)]))
	}
	if err := os.WriteFile(filepath.Join(notebookDir, "index.md"), []byte(b.String()), 0o600); err != nil {
		return err
	}
	return nil
}

// notebookRunID derives a unique, human-sortable id for this run from the run
// directory name + a timestamp. Cross-run dedupe keys on run_id + question_id +
// arm, so rerunning the same directory later still gets a distinct id.
func notebookRunID(runDir string) string {
	base := filepath.Base(strings.TrimRight(runDir, string(filepath.Separator)))
	ts := time.Now().Format("20060102-150405")
	id := base
	if id == "" || id == "." || id == "/" {
		id = "run"
	}
	return fmt.Sprintf("%s-%s", ts, id)
}

const notebookAdviseSystemPrompt = `You are a memory-systems evaluation analyst reviewing a batch of failed questions from a LoCoMo long-conversation memory eval run.
For each failure class in the user message, explain in Chinese (简体中文):
1. WHY that class of mistake happens in a retrieval+evidence-assembly+answer pipeline;
2. ONE concrete, actionable fix for next time — limited to retrieval / evidence-assembly / answer-prompt / evaluation levers.
Never give "use a stronger model" as the only fix. Keep each class to 3-6 sentences. Mark anything uncertain as [需要验证]. These are drafts a human will review.`

// cell escapes a table cell so long/emoji text does not break markdown.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) > 140 {
		s = string([]rune(s)[:140]) + "…"
	}
	return s
}
