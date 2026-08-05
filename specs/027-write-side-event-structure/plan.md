# Implementation Plan: 写入侧事件时序结构记忆

**Branch**: `027-write-side-event-structure` | **Date**: 2026-08-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/027-write-side-event-structure/spec.md`

## Summary

021 已证伪 6 次 temporal 检索/答题杠杆，收敛出「temporal 真差距在写入侧结构化记忆」。
alphaXiv 定向调研（复盘 doc 第 6 节）找到两份干净固定变量消融：SEGTREEMEM（temporal
有序 segment tree，置换 30% turn 对掉 0.111 vs 非时序 0.020）与 StructMem（event 双视角
抽取 + 周期性跨事件合并，temporal 81.62 vs flat 78.50 vs 实体图 76.64）。两篇共同且与
engram 既有证据（Mem0g multi-hop 有害、014/021）对齐：**post-hoc 实体图不涨点，写入侧
event-centric 时序层级才涨点**。027 把这一结论落成可验证机制：**写入侧 event 投影**——
本地 sidecar 对原始对话做事件双视角抽取（事实 + 关系）+ 时间锚定，周期性合并为跨事件
关系摘要；检索侧把事件 + 摘要带入 answerer（阶段 1）；时间有序 segment tree 为阶段 2/3
可选增量。在冻结协议下与 chunk 基线配对消融，验证同预算下 temporal/multi-hop 端到端
转化。**008 铁律：端到端转化才算 GO，覆盖率/结构完整性不算。**

## Technical Context

**Language/Version**: Go 1.25.0（CGO_ENABLED=0 硬门）

**Primary Dependencies**: `modernc.org/sqlite`（已有）、`memory/pipeline`（022 已交付 fact 抽取）、`provider/`（LLM 抽象，event 抽取走本地 sidecar）、`cmd/locomo-bench/representation_eval.go`（025 已落地三渲染器 shared-anchor 设计）、`cmd/locomo-bench` 的 `--only-questions` formal 子集模式（023 已交付）

**Storage**: 复用 022 Evidence Ledger SQLite schema（append-only 原文不变）；新增 event 投影（**可丢弃可重建**，config-hash 幂等，如 022 fact 投影），不新增 schema 除非 relation-summary 需要（见 research.md D2）

**Testing**: `CGO_ENABLED=0 go test -count=1`（引擎离线测试，LLM 端点 stub）；event 抽取用固定 fixture byte-replay（合法/非法 JSON、缺字段、幻觉样本）；配对验证走 `cmd/locomo-bench` formal protocol

**Target Platform**: Linux（WSL2 开发）/ AutoDL GPU 评测箱（远程 eval）

**Project Type**: Go library（引擎侧 event 投影增量）+ eval harness（`cmd/locomo-bench` 表示渲染器增量）

**Performance Goals**: event 抽取为 LLM sidecar 调用（记录每题成本，FR-034 类约束不适用本 feature 但 token 记账必报）；检索/渲染路径 deterministic 纯 Go；单题编译 <100ms

**Constraints**: 默认关（FR-004/010）；无 LLM 端点退回 chunk 路径（FR-005，fail-closed 零行为变化）；禁止付费云模型/reranker（DEATH RULE）；配对必须同 store + 候选逐字节一致（025 纪律）；token cap 不变量（FR-008）

**Scale/Scope**: 单用户 ~100k entry；LoCoMo 1,540 answerable + LongMemEval-S 500

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| I. Local-first、offline 默认 | PASS | event 抽取走本地 sidecar（Ollama/vllm，可替换）；无 LLM 时退回 chunk 路径零行为变化；机制默认关 |
| II. Engine/adapter 分离 | PASS | event 投影作为引擎侧公开能力（类似 022 fact 投影），引擎独立可单测（LLM 端点 stub）；harness 侧只做表示渲染 + 配对验证 |
| III. Contract-first、namespace 隔离 | PASS | event 投影契约（Event/RelationEntry 结构、抽取 prompt、fail-closed 校验）在 plan 定稿后再实现；不新增跨 namespace 访问 |
| IV. 评测回归门 | **PENDING** | 配对消融（chunk 基线 vs event 表示）必须在 comparable slice 确认 temporal/multi-hop 增益且 overall 不回归，否则不得 merge（027 核心验证） |
| V. 优雅降级 & 诚实规模 | PASS | 无 LLM/embedding 时退 chunk 路径可跑；事件有界（每 event 证据数上限）；不承诺超 100k entry 能力 |

## Project Structure

### Documentation (this feature)

```text
specs/027-write-side-event-structure/
├── spec.md                # feature spec
├── plan.md                # this file
├── research.md            # Phase 0: 决策（模型选型/投影位置/时间锚定/fail-closed/基线）
├── data-model.md          # Phase 1: 027 增量数据（Event/RelationEntry/RelationSummary）
├── quickstart.md          # Phase 1: 阶段 0 诊断 + 阶段 1 先导跑法
├── contracts/             # Phase 1: 027 增量契约（Event 数据契约 + 抽取 prompt + fail-closed 校验）
└── tasks.md               # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
# 引擎侧（027 增量：写入侧 event 投影，可重建投影、LLM 端点可替换）
memory/eventstore/
├── event.go               # Event（事实视角 + 关系视角 + 时间锚定）/ RelationEntry / EventStore（可重建投影）
├── extract.go             # 双视角抽取：调用 provider.LLM + schema 校验（fail-closed 退回原文）
├── extract_test.go        # fixture byte-replay：合法/非法 JSON、缺字段、幻觉样本、fail-closed
├── consolidate.go         # 周期性跨事件合并（RelationSummary，StructMem 式 synthesis）
└── consolidate_test.go    # 合并触发条件、有界、config-hash 幂等

