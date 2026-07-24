package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

// modelCaller is one text-in/text-out call against the benchmark model.
type modelCaller func(ctx context.Context, system, user string) (string, error)

type usageModelCaller func(ctx context.Context, system, user string) (string, provider.Usage, error)

const canonicalAbstainDecline = "I don't know; this information is not mentioned in the conversation."

const abstainLowConfidenceHint = "LOW-CONFIDENCE RETRIEVAL HINT: Verify that the retrieved memories support the premise before answering."

// answerWithAbstentionDecision owns the one place an operating point can skip
// an answer-model call. The judge call remains in the normal runner path so
// the hard-gate result is graded by the existing adversarial-gold convention.
func answerWithAbstentionDecision(ctx context.Context, decision AbstainDecision, opt options, systemPrompt, userPrompt string, answerCall usageModelCaller) (string, provider.Usage, bool, error) {
	if opt.abstainHard && decision.Abstain {
		return canonicalAbstainDecline, provider.Usage{}, true, nil
	}
	if opt.abstainSoft && decision.Abstain {
		systemPrompt += "\n\n" + abstainLowConfidenceHint
	}
	predicted, usage, err := answerCall(ctx, systemPrompt, userPrompt)
	return predicted, usage, false, err
}

func modelCallerFromUsage(c usageModelCaller) modelCaller {
	return func(ctx context.Context, system, user string) (string, error) {
		text, _, err := c(ctx, system, user)
		return text, err
	}
}

func usageCallerFromModel(c modelCaller) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		text, err := c(ctx, system, user)
		return text, provider.Usage{}, err
	}
}

// failoverModelCaller tries each caller in order, returning the first success.
// A non-cancellation error falls over to the next endpoint (e.g. a relay's
// backup base URL surviving a transient 502 on the primary); a cancelled
// context is terminal and never falls over. Returns the last error if every
// endpoint fails. Callers with fewer than two entries degrade to the single
// caller (or a no-op error when empty).
func failoverModelCaller(callers ...modelCaller) modelCaller {
	return func(ctx context.Context, system, user string) (string, error) {
		var lastErr error
		for _, c := range callers {
			if c == nil {
				continue
			}
			text, err := c(ctx, system, user)
			if err == nil {
				return text, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return "", err
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("failoverModelCaller: no endpoints configured")
		}
		return "", lastErr
	}
}

// newModelCaller wraps a provider.Provider into a modelCaller. Callers that
// need a reproducible generation temperature may provide one optional value.
func newModelCaller(p provider.Provider, model string, maxTokens int, temperature ...float64) modelCaller {
	fixedTemperature := 0.0
	if len(temperature) > 0 {
		fixedTemperature = temperature[0]
	}
	return modelCallerFromUsage(newUsageModelCaller(p, model, maxTokens, fixedTemperature, "", nil))
}

// newModelCallerWithUsage is the accounting-aware provider adapter. Provider
// adapters already normalize vendor usage responses into EventUsage; this
// wrapper preserves the text-only modelCaller contract while forwarding totals
// to the benchmark cost ledger.
func newModelCallerWithUsage(p provider.Provider, model string, maxTokens int, role string, record func(role, model string, usage provider.Usage)) modelCaller {
	return modelCallerFromUsage(newUsageModelCallerWithUsage(p, model, maxTokens, role, record))
}

func newUsageModelCallerWithUsage(p provider.Provider, model string, maxTokens int, role string, record func(role, model string, usage provider.Usage)) usageModelCaller {
	return newUsageModelCaller(p, model, maxTokens, 0, role, record)
}

func newUsageModelCaller(p provider.Provider, model string, maxTokens int, temperature float64, role string, record func(role, model string, usage provider.Usage)) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		req := provider.Request{
			Model:       model,
			System:      system,
			MaxTokens:   maxTokens,
			Temperature: temperature,
			Messages: []provider.Message{{
				Role:    provider.RoleUser,
				Content: []provider.ContentBlock{{Type: provider.BlockText, Text: user}},
			}},
		}
		ch, err := p.Stream(ctx, req)
		if err != nil {
			if record != nil {
				record(role, model, provider.Usage{})
			}
			return "", provider.Usage{}, err
		}
		var sb strings.Builder
		usage := provider.Usage{}
		for ev := range ch {
			switch ev.Type {
			case provider.EventTextDelta:
				sb.WriteString(ev.TextDelta)
			case provider.EventUsage:
				if ev.Usage != nil {
					usage.InputTokens += ev.Usage.InputTokens
					usage.OutputTokens += ev.Usage.OutputTokens
				}
			case provider.EventError:
				if ev.Error != nil {
					if record != nil {
						record(role, model, usage)
					}
					return "", usage, ev.Error
				}
			}
		}
		if record != nil {
			record(role, model, usage)
		}
		return sb.String(), usage, nil
	}
}

