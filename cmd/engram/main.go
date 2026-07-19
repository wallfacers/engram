package main

import (
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is expanded into the command dispatcher in T010.
func run(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) int {
	return 0
}
