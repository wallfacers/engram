# Feature Specification: 跨消息语义聚类 episode 表示

**Feature Branch**: `025-semantic-episode-cluster`

**Created**: 2026-08-01

**Status**: Draft

**Input**: User description: "跨消息语义聚类 episode 表示:在 022 已交付的 episode 引擎资产基础上,实现跨消息语义聚类,把语义相关的多跳证据(跨原始消息/跨 session)聚类成 episode 候选,缩小同预算对 MemOS 的信息密度差距。"

## Clarifications

### Session 2026-08-01(承接决策)

- **承接 022 episode 引擎资产**:022 已交付 `memory/episode.go`(EpisodeSegmenter/EpisodeStore 接口)、`cmd/locomo-bench/representation_eval.go`(ReprChunk900/ReprRawTurnWindow/ReprSemanticEpisode 三渲染器)、`evalRankedAnchor` 锚定机制与 v7 迁移中 episode 表。**但注意(2026-08-01 核查修正)**:022 的 T046/T047 对应单测**已存在且通过**(`episode_test.go` 13 tests、`representation_eval_test.go` 14 tests、`representation_integration_test.go` 6 tests);真实的未验证缺口是 **`RebuildSession` 从未在正式 eval 被调用,episode 投影从未构建过**。本 feature **承接并复用**这些资产(代码+测试),不做重复实现,焦点是"接线验证 + 跨 session 扩展"。
- **核心差异:同 session 连续边界 → 跨消息语义聚类**:022 原设计的 EpisodeSegmenter 只做"同 session 连续 Evidence 边界"(一段对话内按边界切段)。024 四臂负结果(neighbor_extend 的"共享 evidence 兄弟"实为同消息内 fact)证明:multi-hop 缺的不是同消息邻居,而是**跨原始消息/跨 session 的语义相关证据**。本 feature 把 episode 的定义从"时间连续段"改为"**语义相关证据簇**"——语义上围绕同一事件/话题、散落在不同消息甚至不同 session 的证据聚成一个 episode 候选。
- **与 024 负结果的衔接**:024 证明"共享原文的机械血缘"无效;本 feature 的跨消息语义聚类是**语义驱动**(离线信号:共享实体/关键词重叠为主,embedding cosine 为可选 overlay),不是血缘驱动。验证靶子明确指向 024 的 multi-hop 短板。
- **验证框架**:复用 022 三表示 bake-off 的"同 anchor 不同 renderer"设计(chunk_900 / raw_turn_window / semantic_episode 共用 ranked anchor artifact),在 022 冻结协议(cap 3600)下配对验证。

### 与 022 的关系

- 022 verdict 为 HOLD(artifact 部分有效,B1 正式 verdict 未达 CONTINUE)。本 feature 是 022 US2(semantic episode 表示)的**承接与扩展**,不是替换。
- 022 已证伪的方向不重复:write-time 融合写回(Retain or Consolidate 负)、同 session 连续边界的 episode 表示(未经验证即被本 feature 扩展)。
- 本 feature 与 022 共享 store 与协议资产(022.v1 protocol、lossless chunks、双基准)。

### Session 2026-08-01(聚类决策)

维护者委托决策,依据:① `evalRankedAnchor` 锚在检索命中的 fact/chunk 而非 episode;② 022 三渲染器全部渲染**原始 Evidence 文本**(不是压缩 fact 文本);③ MemOS 实证"命中 fact 沿血缘回收原始 span 喂 answerer,减 token 不丢信息"。

- **Q: 语义聚类作用于什么对象粒度 → A: 原始 Evidence 级(跨消息/跨 session 的相关证据簇)**,不是 fact 级。命中 fact/chunk 后把所属 episode 的**原始证据叙事**(speaker: content)按配额带入 answerer。理由:fact 级聚类=语义化 neighbor_extend(024 已证伪,压缩文本丢失原始措辞);原文叙事与 022 三渲染器同范式,可直接复用 `semanticEpisodeRenderer` + `EpisodeStore`。
- **Q: 聚类构建时机 → A: 全量可重建投影**,扩展 EpisodeStore 为跨 session 语义聚类 builder,config-hash 幂等重建(可丢弃可重建);不做查询时在线聚类(不确定、难冻结、难 bake-off),不做写入时增量。
- **Q: 无 embedding 端点时的离线信号 → A: 共享实体重叠 + 关键词/主题重叠**(确定性,阈值可配),embedding cosine 仅作可选 overlay(默认关)。与 024 trigram 抑制(0.09% 触发率)区分:聚类是**分组动作**而非抑制动作,阈值更宽松、面向"同一事件/话题"。
- **Q: episode 有界口径 → A: 每 episode 证据数上限可配**(默认值见 research.md Decision 2),跨 session 默认允许;跨 namespace 保持隔离。
- **Q: 与 022 EpisodeSegmenter 关系 → A: 原 segmenter(同 session 连续边界)保留仅用于验证补全(T046/T047),不作 025 生产路径;025 用新的 SemanticClusterer(跨消息语义聚类)作为实际构建器。**

