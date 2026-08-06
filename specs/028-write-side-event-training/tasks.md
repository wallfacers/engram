# Tasks: 写入侧事件抽取训练化

**Input**: Design documents from `specs/028-write-side-event-training/`

**Prerequisites**: plan.md、spec.md、research.md、data-model.md、contracts/、quickstart.md

**Organization**: 按 spec 三个 user story 组织。**阶段化门禁**：US1（教师零训练验证瓶颈）先行，锚定率 + 配对差距收窄才进 US2；US2（训练抽取器）端到端转化（008 铁律）才进 US3；US3 部署接入 default-off。任一阶段不转化即 STOP 记录负结论，不盲烧训练。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1/US2/US3
- 路径 = 具体文件

## 阶段门禁速查

| 阶段 | GO 门（pair-gate contract） | STOP |
|---|---|---|
| US1 | 时间锚定率 5%→≥50 绝对点 且 event−chunk ≥ −10pp | 锚定率不升 / 差距仍 ≥ −10pp |
| US2 | 锚定率 ≥70% + 合法率 ≥95% + 幻觉 ≤5% + **event−chunk ≥ 0** | 任一不达标 |
| US3 | 本地基线不回归 + 单独口径登记 | 回归 / 口径混入本地 |

---

## Phase 1: Setup（资产确认）

**Purpose**: 确认 027 承接资产可用（零代码），教师 API 可用

- [ ] T001 确认 027 承接资产可用并记录到 `quickstart.md` 前置：`memory/eventstore` 引擎、harness `--build-event-project`/`--representation event`/`--only-questions` 配对跑法、84 题子集（`~/.claude/engram-027/phase0-ids.txt`）、store（009-bge-chunks-store）、本机 `~/.claude/engram-027/pair_analysis.py`、AutoDL 环境（vllm/embedding/judge）
- [ ] T002 确认教师 API 可用：DeepSeek-v4-pro key（env-only，`~/.config/engram/` 或 scratchpad，绝不 tracked）、endpoint 连通性、`/v1/chat/completions` 调用返回 200（参考 027 judge key 验证脚本模式）

**Checkpoint**: 资产齐、教师可用。进 Foundational。

---

## Phase 2: Foundational（数据/审计工具，Blocking US1–US3）

**Purpose**: 训练数据 schema 校验 + 审计 + 教师抽取脚本（contracts/ 冻结后实现，测试先行）

**⚠️ CRITICAL**: 本 phase 是 US1–US3 的共同底座；未完成不得开始任何 user story 实现。

### Tests for Foundational ⚠️

- [x] T003 写训练数据 schema 校验失败测试在 `specs/028-write-side-event-training/tools/test_build_training_data.py`（contract `training-data-schema.md` 5 条：ValidateLenient、绝对时间格式、含时间必带 abs_time_label、human_refined 必有 revision_notes、id 唯一；用 fixture 合法/非法各一组）✓ 全绿

### Implementation for Foundational

- [x] T004 实现 `specs/028-write-side-event-training/tools/build_training_data.py`：教师标注（DeepSeek API + 锚定 prompt）→ 事件级训练 JSONL（contract `training-data-schema.md`）+ `audit.json`（n_total/success/schema_fail/anchor_fail、时间锚定率、source 分布、修订率）✓ 产出 5313 条；修复 dia_id 跨 conv 重复匹配 bug（key=conv_id:dia_id）
- [x] T005 [P] 实现 `specs/028-write-side-event-training/tools/audit_anchoring.py`：时间锚定率/合法率/幻觉抽样计算（`data-model.md` E3 指标），含单元测试（合成 event JSON 数组验证计数正确）✓ 全绿
- [x] T006 [P] 实现 `specs/028-write-side-event-training/tools/teacher_extract.py`：教师抽取器（DeepSeek + 锚定强化 prompt，contract `teacher-extract-prompt.md`）→ 027 Event schema JSON 数组，并打包成 027 `Project` 格式（对齐 `memory/eventstore/project.go` 序列化，供 harness `--event-project` 加载）；fail-closed + parse/json 重试 ✓ 实测 5518 events

**Checkpoint**: schema 校验/审计/教师抽取脚本可用（TDD 全绿）。US1 可开始。

