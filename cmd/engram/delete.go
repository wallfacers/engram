package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wallfacers/engram/store"
)

func runDelete(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return diagnose(stderr, exitUsage, "memory name is required", "run: engram delete <name>")
	}
	if err := handle.entries.Delete(ctx, args[0]); errors.Is(err, store.ErrNotFound) {
		return diagnose(stderr, exitNotFound, fmt.Sprintf("memory %q not found", args[0]), "run: engram list")
	} else if err != nil {
		return diagnose(stderr, exitEngine, "unable to delete memory", "check the data directory and try again")
	}
	fmt.Fprintf(stdout, "# deleted\n\n- name: %s\n", args[0])
	return exitOK
}
