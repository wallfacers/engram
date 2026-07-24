# Phase 0 Research: Retrieval-Side Temporal Window Recall

设计已在 brainstorm 阶段收敛;本文件记录关键决策的 decision / rationale / alternatives。文献锚点回链 [docs/memory-strategy.md](../../docs/memory-strategy.md)(CLAUDE.md 认定的生物启发检索 P0 权威出处,citations 经 alphaXiv 建成),不在 plan 阶段重复 alphaXiv 检索。

## D1 — 瓶颈归因先于机制:免费召回诊断门(US1)

**Decision**:在写任何引擎机制前,先跑零答题/judge token 的 retrieval-only 四层诊断(解析覆盖 / event_date 覆盖 / gold rank 分布 / oracle 抬升),GO 才建召回臂。

**Rationale**:008 的决定性教训——reranker coverage +15.457pp 但端到端答题 −0.06pp(McNemar p=1.0)。coverage/召回增益 ≠ 答题增益。temporal 短板同时有召回侧(gold 深埋)与答题侧(区间/时长算术 T-2、候选去歧 T-3)两套假设([docs/locomo-score-levers.md](../../docs/locomo-score-levers.md) L284-285),不先锁定瓶颈就建机制会重蹈 008 覆辙。诊断门是 feature 的失败测试形态,也是止损闸。

**Alternatives rejected**:
- 直接建机制、把诊断当实现后验证门 → 若瓶颈其实在答题侧,白烧引擎工。
- 召回+答题双轨一起建 → scope 过大,违单一 spec 原则;且无法归因。

## D2 — oracle 上界形态:纯 event_date∈window 理想臂(不受 RRF 稀释)

**Decision**:US1 Layer 3 的 oracle 臂测**纯理想上界**——直接把所有落在 query 解析时间窗内的事实拉出,测其中有多少 Layer 2 深埋 gold 能进 top-30;**不**先测受 RRF 融合稀释的现实臂。

**Rationale**:上界都无头顶(oracle 抬升不显著)就直接止损,省得建了融合臂才发现被稀释。这是维护者在 brainstorm 明确要的"先测纯理想上界"。010/011/012 教训:gold 深埋 ≠ 机制能挖出;oracle 臂直接回答"时间窗召回**具体**能不能救起这些 gold"。

**Alternatives rejected**:
- 先测现实 RRF 融合臂 → 混入融合稀释,无法区分"时间窗信号弱"与"融合把它压下去"两种失败。

## D3 — 机制:独立召回臂 vs 后处理乘子升级

**Decision**:新增**独立召回臂**——`NamesByEventWindow` 范围查询把时间窗内事实(含深埋池外的)拉进候选池,产出自己的 rank list 作为 RRF 平权第 4 路。**不是**把现有 `applyTemporal` 软乘子改写为信号。

**Rationale**:现 `applyTemporal` 是**后处理重打分器**,只作用于已被语义/关键词门控的 fused 池(`pool = k×candidateMultiplier`,floor 100),够不着 rank 71-90 的深埋 gold。真正的"检索侧结构"必须**改变候选集本身**——这正是 MemOS MemReader 查询侧时间窗过滤的机制([docs/memory-strategy.md](../../docs/memory-strategy.md) §MemOS),Chronos 2603.16862 佐证(去 event 索引直接砍半准确率 → 结构化时间召回是硬杠杆)。

**Alternatives rejected**:
- 仅把 T_score 表达为 RRF 信号(复用现有池)→ 不改候选集,救不了深埋 gold,等于换皮乘子。
- 暴力扩大候选池 → 违维护者"信号非体量"哲学([[lever-philosophy-signal-not-volume]]);且 008 reranker 证明堆召回不转化答题。

## D4 — Fork A:保留既有 `applyTemporal` 后处理乘子(加性)

**Decision**:保留 `applyTemporal` 软乘子不动;召回臂为加性新杠杆。两者可同开。

