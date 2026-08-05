---

description: "Task list for 027 write-side event structure memory"
---

# Tasks: 写入侧事件时序结构记忆（027）

**Input**: Design documents from `/specs/027-write-side-event-structure/`

**Prerequisites**: plan.md（required）、spec.md（user stories）、research.md（D1–D5）、data-model.md、contracts/、quickstart.md

**Tests**: 本项目引擎行为改动为 TDD（CLAUDE.md 硬门）——测试先行、先红后绿。

**Organization**: 按 spec 四 user story 组织。**阶段化门禁**：US1（阶段 0 诊断）先行，gold 在池且打包缺上下文才进 US2；US2（阶段 1 先导）端到端转化才进 US3；US3（阶段 2 全量）GO 才允许 US4（阶段 3，P2 可选）。008 铁律下任一阶段不转化即 STOP。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel（不同文件、无依赖）
- **[Story]**: US1–US4（映射 spec.md user stories）
- Include exact file paths in descriptions

## Path Conventions

- 引擎侧：`memory/eventstore/`（027 新增可重建投影，如 022 fact 投影）
- harness 侧：`cmd/locomo-bench/`（复用 025 shared-anchor 渲染器 + 023 `--only-questions`）

---

## Phase 1: Setup（资产确认 + fixture 准备）

**Purpose**: 确认承接资产可用、准备测试夹具（零代码，不碰引擎）

- [x] T001 核实承接资产可用：`provider/` LLM 抽象接口、022 Evidence Ledger、`cmd/locomo-bench/representation_eval.go` 的 shared-anchor 设计、`--only-questions` formal 子集模式（023）；记录每项可用性到 quickstart 前置条件
- [x] T002 准备 event 抽取 fixture：合法/非法 JSON、缺字段、空 fact_entries、未知 relation_type、超长 text、source 缺失——**内联在 `memory/eventstore/event_test.go`**（未建独立 testdata/，byte-replay 测试直接覆盖）

---

## Phase 2: User Story 1 - 阶段 0 诊断：gold 在不在池（Priority: P1）🎯 MVP

**Goal**: 零成本确诊 temporal + multi-hop 错题里答案片段是否在候选池、是否打包丢上下文——决定 B 是否有救（spec US1，FR-001 诊断、SC 阶段 0）

**Independent Test**: 产出分类计数（gold 在池 / 不在池 / 在池但打包缺上下文）；「在池但打包缺上下文」占比高（预期多数）→ GO 进 US2；gold 不在池占比高 → STOP 记录负结论（不烧引擎实现）

### Implementation for User Story 1

- [x] T003 [US1] 从已冻结 store 导出 temporal + multi-hop 错题清单（含 gold 答案），按 conversation 分组，写 `specs/027-write-side-event-structure/diagnosis/diagnosis-cohort.json`（84 题：temporal 59 + multi-hop 25，来源 009-full-B-cattopk run-1 = 86.4%）
- [x] T004 [US1] 诊断 gold 在不在对话/候选：写入覆盖 100%（84/84 evidence 原文在 store）+ candidate oracle（top-30 hybrid coverage：session recall 89.6% / turn recall 2.4%）——自动判定 + 抽样样例（`diagnose_phase0.py` + `coverage-run-hybrid/coverage.json`）
- [x] T005 [US1] 产出阶段 0 诊断报告（`diagnosis/phase0-report.md`）：**判定 GO**——gold 在池（写入覆盖 + session 覆盖），STOP 门不触发；错题机制=关系/时序表达 + 检索精确命中弱，正是 B 的写入侧 event 结构针对点。进 US2（Foundational）

**Checkpoint**: 阶段 0 判定产出。GO 才继续。

---

## Phase 3: Foundational - 引擎侧 event 投影（Blocking US2–US4）

**Purpose**: `memory/eventstore/` 可重建投影核心（data-model.md + contracts/）。**TDD：测试先行**。完成前 US2 不能开始。

**⚠️ CRITICAL**: 本 phase 是 US2–US4 的共同底座；未完成不得开始任何 user story 实现。

### Tests for Foundational ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [x] T006 [P] 写 Event 类型 + schema 校验器的失败测试在 `memory/eventstore/event_test.go`（覆盖 event-contract.md 全部校验规则：合法/非法/缺字段/空数组/未知枚举/超长/无 source）✓ 全绿
- [x] T007 [P] 写抽取 client fail-closed 的失败测试在 `memory/eventstore/event_test.go`（fixture byte-replay：抽取成功→Event；任意校验失败→退回原文、记录失败原因；LLM 端点不可达→零行为变化）✓ 全绿

### Implementation for Foundational

