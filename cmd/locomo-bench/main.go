// Command locomo-bench evaluates the memory subsystem on the LoCoMo benchmark
// (memory-hybrid-retrieval-locomo). It ingests each conversation through the
// ADD-only extraction pipeline into a throwaway store, answers each question in
// a single pass from the top-k retrieval results, and scores answers with an
// LLM-as-a-Judge aligned with the open mem0ai/memory-benchmarks methodology.
//
// The --retrieval flag switches the backend (fts | hybrid | both). "both" runs
// the two retrievers over ONE shared extraction so the semantic signal's uplift
// is measured A-B under identical extraction, answering, and judging — and the
// costly extraction pass is paid once, not twice. Runs are resumable via a
// per-arm JSONL artifact and parallelized with a global LLM-call semaphore.
//
// --chunks additionally indexes verbatim session chunks alongside the extracted
// facts (a chunks ∪ artifacts union store; extraction alone is lossy
// distillation — arXiv:2601.00821). --store-dir persists each conversation's
// store so later runs reuse the paid extraction pass verbatim.
//
// Credentials come from the environment only and are never logged or written to
// run artifacts:
//
//	LOCOMO_API_KEY   (required) answer + judge model key
//	LOCOMO_PROVIDER  (default anthropic; set "openai" for OpenAI-chat endpoints)
//	LOCOMO_BASE_URL  (default https://api.deepseek.com/anthropic)
//	LOCOMO_MODEL     (default deepseek-v4-pro)     answer + judge model
//	EXTRACT_MODEL    (default = LOCOMO_MODEL)      extraction model (a fast,
//	                 non-reasoning model here cuts wall-clock and cost markedly)
//	EMBED_API_KEY / EMBED_BASE_URL / EMBED_MODEL  (hybrid arm embedding client)
//	EMBED_RERANK_MODEL  (optional; enables the hybrid arm's cross-encoder
//	                 rerank stage against the same EMBED_BASE_URL endpoint)
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/anthropic"
	"github.com/wallfacers/engram/provider/openai"
	"github.com/wallfacers/engram/store"
)

type options struct {
	dataPath             string
	runDir               string
	storeDir             string
	datasetFormat        string
	compareSpec          string
	repeats              int
	estimate             bool
	noIDKRetry           bool
	budgetBaseline       float64
	retrieval            string
	maxConvs             int
	maxQuestions         int
	topK                 int
	maxTokens            int
	concurrency          int
	chunks               bool
	chunkQuota           int
	filterPool           int
	assoc                bool
	assocDepth           int
	temporalScore        bool
	temporalHardFilter   bool
	conflictResolution   bool
	abstainPrompt        bool
	forceAnswer          bool
	temporalAnswerPrompt bool
	opinionPass          bool
	adversarial          int
	catTopKSpec          string
	catQuotaSpec         string
	catTopK              map[int]int
	catQuota             map[int]int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "locomo-bench:", err)
		os.Exit(1)
	}
}

