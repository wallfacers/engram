---
title: 长期记忆系统成绩与机制证据登记
summary: 本文把 LoCoMo 与 LongMemEval 的完整系统成绩、受控机制证据、工程依赖和 engram 待证伪假设分开记录，避免从跨栈高分反推未经消融的架构结论。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [high-scoring-memory-systems]
tags: [research, memory, locomo, longmemeval]
---

# 长期记忆系统成绩与机制证据登记

本文记录截至 2026-07-29 经 alphaXiv 核验的 LoCoMo 与 LongMemEval 论文证据。
它不再把“高分完整系统”和“有因果支持的机制”混成一个主参考集。engram 的候选
路线见[双基准架构探索](../product/explorations/benchmark-parity-memory-architecture.md)，
当前自身分数只在[评测结果正本](../evaluation/results.md)维护。

## 证据分层与使用规则

论文证据分为四层，结论强度不能跨层提升：

1. **成绩与复现登记**：记录完整系统分数、分母、模型栈和公开产物。高分只能证明
   该 bundle 在其论文口径有效，不能证明 bundle 中每个结构都有效。
2. **受控机制证据**：优先采用固定候选池、answerer、judge、预算或单组件的实验。
   未达到 90% 的系统也可提供高价值机制证据。
3. **工程依赖与成本**：单独登记托管模型、reranker、超大上下文、训练产物和硬件，
   判断其能否迁移到 local-first 默认路径。
4. **engram 待证伪假设**：把外部证据改写为同候选、同预算、可停止的本地实验，
   通过前不得升格为默认架构。

跨论文绝对分数不能直接相减。数据版本、分母、answerer、judge、答案提示、候选数、
上下文预算或聚合规则只要有一项不同，就只能作数值参照。

### 审计警示：judge 口径与分母是头号不可比源（2026-07-30 核实）

逐篇核对源文件后，同题不同 judge 能制造 20pp 量级的伪差距，远大于任何架构差异。

**judge 口径**：ByteRover / 第一层多数用宽松 semantic-match 或托管 Gemini justifier
（90+ 分）；MemChain 用开源 LoCoMo-Refined judge（同批题仅 69.80）；Hindsight 用
GPT-OSS-120B judge 且自承 "not directly comparable to official benchmark judge"；
True Memory 的 LoCoMo 用宽松 semantic-match（93.0 = oracle ceiling 92.99）、LongMemEval
却用 strict（87.8），**单篇内部口径都不一致**。

**分母 / 子集**（LoCoMo 全量 1,986 = category 1–4 共 1,540 + category 5 adversarial 446；LongMemEval 全量 500）：

| 论文 | LoCoMo | LongMemEval |
|---|---|---|
| ByteRover | 1,982（全量口径，含 adversarial） | 500 |
| Mandol | 1,986（全量口径，含 adversarial） | 500 |
| LazyMem | 314（末两段对话） | 100（test split） |
| Retain or Consolidate | 分层采样 | 75（test split） |
| NapMem | 1,315（说话人切分） | 100（OOD held-out） |
| Chronos 消融 | — | 116（ablation subset） |

分母差异要分清"全量 vs 子集"与"注水"是两回事：engram 与 MemOS 使用 category 1–4
answerable 子集 1,540（不含 category 5 adversarial），ByteRover/Mandol 使用全量口径
1,982/1,986（含 category 5 adversarial）。adversarial 是否计入、是否按拒答处理，
决定两口径能否直接可比；这不是分母注水。

没有任何一篇同时用「category 1–4 的 1,540 + 全量 500 + 干净逐组件消融」；LongMemEval 全量 500
配干净消融是当前空白。不可迁移的托管依赖另见第三层：Chronos 依赖付费 Cohere Rerank v3，
ByteRover 依赖不可消融的 Gemini 3.1 Pro justifier，EverMemOS 的 4B GPU embedder + Neo4j
检索栈不开源且 True Memory 明确「cannot be run on local hardware」，Hindsight 的检索
token budget 在论文中是未填的 `<add>` 占位符。

## 第一层：成绩与复现登记

