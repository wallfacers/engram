# Tasks: Confidence-Gated Iterative Retrieval

**Input**: Design documents from `/specs/041-confidence-gated-retrieval/`

**Prerequisites**: plan.md（已备）、spec.md（已备）、research.md、data-model.md、contracts/（均已备）

**Tests**: 本 feature 遵循项目宪法「测试先行」——涉及引擎行为/机制的实现先写可失败测试再实现。以下每个实现任务前都有对应的测试任务（TDD）。

**Organization**: Tasks are grouped by user story. US2 严格排在 US1 通过之后（spec US1 为生死前提）；US3 依赖 US2 在真实 run 上产出的数据。

## Format: `[ID] [P?] [Story] Description`

## Path Conventions

- 本 feature 全部代码在 `cmd/locomo-bench/`（eval harness，`package main`），沿用 040 `adaptive_topk.go` 同目录同包模式。
- **引擎零改动门禁**：所有任务完成后 `git diff --name-only -- memory embedding provider store internal` 必须为空（宪法 II）。

---

## Phase 1: Setup

**Purpose**: 本 feature 不新增工程/依赖/存储（纯 harness 内新增 Go 文件），无独立 setup 阶段。直接进入 Foundational。

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 共享类型定义 + flag 接线 + 关闭态黄金测试。这些是 US1/US2/US3 都依赖的底座，先于任何 user story。

**⚠️ CRITICAL**: 无此阶段则 user story 无法开始

- [x] T001 定义共享实体类型（`HesitationSignal` / `signalHit` / `BudgetLadder` / `IterationDecisionRecord` / `CalibrationThreshold`，字段见 data-model.md）于 `cmd/locomo-bench/confidence_gate.go` 与 `cmd/locomo-bench/iterative_retrieval.go`
- [x] T002 [P] 注册新 flags 并加校验（`--confidence-gated` / `--confidence-shallow-k` / `--confidence-deep-k` / `--confidence-threshold` / `--confidence-max-rounds`；约束见 contracts/cli-confidence-gated.md：deep>shallow、禁与 `--multi-query`/`--cat-top-k` 组合、禁与 formal B1 冻结组合、threshold>=0）于 `cmd/locomo-bench/main.go` 与 `cmd/locomo-bench/iterative_retrieval_cli.go`
- [x] T003 [P] 关闭态黄金测试：`--confidence-gated=false` 时本 feature 所有代码路径零字节差异（断言 `detectHesitation`/迭代循环在 gate off 时不被调用，检索/prompt/作答/判题路径与固定 top-k 完全一致，diff `results-hybrid.jsonl` 逐题 predicted/correct）于 `cmd/locomo-bench/iterative_retrieval_test.go`（对齐 `TestAnswerPromptGoldenBaseline` 守护方式）

**Checkpoint**: Foundation ready - 类型、flag、关闭态黄金齐备，user story 可开始

---

## Phase 3: User Story 1 - 犹豫信号检测（Priority: P1）🎯 MVP

**Goal**: 证明「犹豫」能从 answerer 正常生成中**确定性提取**（FR-002/003），且在全量标注集上区分「答对/答错」达到门槛（research Decision 2：答错题犹豫率 ≥60%、答对题假阳性 ≤30%）。这是整个 feature 的生死前提，**不达标即停线**（spec US1 Acceptance 3）。

**Independent Test**: 在既有 run（top-k 30/150）的 `results-hybrid.jsonl` 上跑 `--probe-hesitation`，产出混淆矩阵 + 门槛判定；不依赖迭代实现。

### Tests for User Story 1（TDD：先写，先 FAIL）⚠️

- [x] T004 [P] [US1] 检测器确定性 + 规则边界单测（同一 predicted → 恒定 `(hesit, deepened)`；空文本/无 thinking/拒答/多候选列举/猜测语气各用例；`extractFinalAnswer` 前后分跑规则）于 `cmd/locomo-bench/confidence_gate_test.go`

### Implementation for User Story 1

