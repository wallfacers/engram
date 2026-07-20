# Quickstart: engram CLI (AI-first)

## Build

```bash
CGO_ENABLED=0 go build -o engram ./cmd/engram   # pure-Go, cross-compilable
```

## Run (offline — no endpoints needed)

```bash
export ENGRAM_DATA_DIR=~/.engram/memory        # or pass --data-dir each call

engram add --name dark-mode --content "The user prefers dark mode." --category preference
engram search "appearance settings"
engram get dark-mode
engram list
engram delete dark-mode
```

- Success output is **markdown on stdout**; diagnostics go to **stderr**.
- Non-zero exit on any failure, with a `<what> — <next action>` diagnostic.

## Namespaces

```bash
engram --namespace work add --name deadline --content "Ship v1 by Friday."
engram --namespace work list
engram namespaces                              # lists every <ns>.db under the data dir
```

Invalid namespaces (`../x`, `a/b`, `.`) are rejected with exit 5 and create no
files outside the data directory.

## With embedding (semantic signal + durable vectors)

```bash
export ENGRAM_EMBED_BASE_URL=http://127.0.0.1:11434/v1
export ENGRAM_EMBED_MODEL=qwen3-embedding:0.6b
# export ENGRAM_EMBED_API_KEY=...   # only if the endpoint needs one (env-only)

engram add --name trip --content "Kyoto trip in May."   # vector drained before exit
engram search "japan travel"                             # semantic no longer 'degraded'
```

Without an embedding endpoint, `search` still runs (keyword + entity) and marks the
semantic signal unavailable — it never fails for a missing endpoint.

## With an LLM (conversation ingestion)

```bash
export ENGRAM_LLM_BASE_URL=... ENGRAM_LLM_MODEL=... ENGRAM_LLM_PROVIDER=openai
export ENGRAM_LLM_API_KEY=...     # env-only

printf 'user: I moved to Berlin last month.\nassistant: Noted!\n' | engram ingest
```

- stdin format: one turn per line, `user:` or `assistant:` prefix.
- Without an LLM configured, `ingest` exits 4 with:
  `ingest requires an LLM — set ENGRAM_LLM_BASE_URL/MODEL/PROVIDER and ENGRAM_LLM_API_KEY`.

## Inspect & export

```bash
engram stats        # entry count, non-pinned count, manifest-size estimate
engram export       # full deterministic markdown dump of every entry
engram version      # build version
```

## Verify offline (no endpoints, no network)

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/engram/...
```

Tests run in-process (`run(args, stdin, stdout, stderr) int`) plus one `os/exec`
end-to-end smoke; `parity_test.go` asserts `engram search` == direct
`Retriever.Search`; the engine stays untouched
(`git diff --name-only -- memory embedding provider store internal` is empty).

## Wire into an AI agent

Point the agent's shell/tool at the built `engram` binary with `ENGRAM_DATA_DIR`
(and optional embedding/LLM env) set. The agent calls e.g. `engram search "<q>"`
and reads the markdown; on failure it reads the stderr diagnostic and follows the
suggested next action.
```
