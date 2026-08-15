package main

// 045 reverify_042 permanent tests. The final-span goldens below were
// generated against 043's deepenFinalSpanSignal (commit 1eb9cdd) with a
// throwaway cross-check that asserted rv ≡ deepen on every trace BEFORE the
// goldens were frozen (2026-08-16); carrying them as data keeps the
// equivalence regression alive after 044 deletes confidence_deepen*.go.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rvFixture struct {
	Name     string          `json:"name"`
	Tokens   []rvLogprobToken `json:"tokens"`
	Features []float64       `json:"features"`
	Avail    bool            `json:"available"`
	Reason   string          `json:"reason"`
}

// rvFixturesJSON is the frozen equivalence battery (7 traces).
const rvFixturesJSON = `[{"name":"inline-thinking-plus-special","tokens":[{"Token":"<think>","Bytes":"PHRoaW5rPg==","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2},{"Token":" the","Bytes":"IHRoZQ==","Logprob":-0.2,"Top1":-0.9,"Top2":-1.3},{"Token":" user","Bytes":"IHVzZXI=","Logprob":-0.3,"Top1":-1,"Top2":-1.4},{"Token":" asked","Bytes":"IGFza2Vk","Logprob":-0.15,"Top1":-1.2,"Top2":-1.5},{"Token":"</think>","Bytes":"PC90aGluaz4=","Logprob":-0.05,"Top1":-0.7,"Top2":-1.6},{"Token":" Paris","Bytes":"IFBhcmlz","Logprob":-0.02,"Top1":-0.4,"Top2":-1.7},{"Token":".","Bytes":"Lg==","Logprob":-0.01,"Top1":-0.5,"Top2":-1.8},{"Token":"<|im_end|>","Bytes":"PHxpbV9lbmR8Pg==","Logprob":-0.001,"Top1":-0.2,"Top2":-1.9}],"features":[-0.015,-0.02,1.2999999999999998],"available":true},{"name":"no-thinking","tokens":[{"Token":" Yes","Bytes":"IFllcw==","Logprob":-0.2,"Top1":-0.8,"Top2":-1.1},{"Token":".","Bytes":"Lg==","Logprob":-0.1,"Top1":-0.6,"Top2":-1.2}],"features":[-0.15000000000000002,-0.2,0.45],"available":true},{"name":"two-close-delims-last-wins","tokens":[{"Token":"</think>","Bytes":"PC90aGluaz4=","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2},{"Token":" mid","Bytes":"IG1pZA==","Logprob":-0.2,"Top1":-0.9,"Top2":-1.3},{"Token":"</think>","Bytes":"PC90aGluaz4=","Logprob":-0.05,"Top1":-0.7,"Top2":-1.4},{"Token":" final","Bytes":"IGZpbmFs","Logprob":-0.02,"Top1":-0.4,"Top2":-1.5}],"features":[-0.02,-0.02,1.1],"available":true},{"name":"specials-interspersed","tokens":[{"Token":"<|im_start|>","Bytes":"PHxpbV9zdGFydHw+","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2},{"Token":" answer","Bytes":"IGFuc3dlcg==","Logprob":-0.2,"Top1":-0.9,"Top2":-1.3},{"Token":"<|endoftext|>","Bytes":"PHxlbmRvZnRleHR8Pg==","Logprob":-0.001,"Top1":-0.2,"Top2":-1.4},{"Token":" here","Bytes":"IGhlcmU=","Logprob":-0.03,"Top1":-0.4,"Top2":-1.5},{"Token":"<|im_end|>","Bytes":"PHxpbV9lbmR8Pg==","Logprob":-0.001,"Top1":-0.2,"Top2":-1.6}],"features":[-0.115,-0.2,0.75],"available":true},{"name":"missing-top2-in-final-span","tokens":[{"Token":"</think>","Bytes":"PC90aGluaz4=","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2},{"Token":" x","Bytes":"IHg=","Logprob":-0.2,"Top1":-0.9,"Top2":0}],"available":false,"reason":"missing_top2"},{"name":"empty-final-span","tokens":[{"Token":" pre","Bytes":"IHByZQ==","Logprob":-0.2,"Top1":-0.9,"Top2":-1.3},{"Token":"</think>","Bytes":"PC90aGluaz4=","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2}],"available":false,"reason":"empty_final_span"},{"name":"delim-straddles-token","tokens":[{"Token":" think</think>","Bytes":"IHRoaW5rPC90aGluaz4=","Logprob":-0.1,"Top1":-1.1,"Top2":-1.2},{"Token":" tail","Bytes":"IHRhaWw=","Logprob":-0.02,"Top1":-0.4,"Top2":-1.5}],"features":[-0.02,-0.02,1.1],"available":true}]`

