# Implementation Plan: engram CLI (AI-first)

**Branch**: `004-cli-ai-first` | **Date**: 2026-07-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-cli-ai-first/spec.md`

## Summary

在 engram 引擎之上新增**第三个薄适配层**——一个 AI-first 的 CLI(`cmd/engram`)。消费者是 **AI agent**(如编码助手 `engram search "..."` 读输出),因此差异点是 **AI 友好的输出与错误契约**:每个命令输出确定性 markdown 文档(沿用引擎 `RenderExport` 的 house style),每个失败输出「说清哪错+下一步」的诊断并给非零退出码。命令面 10 个,与 MCP 已冻结的工具语义逐一对齐(behavior-preserving),另加 `stats/export/namespaces/version` 运维面。适配层**只走引擎公开 API 组装**(`store.Open`→`NewEntryStore`/`NewVectorStore`/`NewEmbedder`/`NewRetriever`→`pipeline.New`),**不 import `mcpserver`**,引擎公开面与 store schema 逐字节不动。关键正确性:一次性进程在退出前必须 drain 异步 embedder,否则语义向量静默丢失。

## Technical Context

**Language/Version**: Go 1.25.0(承 002 已抬的 module 基线,不再变动)。

**Primary Dependencies**: 仅标准库(`flag` 做子命令分发,`os`/`io` 做 IO)+ 复用引擎 `github.com/wallfacers/engram/{memory,memory/pipeline,embedding,store}`。**不引入 cobra 等 CLI 框架**(依赖最小化;子命令面小、分发手写足够,见 research R1)。**不依赖 `mcpserver`**(adapter→adapter 耦合禁止)。

**Storage**: 每 namespace 一份本地 SQLite 文件(`<data-dir>/<ns>.db`,经引擎 `store.Open`);适配层不引入平行存储。

**Testing**: `go test`;子命令做成进程内纯函数 `run(args, stdin, stdout, stderr) int` 直接断言(快、CGO=0 友好);外加一条 `os/exec` 端到端 smoke;`parity_test` 证 `engram search` == 直接 `Retriever.Search`;`CGO_ENABLED=0 go build/test ./...` 门禁。

**Target Platform**: 本地可执行程序(Linux/macOS/Windows,交叉编译无 CGO),被 AI agent 以一次性子进程拉起。

**Project Type**: 单仓库内的 CLI/adapter(引擎库 + 新增 `cmd/engram` 二进制包)。

**Performance Goals**: 无强吞吐目标(本地单用户、一次性调用)。约束是启动+单命令延迟低到可被 agent 交互式调用;`add`/`ingest` 在配 embedding 时同步 drain 后再退出(正确性优先于延迟)。

**Constraints**: 默认离线可运行(无端点即可 add/search/get/list/delete + 降级检索);无 CGO;不改引擎公开 API 与 store schema;密钥零泄漏(仅 env,不进输出/错误/tracked 文件);输出确定性(便于测试与 agent 消费)。

**Scale/Scope**: 单用户;一次性进程,单命令只开当前 namespace 一份库;每 namespace ~10 万条级(承 001/002 规模声明,不超边界)。

## Constitution Check

*GATE: 设计前后各核对一次,五项原则全部满足,无需 Complexity Tracking。*

- **I. 本地优先/默认离线** ✅:`add/search/get/list/delete` 无端点即可跑;embedding/LLM 均可选,`embClient==nil` 即纯离线 FTS+实体检索;`ingest` 需 LLM 但缺失时是 AI 友好报错而非崩溃(FR-002/FR-007/SC-001)。
- **II. 引擎/适配分离** ✅:新增独立 `cmd/engram` 包,只经引擎公开构造器组装、只调公开 API;引擎不感知 CLI/namespace;**不 import mcpserver**;引擎内部零改动(FR-014/SC-006,`git diff -- memory embedding provider store internal` 空)。
- **III. 契约优先 & namespace 隔离** ✅:先出 `contracts/cli-commands.md` 冻结命令/flag/stdout形状/stderr诊断/退出码,再实现;namespace 物理独立库,`--namespace` 选择,校验+路径逃逸断言在适配层持有(FR-009/FR-010/SC-004)。
- **IV. 评测回归门禁** ✅(按构造):纯 adapter,不改引擎检索/抽取/存储算法,LoCoMo 指标不变;以 SC-002 parity(`engram search`==直接 `Retriever.Search`)+ 引擎单测全绿 + 引擎 diff 空作机器证明,替代全量 bench 重跑(见 research R6)。
- **V. 优雅降级 & 规模诚实** ✅:检索沿用引擎三路独立降级;降级标注取结构化诚实口径(是否配 embedding,不探测引擎);~10 万条规模与一次性生命周期如实写入文档。

**技术约束核对**:无 CGO(纯 Go + CGO=0 门禁)✅;依赖最小化(零新增外部依赖,仅标准库+引擎)✅;模型侧可替换(复用引擎 embedding/provider 接口)✅;单一存储真相(直接用引擎 store,无副本)✅。

## Project Structure

### Documentation (this feature)

```text
specs/004-cli-ai-first/
├── plan.md              # 本文件
├── research.md          # Phase 0:R1 CLI框架取舍 / R2 组装逻辑归属 / R3 一次性drain正确性 / R4 namespace校验归属 / R5 输出&错误契约 / R6 测试策略
├── data-model.md        # Phase 1:命令I-O、配置、namespace、退出码/诊断 实体与不变量
├── quickstart.md        # Phase 1:构建、配置、10 命令用法、离线验证、接入 agent
├── contracts/
│   └── cli-commands.md  # Phase 1:10 个命令的 名称/参数/flag/stdout形状/stderr诊断/退出码(冻结契约)
└── tasks.md             # Phase 2(/speckit-tasks 生成,非本命令)
```

### Source Code (repository root)

```text
cmd/
├── locomo-bench/           # 既有,不动
├── engram-mcp/             # 既有 MCP 适配二进制,不动
└── engram/                 # 新增:AI-first CLI(package main,对齐 locomo-bench 布局)
    ├── main.go             # 薄入口:os.Args → run(...) int → os.Exit
    ├── run.go              # 全局 flag 解析 + 子命令分发表 + 打开 namespace 句柄
    ├── config.go           # flags + ENGRAM_* env loader(自持,不 import mcpserver);key 仅 env
    ├── config_test.go      # flag-wins-over-env、key 不泄漏
    ├── namespace.go        # namespace 校验(^[A-Za-z0-9._-]{1,64}$)+ 路径逃逸断言(适配层自持)
    ├── namespace_test.go   # SC-004 逃逸拒绝表 + 库外零文件
    ├── engine.go           # 经公开 API 组装引擎句柄(Open→EntryStore/VectorStore/Embedder/Retriever→pipeline.New)+ Close(退出前 drain embedder)
    ├── add.go search.go get.go list.go delete.go ingest.go   # 每命令一文件 handler(并行编辑无争用)
    ├── stats.go export.go namespaces.go version.go            # 运维命令 handler
    ├── render.go           # AI 友好 markdown 渲染器(search/get/list/stats);export 复用 memory.RenderExport
    ├── render_test.go      # 确定性、pinned 优先、片段
    ├── errors.go           # AI 友好诊断构造 + 退出码常量
    ├── commands_test.go    # 集成:进程内 run() 驱动 US1/US2/US3
    ├── lifecycle_test.go   # FR-008/SC-003:配 embedding 时 add/ingest 退出前向量已落盘
    ├── parity_test.go      # SC-002:engram search == 直接 Retriever.Search
    └── e2e_test.go         # os/exec 端到端 smoke(构建后跑真二进制)

# 引擎包(memory/ embedding/ provider/ store/ internal/):本特性不改
# mcpserver/:本特性不 import、不改
```

**Structure Decision**:遵循既有布局——新增二进制入 `cmd/engram/`(对齐 `cmd/engram-mcp/`、`cmd/locomo-bench/` 的 `package main` + 多文件 + `_test.go` 惯例)。薄 `main` 委托给可测的 `run(args, stdin, stdout, stderr) int`,使每个子命令进程内可测。**组装逻辑(engine.go)与 namespace 校验(namespace.go)是与 mcpserver 概念同源但故意各自持有的副本**——不 import mcpserver(禁 adapter 耦合),不下沉引擎(引擎不认 namespace);二者最终的共享归宿是推后的 SDK 门面包,本特性明确不做(见 research R2)。

## Complexity Tracking

> 无宪法违背需要证成——本节留空。CLI 与 mcpserver 之间存在小段「组装逻辑 + namespace 校验」概念重复,但这是宪法 II(禁 adapter 互相 import)与 III(引擎不认 namespace)共同强制的合规结果,非可消除的复杂度;其收敛点(SDK 门面)已作为独立未来特性记录,不在本特性引入。
