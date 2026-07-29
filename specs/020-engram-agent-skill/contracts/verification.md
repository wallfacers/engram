# Contract: Verification and Release Evidence

**Feature**: `020-engram-agent-skill`

No single check is sufficient. A release candidate passes only after all gates below complete with
recorded evidence.

## Gate 1: Deterministic package validation

Planned files:

```text
scripts/validate-agent-skill.mjs
scripts/validate-agent-skill.test.mjs
```

Use Node.js 24 standard library only. Tests construct isolated valid/invalid fixtures before the
validator is trusted. The validator checks:

- exactly one canonical `skills/engram/SKILL.md`;
- exact two-field YAML frontmatter;
- name, directory and description constraints;
- complete-file line count and `engram-body-token-estimate-v1` budget;
- `engram-package-sha256-v1` golden cases for path order, CRLF/CR, root symlink/copy equivalence,
  internal-symlink rejection and extra-file drift;
- direct relative references exist and do not escape the package;
- required package tree, license and eval files;
- `contract.json` schema, unique/sorted names and full intent coverage;
- eval schema, unique IDs and required scenario coverage;
- at least 20 trigger cases with balanced positive/near-miss negatives;
- no obvious secret-shaped fixture values;
- no client-specific canonical body copies;
- README/docs quick commands are byte-equivalent after whitespace normalization;
- release-mode literal tag is exactly `engram-skill-v<contract.json skill.version>` and mutable branch or
  commit-SHA self-reference is rejected;
- no unreplaced `<ENGRAM_SKILL_TAG>` in user-facing package/README/docs commands when running release
  validation mode; specification contract examples are excluded.

`skills-ref validate skills/engram` is an optional advisory cross-check only when its exact distribution
and version have already been pinned and cached. Its absence is not a release blocker. If it is run and
fails, release is blocked until the package is corrected or the maintainer records a version-specific
incompatibility disposition backed by the current open specification; the repository validator remains
authoritative for engram-specific contracts.

## Gate 2: Runtime surface contract

Planned files:

```text
mcpserver/skill_contract_test.go
cmd/engram/skill_contract_test.go
```

`mcpserver` test:

- starts offline and LLM-capable servers with the official in-memory MCP transport;
- calls real `tools/list`;
- compares the always and conditional sets to `references/contract.json`;
- proves no CLI-only fake tool is exposed.

`cmd/engram` test:

- reads the package manifest;
- compares its CLI set directly with package-local `knownCommands`;
- asserts every intent maps to a real command or explicit MCP-only/conditional path;
- reuses existing offline CRUD and CLI/direct retriever parity tests.

The tests do not parse Go source strings and do not export a new product API.

## Gate 3: Isolated installer matrix

Planned file:

```text
scripts/test-agent-skill-install.mjs
```

The runner creates all state under a caller-provided session scratch directory. It isolates HOME, XDG,
temp, npm, Claude, Codex and GitHub CLI paths and uses the local canonical package for ordinary CI.

Matrix:

| Case | Clients | Scope | Mode | Repeat/conflict |
|---|---|---|---|---|
| A1–A3 | one per client | project | copy/default | first install |
| A4–A6 | one per client | user | copy/default | first install |
| B1 | all three | project | symlink | first + same source twice |
| B2 | all three | user | symlink | first + same source twice |
| B3 | all three | user | copy | first + same source twice |
| C1 | all three | user | symlink | unknown existing target, cancel |
| C2 | all three | user | symlink | explicit managed replacement |
| C3 | all three | user | copy | simulated interruption + recovery |

Assertions:

- exact paths and one discovered name per client;
- same version and `engram-package-sha256-v1` digest after successful combination or repeat;
- unknown target unchanged after cancel;
- no MCP config, executable path, real home or repository mutation;
- no stale source file after upgrade;
- partial state never labeled overall success.

Remote release smoke adds one case using the exact published predeclared tag URL and pinned installer.
Before installation, the runner proves that the tag resolves to the recorded candidate commit and that
the remote package produces the candidate digest.

## Gate 4: Real-client discovery

Before declaring support, run the released candidate separately with current stable:

- Claude Code;
- Codex;
- OpenCode.

For each client and both scopes, record:

```json
{
  "client": "codex",
  "client_version": "...",
  "scope": "user",
  "source_tag": "...",
  "skill_version": "0.1.0",
  "digest_algorithm": "engram-package-sha256-v1",
  "content_digest": "...",
  "discovery_path": "...",
  "discovered_count": 1,
  "explicit_invocation": "$engram",
  "loaded": true,
  "execution_cost_class": "local",
  "incremental_model_cost": 0,
  "tool_dependency_state": "intentionally unavailable for discovery-only case"
}
```

At least one all-three combination environment is also verified. Client discovery, not installer exit 0,
is the pass condition. Before invocation, `execution_cost_class` must be `local` or
`existing-flat-rate`; `metered` or `unknown` must block execution. Authentication, an ineligible billing
path or a missing local client may block a release smoke, but none may be replaced with an unverified
support claim or temporary pay-as-you-go credentials.

## Gate 5: Skill behavior and trigger evaluation

`evals/evals.json` contains objective expectations for representative workflows. For each selected eval,
run both:

- `with_skill`;
- `without_skill` baseline.

Store iteration outputs outside the source repository in the session scratchpad. Grade objective
expectations, aggregate `benchmark.json`, run an analyst pass and generate the skill-creator static review
viewer. Before running, record an eligible `local` or `existing-flat-rate` execution cost class, runner,
model when visible, call count and zero incremental cost. The maintainer reviews examples before a
revision is accepted and records exactly one disposition:

- `approved-no-comments`;
- `changes-requested`;
- `approved-after-changes`.

No response is `blocked`, not an empty-feedback approval. `changes-requested` requires another iteration;
Gate 5 passes only with `approved-no-comments` or `approved-after-changes`.

Minimum behavior assertions:

- only real MCP/CLI names;
- one selected surface and namespace;
- no duplicate write or cross-store merge;
- explicit mutation only;
- accurate empty/not-found/degraded/capability failure;
- no secret persistence or reflection;
- offline local path retained;
- no paid service recommendation;
- no metered evaluation path or incremental model cost.

`trigger-evals.json` contains at least 20 realistic positive and negative requests. Acceptance follows
SC-009: all explicit engram positives load; indirect persistent-memory positive rate is at least 90%;
near-miss false-positive rate is at most 10%. Improve description using held-out results rather than
memorizing the test strings.

## Gate 6: Documentation and repository regression

Run:

```bash
node --test scripts/validate-agent-skill.test.mjs
node scripts/validate-agent-skill.mjs
node --test docs/validation/check-docs.test.mjs
node docs/validation/check-docs.mjs

CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram ./cmd/engram-mcp
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go vet ./...

git diff --name-only -- memory embedding provider store internal
git diff --check
```

Expected:

- every command exits 0;
- engine diff command prints nothing;
- docs portal, root READMEs and package install reference agree;
- no metered engram provider, hosted reranker/recall model or pay-as-you-go client path is configured;
- recorded incremental model cost is zero;
- LoCoMo is not run because engine behavior is unchanged by construction.

## Release evidence

Implementation creates `specs/020-engram-agent-skill/validation-report.md` containing:

- installer and all three client versions;
- predeclared immutable release tag, candidate commit, digest algorithm and package digest;
- outputs for gates 1–4 and 6;
- behavior benchmark summary and final approving human review disposition for gate 5;
- explicit engine-diff and zero-incremental-cost ledger;
- any skipped check with reason. A skipped required real-client smoke means release is not yet complete.