func rvLoadFixtures(t *testing.T) []rvFixture {
	t.Helper()
	var fixtures []rvFixture
	if err := json.Unmarshal([]byte(rvFixturesJSON), &fixtures); err != nil {
		t.Fatalf("fixtures JSON: %v", err)
	}
	if len(fixtures) != 7 {
		t.Fatalf("want 7 fixtures, got %d", len(fixtures))
	}
	return fixtures
}

func TestRvFinalSpanSignalGoldens(t *testing.T) {
	for _, fx := range rvLoadFixtures(t) {
		feats, ok, reason := rvFinalSpanSignal(fx.Tokens)
		if ok != fx.Avail {
			t.Errorf("%s: availability = %v, want %v (reason %q)", fx.Name, ok, fx.Avail, reason)
			continue
		}
		if !fx.Avail {
			if reason != fx.Reason {
				t.Errorf("%s: reason = %q, want %q", fx.Name, reason, fx.Reason)
			}
			continue
		}
		if len(feats) != len(fx.Features) {
			t.Fatalf("%s: %d features, want %d", fx.Name, len(feats), len(fx.Features))
		}
		for i := range feats {
			if math.Abs(feats[i]-fx.Features[i]) > 1e-12 {
				t.Errorf("%s: feature[%d] = %v, want %v", fx.Name, i, feats[i], fx.Features[i])
			}
		}
	}
}

// TestRvCallerContract pins the wire contract with a stub server: temperature
// MUST be 0 (the 042 bug), stream false, logprobs on, top_logprobs 2, and the
// Top1/Top2 selection orders alternatives by descending logprob.
func TestRvCallerContract(t *testing.T) {
	var sawTemp json.Number
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("request JSON: %v", err)
		}
		if v, ok := req["temperature"].(float64); !ok || v != 0 {
			t.Errorf("temperature = %v (ok=%v), want 0 — the 042 omission is measurement bug #1", req["temperature"], ok)
		}
		sawTemp = json.Number("seen")
		if req["stream"] != false {
			t.Errorf("stream = %v, want false", req["stream"])
		}
		if req["logprobs"] != true {
			t.Errorf("logprobs = %v, want true", req["logprobs"])
		}
		if req["top_logprobs"] != float64(2) {
			t.Errorf("top_logprobs = %v, want 2", req["top_logprobs"])
		}
		resp := `{"choices":[{"message":{"content":"A."},"logprobs":{"content":[{"token":"A","bytes":[65],"logprob":-0.1,"top_logprobs":[{"token":"A","bytes":[65],"logprob":-0.1},{"token":"B","bytes":[66],"logprob":-2.5}]},{"token":".","bytes":[46],"logprob":-0.3,"top_logprobs":[{"token":"!","bytes":[33],"logprob":-1.9},{"token":".","bytes":[46],"logprob":-0.3}]}]}}],"usage":{"prompt_tokens":100,"completion_tokens":2}}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	defer srv.Close()

	caller, err := rvNewCaller(srv.URL, "", "test-model", 512, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	completion, err := caller.complete(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatal(err)
	}
	if _ = sawTemp; completion.Content != "A." {
		t.Errorf("content = %q, want %q", completion.Content, "A.")
	}
	if len(completion.Tokens) != 2 {
		t.Fatalf("tokens = %d, want 2", len(completion.Tokens))
	}
	// Token 0: alternatives sorted desc → top1=-0.1 (sampled), top2=-2.5.
	if completion.Tokens[0].Top1 != -0.1 || completion.Tokens[0].Top2 != -2.5 {
		t.Errorf("token0 top1/top2 = %v/%v, want -0.1/-2.5", completion.Tokens[0].Top1, completion.Tokens[0].Top2)
	}
	// Token 1: response lists ! before . — caller must order by logprob.
	if completion.Tokens[1].Top1 != -0.3 || completion.Tokens[1].Top2 != -1.9 {
		t.Errorf("token1 top1/top2 = %v/%v, want -0.3/-1.9", completion.Tokens[1].Top1, completion.Tokens[1].Top2)
	}
	if completion.InputTokens != 100 || completion.OutputTokns != 2 {
		t.Errorf("usage = %d/%d, want 100/2", completion.InputTokens, completion.OutputTokns)
	}
}

func TestRvCallerRetriesOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"logprobs":null}],"usage":null}`)
	}))
	defer srv.Close()
	caller, err := rvNewCaller(srv.URL, "", "m", 64, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("want success on 3rd attempt, got %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRvCallerTerminalOn400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	caller, _ := rvNewCaller(srv.URL, "", "m", 64, srv.Client())
	_, err := caller.complete(context.Background(), "s", "u")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want terminal 400 error, got %v", err)
	}
}

