package main

import (
	"context"
	"strconv"
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
		ctx, protocol, opt, retriever, projections, nil, qa, nil, map[string]string{"D1:1": evidence[0].ID},
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

// compilerArmSourceSpec describes one Evidence message backing a formal anchor.
// A zero start/end denotes a FullSource reference (KEEP); otherwise the rune
// half-open span [start, end) is an EXTRACT partial span.
type compilerArmSourceSpec struct {
	datasetID  string
	content    string
	sessionID  string
	speaker    string
	occurredAt time.Time
	start      int
	end        int
}

// compilerArmQuestionSpec describes a single eval question whose anchors are
// each a retrieval-marker projection backed by one or more Evidence sources.
// Every projection's content carries compilerArmRetrievalToken so FTS surfaces
// all anchors for the one question — the question stays single (单题).
type compilerArmQuestionSpec struct {
	questionID string
	category   int
	anchors    [][]compilerArmSourceSpec
	tokenCap   int // 0 => keep the sourceTestProtocol default
}

const compilerArmRetrievalToken = "needlefact"

// materializeCompilerArmSpec drives one question end-to-end through the
// compiler arm (materializeFormalB1Question with compilerArm="extractive"),
// exactly the path the remote eval GPU runs. It needs no endpoint: retrieval is
// FTS-only (nil embedder) and the compiler is answerer-free. This is the shared
// single-question fixture for the shape matrix below.
func materializeCompilerArmSpec(t *testing.T, spec compilerArmQuestionSpec) (
	formalFrozenQuestion, evalProtocol, options, locomoQA, map[string]memory.Evidence,
) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	entries := memory.NewEntryStore(st.DB())
	ledger := entries.Ledger()
	evidenceByID := make(map[string]memory.Evidence)
	turnEvidence := make(map[string]string)
	var qaEvidence []string

	for anchorIdx, sources := range spec.anchors {
		refs := make([]memory.EvidenceRef, 0, len(sources))
		for srcIdx, src := range sources {
			input := memory.EvidenceInput{
				ExternalSourceID: src.datasetID,
				SourceType:       memory.EvidenceMessage,
				SourceSessionID:  src.sessionID,
				Speaker:          src.speaker,
				Ordinal:          srcIdx,
				Content:          src.content,
				RecordedAt:       time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
			}
			if !src.occurredAt.IsZero() {
				occurred := src.occurredAt
				input.OccurredAt = &occurred
			}
			created, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{input})
			if err != nil {
				t.Fatalf("append evidence %s: %v", src.datasetID, err)
			}
			evidenceByID[created[0].ID] = created[0]
			turnEvidence[src.datasetID] = created[0].ID
			qaEvidence = append(qaEvidence, src.datasetID)

			ref := memory.EvidenceRef{EvidenceID: created[0].ID, SourceOrder: srcIdx}
			if src.start == 0 && src.end == 0 {
				ref.FullSource = true
			} else {
				start, end := src.start, src.end
				span := string([]rune(src.content)[start:end])
				ref.StartChar = &start
				ref.EndChar = &end
				ref.SpanDigest = strings.TrimPrefix(evalTextDigest(span), "sha256:")
			}
			refs = append(refs, ref)
		}
		entry := &memory.Entry{
			Name:     "proj-" + strconv.Itoa(anchorIdx),
			Trigger:  "retrieval-marker",
			Content:  compilerArmRetrievalToken + " anchor-" + strconv.Itoa(anchorIdx),
			Category: "fact",
		}
		if err := entries.UpsertWithSources(ctx, entry, refs); err != nil {
			t.Fatalf("upsert anchor %d: %v", anchorIdx, err)
		}
	}

	protocol := sourceTestProtocol()
	if spec.tokenCap > 0 {
		protocol.Budget.AnswerInputTokenCap = spec.tokenCap
	}
	qa := locomoQA{
		QuestionID: spec.questionID,
		Question:   compilerArmRetrievalToken,
		Category:   spec.category,
		Evidence:   qaEvidence,
	}
	opt := options{
		answerModel:    protocol.Models.Answerer.ID,
		formalCounter:  lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
		formalEvidence: ledger,
		compilerArm:    "extractive",
	}
	retriever := memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil)
	projections := memory.NewProjectionStore(st.DB())
	frozen := materializeFormalB1Question(ctx, protocol, opt, retriever, projections, nil, qa, nil, turnEvidence)
	return frozen, protocol, opt, qa, evidenceByID
}