## Background and Scope

### 问题

budget-ablation(023)证明 engram 同预算对 MemOS 的差距(1059 tok 下 76.85% vs 82.47%)由**信息密度**驱动。024 四臂证明"共享原文的机械血缘"(write_dedup/neighbor_extend)不是补差距的杠杆——multi-hop 类目(84.6% control)最缺的是**跨消息的语义相关证据**:一个 multi-hop 问题的答案往往散落在几条不同的原始消息里,而现有检索把每条消息切成独立 chunk/fact,命中一个就只带一个,answerer 看不到完整上下文。

### 目标

在不放宽预算、不引入付费云模型、不破坏 append-only Evidence Ledger 的前提下,用跨消息语义聚类提高"每个 answer token 携带的有效证据量":把语义相关的多跳证据(跨消息/跨 session)聚合成 episode 候选,让 answerer 在一次预算内看到完整事件脉络。

### 范围边界

- **在范围内**:跨消息语义聚类(语义驱动的 episode 构建);022 episode 引擎的验证补全(承接资产的测试);三表示 bake-off 配对验证;纯本地实现。
- **不在范围内**:LLM 融合写回、付费 reranker、图数据库引入、write-time 压缩/摘要、write_dedup/neighbor_extend 复用(024 已证伪)、compiler 正式化(022 H2,独立 feature)。
- 机制默认关(宪法 I/V);无 LLM 端时须有纯离线判定路径(宪法 V)。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 跨消息语义聚类,多跳证据一次到位 (Priority: P1)

用户查询命中一条 fact 时,系统把它所属的**语义 episode**(围绕同一事件/话题、散落在不同原始消息甚至不同 session 的相关证据)整体带入 answerer 上下文,让依赖多条证据的 multi-hop 问题在**不增加检索预算**的情况下看到完整事件脉络。

**Why this priority**: 这是本 feature 的信息密度核心。024 的 neighbor_extend 已证明"共享原文的机械兄弟"无效;跨消息语义聚类是替代它的语义驱动杠杆。

**Independent Test**: 构造一个"同一事件分散在多条消息、且语义相关"的 store,查询命中该事件的一条 fact 时断言:语义 episode 把其余跨消息相关证据带进候选,且默认关闭时行为与现状完全一致。

**Acceptance Scenarios**:

1. **Given** 一个 store 里同一事件(如"项目上线推迟")分散在 3 条不同原始消息、各含部分事实,**When** 查询命中其中一条事实,**Then** 语义 episode 把另外两条消息的相关证据一起带进 answerer 上下文(检索/候选不动,episode 投影在渲染前静态构建,渲染时按 anchor→lineage→episode 展开,有界)。
2. **Given** 语义聚类开启,**When** 命中 fact 所属 episode 的可扩展证据数超过上限,**Then** 扩展有界(上限可配),不无界放大候选/token。
3. **Given** 语义聚类关闭(默认),**When** 查询命中 fact,**Then** 上下文与现状完全一致(零行为变化)。

---

### User Story 2 - 022 episode 引擎验证补全,承接资产可信 (Priority: P1)

022 遗留的 episode 引擎(EpisodeSegmenter/EpisodeStore、三渲染器、锚定机制)在本 feature 被承接复用前,必须**验证其实现可用并接线**(而非补测试——T046/T047 对应单测已由 022 交付,13+14+6 个全部通过,见 research.md R0)。真实的未验证缺口是:**`RebuildSession` 从未在正式 eval 被调用,episode 投影从未构建过**。承接未接线的代码是风险:先确认单测覆盖设计,再补跨 session 能力测试,再扩展为跨消息语义聚类。

**Why this priority**: 承接 022 未接线的资产是风险。真实缺口是"从未在正式 pipeline 构建过 episode"而非"缺测试"。先钉住构建链路,再扩展。

**Independent Test**: 核实 022 交付的 episode/渲染单测覆盖设计且通过;为跨 session 语义聚类补 TDD 失败测试(跨 session 相关证据聚成同 episode、不相关不聚、有界截断确定性、config-hash 幂等、无 embedding 时纯离线可判定),断言承接后构建链路可用。

