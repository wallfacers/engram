package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/filter/jev"
)

// --- wire mirrors: identical json tags to filter/jev/jev.go (§2) ------------
//
// The shim must speak the client's wire shape exactly. These mirrors are the
// test's independent copy of that shape, so a rename on either side fails the
// round-trip test below instead of silently degrading every eval call.

type wireRequest struct {
	Model     string                  `json:"model"`
	State     wireState               `json:"state"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireState struct {
	Query    string                `json:"query"`
	Memories map[string]wireMemory `json:"memories"`
}

type wireMemory struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type wireQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

// wireResponse mirrors the tolerant response shapes the client accepts
// (jev.go pointerResponse): probabilities / questions / results / usage / error.
type wireResponse struct {
	Probabilities map[string]float64         `json:"probabilities"`
	Questions     map[string]json.RawMessage `json:"questions"`
	Results       []wireResult               `json:"results"`
	Usage         *wireUsage                 `json:"usage"`
	Error         *wireError                 `json:"error"`
}

type wireResult struct {
	Key         string  `json:"key"`
	Question    string  `json:"question"`
	ID          string  `json:"id"`
	Probability float64 `json:"probability"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
}

type wireError struct {
	Message string `json:"message"`
}

func pointerRequestFor(query, model string, ids ...string) wireRequest {
	req := wireRequest{
		Model:     model,
		State:     wireState{Query: query, Memories: map[string]wireMemory{}},
		Questions: map[string]wireQuestion{},
	}
	for _, id := range ids {
		req.State.Memories[id] = wireMemory{Name: "memory-" + id, Text: "body-" + id}
		req.Questions["need_"+id] = wireQuestion{
			Type:         "noul",
			Instructions: fmt.Sprintf("Memory %s is necessary to answer or act on the query.", id),
		}
	}
	return req
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// postWireErr is goroutine-safe so the concurrency test can use it.
func postWireErr(url string, body []byte) (int, []byte, error) {
	resp, err := http.Post(url+"/answers", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func postWire(t *testing.T, url string, body []byte) (int, []byte) {
	t.Helper()
	status, raw, err := postWireErr(url, body)
	if err != nil {
		t.Fatalf("post pointer request: %v", err)
	}
	return status, raw
}

func decodeWire(t *testing.T, raw []byte) wireResponse {
	t.Helper()
	var out wireResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode pointer response %q: %v", raw, err)
	}
	return out
}

func requireStatus(t *testing.T, got, want int, raw []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d (body %s)", got, want, raw)
	}
}

// --- fake upstream (OpenAI-compatible chat.completions) --------------------

type upstreamChatRequest struct {
	Model       string                `json:"model"`
	Messages    []upstreamChatMessage `json:"messages"`
	Temperature float64               `json:"temperature"`
	MaxTokens   int                   `json:"max_tokens"`
	// ChatTemplateKwargs is the vllm body extension carrying the chat template's
	// thinking switch. It stays nil when the shim omits the field.
	ChatTemplateKwargs map[string]bool `json:"chat_template_kwargs"`
}

type upstreamChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func chatReply(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 128, "completion_tokens": 8},
	}
}

type upstreamRecorder struct {
	mu       sync.Mutex
	requests []upstreamChatRequest
	raws     [][]byte
}

func (r *upstreamRecorder) record(req upstreamChatRequest, raw []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	r.raws = append(r.raws, raw)
	return len(r.requests) - 1
}

// rawAt returns the exact bytes the shim sent. Field *presence* needs the bytes:
// a decoded nil map cannot distinguish an omitted chat_template_kwargs from one
// sent as null.
func (r *upstreamRecorder) rawAt(i int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.raws[i]
}

func (r *upstreamRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *upstreamRecorder) at(i int) upstreamChatRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[i]
}

