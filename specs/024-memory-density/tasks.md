---

description: "Task list for 024-memory-density implementation"

---

# Tasks: 同预算记忆密度杠杆

**Input**: Design documents from `/specs/024-memory-density/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/mechanism-bindings.md, quickstart.md

**Tests**: 本项目遵循 CLAUDE.md 硬规则——引擎行为改动 TDD（先写失败测试，No test = not done）。因此每个 US 的测试任务是必需的。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## 归因提交纪律（宪法 IV）

- 引擎机制代码（US1/US2 实现）与 eval 协议绑定（Foundational 的 mechanism 框架）与 eval 配置（US3 的 manifest）**分 commit** 提交。
- 每任务完成即 commit；不要在 US 之间混提交。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 固定实验前提

- [ ] T001 确认在 `024-memory-density` 分支且 active feature 为 024（`cat .specify/feature.json` → `specs/024-memory-density`）
- [ ] T002 确认 022 冻结协议与 store 资产可用：`/root/022-runs/b1-high.json`、`b1-low.json`、LoCoMo 基线（85.32% B0 / 82.1% B1-high）、LongMemEval-S 基线，记录到本 feature 的基准登记

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 两个 US 与验证都依赖的公共件

**⚠️ CRITICAL**: US1/US2 的实现需要判定接口，US3 的验证需要 mechanism 绑定框架

- [ ] T003 [P] 在 `cmd/locomo-bench/eval_runner.go` 的 `freezeFormalProtocol` / `validateFormalMechanismBinding` 与 `cmd/locomo-bench/main.go` 的 `validateMechanismArms` / `supportedArmMechanisms` 中，按 `contracts/mechanism-bindings.md` 增加 `write_dedup` / `neighbor_extend` 两个新 mechanism key（解析、fail closed、参与 protocol_hash，缺省 false）
- [ ] T004 [P] 在 `memory/pipeline/pipeline.go` 定义冗余判定接口（如 `ShouldSuppress(ctx, existing, incoming *memory.Entry) bool`）+ 审计统计结构（判定/抑制/疑似误伤计数），作为 US1 的注入点
- [ ] T005 在 `memory/curation/` 新增 `suppress.go`：封装复用 `dedup.go` 的 `normalizeText` + 字符 trigram Jaccard（阈值 0.7 可配，FTS pre-filter 限 candidate pairs），实现 `T004` 接口的离线默认判定器；无 embedding 端点时此路径即唯一路径（依赖 T004 的接口签名）

**Checkpoint**: 判定接口 + 离线实现 + mechanism 框架就绪，US1/US2/US3 可开始

---

## Phase 3: User Story 1 - 写入时冗余抑制 (Priority: P1) 🎯 MVP

**Goal**: 新增 atomic fact 投影前检测语义冗余，抑制重复投影创建（evidence ledger 无损），提高候选信息密度

**Independent Test**: 离线写入两条近似事实 → 第二条投影被抑制且两条 evidence 完整；关闭时行为与现状一致

### Tests for User Story 1（先写、先红）⚠️

- [ ] T006 [P] [US1] `memory/pipeline/` 新增测试：写入两条近似事实 → 第二条不产生新投影、evidence 完整（`storeFact` 抑制）
- [ ] T007 [P] [US1] 测试：冗余抑制关闭（默认）时写入近似事实 → 与现状逐字节一致（零行为变化）
- [ ] T008 [P] [US1] 测试：无 embedding 端点 → 走离线 Jaccard 判定路径（不 panic、不依赖 sidecar）
- [ ] T009 [P] [US1] 测试：冲突事实（如日期纠正，非冗余）→ 不被抑制、照常建投影

### Implementation for User Story 1

- [ ] T010 [P] [US1] 在 `memory/pipeline/pipeline.go` 的 `storeFact` 创建投影前接入 `T004` 判定接口：判定冗余则不执行 `replaceAtomicFactProjectionTx` 的 create 分支（复用 `memory/projection.go`），evidence 保持完整
- [ ] T011 [P] [US1] 实现审计统计输出（判定/抑制/疑似误伤计数），写入 run 统计产物供误伤率评估（spec FR-005）
- [ ] T012 [US1] 可选叠加：embedding 语义相似度判定层（默认关，需本地 sidecar，阈值可配 ~0.9），与离线结果 OR/加权组合（research.md Decision 1）；本任务依赖 T004

**Checkpoint**: US1 可独立验证——抑制开启/关闭行为如 spec US1 三场景

---

## Phase 4: User Story 2 - 命中后邻居扩展 (Priority: P1)

**Goal**: 检索命中 fact 后，在候选冻结之后、answerer 组装之前，沿共享 evidence 取兄弟 fact（depth-1 有界）扩展上下文

**Independent Test**: 两 fact 共享 evidence → 命中其一断言另一出现在扩展候选；无邻居零变化

### Tests for User Story 2（先写、先红）⚠️

- [ ] T013 [P] [US2] `memory/projection.go` 或 `cmd/locomo-bench/` 新增测试：两 fact 共享 evidence → 命中其一，兄弟出现在扩展候选
- [ ] T014 [P] [US2] 测试：无邻居可扩展时 → 行为与关闭一致（零变化）
- [ ] T015 [P] [US2] 测试：兄弟数量超上限 → 有界截断（不无界放大候选/token）

### Implementation for User Story 2

- [ ] T016 [P] [US2] 在 `memory/projection.go` 的 `ProjectionStore` 新增兄弟 fact 查询方法（`memory_projection_sources` 共享 `evidence_id` 推导，depth-1，上限可配，确定性顺序：evidence 顺序 → fact name，见 data-model.md）
- [ ] T017 [P] [US2] 在 `cmd/locomo-bench` 的 formal materialize（候选冻结后、answerer 上下文组装前）接入邻居扩展：命中 fact 集 → 有界兄弟扩展 → 合并进 answer 上下文（不触发额外检索调用，spec FR-007）

**Checkpoint**: US2 可独立验证——扩展开启/关闭行为如 spec US2 三场景

---

## Phase 5: User Story 3 - 双基准同预算配对验证 (Priority: P1)

**Goal**: 在 022 冻结协议 + 固定 answer-context token cap 下，对两机制做四臂配对消融，确认独立收益且不回归基线

**Independent Test**: 四臂（关/开 write_dedup × 关/开 neighbor_extend，repeats≥3）在双基准上均有分类别明细与配对统计

### Implementation for User Story 3

- [ ] T018 [US3] 冻结四个 protocol manifest（none / dedup-only / neighbor-only / both），基于 022 的 `b1-high.json` 冻结基线，新增机制 flag 绑定（`contracts/mechanism-bindings.md`）
- [ ] T019 [US3] LoCoMo 四臂配对（F0 门）：`cmd/locomo-bench --eval-protocol <manifest> --run-dir ...`，repeats≥3，固定 token cap；判断任一机制不显著回归、有方向收益则进下一步，无收益/负则记录负结果收口（spec SC-003）
- [ ] T020 [US3] LongMemEval-S 四臂配对（同 recipe，数据 `testdata/longmemeval/longmemeval_s_cleaned.json`）
- [ ] T021 [US3] 报告与归档：双基准 overall + 分类别 + exact McNemar + token 记账 + 候选冗余下降度量（SC-001）+ 交互效应（SC-003）写入 `docs/evaluation/`；负收益机制保持默认关并记录 verdict

**Checkpoint**: US3 产出双基准四臂验证报告，宪法 IV 门完成

---

## Phase N: Polish & Cross-Cutting Concerns

**Purpose**: 收尾、门禁与一致性

- [ ] T022 [P] 引擎门：`CGO_ENABLED=0 go build ./...` 零错误 + `CGO_ENABLED=0 go test -count=1 ./...` 全绿（含新增 TDD 测试）
- [ ] T023 [P] adapter 无关确认：`git diff --name-only -- mcpserver` 为空；引擎 diff guard（`git diff --name-only -- memory embedding provider store internal` 仅含本 feature 新增文件）
- [ ] T024 [P] 文档收尾：更新 `spec.md` 的 Clarifications（补验证结果）、`research.md` 的 Residual Risks（补实测），确认 `tasks.md` 全部勾选与 spec/plan 一致
- [ ] T025 [P] 归因提交核查：机制代码、协议绑定、eval 配置为三个独立 commit（宪法 IV 归因）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，可立即开始
- **Foundational (Phase 2)**: 依赖 Setup；**阻塞全部 US**（T003 → US3；T004/T005 → US1）
- **US1 (Phase 3)**: 依赖 T004/T005；不依赖 US2
- **US2 (Phase 4)**: 不依赖 US1/US2 之外（用现有 projection/lineage）；可与 US1 并行
- **US3 (Phase 5)**: 依赖 US1+US2 实现完成 + T003 mechanism 框架
- **Polish (Final Phase)**: 依赖所有 US

### User Story Dependencies

- **US1 (P1)**: Foundational 后即可开始；与 US2 并行
- **US2 (P1)**: Foundational 后即可开始；与 US1 并行（不同文件：projection.go vs pipeline.go）
- **US3 (P1)**: 依赖 US1+US2 完成（四臂需要两个机制都可开关）

### Within Each User Story

- 测试 MUST 先写、先红，再实现
- 判定接口/查询方法 → 机制实现 → 审计/扩展接入
- US 完成（各自 checkpoint 可独立验证）再进下一个

### Parallel Opportunities

- Foundational: T003（harness）与 T004/T005（engine）可并行 [P]
- US1 与 US2 可并行（不同文件、无依赖）
- 各 US 的测试任务 [P] 可并行
- US3 的 manifest 冻结（T018）可与其他 US 的收尾并行

---

## Parallel Example: 先并行打通 US1 与 US2

```bash
# US1: 先写四条失败测试，再实现
Task: "T006-T009 离线测试（storeFact 抑制 / 关闭零变化 / 离线路径 / 冲突保留）"
Task: "T010 接入 ShouldSuppress → storeFact 创建前抑制"

