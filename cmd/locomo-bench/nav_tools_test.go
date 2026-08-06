package main

import (
	"errors"
	"testing"
)

// navToolErrKind extracts the typed navToolError kind from an error, or fails
// the test if it is not a navToolError.
func navToolErrKind(t *testing.T, err error) navToolErrorKind {
	t.Helper()
	var nte *navToolError
	if !errors.As(err, &nte) {
		t.Fatalf("want *navToolError, got %T: %v", err, err)
	}
	return nte.Kind
}

func TestNavParseSearchValid(t *testing.T) {
	action, err := parseNavToolCall(`{"tool":"search","tool_args":{"query":"area hit by a flood","k":8},"rationale":"raw query first"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if action.Tool != "search" || action.Search == nil {
		t.Fatalf("want search action, got %#v", action)
	}
	if action.Search.Query != "area hit by a flood" || action.Search.K != 8 {
		t.Fatalf("search args: %#v", action.Search)
	}
	if action.Rationale != "raw query first" {
		t.Fatalf("rationale: %q", action.Rationale)
	}
}

func TestNavParseSearchDefaultsK(t *testing.T) {
	action, err := parseNavToolCall(`{"tool":"search","tool_args":{"query":"flood"}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if action.Search.K != navToolDefaultK {
		t.Fatalf("default k = %d, want %d", action.Search.K, navToolDefaultK)
	}
}

func TestNavParseExpandAndFollowValid(t *testing.T) {
	expand, err := parseNavToolCall(`{"tool":"expand_query","tool_args":{"text":"flood 2023 May","k":10},"rationale":"narrow by time"}`)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if expand.Tool != "expand_query" || expand.Expand == nil || expand.Expand.Text != "flood 2023 May" || expand.Expand.K != 10 {
		t.Fatalf("expand: %#v", expand)
	}
	follow, err := parseNavToolCall(`{"tool":"follow_entity","tool_args":{"entity":"Caroline","k":6}}`)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if follow.Tool != "follow_entity" || follow.Follow == nil || follow.Follow.Entity != "Caroline" || follow.Follow.K != 6 {
		t.Fatalf("follow: %#v", follow)
	}
}

func TestNavParseStopValid(t *testing.T) {
	action, err := parseNavToolCall(`{"tool":"stop","tool_args":{"evidence_ids":["c1","c2"],"assembly":"first_n"}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if action.Tool != "stop" || action.Stop == nil {
		t.Fatalf("want stop action, got %#v", action)
	}
	if len(action.Stop.EvidenceIDs) != 2 || action.Stop.EvidenceIDs[0] != "c1" || action.Stop.EvidenceIDs[1] != "c2" {
		t.Fatalf("stop evidence_ids: %#v", action.Stop.EvidenceIDs)
	}
	if action.Stop.Assembly != "first_n" {
		t.Fatalf("assembly: %q", action.Stop.Assembly)
	}
}

func TestNavParseStopDefaultsAssembly(t *testing.T) {
	action, err := parseNavToolCall(`{"tool":"stop","tool_args":{"evidence_ids":["c1"]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if action.Stop.Assembly != "first_n" {
		t.Fatalf("default assembly = %q, want first_n", action.Stop.Assembly)
	}
}

func TestNavParseUnknownTool(t *testing.T) {
	_, err := parseNavToolCall(`{"tool":"assess","tool_args":{},"rationale":"..."}`)
	if err == nil {
		t.Fatal("want error on unknown tool")
	}
	if k := navToolErrKind(t, err); k != navErrUnknownTool {
		t.Fatalf("kind = %v, want navErrUnknownTool", k)
	}
}

func TestNavParseMalformedJSON(t *testing.T) {
	for _, bad := range []string{"not json", "", "{", `{"tool":}`} {
		_, err := parseNavToolCall(bad)
		if err == nil {
			t.Fatalf("want error for %q", bad)
		}
		if k := navToolErrKind(t, err); k != navErrParse {
			t.Fatalf("kind for %q = %v, want navErrParse", bad, k)
		}
	}
}

func TestNavParseInvalidArgs(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"search empty query", `{"tool":"search","tool_args":{"query":"  "}}`},
		{"search k too large", `{"tool":"search","tool_args":{"query":"q","k":99}}`},
		{"expand empty text", `{"tool":"expand_query","tool_args":{"text":""}}`},
		{"follow empty entity", `{"tool":"follow_entity","tool_args":{"entity":""}}`},
		{"stop bad assembly", `{"tool":"stop","tool_args":{"evidence_ids":["c1"],"assembly":"chaos"}}`},
		{"search args not object", `{"tool":"search","tool_args":["q"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNavToolCall(tc.in)
			if err == nil {
				t.Fatal("want error")
			}
			if k := navToolErrKind(t, err); k != navErrInvalidArgs {
				t.Fatalf("kind = %v, want navErrInvalidArgs", k)
			}
		})
	}
}

func TestNavParseCodeFenceStrip(t *testing.T) {
	action, err := parseNavToolCall("```json\n{\"tool\":\"stop\",\"tool_args\":{\"evidence_ids\":[\"c1\"]}}\n```")
	if err != nil {
		t.Fatalf("parse fenced: %v", err)
	}
	if action.Tool != "stop" {
		t.Fatalf("tool = %q, want stop", action.Tool)
	}
}

// TestNavParseReasoningModelOutput: the sidecar is a reasoning model whose raw
// output interleaves chain-of-thought with the tool JSON. The parser must
// extract the first valid object from the mixed text.
func TestNavParseReasoningModelOutput(t *testing.T) {
	const mixed = `The user wants to know when Andrew adopted Scout.
I have seen no evidence yet.
Let's search.
{"tool":"search","tool_args":{"query":"Andrew adopted Scout","k":8},"rationale":"no evidence yet"}
Wait, the prompt says emit ONLY JSON.`
	action, err := parseNavToolCall(mixed)
	if err != nil {
		t.Fatalf("parse reasoning-model output: %v", err)
	}
	if action.Tool != "search" || action.Search == nil {
		t.Fatalf("want search action, got %#v", action)
	}
	if action.Search.Query != "Andrew adopted Scout" || action.Search.K != 8 {
		t.Fatalf("search args: %#v", action.Search)
	}
}

// TestNavParseReasoningModelOutputInvalid: reasoning text without any valid
// JSON object must still fail closed (navErrParse).
func TestNavParseReasoningModelOutputInvalid(t *testing.T) {
	const thinking = `Let me think about this question. The user asks about a flood. I need to search.`
	_, err := parseNavToolCall(thinking)
	if err == nil {
		t.Fatal("want error for reasoning text without JSON")
	}
	if k := navToolErrKind(t, err); k != navErrParse {
		t.Fatalf("kind = %v, want navErrParse", k)
	}
}

func TestNavParseNullArgsOK(t *testing.T) {
	action, err := parseNavToolCall(`{"tool":"stop","tool_args":null}`)
	if err != nil {
		t.Fatalf("parse null args: %v", err)
	}
	if action.Stop == nil || len(action.Stop.EvidenceIDs) != 0 {
		t.Fatalf("stop: %#v", action.Stop)
	}
}
