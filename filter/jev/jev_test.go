package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/filter"
)

func testCandidates(n int) []filter.Candidate {
	cands := make([]filter.Candidate, n)
	for i := range cands {
		cands[i] = filter.Candidate{
			ID:    fmt.Sprintf("m%d", i),
			Name:  fmt.Sprintf("memory-%d", i),
			Text:  fmt.Sprintf("body-%d", i),
			Score: float64(n - i),
		}
	}
	return cands
}

func decodePointer(t *testing.T, r *http.Request) (pointerRequest, []string) {
	t.Helper()
	var req pointerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("decode pointer request: %v", err)
		return req, nil
	}
	keys := make([]string, 0, len(req.Questions))
	for k := range req.Questions {
		keys = append(keys, k)
	}
	return req, keys
}

func respondProbabilities(w http.ResponseWriter, keys []string, p float64) {
	probs := map[string]float64{}
	for _, k := range keys {
		probs[k] = p
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"probabilities": probs})
}

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil || c == nil {
		t.Fatalf("new client: %v %v", c, err)
	}
	return c
}

func TestNewDisabledReturnsTypedNil(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{BaseURL: "http://x", Model: "m"},
		{BaseURL: "http://x", APIKey: "k"},
		{Model: "m", APIKey: "k"},
		{BaseURL: "http://x", Model: "m", APIKey: "  "},
	} {
		c, err := New(cfg)
		if err != nil || c != nil {
			t.Fatalf("expected (nil, nil) for %+v, got (%v, %v)", cfg, c, err)
		}
	}
}

func TestFilterRequestShapeAndAlignment(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		req, keys := decodePointer(t, r)
		if req.Model != "jev-pinned-1" {
			t.Errorf("model not pinned in request: %q", req.Model)
		}
		if req.State.Query != "how do I install pnpm?" {
			t.Errorf("state.query missing: %q", req.State.Query)
		}
		if len(req.State.Memories) != 2 {
			t.Errorf("expected 2 pointer memories, got %d", len(req.State.Memories))
		}
		if m, ok := req.State.Memories["m0"]; !ok || m.Name != "memory-0" || m.Text != "body-0" {
			t.Errorf("unexpected memory payload: %+v", req.State.Memories)
		}
		for _, k := range keys {
			q := req.Questions[k]
			if q.Type != "noul" {
				t.Errorf("question %s must be noul, got %q", k, q.Type)
			}
			if !strings.Contains(q.Instructions, "necessary to answer or act on the query") {
				t.Errorf("question %s must be conditional on the query, got %q", k, q.Instructions)
			}
			if !strings.Contains(q.Instructions, "how do I install pnpm?") {
				t.Errorf("question %s must restate the query, got %q", k, q.Instructions)
			}
		}
		respondProbabilities(w, keys, 0.7)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "jev-pinned-1", APIKey: "k"})
	probs, meta, err := c.Filter(context.Background(), "how do I install pnpm?", testCandidates(2))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(probs) != 2 || probs[0] != 0.7 || probs[1] != 0.7 {
		t.Fatalf("probabilities not aligned with candidates: %v", probs)
	}
	if got, want := gotPath, "/answers"; got != want {
		t.Fatalf("expected default endpoint %s, got %s", want, got)
	}
	if meta.Backend != filter.BackendJev || meta.Degraded || meta.Theta != 0.5 {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestFilterProbabilityIndexesByQuestionKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		probs := map[string]float64{}
		for i, k := range keys {
			probs[k] = float64(i) / 10
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"probabilities": probs})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	probs, _, err := c.Filter(context.Background(), "q", testCandidates(3))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(probs) != 3 {
		t.Fatalf("expected 3 probabilities, got %v", probs)
	}
	// Responses are matched by volume, not by arrival order, so encoding must be
	// per-key: m0 and m2 must differ here.
	if probs[0] == probs[2] {
		t.Fatalf("probabilities were not mapped back by key: %v", probs)
	}
}

func TestFilterMissingProbabilityIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"probabilities": map[string]float64{keys[0]: 0.9}})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	probs, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err == nil {
		t.Fatal("expected an error for a missing probability")
	}
	if probs != nil || !meta.Degraded {
		t.Fatalf("no partial adoption allowed: probs=%v meta=%+v", probs, meta)
	}
}

