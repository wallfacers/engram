package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
)

type adjudicationMode string

const (
	adjudicationModeBuild    adjudicationMode = "build"
	adjudicationModeValidate adjudicationMode = "validate"
	adjudicationModeRun      adjudicationMode = "run"
	adjudicationModeScore    adjudicationMode = "score"

	adjudicationAuditModeBuild    adjudicationMode = "audit-build"
	adjudicationAuditModeValidate adjudicationMode = "audit-validate"
	adjudicationAuditModeRun      adjudicationMode = "audit-run"
	adjudicationAuditModeScore    adjudicationMode = "audit-score"
	adjudicationModeAttribution   adjudicationMode = "attribution"
)

type adjudicationCandidatePaths []string

func (paths *adjudicationCandidatePaths) String() string {
	return strings.Join(*paths, ",")
}

func (paths *adjudicationCandidatePaths) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("adjudication candidate path must not be empty")
	}
	*paths = append(*paths, value)
	return nil
}

func adjudicationModeFor(opt options) (adjudicationMode, error) {
	configured := []struct {
		mode adjudicationMode
		dir  string
	}{
		{adjudicationModeBuild, opt.adjudicationBuildDir},
		{adjudicationModeValidate, opt.adjudicationValidateDir},
		{adjudicationModeRun, opt.adjudicationRunDir},
		{adjudicationModeScore, opt.adjudicationScoreDir},
		{adjudicationAuditModeBuild, opt.adjudicationAuditBuildDir},
		{adjudicationAuditModeValidate, opt.adjudicationAuditValidateDir},
		{adjudicationAuditModeRun, opt.adjudicationAuditRunDir},
		{adjudicationAuditModeScore, opt.adjudicationAuditScoreDir},
		{adjudicationModeAttribution, opt.adjudicationAttributionDir},
	}
	var selected adjudicationMode
	for _, item := range configured {
		if strings.TrimSpace(item.dir) == "" {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("adjudication modes are mutually exclusive")
		}
		selected = item.mode
	}
	auditAuxiliary := strings.TrimSpace(opt.adjudicationSourceDir) != "" || strings.TrimSpace(opt.adjudicationAuditSeed) != "" ||
		opt.adjudicationAuditAllowPaid || (opt.adjudicationAuditMaxTokens != 0 && opt.adjudicationAuditMaxTokens != 768)
	if auditAuxiliary && (selected == "" || selected == adjudicationModeBuild || selected == adjudicationModeValidate ||
		selected == adjudicationModeRun || selected == adjudicationModeScore) {
		return "", fmt.Errorf("035 adjudication audit options require a matching audit mode")
	}
	// 036 attribution has its own optional audit-dir flag and must not be
	// confused with 035's audit options (which stay 035-mode-only).
	if selected == adjudicationModeAttribution && strings.TrimSpace(opt.adjudicationAuditDir) != "" &&
		strings.TrimSpace(opt.adjudicationAuditSeed) != "" {
		return "", fmt.Errorf("036 attribution audit uses --adjudication-audit-source only")
	}
	return selected, nil
}

func runAdjudicationCLI(ctx context.Context, opt options) error {
	mode, err := adjudicationModeFor(opt)
	if err != nil {
		return err
	}
	switch mode {
	case adjudicationModeBuild:
		return runAdjudicationBuildCLI(ctx, opt)
	case adjudicationModeValidate:
		return runAdjudicationValidateCLI(opt)
	case adjudicationModeRun:
		return runAdjudicationProviderCLI(ctx, opt)
	case adjudicationModeScore:
		return runAdjudicationScoreCLI(opt)
	case adjudicationAuditModeBuild:
		return runAdjudicationAuditBuildCLI(ctx, opt)
	case adjudicationAuditModeValidate:
		return runAdjudicationAuditValidateCLI(opt)
	case adjudicationAuditModeRun:
		return runAdjudicationAuditProviderCLI(ctx, opt)
	case adjudicationAuditModeScore:
		return runAdjudicationAuditScoreCLI(opt)
	case adjudicationModeAttribution:
		return runDecisionGapAttributionCLI(ctx, opt)
	default:
		return fmt.Errorf("adjudication mode is required")
	}
}

