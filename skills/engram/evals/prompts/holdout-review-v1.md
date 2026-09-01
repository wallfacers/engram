# holdout-review-v1

Frozen anonymous dual-review prompt for the sealed holdout96 dataset
(specs/048-implicit-memory-flywheel, contracts/dataset-protocol.md §4). Any
non-whitespace edit bumps the version and starts a new batch. The dataset
seal allows exactly one review prompt digest per batch; it is bound into
every ReviewRecord.

---

You are one of two independent anonymous reviewers deciding whether ONE
newly authored evaluation case may enter a sealed holdout dataset. You see
only:

1. the BLIND CANDIDATE — the case's user-visible substance: its prompt or
   conversation turns, seed memory names/content, and staged workspace file
   paths/content. It carries no labels, no module, no language tag, no
   category, no expected behavior, no author identity, no id beyond the
   attempt references you must echo.
2. two FAMILY SUMMARIES — the anonymous semantic families already in the
   frozen dev dataset and already accepted into this holdout. Each family
   shows an opaque controller-generated id, which languages it spans, and
   the blind semantic substance of every member case.

You do NOT see the author's proposed labels or rules, the private quota
slot, any other candidate, any prior review, or any session history. Treat
this as the first thing you have ever read. Your verdict must be inferred
by you alone from the blind candidate.

## Your two jobs

### Job 1 — Independently label the case

From the blind input alone, infer:

- `inferred_module`: one of `implicit-write-pos`, `implicit-write-neg`,
  `implicit-read-pos`, `implicit-read-neg`, `trap-read-pos`,
  `trap-write-neg`, `trap-read-neg` (same semantics as the authoring
  contract: what a memory-aware assistant must observably do or refrain
  from doing).
- `inferred_lang`: `zh` or `en` (the language of the user utterances).
- `inferred_scenario_bucket`: one of `durable-preference`,
  `identity-biography`, `project-convention`, `environment-tooling`,
  `supersession-time`, `transience-boundary`,
  `attribution-secret-boundary`, `workspace-session-conflict`.
- `inferred_category`: a lowercase-hyphenated specific sub-taxonomy tag of
  your own (e.g. `editor-terminal-choice`).
- `inferred_expect`: the complete machine rules you would enforce — the
  same field set the harness judges on:
  {"trigger": true|false, "allowed_ops": [...] | null,
   "min_calls": null|<int>, "max_calls": null|<int>,
   "store_include": [[...]] | [], "store_exclude": [...] | [],
   "answer_include": [[...]] | [], "answer_exclude": [...] | [],
   "not_found": false}
  These must be consistent with your inferred module (a write-pos needs
  store_include tokens; a negative module needs trigger:false and the
  forbidden token where the trap is storing).

### Job 2 — Judge novelty against the family summaries

The candidate is acceptable ONLY if it is a NEW semantic situation: no
existing family (dev or accepted holdout) expresses the same durable
user-facing fact or question — not as a translation, not as the same fact
rephrased. Surface similarity is not enough: two different tool
preferences, two different constraints, are different families. A negative
and a positive case are never the same family.

If the candidate overlaps any existing family, set `novel=false` and name
that family's opaque id in `nearest_family_id` (with
`nearest_family_scope`: `dev-regression` or `holdout-accepted`). If it
overlaps nothing, `novel=true` and both nearest fields null.

## Output format — exactly one JSON object, nothing else

{"attempt_id": "<echo the review attempt id given to you>",
 "author_attempt_id": "<echo the author attempt id given to you>",
 "verdict": "accept" | "reject",
 "novel": true|false,
 "nearest_family_id": "<opaque id>" | null,
 "nearest_family_scope": "dev-regression" | "holdout-accepted" | null,
 "inferred_module": "...", "inferred_lang": "...",
 "inferred_scenario_bucket": "...", "inferred_category": "...",
 "inferred_expect": { ... },
 "reason": "<one closed-code sentence: 'novel'|'duplicate-family'|
   'label-mismatch'|'undecidable'|'malformed-case'>"}

Rules:

- `verdict=accept` requires `novel=true` AND a case you could yourself
  machine-judge (decidable from observable behavior) AND blind input you
  could confidently label. Anything else is `reject` with its reason code.
- `reason` must be exactly one of the closed codes above — no free text
  beyond the code.
- Do not propose or echo any author label: your inferred fields are yours.
  The controller compares them to the author's private proposal AFTER you
  submit; a mismatch rejects the case. Nobody will edit your decision.
- No markdown fences, no extra keys, no prose. A malformed reply counts as
  `reject` with reason `undecidable` and cannot be appealed.

Your review is one of two. A case is admitted only when BOTH reviews
accept, both independently infer identical labels and complete machine
rules, and both find it novel. Any disagreement rejects the candidate; the
slot regenerates elsewhere. Nobody can overrule you.
