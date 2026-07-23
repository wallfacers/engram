# Feature Specification: 多查询检索 —— 提质型深召回(multi-query retrieval)

**Feature Branch**: `010-multi-query-retrieval`

**Created**: 2026-07-23

**Status**: Draft

**Input**: brainstorm 定稿 `docs/superpowers/specs/2026-07-23-multi-query-retrieval-design.md`

## 背景与动机

拉平 MemOS(88.83),当前 engram 提质路线 ~85.4%(bge-large 已转化召回赢),gap ~3.4pp。009 深召回诊断证明拉分卡点是**深层召回**——真靶心(答错且 gold 在池内)的 gold fact 在宽池里中位 rank 71–90、无一进 top-30,`outranked_by` 信号弥散无单一机制主导 → 重排救不动(US2 排序机制已 STOP),因 gold 根本没进候选前列。多跳 enumeration 题「需多 session 证据」是这批题的共性。

把 gold 捞进 top-30 有两条路,而 maintainer 的杠杆哲学(`docs/locomo-score-levers.md` 总账段)已定调**只认提质、反感加量**:

- **加量**(撑 top-k / 扩池):cat-top-k 已证有效(+0.9pp)但拿 context 税换(multi-hop context 2.4×),检索没变聪明、不可移植、带成本税 → 降级为 optional 非默认。
- **提质**(同 top-k=30 下让 gold 自己升上来):bge-large 式的赢(同预算、向量更强 → +1.3pp)。**本 feature 走这条。**

核心机制:多跳问题作为**单一 query** 嵌入得很差;把它**分解成子查询各自精检、再由引擎 RRF-of-RRF 融合**,让同时命中多个子部分的 fact 被多张选票顶进 top-30 —— 不加 context、可移植、无付费杠杆。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 引擎多查询融合机制 `SearchMulti`(Priority: P1)

作为 engram 引擎维护者,我要给引擎一个新的公共入口 `SearchMulti(ctx, subqueries []string, k int)`:它对每个子查询各跑一遍现有三信号 hybrid,再用引擎**已有的 RRF(k=60,免调参)**把 N 个排序列表当投票者融合成一个,返回正常 top-k。引擎**只收 `[]string`、永不碰 query-时 LLM**(拆不拆是调用方的事),且 `len==1` 时结果与现有 `Search` 逐字节相同——离线默认路径零改。

**Why this priority**: 这是不可被 adapter 重造的核心算法(融合机制必须留在引擎,宪法 II),也是整枪的地基。它纯离线、near-free(retrieval-only,不调答题模型)、当前机器窗口内单测即可验,是可独立交付并立即产出价值的 MVP。它触及引擎(`memory/retriever.go`),必须 contract-first、退化保真、与 US2 的 adapter 改动分离提交。

**Independent Test**: 纯离线单测 —— (a) parity:`SearchMulti(ctx, []string{q}, k)` 结果逐字节等于 `Search(ctx, q, k)`;(b) 融合 golden:小 fixture 里构造一个 gold fact 在单查询下 rank>k,喂入手写子查询数组,断言 RRF-of-RRF 后它进 top-k、且多子查询共同命中的 fact 排名高于单命中 fact(确定性,无需真 LLM);(c) `CGO_ENABLED=0 go build/test` 通过。

**Acceptance Scenarios**:

1. **Given** 一个 store 与 query `q`,**When** 调 `SearchMulti(ctx, []string{q}, k)`,**Then** 返回结果集与 `Search(ctx, q, k)` 逐字节相同(退化保真,离线默认不变)。
2. **Given** 一个 gold fact 在单查询 `q` 下 rank>k、但能被 `q` 分解出的两个子查询分别较高命中,**When** 调 `SearchMulti(ctx, subqueries, k)`,**Then** 该 fact 被 RRF-of-RRF 顶进 top-k。
3. **Given** 同一批子查询,**When** 一个 fact 被多个子查询命中、另一个只被单个子查询命中且各自 rank 相近,**Then** 共同命中的 fact 融合得分更高、排名更前(多选票机制可复算)。
4. **Given** `subqueries` 为空或含空串,**When** 调 `SearchMulti`,**Then** 行为有定义且不 panic(空→空结果或等价单查询;空串子查询被跳过,见 Edge Cases)。
5. **Given** 融合实现,**When** 复用现有 RRF 常数(k=60),**Then** 不引入任何对 LoCoMo 拟合的新可调权重。