// perCallTimeout bounds one LLM call for as long as it holds a semaphore
// slot. Without it a relay connection that dies mid-run leaves every
// in-flight Stream waiting on headers forever and the whole bench deadlocks
// on the semaphore (observed 2026-07-19, Strike 1). Streamed bench answers
// finish in seconds; three minutes is generous.
const perCallTimeout = 3 * time.Minute

func gateUsage(sem chan struct{}, c usageModelCaller) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return "", provider.Usage{}, ctx.Err()
		}
		defer func() { <-sem }()
		var (
			lastUsage provider.Usage
			lastErr   error
		)
		for attempt := 0; attempt < 2; attempt++ {
			callCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
			text, usage, err := c(callCtx, system, user)
			cancel()
			if err == nil || ctx.Err() != nil {
				return text, usage, err
			}
			lastUsage, lastErr = usage, err
		}
		return "", lastUsage, lastErr
	}
}

const answerSystemPrompt = `You answer a question about a long conversation using ONLY the retrieved memories provided. Rules:
- Answer with the shortest phrase that fully answers the question — a name, a date, a place, a list. No explanation, no restating the question.
- For "when" questions, read the time from the memory's [event: YYYY-MM-DD] marker (that is when it happened). NEVER answer relative to today's date. Answer at the granularity the memory supports (a month like "May 2023" is fine if that is all that is known).
- Write dates in natural form like "21 July 2023" or "May 2023" — never ISO format like 2023-07-21.
- Make your best supported inference from the evidence — combine multiple memories if needed. Only reply "I don't know" when NO retrieved memory is relevant to the question at all; do not bail out just because the phrasing differs.`

const temporalAnswerPrompt = `You answer a temporal question about a long conversation using ONLY the retrieved memories provided. TEMPORAL REASONING PLAN:
- For every candidate memory, list its [event: YYYY-MM-DD] marker before deciding.
- normalize the candidate dates to a common timeline, then compare the dates and determine the requested order or interval.
- Output the absolute date in natural language, never ISO format. Keep the answer short and do not restate the question.
- Only reply "I don't know" when no retrieved memory is relevant to the question.`

// multiHopAnswerPrompt targets LoCoMo category 1 (multi-hop), which is
// dominated by enumeration/aggregation questions ("what things has X done",
// "how many times…") whose gold answers are lists assembled from evidence
// scattered across many sessions. The v7 failure analysis showed 95% of
// multi-hop misses were partial answers, not retrieval IDKs — the model
// stopped at the most salient item instead of sweeping every memory.
const multiHopAnswerPrompt = `You answer a question about a long conversation using ONLY the retrieved memories provided. This question aggregates evidence scattered across MANY memories — an enumeration, a count, or a comparison. Rules:
- Scan EVERY retrieved memory before answering; the relevant items are scattered, never adjacent. Do not stop at the first match.
- For "what/which (things)" questions, enumerate ALL distinct items the memories explicitly support, as a short comma-separated list. Completeness decides correctness: one missing item makes the whole answer wrong. Do NOT pad the list with plausible extras the memories never state.
- For "how many" questions, work it out before answering: silently list every qualifying occurrence with its [event: YYYY-MM-DD] date, MERGE mentions that describe the same occasion (the same event often appears in several memories — a raw dialogue excerpt and an extracted fact, or two retellings; same date usually means same occasion), count the merged list, and answer with just that number.
- Mentions on DIFFERENT dates are usually different occasions — count them separately unless clearly the same event retold.
- For "when" questions, read the time from the [event: YYYY-MM-DD] marker; write dates naturally like "21 July 2023", never ISO format.
- Answer with the shortest phrase that fully answers the question. No explanation, no restating the question.
- Only reply "I don't know" when NO retrieved memory is relevant to the question at all.`

