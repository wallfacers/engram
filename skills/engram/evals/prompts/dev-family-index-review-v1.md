# dev-family-index-review-v1

Frozen review prompt for `dev-family-index-v1` cross-language mirror candidates
(specs/048-implicit-memory-flywheel, contracts/dataset-protocol.md §2). Any
non-whitespace edit bumps the version and re-runs the derivation. The prompt
digest (`lf-normalized-sha256-v1`) is bound into every derivation receipt.

---

You are one of three independent reviewer lanes building a semantic family
index over an existing memory-trigger evaluation dataset. You receive one
candidate PAIR of cases (A and B). Each case shows only:

- the user-visible prompt or conversation turns;
- seed memory names/content if present;
- staged workspace file paths/content if present.

You do NOT see case IDs, language tags, module labels, categories, expected
behaviors, judge rules, or where each case came from. You have no knowledge of
other pairs, and no session history: treat this as the first thing you have
ever read.

TASK: decide whether A and B express THE SAME durable user-facing semantic
fact or question — a translation, or the same fact phrased differently —
such that keeping both as separate evaluation cases would double-count one
semantic situation.

Judge by meaning, not by wording: if A and B would be written into a memory
as the same fact (write-side pair), or answered from memory by the same fact
(read-side pair), they are the same family. Surface similarity is not
enough: two cases that merely share a topic (two different tool preferences,
two different constraints) are NOT the same family. A negative case (one
that must NOT trigger a memory operation) and a positive case are never the
same family.

OUTPUT: respond with exactly one JSON object and nothing else:

{"same_family": true|false, "canonical_family_digest": "<string>"}

Rules for the output:

- `same_family` is your independent decision under the rule above.
- `canonical_family_digest` is a short stable identifier describing the
  shared semantic fact when `same_family` is true (e.g. "pkg-manager-pnpm").
  When `same_family` is false, use the empty string "".
- If you cannot determine the meaning of either case, answer
  `{"same_family": false, "canonical_family_digest": ""}` — an uncertain
  reviewer never joins a family.
- No prose, no markdown fences, no extra keys. A malformed reply counts as
  a disagreement and the pair stays split.

Your reply is one of three votes. A pair joins one family only when all
three lanes return `same_family=true` with identical
`canonical_family_digest` values. Any disagreement leaves the pair split;
nobody can overrule or edit your decision.
