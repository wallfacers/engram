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
//	LOCOMO_API_KEY   (required) answer-side key; judge fallback key
//	LOCOMO_PROVIDER  (default anthropic; set "openai" for OpenAI-chat endpoints)
//	LOCOMO_BASE_URL  (default https://api.deepseek.com/anthropic)
//	LOCOMO_MODEL     (default deepseek-v4-pro)     answer-side model
//	JUDGE_PROVIDER / JUDGE_BASE_URL / JUDGE_API_KEY / JUDGE_MODEL
//	                 (optional; each falls back independently to LOCOMO_*)
//	EXTRACT_MODEL    (default = LOCOMO_MODEL)      extraction model (a fast,
//	                 non-reasoning model here cuts wall-clock and cost markedly)
//	EMBED_API_KEY / EMBED_BASE_URL / EMBED_MODEL  (hybrid arm embedding client)
//	EMBED_RERANK_MODEL  (optional; enables the hybrid arm's cross-encoder
//	                 rerank stage against the same EMBED_BASE_URL endpoint)
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/eventstore"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/provider/anthropic"
	"github.com/wallfacers/engram/provider/openai"
	"github.com/wallfacers/engram/store"
)

type options struct {
	adjudicationBuildDir       string
	adjudicationValidateDir    string
	adjudicationRunDir         string
	adjudicationScoreDir       string
	adjudicationCandidates     []string
	adjudicationTracePath      string
	adjudicationSeed           string
	adjudicationAllowPaid      bool
	adjudicationMaxTokens      int
	adjudicationTemporalPrompt bool
	dataPath                   string
	runDir                     string
	storeDir                   string
	aliasShadow                string
	aliasShadowPrepared        bool
	doc2query                  string
	doc2queryPrepared          bool
	doc2queryBuild             bool
	datasetFormat              string
	compareSpec                string
	evalValidate               string
	evalB0ProtocolPath         string
	evalFreezeB0Protocol       string
	b0Protocol                 *evalProtocol
	evalProtocolPath           string
	fixedGoldOracle            bool
	evalFreezeProtocol         string
	controlProtocolPath        string
	evalBudgetProfile          string
	answerInputTokenCap        int
	counterFingerprint         string
	tokenCounterCalibrate      bool
	tokenCounterBaseURL        string
	formalProtocol             *evalProtocol
	formalCounter              evidencecompiler.TokenCounter
	formalQuestionGate         chan struct{}
	formalReplay               *formalQuestionReplay
	formalCalls                *formalCallJournal
	formalRunIndex             int
	repeats                    int
	judgeAuditPrepare          string
	judgeAuditFinalize         string
	auditControlArm            string
	auditTreatmentArm          string
	auditBenchmark             string
	auditRepeats               int
	auditPlanSeed              string
	auditConcordantPerStratum  int
	auditAccuracyGate          float64
	auditReviews               string
	auditDecisions             string
	estimate                   bool
	noIDKRetry                 bool
	budgetBaseline             float64
	retrieval                  string
	multiQuery                 bool
	mqMaxSubqueries            int
	recallDiagnostic           bool
	maxConvs                   int
	maxQuestions               int
	onlyCategory               int
	onlyEnumeration            bool
	onlyQuestionsFile          string          // --only-questions: path to a question-ID whitelist
	onlyQuestions              map[string]bool // parsed whitelist (nil = no filter)
	topK                       int
	maxTokens                  int
	// confidenceGated enables 041 confidence-gated iterative retrieval: a
	// shallow retrieve→answer, a deterministic hesitation check on the answerer
	// generation, and a deepen→answer pass only when hesitant. Off = the
	// fixed-top-k path byte-identical (SC-003). Harness-only, engine untouched.
	confidenceGated      bool
	confidenceShallowK   int
	confidenceDeepK      int
	confidenceThreshold  float64
	confidenceMaxRounds  int
	confidenceGateJournal *confidenceGateJournal // runtime-only writer for conf_gate_decisions.jsonl
	probeHesitation       bool                   // --probe-hesitation: US1 offline discrimination probe
	probeHesitationJSONL  string                 // --probe-hesitation-jsonl: results-hybrid.jsonl path override
	concurrency                int
	chunks                     bool
	chunkQuota                 int
	filterPool                 int
	assoc                      bool
	assocDepth                 int
	clusterSweep               bool
	temporalScore              bool
	temporalHardFilter         bool
	conflictResolution         bool
	supersededPenalty          float64
	abstainPrompt              bool
	abstainHard                bool
	abstainSoft                bool
	forceAnswer                bool
	imageCaptions              bool
	temporalAnswerPrompt       bool
	temporalDateScaffold       bool
	lmeTypedPrompts            bool
	unifiedAnswerContract      bool
	unifiedPairAudit           bool // runtime-only: exact hybrid vs hybrid+unified fail-closed call/context audit
	unifiedPairDatasetDigest   string
	unifiedProbeFixture        string
	unifiedProbeOut            string
	unifiedProbeRepeats        int
	explicitFlags              map[string]bool // runtime-only: used to reject ignored flags in dedicated modes
	iris                       bool
	irisDepth                  int
	judgeMem0Aligned           bool
	answerModel                string
	judgeModel                 string
	rerank                     bool
	pcic                       bool
	oracle                     bool
	pcicAnnotate               bool
	pcicFillTurns              string
	pcicMetaPath               string
	pcicMeta                   *PCICMeta
	abstainProbe               bool
	abstainProbeOut            string
	abstainGateSpec            string
	abstainGate                AbstainGate
	selector                   chunkSelector
	opinionPass                bool
	adversarial                int
	catTopKSpec                string
	catQuotaSpec               string
	catTopK                    map[int]int
	catQuota                   map[int]int
	coverageOnly               bool
	temporalDiagnostic         bool
	attributionTrace           bool
	joinResults                string
	embedProbe                 bool
	outrankCap                 int
	widePool                   int
	factCoverageTau            float64
	contextParity              *contextParityJournal
	// formalEvidence is the active, namespace-local Evidence Ledger reader
	// used by formal source expansion and the independent pre-answer
	// span/citation validator. It is runtime-only and is never serialized.
	formalEvidence formalEvidenceReader
	// planner is the runtime-only local Planner sidecar adapter wired into the
	// evidencecompiler engine when --compiler-arm planner is configured with a
	// sidecar. It is never serialized.
	planner evidencecompiler.Planner
	// representationArm selects the source-representation renderer for the
	// formal B1 pipeline. ReprChunk900 is the legacy default and is
	// byte-identical to the pre-022 split-chunk expansion.
	representationArm RepresentationKind
	// compilerArm selects the evidence-compilation strategy for the formal
	// B1 pipeline. "" means legacy ranked-prefix packer; "extractive" and
	// "planner" use the evidencecompiler engine (planner Nil →extractive fallback).
	compilerArm string
	// temporalResolution enables 027 query-time temporal validity resolution:
	// within the fixed candidate pool, deterministically organize the time
	// structure of hit evidence (current-value / evolution-chain / temporal-window)
	// before bundle assembly. Additive mechanism flag; default false. Mutual
	// exclusion with --compiler-arm keeps the packer dispatch unambiguous.
	temporalResolution bool
	// plannerBaseURL/plannerModel configure the local Planner sidecar
	// (vllm/ollama, OpenAI-compatible). They are only consumed when
	// --compiler-arm planner; if empty the planner stays nil and the compiler
	// degrades to the deterministic extractive fallback (023 FR-019).
	plannerBaseURL string
	plannerModel   string
	plannerTimeout time.Duration // 0 → defaultPlannerTimeout
	// eventProjection selects the event-projection shadow mode for
	// structured-gap refetch (E0/E1/E2/E3). "" means off.
	eventProjection string
	// eventProjectPath is the 027 write-side event projection file consumed by
	// the event representation renderer (--representation event). Build it with
	// --build-event-project. Empty disables event rendering.
	eventProjectPath string
	// eventProject is the runtime-loaded projection. Never serialized.
	eventProject *eventstore.Project
	// buildEventProjectOut, when set, builds the event projection to this path
	// and exits (no answer/judge). Extraction LLM configured via eventLLM*.
	buildEventProjectOut string
	eventLLMBaseURL      string
	eventLLMModel        string
	eventLLMAPIKey       string
	// gapRefetch enables structured-gap refetch retrieval. Only valid in
	// formal B1 runs; requires --event-projection.
	gapRefetch bool
	// writeDedup enables 024 write-time redundancy suppression: new atomic
	// fact projections are compared against existing facts and suppressed as
	// semantic duplicates (US1). Additive mechanism flag; default false.
	writeDedup bool
	// neighborExtend enables 024 hit-time neighbor extension: after candidate
	// freeze and before answerer assembly, sibling facts sharing evidence are
	// added to the answer context (US2). Additive mechanism flag; default false.
	neighborExtend bool
	// formalEpisodes is the runtime-only EpisodeStore, required by the
	// semantic_episode representation renderer. Nil when not available.
	formalEpisodes *memory.EpisodeStore
	// episodeCluster enables 025 cross-session semantic episode clustering:
	// rebuild semantic_episode projections before rendering (default off).
	episodeCluster bool
	// clusterMinKeywordJaccard / clusterEmbedThresh / clusterMaxEvidence tune the
	// 025 SemanticClusterer when --episode-cluster is on (research.md R3/R4).
	clusterMinKeywordJaccard float64
	clusterEmbedThresh       float64
	clusterMaxEvidence       int
	// nav enables 029 agentic multi-step memory navigation: a reasoning loop
	// over search / expand_query / follow_entity / stop tools whose final
	// evidence bundle feeds the answerer instead of the single-shot top-k list.
	// Harness-only (specs/029); engine untouched. Off by default.
	nav bool
	// navMaxSteps bounds the navigation loop (default 4). Exceeding it without
	// stop triggers the fail-closed single-shot fallback (contracts
	// navigation-tools.md hard constraint).
	navMaxSteps int
	// navK is the per-tool retrieval depth inside the navigation loop (default
	// 8). Distinct from the outer --top-k single-shot budget.
	navK int
	// navDiagnose enables 029 US1 zero-cost rescue-space diagnostic: emits
	// per-question retrieval diagnosis (in_pool / single top-k gold rank / wide
	// pool / deterministic rewrite + follow_entity rescue simulation) to
	// run-dir/nav-diagnose.jsonl for nav_diagnose.py. Retrieval-only; no
	// answer/judge/extraction LLM call.
	navDiagnose bool
	// navTraj is the runtime-only, concurrency-safe writer for the navigation
	// arm's run-dir/nav-trajectories.jsonl. Created per repeat run when --nav is
	// on; never serialized.
	navTraj *navTrajectoryJournal
	// navDecideCall is the runtime-only navigation decide caller (direct vLLM
	// HTTP with thinking disabled — fast pure-JSON tool calls). Nil falls back
	// to answerCall. Never serialized.
	navDecideCall usageModelCaller
	// 030 read-side evidence assembly (specs/030). Only --trace-mediation is
	// default-on (030 full-set verification: 85.91% @ 468 tok — budget-efficient
	// read-side mediation); it needs a configured answerer LLM as sidecar and
	// degrades to the legacy byte-identical path (SC-004) when the sidecar is
	// unavailable. The rest default OFF — when off the answer-context path is
	// byte-identical to the legacy path (SC-004 parity). Engine untouched (FR-001).
	evidenceAssembly          bool // --evidence-assembly: assemble evidence (exact token accounting + chunk-first + category structure); default off
	assemblyDiagnose          bool // --assembly-diagnose: retrieval-only assembly audit (chunk_fraction / token ledger) to run-dir; default off
	assemblyAudit             bool // --assembly-audit: write the same assembly audit from the real answer path; default off
	assemblyLegacyEntityOrder bool // --assembly-legacy-entity-order: benchmark-only pre-033 multi-hop group-major control; default off
	traceMediation            bool // --trace-mediation: US2 grounded-evidence mediator (sidecar; fail-closed gate); default on
	consolidate               bool // --consolidate: US3 conditional compression (only when over cap AND explicitly enabled); default off
	relationContext           bool // 031: append the structural-context relation block to the assembled/traced answer context; default off (parity)
	// assemblyCounter is the runtime-only exact tokenizer for 030 evidence
	// assembly (chat-aware /tokenize, reuses the formal 022 counter config).
	// Never serialized; nil → estimate-ledger fallback (tokens_estimated=true).
	assemblyCounter *assemblyTokenCounter
	// assemblyJournal is the runtime-only, concurrency-safe writer for assembly
	// receipts. Retrieval-only --assembly-diagnose writes assembly-diagnose.jsonl;
	// answer-path --assembly-audit writes assembly-audit.jsonl. Never serialized.
	assemblyJournal *assemblyJournal
	// traceSidecarCaller is the runtime-only 030 US2 grounded-trace generator
	// (DeepSeek-flash via harness-side vLLM HTTP). Nil → legacy path (byte-
	// identical, SC-004). Never serialized.
	traceSidecarCaller usageModelCaller
	// traceGateJournal is the runtime-only, concurrency-safe writer for the 030
	// US2 fail-closed gate audit (run-dir/trace-gate.jsonl). Never serialized.
	traceGateJournal *traceGateJournal
	// consolidateCall is the runtime-only 030 US3 compression generator (reuses
	// the trace-sidecar JSON caller). Nil → deterministic truncation on over-cap.
	// Never serialized.
	consolidateCall usageModelCaller

	adjudicationAuditBuildDir    string
	adjudicationAuditValidateDir string
	adjudicationAuditRunDir      string
	adjudicationAuditScoreDir    string
	adjudicationSourceDir        string
	adjudicationAuditSeed        string
	adjudicationAuditAllowPaid   bool
	adjudicationAuditMaxTokens   int
	adjudicationAttributionDir   string
	adjudicationAuditDir         string
	// --trace-multi-evidence (032): relax the trace sidecar to several evidence
	// statements by intent (multi_hop/temporal → 3-6) instead of the legacy
	// single-evidence prompt. Off by default (SC-004).
	traceMultiEvidence bool // --trace-multi-evidence: intent-breadth evidence prompt for the trace sidecar
	traceEvidenceCap   int  // --trace-evidence-cap: hard cap on evidence statements kept under --trace-multi-evidence (0 = no cap)
	traceFallbackTopk  int  // --trace-fallback-topk: if the trace sidecar cites NONE of the retrieval top-k candidates, fall back to the top-k raw candidates as the answer context (0 = off)
	// notebook (--notebook): inline gold attribution + cross-run mistake book.
	// Off by default — results stay byte-identical (SC-004).
	notebook        bool    // --notebook: accumulate per-question gold attribution + mistakes into the notebook
	notebookAdvise  bool    // --notebook-advise: draft "how to solve this class" advice via the answerer LLM
	notebookDir     string  // --notebook-dir: output dir for notebook.jsonl / mistakes-*.md / index.md (default ./eval-notebook)
	notebookFactTau float64 // --notebook-fact-tau: notebook attribution fact-coverage threshold (lower than factCoverageTau: the notebook must flag "gold plausibly in context" rather than require strict lexical proof). Does NOT affect retrieval or the formal protocol.

	// --counter-refine (L2): after the first answer, verify the draft against
	// counter-evidence selected from the retrieved hits and optionally REVISE.
	// Default off → results stay byte-identical (CounterRefine arXiv:2603.16091).
	counterRefine bool // --counter-refine: answer-conditioned counter-evidence revise pass
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "locomo-bench:", err)
		os.Exit(1)
	}
}

