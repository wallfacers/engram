package main

// 045 ride-along: 042 counterfactual-signal re-verification, SELF-CONTAINED.
//
// 043's verdict (docs/evaluation/reports/043-confidence-deepen-pilot-verdict-
// 2026-08-15.md) proved the 042 signal-collection path fails structurally on
// the current vllm stack (304/304 content_not_generated_suffix: inline
// thinking + trailing <|im_end|> break the strict "content is an exact suffix
// of reconstructed bytes" precondition), so 042's "5/5958 NO-GO" was never a
// valid measurement. This file re-collects the signal with the fixed
// measurement (commit 1eb9cdd semantics: temperature=0 on the logprob channel
// + robust final-span mapping) WITHOUT importing anything from
// counterfactual_utility*.go or confidence_deepen*.go — the 044
// default-off-cleanup deletes those files, and 045 must not dangle.
//
// Formula-faithful replications below (equivalence proven against goldens in
// reverify_042_test.go): the final-span mapper reproduces
// deepenFinalSpanSignal, and the caller reproduces utilityLogprobCaller's
// wire contract except that temperature is ALWAYS pinned to 0 (the 042
// omission was one of the two measurement bugs).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
)

const (
	rvMaxAttempts    = 3
	rvResponseLimit  = 64 << 20 // 64 MiB, same bound as the 042 caller
	rvDefaultMaxTok  = 8000
	rvBootstrapIters = 2000
	rvBootstrapSeed  = 43
	rvStreamBufCap   = 1 << 20 // SSE line buffer
)

// rvSpecialTokens are vllm generation-end specials (copy of the 043 set).
var rvSpecialTokens = map[string]bool{
	"<|im_end|>":    true,
	"<|endoftext|>": true,
	"<|im_start|>":  true,
}

// rvLogprobToken mirrors the 042 token shape (own struct; no import).
type rvLogprobToken struct {
	Token   string
	Bytes   []byte
	Logprob float64
	Top1    float64
	Top2    float64 // 0.0 == absent
}

type rvCompletion struct {
	Content     string
	Tokens      []rvLogprobToken
	InputTokens int
	OutputTokns int
}

// rvCaller is a minimal OpenAI-compatible non-streaming chat/completions
// caller with logprobs, temperature pinned to 0.
type rvCaller struct {
	endpoint  string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func rvNewCaller(baseURL, apiKey, model string, maxTokens int, client *http.Client) (*rvCaller, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("reverify caller requires base URL and model")
	}
	if client == nil {
		client = http.DefaultClient
	}
	if maxTokens <= 0 {
		maxTokens = rvDefaultMaxTok
	}
	return &rvCaller{
		endpoint:  strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    client,
	}, nil
}

