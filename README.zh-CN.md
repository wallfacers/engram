<h1 align="center">engram</h1>

<p align="center">
  <strong>AI Agent 的本地优先长期记忆。</strong>
</p>

<p align="center">
  纯 Go · 嵌入式 SQLite · MCP 与 CLI · 混合检索
</p>

<p align="center">
  <a href="README.md">English</a>
  · <a href="#快速开始">快速开始</a>
  · <a href="#架构">架构</a>
  · <a href="#基准评测">基准评测</a>
  · <a href="docs/README.md">文档</a>
</p>

<p align="center">
  <a href="https://github.com/wallfacers/engram/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/wallfacers/engram/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://go.dev/"><img alt="Go 1.25" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://github.com/wallfacers/engram/blob/master/LICENSE"><img alt="Apache 2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
  <img alt="CGO disabled" src="https://img.shields.io/badge/CGO-disabled-2f855a">
</p>

engram 为 Codex、Claude Code、Cursor 和自研 Agent 提供持久记忆，无需部署托管数据库。
每个命名空间存储为独立的本地 SQLite 文件；同一套纯 Go 引擎可通过 MCP stdio
server、适合自动化的 CLI 和可嵌入的 Go 包使用。

默认即可离线读写并使用关键词检索。只有需要语义检索、对话事实抽取或记忆整理时，
才需要接入兼容的本地或远程模型端点。

## 为什么选择 engram

| | |
|---|---|
| **默认本地**<br>数据保存在本机 SQLite 中，核心路径无需云账号，也不依赖网络。 | **没有模型也能用**<br>未配置任何模型端点时，CRUD、命名空间、导出和关键词检索仍然可用。 |
| **按需混合检索**<br>语义、BM25 关键词和实体信号通过免调参的 RRF（Reciprocal Rank Fusion）融合。 | **显式记忆生命周期**<br>由 Agent 决定何时写入或抽取；curation 已交付但默认关闭，普通聊天不会被隐式采集。 |
| **一个引擎，轻薄适配**<br>MCP 与 CLI 只调用引擎公开 API，不重复实现记忆逻辑。 | **部署简单**<br>纯 Go、无 CGO、嵌入式数据库，不需要单独运维向量数据库。 |

## 快速开始

### 1. 安装 Agent Skill

前置条件：Node.js >=22.20.0、npx/npm、Git 与网络。该命令只安装 skill；CLI
二进制与 MCP server 仍需分别安装和配置。默认命令在公共技能目录
`~/.agents/skills/engram` 只保留**一份共享拷贝**，其余客户端（包括只认自家
`~/.claude/skills/` 的 Claude Code）通过符号链接引用它；Codex/OpenCode 原生扫描
`~/.agents/skills/`，装完即被发现。`--agent <id>` 仅用于显式限定安装到某一客户端
的专有目录。

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram --global
```

请保留安装器的写入确认；默认选择 `Symlink`，受限文件系统选择 `Copy`。安装后重载
各客户端，并确认每个客户端恰好发现一个 `engram` skill。项目作用域、单客户端安装、
离线/手动后备、升级与恢复，请以唯一的 [skill 安装正本](skills/engram/references/install.md) 为准。

### 2. 构建并离线使用 CLI

需要 Go 1.25 或更高版本。

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

以上命令会创建 `data/default.db`，并完成一次降级但可用的离线搜索。后续配置 embedding
端点即可增加语义检索通道，已有数据和命令无需改变。

常用命令：

```bash
./bin/engram get preferred-editor
./bin/engram list
./bin/engram stats
./bin/engram export
./bin/engram delete preferred-editor
./bin/engram namespaces
./bin/engram version
```

对话抽取、curation、命名空间及自动化行为详见
[CLI 使用指南](docs/guides/cli.md)。

### 3. 接入 MCP 客户端

构建 stdio server：

```bash
CGO_ENABLED=0 go build -o ./bin/engram-mcp ./cmd/engram-mcp
```

在 MCP 客户端中注册二进制文件的绝对路径。不同客户端的外层配置格式可能不同：

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

未配置模型时，server 暴露 CRUD、不可损失的 `memory_ingest_v2` 以及
`memory_evidence_get`/生命周期工具；配置 LLM 后增加旧版事实抽取 `memory_ingest`。每个工具
都可指定 namespace，不同 namespace 使用相互隔离的数据库文件。

客户端接入、工具边界与可选 curation 详见
[MCP Server 配置指南](docs/guides/mcp-server.md)。

## 架构

```mermaid
flowchart LR
    subgraph clients["客户端"]
        agent["AI Agent<br/>Codex · Claude Code · Cursor"]
        app["Go 应用"]
    end

    subgraph interfaces["轻薄接入层"]
        mcp["MCP Server<br/>stdio"]
        cli["AI-first CLI"]
        api["公开 Go API"]
    end

    subgraph engine["记忆引擎 · 纯 Go"]
        write["直接写入<br/>与显式抽取"]
        search["混合检索<br/>语义 · BM25 · 实体 → RRF"]
        curate["可选 Curation"]
    end

    db[("每个 Namespace 一个 SQLite<br/>WAL · FTS5")]
    models["可选模型 Sidecar<br/>Embedding · LLM"]

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
    models -. "抽取与向量化" .-> write
    models -. "查询向量化" .-> search
    models -. "判定" .-> curate

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

