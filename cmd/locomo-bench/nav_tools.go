package main

// 029 agentic navigation tool set (contracts/navigation-tools.md). The model
// emits ONE JSON object per step via structured output (local_planner.go
// pattern — no vllm native function-calling); this file parses and validates
// those calls against the frozen whitelist, and executes them against the
// engine's existing hybrid retrieval. The engine itself is untouched.
//
// Tool whitelist (hard): search | expand_query | follow_entity | stop.
// Any other name is a navErrUnknownTool; malformed JSON is a navErrParse;
// structurally invalid arguments are a navErrInvalidArgs. All three collapse to
// the fail-closed single-shot path in agentic_nav.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// navToolNames is the frozen whitelist (contracts/navigation-tools.md).
var navToolNames = map[string]bool{
	"search":        true,
	"expand_query":  true,
	"follow_entity": true,
	"stop":          true,
}

const (
	// navToolMaxK is the per-tool retrieval depth cap (contracts: k ≤ 12).
	navToolMaxK = 12
	// navToolDefaultK is applied when k is omitted or ≤ 0.
	navToolDefaultK = 8
)

// navToolErrorKind classifies a rejected tool call so the orchestrator can
// decide retry vs fail-closed uniformly.
type navToolErrorKind int

const (
	navErrParse navToolErrorKind = iota // JSON did not unmarshal at all
	navErrUnknownTool                   // tool name not in the whitelist
	navErrInvalidArgs                   // tool_args structurally invalid
)

// navToolError is the typed error surface for parse/validation failures.
// Callers test with errors.As(*navToolError) and switch on Kind.
type navToolError struct {
	Kind navToolErrorKind
	Tool string // offending tool name (may be empty for navErrParse)
	Msg  string
}

func (e *navToolError) Error() string {
	switch e.Kind {
	case navErrParse:
		return fmt.Sprintf("nav tool call: parse failure: %s", e.Msg)
	case navErrUnknownTool:
		return fmt.Sprintf("nav tool call: unknown tool %q: %s", e.Tool, e.Msg)
	case navErrInvalidArgs:
		return fmt.Sprintf("nav tool call: invalid args for %q: %s", e.Tool, e.Msg)
	}
	return fmt.Sprintf("nav tool call: %s", e.Msg)
}

func (e *navToolError) Unwrap() error { return errNavUnavailable }

// navToolAction is the validated, type-dispatched form of one model tool call.
// Exactly one of the pointer fields is non-nil, matching Tool.
type navToolAction struct {
	Tool      string
	Rationale string
	Search    *navSearchArgs
	Expand    *navExpandArgs
	Follow    *navFollowArgs
	Stop      *navStopArgs
}

// navSearchArgs mirrors search(query, k). k ≤ 0 → navToolDefaultK.
type navSearchArgs struct {
	Query string `json:"query"`
	K     int    `json:"k"`
}

// navExpandArgs mirrors expand_query(text, k).
type navExpandArgs struct {
	Text string `json:"text"`
	K    int    `json:"k"`
}

// navFollowArgs mirrors follow_entity(entity, k).
type navFollowArgs struct {
	Entity string `json:"entity"`
	K      int    `json:"k"`
}

// navStopArgs mirrors stop(evidence_ids, assembly). assembly ∈
// {first_n, dedup}; empty → first_n.
type navStopArgs struct {
	EvidenceIDs []string `json:"evidence_ids"`
	Assembly    string   `json:"assembly"`
}