func run() error {
	var opt options
	flag.StringVar(&opt.dataPath, "data", "", "path to LoCoMo JSON dataset (required)")
	flag.StringVar(&opt.runDir, "run-dir", "", "directory for resumable JSONL run artifacts (required)")
	flag.StringVar(&opt.datasetFormat, "dataset-format", "locomo", "dataset format: locomo | longmemeval")
	flag.StringVar(&opt.compareSpec, "compare", "", "compare two run directories: --compare DIR_A DIR_B")
	flag.IntVar(&opt.repeats, "repeats", 1, "independent repeated evaluation runs")
	flag.BoolVar(&opt.estimate, "estimate", false, "estimate local cost and exit without API calls")
	flag.BoolVar(&opt.noIDKRetry, "no-idk-retry", false, "disable the legacy IDK retrieval retries")
	flag.Float64Var(&opt.budgetBaseline, "budget-baseline", 0, "calibrated answer context token baseline for the 1.5x budget gate")
	flag.StringVar(&opt.retrieval, "retrieval", "both", "retrieval backend: fts | hybrid | both")
	flag.IntVar(&opt.maxConvs, "conversations", 0, "limit number of conversations (0 = all)")
	flag.IntVar(&opt.maxQuestions, "questions", 0, "limit questions per conversation (0 = all)")
	flag.IntVar(&opt.topK, "top-k", 30, "retrieval budget per question")
	flag.IntVar(&opt.maxTokens, "max-tokens", 8000, "max output tokens (reasoning models need headroom for thinking + answer)")
	flag.IntVar(&opt.concurrency, "concurrency", 24, "max concurrent in-flight LLM calls")
	flag.BoolVar(&opt.chunks, "chunks", false, "union store: index verbatim session chunks alongside extracted facts (applies to every arm)")
	flag.IntVar(&opt.chunkQuota, "chunk-quota", 0, "reserve this many top-k slots for verbatim chunks (0 = pure fused order)")
	flag.IntVar(&opt.filterPool, "filter-pool", 0, "listwise LLM filter: retrieve this many candidates, one LLM call selects the relevant subset (0 = off; must exceed top-k to matter)")
	flag.BoolVar(&opt.assoc, "assoc", false, "enable associative graph retrieval")
	flag.IntVar(&opt.assocDepth, "assoc-depth", 2, "associative graph walk depth (maximum 2)")
	flag.BoolVar(&opt.temporalScore, "temporal-score", false, "enable soft temporal retrieval scoring")
	flag.BoolVar(&opt.temporalHardFilter, "temporal-hard-filter", false, "experimental hard temporal candidate filter")
	flag.BoolVar(&opt.abstainPrompt, "abstain-prompt", false, "use the abstention-oriented answer prompt")
	flag.BoolVar(&opt.forceAnswer, "force-answer", false, "require a best guess instead of an I don't know answer")
	flag.BoolVar(&opt.temporalAnswerPrompt, "temporal-answer-prompt", false, "use the temporal reasoning answer prompt for category 2")
	flag.StringVar(&opt.catTopKSpec, "cat-top-k", "", `per-category top-k overrides, e.g. "1=150" — multi-hop enumeration questions need evidence from many sessions`)
	flag.StringVar(&opt.catQuotaSpec, "cat-chunk-quota", "", `per-category chunk-quota overrides, e.g. "1=50,4=30"`)
	flag.BoolVar(&opt.opinionPass, "opinion-pass", false, "run a supplementary extraction pass focused on opinions/preferences/traits (ADD-only; run once per store — resuming with this flag duplicates entries)")
	flag.IntVar(&opt.adversarial, "adversarial", 0, "include category-5 adversarial questions, scored by refusal per the Mem0 convention (0 = skip, -1 = all, N = at most N per conversation)")
	flag.StringVar(&opt.storeDir, "store-dir", "", "persist per-conversation stores here and reuse their extraction on re-runs (default in-memory)")
	if err := flag.CommandLine.Parse(normalizeCompareArgs(os.Args[1:])); err != nil {
		return err
	}
	if err := validatePromptModes(opt); err != nil {
		return err
	}
	if err := validateAssocDepth(opt.assocDepth); err != nil {
		return err
	}

	if opt.compareSpec != "" {
		dirs, err := parseCompareSpec(opt.compareSpec)
		if err != nil {
			return err
		}
		report, err := compareRunDirs(dirs[0], dirs[1])
		if err != nil {
			return err
		}
		if err := writeCompare(filepath.Join(dirs[0], "compare.json"), report); err != nil {
			return fmt.Errorf("write compare.json: %w", err)
		}
		fmt.Printf("compare: n_a=%d n_b=%d flips A→B=%d B→A=%d McNemar p=%.6f CI overlap=%t verdict=%s\n",
			report.NA, report.NB, report.FlipsAToB, report.FlipsBToA, report.McNemarP, report.CIOverlap, report.Verdict)
		return nil
	}
	if opt.dataPath == "" {
		flag.Usage()
		return fmt.Errorf("--data is required")
	}
	if opt.repeats < 1 {
		return fmt.Errorf("--repeats must be at least 1")
	}
	if opt.datasetFormat != "locomo" && opt.datasetFormat != "longmemeval" {
		return fmt.Errorf("--dataset-format must be locomo or longmemeval, got %q", opt.datasetFormat)
	}
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	for _, arm := range arms {
		if err := validatePromptModes(optionsForArm(opt, arm)); err != nil {
			return fmt.Errorf("arm %s: %w", arm, err)
		}
	}
	if opt.catTopK, err = parseCatOverrides(opt.catTopKSpec); err != nil {
		return fmt.Errorf("--cat-top-k: %w", err)
	}
	if opt.catQuota, err = parseCatOverrides(opt.catQuotaSpec); err != nil {
		return fmt.Errorf("--cat-chunk-quota: %w", err)
	}
	if opt.concurrency < 1 {
		opt.concurrency = 1
	}

	convs, err := loadBenchmarkDataset(opt.dataPath, opt.datasetFormat)
	if err != nil {
		return err
	}
	sampledConversations := 0
	if opt.maxConvs > 0 && opt.maxConvs < len(convs) {
		sampledConversations = opt.maxConvs
		convs = convs[:opt.maxConvs]
	}
	prices, err := parsePriceTable(os.Getenv("LOCOMO_PRICE_TABLE"))
	if err != nil {
		return err
	}
	model := envOr("LOCOMO_MODEL", "deepseek-v4-pro")
	extractModel := envOr("EXTRACT_MODEL", model)
	if opt.estimate {
		return printEstimate(convs, opt, prices, model, extractModel)
	}
	if opt.runDir == "" {
		return fmt.Errorf("--run-dir is required unless --estimate or --compare is used")
	}
	apiKey := os.Getenv("LOCOMO_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LOCOMO_API_KEY is required (never passed as a flag so it stays out of process listings)")
	}
	baseURL := envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if sampledConversations > 0 {
		logger.Info("sampling conversations", "limit", sampledConversations)
	}

	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if err := checkRunDirRegime(opt); err != nil {
		return err
	}
	// One provider; a global semaphore caps concurrent in-flight LLM calls so
	// many conversations/questions run in parallel without exceeding the rate
	// limit. Every LLM call (extraction, answer, judge) passes through it.
	// Provider protocol is selectable so the harness can target either an
	// Anthropic-messages endpoint (default) or an OpenAI-chat-completions one
	// (LOCOMO_PROVIDER=openai). Both satisfy provider.Provider identically.
	var prov provider.Provider
	switch strings.ToLower(envOr("LOCOMO_PROVIDER", "anthropic")) {
	case "openai":
		prov = openai.New(openai.Options{APIKey: apiKey, BaseURL: baseURL, IncludeUsage: true})
	case "anthropic", "":
		prov = anthropic.New(anthropic.Options{APIKey: apiKey, BaseURL: baseURL, DefaultMaxTokens: opt.maxTokens})
	default:
		return fmt.Errorf("LOCOMO_PROVIDER must be anthropic or openai, got %q", os.Getenv("LOCOMO_PROVIDER"))
	}
	sem := make(chan struct{}, opt.concurrency)
	ledger := newCostLedger(prices)
	recordUsage := func(role, model string, usage provider.Usage) {
		recordBenchUsage(ledger, role, model, usage)
	}
	answerUsageCall := gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "answer", recordUsage))
	filterCall := modelCallerFromUsage(gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "filter", recordUsage)))
	rewriteCall := modelCallerFromUsage(gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "rewrite", recordUsage)))
	judgeUsageCall := gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "judge", recordUsage))
	extractCall := pipeline.ModelCaller(gate(sem, newModelCallerWithUsage(prov, extractModel, opt.maxTokens, "extract", recordUsage)))

	// The embedding client is shared across conversations (safe for concurrent
	// use) and only built when a hybrid arm is present.
	var embClient embedding.Client
	if hasArm(arms, "hybrid") {
		embClient = buildBenchEmbeddingClient(logger, func(inputTokens, outputTokens int) {
			ledger.Add("embed", envOr("EMBED_MODEL", "qwen3-embedding:0.6b"), inputTokens, outputTokens)
		})
	}

	logger.Info("starting", "conversations", len(convs), "arms", arms, "concurrency", opt.concurrency,
		"model", model, "extract_model", extractModel, "top_k", opt.topK)

	ctx := context.Background()
	storeDir := opt.storeDir
	if storeDir == "" {
		storeDir = filepath.Join(opt.runDir, ".stores")
	}
	buildOpt := opt
	buildOpt.storeDir = storeDir
	runtimes := make([]*conversationRuntime, len(convs))
	var buildWG sync.WaitGroup
	var buildMu sync.Mutex
	var buildErr error
	for ci := range convs {
		buildWG.Add(1)
		go func(index int) {
			defer buildWG.Done()
			runtime, err := buildConversationRuntime(ctx, buildOpt, convs[index], extractCall, embClient, arms, logger)
			buildMu.Lock()
			defer buildMu.Unlock()
			if err != nil {
				if buildErr == nil {
					buildErr = fmt.Errorf("conversation %d: %w", convs[index].ID, err)
				}
				return
			}
			runtimes[index] = runtime
		}(ci)
	}
	buildWG.Wait()
	if buildErr != nil {
		for _, runtime := range runtimes {
			runtime.Close()
		}
		return buildErr
	}
	defer func() {
		for _, runtime := range runtimes {
			runtime.Close()
		}
	}()

	for repeat := 1; repeat <= opt.repeats; repeat++ {
		repeatOpt := opt
		if opt.repeats > 1 {
			repeatOpt.runDir = filepath.Join(opt.runDir, fmt.Sprintf("run-%d", repeat))
		}
		if err := os.MkdirAll(repeatOpt.runDir, 0o755); err != nil {
			return fmt.Errorf("create repeat run dir: %w", err)
		}
		states := make([]*armState, 0, len(arms))
		for _, name := range arms {
			j, err := openJournal(repeatOpt.runDir, name)
			if err != nil {
				return err
			}
			states = append(states, &armState{name: name, agg: newAggregator(), journal: j})
		}
		var wg sync.WaitGroup
		for ci := range convs {
			wg.Add(1)
			go func(conv conversation, current []*armState) {
				defer wg.Done()
				index := conv.ID
				if index < 0 || index >= len(runtimes) || runtimes[index] == nil {
					logger.Warn("conversation runtime unavailable", "conversation", conv.ID)
					return
				}
				if err := answerConversationWithUsage(ctx, repeatOpt, conv, runtimes[index], answerUsageCall, filterCall, rewriteCall, judgeUsageCall, current, logger); err != nil {
					logger.Warn("conversation failed", "conversation", conv.ID, "err", err)
				}
			}(convs[ci], states)
		}
		wg.Wait()
		for _, state := range states {
			state.journal.Close()
			report(state, repeatOpt)
		}
		if len(states) == 2 {
			reportDelta(states[0], states[1])
		}
	}
	if len(arms) > 2 {
		warnExtraPairedArms(logger, arms)
	}
	if len(arms) >= 2 {
		runsA, err := loadArmRuns(opt.runDir, arms[0], opt.repeats)
		if err != nil {
			return fmt.Errorf("load paired arm %s: %w", arms[0], err)
		}
		runsB, err := loadArmRuns(opt.runDir, arms[1], opt.repeats)
		if err != nil {
			return fmt.Errorf("load paired arm %s: %w", arms[1], err)
		}
		paired, err := pairedReport(runsA, runsB)
		if err != nil {
			return fmt.Errorf("build paired report: %w", err)
		}
		if err := writePaired(filepath.Join(opt.runDir, "paired.json"), paired); err != nil {
			return fmt.Errorf("write paired.json: %w", err)
		}
	}
	ledger.EstimatedUSD = estimateDatasetCost(convs, opt, prices, model, extractModel)
	if err := writeCost(filepath.Join(opt.runDir, "cost.json"), ledger.Report()); err != nil {
		return fmt.Errorf("write cost.json: %w", err)
	}
	for _, arm := range arms {
		runs, err := loadArmRuns(opt.runDir, arm, opt.repeats)
		if err != nil {
			return err
		}
		stats := statsFromRuns(runs)
		path := filepath.Join(opt.runDir, "stats.json")
		if len(arms) > 1 {
			path = filepath.Join(opt.runDir, "stats-"+arm+".json")
		}
		if err := writeStats(path, stats); err != nil {
			return fmt.Errorf("write stats: %w", err)
		}
		printStatsSummary(arm, stats)
	}
	fmt.Printf("cost: actual_usd=%.6f %s\n", ledger.ActualUSD(), formatBudgetSummary(ledger.AnswerContextTokensMean(), opt.budgetBaseline))
	return nil
}

