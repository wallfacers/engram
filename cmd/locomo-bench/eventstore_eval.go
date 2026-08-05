package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/eventstore"
)

// eventMaxTokens caps the event-extraction request. A dual-perspective event
// JSON is a few hundred tokens; keeping this small also keeps it well under the
// 7B sidecar's context window (max-model-len 8192).
const eventMaxTokens = 2048

// buildEventExtractCall wires the event-extraction LLM (a local OpenAI-
// compatible sidecar, e.g. vllm Qwen2.5-7B) into an eventstore.ModelCaller.
// Configuration flows through flags/env only; secrets never enter tracked files
// (DEATH RULE: this is a local sidecar, never a paid cloud recall model).
func buildEventExtractCall(opt options, logger *slog.Logger) (eventstore.ModelCaller, error) {
	baseURL := firstNonEmpty(opt.eventLLMBaseURL, os.Getenv("EVENT_LLM_BASE_URL"))
	model := firstNonEmpty(opt.eventLLMModel, os.Getenv("EVENT_LLM_MODEL"))
	apiKey := firstNonEmpty(opt.eventLLMAPIKey, os.Getenv("EVENT_LLM_API_KEY"))
	if baseURL == "" || model == "" {
		return nil, fmt.Errorf("event extraction requires --event-llm-base-url and --event-llm-model (or EVENT_LLM_BASE_URL/EVENT_LLM_MODEL)")
	}
	prov, err := buildBenchProvider("openai", apiKey, baseURL, eventMaxTokens, "EVENT_LLM")
	if err != nil {
		return nil, fmt.Errorf("event extraction provider: %w", err)
	}
	caller := newModelCaller(prov, model, eventMaxTokens)
	logger.Info("event extraction LLM wired", "model", model, "base_url_host", baseURLHost(baseURL))
	return eventstore.ModelCaller(caller), nil
}

// runBuildEventProject extracts a dual-perspective event per message and
// writes the 027 event projection JSON (configHash-namespaced, rebuildable).
// Fail-closed: a model/schema failure on a message skips that event and the
// raw chunk path remains the fallback. Requires a prepared store so evidence
// IDs are stable (turnEvidence maps dataset dia_id → Evidence ID).
func runBuildEventProject(ctx context.Context, opt options, convs []conversation, runtimes []*conversationRuntime, logger *slog.Logger) error {
	call, err := buildEventExtractCall(opt, logger)
	if err != nil {
		return err
	}
	extractor := eventstore.NewExtractor(call)
	const configHash = "027-event-v1"

	type job struct {
		input eventstore.ExtractInput
	}
	var jobs []job
	for ci := range convs {
		rt := runtimes[ci]
		if rt == nil {
			continue
		}
		conv := convs[ci]
		for _, s := range conv.Sessions {
			for ti := range s.Turns {
				t := &s.Turns[ti]
				if strings.TrimSpace(t.Text) == "" {
					continue
				}
				evidenceID := rt.turnEvidence[t.DiaID]
				if evidenceID == "" {
					continue // no evidence mapping; skip (fail-closed)
				}
				jobs = append(jobs, job{input: eventstore.ExtractInput{
					ConversationID:      fmt.Sprintf("conv-%d", conv.ID),
					SourceLedgerID:      evidenceID,
					Speaker:             t.Speaker,
					MessageText:         t.Text,
					ConversationContext: sessionContext(s.Turns, ti, 4),
				}})
			}
		}
	}
	if len(jobs) == 0 {
		return fmt.Errorf("event projection build: no extractable messages (turnEvidence empty? add --chunks)")
	}

	// Concurrent extraction bounded by the 7B sidecar's sequence capacity.
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	all := make([]eventstore.Event, 0, len(jobs))
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ev, err := extractor.ExtractOne(ctx, j.input)
			if err != nil {
				return // fail-closed skip; the raw chunk path stays the fallback
			}
			mu.Lock()
			all = append(all, *ev)
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	if len(all) == 0 {
		stats := extractor.Stats()
		return fmt.Errorf("event projection build produced zero events (attempts=%d failures=%d)", stats.Attempts, stats.Failures)
	}
	proj := eventstore.BuildProject("all", configHash, all, nil)
	if err := proj.Write(opt.buildEventProjectOut); err != nil {
		return fmt.Errorf("event projection write: %w", err)
	}
	stats := extractor.Stats()
	logger.Info("event projection built", "path", opt.buildEventProjectOut, "events", len(all),
		"attempts", stats.Attempts, "successes", stats.Successes, "failures", stats.Failures,
		"schema_failures", stats.FailureReasons["schema"], "model_failures", stats.FailureReasons["model_call"])
	return nil
}

