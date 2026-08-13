# Validation Report: Portable Memory Evidence Guidance

**Date**: 2026-08-13

**Status**: All implementation and release-candidate gates passed; a small
candidate smoke passed after one general fix, while the paired behavior/score
evaluation remains handed off and is not run in this change.

## Delivered contract

- `memory-evidence-guidance/v1` is present in MCP initialization, the engram
  Skill workflow, and its standalone reference.
- Skill frontmatter remains activation-focused; the evidence policy lives in
  progressively disclosed body/reference content.
- All registered tools expose descriptions and standard side-effect hints.
- `memory_search` additively exposes ranked-subset scope, effective limit,
  returned count, and existing public result provenance.
- No answer-generation tool or required endpoint was added.

## Verification

```text
CGO_ENABLED=0 go test -count=1 ./mcpserver
ok github.com/wallfacers/engram/mcpserver

CGO_ENABLED=0 go test -count=1 ./...
PASS (all packages)

CGO_ENABLED=0 go build ./...
PASS

CGO_ENABLED=0 go vet ./...
PASS

jq empty skills/engram/evals/evals.json
PASS

git diff --check
PASS

git diff --name-only -- memory embedding provider store internal
<empty>
```

The MCP test suite includes in-memory initialization, tool discovery,
annotations, offline CRUD/degradation, namespace isolation, public provenance,
and direct `Retriever.Search` order/content parity.

The Skill Creator static validator also returned `Skill is valid!`. A candidate
`engram.skill` archive was generated outside the repository and inspected with
Python's standard zip tooling. It contains `SKILL.md`, the license, and all five
runtime references including `evidence-guidance.md`; development eval files are
correctly excluded from the distributable package. The final post-smoke package
SHA-256 is
`de04efcd91881aef9630bdd247a80fc549c8f06615e03a73ec23adafaadf305d`.

## Deliberately not run

No model-backed memory score or Skill-agent behavior comparison was run. The
user assigned execution to another Agent. Three portable `evidence-*` behavior
cases are checked into `skills/engram/evals/evals.json`; the reproducible
baseline is commit `35b45c1`. Until that separate run is reviewed, these changes
claim protocol and contract correctness only, not measured answer-quality gain.

## Small candidate behavior smoke

After the implementation gates, three candidate-only cases were run with
Codex CLI `0.147.0`, model `gpt-5.6-sol`, reasoning effort `none`, read-only
sandboxing, ephemeral sessions, no memory tool calls, and the checked-in Skill
plus `evidence-guidance.md`. This is a product-behavior smoke, not a score or a
control/treatment comparison.

| Case | First result | Outcome |
|---|---|---|
| Similar entity / missing attribute | Kept Alex Chen separate from Alex Cheng and did not transfer the allergy | PASS |
| Ranked subset list | Returned the two supported preferences and warned that more may exist at the limit | PASS |
| Current state with missing event time | Incorrectly said the user no longer worked at Northwind | FAIL |

The failure exposed a general real-world ambiguity: the candidate prohibited
using `created_at` as event time but did not explicitly prohibit treating search
rank or result-array order as event order. The contract was clarified across
Skill, MCP instructions, documentation, and tests: search rank, array order, and
`created_at` do not establish event order; a state change without event time or
an explicit sequence cannot supersede a dated state.

The failed case was then rerun under the same configuration. The answer kept the
current employer unresolved and explained that the departure lacked an event
date: PASS. Final smoke result after the general fix: **3/3 PASS**.

An earlier attempt to run the same three cases through the local Claude CLI did
not reach inference: all calls returned `401 OAuth access token has been
revoked`. Those calls are infrastructure failures and are excluded from the
behavior result. The separate Agent-owned paired evaluation remains necessary;
this single-trial candidate smoke supports no statistical or comparative claim.

## Release state

The repository Skill content is updated, but this report does not claim that a
new immutable Skill release tag has been published. Publication should occur
only after the separate behavior review accepts the candidate.
