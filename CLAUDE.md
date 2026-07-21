# engram

**Local-first embeddable memory layer**: one tuned memory engine (storage · hybrid retrieval · extraction · curation) plus thin integration surfaces (MCP server today; CLI/SDK later). Extracted from `workhorse-agent`'s memory subsystem; behavior-preserving.

## Architecture

Pure-Go library + thin adapters. The engine is host-agnostic and offline-capable; adapters only call the engine's public API. Governing law: [.specify/memory/constitution.md](.specify/memory/constitution.md).

```
memory/              # THE ENGINE (public packages) — entrystore/retriever/embedder/vectorstore/
                     #   entities/usagelog/snapshot/export/migrate/writer/queryplan
  pipeline/          #   ADD-only fact extraction from conversation turns
  curation/          #   deterministic scorer + near-dup dedup + LLM judge + leader-lease
  prompt/            #   extraction/curation prompt templates
embedding/           # ENGINE: embedding.Client + HTTPClient + Reranker (OpenAI-compatible /v1)
provider/            # ENGINE: LLM provider abstraction (+ anthropic/ + openai/)
store/               # ENGINE: SQLite (modernc, pure-Go) — Open/Options/migrations/ProbeFTS5
internal/            # ENGINE-internal: idgen/ (ID gen), version/ (UserAgent) — not for external use
mcpserver/           # ADAPTER: MCP stdio server over the engine (config/namespace/registry/server/tools/provider)
cmd/
  engram-mcp/        #   engram MCP server binary (thin main → mcpserver)
  locomo-bench/      #   LoCoMo eval harness (product regression + paper infra)
specs/               # spec-kit SDD: specs/NNN-feature/ spec·plan·tasks·research·data-model·contracts
docs/                # strategy, extraction background, freshness backlog, MCP/CLI guides
testdata/            # parity goldens; locomo/ dataset (gitignored, public, not redistributed)
.github/workflows/   # ci.yml: CGO=0 build + test + vet, Go 1.25
```

## Tech Stack

- **Language**: Go 1.25.0 (bumped from 1.22 for MCP SDK v1.5.0). **No CGO** — must stay pure-Go / cross-compilable.
- **Storage**: SQLite via `modernc.org/sqlite` (pure-Go). One `*sql.DB` per store, `SetMaxOpenConns(1)` (single-writer), WAL, FTS5 trigram.
- **Retrieval**: three-signal hybrid — semantic (cosine over stored vectors) + keyword (FTS5 BM25 + LIKE fallback) + entity (exact-match) — fused with RRF (k=60, tuning-free). Optional cross-encoder reranker.
- **MCP**: official `github.com/modelcontextprotocol/go-sdk` v1.5.0, **stdio only**. Schemas via struct reflection (`jsonschema` tags).
- **Model side (both optional/offline-degradable)**: embedding + LLM go through replaceable interfaces to local sidecars (Ollama / fastembed, OpenAI-compatible). Never hardcode a single provider into the engine.

## Build & Run

```bash
# Everything must build and test with CGO disabled (hard gate)
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...          # engine tests + mcpserver contract/parity/isolation

# MCP server (offline: no embedding/LLM endpoints needed)
go build ./cmd/engram-mcp && ./engram-mcp --data-dir ~/.engram/memory
#   config via flags or env: ENGRAM_DATA_DIR, ENGRAM_EMBED_BASE_URL/MODEL/API_KEY,
#   ENGRAM_LLM_PROVIDER/BASE_URL/MODEL/API_KEY, ENGRAM_MAX_OPEN_NAMESPACES
#   secrets ONLY via env — never a tracked file. memory_ingest tool appears only when LLM configured.

# LoCoMo eval (optional; needs dataset + endpoints)
go run ./cmd/locomo-bench --data <locomo.json> --run-dir ./.locomo-run --retrieval both
#   env: LOCOMO_API_KEY/BASE_URL/MODEL, LOCOMO_PROVIDER(anthropic|openai), EXTRACT_MODEL,
#        EMBED_BASE_URL/MODEL/API_KEY/RERANK_MODEL. run-dir + dataset are gitignored.
```

## Constitution — the five non-negotiables

Full text: [.specify/memory/constitution.md](.specify/memory/constitution.md) (v1.0.0). Every spec/plan/PR review MUST check against these:

