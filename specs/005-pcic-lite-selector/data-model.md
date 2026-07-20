# Phase 1 Data Model: PCIC-lite Span Selector

All types live in the `cmd/locomo-bench` package. None are persisted to the engine store;
`pcic_meta` is a bench sidecar JSON. Field names below are conceptual — Go structs use the
package's existing JSON tag conventions.

## SpanClaim (pcic_meta record)

One typed claim per raw dialogue turn, produced by the one-time offline annotation pass.

| Field | Type | Notes |
|-------|------|-------|
| `span_id` | string | the dialogue-turn id (`dia_id`); primary key |
| `entity` | string | normalized subject entity (`memory.EntityNorm` form) |
| `slot` | string | attribute/relation name (e.g. `job`, `location`, `owns_pet`) |
| `value` | string | the slot's value |
| `polarity` | enum `affirm`\|`negate` | whether the claim asserts or denies the value |
| `time_state` | string | coarse temporal state label (e.g. `past`, `current`, or a period key) — distinguishes state changes |
| `source_turn_ids` | []string | turn ids this claim is grounded in (usually `[span_id]`; may include coref antecedents) |

**Validation**: `span_id` non-empty and unique; unknown/failed annotations are simply absent
(the selector treats absent spans as role "unknown"). No schema migration — this is a sidecar.

**Sidecar file**: a JSON map `{ span_id → SpanClaim }` (plus a small header with the annotate
model + dataset fingerprint for cache validation), written under the run/store dir, gitignored.

## CandidateWindow (per query, transient)

The selector's working set for one query.

| Field | Type | Notes |
|-------|------|-------|
| `chunks` | []memory.Result | top-60 chunk candidates by rerank score (from the reranked `wide` list) |
| `factSlots` | []memory.Result | the 18 fact results, passed through unchanged |
| `chunkTurns` | map[string][]string | chunk-name → covered dialogue-ids (existing map) |
| `demandAtoms` | []DemandAtom | query's required entity/slot atoms |
| `budget` | int | chunk slots = 12 |
| `tokenCeiling` | int | baseline 12-chunk token count for this query (budget-neutral cap) |

## DemandAtom (per query, transient)

| Field | Type | Notes |
|-------|------|-------|
| `entity` | string | normalized query entity token (`memory.EntityQueryTokens` + `EntitySignalsForQuery`) |
| `slot` | string \| "" | slot if inferable from the query; empty when only the entity is known |
| `satisfied` | bool | set true once a selected chunk covers this atom (drives complement priority) |

## ChunkRole (per candidate, transient)

Derived by aggregating the `SpanClaim`s of a chunk's covered turns relative to the query and
the already-selected set. A chunk may carry several role flags.

| Role | Meaning | Selector effect |
|------|---------|-----------------|
| `anchor` | one of the top-2 pointwise-rerank chunks | always kept (anchor_violation invariant) |
| `duplicate` | same entity+slot+value+time_state as an already-selected chunk | collapsed to highest score |
| `state_conflict` | same entity+slot, different value/time_state vs a selected chunk | never collapsed — both kept |
| `complement` | covers an unmet demand atom | prioritized to fill remaining slots |
| `lure` | high-confidence same-entity + wrong-slot + no unmet demand | suppressed (only when confident) |
| `unknown` | no `pcic_meta`, or ambiguous | non-penalizing — falls back to rerank order |

**State transition (selection loop)**: start with all top-60 chunks unselected → protect
anchors → collapse duplicates → for remaining slots, prefer `complement` chunks (updating
`DemandAtom.satisfied`), never dropping a `state_conflict`, suppressing only confident
`lure`s → fill leftover slots by rerank score → stop at 12 chunks or `tokenCeiling`.

## CoverageMetrics (per arm, reported)

Emitted alongside the existing `turn_recall` / `session_recall` in `coverage.json`.

| Field | Type | Definition (see research R5) |
|-------|------|------------------------------|
| `selection_survival` | float | selected_gold_turns / candidate_gold_turns |
| `complement_drop` | float | fraction of questions dropping an in-window gold turn |
| `anchor_violation` | int | questions where a top-2 anchor chunk is missing from the selection (must be 0) |

## Relationships

- `SpanClaim.source_turn_ids` ⟶ dialogue turns ⟶ (via existing `chunkTurns`) ⟶ `CandidateWindow.chunks`.
- `DemandAtom` (from query) drives `complement` role on `CandidateWindow.chunks`.
- Oracle selector ignores `SpanClaim`/`DemandAtom`; it selects purely by gold-turn coverage
  to produce the ceiling reported next to the real selector.
