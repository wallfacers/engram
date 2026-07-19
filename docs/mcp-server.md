# engram MCP server — build & wire into an MCP client

`engram-mcp` exposes the engram memory engine as a Model Context Protocol
(MCP) **stdio** server, so any MCP client (Claude Code, etc.) can use engram as
a memory backend. This is the thin adapter over the engine — it only calls the
engine's public API; the engine stays host-agnostic and offline-capable.

Contract reference: [../specs/002-mcp-server/contracts/mcp-tools.md](../specs/002-mcp-server/contracts/mcp-tools.md).

## Build

```bash
CGO_ENABLED=0 go build -o engram-mcp ./cmd/engram-mcp   # pure-Go, cross-compilable
```

## Run

The only required setting is a data directory. Everything else (embedding, LLM)
is optional and off by default — the server never fails to start because an
endpoint is absent.

```bash
./engram-mcp --data-dir ~/.engram/memory
```

Startup logs a one-line summary to **stderr** (never stdout, which carries the
JSON-RPC stream): `data_dir`, whether an `embedding` client and `memory_ingest`
tool are active, and `max_open_namespaces`. No secrets are ever logged.

### Configuration

Non-secret settings take a flag or an `ENGRAM_*` env var (flag wins). **API keys
are env-only — never a flag, never a tracked file.**

| Setting | Flag | Env | Default |
|---------|------|-----|---------|
| Data directory (**required**) | `--data-dir` | `ENGRAM_DATA_DIR` | — |
| Embedding endpoint | `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | (offline) |
| Embedding model | `--embed-model` | `ENGRAM_EMBED_MODEL` | (offline) |
| Embedding API key | — | `ENGRAM_EMBED_API_KEY` | — |
| LLM endpoint | `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | (no ingest) |
| LLM model | `--llm-model` | `ENGRAM_LLM_MODEL` | (no ingest) |
| LLM provider (`openai`\|`anthropic`) | `--llm-provider` | `ENGRAM_LLM_PROVIDER` | — |
| LLM API key | — | `ENGRAM_LLM_API_KEY` | — |
| Max cached namespaces | `--max-open-namespaces` | `ENGRAM_MAX_OPEN_NAMESPACES` | 64 |

### Three modes

1. **Pure offline** (no endpoints): exposes 5 tools —
   `memory_write` / `memory_search` / `memory_list` / `memory_get` /
   `memory_delete`. `memory_search` runs keyword + entity signals and marks
   `degraded.semantic = true` in its response (the semantic arm is honestly
   reported absent, per structural fact — the server does not probe the engine).
2. **+ Embedding** (`--embed-base-url` + `--embed-model`, e.g. a local Ollama at
   `http://127.0.0.1:11434/v1`): `memory_search` fuses all three signals and
   reports `degraded.semantic = false`.
3. **+ LLM** (`--llm-base-url` + `--llm-model` + `--llm-provider`, API key via
   env): additionally exposes `memory_ingest`, which extracts durable facts from
   conversation turns into a namespace. When no LLM is configured this tool does
   **not** appear in `tools/list`; the other 5 are unaffected.

## Wire into Claude Code

CLI:

```bash
claude mcp add engram -- /abs/path/to/engram-mcp --data-dir /home/you/.engram/memory
```

Or a JSON MCP config block:

```json
{
  "mcpServers": {
    "engram": {
      "command": "/abs/path/to/engram-mcp",
      "args": ["--data-dir", "/home/you/.engram/memory"],
      "env": {
        "ENGRAM_EMBED_BASE_URL": "http://127.0.0.1:11434/v1",
        "ENGRAM_EMBED_MODEL": "qwen3-embedding:0.6b"
      }
    }
  }
}
```

Put any API keys in `env` (or the surrounding shell), never in `args`.

## Namespaces

Every tool accepts an optional `namespace` string; omitted/empty falls back to
`default`. Each namespace is an **independent** engine store at
`<data-dir>/<namespace>.db` — cross-namespace access is impossible at the
storage layer. Namespace ids are validated (`^[A-Za-z0-9._-]{1,64}$`, no `.` /
`..` / path separators, and the resolved path is asserted to stay inside the
data directory); anything that could escape the data directory is rejected.

Open stores are cached with an LRU bound (`--max-open-namespaces`, default 64);
a namespace in active use is never evicted underneath an in-flight call, so the
bound is a soft target that may be exceeded transiently under concurrent load.

## Verify offline (no endpoints, no network)

```bash
CGO_ENABLED=0 go test -count=1 ./mcpserver/...
```

The MCP contract tests use the SDK's in-memory transport (no subprocess, no
network): tools/list shape (5 offline / 6 with LLM, each with a non-empty
description + valid input schema), write→search/get/list round-trip, retrieval
parity vs the engine, cross-namespace isolation, path-escape rejection, and
degraded-marker honesty.