// openDomainAnswerPrompt relaxes the grounding rule for open-domain questions
// (LoCoMo category 3), which probe opinion, motivation, and likely behavior
// rather than exact fact lookup. Mirrors AtomMem's split prompt design: ground
// in memories, but reason with common sense and world knowledge on top.
const openDomainAnswerPrompt = `You answer a question about a person based on retrieved memories from their long conversation. This question asks about opinions, motivations, preferences, or likely behavior — not an exact fact lookup. Follow this reasoning plan silently:
1. Scan EVERY retrieved memory before reasoning; do not stop at the first relevant memory.
2. Extract the clues about the person and relevant events, including their traits, habits, values, preferences, relationships, and past experiences.
3. Use common sense and world knowledge to explain the cause-and-effect connections between those clues and the person's possible opinions, motivations, preferences, or likely behavior.
4. Combine the clues and rule out guesses that conflict with the memories or lack support from the evidence and reasonable inference.
5. Choose the most specific, most likely conclusion supported by the combined evidence.
After completing the reasoning plan, output ONLY the final short, direct answer as a phrase or sentence. No explanation, no restating the question.
Only reply "I don't know" when the memories offer no basis whatsoever for even an informed inference.`

const forceAnswerSystemPrompt = `You answer a question about a long conversation using ONLY the retrieved memories provided. Rules:
- Answer with the shortest phrase that fully answers the question — a name, a date, a place, a list. No explanation, no restating the question.
- For "when" questions, read the time from the memory's [event: YYYY-MM-DD] marker (that is when it happened). NEVER answer relative to today's date. Answer at the granularity the memory supports (a month like "May 2023" is fine if that is all that is known).
- Write dates in natural form like "21 July 2023" or "May 2023" — never ISO format like 2023-07-21.
- Make your best supported inference from the evidence — combine multiple memories if needed. Always provide your best guess based on the retrieved memories and reasonable inference; never decline with an uncertainty response.`

const forceTemporalAnswerPrompt = `You answer a temporal question about a long conversation using ONLY the retrieved memories provided. TEMPORAL REASONING PLAN:
- For every candidate memory, list its [event: YYYY-MM-DD] marker before deciding.
- normalize the candidate dates to a common timeline, then compare the dates and determine the requested order or interval.
- Output the absolute date in natural language, never ISO format. Keep the answer short and do not restate the question.
- This is an answerable evaluation: always provide your best guess from the retrieved memories and never decline.`

const forceMultiHopAnswerPrompt = `You answer a question about a long conversation using ONLY the retrieved memories provided. This question aggregates evidence scattered across MANY memories — an enumeration, a count, or a comparison. Rules:
- Scan EVERY retrieved memory before answering; the relevant items are scattered, never adjacent. Do not stop at the first match.
- For "what/which (things)" questions, enumerate ALL distinct items the memories explicitly support, as a short comma-separated list. Completeness decides correctness: one missing item makes the whole answer wrong. Do NOT pad the list with plausible extras the memories never state.
- For "how many" questions, work it out before answering: silently list every qualifying occurrence with its [event: YYYY-MM-DD] date, MERGE mentions that describe the same occasion (the same event often appears in several memories — a raw dialogue excerpt and an extracted fact, or two retellings; same date usually means same occasion), count the merged list, and answer with just that number.
- Mentions on DIFFERENT dates are usually different occasions — count them separately unless clearly the same event retold.
- For "when" questions, read the time from the [event: YYYY-MM-DD] marker; write dates naturally like "21 July 2023", never ISO format.
- Answer with the shortest phrase that fully answers the question. No explanation, no restating the question.
- This is an answerable evaluation: always provide your best guess from the retrieved memories and never decline with an uncertainty response.`

