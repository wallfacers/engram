package main

import (
	"fmt"
	"io"

	"github.com/wallfacers/engram/internal/version"
)

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "version does not accept arguments", "run: engram version")
	}
	fmt.Fprintln(stdout, version.Version)
	return exitOK
}
