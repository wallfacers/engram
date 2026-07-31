---
title: 022 Benchmark-Parity Memory Architecture — Evaluation Record
summary: 本文记录 022 的评测协议、有效性门、运行证据、负结果与尚未完成的双基准验收；不把诊断探针写成正式分数。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [benchmark-parity-memory-architecture-evaluation]
tags: [evaluation, locomo, longmemeval, evidence-compiler]
---

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

### Full LoCoMo v12 is INVALID: repetitions rematerialized candidates

**Date**: 2026-07-30

**Protocol hash**:
`sha256:ae3cd5d2a903890c11dde1ab9d41df4d0b2d66c2b140cf8d4a1dac72990a762a`

**Runner commit**: `7acc71313fd65012d08a44f205b234d74ab0cfae`

The low-cap LoCoMo run completed all three 1,540-question answer passes with
one answer call per question. Their raw judge outcomes were
1,052/1,540 (68.31%), 1,085/1,540 (70.45%), and 1,082/1,540 (70.26%).
These are diagnostic call outcomes only. They are **not B1 scores**, are not
eligible for a majority metric, and do not enter F0: six questions violated
FR-040 because the old outer repeat loop re-ran retrieval and packing instead
of replaying one frozen Candidate/Trace/Bundle:

- `conv-0-q-52`
- `conv-1-q-11`
- `conv-1-q-13`
- `conv-3-q-42`
- `conv-6-q-57`
- `conv-9-q-11`

The preserved invalid artifacts are identified by:

| Artifact | SHA-256 |
|---|---|
| `protocol.json` file | `5d83c9eded21414ebfb5b44ef4651dcee81d8fc41157bd35bcc8a650265807bc` |
| `candidates.jsonl` | `d8b8d190446f95608c9bd0604121c8da3cd8baa857ce85cfdc40d4e76a0293b1` |
| `compile_trace.jsonl` | `c790daa41e6271007f2496d15203a1ff134f4c384032d66bab65161d82b7f097` |
| `bundles.jsonl` | `d3166a3a88e0282ea6c4e23c181a47ac758abba5f0864e01e418250c7d851f1e` |
| `classification.jsonl` | `c3e2ce0b7b28969f7d0fc3b3c8d7a11317c33466342bc66a4e88ae9d90f151be` |
| `summary.json` | `5773f18ee48a6f154aa936948de17a91f4ddc7a1b738966148457893bca610df` |

The correction separates the formal path into one materialization step and an
answer-only replay step. A strict `formal_freeze.jsonl` staging journal is
flushed and synced before the first answer call, so a restart after freezing
cannot silently retrieve again. It uses per-question singleflight rather than
serializing the 1,540 questions. Resume rejects malformed, duplicate,
cross-protocol, or digest-conflicting freeze records. Answer/judge failures no
longer mutate frozen Trace/Bundle validity, and an INVALID summary omits
`metrics` entirely so the obsolete apparent 69.74% majority cannot be mistaken
for a paper-eligible result.

External-call replay has a second durable boundary in `formal_calls.jsonl`.
After exact-input preflight, the runner syncs `STARTED` before the answer call,
makes at most one answer call and one judge call, then syncs a terminal
`COMPLETED`/`FAILED` record containing the full result before deriving the
ordinary repetition journal. Resume replays a terminal result without another
provider call and refuses an orphan `STARTED`, a result without a terminal, or
a terminal/result digest conflict. A pre-call failure may omit `STARTED` only
when both provider-call counters are zero.

The final validity gate also binds the ordered 1,540-question denominator and
question-ID digest, validates all three repetition journals, and checks the
full Candidate/Trace/Bundle cap, counter, source, answer-call, and judge-call
metadata before metrics exist. Materialization always writes a scoreless
summary; only the independent persisted-artifact validator may publish its
metrics. Per-repetition output is
`score=pending-validation`; percentages are not printed before that gate.
Formal resume also bypasses the unrelated legacy multi-query
`context_parity.jsonl` gate; its own freeze/call/result journals are the
authoritative replay contract.

Regression coverage now proves:

- a materializer that would return a different candidate on every invocation
  is called exactly once across three repetitions;
- the freeze survives process reopen before any answer result exists;
- production `answerConversationWithUsage` still completes repetitions 2 and
  3 after the source Entry is deleted following repetition 1, proving those
  repetitions do not retrieve again;
- all three answer calls receive byte-identical system/user inputs;
- answer failure cannot mutate Candidate/Trace/Bundle; and
- deliberate repetition drift or malformed frozen metadata produces an
  INVALID summary with no `metrics`;
- a stable but incomplete denominator is rejected; and
- an orphan external-call intent is refused while a synced terminal result can
  reconstruct its missing ordinary journal without another model call.

| Verification | Result |
|---|---|
| Red test before implementation | PASS as a TDD checkpoint — `TestFormalQuestionReplay*` failed to compile because the replay boundary did not exist |
| Formal replay/package tests | PASS — `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` |
| Cross-platform build | PASS — `CGO_ENABLED=0 go build ./...` |
| Full offline regression | PASS — `CGO_ENABLED=0 go test -count=1 ./...` |
| Static analysis | PASS — `CGO_ENABLED=0 go vet ./...` |
| Engine diff guard | PASS — no changed path under `memory/`, `embedding/`, `provider/`, `store/`, or `internal/` |

This repairs the B1 ruler only. It does not improve retrieval or answer quality,
does not make the v12 outcomes valid, and does not unlock T021/T022. Fresh
protocols must bind the eventual clean replay-fix commit before another formal
run. No local `locomo-bench`, vLLM, tokenizer tunnel, or evaluation SSH process
was running at the time of this audit.

### T109/T110 local prerequisite checkpoint: source-grounded B1 and fixed-gold F0

**Date**: 2026-07-30

T020 remains isolated in commit `6ae1aaa` (`fix(022): make formal repetitions
crash-safe`). This checkpoint contains only the prerequisites requested before
T021; it does not contain a benchmark run or score.

T109 now makes B1 navigation candidates answer-facing only after batch expansion
through direct projection lineage into active raw-message Evidence. Admission
counts the expanded bytes, preserves whole ranked anchor groups, and rejects
projection text, `direct_write`, `legacy_entry`, inactive sources, partial
anchors, reordered anchors, or a later anchor that only fits by skipping the
first. Bundle items carry one candidate plus one Unicode code-point span.
An independent pre-answer Ledger reread reconstructs source bytes, span digest,
candidate citation, source union, rendered answer input, and an Evidence state
receipt bound to ID/type/state/revision/content. Source, span, and citation
validity remain separate dimensions. Evidence revision drift invalidates replay
before any answer/judge call.

