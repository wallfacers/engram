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
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/provider"
)

func TestAdjudicationAuditModesRejectEveryMixedPair(t *testing.T) {
	modes := []func(*options){
		func(opt *options) { opt.adjudicationBuildDir = "034-build" },
		func(opt *options) { opt.adjudicationValidateDir = "034-validate" },
		func(opt *options) { opt.adjudicationRunDir = "034-run" },
		func(opt *options) { opt.adjudicationScoreDir = "034-score" },
		func(opt *options) { opt.adjudicationAuditBuildDir = "035-build" },
		func(opt *options) { opt.adjudicationAuditValidateDir = "035-validate" },
		func(opt *options) { opt.adjudicationAuditRunDir = "035-run" },
		func(opt *options) { opt.adjudicationAuditScoreDir = "035-score" },
	}
	for left := range modes {
		for right := left + 1; right < len(modes); right++ {
			var opt options
			modes[left](&opt)
			modes[right](&opt)
			if _, err := adjudicationModeFor(opt); err == nil {
				t.Fatalf("mixed modes %d/%d were accepted", left, right)
			}
		}
	}
}

func TestAdjudicationAuditModeOwnedOptionsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		opt  options
		mode adjudicationMode
	}{
		{name: "build-needs-source", opt: options{adjudicationAuditBuildDir: "out", adjudicationAuditSeed: "seed"}, mode: adjudicationAuditModeBuild},
		{name: "validate-forbids-source", opt: options{adjudicationAuditValidateDir: "out", adjudicationSourceDir: "parent"}, mode: adjudicationAuditModeValidate},
		{name: "run-needs-paid", opt: options{adjudicationAuditRunDir: "out", adjudicationAuditMaxTokens: 768}, mode: adjudicationAuditModeRun},
		{name: "score-needs-three-candidates", opt: options{adjudicationAuditScoreDir: "out", adjudicationSourceDir: "parent"}, mode: adjudicationAuditModeScore},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateAdjudicationAuditCLIOptions(test.opt, test.mode); err == nil {
				t.Fatal("invalid mode-owned options were accepted")
			}
		})
	}
}

func TestAdjudicationAuditAuxiliaryOptionsRequireAuditMode(t *testing.T) {
	for _, opt := range []options{
		{adjudicationSourceDir: "parent"},
		{adjudicationAuditSeed: "seed"},
		{adjudicationAuditAllowPaid: true},
		{adjudicationBuildDir: "034", adjudicationSourceDir: "parent"},
	} {
		if _, err := adjudicationModeFor(opt); err == nil {
			t.Fatalf("orphan/cross-protocol audit option was accepted: %#v", opt)
		}
	}
}

func TestAdjudicationAuditValidModeOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  options
		mode adjudicationMode
	}{
		{name: "build", opt: options{adjudicationAuditBuildDir: "out", adjudicationSourceDir: "parent", adjudicationAuditSeed: "seed"}, mode: adjudicationAuditModeBuild},
		{name: "validate", opt: options{adjudicationAuditValidateDir: "out"}, mode: adjudicationAuditModeValidate},
		{name: "run", opt: options{adjudicationAuditRunDir: "out", adjudicationAuditAllowPaid: true, adjudicationAuditMaxTokens: 768, concurrency: 32}, mode: adjudicationAuditModeRun},
		{name: "score", opt: options{adjudicationAuditScoreDir: "out", adjudicationSourceDir: "parent", adjudicationCandidates: []string{"a", "b", "c"}}, mode: adjudicationAuditModeScore},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateAdjudicationAuditCLIOptions(test.opt, test.mode); err != nil {
				t.Fatalf("valid options rejected: %v", err)
			}
		})
	}
}

func TestAdjudicationAuditErrorsDoNotEchoDirectories(t *testing.T) {
	secretLikePath := "https://user:password@example.test/private"
	opt := options{adjudicationAuditBuildDir: secretLikePath}
	err := validateAdjudicationAuditCLIOptions(opt, adjudicationAuditModeBuild)
	if err == nil {
		t.Fatal("incomplete build options were accepted")
	}
	if strings.Contains(err.Error(), secretLikePath) {
		t.Fatalf("option error echoed operator path: %v", err)
	}
}

