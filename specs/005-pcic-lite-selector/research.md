# Phase 0 Research: PCIC-lite Span Selector

All unknowns from the plan's Technical Context are resolved below. No open
NEEDS CLARIFICATION remains.

## R1 — Query demand atoms without a query-time LLM or engine change (Constitution II)

**Decision**: Derive demand atoms and candidate-chunk entities from the engine's existing
**public** deterministic entity API; add nothing to the engine.

- Query side: `memory.EntityQueryTokens(query) []string` (pure, deterministic) plus
  `(*EntryStore).EntitySignalsForQuery(ctx, query) ([]cues, counts, err)` give the
  normalized entity tokens a query demands — the demand atoms.
- Candidate side: `(*EntryStore).EntitiesByEntry(ctx, names) map[string][]string` and
  `EntitiesOf` return the entities attached to each candidate entry, and
  `memory.EntityNorm` normalizes for matching.

**Rationale**: These are already public and used by the retriever's entity signal; they are
LLM-free and offline. The bench composes them — it does not reimplement or modify engine
logic. Constitution II holds with no engine diff.

**Alternatives considered**: (a) a bench-local tokenizer/NER — rejected: duplicates engine
logic and risks diverging normalization; (b) exposing a new engine entry — rejected:
unnecessary, and adapter work must not expand the engine contract when the public API already
suffices.

## R2 — Does the rerank pool cover the top-60 chunk candidate window?

**Decision**: Yes. Use the already-reranked `wide` list from
`SearchWithDiagnostics(query, widePool≥300)`; partition to chunks; take the top-60 by rerank
score as the selector's candidate window. No change to rerank depth.

**Rationale**: `Retriever.rerank` sets `pool := max(rerankPool, k)` and reranks up to `k`
candidates; the bench passes `k = widePool ≥ 300`, so all ~300 fused candidates (facts +
chunks) are cross-encoder-scored before `applyChunkQuota`. The top-60 chunks are therefore
fully reranked. Empirically confirmed by the free headroom probe (rerank recall@48 chunks =
0.917 multi-hop is only reachable if ranks 13–48 were reranked).

**Alternatives considered**: forcing a dedicated 60-deep rerank pass — rejected: redundant,
the existing 300-wide rerank already covers it.

## R3 — Offline typed-claim annotation source

**Decision**: A one-time `--pcic-annotate` pass in the bench, backed by the existing
`provider.Provider` (relay model `gpt-5.6-luna`, the frozen 003 model), extracts per-turn
typed claims (`entity/slot/value/polarity/time_state`) and records `source_turn_ids`. Output
is cached to a `pcic_meta` JSON sidecar and reused (like `--store-dir`). The annotation prompt
template lives **in the bench package**, not in `memory/prompt/`.

**Rationale**: The offline pass is a build-time cost paid once (~¥5–15 for 10 conversations),
keeping query-time LLM-free (FR-007). Keeping the prompt in the bench honors the engine
zero-change gate (the engine's `prompt/` package is off-limits for adapter work). Using the
frozen model keeps annotations comparable to the extraction regime.

**Alternatives considered**: (a) heuristic/NER-only annotation — rejected by the user's choice
of one-time LLM annotation for full slot/value/polarity fidelity; (b) adding a template to the
engine `prompt/` package — rejected: touches the engine.

## R4 — Oracle 12-chunk selector formulation

**Decision**: Greedy maximum-coverage set-cover: repeatedly pick the candidate chunk that
adds the most uncovered gold turns until 12 chunks or all gold turns are covered.

**Rationale**: Coverage of gold turns by chunks is a monotone submodular set function, so
greedy is a (1−1/e) approximation in general; but because multi-hop gold is mean 3.1 / p90 5
turns and packs into few chunks (all ≤12), greedy reaches full candidate-pool coverage in
practice — an exact realizable 12-slot ceiling. It reuses the existing chunk→dialogue-id map
and gold-turn labels from the coverage grader.

**Alternatives considered**: exact ILP — rejected: unnecessary given the small gold sets;
greedy is optimal here and far simpler.

## R5 — Safety-metric definitions (coverage gate)

**Decision**: Compute, per arm, over gradeable questions:

- `selection_survival` = selected_gold_turns / candidate_gold_turns — of the gold turns
  present anywhere in the top-60 candidate window, the fraction the selector keeps in its 12.
- `complement_drop` = fraction of questions where a **necessary complementary** gold turn
  (a gold turn that was in the candidate window) is dropped from the selected set — i.e.
  the question's selected gold-turn coverage is strictly below its candidate gold-turn
  coverage. Gate: ≤5% (complement recall ≥95%).
- `anchor_violation` = count of questions where one of the top-2 pointwise-rerank chunk
  candidates is absent from the selected set — MUST be 0.

**Rationale**: These make the selector's asymmetric behavior falsifiable and directly encode
SC-003/SC-004. They reuse the existing exact-turn provenance (chunk `dia_id` mapping), so no
new labeling is needed.

**Alternatives considered**: the external proposal's fuller suite (`actionable_miss`,
`lure_false_alarm`, same-cue stress set with `d'`) — deferred by the user's "lean MVP" scope
decision; recorded as future work once the coverage gate is cleared.

## R6 — Reader/eval regime for the post-gate paid step

**Decision**: Reuse the existing paired-arm answer path unchanged — `--retrieval
hybrid+rerank,hybrid+rerank+pcic` produces `paired.json` with McNemar automatically (same
machinery proven in the rerank eval). Same reader, same answer prompt, same per-query token
ceiling; the only variable is the `+pcic` selection stage.

**Rationale**: Holding the reader and prompt fixed isolates the selector's independent
increment, matching the rerank eval's口径. No new eval code is required beyond the arm
mechanism.

**Alternatives considered**: a bespoke selector-answer harness — rejected: the existing paired
+ McNemar path already isolates a single-variable contrast.