// extractNavJSONCandidates returns every candidate JSON object in a
// reasoning-model response, in text order: the whole response when it is
// already valid JSON, a ```json … ``` fence when present, and every
// brace-balanced object at each '{' position. The caller walks candidates and
// keeps the first that parses into a valid whitelisted tool call — the thinking
// text may contain JSON-shaped fragments that are not the real tool call.
func extractNavJSONCandidates(text string) [][]byte {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	var out [][]byte
	if json.Valid([]byte(trimmed)) {
		return [][]byte{[]byte(trimmed)}
	}
	if idx := strings.Index(trimmed, "```json"); idx >= 0 {
		rest := trimmed[idx+len("```json"):]
		if end := strings.Index(rest, "```"); end >= 0 {
			cand := strings.TrimSpace(rest[:end])
			if json.Valid([]byte(cand)) {
				out = append(out, []byte(cand))
			}
		}
	}
	for start := strings.IndexByte(trimmed, '{'); start >= 0; {
		if cand, ok := balancedJSONObject(trimmed, start); ok {
			out = append(out, cand)
		}
		next := strings.IndexByte(trimmed[start+1:], '{')
		if next < 0 {
			break
		}
		start = start + 1 + next
	}
	return out
}

// balancedJSONObject returns the bytes from trimmed[start] to the matching
// closing brace if the object is complete and valid JSON.
func balancedJSONObject(trimmed string, start int) ([]byte, bool) {
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(trimmed); i++ {
		ch := trimmed[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				cand := trimmed[start : i+1]
				if json.Valid([]byte(cand)) {
					return []byte(cand), true
				}
				return nil, false
			}
		}
	}
	return nil, false
}

// navToolCall is the raw wire shape the model emits each step.
type navToolCall struct {
	Tool      string          `json:"tool"`
	ToolArgs  json.RawMessage `json:"tool_args"`
	Rationale string          `json:"rationale"`
}

// parseNavToolCall maps one model response onto a validated navToolAction.
// The sidecar is a reasoning model (Qwen3.6-class): its raw response interleaves
// chain-of-thought text with the tool-call JSON, and the thinking may itself
// quote JSON-shaped fragments (schema examples, empty tool placeholders). So we
// walk every candidate JSON object and return the FIRST that parses into a
// valid whitelisted tool call — not just the first object in the text.
func parseNavToolCall(text string) (navToolAction, error) {
	candidates := extractNavJSONCandidates(text)
	if len(candidates) == 0 {
		return navToolAction{}, &navToolError{Kind: navErrParse, Msg: "no JSON object in response"}
	}
	var lastErr error
	for _, obj := range candidates {
		action, err := parseNavToolCallObject(obj)
		if err == nil {
			return action, nil
		}
		lastErr = err
	}
	return navToolAction{}, lastErr
}

// parseNavToolCallObject maps one candidate JSON object onto a validated action.
func parseNavToolCallObject(obj []byte) (navToolAction, error) {
	var raw navToolCall
	if err := json.Unmarshal(obj, &raw); err != nil {
		return navToolAction{}, &navToolError{Kind: navErrParse, Msg: err.Error()}
	}
	tool := strings.TrimSpace(raw.Tool)
	if !navToolNames[tool] {
		return navToolAction{}, &navToolError{Kind: navErrUnknownTool, Tool: tool, Msg: "not in whitelist {search,expand_query,follow_entity,stop}"}
	}
	action := navToolAction{Tool: tool, Rationale: strings.TrimSpace(raw.Rationale)}
	args := raw.ToolArgs
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	switch tool {
	case "search":
		var a navSearchArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return navToolAction{}, &navToolError{Kind: navErrInvalidArgs, Tool: tool, Msg: err.Error()}
		}
		if err := validateSearchArgs(&a); err != nil {
			return navToolAction{}, err
		}
		action.Search = &a
	case "expand_query":
		var a navExpandArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return navToolAction{}, &navToolError{Kind: navErrInvalidArgs, Tool: tool, Msg: err.Error()}
		}
		if err := validateExpandArgs(&a); err != nil {
			return navToolAction{}, err
		}
		action.Expand = &a
	case "follow_entity":
		var a navFollowArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return navToolAction{}, &navToolError{Kind: navErrInvalidArgs, Tool: tool, Msg: err.Error()}
		}
		if err := validateFollowArgs(&a); err != nil {
			return navToolAction{}, err
		}
		action.Follow = &a
	case "stop":
		var a navStopArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return navToolAction{}, &navToolError{Kind: navErrInvalidArgs, Tool: tool, Msg: err.Error()}
		}
		if err := validateStopArgs(&a); err != nil {
			return navToolAction{}, err
		}
		action.Stop = &a
	}
	return action, nil
}

