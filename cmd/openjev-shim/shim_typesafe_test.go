package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/filter/jev"
)

// --- wire mirrors: the TypeSafe "systemone" evaluation-model protocol --------
//
// These mirrors are the test's independent copy of the systemone shape, so a
// rename on either side of the translation fails here instead of silently
// degrading a paid eval run.

type typesafeWireRequest struct {
	Model     string                          `json:"model"`
	State     typesafeWireState               `json:"state"`
	Questions map[string]typesafeWireQuestion `json:"questions"`
}

type typesafeWireState struct {
	Descriptor string                        `json:"descriptor"`
	Query      string                        `json:"query"`
	Memories   map[string]typesafeWireMemory `json:"memories"`
}

type typesafeWireMemory struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type typesafeWireQuestion struct {
	Type         string                   `json:"type"`
	Criteria     typesafeWireCriteria     `json:"criteria"`
	Instructions typesafeWireInstructions `json:"instructions"`
}

type typesafeWireCriteria struct {
	Yes string `json:"yes"`
	No  string `json:"no"`
}

type typesafeWireInstructions struct {
	Goal  string   `json:"goal"`
	Rules []string `json:"rules"`
}

// --- fake typesafe endpoint -------------------------------------------------

type fakeReply struct {
	status int
	body   string
}

type typesafeRecorder struct {
	mu      sync.Mutex
	raws    [][]byte
	headers []http.Header
	paths   []string
	at      []time.Time
}

func (r *typesafeRecorder) record(raw []byte, header http.Header, path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raws = append(r.raws, raw)
	r.headers = append(r.headers, header)
	r.paths = append(r.paths, path)
	r.at = append(r.at, time.Now())
	return len(r.raws) - 1
}

func (r *typesafeRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.raws)
}

func (r *typesafeRecorder) rawAt(i int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.raws[i]
}

func (r *typesafeRecorder) headerAt(i int) http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headers[i]
}

func (r *typesafeRecorder) pathAt(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paths[i]
}

func (r *typesafeRecorder) atTime(i int) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at[i]
}

// newFakeTypesafe serves the scripted replies in order (the last one repeats) and
// records every request body, header set and path.
func newFakeTypesafe(t *testing.T, replies ...fakeReply) (*httptest.Server, *typesafeRecorder) {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("newFakeTypesafe needs at least one reply")
	}
	rec := &typesafeRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := readAllLimited(r, 1<<20)
		if err != nil {
			t.Errorf("read typesafe request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply := replies[min(rec.record(raw, r.Header.Clone(), r.URL.Path), len(replies)-1)]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func readAllLimited(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close() //nolint:errcheck
	return io.ReadAll(io.LimitReader(r.Body, limit))
}

// typesafeAnswers renders an answers object for the given id -> "yes" probability
// pairs; an id mapped to nil carries no probabilities at all (the missing-"yes"
// case).
func typesafeAnswers(entries map[string]*float64) map[string]any {
	answers := make(map[string]any, len(entries))
	for id, yes := range entries {
		answer := map[string]any{"type": "choice", "choice": "yes"}
		if yes != nil {
			answer["probabilities"] = map[string]float64{"yes": *yes, "no": 1 - *yes}
		}
		answers[id] = answer
	}
	return answers
}

func float64Ptr(v float64) *float64 { return &v }

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// typesafeReply builds a systemone reply with the gateway's camelCase usage.
func typesafeReply(t *testing.T, inputTokens, outputTokens int, answers map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"answers":  answers,
		"usage":    map[string]int{"inputTokens": inputTokens, "outputTokens": outputTokens},
		"rounding": map[string]int{"probabilityDecimals": 2},
	})
}

