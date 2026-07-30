---
title: 超越 Mem0 的查询期证据编译架构探索
summary: 本文提出以不可损失 Evidence Ledger、语义 Episode 视图和查询期 Evidence Compiler 为核心的双基准路线；Event、Scene、Profile 与 graph 均为待独立消融的可重建 projection。
status: proposed
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-30
canonical_for: [benchmark-parity-memory-architecture]
tags: [product, exploration, memory, benchmarks, evidence-compiler]
---

# 超越 Mem0 的查询期证据编译架构探索

本文是**未实现**的架构探索。目标是让 engram 在 LoCoMo 和 LongMemEval-S 上形成
论文级、可复现、local-first 的结果，并在数值上严格超过 Mem0 托管平台自报的
92.5% / 94.4%。外部依据见[长期记忆系统成绩与机制证据登记](../../research/high-scoring-memory-systems.md)，
当前能力与分数仍以[能力边界](../capabilities.md)和[评测结果](../../evaluation/results.md)
为准。

## 纠偏后的结论

高分论文并没有证明“完整 L0→L3 层级图”是共同充分条件。更可信、也更容易受控验证的
方向是：

> 不可损失 Evidence Ledger + Semantic Episode View + Query-time Evidence Compiler。

主要智能先放在**检索之后、作答之前**。Event、Scene、Profile、current-state 和 graph
不再组成请求必须逐层经过的记忆体系，而是从 Ledger 重建的 projection。每种 projection
只有在独立消融证明净收益后，才可加入默认路径。

这项结论明确取代此前探索中的两项假设：

- 不预先建设强制的 L0 Evidence → L1 Fact → L2 Event → L3 Derived 全层级；
- 不用 022 替换或扩张 003 graph 契约为统一数据模型。

## 目标与声明边界

### 数值北极星

- LoCoMo category 1–4：至少 1,425/1,540，即 **≥92.6%**。
- LongMemEval-S full 500：至少 473/500，即 **≥94.6%**。
- 两个 benchmark 必须由同一公开架构和默认 recipe 达成。

这是跨栈数值门，不自动构成“受控优于 Mem0”的科学声明。后者还要求同数据版本、
answerer、judge、prompt、候选预算、answer-context token cap 和聚合规则。仅通过
更强 answerer、更宽松 judge、更大 top-k 或更多上下文越线，不计作记忆架构收益。

### 论文级证据包

达标报告至少包含数据版本、分母、逐类别与逐题结果、全部模型与 prompt fingerprint、
候选数、answer-context 平均/p95 token、检索调用次数、延迟、成本、置信区间和配对
检验。评测配置变更必须与算法变更分开提交。

## 推荐结构

```text
Append-only Evidence Ledger
  ├─ Raw Turn Index          原文、speaker、time、stable source ID
  ├─ Semantic Episode View  语义边界、完整事件叙述、source IDs
  ├─ Atomic Fact View       高精度候选导航，不是唯一真相
  └─ Optional Projections   Event/State · Scene · Profile · Graph

query
  → 固定宽候选召回（保留现有 union + RRF）
  → Evidence Need
  → Evidence Compiler
  → Grounded Trace
  → Exact-token Evidence Bundle
  → 必要时最多一次证据缺口补检
  → answerer（一次）
```

“Append-only”指普通写入和派生流程不能覆盖或静默丢失原始证据，不否定用户明确要求的
删除。普通删除先 tombstone 并停止检索；隐私/合规 purge 必须能物理清除原文和所有
派生 projection，再留下不含敏感内容的审计结果。

## 数据契约

### Evidence Ledger

Ledger 的最小逻辑记录是 message/turn 级 Evidence，包含：

- stable source ID、namespace、session、speaker 和原文；
- occurrence time 与记录时间；
- 来源类型和必要的调用方 provenance；
- tombstone/purge 状态。

抽取、合并、curation 或重建 projection 都不得把它物理替代。调用方直接 `write/add`
且没有上游 conversation turn 时，该调用本身是有来源的 self-evidence；这保留现有
写入兼容性。被禁止的是 compiler 凭空生成、又无法绑定 source ID 的新事实。

### Semantic Episode View

Episode 按语义边界组织一段完整事件叙述，并保存组成它的 source IDs 和边界。它是
可丢弃、可重建的查询视图，不是新的事实真相层。第一轮实验必须把它与当前约
900-character chunk、raw-turn window 放在同一候选与 token cap 下比较。

### Atomic Fact View

Atomic Fact 继续承担 keyword、semantic 和 entity 的高精度导航，但每条事实必须能
回到支持它的 Evidence。事实命中不等于必须把该事实文本直接送入 answerer；Compiler
可以沿 source ID 回收更完整的原文 span。

### Optional Projections

Event/State、Scene、Profile 和 graph 共享四条规则：

1. 能从 Evidence Ledger 重建；
2. 必须保存到 source IDs 的 lineage；
3. 可以独立关闭和清空，不影响 Ledger 正确性；
4. 通过预注册的单变量消融前，不进入默认查询路径。