// newFakeUpstream serves the fixed sequence of assistant contents; the last one
// repeats, so a two-attempt call sees garbage then a valid reply.
func newFakeUpstream(t *testing.T, contents ...string) (*httptest.Server, *upstreamRecorder) {
	t.Helper()
	rec := &upstreamRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			t.Errorf("read upstream chat request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req upstreamChatRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("decode upstream chat request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		content := contents[min(rec.record(req, raw), len(contents)-1)]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatReply(content))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func newShimForTest(t *testing.T, upstreamURL string, mutate func(*shimConfig)) *httptest.Server {
	t.Helper()
	cfg := shimConfig{
		UpstreamBaseURL: upstreamURL,
		UpstreamModel:   "openjev-test",
		MaxQuestions:    defaultMaxQuestions,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return serveShimForTest(t, cfg)
}

// serveShimForTest adds the test-only defaults (quiet logger, short upstream
// deadline) and serves the handler, so a config built by hand and one sourced
// from parseConfig take the identical production path.
func serveShimForTest(t *testing.T, cfg shimConfig) *httptest.Server {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	if cfg.UpstreamTimeout <= 0 {
		cfg.UpstreamTimeout = 2 * time.Second
	}
	srv, err := newShimServer(cfg)
	if err != nil {
		t.Fatalf("new shim server: %v", err)
	}
	httpSrv := httptest.NewServer(srv.handler())
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

// --- (a) golden pass-through, clamping, missing key -> 0 --------------------

func TestAnswersGoldenWithClampingAndMissingKeys(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m0": 1.7, "need_m1": -0.4, "need_m2": 0.42, "unrelated": 0.9}`)
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("what is the deploy code?", "openjev-test", "m0", "m1", "m2", "m3", "m4"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	out := decodeWire(t, raw)
	want := map[string]float64{
		"need_m0": 1,    // clamped down from 1.7
		"need_m1": 0,    // clamped up from -0.4
		"need_m2": 0.42, // passed through
		"need_m3": 0,    // absent from the reply -> 0
		"need_m4": 0,
	}
	if !reflect.DeepEqual(out.Probabilities, want) {
		t.Errorf("probabilities = %v, want %v", out.Probabilities, want)
	}
	if out.Usage == nil || out.Usage.PromptTokens == 0 {
		t.Errorf("usage must be passed through so the client can account tokens: %+v", out.Usage)
	}
	if rec.count() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (a parseable reply must not retry)", rec.count())
	}

	call := rec.at(0)
	if call.Model != "openjev-test" {
		t.Errorf("upstream model = %q, want the configured model", call.Model)
	}
	if call.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", call.Temperature)
	}
	if call.MaxTokens != 1280 {
		t.Errorf("max_tokens = %d, want 1280 (5 questions: 24*5+1024 = 1144, floored)", call.MaxTokens)
	}
	if len(call.Messages) != 2 || call.Messages[0].Role != "system" || call.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want one system + one user prompt", call.Messages)
	}
	prompt := call.Messages[1].Content
	for _, needle := range []string{"what is the deploy code?", "m4", "body-m4", "need_m4", "noul", "Memory m4 is necessary"} {
		if !strings.Contains(prompt, needle) {
			t.Errorf("user prompt is missing %q:\n%s", needle, prompt)
		}
	}
	for _, needle := range []string{"JSON", "[0,1]", "probability"} {
		if !strings.Contains(call.Messages[0].Content, needle) {
			t.Errorf("system prompt is missing %q:\n%s", needle, call.Messages[0].Content)
		}
	}
}

// --- (b) garbage -> one stricter retry -> success ---------------------------

func TestUnparseableReplyRetriesOnceWithStrictReminder(t *testing.T) {
	upstream, rec := newFakeUpstream(t,
		"memory m0 is clearly relevant, I would say yes",
		`{"need_m0": 0.8, "need_m1": 0.1}`)
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.8, "need_m1": 0.1}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
	if rec.count() != 2 {
		t.Fatalf("upstream calls = %d, want 2 (exactly one retry)", rec.count())
	}

	first := rec.at(0)
	if len(first.Messages) != 2 {
		t.Fatalf("first attempt messages = %d, want 2 (system + user)", len(first.Messages))
	}
	retry := rec.at(1)
	if len(retry.Messages) != 3 {
		t.Fatalf("retry messages = %d, want 3 (system + user + strict reminder)", len(retry.Messages))
	}
	reminder := retry.Messages[2]
	if reminder.Role != "user" {
		t.Errorf("reminder role = %q, want user", reminder.Role)
	}
	if !strings.Contains(reminder.Content, "JSON ONLY") {
		t.Errorf("reminder must demand JSON only:\n%s", reminder.Content)
	}
	for _, key := range []string{"need_m0", "need_m1"} {
		if !strings.Contains(reminder.Content, key) {
			t.Errorf("reminder must list every question key %q:\n%s", key, reminder.Content)
		}
	}
	if strings.Contains(retry.Messages[1].Content, "JSON ONLY") {
		t.Error("the strict reminder must only appear on the retry, not the first attempt")
	}
	if retry.Messages[1].Content != first.Messages[1].Content {
		t.Error("the retry must resend the same pointer protocol prompt")
	}
}

// --- (c) double garbage -> 502 ---------------------------------------------

func TestDoubleGarbageReturnsBadGateway(t *testing.T) {
	upstream, rec := newFakeUpstream(t, "still not json, sorry")
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusBadGateway, raw)

	out := decodeWire(t, raw)
	if out.Error == nil || out.Error.Message == "" {
		t.Fatalf("502 body must carry a short error message, got %s", raw)
	}
	if rec.count() != 2 {
		t.Fatalf("upstream calls = %d, want 2 (one retry, then fail)", rec.count())
	}
	if len(raw) > 512 {
		t.Errorf("error body must stay short, got %d bytes", len(raw))
	}
}

// --- (d) too many questions -> 413, no upstream call -----------------------

