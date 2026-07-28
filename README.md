# engram

> 📖 This README is bilingual: **English** first, **简体中文** right after, in every section.
> 本 README 为双语:每节**英文**在前,**简体中文**紧跟其后。

[![Go](https://img.shields.io/badge/Go-1.25.0-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-disabled-00599C)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-SDK%20v1.5.0-0052CC)](https://modelcontextprotocol.io/)
[![Storage](https://img.shields.io/badge/SQLite-pure--Go%20%7C%20WAL%20%7C%20FTS5-003B57)](https://pkg.go.dev/modernc.org/sqlite)
[![Retrieval](https://img.shields.io/badge/retrieval-semantic%20%2B%20BM25%20%2B%20entity%20RRF-success)](#architecture)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

A **local-first, embeddable memory layer** for any AI agent. One tuned memory engine
behind thin integration surfaces — MCP server / AI-first CLI / SDK — giving Codex,
Claude Code, Cursor, or your own agent long-term memory without you building one.

**可嵌入任意智能体的本地优先记忆层**:一套记忆引擎,多个集成面——MCP server /
AI-first CLI / SDK——让 Codex、Claude Code、Cursor、自研 Agent 无需自建记忆即可拥有长期记忆。

Three-signal hybrid retrieval (semantic + BM25 keyword + entity, RRF fusion) + ADD-only
fact extraction + deterministic curation. Runs fully offline, pure Go, no CGO, cross-compilable.

三路混合检索(语义 + BM25 关键词 + 实体,RRF 融合)+ ADD-only 事实抽取 + 确定性 curation,
完全离线可运行,纯 Go、无 CGO、可交叉编译。

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
**特性**

- **Local-first, offline by default** — core read/write paths need no network or cloud; SQLite local files; embedding / LLM are swappable local sidecars (Ollama / fastembed / any OpenAI-compatible endpoint), never a required hosted service.
  **默认离线、本地优先** —— 核心读写路径无需网络/云;SQLite 本地文件;embedding / LLM 是可替换的本地 sidecar(Ollama / fastembed / 任意 OpenAI 兼容端点),绝非强制依赖的托管服务。
- **Three-signal hybrid retrieval** — semantic (cosine) + keyword (FTS5 BM25, LIKE fallback) + entity (exact match), fused with RRF (k=60, tuning-free); optional cross-encoder reranker. Signals degrade per lane — a missing lane silently drops out of the fusion instead of failing the whole query.
  **三信号混合检索** —— 语义(cosine)+ 关键词(FTS5 BM25,LIKE 兜底)+ 实体(精确匹配),RRF(k=60,免调参)融合;可选 cross-encoder reranker。信号逐路降级,缺哪路自动剔除、绝不整体失败。
- **Engine / adapter separation** — the memory engine is an independent, host-free, unit-testable library; MCP / CLI / SDK call only its public API and carry no host-specific logic.
  **引擎 / 适配器隔离** —— 记忆引擎是独立、无宿主、可单测的库;MCP / CLI / SDK 只通过引擎公开 API 调用,不耦合宿主逻辑。
- **Namespace isolation** — one namespace = one independent engine store (`<dataDir>/<ns>.db`) with LRU bound + eviction; cross-namespace access is off by default. Path-escape reads/writes = 0.
  **命名空间隔离** —— 一个命名空间 = 一个独立引擎 store(`<dataDir>/<ns>.db`),LRU + 驱逐;跨命名空间访问默认关闭。路径逃逸读写 = 0。
- **Evaluation regression gate** — any change touching retrieval / extraction / curation / storage / embedding must run a comparable-metric LoCoMo eval before merge and must not regress the baseline.
  **评测回归门禁** —— 检索 / 抽取 / curation / 存储 / embedding 的任何改动,合入前必须跑可比指标的 LoCoMo 评测且不得回归基线。

## Benchmark
**基准评测**

> ⚠️ **Read before comparing**: every number is a function of **(dataset × answerer × judge × recipe)**,
> not "engram's score". Cross-row comparison is valid **only when exactly one axis differs**;
> never compare directly with others' leaderboard numbers. 🔬 = same-stack, measured by this project;
> 📣 = vendor self-report, not reproduced. Source of truth:
> [`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md).
>
> ⚠️ **读表前必读**:每一个数字都是 **(数据集 × 答题模型 × 判题模型 × 配方)** 的函数,**不是"engram 的分"**。
> 跨行比较只在**恰好一个轴不同**时有效;与他人 leaderboard 数字**一律不可直接横比**。
> 🔬 = 本项目同栈实测,📣 = 厂商自报未复现。正本:[`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md)。

**Unified conditions / 统一条件**: embedding `bge-large-en-v1.5` 1024d (local sidecar);
engram retrieval uses the canonical recipe (`--top-k 30 --chunk-quota 12 --force-answer`, hybrid,
**no reranker**); judging uses the mem0-aligned prompt.
嵌入 `bge-large-en-v1.5` 1024d(本地 sidecar);engram 检索走 canonical recipe
(`--top-k 30 --chunk-quota 12 --force-answer`,hybrid,**无 reranker**);判题用 mem0-aligned prompt。

### Same-stack, measured — the only directly comparable table
### 同栈实测 —— 唯一可直接比较的表

Same machine, Qwen answerer + bge-large embedding + identical judge prompt / judge model; competitors run their own code, unmodified.
同一台机器的 Qwen 答题 + bge-large 嵌入 + 同一 judge prompt / 同一 judge 模型;竞品跑自家代码,零改动。

| Dataset 数据集 (n) | Framework 框架 | Answerer 答题模型 | Judge 判题模型 | Score 得分 |
|---|---|---|---|---:|
| **LoCoMo (1540)** | **engram** 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | **85.71%** |
| LoCoMo (1540) | engram 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-pro | 83.77% |
| LoCoMo (1540) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **89.03%** |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | 82.40% |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-pro | 80.26% |
| **LongMemEval-S (500)** | **engram** 🔬 | Qwen3.6-35B · local vllm | deepseek-v4-flash | **80.80%** |
| LongMemEval-S (500) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **86.00%** |

### Best self-reported scores — NOT directly comparable (different stacks)
### 各方自报最好成绩 —— 不可直接比较(栈不同)

| Dataset 数据集 | engram 🔬 | MemOS 📣 | Mem0 📣 |
|---|---:|---:|---:|
| LoCoMo | **89.03%** (v4-pro, n=1540) | 88.83 | 92.5 |
| LongMemEval | **86.00%** (v4-pro, S-cleaned 500) | 89.20 | 94.4 |

### LoCoMo per-category scores (1540, judge=v4-flash, majority of 3 runs)
### LoCoMo 分类别得分(1540,judge=v4-flash,3 跑多数)

| Category 类别 | n | engram (Qwen) | engram (v4-pro) | MemOS@same-stack 同栈 | Δ (engram−MemOS) |
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
>
> 分类别的"比分"形状:MemOS 的 tree/graph 记忆组织**只在 multi-hop 赢 1.77pp**(正是它该赢的地方);
> single-hop / open-domain 各输 6pp+。engram 换强答题模型的收益也极不均匀——temporal +7.5pp、
> open-domain +6.3pp,而 multi-hop 仅 +1.1pp,说明 multi-hop 瓶颈在检索侧,其余类别瓶颈在答题侧。

### Three key deltas
### 三个关键 Δ

| Axis 轴 | Change 变化 | Net effect 净效应 | Conclusion 结论 |
|---|---|---:|---|
| **Framework (same-stack)** | engram − MemOS, LoCoMo 1540 | **+3.31pp** (v4-flash) / **+3.51pp** (v4-pro) | engram leads, holds under both judges |
| **Answerer** | Qwen → v4-pro | **+3.32pp** (LoCoMo) / **+5.20pp** (LME, p=0.0049) | Stronger answerer cashes in on temporal / open-domain |
| **Judge** | v4-flash → v4-pro | **−2 to −3pp** | Additive offset; does not change any delta's direction |

> Mem0's 92.5 / 94.4 come from a **managed platform** (proprietary optimizations not in the open-source SDK)
> + `top_200` retrieval budget, so they cannot be reproduced same-stack; the real gap to Mem0 is
> **unknown**. MemOS self-report 88.83 → same-stack 82.40 — the **−6.43pp is entirely regime artifact**
> (answerer strength + judge leniency).
>
> Mem0 的 92.5 / 94.4 来自**托管平台**(含开源 SDK 不带的私有优化)+ `top_200` 检索预算,无法同栈复现;
> 对 Mem0 的真实差距 = **未知数**。MemOS 自报 88.83 → 同栈 82.40,**−6.43pp 全是 regime 伪影**(答题模型强度 + judge 宽松度)。

## Architecture
**架构**

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

**检索流水线**:query → 三路并行(语义 cosine / FTS5 BM25 / 实体精确匹配)→ RRF(k=60)融合
→(可选)cross-encoder rerank → 返回。任一路信号缺失即静默退出融合,不拖垮整体;适配器只从**结构性事实**
(如"未配置 embedding 端点")报告降级,不探测引擎。

## Quick Start
**快速开始**

Requires Go 1.25+. All build & test pass under `CGO_ENABLED=0`.
要求 Go 1.25+。全部构建与测试在 `CGO_ENABLED=0` 下通过。

```bash
git clone https://github.com/wallfacers/engram.git && cd engram
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test  -count=1 ./...
```

### As an MCP server (Codex / Claude Code / Cursor)
### 作为 MCP server 接入(Codex / Claude Code / Cursor)

Build the server, then register a local stdio process in your MCP client config.
构建 server,然后在 MCP 客户端配置中注册本地 stdio 进程。

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

启动后,Agent 可调用 `memory_write` / `memory_search` / `memory_list` / `memory_get` / `memory_delete`;
配置 LLM 后额外出现 `memory_ingest`。**不配置任何端点仍可离线运行。**接入细节见 [`docs/mcp-server.md`](docs/mcp-server.md)。

### As an AI-first CLI
### 作为 AI-first CLI

Successful commands write deterministic markdown to stdout; failures write one actionable diagnostic to
stderr and return non-zero — easy for AI agents / cron / CI to consume. Full command list: [`docs/cli.md`](docs/cli.md).
成功命令向 stdout 写确定性 markdown,失败向 stderr 写一条可操作诊断并返回非零退出码——便于 AI Agent / cron / CI 直接消费。完整命令见 [`docs/cli.md`](docs/cli.md)。

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
**配置**

Non-secret config is either a global flag or an `ENGRAM_*` env var (flag wins over env).
**API keys are env-only — never passed as a flag, never written to logs / tool responses / config files.**
非密钥配置既可用全局 flag、也可用 `ENGRAM_*` 环境变量(flag 覆盖环境变量)。**API key 只走环境变量,绝不作为 flag 传入,也绝不写入日志 / tool 响应 / 配置文件。**

| Flag | Env 环境变量 | Default 默认 | Description 说明 |
|---|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | required 必填 | SQLite storage dir 存储目录 |
| `--namespace` | `ENGRAM_NAMESPACE` | `default` | namespace (CLI) 命名空间 |
| — | `ENGRAM_MAX_OPEN_NAMESPACES` | — | namespace LRU cap (MCP) 命名空间 LRU 上限 |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | offline 离线 | OpenAI-compatible `/v1` endpoint 端点 |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | offline 离线 | embedding model name 嵌入模型名 |
| — | `ENGRAM_EMBED_API_KEY` | — | embedding key (env only) 嵌入密钥(仅 env) |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | ingest unavailable | `anthropic` / `openai` |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | ingest unavailable | LLM endpoint LLM 端点 |
| `--llm-model` | `ENGRAM_LLM_MODEL` | ingest unavailable | LLM model name LLM 模型名 |
| — | `ENGRAM_LLM_API_KEY` | — | LLM key (env only) LLM 密钥(仅 env) |

## Evaluation
**评测与复现**

engram's retrieval fidelity is proven by deterministic parity goldens (`testdata/parity/`) + the LoCoMo
harness, not by trust. Offline gates need no network.
engram 的检索保真度由确定性 parity golden(`testdata/parity/`)+ LoCoMo harness 证明,而非靠信任。离线门禁无需外网。

```bash
go test ./memory -run TestRetrievalParity        # retrieval parity (memory_search == Retriever.Search)
go test ./memory -run TestSignalDegradation      # per-lane signal degradation
```

End-to-end answer eval needs your own dataset + endpoints (`.locomo-run/`, `*.db`, `testdata/locomo/` are gitignored):
端到端答题评测需自备数据集与端点(`.locomo-run/`、`*.db`、`testdata/locomo/` 均已 gitignore):

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
canonical recipe(四必选 flag)、三后端栈与踩坑史见 [`docs/locomo-e2e-eval-reproduction.md`](docs/locomo-e2e-eval-reproduction.md);
评分杠杆实验台账见 [`docs/locomo-score-levers.md`](docs/locomo-score-levers.md)。

## Project Structure
**项目结构**

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
**文档**

- [`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md) — **eval results master table (any external score quote must cite this)** / **评测结果总表(对外引用任何分数一律以本表为准)**
- [`docs/competitive-benchmarks.md`](docs/competitive-benchmarks.md) — competitor targets + methodology audit / 竞品对标基准 + 口径核对
- [`docs/memos-inhouse-locomo-repro.md`](docs/memos-inhouse-locomo-repro.md) — MemOS same-stack reproduction methodology / MemOS 同栈复现方法学正本
- [`docs/memory-strategy.md`](docs/memory-strategy.md) — tech & strategy source of truth, score-lever backlog / 技术与战略正本、涨点 backlog
- [`docs/memory-architecture.md`](docs/memory-architecture.md) — runtime architecture: extraction timing, write/retrieval/curation flow, SQLite schema / 运行架构总览
- [`docs/mcp-server.md`](docs/mcp-server.md) · [`docs/cli.md`](docs/cli.md) — adapter usage / 适配器用法
- [`docs/README.md`](docs/README.md) — full doc index (with status semantics) / 文档全索引(含状态语义)

## Development
**开发规范**

- **Spec-driven** with [github/spec-kit](https://github.com/github/spec-kit) — `constitution → specify → plan → tasks → implement`. Scaffolding in `.specify/`, Claude integration skills in `.claude/skills/`.
  **规范驱动开发**:采用 [github/spec-kit](https://github.com/github/spec-kit) —— `constitution → specify → plan → tasks → implement`。脚手架在 `.specify/`,Claude 集成 skills 在 `.claude/skills/`。
- **Constitution (five non-negotiables)**, source [`.specify/memory/constitution.md`](.specify/memory/constitution.md): ① local-first/offline ② engine-adapter separation ③ contract-first + namespace isolation ④ evaluation regression gate ⑤ graceful degradation + honest scale.
  **宪法(五条不可妥协)**,正本 [`.specify/memory/constitution.md`](.specify/memory/constitution.md):① local-first 默认离线 ② 引擎/适配器隔离 ③ 契约优先 + 命名空间隔离 ④ 评测回归门禁 ⑤ 优雅降级 + 诚实量级。
- **Commit discipline**: engine changes vs eval-config changes on separate commits (attribution); secrets never enter tracked files / logs / tool responses.
  **提交纪律**:引擎改动与评测配置改动分轨提交(可归因);密钥绝不进 tracked 文件 / 日志 / tool 响应。

## License
**协议**

engram is licensed under the [Apache License 2.0](./LICENSE). Copyright 2026 wallfacers.
Contributions are licensed under the same terms per License §5.

engram 基于 [Apache License 2.0](./LICENSE) 开源。Copyright 2026 wallfacers。
贡献者提交的 Contribution 依据该 License 第 5 条自动按相同条款许可。
