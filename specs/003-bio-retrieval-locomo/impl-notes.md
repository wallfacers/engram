# Feature 003 Implementation Notes

## Batch 3.5 Known Limitations

- When `EMBED_RERANK_MODEL` is enabled, the cross-encoder rerank can override
  temporal multipliers, and entity-neighbor expansion can reintroduce entries
  removed by `TemporalHardFilter`. The current Strike 2 evaluation environment
  does not enable reranking; this interaction is intentionally deferred.
- `--abstain-prompt` was a no-op in the answer path before US5. Batch 7 wires it
  to the Abstain-R1 regime (`answerPromptForRegime`); see below.

## Batch 7 US5 — Abstention and Conflict Resolution (offline)

Engine + bench implemented offline (zero API cost); the Strike 3 judgment (T036)
and any effect on scores remain maintainer-gated.

- **Supersede/Unsupersede** (`memory/entrystore.go`): non-destructive suppression
  via a dedicated `UPDATE ... superseded_by` — `Upsert`'s `ON CONFLICT` clause
  deliberately never touches `superseded_by`, so a separate method is required.
  Validation: unknown loser → `store.ErrNotFound`; self-reference → `ErrSupersedeSelf`;
  pinned loser → `ErrSupersedePinned`; unknown winner → wrapped error.
- **Retrieval downweight** (`memory/retriever.go` `applySupersededPenalty`): applied
  after fusion. No-op at the zero value (parity) and at `>= 1` (no downweight).
  Temporal-intent queries (`ParseTemporalIntent`) are exempt — a superseded fact
  is often the correct answer to a time-scoped question.
- **Test placement deviation from T030**: the lifecycle test lives in
  `entrystore_test.go` (`TestSupersedeLifecycle`), but the retrieval downweight /
  temporal-exemption tests live in `retriever_test.go`
  (`TestRetriever_SupersededPenalty*`) where the retriever harness is — cleaner
  than the task's suggested single-file placement, same coverage.
- **Curation four-class** (`judge.go` `ConflictDecision`, `worker.go` apply order
  merge → conflicts → evict; `prompt/curation_judge.go`): a contradictory pair
  emits `{loser,winner}` and is suppressed, never merged or evicted. Legacy
  three-class judge output (no `conflicts` key) still parses.
- **Conflict-only build pass** (`curation.Worker.ResolveConflictsPass`): clusters
  near-duplicates store-wide and applies ONLY supersede decisions — reusing the
  full `curate()` would also evict/merge and confound the arm. The bench runs it
  once per store when the `conflict` mechanism is on; superseded markers are inert
  for arms that leave the penalty at zero, so a shared paired store stays valid.
- **Bench wiring** (`cmd/locomo-bench`): `--conflict-resolution` +
  `--superseded-penalty` (default 0.3, only bites under conflict resolution),
  `hybrid+conflict` and `hybrid+abstain` arm suffixes now supported.
- **T035 Abstain-R1** (`runner.go` `abstainAnswerPrompt` + `answerPromptForRegime`):
  a scoring-convention (口径) change — **must be committed separately** from the
  T032–T034 algorithm work (constitution IV attribution). Mutually exclusive with
  `--force-answer` (already guarded by `validatePromptModes`). Fabrication /
  false-refusal are already captured by the existing adversarial (category-5)
  refusal scoring; no new report field was needed.

## Batch 5 Answer-Plan Isolation

- The `+tplan` arm suffix enables the temporal reasoning answer prompt only for
  category 2 questions. Non-temporal categories use byte-identical prompts in
  the paired arms.
- Evaluate this isolation only from paired category 2 results. It must not be
  generalized to the full benchmark score.
