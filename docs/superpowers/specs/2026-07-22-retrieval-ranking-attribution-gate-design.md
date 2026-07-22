# 归因门控的检索排序(retrieval-ranking, evidence-gated)— 设计

**日期**:2026-07-22 · **状态**:brainstorm 设计定稿,待 speckit 形式化 · **对标动机**:拉平 MemOS 88.83(当前 engram 83.70%,gap ~5.1pp)

## 一句话论点

诊断证明"检索排序"是 single/multi-hop/temporal 三处最大错因(~45%),但**盲改排序 = 高翻转低净收益**(008 rerank 救 51 题却弄错 45 题,overall 反降),真正的卡点是**缺逐题检索归因**——A 和 B 两份诊断独立栽在同一堵墙。故第一枪不是直接改算法,而是**先建归因 trace(US1,免费/adapter),再用它门控一个定向排序改动(US2,gated/engine)**。

## 背景与证据

- 当前融合(`memory/retriever.go`)是**等权 RRF**:每路 `1/(60+rank)`,无权重、无分数幅度,tuning-free(k=60,design D4)。候选池由 BM25 keyword 界定(`pool = k*10, min 100`)。
- 竞争事实病灶(A 诊断实例):`kundalini yoga` 被 `aerial yoga` 压过、`MinaLima` 被 `LOTR map` 压过、adoption fact 被另一 summer plan 压过——**gold fact 在 store 内、在候选池内,但排名低于 top-k**;rerank 臂能救回 → 证明是排序问题,不是召回缺失。
- coverage≠answer(008 US4 铁证):turn@30 +15.457pp 召回,端到端答题 −0.06pp / p=1.0,temporal 净 −9。→ **coverage 只作诊断,不作 verdict**。
- 归因缺口:`.locomo-run/008-us4-e2e/results-hybrid.jsonl` **没存逐题 retrieved hits**;B 的 temporal 诊断 16/57 只能用"仅改排序后转正"的代理数。**没有逐题 trace,任何排序改动都只能盲评端到端翻转,看不到 gold turn 有没有进/出 top-k。**

## 决策(brainstorm 已定)

- **落点**:二合一——US1 归因 trace 先行,US2 用它门控定向排序(用户拍板)。
- **US2 机制**:留给 US1 证据挑,不现在钉死(用户拍板)——守住 RRF tuning-free 可移植原则,避免盲设机制。
- **US1 归因粒度**:**fact/chunk 级映射到 gold turn**,不用 turn@k(已知"turn@k 对 fact 级 assoc 失明"坑,见 memory coverage-diagnosis-2026-07-22)。

## § 1 架构与边界(engine/adapter 分离,宪法 II)

| | 层 | 改动面 | 风险 | 何时交付 |
|---|---|---|---|---|
| **US1** 归因 trace | 纯 adapter(`cmd/locomo-bench`) | 引擎零改,只调 `Retriever.Search` 记排名 | 无 Constitution IV 回归面 | 先交付,单独 commit |
| **US2** 定向排序 | engine 增量(`memory/retriever.go`) | contract-first,默认关,三道门 | 需端到端决胜门 | US1 证据后,gated,提交分离 |

`git diff --name-only -- memory embedding provider store internal` 在 US1 阶段必须为空。

## § 2 US1 — 逐题检索归因 trace(FREE gate,MVP)

对每题 retrieval-only 复跑(复用 008 固化 store,**不调答题模型**),持久化一条 trace:

- `gold_evidence`:该题 gold 证据的 session:turn 集合(从 LoCoMo dataset 解析)。
- `retrieved`:top-N 命中 `{name, rank, rrf_score, per-signal rank(sem/kw/entity), 映射到的 gold turn?}`。
- **fact 级归因判定**:`gold_in_pool?`(gold 证据对应 fact 是否进候选池)、`gold_rank`(是否进 top-k、第几名)、`outranked_by`(排在 gold 之上的竞争 fact)。
- **四象限**(join 现有 `results-hybrid.jsonl` 对错):
  1. gold 在 top-k 且答对 —— 正常;
  2. gold 在 top-k 但答错 —— **答题侧**(槽位/主语/时间限定,A 诊断 b 类);
  3. gold 在池内但排名靠后 —— **US2 靶心**;
  4. gold 未进池 —— 抽取/召回侧(图像 caption 缺失等,A 诊断 a 类)。
- **embedding 查询确定性探针**:同一 query 连嵌两次,断言向量 bit-identical(或有界 δ)。诊断所说"运行退化"若真会稳定复现——纯 Go 可验的噪声源,先钉住。

**产出**:`trace.jsonl` + 分布表,把 A 的"37 排序题"精确定位到"gold 在池内第几名、被谁压",把 coverage≠answer 从谜团变逐题证据。

## § 3 US2 — 定向排序改动(GATED,机制由 US1 证据挑)

看第三象限竞争事实分类后,从纯 Go、tuning-free 候选里挑**一个**:
- (a) **score-aware RRF**:融合带回 cosine/BM25 幅度边距,让近似竞争事实可区分;
- (b) **近重去重/MMR**:候选池内近似 fact 去重,防副本挤占 top-k;
- (c) **实体/时间锚约束**:题里有 anchor 时约束候选(与 T-3/T-4 正交)。

默认关:`RetrieverOptions` 加新 bool,零值 = 旧行为逐字节不变。

## § 4 三道门(Constitution IV + 无付费 rerank 死规则)

1. **纯 Go 契约门**:新排序逻辑单测 + parity(默认关时逐字节等于现基线)+ `CGO_ENABLED=0`。
2. **离线归因门**:US1 trace 复跑,断言目标象限 gold 排名上升;**无云 reranker、无任何付费杠杆**。
3. **端到端决胜门**:同机配对 hybrid vs hybrid+US2,唯一变量 = 排序机制;先目标类再全量 1540,要求 **McNemar above-noise + overall 及任一非目标类不显著回退**。coverage 只作诊断。**越不过 → NO-GO 出货、保留为诊断能力**(与 US1 reranker 同样诚实处理)。

反证基线须超越:legacy temporal Δ−0.3pp/p=1.000(003)、US1 reranker 端到端 −0.06pp/p=1.0(008)。

## § 5 验证形(TDD,先写失败测试)

- **US1**:小 fixture 上断言 trace 正确报出 `gold_rank` + `outranked_by`(确定性 golden);embedding 探针断言二次嵌入一致。
- **US2**:parity 测试(关时零变)→ 归因 delta 测试(目标象限改善)→ 端到端配对脚本(McNemar)。

## 自由/成本门

- **US1**:near-free。retrieval-only + 复用已有答题结果 join,不烧答题 token,当前机器窗口内即可跑。
- **US2**:决胜门需答题机器窗口(远端 vllm Qwen 栈)。

## 非目标(YAGNI)

- 不引入 LoCoMo 拟合的 per-signal 权重(破坏 tuning-free 可移植性)。
- 不用任何云 reranker/recall 模型作杠杆(死规则)。
- 不做答题侧改动(槽位约束、IDK retry)——那是另一条线(A 诊断 b/d 类),本 spec 不碰。
- 不做抽取侧图像 caption 入库——独立高确定性簇,另立项。

## 关联

诊断正本 [`locomo-single-multihop-failure-diagnosis.md`](../../locomo-single-multihop-failure-diagnosis.md)、[`temporal-t4-design.md`](../../temporal-t4-design.md)、杠杆台账 [`locomo-score-levers.md`](../../locomo-score-levers.md)、北极星 [`capability-and-product-north-star.md`](../../capability-and-product-north-star.md)。
