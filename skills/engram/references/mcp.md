# engram MCP reference

Use this reference only after preflight has shown that a connected MCP server
actually exposes the required engram tool. The authoritative names are in
[`contract.json`](contract.json); do not infer tools from prose or configuration.

## Always registered tools

| Tool | Required input | Optional input | Result evidence |
|---|---|---|---|
| `memory_write` | `name`, `content` | `namespace`, `trigger`, `category`, `pinned` | `name`, `written:true` |
| `memory_search` | `query` | `namespace`, positive `limit` (default 8) | `results`, `degraded.semantic`, `degraded.reason` |
| `memory_list` | — | `namespace` | `entries` |
| `memory_get` | `name` | `namespace` | `entry`, or a tool error when absent |
| `memory_delete` | `name` | `namespace` | `deleted:true` or `deleted:false` |

Pass the selected namespace on every relevant call. Empty input resolves to
`default`, but report it explicitly. A valid namespace matches
`^[A-Za-z0-9._-]{1,64}$`; reject `.`, `..`, `/`, and `\\` before a call.

`memory_write` is an upsert, so it requires explicit user intent and a clear
name/content target. Do not silently persist ordinary conversation. Do not send
likely credentials or secrets in `content` or `trigger`. Content over the
adapter budget (1,200 Unicode code points) and a trigger over 120 single-line
code points remain rejected; preserve that error rather than truncating it.

## Conditional ingest

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

The five always-registered tools work without embedding or LLM endpoints.
Offline search can return `degraded.semantic:true` with a reason; report that
returned structural fact, but do not probe engine-internal signal failures.
An empty search/list, `memory_get` tool error, `deleted:false`, validation
error, or adapter error stays empty, not-found, blocked, or failed—not success.

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
