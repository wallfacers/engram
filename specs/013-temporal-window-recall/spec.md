# Feature Specification: Retrieval-Side Temporal Window Recall

**Feature Branch**: `013-temporal-window-recall`

**Created**: 2026-07-24

**Status**: Draft

**Input**: User description: "走spec — 检索侧结构 P0 杠杆:把 temporal 从融合后软乘子升级为一等召回信号(时间窗召回臂 → RRF 第 4 路),直击 LoCoMo temporal 类 82.24% 短板。brainstorm 收敛:先跑免费召回诊断门确认瓶颈在召回侧,再建机制。"

## Context & Motivation *(informative, non-normative)*

engram 已有相当完整的 temporal 件:`ParseTemporalIntent` 把 query 确定性解析为 `TimeWindow`(绝对日期 / 相对表达 / before-after 序 / current-historical 态);`event_start`/`event_end`/`event_date` 已在 schema(migration v2);`applyTemporal` 在 RRF 融合**之后**施加软乘子 `TemporalScore = exp(-gap/τ)` + 可选硬过滤。

结构性天花板:`applyTemporal` 只作用在**已被语义/关键词门控过的 fused 候选池**上——它是**后处理重打分器**,不是**召回塑形器**。若某条时间匹配的 gold 事实语义上没浮进候选池(rank 71-90 深埋),temporal 乘子根本看不到它。LoCoMo temporal 类停在 82.24%(次弱类),且对暴力 rerank 脆弱(008: cross-encoder 按单轮相关性重排把 temporal 砸 −9)。

本 feature 把 temporal 升级为**独立召回臂**:落在 query 时间窗内的事实,由一条按 event_date 直接拉取的 SQL 范围查询进池,产出自己的 rank list 作为 RRF 平权第 4 路。这与 MemOS MemReader 的查询侧时间窗过滤同构,且 Chronos(去 event 索引砍半准确率)提供机制证据。

**关键前置纪律(008 铁律)**:coverage/召回增益 **不等于** 答题增益。因此本 feature 在写任何机制代码前,先跑一个**免费的 retrieval-only 召回诊断门**证实瓶颈确在召回侧;诊断不过则止损,不建机制。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 免费召回诊断门:证实/证伪 temporal 召回瓶颈 (Priority: P1)

作为 engram 维护者,在投入任何引擎机制改动前,我要用一个**零答题/零 judge token** 的 retrieval-only 诊断,判定 LoCoMo temporal 类 82.24% 的短板到底在召回侧(gold 深埋池外)还是在解析/抽取/答题侧,从而决定是否值得建时间窗召回臂。

**Why this priority**: 这是整个 feature 的止损闸,也是它的"失败测试形态"。008 的教训(reranker coverage +15pp 但端到端 −0.06pp)证明,不先锁定瓶颈就建机制会白烧工。此门必须先于 US2/US3,且独立可跑、独立产结论——即便 US2/US3 从不实现,此门本身就交付一个诚实的方向判定。

**Independent Test**: 在固化的 LoCoMo store(bge-large 正本)上,对 temporal 类问题跑四层度量,输出一张分层诊断表,无需调用答题器/judge,near-free。

**Acceptance Scenarios**:

1. **Given** 一个含 temporal 类问题与 gold 标注的 LoCoMo store,**When** 运行召回诊断,**Then** 输出 Layer 0(时间意图解析覆盖率)、Layer 1(gold event_date 覆盖率)、Layer 2(gold 当前 rank 分布 / 是否深埋池外)、Layer 3(纯 event_date∈window oracle 臂把多少深埋 gold 抬进 top-30)四项度量,全程零答题/judge token。
2. **Given** 诊断四层结果,**When** 评估 GO 判据,**Then** 仅当 Layer 0 解析覆盖率有意义 **且** Layer 1 event_date 覆盖率有意义 **且** Layer 2 gold 确实深埋 **且** Layer 3 oracle 臂能把有意义数量的 gold 抬进 top-30 时,判 GO(进入 US2);任一层塌则判 NO-GO 并记录病因归属(解析器 / 抽取侧 / 答题侧)。
3. **Given** 诊断为 baseline/oracle 配对,**When** 度量 rank 分布,**Then** baseline 臂(temporal 召回关)与 oracle 臂(纯时间窗召回)在同一 query 集上配对,delta 可直接归因于时间窗召回。