T110 adds a dedicated `--fixed-gold-oracle` path and dataset-aware no-model
`--eval-validate` path. The oracle accepts only the frozen three-repetition B1
`legacy_count_packer` control, with `idk_retry`, IRIS, rerank, and planner
disabled. The control recipe must be exactly plain `fts` or `hybrid`; arm
suffixes and equivalent global association, temporal, conflict, PCIC, selector,
or shadow mechanisms fail closed. It stores benchmark turn IDs as
`dataset_source_ids`, loads every gold raw-message Evidence into one untruncated
input, performs no
retrieval/extraction/embedding call, and preserves the LongMemEval abstention
empty-Evidence exception only for the registered adversarial type. Provider,
model ID/revision, prompts, exact input counter/cap, and max output tokens are
frozen. The control embedding fingerprint remains provenance only and is not a
runtime oracle dependency.

The oracle artifact, call journal, and pending INVALID summary are created
exclusively before provider use. Every answer/judge attempt has a synced intent
and terminal receipt; the formal wrapper makes no transparent retry. The
result-driven scheduler dispatches at most the configured concurrency and never
dispatches a replacement after any worker reports INVALID; already dispatched
calls remain auditable. An INVALID summary suppresses the diagnostic score and
retains actual answer/judge totals. Independent read-back rebuilds the Ledger
from the dataset, reconstructs answer and judge inputs, checks raw
verdict/majority, verifies the call journal, and requires the persisted summary
to equal the derived summary.

Protocol hardening found during independent review also freezes provider IDs and
`max_output_tokens`. The oracle is an exclusive mode: compare, calibration,
protocol freeze, retrieval diagnostics, doc2query/alias-shadow, PCIC,
abstention, coverage, and other early-return modes are rejected before their
work can start.

| Verification | Result |
|---|---|
| Source/Bundle/Unicode/prefix targeted tests | PASS |
| Fixed-gold control, overwrite, journal, tamper, full LongMemEval-500 fake-call path, and read-back tests | PASS |
| Harness package | PASS — `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` |
| Cross-platform build | PASS — `CGO_ENABLED=0 go build ./...` |
| Full offline regression | PASS — `CGO_ENABLED=0 go test -count=1 ./...` |
| Static analysis | PASS — `CGO_ENABLED=0 go vet ./...` |
| Diff hygiene | PASS — `git diff --check` |
| Engine diff guard | PASS — no changed path under `memory/`, `embedding/`, `provider/`, `store/`, or `internal/` |
| Remote/model/formal execution | NOT RUN — no SSH, GPU, vLLM, tunnel, provider, T021, B1, or F0 score run |

This checkpoint still does not unlock T021. T044 and T045 remain incomplete, and
the audit found that the current protocol freezer/runner implements only B1
while T021 also requires a real B0 continuity path. That missing path is now
tracked explicitly as T111. Until T044, T045, and T111 are complete, no formal
B0/B1/oracle execution should start.

### T111 independent B0 continuity checkpoint

**Date**: 2026-07-30

The B0 continuity path now has dedicated `--eval-freeze-b0-protocol` and
`--eval-b0-protocol` entry points. Its manifest freezes the clean commit,
dataset denominator, lossless v7 ingestion, current retrieval/packing prompts,
answerer/extractor/judge identities, three repetitions, and
`legacy_product_continuity` with IDK retry enabled. It does not accept B1
profile/cap/counter flags.

Each ordinary result carries a B0-only receipt with logical answer, query
rewrite, and judge call counts plus an explicit legacy-retry bit. These counts
measure the adaptive IDK control flow; transport retries remain part of the
current product wrapper and global cost ledger. The independent read-back path
rebuilds the denominator and summary from the dataset and three result
journals. B0 is always `promotion_eligible=false` and fails closed if any
Candidate, Trace, Bundle, classification, formal replay/call journal, or
fixed-gold artifact appears in its run directory.

| Verification | Result |
|---|---|
| Red test before implementation | PASS as a TDD checkpoint — B0 mode, call recorder, receipt, and summary symbols were absent and the targeted test failed to compile |
| B0 protocol/runner/summary/read-back integration tests | PASS |
| Harness package | PASS — `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` |
| Cross-platform build | PASS — `CGO_ENABLED=0 go build ./...` |
| Full offline regression | PASS — `CGO_ENABLED=0 go test -count=1 ./...` |
| Static analysis | PASS — `CGO_ENABLED=0 go vet ./...` |
| Diff hygiene | PASS — `git diff --check` |
| Formal execution | NOT RUN — this checkpoint makes B0 executable but does not create a score |

T111 is complete. T044 and T045 remain the two local/acceptance prerequisites
before T021 may freeze and execute B0, valid low/high B1, and fixed-gold
oracle artifacts.

### T044 storage/extraction regression slice

**Date**: 2026-07-30

The comparable slice used the first two LoCoMo conversations and all 40 of
their category 1–4 questions. Both sides used the same Qwen answer/extraction
model, BGE embedding model, DeepSeek judge, `hybrid`, lossless chunks,
`chunk-quota=7`, `top-k=30`, force-answer, aligned-judge, and disabled legacy
IDK retry. The source baseline was `d9b8916`; the Ledger candidate was
`7ee713b`.

The first baseline attempt was invalid because its session judge credential
returned HTTP 401. A later candidate diagnostic completed at 36/40 but had one
extractor JSON parse failure, one extractor transport failure, and recovered
embedding retries, so it is retained in the scratchpad but excluded from the
gate. Neither invalid attempt is reported as a comparable score.

The accepted pair completed exactly 40 results on both sides without model,
judge, retrieval, or store errors:

| Arm | Correct | Accuracy | Gate interpretation |
|---|---:|---:|---|
| Pre-Ledger baseline | 37/40 | 92.5% | Frozen comparison |
| Post-Ledger clean run, raw judge | 38/40 | 95.0% | No raw-score regression |
| Post-Ledger, conservative identical-answer correction | 37/40 | 92.5% | No regression |

One question (`conv-0-q-11`) received opposite judge labels even though both
runs produced the identical answer text `her home country`. Treating that
answer as incorrect on both sides removes the judge-only gain and leaves the
candidate exactly level with the baseline. This slice therefore establishes
non-regression; it is not a formal B1 score or a substitute for the T022 judge
audit.

| Artifact | Baseline SHA-256 | Post-Ledger SHA-256 |
|---|---|---|
| `results-hybrid.jsonl` | `eeaa7c6328b103423d58d62cecfbc2cf173617768b407dae5ad75d5b1614fed2` | `e5f991ca76bfdac68456df1d1fc4abe31ce6836185fad5ce269a7734687ad5c5` |
| `stats.json` | `351f6a45f7308d5bf00b601798826ac5fe9c7e2ab1cbf663853de30188bc0761` | `39f004d1bd1489515357b17ecd584a7557f4894b2edde9007e0632dee5b50a51` |
| `cost.json` | `04b54a40a1c85ade7231cfd8d75aa550fdaa4f3443494d72fcf9afefdaff15fb` | `0ec6807f6ed65fef4f60442e72c2baa37e772d6a629eed33282793803fcdc421` |
| `regime.json` | `64050dd583d1d3a7ab4c99b04e88a76402f815efffdf6aad78bcb4b219016461` | `64050dd583d1d3a7ab4c99b04e88a76402f815efffdf6aad78bcb4b219016461` |
| `context_parity.jsonl` | `dba4307b7567d8bcc9ce477080c71f34f017af46224e02a5ef524fd39a7e5a7f` | `8d9d2dd083cde717fc814c8578847d30e5941de416b7e8829bf6e68e8aaf329d` |

