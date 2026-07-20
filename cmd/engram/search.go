package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func runSearch(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	query, limit, err := parseSearchArgs(args)
	if err != nil {
		return diagnose(stderr, exitUsage, err.Error(), "run: engram search <query> [--limit <n>]")
	}
	results, err := handle.retriever.Search(ctx, query, limit)
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to search memories", "check the data directory and try again")
	}
	fmt.Fprint(stdout, renderSearch(results, handle.embClient == nil))
	return exitOK
}

func parseSearchArgs(args []string) (string, int, error) {
	limit := 8
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--limit" {
			i++
			if i == len(args) {
				return "", 0, fmt.Errorf("search limit is required")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return "", 0, fmt.Errorf("search limit must be an integer")
			}
			limit = n
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--limit="); ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return "", 0, fmt.Errorf("search limit must be an integer")
			}
			limit = n
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", 0, fmt.Errorf("unknown search flag %q", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return "", 0, fmt.Errorf("search query is required")
	}
	if limit <= 0 {
		return "", 0, fmt.Errorf("search limit must be greater than zero")
	}
	return positional[0], limit, nil
}
