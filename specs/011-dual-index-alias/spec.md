# Feature Specification: 写入侧表示 —— dual-index alias 向量(提质型深召回)

**Feature Branch**: `011-dual-index-alias`

**Created**: 2026-07-24

**Status**: Draft

**Input**: brainstorm 定稿 `docs/superpowers/specs/2026-07-24-write-side-alias-embedding-design.md`

## 背景与动机

009/010 三处独立印证瓶颈是**深层召回**:真靶心题的 gold fact 中位 rank 71–90、无一进 top-30。010 把「query 侧分解 + RRF-of-RRF」做实并门②证伪(gold 各子查询均弱命中,融合顶不动反挤出),据此把提质方向**收窄到写入侧**——瓶颈是 gold fact 的**向量可发现性**,而非 query 侧改写。

链路探明:fact 抽取的 `aliases`(概念锚点,如 `painting`/`self-acceptance`)**已免费产出、已落库,却从不参与嵌入**——只喂 keyword/temporal 通道,semantic(向量)通道从未见过它们。009 店 conv0 探针:213 facts 中 **111(52%)有 alias**。

文献坐实并修正机制:**doc2query**(1904.08375)证文档扩展有效但**追加过多会稀释原文表示**;**Doc2Query++**(2510.09557)证 **naive 单向量 append 对强 dense encoder(bge-large)会注入噪声掉分**,**Dual-Index Fusion(原文向量与扩展向量分开、max-pool 聚合)才是 dense 正解**。本特性据此走 dual-index,**不走单向量 append**。

核心机制:有 alias 的 fact 产一条独立 name 的**影子向量**(aliases 合并嵌入)存入 `memory_embeddings`;retriever semantic 命中影子后**归并回源 fact**,同源 fact 的 text-cosine 与 alias-cosine **取 max**(max-pool,不双重计票)作为单一 semantic 分,再进现有三信号 RRF(k=60);**源 fact 的 text 向量完全不动** —— 提质、不加量、无稀释、tuning-free、无付费杠杆。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 引擎 dual-index alias 向量(Priority: P1)

作为 engram 引擎维护者,我要让**有 alias 的 fact** 在 semantic 通道从「概念/别名角度」也可发现:写入时为其 aliases 产一条独立 name 的影子向量存入 `memory_embeddings`;检索时 retriever 的 semantic 信号命中影子向量后归并回源 fact,同源 fact 的 text/alias cosine 取 max 作为该 fact 的 semantic 相似度(每 fact 仍只贡献一个 semantic 排名),再进现有三信号 RRF。**源 fact 的 text 向量、无 alias 的 fact、所有 chunk 的嵌入与检索结果逐字节不变**;影子 name 绝不出现在最终结果。

**Why this priority**: 这是不可被 adapter 重造的核心检索算法(dual-index 归并必须留在引擎,宪法 II),也是整枪的地基。纯离线、near-free(retrieval-only 可验)、当前机器窗口内单测即可验,是可独立交付并立即产出价值的 MVP。它触及引擎(`memory/embedder.go`、`memory/retriever.go`),必须 contract-first、退化保真、与 US2 的 adapter 改动分离提交。

**Independent Test**: 纯离线单测 —— (a) parity:无 alias 的 fact 与所有 chunk 的 semantic 检索结果逐字节等于现状;(b) 归并 golden:构造一个 gold fact 的 text 向量对 query 弱命中、但其 alias 影子向量对 query 强命中,断言该 fact 经 max-pool 归并后 semantic 排名上升、进入结果,且**影子 name 不泄漏**;(c) 去重:同源 fact 的 text 与 alias 双命中只产一条源 fact 结果;(d) `CGO_ENABLED=0 go build/test` 通过。

**Acceptance Scenarios**:

1. **Given** 一个含无 alias fact 与 chunk 的 store,**When** 检索,**Then** 这些 entry 的 semantic 相似度与最终排序逐字节等于现状(退化保真,离线默认不变)。
2. **Given** 一个 gold fact 的 text 向量对 query 弱命中、其 alias 影子向量强命中,**When** 检索,**Then** 该 fact 的 semantic 分取两者 max、排名上升并进入结果。
3. **Given** 同一源 fact 的 text 向量与 alias 影子向量都被 semantic 命中,**When** 归并,**Then** 结果中该 fact 只出现一次(去重),semantic 通道只为它计一次票(不双重计票)。
4. **Given** 任一 alias 影子向量命中,**When** 返回结果,**Then** 结果里只含源 fact 的真实 name,影子 name(`#alias` 约定)绝不泄漏。
5. **Given** 融合实现,**When** 复用现有 RRF 常数(k=60)与无参数 max-pool,**Then** 不引入任何对 LoCoMo 拟合的新可调权重(无 α)。
6. **Given** embedding client 为 nil(离线),**When** 检索,**Then** 无任何向量、semantic 整体缺席,keyword+entity 照常,aliases 仍走原 keyword 通道(per-signal 降级,不崩)。