func TestFilterShardsConcurrentlyAboveThreshold(t *testing.T) {
	const shards = 4 // 150 candidates at 48/shard
	// The barrier test is the one wall-clock-sensitive case in this file: it needs
	// a deadline far longer than any plausible scheduling delay, so a slow box
	// cannot turn jitter into a failure. The whole-call deadline is strictly more
	// generous than the guard, so a serializing client trips the guard's
	// diagnostic rather than a bare deadline error.
	const shardBarrierGuard = 20 * time.Second
	// shardBarrierDeadline is the client's whole-call deadline: strictly more
	// generous than the guard, so a serializing client is reported as a
	// serialization instead of as a bare timeout.
	const shardBarrierDeadline = 30 * time.Second
	var requests int32
	var arrived int32
	release := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		atomic.AddInt32(&requests, 1)
		if n := atomic.AddInt32(&arrived, 1); int(n) == shards {
			once.Do(func() { close(release) })
		}
		// The client's whole-call deadline outlasts this guard, so a serializing
		// client trips the guard diagnostic below instead of a bare deadline error.
		// Were the two inverted, a scheduling hiccup would trip the whole-call
		// deadline first and mask the diagnostic.
		select {
		case <-release:
		case <-time.After(shardBarrierGuard):
			t.Errorf("only %d of %d shards were in flight: the client serialized them",
				atomic.LoadInt32(&arrived), shards)
		}
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", ShardSize: 48, Deadline: shardBarrierDeadline})
	probs, meta, err := c.Filter(context.Background(), "q", testCandidates(150))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(probs) != 150 {
		t.Fatalf("expected 150 aligned probabilities, got %d", len(probs))
	}
	for i, p := range probs {
		if p != 0.9 {
			t.Fatalf("probability %d lost in the shard merge: %v", i, p)
		}
	}
	if got := atomic.LoadInt32(&requests); got != shards {
		t.Fatalf("expected %d shard requests, got %d", shards, got)
	}
	if meta.Degraded {
		t.Fatalf("unexpected degradation: %+v", meta)
	}
}

func TestFilterShardPayloadStaysWithinBounds(t *testing.T) {
	var maxMemories int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, keys := decodePointer(t, r)
		if n := int32(len(req.State.Memories)); n > atomic.LoadInt32(&maxMemories) {
			atomic.StoreInt32(&maxMemories, n)
		}
		respondProbabilities(w, keys, 0.6)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", ShardSize: 200})
	// ShardSize is clamped into the documented window; 200 coerces down to the ceiling.
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(150)); err != nil {
		t.Fatalf("filter: %v", err)
	}
	if got := atomic.LoadInt32(&maxMemories); got > maxShardSize || got < minShardSize {
		t.Fatalf("shard size outside the %d..%d contract window: %d", minShardSize, maxShardSize, got)
	}
}

// TestShardCandidatesHonorsAConfiguredSizeBelowTheOldFloor pins the shard shape
// directly: the eval client configures small shards because the hosted gateway
// rejects the large pointer requests, so the pool must split at the configured
// size instead of at a floor the engine used to impose.
func TestShardCandidatesHonorsAConfiguredSizeBelowTheOldFloor(t *testing.T) {
	shards := shardCandidates(testCandidates(25), 16, 6)
	if len(shards) != 5 {
		t.Fatalf("shards = %d, want 5 (25 candidates at 6 per shard)", len(shards))
	}
	var merged int
	for i, shard := range shards {
		want := 6
		if i == len(shards)-1 {
			want = 1
		}
		if len(shard) != want {
			t.Errorf("shard %d = %d candidates, want %d", i, len(shard), want)
		}
		for j, ref := range shard {
			if ref.idx != merged+j {
				t.Errorf("shard %d ref %d has idx %d, want %d", i, j, ref.idx, merged+j)
			}
		}
		merged += len(shard)
	}
	if merged != 25 {
		t.Errorf("merged candidates = %d, want 25", merged)
	}
}

