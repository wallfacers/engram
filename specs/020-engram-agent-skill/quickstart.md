# Quickstart: Implement and Validate the engram Agent Skill

**Feature**: `020-engram-agent-skill`

**Audience**: implementers and release reviewers

Run from `/home/wallfacers/project/engram`. All generated install homes, npm caches, eval workspaces and
client transcripts must live in the current session scratchpad, never in the repository root, real home
or system `/tmp`.

## 1. Confirm isolation and prerequisites

```bash
git worktree list
git status --short
git log --oneline -15
git diff --name-only -- memory embedding provider store internal

go version
node --version
npm --version
git --version
```

Expected:

- active feature pointer names `specs/020-engram-agent-skill`;
- no unfamiliar overlapping edits;
- engine diff is empty;
- Go is 1.25.x;
- Node is at least 22.20.0 (CI baseline: 24.x).

Provide an existing session scratch directory:

```bash
test -n "${ENGRAM_SESSION_SCRATCH:-}" || {
  echo "set ENGRAM_SESSION_SCRATCH to this session's scratchpad directory" >&2
  exit 2
}

ENGRAM_SKILL_SCRATCH="${ENGRAM_SESSION_SCRATCH}/feature-020"
export ENGRAM_SKILL_SCRATCH
mkdir -p "${ENGRAM_SKILL_SCRATCH}"
```

Do not assign a temporary value to `HOME`, `CODEX_HOME` or system-wide options in the parent shell.
The install test runner creates a child environment with isolated values.

## 2. Implement the package in dependency order

Create these source artifacts first:

```text
skills/engram/SKILL.md
skills/engram/LICENSE
skills/engram/references/contract.json
skills/engram/references/mcp.md
skills/engram/references/cli.md
skills/engram/references/install.md
skills/engram/evals/evals.json
skills/engram/evals/trigger-evals.json
```

Then add:

```text
scripts/validate-agent-skill.mjs
scripts/validate-agent-skill.test.mjs
scripts/test-agent-skill-install.mjs
mcpserver/skill_contract_test.go
cmd/engram/skill_contract_test.go
```

The package contract is [skill-package.md](./contracts/skill-package.md); do not invent client-specific
copies while implementing installation tests.

## 3. Run package tests before trusting the validator

```bash
node --test scripts/validate-agent-skill.test.mjs
node scripts/validate-agent-skill.mjs --source
```

Expected:

- negative fixtures demonstrate failure for bad frontmatter, escaping/missing references, stale
  command names, malformed evals and secret-shaped values;
- the canonical package passes;
- exactly one canonical `engram` body is reported;
- source mode may allow the documented `<ENGRAM_SKILL_TAG>` placeholder until release-candidate mode;
- reported line/token metrics use `engram-body-token-estimate-v1`, and package identity uses
  `engram-package-sha256-v1`.

If an Agent Skills reference validator's exact distribution/version is already pinned and cached:

```bash
skills-ref validate skills/engram
```

Record its version and treat the result as advisory format evidence, not a replacement for engram-specific
validation. Absence is not a blocker; an executed failure blocks release until corrected or explicitly
disposed as a version-specific incompatibility against the current open specification.

## 4. Verify the actual MCP and CLI surfaces

```bash
CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram ./cmd/engram-mcp
```

Expected:

- offline MCP lists exactly five always-on tools;
- LLM-capable MCP additionally lists `memory_ingest`;
- CLI manifest set exactly equals `knownCommands`;
- nonexistent MCP names such as `memory_curate` fail the contract fixture;
- existing offline, parity and namespace tests remain green.

## 5. Run the isolated local installation matrix

```bash
node scripts/test-agent-skill-install.mjs \
  --scratch "${ENGRAM_SKILL_SCRATCH}/install-matrix" \
  --source ./skills/engram \
  --installer-version 1.5.20
```

Expected summary:

```text
single-client project/user: 6/6 pass
combined project/user: 2/2 pass
copy and symlink/fallback: pass
same-version reinstall: one discovered skill per client
unknown collision cancel: original digest unchanged
explicit replacement: pass
interruption recovery: all final digests equal
real home/repository/MCP config mutation: 0
```

Afterward:

```bash
git status --short
git diff --name-only -- memory embedding provider store internal
```

No install artifact may appear under the repository unless it is an expected tracked source change.

## 6. Draft, benchmark and review behavior

Use the `skill-creator` workflow with `skills/engram/evals/evals.json`:

1. Record the runner/model and prove `execution_cost_class` is `local` or `existing-flat-rate`; stop on
   `metered` or `unknown`.
2. Launch each selected eval with the skill and without the skill in the same iteration.
3. Store all outputs under `${ENGRAM_SKILL_SCRATCH}/engram-workspace/`.
4. Grade objective expectations and aggregate `benchmark.json`.
5. Generate the static review viewer with the skill-creator `generate_review.py`.
6. Obtain an explicit maintainer disposition: `approved-no-comments`, `changes-requested`, or
   `approved-after-changes`. No response is blocked, and `changes-requested` requires another iteration.
