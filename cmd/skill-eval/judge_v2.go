package main

import (
	"fmt"
	"strings"
)

// JudgeV2 is the deterministic v2 judge (four closed failure classes +
// terminal runner-error). Pure function of (case, events, storeDump, runErr):
// identical inputs always produce an identical verdict.
func JudgeV2(c *TriggerCaseV2, events []Event, storeDump string, runErr error) Verdict {
	v := Verdict{CaseID: c.ID}
	if runErr != nil {
		v.Failure = "runner-error"
		v.Detail = "terminal runner error: " + runErr.Error()
		return v
	}
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
		misfire := len(ops) > 0
		if strings.HasSuffix(c.Module, "-write-neg") {
			misfire = count("write") > 0 || count("delete") > 0
		}
		if misfire {
			v.Failure = "false-positive"
			v.Detail = fmt.Sprintf("expected no engram misfire, saw ops %v", ops)
			return v
		}
		if forbidden := matchAnyToken(storeDump, c.Expect.StoreExclude); forbidden != "" {
			v.Failure = "false-positive"
			v.Detail = fmt.Sprintf("forbidden content %q present in store after the turn", forbidden)
			return v
		}
		v.Pass = true
		return v
	}

	// Call-count window: min (default 1) .. max (default min).
	minCalls, maxCalls := 1, 1
	if c.Expect.MinCalls != nil {
		minCalls = *c.Expect.MinCalls
	}
	if c.Expect.MaxCalls != nil {
		maxCalls = *c.Expect.MaxCalls
	}

	switch {
	case strings.HasSuffix(c.Module, "-write-pos"):
		if count("write") < minCalls {
			v.Failure = "false-negative"
			v.Detail = "no engram write call observed"
			return v
		}
		if maxCalls > 0 && count("write") > maxCalls {
			v.Failure = "wrong-op"
			v.Detail = fmt.Sprintf("write called %d times (max %d)", count("write"), maxCalls)
			return v
		}
		for _, inc := range c.Expect.StoreInclude {
			if !matchAlternationGroups(storeDump, inc) {
				v.Failure = "wrong-op"
				v.Detail = fmt.Sprintf("store dump missing required content %v", inc)
				return v
			}
		}
		if forbidden := matchAnyToken(storeDump, c.Expect.StoreExclude); forbidden != "" {
			v.Failure = "wrong-op"
			v.Detail = fmt.Sprintf("store carries forbidden content %q", forbidden)
			return v
		}
		// Same-turn acknowledgment: explicit answer_include groups win; else
		// the frozen ack token list.
		if len(c.Expect.AnswerInclude) > 0 {
			for _, inc := range c.Expect.AnswerInclude {
				if !matchAlternationGroups(answer, inc) {
					v.Failure = "wrong-report"
					v.Detail = fmt.Sprintf("answer missing acknowledgment %v", inc)
					return v
				}
			}
		} else if !matchAny(answer, ackTokens) {
			v.Failure = "wrong-report"
			v.Detail = "write happened but same-turn acknowledgment not detected"
			return v
		}
		if forbidden := matchAnyToken(answer, c.Expect.AnswerExclude); forbidden != "" {
			v.Failure = "wrong-report"
			v.Detail = fmt.Sprintf("answer contains forbidden content %q", forbidden)
			return v
		}
		v.Pass = true
	case strings.HasSuffix(c.Module, "-read-pos"):
		if retrievals < minCalls {
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
			if !matchAlternationGroups(answer, inc) {
				v.Failure = "wrong-report"
				v.Detail = fmt.Sprintf("answer missing required content %v", inc)
				return v
			}
		}
		if forbidden := matchAnyToken(answer, c.Expect.AnswerExclude); forbidden != "" {
			v.Failure = "wrong-report"
			v.Detail = fmt.Sprintf("answer contains forbidden content %q", forbidden)
			return v
		}
		v.Pass = true
	case c.Module == "regression":
		if len(ops) == 0 {
			v.Failure = "false-negative"
			v.Detail = "explicit engram request produced no engram call"
			return v
		}
		v.Pass = true
	default:
		if retrievals+count("write") == 0 {
			v.Failure = "false-negative"
			return v
		}
		v.Pass = true
	}
	return v
}

// matchAlternationGroups evaluates v2 alternation groups: every group must
// have at least one candidate word present.
func matchAlternationGroups(haystack string, groups []string) bool {
	for _, word := range groups {
		if strings.Contains(strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(word))) {
			return true
		}
	}
	return false
}
