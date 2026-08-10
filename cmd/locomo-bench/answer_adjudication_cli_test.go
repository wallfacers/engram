package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
)

func TestAdjudicationModeValidationRejectsMixedModes(t *testing.T) {
	opt := options{adjudicationBuildDir: "build", adjudicationRunDir: "run"}
	if _, err := adjudicationModeFor(opt); err == nil {
		t.Fatal("mixed adjudication modes must fail")
	}
}

func TestAdjudicationModeValidationLeavesOrdinaryCLIUnselected(t *testing.T) {
	mode, err := adjudicationModeFor(options{})
	if err != nil || mode != "" {
		t.Fatalf("ordinary no-mode options selected %q: %v", mode, err)
	}
	if err := validateAdjudicationCLIOptions(options{
		adjudicationRunDir: "run", adjudicationMaxTokens: 64,
	}, adjudicationModeRun); err == nil {
		t.Fatal("run without explicit paid acknowledgement was accepted")
	}
	if err := validateAdjudicationCLIOptions(options{
		adjudicationRunDir: "run", adjudicationMaxTokens: 64, adjudicationAllowPaid: true,
	}, adjudicationModeRun); err != nil {
		t.Fatalf("valid paid run options: %v", err)
	}
}

func TestReadAdjudicationEvidenceImmutableStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conv0.db")
	st, err := store.Open(ctx, store.Options{DSN: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2023, 5, 8, 0, 0, 0, 0, time.UTC)
	entry := &memory.Entry{Name: "fact", Content: "painted a sunrise", EventDate: &when, SourceSessionID: "conv0-sess1"}
	if err := memory.NewEntryStore(st.DB()).Upsert(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	before, err := fileSHA256(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	items, semantic, err := readAdjudicationEvidenceForConv(ctx, dbPath, []adjudicationTraceHit{{Name: "fact", Rank: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EvidenceID != "E01" || items[0].Content == "" || semantic == "" {
		t.Fatalf("unexpected evidence: %#v semantic=%q", items, semantic)
	}
	after, err := fileSHA256(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("immutable evidence read changed DB: %s -> %s", before, after)
	}
}

func TestAdjudicationProviderConfigIsExplicitAndSecretSafe(t *testing.T) {
	values := map[string]string{
		"ADJUDICATOR_PROVIDER": "openai", "ADJUDICATOR_BASE_URL": "https://example.test/v1",
		"ADJUDICATOR_MODEL": "verifier", "ADJUDICATOR_MODEL_REVISION": "rev-1",
		"ADJUDICATOR_API_KEY": "test-only-secret",
	}
	getenv := func(key string) string { return values[key] }
	config, err := loadAdjudicationProviderConfig(getenv, 32, 128)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), values["ADJUDICATOR_API_KEY"]) || strings.Contains(string(raw), values["ADJUDICATOR_BASE_URL"]) {
		t.Fatalf("serialized provider config leaked a secret or raw URL: %s", raw)
	}
	delete(values, "ADJUDICATOR_MODEL_REVISION")
	if _, err := loadAdjudicationProviderConfig(getenv, 32, 128); err == nil {
		t.Fatal("incomplete ADJUDICATOR_* configuration was accepted")
	}
	values["ADJUDICATOR_MODEL_REVISION"] = "rev-1"
	values["ADJUDICATOR_BASE_URL"] = "https://user:pass@example.test/v1?token=x"
	if _, err := loadAdjudicationProviderConfig(getenv, 32, 128); err == nil {
		t.Fatal("unsafe adjudicator base URL was accepted")
	}
}

func TestExecuteAdjudicationRunResumeBindsModelIdentity(t *testing.T) {
	dir := t.TempDir()
	manifest, packets := writeAdjudicationPublicFixture(t, dir, []adjudicationPacket{testAdjudicationPacket()})
	config := adjudicationRunConfig{
		Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "model-a",
		ModelRevision: "v1", Concurrency: 1, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("test-binary"),
	}
	calls := 0
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		calls++
		return `{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"high"}`, provider.Usage{}, nil
	}
	first, err := executeAdjudicationRun(context.Background(), dir, manifest, packets, config, caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("first run calls = %d", calls)
	}
	for _, name := range []string{adjudicationDecisionsFile, adjudicationSealFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	calls = 0
	resumed, err := executeAdjudicationRun(context.Background(), dir, manifest, packets, config, caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || resumed.DecisionSetDigest != first.DecisionSetDigest {
		t.Fatalf("resume calls=%d seal=%#v", calls, resumed)
	}
	for _, name := range []string{adjudicationDecisionsFile, adjudicationSealFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	calls = 0
	drifted := config
	drifted.Model = "model-b"
	if _, err := executeAdjudicationRun(context.Background(), dir, manifest, packets, drifted, caller); err == nil {
		t.Fatal("resume accepted changed model identity")
	}
	if calls != 0 {
		t.Fatalf("identity-drift resume made %d calls", calls)
	}
}

func TestExecuteAdjudicationRunConcurrencyAndDeterministicSeal(t *testing.T) {
	const triggered = 40
	packets := make([]adjudicationPacket, 0, triggered+1)
	protocolHash := adjudicationTextDigest("runner-protocol")
	for i := 0; i < triggered+1; i++ {
		packet := testAdjudicationPacket()
		packet.ProtocolHash = protocolHash
		packet.PacketID = "packet:" + questionID(0, i)
		packet.Q = i
		packet.QuestionID = questionID(0, i)
		if i == triggered {
			packet.Candidates[0].Answer = "same"
			packet.Candidates[0].AnswerDigest = adjudicationTextDigest("same")
			packet.Candidates[1] = packet.Candidates[0]
			packet.Candidates[1].Slot = "C2"
			packet.Candidates[2] = packet.Candidates[0]
			packet.Candidates[2].Slot = "C3"
			packet.Triggered = false
		}
		packet.PacketDigest = adjudicationPacketDigest(packet)
		packets = append(packets, packet)
	}
	packetSetDigest, _, err := adjudicationJSONLDigest(packets)
	if err != nil {
		t.Fatal(err)
	}
	manifest := adjudicationManifest{
		Schema: adjudicationManifestSchema, ProtocolHash: protocolHash, PacketSetDigest: packetSetDigest,
		PromptDigest: adjudicationPromptDigest(), QuestionCount: len(packets), TriggeredCount: triggered,
	}
	var active, maximum, calls int32
	started := make(chan struct{}, 32)
	release := make(chan struct{})
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		current := atomic.AddInt32(&active, 1)
		atomic.AddInt32(&calls, 1)
		for {
			old := atomic.LoadInt32(&maximum)
			if current <= old || atomic.CompareAndSwapInt32(&maximum, old, current) {
				break
			}
		}
		if current <= 32 {
			started <- struct{}{}
		}
		<-release
		atomic.AddInt32(&active, -1)
		return `{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"high"}`,
			provider.Usage{InputTokens: 10, OutputTokens: 2}, nil
	}
	type runResult struct {
		seal adjudicationSeal
		err  error
	}
	resultCh := make(chan runResult, 1)
	dir := t.TempDir()
	go func() {
		seal, runErr := executeAdjudicationRun(context.Background(), dir, manifest, packets,
			adjudicationRunConfig{Provider: "stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "stub", ModelRevision: "v1", Concurrency: 32, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("test-binary")}, caller)
		resultCh <- runResult{seal: seal, err: runErr}
	}()
	for i := 0; i < 32; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("runner did not reach concurrency 32")
		}
	}
	if got := atomic.LoadInt32(&maximum); got != 32 {
		t.Fatalf("maximum in-flight = %d, want 32", got)
	}
	close(release)
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.seal.PlannedCalls != triggered || result.seal.ProviderAttempts != triggered ||
		result.seal.QuestionCount != len(packets) || atomic.LoadInt32(&calls) != triggered {
		t.Fatalf("seal/calls mismatch: %#v calls=%d", result.seal, calls)
	}
	if result.seal.InputTokens != triggered*10 || result.seal.OutputTokens != triggered*2 {
		t.Fatalf("usage totals = %d/%d", result.seal.InputTokens, result.seal.OutputTokens)
	}
	decisions, err := readAdjudicationDecisions(filepath.Join(dir, adjudicationDecisionsFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != len(packets) || decisions[len(decisions)-1].ProviderAttempts != 0 {
		t.Fatalf("sealed decisions = %d; non-trigger=%#v", len(decisions), decisions[len(decisions)-1])
	}
}

func TestExecuteAdjudicationFrozenOfflineStub(t *testing.T) {
	fixtureDir := os.Getenv("ADJUDICATION_OFFLINE_FIXTURE_DIR")
	outputRoot := os.Getenv("ADJUDICATION_OFFLINE_OUTPUT_DIR")
	if fixtureDir == "" || outputRoot == "" {
		t.Skip("set ADJUDICATION_OFFLINE_FIXTURE_DIR and ADJUDICATION_OFFLINE_OUTPUT_DIR for the frozen offline receipt")
	}
	manifest, packets, err := loadAndValidateAdjudicationPublic(fixtureDir, true)
	if err != nil {
		t.Fatal(err)
	}
	config := adjudicationRunConfig{
		Provider: "offline-stub", BaseURLDigest: adjudicationTextDigest("offline"), Model: "fixed-c1",
		ModelRevision: "v1", Concurrency: 32, MaxTokens: 64, BinaryDigest: adjudicationTextDigest("offline-stub-binary"),
		PricingStatus: "declared_zero",
	}
	zero := 0.0
	config.InputCNYPerMillion, config.OutputCNYPerMillion = &zero, &zero
	caller := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"selected_slot":"C1","evidence_ids":["E01"],"confidence":"high"}`,
			provider.Usage{InputTokens: 17, OutputTokens: 3}, nil
	}
	var seals []adjudicationSeal
	for run := 1; run <= 2; run++ {
		dir := filepath.Join(outputRoot, "run-"+string(rune('0'+run)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		seal, err := executeAdjudicationRun(context.Background(), dir, manifest, packets, config, caller)
		if err != nil {
			t.Fatal(err)
		}
		validated, decisions, err := validateAdjudicationSeal(dir, manifest, packets)
		if err != nil {
			t.Fatal(err)
		}
		if validated.DecisionSetDigest != seal.DecisionSetDigest || len(decisions) != len(packets) {
			t.Fatalf("seal validation mismatch: %#v decisions=%d", validated, len(decisions))
		}
		seals = append(seals, seal)
	}
	if seals[0].DecisionSetDigest != seals[1].DecisionSetDigest || seals[0].ProviderAttempts != adjudicationFrozenTriggerCount {
		t.Fatalf("offline seals differ or calls are wrong: %#v %#v", seals[0], seals[1])
	}
	leftDecisions, err := os.ReadFile(filepath.Join(outputRoot, "run-1", adjudicationDecisionsFile))
	if err != nil {
		t.Fatal(err)
	}
	rightDecisions, err := os.ReadFile(filepath.Join(outputRoot, "run-2", adjudicationDecisionsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(leftDecisions, rightDecisions) {
		t.Fatal("concurrent frozen decisions are not byte-identical")
	}
}
