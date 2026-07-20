package prompt

import (
	"strings"
	"testing"
)

func TestCurationJudgeClusterRendersMemberContentAndTime(t *testing.T) {
	clusters := []CurationJudgeCluster{{
		Names: []string{"home-a", "home-b"},
		Members: []CurationJudgeClusterMember{
			{Name: "home-a", Content: "the user lives in Paris", When: "2023-01-05"},
			{Name: "home-b", Content: "the user lives in Berlin", When: "2023-08-20"},
		},
	}}
	out := BuildCurationJudgeUserPrompt(nil, clusters)
	// The judge must see each member's content and time to tell a contradiction
	// from a compatible duplicate and to pick the newer winner.
	for _, want := range []string{"home-a", "home-b", "lives in Paris", "lives in Berlin", "2023-08-20"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cluster prompt missing %q:\n%s", want, out)
		}
	}
}

func TestCurationJudgeClusterFallsBackToNames(t *testing.T) {
	// A cluster with no Members (legacy callers) still renders its names.
	out := BuildCurationJudgeUserPrompt(nil, []CurationJudgeCluster{{Names: []string{"x", "y"}}})
	if !strings.Contains(out, "x") || !strings.Contains(out, "y") {
		t.Fatalf("names-only cluster lost its names:\n%s", out)
	}
}
