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
| LoCoMo category 1–4 | 1,540 | low | 1,100 | `sha256:52c853fb9c3c951329ae270ced3507d0aa2f270281766cd01e5f0174bb52af20` |
| LoCoMo category 1–4 | 1,540 | high | 3,600 | `sha256:f198ae8582e9bc57fe4ff56e84e85f324213f0d4c925246c5a2fb782275f9b17` |
| LongMemEval-S cleaned full | 500 | low | 1,100 | `sha256:dd0747bda23cb06df6a350b72d8f428f10779b613f19b8c2b83a70ed2711b298` |
| LongMemEval-S cleaned full | 500 | high | 3,600 | `sha256:02a3becdd264b6c88d23d37ed9785a473ed398267b16ea218796f6a7be0f6678` |

These manifests are configuration artifacts, not B1 results: no full answer
run, candidate artifact, score, oracle, or judge audit has been accepted.
They will be regenerated after the accompanying runner/report commits so their
clean git provenance remains exact. B0 continuity still needs its separate
legacy-retry accounting path and is not represented by these causal B1
manifests.

### Historical B1 execution hold: v6 cannot satisfy source validity

The live formal-runner check exposed a real ordering constraint in the current
plan. v6 fact hits have stable entry names and session IDs but no direct raw
Evidence lineage. A chunk quota improves raw-turn presence but does not remove
fact hits from the frozen candidate set. Treating a synthetic `legacy-entry:*`
identifier as a source would falsely claim the span/lineage guarantee required
by 022.

The runner therefore records `source_lineage_unavailable`, makes zero
answer/judge calls for that question, and emits an INVALID summary rather than
an apparent B1 score. Full 1,540/500 B1 is intentionally held until the
Ledger/projection increment makes source IDs and spans verifiable, or a
separately specified raw-turn-only baseline is approved. The temporary GPU
services used solely for the calibration were stopped and both local tunnels
were confirmed closed.

## US1 Ledger implementation verification (not a score)

**Date**: 2026-07-30

Evidence Ledger engine and MCP increments were checked offline after v7
migration, source-first ingest, merge lineage, lifecycle closure, stale-index
guards, and the additive MCP tools were added. No model endpoint, remote GPU,
LoCoMo answer call, LongMemEval answer call, judge call, or benchmark score was
used in this check.

| Check | Command / assertion | Result |
|---|---|---|
| Lifecycle checkpoint retry | `CGO_ENABLED=0 go test -count=1 ./memory -run 'TestLedgerPurgeReportsAndRetriesBusyWALCheckpoint'` | PASS — a held WAL reader yields logical purge + `ErrPurgeIncomplete`; retry checkpoints after the reader releases |
| Stale-index race | `TestEmbedderDoesNotPublishVectorAfterEvidenceTombstone` | PASS — existing vectors are removed, slow base write is rejected, and stale FTS candidates fail closed |
| Offline end-to-end closure | `TestEvidenceLifecycleSurvivesIngestMergeAndPurgeClosure` | PASS — ingest → facts → merge → tombstone → restore → purge preserves raw source semantics and deletes the dependent projection |
| Batch-lineage guard | `TestProjectionSourcesBatchLookupHasNoPerCandidateQueries` | PASS — 1,201 candidates use exactly 3 `QueryContext` calls (500-ID batches) |
| 100k scalability artifact | `BenchmarkProjectionSourcesBatch100K` | Added, not executed in this verification; it constructs 100,000 Evidence/projection/source rows and asserts exactly 200 lookup queries per iteration |
| MCP contract | `CGO_ENABLED=0 go test -count=1 ./mcpserver` | PASS — `memory_ingest_v2` stores Evidence offline; get/lifecycle paths preserve namespace isolation and do not leak purged content |
| Full offline regression | `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./...` | PASS — all listed packages passed; `internal/version` has no tests |

This closes only the US1 code-health checkpoint. Formal B1 remains unrun until
the benchmark ingestion/replay path materializes frozen candidates from the
new source-backed Ledger rather than v6 synthetic lineage.

### Post-Ledger formal source chain (no score yet)

The benchmark ingestion and formal replay path now have that missing source
chain. Each source turn is appended to the Ledger with its dataset dialogue ID,
original speaker, session time, and exact turn payload. Extraction reuses that
same immutable record; verbatim chunks are written with direct source refs.
The formal runner resolves each returned Atomic Fact projection through the
batched `ProjectionStore.SourcesByProjectionIDs` lookup and resolves the QA
gold dialogue IDs to the same Ledger IDs. It rejects a hit with a missing
projection or source before the answer call; it does not mint `legacy-entry:*`
identifiers.

| Check | Result |
|---|---|
| `TestChunkUpsertAndRetrieve` | PASS — a chunk directly references the raw `D1:1` Ledger record and rerun reuses it idempotently |
| `TestFormalCandidateSourcesUseLedgerEvidenceIDs` | PASS — ranked candidate, gold resolution, bundle, one answer call, and one judge call all use the same real Ledger Evidence ID |
| `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` | PASS |

The v6 protocol hashes above are historical pre-Ledger templates and must not
be used to run B1. A clean v7 `ledger_lossless_chunks_v2` manifest and a fresh
store are the remaining prerequisites before the first paid B1 slice; the GPU
remains stopped until those artifacts are ready.

### Post-Ledger B1 execution attempt invalidated (no score)

**Date**: 2026-07-30

The first fresh-store LoCoMo low-cap v7 attempt completed repetitions 1 and 2,
but repetition 3 became invalid before completion. The candidate artifacts for
the affected questions retained real, active direct Ledger Evidence refs; the
apparent `source_lineage_unavailable` label was therefore a runner attribution
bug triggered after a `/tokenize` preflight failure, not a source-chain break.
No B1 score, category score, paired result, or gate decision is accepted from
this run.

Two committed harness corrections follow from that audit:

1. `28da845` reports a counter/budget failure as such and no longer relabels a
   source-backed candidate as unavailable merely because no context was
   packed.
2. `bc43eca` makes each exact `/tokenize` preflight share the answer/judge
   concurrency gate. This prevents all question goroutines from overwhelming
   the tokenizer while preserving exact-input counting and fail-closed
   semantics.

Fresh v8 manifests bind the repaired runner commit and the same frozen model,
dataset, retrieval, and budget inputs:

| Benchmark | Profile | B1 protocol hash |
|---|---|---|
| LoCoMo category 1–4 | low | `sha256:db9e55eb488264173636d19e0fd0747ead5b9d0b96ea5485eb462d9553f21769` |
| LoCoMo category 1–4 | high | `sha256:c237721214a8454f7ef6a8282bf759d176a8eba0d461f853ea2dea32ceef4d15` |
| LongMemEval-S cleaned full | low | `sha256:7a8843580237ac5d301b8b29bb7f479420351966ff63830f1d241fee51368e65` |
| LongMemEval-S cleaned full | high | `sha256:8118dbffd340f934b9f87a62bb60b804183b201c8b0b74077a2ab490c0c771ea` |

The replacement run is currently blocked by evaluation infrastructure, not by
the harness: the new remote instance has no local snapshot of the frozen Qwen
answer/extraction model and has no outbound model-download access. At the
time of this record there are no vLLM processes and GPU memory use is 0 MiB;
the instance itself still needs the maintainer's provider-console stop action
if it remains billable.
