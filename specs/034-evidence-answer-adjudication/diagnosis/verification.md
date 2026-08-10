# 034 Final Verification

**Date**: 2026-08-10
**Result**: PASS for implementation/integrity; paid Stage-0 sealed successfully and its promotion verdict is NO-GO.

## Build and tests

All Go commands used `CGO_ENABLED=0`.

```text
$ go build ./...
exit=0

$ go test -count=1 ./cmd/locomo-bench
ok   github.com/wallfacers/engram/cmd/locomo-bench 52.391s

$ go test -count=1 ./...
exit=0
ok   github.com/wallfacers/engram/cmd/engram 4.940s
ok   github.com/wallfacers/engram/cmd/engram-mcp 0.015s
ok   github.com/wallfacers/engram/cmd/locomo-bench 66.044s
ok   github.com/wallfacers/engram/cmd/planner-build 0.060s
ok   github.com/wallfacers/engram/embedding 0.273s
ok   github.com/wallfacers/engram/internal/idgen 0.020s
ok   github.com/wallfacers/engram/mcpserver 35.417s
ok   github.com/wallfacers/engram/memory 6.889s
ok   github.com/wallfacers/engram/memory/curation 1.551s
ok   github.com/wallfacers/engram/memory/eventstore 0.027s
ok   github.com/wallfacers/engram/memory/evidencecompiler 0.018s
ok   github.com/wallfacers/engram/memory/pipeline 0.567s
ok   github.com/wallfacers/engram/memory/prompt 0.008s
ok   github.com/wallfacers/engram/provider 0.031s
ok   github.com/wallfacers/engram/provider/anthropic 0.273s
ok   github.com/wallfacers/engram/provider/openai 0.212s
ok   github.com/wallfacers/engram/store 0.785s
```

Packages reported as having no test files and the evidencecompiler internal packages also exited successfully; the
complete log is retained in session scratch. `git diff --check` produced no output.

The ordinary no-adjudication dispatch regression was also run directly:

```text
=== RUN   TestAdjudicationModeValidationLeavesOrdinaryCLIUnselected
--- PASS: TestAdjudicationModeValidationLeavesOrdinaryCLIUnselected (0.00s)
PASS
ok   github.com/wallfacers/engram/cmd/locomo-bench 0.004s
```

## Frozen artifact and source safety

Final public validation:

```text
adjudication valid: protocol=sha256:9b840473b0c1fef8c5c0f97a55c5cde6fb7fa771efb8103ff74a526aa99efb19 questions=1540 triggered=771 context_parity=1532 triggered_context_parity=766
```

- The final 32-concurrency offline stub completed twice with 771 attempts, 1540 decisions, strict seal validation, and
  byte-identical decisions/seals. See `offline-seal-receipt.md`.
- The actual offline score CLI joined the custody-matching candidate journals only after seal validation and reproduced
  1371 majority, 1368 text control, 1411 oracle, 96/88 mixed rows, and 13/5 instability. See
  `offline-score-receipt.md`.
- All ten source DB SHA-256 values still exactly match `custody.json`; the source directory contains zero `-wal` or
  `-shm` files.
- `git diff --name-only -- memory embedding provider store internal` produced no output: engine changes = 0.
- The touched-source/spec secret scan for key-shaped `sk-...` values produced no matches.
- The paid run revalidated the same public protocol, executed exactly 771 one-attempt calls at concurrency 32, emitted
  1540 decisions, and produced a valid seal. It recorded 718 accepted selections, 53 triggered fallbacks, zero retries,
  and 4,310,100 / 26,590 input/output tokens with `unpriced` cost status.
- Post-seal scoring reproduced every frozen denominator and returned NO-GO at 1378/1540 and 61/88 triggered mixed.
  See `paid-run-receipt.md` and `verdict.md`.

## Scope disposition

T001–T035 are complete. T036 is complete as a conditional N/A: T035 was NO-GO, so the feature stopped without creating
or running a formal paired-rejudge protocol. The fixed-C1 offline stub remains only a scorer test; the paid Stage-0
NO-GO in `verdict.md` is the feature verdict.
