# LoCoMo 跑分杠杆台账

本文件是 LoCoMo 检索/答题**杠杆实验的持久正本**(tracked,跨环境不失传)——记录每条杠杆的 verdict、真实数字、口径边界。本地 session memory 只做快速召回,结论以此为准。逐条细节见对应 `specs/NNN-*/eval-log.md`。

**通用口径**:所有 coverage 数为 `--coverage-only` 的 exact-turn recall(turn@k),零 answer/judge 调用(免费);分母 = 有可解析 gold turn evidence 的题(locomo10 全量为 1532)。coverage 增益是端到端答题增益的**必要非充分**条件;声明答题分需另跑端到端。所有采纳杠杆按宪法 V **默认关 / opt-in**。

---

## Feature 008 — score levers(2026-07-22)

固定栈:答题/抽取 = 本地 vllm `Qwen/Qwen3.6-35B-A3B-FP8`;embedding = 本地 fastembed;judge = `deepseek-v4-flash`(mem0-aligned)。所有 sidecar 纯本地、无云依赖。引擎零改(`git diff -- memory embedding provider store internal` 全空)。

| 杠杆 | 层 | 免费 coverage 闸 | **端到端答题(决胜)** |
|---|---|---|---|
| **US1 本地 reranker** `bge-reranker-v2-m3` | retrieval | ✅ +15.457pp turn@k | ❌ **NO-GO −0.06pp(p=1.0)**——coverage 幻觉 |
| **US3 大 embedder** `bge-large-en-v1.5` 1024d | embedding | ✅ +3.793pp turn@k | 未端到端验(候选/备胎;coverage 幻觉风险同 US1) |
| **US2 open-domain 五步提示** | answer | — | ❌ NO-GO −2.1pp(cat-3, p=0.774) |

> **008 决定性教训(US4)**:coverage/turn@k 增益**不等于**答题增益。US1 reranker 拿 +15.457pp 召回,端到端答题 **−0.06pp(McNemar p=1.0, within-noise)**——它 helps 3 类 +8 但把 **temporal 砸 −9**(cross-encoder 按单轮相关性重排,挤掉时序上下文)。**以后杠杆一律以端到端答题分为准,coverage 只作诊断,不作 verdict。**

### ⭐ 新诚实参考点(US4,无 reranker)

engram 端到端 **overall 83.70%**(mem0-aligned judge, 本地 Qwen3.6-35B 栈, top-k30, 全量 1540)。取代旧 luna/strict-judge 50.7% 伪影。

| 类别 | 正确率 | n | 差距诊断 |
|---|---:|---:|---|
| single-hop | 86.68% | 841 | 已接近 MemOS 级;大 n 有杠杆 |
| multi-hop | 85.82% | 282 | 已接近 MemOS 级 |
| **temporal** | **82.24%** | 321 | 次弱,脆(reranker 会害);时序推理 |
| **open-domain** | **56.25%** | 96 | **最弱**,coverage 加满也不动(54→56)→答题/推理/判题问题,非检索 |

**vs 目标**:MemOS 88.83(gap ~5.1pp)/ Mem0 92.5(gap ~8.8pp)。**拉平方向 = open-domain + temporal + single-hop 精度,不是堆检索召回。**

> ⚠️ **口径注**:83.70% 是 `force_answer=false`(**允许拒答**)下拿的,比 Mem0/OmniMemEval 的**强制作答无 IDK**口径更严。对标竞品的可比数字见下 force-answer 行。

### force-answer 口径对齐 A/B(2026-07-22)

`--force-answer` on vs off(off 臂=上表 83.70% hybrid;单变量=是否允许拒答 + force* prompt)。全量 1540,mem0-aligned judge,引擎零改。

| 类别 | off(拒答) | on(force) | 净(题) |
|---|---:|---:|---:|
| temporal | 82.24% | 83.80% | **+5** |
| multi-hop | 85.82% | 86.88% | **+3** |
| single-hop | 86.68% | 86.92% | +2 |
| **open-domain** | 56.25% | 54.17% | **−2** |
| **OVERALL** | 83.70% | **84.22%** | **+8(+0.52pp)** |