- [x] T008 实现 Event/FactEntry/RelationEntry/RelationSummary 类型在 `memory/eventstore/event.go`（data-model.md 字段契约，含 grounded 字段）
- [x] T009 实现 schema 校验器：**并入 `memory/eventstore/event.go`**（`Validate` 严格 + `ValidateLenient` 宽容 relation_type，纯 Go 确定性，无 LLM 依赖）
- [x] T010 实现抽取 client 在 `memory/eventstore/extract.go`：调用 `provider/` LLM（本地 sidecar）+ 校验，失败 fail-closed 退回原文 chunk、记录失败（contracts/fail-closed.md）
- [x] T011 实现可重建投影在 `memory/eventstore/project.go`：config-hash 幂等重建（`BuildProject`/`Write` 原子写/`LoadProject`/`Render` 有界），**真实 build-event 产出 5870 事件投影**（FR-003）
- [x] T012 跨事件合并（RelationSummary）：**类型已实现（event.go），周期性合并逻辑未实现**——RelationSummary 直接随 event 渲染，NO-GO 下无需周期合并（FR-007）
- [x] T013 判定统计：**并入 `memory/eventstore/extract.go` 的 `Stats`**（attempts/successes/failures + FailureReasons 分类，FR-006）
- [x] T014 引擎自检：`CGO_ENABLED=0 go test -count=1 ./memory/eventstore/` 全绿 ✓

**Checkpoint**: 引擎侧 event 投影可用（TDD 全绿）。US2 可开始。

---

## Phase 4: User Story 2 - 阶段 1 先导：event vs chunk 配对（Priority: P1）

**Goal**: 本地 sidecar 抽 event，`--only-questions` 子集配对 temporal + multi-hop，验证端到端转化（spec US2，FR-001/004/005/008、SC 阶段 1）

**Independent Test**: 同一子集题、同一 answerer/judge/token cap，event store vs chunk store 的 majority 配对（McNemar）；temporal + multi-hop 提升且 overall 不回归 → GO 进 US3；否则 STOP（008 铁律）

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [x] T015 [P] [US2] 写 event 渲染器失败测试在 `cmd/locomo-bench/representation_eval_test.go`：同一 ranked anchor 下 ReprEvent 渲染出事件 + 关系摘要上下文（复用 025 shared-anchor 设计，事件有界、默认关零行为变化）

### Implementation for User Story 2

- [x] T016 [P] [US2] 实现 event 渲染器在 `cmd/locomo-bench/representation_eval.go`：新增 ReprEvent（围绕同一 anchor 渲染 event + RelationSummary，有界）
- [x] T017 [US2] 实现 `--representation event` flag + event store 构建 + 配对跑法在 `cmd/locomo-bench/eventstore_eval.go`（复用 `--only-questions` 子集模式，`config-hash` 记录）
- [x] T018 [US2] 跑先导配对：temporal + multi-hop 子集（`--only-questions`），event vs chunk，repeats=3（quickstart 阶段 1 命令）
- [x] T019 [US2] 抽取质量审计：build-event 5870/5882 成功（99.8%），schema 校验 fail-closed 生效；**D1 门暴露时间锚定缺陷**（5870 event 仅 5% 带绝对时间）——主导失败机制，未升级 35B（verdict 已明确方向，升级不改变结论）
- [x] T020 [US2] 先导 verdict：chunk majority 50.0% vs event 23.8%，**−26.2pp，McNemar p=0.0016 → NO-GO STOP**（008 铁律，不进入默认路径）。[verdict](../../docs/evaluation/reports/027-write-side-event-verdict.md)

**Checkpoint**: 阶段 1 先导 verdict 产出 = **STOP（NO-GO）**。US3/US4 不再执行。

---

## Phase 5: User Story 3 - 阶段 2 全量配对：双基准不回归（Priority: P1）

**Goal**: LoCoMo 1540 + LongMemEval-S 500 全量配对，确认 temporal/multi-hop 增益且 overall 不回归（spec US3，宪法 IV、SC 阶段 2）

**Independent Test**: 冻结协议全量配对（chunk 基线 vs event 表示），repeats≥3，分类别配对统计；GO 门 = temporal+multi-hop ≥2.0pp 且 McNemar p<0.05 量级 + overall non-regression + validity 全绿；负则记录 verdict、保持默认关

### Implementation for User Story 3

- [x] T021 [US3] ~~跑 LoCoMo 全量配对（1540）~~ **不再执行**——阶段 1 已 NO-GO（−26.2pp/p=0.0016），008 铁律下不进全量
- [x] T022 [US3] ~~跑 LongMemEval-S 全量配对（500）~~ **不再执行**——同上
- [x] T023 [US3] ~~分类别配对统计~~ **已由阶段 1 先导覆盖**（McNemar + 分类别明细见 [verdict](../../docs/evaluation/reports/027-write-side-event-verdict.md)）
- [x] T024 [US3] verdict 收口：**NO-GO**，记录负结果（FR-010），不进入默认路径

**Checkpoint**: 阶段 2 verdict 收口 = **NO-GO（阶段 1 已 STOP，全量未跑）**。

---

## Phase 6: User Story 4 - 阶段 3: segment tree 粒度（Priority: P2，可选，仅 US3 GO 后）

**Goal**: 时间有序 segment tree（SEGTREEMEM 式）在不降分下减少 token 或提升跨事件召回（spec US4，FR-009）

