# engram CLI

> 🧭 **状态**: 已交付(spec 004-cli-ai-first) · **目标**: engram CLI 适配器用法参考。

`engram` is the AI-first, one-shot command-line adapter for the local engram
memory engine. Successful commands write deterministic markdown to stdout.
Failures write one actionable diagnostic to stderr and return a non-zero exit
code.

## Build

```bash
CGO_ENABLED=0 go build -o engram ./cmd/engram
```

## Configuration

Pass non-secret configuration as a global flag or `ENGRAM_*` environment
variable. A flag wins over its environment variable. API keys are environment
only and must never be passed as flags.

| Flag | Environment | Default |
|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | required |
| `--namespace` | `ENGRAM_NAMESPACE` | `default` |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | offline |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | offline |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | ingest unavailable |
| `--llm-model` | `ENGRAM_LLM_MODEL` | ingest unavailable |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | ingest unavailable |

Embedding and LLM API keys are read only from `ENGRAM_EMBED_API_KEY` and
`ENGRAM_LLM_API_KEY`.

## Commands

```bash
engram --data-dir ~/.engram/memory add --name dark-mode --content "The user prefers dark mode." --category preference
engram --data-dir ~/.engram/memory search "appearance settings"
engram --data-dir ~/.engram/memory get dark-mode
engram --data-dir ~/.engram/memory list
engram --data-dir ~/.engram/memory delete dark-mode
printf 'user: I moved to Berlin last month.\nassistant: Noted!\n' | engram --data-dir ~/.engram/memory ingest
engram --data-dir ~/.engram/memory curate
engram --data-dir ~/.engram/memory stats
engram --data-dir ~/.engram/memory export
engram --data-dir ~/.engram/memory namespaces
engram --data-dir ~/.engram/memory version
```

`ingest` reads one `user:` or `assistant:` turn per stdin line and requires an
LLM configuration. `add`, `search`, `get`, `list`, and `delete` run without any
network endpoint. When no embedding endpoint is configured, search remains
available and declares semantic degradation in its markdown document.

`curate` also requires an LLM. It synchronously runs one curation pass for the
selected namespace and returns only after the pass finishes:

```markdown
# curated

- namespace: default
- status: completed
```

The pass has a two-minute deadline. With no work it normally performs only a
local SQLite check; with candidates, elapsed time is primarily the local scan
plus one LLM judge call, so there is no fixed average duration. Timeout or
caller cancellation returns a non-zero diagnostic and prevents late apply.
`completed` means the pass ended safely, not that a merge or eviction
necessarily occurred.

Ordinary `add` and `ingest` never start or notify curation. This keeps their
latency and model cost predictable. The one-shot mode is well suited to manual
maintenance, cron and CI because completion is observable, but the caller must
wait and schedule it explicitly. For continuously written Agent memory, MCP's
explicitly enabled persistent worker offers non-blocking writes, debounce and a
30-minute fallback timer at the cost of a long-running service and background
LLM work.

See [memory-architecture.md](./memory-architecture.md) for the complete flow,
timing comparison and SQLite table diagram.

Namespaces are separate `<namespace>.db` files under the data directory.
Namespace names must match `^[A-Za-z0-9._-]{1,64}$` and cannot be `.` or `..`.

## Verify Offline

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/engram/...
```

An AI agent can call `engram search "..."` and consume stdout as markdown. On a
failure it should read stderr and follow the stated next action.
