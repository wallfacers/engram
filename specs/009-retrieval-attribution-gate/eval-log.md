# 009 Retrieval Attribution Gate - Evaluation Log

## 2026-07-23 - T013 through T016

### US1 full attribution

Configuration: frozen 008 large-embedding stores, hybrid retrieval, top-k 30, chunks enabled, archived 008 correctness join, and local 1024-dimensional embedding endpoint. No answer or judge model was configured or run.

| Check | Result | Evidence |
|---|---|---|
| SC-001 coverage | PASS | 1,540 traces = 1,532 gradeable + 8 `gold_unresolved`; unresolved records are excluded from each category denominator. |
| SC-002 attributable competition | **FAIL** | Wrong-answer Q3 has 233 records, but `outranked_by` is empty for 233/233. The required >=90% competition evidence coverage is 0%. |
| SC-003 zero answer cost | PASS | Both logs report `answer_calls=0` and `judge_calls=0`. |
| SC-004 determinism | PASS | Both traces and both quadrant distributions are byte-identical; trace SHA-256 is `a6f55503f12b87bbf3e87047dea1b7de9e16713be46e05041c1e7fd82d1a707d`. |
| SC-005 engine zero-change | PASS | `git diff --name-only -- memory embedding provider store internal` is empty. |

Quadrant totals are mutually exclusive and exhaustive: Q1 52, Q2 14, Q3 1,458, Q4 8, plus 8 unresolved outside the 1,532 denominator. The two embedding probes reported `unstable`, but this did not change trace bytes between runs.

### T016 evidence gate

**Verdict: STOP, evidence does not support mechanism selection.**

The Q3 sample is not too small: 233 Q3 records also have `correct=false` (193 in the feature target classes after excluding open-domain). The blocker is evidence quality:

1. No wrong-answer Q3 record serializes an `outranked_by` competitor, and no wide-pool `gold_rank` is available.
2. Extracted facts are not mapped to exact gold turns. For the specified `conv=2,q=111` target, the answer-bearing Kundalini fact is rank 4 but marked non-gold, so the Q3 label does not prove that the answer fact was pushed out of top-k.
3. A proxy analysis of top-5 retrieved names shows overlapping signals: 85/233 explicit time anchors and 55/233 near-duplicate pairs at token Jaccard >=0.50. This does not isolate one of the three candidate mechanisms.

Selected mechanism: **none**. Expected quadrant improvement: **not applicable until SC-002 produces decision-grade competitor evidence**.

Hard boundaries observed: no cloud/paid reranker, no `memory/` or engine changes, no end-to-end answer run, and no answer-token cost. T017 and later gates remain unstarted pending maintainer direction.

---

## 2026-07-23 — 归因 adapter 修复后重跑(fix 7c8f194)

首版归因两根因(A: outranked_by 结构性恒空;B: covers_gold 对 fact 命中失明)已修。同配置 hybrid 全量重跑(box bge-large 1024d 经隧道,retrieval-only,`answer_calls=0`)。

### 象限修正(hybrid,1540)

| 象限 | 首版(bug) | 修复后(τ=0.8) |
|---|---|---|
| q1_ok | 52 (3.4%) | **536 (34.8%)** |
| q2_answer_side | 14 | 91 (5.9%) |
| q3_us2_target | **1458 (94.7% 伪影)** | **899 (58.4%)** |
| q4_extraction_side | 8 | 6 |
| gold_unresolved | 8 | 8 |

- **SC-002 PASS**:真靶心(Q3&答错)156 条,`outranked_by` 非空 **156/156 = 100%**(≥90%)。根因 A 修复。
- **fact 路径生效**:top-K 覆盖里 fact=575 / chunk=52。conv2/q111 的 kundalini fact(rank 4)现 `covers_gold=true` → 正确归 Q2(不再 Q3 伪判)。
- **SC-004**:象限分布两跑**完全相同**、**0/1540 题换象限**(结论可复现);但 byte 级 FAIL —— 差异全在 `rrf_score` 第 5 位小数,源自 **vllm-GPU 查询嵌入非确定性**(`embed_probe` verdict `unstable`,bit_identical 0.875),非覆盖逻辑(覆盖纯词法确定)。要真 byte 一致须换确定性 CPU 嵌入器(fastembed)。

### τ 敏感性(0.8 vs 0.6)

| | Q1 | Q3 占比 | 真靶心 | gold 中位深度 | ≤30 |
|---|---|---|---|---|---|
| τ=0.8 | 536 | 58.4% | 156 | 90 | 0 |
| τ=0.6 | 867 | 33.2% | 100 | 71 | 0 |

- τ=0.8 **偏严**:放松到 0.6,331 题 Q3→Q1(gold 其实在 top-K,被抽取改写得 exact-match 漏判)。词法匹配受抽取 paraphrase 限制,已知软限制。
- **但无论 τ,真靶心 gold 都排得很深(中位 71–90,ZERO ≤30)**。

### US2 证据门 verdict:**STOP(基于可信证据)**

不同于首版「证据坏了」,修复后证据可信,却指向**另一个诚实结论**:真 US2 靶心的 gold 在宽池里排得极深(中位 71–90,无一 ≤30,70 条 100+),outranked_by 信号弥散(时间锚 19%、近重 5%,无单一机制主导)。这是**深层召回/检索缺,不是 top-K 排序问题**——tuning-free 重排(score-aware RRF / MMR / 实体·时间锚)**救不动 rank-90 的 gold**。与既往「reranker coverage 涨但端到端 NO-GO」「短板在检索覆盖非答题深度」一致。

**不选机制、不启动 US2 引擎改动、不跑端到端答题。** 若未来要拿这类题,方向是**召回/检索深度**(更好的 embedder / 混合信号召回 / chunk 粒度),非排序精修。
