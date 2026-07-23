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
