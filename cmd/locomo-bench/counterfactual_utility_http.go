package main

// 042 harness-only, non-streaming OpenAI-compatible Chat Completions caller with
// per-token logprobs, plus the strict final-answer span mapper. It is benchmark
// infrastructure only: default off, no provider/ engine contract change
// (Constitution II). The request keeps the current answer recipe (same messages,
// model, max_tokens, thinking-on) and adds only {stream:false, logprobs:true,
// top_logprobs:2}; the `temperature` field stays omitted (research.md Decision 2).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Frozen routing feature order (research.md Decision 3).
var utilityRoutingFeatureNames = []string{
	"final_mean_logprob",
	"final_p10_logprob",
	"final_mean_top1_top2_margin",
}

// utilityLogprobToken is one generated token's sanitized numeric trace.
type utilityLogprobToken struct {
	Token   string  // decoded token text (used only when bytes are missing)
	Bytes   []byte  // raw token bytes (authoritative for span mapping)
	Logprob float64 // sampled logprob
	Top1    float64 // highest top alternative logprob
	Top2    float64 // second-highest top alternative logprob; 0.0 == absent
}

// utilityCompletion is the parsed non-streaming response, label-blind.
type utilityCompletion struct {
	Content        string
	Usage          utilityAnswerUsage
	Tokens         []utilityLogprobToken
	ResponseDigest string
}

type utilityLogprobCallerConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	MaxModelLen int
	HTTPClient  *http.Client
	// Temperature optionally pins the request temperature. nil keeps the 042
	// frozen behavior (temperature omitted). 043 sets 0 to match the streaming
	// bench channel so the dual-channel pilot compares like-for-like decoding.
	Temperature *float64
}

type utilityLogprobCaller struct {
	endpoint     string
	apiKey       string
	model        string
	maxTokens    int
	maxModelLen  int
	client       *http.Client
	temperature  *float64
}

func utilityNewLogprobCaller(cfg utilityLogprobCallerConfig) (*utilityLogprobCaller, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("utility logprob caller requires base URL and model")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8000
	}
	maxModelLen := cfg.MaxModelLen
	if maxModelLen <= 0 {
		maxModelLen = utilityMaxModelLen
	}
	return &utilityLogprobCaller{
		endpoint:    strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions",
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		maxTokens:   maxTokens,
		maxModelLen: maxModelLen,
		client:      cfg.HTTPClient,
		temperature: cfg.Temperature,
	}, nil
}

