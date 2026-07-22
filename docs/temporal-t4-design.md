# Temporal 短板诊断与 T-4(第 4 路时间融合)可行性设计

本文件是 LoCoMo **temporal 类**错题归因 + **T-4 独立时间检索路**的持久正本(tracked,跨环境不失传)。结论以此为准;实验细节见 [`specs/008-locomo-score-levers/eval-log.md`](../specs/008-locomo-score-levers/eval-log.md)、杠杆台账见 [`locomo-score-levers.md`](./locomo-score-levers.md)。

**来源与核验**:诊断由外部 agent 产出(基于 `.locomo-run/008-us4-e2e/results-hybrid.jsonl` 57 条 temporal 错题),本 session 已独立核验所有引用的代码锚点与反证数字为真(见文末「核验记录」)。

---

## 最终 Verdict

- **工程层面:GO** —— T-4 可由纯 Go / SQLite / 标准库完成核心检索路径,**无需云模型、无需云 reranker**(符合宪法 I/V 与 [[no-paid-rerank-lever]] 死规则),可 contract-first 立实验。
- **出货 / 涨分层面:NO-GO(当前)** —— T-4 直接覆盖的只是**部分检索/排序错误**,解决不了 14 条 IDK、4 条时长算术、4 条 gold 摩擦(共 22/57 在答题/评测侧)。**必须端到端配对评测越过旧 temporal-score 的 within-noise 反证**(003 记录 temporal Δ−0.3pp / p=1.000),才允许转默认。coverage 一律仅作诊断。

---

## 一、57 条 temporal 错题分布(互斥优先级:IDK → 时长算术 → gold → 检索代理 → 候选选择)

| 主因 | 数量 | 占 57 | 主要层 |
|---|---:|---:|---|
| IDK 弃答 | 14 | 24.6% | 答题侧 |
| 区间/时长不计算 | 4 | 7.0% | 答题侧 |
| 候选日期/时间状态选错 | 19 | 33.3% | 检索与答题交界 |
| 相对/模糊/不一致 gold | 4 | 7.0% | 数据/评测侧 |
| 检索未召回或排序侧代理 | 16 | 28.1% | 检索侧 |

**分层结论**:
- **答题侧明确缺口**:14 IDK + 4 时长算术;range 不暴露给 reader 又卡死了 T-2(区间算术)。
- **检索侧明确机会**:≥16 条能由**纯检索排序变化**修复(用"仅换排序后端到端转正"作可审计代理,28.1%)。
- **交界缺口**:19 条需"候选内容 + 结构化日期 + 按时间排序展示"共同完成,**单加 T-score 不足** → 更依赖 T-3 候选展示消歧,而非 T-4 窗口匹配。
- **"When did X happen?" 类**通常没有查询时间窗,`ParseTemporalIntent` 无从知道正确日期 → 这类靠 T-3,不靠 T-4。

**诊断限制(诚实)**:`results-hybrid.jsonl` **没存逐题 retrieved hits**,严格"gold turn 未进 top-k"无法精确复原;16/57 是"仅改排序后转正"的代理数。补充反证:reranker 把 temporal turn recall +14.56pp(79.47→94.03)但端到端 temporal **−9 题**(82.24→79.44)→ **coverage 只能定位候选变化,不能作 verdict**([[008-us1-reranker-verdict]] 同源教训)。

**全量题号审计**(conv/q):
- IDK:3/2,3/9,3/41,3/43,4/20,4/40,5/55,6/11,6/37,6/53,6/61,6/64,7/37,8/70
- 时长:5/47,6/62,7/46,8/39
- gold:2/48,5/4,8/24,9/68
- 检索代理:0/33,0/74,1/13,2/56,3/7,3/26,4/56,5/10,6/10,6/59,7/45,7/56,7/81,9/17,9/52,9/65
- 候选选择:其余 19 条(集合已核验无重复无遗漏)。

代表例:conv-8-q-39 两次就诊隔三月却答 May 2023(找到端点没做日期差,时长算术);conv-6-q-1 指定 2022-03-16 问活动 gold=bowling 答 Gaming(多娱乐活动未按日期消歧,候选选择);conv-2-q-48 原文 "last Sunday"(session 2023-07-31)预测 2023-07-23 与原文一致但 gold 写 "Sunday before 3 July"(**gold 异常**)。

