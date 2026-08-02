package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
)

func TestFormalB1SourceExpansionUsesActiveEvidenceAndUnicodeSpan(t *testing.T) {
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
	}
	frozen := materializeFormalB1Question(
		ctx, protocol, opt,
		memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil),
		memory.NewProjectionStore(st.DB()), nil, qa, nil, map[string]string{"D1:1": evidence[0].ID},
	)
	if len(frozen.InvalidReasons) != 0 {
		t.Fatalf("source-backed materialization invalid: %v", frozen.InvalidReasons)
	}
	if len(frozen.Candidate.Anchors) != 1 || frozen.Candidate.Anchors[0].TextDigest != evalTextDigest("retrieval-marker PROJECTION_ONLY_SHORT") {
		t.Fatalf("navigation anchor did not preserve projection identity: %+v", frozen.Candidate.Anchors)
	}
	if len(frozen.Candidate.RenderedCandidates) != 1 || frozen.Candidate.RenderedCandidates[0].Text != spanText {
		t.Fatalf("rendered candidate = %+v, want exact active Evidence span %q", frozen.Candidate.RenderedCandidates, spanText)
	}
	if strings.Contains(frozen.Bundle.RenderedContext, "retrieval-marker PROJECTION_ONLY_SHORT") || !strings.Contains(frozen.Bundle.RenderedContext, spanText) {
		t.Fatalf("answer context did not replace projection text with Evidence span: %q", frozen.Bundle.RenderedContext)
	}
	if len(frozen.Bundle.Items) != 1 || frozen.Bundle.Items[0].Text != spanText ||
		len(frozen.Bundle.Items[0].Sources) != 1 {
		t.Fatalf("bundle items = %+v, want one cited Evidence span", frozen.Bundle.Items)
	}
	activeSourceOK, activeSpanOK, activeCitationOK := inspectFormalActiveValidation(frozen.Bundle.ActiveValidation)
	if !activeSourceOK || !activeSpanOK || !activeCitationOK {
		t.Fatalf("materialized Bundle has no valid independent Ledger receipt: %+v", frozen.Bundle.ActiveValidation)
	}
	gotSpan := frozen.Bundle.Items[0].Sources[0]
	if gotSpan.EvidenceID != evidence[0].ID || gotSpan.StartChar != start || gotSpan.EndChar != end ||
		gotSpan.SpanDigest != evalTextDigest(spanText) {
		t.Fatalf("bundle span = %+v, want [%d,%d) over %q", gotSpan, start, end, spanText)
	}
	if err := validateActiveFormalBundle(ctx, ledger, protocol, opt, qa, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("independent active-source validator rejected valid bundle: %v", err)
	}
	for _, sourceType := range []memory.EvidenceSourceType{memory.EvidenceDirectWrite, memory.EvidenceLegacyEntry} {
		t.Run("active validator rejects "+string(sourceType), func(t *testing.T) {
			nonMessage := evidence[0]
			nonMessage.SourceType = sourceType
			reader := formalEvidenceReaderFunc(func(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
				if len(ids) != 1 || ids[0] != nonMessage.ID {
					return nil, fmt.Errorf("unexpected source request: %v", ids)
				}
				return map[string]memory.Evidence{nonMessage.ID: nonMessage}, nil
			})
			if err := validateActiveFormalBundle(ctx, reader, protocol, opt, qa, frozen.Candidate, frozen.Trace, frozen.Bundle); err == nil {
				t.Fatalf("active validator accepted answer-facing %s Evidence", sourceType)
			}
		})
	}

	for _, mutate := range []struct {
		name string
		fn   func(*formalFrozenQuestion)
	}{
		{name: "offset", fn: func(value *formalFrozenQuestion) { value.Bundle.Items[0].Sources[0].StartChar = -1 }},
		{name: "span digest", fn: func(value *formalFrozenQuestion) {
			value.Bundle.Items[0].Sources[0].SpanDigest = fixtureDigest("other")
		}},
		{name: "item text", fn: func(value *formalFrozenQuestion) { value.Bundle.Items[0].Text = "tampered" }},
		{name: "citation candidate", fn: func(value *formalFrozenQuestion) { value.Bundle.Items[0].CandidateIDs[0] = "unknown" }},
		{name: "source union", fn: func(value *formalFrozenQuestion) { value.Bundle.SourceIDs = append(value.Bundle.SourceIDs, "unknown") }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			tampered := cloneFormalFrozenQuestion(frozen)
			mutate.fn(&tampered)
			if err := validateActiveFormalBundle(ctx, ledger, protocol, opt, qa, tampered.Candidate, tampered.Trace, tampered.Bundle); err == nil {
				t.Fatal("independent validator accepted tampered item/span/citation")
			}
		})
	}
	citationOnly := cloneFormalFrozenQuestion(frozen)
	citationOnly.Bundle.Items[0].CandidateIDs[0] = "unknown"
	receipt, err := validateActiveFormalBundleReceipt(
		ctx, ledger, protocol, opt, qa,
		citationOnly.Candidate, citationOnly.Trace, citationOnly.Bundle,
	)
	if err == nil {
		t.Fatal("citation-only tamper unexpectedly passed active validation")
	}
	sourceOK, spanOK, citationOK := inspectFormalActiveValidation(persistFormalActiveValidation(receipt))
	if !sourceOK || !spanOK || citationOK {
		t.Fatalf(
			"citation-only failure collapsed independent validity dimensions: source=%t span=%t citation=%t receipt=%+v",
			sourceOK, spanOK, citationOK, receipt,
		)
	}
	revisionDrift := evidence[0]
	revisionDrift.Revision++
	driftOpt := opt
	driftOpt.formalEvidence = formalEvidenceReaderFunc(func(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
		if len(ids) != 1 || ids[0] != revisionDrift.ID {
			return nil, fmt.Errorf("unexpected source request: %v", ids)
		}
		return map[string]memory.Evidence{revisionDrift.ID: revisionDrift}, nil
	})
	drifted := revalidateFrozenFormalSources(ctx, protocol, driftOpt, qa, frozen)
	if !hasInvalidReason(drifted.InvalidReasons, "source_state_drift") {
		t.Fatalf("active Evidence revision drift did not invalidate byte replay: %+v", drifted)
	}

	noItems := cloneFormalFrozenQuestion(frozen)
	noItems.Bundle.Items = nil
	noItems.Bundle.SourceValid = true
	if err := validateFormalFrozenPayload(protocol, noItems.Candidate, noItems.Trace, noItems.Bundle); err == nil {
		t.Fatal("SourceValid=true without item/span/citation passed artifact validation")
	}

	if err := ledger.Tombstone(ctx, memory.LifecycleRequest{
		EvidenceID: evidence[0].ID, RequestID: "test-tombstone", ReasonCode: "test",
	}); err != nil {
		t.Fatal(err)
	}
	revalidated := revalidateFrozenFormalSources(ctx, protocol, opt, qa, frozen)
	activeSourceOK, activeSpanOK, activeCitationOK = inspectFormalActiveValidation(revalidated.Bundle.ActiveValidation)
	if activeSourceOK || activeSpanOK || activeCitationOK {
		t.Fatalf("inactive Evidence retained a passing validation receipt: %+v", revalidated.Bundle.ActiveValidation)
	}
	answerCalls, judgeCalls := 0, 0
	_, _, _, run := answerFrozenFormalB1Question(
		ctx, protocol, opt,
		func(context.Context, string, string) (string, provider.Usage, error) {
			answerCalls++
			return "unexpected", provider.Usage{}, nil
		},
		func(context.Context, string, string) (string, provider.Usage, error) {
			judgeCalls++
			return `{"correct":true}`, provider.Usage{}, nil
		},
		qa, revalidated, 1,
	)
	if answerCalls != 0 || judgeCalls != 0 || !hasInvalidReason(run.InvalidReasons, "source_span_or_citation_invalid") {
		t.Fatalf("inactive Evidence reached model calls: answer=%d judge=%d reasons=%v", answerCalls, judgeCalls, run.InvalidReasons)
	}
}

