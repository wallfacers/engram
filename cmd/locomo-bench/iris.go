package main

// Feature 021 (US1): IRIS evidence-gap-driven iterative retrieval (EviMem,
// arXiv 2604.27695). Upgrades the harness's passive IDK-rewrite chain into an
// ACTIVE sufficiency-driven loop for temporal questions under a fixed
// (MemOS-aligned) context budget. The engine is untouched; this lives entirely
// in cmd/locomo-bench.
//
// MVP v1 design notes (see specs/021-iris-evidence-gap-retrieval/plan.md):
//   - Each round retrieves at the same topK (budget per round is unchanged).
//   - EvalSufficiency sees the ACCUMULATED evidence (richer context for judging
//     sufficiency); the answerer sees a SLOT-MERGED set capped at topK so the
//     answerer's context budget stays aligned with the flat-hybrid baseline
//     (no budget inflation). Half the slots reserve for round-0 anchors, half
//     for fresh gap-targeted evidence so the diagnosed gap actually enters the
//     answerer's context.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wallfacers/engram/memory"
)

const irisMaxDepthDefault = 3

// Convergence thresholds (EviMem IRIS calibration). Temporal questions require
// stricter confidence to terminate.
const (
	irisThetaGeneral  = 0.70
	irisThetaTemporal = 0.85
)

const sufficiencySystemPrompt = `You evaluate whether the retrieved memories are SUFFICIENT to answer a question, considering the accumulated evidence as a whole. Output STRICT JSON only: {"tier":"EXACT"|"INFERRABLE"|"PARTIAL","confidence":0.0,"missing":"short phrase"}.
- EXACT: a precise, direct answer is present in the memories.
- INFERRABLE: enough clues exist for a reasonable inference of the answer.
- PARTIAL: related evidence is present but a specific fact is still missing.
"confidence" is a number in [0,1]. "missing" names the SPECIFIC fact/evidence still needed to answer (empty string when EXACT). For temporal questions (ordering, intervals, "when did X happen or change"), verify there are enough dated [event: YYYY-MM-DD] anchors to determine the requested order or interval; if a needed date, event, or entity is absent, name it in "missing".`

type sufficiencyResult struct {
	Tier       string  `json:"tier"`
	Confidence float64 `json:"confidence"`
	Missing    string  `json:"missing"`
}

func evalSufficiency(ctx context.Context, call usageModelCaller, question string, hits []memory.Result, category int) (sufficiencyResult, error) {
	user := buildSufficiencyUserPrompt(question, hits)
	raw, _, err := call(ctx, sufficiencySystemPrompt, user)
	if err != nil {
		// Fail-safe: a broken eval must not block answering or loop forever.
		return sufficiencyResult{Tier: "EXACT", Confidence: 1.0}, err
	}
	return parseSufficiency(raw), nil
}

func buildSufficiencyUserPrompt(question string, hits []memory.Result) string {
	var b strings.Builder
	b.WriteString("RETRIEVED MEMORIES:\n")
	memories := toMemories(hits)
	if len(memories) == 0 {
		b.WriteString("(none)\n")
	}
	for i, m := range memories {
		fmt.Fprintf(&b, "%d. %s\n", i+1, m.Line())
	}
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nReturn the JSON verdict now.", question)
	return b.String()
}

// parseSufficiency extracts tier/confidence/missing tolerantly (mirrors
// parseJudgeVerdict's defensive style — the model occasionally wraps JSON in
// prose).
func parseSufficiency(raw string) sufficiencyResult {
	var s sufficiencyResult
	lo, hi := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if lo >= 0 && hi > lo {
		if json.Unmarshal([]byte(raw[lo:hi+1]), &s) == nil && strings.TrimSpace(s.Tier) != "" {
			normalizeSufficiency(&s)
			return s
		}
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "exact"):
		s.Tier = "EXACT"
	case strings.Contains(lower, "inferrable"):
		s.Tier = "INFERRABLE"
	default:
		s.Tier = "PARTIAL"
	}
	return s
}

func normalizeSufficiency(s *sufficiencyResult) {
	s.Tier = strings.ToUpper(strings.TrimSpace(s.Tier))
	switch s.Tier {
	case "EXACT", "INFERRABLE", "PARTIAL":
	default:
		s.Tier = "PARTIAL"
	}
	if s.Confidence < 0 {
		s.Confidence = 0
	} else if s.Confidence > 1 {
		s.Confidence = 1
	}
}

