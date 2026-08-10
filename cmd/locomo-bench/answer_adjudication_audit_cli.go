package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/wallfacers/engram/provider"
)

func loadAndValidateAdjudicationAuditParent(dir string, requireFrozen bool) (adjudicationAuditParentReceipt, []adjudicationPacket, []adjudicationDecision, error) {
	var receipt adjudicationAuditParentReceipt
	manifest, packets, err := loadAndValidateAdjudicationPublic(dir, requireFrozen)
	if err != nil {
		return receipt, nil, nil, err
	}
	seal, decisions, err := validateAdjudicationSeal(dir, manifest, packets)
	if err != nil {
		return receipt, nil, nil, err
	}
	manifestRaw, err := readCanonicalAdjudicationAuditParentJSON(filepath.Join(dir, adjudicationManifestFile), &adjudicationManifest{})
	if err != nil {
		return receipt, nil, nil, fmt.Errorf("validate parent manifest bytes: %w", err)
	}
	sealRaw, err := readCanonicalAdjudicationAuditParentJSON(filepath.Join(dir, adjudicationSealFile), &adjudicationSeal{})
	if err != nil {
		return receipt, nil, nil, fmt.Errorf("validate parent seal bytes: %w", err)
	}
	rawDigests := make(map[string]string, 3)
	for _, name := range []string{adjudicationPacketsFile, adjudicationCallsFile, adjudicationDecisionsFile} {
		digest, digestErr := fileSHA256(filepath.Join(dir, name))
		if digestErr != nil {
			return receipt, nil, nil, digestErr
		}
		rawDigests[name] = digest
	}
	selected, fallback := 0, 0
	for _, decision := range decisions {
		switch decision.State {
		case adjudicationDecisionSelected:
			selected++
		case adjudicationDecisionFallback:
			fallback++
		default:
			return receipt, nil, nil, fmt.Errorf("unknown parent decision state")
		}
	}
	receipt = adjudicationAuditParentReceipt{
		ProtocolHash: manifest.ProtocolHash, PacketSetDigest: manifest.PacketSetDigest,
		DecisionSetDigest: seal.DecisionSetDigest, PromptDigest: manifest.PromptDigest,
		ManifestRawDigest: adjudicationTextDigest(string(manifestRaw)), PacketsRawDigest: rawDigests[adjudicationPacketsFile],
		CallsRawDigest: rawDigests[adjudicationCallsFile], DecisionsRawDigest: rawDigests[adjudicationDecisionsFile],
		SealRawDigest: adjudicationTextDigest(string(sealRaw)), QuestionCount: manifest.QuestionCount,
		TriggeredCount: manifest.TriggeredCount, SelectedCount: selected, FallbackCount: fallback,
		ProviderAttempts: seal.ProviderAttempts, Retries: seal.Retries,
	}
	if requireFrozen {
		if err := validateFrozenAdjudicationAuditParentReceipt(receipt); err != nil {
			return receipt, nil, nil, err
		}
	}
	return receipt, packets, decisions, nil
}

func readCanonicalAdjudicationAuditParentJSON(path string, target any) ([]byte, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected parent artifact
	if err != nil {
		return nil, err
	}
	if err := decodeStrictAdjudicationJSON(raw, target); err != nil {
		return nil, err
	}
	canonical, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return nil, err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(raw, canonical) {
		return nil, fmt.Errorf("parent JSON bytes are not canonical")
	}
	return raw, nil
}