func TestFormalB1SourceExpansionCountsExpandedBytesBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	entries := memory.NewEntryStore(st.DB())
	ledger := entries.Ledger()
	raw := strings.Repeat("很长的原始证据🙂", 120)
	evidence, err := ledger.AppendBatch(ctx, []memory.EvidenceInput{{
		ExternalSourceID: "D1:1", SourceType: memory.EvidenceMessage,
		SourceSessionID: "conv0-sess1", Speaker: "Caroline", Ordinal: 0,
		Content: raw, RecordedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := entries.UpsertWithSources(ctx, &memory.Entry{
		Name: "tiny-fact", Trigger: "needle", Content: "needle", Category: "fact",
	}, []memory.EvidenceRef{{EvidenceID: evidence[0].ID, SourceOrder: 0, FullSource: true}}); err != nil {
		t.Fatal(err)
	}

	protocol := sourceTestProtocol()
	protocol.Budget.AnswerInputTokenCap = 300
	qa := locomoQA{QuestionID: "locomo:0:0", Question: "needle?", Category: 4, Evidence: []string{"D1:1"}}
	opt := options{
		answerModel: protocol.Models.Answerer.ID, formalEvidence: ledger,
		formalCounter: lengthCounter{fingerprint: protocol.Budget.CounterFingerprint},
	}
	frozen := materializeFormalB1Question(
		ctx, protocol, opt,
		memory.NewRetriever(entries, memory.NewVectorStore(st.DB()), nil),
		memory.NewProjectionStore(st.DB()), nil, qa, nil, map[string]string{"D1:1": evidence[0].ID},
	)
	if !hasInvalidReason(frozen.InvalidReasons, "no_evidence_fits_token_cap") ||
		!hasInvalidReason(frozen.InvalidReasons, "answer_input_budget_impossible") {
		t.Fatalf("expanded over-cap source was admitted using short projection bytes: %v", frozen.InvalidReasons)
	}
	if len(frozen.Bundle.Items) != 0 || strings.Contains(frozen.Bundle.RenderedContext, "needle\n") {
		t.Fatalf("over-cap source leaked a partial/projection bundle: %+v", frozen.Bundle)
	}
}

func sourceTestProtocol() evalProtocol {
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	protocol.Retrieval.Recipe = "fts"
	protocol.Retrieval.CandidateLimit = 5
	protocol.Budget.CandidateLimit = 5
	protocol.Budget.AnswerInputTokenCap = 100_000
	protocol.Budget.CounterFingerprint = "sha256:length"
	return protocol
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func TestFormalRenderedSourceIDsDoNotCollideOnDuplicateEntryNames(t *testing.T) {
	left := memory.Result{ID: "left", ProjectionID: "projection-left", Name: "duplicate"}
	right := memory.Result{ID: "right", ProjectionID: "projection-right", Name: "duplicate"}
	if formalCandidateID(left) == formalCandidateID(right) {
		t.Fatalf("fixture candidate IDs unexpectedly collide")
	}
	if got := formalRenderedSourceID(formalCandidateID(left), 0, "evidence"); got ==
		formalRenderedSourceID(formalCandidateID(right), 0, "evidence") {
		t.Fatalf("rendered source IDs collided across duplicate names: %s", got)
	}
	if got := formalRenderedSourceID(formalCandidateID(left), 0, "evidence"); !strings.Contains(got, fmt.Sprint("projection-left")) {
		t.Fatalf("rendered source ID %q does not retain stable candidate identity", got)
	}
}

func TestCloneFormalFrozenQuestionDeepCopiesBundleCitations(t *testing.T) {
	protocol := sourceTestProtocol()
	candidate := testCandidateArtifact()
	candidate.ProtocolHash = protocol.ProtocolHash
	trace := buildFormalTrace(protocol, candidate.QuestionID, candidate)
	bundle := testFormalBundle(
		protocol, candidate, trace, "frozen context", 12, fixtureDigest("system"),
	)
	original := formalFrozenQuestion{Candidate: candidate, Trace: trace, Bundle: bundle}
	cloned := cloneFormalFrozenQuestion(original)

	cloned.Bundle.Items[0].CandidateIDs[0] = "tampered-candidate"
	cloned.Bundle.Items[0].Sources[0].StartChar = 9
	if original.Bundle.Items[0].CandidateIDs[0] == "tampered-candidate" ||
		original.Bundle.Items[0].Sources[0].StartChar == 9 {
		t.Fatal("nested Bundle citation mutation contaminated frozen replay state")
	}
}

func TestFormalEvidenceExpansionRejectsNonMessageEvidence(t *testing.T) {
	for _, sourceType := range []memory.EvidenceSourceType{memory.EvidenceDirectWrite, memory.EvidenceLegacyEntry} {
		t.Run(string(sourceType), func(t *testing.T) {
			evidence := formalSourceTestEvidence("evidence-"+string(sourceType), "projection snapshot", sourceType)
			if _, _, _, err := formalEvidenceRefSpan(evidence, memory.EvidenceRef{
				EvidenceID: evidence.ID, SourceOrder: 0, FullSource: true,
			}); err == nil {
				t.Fatalf("source expansion accepted answer-facing %s Evidence", sourceType)
			}
		})
	}
}

func TestFormalB1PackerAndValidatorRequireCompleteRankedAnchorPrefix(t *testing.T) {
	ctx := context.Background()
	protocol := sourceTestProtocol()
	system := answerPromptForRegime(4, false, false, false)
	qa := locomoQA{QuestionID: "locomo:0:prefix", Question: "What was said?", Category: 4}
	reader := formalEvidenceMap{}

	firstA := formalSourceTestEvidence("e-first-a", "first source alpha", memory.EvidenceMessage)
	firstB := formalSourceTestEvidence("e-first-b", "first source beta", memory.EvidenceMessage)
	second := formalSourceTestEvidence("e-second", "second source", memory.EvidenceMessage)
	for _, evidence := range []memory.Evidence{firstA, firstB, second} {
		reader[evidence.ID] = evidence
	}
	anchors := []formalExpandedAnchor{
		formalSourceTestAnchor(t, "projection:first", 0.9, firstA, firstB),
		formalSourceTestAnchor(t, "projection:second", 0.8, second),
	}
	formalSourceTestRankAnchors(anchors)
	counter := lengthCounter{fingerprint: protocol.Budget.CounterFingerprint}

	firstOnlyInput := formalSourceTestInput(protocol, system, qa, anchors[0].Sources)
	protocol.Budget.AnswerInputTokenCap = len([]rune(firstOnlyInput.System + firstOnlyInput.User))
	selected, input, count, evidenceTokens, err := packExpandedFormalInput(
		ctx, protocol, counter, system, qa, anchors, false,
	)
	if err != nil {
		t.Fatalf("pack complete first anchor: %v", err)
	}
	if len(selected) != 2 || selected[0].Evidence.ID != firstA.ID || selected[1].Evidence.ID != firstB.ID {
		t.Fatalf("selected sources = %v, want both sources of first anchor only", formalSourceTestIDs(selected))
	}
	candidate := buildExpandedFormalCandidateArtifact(protocol, qa, anchors, nil, 1)
	trace := buildFormalTraceForItems(protocol, qa.QuestionID, candidate, formalBundleItems(selected))
	bundle := buildExpandedFormalBundle(protocol, qa.QuestionID, candidate, trace, selected, input)
	bundle.AnswerInputTokens = count.InputTokens
	bundle.EvidenceTokens = evidenceTokens
	bundle.WithinCap = true
	if err := validateActiveFormalBundle(ctx, reader, protocol, options{}, qa, candidate, trace, bundle); err != nil {
		t.Fatalf("validator rejected complete ranked anchor prefix: %v", err)
	}

	for _, tc := range []struct {
		name    string
		sources []formalExpandedSource
	}{
		{name: "partial first anchor", sources: anchors[0].Sources[:1]},
		{name: "reordered first anchor", sources: []formalExpandedSource{anchors[0].Sources[1], anchors[0].Sources[0]}},
		{name: "skipped first anchor", sources: anchors[1].Sources},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tamperedTrace, tamperedBundle := formalSourceTestBundle(protocol, system, qa, candidate, tc.sources)
			if err := validateActiveFormalBundle(ctx, reader, protocol, options{}, qa, candidate, tamperedTrace, tamperedBundle); err == nil {
				t.Fatal("validator accepted a coherent but non-prefix/partial/reordered Bundle")
			}
			// The artifact-only validator must enforce the same prefix rule
			// independently of a producer-owned persisted receipt.
			tamperedBundle.ActiveValidation = evalFormalActiveValidation{
				Checked:             true,
				AllowedIDsDigest:    tamperedTrace.SourceValidation.AllowedIDsDigest,
				EvidenceStateDigest: fixtureDigest("fabricated-active-state"),
				ResolvedCount:       len(tamperedBundle.SourceIDs),
				SourceValid:         true,
				SpanValid:           true,
				CitationValid:       true,
			}
			tamperedBundle.ActiveValidation.ReceiptDigest = formalActiveValidationDigest(tamperedBundle.ActiveValidation)
			if err := validateFormalFrozenPayload(protocol, candidate, tamperedTrace, tamperedBundle); err == nil {
				t.Fatal("artifact-only validator trusted a fabricated receipt for a non-prefix Bundle")
			}
		})
	}

	huge := formalSourceTestEvidence("e-huge", strings.Repeat("oversized ", 200), memory.EvidenceMessage)
	tiny := formalSourceTestEvidence("e-tiny", "tiny", memory.EvidenceMessage)
	oversizedAnchors := []formalExpandedAnchor{
		formalSourceTestAnchor(t, "projection:oversized", 0.9, huge),
		formalSourceTestAnchor(t, "projection:tiny", 0.8, tiny),
	}
	formalSourceTestRankAnchors(oversizedAnchors)
	staticInput := formalSourceTestInput(protocol, system, qa, nil)
	tinyInput := formalSourceTestInput(protocol, system, qa, oversizedAnchors[1].Sources)
	protocol.Budget.AnswerInputTokenCap = len([]rune(tinyInput.System + tinyInput.User))
	if protocol.Budget.AnswerInputTokenCap <= len([]rune(staticInput.System+staticInput.User)) {
		t.Fatal("test cap does not leave room for the later tiny anchor")
	}
	selected, _, _, _, err = packExpandedFormalInput(ctx, protocol, counter, system, qa, oversizedAnchors, false)
	if err == nil || len(selected) != 0 {
		t.Fatalf("packer skipped an oversized first anchor and admitted a later one: selected=%v err=%v", formalSourceTestIDs(selected), err)
	}
}

type formalEvidenceReaderFunc func(context.Context, []string) (map[string]memory.Evidence, error)

func (reader formalEvidenceReaderFunc) GetMany(ctx context.Context, ids []string) (map[string]memory.Evidence, error) {
	return reader(ctx, ids)
}

type formalEvidenceMap map[string]memory.Evidence

func (values formalEvidenceMap) GetMany(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
	out := make(map[string]memory.Evidence, len(ids))
	for _, id := range ids {
		evidence, ok := values[id]
		if !ok {
			return nil, fmt.Errorf("missing test Evidence %q", id)
		}
		out[id] = evidence
	}
	return out, nil
}

func formalSourceTestEvidence(id, content string, sourceType memory.EvidenceSourceType) memory.Evidence {
	recorded := time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC)
	return memory.Evidence{
		ID: id, SourceType: sourceType, SourceSessionID: "conv0-sess1",
		Speaker: "Caroline", Content: content, RecordedAt: recorded,
		ContentDigest: strings.TrimPrefix(evalTextDigest(content), "sha256:"),
		State:         memory.EvidenceActive, Revision: 1,
	}
}

