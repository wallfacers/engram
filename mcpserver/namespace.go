package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const defaultNamespace = "default"

var namespacePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func normalizeNamespace(namespace string) (string, error) {
	if namespace == "" {
		return defaultNamespace, nil
	}
	if namespace == "." || namespace == ".." || !namespacePattern.MatchString(namespace) {
		return "", fmt.Errorf("invalid namespace %q", namespace)
	}
	return namespace, nil
}

func namespaceDatabasePath(dataDir, namespace string) (string, error) {
	ns, err := normalizeNamespace(namespace)
	if err != nil {
		return "", err
	}
	if dataDir == "" {
		return "", errors.New("data directory is required")
	}

	base, err := filepath.Abs(filepath.Clean(dataDir))
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}

	path := filepath.Join(base, ns+".db")
	if err := assertWithinDirectory(base, path); err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if err := assertWithinDirectory(base, resolved); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve namespace database path: %w", err)
	}
	return path, nil
}

func assertWithinDirectory(base, path string) error {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return fmt.Errorf("check namespace database path: %w", err)
	}
	if rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("namespace database path %q escapes data directory", path)
	}
	return nil
}
