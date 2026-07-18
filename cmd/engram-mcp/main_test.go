package main

import (
	"testing"

	"github.com/wallfacers/engram/mcpserver"
)

func TestBuildEmbeddingClientKeepsOfflineClientNil(t *testing.T) {
	client, err := buildEmbeddingClient(mcpserver.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if client != nil {
		t.Fatalf("offline embedding client has dynamic type %T, want nil", client)
	}
}
