# Implementation Plan: 记忆引擎抽离(Memory Engine Extraction)

**Branch**: `001-memory-engine-extraction` | **Date**: 2026-07-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-memory-engine-extraction/spec.md`

## Summary

把 workhorse-agent 的记忆引擎(`internal/memory` + 子包 `curation`/`pipeline` + `cmd/locomo-bench`)搬进 engram,成为 module `github.com/wallfacers/engram` 下自洽、可离线独立构建的库。通用基础设施(`embedding`/`idgen`/`provider`)整包搬;混杂的共享包(`store/sqlite`、`prompt`)按记忆相关性切片,engram 只拥有含记忆 schema 的独立 store 与两份记忆 prompt;宿主耦合 `sessionsearch.BuildPlan/LikeFragments`(纯 CJK 分词、零宿主依赖)内化进检索层。保真通过**确定性检索对拍**证明,`locomo-bench` 随迁作回归 + 论文设施。

## Technical Context

**Language/Version**: Go 1.22(与源仓库 `go 1.22.0` 对齐)

**Primary Dependencies**: `modernc.org/sqlite`(纯 Go,无 CGO);本地 embedding sidecar(Ollama / fastembed,经 `EMBED_BASE_URL` 等环境变量指向);LLM provider 抽象(anthropic / openai 兼容端点,抽取/curation 用)

**Storage**: 单文件 SQLite;engram 只含记忆 schema(`memory_entries` / `memory_entries_fts` + 触发器 / `memory_entities` / `memory_embeddings` / `memory_curation_lease`)

**Testing**: `go test ./...`(既有单测平移)+ 新增确定性检索对拍 golden test

**Target Platform**: Linux / macOS,纯 Go 可交叉编译;核心路径离线可运行

**Project Type**: 单一 Go module(库 + 一个 `cmd/` 评测工具)

**Performance Goals**: 不改变现状;保持源引擎在单用户 ~10 万条级记忆的 Go 余弦扫描性能

**Constraints**: 无 CGO;核心功能路径无外网/无宿主源码可运行;抽离 diff 行为保真(检索输出逐条一致)

**Scale/Scope**: 抽离约 5910 行 memory + 2405 行子包 + 1933 行 bench + 切片的 store/prompt/sessionsearch 子集;单开发者,单里程碑

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 门禁 | 本特性对齐 |
|------|------|-----------|
| I. 本地优先,默认离线 | 核心路径无云依赖、无 CGO | ✅ 沿用 `modernc.org/sqlite` + 本地 sidecar;抽离不引入托管必需项 |
| II. 引擎与适配层分离 | 引擎不依赖宿主类型 | ✅ 本特性正是实现该原则:切断 `sessionsearch`,store 不携带会话/权限 schema |
| III. 契约优先与命名空间隔离 | 先定契约再实现 | ⚠️ 本特性**刻意冻结**公开 API 为"与抽离前同形"(FR-013);namespace 隔离留待后续契约 spec。属**范围内的有意推迟**,非违背——见下 |
| IV. 评测回归门禁 | 合并前跑可比评测、不回退 | ✅ 确定性检索对拍(SC-003 逐条 100%)+ `locomo-bench` 随迁 + 全量一次性 sanity check |
| V. 优雅降级与规模诚实 | 信号独立降级、规模如实 | ✅ FR-009 要求三路信号降级行为与抽离前一致;不改规模边界 |

**原则 III 说明**:本特性是"纯搬运保真",若同时重设计对外契约会破坏可归因性(违背原则 IV"口径改动与算法改动分开")。故本特性冻结 API 形状、只搬家;namespace 隔离与错误语义规整作为紧邻的后续「契约」spec。此推迟已在 spec Assumptions 与 FR-013 显式声明,不作为未证成的复杂度。**门禁通过。**

## Project Structure

### Documentation (this feature)

```text
specs/001-memory-engine-extraction/
├── plan.md              # 本文件
├── research.md          # Phase 0:切片归属与保真策略研究
├── data-model.md        # Phase 1:记忆 schema 与实体
├── quickstart.md        # Phase 1:构建/测试/对拍/bench 上手
├── contracts/           # Phase 1:公开 Go API 面契约(非 REST)
│   └── go-api-surface.md
└── tasks.md             # Phase 2:speckit-tasks 生成(非本命令产出)
```

### Source Code (repository root)

```text
engram/
├── go.mod                      # module github.com/wallfacers/engram, go 1.22
├── go.sum
├── memory/                     # 原 internal/memory → 提为公开包(对外入口)
│   ├── entrystore.go  retriever.go  embedder.go  vectorstore.go
│   ├── entities.go  block.go  budgets.go  writer.go  usagelog.go
│   ├── migrate.go  snapshot.go  export.go
│   ├── queryplan.go            # 新增:内化 sessionsearch 的 buildPlan/likeFragments(纯 CJK 分词)
│   ├── queryplan_test.go       # 随迁 sessionsearch 相关测试
│   ├── *_test.go               # 既有单测平移
│   ├── curation/               # 原 internal/memory/curation
│   ├── pipeline/               # 原 internal/memory/pipeline
│   └── prompt/                 # 切片:memory_extraction.go + curation_judge.go + template 依赖闭包
├── embedding/                  # 原 internal/embedding,整包搬
├── provider/                   # 原 internal/provider(含 anthropic/ openai/),整包搬
├── store/                      # 切片:仅记忆 schema 的独立 store(合并两个宿主 store 包的记忆闭包)
│   ├── store.go                # 切自 internal/store:记忆接口/类型(Store/ErrNotFound/Upsert/BumpUsage),去会话类型
│   ├── sqlite.go               # 切自 internal/store/sqlite:Open/Options/Store(去会话/权限)
│   ├── migrations.go           # 仅记忆迁移链(renumber v1..vN)
│   ├── funcs.go                # 仅 ProbeFTS5(去 extract_text)
│   └── *_test.go               # fts5/migrations/probe 相关记忆测试
├── internal/idgen/             # 原 internal/idgen,整包搬(engram 内部)
├── internal/version/           # 原 internal/version,搬入;UserAgent 去品牌化为 "engram/"(provider 依赖,见 research R1b)
├── cmd/locomo-bench/           # 原样搬:回归 + 论文设施
└── testdata/parity/            # 新增:确定性对拍的固定语料/query/向量与基线快照
```

**Structure Decision**: 单一 Go module。顶层 `memory/` 为公开入口包;`store/`、`embedding/`、`provider/` 为其依赖的公开基础设施包;`internal/idgen/` 为不对外的内部包;`cmd/locomo-bench/` 为评测工具。目录以"包语义"划分,与源仓库最大程度同构以保证行为保真与可归因 diff。

## Complexity Tracking

> 无违背宪法的复杂度需要证成。原则 III 的 API 冻结为范围内有意推迟,已在 Constitution Check 说明,不构成违背。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (无) | — | — |