// sessionContext returns up to n preceding turns as "Speaker: text" lines.
func sessionContext(turns []turn, at, n int) []string {
	start := at - n
	if start < 0 {
		start = 0
	}
	var out []string
	for i := start; i < at; i++ {
		if strings.TrimSpace(turns[i].Text) == "" {
			continue
		}
		out = append(out, turns[i].Speaker+": "+turns[i].Text)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// renderEventHitsForQuery replaces a question's retrieved hits with the
// query-relevant event projection of the same conversation. It locates the
// conversation from the first hit's source-session id, scores that
// conversation's events against the question by keyword overlap, and renders
// the top-scoring events (facts + relations + temporal anchors) as the answer
// context. If nothing is relevant it degrades to the original hits
// (fail-closed, zero behavior change when the projection is absent).
func renderEventHitsForQuery(qa locomoQA, hits []memory.Result, proj *eventstore.Project, topK int) []memory.Result {
	if proj == nil || len(hits) == 0 || topK < 1 {
		return hits
	}
	convID := ""
	for _, h := range hits {
		if id := parseConvFromSession(h.SourceSessionID); id != "" {
			convID = id
			break
		}
	}
	if convID == "" {
		return hits
	}
	var events []*eventstore.Event
	for i := range proj.Events {
		if proj.Events[i].ConversationID == convID {
			events = append(events, &proj.Events[i])
		}
	}
	if len(events) == 0 {
		return hits
	}
	qTok := eventQueryTokens(qa.Question)
	type scoredEvent struct {
		ev    *eventstore.Event
		score int
	}
	scored := make([]scoredEvent, 0, len(events))
	for _, ev := range events {
		scored = append(scored, scoredEvent{ev: ev, score: eventTextOverlap(qTok, ev)})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	out := make([]memory.Result, 0, len(scored))
	for _, s := range scored {
		if s.score <= 0 {
			continue
		}
		text := renderEventText(s.ev)
		out = append(out, memory.Result{
			ID:              "event-" + s.ev.ID,
			Name:            "event-" + s.ev.ID,
			Content:         text,
			Score:           float64(s.score),
			SourceSessionID: hits[0].SourceSessionID,
		})
	}
	if len(out) == 0 {
		return hits
	}
	return out
}

var convSessionIDRe = regexp.MustCompile(`conv-?(\d+)`)

// parseConvFromSession extracts "conv-N" from a harness source-session id like
// "conv5-sess1" (pipeline) or "conv-5-sess1".
func parseConvFromSession(sessionID string) string {
	m := convSessionIDRe.FindStringSubmatch(sessionID)
	if len(m) == 2 {
		return "conv-" + m[1]
	}
	return ""
}

var eventStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "at": true, "for": true, "with": true,
	"from": true, "by": true, "was": true, "were": true, "is": true, "are": true,
	"be": true, "been": true, "being": true, "as": true, "it": true, "its": true,
	"this": true, "that": true, "these": true, "those": true, "they": true,
	"them": true, "their": true, "he": true, "she": true, "his": true, "her": true,
	"we": true, "our": true, "you": true, "your": true, "i": true, "me": true, "my": true,
	"did": true, "does": true, "do": true, "have": true, "has": true, "had": true,
	"can": true, "could": true, "will": true, "would": true, "should": true,
	"may": true, "might": true, "not": true, "no": true, "yes": true, "so": true,
	"just": true, "then": true, "there": true, "here": true, "what": true,
	"when": true, "where": true, "who": true, "whom": true, "which": true, "why": true, "how": true,
}

func eventQueryTokens(q string) map[string]bool {
	out := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.Trim(w, ".,?!:;\"'()")
		if len(w) > 2 && !eventStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func eventTextOverlap(qTok map[string]bool, ev *eventstore.Event) int {
	score := 0
	seen := make(map[string]bool)
	consider := func(s string) {
		for _, w := range strings.Fields(strings.ToLower(s)) {
			w = strings.Trim(w, ".,?!:;\"'()")
			if qTok[w] && !seen[w] {
				seen[w] = true
				score++
			}
		}
	}
	for _, f := range ev.FactEntries {
		consider(f.Text)
	}
	for _, r := range ev.RelationEntries {
		consider(r.Text)
		consider(r.Subject)
		consider(r.Object)
	}
	consider(ev.RelativeRef)
	consider(ev.AbsoluteTS)
	return score
}
