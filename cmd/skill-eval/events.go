package main

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// EventKind enumerates normalized runner-output events.
type EventKind string

const (
	EventEngramCall EventKind = "engram_call"    // an engram operation actually executed
	EventText       EventKind = "assistant_text" // assistant-visible text
)

// Event is the normalized, tool-agnostic view of one runner output item.
type Event struct {
	Kind EventKind
	Op   string // write|search|get|list|delete|other — set for engram_call
	Via  string // mcp|cli — set for engram_call
	Text string // set for assistant_text
}

// cliSubcommands maps engram CLI subcommands to judge ops.
var cliSubcommands = map[string]string{
	"add": "write", "search": "search", "get": "get", "list": "list",
	"delete": "delete", "version": "other", "stats": "other", "export": "other",
	"curate": "other", "ingest": "other",
}

// mcpToolOps maps engram MCP tool names to judge ops.
var mcpToolOps = map[string]string{
	"memory_write": "write", "memory_search": "search", "memory_get": "get",
	"memory_list": "list", "memory_delete": "delete",
}

// cliInvocation finds an actual engram CLI invocation inside a shell command
// string. It deliberately does NOT match: engram-mcp, `go build/run` lines,
// `which`/`ls`/`sed` mentions, or bare paths without a subcommand — those are
// exploration, not memory operations. Returns the op and true when found.
func cliInvocation(command string) (string, bool) {
	// Neutralize the MCP binary name so "engram-mcp" never matches "engram".
	s := strings.ReplaceAll(command, "engram-mcp", "\x00mcp\x00")
	for _, seg := range regexp.MustCompile(`[;|&\n]`).Split(s, -1) {
		seg = strings.TrimSpace(seg)
		// Take the command token chain; an engram invocation is any token
		// "engram" or */engram possibly preceded by env assignments, then
		// global flags, then a subcommand.
		fields := strings.Fields(seg)
		idx := -1
		for i, f := range fields {
			base := f
			if i := strings.LastIndexByte(base, '/'); i >= 0 {
				base = base[i+1:]
			}
			if base == "engram" {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		expectValue := false
		for _, f := range fields[idx+1:] {
			if expectValue {
				expectValue = false
				continue // value of a preceding global flag (e.g. --data-dir X)
			}
			if strings.HasPrefix(f, "-") {
				// Global flag: `--flag=value` carries its own value; a bare
				// `--flag` consumes the next word.
				expectValue = !strings.Contains(f, "=")
				continue
			}
			if op, ok := cliSubcommands[f]; ok {
				return op, true
			}
			break // first non-flag word wasn't a known subcommand
		}
	}
	return "", false
}

// mcpOp classifies an MCP tool name; ok only for engram memory tools.
func mcpOp(tool string) (string, bool) {
	if op, ok := mcpToolOps[tool]; ok {
		return op, true
	}
	return "", false
}

func engramMCPName(name string) (string, bool) {
	// claude: mcp__engram__memory_write ; others may use bare memory_write.
	if strings.HasPrefix(name, "mcp__engram__") {
		return strings.TrimPrefix(name, "mcp__engram__"), true
	}
	if strings.HasPrefix(name, "memory_") {
		return name, true
	}
	return "", false
}

// ---------- per-tool raw JSONL parsing ----------

// parseJSONLStream reads a raw runner stdout stream line by line, skipping
// non-JSON noise lines (e.g. codex's "Reading additional input from stdin...").
func parseJSONLStream(r io.Reader, handle func(obj map[string]any)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		handle(obj)
	}
}

// ParseClaude normalizes a claude-code stream-json (--verbose) output.
func ParseClaude(r io.Reader) []Event {
	var ev []Event
	parseJSONLStream(r, func(obj map[string]any) {
		switch obj["type"] {
		case "assistant":
			msg, _ := obj["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, item := range content {
				blk, _ := item.(map[string]any)
				switch blk["type"] {
				case "tool_use":
					name, _ := blk["name"].(string)
					if tool, ok := engramMCPName(name); ok {
						if op, ok := mcpOp(tool); ok {
							ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "mcp"})
						}
					} else if name == "Bash" {
						// CLI-only intents (stats/export/version) surface as Bash
						// commands; codex/opencode parse their shell tool the
						// same way, so an invisible CLI fallback here would be
						// judged "no engram call".
						input, _ := blk["input"].(map[string]any)
						cmd, _ := input["command"].(string)
						if op, ok := cliInvocation(cmd); ok {
							ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "cli"})
						}
					}
				case "text":
					t, _ := blk["text"].(string)
					if t != "" {
						ev = append(ev, Event{Kind: EventText, Text: t})
					}
				}
			}
		case "result":
			res, _ := obj["result"].(string)
			if res != "" {
				ev = append(ev, Event{Kind: EventText, Text: res})
			}
		}
	})
	return ev
}

// ParseCodex normalizes a `codex exec --json` output.
func ParseCodex(r io.Reader) []Event {
	var ev []Event
	parseJSONLStream(r, func(obj map[string]any) {
		if obj["type"] != "item.completed" {
			return
		}
		item, _ := obj["item"].(map[string]any)
		switch item["type"] {
		case "mcp_tool_call":
			server, _ := item["server"].(string)
			tool, _ := item["tool"].(string)
			if server == "engram" {
				if op, ok := mcpOp(tool); ok {
					ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "mcp"})
				}
			}
		case "command_execution":
			cmd, _ := item["command"].(string)
			if op, ok := cliInvocation(cmd); ok {
				ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "cli"})
			}
		case "agent_message":
			t, _ := item["text"].(string)
			if t != "" {
				ev = append(ev, Event{Kind: EventText, Text: t})
			}
		}
	})
	return ev
}

// ParseOpenCode normalizes an `opencode run --format json` output.
func ParseOpenCode(r io.Reader) []Event {
	var ev []Event
	parseJSONLStream(r, func(obj map[string]any) {
		switch obj["type"] {
		case "text":
			part, _ := obj["part"].(map[string]any)
			t, _ := part["text"].(string)
			if t != "" {
				ev = append(ev, Event{Kind: EventText, Text: t})
			}
		case "tool_use":
			part, _ := obj["part"].(map[string]any)
			tool, _ := part["tool"].(string)
			if tool == "shell" {
				state, _ := part["state"].(map[string]any)
				input, _ := state["input"].(map[string]any)
				cmd, _ := input["command"].(string)
				if op, ok := cliInvocation(cmd); ok {
					ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "cli"})
				}
			} else if tool != "" && tool != "skill" && tool != "read" {
				// Defensive: any future direct MCP tool_use whose name is an
				// engram memory tool.
				if t, ok := engramMCPName(tool); ok {
					if op, ok := mcpOp(t); ok {
						ev = append(ev, Event{Kind: EventEngramCall, Op: op, Via: "mcp"})
					}
				}
			}
		}
	})
	return ev
}
