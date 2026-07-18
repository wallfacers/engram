package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeNamespace(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{name: "default", want: "default", ok: true},
		{name: "projectA", want: "projectA", ok: true},
		{name: "a.b_c-1", want: "a.b_c-1", ok: true},
		{name: "", want: defaultNamespace, ok: true},
		{name: ".."},
		{name: "."},
		{name: "a/b"},
		{name: `a\b`},
		{name: "/absolute"},
		{name: strings.Repeat("a", 65)},
		{name: "has space"},
		{name: "has%percent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeNamespace(tt.name)
			if tt.ok {
				if err != nil {
					t.Fatalf("normalizeNamespace(%q): %v", tt.name, err)
				}
				if got != tt.want {
					t.Fatalf("normalizeNamespace(%q) = %q, want %q", tt.name, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("normalizeNamespace(%q) unexpectedly accepted as %q", tt.name, got)
			}
		})
	}
}

func TestNamespaceDatabasePathStaysInDataDir(t *testing.T) {
	dataDir := t.TempDir()
	path, err := namespaceDatabasePath(dataDir, "projectA")
	if err != nil {
		t.Fatal(err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolvedPath, resolvedDir+string(filepath.Separator)) {
		t.Fatalf("database path %q escaped data dir %q", resolvedPath, resolvedDir)
	}
}