---

### User Story 2 - 时间窗召回臂进 RRF 第 4 路(引擎机制) (Priority: P2)

作为 engram 引擎,当一条查询带时间意图时,我要用一条按 event_date 直接拉取的召回臂,把落在解析时间窗内的事实(含语义/关键词漏掉、深埋池外的)拉进候选池,并以其时间邻近排名作为 RRF 平权第 4 路信号参与融合——从而把时间匹配的深埋 gold 抬进 top-K。

**Why this priority**: 这是 feature 的实质机制,但**条件于 US1 诊断 GO**。它是引擎新能力(contract-first),必须默认关、降级安全、parity 不破。

**Independent Test**: 引擎单测(免费,offline):(a) 时间窗范围查询正确性;(b) 召回臂关时检索结果 byte-identical(parity golden 不破);(c) 无时间意图 / 无 event_date 时召回臂静默掉出 RRF 和(降级);(d) 一条深埋池外、但 event_date 落窗内的事实,在臂开启后进入 top-K。

**Acceptance Scenarios**:

1. **Given** 一批带 `event_start`/`event_end`/`event_date` 的事实与一个解析出的时间窗,**When** 召回臂运行,**Then** 且仅当事实的事件区间与时间窗相交时,该事实被纳入臂的候选集,并按时间邻近(gap 升序)排名。
2. **Given** 一条查询无时间意图(`ParseTemporalIntent` 返回 ok=false),**When** 检索运行,**Then** 召回臂不产出任何 rank,检索结果与臂未引入时 byte-identical。
3. **Given** 一条事实无任何 event 日期,**When** 召回臂运行,**Then** 该事实不进臂候选集,且不因此报错(逐信号降级)。
4. **Given** 一条语义/关键词排名深埋在候选池 cutoff 之外、但 event_date 落在时间窗内的 gold 事实,**When** 召回臂开启,**Then** 该事实通过第 4 路 RRF 贡献进入融合结果的 top-K。
5. **Given** 召回臂作为平权信号进 RRF(k=60),**When** 融合运行,**Then** 不引入任何需网格搜索的 temporal 专属权重(守 tuning-free)。

---

### User Story 3 - 端到端答题回归验证(box, repeats=3) (Priority: P3)

作为 engram 维护者,在时间窗召回臂落地后,我要用配对端到端 LoCoMo eval 证明 temporal 类**答题分**真涨且总分不回归,才认定它是一个可出货的赢——以答题分为准,coverage 仅作诊断(008 铁律)。

**Why this priority**: 宪法 IV 硬门。US1 诊断 GO + US2 机制通过引擎单测仍**不足以**宣称赢:必须端到端配对验证。此门条件于 US2 完成。

**Independent Test**: 在 box 上跑配对 eval(召回臂 off vs on),repeats=3 覆盖 temp=1.0 答题噪声,McNemar 配对检验 temporal 类答题分,并确认总分不回归。

**Acceptance Scenarios**:

1. **Given** 同一固化 store 与同一答题/judge 栈,**When** 跑召回臂 off/on 配对 eval(repeats=3),**Then** temporal 类答题分 on ≥ off 且差分在答题噪声带外(否则记 within-noise / NO-GO)。
2. **Given** 配对 eval 结果,**When** 检查非 temporal 类,**Then** 总分不回归(single-hop / multi-hop / open-domain 不被害),否则记为污染 NO-GO。
3. **Given** eval-config 改动,**When** 提交,**Then** 与算法改动分开 commit(宪法 IV 归因)。

---

### Edge Cases

