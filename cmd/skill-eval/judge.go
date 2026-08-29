package main

import (
	"fmt"
	"strings"
)

// Verdict is one case's deterministic judgement.
type Verdict struct {
	CaseID  string `json:"case_id"`
	Tool    string `json:"tool"`
	Pass    bool   `json:"pass"`
	Failure string `json:"failure,omitempty"` // false-negative | false-positive | wrong-op | wrong-report | failed
	Detail  string `json:"detail,omitempty"`
}

// ackTokens: same-turn acknowledgment phrasings (write cases).
var ackTokens = []string{
	"已记", "已保存", "已存", "记下", "记住", "存入", "写入", "已录入", "帮你记",
	"已更新", "更新记忆", "记入", "记好", "记到",
	"saved", "stored", "recorded", "noted", "updated", "added to memory", "wrote to memory",
}

// notFoundTokens: honest not-found phrasings (read cases with empty stores).
var notFoundTokens = []string{
	"没有", "未找到", "没找到", "没记录", "未存储", "没存", "没有找到", "查不到", "不曾",
	"not found", "no stored", "nothing stored", "couldn't find", "no record",
	"don't have", "nothing in", "no memory of", "haven't stored",
}

// matchAlternation reports whether any |-separated alternative appears in the
// haystack (case-insensitive for ASCII).
func matchAlternation(haystack, alternation string) bool {
	h := strings.ToLower(haystack)
	for _, alt := range strings.Split(alternation, "|") {
		if strings.Contains(h, strings.ToLower(strings.TrimSpace(alt))) {
			return true
		}
	}
	return false
}

func matchAny(haystack string, tokens []string) bool {
	for _, t := range tokens {
		if strings.Contains(strings.ToLower(haystack), strings.ToLower(t)) {
			return true
		}
	}
	return false
}

// Judge evaluates one case from its normalized events and the store dump
// (CLI `list` output taken after the turn; empty for read-only verification).
// The judgement is a pure function of its inputs — same inputs, same verdict.
func Judge(c Case, events []Event, storeDump string) Verdict {
	v := Verdict{CaseID: c.ID}
	var ops []string
	var textParts []string
	for _, e := range events {
		if e.Kind == EventEngramCall {
			ops = append(ops, e.Op)
		} else if e.Kind == EventText {
			textParts = append(textParts, e.Text)
		}
	}
	answer := strings.Join(textParts, "\n")
	count := func(op string) int {
		n := 0
		for _, o := range ops {
			if o == op {
				n++
			}
		}
		return n
	}
	retrievals := count("search") + count("get") + count("list")

	if !c.Expect.Trigger {
		// Write-negative cases: only a mutating engram op (write/delete) is a
		// misfire — a search performed while declining to store ("别记" → the
		// agent verifies nothing stale exists) is careful behavior, not a
		// violation. Read-negative cases keep the strict any-call rule: for a
		// memory-independent question even a search is an over-trigger.
		misfire := len(ops) > 0
		if c.Module == "implicit-write-neg" {
			misfire = count("write") > 0 || count("delete") > 0
		}
		if misfire {
			v.Failure = "false-positive"
			v.Detail = fmt.Sprintf("expected no engram misfire, saw ops %v", ops)
		} else {
			v.Pass = true
		}
		return v
	}

	// Positive cases.
	switch c.Module {
	case "implicit-write-pos":
		switch {
		case count("write") == 0:
			v.Failure = "false-negative"
			v.Detail = "no engram write call observed"
			return v
		}
		for _, inc := range c.Expect.StoreInclude {
			if !matchAlternation(storeDump, inc) {
				v.Failure = "wrong-op"
				v.Detail = fmt.Sprintf("store dump missing required content %q", inc)
				return v
			}
		}
		if c.Expect.Acknowledge && !matchAny(answer, ackTokens) {
			v.Failure = "wrong-report"
			v.Detail = "write happened but same-turn acknowledgment not detected"
			return v
		}
		v.Pass = true
	case "implicit-read-pos":
		if retrievals == 0 {
			v.Failure = "false-negative"
			v.Detail = "no engram search/get/list call observed"
			return v
		}
		if c.Expect.NotFound {
			if !matchAny(answer, notFoundTokens) {
				v.Failure = "wrong-report"
				v.Detail = "empty result not honestly reported (no not-found phrasing)"
			} else {
				v.Pass = true
			}
			return v
		}
		for _, inc := range c.Expect.AnswerInclude {
			if !matchAlternation(answer, inc) {
				v.Failure = "wrong-report"
				v.Detail = fmt.Sprintf("answer missing required content %q", inc)
				return v
			}
		}
		v.Pass = true
	case "regression":
		// 020 legacy trigger cases: an engram operation of any kind counts as
		// the skill having fired; negative cases are handled above.
		if len(ops) == 0 {
			v.Failure = "false-negative"
			v.Detail = "explicit engram request produced no engram call"
			return v
		}
		v.Pass = true
	default:
		// Unknown positive module — treat as retrieval-style expectation.
		if retrievals+count("write") == 0 {
			v.Failure = "false-negative"
			return v
		}
		v.Pass = true
	}
	return v
}
