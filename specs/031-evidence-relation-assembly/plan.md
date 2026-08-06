# Implementation Plan: 读侧证据关联装配（Evidence Relation Assembly）

**Branch**: `031-evidence-relation-assembly` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/031-evidence-relation-assembly/spec.md`

## Summary

在 030 读侧证据装配（chunk-first + cap 截断 + 类别条件排序 + trace 引用链）已收口的地基上，031 只增加一个读侧增量：**在装配好的证据上计算并注入证据间显式关系（structural context）**——共享实体的 `related_to`、时间邻近/后继的 `temporal_next`、因果指示词的 `caused_by`，按问题类别组织成结构上下文块喂给 answerer（multi-hop 按链、temporal 按时序）。实证锚点：MemCog（`2605.28046`，alphaXiv 核实）w/o Graph Overlay **↓6.79pp**、w/o Hierarchy **↓6.53pp**。纯 Go、离线、确定性、默认关；配对评测（008 铁律）为唯一 GO 门。

**技术路线**：关系计算是 harness 内新增的确定性纯 Go 阶段，**不修改 engine、不写侧重构、不引入 agent 决策**。输入是 030 `assembleEvidence` 已产出的有序证据（`memory.Result`），输出是一个结构上下文块，注入 answerer 用户 prompt。证据间关系在候选集内临时计算，不改 store 内容表示。

## Technical Context

**Language/Version**: Go 1.25.0，`CGO_ENABLED=0`（纯 Go 硬门）

**Primary Dependencies**: 无新第三方依赖——只用标准库（`regexp` / `sort` / `slices` / `time`）；复用 `cmd/locomo-bench` 已有装配器（`assembly.go` / `trace_gate.go`）与 `memory.Result` 结构。

**Storage**: 只读。关系计算输入来自 `memory.Result`：
- `Content`（文本）→ 实体提取（harness 内确定性正则：title-case / quoted 短语，复用 029 `nav_diagnose_cli.go` 的 `extractEntitiesFromHits` 模式）与因果指示词匹配
- `EventDate *time.Time` → `temporal_next`（`assemblyDateRank` 已有日期键，直接复用）
- store 的 `memory_entities` 表已存在（schema v2），但 **不作为必需依赖**——031 保持 harness 内自算，避免触碰 engine API 面（FR-008 零改动更稳）。

**Testing**: 
- 离线单测：`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`（确定性、空关系 fail-soft、parity 门、token 记账）
- 配对评测：`cmd/locomo-bench --relation-context`（84 题子集 × 3 reps majority ≥ 030 基线 → 全量 1540 复跑）

**Target Platform**: Linux（WSL2 编译 → AutoDL 云端评测）；引擎/适配层不受影响。

**Project Type**: Go 评测 harness 增量（`cmd/locomo-bench`），非 engine、非 adapter。

**Performance Goals**: 关系计算在候选证据集内（cap 内 ≤ 数十 units）最坏 O(n²) 可接受（每次问答一次）；**不增加 answerer token 预算**——结构上下文块计入 030 现有 exact-token 记账，超 cap 时按证据优先级截断（结构上下文是证据的附加元数据，随被截断证据一起消失）。

**Constraints**:
- **引擎零改动（FR-008 硬门）**：`git diff --name-only -- memory embedding provider store internal` 必须为空
- **默认关 + SC-004 parity**：`--relation-context` 默认 false；关闭时现有装配路径逐字节不变
- **token 预算纪律**：不大力出奇迹——结构上下文块本身计入 cap，关系边有容量上限
- **设计红线**：不 agent 导航（029 教训）、不写侧重构（027/028/014）、不实体图遍历（014）、不用付费云 rerank（DEATH RULE）

**Scale/Scope**: 单候选证据集（单次问答），关系计算本地化，无跨会话持久结构。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 结论 |
|---|---|---|
| **I 本地优先，默认离线** | 关系计算纯 Go、确定性、零模型；`--relation-context` 默认关 | ✅ 无违规 |
| **II 引擎与适配层分离** | 只动 `cmd/locomo-bench`；FR-008 diff 门保证 engine 零改动 | ✅ 无违规 |
| **III 契约优先与命名空间隔离** | 契约先在 `contracts/evidence-relations.md` 定死（输入/输出/错误语义/parity） | ✅ 无违规 |
| **IV 评测回归门禁** | 008 铁律配对：同 store/子集/answerer/judge/cap，majority ≥ 030 基线，类别不回归（L0-3） | ✅ 无违规 |
| **V 优雅降级与规模诚实** | 关系为空/失败 → fail-soft 走 030 路径（绝不劣化上下文）；规模诚实（本地化单次计算） | ✅ 无违规 |

全部五项通过，无 Complexity Tracking 需记录的违规。

## Project Structure

### Documentation (this feature)

```text
specs/031-evidence-relation-assembly/
├── plan.md              # 本文件
├── research.md          # Phase 0：决策（实体提取/因果词典/关系 cap/叠加顺序）
├── data-model.md        # Phase 1：EvidenceUnit / RelationEdge / StructuralContextBlock
├── quickstart.md        # Phase 1：验证场景（离线单测 + 配对命令）
├── contracts/           # Phase 1：evidence-relations.md（关系计算契约）
├── checklists/requirements.md
└── tasks.md             # Phase 2（/speckit-tasks 产出）
```

### Source Code (cmd/locomo-bench/)

```text
cmd/locomo-bench/
├── relation_graph.go       # 【新】关系计算 + 结构上下文块渲染（纯 Go 确定性）
├── relation_graph_test.go  # 【新】离线单测：确定性 / 三类关系 / 空关系 fail-soft / parity
├── main.go                 # 【改】注册 --relation-context flag + 装配流程接线
├── assembly.go             # 【改，最小】装配后注入结构上下文块（或独立 render 入口，off 时 byte-parity 不变）
└── trace_mediation.go      # 【改，可选】trace 叠加时结构上下文的引用门校验（复用 trace_gate）
```

**Structure Decision**: 单项目结构——feature 完全落在 `cmd/locomo-bench/`（评测 harness），与 030 一致。新增 `relation_graph.go` 一个核心文件 + 配套测试；`assembly.go`/`main.go` 只做最小接线改动（新增 flag、装配后注入点）。不新建包，不碰 engine。

## Complexity Tracking

> 无违规（Constitution Check 全部通过），本节留空。
