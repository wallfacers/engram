<h1 align="center">engram</h1>

<p align="center">
  <strong>Local-first long-term memory for AI agents.</strong>
</p>

<p align="center">
  Pure Go · Embedded SQLite · MCP &amp; CLI · Hybrid retrieval
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a>
  · <a href="#quick-start">Quick start</a>
  · <a href="#architecture">Architecture</a>
  · <a href="#benchmarks">Benchmarks</a>
  · <a href="docs/README.md">Documentation</a>
</p>

<p align="center">
  <a href="https://github.com/wallfacers/engram/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/wallfacers/engram/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://go.dev/"><img alt="Go 1.25" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://github.com/wallfacers/engram/blob/master/LICENSE"><img alt="Apache 2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
  <img alt="CGO disabled" src="https://img.shields.io/badge/CGO-disabled-2f855a">
</p>

engram gives Codex, Claude Code, Cursor, and custom agents durable memory
without requiring a hosted database. Every namespace lives in its own local
SQLite file, and the same pure-Go engine is available through an MCP stdio
server, an automation-friendly CLI, and embeddable Go packages.

Start with fully offline read/write and keyword retrieval. Add compatible local
or remote model endpoints only when you want semantic search, conversation
fact extraction, or memory curation.

## Why engram

| | |
|---|---|
| **Local by default**<br>Data stays in SQLite on your machine. Core paths need neither a cloud account nor a network connection. | **Useful without models**<br>CRUD, namespaces, export, and keyword retrieval remain available when every model endpoint is absent. |
| **Hybrid when available**<br>Semantic, BM25 keyword, and entity signals are combined with tuning-free Reciprocal Rank Fusion. | **Explicit lifecycle**<br>Agents decide when to write or ingest. Curation is shipped but opt-in; ordinary chat is never captured implicitly. |
| **One engine, thin adapters**<br>MCP and CLI call the public engine API instead of reimplementing memory behavior. | **Portable deployment**<br>Pure Go, no CGO, an embedded database, and no separate vector database to operate. |

## Quick start

### 1. Install the agent skill

Prerequisites: Node.js >=22.20.0, npx/npm, Git, and network access. The command
installs only the skill; install the CLI and configure the MCP server
separately. With `--global`, `skills@1.5.20` writes `~/.claude/skills/engram`
(Claude Code) and `~/.agents/skills/engram` (Codex/OpenCode); Codex and OpenCode
scan `~/.agents/skills/`, so the package is discovered as-is with no extra step.

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram --global --agent claude-code --agent codex --agent opencode
```

Keep the installer's write confirmation; choose `Symlink` (default) or `Copy`
on restricted filesystems. After install, reload each client and verify it
discovers exactly one `engram` skill. For project scope, one-client installs,
offline/manual fallback, upgrades, and recovery, see the canonical [skill
installation reference](skills/engram/references/install.md).

### 2. Build and use the CLI offline

Requires Go 1.25 or newer.

```bash
git clone https://github.com/wallfacers/engram.git
cd engram

mkdir -p bin
CGO_ENABLED=0 go build -o ./bin/engram ./cmd/engram
export ENGRAM_DATA_DIR="$PWD/data"

./bin/engram add \
  --name preferred-editor \
  --content "The user prefers Neovim for Go development." \
  --category preference

./bin/engram search "Neovim"
```

The example creates `data/default.db` and performs a degraded-but-functional
offline search. Configure an embedding endpoint later to add the semantic lane;
your existing data and commands stay the same.

Common commands:

```bash
./bin/engram get preferred-editor
./bin/engram list
./bin/engram stats
./bin/engram export
./bin/engram delete preferred-editor
./bin/engram namespaces
./bin/engram version
```

See the [CLI guide](docs/guides/cli.md) for ingest, curation, namespaces, and
automation behavior.

### 3. Connect an MCP client

Build the stdio server:

```bash
CGO_ENABLED=0 go build -o ./bin/engram-mcp ./cmd/engram-mcp
```

Register the absolute binary path in your MCP client. The surrounding config
shape varies by client:

```json
{
  "mcpServers": {
    "engram": {
      "command": "/absolute/path/to/engram/bin/engram-mcp",
      "args": ["--data-dir", "/absolute/path/to/engram-data"]
    }
  }
}
```

With no model configuration, the server exposes CRUD plus lossless
`memory_ingest_v2` and the `memory_evidence_get`/lifecycle tools. Configure an
LLM to add legacy `memory_ingest` fact extraction. Each tool accepts a
namespace; namespaces are isolated in separate database files.

See the [MCP server guide](docs/guides/mcp-server.md) for client integration,
tool boundaries, and opt-in curation.

## Architecture

```mermaid
flowchart LR
    subgraph clients["Clients"]
        agent["AI agents<br/>Codex · Claude Code · Cursor"]
        app["Go applications"]
    end

    subgraph interfaces["Thin interfaces"]
        mcp["MCP server<br/>stdio"]
        cli["AI-first CLI"]
        api["Public Go API"]
    end

    subgraph engine["Memory engine · pure Go"]
        write["Direct write<br/>& explicit ingest"]
        search["Hybrid retrieval<br/>semantic · BM25 · entity → RRF"]
        curate["Opt-in curation"]
    end

    db[("SQLite per namespace<br/>WAL · FTS5")]
    models["Optional model sidecars<br/>Embeddings · LLM"]

    agent --> mcp
    agent --> cli
    app --> api
    mcp --> api
    cli --> api
    api --> write
    api --> search
    api --> curate
    write --> db
    curate --> db
    db --> search
    models -. "extract & embed" .-> write
    models -. "embed query" .-> search
    models -. "judge" .-> curate

    classDef client fill:#f8fafc,stroke:#94a3b8,color:#0f172a
    classDef interface fill:#eef2ff,stroke:#6366f1,color:#1e1b4b
    classDef core fill:#ecfdf5,stroke:#10b981,color:#064e3b
    classDef storage fill:#fff7ed,stroke:#f97316,color:#7c2d12
    classDef optional fill:#faf5ff,stroke:#a855f7,color:#581c87,stroke-dasharray:5 5

    class agent,app client
    class mcp,cli,api interface
    class write,search,curate core
    class db storage
    class models optional