func runAdjudicationAuditBuildCLI(ctx context.Context, opt options) error {
	_ = ctx
	if err := validateAdjudicationAuditCLIOptions(opt, adjudicationAuditModeBuild); err != nil {
		return err
	}
	for _, name := range []string{
		adjudicationAuditManifestFile, adjudicationAuditPacketsFile, adjudicationAuditResolverMapFile,
		adjudicationAuditCallsFile, adjudicationAuditDecisionsFile, adjudicationAuditSealFile, adjudicationAuditScoreFile,
	} {
		if _, err := os.Stat(filepath.Join(opt.adjudicationAuditBuildDir, name)); err == nil {
			return fmt.Errorf("adjudication audit build refuses existing artifact")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	parent, packets, decisions, err := loadAndValidateAdjudicationAuditParent(opt.adjudicationSourceDir, true)
	if err != nil {
		return err
	}
	manifest, auditPackets, resolver, err := deriveAdjudicationAuditArtifacts(parent, packets, decisions, opt.adjudicationAuditSeed)
	if err != nil {
		return err
	}
	if err := writeAdjudicationAuditBuild(opt.adjudicationAuditBuildDir, manifest, auditPackets, resolver); err != nil {
		return err
	}
	_, _, _, err = loadAndValidateAdjudicationAuditBuild(opt.adjudicationAuditBuildDir, true)
	return err
}

func runAdjudicationAuditValidateCLI(opt options) error {
	if err := validateAdjudicationAuditCLIOptions(opt, adjudicationAuditModeValidate); err != nil {
		return err
	}
	_, _, _, err := loadAndValidateAdjudicationAuditBuild(opt.adjudicationAuditValidateDir, true)
	return err
}

func runAdjudicationAuditProviderCLI(ctx context.Context, opt options) error {
	if err := validateAdjudicationAuditCLIOptions(opt, adjudicationAuditModeRun); err != nil {
		return err
	}
	manifest, packets, resolver, err := loadAndValidateAdjudicationAuditBuild(opt.adjudicationAuditRunDir, true)
	if err != nil {
		return err
	}
	config, err := loadAdjudicationProviderConfig(os.Getenv, opt.concurrency, opt.adjudicationAuditMaxTokens)
	if err != nil {
		return err
	}
	if executable, executableErr := os.Executable(); executableErr == nil {
		config.BinaryDigest, _ = fileSHA256(executable)
	}
	prov, err := buildBenchProvider(config.Provider, config.apiKey, config.rawBaseURL, config.MaxTokens, "ADJUDICATOR_PROVIDER")
	if err != nil {
		return err
	}
	caller := newAdjudicationUsageCaller(prov, config.Model, config.MaxTokens)
	seal, err := executeAdjudicationAuditRun(ctx, opt.adjudicationAuditRunDir, manifest, packets, resolver, config, caller)
	if err != nil {
		return err
	}
	fmt.Printf("adjudication audit sealed: protocol=%s decisions=%d attempts=%d completed=%d failed=%d switched=%d input_tokens=%d output_tokens=%d pricing=%s\n",
		seal.ProtocolHash, seal.DecisionCount, seal.ProviderAttempts, seal.CompletedCalls, seal.FailedCalls,
		seal.SwitchedCount, seal.InputTokens, seal.OutputTokens, seal.PricingStatus)
	return nil
}

func executeAdjudicationAuditRun(ctx context.Context, dir string, manifest adjudicationAuditManifest, packets []adjudicationAuditPacket, resolver []adjudicationAuditResolverMapRecord, config adjudicationRunConfig, caller usageModelCaller) (adjudicationAuditSeal, error) {
	if config.Concurrency < 1 || config.MaxTokens < 1 || strings.TrimSpace(config.Provider) == "" ||
		strings.TrimSpace(config.Model) == "" || strings.TrimSpace(config.ModelRevision) == "" ||
		!isDigest(config.BaseURLDigest) || !isDigest(config.BinaryDigest) || len(packets) != manifest.RiskCount ||
		len(resolver) != manifest.QuestionCount || manifest.PlannedCalls < 1 || manifest.PlannedCalls != len(packets)*2 {
		return adjudicationAuditSeal{}, fmt.Errorf("invalid adjudication audit run configuration or inputs")
	}
	decisionPath := filepath.Join(dir, adjudicationAuditDecisionsFile)
	sealPath := filepath.Join(dir, adjudicationAuditSealFile)
	_, decisionErr := os.Stat(decisionPath)
	_, sealErr := os.Stat(sealPath)
	if decisionErr == nil || sealErr == nil {
		if decisionErr == nil && sealErr == nil {
			seal, _, err := validateAdjudicationAuditSeal(dir, manifest, packets, resolver, false)
			if err == nil && (seal.Provider != config.Provider || seal.BaseURLDigest != config.BaseURLDigest ||
				seal.Model != config.Model || seal.ModelRevision != config.ModelRevision || seal.MaxTokens != config.MaxTokens ||
				seal.BinaryDigest != config.BinaryDigest) {
				return adjudicationAuditSeal{}, fmt.Errorf("completed audit run identity drift")
			}
			return seal, err
		}
		return adjudicationAuditSeal{}, fmt.Errorf("partial adjudication audit decision/seal artifacts")
	}
	if !os.IsNotExist(decisionErr) {
		return adjudicationAuditSeal{}, decisionErr
	}
	if !os.IsNotExist(sealErr) {
		return adjudicationAuditSeal{}, sealErr
	}
	if _, err := os.Stat(filepath.Join(dir, adjudicationAuditScoreFile)); err == nil {
		return adjudicationAuditSeal{}, fmt.Errorf("adjudication audit run refuses existing score")
	} else if !os.IsNotExist(err) {
		return adjudicationAuditSeal{}, err
	}
	journal, err := openAdjudicationAuditCallJournal(filepath.Join(dir, adjudicationAuditCallsFile), manifest.ProtocolHash, packets)
	if err != nil {
		return adjudicationAuditSeal{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = journal.Close()
		}
	}()
	type workItem struct {
		packet adjudicationAuditPacket
		view   adjudicationAuditView
	}
	runIdentity := adjudicationAuditRunIdentityDigest(config, manifest)
	work := make([]workItem, 0, manifest.PlannedCalls)
	for _, packet := range packets {
		for _, view := range packet.Views {
			inputDigest := adjudicationAuditInputDigest(packet, view, runIdentity)
			if prior, ok := journal.TerminalRecord(packet.PacketID, view.ViewID); ok {
				if prior.InputDigest != inputDigest {
					return adjudicationAuditSeal{}, fmt.Errorf("audit resume call identity drift")
				}
				continue
			}
			work = append(work, workItem{packet: packet, view: view})
		}
	}
	jobs := make(chan workItem)
	errorsCh := make(chan error, config.Concurrency)
	workerCount := config.Concurrency
	if workerCount > len(work) {
		workerCount = len(work)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				if err := executeOneAdjudicationAuditCall(ctx, item.packet, item.view, runIdentity, caller, journal); err != nil {
					errorsCh <- err
				}
			}
		}()
	}
	go func() {
		for _, item := range work {
			jobs <- item
		}
		close(jobs)
		workers.Wait()
		close(errorsCh)
	}()
	var firstErr error
	for runErr := range errorsCh {
		if firstErr == nil {
			firstErr = runErr
		}
	}
	if firstErr != nil {
		return adjudicationAuditSeal{}, firstErr
	}
	if err := journal.Close(); err != nil {
		return adjudicationAuditSeal{}, err
	}
	closed = true
	terminals := journal.SortedTerminals()
	if len(terminals) != manifest.PlannedCalls {
		return adjudicationAuditSeal{}, fmt.Errorf("audit run lacks complete terminal coverage")
	}
	packetByID := make(map[string]adjudicationAuditPacket, len(packets))
	terminalsByPacket := make(map[string][]adjudicationAuditCallRecord, len(packets))
	for _, packet := range packets {
		packetByID[packet.PacketID] = packet
	}
	for _, terminal := range terminals {
		terminalsByPacket[terminal.PacketID] = append(terminalsByPacket[terminal.PacketID], terminal)
	}
	decisions := make([]adjudicationAuditDecision, 0, len(resolver))
	for _, record := range resolver {
		if !record.Risk {
			decisions = append(decisions, retainedNonriskAdjudicationAuditDecision(record))
			continue
		}
		decision, err := resolveAdjudicationAuditDecision(record, packetByID[record.PacketID], terminalsByPacket[record.PacketID])
		if err != nil {
			return adjudicationAuditSeal{}, err
		}
		decisions = append(decisions, decision)
	}
	sort.Slice(decisions, func(i, j int) bool {
		return adjudicationIdentityLess(decisions[i].Conv, decisions[i].Q, decisions[j].Conv, decisions[j].Q)
	})
	decisionSetDigest, decisionRaw, err := adjudicationJSONLDigest(decisions)
	if err != nil {
		return adjudicationAuditSeal{}, err
	}
	callStateDigest, _, err := adjudicationJSONLDigest(terminals)
	if err != nil {
		return adjudicationAuditSeal{}, err
	}
	started, completed, failed := journal.Stats()
	resolutionCounts := make(map[string]int)
	retained, switched, attempts, inputTokens, outputTokens := 0, 0, 0, 0, 0
	for _, decision := range decisions {
		resolutionCounts[decision.Resolution]++
		attempts += decision.ProviderAttempts
		inputTokens += decision.InputTokens
		outputTokens += decision.OutputTokens
		if decision.Resolution == adjudicationAuditResolutionSwitched {
			switched++
		} else {
			retained++
		}
	}
	failureCounts := make(map[string]int)
	for _, terminal := range terminals {
		if terminal.State == adjudicationAuditCallFailed {
			failureCounts[terminal.FailureReason]++
		}
	}
	seal := adjudicationAuditSeal{
		Schema: adjudicationAuditSealSchema, ProtocolHash: manifest.ProtocolHash, Parent: manifest.Parent,
		AuditPacketSetDigest: manifest.AuditPacketSetDigest, ResolverMapSetDigest: manifest.ResolverMapSetDigest,
		CanonicalCallStateDigest: callStateDigest, DecisionSetDigest: decisionSetDigest,
		EntailmentPromptDigest: manifest.EntailmentPromptDigest, FalsificationPromptDigest: manifest.FalsificationPromptDigest,
		ResolverDigest: manifest.ResolverDigest, Provider: config.Provider, BaseURLDigest: config.BaseURLDigest,
		Model: config.Model, ModelRevision: config.ModelRevision, MaxTokens: config.MaxTokens, BinaryDigest: config.BinaryDigest,
		QuestionCount: manifest.QuestionCount, RiskCount: manifest.RiskCount, ViewCount: manifest.ViewCount,
		PlannedCalls: manifest.PlannedCalls, StartedCalls: started, TerminalCalls: completed + failed,
		CompletedCalls: completed, FailedCalls: failed, ProviderAttempts: attempts, Retries: 0,
		DecisionCount: len(decisions), RetainedCount: retained, SwitchedCount: switched,
		ResolutionCounts: resolutionCounts, FailureCounts: failureCounts, InputTokens: inputTokens, OutputTokens: outputTokens,
		PricingStatus: config.PricingStatus, InputCNYPerMillion: config.InputCNYPerMillion,
		OutputCNYPerMillion: config.OutputCNYPerMillion, Valid: true,
	}
	if seal.PricingStatus == "" {
		seal.PricingStatus = "unpriced"
	}
	if config.InputCNYPerMillion != nil && config.OutputCNYPerMillion != nil {
		estimated := (float64(inputTokens)*(*config.InputCNYPerMillion) + float64(outputTokens)*(*config.OutputCNYPerMillion)) / 1_000_000
		seal.EstimatedCNY = &estimated
	}
	if started != manifest.PlannedCalls || attempts != manifest.PlannedCalls || completed+failed != manifest.PlannedCalls {
		return adjudicationAuditSeal{}, fmt.Errorf("adjudication audit seal counts are inconsistent")
	}
	seal.SealDigest = adjudicationAuditSealDigest(seal)
	if err := writeAdjudicationAtomic(decisionPath, decisionRaw); err != nil {
		return adjudicationAuditSeal{}, err
	}
	if err := writeJSON(sealPath, seal); err != nil {
		return adjudicationAuditSeal{}, err
	}
	validated, _, err := validateAdjudicationAuditSeal(dir, manifest, packets, resolver, false)
	if err != nil {
		return adjudicationAuditSeal{}, err
	}
	return validated, nil
}