// TestFilterRespectsAConfiguredShardSizeBelowTheOldFloor drives the same shape
// through the client: 150 candidates above the default threshold must arrive as
// 25 six-candidate requests, each carrying six memories in the shared state, and
// every probability must survive the merge.
func TestFilterRespectsAConfiguredShardSizeBelowTheOldFloor(t *testing.T) {
	var requests int32
	var maxMemories int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, keys := decodePointer(t, r)
		atomic.AddInt32(&requests, 1)
		if n := int32(len(req.State.Memories)); n > atomic.LoadInt32(&maxMemories) {
			atomic.StoreInt32(&maxMemories, n)
		}
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", ShardSize: 6})
	probs, meta, err := c.Filter(context.Background(), "q", testCandidates(150))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 25 {
		t.Fatalf("requests = %d, want 25 (150 candidates at 6 per shard)", got)
	}
	if got := atomic.LoadInt32(&maxMemories); got != 6 {
		t.Fatalf("largest shard = %d memories, want the configured 6", got)
	}
	if len(probs) != 150 {
		t.Fatalf("aligned probabilities = %d, want 150", len(probs))
	}
	for i, p := range probs {
		if p != 0.9 {
			t.Fatalf("probability %d lost in the shard merge: %v", i, p)
		}
	}
	if meta.Degraded {
		t.Fatalf("unexpected degradation: %+v", meta)
	}
}

// TestNewClampsShardSizeIntoTheDocumentedWindow pins the clamp: values below the
// floor coerce up, values above the ceiling coerce down, and an unset size keeps
// the production default of 48 untouched.
func TestNewClampsShardSizeIntoTheDocumentedWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int
		want int
	}{
		{"unset keeps the production default", 0, defaultShardSize},
		{"below the floor coerces up", 1, minShardSize},
		{"the floor itself is respected", minShardSize, minShardSize},
		{"the eval size is respected", 12, 12},
		{"the ceiling itself is respected", maxShardSize, maxShardSize},
		{"above the ceiling coerces down", 1024, maxShardSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustClient(t, Config{BaseURL: "http://127.0.0.1:1", Model: "m", APIKey: "k", ShardSize: tc.in})
			if c.cfg.ShardSize != tc.want {
				t.Errorf("ShardSize = %d, want %d", c.cfg.ShardSize, tc.want)
			}
		})
	}
}

// TestFilterDefaultConfigStillShardsAt48 pins the production shape end-to-end: an
// unset ShardSize still splits 150 candidates into 48-candidate requests, so the
// eval-side clamp widening cannot quietly move the shipped default.
func TestFilterDefaultConfigStillShardsAt48(t *testing.T) {
	var requests int32
	var maxMemories int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, keys := decodePointer(t, r)
		atomic.AddInt32(&requests, 1)
		if n := int32(len(req.State.Memories)); n > atomic.LoadInt32(&maxMemories) {
			atomic.StoreInt32(&maxMemories, n)
		}
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(150)); err != nil {
		t.Fatalf("filter: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 4 {
		t.Fatalf("requests = %d, want 4 (150 candidates at the default 48 per shard)", got)
	}
	if got := atomic.LoadInt32(&maxMemories); got != 48 {
		t.Fatalf("largest shard = %d memories, want the default 48", got)
	}
}

func TestFilterShardFailureDegradesWholeCall(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&requests, 1) == 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, keys := decodePointer(t, r)
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", ShardSize: 48})
	probs, meta, err := c.Filter(context.Background(), "q", testCandidates(150))
	if err == nil {
		t.Fatal("expected an error when a shard fails")
	}
	if probs != nil || !meta.Degraded {
		t.Fatalf("partial adoption is forbidden: probs=%v meta=%+v", probs, meta)
	}
}

func TestFilterDoesNotRetry(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(2)); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("retries break degrade-arm determinism: got %d requests", got)
	}
}

func TestFilterNegativeCacheSkipsEndpointUntilTTL(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", NegativeCacheTTL: 30 * time.Second})
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(2)); err == nil {
		t.Fatal("expected the first call to fail")
	}
	first := atomic.LoadInt32(&requests)

	_, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err == nil || !meta.Degraded {
		t.Fatalf("expected a cached degrade, got err=%v meta=%+v", err, meta)
	}
	if got := atomic.LoadInt32(&requests); got != first {
		t.Fatalf("negative cache must skip the network: %d -> %d requests", first, got)
	}
	if !strings.Contains(strings.Join(meta.Notes, ";"), "negative cache") {
		t.Fatalf("expected a negative-cache note, got %v", meta.Notes)
	}

	// Not a breaker: once the TTL lapses the endpoint is tried again.
	c.now = func() time.Time { return time.Now().Add(31 * time.Second) }
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(2)); err == nil {
		t.Fatal("expected the retried call to fail against the broken endpoint")
	}
	if got := atomic.LoadInt32(&requests); got != first+1 {
		t.Fatalf("expected the endpoint to be retried after the TTL: %d requests", got)
	}
}

func TestFilterRespectsWholeCallDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_ = json.NewEncoder(w).Encode(map[string]any{"probabilities": map[string]float64{}})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", Deadline: 200 * time.Millisecond})
	start := time.Now()
	probs, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	elapsed := time.Since(start)
	if err == nil || probs != nil || !meta.Degraded {
		t.Fatalf("expected a degraded deadline failure, got probs=%v meta=%+v err=%v", probs, meta, err)
	}
	if elapsed > time.Second {
		t.Fatalf("filter ignored its deadline: %v", elapsed)
	}
}

func TestFilterCostFromUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probabilities": func() map[string]float64 {
				p := map[string]float64{}
				for _, k := range keys {
					p[k] = 0.9
				}
				return p
			}(),
			"usage": map[string]any{"input_tokens": 2000},
		})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", PricePerMillionInputTokens: 0.2})
	_, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if math.Abs(meta.CostUSD-0.0004) > 1e-9 {
		t.Fatalf("expected 2000 tokens * $0.2/M = $0.0004, got %v", meta.CostUSD)
	}
}

func TestFilterCostUnknownIsNotedNotInvented(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", PricePerMillionInputTokens: 0.2})
	_, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if meta.CostUSD != 0 {
		t.Fatalf("cost must stay 0 without usage, got %v", meta.CostUSD)
	}
	if !strings.Contains(strings.Join(meta.Notes, ";"), "usage") {
		t.Fatalf("expected an unknown-cost note, got %v", meta.Notes)
	}
}

func TestFilterMetaAccountsKeptAndDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		respondProbabilities(w, keys, 0.1)
	}))
	defer srv.Close()

	cands := testCandidates(3)
	cands[0].Pinned = true
	cands[0].Trigger = "pnpm"
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	_, meta, err := c.Filter(context.Background(), "install pnpm", cands)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if meta.Kept != 1 || meta.Dropped != 2 {
		t.Fatalf("pinned injections count as kept and never as dropped: %+v", meta)
	}
}

func TestFilterErrorNeverLeaksAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "boom"}})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "sk-verysecretvalue"})
	_, _, err := c.Filter(context.Background(), "q", testCandidates(1))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-verysecretvalue") {
		t.Fatalf("error leaks the API key: %v", err)
	}
}

func TestFilterEmptyCandidatesSkipsNetwork(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	probs, meta, err := c.Filter(context.Background(), "q", nil)
	if err != nil || len(probs) != 0 || meta.Degraded {
		t.Fatalf("empty input must be a no-op: %v %+v %v", probs, meta, err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("empty input must not call the endpoint, got %d requests", got)
	}
}

// --- write gate -------------------------------------------------------------

func gateProbsServer(t *testing.T, durable, preference, secret float64, onRequest func(pointerRequest)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, keys := decodePointer(t, r)
		if onRequest != nil {
			onRequest(req)
		}
		probs := map[string]float64{}
		for _, k := range keys {
			switch k {
			case filter.ReasonDurable:
				probs[k] = durable
			case filter.ReasonPreference:
				probs[k] = preference
			case filter.ReasonSecret:
				probs[k] = secret
			default:
				t.Errorf("unexpected gate question %q", k)
				probs[k] = 0
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"probabilities": probs})
	}))
}

func TestGateSecretDraftShortCircuitsLocally(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	draft := "my deepseek key is sk-abcdefgh12345678"

	dec, err := c.Gate(context.Background(), "remember my key", draft)
	if err != nil {
		t.Fatalf("local short-circuit must not error: %v", err)
	}
	if dec.Route != filter.RouteSkip {
		t.Fatalf("secret without an explicit request must be skipped, got %q", dec.Route)
	}
	if dec.Reasons[filter.ReasonSecret] != 1 {
		t.Fatalf("expected is_secret=1, got %+v", dec.Reasons)
	}

	dec, err = c.GateWithOptions(context.Background(), GateRequest{
		UserTurn:      "remember my deepseek key",
		Draft:         draft,
		UserRequested: true,
	})
	if err != nil {
		t.Fatalf("local short-circuit must not error: %v", err)
	}
	if dec.Route != filter.RouteWrite {
		t.Fatalf("an explicit request must write, got %q", dec.Route)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("secret-shaped drafts must never be sent anywhere, got %d requests", got)
	}
}