func TestTooManyQuestionsReturns413WithoutUpstreamCall(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{}`)
	shim := newShimForTest(t, upstream.URL, func(c *shimConfig) { c.MaxQuestions = 3 })

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1", "m2", "m3"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusRequestEntityTooLarge, raw)
	if rec.count() != 0 {
		t.Fatalf("upstream calls = %d, want 0 (an over-bound request must not reach the model)", rec.count())
	}
}

// --- (e) upstream failure -> 502 -------------------------------------------

func TestUpstreamErrorReturnsBadGatewayWithoutRetry(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "model not loaded"}})
	}))
	t.Cleanup(upstream.Close)

	shim := newShimForTest(t, upstream.URL, nil)
	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusBadGateway, raw)

	if out := decodeWire(t, raw); out.Error == nil || !strings.Contains(out.Error.Message, "model not loaded") {
		t.Errorf("502 body should surface the upstream message, got %s", raw)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (transport failures are not retried)", got)
	}
}

func TestUpstreamTimeoutReturnsBadGateway(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Drain the body first: net/http only watches for a client disconnect
		// once the request body has been read.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)

	shim := newShimForTest(t, upstream.URL, func(c *shimConfig) { c.UpstreamTimeout = 50 * time.Millisecond })
	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0"))

	start := time.Now()
	status, raw := postWire(t, shim.URL, body)
	elapsed := time.Since(start)

	requireStatus(t, status, http.StatusBadGateway, raw)
	if elapsed > 3*time.Second {
		t.Fatalf("timeout took %s, want the configured 50ms upstream deadline to bite", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (a timeout is not retried)", got)
	}
}

func TestUnreachableUpstreamReturnsBadGateway(t *testing.T) {
	shim := newShimForTest(t, "http://127.0.0.1:1", nil)
	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusBadGateway, raw)
}

// --- (f) concurrency ------------------------------------------------------

func TestConcurrentAnswersAreRaceClean(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m0": 0.7, "need_m1": 0.3}`)
	shim := newShimForTest(t, upstream.URL, nil)
	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))

	const workers = 16
	want := map[string]float64{"need_m0": 0.7, "need_m1": 0.3}

	var wg sync.WaitGroup
	failures := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, raw, err := postWireErr(shim.URL, body)
			if err != nil {
				failures <- fmt.Sprintf("post: %v", err)
				return
			}
			if status != http.StatusOK {
				failures <- fmt.Sprintf("status = %d (body %s)", status, raw)
				return
			}
			var out wireResponse
			if err := json.Unmarshal(raw, &out); err != nil {
				failures <- fmt.Sprintf("decode %q: %v", raw, err)
				return
			}
			if !reflect.DeepEqual(out.Probabilities, want) {
				failures <- fmt.Sprintf("probabilities = %v, want %v", out.Probabilities, want)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for msg := range failures {
		t.Error(msg)
	}
	if rec.count() != workers {
		t.Fatalf("upstream calls = %d, want %d (one per request)", rec.count(), workers)
	}
}

// --- (g) exact wire shape round-trip --------------------------------------

func TestPointerWireShapeRoundTrip(t *testing.T) {
	// The canonical request, frozen as bytes: field names/order must match the
	// tags filter/jev marshals, or the shim is not a drop-in backend.
	const wantRequest = `{"model":"openjev-test","state":{"query":"what is the code?","memories":{"m1":{"name":"memory-m1","text":"body-m1"}}},"questions":{"need_m1":{"type":"noul","instructions":"Memory m1 is necessary to answer or act on the query."}}}`
	req := pointerRequestFor("what is the code?", "openjev-test", "m1")
	if got := string(mustMarshal(t, req)); got != wantRequest {
		t.Fatalf("mirror request drifted from the filter/jev wire shape:\n got %s\nwant %s", got, wantRequest)
	}

	upstream, rec := newFakeUpstream(t, `{"need_m1": 0.61}`)
	shim := newShimForTest(t, upstream.URL, nil)

	status, raw := postWire(t, shim.URL, []byte(wantRequest))
	requireStatus(t, status, http.StatusOK, raw)

	out := decodeWire(t, raw)
	if len(out.Probabilities) != 1 || out.Probabilities["need_m1"] != 0.61 {
		t.Errorf("probabilities = %v, want {need_m1:0.61}", out.Probabilities)
	}
	if out.Questions != nil || out.Results != nil {
		t.Errorf("the shim must answer in the single canonical shape, got questions=%v results=%v", out.Questions, out.Results)
	}
	if out.Error != nil {
		t.Errorf("a successful reply must not carry an error: %+v", out.Error)
	}
	if out.Usage == nil || out.Usage.PromptTokens == 0 {
		t.Errorf("usage missing: %+v", out.Usage)
	}

	// The prompt must restate the pointer keys, so a rename on either side of
	// the translation breaks here rather than in a paid eval run.
	prompt := rec.at(0).Messages[1].Content
	for _, needle := range []string{"need_m1", "Memory m1 is necessary to answer or act on the query.", "m1", "body-m1"} {
		if !strings.Contains(prompt, needle) {
			t.Errorf("upstream prompt is missing %q:\n%s", needle, prompt)
		}
	}
}

