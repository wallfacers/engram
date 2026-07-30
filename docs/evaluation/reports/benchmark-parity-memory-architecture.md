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