---

## 二、引擎时序现状(已核验的代码事实)

| 能力 | 当前事实 | 缺口 |
|---|---|---|
| event_date | `Entry.EventDate` 单 instant(SQLite 微秒);YYYY-MM/YYYY 解析为该月/年**第一天** | 粗粒度时间塌成伪精确点 |
| 时间范围 | `event_start`/`event_end` 已存在(`store/migrations.go:165-166`,秒,已索引 185-186) | 与 event_date 单位不一致;未成统一契约 |
| 抽取 | prompt 已要 event_start/end,相对时间由抽取模型按 session date 解析;pipeline 只做 ISO 解析 | 任意 NL→range 非纯 Go;错误结构会被检索放大 |
| 固化库实况 | 3,744 entries;1,564 有范围字段;**仅 85 条非零宽度范围**;1,683 aliases | "有范围列"≠范围覆盖充分 |
| query intent | `TimeWindow{Start/End/Intent/State/AnchorEntity/AnchorTime/Fuzzy}`(`memory/temporal.go`) | 不支持 next month / N weeks ago / first weekend / early-late / season / how long |
| 时间打分 | overlap=1 否则 exp(-gap/τ),默认 τ=30 天,未知时间中性 | 是**三路 RRF 后乘分**(`retriever.go:185 applyTemporal`),不是第 4 路 |
| 融合 | semantic+keyword+entity 三路 RRF;时间别名并入 keyword;before/after 有补充召回 | 时间无独立 rank list;reranker 会完全丢弃原融合分 |
| 对外结果 | `Search.Result` 只有 EventDate;bench reader 只渲染 `[event: YYYY-MM-DD]` | **range 已入库但答题侧看不见** |
| superseded | 单向 superseded_by,手工/curation 设置 + 一跳 penalty | 无反向边/cycle 校验/slot 语义/递归解链;008 库**实际 0 条链** |
| 启用状态 | `TemporalScore`(retriever.go:48)/hard filter/conflict resolution **均默认关**;008 baseline 未启用 temporal | 现有能力非出货默认,也未过端到端门 |

**语义坑**:verbatim chunk 把 session date 写进 EventDate,实际更接近 **mention_time** 而非事件发生时间 —— 正是 [`docs/memory-freshness-and-retrieval-policy.md`](./memory-freshness-and-retrieval-policy.md) 要求拆开的概念。

---

## 三、T-4 落地设计草案

### 1. 数据结构与迁移(additive)
- `event_start`/`event_end` 成唯一规范事件范围:UTC、闭区间、both-or-none;`end<start` 拒写。
- 新增 `mention_time`:verbatim chunk 的 session date 写这里,**不再参与 T-score**。
- `event_date` 保留为兼容字段;旧记录只可退化为 `[event_date,event_date]`,**不得猜测原始精度**。
- YYYY / YYYY-MM / YYYY-MM-DD 分别展开为完整年/月/日范围,而非第一天点值。
- `memory.Result` / MCP `memory_search` / bench reader **additive** 暴露 event_start/end;旧 event_date 输出不变。
- 保留 legacy `TemporalScore bool` 维契约;新增明确模式 `TemporalFusionRRF`,零值仍为 off。

### 2. 第 4 路契约
候选集先由三路产生:`C = semantic ∪ keyword ∪ entity`。时间路**只在有确定查询边界时对 C 排序**(避免"同月但完全无关"全库事件涌入)。
- `T(e)=1` 当事件范围与查询窗相交,否则 `exp(-gap/τ)`;**未知 event range 不进时间 rank list**。
- `RRF(e)=Σ 1/(60+rank_{sem|kw|entity|time})`。
- 约束:state-only / 无 anchor / 不可确定的 fuzzy 查询**不产生时间路**;before/after 补充召回须由 anchor entity/alias 约束;**禁止 hard filter 作出货模式**,缺时间信号时字节级退化到三路;superseded 判断与时间路**正交**(历史窗允许旧事实,仅 current 查询才压制,不能"一见 temporal 就全豁免");**T-4 主评测路径不接任何云 reranker,本地 reranker 也不作过门依赖**。

