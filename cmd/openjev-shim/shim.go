package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultPort            = 8020
	defaultUpstreamBaseURL = "http://127.0.0.1:8000/v1"
	defaultMaxQuestions    = 256
	// defaultUpstreamTimeout is the shim's own safety net. The binding bound is
	// the 051 client's deadline: the client hangs up first.
	defaultUpstreamTimeout = 120 * time.Second
	chatCompletionsPath    = "/chat/completions"
	maxRequestBytes        = 8 << 20
	maxUpstreamBytes       = 4 << 20
	// maxAttempts is one call plus the single stricter retry (contract §2 is an
	// answer-per-key protocol: silence would silently drop every candidate).
	maxAttempts = 2
	// maxJSONAttempts bounds how many "{" positions the tolerant parser tries.
	maxJSONAttempts = 64

	// maxTokensPerQuestion budgets the upstream reply for one question key. A real
	// shard entry (`"need_m123": 0.85,`) costs 12-15 tokens once BPE splits the key
	// digits, so 24 leaves roughly 2x headroom: the old 4-per-key formula truncated
	// a 64-key reply at 320 tokens before the closing brace, which is what produced
	// finish_reason=length and the 502.
	maxTokensPerQuestion = 24
	// responseTokenAllowance is the per-call, per-question-count-independent tax:
	// measured on the real box (051 P1.5c forensics), the upstream emits ~2497 chars
	// (~625+ tokens) of inline thinking prose before the JSON on every call, and it
	// is not exposed as a reasoning_content key. 1024 absorbs that plus the object
	// braces and the response skeleton the prompt asks the model to mirror.
	responseTokenAllowance = 1024
	// minCompletionTokens floors a small request: the inline-thinking tax alone is
	// ~625 tokens, so a 2-question budget of 24*2+1024 = 1072 would still truncate.
	minCompletionTokens = 1280
	// maxCompletionTokens caps one request's budget, so an over-wide shard cannot
	// ask the upstream for unbounded output.
	maxCompletionTokens = 8192
)

// shimConfig parameterizes the translation server. A zero value is usable as
// long as UpstreamModel is set for the default chat mode: every other field
// falls back to its documented default in applyDefaults.
type shimConfig struct {
	// Port is the loopback port serve binds (default 8020).
	Port int
	// UpstreamMode selects the upstream protocol: upstreamModeChat (the default)
	// speaks OpenAI-compatible chat.completions against UpstreamBaseURL;
	// upstreamModeTypesafe speaks the TypeSafe "systemone" evaluation-model
	// protocol against TypesafeEndpoint, which is the real Jev backend.
	UpstreamMode string
	// UpstreamBaseURL is the OpenAI-compatible base URL; chatCompletionsPath is
	// appended to it (default http://127.0.0.1:8000/v1). Chat mode only.
	UpstreamBaseURL string
	// UpstreamModel is the model revision sent upstream as "model" in chat mode,
	// where it is required. Typesafe mode sends TypesafeModel instead.
	UpstreamModel string
	// TypesafeEndpoint is the systemone evaluation-model endpoint (default
	// https://ai-gateway.vercel.sh/v4/ai/evaluation-model). Typesafe mode only.
	TypesafeEndpoint string
	// TypesafeModel is the evaluation-model id the systemone body pins (default
	// typesafe-ai/jev). Typesafe mode only.
	TypesafeModel string
	// TypesafeAPIKey is the systemone credential, sent as a bearer token. It is
	// required in typesafe mode and is never logged, echoed, or included in an
	// error or response.
	TypesafeAPIKey string
	// MaxQuestions bounds one request's question count; a larger request is
	// rejected with 413 before any model call (default 256).
	MaxQuestions int
	// MaxCompletionTokens overrides the computed upstream completion budget. Zero
	// (the default) computes it — see maxTokensFor — and a positive value is sent
	// verbatim, floor and cap included, because an operator who sets it means it.
	MaxCompletionTokens int
	// Thinking lets the upstream chat template run its chain-of-thought. It
	// defaults to false ("off"), where the shim sends chat_template_kwargs
	// {"enable_thinking": false} so a hybrid-thinking model (Qwen3.x on vllm) does
	// not spend the completion budget on reasoning before the JSON. Set it true
	// only for an upstream that rejects that body key.
	Thinking bool
	// UpstreamTimeout caps a single upstream call (default 120s).
	UpstreamTimeout time.Duration
	// Logger receives one line per request plus rejections (default log.Default).
	Logger *log.Logger
}