1. **Local-first, offline by default** — core paths run with no network/cloud; SQLite local files; embedding/LLM are replaceable local sidecars, never a required hosted service. Any online capability is explicitly opt-in, default-off.
2. **Engine/adapter separation** — the engine is an independent, host-free, unit-testable library. Host-specific logic lives ONLY in thin adapters (MCP/CLI/SDK), which interact solely through the engine's public API. Adding an integration surface MUST NOT require changing engine internals.
3. **Contract-first & namespace isolation** — freeze the outward contract (API shape, error semantics, schema) in spec/plan before implementing; breaking changes bump MAJOR + migration notes. All memory reads/writes are namespace-isolated; cross-namespace access is explicit and off by default.
4. **Evaluation regression gate (NON-NEGOTIABLE)** — any change touching retrieval/extraction/curation/storage/embedding MUST run comparable-metric eval before merge and MUST NOT regress the baseline (LoCoMo answerable mean). Intended metric changes declare a new baseline + rationale. Eval-config changes commit separately from algorithm changes (attribution).
5. **Graceful degradation & honest scale** — multi-signal features degrade per-signal, never fail whole. Scale/limits are stated honestly (current: single-user ~100k-entry class; no million-token claims); over-boundary work (ANN/quantization) is explicit future work, not an implicit promise.

## Key Conventions

- **The engine is untouchable from adapter work (hard)**: an MCP/CLI/SDK feature MUST NOT modify any `.go` under `memory/ embedding/ provider/ store/ internal/`. If an adapter genuinely needs a public entry the engine lacks, STOP and surface it as an explicit increment to the engine's contract — never bypass the engine or reimplement its algorithms in the adapter. Verify with `git diff --name-only -- memory embedding provider store internal` (must be empty for adapter features).
- **Schema truth = migrations, not a DDL file**: `store/migrations.go` is the authority — `migrationsByVersion` (v1 = memory store + FTS5 mirror + curation lease; v2 = hybrid: event_date/fact_source columns + `memory_embeddings` + `memory_entities`). `schema_version` bumps per applied migration inside its own tx. Memory tables have **no** FK to sessions (`source_session_id` is plain TEXT). Never edit a shipped migration; add a new version.
- **Namespace isolation is an adapter concern (MCP)**: one namespace → one independent engine store (`<dataDir>/<ns>.db`), managed by a `namespace→handle` registry with LRU bound + eviction. The engine does not know about namespaces; its schema stays unchanged. Validate namespace ids (whitelist `^[A-Za-z0-9._-]{1,64}$`, reject `.`/`..`/separators, assert the joined path stays inside dataDir) — path-escape reads/writes = 0.
- **Retrieval degradation is silent-by-design in the engine**: absent signals drop out of the RRF sum (nil embedding client → keyword+entity; no entities → keyword only). Adapters report degradation only from **structural** facts they honestly know (e.g. "no embedding endpoint configured"), never by probing the engine — the engine intentionally swallows per-signal failures.
- **Typed-nil discipline**: engine constructors like `embedding.New` / `NewReranker` return a **concrete** `nil` + `nil` error when unconfigured. Collapse it to an untyped-nil interface at the boundary (`if c == nil { return nil, nil }`) before storing as an interface — otherwise `iface == nil` is false and a nil pointer gets dereferenced.
- **Secrets**: API keys flow through env → provider only. Never log, serialize into a tool response, or write to a tracked file. `.locomo-run/`, `*.db`, `testdata/locomo/` are gitignored.
- **No paid cloud rerank/recall model as a scoring lever (HARD — death rule, non-negotiable)**: NEVER use a hosted reranker/recall model (e.g. DashScope `gte-rerank-v2`, or any cloud cross-encoder) as a means to *gain eval points* or as part of the default / shipped / recommended stack. Rationale: (1) it is **not pure client-side tech** — an integrator embedding engram cannot be assumed to have such a model, so a win that depends on it is not a portable win; (2) cloud rerankers are **expensive** (≈¥150 burned in a single day of eval). The engine's cross-encoder reranker stays strictly **optional, opt-in, default-off** (Constitution I & V); any real score improvement MUST come from pure-Go, offline-capable, client-side techniques. A lever that only clears its gate *with* a paid cloud reranker is NOT a valid win — do not report it as one, do not make it default, do not spend on it to chase points. (Diagnostic-only reranker runs, clearly labeled and never presented as the shipped path, are the sole exception.)

## Knowledge Map

Strategy & positioning: [docs/memory-strategy.md](docs/memory-strategy.md). Extraction provenance: [docs/background-extraction-from-workhorse-agent.md](docs/background-extraction-from-workhorse-agent.md). Unresolved freshness, state-consistency, memory-hallucination, and conditional-recall problem: [docs/memory-freshness-and-retrieval-policy.md](docs/memory-freshness-and-retrieval-policy.md) — this is a required future feature, **not a current capability**. MCP build/wire/modes: [docs/mcp-server.md](docs/mcp-server.md). CLI usage: [docs/cli.md](docs/cli.md). Per-feature detail lives in `specs/NNN-*/`. Delivered so far: **001** memory-engine-extraction, **002** mcp-server. Retrieval fidelity is proven by deterministic parity goldens (`testdata/parity/`) + the LoCoMo harness, not by trust.

