# 033 Preflight Receipt

**Frozen**: 2026-08-10, before any 033 Go behavior change or answer/judge call.

## Repository state

- HEAD: `03281ceaed6025c97fc1679fd1952f39ea9e7066`
- Worktree: `.claude/worktrees/033-chunk-first-contract-repair`
- Expected dirty state at freeze: modified `.specify/feature.json` and untracked
  `specs/033-chunk-first-contract-repair/`; no tracked Go source change.
- Sibling worktree `.claude/worktrees/031-evidence-relation-assembly` remained untouched at `0ea3740`.
- `git diff --name-only -- memory embedding provider store internal`: empty.

## Frozen data and store

- Dataset: `/home/wushengzhou/workspace/github/engram/testdata/locomo/locomo.json`
- Dataset bytes: `2,805,274`
- Dataset SHA-256: `79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4`
- Store: `/home/wushengzhou/workspace/github/engram/.locomo-run/009-bge-chunks-store`
- Store shape: `conv0.db` through `conv9.db`, 10 SQLite files, `71,536,640` bytes total.
- Sorted `sha256sum *.db` manifest SHA-256:
  `d3b8bd4ebc18090f112a78b85d141fe511fb05aef109e3b37386dee20879d772`.

## Frozen cohorts

| File | Lines | Unique | SHA-256 |
|---|---:|---:|---|
| `target-32.txt` | 32 | 32 | `2f0ed8586c8648b1fcfecc95db512fdfcd0e1e77813bc2d83ed599ace7531f4b` |
| `guard-32.txt` | 32 | 32 | `864bdff5115c0bd93a135cf8ae0d8e7490ac42776abf3e8ba500d0197119b581` |
| `probe-64.txt` | 64 | 64 | `3ac0efc5ccbaa2e677eee3b97c1f0cc5bb11f59f8af30d82c297c0fc36237eba` |

`probe-64.txt` is byte-for-byte the concatenation of target then guard (`cmp` exit 0).

## Pre-change test baseline

Command:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Test(Assemble|Assembly)'
```

Exact output:

```text
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.004s
```

The baseline is green but does not cover the missing contract. Existing
`TestAssembleChunkFirst` exercises single-hop only. There was no multi-hop
canonical chunk-before-fact test, stable SourceID tie-break test,
`EvidenceAssembly.Units` ↔ prompt sequence test, canonical-prefix cap test,
private-label poison test, or benchmark-only legacy mode test.
