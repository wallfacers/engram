# Feature Specification: 双基准查询期证据编译架构

**Feature Branch**: `022-benchmark-parity-memory-architecture`

**Created**: 2026-07-29

**Status**: Draft

**Input**: User description: "基于 LoCoMo 与 LongMemEval 的高分系统和受控机制证据，
以不可损失 Evidence Ledger、语义 Episode 视图和查询期 Evidence Compiler 为核心；
Event、Scene、Profile、graph 只作为可重建 projection 独立消融。同一公开架构最终至少
严格超过 Mem0 托管平台自报的 LoCoMo 92.5% 和 LongMemEval 94.4%，且涨分不能来自
更宽松 judge、更强 answerer、付费云 reranker 或上下文预算膨胀。"

## Clarifications

### Revision 2026-07-29

本次论文复核后的用户决策取代本特性同日较早的冲突澄清；被取代的结论不再构成
需求或完成条件：

- 核心结构是不可损失 Evidence Ledger、Semantic Episode View 和 Query-time
  Evidence Compiler，不是必须逐层经过的 L0→L3 记忆体系。
- 每条显式摄入的原始消息具有稳定 Evidence 身份；episode/session 只负责分组。
  没有上游对话的直接写入以调用本身作为 self-evidence，保持既有写入兼容。
- 普通删除采用可恢复 tombstone；显式隐私/合规 purge 清除原文和可恢复副本，并使
  无有效来源的 projection 失效或重建。
- Event、Scene、Profile、current-state 和 graph 都是可重建 projection；必须先做
  独立消融，不能因“架构完整”预先建设或默认启用。
- 022 不替换、不迁移、不退役 003 graph 契约。003 只能在后续实验中作为可关闭的
  候选扩展或 projection。
- 首轮只允许在明确缺少 entity、时间段或第二个操作数时补检一次。Answerer 只在最终
  Evidence Bundle 完成后调用一次。
- 上下文使用实际 tokenizer 精确计数；每个实验冻结相同 token cap，并可预注册多个
  预算点，不再把单一 4k cap 当作普遍架构规则。

## Background and Scope

engram 当前以原子事实和多信号 RRF 为主要检索结构。已有预算消融显示：扩大 answer
context 可以提高总体分数，但在预算对齐后，multi-hop 与 temporal 仍显著落后。单独增加
entity association、rerank、multi-query、时间打分和 consolidation 等杠杆也没有形成
可复现的默认收益。

论文复核没有证明“高分系统都依赖完整层级图”。更强的机制证据指向三个更窄的问题：

1. 语义分段可能比固定字符、固定消息或固定 token 分段更适合作为检索单元；
2. 固定候选池之后的 evidence planning、span extraction、去噪和预算组装会显著影响
   候选能否被 answerer 使用；
3. 压缩是否有益取决于原始证据能否装入预算，而不是固定的 write-time consolidation。

本特性首先区分“候选没有证据”和“候选已有证据但上下文编译失败”。它覆盖原始 Evidence
的可靠保存与来源契约、三种表示的同预算对照、固定候选 Evidence Compiler、可审计
Grounded Trace、精确 token Evidence Bundle，以及必要时的一次定向补检。

Event/State、Scene、Profile 和 graph 只在前述核心实验之后按剩余错误独立验证。它们不是
V1 请求必须经过的层，也不是为了宣告架构完整而必须交付的结构。若核心路径已达成双基准
目标，后续 projection 可以不建；若未达成，只有通过预注册 stop/go 门的 projection 才能
进入默认路径。

本特性不承诺跨 namespace 推理、超大规模语料、在线集群、托管模型默认路径或通过改变
评测口径取得名义涨分。

## User Scenarios & Testing *(mandatory)*

本特性的主要用户是维护记忆质量的研究者，以及把 engram 嵌入宿主的集成者。研究者需要
定位错误发生在表示、候选召回还是证据编译；集成者需要新增路径仍然离线、可审计、可降级
且不破坏既有搜索合同。

### User Story 1 - 不可损失、可核验的 Evidence Ledger (Priority: P1)

集成者显式摄入对话后，需要每条原始消息具有稳定来源身份。事实、episode 和其他
projection 可以被重建或删除，但不得在抽取、合并、curation 或摘要时静默覆盖原文。

**Why this priority**: Query-time Compiler 只有在 source 可定位、可引用时才能证明没有
凭空补写证据；它也是表示对照和错题归因的共同地基。

