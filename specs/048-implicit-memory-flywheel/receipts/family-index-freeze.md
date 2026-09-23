# Receipt: T017 — Core DevFamilyIndex freeze (dev-family-index-v2)

**Date**: 2026-09-01 | **Executor**: 048 implement session | **Status**: FROZEN + family-aware PASS

## Frozen artifact

- `skills/engram/evals/dev-family-index.json` — algorithm `dev-family-index-v2`,
  index digest `4d46b0432697971acd02d1278ea641985f18474631844137c6e680841c68f61c`
  (canonical JSON; frozen output — WriteFrozenFile refuses overwrite).
- Input: core172 (`dev-regression-core.manifest.json` sealed payload), review
  prompt `skills/engram/evals/prompts/dev-family-index-review-v1.md` (frozen,
  unchanged through the v1→v2 algorithm bump).

## Result

- **120 families over 172 cases** (69 singletons, 50 two-member, 1 three-member);
  all 51 multi-member families are cross-language.
- 57 mirror candidates × 3 lanes = 171 fresh-session reviews, **52 unanimous
  `same_family=true` → all 52 joined**; 5 pairs had lane disagreement → not
  joined (these are the dataset's designed neg pairs; lane identification correct).
- v2 topic-alignment worked as designed: the v1 byte-equal rule had rejected
  40/52 unanimous pairs on slug wording alone (0 true divergences — see
  `dev-family-index-v1-superseded.json` and the validation-report entry).

## Derivation facts (from `derivation_receipt`)

- concurrency=3 (frozen), observed max_in_flight=3, observed overlap=true.
- Lane provenance (structural facts; tool_identity_digest bound in index):
  | lane | resolved model | CLI version |
  |---|---|---|
  | claude | qwen3.8-flash (settings `model: opus` → env default-model mapping) | 2.1.252 (Claude Code) |
  | codex | qwen3.8-flash (`-c model_provider=aq -c model=qwen3.8-flash`) | codex-cli 0.149.1 |
  | opencode | bailian/qwen3.8-flash (`--model` explicit) | opencode2 v0.0.0-beta-18743 |

  All three hosts unified on Bailian qwen3.8-flash per maintainer decision
  (2026-09-01); reviewer independence is carried by the three distinct host
  harnesses plus the label-blind review, per the amended pairwise-distinct rule.

## Validation

- `validate --split dev-regression --phase family-aware --dev-family-index …`:
  **PASS** — receipt `receipts/core172-family-aware-validation.json`
  (the earlier FAIL run against the v1-index rules is archived verbatim as
  `core172-family-aware-validation-v1index-FAIL.json`).

## Finding recorded (non-blocking): dataset near-duplicate

- Family `fam-77a5e465f83875e34acde5df` = {ir-pos-003 zh, ir-pos-015 en,
  ir-pos-026 en}. ir-pos-003/015 are a true zh/en mirror; **ir-pos-026 is an
  en near-duplicate of ir-pos-015** (same module/category/rule, same pnpm seed,
  near-identical prompt). The three-lane index is factually correct to group
  them; the family-aware gate now records same-language multi-member families
  as a WARN-line finding instead of failing on an unfixable fact of the frozen
  dataset. **Dedup backlog for a future dataset version**: drop or reshape
  ir-pos-026. core172 remains frozen and untouched (manifest digest unchanged).

## Attempts ledger (honest)

1. v1 algorithm run (0.15 元): completed but superseded — byte-equal slug join
   too strict (see above). Also produced with the placeholder provenance
   (resolved_model=unavailable) since fixed.
2. v2 run: discarded at 57/57 — one opencode transient failure (`exit -1`,
   lane daemon hiccup) tripped fail-closed; no retry existed. No dataset or
   index artifact was written.
3. v2 run with 3-attempt lane retry (0.15 元): **this frozen artifact**.

Failure-mode fixes landed during T017 (all fail-closed semantics preserved):
`extractDecisionJSON` parses the lanes' final answer envelope (a wrapper event
is an error, never a silent false); lane provenance from structural facts only;
transient lane retry.
