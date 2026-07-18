# Implementation Plan: 生物启发检索涨点 —— 顺序可测四枪闭合 Mem0 gap

**Branch**: `003-bio-retrieval-locomo` | **Date**: 2026-07-19 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/003-bio-retrieval-locomo/spec.md`

## Summary

在 local-first 约束内把 engram 的 LoCoMo / LongMemEval_S 推到 85+（对标 Mem0 2026-04：
92.5 / 94.4），方法为**顺序可测四枪**：先建多跑测量地基（US1）与模型 regime 校准基线
（US2），再依次上多跳联想检索（US3，实体平表→共现图+游走）、检索侧时间结构化（US4）、
拒答校准+冲突消解（US5）。每枪独立开关、独立多跑测量（95% CI + 配对 diff），未达
±3-5pp 噪声带即回退；全程受预算门禁（single-pass、token 同量级、每跑算账）约束。

技术路线：引擎侧（`memory/`、`store/`）加 migration v3（共现边表、事件时间范围、
被取代标记）与两路新检索信号（联想游走、时间相关性），全部纯 Go、可独立降级；评测侧
（`cmd/locomo-bench`）加 `--repeats`/统计报告/配对 diff/LongMemEval_S 加载器/费用账。
评测口径改动与算法改动分开提交（宪法 IV）。

## Technical Context

**Language/Version**: Go 1.24（现 go.mod 约定，纯 Go、无 CGO）

**Primary Dependencies**: 标准库 + `modernc.org/sqlite`（既有）；不新增第三方依赖
（统计计算、图游走、时间解析均标准库实现）

**Storage**: 单文件 SQLite（migration v3 增量：`memory_entity_edges`、
`memory_event_aliases`、`memory_entries` 加 `event_start/event_end/superseded_by` 列）

**Testing**: `go test`（契约/集成测试离线可跑）；检索保真门禁 `TestRetrievalParity`、
降级测试 `TestSignalDegradation` 须扩展覆盖新信号；评测回归用 `cmd/locomo-bench` 多跑

**Target Platform**: Linux/macOS/Windows 单二进制（交叉编译，CGO_ENABLED=0）

**Project Type**: Go library（engine）+ CLI（bench harness）

**Performance Goals**: 检索延迟不显著回退（图游走 depth≤2、边表带索引）；答题
single-pass；答题上下文 token ≤ 校准基线 1.5×（SC-007）

**Constraints**: 离线可运行（宪法 I）；引擎不依赖宿主（宪法 II）；schema 变更走
migration + MAJOR 语义（宪法 III）；评测口径与算法分开提交（宪法 IV）；每信号独立
降级（宪法 V）；每次评测先预估费用、跑后记账（FR-014）

**Scale/Scope**: 单用户 ~10 万条记忆；LoCoMo 10 段对话全量 ×N 跑；LongMemEval_S
~500 题（分批、维护者控预算）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 状态 |
|------|------|------|
| I 本地优先默认离线 | 新机制全部本地纯 Go：共现图、游走、规则式时间解析、非破坏压制。别名生成用抽取期已有的 LLM 调用顺带产出（写入侧，可指本地端点），查询侧零新增 LLM 调用 | ✅ |
| II 引擎/适配层分离 | 图/时间/压制在 `memory/`+`store/`（引擎）；多跑/统计/LME 加载/费用账在 `cmd/locomo-bench`（评测工具）；互不渗透 | ✅ |
| III 契约优先/命名空间 | schema 变更走 migration v3 并在 contracts/ 先定契约；新增列均 nullable/默认值，向后兼容（非 MAJOR）；不触及 namespace 语义 | ✅ |
| IV 评测回归门禁 | 本 feature 即是把门禁升级为多跑+CI 的工程；每枪算法改动与评测口径改动分开提交；每枪合并前跑可比口径多跑 | ✅ |
| V 优雅降级/规模诚实 | 联想信号无边表/无 embedding 时跳过；时间信号无 event 范围时跳过；均回落到现三路 RRF；规模边界如实：~10 万条，BEAM 非目标 | ✅ |
| 技术约束 | 无 CGO、零新依赖、模型侧走既有接口、单一存储真相 | ✅ |

**Post-Phase-1 re-check**: 通过——data-model 仅增量迁移；contracts 未破坏既有 Go API
（只加方法/可选项）；bench flag 均默认关闭新机制（FR-011 可独立开关）。

## Project Structure

### Documentation (this feature)

```text
specs/003-bio-retrieval-locomo/
├── plan.md              # 本文件
├── research.md          # Phase 0：机制选型决策（已定稿）
├── data-model.md        # Phase 1：migration v3 与实体关系
├── quickstart.md        # Phase 1：每枪的运行/算账/判定流程
├── contracts/
│   ├── engine-api.md    # 引擎新增 Go API 契约
│   └── bench-cli.md     # bench 新增 flag/报告格式/费用账契约
└── tasks.md             # Phase 2（/speckit-tasks 生成，非本命令产出）
```

### Source Code (repository root)

```text
store/
└── migrations.go        # +Migration v3（边表/别名表/时间范围列/superseded_by）

memory/
├── retriever.go         # +associativeRanks（游走信号）+temporalScore（时间信号）
│                        #  fuseRRF 扩展为可变多路；superseded 降权/过滤
├── entrystore.go        # +Entry 字段（EventStart/End、SupersededBy）+边表/别名访问器
├── entities.go          # +query-to-entity 全句匹配路径
├── graph.go             # 新增：共现边构建、IDF 种子权重、质心游走 depth-2
├── temporal.go          # 新增：规则式时间意图解析（会话时间戳锚定）+T_score
├── pipeline/pipeline.go # 抽取落库时写边表/别名/时间范围
├── prompt/              # 抽取 prompt 扩展（别名+时间范围字段）；curation 四分类 prompt
└── curation/
    ├── judge.go         # keep/evict/merge → +Compatible/Contradictory/Subsumes/Subsumed
    └── worker.go        # apply 增加非破坏压制（写 superseded_by）

cmd/locomo-bench/
├── main.go              # +--repeats/--estimate/--price-*/机制开关 flag
├── stats.go             # 新增：均值±95%CI、配对 diff（逐题翻转+显著性）
├── cost.go              # 新增：token 用量捕获、预估/实际费用账
├── longmemeval.go       # 新增：LongMemEval_S 加载器与题型→类别映射
└── runner.go            # 答题 prompt 换 Abstain-R1 1:4 ICL 契约（独立提交）
```

**Structure Decision**: 沿用既有单 module 布局——引擎改动落 `memory/`+`store/`，
评测改动落 `cmd/locomo-bench/`，二者通过引擎公开 API 交互（宪法 II）。新文件仅 4 个
（graph.go、temporal.go、stats.go、cost.go、longmemeval.go），其余为最小增量修改。

## Complexity Tracking

> 无宪法违背项，无需豁免。

（备忘一项设计取舍，非违背：`fuseRRF` 从固定三路扩为可变多路签名属引擎内部函数，
不动公开 API；`Search` 公开签名不变，新信号经 `Retriever` 内部选项启用。）