# harness 侧（表示渲染 + 配对验证）
cmd/locomo-bench/
├── representation_eval.go # [027] 新增 event 渲染器（复用 025 的 shared-anchor 三渲染器设计）
├── eventstore_eval.go     # [027] event store 构建 + 配对跑法（--only-questions 子集 + 全量）
└── eventstore_eval_test.go# [027] event 渲染器 byte-replay + fail-closed + 配对集成测试
```

**Structure Decision**: 引擎侧新增 `memory/eventstore/` 作为**可重建投影**（如 022 fact 投影、025 EpisodeStore 的语义），LLM 依赖走 `provider/` 抽象（本地 sidecar 可替换）；harness 侧复用 025 的 shared-anchor 表示渲染器设计，只增加 event 渲染器与配对跑法。segment tree（阶段 2/3）先不在 Project Structure 中承诺，仅当阶段 1 GO 后作为增量评估。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| 无 | 事件投影作为独立 `memory/eventstore/` 包（类似 022 fact 投影）是引擎 feature 的合理边界；harness 只做渲染与验证，不重写引擎 | 塞进 `memory/pipeline` 会耦合 fact 抽取；放 harness 侧会违反引擎/适配分离（宪法 II） |

## Phases

### Phase 0: 决策与研究方向

见 research.md。核心决策:
- **D1**: event 抽取模型选型（本地 sidecar：7B 先试格式合法性，fail-closed 兜底；质量不足升 35B）
- **D2**: event 投影落位（引擎侧 `memory/eventstore/` 可重建投影 vs harness 侧）——relation-summary 是否需新 schema
- **D3**: 时间锚定方案（绝对时间解析 + 相对引用保留，参考 013 解析器点火率教训）
- **D4**: fail-closed 校验机制（schema 校验 + 退回原文 chunk）
- **D5**: 配对基线（LoCoMo 85.19% B1 已收口，可直接引用；先导用 `--only-questions` residual 子集）

### Phase 1: 设计与契约

- contracts/:Event 数据契约（事实/关系双视角字段 + 时间锚定）+ 抽取 prompt 契约 + fail-closed 校验契约（输入/输出形状）
- data-model.md: 027 新增实体（Event/RelationEntry/RelationSummary），复用 022 Evidence Ledger，不破坏 append-only
- quickstart.md: 阶段 0 诊断跑法（gold 在不在池人审）+ 阶段 1 先导跑法（`--only-questions` 子集配对）

### Phase 2: 实现计划（见 tasks.md，经 /speckit-tasks 生成）

**MVP**（阶段 1 先导）: 引擎侧 event 抽取 + fail-closed + 可重建投影；harness 侧 event 渲染器；`--only-questions` 子集配对（temporal + multi-hop residual 题）验证端到端转化。
**完整**（阶段 2）: LoCoMo 1540 + LongMemEval-S 500 全量配对；分类别报告；verdict 收口（GO → 讨论默认路径 / NO-GO → 记录负结果）。
**可选**（阶段 3）: segment tree 构建 + 沿树传播检索（仅阶段 2 GO 后）。

## Key Technical Decisions

1. **承接而非重写**：event 抽取复用 `provider/` LLM 抽象与 022 Evidence Ledger；表示渲染复用 025 的 shared-anchor 设计（同一 ranked anchor，只变渲染器 → 配对归因干净）。**不实现** post-hoc 实体图（014/021/Mem0g 证伪）、检索侧杠杆（021 穷尽）、LLM 融合写回（022 证伪）。
2. **双视角抽取 + 时间锚定（StructMem 式）**：事实视角（发生了什么）+ 关系视角（谁和谁/因果/共同参与），每条钉时间戳。这比 023 的 planner proposal 简单得多（固定字段输出 vs 开放动作规划），但仍 MUST fail-closed（schema 校验失败 → 退回存原文 chunk，不污染 store）。
3. **配对纪律（025 教训）**：两臂同一 store 候选逐字节一致、同一 answerer/judge/token cap，只差写入侧表示；报告 candidate oracle 区分「gold 在池但打包丢上下文」vs「gold 不在池」；小 delta 不单独作 promotion 依据（answerer temp=1.0 需多次重复 + 配对统计）。
4. **阶段化门禁（不盲烧，复盘纪律）**：阶段 0 诊断（零成本确诊 gold 在不在池，不在则 STOP）→ 阶段 1 先导（`--only-questions` 子集，~35 分钟）→ 阶段 2 全量配对 → 阶段 3 segment tree（可选）。008 铁律下任一阶段端到端不转化即 STOP。
5. **时间锚定防坑（013 教训）**：不依赖高点火率的时间解析器。事件条目保留原始相对引用（"去年""下周三"）+ 尝试解析绝对时间，解析失败不丢弃条目、退回保留引用，让 answerer 在合并摘要中推理。
