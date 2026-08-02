package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
)

// errPlannerUnavailable wraps every reason a local Planner must fall back to
// the deterministic Compiler (unconfigured, sidecar error, planning timeout,
// unparsable/invalid proposal). It is deliberately NOT context.Canceled or
// context.DeadlineExceeded, so the evidencecompiler orchestrator's fallback
// branch fires instead of the propagate branch (spec FR-019: caller
// cancellation/deadline propagate unchanged; planner-internal timeout falls
// back, and never masquerades as successful degradation).
var errPlannerUnavailable = errors.New("local planner unavailable")

// defaultPlannerTimeout bounds one proposal generation. FR-034 gates p95 ≤
// 2.0s; this timeout is the hard fallback bound, not the latency target.
const defaultPlannerTimeout = 6 * time.Second

// plannerSystemPrompt instructs the sidecar model to emit the frozen proposal
// schema. The prompt template is itself a 023 training asset; it is frozen and
// refined together with the training recipe, and its digest is recorded in the
// eval protocol fingerprint.
const plannerSystemPrompt = `You are the Evidence Planner for a memory retrieval system. Given a question and a ranked list of candidate evidence, decide which evidence to keep/extract/merge and what constraints the answer must satisfy.

Emit ONLY a JSON object with this exact shape:
{"need":{"entities":["..."],"time_constraints":["YYYY-MM-DD"],"operands":[{"name":"...","satisfied":false}],"list_cardinality":{"known":false,"count":0},"update_state":"","gap":null},"actions":[{"kind":"KEEP","candidate_id":"...","source_id":"..."}]}

Action kinds are exactly: KEEP, EXTRACT, DROP, MERGE, FETCH_SOURCE.
Every action must reference only the candidate/source ids given to you. Do not invent ids, sources, or constraints.`

type localPlannerConfig struct {
	Provider     provider.Provider
	Model        string
	MaxTokens    int
	Timeout      time.Duration // 0 → defaultPlannerTimeout
	SystemPrompt string        // empty → plannerSystemPrompt
}

// localPlanner implements evidencecompiler.Planner by calling a self-hosted
// sidecar (vllm/ollama, OpenAI-compatible) serving a fine-tuned Qwen
// checkpoint. It is proposal-only: it holds no Store, Search, Bundle-write, or
// answer capability — the frozen Planner contract enforces that boundary.
type localPlanner struct {
	call    usageModelCaller
	timeout time.Duration
	system  string
}

func newLocalPlanner(cfg localPlannerConfig) (*localPlanner, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("%w: nil provider", errPlannerUnavailable)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("%w: empty model", errPlannerUnavailable)
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultPlannerTimeout
	}
	system := cfg.SystemPrompt
	if strings.TrimSpace(system) == "" {
		system = plannerSystemPrompt
	}
	return &localPlanner{
		call:    newUsageModelCaller(cfg.Provider, cfg.Model, maxTokens, 0, "planner", nil),
		timeout: timeout,
		system:  system,
	}, nil
}

func (p *localPlanner) Propose(ctx context.Context, query string, candidates []evidencecompiler.Candidate) (evidencecompiler.Proposal, error) {
	user := renderPlannerPrompt(query, candidates)
	timeoutCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	text, _, err := p.call(timeoutCtx, p.system, user)
	if err != nil {
		// Caller cancellation/deadline propagates unchanged (FR-019).
		if ctxErr := ctx.Err(); ctxErr != nil {
			return evidencecompiler.Proposal{}, ctxErr
		}
		// Planner-internal planning timeout falls back (plain error, not a
		// propagated context error — the orchestrator's fallback branch fires).
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return evidencecompiler.Proposal{}, fmt.Errorf("%w: planning timeout after %s", errPlannerUnavailable, p.timeout)
		}
		return evidencecompiler.Proposal{}, fmt.Errorf("%w: sidecar call: %v", errPlannerUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return evidencecompiler.Proposal{}, err
	}
	proposal, err := parsePlannerProposal(text)
	if err != nil {
		return evidencecompiler.Proposal{}, fmt.Errorf("%w: %v", errPlannerUnavailable, err)
	}
	return proposal, nil
}

