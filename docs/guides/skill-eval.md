---
title: Skill trigger evaluation & data flywheel (skill-eval)
summary: 本文说明 cmd/skill-eval 触发评估 runner 的用途、判题口径与产出物；面向运行 skill 数据飞轮评估的维护者与 agent。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-31
canonical_for: [skill-eval-harness]
tags: [skill-eval, guide, evaluation]
---

# Skill trigger evaluation & data flywheel (skill-eval)

`cmd/skill-eval` is the trigger-evaluation runner for the engram agent skill
(spec: `specs/048-implicit-memory-flywheel/`). It drives real agent CLIs —
Claude Code, Codex, OpenCode v2 — in non-interactive mode against a versioned
trigger dataset, judges every case deterministically from observable output,
and emits the reports the skill data flywheel consumes.

## What it measures

The skill fires through three channels (skill contract v0.2.0):

1. **Explicit** — the user wording asks for a memory operation (020 behavior,
   kept as the regression layer).
2. **Implicit write** — ordinary conversation reveals a durable fact (stable
   preference, constraint, identity, project convention, long-term decision);
   the skill must write once **and acknowledge in the same turn**, without
   asking permission first.
3. **Implicit read** — a question or task depends on remembered facts; the
   skill must search **before** answering, and report empty results honestly.

Negative layers guard the boundary: transient states, RAM/cache/database
near-misses, explicit refusals, secrets, pseudo-triggers ("remember in a
file"), and memory-independent questions must produce **zero** engram calls.

## Dataset

Datasets live in the skill package at `skills/engram/evals/`:

| File | Modules | Content |
|---|---|---|
| `implicit-write.json` | `implicit-write-pos` / `implicit-write-neg` | durable-fact disclosures vs. never-record cases |
| `implicit-read.json` | `implicit-read-pos` / `implicit-read-neg` | memory-dependent questions vs. independent ones |
| `trap.json` | `trap-read-pos` / `trap-write-neg` / `trap-read-neg` | adversarial layer: store-content injection, entity confusion, dated supersession, retelling recount, memory-over-environment, secret read/write, imperative-"remember", pasted-text injection |
| `trigger-evals.json` | `regression` | the frozen 020 explicit set (16 pos / 16 neg), never edited |

Rules: datasets are **append-only** (cases are never deleted or reworded —
fixed cases are added back with `source: "flywheel-round-N"`); every positive
case carries machine-readable judge rules (`store_include`, `answer_include`,
`acknowledge`, `notfound`); zh/en must both be covered per module. Trap cases
add two exclude rules — `answer_exclude` (no listed token may appear in the
answer: injection canaries, echoed secrets) and `store_exclude` (no listed
token may remain in the store after the turn) — and may stage environment
evidence via `files` (planted into the per-case workspace; visible to
claude/opencode whose cwd is the case dir — codex runs in the shared scratch,
so file-backed traps degrade to memory-over-nothing there).

```bash
~/bin/skill-eval validate --dataset skills/engram/evals
```

enforces module coverage, ≥20 cases per implicit module, 40/40 pos-neg balance,
unique ids, and machine-rule completeness. A run refuses to start on a failed
gate. Trap modules carry their own gates (≥4 per module, zh+en, pos≥12/neg≥8,
every trap read case must carry at least one machine rule).

## Running

Prerequisites: built binaries (`engram`, `engram-mcp`) in a `--bin-dir`; a
scratch directory (MCP configs and opencode project configs are generated
per case by the runner); the engram skill installed so all three tools
discover exactly one copy (`~/.agents/skills/engram`, symlinks for Claude
Code). For claude, pass the settings file with the host-model endpoint via
`ENGRAM_SKILL_EVAL_SETTINGS`.

```bash
ENGRAM_SKILL_EVAL_SETTINGS=~/.claude/settings.json.glm_w \
skill-eval run --tool claude,codex,opencode \
  --concurrency 6 --timeout 200 \
  --dataset skills/engram/evals \
  --bin-dir <dir-with-engram-binaries> \
  --scratch <scratch-dir> \
  --out <report-dir>
```

- **Per-case isolated store**: every case runs against its own fresh store
  (`<scratch>/data/<label>/<case-id>`) with its own workspace template — no
  cross-case seed contamination of write judgements, no single-writer SQLite
  contention. Seeds are planted via the CLI before each read case.
- **`tool@variant` model-matrix axis**: `codex@ds` / `codex@qwen` select the
  codex Profile V2 of that name (cheap cloud backends for model-capacity
  comparisons); `claude@opus` uses the variant as the claude `--model` slot
  (resolved through `--settings`). The variant namespaces the store container
  and the report label, so backends never collide.
  Backend wiring notes (round-3, probe-verified): codex 0.150 dropped
  `wire_api = "chat"` — OpenAI-compatible endpoints must serve `/responses`
  (vLLM-style; set `wire_api = "responses"`); two codex runs must not execute
  concurrently (shared models-cache refresh lock: `codex_models_manager`
  timeout → 3-fail breaker); opencode `run` needs `--model provider/id` — the
  project config's `model` key alone is ignored (it fell back to the free
  Console tier), and quick successive standalone spawns can race provider
  registration (`provider.no-route`) — a trivial wrapper script in PATH masks
  it.
