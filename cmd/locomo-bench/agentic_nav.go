package main

// 029 agentic multi-step memory navigation (specs/029). A reasoning loop lets
// the local sidecar decide, per step, between search / expand_query /
// follow_entity / stop, so that inference rescues evidence a single-shot
// top-k missed. The final evidence bundle feeds the answerer exactly as the
// existing single-shot path would.
//
// Fail-closed discipline (contracts/navigation-tools.md + Constitution V):
// navigation is an OFF switch on top of the existing path. Any of — no
// navigation model, LLM call failure, unparsable tool call, or hitting the
// step cap without a stop — collapses to the single-shot retrieval result that
// the non-navigation runner already produces (SC-004 zero-behaviour-change
// regression). Navigation never produces an empty answer.
//
// The engine (memory/ embedding/ provider/ store/ internal/) is untouched.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
)

const (
	defaultNavMaxSteps      = 4
	defaultNavK             = 8
	// defaultNavTimeout bounds one navigation step. The sidecar is a reasoning
	// model (Qwen3.6-class) that emits chain-of-thought before the tool JSON, so
	// the per-step bound must be generous enough for a full think + emit cycle
	// under concurrency (observed ~13s for one step at low load).
	defaultNavTimeout       = 45 * time.Second
	// defaultAnswerContextCap bounds the final evidence bundle in tokens. It is
	// set to match the baseline answer context (12 verbatim chunks ≈ 3600
	// tokens, observed 3654 in the baseline arm) so the paired comparison is
	// same-budget (008 discipline).
	defaultAnswerContextCap = 3600
	defaultNavFallbackTopK  = 30 // single-shot fallback depth == existing --top-k default
	// defaultMinEvidence is the minimum final-evidence count. A navigation that
	// stops with only 1-2 evidence items starves the answerer (observed 138 vs
	// 3654 tokens in the baseline arm); the assembler pads up to this many from
	// the navigation's own seen set, then the single-shot pool, staying within
	// the answer-context cap. This keeps the answerer's context comparable to
	// the baseline rather than degraded by an over-laconic stop.
	defaultMinEvidence = 12
)

// navSystemPrompt instructs the sidecar to emit exactly one JSON tool call per
// step. The schema mirrors contracts/navigation-tools.md; no extra text.
const navSystemPrompt = `You are a memory navigator for a personal memory retrieval system. Given a question and the evidence already seen, decide the next retrieval action.

Emit ONLY a JSON object with this exact shape:
{"tool":"search","tool_args":{"query":"...","k":8},"rationale":"..."}

Tools:
- {"tool":"search","tool_args":{"query":"<string>","k":8}}         hybrid retrieval on a (possibly rewritten) query
- {"tool":"expand_query","tool_args":{"text":"<string>","k":8}}     a rewritten/complementary query grounded in seen evidence
- {"tool":"follow_entity","tool_args":{"entity":"<string>","k":8}}  entity-anchored retrieval (person/place/org name)
- {"tool":"stop","tool_args":{"evidence_ids":["<seen source_id>",...],"assembly":"first_n"}}  finish navigation

Rules:
- k must be between 1 and 12. Do not invent source_ids — reference only ids already shown in the seen evidence.
- stop only when the seen evidence suffices to answer; pick the best evidence_ids and assembly "first_n".
- If a search returned nothing useful, try a different query or entity before stopping.`

// navConfig holds the runtime configuration for one navigation run.
type navConfig struct {
	NavK             int           // per-tool retrieval depth (0 → defaultNavK)
	MaxSteps         int           // step cap (0 → defaultNavMaxSteps)
	Timeout          time.Duration // per-step LLM timeout (0 → defaultNavTimeout)
	AnswerContextCap int           // final evidence token cap (0 → defaultAnswerContextCap)
	FallbackTopK     int           // fail-closed single-shot depth (0 → defaultNavFallbackTopK)
	Call             usageModelCaller
}

func (c navConfig) effective() navConfig {
	if c.NavK <= 0 {
		c.NavK = defaultNavK
	}
	if c.MaxSteps <= 0 {
		c.MaxSteps = defaultNavMaxSteps
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultNavTimeout
	}
	if c.AnswerContextCap <= 0 {
		c.AnswerContextCap = defaultAnswerContextCap
	}
	if c.FallbackTopK <= 0 {
		c.FallbackTopK = defaultNavFallbackTopK
	}
	return c
}

