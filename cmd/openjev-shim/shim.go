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
	"slices"
	"strings"
	"time"
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

	maxTokensPerQuestion   = 4
	responseTokenAllowance = 64
)

// shimConfig parameterizes the translation server. A zero value is usable as
// long as UpstreamModel is set: every other field falls back to its documented
// default in applyDefaults.
type shimConfig struct {
	// Port is the loopback port serve binds (default 8020).
	Port int
	// UpstreamBaseURL is the OpenAI-compatible base URL; chatCompletionsPath is
	// appended to it (default http://127.0.0.1:8000/v1).
	UpstreamBaseURL string
	// UpstreamModel is the model revision sent upstream as "model" (required).
	UpstreamModel string
	// MaxQuestions bounds one request's question count; a larger request is
	// rejected with 413 before any model call (default 256).
	MaxQuestions int
	// UpstreamTimeout caps a single upstream call (default 120s).
	UpstreamTimeout time.Duration
	// Logger receives one line per request plus rejections (default log.Default).
	Logger *log.Logger
}

// applyDefaults fills every unset field and rejects a config that cannot work.
// It is idempotent, so parseConfig and newShimServer can both call it.
func (c *shimConfig) applyDefaults() error {
	if strings.TrimSpace(c.UpstreamModel) == "" {
		return errors.New("OPENJEV_UPSTREAM_MODEL is required: an openjev run must pin the model revision it measured")
	}
	if strings.TrimSpace(c.UpstreamBaseURL) == "" {
		c.UpstreamBaseURL = defaultUpstreamBaseURL
	}
	c.UpstreamBaseURL = strings.TrimRight(c.UpstreamBaseURL, "/")
	if !strings.HasPrefix(c.UpstreamBaseURL, "http://") && !strings.HasPrefix(c.UpstreamBaseURL, "https://") {
		return fmt.Errorf("OPENJEV_UPSTREAM_BASE_URL must be an http(s) URL, got %q", c.UpstreamBaseURL)
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
	srv.logf("listening on http://%s (upstream %s, model %s, max_questions=%d)",
		httpSrv.Addr, srv.cfg.UpstreamBaseURL, srv.cfg.UpstreamModel, srv.cfg.MaxQuestions)
	return httpSrv.ListenAndServe()
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

// score translates once and, if the reply is not usable JSON, once more with the
// stricter reminder. Only the reply *shape* is retried: a transport failure or a
// non-2xx upstream is terminal, because asking again cannot fix an endpoint that
// is unreachable or refusing.
func (s *shimServer) score(ctx context.Context, req pointerRequest, keys []string) (map[string]float64, *pointerUsage, int, string) {
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
	body, err := json.Marshal(chatRequest{
		Model:       s.cfg.UpstreamModel,
		Messages:    promptMessages(req, keys, strict),
		Temperature: 0,
		MaxTokens:   maxTokensFor(len(keys)),
	})
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

// maxTokensFor bounds the reply: a few tokens per probability plus a small
// allowance for the JSON punctuation and any chat framing.
func maxTokensFor(questions int) int {
	return maxTokensPerQuestion*questions + responseTokenAllowance
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
