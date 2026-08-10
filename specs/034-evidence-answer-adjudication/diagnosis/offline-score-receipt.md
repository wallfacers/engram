# 034 Offline Historical-Score Receipt

**Date**: 2026-08-10
**Status**: PASS for scorer integrity; the fixed-C1 stub is intentionally **NO-GO** and is not the feature verdict.

The final offline stub seal was joined, only after seal validation, to the three custody-matching frozen candidate
journals. This run validates the complete score CLI and historical denominators without a provider or network call. The
stub always selects blinded slot C1, so its accuracy is not evidence for or against a real adjudicator.

| Diagnostic | Result |
|---|---:|
| Historical verdict majority | 1371/1540 |
| Executable deterministic text control | 1368/1540 |
| Candidate oracle | 1411/1540 |
| Mixed / triggered mixed rows | 96 / 88 |
| Judge instability / triggered instability | 13 / 5 |
| Fixed-C1 historical mapping | 1367/1540 |
| Fixed-C1 triggered-mixed correct | 50/88 |
| Context parity / triggered context parity | 1532 / 766 |
| Integrity / frozen diagnostics | true / true |
| Stub verdict | `NO_GO` |

The first scorer pass exposed a one-question control mismatch (1367 rather than 1368): identical answer text with
conflicting legacy judge labels was being resolved by the blinded runtime slot. The corrected scorer uses the canonical
label-free source digest only to break an exact-answer historical tie. Runtime answer text is unchanged, and a verifier
cannot gain points by choosing a favorable duplicate slot. The rerun reproduced every frozen denominator above.

The fixed-C1 paired comparison against text control was control-only 12, selected-only 11, exact McNemar p=1.0; no
Holm-significant negative category appeared. It still failed both promotion thresholds (selected <1387 and
triggered-mixed correct <69), as expected for a non-semantic stub.

The canonical score artifact SHA-256 is
`f9b99b4f61192cc0c4df3045bb92dd2403920fcb2bb69093b928ec73f3fe7e00`. It is stored only in session scratch and is
labelled `historical_verdict_mapping`, never as a formal LoCoMo score.