// applyDefaults fills every unset field and rejects a config that cannot work.
// It is idempotent, so parseConfig and newShimServer can both call it.
func (c *shimConfig) applyDefaults() error {
	mode := strings.ToLower(strings.TrimSpace(c.UpstreamMode))
	switch mode {
	case "":
		mode = upstreamModeChat
	case upstreamModeChat, upstreamModeTypesafe:
	default:
		return fmt.Errorf("OPENJEV_UPSTREAM must be %q or %q, got %q", upstreamModeChat, upstreamModeTypesafe, c.UpstreamMode)
	}
	c.UpstreamMode = mode
	if c.UpstreamMode == upstreamModeChat && strings.TrimSpace(c.UpstreamModel) == "" {
		return errors.New("OPENJEV_UPSTREAM_MODEL is required: an openjev run must pin the model revision it measured")
	}
	if strings.TrimSpace(c.UpstreamBaseURL) == "" {
		c.UpstreamBaseURL = defaultUpstreamBaseURL
	}
	c.UpstreamBaseURL = strings.TrimRight(c.UpstreamBaseURL, "/")
	if !strings.HasPrefix(c.UpstreamBaseURL, "http://") && !strings.HasPrefix(c.UpstreamBaseURL, "https://") {
		return fmt.Errorf("OPENJEV_UPSTREAM_BASE_URL must be an http(s) URL, got %q", c.UpstreamBaseURL)
	}
	if c.UpstreamMode == upstreamModeTypesafe {
		if strings.TrimSpace(c.TypesafeEndpoint) == "" {
			c.TypesafeEndpoint = defaultTypesafeEndpoint
		}
		c.TypesafeEndpoint = strings.TrimRight(c.TypesafeEndpoint, "/")
		if !strings.HasPrefix(c.TypesafeEndpoint, "http://") && !strings.HasPrefix(c.TypesafeEndpoint, "https://") {
			return fmt.Errorf("OPENJEV_TYPESAFE_ENDPOINT must be an http(s) URL, got %q", c.TypesafeEndpoint)
		}
		if strings.TrimSpace(c.TypesafeModel) == "" {
			c.TypesafeModel = defaultTypesafeModel
		}
		if strings.TrimSpace(c.TypesafeAPIKey) == "" {
			return errors.New("OPENJEV_TYPESAFE_API_KEY is required when OPENJEV_UPSTREAM=typesafe")
		}
	}
	if c.MaxQuestions <= 0 {
		c.MaxQuestions = defaultMaxQuestions
	}
	if c.UpstreamTimeout <= 0 {
		c.UpstreamTimeout = defaultUpstreamTimeout
	}
	if c.Logger == nil {
		c.Logger = log.Default()
	}
	return nil
}

// shimServer is immutable after construction, which is what makes it safe for
// the concurrent shard calls the 051 client issues.
type shimServer struct {
	cfg    shimConfig
	client *http.Client
	logger *log.Logger
}

func newShimServer(cfg shimConfig) (*shimServer, error) {
	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	return &shimServer{cfg: cfg, client: &http.Client{}, logger: cfg.Logger}, nil
}

func (s *shimServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/answers", s.handleAnswers)
	mux.HandleFunc("/healthz", s.handleHealthz)
	return mux
}

// serve runs the shim. It is unauthenticated by design and binds 127.0.0.1
// only: the eval box reaches it over loopback, and there is no credential to
// carry.
func serve(cfg shimConfig) error {
	srv, err := newShimServer(cfg)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", srv.cfg.Port),
		Handler:           srv.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv.logStartup(httpSrv.Addr)
	return httpSrv.ListenAndServe()
}

// logStartup names the effective upstream wiring once, for the operator who has
// to know which backend a run measured. It never carries the typesafe API key:
// credentials must not reach a log, a tool response or a tracked file.
func (s *shimServer) logStartup(addr string) {
	if s.cfg.UpstreamMode == upstreamModeTypesafe {
		s.logf("listening on http://%s (upstream=typesafe, endpoint %s, model %s, max_questions=%d)",
			addr, s.cfg.TypesafeEndpoint, s.cfg.TypesafeModel, s.cfg.MaxQuestions)
		return
	}
	s.logf("listening on http://%s (upstream=chat, base_url %s, model %s, max_questions=%d, max_completion_tokens=%d (0 = computed), thinking=%t)",
		addr, s.cfg.UpstreamBaseURL, s.cfg.UpstreamModel,
		s.cfg.MaxQuestions, s.cfg.MaxCompletionTokens, s.cfg.Thinking)
}

