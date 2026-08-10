# Feature Specification: Multi-hop Chunk-First Contract Repair

**Feature Branch**: `worktree-033-chunk-first-contract-repair`

**Created**: 2026-08-10

**Status**: Rejected — paid probe NO-GO; full-set forbidden; do not merge

**Input**: User description: "在遵守 engram 宪章与禁用付费云端 reranker 的前提下，修复 LoCoMo 读侧装配在 multi-hop 类别中没有兑现 chunk-first 合同的问题，以纯本地、确定性方式提升证据可见性，并通过同口径配对评测冲击严格超过 90%。"

## Decision and Scope

Feature 030 已规定装配顺序为「所有原文 chunk 优先，fact 补足」，并要求 multi-hop
问题在此基础上按实体组织。当前 multi-hop 路径先按实体铺平证据，组内仅按相关分排序，因而
可能让高分 fact 出现在后续 chunk 之前。当前记录与渲染都沿用这条旧顺序；但修复后若渲染器
继续自行重新分组，它会把已经分层的证据再次混排。因此排序与渲染必须共享同一顺序真相。
本 feature 只修复这个既有合同缺口，不增加召回、不改变输入候选闭包、不引入新模型。

范围内：

- 统一 multi-hop 的装配记录与实际答题上下文顺序；
- 保证所有 chunk 位于所有 fact 之前，同时保留实体组织提示；
- 提供仅供配对归因与回退使用的旧顺序对照，并把其状态写入实验收据；
- 冻结零调用诊断、目标/guard 小探针和全量配对晋级门。

范围外：

- 修改检索、embedding、store、抽取、curation 或 memory engine；
- 使用 gold、judge 标签或 gold evidence 决定运行时排序；
- 新增 agent 导航、trace 生成、候选答案裁决或付费云 rerank/recall；
- 把历史七月 89.03 与新 treatment 直接相减来声明增量。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 原文证据始终优先 (Priority: P1)

评测维护者启用证据装配后，无论问题是否需要实体组织，答题者都先看到保真度最高的原文
chunk，再看到抽取 fact；multi-hop 的实体提示不能推翻这个全局优先级。

**Why this priority**: 64 题零调用诊断显示现有实现会在 multi-hop 分支重新混排证据，而
目标残差的 gold 原文通常位于较后位置。先兑现已有合同，才能把后续涨点归因于证据展示而非
新召回或模型变化。

**Independent Test**: 使用混合 chunk/fact、多个实体和无实体证据的固定输入，验证装配记录与
实际答题上下文均满足 chunk-before-fact，候选内容和数量不变，重复运行输出一致。

**Acceptance Scenarios**:

1. **Given** 一个 multi-hop 候选集中同时包含 chunk 与 fact，**When** 进行证据装配，
   **Then** 装配记录和实际答题上下文中的每个 chunk 都位于任一 fact 之前。
2. **Given** chunk 与 fact 共享同一实体，**When** 呈现实体验证据组织，**Then** 两类证据仍分属
   chunk-first 顺序层，且实体归属仍清晰可见。
3. **Given** 候选中只有 chunk、只有 fact 或没有可识别实体，**When** 装配，**Then** 内容不丢失、
   顺序确定且不会因空分组产生空答上下文。
4. **Given** 相同输入重复执行，**When** 比较产物，**Then** 装配记录与答题上下文逐字节一致。

---

### User Story 2 - 修复不改变检索与其他类别 (Priority: P1)

评测维护者能够确认这是一项纯展示顺序修复：候选集合、候选文本、检索调用数和非 multi-hop
类别的行为均保持不变；未启用证据装配时仍走原有路径。

**Why this priority**: 只有固定检索与候选集合，端到端差异才能归因于 multi-hop 的 chunk-first
合同修复；这也是评测回归门与引擎/适配层边界的前提。

**Independent Test**: 对冻结输入比较修复前后的候选 ID/text 多重集、非 multi-hop 产物以及关闭
装配时的上下文，全部必须保持一致。

**Acceptance Scenarios**:

1. **Given** 同一冻结的装配器输入闭包，**When** 分别走旧顺序 control 与修复后的 treatment，
   **Then** 输入候选 ID、文本和数量完全相同，只有 multi-hop 的展示顺序/分组边界允许变化；
   若预算截断触发，两边各自只能保留其规范顺序的最长可预算前缀。
2. **Given** temporal、single-hop 或 open-domain 问题，**When** 启用装配，**Then** 其装配记录与
   实际上下文相对修复前逐字节不变。
3. **Given** 证据装配关闭，**When** 执行任意问题，**Then** 原有答题上下文逐字节不变。
4. **Given** 任一模型端点缺失，**When** 执行离线合同测试与诊断，**Then** 修复仍可完整验证。

---

### User Story 3 - 用预注册配对门验证突破 (Priority: P2)