func TestAdjudicationAuditParentReplayRejectsTamper(t *testing.T) {
	dir := t.TempDir()
	parentManifest, parentPackets := writeAdjudicationPublicFixture(t, dir, []adjudicationPacket{testAdjudicationAuditParentPacket()})
	config := adjudicationRunConfig{
		Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "stub", ModelRevision: "v1",
		Concurrency: 1, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("binary"), PricingStatus: "unpriced",
	}
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"selected_slot":"C3","evidence_ids":["E01"],"confidence":"high"}`, provider.Usage{}, nil
	}
	if _, err := executeAdjudicationRun(context.Background(), dir, parentManifest, parentPackets, config, caller); err != nil {
		t.Fatal(err)
	}
	receipt, packets, decisions, err := loadAndValidateAdjudicationAuditParent(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.QuestionCount != 1 || len(packets) != 1 || len(decisions) != 1 || receipt.CallsRawDigest == "" {
		t.Fatalf("incomplete parent replay: %#v packets=%d decisions=%d", receipt, len(packets), len(decisions))
	}
	sealPath := filepath.Join(dir, adjudicationSealFile)
	raw, err := os.ReadFile(sealPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sealPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadAndValidateAdjudicationAuditParent(dir, false); err == nil {
		t.Fatal("tampered parent seal bytes were accepted")
	}
}

func TestAdjudicationAuditBuildArtifactsRoundTripByteStable(t *testing.T) {
	parent := testAdjudicationAuditParentPacket()
	decision := selectedAdjudicationDecision(parent, adjudicationVerifierResponse{
		SelectedSlot: "C3", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	manifest, packets, resolver, err := deriveAdjudicationAuditArtifacts(
		adjudicationAuditParentReceipt{ProtocolHash: adjudicationTextDigest("parent")},
		[]adjudicationPacket{parent}, []adjudicationDecision{decision}, "seed",
	)
	if err != nil {
		t.Fatal(err)
	}
	left, right := t.TempDir(), t.TempDir()
	if err := writeAdjudicationAuditBuild(left, manifest, packets, resolver); err != nil {
		t.Fatal(err)
	}
	if err := writeAdjudicationAuditBuild(right, manifest, packets, resolver); err != nil {
		t.Fatal(err)
	}
	loadedManifest, loadedPackets, loadedResolver, err := loadAndValidateAdjudicationAuditBuild(left, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedManifest, manifest) || !reflect.DeepEqual(loadedPackets, packets) || !reflect.DeepEqual(loadedResolver, resolver) {
		t.Fatal("round-trip build artifacts changed")
	}
	for _, name := range []string{adjudicationAuditManifestFile, adjudicationAuditPacketsFile, adjudicationAuditResolverMapFile} {
		leftRaw, err := os.ReadFile(filepath.Join(left, name))
		if err != nil {
			t.Fatal(err)
		}
		rightRaw, err := os.ReadFile(filepath.Join(right, name))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(leftRaw, rightRaw) {
			t.Fatalf("%s is not byte-stable", name)
		}
	}
}

func TestExecuteAdjudicationAuditRunCallsBothViewsAndSeals(t *testing.T) {
	manifest, packets, resolver := testAdjudicationAuditRunFixture(t)
	config := adjudicationRunConfig{
		Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "stub", ModelRevision: "v1",
		Concurrency: 32, MaxTokens: 768, BinaryDigest: adjudicationTextDigest("binary"), PricingStatus: "unpriced",
	}
	var calls int32
	caller := func(_ context.Context, _, _ string) (string, provider.Usage, error) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			return "", provider.Usage{InputTokens: 3}, errors.New("closed provider failure")
		}
		return `{"assessments":[{"slot":"A1","support":{"value":"unclear","evidence_ids":[]},"contradiction":{"value":"unclear","evidence_ids":[]}},{"slot":"A2","support":{"value":"unclear","evidence_ids":[]},"contradiction":{"value":"unclear","evidence_ids":[]}}]}`,
			provider.Usage{InputTokens: 5, OutputTokens: 2}, nil
	}
	dir := t.TempDir()
	seal, err := executeAdjudicationAuditRun(context.Background(), dir, manifest, packets, resolver, config, caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || seal.PlannedCalls != 2 || seal.StartedCalls != 2 || seal.TerminalCalls != 2 ||
		seal.ProviderAttempts != 2 || seal.Retries != 0 || seal.FailedCalls != 1 || seal.DecisionCount != 2 {
		t.Fatalf("unexpected audit seal/calls: calls=%d seal=%#v", calls, seal)
	}
	validated, decisions, err := validateAdjudicationAuditSeal(dir, manifest, packets, resolver, false)
	if err != nil {
		t.Fatal(err)
	}
	if validated.SealDigest != seal.SealDigest || len(decisions) != 2 || decisions[0].Resolution != adjudicationAuditResolutionRetained ||
		decisions[1].Resolution != adjudicationAuditResolutionRetainedNonrisk {
		t.Fatalf("invalid resolved decisions: %#v", decisions)
	}
	for _, name := range []string{adjudicationAuditDecisionsFile, adjudicationAuditSealFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	atomic.StoreInt32(&calls, 0)
	resumed, err := executeAdjudicationAuditRun(context.Background(), dir, manifest, packets, resolver, config, caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || resumed.SealDigest != seal.SealDigest {
		t.Fatalf("resume repeated calls or changed seal: calls=%d seal=%#v", calls, resumed)
	}
	drifted := config
	drifted.Model = "different-model"
	if _, err := executeAdjudicationAuditRun(context.Background(), dir, manifest, packets, resolver, drifted, caller); err == nil {
		t.Fatal("completed audit accepted changed model identity")
	}
	if calls != 0 {
		t.Fatalf("identity-drift validation made %d calls", calls)
	}
}

func TestAdjudicationAuditJournalRejectsOrphanStarted(t *testing.T) {
	manifest, packets, _ := testAdjudicationAuditRunFixture(t)
	path := filepath.Join(t.TempDir(), adjudicationAuditCallsFile)
	journal, err := openAdjudicationAuditCallJournal(path, manifest.ProtocolHash, packets)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Start(packets[0], packets[0].Views[0], adjudicationTextDigest("input")); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openAdjudicationAuditCallJournal(path, manifest.ProtocolHash, packets); err == nil {
		t.Fatal("orphan STARTED record was resumable")
	}
}

func testAdjudicationAuditRunFixture(t *testing.T) (adjudicationAuditManifest, []adjudicationAuditPacket, []adjudicationAuditResolverMapRecord) {
	t.Helper()
	risk := testAdjudicationAuditParentPacket()
	risk.PacketID = "packet:conv-0-q-0"
	risk.Q = 0
	risk.QuestionID = questionID(0, 0)
	risk.PacketDigest = adjudicationPacketDigest(risk)
	riskDecision := selectedAdjudicationDecision(risk, adjudicationVerifierResponse{
		SelectedSlot: "C3", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	nonrisk := testAdjudicationAuditParentPacket()
	nonrisk.PacketID = "packet:conv-0-q-1"
	nonrisk.Q = 1
	nonrisk.QuestionID = questionID(0, 1)
	nonrisk.PacketDigest = adjudicationPacketDigest(nonrisk)
	nonriskDecision := selectedAdjudicationDecision(nonrisk, adjudicationVerifierResponse{
		SelectedSlot: "C1", EvidenceIDs: []string{"E01"}, Confidence: "high",
	}, provider.Usage{})
	manifest, packets, resolver, err := deriveAdjudicationAuditArtifacts(
		adjudicationAuditParentReceipt{ProtocolHash: adjudicationTextDigest("parent")},
		[]adjudicationPacket{risk, nonrisk}, []adjudicationDecision{riskDecision, nonriskDecision}, "seed",
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, packets, resolver
}

func TestExecuteAdjudicationAuditFrozenOfflineStub(t *testing.T) {
	fixtureDir := os.Getenv("ADJUDICATION_AUDIT_OFFLINE_FIXTURE_DIR")
	outputRoot := os.Getenv("ADJUDICATION_AUDIT_OFFLINE_OUTPUT_DIR")
	if fixtureDir == "" || outputRoot == "" {
		t.Skip("set ADJUDICATION_AUDIT_OFFLINE_FIXTURE_DIR and ADJUDICATION_AUDIT_OFFLINE_OUTPUT_DIR for the frozen receipt")
	}
	manifest, packets, resolver, err := loadAndValidateAdjudicationAuditBuild(fixtureDir, true)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	config := adjudicationRunConfig{
		Provider: "offline-stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "all-unclear",
		ModelRevision: "v1", Concurrency: 32, MaxTokens: 768, BinaryDigest: adjudicationTextDigest("offline-binary"),
		PricingStatus: "declared_zero", InputCNYPerMillion: &zero, OutputCNYPerMillion: &zero,
	}
	caller := func(_ context.Context, _, user string) (string, provider.Usage, error) {
		var input struct {
			Candidates []adjudicationAuditViewCandidate `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(user), &input); err != nil {
			return "", provider.Usage{}, err
		}
		response := adjudicationAuditResponse{Assessments: make([]adjudicationAuditCandidateAssessment, 0, len(input.Candidates))}
		for _, candidate := range input.Candidates {
			response.Assessments = append(response.Assessments, adjudicationAuditCandidateAssessment{
				Slot:          candidate.Slot,
				Support:       adjudicationAuditAxis{Value: "unclear", EvidenceIDs: []string{}},
				Contradiction: adjudicationAuditAxis{Value: "unclear", EvidenceIDs: []string{}},
			})
		}
		raw, err := json.Marshal(response)
		return string(raw), provider.Usage{InputTokens: 7, OutputTokens: 2}, err
	}
	var seals []adjudicationAuditSeal
	for run := 1; run <= 2; run++ {
		dir := filepath.Join(outputRoot, fmt.Sprintf("run-%d", run))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{adjudicationAuditManifestFile, adjudicationAuditPacketsFile, adjudicationAuditResolverMapFile} {
			raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		seal, err := executeAdjudicationAuditRun(context.Background(), dir, manifest, packets, resolver, config, caller)
		if err != nil {
			t.Fatal(err)
		}
		if _, decisions, err := validateAdjudicationAuditSeal(dir, manifest, packets, resolver, true); err != nil {
			t.Fatal(err)
		} else if len(decisions) != adjudicationAuditFrozenQuestionCount {
			t.Fatalf("decisions = %d", len(decisions))
		}
		seals = append(seals, seal)
	}
	if seals[0].DecisionSetDigest != seals[1].DecisionSetDigest ||
		seals[0].CanonicalCallStateDigest != seals[1].CanonicalCallStateDigest ||
		seals[0].ProviderAttempts != adjudicationAuditFrozenViewCount || seals[0].Retries != 0 {
		t.Fatalf("offline frozen audit is not deterministic: %#v %#v", seals[0], seals[1])
	}
}