func runAdjudicationBuildCLI(ctx context.Context, opt options) error {
	if err := validateAdjudicationCLIOptions(opt, adjudicationModeBuild); err != nil {
		return err
	}
	for _, name := range []string{
		adjudicationManifestFile, adjudicationPacketsFile, adjudicationSlotMapFile,
		adjudicationCustodyFile, adjudicationCallsFile, adjudicationDecisionsFile,
		adjudicationSealFile, adjudicationScoreFile,
	} {
		if _, err := os.Stat(filepath.Join(opt.adjudicationBuildDir, name)); err == nil {
			return fmt.Errorf("adjudication build refuses existing artifact %s", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	sources := make([]adjudicationCandidateSource, 0, 3)
	for _, path := range opt.adjudicationCandidates {
		source, err := loadAdjudicationCandidateSource(path)
		if err != nil {
			return err
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].SanitizedDigest < sources[j].SanitizedDigest })
	for i := 1; i < len(sources); i++ {
		if sources[i-1].SanitizedDigest == sources[i].SanitizedDigest {
			return fmt.Errorf("candidate sources require distinct sanitized digests")
		}
	}
	if len(sources[0].Records) != adjudicationFrozenQuestionCount {
		return fmt.Errorf("candidate question count = %d, want %d", len(sources[0].Records), adjudicationFrozenQuestionCount)
	}
	for i := 1; i < len(sources); i++ {
		if len(sources[i].Records) != len(sources[0].Records) {
			return fmt.Errorf("candidate source question counts differ")
		}
	}
	trace, err := loadAdjudicationTraceSource(opt.adjudicationTracePath)
	if err != nil {
		return err
	}
	if len(trace.Records) != adjudicationFrozenQuestionCount {
		return fmt.Errorf("trace question count = %d, want %d", len(trace.Records), adjudicationFrozenQuestionCount)
	}
	traceByQuestion := make(map[string]adjudicationTraceInput, len(trace.Records))
	namesByConv := make(map[int][]string)
	for _, record := range trace.Records {
		qid := questionID(record.Conv, record.Q)
		traceByQuestion[qid] = record
		for _, hit := range record.Retrieved {
			namesByConv[record.Conv] = append(namesByConv[record.Conv], hit.Name)
		}
	}
	convIDs := make([]int, 0, len(namesByConv))
	for conv := range namesByConv {
		convIDs = append(convIDs, conv)
	}
	sort.Ints(convIDs)
	catalogs := make(map[int]adjudicationEvidenceCatalog, len(convIDs))
	storeReceipts := make([]adjudicationStoreReceipt, 0, len(convIDs))
	type semanticStoreReceipt struct {
		Conv   int    `json:"conv"`
		Digest string `json:"digest"`
	}
	semanticReceipts := make([]semanticStoreReceipt, 0, len(convIDs))
	for _, conv := range convIDs {
		dbPath := filepath.Join(opt.storeDir, fmt.Sprintf("conv%d.db", conv))
		catalog, err := loadAdjudicationEvidenceCatalog(ctx, dbPath, namesByConv[conv])
		if err != nil {
			return fmt.Errorf("conversation %d evidence: %w", conv, err)
		}
		catalogs[conv] = catalog
		storeReceipts = append(storeReceipts, adjudicationStoreReceipt{
			Conv: conv, RawDigest: catalog.RawDigest, Size: catalog.RawSize, SemanticDigest: catalog.SemanticDigest,
		})
		semanticReceipts = append(semanticReceipts, semanticStoreReceipt{Conv: conv, Digest: catalog.SemanticDigest})
	}
	packets := make([]adjudicationPacket, 0, adjudicationFrozenQuestionCount)
	slotMaps := make([]adjudicationSlotMapRecord, 0, adjudicationFrozenQuestionCount)
	questionIDs := make([]string, 0, adjudicationFrozenQuestionCount)
	triggeredCount, parityCount, triggeredParityCount := 0, 0, 0
	for index := 0; index < len(sources[0].Records); index++ {
		base := sources[0].Records[index]
		candidates := make([]adjudicationCandidate, len(sources))
		parity := true
		for sourceIndex, source := range sources {
			record := source.Records[index]
			if record.Conv != base.Conv || record.Q != base.Q || record.QuestionID != base.QuestionID ||
				record.Category != base.Category || record.Question != base.Question ||
				record.AnswerRegime != base.AnswerRegime || record.RetrievalFlags != base.RetrievalFlags {
				return fmt.Errorf("candidate metadata drift for %q", base.QuestionID)
			}
			if record.InputTokens != base.InputTokens || record.AnswerContextTokens != base.AnswerContextTokens {
				parity = false
			}
			candidates[sourceIndex] = adjudicationCandidate{
				Answer: record.Predicted, Normalized: normalizeAdjudicationAnswer(record.Predicted),
				AnswerDigest: adjudicationTextDigest(record.Predicted), SourceDigest: source.SanitizedDigest,
			}
		}
		traceRecord, ok := traceByQuestion[base.QuestionID]
		if !ok || traceRecord.Conv != base.Conv || traceRecord.Q != base.Q || traceRecord.Category != base.Category {
			return fmt.Errorf("trace mismatch for %q", base.QuestionID)
		}
		catalog := catalogs[base.Conv]
		evidence := make([]adjudicationEvidenceItem, len(traceRecord.Retrieved))
		for hitIndex, hit := range traceRecord.Retrieved {
			content := catalog.Rendered[hit.Name]
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("missing rendered evidence %q", hit.Name)
			}
			evidence[hitIndex] = adjudicationEvidenceItem{
				EvidenceID: fmtEvidenceID(hitIndex + 1), Rank: hitIndex + 1, Content: content,
				ContentDigest: adjudicationTextDigest(content),
			}
		}
		ordered := adjudicationBlindedSourceOrder(opt.adjudicationSeed, base.QuestionID, candidates)
		packetCandidates := make([]adjudicationPacketCandidate, len(ordered))
		slotSources := make([]adjudicationSlotSource, len(ordered))
		for candidateIndex, candidate := range ordered {
			slot := fmt.Sprintf("C%d", candidateIndex+1)
			packetCandidates[candidateIndex] = adjudicationPacketCandidate{
				Slot: slot, Answer: candidate.Answer, AnswerDigest: candidate.AnswerDigest,
			}
			slotSources[candidateIndex] = adjudicationSlotSource{
				Slot: slot, SourceDigest: candidate.SourceDigest, AnswerDigest: candidate.AnswerDigest,
				NormalizedAnswerDigest: adjudicationTextDigest(candidate.Normalized),
			}
		}
		isTriggered := candidates[0].Normalized != candidates[1].Normalized || candidates[0].Normalized != candidates[2].Normalized
		if isTriggered {
			triggeredCount++
		}
		if parity {
			parityCount++
			if isTriggered {
				triggeredParityCount++
			}
		}
		packetID := "packet:" + strings.TrimPrefix(adjudicationTextDigest("034\x00"+base.QuestionID), "sha256:")[:24]
		packets = append(packets, adjudicationPacket{
			Schema: adjudicationPacketSchema, PacketID: packetID, Conv: base.Conv, Q: base.Q,
			QuestionID: base.QuestionID, Category: base.Category, Question: base.Question,
			Triggered: isTriggered, ContextParity: parity, Evidence: evidence, Candidates: packetCandidates,
		})
		slotMaps = append(slotMaps, adjudicationSlotMapRecord{
			PacketID: packetID, Conv: base.Conv, Q: base.Q, QuestionID: base.QuestionID, Slots: slotSources,
		})
		questionIDs = append(questionIDs, base.QuestionID)
	}
	manifest := adjudicationManifest{
		Schema: adjudicationManifestSchema, ProtocolID: "034-stage0-legacy-v1",
		Normalizer: "ascii_lower_alnum_v1", PermutationSeedDigest: adjudicationTextDigest(opt.adjudicationSeed),
		SanitizedTraceDigest: trace.SanitizedDigest, StoreSemanticDigest: evalJSONDigest(semanticReceipts),
		QuestionIDsDigest: evalJSONDigest(questionIDs), PromptDigest: adjudicationPromptDigest(),
		QuestionCount: len(packets), TriggeredCount: triggeredCount, ContextParityCount: parityCount,
		TriggeredContextParityCount: triggeredParityCount,
	}
	for _, source := range sources {
		manifest.SanitizedCandidateSourceDigests = append(manifest.SanitizedCandidateSourceDigests, source.SanitizedDigest)
	}
	manifest.ProtocolHash = adjudicationManifestProtocolHash(manifest)
	for i := range packets {
		packets[i].ProtocolHash = manifest.ProtocolHash
		packets[i].PacketDigest = adjudicationPacketDigest(packets[i])
	}
	packetSetDigest, packetBytes, err := adjudicationJSONLDigest(packets)
	if err != nil {
		return err
	}
	manifest.PacketSetDigest = packetSetDigest
	slotMapDigest, slotMapBytes, err := adjudicationJSONLDigest(slotMaps)
	if err != nil {
		return err
	}
	custody := adjudicationCustody{
		Schema: adjudicationCustodySchema, ProtocolHash: manifest.ProtocolHash,
		TraceSource: adjudicationRawReceipt{Digest: trace.RawDigest, Size: trace.RawSize, Count: len(trace.Records)},
		Stores:      storeReceipts, StoreInventoryDigest: evalJSONDigest(storeReceipts),
		QuestionIDsDigest: manifest.QuestionIDsDigest, SlotMapDigest: slotMapDigest,
		CandidateModelClaim: "deepseek-v4-pro", CandidateProvenanceStatus: "legacy_operator_claim",
	}
	for _, source := range sources {
		custody.CandidateSources = append(custody.CandidateSources, adjudicationRawReceipt{
			Digest: source.RawDigest, Size: source.RawSize, Count: len(source.Records),
		})
	}
	custody.GitCommit, custody.GitDirty = adjudicationGitReceipt()
	if executable, err := os.Executable(); err == nil {
		custody.BuildBinaryDigest, _ = fileSHA256(executable)
	}
	if err := os.MkdirAll(opt.adjudicationBuildDir, 0o755); err != nil {
		return err
	}
	if err := writeAdjudicationAtomic(filepath.Join(opt.adjudicationBuildDir, adjudicationPacketsFile), packetBytes); err != nil {
		return err
	}
	if err := writeAdjudicationAtomic(filepath.Join(opt.adjudicationBuildDir, adjudicationSlotMapFile), slotMapBytes); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(opt.adjudicationBuildDir, adjudicationCustodyFile), custody); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(opt.adjudicationBuildDir, adjudicationManifestFile), manifest); err != nil {
		return err
	}
	if _, _, err := loadAndValidateAdjudicationPublic(opt.adjudicationBuildDir, true); err != nil {
		return fmt.Errorf("validate built adjudication artifacts: %w", err)
	}
	fmt.Printf("adjudication built: protocol=%s questions=%d triggered=%d context_parity=%d triggered_context_parity=%d calls=0\n",
		manifest.ProtocolHash, manifest.QuestionCount, manifest.TriggeredCount,
		manifest.ContextParityCount, manifest.TriggeredContextParityCount)
	return nil
}

