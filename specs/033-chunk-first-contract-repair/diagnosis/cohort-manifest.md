# 64-question Probe Cohort Manifest

**Frozen**: 2026-08-10, before any 033 answer/judge call

## Target selection

`target-32.txt` was selected from the July deepseek-v4-pro three-run majority errors after retrieval-only
attribution. It covers direct evidence/caption selection, aggregation/near-duplicate resolution, temporal
calculation and four known noise sentinels. The selection is used only to allocate evaluation budget; runtime
ordering never reads membership, gold answers, correctness labels or attribution ranks.

## Guard matching

`guard-32.txt` contains questions correct in all three archived runs with gold present in top-30. Each line is
paired to the same line of `target-32.txt`, prioritizing exact matches on category, first gold-hit kind, rank band
and complete/partial gold-turn coverage, then conversation and nearest context/rank profile.

Both cohorts contain 9 multi-hop, 7 temporal and 16 single-hop questions. Two pairs lack an exact rank-band
match but preserve category/kind/coverage:

- `conv-4-q-55` → `conv-8-q-76` (temporal/fact/partial; rank 3→18)
- `conv-9-q-62` → `conv-8-q-40` (temporal/chunk/complete; rank 9→3)

## Gate

- Main GO: repaired assembly vs no-assembly baseline rescues at least 8/32 target questions net.
- Guard: repaired assembly loses at most 1/32 guard question net.
- Isolated attribution: legacy assembly vs repaired assembly is reported separately and never substituted for the
  main gate.
- Failure of either main threshold stops full-set evaluation.

## Integrity

- `probe-64.txt` is the exact concatenation of target then guard.
- All three files must have unique IDs and counts 32/32/64.
- Frozen SHA-256 values:
  - `target-32.txt`: `2f0ed8586c8648b1fcfecc95db512fdfcd0e1e77813bc2d83ed599ace7531f4b`
  - `guard-32.txt`: `864bdff5115c0bd93a135cf8ae0d8e7490ac42776abf3e8ba500d0197119b581`
  - `probe-64.txt`: `3ac0efc5ccbaa2e677eee3b97c1f0cc5bb11f59f8af30d82c297c0fc36237eba`

## Validation receipt

Validated on 2026-08-10 before implementation:

- line counts: `32 / 32 / 64`;
- unique counts: `32 / 32 / 64`;
- all SHA-256 values exactly match the frozen values above;
- `probe-64.txt` is byte-for-byte target followed by guard (`cmp` exit 0).