func TestGateMasksUserTurnBeforeCloud(t *testing.T) {
	var seen pointerRequest
	srv := gateProbsServer(t, 0.9, 0.1, 0.05, func(req pointerRequest) { seen = req })
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	dec, err := c.Gate(context.Background(), "here is my hf_abcdefghijklmnop token", "the user keeps their HuggingFace token handy")
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if dec.Route != filter.RouteWrite {
		t.Fatalf("expected a write for durable non-secret content, got %q", dec.Route)
	}
	if strings.Contains(seen.State.Query, "hf_abcdefghijklmnop") {
		t.Fatalf("raw secret reached the cloud: %q", seen.State.Query)
	}
	if !strings.Contains(seen.State.Query, "[REDACTED]") {
		t.Fatalf("expected the turn to be masked in transit: %q", seen.State.Query)
	}
}

func TestGateRoutesForDurabilityAndPreference(t *testing.T) {
	cases := []struct {
		name                  string
		durable, pref, secret float64
		want                  string
	}{
		{"durable", 0.9, 0.1, 0.05, filter.RouteWrite},
		{"preference", 0.2, 0.9, 0.05, filter.RouteWrite},
		{"one-off", 0.1, 0.1, 0.05, filter.RouteSkip},
		{"secret", 0.9, 0.1, 0.9, filter.RouteSkip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := gateProbsServer(t, tc.durable, tc.pref, tc.secret, nil)
			defer srv.Close()
			c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
			dec, err := c.Gate(context.Background(), "user turn", "a draft worth considering")
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if dec.Route != tc.want {
				t.Fatalf("expected %q, got %q (reasons %+v)", tc.want, dec.Route, dec.Reasons)
			}
		})
	}
}

func TestGateUserRequestedOverridesCloudSecret(t *testing.T) {
	srv := gateProbsServer(t, 0.9, 0.1, 0.9, nil)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	dec, err := c.GateWithOptions(context.Background(), GateRequest{
		UserTurn:      "save this vendor token for me",
		Draft:         "vendor credential for the eval box",
		UserRequested: true,
	})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if dec.Route != filter.RouteWrite {
		t.Fatalf("an explicit user request must win over the secret verdict, got %q", dec.Route)
	}
}

func TestGateNeverAsksNeedsProbe(t *testing.T) {
	var seen pointerRequest
	srv := gateProbsServer(t, 0.9, 0.1, 0.05, func(req pointerRequest) { seen = req })
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	dec, err := c.Gate(context.Background(), "turn", "durable draft")
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if _, ok := seen.Questions[filter.ReasonNeedsProbe]; ok {
		t.Fatalf("needs_probe is disabled this iteration and must not be asked")
	}
	if dec.Reasons[filter.ReasonNeedsProbe] != 0 {
		t.Fatalf("needs_probe must stay 0, got %v", dec.Reasons[filter.ReasonNeedsProbe])
	}
}

func TestGateFailsOpenOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", GateTimeout: 100 * time.Millisecond})
	start := time.Now()
	dec, err := c.Gate(context.Background(), "turn", "a draft")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a timed-out gate must surface an error so callers keep their own rules")
	}
	if dec.Route != filter.RouteNoGate {
		t.Fatalf("fail-open must report no gate, got %q", dec.Route)
	}
	if elapsed > time.Second {
		t.Fatalf("gate ignored its 500ms-class timeout: %v", elapsed)
	}
}

func TestGateDoesNotRetry(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if _, err := c.Gate(context.Background(), "turn", "a draft"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("expected exactly one gate request, got %d", got)
	}
}

func TestGateFailsOpenDuringNegativeCache(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if _, _, err := c.Filter(context.Background(), "q", testCandidates(2)); err == nil {
		t.Fatal("expected the filter call to fail")
	}
	before := atomic.LoadInt32(&requests)
	if _, err := c.Gate(context.Background(), "turn", "a durable draft"); err == nil {
		t.Fatal("expected the gate to report no gate")
	}
	if got := atomic.LoadInt32(&requests); got != before {
		t.Fatalf("the negative cache must skip the gate call too: %d -> %d", before, got)
	}
}

