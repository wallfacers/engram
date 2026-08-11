package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func TestAdjudicationNormalizationAndTextControl(t *testing.T) {
	if got, want := normalizeAdjudicationAnswer(" HÉLLO, World! 42 "), "hlloworld42"; got != want {
		t.Fatalf("normalize = %q, want %q", got, want)
	}
	candidates := []adjudicationPacketCandidate{
		{Slot: "C3", Answer: "Zulu"},
		{Slot: "C1", Answer: "alpha"},
		{Slot: "C2", Answer: "ALPHA!"},
	}
	if got := adjudicationTextControlSlot(candidates); got != "C2" {
		t.Fatalf("2:1 text control = %q, want C2", got)
	}
	threeWay := []adjudicationPacketCandidate{
		{Slot: "C1", Answer: "Zulu"}, {Slot: "C2", Answer: "Beta"}, {Slot: "C3", Answer: "Alpha"},
	}
	if got := adjudicationTextControlSlot(threeWay); got != "C3" {
		t.Fatalf("three-way text control = %q, want C3", got)
	}
}

func TestAdjudicationHistoricalTieUsesCanonicalSource(t *testing.T) {
	packet := adjudicationPacket{Candidates: []adjudicationPacketCandidate{
		{Slot: "C1", Answer: "same"}, {Slot: "C2", Answer: "same"}, {Slot: "C3", Answer: "other"},
	}}
	slotMap := adjudicationSlotMapRecord{Slots: []adjudicationSlotSource{
		{Slot: "C1", SourceDigest: "sha256:zzz"},
		{Slot: "C2", SourceDigest: "sha256:aaa"},
		{Slot: "C3", SourceDigest: "sha256:mmm"},
	}}
	if got := adjudicationTextControlSlot(packet.Candidates); got != "C1" {
		t.Fatalf("packet-only control = %q, want stable visible slot C1", got)
	}
	if got := canonicalHistoricalSlotForSameAnswer(packet, slotMap, "C1"); got != "C2" {
		t.Fatalf("historical duplicate-answer tie = %q, want canonical-source C2", got)
	}
}

func TestAdjudicationPermutationIgnoresSourceOrder(t *testing.T) {
	left := []adjudicationCandidate{
		{Answer: "one", SourceDigest: "sha256:ccc"},
		{Answer: "two", SourceDigest: "sha256:aaa"},
		{Answer: "three", SourceDigest: "sha256:bbb"},
	}
	right := []adjudicationCandidate{left[2], left[0], left[1]}
	gotLeft := blindAdjudicationCandidates("seed", "conv-0-q-1", left)
	gotRight := blindAdjudicationCandidates("seed", "conv-0-q-1", right)
	if !reflect.DeepEqual(gotLeft, gotRight) {
		t.Fatalf("source reorder changed blinded candidates:\nleft=%#v\nright=%#v", gotLeft, gotRight)
	}
}

func TestParseAdjudicationVerifierResponseStrict(t *testing.T) {
	packet := adjudicationPacket{
		Candidates: []adjudicationPacketCandidate{{Slot: "C1"}, {Slot: "C2"}, {Slot: "C3"}},
		Evidence:   []adjudicationEvidenceItem{{EvidenceID: "E01"}, {EvidenceID: "E02"}},
	}
	got, err := parseAdjudicationVerifierResponse(`{"selected_slot":"C2","evidence_ids":["E01"],"confidence":"high"}`, packet)
	if err != nil {
		t.Fatalf("valid response: %v", err)
	}
	if got.SelectedSlot != "C2" || !reflect.DeepEqual(got.EvidenceIDs, []string{"E01"}) {
		t.Fatalf("parsed response = %#v", got)
	}
	for _, raw := range []string{
		`{"selected_slot":"C4","evidence_ids":["E01"],"confidence":"high"}`,
		`{"selected_slot":"C1","evidence_ids":[],"confidence":"high"}`,
		`{"selected_slot":"C1","evidence_ids":["E99"],"confidence":"high"}`,
		`{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"low"}`,
		`{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"high","answer":"free"}`,
	} {
		if _, err := parseAdjudicationVerifierResponse(raw, packet); err == nil {
			t.Fatalf("accepted invalid response %s", raw)
		}
	}
}

