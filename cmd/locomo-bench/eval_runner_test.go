package main

import "testing"

func TestPrepareFrozenEvalOptionsRejectsIRISAndForcesOneAnswerPath(t *testing.T) {
	protocol := testEvalProtocol()
	prepared, err := prepareFrozenEvalOptions(protocol, options{noIDKRetry: false})
	if err != nil {
		t.Fatalf("prepare formal options: %v", err)
	}
	if !prepared.noIDKRetry || prepared.iris {
		t.Fatalf("formal options = %+v, want legacy retry off and IRIS off", prepared)
	}
	if _, err := prepareFrozenEvalOptions(protocol, options{iris: true, noIDKRetry: true}); err == nil {
		t.Fatal("formal protocol unexpectedly accepted IRIS")
	}
	if _, err := prepareFrozenEvalOptions(protocol, options{rerank: true, noIDKRetry: true}); err == nil {
		t.Fatal("formal protocol unexpectedly accepted reranker")
	}
}
