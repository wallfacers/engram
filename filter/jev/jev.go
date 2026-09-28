// Package jev implements the Jev relevance filter and write gate for engram.
//
// Jev is a calibrated probability model: instead of an opaque relevance score it
// answers one conditional question per memory ("is this memory necessary to
// answer or act on the query?") and returns a probability. The client speaks the
// pointer protocol from the design doc (§21.4) over plain net/http — shared
// `state` carrying the memories once, plus one question per memory — so a large
// pool costs one copy of the text instead of one copy per question.
//
// The client is optional and offline-degradable: New returns (nil, nil) when
// unconfigured, failures return an error with Degraded=true and no partial
// probabilities, and the write gate fails open so an unavailable gate never
// blocks a write. Credentials never appear in errors, and secret-shaped text is
// short-circuited or masked before it can reach the endpoint.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/filter"
)

const (
	defaultPath       = "/answers"
	minShardSize      = 6
	maxShardSize      = 64
	defaultShardSize  = 48
	defaultShardLimit = 64
	maxResponseBytes  = 4 << 20
)

// errNegativeCache reports a call skipped because the endpoint failed recently.
// It is a cache, not a circuit breaker: the endpoint is retried once the TTL
// lapses.
var errNegativeCache = errors.New("jev: endpoint is in the negative cache")

// Config parameterizes the Jev client. BaseURL, Model and APIKey must all be
// non-empty for New to return a usable client; every other field has a
// documented default.
type Config struct {
	BaseURL string
	Model   string
	APIKey  string
	// Path is appended to BaseURL. The Jev wire contract does not pin the route
	// yet, so it stays configurable (default "/answers").
	Path string
	// Deadline bounds the whole Filter call (default 1s). It is derived from the
	// caller context, never extended.
	Deadline time.Duration
	// PerRequestTimeout caps a single HTTP request (default 1s); the effective
	// value is always min(remaining deadline, this cap).
	PerRequestTimeout time.Duration
	// ShardThreshold is the pool size above which the pool is split into
	// concurrent shards (default 64).
	ShardThreshold int
	// ShardSize is the number of candidates per shard, clamped into [6,64]
	// (default 48). The floor is small because a hosted endpoint may reject large
	// pointer requests outright; a caller with room to spare keeps 48.
	ShardSize int
	// NegativeCacheTTL suppresses new calls to a failing endpoint (default 30s).
	NegativeCacheTTL time.Duration
	// PricePerMillionInputTokens converts response usage into CostUSD. Zero keeps
	// cost at 0; the price list is the operator's to maintain.
	PricePerMillionInputTokens float64
	// Policy is the client's own accounting policy, used for FilterMeta
	// Kept/Dropped. The caller's policy still governs the final selection.
	Policy filter.Policy
	// GateTheta thresholds the write-gate probabilities (default 0.5).
	GateTheta float64
	// GateTimeout bounds the write-gate call; failure means "no gate", so the
	// default is short (500ms).
	GateTimeout time.Duration
}

// Client is the Jev implementation of both filter.RelevanceFilter and
// filter.WriteGate.
type Client struct {
	cfg  Config
	http *http.Client
	// now is swappable in tests to exercise the negative-cache TTL.
	now func() time.Time

	mu          sync.Mutex
	failedUntil time.Time
}

var (
	_ filter.RelevanceFilter = (*Client)(nil)
	_ filter.WriteGate       = (*Client)(nil)
)

// New builds a Client. It returns (nil, nil) when BaseURL, Model or APIKey is
// empty — the documented "Jev disabled" state, so callers can write
// `c, _ := jev.New(cfg); if c == nil { /* degrade to RRF top-N */ }`. The
// concrete nil is collapsed to an untyped nil at the interface boundary.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, nil
	}
	if cfg.Path == "" {
		cfg.Path = defaultPath
	}
	if !strings.HasPrefix(cfg.Path, "/") {
		cfg.Path = "/" + cfg.Path
	}
	if cfg.Deadline <= 0 {
		cfg.Deadline = time.Second
	}
	if cfg.PerRequestTimeout <= 0 {
		cfg.PerRequestTimeout = time.Second
	}
	if cfg.ShardThreshold <= 0 {
		cfg.ShardThreshold = defaultShardLimit
	}
	if cfg.ShardSize <= 0 {
		cfg.ShardSize = defaultShardSize
	}
	if cfg.ShardSize < minShardSize {
		cfg.ShardSize = minShardSize
	}
	if cfg.ShardSize > maxShardSize {
		cfg.ShardSize = maxShardSize
	}
	if cfg.NegativeCacheTTL <= 0 {
		cfg.NegativeCacheTTL = 30 * time.Second
	}
	if cfg.Policy == (filter.Policy{}) {
		cfg.Policy = filter.DefaultPolicy()
	}
	if cfg.GateTheta <= 0 {
		cfg.GateTheta = 0.5
	}
	if cfg.GateTimeout <= 0 {
		cfg.GateTimeout = 500 * time.Millisecond
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.PerRequestTimeout}, now: time.Now}, nil
}