func newShimForTypesafe(t *testing.T, endpoint string, mutate func(*shimConfig)) *httptest.Server {
	t.Helper()
	cfg := shimConfig{
		UpstreamMode:     upstreamModeTypesafe,
		TypesafeEndpoint: endpoint,
		TypesafeModel:    defaultTypesafeModel,
		TypesafeAPIKey:   "test-typesafe-key",
		MaxQuestions:     defaultMaxQuestions,
		UpstreamTimeout:  5 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return serveShimForTest(t, cfg)
}

// --- (a) translation: ids preserved, state carries query + memories ---------

func TestTypesafeTranslationPreservesNeedIDsAndState(t *testing.T) {
	reply := typesafeReply(t, 333, 32, typesafeAnswers(map[string]*float64{
		"need_m1": float64Ptr(0.93),
	}))
	endpoint, rec := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: reply})
	shim := newShimForTypesafe(t, endpoint.URL+"/v4/ai/evaluation-model", nil)

	// The pointer request's model is the client's label; the body must carry the
	// configured typesafe model instead.
	body := mustMarshal(t, pointerRequestFor("what is the deploy code?", "ENGINE-LABEL", "m1", "m2"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	if rec.count() != 1 {
		t.Fatalf("typesafe calls = %d, want 1", rec.count())
	}
	if got, want := rec.pathAt(0), "/v4/ai/evaluation-model"; got != want {
		t.Errorf("request path = %q, want the endpoint URL verbatim %q", got, want)
	}

	var sent typesafeWireRequest
	if err := json.Unmarshal(rec.rawAt(0), &sent); err != nil {
		t.Fatalf("decode typesafe body %s: %v", rec.rawAt(0), err)
	}
	if sent.Model != defaultTypesafeModel {
		t.Errorf("model = %q, want the configured typesafe model %q", sent.Model, defaultTypesafeModel)
	}
	if sent.State.Descriptor != "engram read-side relevance filter" {
		t.Errorf("state.descriptor = %q", sent.State.Descriptor)
	}
	if sent.State.Query != "what is the deploy code?" {
		t.Errorf("state.query = %q, want the pointer query verbatim", sent.State.Query)
	}
	if len(sent.State.Memories) != 2 {
		t.Fatalf("state.memories = %v, want the pointer memories as given", sent.State.Memories)
	}
	if got := sent.State.Memories["m1"]; got.Name != "memory-m1" || got.Text != "body-m1" {
		t.Errorf("state.memories[m1] = %+v, want the pointer memory as given", got)
	}

	if len(sent.Questions) != 2 {
		t.Fatalf("questions = %v, want one per pointer question key", sent.Questions)
	}
	if _, present := sent.Questions["m1"]; present {
		t.Error("questions must be keyed by the pointer question ids, not the memory ids")
	}
	for _, id := range []string{"need_m1", "need_m2"} {
		question, ok := sent.Questions[id]
		if !ok {
			t.Fatalf("questions is missing the pointer id %q: %v", id, sent.Questions)
		}
		if question.Type != "choice" {
			t.Errorf("%s: type = %q, want choice", id, question.Type)
		}
		if question.Criteria.Yes != "this memory is needed to answer the query" {
			t.Errorf("%s: criteria.yes = %q", id, question.Criteria.Yes)
		}
		if question.Criteria.No != "this memory is not needed" {
			t.Errorf("%s: criteria.no = %q", id, question.Criteria.No)
		}
		if question.Instructions.Goal != "decide whether the memory is needed for the query" {
			t.Errorf("%s: instructions.goal = %q", id, question.Instructions.Goal)
		}
		want := []string{"judge strictly by relevance to answering the query"}
		if !reflect.DeepEqual(question.Instructions.Rules, want) {
			t.Errorf("%s: instructions.rules = %v, want %v", id, question.Instructions.Rules, want)
		}
	}

	// The pointer protocol's point: the memories travel once, not once per question.
	if got := strings.Count(string(rec.rawAt(0)), "body-m1"); got != 1 {
		t.Errorf("the request body carries the memory text %d times, want 1 (shared state)", got)
	}

	out := decodeWire(t, raw)
	want := map[string]float64{"need_m1": 0.93, "need_m2": 0}
	if !reflect.DeepEqual(out.Probabilities, want) {
		t.Errorf("probabilities = %v, want %v", out.Probabilities, want)
	}
	if out.Usage == nil || out.Usage.PromptTokens != 333 || out.Usage.CompletionTokens != 32 {
		t.Errorf("usage = %+v, want prompt_tokens=333 completion_tokens=32 (from inputTokens/outputTokens)", out.Usage)
	}
}

// --- (b) response mapping: missing "yes" -> 0, clamps, usage ----------------

func TestTypesafeResponseMappingClampsAndDefaultsMissingYes(t *testing.T) {
	// need_m0 in range; need_m1 carries no probabilities at all; need_m2 is
	// over-range; need_m3 is negative; need_m4 is absent from answers entirely.
	reply := `{"answers":{` +
		`"need_m0":{"type":"choice","choice":"yes","probabilities":{"yes":0.87,"no":0.13}},` +
		`"need_m1":{"type":"choice","choice":"yes"},` +
		`"need_m2":{"type":"choice","choice":"yes","probabilities":{"yes":1.4,"no":0}},` +
		`"need_m3":{"type":"choice","choice":"no","probabilities":{"yes":-0.2,"no":1}}` +
		`},"usage":{"inputTokens":10,"outputTokens":4},"rounding":{"probabilityDecimals":2}}`
	endpoint, _ := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: reply})
	shim := newShimForTypesafe(t, endpoint.URL, nil)

	body := mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0", "m1", "m2", "m3", "m4"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusOK, raw)

	out := decodeWire(t, raw)
	want := map[string]float64{
		"need_m0": 0.87, // passed through
		"need_m1": 0,    // answer present, no "yes" -> drop
		"need_m2": 1,    // replied 1.4 -> clamped down to 1
		"need_m3": 0,    // replied -0.2 -> clamped up to 0
		"need_m4": 0,    // no answer at all -> drop
	}
	if !reflect.DeepEqual(out.Probabilities, want) {
		t.Errorf("probabilities = %v, want %v", out.Probabilities, want)
	}
	if out.Usage == nil || out.Usage.PromptTokens != 10 || out.Usage.CompletionTokens != 4 {
		t.Errorf("usage = %+v, want 10/4", out.Usage)
	}

	// A reply without usage stays "unknown" rather than being reported as zero.
	noUsage, _ := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: mustJSON(t, map[string]any{
		"answers": typesafeAnswers(map[string]*float64{"need_m0": float64Ptr(0.5)}),
	})})
	bare := newShimForTypesafe(t, noUsage.URL, nil)
	status, raw = postWire(t, bare.URL, body)
	requireStatus(t, status, http.StatusOK, raw)
	if got := decodeWire(t, raw).Usage; got != nil {
		t.Errorf("usage = %+v, want omitted when the endpoint reported none", got)
	}
}