func formalSourceTestAnchor(t *testing.T, anchorID string, score float64, evidence ...memory.Evidence) formalExpandedAnchor {
	t.Helper()
	anchor := formalExpandedAnchor{
		Hit: memory.Result{
			ProjectionID: strings.TrimPrefix(anchorID, "projection:"),
			Content:      "navigation " + anchorID,
			Score:        score,
		},
		CandidateID: anchorID,
	}
	for index, sourceEvidence := range evidence {
		ref := memory.EvidenceRef{EvidenceID: sourceEvidence.ID, SourceOrder: index, FullSource: true}
		text, span, kind, err := formalEvidenceRefSpan(sourceEvidence, ref)
		if err != nil {
			t.Fatalf("build source %d for %s: %v", index, anchorID, err)
		}
		renderedID := formalRenderedSourceID(anchorID, index, sourceEvidence.ID)
		source := formalExpandedSource{
			Ref:      ref,
			Evidence: sourceEvidence,
			Result:   formalEvidenceResult(renderedID, text, sourceEvidence),
			Candidate: evalRenderedCandidate{
				CandidateID: renderedID, Kind: "raw_turn", Rank: index + 1,
				Score: score, Text: text, TextDigest: evalTextDigest(text),
				SourceIDs: []string{sourceEvidence.ID}, ExpandedFrom: []string{anchorID},
				ExpansionCount: len(evidence) - 1,
			},
			Item: evalFormalBundleItem{
				ItemID: formalBundleItemID(renderedID), Kind: kind, Text: text,
				CandidateIDs: []string{renderedID}, Sources: []evalFormalSourceSpan{span},
			},
		}
		anchor.SourceIDs = append(anchor.SourceIDs, sourceEvidence.ID)
		anchor.Sources = append(anchor.Sources, source)
	}
	return anchor
}

