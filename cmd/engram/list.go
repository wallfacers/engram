package main

import (
	"context"
	"fmt"
	"io"
)

func runList(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "list does not accept arguments", "run: engram list")
	}
	entries, err := handle.entries.List(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to list memories", "check the data directory and try again")
	}
	fmt.Fprint(stdout, renderList(entries))
	return exitOK
}