func validateSearchArgs(a *navSearchArgs) error {
	if strings.TrimSpace(a.Query) == "" {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "search", Msg: "query is required"}
	}
	if a.K > navToolMaxK {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "search", Msg: fmt.Sprintf("k=%d exceeds cap %d", a.K, navToolMaxK)}
	}
	if a.K <= 0 {
		a.K = navToolDefaultK
	}
	return nil
}

func validateExpandArgs(a *navExpandArgs) error {
	if strings.TrimSpace(a.Text) == "" {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "expand_query", Msg: "text is required"}
	}
	if a.K > navToolMaxK {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "expand_query", Msg: fmt.Sprintf("k=%d exceeds cap %d", a.K, navToolMaxK)}
	}
	if a.K <= 0 {
		a.K = navToolDefaultK
	}
	return nil
}

func validateFollowArgs(a *navFollowArgs) error {
	if strings.TrimSpace(a.Entity) == "" {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "follow_entity", Msg: "entity is required"}
	}
	if a.K > navToolMaxK {
		return &navToolError{Kind: navErrInvalidArgs, Tool: "follow_entity", Msg: fmt.Sprintf("k=%d exceeds cap %d", a.K, navToolMaxK)}
	}
	if a.K <= 0 {
		a.K = navToolDefaultK
	}
	return nil
}

func validateStopArgs(a *navStopArgs) error {
	switch a.Assembly {
	case "", "first_n", "dedup":
	default:
		return &navToolError{Kind: navErrInvalidArgs, Tool: "stop", Msg: fmt.Sprintf("unknown assembly %q", a.Assembly)}
	}
	if a.Assembly == "" {
		a.Assembly = "first_n"
	}
	return nil
}

// executeNavTool runs one non-stop tool against the engine's existing hybrid
// retrieval (reusing memory.Retriever.Search as-is — engine untouched). It
// returns the raw top-k evidence for the step plus a stable key set for
// cross-step dedup. Never errors on a degraded signal (retrieval is
// silent-by-design in the engine).
func executeNavTool(ctx context.Context, retriever *memory.Retriever, action navToolAction) ([]NavEvidence, error) {
	query := ""
	switch {
	case action.Search != nil:
		query = action.Search.Query
	case action.Expand != nil:
		query = action.Expand.Text
	case action.Follow != nil:
		// Entity-anchored retrieval is best expressed as a lexical query on
		// the entity name; the hybrid path fuses semantic + keyword + entity
		// signals, so an exact entity match surfaces via the keyword/entity
		// legs. (029 US2; no engine change.)
		query = action.Follow.Entity
	default:
		return nil, fmt.Errorf("%w: cannot execute tool %q without search/expand/follow args", errNavUnavailable, action.Tool)
	}
	k := navToolDefaultK
	switch {
	case action.Search != nil:
		k = action.Search.K
	case action.Expand != nil:
		k = action.Expand.K
	case action.Follow != nil:
		k = action.Follow.K
	}
	results, err := retriever.Search(ctx, query, k)
	if err != nil {
		return nil, fmt.Errorf("%w: retrieve %q: %v", errNavUnavailable, action.Tool, err)
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
	return evidence, nil
}

// errNavUnavailable wraps every reason navigation must fail closed to the
// single-shot retrieval path (unconfigured sidecar, LLM error, unparsable tool
// call, step cap). It mirrors errPlannerUnavailable semantics: callers test it
// with errors.Is to route to the fallback branch, never to propagate.
var errNavUnavailable = errors.New("agentic navigation unavailable")
