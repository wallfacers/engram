# Feature Specification: 归因门控的检索排序(evidence-gated retrieval ranking)

**Feature Branch**: `009-retrieval-attribution-gate`

**Created**: 2026-07-22

**Status**: Draft

**Input**: brainstorm 定稿 `docs/superpowers/specs/2026-07-22-retrieval-ranking-attribution-gate-design.md`

## 背景与动机

拉平 MemOS(88.83)/ Mem0(92.5),当前 engram LoCoMo 端到端 83.70%。错题诊断(A/B 两份)证明**检索排序**是 single-hop(45.5%)、multi-hop(45%)、temporal(≤16 条)三处最大错因:gold fact 在 store 内、在候选池内,却排名低于 top-k(竞争事实压过,如 `kundalini yoga` 被 `aerial yoga` 压过)。

但**盲改排序已被证伪**:008 cross-encoder rerank 拿 +15.457pp 召回,端到端答题 −0.06pp / p=1.0,temporal 净 −9(高翻转、低净收益)。真正卡点是**缺逐题检索归因**——A 和 B 独立栽在同一墙:没有持久化的逐题 retrieved hits,任何排序改动只能盲评端到端翻转,看不到 gold turn 是否进/出 top-k。故本 feature 先建归因证据基座(US1),再用证据门控一个定向排序改动(US2)。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 逐题检索归因 trace(Priority: P1)

作为 engram 评测工程师,我要对 LoCoMo 每道题拿到一条可审计的检索归因记录:gold 证据是否被召回、排第几名、被哪些竞争 fact 压过,并与该题答对/答错关联,从而把"检索排序是最大错因"从聚合断言细化为逐题证据,定位真正"gold 在池内但排名靠后"的靶心题。

**Why this priority**: 这是整枪的证据基座。没有它,US2 的任何排序改动都只能盲评(008 rerank 教训)。它纯 adapter(引擎零改)、near-free(retrieval-only,不调答题模型)、当前机器窗口内即可跑,是可独立交付并立即产出价值的 MVP。

**Independent Test**: 在固化 store 上对一组 LoCoMo 题跑归因 trace,产出 `trace.jsonl` + 分布表;人工抽查若干已知靶心题(如 conv-2-q-111 kundalini/aerial),trace 正确报出 gold 在池内、排名、以及压过它的竞争 fact。全程零答题 token 调用。

**Acceptance Scenarios**:

1. **Given** 一个固化 store 与该 conversation 的 gold 证据(session:turn),**When** 对某题跑 retrieval-only 归因,**Then** trace 记录该题 `gold_evidence` turn 集合、top-N `retrieved`(含 name/rank/rrf_score/per-signal rank/映射到的 gold turn)、以及 fact 级判定 `gold_in_pool` / `gold_rank` / `outranked_by`。
2. **Given** 一道 gold fact 明确在 store 但答错的题,**When** 归因与现有 `results-hybrid.jsonl` 的对错 join,**Then** 该题被划入四象限之一(top-k 答对 / top-k 答错→答题侧 / 池内排名靠后→US2 靶心 / 未进池→抽取侧),分类互斥且可复算。
3. **Given** 同一 query,**When** 连续两次生成其查询 embedding,**Then** 确定性探针报告两次向量是否 bit-identical(或落在声明的有界 δ 内),使"embedding 运行退化"这一噪声源可观测。
4. **Given** 归因跑完,**When** 汇总,**Then** 产出一张分布表把诊断的"排序题"精确切分为三象限计数,可复现(同输入同输出)。

---

### User Story 2 - 证据门控的定向排序改动(Priority: P2)

作为 engram 维护者,我要基于 US1 证据挑选并实现**一个**纯 Go、tuning-free 的排序改动(默认关),专治"gold 在池内但排名靠后"的竞争事实象限,并让它必须越过三道门(纯 Go 契约 / 离线归因 / 端到端决胜)才允许作为出货默认。

**Why this priority**: 这是实际拿分动作,但依赖 US1 证据决定改什么、且需端到端机器窗口验证,风险高于 US1。它触及引擎(`memory/retriever.go`),必须 contract-first、默认关、提交与 US1 分离。

**Independent Test**: 关闭时逐字节等于现基线(parity);打开时在 US1 trace 上目标象限 gold 排名上升;端到端同机配对 hybrid vs hybrid+US2,McNemar 判定 above-noise 且非目标类不回退。