// Model reports the configured model revision, which evaluation manifests pin.
func (c *Client) Model() string { return c.cfg.Model }

// Filter scores every candidate against the query and returns probabilities
// aligned by index. On any failure (shard error, timeout, missing probability)
// it returns a non-nil error, Degraded=true and no probabilities at all: a
// partially scored pool would produce an unreproducible short list.
func (c *Client) Filter(ctx context.Context, query string, cands []filter.Candidate) ([]float64, filter.FilterMeta, error) {
	start := c.now()
	meta := filter.FilterMeta{Backend: filter.BackendJev, Theta: c.cfg.Policy.Theta}
	if len(cands) == 0 {
		return nil, meta, nil
	}
	if c.inNegativeCache() {
		meta.Degraded = true
		meta.LatencyMs = elapsedMs(start, c.now())
		meta.Notes = append(meta.Notes, fmt.Sprintf(
			"negative cache: endpoint failed within the last %s; degraded without a network call", c.cfg.NegativeCacheTTL))
		return nil, meta, errNegativeCache
	}

	overall, cancel := context.WithTimeout(ctx, c.cfg.Deadline)
	defer cancel()

	shards := shardCandidates(cands, c.cfg.ShardThreshold, c.cfg.ShardSize)
	probs := make([]float64, len(cands))
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		firstErr     error
		usage        usageAccount
		usageMissing bool
	)
	for _, shard := range shards {
		wg.Add(1)
		go func(shard []candidateRef) {
			defer wg.Done()
			shardProbs, shardUsage, err := c.scoreShard(overall, query, shard)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			usage.InputTokens += shardUsage.InputTokens
			usage.OutputTokens += shardUsage.OutputTokens
			usage.Known = usage.Known || shardUsage.Known
			if !shardUsage.Known {
				usageMissing = true
			}
			for i, p := range shardProbs {
				probs[shard[i].idx] = p
			}
		}(shard)
	}
	wg.Wait()

	meta.LatencyMs = elapsedMs(start, c.now())
	meta.InputTokens = usage.InputTokens
	meta.OutputTokens = usage.OutputTokens
	meta.CostUSD = costOf(usage, c.cfg.PricePerMillionInputTokens)
	if usageMissing {
		meta.Notes = append(meta.Notes, "cost unknown: response carried no usage")
	}
	if firstErr != nil {
		c.markFailure()
		meta.Degraded = true
		meta.Notes = append(meta.Notes, "shard failure: no partial adoption; the caller must fall back to the fused top-N")
		return nil, meta, firstErr
	}

	kept := filter.SelectForQuery(query, probs, cands, c.cfg.Policy)
	meta.Kept = len(kept)
	meta.Dropped = len(cands) - len(kept)
	if c.cfg.Policy.KShowMax > 0 && meta.Kept > c.cfg.Policy.KShowMax {
		meta.Notes = append(meta.Notes, fmt.Sprintf(
			"pinned override: %d entries exceed KShowMax %d", meta.Kept, c.cfg.Policy.KShowMax))
	}
	return probs, meta, nil
}

// Gate implements filter.WriteGate. user_requested is not part of the shorthand
// signature; callers that carry it use GateWithOptions.
func (c *Client) Gate(ctx context.Context, userTurn, draft string) (filter.GateDecision, error) {
	return c.GateWithOptions(ctx, filter.GateRequest{UserTurn: userTurn, Draft: draft})
}

// GateRequest is the canonical write-gate input, aliased here so existing
// jev.GateRequest references keep compiling. The type lives in package filter
// (data-model.md) so a third-party WriteGate can carry UserRequested without
// importing this package.
type GateRequest = filter.GateRequest