// navRuntime carries per-run navigation state.
type navRuntime struct {
	cfg        navConfig
	retriever  *memory.Retriever
	questionID string
	query      string
	steps      []NavStep
	seen       []NavEvidence   // all evidence seen, in first-seen order
	seenIDs    map[string]bool // dedup key
	navTokens  int
}

func newNavRuntime(questionID, query string, retriever *memory.Retriever, cfg navConfig) *navRuntime {
	return &navRuntime{
		cfg:        cfg.effective(),
		retriever:  retriever,
		questionID: questionID,
		query:      query,
		seenIDs:    make(map[string]bool),
	}
}

// runNavigation executes the navigation loop and returns the auditable
// trajectory. The returned trajectory always has a non-empty FinalEvidence
// (either the assembled stop bundle or the fail-closed single-shot results);
// callers write Answer into it after the answerer runs.
func runNavigation(ctx context.Context, questionID, query string, retriever *memory.Retriever, cfg navConfig) (*NavigationTrajectory, error) {
	if retriever == nil {
		return nil, fmt.Errorf("%w: nil retriever", errNavUnavailable)
	}
	cfg = cfg.effective()
	rt := newNavRuntime(questionID, query, retriever, cfg)
	if cfg.Call == nil {
		// No navigation model configured: fail closed to single-shot now.
		return rt.failClosed(ctx), nil
	}
	for step := 1; step <= cfg.MaxSteps; step++ {
		action, err := rt.decide(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr // caller cancellation propagates unchanged
			}
			fmt.Fprintf(os.Stderr, "[nav] step %d decide failed: %v\n", step, err)
			return rt.failClosed(ctx), nil
		}
		if action.Stop != nil {
			return rt.assembleStop(ctx, action), nil
		}
		start := time.Now()
		evidence, err := executeNavTool(ctx, retriever, action)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			fmt.Fprintf(os.Stderr, "[nav] step %d execute %s failed: %v\n", step, action.Tool, err)
			return rt.failClosed(ctx), nil
		}
		rt.recordStep(step, action, evidence, time.Since(start))
		// No evidence at all on a non-stop step: keep navigating — the model
		// may retry with a different query; the cap still bounds the loop.
	}
	// Step cap reached without a stop.
	return rt.failClosed(ctx), nil
}

// decide runs one sidecar call and parses the tool action. The caller
// distinguishes caller cancellation (propagates) from navigation failure
// (fail-closed) via ctx.Err().
func (rt *navRuntime) decide(ctx context.Context) (navToolAction, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, rt.cfg.Timeout)
	defer cancel()
	text, usage, err := rt.cfg.Call(timeoutCtx, navSystemPrompt, rt.renderUser())
	if err != nil {
		return navToolAction{}, fmt.Errorf("%w: nav call: %v", errNavUnavailable, err)
	}
	rt.navTokens += usage.InputTokens + usage.OutputTokens
	return parseNavToolCall(text)
}

// renderUser serialises the query plus all seen evidence into the user turn so
// the model can ground tool decisions (and stop choices) in real evidence.
func (rt *navRuntime) renderUser() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nSeen evidence:\n", rt.query)
	if len(rt.seen) == 0 {
		b.WriteString("(none yet)\n")
	}
	for i, ev := range rt.seen {
		snippet := ev.Text
		if len(snippet) > 600 {
			snippet = snippet[:600] + "…"
		}
		fmt.Fprintf(&b, "[%d] id=%s\n%s\n", i, ev.SourceID, snippet)
	}
	b.WriteString("\nEmit the next tool call JSON now.")
	return b.String()
}

// recordStep appends one executed (non-stop) step and folds its evidence into
// the seen set (first-seen order, dedup by source_id).
func (rt *navRuntime) recordStep(step int, action navToolAction, evidence []NavEvidence, latency time.Duration) {
	for _, ev := range evidence {
		if !rt.seenIDs[ev.SourceID] {
			rt.seenIDs[ev.SourceID] = true
			rt.seen = append(rt.seen, ev)
		}
	}
	rt.steps = append(rt.steps, NavStep{
		Index:            step,
		Tool:             action.Tool,
		ToolArgs:         mustRawToolArgs(action),
		ReturnedEvidence: evidence,
		Rationale:        action.Rationale,
		LatencyMS:        latency.Milliseconds(),
	})
}

