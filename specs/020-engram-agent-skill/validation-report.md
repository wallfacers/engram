# Validation Report: engram Agent Skill

**Feature**: `020-engram-agent-skill`

**Status**: implementation complete; release qualification remains blocked by
the explicitly listed external prerequisites. This file records factual
evidence and release blockers. A blocker is never treated as a passing
verification result.

## T001 — Workspace and parallel-feature baseline

- Recorded at commit `ab791d06865f4ddf4662ee3ee517d37fd7599481`
  (`ab791d0 docs: restore detailed benchmark comparisons`).
- Current feature-owned pre-existing changes are `.specify/feature.json` and the
  untracked `specs/020-engram-agent-skill/` directory. They were preserved.
- `git diff --name-only -- memory embedding provider store internal` produced no
  output before implementation. This adapter feature must keep that result empty.
- The sibling worktree is
  `.claude/worktrees/bio-retrieval-locomo` at `4e89c89`. Its active feature is
  `003-bio-retrieval-locomo`; its local edits are `CLAUDE.md` plus deletion of two
  historical `docs/superpowers/specs/` drafts. They are not touched here.
- Feature 003 principally changes retrieval/evaluation surfaces; it has a future
  README/docs task that may overlap this feature's documentation integration.
  Recheck the sibling before editing README or docs files; no current file
  collision is being resolved by this feature.

## T002 — Toolchain, scratch boundary, and cost ledger

| Item | Observed result | Eligibility / action |
|---|---|---|
| Go | `go1.25.0 linux/amd64` | eligible for offline build/test |
| Node.js | `v24.14.1` | eligible; meets installer and CI baseline |
| npm | `11.11.0` | available |
| Git | `2.43.0` | available |
| `skills@1.5.20` | cached metadata declares Node `>=22.20.0` | compatible; no remote install run yet |
| Claude Code | `2.1.220` command available | cost class `unknown`; no agent invocation run |
| Codex | wrapper found, but `@openai/codex-linux-x64` is missing | blocked; no invocation run |
| OpenCode | command absent | blocked; no invocation run |

- Session scratchpad: `/home/wallfacers/.codex/sessions/engram-020.NsgiUF`.
  All install fixtures, caches and generated test state stay below this path.
- No engram provider, hosted reranker/recall model, LoCoMo run, agent inference,
  or pay-as-you-go credential has been configured or used.
- All unexecuted behavior and real-client checks have `execution_cost_class:
  unknown`; they remain release blockers until a local or existing-flat-rate path
  is recorded with incremental model cost `0`.

## Open release blockers

1. OpenCode is not installed in this environment.
2. The Codex wrapper lacks its Linux optional binary dependency.
3. No eligible, documented zero-incremental-cost agent-inference path or
   maintainer behavior-review disposition has been supplied.
4. No maintainer authorization exists to create or push the predeclared release
   tag after a candidate commit is formed.

## T003–T008 — Foundational package and validator

- T003 test-first evidence: before `scripts/validate-agent-skill.mjs` existed,
  `node --test scripts/validate-agent-skill.test.mjs` failed with
  `ERR_MODULE_NOT_FOUND` for that implementation module. The suite covers
  frontmatter, description semantics, reference escape/missing paths, manifest
  and eval schemas, secret-shaped fixtures, token boundaries, digest ordering and
  newline normalization, root/internal symlinks, extra-file drift, duplicate
  bodies, documentation command drift, and release placeholders.
- T004 implementation evidence: after the Node-standard-library validator and
  reusable identity helpers were added, the same suite passed `7/7`.
- T005–T007 created the only canonical package at `skills/engram/`, including a
  two-field frontmatter body, LF-identical Apache-2.0 license, references,
  versioned manifest shell, and eval shells. No client-specific body was added.
- T008: the validator test suite passes, while source validation intentionally
  fails because the skeleton still has empty MCP/CLI/intent arrays, no behavior or
  trigger cases, and no synchronized installation command in package/docs. Those
  failures are expected pending US1–US4 and are not release evidence.

## T009–T016 — Installation and discovery MVP

- T009 first added a failing assertion for the user-scope default: removal of
  `--global` was not rejected. The validator now rejects that drift; its Node
  suite passes `8/8`, including pinned `skills@1.5.20`, version-derived tag,
  commit-SHA self-reference, required agents, default non-overwrite behavior,
  project/single-client command shape, and entrypoint synchronization checks.
- T010's local contract runner first stopped before mutations because the
  skeleton lacked the installation reference and synchronized entrypoints. It
  now creates only isolated child profiles below the session scratchpad and
  reuses `engram-package-sha256-v1`.
- T011–T014 completed the single package installation reference and synchronized
  English README, Chinese README, and documentation portal quick command. All
  state that the skill does not install the CLI binary or modify MCP config.
