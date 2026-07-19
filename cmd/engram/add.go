package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/wallfacers/engram/memory"
)

func runAdd(ctx context.Context, handle *engineHandle, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "memory name")
	content := fs.String("content", "", "memory content")
	trigger := fs.String("trigger", "", "retrieval trigger")
	category := fs.String("category", "", "memory category")
	pinned := fs.Bool("pinned", false, "pin memory")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 {
		return diagnose(stderr, exitUsage, "add requires valid flags", "run: engram add --name <name> --content <text>")
	}
	if strings.TrimSpace(*name) == "" {
		return diagnose(stderr, exitUsage, "memory name is required", "run: engram add --name <name> --content <text>")
	}
	budgets := memory.DefaultBudgets()
	if err := budgets.CheckEntryContent(*content); err != nil {
		return diagnoseBudgetError(stderr, err)
	}
	if err := budgets.CheckTrigger(*trigger); err != nil {
		return diagnoseBudgetError(stderr, err)
	}
	entry := &memory.Entry{
		Name:      strings.TrimSpace(*name),
		Trigger:   *trigger,
		Content:   *content,
		Category:  *category,
		Pinned:    *pinned,
		CharCount: memory.CharCount(*content),
	}
	if err := handle.entries.Upsert(ctx, entry); err != nil {
		return diagnose(stderr, exitEngine, "unable to store memory", "check the data directory and try again")
	}
	handle.embedder.Enqueue(entry.Name)
	fmt.Fprintf(stdout, "# added\n\n- name: %s\n", entry.Name)
	return exitOK
}

func diagnoseBudgetError(stderr io.Writer, err error) int {
	var tooLarge memory.ErrMemoryTooLarge
	if errors.As(err, &tooLarge) {
		return diagnose(stderr, exitContentReject, fmt.Sprintf("content rejected: limit=%d actual=%d", tooLarge.Limit, tooLarge.Actual), "shorten the memory")
	}
	return diagnose(stderr, exitContentReject, "content rejected", "shorten the memory or trigger")
}