func (s *shimServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "healthz answers GET")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// handleAnswers is the pointer protocol entry point: one request in, one
// upstream chat call out, one probability per question key back.
func (s *shimServer) handleAnswers(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "answers answers POST")
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.logf("answers rejected: body exceeds %d bytes", maxRequestBytes)
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds %d bytes", maxRequestBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "read request body failed")
		return
	}

	var req pointerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "request is not the Jev pointer JSON shape")
		return
	}
	keys := slices.Sorted(maps.Keys(req.Questions))
	if len(keys) == 0 {
		writeError(w, http.StatusBadRequest, "request carries no questions")
		return
	}
	if len(keys) > s.cfg.MaxQuestions {
		s.logf("answers rejected: %d questions exceed OPENJEV_MAX_QUESTIONS=%d", len(keys), s.cfg.MaxQuestions)
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("%d questions exceed OPENJEV_MAX_QUESTIONS=%d", len(keys), s.cfg.MaxQuestions))
		return
	}

	probs, usage, attempts, problem := s.score(r.Context(), req, keys)
	if probs == nil {
		s.logf("answers failed: questions=%d attempts=%d elapsed=%s problem=%s",
			len(keys), attempts, time.Since(start), problem)
		writeError(w, http.StatusBadGateway, problem)
		return
	}

	// Every requested key is answered: a probability the model omitted is 0
	// (drop), which is the contract the client depends on.
	answers := make(map[string]float64, len(keys))
	for _, key := range keys {
		answers[key] = clampProbability(probs[key])
	}
	s.logf("answers ok: questions=%d attempts=%d elapsed=%s", len(keys), attempts, time.Since(start))
	writeJSON(w, http.StatusOK, pointerResponse{Probabilities: answers, Usage: usage})
}

// score translates the pointer request against whichever upstream protocol the
// config selected. The two modes differ only in the outside edge: the inside edge
// (pointer in, one probability per requested key out) is identical.
func (s *shimServer) score(ctx context.Context, req pointerRequest, keys []string) (map[string]float64, *pointerUsage, int, string) {
	if s.cfg.UpstreamMode == upstreamModeTypesafe {
		return s.scoreTypesafe(ctx, req, keys)
	}
	return s.scoreChat(ctx, req, keys)
}

// scoreChat translates once and, if the reply is not usable JSON, once more with
// the stricter reminder. Only the reply *shape* is retried: a transport failure or
// a non-2xx upstream is terminal, because asking again cannot fix an endpoint that
// is unreachable or refusing.
func (s *shimServer) scoreChat(ctx context.Context, req pointerRequest, keys []string) (map[string]float64, *pointerUsage, int, string) {
	var (
		usage   *pointerUsage
		problem string
	)
	for attempts := 1; ; attempts++ {
		content, finish, callUsage, err := s.callUpstream(ctx, req, keys, attempts > 1)
		usage = addUsage(usage, callUsage)
		if err != nil {
			return nil, usage, attempts, err.Error()
		}
		if probs, ok := firstProbabilityObject(content, keys); ok {
			return probs, usage, attempts, ""
		}
		problem = "upstream reply contained no JSON object answering the questions"
		if finish != "" {
			problem = fmt.Sprintf("%s (finish_reason=%s)", problem, finish)
		}
		if attempts >= maxAttempts {
			return nil, usage, attempts, problem
		}
	}
}

// callUpstream sends one chat.completions request for the whole pointer request.
// strict marks the retry, which appends the JSON-only reminder.
func (s *shimServer) callUpstream(ctx context.Context, req pointerRequest, keys []string, strict bool) (string, string, *pointerUsage, error) {
	call := chatRequest{
		Model:       s.cfg.UpstreamModel,
		Messages:    promptMessages(req, keys, strict),
		Temperature: 0,
		MaxTokens:   maxTokensFor(len(keys), s.cfg.MaxCompletionTokens),
	}
	if !s.cfg.Thinking {
		// vllm exposes the chat template's thinking switch as a request-body
		// extension rather than a serve-time flag: without it a hybrid-thinking
		// model spends the completion budget on chain-of-thought and the JSON
		// answer never arrives inside the budget.
		call.ChatTemplateKwargs = map[string]bool{"enable_thinking": false}
	}
	body, err := json.Marshal(call)
	if err != nil {
		return "", "", nil, fmt.Errorf("encode upstream request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, s.cfg.UpstreamTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		s.cfg.UpstreamBaseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return "", "", nil, fmt.Errorf("build upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return "", "", nil, fmt.Errorf("upstream call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBytes))
	if err != nil {
		return "", "", nil, fmt.Errorf("read upstream response: %w", err)
	}

	var out chatResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil && resp.StatusCode == http.StatusOK {
			return "", "", nil, fmt.Errorf("upstream reply is not chat.completions JSON: %w", err)
		}
	}
	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			message = out.Error.Message
		}
		return "", "", out.Usage, fmt.Errorf("upstream error: %s", message)
	}
	content, finish := assistantText(out)
	return content, finish, out.Usage, nil
}

