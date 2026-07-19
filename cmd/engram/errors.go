package main

import (
	"fmt"
	"io"
)

const (
	exitOK            = 0
	exitEngine        = 1
	exitUsage         = 2
	exitNotFound      = 3
	exitCapability    = 4
	exitInvalidNS     = 5
	exitContentReject = 6
)

func diagnose(stderr io.Writer, code int, problem, next string) int {
	fmt.Fprintf(stderr, "%s — %s\n", problem, next)
	return code
}
