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

## Formal B1 protocol freeze (scores pending)

**Date**: 2026-07-30

The formal runner now uses one hybrid retrieval arm, lossless chunks with a
12-slot raw-chunk quota, `top-k=30`, the current force-answer + aligned-judge
prompt regime, and no IRIS, reranker, multi-query, filter or legacy IDK retry.
It preflights the complete answer input with the calibrated counter and packs
the legacy rank-order context to the exact cap before the sole answer call.

The calibration artifact is retained only in the session scratchpad; its
immutable SHA-256 is
`b54af313931ae1cf8c6ec6d03454014ba7165e4238c8d0d255692c3ef5040a89`.
It contains eight fixture counts and all preflight/runtime deltas are zero.

| Benchmark | Denominator | Profile | Exact cap | B1 protocol hash |
|---|---:|---|---:|---|
| LoCoMo category 1–4 | 1,540 | low | 1,100 | `sha256:d72e8a9646ec6f582ef0ff0c577faa32e4d785b51236884315621d5b17d59b94` |
| LoCoMo category 1–4 | 1,540 | high | 3,600 | `sha256:968992bf3592dc929b991109930920443caaba7e6a9873f3614388a37045e2cd` |
| LongMemEval-S cleaned full | 500 | low | 1,100 | `sha256:db823cc6a65d698a9a9619fe767799f21111eb363a8e5b803f29c1c72c83af77` |
| LongMemEval-S cleaned full | 500 | high | 3,600 | `sha256:721a95e82c8c3cfd08f764cf8ba1bcd88030fe05b339edee412ad886e59c7368` |

These manifests are configuration artifacts, not B1 results: no full answer
run, candidate artifact, score, oracle, or judge audit has been accepted.
They will be regenerated after the accompanying runner/report commits so their
clean git provenance remains exact. B0 continuity still needs its separate
legacy-retry accounting path and is not represented by these causal B1
manifests.