// assistantText picks the first non-empty assistant message. A reply that was cut
// off mid-reasoning leaves content empty; the shim refuses to mine a reasoning
// draft (reasoning_content) as the answer, because a probability drafted
// mid-thought was not the model's conclusion.
func assistantText(out chatResponse) (string, string) {
	for _, choice := range out.Choices {
		if strings.TrimSpace(choice.Message.Content) != "" {
			return choice.Message.Content, choice.FinishReason
		}
	}
	if len(out.Choices) > 0 {
		return "", out.Choices[0].FinishReason
	}
	return "", ""
}

// promptMessages is the whole translation: the pointer request is rendered as
// one system instruction plus one user prompt, optionally with the retry
// reminder. Key order is stable so a rerun produces a byte-identical prompt.
func promptMessages(req pointerRequest, keys []string, strict bool) []chatMessage {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt(req, keys)},
	}
	if strict {
		messages = append(messages, chatMessage{Role: "user", Content: strictReminder(keys)})
	}
	return messages
}

const systemPrompt = "You are a calibrated relevance estimator. For every question you output the probability, in [0,1], " +
	"that the proposition it states is true for the given query and memories: 0 means certainly false, 0.5 means a coin flip, " +
	"1 means certainly true. Judge each question on its own; do not renormalize. " +
	"Reply with a single JSON object mapping every question key to a number, and nothing else: no prose, no markdown, no code fences, no explanations."

func userPrompt(req pointerRequest, keys []string) string {
	var b strings.Builder
	b.WriteString("Query:\n")
	b.WriteString(strings.TrimSpace(req.State.Query))
	b.WriteString("\n\nMemories:\n")
	memoryKeys := slices.Sorted(maps.Keys(req.State.Memories))
	if len(memoryKeys) == 0 {
		b.WriteString("(none)\n")
	}
	for _, key := range memoryKeys {
		memory := req.State.Memories[key]
		fmt.Fprintf(&b, "[%s] name: %s\n", key, memory.Name)
		fmt.Fprintf(&b, "text: %s\n", memory.Text)
	}
	b.WriteString("\nQuestions (answer every key):\n")
	for _, key := range keys {
		question := req.Questions[key]
		fmt.Fprintf(&b, "- %s (type: %s): %s\n", key, question.Type, question.Instructions)
	}
	b.WriteString("\nReply with JSON only: one entry per question key, each value a number in [0,1], e.g.\n")
	b.WriteString(responseSkeleton(keys))
	return b.String()
}

func strictReminder(keys []string) string {
	var b strings.Builder
	b.WriteString("Your previous reply was not JSON. Reply with JSON ONLY: a single object holding exactly these keys, each value a number in [0,1], with no prose, no markdown and no code fences.\n")
	b.WriteString("JSON ONLY:\n")
	b.WriteString(responseSkeleton(keys))
	return b.String()
}

// responseSkeleton renders the expected answer shape with every requested key,
// so a key the model would otherwise invent shows up as a mismatch.
func responseSkeleton(keys []string) string {
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, fmt.Sprintf("%q: 0.0", key))
	}
	return "{" + strings.Join(entries, ", ") + "}"
}

// maxTokensFor is the upstream completion budget: a per-key allowance plus the
// inline-thinking tax, floored for small requests and capped for wide ones. A
// positive override (OPENJEV_MAX_COMPLETION_TOKENS) replaces the computed value
// verbatim — floor and cap included — so an operator can fit an upstream's context
// window, or shrink a test's cost, without a rebuild.
//
// Measured on the real box (051 P1.5c): one `"need_mNNN": 0.85,` entry costs
// 12-15 tokens (24 per key leaves ~2x headroom), and the upstream prepends ~2497
// chars (~625+ tokens) of inline thinking on every call regardless of question
// count (1024 of allowance). The previous 4-per-key + 64 formula is what truncated
// a 64-question reply at 320 tokens and produced finish_reason=length ->
// unparseable JSON -> retry -> 502.
//
// Examples: 2 questions -> 1280 (floor, 1072 computed); 64 -> 2560; 400 -> 8192
// (cap, 10624 computed).
func maxTokensFor(questions, override int) int {
	if override > 0 {
		return override
	}
	budget := maxTokensPerQuestion*questions + responseTokenAllowance
	if budget < minCompletionTokens {
		return minCompletionTokens
	}
	if budget > maxCompletionTokens {
		return maxCompletionTokens
	}
	return budget
}

