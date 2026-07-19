# Data Model: engram CLI (AI-first) — Phase 1

The CLI introduces **no new persisted data** — it renders and mutates the engine's
existing `memory.Entry` records through the public API. The "entities" below are
the adapter's in-process shapes and invariants.

## Config

Resolved once per invocation from flags (win) then `ENGRAM_*` env.

| Field | Flag | Env | Default | Notes |
|-------|------|-----|---------|-------|
| Data dir (**required**) | `--data-dir` | `ENGRAM_DATA_DIR` | — | root holding all `<ns>.db` |
| Namespace | `--namespace` | `ENGRAM_NAMESPACE` | `default` | validated (see Namespace) |
| Embedding base URL | `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | (offline) | |
| Embedding model | `--embed-model` | `ENGRAM_EMBED_MODEL` | (offline) | |
| Embedding API key | — | `ENGRAM_EMBED_API_KEY` | — | **env-only** |
| LLM base URL | `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | (no ingest) | |
| LLM model | `--llm-model` | `ENGRAM_LLM_MODEL` | (no ingest) | |
| LLM provider | `--llm-provider` | `ENGRAM_LLM_PROVIDER` | — | `openai`\|`anthropic` |
| LLM API key | — | `ENGRAM_LLM_API_KEY` | — | **env-only** |

- **Invariant**: API keys never appear in stdout, stderr, or any tracked file.
- **Invariant**: names mirror the MCP server's `ENGRAM_*` set (operator
  consistency); the loader is the CLI's own (no `mcpserver` import).
- **Degradation**: absent embedding → offline retrieval + honest `semantic
  unavailable` marker. Absent LLM → `ingest` unavailable (clear diagnostic); other
  commands unaffected.

## Namespace

- **Shape**: a non-empty string selecting an isolated store at
  `<data-dir>/<namespace>.db`.
- **Validation**: `^[A-Za-z0-9._-]{1,64}$`; reject `.`, `..`, and any path
  separator; resolve+clean the path and assert it stays inside the data directory.
- **Invariant (SC-004)**: any rejected namespace creates **zero** files outside the
  data directory.
- **Ownership**: validator lives in the CLI adapter (engine is namespace-agnostic;
  no import of the MCP validator).

## Engine Handle (in-process, per invocation)

Assembled from Config via public API; closed before exit.

- **Members**: `*store.Store`, `*memory.EntryStore`, `*memory.VectorStore`,
  `*memory.Embedder` (nil-embedding-safe), `*memory.Retriever`,
  `*pipeline.Pipeline` (nil when no LLM).
- **Lifecycle invariant (FR-008/SC-003)**: `Close()` drains the async embedder
  (`Embedder.Close()` → `wg.Wait()`) then closes the store, so any vector queued by
  `add`/`ingest` is durably written before process exit. No-op drain when embedding
  is absent.

## Command I/O (rendered, not persisted)

Each command consumes flags/args + stdin, produces a markdown document on stdout,
and a diagnostic + non-zero code on failure.

| Command | Input | Success output (markdown) |
|---------|-------|---------------------------|
| `add` | `--name --content [--trigger --category --pinned]` | write confirmation (name) |
| `ingest` | conversation turns via stdin | extracted count + new entry names |
| `delete` | `<name>` | deletion confirmation |
| `search` | `<query> [--limit]` | ranked hits: name · score · snippet · content + degraded marker |
| `get` | `<name>` | full record (stable fields): name, content, trigger, category, pinned, durability, hits, timestamps |
| `list` | — | all entries, pinned-first, importance-ordered |
| `stats` | — | entry count, non-pinned count, manifest-size estimate |
| `export` | — | `memory.RenderExport` over all entries |
| `namespaces` | — | list of `<ns>` present under data dir |
| `version` | — | build version string |

- **Stable-field rule**: `get`/`list`/`export` surface only the **stable** Entry
  subset now. Provisional fields (`EventStart`/`EventEnd` and later additions) are
  serialized in the **final** implementation step, against the then-frozen shape.
- **Determinism**: multi-entry renders sort deterministically (pinned first, then
  name) so output is diff- and test-stable.

## Exit / Diagnostic model

- `0` success; non-zero on any failure.
- Diagnostic form: `<what went wrong> — <next action>` to **stderr**; stdout stays
  clean (document-only) so a piping agent gets an uncontaminated payload.
- Distinct non-zero codes for usage error vs not-found vs missing-capability (LLM)
  vs engine error — enumerated and frozen in `contracts/cli-commands.md`.
