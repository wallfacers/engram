# 竞品对标基准(涨点目标表)

> 🧭 **状态**: 活跃 · **目标**: 为 engram 涨点锚定外部竞品目标。MemOS 88.83 的机制
> 拆解**正本**见 [`memory-strategy.md`](./memory-strategy.md) 附;本文只记竞品分与差距,不重复拆解。
>
> 目的:为 engram 的"涨点"工作锚定外部目标。记录竞品公开分数 + 与 engram 现状的差距。
> **口径已核实(§4/§6)**:读源码后确认 —— 分母/类别(cat-5 排除)/拒答/聚合四轴与 engram **相同**,可比;唯 **judge 宽松度不同**(engram 更严,§6)+ 检索预算/答题模型不同。真实差距 ~23pp(同 1540 分母),其中一块是 judge 严格度伪影。竞品分仍来自各家自报模型栈,leaderboard 绝对值不可与 engram 直接混用,但**口径结构已对齐**。

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
| LoCoMo 可答(1540,cat-5 排除) | **65.4%** | MemOS **88.83** / Mem0 **92.5** | **~23pp**(同分母,可比) |
| ~~全盘 1986 含对抗题算 0 ~50.7%~~ | — | — | **作废**:竞品同样排除 cat-5,这是错误参照 |

**⚠️ 两处未证实**:(1) Mem0 自家 `memory-benchmarks` 仓是未初始化的 git submodule(空),无法读其源码核对——上表 Mem0 列按"OmniMemEval 驱动 Mem0 + Mem0 论文惯例"推断,**未逐行验证**;(2) engram 的 judge 是否和 gpt-4o-mini 一样宽松未核——若 engram judge 更严,部分差距是 judge 口径伪影。

---

## 5. 战略观察(涨点方向的关键含义)

**① Mem0 的"赢法清单"= engram 已有的架构。** 多信号融合(semantic+BM25+entity)、ADD-only 抽取、实体链接、时间推理——engram 五条**全部已具备**。这意味着我们和 Mem0 的差距**大概率不在架构,在(a)口径可比性 (b)抽取/检索质量调优**,而非缺能力。→ 优先排查口径 + 抽取质量,而不是加新机制。

**② MemOS 是 engram 最贴身的对标。** 它的本地插件与 engram 定位完全一致(100% 本地、FTS5+vector、SQLite、零云),且 LoCoMo 88.83。**它证明纯本地栈能上 88+**——所以 engram 的天花板不受"本地/离线"限制,死规则(禁付费云 rerank)不是借口。差距是实打实的质量差距。

**③ 两家都强调"时间推理"和"实体链接"。** 与我们的 gap 分解一致:engram temporal 74.1%、single-hop 70.3% 的答题侧短板,很可能就在时间推理 + 实体链接的**质量**上,而非多跳检索。→ 支持"按瓶颈分兵、优先答题/抽取侧"而非只打多跳。

**④ 差距 ~23pp,但其中一块是 judge 严格度伪影(源码已证实)。** 分母/类别/拒答/聚合四轴与 engram 相同,但 **judge 宽松度不同**——engram 的 judge 比 Mem0 / OmniMemEval **严格得多**(逐条见 §6)。所以 23pp = judge 严格度伪影 + 真质量差距,两者未分离前不能当纯实力差。剩余贡献者按优先级:

1. **judge 严格度对齐(§6,最高 EV)**——engram judge 无"部分给分"、无"日期 ±14 天容差",两家都有。对齐后可回收伪影那部分。属口径改动 → 宪法 IV 声明新基线、单独 commit。
2. **答题 prompt 工程(免费真杠杆)**——Mem0 的 `ANSWER_GENERATION_PROMPT` 是 5 步推理(扫全部→实体校验→跨记忆合并→选最具体→时间锚定,`prompts.py:40-100`),engram 答题 prompt 简单得多。纯 prompt、可移植、过死规则。tplan 实验已证 prompt 是真杠杆。
3. **答题模型强度**——MemOS 答题模型未 pin;若 leaderboard 用强模型而 engram 用 luna relay,是 regime 杠杆(非 engram 引擎)。
4. **检索/抽取质量**——以上剥离后剩余的,才是底层记忆抽取 + 检索质量的真差距,engram 的主战场。

**⑤ 对抗题(cat-5)工作与 leaderboard 无关。** 既然 Mem0/MemOS 都排除 cat-5,006 拒答闸即便 GO 也不会改变 LoCoMo 榜位——它服务的是 Synthius 那种把 cat-5 计入的**不同口径**。这回头印证了:006 NO-GO 没有损失榜位,且没为一个"榜上不计分"的切片花冤枉钱。对抗题归为"Synthius 口径下的独立目标",不进 LoCoMo 主线。

---

## 6. Judge 严格度逐条对比(源码实证,2026-07-21)

engram judge = `cmd/locomo-bench/runner.go:451-457`;Mem0 judge = `mem0/evaluation/benchmarks/locomo/prompts.py:218-245`;OmniMemEval judge = `OmniMemEval/scripts/utils/prompts.py:216-240`。三方都是二元 LLM-judge,但宽松度不同:

| 规则 | Mem0 | OmniMemEval | engram | 结论 |
|---|---|---|---|---|
| **部分给分** | gold 列表命中**≥1 项即 CORRECT**,零命中才 WRONG(`prompts.py:222`) | "沾同一主题即 CORRECT"(`prompts.py:227`) | **无**;"遗漏 gold fact 即 false"(`runner.go:456`) | **engram 最严** — 列举/计数/多跳题系统性吃亏 |
| **日期容差** | ±14 天 CORRECT,时长 ±50% CORRECT,相对日期匹配(`prompts.py:228`) | 对时间格式宽松(`prompts.py:229`) | "日期**不同就 false**"(`runner.go:455`) | **engram 最严** — temporal(321 题)吃亏 |
| **情绪/同义** | proud=fulfilled=accomplished,同价即对(`prompts.py:224,230`) | 主题相同即可 | 仅接受"同义改写" | engram 略严 |
| WRONG 触发 | 仅"零命中 或 完全跑题"(`prompts.py:236-238`) | 仅"非同一主题" | "矛盾 / 遗漏 / 名字·日期·数字错 / 说不知道" | **engram 最严** |

**engram judge 的注释自称"aligned with mem0ai/memory-benchmarks"(`runner.go:449`),但实际未对齐**:缺"部分给分"与"±14 天日期容差"两条 Mem0 的核心宽松规则。这是一个具体、可修的口径缺口。

**行动含义**:
- 修 judge(补部分给分 + 日期容差)是 `cmd/locomo-bench` 内的小改动,不碰引擎。属**口径改动 → 宪法 IV**:必须声明新基线、eval 结果单独 commit、并明确"这是对齐竞品口径,不是算法涨点"。
- ⚠️ 能否"零成本重判旧 transcript"取决于旧答题产物是否还在(此前都在 scratchpad、gitignored,可能已失效);若失效则需重跑答题(有 token 成本,过成本闸 + 授权)。
- 诚实边界:judge 放宽会同时抬高**所有**被对比方在**本 harness 下**的分,只用于**口径对齐的公平比较**,不能与竞品 leaderboard 分直接混用宣称。
