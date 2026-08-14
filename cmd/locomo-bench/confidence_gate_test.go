package main

import (
	"testing"
)

func TestDetectHesitationDeterminism(t *testing.T) {
	// FR-002: the same generation text must always produce the identical
	// (signal, deepened) — no randomness, no model calls.
	texts := []string{
		"",
		"Berlin",
		"I'm not sure. Could be Berlin.",
		"<thinking>I'm not certain which one.</thinking>\nBerlin",
		"I don't know",
	}
	for _, text := range texts {
		first, firstDeep := detectHesitation(text, 3.0)
		for i := 0; i < 5; i++ {
			got, gotDeep := detectHesitation(text, 3.0)
			if got.Score != first.Score || got.Decision != first.Decision || gotDeep != firstDeep {
				t.Fatalf("detectHesitation(%q) not deterministic: first score=%v deep=%v, got score=%v deep=%v",
					text, first.Score, firstDeep, got.Score, gotDeep)
			}
		}
	}
}

func TestDetectHesitationRules(t *testing.T) {
	cases := []struct {
		name      string
		pred      string
		wantScore float64
		wantDeep  bool
		wantHits  []string // subset of signal names that must be present
	}{
		{
			name:      "empty generation",
			pred:      "",
			wantScore: 4, // idk_refusal(3) + empty_final(1)
			wantDeep:  true,
			wantHits:  []string{"idk_refusal", "empty_final"},
		},
		{
			name:      "explicit refusal",
			pred:      "I don't know",
			wantScore: 3, // idk_refusal
			wantDeep:  true,
			wantHits:  []string{"idk_refusal"},
		},
		{
			name:      "confident short answer",
			pred:      "Berlin",
			wantScore: 0,
			wantDeep:  false,
		},
		{
			name:      "not-mentioned refusal",
			pred:      "The information is not mentioned in the conversation.",
			wantScore: 3, // idk_refusal
			wantDeep:  true,
			wantHits:  []string{"idk_refusal"},
		},
		{
			name:      "strong uncertainty plus guess",
			pred:      "I'm not sure. Could be Berlin.",
			wantScore: 5, // strong(3) + mid(2)
			wantDeep:  true,
			wantHits:  []string{"strong_uncertainty", "mid_guess"},
		},
		{
			name:      "multi-candidate thinking",
			pred:      "Either Berlin or Paris, not sure.",
			wantScore: 3, // multi_candidate
			wantDeep:  true,
			wantHits:  []string{"multi_candidate"},
		},
		{
			name:      "guess below threshold",
			pred:      "Maybe May 2023",
			wantScore: 2, // mid_guess only — below the 3.0 deepen threshold
			wantDeep:  false,
		},
		{
			name:      "uncertainty lives in the thinking preamble",
			pred:      "<thinking>I'm not certain which one.</thinking>\nBerlin",
			wantScore: 3, // strong_uncertainty in the thinking segment
			wantDeep:  true,
			wantHits:  []string{"strong_uncertainty"},
		},
		{
			name:      "hedge alone stays below threshold",
			pred:      "I think Berlin",
			wantScore: 1, // weak_hedge
			wantDeep:  false,
		},
		{
			name:      "question-mark restatement",
			pred:      "Is it Berlin?",
			wantScore: 1, // question_mark
			wantDeep:  false,
			wantHits:  []string{"question_mark"},
		},
		{
			name:      "thinking block without closing marker runs on final only",
			pred:      "I think the answer is Berlin.",
			wantScore: 1, // weak_hedge
			wantDeep:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig, deepened := detectHesitation(tc.pred, 3.0)
			if sig.Score != tc.wantScore {
				t.Errorf("score = %v, want %v (hits=%v)", sig.Score, tc.wantScore, sig.Hits)
			}
			if deepened != tc.wantDeep {
				t.Errorf("deepened = %v, want %v", deepened, tc.wantDeep)
			}
			for _, name := range tc.wantHits {
				if !containsHit(sig, name) {
					t.Errorf("missing signal %q in hits: %v", name, sig.Hits)
				}
			}
		})
	}
}

func TestDetectHesitationThresholdTuning(t *testing.T) {
	// US3: a higher threshold makes the deepen decision more conservative.
	pred := "Maybe May 2023" // score 2 (mid_guess)
	if _, deep := detectHesitation(pred, 2.0); !deep {
		t.Error("score 2 should deepen at threshold 2.0")
	}
	if _, deep := detectHesitation(pred, 3.0); deep {
		t.Error("score 2 should not deepen at threshold 3.0")
	}
}

func TestDetectHesitationSignalLevelBanding(t *testing.T) {
	cases := []struct {
		score float64
		want  hesitancy
	}{
		{0, hesitConfident},
		{1, hesitConfident},
		{2, hesitConfident},
		{3, hesitWeak},
		{5, hesitWeak},
		{6, hesitStrong},
	}
	for _, tc := range cases {
		if got := bandHesitancy(tc.score); got != tc.want {
			t.Errorf("bandHesitancy(%v) = %v, want %v", tc.score, got, tc.want)
		}
	}
}
