# Receipt: T018 — Exploratory diagnostic baseline (pre-revision skill v0.2.7)

**Date**: 2026-09-01 | **Executor**: 048 implement session | **Status**: COMPLETE

> **Score eligibility: INELIGIBLE.** This is a dev-only diagnostic baseline
> (`score_eligible=false` in every run receipt). It must NOT be cited for
> SC-5 attribution, as an official dev/regression score, or as the formal
> baseline of the flywheel comparison. The formal baseline is produced by
> the T045+ primary-series flow against an immutable snapshot with a
> shared CoreExecutionPlanReceipt.

## Tested artifact

- Skill package: frozen pre-revision snapshot `snap-910fb87800415a2f5f7ae490`
  (v0.2.7, package digest `1ab788db…3f24b4`), materialized at
  `~/.engram-eval/skill-snapshot-prerevision/` and installed byte-identical
  to `~/.agents/skills/engram` (codex/opencode discovery) with
  `~/.claude/skills/engram` symlinked (claude discovery). Verified
  `installed == repo package` by full-file sha256 diff before launch.
- Green-test receipt `formal-tooling` (passed) + package-validation receipt
  bound to that snapshot: `receipts/t018-formal-tooling-green-test.json`,
  `receipts/t018-package-validation.json`.

## Environment

- Dataset: core172 (`dev-regression-core.manifest.json`, sealed), 172 cases
  × 3 hosts = 516 fresh CLI invocations, concurrency 2 per leg, observed
  max_in_flight=2 overlap=true on every leg.
- All three hosts unified on Bailian qwen3.8-flash (maintainer decision
  2026-09-01): claude 2.1.252 (`--settings …aly_qwen_w`, MCP wired via
  per-case `--mcp-config`), codex-cli 0.149.1 (`-c model_provider=aq -c
  model=qwen3.8-flash --yolo`, MCP via `-c mcp_servers.engram.*`),
  opencode2 v0.0.0-beta-18743 (`bailian/qwen3.8-flash`, whitelist env — see
  failbook for the three harness defects fixed mid-task).

## Module × host results (172 cases each)

| module | claude | codex | opencode (rejudged) |
|---|---|---|---|
| implicit-write-pos | 27/28 (96%) | 11/28 (39%) | 22/28 (79%) |
| implicit-write-neg | 28/28 (100%) | 27/28 (96%) | 28/28 (100%) |
| implicit-read-pos | 24/28 (86%) | 23/28 (82%) | 17/28 (61%) |
| implicit-read-neg | 28/28 (100%) | 26/28 (93%) | 28/28 (100%) |
| trap-read-pos | 18/18 (100%) | 16/18 (89%) | 15/18 (83%) |
| trap-write-neg | 6/6 (100%) | 6/6 (100%) | 6/6 (100%) |
| trap-read-neg | 4/4 (100%) | 4/4 (100%) | 4/4 (100%) |
| regression (020 legacy) | 30/32 (94%) | 29/32 (91%) | 32/32 (100%) |
| **total** | **165/172 (95.9%)** | **142/172 (82.6%)** | **152/172 (88.4%)** |

opencode column note: the live run reported 32 `runner-error` verdicts that
were a harness exit-code misread (opencode2 exits 1 after emitting a
completed stream — "Session interrupted: shutdown" follows the final
answer). `skill-eval rejudge` re-ran the deterministic judge offline over
the same raw streams and stores (no CLI/model invoked); the table uses the
rejudged verdicts. Verdict list: `.scratch/t018-diag/opencode-r3-rejudge.json`
(session artifact; not a formal receipt).

## Failure profile → revision targets (input to T021, non-claiming)

- **codex `implicit-write-pos` 39%** — 17/17 failures are
  `wrong-op: write called 2 times (max 1)`. The write lands with the right
  content; the "exactly once" contract is violated by a duplicate second
  write. T021 draft must make "one write, never re-issue" explicit for the
  codex harness surface (and T055 will confirm whether this is
  contract-matching or a judge-window question — no SC claim now).
- **opencode `implicit-read-pos` 61%** — 11 false-negatives (answered
  without any memory retrieval) dominate; plus scattered write-pos misses.
- **claude residuals** — 4 failures: 2 read-pos false-negative, 2
  read-pos wrong-report, 1 write-pos false-negative, 1 regression
  false-positive/false-negative pair.
- Regression layer (020 legacy) ≥91% on every host — no host regressed
  below the v0.2.x-era regression band from skill contract changes.

## Attempts ledger (honest)

1. r1 (all three hosts, killed at ~18 cases): installed skill was 0.1.0 —
   not the frozen snapshot; claude had no symlink. Aborted, environment
   re-synced. No data used.
2. opencode r2 (172/172 against 0.1.0): superseded by the version fix; kept
   as `opencode-r2-stale-0.1.0skill/`.
3. r3 (this receipt): claude-r3, codex-r3 exit 0; opencode-r3 exit 0 with
   the exit-code misread later corrected offline by rejudge.
4. Cost: aborted/superseded invocations ≈ ¥2–3; r3 = 516 calls
   (≈¥9–15 band as approved). Total T018 within the approved envelope.

## Harness defects found and fixed during T018 (tracked in failbook)

1. v2 runner MCP wiring missing for claude/codex (present in v1) — fixed.
2. Relative `--scratch` paths broke three downstream consumers —
   `filepath.Abs` at the case-dir boundary.
3. opencode2 provider routing poisoned by inherited shell env — whitelist
   env for the opencode child; plus the `cmd.Env` clobber bug.
4. opencode2 exit-code-after-completion misread as runner-error —
   completion-event stream now outranks exit code; `rejudge` subcommand
   added for offline re-verdicts.