func formalSourceTestInput(protocol evalProtocol, system string, qa locomoQA, sources []formalExpandedSource) evidencecompiler.AnswerInput {
	results := make([]memory.Result, 0, len(sources))
	for _, source := range sources {
		results = append(results, source.Result)
	}
	return evidencecompiler.AnswerInput{
		Model: protocol.Models.Answerer.ID, System: system,
		User: buildAnswerContextPrompt(qa.Question, results, qa.QuestionDate, qa.Category, false),
	}
}

func formalSourceTestRankAnchors(anchors []formalExpandedAnchor) {
	rank := 1
	for anchorIndex := range anchors {
		for sourceIndex := range anchors[anchorIndex].Sources {
			anchors[anchorIndex].Sources[sourceIndex].Candidate.Rank = rank
			rank++
		}
	}
}

func formalSourceTestBundle(
	protocol evalProtocol,
	system string,
	qa locomoQA,
	candidate evalCandidateArtifact,
	sources []formalExpandedSource,
) (evalFormalTraceRecord, evalFormalBundleRecord) {
	input := formalSourceTestInput(protocol, system, qa, sources)
	trace := buildFormalTraceForItems(protocol, qa.QuestionID, candidate, formalBundleItems(sources))
	bundle := buildExpandedFormalBundle(protocol, qa.QuestionID, candidate, trace, sources, input)
	staticInput := formalSourceTestInput(protocol, system, qa, nil)
	bundle.AnswerInputTokens = len([]rune(input.System + input.User))
	bundle.EvidenceTokens = bundle.AnswerInputTokens - len([]rune(staticInput.System+staticInput.User))
	bundle.WithinCap = bundle.AnswerInputTokens <= protocol.Budget.AnswerInputTokenCap
	return trace, bundle
}

