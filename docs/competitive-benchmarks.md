# 竞品对标基准(涨点目标表)

> 🧭 **状态**: 活跃 · **目标**: 为 engram 涨点锚定外部竞品目标。MemOS 88.83 的机制
> 拆解**正本**见 [`memory-strategy.md`](./memory-strategy.md) 附;本文只记竞品分与差距,不重复拆解。
>
> 目的:为 engram 的"涨点"工作锚定外部目标。记录竞品公开分数 + 与 engram 现状的差距。
> **口径已核实(§4/§6)**:读源码后确认 —— 分母/类别(cat-5 排除)/拒答/聚合四轴与 engram **相同**,可比;唯 **judge 宽松度不同**(engram 更严,§6)+ 检索预算/答题模型不同。竞品分仍来自各家自报模型栈,leaderboard 绝对值不可与 engram 直接混用,但**口径结构已对齐**。
>
> 🚨 **2026-07-26 重大更新 —— 对 MemOS 的差距已实测,方向是反的。** MemOS 自家代码跑在
> engram 同款答题模型 + embedder + judge 上 = **82.40%**,**低于** engram 同口径 **85.71%**。
> 本文旧口径的"落后 ~23pp、要追赶 MemOS"**已作废**(§5②/§5④ 已改写,残留 caveat 见那里)。
> 全文其余按 88.83 写的推论,凡未标注更新的,一律按"leaderboard 分含 6.43pp regime 红利"折算再读。

采集日期:2026-07-21。

---

## 1. Mem0 — 新记忆算法(2026-04)

📄 *Benchmarking Mem0's token-efficient memory algorithm*

| Benchmark | Old | **New** | Tokens | Latency p50 |
|---|---|---|---|---|
| LoCoMo | 71.4 | **92.5** | 7.0K | 0.88s |
| LongMemEval | 67.8 | **94.4** | 6.8K | 1.09s |
| BEAM (1M) | — | 64.1 | 6.7K | 1.00s |
| BEAM (10M) | — | 48.6 | 6.9K | 1.05s |

- 口径:同一"production-representative"模型栈;**单发检索(一次调用,无 agentic loop),top_200 检索预算**。
- 分数反映 Mem0 **托管平台**(含开源 SDK 不带的私有优化);开源用户"方向性相似,数字不同"。

**What changed(他们自报的赢法):**
1. **单发 ADD-only 抽取** —— 一次 LLM 调用,无 UPDATE/DELETE;记忆累积不覆写。
2. **Agent 生成的事实一等公民** —— agent 确认的动作以同等权重存储。
3. **实体链接** —— 实体被抽取、嵌入、跨记忆链接,用于检索增强。
4. **多信号检索** —— semantic + BM25 keyword + entity 并行打分融合。
5. **时间推理** —— time-aware 检索,对"当前状态/过去事件/未来计划"排出正确的带日期实例。

评测框架开源、可复现。

---

## 2. MemOS — OmniMemEval 领先(2026-07-02)

🏆 *MemOS Advances Agent and User Memory Benchmarks*(OpenClaw 五项 agent 任务平均完成率 36.63% → 50.87%)

| Benchmark | Score |
|---|---|
| **LoCoMo** | **88.83** |
| **LongMemEval** | **89.20** |
| PersonaMem v2 | 40.58 |
| HaluMem | 80.91 |
| BEAM-10M | 56.75 |
| GDPVal | 62.07 |
| LiveCodeBench | 64.96 |
| OmniMath | 61.00 |
| SWE-Bench | 38.46 |
| BrowseComp-Plus | 23.85 |

- 经 OmniMemEval 评测(14 个商业记忆产品 × 10 数据集的统一评测)。
- **本地插件线(与 engram 定位正面撞车)**:memos-local-plugin 2.0 / Hermes Agent / OpenClaw 本地插件——**100% 本地、零云依赖、混合检索 FTS5 + vector、smart dedup、分层 skill evolution、持久 SQLite**。这与 engram"local-first embeddable"是同一生态位。

---

## 3. 参考链接