func TestRvAUCMatchesManualWMW(t *testing.T) {
	pos := []float64{0.9, 0.8, 0.3}
	neg := []float64{0.4, 0.1}
	auc, err := rvAUC(pos, neg)
	if err != nil {
		t.Fatal(err)
	}
	// Pairs: (0.9>0.4, 0.9>0.1)=2; (0.8>0.4, 0.8>0.1)=2; (0.3<0.4, 0.3>0.1)=1 → 5/6.
	if math.Abs(auc-5.0/6.0) > 1e-12 {
		t.Errorf("auc = %v, want 5/6", auc)
	}
	// Tie credit: pairs (0.5,0.5)=0.5, (0.5,0.1)=1, (0.2,0.5)=0, (0.2,0.1)=1 → 2.5/4.
	pos2 := []float64{0.5, 0.2}
	neg2 := []float64{0.5, 0.1}
	auc2, _ := rvAUC(pos2, neg2)
	if math.Abs(auc2-0.625) > 1e-12 {
		t.Errorf("tie auc = %v, want 0.625", auc2)
	}
}

func TestRvAUCBootstrapDeterministic(t *testing.T) {
	pos := []float64{0.9, 0.8, 0.7, 0.6, 0.2}
	neg := []float64{0.5, 0.4, 0.3, 0.1, 0.15}
	lo1, hi1, err := rvAUCBootstrap(pos, neg, 43, 200)
	if err != nil {
		t.Fatal(err)
	}
	lo2, hi2, _ := rvAUCBootstrap(pos, neg, 43, 200)
	if lo1 != lo2 || hi1 != hi2 {
		t.Errorf("bootstrap not deterministic: (%v,%v) vs (%v,%v)", lo1, hi1, lo2, hi2)
	}
	if lo1 > hi1 {
		t.Errorf("lo > hi: %v > %v", lo1, hi1)
	}
}

// TestRvTokenBytesRoundTrip guards the base64 fixture encoding path.
func TestRvTokenBytesRoundTrip(t *testing.T) {
	b, err := json.Marshal(rvLogprobToken{Token: "x", Bytes: []byte(" tail")})
	if err != nil {
		t.Fatal(err)
	}
	var back rvLogprobToken
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if string(back.Bytes) != " tail" {
		t.Errorf("bytes round trip = %q", string(back.Bytes))
	}
	if _, err := base64.StdEncoding.DecodeString("IHRhaWw="); err != nil {
		t.Fatal(err)
	}
}