func runAdjudicationValidateCLI(opt options) error {
	if err := validateAdjudicationCLIOptions(opt, adjudicationModeValidate); err != nil {
		return err
	}
	manifest, _, err := loadAndValidateAdjudicationPublic(opt.adjudicationValidateDir, true)
	if err != nil {
		return err
	}
	fmt.Printf("adjudication valid: protocol=%s questions=%d triggered=%d context_parity=%d triggered_context_parity=%d\n",
		manifest.ProtocolHash, manifest.QuestionCount, manifest.TriggeredCount,
		manifest.ContextParityCount, manifest.TriggeredContextParityCount)
	return nil
}

type adjudicationRunConfig struct {
	Provider            string   `json:"provider"`
	BaseURLDigest       string   `json:"base_url_digest"`
	Model               string   `json:"model"`
	ModelRevision       string   `json:"model_revision"`
	BinaryDigest        string   `json:"binary_digest,omitempty"`
	Concurrency         int      `json:"concurrency"`
	MaxTokens           int      `json:"max_tokens"`
	PricingStatus       string   `json:"pricing_status"`
	InputCNYPerMillion  *float64 `json:"input_cny_per_million,omitempty"`
	OutputCNYPerMillion *float64 `json:"output_cny_per_million,omitempty"`
	rawBaseURL          string
	apiKey              string
}

