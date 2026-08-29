# engram MCP reference

Use this reference only after preflight has shown that a connected MCP server
actually exposes the required engram tool. The authoritative names are in
[`contract.json`](contract.json); do not infer tools from prose or configuration.

## Always registered tools

| Tool | Required input | Optional input | Result evidence |
|---|---|---|---|
| `memory_write` | `name`, `content` | `namespace`, `trigger`, `category`, `pinned` | `name`, `written:true` |
| `memory_search` | `query` | `namespace`, positive `limit` (default 8) | `scope`, `limit`, `returned`, ranked `results`, degradation |
| `memory_list` | — | `namespace` | `entries` |
| `memory_get` | `name` | `namespace` | `entry`, or a tool error when absent |
| `memory_delete` | `name` | `namespace` | `deleted:true` or `deleted:false` |
| `memory_ingest_v2` | `session_id`, source-identified ordered `messages` | `namespace`, `extract` (default `true`) | persisted Evidence receipts, extracted entries, degradation |
| `memory_evidence_get` | ordered `evidence_ids` | `namespace` | requested active Evidence in the same order |
| `memory_evidence_tombstone` | `evidence_id`, `reason_code` | `namespace`, `request_id` | tombstoned state |
| `memory_evidence_restore` | `evidence_id`, `reason_code` | `namespace`, `request_id` | active state; derived views stay stale |
| `memory_evidence_purge` | `evidence_id`, `reason_code` | `namespace`, `request_id` | purged state and checkpoint status |

Pass the selected namespace on every relevant call. Empty input resolves to
`default`, but report it explicitly. A valid namespace matches
`^[A-Za-z0-9._-]{1,64}$`; reject `.`, `..`, `/`, and `\\` before a call.

`memory_write` is an upsert, so it requires explicit user intent and a clear
name/content target. Do not silently persist ordinary conversation. Because the
upsert is keyed by name, updating an existing entry without passing `pinned`
resets that entry's pin to unset; re-pass `pinned` when a previously pinned
memory must stay pinned. Do not send likely credentials or secrets in `content`
or `trigger`. Content over the adapter budget (1,200 Unicode code points) and a
trigger over 120 single-line code points remain rejected; preserve that error
rather than truncating it.

## Search evidence semantics

Every successful `memory_search` response includes:

`query` guidance: search with **short keyword queries** — two to four content
terms naming the target fact, including the constraint/attribute vocabulary
when one governs the task (`allergy 过敏`, `timezone 时区`, `naming convention`,
`preference 偏好`), not a full sentence or question and not only the task
topic. Long mixed-language queries match nothing; if a keyword query returns
empty, retry once with the most distinctive single term (a name, brand, or
domain word) before reporting empty.

```json
{
  "scope": "ranked_subset",
  "limit": 8,
  "returned": 1,
  "results": [
    {
      "id": "...",
      "projection_id": "...",
      "projection_kind": "...",
      "source_session_id": "...",
      "name": "...",
      "content": "...",
      "event_date": null,
      "created_at": "..."
    }
  ],
  "degraded": {"semantic": true, "reason": "..."}
}
```

`limit` is the effective top-k bound and `returned` equals the number of result
objects. `scope:"ranked_subset"` means the response does not report an exhaustive
match count. Reaching the limit, returning fewer rows, returning no rows, or
reporting semantic degradation does not prove a requested fact absent.

`id` is the stable entry ID. Projection and source-session fields are empty when
the engine result has no such public provenance. `event_date` is an event-time
hint when present; `created_at` is entry storage time and must not be silently
used as the event date. Search rank, result-array order, and `created_at` also do
not establish event order. Read [the evidence guidance](evidence-guidance.md)
before using these results to answer a user.

## Lossless and conditional ingest

`memory_ingest_v2` is always registered. Use it only after explicit user
intent and only with caller-provided stable `session_id`, per-turn `source_id`,
zero-based ordinal, and `user`/`assistant` roles. It saves original source
content before attempting optional extraction. When no LLM is configured, it
returns `extracted_count:0` and `degraded:["extraction_unavailable"]`: that is
a successful source write, not a fact-extraction claim. Do not place secrets in
source text, source IDs, request IDs, or lifecycle reason codes.

`memory_ingest` exists only when it appears in the current `tools/list`; its
presence means the server was configured with an LLM caller. It accepts:

```json
{
  "namespace": "default",
  "messages": [
    {"role": "user", "text": "..."},
    {"role": "assistant", "text": "..."}
  ]
}
```

Roles are only `user` and `assistant`. Execute it only after an explicit user
request to ingest and report the returned `extracted_count` and entries. A zero
count is not a successful extraction claim. If the tool is absent, say that
ingest needs an LLM-configured engram MCP server; do not guess another name or
fall back to a different state-changing operation.

## Offline, errors, and non-tools

The ten always-registered tools work without embedding or LLM endpoints.
Offline search can return `degraded.semantic:true` with a reason; report that
returned structural fact, but do not probe engine-internal signal failures.
An empty search/list, `memory_get` tool error, `deleted:false`, validation
error, unavailable Evidence, or adapter error stays empty, not-found, blocked,
or failed—not success.

MCP tool annotations describe read-only, destructive, idempotent, and open-world
behavior for planning. They are hints, not authorization. Ingest tools may be
marked open-world because optional extraction can call a configured replaceable
model endpoint; this does not make any endpoint a server startup requirement.

These MCP tools do **not** exist and must never be called:

```text
memory_curate
memory_stats
memory_export
memory_namespaces
memory_version
```

Curation on MCP is an optional server-startup configuration, not a tool. This
skill neither enables it nor treats a normal write or ingest as proof that a
curation pass changed anything.