// TestEngineClientRoundTripAgainstTypesafeUpstream drives the shim in typesafe
// mode with the real filter/jev client: no mirror types are involved on the
// response side, so this is the exact field-name contract the engine reads.
func TestEngineClientRoundTripAgainstTypesafeUpstream(t *testing.T) {
	reply := typesafeReply(t, 333, 32, typesafeAnswers(map[string]*float64{
		"need_m0": float64Ptr(0.9),
		"need_m1": float64Ptr(0.2),
		"need_m2": float64Ptr(0.9),
	}))
	endpoint, _ := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: reply})
	shim := newShimForTypesafe(t, endpoint.URL, nil)

	client, err := jev.New(jev.Config{
		BaseURL:           shim.URL,
		Model:             defaultTypesafeModel,
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
		t.Fatalf("filter through the typesafe shim: %v (meta %+v)", err, meta)
	}
	if want := []float64{0.9, 0.2, 0.9}; !reflect.DeepEqual(probs, want) {
		t.Errorf("probabilities = %v, want %v", probs, want)
	}
	if meta.Degraded {
		t.Errorf("a successful typesafe call must not degrade: notes=%v", meta.Notes)
	}
	if meta.InputTokens != 333 || meta.OutputTokens != 32 {
		t.Errorf("token accounting = in:%d out:%d, want the gateway usage in:333 out:32", meta.InputTokens, meta.OutputTokens)
	}
}

// --- (c) gateway headers ---------------------------------------------------

func TestTypesafeHeadersForGatewayAndOtherHosts(t *testing.T) {
	gateway := http.Header{}
	applyTypesafeHeaders(gateway, defaultTypesafeEndpoint, defaultTypesafeModel, "secret-key")
	if got := gateway.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want the bearer key", got)
	}
	if got := gateway.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	for header, want := range map[string]string{
		"ai-gateway-protocol-version":               "0.0.1",
		"ai-evaluation-model-specification-version": "4",
		"ai-model-id": defaultTypesafeModel,
	} {
		if got := gateway.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	// A self-hosted systemone endpoint must not receive the gateway headers.
	other := http.Header{}
	applyTypesafeHeaders(other, "http://127.0.0.1:8000/v4/ai/evaluation-model", defaultTypesafeModel, "secret-key")
	if got := other.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("non-gateway Authorization = %q", got)
	}
	for _, header := range []string{
		"ai-gateway-protocol-version",
		"ai-evaluation-model-specification-version",
		"ai-model-id",
	} {
		if got := other.Get(header); got != "" {
			t.Errorf("non-gateway host must not send %s, got %q", header, got)
		}
	}
}