func recordBenchUsage(ledger *costLedger, role, model string, usage provider.Usage) {
	ledger.Add(role, model, usage.InputTokens, usage.OutputTokens)
	if role == "answer" {
		ledger.AddContextTokens(usage.InputTokens)
	}
}

func formatBudgetSummary(mean, baseline float64) string {
	if baseline <= 0 {
		return fmt.Sprintf("answer_context_tokens_mean=%.0f budget_ratio=unavailable", mean)
	}
	ratio := mean / baseline
	warning := ""
	if ratio > 1.5 {
		warning = " WARNING: answer context budget exceeds 1.5x baseline; uplift may be budget inflation and is invalid"
	}
	return fmt.Sprintf("answer_context_tokens_mean=%.0f budget_ratio=%.2fx%s", mean, ratio, warning)
}

// armState holds one retrieval arm's grading state.
type armState struct {
	name    string
	agg     *aggregator
	journal *journal
}

func armsFor(retrieval string) ([]string, error) {
	if strings.TrimSpace(retrieval) == "" {
		return nil, fmt.Errorf("--retrieval must not be empty")
	}
	var arms []string
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(retrieval, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "both" {
			for _, defaultArm := range []string{"fts", "hybrid"} {
				if _, duplicate := seen[defaultArm]; duplicate {
					return nil, fmt.Errorf("duplicate retrieval arm %q", defaultArm)
				}
				seen[defaultArm] = struct{}{}
				arms = append(arms, defaultArm)
			}
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate retrieval arm %q", name)
		}
		if _, err := parseArm(name); err != nil {
			return nil, err
		}
		seen[name] = struct{}{}
		arms = append(arms, name)
	}
	if len(arms) == 0 {
		return nil, fmt.Errorf("--retrieval must specify at least one arm")
	}
	return arms, nil
}

type armSpec struct {
	backend    string
	overrides  bool
	mechanisms map[string]bool
}

var supportedArmMechanisms = map[string]struct{}{
	"assoc":    {},
	"temporal": {},
}

func parseArm(name string) (armSpec, error) {
	parts := strings.Split(strings.TrimSpace(name), "+")
	backend := strings.ToLower(strings.TrimSpace(parts[0]))
	if backend != "fts" && backend != "hybrid" {
		return armSpec{}, fmt.Errorf("invalid retrieval arm %q: backend must be fts or hybrid", name)
	}
	spec := armSpec{backend: backend, mechanisms: map[string]bool{}}
	for _, raw := range parts[1:] {
		mechanism := strings.ToLower(strings.TrimSpace(raw))
		if mechanism == "" {
			return armSpec{}, fmt.Errorf("invalid retrieval arm %q: empty mechanism suffix", name)
		}
		if mechanism == "conflict" || mechanism == "abstain" {
			return armSpec{}, fmt.Errorf("invalid retrieval arm %q: %s not implemented until US4/US5", name, mechanism)
		}
		if _, ok := supportedArmMechanisms[mechanism]; !ok {
			return armSpec{}, fmt.Errorf("invalid retrieval arm %q: unsupported mechanism %q", name, mechanism)
		}
		if spec.mechanisms[mechanism] {
			return armSpec{}, fmt.Errorf("invalid retrieval arm %q: duplicate mechanism %q", name, mechanism)
		}
		spec.overrides = true
		spec.mechanisms[mechanism] = true
	}
	return spec, nil
}

func armBackend(name string) string {
	spec, err := parseArm(name)
	if err != nil {
		return strings.SplitN(strings.ToLower(name), "+", 2)[0]
	}
	return spec.backend
}

func optionsForArm(global options, name string) options {
	spec, err := parseArm(name)
	if err != nil {
		return options{}
	}
	if !spec.overrides {
		arm := global
		arm.assoc = false
		arm.temporalScore = false
		arm.temporalHardFilter = false
		arm.conflictResolution = false
		arm.abstainPrompt = false
		return arm
	}
	arm := global
	arm.assoc = spec.mechanisms["assoc"]
	arm.temporalScore = spec.mechanisms["temporal"]
	arm.temporalHardFilter = false
	arm.conflictResolution = spec.mechanisms["conflict"]
	arm.abstainPrompt = spec.mechanisms["abstain"]
	return arm
}

func optionsForRun(global options, name string, multiArm bool) options {
	if !multiArm {
		spec, err := parseArm(name)
		if err == nil && !spec.overrides {
			return global
		}
	}
	return optionsForArm(global, name)
}

