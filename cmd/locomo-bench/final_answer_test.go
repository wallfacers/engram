package main

import (
	"strings"
	"testing"
)

func TestExtractFinalAnswer(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "qwen think tag",
			in:   "I need to find the date of the festival.\n\nMemory 3: ...\nReady.\n</think>\n\nMarch 2023",
			want: "March 2023",
		},
		{
			name: "no leading tag",
			in:   "The user wants to know X.\nLooking at memories...\nReady.\n</think>\n\nPositive reinforcement training",
			want: "Positive reinforcement training",
		},
		{
			name: "thinking variant tag",
			in:   "Let me think.\n</thinking>\nThe answer is 42",
			want: "The answer is 42",
		},
		{
			name: "last closing tag wins",
			in:   "</think>\nnot the answer\n</think>\n\nreal answer",
			want: "real answer",
		},
		{
			name: "stray response label after tag",
			in:   "</think>\nresponse\n\nSeattle",
			want: "Seattle",
		},
		{
			name: "no thinking structure passthrough",
			in:   "Positive reinforcement training",
			want: "Positive reinforcement training",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "whitespace only",
			in:   "   \n\t ",
			want: "",
		},
		{
			name: "bracket reasoning tag",
			in:   "[reasoning]\nconsider A vs B\n[/reasoning]\nfinal: A",
			want: "final: A",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractFinalAnswer(tc.in); got != tc.want {
				t.Errorf("extractFinalAnswer(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildJudgePromptGradesFinalAnswer guards the single choke point every
// judge verdict flows through: PREDICTED ANSWER must carry the final answer,
// not the thinking preamble (candidate values / self-corrections leak noise
// into the verdict otherwise).
func TestBuildJudgePromptGradesFinalAnswer(t *testing.T) {
	pred := "The user wants to know the date.\nI will search.\n...\nReady.\n</think>\n\nMarch 2023"
	got := buildJudgePrompt("When?", "March 2023", pred)
	if !strings.Contains(got, "PREDICTED ANSWER: March 2023") {
		t.Errorf("judge prompt must contain clean final answer, got:\n%s", got)
	}
	if strings.Contains(got, "I will search") {
		t.Errorf("judge prompt leaked thinking preamble:\n%s", got)
	}
}

// TestBuildJudgePromptNonThinkingUntouched: a non-thinking model's completion
// must reach the judge byte-identical (identity transform).
func TestBuildJudgePromptNonThinkingUntouched(t *testing.T) {
	pred := "Seattle, Washington"
	got := buildJudgePrompt("Where?", "Seattle", pred)
	if !strings.Contains(got, "PREDICTED ANSWER: Seattle, Washington") {
		t.Errorf("non-thinking completion must pass through untouched, got:\n%s", got)
	}
}

// TestAnswerFocusPromptGated: --answer-focus-prompt must be a pure opt-in.
// Default path is byte-identical; the focus suffix lands only on the generic
// single-hop prompt, never on multi-hop enumeration or open-domain inference.
func TestAnswerFocusPromptGated(t *testing.T) {
	// default (flag off): generic single-hop prompt unchanged
	base := answerPromptForRegime(4, false, false, false, false)
	if base != answerSystemPrompt {
		t.Fatal("default single-hop prompt must stay byte-identical")
	}
	// focus on: generic single-hop gains the two rules
	focused := answerPromptForRegime(4, false, false, false, true)
	if focused == base {
		t.Fatal("focus must change the single-hop prompt")
	}
	if !strings.Contains(focused, "answer ONLY that requested fact") || !strings.Contains(focused, "Prefer exact names") {
		t.Errorf("focus suffix missing expected rules:\n%s", focused)
	}
	// focus must NOT touch multi-hop (enumeration) or open-domain (inference)
	if got := answerPromptForRegime(1, false, false, false, true); got != multiHopAnswerPrompt {
		t.Errorf("focus must not alter multi-hop prompt")
	}
	if got := answerPromptForRegime(3, false, false, false, true); got != openDomainAnswerPrompt {
		t.Errorf("focus must not alter open-domain prompt")
	}
	// force variant gets it too
	fForce := answerPromptForRegime(4, true, false, false, true)
	if fForce == forceAnswerSystemPrompt {
		t.Errorf("focus must also extend the force single-hop prompt")
	}
	if !strings.Contains(fForce, "answer ONLY that requested fact") {
		t.Errorf("force focus missing rules")
	}
}
