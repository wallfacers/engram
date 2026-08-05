# Feature Specification: 查询时时间有效性解析

**Feature Branch**: `027-temporal-validity-resolution`

**Created**: 2026-08-06

**Status**: Draft

**Input**: User description: "在 022 Evidence Ledger(append-only)基础上实现查询时时间有效性解析:保留 superseded 事实的完整演化链,查询时按 validity interval 用确定性结构化解析选择当前值/演化链喂给固定 answerer;同 store、候选逐字节一致配对验证 temporal/multi-hop 是否相对 chunk_900 基线涨点。纯 Go、离线、确定性。文献锚点 APEX-MEM(2604.14362):append-only vs eager consolidation 在 temporal 上 +14~25pp。"

## Decision and Relationship to Feature 022

022 交付了 append-only Evidence Ledger（消息级原文、来源/隐私生命周期合同）与冻结的
chunk_900 候选 + 固定 answerer + fixed-gold oracle + LoCoMo B1 基线（85.19% majority /
84.7% stats）。engram 已证伪的六次 temporal 检索侧杠杆（013 时间窗召回、014 answer-side
temporal contract、017 temporal 检索、021 IRIS 缺口补检）全部属于**检索侧**：它们的机制是
"改变检索/召回/补检去拿更多时间证据"。

027 不重复上述检索侧杠杆。027 承接 022 的 append-only Ledger 与冻结候选，检验一个**未被
上述证伪覆盖的查侧结构化时间推理**命题：候选已经命中事实（包括 superseded 旧值与当前值），
但**查询时未按时间有效性解析事实演化**——回答 temporal / knowledge-update / multi-hop
演化类问题时，用了错误版本或未组装演化链。外部受控证据（APEX-MEM, 2604.14362, ACL 2026）
显示：append-only 存储 + 查询时按 validity interval 解析，相对 eager consolidation 在 temporal
类目上是 **+14~25pp** 的机制性差异（APEX-MEM 90.63 vs Mem0 75.71 / MIRIX 65.62 / Zep 76.60），
且 GraphSQL 结构化时间查询单加一项 +9.37pp（temporal 72.92→82.29）。

| 能力面 | 022 所有权 | 027 所有权 | 并行结论 |
|---|---|---|---|
| append-only Ledger、来源 lineage、消息级原文 | 定义、实现并冻结 | 只读消费发布来源的演化序列 | 不冲突 |
| chunk_900 候选、候选冻结、retrieval、top-k、RRF | 定义、实现并冻结 | 不改变候选或表示 | 不冲突 |
| deterministic Compiler、exact-token/extractive arm、token 门、fallback | 定义、实现并维护 | 不复制、不修改，只接受校验结果 | 不冲突 |
| answerer、judge、一次作答纪律、token cap | 定义、实现并冻结 | 复用冻结栈，不改调用次数 | 不冲突 |
| **时间有效性解析（temporal validity resolution）** | 未实现（待消融实验面） | 定义、实现并配对验证 | **027 独占** |
| 正式涨点报告 | 发布 022 基线 | 在独立报告中做冻结配对与 verdict | 串行依赖 |

027 的 spec 可以与 022 并行评审，但配对消融、正式评测 MUST 依赖 022 的 accepted baseline
（022 已收口：LoCoMo B1 85.19% majority / 84.7% stats，协议 `sha256:263b52b6…`）。若 027
发现必须改变 022 的公开合同或评测主路径，027 MUST 标记 `BLOCKED` 并把变更请求交回 022；
不得在 027 内绕过、复制或隐式扩展该能力。

## Background and Scope

### 问题

engram 在固定候选 + 固定 answerer 下的 temporal 类目显著弱于 multi-hop/single-hop
（B1 分类别：temporal 80.7% vs single-hop 84.9% / multi-hop 85.1%）。六次检索侧 temporal
杠杆（013/014/017/021 共 6 次 NO-GO）证明"拿更多时间证据"不涨点。022 收口的 density
hypothesis 把差距锚定在命中后的证据覆盖；但**命中后是否按时间有效性正确解析事实演化**未被
隔离检验——候选池同时包含 superseded 旧值与当前值时，回答"现在是什么"可能用了旧值，回答
"何时变成这样"可能缺少演化链。

### 目标

在 022 冻结协议（cap 3600→027 沿用 022 当前 cap、LoCoMo 1,540 answerable、固定 answerer/
judge）下，对 temporal / knowledge-update / 演化类 multi-hop 题，在**固定候选池内**执行确定性
时间有效性解析：按 event_date 与事实主题，用纯 Go 规则（非 LLM）判定
（a）**当前值解析**：知识更新类 query 选取最新 valid 版本；
（b）**演化链组装**：时间演化类 query 按时间序组装完整 superseded→current 链；
（c）**时间窗约束**：query 含显式时间范围时，仅保留 validity 覆盖该范围的版本；
并把解析结果组装进 answer bundle（在冻结 token cap 内），喂给固定 answerer。同 store、候选
逐字节一致配对验证是否相对 chunk_900 基线在 temporal/multi-hop 涨点且 overall 不回归。