评测维护者先在冻结的 32 道目标残差和 32 道匹配 guard 上比较三臂：A 未装配 baseline、B 旧
multi-hop 顺序 legacy、C 修复后 treatment。A/C 回答「完整修复装配能否救分」，B/C 仅在两组中
共 18 道 multi-hop 问题上回答「本次合同修复相对旧装配的独立贡献」；随后按同一数据、store、
检索、answerer、judge、prompt、预算与三次聚合规则进行全量验证。

**Why this priority**: 单次观测和跨日期绝对分会被采样、模型别名及 judge 漂移污染。小探针先止损，
通过后使用同一 binary、fresh run dirs，并在同一时间窗口交错运行 A/C，才能诚实判断是否严格
突破 90%。不同 arm 是独立进程，不声称“同进程”。

**Independent Test**: B/C 仅相差本 feature 的排序修复；A/C 是完整 evidence assembly 的主门。
先导门通过后，全量 1540 道 cat 1–4 对 A/C 各运行三次，并生成逐题翻转、类别结果、调用成本和
协议摘要。

**Acceptance Scenarios**:

1. **Given** 冻结的 target-32 与 matched-guard-32，**When** A/C 对 64 题各运行三次且 B 对其中
   18 道 multi-hop 题运行三次，**Then** C 相对 A 在 target 上净救回至少 8 道，且 guard 净损失
   不超过 1 道；同时单独报告 C 相对 B 的 multi-hop 翻转。主门未通过则停止全量评测并登记 NO-GO。
2. **Given** 小探针通过，**When** 执行全量配对，**Then** treatment 的三次多数结果严格高于
   90.00%（至少 1387/1540），并报告同一 binary、同一时间窗口下 C 相对 A baseline 的逐题翻转
   和显著性。
3. **Given** 任一类别出现统计上可信的回退，**When** 裁决，**Then** 不晋级该 treatment，完整记录
   回退类别和回退方案。“统计上可信”定义为分类别 exact McNemar 检验经 Holm 多重校正后
   `p < 0.05` 且净变化为负。
4. **Given** 历史 89.03 产物，**When** 报告新结果，**Then** 历史值只作背景，不充当新实验 control。
5. **Given** target-32 的 retrieval-only gold-in-pool backstop，**When** 裁决付费探针，**Then** 对全部
   19 道 chunk-gold 题逐题报告 A/C 的实际 provider `answer_context_tokens`，并对 C 的 assembly cap、
   admitted/input 数量、gold chunk 是否 admitted、是否截断及 token 计数来源（exact 或 estimated）逐次报告；只有
   `admitted < input_candidate_count` 才可称为 cap 截断，不能用均值推断。
6. **Given** 从冻结 trace 派生的 `chunk-gold && gold_rank_topk >= 19` 分层，**When** 分析 C-vs-A，
   **Then** 分别报告该层与其余 probe 的 paired flips。该 gold-derived 分层只能在结果冻结后用于
   事后归因，不得进入运行时排序、arm 选择、探针选题或 GO 主门。

### Edge Cases

- 多个实体跨 chunk/fact 重复出现时，分组标签可以重复，但不得使 fact 越过任何 chunk。
- 证据没有可识别实体时，仍按 kind 与原有稳定次序呈现到明确的 ungrouped 分组。
- 同分证据必须有稳定、可复现的次级顺序；不得依赖 map 迭代顺序。
- 精确 token 预算触发截断时，只能从最低优先级尾部移除，且装配记录必须与实际渲染一致。
- 候选为空时仍生成合法的空上下文提示，不 panic、不调用额外模型。
- relation/trace 等其他可选结构不得被本 feature 隐式开启；组合效果不纳入本 feature 的涨点声明。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: multi-hop 证据装配 MUST 对完整候选集合执行全局 chunk-before-fact 优先级；任何
  fact 均不得出现在任一 chunk 之前。
- **FR-002**: multi-hop 装配 MUST 保留可读的实体组织；同一 kind 内的实体组与组内证据顺序
  MUST 确定、稳定、可复现。
- **FR-003**: 装配记录中的证据顺序 MUST 与实际答题上下文中的证据顺序一致。
- **FR-004**: 修复 MUST NOT 增删或改写装配器输入闭包中的候选证据；B legacy/C treatment 的输入
  候选 ID、文本和数量 MUST 完全一致。预算截断后的输出可以不同，但 MUST 分别是各自规范顺序的
  最长可预算前缀。
- **FR-005**: temporal、single-hop、open-domain 以及装配关闭路径 MUST 保持逐字节不变。
- **FR-006**: 排序与分组 MUST 完全本地、确定性运行，不调用 LLM、reranker 或任何托管服务。
- **FR-007**: 运行时排序 MUST NOT 读取 gold answer、gold evidence、judge verdict、correctness 或
  attribution 的 gold-rank 信息；改变这些私有字段 MUST 不改变排序、prompt bytes、调用数或模式选择。