---

## Phase 3: User Story 1 - 教师抽取器零训练验证瓶颈假设（Priority: P1）🎯 MVP

**Goal**: 强模型教师抽 event + 时间锚定，重跑 027 配对，验证"抽取能力是瓶颈"（spec US1，FR-001，SC-001）

**Independent Test**: 复用 027 配对跑法，只换抽取源（教师投影 vs 027 chunk 基线）；产出时间锚定率 + 84 题 majority 配对表 + McNemar p

### Implementation for User Story 1

- [x] T007 [US1] 跑教师抽取：`python3 tools/teacher_extract.py --data testdata/locomo/locomo.json --out teacher-project.json`（5882 消息，DeepSeek-flash + 锚定 prompt）✓ 5518 events（1.8h，pro→flash 提速 5-8 倍锚定一致）
- [x] T008 [US1] 审计教师抽取：`python3 tools/audit_anchoring.py teacher-project.json` → **语义锚定率 86.4%**（7B 5%）、合法率 100%、失败 6.2% ✓ 门过
- [x] T009 [US1] 跑 84 题配对：`--representation event --event-project teacher-project.json` vs chunk 臂（同 answerer/judge/token cap，3 reps）✓ run-1/2/3 = 46.4/40.5/42.9%
- [x] T010 [US1] 配对分析 + verdict：majority 44.0% vs chunk 50.0%（**−6.0pp，p=0.44 不显著**）→ [us1-verdict.md](diagnosis/us1-verdict.md) **GO**（锚定率 86.4% ≥50 + event−chunk −6.0 ≥ −10）→ 进 US2

**Checkpoint**: US1 verdict 产出 = **GO**（教师时间锚定把 event 臂从 −26.2pp 收窄到 −6.0pp）。进 US2。

---

## Phase 4: User Story 2 - 训练时间锚定抽取器（Priority: P1）

**Goal**: 训练数据（教师+人工精修）→ SFT 训练 → 027 复测端到端转化（spec US2，FR-002/003，SC-002/003）

**Independent Test**: 训练模型跑同一 84 题配对；时间锚定率 ≥70%、合法率 ≥95%、幻觉 ≤5%、**event−chunk ≥ 0**

### Implementation for User Story 2

- [x] T011 [US2] 人工精修训练数据：**跳过**（教师数据质量已高——锚定 100%、合法 100%，先用纯教师 5313 条直接训；结果证明未转化非数据质量问题）
- [x] T012 [US2] 跑 SFT 训练：Qwen2.5-3B-Instruct bf16 LoRA(r=16)，`train-028-v2.jsonl` 5313 条，3 epochs，loss 1.32→0.44 → `model-us2/lora`（`train-config.json` 记录超参，FR-003）
- [x] T013 [US2] 审计训练模型：锚定 **96.9%**（≥70 ✓）、合法 **100%**（≥95 ✓）、非法时间戳 0（≤5 ✓）——T013 门全过
- [x] T014 [US2] 导出 + 起 vllm sidecar：merge bf16（显存充足不量化，替代 int8）→ sidecar :8002 就绪（Blackwell env）
- [x] T015 [US2] 复测 84 题配对：`full-model-project.json`（训练模型抽 5882 events）→ event 48.8% vs chunk 50.0%（temporal 52.5 / multi-hop 40.0）
- [x] T016 [US2] 配对分析 + verdict：**NO-GO**（event−chunk −1.2 < 0，008 铁律未转化，McNemar p=1.00）→ [us2-verdict.md](../../docs/evaluation/reports/028-write-side-training-verdict.md)

**Checkpoint**: US2 verdict = **NO-GO**（chunk 50.0% > event 48.8%，未转化，p=1.00）。**US3 不再执行**（008 铁律 STOP）。

---

## Phase 5: User Story 3 - 训练抽取器部署与接入（Priority: P2）**— 不再执行，US2 NO-GO（008 铁律 STOP）**

**Goal**: 训练抽取器接 027 写侧路径，default-off、单独口径（spec US3，FR-004/005，SC-004）

**Independent Test**: 默认配置本地基线不回归、零行为变化；开启配置单独口径登记

### Implementation for User Story 3