// assertCompilerArmQuestionValid is the shared "zero per-question invalid"
// invariant block. It re-asserts every formal gate the answerer path enforces
// before any provider call: no invalid reasons, the frozen-payload validator
// (source/span/citation + anchor prefix + active Ledger receipt), cross-artifact
// digest identity, an independent active-source reread, 100% active Ledger
// evidence, and a recorded preflight token count. Every valid shape below must
// pass this unchanged.
func assertCompilerArmQuestionValid(
	t *testing.T, ctx context.Context,
	frozen formalFrozenQuestion, protocol evalProtocol, opt options, qa locomoQA,
	evidenceByID map[string]memory.Evidence,
) {
	t.Helper()
	if len(frozen.InvalidReasons) != 0 {
		t.Fatalf("compiler-arm materialization invalid: %v", frozen.InvalidReasons)
	}
	if err := validateFormalFrozenPayload(protocol, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("compiler-arm bundle failed frozen-payload validator (would be flagged source_span_or_citation_invalid): %v", err)
	}
	if frozen.Trace.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.CandidateSetDigest != frozen.Candidate.CandidateSetDigest ||
		frozen.Bundle.TraceDigest != frozen.Trace.TraceDigest ||
		frozen.Bundle.RenderedDigest != evalTextDigest(frozen.Bundle.RenderedContext) {
		t.Fatalf("compiler-arm cross-artifact digest identity drift (would be flagged answer_input_drift)")
	}
	if err := validateActiveFormalBundle(ctx, opt.formalEvidence, protocol, opt, qa, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("compiler-arm bundle rejected by independent active-source validator: %v", err)
	}
	if len(frozen.Bundle.Items) == 0 {
		t.Fatal("compiler-arm produced an empty bundle (no evidence fit token cap)")
	}
	allowed := make(map[string]bool, len(evidenceByID))
	for _, ev := range evidenceByID {
		allowed[ev.ID] = true
	}
	for _, item := range frozen.Bundle.Items {
		for _, src := range item.Sources {
			if !allowed[src.EvidenceID] {
				t.Fatalf("compiler-arm bundle cites non-ledger source %q (fabrication)", src.EvidenceID)
			}
		}
	}
	if frozen.Bundle.AnswerInputTokens <= 0 {
		t.Fatalf("compiler-arm bundle has no preflight token count: %d", frozen.Bundle.AnswerInputTokens)
	}
	if frozen.Bundle.CounterFingerprint != protocol.Budget.CounterFingerprint {
		t.Fatalf("compiler-arm counter fingerprint = %q, want %q", frozen.Bundle.CounterFingerprint, protocol.Budget.CounterFingerprint)
	}
}