func hasArm(arms []string, name string) bool {
	for _, a := range arms {
		if armBackend(a) == name {
			return true
		}
	}
	return false
}

// gate wraps a modelCaller so each call holds one slot of the global semaphore
// for its full duration — the true in-flight-call limit. Shares gateUsage's
// per-call timeout and retry so extraction calls cannot deadlock the
// semaphore either.
func gate(sem chan struct{}, c modelCaller) modelCaller {
	return modelCallerFromUsage(gateUsage(sem, usageCallerFromModel(c)))
}

// conversationRuntime owns one prepared conversation store and its read-only
// retrievers. It stays open across repeated answer/judge runs so extraction and
// embedding are not paid again for every repeat.
type conversationRuntime struct {
	store      *store.Store
	retrievers map[string]*memory.Retriever
}

func (r *conversationRuntime) Close() {
	if r == nil || r.store == nil {
		return
	}
	_ = r.store.Close()
}

// buildConversationRuntime performs the one-time extraction, optional opinion
// pass, chunk ingestion, and embedding backfill for one conversation.
func buildConversationRuntime(ctx context.Context, opt options, conv conversation, extractCall pipeline.ModelCaller, embClient embedding.Client, arms []string, logger *slog.Logger) (*conversationRuntime, error) {
	dsn := ":memory:"
	if opt.storeDir != "" {
		if err := os.MkdirAll(opt.storeDir, 0o755); err != nil {
			return nil, fmt.Errorf("create store dir: %w", err)
		}
		dsn = filepath.Join(opt.storeDir, fmt.Sprintf("conv%d.db", conv.ID))
	}
	st, err := store.Open(ctx, store.Options{DSN: dsn})
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	keepStore := false
	defer func() {
		if !keepStore {
			_ = st.Close()
		}
	}()

	es := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(es, vectors, embClient, memory.DefaultEmbedBuffer)

	pipe := pipeline.New(pipeline.Config{
		Entries:  es,
		Embedder: embedder,
		Call:     extractCall,
		Budgets:  memory.DefaultBudgets(),
	})

	// Ingest each session with its date (extraction is the shared, once-paid
	// pass). A persisted store that already holds extracted facts skips it.
	if n, err := countExtracted(ctx, es); err != nil {
		return nil, err
	} else if n > 0 {
		if temporalMechanismEnabled(opt, arms) {
			if err := validateTemporalStore(ctx, st.DB(), n); err != nil {
				return nil, err
			}
		}
		logger.Info("reusing persisted extraction", "conversation", conv.ID, "facts", n)
	} else {
		for _, s := range conv.Sessions {
			msgs := make([]pipeline.Message, 0, len(s.Turns))
			for _, tn := range s.Turns {
				msgs = append(msgs, pipeline.Message{Role: "user", Text: tn.Speaker + ": " + tn.Text})
			}
			if _, err := pipe.Ingest(ctx, s.Date, fmt.Sprintf("conv%d-sess%d", conv.ID, s.Index), msgs); err != nil {
				logger.Warn("ingest session failed", "conversation", conv.ID, "session", s.Index, "err", err)
			}
		}
	}
	if opt.opinionPass {
		// Supplementary ADD-only extraction: opinions, preferences, and traits
		// are systematically under-captured by the event-focused main pass and
		// are what LoCoMo open-domain questions probe. The existing facts stay
		// untouched; this only adds entries.
		opinionPipe := pipeline.New(pipeline.Config{
			Entries:  es,
			Embedder: embedder,
			Call: func(ctx context.Context, system, user string) (string, error) {
				return extractCall(ctx, system+opinionExtractionAddendum, user)
			},
			Budgets: memory.DefaultBudgets(),
		})
		added := 0
		for _, s := range conv.Sessions {
			msgs := make([]pipeline.Message, 0, len(s.Turns))
			for _, tn := range s.Turns {
				msgs = append(msgs, pipeline.Message{Role: "user", Text: tn.Speaker + ": " + tn.Text})
			}
			n, err := opinionPipe.Ingest(ctx, s.Date, fmt.Sprintf("conv%d-sess%d-op", conv.ID, s.Index), msgs)
			if err != nil {
				logger.Warn("opinion pass failed", "conversation", conv.ID, "session", s.Index, "err", err)
				continue
			}
			added += n
		}
		logger.Info("opinion pass done", "conversation", conv.ID, "entries_added", added)
	}
	if opt.chunks {
		if n, err := ingestChunks(ctx, es, conv); err != nil {
			logger.Warn("chunk ingest failed", "conversation", conv.ID, "err", err)
		} else {
			logger.Info("verbatim chunks ingested", "conversation", conv.ID, "chunks", n)
		}
	}
	// Drain embeddings synchronously before answering (only meaningful when a
	// hybrid arm supplied an embedding client).
	if err := embedder.Backfill(ctx); err != nil {
		logger.Warn("embedding backfill failed", "conversation", conv.ID, "err", err)
	}
	embedder.Close()

	// One retriever per arm over the same store. Only the hybrid arm gets the
	// semantic signal and the optional rerank stage; fts stays the pure legacy
	// baseline.
	retrievers := make(map[string]*memory.Retriever, len(arms))
	for _, arm := range arms {
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		retrieverOpts := retrieverOptionsForAt(armOpt, temporalNowForConversation(conv))
		if armBackend(arm) == "hybrid" {
			retrievers[arm] = memory.NewRetrieverWithOptions(es, vectors, embClient, buildBenchReranker(), retrieverOpts)
		} else {
			retrievers[arm] = memory.NewRetrieverWithOptions(es, vectors, nil, nil, retrieverOpts)
		}
	}
	keepStore = true
	return &conversationRuntime{store: st, retrievers: retrievers}, nil
}

func retrieverOptionsFor(opt options) memory.RetrieverOptions {
	return retrieverOptionsForAt(opt, time.Time{})
}

func retrieverOptionsForAt(opt options, now time.Time) memory.RetrieverOptions {
	return memory.RetrieverOptions{
		Associative:        opt.assoc,
		AssocDepth:         opt.assocDepth,
		TemporalScore:      opt.temporalScore || opt.temporalHardFilter,
		TemporalHardFilter: opt.temporalHardFilter,
		Now:                now,
	}
}

func retrievalFingerprint(opt options) string {
	depth := opt.assocDepth
	if depth <= 0 || depth > 2 {
		depth = 2
	}
	fingerprint := fmt.Sprintf("assoc=%t;assoc_depth=%d", opt.assoc, depth)
	if opt.temporalScore || opt.temporalHardFilter {
		fingerprint += fmt.Sprintf(";temporal_score=%t;temporal_hard_filter=%t", opt.temporalScore || opt.temporalHardFilter, opt.temporalHardFilter)
	}
	return fingerprint
}

func temporalNowForConversation(conv conversation) time.Time {
	var latest time.Time
	for _, session := range conv.Sessions {
		if session.Date.IsZero() || (!latest.IsZero() && !session.Date.After(latest)) {
			continue
		}
		latest = session.Date.UTC()
	}
	return latest
}