- **时间窗解析失败但问题确是 temporal**:`ParseTemporalIntent` 覆盖率 < 阈值 → 病因在解析器(Fork B: event 词法喂 FTS / 解析增强),不在召回结构;US1 Layer 0 捕获此情形并止损。
- **gold 事实无 event_date**:抽取未打日期 → 召回臂无从查起;US1 Layer 1 捕获,病因归抽取侧。
- **时间窗过宽(如整年)命中过多事实**:召回臂候选集膨胀;RRF 平权融合自然稀释低相关项,但需确认不淹没其他信号(US2 单测覆盖 + US3 端到端把关)。
- **before/after 开区间(单侧无界窗)**:`TimeWindow` 一端为零值表示无界;范围查询需正确处理半开区间。
- **事件区间跨越窗边界(部分相交)**:采用区间相交语义(`event_end >= window.Start AND event_start <= window.End`),部分相交即纳入。
- **召回臂与既有 `applyTemporal` 后处理乘子同开**:两者同向(臂拉进池 + 乘子池内精修),不得双重加权导致 in-window 事实被过度提升或 near-window 事实被乘子塌陷;保留乘子(Fork A)但确认交互无害。
- **无时间意图查询的 parity**:绝大多数查询无时间意图,召回臂必须对它们完全无副作用(byte-identical),否则破坏既有 parity golden。

## Requirements *(mandatory)*

### Functional Requirements

**诊断门(US1)**

- **FR-001**: 系统 MUST 提供一个 retrieval-only 的 temporal 召回诊断,对 LoCoMo temporal 类问题输出四层度量,全程不调用答题器或 judge(零答题/judge token)。
- **FR-002**: 诊断 MUST 度量 Layer 0 —— temporal 类 query 中时间意图被成功解析出非空时间窗的占比。
- **FR-003**: 诊断 MUST 度量 Layer 1 —— temporal 类 gold 事实携带可解析 event 日期的占比。
- **FR-004**: 诊断 MUST 度量 Layer 2 —— temporal 类 gold 事实在当前融合排名中的 rank 分布(尤其落在候选池 cutoff 之外的比例)。
- **FR-005**: 诊断 MUST 度量 Layer 3 —— 一条纯 event_date∈window 的 oracle 召回臂能把多少 Layer 2 中深埋的 gold 事实抬进 top-30(配对 delta,归因于时间窗召回)。
- **FR-006**: 诊断 MUST 输出一个明确的 GO/NO-GO 判定:四层全过判 GO;任一层塌判 NO-GO 并记录病因归属(解析器 / 抽取侧 / 答题侧 / 召回天花板不足)。

**召回臂机制(US2,条件于 US1 GO)**

- **FR-007**: 引擎 MUST 提供一条按 event 日期范围拉取事实名称的查询能力,输入解析出的时间窗(含半开区间),返回事件区间与窗相交的事实,采用区间相交语义。
- **FR-008**: 检索 MUST 在查询带时间意图时,将时间窗召回臂的排名(按时间邻近)作为一路信号纳入 RRF 融合。
- **FR-009**: 召回臂 MUST 作为 RRF 平权信号参与,不引入任何需网格搜索的 temporal 专属融合权重(守 tuning-free)。
- **FR-010**: 召回臂 MUST 默认关闭(由既有 temporal 选项与"查询带时间意图"共同门控);任一条件不满足时,检索结果与臂未引入时 byte-identical(既有 parity golden 不破)。
- **FR-011**: 召回臂 MUST 逐信号降级:查询无时间意图 → 臂不产 rank 并静默掉出 RRF 和;事实无 event 日期 → 不进臂候选集且不报错。
- **FR-012**: 召回臂 MUST 与既有 `applyTemporal` 后处理乘子共存且交互无害(保留乘子,Fork A);不得因两者同开而对同一事实产生破坏性的双重加权。
- **FR-013**: 引擎改动 MUST 只走引擎公共契约增量;适配器(locomo-bench / mcp)不得重实现召回臂算法或绕过引擎。

**端到端回归(US3,条件于 US2 完成)**

- **FR-014**: 系统 MUST 支持在配对(臂 off / on)下跑端到端 LoCoMo eval,repeats≥3 覆盖答题噪声,并对 temporal 类答题分做配对检验。
- **FR-015**: 端到端验证 MUST 以答题分为准(coverage 仅作诊断),并确认非 temporal 类总分不回归。
- **FR-016**: eval-config 改动 MUST 与算法改动分开提交(宪法 IV 归因)。

