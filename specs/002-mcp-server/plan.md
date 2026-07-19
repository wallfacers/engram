# Implementation Plan: MCP Server 适配层

**Branch**: `002-mcp-server` | **Date**: 2026-07-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-mcp-server/spec.md`

## Summary

在 engram 引擎之上新增一个**独立、薄适配层的 MCP server**(stdio 传输),把引擎的记忆读写能力经 Model Context Protocol 暴露给任意 MCP 客户端。采用官方 Go SDK `modelcontextprotocol/go-sdk` v1.5.0(纯 Go、仅 stdio)。多 namespace 隔离**在适配层实现**——每 namespace 一份独立引擎 store(`dataDir/<ns>.db`),由 Registry 惰性打开、LRU 有界缓存;引擎公开面与 store schema 逐字节不动(守住 001 已对拍行为)。可选 LLM 抽取工具按 provider 配置显隐,保 P1/P2 纯离线。

## Technical Context

**Language/Version**: Go 1.25.0(SDK v1.5.0 强制 Go≥1.25;由此把 module 基线从 001 的 1.22 抬到 1.25.0。Go 向后兼容,引擎行为不变、既有测试全绿。见 research R1 修订)

**Primary Dependencies**: `github.com/modelcontextprotocol/go-sdk` v1.5.0(仅 stdio);复用引擎 `github.com/wallfacers/engram/{memory,memory/pipeline,embedding,provider,store}`。无新增需 C 工具链的依赖。

**Storage**: 每 namespace 一份本地 SQLite 文件(`modernc.org/sqlite`,纯 Go,经引擎 `store.Open`);适配层不引入平行存储。

**Testing**: `go test`;契约测用 SDK 进程内(in-memory)client↔server 传输;`CGO_ENABLED=0 go build ./...` 门禁。

**Target Platform**: 本地可执行程序(Linux/macOS/Windows,交叉编译无 CGO),被 MCP 客户端以子进程 + stdio 拉起。

**Project Type**: 单仓库内的 CLI/adapter(引擎库 + 新增 `cmd/engram-mcp` 二进制 + `mcpserver` 适配包)。

**Performance Goals**: 无强吞吐目标(本地单用户,交互式)。约束是不因 namespace 增多而无界耗资源(FR-013:LRU 有界句柄)。

**Constraints**: 默认离线可运行(无外网/无端点即可启动并提供写/列/查/删/降级检索);无 CGO;不改引擎公开 API 与 store schema;密钥零泄漏。

**Scale/Scope**: 单用户;单进程多 namespace(默认 LRU 上限 64 个同时打开);每 namespace ~10 万条级(承 001 规模声明,不超边界)。

## Constitution Check

*GATE: 已在设计前后各核对一次,五项原则全部满足或以更简方案规避,无需 Complexity Tracking。*

- **I. 本地优先/默认离线** ✅:MVP 仅 stdio;embedding/LLM 均为可选,`embClient==nil` 即纯离线 FTS+实体检索,server 无端点也能启动(US2/FR-014)。无托管服务被设为必需。
- **II. 引擎/适配分离** ✅:新增独立 `cmd/engram-mcp` + `mcpserver` 包,只通过引擎公开 API 交互;引擎不感知 MCP/namespace。新增集成面**不改引擎内部契约**(FR-021/SC-005)。
- **III. 契约优先 & namespace 隔离** ✅:先出 `contracts/mcp-tools.md` 冻结工具契约再实现;namespace 隔离是本特性一等目标(US3/FR-009~013),物理独立库,默认不可跨越(FR-022 明确不做跨 namespace 访问)。
- **IV. 评测回归门禁** ✅(按构造):适配层不改引擎算法,LoCoMo 指标不变;以 SC-003 parity(MCP 检索==直接检索)+ SC-005(引擎单测全绿、公开面/schema 不变)作机器证明,替代全量 bench 重跑(见 research R6)。
- **V. 优雅降级 & 规模诚实** ✅:检索沿用引擎三路独立降级;降级标注取**结构化诚实口径**(是否配 embedding,不谎称,见 research R3);LRU 上限与 ~10 万条规模如实写入文档,不夸大。

**技术约束核对**:无 CGO(SDK 纯 Go,CGO=0 构建门禁)✅;依赖最小化(仅加一个官方可审计依赖)✅;模型侧可替换(复用引擎 embedding/provider 接口)✅;单一存储真相(直接用引擎 store,无副本)✅。

## Project Structure

### Documentation (this feature)

```text
specs/002-mcp-server/
├── plan.md              # 本文件
├── research.md          # Phase 0:R1 SDK选型 / R2 namespace注册表 / R3 降级口径 / R4 抽取显隐 / R5 布局 / R6 测试
├── data-model.md        # Phase 1:namespace/Registry/工具I-O/配置 实体与不变量
├── quickstart.md        # Phase 1:构建、配置、接客户端、跑测、离线验证
├── contracts/
│   └── mcp-tools.md     # Phase 1:6 个 MCP 工具的名称/输入schema/输出形状/错误映射(冻结契约)
└── tasks.md             # Phase 2(/speckit-tasks 生成,非本命令)
```

### Source Code (repository root)

```text
cmd/
├── locomo-bench/            # 既有,不动
└── engram-mcp/
    └── main.go              # 新增:薄入口——解析配置、构建 Registry+SDK server、Run stdio

mcpserver/                   # 新增:可测适配包(引擎无耦合)
├── config.go               # Config:数据目录、可选 embedding/LLM 端点·模型·密钥、LRU 上限
├── config_test.go
├── namespace.go            # namespace 校验(路径逃逸白名单)+ 默认 namespace
├── namespace_test.go       # SC-009 逃逸拒绝表
├── registry.go             # namespace→引擎单元 惰性开库 + LRU 有界缓存 + Close
├── registry_test.go        # 惰性/隔离/淘汰
├── server.go               # 构建 mcp.Server,按 provider 显隐注册工具,降级标注
├── tools.go                # 6 个工具 handler(write/search/list/get/delete/ingest)
├── tools_test.go           # 契约/集成:in-memory client 驱动 US1/US2/US3/US4
└── parity_test.go          # SC-003:memory_search == 直接 Retriever.Search

# 引擎包(memory/ embedding/ provider/ store/ internal/):本特性不改(除非记录为 001 契约增量)
```

**Structure Decision**:遵循既有布局——新增二进制入 `cmd/engram-mcp/`(对齐 `cmd/locomo-bench/`),适配逻辑入顶层可测包 `mcpserver/`(对齐顶层 `memory/`)。薄 main + 厚可测包,满足宪法「测试先行/契约测试」。命名 `mcpserver` 规避与 SDK `mcp` import 标识冲突。

## Complexity Tracking

> 无宪法违背需要证成——本节留空。namespace 纳入虽扩大范围,但以"适配层每 namespace 独立库"的更简方案实现,未触碰引擎、未加引擎迁移,不构成复杂度违规。
