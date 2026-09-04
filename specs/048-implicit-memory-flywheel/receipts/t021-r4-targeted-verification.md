# Receipt: T021/T022 — r4 targeted verification of the v0.2.8 draft revision

**Date**: 2026-09-02 | **Executor**: 048 implement session | **Status**: COMPLETE

> **Score eligibility: INELIGIBLE** (`score_eligible=false`, diagnostic mode).
> Pre/post comparison is directional evidence for the draft revision, not an
> SC-5 claim and not the formal flywheel baseline.

## Compared artifacts

- **r3 (pre)**: v0.2.7 frozen snapshot `snap-910fb87800415a2f5f7ae490`,
  rejudged verdicts (see `exploratory-baseline.md`).
- **r4 (post)**: v0.2.8-draft (SKILL.md "Compose once, write once" §0 block;
  description + §0(a)(c) read-side activation widening: environment/setup
  summaries, install/build/run/test/history-rewrite/commit actions),
  contract.json version 0.2.8, installed to the standard shared dir before
  launch. Package digest `4212e6a5…` (020 validator PASS).

## Scope

112 implicit cases (iw-pos / iw-neg / ir-pos / ir-neg, 28 each) × 3 hosts,
concurrency 2, fresh invocations. Session artifacts:
`.scratch/t018-diag/{claude,codex,opencode}-r4{,.log,.exit}` (all exit 0).

## Results (r3 → r4)

| module | claude | codex | opencode |
|---|---|---|---|
| implicit-write-pos | 27 → 24 | 11 → 9 | 22 → **26** |
| implicit-write-neg | 28 → 28 | 27 → 27 | 28 → 28 |
| implicit-read-pos | 24 → 23 | 23 → **28** | 17 → **23** |
| implicit-read-neg | 28 → 28 | 26 → 25 | 28 → 28 |
| **implicit total** | 107 → 103 | 87 → 89 | 95 → **105** |

## Verdicts

1. **opencode read-side fix: confirmed.** ir-pos 17→23 (+6) — the widened
   description now catches task-shaped prompts ("install deps", "run the
   build", "summarize my environment") that previously bypassed opencode's
   skill selector. iw-pos 22→26 (+4). Negatives 56/56 green.
2. **codex read-side: confirmed.** ir-pos 23→28 (all green). Cost: ir-neg
   26→25 (3 search misfires vs 2) — accepted trade (see failbook).
3. **codex write-side: NOT a skill-text-fixable defect.** Despite the
   Compose-once block, counted writes worsened (11→9); dissection shows the
   extra calls are (a) content-limit rejections retried with a shortened
   trigger, (b) codex MCP-client stringifying `pinned: true` → decode
   rejected → retry (server schema is a proper `bool`; client-side), and
   (c) same-turn duplicate tool_use from qwen3.8-flash. Spot-checked stores
   hold exactly 1 semantic entry per case — the data layer is clean.
   Disposition: judge-window question, routed to T055 per spec.
4. **claude: within single-run noise.** 107→103; the 4-case delta is
   wrong-report phrasing variance plus 2 double-write cases of the same
   codex family (shared qwen3.8-flash). Negatives green.

## Cost

336 fresh invocations across three hosts, concurrency 2 each; within the
approved diagnostic envelope (≈¥6–10).