func TestAdjudicationVerifierPromptIsPacketOnly(t *testing.T) {
	packet := testAdjudicationPacket()
	user, err := buildAdjudicationVerifierPrompt(packet)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"question":"Q?"`, `"evidence_id":"E01"`, `"slot":"C1"`} {
		if !strings.Contains(user, want) {
			t.Fatalf("prompt does not contain %s: %s", want, user)
		}
	}
	for _, forbidden := range []string{"gold", "correct", "source_digest", "entry_name"} {
		if strings.Contains(strings.ToLower(user), forbidden) {
			t.Fatalf("packet-only prompt contains forbidden %q", forbidden)
		}
	}
	if !strings.Contains(adjudicationVerifierSystemPrompt, "Never write a new answer") {
		t.Fatal("system prompt does not prohibit a fourth answer")
	}
}

// TestAdjudicationTemporalPromptIsOptInAndCategoryScoped proves the 90pp
// lever stays byte-identical by default: adjudicationSystemPromptFor returns
// the frozen generic prompt unless the temporal contract is explicitly enabled
// AND the packet is a category-2 (temporal) question. Enabling the flag must
// change the prompt digest so a tplan-arm seal cannot be confused with a
// generic-arm seal.
func TestAdjudicationTemporalPromptIsOptInAndCategoryScoped(t *testing.T) {
	prior := adjudicationTemporalPromptEnabled
	defer func() { adjudicationTemporalPromptEnabled = prior }()

	temporal := testAdjudicationPacket()
	temporal.Category = 2

	// Default off: byte-identical generic prompt for every category.
	adjudicationTemporalPromptEnabled = false
	if got := adjudicationSystemPromptFor(temporal); got != adjudicationVerifierSystemPrompt {
		t.Fatalf("flag-off system prompt diverged from frozen generic")
	}
	nonTemporal := testAdjudicationPacket() // category 1
	if got := adjudicationSystemPromptFor(nonTemporal); got != adjudicationVerifierSystemPrompt {
		t.Fatalf("flag-off non-temporal system prompt diverged")
	}

	// Flag on: category-2 switches to the temporal reasoning contract.
	adjudicationTemporalPromptEnabled = true
	tplan := adjudicationSystemPromptFor(temporal)
	if tplan == adjudicationVerifierSystemPrompt {
		t.Fatal("temporal flag on but category-2 prompt is still generic")
	}
	for _, want := range []string{"TEMPORAL REASONING PLAN", "[event:", "timeline"} {
		if !strings.Contains(tplan, want) {
			t.Fatalf("temporal prompt missing %q", want)
		}
	}
	// Non-temporal categories keep the generic prompt even under the flag.
	if got := adjudicationSystemPromptFor(nonTemporal); got != adjudicationVerifierSystemPrompt {
		t.Fatalf("temporal flag must not change non-temporal prompt")
	}

	// The frozen manifest digest stays generic in every mode (the temporal
	// contract is a run-time prompt variant, not a manifest rewrite).
	if got := adjudicationPromptDigest(); got != adjudicationTextDigest(adjudicationVerifierSystemPrompt+"\x00packet-json-v1") {
		t.Fatalf("manifest prompt digest must stay generic, got %s", got)
	}
	// The per-packet input identity must differ between arms (seal identity).
	adjudicationTemporalPromptEnabled = true
	tplanInput, err := adjudicationPacketInputDigest(temporal, "runid")
	if err != nil {
		t.Fatal(err)
	}
	adjudicationTemporalPromptEnabled = false
	genericInput, err := adjudicationPacketInputDigest(temporal, "runid")
	if err != nil {
		t.Fatal(err)
	}
	if tplanInput == genericInput {
		t.Fatal("temporal and generic input digests must differ")
	}
}

func TestAdjudicationCallJournalResumeAndOrphanRefusal(t *testing.T) {
	packet := testAdjudicationPacket()
	packet.PacketDigest = adjudicationPacketDigest(packet)
	path := filepath.Join(t.TempDir(), adjudicationCallsFile)
	journal, err := openAdjudicationCallJournal(path, packet.ProtocolHash, []adjudicationPacket{packet})
	if err != nil {
		t.Fatal(err)
	}
	inputDigest := adjudicationTextDigest("input")
	if err := journal.Start(packet, inputDigest); err != nil {
		t.Fatal(err)
	}
	decision := selectedAdjudicationDecision(packet, adjudicationVerifierResponse{
		SelectedSlot: "C1", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{InputTokens: 11, OutputTokens: 3})
	if err := journal.Terminal(packet, inputDigest, decision, true); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := openAdjudicationCallJournal(path, packet.ProtocolHash, []adjudicationPacket{packet})
	if err != nil {
		t.Fatalf("resume complete journal: %v", err)
	}
	got, ok := resumed.TerminalDecision(packet.PacketID)
	if !ok || got.DecisionDigest != decision.DecisionDigest {
		t.Fatalf("resume terminal = %#v, %v", got, ok)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	orphanPath := filepath.Join(t.TempDir(), adjudicationCallsFile)
	orphan, err := openAdjudicationCallJournal(orphanPath, packet.ProtocolHash, []adjudicationPacket{packet})
	if err != nil {
		t.Fatal(err)
	}
	if err := orphan.Start(packet, inputDigest); err != nil {
		t.Fatal(err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openAdjudicationCallJournal(orphanPath, packet.ProtocolHash, []adjudicationPacket{packet}); err == nil {
		t.Fatal("orphan STARTED journal was accepted")
	}
}

func TestAdjudicationPacketFailureFallsBackWithoutRawError(t *testing.T) {
	packet := testAdjudicationPacket()
	packet.PacketDigest = adjudicationPacketDigest(packet)
	decision, completed, err := adjudicateOnePacket(context.Background(), packet,
		func(context.Context, string, string) (string, provider.Usage, error) {
			return "provider-secret-body", provider.Usage{InputTokens: 7}, errors.New("raw upstream secret")
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed || decision.State != adjudicationDecisionFallback || decision.FallbackReason != adjudicationFallbackProviderFailed {
		t.Fatalf("provider fallback = %#v completed=%v", decision, completed)
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "upstream") {
		t.Fatalf("decision leaked provider error: %s", raw)
	}
}

func TestAdjudicationScoreRefusesHiddenLoadBeforeValidSeal(t *testing.T) {
	dir := t.TempDir()
	packet := testAdjudicationPacket()
	manifest, _ := writeAdjudicationPublicFixture(t, dir, []adjudicationPacket{packet})
	called := 0
	_, err := scoreSealedAdjudication(dir, false, func() (adjudicationHiddenInputs, error) {
		called++
		return adjudicationHiddenInputs{}, nil
	})
	if err == nil {
		t.Fatal("score accepted a missing seal")
	}
	if called != 0 {
		t.Fatalf("hidden loader ran %d times before seal validation", called)
	}
	_ = manifest
}

func TestAdjudicationScoreLoadsHiddenOnlyAfterCompleteSeal(t *testing.T) {
	dir := t.TempDir()
	manifest, packets := writeAdjudicationPublicFixture(t, dir, []adjudicationPacket{testAdjudicationPacket()})
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"high"}`, provider.Usage{}, nil
	}
	_, err := executeAdjudicationRun(context.Background(), dir, manifest, packets,
		adjudicationRunConfig{Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "stub", ModelRevision: "v1", Concurrency: 1, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("test-binary")}, caller)
	if err != nil {
		t.Fatal(err)
	}
	packet := packets[0]
	hidden := adjudicationHiddenInputs{
		Sources: map[string]map[string]adjudicationHiddenCandidate{
			"s1": {packet.QuestionID: {Answer: "one", Normalized: "one", Correct: true}},
			"s2": {packet.QuestionID: {Answer: "two", Normalized: "two", Correct: false}},
			"s3": {packet.QuestionID: {Answer: "two", Normalized: "two", Correct: true}},
		},
		SlotMaps: map[string]adjudicationSlotMapRecord{packet.PacketID: {
			PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
			Slots: []adjudicationSlotSource{
				{Slot: "C1", SourceDigest: "s1", AnswerDigest: packet.Candidates[0].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("one")},
				{Slot: "C2", SourceDigest: "s2", AnswerDigest: packet.Candidates[1].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("two")},
				{Slot: "C3", SourceDigest: "s3", AnswerDigest: packet.Candidates[2].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("two")},
			},
		}}, IntegrityValid: true,
	}
	called := 0
	if _, err := scoreSealedAdjudication(dir, false, func() (adjudicationHiddenInputs, error) {
		called++
		return hidden, nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("valid seal hidden loads = %d, want 1", called)
	}
	var seal adjudicationSeal
	if err := readJSON(filepath.Join(dir, adjudicationSealFile), &seal); err != nil {
		t.Fatal(err)
	}
	seal.PacketSetDigest = adjudicationTextDigest("tampered")
	if err := writeJSON(filepath.Join(dir, adjudicationSealFile), seal); err != nil {
		t.Fatal(err)
	}
	called = 0
	if _, err := scoreSealedAdjudication(dir, false, func() (adjudicationHiddenInputs, error) {
		called++
		return hidden, nil
	}); err == nil {
		t.Fatal("tampered seal was accepted")
	}
	if called != 0 {
		t.Fatalf("tampered seal reached hidden loader %d times", called)
	}
}