**Acceptance Scenarios**:

1. **Given** US1 分布表指出竞争事实象限的主导模式,**When** 从候选机制(score-aware RRF / 近重去重 / 实体·时间锚约束)中选定一个实现,**Then** 该机制为纯 Go、offline、无任何云/付费 reranker 依赖。
2. **Given** 新排序选项默认关(`RetrieverOptions` 新 bool 零值),**When** 跑现有 parity golden,**Then** 检索结果与现基线逐字节一致(零回归)。
3. **Given** 新排序选项打开,**When** 在 US1 trace 上复跑离线归因,**Then** 目标象限的 gold 平均排名上升,且不引入云 reranker、不使用付费杠杆。
4. **Given** 同机配对端到端评测(唯一变量=排序机制),**When** 先跑目标类再全量 1540 并算配对 McNemar,**Then** 达 above-noise 且 overall 与任一非目标类不显著回退方可判 GO;否则判 NO-GO 出货、保留为诊断能力(coverage 增益不作 verdict)。

---

### Edge Cases

- **gold 证据无法解析**:某题 LoCoMo evidence 缺失/格式异常 → 该题标记 `gold_unresolved`,计入单独桶,不污染四象限分母。
- **fact↔gold turn 映射多义**:一个检索 fact 可能源自多个 turn,或一个 gold turn 抽出多个 fact → 采用 fact 级映射,命中任一 gold turn 即记为覆盖(避免 turn@k 对 fact 级 assoc 失明);映射规则须确定、可复算。
- **embedding 探针检出不确定**:若二次嵌入不一致 → US1 如实报告 δ 分布,不掩盖;US2 的端到端配对须把此噪声计入判定(不得把 embedding 抖动误读为排序增益)。
- **候选池边界**:gold fact 落在候选池之外(未进池)→ 归 US2 无法修的抽取/召回象限,US2 不得靠扩池"迎合"(那是另一条线)。
- **US2 越不过决胜门**:如实判 NO-GO,保留为默认关的诊断能力,写入 eval-log 与杠杆台账,不作出货默认(与 008 US1 reranker 同样处理)。

## Requirements *(mandatory)*

### Functional Requirements

**US1 — 归因 trace(纯 adapter,引擎零改)**

- **FR-001**: 系统 MUST 对每道被评测的 LoCoMo 题产出一条归因记录,包含 `gold_evidence`(session:turn 集合)、`retrieved`(top-N:name、rank、rrf_score、每路信号 rank、映射到的 gold turn)、`gold_in_pool`、`gold_rank`、`outranked_by`。
- **FR-002**: 归因 MUST 以 retrieval-only 方式运行(复用固化 store,**不调用答题模型**),不产生答题 token 成本。
- **FR-003**: gold 覆盖判定 MUST 在 **fact/chunk 级**映射到 gold turn(命中任一 gold turn 即覆盖),不得用 turn@k 聚合口径。
- **FR-004**: 系统 MUST 通过与现有答题结果(`results-hybrid.jsonl`)join,把每题划入四个互斥象限之一,并输出各象限计数分布表。
- **FR-005**: 系统 MUST 提供 embedding 查询确定性探针:对同一 query 两次嵌入并报告是否一致(或有界 δ)。
- **FR-006**: US1 全过程 MUST NOT 修改 `memory/ embedding/ provider/ store/ internal/` 下任何引擎代码(`git diff --name-only` 于这些路径为空)。
- **FR-007**: 归因产出 MUST 确定性可复算(同 store + 同题集 → 同 trace),且写入 gitignored 的 run 目录。

**US2 — 定向排序改动(引擎增量,默认关,gated)**