---

### User Story 2 - adapter re-embed 固化店 + 三道门 gated 出货(Priority: P2)

作为 engram 评测工程师,我要在 adapter(`cmd/locomo-bench`)侧对 009 固化店**只重嵌入不重抽取**产出 alias 影子向量,并让这条提质路线越过三道门(纯 Go 契约 / 分层离线召回 / 端到端决胜),证明**同 top-k=30、answer-context 不涨**下拿到目标类转化,才允许判 GO。

**Why this priority**: 这是实际拿分动作,但依赖 US1 引擎能力、需 box 端到端窗口验证、且 alias 覆盖(52%)/短标签的天花板有不确定性,风险高于 US1。它纯 adapter(引擎零改),re-embed 编排与三道门是调用方评测策略。

**Independent Test**: 分层离线召回门 —— 在 009 固化店上产/不产 alias 影子向量两版,同 query 检索,**主看「gold 有 alias」子层** gold rank delta 与 coverage@30 delta(全局会被 48% 无 alias 层稀释);仅诊断。端到端决胜门 —— 同机配对 baseline 店 vs treatment 店(唯一变量=alias 影子向量),box vllm Qwen 栈 + deepseek mem0-aligned judge + canonical recipe、repeats=3,配对 McNemar,目标类 above-noise、非目标类不回退、`answer_context_tokens` 不涨。

**Acceptance Scenarios**:

1. **Given** 009 固化店,**When** adapter re-embed,**Then** 为有 alias 的 fact 产 alias 影子向量、不重抽取(不调抽取 LLM)、不改 fact 内容与 text 向量;失败/缺向量退化为无该影子(per-signal 降级)。
2. **Given** 分层离线召回门,**When** 对目标 multi-hop / open-domain 题跑 single(无影子)vs treatment(有影子),**Then** 输出「gold 有 alias」子层与全局各自的 gold rank delta / coverage@30 delta;**无云 reranker、无任何付费杠杆**;coverage 增益**只作诊断不作 verdict**(008 US4 铁证)。
3. **Given** 分层召回门结果,**When** 「gold 有 alias」子层 gold 未净升 top-30,**Then** 判 NO-GO 止损,不启动端到端决胜门(省 box 答题窗口)。
4. **Given** 同机配对端到端(唯一变量=alias 影子向量),**When** 算配对 McNemar,**Then** 达 **目标类 above-noise 提升 + overall 及任一非目标类不显著回退 + `answer_context_tokens` 不显著上升 + 最终 top-k=30 恒等**方可判 GO;否则判 NO-GO 出货,保留为默认关能力。
5. **Given** GO/NO-GO 结论,**When** 收口,**Then** 写入 eval-log 与杠杆台账(`docs/locomo-score-levers.md`),对比反证基线(008 reranker −0.06/p=1.0、009 cluster-sweep +0.4 噪声内、010 分解 NO-GO),并在**不加 context** 下拿到目标类转化才算提质赢。

---

### Edge Cases

- **无 alias 的 fact / 所有 chunk**:无影子向量,semantic 相似度与检索结果逐字节等于现状(退化保真)。
- **alias 去重后为空**(全是 Content 已含词或空串):不产影子向量,等价无 alias(逐字节不变)。
- **影子向量命中但源 fact 已被 supersede/删除**:归并时源 fact 不存在 → 该影子命中被丢弃,不产生悬空结果、不 panic。
- **同一源 fact 多 alias**:aliases 合并为**一条**影子向量(非每 alias 一条),避免影子向量爆炸稀释;同源 text/alias 取 max 后只计一票。
- **embedding client 未配置(离线)**:无任何向量,semantic 缺席,aliases 仍走原 keyword 通道;dual-index 静默降级(宪法 V)。
- **隐性加量伪装**:本机制不扩 top-k、不扩池、不加 context(仅新增同源向量,max-pool 后 semantic 每 fact 仍一票)→ 结构上无加量可能;门③ 仍记账 `answer_context_tokens` 坐实。
- **端到端越不过决胜门**:如实判 NO-GO,保留为默认关能力,写入 eval-log 与台账(与 008 reranker / 010 分解同样诚实处理)。

## Requirements *(mandatory)*

### Functional Requirements