// firstProbabilityObject finds the first {...} object in the reply that decodes
// into a flat key->number map carrying at least one requested question key.
// Leading prose, a reasoning span, a code fence and a nested wrapper
// ({"probabilities": {...}}) all parse: the scan starts at the first brace and
// advances one brace at a time, and the client tolerates those shapes too. A
// reply that answers no requested key at all counts as unusable and is retried
// rather than reported as "everything scores 0", which would silently empty the
// short list.
func firstProbabilityObject(text string, keys []string) (map[string]float64, bool) {
	text = dropReasoningSpans(text)
	requested := make(map[string]bool, len(keys))
	for _, key := range keys {
		requested[key] = true
	}
	for attempts, offset := 0, 0; attempts < maxJSONAttempts; attempts++ {
		rel := strings.IndexByte(text[offset:], '{')
		if rel < 0 {
			return nil, false
		}
		start := offset + rel
		if block, balanced := balancedJSONObject(text, start); balanced {
			var candidate map[string]float64
			if err := json.Unmarshal([]byte(block), &candidate); err == nil {
				for key := range candidate {
					if requested[key] {
						return candidate, true
					}
				}
			}
		}
		offset = start + 1
	}
	return nil, false
}

// balancedJSONObject returns text[start:] up to and including the brace that
// closes the object opened at start. It walks the reply byte by byte instead of
// decoding a reader, and braces inside JSON strings (honoring backslash escapes)
// are data rather than structure, so an answer carrying text that itself contains
// "}" or an escaped quote cannot cut the object short.
func balancedJSONObject(text string, start int) (string, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], true
			}
		}
	}
	return "", false
}

// reasoningSpans are the chain-of-thought wrapper pairs the upstream model can
// wrap its reasoning in (the Qwen/DeepSeek think pair and its "-ing" spelling).
// Each tag is assembled from fragments so no literal in this file reads as markup.
var reasoningSpans = [][2]string{
	{"<" + "think" + ">", "<" + "/think" + ">"},
	{"<" + "thinking" + ">", "<" + "/thinking" + ">"},
}

// dropReasoningSpans removes the model's chain-of-thought spans. A probability
// drafted inside a reasoning span is a draft, not the answer, so the span is
// dropped before the JSON is located; the surrounding prose needs no handling at
// all, because firstProbabilityObject starts at the first brace. The scan uses
// strings.Cut, so it never computes tag offsets of its own.
func dropReasoningSpans(text string) string {
	for _, pair := range reasoningSpans {
		text = dropReasoningSpan(text, pair[0], pair[1])
	}
	return text
}

