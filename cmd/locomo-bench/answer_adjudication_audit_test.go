package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func TestAdjudicationAuditGroupsCollapseNormalizedDuplicates(t *testing.T) {
	packet := testAdjudicationAuditParentPacket()
	groups, err := groupAdjudicationAuditAnswers(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2: %#v", len(groups), groups)
	}
	seenMembers := 0
	for _, group := range groups {
		seenMembers += len(group.MemberAnswerDigests)
		if group.GroupDigest == "" || group.NormalizedDigest == "" || group.RepresentativeParentSlot == "" ||
			group.RepresentativeAnswerDigest == "" {
			t.Fatalf("incomplete group: %#v", group)
		}
	}
	if seenMembers != 3 {
		t.Fatalf("member coverage = %d, want 3", seenMembers)
	}
}

func TestAdjudicationAuditViewsAreDeterministicAndDeranged(t *testing.T) {
	packet := testAdjudicationAuditParentPacket()
	groups, err := groupAdjudicationAuditAnswers(packet)
	if err != nil {
		t.Fatal(err)
	}
	first, firstMaps, err := buildAdjudicationAuditViews("fixed-seed", packet, groups)
	if err != nil {
		t.Fatal(err)
	}
	second, secondMaps, err := buildAdjudicationAuditViews("fixed-seed", packet, groups)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstMaps, secondMaps) {
		t.Fatal("same seed did not reproduce byte-equivalent views")
	}
	if len(first) != 2 || first[0].ViewID != adjudicationAuditViewEntailment || first[1].ViewID != adjudicationAuditViewFalsification {
		t.Fatalf("unexpected views: %#v", first)
	}
	if reflect.DeepEqual(first[0].Candidates, first[1].Candidates) {
		t.Fatal("falsification view did not rotate candidate order")
	}
	for _, view := range first {
		for i, candidate := range view.Candidates {
			if candidate.Slot != "A"+string(rune('1'+i)) || strings.TrimSpace(candidate.RepresentativeAnswer) == "" {
				t.Fatalf("invalid view-local candidate: %#v", candidate)
			}
		}
	}
}

func TestAdjudicationAuditPacketStrictSchemaAndProviderSafety(t *testing.T) {
	parent := testAdjudicationAuditParentPacket()
	groups, err := groupAdjudicationAuditAnswers(parent)
	if err != nil {
		t.Fatal(err)
	}
	views, _, err := buildAdjudicationAuditViews("seed", parent, groups)
	if err != nil {
		t.Fatal(err)
	}
	packet := adjudicationAuditPacket{
		Schema: adjudicationAuditPacketSchema, ProtocolHash: adjudicationTextDigest("protocol"), PacketID: parent.PacketID,
		Conv: parent.Conv, Q: parent.Q, QuestionID: parent.QuestionID, Category: parent.Category,
		Question: parent.Question, ContextParity: parent.ContextParity, Evidence: parent.Evidence, Views: views,
	}
	packet.PacketDigest = adjudicationAuditPacketDigest(packet)
	if err := validateAdjudicationAuditPacket(packet, packet.ProtocolHash); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"selected`, `"current`, `"control`, `"fallback_reason`, `"source`, `"group_digest`, `"member`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("provider packet contains forbidden field %q: %s", forbidden, raw)
		}
	}
	raw = append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
	if _, err := decodeAdjudicationAuditPacket(raw); err == nil {
		t.Fatal("unknown packet field was accepted")
	}
}

