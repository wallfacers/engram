---
title: 借鉴批次：本地优先 vs SaaS
summary: 基于 2026-07-30 对 12 篇高分记忆论文的逐篇核实，把可借鉴机制切分为本地优先批（纯 Go / 预算下提质 / 不大力出奇迹）和 SaaS 批（托管+训练+大预算，放宽容许大力出奇迹），并给出连 SaaS 都不借鉴的清单。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-30
canonical_for: [lever-batch-local-vs-saas]
tags: [research, memory, locomo, longmemeval, local-first, saas, evidence-compiler]
---

# 借鉴批次：本地优先 vs SaaS

本文把 [长期记忆系统成绩与机制证据登记](high-scoring-memory-systems.md) 中经 alphaXiv
逐篇核实的 12 篇论文，按「借鉴能力」重排，并按 engram 的两条产品线切分为两批：**本地优先批**
和 **SaaS 批**。两批都受同一证据纪律约束——只借鉴有独立受控实证支持的机制，不把 bundle 高分
当成单结构卖点。完整的架构合同和 stop/go 顺序维护在
[查询期证据编译架构探索](../product/explorations/benchmark-parity-memory-architecture.md)；
当前自身分数只在 [评测结果](../evaluation/results.md) 维护。

## 借鉴能力判断标准

一条机制可借鉴，当且仅当它有**固定变量的单组件消融**支持：固定候选池、answerer、judge、
预算或单组件，其余轴不变。仅出现在某个高分完整系统里、无法拆分的结构（层级图、多 memory space、
agent loop）不算可借鉴机制，只能算「该 bundle 在其论文口径有效」。

按此标准，12 篇分三级：

| 级别 | 论文 | 含义 |
|---|---|---|
| **A · 受控机制证据强** | LazyMem、MemChain、Retain or Consolidate、EverMemOS、True Memory、Mi-Memory | 有单变量消融，机制可迁移 |
| **B · 条件性/弱支持** | Chronos、Mem0g | 仅特定子集/模型有效，或反向证伪 |
| **C · 高分但不可拆/不可迁移** | ByteRover、Mandol、Hindsight、NapMem | bundle 高分无法归因到单结构，整套重栈 |

跨论文绝对分数不可直接相减：judge 口径与分母是头号不可比源（同题不同 judge 可制造 20pp 量级
伪差距），完整审计见证据登记的「审计警示」一节。各论文 arXiv 链接亦见该文。

## 本地优先批（engram）

### 约束

- 纯 Go deterministic 先行；离线 sidecar（Ollama / fastembed）显式 opt-in、默认关闭。
- **预算下提质**：不加 top-k、不加上下文预算、不用付费云 rerank/recall 涨分（Constitution
  I/V 与 DEATH RULE）。被认可的赢是 bge-large 式「同预算下提质」，不是「加预算 / 加召回广度」
  的赢；一个只有靠付费云 reranker 或更大预算才过门的杠杆不算有效赢。
- 不预建完整 event/scene/profile/graph 层级；每种 projection 在独立消融证明净收益前不进默认路径。
- 几乎全部杠杆落在「检索之后、作答之前」或「评测纪律」，补检索侧杠杆线探尽后的空白（当前
  参考点 LoCoMo 85.71%、LongMemEval-S 80.80%）。

### 本地优先借鉴清单