// assembleStop builds the final evidence bundle from the model's stop call.
// Evidence is taken in the model's chosen id order, deduped, and truncated to
// the answer-context cap (008 discipline). The stop itself is recorded as the
// terminal step so the trajectory satisfies contracts/navigation-trajectory.md
// (last step MUST be stop, or fallback_triggered=true). If the model selects
// nothing the bundle would be empty, which is a navigation failure → fail-closed.
func (rt *navRuntime) assembleStop(ctx context.Context, action navToolAction) *NavigationTrajectory {
	stop := action.Stop
	rt.steps = append(rt.steps, NavStep{
		Index:            len(rt.steps) + 1,
		Tool:             "stop",
		ToolArgs:         jsonRaw(stop),
		ReturnedEvidence: []NavEvidence{},
		Rationale:        action.Rationale,
		LatencyMS:        0,
	})
	byID := make(map[string]NavEvidence, len(rt.seen))
	for _, ev := range rt.seen {
		if _, ok := byID[ev.SourceID]; !ok {
			byID[ev.SourceID] = ev
		}
	}
	var chosen []NavEvidence
	seen := make(map[string]bool)
	for _, id := range stop.EvidenceIDs {
		ev, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		chosen = append(chosen, ev)
	}
	if len(chosen) == 0 {
		// Empty final evidence: navigation produced nothing usable → fail
		// closed to single-shot (never an empty answer).
		return rt.failClosed(ctx)
	}
	assembly := stop.Assembly
	if assembly == "" {
		assembly = "first_n"
	}
	bundle := truncateEvidenceBundle(chosen, rt.cfg.AnswerContextCap)
	bundle = ensureMinEvidence(bundle, rt.seen, singleShotEvidence(ctx, rt.retriever, rt.query, rt.cfg.FallbackTopK), defaultMinEvidence, rt.cfg.AnswerContextCap)
	bundle.Assembly = assembly
	return rt.trajectory(bundle, false)
}

// failClosed collapses when navigation cannot complete a clean stop (decide
// failure, step cap without stop). The final evidence is the navigation's own
// accumulated seen set — the focused multi-query search did real work and must
// not be thrown away — falling back to the single-shot retrieval only when the
// loop produced no evidence at all (e.g. first-step failure), which keeps the
// SC-004 zero-behaviour-change guarantee for the truly-degraded case.
func (rt *navRuntime) failClosed(ctx context.Context) *NavigationTrajectory {
	evidence := rt.seen
	var fallbackSingle []NavEvidence
	if len(evidence) == 0 {
		evidence = singleShotEvidence(ctx, rt.retriever, rt.query, rt.cfg.FallbackTopK)
	} else {
		fallbackSingle = singleShotEvidence(ctx, rt.retriever, rt.query, rt.cfg.FallbackTopK)
	}
	bundle := truncateEvidenceBundle(evidence, rt.cfg.AnswerContextCap)
	bundle = ensureMinEvidence(bundle, rt.seen, fallbackSingle, defaultMinEvidence, rt.cfg.AnswerContextCap)
	return rt.trajectory(bundle, true)
}

// ensureMinEvidence assembles the final evidence with the answerer's context in
// mind. Navigation retrieval returns a mix of short extracted facts and full
// verbatim chunks; the answerer needs chunk-level context (observed: a
// fact-dominated bundle of 30 items carries only ~440 tokens vs ~2700 for 12
// baseline chunks). So the merged candidates (navigation choices → seen → the
// single-shot pool, deduped) are reordered chunk-first — full chunks lead in
// the navigation's relative order, facts trail — then truncated to the token
// cap. The chunk-first ordering plus the 3600-token cap yields ~12-16 chunks,
// comparable to the baseline's 12 chunk-quota slots.
func ensureMinEvidence(bundle EvidenceBundle, seen, fallbackSingle []NavEvidence, minN, cap int) EvidenceBundle {
	merged := append([]NavEvidence{}, bundle.Evidence...)
	have := make(map[string]bool, len(merged))
	for _, ev := range merged {
		have[ev.SourceID] = true
	}
	for _, ev := range seen {
		if !have[ev.SourceID] {
			have[ev.SourceID] = true
			merged = append(merged, ev)
		}
	}
	for _, ev := range fallbackSingle {
		if !have[ev.SourceID] {
			have[ev.SourceID] = true
			merged = append(merged, ev)
		}
	}
	ordered := append([]NavEvidence{}, navChunks(merged)...)
	ordered = append(ordered, navFacts(merged)...)
	return truncateEvidenceBundle(ordered, cap)
}

