# Implementation Plan: 离线固结 · 跨 session 桥接合成

**Branch**: `015-consolidation-bridging` | **Date**: 2026-07-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/015-consolidation-bridging/spec.md`

**上游事实源**: [归档设计](../../docs/archive/designs/2026-07-25-offline-consolidation-bridging-design.md)（brainstorming 逐段确认）

## Summary

在「在线抽取」与「策展减法」之间新增引擎的第三个写入阶段：一趟离线后台 pass，
跨 session 枚举共享稀有实体的证据对，由语言模型合成一条新的桥接事实并作为**同构
的普通记忆**落库。目标是把需要多跳才能拼出的答案，预先变成一跳即可命中的事实，
攻 LoCoMo multi-hop。

技术路径：纯 Go 确定性候选枚举（跨 session + 共享实体 + IDF 剪枝 + top-K）→ 模型
合成（**带拒绝权**）→ 严格校验 → 走既有正规写入路径落库 → `memory_bridges` 记
血缘（`pair_key` 唯一索引提供幂等）。作业形态照 `curation.Worker` 的
lease/heartbeat/inert 模子，使未来多租户化零改造。

**执行纪律**：必须先过两级免费证伪门（门 0 零模型、门 1 不调回答模型），任一不过
即判死，**在此之前不写任何引擎代码**。

## Technical Context

**Language/Version**: Go 1.25.0

**Primary Dependencies**: 标准库 + `modernc.org/sqlite`（纯 Go）。**本特性不引入
任何新的第三方依赖**。

**Storage**: SQLite（`modernc.org/sqlite`），单 `*sql.DB`、`SetMaxOpenConns(1)`
单写连接、WAL、FTS5 trigram。新增 migration **v6**（`memory_bridges` 表）。

**Testing**: `go test`，全部离线。模型侧用 stub 闭包（`ModelCaller` 是函数类型），
向量侧可为 nil。无网络、无子进程。

**Target Platform**: 跨平台纯 Go 库（**CGO_ENABLED=0 硬门禁**）。

**Project Type**: 单体 Go 库（引擎）+ 薄适配层。本特性**只动引擎**，适配层零改动。

**Performance Goals**: 固结是离线后台作业，不在请求热路径上，**无延迟目标**。
唯一的量化约束是单趟有界：`TopKPerPass`（初始 2000）与 `MaxBucketSize`。

**Constraints**:
- 离线可运行：未配置模型 ⇒ 完全 inert（宪法 I）
- 检索侧零改动（FR-016）
- ADD-only：绝不改写/删除源记忆（FR-015）
- 幂等 + 可恢复（FR-019/FR-024）
- 固结与策展互斥（共用单例 lease，research R2）

**Scale/Scope**: 沿用既有诚实规模声明——单用户 ~10 万条级记忆。候选枚举是一次
全表扫描 + 内存分桶，在该量级下可接受；**不承诺**更大规模，超边界属未来工作。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 门禁判定 | 依据 |
|---|---|---|
| **I. 本地优先，默认离线** | ✅ PASS | `call == nil` ⇒ worker 完全 inert，固结不运行且无副作用（FR-020）。`embedder == nil` ⇒ 产物仍落库、仅缺语义向量。核心读写与检索路径**不依赖**固结 |
| **II. 引擎与适配层分离** | ✅ PASS | 纯引擎增量。`mcpserver/` 零改动，不新增 MCP 工具。新包 `memory/consolidation/` 不依赖任何宿主类型 |
| **III. 契约优先与命名空间隔离** | ✅ PASS | 契约先冻结于 [contracts/consolidation-api.md](./contracts/consolidation-api.md)。migration v6 **新增不改旧**；产物同构 ⇒ 检索契约不变。namespace 隔离是适配层的一 ns 一 store，引擎无感，不受影响 |
| **IV. 评测回归门禁（不可协商）** | ✅ PASS（有强化） | 本特性动写入路径 ⇒ 合并前**必须**全量 LoCoMo 可比口径对比：multi-hop 提升 + 整体不低于基线。eval 配置改动与算法改动**分开提交**。此外本特性自带**前置**证伪门（门 0/门 1），比宪法要求更严 |
| **V. 优雅降级与规模诚实** | ✅ PASS | 单对失败 ⇒ WARN + 跳过 + 整趟继续，绝不影响既有检索/写入（FR-025）。模型缺失/向量缺失均逐信号降级。规模沿用既有 ~10 万条级声明，不作新承诺 |

**技术约束核对**：无 CGO ✅（不引入任何依赖）；依赖最小化 ✅（零新依赖）；
可替换模型侧 ✅（`ModelCaller` 函数类型，不绑定 provider）；单一存储真相 ✅
（复用同一 SQLite，不建平行副本）。

**Post-Phase-1 复查**: 设计产出（data-model / contracts / quickstart）后重新核对
上表，**结论不变，无新增违规**。Complexity Tracking 为空。

## Project Structure

### Documentation (this feature)

```text
specs/015-consolidation-bridging/
├── plan.md              # 本文件
├── spec.md              # 需求（25 FR / 9 SC / 3 用户故事）
├── research.md          # Phase 0：R1–R7，含两处对设计文档的事实修正
├── data-model.md        # Phase 1：v6 schema + 内存态实体 + 状态流转
├── quickstart.md        # Phase 1：P1→P2→P3 执行手册
├── contracts/
│   └── consolidation-api.md   # Phase 1：包公开 API 契约 + 零改动承诺
├── checklists/
│   └── requirements.md  # spec 质量清单（已通过）
└── tasks.md             # Phase 2 输出（由 speckit-tasks 生成，非本命令）
```

### Source Code (repository root)

```text
memory/
├── consolidation/                 # 【新增】与 curation/ 平级
│   ├── candidates.go              #   纯确定性候选枚举（零模型）
│   ├── candidates_test.go
│   ├── verdict.go                 #   ParseVerdict / ValidateVerdict
│   ├── verdict_test.go
│   ├── worker.go                  #   RunPass / Notify / Start，复用 curation.Lease
│   └── worker_test.go
├── prompt/
│   └── consolidation.go           # 【新增】固结提示词（含拒绝权 + 源回述）
├── curation/                      # 【不改】仅被 import 取用 Lease
├── entrystore.go                  # 【不改】
├── entities.go / graph.go         # 【不改】
├── embedder.go                    # 【不改】
└── retriever.go                   # 【不改】检索侧零改动

