# Feature Specification: 写入侧事件抽取训练化

**Feature Directory**: `specs/028-write-side-event-training`

**Created**: 2026-08-05

**Status**: **Closed（NO-GO 收口，2026-08-06）**——US1 教师零训练验证 GO，US2 训练复测 NO-GO（008 铁律未转化），US3 不再执行。实际结果见下方「实际收口」。

**Input**: 走 spec 规范开 SaaS 线——训练抽取器做写侧结构（SaaS 线第一步）

## 实际收口（2026-08-06，NO-GO）

本 feature 已按门禁执行完并收口为 **NO-GO**。阶段判定与证据如下：

- **US1（教师零训练验证，GO）**：DeepSeek-flash 教师抽取时间锚定 **86.4%**（vs 7B 5%），84 题配对 event 44.0% vs chunk 50.0%（−6.0pp，p=0.44）——假设「抽取能力是瓶颈」成立，且训练可解被零成本坐实。见 [diagnosis/us1-verdict.md](diagnosis/us1-verdict.md)。
- **US2（训练抽取器 + 复测，NO-GO）**：Qwen2.5-3B-LoRA 训练（5313 条教师数据，3 epochs，loss 1.32→0.44，全本地栈 $0），T013 审计全过——时间锚定 **96.9%**（≥70 门）、schema 合法 **100%**（≥95 门）、非法时间戳 0（≤5% 门）；但端到端配对 **chunk 50.0% vs event 48.8%（−1.2pp，McNemar p=1.00）未转化**——008 铁律唯一 GO 门（event ≥ chunk）未达成。temporal +3.3pp（52.5 vs 49.2，p=0.81）首次转正但噪声内；multi-hop −12pp（40.0 vs 52.0，p=0.51）倒退。三臂差距收窄链 **−26.2(7B) → −6.0(teacher) → −1.2(trained) pp**。完整见 [docs/evaluation/reports/028-write-side-training-verdict.md](../../docs/evaluation/reports/028-write-side-training-verdict.md)。
- **US2 失败机制**：训练抽取器在中间指标彻底解决 027 瓶颈（锚定 96.9%），但端到端仍不转化——写侧 event 表示替代原文 chunk 的固有损耗（原文保真丢失）未被关系/时间结构补偿。**训练无法超越蒸馏上限 = 教师**（教师自身 −6.0pp 未转化）。写侧 event 表示**第三次端到端不转化**（027 / 028-US1 / 028-US2）。
- **US3（部署接入）不再执行**：US2 未转化即 STOP（008 铁律），default-off 维持现状，无需额外接入。
- **出货影响（FR-010 延续）**：event 投影 / `--representation event` 保持 **default-off**；训练抽取器（`train_sft.py`/`train.sh`/`export_deploy.sh`，含 transformers 5.x 兼容修复）作为 **SaaS 线能力资产**记录保留。tasks.md 已如实收口。

## 阶段化（门禁驱动，不盲烧）

027 实测证明：**7B 无训练抽取丢绝对时间锚定**（5870 事件仅 5% 带绝对时间，47/63 错题 predicted 是相对时间词），写侧 event 结构端到端 −26.2pp（p=0.0016）。但 arXiv 核实（AtomMem）证明**训练级抽取器把时间锚定当显式训练目标后，temporal 类目 +31.1pp**。

本 feature 验证一件事：**训练能否解掉 027 的失败点**。全程守 008 铁律（端到端转化才算 GO）；SaaS 线允许训练/托管，但**分数单独口径声明，不回填为本地涨点**（死亡规则不变）。

## Background and Scope

### 问题

写侧 event 结构（SEGTREEMEM/StructMem 借鉴）的价值被抽取器能力卡死：结构本身合理，但 7B 无训练把绝对日期泛化成相对词。本地铁律（无训练、纯 Go）堵死了"训练抽取器"这条路，所以这条线属于 **SaaS 线**（允许训练算力，不受本地 DEATH RULE 约束）。