// checkRunDirRegime pins a run dir to one answer regime. Journal resume keys
// on (conversation, question) only, so resuming an existing run dir under a
// different regime would silently mix results graded under two 口径 in one
// artifact; refuse instead.
func checkRunDirRegime(opt options) error {
	// Arm suffixes can override answer-regime mechanisms per arm (e.g.
	// +abstain), so the arm layout is part of the pinned regime too.
	regime := answerRegimeFingerprint(opt) + ";retrieval=" + opt.retrieval
	path := filepath.Join(opt.runDir, "regime.json")
	data, err := os.ReadFile(path)
	if err == nil {
		prev := strings.TrimSpace(string(data))
		if prev != regime {
			return fmt.Errorf("run dir %s was written under answer regime %q; current flags give %q — use a fresh --run-dir (journal resume would mix regimes)", opt.runDir, prev, regime)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("read run dir regime: %w", err)
	}
	if err := os.WriteFile(path, []byte(regime+"\n"), 0o644); err != nil {
		return fmt.Errorf("write run dir regime: %w", err)
	}
	return nil
}

func answerRegimeFingerprint(opt options) string {
	fingerprint := fmt.Sprintf("force_answer=%t;abstain_prompt=%t;no_idk_retry=%t", opt.forceAnswer, opt.abstainPrompt, opt.noIDKRetry)
	if opt.temporalAnswerPrompt {
		fingerprint += ";temporal_answer_prompt=true"
	}
	return fingerprint
}

func warnExtraPairedArms(logger *slog.Logger, arms []string) {
	if len(arms) <= 2 {
		return
	}
	logger.Warn("paired report uses first two arms; remaining arms are not paired", "paired_arms", arms[:2], "all_arms", arms)
}

func validateAssocDepth(depth int) error {
	if depth > 2 {
		return fmt.Errorf("--assoc-depth must be at most 2, got %d", depth)
	}
	return nil
}

func validatePromptModes(opt options) error {
	if opt.forceAnswer && opt.abstainPrompt {
		return fmt.Errorf("--force-answer and --abstain-prompt are mutually exclusive")
	}
	return nil
}

// answerConversation runs only the answer/judge phase for a prepared
// conversation. Questions run concurrently and are bounded by the global
// LLM-call semaphore.
func answerConversation(ctx context.Context, opt options, conv conversation, runtime *conversationRuntime, answerCall, judgeCall modelCaller, states []*armState, logger *slog.Logger) error {
	return answerConversationWithUsage(ctx, opt, conv, runtime, usageCallerFromModel(answerCall), answerCall, answerCall, usageCallerFromModel(judgeCall), states, logger)
}

func answerConversationWithUsage(ctx context.Context, opt options, conv conversation, runtime *conversationRuntime, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, states []*armState, logger *slog.Logger) error {
	if runtime == nil {
		return fmt.Errorf("conversation runtime is nil")
	}

	var qwg sync.WaitGroup
	selected := selectQuestions(conv, opt)
	for _, selectedQuestion := range selected {
		qi, qa := selectedQuestion.Index, selectedQuestion.QA
		key := resultKey{Conv: conv.ID, Q: qi}
		for _, s := range states {
			if prev, ok := s.journal.lookup(key); ok {
				s.agg.add(qa.Category, prev.Correct) // resume: reuse recorded result
				continue
			}
			qwg.Add(1)
			go func(s *armState, qa locomoQA, key resultKey, armOpt options) {
				defer qwg.Done()
				correct, predicted, usage := answerAndJudgeWithUsage(ctx, runtime.retrievers[s.name], answerCall, filterCall, rewriteCall, judgeCall, armOpt, qa, logger)
				s.agg.add(qa.Category, correct)
				s.journal.write(result{
					Conv:                key.Conv,
					Q:                   key.Q,
					QuestionID:          qa.QuestionID,
					Category:            qa.Category,
					CategoryName:        qa.CategoryName,
					QuestionType:        qa.QuestionType,
					Adversarial:         qa.Adversarial || qa.Category == adversarialCategory,
					Correct:             correct,
					Question:            qa.Question,
					Gold:                goldFor(qa),
					Predicted:           predicted,
					RetrievalFlags:      retrievalFingerprint(armOpt),
					AnswerRegime:        answerRegimeFingerprint(armOpt),
					InputTokens:         usage.InputTokens,
					OutputTokens:        usage.OutputTokens,
					AnswerContextTokens: usage.InputTokens,
				})
			}(s, qa, key, optionsForRun(opt, s.name, len(states) > 1))
		}
	}
	qwg.Wait()
	logger.Info("conversation done", "conversation", conv.ID, "answered", len(selected))
	return nil
}

// processConversation remains a one-shot compatibility wrapper for callers
// that do not need repeated runs.
func processConversation(ctx context.Context, opt options, conv conversation, extractCall pipeline.ModelCaller, answerCall, judgeCall modelCaller, embClient embedding.Client, states []*armState, logger *slog.Logger) error {
	arms := make([]string, 0, len(states))
	for _, state := range states {
		arms = append(arms, state.name)
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extractCall, embClient, arms, logger)
	if err != nil {
		return err
	}
	defer runtime.Close()
	return answerConversation(ctx, opt, conv, runtime, answerCall, judgeCall, states, logger)
}

// opinionExtractionAddendum retargets the extraction prompt at the subjective
// layer the event-focused main pass under-captures.
const opinionExtractionAddendum = `

IMPORTANT OVERRIDE FOR THIS PASS: extract ONLY subjective facts — opinions, preferences, likes and dislikes, values, personality traits, fears, aspirations, plans, and intentions. Attribute every fact to its speaker by name (e.g. "Melanie prefers…", "Caroline believes…"). Do NOT extract plain events, dates, or activities; those are already captured. If a message contains no subjective content, extract nothing from it.`

// countExtracted reports how many non-chunk entries the store already holds,
// which signals that a persisted store's extraction pass can be reused.
func countExtracted(ctx context.Context, es *memory.EntryStore) (int, error) {
	entries, err := es.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("count extracted: %w", err)
	}
	n := 0
	for _, e := range entries {
		if e.FactSource != "verbatim_chunk" {
			n++
		}
	}
	return n, nil
}

func temporalMechanismEnabled(opt options, arms []string) bool {
	for _, arm := range arms {
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		if armOpt.temporalScore || armOpt.temporalHardFilter {
			return true
		}
	}
	return false
}

func validateTemporalStore(ctx context.Context, db *sql.DB, facts int) error {
	if facts <= 0 {
		return nil
	}
	var ranged, aliases, dated int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM memory_entries WHERE event_start IS NOT NULL`).Scan(&ranged); err != nil {
		return fmt.Errorf("check temporal event ranges: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM memory_event_aliases`).Scan(&aliases); err != nil {
		return fmt.Errorf("check temporal aliases: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM memory_entries WHERE event_date IS NOT NULL`).Scan(&dated); err != nil {
		return fmt.Errorf("check temporal event dates: %w", err)
	}
	// dated>0 with no ranges/aliases is the pre-T026 extraction signature; a
	// store whose extraction legitimately produced no dates at all (dated==0)
	// must pass, or rebuilding would reproduce the same state forever.
	if ranged == 0 && aliases == 0 && dated > 0 {
		return fmt.Errorf("temporal retrieval requires rebuilding persisted store: %d facts have event dates but no event ranges or aliases (pre-temporal extraction)", facts)
	}
	return nil
}