### 范围边界

- **在范围内**：harness 侧确定性问题态解析器（当前值/演化链/时间窗）；解析输出的 bundle 组装
  与 token 记账；与 chunk_900 同 store 配对消融；candidate-oracle 归因（区分 resolution miss
  与 candidate miss）；纯 Go 确定性路径；默认关零行为变化。
- **不在范围内**：写侧 consolidation / overwrite（保留 append-only，只读消费）；检索/召回/
  top-k/补检改动（检索侧六次 NO-GO 已证伪）；图存储、property graph、SQL graph（APEX-MEM
  的 GraphSQL 是可选研究对照，不进 V1 实现）；LLM 判定事实主题/演化（须纯 Go 确定性）；
  付费云服务；引擎层契约改动（除非走显式 increment）；写入侧 fact-version 新 schema。
- 机制默认关（宪法 I/V）；无 LLM 端点时须有纯离线判定路径（宪法 V）；不得用付费云模型涨点
  （DEATH RULE）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 时间有效性解析在 formal B1 下可用且确定 (Priority: P1)

固定候选池内，查询时确定性解析事实演化：知识更新题取当前值、演化题组装演化链、显式时间窗
题按窗口过滤；同 query+候选重复运行输出逐字节一致；默认关时行为与 chunk_900 基线完全一致。

**Why this priority**: 这是 027 的 MVP——证明"解析事实演化"这一机制在 formal B1 冻结协议下
可确定运行，不引入 LLM、不改变候选，是后续配对消融的前提。

**Independent Test**: 构造含 superseded/current 双版本的固定候选池，断言解析器输出当前值/
演化链/时间窗过滤符合确定性规则；重复运行 digest 一致；默认关（`--temporal-resolution` 未设）
时与 chunk_900 基线零行为变化。

**Acceptance Scenarios**:

1. **Given** 候选池含同一事实的 superseded 旧值与当前值，**When** 执行当前值解析，**Then**
   bundle 选用最新 valid 版本，候选 ID 与基线逐字节一致，无 LLM 调用。
2. **Given** 候选池含某主题的多时间点版本，**When** 执行演化链解析，**Then** bundle 按时间序
   组装完整 superseded→current 链，每项绑 source ID 与 event_date。
3. **Given** query 含显式时间范围，**When** 执行时间窗约束，**Then** 仅保留 validity 覆盖该
   范围的版本，越窗版本被排除且记录丢弃原因。
4. **Given** 同一 query + 同一候选池，**When** 重复运行任一解析模式，**Then** 输出逐字节一致
   （deterministic）。
5. **Given** 解析机制关闭（默认），**When** 查询，**Then** 行为与 chunk_900 基线完全一致
   （零行为变化）。

---

### User Story 2 - 同 store 配对消融，temporal 解析不回归基线 (Priority: P1)

维护者在 022 冻结协议（LoCoMo 1,540 answerable）下，对"temporal-resolution arm vs chunk_900
基线"做同 store、候选逐字节一致配对，确认查询时时间有效性解析是否带来 temporal/multi-hop
增益且 overall 不回归。

**Why this priority**: 宪法 IV 回归门 + 026/025/024 的配对纪律。负结果可接受，但必须归因干净
（只差解析机制，不差检索/表示）。

**Independent Test**: 在 022 冻结协议与同一 store 下跑 chunk_900 对照 vs `--temporal-resolution`
arm，repeats≥3，对比 overall 与分类别配对统计（重点 temporal / knowledge-update / 演化类
multi-hop）。

**Acceptance Scenarios**:

1. **Given** 022 冻结协议与同一 store，**When** 只切换 temporal-resolution 开关，**Then**
   LoCoMo overall 不显著回归基线，且 temporal/knowledge-update/演化类 multi-hop 分类别报告
   明细（预期增益，负则记录负结果）。
2. **Given** 配对有效（候选逐字节一致），**When** 报告差异，**Then** 归因到单一解析机制变量；
   报告 candidate oracle（superseded/current 双版本是否都在池）以区分 resolution miss 与
   candidate miss。
3. **Given** 任一解析模式相对基线负收益，**When** 报告，**Then** 按 FR-011 记录 verdict 并保持
   默认关，不进入默认路径。

---

### User Story 3 - 归因：temporal 短板是否真在"未解析演化" (Priority: P2)

研究者需要区分 temporal 错题的三类归因：candidate miss（superseded/current 版本不在候选池）、
resolution miss（版本在池但未按时间解析）、answerer miss（证据正确但 answerer 答错）。只有确认
存在 resolution miss 时，027 的机制才值得推广。

