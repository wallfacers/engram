# Feature 004 — engram CLI (AI-first) — Design

**Date**: 2026-07-20
**Status**: Approved (brainstorming) → feeds spec-kit specify → plan → tasks
**Author**: brainstorming session

## 1. Purpose & Positioning

`cmd/engram` is the **third thin adapter** over the engram engine (after the MCP
server). Its consumer is an **AI agent** — e.g. Claude Code invoking
`engram search "..."` and reading the result — **not** a human at a terminal and
**not** a shell script parsing JSON. The differentiator is therefore an
**AI-first output and error contract**: every command emits an AI-friendly
markdown document, and every failure emits an AI-friendly diagnostic that names
the problem and the next action.

This is a new integration *surface*, so per Constitution II it interacts with the
engine **solely through the engine's public API** and MUST NOT change any engine
internals.

## 2. Placement & Compliance (hard constraints)

- New directory `cmd/engram/` (plus `cmd/engram/internal/` if needed for
  renderers/config helpers). No engine files touched:
  `git diff --name-only -- memory embedding provider store internal` MUST be empty.
- **MUST NOT import `mcpserver`** — that is adapter→adapter coupling. Any wiring
  or policy the CLI shares in *spirit* with MCP (config env names, namespace
  validation) is re-expressed in the CLI adapter, not borrowed by import.
- Assembly goes through public constructors only:
  `store.Open` → `memory.NewEntryStore` / `NewVectorStore` / `NewEmbedder` /
  `NewRetriever` → `pipeline.New`. Composing these **is** using the public API
  (not reimplementing engine internals), which is compliant.

## 3. Command Surface (10 commands)

- **Write**: `add`, `ingest` (available only when an LLM is configured), `delete`
- **Read**: `search`, `get`, `list`, `stats`, `export`
- **Ops**: `namespaces` (enumerate `<data-dir>/*.db`), `version`

Semantics mirror the frozen MCP tool set one-for-one where they overlap
(behavior-preserving), extended with `stats` / `export` / `namespaces` / `version`
for human/agent operability.

## 4. Output Contract (AI-first)

- Each command emits an **AI-friendly markdown document** in the house style
  already validated by the engine's `memory.RenderExport` (deterministic,
  pinned-first, pure-function rendering). Read commands (`search`/`get`/`list`/
  `stats`) render in the adapter; `export` reuses `RenderExport` directly.
- **No `--json` flag** (YAGNI — the consumer is an AI that reads documents well;
  a JSON surface can be added later if a real machine consumer appears).
- **Errors are AI-friendly**: written to stderr, stating what went wrong and the
  next action (e.g. `memory "foo" not found — run: engram list`), with a non-zero
  exit code. Exit codes are part of the frozen contract.

## 5. Configuration

- Reuses the MCP `ENGRAM_*` env var **names** for consistency, via the CLI's own
  loader (not by importing mcpserver): `--data-dir` / `--namespace` and the
  embedding / LLM settings, flag-wins-over-env.
- **API keys via env only** — never a flag, never echoed into output, never
  written to a tracked file.
- Offline by default: with no embedding/LLM endpoint the CLI still runs
  `add`/`search`/`list`/`get`/`delete`; `ingest` requires an LLM and is otherwise
  a clear AI-friendly error.

## 6. One-Shot Lifecycle Correctness (load-bearing)

The MCP server is **long-lived**, so `add` can `embedder.Enqueue(name)` and let a
background goroutine embed. The CLI is a **one-shot** process: after `add`/`ingest`
it must **`defer embedder.Close()`** (which `wg.Wait()`s the drain) *before
exit*, or the semantic vector is silently never written. When offline (nil
embedding client) this is a no-op. This is a correctness trap unique to the
one-shot lifecycle and is captured explicitly so tests assert it.

## 7. Namespace Model

- `--namespace` (default `default`) resolves to `<data-dir>/<namespace>.db`.
- Validation `^[A-Za-z0-9._-]{1,64}$` plus a path-escape assertion (resolved path
  stays inside data-dir), same rule as MCP. Because the engine **does not know
  about namespaces** (Constitution III makes it an adapter concern), this ~10-line
  validator is **intentionally held per-adapter** rather than pushed into the
  engine — two adapters each carrying it is the compliant choice, not duplication
  to eliminate.

## 8. Testing

- Black-box or in-process: prefer each subcommand as a pure
  `run(args, stdout, stderr) int` so tests run in-process (fast, CGO=0-friendly);
  `os/exec` against the built binary for a thin end-to-end smoke.
- **Parity hard gate**: `engram search` results == direct `Retriever.Search`,
  proving the adapter did not alter retrieval semantics (satisfies Constitution IV
  invariant-by-construction — a pure adapter over unchanged engine paths, proven
  by parity + green tests rather than a full LoCoMo re-run).
- All engine-independent tests run offline (nil embedding/LLM).

## 9. Constitution Check

- **I Local-first / offline**: runs with no endpoints for the CRUD path. ✅
- **II Engine/adapter separation**: engine untouched; public API only; no
  mcpserver import. ✅
- **III Contract-first & namespace isolation**: CLI command/output/error/exit-code
  shape frozen in spec before implementation; per-namespace DB isolation. ✅
- **IV Evaluation regression gate**: pure adapter over unchanged engine retrieval;
  proven via parity + green tests, no LoCoMo re-run required. ✅
- **V Graceful degradation & honest scale**: per-signal degradation inherited from
  the engine; offline reported honestly from structural facts. ✅

## 10. Deferred (by agreement)

- **SDK facade package** (`engram.Open(dataDir, Options) *Memory`) — a future
  feature, not in 004.
- **`--json` output** — YAGNI until a real machine consumer exists.
- **Entry new-field serialization** (EventStart/EventEnd, etc.) — sequenced as the
  **last** implementation step, written against the *then-frozen* Entry shape; the
  CLI body first surfaces only stable fields.