// TestTypesafeRequestHeadersOnTheWire pins the headers the fake endpoint really
// received: the bearer key, JSON content type, and no gateway headers for a
// loopback endpoint.
func TestTypesafeRequestHeadersOnTheWire(t *testing.T) {
	endpoint, rec := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: typesafeReply(t, 1, 1,
		typesafeAnswers(map[string]*float64{"need_m0": float64Ptr(0.5)}))})
	shim := newShimForTypesafe(t, endpoint.URL, nil)

	status, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
	requireStatus(t, status, http.StatusOK, raw)

	header := rec.headerAt(0)
	if got := header.Get("Authorization"); got != "Bearer test-typesafe-key" {
		t.Errorf("Authorization = %q, want the configured key", got)
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := header.Get("ai-model-id"); got != "" {
		t.Errorf("a loopback endpoint must not receive the gateway headers, got ai-model-id=%q", got)
	}
}

// --- (d) retry / failure ---------------------------------------------------

func TestTypesafeRetriesRetryableStatusesThenSucceeds(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			endpoint, rec := newFakeTypesafe(t,
				fakeReply{status: status, body: `{"error":{"message":"gateway is busy"}}`},
				fakeReply{status: http.StatusOK, body: typesafeReply(t, 12, 3,
					typesafeAnswers(map[string]*float64{"need_m0": float64Ptr(0.8)}))},
			)
			shim := newShimForTypesafe(t, endpoint.URL, nil)

			status_, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
			requireStatus(t, status_, http.StatusOK, raw)
			if rec.count() != 2 {
				t.Fatalf("typesafe calls = %d, want 2 (retry once after %d)", rec.count(), status)
			}
			if got := decodeWire(t, raw).Probabilities["need_m0"]; got != 0.8 {
				t.Errorf("need_m0 = %v, want 0.8", got)
			}
			// The retry waits ~0.5s: the backoff is real, not a hot loop.
			if gap := rec.atTime(1).Sub(rec.atTime(0)); gap < 400*time.Millisecond || gap > 2*time.Second {
				t.Errorf("backoff before the first retry = %s, want ~0.5s", gap)
			}
		})
	}
}

func TestTypesafeRetryExhaustionReturns502WithTheStatus(t *testing.T) {
	endpoint, rec := newFakeTypesafe(t,
		fakeReply{status: http.StatusTooManyRequests, body: `{"error":{"message":"rate limited"}}`})
	shim := newShimForTypesafe(t, endpoint.URL, nil)

	status, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
	requireStatus(t, status, http.StatusBadGateway, raw)

	if rec.count() != 3 {
		t.Fatalf("typesafe calls = %d, want 3 (the initial call plus two retries)", rec.count())
	}
	if gap := rec.atTime(2).Sub(rec.atTime(1)); gap < 900*time.Millisecond {
		t.Errorf("second backoff = %s, want ~1.0s", gap)
	}
	out := decodeWire(t, raw)
	if out.Error == nil || !strings.Contains(out.Error.Message, "429") {
		t.Errorf("502 message = %+v, want the upstream status named", out.Error)
	}
	if out.Error != nil && !strings.Contains(out.Error.Message, "rate limited") {
		t.Errorf("502 message = %q, want the upstream message surfaced", out.Error.Message)
	}
	if strings.Contains(string(raw), "test-typesafe-key") {
		t.Errorf("the 502 body must never carry the API key: %s", raw)
	}
}

func TestTypesafeNonRetryableStatusReturns502WithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			endpoint, rec := newFakeTypesafe(t, fakeReply{status: status, body: `{"error":{"message":"nope"}}`})
			shim := newShimForTypesafe(t, endpoint.URL, nil)

			got, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
			requireStatus(t, got, http.StatusBadGateway, raw)
			if rec.count() != 1 {
				t.Fatalf("typesafe calls = %d, want 1 (status %d is terminal)", rec.count(), status)
			}
			out := decodeWire(t, raw)
			if out.Error == nil || !strings.Contains(out.Error.Message, strconv.Itoa(status)) {
				t.Errorf("502 message = %+v, want the upstream status %d named", out.Error, status)
			}
			if strings.Contains(string(raw), "test-typesafe-key") {
				t.Errorf("the 502 body must never carry the API key: %s", raw)
			}
		})
	}
}

