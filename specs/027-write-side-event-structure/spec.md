# Feature Specification: 写入侧事件时序结构记忆

**Feature Branch**: `027-write-side-event-structure`

**Created**: 2026-08-05

**Status**: Draft

**Input**: User description: "方向 B——写入侧 event-centric 时序结构记忆：借鉴 SEGTREEMEM（时间有序 segment tree）与 StructMem（事件双视角抽取 + 周期性跨事件合并）的干净消融证据，验证在 engram 本地口径（纯 Go + 本地 sidecar，不付费云）下，把『存文字片段』改为『写入时按事件组织、保留时间与关系』能否端到端提升 temporal + multi-hop。来源：docs/evaluation/reports/cost-effectiveness-retrospective-2026-08.md 第 6 节调研。"

## Clarifications

### Session 2026-08-05（承接决策）

- **承接 021 的收敛结论**：021 已证伪 6 次 temporal 检索/答题杠杆，收敛出「temporal 真差距在写入侧结构化记忆（tree/graph）」。本 feature 是这一结论的**首个写入侧实现验证**，不是检索侧再试一轮。
- **借鉴证据（alphaXiv 精读，2026-08-05 落盘于复盘 doc 第 6 节）**：[SEGTREEMEM](https://www.alphaxiv.org/abs/2606.04555) 证明 temporal 有序 segment tree 相对非时序聚类树一致更好（置换 30% turn 对掉 0.111 vs 非时序 0.020）；[StructMem](https://www.alphaxiv.org/abs/2604.21748) 证明 event 双视角 + 周期性合并相对 flat/实体图在 temporal 上 81.62 vs 78.50 vs 76.64（post-hoc 实体图反而更差）。**两篇共同且与 engram 既有证据（Mem0g multi-hop 有害、014/021）精确对齐：post-hoc 实体-关系图不涨点，写入侧 event-centric 时序层级才涨点。**
- **与已证伪方向区分**：本 feature **不做** post-hoc 实体图（Mem0g/014 已证伪）、不做检索侧杠杆（021 已穷尽）、不做 LLM 融合写回（022 已证伪）。本 feature 的唯一改动点是**写入侧表示**：原始对话 → 时间锚定的事件条目（事实 + 关系双视角），并按需合并为跨事件关系摘要。
- **LLM 写入侧约束**：event 抽取需要 LLM（本地 sidecar）。023 教训是 LLM 输出规范性不可控 → 本 feature MUST 有 **fail-closed 校验**（抽取失败/非法 → 退回存原文 chunk，不污染 store），且 event 抽取任务比 023 的 planner proposal 简单得多（固定字段输出 vs 开放动作规划）。
- **008 铁律**：中间信号（coverage/召回/结构完整性）不转化端到端是历史反复验证的。SEGTREEMEM/StructMem 是**整套写入侧替换**才涨点；engram 照搬单组件能否转化**无现成证据**。本 feature 以端到端正确率为唯一 verdict 依据，先做阶段 0 诊断再决定投入。

### 阶段化（门禁驱动，不盲烧）

- **阶段 0（零成本诊断，L0-1/L0-2）**：拿已冻结 store，temporal + multi-hop 错题喂全对话，确认答案片段**在候选池但打包丢了上下文**（→ B 有救）还是**根本不在池**（→ B 救不了，直接 STOP）。
- **阶段 1（最小先导，复用 023 的 `--only-questions` formal 子集模式）**：本地 sidecar 抽 event，同一批 residual 题配对比较 event store vs chunk store 的 temporal/multi-hop 正确数。一次 ~35 分钟。
- **阶段 2（全量配对）**：阶段 1 端到端转化后才跑 LoCoMo 1540 + LongMemEval-S 500 全量配对。
- **阶段 3（segment tree 检索传播，可选）**：若平铺 event 已转化，再验证树形粒度是否进一步省 token / 提分。

## Background and Scope

### 问题

engram 同预算对 MemOS 的 multi-hop/temporal 缺口已被 budget-ablation 坐实（对齐 1059 tok 时 multi-hop −10.99pp、temporal −9.35pp，均 p<0.002）。021 证明该缺口**不在检索侧**（6 次杠杆全 NO-GO），收敛到**写入侧结构化记忆**。当前写入侧是 chunk-verbatim（原文分块直存）+ 查询侧 RRF 召回——**保留了个别事实，但丢失了事件之间的时间顺序与因果/共同参与关系**。SEGTREEMEM/StructMem 给出干净消融：写入时保留事件结构是 temporal/multi-hop 的真杠杆。

### 目标

在不放宽预算、不引入付费云模型、不破坏 append-only Evidence Ledger 的前提下，验证「写入侧 event 时序结构」能否端到端提升 temporal + multi-hop。**本 feature 是研究实验（默认关），产出 verdict；只有双基准配对通过才讨论进入默认路径。**

### 范围边界

- **在范围内**：event 双视角抽取（事实 + 关系）+ 时间锚定（阶段 1）；周期性跨事件合并/关系摘要（可选）；segment tree 构建 + 沿树传播检索（阶段 2/3，门禁驱动）；fail-closed 校验；配对消融验证。
- **不在范围内**：post-hoc 实体-关系图（014/021/Mem0g 证伪）；检索侧杠杆（021 穷尽）；付费云模型/reranker（DEATH RULE）；LLM 融合写回（022 证伪）；扩大 answer-context 预算（大力出奇迹，不认可）；产品化承诺（本 feature 只出 verdict）。
- 机制默认关（宪法 I/V）；无 LLM 端点时 MUST 退回现有 chunk 路径（宪法 V 优雅降级）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 阶段 0 诊断：先确诊 gold 在不在池（Priority: P1)