func loadAdjudicationProviderConfig(getenv func(string) string, concurrency, maxTokens int) (adjudicationRunConfig, error) {
	config := adjudicationRunConfig{
		Provider:      strings.ToLower(strings.TrimSpace(getenv("ADJUDICATOR_PROVIDER"))),
		Model:         strings.TrimSpace(getenv("ADJUDICATOR_MODEL")),
		ModelRevision: strings.TrimSpace(getenv("ADJUDICATOR_MODEL_REVISION")),
		rawBaseURL:    strings.TrimSpace(getenv("ADJUDICATOR_BASE_URL")),
		apiKey:        strings.TrimSpace(getenv("ADJUDICATOR_API_KEY")), Concurrency: concurrency, MaxTokens: maxTokens,
		PricingStatus: "unpriced",
	}
	if config.Provider == "" || config.Model == "" || config.ModelRevision == "" || config.rawBaseURL == "" ||
		config.apiKey == "" || concurrency < 1 || maxTokens < 1 {
		return adjudicationRunConfig{}, fmt.Errorf("complete ADJUDICATOR_PROVIDER/BASE_URL/MODEL/MODEL_REVISION/API_KEY and positive concurrency/output cap are required")
	}
	if config.Provider != "openai" && config.Provider != "anthropic" {
		return adjudicationRunConfig{}, fmt.Errorf("ADJUDICATOR_PROVIDER must be anthropic or openai")
	}
	parsed, err := url.Parse(config.rawBaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return adjudicationRunConfig{}, fmt.Errorf("ADJUDICATOR_BASE_URL must be an http(s) URL without userinfo, query, or fragment")
	}
	config.BaseURLDigest = adjudicationTextDigest(config.rawBaseURL)
	inputRaw := strings.TrimSpace(getenv("ADJUDICATOR_INPUT_CNY_PER_MILLION"))
	outputRaw := strings.TrimSpace(getenv("ADJUDICATOR_OUTPUT_CNY_PER_MILLION"))
	if (inputRaw == "") != (outputRaw == "") {
		return adjudicationRunConfig{}, fmt.Errorf("both adjudicator pricing variables must be supplied together")
	}
	if inputRaw != "" {
		inputPrice, err := strconv.ParseFloat(inputRaw, 64)
		if err != nil || inputPrice < 0 || math.IsNaN(inputPrice) || math.IsInf(inputPrice, 0) {
			return adjudicationRunConfig{}, fmt.Errorf("invalid ADJUDICATOR_INPUT_CNY_PER_MILLION")
		}
		outputPrice, err := strconv.ParseFloat(outputRaw, 64)
		if err != nil || outputPrice < 0 || math.IsNaN(outputPrice) || math.IsInf(outputPrice, 0) {
			return adjudicationRunConfig{}, fmt.Errorf("invalid ADJUDICATOR_OUTPUT_CNY_PER_MILLION")
		}
		config.InputCNYPerMillion, config.OutputCNYPerMillion = &inputPrice, &outputPrice
		config.PricingStatus = "priced"
		if inputPrice == 0 && outputPrice == 0 {
			config.PricingStatus = "declared_zero"
		}
	}
	return config, nil
}

func runAdjudicationProviderCLI(ctx context.Context, opt options) error {
	if err := validateAdjudicationCLIOptions(opt, adjudicationModeRun); err != nil {
		return err
	}
	// The temporal adjudication contract is a run-time prompt variant scoped
	// to category-2 packets; it changes the per-decision input identity but not
	// the frozen manifest. Set the package switch from the flag for this run.
	adjudicationTemporalPromptEnabled = opt.adjudicationTemporalPrompt
	if adjudicationTemporalPromptEnabled {
		fmt.Printf("adjudication: temporal reasoning contract enabled for category-2 (prompt digest %s)\n",
			adjudicationTemporalPromptDigest())
	}
	manifest, packets, err := loadAndValidateAdjudicationPublic(opt.adjudicationRunDir, true)
	if err != nil {
		return err
	}
	config, err := loadAdjudicationProviderConfig(os.Getenv, opt.concurrency, opt.adjudicationMaxTokens)
	if err != nil {
		return err
	}
	if executable, err := os.Executable(); err == nil {
		config.BinaryDigest, _ = fileSHA256(executable)
	}
	prov, err := buildBenchProvider(config.Provider, config.apiKey, config.rawBaseURL, config.MaxTokens, "ADJUDICATOR_PROVIDER")
	if err != nil {
		return err
	}
	baseCaller := newAdjudicationUsageCaller(prov, config.Model, config.MaxTokens)
	caller := gateUsageOnce(make(chan struct{}, config.Concurrency), baseCaller)
	seal, err := executeAdjudicationRun(ctx, opt.adjudicationRunDir, manifest, packets, config, caller)
	if err != nil {
		return err
	}
	fmt.Printf("adjudication sealed: protocol=%s decisions=%d attempts=%d selected=%d fallback=%d input_tokens=%d output_tokens=%d pricing=%s\n",
		seal.ProtocolHash, seal.DecisionCount, seal.ProviderAttempts, seal.CompletedCalls, seal.FailedCalls,
		seal.InputTokens, seal.OutputTokens, seal.PricingStatus)
	return nil
}