const forceOpenDomainAnswerPrompt = `You answer a question about a person based on retrieved memories from their long conversation. This question asks about opinions, motivations, preferences, or likely behavior — not an exact fact lookup. Rules:
- Ground your answer in the retrieved memories: use them to understand the person's traits, habits, values, and past events.
- COMBINE the memories with common sense, cause-and-effect reasoning, and world knowledge to infer the most plausible answer. An answer supported by reasonable inference is required.
- Answer with a short, direct phrase or sentence. No explanation, no restating the question.
- This is an answerable evaluation: always provide your best guess based on the memories and reasonable inference; never decline with an uncertainty response.`

// abstainAnswerPrompt is the Abstain-R1 answer regime (feature 003 US5, a
// scoring-convention change committed separately from the algorithm work). It
// teaches abstention with a 1:4 in-context ratio — one refusal example among
// four answerable ones — so the model refuses ONLY when the memories cannot
// support any answer, and when it refuses it names the missing information
// instead of a bare bail-out. It is mutually exclusive with --force-answer.
const abstainAnswerPrompt = `You answer a question about a long conversation using ONLY the retrieved memories provided. Rules:
- If the memories support an answer, reply with the shortest phrase that fully answers it — a name, a date, a place, a list. No explanation, no restating the question.
- For "when" questions, read the time from the memory's [event: YYYY-MM-DD] marker; write dates naturally like "21 July 2023", never ISO format.
- If — and ONLY if — NO retrieved memory contains the information the question asks for, reply "I don't know" and then, in the same line, name the specific fact that is missing (e.g. "I don't know — no memory records where the trip took place"). Do NOT guess or invent facts the memories never state; a confident fabrication is worse than an honest refusal.
Examples:
Q: Where does the user work? Memories mention the user's job at Acme. A: Acme
Q: When did the user visit Oslo? A memory marks [event: 2023-05-07] for the Oslo trip. A: 7 May 2023
Q: What pets does the user have? A memory says the user adopted a cat named Mittens. A: a cat (Mittens)
Q: How many siblings does the user have? A memory says the user has two brothers. A: two
Q: What is the user's blood type? No memory mentions blood type. A: I don't know — no memory records the user's blood type.`

// answerPromptFor picks the pre-temporal system prompt by LoCoMo category.
func answerPromptFor(category int) string {
	return answerPromptForOptionsWithTemporal(category, false, false)
}

func answerPromptForOptions(category int, forceAnswer bool) string {
	return answerPromptForOptionsWithTemporal(category, forceAnswer, false)
}

func answerPromptForOptionsWithTemporal(category int, forceAnswer, temporalAnswer bool) string {
	return answerPromptForRegime(category, forceAnswer, temporalAnswer, false)
}

// answerPromptForRegime selects the answer system prompt. The abstention regime
// takes precedence over category- and temporal-specific answerable prompts: it
// is a distinct scoring convention (Strike 3), not a per-category refinement.
func answerPromptForRegime(category int, forceAnswer, temporalAnswer, abstain bool) string {
	if abstain {
		return abstainAnswerPrompt
	}
	if temporalAnswer && category == 2 {
		if forceAnswer {
			return forceTemporalAnswerPrompt
		}
		return temporalAnswerPrompt
	}
	if forceAnswer {
		switch category {
		case 1:
			return forceMultiHopAnswerPrompt
		case 3:
			return forceOpenDomainAnswerPrompt
		default:
			return forceAnswerSystemPrompt
		}
	}
	switch category {
	case 1:
		return multiHopAnswerPrompt
	case 3:
		return openDomainAnswerPrompt
	default:
		return answerSystemPrompt
	}
}

// queryRewriteSystemPrompt turns a failed question into an alternative retrieval
// query (EverMemOS-style second-round rewriting, triggered only on IDK).
const queryRewriteSystemPrompt = `A memory search for the following question returned nothing relevant. Write ONE alternative search query for the same information need: use different words — synonyms, the underlying event or object, likely entity names — not a rephrasing of the question. Output ONLY the query text, a short keyword-style phrase, no quotes, no explanation.`