- **FR-008**: 系统 MUST 提供一个新的检索排序选项,通过 `RetrieverOptions` 的新布尔字段控制,**零值 = 现有三信号等权 RRF 行为逐字节不变**。
- **FR-009**: 选定的排序机制 MUST 为纯 Go、offline、`CGO_ENABLED=0` 可构建,且 MUST NOT 依赖任何云端或付费 reranker/recall 模型。
- **FR-010**: 排序机制的具体选择 MUST 由 US1 证据(竞争事实象限主导模式)驱动,从候选集(score-aware RRF / 近重去重 / 实体·时间锚约束)中选定,MUST NOT 引入对 LoCoMo 拟合的 per-signal 权重。
- **FR-011**: US2 MUST 通过三道门方可判 GO 出货默认:①纯 Go 契约门(单测 + parity 关时零变 + CGO_ENABLED=0);②离线归因门(US1 trace 上目标象限 gold 排名上升,无云/付费杠杆);③端到端决胜门(同机配对 McNemar above-noise + overall 及任一非目标类不显著回退)。
- **FR-012**: 若 US2 未越过端到端决胜门,系统 MUST 判 NO-GO 出货,保留为默认关的诊断能力,并把结论(含 coverage/答题差分)写入 eval-log 与杠杆台账;coverage 增益 MUST NOT 单独用作 GO 依据。
- **FR-013**: US2 的引擎改动 MUST 与 US1 的 adapter 改动分开提交(归因分离,宪法 IV)。

### Key Entities

- **归因记录(AttributionTrace)**:一道题的检索归因单元。属性:题 id、`gold_evidence` turn 集合、`retrieved` 列表(name/rank/rrf_score/per-signal ranks/mapped gold turn)、`gold_in_pool`、`gold_rank`、`outranked_by`、四象限标签、答题对错(join 得来)。
- **象限分布表(QuadrantDistribution)**:按四象限聚合的计数与占比,针对目标类(single/multi-hop/temporal)。
- **embedding 确定性报告(EmbeddingDeterminismProbe)**:同 query 两次嵌入的一致性/δ 记录。
- **排序机制开关(RankingOption)**:引擎侧默认关的布尔选项,零值保持现基线行为。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**(US1 覆盖):对目标题集 100% 产出归因记录(gold 可解析题),gold 不可解析题单独计桶不污染分母。
- **SC-002**(US1 可归因):诊断中被标为"排序"的错题,≥90% 能被 trace 明确归入四象限之一并给出 `gold_rank` 与(若在池内)`outranked_by`。
- **SC-003**(US1 免费):归因运行的答题模型调用次数 = 0(near-free 验证)。
- **SC-004**(US1 确定性):同 store + 同题集重复运行,trace 与分布表逐字段一致(可复算)。
- **SC-005**(US1 引擎零改):`git diff --name-only -- memory embedding provider store internal` 在 US1 交付时为空。
- **SC-006**(US2 零回归):排序选项关闭时,parity golden 与现基线逐字节一致。
- **SC-007**(US2 判定诚实):US2 的 GO/NO-GO 完全由端到端配对 McNemar(above-noise + 非目标类不回退)决定;coverage 增益不作 GO 依据。判定须超越反证基线(legacy temporal −0.3pp/p=1.0、008 reranker −0.06pp/p=1.0)。

## Assumptions

- 复用 008 的固化 store(`.locomo-run/007-us2/cov-store` 一类)与已有答题结果 `results-hybrid.jsonl`,不重新抽取、不重跑答题即可完成 US1。
- gold 证据可从 LoCoMo dataset 的 evidence 字段解析为 session:turn;解析器沿用 harness 现有口径。
- US2 的端到端决胜门在本地 vllm Qwen 栈(远端隧道)+ deepseek judge 上跑,机器窗口可用;判题沿用 mem0-aligned 口径(007)。
- "目标类"= single-hop + multi-hop + temporal;open-domain 不在本 feature 排序靶心内(其短板另有诊断)。
- fact/chunk 与 gold turn 的映射基于 entry 的 source 元数据;若元数据不足以精确映射,采用保守"命中任一 gold turn 即覆盖"并在 trace 标注映射置信度。

## 宪法对齐(Constitution Check)

- **I 本地优先/离线**:US1 retrieval-only 全离线;US2 纯 Go/offline,禁云 reranker。✅
- **II 引擎/适配器分离**:US1 纯 adapter 引擎零改;US2 走引擎公共选项契约、默认关。✅
- **III 契约优先/命名空间**:US2 contract-first,`RetrieverOptions` 零值不变;US1 不碰引擎 schema。✅
- **IV 评测回归门(非协商)**:US2 三道门,端到端配对 McNemar 决胜,提交分离。✅
- **V 优雅降级/诚实规模**:US2 越不过即 NO-GO 保留诊断;coverage 不作 verdict;不夸大。✅
- **死规则**:全程无付费/云 reranker 作杠杆。✅
