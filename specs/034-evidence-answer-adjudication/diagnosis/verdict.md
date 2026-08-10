# 034 Stage-0 Verdict

**Date**: 2026-08-10
**Verdict**: **NO-GO — stop; do not start a formal paired rejudge from this protocol.**

This is a post-seal `historical_verdict_mapping`, not a new formal LoCoMo score. Integrity and every frozen diagnostic
passed, but both preregistered promotion thresholds failed.

| Gate / diagnostic | Result | Required |
|---|---:|---:|
| Selected historical mapping | 1378/1540 (89.48%) | >=1387/1540 (90.06%) |
| Triggered mixed-verdict correct | 61/88 | >=69/88 |
| Historical verdict majority | 1371/1540 | diagnostic |
| Executable deterministic text control | 1368/1540 | control |
| Candidate oracle | 1411/1540 | upper candidate-space diagnostic |
| Category non-regression | PASS | no Holm-significant negative category |
| Integrity / frozen diagnostics | true / true | both true |

The selected mapping improved the executable control by 10 questions: 23 control-wrong questions were rescued and 13
control-correct questions were changed to wrong. Exact McNemar p=0.132498, so this Stage-0 does not establish a reliable
paired improvement. Full-cohort category deltas were +2.48 pp multi-hop, -0.93 pp temporal, 0 open-domain, and +0.71 pp
single-hop; none was a Holm-significant negative regression.

## Frozen strata and sensitivity

- Triggered questions: 771; context-parity triggered: 766; context-drift triggered: 5.
- Mixed-verdict questions: 96; triggered mixed-verdict: 88.
- Judge-instability questions: 13; triggered judge-instability: 5.
- Instability sensitivity places the selected mapping in [1375, 1385] and triggered correctness in [666, 668]. Even
  the favorable selected bound, 1385, remains below the 1387 promotion threshold.

## Why this direction stopped below 90

The failure was selection precision, not recall or evidence availability. The adjudicator made 718 accepted
high-confidence selections and 53 triggered fallbacks. Relative to control, the accepted selections were correct on
637/718 versus 627/718 for control, but their net gain was only +10 because 13 useful control choices were overridden.

The 53 triggered fallbacks are not enough to close the gap:

| Fallback cohort | Rows | Current mapped correct | Candidate-oracle correct | Maximum rescue |
|---|---:|---:|---:|---:|
| `low_confidence` | 49 | 26 | 31 | +5 |
| `invalid_response` | 4 | 4 | 4 | 0 |
| Total | 53 | 30 | 35 | +5 |

Thus even an oracle-perfect replacement for every fallback reaches only 1383/1540, still four questions below the
gate. Those five rescuable fallback rows are mixed-verdict rows, so fallback repair can lift 61/88 only to 66/88; at
least three accepted high-confidence mixed decisions must also be corrected.

Among the 718 accepted selections, 22 selected-wrong rows still had a correct candidate available: 13 were
control-only losses and 9 had both selected and control wrong while the remaining candidate was correct. The main
remaining candidate-space opportunity is therefore a more precise adjudication of accepted selections, not accepting
medium/low confidence wholesale.

## Disposition and possible next hypothesis

Per the frozen protocol, NO-GO ends feature 034 and forbids a formal rejudge. No retrieval, reranker, engine, or default
benchmark change is justified by this run.

The candidate oracle (1411/1540) proves that enough answer candidates exist in principle, but this one-pass selector did
not identify them reliably. A separate future spec may test a risk-controlled second pass over label-blind candidate
disagreements: independently verify the chosen candidate and the deterministic control, then override only when the
evidence check is consistent. It should target the 13 harmful overrides and 9 both-wrong/third-candidate rows, include a
predeclared temporal safeguard, use a fresh protocol and fresh seal, and first prove on a frozen Stage-0 that it adds at
least nine net questions and eight triggered-mixed questions. It must not reuse this seal, tune on hidden labels, or be
reported as a reranker/recall improvement.

Canonical score artifact SHA-256:
`ffffcd171e5b71aef42eb590609a6e45b3281019db220aa14937d2b7c4af2179`.