**Acceptance Scenarios**:

1. **Given** 022 遗留的 episode 引擎代码及其单测(已存在),**When** 核实其覆盖"同 session 连续边界"等设计且全部通过,**Then** 确认承接资产单测绿,无需重复补写;记录"`RebuildSession` 未接线"为真实缺口。
2. **Given** 三表示渲染器(chunk_900/raw_turn_window/semantic_episode)已有代码与 shared-anchor 测试,**When** 为跨 session 语义聚类补失败测试(SemanticClusterer),**Then** 断言跨 session 相关证据聚成同 episode、不相关不聚、有界确定性、config-hash 幂等、无 embedding 时纯离线可判定。
3. **Given** 承接验证完成,**When** 开始跨消息语义聚类实现,**Then** 新实现基于已验证的引擎与构建链路(RebuildAll),不重复造轮子。

---

### User Story 3 - 双基准配对验证,语义 episode 不回归基线 (Priority: P1)

维护者在 LoCoMo 与 LongMemEval-S 上、在**固定 answer-context token cap** 的 022 协议下,对"semantic episode 表示 vs chunk_900 基线"做配对消融,确认跨消息语义聚类是否带来 multi-hop 增益,且不回归当前基线。

**Why this priority**: 宪法 IV 的回归门 + 022/024 的配对纪律。负结果可接受,但必须归因干净。

**Independent Test**: 在 022 冻结协议下跑配对(chunk_900 基线 vs semantic_episode),repeats≥3,对比 overall 与分类别配对统计,尤其 multi-hop。

**Acceptance Scenarios**:

1. **Given** 022 冻结协议与基线,**When** 只开启 semantic_episode 表示,**Then** LoCoMo 的 overall 不显著回归基线,且 multi-hop 类别报告明细(预期有增益,负则记录负结果)。
2. **Given** 同上,**When** 报告结果,**Then** 分类别明细 + 配对统计 + token 记账齐全,归因到单一机制(表示差异)。
3. **Given** semantic_episode 与 chunk_900 对比,**When** 报告差异,**Then** 显式报告"同 anchor 不同 renderer"的设计,不把检索差异混入表示差异。

---

### Edge Cases

- **语义误聚类**:两条语义相近但事实不同的证据(如同一事件的进展阶段)被聚进同一 episode → 需可观测(审计聚类依据),不允许静默污染。判定依据必须是确定性的(实体/关键词重叠阈值 + 可选 embedding overlay),不得含不可复现的隐式信号。
- **聚类规模爆炸**:高频话题的语义邻居数量很大 → 聚类必须**有界**(每 episode 证据数上限可配,默认值见 research.md Decision 2),不无界放大候选/token。
- **跨 session 边界**:语义相关的证据跨 session → 聚类默认允许跨 session(需有界);跨 namespace 仍隔离。022 原 `RebuildSession` 的 session 边界语义被 SemanticClusterer 取代,但跨 session 聚类仍是**同一 namespace 内**的证据簇。
- **无 embedding 端点**:offline 降级模式下语义聚类退回纯离线信号(共享实体重叠 + 关键词/主题重叠,确定性阈值),不得依赖不可用的在线端点。
- **与 022 三表示的关系**:semantic_episode 与 chunk_900/raw_turn_window 是**表示层候选单元差异**(同一 anchor 不同渲染),不是检索差异——报告须区分表示与检索的贡献。025 只改 semantic_episode 一臂的 source closure(同 session 连续 → 跨消息语义簇),chunk_900/raw_turn_window 保持不动,保证配对归因干净。
- **append-only 保障**:episode 是**可丢弃可重建的投影视图**(类似 fact 投影),evidence ledger 保持 append-only 无损;聚类删除/重建不得删改原文。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 支持把语义相关的跨消息证据聚类为 episode 候选,聚类对象为**原始 Evidence 级**(候选单元是证据簇,不是 fact 文本)。主判定信号为**离线信号**(共享实体重叠 + 关键词/主题重叠,确定性、阈值可配)+ **可选语义 overlay**(embedding cosine,本地 sidecar 可选,默认关),任一达成即可聚(见 research.md Decision 1)。系统 MUST 提供无 embedding 端点的纯离线聚类降级路径。
- **FR-002**: 聚类出的 episode MUST 作为**候选单元**进入检索与 answerer 上下文(而非仅作展示);命中 episode 内任一 fact/chunk 时,episode 的整体上下文(原始证据叙事)可按配额带入。
- **FR-003**: 聚类 MUST 默认关闭;关闭时 MUST 与现状逐字节一致(零行为变化)。
- **FR-004**: 聚类 MUST 有界(上限可配,默认值取值与理由见 research.md Decision 2),不无界放大候选/token。
- **FR-005**: 聚类 MUST 保持 append-only Evidence Ledger 无损;episode 是可丢弃可重建的投影(全量重建、config-hash 幂等),删除/重建不得删改原文。
- **FR-006**: 聚类 MUST 记录可审计的判定统计(判定总数、聚类数、疑似误聚类数),供评估误聚类率。
- **FR-007**: 承接的 022 episode 引擎 MUST 在本 feature 中先核实单测覆盖并**接线验证构建链路**(T046/T047 对应单测已存在,真实缺口是 `RebuildSession` 未在正式 eval 接线),再扩展为跨消息语义聚类;未接线的承接代码不得直接用于正式实验。022 原 EpisodeSegmenter(同 session 连续边界)仅用于验证,025 生产路径使用新的 SemanticClusterer(跨消息语义聚类)。
- **FR-008**: 表示差异 MUST 与检索差异分离:semantic_episode 与 chunk_900 使用**同一 ranked anchor artifact**(022 设计),配对消融归因到单一表示变量。
- **FR-009**: 聚类 MUST 纯本地可运行,MUST NOT 依赖付费云模型或强制 LLM 调用;任何可选 LLM 判定 MUST 默认关。
- **FR-010**: 若经配对消融证明 semantic_episode 无收益或负收益,该机制 MUST 保持默认关并记录 verdict,不得进入默认路径(宪法 I/V)。
- **FR-011**: 跨 session 聚类 MUST 默认允许但有界;跨 namespace 访问 MUST 保持隔离(宪法 III)。

