package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/wallfacers/engram/embedding"
)

// retryingEmbedder wraps an embedding.Client with bounded retry + doubling
// backoff. The engine intentionally treats a failed Embed as a silently
// degraded semantic signal (Constitution V) — correct in production, but in an
// eval run a single transient sidecar connection reset silently turns that
// question's retrieval into keyword+entity-only and contaminates paired
// comparisons (008 US4 logged 270 such degradations). Absorbing transient
// faults here keeps the engine untouched while making bench retrieval honestly
// three-signal.
//
// Any error is retried (a bench talks to one known-good local sidecar; a
// non-transient misconfiguration just costs two extra attempts) unless the
// caller's context is done, which aborts immediately.
type retryingEmbedder struct {
	inner     embedding.Client
	attempts  int
	backoff   time.Duration
	sleep     func(time.Duration)
	logger    *slog.Logger
	retried   atomic.Int64 // calls that succeeded only after ≥1 retry
	exhausted atomic.Int64 // calls that failed all attempts
}

func newRetryingEmbedder(inner embedding.Client, attempts int, backoff time.Duration, logger *slog.Logger) *retryingEmbedder {
	if attempts < 1 {
		attempts = 1
	}
	return &retryingEmbedder{inner: inner, attempts: attempts, backoff: backoff, sleep: time.Sleep, logger: logger}
}

func (r *retryingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error
	delay := r.backoff
	for attempt := 1; attempt <= r.attempts; attempt++ {
		vecs, err := r.inner.Embed(ctx, texts)
		if err == nil {
			if attempt > 1 {
				r.retried.Add(1)
				r.logger.Info("embed recovered after retry", "attempt", attempt, "texts", len(texts))
			}
			return vecs, nil
		}
		lastErr = err
		if attempt == r.attempts {
			break
		}
		r.sleep(delay)
		delay *= 2
		if ctx.Err() != nil {
			r.exhausted.Add(1)
			return nil, ctx.Err()
		}
	}
	r.exhausted.Add(1)
	r.logger.Warn("embed retries exhausted; semantic signal will degrade for this call",
		"attempts", r.attempts, "texts", len(texts), "err", lastErr)
	return nil, lastErr
}

func (r *retryingEmbedder) Model() string { return r.inner.Model() }