// TestEngineClientRoundTrip drives the shim with the real filter/jev client:
// no mirror types are involved, so this is the exact production wire path.
func TestEngineClientRoundTrip(t *testing.T) {
	upstream, _ := newFakeUpstream(t, `{"need_m0": 0.9, "need_m1": 0.2, "need_m2": 0.9}`)
	shim := newShimForTest(t, upstream.URL, nil)

	client, err := jev.New(jev.Config{
		BaseURL:           shim.URL,
		Model:             "openjev-test",
		APIKey:            "test-key",
		Deadline:          5 * time.Second,
		PerRequestTimeout: 5 * time.Second,
	})
	if err != nil || client == nil {
		t.Fatalf("new jev client: %v %v", client, err)
	}

	cands := []filter.Candidate{
		{ID: "m0", Name: "memory-m0", Text: "body-m0", Score: 3},
		{ID: "m1", Name: "memory-m1", Text: "body-m1", Score: 2},
		{ID: "m2", Name: "memory-m2", Text: "body-m2", Score: 1},
	}
	probs, meta, err := client.Filter(context.Background(), "which memories matter?", cands)
	if err != nil {
		t.Fatalf("filter through the shim: %v (meta %+v)", err, meta)
	}
	if want := []float64{0.9, 0.2, 0.9}; !reflect.DeepEqual(probs, want) {
		t.Errorf("probabilities = %v, want %v", probs, want)
	}
	if meta.Degraded {
		t.Errorf("a successful shim call must not degrade: notes=%v", meta.Notes)
	}
	if meta.InputTokens != 128 || meta.OutputTokens != 8 {
		t.Errorf("token accounting = in:%d out:%d, want the upstream usage in:128 out:8", meta.InputTokens, meta.OutputTokens)
	}
	if want := 2; meta.Kept != want || meta.Dropped != len(cands)-want {
		t.Errorf("kept/dropped = %d/%d, want %d/%d", meta.Kept, meta.Dropped, want, len(cands)-want)
	}
}

// TestJevDeadlineIsCallerWidenable records how the 051 client deadline works:
// Config.Deadline is the whole-call bound and Config.PerRequestTimeout caps a
// single request at min(remaining, cap). Both are adapter-settable, so openjev
// wiring must widen both — no filter/ change is required.
func TestJevDeadlineIsCallerWidenable(t *testing.T) {
	const upstreamDelay = 1200 * time.Millisecond
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(upstreamDelay):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(chatReply(`{"need_m0": 0.9}`))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)

	shim := newShimForTest(t, upstream.URL, func(c *shimConfig) { c.UpstreamTimeout = 30 * time.Second })
	cands := []filter.Candidate{{ID: "m0", Name: "memory-m0", Text: "body-m0", Score: 1}}

	newClient := func(t *testing.T, cfg jev.Config) *jev.Client {
		t.Helper()
		cfg.BaseURL = shim.URL
		cfg.Model = "openjev-test"
		cfg.APIKey = "test-key"
		client, err := jev.New(cfg)
		if err != nil || client == nil {
			t.Fatalf("new jev client: %v %v", client, err)
		}
		return client
	}

	// Stock defaults: Deadline 1s and PerRequestTimeout 1s — a local 35B shim
	// cannot answer inside 1s, so the call degrades.
	if _, meta, err := newClient(t, jev.Config{}).Filter(context.Background(), "q", cands); err == nil || !meta.Degraded {
		t.Errorf("default 1s deadline: err = %v, degraded = %v; want a degraded failure", err, meta.Degraded)
	}

	// Widening only Deadline is not enough: the per-request cap still bites.
	if _, _, err := newClient(t, jev.Config{Deadline: 30 * time.Second}).Filter(context.Background(), "q", cands); err == nil {
		t.Error("Deadline alone must not bypass the default 1s per-request cap")
	}

	// The openjev wiring: widen both knobs.
	probs, meta, err := newClient(t, jev.Config{
		Deadline:          30 * time.Second,
		PerRequestTimeout: 30 * time.Second,
	}).Filter(context.Background(), "q", cands)
	if err != nil {
		t.Fatalf("widened deadline must let the shim answer: %v", err)
	}
	if want := []float64{0.9}; !reflect.DeepEqual(probs, want) {
		t.Errorf("probabilities = %v, want %v", probs, want)
	}
	if meta.Degraded {
		t.Errorf("widened call must not degrade: %v", meta.Notes)
	}
}

// --- reasoning-trace tolerance --------------------------------------------

// The wrappers below mirror what a reasoning model emits. This file spells them
// independently of shim.go (and from fragments) so these tests cannot pass
// vacuously if a constant there drifts or is corrupted.
const (
	upstreamThinkOpen     = "<" + "think" + ">"
	upstreamThinkClose    = "<" + "/think" + ">"
	upstreamThinkingOpen  = "<" + "thinking" + ">"
	upstreamThinkingClose = "<" + "/thinking" + ">"
)

func reasoningSpan(open, close, body string) string { return open + body + close }

func TestReasoningTraceIsNotAdoptedAsTheAnswer(t *testing.T) {
	// The draft inside the reasoning span answers need_m0 with 0.9; the final
	// answer after it says 0.2/0.8. Adopting the draft would score a memory
	// strongly on a thought the model then reconsidered.
	upstream, _ := newFakeUpstream(t,
		reasoningSpan(upstreamThinkOpen, upstreamThinkClose, "Maybe m0 matters. {\"need_m0\": 0.9} Let me reconsider.")+
			"Here is the answer:\n{\"need_m0\": 0.2, \"need_m1\": 0.8}")
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.2, "need_m1": 0.8}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want the post-thinking answer %v", got, want)
	}
}

