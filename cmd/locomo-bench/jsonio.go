package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func scanResultsJSONL(path string, visit func(result)) error {
	f, err := os.Open(path) //nolint:gosec // operator-selected run artifact
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item result
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue // tolerate malformed or partial lines, matching resume behavior
		}
		visit(item)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

// scanResultsJSONLStrict is for fail-closed evaluation artifacts. Legacy
// journals intentionally tolerate a torn final line for ordinary resume, but
// formal materialization and isolated paired experiments must never turn a
// malformed line into a silently smaller denominator.
func scanResultsJSONLStrict(path string, visit func(result) error) error {
	f, err := os.Open(path) //nolint:gosec // operator-selected formal run artifact
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var item result
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return fmt.Errorf("decode %s line %d: %w", path, line, err)
		}
		if err := visit(item); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

// readJSON reads and decodes one JSON document from path. It is the strict
// counterpart of writeJSON: any decode error is returned (no tolerance).
func readJSON(path string, value any) error {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected artifact
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