// TestFilterMasksStoredSecretsInTheWireRequest pins the arbitration decision that
// the read path masks stored memory text too: a credential the user explicitly
// asked to store (the user_requested override) may live in a local entry, but it
// must never be shipped to the endpoint in cleartext, and the surrounding memory
// text must survive so relevance scoring is unaffected.
func TestFilterMasksStoredSecretsInTheWireRequest(t *testing.T) {
	var seen pointerRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, keys := decodePointer(t, r)
		seen = req
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	cands := []filter.Candidate{
		{ID: "m1", Name: "stored-key", Text: "my huggingface token is hf_abcdefghijklmnop"},
		{ID: "m2", Name: "editor", Text: "the user edits in vim"},
	}
	if _, _, err := c.Filter(context.Background(), "which editor?", cands); err != nil {
		t.Fatalf("filter: %v", err)
	}
	serialized, err := json.Marshal(seen.State.Memories)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "hf_abcdefghijklmnop") {
		t.Fatalf("a stored credential reached the endpoint in cleartext: %s", serialized)
	}
	if !strings.Contains(string(serialized), "[REDACTED]") {
		t.Fatalf("the stored credential was not masked: %s", serialized)
	}
	if !strings.Contains(string(serialized), "the user edits in vim") {
		t.Fatalf("ordinary memory text must survive masking: %s", serialized)
	}
}

// TestGateEmptyDraftIsNoGate pins the arbitration decision for a degenerate
// input: an empty draft is not one of the contract's skip rules (one-off, secret),
// so the gate reports "no decision" and the caller keeps its own write rules — a
// gate must never block a write on an input its rules do not cover.
func TestGateEmptyDraftIsNoGate(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	dec, err := c.Gate(context.Background(), "turn", "   ")
	if err != nil {
		t.Fatalf("empty draft: %v", err)
	}
	if dec.Route != filter.RouteNoGate {
		t.Fatalf("empty draft must leave the write rules to the caller, got %q", dec.Route)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("empty draft must not call the endpoint, got %d requests", got)
	}
}

func TestFilterReportsUsageTokensWithoutInventing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		probs := map[string]float64{}
		for _, k := range keys {
			probs[k] = 0.9
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probabilities": probs,
			"usage":         map[string]any{"input_tokens": 2000, "output_tokens": 50},
		})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", PricePerMillionInputTokens: 0.2})
	_, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if meta.InputTokens != 2000 || meta.OutputTokens != 50 {
		t.Fatalf("usage tokens = %d/%d, want 2000/50 (SC-007 token accounting)", meta.InputTokens, meta.OutputTokens)
	}
	if math.Abs(meta.CostUSD-0.0004) > 1e-9 {
		t.Fatalf("cost = %v, want 0.0004", meta.CostUSD)
	}
}

func TestFilterTokensStayZeroWhenUsageIsAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		respondProbabilities(w, keys, 0.9)
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", PricePerMillionInputTokens: 0.2})
	_, meta, err := c.Filter(context.Background(), "q", testCandidates(2))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if meta.InputTokens != 0 || meta.OutputTokens != 0 {
		t.Fatalf("tokens must stay 0 (unknown, not invented), got %d/%d", meta.InputTokens, meta.OutputTokens)
	}
	if !strings.Contains(strings.Join(meta.Notes, ";"), "usage") {
		t.Fatalf("expected an unknown-usage note, got %v", meta.Notes)
	}
}

func TestFilterSumsUsageTokensAcrossShards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		probs := map[string]float64{}
		for _, k := range keys {
			probs[k] = 0.9
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probabilities": probs,
			"usage":         map[string]any{"input_tokens": 100, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	// 100 candidates above the shard threshold split into three requests.
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k", ShardThreshold: 64, ShardSize: 48})
	_, meta, err := c.Filter(context.Background(), "q", testCandidates(100))
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if meta.InputTokens != 300 || meta.OutputTokens != 15 {
		t.Fatalf("sharded usage tokens = %d/%d, want 300/15 summed across shards", meta.InputTokens, meta.OutputTokens)
	}
}

func TestGateReportsUsageTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keys := decodePointer(t, r)
		probs := map[string]float64{}
		for _, k := range keys {
			probs[k] = 0
		}
		probs[filter.ReasonDurable] = 0.9
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probabilities": probs,
			"usage":         map[string]any{"input_tokens": 500, "output_tokens": 40},
		})
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, Model: "m", APIKey: "k"})
	dec, err := c.Gate(context.Background(), "user turn", "a durable draft")
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if dec.Meta.InputTokens != 500 || dec.Meta.OutputTokens != 40 {
		t.Fatalf("gate usage tokens = %d/%d, want 500/40", dec.Meta.InputTokens, dec.Meta.OutputTokens)
	}
	if dec.Route != filter.RouteWrite {
		t.Fatalf("route = %q, want write", dec.Route)
	}
}
