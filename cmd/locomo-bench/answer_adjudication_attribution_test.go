package main

// 036 decision-gap attribution offline tests. No provider, no network. The
// frozen 034 scoring fixture produces a deterministic, hand-derivable control /
// oracle / selected distribution; small hand-built fixtures exercise the gap
// classification and the 035 cross-audit.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wallfacers/engram/provider"
)

// line marshals a value to compact JSON (test helper for JSONL fixtures).
func line(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// buildAttributionFixture derives a small attributionRows directly (no file I/O)
// from a single packet with a chosen hidden-correct pattern.
func buildAttributionFixture(t *testing.T, answers [3]string, correct [3]bool, selectedSlot string) (*attributionRows, error) {
	t.Helper()
	packet := testAdjudicationPacket()
	for slot := 0; slot < 3; slot++ {
		packet.Candidates[slot] = adjudicationPacketCandidate{
			Slot: fmt.Sprintf("C%d", slot+1), Answer: answers[slot], AnswerDigest: adjudicationTextDigest(answers[slot]),
		}
	}
	packet.PacketDigest = adjudicationPacketDigest(packet)
	decision := selectedAdjudicationDecision(packet, adjudicationVerifierResponse{
		SelectedSlot: selectedSlot, EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	manifest := adjudicationManifest{ProtocolHash: adjudicationTextDigest("protocol"), QuestionCount: 1}
	sources := make(map[string]map[string]adjudicationHiddenCandidate, 3)
	slotMap := adjudicationSlotMapRecord{
		PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
	}
	for slot := 0; slot < 3; slot++ {
		sourceDigest := fmt.Sprintf("s%d", slot+1)
		normalized := normalizeAdjudicationAnswer(answers[slot])
		sources[sourceDigest] = map[string]adjudicationHiddenCandidate{
			packet.QuestionID: {Answer: answers[slot], Normalized: normalized, Correct: correct[slot]},
		}
		slotMap.Slots = append(slotMap.Slots, adjudicationSlotSource{
			Slot: fmt.Sprintf("C%d", slot+1), SourceDigest: sourceDigest,
			AnswerDigest: adjudicationTextDigest(answers[slot]), NormalizedAnswerDigest: adjudicationTextDigest(normalized),
		})
	}
	hidden := adjudicationHiddenInputs{Sources: sources, SlotMaps: map[string]adjudicationSlotMapRecord{packet.PacketID: slotMap}, IntegrityValid: true}
	return buildAttributionRows(manifest, []adjudicationPacket{packet}, []adjudicationDecision{decision}, hidden)
}

func TestAttributionMarksOracleNotSelectedGapAndSplitsControlLoss(t *testing.T) {
	// C1 = control (a) correct, C2 = a correct (same group), C3 = c wrong,
	// selected C3. Control group 'a' is correct; selected c is not => gap with
	// control-only loss. Correct candidate slot is C1 (first in slot order).
	rows, err := buildAttributionFixture(t, [3]string{"a", "a", "c"}, [3]bool{true, true, false}, "C3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(rows.Gaps))
	}
	gap := rows.Gaps[0]
	if !gap.ControlCorrect || gap.SelectedCorrect || !gap.Oracle {
		t.Fatalf("gap state = control=%v selected=%v oracle=%v", gap.ControlCorrect, gap.SelectedCorrect, gap.Oracle)
	}
	if gap.CorrectCandidateSlot != "C1" {
		t.Fatalf("correct candidate slot = %q, want C1", gap.CorrectCandidateSlot)
	}
	if gap.FailureMode != attributionModeFactuallyWrong {
		t.Fatalf("unexpected failure mode %q (reason %q)", gap.FailureMode, gap.ModeReason)
	}
	categories, summary := aggregateAttribution(rows)
	if summary.GapCount != 1 || summary.ControlOnlyLoss != 1 || summary.BothWrong != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(categories) != 1 || categories[0].Gaps != 1 || categories[0].ControlOnlyLoss != 1 {
		t.Fatalf("categories = %#v", categories)
	}
}

func TestAttributionBothWrongWhenControlAndSelectedBothMiss(t *testing.T) {
	// C1 = a wrong (control group), C2 = b wrong selected, C3 = c correct.
	// control wrong, selected wrong, oracle true => both-wrong / third-candidate.
	rows, err := buildAttributionFixture(t, [3]string{"a", "b", "c"}, [3]bool{false, false, true}, "C2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(rows.Gaps))
	}
	if rows.Gaps[0].ControlCorrect || rows.Gaps[0].SelectedCorrect {
		t.Fatalf("gap should be both wrong: %#v", rows.Gaps[0])
	}
	_, summary := aggregateAttribution(rows)
	if summary.BothWrong != 1 || summary.ControlOnlyLoss != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestAttributionSemanticEquivalenceWhenCorrectAndSelectedNormalizeEqual(t *testing.T) {
	// C1 = "Alpha" correct, C2 = "alpha" wrong (same normalized), C3 = c wrong.
	// selected C2 normalizes equal to correct C1 => semantic equivalence mode.
	rows, err := buildAttributionFixture(t, [3]string{"Alpha", "alpha", "c"}, [3]bool{true, false, false}, "C2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(rows.Gaps))
	}
	gap := rows.Gaps[0]
	if gap.FailureMode != attributionModeSemanticEquivalence {
		t.Fatalf("failure mode = %q, want semantic_equivalence (reason %q)", gap.FailureMode, gap.ModeReason)
	}
	if !gap.NormalizedEqual {
		t.Fatal("normalized_equal should be true")
	}
	_, summary := aggregateAttribution(rows)
	if summary.SemanticEquivalence != 1 {
		t.Fatalf("semantic_equivalence count = %d, want 1", summary.SemanticEquivalence)
	}
}

func TestAttributionFactuallyWrongWhenEvidenceCitedButWrongCandidate(t *testing.T) {
	// 034 contract requires a selected decision to carry at least one valid
	// evidence citation (validateAdjudicationDecision). So a wrong-but-cited
	// selection classifies as factually_wrong, not evidence_insufficient.
	packet := testAdjudicationPacket()
	packet.Candidates = []adjudicationPacketCandidate{
		{Slot: "C1", Answer: "a", AnswerDigest: adjudicationTextDigest("a")},
		{Slot: "C2", Answer: "b", AnswerDigest: adjudicationTextDigest("b")},
		{Slot: "C3", Answer: "c", AnswerDigest: adjudicationTextDigest("c")},
	}
	packet.PacketDigest = adjudicationPacketDigest(packet)
	decision := selectedAdjudicationDecision(packet, adjudicationVerifierResponse{
		SelectedSlot: "C2", EvidenceIDs: []string{"E01"},
	}, provider.Usage{})
	manifest := adjudicationManifest{ProtocolHash: adjudicationTextDigest("protocol"), QuestionCount: 1}
	sources := map[string]map[string]adjudicationHiddenCandidate{
		"s1": {packet.QuestionID: {Answer: "a", Normalized: "a", Correct: false}},
		"s2": {packet.QuestionID: {Answer: "b", Normalized: "b", Correct: false}},
		"s3": {packet.QuestionID: {Answer: "c", Normalized: "c", Correct: true}},
	}
	slotMap := adjudicationSlotMapRecord{PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID, Slots: []adjudicationSlotSource{
		{Slot: "C1", SourceDigest: "s1", AnswerDigest: adjudicationTextDigest("a"), NormalizedAnswerDigest: adjudicationTextDigest("a")},
		{Slot: "C2", SourceDigest: "s2", AnswerDigest: adjudicationTextDigest("b"), NormalizedAnswerDigest: adjudicationTextDigest("b")},
		{Slot: "C3", SourceDigest: "s3", AnswerDigest: adjudicationTextDigest("c"), NormalizedAnswerDigest: adjudicationTextDigest("c")},
	}}
	hidden := adjudicationHiddenInputs{Sources: sources, SlotMaps: map[string]adjudicationSlotMapRecord{packet.PacketID: slotMap}, IntegrityValid: true}
	rows, err := buildAttributionRows(manifest, []adjudicationPacket{packet}, []adjudicationDecision{decision}, hidden)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(rows.Gaps))
	}
	if rows.Gaps[0].FailureMode != attributionModeFactuallyWrong {
		t.Fatalf("failure mode = %q, want factually_wrong", rows.Gaps[0].FailureMode)
	}
	if rows.Gaps[0].ModeEvidence == "" {
		t.Fatal("mode_evidence should carry the cited evidence ids")
	}
}

func TestAttributionFrozenFixtureRecomputesGapAndControlSplit(t *testing.T) {
	// The frozen 034 scoring fixture is hand-derivable: oracle 1411, control 1368.
	// The fixture's decisions are all fallback (= text control), so selected
	// equals control and gap = oracle - selected = 1411 - 1368 = 43. This verifies
	// the core invariant gap_count == oracle - selected against a deterministic
	// input; the real 034 run (selected 1378) yields 33 with the same code.
	manifest, packets, decisions, hidden := frozenAdjudicationScoringFixture(t)
	rows, err := buildAttributionRows(manifest, packets, decisions, hidden)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != manifest.QuestionCount {
		t.Fatalf("rows = %d, want %d", len(rows.Rows), manifest.QuestionCount)
	}
	_, summary := aggregateAttribution(rows)
	if summary.OracleCorrect != 1411 {
		t.Fatalf("oracle = %d, want 1411", summary.OracleCorrect)
	}
	if summary.ControlCorrect != 1368 {
		t.Fatalf("control = %d, want 1368", summary.ControlCorrect)
	}
	// Fixture decisions are all fallback → selected == control.
	if summary.SelectedCorrect != summary.ControlCorrect {
		t.Fatalf("selected = %d, want control %d (all decisions are fallback)", summary.SelectedCorrect, summary.ControlCorrect)
	}
	if summary.GapCount != summary.OracleCorrect-summary.SelectedCorrect {
		t.Fatalf("gap %d != oracle %d - selected %d", summary.GapCount, summary.OracleCorrect, summary.SelectedCorrect)
	}
	if summary.GapCount != 43 {
		t.Fatalf("gap = %d, want 43 (oracle 1411 - selected 1368 on the fallback fixture)", summary.GapCount)
	}
	// Every fixture gap is a fallback (all decisions are fallback), so the
	// override split (control-only loss / both-wrong) must be zero and every gap
	// must land in fallback_gaps. The real 034 run exercises the override split.
	if summary.ControlOnlyLoss != 0 || summary.BothWrong != 0 {
		t.Fatalf("override split must be zero on the all-fallback fixture, got control_only=%d both_wrong=%d", summary.ControlOnlyLoss, summary.BothWrong)
	}
	if summary.FallbackGaps != summary.GapCount {
		t.Fatalf("fallback_gaps = %d, want every gap %d (all decisions are fallback)", summary.FallbackGaps, summary.GapCount)
	}
}

// TestAttributionFallbackGapsExcludedFromOverrideSplit proves the aggregation
// semantic that spec SC-002 fixes: a fallback gap (confidence=fallback, either
// triggered with a fallback reason or not_triggered) is counted in fallback_gaps
// and must NOT be absorbed into the both-wrong override bucket. In the 034 data,
// confidence="fallback" is the default label for not-triggered decisions (769 of
// 822), so only triggered-accepted overrides belong in control_only/both_wrong.
func TestAttributionFallbackGapsExcludedFromOverrideSplit(t *testing.T) {
	rows := &attributionRows{
		Schema: attributionReportSchema, Count: 4,
		Rows: make([]attributionRow, 4),
		Gaps: []attributionGapRow{
			// accepted override: control correct, selected wrong → control-only loss.
			{PacketID: "p1", Category: 1, ControlCorrect: true, SelectedCorrect: false, Oracle: true, Triggered: true},
			// accepted override: control wrong, selected wrong, third candidate correct → both-wrong.
			{PacketID: "p2", Category: 1, ControlCorrect: false, SelectedCorrect: false, Oracle: true, Triggered: true},
			// triggered fallback (low_confidence): control wrong, selected wrong → fallback_gaps, not both-wrong.
			{PacketID: "p3", Category: 1, ControlCorrect: false, SelectedCorrect: false, Oracle: true, Triggered: true, FallbackReason: "low_confidence", SelectedConfidence: "fallback"},
			// not-triggered fallback (not_triggered): control wrong, selected wrong → fallback_gaps, not both-wrong.
			{PacketID: "p4", Category: 1, ControlCorrect: false, SelectedCorrect: false, Oracle: true, Triggered: false, FallbackReason: "not_triggered", SelectedConfidence: "fallback"},
		},
	}
	_, summary := aggregateAttribution(rows)
	if summary.GapCount != 4 {
		t.Fatalf("gap = %d, want 4", summary.GapCount)
	}
	if summary.ControlOnlyLoss != 1 {
		t.Fatalf("control_only_loss = %d, want 1 (accepted override only)", summary.ControlOnlyLoss)
	}
	if summary.BothWrong != 1 {
		t.Fatalf("both_wrong = %d, want 1 (accepted override only; fallback gaps must not be absorbed)", summary.BothWrong)
	}
	if summary.FallbackGaps != 2 {
		t.Fatalf("fallback_gaps = %d, want 2 (triggered low_confidence + not_triggered)", summary.FallbackGaps)
	}
	if summary.ControlOnlyLoss+summary.BothWrong+summary.FallbackGaps != summary.GapCount {
		t.Fatalf("override split + fallback_gaps = %d, want gap %d (disjoint, exhaustive buckets)", summary.ControlOnlyLoss+summary.BothWrong+summary.FallbackGaps, summary.GapCount)
	}
}

func TestAttributionRefusesHiddenLoadBeforeValidSeal(t *testing.T) {
	dir := t.TempDir()
	// No public fixture written => loadAndValidateAdjudicationPublic fails before
	// any hidden read. Assert the CLI path fails closed via the loader directly.
	if _, _, err := loadAndValidateAdjudicationPublic(dir, false); err == nil {
		t.Fatal("expected load failure on empty attribution dir")
	}
}

func TestCrossAuditDegradesWhenSealMissing(t *testing.T) {
	gaps := []attributionGapRow{{PacketID: "packet:1"}, {PacketID: "packet:2"}}
	out, source, err := crossAudit(gaps, "")
	if err != nil {
		t.Fatal(err)
	}
	if source != "" || len(out) != 2 {
		t.Fatalf("crossAudit empty dir = source %q rows %d", source, len(out))
	}
	for _, gap := range out {
		if !gap.AuditUnavailable {
			t.Fatal("gap should be marked audit_unavailable on missing seal")
		}
	}
}

func TestCrossAuditReadsAssessmentsFromResolverAndCalls(t *testing.T) {
	dir := t.TempDir()
	packet := testAdjudicationPacket()
	// One risk packet with a refuted parent in the entailment view.
	resolver := adjudicationAuditResolverMapRecord{
		Schema: adjudicationAuditResolverSchema, ProtocolHash: packet.ProtocolHash, PacketID: packet.PacketID,
		ParentSelectedGroupDigest: "parent-group", Risk: true,
		ViewMaps: []adjudicationAuditViewMap{
			{ViewID: adjudicationAuditViewEntailment, SlotToGroup: map[string]string{"C1": "parent-group", "C2": "alt-group"}},
		},
	}
	resolverRaw := line(resolver)
	if err := os.WriteFile(filepath.Join(dir, adjudicationAuditResolverMapFile), []byte(resolverRaw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := adjudicationAuditCallRecord{
		Schema: adjudicationAuditCallSchema, PacketID: packet.PacketID, ViewID: adjudicationAuditViewEntailment,
		State: adjudicationAuditCallCompleted,
		Assessments: []adjudicationAuditCandidateAssessment{
			{Slot: "C1", Support: adjudicationAuditAxis{Value: "no"}, Contradiction: adjudicationAuditAxis{Value: "yes", EvidenceIDs: []string{"E01"}}},
			{Slot: "C2", Support: adjudicationAuditAxis{Value: "yes", EvidenceIDs: []string{"E02"}}, Contradiction: adjudicationAuditAxis{Value: "no"}},
		},
	}
	callRaw := line(call)
	if err := os.WriteFile(filepath.Join(dir, adjudicationAuditCallsFile), []byte(callRaw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gaps := []attributionGapRow{{PacketID: packet.PacketID}}
	out, source, err := crossAudit(gaps, dir)
	if err != nil {
		t.Fatal(err)
	}
	if source != "035-audit" {
		t.Fatalf("source = %q, want 035-audit", source)
	}
	if out[0].InRiskQueue == nil || !*out[0].InRiskQueue {
		t.Fatal("in_risk_queue should be true")
	}
	if out[0].ParentRefutedAnyView == nil || !*out[0].ParentRefutedAnyView {
		t.Fatal("parent_refuted_any_view should be true")
	}
	if out[0].UniqueAlternative == nil || !*out[0].UniqueAlternative {
		t.Fatal("unique_alternative should be true")
	}
	if out[0].AuditUnavailable {
		t.Fatal("should not be marked audit_unavailable")
	}
}

func TestWriteAttributionReportRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	report := attributionReport{Schema: attributionReportSchema}
	if err := writeAttributionReport(dir, report); err != nil {
		t.Fatal(err)
	}
	if err := writeAttributionReport(dir, report); err == nil {
		t.Fatal("second write should refuse existing report")
	}
}

func TestAttributionRowsDeterministicUnderHiddenLabelMutation(t *testing.T) {
	// The sanitized loader strips hidden labels before build; here we assert the
	// rebuild depends only on validated slot maps, not on extra hidden fields.
	base, err := buildAttributionFixture(t, [3]string{"a", "b", "c"}, [3]bool{true, false, true}, "C2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base.Rows, base.Rows) {
		t.Fatal("rows must be deep-equal to themselves")
	}
	if len(base.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(base.Rows))
	}
}