T044 is complete.

### T045 Ledger MVP independent acceptance

**Date**: 2026-07-30

The Ledger was accepted independently of Episode, Compiler, Event, or optional
projection work. The default EntryStore/Search path, source-backed projection
lookup, lifecycle closure, MCP namespace boundary, and the existing 003 graph
contracts all passed with the optional later-stage mechanisms absent.

| Check | Result |
|---|---|
| Lifecycle, stale-index, ingest/merge/purge closure, batched projection lookup | PASS — targeted `./memory` tests |
| Default retrieval parity and 003 graph contracts | PASS — targeted `./memory` tests |
| Namespace isolation, path containment, Evidence/MCP contract | PASS — targeted `./mcpserver` tests |
| LongMemEval oversized-turn preservation and repeated-DiaID coverage | PASS — three targeted `./cmd/locomo-bench` tests |
| 100k source-lineage benchmark | PASS — `373656734 ns/op`, `52998568 B/op`, `1406745 allocs/op`; the fixture asserts exactly 200 batched lookup queries for 100,000 candidates |
| Comparable answerable slice | PASS — conservative 37/40 equals the 37/40 baseline |

This closes the US1 checkpoint: every accepted active Atomic Fact remains
source-backed, lifecycle deletion fails closed in projections and side
indexes, namespace isolation remains adapter-enforced, and default write/search
continues without any later-stage representation or compiler. T045 is complete.
Together with T109, T110, and T111, this unlocks T021 protocol freezing and
formal execution; it does not itself produce B0, B1, oracle, SC-002, SC-003, or
F0 results.

## T021/T022 formal acceptance attempt: HOLD on independent B1 read-back

**Date**: 2026-07-30
**Runner commit**: `9eeec93a2d51ccd6b8784e9f557051ec32a3973b`
**Formal binary SHA-256**:
`dd90187b168f650236e5505f70f157b39a32192fe2c2d22e04fc4f17d7ff60b4`

All six manifests were frozen from the same clean worktree, with the calibrated
counter fingerprint
`sha256:13dda39a2a9b241cef10ffde7eba02943e77fdbed9c80b39f27b3ed874e66997`,
`hybrid`, `top-k=30`, `chunk-quota=7`, no reranker, and the
`ledger_lossless_chunks_v2` ingestion recipe:

| Benchmark / arm | Cap | Protocol hash | Execution status |
|---|---:|---|---|
| LoCoMo B0 continuity | unbounded legacy | `sha256:49ba0fa3a53afde56ac3a4a34168aea797375e9fea4bf507f7c2abda779ae41c` | VALID continuity only |
| LoCoMo B1 low | 1,100 | `sha256:940947283dd4cca3baf53a7fcb62e9cf46f1e720cf32d5e5fadb1f5d695cb883` | INVALID for acceptance: independent read-back rejected |
| LoCoMo B1 high | 3,600 | `sha256:a8e4c15d11d1dd3314611a0efe38dba9bacaffdf8d48c70bf21ebfe59f1cc929` | NOT RUN after hard validity failure |
| LongMemEval-S B0 continuity | unbounded legacy | `sha256:eb188aa130fae43615bc6e2358392551e58766a2f7684bbb2c4c585cfc54c82f` | NOT RUN after hard validity failure |
| LongMemEval-S B1 low | 1,100 | `sha256:81abb6289e787a0173efb6d63888ad40b726e6b953c3891052eb7326eaaa121a` | NOT RUN after hard validity failure |
| LongMemEval-S B1 high | 3,600 | `sha256:5d99d8bfb7a425efae79ceccb51e311ded0564340c4b4d6a5d3fe886825e778c` | NOT RUN after hard validity failure |

### Shared LoCoMo Ledger prebuild

The one accepted prebuild retained all degradation instead of selecting a
zero-warning run. Ten of ten SQLite databases returned `integrity_check=ok`.
The store contains approximately 5,882 Evidence records, 2,758 facts, and
1,056 chunks. Every fact/chunk has at least one source and every real Entry has
an embedding; additional embedding rows belong to the deliberate `#alias`
shadow and are not orphans.

Observed degradation was one extraction JSON parse failure, one extraction
transport EOF, ten invalid-source fact rejections, twelve embedding retries
that recovered, and zero write-behind failures. Under the Ledger-first
pipeline contract, the immutable raw Evidence and verbatim chunks remain
available when an optional fact projection fails.

| Prebuild artifact | SHA-256 |
|---|---|
| `run.log` | `a798e22d78e2f845529f6436bb82afaeb4f7b2ced86054eba38a20ca71bef4a8` |
| `coverage.json` | `d98e4c5bc738d35e812c1ffc9a9f914bf2269a6ec00e3c875719a9cf515fdd8c` |

### LoCoMo B0 continuity is valid but not promotion-eligible

Independent dataset read-back passed:

```text
eval-validate-b0: protocol=sha256:49ba0fa3a53afde56ac3a4a34168aea797375e9fea4bf507f7c2abda779ae41c majority_correct=1314/1540 valid=true promotion_eligible=false
```

The three raw repetitions were 1,297/1,540, 1,313/1,540, and 1,320/1,540.
Majority correctness was 1,314/1,540 (85.32%). The receipt records 4,627
answer calls, seven query rewrites, 4,620 judge calls, and seven questions that
used the legacy retry. B0 is historical continuity only and cannot satisfy
SC-002 or serve as a B1 control.

| B0 artifact | SHA-256 |
|---|---|
| `protocol.json` | `c1bc6852288cc115c1035eac56d55727d25b54cffe6edd5e1c69624676cde41c` |
| `b0_continuity_summary.json` | `4463576a0b65586d575cf08d09159960dc5bbf2ecc36f6579215cc682146fff8` |
| repetition 1 | `54720b9620027b1c689abc5fe1eef9ceb6bff6c7496a44d27897d96eca829725` |
| repetition 2 | `5f81f56714350a5100da3ec329d019193104028a7e11ed0130ddff994a5c36ae` |
| repetition 3 | `574c0279295082760b00b2cef5d44c5a62e44b319aeefbc06fd1b2e2210ef7ea` |

### LoCoMo B1 low completed provider calls but failed the independent gate

