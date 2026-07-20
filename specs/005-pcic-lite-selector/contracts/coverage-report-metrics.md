# Contract: Coverage Report Metric Extensions

**Surface**: `cmd/locomo-bench` coverage-only mode output (`coverage.json` + printed matrix).

## Frozen contract

- The per-arm coverage record gains three fields alongside the existing
  `turn_recall` / `session_recall`:
  ```json
  { "arm": "...", "top_k": 30, "overall": { "n","turn_recall","session_recall",
    "selection_survival","complement_drop","anchor_violation" }, "by_category": { ... } }
  ```
- Definitions are fixed (research R5):
  - `selection_survival` — selected_gold_turns / candidate_gold_turns.
  - `complement_drop` — fraction of gradeable questions whose selected gold-turn coverage is
    strictly below their candidate-window gold-turn coverage.
  - `anchor_violation` — count of questions missing a top-2 pointwise-rerank anchor chunk.
- For arms without a selector (`hybrid`, `hybrid+rerank`), the three fields are still emitted:
  `selection_survival` = 1.0 by construction (no post-rerank selection drops in-window gold
  beyond the quota cut — measured against the same candidate window for comparability),
  `complement_drop` measured identically, `anchor_violation` = 0. This keeps every arm on one
  comparable schema.
- The three arms of the MVP bake-off are reported together: `hybrid+rerank` (baseline),
  `hybrid+rerank+pcic` (selector), `hybrid+rerank+oracle` (ceiling).

## Gate mapping (Success Criteria)

| Field / comparison | Gate | SC |
|--------------------|------|----|
| `turn_recall` Δ (pcic − rerank) | ≥ +2pp overall OR +4pp multi-hop | SC-001 |
| per-category `turn_recall` Δ | no category < −1pp | SC-002 |
| `complement_drop` (pcic) | ≤ 0.05 | SC-003 |
| `anchor_violation` (pcic) | = 0 | SC-004 |

## Tests (contract)

- `TestCoverageEmitsSelectorMetrics`: a synthetic run emits the three fields per arm with the
  defined semantics.
- `TestSelectorMetricsGateThresholds`: given crafted selections, the reported metrics match
  hand-computed survival/complement/anchor values.