func run() error {
	var opt options
	var adjudicationCandidates adjudicationCandidatePaths
	flag.StringVar(&opt.adjudicationBuildDir, "adjudication-build", "", "034 offline: build label-blind answer-adjudication packets in this directory")
	flag.StringVar(&opt.adjudicationValidateDir, "adjudication-validate", "", "034 offline: validate public answer-adjudication packets in this directory")
	flag.StringVar(&opt.adjudicationRunDir, "adjudication-run", "", "034 opt-in: run the answer-side verifier and seal decisions in this directory")
	flag.StringVar(&opt.adjudicationScoreDir, "adjudication-score", "", "034 offline: score a sealed decision set against historical verdicts")
	flag.StringVar(&opt.adjudicationAuditBuildDir, "adjudication-audit-build", "", "035 offline: build the frozen risk-controlled audit queue in this directory")
	flag.StringVar(&opt.adjudicationAuditValidateDir, "adjudication-audit-validate", "", "035 offline: validate risk-controlled audit build artifacts in this directory")
	flag.StringVar(&opt.adjudicationAuditRunDir, "adjudication-audit-run", "", "035 opt-in: run the dual-view evidence audit and seal decisions in this directory")
	flag.StringVar(&opt.adjudicationAuditScoreDir, "adjudication-audit-score", "", "035 offline: score a sealed second-pass decision set")
	flag.StringVar(&opt.adjudicationSourceDir, "adjudication-source", "", "frozen 034 parent artifact directory (035 build/score only)")
	flag.StringVar(&opt.adjudicationAuditSeed, "adjudication-audit-seed", "", "label-independent 035 view permutation seed (build only)")
	flag.StringVar(&opt.adjudicationAttributionDir, "adjudication-attribution", "", "036 offline: attribute the 034/035 decision gap (zero model calls) in this 034 directory")
	flag.StringVar(&opt.adjudicationAuditDir, "adjudication-audit-source", "", "036 optional: 035 audit seal directory for gap cross-validation; missing degrades to audit_unavailable")
	flag.BoolVar(&opt.adjudicationAuditAllowPaid, "adjudication-audit-allow-paid", false, "explicitly allow the 035 hosted dual-view audit to incur cost")
	flag.IntVar(&opt.adjudicationAuditMaxTokens, "adjudication-audit-max-tokens", 768, "maximum 035 audit output tokens")
	flag.Var(&adjudicationCandidates, "adjudication-candidate", "candidate results JSONL (repeat exactly three times for 034 build/score or 035 audit score)")
	flag.StringVar(&opt.adjudicationTracePath, "adjudication-trace", "", "sanitized-at-read attribution trace source (required for --adjudication-build)")
	flag.StringVar(&opt.adjudicationSeed, "adjudication-seed", "", "label-independent permutation seed (required for --adjudication-build)")
	flag.BoolVar(&opt.adjudicationAllowPaid, "adjudication-allow-paid", false, "explicitly allow the 034 hosted verifier run to incur cost")
	flag.IntVar(&opt.adjudicationMaxTokens, "adjudication-max-tokens", 512, "maximum verifier output tokens")
	flag.BoolVar(&opt.adjudicationTemporalPrompt, "adjudication-temporal-prompt", false, "036/90pp opt-in: use the temporal reasoning contract for category-2 adjudication (default off, byte-identical generic prompt)")
	flag.StringVar(&opt.dataPath, "data", "", "path to LoCoMo JSON dataset (required)")
	flag.StringVar(&opt.runDir, "run-dir", "", "directory for resumable JSONL run artifacts (required)")
	flag.StringVar(&opt.datasetFormat, "dataset-format", "locomo", "dataset format: locomo | longmemeval")
	flag.StringVar(&opt.compareSpec, "compare", "", "compare two run directories: --compare DIR_A DIR_B")
	flag.StringVar(&opt.evalValidate, "eval-validate", "", "validate a frozen 022.v1 artifact run directory and exit (fixed-gold runs also require --data; never makes model calls)")
	flag.StringVar(&opt.evalB0ProtocolPath, "eval-b0-protocol", "", "frozen 022.v1 B0 continuity manifest; enables the independent legacy runner")
	flag.StringVar(&opt.evalFreezeB0Protocol, "eval-freeze-b0-protocol", "", "write a clean-worktree B0 continuity manifest and exit (no model calls)")
	flag.StringVar(&opt.evalProtocolPath, "eval-protocol", "", "frozen 022.v1 protocol manifest; enables formal B1-compatible runner")
	flag.BoolVar(&opt.fixedGoldOracle, "fixed-gold-oracle", false, "run the diagnostic-only all-gold Evidence ceiling from a frozen B1 protocol (no retrieval/extraction)")
	flag.StringVar(&opt.evalFreezeProtocol, "eval-freeze-protocol", "", "write a clean-worktree formal 022 B1 or treatment protocol manifest and exit (no model calls)")
	flag.StringVar(&opt.controlProtocolPath, "control-protocol", "", "frozen B1 control manifest whose protocol hash a treatment freeze must bind")
	flag.StringVar(&opt.evalBudgetProfile, "eval-budget-profile", "", "formal protocol budget profile: low | high (required with --eval-freeze-protocol)")
	flag.IntVar(&opt.answerInputTokenCap, "answer-input-cap", 0, "exact formal answer-input token cap (required with --eval-freeze-protocol)")
	flag.StringVar(&opt.counterFingerprint, "counter-fingerprint", "", "calibrated formal answer tokenizer fingerprint (required with --eval-freeze-protocol)")
	flag.BoolVar(&opt.tokenCounterCalibrate, "token-counter-calibrate", false, "compare formal /tokenize preflight with local answer-runtime usage and write a calibration artifact")
	flag.StringVar(&opt.judgeAuditPrepare, "judge-audit-prepare", "", "prepare a blinded judge-audit from control/treatment answer journals in a run directory and exit (offline)")
	flag.StringVar(&opt.judgeAuditFinalize, "judge-audit-finalize", "", "finalize a judge-audit with two reviewer decision files and exit (offline)")
	flag.StringVar(&opt.auditControlArm, "audit-control-arm", "", "control arm name for --judge-audit-prepare")
	flag.StringVar(&opt.auditTreatmentArm, "audit-treatment-arm", "", "treatment arm name for --judge-audit-prepare")
	flag.StringVar(&opt.auditBenchmark, "audit-benchmark", "", "benchmark name recorded in judge-audit packets (default locomo)")
	flag.IntVar(&opt.auditRepeats, "audit-repeats", 3, "answer repetition count for judge-audit journals (must be odd)")
	flag.StringVar(&opt.auditPlanSeed, "audit-plan-seed", "", "deterministic judge-audit sampling seed (required)")
	flag.IntVar(&opt.auditConcordantPerStratum, "audit-concordant-per-stratum", 1, "concordant questions sampled per stratum")
	flag.Float64Var(&opt.auditAccuracyGate, "audit-accuracy-gate", 0.9, "accuracy gate for raw/corrected verdict-change detection")
	flag.StringVar(&opt.auditReviews, "audit-reviews", "", "reviewer decisions JSON for --judge-audit-finalize (required)")
	flag.StringVar(&opt.auditDecisions, "audit-decisions", "", "optional adjudication JSON for --judge-audit-finalize")
	flag.StringVar(&opt.tokenCounterBaseURL, "token-counter-base-url", "", "local vLLM base URL for formal 022 answer-input preflight (for example http://127.0.0.1:8000/v1)")
	flag.Func("representation", "source representation: chunk_900 | raw_turn_window | semantic_episode (default chunk_900)", func(s string) error {
		switch RepresentationKind(s) {
		case ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode, ReprEvent:
			opt.representationArm = RepresentationKind(s)
			return nil
		default:
			return fmt.Errorf("invalid representation %q: must be chunk_900, raw_turn_window, semantic_episode, or event", s)
		}
	})
	flag.StringVar(&opt.compilerArm, "compiler-arm", "", "evidence compilation strategy: extractive | planner | exact_token (unset = legacy ranked-prefix packer)")
	flag.BoolVar(&opt.temporalResolution, "temporal-resolution", false, "027 query-time temporal validity resolution: organize hit evidence time structure in fixed candidate pool (additive mechanism flag; formal context required, mutually exclusive with --compiler-arm)")
	flag.StringVar(&opt.plannerBaseURL, "planner-base-url", "", "local planner sidecar base URL (OpenAI-compatible; enables --compiler-arm planner; empty = extractive fallback)")
	flag.StringVar(&opt.plannerModel, "planner-model", "", "planner model served by the sidecar (e.g. Qwen2.5-7B-Instruct)")
	flag.DurationVar(&opt.plannerTimeout, "planner-timeout", 0, "planner proposal timeout (0 = default 6s)")
	flag.StringVar(&opt.eventProjection, "event-projection", "", "event projection shadow mode for structured-gap refetch: E0 | E1 | E2 | E3")
	flag.BoolVar(&opt.gapRefetch, "gap-refetch", false, "enable structured-gap refetch retrieval (requires --event-projection)")
	flag.StringVar(&opt.eventProjectPath, "event-project", "", "path to the 027 write-side event projection JSON (build with --build-event-project); required for --representation event")
	flag.StringVar(&opt.buildEventProjectOut, "build-event-project", "", "build the 027 event projection JSON to this path (read convs + extract events via --event-llm-*) and exit; no answer/judge")
	flag.StringVar(&opt.eventLLMBaseURL, "event-llm-base-url", "", "event extraction LLM base URL (OpenAI-compatible local sidecar; also via ENGRAM/EVENT_LLM_BASE_URL)")
	flag.StringVar(&opt.eventLLMModel, "event-llm-model", "", "event extraction LLM model name")
	flag.StringVar(&opt.eventLLMAPIKey, "event-llm-api-key", "", "event extraction LLM API key (secrets via env only; leave empty for local sidecars)")
	flag.BoolVar(&opt.writeDedup, "write-dedup", false, "024 write-time redundancy suppression: suppress duplicate fact projections (additive mechanism flag; formal context required)")
	flag.BoolVar(&opt.neighborExtend, "neighbor-extend", false, "024 hit-time neighbor extension: add shared-evidence sibling facts to answer context (additive mechanism flag; formal context required)")
	flag.BoolVar(&opt.episodeCluster, "episode-cluster", false, "025 cross-session semantic episode clustering: rebuild semantic_episode projections from the clusterer before rendering (additive mechanism flag; semantic_episode representation required)")
	flag.Float64Var(&opt.clusterMinKeywordJaccard, "cluster-min-keyword-jaccard", 0.25, "025 offline clustering shared-keyword Jaccard threshold (used with --episode-cluster)")
	flag.Float64Var(&opt.clusterEmbedThresh, "cluster-embed-thresh", 0.9, "025 clustering embedding-cosine overlay threshold (used with --episode-cluster; needs EMBED endpoint)")
	flag.IntVar(&opt.clusterMaxEvidence, "cluster-max-evidence", 8, "025 per-episode evidence cap (used with --episode-cluster)")
	flag.BoolVar(&opt.nav, "nav", false, "029 agentic multi-step navigation: reasoning loop over search/expand_query/follow_entity/stop tools; final evidence bundle feeds the answerer (harness-only, engine untouched; off by default)")
	flag.IntVar(&opt.navMaxSteps, "nav-max-steps", 4, "029 navigation loop step cap; exceeding it without stop triggers the fail-closed single-shot fallback")
	flag.IntVar(&opt.navK, "nav-k", 8, "029 per-tool retrieval depth inside the navigation loop (distinct from --top-k)")
	flag.BoolVar(&opt.navDiagnose, "nav-diagnose", false, "029 US1 zero-cost rescue-space diagnostic: write per-question retrieval diagnosis to run-dir/nav-diagnose.jsonl for nav_diagnose.py (retrieval-only, needs --store-dir + --run-dir + --chunks)")
	flag.IntVar(&opt.repeats, "repeats", 1, "independent repeated evaluation runs")
	flag.BoolVar(&opt.estimate, "estimate", false, "estimate local cost and exit without API calls")
	flag.BoolVar(&opt.noIDKRetry, "no-idk-retry", false, "disable the legacy IDK retrieval retries")
	flag.Float64Var(&opt.budgetBaseline, "budget-baseline", 0, "calibrated answer context token baseline for the 1.5x budget gate")
	flag.StringVar(&opt.retrieval, "retrieval", "both", "retrieval backend: fts | hybrid | both")
	flag.BoolVar(&opt.multiQuery, "multi-query", false, "decompose each question and retrieve with SearchMulti")
	flag.IntVar(&opt.mqMaxSubqueries, "mq-max-subqueries", 4, "maximum subqueries produced for multi-query retrieval")
	flag.BoolVar(&opt.recallDiagnostic, "recall-diagnostic", false, "retrieval-only single-vs-multi gold-rank and coverage@30 diagnostic")
	flag.IntVar(&opt.maxConvs, "conversations", 0, "limit number of conversations (0 = all)")
	flag.IntVar(&opt.maxQuestions, "questions", 0, "limit questions per conversation (0 = all)")
	flag.IntVar(&opt.onlyCategory, "only-category", 0, "evaluate only this question category (0 = all)")
	flag.BoolVar(&opt.onlyEnumeration, "only-enumeration", false, "evaluate only enumeration questions")
	flag.StringVar(&opt.onlyQuestionsFile, "only-questions", "", "run only these question IDs (one `conv-N-q-M` per line, # = comment; research-subset mode — formal B0/B1 allowed, terminal coverage validation reports an error on subsets)")
	flag.IntVar(&opt.topK, "top-k", 30, "retrieval budget per question")
	flag.IntVar(&opt.maxTokens, "max-tokens", 8000, "max output tokens (reasoning models need headroom for thinking + answer)")
	// 041 confidence-gated iterative retrieval (specs/041). Default off; off is
	// byte-identical to fixed top-k. deep > shallow is enforced in validation.
	flag.BoolVar(&opt.confidenceGated, "confidence-gated", false, "041: iterative retrieval — shallow retrieve→answer, deepen→answer only when the answerer's generation is hesitant (default off; off = fixed top-k byte-identical)")
	flag.IntVar(&opt.confidenceShallowK, "confidence-shallow-k", 30, "041: first-round retrieval depth (shallow)")
	flag.IntVar(&opt.confidenceDeepK, "confidence-deep-k", 150, "041: second-round retrieval depth (deep, reached only when the shallow answer is hesitant)")
	flag.Float64Var(&opt.confidenceThreshold, "confidence-threshold", 3.0, "041: hesitation score at or above which the shallow answer triggers a deepen pass")
	flag.IntVar(&opt.confidenceMaxRounds, "confidence-max-rounds", 2, "041: maximum iteration rounds (>=2; round 2 is final regardless of remaining hesitation)")
	flag.BoolVar(&opt.probeHesitation, "probe-hesitation", false, "041 US1: run the zero-cost hesitation-discrimination probe over an existing results-hybrid.jsonl and exit (writes run-dir/hesitation-probe.json)")
	flag.StringVar(&opt.probeHesitationJSONL, "probe-hesitation-jsonl", "", "041 US1: results-hybrid.jsonl to probe (default <run-dir>/results-hybrid.jsonl)")
	flag.IntVar(&opt.concurrency, "concurrency", 24, "max concurrent in-flight LLM calls")
	flag.BoolVar(&opt.chunks, "chunks", false, "union store: index verbatim session chunks alongside extracted facts (applies to every arm)")
	flag.IntVar(&opt.chunkQuota, "chunk-quota", 0, "reserve this many top-k slots for verbatim chunks (0 = pure fused order)")
	flag.IntVar(&chunkTargetChars, "chunk-target-chars", 900, "soft target per verbatim chunk in code points (store-build time; lower = finer turn-granularity chunks; stores built with different values are NOT comparable)")
	flag.IntVar(&chunkMaxChars, "chunk-max-chars", 1100, "hard cap per stored verbatim chunk in code points (store-build time; must exceed --chunk-target-chars)")
	flag.IntVar(&opt.filterPool, "filter-pool", 0, "listwise LLM filter: retrieve this many candidates, one LLM call selects the relevant subset (0 = off; must exceed top-k to matter)")
	flag.BoolVar(&opt.assoc, "assoc", false, "enable associative graph retrieval")
	flag.IntVar(&opt.assocDepth, "assoc-depth", 2, "associative graph walk depth (maximum 2)")
	flag.BoolVar(&opt.clusterSweep, "cluster-sweep", false, "sweep one-hop entity clusters for enumeration questions")
	flag.BoolVar(&opt.temporalScore, "temporal-score", false, "enable soft temporal retrieval scoring")
	flag.BoolVar(&opt.temporalHardFilter, "temporal-hard-filter", false, "experimental hard temporal candidate filter")
	flag.BoolVar(&opt.conflictResolution, "conflict-resolution", false, "resolve contradictory facts during store build (non-destructive supersede) and downweight superseded entries at retrieval")
	flag.Float64Var(&opt.supersededPenalty, "superseded-penalty", 0.3, "retrieval score multiplier for superseded entries [0,1]; only applies when --conflict-resolution is on")
	flag.BoolVar(&opt.abstainPrompt, "abstain-prompt", false, "use the abstention-oriented answer prompt")
	flag.BoolVar(&opt.forceAnswer, "force-answer", false, "require a best guess instead of an I don't know answer")
	flag.BoolVar(&opt.imageCaptions, "image-captions", false, "fold each turn's blip_caption into its text at ingestion (image-borne facts become retrievable); changes extraction input, so stores built with/without it are not comparable")
	flag.BoolVar(&opt.temporalAnswerPrompt, "temporal-answer-prompt", false, "use the temporal reasoning answer prompt for category 2")
	flag.BoolVar(&opt.lmeTypedPrompts, "lme-typed-prompts", false, "LongMemEval: map question_type to the matching LoCoMo contract (multi-session→multi-hop, temporal-reasoning→temporal); default off, eval-config change")
	flag.BoolVar(&opt.unifiedAnswerContract, "unified-answer-contract", false, "experimental dataset/category-independent evidence-grounded answer contract (default off; isolated scoring also requires --no-idk-retry and --trace-mediation=false)")
	flag.StringVar(&opt.unifiedProbeFixture, "unified-answer-probe", "", "run the dedicated paired generic-vs-unified behavior probe from this fixture JSON and exit")
	flag.StringVar(&opt.unifiedProbeOut, "unified-answer-probe-out", "", "write the paired behavior-probe audit report to this JSON path")
	flag.IntVar(&opt.unifiedProbeRepeats, "unified-answer-probe-repeats", 3, "paired behavior-probe repetitions (must be a positive odd number)")
	flag.BoolVar(&opt.temporalDateScaffold, "temporal-date-scaffold", false, "prepend a deterministic TIMELINE block (sorted dates + computed span) to category-2 answer context; the dates are computed in code rather than left to the model")
	flag.BoolVar(&opt.iris, "iris", false, "enable IRIS evidence-gap iterative retrieval for category-2 temporal questions (sufficiency-driven query refinement; fixed MemOS-aligned budget; harness-only, engine untouched)")
	flag.IntVar(&opt.irisDepth, "iris-depth", 3, "maximum IRIS sufficiency-driven retrieval rounds (including the initial retrieval)")
	flag.BoolVar(&opt.judgeMem0Aligned, "judge-mem0-aligned", false, "use the Mem0-aligned lenient judge rules")
	flag.BoolVar(&opt.rerank, "rerank", false, "apply the cross-encoder rerank stage (needs EMBED_RERANK_MODEL); for paired runs use the hybrid+rerank arm suffix instead")
	flag.BoolVar(&opt.pcic, "pcic", false, "apply the PCIC-lite chunk selector; for paired runs use the +pcic arm suffix instead")
	flag.StringVar(&opt.pcicMetaPath, "pcic-meta", "", "path to the read-only PCIC metadata sidecar (default: <store-dir>/pcic_meta.json or <run-dir>/pcic_meta.json)")
	flag.BoolVar(&opt.abstainProbe, "abstain-probe", false, "run the zero-cost offline abstention probe and exit")
	flag.StringVar(&opt.abstainProbeOut, "abstain-probe-out", "", "path for abstain-probe.json (default: <store-dir|run-dir>/abstain-probe.json)")
	flag.StringVar(&opt.abstainGateSpec, "abstain-gate", "advrecall=0.40,falseabstain=0.05,net=100", "abstention probe gate override: advrecall=FLOAT,falseabstain=FLOAT,net=INT")
	flag.BoolVar(&opt.pcicAnnotate, "pcic-annotate", false, "one-time offline pass: extract per-turn typed claims via the annotation model and write the pcic_meta sidecar, then exit (idempotent: skips when a matching sidecar already exists)")
	flag.StringVar(&opt.pcicFillTurns, "pcic-fill-turns", "", "with --pcic-annotate: re-annotate ONLY these conv-scoped turn keys (comma-separated, e.g. conv-0/D15:1,conv-0/D14:32) and merge into the existing sidecar — pays for exactly those turns")
	flag.StringVar(&opt.catTopKSpec, "cat-top-k", "", `per-category top-k overrides, e.g. "1=150" — multi-hop enumeration questions need evidence from many sessions`)
	flag.StringVar(&opt.catQuotaSpec, "cat-chunk-quota", "", `per-category chunk-quota overrides, e.g. "1=50,4=30"`)
	flag.BoolVar(&opt.opinionPass, "opinion-pass", false, "run a supplementary extraction pass focused on opinions/preferences/traits (ADD-only; run once per store — resuming with this flag duplicates entries)")
	flag.IntVar(&opt.adversarial, "adversarial", 0, "include category-5 adversarial questions, scored by refusal per the Mem0 convention (0 = skip, -1 = all, N = at most N per conversation)")
	flag.StringVar(&opt.storeDir, "store-dir", "", "persist per-conversation stores here and reuse their extraction on re-runs (default in-memory)")
	flag.StringVar(&opt.aliasShadow, "alias-shadow", aliasShadowOff, "alias shadow arm: off | baseline | treatment")
	flag.StringVar(&opt.doc2query, "doc2query", doc2queryOff, "doc2query pseudo-query shadow arm: off | baseline | treatment (store-dir must be a --doc2query-build store)")
	flag.BoolVar(&opt.doc2queryBuild, "doc2query-build", false, "one-time: copy canonical store, LLM-generate pseudo-queries, embed #query shadows into <run-dir>/doc2query-store")
	flag.BoolVar(&opt.coverageOnly, "coverage-only", false, "retrieval-only bake-off: grade every arm on exact-turn / session evidence recall and write coverage.json, making NO answer or judge LLM call (needs --chunks for turn recall)")
	flag.BoolVar(&opt.temporalDiagnostic, "temporal-diagnostic", false, "retrieval-only four-layer temporal recall diagnostic over temporal-category questions (feature 013 US1; needs --store-dir + --data; makes NO answer/judge/extraction LLM call)")
	flag.BoolVar(&opt.attributionTrace, "attribution-trace", false, "retrieval-only per-question attribution trace (requires a persisted store)")
	flag.StringVar(&opt.joinResults, "join-results", "", "archived results JSONL to join by (conv,q) for correctness quadrants")
	flag.BoolVar(&opt.embedProbe, "embed-probe", false, "with --attribution-trace, probe query embedding determinism")
	flag.IntVar(&opt.outrankCap, "outrank-cap", 5, "maximum non-gold hits to record before the first gold hit")
	flag.IntVar(&opt.widePool, "wide-pool", 0, "candidate pool size for gold_in_pool (0 = max(300, top-k*6))")
	flag.Float64Var(&opt.factCoverageTau, "fact-coverage-tau", defaultFactCoverageTau, "attribution: min fraction of a fact's content words that must appear in a gold turn (session-gated) to count as covering it")
	// 030 read-side evidence assembly flags (specs/030). --trace-mediation
	// defaults ON (budget-efficient verified path); the rest default OFF.
	flag.BoolVar(&opt.evidenceAssembly, "evidence-assembly", false, "030 US1: assemble retrieved evidence (exact token accounting + chunk-first + category structure) before answering; off = legacy byte-identical path")
	flag.BoolVar(&opt.assemblyDiagnose, "assembly-diagnose", false, "030 US1 retrieval-only: emit per-question evidence-assembly audit (chunk_fraction / total_tokens / structure / tokens_estimated) to run-dir/assembly-diagnose.jsonl (needs --store-dir + --run-dir + --chunks + --evidence-assembly)")
	flag.BoolVar(&opt.assemblyAudit, "assembly-audit", false, "033 benchmark audit: emit the evidence-assembly receipt from each real answer pass to run-dir/assembly-audit.jsonl (requires --evidence-assembly; write-only)")
	flag.BoolVar(&opt.assemblyLegacyEntityOrder, "assembly-legacy-entity-order", false, "033 benchmark-only: use the pre-repair multi-hop group-major evidence order (requires --evidence-assembly; default false)")
	flag.BoolVar(&opt.traceMediation, "trace-mediation", true, "030 US2: grounded-evidence mediator (plan/trace/actions/evidence via sidecar; fail-closed gate in pure Go); on = default (needs answerer LLM as sidecar; no sidecar degrades to legacy path); set false for the legacy byte-identical path")
	flag.BoolVar(&opt.consolidate, "consolidate", false, "030 US3: conditional consolidation — compress only when evidence exceeds the answer-context cap AND this flag is set; off = retain raw (default)")
	flag.BoolVar(&opt.relationContext, "relation-context", false, "031: append the structural-context relation block (related_to/temporal_next/caused_by) to the assembled or trace-mediated answer context; off = legacy byte-identical path")
	flag.BoolVar(&opt.notebook, "notebook", false, "after the run, capture per-question gold attribution (gold_resolved/candidate_covered/bundle_covered) + accumulate mistakes into the notebook dir (default ./eval-notebook); off = results byte-identical (SC-004)")
	flag.BoolVar(&opt.notebookAdvise, "notebook-advise", false, "with --notebook, ask the answerer LLM to draft 'how to solve this class next time' advice for this run's mistakes (writes advice-<run_id>.md)")
	flag.StringVar(&opt.notebookDir, "notebook-dir", "", "notebook output dir (default ./eval-notebook)")
	flag.BoolVar(&opt.counterRefine, "counter-refine", false, "L2: after the first answer, verify the draft against counter-evidence selected from the retrieved hits and REVISE if better supported (default off; off = results byte-identical)")
	flag.Float64Var(&opt.notebookFactTau, "notebook-fact-tau", defaultNotebookFactTau, "notebook attribution: min fraction of a fact's content words that must appear in a gold turn (session-gated) to count as covering it. Lower than --fact-coverage-tau so the notebook flags 'gold plausibly in context' instead of requiring strict lexical proof; does NOT touch retrieval or the formal protocol")
	flag.BoolVar(&opt.traceMultiEvidence, "trace-multi-evidence", false, "032: relax the trace sidecar to intent-breadth evidence (fact_lookup/preference_recall → 1-2, multi_hop/temporal_state_tracking → 3-6 statements) instead of the legacy single-evidence prompt; off = legacy prompt (SC-004)")
	flag.IntVar(&opt.traceEvidenceCap, "trace-evidence-cap", 0, "with --trace-multi-evidence, hard cap on the number of evidence statements kept (0 = no cap)")
	flag.IntVar(&opt.traceFallbackTopk, "trace-fallback-topk", 0, "compiler-miss guard: if the trace sidecar cites NONE of the retrieval top-k candidates, use the top-k raw candidates as the answer context instead (0 = off)")
	if err := flag.CommandLine.Parse(normalizeCompareArgs(os.Args[1:])); err != nil {
		return err
	}
	opt.explicitFlags = make(map[string]bool)
	flag.CommandLine.Visit(func(f *flag.Flag) {
		opt.explicitFlags[f.Name] = true
	})
	if probeMode, err := validateUnifiedAnswerContractProbeMode(opt); err != nil {
		return err
	} else if probeMode {
		return runUnifiedAnswerContractProbeMode(context.Background(), opt, os.Getenv)
	}
	opt.adjudicationCandidates = append([]string(nil), adjudicationCandidates...)
	if adjudicationMode, err := adjudicationModeFor(opt); err != nil {
		return err
	} else if adjudicationMode != "" {
		return runAdjudicationCLI(context.Background(), opt)
	}
	if err := validateFixedGoldOracleMode(opt); err != nil {
		return err
	}
	if err := validateB0ContinuityMode(opt); err != nil {
		return err
	}
	if err := validatePromptModes(opt); err != nil {
		return err
	}
	if err := validateAssemblyOptions(opt); err != nil {
		return err
	}
	if err := validateAssocDepth(opt.assocDepth); err != nil {
		return err
	}
	if opt.representationArm == "" {
		opt.representationArm = ReprChunk900
	}
	if err := validateMechanismArms(opt); err != nil {
		return err
	}
	if err := validateConfidenceGatedOptions(opt); err != nil {
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
	if opt.evalValidate != "" {
		if b0ContinuityArtifactsPresent(opt.evalValidate) {
			return runB0ContinuityValidateCLI(opt)
		}
		if fixedGoldOracleArtifactsPresent(opt.evalValidate) {
			return runFixedGoldOracleValidateCLI(context.Background(), opt)
		}
		return runEvalArtifactValidateCLI(opt.evalValidate)
	}
	if opt.judgeAuditPrepare != "" {
		opt.runDir = opt.judgeAuditPrepare
		return runJudgeAuditPrepareCLI(opt)
	}
	if opt.judgeAuditFinalize != "" {
		opt.runDir = opt.judgeAuditFinalize
		return runJudgeAuditFinalizeCLI(opt)
	}
	if opt.tokenCounterCalibrate {
		return runFormalTokenCalibrationCLI(opt)
	}
	if opt.dataPath == "" {
		flag.Usage()
		return fmt.Errorf("--data is required")
	}
	if opt.repeats < 1 {
		return fmt.Errorf("--repeats must be at least 1")
	}
	if (opt.multiQuery || (opt.recallDiagnostic && !aliasShadowEnabled(opt) && !doc2queryEnabled(opt))) && opt.mqMaxSubqueries < 1 {
		return fmt.Errorf("--mq-max-subqueries must be at least 1")
	}
	if opt.recallDiagnostic {
		if opt.estimate || opt.attributionTrace || opt.coverageOnly || opt.abstainProbe || opt.pcicAnnotate {
			return fmt.Errorf("--recall-diagnostic cannot be combined with estimate, attribution, coverage-only, abstain-probe, or pcic-annotate modes")
		}
	}
	if opt.navDiagnose {
		if opt.recallDiagnostic || opt.temporalDiagnostic || opt.attributionTrace || opt.coverageOnly || opt.abstainProbe || opt.pcicAnnotate {
			return fmt.Errorf("--nav-diagnose cannot be combined with other retrieval-only diagnostic modes (recall-diagnostic, temporal-diagnostic, attribution, coverage-only, abstain-probe, pcic-annotate)")
		}
	}
	if opt.multiQuery && !opt.recallDiagnostic && (opt.estimate || opt.attributionTrace || opt.coverageOnly || opt.abstainProbe || opt.pcicAnnotate) {
		return fmt.Errorf("--multi-query is supported only by answer/judge runs; use --recall-diagnostic for retrieval-only comparison")
	}
	if opt.datasetFormat != "locomo" && opt.datasetFormat != "longmemeval" {
		return fmt.Errorf("--dataset-format must be locomo or longmemeval, got %q", opt.datasetFormat)
	}
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	if unifiedPromptPairExperimentRequested(opt, arms) {
		if err := validateUnifiedPromptPairExperiment(opt, arms); err != nil {
			return err
		}
		opt.unifiedPairAudit = true
	}
	if opt.evalB0ProtocolPath != "" {
		if opt.evalProtocolPath != "" || opt.fixedGoldOracle {
			return fmt.Errorf("--eval-b0-protocol cannot be combined with B1 or fixed-gold modes")
		}
		protocol, prepared, err := prepareB0ContinuityEvalRun(opt.evalB0ProtocolPath, opt.runDir, opt)
		if err != nil {
			return err
		}
		if err := validateB0ContinuityRunnerOptions(protocol, prepared, arms); err != nil {
			return err
		}
		// Research-subset mode (--only-questions) waives git provenance: the
		// pilot runs a modified harness, so the worktree cannot match the
		// frozen commit. Full-cohort runs keep the strict check.
		if opt.onlyQuestionsFile == "" {
			if err := verifyFormalGitProvenance(protocol); err != nil {
				return err
			}
		}
		opt = prepared
		opt.b0Protocol = &protocol
	}
	if opt.evalProtocolPath != "" {
		if opt.evalFreezeB0Protocol != "" {
			return fmt.Errorf("--eval-protocol cannot be combined with --eval-freeze-b0-protocol")
		}
		protocol, prepared, err := prepareFormalEvalRun(opt.evalProtocolPath, opt.runDir, opt)
		if err != nil {
			return err
		}
		if protocol.Experiment.Stage != "b1" {
			return fmt.Errorf("formal runner currently supports only b1 protocol stage, got %q", protocol.Experiment.Stage)
		}
		if err := validateFormalRunnerOptions(protocol, prepared, arms); err != nil {
			return err
		}
		// Research-subset mode (--only-questions) waives git provenance: the
		// pilot runs a modified harness, so the worktree cannot match the
		// frozen commit. Full-cohort runs keep the strict check.
		if opt.onlyQuestionsFile == "" {
			if err := verifyFormalGitProvenance(protocol); err != nil {
				return err
			}
		}
		opt = prepared
		opt.formalProtocol = &protocol
	}
	if opt.fixedGoldOracle && opt.formalProtocol == nil {
		return fmt.Errorf("--fixed-gold-oracle requires --eval-protocol")
	}
	if opt.multiQuery && !opt.recallDiagnostic && len(arms) != 1 {
		return fmt.Errorf("--multi-query requires exactly one retrieval backend so context_parity.jsonl has one row per question")
	}
	for _, arm := range arms {
		if err := validatePromptModes(optionsForRun(opt, arm, len(arms) > 1)); err != nil {
			return fmt.Errorf("arm %s: %w", arm, err)
		}
		spec, _ := parseArm(arm)
		if (spec.mechanisms["pcic"] || spec.mechanisms["oracle"]) && spec.backend != "hybrid" {
			return fmt.Errorf("arm %s: pcic/oracle selection requires the hybrid backend", arm)
		}
		if spec.mechanisms["oracle"] && !opt.coverageOnly {
			return fmt.Errorf("arm %s: oracle is allowed only with --coverage-only", arm)
		}
	}
	if opt.catTopK, err = parseCatOverrides(opt.catTopKSpec); err != nil {
		return fmt.Errorf("--cat-top-k: %w", err)
	}
	if opt.catQuota, err = parseCatOverrides(opt.catQuotaSpec); err != nil {
		return fmt.Errorf("--cat-chunk-quota: %w", err)
	}
	if err := validateAliasShadowOptions(opt); err != nil {
		return err
	}
	if err := validateDoc2QueryOptions(opt); err != nil {
		return err
	}
	if opt.multiQuery {
		if opt.topK != multiQueryFinalTopK {
			return fmt.Errorf("--multi-query requires --top-k %d to preserve context parity", multiQueryFinalTopK)
		}
		for category, topK := range opt.catTopK {
			if topK != multiQueryFinalTopK {
				return fmt.Errorf("--multi-query requires category %d top-k to remain %d, got %d", category, multiQueryFinalTopK, topK)
			}
		}
		if opt.filterPool > 0 {
			return fmt.Errorf("--filter-pool is not allowed with --multi-query because SearchMulti's final budget must remain %d", multiQueryFinalTopK)
		}
		if opt.chunkQuota > multiQueryFinalTopK {
			return fmt.Errorf("--multi-query requires --chunk-quota at most %d", multiQueryFinalTopK)
		}
		for category, quota := range opt.catQuota {
			if quota > multiQueryFinalTopK {
				return fmt.Errorf("--multi-query requires category %d chunk quota at most %d, got %d", category, multiQueryFinalTopK, quota)
			}
		}
	}
	if opt.abstainGate, err = parseAbstainGate(opt.abstainGateSpec); err != nil {
		return err
	}
	if opt.concurrency < 1 {
		opt.concurrency = 1
	}

	convs, err := loadBenchmarkDataset(opt.dataPath, opt.datasetFormat, opt.imageCaptions)
	if err != nil {
		return err
	}
	if opt.unifiedPairAudit {
		rawDataset, err := os.ReadFile(opt.dataPath)
		if err != nil {
			return fmt.Errorf("bind unified prompt paired dataset bytes: %w", err)
		}
		opt.unifiedPairDatasetDigest = evalTextDigest(string(rawDataset))
	}
	if opt.onlyQuestionsFile != "" {
		ids, err := readQuestionWhitelist(opt.onlyQuestionsFile)
		if err != nil {
			return err
		}
		opt.onlyQuestions = ids
		if err := validateQuestionWhitelistCoverage(convs, opt); err != nil {
			return err
		}
	}
	if opt.formalProtocol != nil || opt.b0Protocol != nil {
		if opt.maxConvs != 0 || opt.maxQuestions != 0 || opt.onlyCategory != 0 || opt.onlyEnumeration || opt.adversarial != 0 {
			return fmt.Errorf("022 B0/B1 evaluation refuses dataset/question sampling")
		}
		// --only-questions is permitted under B0/B1 as a research-subset mode:
		// the run answers only the whitelisted questions, then the terminal
		// materialize/validate step (which asserts full-cohort coverage) errors
		// on the subset — read per-question results from the run journal.
		protocol := opt.formalProtocol
		if protocol == nil {
			protocol = opt.b0Protocol
		}
		if err := verifyFormalDataset(*protocol, opt.dataPath, opt.datasetFormat, convs); err != nil {
			return err
		}
	}
	if opt.evalFreezeB0Protocol != "" {
		return freezeB0ContinuityProtocol(opt, convs)
	}
	if opt.evalFreezeProtocol != "" {
		controlHash := ""
		if formalTreatmentMechanismRequested(opt) {
			if strings.TrimSpace(opt.controlProtocolPath) == "" {
				return fmt.Errorf("treatment freeze requires --control-protocol <frozen B1 manifest>")
			}
			var err error
			if controlHash, err = readEvalProtocolHash(opt.controlProtocolPath); err != nil {
				return err
			}
		}
		return freezeFormalProtocol(opt, convs, controlHash)
	}
	sampledConversations := 0
	if opt.maxConvs > 0 && opt.maxConvs < len(convs) {
		sampledConversations = opt.maxConvs
		convs = convs[:opt.maxConvs]
		if err := validateQuestionWhitelistCoverage(convs, opt); err != nil {
			return err
		}
	}
	if err := prepareAliasShadowStore(&opt); err != nil {
		return err
	}
	if opt.doc2queryBuild {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		apiKey := os.Getenv("LOCOMO_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("LOCOMO_API_KEY is required for --doc2query-build (never passed as a flag so it stays out of process listings)")
		}
		genModel := envOr("EXTRACT_MODEL", envOr("LOCOMO_MODEL", defaultLoCoMoModel))
		prov, err := buildBenchProvider(envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider), apiKey, envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic"), opt.maxTokens, "LOCOMO_PROVIDER")
		if err != nil {
			return err
		}
		genCall := gate(make(chan struct{}, opt.concurrency), newModelCaller(prov, genModel, opt.maxTokens, doc2queryTemperature))
		embClient := buildBenchEmbeddingClient(logger, nil)
		return runDoc2QueryBuild(context.Background(), opt, convs, genCall, embClient, logger)
	}
	if err := prepareDoc2QueryStore(&opt); err != nil {
		return err
	}
	if opt.temporalDiagnostic {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if strings.TrimSpace(opt.storeDir) == "" {
			return fmt.Errorf("--temporal-diagnostic requires --store-dir (the prebuilt bge-large store)")
		}
		if strings.TrimSpace(opt.runDir) == "" {
			return fmt.Errorf("--temporal-diagnostic requires --run-dir")
		}
		if sampledConversations > 0 {
			logger.Info("sampling conversations", "limit", sampledConversations)
		}
		// Retrieval-only: build only the embedding client (never an answer/judge
		// caller). The extractNever guard inside runTemporalDiagnostic asserts
		// extraction is never called.
		var embClient embedding.Client
		if hasArm(arms, "hybrid") {
			embClient = buildBenchEmbeddingClient(logger, nil)
		}
		return runTemporalDiagnostic(context.Background(), opt, convs, embClient, logger)
	}
	if opt.navDiagnose {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if sampledConversations > 0 {
			logger.Info("sampling conversations", "limit", sampledConversations)
		}
		// Retrieval-only: build only the embedding client (never an
		// answer/judge/extraction caller).
		var embClient embedding.Client
		if hasArm(arms, "hybrid") {
			embClient = buildBenchEmbeddingClient(logger, nil)
		}
		return runNavDiagnoseCLI(context.Background(), opt, convs, arms, embClient, logger)
	}
	if opt.assemblyDiagnose {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if sampledConversations > 0 {
			logger.Info("sampling conversations", "limit", sampledConversations)
		}
		// Retrieval-only: build only the embedding client (never an
		// answer/judge/extraction caller); the exact tokenizer is optional.
		var embClient embedding.Client
		if hasArm(arms, "hybrid") {
			embClient = buildBenchEmbeddingClient(logger, nil)
		}
		return runAssemblyDiagnoseCLI(context.Background(), opt, convs, arms, embClient, logger)
	}
	if opt.recallDiagnostic {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if sampledConversations > 0 {
			logger.Info("sampling conversations", "limit", sampledConversations)
		}
		return runRecallDiagnosticCLI(context.Background(), opt, convs, arms, logger)
	}
	if opt.attributionTrace {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if sampledConversations > 0 {
			logger.Info("sampling conversations", "limit", sampledConversations)
		}
		return runAttributionCLI(context.Background(), opt, convs, arms, logger)
	}
	prices, err := parsePriceTable(os.Getenv("LOCOMO_PRICE_TABLE"))
	if err != nil {
		return err
	}
	model := envOr("LOCOMO_MODEL", defaultLoCoMoModel)
	extractModel := envOr("EXTRACT_MODEL", model)
	answerProvider := envOr("LOCOMO_PROVIDER", defaultLoCoMoProvider)
	judgeConfig := resolveJudgeConfig(os.Getenv)
	opt.answerModel = model
	opt.judgeModel = judgeConfig.Model
	if opt.formalProtocol != nil || opt.b0Protocol != nil {
		protocol := opt.formalProtocol
		if protocol == nil {
			protocol = opt.b0Protocol
		}
		answerRevision := envOr("LOCOMO_MODEL_REVISION", model)
		judgeRevision := envOr("JUDGE_MODEL_REVISION", judgeConfig.Model)
		extractorRevision := envOr("EXTRACT_MODEL_REVISION", extractModel)
		if protocol.Models.Answerer.ID != model ||
			protocol.Models.Answerer.Revision != answerRevision ||
			protocol.Models.Answerer.Provider != answerProvider ||
			protocol.Models.Judge.ID != judgeConfig.Model ||
			protocol.Models.Judge.Revision != judgeRevision ||
			protocol.Models.Judge.Provider != judgeConfig.Provider ||
			(!opt.fixedGoldOracle &&
				(protocol.Models.Extractor.ID != extractModel ||
					protocol.Models.Extractor.Revision != extractorRevision ||
					protocol.Models.Extractor.Provider != answerProvider)) {
			return fmt.Errorf("022 model providers, IDs, or revisions differ from frozen protocol")
		}
	}
	if opt.estimate {
		return printEstimate(convs, opt, prices, model, extractModel, judgeConfig.Model)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if sampledConversations > 0 {
		logger.Info("sampling conversations", "limit", sampledConversations)
	}
	if opt.pcicAnnotate {
		apiKey := os.Getenv("LOCOMO_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("LOCOMO_API_KEY is required (never passed as a flag so it stays out of process listings)")
		}
		return runPCICAnnotate(opt, convs, apiKey, envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic"), logger)
	}
	if opt.runDir == "" && !opt.abstainProbe && !opt.probeHesitation {
		return fmt.Errorf("--run-dir is required unless --estimate or --compare is used")
	}
	if opt.runDir != "" && !opt.abstainProbe {
		if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
			return fmt.Errorf("create run dir: %w", err)
		}
	}
	if pcicEnabledForRun(opt, arms) || opt.abstainProbe {
		metaPath := opt.pcicMetaPath
		if metaPath == "" {
			baseDir := opt.storeDir
			if baseDir == "" {
				baseDir = opt.runDir
			}
			metaPath = filepath.Join(baseDir, "pcic_meta.json")
		}
		fingerprint, err := pcicDatasetFingerprint(opt.dataPath)
		if err != nil {
			return err
		}
		opt.pcicMeta, err = loadPCICMeta(metaPath, PCICMetaHeader{
			AnnotateModel:      envOr("PCIC_ANNOTATE_MODEL", "gpt-5.6-luna"),
			DatasetFingerprint: fingerprint,
		}, logger)
		if err != nil {
			return err
		}
		if opt.pcicMeta == nil {
			logger.Warn("pcic_meta unavailable; selector will use rerank order", "path", metaPath)
		}
	}
	if opt.abstainProbe {
		return runAbstainProbeCLI(context.Background(), opt, convs, arms, logger)
	}
	if opt.probeHesitation {
		// 041 US1: zero-cost offline discrimination probe over an existing
		// results-hybrid.jsonl. Needs no LLM/retriever.
		return runHesitationProbeCLI(opt)
	}
	apiKey := os.Getenv("LOCOMO_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LOCOMO_API_KEY is required (never passed as a flag so it stays out of process listings)")
	}
	baseURL := envOr("LOCOMO_BASE_URL", "https://api.deepseek.com/anthropic")
	if opt.formalProtocol != nil {
		if strings.TrimSpace(opt.tokenCounterBaseURL) == "" {
			return fmt.Errorf("formal 022 evaluation requires --token-counter-base-url")
		}
	}
	if !opt.coverageOnly {
		// The regime pin guards answer-journal resume from mixing 口径; coverage
		// writes no journal, so it has no regime to protect.
		if opt.unifiedPairAudit {
			if err := requireFreshUnifiedPromptPairRunDir(opt.runDir); err != nil {
				return err
			}
		}
		if err := checkRunDirRegime(opt); err != nil {
			return err
		}
	}
	// A global semaphore caps concurrent in-flight LLM calls so many
	// conversations/questions run in parallel without exceeding the rate limit.
	// Answer-side calls share prov; judge calls use judgeProv so the two endpoint
	// configurations can be changed independently.
	// Provider protocol is selectable so the harness can target either an
	// Anthropic-messages endpoint (default) or an OpenAI-chat-completions one
	// (LOCOMO_PROVIDER=openai). Both satisfy provider.Provider identically.
	prov, err := buildBenchProvider(answerProvider, apiKey, baseURL, opt.maxTokens, "LOCOMO_PROVIDER")
	if err != nil {
		return err
	}
	judgeProv, err := buildBenchProvider(judgeConfig.Provider, judgeConfig.APIKey, judgeConfig.BaseURL, opt.maxTokens, "JUDGE_PROVIDER")
	if err != nil {
		return err
	}
	if opt.compilerArm == "planner" {
		// A configured planner arm consumes a self-hosted sidecar through the
		// same provider abstraction as the answerer/judge. Without a sidecar
		// config the planner stays nil and the compiler degrades to the
		// deterministic extractive fallback (023 FR-019) — a hard error here
		// would break the frozen 026/027 "planner arm runs as extractive"
		// behavior.
		if strings.TrimSpace(opt.plannerBaseURL) != "" && strings.TrimSpace(opt.plannerModel) != "" {
			plannerProv, err := buildBenchProvider("openai", apiKey, opt.plannerBaseURL, opt.maxTokens, "PLANNER_PROVIDER")
			if err != nil {
				return fmt.Errorf("configure planner provider: %w", err)
			}
			lp, err := newLocalPlanner(localPlannerConfig{
				Provider: plannerProv, Model: opt.plannerModel,
				MaxTokens: opt.maxTokens, Timeout: opt.plannerTimeout,
			})
			if err != nil {
				return fmt.Errorf("configure local planner: %w", err)
			}
			opt.planner = lp
		} else {
			logger.Warn("--compiler-arm planner without --planner-base-url/--planner-model; degrading to deterministic extractive fallback")
		}
	}
	sem := make(chan struct{}, opt.concurrency)
	if opt.formalProtocol != nil {
		counter, err := newVLLMTokenCounter(vllmTokenCounterConfig{
			BaseURL: opt.tokenCounterBaseURL, APIKey: apiKey, Fingerprint: opt.formalProtocol.Budget.CounterFingerprint,
		})
		if err != nil {
			return fmt.Errorf("configure formal token counter: %w", err)
		}
		opt.formalCounter = gateTokenCounter(make(chan struct{}, formalTokenCounterLimit(opt.concurrency)), counter)
		opt.formalQuestionGate = make(chan struct{}, opt.concurrency)
	}
	ledger := newCostLedger(prices)
	recordUsage := func(role, model string, usage provider.Usage) {
		recordBenchUsage(ledger, role, model, usage)
	}
	answerProviderCall := newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "answer", recordUsage)
	judgeProviderCall := newUsageModelCallerWithUsage(judgeProv, judgeConfig.Model, opt.maxTokens, "judge", recordUsage)
	answerUsageCall := gateUsage(sem, answerProviderCall)
	judgeUsageCall := gateUsage(sem, judgeProviderCall)
	if opt.unifiedPairAudit {
		// The paired audit records wrapper calls. Keep provider attempts at one so
		// a transient first failure cannot be hidden by gateUsage's retry.
		answerUsageCall = gateUsageOnce(sem, answerProviderCall)
		judgeUsageCall = gateUsageOnce(sem, judgeProviderCall)
	}
	if opt.nav {
		// 029 navigation decide caller: direct vLLM HTTP with thinking disabled
		// for fast, pure-JSON tool calls (Qwen3.6 reasoning model otherwise
		// emits chain-of-thought and runs ~20x slower per step). Same endpoint
		// and model as the answerer; harness-side only.
		navDecide, navErr := newNavDecideCaller(navDecideConfig{
			BaseURL: baseURL, APIKey: apiKey, Model: model,
		})
		if navErr != nil {
			logger.Warn("nav decide caller unavailable; navigation falls back to answerCall", "err", navErr)
		} else {
			opt.navDecideCall = navDecide
		}
	}
	if opt.evidenceAssembly || opt.assemblyDiagnose {
		// 030 US1 exact tokenizer for evidence assembly (specs/030, research
		// decision 1). Reuses the formal 022 /tokenize counter config so the
		// assembly's TotalTokens is the same chat-aware count the answerer
		// consumes. Unavailable → estimate-ledger fallback (tokens_estimated).
		counter, counterErr := newVLLMTokenCounter(vllmTokenCounterConfig{
			BaseURL: opt.tokenCounterBaseURL, APIKey: apiKey, Fingerprint: opt.counterFingerprint,
		})
		if counterErr != nil {
			logger.Warn("assembly exact tokenizer unavailable; falling back to estimate ledger", "err", counterErr)
		} else {
			opt.assemblyCounter = &assemblyTokenCounter{counter: counter, answerModel: model}
		}
	}
	if opt.traceMediation {
		// 030 US2 grounded-trace generator (specs/030). Reuses the answerer's
		// vLLM endpoint/model; harness-side only. Unavailable → legacy path.
		traceCall, traceErr := newTraceSidecarCaller(traceSidecarConfig{
			BaseURL: baseURL, APIKey: apiKey, Model: model,
		})
		if traceErr != nil {
			logger.Warn("trace sidecar caller unavailable; --trace-mediation (default on) degrades to the legacy byte-identical path", "err", traceErr)
		} else {
			// Bind the bench usage ledger so the trace sidecar's token spend is
			// visible in cost.json (previously discarded at the call site,
			// silently under-reporting the trace arm).
			base := traceCall
			opt.traceSidecarCaller = func(ctx context.Context, system, user string) (string, provider.Usage, error) {
				text, usage, err := base(ctx, system, user)
				if usage.InputTokens > 0 || usage.OutputTokens > 0 {
					recordUsage("trace", model, usage)
				}
				return text, usage, err
			}
		}
	}
	if opt.consolidate {
		// 030 US3 compression generator (specs/030). Reuses the same harness-side
		// JSON caller as the trace sidecar; unavailable → deterministic
		// truncation on over-cap (the legacy behaviour).
		consCall, consErr := newTraceSidecarCaller(traceSidecarConfig{
			BaseURL: baseURL, APIKey: apiKey, Model: model,
		})
		if consErr != nil {
			logger.Warn("consolidation sidecar unavailable; over-cap degrades to truncation", "err", consErr)
		} else {
			base := consCall
			opt.consolidateCall = func(ctx context.Context, system, user string) (string, provider.Usage, error) {
				text, usage, err := base(ctx, system, user)
				if usage.InputTokens > 0 || usage.OutputTokens > 0 {
					recordUsage("consolidate", model, usage)
				}
				return text, usage, err
			}
		}
	}
	if opt.formalProtocol != nil {
		// Formal call counts are provider-attempt counts. Transparent retries
		// would make the persisted one-call contract untrue.
		answerUsageCall = gateUsageOnce(sem, answerProviderCall)
		judgeUsageCall = gateUsageOnce(sem, judgeProviderCall)
	}
	filterCall := modelCallerFromUsage(gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "filter", recordUsage)))
	rewriteCall := modelCallerFromUsage(gateUsage(sem, newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "rewrite", recordUsage)))
	extractCall := pipeline.ModelCaller(gate(sem, newModelCallerWithUsage(prov, extractModel, opt.maxTokens, "extract", recordUsage)))

	if opt.fixedGoldOracle {
		// The fixed-gold oracle is diagnostic-only — never a formal score arm — so
		// its answer/judge calls may transparently retry once to absorb transient
		// infrastructure failures. Without this, one dead relay / vllm hiccup on a
		// single question fails the entire fail-closed run (observed 2026-08-03:
		// the same input succeeded on repetition 1 and failed on repetition 2).
		// B1's formal one-call contract above is untouched.
		answerUsageCall = gateUsage(sem, answerProviderCall)
		judgeUsageCall = gateUsage(sem, judgeProviderCall)
		summary, err := runFixedGoldOracleDataset(
			context.Background(), *opt.formalProtocol, opt, convs, answerUsageCall, judgeUsageCall,
		)
		if err != nil {
			return err
		}
		fmt.Printf(
			"fixed-gold oracle: correct=%d/%d target=%d met=%t diagnostic_only=true\n",
			summary.OracleDiagnostic.Correct,
			summary.OracleDiagnostic.Denominator,
			summary.OracleDiagnostic.TargetCorrect,
			summary.OracleDiagnostic.TargetMet,
		)
		return nil
	}

	// The embedding client is shared across conversations (safe for concurrent
	// use) and only built when a hybrid arm is present.
	var embClient embedding.Client
	if hasArm(arms, "hybrid") {
		embClient = buildBenchEmbeddingClient(logger, func(inputTokens, outputTokens int) {
			ledger.Add("embed", envOr("EMBED_MODEL", "qwen3-embedding:0.6b"), inputTokens, outputTokens)
		})
		if opt.unifiedPairAudit && embClient == nil {
			return fmt.Errorf("unified prompt paired experiment requires a configured embedding endpoint; refusing silent hybrid degradation")
		}
	}

	logger.Info("starting", "conversations", len(convs), "arms", arms, "concurrency", opt.concurrency,
		"model", model, "extract_model", extractModel, "judge_base_url_host", baseURLHost(judgeConfig.BaseURL),
		"judge_model", judgeConfig.Model, "top_k", opt.topK)

	ctx := context.Background()
	storeDir := opt.storeDir
	if storeDir == "" {
		storeDir = filepath.Join(opt.runDir, ".stores")
	}
	buildOpt := opt
	buildOpt.storeDir = storeDir
	runtimes := make([]*conversationRuntime, len(convs))
	// Bound the store-build phase: each conversation opens its own SQLite handle
	// and ingests verbatim chunks; unbounded N-parallel opens convoy on the
	// modernc SQLite mutex and stall (LME 500/200 stall, 50 fine). A modest cap
	// keeps build throughput high without the lock convoy.
	const buildConcurrency = 16
	buildSem := make(chan struct{}, buildConcurrency)
	var buildWG sync.WaitGroup
	var buildMu sync.Mutex
	var buildErr error
	for ci := range convs {
		buildWG.Add(1)
		go func(index int) {
			defer buildWG.Done()
			buildSem <- struct{}{}
			defer func() { <-buildSem }()
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
	// 024 write-time redundancy suppression audit (FR-005 / SC-001): aggregate
	// per-conversation suppression counters into a run artifact so a write_dedup
	// arm's mis-suppression rate is assessable without re-running the build.
	if opt.writeDedup {
		if err := writeSuppressionAudit(filepath.Join(opt.runDir, "suppression-audit.json"), runtimes); err != nil {
			for _, runtime := range runtimes {
				runtime.Close()
			}
			return err
		}
	}
	defer func() {
		for _, runtime := range runtimes {
			runtime.Close()
		}
	}()

	// 027 write-side event projection: load it once when the event representation
	// is requested (before any answer/judge work), and build it on demand via
	// --build-event-project.
	if opt.buildEventProjectOut != "" {
		if err := runBuildEventProject(ctx, opt, convs, runtimes, logger); err != nil {
			return err
		}
		return nil
	}
	if opt.representationArm == ReprEvent {
		if strings.TrimSpace(opt.eventProjectPath) == "" {
			return fmt.Errorf("--representation event requires --event-project")
		}
		proj, err := eventstore.LoadProject(opt.eventProjectPath)
		if err != nil {
			return fmt.Errorf("load event project %q: %w", opt.eventProjectPath, err)
		}
		opt.eventProject = proj
		logger.Info("event projection loaded", "path", opt.eventProjectPath, "events", len(proj.Events))
	}

	if opt.coverageOnly {
		// Retrieval-only bake-off: no answer/judge tokens are spent, so the only
		// cost is the one-time store build (reusable via --store-dir) plus query
		// embeddings from the local sidecar. Skips the repeat/paired/stats/cost
		// answer machinery entirely.
		return runCoverage(ctx, opt, convs, runtimes, arms, logger)
	}

	var formalReplay *formalQuestionReplay
	var formalCalls *formalCallJournal
	formalJournalsClosed := false
	if opt.formalProtocol != nil {
		formalReplay, err = openFormalQuestionReplay(opt.runDir, opt.formalProtocol.ProtocolHash)
		if err != nil {
			return fmt.Errorf("open formal question replay: %w", err)
		}
		formalCalls, err = openFormalCallJournal(opt.runDir, opt.formalProtocol.ProtocolHash)
		if err != nil {
			_ = formalReplay.Close()
			return fmt.Errorf("open formal call journal: %w", err)
		}
		opt.formalReplay = formalReplay
		opt.formalCalls = formalCalls
		defer func() {
			if !formalJournalsClosed {
				_ = formalCalls.Close()
				_ = formalReplay.Close()
			}
		}()
	}

	for repeat := 1; repeat <= opt.repeats; repeat++ {
		if opt.unifiedPairAudit {
			if retrying, ok := embClient.(*retryingEmbedder); ok && retrying.exhausted.Load() != 0 {
				return fmt.Errorf("unified prompt paired experiment observed %d exhausted embedding calls before repetition %d; refusing to score a degraded hybrid run", retrying.exhausted.Load(), repeat)
			}
		}
		repeatOpt := opt
		repeatOpt.formalRunIndex = repeat
		if opt.repeats > 1 {
			repeatOpt.runDir = filepath.Join(opt.runDir, fmt.Sprintf("run-%d", repeat))
		}
		if err := os.MkdirAll(repeatOpt.runDir, 0o755); err != nil {
			return fmt.Errorf("create repeat run dir: %w", err)
		}
		if opt.nav {
			traj, err := openNavTrajectoryJournal(filepath.Join(repeatOpt.runDir, "nav-trajectories.jsonl"))
			if err != nil {
				return fmt.Errorf("open nav-trajectories.jsonl: %w", err)
			}
			repeatOpt.navTraj = traj
			defer func() { _ = traj.Close() }()
		}
		if opt.confidenceGated {
			cgj, err := openConfidenceGateJournal(filepath.Join(repeatOpt.runDir, "conf_gate_decisions.jsonl"))
			if err != nil {
				return fmt.Errorf("open conf_gate_decisions.jsonl: %w", err)
			}
			repeatOpt.confidenceGateJournal = cgj
			defer func() { _ = cgj.Close() }()
		}
		if opt.assemblyAudit {
			j, err := openAssemblyJournal(filepath.Join(repeatOpt.runDir, "assembly-audit.jsonl"))
			if err != nil {
				return fmt.Errorf("open assembly-audit.jsonl: %w", err)
			}
			repeatOpt.assemblyJournal = j
			defer func() { _ = j.Close() }()
		}
		if opt.traceMediation {
			tj, err := openTraceGateJournal(filepath.Join(repeatOpt.runDir, "trace-gate.jsonl"))
			if err != nil {
				return fmt.Errorf("open trace-gate.jsonl: %w", err)
			}
			repeatOpt.traceGateJournal = tj
			defer func() { _ = tj.Close() }()
		}
		parity, err := openContextParityJournal(repeatOpt.runDir)
		if err != nil {
			return err
		}
		repeatOpt.contextParity = parity
		states := make([]*armState, 0, len(arms))
		for _, name := range arms {
			j, err := openJournal(repeatOpt.runDir, name)
			if err != nil {
				_ = parity.Close()
				return err
			}
			states = append(states, &armState{name: name, agg: newAggregator(), journal: j})
		}
		if repeatOpt.formalCalls != nil {
			if len(states) != 1 {
				for _, state := range states {
					state.journal.Close()
				}
				_ = parity.Close()
				return fmt.Errorf("formal run requires exactly one result journal")
			}
			if err := repeatOpt.formalCalls.Reconcile(repeat, states[0].journal); err != nil {
				for _, state := range states {
					state.journal.Close()
				}
				_ = parity.Close()
				return fmt.Errorf("reconcile formal call journal before repetition %d: %w", repeat, err)
			}
		}
		if repeat == 1 && repeatOpt.formalReplay != nil {
			if err := seedFormalQuestionReplay(repeatOpt.formalReplay, states[0].journal); err != nil {
				for _, state := range states {
					state.journal.Close()
				}
				_ = parity.Close()
				return fmt.Errorf("seed formal question replay: %w", err)
			}
		}
		if err := validateContextParityResume(repeatOpt, convs, states); err != nil {
			for _, state := range states {
				state.journal.Close()
			}
			_ = parity.Close()
			return err
		}
		var wg sync.WaitGroup
		var repeatErrMu sync.Mutex
		var repeatErr error
		// Bound the answer phase like the build phase: per-conversation retrieval
		// hits the SQLite store concurrently, and unbounded N-parallel retrieval
		// convoys on the modernc SQLite mutex (same stall as build). Reuse the
		// LLM concurrency as the retrieval ceiling.
		ansSem := make(chan struct{}, opt.concurrency)
		for ci := range convs {
			wg.Add(1)
			go func(conv conversation, current []*armState) {
				defer wg.Done()
				ansSem <- struct{}{}
				defer func() { <-ansSem }()
				index := conv.ID
				if index < 0 || index >= len(runtimes) || runtimes[index] == nil {
					logger.Warn("conversation runtime unavailable", "conversation", conv.ID)
					if repeatOpt.formalProtocol != nil {
						repeatErrMu.Lock()
						if repeatErr == nil {
							repeatErr = fmt.Errorf("conversation %d runtime unavailable", conv.ID)
						}
						repeatErrMu.Unlock()
					}
					return
				}
				if err := answerConversationWithUsage(ctx, repeatOpt, conv, runtimes[index], answerUsageCall, filterCall, rewriteCall, judgeUsageCall, current, logger); err != nil {
					logger.Warn("conversation failed", "conversation", conv.ID, "err", err)
					repeatErrMu.Lock()
					if repeatErr == nil {
						repeatErr = fmt.Errorf("conversation %d: %w", conv.ID, err)
					}
					repeatErrMu.Unlock()
				}
			}(convs[ci], states)
		}
		wg.Wait()
		if repeatOpt.formalCalls != nil {
			if err := repeatOpt.formalCalls.Reconcile(repeat, states[0].journal); err != nil {
				repeatErrMu.Lock()
				if repeatErr == nil {
					repeatErr = fmt.Errorf("reconcile formal call journal after repetition %d: %w", repeat, err)
				}
				repeatErrMu.Unlock()
			}
		}
		for _, state := range states {
			state.journal.Close()
		}
		if err := parity.Close(); err != nil {
			return err
		}
		repeatErrMu.Lock()
		currentRepeatErr := repeatErr
		repeatErrMu.Unlock()
		if currentRepeatErr != nil {
			return currentRepeatErr
		}
		if repeatOpt.unifiedPairAudit {
			if retrying, ok := embClient.(*retryingEmbedder); ok && retrying.exhausted.Load() != 0 {
				return fmt.Errorf("unified prompt paired experiment observed %d exhausted embedding calls in repetition %d; refusing to score a degraded hybrid run", retrying.exhausted.Load(), repeat)
			}
			receipt, validationErr := validateUnifiedPromptPairRepeat(repeatOpt.runDir, repeatOpt, convs, arms)
			if err := writeUnifiedPromptPairValidationReceipt(repeatOpt.runDir, receipt, validationErr); err != nil {
				return fmt.Errorf("write unified prompt pair validation receipt: %w", err)
			}
			if validationErr != nil {
				return fmt.Errorf("unified prompt pair validation failed before scoring: %w", validationErr)
			}
		}
		for _, state := range states {
			if repeatOpt.unifiedPairAudit {
				fmt.Printf("unified prompt repetition=%d arm=%s recorded=%d score=pending-all-repeat-validation\n",
					repeat, state.name, state.journal.count())
			} else if formalRepeatScoresVisible(repeatOpt) {
				report(state, repeatOpt)
			} else {
				fmt.Printf("formal repetition=%d recorded=%d/%d score=pending-validation\n",
					repeat, state.journal.count(), repeatOpt.formalProtocol.Benchmark.QuestionCount)
			}
		}
		if len(states) == 2 && !repeatOpt.unifiedPairAudit {
			reportDelta(states[0], states[1])
		}
	}
	if formalCalls != nil {
		if err := formalCalls.Close(); err != nil {
			return err
		}
	}
	if formalReplay != nil {
		if err := formalReplay.Close(); err != nil {
			return err
		}
	}
	formalJournalsClosed = true
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
	if opt.b0Protocol != nil {
		runs, err := loadArmRuns(opt.runDir, arms[0], opt.repeats)
		if err != nil {
			return fmt.Errorf("load B0 continuity runs: %w", err)
		}
		summary, err := materializeB0ContinuitySummary(
			opt.runDir, *opt.b0Protocol, runs, formalQuestionIDs(opt.datasetFormat, convs),
		)
		if err != nil {
			return err
		}
		fmt.Printf(
			"B0 continuity: majority_correct=%d/%d answer_calls=%d rewrite_calls=%d judge_calls=%d promotion_eligible=false\n",
			summary.MajorityCorrect, summary.Denominator, summary.AnswerCalls,
			summary.RewriteCalls, summary.JudgeCalls,
		)
	}
	if opt.formalProtocol != nil {
		repeatDirs := formalRepeatRunDirs(opt.runDir, opt.repeats)
		formalRuns, err := loadFormalQuestionRuns(repeatDirs, arms[0])
		if err != nil {
			return err
		}
		summary, err := materializeFormalB1Artifacts(opt.runDir, *opt.formalProtocol, formalRuns)
		if err != nil {
			return fmt.Errorf("materialize formal 022 artifacts: %w", err)
		}
		if !summary.Validity.isComplete() {
			return fmt.Errorf("formal 022 artifact validity failed; summary preserved at %s", filepath.Join(opt.runDir, evalSummaryArtifactFile))
		}
		expectedQuestionIDs := formalQuestionIDs(opt.datasetFormat, convs)
		validated, err := validateEvalArtifactRun(opt.runDir, *opt.formalProtocol, expectedQuestionIDs)
		if err != nil {
			return fmt.Errorf("validate formal 022 artifacts: %w", err)
		}
		if _, err := publishFormalB1Metrics(opt.runDir, validated, *opt.formalProtocol); err != nil {
			return fmt.Errorf("publish formal 022 metrics: %w", err)
		}
	}
	ledger.EstimatedUSD = estimateDatasetCost(convs, opt, prices, model, extractModel, judgeConfig.Model)
	if err := writeCost(filepath.Join(opt.runDir, "cost.json"), ledger.Report()); err != nil {
		return fmt.Errorf("write cost.json: %w", err)
	}
	runsByArm := make(map[string][][]result, len(arms))
	for _, arm := range arms {
		runs, err := loadArmRuns(opt.runDir, arm, opt.repeats)
		if err != nil {
			return err
		}
		runsByArm[arm] = runs
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
	if frontier, complete, err := frontierFromRuns(arms, runsByArm); err != nil {
		return fmt.Errorf("build frontier: %w", err)
	} else if complete {
		if err := writeFrontier(filepath.Join(opt.runDir, "frontier.json"), frontier); err != nil {
			return fmt.Errorf("write frontier.json: %w", err)
		}
	}
	fmt.Printf("cost: actual_usd=%.6f %s\n", ledger.ActualUSD(), formatBudgetSummary(ledger.AnswerContextTokensMean(), opt.budgetBaseline))
	if opt.notebook {
		if err := mountNotebook(ctx, opt, prov, model, logger); err != nil {
			return err
		}
	}
	return nil
}

// mountNotebook accumulates the run's per-question attribution + mistakes into
// the notebook dir (default ./eval-notebook), writes the markdown mistake book,
// and optionally drafts "how to solve this class" advice via the answerer LLM.
func mountNotebook(ctx context.Context, opt options, prov provider.Provider, model string, logger *slog.Logger) error {
	notebookDir := opt.notebookDir
	if notebookDir == "" {
		notebookDir = "eval-notebook"
	}
	runID := notebookRunID(opt.runDir)
	importedAt := time.Now()
	var advisor func(context.Context, string) (string, error)
	if opt.notebookAdvise {
		call := newUsageModelCallerWithUsage(prov, model, opt.maxTokens, "notebook-advise", nil)
		gated := gateUsage(make(chan struct{}, 4), call)
		advisor = func(ctx context.Context, prompt string) (string, error) {
			text, _, err := gated(ctx, notebookAdviseSystemPrompt, prompt)
			return text, err
		}
	}
	summary, err := writeNotebook(ctx, opt, runID, importedAt, notebookDir, advisor)
	if err != nil {
		return fmt.Errorf("write notebook: %w", err)
	}
	acc := 0.0
	if summary.Total > 0 {
		acc = 100 * float64(summary.Correct) / float64(summary.Total)
	}
	fmt.Printf("notebook: run %s → %d/%d (%.2f%%) at %s\n", runID, summary.Correct, summary.Total, acc, notebookDir)
	return nil
}

// runPCICAnnotate is the one-time offline `--pcic-annotate` pass. It extracts
// per-turn typed claims through the annotation model and writes the pcic_meta
// sidecar, touching no engine store. It is idempotent: a sidecar whose header
// already matches the annotation model + dataset fingerprint is a cache hit and
// the pass exits without spending tokens.
func runPCICAnnotate(opt options, convs []conversation, apiKey, baseURL string, logger *slog.Logger) error {
	model := envOr("PCIC_ANNOTATE_MODEL", "gpt-5.6-luna")
	fingerprint, err := pcicDatasetFingerprint(opt.dataPath)
	if err != nil {
		return err
	}
	metaPath := opt.pcicMetaPath
	if metaPath == "" {
		baseDir := opt.storeDir
		if baseDir == "" {
			baseDir = opt.runDir
		}
		if baseDir == "" {
			return fmt.Errorf("--pcic-annotate needs --pcic-meta, --store-dir, or --run-dir to place the sidecar")
		}
		metaPath = filepath.Join(baseDir, "pcic_meta.json")
	}
	expected := PCICMetaHeader{AnnotateModel: model, DatasetFingerprint: fingerprint}
	// The full-pass cache-hit short-circuit must NOT fire in fill mode: gap-fill
	// exists precisely to patch turns into an already-written sidecar.
	if opt.pcicFillTurns == "" {
		if existing, err := loadPCICMeta(metaPath, expected, logger); err == nil && existing != nil {
			logger.Info("pcic_meta cache hit; annotation skipped", "path", metaPath, "spans", len(existing.Spans))
			return nil
		}
	}

	// Build one gated caller per endpoint. LOCOMO_BASE_URL_FALLBACK (optional)
	// is a backup relay base URL: when the primary returns a transient error
	// mid-pass (e.g. the relay's upstream model backend 502s for a window), the
	// annotation falls over to the backup instead of skipping the span. Both
	// share the credential and the semaphore.
	sem := make(chan struct{}, opt.concurrency)
	primaryProv, err := buildAnnotateProvider(apiKey, baseURL, opt.maxTokens)
	if err != nil {
		return err
	}
	callers := []modelCaller{gate(sem, newModelCaller(primaryProv, model, opt.maxTokens))}
	if fallbackURL := os.Getenv("LOCOMO_BASE_URL_FALLBACK"); fallbackURL != "" {
		fallbackProv, err := buildAnnotateProvider(apiKey, fallbackURL, opt.maxTokens)
		if err != nil {
			return err
		}
		callers = append(callers, gate(sem, newModelCaller(fallbackProv, model, opt.maxTokens)))
		logger.Info("pcic annotation failover enabled", "fallback", fallbackURL)
	}
	call := failoverModelCaller(callers...)

	// Targeted gap-fill: patch only the requested turns into the existing sidecar
	// (e.g. the handful a transient relay blip left unannotated) — never re-pay
	// for the whole dataset.
	if opt.pcicFillTurns != "" {
		keys := strings.Split(opt.pcicFillTurns, ",")
		existing, err := loadPCICMeta(metaPath, PCICMetaHeader{AnnotateModel: model, DatasetFingerprint: fingerprint}, logger)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("--pcic-fill-turns needs an existing matching sidecar at %s", metaPath)
		}
		logger.Info("pcic fill starting", "turns", len(keys), "path", metaPath)
		meta, filled, missing, err := fillPCICMeta(context.Background(), convs, *existing, keys, call, logger)
		if err != nil {
			return err
		}
		if err := savePCICMeta(metaPath, meta); err != nil {
			return err
		}
		logger.Info("pcic fill complete", "filled", filled, "missing", missing, "spans", len(meta.Spans))
		if len(missing) > 0 {
			return fmt.Errorf("pcic fill left %d turn(s) unfilled: %v", len(missing), missing)
		}
		return nil
	}

	logger.Info("pcic annotation starting", "model", model, "conversations", len(convs), "path", metaPath)
	meta, err := annotatePCICMeta(context.Background(), convs, model, fingerprint, call, opt.concurrency, logger)
	if err != nil {
		return err
	}
	if err := savePCICMeta(metaPath, meta); err != nil {
		return err
	}
	logger.Info("pcic_meta written", "path", metaPath, "spans", len(meta.Spans))
	return nil
}

// buildAnnotateProvider constructs a provider for one relay base URL, honoring
// LOCOMO_PROVIDER. Used by the annotation pass to build primary + fallback
// endpoints that share the same credential.
func buildAnnotateProvider(apiKey, baseURL string, maxTokens int) (provider.Provider, error) {
	switch strings.ToLower(envOr("LOCOMO_PROVIDER", "anthropic")) {
	case "openai":
		return openai.New(openai.Options{APIKey: apiKey, BaseURL: baseURL, IncludeUsage: true}), nil
	case "anthropic", "":
		return anthropic.New(anthropic.Options{APIKey: apiKey, BaseURL: baseURL, DefaultMaxTokens: maxTokens}), nil
	default:
		return nil, fmt.Errorf("LOCOMO_PROVIDER must be anthropic or openai, got %q", os.Getenv("LOCOMO_PROVIDER"))
	}
}

// unifiedAnswerContractProbeRuntimeConfig is intentionally private and
// short-lived. Credentials are needed to construct providers but are never
// copied into the probe's persisted manifest or terminal summary.
type unifiedAnswerContractProbeRuntimeConfig struct {
	answerProvider string
	answerBaseURL  string
	answerAPIKey   string
	answerMetadata unifiedAnswerContractProbeModelMetadata
	judge          judgeConfig
	judgeMetadata  unifiedAnswerContractProbeModelMetadata
}

// validateUnifiedAnswerContractProbeMode recognizes the dedicated behavior
// probe before ordinary dataset/run validation. Both paths are explicit so an
// output typo cannot silently fall through into a benchmark run.
func validateUnifiedAnswerContractProbeMode(opt options) (bool, error) {
	fixturePath := strings.TrimSpace(opt.unifiedProbeFixture)
	reportPath := strings.TrimSpace(opt.unifiedProbeOut)
	requested := fixturePath != "" || reportPath != ""
	if !requested {
		return false, nil
	}
	if fixturePath == "" {
		return true, fmt.Errorf("--unified-answer-probe is required when --unified-answer-probe-out is set")
	}
	if reportPath == "" {
		return true, fmt.Errorf("--unified-answer-probe-out is required with --unified-answer-probe")
	}
	if opt.unifiedProbeRepeats <= 0 || opt.unifiedProbeRepeats%2 == 0 {
		return true, fmt.Errorf("--unified-answer-probe-repeats must be a positive odd number, got %d", opt.unifiedProbeRepeats)
	}
	if opt.maxTokens <= 0 {
		return true, fmt.Errorf("--max-tokens must be positive for --unified-answer-probe")
	}
	allowed := map[string]bool{
		"unified-answer-probe":         true,
		"unified-answer-probe-out":     true,
		"unified-answer-probe-repeats": true,
		"max-tokens":                   true,
		"concurrency":                  true,
	}
	var ignored []string
	for name := range opt.explicitFlags {
		if !allowed[name] {
			ignored = append(ignored, "--"+name)
		}
	}
	if len(ignored) > 0 {
		sort.Strings(ignored)
		return true, fmt.Errorf("--unified-answer-probe is a dedicated mode; unsupported flags would be ignored: %s", strings.Join(ignored, ", "))
	}
	if opt.dataPath != "" || opt.runDir != "" || opt.compareSpec != "" {
		return true, fmt.Errorf("--unified-answer-probe is a dedicated mode and cannot be combined with --data, --run-dir, or --compare")
	}
	return true, nil
}

func resolveUnifiedAnswerContractProbeRuntimeConfig(getenv func(string) string) (unifiedAnswerContractProbeRuntimeConfig, error) {
	var config unifiedAnswerContractProbeRuntimeConfig
	if getenv == nil {
		return config, fmt.Errorf("resolve unified answer contract probe configuration: environment reader is nil")
	}
	config.answerProvider = strings.ToLower(strings.TrimSpace(envValueOr(getenv, "LOCOMO_PROVIDER", defaultLoCoMoProvider)))
	config.answerBaseURL = envValueOr(getenv, "LOCOMO_BASE_URL", defaultLoCoMoBaseURL)
	config.answerAPIKey = getenv("LOCOMO_API_KEY")
	if strings.TrimSpace(config.answerAPIKey) == "" {
		return config, fmt.Errorf("LOCOMO_API_KEY is required for --unified-answer-probe (credentials are accepted through the environment only)")
	}
	answerModel := envValueOr(getenv, "LOCOMO_MODEL", defaultLoCoMoModel)
	config.answerMetadata = unifiedAnswerContractProbeModelMetadata{
		Provider: config.answerProvider,
		Model:    answerModel,
		Revision: modelRevisionMetadata(getenv, "LOCOMO_MODEL_REVISION", answerModel),
	}

	config.judge = resolveJudgeConfig(getenv)
	config.judge.Provider = strings.ToLower(strings.TrimSpace(config.judge.Provider))
	if strings.TrimSpace(config.judge.APIKey) == "" {
		return config, fmt.Errorf("JUDGE_API_KEY or LOCOMO_API_KEY is required for --unified-answer-probe (credentials are accepted through the environment only)")
	}
	config.judgeMetadata = unifiedAnswerContractProbeModelMetadata{
		Provider: config.judge.Provider,
		Model:    config.judge.Model,
		Revision: modelRevisionMetadata(getenv, "JUDGE_MODEL_REVISION", config.judge.Model),
	}
	return config, nil
}

func envValueOr(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func modelRevisionMetadata(getenv func(string) string, key, model string) string {
	if revision := strings.TrimSpace(getenv(key)); revision != "" {
		return revision
	}
	return "unverified:" + strings.TrimSpace(model)
}

func runUnifiedAnswerContractProbeMode(ctx context.Context, opt options, getenv func(string) string) error {
	runtime, err := resolveUnifiedAnswerContractProbeRuntimeConfig(getenv)
	if err != nil {
		return err
	}
	answerProvider, err := buildBenchProvider(runtime.answerProvider, runtime.answerAPIKey, runtime.answerBaseURL, opt.maxTokens, "LOCOMO_PROVIDER")
	if err != nil {
		return err
	}
	judgeProvider, err := buildBenchProvider(runtime.judge.Provider, runtime.judge.APIKey, runtime.judge.BaseURL, opt.maxTokens, "JUDGE_PROVIDER")
	if err != nil {
		return err
	}
	concurrency := opt.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	// The probe records failures as experimental outcomes. Transparent retries
	// would hide those outcomes, so both roles make exactly one provider attempt.
	answerCall := gateUsageOnce(sem, newUsageModelCallerWithUsage(
		answerProvider, runtime.answerMetadata.Model, opt.maxTokens, "answer", nil,
	))
	judgeCall := gateUsageOnce(sem, newUsageModelCallerWithUsage(
		judgeProvider, runtime.judgeMetadata.Model, opt.maxTokens, "judge", nil,
	))
	binaryDigest, sourceRevision, sourceModified, err := unifiedAnswerContractProbeBuildProvenance()
	if err != nil {
		return err
	}
	report, err := runUnifiedAnswerContractProbeCLI(ctx, unifiedAnswerContractProbeCLIConfig{
		FixturePath:          opt.unifiedProbeFixture,
		ReportPath:           opt.unifiedProbeOut,
		Repeats:              opt.unifiedProbeRepeats,
		AnswerModel:          runtime.answerMetadata,
		JudgeModel:           runtime.judgeMetadata,
		AnswerEndpointDigest: evalTextDigest(strings.TrimSpace(runtime.answerBaseURL)),
		JudgeEndpointDigest:  evalTextDigest(strings.TrimSpace(runtime.judge.BaseURL)),
		MaxTokens:            opt.maxTokens,
		ThinkingDisabled:     benchNoThinking,
		BinaryDigest:         binaryDigest,
		SourceRevision:       sourceRevision,
		SourceModified:       sourceModified,
	}, answerCall, judgeCall)
	if err != nil {
		return err
	}
	fmt.Printf("unified-answer-probe: completed %d paired arm attempts; report=%s\n", len(report.Results), opt.unifiedProbeOut)
	return nil
}

func unifiedAnswerContractProbeBuildProvenance() (binaryDigest, sourceRevision string, sourceModified bool, err error) {
	executable, err := os.Executable()
	if err != nil {
		return "", "", false, fmt.Errorf("locate unified answer probe binary: %w", err)
	}
	raw, err := os.ReadFile(executable) //nolint:gosec // hashes the running binary; never persists its bytes
	if err != nil {
		return "", "", false, fmt.Errorf("hash unified answer probe binary: %w", err)
	}
	sum := sha256.Sum256(raw)
	binaryDigest = "sha256:" + hex.EncodeToString(sum[:])
	sourceRevision = "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if strings.TrimSpace(setting.Value) != "" {
					sourceRevision = setting.Value
				}
			case "vcs.modified":
				sourceModified = setting.Value == "true"
			}
		}
	}
	return binaryDigest, sourceRevision, sourceModified, nil
}

