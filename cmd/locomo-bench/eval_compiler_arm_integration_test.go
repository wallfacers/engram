package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

// TestFormalB1CompilerArmMaterializesValidBundle is the offline closure-review
// guard for the US3 compiler-arm wiring. It drives the compiler arm end-to-end
// through materializeFormalB1Question and asserts the formal invariants that
// prepareFrozenFormalB1Answer enforces before any provider call:
//   - no invalid reasons (passes revalidateFrozenFormalSources + structural
//     candidate/trace/bundle validators inside materialize)
//   - validateFormalFrozenPayload passes (the :499 source_span_or_citation gate)
//   - cross-artifact digest identity holds (the :503-513 answer_input_drift gate)
//   - sources are 100% active Ledger evidence (no fabrication)
//
// Extractive Compile is answerer-free and resolver/counter-bounded, so this
// needs no GPU/endpoint — it is the cheap gate that must pass before spending
// remote eval budget on the compiler arm. The default chunk_900 path is already
// covered by eval_source_bundle_test.go; this fills the compiler-arm coverage
// gap. It caught multiple wiring bugs (TextDigest format, span/text identity,
// RenderedCandidates overwrite, RenderedContext reconstruction) before any GPU
// spend.
func TestFormalB1CompilerArmMaterializesValidBundle(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	entries := memory.NewEntryStore(st.DB())
	ledger := entries.Ledger()
	const sourceText = "前缀🙂中文后缀"
	evidence, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:1",
		SourceType:       memory.EvidenceMessage,
		SourceSessionID:  "conv0-sess1",
		Speaker:          "Caroline",
		Ordinal:          0,
		Content:          sourceText,
		OccurredAt:       ptrTime(time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)),
		RecordedAt:       time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	start, end := 2, 5
	const spanText = "🙂中文"
	if err := entries.UpsertWithSources(ctx, &memory.Entry{
		Name: "projection-only", Trigger: "retrieval-marker", Content: "retrieval-marker PROJECTION_ONLY_SHORT", Category: "fact",
	}, []memory.EvidenceRef{{
		EvidenceID: evidence[0].ID, SourceOrder: 0, StartChar: &start, EndChar: &end,
		SpanDigest: strings.TrimPrefix(evalTextDigest(spanText), "sha256:"),
	}}); err != nil {
		t.Fatal(err)
	}

	protocol := sourceTestProtocol()
	qa := locomoQA{
		QuestionID: "locomo:0:0", Question: "PROJECTION_ONLY_SHORT", Category: 4,
		Evidence: []string{"D1:1"},
	}
	opt := options{
		answerModel:    protocol.Models.Answerer.ID,
		formalCounter:  lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
		formalEvidence: ledger,
		compilerArm:    "extractive",
	}
	retriever := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil)
	projections := memory.NewProjectionStore(st.DB())
	frozen := materializeFormalB1Question(
		ctx, protocol, opt, retriever, projections, qa, nil, map[string]string{"D1:1": evidence[0].ID},
	)

	// (1) Compiler arm must not produce invalid reasons.
	if len(frozen.InvalidReasons) != 0 {
		t.Fatalf("compiler-arm materialization invalid: %v", frozen.InvalidReasons)
	}
	// (2) Independent source-span/citation validator (the prepareFrozenFormalB1Answer :499 gate).
	if err := validateFormalFrozenPayload(protocol, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("compiler-arm bundle failed frozen-payload validator (would be flagged source_span_or_citation_invalid): %v", err)
	}
	// (3) Cross-artifact digest identity — the :503-513 answer_input_drift gate.
	if frozen.Trace.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.TraceDigest != frozen.Trace.TraceDigest ||
		frozen.Bundle.RenderedDigest != evalTextDigest(frozen.Bundle.RenderedContext) {
		t.Fatalf("compiler-arm cross-artifact digest identity drift (would be flagged answer_input_drift)")
	}
	// (4) Sources are 100% active Ledger evidence (no fabrication).
	if err := validateActiveFormalBundle(ctx, ledger, protocol, opt, qa, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("compiler-arm bundle rejected by independent active-source validator: %v", err)
	}
	if len(frozen.Bundle.Items) == 0 {
		t.Fatal("compiler-arm produced an empty bundle (no evidence fit token cap)")
	}
	for _, item := range frozen.Bundle.Items {
		for _, src := range item.Sources {
			if src.EvidenceID != evidence[0].ID {
				t.Fatalf("compiler-arm bundle cites non-ledger source %q (want %q)", src.EvidenceID, evidence[0].ID)
			}
		}
	}
	// (5) Preflight token count recorded under the frozen counter fingerprint.
	if frozen.Bundle.AnswerInputTokens <= 0 {
		t.Fatalf("compiler-arm bundle has no preflight token count: %d", frozen.Bundle.AnswerInputTokens)
	}
	if frozen.Bundle.CounterFingerprint != protocol.Budget.CounterFingerprint {
		t.Fatalf("compiler-arm counter fingerprint = %q, want %q", frozen.Bundle.CounterFingerprint, protocol.Budget.CounterFingerprint)
	}
}
