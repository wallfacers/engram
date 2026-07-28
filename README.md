# engram

> 可嵌入任意智能体的**本地优先记忆层**:一套记忆引擎,多个集成面
> —— MCP server / AI-first CLI / SDK——让 Codex、Claude Code、Cursor、
> 自研 Agent 无需自建记忆即可拥有长期记忆。

[![Go](https://img.shields.io/badge/Go-1.25.0-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-disabled-00599C)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-SDK%20v1.5.0-0052CC)](https://modelcontextprotocol.io/)
[![Storage](https://img.shields.io/badge/SQLite-pure--Go%20%7C%20WAL%20%7C%20FTS5-003B57)](https://pkg.go.dev/modernc.org/sqlite)
[![Retrieval](https://img.shields.io/badge/retrieval-semantic%20%2B%20BM25%20%2B%20entity%20RRF-success)](#架构)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

三路混合检索(语义 + BM25 关键词 + 实体,RRF 融合)+ ADD-only 事实抽取
+ 确定性 curation,完全离线可运行,纯 Go、无 CGO、可交叉编译。核心引擎抽离自
[workhorse-agent](https://github.com/wallfacers/workhorse-agent) 的记忆子系统。

---

## 目录

- [特性](#特性)
- [基准评测](#基准评测)
- [架构](#架构)
- [快速开始](#快速开始)
- [配置](#配置)
- [评测与复现](#评测与复现)
- [项目结构](#项目结构)
- [文档](#文档)
- [开发规范](#开发规范)
- [License](#license)

## 特性

- **Local-first,默认离线** —— 核心读写路径无需网络/云;SQLite 本地文件;embedding / LLM 是可替换的本地 sidecar(Ollama / fastembed / 任意 OpenAI 兼容端点),绝非强制依赖的托管服务。
- **三信号混合检索** —— 语义(cosine)+ 关键词(FTS5 BM25,LIKE 兜底)+ 实体(精确匹配),RRF(k=60,免调参)融合;可选 cross-encoder reranker。信号逐路降级,缺哪路自动剔除、绝不整体失败。
- **引擎 / 适配器隔离** —— 记忆引擎是独立、无宿主、可单测的库;MCP / CLI / SDK 只通过引擎公开 API 调用,不耦合宿主逻辑。
- **命名空间隔离** —— 一个命名空间 = 一个独立引擎 store(`<dataDir>/<ns>.db`),LRU + 驱逐;跨命名空间访问默认关闭。路径逃逸读写 = 0。
- **评测回归门禁** —— 检索 / 抽取 / curation / 存储 / embedding 的任何改动,合入前必须跑可比指标的 LoCoMo 评测且不得回归基线。

## 基准评测

> ⚠️ **读表前必读**:每一个数字都是 **(数据集 × 答题模型 × 判题模型 × 配方)**
> 的函数,**不是"engram 的分"**。跨行比较只在**恰好一个轴不同**时有效;
> 与他人 leaderboard 数字**一律不可直接横比**。🔬 = 本项目同栈实测,📣 = 厂商自报未复现。
> 正本:[`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md)。

**统一条件**:嵌入 `bge-large-en-v1.5` 1024d(本地 sidecar);engram 检索走 canonical recipe
(`--top-k 30 --chunk-quota 12 --force-answer`,hybrid,**无 reranker**);判题用 mem0-aligned prompt。

### 同栈实测 —— 唯一可直接比较的表

同一台机器的 Qwen 答题 + bge-large 嵌入 + 同一 judge prompt / 同一 judge 模型;竞品跑自家代码,零改动。

| 数据集 (n) | 框架 | 答题模型 | 判题模型 | 得分 |
|---|---|---|---|---:|
| **LoCoMo (1540)** | **engram** 🔬 | Qwen3.6-35B · 本地 vllm | deepseek-v4-flash | **85.71%** |
| LoCoMo (1540) | engram 🔬 | Qwen3.6-35B · 本地 vllm | deepseek-v4-pro | 83.77% |
| LoCoMo (1540) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **89.03%** |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · 本地 vllm | deepseek-v4-flash | 82.40% |
| LoCoMo (1540) | MemOS 🔬 | Qwen3.6-35B · 本地 vllm | deepseek-v4-pro | 80.26% |
| **LongMemEval-S (500)** | **engram** 🔬 | Qwen3.6-35B · 本地 vllm | deepseek-v4-flash | **80.80%** |
| LongMemEval-S (500) | engram 🔬 | deepseek-v4-pro · API | deepseek-v4-flash | **86.00%** |

### 各方自报最好成绩 —— 不可直接比较(栈不同)

| 数据集 | engram 🔬 | MemOS 📣 | Mem0 📣 |
|---|---:|---:|---:|
| LoCoMo | **89.03%**(v4-pro,n=1540) | 88.83 | 92.5 |
| LongMemEval | **86.00%**(v4-pro,S-cleaned 500) | 89.20 | 94.4 |

### 三个关键 Δ

| 轴 | 变化 | 净效应 | 结论 |
|---|---|---:|---|
| **框架(同栈)** | engram − MemOS,LoCoMo 1540 | **+3.31pp**(v4-flash)/ **+3.51pp**(v4-pro) | engram 领先,两个 judge 下均成立 |
| **答题模型** | Qwen → v4-pro | **+3.32pp**(LoCoMo)/ **+5.20pp**(LME,p=0.0049) | 强 answerer 主要兑现 temporal / open-domain |
| **判题模型** | v4-flash → v4-pro | **−2 ~ −3pp** | 加性偏移,不改变任何 Δ 的方向 |

> Mem0 的 92.5 / 94.4 来自**托管平台**(含开源 SDK 不带的私有优化)+ `top_200` 检索预算,
> 无法同栈复现;对 Mem0 的真实差距 = **未知数**。MemOS 自报 88.83 → 同栈 82.40,
> **−6.43pp 全是 regime 伪影**(答题模型强度 + judge 宽松度)。

## 架构

```
        ┌─────────────── 适配层(薄) ───────────────┐
        │  mcpserver (stdio)   cmd/engram (CLI)       │
        │         MCP / CLI / SDK 仅调用公共 API       │
        └──────────────────────┬───────────────────────┘
                               ▼  公共 API
        ┌─────────────── 引擎层(纯库,离线) ──────────┐
        │  memory/   entrystore · retriever · pipeline │
        │            curation · prompt · queryplan     │
        │  embedding/  embedder + reranker (可选)      │
        │  provider/    LLM 抽象 (anthropic / openai)  │
        │  store/       SQLite: WAL · FTS5 trigram     │
        └──────────────────────────────────────────────┘
```

**检索流水线**:query → 三路并行(语义 cosine / FTS5 BM25 / 实体精确匹配)
→ RRF(k=60)融合 → (可选)cross-encoder rerank → 返回。任一路信号缺失即静默退出融合,
不拖垮整体;适配器只从**结构性事实**(如"未配置 embedding 端点")报告降级,不探测引擎。

## 快速开始

要求 Go 1.25+。全部构建与测试在 `CGO_ENABLED=0` 下通过。

```bash
git clone https://github.com/wallfacers/engram.git && cd engram
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test  -count=1 ./...
```

### 作为 MCP server 接入(Codex / Claude Code / Cursor)

```bash
CGO_ENABLED=0 go build -o engram-mcp ./cmd/engram-mcp
```

在 MCP 客户端配置中注册本地 stdio 进程:

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

启动后,Agent 可调用 `memory_write` / `memory_search` / `memory_list` / `memory_get` /
`memory_delete`;配置 LLM 后额外出现 `memory_ingest`。**不配置任何端点仍可离线运行。**
接入细节见 [`docs/mcp-server.md`](docs/mcp-server.md)。

### 作为 AI-first CLI

```bash
CGO_ENABLED=0 go build -o engram ./cmd/engram

engram --data-dir ~/.engram/memory add    --name dark-mode --content "用户偏好深色模式" --category preference
engram --data-dir ~/.engram/memory search "外观设置"
engram --data-dir ~/.engram/memory get    dark-mode
engram --data-dir ~/.engram/memory list
engram --data-dir ~/.engram/memory delete dark-mode
printf 'user: 上个月搬到柏林。\nassistant: 已记录。\n' | engram --data-dir ~/.engram/memory ingest   # 需 LLM
engram --data-dir ~/.engram/memory stats
```

成功命令向 stdout 写确定性 markdown,失败向 stderr 写一条可操作诊断并返回非零退出码——
便于 AI Agent / cron / CI 直接消费。完整命令见 [`docs/cli.md`](docs/cli.md)。

## 配置

非密钥配置既可用全局 flag、也可用 `ENGRAM_*` 环境变量(flag 覆盖环境变量)。
**API key 只走环境变量,绝不作为 flag 传入,也绝不写入日志 / tool 响应 / 配置文件。**

| Flag | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | 必填 | SQLite 存储目录 |
| `--namespace` | `ENGRAM_NAMESPACE` | `default` | 命名空间(CLI) |
| — | `ENGRAM_MAX_OPEN_NAMESPACES` | — | 命名空间 LRU 上限(MCP) |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | 离线 | OpenAI 兼容 `/v1` 端点 |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | 离线 | 嵌入模型名 |
| — | `ENGRAM_EMBED_API_KEY` | — | 嵌入密钥(仅 env) |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | ingest 不可用 | `anthropic` / `openai` |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | ingest 不可用 | LLM 端点 |
| `--llm-model` | `ENGRAM_LLM_MODEL` | ingest 不可用 | LLM 模型名 |
| — | `ENGRAM_LLM_API_KEY` | — | LLM 密钥(仅 env) |

## 评测与复现

engram 的检索保真度由确定性 parity golden(`testdata/parity/`)+ LoCoMo harness 证明,而非靠信任。
离线门禁无需外网:

```bash
go test ./memory -run TestRetrievalParity        # 检索 parity(memory_search == Retriever.Search)
go test ./memory -run TestSignalDegradation      # 三路信号逐路降级
```

端到端答题评测需自备数据集与端点(`.locomo-run/`、`*.db`、`testdata/locomo/` 均已 gitignore):

```bash
go build ./cmd/locomo-bench
export LOCOMO_API_KEY=...      LOCOMO_BASE_URL=...   LOCOMO_MODEL=...   LOCOMO_PROVIDER=anthropic
export EXTRACT_MODEL=...
export EMBED_BASE_URL=http://127.0.0.1:11434/v1   EMBED_MODEL=...   EMBED_API_KEY=...
go run ./cmd/locomo-bench --data ./path/to/locomo.json \
      --run-dir ./.locomo-run --retrieval both
```

canonical recipe(四必选 flag)、三后端栈与踩坑史见
[`docs/locomo-e2e-eval-reproduction.md`](docs/locomo-e2e-eval-reproduction.md);
评分杠杆实验台账见 [`docs/locomo-score-levers.md`](docs/locomo-score-levers.md)。

## 项目结构

```
memory/        引擎核心(公开包):entrystore / retriever / pipeline / curation / prompt / queryplan
embedding/     引擎:embedder + reranker(OpenAI 兼容 /v1,可选)
provider/      引擎:LLM 抽象(+ anthropic / + openai)
store/         引擎:SQLite(modernc,纯 Go)— Open / Options / migrations / ProbeFTS5
internal/      引擎内部:idgen / version(不对外)
mcpserver/     适配器:MCP stdio server(config / namespace / registry / server / tools)
cmd/
  engram-mcp/    MCP server 二进制(薄 main)
  engram/        AI-first CLI 二进制
  locomo-bench/  LoCoMo 评测 harness
specs/          spec-kit SDD:specs/NNN-feature/ 的 spec·plan·tasks·research·data-model·contracts
docs/           战略 / 背景 / 竞品 / 适配器用法 / 评测 / 未决问题
testdata/       parity goldens;locomo/ 数据集(gitignored)
```

## 文档

- [`docs/results-matrix-2026-07-26.md`](docs/results-matrix-2026-07-26.md) — **评测结果总表(对外引用任何分数一律以本表为准)**
- [`docs/competitive-benchmarks.md`](docs/competitive-benchmarks.md) — 竞品对标基准 + 口径核对
- [`docs/memos-inhouse-locomo-repro.md`](docs/memos-inhouse-locomo-repro.md) — MemOS 同栈复现方法学正本
- [`docs/memory-strategy.md`](docs/memory-strategy.md) — 技术与战略正本、涨点 backlog
- [`docs/memory-architecture.md`](docs/memory-architecture.md) — 运行架构总览:抽取时机、写入/检索/curation 流程、SQLite 表图
- [`docs/mcp-server.md`](docs/mcp-server.md) · [`docs/cli.md`](docs/cli.md) — 适配器用法
- [`docs/background-extraction-from-workhorse-agent.md`](docs/background-extraction-from-workhorse-agent.md) — 立项背景与边界
- [`docs/README.md`](docs/README.md) — 文档全索引(含状态语义)

## 开发规范

- **规范驱动开发**:采用 [github/spec-kit](https://github.com/github/spec-kit) ——
  `constitution → specify → plan → tasks → implement`。脚手架在 `.specify/`,Claude 集成 skills 在 `.claude/skills/`。
- **宪法(五条不可妥协)**,正本 [`.specify/memory/constitution.md`](.specify/memory/constitution.md):
  ① local-first 默认离线 ② 引擎/适配器隔离 ③ 契约优先 + 命名空间隔离 ④ 评测回归门禁 ⑤ 优雅降级 + 诚实量级。
- **提交纪律**:引擎改动与评测配置改动分轨提交(可归因);密钥绝不进 tracked 文件 / 日志 / tool 响应。

## License

engram 基于 [Apache License 2.0](./LICENSE) 开源。Copyright 2026 wallfacers。

依据该 License 第 5 条,贡献者提交的 Contribution 自动按相同条款许可。
