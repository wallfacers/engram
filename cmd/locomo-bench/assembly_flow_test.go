package main

// 030 US1 assembler flow tests (specs/030, T006). Assert the read-side
// assembly behaviours before they are implemented: chunk-first ordering,
// temporal date ordering, cap truncation with exact-token accounting, and the
// estimate-fallback degradation signal. Offline (stub token counter).

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

var numberedEvidenceLineRE = regexp.MustCompile(`(?m)^\d+\. (.*)$`)

func hit(name, content string, score float64) memory.Result {
	return memory.Result{Name: name, Content: content, Score: score}
}

func dated(name, date string, score float64) memory.Result {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return memory.Result{Name: name, Content: name + " content", EventDate: &t, Score: score}
}

func testAssemblyConfig() assemblyConfig {
	return assemblyConfig{Cap: 3600, CurrentDate: "2026-01-01", Scaffold: false}
}

// proportionalStub counts the user prompt at ~len/4 + fixed overhead, so cap
// truncation iterations are observable without a network.
type proportionalStub struct{}

func (proportionalStub) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{InputTokens: len(input.User)/4 + 10, Fingerprint: "stub"}, nil
}

func TestAssembleChunkFirst(t *testing.T) {
	hits := []memory.Result{
		hit("fact-a", "fact aaaa", 5),
		hit("chunk-1", "chunk one", 3),
		hit("fact-b", "fact bbbb", 4),
		hit("chunk-2", "chunk two", 2),
	}
	asm, _, err := assembleEvidence(context.Background(), "q", hits, assemblyCategorySingleHop, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	got := []string{}
	for _, u := range asm.Units {
		got = append(got, u.SourceID)
	}
	// All chunks before all facts; within group, score desc.
	want := []string{"chunk-1", "chunk-2", "fact-a", "fact-b"}
	if len(got) != len(want) {
		t.Fatalf("unit order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unit order = %v, want %v", got, want)
		}
	}
}

func TestAssembleTemporalOrder(t *testing.T) {
	hits := []memory.Result{
		dated("chunk-later", "2024-03-01", 9),
		dated("fact-old", "2022-01-01", 1),
		dated("chunk-earlier", "2023-01-01", 5),
		dated("fact-new", "2024-06-01", 2),
	}
	asm, _, err := assembleEvidence(context.Background(), "q", hits, temporalCategory, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	got := []string{}
	for _, u := range asm.Units {
		got = append(got, u.SourceID)
	}
	// chunk group by date asc, then fact group by date asc.
	want := []string{"chunk-earlier", "chunk-later", "fact-old", "fact-new"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("temporal order = %v, want %v", got, want)
		}
	}
	if asm.Structure != "temporal" {
		t.Fatalf("structure = %q, want temporal", asm.Structure)
	}
}

func TestAssembleCapTruncation(t *testing.T) {
	counter := &assemblyTokenCounter{counter: proportionalStub{}, answerModel: "qwen"}
	cfg := assemblyConfig{Cap: 100, CurrentDate: "2026-01-01", Scaffold: false, SystemPrompt: "sys"}
	hits := []memory.Result{}
	for i := 0; i < 20; i++ {
		hits = append(hits, hit("chunk-"+string(rune('a'+i)), "long evidence line "+strings.Repeat("x", 60), float64(20-i)))
	}
	asm, _, err := assembleEvidence(context.Background(), "q", hits, assemblyCategorySingleHop, cfg, counter)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if asm.TotalTokens > 100 {
		t.Fatalf("TotalTokens = %d, want ≤ cap 100", asm.TotalTokens)
	}
	if len(asm.Units) == len(hits) {
		t.Fatal("expected truncation to drop some units")
	}
	if asm.TokensEstimated {
		t.Fatal("exact counter present; tokens_estimated must be false")
	}
}