// renderPlannerPrompt serialises the query and frozen candidates into the
// user turn. Candidate ids/text/sources are passed verbatim so the sidecar can
// ground Need/actions in the lineage; it never re-retrieves or enlarges the
// pool (FR-017).
func renderPlannerPrompt(query string, candidates []evidencecompiler.Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nRanked candidates:\n", query)
	if len(candidates) == 0 {
		b.WriteString("(none)\n")
	}
	for i, c := range candidates {
		fmt.Fprintf(&b, "[%d] id=%s kind=%s rank=%d score=%.4f sources=%s\n%s\n",
			i, c.ID, c.Kind, c.Rank, c.Score, strings.Join(c.SourceIDs, ","), c.Text)
	}
	b.WriteString("\nEmit the proposal JSON now.")
	return b.String()
}

// parsePlannerProposal maps a sidecar JSON response onto the frozen Proposal
// contract. The response schema uses snake_case keys for model-friendliness;
// the mapping is the adapter's internal boundary. Any structural violation
// (bad JSON, unknown action kind, unparsable gap) is an error, which the
// caller wraps in errPlannerUnavailable → deterministic fallback.
func parsePlannerProposal(text string) (evidencecompiler.Proposal, error) {
	trimmed := stripJSONFence(strings.TrimSpace(text))
	var out plannerOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return evidencecompiler.Proposal{}, fmt.Errorf("parse proposal json: %v", err)
	}
	actions := make([]evidencecompiler.Action, 0, len(out.Actions))
	for _, a := range out.Actions {
		kind, ok := plannerActionKind(a.Kind)
		if !ok {
			return evidencecompiler.Proposal{}, fmt.Errorf("invalid action kind %q", a.Kind)
		}
		action := evidencecompiler.Action{
			Kind:        kind,
			CandidateID: a.CandidateID,
			SourceID:    a.SourceID,
			ReasonCode:  a.ReasonCode,
		}
		if a.Span != nil {
			action.Span = &evidencecompiler.SourceSpan{
				SourceID:   a.Span.SourceID,
				StartChar:  a.Span.StartChar,
				EndChar:    a.Span.EndChar,
				SpanDigest: a.Span.SpanDigest,
			}
		}
		for _, s := range a.Sentences {
			sentence := evidencecompiler.GroundedSentence{Text: s.Text}
			for _, sp := range s.Sources {
				sentence.Sources = append(sentence.Sources, evidencecompiler.SourceSpan{
					SourceID:   sp.SourceID,
					StartChar:  sp.StartChar,
					EndChar:    sp.EndChar,
					SpanDigest: sp.SpanDigest,
				})
			}
			action.Sentences = append(action.Sentences, sentence)
		}
		actions = append(actions, action)
	}
	need := evidencecompiler.EvidenceNeed{
		Entities:        out.Need.Entities,
		TimeConstraints: out.Need.TimeConstraints,
		UpdateState:     out.Need.UpdateState,
		ListCardinality: evidencecompiler.Cardinality{Known: out.Need.ListCardinality.Known, Count: out.Need.ListCardinality.Count},
	}
	for _, o := range out.Need.Operands {
		need.Operands = append(need.Operands, evidencecompiler.Operand{Name: o.Name, Satisfied: o.Satisfied})
	}
	if out.Need.Gap != nil {
		gap, err := plannerGapToContract(*out.Need.Gap)
		if err != nil {
			return evidencecompiler.Proposal{}, fmt.Errorf("invalid gap: %v", err)
		}
		need.Gap = &gap
	}
	return evidencecompiler.Proposal{Need: need, Actions: actions}, nil
}

