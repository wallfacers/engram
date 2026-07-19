package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runNamespaces(config Config, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return diagnose(stderr, exitUsage, "namespaces does not accept arguments", "run: engram namespaces")
	}
	entries, err := os.ReadDir(config.DataDir)
	if os.IsNotExist(err) {
		fmt.Fprint(stdout, "# namespaces\n\n_0 namespaces_\n")
		return exitOK
	}
	if err != nil {
		return diagnose(stderr, exitEngine, "unable to list namespaces", "check --data-dir and try again")
	}
	namespaces := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		namespaces = append(namespaces, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
	}
	sort.Strings(namespaces)
	fmt.Fprintf(stdout, "# namespaces\n\n_%d namespaces_\n", len(namespaces))
	for _, namespace := range namespaces {
		fmt.Fprintf(stdout, "- %s\n", namespace)
	}
	return exitOK
}