// sufficient reports whether the accumulated evidence is enough to stop the
// IRIS loop and answer.
func sufficient(s sufficiencyResult, category int) bool {
	switch s.Tier {
	case "EXACT":
		return true
	case "INFERRABLE":
		theta := irisThetaGeneral
		if category == 2 {
			theta = irisThetaTemporal
		}
		return s.Confidence >= theta
	default:
		return false
	}
}

const refineQuerySystemPrompt = `A memory search for a question retrieved PARTIAL evidence. Write ONE alternative search query that targets the SPECIFIC missing information, using different words — synonyms, the underlying event, object, or likely entity names — not a rephrasing of the question. Anchor on the original question to avoid topic drift. Output ONLY the query text, a short keyword-style phrase, no quotes, no explanation.`

func refineQuery(ctx context.Context, call modelCaller, question, missing string) (string, error) {
	user := fmt.Sprintf("QUESTION: %s\n\nMISSING INFORMATION: %s\n\nGenerate ONE improved search query that targets the missing information.", question, missing)
	q, err := call(ctx, refineQuerySystemPrompt, user)
	if err != nil {
		return question, err // fail-safe: re-search the original question
	}
	q = strings.TrimSpace(q)
	q = strings.Trim(q, "\"'“”")
	if q == "" {
		return question, nil
	}
	return q, nil
}

// irisRetrieve runs the IRIS evidence-gap loop and returns the answerer's hit
// set (slot-merged, capped at topK). The loop accumulates evidence across up to
// `depth` rounds; EvalSufficiency sees the accumulation, the answerer sees a
// budget-aligned slot merge.
func irisRetrieve(ctx context.Context, retriever *memory.Retriever, filterCall, rewriteCall modelCaller, evalCall usageModelCaller, question string, topK, quota int, opt options, category int) ([]memory.Result, error) {
	hits0, _, _, err := retrieveQuestionWithDiagnostics(ctx, retriever, filterCall, rewriteCall, question, topK, quota, opt)
	if err != nil {
		return hits0, err
	}
	depth := opt.irisDepth
	if depth < 1 {
		depth = irisMaxDepthDefault
	}
	acc := dedupHits(hits0)
	var fresh []memory.Result // gap-targeted hits not present in round 0
	for i := 1; i < depth; i++ {
		s, evalErr := evalSufficiency(ctx, evalCall, question, acc, category)
		if evalErr != nil {
			break // fail-safe: stop iterating, answer with current merge
		}
		if sufficient(s, category) {
			break
		}
		refined := question
		if strings.TrimSpace(s.Missing) != "" {
			if rq, err := refineQuery(ctx, rewriteCall, question, s.Missing); err == nil {
				refined = rq
			}
		}
		newHits, _, _, rerr := retrieveQuestionWithDiagnostics(ctx, retriever, filterCall, rewriteCall, refined, topK, quota, opt)
		if rerr != nil {
			break
		}
		acc = dedupHits(append(acc, newHits...))
		for _, h := range newHits {
			if !hitPresent(h, hits0) {
				fresh = append(fresh, h)
			}
		}
	}
	return irisMerge(hits0, fresh, topK), nil
}

// irisMerge reserves half the topK slots for round-0 anchors and half for fresh
// gap-targeted evidence, dedups, and caps at topK — keeping the answerer's
// context budget aligned with the flat-hybrid baseline while letting the
// diagnosed gap enter the context.
func irisMerge(hits0, fresh []memory.Result, topK int) []memory.Result {
	reserve0 := topK / 2
	if reserve0 < 1 {
		reserve0 = 1
	}
	seen := make(map[string]struct{})
	out := make([]memory.Result, 0, topK)
	add := func(h memory.Result) bool {
		if len(out) >= topK {
			return false // cap reached → caller stops
		}
		key := hitKey(h)
		if _, ok := seen[key]; ok {
			return true // duplicate → skip but keep iterating
		}
		seen[key] = struct{}{}
		out = append(out, h)
		return true
	}
	for i := 0; i < reserve0 && i < len(hits0); i++ {
		add(hits0[i])
	}
	for _, h := range fresh {
		if !add(h) {
			break
		}
	}
	for _, h := range hits0[reserve0:] {
		if !add(h) {
			break
		}
	}
	return out
}

func dedupHits(hits []memory.Result) []memory.Result {
	seen := make(map[string]struct{}, len(hits))
	out := make([]memory.Result, 0, len(hits))
	for _, h := range hits {
		key := hitKey(h)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	return out
}

func hitPresent(h memory.Result, hits []memory.Result) bool {
	key := hitKey(h)
	for _, x := range hits {
		if hitKey(x) == key {
			return true
		}
	}
	return false
}

func hitKey(h memory.Result) string {
	return h.Name + "\x00" + h.Content
}