- **FR-008**: 小探针与全量评测 MUST 冻结数据、store、检索、answerer、judge、prompt、token 预算、
  思考模式和三次聚合规则。正式口径固定 `LOCOMO_NO_THINKING=0`、legacy IDK retry 开启、三次
  聚合；A/C 是完整装配主门，只有 B/C 的唯一变量是本 feature 的 multi-hop 顺序修复。
- **FR-009**: 小探针未满足 target/guard 门时 MUST 停止，不得以扩大预算或叠加其他机制补救。
- **FR-010**: 本 feature MUST 不修改 `memory/`、`embedding/`、`provider/`、`store/` 或 `internal/`
  下的任何运行代码。
- **FR-011**: 任何正式突破声明 MUST 同时给出 treatment 绝对分、同一 binary 与同一时间窗口下的
  paired flips、分类别 Holm 校正结果、协议摘要、成本和失败回退；不得依赖付费云 rerank/recall。
- **FR-012**: 系统 MUST 提供仅用于 benchmark 配对的旧 multi-hop 顺序 control；它 MUST 默认关闭、
  不改变修复后的正常装配行为，并 MUST 进入协议指纹与实验收据。
- **FR-013**: 付费探针 MUST 为 C（以及适用的 B）生成与实际答题路径同轮的 assembly audit；分析器
  MUST 将它与每次结果中的 provider `answer_context_tokens` 配对，逐题区分 cap 截断救回与未截断的
  聚焦重排。exact counter 不可用时 MUST 标记 estimated，且不得宣称 token-exact。
- **FR-014**: gold-derived cohort MUST 从冻结的 retrieval-only trace 可复算派生并记录 digest，只能
  用于结果冻结后的分层报告；它 MUST NOT 改变执行 cohort、排序、prompt、调用数或 GO 判据。

### Key Entities

- **Evidence Candidate**: 检索返回的只读证据，包含稳定 ID、原文内容、证据类型、相关分与可选实体。
- **Kind Layer**: 全局展示层，顺序固定为 chunk 后 fact；实体组织只能在层内发生。
- **Entity Group**: 同一 kind 层内共享实体标签的一组证据；无法归组的证据进入稳定的 ungrouped 组。
- **Assembly Record**: 可审计的证据有序列表及预算信息，必须忠实反映答题者实际看到的证据顺序。
- **Paired Evaluation Receipt**: 冻结 A/C 主门与 B/C 归因的公共条件、变量边界、逐题翻转、类别结果、
  成本与产物摘要的记录。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 对所有离线合同用例，multi-hop 装配记录和实际答题上下文均达到 100% chunk-before-fact，
  重复执行产物逐字节一致。
- **SC-002**: 冻结诊断中 B legacy/C treatment 的装配器输入闭包一致率为 100%，各自输出均为其规范
  顺序的最长可预算前缀；非 multi-hop 与关闭路径的字节一致率为 100%，额外模型调用数为 0。
- **SC-003**: 64 题预注册探针中，target-32 的 C-only 减 A-only 至少为 8，matched-guard-32 的
  A-only 减 C-only 不超过 1；B/C 只在 18 道 multi-hop 题上单独报告，不替代主门；未达到即停止。
- **SC-004**: 全量 LoCoMo cat 1–4、三次多数的 treatment 至少达到 1387/1540（严格 >90.00%），
  且同一 binary 与同一时间窗口下 C 相对 A baseline 的配对结果为正，没有 exact McNemar 经 Holm
  校正后 `p < 0.05` 的净负类别回退。
- **SC-005**: 全量报告覆盖 1540/1540 道问题，记录全部 answer/judge 调用、上下文预算、协议摘要、
  逐题翻转和可复算产物；任何缺失均使突破声明无效。
- **SC-006**: 引擎目录变更数为 0，且完整无 CGO 构建与测试通过。
- **SC-007**: 探针 verdict 覆盖 19/19 chunk-gold 题的逐次 context/cap/truncation 记录，并分别给出
  `chunk-gold && rank>=19` 与 remainder 的 C-vs-A 翻转；缺记录或把 estimated 当 exact 均使归因无效。

## Assumptions

- 当前 LoCoMo 数据版本、`009-bge-chunks-store` 和 bge-large 混合检索可用于同一 binary、同一时间
  窗口、fresh run-dir 的 paired 实验；
  历史 89.03 的 store/模型快照未完全冻结，因此只作诊断背景。
- 030 的 universal chunk-first 合同优先于类别内组织；实体组织在 chunk/fact 两个 kind 层内保留。
- 当前 64 题零调用诊断显示所有估算 assembly 上下文低于 harness 的 3600 token cap；外部同日
  baseline 观测使用约 5000 token 的 provider context 口径，两者不是同一计数。正式报告同时保留
  provider usage 与 assembly audit，不以任一均值代替逐题截断判断。
- 付费 answerer/judge 只在用户授权的配对评测阶段调用；实现、单测与 retrieval-only 诊断均离线完成。
- 若本修复未通过预注册小门，候选答案裁决属于独立的后续 benchmark-only feature，不在本 feature
  中混入或用于挽救结果。
