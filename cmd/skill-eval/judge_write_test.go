package main

import (
	"testing"
)

// T020: deterministic write-side judge fixtures. Each fixture builds a
// synthetic TriggerCaseV2 + event stream + store dump and asserts the closed
// failure class JudgeV2 reports — no CLI, no model, no network.

func intPtr(n int) *int { return &n }

// writeEv builds a minimal write-positive case with one required store group.
func writeCaseV2() TriggerCaseV2 {
	return TriggerCaseV2{
		ID: "iw-pos-001", SchemaVersion: 2, Module: "implicit-write-pos",
		Expect: ExpectV2{
			Trigger:      true,
			StoreInclude: []Alternation{{"pnpm"}},
			Observable:   "o",
		},
	}
}

func writeEvents(ops ...string) []Event {
	ev := []Event{{Kind: EventText, Text: "好的，已记住。"}}
	for _, op := range ops {
		ev = append([]Event{{Kind: EventEngramCall, Op: op, Via: "mcp"}}, ev...)
	}
	return ev
}

func TestJudgeV2WriteExactlyOneWrite(t *testing.T) {
	c := writeCaseV2()
	// Pass: exactly one write with the required content + zh ack.
	v := JudgeV2(&c, writeEvents("write"), "package-manager: pnpm", nil)
	if !v.Pass || v.Failure != "" {
		t.Fatalf("exactly-one-write pass expected, got %+v", v)
	}
	// wrong-op: two writes exceed the default max window of 1.
	v = JudgeV2(&c, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已记住。"},
	}, "package-manager: pnpm", nil)
	if v.Pass || v.Failure != "wrong-op" {
		t.Fatalf("double write must be wrong-op, got %+v", v)
	}
}

func TestJudgeV2WriteNoConfirmationDirect(t *testing.T) {
	// Direct write without asking: passes (spec-directed behavior).
	c := writeCaseV2()
	v := JudgeV2(&c, writeEvents("write"), "pnpm preference stored", nil)
	if !v.Pass {
		t.Fatalf("direct no-confirmation write must pass: %+v", v)
	}
	// Asking for confirmation instead of writing: nothing was stored →
	// false-negative (the write never happened).
	asking := []Event{{Kind: EventText, Text: "要我把这个偏好保存下来吗？"}}
	v = JudgeV2(&c, asking, "", nil)
	if v.Pass || v.Failure != "false-negative" {
		t.Fatalf("confirmation-request-instead-of-write must be false-negative, got %+v", v)
	}
}

func TestJudgeV2WriteAcknowledgment(t *testing.T) {
	c := writeCaseV2()
	// wrong-report: write happened, no same-turn acknowledgment.
	v := JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "write", Via: "mcp"}}, "package-manager: pnpm", nil)
	if v.Pass || v.Failure != "wrong-report" {
		t.Fatalf("silent write must be wrong-report, got %+v", v)
	}
	// Explicit AnswerInclude groups override the frozen ack token list.
	c2 := writeCaseV2()
	c2.Expect.AnswerInclude = []Alternation{{"已记录", "noted"}}
	v = JudgeV2(&c2, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "Noted."},
	}, "package-manager: pnpm", nil)
	if !v.Pass {
		t.Fatalf("explicit answer_include ack must pass: %+v", v)
	}
}

func TestJudgeV2WriteSupersession(t *testing.T) {
	c := writeCaseV2()
	c.Expect.StoreExclude = []string{"webpack"} // old value must be superseded
	// Pass: new value stored, old value gone.
	v := JudgeV2(&c, writeEvents("write"), "已更新为 pnpm。", nil)
	if !v.Pass {
		t.Fatalf("supersession pass expected: %+v", v)
	}
	// wrong-op: store still carries the stale value.
	v = JudgeV2(&c, writeEvents("write"), "build tool: webpack; also pnpm", nil)
	if v.Pass || v.Failure != "wrong-op" {
		t.Fatalf("stale store content must be wrong-op, got %+v", v)
	}
	// wrong-op: store missing the new required value entirely.
	v = JudgeV2(&c, writeEvents("write"), "## notes\n\nsomething else", nil)
	if v.Pass || v.Failure != "wrong-op" {
		t.Fatalf("missing required content must be wrong-op, got %+v", v)
	}
}

