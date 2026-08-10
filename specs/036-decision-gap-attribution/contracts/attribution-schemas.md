# Artifact Contracts: Decision-Gap Attribution v1

Canonical JSON uses Go `encoding/json` field order from the frozen structs and a trailing newline for JSON documents.
JSONL digests are computed over numeric `(conv,q)` sorted canonical records, each followed by `\n`. Every digest string
is `sha256:<lowercase-hex>`.

## `decision-gap-attribution.json`

Schema: `036.decision-gap-attribution.v1`.

Required fields:

```text
schema, result_kind, protocol_hash, rows, categories, summary
```

- `result_kind` is always `decision_gap_attribution`; this is a diagnostic artifact, never a formal LoCoMo score.
- `rows` is the full per-question three-way rebuild; `rows.rows[].count == protocol question_count` (1540 on the frozen
  run). Each row carries `majority_correct / oracle / control_correct / selected_correct / control_slot / selected_slot`.
- `rows.gaps[]` are exactly the questions where `oracle == true && selected_correct == false`. Each gap carries the
  machine-checkable failure-mode evidence: `normalized_equal`, `failure_mode`, `mode_evidence`, `mode_normalized_equal`,
  `mode_reason`, and (when 035 audit data is present) `in_risk_queue`, `parent_refuted_any_view`,
  `unique_alternative`, `audit_unavailable`.
- `categories[]` is the category × failure-mode table; the four category rows (1–4) sum to `rows.gaps` length.
- `summary` carries `oracle_correct / selected_correct / control_correct / majority_correct / gap_count /
  control_only_loss / both_wrong / fallback_gaps / evidence_insufficient / factually_wrong / semantic_equivalence /
  unclear / dominant_mode`.

## Failure modes

Deterministic, machine-checkable, in order:

1. `semantic_equivalence` — some correct candidate shares the selected answer's normalized text (`normalized_equal: true`).
2. `factually_wrong` — selected decision cites at least one valid evidence ID but picked a non-correct candidate
   (`mode_evidence` lists the cited evidence).
3. `evidence_insufficient` — selected decision cites no evidence. **Contract note:** the 034 selected-decision schema
   requires ≥1 evidence citation, so this mode is a defensive branch and should not fire on validated 034 inputs.
4. `unclear` — cannot be decided from available fields.

## Inputs (read-only, never modified)

- 034 stage dir: `manifest.json`, `packets.jsonl`, `sealed-decisions.jsonl`, plus the custody-validated hidden
  `slot-map.jsonl` and the three `results-hybrid.jsonl` candidate sources.
- Optional 035 audit dir: `resolver-map.jsonl` + `audit-calls.jsonl`. Missing/invalid audit dir → every gap row is
  marked `audit_unavailable` and the report still succeeds.

## Forbidden fields

No `gold`, `correct`, `verdict`, `raw_source`, `credential`, raw endpoint, provider response, or provider error may
appear in the report. The attribution process never writes to `memory/ embedding/ provider/ store/ internal/`.