- [x] T005 [US1] 实现 `detectHesitation(pred)` + `signalHits(pred)`（强/中/弱加权规则集 + 强度分段，复用 `isIDK` 与 `extractFinalAnswer`；纯文本纯函数，无模型调用）于 `cmd/locomo-bench/confidence_gate.go`
- [x] T006 [US1] 实现 `--probe-hesitation` 离线诊断（读已有 `results-hybrid.jsonl` 的 `correct/gold/predicted` → 逐题 `detectHesitation` → 2×2 混淆矩阵 + 答错题犹豫率/答对题假阳性率 + 门槛 PASS/FAIL 判定）于 `cmd/locomo-bench/iterative_retrieval_cli.go`
- [x] T007 [US1] 在真实 run 数据上跑区分度验证：复用已有 top-k 30/150 run 的 `results-hybrid.jsonl`（远程只读或本地既有），确认门槛达标并记录数值到 `docs/evaluation/reports/041-confidence-gated-verdict.md`；**不达标 → 停止，不进 US2**。**结果：边缘 GO（flash recall 58–63%，2/3 rep 达标），但 040 的「89% 犹豫」确认为人工宽松判读高估。**

**Checkpoint**: US1 通过（区分度达标）才允许进入 US2

---

## Phase 4: User Story 2 - 置信度门控迭代检索（Priority: P1）

**Goal**: 浅检索作答 → 犹豫加深 → 重答，以「读到足够就停」替代固定 top-k 150（FR-001/004/005/008）。默认关闭，关闭时逐字节一致（SC-003，已由 T003 守护）。

**Independent Test**: 迭代 vs 固定 top-k 150 配对评测（3-rep 多数票，同 judge），正确率无统计显著回退 + 平均输入 token 显著下降（SC-002）。

### Tests for User Story 2（TDD：先写，先 FAIL）⚠️

- [x] T008 [P] [US2] 迭代循环流程单测：stub answerer 返回可控文本（自信 → 停浅轮、犹豫 → 加深重答、两轮后超限即停）、验证 `IterationDecisionRecord` 字段正确、深轮 hits 来自 `DeepTopK` 检索于 `cmd/locomo-bench/iterative_retrieval_test.go`

### Implementation for User Story 2

- [x] T009 [US2] 实现 `runConfidenceGatedQuestion(ctx, retriever, qa, opt, answerCall, judgeCall)`：浅轮 `retrieveWithQuotaDiagnostics(ShallowTopK)` → `buildAnswerContextPrompt` → answer → `detectHesitation` → 自信停/犹豫深轮（`DeepTopK` 重答）；FR-005（无 thinking 回退固定深度语义）、FR-004（maxRounds=2）于 `cmd/locomo-bench/iterative_retrieval.go`（复用既有函数不改签名）
- [x] T010 [US2] 实现 `evaluateConfidenceGated(opt, runDir)` 批量入口：逐题循环、`results-hybrid.jsonl` + `conf_gate_decisions.jsonl` 落盘、`summary`（总正确率/加深率/平均输入 token/平均证据条数，token 用现有 `token_counter`）于 `cmd/locomo-bench/iterative_retrieval_cli.go`
- [x] T011 [US2] 迭代 vs 基线配对评测（宪法 IV 门禁）：迭代（`--top-k 30 --confidence-gated`）vs 固定 `--top-k 150`，3-rep 多数票配对比较正确率（无显著回退）+ 加深率 + 平均输入 token；结果与结论写入 `docs/evaluation/reports/041-confidence-gated-verdict.md`。**VERDICT: NO-GO** —— Qwen iter 88.8% < k150 90.1%（加深净负 rescued2/harmed4）、unified U-iter < U-k30=k150、deepseek-v4-flash recall 50% 不达 60% 门槛。详见 verdict 文档。

**Checkpoint**: US2 配对评测完成，正确率门禁通过/未通过均记录 verdict

---

## Phase 5: User Story 3 - 阈值校准（Priority: P2）

**Goal**: 把犹豫强度阈值校准到「加深率/正确率/预算」的稳健平衡（research Decision 2 后冻结默认 `--confidence-threshold`；规避 calibration failure——When Should Active RAG Retrieve 警告）。

**Independent Test**: 用 held-out 数据子集 sweep 阈值，加深率/平均预算/正确率三者同时满足目标区间；可独立于 US2 核心循环评估。

### Implementation for User Story 3

- [x] T012 [US3] 实现 `--confidence-calibrate` 阈值 sweep 工具（读 `conf_gate_decisions.jsonl` 或 `results-hybrid.jsonl`，对阈值区间 sweep `Score` 门槛 → 加深率/正确率/平均预算曲线输出）于 `cmd/locomo-bench/iterative_retrieval_cli.go`
- [x] T013 [US3] 在真实 run 数据上校准阈值并冻结默认值：更新 `main.go` 中 `--confidence-threshold` 默认、`contracts/cli-confidence-gated.md` 与 `research.md` Decision 2 中的门槛记录；记录校准结果到 verdict 文档。**冻结 3.0（US1 2/3 run PASS 带最优平衡点）；deepseek 校准确认无 threshold 同时达标 recall≥60%+fp≤30%。**

