package main

import "testing"

func writeCase() Case {
	return Case{
		ID: "iw-pos-001", Module: "implicit-write-pos", Lang: "zh",
		Expect: Expect{Trigger: true, StoreInclude: []string{"OpenAPI"}, Acknowledge: true},
	}
}

func TestJudgeWritePos(t *testing.T) {
	base := writeCase()

	// false-negative: no calls at all.
	v := Judge(base, []Event{{Kind: EventText, Text: "好的,我帮你改。"}}, "")
	if v.Pass || v.Failure != "false-negative" {
		t.Errorf("no-write case: %+v", v)
	}

	// wrong-op: write happened but wrong content persisted.
	v = Judge(base, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已记住你的文档偏好。"},
	}, "# memories\n\nmarkdown preference\n")
	if v.Pass || v.Failure != "wrong-op" {
		t.Errorf("wrong-content case: %+v", v)
	}

	// wrong-report: correct write, no same-turn acknowledgment.
	v = Judge(base, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "Done."},
	}, "OpenAPI 3 preference stored")
	if v.Pass || v.Failure != "wrong-report" {
		t.Errorf("no-ack case: %+v", v)
	}

	// pass: case-insensitive store match + zh ack.
	v = Judge(base, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已保存:以后接口文档用 openapi 3。"},
	}, "## doc-pref\n\nUser wants OpenAPI 3.")
	if !v.Pass || v.Failure != "" {
		t.Errorf("pass case: %+v", v)
	}
}

func TestJudgeWriteNeg(t *testing.T) {
	c := Case{ID: "iw-neg-001", Module: "implicit-write-neg", Expect: Expect{Trigger: false}}
	if v := Judge(c, []Event{{Kind: EventText, Text: "好的"}}, ""); !v.Pass {
		t.Errorf("clean neg should pass: %+v", v)
	}
	v := Judge(c, []Event{{Kind: EventEngramCall, Op: "write", Via: "cli"}}, "")
	if v.Pass || v.Failure != "false-positive" {
		t.Errorf("false-positive neg: %+v", v)
	}
	// A read while declining to store (refusal case: verify nothing stale
	// exists) is careful behavior, not a misfire.
	v = Judge(c, []Event{
		{Kind: EventEngramCall, Op: "search", Via: "mcp"},
		{Kind: EventText, Text: "好的,不记;记忆里也没有旧条目需要清理。"},
	}, "")
	if !v.Pass {
		t.Errorf("search-only refusal handling should pass: %+v", v)
	}
	// Read-negative keeps the strict any-call rule.
	r := Case{ID: "ir-neg-001", Module: "implicit-read-neg", Expect: Expect{Trigger: false}}
	v = Judge(r, []Event{{Kind: EventEngramCall, Op: "search", Via: "mcp"}}, "")
	if v.Pass || v.Failure != "false-positive" {
		t.Errorf("read-neg search must stay a misfire: %+v", v)
	}
}

func readCase() Case {
	return Case{
		ID: "ir-pos-001", Module: "implicit-read-pos", Lang: "zh",
		Expect: Expect{Trigger: true, AnswerInclude: []string{"pnpm"}},
	}
}

func TestJudgeReadPos(t *testing.T) {
	base := readCase()
	if v := Judge(base, []Event{{Kind: EventText, Text: "npm?"}}, ""); v.Pass || v.Failure != "false-negative" {
		t.Errorf("no-search case: %+v", v)
	}
	v := Judge(base, []Event{
		{Kind: EventEngramCall, Op: "search", Via: "mcp"},
		{Kind: EventText, Text: "你用的是 pnpm。"},
	}, "")
	if !v.Pass {
		t.Errorf("read pass case: %+v", v)
	}
	v = Judge(base, []Event{
		{Kind: EventEngramCall, Op: "get", Via: "mcp"},
		{Kind: EventText, Text: "你用的是 npm。"},
	}, "")
	if v.Pass || v.Failure != "wrong-report" {
		t.Errorf("wrong answer: %+v", v)
	}

	// notfound case: search ran but answer invents.
	nf := Case{ID: "ir-pos-006", Module: "implicit-read-pos",
		Expect: Expect{Trigger: true, NotFound: true}}
	v = Judge(nf, []Event{
		{Kind: EventEngramCall, Op: "search", Via: "mcp"},
		{Kind: EventText, Text: "你用的是 Feedly。"},
	}, "")
	if v.Pass || v.Failure != "wrong-report" {
		t.Errorf("invented answer must fail: %+v", v)
	}
	v = Judge(nf, []Event{
		{Kind: EventEngramCall, Op: "search", Via: "mcp"},
		{Kind: EventText, Text: "我查了记忆,没有找到你存过 RSS 阅读器。"},
	}, "")
	if !v.Pass {
		t.Errorf("honest not-found must pass: %+v", v)
	}
}

func TestJudgeRegression(t *testing.T) {
	pos := Case{ID: "reg-001", Module: "regression", Expect: Expect{Trigger: true}}
	if v := Judge(pos, []Event{{Kind: EventEngramCall, Op: "list", Via: "mcp"}}, ""); !v.Pass {
		t.Errorf("regression pos with any op: %+v", v)
	}
	if v := Judge(pos, nil, ""); v.Pass || v.Failure != "false-negative" {
		t.Errorf("regression pos without op: %+v", v)
	}
}

func TestJudgeAckPhrasings(t *testing.T) {
	base := writeCase()
	// Natural update/record acknowledgments observed in real runs (f1):
	// "已更新记忆", "记入长期记忆" — write + store were correct, only the
	// ack token list was too narrow to see them.
	for _, ack := range []string{
		"已更新记忆:服务器换成 Debian 12。",
		"我已经把这条信息记入长期记忆:花生过敏。",
		"已记好了,以后按这个来。",
		"Noted — stored your OpenAPI preference.",
	} {
		v := Judge(base, []Event{
			{Kind: EventEngramCall, Op: "write", Via: "mcp"},
			{Kind: EventText, Text: ack},
		}, "OpenAPI 3 preference stored")
		if !v.Pass {
			t.Errorf("ack phrasing %q should pass, got %+v", ack, v)
		}
	}
}

func TestJudgeDeterministic(t *testing.T) {
	c := writeCase()
	events := []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已保存 OpenAPI 偏好"},
	}
	a := Judge(c, events, "OpenAPI stored")
	b := Judge(c, events, "OpenAPI stored")
	if a != b {
		t.Errorf("judge not deterministic: %+v vs %+v", a, b)
	}
}
