package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// scriptedEmbedder fails the first failN Embed calls, then succeeds.
type scriptedEmbedder struct {
	failN  int
	calls  int
	result [][]float32
}

func (s *scriptedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.calls++
	if s.calls <= s.failN {
		return nil, errors.New("read tcp 127.0.0.1: connection reset by peer")
	}
	return s.result, nil
}

func (s *scriptedEmbedder) Model() string { return "stub-model" }

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestRetryingEmbedderRecoversFromTransientFailures(t *testing.T) {
	inner := &scriptedEmbedder{failN: 2, result: [][]float32{{0.1, 0.2}}}
	var slept []time.Duration
	re := newRetryingEmbedder(inner, 3, 100*time.Millisecond, discardLogger())
	re.sleep = func(d time.Duration) { slept = append(slept, d) }

	vecs, err := re.Embed(context.Background(), []string{"q"})
	if err != nil {
		t.Fatalf("Embed: unexpected error after retries: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 2 {
		t.Fatalf("Embed: got %v, want the inner result", vecs)
	}
	if inner.calls != 3 {
		t.Fatalf("inner calls = %d, want 3 (2 failures + 1 success)", inner.calls)
	}
	if len(slept) != 2 {
		t.Fatalf("sleeps = %d, want 2 (one per retry)", len(slept))
	}
	if !(slept[0] == 100*time.Millisecond && slept[1] == 200*time.Millisecond) {
		t.Fatalf("backoff = %v, want doubling from 100ms", slept)
	}
}

func TestRetryingEmbedderFirstTrySuccessDoesNotSleep(t *testing.T) {
	inner := &scriptedEmbedder{failN: 0, result: [][]float32{{1}}}
	re := newRetryingEmbedder(inner, 3, 100*time.Millisecond, discardLogger())
	re.sleep = func(time.Duration) { t.Fatal("sleep must not be called on first-try success") }

	if _, err := re.Embed(context.Background(), []string{"q"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

func TestRetryingEmbedderExhaustsAndReturnsLastError(t *testing.T) {
	inner := &scriptedEmbedder{failN: 99}
	re := newRetryingEmbedder(inner, 3, time.Millisecond, discardLogger())
	re.sleep = func(time.Duration) {}

	_, err := re.Embed(context.Background(), []string{"q"})
	if err == nil {
		t.Fatal("Embed: want error after exhausting attempts")
	}
	if inner.calls != 3 {
		t.Fatalf("inner calls = %d, want exactly 3 attempts", inner.calls)
	}
	if got := re.exhausted.Load(); got != 1 {
		t.Fatalf("exhausted counter = %d, want 1", got)
	}
}

func TestRetryingEmbedderStopsWhenContextCanceled(t *testing.T) {
	inner := &scriptedEmbedder{failN: 99}
	ctx, cancel := context.WithCancel(context.Background())
	re := newRetryingEmbedder(inner, 5, time.Millisecond, discardLogger())
	re.sleep = func(time.Duration) { cancel() } // cancel while backing off

	_, err := re.Embed(ctx, []string{"q"})
	if err == nil {
		t.Fatal("Embed: want error when context canceled")
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1 (no retry after cancel)", inner.calls)
	}
}

func TestRetryingEmbedderDelegatesModel(t *testing.T) {
	re := newRetryingEmbedder(&scriptedEmbedder{}, 3, time.Millisecond, discardLogger())
	if got := re.Model(); got != "stub-model" {
		t.Fatalf("Model() = %q, want delegation to inner", got)
	}
}