- Mem0: https://github.com/mem0ai/mem0
- memos(usememos): https://github.com/usememos/memos
- MemOS(MemTensor): https://github.com/MemTensor/MemOS
- OmniMemEval: https://github.com/MemTensor/OmniMemEval

---

## 4. 口径核对结论(2026-07-21,读 OmniMemEval 源码实证)

**决定性发现:OmniMemEval(= MemOS 的 LoCoMo 评测框架,同代码也驱动 Mem0)的口径与 engram 逐条相同**——不是"未统一",是**已对齐**:

| 轴 | OmniMemEval / MemOS | engram | 一致? |
|---|---|---|---|
| 类别 / 对抗题 | **只算 cat 1-4 = 1540 题,cat-5 对抗题从检索阶段硬编码排除**(`locomo_common.py:31` `category != 5`) | 同,1540,cat-5 排除 | **SAME** |
| 拒答 | 无 IDK 选项、无重试、无对抗处理(`prompts.py:59-90`) | force-answer `--no-idk-retry` | **SAME** |
| judge | LLM-judge 二元 CORRECT/WRONG,**"宽松打分——只要沾同一主题就算 CORRECT"**(`prompts.py:227`),默认 **gpt-4o-mini** | LLM-judge(relay)二元 | 风格 SAME,**模型/宽松度待核** |
| 聚合 | 1540 题 micro-average + 分类别(`locomo_eval.py:400-421`) | 同 | **SAME** |
| 检索预算 | top_k=20 **每 speaker**(~40 合并),无 chunk 子配额(`locomo_search.py:66-67,556`) | top_k=30 / chunk 12 / facts 18 | **DIFFERENT** |
| 答题模型 | env `ANSWER_MODEL`,无代码默认(未 pin) | relay 模型 | **DIFFERENT/未知** |

→ **口径对齐后的真实差距(同一 1540 题分母)**:

| 口径 | engram | 目标 | 差距 |
|---|---|---|---|
| **同栈实测(2026-07-26,唯一真可比)** | **85.71%** | **MemOS@同栈 82.40** | **engram 领先 3.31pp**(§5④) |
| ~~LoCoMo 可答(1540,cat-5 排除)~~ | ~~**65.4%**~~ | MemOS **88.83** / Mem0 **92.5** | ~~**~23pp**~~ **已过期**,见下 |
| ~~全盘 1986 含对抗题算 0 ~50.7%~~ | — | — | **作废**:竞品同样排除 cat-5,这是错误参照 |

> ⚠️ **上表第二行两端都已过期,别再引用**:(a) engram 侧 65.4% 是 2026-07-21 的旧数,
> 判题口径对齐(007)后诚实水位是 **83.70%**(bge-small 单 run)/ **85.4%**(bge-large 3-rep mean)
> —— 见 [`capability-and-product-north-star.md`](./capability-and-product-north-star.md);
> (b) 目标侧 88.83 / 92.5 是**竞品自报栈**分,同栈复现证明 MemOS 那侧含 6.43pp regime 红利
> (§5④)。**"~23pp 差距"是两端各错一次叠出来的,不是真差距。**
> **Mem0 的 92.5 尚未做同栈剥离,对 Mem0 的差距仍是未知数,不能套用 MemOS 的结论。**

### 📌 engram 的 LongMemEval 分数(2026-07-28,**已测,勿重跑**)

LongMemEval-S (cleaned) **全量 500** 已跑完两个 answerer 档,正本
[`specs/016-longmemeval-crossbench/verdict.md`](../specs/016-longmemeval-crossbench/verdict.md)
US4/US5/US6:

| 答题器 | 总分 | per-rep | 性质 |
|---|---:|---|---|
| **Qwen3.6-35B**(本地 vllm) | **80.80%** | [79.0, 78.4, 80.4] | **local-first 基线**(宪法 I,默认栈) |
| **deepseek-v4-pro**(API) | **86.00%** | [86.0, 85.4, 86.2] | 强 answerer 档(付费 API,非默认栈) |