**Independent Test**: 摄入含多消息事实、直接写入、修订、删除和两个 namespace 的夹具，
验证来源稳定、原文未被派生流程修改、删除闭包正确且跨域读取为零。

**Acceptance Scenarios**:

1. **Given** 一条事实由两条消息共同支持，**When** 查看其来源，
   **Then** 两条消息均以稳定 source ID 定位，原文与 speaker/time 保持不变。
2. **Given** 调用方直接写入事实而没有上游对话，**When** 保存并搜索，
   **Then** 该调用作为 self-evidence 正常工作，不因缺少 conversation turn 失败。
3. **Given** Evidence 被普通删除且仍有 projection 引用，**When** 查询，
   **Then** Evidence 立即退出检索、保留可恢复 tombstone，已无其他有效支持的
   projection 暂停服务。
4. **Given** Evidence 收到显式隐私 purge，**When** 操作提交，
   **Then** 原文和可恢复副本被清除，依赖项失效或重建，只留下不含原文的处置审计。
5. **Given** 两个 namespace 使用相同 source ID，**When** 读取来源，
   **Then** 当前 namespace 之外的 Evidence 可见数为零。

---

### User Story 2 - 同候选、同预算选择记忆表示 (Priority: P1)

研究者需要在改变查询策略之前，判断固定字符块、raw-turn window 与 semantic episode
哪种表示更能保存可回答证据。Episode 必须是可重建视图，而不是新增一层不可替代的真相。

**Why this priority**: 语义分段是外部论文中较干净的表示证据。先隔离表示变量，可以避免把
候选变化、预算变化和 Compiler 收益混在一起。

**Independent Test**: 在冻结数据、候选规则、answerer、judge 和 token cap 后，对三个表示
运行相同题目，比较 evidence coverage、预算截断和逐题答案差异。

**Acceptance Scenarios**:

1. **Given** 同一组原始对话和查询，**When** 比较当前约 900-character chunk、
   raw-turn window 与 semantic episode，**Then** 三个实验臂使用相同候选预算和
   answer-context token cap，并保存逐题配对结果。
2. **Given** 一个 semantic episode 由多条消息组成，**When** 查看或重建它，
   **Then** 它保留完整 source ID 列表和语义边界，删除该视图不会删除 Evidence。
3. **Given** 某表示只提高一个 benchmark、显著回退另一个，
   **When** 作出默认选择，**Then** 该表示保持实验性，不能成为共同默认。
4. **Given** 没有任何新表示通过预注册门槛，**When** 阶段结束，
   **Then** 系统保留既有表示并记录负结果，而不是为完成架构强行切换。

---

### User Story 3 - 固定候选的 Query-time Evidence Compiler (Priority: P1)

研究者需要在不改变检索、不发起第二次查询的条件下，把固定候选编译成更可回答的证据包。
Compiler 先声明 Evidence Need，再执行有来源的保留、抽取、丢弃、合并或 source recovery。

**Why this priority**: 当前错题可能不是候选召回失败，而是按条数装填、证据碎片化或预算
浪费。固定候选实验能直接测量 answer-facing evidence contract 的贡献。

**Independent Test**: 冻结每题候选 ID，依次比较现有装填、精确 token relevance packer、
deterministic extractive compiler 和可选本地 compiler；验证所有输出来源、字符偏移、
token cap 和一次 answerer 调用。

**Acceptance Scenarios**:

1. **Given** 候选已包含回答所需全部证据，**When** 编译，
   **Then** Trace 声明所需 entity、time、operands、list cardinality 或 update state，
   并输出满足这些需求的 Evidence Bundle。
2. **Given** 必要原文可以装入 token cap，**When** 编译，
   **Then** 原文被优先保留，不进行无必要的生成式压缩。
3. **Given** 原文无法全部装入 cap，**When** 执行 `EXTRACT`，
   **Then** 每个 span 都绑定 source ID 和有效字符起止偏移，且可从原文精确复原。
4. **Given** 需要 `MERGE` 多条证据，**When** 生成合并内容，
   **Then** 每个生成句分别引用有效 source IDs；无法验证的句子不进入 Bundle。
5. **Given** Compiler 想添加候选中不存在的事实，**When** 没有 source ID，
   **Then** 无来源 `ADD` 被拒绝，并退化到有来源的 extractive 输出。
