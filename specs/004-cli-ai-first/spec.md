# Feature Specification: engram CLI (AI-first)

**Feature Branch**: `004-cli-ai-first`

**Created**: 2026-07-20

**Status**: Draft

**Input**: User description: "AI-first CLI adapter over the engram engine: engram add/ingest/delete/search/get/list/stats/export/namespaces/version, AI-friendly markdown output and diagnostics, offline-by-default, engine untouched, no mcpserver import"

## Overview

`engram` is a command-line surface over the engram memory engine whose **primary
consumer is an AI agent** (e.g. a coding assistant shelling out to
`engram search "..."` and reading the result), not a human at a terminal and not
a shell script parsing JSON. Its defining property is an **AI-first output and
error contract**: every command emits an AI-friendly markdown document, and every
failure emits an AI-friendly diagnostic naming the problem and the next action.
It is the engine's third thin adapter after the MCP server and reuses the same
frozen memory semantics.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Offline memory read/write from the command line (Priority: P1)

An AI agent (or a person driving one) needs to store durable facts and retrieve
them later, from a plain command invocation, with no network, embedding, or LLM
endpoint required. It runs `engram add`, `engram search`, `engram get`,
`engram list`, and `engram delete` against a local data directory and reads back
compact markdown it can act on.

**Why this priority**: This is the minimum viable memory CLI. Without offline
read/write there is no product; every other command builds on this store.

**Independent Test**: Point the CLI at an empty temp data directory with no
endpoints configured; `add` a memory, `search`/`get`/`list` it back, `delete` it,
and confirm it is gone — all offline, asserting the markdown output names the
stored fact.

**Acceptance Scenarios**:

1. **Given** an empty data directory and no endpoints, **When** `engram add` stores a named memory, **Then** the command exits 0 and confirms the write in markdown.
2. **Given** a stored memory, **When** `engram search` is run with a matching query, **Then** the matching memory appears in the markdown result with its name and content.
3. **Given** a stored memory, **When** `engram get <name>` is run, **Then** its full record is rendered as a markdown document.
4. **Given** several stored memories, **When** `engram list` is run, **Then** all are rendered, importance-ordered (pinned first).
5. **Given** a stored memory, **When** `engram delete <name>` is run and then `engram get <name>`, **Then** delete exits 0 and the subsequent get reports the memory not found.
6. **Given** no endpoints configured, **When** `engram search` runs, **Then** the output honestly marks the semantic signal as unavailable (degraded) without failing.

---

### User Story 2 - Extract durable facts from a conversation (Priority: P2)

When an LLM endpoint is configured, an AI agent feeds recent conversation turns to
`engram ingest` and the CLI extracts durable facts into the store, so memory can
be grown from dialogue rather than only hand-written entries.

**Why this priority**: Ingestion is the engine's differentiator, but it depends on
an external LLM and on US1's store, so it follows the offline MVP.

**Independent Test**: With a stubbed/local LLM configured, pipe a short
conversation to `engram ingest`; confirm new entries appear via `engram list` and
the command reports how many facts were extracted. With no LLM configured, confirm
`ingest` fails with an AI-friendly diagnostic and a non-zero exit code.

**Acceptance Scenarios**:

1. **Given** an LLM endpoint is configured, **When** `engram ingest` receives conversation turns, **Then** durable facts are stored and the command reports the extracted count in markdown.
2. **Given** no LLM endpoint is configured, **When** `engram ingest` is invoked, **Then** it exits non-zero with a diagnostic stating an LLM is required and how to configure one.
3. **Given** an embedding endpoint is also configured, **When** `ingest` (or `add`) completes, **Then** the semantic vector for each new entry is durably written before the process exits.

---

### User Story 3 - Inspect and export a memory store (Priority: P3)

An operator or agent needs to understand and back up a store: how many memories
exist, which namespaces are present, and a full human/agent-readable export.

**Why this priority**: Operability and backup add real value but are not required
to store and retrieve memories; they layer on top of US1.

**Independent Test**: Against a store with a known set of memories across two
namespaces, run `engram stats`, `engram namespaces`, `engram export`, and
`engram version`; assert the counts, the namespace list, and that the export
contains every entry as markdown.

**Acceptance Scenarios**:

1. **Given** a store with N memories, **When** `engram stats` is run, **Then** it reports the entry count and storage figures in markdown.
2. **Given** two namespaces exist under the data directory, **When** `engram namespaces` is run, **Then** both are listed and no others.
3. **Given** a store with memories, **When** `engram export` is run, **Then** every entry is rendered as a single deterministic markdown document.
4. **When** `engram version` is run, **Then** it prints the build version and exits 0.

---

### Edge Cases

- **Invalid namespace**: a namespace containing path separators, `.`/`..`, or
  outside the allowed pattern is rejected with a diagnostic; **no file is created
  outside the data directory**.
- **Missing name**: `get`/`delete` on a non-existent memory exits non-zero with a
  diagnostic that names the memory and suggests `engram list`.
- **Ingest without LLM**: rejected with an AI-friendly "LLM required" diagnostic.
- **Empty/oversized content**: `add` rejects content that violates engine budgets
  with a diagnostic stating the limit and the actual size.
