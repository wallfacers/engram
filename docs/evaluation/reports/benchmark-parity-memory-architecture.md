# 022 Benchmark-Parity Memory Architecture — Evaluation Record

## Pre-algorithm code-health baseline

**Date**: 2026-07-30
**022 implementation base**: `d9b8916` (`eval(021): close retrieval-side 真实超越 line — IRIS US1 + graph A both NO-GO`)  
**022 planning commit**: `feb6ba0531ff4d364941ddeac156a9e715f8ace3`

This is a source-health checkpoint before any 022 storage, retrieval,
extraction, compiler, or benchmark algorithm change. It is **not** a LoCoMo or
LongMemEval score, a B0/B1 artifact, or a promotion verdict.

| Check | Command | Result |
|---|---|---|
| Cross-platform build | `CGO_ENABLED=0 go build ./...` | PASS |
| Offline full test suite | `CGO_ENABLED=0 go test -count=1 ./...` | PASS — all listed packages passed; `internal/version` has no test files |
| 003 retrieval/graph contract | `CGO_ENABLED=0 go test -count=1 ./memory -run '^(TestRetrievalParity|TestUpsertEdgesNormalizesAccumulatesAndQueriesBothDirections|TestEntityDocFreqAndDepthTwoWalkUseIDFAndDepthLimit|TestWalkEntityGraphDoesNotEchoVisitedSeeds|TestEntityClusterEntriesIncludesSeedAndOneHopCoSyn|TestEntityEntryScoresCapsDenseEntitySetsByScore|TestAssociativeSearchSurfacesGraphOnlyEntry)$'` | PASS |
| Namespace isolation and path boundary | `CGO_ENABLED=0 go test -count=1 ./mcpserver -run '^(TestNamespacesAreIsolatedAndDefaultIsIndependent|TestNormalizeNamespace|TestNamespaceDatabasePathStaysInDataDir|TestToolsRejectPathLikeNamespacesWithoutCreatingOutsideFiles)$'` | PASS |

## Answerer tokenizer calibration (not B0/B1)

**Date**: 2026-07-30
**Purpose**: verify the planned 022 preflight counter against the same vLLM
chat runtime before any formal context cap or benchmark score is frozen.

The local-only SSH tunnel to the temporary evaluation GPU served
`Qwen/Qwen3.6-35B-A3B-FP8` with thinking disabled. The `/tokenize` request used
the same two-message `system` + `user` shape and generation prompt as the
OpenAI-compatible answer call. For each fixture, its preflight count exactly
matched the answer response's `usage.prompt_tokens`:

| Fixture | Preflight tokens | Runtime prompt tokens | Delta |
|---|---:|---:|---:|
| CJK | 39 | 39 | 0 |
| Emoji | 45 | 45 | 0 |
| Numbers and timestamp | 71 | 71 | 0 |
| Chat-role boundary | 37 | 37 | 0 |

The frozen counter fingerprint for this runtime is
`sha256:13dda39a2a9b241cef10ffde7eba02943e77fdbed9c80b39f27b3ed874e66997`.
It derives from vLLM version, the served model's tokenizer/config file digests,
and the disabled-thinking chat-template setting; it contains no endpoint or
credential.

No exact B1 cap, protocol manifest, candidate artifact, score, or judge audit
has been frozen yet. They remain dependent on completing the formal runner
integration and then running the full 1,540/500 denominators.
