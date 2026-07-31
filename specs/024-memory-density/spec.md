# Feature Specification: 同预算记忆密度杠杆

**Feature Branch**: `024-memory-density`

**Created**: 2026-07-31

**Status**: Draft

**Input**: User description: "同预算记忆密度杠杆：借鉴 MemOS Tree+事实 模式中两个经源码验证、且与 engram 宪法兼容的机制，作为 022 的后续增量实验。(1) 写入时冗余抑制：新增 atomic fact 投影时用 embedding 相似度检测与已有事实的语义冗余，抑制重复投影写入——evidence ledger 保持 append-only 无损，只跳过冗余的 fact 投影，提高检索候选的信息密度。(2) 命中后邻居扩展：检索命中一个 fact 后，沿 lineage/relation 取相关兄弟 fact 或父主题节点，把相关上下文带进 answerer。两者默认关、纯本地、可独立消融，用 LoCoMo/LongMemEval 双基准同预算配对验证是否真涨点。"

## Clarifications

### Session 2026-07-31（实现验证回填）

- **判定信号基于 fact Content 而非 name**：`entryName` 带随机 ULID 尾，若把 name 纳入 trigram Jaccard 会把近似事实的相似度从 ~0.87 稀释到 ~0.5（随机尾噪声淹没 content 信号）。`memory/curation.Suppressor` 的 `suppressionText` 只用 Content（小写 + 空白折叠），与 dedup 的 `normalizeText` 区分开（后者用于 curation Cluster，name 是稳定标识）。
- **候选查询用采样 OR trigram**：`buildPlan` 的全 trigram AND 对"尾部差一个字"的近似文本不命中（候选=0）；`SimilarEntries` 用均匀采样 OR（每 k 个 trigram 取 1，最多 12 个），近似文本仍能进入候选，精确 Jaccard 兜底判定。
- **冲突保护依赖 EventDate**：`ShouldSuppress` 只在两 entry 的 `EventDate` 都非 nil 且不等时视为冲突（FR-003）；判定发生在投影创建前，`storeFact` 需先解析 incoming 的 `event_date` 再判定。
- **邻居扩展只在 legacy packer 路径**：compiler arm 的 candidate-replay 必须 byte-identical，兄弟扩展会改变候选集 → 只在非 compiler 的 materialize 路径生效（FR-007）。
- **四臂 manifest 冻结验证**：`--eval-freeze-protocol` + `--write-dedup`/`--neighbor-extend` 产出独立 hash 的 b1 manifest（control 3-key 与 022 资产逐字节一致；dedup/neighbor 各带独立 hash → 归因成立）；`--eval-protocol control + --write-dedup` 在模型调用前 fail closed（`write_dedup=false differs from requested true`）。

### 022 外部证据（初始）

- **写入时去重的依据**：MemOS `manager.py` 用 `merged_threshold=0.92` 的 embedding 相似度 + LLM 判定做写入时合并，保证库里的事实非冗余、信息密集。这是其"低预算高信息密度"（同栈复现 1059 tok 达 82.4%，engram 同预算仅 76.85%）的根因。
- **engram 版本不能照搬 LLM 融合写回**：`Retain or Consolidate` 证明宽松预算下 write-time merge 显著为负（−0.107 [−0.204,−0.013]），且融合写回破坏 022 的 append-only Evidence Ledger 宪法。因此本 spec 只取"抑制**冗余投影**"（投影可丢弃可重建，不碰 evidence），不做"融合覆盖"。
- **命中后邻居扩展的依据**：MemOS 检索在命中节点后取 depth-2 图邻居（父 topic + 兄弟 fact），multi-hop 相关事实在树上相邻。engram 已有 `memory_projection_sources` 的 relation/共源关系，可离线实现等价扩展，无需引入图数据库。
- **验证纪律**：沿用 022/023 的配对消融 + 宪法 IV 双基准门；两机制默认关，未过门不得进默认路径。

