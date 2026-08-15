package main

// 045 answer-in-context tests. The normalization is FROZEN (plan R5); these
// tests pin it so no one "improves" matching to chase a gate number.

import (
	"math"
	"testing"
)

func TestAicNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Paris  ", "paris"},
		{"New\n\tYork   City", "new york city"},
		{"Eiffel Tower", "eiffel tower"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := aicNormalize(c.in); got != c.want {
			t.Errorf("aicNormalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAicMatch(t *testing.T) {
	ctx := "The user visited the Eiffel Tower in 2019 with Marie."
	cases := []struct {
		aliases []string
		want    string
		ok      bool
	}{
		{[]string{"Eiffel Tower"}, "Eiffel Tower", true},
		{[]string{"nope", "marie"}, "marie", true},   // any-alias, case-insensitive
		{[]string{"  eiffel   tower "}, "  eiffel   tower ", true}, // alias normalization
		{[]string{"2019"}, "2019", true},
		{[]string{"Berlin"}, "", false},
		{[]string{""}, "", false}, // empty alias never matches
	}
	for _, c := range cases {
		got, ok := aicMatch(ctx, c.aliases)
		if ok != c.ok || got != c.want {
			t.Errorf("aicMatch(aliases=%v) = (%q,%v), want (%q,%v)", c.aliases, got, ok, c.want, c.ok)
		}
	}
}

func TestAicMatchSubstringAcrossSpaces(t *testing.T) {
	// Multi-word gold must match as a contiguous normalized substring.
	ctx := "answer: Mount Rushmore National Memorial is located in South Dakota"
	if _, ok := aicMatch(ctx, []string{"Rushmore National"}); !ok {
		t.Error("contiguous multi-word alias must match")
	}
	if _, ok := aicMatch(ctx, []string{"Rushmore Memorial"}); ok {
		t.Error("non-contiguous words must NOT match (no bag-of-words)")
	}
}

func TestAicArmFrom(t *testing.T) {
	rows := []aicRow{
		{QuestionID: "q1", InContext: true},
		{QuestionID: "q2", InContext: false},
		{QuestionID: "q3", InContext: true, UnmatchableInPool: true}, // stays in denominator
		{QuestionID: "q4", InContext: false},
	}
	arm := aicArmFrom(rows, func(id string) float64 {
		switch id {
		case "q1", "q3":
			return 100
		default:
			return 50
		}
	})
	if arm.Total != 4 || arm.InContext != 2 {
		t.Errorf("arm counts = %d/%d, want in=2 total=4", arm.InContext, arm.Total)
	}
	if math.Abs(arm.AIC-0.5) > 1e-12 {
		t.Errorf("AIC = %v, want 0.5", arm.AIC)
	}
	if math.Abs(arm.TokensMean-75) > 1e-12 {
		t.Errorf("tokens mean = %v, want 75", arm.TokensMean)
	}
}
