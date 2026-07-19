package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidNamespacesAreRejectedWithoutEscapingDataDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	outside := filepath.Join(filepath.Dir(dataDir), "outside")
	abs := filepath.Join(outside, "absolute")

	for _, namespace := range []string{"../outside", "a/b", `a\b`, abs, ".", ".."} {
		t.Run(namespace, func(t *testing.T) {
			if _, err := namespaceDatabasePath(dataDir, namespace); err == nil {
				t.Fatalf("namespaceDatabasePath(%q) unexpectedly succeeded", namespace)
			}

			var stdout, stderr bytes.Buffer
			got := run([]string{"--data-dir", dataDir, "--namespace", namespace, "list"}, strings.NewReader(""), &stdout, &stderr)
			if got != exitInvalidNS {
				t.Fatalf("run exit code = %d, want %d", got, exitInvalidNS)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); !strings.Contains(got, "invalid namespace") || !strings.Contains(got, "allowed:") {
				t.Errorf("stderr = %q, want invalid namespace diagnostic", got)
			}
			if _, err := os.Stat(outside + ".db"); !os.IsNotExist(err) {
				t.Errorf("invalid namespace created database outside data dir: %v", err)
			}
			if _, err := os.Stat(abs + ".db"); !os.IsNotExist(err) {
				t.Errorf("absolute namespace created database outside data dir: %v", err)
			}
		})
	}
}