// TestThinkingSpanProseAndJSON is the P1.5 acceptance sample: a completed
// reasoning span, prose, then the answer object.
func TestThinkingSpanProseAndJSON(t *testing.T) {
	upstream, rec := newFakeUpstream(t,
		reasoningSpan(upstreamThinkOpen, upstreamThinkClose, "I should weigh m0 and m1.")+
			"Here is the answer:\n{\"need_m0\": 0.2, \"need_m1\": 0.8}")
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.2, "need_m1": 0.8}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
	if rec.count() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (a parseable reply must not retry)", rec.count())
	}
}

// TestReasoningSpellingsAreBothDropped: whichever wrapper pair the model closes
// its thought with, the draft inside it must not reach the scan.
func TestReasoningSpellingsAreBothDropped(t *testing.T) {
	spans := []struct{ open, close string }{
		{upstreamThinkOpen, upstreamThinkClose},
		{upstreamThinkingOpen, upstreamThinkingClose},
	}
	for _, span := range spans {
		reply := reasoningSpan(span.open, span.close, `{"need_m0": 0.9}`) + `{"need_m0": 0.2}`
		if got, want := dropReasoningSpans(reply), `{"need_m0": 0.2}`; got != want {
			t.Errorf("span %q..%q: dropped to %q, want %q", span.open, span.close, got, want)
		}
	}
}