6. **Given** 可选本地 compiler 未配置、失败或输出不合法，**When** 查询，
   **Then** deterministic extractive packer 接管，基础查询继续成功。
7. **Given** Bundle 超过冻结 token cap，**When** 校验，
   **Then** answerer 不被调用；系统继续按可审计优先级裁剪或返回预算不足。

---

### User Story 4 - 独立验证 Event/State projection (Priority: P2)

在核心表示与 Compiler 仍留下 temporal/update 错误时，研究者需要分别判断结构化 event、
日期算子和 source-turn recovery 是否真正带来收益，而不是一次上线后把 bundle 差值归给
“事件层”。

**Why this priority**: 外部 event 消融具有强模型依赖。它值得验证，但证据不足以支持全局
预建或默认启用。

**Independent Test**: 只在预注册 temporal/update 子集上，以当前时间字段为对照，分别
开关 event object、日期算子和 source recovery，并保持其余条件不变。

**Acceptance Scenarios**:

1. **Given** 核心路径的 temporal/update 错题已分类，**When** 开始 event 实验，
   **Then** event object、日期算子和 source recovery 各有独立实验臂与逐题结果。
2. **Given** 某 event 由多条 Evidence 派生，**When** 查看结果，
   **Then** event 可回到全部来源，且清空 event projection 后可从 Ledger 重建。
3. **Given** event 实验未通过 stop/go 门，**When** 结束阶段，
   **Then** event 保持默认关闭，基础查询和 Evidence Ledger 不受影响。

---

### User Story 5 - 一次缺口补检与窄用途 projection (Priority: P3)

在固定候选 Compiler 已证明有效、但 Trace 指出明确证据缺口时，研究者需要最多一次定向
补检。只有剩余错题给出明确依据时，才分别测试 Scene、Profile 或 graph。

**Why this priority**: 迭代和复杂 projection 都会增加变量与成本，必须在核心路径之后根据
可观察缺口引入。

**Independent Test**: 对缺 entity、时间段和第二操作数的夹具验证一次定向补检；分别在
跨 session、preference/current-state 和缺桥子集上验证 Scene、Profile、graph 的独立
开关和同预算对照。

**Acceptance Scenarios**:

1. **Given** 首轮 Trace 已满足全部 Evidence Need，**When** 编译完成，
   **Then** 系统不发起补检。
2. **Given** Trace 明确缺少 entity、时间段或第二操作数，**When** 允许补检，
   **Then** 系统只发起一次携带该结构化 gap 的定向检索，并计入累计预算。
3. **Given** 首轮只有低置信度但没有可描述 gap，**When** 评估补检，
   **Then** 系统不执行泛化 query rewrite 或无限扩大召回。
4. **Given** Scene、Profile 或 graph 未通过各自目标子集的独立门禁，
   **When** 发布默认 recipe，**Then** 对应 projection 不存在于默认请求路径。
5. **Given** 后续 graph 实验开启，**When** 查询，
   **Then** 003 既有合同保持不变，graph 仅提供可关闭、带来源的候选扩展。

### Edge Cases

- 原始消息没有产生事实或 episode 时，Evidence 仍被保留，不能伪装成已抽取事实。
- 同一事实跨 session 获得多个支持来源时，全部实际来源都必须保留。
- source ID 存在但原文已被 purge 时，Compiler 不得把 tombstone 或审计记录当作证据。
- 字符偏移越界、UTF-8 边界无效或抽取 span 与原文不一致时，该 action 必须失败并降级。
- `MERGE` 中只有部分句子可验证时，不可验证句子被删除，而不是借共享引用通过校验。
- list 查询无法确定预期 cardinality 时，Trace 必须标记未知，不能伪造“证据充分”。
- 候选没有黄金证据时，运行标记 candidate miss；候选有证据但 Bundle 遗漏时，标记
  compiler miss，两者不得合并统计。
- 固定 token cap 小于一条不可拆分证据时，系统返回预算不足并记录实际需求，不得超 cap。
- Event、Scene、Profile 或 graph projection 缺失、过期或损坏时，系统只降级该视图，
  Ledger 和基础检索继续工作。
- 评测数据版本、分母、模型、judge、prompt 或预算 fingerprint 与冻结协议不一致时，
  运行不得进入可比较结果集。
- namespace 或 source ID 碰撞时，跨 namespace 可见记录必须为零。
- 超过单用户约 100k 记忆时，系统必须如实报告超出本特性保证范围。