**Δ = +5.20pp**,配对 McNemar χ²=8.45 p=**0.0049** SIGNIFICANT。涨幅集中在 Qwen
最弱的 preference(+33pp)/ temporal(+8.3)/ multi-session(+5.3)。

- **检索侧/上下文预算已探尽**(US5):gold rank 中位 = 2、bracket top-k 15/30/40/50 全 ns。
  瓶颈是 answer-side(87.5%),换更强 answerer 是唯一有效杠杆。
- **与竞品不可直接比**:MemOS 论文 89.20 / Mem0 blog 94.4 是 GPT-4o(-mini)口径 +
  自报 judge;engram 86% 是 v4-pro + mem0-aligned judge。但同 answerer 量级下
  engram+v4-pro 86% vs MemOS 论文 89.2,差距在 judge 宽松度 + 数据版本(cleaned vs 旧版)。
- **LoCoMo"强 answerer 反伤 opinion"不得外推到 LongMemEval preference**(LCoMo −5pp vs
  LME +33pp,题型本质不同)。

**⚠️ 两处未证实**:(1) Mem0 自家 `memory-benchmarks` 仓是未初始化的 git submodule(空),无法读其源码核对——上表 Mem0 列按"OmniMemEval 驱动 Mem0 + Mem0 论文惯例"推断,**未逐行验证**;(2) engram 的 judge 是否和 gpt-4o-mini 一样宽松未核——若 engram judge 更严,部分差距是 judge 口径伪影。

---

## 5. 战略观察(涨点方向的关键含义)

**① Mem0 的"赢法清单"= engram 已有的架构。** 多信号融合(semantic+BM25+entity)、ADD-only 抽取、实体链接、时间推理——engram 五条**全部已具备**。这意味着我们和 Mem0 的差距**大概率不在架构,在(a)口径可比性 (b)抽取/检索质量调优**,而非缺能力。→ 优先排查口径 + 抽取质量,而不是加新机制。

**② MemOS 是 engram 最贴身的对标。~~它证明纯本地栈能上 88+~~ —— 后半句已被同栈复现证伪(2026-07-26)。** 它的本地插件与 engram 定位完全一致(100% 本地、FTS5+vector、SQLite、零云),这条不变。但"88.83 证明纯本地栈能上 88+"**不成立**:用 MemOS 自家代码跑在 engram 同款答题模型 + 同款 embedder + 同一 judge 上,**MemOS = 82.40%,反而低于 engram 同口径 85.71%**(逐条见 [`memos-inhouse-locomo-repro.md §6`](./memos-inhouse-locomo-repro.md))。**88.83 里的 6.43pp 是 answerer + judge 的 regime 红利,不是本地栈的能力上界。**
> 原推论"天花板不受本地/离线限制"**结论依然成立,但论据要换**:不再靠"MemOS 拿到过 88",而靠"engram 自己在纯本地栈上已经 85.71%,且领先最贴身的本地竞品"。死规则(禁付费云 rerank)照旧不是借口——**恰恰相反,MemOS 默认栈带本地 reranker + graph 检索都没赢,说明堆这类组件不是出路。**

**③ 两家都强调"时间推理"和"实体链接"。** 与我们的 gap 分解一致:engram temporal 74.1%、single-hop 70.3% 的答题侧短板,很可能就在时间推理 + 实体链接的**质量**上,而非多跳检索。→ 支持"按瓶颈分兵、优先答题/抽取侧"而非只打多跳。

**④ ~~差距 ~23pp~~ —— 对 MemOS 的差距已实测分离完毕:伪影 −6.43pp,真差距 **engram 领先 3.31pp**(2026-07-26)。**