**Rationale**:召回臂改**候选集**(挖深埋),乘子精修**池内序**(exp(-gap/τ) 软衰减,in-window 事实返回 1 无害),职责正交不冗余。保留 → 臂关时 byte-identical、现有 parity golden 不破、可逆、宪法 IV 面积最小。双重加权同向(臂 RRF 提名 + 乘子池内乘),是我们要的方向。与 011/012"加性影子信号、不碰既有路径"一脉。

**Alternatives rejected**:
- 换掉乘子(temporal 纯召回信号)→ 更干净但改现有行为、要重刷 parity golden、宪法 IV 面积更大。
- 交互风险(near-window 事实被乘子塌陷):由 US2 单测覆盖交互无害性;必要时臂拉进的事实其 T_score 若 <1 仅表示确在窗外邻近,乘子降权合理,不构成 bug。

## D5 — Fork B:不做 event 词法喂 FTS(降 P1,转诊断度量)

**Decision**:不实现"event_date 词法别名喂 FTS"这条冗余路径;转成 US1 Layer 0(解析覆盖率)一个度量。仅当 Layer 0 证实很多 temporal query 没被 `ParseTemporalIntent` 解析出时间窗,才回捞。

**Rationale**:召回臂已直接按 event_date 拉取,FTS 喂日期是同目标冗余(YAGNI)。真正的风险是"臂永不点火"(query 没解析出窗)——那病因在解析器,Layer 0 直接捕获。

**Alternatives rejected**:
- 顺带做 FTS 喂日期 → scope 膨胀、冗余路径,且未经诊断证明必要。

## D6 — 存储底座已就绪,无需新 migration

**Decision**:复用 migration v4(`v4TemporalIndexes`)已建的 `idx_memory_entries_event_start`/`_event_end` 索引与既有 `event_start`/`event_end`(nullable unix seconds)、`event_date`(nullable micros)列;**本 feature 零 migration、零 schema 改**。

**Rationale**:核查 `store/migrations.go`:v4 已建两索引,`temporalIntersects` 与 `ranksFromOrder(names)→map[string]int` helper 已在 `memory/`。召回臂 = 一条命中索引的 `WHERE event_end >= ? AND event_start <= ?` 范围查询 + 对结果套 `ranksFromOrder`。引擎净增缩至一个只读方法 + 一路信号接线。

**Alternatives rejected**:
- 新建 migration 加复合索引 → 现有单列索引已足够(SQLite 可用其一 + 过滤);honest-scale ~100k class 下索引扫描可接受。过早优化违 YAGNI。

## D7 — 区间相交语义 + 半开区间 + event_date 回退

**Decision**:相交判定 `event_end >= window.Start AND event_start <= window.End`(部分相交即纳入);`window` 一端为零值(before/after 开区间)时该侧无界;事实 `event_start`/`event_end` 均 NULL 时回退用 `event_date`(与 `applyTemporal` L334-336 同规则:`start, end = EventDate, EventDate`)。

**Rationale**:与既有 `applyTemporal`/`temporalIntersects` 语义一致,避免召回臂与乘子对"相交"判定不一致导致的行为撕裂。半开区间是 before/after 题的常态(`TimeWindow.Start`/`End` 之一为零值)。

**Alternatives rejected**:
- 严格包含(事实完全落窗内)→ 漏掉跨边界事件,过严。
- 忽略 event_date 回退 → 只有 event_start/end 的少数事实可召回,大幅缩小臂覆盖(多数 LoCoMo 事实只有 event_date)。

## D8 — 排名序:时间邻近(gap 升序)

**Decision**:召回臂内部按事件与时间窗的 gap 升序排名(完全落窗内 gap=0 并列,次级按 name 稳定序),再交 `ranksFromOrder` 转 RRF rank。

**Rationale**:RRF 只用序不用绝对分,时间邻近序把"最贴窗"的事实排前,符合 temporal 题直觉。稳定次级序(name)保证确定性(可复现,配合 parity)。

**Alternatives rejected**:
- 用 `TemporalScore` 绝对值排序 → RRF 只吃序,绝对值多余;且 exp 衰减在窗内全 1 无区分度。
