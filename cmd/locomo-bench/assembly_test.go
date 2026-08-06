package main

// 030 Foundational unit tests (specs/030, T005). Cover the assembly data
// types and the exact-token counter boundary: kind classification, chunk
// fraction metric, per-unit estimate bookkeeping, and the exact-counter
// unavailable/exact paths. All tests are offline (stub token counter, no
// network).

import (
	"context"
	"testing"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

func TestKindOfEvidence(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"chunk-abc", "chunk"},
		{"chunk-something", "chunk"},
		{"fact-xyz", "fact"},
		{"plain-name", "fact"},
		{"", "fact"},
	}
	for _, c := range cases {
		if got := kindOfEvidence(c.name); got != c.want {
			t.Errorf("kindOfEvidence(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestChunkTokenFraction(t *testing.T) {
	mk := func(kind string, tokens int) EvidenceUnit {
		return EvidenceUnit{Kind: kind, TokenCount: tokens}
	}
	cases := []struct {
		name  string
		units []EvidenceUnit
		want  float64
	}{
		{"all chunk", []EvidenceUnit{mk("chunk", 10), mk("chunk", 20)}, 1.0},
		{"all fact", []EvidenceUnit{mk("fact", 10), mk("fact", 20)}, 0.0},
		{"mixed", []EvidenceUnit{mk("chunk", 10), mk("fact", 30), mk("chunk", 10)}, 0.4},
		{"empty", nil, 0.0},
		{"zero tokens", []EvidenceUnit{mk("chunk", 0), mk("fact", 0)}, 0.0},
		{"consolidated not chunk", []EvidenceUnit{mk("consolidated", 10), mk("chunk", 10)}, 0.5},
	}
	for _, c := range cases {
		a := &EvidenceAssembly{Units: c.units}
		got := a.chunkTokenFraction()
		if got != c.want {
			t.Errorf("%s: chunkTokenFraction() = %v, want %v", c.name, got, c.want)
		}
	}
}

// stubTokenCounter is an offline fake for evidencecompiler.TokenCounter.
type stubTokenCounter struct {
	count int
	err   error
}

func (s *stubTokenCounter) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	if s.err != nil {
		return evidencecompiler.TokenCount{}, s.err
	}
	return evidencecompiler.TokenCount{InputTokens: s.count, Fingerprint: "stub"}, nil
}

func TestAssemblyTokenCounterUnavailable(t *testing.T) {
	// Nil counter → exact counter unavailable: ok=false, no error (estimate fallback signal).
	c := &assemblyTokenCounter{counter: nil, answerModel: "model"}
	_, ok, err := c.countPrompt(context.Background(), "sys", "user")
	if err != nil || ok {
		t.Fatalf("nil counter: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	// Empty answerer model → unavailable even with a counter.
	c2 := &assemblyTokenCounter{counter: &stubTokenCounter{count: 7}, answerModel: ""}
	_, ok2, err2 := c2.countPrompt(context.Background(), "sys", "user")
	if err2 != nil || ok2 {
		t.Fatalf("empty model: ok=%v err=%v, want ok=false err=nil", ok2, err2)
	}
	// Nil receiver → unavailable.
	var nilCounter *assemblyTokenCounter
	_, ok3, err3 := nilCounter.countPrompt(context.Background(), "sys", "user")
	if err3 != nil || ok3 {
		t.Fatalf("nil receiver: ok=%v err=%v, want ok=false err=nil", ok3, err3)
	}
}

func TestAssemblyTokenCounterExact(t *testing.T) {
	c := &assemblyTokenCounter{counter: &stubTokenCounter{count: 1234}, answerModel: "qwen"}
	count, ok, err := c.countPrompt(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("exact count: unexpected error: %v", err)
	}
	if !ok || count != 1234 {
		t.Fatalf("exact count: ok=%v count=%d, want ok=true count=1234", ok, count)
	}
}

func TestAssemblyTokenCounterPropagatesError(t *testing.T) {
	c := &assemblyTokenCounter{counter: &stubTokenCounter{err: errStub}, answerModel: "qwen"}
	_, ok, err := c.countPrompt(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected counter error to propagate")
	}
	if ok {
		t.Fatal("ok must be false when the counter errors")
	}
}

// errStub is a sentinel for the stub error-propagation test.
var errStub = &stubError{}

type stubError struct{}

func (*stubError) Error() string { return "stub error" }
