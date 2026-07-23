# Phase 0 Research: 多查询检索(multi-query retrieval)

关键未知点已解;每条给出决策、依据、被否方案。引擎证据来自 `memory/retriever.go`(`Search`:121、`SearchWithDiagnostics`:128、`fuseRRF`:830、`rrfK=60`:18、`ranksFromOrder`)。

## 决策表

| # | 问题 | 决策 | 依据 | 被否方案 |
|---|---|---|---|---|
| D1 | `len==1` 如何逐字节退化保真 | `SearchMulti` 在规范化(trim/去空/去重)后若剩 1 个子查询,**直接委托 `SearchWithDiagnostics(ctx, sub, k)` 并返回其 `[]Result`**,不进二级融合 | 二级 RRF 会重写 `Score`(变成 RRF-of-RRF 值)→ 破坏 parity。短路是唯一逐字节相等的路径 | 「统一走融合、N=1 时融合退化」——否:融合后 Score 语义变了,SC-001 挂 |
| D2 | 每子查询精检多深(候选深度 D) | 每子查询用 **`SearchWithDiagnostics(ctx, sub_i, D)`**,其中 **`D = k*candidateMultiplier`(=300 for k=30,复用现有常数)** 作为 L_i 深度;RRF-of-RRF 融合后**再截断到最终 k=30**。**注意**:必须传 `D` 而非 `k`——`SearchWithDiagnostics` 内部会截断到传入的 k(retriever.go:211),若传 `k=30` 则 L_i 只有 30 条,009 诊断的 gold(中位 rank 71–90)会落在单子查询 31–90 名被截掉、融合捞不回,击穿本特性存在理由 | 单查询内部本就用 `k*candidateMultiplier` 候选池(retriever.go:140);多查询沿用同一深度作 L_i = 不新增**答题**预算(最终喂答题仍 top-k=30),D 只是内部融合候选宇宙 | 「每子查询也只取 top-k(传 k)」——否:gold 在 rank∈(k, D] 被过早截断,融合无从捞回 |
| D3 | RRF-of-RRF 与既有后处理(temporal/superseded/rerank/cluster-sweep)的次序 | 后处理**留在每子查询内部**(`SearchWithDiagnostics` 已逐子查询施加);二级 `fuseRRF` **只对有序名单按 rank 聚合**(`ranksFromOrder(L_i)` → `fuseRRF(ranks...)`),融合层不再加后处理 | RRF 本就只吃 rank、不吃 score(retriever.go:830-836);每个 L_i 已是「完整 hybrid 排名」。融合层保持纯净 = 无新可调旋钮 | 「把 N 路候选合并后再统一跑一遍 temporal/rerank」——否:引入融合层新逻辑 + 重复后处理,破坏 tuning-free |
| D4 | 子查询数 N 上限放哪 | 引擎 `SearchMulti` **对任意 `len` 有定义、不硬编码 N**;`N≤4` 上限是 **adapter 侧提质策略**(`cmd/locomo-bench`) | 引擎是机制、不是策略(宪法 II)。N 上限是防隐性扩池的调用方旋钮,冻进引擎契约会限制别的集成方 | 「引擎硬编码 N≤4」——否:把调用方策略焊进引擎契约 |
| D5 | query 分解器用什么 | 复用 `cmd/locomo-bench` 现有**答题 LLM provider**,每题一次轻量重写产出 ≤4 子查询(含原 query 兜底);失败/超时/>N → 退化 `[]string{q}` | 无新依赖;每题一次轻量调用远比 filter-pool 读 200 候选便宜(009 教训)。分解是 adapter 策略,不入引擎 | 「引擎内置分解」——否:引擎永不碰 query-时 LLM(宪法 I) |
| D6 | 空 / 空串 / 重复子查询 | 规范化:逐个 `TrimSpace`、丢空串、精确去重。规范化后为空 → 返回 `nil`(等价 `Search("")`);剩 1 个 → 走 D1 短路;某子查询检索返回空 → 该投票者对所有 doc 记 0,不影响其余(静默降级) | 与 `Search` 对空 query 的现有语义一致(retriever.go:133-136);缺信号降级是宪法 V | 「空串报错」——否:破坏优雅降级 |
| D7 | 并发还是串行跑 N 个子查询 | **串行**(N≤4,cheap);融合前对 doc 名排序保证确定性 | 确定性是 golden 前提(SC-002 可复算);N≤4 时并发收益微小、却引入合并竞态 | 「并发精检」——否:确定性风险 > 延迟收益 |
| D7b | 同一 doc 被多子查询命中如何加权 | 天然由 RRF 处理:`score(d)=Σ_i 1/(60+rank_i(d))`,命中越多子查询、rank 越前 → 分越高 → 顶进 top-k | 这正是多跳想要的副作用(共同命中 = 多选票);复用 `fuseRRF` 累加语义,零新代码 | —— |
| D8 | reranker 交互 | reranker 保持 optional/默认关(死规则);若配置则在**每子查询内**跑,不在融合层;出货路径 reranker off | 死规则:云 reranker 不作杠杆;融合层不引入 rerank | 「融合后统一 rerank」——否:触碰死规则边界且改融合层 |

## 提质 vs 加量的边界论证(本特性存在理由)

- **cat-top-k(加量,已否)**:把喂**答题器**的 top-k 从 30 撑到 150 → context 2.4× 税。变的是「几个」。
- **本特性(提质)**:每子查询的**内部候选池深度**沿用单查询本就用的 `k*candidateMultiplier`(D2);**最终喂答题器仍 top-k=30**。变的是「哪 30 个」——RRF-of-RRF 让共同命中的 gold 顶上来。
- 硬校验(FR-010 / SC-004):端到端配对里 `answer_context_tokens` 多查询 vs 单查询**不显著上升**。若上升 → 说明分解变相扩了喂答题的量 = 隐性加量,判负。这是与 cat-top-k 的可测分界线。

## 反证基线(决胜门须超越)

- 008 cross-encoder reranker:coverage +15.457pp 但端到端 −0.06pp / p=1.0(coverage 幻觉,不转化)。
- 009 cluster-sweep:配对 +0.4pp 落噪声带(表观增益是批间答题噪声漂移)。
- cat-top-k:+0.9pp 但带 context 税(加量对照)。
- ⇒ 本特性须在**不加 context** 下拿到 multi-hop above-noise 转化,才算提质赢;否则 NO-GO 保留诊断。

## 未决 → 已决

原 spec 无 `[NEEDS CLARIFICATION]`;上表 D1–D8 覆盖实现层全部开放点。无遗留未决项进入 Phase 1。