## Working Rules

### Post-Edit Verification
- After each edit: `CGO_ENABLED=0 go build ./...` → zero errors before continuing. For touched packages, `CGO_ENABLED=0 go test -count=1 ./<pkg>`.
- Skip only for high-confidence trivial changes (comments/copy); when unsure, run it.

### Testing (test-first)
- Engine-behavior changes are TDD: write a failing test (contract/integration) before the implementation. No test = not done.
- Engine tests run offline (vectors stubbed / nil client). MCP contract tests use the SDK in-memory transport (`mcp.NewInMemoryTransports`) — no subprocess, no network.
- Deterministic parity (retrieval fidelity) and cross-namespace isolation are HARD gates and live in CI.

### Evaluation Regression Gate (Constitution IV — hard)
- Touching retrieval/extraction/curation/storage/embedding → run `cmd/locomo-bench` on a comparable slice and confirm no regression vs the current baseline before merge. A pure adapter that only calls unchanged engine paths is invariant *by construction* — prove it via parity (`memory_search` == direct `Retriever.Search`) + green engine tests instead of a full re-run, and say so.

### Long-Running Commands on WSL2 — MUST Detach (hard rule)
This machine is WSL2. The Bash tool detects completion by stdout EOF, not process exit; long children (locomo-bench, `ollama serve`, big test runs) inherit the pipe and appear to "hang" after finishing — `>log 2>&1` alone is NOT enough. Detach with `setsid`, poll with a single instant check (never a foreground `sleep` loop). Logs → session scratchpad.
```bash
setsid bash -c 'go run ./cmd/locomo-bench ... >run.log 2>&1; echo $? >run.exit' </dev/null >/dev/null 2>&1 & disown
[ -f run.exit ] && echo "exit=$(cat run.exit)" || tail -1 run.log   # poll
```

### Parallel-Feature Isolation (SDD, hard)
Spec-kit's single global active-feature pointer (`.specify/feature.json`) is silently stolen by a newer feature. Isolate: one git worktree per parallel feature (`.claude/worktrees/<feature>`), one working copy = one active feature. Pin within a shell via `export SPECIFY_FEATURE_DIRECTORY=specs/<NNN-feature>` (NOT `SPECIFY_FEATURE`). Before start / at plan / before merge: `git worktree list` + read each active sibling `specs/*/spec.md` and the surface it touches; reconcile overlap first.

### Concurrent Multi-Agent Editing — Never Discard Another Agent's Work (hard rule)
Implementation is often delegated to an external agent while this session reviews/backstops, and features run in parallel worktrees (e.g. 003 lives in `.claude/worktrees/bio-retrieval-locomo`). Work you didn't author is load-bearing until proven otherwise.
- **Never** discard/revert/reset/overwrite another agent's or another feature's changes, commits, or worktree without explicit user authorization. Route around unfamiliar code or ask.
- **Detect first**: `git status` / `git log --oneline -15` / `git worktree list` and read fresh before touching anything; when unsure whose work it is, assume another agent's.
- **On collision STOP and escalate** with exact files + line ranges + both intents + git evidence; never pick a winner.
- **Verify, don't trust reports**: when reviewing delegated work, independently re-run `CGO_ENABLED=0 go test -count=1 ./...`, confirm the engine is untouched via `git diff`, and check that parity/isolation tests actually assert (not self-comparing tautologies).

### Temporary Files
- All scratch/temp files (bench logs, downloads, drafts, throwaway probes) → the session scratchpad dir, never repo root / tracked paths / system `/tmp`. A throwaway harness placed in a source repo MUST be deleted and that repo restored to its exact prior git status.

### Exploration & Clarification
- Ideas/design/troubleshooting: explore freely. Major changes (new engine capability, new adapter surface, algorithm change) run `superpowers:brainstorming` first, then the spec-kit chain (`specify → plan → tasks → analyze → implement`).
- Unclear requirements/scope/approach → ask first, never guess.

### Preferred Workflow (maintainer's standing habit)
- Default sequence for any substantive feature: **brainstorm first (`superpowers:brainstorming`, TDD-minded — nail the failing-test/verification shape while designing), THEN the SDD chain (`specify → plan → tasks → analyze → implement`)**. Brainstorm converges the design + the free/cost gates; SDD formalizes it. Don't jump straight to `specify` for non-trivial work — the brainstorm comes first.

### Response Style
- Concise and direct, no filler. Report faithfully: failed test → say so + paste output; skipped step → say it was skipped; done + verified → state it plainly.

<!-- SPECKIT START -->
Active features live under `specs/NNN-*/`. Before implementing, read the relevant
feature's `plan.md` for tech context, structure, and conventions.
<!-- SPECKIT END -->