7. Repeat until the behavior contract passes with a final approving disposition, without overfitting, and
   record zero incremental model cost.

Run the 20+ queries in `trigger-evals.json` against the final description. Expected:

- every explicit engram query triggers;
- indirect persistent-memory positives trigger at least 90%;
- RAM/cache/generic-database/transient-context near misses trigger at most 10%;
- no negative query causes a state-changing engram call.

Keep benchmark workspaces and viewer HTML in the session scratchpad; only the reusable eval definitions
belong in the package.

## 7. Validate documentation integration

```bash
node --test docs/validation/check-docs.test.mjs
node docs/validation/check-docs.mjs
node scripts/validate-agent-skill.mjs --source
```

Expected:

- `README.md`, `README.zh-CN.md` and `docs/README.md` normalize to the same canonical quick command;
- CLI/MCP guides link the package install reference without duplicating its full matrix;
- the CLI guide no longer claims `engram --help` is a complete command reference;
- all local links and docs metadata pass.

## 8. Prepare an immutable release candidate

Derive the unused release tag from `contract.json`'s skill version before final package content is
frozen. For version `0.1.0`, the required literal is:

```bash
ENGRAM_SKILL_TAG="engram-skill-v0.1.0"
export ENGRAM_SKILL_TAG

if git show-ref --verify --quiet "refs/tags/${ENGRAM_SKILL_TAG}"; then
  echo "release tag already exists" >&2
  exit 1
fi
if git ls-remote --exit-code --tags origin "refs/tags/${ENGRAM_SKILL_TAG}" >/dev/null 2>&1; then
  echo "remote release tag already exists" >&2
  exit 1
fi
```

Replace every product-doc `<ENGRAM_SKILL_TAG>` placeholder with this literal tag before creating the
candidate commit, then run candidate validation:

```bash
node scripts/validate-agent-skill.mjs --release
```

Expected: no placeholder, mutable branch URL, commit-SHA self-reference, unpinned installer or divergent
quick command. Record the validator's `engram-package-sha256-v1` digest.

Create the exact candidate commit through the maintainer-approved release workflow and record its full
SHA. Creating or pushing the tag is an external publication action: stop unless the maintainer explicitly
authorizes it. After the maintainer publishes the predeclared tag at that candidate commit, verify the
binding before any remote install:

```bash
ENGRAM_SKILL_CANDIDATE="<full-candidate-commit>"
test "$(git rev-parse "${ENGRAM_SKILL_TAG}^{commit}")" = "${ENGRAM_SKILL_CANDIDATE}"
git ls-tree -r --name-only "${ENGRAM_SKILL_TAG}" -- skills/engram

npx --yes skills@1.5.20 add \
  "https://github.com/wallfacers/engram/tree/${ENGRAM_SKILL_TAG}/skills/engram" \
  --list
```

Expected: exactly one discovered skill named `engram`, and the tag's remote package digest equals the
candidate digest. A full commit SHA remains external evidence; it is never written back into the package.

## 9. Real-client discovery smoke

Use three fresh child environments rooted under `${ENGRAM_SKILL_SCRATCH}/clients/`; never install into
the maintainer's normal profiles.

Before invocation, record `execution_cost_class`. Continue only for `local` or `existing-flat-rate`; do
not create pay-as-you-go credentials for the smoke.

For each current stable client:

1. Run the exact release command for project scope.
2. Restart/open a new session.
3. Invoke:
   - Claude Code: `/engram`
   - Codex: `$engram` or select it from `/skills`
   - OpenCode: explicitly ask it to load and use the `engram` skill
4. Confirm one discovered skill with the expected version and `engram-package-sha256-v1` digest.
5. Repeat in user scope.
6. Record client version, actual path, invocation, cost class, incremental cost `0` and outcome.

Then install once with all three agent targets in one isolated user environment and repeat discovery.
Installer exit 0 without client discovery is a failure.

If authentication, an eligible zero-incremental-cost path or a client binary is missing, record the case
as blocked; do not mark that client supported until the smoke is actually run.

## 10. Full regression gate

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go vet ./...

node --test scripts/validate-agent-skill.test.mjs
node scripts/validate-agent-skill.mjs --release
node --test docs/validation/check-docs.test.mjs
node docs/validation/check-docs.mjs

git diff --name-only -- memory embedding provider store internal
git diff --check
```

Expected:

- every command exits 0;
- engine diff output is empty;
- no metered provider/client path was configured and recorded incremental model cost is zero;
- no LoCoMo run was needed because engine behavior is unchanged.

Write all gate results and any honest blocker to
`specs/020-engram-agent-skill/validation-report.md`. A skipped real-client smoke is a release blocker,
not a passing result.