- **verdict:边际正 / 口径对齐**。+0.52pp(144 flips 净 +8),**大概率在单跑噪声带内**(答题非确定性 temp=1.0)+ force flag 混淆(同时换 prompt)。**非算法涨点,是向竞品口径靠拢**——84.22% 是对标 MemOS/Mem0 的**可比数**(gap MemOS ~4.6pp)。
- **机制**:收益全来自 temporal/multi-hop 的**事实题**强制猜回;**open-domain 反 −2**(强制猜 opinion 题比拒答更差,IDK 15→0 但净负)。**⇒ open-domain 56% 不是弃答问题,是真推理/口径难度**;open-domain 杠杆改走 OD-2(多候选输出)/ OD-3(抽取软线索),**force-answer 救 open-domain(OD-1)已死**。

### US1 — 本地 reranker(旗舰,决胜杠杆)

- overall turn@30 **77.012% → 92.468% = +15.457pp**(超 +4pp 闸 ≈3.9×);每类 +14~20.7pp(multi-hop +20.743、temporal +14.564、open-domain +14.896、single-hop +14.090);session recall +2.378pp。
- 本地双端点 sidecar(bge-small 384d embed + bge-reranker-v2-m3 cross-encoder),源码零外网调用、本地文件加载;自检 `models=2`、forbidden-list 拦截云型号。batch=50 median 122ms。
- **意义**:把 007 复盘中被死规则判负的**付费** gte-rerank 赢面(曾 +8.3pp)转成**纯本地、可移植、零云**的合法赢,幅度翻倍。这是 pure-Go/offline 可复现的正当拿分路径。
- **限定**:coverage 增益,非答题正确率;端到端声明待 US4 授权。默认关。

### US3 — 大 embedder(候选/备胎,可与 US1 叠)

- overall turn@30 **77.012% → 80.804% = +3.793pp**;open-domain +5.441 最亮、multi-hop +4.778。
- **代价**:大向量纯 Go `TopKCosine` 扫描 **2.37×**(399µs vs 168µs);换维度必**整店重建**。
- **诚实边界**:large 重建 2832 facts vs small 2688(chunks 同 1056),抽取重跑过 → +3.79pp **含重抽取方差,非纯 embedder bit-identical 因果隔离**;冻结重建契约下过闸。

### US2 — open-domain 五步推理提示(证伪)