### 目标

产出**"时间锚定 + 原子事实"的训练级写侧抽取能力**，并验证它能否把写侧 event 结构拉回 ≥ chunk 基线。若验证通过，为 SaaS 线写侧结构落地打底，也是后续 agentic 检索的写侧地基。

### 范围边界

- **In scope**：教师抽取器零训练验证瓶颈（US1）、训练时间锚定抽取器（US2）、部署接入（US3）
- **Out of scope**：agentic 检索/推理（MRAgent 多步遍历、SLEUTH 推理状态，后续独立 feature）；产品化接入垂直场景（车机/手机，后续 feature）；本地评测可信度收尾（LongMemEval 复跑、judge audit，并行独立工作）

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 教师抽取器零训练验证"能力是瓶颈"（Priority: P1）

先不花钱训练，用**强模型教师抽取器**（托管 API，单次调用）重跑 027 配对，验证"抽取能力是写侧结构成败的瓶颈"这个假设是否成立。

**Why this priority**: 最省钱的假设检验。如果强模型抽取 + 时间锚定都拉不平 event vs chunk，那 027 失败不只是能力问题，训练也不值得投——直接 STOP，省掉整个训练成本。如果拉平/超越，则"训练可解"被零成本坐实，再投训练。

**Independent Test**: 复用 027 的 harness（`--build-event-project` + `--representation event`）只换抽取 LLM 为教师模型，同一 84 题子集、同 answerer/judge/token cap、3 reps majority 配对 event vs chunk。

**Acceptance Scenarios**:

1. **Given** 教师抽取完成、时间锚定率相对 7B 显著提升（5% → ≥50% 绝对提升），**When** 重跑 84 题配对，**Then** event 臂 majority ≥ chunk 臂 或差距大幅收窄（从 −26.2pp 收窄到 ±10pp 内）→ 假设成立，GO 进 US2
2. **Given** 教师抽取完成，**When** 重跑配对仍大幅落后 chunk（差距 ≥ −10pp），**Then** 瓶颈不只是抽取能力 → STOP，记录负结论，不投训练
3. **Given** 教师抽取因模型/成本不可行，**When** 无法获得高质量时间锚定抽取，**Then** 记录降级结论，STOP

---

### User Story 2 - 训练时间锚定抽取器（Priority: P1）

US1 成立后，构建训练数据（教师标注 + 人工精修），训练一个小规模抽取器（时间锚定 + 原子事实），**用它重跑 027 配对**验证端到端转化。

**Why this priority**: 这是 SaaS 线写侧结构的地基。训练成功后，写侧 event 结构才有"能用的抽取器"，也是后续 agentic 检索/产品化的写侧依赖。

**Independent Test**: 训练抽取器跑同一 84 题子集配对；同时报告时间锚定率、schema 合法率、幻觉抽样。

**Acceptance Scenarios**:

1. **Given** 训练完成、时间锚定率 ≥70%、schema 合法率 ≥95%、幻觉率 ≤5%，**When** 重跑 027 配对，**Then** event 臂 majority ≥ chunk 臂（008 铁律，端到端转化）→ GO 进 US3
2. **Given** 训练完成但端到端未转化（event < chunk），**When** 分析失败机制，**Then** 记录 NO-GO 与原因（数据/规模/方法），不进入默认路径，STOP
3. **Given** 训练数据不足或过拟合，**When** 无法达到时间锚定率/合法率门，**Then** 记录数据缺口，评估扩充数据或换方法，不硬上线

---

### User Story 3 - 训练抽取器部署与接入（Priority: P2）

训练后抽取器量化部署（本地 sidecar 或托管），接入 027 已建好的 eventstore 写侧路径，**default-off、单独口径声明**。

**Why this priority**: 训练能力只有落地到写侧路径才有产品价值；但它是 P2——前提是 US2 端到端转化成立。