> **同栈复现结果**(方法/口径诚实项/产物见 [`memos-inhouse-locomo-repro.md §6`](./memos-inhouse-locomo-repro.md)):
> 固定答题模型(同一台 box、同一 Qwen3.6-35B-A3B-FP8)+ 固定 embedder(bge-large)+
> 同一 judge prompt 同一 judge 模型(deepseek-v4-flash)后 ——
>
> | | LoCoMo cat1-4(1540) | multi-hop | temporal | open-domain | single-hop |
> |---|---:|---:|---:|---:|---:|
> | **MemOS @ engram 同栈** | **82.40%** | 89.36% | 82.55% | 59.38% | 82.64% |
> | **engram `009-full-A-base`** | **85.71%** | 87.59% | 81.93% | 65.62% | 88.82% |
> | **Δ(engram − MemOS)** | **+3.31** | −1.77 | −0.62 | **+6.24** | **+6.18** |
>
> - **leaderboard 88.83 → 同栈 82.40 = −6.43pp,全部是 regime 伪影**(答题模型强度 + judge 宽松度)。
>   下面第 3 条"答题模型强度"这个假设**成立且量级远超预期**。
> - **真机制差距是负的**:MemOS 的 tree/graph 记忆组织**只在 multi-hop 赢 1.77pp**;
>   single-hop / open-domain 各输 6pp+。→ **"engram 落后 23pp、要追赶"这个战略前提对 MemOS 一支已作废。**
> - ⚠️ **最大残留混淆**:两边各用自家默认检索预算,engram 实测喂给 answerer **3262 tok/次**、
>   MemOS 仅 **~1059 tok/次(≈3 倍差)**。+3.31pp 里有多少来自"上下文更多"而非"记忆更好"未剥离。
>   另:MemOS 单次答题无误差带,且未做 1540 题配对 McNemar。**引用本结论必须带这三条 caveat。**
> - **对 Mem0 的 92.5 仍未做同样剥离** —— 它的差距**尚未分离**,不能套用本结论。

**原分析(保留为记录)**:分母/类别/拒答/聚合四轴与 engram 相同,但 **judge 宽松度不同**——engram 的 judge 比 Mem0 / OmniMemEval **严格得多**(逐条见 §6)。所以 23pp = judge 严格度伪影 + 真质量差距,两者未分离前不能当纯实力差。剩余贡献者按优先级:

1. **judge 严格度对齐(§6,最高 EV)**——engram judge 无"部分给分"、无"日期 ±14 天容差",两家都有。对齐后可回收伪影那部分。属口径改动 → 宪法 IV 声明新基线、单独 commit。
2. **答题 prompt 工程(免费真杠杆)**——Mem0 的 `ANSWER_GENERATION_PROMPT` 是 5 步推理(扫全部→实体校验→跨记忆合并→选最具体→时间锚定,`prompts.py:40-100`),engram 答题 prompt 简单得多。纯 prompt、可移植、过死规则。tplan 实验已证 prompt 是真杠杆。
3. ~~**答题模型强度**~~ —— **✅ 已剥离(2026-07-26),见上方灰框**。MemOS 答题模型未 pin 的怀疑被证实:同栈后 MemOS 掉 6.43pp。剥离办法(用 MemOS 自家代码跑 engram 同款栈)已执行完毕,方法与踩坑正本见 [`memos-inhouse-locomo-repro.md`](./memos-inhouse-locomo-repro.md)。**同一手法尚未对 Mem0 做**。
4. **检索/抽取质量**——以上剥离后剩余的,才是底层记忆抽取 + 检索质量的真差距,engram 的主战场。

**⑤ 对抗题(cat-5)工作与 leaderboard 无关。** 既然 Mem0/MemOS 都排除 cat-5,006 拒答闸即便 GO 也不会改变 LoCoMo 榜位——它服务的是 Synthius 那种把 cat-5 计入的**不同口径**。这回头印证了:006 NO-GO 没有损失榜位,且没为一个"榜上不计分"的切片花冤枉钱。对抗题归为"Synthius 口径下的独立目标",不进 LoCoMo 主线。

---

## 6. Judge 严格度逐条对比(源码实证,2026-07-21)