func TestScoreAdjudicationDecisionsComputesControlOracleMixedAndInstability(t *testing.T) {
	packet := testAdjudicationPacket()
	packet.PacketDigest = adjudicationPacketDigest(packet)
	decision := selectedAdjudicationDecision(packet, adjudicationVerifierResponse{
		SelectedSlot: "C1", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	sources := map[string]map[string]adjudicationHiddenCandidate{
		"s1": {packet.QuestionID: {Answer: "one", Normalized: "one", Correct: true}},
		"s2": {packet.QuestionID: {Answer: "two", Normalized: "two", Correct: true}},
		"s3": {packet.QuestionID: {Answer: "two", Normalized: "two", Correct: false}},
	}
	hidden := adjudicationHiddenInputs{
		Sources: sources,
		SlotMaps: map[string]adjudicationSlotMapRecord{packet.PacketID: {
			PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
			Slots: []adjudicationSlotSource{
				{Slot: "C1", SourceDigest: "s1", AnswerDigest: packet.Candidates[0].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("one")},
				{Slot: "C2", SourceDigest: "s2", AnswerDigest: packet.Candidates[1].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("two")},
				{Slot: "C3", SourceDigest: "s3", AnswerDigest: packet.Candidates[2].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("two")},
			},
		}},
		IntegrityValid: true,
	}
	report, err := scoreAdjudicationDecisions(adjudicationManifest{QuestionCount: 1, TriggeredCount: 1},
		[]adjudicationPacket{packet}, []adjudicationDecision{decision}, hidden, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Selected.Correct != 1 || report.TextControl.Correct != 1 || report.CandidateOracle.Correct != 1 ||
		report.VerdictMajority.Correct != 1 || report.Mixed.Total != 1 || report.Mixed.Correct != 1 ||
		report.JudgeInstability.Total != 1 || report.JudgeInstability.Triggered != 1 {
		t.Fatalf("unexpected score report: %#v", report)
	}
}

func TestAdjudicationStage0PromotionGateIsExact(t *testing.T) {
	base := adjudicationStage0GateInput{
		SelectedCorrect: 1387, QuestionCount: 1540, MixedCorrect: 69, MixedTotal: 88,
		IntegrityValid: true, FrozenDiagnosticsValid: true,
		Categories: map[string]evalCategoryGateResult{"single-hop": {Category: "single-hop"}},
	}
	if got := adjudicationStage0Verdict(base); got != "GO" {
		t.Fatalf("exact threshold verdict = %q, want GO", got)
	}
	for name, mutate := range map[string]func(*adjudicationStage0GateInput){
		"overall":       func(in *adjudicationStage0GateInput) { in.SelectedCorrect-- },
		"mixed":         func(in *adjudicationStage0GateInput) { in.MixedCorrect-- },
		"integrity":     func(in *adjudicationStage0GateInput) { in.IntegrityValid = false },
		"digest cohort": func(in *adjudicationStage0GateInput) { in.FrozenDiagnosticsValid = false },
		"category": func(in *adjudicationStage0GateInput) {
			in.Categories["single-hop"] = evalCategoryGateResult{Category: "single-hop", HolmSignificantNegative: true}
		},
	} {
		candidate := base
		candidate.Categories = map[string]evalCategoryGateResult{"single-hop": base.Categories["single-hop"]}
		mutate(&candidate)
		if got := adjudicationStage0Verdict(candidate); got != "NO_GO" {
			t.Fatalf("%s failed gate with verdict %q", name, got)
		}
	}
}

func TestAdjudicationFrozenHistoricalDiagnostics(t *testing.T) {
	manifest, packets, decisions, hidden := frozenAdjudicationScoringFixture(t)
	report, err := scoreAdjudicationDecisions(manifest, packets, decisions, hidden, true)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]int{
		"verdict majority":      {report.VerdictMajority.Correct, 1371},
		"text control":          {report.TextControl.Correct, 1368},
		"candidate oracle":      {report.CandidateOracle.Correct, 1411},
		"mixed total":           {report.Mixed.Total, 96},
		"triggered mixed":       {report.TriggeredMixed.Total, 88},
		"instability":           {report.JudgeInstability.Total, 13},
		"triggered instability": {report.JudgeInstability.Triggered, 5},
	}
	for name, values := range checks {
		if values[0] != values[1] {
			t.Fatalf("%s = %d, want %d", name, values[0], values[1])
		}
	}
	if !report.FrozenDiagnosticsValid || report.Verdict != "NO_GO" ||
		report.Paired.ControlOnly != 0 || report.Paired.SelectedOnly != 0 || report.Paired.McNemarP != 1 {
		t.Fatalf("frozen report validity/pairing = %#v", report)
	}
}

func frozenAdjudicationScoringFixture(t *testing.T) (adjudicationManifest, []adjudicationPacket, []adjudicationDecision, adjudicationHiddenInputs) {
	t.Helper()
	protocolHash := adjudicationTextDigest("frozen-score-fixture")
	manifest := adjudicationManifest{
		ProtocolHash: protocolHash, QuestionCount: adjudicationFrozenQuestionCount,
		TriggeredCount: adjudicationFrozenTriggerCount, ContextParityCount: adjudicationFrozenContextParityCount,
		TriggeredContextParityCount: adjudicationFrozenTriggeredContextParityCount,
	}
	hidden := adjudicationHiddenInputs{
		Sources: map[string]map[string]adjudicationHiddenCandidate{
			"s1": {}, "s2": {}, "s3": {},
		},
		SlotMaps: make(map[string]adjudicationSlotMapRecord, adjudicationFrozenQuestionCount), IntegrityValid: true,
	}
	packets := make([]adjudicationPacket, 0, adjudicationFrozenQuestionCount)
	decisions := make([]adjudicationDecision, 0, adjudicationFrozenQuestionCount)
	for q := 0; q < adjudicationFrozenQuestionCount; q++ {
		triggered := q < 88 || (q >= 96 && q < 779)
		contextParity := !(q < 5 || (q >= 88 && q < 91))
		correct := [3]bool{true, true, true}
		switch {
		case q < 56:
			correct = [3]bool{true, true, false}
			if q >= 53 {
				correct = [3]bool{false, true, true}
			}
		case q < 96:
			correct = [3]bool{false, true, false}
		case q < 225:
			correct = [3]bool{false, false, false}
		}
		answers := [3]string{"a", "b", "c"}
		if !triggered {
			answers = [3]string{"a", "a", "a"}
		}
		if q < 5 {
			answers = [3]string{"a", "a", "b"}
			correct = [3]bool{true, false, true}
		}
		packet := testAdjudicationPacket()
		packet.ProtocolHash = protocolHash
		packet.PacketID = fmt.Sprintf("packet:%04d", q)
		packet.Q = q
		packet.QuestionID = questionID(0, q)
		packet.Category = q%4 + 1
		packet.Triggered = triggered
		packet.ContextParity = contextParity
		for slot := 0; slot < 3; slot++ {
			packet.Candidates[slot] = adjudicationPacketCandidate{
				Slot: fmt.Sprintf("C%d", slot+1), Answer: answers[slot], AnswerDigest: adjudicationTextDigest(answers[slot]),
			}
		}
		packet.PacketDigest = adjudicationPacketDigest(packet)
		packets = append(packets, packet)
		reason, attempts := adjudicationFallbackNotTriggered, 0
		if triggered {
			reason, attempts = adjudicationFallbackInvalidResponse, 1
		}
		decisions = append(decisions, fallbackAdjudicationDecision(packet, reason, attempts, provider.Usage{}))
		slotMap := adjudicationSlotMapRecord{
			PacketID: packet.PacketID, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
		}
		for slot := 0; slot < 3; slot++ {
			sourceDigest := fmt.Sprintf("s%d", slot+1)
			normalized := normalizeAdjudicationAnswer(answers[slot])
			hidden.Sources[sourceDigest][packet.QuestionID] = adjudicationHiddenCandidate{
				Answer: answers[slot], Normalized: normalized, Correct: correct[slot],
			}
			slotMap.Slots = append(slotMap.Slots, adjudicationSlotSource{
				Slot: fmt.Sprintf("C%d", slot+1), SourceDigest: sourceDigest,
				AnswerDigest: adjudicationTextDigest(answers[slot]), NormalizedAnswerDigest: adjudicationTextDigest(normalized),
			})
		}
		hidden.SlotMaps[packet.PacketID] = slotMap
	}
	return manifest, packets, decisions, hidden
}

func TestAdjudicationCandidateSourceIgnoresHiddenLabels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.jsonl")
	write := func(correct bool, gold string) {
		t.Helper()
		line := map[string]any{
			"conv": 0, "q": 1, "question_id": "conv-0-q-1", "category": 2,
			"category_name": "temporal", "question": "When?", "predicted": "May 2022",
			"answer_regime": "frozen", "retrieval_flags": "hybrid", "input_tokens": 10,
			"answer_context_tokens": 8, "correct": correct, "gold": gold,
		}
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(true, "secret-a")
	first, err := loadAdjudicationCandidateSource(path)
	if err != nil {
		t.Fatal(err)
	}
	write(false, "secret-b")
	second, err := loadAdjudicationCandidateSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.SanitizedDigest != second.SanitizedDigest || !reflect.DeepEqual(first.Records, second.Records) {
		t.Fatalf("hidden label mutation changed sanitized input: %#v %#v", first, second)
	}
	if first.RawDigest == second.RawDigest {
		t.Fatal("custody digest must observe raw hidden-label mutation")
	}
}

func TestAdjudicationCandidateSourceStrictlyRejectsMalformedAndDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.jsonl")
	valid := `{"conv":0,"q":1,"question_id":"conv-0-q-1","category":1,"question":"Q","predicted":"A","answer_regime":"r","retrieval_flags":"f","input_tokens":1,"answer_context_tokens":1}`
	for name, content := range map[string]string{
		"malformed": valid + "\n{",
		"duplicate": valid + "\n" + valid + "\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadAdjudicationCandidateSource(path); err == nil {
			t.Fatalf("%s input was accepted", name)
		}
	}
}