- T015 local matrix result: 6/6 single-client scope cases, 3/3 combined
  copy/symlink cases with same-version reinstallation, unknown-collision cancel,
  explicit replacement, and interruption recovery passed. Snapshot identity:
  `engram-package-sha256-v1`
  `b874276bf3714738d0647b6681faca73786c90f7da6603ef4f17ca1e5b2d63e3`.
  The runner asserted all generated HOME/XDG/npm/Claude/Codex/GitHub state was
  below the session scratchpad; repository, real home, executables, and MCP
  config mutation were `0` by construction. The final package digest will be
  refreshed after the remaining stories.
- T016 is blocked, not passed: Claude Code is available but has no recorded
  eligible cost class; Codex cannot start because its Linux optional dependency
  is missing; OpenCode is absent. No client inference was attempted.

## T017–T024 — Runtime routing contract

- T017/T018 test-first evidence: with the manifest arrays empty, the new Go
  contracts failed exactly as intended: MCP `tools/list` returned the five real
  offline tools while the manifest was empty, and CLI `knownCommands` returned
  11 real commands while the manifest was empty.
- T020 filled the machine contract from runtime facts. `CGO_ENABLED=0 go build
  ./...` and `CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram` then
  passed. The tests use in-memory MCP transport, compare both offline and
  LLM-capable tool lists, reject CLI-only fake MCP names, and compare all 11
  CLI intents directly with `knownCommands`.
- T019, T021–T023 added objective behavior definitions, exact MCP/CLI
  references, and the MCP-first single-surface workflow body. The source is
  limited to existing public adapter names and preserves namespace, explicit
  mutation, LLM, offline, store-identity, secret, and evidence semantics.
- T024 behavior A/B execution remains blocked by the same `unknown` cost class
  and unavailable client/runtime conditions recorded above. Runtime Go contract
  validation is complete; no paid model, provider, or LoCoMo run was used.

## T025–T031 — Offline and safety boundaries

- T025 first added a failing validator test for a hosted-reranker recommendation,
  a path-escaping namespace command, and omitted `missing-embedding` coverage.
  The expanded Node suite now passes `9/9` after making all three conditions
  deterministic release failures.
- T026–T029 cover offline CRUD/keyword degradation, missing-LLM ingest and
  curation, invalid namespace, secret-bearing input, content-budget rejection,
  empty/not-found, and cross-store mismatch. The workflow and references retain
  environment-only credentials, adapter content limits, and no-paid-reranker
  rules.
- T030 and T031's static Go/offline, audit, and engine-diff components will be
  rerun with the full final package. Their required with-skill/without-skill
  agent behavior execution is blocked by the recorded ineligible environment;
  no substitute paid path will be used.

## T032–T042 — Single source, trigger coverage, and CI

- T032 first added an imbalanced 24-case trigger fixture; the validator initially
  accepted it, then gained a deterministic 40%/40% positive/near-miss balance
  gate. The full Node suite passes `10/10`.
- T033–T035 now pass source validation with exactly one canonical body, one-hop
  references, manifest/runtime coverage, 17 objective behavior evals, and 24
  trigger cases (12 positive, 12 near-miss negative). Current identity is
  `engram-package-sha256-v1`
  `13282abc3e57e41196ffde16c9d19d0d2dabf97db6e28cd2357d566ec7ac8012`;
  `SKILL.md` is 103 normalized lines and 1,089 estimated tokens.
- The `skill-creator` static quick validator accepted `skills/engram`. Its
  progressive-disclosure guidance informed the 103-line core plus direct MCP,
  CLI, installation, and machine-contract references.
- T036, T037, and T038 remain blocked: no local/existing-flat-rate agent runner
  is recorded, no A/B outputs or review viewer exist, and no maintainer has
  supplied one of the required approving dispositions. The held-out trigger
  rates therefore cannot be claimed.
- T039/T040 corrected the unsupported `engram --help` claim, documented skill
  boundaries in both adapter guides, and linked the unique install reference.
- T041 adds a Node 24 CI job for package tests/source validation, isolated local
  installation matrix, and docs gates while retaining the Go and CGO=0 jobs.
  PyYAML parsed the workflow successfully. `skills-ref` is not installed, so its
  optional advisory check was not run.
- T030's static adapter test run passed for `mcpserver`, `cmd/engram`, and
  `cmd/engram-mcp`; T031's final static scan found no secret-shaped value in
  package, docs, tests, or this session scratchpad, and the engine-directory
  diff remains empty. The behavioral portions remain blocked as above.

## Final local installer and static verification — T015/T030/T031/T042/T046 evidence