维护者在投入任何实现前，先对 temporal + multi-hop 错题做零成本诊断：答案片段是否在候选池内、若在是否因扁平打包丢失了所需的事件上下文。这是 L0-1/L0-2 的落地，决定 B 方向是否有救。

**Why this priority**: 复盘第 5 节纪律——先分类再上杠杆，根除 021「没查 gold 在不在池就硬上」的教训。若 gold 不在池，写入侧表示改动救不了，直接 STOP 省全部机器。

**Independent Test**: 对已冻结 store 的 temporal + multi-hop 错题，构造全对话（LoCoMo 装得下），人审抽样：gold 文本在对话中是否存在、是否被现有检索捞进候选。产出分类计数。

**Acceptance Scenarios**:

1. **Given** 已冻结 store 与错题清单,**When** 把 temporal + multi-hop 错题喂给全对话,**Then** 报告分类计数：gold 在池 / 不在池 / 在池但打包缺上下文。
2. **Given** gold 在池但打包缺上下文的题占比高（预期多数）,**When** 判定,**Then** 确认 B 有救，进入阶段 1；若 gold 不在池占比高，STOP 记录负结论。

---

### User Story 2 - 阶段 1 先导：event store vs chunk store 配对（Priority: P1)

用本地 sidecar 对原始对话做 event 双视角抽取（事实 + 关系条目）+ 时间锚定，存为独立 event store；在同一批 residual 题、同一 answerer/judge/预算下，配对比较 temporal/multi-hop 正确数。复用 023 的 `--only-questions` formal 子集模式（~35 分钟/次）。

**Why this priority**: 这是端到端转化的最小可证实验。008 铁律下，中间信号提升不算赢，必须以同预算端到端正确数为准。

**Independent Test**: 构造同一对话的 chunk store 与 event store，同一 `--only-questions` 子集、同 answerer/judge，repeats≥3，对比 temporal/multi-hop majority 正确数；抽检 event 抽取质量（fail-closed 率、幻觉率）。

**Acceptance Scenarios**:

1. **Given** 同一对话、同一子集题,**When** 只把 store 从 chunk 换成 event,**Then** 报告 temporal + multi-hop 正确数的配对差（McNemar）；事件抽取失败时 fail-closed 退回原文，store 不被污染。
2. **Given** 阶段 1 有转化,**When** 判定,**Then** 进入阶段 2 全量配对；若无转化或负收益，STOP 记录（008 铁律，不进入默认路径）。