// TestTypesafeBackoffIsBoundedByTheUpstreamDeadline pins the bound: a backoff must
// never outlive the call deadline the client's 30s budget sits inside.
func TestTypesafeBackoffIsBoundedByTheUpstreamDeadline(t *testing.T) {
	endpoint, rec := newFakeTypesafe(t, fakeReply{status: http.StatusTooManyRequests, body: `{"error":{"message":"rate limited"}}`})
	shim := newShimForTypesafe(t, endpoint.URL, func(c *shimConfig) { c.UpstreamTimeout = 100 * time.Millisecond })

	start := time.Now()
	status, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
	elapsed := time.Since(start)

	requireStatus(t, status, http.StatusBadGateway, raw)
	if elapsed > time.Second {
		t.Fatalf("call took %s, want the 100ms deadline to cut the 0.5s backoff", elapsed)
	}
	if rec.count() != 1 {
		t.Errorf("typesafe calls = %d, want 1 (the deadline ends the retry budget)", rec.count())
	}
}

func TestTypesafeUnparseableReplyReturns502(t *testing.T) {
	endpoint, rec := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: "not json at all"})
	shim := newShimForTypesafe(t, endpoint.URL, nil)

	status, raw := postWire(t, shim.URL, mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0")))
	requireStatus(t, status, http.StatusBadGateway, raw)
	if rec.count() != 1 {
		t.Errorf("typesafe calls = %d, want 1 (a bad body is not retryable)", rec.count())
	}
}

func TestTypesafeTooManyQuestionsReturns413WithoutUpstreamCall(t *testing.T) {
	endpoint, rec := newFakeTypesafe(t, fakeReply{status: http.StatusOK, body: typesafeReply(t, 1, 1, nil)})
	shim := newShimForTypesafe(t, endpoint.URL, func(c *shimConfig) { c.MaxQuestions = 3 })

	body := mustMarshal(t, pointerRequestFor("q", "ENGINE-LABEL", "m0", "m1", "m2", "m3"))
	status, raw := postWire(t, shim.URL, body)
	requireStatus(t, status, http.StatusRequestEntityTooLarge, raw)
	if rec.count() != 0 {
		t.Fatalf("typesafe calls = %d, want 0 (the over-bound guard is upstream-mode independent)", rec.count())
	}
}

// --- (e) configuration -----------------------------------------------------

func TestParseConfigUpstreamModeEnv(t *testing.T) {
	// Default stays chat: byte-identical behavior for every existing run.
	cfg := mustParseConfig(t, map[string]string{"OPENJEV_UPSTREAM_MODEL": "m"})
	if cfg.UpstreamMode != upstreamModeChat {
		t.Errorf("upstream mode = %q, want the chat default", cfg.UpstreamMode)
	}

	// typesafe needs only the key: model and endpoint have documented defaults.
	cfg = mustParseConfig(t, map[string]string{
		"OPENJEV_UPSTREAM":          "typesafe",
		"OPENJEV_TYPESAFE_API_KEY":  "secret-key",
		"OPENJEV_UPSTREAM_BASE_URL": "http://127.0.0.1:8000/v1",
	})
	if cfg.UpstreamMode != upstreamModeTypesafe {
		t.Errorf("upstream mode = %q, want typesafe", cfg.UpstreamMode)
	}
	if cfg.TypesafeEndpoint != defaultTypesafeEndpoint {
		t.Errorf("endpoint = %q, want %q", cfg.TypesafeEndpoint, defaultTypesafeEndpoint)
	}
	if cfg.TypesafeModel != defaultTypesafeModel {
		t.Errorf("typesafe model = %q, want %q", cfg.TypesafeModel, defaultTypesafeModel)
	}
	if cfg.TypesafeAPIKey != "secret-key" {
		t.Errorf("typesafe key is not carried through: %q", cfg.TypesafeAPIKey)
	}

	// Explicit overrides win, and the mode value is case/space tolerant.
	cfg = mustParseConfig(t, map[string]string{
		"OPENJEV_UPSTREAM":          " TypeSafe ",
		"OPENJEV_TYPESAFE_API_KEY":  "k",
		"OPENJEV_TYPESAFE_ENDPOINT": "http://127.0.0.1:9999/v4/ai/evaluation-model",
		"OPENJEV_TYPESAFE_MODEL":    "typesafe-ai/jev-pinned",
	})
	if cfg.UpstreamMode != upstreamModeTypesafe {
		t.Errorf("upstream mode = %q, want typesafe", cfg.UpstreamMode)
	}
	if cfg.TypesafeEndpoint != "http://127.0.0.1:9999/v4/ai/evaluation-model" {
		t.Errorf("endpoint = %q", cfg.TypesafeEndpoint)
	}
	if cfg.TypesafeModel != "typesafe-ai/jev-pinned" {
		t.Errorf("typesafe model = %q", cfg.TypesafeModel)
	}
}