> 🚨 **本节结论已被 spec 007 落实,勿再当作待办引用(2026-07-27 核实)。**
> 下表比的是**旧的 strict judge**(`judgeSystemPrompt`,现 `runner.go:494-500`)。
> 007 已新增 **`judgeMem0AlignedSystemPrompt`**(现 `runner.go:502-513`),
> 「部分给分」在 L505、「±14 天日期容差 + 时长 ±50% + 相对日期」在 L508,
> 由 `--judge-mem0-aligned` 启用 + anti-放水 golden 门(26/26)守住。
> **本仓所有现行基线(85.71% / 89.03% / MemOS 同栈对跑)都是用对齐版跑的**
> ([`results-matrix-2026-07-26.md`](./results-matrix-2026-07-26.md) §1)。
> 因此本节末尾「**是一个具体、可修的口径缺口**」那句**已经不成立**——缺口已修。
> 该句曾被 [`locomo-score-levers.md`](./locomo-score-levers.md) 误引为"剩余未验杠杆 #1(~1.7pp)",
> 已在那边更正并除名。**本节自此仅作为「对齐前后差异」的历史记录保留。**

engram judge = `cmd/locomo-bench/runner.go:451-457`;Mem0 judge = `mem0/evaluation/benchmarks/locomo/prompts.py:218-245`;OmniMemEval judge = `OmniMemEval/scripts/utils/prompts.py:216-240`。三方都是二元 LLM-judge,但宽松度不同:

| 规则 | Mem0 | OmniMemEval | engram | 结论 |
|---|---|---|---|---|
| **部分给分** | gold 列表命中**≥1 项即 CORRECT**,零命中才 WRONG(`prompts.py:222`) | "沾同一主题即 CORRECT"(`prompts.py:227`) | **无**;"遗漏 gold fact 即 false"(`runner.go:456`) | **engram 最严** — 列举/计数/多跳题系统性吃亏 |
| **日期容差** | ±14 天 CORRECT,时长 ±50% CORRECT,相对日期匹配(`prompts.py:228`) | 对时间格式宽松(`prompts.py:229`) | "日期**不同就 false**"(`runner.go:455`) | **engram 最严** — temporal(321 题)吃亏 |
| **情绪/同义** | proud=fulfilled=accomplished,同价即对(`prompts.py:224,230`) | 主题相同即可 | 仅接受"同义改写" | engram 略严 |
| WRONG 触发 | 仅"零命中 或 完全跑题"(`prompts.py:236-238`) | 仅"非同一主题" | "矛盾 / 遗漏 / 名字·日期·数字错 / 说不知道" | **engram 最严** |

~~**engram judge 的注释自称"aligned with mem0ai/memory-benchmarks"(`runner.go:449`),但实际未对齐**:缺"部分给分"与"±14 天日期容差"两条 Mem0 的核心宽松规则。这是一个具体、可修的口径缺口。~~

✅ **已修(spec 007,2026-07-21 当日)**:新增 `judgeMem0AlignedSystemPrompt`(`runner.go:502-513`),
两条规则齐备(L505 部分给分 / L508 ±14 天 + 时长 ±50% + 相对日期),由默认关的
`--judge-mem0-aligned` 启用、fingerprint 追加 `;judge=mem0-aligned` 做口径隔离、
anti-放水 golden 夹具 26/26 守住。**旧 strict judge 作为 `judgeSystemPrompt` 保留**,
两套并存、可切换——上表因此仍是有效的"两套 judge 差在哪"对照,只是"待修"性质已消失。

**行动含义(已兑现,存档)**:
- ~~修 judge 是 `cmd/locomo-bench` 内的小改动,不碰引擎~~ → ✅ 已做,引擎零改。
- ~~能否"零成本重判旧 transcript"取决于旧答题产物是否还在~~ → 已不适用:对齐版 judge 已是
  canonical recipe 的常规口径,现行基线直接用它跑,无需回溯重判。
- **诚实边界(依然生效)**:judge 放宽会同时抬高**所有**被对比方在**本 harness 下**的分,
  只用于**口径对齐的公平比较**,不能与竞品 leaderboard 分直接混用宣称。
  ⇒ 这正是 [`results-matrix-2026-07-26.md`](./results-matrix-2026-07-26.md) §3「判题模型轴」
  与 §5.2「leaderboard 数字 vs 同栈数字」在执行的纪律。
