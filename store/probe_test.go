package store_test

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/store"
)

func TestProbeFTS5_Success(t *testing.T) {
	s, err := store.Open(context.Background(), store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Error("DB() should not return nil")
	}
}