---

### User Story 2 - 调用方 query 分解 + 三道门 gated 出货(Priority: P2)

作为 engram 评测工程师,我要在 adapter(`cmd/locomo-bench`)侧用 LLM 把 LoCoMo 问题分解成 ≤4 个子查询喂给 `SearchMulti`,并让这条提质路线必须越过三道门(纯 Go 契约 / 离线召回 / 端到端决胜)、且**证明 answer-context 不涨**(否则是偷偷加量,判负),才允许判 GO。

**Why this priority**: 这是实际拿分动作,但依赖 US1 引擎 API、需 box 端到端窗口验证、且拆解质量有不确定性,风险高于 US1。它纯 adapter(引擎零改),LLM 分解是调用方策略。

**Independent Test**: 离线召回门 —— 在固化 store 上用 canned/LLM 子查询复跑,断言目标题 gold 从 rank>30 升进 top-30、coverage@30 提升(仅诊断);端到端决胜门 —— 同机配对 `Search`(单查询)vs `SearchMulti`(分解),唯一变量=分解,box vllm Qwen 栈 + deepseek mem0-aligned judge + canonical recipe、repeats=3,McNemar 判 multi-hop above-noise、overall 及任一非目标类不回退、且 `answer_context_tokens` 不显著上升。

**Acceptance Scenarios**:

1. **Given** 一道多跳 LoCoMo 题,**When** adapter 用 LLM 分解,**Then** 产出 ≤4 个子查询(含原 query 作兜底);分解失败/超限时退化为单查询(`[]string{q}`),端到端等价现基线。
2. **Given** 分解后的子查询,**When** 调 `SearchMulti(ctx, subqueries, 30)`,**Then** 最终返回严格 top-k=30、喂答题的 context 预算与单查询同量级(提质,不加量)。
3. **Given** 固化 store 上的离线复跑,**When** 对目标多跳题跑召回门,**Then** gold 平均排名上升 / coverage@30 提升,且**无云 reranker、无任何付费杠杆**;coverage 增益**只作诊断不作 verdict**(008 US4 铁证)。
4. **Given** 同机配对端到端(唯一变量=分解),**When** 算配对 McNemar,**Then** 达 **multi-hop above-noise 提升 + overall 及任一非目标类不显著回退 + `answer_context_tokens` 不显著上升**方可判 GO;否则判 NO-GO 出货,保留为诊断能力。
5. **Given** GO/NO-GO 结论,**When** 收口,**Then** 写入 eval-log 与杠杆台账(`docs/locomo-score-levers.md`),并须超越加量对照 cat-top-k(+0.9pp 但带 context 税)——本特性要在**不加 context** 下拿到可比或更好的 multi-hop 转化才算提质赢。

---

### Edge Cases

- **子查询数组含原 query 之外只有 1 项**:即 `len==1` → 严格走 parity 路径,逐字节等于 `Search`(离线默认不变)。
- **空数组 / 空串子查询**:空数组行为有定义(空结果或错误,须在契约里定死);含空串的子查询被跳过,不参与融合、不 panic。
- **某子查询检索为空**:该投票者贡献空列表,RRF-of-RRF 视其对所有 doc 记 0 分,不影响其余子查询融合(缺信号静默降级,宪法 V)。
- **分解退化**:LLM 分解失败 / 返回 >4 项 / 全同质 → adapter 退化为单查询,端到端等价现基线(不得因分解失败而崩)。
- **隐性加量伪装**:若分解使 answer-context 显著上升(N 路 top-k 变相扩池喂给答题器)→ 视为加量,端到端门判负,不得报为提质赢。
- **端到端越不过决胜门**:如实判 NO-GO,保留为默认关的诊断能力,写入 eval-log 与台账(与 008 reranker / 009 cluster-sweep 同样诚实处理)。