// answerAndJudge retrieves, answers, and grades one question. When the first
// answer is an IDK bail-out, one rewrite-and-retry round runs: the model
// produces an alternative search query, its hits are unioned with the first
// round's, and the question is answered again (EverMemOS-style second round,
// paid only for the IDK tail). Returns (correct, predicted answer).
func answerAndJudge(ctx context.Context, retriever *memory.Retriever, answerCall, judgeCall modelCaller, opt options, qa locomoQA, logger *slog.Logger) (bool, string) {
	correct, predicted, _ := answerAndJudgeWithUsage(ctx, retriever, usageCallerFromModel(answerCall), answerCall, answerCall, usageCallerFromModel(judgeCall), opt, qa, logger)
	return correct, predicted
}

func answerAndJudgeWithUsage(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, opt options, qa locomoQA, logger *slog.Logger) (bool, string, provider.Usage) {
	topK, quota := opt.retrievalFor(qa.Category)
	hits, err := retrieve(ctx, retriever, filterCall, qa.Question, topK, quota, opt)
	if err != nil {
		logger.Warn("retrieve failed; question scored wrong", "err", err)
		return false, "", provider.Usage{}
	}
	prompt := answerPromptForOptionsWithTemporal(qa.Category, opt.forceAnswer, opt.temporalAnswerPrompt)
	predicted, usage, err := answerCall(ctx, prompt, buildAnswerPrompt(qa.Question, toMemories(hits)))
	if err != nil {
		logger.Warn("answer call failed; question scored wrong", "err", err)
		return false, "", usage
	}

	if isIDK(predicted) && !opt.noIDKRetry {
		if retry, retryUsage, ok := retryWithRewriteUsage(ctx, retriever, answerCall, filterCall, rewriteCall, opt, qa, prompt, hits); ok {
			predicted = retry
			usage = retryUsage
		} else if retry, retryUsage, ok := retryWithWiderNetUsage(ctx, retriever, answerCall, opt, qa, prompt); ok {
			predicted = retry
			usage = retryUsage
		}
	}

	verdict, _, err := judgeCall(ctx, judgeSystemPrompt, buildJudgePrompt(qa.Question, goldFor(qa), predicted))
	if err != nil {
		logger.Warn("judge call failed; question scored wrong", "err", err)
		return false, predicted, usage
	}
	return parseJudgeVerdict(verdict), predicted, usage
}

// adversarialGold is the judge-facing gold for category-5 questions. They have
// no answer in the conversation (the dataset's adversarial_answer field is the
// TRAP, not the truth) — per the Mem0/LoCoMo convention a correct response
// declines to answer.
const adversarialGold = `This question cannot be answered from the conversation — it contains no such information. The correct response DECLINES: it says the information is not mentioned, not available, or "I don't know". Any confident substantive answer is wrong.`

func goldFor(qa locomoQA) string {
	if qa.Adversarial || qa.Category == adversarialCategory {
		return adversarialGold
	}
	return qa.AnswerText()
}

// parseCatOverrides parses "cat=value" pairs ("1=150,4=30") into a map.
func parseCatOverrides(spec string) (map[int]int, error) {
	m := map[int]int{}
	if spec == "" {
		return m, nil
	}
	for _, part := range strings.Split(spec, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected cat=value, got %q", part)
		}
		c, err1 := strconv.Atoi(strings.TrimSpace(kv[0]))
		v, err2 := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err1 != nil || err2 != nil || c < 1 || v < 1 {
			return nil, fmt.Errorf("invalid pair %q", part)
		}
		m[c] = v
	}
	return m, nil
}

// retrievalFor resolves the per-question retrieval budget; categories with an
// override (e.g. multi-hop enumeration needs breadth) diverge from the global
// defaults.
func (o options) retrievalFor(category int) (topK, quota int) {
	topK, quota = o.topK, o.chunkQuota
	if v, ok := o.catTopK[category]; ok {
		topK = v
	}
	if v, ok := o.catQuota[category]; ok {
		quota = v
	}
	return topK, quota
}

// retrieve is the per-question retrieval front door: quota'd top-k, optionally
// widened + narrowed by the listwise LLM filter when --filter-pool is set.
func retrieve(ctx context.Context, retriever *memory.Retriever, filterCall modelCaller, query string, topK, quota int, opt options) ([]memory.Result, error) {
	if opt.filterPool > topK {
		return retrieveFiltered(ctx, retriever, filterCall, query, topK, quota, opt.filterPool)
	}
	return retrieveWithQuota(ctx, retriever, query, topK, quota)
}

// retryWithRewrite runs the IDK second round. Returns (answer, true) only when
// the retry produced a non-IDK answer worth keeping.
func retryWithRewrite(ctx context.Context, retriever *memory.Retriever, call modelCaller, opt options, qa locomoQA, prompt string, first []memory.Result) (string, bool) {
	return retryWithRewriteLegacy(ctx, retriever, call, call, call, opt, qa, prompt, first)
}

func retryWithRewriteLegacy(ctx context.Context, retriever *memory.Retriever, answerCall, filterCall, rewriteCall modelCaller, opt options, qa locomoQA, prompt string, first []memory.Result) (string, bool) {
	rewritten, err := rewriteCall(ctx, queryRewriteSystemPrompt, "QUESTION: "+qa.Question)
	if err != nil {
		return "", false
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" || rewritten == qa.Question {
		return "", false
	}
	topK, quota := opt.retrievalFor(qa.Category)
	more, err := retrieve(ctx, retriever, filterCall, rewritten, topK, quota, opt)
	if err != nil || len(more) == 0 {
		return "", false
	}
	seen := make(map[string]struct{}, len(first))
	union := make([]memory.Result, 0, len(first)+len(more))
	for _, h := range first {
		seen[h.Name] = struct{}{}
		union = append(union, h)
	}
	fresh := 0
	for _, h := range more {
		if _, dup := seen[h.Name]; dup {
			continue
		}
		union = append(union, h)
		fresh++
	}
	if fresh == 0 {
		return "", false
	}
	retry, err := answerCall(ctx, prompt, buildAnswerPrompt(qa.Question, toMemories(union)))
	if err != nil || isIDK(retry) {
		return "", false
	}
	return retry, true
}