- The pinned upstream installer was queried in an isolated HOME/npm cache:
  `npx --yes skills@1.5.20 add --help` accepted `--global`, repeated
  `--agent`, `--skill`, `--copy`, and the installer-level `--yes` flags. Its
  local `--list` invocation recognized the canonical package and exactly one
  `engram` skill.
- The same upstream installer then installed the local canonical source with
  `--copy --yes` into an isolated user profile and an isolated Git project,
  each with `claude-code`, `codex`, and `opencode` selected. It reported the
  shared `.agents/skills/engram` target for Codex/OpenCode and the corresponding
  `.claude/skills/engram` target for Claude Code. The four installed package
  directories compared exactly to `skills/engram/` with
  `diff -r --no-dereference`; all installer state remained below the session
  scratchpad. This validates the current command shape and paths, but does not
  replace T016/T045's real-client discovery and invocation requirements.
- Final reproducible local gates passed:

  ```text
  CGO_ENABLED=0 go build ./...
  CGO_ENABLED=0 go test -count=1 ./...
  CGO_ENABLED=0 go vet ./...
  node --test scripts/validate-agent-skill.test.mjs       # 10/10
  node scripts/validate-agent-skill.mjs --source
  node scripts/test-agent-skill-install.mjs ...           # all cases pass
  node --test docs/validation/check-docs.test.mjs         # 9/9
  node docs/validation/check-docs.mjs
  ```

- Final source identity is `engram-package-sha256-v1`
  `2e3ee019f28851082be234a684a6a1b5a294dbd98326952bc8d71e93643f136d` after
  the T043 candidate freeze (replaces `13282abc…` from the pre-candidate
  package). `git diff --check` and the engine-directory diff gate are rerun
  after this report update before handoff.
- `node scripts/validate-agent-skill.mjs --release` now passes: the four
  user-facing installation references carry the literal `engram-skill-v0.1.0`
  tag, with no `<ENGRAM_SKILL_TAG>` placeholder, mutable branch URL, or
  commit-SHA self-reference. `--source` reports the same digest.
- The local ref `engram-skill-v0.1.0` is still absent, and the read-only
  remote lookup found no matching ref. Both must be rechecked immediately
  before the authorized publication step.
- No LoCoMo run is required: this feature changes only skill, documentation,
  validation, CI, and adapter contract-test files, not retrieval, extraction,
  curation, storage, embedding, or their algorithms. Existing offline parity
  and full Go tests therefore provide the relevant invariant-by-construction
  evidence. Incremental model cost remains `0`; no provider or reranker was
  configured.

## T043 — Release candidate preparation

- Proposed predeclared tag: `engram-skill-v0.1.0` (derived from
  `references/contract.json` `skill.version` `0.1.0`). The tag exists neither
  locally nor on the remote.
- The `<ENGRAM_SKILL_TAG>` placeholder was replaced with the literal tag in
  the four user-facing surfaces only (`references/install.md`, `README.md`,
  `README.zh-CN.md`, `docs/README.md`). Spec-internal placeholders in
  `specs/020-*/` and `scripts/` are normative process documentation and were
  intentionally left unchanged; the validator scopes its release gate to the
  four user-facing files only.
- Candidate commit: `cb83667bb877111d4507f51e7e8bd0be866c955f` on branch
  `release/skill-v0.1.0`. The tag is to point at this exact commit.
- Candidate validation gates all passed on a clean engine-directory diff:
  `node scripts/validate-agent-skill.mjs --release` (ok) and `--source` (ok,
  same digest), `node --test scripts/validate-agent-skill.test.mjs` (10/10),
  `node docs/validation/check-docs.mjs` (passed), `CGO_ENABLED=0 go build
  ./...`, `go vet ./...`, and `CGO_ENABLED=0 go test -count=1 ./...` (all
  packages ok). No LoCoMo run was required or run; incremental model cost
  remains `0`.
- Not performed (external publication, requires maintainer authorization):
  merging the candidate to `master`, creating or pushing the
  `engram-skill-v0.1.0` tag (T044), remote `--list`/install smoke, and
  real-client discovery (T045).

## Remaining release gates

- T016/T045: real Claude Code, Codex, and OpenCode discovery plus explicit
  invocation remain blocked by the unavailable/broken clients and unknown
  inference cost class.
- T024/T030/T036/T038: with-skill/without-skill behavior and held-out trigger
  evaluation require a documented local or existing-flat-rate runner with zero
  incremental cost.
- T037/T042: a maintainer must record an approving human-review disposition.
- T043: candidate commit `cb83667…` is formed on `release/skill-v0.1.0`; it
  is not yet merged to `master` and the tag is not yet created.
- T044: a maintainer must authorize merging the candidate, then creating and
  pushing the exact `engram-skill-v0.1.0` tag at `cb83667…`, before remote
  installation validation.
