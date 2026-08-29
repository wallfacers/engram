package main

import (
	"strings"
	"testing"
)

func TestCliInvocation(t *testing.T) {
	cases := []struct {
		cmd  string
		want string // "" means no invocation detected
		ok   bool
	}{
		{`engram add --name x --content y`, "write", true},
		{`./engram --data-dir /tmp/d add --name x --content y`, "write", true},
		{`/home/user/bin/engram search "tea"`, "search", true},
		{`engram --data-dir /tmp/d list`, "list", true},
		{`cd repo && ./engram --data-dir /d search pnpm`, "search", true},
		{`engram-mcp --data-dir /d`, "", false},
		{`go build -o engram ./cmd/engram`, "", false},
		{`CGO_ENABLED=0 go build ./cmd/engram`, "", false},
		{`go run ./cmd/engram-mcp --data-dir /d version`, "", false},
		{`which engram || which engram-mcp`, "", false},
		{`ls -la engram bin/`, "", false},
		{`sed -n '1,240p' /home/u/.agents/skills/engram/SKILL.md`, "", false},
		{`mkdir -p /d && cd repo && ./engram version`, "other", true},
		{`./engram --data-dir /d delete --name old || true`, "delete", true},
		{`echo "engram add is a command"`, "", false}, // prose mention inside echo
		{`engram version 2>&1 || true`, "other", true},
	}
	for _, c := range cases {
		got, ok := cliInvocation(c.cmd)
		if got != c.want || ok != c.ok {
			t.Errorf("cliInvocation(%q) = (%q,%v), want (%q,%v)", c.cmd, got, ok, c.want, c.ok)
		}
	}
}

func TestParseClaude(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"system","subtype":"init_status"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Saving this."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__engram__memory_write","input":{"name":"tea","content":"jasmine"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__engram__memory_search","input":{"query":"tea"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"result","result":"stored: tea (jasmine)"}`,
	}, "\n")
	ev := ParseClaude(strings.NewReader(in))
	var writes, searches, texts int
	for _, e := range ev {
		switch {
		case e.Kind == EventEngramCall && e.Op == "write" && e.Via == "mcp":
			writes++
		case e.Kind == EventEngramCall && e.Op == "search":
			searches++
		case e.Kind == EventText:
			texts++
		}
	}
	if writes != 1 || searches != 1 || texts != 2 {
		t.Errorf("claude parse: writes=%d searches=%d texts=%d, events=%+v", writes, searches, texts, ev)
	}
}

func TestParseClaudeIgnoresNonEngramTools(t *testing.T) {
	in := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	ev := ParseClaude(strings.NewReader(in))
	if len(ev) != 0 {
		t.Errorf("non-engram tool_use produced events: %+v", ev)
	}
}

func TestParseCodex(t *testing.T) {
	in := strings.Join([]string{
		`Reading additional input from stdin...`,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"I'll write it."}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"/bin/bash -lc \"sed -n '1,240p' /home/u/.agents/skills/engram/SKILL.md\""}}`,
		`{"type":"item.completed","item":{"id":"i2","type":"command_execution","command":"/bin/bash -lc \"engram-mcp --data-dir /d version\""}}`,
		`{"type":"item.completed","item":{"id":"i3","type":"mcp_tool_call","server":"engram","tool":"memory_write","arguments":{}}}`,
		`{"type":"item.completed","item":{"id":"i4","type":"mcp_tool_call","server":"other","tool":"memory_write","arguments":{}}}`,
		`{"type":"item.completed","item":{"id":"i5","type":"agent_message","text":"done, saved."}}`,
	}, "\n")
	ev := ParseCodex(strings.NewReader(in))
	var writes, texts int
	for _, e := range ev {
		if e.Kind == EventEngramCall {
			if e.Op != "write" || e.Via != "mcp" {
				t.Errorf("unexpected engram call %+v", e)
			}
			writes++
		}
		if e.Kind == EventText {
			texts++
		}
	}
	if writes != 1 {
		t.Errorf("codex writes=%d (want 1, SKILL.md sed + engram-mcp version must not count)", writes)
	}
	if texts != 2 {
		t.Errorf("codex texts=%d", texts)
	}
}

func TestParseOpenCode(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"tool_use","part":{"tool":"skill","state":{"input":{"id":"engram"}}}}`,
		`{"type":"tool_use","part":{"tool":"read","state":{"input":{"path":"/home/u/project/engram/store/store.go"}}}}`,
		`{"type":"tool_use","part":{"tool":"shell","state":{"input":{"command":"which engram || which engram-mcp 2>/dev/null; CGO_ENABLED=0 go build ./cmd/engram"}}}}`,
		`{"type":"tool_use","part":{"tool":"shell","state":{"input":{"command":"cd /repo && ./engram --data-dir /d add --name tea --content 'jasmine tea'"}}}}`,
		`{"type":"tool_use","part":{"tool":"shell","state":{"input":{"command":"./engram --data-dir /d search tea"}}}}`,
		`{"type":"text","part":{"type":"text","text":"已保存 tea-preference。"}}`,
	}, "\n")
	ev := ParseOpenCode(strings.NewReader(in))
	var writes, searches int
	for _, e := range ev {
		if e.Kind == EventEngramCall {
			switch e.Op {
			case "write":
				writes++
			case "search":
				searches++
			}
			if e.Via != "cli" {
				t.Errorf("expected cli via, got %+v", e)
			}
		}
	}
	if writes != 1 || searches != 1 {
		t.Errorf("opencode writes=%d searches=%d (explore/build/read must not count)", writes, searches)
	}
}