// TestFirstProbabilityObjectScansBalancedBraces pins the extraction itself: no
// offset arithmetic on tags, no dependence on where the JSON starts, and a
// balanced walk that survives stray braces in the surrounding prose.
func TestFirstProbabilityObjectScansBalancedBraces(t *testing.T) {
	keys := []string{"need_m0", "need_m1"}
	tests := []struct {
		name  string
		reply string
		want  map[string]float64
	}{
		{
			name: "reasoning span then prose then JSON",
			reply: reasoningSpan(upstreamThinkOpen, upstreamThinkClose, `draft {"need_m0": 0.9}`) +
				"Here is the answer:\n{\"need_m0\": 0.2, \"need_m1\": 0.8}",
			want: map[string]float64{"need_m0": 0.2, "need_m1": 0.8},
		},
		{
			name:  "stray brace pair in the prose before the answer",
			reply: "The set {m0, m1} is what matters.\n{\"need_m0\": 0.4}",
			want:  map[string]float64{"need_m0": 0.4},
		},
		{
			name:  "trailing prose after the answer",
			reply: "{\"need_m0\": 0.6}\n\nHope that helps!",
			want:  map[string]float64{"need_m0": 0.6},
		},
		{
			name:  "code fence and nested wrapper",
			reply: "Sure!\n```json\n{\"probabilities\": {\"need_m1\": 0.44}}\n```",
			want:  map[string]float64{"need_m1": 0.44},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := firstProbabilityObject(tt.reply, keys)
			if !ok {
				t.Fatalf("no probability object found in %q", tt.reply)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("probabilities = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBalancedJSONObjectSkipsBracesInsideStrings proves the walker treats braces
// inside JSON strings (and escaped quotes) as data: a naive counter would return
// at the brace inside the note and hand json.Unmarshal a truncated object.
func TestBalancedJSONObjectSkipsBracesInsideStrings(t *testing.T) {
	object := `{"note": "a } brace and {\" escaped quote", "n": 1}`
	block, ok := balancedJSONObject(object, 0)
	if !ok || block != object {
		t.Fatalf("balanced object = %q, %v; want the whole object %q", block, ok, object)
	}
	if partial, ok := balancedJSONObject(`{"need_m0": 0.5`, 0); ok {
		t.Errorf("a truncated object must not report a balanced block, got %q", partial)
	}
}

func TestNestedAndProseWrappedJSONIsAccepted(t *testing.T) {
	upstream, _ := newFakeUpstream(t, "Sure! ```json\n{\"probabilities\": {\"need_m0\": 0.44}}\n``` done")
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.44}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
}

// --- routing / health -----------------------------------------------------

func TestHealthzAndRouting(t *testing.T) {
	shim := newShimForTest(t, "http://127.0.0.1:1", nil)

	resp, err := http.Get(shim.URL + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		t.Errorf("/healthz = %d %q, want 200 \"ok\"", resp.StatusCode, body)
	}

	req, err := http.NewRequest(http.MethodGet, shim.URL+"/answers", nil)
	if err != nil {
		t.Fatalf("build GET /answers: %v", err)
	}
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /answers: %v", err)
	}
	defer got.Body.Close() //nolint:errcheck
	if got.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /answers = %d, want 405", got.StatusCode)
	}

	unknown, err := http.Post(shim.URL+"/nope", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post /nope: %v", err)
	}
	defer unknown.Body.Close() //nolint:errcheck
	if unknown.StatusCode != http.StatusNotFound {
		t.Errorf("POST /nope = %d, want 404", unknown.StatusCode)
	}

	status, raw := postWire(t, shim.URL, []byte("{not json"))
	requireStatus(t, status, http.StatusBadRequest, raw)

	status, raw = postWire(t, shim.URL, mustMarshal(t, wireRequest{Model: "openjev-test"}))
	requireStatus(t, status, http.StatusBadRequest, raw)
}

func TestOversizedBodyIsRejected(t *testing.T) {
	shim := newShimForTest(t, "http://127.0.0.1:1", nil)
	huge := bytes.Repeat([]byte("a"), maxRequestBytes+1024)
	status, raw := postWire(t, shim.URL, huge)
	requireStatus(t, status, http.StatusRequestEntityTooLarge, raw)
}

// --- configuration --------------------------------------------------------

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseConfigFromEnv(t *testing.T) {
	cfg, err := parseConfig(nil, envFrom(map[string]string{
		"OPENJEV_UPSTREAM_BASE_URL": "http://127.0.0.1:9000/v1",
		"OPENJEV_UPSTREAM_MODEL":    "Qwen/Qwen3.6-35B-A3B-FP8",
		"OPENJEV_MAX_QUESTIONS":     "64",
	}))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("port = %d, want the default %d", cfg.Port, defaultPort)
	}
	if cfg.UpstreamBaseURL != "http://127.0.0.1:9000/v1" {
		t.Errorf("base URL = %q", cfg.UpstreamBaseURL)
	}
	if cfg.UpstreamModel != "Qwen/Qwen3.6-35B-A3B-FP8" {
		t.Errorf("model = %q", cfg.UpstreamModel)
	}
	if cfg.MaxQuestions != 64 {
		t.Errorf("max questions = %d, want 64", cfg.MaxQuestions)
	}

	// Only the model is required; the base URL and the bounds keep defaults.
	cfg, err = parseConfig(nil, envFrom(map[string]string{"OPENJEV_UPSTREAM_MODEL": "m"}))
	if err != nil {
		t.Fatalf("parseConfig with defaults: %v", err)
	}
	if cfg.UpstreamBaseURL != defaultUpstreamBaseURL {
		t.Errorf("base URL = %q, want %q", cfg.UpstreamBaseURL, defaultUpstreamBaseURL)
	}
	if cfg.MaxQuestions != defaultMaxQuestions {
		t.Errorf("max questions = %d, want %d", cfg.MaxQuestions, defaultMaxQuestions)
	}

	if _, err := parseConfig(nil, envFrom(nil)); err == nil || !strings.Contains(err.Error(), "OPENJEV_UPSTREAM_MODEL") {
		t.Errorf("missing model: err = %v, want a named error", err)
	}
	for _, raw := range []string{"abc", "0", "-4"} {
		if _, err := parseConfig(nil, envFrom(map[string]string{
			"OPENJEV_UPSTREAM_MODEL": "m",
			"OPENJEV_MAX_QUESTIONS":  raw,
		})); err == nil {
			t.Errorf("OPENJEV_MAX_QUESTIONS=%q must be rejected", raw)
		}
	}
}

func TestParseConfigPortFlag(t *testing.T) {
	env := envFrom(map[string]string{"OPENJEV_UPSTREAM_MODEL": "m"})

	cfg, err := parseConfig([]string{"--port", "9123"}, env)
	if err != nil {
		t.Fatalf("parseConfig --port: %v", err)
	}
	if cfg.Port != 9123 {
		t.Errorf("port = %d, want 9123", cfg.Port)
	}

	for _, raw := range []string{"0", "-1", "70000"} {
		if _, err := parseConfig([]string{"--port", raw}, env); err == nil {
			t.Errorf("--port %s must be rejected", raw)
		}
	}
}

func TestNewShimServerDefaults(t *testing.T) {
	srv, err := newShimServer(shimConfig{UpstreamModel: "m"})
	if err != nil {
		t.Fatalf("newShimServer: %v", err)
	}
	if srv.cfg.UpstreamBaseURL != defaultUpstreamBaseURL || srv.cfg.MaxQuestions != defaultMaxQuestions {
		t.Errorf("defaults not applied: %+v", srv.cfg)
	}
	if srv.cfg.UpstreamTimeout <= 0 {
		t.Errorf("upstream timeout = %s, want a positive default", srv.cfg.UpstreamTimeout)
	}
	if srv.logger == nil {
		t.Error("logger must default to a usable logger")
	}

	if _, err := newShimServer(shimConfig{UpstreamBaseURL: "http://127.0.0.1:1"}); err == nil {
		t.Error("a missing upstream model must be rejected")
	}
}

// --- upstream budget & thinking switch (P1.5c) -----------------------------
//
// Two real-box smoke defects live here. (1) The old budget (4 tokens per
// question + 64) truncated a 64-key shard reply at 320 tokens, before the closing
// brace, so the reply arrived as `finish_reason=length` and unparseable JSON and
// the call retried into a 502; the upstream additionally prepends ~2497 chars
// (~625+ tokens) of inline thinking on every call, so the allowance is per call,
// not per key. (2) The hybrid-thinking upstream burned the completion budget on
// chain-of-thought, which the shim now switches off in the request body.

func memoryIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%02d", i)
	}
	return ids
}