// logprobResponse is the non-streaming response shape.
type logprobResponse struct {
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

// utilityClassifyHTTPStatus maps an HTTP status to a closed failure reason and
// whether it may be retried. OK returns ("", false).
func utilityClassifyHTTPStatus(code int) (utilityFailureReason, bool) {
	switch {
	case code >= 200 && code < 300:
		return "", false
	case code == http.StatusTooManyRequests:
		return utilityFailureHTTP429, true
	case code >= 500:
		return utilityFailureHTTP5xx, true
	default:
		return utilityFailureHTTP4xx, false
	}
}

// complete performs one logical call with at most three provider attempts. Only
// timeout / network / HTTP 429 / HTTP 5xx retry; everything else is terminal.
func (c *utilityLogprobCaller) complete(ctx context.Context, system, user string) (utilityCompletion, error) {
	var lastErr error
	for attempt := 1; attempt <= utilityMaxAttempts; attempt++ {
		completion, retryable, err := c.attempt(ctx, system, user)
		if err == nil {
			return completion, nil
		}
		lastErr = err
		if !retryable || attempt == utilityMaxAttempts {
			return utilityCompletion{}, err
		}
	}
	return utilityCompletion{}, lastErr
}

// attempt performs one HTTP round trip. The returned bool is the retryable flag
// derived from the closed failure reason.
func (c *utilityLogprobCaller) attempt(ctx context.Context, system, user string) (utilityCompletion, bool, error) {
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
		// `temperature` intentionally omitted by default
		// (temperature_request_mode=omitted); a non-nil c.temperature pins it.
	}
	if c.temperature != nil {
		body["temperature"] = *c.temperature
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return utilityCompletion{}, false, fmt.Errorf("utility encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return utilityCompletion{}, false, fmt.Errorf("utility request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	started := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return utilityCompletion{}, false, fmt.Errorf("utility call canceled: %w", ctx.Err())
		}
		return utilityCompletion{}, true, fmt.Errorf("utility network error")
	}
	defer resp.Body.Close() //nolint:errcheck

	reason, retryable := utilityClassifyHTTPStatus(resp.StatusCode)
	if reason != "" {
		// Drain only up to a small bound; never persist the raw body.
		_, _ = io.CopyN(io.Discard, resp.Body, 4*1024)
		return utilityCompletion{}, retryable, fmt.Errorf("utility status %s", reason)
	}

	// 64 MiB hard body limit: read to limit+1, exceeding means response_too_large.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, utilityResponseLimit+1))
	if err != nil {
		return utilityCompletion{}, false, fmt.Errorf("utility read: %w", err)
	}
	if len(raw) > utilityResponseLimit {
		return utilityCompletion{}, false, fmt.Errorf("utility response too large (>%d bytes)", utilityResponseLimit)
	}
	_ = started

	var out logprobResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return utilityCompletion{}, false, fmt.Errorf("utility decode error")
	}
	if len(out.Choices) == 0 {
		return utilityCompletion{}, false, fmt.Errorf("utility empty choice")
	}
	completion := utilityCompletion{
		Content:        out.Choices[0].Message.Content,
		ResponseDigest: utilityEndpointDigest(fmt.Sprintf("response:%d", len(raw))),
	}
	if out.Usage != nil {
		completion.Usage = utilityAnswerUsage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}
		if completion.Usage.InputTokens < 0 || completion.Usage.OutputTokens < 0 {
			return utilityCompletion{}, false, fmt.Errorf("utility invalid usage")
		}
	}
	if lp := out.Choices[0].Logprobs; lp != nil {
		for _, tok := range lp.Content {
			bt := utilityTokenBytes(tok.Bytes, tok.Token)
			top1, top2 := utilityTopAlternatives(tok.TopLogprobs)
			completion.Tokens = append(completion.Tokens, utilityLogprobToken{
				Token:   tok.Token,
				Bytes:   bt,
				Logprob: tok.Logprob,
				Top1:    top1,
				Top2:    top2,
			})
		}
	}
	return completion, false, nil
}

// utilityTokenBytes converts the response byte array to []byte, falling back to
// the decoded token string when bytes are missing (only used for mapping when it
// re-encodes unambiguously; the strict mapper rejects ambiguity).
func utilityTokenBytes(raw []int, token string) []byte {
	if len(raw) == 0 {
		return []byte(token)
	}
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = byte(b)
	}
	return out
}

// utilityTopAlternatives sorts the top-logprobs alternatives by descending
// logprob and returns the two highest. Top2 is 0.0 when fewer than two
// alternatives are present (the strict mapper treats that as missing).
func utilityTopAlternatives(alts []struct {
	Token   string  `json:"token"`
	Bytes   []int   `json:"bytes"`
	Logprob float64 `json:"logprob"`
}) (float64, float64) {
	if len(alts) == 0 {
		return 0, 0
	}
	top := make([]float64, 0, len(alts))
	for _, a := range alts {
		top = append(top, a.Logprob)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(top)))
	if len(top) == 1 {
		return top[0], 0
	}
	return top[0], top[1]
}

