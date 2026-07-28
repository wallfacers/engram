# engram

> **English** | [简体中文](README.zh-CN.md)

[![Go](https://img.shields.io/badge/Go-1.25.0-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-disabled-00599C)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-SDK%20v1.5.0-0052CC)](https://modelcontextprotocol.io/)
[![Storage](https://img.shields.io/badge/SQLite-pure--Go%20%7C%20WAL%20%7C%20FTS5-003B57)](https://pkg.go.dev/modernc.org/sqlite)
[![Retrieval](https://img.shields.io/badge/retrieval-semantic%20%2B%20BM25%20%2B%20entity%20RRF-success)](#architecture)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

A **local-first, embeddable memory layer** for any AI agent. One tuned memory engine
behind thin integration surfaces — MCP server / AI-first CLI / SDK — giving Codex,
Claude Code, Cursor, or your own agent long-term memory without you building one.

Three-signal hybrid retrieval (semantic + BM25 keyword + entity, RRF fusion) + ADD-only
fact extraction + deterministic curation. Runs fully offline, pure Go, no CGO, cross-compilable.

---

## Table of Contents

- [Features](#features)
- [Benchmark](#benchmark)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Evaluation](#evaluation)
- [Project Structure](#project-structure)
- [Documentation](#documentation)
- [Development](#development)
- [License](#license)

## Features

- **Local-first, offline by default** — core read/write paths need no network or cloud; SQLite local files; embedding / LLM are swappable local sidecars (Ollama / fastembed / any OpenAI-compatible endpoint), never a required hosted service.
- **Three-signal hybrid retrieval** — semantic (cosine) + keyword (FTS5 BM25, LIKE fallback) + entity (exact match), fused with RRF (k=60, tuning-free); optional cross-encoder reranker. Signals degrade per lane — a missing lane silently drops out of the fusion instead of failing the whole query.
- **Engine / adapter separation** — the memory engine is an independent, host-free, unit-testable library; MCP / CLI / SDK call only its public API and carry no host-specific logic.
- **Namespace isolation** — one namespace = one independent engine store (`<dataDir>/<ns>.db`) with LRU bound + eviction; cross-namespace access is off by default. Path-escape reads/writes = 0.
- **Evaluation regression gate** — any change touching retrieval / extraction / curation / storage / embedding must run a comparable-metric LoCoMo eval before merge and must not regress the baseline.

## Benchmark

> ⚠️ **Read before comparing**: every number is a function of **(dataset × answerer × judge × recipe)**,
> not "engram's score". Cross-row comparison is valid **only when exactly one axis differs**;
> never compare directly with others' leaderboard numbers. 🔬 = same-stack, measured by this project;
> 📣 = vendor self-report, not reproduced. Source of truth:
> [`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md).

**Unified conditions**: embedding `bge-large-en-v1.5` 1024d (local sidecar); engram retrieval uses the
canonical recipe (`--top-k 30 --chunk-quota 12 --force-answer`, hybrid, **no reranker**); judging uses
the mem0-aligned prompt.

### Same-stack, measured — the only directly comparable table

Same machine, Qwen answerer + bge-large embedding + identical judge prompt / judge model; competitors run their own code, unmodified.

| Dataset (n) | Framework | Answerer | Judge | Score |
|---|---|---|---|---:|
| **LoCoMo (1540)** | **engram** 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | **85.71%** |
| LoCoMo (1540) | engram 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-pro | 83.77% |
| LoCoMo (1540) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **89.03%** |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | 82.40% |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-pro | 80.26% |
| **LongMemEval-S (500)** | **engram** 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | **80.80%** |
| LongMemEval-S (500) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **86.00%** |

### Best self-reported scores — NOT directly comparable (different stacks)

| Dataset | engram 🔬 | MemOS 📣 | Mem0 📣 |
|---|---:|---:|---:|
| LoCoMo | **89.03%** (v4-pro, n=1540) | 88.83 | 92.5 |
| LongMemEval | **86.00%** (v4-pro, S-cleaned 500) | 89.20 | 94.4 |

### LoCoMo per-category scores (1540, judge=v4-flash, majority of 3 runs)

| Category | n | engram (Qwen) | engram (v4-pro) | MemOS@same-stack | Δ (engram−MemOS) |
|---|---:|---:|---:|---:|---:|
| single-hop | 841 | 88.82% | 90.96% | 82.64% | **+6.18** |
| multi-hop | 282 | 87.59% | 88.65% | 89.36% | −1.77 |
| temporal | 321 | 81.93% | 89.41% | 82.55% | −0.62 |
| open-domain | 96 | 65.62% | 71.88% | 59.38% | **+6.24** |
| **OVERALL** | **1540** | **85.71%** | **89.03%** | **82.40%** | **+3.31** |

> The per-category shape: MemOS's tree/graph memory **only wins multi-hop by 1.77pp**
> (exactly where it should); it loses 6pp+ on single-hop / open-domain. The gain from a stronger
> engram answerer is also uneven — temporal +7.5pp, open-domain +6.3pp, but multi-hop only +1.1pp,
> meaning multi-hop is bottlenecked on retrieval while the others are bottlenecked on answering.

### Three key deltas

| Axis | Change | Net effect | Conclusion |
|---|---|---:|---|
| **Framework (same-stack)** | engram − MemOS, LoCoMo 1540 | **+3.31pp** (v4-flash) / **+3.51pp** (v4-pro) | engram leads, holds under both judges |
| **Answerer** | Qwen → v4-pro | **+3.32pp** (LoCoMo) / **+5.20pp** (LME, p=0.0049) | Stronger answerer cashes in on temporal / open-domain |
| **Judge** | v4-flash → v4-pro | **−2 to −3pp** | Additive offset; does not change any delta's direction |

> Mem0's 92.5 / 94.4 come from a **managed platform** (proprietary optimizations not in the open-source SDK)
> + `top_200` retrieval budget, so they cannot be reproduced same-stack; the real gap to Mem0 is
> **unknown**. MemOS self-report 88.83 → same-stack 82.40 — the **−6.43pp is entirely regime artifact**
> (answerer strength + judge leniency).

## Architecture

```
        ┌─────────────── Adapter layer (thin) ──────────────┐
        │  mcpserver (stdio)   cmd/engram (CLI)              │
        │         MCP / CLI / SDK call the public API only    │
        └──────────────────────┬──────────────────────────────┘
                               ▼  public API
        ┌─────────────── Engine layer (pure lib, offline) ───┐
        │  memory/   entrystore · retriever · pipeline        │
        │            curation · prompt · queryplan            │
        │  embedding/  embedder + reranker (optional)         │
        │  provider/    LLM abstraction (anthropic / openai)  │
        │  store/       SQLite: WAL · FTS5 trigram            │
        └─────────────────────────────────────────────────────┘
```

**Retrieval pipeline**: query → three lanes in parallel (semantic cosine / FTS5 BM25 / entity exact match)
→ RRF (k=60) fusion → (optional) cross-encoder rerank → return. A missing lane silently drops out of the
fusion instead of failing the query; adapters report degradation only from **structural facts** (e.g. "no
embedding endpoint configured"), never by probing the engine.

## Quick Start

Requires Go 1.25+. All build & test pass under `CGO_ENABLED=0`.

```bash
git clone https://github.com/wallfacers/engram.git && cd engram
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test  -count=1 ./...
```

### As an MCP server (Codex / Claude Code / Cursor)

Build the server, then register a local stdio process in your MCP client config.

```bash
CGO_ENABLED=0 go build -o engram-mcp ./cmd/engram-mcp
```

```json
{
  "mcpServers": {
    "engram": {
      "command": "/abs/path/to/engram-mcp",
      "args": ["--data-dir", "/home/you/.engram/memory"]
    }
  }
}
```

After launch, the agent can call `memory_write` / `memory_search` / `memory_list` / `memory_get` /
`memory_delete`; `memory_ingest` appears once an LLM is configured. **Runs offline with no endpoints configured.**
Details: [`docs/mcp-server.md`](docs/mcp-server.md).

### As an AI-first CLI

Successful commands write deterministic markdown to stdout; failures write one actionable diagnostic to
stderr and return non-zero — easy for AI agents / cron / CI to consume. Full command list: [`docs/cli.md`](docs/cli.md).

```bash
CGO_ENABLED=0 go build -o engram ./cmd/engram

engram --data-dir ~/.engram/memory add    --name dark-mode --content "user prefers dark mode" --category preference
engram --data-dir ~/.engram/memory search "appearance settings"
engram --data-dir ~/.engram/memory get    dark-mode
engram --data-dir ~/.engram/memory list
engram --data-dir ~/.engram/memory delete dark-mode
printf 'user: I moved to Berlin last month.\nassistant: Noted.\n' | engram --data-dir ~/.engram/memory ingest   # needs LLM
engram --data-dir ~/.engram/memory stats
```

## Configuration

Non-secret config is either a global flag or an `ENGRAM_*` env var (flag wins over env).
**API keys are env-only — never passed as a flag, never written to logs / tool responses / config files.**

| Flag | Env | Default | Description |
|---|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | required | SQLite storage dir |
| `--namespace` | `ENGRAM_NAMESPACE` | `default` | namespace (CLI) |
| — | `ENGRAM_MAX_OPEN_NAMESPACES` | — | namespace LRU cap (MCP) |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | offline | OpenAI-compatible `/v1` endpoint |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | offline | embedding model name |
| — | `ENGRAM_EMBED_API_KEY` | — | embedding key (env only) |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | ingest unavailable | `anthropic` / `openai` |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | ingest unavailable | LLM endpoint |
| `--llm-model` | `ENGRAM_LLM_MODEL` | ingest unavailable | LLM model name |
| — | `ENGRAM_LLM_API_KEY` | — | LLM key (env only) |

## Evaluation

engram's retrieval fidelity is proven by deterministic parity goldens (`testdata/parity/`) + the LoCoMo
harness, not by trust. Offline gates need no network.

```bash
go test ./memory -run TestRetrievalParity        # retrieval parity (memory_search == Retriever.Search)
go test ./memory -run TestSignalDegradation      # per-lane signal degradation
```

End-to-end answer eval needs your own dataset + endpoints (`.locomo-run/`, `*.db`, `testdata/locomo/` are gitignored):

```bash
go build ./cmd/locomo-bench
export LOCOMO_API_KEY=...      LOCOMO_BASE_URL=...   LOCOMO_MODEL=...   LOCOMO_PROVIDER=anthropic
export EXTRACT_MODEL=...
export EMBED_BASE_URL=http://127.0.0.1:11434/v1   EMBED_MODEL=...   EMBED_API_KEY=...
go run ./cmd/locomo-bench --data ./path/to/locomo.json \
      --run-dir ./.locomo-run --retrieval both
```

Canonical recipe (four required flags), three backend stacks and known pitfalls:
[`docs/locomo-e2e-eval-reproduction.md`](docs/locomo-e2e-eval-reproduction.md);
score-lever experiment ledger: [`docs/locomo-score-levers.md`](docs/locomo-score-levers.md).

## Project Structure

```
memory/        engine core (public pkgs): entrystore / retriever / pipeline / curation / prompt / queryplan
embedding/     engine: embedder + reranker (OpenAI-compatible /v1, optional)
provider/      engine: LLM abstraction (+ anthropic / + openai)
store/         engine: SQLite (modernc, pure Go) — Open / Options / migrations / ProbeFTS5
internal/      engine-internal: idgen / version (not for external use)
mcpserver/     adapter: MCP stdio server (config / namespace / registry / server / tools)
cmd/
  engram-mcp/    MCP server binary (thin main)
  engram/        AI-first CLI binary
  locomo-bench/  LoCoMo eval harness
specs/          spec-kit SDD: specs/NNN-feature/ spec·plan·tasks·research·data-model·contracts
docs/           strategy / background / competitors / adapter usage / eval / open issues
testdata/       parity goldens; locomo/ dataset (gitignored)
```

## Documentation

- [`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md) — **eval results master table (any external score quote must cite this)**
- [`docs/competitive-benchmarks.md`](docs/competitive-benchmarks.md) — competitor targets + methodology audit
- [`docs/memos-inhouse-locomo-repro.md`](docs/memos-inhouse-locomo-repro.md) — MemOS same-stack reproduction methodology
- [`docs/memory-strategy.md`](docs/memory-strategy.md) — tech & strategy source of truth, score-lever backlog
- [`docs/memory-architecture.md`](docs/memory-architecture.md) — runtime architecture: extraction timing, write/retrieval/curation flow, SQLite schema
- [`docs/mcp-server.md`](docs/mcp-server.md) · [`docs/cli.md`](docs/cli.md) — adapter usage
- [`docs/README.md`](docs/README.md) — full doc index (with status semantics)

## Development

- **Spec-driven** with [github/spec-kit](https://github.com/github/spec-kit) — `constitution → specify → plan → tasks → implement`. Scaffolding in `.specify/`, Claude integration skills in `.claude/skills/`.
- **Constitution (five non-negotiables)**, source [`.specify/memory/constitution.md`](.specify/memory/constitution.md): ① local-first/offline ② engine-adapter separation ③ contract-first + namespace isolation ④ evaluation regression gate ⑤ graceful degradation + honest scale.
- **Commit discipline**: engine changes vs eval-config changes on separate commits (attribution); secrets never enter tracked files / logs / tool responses.

## License

engram is licensed under the [Apache License 2.0](./LICENSE). Copyright 2026 wallfacers.
Contributions are licensed under the same terms per License §5.
