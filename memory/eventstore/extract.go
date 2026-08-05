package eventstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ModelCaller performs one text-in/text-out model call. It mirrors the
// pipeline.ModelCaller shape so the eval harness can wire the same provider
// adapter for both fact extraction and event extraction; tests inject a mock.
type ModelCaller func(ctx context.Context, system, user string) (string, error)

// ExtractInput is one message plus its conversation context.
type ExtractInput struct {
	ConversationID    string
	SourceLedgerID    string
	Speaker           string
	MessageText       string
	ConversationContext []string // recent message texts, oldest first
}

// ExtractionStats are the audit counters for fail-closed event extraction
// (contracts/fail-closed.md, FR-006).
type ExtractionStats struct {
	Attempts       int
	Successes      int
	Failures       int
	FailureReasons map[string]int
}

func (s *ExtractionStats) record(reason string) {
	if s.FailureReasons == nil {
		s.FailureReasons = make(map[string]int)
	}
	s.FailureReasons[reason]++
}

// Extractor runs one event extraction per message and fail-closes on any
// model or schema failure. A nil call or nil extractor makes ExtractOne a
// hard error — the caller MUST fall back to the raw chunk path.
type Extractor struct {
	call  ModelCaller
	mu    sync.Mutex
	stats ExtractionStats
}

// NewExtractor wraps a model caller. A nil call yields an extractor that
// always fails closed (degraded store, zero behavior change).
func NewExtractor(call ModelCaller) *Extractor {
	if call == nil {
		call = func(context.Context, string, string) (string, error) {
			return "", fmt.Errorf("%w: nil model caller", ErrInvalidEvent)
		}
	}
	return &Extractor{call: call}
}

// Stats returns a copy of the cumulative extraction audit counters.
func (x *Extractor) Stats() ExtractionStats {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := x.stats
	out.FailureReasons = make(map[string]int, len(x.stats.FailureReasons))
	for k, v := range x.stats.FailureReasons {
		out.FailureReasons[k] = v
	}
	return out
}

// ExtractOne extracts and schema-validates one event from a message. It never
// returns a partially-validated event: any model call error, malformed output,
// or schema violation fail-closes with a non-nil error and the caller MUST
// discard the message's event and fall back to the raw chunk path.
func (x *Extractor) ExtractOne(ctx context.Context, input ExtractInput) (*Event, error) {
	x.mu.Lock()
	x.stats.Attempts++
	x.mu.Unlock()
	raw, err := x.call(ctx, EventExtractionSystemPrompt, buildEventUserPrompt(input))
	if err != nil {
		x.mu.Lock()
		x.stats.Failures++
		x.stats.record("model_call")
		x.mu.Unlock()
		slog.Warn("eventstore: extraction model call failed", "conv", input.ConversationID, "err", err)
		return nil, fmt.Errorf("eventstore: model call: %w", err)
	}
	ev, err := ValidateLenient([]byte(cleanJSON(raw)))
	if err != nil {
		x.mu.Lock()
		x.stats.Failures++
		x.stats.record("schema")
		x.mu.Unlock()
		slog.Warn("eventstore: extraction schema validation failed", "conv", input.ConversationID, "err", err)
		return nil, err
	}
	ev.ID = eventID(ev, input.SourceLedgerID)
	for i := range ev.FactEntries {
		ev.FactEntries[i].Grounded = true
	}
	x.mu.Lock()
	x.stats.Successes++
	x.mu.Unlock()
	return ev, nil
}

// eventID derives a deterministic ID from the event content and its source,
// so a rebuild over identical input+config reproduces the same projection.
func eventID(ev *Event, sourceID string) string {
	payload, _ := json.Marshal(ev)
	h := sha256.Sum256(append(payload, []byte(sourceID)...))
	return "ev-" + hex.EncodeToString(h[:8])
}

// cleanJSON extracts the first balanced JSON object from a model reply that may
// wrap it in prose or markdown.
func cleanJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return s
	}
	return s[start : end+1]
}

// EventExtractionSystemPrompt instructs a small model to produce a strict-JSON
// dual-perspective event entry (contracts/extract-prompt.md).
const EventExtractionSystemPrompt = `You are a memory extractor. Convert ONE conversation message into an "event entry" as strict JSON.

Include:
1. fact_entries (factual perspective): what happened in this message — objective event descriptions, one fact per item, natural language.
2. relation_entries (relational perspective): interpersonal / causal / co-participation / temporal-order / preference relations evident in this message.
   relation_type is one of: interpersonal | causal | co_participation | temporal_order | preference.
   subject/object are the involved entities; text keeps the contextual natural language (e.g. "Melanie is interested in Caroline's Pride experience and wants to go together next time").
3. absolute_ts: an ISO-8601 timestamp if the message states an explicit absolute time; otherwise empty string.
4. relative_ref: the verbatim relative time reference in the message (e.g. "last year", "next Wednesday"); empty string if none.

Constraints:
- Extract ONLY what is explicitly stated in the message. Never invent facts, relations, entities, or times beyond it.
- fact_entries MUST be a non-empty JSON ARRAY of objects {"text": "...", "grounded": true}. Never output fact_entries as a plain string or empty array.
- relation_entries MUST be a JSON ARRAY (may be empty []). Each element is {"relation_type": "...", "subject": "...", "object": "...", "text": "..."}.
- "source_ledger_ids" MUST be a non-empty JSON ARRAY containing exactly the source_id printed next to the TARGET MESSAGE. Never output it empty.
- Each fact/relation is ONE short sentence; resolve pronouns; name subjects explicitly.
- Output STRICT JSON only, matching the schema exactly — no markdown, no prose, no extra fields.

Output shape:
{"conversation_id":"...","source_ledger_ids":["..."],"speaker":"...","fact_entries":[{"text":"...","grounded":true}],"relation_entries":[{"relation_type":"co_participation","subject":"...","object":"...","text":"..."}],"absolute_ts":"YYYY-MM-DDTHH:MM:SSZ","relative_ref":"..."}`

// buildEventUserPrompt renders the conversation context and the target message.
func buildEventUserPrompt(input ExtractInput) string {
	var b strings.Builder
	if input.ConversationID != "" {
		fmt.Fprintf(&b, "CONVERSATION: %s\n\n", input.ConversationID)
	}
	if len(input.ConversationContext) > 0 {
		b.WriteString("RECENT CONTEXT:\n")
		for i, ctx := range input.ConversationContext {
			if strings.TrimSpace(ctx) == "" {
				continue
			}
			fmt.Fprintf(&b, "[ctx-%d] %s\n", i, oneLine(ctx))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "TARGET MESSAGE (speaker=%s, source_id=%s):\n%s\n\n",
		input.Speaker, input.SourceLedgerID, oneLine(input.MessageText))
	b.WriteString("Return the JSON now.")
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
