package main

// 030 US3 conditional-consolidation tests (specs/030, T020). Assert the
// budget-dependent decision: within budget → retain raw (byte-identical);
// over cap + no sidecar → deterministic truncation; over cap + sidecar →
// compact replacement ≤ cap; sidecar failure → truncation fallback. Offline.

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func consUnits(n, per int) []EvidenceUnit {
	units := make([]EvidenceUnit, 0, n)
	for i := 0; i < n; i++ {
		units = append(units, EvidenceUnit{
			SourceID:  "chunk-" + string(rune('a'+i)),
			Text:      strings.Repeat("x", per),
			Kind:      "chunk",
			TokenCount: estimateTokens(strings.Repeat("x", per)),
			Estimated: true,
		})
	}
	return units
}

func TestConsolidateWithinBudgetRetains(t *testing.T) {
	units := consUnits(3, 40) // ~30 tokens total
	cfg := consolidateConfig{Cap: 3600}
	out, replaced, compressed, err := consolidateUnits(context.Background(), "q", units, cfg)
	if err != nil {
		t.Fatalf("consolidateUnits: %v", err)
	}
	if compressed {
		t.Fatal("within budget must retain raw (compressed=false)")
	}
	if len(replaced) != 0 {
		t.Fatalf("within budget: replaced = %v, want empty", replaced)
	}
	if len(out) != len(units) {
		t.Fatalf("within budget must be byte-identical in length: %d vs %d", len(out), len(units))
	}
	for i := range units {
		if out[i].Text != units[i].Text {
			t.Fatalf("within budget must retain raw text at %d", i)
		}
	}
}

func TestConsolidateDeterministicTruncation(t *testing.T) {
	units := consUnits(10, 200) // ~500 tokens, over a tight cap
	cfg := consolidateConfig{Cap: 100} // no sidecar → truncate
	out, replaced, compressed, err := consolidateUnits(context.Background(), "q", units, cfg)
	if err != nil {
		t.Fatalf("consolidateUnits: %v", err)
	}
	if !compressed {
		t.Fatal("over cap must trigger consolidation")
	}
	if len(replaced) == 0 {
		t.Fatal("truncation must record replaced unit IDs")
	}
	if totalUnitTokens(out) > 100 {
		t.Fatalf("truncated units exceed cap: %d > 100", totalUnitTokens(out))
	}
	if len(out) >= len(units) {
		t.Fatal("expected some units dropped")
	}
}

// stubCompressCall returns a compact replacement JSON.
func stubCompressCall(_ context.Context, _, _ string) (string, provider.Usage, error) {
	return `{"compact_evidence": [{"text": "compact summary that fits"}]}`, provider.Usage{}, nil
}

func TestConsolidateSidecarCompression(t *testing.T) {
	units := consUnits(10, 200) // over a tight cap
	cfg := consolidateConfig{Cap: 100, Call: stubCompressCall}
	out, replaced, compressed, err := consolidateUnits(context.Background(), "q", units, cfg)
	if err != nil {
		t.Fatalf("consolidateUnits: %v", err)
	}
	if !compressed {
		t.Fatal("over cap + sidecar must compress")
	}
	if len(out) != 1 || out[0].Kind != "consolidated" {
		t.Fatalf("expected one consolidated unit, got %d (%v)", len(out), out[0].Kind)
	}
	if len(replaced) != len(units) {
		t.Fatalf("replaced IDs must cover all source units: %d vs %d", len(replaced), len(units))
	}
	if totalUnitTokens(out) > 100 {
		t.Fatalf("compressed output exceeds cap: %d > 100", totalUnitTokens(out))
	}
}

// failCompressCall errors → caller falls back to truncation.
func failCompressCall(_ context.Context, _, _ string) (string, provider.Usage, error) {
	return "", provider.Usage{}, errStub
}

func TestConsolidateSidecarFailureFallsBack(t *testing.T) {
	units := consUnits(10, 200)
	cfg := consolidateConfig{Cap: 100, Call: failCompressCall}
	out, replaced, compressed, err := consolidateUnits(context.Background(), "q", units, cfg)
	if err != nil {
		t.Fatalf("consolidateUnits: %v", err)
	}
	if !compressed {
		t.Fatal("sidecar failure must still apply deterministic truncation")
	}
	if len(replaced) == 0 {
		t.Fatal("truncation must record replaced IDs on fallback")
	}
	if totalUnitTokens(out) > 100 {
		t.Fatalf("fallback truncation exceeds cap: %d", totalUnitTokens(out))
	}
}

func TestConsolidateInvalidOutputFallsBack(t *testing.T) {
	bad := func(_ context.Context, _, _ string) (string, provider.Usage, error) {
		return `{not json`, provider.Usage{}, nil
	}
	units := consUnits(10, 200)
	cfg := consolidateConfig{Cap: 100, Call: bad}
	out, _, compressed, err := consolidateUnits(context.Background(), "q", units, cfg)
	if err != nil {
		t.Fatalf("consolidateUnits: %v", err)
	}
	if !compressed {
		t.Fatal("invalid sidecar output must fall back to truncation")
	}
	if totalUnitTokens(out) > 100 {
		t.Fatalf("fallback truncation exceeds cap: %d", totalUnitTokens(out))
	}
}
