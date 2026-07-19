package main

import (
	"context"
	"fmt"
	"io"
)

func runExport(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "export does not accept arguments", "run: engram export")
	}
	entries, err := handle.entries.List(ctx)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to export memories", "check the data directory and try again")
	}
	fmt.Fprint(stdout, renderExport(entries))
	return exitOK
}
