# Tasks: Agentic 多步记忆导航

**Input**: Design documents from `/specs/029-agentic-memory-navigation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Organization**: Tasks grouped by user story（US1 诊断 → US2 先导 → US3 结构 → US4 SaaS，门禁驱动，前段不转化即 STOP）。

## 阶段化门禁

- **US1（零成本诊断）**：`rescueable 占比 ≥ 20%` 才进 US2；否则 STOP 记录负结论。
- **US2（先导）**：008 铁律 `多步 majority ≥ 单次基线`（同预算）+ 类别不回归（L0-3）才 GO；否则 STOP。
- **US3（结构导航）**：仅在 US2 GO 后执行；同预算不降 + token 节流才保留。
- **US4（SaaS RL）**：仅在 US2 GO 后评估；SaaS 单独口径，不回填本地。

**⚠️ CRITICAL**: 引擎 untouchable（FR-003）——所有改动限 `cmd/locomo-bench/` 与 `specs/029/tools/`；`git diff --name-only -- memory embedding provider store internal` 必须为空。

---

## Phase 1: Setup（共享基础设施）

**Purpose**: 项目初始化 + 评测 flag 骨架

- [X] T001 创建 `specs/029-agentic-memory-navigation/tools/` 目录 + 分析脚本骨架（`nav_diagnose.py` / `nav_analyze.py` 空壳带 argparse）
- [X] T002 在 `cmd/locomo-bench/main.go` 添加 `--nav` / `--nav-max-steps`（默认 4）/ `--nav-k`（默认 8）flag 声明（暂不接线执行）

---

## Phase 2: Foundational（阻断 US2–US4 的 Go 导航骨架）

**Purpose**: 导航轨迹 schema、工具 JSON 解析、fail-closed 骨架——US2/3/4 的共同底座。**US1（纯 Python 诊断）不依赖本 phase，可并行开始。**

**⚠️ CRITICAL**: 本 phase 未完成不得开始任何 Go 侧导航实现。

- [X] T003 工具 JSON 解析器 `cmd/locomo-bench/nav_tools.go`：解析 `{"tool":..,"tool_args":..,"rationale":..}` → 校验工具名白名单（`search`/`expand_query`/`follow_entity`/`stop`）+ 参数类型 → 返回错误类型（未知工具 / 解析失败 / 参数非法），对齐 `contracts/navigation-tools.md`
- [X] T004 导航轨迹结构 + JSON 序列化 `cmd/locomo-bench/nav_trajectory.go`：`NavigationTrajectory`/`NavStep`/`EvidenceBundle`/`BudgetUsage`（`data-model.md` 字段 + `contracts/navigation-trajectory.md` schema），含 `budget_usage` 记账字段
- [X] T005 fail-closed 骨架 `cmd/locomo-bench/agentic_nav.go`：步数上限检查 + 解析/LLM 失败 → `fallback_triggered=true` → 用单次检索 top-k 结果作答（与现状一致，宪法 V）
- [X] T006 [P] 单测 `cmd/locomo-bench/nav_tools_test.go` + `nav_trajectory_test.go`：工具解析（合法/非法/未知工具）、轨迹序列化往返、fail-closed 触发（stub provider，无网络）

**Checkpoint**: Go 导航骨架就绪（解析/轨迹/fail-closed 全绿）→ US2 可实现；US1 已可并行跑。

---

## Phase 3: User Story 1 - 零成本诊断：多步导航的救回空间（Priority: P1）🎯 MVP

**Goal**: 对 84 题逐题三分类（gold 在池 / 单次 top-k 捞到 / 模拟多步可救 / 不在池），量化多步导航的救回空间，决定是否投入 US2。

**Independent Test**: `python specs/029-agentic-memory-navigation/tools/nav_diagnose.py` 产出 `diagnosis-report.json`（三分类计数 + rescueable 题的模拟动作与命中证据），可直接判定 rescueable 占比 ≥20%。

### Implementation for User Story 1

- [X] T007 [US1] 检索诊断产出：`cmd/locomo-bench` 新增 `--nav-diagnose` 模式（`nav_diagnose_cli.go`），逐题输出 gold 在池（全对话 oracle）+ 单次 top-k=30 gold rank + wide-pool gold rank → `run-dir/nav-diagnose.jsonl`（真实混合检索由 harness 保证，Python 无法跨语言调引擎）
- [X] T008 [US1] `nav_diagnose.py` 消费 `nav-diagnose.jsonl`：确定性模拟多步动作（rewrite 换查询 / follow_entity 实体跟链 / wide-pool 换粒度）→ 三分类 `in_pool`/`topk_hit`/`rescueable`/`not_in_pool` + 归因分布 + 抽样（合成数据单测通过）
- [X] T009 [US1] 跑诊断 + 写 `diagnosis/us1-verdict.md`：三分类计数 + 归因分布 + 抽样人审；`rescueable ≥ 20%` → GO 进 US2，否则 STOP 记录负结论 —— **实测 rescueable_share=0.655 → GO 进 US2**

**Checkpoint**: US1 verdict 产出。`rescueable` 占比决定是否继续。

---

## Phase 4: User Story 2 - 最小先导：推理驱动多步检索（Priority: P1）

**Goal**: 本地 35B 做多步导航（search→assess→decide→stop），84 题 × 3 reps 配对单次基线，验证 008 铁律（多步 ≥ 单次，同预算）。

**Independent Test**: `locomo-bench --nav` vs 单次基线，同 store/子集/answerer/judge，3 reps majority + McNemar + 类别不回归；`nav_analyze.py` 报告 majority/token 记账。

### Tests for User Story 2（TDD，先写失败测试）

- [X] T010 [P] [US2] 导航状态机测试 `cmd/locomo-bench/agentic_nav_test.go`：stub provider 返回固定工具调用序列 → 断言步进/证据累积/stop 组装/超步数 fallback（先红后绿）
- [X] T011 [P] [US2] 预算纪律测试：`stop` 证据包 token 超 cap → 截断/拒绝 + 记账断言（008 纪律）

### Implementation for User Story 2

- [X] T012 [P] [US2] `nav_tools.go` 工具执行：`search`/`expand_query`/`follow_entity` 调用引擎混合检索（复用现有 Retriever.Search 多次调用，返回 `Evidence[]` 含 `retrieved_by`），引擎零改动
- [X] T013 [P] [US2] `agentic_nav.go` 导航循环：系统 prompt（工具 schema + 已见证据注入）→ `provider.Provider` 调用（复用 local_planner 的 `newUsageModelCaller` 模式）→ 解析工具调用 → 执行 → 步数/预算记账 → `stop` 或 fallback
- [X] T014 [P] [US2] `nav_analyze.py`：消费轨迹 JSONL → majority + McNemar + 步数分布/token 记账 + 类别不回归（L0-3）
- [X] T015 [US2] `--nav` 接入 `cmd/locomo-bench/main.go` 现有配对路径：`--only-questions` + `--repeats 3` + judge（mem0-aligned）+ 轨迹落盘 `run-dir/nav-trajectories.jsonl`（`nav_integration_test.go` 端到端 stub 验证）
- [X] T016 [US2] 跑配对：基线（现有路径）vs 多步导航（`--nav`），84 题 × 3 reps，同 store/answerer/judge/预算 —— 4 次机制归因重跑（enable_thinking / 证据补足 / chunk-first），nav 25.0%→32.9%→34.5%→29.8%，基线 47.6%
- [X] T017 [US2] 配对分析 + 写 `diagnosis/us2-verdict.md`：majority + McNemar + 类别明细 + token 记账；008 铁律（多步 ≥ 单次）→ **nav 29.8% < base 47.6%（−17.9pp, p=0.0059）→ NO-GO，US3/US4 不执行**

**Checkpoint**: US2 verdict。端到端转化决定 US3/US4 是否执行。

---

## Phase 5: User Story 3 - 结构化层级导航（Priority: P2）**门禁：US2 GO 后**

**Goal**: MemCog 式层级（保留原文 + page/section + 类型化链接），验证相对平铺多步是提分还是省 token。

**Independent Test**: 「结构化导航 vs 平铺多步」配对，同预算正确率不降 + token 节流或等价；关闭层级投影逐字节回到平铺多步。

### Implementation for User Story 3

- [ ] T018 [P] [US3] `nav_project.py` 层级投影构建：原文之上 page/section 总结 + 类型化链接（`related_to`/`temporal_next`/`caused_by`/`contrasts_with`），可重建（config-hash 幂等，027 模式），**不替换原文** —— ⛔ US2 NO-GO，不执行
- [ ] T019 [US3] 结构化导航工具（`browse_dimension`/`read_page`/`follow_link`）+ 导航循环扩展，检索最终回到原文证据 —— ⛔ US2 NO-GO，不执行
- [ ] T020 [US3] 配对（结构化 vs 平铺多步）+ verdict：同预算不降 + token 记账（FR-005 默认关、可重建） —— ⛔ US2 NO-GO，不执行

**Checkpoint**: US3 verdict。层级导航净增益才考虑保留，否则维持平铺多步。

---

## Phase 6: User Story 4 - RL 导航策略训练（Priority: P2，SaaS）**门禁：US2 GO 后评估**

**Goal**: NapMem 式记忆金字塔 + GRPO 训练导航策略，SaaS 线单独口径。

**Independent Test**: 训练前后配对 + 非记忆任务不退化 + 工具调用行为（步数/命中率）报告；分数单独口径（SaaS/训练标记）。

### Implementation for User Story 4

- [ ] T021 [US4] 训练数据构建：导航轨迹（T015 落盘）→ 记忆多粒度金字塔（raw/records/topics/profile）+ 轨迹 reward 标注（正确作答 + 工具使用） —— ⛔ US2 NO-GO，不执行
- [ ] T022 [US4] GRPO 训练导航策略（SaaS 算力，AutoDL）+ 超参/seed 记录（可复现，FR-003 模式） —— ⛔ US2 NO-GO，不执行
- [ ] T023 [US4] 配对 + 单独口径登记：分数写入 `docs/evaluation/results.md` 单独行（SaaS/训练标记），不回填本地（死亡规则）；非记忆任务（推理/工具调用）不显著退化验证 —— ⛔ US2 NO-GO，不执行

**Checkpoint**: US4 verdict（SaaS 单独口径）。

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 收口（US2 已判定后即可启动，不等 US3/US4）

- [X] T024 各阶段 verdict 落 `docs/evaluation/`：`reports/029-agentic-memory-navigation-verdict.md`（US1/US2 各阶段 + 配对数据）+ `docs/evaluation/experiment-verdicts.md` 029 行（verdict/范围/出货影响/证据）
- [X] T025 更新复盘 doc `docs/research/lever-batch-local-vs-saas.md`：S-2（agentic 导航）实测结论（端到端转化与否，008 铁律约束）
- [X] T026 commit：docs + feat 分开 commit（宪法 IV 归因）；引擎零改动验证 `git diff --name-only -- memory embedding provider store internal` 为空
- [X] T027 零行为变化验证：`--nav` 默认关（SC-004）；nav 逻辑全部条件分支（`opt.nav && opt.navTraj != nil`），默认路径 `retrieveQuestionWithDiagnostics` 原样走；全包测试绿（无 nav 路径回归）

**Checkpoint**: 029 收口（US1/US2 verdict 落盘，US3/US4 按门禁标记执行/跳过）。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，可立即开始
- **Foundational (Phase 2)**: 依赖 Setup；**BLOCKS US2–US4**（US1 纯 Python 诊断可并行）
- **User Stories**: US1 → US2 → US3 → US4 串行门禁（阶段化，不并行——US1 GO 才进 US2，US2 GO 才进 US3/US4）
- **Polish (Phase 7)**: 依赖 US2 verdict（US1/US2 已判定后启动）

### User Story Dependencies

- **US1 (P1)**: 依赖 Setup；不依赖 Foundational（纯 Python）
- **US2 (P1)**: 依赖 Foundational（Go 导航骨架）+ US1 GO
- **US3 (P2)**: 依赖 US2 GO（门禁）
- **US4 (P2, SaaS)**: 依赖 US2 GO（门禁评估）

### Within Each User Story

- Go 部分：TDD（T010/T011 先红后绿）→ 工具执行 → 导航循环 → 配对
- Python 部分：诊断脚本 → 跑 → verdict
- Story 完成再进下一优先级

### Parallel Opportunities

- T003/T004（Foundational）与 T007（US1 诊断脚本）并行
- T010/T011（US2 测试）并行；T012/T013/T014（US2 实现，不同文件）并行
- T007/T008（US1）串行（T008 依赖 T007 输出）

---

## Parallel Example: US2 启动

```bash
# Foundational 完成后并行开 US2 测试与实现：
Task: "T010 导航状态机测试 (agentic_nav_test.go)"
Task: "T011 预算纪律测试 (nav_tools_test.go 扩展)"
Task: "T012 工具执行 (nav_tools.go)"
Task: "T013 导航循环 (agentic_nav.go)"
Task: "T014 配对分析 (nav_analyze.py)"
```

---

## Implementation Strategy

### MVP First（US1 诊断）

1. Setup（T001-T002）
2. US1 诊断（T007-T009）——**纯本地零成本**，产出 rescueable 占比 → 决定整线生死
3. STOP 验证：rescueable ≥20% 才投 US2（Go 导航）

### Incremental Delivery

1. US1 诊断 → verdict（MVP，救回空间）
2. US2 先导配对 → verdict（008 铁律唯一 GO 门）
3. US3 结构导航 / US4 SaaS RL → 门禁后

### Parallel Team Strategy

- US1 诊断脚本与 Go 导航骨架（Foundational）可双线并行
- US2 的测试（TDD）与实现并行推进

---

## Notes

- [P] 任务 = 不同文件，无依赖
- [USn] 标签映射到 spec 的 user story
- 门禁未过（US1 rescueable<20% / US2 不转化）→ 记录负结论 STOP，不硬投 US3/US4
- 评测配置变更与算法改动分开 commit（宪法 IV）
- 引擎零改动硬门：`git diff --name-only -- memory embedding provider store internal` 为空
- 避免：跨 story 依赖破坏独立性、同文件冲突、门禁未过仍硬投