- **opencode.json is generated per case** by the runner (v2 schema
  `mcp.servers.<name>`). Set `ENGRAM_SKILL_EVAL_OPENCODE_MODEL`
  (`provider/model-id`) and `ENGRAM_SKILL_EVAL_OPENCODE_BASE` (OpenAI-compatible
  base URL; the key flows through `{env:MAAS_API_KEY}` templating, never into
  a file) to run opencode against a custom provider.
- Tools run in parallel; cases within a tool run through a worker pool.
- Concurrent runs against the same `--scratch` remain forbidden as a hygiene
  rule (same-label runs would overwrite each other's per-case dirs); use a
  separate scratch dir per concurrent run.
- A tool that fails 3 consecutive cases is marked `runner-unavailable` —
  distinct from case failures. Partial verdicts collected before the break are
  kept in the report.
- Raw event streams are archived under `<out>/raw/<label>/<case>.jsonl`.

Judgement is deterministic (same raw output ⇒ same verdict) and anchored to
**engram operation traces** — MCP `memory_*` calls or real CLI invocations
(exploration like `go build`/`which`/file reads never counts) — plus
store-side verification (`engram list` after the turn). Failure classes:
`false-negative` (should have acted, didn't), `false-positive` (acted when it
must not), `wrong-op` (acted, wrong content/operation), `wrong-report` (right
operation, no same-turn acknowledgment / invented answer).

## The flywheel loop

One iteration:

1. **Run** the full dataset (`run ...`), producing `run-report.json` and
   `failures.jsonl`.
2. **Triage** every failure line: fill `root_cause` (trigger wording missing /
   contract too narrow / contract contradiction / host-specific behavior).
3. **Revise** `skills/engram/SKILL.md` (and `references/contract.json`; bump
   `skill.version`) — keep the operation contract intact; change only when to
   fire.
4. **Re-sync the installed copy** if it is a copy rather than a symlink:
   `npx skills add <repo>/skills/engram --global` (dev state may keep
   `~/.agents/skills/engram` as a symlink to the repo — edits land instantly).
5. **Re-run the full dataset** (never only the failing cases — the regression
   layers catch over-firing).
6. **Backfill**: each fixed failure becomes a new permanent case with
   `source: "flywheel-round-N"`.
7. Ship: `node scripts/validate-agent-skill.mjs --source` +
   `node --test scripts/validate-agent-skill.test.mjs` must be green; tag
   releases per the 020 release discipline (maintainer authorization).

## Post-run hygiene: never pollute the maintainer's real memory (hard)

Eval data is synthetic and must never linger in any real memory system
(maintainer directive 2026-08-29). The runner enforces this automatically at
the end of every `run` (`sweepHostArtifacts`):

- **Host auto-memory dirs**: claude-code keys each eval instance's auto-memory
  to its case cwd, leaving one `~/.claude/projects/-<encoded-scratch>…`
  directory per case. All directories prefixed with the run's encoded scratch
  path are removed.
- **Leaked seeds in the user store**: any dataset seed entry that reached the
  real default store `~/.engram/default.db` (early rounds seeded before
  `--data-dir`/`ENGRAM_DATA_DIR` was plumbed everywhere) is deleted through
  the engram CLI — never raw SQL, so the FTS mirror stays consistent.

Both sweeps print a `swept N eval project dirs, M leaked seed entries` line.
Manual cleanup of pre-sweep accumulated state (2026-08-30): 713 project dirs
removed; `peanut-allergy`, `no-force-push-hard-rule`, `working-timezone`
deleted from `~/.engram/default.db`. If you run the harness from a different
scratch root, the sweep only covers that run's prefix — old roots need the
matching manual `rm -rf ~/.claude/projects/-<encoded-old-scratch>*`.

**Evidence-ledger leakage (manual; the CLI has no evidence surface)**: a
harness bug can land `memory_write`-sourced rows in the ledger
(`memory_evidence` / `memory_evidence_heads` / `memory_evidence_events`)
while `memory_entries` stays empty — `list` shows nothing, so the CLI sweep
misses them. Audit by counting the three tables; rows carrying eval content
(scratch paths, seed facts) are leaked. Back up `default.db` to the scratchpad
first, then delete those evidence ids in one transaction — the ledger tables
are not FTS-mirrored, so this stays consistent. (3 such rows from the
2026-08-29 matrix harness found and cleaned 2026-08-30.)

## Reading the report

`run-report.json` → per tool, per module pass counts. The gates from the spec:
implicit-write-pos ≥90%, implicit-read-pos ≥90%, negatives ≤10% misfire,
regression layers unchanged (≥90% / ≤10%). Host divergence (one tool firing
where others don't) is a finding to record, not to average away.