The runner completed all three 1,540-question repetitions. It recorded exactly
one frozen Candidate/Trace/Bundle per question, 4,620 answer results, and 4,620
paired `started`/`completed` call-journal records. The materialized artifact
reported `source_lineage_unavailable=0`, 1,540/1,540 locally valid
classifications, identical frozen inputs across repetitions, and one
answer/judge call per repetition.

Those facts do **not** make the result acceptable. The required independent
command failed:

```text
locomo-bench: expected question ID digest differs from protocol
```

The cause is deterministic:

1. `materializeFormalB1Artifacts` verifies the first journal in the frozen
   numeric dataset order, matching the protocol digest.
2. It then writes the immutable artifact arrays using lexical `mapKeys`
   ordering, producing sequences such as `conv-0-q-1`, `conv-0-q-10`,
   `conv-0-q-100`, … before `conv-0-q-2`.
3. `runEvalArtifactValidateCLI` derives its expected ordered IDs directly from
   that lexically sorted `candidates.jsonl` and compares their digest with the
   dataset-order protocol digest.

Consequently the standalone read-back cannot validate any normal multi-digit
question set written by this path. The runner-generated raw repetitions
(957/1,540, 959/1,540, and 964/1,540) and its apparent majority
967/1,540 (62.79%) are preserved only as rejected diagnostics. They are not a
B1 score, not SC-002 evidence, and must not be used for comparison or
promotion.

| Rejected B1-low artifact | SHA-256 |
|---|---|
| `protocol.json` | `d36ad6dc071df80119ac7888c038793147a971de057a61233b1eeed385ea6955` |
| `candidates.jsonl` | `66535db58fddd8c5400e0ae5f998be2f7100d0fe113b71240dc663f829e57cfa` |
| `compile_trace.jsonl` | `df70ddf995162d1c7f07d662b63e0cd16751afbc2df2703a9757283d6dc0156c` |
| `bundles.jsonl` | `be74566be4c273323f2854adfeaad675a9f42f9b39ee7483e041f8834c551bbf` |
| `classification.jsonl` | `e7ef56e4ccc6d6a81566899e31d1235b9386c00ab673ba14856d3b3569b60f39` |
| `formal_freeze.jsonl` | `6322c7a3563d04d4c535b4412dd13434aa0b4d1b1b628ecd55cbfbb3f7037d65` |
| `formal_calls.jsonl` | `332092ff44ebc1f2154e253fc64343336cee8da236a2633b2c2887afcd6bfc0a` |
| rejected `summary.json` | `aa4c10eb93ae5ffd06ce64d68a66783143e49a17ea5bab9a05cc76506eb9459b` |
| repetition 1 | `fd0deac498cbe3551656891439d26cd8c6abf3872493edfa21cf7cf0b14b72b2` |
| repetition 2 | `df5ecc8a8b39805d5e72182481b032e625e5f6f473942c62d624f2be0900fa23` |
| repetition 3 | `94778d59f01304ecb34f43f3756471fa49ed161bc8d3d5611706526a386ae875` |

Per the hard validity rule, the high-cap, fixed-gold, and LongMemEval runs were
not started after this failure. Continuing would spend model/judge resources
without producing a valid F0 input. The required two-independent-reviewer
judge audit is also incomplete: the repository contains selection,
blinding/adjudication, and summary functions, but no operational CLI or two
independent reviewer decisions. A single operator must not synthesize them.

### Acceptance status and unique F0 verdict

| Gate | Required | Accepted result |
|---|---:|---|
| SC-002 LoCoMo | at least 1,425/1,540 | **NO ACCEPTED RESULT** — B0 is continuity-only; B1 read-back failed |
| SC-003 LongMemEval-S | at least 473/500 | **NO ACCEPTED RESULT** — stopped before execution on the upstream hard-validity failure |
| B1 low/high validity | 100% independent read-back | **FAIL / INCOMPLETE** |
| Fixed-gold oracle | valid on both benchmarks | **NOT RUN** |
| Judge audit | two independent reviewers | **INCOMPLETE** |

**F0 verdict: `HOLD`.**

This is the only contract-valid verdict while an artifact hard gate and judge
audit remain incomplete. T021 and T022 remain unchecked. T046–T098 stay
locked, no mechanism/default is promoted, and the next permissible work is to
repair the independent B1 ordering contract with a failing regression test
before generating fresh protocols and fresh run directories.

Post-report branch verification passed:
`CGO_ENABLED=0 go build ./...`,
`CGO_ENABLED=0 go test -count=1 ./...`,
`CGO_ENABLED=0 go vet ./...`, and `git diff --check`.

## B1 read-back ordering contract repair

**Date**: 2026-07-30
**Scope**: `cmd/locomo-bench/eval_artifact.go` only.

The HOLD above has a single deterministic cause, now fixed locally. The
materializer verified the first journal in frozen numeric dataset order
(matching the protocol digest) but then wrote the immutable
Candidate/Trace/Bundle/classification arrays using lexical `mapKeys` ordering.
With real zero-padding-free question IDs (`conv-0-q-1` … `conv-0-q-11`) the two
orderings diverge from `q=10` onward, so the independent no-model read-back
derived its expected IDs from a lexically sorted `candidates.jsonl` and failed
the dataset-order protocol digest for every multi-digit question set — exactly
the `expected question ID digest differs from protocol` rejection recorded
above.

The fix writes the artifact arrays in the same dataset numeric order already
validated against the protocol digest (`orderedQuestionIDs`), instead of
`mapKeys(expected)`. No path under `memory/`, `embedding/`, `provider/`,
`store/`, or `internal/` changed.

A regression test reproduces the divergence before the fix.
`TestMaterializeFormalB1ArtifactsWritesNumericQuestionOrder` materializes an
11-question multi-digit set and asserts the written candidate order matches the
numeric protocol order. Before the fix it failed with
`written=[conv-0-q-1 conv-0-q-10 conv-0-q-11 conv-0-q-2 … conv-0-q-9]`; after
the fix it passes.

| Verification | Result |
|---|---|
| Regression test (red → green) | PASS |
| `CGO_ENABLED=0 go build ./...` | PASS |
| `CGO_ENABLED=0 go test -count=1 ./...` | PASS — all packages green |
| `CGO_ENABLED=0 go vet ./...` | PASS |
| Engine diff guard | PASS — no changed path under `memory/ embedding/ provider/ store/ internal/` |

This repairs the B1 ruler only. It does **not** itself produce a B0, B1, oracle,
SC-002, SC-003, or F0 result, and T021/T022 remain unchecked. The previously
rejected B1-low artifact was materialized by the lexical path and cannot be
reused: a fresh protocol must be frozen against the repaired commit, a fresh
run directory opened, and B0 continuity → valid low/high B1 → fixed-gold oracle
re-executed (LoCoMo and LongMemEval-S each) under the same recipe before the
two-independent-reviewer judge audit and the unique F0 verdict. That re-run
still requires the remote-GPU prerequisite noted above — a local snapshot of
the frozen Qwen answer/extraction model on an instance with no outbound
download access.

