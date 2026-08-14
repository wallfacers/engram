# Tasks: 自适应检索深度

**Input**: Design documents from `specs/040-adaptive-topk-dedup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: gap-knee 检测是纯 Go 确定性算法，按项目「测试先行」惯例写单测（便宜且必要，防回归）。

**Organization**: 任务按 user story 编排。**US2 对 US1 存在条件依赖**：US2 实现前必须通过 US1 的诊断判定（T006），这是「不把现有 91.10% 分数整没」的核心安全网。

## Phase 1: Foundational（核心算法，US1/US2 共用）

**Purpose**: gap-knee 检测纯函数是诊断与自适应共同依赖的核心，必须先落地并单测。

- [ ] T001 写 gap-knee 检测单测（先失败）：在 `cmd/locomo-bench/adaptive_topk_test.go` 覆盖「离散分数序列的拐点定位」「无拐点回退 fallback」「minK clamp」「空序列/单元素序列退化」「仅单一信号（keyword）的短/稀疏分数序列（信号降级场景，FR-007）」五类 case
- [ ] T002 实现 gap-knee 检测纯函数：在 `cmd/locomo-bench/adaptive_topk.go` 实现 `detectAdaptiveTopK(scores []float64, minK, fixedTopK int) (adaptiveTopK, kneeIndex int, fallback bool)`，含归一化 → 相邻 gap → 最大拐点 → clamp，遵循 research.md Decision 1

**Checkpoint**: `CGO_ENABLED=0 go test ./cmd/locomo-bench -run AdaptiveTopK` 全绿

---

## Phase 2: User Story 1 - 零成本诊断（Priority: P1）🎯 MVP

**Goal**: 交付 headroom 诊断命令，逐题产出「gold 在宽池 rank + 自适应 k* + 是否丢 gold」及汇总指标，证明「缩减深度不丢关键证据」前提是否成立。

**Independent Test**: 本地跑 `--adaptive-topk-diagnose`（无 answerer/judge 调用），产出 `adaptive-headroom.jsonl` + `adaptive-headroom.json`，读 `dropped_gold_ratio` 与 `knee_rate` 判定（见 quickstart 阶段 0）。

- [ ] T003 [US1] 实现 headroom 诊断命令：在 `cmd/locomo-bench/adaptive_diagnose.go` 复用 `retrieveWithQuotaDiagnostics` 的宽池结果（`[]memory.Result` 带 Score）与 attribution 的 `gold_rank_pool`，对宽池 Score 序列跑 `detectAdaptiveTopK`，逐题计算 `dropped_gold`，汇总写 JSONL + JSON（schema 见 contracts/cli-adaptive-topk.md）
- [ ] T004 [P] [US1] 写诊断汇总单测：在 `cmd/locomo-bench/adaptive_diagnose_test.go` 覆盖 `dropped_gold` 判定（`adaptive_topk < gold_rank_pool`）与汇总统计（`dropped_gold_ratio` / `knee_rate` / `mean_budget_reduction`）
- [ ] T005 [US1] 注册 flag：在 `cmd/locomo-bench/main.go` 新增 `--adaptive-topk-diagnose`（bool，默认 false），并校验与 `--coverage-only` / `--attribution-trace` 等检索-only 模式的互斥关系
- [ ] T006 [US1] 跑阶段 0 诊断：本地 store + `locomo.json` + `--chunks --chunk-quota 12 --top-k 150 --adaptive-topk-diagnose`，产出 `adaptive-headroom.json`，记录 `dropped_gold_ratio` / `knee_rate`，对照 contracts 判定线给出 GO/NO-GO；并按 gold-rank 分布冻结 `--adaptive-min-k` 默认值（回填 contracts/cli-adaptive-topk.md 与 data-model.md 的「待冻结」处）

**Checkpoint**: US1 完成，诊断判定出具 —— **若 NO-GO，feature 在此 STOP，不进入 Phase 3**；若 GO，进入 Phase 3。

---

## Phase 3: User Story 2 - 自适应检索深度（Priority: P1，条件依赖 T006 GO）

**Goal**: 交付 opt-in、默认关闭的 `--adaptive-topk`，在 harness 层 per-query 决定 k*，替代固定 top-k。

**Independent Test**: 端到端配对（control 臂 vs adaptive 臂），验证「关闭时逐字节一致」+「开启时平均证据消耗下降且正确率不显著回退」（见 quickstart 阶段 1）。

**前置条件**: T006 诊断判定为 GO。

- [ ] T007 [US2] 注册自适应 flag：在 `cmd/locomo-bench/main.go` 新增 `--adaptive-topk`（bool，默认 false）与 `--adaptive-min-k`（int，默认待 T006 后冻结）
- [ ] T008 [US2] 插入自适应 k*：在 `cmd/locomo-bench/chunks.go` 的 `retrieveWithQuotaDiagnostics` 中，宽池检索后、`applyChunkQuota` 前，当 `--adaptive-topk` 开启时调用 `detectAdaptiveTopK` 得到 k* 并作为 topK 传入；flag off 时路径不触达（保证 FR-001 字节一致），quota 恒不变（FR-003）
- [ ] T009 [P] [US2] 写集成级单测：在 `cmd/locomo-bench/adaptive_topk_test.go` 覆盖「flag off 时结果与固定深度一致」「开启时 quota 不变、facts 槽位 = k* − quota」「fallback 时 k* = fixedTopK」
- [ ] T010 [US2] 跑阶段 1 端到端配对：control 臂（`--top-k 150 --chunk-quota 12 --chunks`）vs adaptive 臂（同 recipe + `--adaptive-topk`），3-rep 多数票 + clean 重判，配对 McNemar + 类别非回归，验证不显著回退（宪法 IV）

**Checkpoint**: US2 完成，配对结果出具；不显著回退则可作为转正候选（转正是独立决策）。

---

## Phase 4: Polish & Cross-Cutting Concerns

**Purpose**: 收尾验证与文档。

- [ ] T011 [P] 写 verdict 报告：把诊断 + 配对的最终结论写入 `docs/evaluation/reports/040-adaptive-topk-verdict.md`；若转正，同步更新 `docs/evaluation/results.md`
- [ ] T012 最终验证：`CGO_ENABLED=0 go build ./...` 零错误 + `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 全绿 + `git diff --name-only -- memory embedding provider store internal` 为空（引擎零改动）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Foundational (Phase 1)**: 无依赖，可立即开始
- **US1 诊断 (Phase 2)**: 依赖 Phase 1 的 gap-knee 检测
- **US2 自适应 (Phase 3)**: 依赖 Phase 2 的 T006 诊断判定为 **GO**（条件依赖，非无条件）
- **Polish (Phase 4)**: 依赖 US1（及若实现的 US2）