用途必须窄化：Event/State 只服务 temporal/update，Scene 只测试跨 session 的候选扩展，
Profile 只服务 preference/current-state，graph 只作局部关系扩展或候选信号。

## 查询期 Evidence Compiler 契约

### Evidence Need

Compiler 在候选固定后先声明作答缺什么，而不是直接按排名截断。最小字段包括：

- 关键 entity；
- 时间锚、范围或先后关系；
- multi-hop 的操作数/桥接对象；
- list 查询的预期 cardinality；
- current state、历史状态或 update 关系；
- 仍未满足的显式 gap。

这份结构必须可落 trace，以便区分“检索没找到”和“候选已有但上下文组装失败”。

### Compiler actions

V1 只允许以下可审计动作：

| Action | 语义 | 来源要求 |
|---|---|---|
| `KEEP` | 原样保留候选或 source | 绑定 source ID |
| `EXTRACT(span)` | 按字符偏移抽取原文片段 | 绑定 source ID、start、end |
| `DROP` | 从 answer bundle 丢弃候选 | 记录理由 |
| `MERGE` | 在证据装不下时合并互补内容 | 每个生成句分别引用 source IDs |
| `FETCH_SOURCE` | 沿候选 lineage 直接取回 Ledger 原文 | 只能读取已有 source ID |

`FETCH_SOURCE` 是有 ID 的 source recovery，不是第二次语义检索。V1 禁止无来源
`ADD`；任何生成内容无法逐句引用来源时，必须丢弃或退回 extractive 输出。

### 精确预算与压缩策略

Bundle 以实际 tokenizer 做 exact-token 计算，而不是按条数、字符数或估算值装填。
如果必要原文能装入冻结 cap，就保留原文；只有原文装不下时，才允许
`EXTRACT` 或有来源的 `MERGE`。预算是实验变量：每个 A/B 使用相同 cap，并报告多个
预注册预算点；不把单一 4k 或任意固定阈值写成普遍结构规律。

Grounded Trace 至少记录 Evidence Need、候选 ID、动作、字符 span、source IDs、
时间关系、冲突关系、token 取舍和未满足 gap。Answerer 只接收最终 Bundle，并保持
一次调用。

### 最多一次缺口补检

只有 Trace 明确指出缺少某个 entity、时间段或第二个操作数时，才可发起一次定向补检。
补检必须携带结构化 gap，累计候选、token 和调用次数均受同一实验预算约束。若首轮
只是置信度低、但没有可描述缺口，不得用泛化 query rewrite 扩大召回。

## 本地实现边界

可靠存储、现有混合检索、source recovery、exact-token budget、字符偏移验证、引用
校验和 deterministic fallback 由纯 Go 路径承担。

可选本地 1.5B–4B compiler sidecar 可以承担 Evidence Need、planning 和有来源的
compression；它必须可替换、显式 opt-in、默认关闭。未配置或调用失败时，系统退化到
deterministic extractive packer，而不是让整条查询失败。不得使用付费云 reranker 或
recall model 作为默认、推荐或论文涨分杠杆。

## 003 Graph 的边界

003 的 entity normalization、局部 walk 和逐信号降级保持现状。022 不迁移旧图数据，
也不把 `co`/`syn` entity edge 扩成统一 typed graph。

若固定候选 Compiler 和 Event 实验之后，剩余 multi-hop 错误确实表现为候选缺桥，
003 graph 才能作为可关闭的候选扩展对照。它仍须返回 source-linked 候选并接受同预算
消融；不能绕过引擎契约，在 benchmark adapter 私建另一套记忆算法。

## 分阶段证伪顺序

### 阶段 0：冻结尺子

先刷新 LoCoMo 和 LongMemEval-S full 500 基线，冻结数据、分母、answerer、judge、
prompt、embedding、候选规则和每个预算点。保存逐题 candidate oracle：正确证据是否已
存在于固定候选池。

### 阶段 1：表示层 bake-off

在相同候选构造和相同 token cap 下比较：

1. 当前约 900-character chunk；
2. raw-turn window；
3. semantic episode。

先做 retrieval/evidence coverage，再做同预算端到端配对实验。Semantic Episode 只有
同时提升证据覆盖或答案质量、且不造成另一 benchmark 显著回退时，才可成为默认视图。

### 阶段 2：固定候选 Evidence Compiler

冻结阶段 1 的候选 ID，不改检索、不做补检，依次比较：

1. 当前按条数装填；
2. exact-token relevance packer；
3. deterministic extractive compiler；
4. 可选本地 1.5B–4B compiler。

这一阶段必须证明“候选中已有证据被编译成更可回答的 bundle”。若 candidate oracle
显示证据根本不在候选池，不能把失败错误归因于 Compiler。

### 阶段 3：独立 Event projection

只在 temporal/update 子集分别比较：

1. 当前 Entry 时间字段；
2. 独立 event object；
3. 日期算子；
4. source-turn recovery。

