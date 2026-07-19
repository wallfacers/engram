# Contract: engram CLI commands (frozen)

This is the outward contract for `cmd/engram`. It is frozen before implementation
(Constitution III). Breaking changes bump MAJOR + migration notes.

## Global

```
engram [global flags] <command> [args]
```

- **Global flags**: `--data-dir` (required), `--namespace` (default `default`),
  `--embed-base-url`, `--embed-model`, `--llm-base-url`, `--llm-model`,
  `--llm-provider`. Flags win over the matching `ENGRAM_*` env var.
- **API keys**: `ENGRAM_EMBED_API_KEY`, `ENGRAM_LLM_API_KEY` — **env only**, never
  a flag, never echoed.
- **Streams**: the command's markdown document → **stdout**; diagnostics/logs →
  **stderr**. stdout is document-only (pipe-clean).
- **Startup**: never fails for a missing embedding/LLM endpoint (offline default).

## Exit codes (frozen)

| Code | Meaning |
|------|---------|
| `0` | success |
| `1` | engine / IO error |
| `2` | usage error (unknown command, bad/missing flag or arg) |
| `3` | memory not found (`get`/`delete`) |
| `4` | capability unavailable (`ingest` without LLM configured) |
| `5` | invalid namespace / path-escape rejected |
| `6` | content rejected by engine budget (too large / bad trigger) |

## Diagnostic form (frozen)

Single line to stderr: `<what went wrong> — <next action>`. Examples:

- `memory "foo" not found — run: engram list`
- `ingest requires an LLM — set ENGRAM_LLM_BASE_URL/MODEL/PROVIDER and ENGRAM_LLM_API_KEY`
- `invalid namespace "a/b" — allowed: ^[A-Za-z0-9._-]{1,64}$`
- `content rejected: limit=<N> actual=<M> — shorten the memory`

## Commands

### `add` — store a memory  (≙ MCP `memory_write`)

`engram add --name <name> --content <text> [--trigger <t>] [--category <c>] [--pinned]`

- stdout: `# added\n\n- name: <name>\n`
- errors: empty name → 2; budget → 6.
- If embedding configured: the entry's vector is drained to disk before exit.

### `search` — hybrid retrieval  (≙ MCP `memory_search`)

`engram search <query> [--limit <n>]` (default limit 8; `--limit <= 0` → 2)

- stdout: markdown list, each hit `## <name>` + `- score: <f>` + snippet + content;
  header line notes degraded state when semantic is unavailable.
- **Parity (SC-002)**: hit set/order == direct `Retriever.Search(query, limit)`.

### `get` — one memory  (≙ MCP `memory_get`)

`engram get <name>`

- stdout: full record markdown (stable fields only, see data-model).
- not found → 3.

### `list` — all memories  (≙ MCP `memory_list`)

`engram list`

- stdout: all entries, pinned-first then name; empty store → a valid empty doc.

### `delete` — remove a memory  (≙ MCP `memory_delete`)

`engram delete <name>`

- stdout: `# deleted\n\n- name: <name>\n`; not found → 3.

### `ingest` — extract facts from conversation  (≙ MCP `memory_ingest`)

`engram ingest`  — reads conversation turns from **stdin** (role-tagged lines or a
simple turn format defined in quickstart).

- requires LLM; absent → 4.
- stdout: `# ingested\n\n- extracted: <count>\n` + list of new entry names.
- If embedding configured: new vectors drained before exit.

### `stats` — store summary

`engram stats`

- stdout: `# stats` + entry count, non-pinned count, manifest-size estimate.

### `export` — full dump

`engram export`

- stdout: `memory.RenderExport` over all entries (deterministic markdown).

### `namespaces` — list namespaces

`engram namespaces`

- stdout: markdown list of `<ns>` present as `<data-dir>/*.db`, and no others.

### `version` — build version

`engram version`

- stdout: version string (from `internal/version`); exit 0.

## Invariants asserted by tests

- Offline: `add/search/get/list/delete` succeed with no endpoints (SC-001).
- Path-escape: every invalid namespace → 0 files outside data dir (SC-004).
- Every failure path → non-zero + diagnostic naming a next action (SC-005).
- Engine untouched: `git diff --name-only -- memory embedding provider store internal`
  empty (SC-006).
