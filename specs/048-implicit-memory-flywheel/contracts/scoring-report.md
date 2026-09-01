# Contract: Dual Official Score & Report

**Feature**: `048-implicit-memory-flywheel`

**Contract version**: 4

**Date**: 2026-09-01

## 1. Score identities

The report contains exactly two headline score families:

1. `dev/regression score` — the immutable 172-case dev/regression core manifest. Append-only flywheel extensions are reported separately and never alter this denominator.
2. `generalization score` — the sealed 96-case holdout manifest.

Each family is a matrix of Claude Code, Codex and OpenCode2 host scorecards. It is not a single cross-host average. The renderer may show an all-host supplemental total only under the label `non-gating supplemental`; it cannot determine PASS.

## 2. Eligibility

An `official-dual` host × split score is official only when all of these hold:

- exactly ordinals 1, 2 and 3 exist under one sealed series;
- all three have the same sealed skill, runner, judge, dataset, invocation-template and stable tool-identity values; per-run provenance timestamps may differ;
- the sealed series binds one anchored immutable `FrozenSkillPackageSnapshot`, a passing `SkillPackageValidationReceipt` produced for that exact snapshot file list/package digest, and a passing `series-prepare` `GreenTestReceipt`; primary children use only the frozen snapshot, never the mutable package source;
- an `official-dual` manifest carries a stable `CandidateBindingV1` digest over the snapshot/anchor/package receipt, runner/judge/validator, exact dataset identities, core plan, stable tool/config/execution-policy and `series-prepare` `stable_identity_digest`; the digest excludes series/manifest/exact per-series green, `pre-holdout` and runtime receipt identities, but those per-series artifacts remain mandatory and are checked separately;
- any required host × worker-slot `WorkspaceCanaryReceipt` map matches the final frozen snapshot/template/worker-slot configuration;
- every ordinal has the exact expected 172 or 96 cases, exactly once each;
- every case has raw-event, normalized-event, store-dump, workspace, verdict and disposable-state isolation/reset receipts;
- a series containing holdout binds a successful pre-seal `ProtectedExecutionReceipt` whose host × worker-slot probe matrix matches the final invocation templates/concurrency and proves controller-confirmed protected-root, author/review-state and active-sibling denial plus own-workspace readability; every case additionally proves its assigned worker slot/probe, matching actual child identity/template/access boundary, prior-case-state/retired-workspace denial, root non-reuse and core/holdout allocator separation;
- no diagnostic artifact, `--only`, sample, case retry, partial denominator or “later run wins” replacement contributes.

Otherwise its state is `INVALID`, not zero, not unavailable-as-pass and not a partial score. If any required host/split is invalid, the overall formal verdict is `INVALID` rather than PASS.

A `dev-comparison` series uses the same primary completeness, three-ordinal and stable provenance-identity rules for core172, but `score` must reject it and it can never populate an `OfficialScoreReport` or headline. `failure-archive` and `compare` create their own sealed dev/flywheel receipts only; neither can emit an official score family.

For a holdout-bound series, ordinal 1 must additionally bind a fresh passing `pre-holdout` GreenTestReceipt created after that series' complete core leg. It must name the exact sealed manifest, the manifest's stable `candidate_binding_digest` and the complete core-leg receipt-set digest, and the protected binding ledger must associate those exact values with that series before any holdout child starts. If the series later becomes `INVALID`, no run or per-attempt green-test receipt from it is score-eligible. A recovery score exists only after a new series independently seals a new manifest with the same stable `CandidateBindingV1` digest, reruns core172 completely, creates a **new** `pre-holdout` receipt bound to that new manifest/core completion, associates it as a new attempt in the same binding ledger, and then completes holdout96 for all hosts and ordinals. The final report names only that complete recovery series as `series_id`; prior invalid series appear only as non-scoring `binding_ledger_evidence` digests. Treating the old manifest or old `pre-holdout` receipt as part of the stable binding, or combining old successful ordinals/receipts with new ones, is a hard rejection.

## 3. Per-run metrics

For each run ordinal and each applicable module:

```text
positive_pass_rate = passed_cases / expected_positive_cases
negative_false_positive_rate = false_positive_cases / expected_negative_cases
```

All denominators are written beside their numerators. `runner-error` never disappears: a complete primary run either classifies it according to the sealed rules and retains it in the denominator, or becomes invalid. The report never replaces missing cases with zero-denominator NA while still calculating a score.

The sealed runner-error mapping is conservative and module-specific: in a positive module, terminal `runner-error` is a non-pass; in a negative module, it is reported separately as `runner_error_cases` **and** counts in `false_positive_cases` for the official negative gate numerator. Thus a negative result that is unknown because the runner failed cannot improve a false-positive rate. This mapping does not change the requirement that every case retain one terminal receipt.

## 4. Median rule

For every host × split × metric, compute the three run rates independently. Sort the three scalar rates numerically; the second value is the official median.

```text
official_median(host, split, metric) = sort(rate(run-01), rate(run-02), rate(run-03))[1]
```

The report preserves each `(passed, expected, rate)` tuple; it must not report only the median. Module medians are computed before any supplemental all-module/all-host display.

## 5. Gates

The user-selected A policy is mandatory: each host separately meets every gate applicable to its split.

| Gate | dev/regression | holdout |
|---|---|---|
| implicit-write-pos | median ≥90% | median ≥90% |
| implicit-read-pos | median ≥90% | median ≥90% |
| implicit-write-neg + implicit-read-neg | false-positive median ≤10% | false-positive median ≤10% |
| 020 explicit regression pos/neg | pos median ≥90%; neg FP median ≤10% | not applicable (module absent) |
| trap-read-pos | reported if present | median ≥90% |
| trap-write-neg + trap-read-neg | reported if present | false-positive median ≤10% |