实线是本地路径，模型连接均为可选：

1. **写入：** `add` / `memory_write` 直接保存调用方提供的内容；只有显式
   `ingest` 才会调用 LLM 抽取持久事实后写入。
2. **检索：** 关键词检索始终在本地运行；实体索引与依赖可用时，实体匹配和语义
   cosine 结果参与排序，RRF 只融合成功返回的通道。
3. **整理：** 有界 curation pass 会评分并去重记忆；只有 CLI 显式调用或 MCP server
   主动开启后才会执行。

实现边界、存储原语与 provenance 详见
[记忆系统架构](docs/architecture/memory-system.md)。

## 能力矩阵

| 能力 | 默认行为 | 可选依赖 |
|---|---|---|
| 本地 CRUD、列表、统计、导出 | 离线可用 | 无 |
| 关键词检索 | 离线可用 | 无 |
| 实体检索 | 已索引实体事实存在时参与 | 实体事实由显式 ingest 产生 |
| 语义检索 | 不可用时自动退出融合 | OpenAI 兼容 embedding 端点 |
| 对话事实抽取 | 无模型时不暴露 | OpenAI 或 Anthropic 兼容 LLM |
| 记忆 curation | 默认关闭 | LLM；CLI 显式命令或 MCP 开关 |
| 命名空间隔离 | 每个 namespace 一个 SQLite 文件 | 无 |

engram 当前面向本地、单用户、约 10 万条记忆规模，不是分布式向量服务，也尚未宣称
完整解决记忆新鲜度与状态一致性。已交付与规划能力的权威边界见
[当前能力边界](docs/product/capabilities.md)。

## 配置

非密钥 flag 会覆盖对应的 `ENGRAM_*` 环境变量。API key 只能通过环境变量提供，
不会作为命令行 flag 暴露。

| Flag | 环境变量 | 用途 |
|---|---|---|
| `--data-dir` | `ENGRAM_DATA_DIR` | namespace 数据库所在目录 |
| `--namespace` | `ENGRAM_NAMESPACE` | CLI namespace，默认 `default` |
| `--embed-base-url` | `ENGRAM_EMBED_BASE_URL` | OpenAI 兼容 embedding 端点 |
| `--embed-model` | `ENGRAM_EMBED_MODEL` | embedding 模型名 |
| — | `ENGRAM_EMBED_API_KEY` | embedding API key |
| `--llm-provider` | `ENGRAM_LLM_PROVIDER` | `openai` 或 `anthropic` |
| `--llm-base-url` | `ENGRAM_LLM_BASE_URL` | LLM 端点 |
| `--llm-model` | `ENGRAM_LLM_MODEL` | LLM 模型名 |
| — | `ENGRAM_LLM_API_KEY` | LLM API key |
| `--max-open-namespaces` | `ENGRAM_MAX_OPEN_NAMESPACES` | MCP namespace 缓存上限，默认 `64` |
| `--curation-enabled` | `ENGRAM_CURATION_ENABLED` | 开启 MCP 持久 curation，默认 `false` |

最后两项是 MCP server 配置。请以当前安装版本的 `engram-mcp --help` 为准确启动契约。