func buildBenchProvider(providerName, apiKey, baseURL string, maxTokens int, envName string) (provider.Provider, error) {
	switch strings.ToLower(providerName) {
	case "openai":
		return openai.New(openai.Options{APIKey: apiKey, BaseURL: baseURL, IncludeUsage: true}), nil
	case "anthropic", "":
		return anthropic.New(anthropic.Options{APIKey: apiKey, BaseURL: baseURL, DefaultMaxTokens: maxTokens}), nil
	default:
		return nil, fmt.Errorf("%s must be anthropic or openai, got %q", envName, providerName)
	}
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

const defaultSweepBudgetBaseline = 5145

func sweepOverBudget(opt options, sweepUsed bool, usage provider.Usage) bool {
	if !sweepUsed {
		return false
	}
	baseline := opt.budgetBaseline
	if baseline <= 0 {
		baseline = defaultSweepBudgetBaseline
	}
	return float64(usage.InputTokens) > baseline*1.5
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
	"assoc":        {},
	"sweep":        {},
	"temporal":     {},
	"tplan":        {},
	"conflict":     {},
	"abstain":      {},
	"abstain-hard": {},
	"abstain-soft": {},
	"rerank":       {},
	"pcic":         {},
	"oracle":       {},
	"unified":      {},
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
		arm.clusterSweep = false
		arm.conflictResolution = false
		arm.abstainPrompt = false
		arm.abstainHard = false
		arm.abstainSoft = false
		arm.rerank = false
		arm.pcic = false
		arm.oracle = false
		arm.unifiedAnswerContract = global.unifiedAnswerContract
		return arm
	}
	arm := global
	arm.assoc = spec.mechanisms["assoc"]
	arm.clusterSweep = spec.mechanisms["sweep"]
	arm.temporalScore = spec.mechanisms["temporal"]
	arm.temporalHardFilter = false
	arm.conflictResolution = spec.mechanisms["conflict"]
	arm.abstainPrompt = spec.mechanisms["abstain"] || spec.mechanisms["abstain-soft"]
	arm.abstainHard = spec.mechanisms["abstain-hard"]
	arm.abstainSoft = spec.mechanisms["abstain-soft"]
	arm.temporalAnswerPrompt = global.temporalAnswerPrompt || spec.mechanisms["tplan"]
	arm.unifiedAnswerContract = global.unifiedAnswerContract || spec.mechanisms["unified"]
	arm.rerank = spec.mechanisms["rerank"]
	arm.pcic = spec.mechanisms["pcic"]
	arm.oracle = spec.mechanisms["oracle"]
	return arm
}