### 3. Contract-first 与三道评测门(宪法 IV,constitution.md:68)
先在 spec/data-model/engine contract **冻结**时间单位、区间边界、兼容读写、错误语义、JSON 字段再实现。**重新解释既有 event_date 是破坏性变化,必须避免或按 MAJOR 处理**。
1. **纯 Go 契约门**:范围归一化、relative parser、RRF 第 4 路、未知时间降级、稳定 tie-break、旧库迁移、MCP/API round-trip、`CGO_ENABLED=0`、默认关 parity。
2. **离线诊断门**:同固化 store、同 top-k/quota,**逐题持久化 gold turn 与 retrieved hits**(补上本次缺的);coverage / 范围覆盖率 / 错误时间采用率仅作诊断。
3. **端到端决胜门**:同进程配对 hybrid vs hybrid+temporal-rrf,唯一变量=融合机制;先 temporal 321 再全量 1540。要求 temporal 配对 McNemar **above-noise** + overall 及任一非目标类不显著回退 + context budget 不增。**评测口径/prompt 改动单独提交**。
> 反证基线(必须超越):[`specs/003-bio-retrieval-locomo/eval-log.md:149`](../specs/003-bio-retrieval-locomo/eval-log.md) 记录 legacy temporal **Δ−0.3pp / p=1.000**(B救活6/B搞砸7,零效果)、overall −0.8pp within-noise。新第 4 路不得因实现形式不同而跳过门禁。

### 4. 纯 Go 离线边界与风险
- **可纯 Go**:schema/migration、ISO 与有限相对表达式归一化、区间距离、SQL 索引、RRF、诊断、全部单元/契约测试。
- **不能纯规则保证**:从任意对话文本识别事件及真实范围 → 接受调用方直接提供 range,或用**本地可替换**抽取器;不得依赖云。
- **风险排序**:①错误 event time 被额外加权 ②第四票引入同窗噪声 ③查询无时间约束→覆盖天花板 ④旧 chunk 拿 mention time 当 event time ⑤range 暴露后 reader 仍不做算术。
- **缓解**:候选集内时间排序、unknown 不投票、mention/event 拆分、逐题 trace、T-2/T-3/T-4 分别消融。

---

## 四、与答题侧杠杆的分工(temporal 拉平不能只靠 T-4)

| 杠杆 | 治哪类错题 | 层 | 状态 |
|---|---|---|---|
| **T-2** 区间/时长算术(依赖 range 暴露给 reader) | 4 时长算术 | 答题(+引擎暴露) | 待立项 |
| **T-3** 候选日期消歧展示(按时间排序 + 结构化日期) | 19 候选选择大头 | 检索+答题 | 待立项 |
| **T-4** 第 4 路时间融合 RRF | ≤16 检索排序 | 检索 | 本文,contract-first GO / 出货 NO-GO |
| force-answer(口径对齐) | 部分 14 IDK | 答题 | 已验:temporal +5 题但属口径对齐非涨点,见 [locomo-score-levers.md](./locomo-score-levers.md) |
| gold 摩擦(4 条) | —— | 数据/评测 | 不可修,判题噪声,诚实计入 |

---

## 核验记录(本 session 独立复核,2026-07-22)

- `event_start`/`event_end` 列 + 索引:`store/migrations.go:165-166,185-186` ✅
- legacy temporal 反证 **Δ−0.3pp / p=1.000**:`specs/003-bio-retrieval-locomo/eval-log.md:149` ✅ 一字不差
- `TemporalScore bool` 默认关 + `applyTemporal` post-RRF 乘分:`memory/retriever.go:48,147,185` ✅
- `TimeWindow{Intent,AnchorEntity,Fuzzy}`:`memory/temporal.go` ✅
- 引擎零改:`git diff --name-only -- memory embedding provider store internal mcpserver` 空 ✅

关联:[[competitive-targets]]、[[locomo-reference-83]]、[[008-us1-reranker-verdict]]、[[verdicts-go-to-tracked-docs]]。
