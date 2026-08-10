# 033 Paid Probe Verdict — NO-GO

**Date**: 2026-08-10  
**Valid scratch**: `/home/wushengzhou/.claude/session-scratch/033-probe-run.ew8rCD`  
**Decision**: **NO-GO; full-set A/C is forbidden.**

Post-verdict root-cause analysis corrected the earlier `gold_in_pool` interpretation and identified incomplete
multi-turn coverage, caption payload omission, and event/set aggregation as the actual residuals. See
[033 failure analysis](../../../docs/evaluation/reports/033-chunk-first-failure-analysis.md).

## Primary gate

The 3-repeat majority probe completed with exact coverage: A 192/192 repetitions, B 54/54, C 192/192;
all processes exited 0. These are intentionally selected residual/guard cohorts, not an absolute LoCoMo score.

| Cohort | A majority correct | C majority correct | A-only | C-only | C−A net | exact McNemar p |
|---|---:|---:|---:|---:|---:|---:|
| target-32 | 4/32 | 5/32 | 1 | 2 | **+1** | 1.0 |
| guard-32 | 32/32 | 32/32 | 0 | 0 | 0 | 1.0 |
| probe-64 | 36/64 | 37/64 | 1 | 2 | +1 | 1.0 |

The pre-registered gate required target net ≥+8 and guard net loss ≤1. Guard passed, but target missed by seven;
therefore the treatment is not promoted. Target C-only IDs were `conv-7-q-123` and `conv-8-q-73`; A-only was
`conv-9-q-60`.

Per-repeat probe totals were A `37/64, 37/64, 35/64`, C `38/64, 37/64, 36/64`; B was `9/18` in all three
repeats. The majority decision above, not the average of these repeat totals, is the registered gate.

B legacy versus C treatment on the isolated 18 multi-hop questions was exactly unchanged: both correct 9,
both wrong 9, zero flips, p=1.0. The repaired multi-hop order therefore contributed no measured answer gain.

## Backstop questions

The literal `chunk-gold && gold_rank_topk >= 19` cohort contains 16 questions (not 14): A and C were both
correct on 1, both wrong on 15, with zero flips. The remaining 48 questions supplied all observed movement
(A-only 1, C-only 2, net +1). Thus the effect did not concentrate in the predicted tail-rank stratum.

Across all 19 chunk-gold questions × 3 repeats:

- C admitted all 30/30 candidates in every row;
- the frozen gold chunk was admitted in 57/57 rows;
- cap truncation occurred in 0/57 rows, and gold-excluded-by-cap in 0/57;
- assembly totals were estimate-ledger values under the 3600 cap (`tokens_estimated=true` in 57/57), while A/C
  provider contexts below are actual OpenAI-compatible usage counts.

This is a **focus reorder**, not a cap rescue. It cannot plausibly supply the ≥8 target gain needed to justify a
full run.