## Requirements *(mandatory)*

### Functional Requirements

**评测与因果归因**

- **FR-001**: 在算法变更前，系统 MUST 在无损摄入路径上刷新 LoCoMo category 1–4
  和 LongMemEval-S full 500 基线，并保存逐题结果。
- **FR-002**: 每个可比较实验 MUST 固定并记录数据版本、分母、answerer、judge、
  prompt fingerprint、抽取配置、候选规则、候选预算、answer-context token cap 和
  聚合规则。
- **FR-003**: 实验 MUST 依次隔离表示、固定候选 Compiler、Event projection、一次补检
  和其他窄用途 projection；后一阶段不得掩盖前一阶段未解释的变化。
- **FR-004**: 每题 MUST 记录黄金证据是否存在于冻结候选池，并将 candidate miss 与
  compiler miss 分开报告。
- **FR-005**: 每个候选机制 MUST 可独立启用、禁用和消融；未通过预注册 stop/go 门的
  机制 MUST 默认关闭。达到双基准目标后，系统 MUST NOT 为“架构完整”强制建设后续
  projection。

**Evidence Ledger 与可重建视图**

- **FR-006**: 显式摄入对话时，系统 MUST 将每条原始消息保存为具有稳定 source ID 的
  Evidence，并保留 namespace、session、speaker、原文、发生时间和记录时间。
- **FR-007**: 抽取、合并、摘要、curation 和 projection 重建 MUST NOT 覆盖或物理替代
  active Evidence 原文。
- **FR-008**: 直接写入且没有上游对话的内容 MUST 以调用本身作为 self-evidence 正常
  保存、搜索和审计。
- **FR-009**: Fact、Episode 及其他 projection MUST 保留全部实际支持 source IDs；
  episode/session 分组不得替代消息级来源。
- **FR-010**: 普通删除 MUST 创建可恢复 tombstone 并立即排除 Evidence；显式隐私或
  合规 purge MUST 清除原文和可恢复副本，使无有效支持的依赖项失效或重建，只保留
  不含原文的处置审计。
- **FR-011**: Evidence 和所有 projection MUST 保持 namespace 隔离；跨 namespace
  关系、source recovery 和读取默认禁止。
- **FR-012**: Semantic Episode、Atomic Fact、Event/State、Scene、Profile 和 graph
  MUST 是可关闭、可清空、可从有效 Evidence 重建的视图，不得成为不可审计的第二真相。
- **FR-013**: 表示 bake-off MUST 在相同数据、候选规则和 token cap 下比较当前约
  900-character chunk、raw-turn window 与 semantic episode，并保存 evidence coverage、
  预算截断和逐题答案结果。

**Query-time Evidence Compiler**

- **FR-014**: 固定候选 Compiler 实验 MUST 冻结每题候选 ID，不得改变检索或发起补检。
- **FR-015**: Compiler MUST 生成可审计 Evidence Need，至少能表达 entity、time、
  operands、list cardinality、update state 和未满足 gap。
- **FR-016**: V1 Compiler MUST 只允许 `KEEP`、`EXTRACT(span)`、`DROP`、`MERGE` 和
  `FETCH_SOURCE`；无来源 `ADD` MUST 被拒绝。
- **FR-017**: 每个 `EXTRACT` MUST 绑定 source ID 和有效字符起止偏移，并能从规范原文
  精确复原。
- **FR-018**: 每个 `MERGE` 输出句 MUST 分别绑定一个或多个有效 source IDs；任何无法
  验证来源的句子 MUST 不进入最终 Bundle。
- **FR-019**: `FETCH_SOURCE` MUST 只沿候选已有 lineage 读取确定 source ID，不得借该
  action 发起新的语义检索。
- **FR-020**: Evidence Bundle MUST 使用实际 answerer tokenizer 精确计数，并 MUST
  不超过该实验冻结的 token cap；估算 token 或按条数装填不得作为最终边界判断。
- **FR-021**: 当必要原文能装入 token cap 时，Compiler MUST 优先保留原文；只有原文
  装不下时，才可执行 extractive compression 或有逐句来源的 merge。
- **FR-022**: Grounded Trace MUST 记录 Evidence Need、候选与 source IDs、所执行 action、
  span、时间/冲突关系、token 取舍、丢弃原因和仍未满足的 gap。
- **FR-023**: 可选 compiler 不可用、失败或输出校验不通过时，系统 MUST 退化到
  deterministic extractive packer，基础查询不得整体失败。