---

### User Story 3 - 阶段 2 全量配对：双基准不回归基线（Priority: P1)

在 LoCoMo 1540 + LongMemEval-S 500 上、固定 answer-context token cap 下，对「event 表示 vs chunk 基线」做全量配对消融，确认 temporal/multi-hop 增益且 overall 不回归（宪法 IV）。

**Why this priority**: 宪法 IV 回归门 + 022/024/025 配对纪律。负结果可接受，但必须归因干净。

**Independent Test**: 在冻结协议下跑配对（chunk 基线 vs event 表示），repeats≥3，对比 overall 与分类别配对统计，重点 temporal + multi-hop。

**Acceptance Scenarios**:

1. **Given** 冻结协议与基线,**When** 只开启 event 表示,**Then** LoCoMo overall 不显著回归基线，且 temporal/multi-hop 报告配对明细。
2. **Given** 同上,**When** 报告结果,**Then** 分类别明细 + 配对统计 + token 记账齐全，归因到单一机制（写入侧表示差异）。
3. **Given** 全量配对证明无收益或负收益,**When** 报告,**Then** 按 FR-010 记录 verdict 并保持默认关，不进入默认路径。

---

### User Story 4 - 阶段 3：segment tree 粒度（可选，Priority: P2)

若平铺 event 已转化，验证时间有序 segment tree（SEGTREEMEM 式）是否在不降分下进一步减少 answer-context token，或提升 multi-hop 的跨事件召回。门禁：仅阶段 2 GO 后才启动。

**Why this priority**: 树形粒度是省 token 的工程增益，非涨点承诺；必须在平铺 event 已验证转化后才值得投入。

**Independent Test**: 同一事件 store 上，segment tree 检索传播 vs 平铺 event 召回，比较同一预算下的正确数与 token 消耗。

**Acceptance Scenarios**:

1. **Given** 阶段 2 已 GO,**When** 只变检索粒度（平铺 event → segment tree + 传播),**Then** 同预算正确数不降，且 token 记账显示节流或等价。
2. **Given** 树形粒度无增益或更差,**When** 报告,**Then** 记录并维持平铺 event 路径，不强行上线。

---

### Edge Cases

- **event 抽取失败/非法输出**：本地 LLM 输出不符合 schema → fail-closed 退回存原文 chunk，绝不让脏 event 进 store；记录抽取失败率。
- **幻觉 event**：LLM 抽取出对话中不存在的事实/关系 → 需抽样人审 + 审计判定统计（类似 StructMem 的 fidelity audit），不允许静默污染。
- **时间锚定歧义**：相对时间（"去年""下周三"）需在抽取时解析为绝对时间或保留相对引用 → 参考 013/017 教训（解析器点火率是坑），阶段 1 先抽检时间锚定质量。
- **同源重复事件**：多条消息描述同一事件（进展/补充）→ event 需要去重或合并策略，避免 answer-context 塞满同一事件的多份副本。
- **跨 session 事件**：同一事件跨 session 提及 → event 默认允许跨 session 关联（需有界），跨 namespace 仍隔离。
- **无 LLM 端点**：offline 降级 MUST 退回现有 chunk 路径（零行为变化），event 表示关闭。
- **预算不变量**：event 表示不得暗中扩大 answer-context（同 top-k / token cap 配对）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 支持把原始对话按「事件」组织为时间锚定的事件条目，条目分**事实视角**（发生了什么）与**关系视角**（谁和谁、因果/共同参与）双视角。构建对象为原始 Evidence 级，不破坏 append-only ledger。
- **FR-002**: 事件条目 MUST 保留时间锚定（绝对时间或可解析的相对引用），使检索能按时间顺序还原事件脉络。
- **FR-003**: 事件抽取 MUST 是**可丢弃可重建的投影**（config-hash 幂等重建，如 022 fact 投影），删除/重建不得删改原文。
- **FR-004**: 事件抽取 MUST 默认关闭；关闭时 MUST 与现有 chunk 路径逐字节一致（零行为变化）。
- **FR-005**: LLM 抽取 MUST 走本地 sidecar（Ollama/vllm），MUST NOT 依赖付费云模型；抽取失败/非法输出 MUST fail-closed 退回原文 chunk，不污染 store（宪法 I、DEATH RULE）。
- **FR-006**: 事件表示 MUST 记录可审计的判定统计（抽取数、失败率、疑似幻觉率、合并数），供 fidelity 审计。
- **FR-007**: 事件表示 MUST 有界（每事件证据数上限可配），不得无界放大候选/token（宪法 V 诚实规模）。
- **FR-008**: 配对消融 MUST 固定 answer-context token cap 与候选 anchor，只变写入侧表示，归因到单一机制（008 纪律）。
- **FR-009**: 检索侧 MUST 支持事件 + 关系摘要召回（阶段 1）；segment tree 构建 + 沿树传播检索为阶段 2/3 可选增量（门禁驱动）。
- **FR-010**: 若配对消融证明 event 表示无收益或负收益，该机制 MUST 保持默认关并记录 verdict，不得进入默认路径（宪法 I/V）。
- **FR-011**: 跨 session 事件关联 MUST 默认允许但有界；跨 namespace 访问 MUST 保持隔离（宪法 III）。