**Why this priority**: 023 的 compiler-eligible residual 量化已证明"candidate 够但答错"是真实
残差。027 需要同样的纪律：若 temporal 错题全是 candidate miss，027 的解析机制对 temporal
天花板无贡献（LazyMem 分界：recall 低时构造救不了缺失证据）。

**Independent Test**: 对 temporal/knowledge-update 全量错题逐题分类 candidate miss / resolution
miss / answerer miss，统计 resolution-miss 占比；只有 resolution-miss 非空才支持 027 机制价值。

**Acceptance Scenarios**:

1. **Given** temporal 错题集与 fixed-gold oracle，**When** 逐题检查 superseded/current 版本是否
   在候选池，**Then** 输出 candidate-oracle 归因表，区分三类 miss。
2. **Given** resolution-miss 占比为 0 或可忽略，**When** 评估推广，**Then** 记录负结论，027 机制
   不作为推荐路径（FR-011）。
3. **Given** resolution-miss 占比显著，**When** 配对消融，**Then** 解析机制的增益可归因到 resolution
   miss 被解析覆盖的题目，而非噪声。

### Edge Cases

- **候选内无 superseded 版本（单版本事实）**：解析退化为基线行为，不新增 token，无 LLM 调用。
- **同一事实的 superseded/current 版本跨多条候选**：演化链组装必须跨候选按 event_date 全序，
  不得因 chunk 边界截断演化链。
- **event_date 缺失或不可解析**：该事实按无时间信息处理，不参与时间解析，保持基线行为；记录
  计数供归因。
- **query 无显式时间约束**：默认按当前值解析，不臆测时间窗。
- **解析输出超 token cap**：沿用 022 的 fail-closed 预算行为，按 relevance 裁剪；不得提高 cap
  或丢弃超预算题以挽救分数。
- **gold 不在候选池（candidate miss）**：解析救不了缺失证据（LazyMem 分界）。配对报告须用
  candidate oracle 区分 resolution miss 与 candidate miss，不把 candidate miss 归因于解析器。
- **答案键噪声**：LoCoMo 6.4% 答案键错误（99/1,540，multi-hop 9.9%），temporal 类别小 delta
  须记录噪声并谨慎解读，优先看大 delta 方向。
- **候选一致性**：两臂必须同一 store、候选逐字节一致（025 教训：不同 store = 不可配对）。
- **无 LLM / 无 embedding 端点**：解析路径纯 Go 确定性，无端点时仍可完整运行（宪法 V）。
- **与 013/014/017/021 的关系**：027 不改变检索/召回/补检；若 resolution 与检索侧杠杆叠加，
  是独立后续实验，不在 027 内混用归因。
- **resolution miss 但 answerer 仍答错**：归为 answerer-side ceiling，不纳入 resolution-miss
  计数，不把 answerer 短板伪装成解析器收益。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 在固定候选池内按 query 执行确定性时间有效性解析：query 含显式时间
  约束时按 validity 过滤，知识更新语义时选当前值，演化语义时按 event_date 组装 superseded→current
  链；解析过程 MUST 为纯 Go 确定性规则，MUST NOT 调用 LLM 判定事实主题或演化关系。
- **FR-002**: 解析 MUST 基于 022 已交付的 append-only Evidence Ledger 与冻结候选，只读消费
  来源演化序列；**不重写引擎、不新增写侧 schema、不改 022 候选/表示/Compiler 契约**；引擎契约
  改动 MUST 走显式 increment（宪法 II）。
- **FR-003**: 解析产物 MUST 组装进 answer bundle，逐项绑 source ID 与 event_date，且在冻结
  token cap 内；超 cap 时沿用 022 fail-closed 预算行为。
- **FR-004**: 解析 MUST 默认关闭；关闭时 MUST 与 chunk_900 基线行为完全一致（零行为变化）。
- **FR-005**: 解析 MUST fail-closed：event_date 缺失、无演化证据、query 无时间语义等不适用情形
  一律退化为基线行为，不产生无来源或臆测内容。
- **FR-006**: 无 LLM/embedding 端点时解析路径 MUST 可完整离线运行（宪法 V）；MUST NOT 依赖
  付费云 reranker/recall/Planner/answerer（DEATH RULE）。
- **FR-007**: 配对消融 MUST 同 store、候选逐字节一致，只差解析开关；报告 MUST 含 candidate
  oracle（superseded/current 是否都在池）以区分 resolution miss 与 candidate miss。
- **FR-008**: 报告 MUST 逐题记录解析模式（当前值/演化链/时间窗/退化）、命中版本数、token 取舍、
  未满足 gap，供归因。