**Checkpoint**: 阈值冻结，机制可用性（不普遍加深/不普遍不加深）有实证支撑

---

## Phase N: Polish & Cross-Cutting Concerns

**Purpose**: 收尾——文档、验证、引擎零改动确认

- [x] T014 [P] 更新 `cmd/locomo-bench/README` 或 `docs/` 中 locomo-bench flags 说明（新增 5 个 `--confidence-*` flag + `--probe-hesitation` + `--confidence-calibrate`）
- [ ] T015 全量验证：`CGO_ENABLED=0 go build ./...` 零错误 + `CGO_ENABLED=0 go test -count=1 ./...` 全绿 + `git diff --name-only -- memory embedding provider store internal` 为空（宪法 II 硬门禁）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无（本 feature 跳过）
- **Foundational (Phase 2)**: T001→T002/T003（T002/T003 可并行，不同文件）——BLOCKS 所有 user story
- **User Stories (Phase 3+)**: 串行——**US2 严格在 US1 通过之后**（spec 生死前提）；US3 在 US2 真实数据之后
- **Polish (Final Phase)**: 依赖全部 user story

### User Story Dependencies

- **US1 (P1)**: 依赖 Foundational（T001 类型、T006 用 T004/T005）
- **US2 (P1)**: **依赖 US1 区分度达标（T007）**；实现依赖 T001/T002/T009
- **US3 (P2)**: 依赖 US2 的 `conf_gate_decisions.jsonl`（T010 产物）

### Within Each User Story

- 测试（T004/T008）先写、先 FAIL，再实现（T005/T009）
- 实现 → 真实数据验证（T007/T011/T013）

### Parallel Opportunities

- T002 [P] 与 T003 [P]（不同文件：cli 接线 vs golden 测试）
- T004 [P]（US1 测试）可与 T001 并行（不同文件）
- T008 [P]（US2 测试）依赖 T001 类型定义后可写，可与 T005/T006 并行（不同文件）
- 各 user story 间不可并行（US2 依赖 US1 通过、US3 依赖 US2 数据）

### Parallel Example

```bash
# Foundational 并行（T002、T003 不同文件）：
Task: "注册 flags + 校验于 main.go / iterative_retrieval_cli.go"
Task: "关闭态黄金测试于 iterative_retrieval_test.go"
```

---

## Implementation Strategy

### MVP First（US1 Only）

1. 完成 Phase 2: Foundational（T001-T003）
2. 完成 Phase 3: US1（T004-T007）
3. **STOP and VALIDATE**: `--probe-hesitation` 区分度达标？不达标 → 停线（verdict 记录 NO-GO，机制不可行）
4. 达标 → 可先产出 US1 中间 verdict（信号存在性坐实）

### Incremental Delivery

1. Foundational → 关闭态黄金确认（零侵入）
2. US1 → 犹豫信号确定性 + 区分度验证（MVP：机制前提坐实）
3. US2 → 迭代循环 + 配对评测（核心价值：省预算不降分）
4. US3 → 阈值校准（可用性优化）
5. Polish → 文档 + 全量验证 + 引擎零改动确认

### 运行约束（关键）

- US1/US2/US3 的真实数据验证均在**既有或新增 run 上**跑，需要 answerer + judge 端点（远程 vllm box，AutoDL）；US1（T007）优先复用**已有** run 的 `results-hybrid.jsonl`（零新模型成本）
- 全量跑分按 CLAUDE.md「Long-Running Commands on WSL2 — MUST Detach」用 `setsid` 后台 + poll；AutoDL box 用完关机（维护者已授权）
- 评测口径改动（新 flag）与算法改动分开提交（宪法 IV）

## Notes

- [P] tasks = 不同文件、无依赖
- [Story] label 映射到 spec 的 user story
- 每个 user story 独立可完成、可测试
- 测试先 FAIL 再实现
- 每逻辑组提交一次；041 worktree 分支 `worktree-041-confidence-gated-retrieval`
- 避免：模糊任务、同文件冲突、破坏 US 独立性的跨 story 依赖