### Key Entities

- **Event（事件条目）**：原始对话按事件切分的结构化单元，含事实视角、关系视角与时间锚定；本 feature 的核心新候选单元。
- **Relation Summary（跨事件关系摘要）**：周期性把语义相关、时间相邻的事件合并成的跨事件关系文本（StructMem 式 synthesis），供 multi-hop 推理。
- **Segment Tree（时间有序对话树，阶段 2/3 可选）**：按时间连续段组织的层级索引，内部节点为段总结，检索沿树传播（SEGTREEMEM 式）。
- **Evidence Ledger（原始证据）**：append-only 消息级原文；任何事件/摘要投影都不得删改。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: event 表示开启后，LoCoMo **temporal + multi-hop 类别**相对 chunk 基线有可测提升（配对统计，GO 门：类别提升 ≥2.0pp 且 McNemar p<0.05 量级），且整体不显著回归。
- **SC-002**: LoCoMo 与 LongMemEval-S 的 overall 在**相同 token cap** 下不显著回归基线（宪法 IV）；若 negative，以负结果记录而不进入默认路径。
- **SC-003**: 配对实验有分类别明细、配对统计与 token 记账；表示差异与检索差异被显式分离（同 anchor 同预算）。
- **SC-004**: 关闭 event 表示时，双基准在冻结协议下的结果与现状一致（回归门对照）。
- **SC-005**: 无 LLM 端点、无 embedding 的离线环境下，降级路径（退回 chunk）可完整运行（宪法 V 离线可退化）；event 抽取失败率有记录且 fail-closed 生效。

## Assumptions

- **数据与协议**:复用 022 的冻结协议、lossless chunks、LoCoMo（1540 题）/LongMemEval-S（500 题）与现有 store 资产；复用 023 的 `--only-questions` formal 子集模式跑先导。
- **本地 sidecar**:本地已有可用的 LLM sidecar（vllm 7B/35B 或 Ollama）用于 event 抽取；embedding 用现有 bge-large 端点。
- **判断基准**:成功以「同预算下的端到端正确率」为准，不以 coverage/recall/结构完整性作 verdict（008 教训 + 复盘规律 1）。
- **范围**:本 feature 是研究实验（默认关），不承诺 productization；只有双基准配对通过才讨论进入默认路径。
- **对 MemOS 的对标**:「更好」= 同预算下 temporal/multi-hop 达到或接近 MemOS 的 tree 水平；付费云模型/reranker 明确排除（DEATH RULE）。
- **依赖**:依赖已冻结 store 资产与已收口基线（LoCoMo 85.19%）；阶段 0 诊断若显示 gold 不在池占比高，本 feature 在阶段 1 前 STOP。