`dev/regression` preserves the 020 explicit regression gate. Holdout’s trap gate uses the same positive/negative thresholds and is not optional. A host can pass only if every applicable row passes. Any host failure means `FAIL`; no across-host mean can mask it.

With the frozen 8 positive / 8 negative trap denominators, the thresholds are intentionally exacting: a single ordinal passes the positive gate only at 8/8 and the negative gate only at 0/8 false positives. The three-run median passes when at least two ordinals meet those exact rates. The scorer must not round 7/8 up to 90% or 1/8 down to 10%.

## 6. Required report fields

```json
{
  "schema_version": 3,
  "series_id": "...",
  "series_manifest_digest": "...",
  "candidate_binding_digest": "...",
  "holdout_binding_receipt_digest": "...",
  "frozen_skill_snapshot_digest": "...",
  "frozen_skill_snapshot_anchor_digest": "...",
  "core_execution_plan_digest": "...",
  "skill_package_validation_receipt_digest": "...",
  "green_test_receipt_digests": {"series_prepare": "...", "pre_holdout": "..."},
  "protected_execution_receipt_digest": "...",
  "workspace_canary_receipt_digests": {"claude": {"1": "..."}, "codex": {"1": "..."}, "opencode": {"1": "..."}},
  "dev_regression_score": {
    "dataset_digest": "...",
    "hosts": [{"host": "claude", "runs": ["..."], "metrics": {}, "gate": "pass"}]
  },
  "generalization_score": {
    "dataset_digest": "...",
    "hosts": [{"host": "codex", "runs": ["..."], "metrics": {}, "gate": "pass"}]
  },
  "supplemental_cross_host": {"non_gating": true, "metrics": {}},
  "bias_diagnostics": {
    "non_gating": true,
    "scenario_buckets": {},
    "evaluated_host_by_author_module_language": {},
    "self_author_gap": {},
    "author_review_funnel": {}
  },
  "diagnostic_artifacts_used": false,
  "binding_ledger_evidence": [{"invalid_series_id_digest": "...", "recovery_event_digest": "...", "scoring_input": false}],
  "overall_verdict": "pass",
  "report_digest": "...",
  "seal_digest": "..."
}
```

For every host, report CLI version, resolved model or `unavailable`, profile/model request, sanitized configuration digest, invocation-template digest, frozen skill snapshot/package/anchor, dataset/runner/judge digests, timeouts, concurrency, case order seeds and artifact paths. `ToolProvenance.source_revision` identifies only the runner source subtree and/or runner binary; it excludes the skill package, datasets, specs/docs and artifacts. Do not report secrets, raw settings/config, endpoint URLs or arbitrary stderr.

For holdout, also report the pre-registered scenario-bucket slices, `evaluated host × author lane × module × language`, self-author versus other-author gap, and generation/review funnel under `bias_diagnostics.non_gating=true`. Every rate cell MUST write its numerator, denominator, `independent_case_count` (not ordinal count), and `low_n=true|false`; a three-ordinal repeat does not increase independent cases. The renderer must mark every small cell as low-N instead of treating an empty/small rate as evidence of no source bias. These slices cannot alter case admission after scoring, any gate, `overall_verdict`, or either headline score; they exist to detect residual source/semantic skew and to motivate only a future newly generated dataset version.

## 7. Interpretation rules

- State the two scores separately everywhere: terminal output, JSON, validation report, dataset card and release notes.
- `generalization score` is valid only for a sealed holdout not used to tune the evaluated skill. Once the holdout is consumed, retain the historic score but do not call it unseen for a new skill revision.
- Because the three evaluated host families also participate in authoring/review, label the holdout result `untuned/session-isolated synthetic holdout generalization evidence`; never call it model-unseen or claim the underlying models never processed the synthetic cases.
- A diagnostic rerun can explain a failure but cannot alter any official rate, median, manifest, report or verdict.
- A runner-unavailable result is infrastructure truth, not model success/failure. It makes the expected official score unavailable/invalid and must be displayed beside other hosts.
- Report host differences as findings. Do not label a three-host claim PASS when only a combined total passes.

## 8. SC-5 flywheel comparison eligibility

The exploratory diagnostic run captured before the final runner/judge exists is not an SC-5 baseline. The comparable baseline is a `dev-comparison` primary series produced only after the final command surface and deterministic judge are frozen, using an anchored immutable pre-revision `FrozenSkillPackageSnapshot` and complete core172 for all three hosts and all three ordinals. The candidate comparison similarly names an independently anchored final snapshot; neither mutable `skills/engram` source tree may be opened as the evaluated package.

A before/after row is eligible for the SC-5 fail-to-pass claim only when baseline and candidate reference the exact same sealed `CoreExecutionPlanReceipt`: runner, judge, core dataset, timeout, concurrency, three case-order seeds, normalized evaluated-child execution/isolation template and each host's stable `tool_identity_digest` must therefore be identical. `ToolProvenance.source_revision` is runner-only, so a skill/docs edit cannot masquerade as runner drift. `captured_at`, purpose, series ID and unique artifact IDs are not identity inputs and may differ. The two anchored frozen skill-package digests are the sole intentional variable; a purpose label or unique artifact ID must not alter a core child's invocation semantics. For each host × core case, its before/after state is the binary median of the three ordinal terminal pass/fail states: two or three passes is pass; two or three non-passes is fail. Append-only extension results are reported separately and cannot substitute for a comparable core172 before/after row. At least one eligible baseline-median failure must become a post-change median pass and every eligible baseline-median pass-to-fail regression must be counted; otherwise SC-5 is `FAIL`. The comparison must read only sealed core172 receipts; any holdout plaintext/child receipt input is a hard rejection.