## Decision and Relationship to Feature 022

- 022 交付了 Evidence Ledger、atomic fact 投影、lineage 与 Compiler 框架，verdict 为 `HOLD`。本 feature 是 022 的**后续增量实验**，不是替换。
- 022 已证伪的方向本 feature **不再重复**：pure-fact 压缩（73.70% < 83.83%，single-hop −16.0pp）证明"压缩替代原文"是 NO-GO；本 feature 不做 write-time 摘要/压缩。
- 022 未验证的 H1/H2（semantic episode、query-time compiler 正式化）与本 feature **互补而非依赖**：本 feature 的两机制作用于"写入侧的候选密度"与"检索侧的上下文连贯性"，可在任何表示（chunk / raw-turn / episode）之上独立开关。
- 本 feature 与 022 共享 store 与协议资产（`022.v1` protocol、lossless chunks、LoCoMo/LongMemEval-S 双基准），所有实验在冻结协议下配对运行。

## Background and Scope

### 问题

budget-ablation 已证明 LoCoMo 分数主要是 answer-context token 预算的函数：engram 用 3614 tok 达 85.7%，对齐 MemOS 的 1059 tok 后仅 76.85%，而 MemOS 在 1059 tok 下仍达 82.47%。差距不在"记忆机制更聪明"，而在**同预算下的信息密度**：MemOS 的候选里没有冗余事实、且命中后能带出相关兄弟事实。

### 目标

在不放宽预算、不引入付费云模型、不破坏 append-only Evidence Ledger 的前提下，用两个纯本地机制提高"每个 answer token 携带的有效证据量"，目标是在**同预算**（与当前基线相同的 answer-context token cap）下缩小对 MemOS 的差距，且不回归当前基线。

### 范围边界

- **在范围内**：写入时冗余抑制；命中后邻居扩展；两机制在 022 协议下的独立消融与双基准配对验证。
- **不在范围内**：LLM 融合写回、write-time 压缩/摘要、图数据库引入、付费 reranker、semantic episode 表示（022 H1，独立 feature）、compiler 正式化（022 H2，独立 feature）。
- 两机制默认关（宪法 I/V）；无 LLM 端时须有纯离线判定路径（宪法 V）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 写入时冗余抑制，让库里的记忆不重复 (Priority: P1)

用户（记忆系统的调用方）向引擎写入同一事件的近似描述时，系统只保留第一次的 atomic fact 投影，后续语义等价的描述**不产生新的重复投影**；原始消息仍全部进入 evidence ledger，不丢失任何原文。

**Why this priority**: 这是信息密度的第一来源。MemOS 靠它在 1059 tok 下装下别的系统 3614 tok 才装得下的有效信息。冗余投影进不了检索候选，answerer 就不会把预算浪费在重复内容上。

**Independent Test**: 在离线、无 embedding 端点的降级模式下写入两条近似事实，断言第二条不产生投影（或产生标记为 redundant 的 relation），且两条原文 evidence 都完整保留。

**Acceptance Scenarios**:

1. **Given** 一个空 store，**When** 依次写入两条语义等价的事实（如 "小明 1 月 14 日开家长会" 与 "小明下周三下午两点有家长会，那天是 1 月 14 号"），**Then** 第二条不产生新的 atomic fact 投影，两条原文消息的 evidence 均存在。
2. **Given** 写入时冗余检测开启，**When** 新事实与已有事实的语义相似度超过阈值，**Then** 新投影被抑制，不进入检索候选，且不触发任何 LLM 调用（纯离线判定路径）。
3. **Given** 冗余检测关闭（默认），**When** 写入近似事实，**Then** 行为与现状完全一致（无任何行为变化）。

---

### User Story 2 - 命中后邻居扩展，多跳相关的上下文一起出现 (Priority: P1)

用户查询命中一个 fact 时，系统沿其 lineage/relation 把**同一事件/主题的相关兄弟 fact**一起带进 answerer 上下文，让依赖多条证据才能作答的问题（multi-hop / temporal）在**不增加检索预算**的情况下看到连贯证据。