func mustParseConfig(t *testing.T, env map[string]string) shimConfig {
	t.Helper()
	cfg, err := parseConfig(nil, envFrom(env))
	if err != nil {
		t.Fatalf("parseConfig(%v): %v", env, err)
	}
	return cfg
}

// mergedEnv layers extra onto base, so every case still carries the required
// OPENJEV_UPSTREAM_MODEL.
func mergedEnv(base, extra map[string]string) map[string]string {
	env := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// TestMaxTokensForBudgetBoundaries pins the budget arithmetic and its three
// documented boundaries: the 1280 floor, the per-key slope, and the 8192 cap.
func TestMaxTokensForBudgetBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		questions int
		override  int
		want      int
	}{
		{name: "no questions takes the floor", questions: 0, want: 1280},
		{name: "small request takes the floor", questions: 2, want: 1280}, // 24*2+1024 = 1072
		{name: "mid request scales per key", questions: 32, want: 1792},   // 24*32+1024
		{name: "sixty-four questions", questions: 64, want: 2560},         // 24*64+1024 — the real shard size
		{name: "last size below the cap", questions: 298, want: 8176},     // 24*298+1024
		{name: "first size past the cap", questions: 299, want: 8192},     // 8200 -> capped
		{name: "wide shard stays capped", questions: 1024, want: 8192},
		{name: "override wins over the computed value", questions: 64, override: 512, want: 512},
		{name: "override may sit below the floor", questions: 2, override: 128, want: 128},
		{name: "override may exceed the cap", questions: 64, override: 16384, want: 16384},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxTokensFor(tt.questions, tt.override); got != tt.want {
				t.Errorf("maxTokensFor(%d, %d) = %d, want %d", tt.questions, tt.override, got, tt.want)
			}
		})
	}
}

// TestUpstreamBodyBudgetAndThinkingOffByDefault checks the two new body fields on
// the exact bytes the shim sends: a 2-question request gets the 1280-token floor
// (it must still clear the ~625-token inline-thinking tax), and the default body
// switches the chat template's thinking off.
func TestUpstreamBodyBudgetAndThinkingOffByDefault(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m0": 0.7, "need_m1": 0.3}`)
	shim := newShimForTest(t, upstream.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.7, "need_m1": 0.3}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
	if rec.count() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (a parseable reply must not retry)", rec.count())
	}
	call := rec.at(0)
	if call.MaxTokens != 1280 {
		t.Errorf("max_tokens = %d, want 1280 (2 questions: 24*2+1024 = 1072, floored)", call.MaxTokens)
	}
	if len(call.ChatTemplateKwargs) != 1 || call.ChatTemplateKwargs["enable_thinking"] {
		t.Errorf("chat_template_kwargs = %v, want {enable_thinking:false}", call.ChatTemplateKwargs)
	}
	if sent := string(rec.rawAt(0)); !strings.Contains(sent, `"chat_template_kwargs":{"enable_thinking":false}`) {
		t.Errorf("body must carry the thinking switch verbatim:\n%s", sent)
	}
}

// TestUpstreamBudgetScalesWithQuestionCount is the DEFECT 1 regression at the
// handler level: a real 64-question shard reply costs ~900 tokens of JSON plus the
// ~625-token inline-thinking tax, so its budget must clear that instead of the 320
// the old formula allowed.
func TestUpstreamBudgetScalesWithQuestionCount(t *testing.T) {
	tests := []struct {
		questions int
		want      int
	}{
		{questions: 5, want: 1280},  // 24*5+1024 = 1144 -> floor
		{questions: 64, want: 2560}, // the real shard size
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_questions", tt.questions), func(t *testing.T) {
			upstream, rec := newFakeUpstream(t, `{"need_m00": 0.9}`)
			shim := newShimForTest(t, upstream.URL, nil)

			body := mustMarshal(t, pointerRequestFor("q", "openjev-test", memoryIDs(tt.questions)...))
			status, raw := postWire(t, shim.URL, body)
			requireStatus(t, status, http.StatusOK, raw)

			if got := rec.at(0).MaxTokens; got != tt.want {
				t.Errorf("%d questions: max_tokens = %d, want %d", tt.questions, got, tt.want)
			}
			if got := decodeWire(t, raw).Probabilities["need_m00"]; got != 0.9 {
				t.Errorf("%d questions: need_m00 = %v, want 0.9", tt.questions, got)
			}
		})
	}
}

// TestUpstreamBudgetHonorsTheCapForWideShards walks the 8192 cap through the
// handler, so one over-wide request cannot ask the upstream for unbounded output.
func TestUpstreamBudgetHonorsTheCapForWideShards(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m00": 0.5}`)
	shim := newShimForTest(t, upstream.URL, func(c *shimConfig) { c.MaxQuestions = 400 })

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", memoryIDs(400)...))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	if got := rec.at(0).MaxTokens; got != 8192 {
		t.Errorf("max_tokens = %d, want the 8192 cap (24*400+1024 = 10624)", got)
	}
}