func retryWithRewriteUsage(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, opt options, qa locomoQA, prompt string, first []memory.Result) (string, provider.Usage, bool) {
	rewritten, err := rewriteCall(ctx, queryRewriteSystemPrompt, "QUESTION: "+qa.Question)
	if err != nil {
		return "", provider.Usage{}, false
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" || rewritten == qa.Question {
		return "", provider.Usage{}, false
	}
	topK, quota := opt.retrievalFor(qa.Category)
	more, err := retrieve(ctx, retriever, filterCall, rewritten, topK, quota, opt)
	if err != nil || len(more) == 0 {
		return "", provider.Usage{}, false
	}
	seen := make(map[string]struct{}, len(first))
	union := make([]memory.Result, 0, len(first)+len(more))
	for _, h := range first {
		seen[h.Name] = struct{}{}
		union = append(union, h)
	}
	fresh := 0
	for _, h := range more {
		if _, dup := seen[h.Name]; dup {
			continue
		}
		union = append(union, h)
		fresh++
	}
	if fresh == 0 {
		return "", provider.Usage{}, false
	}
	retry, usage, err := answerCall(ctx, prompt, buildAnswerPrompt(qa.Question, toMemories(union)))
	if err != nil || isIDK(retry) {
		return "", usage, false
	}
	return retry, usage, true
}

// retryWithWiderNet is the second-stage IDK escalation: when the rewrite round
// also failed, re-retrieve the ORIGINAL question at 3× breadth and answer once
// more. It only ever fires on the IDK tail, so an aggressive net is safe — any
// grounded answer beats a bail-out. Returns (answer, true) only on a non-IDK
// answer.
func retryWithWiderNet(ctx context.Context, retriever *memory.Retriever, call modelCaller, opt options, qa locomoQA, prompt string) (string, bool) {
	retry, _, ok := retryWithWiderNetUsage(ctx, retriever, usageCallerFromModel(call), opt, qa, prompt)
	return retry, ok
}

func retryWithWiderNetUsage(ctx context.Context, retriever *memory.Retriever, call usageModelCaller, opt options, qa locomoQA, prompt string) (string, provider.Usage, bool) {
	topK, quota := opt.retrievalFor(qa.Category)
	hits, err := retrieveWithQuota(ctx, retriever, qa.Question, topK*3, quota*3)
	if err != nil || len(hits) <= topK {
		return "", provider.Usage{}, false
	}
	retry, usage, err := call(ctx, prompt, buildAnswerPrompt(qa.Question, toMemories(hits)))
	if err != nil || isIDK(retry) {
		return "", usage, false
	}
	return retry, usage, true
}

// toMemories converts retrieval hits into the prompt-facing form.
func toMemories(hits []memory.Result) []retrievedMemory {
	mems := make([]retrievedMemory, 0, len(hits))
	for _, h := range hits {
		rm := retrievedMemory{Name: h.Name, Content: h.Content, SourceSessionID: h.SourceSessionID}
		if h.EventDate != nil && !h.EventDate.IsZero() {
			rm.EventDate = h.EventDate.Format("2006-01-02")
		}
		if !h.CreatedAt.IsZero() {
			rm.Recorded = h.CreatedAt.Format("2006-01-02")
		}
		mems = append(mems, rm)
	}
	return mems
}

// buildBenchEmbeddingClient builds the embedding client from EMBED_* env, with
// local defaults. Returns nil (semantic disabled) on failure.
func buildBenchEmbeddingClient(logger *slog.Logger, usage func(inputTokens, outputTokens int)) embedding.Client {
	c, err := embedding.New(embedding.Config{
		BaseURL: envOr("EMBED_BASE_URL", "http://127.0.0.1:11434/v1"),
		Model:   envOr("EMBED_MODEL", "qwen3-embedding:0.6b"),
		APIKey:  os.Getenv("EMBED_API_KEY"),
		Timeout: 30 * time.Second,
		Usage:   usage,
	})
	if err != nil || c == nil {
		logger.Warn("hybrid arm: embedding client unavailable; semantic signal disabled (degrades to BM25+entity)")
		return nil
	}
	return c
}