- cat-3 单变量 A/B:旧 63.5% → 新 61.5% = **−2.1pp**;McNemar b=7/c=5/**p=0.774**(不显著);其余三类由选择路径不变无回归。
- **结论**:归纳/CoT 提示对 open-domain 无正收益,反略降。**短板在检索覆盖,不在答题推理深度**(US1/US3 的 open-domain 大幅正增益反证此点)。commits 61b5311(mechanism)+ 5e172c9(eval)。

### 口径 gotcha(影响所有配对推理)

- **答题非确定性**:`newUsageModelCallerWithUsage`(runner.go)不设 `Temperature`,零值被 `omitempty` 省略 → 远端 vllm 默认 `temperature=1.0, do_sample=true`(**不是** temp=0)。配对 McNemar 只是共享采样配置下的**单次配对观测**,不可宣称确定性差分。

### 下一步(需授权)

- **US4**:US1 已过闸 → 端到端默认预算(top-k 30)重跑 `hybrid` vs `hybrid+rerank`,声明新参考点 + false→true flip 抽查;reranker 保持默认关。US1+US3 联合收益尚未验证。
- 细节正本:[`specs/008-locomo-score-levers/eval-log.md`](../specs/008-locomo-score-levers/eval-log.md);对标目标见 [`competitive-benchmarks.md`](./competitive-benchmarks.md)。

---

## Feature 009 — 归因门控(attribution,2026-07-23)

固定栈同 008(embedding 换 box vllm bge-large 1024d)。引擎零改(全在 `cmd/locomo-bench/` adapter)。逐条正本:[`specs/009-retrieval-attribution-gate/eval-log.md`](../specs/009-retrieval-attribution-gate/eval-log.md)。

**US1 逐题检索归因 trace**:retrieval-only 把错题切四象限(Q1 对 / Q2 答题侧 / Q3 排序靶心 / Q4 抽取侧),零答题成本。首版有两 adapter bug(`outranked_by` 结构性恒空;`covers_gold` 对 fact 命中失明→Q3=94.7% 伪影),修复后(fact 感知覆盖 + wide-pool outranked,commit 7c8f194)Q3→58.4%、Q1→34.8%、SC-002 outranked 非空 100%。fact↔turn 用词法内容匹配桥接(fact 只有 session 级溯源),`--fact-coverage-tau` 默认 0.8(偏严,已知软限制)。

### ⭐ 决定性诊断:瓶颈是**深层召回**,不是 top-K 排序

修复后可信证据显示:真 US2 靶心(Q3 且答错)的 gold 在宽池里**中位 rank 71–90、无一 ≤30**、70/156 在 100+;outranked_by 信号弥散(时间锚 19% / 近重 5%,**无单一机制主导**)。

- **US2 排序机制 = STOP(NO-GO)**:tuning-free 重排(score-aware RRF / MMR / 实体·时间锚)**救不动 rank-90 的 gold**——它们重排 top 候选,而 gold 根本没进候选前列。这不是排序问题。
- 与 008「reranker coverage +15pp 但端到端 NO-GO」、[US2 open-domain 提示证伪]「短板在检索覆盖非答题深度」**三处独立印证同一结论**。
- **后续拿分方向 = 召回/检索深度**,非排序精修:更强 embedder、混合信号召回扩召、chunk 粒度/抽取覆盖。**不是** cross-encoder 重排(死规则:云/付费 rerank 禁用;本地 reranker 已证 coverage 幻觉)。

### 口径 gotcha(归因专有)

- **SC-004 确定性依赖嵌入后端**:vllm-GPU 查询嵌入非确定(`embed_probe` unstable,bit_identical 0.875)→ trace byte 级不可复现(差异在 rrf_score 尾数);但**象限分布两跑完全相同、0/1540 换象限**,结论可复现。要 byte 一致须用确定性 CPU 嵌入器(fastembed)。覆盖判定本身纯词法确定。

### ⭐ bge-large 端到端 = GO 候选(008 US3 coverage 赢**已转化**为答题分,2026-07-23)

009 诊断说「后续方向 = 更强 embedder」。这轮把 008 US3 的 bge-large 从 coverage 候选**推到端到端答题验证**——可复现流程/踩坑正本见 [`locomo-e2e-eval-reproduction.md`](./locomo-e2e-eval-reproduction.md)。

栈:box vllm Qwen 答题 + deepseek-v4-flash **mem0-aligned** judge + `--chunks --top-k 30 --chunk-quota 12 --force-answer`。

| store / embedder | overall | Δ |
|---|---|---|
| bge-small @ `007-us2/cov-store`(控制/自检) | **84.03%**(1294/1540) | 复现记录基线 84.22%(±0.2pp)→ 管线验证 ✓ |
| **bge-large @ 全新 q12 店** | **85.45%**(1316/1540) | **+1.42pp / 净 +22 题** |

分类:single-hop +1.7 · multi-hop +2.1 · temporal −0.6 · **open-domain +4.2**(3/4 类涨,含最难的两类)。

- **与 008 reranker 决定性不同**:reranker coverage +15pp 但端到端 NO-GO(幻觉);bge-large coverage +3.79pp **真转化**成答题 +1.42pp。**这是首个已转化的召回赢**。
- **可移植/合规**:bge-large 开源权重、离线可跑(fastembed CPU / vllm),**非付费云 rerank**,不碰死规则;符合 Constitution I/V。可作默认 embedder 升级路径候选。
- **口径**:85.45% 是可比数(force-answer + mem0-aligned,同竞品口径)→ 对 MemOS 88.83 的 gap 从 ~4.6pp 收窄到 **~3.4pp**。
- **诚实 caveat(未过硬因果闸)**:(1) bge-large 店是**今天全新抽取**、bge-small 是旧 cov-store → +1.42pp **混了重抽取方差**,非 bit-identical 纯 embedder 隔离,真实纯因果可能 <1.42pp;(2) **单跑 temp=1.0 非确定**,+22 题含噪声(控制组只偏基线 0.2pp,噪声不大但仍需 repeats 坐实)。**出货前须**:同抽取 bit-identical 对照 + repeats≥3。
- **本轮元教训**:先前 59%→70% 全是**漏 `--judge-mem0-aligned`**(+ chunk-quota 0)的配置伪影,与 embedder 无关;控制自检(cov-store 复现 84%)是把伪影和真信号分开的唯一手段。踩坑全表见 reproduction runbook。