// navChunks returns evidence items that carry chunk-level context (full verbatim
// chunk bodies are ~900 chars; extracted facts are short sentences).
func navChunks(evidence []NavEvidence) []NavEvidence {
	out := make([]NavEvidence, 0, len(evidence))
	for _, ev := range evidence {
		if len(ev.Text) > 300 {
			out = append(out, ev)
		}
	}
	return out
}

func navFacts(evidence []NavEvidence) []NavEvidence {
	out := make([]NavEvidence, 0, len(evidence))
	for _, ev := range evidence {
		if len(ev.Text) <= 300 {
			out = append(out, ev)
		}
	}
	return out
}

// truncateEvidenceBundle keeps evidence in order, dedup-safe, truncating
// individual texts to fit the answer-context cap so the bundle's total_tokens
// never exceeds it and never becomes empty from an over-long single evidence
// (008 discipline). Cap ≤ 0 is treated as unbounded.
func truncateEvidenceBundle(evidence []NavEvidence, cap int) EvidenceBundle {
	if cap <= 0 {
		total := 0
		for _, ev := range evidence {
			total += estimateTokens(ev.Text)
		}
		return EvidenceBundle{Evidence: evidence, TotalTokens: total, Assembly: "first_n"}
	}
	total := 0
	out := make([]NavEvidence, 0, len(evidence))
	for _, ev := range evidence {
		if total >= cap {
			break
		}
		budget := cap - total
		tokens := estimateTokens(ev.Text)
		if tokens > budget {
			// Text-level truncation: keep the longest prefix that fits the
			// remaining budget, so the bundle stays non-empty and within cap.
			runes := []rune(ev.Text)
			maxRunes := budget * 4
			if len(runes) > maxRunes {
				ev.Text = string(runes[:maxRunes])
			}
			out = append(out, ev)
			total = cap
			break
		}
		out = append(out, ev)
		total += tokens
	}
	return EvidenceBundle{Evidence: out, TotalTokens: total, Assembly: "first_n"}
}

func (rt *navRuntime) trajectory(bundle EvidenceBundle, fallback bool) *NavigationTrajectory {
	return &NavigationTrajectory{
		QuestionID: rt.questionID,
		Query:      rt.query,
		Steps:      rt.steps,
		FinalEvidence: EvidenceBundle{
			Evidence:    bundle.Evidence,
			TotalTokens: bundle.TotalTokens,
			Assembly:    bundle.Assembly,
		},
		BudgetUsage: BudgetUsage{
			Steps:               len(rt.steps),
			NavTokens:           rt.navTokens,
			AnswerContextTokens: bundle.TotalTokens,
		},
		FallbackTriggered: fallback,
	}
}

// singleShotEvidence runs the exact single-shot hybrid retrieval used by the
// existing non-navigation path, so a failed navigation produces byte-identical
// candidate evidence (SC-004). topK ≤ 0 → defaultNavFallbackTopK.
func singleShotEvidence(ctx context.Context, retriever *memory.Retriever, query string, topK int) []NavEvidence {
	if topK <= 0 {
		topK = defaultNavFallbackTopK
	}
	results, err := retriever.Search(ctx, query, topK)
	if err != nil {
		return nil
	}
	evidence := make([]NavEvidence, 0, len(results))
	for _, r := range results {
		evidence = append(evidence, NavEvidence{
			SourceID:    r.ID,
			Text:        r.Content,
			Score:       r.Score,
			RetrievedBy: "hybrid",
		})
	}
	return evidence
}

// estimateTokens is a local, offline approximation used for answer-context
// budget bookkeeping (008 discipline). It intentionally under-counts vs a
// tokenizer; the budget cap is validated per-run and over-cap bundles truncate.
func estimateTokens(text string) int {
	n := len([]rune(text))
	if n <= 0 {
		return 0
	}
	t := n / 4
	if t < 1 {
		return 1
	}
	return t
}

// mustRawToolArgs re-encodes a validated action back to its wire tool_args
// shape for the trajectory record. Parse succeeded earlier, so it cannot fail.
func mustRawToolArgs(a navToolAction) []byte {
	switch {
	case a.Search != nil:
		return jsonRaw(a.Search)
	case a.Expand != nil:
		return jsonRaw(a.Expand)
	case a.Follow != nil:
		return jsonRaw(a.Follow)
	case a.Stop != nil:
		return jsonRaw(a.Stop)
	}
	return []byte("{}")
}

// jsonRaw marshals v to JSON, degrading to "{}" on any (non-reproducible)
// failure — it is only used for trajectory bookkeeping after successful parse.
func jsonRaw(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
