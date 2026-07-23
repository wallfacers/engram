# 多查询检索:提质型深召回(query 分解 → 引擎 RRF 融合)— 设计

**日期**:2026-07-23 · **状态**:brainstorm 设计定稿,待 spec-kit 形式化 · **对标动机**:拉平 MemOS 88.83(当前 engram 提质路线 ~85.4%,gap ~3.4pp)· **特性号**:010

## 一句话论点

009 诊断证明拉分卡点是**深层召回**——真靶心题的 gold fact 在宽池里中位 rank 71–90、无一进 top-30(非 top-K 排序问题,US2 排序机制已 STOP)。但把 gold 捞进 top-30 有两条路:**加量**(撑 top-k / 扩池 —— cat-top-k 已证有效但拿 context 税换,maintainer 明确反感,见 杠杆哲学(`locomo-score-levers.md` 总账段))和**提质**(同 top-k=30 下让 gold 自己升上来)。本特性走提质:**多跳问题作为单一 query 嵌入得很差,把它分解成子查询各自精检、再由引擎 RRF-of-RRF 融合**,让同时命中多个子部分的 fact 被多张选票顶进 top-30 —— 不加 context、可移植、无付费杠杆。

## 背景与证据

- `Retriever.Search(ctx, query string, k int)`(`memory/retriever.go:121`)是**单 query、纯离线**入口:三信号 hybrid(semantic cosine + FTS5 BM25 keyword + entity 精确匹配)等权 RRF(k=60,tuning-free),缺信号静默降级。**检索路径无任何 query-时 LLM**(宪法 I 离线优先的核心)。
- `memory/queryplan.go` 只是 FTS5 匹配表达式合成(分词 / CJK trigram / LIKE fragment),**不是**语义 query 规划 —— 分解的家不在这,是全新能力。
- 009 深召回诊断:真 US2 靶心(答错且 gold 在池内)的 gold **中位 rank 71–90、70/156 在 100+**;outranked_by 信号弥散无单一机制主导 → 重排救不动,因 gold 根本没进候选前列。多跳 enumeration 题「需多 session 证据」是这批题的共性。
- 提质 vs 加量的判例:bge-large(同 top-k、向量更强)= 已转化召回赢 +1.3pp(符合哲学);cat-top-k(撑 top-k 到 150)= +0.9pp 但 context 2.4× 税,降级为 optional 非默认。本特性要做 bge-large 那类赢。

## 决策(brainstorm 已定,maintainer 拍板)

- **落点**:引擎可选 hook,**不**走 adapter 侧、**不**走非 LLM 结构化分解 —— 保证融合算法留在引擎(不被各 adapter 重造),同时 LLM 分解在调用方。
- **触发**:引擎**纯机制,永不自动烧 LLM** —— 拆不拆、花不花 LLM 全由调用方传入的子查询数组决定;`subqueries==[原query]` 即单查询 no-op。

## § 1 架构与边界(engine/adapter 分离,宪法 II)

| 层 | 改动面 | 契约 |
|---|---|---|
| **引擎**(`memory/retriever.go`)| 新增 `SearchMulti(ctx, subqueries []string, k int) ([]Result, error)` | 每个子查询跑现有 hybrid,RRF-of-RRF 融合,返回正常 top-k;`len==1` 逐字节等于 `Search` |
| **调用方**(adapter:`cmd/locomo-bench`,后续 MCP)| 用 LLM 把 query 分解成 subqueries 传入 | LLM 分解是调用方策略;引擎不碰 LLM |

- 引擎新增的是**融合机制**(不可被 adapter 重造的算法);LLM 分解是**调用方策略**(何时/如何拆)。这是 brainstorm 里 (a)(b) 两选项的合题。
- `Search` 保留为 `SearchMulti(ctx, []string{query}, k)` 的薄封装(或反之),离线默认路径**逐字节不变**。
- 契约冻结:`SearchMulti` 是新增公共 API(宪法 III contract-first),非破坏性,无 schema 变更。

## § 2 机制:RRF-of-RRF 融合