**Why this priority**: 检索单点命中的 fact 往往信息不足；相关事实在库里相邻，扩展是"免费"的上下文连贯性。对应 MemOS 的 depth-2 图邻居，但用 engram 已有的 lineage/relation 离线实现。

**Independent Test**: 构造一个 fact 与其兄弟 fact 共享 evidence 的 store，命中该 fact 时断言兄弟 fact 出现在扩展后的候选里，且默认关闭时行为不变。

**Acceptance Scenarios**:

1. **Given** 两个 fact 由同一条（或强关联的）evidence 支撑，**When** 查询命中其中一个，**Then** 另一个出现在扩展后的 answerer 上下文中（在候选冻结后、组装前扩展）。
2. **Given** 邻居扩展开启，**When** 命中的 fact 没有可扩展的 relation 邻居，**Then** 候选与未开启时一致（扩展为空时零变化）。
3. **Given** 邻居扩展关闭（默认），**When** 查询命中 fact，**Then** 上下文与现状完全一致。

---

### User Story 3 - 双基准同预算配对验证，任一机制不得回归 (Priority: P1)

维护者在 LoCoMo 与 LongMemEval-S 上、在**固定 answer-context token cap** 的 022 协议下，对两个机制分别做开关配对实验，确认每个机制的独立收益（或负结果），且不回归当前基线。

**Why this priority**: 宪法 IV 的回归门 + 022/023 的配对纪律。机制可以证明无效（负结果也接受），但不能默默回归基线或把两机制混在一起归因。

**Independent Test**: 在 022 冻结协议下跑四臂（关/开冗余抑制 × 关/开邻居扩展，各 repeats≥3），对比 overall 与分类别的配对统计。

**Acceptance Scenarios**:

1. **Given** 022 冻结协议与当前基线分数，**When** 只开启冗余抑制（邻居扩展关），**Then** LoCoMo 与 LongMemEval-S 的 overall 不显著回归基线，且报告分类别明细。
2. **Given** 同上，**When** 只开启邻居扩展，**Then** 同样不显著回归，且与臂 1 的差异可归因到单一机制。
3. **Given** 两机制均开启，**When** 与各自单开对比，**Then** 交互效应被显式报告（可叠加 or 相互抵消，不得默认假设可加）。

---

### Edge Cases