func TestAdjudicationPacketStrictDecodeAndDigest(t *testing.T) {
	packet := testAdjudicationPacket()
	packet.PacketDigest = adjudicationPacketDigest(packet)
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeAdjudicationPacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.PacketDigest != packet.PacketDigest {
		t.Fatal("packet digest changed after decode")
	}
	withForbidden := strings.TrimSuffix(string(raw), "}") + `,"gold":"leak"}`
	if _, err := decodeAdjudicationPacket([]byte(withForbidden)); err == nil {
		t.Fatal("packet with forbidden/unknown field was accepted")
	}
	decoded.Question = "tampered"
	if err := validateAdjudicationPacket(decoded, "sha256:protocol"); err == nil {
		t.Fatal("tampered packet digest was accepted")
	}
}

func testAdjudicationPacket() adjudicationPacket {
	protocolHash := adjudicationTextDigest("protocol")
	evidence := make([]adjudicationEvidenceItem, 30)
	for i := range evidence {
		content := "evidence"
		evidence[i] = adjudicationEvidenceItem{
			EvidenceID: fmtEvidenceID(i + 1), Rank: i + 1, Content: content,
			ContentDigest: adjudicationTextDigest(content),
		}
	}
	candidates := []adjudicationPacketCandidate{
		{Slot: "C1", Answer: "one", AnswerDigest: adjudicationTextDigest("one")},
		{Slot: "C2", Answer: "two", AnswerDigest: adjudicationTextDigest("two")},
		{Slot: "C3", Answer: "two", AnswerDigest: adjudicationTextDigest("two")},
	}
	return adjudicationPacket{
		Schema: adjudicationPacketSchema, ProtocolHash: protocolHash, PacketID: "packet:1",
		Conv: 0, Q: 1, QuestionID: "conv-0-q-1", Category: 1, Question: "Q?",
		Triggered: true, ContextParity: true, Evidence: evidence, Candidates: candidates,
	}
}