func pcicEnabledForRun(global options, arms []string) bool {
	for _, arm := range arms {
		armOpt := optionsForRun(global, arm, len(arms) > 1)
		if armOpt.pcic || armOpt.abstainHard || armOpt.abstainSoft {
			return true
		}
	}
	return false
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
	store       *store.Store
	entries     *memory.EntryStore
	projections *memory.ProjectionStore
	episodes    *memory.EpisodeStore
	vectors     *memory.VectorStore
	embedClient embedding.Client
	retrievers  map[string]*memory.Retriever
	reranked    map[string]bool
	// chunkTurns maps a verbatim-chunk entry name to the dialogue ids its text
	// covers (D<session>:<turn>), enabling exact-turn evidence recall. Empty when
	// chunks are not ingested.
	chunkTurns map[string][]string
	// turnEvidence maps the dataset dialogue ID to its namespace-local Ledger
	// Evidence ID. It is used only by formal source coverage materialization.
	turnEvidence map[string]string
	// suppression holds the cumulative write-time redundancy audit counters for
	// this conversation when write_dedup is enabled (024 US1 / FR-005). It is
	// aggregated into the run's suppression audit artifact.
	suppression pipeline.SuppressionStats
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
	if aliasShadowEnabled(opt) && !opt.aliasShadowPrepared {
		return nil, fmt.Errorf("alias-shadow runtime requires a prepared run-local store copy; refusing to open %s", opt.storeDir)
	}
	if doc2queryEnabled(opt) && !opt.doc2queryPrepared {
		return nil, fmt.Errorf("doc2query runtime requires a prepared run-local store copy; refusing to open %s", opt.storeDir)
	}
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
	projections := memory.NewProjectionStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	episodes := memory.NewEpisodeStore(st.DB(), es.Ledger(), projections)
	embedder := memory.NewEmbedder(es, vectors, embClient, memory.DefaultEmbedBuffer)

	var suppressor pipeline.RedundancySuppressor
	if opt.writeDedup {
		// 024 write-time redundancy suppression (US1): the engine's offline
		// Jaccard suppressor, pure local, no embedding/LLM required (FR-010).
		suppressor = curation.NewSuppressor(0) // 0 → default threshold 0.7
	}
	pipe := pipeline.New(pipeline.Config{
		Entries:    es,
		Embedder:   embedder,
		Call:       extractCall,
		Budgets:    memory.DefaultBudgets(),
		Suppressor: suppressor,
	})

	// Ingest each session with its date (extraction is the shared, once-paid
	// pass). A persisted store that already holds extracted facts skips it.
	if n, err := countExtracted(ctx, es); err != nil {
		return nil, err
	} else if n > 0 {
		// A store built with --chunks must only be reused by a run that also
		// enables --chunks; otherwise its verbatim chunk entries silently stay
		// in the retrieval pool (the retriever never filters category="chunk").
		if err := validateChunkRegime(ctx, st.DB(), opt.chunks, dsn); err != nil {
			return nil, err
		}
		if temporalMechanismEnabled(opt, arms) {
			if err := validateTemporalStore(ctx, st.DB(), n); err != nil {
				return nil, err
			}
		}
		logger.Info("reusing persisted extraction", "conversation", conv.ID, "facts", n)
	} else {
		if aliasShadowEnabled(opt) {
			return nil, fmt.Errorf("alias-shadow requires a prebuilt persisted store for conversation %d; refusing to call extraction", conv.ID)
		}
		if doc2queryEnabled(opt) {
			return nil, fmt.Errorf("doc2query requires a prebuilt persisted store for conversation %d; refusing to call extraction", conv.ID)
		}
		for _, s := range conv.Sessions {
			msgs := benchmarkSessionMessages(conv, s)
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
			msgs := benchmarkSessionMessages(conv, s)
			n, err := opinionPipe.Ingest(ctx, s.Date, fmt.Sprintf("conv%d-sess%d-op", conv.ID, s.Index), msgs)
			if err != nil {
				logger.Warn("opinion pass failed", "conversation", conv.ID, "session", s.Index, "err", err)
				continue
			}
			added += n
		}
		logger.Info("opinion pass done", "conversation", conv.ID, "entries_added", added)
	}
	var chunkTurns map[string][]string
	var turnEvidence map[string]string
	// clusterConfigHash fingerprints the 025 clustering config so different
	// thresholds rebuild episodes under a distinct config hash (idempotent
	// within one config, isolated across configs).
	clusterConfigHash := func(opt options) string {
		h := sha256.New()
		fmt.Fprintf(h, "jaccard=%g;embed=%g;max=%d", opt.clusterMinKeywordJaccard, opt.clusterEmbedThresh, opt.clusterMaxEvidence)
		return hex.EncodeToString(h.Sum(nil))[:12]
	}
	if opt.chunks {
		if turns, evidence, n, err := ingestChunks(ctx, es, conv); err != nil {
			logger.Warn("chunk ingest failed", "conversation", conv.ID, "err", err)
		} else {
			chunkTurns = turns
			turnEvidence = evidence
			logger.Info("verbatim chunks ingested", "conversation", conv.ID, "chunks", n)
		}
	}
	if conflictMechanismEnabled(opt, arms) {
		// One non-destructive supersede pass over the built store. Superseded
		// markers are inert for arms that leave the penalty at zero, so a shared
		// store stays valid for the paired baseline arm.
		cw := curation.NewWorker(es, st.DB(), curation.ModelCaller(extractCall), curation.Config{
			Budgets: memory.DefaultBudgets(),
		}, logger)
		if err := cw.ResolveConflictsPass(ctx); err != nil {
			logger.Warn("conflict resolution pass failed", "conversation", conv.ID, "err", err)
		}
	}
	// Drain embeddings synchronously before answering (only meaningful when a
	// hybrid arm supplied an embedding client).
	if err := embedder.Backfill(ctx); err != nil {
		logger.Warn("embedding backfill failed", "conversation", conv.ID, "err", err)
	}
	embedder.Close()
	// 025 cross-session semantic episode clustering: rebuild semantic_episode
	// projections from all active Evidence (across sessions) before answering.
	// Default off; with semantic_episode representation the renderer then has
	// real episode projections to expand instead of falling back to raw anchors.
	if opt.episodeCluster {
		opts := memory.ClusterOptions{
			MinKeywordJaccard:     opt.clusterMinKeywordJaccard,
			MaxEvidencePerEpisode: opt.clusterMaxEvidence,
			EmbedThresh:           opt.clusterEmbedThresh,
		}
		var clusterer memory.SemanticClusterer
		if embClient != nil {
			clusterer = memory.NewHybridClusterer(opts, embClient)
		} else {
			clusterer = memory.NewOfflineClusterer(opts)
		}
		projs, err := episodes.RebuildAll(ctx, clusterer, "025", "episode-cluster:"+clusterConfigHash(opt))
		if err != nil {
			logger.Warn("episode clustering failed; semantic_episode arm will fall back", "conversation", conv.ID, "err", err)
		} else {
			logger.Info("episode clustering rebuilt", "conversation", conv.ID, "episodes", len(projs))
		}
	}
	if aliasShadowEnabled(opt) {
		count, err := enforceAliasShadowStoreMode(ctx, st.DB(), opt.aliasShadow)
		if err != nil {
			return nil, err
		}
		logger.Info("alias-shadow store prepared", "conversation", conv.ID, "arm", opt.aliasShadow, "shadow_vectors", count)
	}
	if doc2queryEnabled(opt) {
		model := ""
		if embClient != nil {
			model = embClient.Model()
		}
		count, err := enforceDoc2QueryStoreMode(ctx, st.DB(), opt.doc2query, model)
		if err != nil {
			return nil, err
		}
		logger.Info("doc2query store prepared", "conversation", conv.ID, "arm", opt.doc2query, "shadow_vectors", count)
	}

	// One retriever per arm over the same store. Only the hybrid arm gets the
	// semantic signal and the optional rerank stage; fts stays the pure legacy
	// baseline.
	retrievers := make(map[string]*memory.Retriever, len(arms))
	reranked := make(map[string]bool, len(arms))
	for _, arm := range arms {
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		retrieverOpts := retrieverOptionsForAt(armOpt, temporalNowForConversation(conv))
		if armBackend(arm) == "hybrid" {
			var reranker embedding.Reranker
			if armOpt.rerank {
				reranker = buildBenchReranker()
			}
			reranked[arm] = reranker != nil
			retrievers[arm] = memory.NewRetrieverWithOptions(es, vectors, embClient, reranker, retrieverOpts)
		} else {
			retrievers[arm] = memory.NewRetrieverWithOptions(es, vectors, nil, nil, retrieverOpts)
		}
	}
	keepStore = true
	return &conversationRuntime{
		store: st, entries: es, projections: projections, episodes: episodes,
		vectors: vectors, embedClient: embClient, retrievers: retrievers,
		reranked: reranked, chunkTurns: chunkTurns, turnEvidence: turnEvidence,
		suppression: pipe.SuppressionStats(),
	}, nil
}

