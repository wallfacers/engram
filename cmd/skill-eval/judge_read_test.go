package main

import (
	"testing"
)

// ---------------------------------------------------------------------------
// T024: deterministic read-side judge fixtures (synthetic events, no CLI).
// Search-before-answer, evidence-grounded output, honest not-found, stale
// fact safety, enumeration, read-negatives, and closed failure mapping.

func readCaseV2() TriggerCaseV2 {
	return TriggerCaseV2{
		ID: "ir-pos-001", SchemaVersion: 2, Module: "implicit-read-pos",
		Expect: ExpectV2{
			Trigger:       true,
			AnswerInclude: []Alternation{{"pnpm"}},
			Observable:    "o",
		},
	}
}

func searchEvents(answer string) []Event {
	return []Event{
		{Kind: EventEngramCall, Op: "search", Via: "mcp"},
		{Kind: EventText, Text: answer},
	}
}

func TestJudgeV2ReadSearchBeforeAnswer(t *testing.T) {
	c := readCaseV2()
	// Pass: searched, then answered with the remembered value.
	v := JudgeV2(&c, searchEvents("你平时用 pnpm。"), "", nil)
	if !v.Pass {
		t.Fatalf("search+grounded answer must pass: %+v", v)
	}
	// false-negative: answered from the environment without any memory call.
	v = JudgeV2(&c, []Event{{Kind: EventText, Text: "这个项目用的是 pnpm。"}}, "", nil)
	if v.Pass || v.Failure != "false-negative" {
		t.Fatalf("answer without search must be false-negative, got %+v", v)
	}
	// get and list count as retrieval calls too.
	v = JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "get", Via: "cli"}, {Kind: EventText, Text: "pnpm"}}, "", nil)
	if !v.Pass {
		t.Fatalf("get counts as retrieval: %+v", v)
	}
}

func TestJudgeV2ReadEvidenceGrounded(t *testing.T) {
	c := readCaseV2()
	// wrong-report: searched but the answer lacks the remembered value.
	v := JudgeV2(&c, searchEvents("项目依赖装好了。"), "", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("ungrounded answer must be wrong-report, got %+v", v)
	}
	// wrong-report: answer contradicts the store via answer_exclude.
	c2 := readCaseV2()
	c2.Expect.AnswerExclude = []string{"npm"}
	v = JudgeV2(&c2, searchEvents("你用的是 npm。"), "", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("contradicted answer must be wrong-report, got %+v", v)
	}
}

func TestJudgeV2ReadNotFoundHonest(t *testing.T) {
	c := readCaseV2()
	c.Expect.NotFound = true
	// Pass: empty store reported honestly.
	v := JudgeV2(&c, searchEvents("没有找到相关的记忆记录。"), "", nil)
	if !v.Pass {
		t.Fatalf("honest not-found must pass: %+v", v)
	}
	// wrong-report: nothing found but the answer invents a stored fact.
	v = JudgeV2(&c, searchEvents("你用的是 pnpm。"), "", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("invented answer over empty store must be wrong-report, got %+v", v)
	}
}

func TestJudgeV2ReadStaleSupersededSafe(t *testing.T) {
	// The store holds both an old and a superseding value; the answer must
	// carry the current one (store_include) — stale content alone is a miss.
	c := readCaseV2()
	c.Expect.AnswerInclude = []Alternation{{"pnpm"}}
	c.Expect.AnswerExclude = []string{"webpack"}
	v := JudgeV2(&c, searchEvents("以前用 webpack，现在改成 pnpm 了。"), "", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("stale-contaminated answer must be wrong-report, got %+v", v)
	}
	v = JudgeV2(&c, searchEvents("你现在的包管理器是 pnpm。"), "", nil)
	if !v.Pass {
		t.Fatalf("supersession-aware answer must pass: %+v", v)
	}
}

func TestJudgeV2ReadEnumeration(t *testing.T) {
	// list-based enumeration: retrieval happened and the answer carries the
	// enumerated items.
	c := readCaseV2()
	c.Expect.AnswerInclude = []Alternation{{"nvim", "editor"}}
	v := JudgeV2(&c, []Event{
		{Kind: EventEngramCall, Op: "list", Via: "mcp"},
		{Kind: EventText, Text: "你记录过两个编辑器偏好：nvim 和 vs code。"},
	}, "", nil)
	if !v.Pass {
		t.Fatalf("enumeration via list must pass: %+v", v)
	}
}

func TestJudgeV2ReadNegativeStaysMemoryFree(t *testing.T) {
	c := TriggerCaseV2{ID: "ir-neg-001", SchemaVersion: 2, Module: "implicit-read-neg",
		Expect: ExpectV2{Trigger: false, Observable: "o"}}
	// Pass: a generic technical question triggers no memory call.
	v := JudgeV2(&c, []Event{{Kind: EventText, Text: "Go 的 defer 语义是 FILO。"}}, "", nil)
	if !v.Pass {
		t.Fatalf("clean read-negative must pass: %+v", v)
	}
	// false-positive: hunting for a preference behind a generic topic.
	v = JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "search", Via: "mcp"}}, "", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("unsolicited search must be false-positive, got %+v", v)
	}
}