store/
└── migrations.go                  # 【仅追加】v6ConsolidationBridges

mcpserver/                         # 【不改】不新增 MCP 工具
cmd/locomo-bench/                  # 【不改】证伪门复用已有 --coverage-only
```

**Structure Decision**: 新增单一引擎包 `memory/consolidation/`，与
`memory/curation/` 平级。选此结构的理由：curation 是**减法**（删/合并/抑制），
consolidation 是**加法**（合成新记忆），两者的水位线、判据、失败语义与回滚方式
完全不同，合并进一个包会让职责纠缠、难以独立测试。既有文件的改动面被压到最小
——`store/migrations.go` 追加一段常量，**其余引擎文件一行不动**。

## Phase 0 摘要（详见 research.md）

调研发现**两处设计文档与代码现状不符**，均已按事实修正并回写设计文档：

| 编号 | 发现 | 处置 |
|---|---|---|
| **R1** | 设计文档写「v3 migration」，但 v3/v4/v5 已分别被 003/013/012 占用 | 改为 **v6**；设计文档已更正 |
| **R2** | `memory_curation_lease` 是 `id=1` 的**单例行锁**，复用它 ⇒ 固结与策展**必然互斥** | 接受并明确记录——单写连接下串行本就是正确语义；设计文档已补注 |
| R3 | 落库序列 = `Upsert → PutEntities → UpsertEdges → embedder.Enqueue` | 照抄 pipeline 既有路径 |
| R4 | 设计文档未规定产物实体来源 | 裁定取两源实体**并集**（确定性、零额外模型输出），已标注为 plan 阶段裁定 |
| R5 | 候选枚举需全量倒排，现有公开 API 拼不出 | worker 持 `*sql.DB` 自查（照 `curation.Worker` 先例），**不扩大引擎公开契约** |
| R6 | 证伪门的执行载体 | 一次性脚本，**不进代码库**；复用已有 `--coverage-only`、`Evidence`、`chunkTurns` |
| R7 | `ModelCaller` 签名 | 沿用引擎既有同形函数类型，不复用 curation 的类型本身（避免契约耦合） |

**未解决项**：无。

## Complexity Tracking

> 仅在 Constitution Check 有需要证成的违规时填写。

**本特性无宪法违规，本节为空。**
