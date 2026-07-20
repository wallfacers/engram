package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/embedding"
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
