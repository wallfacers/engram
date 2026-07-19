package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

var knownCommands = map[string]struct{}{
	"add":        {},
	"ingest":     {},
	"delete":     {},
	"search":     {},
	"get":        {},
	"list":       {},
	"stats":      {},
	"export":     {},
	"namespaces": {},
	"version":    {},
}

func run(args []string, _ io.Reader, stdout io.Writer, stderr io.Writer) int {
	config, commandArgs, err := loadConfig(args, os.Getenv)
	if err != nil {
		return diagnose(stderr, exitUsage, err.Error(), "set ENGRAM_DATA_DIR or pass --data-dir")
	}
	if len(commandArgs) == 0 {
		return diagnose(stderr, exitUsage, "command is required", "run: engram <command>")
	}
	command := commandArgs[0]
	if _, ok := knownCommands[command]; !ok {
		return diagnose(stderr, exitUsage, fmt.Sprintf("unknown command %q", command), "run: engram <command>")
	}
	if _, err := normalizeNamespace(config.Namespace); err != nil {
		return diagnose(stderr, exitInvalidNS, err.Error(), "allowed: ^[A-Za-z0-9._-]{1,64}$")
	}

	handle, err := openEngine(context.Background(), config)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to open memory store", "check --data-dir and try again")
	}
	defer handle.Close() //nolint:errcheck // Close drains queued embeddings before process exit.

	switch command {
	case "add":
		return runAdd(context.Background(), handle, commandArgs[1:], stdout, stderr)
	case "search":
		return runSearch(context.Background(), handle, commandArgs[1:], stdout, stderr)
	case "get":
		return runGet(context.Background(), handle, commandArgs[1:], stdout, stderr)
	case "list":
		return runList(context.Background(), handle, commandArgs[1:], stdout, stderr)
	case "delete":
		return runDelete(context.Background(), handle, commandArgs[1:], stdout, stderr)
	}
	return diagnose(stderr, exitUsage, fmt.Sprintf("command %q is not available", command), "run: engram help")
}