- 每个子查询 `q_i` 独立跑现有三信号 hybrid → 得到一个排序列表 `L_i`。
- 把每个 `L_i` 当作一个「投票者」,用引擎**已有的 RRF(k=60,免调参)**把 N 个列表合成一个:`score(d) = Σ_i 1/(60 + rank_i(d))`(未出现在 `L_i` 记 0)。
- 副作用正是多跳想要的:**同时命中多个子部分的 fact 拿多张选票 → 顶进 top-30**;只命中一个子部分的边缘 fact 不会挤爆。
- 复用现有 RRF 常数与实现,不引入新可调权重(守 tuning-free 可移植)。

## § 3 提质不加量的硬约束(本特性的存在理由)

- **最终返回严格 top-k=30**(喂答题的 context 预算不变)—— 这是与 cat-top-k 的分界线:我们改的是「哪 30 个」,不是「几个」。
- 子查询数 N 有上限(草案 ≤4),避免 N 路检索退化成隐性扩池。
- 端到端验证必须在**同 top-k=30、同 answer-context 量级**下比,answer_context_tokens 不得显著上升(涨了就是偷偷加量,判负)。

## § 4 门(Constitution IV + 无付费 rerank 死规则)

1. **纯 Go 契约门**:`SearchMulti` 单测 + parity(`len==1` 时逐字节等于现 `Search` 基线)+ `CGO_ENABLED=0` 构建通过。
2. **离线召回门**:LoCoMo multi-hop 上 canned 子查询复跑,断言**目标题 gold 从 rank>30 升进 top-30**、coverage@30 提升;**无云 reranker、无任何付费杠杆**。coverage 只作诊断不作 verdict(008 US4 铁证)。
3. **端到端决胜门**:同机配对 `Search` vs `SearchMulti`,唯一变量 = 分解;box vllm Qwen 栈 + deepseek mem0-aligned judge + canonical recipe,repeats=3;要求 **multi-hop above-noise 提升 + overall 及任一非目标类不显著回退 + answer_context 不显著上升(提质证明)**。越不过 → NO-GO 出货、保留为诊断能力(与 008 reranker / cluster-sweep 同样诚实处理)。

反证基线须超越:cat-top-k 是加量对照(+0.9pp 但带税);本特性要在**不加 context**下拿到可比或更好的 multi-hop 转化。

## § 5 验证形(TDD,先写失败测试)

- **契约/parity**:`SearchMulti(ctx, []string{q}, k)` 结果集与 `Search(ctx, q, k)` 逐字节相同(退化保真,先写会失败的断言)。
- **融合正确性**:小 fixture,构造 gold fact 在单查询下 rank>k;喂入手写子查询数组,断言 RRF-of-RRF 后 gold 进 top-k、且多子查询共同命中的 fact 排名高于单命中 fact(确定性 golden,无需真 LLM)。
- **离线召回 delta**:LoCoMo multi-hop 复跑,coverage@30 提升(诊断)。
- **端到端配对**:box 栈 McNemar,multi-hop above-noise + answer_context 不涨。

## 自由/成本门

- **契约 + 融合 + 离线召回门**:near-free,纯本地 / retrieval-only,不烧答题 token。
- **端到端决胜门**:需 box vllm Qwen 答题栈窗口(分解用同一 LLM,每题一次轻量 query 重写,远比 filter-pool 读 200 候选便宜)。

## 非目标(YAGNI)

- 不把 LLM 分解器塞进引擎(引擎永不碰 query-时 LLM);引擎只收 `[]string`。
- 不引入 LoCoMo 拟合的融合权重(破坏 tuning-free 可移植)。
- 不做 HyDE / 更好 fact 写入表示 —— 同属提质路线的**后续**独立增量,本 spec 只做 query 分解 + 融合这一枪。
- 不撑 top-k、不扩池、不用任何云 reranker(死规则 + 反加量哲学)。
- 不做答题侧改动。

## 关联

诊断正本 009 归因诊断(`specs/009-retrieval-attribution-gate/`) · 杠杆哲学(`locomo-score-levers.md` 总账段) · 台账 [`locomo-score-levers.md`](../../locomo-score-levers.md) · 复现 runbook [`locomo-e2e-eval-reproduction.md`](../../locomo-e2e-eval-reproduction.md) · 前作设计 [`2026-07-22-retrieval-ranking-attribution-gate-design.md`](./2026-07-22-retrieval-ranking-attribution-gate-design.md)。
