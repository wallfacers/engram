package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

func TestAddDrainsEmbeddingBeforeHandleClose(t *testing.T) {
	config := Config{DataDir: t.TempDir(), Namespace: defaultNamespace}
	client := stubEmbeddingClient{model: "stub-model", vector: []float32{0.25, 0.75}}
	handle, err := openEngineWith(context.Background(), config, client, nil)
	if err != nil {
		t.Fatalf("openEngineWith: %v", err)
	}
	var stdout, stderr strings.Builder
	if code := runAdd(context.Background(), handle, []string{"--name", "durable-vector", "--content", "vectors must persist"}, &stdout, &stderr); code != exitOK {
		handle.Close() //nolint:errcheck
		t.Fatalf("add exit code = %d, stderr = %q", code, stderr.String())
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close writing handle: %v", err)
	}

	fresh, err := openEngineWith(context.Background(), config, client, nil)
	if err != nil {
		t.Fatalf("reopen handle: %v", err)
	}
	defer fresh.Close() //nolint:errcheck
	vectors, err := fresh.vectors.LoadAllForModel(context.Background(), client.Model())
	if err != nil {
		t.Fatalf("load vectors: %v", err)
	}
	if got := vectors["durable-vector"]; len(got) != len(client.vector) {
		t.Fatalf("stored vector = %v, want %v", got, client.vector)
	}
}

type stubEmbeddingClient struct {
	model  string
	vector []float32
}

var _ embedding.Client = stubEmbeddingClient{}

func (c stubEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = append([]float32(nil), c.vector...)
	}
	return vectors, nil
}

func (c stubEmbeddingClient) Model() string { return c.model }

func TestRunCurateHonorsCallerDeadlineWithoutLateApply(t *testing.T) {
	ctx := context.Background()
	callerDone := make(chan error, 1)
	caller := pipeline.ModelCaller(func(callCtx context.Context, _, _ string) (string, error) {
		<-callCtx.Done()
		callerDone <- callCtx.Err()
		return `{"evict":["candidate"],"merge":[]}`, nil
	})
	handle, err := openEngineWith(ctx, Config{
		DataDir: t.TempDir(), Namespace: defaultNamespace,
	}, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := handle.entries.Upsert(ctx, &memory.Entry{Name: "candidate", Content: "must survive timeout"}); err != nil {
		t.Fatal(err)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := runCurate(shortCtx, handle, nil, &stdout, &stderr)
	if code != exitEngine || stdout.Len() != 0 || !strings.Contains(stderr.String(), "curation timed out") {
		t.Fatalf("runCurate = code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	select {
	case err := <-callerDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("caller error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller did not observe deadline")
	}
	if _, err := handle.entries.GetByName(ctx, "candidate"); errors.Is(err, store.ErrNotFound) {
		t.Fatal("timed-out curation applied a late eviction")
	} else if err != nil {
		t.Fatal(err)
	}
}

type cancelOnAppliedLog struct {
	cancel context.CancelFunc
	once   sync.Once
}

func (w *cancelOnAppliedLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "curation: pass applied") {
		w.once.Do(w.cancel)
	}
	return len(p), nil
}

func TestRunCurateReportsSuccessWhenCancellationArrivesAfterCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelOnAppliedLog{cancel: cancel}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	caller := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		return `{"evict":["candidate"],"merge":[]}`, nil
	})
	handle, err := openEngineWith(ctx, Config{
		DataDir: t.TempDir(), Namespace: defaultNamespace,
	}, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := handle.entries.Upsert(context.Background(), &memory.Entry{Name: "candidate", Content: "commit before cancel"}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCurate(ctx, handle, nil, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("committed curate reported code %d, stderr %q", code, stderr.String())
	}
	if _, err := handle.entries.GetByName(context.Background(), "candidate"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected committed eviction, got %v", err)
	}
}

func TestOrdinaryAddDoesNotStartOrNotifyCurator(t *testing.T) {
	ctx := context.Background()
	called := make(chan struct{}, 1)
	caller := pipeline.ModelCaller(func(context.Context, string, string) (string, error) {
		called <- struct{}{}
		return `{"evict":[],"merge":[]}`, nil
	})
	handle, err := openEngineWith(ctx, Config{
		DataDir: t.TempDir(), Namespace: defaultNamespace,
	}, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	var stdout, stderr strings.Builder
	if code := runAdd(ctx, handle, []string{"--name", "manual", "--content", "manual memory"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("add code = %d, stderr = %q", code, stderr.String())
	}
	select {
	case <-called:
		t.Fatal("ordinary add invoked curation")
	case <-time.After(20 * time.Millisecond):
	}
}