### Key Entities

- **TimeWindow**:query 的规范化时间意图(起止边界,可半开;意图/态元数据)。已存在(`memory/temporal.go`),本 feature 复用,不重定义其解析。
- **Event-dated fact**:一条记忆事实,携带 `event_start`/`event_end`/`event_date`(已在 schema v2)。召回臂据此判断与时间窗相交。
- **Temporal recall arm**:新的召回信号——按 event 日期范围拉取相交事实并按时间邻近排名,产出一路 RRF rank list。
- **诊断分层结果**:四层度量(解析覆盖 / event_date 覆盖 / rank 分布 / oracle delta)+ GO/NO-GO 判定 + 病因归属。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**(US1 门):召回诊断在**零答题/judge token** 下产出四层度量表与明确 GO/NO-GO 判定;维护者据此在**不写任何机制代码**的前提下,判定 temporal 短板的病因归属。
- **SC-002**(US1 天花板):诊断的 Layer 3 量化"时间窗召回臂能把多少深埋 temporal gold 抬进 top-30"——这是拟建机制的可测天花板;若天花板不足(oracle 抬升数量不显著),feature 在此止损,不进 US2。
- **SC-003**(US2 parity):对不带时间意图的查询,引入召回臂后检索结果与引入前 100% 一致(既有 parity golden 全绿,零破)。
- **SC-004**(US2 降级):在无时间意图或事实无 event 日期时,检索不因召回臂产生任何错误或整体失败(逐信号降级,per-signal 缺失静默掉出)。
- **SC-005**(US2 召回):US1 Layer 3 中被 oracle 臂识别为"可抬升"的深埋 gold 事实,在真实召回臂开启后,有可测比例进入融合结果 top-K(现实臂逼近 oracle 上界)。
- **SC-006**(US3 端到端,宪法 IV):在 repeats≥3 的配对 eval 下,temporal 类答题分较基线**真涨且在噪声带外**,同时非 temporal 类总分不回归;否则记 within-noise / NO-GO 并诚实收口(不出货、不计为赢)。
- **SC-007**(引擎/适配器隔离,宪法 II):US2 落地后 `git diff --name-only -- memory embedding provider store internal` 反映的引擎改动**只**是本 feature 声明的契约增量;适配器侧无算法重实现。

## Assumptions

- **诊断复用既有基建**:US1 诊断复用 LoCoMo harness 的分层召回诊断基建(012 `runDoc2QueryRecallDiagnostic` 同构),不新建独立 eval 框架。
- **固化 store 为诊断底座**:US1/US3 在既有 bge-large 正本 store(`009-bge-chunks-store` 类)上跑,不重新抽取(避免混入抽取方差)。
- **`ParseTemporalIntent` 复用**:本 feature 不重写时间意图解析;若 US1 Layer 0 显示解析覆盖不足,解析增强是独立后续(Fork B / P1),不在本 feature 范围。
- **event_date 抽取为既有能力**:抽取侧对 event 日期的标注是既有 pipeline 行为;若 US1 Layer 1 显示覆盖不足,抽取增强是独立后续,不在本 feature 范围。
- **Fork A(留乘子)**:保留既有 `applyTemporal` 后处理乘子,召回臂为加性新杠杆;不删乘子以保 parity 与最小改面。
- **Fork B(不做 FTS 喂日期)**:event 词法别名喂 FTS 降 P1,除非 US1 Layer 0 证实解析覆盖不足才回捞。
- **box 为 US3 底座**:端到端 eval 在远端 box(bge-large + 本地答题栈)上跑,近免费;引擎本身保持 local-first,不依赖 box。
- **无云 rerank 杠杆**:本 feature 的任何涨点必须来自纯 Go、offline、客户端可移植的时间窗召回结构;绝不借助付费云 rerank(死规则)。
- **scope 边界**:本 feature 只做"检索侧时间窗召回臂";答题侧 temporal 修复(区间/时长算术契约 T-2、候选按 event_date 去歧 T-3)、supersedes 信念修订链均为独立后续,不在范围内。