// TestOpenJEVThinkingOnOmitsChatTemplateKwargs is the escape hatch: an upstream
// that rejects unknown body keys turns the switch off at the wire level, and the
// pointer protocol still answers normally.
func TestOpenJEVThinkingOnOmitsChatTemplateKwargs(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m0": 0.7, "need_m1": 0.3}`)
	shim := serveShimForTest(t, mustParseConfig(t, map[string]string{
		"OPENJEV_UPSTREAM_BASE_URL": upstream.URL,
		"OPENJEV_UPSTREAM_MODEL":    "openjev-test",
		"OPENJEV_THINKING":          "on",
	}))

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.7, "need_m1": 0.3}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
	if call := rec.at(0); call.ChatTemplateKwargs != nil {
		t.Errorf("OPENJEV_THINKING=on must omit the field, got %v", call.ChatTemplateKwargs)
	}
	if sent := string(rec.rawAt(0)); strings.Contains(sent, "chat_template_kwargs") {
		t.Errorf("OPENJEV_THINKING=on must not send the field:\n%s", sent)
	}
}

// TestOpenJEVMaxCompletionTokensOverrideReachesTheBody: the env override replaces
// the computed budget, so an operator can fit an upstream's context window, or
// shrink a test's cost, without a rebuild.
func TestOpenJEVMaxCompletionTokensOverrideReachesTheBody(t *testing.T) {
	upstream, rec := newFakeUpstream(t, `{"need_m0": 0.7, "need_m1": 0.3}`)
	shim := serveShimForTest(t, mustParseConfig(t, map[string]string{
		"OPENJEV_UPSTREAM_BASE_URL":     upstream.URL,
		"OPENJEV_UPSTREAM_MODEL":        "openjev-test",
		"OPENJEV_MAX_COMPLETION_TOKENS": "3333",
	}))

	body := mustMarshal(t, pointerRequestFor("q", "openjev-test", "m0", "m1"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	want := map[string]float64{"need_m0": 0.7, "need_m1": 0.3}
	if got := decodeWire(t, raw).Probabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("probabilities = %v, want %v", got, want)
	}
	if got := rec.at(0).MaxTokens; got != 3333 {
		t.Errorf("max_tokens = %d, want the OPENJEV_MAX_COMPLETION_TOKENS override 3333", got)
	}
}

// TestParseConfigBudgetAndThinkingEnv pins the env semantics: unset/0 means
// "compute it", a positive number overrides, "on"/"off" toggle the thinking
// switch, and anything else is rejected rather than silently guessed.
func TestParseConfigBudgetAndThinkingEnv(t *testing.T) {
	base := map[string]string{"OPENJEV_UPSTREAM_MODEL": "m"}

	cfg, err := parseConfig(nil, envFrom(base))
	if err != nil {
		t.Fatalf("parseConfig defaults: %v", err)
	}
	if cfg.MaxCompletionTokens != 0 {
		t.Errorf("max completion tokens = %d, want 0 (computed)", cfg.MaxCompletionTokens)
	}
	if cfg.Thinking {
		t.Error("thinking must default to off")
	}

	cases := []struct {
		name       string
		env        map[string]string
		wantTokens int
		wantThink  bool
	}{
		{name: "zero means computed", env: map[string]string{"OPENJEV_MAX_COMPLETION_TOKENS": "0"}},
		{name: "positive override", env: map[string]string{"OPENJEV_MAX_COMPLETION_TOKENS": "4096"}, wantTokens: 4096},
		{name: "thinking on", env: map[string]string{"OPENJEV_THINKING": "on"}, wantThink: true},
		{name: "thinking on is case-insensitive", env: map[string]string{"OPENJEV_THINKING": " ON "}, wantThink: true},
		{name: "thinking off", env: map[string]string{"OPENJEV_THINKING": "off"}, wantThink: false},
		{
			name:       "both explicit",
			env:        map[string]string{"OPENJEV_MAX_COMPLETION_TOKENS": "300", "OPENJEV_THINKING": "on"},
			wantTokens: 300,
			wantThink:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig(nil, envFrom(mergedEnv(base, tc.env)))
			if err != nil {
				t.Fatalf("parseConfig(%v): %v", tc.env, err)
			}
			if cfg.MaxCompletionTokens != tc.wantTokens {
				t.Errorf("max completion tokens = %d, want %d", cfg.MaxCompletionTokens, tc.wantTokens)
			}
			if cfg.Thinking != tc.wantThink {
				t.Errorf("thinking = %v, want %v", cfg.Thinking, tc.wantThink)
			}
		})
	}

	invalid := []map[string]string{
		{"OPENJEV_MAX_COMPLETION_TOKENS": "abc"},
		{"OPENJEV_MAX_COMPLETION_TOKENS": "-4"},
		{"OPENJEV_MAX_COMPLETION_TOKENS": "1.5"},
		{"OPENJEV_THINKING": "maybe"},
		{"OPENJEV_THINKING": "true"},
	}
	for _, env := range invalid {
		_, err := parseConfig(nil, envFrom(mergedEnv(base, env)))
		if err == nil {
			t.Errorf("env %v must be rejected", env)
			continue
		}
		if !strings.Contains(err.Error(), "OPENJEV_") {
			t.Errorf("env %v: err = %v, want a named env error", env, err)
		}
	}
}