- **抑制误伤**：两条事实语义相似但不冗余（如同一事件的不同具体细节）被抑制 → 需要"相似但不等价"的判定边界；误抑制必须可观测（可审计计数），不允许静默丢信息。
- **冲突而非冗余**：新事实与已有事实矛盾（如日期被纠正）→ 不得抑制，须保留（供 temporal 校正），本 feature 不做冲突融合。
- **邻居扩展的规模爆炸**：高关联度 fact 的兄弟数量很大 → 扩展必须有界（上限可配），不无界放大候选/token。
- **无 embedding 端点**：offline 降级模式下冗余判定退回纯离线信号（词/字符重叠、exact-token），不得依赖不可用的在线端点。
- **两机制与既有机制的交互**：与 022 的 Compiler、H1 episode 表示同时开启时的行为未知 → 本 feature 只保证自身开关的独立行为，不保证与其他机制组合后的叠加性。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 在新增 atomic fact 投影前，检测其与已有事实投影的语义冗余。主判定信号为**离线字符相似度**（trigram Jaccard，默认阈值 0.7，FTS pre-filter 限定 candidate pairs；见 research.md Decision 1）；可选叠加 embedding 语义信号（默认关，需本地 sidecar，阈值约 0.9）。系统 MUST 提供无 embedding 端点的纯离线判定降级路径。
- **FR-002**: 被判定为冗余的新投影 MUST 不写入检索候选；对应原始消息的 evidence MUST 完整保留（append-only 不受影响）。
- **FR-003**: 冗余抑制 MUST 只处理"冗余/等价"，MUST NOT 处理"矛盾/冲突"——冲突事实不得被抑制。
- **FR-004**: 冗余抑制 MUST 默认关闭；关闭时 MUST 与现状逐字节一致（零行为变化）。
- **FR-005**: 冗余抑制 MUST 记录可审计的判定统计（判定总数、抑制数、疑似误伤数），供评估误伤率。"疑似误伤"的运行时判定（如：被抑制候选存在独立 evidence 支撑仍被抑制）与配对消融兜底（端到端分类别回退）见 research.md Decision 4。
- **FR-006**: 检索命中 fact 后，系统 MUST 支持沿 lineage/relation 取有界数量的相关兄弟 fact（扩展深度与上限可配置，默认 depth-1 且有界；上限取值与理由见 research.md Decision 2）。
- **FR-007**: 邻居扩展 MUST 在候选冻结之后、answerer 上下文组装之前执行，MUST NOT 触发额外检索调用。
- **FR-008**: 邻居扩展 MUST 默认关闭；关闭时候选与现状完全一致；无邻居可扩展时行为与关闭一致。
- **FR-009**: 两机制 MUST 可独立开关、独立配置，并 MUST 在 022 冻结协议下分别配对验证（宪法 IV 回归门）。
- **FR-010**: 两机制 MUST 纯本地可运行，MUST NOT 依赖付费云模型或强制 LLM 调用；任何可选 LLM 判定 MUST 默认关。
- **FR-011**: 若经配对消融证明某机制无收益或负收益，该机制 MUST 保持默认关并记录 verdict，MUST NOT 进入默认路径（宪法 I/V）。

### Key Entities

- **Fact Projection（事实投影）**: 从 evidence 提取、可丢弃可重建的原子事实视图；冗余抑制作用于其写入。
- **Evidence（原始证据）**: append-only 的消息级原文；任何抑制/扩展都不得删改。
- **Redundant Relation（冗余关系标记）**: 记录"新投影与既有投影冗余"的可审计标记（可选，用于审计而非融合）。
- **Neighbor Context（邻居上下文）**: 命中 fact 扩展出的相关兄弟 fact 集合，有界、可配置。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 冗余抑制开启后，检索候选中的近似重复事实占比下降可测（相对关闭时降低 > 0），且原始 evidence 无任何丢失。
- **SC-002**: 邻居扩展开启后，多跳/时序类别的 answer-context 中相关证据覆盖提升，且 LoCoMo 与 LongMemEval-S 的 overall 在**相同 token cap** 下不显著回归基线（宪法 IV）。
- **SC-003**: 两机制分别开关的四臂配对实验在双基准上均有分类别明细与配对统计；任一机制负收益时，以负结果记录而不进入默认路径。
- **SC-004**: 关闭两机制时，双基准在 022 冻结协议下的结果与现状一致（可作为回归门对照）。
- **SC-005**: 无 embedding 端点、无 LLM 的离线环境下，两机制的降级路径可完整运行并产出判定（宪法 V 的离线可退化）。

## Assumptions

- **数据与协议**：复用 022 的 `022.v1` 协议、lossless chunks、LoCoMo（1540 题）/LongMemEval-S（500 题）双基准与现有 store 资产。
- **判断基准**：信息密度的成功与否以"同预算下的端到端正确率"为准，不以 coverage/recall 等代理指标作 verdict（008 教训：coverage 增益不等于答题增益）。
- **范围**：本 feature 是研究实验（默认关），不承诺 productization；只有双基准同预算配对通过才讨论进入默认路径。
- **对标**：对 MemOS 的"效果更好"定义为"同预算下达到或接近其信息密度"，不是复刻其架构；LLM 融合写回、图数据库、付费 reranker 均明确排除。
- **依赖**：依赖 022 已交付的 Evidence Ledger、projection/lineage 与正式协议资产；若 022 资产未冻结，本 feature 实验不启动。
