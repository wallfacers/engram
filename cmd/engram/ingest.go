package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/pipeline"
)

func runIngest(ctx context.Context, handle *engineHandle, stdin io.Reader, stdout, stderr io.Writer) int {
	if handle.pipe == nil {
		return diagnose(stderr, exitCapability, "ingest requires an LLM", "set ENGRAM_LLM_BASE_URL/MODEL/PROVIDER and ENGRAM_LLM_API_KEY")
	}
	messages, err := parseIngestMessages(stdin)
	if err != nil {
		return diagnose(stderr, exitUsage, err.Error(), "use one user: or assistant: turn per line")
	}
	before, err := handle.entries.List(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to inspect memories before ingest", "check the data directory and try again")
	}
	count, err := handle.pipe.Ingest(ctx, time.Time{}, "", messages)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to ingest conversation", "check the LLM configuration and try again")
	}
	after, err := handle.entries.List(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to inspect memories after ingest", "check the data directory and try again")
	}
	known := make(map[string]struct{}, len(before))
	for _, entry := range before {
		known[entry.Name] = struct{}{}
	}
	fmt.Fprintf(stdout, "# ingested\n\n- extracted: %d\n", count)
	for _, entry := range after {
		if _, exists := known[entry.Name]; !exists {
			fmt.Fprintf(stdout, "- name: %s\n", entry.Name)
		}
	}
	return exitOK
}

func parseIngestMessages(stdin io.Reader) ([]pipeline.Message, error) {
	scanner := bufio.NewScanner(stdin)
	messages := make([]pipeline.Message, 0)
	for scanner.Scan() {
		line := scanner.Text()
		role, text, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid conversation turn %q", line)
		}
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("invalid conversation role %q", role)
		}
		messages = append(messages, pipeline.Message{Role: role, Text: strings.TrimSpace(text)})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read conversation turns: %w", err)
	}
	return messages, nil
}
