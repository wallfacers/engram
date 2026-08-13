# Validation Report: Portable Memory Evidence Guidance

**Date**: 2026-08-13

**Status**: All non-model implementation and release-candidate gates passed;
model-backed Skill behavior run handed off, not run in this change.

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
correctly excluded from the distributable package.

## Deliberately not run

No model-backed memory score or Skill-agent behavior comparison was run. The
user assigned execution to another Agent. Three portable `evidence-*` behavior
cases are checked into `skills/engram/evals/evals.json`; the reproducible
baseline is commit `35b45c1`. Until that separate run is reviewed, these changes
claim protocol and contract correctness only, not measured answer-quality gain.

## Release state

The repository Skill content is updated, but this report does not claim that a
new immutable Skill release tag has been published. Publication should occur
only after the separate behavior review accepts the candidate.
