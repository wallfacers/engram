package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wallfacers/engram/store"
)

func runGet(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return diagnose(stderr, exitUsage, "memory name is required", "run: engram get <name>")
	}
	entry, err := handle.entries.GetByName(ctx, args[0])
	if errors.Is(err, store.ErrNotFound) {
		return diagnose(stderr, exitNotFound, fmt.Sprintf("memory %q not found", args[0]), "run: engram list")
	}
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to read memory", "check the data directory and try again")
	}
	fmt.Fprint(stdout, renderEntry(entry))
	return exitOK
}