## 基准评测

| 基准 | 检索参数 | 答题模型 | 得分 | 备注 |
|---|---|---|---:|---|
| LoCoMo（1,540） | 900-char · k150 | Qwen3.6-35B | **91.43%** | 042 配对 · within-noise |
| LoCoMo（1,540） | 900-char · k150（子集） | Qwen3.8-27B | **91.10%** | k30 majority + 80 题 k150 重判 · [90pp 归因](docs/evaluation/reports/qwen3.8-27b-answerer-swap-2026-08-18.md) |
| LoCoMo（1,540） | 900-char · k30 · q28 | Qwen3.8-27B | **89.74%** | 生产配方锚 · [quota-28 verdict](docs/evaluation/reports/k30-chunk-quota-28-verdict-2026-08-18.md) |
| LoCoMo（1,540） | 900-char · k30 · q12 | Qwen3.8-27B | **89.48%** | 3-rep clean majority · [换答题模型 verdict](docs/evaluation/reports/qwen3.8-27b-answerer-swap-2026-08-18.md) |
| LoCoMo（1,540） | 900-char · k30 | Qwen3.6-35B | **87.9%** | 高于噪声（p=.019）· [038 verdict](docs/evaluation/reports/unified-answer-contract-verdict-2026-08-13.md) |
| LongMemEval-S（500） | 900-char · k30 · q12 | Qwen3.8-27B | **93.40%** | 同口径 +3.2pp · [换答题模型 verdict](docs/evaluation/reports/qwen3.8-27b-answerer-swap-2026-08-18.md) |
| LongMemEval-S（500） | 900-char · k150 | Qwen3.6-35B | **92.0%** | 3-run clean majority · [补跑记录](docs/operations/evaluation/lme-unified-k150-3rep-2026-08-15.md) |
| LongMemEval-S（500） | 900-char · k30 | Qwen3.6-35B | **90.2%** | 高于噪声（p=.0001） |

各行统一：unified 答题契约、deepseek-v4-flash 判题、clean 口径（只判 final answer）、三次配对多数投票。Qwen3.6-35B 即 Qwen3.6-35B-A3B-FP8；未标 quota 的行是 chunk-quota 机制引入前的配方。91.10% 行的全量 k150 等价值已在 Qwen3.6 上实测，同为 91.10%。

[评测详情与复现证据 →](docs/evaluation/results.md)

## 文档

| 目标 | 文档 |
|---|---|
| 浏览全部当前文档 | [文档门户](docs/README.md) |
| 安装 Claude Code、Codex 或 OpenCode skill | [skill 安装正本](skills/engram/references/install.md) |
| 使用 CLI | [CLI 使用指南](docs/guides/cli.md) |
| 接入 MCP 客户端 | [MCP Server 配置指南](docs/guides/mcp-server.md) |
| 理解运行架构 | [记忆系统架构](docs/architecture/memory-system.md) |
| 核对已交付与规划能力 | [当前能力边界](docs/product/capabilities.md) |
| 查看评测证据 | [当前评测结果](docs/evaluation/results.md) |
| 了解产品方向 | [产品路线图](docs/product/roadmap.md) |

<details>
<summary><strong>仓库结构</strong></summary>

```text
memory/         公开记忆引擎、检索、抽取与 curation
embedding/      embedding client 与可选 reranker 接口
provider/       LLM provider 抽象
store/          纯 Go SQLite 初始化与 migration
mcpserver/      MCP stdio 适配器与 namespace registry
cmd/engram/     AI-first CLI
cmd/engram-mcp/ MCP server 可执行程序
cmd/locomo-bench/ 评测工具
docs/           使用、架构、产品、评测与运维文档
specs/          契约优先的功能规格与计划
```

</details>

## 开发

硬性门禁是关闭 CGO 后完成全部构建与测试：

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
go vet ./...

node docs/validation/check-docs.mjs
node --test docs/validation/check-docs.test.mjs
```

检索、抽取、curation、存储或 embedding 的变更在合入前还必须完成可比较的评测。
项目原则见[记忆系统宪法](.specify/memory/constitution.md)，文档治理见
[docs/CONTRIBUTING.md](docs/CONTRIBUTING.md)。

## License

engram 基于 [Apache License 2.0](LICENSE) 开源。
