package main

import (
	"context"
	"errors"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

func mkResult(name, content string) memory.Result {
	return memory.Result{Name: name, Content: content}
}

func names(hits []memory.Result) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}

func TestParseSufficiency(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want sufficiencyResult
	}{
		{"exact", `{"tier":"EXACT","confidence":0.9,"missing":""}`, sufficiencyResult{Tier: "EXACT", Confidence: 0.9}},
		{"partial", `{"tier":"PARTIAL","confidence":0.3,"missing":"date of job change"}`, sufficiencyResult{Tier: "PARTIAL", Confidence: 0.3, Missing: "date of job change"}},
		{"prose_wrapped", `Sure! {"tier":"EXACT","confidence":0.95,"missing":""} hope that helps`, sufficiencyResult{Tier: "EXACT", Confidence: 0.95}},
		{"lowercase_tier", `{"tier":"partial","confidence":0.2,"missing":"x"}`, sufficiencyResult{Tier: "PARTIAL", Confidence: 0.2, Missing: "x"}},
		{"clamp_high_conf", `{"tier":"EXACT","confidence":1.5}`, sufficiencyResult{Tier: "EXACT", Confidence: 1.0}},
		{"unknown_tier_falls_back", `{"tier":"MAYBE","confidence":0.5}`, sufficiencyResult{Tier: "PARTIAL", Confidence: 0.5}},
		{"fallback_keyword_inferrable", `the evidence is inferrable`, sufficiencyResult{Tier: "INFERRABLE"}},
		{"fallback_default_partial", `garbage with no json at all`, sufficiencyResult{Tier: "PARTIAL"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseSufficiency(c.raw)
			if got.Tier != c.want.Tier {
				t.Errorf("tier: got %q want %q", got.Tier, c.want.Tier)
			}
			if got.Confidence != c.want.Confidence {
				t.Errorf("confidence: got %v want %v", got.Confidence, c.want.Confidence)
			}
			if c.want.Missing != "" && got.Missing != c.want.Missing {
				t.Errorf("missing: got %q want %q", got.Missing, c.want.Missing)
			}
		})
	}
}

func TestSufficient(t *testing.T) {
	if !sufficient(sufficiencyResult{Tier: "EXACT", Confidence: 0.1}, 2) {
		t.Error("EXACT must stop regardless of confidence")
	}
	if !sufficient(sufficiencyResult{Tier: "INFERRABLE", Confidence: 0.90}, 2) {
		t.Error("INFERRABLE 0.90 >= temporal 0.85 must stop")
	}
	if sufficient(sufficiencyResult{Tier: "INFERRABLE", Confidence: 0.72}, 2) {
		t.Error("INFERRABLE 0.72 < temporal 0.85 must NOT stop")
	}
	if !sufficient(sufficiencyResult{Tier: "INFERRABLE", Confidence: 0.72}, 1) {
		t.Error("INFERRABLE 0.72 >= general 0.70 must stop for non-temporal")
	}
	if sufficient(sufficiencyResult{Tier: "PARTIAL", Confidence: 0.99}, 2) {
		t.Error("PARTIAL must never stop")
	}
}

func TestIrisMerge(t *testing.T) {
	hits0 := []memory.Result{mkResult("a", "1"), mkResult("b", "2"), mkResult("c", "3"), mkResult("d", "4")}
	fresh := []memory.Result{mkResult("e", "5"), mkResult("a", "1"), mkResult("f", "6")} // a is a dup of round 0
	// topK=4 → reserve0=2 anchors (a,b); fresh fills e then f (a deduped); c,d displaced.
	got := irisMerge(hits0, fresh, 4)
	want := []string{"a", "b", "e", "f"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %v", len(got), len(want), names(got))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("pos %d: got %q want %q (full: %v)", i, got[i].Name, w, names(got))
		}
	}
}

func TestIrisMergeNoFresh(t *testing.T) {
	hits0 := []memory.Result{mkResult("a", "1"), mkResult("b", "2"), mkResult("c", "3")}
	got := irisMerge(hits0, nil, 3)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %v", len(got), len(want), names(got))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("pos %d: got %q want %q", i, got[i].Name, w)
		}
	}
}

func TestIrisMergeCapsAtTopK(t *testing.T) {
	hits0 := []memory.Result{mkResult("a", "1"), mkResult("b", "2"), mkResult("c", "3")}
	fresh := []memory.Result{mkResult("d", "4"), mkResult("e", "5")}
	got := irisMerge(hits0, fresh, 3) // topK=3 → never exceeds 3
	if len(got) > 3 {
		t.Fatalf("merge exceeded topK: %d (%v)", len(got), names(got))
	}
}

func TestDedupHits(t *testing.T) {
	hits := []memory.Result{mkResult("a", "1"), mkResult("a", "1"), mkResult("b", "2"), mkResult("a", "1")}
	got := dedupHits(hits)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %v", len(got), names(got))
	}
}

func TestRefineQuery(t *testing.T) {
	called := false
	fake := func(ctx context.Context, system, user string) (string, error) {
		called = true
		if system == "" || user == "" {
			t.Error("refine received empty prompt")
		}
		return `  "the date Alice changed jobs"  `, nil
	}
	q, err := refineQuery(context.Background(), fake, "When did Alice change careers?", "date of job change")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Error("refine did not call the model")
	}
	if want := "the date Alice changed jobs"; q != want {
		t.Errorf("got %q want %q", q, want)
	}
}

func TestRefineQueryErrorFallback(t *testing.T) {
	fake := func(ctx context.Context, system, user string) (string, error) { return "", errors.New("boom") }
	q, err := refineQuery(context.Background(), fake, "orig-query", "missing")
	// refineQuery propagates the caller error and returns the original query as
	// the fallback value; the IRIS loop checks err == nil before adopting q'.
	if err == nil {
		t.Error("want caller error propagated")
	}
	if q != "orig-query" {
		t.Errorf("want original query as fallback value, got %q", q)
	}
}

func TestRefineQueryEmptyFallback(t *testing.T) {
	fake := func(ctx context.Context, system, user string) (string, error) { return "   ", nil }
	q, err := refineQuery(context.Background(), fake, "orig", "m")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if q != "orig" {
		t.Errorf("empty refine should fall back to original, got %q", q)
	}
}

func TestEvalSufficiency(t *testing.T) {
	fake := func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		if user == "" {
			t.Error("empty user prompt")
		}
		return `{"tier":"PARTIAL","confidence":0.2,"missing":"the event date"}`, provider.Usage{}, nil
	}
	s, err := evalSufficiency(context.Background(), fake, "when?", []memory.Result{mkResult("a", "x")}, 2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.Tier != "PARTIAL" || s.Missing != "the event date" {
		t.Errorf("got %+v", s)
	}
}

func TestEvalSufficiencyFailSafe(t *testing.T) {
	fake := func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		return "", provider.Usage{}, errors.New("endpoint down")
	}
	s, err := evalSufficiency(context.Background(), fake, "q", nil, 2)
	if err == nil {
		t.Error("want error propagated from caller")
	}
	// Fail-safe tier must be EXACT so a broken eval never blocks answering.
	if s.Tier != "EXACT" {
		t.Errorf("fail-safe tier = %q, want EXACT", s.Tier)
	}
}
