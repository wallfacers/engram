# engram

**可嵌入任意智能体的本地优先记忆层。** 一套记忆引擎,三个集成面
——MCP server / skill·CLI 客户端 / API SDK——让 Codex、Claude Code、Cursor、
自研 Agent 无需自建记忆即可获得长期记忆:三路混合检索(语义 + BM25 + 实体 RRF)
+ 抽取 + curation,完全离线可运行,无 CGO 依赖。

engram 的核心引擎抽离自 [workhorse-agent](https://github.com/wallfacers/workhorse-agent)
的记忆子系统(已经 LoCoMo 五轮消融调优)。

## 当前状态

engram 已是可独立构建的 Go module，核心路径使用纯 Go SQLite（`modernc.org/sqlite`），不需要 CGO。
记忆引擎、确定性检索对拍和 `locomo-bench` 已在本仓库内；LoCoMo 端到端评测需要运行者提供数据集和本地或私有模型端点。

## 使用

```bash
go build ./...
go test ./...
CGO_ENABLED=0 go build ./...
go vet ./...
```

### 注册 engram MCP server

先构建 MCP server，然后在 MCP 客户端配置中注册本地 stdio 进程：

```bash
CGO_ENABLED=0 go build -o ./engram-mcp ./cmd/engram-mcp
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

启动后，Agent 可以调用 `memory_write`、`memory_search`、`memory_list`、
`memory_get` 和 `memory_delete`。不配置 embedding 或 LLM 端点时仍可离线运行；
LLM provider 通过 `ENGRAM_LLM_*` 环境变量配置，密钥不会作为命令行参数传入。

检索保真门禁和三路信号降级测试无需外网：

```bash
go test ./memory -run TestRetrievalParity
go test ./memory -run TestSignalDegradation
```

评测工具可先独立构建：

```bash
go build ./cmd/locomo-bench
```

有 LoCoMo 数据集和端点后，用 `--conversations`、`--questions` 做小子集运行：

```bash
export LOCOMO_API_KEY=...
export LOCOMO_BASE_URL=http://127.0.0.1:4000/anthropic
export LOCOMO_MODEL=...
export EXTRACT_MODEL=...
export EMBED_BASE_URL=http://127.0.0.1:11434/v1
export EMBED_MODEL=...
export EMBED_API_KEY=...
go run ./cmd/locomo-bench \
  --data /path/to/locomo.json \
  --run-dir ./testdata/locomo-run \
  --conversations 1 \
  --questions 2
```

完整命令、对拍 fixture 和评测资源要求见 [`quickstart.md`](specs/001-memory-engine-extraction/quickstart.md)。

## 文档

- [`docs/background-extraction-from-workhorse-agent.md`](docs/background-extraction-from-workhorse-agent.md)
  —— 立项背景:为什么从 workhorse-agent 抽离、抽离什么、三个集成面的产品形态。
- [`docs/memory-strategy.md`](docs/memory-strategy.md)
  —— 技术与战略正本:LoCoMo 调优结论、国内外竞品核查、MemOS 88.83 拆解、
  生物启发检索机制调研(Ecphory/Chronos/D-MEM/Abstain-R1/FadeMem/HippoRAG…)
  与统一涨点优先级。

## 开发规范

本项目采用 [github/spec-kit](https://github.com/github/spec-kit) 规范驱动开发:
`constitution → specify → plan → tasks → implement`。脚手架已初始化于 `.specify/`,
Claude 集成 skills 在 `.claude/skills/`。

## 规格与实现记录

- [`specs/001-memory-engine-extraction/`](specs/001-memory-engine-extraction/)
  —— 记忆引擎抽离的 constitution、spec、plan、tasks、research、契约和验证入口。