// isIDK reports whether a predicted answer is an "I don't know" bail-out.
func isIDK(predicted string) bool {
	p := strings.ToLower(strings.TrimSpace(predicted))
	if p == "" {
		return true
	}
	return strings.Contains(p, "don't know") || strings.Contains(p, "do not know") ||
		strings.Contains(p, "not mentioned") || strings.Contains(p, "no information")
}

func buildAnswerPrompt(question string, memories []retrievedMemory) string {
	var b strings.Builder
	b.WriteString("RETRIEVED MEMORIES:\n")
	if len(memories) == 0 {
		b.WriteString("(none)\n")
	}
	for i, m := range memories {
		fmt.Fprintf(&b, "%d. %s\n", i+1, m.Line())
	}
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nAnswer:", question)
	return b.String()
}

func buildAnswerContextPrompt(question string, hits []memory.Result) string {
	memories := toMemories(hits)
	if hasClusterSweepHit(hits) {
		return buildSweepAnswerPrompt(question, memories)
	}
	return buildAnswerPrompt(question, memories)
}

// buildSweepAnswerPrompt groups broad-sweep hits by their source session so
// the answering model can scan one conversation block at a time. The ordinary
// buildAnswerPrompt path intentionally remains unchanged.
func buildSweepAnswerPrompt(question string, memories []retrievedMemory) string {
	type sweepGroup struct {
		id       string
		session  string
		date     string
		memories []retrievedMemory
	}
	groupsByID := make(map[string]*sweepGroup)
	for _, memory := range memories {
		id := memory.SourceSessionID
		if id == "" {
			id = "__unattributed__"
		}
		group := groupsByID[id]
		if group == nil {
			group = &sweepGroup{id: id, session: sweepSessionLabel(id), date: memory.EventDate}
			groupsByID[id] = group
		}
		if group.date == "" || (memory.EventDate != "" && memory.EventDate < group.date) {
			group.date = memory.EventDate
		}
		group.memories = append(group.memories, memory)
	}
	groups := make([]*sweepGroup, 0, len(groupsByID))
	for _, group := range groupsByID {
		sort.SliceStable(group.memories, func(i, j int) bool {
			if group.memories[i].EventDate != group.memories[j].EventDate {
				return group.memories[i].EventDate < group.memories[j].EventDate
			}
			if group.memories[i].Name != group.memories[j].Name {
				return group.memories[i].Name < group.memories[j].Name
			}
			return group.memories[i].Content < group.memories[j].Content
		})
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].date != groups[j].date {
			return groups[i].date < groups[j].date
		}
		if groups[i].session != groups[j].session {
			return sweepSessionNumber(groups[i].session) < sweepSessionNumber(groups[j].session)
		}
		return groups[i].id < groups[j].id
	})

	var b strings.Builder
	b.WriteString("RETRIEVED MEMORIES:\n")
	if len(memories) == 0 {
		b.WriteString("(none)\n")
	}
	position := 1
	for _, group := range groups {
		date := group.date
		if date == "" {
			date = "unknown"
		}
		fmt.Fprintf(&b, "[session %s, %s]\n", group.session, date)
		for _, memory := range group.memories {
			fmt.Fprintf(&b, "%d. %s\n", position, memory.Line())
			position++
		}
	}
	fmt.Fprintf(&b, "\nQUESTION: %s\n\nAnswer:", question)
	return b.String()
}

func sweepSessionLabel(source string) string {
	if source == "__unattributed__" {
		return "unknown"
	}
	const marker = "-sess"
	idx := strings.LastIndex(source, marker)
	if idx < 0 {
		return source
	}
	if _, err := strconv.Atoi(source[idx+len(marker):]); err != nil {
		return source
	}
	return source[idx+len(marker):]
}

func sweepSessionNumber(label string) int {
	n, err := strconv.Atoi(label)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return n
}

