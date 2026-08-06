# Implementation Plan: Agentic 多步记忆导航

**Branch**: `029-agentic-memory-navigation` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/029-agentic-memory-navigation/spec.md`

## Summary

在 028 收口（写侧 event 结构三次证伪）+ 021 收口（检索侧单次表示/排序六次证伪）之后，本 feature 验证唯一未探索的检索范式：**推理驱动多步导航**——本地强模型（现有 35B answerer）在检索中多步决策（换查询 / 跟线索 / 深挖 / 停止），让推理救回单次 top-k 漏掉的证据。参考 MemCog（LoCoMo 92.98 / LongMemEval 95.80）与 NapMem（记忆金字塔 + RL 导航）。**唯一 GO 门：008 铁律（多步 majority ≥ 单次基线，同预算）**。四个门禁化阶段：US1 零成本诊断（救回空间）→ US2 最小先导（推理驱动多步检索，84 题配对）→ US3 结构化层级导航（可选）→ US4 RL 导航策略（SaaS）。

## Technical Context

**Language/Version**: Go 1.25（引擎 + locomo-bench harness；导航编排在 `cmd/locomo-bench`）。评测/分析脚本 Python 3。

**Primary Dependencies**: 复用现有评测栈——`provider.Provider`（35B sidecar OpenAI-compatible）、`Retriever.Search`（混合检索）、embedding 8010、judge（DeepSeek）；复用 `cmd/locomo-bench/local_planner.go` 的「provider 调用 + JSON 结构化输出 + timeout + fail-closed」模式。**无新引擎依赖、不引新第三方库**。

**Storage**: 现有 chunk store（`009-bge-chunks-store`）+ event 投影基建不动。导航不新建存储（US3 层级投影为门禁后的可选增量）。

**Testing**: Go 单测（导航状态机 / 工具 JSON 解析 / fail-closed 回退）+ 配对纪律（84 题 × 3 reps majority + McNemar + 008 铁律 + 类别不回归）。引擎测试离线；导航测试用 stub provider。

**Target Platform**: Linux eval box（WSL2 编译 → 本地 / AutoDL 跑），Blackwell env（answerer 8000 + embed 8010）。

**Project Type**: 评测 harness 扩展（研究实验，默认关，产出 verdict）。

**Performance Goals**: 导航步数硬上限（默认 4 步，可配）；每步 LLM 调用 timeout（默认 6s，复用 planner timeout）；配对近零成本（本地 35B + DeepSeek judge，测试规模 token 可控）。

**Constraints**: 引擎 `memory/ embedding/ provider/ store/ internal/` **MUST NOT 修改**（导航全在 cmd/ + 评测层）；**预算纪律**——多步导航是「推理救回证据」不是「扩大召回」，最终喂给 answerer 的证据 MUST 在既有 answer-context 预算内，导航消耗单独记账；offline 可跑（无强模型/导航失败 → 退回单次检索，宪法 I/V）。

**Scale/Scope**: 84 题先导（temporal 59 + multi-hop 25）→ 门禁后全量（LoCoMo 1540 + LongMemEval-S 500）。US3/US4 门禁驱动。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 通过 | 依据 |
|---|---|---|
| **I 本地优先，默认离线** | ✅ | 导航 agent 复用本地 35B sidecar；无导航/失败时退回单次检索（现有本地路径），offline 完整可跑 |
| **II 引擎/适配分离** | ✅ | 导航编排全部在 `cmd/locomo-bench`（评测 harness，非引擎）；`git diff --name-only -- memory embedding provider store internal` 必须为空（FR-003 硬门） |
| **III 契约优先与命名空间隔离** | ✅ | 导航工具接口、导航轨迹 schema、配对协议在 contracts/ 先冻结再实现；评测不触碰命名空间隔离 |
| **IV 评测回归门禁** | ✅ | 008 铁律唯一 GO 门 + 84 题配对 + 类别不回归（L0-3）；评测配置改动与算法改动分开 commit |
| **V 优雅降级与规模诚实** | ✅ | 导航超步数/失败/无模型 → fail-closed 退回单次检索；规模/预算如实声明，不承诺超预算转化 |

GATE 结果：**全部通过，无 violation**。Complexity Tracking 留空。

## Project Structure

### Documentation (this feature)

```text
specs/029-agentic-memory-navigation/
├── plan.md              # This file
├── research.md          # Phase 0 (决策记录)
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/           # Phase 1 (导航工具/轨迹/配对协议)
├── checklists/
│   └── requirements.md  # spec 质量清单（已过）
└── tasks.md             # Phase 2 (/speckit-tasks 生成)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── agentic_nav.go       # 导航 agent 编排（状态机：search→assess→decide→stop）+ --nav flag
├── agentic_nav_test.go  # 状态机/工具解析/fail-closed 单测（stub provider）
├── nav_tools.go         # 导航工具集（search / expand_query / follow_entity / stop）
├── nav_tools_test.go
├── nav_trajectory.go    # 导航轨迹结构 + JSON 序列化（data-model/contracts 对齐）
└── nav_trajectory_test.go
└── local_planner.go     # 既有（复用其 provider/JSON/timeout 模式，不改）

specs/029-agentic-memory-navigation/tools/
├── nav_diagnose.py      # US1 零成本诊断（单次 top-k vs oracle vs 模拟多步三分类）
├── nav_analyze.py       # US2 配对分析（majority + McNemar + 步数/token 记账）
└── nav_project.py       # US3 层级投影构建（门禁后）
```

**Structure Decision**: 导航编排放在 `cmd/locomo-bench`（Go），遵循 `local_planner.go` 已确立的模式（provider 调用 + JSON 结构化输出 + timeout + fail-closed）。**引擎零改动**（FR-003）。诊断/分析脚本放 `specs/029/tools/`（Python，复用 028 的工具目录模式）。US3/US4 门禁驱动，不预建。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无 violation，此表留空。