- **FR-024**: Answerer MUST 只在 Evidence Bundle 通过来源和 token 校验后调用一次；
  planning、编译和补检阶段不得调用 answerer 试答堆分。

**有界补检与 Optional Projections**

- **FR-025**: 只有 Grounded Trace 明确缺少 entity、时间段或第二操作数时，系统才可
  发起最多一次定向补检；低置信度本身不足以触发补检。
- **FR-026**: 补检 MUST 携带结构化 gap，并与首轮共享预注册的累计候选、token 和调用
  预算；达到任一上限后 MUST 停止并报告剩余 gap。
- **FR-027**: Event/State 实验 MUST 在 temporal/update 子集上分别消融当前时间字段、
  独立 event object、日期算子和 source recovery，不得把组合差值归给单一组件。
- **FR-028**: Scene MUST 仅以跨 session 候选扩展为首要假设，Profile MUST 仅以
  preference/current-state 为首要假设，graph MUST 仅以缺桥或局部关系扩展为首要假设；
  每项均须独立开关和同预算门禁。
- **FR-029**: 022 MUST 保持 003 graph 的既有数据与对外合同不变，不要求迁移、退役或
  扩张为统一 typed graph。

**兼容、离线与规模**

- **FR-030**: 既有默认写入和搜索合同 MUST 保持向后兼容；新结构化路径必须通过版本化
  引擎合同提供，适配层不得重写记忆算法。
- **FR-031**: 默认路径 MUST 在无外网环境完整运行；托管服务不得成为写入、检索、
  Evidence Compiler、预算校验或冲突处理的必需条件。
- **FR-032**: 付费托管 reranker 或 recall model MUST NOT 作为默认能力、推荐配置或
  正式涨分杠杆。
- **FR-033**: 缺少任一 optional projection、embedding、compiler 或检索信号时，系统
  MUST 逐能力降级，基础本地写入与搜索 MUST 继续工作。
- **FR-034**: 新能力 MUST 维持单用户约 100k 记忆的诚实规模边界，不得借本特性承诺
  ANN、百万 token 语料或集群级能力。

**双基准验收**

- **FR-035**: LoCoMo 与 LongMemEval-S MUST 使用同一个公开记忆架构和默认产品 recipe；
  数据集适配差异必须单列，不能用两套私有架构拼接结果。
- **FR-036**: 机制 A/B 的 answerer、judge、冻结候选条件和 answer-context token cap
  MUST 相同；仅提高模型能力、judge 宽松度、top-k 或 token 用量不得计作机制收益。
- **FR-037**: 正式结果 MUST 报告总体与分类别准确率、逐题输出、独立重复或确定性证明、
  置信区间、配对统计、evidence coverage、candidate/compiler miss、平均与 p95 token、
  检索次数、延迟、成本和全部预注册消融。
- **FR-038**: 任何默认改动 MUST 在 LoCoMo 与 LongMemEval-S 上均不相对冻结基线显著
  回退；仅单 benchmark 涨分的机制只能保持实验性。
- **FR-039**: 达成数值目标不得表述为受控优于 Mem0，除非数据、模型、judge、prompt、
  候选预算、上下文预算和聚合规则均已同栈对齐。

### Key Entities

- **Evidence Record（证据记录）**：显式摄入的一条原始消息，或没有上游对话时的一次
  直接写入；具有稳定 source ID、namespace、session、speaker、原文、发生/记录时间和
  active、tombstoned、purged 生命周期。
- **Evidence Projection（证据视图）**：由有效 Evidence 派生、可独立清空和重建的查询
  结构；保存完整 lineage，不充当第二份规范真相。
- **Semantic Episode（语义 Episode）**：按语义边界组织的完整事件叙述及 source ID
  集合，用于与固定字符块和 raw-turn window 做表示对照。
- **Atomic Fact（原子事实）**：面向高精度候选导航的自包含记忆；它回链 Evidence，
  但不是 answerer 必须接收的唯一文本。
- **Evidence Need（证据需求）**：某次查询为作答仍需满足的 entity、time、operands、
  cardinality、state 和 gap。
- **Compiler Action（编译动作）**：对固定候选执行的 KEEP、EXTRACT、DROP、MERGE 或
  FETCH_SOURCE，包含来源、span、理由和校验结果。
