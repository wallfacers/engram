# Contract: Memory Workflow Routing

**Feature**: `020-engram-agent-skill`

**Default namespace**: `default`

## 1. Preflight and surface selection

For every triggered request:

1. Classify one memory intent. If it is only about RAM, cache, a generic database or transient chat
   context, do not apply this skill.
2. Inspect the currently connected tool list for the real engram MCP names. Do not infer connection from
   prose or configuration examples.
3. If the required MCP tool is present, use it and do not duplicate the operation through CLI.
4. Otherwise, check whether `engram` is executable. Use `engram version` for a non-mutating CLI probe;
   do not depend on `engram --help`.
5. For CLI-only operations reached from an MCP session, confirm the CLI data dir and namespace before
   switching surfaces.
6. If neither surface provides the required capability, block with the exact missing dependency.

MCP-first is an operation-level rule, not permission to combine MCP and CLI stores.

## 2. Intent mapping

| Intent | MCP path | CLI path | State change | Required checks |
|---|---|---|---:|---|
| write/update | `memory_write` | `engram [global flags] add --name <name> --content <text> [--trigger <text>] [--category <value>] [--pinned]` | yes | explicit content/name; one namespace; budget; no secret |
| search | `memory_search` | `engram [global flags] search "<query>" [--limit <n>]` | no | limit > 0; preserve empty/degraded |
| exact get | `memory_get` | `engram [global flags] get <name>` | no | preserve not found |
| list | `memory_list` | `engram [global flags] list` | no | one namespace |
| delete | `memory_delete` | `engram [global flags] delete <name>` | yes | explicit name and intent; preserve `deleted:false`/not found |
| ingest | conditional `memory_ingest` | role-tagged stdin into `engram [global flags] ingest` | yes | explicit request; LLM available; roles only user/assistant |
| curate | unavailable | `engram [global flags] curate` | yes | explicit request; LLM available; CLI store identity confirmed |
| stats | unavailable | `engram [global flags] stats` | no | CLI store identity confirmed |
| export | unavailable | `engram [global flags] export` | no | CLI store identity confirmed; do not expose secrets |
| namespace discovery | unavailable | `engram --data-dir <dir> namespaces` | no | data dir explicitly known |
| version | unavailable | `engram version` | no | no data dir required |

`[global flags]` must occur before the command. The supported non-secret flags are:

```text
--data-dir
--namespace
--embed-base-url
--embed-model
--llm-base-url
--llm-model
--llm-provider
```

API keys are environment-only and must never appear in a generated command.

## 3. MCP contract

### Always registered

| Tool | Required input | Optional input | Evidence |
|---|---|---|---|
| `memory_write` | `name`, `content` | `namespace`, `trigger`, `category`, `pinned` | `name`, `written` |
| `memory_search` | `query` | `namespace`, `limit` | `results`, `degraded` |
| `memory_list` | — | `namespace` | `entries` |
| `memory_get` | `name` | `namespace` | `entry` or tool error |
| `memory_delete` | `name` | `namespace` | `deleted` |

### Conditional

`memory_ingest` is callable only when it appears in the connected tool list. Input is:

```json
{
  "namespace": "default",
  "messages": [
    {"role": "user", "text": "..."},
    {"role": "assistant", "text": "..."}
  ]
}
```

Roles are only `user` and `assistant`. Evidence is `extracted_count` and the actual new entries. Absence
of the tool means LLM ingest is unavailable; do not call a guessed name.

### Nonexistent tools

The skill must never call:

```text
memory_curate
memory_stats
memory_export
memory_namespaces
memory_version
```

MCP background curation is server startup configuration, not a tool. The skill does not enable or modify
it.

## 4. Namespace rules

- Empty adapter input resolves to `default`, but the agent still reports `default` explicitly.
- Valid IDs match `^[A-Za-z0-9._-]{1,64}$`.
- Reject `.`, `..`, any `/` or `\`, and any value the adapter rejects.
- Do not search multiple namespaces to improve recall.
- Do not infer that the same namespace string identifies the same database across MCP and CLI.
- A user-requested namespace switch begins a new explicit operation context.

## 5. Mutation and capability rules

The following require explicit user intent:

- write/update;
- delete;
- ingest;
- curate.

“Remember X” is explicit write intent. Ordinary conversation, an inferred preference, or skill activation
alone is not.

If target, namespace or destructive scope is ambiguous, ask before executing. Do not add a second generic
confirmation when the user's intent and target are already clear.

Ingest and curate additionally require an available LLM configuration. `curate` is synchronous,
single-namespace and bounded by the CLI; `status: completed` only means the pass completed, not that an
entry was necessarily merged or evicted. Ordinary CLI add/ingest does not run curation. MCP write/ingest
may notify background curation only when the server was already explicitly started with that opt-in mode.

## 6. Offline and degradation rules

- Write, search, get, list and delete remain valid without a hosted service.
- Missing embedding can degrade search to available local signals.
- For MCP, report the returned `degraded.semantic` and `reason`.
- For CLI, report its rendered degradation statement when present.
- Do not probe engine internals or convert an unknown per-signal failure into a definite claim.
- Never recommend a paid cloud reranker/recall model as a prerequisite or score lever.
- Missing LLM blocks only LLM-dependent intent; it does not make local CRUD unavailable.

## 7. Secret and content safety

Never persist or echo API keys, tokens, passwords, private keys or equivalent credentials. If a request
contains a likely secret:

1. stop before MCP/CLI write, ingest or export;
2. explain that provider secrets belong in existing environment variables;
3. offer to save a non-secret description only;
4. keep the secret out of commands, logs, examples, test fixtures and OperationEvidence.

Content limits inherited from the adapter are 1,200 Unicode code points per entry and 120 code points for
a single-line trigger. Preserve content-rejected errors and ask the user to shorten or split non-secret
content; do not silently truncate.

## 8. Evidence and response contract

Every completed or blocked operation reports, concisely:

```text
surface: mcp | cli
namespace: <id> | n/a
operation: <intent>
status: success | empty | not-found | degraded | blocked | failed
evidence: <actual structured result or concise output>
next step: <only when needed>
```

Rules:

- A write is successful only with `written:true` or CLI exit 0 plus the actual added name.
- A delete keeps `deleted:false` or CLI not-found distinct from success.
- Empty search/list output is evidence of no result, not permission to invent one.
- Get not-found remains not-found.
- Ingest reports the returned extracted count; zero is not rewritten as positive extraction.
- Adapter errors and nonzero CLI exit codes remain failures with the adapter's recommended next step.
- Never use a success from one surface to claim the other surface was updated.

## 9. Representative behavior cases

The release suite includes at least:

1. MCP write then search in one namespace.
2. CLI write then search with MCP absent.
3. MCP and CLI both available: one MCP write only.
4. CLI-only stats/export/namespaces/version with store identity checks.
5. Explicit ingest with and without `memory_ingest`.
6. Explicit curate with and without LLM.
7. Keyword search offline with embedding absent.
8. Empty result and exact get not-found.
9. Invalid `.`, `..`, separator and overlength namespace.
10. Secret-bearing write request.
11. MCP and CLI pointing at different data dirs.
12. Generic RAM/cache/database and transient-chat near misses.
