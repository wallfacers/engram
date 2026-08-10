# 033 Independent Review

**Date**: 2026-08-10  
**Disposition**: review completed; **NO-GO; implementation not merge-ready; feature must not be merged**.

The independent reviewer regenerated the analyzer output from the raw paid-probe artifacts and matched the
recorded summary SHA-256 `e3b974679e0b0c7dfd598f4ee8c1a9075335123eb330d047d6f1690d60f35492`.
Target C-A was only `+1` against the pre-registered `+8` gate, guard loss was `0`, and the majority,
McNemar, Holm, and 57-row cap/admission calculations were independently reproducible. The review therefore
accepts the **NO-GO and stop-full-set decision**.

The review does **not** approve the implementation for merge. The following findings do not overturn this
run's conservative stop decision, but they must be fixed and independently re-reviewed before any future
attempt to retain, merge, or reuse the evaluation framework.

## Merge-blocking findings

1. **HIGH — the analyzer is not protocol-fail-closed.** It validates result shape but does not enforce process
   exit, model/regime, binary/data/store/cohort hashes, thinking/retry settings, judge/extract counts, or cost
   receipts. A future protocol-invalid artifact could still be reported as `valid=true`.
2. **HIGH — the analyzer does not enforce B/C attribution.** It does not require B=`legacy_grouped`,
   C=`kind_layered`, or compare closure, candidate count, cap, and the complete unit multiset. The reviewer
   manually checked all 54 current B/C audit pairs: closure, count, cap, complete units, and modes match, so the
   current zero-flip observation is usable; the reusable tool contract is nevertheless insufficient.
3. **HIGH — the frozen evidence chain is not durably reconstructable.** The driver does not enforce all
   pre-registered receipt hashes, the evaluated binary was built from a dirty/untracked worktree, and the
   verdict lacks a manifest covering every raw result, audit, regime, and log artifact.
4. **MEDIUM — runtime audit failure and resume semantics are unsafe for evaluation.** Assembly failure can
   silently fall back to legacy, audit write failure is only a warning, and resume truncates the audit journal
   before completed result rows are skipped.
5. **MEDIUM — B/C is descriptive, not a strict same-process causal comparison.** B and C ran in separate
   processes at concurrency 10 and 11 and covered 18 and 64 questions respectively. Their configured assembly
   distinction is real and the current inputs were manually matched, but the result must not be described as a
   strict same-process single-variable experiment.
6. **MEDIUM — currency cost is not closed.** Token/call usage is recorded, but these model IDs are unpriced in
   the harness. `actual_usd=0` is not a zero-cost claim; the provider bill is authoritative.

## Additional confirmations

- Canonical chunk-before-fact ordering, the legacy control, and non-multi/off parity are implemented and tested.
- Chunk-gold attribution files were used only after the run; no gold answer/evidence entered runtime prompts or
  retrieval. Target/guard are historically label-enriched cohorts and are not an absolute LoCoMo score.
- No secret value was found in the repository diff, and engine directories are untouched.
- Feature-package build/tests passed. The repository-wide suite remains non-green because of the independently
  reproduced nondeterministic test in the untouched `memory` package recorded in `verification.md`.

## Resolution

T030 is closed as a **terminal rejection**, not as merge approval. Because the pre-registered probe is NO-GO,
no full-set call is permitted and this feature will not be merged. The unresolved review findings are preserved
here as mandatory prerequisites for any future successor experiment; resolving them inside this rejected feature
would require a new binary and a newly frozen protocol, so it is intentionally out of scope for this run.