// utilityMapFinalSignal applies the strict final-suffix span mapper
// (research.md Decision 3) over an already-parsed token trace. Any contract
// violation marks the record signal_unavailable with a closed reason; genuine
// shape errors return an error.
func utilityMapFinalSignal(content string, tokens []utilityLogprobToken) (utilityProbabilitySignal, error) {
	sig := utilityProbabilitySignal{Status: "unavailable"}
	unavailable := func(reason utilitySignalUnavailableReason) (utilityProbabilitySignal, error) {
		sig.Reason = string(reason)
		return sig, nil
	}

	// 1. Reconstruct generated bytes; content must be an exact suffix.
	var recon bytes.Buffer
	for _, tok := range tokens {
		recon.Write(tok.Bytes)
	}
	reconBytes := recon.Bytes()
	if !bytes.HasSuffix(reconBytes, []byte(content)) {
		return unavailable(utilitySigContentNotSuffix)
	}
	sig.ContentDigest = utilityEndpointDigest("content")
	sig.GeneratedTokenCount = len(tokens)
	sig.TokenTraceDigest = utilityEndpointDigest("trace")
	sig.FeatureNamesDigest = utilityEndpointDigest(strings.Join(utilityRoutingFeatureNames, "|"))

	// 2. Final answer from the clean extractor; must be a non-empty suffix.
	final := extractFinalAnswer(content)
	if final == "" {
		return unavailable(utilitySigEmptyFinal)
	}
	if !strings.HasSuffix(content, final) {
		return unavailable(utilitySigFinalNotSuffix)
	}
	finalStart := len(content) - len(final)
	finalEnd := len(content)
	sig.FinalText = final
	sig.FinalByteStart = finalStart
	sig.FinalByteEnd = finalEnd

	// 3. Final-span tokens must align exactly to token boundaries.
	startIdx, endIdx := -1, -1
	pos := 0
	for i := range tokens {
		start := pos
		end := pos + len(tokens[i].Bytes)
		if start == finalStart && startIdx < 0 {
			startIdx = i
		}
		if end == finalEnd && endIdx < 0 {
			endIdx = i + 1
		}
		pos = end
	}
	if startIdx < 0 || endIdx < 0 || startIdx >= endIdx || pos != finalEnd {
		return unavailable(utilitySigBoundaryInToken)
	}

	// 4. Validate and aggregate final-span probabilities.
	var sampled []float64
	var trace []utilityTokenTraceEntry
	marginSum := 0.0
	for _, tok := range tokens[startIdx:endIdx] {
		if math.IsNaN(tok.Logprob) || math.IsInf(tok.Logprob, 0) ||
			math.IsNaN(tok.Top1) || math.IsInf(tok.Top1, 0) ||
			math.IsNaN(tok.Top2) || math.IsInf(tok.Top2, 0) {
			return unavailable(utilitySigNonFinite)
		}
		if tok.Top2 == 0.0 {
			return unavailable(utilitySigMissingTop2)
		}
		sampled = append(sampled, tok.Logprob)
		marginSum += tok.Top1 - tok.Top2
		trace = append(trace, utilityTokenTraceEntry{
			ByteLen:        len(tok.Bytes),
			SampledLogprob: tok.Logprob,
			Top1Logprob:    tok.Top1,
			Top2Logprob:    tok.Top2,
		})
	}
	sig.FinalTokenCount = len(sampled)
	sig.FinalTrace = trace
	sig.FinalLengthStratum = utilityLengthStratum(len(sampled))

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

	sig.Features = []float64{mean, sorted[p10Idx], marginSum / float64(len(sampled))}
	if err := utilityFiniteFloats(sig.Features); err != nil {
		return unavailable(utilitySigNonFinite)
	}
	sig.Status = "available"
	sig.Reason = ""
	return sig, nil
}

// utilityLengthStratum buckets the final token count for stratified reporting.
func utilityLengthStratum(n int) string {
	switch {
	case n == 1:
		return "1"
	case n >= 2 && n <= 4:
		return "2-4"
	case n >= 5 && n <= 16:
		return "5-16"
	default:
		return "17+"
	}
}