// rvResponse is the parsed non-streaming response shape (own structs).
type rvResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Logprobs *struct {
			Content []struct {
				Token       string  `json:"token"`
				Bytes       []int   `json:"bytes"`
				Logprob     float64 `json:"logprob"`
				TopLogprobs []struct {
					Token   string  `json:"token"`
					Bytes   []int   `json:"bytes"`
					Logprob float64 `json:"logprob"`
				} `json:"top_logprobs"`
			} `json:"content"`
		} `json:"logprobs"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// complete performs one logical call with ≤3 attempts; only network errors,
// 429 and 5xx retry (same policy as the 042 caller).
func (c *rvCaller) complete(ctx context.Context, system, user string) (rvCompletion, error) {
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":   c.maxTokens,
		"stream":       false,
		"logprobs":     true,
		"top_logprobs": 2,
		"temperature":  0, // ALWAYS pinned — the 042 omission was measurement bug #1
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return rvCompletion{}, fmt.Errorf("reverify encode: %w", err)
	}
	var lastErr error
	for attempt := 1; attempt <= rvMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return rvCompletion{}, fmt.Errorf("reverify request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return rvCompletion{}, fmt.Errorf("reverify canceled: %w", ctx.Err())
			}
			lastErr = fmt.Errorf("reverify network error")
			continue
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, _ = io.CopyN(io.Discard, resp.Body, 4*1024)
			resp.Body.Close() //nolint:errcheck
			if !retryable || attempt == rvMaxAttempts {
				return rvCompletion{}, fmt.Errorf("reverify status %d", resp.StatusCode)
			}
			lastErr = fmt.Errorf("reverify status %d", resp.StatusCode)
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, rvResponseLimit+1))
		resp.Body.Close() //nolint:errcheck
		if err != nil {
			return rvCompletion{}, fmt.Errorf("reverify read: %w", err)
		}
		if len(raw) > rvResponseLimit {
			return rvCompletion{}, fmt.Errorf("reverify response too large (>%d bytes)", rvResponseLimit)
		}
		var out rvResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return rvCompletion{}, fmt.Errorf("reverify decode error")
		}
		if len(out.Choices) == 0 {
			return rvCompletion{}, fmt.Errorf("reverify empty choice")
		}
		completion := rvCompletion{Content: out.Choices[0].Message.Content}
		if out.Usage != nil {
			completion.InputTokens = out.Usage.PromptTokens
			completion.OutputTokns = out.Usage.CompletionTokens
		}
		if lp := out.Choices[0].Logprobs; lp != nil {
			for _, tok := range lp.Content {
				bt := []byte(tok.Token)
				if len(tok.Bytes) > 0 {
					bt = make([]byte, len(tok.Bytes))
					for i, b := range tok.Bytes {
						bt[i] = byte(b)
					}
				}
				top := make([]float64, 0, len(tok.TopLogprobs))
				for _, a := range tok.TopLogprobs {
					top = append(top, a.Logprob)
				}
				sort.Sort(sort.Reverse(sort.Float64Slice(top)))
				var top1, top2 float64
				if len(top) > 0 {
					top1 = top[0]
				}
				if len(top) > 1 {
					top2 = top[1]
				}
				completion.Tokens = append(completion.Tokens, rvLogprobToken{
					Token: tok.Token, Bytes: bt, Logprob: tok.Logprob, Top1: top1, Top2: top2,
				})
			}
		}
		return completion, nil
	}
	return rvCompletion{}, lastErr
}

// rvStreamer is a minimal OpenAI-compatible SSE streaming client (the second
// channel of the dual-channel flip measurement; same wire contract as the
// bench answering path: stream=true, temperature=0).
type rvStreamer struct {
	endpoint  string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func rvNewStreamer(baseURL, apiKey, model string, maxTokens int) *rvStreamer {
	if maxTokens <= 0 {
		maxTokens = rvDefaultMaxTok
	}
	return &rvStreamer{
		endpoint:  strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    http.DefaultClient,
	}
}

type rvStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (s *rvStreamer) complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":  s.maxTokens,
		"stream":      true,
		"temperature": 0,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("reverify stream encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("reverify stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reverify stream network error")
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, resp.Body, 4*1024)
		return "", fmt.Errorf("reverify stream status %d", resp.StatusCode)
	}
	var sb strings.Builder
	sc := bufio.NewScanner(io.LimitReader(resp.Body, rvResponseLimit+1))
	sc.Buffer(make([]byte, rvStreamBufCap), rvStreamBufCap)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk rvStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip keep-alives/malformed frames
		}
		for _, c := range chunk.Choices {
			sb.WriteString(c.Delta.Content)
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reverify stream read: %w", err)
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("reverify stream empty content")
	}
	return sb.String(), nil
}

// rvFinalSpanSignal replicates deepenFinalSpanSignal (1eb9cdd) exactly: strip
// specials, find the LAST thinking close delimiter in the visible
// reconstruction, map to the first whole visible token at/after it, and
// aggregate [mean logprob, ceil-idx p10 logprob, mean top1−top2 margin].
// thinkingCloseDelims comes from runner.go (a kept file — not a 044 delete
// target), keeping one source of truth for the delimiter set.
func rvFinalSpanSignal(tokens []rvLogprobToken) (features []float64, available bool, reason string) {
	unavail := func(r string) ([]float64, bool, string) { return nil, false, r }
	var visIdx []int
	var starts []int
	var recon []byte
	for i := range tokens {
		if rvSpecialTokens[tokens[i].Token] {
			continue
		}
		visIdx = append(visIdx, i)
		starts = append(starts, len(recon))
		recon = append(recon, tokens[i].Bytes...)
	}
	visible := string(recon)
	finalStart := 0
	for _, d := range thinkingCloseDelims {
		if idx := strings.LastIndex(visible, d); idx >= 0 && idx+len(d) > finalStart {
			finalStart = idx + len(d)
		}
	}
	lo, hi := 0, len(visIdx)
	for lo < hi {
		mid := (lo + hi) / 2
		if starts[mid] < finalStart {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(visIdx) {
		return unavail("empty_final_span")
	}
	var sampled []float64
	marginSum := 0.0
	for v := lo; v < len(visIdx); v++ {
		tok := tokens[visIdx[v]]
		if math.IsNaN(tok.Logprob) || math.IsInf(tok.Logprob, 0) ||
			math.IsNaN(tok.Top1) || math.IsInf(tok.Top1, 0) ||
			math.IsNaN(tok.Top2) || math.IsInf(tok.Top2, 0) {
			return unavail("non_finite")
		}
		if tok.Top2 == 0.0 {
			return unavail("missing_top2")
		}
		sampled = append(sampled, tok.Logprob)
		marginSum += tok.Top1 - tok.Top2
	}
	if len(sampled) == 0 {
		return unavail("empty_final_span")
	}
	mean := 0.0
	for _, v := range sampled {
		mean += v
	}
	mean /= float64(len(sampled))
	sorted := append([]float64(nil), sampled...)
	sort.Float64s(sorted)
	p10Idx := int(math.Ceil(0.10*float64(len(sorted)))) - 1
	if p10Idx < 0 {
		p10Idx = 0
	}
	feats := []float64{mean, sorted[p10Idx], marginSum / float64(len(sampled))}
	for _, f := range feats {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return unavail("non_finite")
		}
	}
	return feats, true, ""
}

// rvAUC replicates deepenAUC's rank-mean WMW exactly (same formula family as
// the 042 calibration and the 043 pilot — comparability requirement).
func rvAUC(posScores, negScores []float64) (float64, error) {
	type scored struct {
		score float64
		pos   bool
	}
	var all []scored
	for _, s := range posScores {
		all = append(all, scored{score: s, pos: true})
	}
	for _, s := range negScores {
		all = append(all, scored{score: s, pos: false})
	}
	if len(all) < 2 {
		return 0, fmt.Errorf("reverify AUC requires at least two scored units, got %d", len(all))
	}
	nPos, nNeg := len(posScores), len(negScores)
	if nPos == 0 || nNeg == 0 {
		return 0, fmt.Errorf("reverify AUC needs both classes, got pos=%d neg=%d", nPos, nNeg)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score < all[j].score })
	ranks := make([]float64, len(all))
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].score == all[i].score {
			j++
		}
		avgRank := (float64(i) + 1 + float64(j)) / 2
		for k := i; k < j; k++ {
			ranks[k] = avgRank
		}
		i = j
	}
	rankSum := 0.0
	for i := range all {
		if all[i].pos {
			rankSum += ranks[i]
		}
	}
	return (rankSum - float64(nPos*(nPos+1))/2) / float64(nPos*nNeg), nil
}

// rvAUCBootstrap replicates deepenAUCBootstrap: fixed-seed percentile
// bootstrap, resampling each class with replacement.
func rvAUCBootstrap(posScores, negScores []float64, seed int64, iterations int) (lo, hi float64, err error) {
	if iterations <= 0 {
		iterations = 1000
	}
	rng := rand.New(rand.NewSource(seed))
	aucs := make([]float64, 0, iterations)
	for i := 0; i < iterations; i++ {
		pos := make([]float64, len(posScores))
		neg := make([]float64, len(negScores))
		for j := range pos {
			pos[j] = posScores[rng.Intn(len(posScores))]
		}
		for j := range neg {
			neg[j] = negScores[rng.Intn(len(negScores))]
		}
		a, err := rvAUC(pos, neg)
		if err != nil {
			continue
		}
		aucs = append(aucs, a)
	}
	if len(aucs) < 2 {
		return 0, 0, fmt.Errorf("reverify bootstrap produced too few AUC samples (%d)", len(aucs))
	}
	sort.Float64s(aucs)
	lo = aucs[int(0.025*float64(len(aucs)))]
	hi = aucs[int(0.975*float64(len(aucs)))]
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo, hi, nil
}