- **FR-009**: 解析 MUST 保持 append-only Ledger 无损；只读消费，不删改 superseded 历史。
- **FR-010**: 若配对证明时间解析无收益或负收益，该机制 MUST 保持默认关并记录 verdict，不得进入
  默认路径（宪法 I/V）。
- **FR-011**: 结果报告 MUST 记录 LoCoMo 答案键噪声（6.4%/multi-hop 9.9%，Penfield audit），
  小 delta 不单独作 promotion 依据。

### Key Entities

- **时间有效性解析（Temporal Validity Resolution）**: 查侧确定性机制，对候选池内事实按
  event_date 判定当前值/演化链/时间窗过滤，输出绑 source 的解析结果。027 的新候选单元。
- **事件演化链（Evolution Chain）**: 同一事实主题跨候选按时间全序的 superseded→current 版本
  序列，解析器组装输出。
- **Resolution Oracle（归因）**: 每题 superseded/current 版本是否已在候选池的诊断记录，区分
  resolution miss 与 candidate miss。
- **Append-only Evidence Ledger（022 承接）**: 消息级原文的不可变历史；解析器只读消费其演化
  序列。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: temporal-resolution arm 在 LoCoMo 上相对 chunk_900 基线在 temporal 与
  knowledge-update 类目有可测提升（配对统计；预期机制：解析当前值/演化链），且 overall 不显著
  回归。
- **SC-002**: LoCoMo overall 在相同 token cap 下不显著回归基线（宪法 IV）；若 negative，以负
  结果记录而不进入默认路径。
- **SC-003**: 配对实验有分类别明细、配对统计、token 记账与 resolution oracle（superseded/
  current 在池判定）；候选逐字节一致性有抽查证据（配对纪律）。
- **SC-004**: 关闭时间解析时，LoCoMo 在 022 冻结协议下的结果与基线一致（回归门对照）。
- **SC-005**: 无 LLM、无 embedding 的离线环境下，确定性解析路径可完整运行并产出合法 bundle
  （宪法 V）。
- **SC-006**: 任一不适用/无证据/无时间语义情形均 fail-closed 退化为基线行为，不产生无来源解析
  内容。
- **SC-007**: temporal/knowledge-update 错题归因（candidate/resolution/answerer miss）覆盖率为
  100%；只有 resolution-miss 占比显著时，解析机制的增益才被归因到该机制。

## Assumptions

- **数据与协议**：复用 022 的 formal protocol、append-only Ledger、LoCoMo 1,540 answerable 与
  现有 store 资产；协议 cap、hybrid retrieval、固定 answerer（Qwen3.6-35B-A3B-FP8）+ judge
  （deepseek-v4-flash）、3 次答题多数（与 024/025/026 配对协议一致）。
- **承接资产**：022 的 `memory/evidencecompiler/`、`cmd/locomo-bench/compiler_eval.go` 与
  `eval_compile_bridge.go` 可直接承接；027 在 harness 侧实现解析器，不重写引擎。
- **supersede 判定方式**：V1 用确定性主题判定（同一实体的多次提及按 event_date 排序，冲突时
  以时间序判定 superseded）；不引入 LLM 判定。若确定性判定在配对中暴露明显误判，记录为已知
  边界而非 V1 阻塞项。
- **时间来源**：复用 022 Ledger 已索引的 event_date 时间信息；不新增写侧时间 schema。
- **判断基准**：时间解析的成功以"同预算下端到端正确率"为准，不以 coverage/recall 等代理指标
  作 verdict（008 教训）。
- **范围**：027 是研究实验（默认关），不承诺 productization；只有配对通过才讨论进入默认路径。
- **文献对标**：APEX-MEM（2604.14362）的 +14~25pp temporal 是**跨栈**证据（其 answerer 为
  GPT5/GPT4o，与 engram 不同），只提供方向性支持；engram 固定栈下的增益必须独立配对验证，
  不得外推 APEX-MEM 的绝对分差。

## Out of Scope

- 重新设计或实现 022 的 Evidence Ledger、表示、候选冻结、deterministic Compiler、Bundle
  validator、token 门、fallback 或 harness 主路径。
- 改变 retrieval、top-k、embedding、rerank、候选预算、token cap、answerer、judge、作答次数
  或 benchmark 分母。
- 写侧 consolidation / overwrite / fact-version schema（保留 append-only，027 只读消费）。
- 图存储、property graph、GraphSQL、SQL graph 遍历（APEX-MEM 的 GraphSQL 是可选的独立研究
  对照，不进 V1）。
- 检索侧 temporal 杠杆（时间窗召回、时序重排、补检）——六次 NO-GO 已证伪，不重复。
- LLM 判定事实主题/演化关系、付费云 reranker/recall/Planner/answerer 作为正式或推荐涨点路径。
- Episode、Event/State、Scene、Profile、graph、conditional refetch 或跨 namespace 推理。