### 关键条件依赖

- **T006 → Phase 3**：诊断是「生死前提」。若 `dropped_gold_ratio` 超阈值或 `knee_rate` 过低，feature 在 US1 之后 STOP，Phase 3/4 的 US2 任务作废。这保证「自适应收缩」绝不会在未证明安全的前提下触碰检索路径。

### Parallel Opportunities

- Phase 1 内：T001（测试）与 T002（实现）按 TDD 顺序串行（测试先失败）。
- Phase 2 内：T004（诊断单测）可与 T003（诊断命令）部分并行（不同文件）。
- Phase 3 内：T009（集成单测）可与 T007/T008 并行（不同文件）。
- 跨 phase 不可并行：US1 与 US2 是条件串行，这是本 feature 的安全网设计，不是可并行的独立 story。

## Implementation Strategy

### MVP First（US1 诊断先行）

1. Phase 1（gap-knee 算法 + 单测）→ 2. Phase 2（US1 诊断）→ 3. **STOP 并判定**：诊断出具 GO/NO-GO → 4. 只有 GO 才做 Phase 3。

这是本 feature 的核心策略：**先花近零成本（无 answerer/judge）证明「缩减不丢 gold」，再决定是否实现自适应**——把「会不会把分数整没」这个风险用数据提前回答，而不是实现后再试错。

### 增量交付

- Phase 1 + 2 → 诊断能力（可独立交付、可复用）
- Phase 3（条件）→ 自适应旋钮（opt-in，默认关）
- Phase 4 → verdict 记录 + 门禁验证

---

## Notes

- 引擎（`memory/ embedding/ provider/ store/ internal/`）**零改动**，所有改动在 `cmd/locomo-bench/`。
- 每完成一个逻辑组 commit 一次；T006 与 T010 是评测任务，跑在本地（诊断）与评测 box（端到端配对，需 Qwen + judge）。
- 若 T006 NO-GO，保留 `adaptive_topk.go` 的 gap-knee 算法与诊断命令（它们是可复用的诊断资产），仅 US2 的 harness 插入（T008）不落地。