// plannerActionKind maps the model-facing action kind string onto the frozen
// ActionKind union. Unknown kinds are rejected (fail closed, FR-016).
func plannerActionKind(kind string) (evidencecompiler.ActionKind, bool) {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case string(evidencecompiler.ActionKeep):
		return evidencecompiler.ActionKeep, true
	case string(evidencecompiler.ActionExtract):
		return evidencecompiler.ActionExtract, true
	case string(evidencecompiler.ActionDrop):
		return evidencecompiler.ActionDrop, true
	case string(evidencecompiler.ActionMerge):
		return evidencecompiler.ActionMerge, true
	case string(evidencecompiler.ActionFetchSource):
		return evidencecompiler.ActionFetchSource, true
	default:
		return "", false
	}
}

func plannerGapToContract(g plannerGap) (evidencecompiler.StructuredGap, error) {
	var start, end *time.Time
	var err error
	if g.Start != "" {
		if start, err = parsePlannerTime(g.Start); err != nil {
			return evidencecompiler.StructuredGap{}, err
		}
	}
	if g.End != "" {
		if end, err = parsePlannerTime(g.End); err != nil {
			return evidencecompiler.StructuredGap{}, err
		}
	}
	kind := evidencecompiler.GapKind(strings.ToUpper(g.Kind))
	switch kind {
	case evidencecompiler.GapEntity, evidencecompiler.GapTimeRange, evidencecompiler.GapSecondOperand:
	default:
		return evidencecompiler.StructuredGap{}, fmt.Errorf("unknown gap kind %q", g.Kind)
	}
	return evidencecompiler.StructuredGap{
		Kind:       kind,
		Entity:     g.Entity,
		Start:      start,
		End:        end,
		Operand:    g.Operand,
		SourceNeed: g.SourceNeed,
	}, nil
}

func parsePlannerTime(s string) (*time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc, nil
		}
	}
	return nil, fmt.Errorf("unparsable time %q", s)
}

// stripJSONFence tolerates a ```json … ``` wrapper some chat templates add.
func stripJSONFence(s string) string {
	for _, marker := range []string{"```json", "```JSON"} {
		if strings.HasPrefix(s, marker) {
			s = strings.TrimPrefix(s, marker)
			break
		}
	}
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// plannerOutput mirrors the frozen Proposal contract in a model-facing
// snake_case JSON shape. It is the adapter's private wire format.
type plannerOutput struct {
	Need    plannerNeed      `json:"need"`
	Actions []plannerAction  `json:"actions"`
}

type plannerNeed struct {
	Entities        []string           `json:"entities"`
	TimeConstraints []string           `json:"time_constraints"`
	Operands        []plannerOperand   `json:"operands"`
	ListCardinality plannerCardinality `json:"list_cardinality"`
	UpdateState     string             `json:"update_state"`
	Gap             *plannerGap        `json:"gap"`
}

type plannerOperand struct {
	Name      string `json:"name"`
	Satisfied bool   `json:"satisfied"`
}

type plannerCardinality struct {
	Known bool `json:"known"`
	Count int  `json:"count"`
}

type plannerGap struct {
	Kind       string `json:"kind"`
	Entity     string `json:"entity"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Operand    string `json:"operand"`
	SourceNeed string `json:"source_need"`
}

type plannerAction struct {
	Kind        string                    `json:"kind"`
	CandidateID string                    `json:"candidate_id"`
	SourceID    string                    `json:"source_id"`
	Span        *plannerSourceSpan        `json:"span"`
	Sentences   []plannerGroundedSentence `json:"sentences"`
	ReasonCode  string                    `json:"reason_code"`
}

type plannerSourceSpan struct {
	SourceID   string `json:"source_id"`
	StartChar  int    `json:"start_char"`
	EndChar    int    `json:"end_char"`
	SpanDigest string `json:"span_digest"`
}

type plannerGroundedSentence struct {
	Text    string              `json:"text"`
	Sources []plannerSourceSpan `json:"sources"`
}
