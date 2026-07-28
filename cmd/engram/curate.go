package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

const curateTimeout = 2 * time.Minute

// runCurate synchronously executes one bounded curation pass for the selected
// namespace. Unlike MCP's persistent mode, CLI construction never starts a
// background worker and ordinary add/ingest commands never notify it.
func runCurate(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "curate takes no arguments", "run: engram curate")
	}
	if handle == nil || handle.curator == nil {
		return diagnose(stderr, exitCapability, "curate requires an LLM", "set ENGRAM_LLM_BASE_URL/MODEL/PROVIDER and ENGRAM_LLM_API_KEY")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	passCtx, cancel := context.WithTimeout(ctx, curateTimeout)
	defer cancel()

	err := handle.curator.RunPassContext(passCtx)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return diagnose(stderr, exitEngine, "curation timed out", "retry engram curate or inspect the LLM provider")
	case errors.Is(err, context.Canceled):
		return diagnose(stderr, exitEngine, "curation cancelled", "retry engram curate")
	}
	fmt.Fprintf(stdout, "# curated\n\n- namespace: %s\n- status: completed\n", handle.namespace)
	return exitOK
}