### 83.83% runtime probe — invalidated by Phase 8 wiring audit (2026-07-31)

**Date**: 2026-07-31

The command requested `--compiler-arm extractive` against the full LoCoMo
1,540 category-1–4 set, but it did **not** include `--eval-protocol` (per
operator record; the retained run directory independently lacks the formal
`candidates.jsonl`/`compile_trace.jsonl`/`bundles.jsonl` artifacts a formal
runner must write, and `cost.json` records 1,546 answer calls, which violates
the formal one-answer rule). Phase 8 code tracing found that `compilerArm` was
consumed only by the formal materializer; the ordinary runner silently
accepted and ignored the flag. Therefore this run did not exercise the
Compiler on a real provider. T112's offline formal-path tests remain valid,
but this runtime probe cannot extend their conclusion.

**Recipe**: answer/extract = remote vLLM `Qwen/Qwen3.6-35B-A3B-FP8` (:8000);
embedding = remote vLLM `BAAI/bge-large-en-v1.5` (:8010, full served name so
`EMBED_MODEL` matches and semantic does not silently degrade); judge =
`deepseek-v4-flash`; `--compiler-arm extractive --chunks --chunk-quota 12
--top-k 30 --retrieval hybrid --force-answer --judge-mem0-aligned --concurrency
40 --repeats 1`. No `--cat-top-k` (differs from the 85.71% row, which adds it).

**Result**: 1,291/1,540 = **83.83%** overall, single rep. Per-category:
single-hop 732/841 (87.0%), multi-hop 245/282 (86.9%), temporal 257/321
(80.1%), open-domain 57/96 (59.4%).

| Check | Result |
|---|---|
| Ordinary result errors | **0** — all 1,540 legacy-runner questions produced results; this is not Compiler Bundle validity evidence |
| Answer/rewrite calls | **1,546 / 6** — `cost.json` proves legacy retry remained active; this violates FR-024 and the formal compiler-arm one-answer rule |
| Judge calls | 1,540 (all judged); `cost.actual_usd=0` is an unpriced-model artifact (`Qwen3.6`, `bge-large`, `deepseek-v4-flash` all unpriced), not free judging |
| Cross-recipe comparability | NOT comparable to the 85.71% row (that adds `--cat-top-k` and 3-rep majority); this is a single-rep, no-cat-top-k probe |
| Compiler execution | **NO** — the non-formal runner ignored `--compiler-arm`; the CLI now rejects every treatment flag outside a formal run and rejects all formal treatment freeze/run requests until T114 implements their complete protocol |
| Formal eligibility | **INVALID as mechanism evidence** — no Compiler execution, frozen `022.v1` pair, three-repetition majority, one-answer compliance, or double-reviewer audit |

| Artifact | SHA-256 |
|---|---|
| `results-hybrid.jsonl` | `bc544c43c10349528ef39f23588fa597e5c153712cdad7c1547cb141908088bc` |
| `stats.json` | `a86b6088fb2ed73d56fb72612d2e1dfe0b653d42c012acadb934dcbc9e68c1d0` |
| `cost.json` | `3005b399e52eca2ac316acf0cf40bb0e6c5d63acda3935229deae4b41a005358` |
| retained archive | `ed806cce7f9ac7b4be06b424a4ebf00c65f5bf1afc249e744f733d71ccb1431a` |

**Interpretation**: 83.83% is only a single-repetition legacy-runner diagnostic
with a silently ignored mechanism flag. It says nothing about Compiler gain or
real-provider Bundle validity. The wiring defect is now fail-closed for the
current B1 legacy control and covered by
`TestMechanismArmsRequireFormalProtocolContext` plus
`TestFormalRunnerOptionsRequireLegacyControlAndRejectTreatments`. A fresh frozen same-store
legacy/compiler pair is still required to measure any compiler Δ. Run artifacts retained off-repo at
`~/.config/engram/022-eval-compile/`.

## Oracle recall×budget gate — can low-token hold recall? (2026-07-31)

The budget ablation showed engram's +3.20pp edge is **entirely token-driven**
(3,605 tok vs MemOS ~1,059; at 1,083 tok engram is −5.62pp). The open question
for a "low-token parity" goal: can retrieval recall be held while the evidence
budget is cut? Six `(top-k, chunk-quota)` configs were run `--coverage-only`
(retrieval only, no answer/judge — only bge query-embedding cost) on the reused
`022-full-store`. top-k is the token proxy.

| top-k | chunk-quota | ≈token | turn_recall | session_recall | n |
|---:|---:|---:|---:|---:|---:|
| 8  | 3  | ~960  | 0.596 | 0.823 | 1532 |
| 10 | 4  | ~1200 | 0.641 | 0.858 | 1532 |
| 15 | 6  | ~1800 | 0.703 | 0.905 | 1532 |
| 20 | 8  | ~2400 | 0.747 | 0.935 | 1532 |
| 30 | 12 | ~3600 | **0.808** | 0.966 | 1532 |
| 60 | 24 | ~7200 | **0.808** | 0.986 | 1532 |

Three hard conclusions:

1. **Recall saturates at k=30** (turn_recall 0.808); doubling to k=60 adds
   nothing. "Bigger top-k raises recall" is falsified — the retrievable gold is
   exhausted by k=30. (The 008/014 pattern again: deep gold does not move.)
2. **Low token forces recall loss** — token and recall are tightly bound:
   k=30→15 (token halved) −10.5pp; k=30→8 (≈MemOS ~1k tok) −21.2pp. There is no
   "low-token, same-recall" gap to exploit.
