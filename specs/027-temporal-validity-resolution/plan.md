# Implementation Plan: 查询时时间有效性解析

**Branch**: `027-temporal-validity-resolution` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/027-temporal-validity-resolution/spec.md`

## Summary

在 022 冻结候选 + 固定 answerer 下，验证"查询时时间有效性解析"是否补上 engram 的 temporal
短板。机制：**固定候选池内、查询时、确定性**组织已命中证据的时间结构——当前值解析（选最新
valid）、演化链组装（按 OccurredAt 全序）、时间窗约束（按 query 显式时间过滤），输出进
answer bundle（cap 内）。与 013/014/017/024 已证伪的 temporal 机制（检索侧重排/过滤、写侧
supersede 惩罚）是**不同的查侧组装面**。接入方式复刻 026：新 mechanism flag →
`mechanism_flags{temporal_resolution:true}` 加性进 formal B1，同 store 候选逐字节一致配对消融。

## Technical Context

**Language/Version**: Go 1.25.0，无 CGO（硬门禁）。增量全在 `cmd/locomo-bench/`（harness
侧 adapter），`memory/` 引擎零改动。

**Primary Dependencies**:
- `memory/evidencecompiler`（引擎，**只读消费**：`Source.OccurredAt`、`EvidenceNeed.TimeConstraints`）
- `cmd/locomo-bench`（026 已验证的 mechanism-flag 接线：`densityMechanismFlagsForOptions`、
  `mergeMechanismFlags`、`formalTreatmentFreeze`、`compileFormalSources`）
- 已有 SQLite（`modernc.org/sqlite`）—— 只读 `memory_evidence.occurred_at` / `memory_evidence_events`（V1 可选）

**Storage**: SQLite（022 现有）。时间来源 = `evidencecompiler.Source.OccurredAt`（harness 构建
source 时已可访问）。**不新增写侧 schema**（migration 历史里 event_start/superseded_by/revision
曾被 ADD 又 DROP，014 已证伪写侧时间 schema 路线）。

**Testing**: `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/`（解析器确定性/退化/候选一致
单测）+ formal B1 paired eval（LoCoMo 1,540 + LongMemEval-S 500，repeats≥3，分类别配对统计）。

**Target Platform**: Linux（AutoDL 评测箱或本地离线），离线可运行。

**Project Type**: benchmark harness adapter（研究实验，默认关；非产品功能）。

**Performance Goals**: 解析器纯 Go 确定性、**零额外 LLM 调用**；配对 run 与 026 同量级
（每臂 ~10-30 分钟，依赖 answerer/judge 端点）。

**Constraints**:
- 引擎零改动（`git diff --name-only -- memory embedding provider store internal` 必须为空）
- 默认关（`--temporal-resolution` 未设时行为与 chunk_900 基线逐字节一致）
- 候选逐字节一致（三臂共享 `compileFormalSources` 同一 flat source list）
- cap 沿用 022 当前冻结值；超 cap fail-closed，不提高 cap 挽救分数
- 纯离线确定性；不依赖付费云 reranker/recall/Planner/answerer（DEATH RULE）

**Scale/Scope**: LoCoMo 1,540 answerable + LongMemEval-S 500 全量配对。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 结论 |
|---|---|---|
| I 本地优先/默认离线 | 解析器纯 Go 确定性，无 LLM/云；默认关；无端点时可离线运行 | ✅ 通过 |
| II 引擎/适配层分离 | 增量限定 `cmd/locomo-bench/`；`memory/evidencecompiler` 只读；引擎公开入口不足时走显式 contract increment | ✅ 通过 |
| III 契约优先 | spec 已定义 resolution 契约；mechanism flag 走 mechanism-bindings 加性模式（不破坏 022 protocol） | ✅ 通过 |
| IV 评测回归门禁 | 全量 paired（同 store、候选逐字节一致、repeats≥3、分类别配对统计）；无回归才可进默认路径 | ✅ 通过 |
| V 优雅降级/诚实规模 | 默认关 = 基线行为；负结果记录不进入默认路径；APEX-MEM 跨栈证据不外推 | ✅ 通过 |

*GATE 通过。无违宪项，Complexity Tracking 留空。*

## Project Structure

### Documentation (this feature)

```text
specs/027-temporal-validity-resolution/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output —— D1–D5 决策 + 文献 + 代码资产盘点
├── data-model.md        # Phase 1 output —— 解析输入/输出结构
├── quickstart.md        # Phase 1 output —— 配对 runbook
├── contracts/           # Phase 1 output —— temporal-resolution-binding.md
├── checklists/
│   └── requirements.md  # spec 质量清单
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── eval_runner.go               # 已存在：mechanism flag 解析 + formalTreatmentFreeze（026 已验证模式）
├── eval_compile_bridge.go       # 已存在：compileFormalSources 构建候选/source（027 解析器前置）
├── temporal_resolution.go       # [027] 解析器：当前值/演化链/时间窗 + 确定性 supersede 判定（NEW）
├── temporal_resolution_test.go  # [027] 确定性/退化/候选一致/时间窗单测（NEW，测试先行）
├── eval_compile_bridge.go       # [027] 解析器接入点：在 bundle 组装前对 source 做时间组织（改）
└── main.go                      # [027] --temporal-resolution flag（改，复用 mechanism 接线）
```

**Structure Decision**: 027 增量限定在 `cmd/locomo-bench/`（harness adapter），新增
`temporal_resolution.go`（解析器，纯 Go 确定性）与对应测试；接线复用 026 已验证的
mechanism-flag → `mechanism_flags{temporal_resolution:true}` → formal B1 协议 hash 归因路径。
引擎目录（`memory/ embedding/ provider/ store/ internal/`）零改动。解析器只读消费
`evidencecompiler.Source.OccurredAt`（通过 `compileFormalSources` 构建的 source list）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违宪项。027 是单 harness 文件 + 接线的最小研究实验，不引入图存储、多工具 agent 或写侧
schema（APEX-MEM 的 GraphSQL 是多工具 agent 方案，V1 用纯规则替代，避免 026 的负结果教训
与 token 成本）。