## Requirements *(mandatory)*

### Functional Requirements

**US1 — 引擎多查询融合机制(引擎增量,退化保真,contract-first)**

- **FR-001**: 引擎 MUST 新增公共入口 `SearchMulti(ctx, subqueries []string, k int) ([]Result, error)`,对每个子查询各跑一遍现有三信号 hybrid 检索。
- **FR-002**: `SearchMulti` MUST 用**引擎已有的 RRF(k=60,tuning-free)**把 N 个子查询排序列表作为投票者融合成一个:`score(d) = Σ_i 1/(60 + rank_i(d))`(未出现在某列表记 0),返回正常 top-k。
- **FR-003**: `SearchMulti(ctx, []string{q}, k)` MUST 与现有 `Search(ctx, q, k)` 结果**逐字节相同**(退化保真);`Search` 保留为薄封装或二者共用同一核心,离线默认路径逐字节不变。
- **FR-004**: 引擎 MUST NOT 在检索路径引入任何 query-时 LLM;`SearchMulti` 只接收 `[]string`,拆解与否是调用方策略(宪法 I)。
- **FR-005**: 融合 MUST NOT 引入任何对 LoCoMo 拟合的新可调权重(复用现有 RRF 常数,守 tuning-free 可移植)。
- **FR-006**: `SearchMulti` MUST 为纯 Go、`CGO_ENABLED=0` 可构建/测试,MUST NOT 依赖任何云端或付费 reranker/recall 模型。
- **FR-007**: 空 / 含空串 / 单元素 `subqueries` 的行为 MUST 在契约中定死且不 panic(见 Edge Cases);某子查询检索为空 MUST 静默降级(该投票者对所有 doc 记 0),不影响其余融合(宪法 V)。
- **FR-008**: `SearchMulti` 是新增公共 API(宪法 III contract-first),MUST 为非破坏性,无 schema 变更。

**US2 — 调用方分解 + 三道门 gated(纯 adapter,引擎零改)**

- **FR-009**: adapter(`cmd/locomo-bench`)MUST 提供用 LLM 把问题分解成 ≤4 个子查询的策略(含原 query 兜底),并把子查询数组传入 `SearchMulti`;分解失败/超限 MUST 退化为单查询,端到端等价现基线。
- **FR-010**: 端到端评测 MUST 保持最终 top-k=30、answer-context 与单查询同量级;`answer_context_tokens` MUST NOT 显著上升(提质证明——涨了即判加量、NO-GO)。
- **FR-011**: 离线召回门 MUST 在固化 store 上复跑,断言目标多跳题 gold 从 rank>30 升进 top-30 / coverage@30 提升,**无云/付费杠杆**;coverage 增益 MUST NOT 单独用作 GO 依据(仅诊断)。
- **FR-012**: 端到端决胜门 MUST 同机配对 `Search` vs `SearchMulti`(唯一变量=分解),box vllm Qwen 栈 + deepseek mem0-aligned judge + canonical recipe、repeats=3,算配对 McNemar;判 GO 须同时满足 **multi-hop above-noise 提升 + overall 及任一非目标类不显著回退 + `answer_context_tokens` 不显著上升**。
- **FR-013**: 若未越过端到端决胜门,系统 MUST 判 NO-GO 出货,保留为诊断能力,并把结论(含 coverage/答题差分/context 对比)写入 eval-log 与 `docs/locomo-score-levers.md` 台账。
- **FR-014**: US2 的 adapter 改动 MUST NOT 修改 `memory/ embedding/ provider/ store/ internal/` 下任何引擎代码(`git diff --name-only` 于这些路径为空),且 MUST 与 US1 的引擎改动分开提交(归因分离,宪法 IV)。

### Key Entities