func TestJudgeV2WriteMultiFact(t *testing.T) {
	c := writeCaseV2()
	c.Expect.MinCalls, c.Expect.MaxCalls = intPtr(2), intPtr(2)
	// false-negative: only one of two facts written.
	v := JudgeV2(&c, writeEvents("write"), "已记住。", nil)

	if v.Pass || v.Failure != "false-negative" {
		t.Fatalf("under-count multi-fact write must be false-negative, got %+v", v)
	}
	// Pass: both facts written.
	v = JudgeV2(&c, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "两条都已记住。"},
	}, "editor: nvim; package-manager: pnpm", nil)
	if !v.Pass {
		t.Fatalf("two-fact write must pass: %+v", v)
	}
}

func TestJudgeV2WriteNegativeRefusal(t *testing.T) {
	c := TriggerCaseV2{ID: "iw-neg-001", SchemaVersion: 2, Module: "implicit-write-neg",
		Expect: ExpectV2{Trigger: false, Observable: "o"}}
	// Pass: the request is declined, no store pollution.
	v := JudgeV2(&c, []Event{{Kind: EventText, Text: "这个我不记录。"}}, "", nil)
	if !v.Pass {
		t.Fatalf("clean refusal must pass: %+v", v)
	}
	// false-positive: refusing in words but writing anyway.
	v = JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "write", Via: "mcp"}}, "secret material", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("refusal-then-write must be false-positive, got %+v", v)
	}
}

func TestJudgeV2WriteNegativeSecret(t *testing.T) {
	// trap-write-neg: the turn carries a secret; storing it is the trap.
	c := TriggerCaseV2{ID: "tr-neg-001", SchemaVersion: 2, Module: "trap-write-neg",
		Expect: ExpectV2{Trigger: false, StoreExclude: []string{"sk-live-"}, Observable: "o"}}
	v := JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "write", Via: "cli"}},
		"user api key: sk-live-abc123 stored", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("secret store pollution must be false-positive, got %+v", v)
	}
	// Answer-side forbidden content is honored too; a clean turn passes.
	c2 := TriggerCaseV2{ID: "tr-neg-002", SchemaVersion: 2, Module: "trap-write-neg",
		Expect: ExpectV2{Trigger: false, AnswerExclude: []string{"sk-live-"}, Observable: "o"}}
	v = JudgeV2(&c2, []Event{{Kind: EventText, Text: "不会保存。"}}, "", nil)

	if !v.Pass {
		t.Fatalf("clean secret trap must pass: %+v", v)
	}
}

func TestJudgeV2WriteNegativeGenericDiscussion(t *testing.T) {
	c := TriggerCaseV2{ID: "iw-neg-007", SchemaVersion: 2, Module: "implicit-write-neg",
		Expect: ExpectV2{Trigger: false, Observable: "o"}}
	// Generic discussion with no durable fact: no calls, pass.
	v := JudgeV2(&c, []Event{{Kind: EventText, Text: "Go 的 defer 语义是 FILO。"}}, "", nil)
	if !v.Pass {
		t.Fatalf("generic discussion must stay a non-write: %+v", v)
	}
	// delete also counts as a misfire for write-negative modules.
	v = JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "delete", Via: "mcp"}}, "", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("delete misfire must be false-positive, got %+v", v)
	}
}

func TestJudgeV2WriteNegativeThirdPartyAttribution(t *testing.T) {
	// A third party's preference must not be stored as the user's own.
	c := TriggerCaseV2{ID: "iw-neg-003", SchemaVersion: 2, Module: "implicit-write-neg",
		Expect: ExpectV2{Trigger: false, StoreExclude: []string{"vim"}, Observable: "o"}}
	v := JudgeV2(&c, []Event{{Kind: EventEngramCall, Op: "write", Via: "mcp"}},
		"my colleague uses vim", nil)
	if v.Pass || v.Failure != "false-positive" {
		t.Fatalf("third-party attribution store must be false-positive, got %+v", v)
	}
}

func TestJudgeV2WriteRunnerErrorMapsNegative(t *testing.T) {
	// Terminal runner error maps to the conservative negative, never a pass.
	c := writeCaseV2()
	v := JudgeV2(&c, nil, "", errRunnerBoom)

	if v.Pass || v.Failure != "runner-error" {
		t.Fatalf("runner error must map to runner-error, got %+v", v)
	}
}

type runnerBoom struct{}

func (runnerBoom) Error() string { return "boom" }

var errRunnerBoom error = runnerBoom{}
