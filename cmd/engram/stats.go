package main

import (
	"context"
	"fmt"
	"io"
)

func runStats(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "stats does not accept arguments", "run: engram stats")
	}
	entries, err := handle.entries.Count(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to count memories", "check the data directory and try again")
	}
	nonPinned, err := handle.entries.CountNonPinned(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to count non-pinned memories", "check the data directory and try again")
	}
	manifestSize, err := handle.entries.ManifestSizeEstimate(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to estimate manifest size", "check the data directory and try again")
	}
	fmt.Fprintf(stdout, "# stats\n\n- entries: %d\n- non-pinned: %d\n- manifest-size-estimate: %d\n", entries, nonPinned, manifestSize)
	return exitOK
}