func formalSourceTestIDs(sources []formalExpandedSource) []string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.Evidence.ID)
	}
	return ids
}

// TestFormalSemanticEpisodeMixedGenuineAndFallbackPrefixValidates reproduces
// the 025 treatment-arm failure: a semantic_episode renderer produces genuine
// episode candidates (multi-source, ID "…/episode") alongside single-source
// fallback candidates (ID "…/episode-fallback:<id>", also Kind=semantic_episode).
// The rebuilt expansion must fold ONLY genuine episodes into one multi-source
// item each, keep fallback anchors as per-source items, and still validate when
// the token cap admits only a ranked prefix of anchors.
func TestFormalSemanticEpisodeMixedGenuineAndFallbackPrefixValidates(t *testing.T) {
	ctx := context.Background()
	protocol := sourceTestProtocol()
	system := answerPromptForRegime(4, false, false, false)
	qa := locomoQA{QuestionID: "locomo:0:mixed", Question: "What was said?", Category: 4}
	reader := formalEvidenceMap{}

	evG1 := formalSourceTestEvidence("e-genuine-1", "genuine episode first source", memory.EvidenceMessage)
	evG2 := formalSourceTestEvidence("e-genuine-2", "genuine episode second source", memory.EvidenceMessage)
	evF1 := formalSourceTestEvidence("e-fallback-1", "fallback first source", memory.EvidenceMessage)
	evF2 := formalSourceTestEvidence("e-fallback-2", "fallback second source", memory.EvidenceMessage)
	for _, evidence := range []memory.Evidence{evG1, evG2, evF1, evF2} {
		reader[evidence.ID] = evidence
	}
	genuine := formalSourceTestAnchor(t, "projection:genuine", 0.9, evG1, evG2)
	fallback := formalSourceTestAnchor(t, "projection:fallback", 0.8, evF1, evF2)
	anchors := []formalExpandedAnchor{genuine, fallback}
	formalSourceTestRankAnchors(anchors)

	// Enriched renderer output: the genuine anchor aggregates both sources into
	// one episode candidate; the fallback anchor degrades to one candidate per
	// source. Both carry Kind=semantic_episode, so only the candidate ID suffix
	// distinguishes them.
	genuineEpisode := evalRenderedCandidate{
		CandidateID:    genuine.CandidateID + "/episode",
		Kind:           string(ReprSemanticEpisode),
		Rank:           1,
		Score:          genuine.Hit.Score,
		Text:           evG1.Content + "\n" + evG2.Content + "\n",
		TextDigest:     evalTextDigest(evG1.Content + "\n" + evG2.Content + "\n"),
		SourceIDs:      []string{evG1.ID, evG2.ID},
		ExpandedFrom:   []string{genuine.CandidateID},
		ExpansionCount: 1,
	}
	var enriched []evalRenderedCandidate
	rank := 1
	for _, source := range fallback.Sources {
		enriched = append(enriched, evalRenderedCandidate{
			CandidateID:    fallback.CandidateID + "/episode-fallback:" + source.Evidence.ID,
			Kind:           string(ReprSemanticEpisode),
			Rank:           rank,
			Score:          fallback.Hit.Score,
			Text:           source.Evidence.Content,
			TextDigest:     evalTextDigest(source.Evidence.Content),
			SourceIDs:      []string{source.Evidence.ID},
			ExpandedFrom:   []string{fallback.CandidateID},
			ExpansionCount: 0,
		})
		rank++
	}
	enriched = append([]evalRenderedCandidate{genuineEpisode}, enriched...)

	rebuilt := rebuildExpandedForEpisodes(anchors, enriched, reader)
	if len(rebuilt) != 2 {
		t.Fatalf("rebuilt anchors = %d, want 2", len(rebuilt))
	}
	// Genuine anchor folded to one multi-source episode item.
	if len(rebuilt[0].Sources) != 1 || len(rebuilt[0].Sources[0].Item.Sources) != 2 {
		t.Fatalf("genuine anchor must fold into one 2-source episode item, got %d items %d sources",
			len(rebuilt[0].Sources), len(rebuilt[0].Sources[0].Item.Sources))
	}
	// Fallback anchor must keep its per-source expansion (two items), never be
	// folded by its episode-fallback candidates.
	if len(rebuilt[1].Sources) != 2 {
		t.Fatalf("fallback anchor must keep 2 per-source items, got %d", len(rebuilt[1].Sources))
	}

	candidate := buildExpandedFormalCandidateArtifact(protocol, qa, rebuilt, nil, 1)
	if len(candidate.RenderedCandidates) != 3 {
		t.Fatalf("rendered candidates = %d, want 3 (one episode + two fallback sources)", len(candidate.RenderedCandidates))
	}
	// Ranks must be renumbered 1..N in artifact order.
	for index, rendered := range candidate.RenderedCandidates {
		if rendered.Rank != index+1 {
			t.Fatalf("rendered candidate %d rank = %d, want %d", index, rendered.Rank, index+1)
		}
	}

	// Token cap admits only the genuine anchor (the prefix boundary at 1 item).
	firstOnlyInput := formalSourceTestInput(protocol, system, qa, rebuilt[0].Sources)
	protocol.Budget.AnswerInputTokenCap = len([]rune(firstOnlyInput.System + firstOnlyInput.User))
	selected, input, count, evidenceTokens, err := packExpandedFormalInput(
		ctx, protocol, lengthCounter{fingerprint: protocol.Budget.CounterFingerprint}, system, qa, rebuilt, false,
	)
	if err != nil {
		t.Fatalf("pack prefix bundle: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected sources = %d, want only the genuine episode item", len(selected))
	}
	trace := buildFormalTraceForItems(protocol, qa.QuestionID, candidate, formalBundleItems(selected))
	bundle := buildExpandedFormalBundle(protocol, qa.QuestionID, candidate, trace, selected, input)
	bundle.AnswerInputTokens = count.InputTokens
	bundle.EvidenceTokens = evidenceTokens
	bundle.WithinCap = true

	opt := options{formalEvidence: reader}
	if err := validateActiveFormalBundle(ctx, reader, protocol, opt, qa, candidate, trace, bundle); err != nil {
		t.Fatalf("active validator rejected genuine+fallback prefix bundle: %v", err)
	}
	// The frozen payload gate additionally requires an independent Ledger
	// receipt, which the production path derives via revalidateFrozenFormalSources.
	frozen := revalidateFrozenFormalSources(ctx, protocol, opt, qa, formalFrozenQuestion{
		Candidate: candidate, Trace: trace, Bundle: bundle,
	})
	if err := validateFormalFrozenPayload(protocol, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("frozen validator rejected genuine+fallback prefix bundle: %v", err)
	}
}