func TestAssembleExactTotal(t *testing.T) {
	fixed := &fixedCountStub{count: 4321}
	counter := &assemblyTokenCounter{counter: fixed, answerModel: "qwen"}
	cfg := assemblyConfig{Cap: 3600, CurrentDate: "2026-01-01", Scaffold: false, SystemPrompt: "sys"}
	asm, _, err := assembleEvidence(context.Background(), "q", []memory.Result{hit("fact-a", "aaa", 1)}, assemblyCategorySingleHop, cfg, counter)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if asm.TotalTokens != 4321 || asm.TokensEstimated {
		t.Fatalf("TotalTokens=%d tokens_estimated=%v, want 4321 false", asm.TotalTokens, asm.TokensEstimated)
	}
}

func TestAssembleTokensEstimatedFallback(t *testing.T) {
	asm, _, err := assembleEvidence(context.Background(), "q", []memory.Result{hit("fact-a", "aaa", 1)}, assemblyCategorySingleHop, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if !asm.TokensEstimated {
		t.Fatal("nil counter must mark tokens_estimated=true")
	}
	if asm.TotalTokens < 1 {
		t.Fatalf("estimate fallback must still report TotalTokens > 0, got %d", asm.TotalTokens)
	}
}

func TestAssembleRender(t *testing.T) {
	hits := []memory.Result{hit("chunk-1", "chunk content", 5), hit("fact-a", "fact content", 4)}
	_, user, err := assembleEvidence(context.Background(), "q", hits, assemblyCategorySingleHop, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	for _, want := range []string{"chunk content", "fact content", "QUESTION: q"} {
		if !strings.Contains(user, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, user)
		}
	}
}

// TestAssembleChunkFractionThreshold is the SC-002 gate: when the candidate set
// carries chunks (the hybrid + chunk-quota retrieval regime the baseline uses),
// the assembler must surface them first so chunk_fraction clears the threshold.
func TestAssembleChunkFractionThreshold(t *testing.T) {
	// 6 chunks (long) + 4 facts (short): chunks dominate the token share.
	hits := []memory.Result{}
	for i := 0; i < 6; i++ {
		hits = append(hits, hit("chunk-"+string(rune('a'+i)), "verbatim chunk "+strings.Repeat("z", 80), float64(10-i)))
	}
	for i := 0; i < 4; i++ {
		hits = append(hits, hit("fact-"+string(rune('a'+i)), "short fact", float64(20-i)))
	}
	asm, _, err := assembleEvidence(context.Background(), "q", hits, assemblyCategorySingleHop, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if asm.ChunkFraction < 0.5 {
		t.Fatalf("chunk_fraction = %.3f, want ≥ 0.5 (SC-002; 029 was ~0.01)", asm.ChunkFraction)
	}
	// chunk units must precede fact units.
	seenFact := false
	for _, u := range asm.Units {
		if u.Kind == "fact" {
			seenFact = true
		}
		if seenFact && u.Kind == "chunk" {
			t.Fatal("fact before chunk in assembled order")
		}
	}
}

func TestAssembleEmptyHits(t *testing.T) {
	asm, user, err := assembleEvidence(context.Background(), "q", nil, assemblyCategorySingleHop, testAssemblyConfig(), nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if len(asm.Units) != 0 {
		t.Fatalf("empty hits must yield empty units, got %d", len(asm.Units))
	}
	if !strings.Contains(user, "(none)") {
		t.Fatalf("empty render missing (none): %q", user)
	}
}

func TestMultiHopPromptMatchesAssemblyUnits(t *testing.T) {
	hits := []memory.Result{
		hit("fact-alice", "Alice Smith derived fact", 99),
		hit("chunk-bob", "Bob Jones raw chunk", 3),
		hit("chunk-alice", "Alice Smith raw chunk", 4),
		hit("fact-bob", "Bob Jones derived fact", 98),
	}
	hits[0].CreatedAt = time.Date(2025, 2, 3, 0, 0, 0, 0, time.UTC)
	asm, prompt, err := assembleEvidence(
		t.Context(), "q", hits, assemblyCategoryMultiHop, testAssemblyConfig(), nil,
	)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	lines := numberedEvidenceLineRE.FindAllStringSubmatch(prompt, -1)
	if len(lines) != len(asm.Units) {
		t.Fatalf("prompt evidence lines = %d, units = %d\n%s", len(lines), len(asm.Units), prompt)
	}
	if !asm.PromptOrderMatchesUnits {
		t.Fatal("assembly audit did not certify prompt/unit evidence-line order")
	}
	for i, unit := range asm.Units {
		if !strings.Contains(lines[i][1], unit.Text) {
			t.Fatalf("prompt line %d = %q, want unit %q", i, lines[i][1], unit.Text)
		}
	}
	for i := 1; i < len(asm.Units); i++ {
		if kindRank(asm.Units[i-1].Kind) > kindRank(asm.Units[i].Kind) {
			t.Fatalf("fact crossed chunk in units: %#v", asm.Units)
		}
	}
}

type evidenceLineCountStub struct{}

func (evidenceLineCountStub) CountInput(_ context.Context, input evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{
		InputTokens: len(numberedEvidenceLineRE.FindAllStringSubmatch(input.User, -1)),
		Fingerprint: "evidence-lines",
	}, nil
}

func TestMultiHopCapKeepsCanonicalPrefix(t *testing.T) {
	hits := []memory.Result{
		hit("fact-a", "Alice Smith derived fact", 99),
		hit("chunk-b", "Bob Jones raw chunk", 3),
		hit("chunk-a", "Alice Smith raw chunk", 4),
		hit("fact-b", "Bob Jones derived fact", 98),
		hit("chunk-z", "plain raw chunk", 2),
	}
	canonical := groupHitsByEntity(hits)
	counter := &assemblyTokenCounter{counter: evidenceLineCountStub{}, answerModel: "stub"}
	cfg := testAssemblyConfig()
	cfg.Cap = 3
	asm, prompt, err := assembleEvidence(t.Context(), "q", hits, assemblyCategoryMultiHop, cfg, counter)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	want := multiHopIDs(canonical[:3])
	got := make([]string, 0, len(asm.Units))
	for _, unit := range asm.Units {
		got = append(got, unit.SourceID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capped IDs = %v, want canonical prefix %v", got, want)
	}
	if len(numberedEvidenceLineRE.FindAllStringSubmatch(prompt, -1)) != len(asm.Units) {
		t.Fatalf("prompt and units diverged after cap:\n%s", prompt)
	}
}

func TestMultiHopEntityHeaderCanRepeatAcrossKinds(t *testing.T) {
	hits := []memory.Result{
		hit("fact-a", "Alice Smith derived fact", 9),
		hit("chunk-a", "Alice Smith raw chunk", 1),
		hit("chunk-b", "Bob Jones raw chunk", 2),
	}
	_, prompt, err := assembleEvidence(
		t.Context(), "q", hits, assemblyCategoryMultiHop, testAssemblyConfig(), nil,
	)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	if got := strings.Count(prompt, "[entity: Alice Smith]"); got != 2 {
		t.Fatalf("Alice headers = %d, want 2 across chunk/fact layers\n%s", got, prompt)
	}
	if strings.Index(prompt, "Alice Smith derived fact") < strings.Index(prompt, "Bob Jones raw chunk") {
		t.Fatalf("fact rendered before all chunks:\n%s", prompt)
	}
}

func TestMultiHopLegacyOrderParityAndAudit(t *testing.T) {
	hits := []memory.Result{
		hit("fact-alice", "Alice Smith derived fact", 99),
		hit("chunk-bob", "Bob Jones raw chunk", 3),
		hit("fact-bob", "Bob Jones derived fact", 98),
		hit("chunk-alice", "Alice Smith raw chunk", 4),
		hit("fact-z", "plain derived fact", 97),
		hit("chunk-z", "plain raw chunk", 2),
	}
	cfg := testAssemblyConfig()
	cfg.EntityOrder = assemblyEntityOrderLegacyGrouped
	asm, prompt, err := assembleEvidence(t.Context(), "q", hits, assemblyCategoryMultiHop, cfg, nil)
	if err != nil {
		t.Fatalf("assembleEvidence: %v", err)
	}
	want := []string{
		"fact-alice", "chunk-alice", "fact-bob", "chunk-bob", "fact-z", "chunk-z",
	}
	got := make([]string, 0, len(asm.Units))
	for _, unit := range asm.Units {
		got = append(got, unit.SourceID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy IDs = %v, want pre-033 order %v", got, want)
	}
	if asm.EntityOrder != assemblyEntityOrderLegacyGrouped {
		t.Fatalf("entity_order = %q, want legacy_grouped", asm.EntityOrder)
	}
	if prompt != buildLegacyEntityAnswerPrompt("q", unitsToResults(asm.Units), cfg.CurrentDate) {
		t.Fatalf("legacy renderer parity failed:\n%s", prompt)
	}
}

func TestMultiHopEntityOrderAuditAndInputClosure(t *testing.T) {
	hits := []memory.Result{
		hit("fact-a", "Alice Smith fact", 9), hit("chunk-a", "Alice Smith chunk", 1),
	}
	normal, _, err := assembleEvidence(
		t.Context(), "q", hits, assemblyCategoryMultiHop, testAssemblyConfig(), nil,
	)
	if err != nil {
		t.Fatalf("normal assemble: %v", err)
	}
	legacyCfg := testAssemblyConfig()
	legacyCfg.EntityOrder = assemblyEntityOrderLegacyGrouped
	legacy, _, err := assembleEvidence(
		t.Context(), "q", hits, assemblyCategoryMultiHop, legacyCfg, nil,
	)
	if err != nil {
		t.Fatalf("legacy assemble: %v", err)
	}
	if normal.EntityOrder != assemblyEntityOrderKindLayered {
		t.Fatalf("normal entity_order = %q", normal.EntityOrder)
	}
	if normal.InputCandidateCount != len(hits) || legacy.InputCandidateCount != len(hits) {
		t.Fatalf("input counts = %d/%d, want %d", normal.InputCandidateCount, legacy.InputCandidateCount, len(hits))
	}
	if normal.InputClosureSHA256 == "" || normal.InputClosureSHA256 != legacy.InputClosureSHA256 {
		t.Fatalf("input closure digests = %q/%q", normal.InputClosureSHA256, legacy.InputClosureSHA256)
	}
}

func TestNonMultiAssemblyModesAreByteIdentical(t *testing.T) {
	hits := []memory.Result{
		hit("fact-a", "derived fact", 9), hit("chunk-a", "raw chunk", 1),
	}
	categoryNames := map[int]string{
		temporalCategory: "temporal", assemblyCategoryOpenDomain: "open-domain",
		assemblyCategorySingleHop: "single-hop",
	}
	for _, category := range []int{temporalCategory, assemblyCategoryOpenDomain, assemblyCategorySingleHop} {
		t.Run(categoryNames[category], func(t *testing.T) {
			normal, normalPrompt, err := assembleEvidence(
				t.Context(), "q", hits, category, testAssemblyConfig(), nil,
			)
			if err != nil {
				t.Fatalf("normal assemble: %v", err)
			}
			legacyCfg := testAssemblyConfig()
			legacyCfg.EntityOrder = assemblyEntityOrderLegacyGrouped
			legacy, legacyPrompt, err := assembleEvidence(
				t.Context(), "q", hits, category, legacyCfg, nil,
			)
			if err != nil {
				t.Fatalf("legacy assemble: %v", err)
			}
			if !reflect.DeepEqual(normal, legacy) || normalPrompt != legacyPrompt {
				t.Fatalf("category %d changed under ignored legacy mode", category)
			}
			if normal.EntityOrder != assemblyEntityOrderNotApplicable {
				t.Fatalf("category %d entity_order = %q", category, normal.EntityOrder)
			}
		})
	}
}

// fixedCountStub is an offline token counter returning a fixed count.
type fixedCountStub struct {
	count int
}

func (f *fixedCountStub) CountInput(_ context.Context, _ evidencecompiler.AnswerInput) (evidencecompiler.TokenCount, error) {
	return evidencecompiler.TokenCount{InputTokens: f.count, Fingerprint: "stub"}, nil
}
