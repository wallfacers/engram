# 033 Retrieval-Only Offline Verdict

**Date**: 2026-08-10  
**Verdict**: MECHANISM GO; no answer-score claim.

## Frozen run

- Dataset/store/cohort: preflight receipts, unchanged.
- Retrieval: hybrid, top-k 30, chunk quota 12, persisted
  `009-bge-chunks-store`, local `BAAI/bge-large-en-v1.5` endpoint.
- Treatment: `entity_order=kind_layered`.
- Legacy: `entity_order=legacy_grouped`.
- Questions: 64 = 18 multi-hop + 14 temporal + 32 single-hop.
- Answer/judge/extraction calls: **0**. Each run directory contains only
  `assembly-diagnose.jsonl`; the retrieval-only dispatch constructs no
  answerer or judge caller.
- Exact tokenizer: unavailable in this retrieval-only environment, honestly
  recorded as `tokens_estimated=true` for 64/64 in both arms. Every record
  admitted all 30 input candidates, so no cap truncation occurred.

## Contract results

| Metric | Legacy | Treatment | Gate |
|---|---:|---:|---|
| Records | 64 | 64 | PASS |
| Input closure equal, paired | \- | 64/64 | PASS |
| Admitted ID/text multiset equal | \- | 64/64 | PASS |
| Non-multi assembly records byte-equivalent | \- | 46/46 | PASS |
| Prompt evidence order matches `Units` | 64/64 | 64/64 | PASS |
| Chunk-before-fact | 46/64 | 64/64 | PASS |
| Correct multi-hop mode receipt | 18/18 | 18/18 | PASS |

The 18 legacy failures are exactly the 18 multi-hop questions. Treatment
therefore repairs the known contract without changing retrieval closure or any
non-multi record.

## No-gold rank-band movement

Chunk positions over the 18 multi-hop records (216 chunks total):

| Rank band | Legacy chunks | Treatment chunks |
|---|---:|---:|
| 1–5 | 77 | 90 |
| 6–10 | 30 | 90 |
| 11–15 | 2 | 36 |
| 16–20 | 0 | 0 |
| 21–30 | 107 | 0 |

The first fact rank moved from legacy mean `6.278` (median `6`, range `1–12`)
to treatment rank `13` for all 18 questions. This is a structural visibility
measurement only; it does not inspect gold answer/evidence or correctness.

## Artifact receipts

- Legacy journal SHA-256:
  `6f463d31cbce905f9e6b7ce253745b893c42f9176772310137ea2a4bea9ee943`
- Treatment journal SHA-256:
  `d07689d0978c8e7421f6c713de66e4ada452d4a818e0a83cf213c3cd9c1f084f`
- Analyzer summary SHA-256:
  `7c44be2b9a06f593aafe2fe3df5a3ea7d8f81eef11fffc976aaae86e7926eadc`
- Scratch root:
  `/home/wushengzhou/.claude/session-scratch/033-diag.jRzLSA/`

`offline_order_analyze.py` returned `valid=true` and exit `0`. This permits
the paid probe tooling phase; it does not itself permit a >90 claim.