func TestDeriveAdjudicationAuditQueueUsesOnlyDecisionMechanics(t *testing.T) {
	override := testAdjudicationAuditParentPacket()
	override.PacketID = "packet:conv-0-q-0"
	override.Q = 0
	override.QuestionID = questionID(0, 0)
	override.PacketDigest = adjudicationPacketDigest(override)
	overrideDecision := selectedAdjudicationDecision(override, adjudicationVerifierResponse{
		SelectedSlot: "C3", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, providerUsage(1, 1))

	fallback := testAdjudicationAuditParentPacket()
	fallback.PacketID = "packet:conv-0-q-1"
	fallback.Q = 1
	fallback.QuestionID = questionID(0, 1)
	fallback.PacketDigest = adjudicationPacketDigest(fallback)
	fallbackDecision := fallbackAdjudicationDecision(fallback, adjudicationFallbackInvalidResponse, 1, providerUsage(2, 1))

	nonrisk := testAdjudicationAuditParentPacket()
	nonrisk.PacketID = "packet:conv-0-q-2"
	nonrisk.Q = 2
	nonrisk.QuestionID = questionID(0, 2)
	nonrisk.PacketDigest = adjudicationPacketDigest(nonrisk)
	nonriskDecision := selectedAdjudicationDecision(nonrisk, adjudicationVerifierResponse{
		SelectedSlot: "C1", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, providerUsage(1, 1))

	manifest, packets, resolver, err := deriveAdjudicationAuditArtifacts(
		adjudicationAuditParentReceipt{ProtocolHash: adjudicationTextDigest("parent")},
		[]adjudicationPacket{override, fallback, nonrisk},
		[]adjudicationDecision{overrideDecision, fallbackDecision, nonriskDecision},
		"queue-seed",
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.QuestionCount != 3 || manifest.RiskCount != 2 || manifest.OverrideCount != 1 ||
		manifest.FallbackCount != 1 || manifest.RetainCount != 1 || manifest.PlannedCalls != 4 ||
		len(packets) != 2 || len(resolver) != 3 {
		t.Fatalf("unexpected queue receipt: %#v packets=%d resolver=%d", manifest, len(packets), len(resolver))
	}
	if !resolver[0].Risk || !resolver[1].Risk || resolver[2].Risk {
		t.Fatalf("risk mapping mismatch: %#v", resolver)
	}
}

func testAdjudicationAuditParentPacket() adjudicationPacket {
	packet := testAdjudicationPacket()
	packet.Candidates = []adjudicationPacketCandidate{
		{Slot: "C1", Answer: "Yes", AnswerDigest: adjudicationTextDigest("Yes")},
		{Slot: "C2", Answer: "YES!", AnswerDigest: adjudicationTextDigest("YES!")},
		{Slot: "C3", Answer: "No", AnswerDigest: adjudicationTextDigest("No")},
	}
	packet.Triggered = true
	packet.PacketDigest = adjudicationPacketDigest(packet)
	return packet
}

func providerUsage(input, output int) provider.Usage {
	return provider.Usage{InputTokens: input, OutputTokens: output}
}

func TestAdjudicationAuditPromptAndStrictResponse(t *testing.T) {
	packet, _ := testAdjudicationAuditRiskFixture(t)
	for _, view := range packet.Views {
		prompt, err := buildAdjudicationAuditPrompt(packet, view.ViewID)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"parent_selected", "text_control", "group_digest", "fallback_reason", "source_digest"} {
			if strings.Contains(strings.ToLower(prompt), forbidden) {
				t.Fatalf("prompt leaked %q: %s", forbidden, prompt)
			}
		}
		valid := `{"assessments":[{"slot":"A1","support":{"value":"yes","evidence_ids":["E01"]},"contradiction":{"value":"no","evidence_ids":[]}},{"slot":"A2","support":{"value":"unclear","evidence_ids":[]},"contradiction":{"value":"yes","evidence_ids":["E02"]}}]}`
		if _, err := parseAdjudicationAuditResponse(valid, packet, view.ViewID); err != nil {
			t.Fatalf("valid response rejected: %v", err)
		}
		invalid := []string{
			valid + ` trailing`,
			strings.Replace(valid, `}]}`, `}],"recommended_slot":"A1"}`, 1),
			strings.Replace(valid, `"evidence_ids":["E01"]`, `"evidence_ids":[]`, 1),
			strings.Replace(valid, `"slot":"A2"`, `"slot":"A1"`, 1),
			`{"assessments":[{"slot":"A1","support":{"value":"yes","evidence_ids":["E99"]},"contradiction":{"value":"no","evidence_ids":[]}}]}`,
		}
		for index, raw := range invalid {
			if _, err := parseAdjudicationAuditResponse(raw, packet, view.ViewID); err == nil {
				t.Fatalf("invalid response %d was accepted: %s", index, raw)
			}
		}
	}
}

func TestAdjudicationAuditResolverSwitchesOnlyOnDualConvergence(t *testing.T) {
	packet, resolver := testAdjudicationAuditRiskFixture(t)
	terminals := make([]adjudicationAuditCallRecord, 0, 2)
	for _, viewMap := range resolver.ViewMaps {
		view, ok := findAdjudicationAuditView(packet, viewMap.ViewID)
		if !ok {
			t.Fatal("fixture lacks audit view")
		}
		assessments := make([]adjudicationAuditCandidateAssessment, 0, len(viewMap.SlotToGroup))
		for index := 0; index < len(viewMap.SlotToGroup); index++ {
			slot := fmt.Sprintf("A%d", index+1)
			group := viewMap.SlotToGroup[slot]
			assessment := adjudicationAuditCandidateAssessment{
				Slot:          slot,
				Support:       adjudicationAuditAxis{Value: "no", EvidenceIDs: []string{}},
				Contradiction: adjudicationAuditAxis{Value: "no", EvidenceIDs: []string{}},
			}
			if group == resolver.ParentSelectedGroupDigest {
				assessment.Contradiction = adjudicationAuditAxis{Value: "yes", EvidenceIDs: []string{"E01"}}
			} else {
				assessment.Support = adjudicationAuditAxis{Value: "yes", EvidenceIDs: []string{"E02"}}
			}
			assessments = append(assessments, assessment)
		}
		record := adjudicationAuditCallRecord{
			Schema: adjudicationAuditCallSchema, ProtocolHash: packet.ProtocolHash, PacketID: packet.PacketID,
			PacketDigest: packet.PacketDigest, ViewID: viewMap.ViewID, ViewDigest: view.ViewDigest, State: adjudicationAuditCallCompleted,
			Attempt: 1, InputDigest: adjudicationTextDigest("input-" + viewMap.ViewID), Assessments: assessments,
		}
		record.TerminalDigest = adjudicationAuditCallTerminalDigest(record)
		terminals = append(terminals, record)
	}
	decision, err := resolveAdjudicationAuditDecision(resolver, packet, terminals)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Resolution != adjudicationAuditResolutionSwitched || decision.FinalGroupDigest == resolver.ParentSelectedGroupDigest {
		t.Fatalf("dual convergence did not switch: %#v", decision)
	}

	for index := range terminals[0].Assessments {
		if resolver.ViewMaps[0].SlotToGroup[terminals[0].Assessments[index].Slot] == resolver.ParentSelectedGroupDigest {
			terminals[0].Assessments[index].Support = adjudicationAuditAxis{Value: "yes", EvidenceIDs: []string{"E03"}}
		}
	}
	terminals[0].TerminalDigest = adjudicationAuditCallTerminalDigest(terminals[0])
	decision, err = resolveAdjudicationAuditDecision(resolver, packet, terminals)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Resolution != adjudicationAuditResolutionRetained || decision.FinalAnswerDigest != resolver.ParentSelectedAnswerDigest {
		t.Fatalf("conflicting audit did not retain exact parent: %#v", decision)
	}
}

func testAdjudicationAuditRiskFixture(t *testing.T) (adjudicationAuditPacket, adjudicationAuditResolverMapRecord) {
	t.Helper()
	parent := testAdjudicationAuditParentPacket()
	decision := selectedAdjudicationDecision(parent, adjudicationVerifierResponse{
		SelectedSlot: "C3", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	_, packets, resolver, err := deriveAdjudicationAuditArtifacts(
		adjudicationAuditParentReceipt{ProtocolHash: adjudicationTextDigest("parent")},
		[]adjudicationPacket{parent}, []adjudicationDecision{decision}, "seed",
	)
	if err != nil {
		t.Fatal(err)
	}
	return packets[0], resolver[0]
}

func TestAdjudicationAuditStage0GateIsStrict(t *testing.T) {
	base := adjudicationAuditStage0GateInput{
		NewCorrect: 1387, NewLower: 1387, QuestionCount: 1540,
		MixedCorrect: 69, MixedLower: 69, MixedTotal: 88,
		NewOnly: 10, ParentOnly: 1, McNemarP: 0.049, TemporalNet: 0,
		IntegrityValid: true, FrozenDiagnosticsValid: true,
		Categories: map[string]evalCategoryGateResult{"temporal": {Category: "temporal"}},
	}
	if got := adjudicationAuditStage0Verdict(base); got != "GO" {
		t.Fatalf("exact audit gate = %q", got)
	}
	mutations := map[string]func(*adjudicationAuditStage0GateInput){
		"point":       func(in *adjudicationAuditStage0GateInput) { in.NewCorrect-- },
		"lower":       func(in *adjudicationAuditStage0GateInput) { in.NewLower-- },
		"mixed":       func(in *adjudicationAuditStage0GateInput) { in.MixedCorrect-- },
		"mixed-lower": func(in *adjudicationAuditStage0GateInput) { in.MixedLower-- },
		"net":         func(in *adjudicationAuditStage0GateInput) { in.ParentOnly++ },
		"p":           func(in *adjudicationAuditStage0GateInput) { in.McNemarP = 0.05 },
		"temporal":    func(in *adjudicationAuditStage0GateInput) { in.TemporalNet = -1 },
		"integrity":   func(in *adjudicationAuditStage0GateInput) { in.IntegrityValid = false },
		"frozen":      func(in *adjudicationAuditStage0GateInput) { in.FrozenDiagnosticsValid = false },
		"category": func(in *adjudicationAuditStage0GateInput) {
			in.Categories["temporal"] = evalCategoryGateResult{Category: "temporal", HolmSignificantNegative: true}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.Categories = map[string]evalCategoryGateResult{"temporal": base.Categories["temporal"]}
			mutate(&candidate)
			if got := adjudicationAuditStage0Verdict(candidate); got != "NO_GO" {
				t.Fatalf("failed gate returned %q", got)
			}
		})
	}
}

func TestScoreAdjudicationAuditDecisionsComputesPairedTemporalAndBounds(t *testing.T) {
	parent := testAdjudicationAuditParentPacket()
	parent.Category = 2
	parent.PacketDigest = adjudicationPacketDigest(parent)
	parentDecision := selectedAdjudicationDecision(parent, adjudicationVerifierResponse{
		SelectedSlot: "C1", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	auditDecision := adjudicationAuditDecision{
		Schema: adjudicationAuditDecisionSchema, ProtocolHash: adjudicationTextDigest("audit"), PacketID: parent.PacketID,
		ParentPacketDigest: parent.PacketDigest, ParentDecisionDigest: parentDecision.DecisionDigest,
		Conv: parent.Conv, Q: parent.Q, QuestionID: parent.QuestionID, FinalParentSlot: "C3",
		FinalAnswerDigest: parent.Candidates[2].AnswerDigest, FinalGroupDigest: adjudicationTextDigest("group"),
		AuditTerminalDigests: []string{adjudicationTextDigest("a"), adjudicationTextDigest("b")},
		Resolution:           adjudicationAuditResolutionSwitched, ResolutionReason: "dual_convergence", ProviderAttempts: 2,
	}
	auditDecision.DecisionDigest = adjudicationAuditDecisionDigest(auditDecision)
	hidden := adjudicationHiddenInputs{
		Sources: map[string]map[string]adjudicationHiddenCandidate{
			"s1": {parent.QuestionID: {Answer: "Yes", Normalized: "yes", Correct: true}},
			"s2": {parent.QuestionID: {Answer: "YES!", Normalized: "yes", Correct: true}},
			"s3": {parent.QuestionID: {Answer: "No", Normalized: "no", Correct: false}},
		},
		SlotMaps: map[string]adjudicationSlotMapRecord{parent.PacketID: {
			PacketID: parent.PacketID, Conv: parent.Conv, Q: parent.Q, QuestionID: parent.QuestionID,
			Slots: []adjudicationSlotSource{
				{Slot: "C1", SourceDigest: "s1", AnswerDigest: parent.Candidates[0].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("yes")},
				{Slot: "C2", SourceDigest: "s2", AnswerDigest: parent.Candidates[1].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("yes")},
				{Slot: "C3", SourceDigest: "s3", AnswerDigest: parent.Candidates[2].AnswerDigest, NormalizedAnswerDigest: adjudicationTextDigest("no")},
			},
		}}, IntegrityValid: true,
	}
	report, err := scoreAdjudicationAuditDecisions(
		adjudicationManifest{QuestionCount: 1, TriggeredCount: 1, ContextParityCount: 1, TriggeredContextParityCount: 1},
		[]adjudicationPacket{parent}, []adjudicationDecision{parentDecision},
		adjudicationAuditManifest{ProtocolHash: auditDecision.ProtocolHash, QuestionCount: 1, RiskCount: 1, ViewCount: 2, PlannedCalls: 2},
		[]adjudicationAuditDecision{auditDecision}, adjudicationAuditSeal{Valid: true}, hidden, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Parent.Correct != 1 || report.New.Correct != 0 || report.Paired.ParentOnly != 1 ||
		report.Paired.NewOnly != 0 || report.Paired.McNemarP != 1 || report.TemporalNet != -1 ||
		report.TriggeredMixedParent.Correct != 1 || report.TriggeredMixedNew.Correct != 0 ||
		report.ResultKind != "historical_verdict_mapping" || report.Verdict != "NO_GO" {
		t.Fatalf("unexpected audit score: %#v", report)
	}
}