func runAdjudicationScoreCLI(opt options) error {
	if err := validateAdjudicationCLIOptions(opt, adjudicationModeScore); err != nil {
		return err
	}
	scorePath := filepath.Join(opt.adjudicationScoreDir, adjudicationScoreFile)
	if _, err := os.Stat(scorePath); err == nil {
		return fmt.Errorf("adjudication score refuses existing %s", adjudicationScoreFile)
	} else if !os.IsNotExist(err) {
		return err
	}
	report, err := scoreSealedAdjudication(opt.adjudicationScoreDir, true, func() (adjudicationHiddenInputs, error) {
		return loadAdjudicationHiddenInputs(opt.adjudicationScoreDir, opt.adjudicationCandidates)
	})
	if err != nil {
		return err
	}
	if err := writeJSON(scorePath, report); err != nil {
		return err
	}
	fmt.Printf("adjudication stage0: verdict=%s selected=%d/%d mixed=%d/%d instability=%d/%d result_kind=%s\n",
		report.Verdict, report.Selected.Correct, report.Selected.Total, report.TriggeredMixed.Correct,
		report.TriggeredMixed.Total, report.JudgeInstability.Total, report.JudgeInstability.Triggered,
		report.ResultKind)
	return nil
}

func newAdjudicationUsageCaller(prov provider.Provider, model string, maxTokens int) usageModelCaller {
	return func(ctx context.Context, system, user string) (string, provider.Usage, error) {
		request := provider.Request{
			Model: model, System: system, MaxTokens: maxTokens, Temperature: 0, ThinkingDisabled: true,
			Messages: []provider.Message{{
				Role: provider.RoleUser, Content: []provider.ContentBlock{{Type: provider.BlockText, Text: user}},
			}},
		}
		stream, err := prov.Stream(ctx, request)
		if err != nil {
			return "", provider.Usage{}, err
		}
		var output strings.Builder
		usage := provider.Usage{}
		for event := range stream {
			switch event.Type {
			case provider.EventTextDelta:
				output.WriteString(event.TextDelta)
			case provider.EventUsage:
				if event.Usage != nil {
					usage.InputTokens += event.Usage.InputTokens
					usage.OutputTokens += event.Usage.OutputTokens
				}
			case provider.EventError:
				if event.Error != nil {
					return "", usage, event.Error
				}
			}
		}
		return output.String(), usage, nil
	}
}

func adjudicateOnePacket(ctx context.Context, packet adjudicationPacket, caller usageModelCaller, journal *adjudicationCallJournal) (adjudicationDecision, bool, error) {
	return adjudicateOnePacketWithIdentity(ctx, packet, caller, journal, "")
}