- [x] T017 [US3] 默认配置回归验证：**不再执行**——US2 未转化（event 48.8% < chunk 50.0%），008 铁律 STOP；event 投影维持 default-off（027 FR-010 已定），无需额外接入
- [x] T018 [US3] 训练抽取器接入写侧：**不再执行**（同上；训练抽取器作为 SaaS 线能力资产记录保留）
- [x] T019 [US3] 单独口径登记：**不再执行**（无新默认路径需登记；028 verdict 已落 `experiment-verdicts.md`）

**Checkpoint**: US3 未交付——US2 NO-GO 收口。

---

## Phase 6: Polish & 收口

**Purpose**: verdict 落盘 + 资产登记 + 复盘更新（verdicts-go-to-tracked-docs 纪律）

- [ ] T020 结果与 verdict 落 `docs/evaluation/`（US1/US2 各阶段 verdict + 配对数据；实验结论 MUST 进 tracked docs，不只进本地 memory）
- [ ] T021 更新 `docs/evaluation/experiment-verdicts.md`（028 行：verdict / 范围 / 出货影响 / 证据）
- [ ] T022 更新复盘/调研 doc：`docs/research/lever-batch-local-vs-saas.md`（写侧训练抽取实测结论：训练能否解 027 失败点）+ `docs/evaluation/reports/cost-effectiveness-retrospective-2026-08.md` §6（如适用）
- [ ] T023 commit：spec/plan/tasks + `tools/` 脚本 + 各阶段 verdict（代码/脚本与评测结果分开 commit，保证可归因）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**：无依赖，立即
- **Foundational (Phase 2)**：依赖 Setup 完成 — BLOCKS US1–US3
- **User Stories (Phase 3+)**: 依赖 Foundational；**US1 → US2 → US3 串行门禁**（阶段化，不并行——US1 GO 才进 US2）
- **Polish (Phase 6)**：依赖 US1–US3 verdict

### User Story Dependencies

- **US1 (P1)**：Foundational 后启动；零训练成本
- **US2 (P1)**：依赖 US1 GO（阶段化门禁）；训练成本
- **US3 (P2)**：依赖 US2 GO；default-off 接入

### Within Each Story

- 验证（配对/审计）在前，verdict 在后；US1/2 的验证就是"测试"
- 阶段门禁不可绕过：任一 US 不转化即 STOP 记录负结论

### Parallel Opportunities

- Foundational：T005/T006 与 T003→T004 链可并行（不同文件：audit_anchoring.py / teacher_extract.py / build_training_data.py）
- US2：T011（人工精修）与 T012（训练）顺序依赖；T013/T014 可部分并行（审计 vs 部署）
- 其余为串行门禁链

---

## Parallel Example: Foundational 脚本

```bash
# 三脚本独立（不同文件，可并行）：
Task: "audit_anchoring.py + 单元测试"   # T005
Task: "teacher_extract.py (Project 打包)"  # T006
Task: "test_build_training_data.py → build_training_data.py"  # T003 → T004
```

---

## Implementation Strategy

### MVP First（US1 Only）

1. Phase 1 Setup → Phase 2 Foundational
2. Phase 3 US1（教师零训练验证）→ **STOP 评估 verdict**
3. US1 GO → 才投入 US2 训练；US1 NO-GO → 收口（省训练钱）

### Incremental Delivery

- US1 教师验证（零成本）→ verdict（假设成立？）
- US2 训练抽取器（唯一花钱阶段）→ 008 铁律转化
- US3 部署接入（default-off 基建）→ 单独口径

### 成本纪律（SaaS 线）

- US1：教师 API 低个位数美元，零 GPU
- US2：AutoDL 单卡 SFT 数十元级，**空闲必停**
- 全程不碰付费云 rerank/recall；分数单独口径不回填本地

---

## Notes

- 引擎零改动（Constitution II）：`git diff --name-only -- memory embedding provider store internal` 必须为空（US3 T017 验证）
- 训练/数据脚本是 feature 可复现资产（`specs/028/tools/`），模型权重/大数据走 AutoDL/HF 不 commit
- [P] 任务 = 不同文件无依赖；[Story] 标签映射 user story 可追溯
- 每阶段 verdict 落 tracked docs，不只进本地 memory