// dropReasoningSpan removes every open..close span for one tag pair. An
// unterminated span swallows the remainder, because a model that never closed its
// thought has no final answer after it.
func dropReasoningSpan(text, open, close string) string {
	if open == "" || close == "" {
		// Both tags are non-empty constants; an empty one would make this loop
		// non-terminating, so leave the reply untouched instead of spinning on it.
		return text
	}
	var b strings.Builder
	for {
		before, rest, found := strings.Cut(text, open)
		if !found {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(before)
		_, after, closed := strings.Cut(rest, close)
		if !closed {
			return b.String()
		}
		text = after
	}
}

// clampProbability keeps an answer inside [0,1]: the wire contract is a
// probability, and a model that says 1.7 or -0.4 is off the scale rather than
// merely uncertain.
func clampProbability(p float64) float64 {
	switch {
	case p < 0:
		return 0
	case p > 1:
		return 1
	default:
		return p
	}
}

// addUsage accumulates both attempts, so a retried call reports what it really
// cost. A reply without usage stays unknown instead of being counted as zero.
func addUsage(total, next *pointerUsage) *pointerUsage {
	if next == nil {
		return total
	}
	if total == nil {
		return &pointerUsage{PromptTokens: next.PromptTokens, CompletionTokens: next.CompletionTokens}
	}
	return &pointerUsage{
		PromptTokens:     total.PromptTokens + next.PromptTokens,
		CompletionTokens: total.CompletionTokens + next.CompletionTokens,
	}
}

func (s *shimServer) logf(format string, args ...any) {
	s.logger.Printf(format, args...)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError answers with the {"error":{"message": ...}} shape the client reads
// when a call fails, so the degradation is diagnosable from the client side.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: errorField{Message: message}})
}

// --- typesafe upstream mode -------------------------------------------------
//
// The real Jev backend is the TypeSafe "systemone" evaluation model served by the
// Vercel AI Gateway: the caller posts {model, state, questions} once and reads
// {answers: {<id>: {choice, probabilities}}, usage} back. The pointer protocol's
// ids travel through unchanged, so the translation maps one pointer request onto
// one systemone request and one pointer key onto one choice question.

const (
	// upstreamModeChat is the default: OpenAI-compatible chat.completions against
	// UpstreamBaseURL, i.e. the local "openjev" approximation.
	upstreamModeChat = "chat"
	// upstreamModeTypesafe speaks the systemone protocol against TypesafeEndpoint:
	// the real Jev backend, labeled "jev" rather than "openjev" in eval claims.
	upstreamModeTypesafe = "typesafe"

	defaultTypesafeEndpoint = "https://ai-gateway.vercel.sh/v4/ai/evaluation-model"
	defaultTypesafeModel    = "typesafe-ai/jev"

	// aiGatewayHostFragment marks the Vercel AI Gateway, whose requests must carry
	// the protocol/specification/model headers below. A self-hosted systemone
	// endpoint must not receive them.
	aiGatewayHostFragment = "ai-gateway"

	headerAIGatewayProtocol  = "ai-gateway-protocol-version"
	headerAIGatewaySpec      = "ai-evaluation-model-specification-version"
	headerAIModelID          = "ai-model-id"
	typesafeProtocolVersion  = "0.0.1"
	typesafeSpecificationVer = "4"

	// typesafeDescriptor labels the caller in the shared state.
	typesafeDescriptor = "engram read-side relevance filter"
	// typesafeCriteriaYes/No and the instruction pair are the fixed System-One
	// choice question the read-side filter asks about each memory; the answer's
	// "yes" probability is what the pointer protocol reports.
	typesafeCriteriaYes = "this memory is needed to answer the query"
	typesafeCriteriaNo  = "this memory is not needed"
	// typesafeGoal/typesafeRule are the generic question kept for a key that does
	// not resolve to a memory (the write gate's keys, an id outside the "need_"
	// convention); the shared state still carries the query and every memory.
	typesafeGoal = "decide whether the memory is needed for the query"
	typesafeRule = "judge strictly by relevance to answering the query"
	// typesafeGoalFormat builds the self-contained question's goal from its own
	// memory text and the query. Measured on the real gateway (051 T19 slice 12):
	// the shared-state-only question scored every memory ~0.95 for an unrelated
	// query, while this binding separated relevant from irrelevant perfectly —
	// the systemone model judges one question in isolation.
	typesafeGoalFormat = "decide whether the memory '%s' is needed to answer the query '%s'"
	// typesafeAgnosticRule is the second half of the measured-working recipe:
	// with a self-contained question, the explicit "no" for unrelated content is
	// what keeps a same-person-different-topic memory at no.
	typesafeAgnosticRule = "answer no for memories about unrelated people, topics, or facts"
	// needQuestionPrefix is the pointer convention binding a conditional question
	// to its memory: state.memories is keyed by the memory id and the question
	// about it is keyed "need_<memory id>" (filter/jev candidateRef).
	needQuestionPrefix = "need_"
	// maxTypesafeGoalMemoryChars bounds the memory text a question's goal embeds.
	// The shared state still carries the full text; 2000 characters is enough to
	// judge one memory's relevance and keeps one goal from crowding out its
	// siblings in the same call.
	maxTypesafeGoalMemoryChars = 2000

	// maxTypesafeRetries is the number of extra attempts after a retryable status:
	// the initial call plus two, spaced by typesafeBackoff.
	maxTypesafeRetries = 2
	// typesafeRetryBackoffStep is the first backoff; retry n waits n * step
	// (0.5s, then 1.0s).
	typesafeRetryBackoffStep = 500 * time.Millisecond

	// statusOverloadedStatus is the gateway's non-standard "model overloaded"
	// status (529); like a rate limit, it clears on its own.
	statusOverloaded = 529
)

// typesafeBackoff is the wait before retry number attempt (1 -> 0.5s, 2 -> 1.0s).
func typesafeBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * typesafeRetryBackoffStep
}

// typesafeRetryableStatus reports whether a status is worth asking again for: the
// gateway's rate limit, its overload status and 503. Every other status is
// terminal, because repeating it cannot change the answer.
func typesafeRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, statusOverloaded, http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}

// scoreTypesafe posts one systemone request per pointer request and retries only
// the statuses that can clear (429/529/503), up to maxTypesafeRetries with a
// doubling backoff. The retry budget lives inside a single upstream deadline, so
// the caller's own deadline (30s for the 051 eval) still bounds the whole call:
// a backoff is never allowed to outlive it.
func (s *shimServer) scoreTypesafe(ctx context.Context, req pointerRequest, keys []string) (map[string]float64, *pointerUsage, int, string) {
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.UpstreamTimeout)
	defer cancel()

	var usage *pointerUsage
	for attempt := 1; ; attempt++ {
		probs, callUsage, retryable, err := s.callTypesafeUpstream(callCtx, req, keys)
		usage = addUsage(usage, callUsage)
		if err == nil {
			return probs, usage, attempt, ""
		}
		if !retryable || attempt > maxTypesafeRetries {
			return nil, usage, attempt, err.Error()
		}
		if !sleepContext(callCtx, typesafeBackoff(attempt)) {
			return nil, usage, attempt, err.Error()
		}
	}
}