- **子查询数组(SubQueries)**:一道题分解出的 `[]string`(≤4,含原 query 兜底);`len==1` 即单查询 no-op。
- **融合排序(FusedRanking)**:N 个子查询排序列表经 RRF-of-RRF 合成的单一 top-k 列表;共同命中的 fact 因多选票排名更前。
- **提质约束记录(ContextParityCheck)**:配对端到端里 `answer_context_tokens` 单查询 vs 多查询的对比,证明 top-k 与 context 预算未涨。
- **分解策略(DecompositionPolicy)**:adapter 侧 LLM 拆解逻辑(何时拆、拆几个、失败退化),纯调用方策略,不入引擎。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**(US1 退化保真):`SearchMulti(ctx, []string{q}, k)` 与 `Search(ctx, q, k)` 结果逐字节一致(parity golden,离线默认零回归)。
- **SC-002**(US1 融合正确):确定性融合 golden 通过——构造的 gold fact(单查询 rank>k)经 RRF-of-RRF 进 top-k,且多子查询共同命中 fact 排名高于单命中 fact;同输入同输出可复算。
- **SC-003**(US1 纯 Go 离线):`SearchMulti` 在 `CGO_ENABLED=0` 下构建+测试通过,无云/付费 reranker 依赖,检索路径无 query-时 LLM。
- **SC-004**(US2 提质硬约束):端到端配对里最终 top-k=30 不变、`answer_context_tokens` 多查询 vs 单查询不显著上升(否则判加量/NO-GO)。
- **SC-005**(US2 召回诊断):目标多跳题在离线召回门上 gold 平均排名上升 / coverage@30 提升;此增益仅作诊断,不单独作 GO 依据。
- **SC-006**(US2 判定诚实):GO/NO-GO 完全由端到端配对 McNemar(multi-hop above-noise + 非目标类不回退 + context 不涨)决定;须超越加量对照 cat-top-k(+0.9pp 带 context 税)——在**不加 context** 下拿到可比或更好的 multi-hop 转化才算提质赢。反证基线:008 reranker −0.06pp/p=1.0、009 cluster-sweep +0.4pp(噪声内)。
- **SC-007**(引擎/适配器分离):US2 交付时 `git diff --name-only -- memory embedding provider store internal` 为空;US1 引擎改动与 US2 adapter 改动分属不同 commit。

## Assumptions

- 复用 009 的固化 store(HF `wallfacers/engram-locomo-artifacts` 的 `009-bge-chunks-store`,bge-large 1024d + chunks)与已有答题栈,不重新抽取即可跑离线召回门与端到端决胜门。
- 端到端决胜门在 box vllm Qwen 栈(远端隧道)+ deepseek mem0-aligned judge 上跑,机器窗口可用;判题沿用 mem0-aligned 口径(007)。分解用同一答题 LLM,每题一次轻量 query 重写(远比 filter-pool 读 200 候选便宜)。
- "目标类"= multi-hop(分解机制的主受益类);single-hop/temporal/open-domain 作非目标类监控不回退,不作主判据。
- 子查询上限 N≤4 是 adapter 侧提质策略(防 N 路检索退化成隐性扩池);引擎 `SearchMulti` 对任意 `len` 有定义,不硬编码 N。
- HyDE / 更好 fact 写入表示是同属提质路线的**后续**独立增量,本 spec 只做 query 分解 + 融合这一枪(YAGNI)。

## 宪法对齐(Constitution Check)

- **I 本地优先/离线**:`SearchMulti` 检索路径无 query-时 LLM、纯 Go/offline;LLM 分解是调用方 opt-in 策略,默认单查询路径不变。✅
- **II 引擎/适配器分离**:融合机制(不可重造的算法)入引擎;LLM 分解(调用方策略)留 adapter;US2 引擎零改。✅
- **III 契约优先/命名空间**:`SearchMulti` 新增公共 API contract-first、非破坏、无 schema 变更;`len==1` 退化保真。✅
- **IV 评测回归门(非协商)**:US2 三道门,端到端配对 McNemar 决胜 + context-parity 提质证明,US1/US2 提交分离。✅
- **V 优雅降级/诚实规模**:空子查询/空检索静默降级;越不过即 NO-GO 保留诊断;coverage 不作 verdict;不夸大。✅
- **死规则**:全程无付费/云 reranker 作杠杆;不撑 top-k、不扩池(反加量哲学)。✅