// TestFormalB1CompilerBundleAllowsReorderedNonPrefix: the 026 query-time
// compiler arm re-orders and re-selects evidence by relevance inside the frozen
// candidate pool, so its bundle is not a mechanical ranked-anchor prefix. The
// compiler branch of the anchor-prefix contract accepts a reordered/partial
// bundle whose every item still maps 1:1 to a rendered candidate and resolves a
// whole-source allowed span — while the same bundle stays rejected under the
// legacy protocol (no compiler mechanism flag).
func TestFormalB1CompilerBundleAllowsReorderedNonPrefix(t *testing.T) {
	ctx := context.Background()
	protocol := sourceTestProtocol()
	system := answerPromptForRegime(4, false, false, false)
	qa := locomoQA{QuestionID: "locomo:0:compiler", Question: "What was said?", Category: 4}
	reader := formalEvidenceMap{}

	firstA := formalSourceTestEvidence("e-comp-first-a", "first source alpha", memory.EvidenceMessage)
	firstB := formalSourceTestEvidence("e-comp-first-b", "first source beta", memory.EvidenceMessage)
	second := formalSourceTestEvidence("e-comp-second", "second source", memory.EvidenceMessage)
	for _, evidence := range []memory.Evidence{firstA, firstB, second} {
		reader[evidence.ID] = evidence
	}
	anchors := []formalExpandedAnchor{
		formalSourceTestAnchor(t, "projection:first", 0.9, firstA, firstB),
		formalSourceTestAnchor(t, "projection:second", 0.8, second),
	}
	formalSourceTestRankAnchors(anchors)
	candidate := buildExpandedFormalCandidateArtifact(protocol, qa, anchors, nil, 1)

	// Compiler selects the second anchor's source BEFORE the first anchor's —
	// a reordered, non-prefix bundle that is still fully auditable (each item is
	// a rendered candidate with a whole-source allowed span).
	reordered := []formalExpandedSource{anchors[1].Sources[0], anchors[0].Sources[1]}
	reorderedInput := formalSourceTestInput(protocol, system, qa, reordered)
	reorderedTrace, reorderedBundle := formalSourceTestBundle(protocol, system, qa, candidate, reordered)
	reorderedBundle.AnswerInputTokens = len([]rune(reorderedInput.System + reorderedInput.User))
	reorderedBundle.EvidenceTokens = reorderedBundle.AnswerInputTokens
	reorderedBundle.WithinCap = true

	// Legacy protocol must reject the reordered bundle.
	if err := validateActiveFormalBundle(ctx, reader, protocol, options{}, qa, candidate, reorderedTrace, reorderedBundle); err == nil {
		t.Fatal("legacy validator accepted a reordered non-prefix compiler bundle")
	}

	// Compiler protocol must accept it.
	compilerProtocol := protocol
	compilerProtocol.Experiment = evalExperimentProtocol{
		Stage: "b1", Arm: "legacy_count_packer", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"compiler": true, "idk_retry": false, "iris": false, "rerank": false},
	}
	if err := validateActiveFormalBundle(ctx, reader, compilerProtocol, options{}, qa, candidate, reorderedTrace, reorderedBundle); err != nil {
		t.Fatalf("compiler validator rejected reordered auditable bundle: %v", err)
	}
}

