# 033 Post-Implementation Verification

**Date**: 2026-08-10  
**Feature verdict**: NO-GO; no full-set promotion run.

## Passed

- `CGO_ENABLED=0 go build ./...`
- `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` — `ok`, 38.891s
- `CGO_ENABLED=1 go test -race -count=1 ./cmd/locomo-bench -run
  'Test(MultiHopCanonical|MultiHopPrompt|MultiHopCap|AssemblyAudit|AnswerPathWritesRuntimeAssemblyAudit)'` — `ok`
- `CGO_ENABLED=0 go vet ./...`
- `test_offline_order_analyze.py` — 5/5
- `test_probe_analyze.py` — 5/5
- `bash -n specs/033-chunk-first-contract-repair/run033.sh`
- `git diff --check`
- `git diff --name-only -- memory embedding provider store internal` — empty
- tracked diff secret-pattern count — 0; the supplied key was injected through no-echo stdin, unset, and never
  written to repository files or logs
- checklist `requirements.md` — 16/16 complete
- `.specify/extensions.yml` — absent; no before/after implementation hook exists

The first attempted race command combined `CGO_ENABLED=0` with `-race` and failed exactly with:

```text
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
```

`quickstart.md` was corrected to use `CGO_ENABLED=1` for the race-only command; all normal build/test gates remain
CGO-disabled.

## Full-suite failure (outside the changed surface)

`CGO_ENABLED=0 go test -count=1 ./...` ran and failed in an untouched engine package:

```text
--- FAIL: TestSemanticClusterCrossSessionGroupsRelatedEvidence (0.01s)
    semantic_cluster_test.go:90: unrelated evidence leaked into episode:
        [01KZN985G7404G07CJDGC0A18E 01KZN985G73K8THJFDRZT5CFHK 01KZN985G6YC5BJ57C3VF5QQ35]
FAIL
FAIL github.com/wallfacers/engram/memory 6.426s
```

A focused `CGO_ENABLED=0 go test -count=10 ./memory -run
'^TestSemanticClusterCrossSessionGroupsRelatedEvidence$'` reproduced the same assertion in 2/10 iterations. No
file under `memory/` or any other engine directory was changed, so 033 does not attempt to mask or fix this
pre-existing nondeterministic engine test. The feature-specific package, race, vet and analyzer gates are green,
but the repository-wide hard gate is faithfully recorded as not fully green.

## Quickstart reconciliation

- Offline contract commands: executed; feature package and race gates pass.
- Retrieval-only 64-question diagnostic: executed on legacy/treatment and repeated on a post-WAL store copy;
  64/64 sorted assembly records and input closures were identical to the pre-attempt treatment artifact.
- Paid probe: executed with valid smoke and aggregate concurrency 32; exact 192/54/192 coverage, then NO-GO.
- Full-set section: intentionally not executed because the paid probe failed its pre-registered target gate.
- All scratch stores, logs, results and summaries remain under `~/.claude/session-scratch/`; none is in the repo.