**Independent Test**: 同事件 store、同预算，segment tree 传播检索 vs 平铺 event 召回；正确数不降且 token 节流或等价；无增益 → 维持平铺 event 路径，不强上线

### Implementation for User Story 4

- [x] T025 [P] [US4] ~~segment tree 构建~~ **不再执行**——US3 未 GO（仅 US3 GO 后可选）
- [x] T026 [P] [US4] ~~沿树传播检索~~ **不再执行**——同上
- [x] T027 [US4] ~~配对验证~~ **不再执行**——同上

**Checkpoint**: 阶段 3 不进入（US2 NO-GO 已 STOP 整条线）。

---

## Phase 7: Polish & 收口

**Purpose**: verdict 落盘 + 资产登记 + 复盘更新（verdicts-go-to-tracked-docs 纪律）

- [x] T028 结果与 verdict 落 `docs/evaluation/`：[027-write-side-event-verdict.md](../../docs/evaluation/reports/027-write-side-event-verdict.md)（实验结论 MUST 进 tracked docs，不能只进本地 memory）
- [x] T029 更新 `docs/evaluation/experiment-verdicts.md`（027 行：verdict / 范围 / 出货影响 / 证据）
- [x] T030 更新复盘 doc `docs/evaluation/reports/cost-effectiveness-retrospective-2026-08.md` §6（B 方向实测证伪 → §6.5 + §6.4 B 行更新）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup（Phase 1）**: 无依赖，可立即开始
- **US1（Phase 2）**: 零成本诊断，不依赖引擎代码——**先行门禁**
- **Foundational（Phase 3）**: 依赖 Setup；**BLOCKS US2–US4**
- **US2（Phase 4）**: 依赖 US1 GO（阶段 0 判定）+ Foundational 完成
- **US3（Phase 5）**: 依赖 US2 GO（阶段 1 转化）
- **US4（Phase 6）**: 依赖 US3 GO（P2，可选）
- **Polish（Phase 7）**: 依赖所有执行的 user story 完成

### User Story Dependencies

- **US1（P1）**: 先行、零成本、STOP 门——gold 不在池则 027 收口
- **US2（P1）**: 端到端转化门（008 铁律）
- **US3（P1）**: 宪法 IV 回归门 + GO 门
- **US4（P2）**: 可选，仅 US3 GO 后

### Within Each Phase

- Tests 先写、先红后绿（TDD 硬门）
- 引擎类型（T008）→ 校验器（T009）→ 抽取 client（T010）→ 投影（T011）→ 合并（T012）→ 统计（T013）
- 门禁判定不通过 → STOP 收口，不继续

### Parallel Opportunities

- Setup：T001/T002 可并行
- Foundational：T006/T007 测试可并行；T008/T012/T013 实现可并行（不同文件）
- US2：T015 测试先行，T016/T017 实现可并行
- 阶段化门禁使 US2–US4 顺序执行（不可并行）

---

## Parallel Example: Foundational

```bash
# 并行写失败测试：
Task: "T006 schema 校验器失败测试 memory/eventstore/event_test.go"
Task: "T007 抽取 client fail-closed 失败测试 memory/eventstore/extract_test.go"
# 测试红 → 实现：
Task: "T008 event 类型 memory/eventstore/event.go"
Task: "T012 consolidate memory/eventstore/consolidate.go"
Task: "T013 metrics memory/eventstore/metrics.go"
```

---

## Implementation Strategy

### MVP First（US1 + US2 先导）

1. Phase 1 Setup → Phase 2 US1（阶段 0 诊断，零成本门禁）
2. 诊断 GO → Phase 3 Foundational（引擎 event 投影）
3. Phase 4 US2 先导配对（`--only-questions` 子集，~35 分钟）
4. **STOP and VALIDATE**: 端到端转化才继续 US3；不转化即收口

### Incremental Delivery

1. Setup + US1 诊断 → STOP/GO 门
2. Foundational 引擎投影 → TDD 全绿
3. US2 先导配对 → 转化门
4. US3 全量配对 → 宪法 IV 回归门 + GO 门
5. US4 segment tree（可选）→ 省 token 验证

### 阶段化门禁（不盲烧，复盘纪律）

- US1 未 GO 不写引擎代码（零成本诊断先行）
- US2 未转化不跑全量（先导省机器）
- US3 未 GO 不碰 segment tree
- 008 铁律：任何阶段端到端不转化即 STOP，记录负结论

---

## Notes

- [P] tasks = 不同文件、无依赖
- [Story] label maps task to spec user story for traceability
- 引擎改动 MUST TDD（先红后绿），`CGO_ENABLED=0 go test -count=1` 硬门
- 引擎隔离：本 feature 是引擎 feature（新增 `memory/eventstore/`），不违反宪法 II；harness 只做渲染与验证
- DEATH RULE：event 抽取 MUST 走本地 sidecar，禁止付费云模型/reranker
- 配对纪律：同 store 候选逐字节一致、同 answerer/judge/token cap、repeats≥3（answerer temp=1.0 噪声）
- 提交纪律：每个逻辑组一个 commit；评测配置改动与算法改动分开提交（宪法 IV 可归因）
