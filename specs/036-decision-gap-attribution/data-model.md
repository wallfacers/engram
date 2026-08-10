# Data Model: Decision-Gap Attribution

## AttributionRow

One question's three-way rebuild from the validated 034 seal.

| Field | Validation |
|---|---|
| `packet_id / conv / q / question_id` | Match the frozen 034 packet identity |
| `category` | 1–4 (fact_lookup / multi_hop / temporal_state_tracking / preference_recall) |
| `majority_correct` | `majorityCorrectness(C1..C3)` from hidden slot-map verdicts |
| `oracle` | `C1 || C2 || C3` correct |
| `control_correct / selected_correct` | Correctness of the canonical text-control slot and the 034 decision slot |
| `control_slot / selected_slot` | Computed via `canonicalHistoricalSlotForSameAnswer` (frozen tie-break) |
| `fallback_reason` | Present only when the decision state is fallback |

## AttributionGapRow

A question with `oracle == true && selected_correct == false`.

| Field | Validation |
|---|---|
| `correct_candidate_slot` | First correct slot in C1..C3 order |
| `selected_slot / selected_confidence` | From the 034 decision |
| `evidence_ids` | Decision's cited evidence (≥1 for selected state) |
| `normalized_equal` | Selected answer normalizes equal to some correct candidate |
| `failure_mode` | `semantic_equivalence` / `factually_wrong` / `evidence_insufficient` / `unclear` |
| `mode_evidence / mode_normalized_equal / mode_reason` | Machine-checkable classification evidence |
| `in_risk_queue / parent_refuted_any_view / unique_alternative` | 035 audit cross-validation (nullable; `audit_unavailable` when no 035 seal) |

## AttributionSummary

| Field | Meaning |
|---|---|
| `oracle_correct / selected_correct / control_correct / majority_correct` | Frozen denominators (1411 / 1378 / 1368 / 1371 on the real run) |
| `gap_count` | Always `oracle_correct - selected_correct` (33 on the real run) |
| `control_only_loss / both_wrong` | Control-correct-but-selected-wrong vs both-wrong-third-candidate |
| `fallback_gaps` | Gaps whose decision state is fallback (non-trigger + triggered) |
| `evidence_insufficient / factually_wrong / semantic_equivalence / unclear` | Failure-mode counts (sum == gap_count) |
| `dominant_mode` | Failure mode with the highest count |

## AttributionReport

The full diagnostic artifact: `rows` + `categories` + `summary` + `audit_source`. Written atomically to
`decision-gap-attribution.json` in the 034 stage dir. `result_kind` is always `decision_gap_attribution`; never a
formal LoCoMo score.
