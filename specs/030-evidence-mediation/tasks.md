# Tasks: 读侧证据装配结构（Evidence Mediation）

**Input**: Design documents from `/specs/030-evidence-mediation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Organization**: Tasks grouped by user story（US1 装配地基 → US2 引用链中介 / US3 条件压缩，门禁驱动，US1 chunk 保底不达标即 STOP）。

## 阶段化门禁

- **US1（纯 Go 零模型）**：`chunk_fraction ≥ 阈值`（SC-002，修 029 的 ~1%）+ token 记账零估算误差才 GO 进 US2/US3；否则 STOP 记录负结论。
- **US2（先导）**：008 铁律 `majority ≥ 单次基线`（同预算）+ 类别不回归（L0-3）才 GO；否则 STOP，US3 独立评估。
- **US3（条件压缩，P2）**：默认关 parity + 超预算子集上压缩 arm 不显著回退才保留；否则维持原文装配。

**⚠️ CRITICAL**: 引擎 untouchable（FR-001）——所有改动限 `cmd/locomo-bench/` 与 `specs/030/tools/`；`git diff --name-only -- memory embedding provider store internal` 必须为空。

---

## Phase 1: Setup（共享基础设施）

**Purpose**: 评测 flag 骨架 + 分析脚本占位

- [X] T001 创建 `specs/030-evidence-mediation/tools/` 目录 + 分析脚本空壳（`assembly_diagnose.py` / `trace_analyze.py` / `consolidation_analyze.py` 带 argparse）
- [X] T002 在 `cmd/locomo-bench/main.go` 声明 4 个 flag（`--evidence-assembly` / `--assembly-diagnose` / `--trace-mediation` / `--consolidate`，全默认关，暂不接线）

---

## Phase 2: Foundational（阻断 US1–US3 的 Go 装配骨架）

**Purpose**: 证据单元类型 + 精确 token 计数基建——US1/2/3 的共同底座。**US1 装配器（Phase 3）依赖本 phase。**

**⚠️ CRITICAL**: 本 phase 未完成不得开始任何装配/中介/压缩实现。

- [X] T003 [P] 装配数据类型 `cmd/locomo-bench/assembly.go`：`EvidenceUnit`（source_id/text/kind/token_count/estimated/score/event_date）+ `EvidenceAssembly`（units/total_tokens/structure/chunk_fraction/tokens_estimated），chunk/fact 判定（`chunk-` 前缀，复用 chunks.go:210），对齐 data-model.md
- [X] T004 [P] 精确 token 计数 `cmd/locomo-bench/assembly_tokens.go`：批量 `/tokenize`（复用 `vllmTokenCounter` 基建 token_counter.go，扩展批量文本输入）；stub-able 接口（离线单测可注入 fake）；`estimateTokens`（agentic_nav.go:419）保留为 fallback 并置 `estimated=true`（宪法 V）
- [X] T005 [P] 单测 `cmd/locomo-bench/assembly_test.go`（TDD 先红后绿）：EvidenceUnit 记账字段 / chunk-fact 判定 / estimateTokens fallback 显式降级标记（stub tokenizer，无网络）

**Checkpoint**: 装配骨架就绪（类型/记账/降级全绿）→ US1 可实现；US2/US3 依赖 US1 先落地。

---

## Phase 3: User Story 1 - 预算诚实的证据装配地基 (Priority: P1) 🎯 MVP

**Goal**: 装配器把候选证据变成 token 精确、chunk 原文优先、按类别结构组织的 EvidenceAssembly（修 029 根因 A 的 fact 稀释）。纯 Go 零模型。

**Independent Test**: `CGO_ENABLED=0 go test -run 'TestAssembly'` 离线断言（token 零估算误差 / chunk_fraction ≥ 阈值 / 类别结构 / cap / parity）+ `--assembly-diagnose` 全量离线审计。

### Tests for User Story 1（TDD，先写失败测试）

- [X] T006 [P] [US1] 装配测试 `cmd/locomo-bench/assembly_test.go`（TDD 先红后绿）：token 记账零估算误差（stub 精确值 vs estimateTokens）、chunk 优先序、cap 截断、全关 parity（先红后绿）

### Implementation for User Story 1

- [X] T007 [P] [US1] 装配器核心 `cmd/locomo-bench/assembly.go`（组装逻辑）：chunk 先入（score 降序）→ fact 补足 → cap 截断；输出 `chunk_fraction`（contracts/evidence-assembly.md）
- [X] T008 [P] [US1] 类别条件装配 `cmd/locomo-bench/entity_grouping.go`：per-category 策略选择器（复用 `retrievalFor(qa.Category)` 模式）；temporal（cat 2）→ 复用 `buildTimelineBlock` + 时间序；multi-hop（cat 1）→ 实体分组（实体提取 + 组内分数序，借鉴 IRIS slot-merge 去重/边界）；3/4 → generic（chunk 优先 + 分数序）（research 决策 4）
- [X] T009 [US1] `--evidence-assembly` 接线 `cmd/locomo-bench/main.go`：条件分支（`opt.evidenceAssembly`），默认关闭时渲染路径逐字节不变（SC-004）；`--assembly-diagnose` 输出装配 JSONL（0o600）
- [X] T010 [US1] `specs/030-evidence-mediation/tools/assembly_diagnose.py`：消费装配 JSONL → chunk_fraction / total_tokens / structure / tokens_estimated 审计报告

### Integration for User Story 1

- [X] T011 [US1] 全量离线装配诊断（LoCoMo store，`--chunks --evidence-assembly --assembly-diagnose`，零模型成本）+ 写 `diagnosis/us1-verdict.md`：chunk_fraction 达标（SC-002）→ GO 进 US2/US3；不达标 STOP 记录负结论

**Checkpoint**: US1 verdict。chunk 保底达标决定 US2/US3 是否开始。

---

## Phase 4: User Story 2 - 引用链证据中介（Grounded Evidence Mediation）(Priority: P1)

**Goal**: sidecar（opt-in）把候选组织成 plan→trace→actions→evidence 四层，E 只喂 answerer；fail-closed 门（纯 Go 确定性）把关。MemChain 去 trace −13.96pp 是 post-retrieval 最强单结构。

**Independent Test**: fail-closed 门离线单测（非法 ID 丢弃 / 解析失败重试→回退 / parity）+ `--trace-mediation` 配对 84 题 × 3 reps majority ≥ 基线（008 铁律）。

### Tests for User Story 2（TDD，先写失败测试）

- [X] T012 [P] [US2] fail-closed 测试 `cmd/locomo-bench/trace_gate_test.go`（TDD 先红后绿）：非法 ID（候选集外）丢弃 → E 空回退 / 解析失败重试一次再回退 / 合法 packet 每 evidence 可回溯 / sidecar 未配置 parity（stub provider，无网络）

### Implementation for User Story 2

- [X] T013 [P] [US2] trace 数据结构 `cmd/locomo-bench/trace_mediation.go`：`plan/trace/actions/evidence` 四层 + 动作词表（KEEP/DROP/MERGE/REFINE/ADD，contracts/grounded-trace.md schema）
- [X] T014 [P] [US2] fail-closed 门 `cmd/locomo-bench/trace_gate.go`：`extractPacketJSON`（遍历候选 JSON，029 `extractNavJSONCandidates` 模式）→ 闭包校验（cited_ids ⊆ C_q）→ E 非空 → 回溯校验 → 依次降级 fallback
- [X] T015 [P] [US2] sidecar caller `cmd/locomo-bench/trace_http.go`：harness-side HTTP（DeepSeek-flash，`enable_thinking=false` 纯 JSON，029 `nav_http.go` 模式）
- [X] T016 [US2] `--trace-mediation` 接线 `cmd/locomo-bench/main.go`：条件分支，E 走 US1 装配渲染（精确 token 记账 + cap）；sidecar 未配置时路径零行为变化
- [X] T017 [US2] `specs/030-evidence-mediation/tools/trace_analyze.py`：消费门状态/轨迹 → majority + McNemar + 类别回归（L0-3）+ 门状态分布（valid/invalid_citation/parse_failed/fallback）

### Integration for User Story 2

- [ ] T018 [US2] 配对：base vs trace arm，84 题 × 3 reps（复用 029 `phase0-ids.txt`，`--only-questions` + `--repeats 3`，同 store/answerer/judge/预算）
- [ ] T019 [US2] 配对分析 + 写 `diagnosis/us2-verdict.md`：008 铁律 majority ≥ 基线 + L0-3 类别不回归 → GO/NO-GO；NO-GO 则 US3 独立评估

**Checkpoint**: US2 verdict。008 铁律唯一 GO 门。

---

## Phase 5: User Story 3 - 条件压缩操作符 (Priority: P2)

**Goal**: 仅当证据确定超预算且显式启用时，压缩（Abstract/Merge）替换原文；默认保留原文（Retain or Consolidate 宽松预算 Merge −0.107 显著为负）。重演预算交叉或诚实报告负结论。

**Independent Test**: 默认关 parity + 超预算 opt-in 压缩 ≤ cap + 压缩 arm 配对不显著回退。

### Tests for User Story 3（TDD，先写失败测试）

- [X] T020 [P] [US3] 压缩测试 `cmd/locomo-bench/consolidate_test.go`（TDD 先红后绿）：默认关（未启用时与 US1 装配逐字节一致）/ 超预算触发 / 压缩后 ≤ cap / replaced_unit_ids 记录

### Implementation for User Story 3

- [X] T021 [P] [US3] 压缩操作符 `cmd/locomo-bench/consolidate.go`：when（复用 US1 精确记账判超预算）/ which（Abstract/Merge 优先于 Rewrite）/ 输出 ≤ cap / `replaced_unit_ids` 审计（contracts/consolidation.md）
- [X] T022 [US3] `--consolidate` 接线 `cmd/locomo-bench/main.go`：条件分支，默认关；未启用时超预算仅截断（现状行为）
- [X] T023 [US3] `specs/030-evidence-mediation/tools/consolidation_analyze.py`：预算交叉分析（超预算题占比 / 压缩后 ≤ cap / e2e 配对不显著回退）

### Integration for User Story 3

- [ ] T024 [US3] 配对：keep vs cons arm（同 84 题）+ 分析 + 写 `diagnosis/us3-verdict.md`：重演预算交叉或诚实报告负结论（008 纪律）

**Checkpoint**: US3 verdict。压缩 arm 不显著回退才保留。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 收口（US2/US3 判定后启动）

- [ ] T025 verdict 落 `docs/evaluation/`：`reports/030-evidence-mediation-verdict.md`（US1/US2/US3 各阶段 + 配对数据）+ `docs/evaluation/experiment-verdicts.md` 030 行
- [X] T026 更新复盘 `docs/research/lever-batch-local-vs-saas.md`：L1-2（引用链）/L1-1（精确装填）实测增量（008 铁律约束）
- [ ] T027 commit：docs + feat 分开 commit（宪法 IV 归因）；引擎零改动验证 `git diff --name-only -- memory embedding provider store internal` 为空
- [X] T028 零行为变化验证：全关默认 parity（`--evidence-assembly`/`--trace-mediation`/`--consolidate` 全默认关）；全包测试绿（无装配路径回归）

**Checkpoint**: 030 收口（US1/US2/US3 verdict 落盘，chunk 保底修复坐实，008 纪律）。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，可立即开始
- **Foundational (Phase 2)**: 依赖 Setup；**BLOCKS US1–US3**
- **User Stories**: US1 → US2/US3（US1 GO 后 US2/US3 可并行；都依赖 US1 装配器）
- **Polish (Phase 6)**: 依赖 US2/US3 判定

### User Story Dependencies

- **US1 (P1)**: 依赖 Foundational；独立完成（纯 Go 零模型）
- **US2 (P1)**: 依赖 Foundational + US1（E 走 US1 装配渲染）；008 铁律门
- **US3 (P2)**: 依赖 Foundational + US1（超预算判定复用 US1 记账）；与 US2 并行（互不依赖）

### Within Each User Story

- Go 部分：TDD（T006/T012/T020 先红后绿）→ 实现 → 接线 → 配对 → verdict
- Python 部分：分析脚本 → 跑 → verdict
- Story 完成再进下一优先级

### Parallel Opportunities

- T003/T004/T005（Foundational）并行
- T007/T008（US1 实现，不同文件）并行；T006 先行
- T012-T015（US2 测试+实现，不同文件）并行
- US2 与 US3 可并行（均依赖 US1，互不依赖）
- T025/T026（Polish 文档）并行

---

## Parallel Example: US1 启动

```bash
Task: "T006 装配测试 (assembly_test.go，先红后绿)"
Task: "T007 装配器核心 (assembly.go)"
Task: "T008 类别条件装配 (entity_grouping.go)"
```

---

## Implementation Strategy

### MVP First（US1 装配地基）

1. Setup（T001-T002）
2. Foundational（T003-T005）——装配骨架
3. US1（T006-T011）——**纯本地零成本**，产出 chunk_fraction 达标 → 决定整线生死
4. STOP 验证：chunk 保底达标才投 US2/US3

### Incremental Delivery

1. US1 装配 → verdict（MVP，修 029 根因 A）
2. US2 引用链配对 → verdict（008 铁律唯一 GO 门）
3. US3 条件压缩配对 → verdict（预算交叉或负结论）
4. 全关默认 parity + 引擎零改动为贯穿约束

### Parallel Team Strategy

- Foundational 与 US1 测试可双线并行
- US2/US3 在 US1 GO 后可双线推进

---

## Notes

- [P] 任务 = 不同文件，无依赖
- [USn] 标签映射到 spec 的 user story
- 门禁未过（US1 chunk_fraction 不达标 / US2 008 不 GO）→ 记录负结论 STOP，不硬投
- 评测配置变更与算法改动分开 commit（宪法 IV）
- 引擎零改动硬门：`git diff --name-only -- memory embedding provider store internal` 为空
- 避免：跨 story 依赖破坏独立性、同文件冲突、门禁未过仍硬投