// callTypesafeUpstream posts one systemone request and translates the reply. The
// retryable flag reports a status the caller may repeat; every other failure
// (transport error, non-retryable status, a non-systemone body) is terminal.
func (s *shimServer) callTypesafeUpstream(ctx context.Context, req pointerRequest, keys []string) (map[string]float64, *pointerUsage, bool, error) {
	body, err := json.Marshal(typesafeRequestBody(s.cfg, req, keys))
	if err != nil {
		return nil, nil, false, fmt.Errorf("encode typesafe request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TypesafeEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, false, fmt.Errorf("build typesafe request: %w", err)
	}
	applyTypesafeHeaders(httpReq.Header, s.cfg.TypesafeEndpoint, s.cfg.TypesafeModel, s.cfg.TypesafeAPIKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, nil, false, fmt.Errorf("typesafe upstream call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBytes))
	if err != nil {
		return nil, nil, false, fmt.Errorf("read typesafe response: %w", err)
	}

	var out typesafeResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil && resp.StatusCode == http.StatusOK {
			return nil, nil, false, fmt.Errorf("typesafe reply is not systemone JSON: %w", err)
		}
	}
	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("typesafe upstream returned status %d", resp.StatusCode)
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			message = fmt.Sprintf("%s: %s", message, out.Error.Message)
		}
		return nil, usageOf(out.Usage), typesafeRetryableStatus(resp.StatusCode), fmt.Errorf("upstream error: %s", message)
	}
	return out.probabilities(), usageOf(out.Usage), false, nil
}

// sleepContext waits for d, or reports false when ctx ends first: a backoff must
// never outlive the deadline the whole call is bounded by.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// typesafeRequestBody is the whole pointer -> systemone translation: the pointer
// ids are preserved exactly (they are the client's accounting keys), the query and
// the memories travel in the shared state, and every key becomes one choice
// question. A key that resolves to a state memory gets a self-contained question —
// ITS memory text and the query in its own instructions — because on the real
// gateway a shared state alone gives the model no separation between a relevant
// and an unrelated memory; a key that does not resolve keeps the generic question.
func typesafeRequestBody(cfg shimConfig, req pointerRequest, keys []string) typesafeRequest {
	memories := req.State.Memories
	if memories == nil {
		// An object, not null: the shared state always carries a memories map.
		memories = map[string]pointerMemory{}
	}
	questions := make(map[string]typesafeQuestion, len(keys))
	for _, key := range keys {
		questions[key] = typesafeQuestionFor(key, memories, req.State.Query)
	}
	return typesafeRequest{
		Model:     cfg.TypesafeModel,
		State:     typesafeState{Descriptor: typesafeDescriptor, Query: req.State.Query, Memories: memories},
		Questions: questions,
	}
}

// typesafeQuestionFor builds one systemone choice question: the criteria are
// fixed, and the instructions are self-contained when the key resolves to a
// memory. A miss keeps the generic instructions (never an error): the write
// gate's keys and any id outside the convention must still be answerable.
func typesafeQuestionFor(key string, memories map[string]pointerMemory, query string) typesafeQuestion {
	question := typesafeQuestion{
		Type:         "choice",
		Criteria:     typesafeCriteria{Yes: typesafeCriteriaYes, No: typesafeCriteriaNo},
		Instructions: typesafeRuleSet{Goal: typesafeGoal, Rules: []string{typesafeRule}},
	}
	mem, ok := lookupTypesafeMemory(key, memories)
	if !ok {
		return question
	}
	question.Instructions.Goal = fmt.Sprintf(typesafeGoalFormat, typesafeMemoryExcerpt(mem.Text), query)
	question.Instructions.Rules = []string{typesafeRule, typesafeAgnosticRule}
	return question
}

// lookupTypesafeMemory resolves the memory a question asks about. The pointer
// protocol keys the memories map by the memory id and the question by
// "need_<memory id>"; a map keyed by the question id itself is accepted too, so
// a differently keyed caller binds instead of silently falling back.
func lookupTypesafeMemory(key string, memories map[string]pointerMemory) (pointerMemory, bool) {
	if mem, ok := memories[key]; ok {
		return mem, true
	}
	if memKey, ok := strings.CutPrefix(key, needQuestionPrefix); ok {
		if mem, ok := memories[memKey]; ok {
			return mem, true
		}
	}
	return pointerMemory{}, false
}