func TestScoreSealedAdjudicationAuditDoesNotLoadHiddenBeforeAuditSeal(t *testing.T) {
	parentDir := t.TempDir()
	parentManifest, parentPackets := writeAdjudicationPublicFixture(t, parentDir, []adjudicationPacket{testAdjudicationAuditParentPacket()})
	config := adjudicationRunConfig{
		Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "stub", ModelRevision: "v1",
		Concurrency: 1, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("binary"), PricingStatus: "unpriced",
	}
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"selected_slot":"C3","evidence_ids":["E01"],"confidence":"high"}`, provider.Usage{}, nil
	}
	if _, err := executeAdjudicationRun(context.Background(), parentDir, parentManifest, parentPackets, config, caller); err != nil {
		t.Fatal(err)
	}
	receipt, packets, decisions, err := loadAndValidateAdjudicationAuditParent(parentDir, false)
	if err != nil {
		t.Fatal(err)
	}
	auditManifest, auditPackets, resolver, err := deriveAdjudicationAuditArtifacts(receipt, packets, decisions, "seed")
	if err != nil {
		t.Fatal(err)
	}
	auditDir := t.TempDir()
	if err := writeAdjudicationAuditBuild(auditDir, auditManifest, auditPackets, resolver); err != nil {
		t.Fatal(err)
	}
	hiddenReads := 0
	_, err = scoreSealedAdjudicationAudit(parentDir, auditDir, false, func() (adjudicationHiddenInputs, error) {
		hiddenReads++
		return adjudicationHiddenInputs{}, nil
	})
	if err == nil {
		t.Fatal("unsealed audit was scoreable")
	}
	if hiddenReads != 0 {
		t.Fatalf("invalid seal caused %d hidden reads", hiddenReads)
	}
}