func executeOneAdjudicationAuditCall(ctx context.Context, packet adjudicationAuditPacket, view adjudicationAuditView, runIdentity string, caller usageModelCaller, journal *adjudicationAuditCallJournal) error {
	prompt, err := buildAdjudicationAuditPrompt(packet, view.ViewID)
	if err != nil {
		return err
	}
	inputDigest := adjudicationAuditInputDigest(packet, view, runIdentity)
	if err := journal.Start(packet, view, inputDigest); err != nil {
		return err
	}
	usage := provider.Usage{}
	failureReason := ""
	var raw string
	if caller == nil {
		failureReason = adjudicationAuditFailureProvider
	} else {
		system := adjudicationAuditEntailmentSystemPrompt
		if view.ViewID == adjudicationAuditViewFalsification {
			system = adjudicationAuditFalsificationSystemPrompt
		}
		raw, usage, err = caller(ctx, system, prompt)
		if err != nil {
			failureReason = adjudicationAuditFailureProvider
		}
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 {
		usage = provider.Usage{}
		failureReason = adjudicationAuditFailureUsage
	}
	var assessments []adjudicationAuditCandidateAssessment
	if failureReason == "" {
		response, parseErr := parseAdjudicationAuditResponse(raw, packet, view.ViewID)
		if parseErr != nil {
			failureReason = adjudicationAuditFailureResponse
		} else {
			assessments = response.Assessments
		}
	}
	return journal.Terminal(packet, view, inputDigest, assessments, failureReason, usage)
}

func scoreSealedAdjudicationAudit(parentDir, auditDir string, requireFrozen bool, hiddenLoader func() (adjudicationHiddenInputs, error)) (adjudicationAuditStage0Score, error) {
	parentReceipt, parentPackets, parentDecisions, err := loadAndValidateAdjudicationAuditParent(parentDir, requireFrozen)
	if err != nil {
		return adjudicationAuditStage0Score{}, err
	}
	parentManifest, _, err := loadAndValidateAdjudicationPublic(parentDir, requireFrozen)
	if err != nil {
		return adjudicationAuditStage0Score{}, err
	}
	auditManifest, auditPackets, resolver, err := loadAndValidateAdjudicationAuditBuild(auditDir, requireFrozen)
	if err != nil {
		return adjudicationAuditStage0Score{}, err
	}
	if !reflect.DeepEqual(parentReceipt, auditManifest.Parent) {
		return adjudicationAuditStage0Score{}, fmt.Errorf("audit parent receipt drift")
	}
	seal, auditDecisions, err := validateAdjudicationAuditSeal(auditDir, auditManifest, auditPackets, resolver, requireFrozen)
	if err != nil {
		return adjudicationAuditStage0Score{}, err
	}
	if hiddenLoader == nil {
		return adjudicationAuditStage0Score{}, fmt.Errorf("audit hidden score loader is required")
	}
	hidden, err := hiddenLoader()
	if err != nil {
		return adjudicationAuditStage0Score{}, err
	}
	return scoreAdjudicationAuditDecisions(parentManifest, parentPackets, parentDecisions, auditManifest, auditDecisions, seal, hidden, requireFrozen)
}

func runAdjudicationAuditScoreCLI(opt options) error {
	if err := validateAdjudicationAuditCLIOptions(opt, adjudicationAuditModeScore); err != nil {
		return err
	}
	scorePath := filepath.Join(opt.adjudicationAuditScoreDir, adjudicationAuditScoreFile)
	if _, err := os.Stat(scorePath); err == nil {
		return fmt.Errorf("adjudication audit score refuses existing artifact")
	} else if !os.IsNotExist(err) {
		return err
	}
	report, err := scoreSealedAdjudicationAudit(opt.adjudicationSourceDir, opt.adjudicationAuditScoreDir, true, func() (adjudicationHiddenInputs, error) {
		return loadAdjudicationHiddenInputs(opt.adjudicationSourceDir, opt.adjudicationCandidates)
	})
	if err != nil {
		return err
	}
	if err := writeJSON(scorePath, report); err != nil {
		return err
	}
	fmt.Printf("adjudication audit stage0: verdict=%s new=%d/%d lower=%d mixed=%d/%d mixed_lower=%d new_only=%d parent_only=%d p=%.6f temporal_net=%d result_kind=%s\n",
		report.Verdict, report.New.Correct, report.New.Total, report.JudgeInstability.NewLower,
		report.TriggeredMixedNew.Correct, report.TriggeredMixedNew.Total,
		report.JudgeInstability.TriggeredMixedLower, report.Paired.NewOnly, report.Paired.ParentOnly,
		report.Paired.McNemarP, report.TemporalNet, report.ResultKind)
	return nil
}

func validateAdjudicationAuditCLIOptions(opt options, mode adjudicationMode) error {
	selected, err := adjudicationModeFor(opt)
	if err != nil {
		return err
	}
	if selected != mode {
		return fmt.Errorf("adjudication audit mode mismatch")
	}
	if opt.adjudicationTracePath != "" || opt.adjudicationSeed != "" || opt.adjudicationAllowPaid {
		return fmt.Errorf("adjudication audit mode rejects 034-only options")
	}
	switch mode {
	case adjudicationAuditModeBuild:
		if strings.TrimSpace(opt.adjudicationAuditBuildDir) == "" || strings.TrimSpace(opt.adjudicationSourceDir) == "" ||
			strings.TrimSpace(opt.adjudicationAuditSeed) == "" || len(opt.adjudicationCandidates) != 0 ||
			opt.adjudicationAuditAllowPaid {
			return fmt.Errorf("adjudication audit build requires directory, parent source, and seed")
		}
	case adjudicationAuditModeValidate:
		if strings.TrimSpace(opt.adjudicationAuditValidateDir) == "" || strings.TrimSpace(opt.adjudicationSourceDir) != "" ||
			strings.TrimSpace(opt.adjudicationAuditSeed) != "" || len(opt.adjudicationCandidates) != 0 ||
			opt.adjudicationAuditAllowPaid {
			return fmt.Errorf("adjudication audit validate accepts only its directory")
		}
	case adjudicationAuditModeRun:
		if strings.TrimSpace(opt.adjudicationAuditRunDir) == "" || strings.TrimSpace(opt.adjudicationSourceDir) != "" ||
			strings.TrimSpace(opt.adjudicationAuditSeed) != "" || len(opt.adjudicationCandidates) != 0 ||
			!opt.adjudicationAuditAllowPaid || opt.adjudicationAuditMaxTokens < 1 || opt.concurrency < 1 {
			return fmt.Errorf("adjudication audit run requires directory, paid acknowledgement, concurrency, and positive output cap")
		}
	case adjudicationAuditModeScore:
		if strings.TrimSpace(opt.adjudicationAuditScoreDir) == "" || strings.TrimSpace(opt.adjudicationSourceDir) == "" ||
			strings.TrimSpace(opt.adjudicationAuditSeed) != "" || len(opt.adjudicationCandidates) != 3 ||
			opt.adjudicationAuditAllowPaid {
			return fmt.Errorf("adjudication audit score requires directory, parent source, and exactly three candidates")
		}
	default:
		return fmt.Errorf("unknown adjudication audit mode")
	}
	return nil
}