func writeAdjudicationPublicFixture(t *testing.T, dir string, packets []adjudicationPacket) (adjudicationManifest, []adjudicationPacket) {
	t.Helper()
	manifest := adjudicationManifest{
		Schema: adjudicationManifestSchema, ProtocolID: "test", Normalizer: "ascii_lower_alnum_v1",
		PermutationSeedDigest:           adjudicationTextDigest("seed"),
		SanitizedCandidateSourceDigests: []string{adjudicationTextDigest("s1"), adjudicationTextDigest("s2"), adjudicationTextDigest("s3")},
		SanitizedTraceDigest:            adjudicationTextDigest("trace"), StoreSemanticDigest: adjudicationTextDigest("store"),
		QuestionIDsDigest: adjudicationTextDigest("questions"), PromptDigest: adjudicationPromptDigest(),
		QuestionCount: len(packets),
	}
	for _, packet := range packets {
		if packet.Triggered {
			manifest.TriggeredCount++
		}
		if packet.ContextParity {
			manifest.ContextParityCount++
			if packet.Triggered {
				manifest.TriggeredContextParityCount++
			}
		}
	}
	manifest.ProtocolHash = adjudicationManifestProtocolHash(manifest)
	for i := range packets {
		packets[i].ProtocolHash = manifest.ProtocolHash
		packets[i].PacketDigest = adjudicationPacketDigest(packets[i])
	}
	digest, raw, err := adjudicationJSONLDigest(packets)
	if err != nil {
		t.Fatal(err)
	}
	manifest.PacketSetDigest = digest
	if err := writeAdjudicationAtomic(filepath.Join(dir, adjudicationPacketsFile), raw); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, adjudicationManifestFile), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest, packets
}
