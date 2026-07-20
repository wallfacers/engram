# Research: engram CLI (AI-first) — Phase 0

All Technical-Context unknowns resolved below. No `NEEDS CLARIFICATION` remain.

## R1 — CLI framework: stdlib `flag` vs cobra/urfave

- **Decision**: Standard library only — `flag` for global flags + a hand-written
  subcommand dispatch table. No third-party CLI framework.
- **Rationale**: The command surface is small and fixed (10 commands, a handful of
  flags). The constitution values dependency minimization and CGO-free
  cross-compilation; adding cobra buys generated help/completion we do not need and
  a dependency to audit. `cmd/locomo-bench` and `cmd/engram-mcp` already dispatch
  with stdlib only — consistency.
- **Alternatives**: cobra (rich help/completion — rejected: unneeded weight);
  urfave/cli (similar — rejected). Revisit only if the command surface grows large
  or shell-completion becomes a requirement.

## R2 — Where does the engine-assembly logic live?

- **Decision**: The CLI re-expresses the ~15-line assembly
  (`store.Open` → `NewEntryStore`/`NewVectorStore`/`NewEmbedder`/`NewRetriever` →
  `pipeline.New`) in its own `cmd/engram/engine.go`. It does **not** import
  `mcpserver.Registry` and does **not** push the assembly into the engine.
- **Rationale**: Constitution II forbids adapter→adapter coupling, so importing
  `mcpserver` is out. The assembly is *use* of the engine's public API, not a
  reimplementation of engine internals, so duplicating it in the CLI adapter is
  compliant. The genuine shared home for "one call → a ready memory instance" is an
  **SDK facade package**, which is explicitly a **future feature** (out of scope
  here). Until then, two adapters each carrying the wiring is the correct,
  constitution-mandated shape — recorded so it is not mistaken for accidental
  duplication.
- **Alternatives**: (a) import mcpserver — rejected (II violation); (b) add an
  engine-level `Assemble`/facade now — rejected (scope creep into the engine for an
  adapter feature; belongs to the deferred SDK feature, designed against a frozen
  API); (c) share via an `internal/` package imported by both adapters — rejected
  (still couples the two adapter surfaces through shared mutable code; the facade is
  the honest boundary and it is deferred).

## R3 — One-shot lifecycle: async embedder must be drained before exit

- **Decision**: `add`/`ingest` write synchronously, then — when embedding is
  configured — the CLI drains the async embedder before the process exits, via
  `defer handle.Close()` where `Close()` calls `Embedder.Close()` (which
  `wg.Wait()`s the drain goroutine). When embedding is absent (nil client) this is
  a no-op and the write still succeeds.
- **Rationale**: The MCP server is long-lived, so `memory.Embedder.Enqueue(name)` +
  a background drain is sufficient there. A one-shot CLI that returns immediately
  after `Enqueue` would exit before the drain goroutine runs, **silently dropping
  the semantic vector**. `Embedder.Close()` already blocks on `wg.Wait()` after
  closing the channel, giving an exact synchronous flush with no engine change.
- **Verification**: `lifecycle_test.go` (FR-008/SC-003) — with a stub embedding
  client, `add` then a fresh open + retrieve must show the vector present.
- **Alternatives**: call `Embedder.Backfill(ctx)` then block — rejected as
  redundant: `Backfill` re-enqueues *all* missing-model names and still relies on
  the same drain; a single `Enqueue` on write + `Close()` drain is the minimal
  correct path. (Guard: `Backfill`/`Enqueue` both no-op on a nil embedder; the CLI
  only constructs the embedder when an embedding client is configured.)

## R4 — Namespace validation: held per-adapter

- **Decision**: The CLI carries its own namespace validator in
  `cmd/engram/namespace.go`: pattern `^[A-Za-z0-9._-]{1,64}$`, reject `.`/`..`/path
  separators, resolve `<data-dir>/<ns>.db` and assert the cleaned path stays inside
  the data directory. Same *rule* as MCP, **not** an import of MCP's code.
- **Rationale**: Constitution III makes namespaces an adapter concern — the engine
  does not know about them, so the validator must not go into the engine. And
  Constitution II forbids importing the MCP adapter. Therefore each adapter holds
  the rule. The rule is a security boundary (path-escape = 0), so it is tested
  directly per adapter rather than trusted across a shared import.
- **Alternatives**: engine-level namespace helper — rejected (III: engine must stay
  namespace-agnostic); import mcpserver.normalizeNamespace — rejected (II).

## R5 — Output & error contract (AI-first)

- **Decision**: Success output is deterministic **markdown** in the `RenderExport`
  house style (headers, importance-ordered, pinned-first, pure-function render);
  `export` reuses `memory.RenderExport` directly, other read commands render in the
  adapter. **No `--json`**. Errors go to **stderr** as an AI-friendly diagnostic
  (`<what went wrong> — <next action>`, e.g. `memory "foo" not found — run: engram
  list`) with a **non-zero exit code**; success is exit `0`. `stdout` carries only
  the command's document so a piping agent gets a clean payload.
- **Rationale**: The consumer is an AI that reads markdown natively and benefits
  from an actionable next-step on failure (self-correction). JSON would be
  token-heavier and is unneeded (no machine parser). Stream separation
  (doc→stdout, diagnostics→stderr) mirrors the MCP server's stdout/stderr
  discipline and keeps output pipeable.
- **Note on `RenderExport` header**: `memory.RenderExport` currently emits a
  `# workhorse-agent memory export` header (extraction-provenance leftover). It is
  engine code and out of scope to change here; `export` uses it verbatim. If a
  branded header is wanted, that is a separate **engine** contract increment, not
  adapter work.
- **Alternatives**: `--json`/JSON-default — rejected (YAGNI, no consumer);
  human-table output — rejected (not the AI consumer's best format).

## R6 — Testing strategy & the evaluation-regression gate

- **Decision**: Each subcommand is a pure `run(args, stdin, stdout, stderr) int`
  tested **in-process** (fast, deterministic, CGO=0). A single `e2e_test.go` builds
  the real binary and runs it via `os/exec` as an end-to-end smoke. `parity_test.go`
  asserts `engram search` results equal a direct `Retriever.Search` on the same
  store (SC-002). All engine-independent tests run **offline** (nil embedding/LLM;
  ingest uses a stub `pipeline.ModelCaller`).
- **Rationale (Constitution IV, by construction)**: A pure adapter over unchanged
  engine retrieval/extraction/storage paths cannot move the LoCoMo metric. Proof is
  mechanical: (a) SC-002 parity == engine's own retrieval, (b) engine unit suite
  green, (c) `git diff --name-only -- memory embedding provider store internal`
  empty (SC-006). This substitutes for a full LoCoMo re-run, per CLAUDE.md's
  invariant-by-construction clause.
- **Alternatives**: full LoCoMo re-run — unnecessary (no engine path changed);
  binary-only e2e for everything — rejected (slow, poor failure localization vs
  in-process `run()`).