- **One-shot vector loss**: because the process is short-lived, a write must not
  return success while its semantic vector is still only queued — the async
  embedder is drained before exit whenever embedding is configured.
- **Secrets**: an API key supplied via environment is never echoed into any
  command output or error.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The CLI MUST provide these commands: `add`, `ingest`, `delete`,
  `search`, `get`, `list`, `stats`, `export`, `namespaces`, `version`.
- **FR-002**: `add`/`search`/`get`/`list`/`delete` MUST operate with **no network,
  embedding, or LLM endpoint** configured (offline by default).
- **FR-003**: The command semantics that overlap the MCP tool set
  (`add`≙`memory_write`, `search`, `get`, `list`, `delete`, `ingest`) MUST be
  **behavior-preserving** — same inputs yield the same stored/returned memories.
- **FR-004**: Every command's success output MUST be an **AI-friendly markdown
  document** (deterministic where the underlying data is; importance-ordered,
  pinned entries first, for multi-entry views).
- **FR-005**: Every failure MUST emit an **AI-friendly diagnostic** to the error
  stream stating what went wrong and the next action, and MUST exit with a
  **non-zero** code; success exits `0`. Exit-code and diagnostic shape are part of
  the frozen contract.
- **FR-006**: `search` MUST report per-signal **degradation honestly** from
  structural facts (e.g. "no embedding endpoint configured → semantic unavailable")
  without failing the command.
- **FR-007**: `ingest` MUST require a configured LLM; absent one it MUST fail per
  FR-005 rather than silently no-op.
- **FR-008**: When embedding is configured, `add`/`ingest` MUST ensure each new
  entry's semantic vector is **durably written before the process exits** (the
  one-shot lifecycle drains the async embedder); when embedding is absent this is
  a no-op and the write still succeeds.
- **FR-009**: All reads/writes MUST be **namespace-isolated**: `--namespace`
  (default `default`) selects an independent store; the engine schema is unchanged.
- **FR-010**: Namespace identifiers MUST be validated (pattern
  `^[A-Za-z0-9._-]{1,64}$`, rejecting `.`/`..`/path separators) and the resolved
  store path MUST be asserted to stay inside the data directory; anything that
  could escape MUST be rejected with **zero files created outside the data
  directory**.
- **FR-011**: Non-secret configuration MUST accept a flag or an `ENGRAM_*`
  environment variable (flag wins); **API keys MUST be accepted via environment
  only** and MUST never appear in any output, error, or tracked file.
- **FR-012**: `export` MUST render **every** entry of the selected namespace as a
  single deterministic markdown document.
- **FR-013**: `namespaces` MUST enumerate exactly the namespaces present under the
  data directory and no others.
- **FR-014**: The CLI MUST NOT modify or bypass the engine — it interacts with the
  memory engine **solely through the engine's public API** and adds no memory
  algorithm of its own.

### Key Entities

- **Memory (Entry)**: the engine's unit of stored memory — name, content,
  trigger, category, pinned flag, usage/timestamps. The CLI serializes the
  **stable** subset for output; newer provisional fields are surfaced last,
  against the eventually-frozen shape.
- **Namespace**: an isolated store selected by name, materialized as one
  independent engine database under the data directory.
- **Data directory**: the root that contains all namespace stores; every resolved
  store path must stay inside it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the US1 read/write flow (`add`→`search`/`get`/`list`→
  `delete`) succeeds with **zero** network/embedding/LLM endpoints configured.
- **SC-002**: `search` results are **identical** to the engine's own retrieval for
  the same query and store (retrieval-fidelity parity), verified by an automated
  parity check.
- **SC-003**: After `add`/`ingest` with embedding configured, the new entry's
  semantic vector is present in the store in a subsequent invocation **100%** of
  the time (no vector lost to the one-shot lifecycle).
- **SC-004**: For every invalid/path-escaping namespace input, the number of files
  created outside the data directory is **0**.
- **SC-005**: 100% of error paths exit non-zero and emit a diagnostic that names
  the failure and a concrete next action.
- **SC-006**: The engine is untouched by this feature — `git diff --name-only --
  memory embedding provider store internal` is **empty** — establishing the change
  as a pure adapter (invariant-by-construction against the evaluation-regression
  gate, so no LoCoMo re-run is required).

## Assumptions

- The CLI is a **one-shot** process (each invocation opens the store, runs one
  command, and exits); there is no daemon or REPL in this feature.
- The `ENGRAM_*` environment variable **names** are reused from the MCP server for
  operator consistency, but the CLI carries its own configuration loading and does
  **not** import the MCP adapter.
- A `--json` machine-output mode is **out of scope** (YAGNI); the consumer is an AI
  that reads markdown well. It may be added later if a real machine consumer
  appears.
- An **SDK facade package** (a one-call assembled memory instance for external Go
  programs) is **out of scope** for this feature and deferred to a future one.
- Serialization of the engine's newer/provisional Entry fields is sequenced as the
  **last** implementation step, written against the then-frozen Entry shape; the
  CLI body first surfaces only stable fields.
- Namespace validation is intentionally held **within the CLI adapter** (the engine
  does not know about namespaces), mirroring — not importing — the MCP rule.