| Question | Gold rank | A provider context r1/r2/r3 | C provider context r1/r2/r3 | C assembly estimate / 3600 | Admitted/input each rep | Gold admitted | Truncated |
|---|---:|---|---|---|---|---|---|
| `conv-0-q-57` | 20 | 3226/3226/3226 | 3226/3226/3226 | 2860/2860/2860 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-0-q-133` | 19 | 3009/3009/3009 | 3009/3009/3009 | 2666/2666/2666 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-0-q-140` | 2 | 3112/3112/3112 | 3112/3112/3112 | 2836/2836/2836 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-1-q-75` | 19 | 3123/3123/3123 | 3123/3123/3123 | 2529/2529/2529 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-2-q-62` | 19 | 3444/3444/3444 | 3471/3471/3471 | 2934/2934/2934 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-3-q-53` | 19 | 3434/3434/3434 | 3480/3480/3480 | 2797/2797/2797 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-3-q-62` | 19 | 3416/3416/3416 | 3457/3457/3457 | 2901/2901/2901 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-3-q-89` | 1 | 3278/3278/3278 | 3278/3278/3278 | 2784/2784/2784 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-4-q-49` | 19 | 3313/3313/3313 | 3353/3353/3353 | 2906/2906/2906 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-5-q-17` | 19 | 3526/3526/3526 | 3559/3559/3559 | 2892/2892/2892 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-5-q-64` | 23 | 3248/3248/3248 | 3248/3248/3248 | 2846/2846/2846 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-6-q-147` | 19 | 3344/3344/3344 | 3344/3344/3344 | 2963/2963/2963 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-7-q-171` | 23 | 3389/3389/9698 | 3389/3389/3389 | 2865/2865/2865 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-8-q-80` | 19 | 3150/3150/3150 | 3150/3150/3150 | 2670/2670/2670 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-8-q-104` | 19 | 3555/3555/3555 | 3555/3555/3555 | 3063/3063/3063 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-8-q-108` | 20 | 3288/3288/3288 | 3288/3288/3288 | 2910/2910/2910 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-8-q-116` | 22 | 3240/3240/3240 | 3240/3240/3240 | 2820/2820/2820 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-8-q-145` | 25 | 3508/3508/3508 | 3508/3508/3508 | 3009/3009/3009 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |
| `conv-9-q-62` | 9 | 3289/3289/3289 | 3289/3289/3289 | 2904/2904/2904 | 30/30 · 30/30 · 30/30 | yes/yes/yes | no/no/no |

The one A context of 9698 reflects the enabled legacy IDK retry path; C had no retry for that repetition. It does
not indicate an assembly cap breach.

## Category and safety gates

| Category | Questions | A-only | C-only | Net | Holm-adjusted p | Significant negative? |
|---|---:|---:|---:|---:|---:|---|
| multi-hop | 18 | 1 | 0 | −1 | 1.0 | no |
| single-hop | 32 | 0 | 1 | +1 | 1.0 | no |
| temporal | 14 | 0 | 1 | +1 | 1.0 | no |

There is no statistically credible category regression, but absence of harm does not compensate for failing the
target uplift gate.

## Protocol, calls and cost receipt

- Answerer: OpenAI-compatible `deepseek-v4-pro`, thinking enabled (`LOCOMO_NO_THINKING=0`).
- Judge: Anthropic-compatible `deepseek-v4-flash`, mem0-aligned.
- Aggregate concurrency 32: A=11, C=11, B=10. Each arm used a manifest-identical private store copy.
- Hybrid, top-k 30, chunk quota 12, chunks enabled, no hosted reranker/recall, no trace mediation.
- Extract calls were 0 in every arm.

| Arm | Answer calls | Answer in/out tokens | Judge calls | Judge in/out tokens | Rewrite calls | Mean provider context |
|---|---:|---:|---:|---:|---:|---:|
| A | 195 | 648,848 / 147,565 | 192 | 9,524 / 20,538 | 2 | 3327.43 |
| B | 54 | 184,068 / 55,228 | 54 | 2,476 / 4,009 | 0 | 3408.67 |
| C | 192 | 633,426 / 139,505 | 192 | 9,553 / 17,523 | 0 | 3299.09 |

The harness price table does not contain these model IDs, so `actual_usd=0` is an unpriced-model artifact and is
not reported as free. Provider usage above is authoritative; final currency cost must come from the provider bill.

## Artifacts and disposition

- Analyzer summary SHA-256: `e3b974679e0b0c7dfd598f4ee8c1a9075335123eb330d047d6f1690d60f35492`.
- Cost SHA-256 A/B/C: `6c9fbfd225006730dc1fdbbcbd7bdd8cdac760a2df815f5cfd8ed30cb1a01616`,
  `c3648afbbba609fb050d890a561f0f73bc44645be77239b7a18e0527a10c3046`,
  `800153da1d85b3378f01031e7f35e5b35e40778e9fc7ea87ee67dbeedbb83e16`.
- Earlier auth/concurrency-invalid attempts remain excluded and are documented in `failed-probe-attempt.md`.
- T025/T026 are not applicable under the pre-registered stop rule. No full-set calls were made and no >90 claim
  is permitted for this mechanism.