3. **The ceiling is retrieval, not token**: even k=60 caps at turn_recall 0.808
   — ~20% of questions have gold outside top-60 (echoes 009's gold-rank 71–90).

Baseline token distribution (compiler-arm chunk-quota=12, all 1,540): mean
3,605 / median 3,623 / max 5,798, narrow (p25=3,500, p75=3,726); **0% of
questions are under 2,000 tok**, i.e. 3.4× MemOS with the budget fully filled by
the 12 reserved chunk slots.

**Verdict**: "low-token parity" is unreachable inside the current chunk+RRF
architecture — cutting token cuts recall (→ cuts score), and bigger top-k does
not buy recall back. The only path to "low token AND high score" is to change
the **evidence representation** (chunk → compact fact) so the same gold is
expressed in fewer tokens, not to retune the budget. This is the aggressive arm
of 022 Increment 2 (Representation bake-off), and the direction the budget
ablation explicitly pointed at ("what should be cut is the amount of evidence
stuffed into the answerer").

## Gate A — does pure fact hold the score? (chunk-quota=0, 2026-07-31)

The pivot's first feasibility gate: if compact facts alone hold accuracy while
slashing token, the pivot is a config change, not a redesign. Ran the same
ordinary legacy runner with the ignored compiler flag at `--chunk-quota 0`
(RRF fused order — chunks drop out, fact-dominated
~30 hits) on the reused store; only answer+judge tokens spent. Single rep, same
regime as the 83.83% row (force-answer, mem0-aligned judge).

| metric | baseline chunk-quota=12 | Gate A pure-fact | Δ |
|---|---|---|---|
| overall | 83.83% | **73.70%** | **−10.13pp** |
| token mean | 3,605 | **1,529** | −58% (→1.44× MemOS) |
| single-hop | 87.0% | 70.99% | **−16.0pp** |
| multi-hop | 86.9% | 83.33% | −3.6pp |
| temporal | 80.1% | 76.01% | −4.1pp |
| open-domain | 59.4% | 61.46% | +2.1pp (noise) |

**Verdict: NO-GO for pure fact.** Accuracy drops 10pp — far outside the ±2pp
noise ruler. Token does fall sharply (3,605→1,529; 53.5% of questions now
≤1,500 tok), but 0% reach MemOS's ~1,059.

**The decisive signal is per-category**: single-hop — direct factual lookup, the
category facts should *dominate* — drops the *most* (−16pp). Extracted facts are
paraphrases/abstractions; they lose the verbatim precision single-hop answers
need, and chunk text is the carrier of that precision (cf. the lever-line
finding "199/200 gold carried by chunks"). multi-hop drops only 3.6pp (cross-turn
reasoning survives on facts). This falsifies "chunk → pure fact" as a drop-in.

**Direction confirmed by Gate A**: the pivot is **not** chunk→pure-fact. It is
chunk → **compact fact + verbatim span** — each fact carries its Evidence-source
verbatim span (the 022 Ledger already gives facts `source_ids` lineage) so
precision is preserved without the 900-char chunk bloat. The design variables are
the fact/span token mix and whether it is category-aware (single-hop needs more
verbatim span; multi-hop leans on fact reasoning). MemOS's MemCube (Payload +
provenance; "structured knowledge fragments", not raw chunks) is the analog.

## Phase 8 local gates and completion audit (2026-07-31)

### Runtime-probe correction and fail-closed CLI

Phase 8 traced every use of `compilerArm`. The formal materializer consumes it,
but the ordinary runner did not; the 83.83% command omitted `--eval-protocol`,
so the flag was silently ignored. This invalidates the earlier interpretation
that a real provider had exercised the Compiler. It does not invalidate T112's
offline formal-path coverage.

`TestMechanismArmsRequireFormalProtocolContext` first reproduced the bug for
representation, compiler and event/gap flags. The CLI is now fail-closed on
three layers: `validateMechanismArms` rejects these treatment flags outside a
formal run and rejects them during freeze before dataset loading; the B1
freezer repeats that refusal at its own boundary; and a formal run accepts only
the exact `stage=b1`, `arm=legacy_count_packer`, empty control hash and three
false control flags, with no treatment CLI option
(`validateFormalMechanismBinding`, covered by
`TestFormalRunnerOptionsRequireLegacyControlAndRejectTreatments`). No
treatment manifest is claimed runnable: bidirectional binding, contract arm
mapping, candidate replay and single-mechanism enforcement remain T114. No
engine behavior or default recipe changed.

### T102–T106 local verification

All logs were written to the session scratchpad; no dataset, DB, credential or
run artifact was added to the repository.

| Task / gate | Result |
|---|---|
| T102 documentation unit tests | PASS — 10/10 `node --test docs/validation/check-docs.test.mjs` |
| T102 022 metadata/link issues | PASS — the 022 report front matter and score-consumer duplication were fixed |
| Full docs validator | FAIL outside 022 — four pre-existing findings remain: duplicate heading + orphan/navigation for `docs/research/lever-batch-local-vs-saas.md`, and missing 021 `contracts/iris-loop.md`; no 022 finding remains |
| T103 `CGO_ENABLED=0 go build ./...` | PASS |
| T103 `CGO_ENABLED=0 go test -count=1 ./...` | PASS — all packages green; `cmd/locomo-bench` 40.764s, `mcpserver` 25.063s |
| T103 `CGO_ENABLED=0 go vet ./...` | PASS |
| T103 `git diff --check` | PASS |
| T104 v7 fresh/idempotent/backfill rollback + v3 round trip | PASS — targeted `./store` |
| T104 deterministic retrieval/signal degradation + 003 graph unchanged | PASS — targeted `./memory` |
| T104 MCP parity/schema/namespace/path containment/offline degradation | PASS — targeted `./mcpserver` |
| T104 formal Compiler shape + graph projection unchanged | PASS — targeted `./cmd/locomo-bench` |
| T105 100k lineage | PASS — 200 batched queries, 856,729,470 ns/op, 53,008,872 B/op, 1,406,751 allocs/op for one iteration |
| T105 candidate/source bounds | PASS — overflow is rejected before resolver access by `TestCompileRejectsCandidateAndSourceBoundsBeforeResolution` |
| T105 purge checkpoint stress | PASS — 20 repeated lifecycle/checkpoint runs |
| T106 MCP secret contract | PASS — configured secrets absent from logs/tool responses |
| T106 tracked artifact scan | PASS — no tracked run dir, benchmark dataset, DB/WAL/SQLite artifact |
| T106 exact live-secret scan | PASS — no current judge/remote credential value found in tracked content or Phase 8 logs |
| T106 privacy purge recovery | PASS — 10 repeated purge/closure runs |
| Optional scanner | `gitleaks` unavailable; exact-value, tracked-path and contract-test fallbacks above were used and recorded |

The 100k result proves bounded batched access rather than a latency SLO. The
allocation count and memory footprint are now documented in the public
capability boundary instead of being hidden behind “100k supported”.

### T107 preflight and blockers

Both required datasets exist locally. The mandatory no-call estimates completed:

| Benchmark | Questions | Repeats | Estimated extraction calls | Estimate interpretation |
|---|---:|---:|---:|---|
| LoCoMo category 1–4 | 1,540 | 3 | 288 | answer/judge models unpriced; USD=0 is not a real cost estimate |
| LongMemEval-S cleaned full | 500 | 3 | 23,867 | answer/judge models unpriced; the extraction volume makes an accidental run especially unacceptable |

T107 was not started. No answer/embed endpoint or `LOCOMO_*`/`EMBED_*` runtime
was available, and the remote GPU cannot be assumed billable/stopped without
provider-console confirmation. More importantly, code convergence found two
artifact blockers which must be fixed before spending model tokens:

1. the protocol freezer always labels formal output `stage=b1,
   arm=legacy_count_packer` and now refuses treatment flags outright (freeze
   with `--compiler-arm`/`--representation`/`--event-projection`/`--gap-refetch`
   errors until T114); it cannot yet bind treatment stage/arm/control hash
   or byte-replay one candidate artifact across compiler arms;
2. judge audit has deterministic selection/blinding/adjudication primitives but
   no operational packet export + two-reviewer import/finalize workflow.

Running full benchmarks before these are fixed would recreate an INVALID or
mislabeled artifact. Convergence tasks T114–T115 capture them. The separate 022
worktree's untracked `judge_audit_cli*.go` files were not overwritten or treated
as delivered code.

### T108 constitution and SC-001–SC-015 audit

| Constitution principle | Result |
|---|---|
| I. Local-first/offline | PASS — Ledger, validator, Compiler fallback and all local gates run without hosted dependencies; model side remains optional |
| II. Engine/adapter separation | PASS — Compiler/Ledger remain engine packages; this Phase 8 CLI fix does not move algorithms into adapters |
| III. Contract-first/namespace isolation | PASS — v7 is additive, namespace/path tests pass, and treatment artifacts now fail closed rather than accepting an unfrozen flag |
| IV. Evaluation regression gate | HOLD — the shipped Ledger slice/parity gate passes, but 022 mechanisms have no valid final dual-benchmark paired evidence |
| V. Graceful degradation/honest scale | PASS — offline degradation passes and the measured 100k time/memory/allocation boundary is published |

| Success criterion | Status | Evidence / missing work |
|---|---|---|
| SC-001 | PARTIAL | valid LoCoMo lossless B0 exists; refreshed LongMemEval-S B0/full per-question result does not |
| SC-002 | FAIL | no accepted 1,425/1,540 LoCoMo formal result |
| SC-003 | FAIL | no accepted 473/500 LongMemEval-S formal result |
| SC-004 | FAIL | numerical targets are not met; no cloud/reranker shortcut was used |
| SC-005 | PARTIAL | three representation implementations/tests exist; full paired artifacts and verdict do not |
| SC-006 | PARTIAL | candidate/cap/compiler bounds are tested; four-arm candidate-replay artifacts do not exist |
| SC-007 | PARTIAL | offline source/span/citation guards pass; no complete formal arm corpus proves 100% rates |
| SC-008 | GUARDED | zero unproven mechanisms are promoted; no mechanism has positive dual-benchmark promotion evidence |
| SC-009 | PARTIAL | gap/one-answer invariants have tests; no full formal run proves corpus-wide compliance |
| SC-010 | PASS (code gate) | Ledger/projection lifecycle, lineage and namespace tests pass |
| SC-011 | PASS (code gate) | optional projection/model/offline degradation tests pass |
| SC-012 | PASS | deterministic retrieval parity, error paths and namespace isolation pass |
| SC-013 | FAIL | final dual-benchmark per-question/statistics/cost/audit artifact set is incomplete |
| SC-014 | PARTIAL | replay/drift tests pass; no accepted full B1 corpus exists after the ordering fix |
| SC-015 | PARTIAL | LoCoMo B0 exists and unique verdict is HOLD; remaining B1/oracle/audit artifacts are incomplete |

Per T108, `speckit-converge` appended Phase 9 T114–T115 for the two newly
isolated infrastructure gaps. 022 remains incomplete and its unique verdict
remains **HOLD**; T107 is unchecked and no default mechanism is promoted.

### Mechanism refined by code trace (post Explore)

A full evidence-chain trace corrects the "fact loses precision" reading.
`toMemories` (main.go:2244) renders chunk and fact **identically** (`[event: …]
<Content>`), and `--compiler-arm extractive` expands **every** hit into
`raw_turn` Evidence spans (`expandFormalEvidence`, eval_source_bridge.go:99,
hard-coded `Kind: "raw_turn"`) — the answerer sees **verbatim turn text, not
fact paraphrase**, so precision is *present*. `CandidateAtomicFact` exists in
types.go:18 but is unused. Gate A's loss is therefore **coverage**, not
precision: ADD-only extracted facts cover fewer turns than verbatim chunks (one
chunk spans ~3-5 turns; one fact points only at its source turn), so
chunk-quota=0 drops the gold turns no extracted fact covers — single-hop, which
needs the exact turn, is hit hardest. The pivot's real problem: **cover as many
gold turns as chunk does, in ~1k tokens.** A hard prerequisite surfaces too —
fact has **no turn-level provenance** in `memory_entries` (only `SourceSessionID`;
dia_id is reachable only via projection→evidence joins, never activated at render
time), so any fact-as-evidence-body design must add a `factTurns` equivalent
first (consumers: evidence.go, attribution.go, coverage.go, abstain_probe.go).

## MemOS reference re-anchored + token-accuracy tradeoff confirmed (alphaXiv, 2026-07-31)

The pivot's "low-token parity" goal was premised on two MemOS anchors that the
paper (arXiv:2507.03724 v4, MemOS-1031, Table 3) **contradicts**:

| old anchor (wrong) | paper Table 3 (authoritative) |
|---|---|
| MemOS ~1,059 tokens | **1,589 tokens** |
| MemOS overall 88.83 | **75.80** (GPT-4o-mini judge, LoCoMo cat 1–4) |

Full Table 3 (GPT-4o-mini backbone): Mem0 1172tok@64.57, Memobase 2102@72.01,
Zep 2701@59.22, MemU 617@56.55, Supermemory 500@55.34, **MemOS-1031 1589@75.80**.

**MemOS §6.3 itself confirms token determines performance**: its chunk-size +
top-k ablation (Figure 9) reports *"performance steadily improving as memory
capacity increases… particularly for multi-hop and temporal reasoning."* MemOS
does not have a low-token-same-score trick — it sits on the same tradeoff curve.

**Five independent lines of evidence now agree that token-accuracy tradeoff is
fundamental, not an engram defect:**

| evidence | result | meaning |
|---|---|---|
| oracle recall×budget | recall saturates at top-k=30 (0.808) | retrieval ceiling; budget doesn't help |
| Gate A0 (pure fact) | 1529 tok → −10.13pp (73.70%) | low token forces coverage loss → score loss |
| Gate B chunk=150 q=0 | turn_recall 0.020 + 7000-chunk slow | RRF suppresses chunks regardless of size; over-fine doesn't scale |
| Gate B chunk=150 q=12 | turn_recall 0.614 (< baseline 0.808) | 12 single-turn chunks cover 12 turns << 60 turns; chunk-finening needs more slots (more token) to match recall |
| MemOS §6.3 (paper) | perf rises with token (chunk-size×top-k) | MemOS self-confirms token decides perf |
| budget-ablation | engram 3614tok→+3.2 / 1083tok→−5.6 | engram's own tradeoff is budget-driven |

**engram on the MemOS token band**: Gate A0 engram@1529tok=73.70% lands on the
same token-performance band as MemOS@1589tok=75.80% (cross-judge not strictly
comparable, but the band agrees). engram's 3605tok@83.83% is the "high-token,
high-score" point of the **same tradeoff curve** MemOS rides — not an engram
defect. "Low token AND hold 83.83%" is unreachable on this curve; the budget
ablation already showed engram@1083tok=−5.62pp.

**Implication for the pivot**: chunk→compact-fact (Gate A0) and chunk-granularity
finening (Gate B) both fail to break the tradeoff — they only move along it. The
"low-token parity" goal needs re-scoping against the corrected MemOS anchors.

## evidencecompiler 分层结构改造（refactor, 2026-07-31）

**分支**: `refactor/evidencecompiler-structure`（基线 022 `f4d58a4`）

将扁平单包 `memory/evidencecompiler`（13 文件 ~2,900 行）重构为分层结构，公开合同
与行为不变：

```
memory/evidencecompiler/
├── export.go        # 公开合同 alias 面（类型/常量/Err* re-export + BuildNeed wrapper）
├── compiler.go      # 门面：New/Compile/Compiler + compileConfig
├── orchestrate.go   # 有状态编排：planner proposal/admission/finalize/trace
└── internal/
    ├── contracts/   # 冻结合同类型/常量/sentinel errors（唯一类型源，无行为）
    ├── need/        # deterministic Need/relations（纯函数）
    ├── validate/    # canonical validation + digest（纯校验）
    ├── extract/     # EvidenceItem/ExtractionPlan/raw-fit/EXTRACT/MERGE 决策（纯决策）
    ├── render/      # Bundle/Trace rendering + bundle 校验（纯渲染）
    └── resolve/     # LedgerResolver + 批量 source resolution（IO 层）
```

依赖方向单向无环：`contracts → validate → need/extract/render/resolve → 顶层编排`。
`internal/` 的 Go import 规则禁止 eval harness 绕过顶层合同直接接触实现层。

| 验证 | 结果 |
|---|---|
| 公开 API 形状 | PASS — 63/63 导出符号（type/const/var/func）与重构前逐项一致（alias + wrapper + var re-export；含 `LedgerResolver`、`Err*`、`BuildNeed`） |
| `CGO_ENABLED=0 go build ./...` | PASS |
| `CGO_ENABLED=0 go test -count=1 ./...` | PASS — 全包绿；`cmd/locomo-bench` 8.392s 通过，证明 eval harness 对 alias 合同编译兼容 |
| `CGO_ENABLED=0 go vet ./...` | PASS |
| `gofmt -l memory/evidencecompiler` | 干净 |
| 行为不变性 | 无算法/默认 recipe 改动；`compiler_test.go` 等原测试原样通过；T021/T022 状态不变（F0 仍 HOLD） |
| 评测回归门 | 纯结构重构，测试全绿 + API 形状一致证明不变性；正式 LoCoMo 回归仍归 T021/T022/T107 既有计划，不额外引入风险 |
| tasks.md 路径同步 | T011、T059-T068 的文件路径已更新到新分层结构 |

本改造只服务结构清晰度，不产生 B0/B1/oracle/SC-002/SC-003 分数，不改变任何
mechanism 的 promotion 状态。US3 的 T059-T068 验收口径（行为/失败测试）不受影响。

## 022 Formal Acceptance Run（2026-08-01）

**运行环境**：云机器 vLLM（Qwen/Qwen3.6-35B-A3B-FP8 answer/extract、BAAI/bge-large-en-v1.5 embed）、DeepSeek judge（deepseek-v4-flash）。代码 commit `23190da`（clean worktree）。counter fingerprint `sha256:4806660…`（fixture delta=0）。数据集 `testdata/locomo/locomo.json`（即官方 locomo10.json，10 对话 / 1986 题 / 1540 可答）。

### LoCoMo 正式结果（3 reps majority correctness，全部 `--eval-validate` valid=true）

| Arm | Budget cap | 总分 | multi-hop | temporal | open-domain | single-hop | answer tokens mean |
|---|---|---:|---:|---:|---:|---:|---:|
| B0 continuity（legacy, IDK retry） | unbounded | **1307/1540 = 84.9%** | 85.8% | 80.8% | 61.5% | 86.6% | 3599 |
| B1 low（formal, 无 IDK retry） | 1100 | **914/1540 = 59.4%** | 181/282=64.2% | 203/321=63.2% | 55/96=57.3% | 475/841=56.5% | 1042 |
| B1 high（formal, 无 IDK retry） | 3600 | **1278/1540 = 83.0%** | 240/282=85.1% | 254/321=79.1% | 58/96=60.4% | 726/841=86.3% | 3406 |

- B0 `answer_calls=4628 judge_calls=4620`（含 legacy IDK retry），`promotion_eligible=false`。
- B1 low/high 均满足 frozen protocol：`idk_retry/iris/rerank=false`、一次 answerer、candidate replay、source/span/citation 100%（validity.valid=true）。artifact hashes 见各 `summary.json`。
- 分类别以 `summary.json by_category` 为准（与运行时 OVERALL mean 口径略异：前者题级 majority，后者 rep 均值）。

**关键观察**：B1 high（83.0%）与 B0（84.9%）差距 1.9pp 是关闭 IDK retry 的 formal 代价；B1 low（59.4%）相对 B1 high **-23.6pp**——低预算 1100 token 严重损害，再次印证 token-accuracy 权衡（`docs/` 中 arch-pivot 记录）。SC-002（≥1425/1540）**未达**。

### LongMemEval-S 与 fixed-gold oracle 未完成（外部中断）

LME B0 的 lossless ingestion（500 conversations，~23.9k extraction calls，LongMemEval 长对话 prefill 慢）运行约 5 小时后被**并发的 024 feature agent 中断**：vLLM 被 kill、`lme-store` 被改名隔离为 `lme-store-corrupted-1785513478`（500 DB 完整保留）、`watch-lme.sh` 被禁用、进程被终止。oracle 先因 vLLM 满载下的第 3 次 repetition 超时失败（answer_failed/judge_failed）多次，随后同样被中断。**LME 双基准未出分，SC-003 未测。**

### 024 并发冲突记录

另一 agent（024）复用同一台云机器，从 022 b1-high baseline 派生 density-arm protocol manifests（`024-control/dedup/neighbor/both`，仅 mechanism_flags 增写去重/邻居扩展键），停用 022 的 vLLM 与 watch。022 与 024 需协调机器/资源与 baseline 归属。本记录未删除/未恢复 024 的任何文件。

### 结论

- LoCoMo 侧有 **valid 的 B0/B1 正式结果**（首次在 022.v1 protocol 下产出）。
- 022 整体 verdict：**HOLD**——双基准（LongMemEval 缺）未完成，SC-002/SC-003 未达，任何 mechanism 未 promotion。
- LoCoMo 低预算硬伤（B1 low -23.6pp）确认后续机制路线应聚焦"~3.6k 预算下提质"而非压缩 token 保分。