func retrieverOptionsFor(opt options) memory.RetrieverOptions {
	return retrieverOptionsForAt(opt, time.Time{})
}

func retrieverOptionsForAt(opt options, now time.Time) memory.RetrieverOptions {
	// The superseded penalty only bites when conflict resolution has actually
	// marked entries during the build; keeping it zero otherwise preserves
	// byte-for-byte parity with the baseline arm.
	supersededPenalty := 0.0
	if opt.conflictResolution {
		supersededPenalty = opt.supersededPenalty
	}
	return memory.RetrieverOptions{
		Associative:        opt.assoc,
		AssocDepth:         opt.assocDepth,
		ClusterSweep:       opt.clusterSweep,
		TemporalScore:      opt.temporalScore || opt.temporalHardFilter,
		TemporalHardFilter: opt.temporalHardFilter,
		SupersededPenalty:  supersededPenalty,
		Now:                now,
	}
}

func retrievalFingerprint(opt options) string {
	depth := opt.assocDepth
	if depth <= 0 || depth > 2 {
		depth = 2
	}
	fingerprint := fmt.Sprintf("assoc=%t;assoc_depth=%d", opt.assoc, depth)
	if opt.clusterSweep {
		fingerprint += ";cluster_sweep=true"
	}
	if opt.temporalScore || opt.temporalHardFilter {
		fingerprint += fmt.Sprintf(";temporal_score=%t;temporal_hard_filter=%t", opt.temporalScore || opt.temporalHardFilter, opt.temporalHardFilter)
	}
	if opt.conflictResolution {
		fingerprint += fmt.Sprintf(";conflict_resolution=true;superseded_penalty=%.3f", opt.supersededPenalty)
	}
	if opt.multiQuery {
		fingerprint += ";" + multiQueryRecipeFingerprint(opt)
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
	// Arm suffixes can override answer-regime mechanisms per arm. Bind every
	// effective arm fingerprint, not only the arm names: a prompt edit under a
	// stable suffix such as +unified must make legacy journal resume fail closed.
	arms, err := armsFor(opt.retrieval)
	if err != nil {
		return err
	}
	armRegimes := make([]string, 0, len(arms))
	for _, arm := range arms {
		armOpt := optionsForRun(opt, arm, len(arms) > 1)
		armRegimes = append(armRegimes, arm+"={"+answerRegimeFingerprint(armOpt)+"}")
	}
	regime := "retrieval=" + opt.retrieval + ";arms=" + strings.Join(armRegimes, ",")
	if opt.multiQuery {
		regime += ";" + retrievalFingerprint(opt)
	}
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
	legacyJournals, err := filepath.Glob(filepath.Join(opt.runDir, "results-*.jsonl"))
	if err != nil {
		return fmt.Errorf("inspect legacy result journals: %w", err)
	}
	if len(legacyJournals) > 0 {
		sort.Strings(legacyJournals)
		return fmt.Errorf("run dir %s contains legacy result journal %s without regime.json — use a fresh --run-dir (prompt provenance cannot be reconstructed)", opt.runDir, filepath.Base(legacyJournals[0]))
	}
	if err := os.WriteFile(path, []byte(regime+"\n"), 0o644); err != nil {
		return fmt.Errorf("write run dir regime: %w", err)
	}
	return nil
}

func answerRegimeFingerprint(opt options) string {
	fingerprint := fmt.Sprintf("force_answer=%t;abstain_prompt=%t;no_idk_retry=%t", opt.forceAnswer, opt.abstainPrompt, opt.noIDKRetry)
	if opt.evidenceAssembly {
		entityOrder := "kind_layered"
		if opt.assemblyLegacyEntityOrder {
			entityOrder = "legacy_grouped"
		}
		fingerprint += ";evidence_assembly=true;assembly_entity_order=" + entityOrder
	}
	if opt.temporalAnswerPrompt {
		fingerprint += ";temporal_answer_prompt=true"
	}
	if opt.lmeTypedPrompts {
		fingerprint += ";lme_typed_prompts=true"
	}
	if opt.unifiedAnswerContract {
		fingerprint += ";unified_answer_contract=true"
	}
	if opt.unifiedPairAudit {
		fingerprint += ";unified_pair_audit=true;provider_attempts=1"
	}
	// Prompt text is executable protocol. Bind the effective bytes for every
	// regime so a changed constant cannot silently resume into an old journal
	// under the same boolean flags.
	fingerprint += ";answer_prompt_digest=" + formalAnswerPromptDigest(opt)
	if opt.traceMediation {
		fingerprint += ";trace_mediation=true"
	}
	if opt.traceMultiEvidence {
		fingerprint += fmt.Sprintf(";trace_multi_evidence=true;trace_evidence_cap=%d", opt.traceEvidenceCap)
	}
	if opt.traceFallbackTopk > 0 {
		fingerprint += fmt.Sprintf(";trace_fallback_topk=%d", opt.traceFallbackTopk)
	}
	if opt.consolidate {
		fingerprint += ";consolidate=true"
	}
	if opt.relationContext {
		fingerprint += ";relation_context=true"
	}
	if opt.counterRefine {
		fingerprint += ";counter_refine=true"
	}
	if opt.temporalDateScaffold {
		fingerprint += ";temporal_date_scaffold=true"
	}
	if opt.judgeMem0Aligned {
		fingerprint += ";judge=mem0-aligned"
	}
	if opt.answerModel != "" && opt.judgeModel != "" && opt.answerModel != opt.judgeModel {
		fingerprint += ";judge_model=" + opt.judgeModel
	}
	return fingerprint
}

func validateAssemblyOptions(opt options) error {
	if opt.assemblyLegacyEntityOrder && !opt.evidenceAssembly {
		return fmt.Errorf("--assembly-legacy-entity-order requires --evidence-assembly")
	}
	if opt.assemblyAudit && !opt.evidenceAssembly {
		return fmt.Errorf("--assembly-audit requires --evidence-assembly")
	}
	if opt.assemblyAudit && opt.assemblyDiagnose {
		return fmt.Errorf("--assembly-audit cannot be combined with retrieval-only --assembly-diagnose")
	}
	return nil
}

func configuredAssemblyEntityOrder(opt options) string {
	if opt.assemblyLegacyEntityOrder {
		return assemblyEntityOrderLegacyGrouped
	}
	return assemblyEntityOrderKindLayered
}

func (o options) judgeAlignmentMode() string {
	if o.judgeMem0Aligned {
		return "mem0-aligned"
	}
	return "strict"
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

// validateConfidenceGatedOptions enforces the 041 flag contract
// (specs/041 contracts/cli-confidence-gated.md). Off (default) is a no-op.
func validateConfidenceGatedOptions(opt options) error {
	if !opt.confidenceGated {
		return nil
	}
	if opt.confidenceDeepK <= opt.confidenceShallowK {
		return fmt.Errorf("--confidence-deep-k (%d) must exceed --confidence-shallow-k (%d)", opt.confidenceDeepK, opt.confidenceShallowK)
	}
	if opt.confidenceThreshold < 0 {
		return fmt.Errorf("--confidence-threshold must be >= 0, got %v", opt.confidenceThreshold)
	}
	if opt.confidenceMaxRounds < 2 {
		return fmt.Errorf("--confidence-max-rounds must be at least 2, got %d", opt.confidenceMaxRounds)
	}
	if opt.multiQuery {
		return fmt.Errorf("--confidence-gated cannot be combined with --multi-query (SearchMulti's final budget must stay fixed)")
	}
	if strings.TrimSpace(opt.catTopKSpec) != "" {
		return fmt.Errorf("--confidence-gated cannot be combined with --cat-top-k (per-category depth overrides are undefined for the two-tier ladder)")
	}
	if strings.TrimSpace(opt.evalProtocolPath) != "" || strings.TrimSpace(opt.evalFreezeProtocol) != "" || strings.TrimSpace(opt.evalFreezeB0Protocol) != "" || opt.fixedGoldOracle {
		return fmt.Errorf("--confidence-gated is an independent opt-in path and cannot be combined with a frozen 022 B0/B1 protocol (its RetrievalCallLimit/AnswerCallLimit differ from the frozen one-call contract)")
	}
	if opt.abstainHard {
		return fmt.Errorf("--confidence-gated cannot be combined with --abstain-hard (the hard gate returns before any answerer generation, so no hesitation signal can be extracted)")
	}
	return nil
}

func validateMechanismArms(opt options) error {
	if err := validateRepresentationArm(opt.representationArm); err != nil {
		return err
	}
	formalContext := strings.TrimSpace(opt.evalProtocolPath) != "" || strings.TrimSpace(opt.evalFreezeProtocol) != ""
	if !formalContext {
		switch {
		case opt.representationArm == ReprEvent && strings.TrimSpace(opt.onlyQuestionsFile) != "":
			// 027 research-subset: the event representation runs in the
			// --only-questions pilot without a frozen-protocol binding.
		case opt.representationArm != "" && opt.representationArm != ReprChunk900:
			return fmt.Errorf("--representation requires --eval-protocol or --eval-freeze-protocol")
		case opt.compilerArm != "":
			return fmt.Errorf("--compiler-arm requires --eval-protocol or --eval-freeze-protocol")
		case opt.eventProjection != "" || opt.gapRefetch:
			return fmt.Errorf("--event-projection/--gap-refetch require --eval-protocol or --eval-freeze-protocol")
		case opt.writeDedup || opt.neighborExtend:
			return fmt.Errorf("--write-dedup/--neighbor-extend require --eval-protocol or --eval-freeze-protocol (024 density mechanisms fail closed outside a formal context)")
		case opt.temporalResolution:
			return fmt.Errorf("--temporal-resolution requires --eval-protocol or --eval-freeze-protocol (027 additive mechanism fails closed outside a formal context)")
		}
	}
	if opt.temporalResolution && opt.compilerArm != "" {
		return fmt.Errorf("--temporal-resolution and --compiler-arm are mutually exclusive (both replace the packer in the formal B1 bundle-assembly dispatch; keep attribution single-mechanism)")
	}
	switch opt.compilerArm {
	case "", "extractive", "planner", "exact_token":
	default:
		return fmt.Errorf("--compiler-arm must be extractive | planner | exact_token, got %q", opt.compilerArm)
	}
	// T114 is complete: a treatment freeze binds exactly one mechanism to its
	// frozen B1 control protocol hash (--control-protocol). freezeFormalProtocol
	// enforces the control-hash requirement; the pre-T114 blanket rejection is
	// removed so compiler/representation/event treatment manifests can freeze.
	if opt.eventProjection != "" {
		switch opt.eventProjection {
		case "E0", "E1", "E2", "E3":
		default:
			return fmt.Errorf("--event-projection must be E0, E1, E2, or E3, got %q", opt.eventProjection)
		}
		if !opt.gapRefetch {
			return fmt.Errorf("--event-projection requires --gap-refetch")
		}
	}
	if opt.gapRefetch && opt.eventProjection == "" {
		return fmt.Errorf("--gap-refetch requires --event-projection")
	}
	// T114 is complete: freezeFormalProtocol requires --control-protocol for any
	// treatment mechanism (buildFormalExperiment enforces isDigest(controlHash)).
	// The pre-T114 blanket freeze rejection was removed so treatment manifests can
	// freeze; a mechanism without a control hash still fails closed below.
	return nil
}

func validateRepresentationArm(arm RepresentationKind) error {
	switch arm {
	case ReprChunk900, ReprRawTurnWindow, ReprSemanticEpisode, ReprEvent:
		return nil
	default:
		return fmt.Errorf("invalid representation %q", arm)
	}
}

func validatePromptModes(opt options) error {
	if opt.forceAnswer && opt.abstainPrompt {
		return fmt.Errorf("--force-answer and --abstain-prompt are mutually exclusive")
	}
	if opt.lmeTypedPrompts && opt.datasetFormat != "longmemeval" {
		return fmt.Errorf("--lme-typed-prompts requires --dataset-format=longmemeval")
	}
	if opt.unifiedAnswerContract {
		var conflicts []string
		for _, conflict := range []struct {
			enabled bool
			name    string
		}{
			{opt.forceAnswer, "--force-answer"},
			{opt.abstainPrompt, "--abstain-prompt"},
			{opt.temporalAnswerPrompt, "--temporal-answer-prompt"},
			{opt.lmeTypedPrompts, "--lme-typed-prompts"},
			{opt.counterRefine, "--counter-refine"},
			{opt.abstainHard, "--abstain-hard"},
			{opt.abstainSoft, "--abstain-soft"},
		} {
			if conflict.enabled {
				conflicts = append(conflicts, conflict.name)
			}
		}
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			return fmt.Errorf("--unified-answer-contract cannot be combined with answer-policy override(s): %s", strings.Join(conflicts, ", "))
		}
		if opt.unifiedPairAudit {
			if !opt.noIDKRetry {
				return fmt.Errorf("--unified-answer-contract requires --no-idk-retry during the isolated prompt experiment so the answer text cannot change retrieval evidence")
			}
			for _, conflict := range []struct {
				enabled bool
				name    string
			}{
				{opt.temporalDateScaffold, "--temporal-date-scaffold"},
				{opt.traceMediation, "--trace-mediation"},
				{strings.TrimSpace(opt.catTopKSpec) != "", "--cat-top-k"},
				{strings.TrimSpace(opt.catQuotaSpec) != "", "--cat-chunk-quota"},
			} {
				if conflict.enabled {
					conflicts = append(conflicts, conflict.name)
				}
			}
			if len(conflicts) > 0 {
				sort.Strings(conflicts)
				return fmt.Errorf("--unified-answer-contract paired experiment must isolate the system prompt; incompatible configuration: %s", strings.Join(conflicts, ", "))
			}
		}
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
	var formalErrMu sync.Mutex
	var formalErr error
	recordFormalErr := func(err error) {
		if err == nil {
			return
		}
		formalErrMu.Lock()
		if formalErr == nil {
			formalErr = err
		}
		formalErrMu.Unlock()
	}
	selected := selectQuestions(conv, opt)
	var parityState *armState
	if len(states) > 0 {
		// The frozen parity schema identifies the query arm, not the retrieval
		// backend. Multi-query runs require one backend; legacy multi-backend runs
		// record their final configured state ("both" ends in hybrid).
		parityState = states[len(states)-1]
	}
	for _, selectedQuestion := range selected {
		qi, qa := selectedQuestion.Index, selectedQuestion.QA
		key := resultKey{Conv: conv.ID, Q: qi}
		for _, s := range states {
			armOpt := optionsForRun(opt, s.name, len(states) > 1)
			if prev, ok := s.journal.lookup(key); ok {
				s.agg.add(qa.Category, prev.Correct) // resume: reuse recorded result
				continue
			}
			qwg.Add(1)
			go func(s *armState, qa locomoQA, key resultKey, armOpt options, writeParity bool) {
				defer qwg.Done()
				if armOpt.b0Protocol != nil {
					recorder := newB0CallRecorder(armOpt.b0Protocol.ProtocolHash, armOpt.formalRunIndex)
					countedAnswer := recorder.wrapAnswer(answerCall)
					countedRewrite := recorder.wrapRewrite(rewriteCall)
					countedJudge := recorder.wrapJudge(judgeCall)
					correct, predicted, usage, sweepUsed, evidence, retrievalMeta, _ := answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(
						ctx, runtime.retrievers[s.name], countedAnswer, filterCall, countedRewrite,
						countedJudge, armOpt, qa, runtime.chunkTurns, nil, nil, logger,
					)
					receipt := recorder.snapshot()
					item := result{
						Conv: key.Conv, Q: key.Q, QuestionID: qa.QuestionID, Category: qa.Category, CategoryName: qa.CategoryName,
						QuestionType: qa.QuestionType, Adversarial: qa.Adversarial || qa.Category == adversarialCategory,
						Correct: correct, Question: qa.Question, Gold: goldFor(qa), Predicted: predicted,
						RetrievalFlags: retrievalFingerprint(armOpt), AnswerRegime: answerRegimeFingerprint(armOpt),
						InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, AnswerContextTokens: usage.InputTokens,
						SweepUsed: sweepUsed, SweepOverBudget: sweepOverBudget(armOpt, sweepUsed, usage),
						EvidenceDiagnostics: evidence, B0Continuity: &receipt,
					}
					_ = retrievalMeta
					s.agg.add(qa.Category, correct)
					if err := s.journal.writeResult(item, true); err != nil {
						recordFormalErr(err)
						logger.Error("write B0 continuity result failed", "conversation", key.Conv, "question", key.Q, "err", err)
					}
					return
				}
				if armOpt.formalProtocol != nil {
					if armOpt.formalReplay == nil || armOpt.formalCalls == nil {
						err := fmt.Errorf("formal replay or call journal unavailable")
						recordFormalErr(err)
						logger.Error("formal question infrastructure unavailable", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					release, err := admitFormalQuestion(ctx, armOpt.formalQuestionGate)
					if err != nil {
						recordFormalErr(err)
						logger.Warn("formal question admission failed", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					defer release()
					armOpt.formalEvidence = runtime.entries.Ledger()
					armOpt.formalEpisodes = runtime.episodes
					frozen, err := armOpt.formalReplay.getOrMaterialize(key, qa.QuestionID, func() formalFrozenQuestion {
						return materializeFormalB1Question(ctx, *armOpt.formalProtocol, armOpt, runtime.retrievers[s.name], runtime.projections, runtime.entries, qa, runtime.chunkTurns, runtime.turnEvidence)
					})
					if err != nil {
						recordFormalErr(err)
						logger.Error("formal question freeze failed; result left resumable", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					revalidated := revalidateFrozenFormalSources(ctx, *armOpt.formalProtocol, armOpt, qa, frozen)
					input, count, formal := prepareFrozenFormalB1Answer(ctx, *armOpt.formalProtocol, armOpt, answerCall, judgeCall, qa, revalidated, armOpt.formalRunIndex)
					// Bind the call journal to the exact payload that passed the
					// active Ledger reread, not the older replay snapshot.
					frozenDigest := formalFrozenPayloadDigest(revalidated)
					inputDigest := evalJSONDigest(input)
					correct := false
					predicted := ""
					usage := provider.Usage{}
					if len(formal.InvalidReasons) == 0 {
						if err := armOpt.formalCalls.Begin(key, qa.QuestionID, armOpt.formalRunIndex, frozenDigest, inputDigest); err != nil {
							recordFormalErr(err)
							logger.Error("record formal call intent failed", "conversation", key.Conv, "question", key.Q, "err", err)
							return
						}
						correct, predicted, usage, formal = callPreparedFrozenFormalB1Answer(ctx, armOpt, answerCall, judgeCall, qa, input, count, formal)
					}
					formal.InvalidReasons = stableStrings(formal.InvalidReasons)
					item := result{
						Conv: key.Conv, Q: key.Q, QuestionID: qa.QuestionID, Category: qa.Category, CategoryName: qa.CategoryName,
						QuestionType: qa.QuestionType, Adversarial: qa.Adversarial || qa.Category == adversarialCategory,
						Correct: correct, Question: qa.Question, Gold: goldFor(qa), Predicted: predicted,
						RetrievalFlags: retrievalFingerprint(armOpt), AnswerRegime: answerRegimeFingerprint(armOpt),
						InputTokens: formal.Answer.InputTokens, OutputTokens: usage.OutputTokens, AnswerContextTokens: formal.Answer.InputTokens,
						Formal022: &formal,
					}
					if len(formal.InvalidReasons) == 0 {
						err = armOpt.formalCalls.Finish(key, qa.QuestionID, armOpt.formalRunIndex, frozenDigest, inputDigest, item)
					} else if formal.Answer.AnswerCalls > 0 || formal.Answer.JudgeCalls > 0 {
						err = armOpt.formalCalls.Finish(key, qa.QuestionID, armOpt.formalRunIndex, frozenDigest, inputDigest, item)
					} else {
						err = armOpt.formalCalls.FailWithoutStart(key, qa.QuestionID, armOpt.formalRunIndex, frozenDigest, inputDigest, item)
					}
					if err != nil {
						recordFormalErr(err)
						logger.Error("record formal call terminal failed", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					s.agg.add(qa.Category, correct)
					if err := s.journal.writeResult(item, true); err != nil {
						recordFormalErr(err)
						logger.Error("write formal result failed; terminal remains replayable", "conversation", key.Conv, "question", key.Q, "err", err)
					}
					return
				}
				armOpt.selector, _ = selectorForArm(runtime, conv.ID, s.name, armOpt, nil, false)
				// 041 confidence-gated iterative retrieval (specs/041). On =
				// shallow retrieve→answer→hesitation check→deepen→answer; off
				// (default) this branch is never entered, keeping the single-shot
				// fixed-top-k path byte-identical (SC-003).
				if armOpt.confidenceGated {
					correct, predicted, usage, rec, iterErr := runConfidenceGatedQuestion(
						ctx, runtime.retrievers[s.name], qa, armOpt,
						budgetLadder{ShallowTopK: armOpt.confidenceShallowK, DeepTopK: armOpt.confidenceDeepK, ChunkQuota: armOpt.chunkQuota},
						confidenceGateConfig{Threshold: armOpt.confidenceThreshold, MaxRounds: armOpt.confidenceMaxRounds},
						answerCall, judgeCall,
					)
					if iterErr != nil {
						recordFormalErr(iterErr)
						logger.Error("confidence-gated question failed; result left resumable", "conversation", key.Conv, "question", key.Q, "err", iterErr)
						return
					}
					if armOpt.confidenceGateJournal != nil {
						if jerr := armOpt.confidenceGateJournal.Write(rec); jerr != nil {
							recordFormalErr(jerr)
							logger.Error("write confidence-gate decision failed", "conversation", key.Conv, "question", key.Q, "err", jerr)
						}
					}
					item := result{
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
					}
					s.agg.add(qa.Category, correct)
					if err := s.journal.writeResult(item, false); err != nil {
						recordFormalErr(err)
						logger.Error("write result failed", "conversation", key.Conv, "question", key.Q, "err", err)
					}
					return
				}
				var abstainRuntime *abstainRuntimeContext
				if armOpt.abstainHard || armOpt.abstainSoft {
					abstainRuntime = &abstainRuntimeContext{runtime: runtime, convID: conv.ID, arm: s.name, meta: armOpt.pcicMeta}
				}
				effectiveAnswerCall, effectiveJudgeCall := answerCall, judgeCall
				var pairObserver *unifiedPromptPairObserver
				if armOpt.unifiedPairAudit {
					pairObserver = newUnifiedPromptPairObserver()
					effectiveAnswerCall = pairObserver.wrapAnswer(answerCall)
					effectiveJudgeCall = pairObserver.wrapJudge(judgeCall)
				}
				correct, predicted, usage, sweepUsed, evidence, retrievalMeta, notebookAttribution := answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(ctx, runtime.retrievers[s.name], effectiveAnswerCall, filterCall, rewriteCall, effectiveJudgeCall, armOpt, qa, runtime.chunkTurns, turnTextIndex(conv), abstainRuntime, logger)
				if writeParity && armOpt.contextParity != nil {
					record := contextParityRecord{
						Conv:                key.Conv,
						Q:                   key.Q,
						Category:            qa.Category,
						Arm:                 contextParityArm(armOpt),
						FinalTopK:           retrievalMeta.finalTopK,
						AnswerContextTokens: usage.InputTokens,
						SubqueryCount:       retrievalMeta.subqueryCount,
					}
					if err := validateAliasShadowContextParity(armOpt, record); err != nil {
						armOpt.contextParity.Fail(err)
						logger.Error("alias-shadow context parity failed; result left resumable", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					if err := validateDoc2QueryContextParity(armOpt, record); err != nil {
						armOpt.contextParity.Fail(err)
						logger.Error("doc2query context parity failed; result left resumable", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
					if err := armOpt.contextParity.Write(record); err != nil {
						logger.Error("write context parity failed; result left resumable", "conversation", key.Conv, "question", key.Q, "err", err)
						return
					}
				}
				item := result{
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
					HardGated:           abstainRuntime != nil && abstainRuntime.hardGated,
					RetrievalFlags:      retrievalFingerprint(armOpt),
					AnswerRegime:        answerRegimeFingerprint(armOpt),
					InputTokens:         usage.InputTokens,
					OutputTokens:        usage.OutputTokens,
					AnswerContextTokens: usage.InputTokens,
					SweepUsed:           sweepUsed,
					SweepOverBudget:     sweepOverBudget(armOpt, sweepUsed, usage),
					EvidenceDiagnostics: evidence,
					UnifiedPairAudit:    nil,
					Notebook:            notebookAttribution,
				}
				if pairObserver != nil {
					audit := pairObserver.snapshot()
					item.UnifiedPairAudit = &audit
				}
				s.agg.add(qa.Category, correct)
				if err := s.journal.writeResult(item, armOpt.unifiedPairAudit); err != nil {
					recordFormalErr(err)
					logger.Error("write result failed", "conversation", key.Conv, "question", key.Q, "err", err)
				}
			}(s, qa, key, armOpt, s == parityState)
		}
	}
	qwg.Wait()
	logger.Info("conversation done", "conversation", conv.ID, "answered", len(selected))
	formalErrMu.Lock()
	defer formalErrMu.Unlock()
	return formalErr
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

func conflictMechanismEnabled(opt options, arms []string) bool {
	for _, arm := range arms {
		if optionsForRun(opt, arm, len(arms) > 1).conflictResolution {
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
	correct, predicted, _, _ := answerAndJudgeWithUsage(ctx, retriever, usageCallerFromModel(answerCall), answerCall, answerCall, usageCallerFromModel(judgeCall), opt, qa, logger)
	return correct, predicted
}

func answerAndJudgeWithUsage(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, opt options, qa locomoQA, logger *slog.Logger) (bool, string, provider.Usage, bool) {
	correct, predicted, usage, sweepUsed, _ := answerAndJudgeWithEvidenceDiagnostics(ctx, retriever, answerCall, filterCall, rewriteCall, judgeCall, opt, qa, nil, logger)
	return correct, predicted, usage, sweepUsed
}

func answerAndJudgeWithEvidenceDiagnostics(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, opt options, qa locomoQA, chunkTurns map[string][]string, logger *slog.Logger) (bool, string, provider.Usage, bool, *sweepEvidenceDiagnostics) {
	return answerAndJudgeWithAbstentionEvidenceDiagnostics(ctx, retriever, answerCall, filterCall, rewriteCall, judgeCall, opt, qa, chunkTurns, nil, nil, logger)
}

type abstainRuntimeContext struct {
	runtime   *conversationRuntime
	convID    int
	arm       string
	meta      *PCICMeta
	hardGated bool
}

func defaultFrontierAbstainThresholds() AbstainThresholdConfig {
	return AbstainThresholdConfig{
		UseClaim:            true,
		ClaimThreshold:      1,
		UseConfidence:       true,
		ConfidenceThreshold: 0.5,
	}
}

func abstainDecisionForHits(ctx context.Context, abstain *abstainRuntimeContext, qa locomoQA, hits []memory.Result) (AbstainDecision, error) {
	if abstain == nil || abstain.runtime == nil {
		return AbstainDecision{}, nil
	}
	signal, err := computeAbstainSignal(ctx, abstain.runtime.entries, qa.Question, abstainSignalInput{
		QuestionID:        qa.QuestionID,
		Category:          qa.Category,
		Candidates:        hits,
		Meta:              abstain.meta,
		ChunkTurns:        abstain.runtime.chunkTurns,
		SpanKey:           func(turnID string) string { return pcicSpanKey(abstain.convID, turnID) },
		Reranked:          abstain.runtime.reranked[abstain.arm],
		CosineByCandidate: probeCandidateCosines(ctx, abstain.runtime, qa.Question, hits),
	})
	if err != nil {
		return AbstainDecision{}, err
	}
	return decideAbstention(signal, defaultFrontierAbstainThresholds()), nil
}

func answerAndJudgeWithAbstentionEvidenceDiagnostics(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, opt options, qa locomoQA, chunkTurns map[string][]string, turnText map[string]string, abstain *abstainRuntimeContext, logger *slog.Logger) (bool, string, provider.Usage, bool, *sweepEvidenceDiagnostics) {
	correct, predicted, usage, sweepUsed, evidence, _, _ := answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(ctx, retriever, answerCall, filterCall, rewriteCall, judgeCall, opt, qa, chunkTurns, turnText, abstain, logger)
	return correct, predicted, usage, sweepUsed, evidence
}

func answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, judgeCall usageModelCaller, opt options, qa locomoQA, chunkTurns map[string][]string, turnText map[string]string, abstain *abstainRuntimeContext, logger *slog.Logger) (bool, string, provider.Usage, bool, *sweepEvidenceDiagnostics, queryRetrievalMeta, *evalNotebookAttribution) {
	topK, quota := opt.retrievalFor(qa.Category)
	var hits []memory.Result
	var searchDiagnostics memory.SearchDiagnostics
	retrievalMeta := queryRetrievalMeta{finalTopK: topK, subqueryCount: 1}
	if opt.nav && opt.navTraj != nil {
		// 029 agentic multi-step navigation (specs/029 US2): the reasoning loop
		// decides the retrieval actions; its final evidence bundle (or the
		// fail-closed accumulated evidence) becomes the answer context. The
		// trajectory is audited to run-dir/nav-trajectories.jsonl. If navigation
		// itself fails (e.g. caller cancellation), fall back to the existing
		// single-shot retrieval so a question is never answered empty.
		navCall := opt.navDecideCall
		if navCall == nil {
			navCall = answerCall // no dedicated caller: navigate via the answerer
		}
		navCfg := navConfig{
			NavK:             opt.navK,
			MaxSteps:         opt.navMaxSteps,
			FallbackTopK:     topK,
			AnswerContextCap: defaultAnswerContextCap,
			Call:             navCall,
		}
		traj, navErr := runNavigation(ctx, qa.QuestionID, qa.Question, retriever, navCfg)
		if navErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, "", provider.Usage{}, false, nil, retrievalMeta, nil
			}
			logger.Warn("navigation failed; falling back to single-shot retrieval", "err", navErr)
			var err error
			hits, searchDiagnostics, retrievalMeta, err = retrieveQuestionWithDiagnostics(ctx, retriever, filterCall, rewriteCall, qa.Question, topK, quota, opt)
			if err != nil {
				logger.Warn("retrieve failed; question scored wrong", "err", err)
				return false, "", provider.Usage{}, false, nil, retrievalMeta, nil
			}
		} else {
			if err := opt.navTraj.Write(*traj); err != nil {
				logger.Warn("write nav trajectory failed", "err", err)
			}
			hits = navEvidenceToResults(traj.FinalEvidence.Evidence)
			retrievalMeta = queryRetrievalMeta{finalTopK: len(hits), subqueryCount: 1}
		}
	} else if opt.iris && qa.Category == 2 {
		// Feature 021 US1: IRIS evidence-gap loop for temporal questions. The
		// loop retrieves, evaluates accumulated sufficiency, diagnoses the gap,
		// and refines the query — at a fixed topK budget (slot-merged) so the
		// answerer's context stays aligned with the flat-hybrid baseline.
		irisHits, irisErr := irisRetrieve(ctx, retriever, filterCall, rewriteCall, answerCall, qa.Question, topK, quota, opt, qa.Category)
		if irisErr != nil {
			logger.Warn("iris retrieve failed; question scored wrong", "err", irisErr)
			return false, "", provider.Usage{}, false, nil, retrievalMeta, nil
		}
		hits = irisHits
	} else {
		var err error
		hits, searchDiagnostics, retrievalMeta, err = retrieveQuestionWithDiagnostics(ctx, retriever, filterCall, rewriteCall, qa.Question, topK, quota, opt)
		if err != nil {
			logger.Warn("retrieve failed; question scored wrong", "err", err)
			return false, "", provider.Usage{}, false, nil, retrievalMeta, nil
		}
	}
	// 027 event representation (research-subset): replace the retrieved hits
	// with the query-relevant event projection so the answer context carries
	// relational structure (facts + relations + temporal anchors) instead of
	// flat chunk/fact text.
	if opt.representationArm == ReprEvent && opt.eventProject != nil && len(hits) > 0 {
		hits = renderEventHitsForQuery(qa, hits, opt.eventProject, opt.topK)
	}
	sweepUsed := searchDiagnostics.SweepUsed || hasClusterSweepHit(hits)
	answerHits, answerDiagnostics := hits, searchDiagnostics
	// contextEvidence tracks the units actually placed in the answer context, so
	// a --notebook run can separate compiler_miss (in pool, dropped by assembly/
	// trace) from answerer_miss (in context, still wrong). Defaults to the final
	// hit set; assembly/trace override it below.
	contextEvidence := hits
	prompt := answerSystemPromptForEval(qa, opt)
	decision, err := abstainDecisionForHits(ctx, abstain, qa, hits)
	if err != nil {
		logger.Warn("abstain signal failed; answering normally", "err", err)
	}
	userPrompt := buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
	if opt.evidenceAssembly {
		// 030 US1 read-side assembly (specs/030): reorder evidence (chunk-first +
		// category structure), render the assembled prompt, and record the exact
		// token ledger. Off by default — legacy byte-identical path (SC-004).
		asm, assembledUser, asmErr := assembleEvidence(ctx, qa.Question, hits, qa.Category, assemblyConfig{
			Cap:             defaultAnswerContextCap,
			CurrentDate:     qa.QuestionDate,
			Scaffold:        opt.temporalDateScaffold,
			SystemPrompt:    prompt,
			QuestionID:      qa.QuestionID,
			RelationEnabled: opt.relationContext,
			EntityOrder:     configuredAssemblyEntityOrder(opt),
		}, opt.assemblyCounter)
		if asmErr != nil {
			logger.Warn("evidence assembly failed; using legacy context", "err", asmErr)
		} else {
			if opt.consolidate && asm.TotalTokens > asm.Cap {
				// 030 US3 conditional consolidation (specs/030): compress only
				// when the evidence provably exceeds the cap AND --consolidate is
				// set; within budget this stays a byte-identical no-op (retain).
				consolidated, _, _, consErr := consolidateUnits(ctx, qa.Question, asm.Units, consolidateConfig{
					Cap: asm.Cap, Call: opt.consolidateCall,
				})
				if consErr != nil {
					logger.Warn("consolidation failed; keeping assembled context", "err", consErr)
				} else {
					userPrompt = buildAnswerContextPrompt(qa.Question, unitsToResults(consolidated), qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
					contextEvidence = unitsToResults(consolidated)
				}
			} else {
				userPrompt = assembledUser
				contextEvidence = unitsToResults(asm.Units)
			}
			if opt.assemblyJournal != nil {
				if err := opt.assemblyJournal.Write(asm); err != nil {
					logger.Warn("write assembly diagnostic failed", "err", err)
				}
			}
		}
	}
	if opt.traceMediation && opt.traceSidecarCaller != nil && len(hits) > 0 {
		// 030 US2 grounded-evidence mediation (specs/030): the sidecar organises
		// the closed candidate set into a packet; the fail-closed gate keeps only
		// boundary-cited, traceable evidence E, which becomes the answer context.
		// Parse failure retries once; gate fallback or caller failure keeps the
		// (possibly assembled) legacy context. On by default (030 full-set
		// verification); off restores the legacy byte-identical path.
		tracePrompt := traceSystemPrompt
		if opt.traceMultiEvidence {
			tracePrompt = traceMultiEvidencePrompt
		}
		boundary := make(map[string]bool, len(hits))
		for _, h := range hits {
			boundary[h.Name] = true
		}
		raw, _, traceErr := opt.traceSidecarCaller(ctx, tracePrompt, traceUserPrompt(qa.Question, hits))
		evidence := []traceEvidence(nil)
		status := traceGateFallback
		retried := false
		if traceErr != nil {
			logger.Warn("trace sidecar call failed; using legacy context", "err", traceErr)
		} else {
			_, evidence, status, _ = mediateTrace(traceMediationInput{Raw: raw, CandidateIDs: boundary})
			if status == traceGateParseFailed {
				retried = true
				raw2, _, retryErr := opt.traceSidecarCaller(ctx, tracePrompt, traceUserPrompt(qa.Question, hits))
				if retryErr != nil {
					logger.Warn("trace sidecar retry failed; using legacy context", "err", retryErr)
				} else {
					_, evidence, status, _ = mediateTrace(traceMediationInput{Raw: raw2, CandidateIDs: boundary})
				}
			}
		}
		if opt.traceMultiEvidence {
			evidence = capEvidence(evidence, opt.traceEvidenceCap)
		}
		answerEvidence := evidenceFromTrace(evidence)
		if opt.traceFallbackTopk > 0 && len(answerEvidence) > 0 && len(hits) > 0 {
			k := opt.traceFallbackTopk
			if k > len(hits) {
				k = len(hits)
			}
			if !evidenceTouchesTopK(evidence, hits[:k]) {
				// trace 侧边车完全没引用检索 top-k 候选 —— compiler_miss 主因
				// (p0-diag3: gold 常 rank top-1/top-5 却被 trace 丢掉)。规则化兜底:
				// 用 top-k 原文作为 answer context,零额外 LLM。
				answerEvidence = hits[:k]
			}
		}
		if len(answerEvidence) > 0 {
			userPrompt = buildAnswerContextPrompt(qa.Question, answerEvidence, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)
			contextEvidence = answerEvidence
		}
		if opt.relationContext && len(answerEvidence) > 0 {
			// 031 T011 (contracts/evidence-relations.md §3): overlay the
			// structural-context block on the trace-mediated context, keeping only
			// edges whose endpoints lie inside the closed candidate boundary
			// (fail-closed reuse of the trace gate). Trace evidence carries no
			// EventDate → temporal chains fail-soft; multi-hop relations still apply.
			if block, _ := computeRelationContext(ctx, answerEvidence, qa.Category); block != nil {
				if kept := relationBlockWithinBoundary(block, boundary); kept != nil {
					kept.Text = renderRelationBlock(kept)
					kept.TokenCount = estimateTokens(kept.Text)
					userPrompt = appendRelationBlock(userPrompt, kept)
				}
			}
		}
		if opt.traceGateJournal != nil {
			if err := opt.traceGateJournal.Write(traceGateRecord{QuestionID: qa.QuestionID, Status: status, EvidenceCount: len(evidence), Retried: retried}); err != nil {
				logger.Warn("write trace gate diagnostic failed", "err", err)
			}
		}
	}
	predicted, usage, hardGated, err := answerWithAbstentionDecision(ctx, decision, opt, prompt, userPrompt, answerCall)
	if abstain != nil {
		abstain.hardGated = hardGated
	}
	if err != nil {
		logger.Warn("answer call failed; question scored wrong", "err", err)
		return false, "", usage, sweepUsed, newSweepEvidenceDiagnostics(qa, answerHits, answerDiagnostics, usage.InputTokens, chunkTurns), retrievalMeta, nil
	}

	if !hardGated && isIDK(predicted) && !opt.noIDKRetry {
		if retry, retryUsage, retryHits, retryDiagnostics, ok := retryWithRewriteUsageDiagnostics(ctx, retriever, answerCall, filterCall, rewriteCall, opt, qa, prompt, hits); ok {
			predicted = retry
			usage = retryUsage
			answerHits = retryHits
			if retryDiagnostics.SweepUsed {
				answerDiagnostics = retryDiagnostics
			}
			sweepUsed = sweepUsed || retryDiagnostics.SweepUsed || hasClusterSweepHit(retryHits)
		} else if retry, retryUsage, retryHits, retryDiagnostics, ok := retryWithWiderNetUsageDiagnostics(ctx, retriever, answerCall, opt, qa, prompt); ok {
			predicted = retry
			usage = retryUsage
			answerHits = retryHits
			if retryDiagnostics.SweepUsed {
				answerDiagnostics = retryDiagnostics
			}
			sweepUsed = sweepUsed || retryDiagnostics.SweepUsed || hasClusterSweepHit(retryHits)
		}
	}
	// --counter-refine (L2): verify the draft against candidate-internal
	// counter-evidence and optionally REVISE before judging. Default off →
	// results byte-identical (CounterRefine, arXiv:2603.16091).
	if opt.counterRefine && !hardGated && len(answerHits) > 0 {
		revised, reviseUsage, rerr := counterRefineAnswer(ctx, answerCall, opt, qa, predicted, answerHits)
		if rerr == nil {
			predicted = revised
			usage.InputTokens += reviseUsage.InputTokens
			usage.OutputTokens += reviseUsage.OutputTokens
		} else {
			logger.Warn("counter-refine call failed; keeping draft", "err", rerr)
		}
	}
	if hardGated {
		retrievalMeta.finalTopK = 0
	} else {
		retrievalMeta.finalTopK = len(answerHits)
	}
	evidence := newSweepEvidenceDiagnostics(qa, answerHits, answerDiagnostics, usage.InputTokens, chunkTurns)

	verdict, _, err := judgeCall(ctx, judgeSystemPromptFor(opt.judgeAlignmentMode()), buildJudgePrompt(qa.Question, goldFor(qa), predicted))
	if err != nil {
		logger.Warn("judge call failed; question scored wrong", "err", err)
		return false, predicted, usage, sweepUsed, evidence, retrievalMeta, nil
	}
	// --notebook: capture the gold attribution against the ACTUAL candidate set
	// and the ACTUAL answer context. Off by default → nil, results unchanged.
	var attribution *evalNotebookAttribution
	if opt.notebook && turnText != nil {
		goldTurns := parsedGoldTurns(qa.Evidence)
		att := computeNotebookAttribution(hits, contextEvidence, chunkTurns, turnText, goldTurns, opt.notebookFactTau)
		if opt.traceMediation && opt.traceSidecarCaller != nil && len(hits) > 0 {
			att.BundleApprox = true
		}
		att.ContextPreview = truncateRunes(userPrompt, notebookContextPreviewLen)
		attribution = &att
	}
	return parseJudgeVerdict(verdict), predicted, usage, sweepUsed, evidence, retrievalMeta, attribution
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
	hits, _, err := retrieveWithDiagnostics(ctx, retriever, filterCall, query, topK, quota, opt)
	return hits, err
}

func retrieveWithDiagnostics(ctx context.Context, retriever *memory.Retriever, filterCall modelCaller, query string, topK, quota int, opt options) ([]memory.Result, memory.SearchDiagnostics, error) {
	if opt.filterPool > topK {
		return retrieveFilteredDiagnostics(ctx, retriever, filterCall, query, topK, quota, opt.filterPool)
	}
	return retrieveWithQuotaDiagnostics(ctx, retriever, query, topK, quota, opt.selector)
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
	var union []memory.Result
	var fresh int
	if opt.multiQuery {
		union, fresh = unionMultiRetryResults(first, more, topK, quota)
	} else {
		union, fresh = unionRetryResults(first, more, 0)
	}
	if fresh == 0 {
		return "", false
	}
	retry, err := answerCall(ctx, prompt, buildAnswerContextPrompt(qa.Question, union, qa.QuestionDate, qa.Category, opt.temporalDateScaffold))
	if err != nil || isIDK(retry) {
		return "", false
	}
	return retry, true
}

func retryWithRewriteUsage(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, opt options, qa locomoQA, prompt string, first []memory.Result) (string, provider.Usage, bool) {
	retry, usage, _, _, ok := retryWithRewriteUsageDiagnostics(ctx, retriever, answerCall, filterCall, rewriteCall, opt, qa, prompt, first)
	return retry, usage, ok
}

func retryWithRewriteUsageDiagnostics(ctx context.Context, retriever *memory.Retriever, answerCall usageModelCaller, filterCall, rewriteCall modelCaller, opt options, qa locomoQA, prompt string, first []memory.Result) (string, provider.Usage, []memory.Result, memory.SearchDiagnostics, bool) {
	rewritten, err := rewriteCall(ctx, queryRewriteSystemPrompt, "QUESTION: "+qa.Question)
	if err != nil {
		return "", provider.Usage{}, nil, memory.SearchDiagnostics{}, false
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" || rewritten == qa.Question {
		return "", provider.Usage{}, nil, memory.SearchDiagnostics{}, false
	}
	topK, quota := opt.retrievalFor(qa.Category)
	more, diagnostics, err := retrieveWithDiagnostics(ctx, retriever, filterCall, rewritten, topK, quota, opt)
	if err != nil || len(more) == 0 {
		return "", provider.Usage{}, nil, diagnostics, false
	}
	var union []memory.Result
	var fresh int
	if opt.multiQuery {
		union, fresh = unionMultiRetryResults(first, more, topK, quota)
	} else {
		union, fresh = unionRetryResults(first, more, 0)
	}
	if fresh == 0 {
		return "", provider.Usage{}, nil, diagnostics, false
	}
	retry, usage, err := answerCall(ctx, prompt, buildAnswerContextPrompt(qa.Question, union, qa.QuestionDate, qa.Category, opt.temporalDateScaffold))
	if err != nil || isIDK(retry) {
		return "", usage, nil, diagnostics, false
	}
	return retry, usage, union, diagnostics, true
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
	retry, usage, _, _, ok := retryWithWiderNetUsageDiagnostics(ctx, retriever, call, opt, qa, prompt)
	return retry, usage, ok
}

func retryWithWiderNetUsageDiagnostics(ctx context.Context, retriever *memory.Retriever, call usageModelCaller, opt options, qa locomoQA, prompt string) (string, provider.Usage, []memory.Result, memory.SearchDiagnostics, bool) {
	topK, quota := opt.retrievalFor(qa.Category)
	hits, diagnostics, err := retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, topK*3, quota*3, opt.selector)
	if err != nil || len(hits) <= topK {
		return "", provider.Usage{}, nil, diagnostics, false
	}
	if opt.multiQuery {
		if quota > 0 {
			hits = applyChunkQuota(hits, topK, quota)
		} else {
			hits = hits[:topK]
		}
	}
	retry, usage, err := call(ctx, prompt, buildAnswerContextPrompt(qa.Question, hits, qa.QuestionDate, qa.Category, opt.temporalDateScaffold))
	if err != nil || isIDK(retry) {
		return "", usage, nil, diagnostics, false
	}
	return retry, usage, hits, diagnostics, true
}

// counterRefineAnswer verifies the draft answer a0 against candidate-internal
// counter-evidence (no second retrieval — the evidence is selected from the
// hits already retrieved) and returns the REVISED answer, or a0 unchanged when
// the revise call fails, returns empty, or bails out (L2, CounterRefine).
func counterRefineAnswer(ctx context.Context, answerCall usageModelCaller, opt options, qa locomoQA, a0 string, hits []memory.Result) (string, provider.Usage, error) {
	counter := selectCounterEvidence(a0, qa.Question, hits, opt.answerInputTokenCap)
	if len(counter) == 0 {
		return a0, provider.Usage{}, nil
	}
	user := counterRefineUserPrompt(qa, a0, counter, opt.temporalDateScaffold)
	revised, usage, err := answerCall(ctx, counterRefineSystemPrompt, user)
	if err != nil {
		return a0, usage, err
	}
	revised = strings.TrimSpace(revised)
	if revised == "" || isIDK(revised) {
		return a0, usage, nil
	}
	return revised, usage, nil
}

// counterRefineUserPrompt assembles the REVISE input: the answer context the
// model already saw (rendered counter-evidence) plus the explicit draft, then
// a KEEP/REVISE instruction. The counter-evidence is a hit subset, so this stays
// within the answer-input budget used for the first answer.
func counterRefineUserPrompt(qa locomoQA, draft string, counter []memory.Result, scaffold bool) string {
	ctx := buildAnswerContextPrompt(qa.Question, counter, qa.QuestionDate, qa.Category, scaffold)
	return fmt.Sprintf("%s\nDRAFT ANSWER: %s\n\nVerify the draft. If the counter-evidence contradicts it or supports a different correct answer, REVISE; otherwise KEEP. Output ONLY the final answer:", ctx, draft)
}

// counterRefineKeyChars keeps draft terms that are discriminative enough to
// match candidate evidence: 4+ letters and not a stop/generic word.
var counterRefineStop = map[string]bool{
	"that": true, "with": true, "have": true, "this": true, "from": true, "they": true,
	"there": true, "would": true, "about": true, "what": true, "were": true, "when": true,
	"which": true, "their": true, "been": true, "will": true, "more": true, "into": true,
	"your": true, "them": true, "only": true, "some": true, "then": true, "than": true,
	"after": true, "before": true, "because": true, "could": true, "should": true,
}

// counterRefineKeys extracts draft terms used to select candidate-internal
// counter-evidence. A draft like "Fixing cars" yields {"fixing","cars"}; an
// IDK bail-out yields none and selects the hit head instead.
func counterRefineKeys(draft string) []string {
	var keys []string
	for _, f := range strings.Fields(strings.ToLower(draft)) {
		t := strings.Trim(f, ",.:;\"'()[]-")
		if len(t) < 4 || counterRefineStop[t] {
			continue
		}
		keys = append(keys, t)
	}
	return keys
}

// selectCounterEvidence chooses candidate-internal counter-evidence from the
// retrieved hits: memories that mention a draft key-term first (the relevant
// but possibly-ignored candidates), falling back to the head of the hits when
// the draft has no discriminative terms. Order is stable (original rank);
// capped by a rough char budget derived from the answer-input token cap.
func selectCounterEvidence(draft, question string, hits []memory.Result, cap int) []memory.Result {
	if len(hits) == 0 {
		return nil
	}
	keys := counterRefineKeys(draft)
	charBudget := 0
	if cap > 0 {
		charBudget = cap * 3 // token→char rough budget, keep the REVISE prompt small
	}
	take := func(from []memory.Result) []memory.Result {
		out := []memory.Result{}
		chars := 0
		for _, h := range from {
			chars += len(h.Content) + len(h.SourceSessionID)
			if charBudget > 0 && chars > charBudget {
				break
			}
			out = append(out, h)
		}
		return out
	}
	if len(keys) == 0 {
		return take(hits)
	}
	var matched, rest []memory.Result
	for _, h := range hits {
		if counterRefineHit(keys, h.Content) {
			matched = append(matched, h)
		} else {
			rest = append(rest, h)
		}
	}
	if len(matched) == 0 {
		return take(rest)
	}
	return take(matched)
}

// counterRefineHit reports whether the memory content mentions any draft key.
func counterRefineHit(keys []string, content string) bool {
	low := strings.ToLower(content)
	for _, k := range keys {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// toMemories converts retrieval hits into the prompt-facing form.
func hasClusterSweepHit(hits []memory.Result) bool {
	for _, hit := range hits {
		if hit.ClusterSweep {
			return true
		}
	}
	return false
}

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
	maxInflight := 4
	if v := os.Getenv("EMBED_MAX_INFLIGHT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxInflight = n
		}
	}
	// EMBED_TRUNCATE_PROMPT_TOKENS: pass `truncate_prompt_tokens` to the
	// endpoint (vllm extension). -1 truncates overlong inputs to the model max
	// length instead of a 400; 0 (default) keeps the historical strict behavior.
	truncate := 0
	if v := os.Getenv("EMBED_TRUNCATE_PROMPT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			truncate = n
		}
	}
	c, err := embedding.New(embedding.Config{
		BaseURL:              envOr("EMBED_BASE_URL", "http://127.0.0.1:11434/v1"),
		Model:                envOr("EMBED_MODEL", "qwen3-embedding:0.6b"),
		APIKey:               os.Getenv("EMBED_API_KEY"),
		Timeout:              30 * time.Second,
		MaxInflight:          maxInflight,
		Usage:                usage,
		TruncatePromptTokens: truncate,
	})
	if err != nil || c == nil {
		logger.Warn("hybrid arm: embedding client unavailable; semantic signal disabled (degrades to BM25+entity)")
		return nil
	}
	// Absorb transient sidecar faults (connection reset / timeout) so eval
	// retrieval stays honestly three-signal; see retryingEmbedder.
	return newRetryingEmbedder(c, 3, 200*time.Millisecond, logger)
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

func formalRepeatScoresVisible(opt options) bool {
	return opt.formalProtocol == nil && !opt.unifiedPairAudit
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
// estimate question counts. Category and enumeration filters apply before
// normal questions obey maxQuestions; the separately configured adversarial
// tail remains eligible after that limit.
func selectQuestions(conv conversation, opt options) []selectedQuestion {
	selected := make([]selectedQuestion, 0, len(conv.QA))
	answered, adversarial := 0, 0
	for index, qa := range conv.QA {
		if opt.onlyQuestions != nil && !opt.onlyQuestions[qa.QuestionID] {
			continue
		}
		if opt.onlyCategory > 0 && qa.Category != opt.onlyCategory {
			continue
		}
		if opt.onlyEnumeration && !memory.ParseEnumerationIntent(qa.Question).IsEnumeration {
			continue
		}
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

// readQuestionWhitelist parses a `conv-N-q-M` question-ID whitelist (one per
// line, `#`-prefixed lines ignored) into a set. Empty files are an error so a
// typo'd path or empty residual list cannot silently run the full dataset.
func readQuestionWhitelist(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --only-questions: %w", err)
	}
	set := make(map[string]bool)
	for _, ln := range strings.Split(string(raw), "\n") {
		id := strings.TrimSpace(ln)
		if id == "" || strings.HasPrefix(id, "#") {
			continue
		}
		if set[id] {
			return nil, fmt.Errorf("--only-questions file %q contains duplicate question ID %q", path, id)
		}
		set[id] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("--only-questions file %q contains no question IDs", path)
	}
	return set, nil
}

func validateQuestionWhitelistCoverage(convs []conversation, opt options) error {
	if opt.onlyQuestions == nil {
		return nil
	}
	selected := make(map[string]bool, len(opt.onlyQuestions))
	for _, conv := range convs {
		for _, item := range selectQuestions(conv, opt) {
			selected[item.QA.QuestionID] = true
		}
	}
	var missing []string
	for id := range opt.onlyQuestions {
		if !selected[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("--only-questions contains %d ID(s) absent from the selected dataset/cohort: %s", len(missing), strings.Join(missing, ", "))
	}
	return nil
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

func estimateReport(convs []conversation, opt options, prices priceTable, model, extractModel, judgeModel string) costReport {
	if judgeModel == "" {
		judgeModel = model
	}
	plan := buildCallPlan(convs, opt)
	report := costReport{ByRole: map[string]*roleCost{
		"extract": estimateRole(prices, extractModel, plan.ExtractionCalls, plan.ExtractionCalls*estimateExtractIn, plan.ExtractionCalls*estimateExtractOut),
		"answer":  estimateRole(prices, model, plan.AnswerCalls, plan.AnswerInTokens, plan.AnswerOutTokens),
		"filter":  estimateRole(prices, model, plan.FilterCalls, plan.FilterInTokens, plan.FilterOutTokens),
		"judge":   estimateRole(prices, judgeModel, plan.JudgeCalls, plan.JudgeInTokens, plan.JudgeOutTokens),
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
	if _, ok := prices.Lookup(judgeModel); !ok && judgeModel != model && judgeModel != extractModel {
		report.UnpricedModels = append(report.UnpricedModels, judgeModel)
	}
	sort.Strings(report.UnpricedModels)
	return report
}

func estimateDatasetCost(convs []conversation, opt options, prices priceTable, model, extractModel, judgeModel string) float64 {
	return estimateReport(convs, opt, prices, model, extractModel, judgeModel).EstimatedUSD
}

func printEstimate(convs []conversation, opt options, prices priceTable, model, extractModel, judgeModel string) error {
	plan := buildCallPlan(convs, opt)
	report := estimateReport(convs, opt, prices, model, extractModel, judgeModel)
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
	if stats.SweepQuestions > 0 {
		fmt.Printf("  %-24s %d/%d  %5.1f%%\n", "SWEEP_OVER_BUDGET", stats.SweepOverBudget, stats.SweepQuestions, stats.SweepOverBudgetRate*100)
	}
}