| 序 | 借鉴机制 | 实证来源与强度 | 本地实现 | 预期方向 | 踩坑 |
|---|---|---|---|---|---|
| **L0-1** | retrieval-bottleneck 三分类诊断（retrieval miss / assembly failure / QA reasoning） | [True Memory](https://www.alphaxiv.org/abs/2605.04897)：357 错题喂 full conversation，92% 可恢复，瓶颈在检索非存储。**极强** | 纯 Go，零模型 | 先分类再上杠杆，根除 021 IRIS「没查 gold 在不在池就硬上」的教训 | full-context 诊断仅在 LoCoMo（装得下窗口）可行；LME 需用 candidate oracle 替代 |
| **L0-2** | candidate oracle coverage 分层（连续 strata，gold 在不在池） | [LazyMem](https://www.alphaxiv.org/abs/2607.22690)：recall@50=0.99（LME）构造能救 / 0.89（LoCoMo）救不了。**强** | 纯 Go | 候选充分但 bundle 未保留 gold → Compiler 队列；gold 不在池 → 补检/写入侧 | 不是 `recall≥0.95` 通用阈值，两 benchmark 分别冻结连续 strata |
| **L0-3** | category non-regression gate（整体涨但任一类别崩则否决） | [Mi-Memory](https://www.alphaxiv.org/abs/2607.18975) D²ACCI：∀c Δc≥−εc。**方法级** | 纯 Go 统计 | 覆盖 013/014 temporal 翻车模式（整体微涨、temporal 崩） | answer temp=1.0，需配多次重复 / bootstrap CI |
| **L0-4** | judge 噪声抽样审计（FN/FP 方向） | LazyMem：LoCoMo 293/294 为 false negative，系统性低估。**强** | 评测基建 | 防个位数 pp 收益落在 judge 噪声内（017 date-scaffold +0.62pp 教训） | 需人审抽样；engram 用 DeepSeek judge，阶段 0 必做 |
| **L1-1** | exact-token packer + MERGE 默认关闭 | [Retain or Consolidate](https://www.alphaxiv.org/abs/2607.17545)：原文装得下优先保留；宽松预算 Merge −0.107 [−0.204, −0.013] 显著为负。**极强** | 纯 Go exact-token 计算 | 预算下提质；MERGE 仅在精确 tokenizer 证明装不下、且 EXTRACT 仍不够时才允许 | 必须用实际 tokenizer，不能按字符/条数估算 |
| **L1-2** | grounded trace cited-ID fail-closed 硬门 | [MemChain](https://www.alphaxiv.org/abs/2607.24097)：去 trace −13.96pp，post-retrieval 最强单结构（> plan −6.21）。**强** | 纯 Go 校验门（生成需 sidecar） | 无效 citation / 无来源 ADD → 不调 answerer、退回 extractive | trace 生成需模型；但「校验 + 丢弃」逻辑确定性，纯 Go 可守门 |
| **L1-3** | deterministic extractive compiler（query-conditioned KEEP/DROP，无模型 fallback） | LazyMem query-conditioned 构造的无模型退化。**中**（推论） | 纯 Go | 无 sidecar 时退化到此，不让整条查询失败 | 绝对增益未单独消融，需 L0-1 量化「候选内已有证据被编译后更可回答」 |
| **L2-1** | semantic episode vs 固定分段 bake-off | [EverMemOS](https://www.alphaxiv.org/abs/2601.02163)：GPT-4.1-mini 语义分段 89.16 vs 512-token 84.52，同候选同预算。**强** | 语义分段需 Ollama sidecar opt-in，默认 900-char chunk | 仅同预算下双 benchmark 净赢才升级默认 | 多轮检索；EverMemOS 主路径不回 raw turn，需评估是否合 engram |

### 执行顺序

`L0-1 → L0-2 → L0-3`（零成本诊断 / 门禁，先拿尺子）→ `L1-1 → L1-2`（纯 Go Compiler 骨架，
预算下提质）→ `L1-3`（deterministic fallback）→ `L2-1`（sidecar opt-in 表示层）。前 7 项与
[架构探索](../product/explorations/benchmark-parity-memory-architecture.md) 的六借鉴点 + deterministic
packer 一致；本文的增量是把每项的实证置信度逐篇坐实（见文末「核实增量」）。

## SaaS 批（远期）

### 约束放宽

托管模型 / 训练 / 大预算都可接受；**放宽容许「大力出奇迹」**，只要能涨点、速度快。但仍守诚实
归因——不可拆 bundle 高分不能当单机制卖点。对应远期 SaaS 锚：垂直设备 / 应用用户习惯记忆
（车机 / 手机），local-first 隐私为卖点；SaaS 线不受 local-first DEATH RULE 约束。

### SaaS 借鉴清单

| 序 | 方向 | 来源 | SaaS 可行性 | 涨点证据 | 速度 |
|---|---|---|---|---|---|
| **S-1** | 训练 1.5B–4B constructor/compiler + RL | LazyMem（4B：prompt-only 0.41 → SFT 0.72 → RL 0.85）、[NapMem](https://www.alphaxiv.org/abs/2607.05794)（GRPO） | SaaS 投训练算力 | LazyMem 4B 用 213 token 达 0.85，68.7× 压缩 | 中（需训练） |
| **S-2** | 托管 justifier + agent loop post-retrieval mediation | [ByteRover](https://www.alphaxiv.org/abs/2604.01599)（Gemini 3.1 Pro 32k justifier） | SaaS 用大模型做证据调解 | ByteRover LME 92.8%；但 −29.4pp 消融同时改了 agent loop + 绕过 justifier，不能归因到层级检索 | 快（调 API） |
| **S-3** | 强 answerer + event calendar 堆叠 | [Chronos](https://www.alphaxiv.org/abs/2603.16862) High（Opus 4.6 → 95.6% vs Low/GPT-4o 92.6%） | SaaS 堆前沿模型 | High 比 Low +3pp；但 event 消融强模型依赖（Low −34.5pp / High −2.6pp） | 快（换模型） |
| **S-4** | 多 memory space + 重检索栈 | [Mandol](https://www.alphaxiv.org/abs/2606.29778)（H800，多 space + 图 + MMR） | SaaS 上 GPU 重栈 | Mandol LME 88.40，但**无逐组件端到端 QA 消融** | 慢（工程重） |
| **S-5** | 极宽预算 full-context 重评估 | Retain or Consolidate（宽松预算下 Merge 显著有害，但 SaaS 预算可极宽） | SaaS 不受 token 成本约束 | 宽松预算下原文优先；可能重定义最优操作选择 | 快 |

## 不借鉴清单（连 SaaS 都避坑）

| 不借鉴 | 证据 | 理由 |
|---|---|---|
| **Mem0g graph for multi-hop** | [Mem0](https://www.alphaxiv.org/abs/2504.19413)g multi-hop 47.19 < base Mem0 51.15 | 实证**有害**；graph 仅在 temporal/open-domain 小幅改善，不可作为 multi-hop 杠杆 |
| **Hindsight `<add>` token budget** | [Hindsight](https://www.alphaxiv.org/abs/2512.12818) 检索预算在正文为未填占位符 | 不可复现；judge（GPT-OSS-120B）自承「not directly comparable to official benchmark judge」 |
| **bundle 高分 → 单结构归因** | Chronos / Mandol / ByteRover 整套栈 | 无法拆分，不能称「层级图 / 多 space / agent loop 带来 X pp」 |
| **本地批任何「加 top-k / 加预算 / 付费云 rerank」变体** | engram [上下文预算剥离](../evaluation/reports/budget-ablation.md) + DEATH RULE | 本地铁律：不大力出奇迹、不付费云涨点；+3.20pp 领先已被证明完全由预算驱动 |

## 核实增量（相对现有架构探索文档）

2026-07-30 逐篇核实源文件后，相对
[架构探索](../product/explorations/benchmark-parity-memory-architecture.md) 已落的六借鉴点，
下列三点新坐实或升格：

1. **LazyMem judge 噪声方向已坐实**：LoCoMo 293/294 flipped label 为 false negative，系统性低估
   （虽排名不变）。L0-4 不是「可能有问题」，而是「确定方向性偏低」；engram 用 DeepSeek-v4-flash
   judge，阶段 0 必须抽样人审确认 FN/FP 率与方向性。
2. **Retain or Consolidate 的 MERGE 负面有了精确 CI**：宽松预算下 Merge −0.107 [−0.204, −0.013]
   显著为负。L1-1 的 MERGE 默认关闭从「应该」升格为「有统计实证」。
3. **MemChain cited-ID 门是 post-retrieval 最强单结构**：去 trace −13.96pp > 去 plan −6.21pp。
   L1-2（grounded trace cited-ID fail-closed）的优先级应高于 evidence plan。

## 分母与可比性边界（核实结论）

逐篇核实证实「没有任何一篇同时满足标准 LoCoMo 1540 + 标准 LongMemEval 500 + 干净逐组件消融 +
可比 judge」。与借鉴判断相关的关键不可比点：

- **LoCoMo 分母口径差异**（全量 vs answerable 子集，非注水）：engram 与 MemOS 用 category 1–4 的 1,540；ByteRover 1982、Mandol 1986 用全量口径（含 category 5 adversarial，完整 1,986）。差异在于 adversarial 是否计入、是否按拒答处理。
- **分母缩水**（<标准）：NapMem 1315（说话人切分）、LazyMem 314（末两段对话）；LongMemEval 缩水：
  LazyMem 100（test split）、NapMem 100（OOD）、Retain or Consolidate 75（test split）。
- **单篇内 judge 口径不一致**：True Memory 的 LoCoMo 用 semantic-match（93.0 = oracle ceiling
  92.99），LongMemEval 用 strict。
- **不可比 judge**：Hindsight（GPT-OSS-120B，自承不可比）、ByteRover（托管 Gemini judge + 独立
  justifier）、Mem0（自定义 LLM-as-Judge）。

因此本批借鉴只取**机制级**证据（固定变量的单组件消融），不取**系统级**绝对分数做横比。

## 实测增量：028 写侧训练抽取（2026-08-06）

S-1 的「训练小抽取器」方向在 [spec 028](../../specs/028-write-side-event-training/spec.md) 做过一次端到端实测，结论对 S-1 的涨点主张有直接约束：

- **训练确实解决中间指标**：Qwen2.5-3B-LoRA（5313 条教师数据，3 epochs）时间锚定 5%(7B) → **96.9%**，schema 合法 100%；教师（DeepSeek-flash）86.4%。抽取能力瓶颈被训练级能力彻底移除。
- **但端到端配对仍不转化**：chunk 50.0% vs event 48.8%（**−1.2pp，McNemar p=1.00**，008 铁律未达成）。三臂差距收窄链 −26.2(7B) → −6.0(teacher) → −1.2(trained) pp 持续收窄但始终未转正。写侧 event 表示**第三次端到端不转化**（027 / 028-US1 / 028-US2）。
- **蒸馏上限 = 教师**：训练数据全部来自教师，模型不可能超过教师的事件语义质量（教师自身 −6.0pp 未转化）。若想突破，需要非教师来源的监督（人工精修超大量 / 奖励信号）。
- **对 S-1 的约束**：LazyMem 的涨点证据（SFT/RL 提高 token 压缩率）是**中间指标**；028 证明"训练抽取能力提升"≠"端到端 QA 转化"。S-1 若推进，涨点主张**必须落端到端配对**（008 纪律），不能以抽取质量/压缩率作出货依据。写侧 event 结构本身的价值已被 027/028 三次证伪，S-1 若做应优先 agentic 检索 / 答题侧，而非写侧 event 表示。

## 实测增量：029 Agentic 多步导航（2026-08-06）

S-2 的「agent loop post-retrieval mediation / agentic 检索」方向（MemCog / NapMem / ByteRover 消融所指）在 [spec 029](../../specs/029-agentic-memory-navigation/spec.md) 做过一次端到端实测，结论对 S-2 / A 类「推理介入检索」主张有直接约束：

- **US1 零成本诊断 GO 但 US2 配对 NO-GO**：确定性模拟显示 `rescueable_share=0.655`（换查询 76% / 换粒度 22% / 跟线索 2%），但真实 35B 导航 agent 配对 **29.8% vs 单次基线 47.6%（−17.9pp，p=0.0059 显著负）**。
- **根因 A（结构性）**：该 store 的裸混合检索以短 fact 为主，chunk 需 `chunk-quota` 机制强制保底——导航组装无 quota，answerer 上下文永远劣化（~500 vs 基线 3654 tokens）。
- **根因 B（机制性）**：模型自主导航不转化 US1 理论空间（73% 不 stop、改写查询命中 gold 差于确定性模拟）。**「换查询可救」需要 oracle 式改写，本地 35B 自主达不到**；US1 模拟高估真实导航救回能力。
- **对 S-2 的约束**：MemCog/NapMem 的「推理介入检索」涨点主张**不可移植到本地弱模型（35B 自主导航）+ 无 quota 的检索路径**。S-2 若推进（托管大模型 justifier / agent loop），必须：(1) 与基线同 answer-context 预算（chunk-quota 对齐，避免 fact 主导劣化）；(2) 端到端配对为唯一 GO 门（008），零成本诊断 GO 不能作依据。
- **附带工程发现**：Qwen3.6 推理模型默认输出思考+JSON 混合（无 `reasoning_content` 分离），agent 每步工具调用需 `enable_thinking=false`（0.6s vs 13s/步）；导航 decide 需独立 vLLM HTTP caller（harness 内，引擎零改动）。

## 实测增量：030 读侧证据装配（2026-08-06）

L1-2（MemChain 引用链）与 L1-1（Retain or Consolidate 精确装填）在 [spec 030](../../specs/030-evidence-mediation/spec.md) 落地为读侧装配结构，US1 机制已实测，US2/US3 配对待评测环境：

- **US1 装配器机制 GO（单测全绿 + 真实 store 诊断）**：chunk-first 排序（chunk_fraction 单测 ≥0.5，029 是 ~0.01）、类别条件结构（temporal→时间序 / multi-hop→实体分组 / generic→分数序）、超 cap 精确截断（复用既有 `vllmTokenCounter` 的 `/tokenize`，TotalTokens 精确）、estimateTokens 显式降级（tokens_estimated=true，宪法 V）。
- **真实发现（029 根因 A 的独立确认）**：keyword-only 检索下 chunks 不进 top 候选（BM25 对长 chunk 分数低，`applyChunkQuota` 无 chunk 可保，chunk_fraction=0）——**装配器无法补偿检索侧 chunk 缺失**；基线的 3654 tokens 来自 hybrid（semantic 召回 chunks）+ `--chunk-quota 12`，不是 keyword。⇒ 读侧装配必须配检索侧 chunk 保底，US2 配对必须用基线同款检索口径（hybrid + chunk-quota）。
- **对 L1-1/L1-2 的约束**：精确装填（L1-1）的核心是"整体精确 count + 逐条估算排序"（evidencecompiler 纪律：整体≠逐条之和）；引用链（L1-2）的 fail-closed 门（闭包校验/回溯校验/解析失败重试→回退）是纯 Go 确定性，trace 生成需 sidecar opt-in。**US2/US3 的涨点主张必须落 008 端到端配对**，机制/单测 GO 不作依据（延续 028 的蒸馏教训）。

## 与现有文档的关系

- 证据来源：[长期记忆系统成绩与机制证据登记](high-scoring-memory-systems.md)（11 篇 + Mem0 的成绩/分母/judge 全表）。
- 架构合同与分阶段证伪顺序：[查询期证据编译架构探索](../product/explorations/benchmark-parity-memory-architecture.md)。
- 研究总纲：[论文方向](paper-direction.md)。
- 当前分数正本：[评测结果](../evaluation/results.md)。