// TestFormalChunkVerbatimFoldPacksProjectionText reproduces the B1-control
// packing fix: the bundle must pack the projection's own text (verbatim chunk
// concatenation or condensed fact) instead of source-expanding it into one item
// per raw message. rebuildExpandedForChunkVerbatim folds each whole-source
// anchor into one chunk-verbatim item carrying hit.Content with every member
// evidence as a whole-source span; the folded bundle must still pass the active
// Ledger reread and the frozen-payload gate. Anchors containing an EXTRACT span
// must keep their per-source expansion untouched.
func TestFormalChunkVerbatimFoldPacksProjectionText(t *testing.T) {
	ctx := context.Background()
	protocol := sourceTestProtocol()
	system := answerPromptForRegime(4, false, false, false)
	qa := locomoQA{QuestionID: "locomo:0:chunkverbatim", Question: "What was said?", Category: 4}
	reader := formalEvidenceMap{}

	evChunk1 := formalSourceTestEvidence("e-chunk-1", "Caroline: first message", memory.EvidenceMessage)
	evChunk2 := formalSourceTestEvidence("e-chunk-2", "John: second message", memory.EvidenceMessage)
	evFact := formalSourceTestEvidence("e-fact-1", "Caroline: full raw message", memory.EvidenceMessage)
	evExtract := formalSourceTestEvidence("e-extract-1", "full raw message to extract from", memory.EvidenceMessage)
	for _, evidence := range []memory.Evidence{evChunk1, evChunk2, evFact, evExtract} {
		reader[evidence.ID] = evidence
	}

	chunk := formalSourceTestAnchor(t, "projection:chunk", 0.9, evChunk1, evChunk2)
	chunk.Hit.Content = evChunk1.Content + "\n" + evChunk2.Content
	fact := formalSourceTestAnchor(t, "projection:fact", 0.8, evFact)
	fact.Hit.Content = "condensed plan fact"

	// A partial-source anchor: its expansion is already the exact projection
	// span, so folding must not touch it.
	extractStart, extractEnd := 0, 7
	extractRef := memory.EvidenceRef{
		EvidenceID: evExtract.ID, SourceOrder: 0, StartChar: &extractStart, EndChar: &extractEnd,
		SpanDigest: strings.TrimPrefix(evalTextDigest(string([]rune(evExtract.Content)[extractStart:extractEnd])), "sha256:"),
	}
	extractText, extractSpan, extractKind, err := formalEvidenceRefSpan(evExtract, extractRef)
	if err != nil {
		t.Fatalf("build extract source: %v", err)
	}
	extractRenderedID := formalRenderedSourceID("projection:extract", 0, evExtract.ID)
	extract := formalExpandedAnchor{
		Hit:         memory.Result{ProjectionID: "projection:extract", Content: extractText, Score: 0.7},
		CandidateID: "projection:extract",
		SourceIDs:   []string{evExtract.ID},
		Sources: []formalExpandedSource{{
			Ref: extractRef, Evidence: evExtract,
			Result: formalEvidenceResult(extractRenderedID, extractText, evExtract),
			Candidate: evalRenderedCandidate{
				CandidateID: extractRenderedID, Kind: "raw_turn", Rank: 1, Score: 0.7,
				Text: extractText, TextDigest: evalTextDigest(extractText),
				SourceIDs: []string{evExtract.ID}, ExpandedFrom: []string{"projection:extract"},
				ExpansionCount: 0,
			},
			Item: evalFormalBundleItem{
				ItemID: formalBundleItemID(extractRenderedID), Kind: extractKind, Text: extractText,
				CandidateIDs: []string{extractRenderedID}, Sources: []evalFormalSourceSpan{extractSpan},
			},
		}},
	}

	anchors := []formalExpandedAnchor{chunk, fact, extract}
	formalSourceTestRankAnchors(anchors)
	rebuilt := rebuildExpandedForChunkVerbatim(anchors)
	if len(rebuilt) != 3 {
		t.Fatalf("rebuilt anchors = %d, want 3", len(rebuilt))
	}
	// Chunk anchor folds into one multi-source item carrying the projection text.
	if len(rebuilt[0].Sources) != 1 || len(rebuilt[0].Sources[0].Item.Sources) != 2 {
		t.Fatalf("chunk anchor must fold into one 2-source item, got %d items %d sources",
			len(rebuilt[0].Sources), len(rebuilt[0].Sources[0].Item.Sources))
	}
	chunkItem := rebuilt[0].Sources[0].Item
	if chunkItem.Text != chunk.Hit.Content || !isChunkVerbatimRendered(rebuilt[0].Sources[0].Candidate) {
		t.Fatalf("chunk item text = %q, want projection text %q (kind %q)",
			chunkItem.Text, chunk.Hit.Content, rebuilt[0].Sources[0].Candidate.Kind)
	}
	if len(chunkItem.CandidateIDs) != 1 || chunkItem.CandidateIDs[0] != rebuilt[0].Sources[0].Candidate.CandidateID {
		t.Fatalf("chunk item candidate IDs = %v, want the folded rendered candidate", chunkItem.CandidateIDs)
	}
	if !strings.HasSuffix(rebuilt[0].Sources[0].Candidate.CandidateID, "/verbatim") {
		t.Fatalf("folded candidate ID %q must carry the /verbatim marker", rebuilt[0].Sources[0].Candidate.CandidateID)
	}
	// Single-source fact anchor also folds to its condensed projection text.
	if len(rebuilt[1].Sources) != 1 || rebuilt[1].Sources[0].Item.Text != fact.Hit.Content {
		t.Fatalf("fact anchor must fold to its condensed projection text, got %d sources text=%q",
			len(rebuilt[1].Sources), rebuilt[1].Sources[0].Item.Text)
	}
	// EXTRACT anchor keeps its per-source expansion.
	if len(rebuilt[2].Sources) != 1 || rebuilt[2].Sources[0].Item.Kind != "EXTRACT" {
		t.Fatalf("extract anchor must keep its per-source expansion, got %d sources kind=%q",
			len(rebuilt[2].Sources), rebuilt[2].Sources[0].Item.Kind)
	}
	// Both folded anchors cite all their member sources as whole-source spans.
	for _, folded := range rebuilt[:2] {
		want := len(folded.SourceIDs)
		if len(folded.Sources[0].Item.Sources) != want {
			t.Fatalf("folded anchor %q cites %d sources, want %d", folded.CandidateID, len(folded.Sources[0].Item.Sources), want)
		}
		for _, span := range folded.Sources[0].Item.Sources {
			evidence := reader[span.EvidenceID]
			if span.StartChar != 0 || span.EndChar != len([]rune(evidence.Content)) || span.SpanDigest != evalTextDigest(evidence.Content) {
				t.Fatalf("folded anchor %q span %q is not whole-source", folded.CandidateID, span.EvidenceID)
			}
		}
	}

	candidate := buildExpandedFormalCandidateArtifact(protocol, qa, rebuilt[:2], nil, 1)
	if len(candidate.RenderedCandidates) != 2 {
		t.Fatalf("rendered candidates = %d, want 2 folded candidates", len(candidate.RenderedCandidates))
	}
	for index, rendered := range candidate.RenderedCandidates {
		if rendered.Rank != index+1 {
			t.Fatalf("rendered candidate %d rank = %d, want %d", index, rendered.Rank, index+1)
		}
	}

	// Token cap admits only the first folded anchor (the prefix boundary at 1 item).
	firstOnlyInput := formalSourceTestInput(protocol, system, qa, rebuilt[0].Sources)
	protocol.Budget.AnswerInputTokenCap = len([]rune(firstOnlyInput.System + firstOnlyInput.User))
	selected, input, count, evidenceTokens, err := packExpandedFormalInput(
		ctx, protocol, lengthCounter{fingerprint: protocol.Budget.CounterFingerprint}, system, qa, rebuilt[:2], false,
	)
	if err != nil {
		t.Fatalf("pack prefix bundle: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected sources = %d, want only the first folded anchor", len(selected))
	}
	trace := buildFormalTraceForItems(protocol, qa.QuestionID, candidate, formalBundleItems(selected))
	bundle := buildExpandedFormalBundle(protocol, qa.QuestionID, candidate, trace, selected, input)
	bundle.AnswerInputTokens = count.InputTokens
	bundle.EvidenceTokens = evidenceTokens
	bundle.WithinCap = true

	opt := options{formalEvidence: reader}
	if err := validateActiveFormalBundle(ctx, reader, protocol, opt, qa, candidate, trace, bundle); err != nil {
		t.Fatalf("active validator rejected folded chunk-verbatim prefix bundle: %v", err)
	}
	// The frozen payload gate additionally requires an independent Ledger
	// receipt, which the production path derives via revalidateFrozenFormalSources.
	frozen := revalidateFrozenFormalSources(ctx, protocol, opt, qa, formalFrozenQuestion{
		Candidate: candidate, Trace: trace, Bundle: bundle,
	})
	if err := validateFormalFrozenPayload(protocol, frozen.Candidate, frozen.Trace, frozen.Bundle); err != nil {
		t.Fatalf("frozen validator rejected folded chunk-verbatim prefix bundle: %v", err)
	}
}