// buildBenchReranker builds the rerank client from EMBED_RERANK_MODEL (empty =
// disabled) against the same EMBED_BASE_URL endpoint.
func buildBenchReranker() embedding.Reranker {
	rr, err := embedding.NewReranker(embedding.RerankConfig{
		BaseURL: envOr("EMBED_BASE_URL", "http://127.0.0.1:11434/v1"),
		Model:   os.Getenv("EMBED_RERANK_MODEL"),
		APIKey:  os.Getenv("EMBED_API_KEY"),
		Timeout: 60 * time.Second,
	})
	if err != nil || rr == nil {
		return nil
	}
	return rr
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ---- aggregation + report ----

type aggregator struct {
	mu         sync.Mutex
	byCategory map[int]*catStat
}

type catStat struct {
	total, correct int
}

func newAggregator() *aggregator { return &aggregator{byCategory: map[int]*catStat{}} }

func (a *aggregator) add(category int, correct bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.byCategory[category]
	if s == nil {
		s = &catStat{}
		a.byCategory[category] = s
	}
	s.total++
	if correct {
		s.correct++
	}
}

func (a *aggregator) overall() (correct, total int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.byCategory {
		correct += s.correct
		total += s.total
	}
	return correct, total
}

func report(s *armState, opt options) {
	a := s.agg
	a.mu.Lock()
	defer a.mu.Unlock()
	fmt.Printf("\n=== LoCoMo results (retrieval=%s, top_k=%d) ===\n", s.name, opt.topK)
	cats := make([]int, 0, len(a.byCategory))
	for c := range a.byCategory {
		cats = append(cats, c)
	}
	sort.Ints(cats)
	var total, correct int
	for _, c := range cats {
		st := a.byCategory[c]
		total += st.total
		correct += st.correct
		fmt.Printf("  %-14s %4d/%4d  %5.1f%%\n", categoryLabel(c), st.correct, st.total, pct(st.correct, st.total))
	}
	fmt.Printf("  %-14s %4d/%4d  %5.1f%%\n", "OVERALL (J)", correct, total, pct(correct, total))
	if opt.maxConvs > 0 || opt.maxQuestions > 0 {
		fmt.Printf("  (sampled run: conversations=%d questions/conv=%d)\n", opt.maxConvs, opt.maxQuestions)
	}
}

// reportDelta prints the A-B uplift between two arms (typically fts vs hybrid).
func reportDelta(a, b *armState) {
	ac, at := a.agg.overall()
	bc, bt := b.agg.overall()
	fmt.Printf("\n=== A-B uplift (%s → %s) ===\n", a.name, b.name)
	fmt.Printf("  %-8s J = %5.1f%%\n", a.name, pct(ac, at))
	fmt.Printf("  %-8s J = %5.1f%%\n", b.name, pct(bc, bt))
	fmt.Printf("  delta       %+5.1f pp\n", pct(bc, bt)-pct(ac, at))
}

func pct(n, d int) float64 {
	return 100 * ratio(n, d)
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func loadArmRuns(baseDir, arm string, repeats int) ([][]result, error) {
	runs := make([][]result, 0, repeats)
	for repeat := 1; repeat <= repeats; repeat++ {
		dir := baseDir
		if repeats > 1 {
			dir = filepath.Join(baseDir, fmt.Sprintf("run-%d", repeat))
		}
		path := filepath.Join(dir, fmt.Sprintf("results-%s.jsonl", arm))
		items, err := readResultsJSONL(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		runs = append(runs, items)
	}
	return runs, nil
}

type selectedQuestion struct {
	Index int
	QA    locomoQA
}

// selectQuestions is the single source of truth for both execution and
// estimate question counts. Normal questions obey maxQuestions, while the
// separately configured adversarial tail remains eligible after that limit.
func selectQuestions(conv conversation, opt options) []selectedQuestion {
	selected := make([]selectedQuestion, 0, len(conv.QA))
	answered, adversarial := 0, 0
	for index, qa := range conv.QA {
		if qa.Adversarial || qa.Category == adversarialCategory {
			include := opt.datasetFormat == "longmemeval" || opt.adversarial < 0 || (opt.adversarial > 0 && adversarial < opt.adversarial)
			if include {
				selected = append(selected, selectedQuestion{Index: index, QA: qa})
				adversarial++
			}
			continue
		}
		if opt.maxQuestions > 0 && answered >= opt.maxQuestions {
			continue
		}
		selected = append(selected, selectedQuestion{Index: index, QA: qa})
		answered++
	}
	return selected
}

func countSelectedQuestions(convs []conversation, opt options) int {
	total := 0
	for _, conv := range convs {
		total += len(selectQuestions(conv, opt))
	}
	return total
}

// Strike 1 full-run measurements (2026-07-19): answer input ≈5146 tok/question,
// judge input ≈4055 tok/question (the judge prompt carries the full retrieval
// context). Nominal prices only — the relay bills cached repeated prefixes at
// roughly half the computed figure.
const (
	estimateExtractIn  = 4_000
	estimateExtractOut = 500
	estimateAnswerIn   = 5_100
	estimateAnswerOut  = 50
	estimateFilterIn   = 1_000
	estimateFilterOut  = 0
	estimateJudgeIn    = 4_000
	estimateJudgeOut   = 100
)

type callPlan struct {
	Questions       int
	ExtractionCalls int
	AnswerCalls     int
	AnswerInTokens  int
	AnswerOutTokens int
	FilterCalls     int
	FilterInTokens  int
	FilterOutTokens int
	JudgeCalls      int
	JudgeInTokens   int
	JudgeOutTokens  int
}

func buildCallPlan(convs []conversation, opt options) callPlan {
	repeats := opt.repeats
	if repeats < 1 {
		repeats = 1
	}
	plan := callPlan{Questions: countSelectedQuestions(convs, opt)}
	passes := 1
	if opt.opinionPass {
		passes++
	}
	for _, conv := range convs {
		plan.ExtractionCalls += len(conv.Sessions) * passes
	}
	armCount := 1
	if arms, err := armsFor(opt.retrieval); err == nil && len(arms) > 0 {
		armCount = len(arms)
	}
	plan.AnswerCalls = plan.Questions * repeats * armCount
	plan.AnswerInTokens = plan.AnswerCalls * estimateAnswerIn
	plan.AnswerOutTokens = plan.AnswerCalls * estimateAnswerOut
	plan.FilterCalls = 0
	if opt.filterPool > opt.topK {
		plan.FilterCalls = plan.Questions * repeats * armCount
		plan.FilterInTokens = plan.FilterCalls * estimateFilterIn
		plan.FilterOutTokens = plan.FilterCalls * estimateFilterOut
	}
	plan.JudgeCalls = plan.Questions * repeats * armCount
	plan.JudgeInTokens = plan.JudgeCalls * estimateJudgeIn
	plan.JudgeOutTokens = plan.JudgeCalls * estimateJudgeOut
	return plan
}

func estimateRole(prices priceTable, model string, calls, inTokens, outTokens int) *roleCost {
	role := &roleCost{Calls: calls, InTokens: inTokens, OutTokens: outTokens}
	if price, ok := prices.Lookup(model); ok {
		role.USD = tokenUSD(price, inTokens, outTokens)
	}
	return role
}

func estimateReport(convs []conversation, opt options, prices priceTable, model, extractModel string) costReport {
	plan := buildCallPlan(convs, opt)
	report := costReport{ByRole: map[string]*roleCost{
		"extract": estimateRole(prices, extractModel, plan.ExtractionCalls, plan.ExtractionCalls*estimateExtractIn, plan.ExtractionCalls*estimateExtractOut),
		"answer":  estimateRole(prices, model, plan.AnswerCalls, plan.AnswerInTokens, plan.AnswerOutTokens),
		"filter":  estimateRole(prices, model, plan.FilterCalls, plan.FilterInTokens, plan.FilterOutTokens),
		"judge":   estimateRole(prices, model, plan.JudgeCalls, plan.JudgeInTokens, plan.JudgeOutTokens),
		"embed":   {},
	}}
	for _, role := range report.ByRole {
		report.EstimatedUSD += role.USD
	}
	if _, ok := prices.Lookup(model); !ok {
		report.UnpricedModels = append(report.UnpricedModels, model)
	}
	if _, ok := prices.Lookup(extractModel); !ok && extractModel != model {
		report.UnpricedModels = append(report.UnpricedModels, extractModel)
	}
	sort.Strings(report.UnpricedModels)
	return report
}

func estimateDatasetCost(convs []conversation, opt options, prices priceTable, model, extractModel string) float64 {
	return estimateReport(convs, opt, prices, model, extractModel).EstimatedUSD
}

func printEstimate(convs []conversation, opt options, prices priceTable, model, extractModel string) error {
	plan := buildCallPlan(convs, opt)
	report := estimateReport(convs, opt, prices, model, extractModel)
	fmt.Printf("estimate: dataset=%s repeats=%d questions=%d extract_calls=%d estimated_usd=%.6f\n",
		opt.datasetFormat, opt.repeats, plan.Questions, plan.ExtractionCalls, report.EstimatedUSD)
	for _, modelName := range report.UnpricedModels {
		fmt.Printf("estimate: unpriced model=%s\n", modelName)
	}
	return nil
}

func printStatsSummary(arm string, stats statsReport) {
	keys := make([]string, 0, len(stats.Categories))
	for category := range stats.Categories {
		keys = append(keys, category)
	}
	sort.Strings(keys)
	fmt.Printf("\n=== repeated stats (retrieval=%s, repeats=%d) ===\n", arm, stats.Repeats)
	for _, category := range keys {
		summary := stats.Categories[category]
		fmt.Printf("  %-24s mean=%5.1f%% ci95=[%5.1f%%,%5.1f%%]\n", category,
			summary.Mean*100, summary.CI95[0]*100, summary.CI95[1]*100)
	}
	fmt.Printf("  %-24s mean=%5.1f%% ci95=[%5.1f%%,%5.1f%%]\n", "OVERALL", stats.Overall.Mean*100, stats.Overall.CI95[0]*100, stats.Overall.CI95[1]*100)
	fmt.Printf("  %-24s mean=%5.1f%% ci95=[%5.1f%%,%5.1f%%]\n", "OVERALL_COMPARABLE", stats.OverallComparable.Mean*100, stats.OverallComparable.CI95[0]*100, stats.OverallComparable.CI95[1]*100)
}