| 论文 | 论文主结果 | 完整系统结构 | 复现与解释边界 |
|---|---|---|---|
| [Chronos](https://www.alphaxiv.org/abs/2603.16862) | LongMemEval-S full 500：Low 92.60%，High 95.60% | raw turn calendar、结构化 event calendar、日期/grep/vector 工具和 ReAct 查询 | 使用 `text-embedding-3-large`、Cohere Rerank v3、Gemini 动态提示和 Claude answerer；事件消融只覆盖 116 题 |
| [ByteRover](https://www.alphaxiv.org/abs/2604.01599) | LoCoMo 96.1%；LongMemEval-S 92.8% | Context Tree、五级渐进检索、relation、justifier 和 agent loop | LoCoMo 为全量口径 1,982 题（含 category 5 adversarial，接近完整 1,986）；使用 Gemini 3 Flash judge 与 Gemini 3.1 Pro justifier；公开分数是整套答案管线结果 |
| [Mandol](https://www.alphaxiv.org/abs/2606.29778) | GPT-4.1-mini：LoCoMo 92.21%，LongMemEval 88.40% | basic graph、episodic/semantic/emotional memory spaces、路由、RRF、图扩展、冲突处理和 MMR | 没有逐组件端到端 QA 消融；主实验使用 H800，只能作为结构观察 |
| [EverMemOS](https://www.alphaxiv.org/abs/2601.02163) | GPT-4.1-mini：LoCoMo 93.05%，LongMemEval 83.00% | Atomic Facts 导航到 MemScene，再扩展 synthesized Episodes；另有 profile | 主查询路径不向 answerer 回收 raw turn；可复用的强证据主要来自语义分段对照 |
| [True Memory](https://www.alphaxiv.org/abs/2605.04897) | LoCoMo Pro 93.0%；LongMemEval strict 87.8% | 原始消息/event、lexical+dense、salience、consolidation、query expansion 和本地 rerank | Pro 预排序取 top-100，论文未披露最终答案上下文的硬 token cap；LoCoMo semantic-match judge 与 strict 口径不可混比 |
| [Mi-Memory](https://www.alphaxiv.org/abs/2607.18975) | MemStack：LoCoMo 93.59%，LongMemEval 87.47% | atomic、session/topic、profile/correction、current state 多空间和 RRF | 论文明确说兼容性观察不能替代联合消融，最终边际贡献仍待验证 |
| [Hindsight](https://www.alphaxiv.org/abs/2512.12818) | LoCoMo 89.61%；LongMemEval 91.4% | facts、experiences、observations、opinions 和 temporal/entity/causal graph | 使用 GPT-OSS actor、强 judge、多信号检索和 cross-encoder；未找到干净的单结构消融 |

这张表不支持“高分系统都证明了层级图有效”。它只说明多个高分 bundle 使用了不同
层级、图、事件或查询组件；真正可迁移的机制必须看下一层。

## 第二层：受控机制证据

### 语义 episode 是值得先验证的表示

[EverMemOS](https://www.alphaxiv.org/abs/2601.02163)在移除 MemScene 的同一设置中
比较分段方式：GPT-4.1-mini 下，固定 10-message、512-token、1024-token 和 session
分段分别为 88.05%、87.55%、84.52% 和 87.66%，语义分段为 89.16%；Qwen 语义分段
为 89.78%。这是当前比“完整层级带来高分”更干净的表示证据。

完整系统 93.05%、移除 MemScene 89.16%、再移除 MemCell/细节 81.82% 的消融会同时
改变表示和查询路径，因此只作为次级支持，不能把全部差值归给 scene。

### 主要智能可以发生在检索之后、作答之前

[LazyMem](https://www.alphaxiv.org/abs/2607.22690)保存 raw messages，先做 top-50
宽召回，再为每个命中扩展前后两条消息，把重叠窗口合并后并行执行 KEEP/DROP 与
query-conditioned compression。LongMemEval 100 题测试集上，32B constructor 为
0.93、平均 1,041 memory tokens；训练后的 4B 为 0.85、213 tokens；普通 top-50
RAG 为 0.75、14,628 tokens。这不是 full 500 结果，却直接支持”检索后构造能提高
证据密度”。其 32B 变体甚至超过 Oracle Turn（0.82）和 Oracle Session（0.84），因为
query-conditioned 过滤噪声的同时保留跨 session 证据，而单一 oracle turn/session 装不下；
人审 judge 标签的噪声审计显示该两变体为零 judge 噪声。

关键边界由 recall@50 决定：LongMemEval recall@50 = 0.99 时，过滤噪声即足以超过
oracle；LoCoMo recall@50 = 0.89 时，构造救不了缺失证据，增益退化为持平 oracle。
这给出 query-time compiler 的经验分界——**recall 高的题它能救，recall 低的题（深层
multi-hop 缺失）它救不了**，与 engram 的 gold-rank 诊断一致。

[MemChain](https://www.alphaxiv.org/abs/2607.24097)固定候选池、answerer、judge、
预算与解码，先生成 evidence plan，再生成带候选 ID 的 grounded trace，最后执行
KEEP/DROP/MERGE/REFINE/ADD。完整系统为 69.80，移除 plan 为 63.59（−6.21pp），
移除 trace 为 55.84（−13.96pp），prompt-only 为 49.35（−20.45pp）。绝对分数不高，
但这是 answer-facing evidence contract 的直接机制证据。其公开仓库包含核心代码、
schema 和 smoke example，不包含私有数据路径、checkpoint、正式评测输出和启动脚本。

### 压缩是否有益取决于原始证据能否装入预算

[Retain or Consolidate](https://www.alphaxiv.org/abs/2607.17545)固定 gold evidence、
answerer、judge 和预算。在 32-token 条件下，Abstract 比 raw retention 高 48pp；
在 256-token 条件下，Abstract 的点估计低 8pp，置信区间为 −16.9 至 0.5，跨过零；
**同一 loose 预算下 Merge 为 −0.107，95% CI [−0.204, −0.013]，显著为负**——即宽松
预算下合并互补内容不仅无益、反而稳定有害。可用结论不是”总要摘要”，而是：

- 原始证据装得下时优先保留原文；
- 装不下时才测试有来源的压缩；
- write-time consolidation 不能作为不随预算变化的默认规则。

### Event 和 graph 目前只得到条件性或弱支持

[Chronos](https://www.alphaxiv.org/abs/2603.16862)的 116 题消融显示明显模型依赖：
Low 配置从 93.1% 降到 turn-only 58.6%（−34.5pp）；High 配置从 94.8% 降到
92.2%（−2.6pp），High temporal 甚至从 93.5% 升至 96.8%。它支持“event 值得在
特定模型和 temporal 子集独立实验”，不支持“event calendar 普遍带来巨大收益”。

[ByteRover](https://www.alphaxiv.org/abs/2604.01599)移除 relation graph 仅从 92.8%
降到 92.4%（−0.4pp）。所谓移除 tiered retrieval 的 63.4% 同时把所有查询送入完整
agent loop、绕过 Tier 0–3 和独立 justifier，改变了答案管线和预算，不能把 −29.4pp
归因于层级检索。

### 训练信号支持长期方向，但不属于 V1 硬依赖

LazyMem 的 4B constructor 从 prompt-only 0.41，经 SFT 到 0.72、RL 到 0.85。
[NapMem](https://www.alphaxiv.org/abs/2607.05794)完整策略相对无 RL 的平均分为
62.74 对 48.39，LongMemEval 为 80.33 对 72.33。两者说明专用训练可能显著优于
prompt-only 策略，但不能证明达到 90%+ 必须训练，也不能让模型成为默认在线依赖。

## 第三层：工程依赖与成本

| 系统/机制 | 主要依赖 | 对 engram 的迁移结论 |
|---|---|---|
| Chronos High | 商业 embedding、Cohere rerank、Gemini 动态提示、Claude answerer | 只迁移可独立消融的 event/date/source 语义，不迁移托管栈 |
| ByteRover | Gemini judge/justifier、大 justifier 预算、agent loop | 不能把整套 bundle 或 −29.4pp 当作本地 tiered retrieval 依据 |
| Mandol | 多 memory space、SPLADE/dense、cross-encoder、H800 | 用作假设目录，不预建完整图或多空间体系 |
| EverMemOS | Qwen embedding/reranker 与 synthesized Episodes | 优先复现实验上较干净的语义分段，不把 raw-turn 回收写成论文事实 |
| True Memory Pro | top-100、query expansion、本地 reranker、未披露的最终 token cap | 只作单机多阶段路径参照，不能声称是“纯本地预算裁剪”的 93.0 |
| LazyMem / MemChain | 训练后的本地 constructor/compiler；公开产物不完整 | 先做纯 Go extractive fallback，再把可替换本地模型作为 opt-in 实验 |

engram 的默认或推荐栈不得用付费云 reranker/recall model 涨分。模型增强必须可本地
部署、可替换、默认关闭；无模型时仍应退化到可审计的 extractive packer。

## 第四层：engram 待证伪假设

外部证据目前只足以支持以下实验顺序：

1. **H1—表示层**：在相同候选、相同 answer-context token cap 下，比较当前
   900-character chunk、raw-turn window 和 semantic episode。若语义 episode 在
   LoCoMo 与 LongMemEval 的配对结果中没有稳定收益，则不把它设为默认视图。
2. **H2—固定候选 Evidence Compiler**：不改检索、不发第二次查询，比较当前按条数
   装填、exact-token relevance packer、extractive query-time compiler，以及可选的
   本地 1.5B–4B compiler。必须先证明候选内已有证据被编译后更可回答。
3. **H3—独立 Event projection**：只在 temporal/update 子集分别消融 event object、
   日期算子和 source-turn 回收。三项不得一起上线后归因。
4. **H4—有界补检与窄用途 projection**：只有 grounded trace 明确缺少实体、时间段
   或第二个操作数时才补检一次。Scene 只测试跨 session 候选扩展，Profile 只测试
   preference/current-state；graph 也只是可关闭的 projection 或候选信号。

每项实验都必须冻结数据、候选池或候选预算、answerer、judge、prompt 与上下文 token，
报告逐题结果、分类别回退、证据覆盖率和成本。未通过独立消融的 Event、Scene、Profile、
graph 均不得成为必须经过的架构层。

## 022 本地机制证据更新（2026-07-31）

022 当前只得到可用于缩小假设空间的诊断证据，尚未得到正式双基准 promotion 证据：

- LoCoMo lossless B0 的三次多数为 1,314/1,540（85.32%），但 B0 按合同只作 continuity。
- 请求 extractive compiler 的单次命令为 1,291/1,540（83.83%），平均 answer context
  3,605 token；但它未带 `--eval-protocol`，compiler flag 被普通 runner 静默忽略，并
  发生 1,546 次 answer 与 6 次 rewrite。因此它不是 Compiler 机制证据；CLI 已改为对
  formal context 外的 mechanism flags fail closed。
- coverage-only 扫描在 top-k 30/60 的 turn recall 都为 0.808；从约 3,600 token 降到
  约 1,200 token 时 turn recall 从 0.808 降到 0.641。该结果继续支持连续 coverage
  分层，不支持把 `recall >= 0.95` 固化为通用阈值。
- pure-fact 把平均 context 从 3,605 降到 1,529 token，却把总体从 83.83% 降到
  73.70%，其中 single-hop −16.0pp。故“chunk 直接替换为抽象 fact”是 NO-GO；若继续
  研究紧凑表示，必须保留可复原 verbatim span，而不是把 write-time 摘要当作真相。

这些内部结果没有改变上述论文事实：Merge 的有效已登记区间仍是
`−0.107 [−0.204, −0.013]`，coverage 仍按连续量解释，judge/分母仍是第一优先级有效性
门。修复后的正式 LoCoMo/LongMemEval B1、双 reviewer judge audit 和逐题配对统计未完成，
所以 022 当前 verdict 只能是 `HOLD`，不能把任一实验臂写成默认能力。

## Mem0 数值目标的边界

[Mem0 2025 论文](https://www.alphaxiv.org/abs/2504.19413)只评测 LoCoMo：Mem0
为 66.88%，Mem0g 为 68.44%，没有报告 LongMemEval；其中 graph 对 multi-hop 低于
基础 Mem0，只在 temporal/open-domain 有小幅改善。

engram 当前数值北极星指向其后续托管平台自报的 LoCoMo 92.5% / LongMemEval 94.4%，
而不是上述论文结果。该托管结果包含开源 SDK 不具备的优化和较大检索预算，无法同栈
复现。因此，engram 可以把它作为严格数值门，但只有冻结数据、answerer、judge、
prompt、候选与上下文预算后的配对实验，才能支撑“受控优于 Mem0”的声明。

## 026 查询期 verbatim 编译证据更新（2026-08-06）

- [Fidelity-Before-Structure](https://www.alphaxiv.org/abs/2601.00821)固定 pipeline 内只
  换存储表示：verbatim chunks 比 LLM-extracted artifacts 高 **15.9pp（LoCoMo）/
  22.0pp（LongMemEval-S）**，机制是 lossy distillation（write-time 丢弃的信息
  retrieval 救不回），69% 的失败来自 extractor 从未写下的 write-time loss。方向性支持
  “query-time 选原文 > write-time 提取”，但不保证在固定候选池内必涨。
- Penfield Labs audit（[dial481/locomo-audit](https://github.com/dial481/locomo-audit)，
  被 [CogniFold](https://www.alphaxiv.org/abs/2605.13438) 引用）：LoCoMo-10 有
  **6.4% 答案键错误**（99/1,540 改分错误，multi-hop 9.9%），完美系统理论上限
  ≈93.6%。engram 配对必须记录答案键噪声，小 delta 不单独作 promotion 依据。
- **026 实测（2026-08-02，LoCoMo B1-high 配对）**：query-time verbatim 编译在固定
  候选池内是**负结果**——compiler 相对 control 82.6% 为 extractive 78.1%（−4.5pp）/
  exact-token 79.0%（−3.6pp），multi-hop 目标类别也无提升。机制：need 剪枝把 answer
  input token 压掉 33%/24%，simple 类目更依赖精确逐证据命中。这**不证伪**
  Fidelity-Before-Structure（它比较的是表示层 verbatim vs artifact，而非 query-time 在
  固定池内剪枝）；但它说明 “query-time 剪枝式编译”不是当前固定候选口径下的有效杠杆，
  `--compiler-arm` 保持默认关。