func TestParseConfigUpstreamModeFailClosed(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		wantName string
	}{
		{
			name:     "typesafe without the key",
			env:      map[string]string{"OPENJEV_UPSTREAM": "typesafe"},
			wantName: "OPENJEV_TYPESAFE_API_KEY",
		},
		{
			name: "typesafe with a blank key",
			env: map[string]string{
				"OPENJEV_UPSTREAM":         "typesafe",
				"OPENJEV_TYPESAFE_API_KEY": "   ",
			},
			wantName: "OPENJEV_TYPESAFE_API_KEY",
		},
		{
			name: "unknown mode",
			env: map[string]string{
				"OPENJEV_UPSTREAM":       "systemone",
				"OPENJEV_UPSTREAM_MODEL": "m",
			},
			wantName: "OPENJEV_UPSTREAM",
		},
		{
			name:     "chat still requires a pinned model",
			env:      map[string]string{"OPENJEV_UPSTREAM": "chat"},
			wantName: "OPENJEV_UPSTREAM_MODEL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(nil, envFrom(tc.env))
			if err == nil {
				t.Fatalf("env %v must be rejected", tc.env)
			}
			if !strings.Contains(err.Error(), tc.wantName) {
				t.Errorf("err = %v, want the env var %s named", err, tc.wantName)
			}
		})
	}

	// newShimServer is the second fail-closed entry point: a hand-built typesafe
	// config without a key must not start either.
	if _, err := newShimServer(shimConfig{UpstreamMode: upstreamModeTypesafe}); err == nil ||
		!strings.Contains(err.Error(), "OPENJEV_TYPESAFE_API_KEY") {
		t.Errorf("newShimServer: err = %v, want the missing key named", err)
	}
	if _, err := newShimServer(shimConfig{
		UpstreamMode:     upstreamModeTypesafe,
		TypesafeEndpoint: "127.0.0.1:9999",
		TypesafeAPIKey:   "k",
	}); err == nil || !strings.Contains(err.Error(), "OPENJEV_TYPESAFE_ENDPOINT") {
		t.Errorf("newShimServer: err = %v, want the bad endpoint named", err)
	}
}

// TestStartupLogNamesTheUpstreamWithoutTheKey pins the startup line's content:
// the effective upstream is visible for an operator, the credential is not.
func TestStartupLogNamesTheUpstreamWithoutTheKey(t *testing.T) {
	var logbuf bytes.Buffer
	srv, err := newShimServer(shimConfig{
		UpstreamMode:     upstreamModeTypesafe,
		TypesafeEndpoint: defaultTypesafeEndpoint,
		TypesafeModel:    defaultTypesafeModel,
		TypesafeAPIKey:   "super-secret-gateway-key",
		Logger:           log.New(&logbuf, "", 0),
	})
	if err != nil {
		t.Fatalf("newShimServer: %v", err)
	}
	srv.logStartup("127.0.0.1:8020")

	line := logbuf.String()
	for _, needle := range []string{"upstream=typesafe", defaultTypesafeEndpoint, defaultTypesafeModel, "max_questions="} {
		if !strings.Contains(line, needle) {
			t.Errorf("startup line is missing %q:\n%s", needle, line)
		}
	}
	if strings.Contains(line, "super-secret-gateway-key") {
		t.Errorf("the startup line must never carry the API key:\n%s", line)
	}

	logbuf.Reset()
	chat, err := newShimServer(shimConfig{UpstreamModel: "m", Logger: log.New(&logbuf, "", 0)})
	if err != nil {
		t.Fatalf("newShimServer chat: %v", err)
	}
	chat.logStartup("127.0.0.1:8020")
	if line := logbuf.String(); !strings.Contains(line, "upstream=chat") {
		t.Errorf("chat startup line = %q, want the upstream mode named", line)
	}
}