// GateWithOptions decides whether a draft may be written. It never blocks on
// failure: an unavailable gate returns RouteNoGate plus an error so callers keep
// their current write rules. Secret-shaped drafts short-circuit locally and are
// never transmitted.
func (c *Client) GateWithOptions(ctx context.Context, req filter.GateRequest) (filter.GateDecision, error) {
	start := c.now()
	dec := filter.GateDecision{
		Reasons: map[string]float64{
			filter.ReasonDurable:    0,
			filter.ReasonPreference: 0,
			filter.ReasonSecret:     0,
			filter.ReasonNeedsProbe: 0,
		},
		Meta: filter.FilterMeta{Backend: filter.BackendJev, Theta: c.cfg.GateTheta},
	}
	if strings.TrimSpace(req.Draft) == "" {
		// Nothing to persist, and the contract's skip rules cover one-off and
		// secret drafts only: an ungated server would persist it, so the gate
		// reports "no decision" and the caller keeps its own write rules.
		dec.Route = filter.RouteNoGate
		dec.Meta.Notes = append(dec.Meta.Notes, "empty draft: no gate decision")
		return dec, nil
	}
	if filter.LooksLikeSecret(req.Draft) {
		dec.Reasons[filter.ReasonSecret] = 1
		dec.Meta.Notes = append(dec.Meta.Notes, "local secret-shape short-circuit: no cloud call")
		dec.Route = filter.RouteSkip
		if req.UserRequested {
			dec.Route = filter.RouteWrite
		}
		return dec, nil
	}
	if c.inNegativeCache() {
		dec.Meta.Notes = append(dec.Meta.Notes, "negative cache: gate skipped")
		return filter.GateDecision{Meta: dec.Meta}, errNegativeCache
	}

	timeout := c.cfg.GateTimeout
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem < timeout {
			timeout = rem
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	verdicts, usage, err := c.gateCall(callCtx, req)
	dec.Meta.LatencyMs = elapsedMs(start, c.now())
	dec.Meta.InputTokens = usage.InputTokens
	dec.Meta.OutputTokens = usage.OutputTokens
	dec.Meta.CostUSD = costOf(usage, c.cfg.PricePerMillionInputTokens)
	if !usage.Known {
		dec.Meta.Notes = append(dec.Meta.Notes, "cost unknown: response carried no usage")
	}
	if err != nil {
		dec.Meta.Notes = append(dec.Meta.Notes, "gate unavailable: callers keep their current write rules")
		return filter.GateDecision{Meta: dec.Meta}, fmt.Errorf("jev: write gate unavailable: %w", err)
	}

	// Precedence: an explicit user request beats the secret verdict, the secret
	// verdict beats durability, and durable/preference content is written.
	theta := c.cfg.GateTheta
	secret := verdicts[filter.ReasonSecret]
	durable := verdicts[filter.ReasonDurable]
	preference := verdicts[filter.ReasonPreference]
	switch {
	case secret >= theta && req.UserRequested:
		dec.Route = filter.RouteWrite
	case secret >= theta:
		dec.Route = filter.RouteSkip
	case durable >= theta || preference >= theta:
		dec.Route = filter.RouteWrite
	default:
		dec.Route = filter.RouteSkip
	}
	dec.Reasons = map[string]float64{
		filter.ReasonDurable:    durable,
		filter.ReasonPreference: preference,
		filter.ReasonSecret:     secret,
		filter.ReasonNeedsProbe: 0, // disabled this iteration: no short-packet line yet
	}
	return dec, nil
}

// candidateRef maps one candidate to its pointer-protocol keys: state.memories
// is keyed by the memory id (contract §2), while the conditional question is
// keyed "need_<memory id>".
type candidateRef struct {
	idx    int
	memKey string
	qKey   string
	cand   filter.Candidate
}

func shardCandidates(cands []filter.Candidate, threshold, size int) [][]candidateRef {
	refs := make([]candidateRef, len(cands))
	seen := make(map[string]bool, len(cands))
	for i, cand := range cands {
		memKey := cand.ID
		if memKey == "" || seen[memKey] {
			memKey = fmt.Sprintf("c%d", i)
		}
		seen[memKey] = true
		refs[i] = candidateRef{idx: i, memKey: memKey, qKey: "need_" + memKey, cand: cand}
	}
	if len(refs) <= threshold {
		return [][]candidateRef{refs}
	}
	var shards [][]candidateRef
	for start := 0; start < len(refs); start += size {
		end := min(start+size, len(refs))
		shards = append(shards, refs[start:end])
	}
	return shards
}

func (c *Client) scoreShard(parent context.Context, query string, shard []candidateRef) ([]float64, usageAccount, error) {
	payload := pointerRequest{
		Model:     c.cfg.Model,
		State:     pointerState{Query: query, Memories: memories(shard)},
		Questions: retrievalQuestions(shard, query),
	}
	ctx, cancel, err := c.requestContext(parent)
	if err != nil {
		return nil, usageAccount{}, err
	}
	defer cancel()

	out, err := c.post(ctx, payload)
	if err != nil {
		return nil, usageAccount{}, err
	}
	byKey := out.probabilityByQuestion()
	probs := make([]float64, len(shard))
	for i, ref := range shard {
		p, ok := byKey[ref.qKey]
		if !ok {
			return nil, usageAccount{}, fmt.Errorf("jev: response is missing the probability for question %q", ref.qKey)
		}
		probs[i] = p
	}
	return probs, accountUsage(out.Usage), nil
}

func (c *Client) gateCall(ctx context.Context, req filter.GateRequest) (map[string]float64, usageAccount, error) {
	draft := filter.MaskSecrets(req.Draft)
	turn := filter.MaskSecrets(req.UserTurn)
	query := strings.TrimSpace(turn)
	if query == "" {
		query = draft
	}
	payload := pointerRequest{
		Model: c.cfg.Model,
		State: pointerState{
			Query:    query,
			Memories: map[string]pointerMemory{"draft": {Name: "draft", Text: draft}},
		},
		Questions: map[string]pointerQuestion{
			filter.ReasonDurable: {
				Type:         "noul",
				Instructions: "The content in memory draft is a stable fact, preference or constraint that stays true across sessions (not a one-off or temporary detail).",
			},
			filter.ReasonPreference: {
				Type:         "noul",
				Instructions: "The content in memory draft is a preference, constraint or identity fact stated by the user about themselves.",
			},
			filter.ReasonSecret: {
				Type:         "noul",
				Instructions: "The content in memory draft contains a secret such as an API key, access token, password or private credential.",
			},
		},
	}
	out, err := c.post(ctx, payload)
	if err != nil {
		return nil, usageAccount{}, err
	}
	byKey := out.probabilityByQuestion()
	verdicts := make(map[string]float64, 3)
	for _, key := range []string{filter.ReasonDurable, filter.ReasonPreference, filter.ReasonSecret} {
		p, ok := byKey[key]
		if !ok {
			return nil, usageAccount{}, fmt.Errorf("jev: write gate response is missing verdict %q", key)
		}
		verdicts[key] = p
	}
	return verdicts, accountUsage(out.Usage), nil
}

// requestContext derives the per-request deadline: min(remaining, per-request
// cap), both bounded by the parent context.
func (c *Client) requestContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	timeout := c.cfg.PerRequestTimeout
	if dl, ok := parent.Deadline(); ok {
		if rem := time.Until(dl); rem < timeout {
			timeout = rem
		}
	}
	if timeout <= 0 {
		return nil, nil, fmt.Errorf("jev: deadline exceeded before the request was sent")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, nil
}

// post sends one pointer-protocol request. Errors never include the API key and
// server-provided messages are masked before they reach a log.
func (c *Client) post(ctx context.Context, payload pointerRequest) (pointerResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return pointerResponse{}, fmt.Errorf("jev: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+c.cfg.Path, bytes.NewReader(body))
	if err != nil {
		return pointerResponse{}, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return pointerResponse{}, fmt.Errorf("jev: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return pointerResponse{}, fmt.Errorf("jev: read response: %w", err)
	}
	var out pointerResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return pointerResponse{}, fmt.Errorf("jev: decode response (status %d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK {
		msg := "endpoint returned non-200"
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return pointerResponse{}, fmt.Errorf("jev: status %d: %s", resp.StatusCode, filter.MaskSecrets(msg))
	}
	return out, nil
}

func (c *Client) inNegativeCache() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now().Before(c.failedUntil)
}

func (c *Client) markFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failedUntil = c.now().Add(c.cfg.NegativeCacheTTL)
}

// memories renders the pointer protocol's shared state. Text is masked before it
// leaves the machine: a memory the user explicitly asked to store (the
// `user_requested` override) may be secret-shaped, and the read-side filter has
// no business shipping a stored credential to the endpoint in cleartext
// (write-gate.md 硬约束 2: a written key only ever enters the local store).
func memories(shard []candidateRef) map[string]pointerMemory {
	out := make(map[string]pointerMemory, len(shard))
	for _, ref := range shard {
		out[ref.memKey] = pointerMemory{Name: ref.cand.Name, Text: filter.MaskSecrets(ref.cand.Text)}
	}
	return out
}

// retrievalQuestions asks one conditional question per memory. The condition is
// the query, not a global "is this important" judgement.
func retrievalQuestions(shard []candidateRef, query string) map[string]pointerQuestion {
	out := make(map[string]pointerQuestion, len(shard))
	for _, ref := range shard {
		out[ref.qKey] = pointerQuestion{
			Type: "noul",
			Instructions: fmt.Sprintf(
				"Memory %s is necessary to answer or act on the query. The query is about %s.",
				ref.memKey, query),
		}
	}
	return out
}

// usageAccount is the token accounting of one model call. Tokens are reported
// exactly as the endpoint stated them; Known is false when the response carried
// no usage at all, in which case the zero counts mean "unknown", not "zero".
type usageAccount struct {
	InputTokens  int
	OutputTokens int
	Known        bool
}

// accountUsage reads the token counts the endpoint reported, tolerating the
// OpenAI-style prompt_/completion_ aliases.
func accountUsage(usage *pointerUsage) usageAccount {
	if usage == nil {
		return usageAccount{}
	}
	input := usage.InputTokens
	if input == 0 {
		input = usage.PromptTokens
	}
	output := usage.OutputTokens
	if output == 0 {
		output = usage.CompletionTokens
	}
	if input == 0 && output == 0 {
		return usageAccount{}
	}
	return usageAccount{InputTokens: input, OutputTokens: output, Known: true}
}

// costOf prices the reported input tokens. Unknown usage costs nothing and is
// flagged by the caller instead of guessed.
func costOf(account usageAccount, pricePerMillion float64) float64 {
	if !account.Known {
		return 0
	}
	return float64(account.InputTokens) / 1e6 * pricePerMillion
}

func elapsedMs(start, end time.Time) int {
	d := end.Sub(start)
	if d < 0 {
		return 0
	}
	return int(d.Milliseconds())
}

// --- wire shapes (contract §2) ---------------------------------------------

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

type pointerResponse struct {
	// Probabilities is the primary shape: question key -> calibrated probability.
	Probabilities map[string]float64         `json:"probabilities"`
	Questions     map[string]json.RawMessage `json:"questions"`
	Results       []pointerResult            `json:"results"`
	Usage         *pointerUsage              `json:"usage"`
	Error         *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type pointerResult struct {
	Key         string  `json:"key"`
	Question    string  `json:"question"`
	ID          string  `json:"id"`
	Probability float64 `json:"probability"`
}

type pointerUsage struct {
	InputTokens      int `json:"input_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// probabilityByQuestion collapses the response shapes the gate and filter
// tolerate: {"probabilities": {k: p}}, {"questions": {k: p | {probability: p}}}
// and {"results": [{key|question|id, probability}]}. The client only depends on
// the "question key -> probability" mapping, so field details converge here.
func (r pointerResponse) probabilityByQuestion() map[string]float64 {
	out := make(map[string]float64, len(r.Probabilities)+len(r.Questions)+len(r.Results))
	for k, v := range r.Probabilities {
		out[k] = v
	}
	for k, raw := range r.Questions {
		if _, ok := out[k]; ok {
			continue
		}
		var number float64
		if err := json.Unmarshal(raw, &number); err == nil {
			out[k] = number
			continue
		}
		var object struct {
			Probability float64 `json:"probability"`
		}
		if err := json.Unmarshal(raw, &object); err == nil {
			out[k] = object.Probability
		}
	}
	for _, res := range r.Results {
		key := res.Key
		if key == "" {
			key = res.Question
		}
		if key == "" {
			key = res.ID
		}
		if key == "" {
			continue
		}
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = res.Probability
	}
	return out
}