// typesafeMemoryExcerpt bounds the memory text a question's goal carries. The
// shared state still holds the full text; the excerpt keeps one long memory from
// spending the call's context on itself. A multi-byte rune straddling the bound
// is dropped, never split.
func typesafeMemoryExcerpt(text string) string {
	if len(text) <= maxTypesafeGoalMemoryChars {
		return text
	}
	cut := maxTypesafeGoalMemoryChars
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// applyTypesafeHeaders sets the endpoint's headers. The gateway requires its
// protocol/specification/model headers; a self-hosted systemone endpoint does not,
// so they are added only for an ai-gateway host. The credential travels in the
// Authorization header and never appears in a log or an error.
func applyTypesafeHeaders(header http.Header, endpoint, model, apiKey string) {
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+apiKey)
	if !isAIGatewayEndpoint(endpoint) {
		return
	}
	header.Set(headerAIGatewayProtocol, typesafeProtocolVersion)
	header.Set(headerAIGatewaySpec, typesafeSpecificationVer)
	header.Set(headerAIModelID, model)
}

// isAIGatewayEndpoint reports whether the endpoint host is the Vercel AI Gateway.
// The endpoint is validated as an http(s) URL before it gets here; the raw-string
// check keeps an unparsable host from silently dropping the required headers.
func isAIGatewayEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return strings.Contains(endpoint, aiGatewayHostFragment)
	}
	return strings.Contains(parsed.Host, aiGatewayHostFragment)
}

// usageOf converts the gateway's camelCase token counts into the OpenAI-style
// fields the client reads. A reply without usage stays unknown (nil) instead of
// being reported as zero tokens.
func usageOf(usage *typesafeUsage) *pointerUsage {
	if usage == nil {
		return nil
	}
	return &pointerUsage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens}
}

// typesafeRequest is the systemone evaluation-model payload.
type typesafeRequest struct {
	Model     string                      `json:"model"`
	State     typesafeState               `json:"state"`
	Questions map[string]typesafeQuestion `json:"questions"`
}

type typesafeState struct {
	Descriptor string                   `json:"descriptor"`
	Query      string                   `json:"query"`
	Memories   map[string]pointerMemory `json:"memories"`
}

type typesafeQuestion struct {
	Type         string           `json:"type"`
	Criteria     typesafeCriteria `json:"criteria"`
	Instructions typesafeRuleSet  `json:"instructions"`
}

// typesafeCriteria is the choice question's yes/no wording; the answer's "yes"
// probability is the one the shim reads back.
type typesafeCriteria struct {
	Yes string `json:"yes"`
	No  string `json:"no"`
}

type typesafeRuleSet struct {
	Goal  string   `json:"goal"`
	Rules []string `json:"rules"`
}

// typesafeResponse is the systemone answer shape: one answer per question id.
type typesafeResponse struct {
	Answers map[string]typesafeAnswer `json:"answers"`
	Usage   *typesafeUsage            `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type typesafeAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// typesafeUsage is the gateway's camelCase token accounting.
type typesafeUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// probabilities maps every answered id to its "yes" probability. An answer without
// a "yes" is 0 (drop) — the same contract the chat mode upholds, and the handler
// clamps the value into [0,1] for both modes.
func (r typesafeResponse) probabilities() map[string]float64 {
	out := make(map[string]float64, len(r.Answers))
	for id, answer := range r.Answers {
		out[id] = answer.Probabilities["yes"]
	}
	return out
}

// --- wire shapes -----------------------------------------------------------

// pointerRequest is the client's pointer protocol in (contract §2): the shared
// state carries the query and the memories once, and one question asks about
// each memory. The model field pins the client's Jev revision, which is not the
// shim's model, so the shim answers with its own pinned upstream revision.
type pointerRequest struct {
	Model     string                     `json:"model"`
	State     pointerState               `json:"state"`
	Questions map[string]pointerQuestion `json:"questions"`
}

type pointerState struct {
	Query    string                   `json:"query"`
	Memories map[string]pointerMemory `json:"memories"`
}

type pointerMemory struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type pointerQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

// pointerResponse is the canonical answer shape the client depends on: one
// probability per question key, every requested key present. Usage mirrors the
// upstream numbers so the client's FilterMeta can account tokens instead of
// reporting them unknown.
type pointerResponse struct {
	Probabilities map[string]float64 `json:"probabilities"`
	Usage         *pointerUsage      `json:"usage,omitempty"`
}

// pointerUsage is the OpenAI-compatible token accounting in both directions:
// the shim reports what the upstream counted for the call(s) it made.
type pointerUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type errorResponse struct {
	Error errorField `json:"error"`
}

type errorField struct {
	Message string `json:"message"`
}

// chatRequest is the upstream OpenAI-compatible payload. Only the fields the
// translation needs are sent.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	// ChatTemplateKwargs carries the vllm chat template's thinking switch. A nil
	// map omits the field entirely (OPENJEV_THINKING=on), for an upstream that
	// rejects unknown body keys.
	ChatTemplateKwargs map[string]bool `json:"chat_template_kwargs,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *pointerUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
