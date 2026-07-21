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
engram --data-dir ~/.engram/memory stats
engram --data-dir ~/.engram/memory export
engram --data-dir ~/.engram/memory namespaces
engram --data-dir ~/.engram/memory version
```

`ingest` reads one `user:` or `assistant:` turn per stdin line and requires an
LLM configuration. `add`, `search`, `get`, `list`, and `delete` run without any
network endpoint. When no embedding endpoint is configured, search remains
available and declares semantic degradation in its markdown document.

Namespaces are separate `<namespace>.db` files under the data directory.
Namespace names must match `^[A-Za-z0-9._-]{1,64}$` and cannot be `.` or `..`.

## Verify Offline

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/engram/...
```

An AI agent can call `engram search "..."` and consume stdout as markdown. On a
failure it should read stderr and follow the stated next action.