**US1 — 引擎 dual-index alias 向量(引擎增量,退化保真,contract-first)**

- **FR-001**: 引擎 MUST 为**有 alias 的 fact** 产一条独立 name(`#alias` 约定)的影子向量,其嵌入文本为该 fact 的 aliases 合并(去掉 Content 已含词),存入 `memory_embeddings`。
- **FR-002**: retriever 的 semantic 信号 MUST 在命中 alias 影子向量时**归并回源 fact**:同源 fact 的 text-cosine 与 alias-cosine **取 max** 作为该 fact 的单一 semantic 相似度(**不双重计票**),semantic 通道每 fact 仍只贡献一个排名,再进现有三信号 RRF(k=60)。
- **FR-003**: 无 alias 的 fact、所有 chunk、以及有 alias fact 的 **text 向量** MUST 保持逐字节不变(退化保真);影子 name MUST NOT 出现在最终 `[]Result`(检索前 resolve 回源 fact 并去重)。
- **FR-004**: 融合 MUST NOT 引入任何对 LoCoMo 拟合的新可调权重(max-pool 无参数 + 复用 RRF k=60,**无 α**,守 tuning-free 可移植)。
- **FR-005**: dual-index alias 向量 MUST 为纯 Go、`CGO_ENABLED=0` 可构建/测试,MUST NOT 依赖任何云端或付费 reranker/recall 模型;检索路径 MUST NOT 引入 query-时 LLM。
- **FR-006**: MUST 无 schema 变更(`memory_embeddings` 复用,影子为独立 name row,`entry_name` PK 不变);为非破坏性新增能力(宪法 III)。
- **FR-007**: 边界行为 MUST 在契约中定死且不 panic:alias 去空→不产影子;影子命中但源 fact 缺失→丢弃该命中;embedding client nil→semantic 整体缺席、per-signal 降级(宪法 V)。
- **FR-008**: 有 alias fact 的 semantic 召回增强是**预期的意图变更**,MUST 按宪法 IV 声明新 baseline;对无 alias fact 与 chunk MUST 提供 parity 证明。

**US2 — adapter re-embed + 三道门 gated(纯 adapter,引擎零改)**

- **FR-009**: adapter(`cmd/locomo-bench`)MUST 提供对 009 固化店**只重嵌入不重抽取**产 alias 影子向量的编排(retrieval-only,不调抽取/答题 LLM);baseline/treatment MUST 都在 canonical 店的 run-local 副本上 Backfill,baseline 副本随后剥离影子,treatment 副本保留;canonical MUST NOT 作为运行店打开/写入。两臂相同复制/重嵌入路径保证唯一变量=影子向量;失败/缺向量退化为无该影子。
- **FR-010**: 端到端评测 MUST 保持最终 top-k=30、answer-context 与 baseline 同量级;`answer_context_tokens` MUST NOT 显著上升(提质证明——涨了即判加量 NO-GO)。
- **FR-011**: 分层离线召回门 MUST 输出「gold 有 alias」子层与全局各自的 gold rank delta / coverage@30 delta,**无云/付费杠杆**;coverage 增益 MUST NOT 单独用作 GO 依据(仅诊断);「gold 有 alias」子层 gold 未净升 top-30 MUST 判 NO-GO 止损、不启动门③。
- **FR-012**: 端到端决胜门 MUST 同机配对 baseline 店 vs treatment 店(唯一变量=alias 影子向量),box vllm Qwen 栈 + deepseek mem0-aligned judge + canonical recipe、repeats=3,算配对 McNemar;判 GO 须同时满足 **目标类 above-noise 提升 + overall 及任一非目标类不显著回退 + `answer_context_tokens` 不显著上升 + top-k=30 恒等**。
- **FR-013**: 若未越过决胜门,系统 MUST 判 NO-GO 出货,保留为默认关能力,并把结论(含子层/全局 coverage、答题差分、context 对比)写入 eval-log 与 `docs/locomo-score-levers.md` 台账。
- **FR-014**: US2 的 adapter 改动 MUST NOT 修改 `memory/ embedding/ provider/ store/ internal/` 下任何引擎代码(`git diff --name-only` 于这些路径为空),且 MUST 与 US1 的引擎改动分开提交(归因分离,宪法 IV)。

### Key Entities

