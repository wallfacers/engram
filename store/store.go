// Package store defines the persistence errors shared by the memory engine and
// the SQLite implementation in this package.
package store

import "errors"

// ErrNotFound is returned when a requested memory entry does not exist.
var ErrNotFound = errors.New("store: not found")
