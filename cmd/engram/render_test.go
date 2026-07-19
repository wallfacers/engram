package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
)

func TestRenderersIncludeEventRange(t *testing.T) {
	start := time.Date(2025, time.March, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2025, time.March, 3, 18, 0, 0, 0, time.UTC)
	entry := &memory.Entry{Name: "trip", Content: "Kyoto trip", EventStart: &start, EventEnd: &end}
	for name, document := range map[string]string{
		"get":  renderEntry(entry),
		"list": renderList([]*memory.Entry{entry}),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(document, "event start: 2025-03-01") || !strings.Contains(document, "event end: 2025-03-03") {
				t.Errorf("%s document = %q, want event range", name, document)
			}
		})
	}

	config := Config{DataDir: t.TempDir(), Namespace: defaultNamespace}
	handle, err := openEngine(context.Background(), config)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	defer handle.Close() //nolint:errcheck
	if err := handle.entries.Upsert(context.Background(), entry); err != nil {
		t.Fatalf("upsert event entry: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := runExport(context.Background(), handle, nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("export exit code = %d, stderr = %q", code, stderr.String())
	}
	if document := stdout.String(); !strings.Contains(document, "event start: 2025-03-01") || !strings.Contains(document, "event end: 2025-03-03") {
		t.Errorf("export document = %q, want event range", document)
	}
}