### Key Entities

- **Semantic Episode(语义 episode)**:围绕同一事件/话题、**跨原始消息/跨 session** 语义聚类而成的**原始 Evidence 证据簇**(候选单元为证据叙事,非 fact 文本);本 feature 的核心新候选单元,由 SemanticClusterer 全量重建。
- **EpisodeBoundary / EpisodeSegmenter(022 承接)**:同 session 连续边界的原始定义;仅用于验证补全,不作 025 生产路径(生产路径为 SemanticClusterer)。
- **Ranked Anchor(锚定,022 承接)**:检索命中的稳定候选锚点(fact/chunk);三表示围绕同一 anchor 展开不同 source closure。
- **Evidence Ledger(原始证据)**:append-only 消息级原文;任何聚类/投影都不得删改。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: semantic_episode 开启后,LoCoMo **multi-hop 类别**相对 chunk_900 基线有可测提升(配对统计,不显著回归 overall);multi-hop 的 answer-context 中跨消息证据覆盖提升可测。
- **SC-002**: LoCoMo 与 LongMemEval-S 的 overall 在**相同 token cap** 下不显著回归基线(宪法 IV);若 negative,以负结果记录而不进入默认路径。
- **SC-003**: 配对实验有分类别明细、配对统计与 token 记账;表示差异与检索差异被显式分离(同 anchor)。
- **SC-004**: 关闭 semantic_episode 时,双基准在 022 冻结协议下的结果与现状一致(回归门对照)。
- **SC-005**: 无 embedding 端点、无 LLM 的离线环境下,聚类的降级路径可完整运行并产出判定(宪法 V 的离线可退化)。

## Assumptions

- **数据与协议**:复用 022 的 `022.v1` 协议、lossless chunks、LoCoMo(1540 题)/LongMemEval-S(500 题)双基准与现有 store 资产。
- **承接资产**:022 的 `memory/episode.go`、`representation_eval.go` 等 episode 引擎代码可直接承接(接口与表结构可用);本 feature 补验证 + 扩展,不重写。扩展方向:EpisodeStore 增加跨 session 语义聚类 builder(SemanticClusterer),`semanticEpisodeRenderer` 一臂的 source closure 由同 session 连续段改为跨消息语义簇。
- **判断基准**:信息密度的成功以"同预算下的端到端正确率"为准,不以 coverage/recall 等代理指标作 verdict(008 教训)。
- **范围**:本 feature 是研究实验(默认关),不承诺 productization;只有双基准配对通过才讨论进入默认路径。
- **对标**:对 MemOS 的"效果更好"定义为"同预算下达到或接近其信息密度";LLM 融合写回、图数据库、付费 reranker 均明确排除。
- **依赖**:依赖 022 已交付的 Evidence Ledger、episode 引擎与正式协议资产;若承接资产无法通过补全测试,本 feature 实验不启动。