**Independent Test**: 默认配置下零行为变化（现有本地基线不回归）；开启训练抽取器后，写侧 event 结构可生成可重建投影，分数单独登记。

**Acceptance Scenarios**:

1. **Given** 默认配置，**When** 运行现有评测，**Then** 本地基线（LoCoMo 85.71%）不回归、零行为变化
2. **Given** 开启训练抽取器写侧路径，**When** 生成 event 投影，**Then** 投影可重建（027 eventstore 契约），且该配置分数以单独口径登记，不回填本地

---

### Edge Cases

- 教师标注的**幻觉 / 时间锚定错误**被学习进训练数据 → US2 数据审计必须有教师源 + 人工修订记录
- 训练数据规模不足以覆盖时间表达多样性（"last Friday"、"three weeks ago"、模糊时间）→ 时间锚定率不达标时先扩数据
- 抽取器在**非 LoCoMo 数据**上的泛化性（垂直场景数据未知）→ 训练数据需含多样性来源，过度拟合 LoCoMo 记 NO-GO
- 训练后模型**部署体积/延迟**超出本地 sidecar 约束 → 量化或托管决策在 US3 记录，不默认托管

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 教师抽取器验证（US1）**必须**复用 027 已建的 harness 与配对纪律（同 store/answerer/judge/token cap、3 reps majority、McNemar），只换抽取 LLM，不改引擎
- **FR-002**: 训练数据（US2）**必须**可审计：每条标注带教师源 + 人工修订记录，时间锚定强制相对→绝对
- **FR-003**: 训练产物（US2）**必须**可复现：数据版本、模型、超参、随机种子全部记录
- **FR-004**: 部署接入（US3）**必须**默认关闭、零行为变化；开启后才生成写侧 event 投影
- **FR-005**: 全程**必须**守配对纪律与单独口径：SaaS 线分数单独登记，不回填为本地涨点
- **FR-006**: 每一阶段产出**必须**记录 verdict（GO/NO-GO + 依据）到 tracked docs，不只进本地记忆

### Key Entities *(include if feature involves data)*

- **写侧抽取器训练集**: 对话语料 + 教师抽取的原子事实/双视角事件 + 强制绝对时间锚定 + 人工修订标记
- **时间锚定率**: 抽取事件中带可靠绝对时间戳的比例（027 基线 5%，目标 ≥70%）
- **训练级抽取器**: 训练产出的模型（规模、量化、部署形态在 US3 决定）
- **配对分数表**: 每阶段 event vs chunk 的 majority 结果 + McNemar p 值（单一真相来源）

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: US1 教师抽取器时间锚定率相对 7B（5%）有**可测提升**（≥50 绝对百分点），且 84 题配对 event 臂与 chunk 臂差距收窄到 ±10pp 内
- **SC-002**: US2 训练抽取器时间锚定率 **≥70%**、schema 合法率 ≥95%、幻觉率 ≤5%
- **SC-003**: US2 端到端配对 event 臂 **≥ chunk 臂**（008 铁律，唯一 GO 门）
- **SC-004**: US3 默认配置本地基线（LoCoMo 85.71%）**不回归**；开启配置分数单独口径登记

## Assumptions

- **SaaS 线允许训练/托管算力**，不受本地 DEATH RULE（无训练/纯 Go/不付费云）约束；但分数单独口径，不回填为本地能力
- 训练抽取器默认部署为**本地 sidecar（量化）**，托管为可选——engram 引擎保持 local-first，SaaS 只放宽训练侧
- 训练算力（如 AutoDL 租用）计入 SaaS 线成本，须遵循"空闲必停"与盘位纪律
- **agentic 检索/推理是后续独立 feature**，不在本 spec 范围；本 feature 只做写侧抽取能力
- 教师模型用现有可用的托管 API（DeepSeek 系），具体选型与成本在 plan 阶段定
- 训练数据以 LoCoMo 对话为起点，必要时补充垂直场景数据（成本允许）
