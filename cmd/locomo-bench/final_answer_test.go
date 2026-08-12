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