- **Grounded Trace（有来源轨迹）**：记录 Evidence Need、候选、action、引用、时间/
  冲突关系、预算决策和未满足 gap 的审计产物。
- **Evidence Bundle（证据集合）**：通过来源与精确 token 校验后交给 answerer 的最终
  上下文。
- **Projection Experiment（视图实验）**：冻结目标子集、控制变量、机制开关、stop/go
  门和逐题结果的 Event、Scene、Profile 或 graph 实验。
- **Evaluation Run（评测运行）**：冻结数据、模型、judge、prompt、候选、预算、机制
  开关与逐题产物的可复现实验单元。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 无损摄入路径下的 LoCoMo category 1–4 与 LongMemEval-S full 500 基线
  刷新完成，全部题目均有可追溯逐题结果和冻结配置 fingerprint。
- **SC-002**: 同一默认架构在 LoCoMo category 1–4 上至少答对 1,425/1,540 题，
  报告准确率不低于 92.6%。
- **SC-003**: 同一默认架构在 LongMemEval-S full 500 上至少答对 473/500 题，
  报告准确率不低于 94.6%。
- **SC-004**: SC-002 与 SC-003 均在无付费托管 reranker/recall model、无默认云依赖
  的路径达成，并使用同一公开产品架构和 recipe。
- **SC-005**: 表示 bake-off 对三种表示的 evidence coverage、预算截断、总体/分类别和
  逐题答案结果覆盖率为 100%；只有通过预注册双基准门的表示被选为默认。
- **SC-006**: 固定候选 Compiler 的所有实验臂候选 ID 一致率为 100%，所有 Bundle 的
  实际 token 均不超过对应冻结 cap；candidate miss 与 compiler miss 分开报告率为 100%。
- **SC-007**: 进入最终 Bundle 的 extractive span 可从 source 原文精确复原率为 100%，
  生成句有效 source 引用覆盖率为 100%，无来源 `ADD` 接受数为 0。
- **SC-008**: 每个进入默认路径的机制在同 answerer、judge、候选条件和 token cap 下，
  相对前一冻结臂具有预注册的端到端配对证据；未过门机制默认启用数为 0。
- **SC-009**: 每次查询的补检次数不超过 1，answerer 调用次数不超过 1；无结构化 gap
  的补检次数为 0。
- **SC-010**: 100% 的 active Fact、Episode 和 optional projection 能回到仍有效的
  source chain；可服务的悬空来源和跨 namespace 来源读取均为 0。
- **SC-011**: optional projection 和可选模型的故障注入证明逐能力降级；基础本地写入
  与搜索成功率为 100%。
- **SC-012**: 未启用新路径时，既有默认写入与搜索的确定性 parity、错误语义和 namespace
  隔离测试全部通过。
- **SC-013**: 正式结果完整记录两 benchmark 的逐类别分数、逐题产物、统计检验、证据
  错误分类、预算、检索次数、延迟、成本和全部预注册消融，缺失字段数为 0。

## Assumptions

- 评测维护者可以取得 LoCoMo category 1–4 与 LongMemEval-S cleaned full 500 数据，
  但数据集本体和运行凭据不进入仓库。
- 当前结果只作为刷新前参考；022 的比较基线必须在冻结协议和无损摄入 store 上重建。
- Mem0 托管平台的 92.5% / 94.4% 是跨栈数值北极星，不是可同栈复现的科学基线。
- 原始对话只在调用方显式 ingest 时进入 Ledger；直接 write/add 不隐式抓取宿主会话。
- 精确 token cap 是预注册实验变量。产品默认值由实验与规划阶段决定，不假设任意单一
  cap 在所有问题和 benchmark 上普遍最优。
- 专用、可本地部署的 memory compiler 可能是长期达到 90%+ 的必要杠杆，但 V1 不把
  模型设为硬依赖；deterministic extractive packer 是必须可用的降级路径。
- 003 graph 保持原合同与数据，不因 022 自动重开其历史负结果；后续若作为候选扩展，
  必须重新证明独立、同预算收益。
- Event、Scene、Profile、current-state 和 graph 均为条件性后续实验。核心路径达标后
  可以不建；未通过独立门禁时不得默认启用。
- ANN、百万 token 语料、跨 namespace 推理、托管 SaaS 和宿主专用策略不在本特性范围。
- 若数值目标只能依靠更强 answerer、不同 judge、更多 token、更多 answerer 调用或
  付费云 rerank 达成，则本特性未达成。