// retrievedMemory is one hit passed to the answering model.
type retrievedMemory struct {
	Name            string
	Content         string
	EventDate       string // rendered date or ""
	Recorded        string
	SourceSessionID string
}

// Line renders a memory with its time markers, mirroring MemorySearch output so
// the answering model sees the same time-aware context the agent would.
func (m retrievedMemory) Line() string {
	var b strings.Builder
	if m.EventDate != "" {
		fmt.Fprintf(&b, "[event: %s] ", m.EventDate)
	}
	if m.Recorded != "" {
		fmt.Fprintf(&b, "[recorded: %s] ", m.Recorded)
	}
	b.WriteString(m.Content)
	return b.String()
}

// judgeSystemPrompt aligns with the open mem0ai/memory-benchmarks LLM-as-a-Judge:
// a lenient semantic-equivalence check, not exact string match.
const judgeSystemPrompt = `You grade a predicted answer against a gold answer for a question about a conversation, aligned with the LoCoMo / mem0 LLM-as-a-judge convention. Output STRICT JSON only: {"correct": true|false}.

Mark "correct": true when the prediction conveys the SAME key fact as the gold answer. Be lenient on form, strict on fact:
- Ignore wording, verbosity, and extra correct detail. A more detailed answer that still contains the gold fact is correct (e.g. gold "reminding herself of her successes" vs prediction "she reminds herself of her successes and progress" → true).
- Accept synonyms and paraphrases of the same fact (e.g. "a trophy" vs "first place" for a contest prize → true).
- Accept a coarser-but-consistent date (gold "May 2023" vs prediction "May 2023" or "8 May 2023" → true); mark false only if the date actually differs.
- Mark false when the prediction contradicts the gold fact, omits it, gives a wrong name/date/number, or says it does not know.`

const judgeMem0AlignedSystemPrompt = `You grade a predicted answer against a gold answer for a question about a conversation. Output STRICT JSON only: {"correct": true|false}.

Judge recalled knowledge by semantic meaning rather than exact phrasing. Mark "correct": true under these rules:
- Give partial credit when the prediction includes at least one correct item from a gold list. Mark false only when it includes none of the gold items.
- Treat synonyms and paraphrases of the same concept as correct.
- Do not penalize extra details or greater specificity when the prediction still includes the gold answer's core fact.
- Treat dates within 14 days of each other as correct: count the day gap and mark the date wrong ONLY when that gap is greater than 14 days (e.g. "1 June" vs "12 June" is 11 days apart -> correct; "1 June" vs "20 June" is 19 days apart -> wrong). Treat durations within 50% as correct, and a relative date as correct when it fits the same time window.
- Accept semantic overlap on the same topic and core idea. For emotions about the same event, accept answers with the same emotional valence.
- When the prediction identifies the same named entity, person, character, or concept, accept the same referent even when its descriptive details differ.
- Focus on facts rather than wording; small differences in phrasing, scope, or specificity do not make a recalled fact wrong.

Mark "correct": false only when the prediction has zero correct gold items or addresses a completely different topic.`

func judgeSystemPromptFor(mode string) string {
	if mode == "mem0-aligned" {
		return judgeMem0AlignedSystemPrompt
	}
	return judgeSystemPrompt
}

func buildJudgePrompt(question, gold, predicted string) string {
	return fmt.Sprintf("QUESTION: %s\n\nGOLD ANSWER: %s\n\nPREDICTED ANSWER: %s\n\nReturn the JSON verdict now.", question, gold, predicted)
}

// parseJudgeVerdict extracts {"correct": bool} tolerantly.
func parseJudgeVerdict(raw string) bool {
	lower := strings.ToLower(raw)
	// Fast path: find "correct" then the next true/false token.
	idx := strings.Index(lower, "correct")
	if idx < 0 {
		return false
	}
	rest := lower[idx:]
	tIdx := strings.Index(rest, "true")
	fIdx := strings.Index(rest, "false")
	switch {
	case tIdx < 0:
		return false
	case fIdx < 0:
		return true
	default:
		return tIdx < fIdx
	}
}
