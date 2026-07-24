package main

import (
	"strings"
	"testing"
)

// TestTemporalContractAnchors pins the strengthened temporal reasoning contract
// (feature 014): the category-2 force+temporal prompt MUST carry all four
// reasoning anchors that target the three diagnosed answer-side failure modes
// (±1 misattribution / relative→absolute / duration arithmetic) plus the
// enumerate-first common cause, and keep the terminal constraints.
// See specs/014-temporal-answer-contract/contracts/temporal-answer-contract.md C1.
func TestTemporalContractAnchors(t *testing.T) {
	got := answerPromptForRegime(2, true, true, false)
	if got != forceTemporalAnswerPrompt {
		t.Fatalf("category-2 force+temporal did not route to forceTemporalAnswerPrompt")
	}
	low := strings.ToLower(got)

	// Each anchor is a set of stable tokens that must co-occur (case-insensitive).
	anchors := []struct {
		name   string
		tokens []string
	}{
		{"enumerate", []string{"enumerate", "[event:"}},
		{"relative→absolute", []string{"relative", "anchor", "never echo"}},
		{"exact-match (anti ±1)", []string{"exact match", "different event"}},
		{"duration arithmetic", []string{"duration", "start and the end"}},
		{"terminal constraints", []string{"never iso", "never decline"}},
	}
	for _, a := range anchors {
		for _, tok := range a.tokens {
			if !strings.Contains(low, tok) {
				t.Errorf("temporal contract missing anchor %q token %q\n---\n%s", a.name, tok, got)
			}
		}
	}
}

// TestTemporalContractInvariants guards feature 014's non-goals: strengthening
// the category-2 contract MUST NOT change any other routing decision (FR-007).
// Complements bench_test.go's opt-in/abstain tests with a feature-local guard.
func TestTemporalContractInvariants(t *testing.T) {
	// Non-temporal categories keep their own force prompts.
	if answerPromptForRegime(1, true, true, false) != forceMultiHopAnswerPrompt {
		t.Error("category-1 force prompt changed")
	}
	if answerPromptForRegime(3, true, true, false) != forceOpenDomainAnswerPrompt {
		t.Error("category-3 force prompt changed")
	}
	// Temporal switch OFF: category-2 falls back to the generic force prompt.
	if answerPromptForRegime(2, true, false, false) != forceAnswerSystemPrompt {
		t.Error("temporal switch OFF must fall back to forceAnswerSystemPrompt")
	}
	// Abstain takes precedence over the temporal contract.
	if answerPromptForRegime(2, true, true, true) != abstainAnswerPrompt {
		t.Error("abstain must override the temporal contract")
	}
}