- **Alias 影子向量(AliasShadowVector)**:有 alias 的 fact 的一条独立 name(`#alias`)向量,嵌入文本 = 该 fact aliases 合并;存 `memory_embeddings`,检索时归并回源 fact,不作为独立结果。
- **归并 semantic 分(MaxPooledSemantic)**:同源 fact 的 text-cosine 与 alias-cosine 取 max 得到的单一 semantic 相似度;每 fact 一票进 RRF。
- **提质约束记录(ContextParityCheck)**:配对端到端里 baseline vs treatment 的 `answer_context_tokens` 与 `final_top_k` 对比,证明未加量。
- **分层召回诊断(StratifiedRecallDiagnostic)**:按「gold fact 是否有 alias」分层的 gold rank / coverage@30 delta,主判据在「有 alias」子层。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**(US1 退化保真):无 alias fact 与所有 chunk 的 semantic 相似度与最终排序逐字节不变;有 alias fact 的 text 向量不变(parity golden,离线默认零回归)。
- **SC-002**(US1 归并正确):确定性 golden 通过——gold fact 的 alias 影子向量强命中时经 max-pool 归并使其 semantic 排名上升进入结果;同源 double-hit 去重为一票;影子 name 不泄漏;同输入同输出可复算。
- **SC-003**(US1 纯 Go 离线 tuning-free):`CGO_ENABLED=0` 下构建+测试通过,无云/付费 reranker 依赖,检索路径无 query-时 LLM,无新可调权重(无 α)。
- **SC-004**(US2 提质硬约束):端到端配对里最终 top-k=30 不变、`answer_context_tokens` treatment vs baseline 不显著上升(否则判加量/NO-GO)。
- **SC-005**(US2 分层召回诊断):目标类在「gold 有 alias」子层上 gold 平均排名上升 / coverage@30 提升;此增益仅作诊断、不单独作 GO 依据;子层不升即止损。
- **SC-006**(US2 判定诚实):GO/NO-GO 完全由端到端配对 McNemar(目标类 above-noise + 非目标类不回退 + context 不涨 + top-k 恒等)决定;对比反证基线(008 reranker −0.06pp/p=1.0、009 cluster-sweep +0.4pp、010 分解 NO-GO),在**不加 context** 下拿到目标类转化才算提质赢。
- **SC-007**(引擎/适配器分离):US2 交付时 `git diff --name-only -- memory embedding provider store internal` 为空;US1 引擎改动与 US2 adapter 改动分属不同 commit。

## Assumptions

- 复用 009 固化店(HF `009-bge-chunks-store`,bge-large 1024d + chunks)与已有答题栈,只重嵌入产 alias 影子向量、不重抽取,即可跑分层召回门与端到端决胜门。
- 端到端决胜门在 box vllm Qwen 栈(远端隧道)+ deepseek mem0-aligned judge 上跑;判题沿用 mem0-aligned 口径(007)。
- 「目标类」= 分解机制主受益类,以 open-domain(64.6% 最大短板,opinion/preference 多样问法)+ multi-hop 为主监控;single-hop/temporal 作非目标类监控不回退。门②分层数据确认哪类实际受益。
- alias 覆盖(009 店 conv0 52%)与短标签质量是第一枪天花板;**增强抽取产问法式 paraphrase(Doc2Query++ coverage-guided QG,需重抽取)** 是同属写入侧的**后续**独立增量,本 spec 只做「现有 aliases 入 dual-index」这一枪(YAGNI)。
- 影子向量用独立 name(`#alias` 约定)存 `memory_embeddings`,不改 `entry_name` PK 语义;`memory_entries` 无对应 row(影子只有向量),retriever 命中后即 resolve 回源 fact。

## 宪法对齐(Constitution Check)

- **I 本地优先/离线**:检索路径无 query-时 LLM;alias 向量与 fact 同源 embedder、纯 Go/offline;离线时 semantic 缺席、aliases 走原 keyword 通道。✅
- **II 引擎/适配器分离**:dual-index 归并机制(不可重造算法)入引擎;re-embed 编排 + 三道门留 adapter;US2 引擎零改。✅
- **III 契约优先/命名空间**:无 schema 变更、非破坏新增;无 alias/chunk 与 text 向量退化保真;影子 name 不泄漏契约。✅
- **IV 评测回归门(非协商)**:US2 三道门,分层召回诊断止损 + 端到端配对 McNemar 决胜 + context-parity 提质证明;有 alias fact 声明意图变更 + 新 baseline;US1/US2 提交分离。✅
- **V 优雅降级/诚实规模**:空 alias/缺向量/离线 per-signal 降级;越不过即 NO-GO 保留能力;coverage 不作 verdict;52% 覆盖天花板诚实标注不夸大。✅
- **死规则**:全程无付费/云 reranker 作杠杆;不撑 top-k、不扩池、不加 context(dual-index 是表示提质,非加量)。✅