```

The solid path is local. Model connections are optional:

1. **Write:** `add` / `memory_write` stores caller-provided content directly;
   explicit `ingest` uses an LLM to extract durable facts before writing.
2. **Search:** keyword search always runs locally. Indexed entity matches and
   semantic cosine results join the ranking when their data and dependencies
   are available; RRF fuses the lanes that succeeded.
3. **Curate:** a bounded pass scores and deduplicates memories. It runs only
   when explicitly invoked by the CLI or enabled for the MCP server.

For implementation boundaries, storage primitives, and provenance, read the
[memory-system architecture](docs/architecture/memory-system.md).

## Capability matrix

| Capability | Default | Optional dependency |
|---|---|---|
| Local CRUD, list, stats, export | Available offline | None |
| Keyword retrieval | Available offline | None |
| Entity retrieval | Used when entity facts are indexed | Facts are produced by explicit ingest |
| Semantic retrieval | Gracefully omitted | OpenAI-compatible embedding endpoint |
| Conversation fact extraction | Not exposed without a model | OpenAI or Anthropic-compatible LLM |
| Memory curation | Opt-in | LLM; explicit CLI command or MCP enablement |
| Namespace isolation | One SQLite file per namespace | None |

engram is currently designed for local, single-user workloads in the
approximately 100k-entry class. It is not a distributed vector service, and it
does not yet claim complete memory freshness or state-consistency guarantees.
See [current capabilities](docs/product/capabilities.md) for the authoritative
shipped/planned boundary.

## Configuration

Non-secret flags override their `ENGRAM_*` environment equivalents. API keys
are environment-only: they are never accepted as command-line flags.

| Flag | Environment | Purpose |
|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | Directory containing namespace databases |
| `--namespace` | `ENGRAM_NAMESPACE` | CLI namespace; defaults to `default` |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | OpenAI-compatible embedding endpoint |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | Embedding model name |
| — | `ENGRAM_EMBED_API_KEY` | Embedding API key |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | `openai` or `anthropic` |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | LLM endpoint |
| `--llm-model` | `ENGRAM_LLM_MODEL` | LLM model name |
| — | `ENGRAM_LLM_API_KEY` | LLM API key |
| `--max-open-namespaces` | `ENGRAM_MAX_OPEN_NAMESPACES` | MCP namespace cache limit; default `64` |
| `--curation-enabled` | `ENGRAM_CURATION_ENABLED` | Enable persistent MCP curation; default `false` |

The final two rows are MCP-server settings. Run `engram-mcp --help` against
your installed version for its exact startup contract.

## Benchmarks

> **91.10% on LoCoMo** — 1,403 correct answers across the full 1,540-question
> benchmark.

Verified with Qwen3.6-35B running locally, thinking + top-k 150, three-run
majority, and clean final-answer judging. The result was independently
reproduced from the original answer runs.

[Benchmark details and reproduction evidence →](docs/evaluation/results.md)

## Documentation

| Goal | Document |
|---|---|
| Browse all current docs | [Documentation portal](docs/README.md) |
| Install the Claude Code, Codex, or OpenCode skill | [Skill installation reference](skills/engram/references/install.md) |
| Use the CLI | [CLI guide](docs/guides/cli.md) |
| Connect an MCP client | [MCP server guide](docs/guides/mcp-server.md) |
| Understand the runtime | [Memory-system architecture](docs/architecture/memory-system.md) |
| Check shipped vs planned features | [Current capabilities](docs/product/capabilities.md) |
| Inspect benchmark evidence | [Evaluation results](docs/evaluation/results.md) |
| See product direction | [Roadmap](docs/product/roadmap.md) |

<details>
<summary><strong>Repository layout</strong></summary>

```text
memory/         public memory engine, retrieval, extraction, and curation
embedding/      embedding client and optional reranker interfaces
provider/       LLM provider abstraction
store/          pure-Go SQLite setup and migrations
mcpserver/      MCP stdio adapter and namespace registry
cmd/engram/     AI-first CLI
cmd/engram-mcp/ MCP server executable
cmd/locomo-bench/ evaluation harness
docs/           user, architecture, product, evaluation, and operations docs
specs/          contract-first feature specifications and plans
```

</details>

## Development

The hard gate is a complete build and test run with CGO disabled:

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
go vet ./...

node docs/validation/check-docs.mjs
node --test docs/validation/check-docs.test.mjs
```

Changes to retrieval, extraction, curation, storage, or embedding also require a
comparable evaluation run before merge. The project principles are recorded in
the [memory constitution](.specify/memory/constitution.md); documentation
governance is in [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md).

## License

engram is available under the [Apache License 2.0](LICENSE).