四项分别开关、分别归因。Chronos 的模型依赖结果意味着 event 不能因一组 bundle
消融就直接全局默认。

### 阶段 4：一次补检与窄用途 projection

先验证结构化 gap 的一次定向补检。只有剩余错题给出明确证据时，再分别试：

- Scene：跨 session 候选扩展；
- Profile：preference/current-state；
- graph：缺少中间实体或局部关系。

每种机制都必须有自身的目标子集、独立开关、同预算对照和停止条件。

### 阶段 5：训练本地 Compiler

只有 deterministic compiler 已证明接口和错误分类有效、且 prompt-only 模型仍是主要
瓶颈时，才构建训练数据与本地模型。训练目标必须奖励 source coverage、citation
correctness、evidence density 和 answer utility，而不只拟合更短摘要。

## 硬门禁

- **默认离线**：托管模型不能成为运行必需项；本地 sidecar 缺失时必须优雅降级。
- **不可损失与可删除并存**：派生流程不能丢原文，明确 privacy purge 必须彻底生效。
- **同候选、同预算归因**：先隔离 representation 和 Compiler，再改变 retrieval。
- **双基准共同过门**：默认改动不得只提升一个 benchmark、显著回退另一个。
- **答案只调用一次**：补检和编译不能靠反复 answerer 自我修正堆分。
- **来源完整**：进入 Bundle 的事实或生成句必须 100% 绑定有效 source ID。
- **评测回归门**：触及存储、抽取或检索后，必须跑可比 LoCoMo 与 LongMemEval。
- **规模诚实**：目标仍是单用户约 100k entry，不借此承诺 ANN 或在线集群。

## 文献审计新增借鉴点（2026-07-30）

逐篇核实 eleven 篇高分 / 机制论文源文件后，下列六点尚未被前述分阶段设计充分吸收，
但都有受控实证支撑，应在阶段 0–2 落实：

1. **candidate oracle 的 coverage 分层应由 LazyMem 启发并在本地冻结**：该论文中
   recall@50 = 0.99 的 LongMemEval 设置能通过 query-time 构造超过 oracle，而
   recall@50 = 0.89 的 LoCoMo 设置仍受缺失证据限制。这是两个不同 benchmark 的经验
   观察，**不是 `recall ≥ 0.95` 的通用 Compiler 阈值**。阶段 0 必须预注册连续
   coverage strata，在每层分别报告 Compiler 效果；gold 不在候选池的题归入
   「补检 / 写入侧」队列，候选充分但 bundle 未保留 gold 的题才归入 Compiler 队列。
2. **retrieval-bottleneck 三分类应纳入阶段 0（True Memory 诊断）**：对 residual 错题
   喂 full conversation 看 recover 率，再三分成 retrieval miss（gold 不在池）/ assembly
   failure（gold 在池但未进 bundle）/ QA reasoning（gold 进了 bundle 但答错）。三类对应
   完全不同的杠杆（补检 / Compiler / answerer 契约），不先分类就上杠杆会重蹈 021 IRIS
   「没查 gold 在不在池就硬上」的覆辙。
3. **MERGE 默认关闭有了统计实证（Retain or Consolidate）**：loose 预算下 Merge
   −0.107 [−0.204, −0.013] 显著为负。Compiler 的 MERGE 动作应改为「仅当原文经精确
   tokenizer 证明装不下、且 EXTRACT 仍不够时才允许」，默认走 KEEP / FETCH_SOURCE。
4. **grounded trace 的 cited-ID 应是 fail-closed 硬门（MemChain）**：受控消融显示
   grounded trace 贡献 −13.96pp，是 post-retrieval mediation 最强结构。MemChain 的
   「每条 evidence 必须绑 candidate ID、ADD 必须是 cited candidates 的可验证 derivation」
   应从「应该」升格为门：无效 citation 或无来源 ADD → 不调 answerer、退回 extractive。
5. **D²ACCI 的 category non-regression gate 应补进评测裁决（Mi-Memory）**：engram 现用
   单次 exact McNemar，而 answer temp=1.0 使其只是单次观测。D²ACCI 的「整体涨但任一
   类别显著崩则否决」正好覆盖 013 / 014 temporal 翻车模式——整体微涨、temporal 崩。
6. **judge 噪声审计是阶段 0 必做项（LazyMem）**：LazyMem 人审全部 label 发现 LoCoMo
   judge 系统性低估（293/294 为 false negative），虽排名不变但会淹没小幅收益。engram
   用 DeepSeek-v4-flash judge，阶段 0 应抽样人审确认 FN / FP 率与方向性，否则 Compiler
   的个位数 pp 收益可能落在 judge 噪声内（017 date-scaffold +0.62pp 的教训）。

## 下一步

022 规范应只冻结 Evidence Ledger、三个表示对照、固定候选 Compiler、可审计 action、
exact-token bundle、deterministic fallback 和评测顺序。Event、Scene、Profile、graph
在规范中只能是后续独立实验，不得成为 V1 完成条件或预建层级。