// TestFormalB1CompilerArmQuestionShapes thickens the compiler-arm offline
// guard from one synthetic shape (single anchor / single source / unicode
// EXTRACT) to the shapes the real eval GPU actually submits, each targeted at
// one of the four contract violations fixed in 56cef8b plus the untested
// compiler-arm budget-impossible path. They are all single-question (单题),
// offline, and answerer-free — the cheap gate that must stay green before any
// remote eval budget is spent on the compiler arm.
func TestFormalB1CompilerArmQuestionShapes(t *testing.T) {
	ctx := context.Background()
	day := func(m time.Month, d int) time.Time { return time.Date(2024, m, d, 0, 0, 0, 0, time.UTC) }

	validShapes := []struct {
		name     string
		spec     compilerArmQuestionSpec
		validate func(t *testing.T, frozen formalFrozenQuestion, evidenceByID map[string]memory.Evidence)
	}{
		{
			// Bug #3 (RenderedCandidates overwritten) + bug #2 (item text was the
			// compiler's grounded sentence, not the verbatim source). Two complete
			// anchors must both survive compilation with exact source text.
			name: "multi_anchor_full_source_keep",
			spec: compilerArmQuestionSpec{
				questionID: "locomo:multi:keep",
				category:   4,
				anchors: [][]compilerArmSourceSpec{
					{{datasetID: "D1:1", content: "Caroline will visit Kyoto in March.", sessionID: "sess-a", speaker: "Caroline", occurredAt: day(time.March, 1)}},
					{{datasetID: "D1:2", content: "She booked a small ryokan near Lake Biwa.", sessionID: "sess-b", speaker: "Caroline", occurredAt: day(time.April, 2)}},
				},
			},
			validate: func(t *testing.T, frozen formalFrozenQuestion, evidenceByID map[string]memory.Evidence) {
				if len(frozen.Bundle.Items) != 2 {
					t.Fatalf("want 2 bundle items (one per anchor), got %d", len(frozen.Bundle.Items))
				}
				if len(frozen.Candidate.RenderedCandidates) != 2 {
					t.Fatalf("RenderedCandidates = %d, want 2 (compiler must not overwrite expansion)", len(frozen.Candidate.RenderedCandidates))
				}
				texts := make(map[string]bool, 2)
				for _, item := range frozen.Bundle.Items {
					if item.Kind != "KEEP" {
						t.Fatalf("item kind = %q, want KEEP for FullSource", item.Kind)
					}
					texts[item.Text] = true
				}
				for _, ev := range evidenceByID {
					if !texts[ev.Content] {
						t.Fatalf("KEEP item text is not the verbatim Evidence content %q (compiler grounded-sentence regression)", ev.Content)
					}
				}
			},
		},
		{
			// Bug #4 (resultHits lacked SourceSessionID/EventDate, so the stored
			// RenderedContext could not be rebuilt per-source). One anchor backed by
			// two distinct-session messages must keep both sources reconstructable.
			name: "multi_source_single_anchor",
			spec: compilerArmQuestionSpec{
				questionID: "locomo:multi:source",
				category:   4,
				anchors: [][]compilerArmSourceSpec{
					{
						{datasetID: "D1:1", content: "Caroline adopted a corgi named Biscuit.", sessionID: "sess-a", speaker: "Caroline", occurredAt: day(time.May, 1)},
						{datasetID: "D1:2", content: "Biscuit loves morning walks by the canal.", sessionID: "sess-b", speaker: "Caroline", occurredAt: day(time.June, 2)},
					},
				},
			},
			validate: func(t *testing.T, frozen formalFrozenQuestion, evidenceByID map[string]memory.Evidence) {
				if len(frozen.Candidate.Anchors) != 1 {
					t.Fatalf("want 1 anchor, got %d", len(frozen.Candidate.Anchors))
				}
				if len(frozen.Bundle.Items) != 2 {
					t.Fatalf("want 2 bundle items (both sources of the single anchor), got %d", len(frozen.Bundle.Items))
				}
				for _, ev := range evidenceByID {
					if !strings.Contains(frozen.Bundle.RenderedContext, ev.Content) {
						t.Fatalf("multi-source RenderedContext lost evidence %q (per-source session/date collapsed): %q", ev.Content, frozen.Bundle.RenderedContext)
					}
				}
			},
		},
		{
			// Bug #1 (TextDigest carried the "sha256:" prefix; sameDigest wants bare
			// hex) + bug #2 for EXTRACT. A unicode partial span must compile to the
			// verbatim span text, not the grounded sentence.
			name: "extract_span_and_keep_mix",
			spec: compilerArmQuestionSpec{
				questionID: "locomo:mix:extract",
				category:   4,
				anchors: [][]compilerArmSourceSpec{
					{{datasetID: "D1:1", content: "前缀🙂中文后缀", sessionID: "sess-a", speaker: "Caroline", occurredAt: day(time.January, 2), start: 2, end: 5}},
					{{datasetID: "D1:2", content: "She prefers trains over planes for long trips.", sessionID: "sess-b", speaker: "Caroline", occurredAt: day(time.February, 3)}},
				},
			},
			validate: func(t *testing.T, frozen formalFrozenQuestion, evidenceByID map[string]memory.Evidence) {
				if len(frozen.Bundle.Items) != 2 {
					t.Fatalf("want 2 bundle items, got %d", len(frozen.Bundle.Items))
				}
				const extractSpan = "🙂中文"
				gotExtract, gotKeep := false, false
				for _, item := range frozen.Bundle.Items {
					switch item.Kind {
					case "EXTRACT":
						gotExtract = true
						if item.Text != extractSpan {
							t.Fatalf("EXTRACT item text = %q, want verbatim unicode span %q", item.Text, extractSpan)
						}
					case "KEEP":
						gotKeep = true
					default:
						t.Fatalf("unexpected item kind %q", item.Kind)
					}
				}
				if !gotExtract || !gotKeep {
					t.Fatalf("want one EXTRACT and one KEEP item, got extract=%t keep=%t", gotExtract, gotKeep)
				}
			},
		},
	}

	for _, tc := range validShapes {
		t.Run(tc.name, func(t *testing.T) {
			frozen, protocol, opt, qa, evidenceByID := materializeCompilerArmSpec(t, tc.spec)
			tc.validate(t, frozen, evidenceByID)
			assertCompilerArmQuestionValid(t, ctx, frozen, protocol, opt, qa, evidenceByID)
		})
	}

	// Compiler-arm budget-impossible wiring is otherwise uncovered: the legacy
	// packer's over-cap path is tested, but the compiler arm's compileErr →
	// invalid propagation is not. A cap below the static answer prompt alone must
	// be flagged honestly, never silently yield a "valid" empty bundle.
	t.Run("budget_impossible_propagates_as_invalid", func(t *testing.T) {
		spec := compilerArmQuestionSpec{
			questionID: "locomo:budget:impossible",
			category:   4,
			tokenCap:   8, // smaller than the static answer prompt alone
			anchors: [][]compilerArmSourceSpec{
				{{datasetID: "D1:1", content: "short evidence", sessionID: "sess-a", speaker: "Caroline", occurredAt: day(time.January, 2)}},
			},
		}
		frozen, _, _, _, _ := materializeCompilerArmSpec(t, spec)
		if !hasInvalidReason(frozen.InvalidReasons, "answer_input_budget_impossible") {
			t.Fatalf("compiler-arm over-cap question not flagged budget-impossible: %v", frozen.InvalidReasons)
		}
		if !hasInvalidReason(frozen.InvalidReasons, "no_evidence_fits_token_cap") {
			t.Fatalf("compiler-arm over-cap question not flagged no-evidence-fits: %v", frozen.InvalidReasons)
		}
		if frozen.Bundle.Valid || frozen.Trace.Valid {
			t.Fatalf("over-cap question left a valid bundle/trace: bundle.Valid=%t trace.Valid=%t", frozen.Bundle.Valid, frozen.Trace.Valid)
		}
		if len(frozen.Bundle.Items) != 0 {
			t.Fatalf("over-cap question produced %d bundle items (want 0)", len(frozen.Bundle.Items))
		}
	})
}