func adjudicateOnePacketWithIdentity(ctx context.Context, packet adjudicationPacket, caller usageModelCaller, journal *adjudicationCallJournal, runIdentity string) (adjudicationDecision, bool, error) {
	if !packet.Triggered {
		decision := fallbackAdjudicationDecision(packet, adjudicationFallbackNotTriggered, 0, provider.Usage{})
		return decision, false, validateAdjudicationDecision(decision, packet)
	}
	userPrompt, err := buildAdjudicationVerifierPrompt(packet)
	if err != nil {
		return adjudicationDecision{}, false, err
	}
	inputDigest, err := adjudicationPacketInputDigest(packet, runIdentity)
	if err != nil {
		return adjudicationDecision{}, false, err
	}
	if journal != nil {
		if err := journal.Start(packet, inputDigest); err != nil {
			return adjudicationDecision{}, false, err
		}
	}
	usage := provider.Usage{}
	var raw string
	if caller == nil {
		err = fmt.Errorf("adjudication provider caller is nil")
	} else {
		raw, usage, err = caller(ctx, adjudicationSystemPromptFor(packet), userPrompt)
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 {
		usage = provider.Usage{}
		err = fmt.Errorf("provider returned invalid usage")
	}
	if err != nil {
		decision := fallbackAdjudicationDecision(packet, adjudicationFallbackProviderFailed, 1, usage)
		if journal != nil {
			if terminalErr := journal.Terminal(packet, inputDigest, decision, false); terminalErr != nil {
				return adjudicationDecision{}, false, terminalErr
			}
		}
		return decision, false, nil
	}
	response, parseErr := parseAdjudicationVerifierResponse(raw, packet)
	if parseErr != nil {
		decision := fallbackAdjudicationDecision(packet, adjudicationVerifierFallbackReason(parseErr), 1, usage)
		if journal != nil {
			if terminalErr := journal.Terminal(packet, inputDigest, decision, false); terminalErr != nil {
				return adjudicationDecision{}, false, terminalErr
			}
		}
		return decision, false, nil
	}
	decision := selectedAdjudicationDecision(packet, response, usage)
	if err := validateAdjudicationDecision(decision, packet); err != nil {
		return adjudicationDecision{}, false, err
	}
	if journal != nil {
		if err := journal.Terminal(packet, inputDigest, decision, true); err != nil {
			return adjudicationDecision{}, false, err
		}
	}
	return decision, true, nil
}

func executeAdjudicationRun(ctx context.Context, dir string, manifest adjudicationManifest, packets []adjudicationPacket, config adjudicationRunConfig, caller usageModelCaller) (adjudicationSeal, error) {
	if config.Concurrency < 1 || config.MaxTokens < 1 || len(packets) != manifest.QuestionCount ||
		strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.Model) == "" ||
		strings.TrimSpace(config.ModelRevision) == "" || !isDigest(config.BaseURLDigest) ||
		!isDigest(config.BinaryDigest) || manifest.PromptDigest != adjudicationPromptDigest() {
		return adjudicationSeal{}, fmt.Errorf("invalid adjudication run configuration or packet count")
	}
	for _, name := range []string{adjudicationDecisionsFile, adjudicationSealFile, adjudicationScoreFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return adjudicationSeal{}, fmt.Errorf("adjudication run refuses existing artifact %s", name)
		} else if !os.IsNotExist(err) {
			return adjudicationSeal{}, err
		}
	}
	journal, err := openAdjudicationCallJournal(filepath.Join(dir, adjudicationCallsFile), manifest.ProtocolHash, packets)
	if err != nil {
		return adjudicationSeal{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = journal.Close()
		}
	}()
	type workItem struct {
		index  int
		packet adjudicationPacket
	}
	type workResult struct {
		index    int
		decision adjudicationDecision
		err      error
	}
	decisions := make([]adjudicationDecision, len(packets))
	runIdentity := adjudicationRunIdentityDigest(config.Provider, config.BaseURLDigest, config.Model,
		config.ModelRevision, config.MaxTokens, config.BinaryDigest, manifest.PromptDigest)
	var pendingItems []workItem
	for i, packet := range packets {
		if !packet.Triggered {
			decision, _, decisionErr := adjudicateOnePacket(ctx, packet, nil, nil)
			if decisionErr != nil {
				return adjudicationSeal{}, decisionErr
			}
			decisions[i] = decision
			continue
		}
		if prior, ok := journal.TerminalDecision(packet.PacketID); ok {
			if err := validateAdjudicationDecision(prior, packet); err != nil {
				return adjudicationSeal{}, err
			}
			priorInput, inputOK := journal.TerminalInputDigest(packet.PacketID)
			wantInput, inputErr := adjudicationPacketInputDigest(packet, runIdentity)
			if inputErr != nil || !inputOK || priorInput != wantInput {
				return adjudicationSeal{}, fmt.Errorf("resume call identity drift for %q", packet.PacketID)
			}
			decisions[i] = prior
			continue
		}
		pendingItems = append(pendingItems, workItem{index: i, packet: packet})
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan workItem)
	results := make(chan workResult, config.Concurrency)
	workerCount := config.Concurrency
	if workerCount > len(pendingItems) {
		workerCount = len(pendingItems)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				decision, _, runErr := adjudicateOnePacketWithIdentity(runCtx, item.packet, caller, journal, runIdentity)
				results <- workResult{index: item.index, decision: decision, err: runErr}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, item := range pendingItems {
			select {
			case jobs <- item:
			case <-runCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	var firstRunErr error
	completedItems := 0
	for result := range results {
		completedItems++
		if result.err != nil {
			if firstRunErr == nil {
				firstRunErr = result.err
				cancel()
			}
			continue
		}
		decisions[result.index] = result.decision
	}
	if firstRunErr != nil {
		return adjudicationSeal{}, firstRunErr
	}
	if completedItems != len(pendingItems) {
		return adjudicationSeal{}, fmt.Errorf("adjudication run interrupted before all pending packets completed")
	}
	if err := journal.Close(); err != nil {
		return adjudicationSeal{}, err
	}
	closed = true
	sort.Slice(decisions, func(i, j int) bool {
		return adjudicationIdentityLess(decisions[i].Conv, decisions[i].Q, decisions[j].Conv, decisions[j].Q)
	})
	packetByID := make(map[string]adjudicationPacket, len(packets))
	for _, packet := range packets {
		packetByID[packet.PacketID] = packet
	}
	fallbacks := make(map[string]int)
	providerAttempts, inputTokens, outputTokens := 0, 0, 0
	for i, decision := range decisions {
		packet, ok := packetByID[decision.PacketID]
		if !ok {
			return adjudicationSeal{}, fmt.Errorf("decision %d has unknown packet", i)
		}
		if err := validateAdjudicationDecision(decision, packet); err != nil {
			return adjudicationSeal{}, fmt.Errorf("validate decision %d: %w", i, err)
		}
		providerAttempts += decision.ProviderAttempts
		inputTokens += decision.InputTokens
		outputTokens += decision.OutputTokens
		if decision.State == adjudicationDecisionFallback {
			fallbacks[decision.FallbackReason]++
		}
	}
	decisionSetDigest, decisionBytes, err := adjudicationJSONLDigest(decisions)
	if err != nil {
		return adjudicationSeal{}, err
	}
	started, completed, failed := journal.Stats()
	seal := adjudicationSeal{
		Schema: adjudicationSealSchema, ProtocolHash: manifest.ProtocolHash, PacketSetDigest: manifest.PacketSetDigest,
		DecisionSetDigest: decisionSetDigest, PromptDigest: manifest.PromptDigest, Provider: config.Provider,
		BaseURLDigest: config.BaseURLDigest, Model: config.Model, ModelRevision: config.ModelRevision,
		MaxTokens: config.MaxTokens, BinaryDigest: config.BinaryDigest, PlannedCalls: manifest.TriggeredCount, StartedCalls: started,
		CompletedCalls: completed, FailedCalls: failed, ProviderAttempts: providerAttempts, Retries: 0,
		InputTokens: inputTokens, OutputTokens: outputTokens, FallbackCounts: fallbacks,
		PricingStatus: config.PricingStatus, InputCNYPerMillion: config.InputCNYPerMillion,
		OutputCNYPerMillion: config.OutputCNYPerMillion, QuestionCount: manifest.QuestionCount,
		DecisionCount: len(decisions), Valid: true,
	}
	if seal.PricingStatus == "" {
		seal.PricingStatus = "unpriced"
	}
	if config.InputCNYPerMillion != nil && config.OutputCNYPerMillion != nil {
		estimated := (float64(inputTokens)*(*config.InputCNYPerMillion) + float64(outputTokens)*(*config.OutputCNYPerMillion)) / 1_000_000
		seal.EstimatedCNY = &estimated
	}
	if seal.StartedCalls != seal.ProviderAttempts || seal.ProviderAttempts != seal.CompletedCalls+seal.FailedCalls ||
		seal.ProviderAttempts != manifest.TriggeredCount || seal.DecisionCount != manifest.QuestionCount {
		return adjudicationSeal{}, fmt.Errorf("adjudication seal counts are inconsistent")
	}
	if err := writeAdjudicationAtomic(filepath.Join(dir, adjudicationDecisionsFile), decisionBytes); err != nil {
		return adjudicationSeal{}, err
	}
	if err := writeJSON(filepath.Join(dir, adjudicationSealFile), seal); err != nil {
		return adjudicationSeal{}, err
	}
	return seal, nil
}

func validateAdjudicationCLIOptions(opt options, mode adjudicationMode) error {
	if _, err := adjudicationModeFor(opt); err != nil {
		return err
	}
	switch mode {
	case adjudicationModeBuild:
		if strings.TrimSpace(opt.adjudicationBuildDir) == "" || len(opt.adjudicationCandidates) != 3 ||
			strings.TrimSpace(opt.adjudicationTracePath) == "" || strings.TrimSpace(opt.storeDir) == "" ||
			strings.TrimSpace(opt.adjudicationSeed) == "" {
			return fmt.Errorf("adjudication build requires DIR, exactly three candidates, trace, store-dir, and seed")
		}
		if opt.adjudicationAllowPaid {
			return fmt.Errorf("adjudication build forbids --adjudication-allow-paid")
		}
	case adjudicationModeValidate:
		if strings.TrimSpace(opt.adjudicationValidateDir) == "" || len(opt.adjudicationCandidates) != 0 ||
			opt.adjudicationTracePath != "" || opt.adjudicationSeed != "" || opt.adjudicationAllowPaid {
			return fmt.Errorf("adjudication validate accepts only its directory")
		}
	case adjudicationModeRun:
		if strings.TrimSpace(opt.adjudicationRunDir) == "" || len(opt.adjudicationCandidates) != 0 ||
			opt.adjudicationTracePath != "" || opt.adjudicationSeed != "" || !opt.adjudicationAllowPaid ||
			opt.adjudicationMaxTokens < 1 {
			return fmt.Errorf("adjudication run requires DIR, --adjudication-allow-paid, and a positive output cap")
		}
	case adjudicationModeScore:
		if strings.TrimSpace(opt.adjudicationScoreDir) == "" || len(opt.adjudicationCandidates) != 3 ||
			opt.adjudicationTracePath != "" || opt.adjudicationSeed != "" || opt.adjudicationAllowPaid {
			return fmt.Errorf("adjudication score requires DIR and exactly three candidates")
		}
	default:
		return fmt.Errorf("unknown adjudication mode %q", mode)
	}
	return nil
}

type adjudicationSemanticEntry struct {
	Name            string `json:"name"`
	Content         string `json:"content"`
	EventDate       string `json:"event_date,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	SourceSessionID string `json:"source_session_id,omitempty"`
}

type adjudicationEvidenceCatalog struct {
	Rendered       map[string]string
	RawDigest      string
	RawSize        int64
	SemanticDigest string
}

func loadAdjudicationEvidenceCatalog(ctx context.Context, dbPath string, names []string) (adjudicationEvidenceCatalog, error) {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			return adjudicationEvidenceCatalog{}, fmt.Errorf("frozen store has sidecar %s", filepath.Base(dbPath+suffix))
		} else if !os.IsNotExist(err) {
			return adjudicationEvidenceCatalog{}, fmt.Errorf("stat frozen store sidecar: %w", err)
		}
	}
	before, err := fileSHA256(dbPath)
	if err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	u := url.URL{Scheme: "file", Path: absPath}
	query := u.Query()
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return adjudicationEvidenceCatalog{}, err
	}
	entryStore := memory.NewEntryStore(db)
	allEntries, err := entryStore.List(ctx)
	if err != nil {
		_ = db.Close()
		return adjudicationEvidenceCatalog{}, err
	}
	semanticEntries := make([]adjudicationSemanticEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		semanticEntries = append(semanticEntries, adjudicationSemanticEntry{
			Name: entry.Name, Content: entry.Content, EventDate: adjudicationTime(entry.EventDate),
			CreatedAt: adjudicationTimeValue(entry.CreatedAt), SourceSessionID: entry.SourceSessionID,
		})
	}
	sort.Slice(semanticEntries, func(i, j int) bool { return semanticEntries[i].Name < semanticEntries[j].Name })
	uniqueNames := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			uniqueNames = append(uniqueNames, name)
		}
	}
	entries, err := entryStore.EntriesByName(ctx, uniqueNames)
	if err != nil {
		_ = db.Close()
		return adjudicationEvidenceCatalog{}, err
	}
	rendered := make(map[string]string, len(uniqueNames))
	for _, name := range uniqueNames {
		entry := entries[name]
		if entry == nil {
			_ = db.Close()
			return adjudicationEvidenceCatalog{}, fmt.Errorf("missing evidence entry %q", name)
		}
		rendered[name] = (retrievedMemory{
			Name: entry.Name, Content: entry.Content, EventDate: adjudicationTime(entry.EventDate),
			Recorded: adjudicationTimeValue(entry.CreatedAt), SourceSessionID: entry.SourceSessionID,
		}).Line()
	}
	if err := db.Close(); err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	after, err := fileSHA256(dbPath)
	if err != nil {
		return adjudicationEvidenceCatalog{}, err
	}
	if before != after {
		return adjudicationEvidenceCatalog{}, fmt.Errorf("frozen store digest changed during read")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			return adjudicationEvidenceCatalog{}, fmt.Errorf("immutable read created sidecar %s", filepath.Base(dbPath+suffix))
		} else if !os.IsNotExist(err) {
			return adjudicationEvidenceCatalog{}, err
		}
	}
	return adjudicationEvidenceCatalog{
		Rendered: rendered, RawDigest: before, RawSize: info.Size(), SemanticDigest: evalJSONDigest(semanticEntries),
	}, nil
}

func readAdjudicationEvidenceForConv(ctx context.Context, dbPath string, hits []adjudicationTraceHit) ([]adjudicationEvidenceItem, string, error) {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			return nil, "", fmt.Errorf("frozen store has sidecar %s", filepath.Base(dbPath+suffix))
		} else if !os.IsNotExist(err) {
			return nil, "", fmt.Errorf("stat frozen store sidecar: %w", err)
		}
	}
	before, err := fileSHA256(dbPath)
	if err != nil {
		return nil, "", err
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve store path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: absPath}
	query := u.Query()
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, "", fmt.Errorf("open immutable store: %w", err)
	}
	db.SetMaxOpenConns(1)
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, "", fmt.Errorf("ping immutable store: %w", err)
	}
	entryStore := memory.NewEntryStore(db)
	allEntries, err := entryStore.List(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("snapshot immutable store: %w", err)
	}
	semanticEntries := make([]adjudicationSemanticEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		semanticEntries = append(semanticEntries, adjudicationSemanticEntry{
			Name: entry.Name, Content: entry.Content, EventDate: adjudicationTime(entry.EventDate),
			CreatedAt: adjudicationTimeValue(entry.CreatedAt), SourceSessionID: entry.SourceSessionID,
		})
	}
	sort.Slice(semanticEntries, func(i, j int) bool { return semanticEntries[i].Name < semanticEntries[j].Name })
	semanticDigest := evalJSONDigest(semanticEntries)
	names := make([]string, len(hits))
	for i, hit := range hits {
		if hit.Rank != i+1 || strings.TrimSpace(hit.Name) == "" {
			return nil, "", fmt.Errorf("invalid trace hit at index %d", i)
		}
		names[i] = hit.Name
	}
	entries, err := entryStore.EntriesByName(ctx, names)
	if err != nil {
		return nil, "", fmt.Errorf("resolve evidence entries: %w", err)
	}
	items := make([]adjudicationEvidenceItem, len(hits))
	seen := make(map[string]bool, len(hits))
	for i, hit := range hits {
		if seen[hit.Name] {
			return nil, "", fmt.Errorf("duplicate evidence entry %q", hit.Name)
		}
		seen[hit.Name] = true
		entry := entries[hit.Name]
		if entry == nil {
			return nil, "", fmt.Errorf("missing evidence entry %q", hit.Name)
		}
		content := (retrievedMemory{
			Name: entry.Name, Content: entry.Content, EventDate: adjudicationTime(entry.EventDate),
			Recorded: adjudicationTimeValue(entry.CreatedAt), SourceSessionID: entry.SourceSessionID,
		}).Line()
		if strings.TrimSpace(content) == "" {
			return nil, "", fmt.Errorf("empty evidence entry %q", hit.Name)
		}
		items[i] = adjudicationEvidenceItem{
			EvidenceID: fmtEvidenceID(i + 1), Rank: i + 1, Content: content,
			ContentDigest: adjudicationTextDigest(content),
		}
	}
	if err := db.Close(); err != nil {
		return nil, "", fmt.Errorf("close immutable store: %w", err)
	}
	closed = true
	after, err := fileSHA256(dbPath)
	if err != nil {
		return nil, "", err
	}
	if before != after {
		return nil, "", fmt.Errorf("frozen store digest changed during read")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			return nil, "", fmt.Errorf("immutable read created sidecar %s", filepath.Base(dbPath+suffix))
		} else if !os.IsNotExist(err) {
			return nil, "", fmt.Errorf("stat post-read sidecar: %w", err)
		}
	}
	return items, semanticDigest, nil
}

func adjudicationTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func adjudicationTimeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func adjudicationGitReceipt() (string, bool) {
	commitRaw, commitErr := exec.Command("git", "rev-parse", "HEAD").Output()     //nolint:gosec // fixed diagnostic command
	statusRaw, statusErr := exec.Command("git", "status", "--porcelain").Output() //nolint:gosec // fixed diagnostic command
	commit := strings.TrimSpace(string(commitRaw))
	if commitErr != nil {
		commit = ""
	}
	return commit, statusErr != nil || len(strings.TrimSpace(string(statusRaw))) > 0
}