# US2: 先写三条失败测试，再实现
Task: "T013-T015 兄弟扩展测试（共享 evidence / 无邻居 / 有界）"
Task: "T016 ProjectionStore 兄弟查询 + T017 materialize 接入"
```

---

## Implementation Strategy

### MVP First (US1 Only)

1. Setup（T001-T002）
2. Foundational：T003（mechanism 框架）+ T004/T005（判定接口 + 离线实现）
3. US1（T006-T012）→ 独立验证（抑制开/关）
4. **STOP & VALIDATE**: LoCoMo 单臂（write_dedup only）跑方向确认
5. 有方向收益再进 US2/US3

### Incremental Delivery

1. Setup + Foundational → 框架就绪
2. US1 → 验证 → 方向确认
3. US2 → 验证
4. US3 → 双基准四臂 + 报告
5. 负收益机制如实归档，不进入默认路径

### 归因提交策略（宪法 IV）

- commit 1: Foundational 机制框架（T003-T005，engine + harness 框架）
- commit 2: US1 实现（T010-T012，engine 机制）
- commit 3: US2 实现（T016-T017，engine 机制）
- commit 4: US3 eval 配置（T018 manifest）+ 报告（T019-T021）
- 机制代码与 eval 配置**绝不混提交**（plan Submission 节）

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 本项目 TDD 为硬规则：每个机制实现前 MUST 有失败测试
- 每任务完成即 commit；提交遵守归因纪律
- Stop at checkpoint（US1 后、US3 前）验证方向再继续
