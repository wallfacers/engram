# Feature Specification: PCIC-lite Span Selector

**Feature Branch**: `005-pcic-lite-selector`

**Created**: 2026-07-20

**Status**: Draft

**Input**: User description: "在 gte-rerank 之后、chunk-quota 截断之前插入一个保守的词典序 span-selector，从固定 top-60 raw chunk 候选中选 12 个（facts 18 名额不变），用离线预计算的 pcic_meta 做 duplicate/lure/state-conflict/complement 的非对称处理，目标是把 reranker top-12 未 surface 的多跳桥接 gold 捞进 12 名额。先免费测 selector-vs-oracle-vs-(hybrid+rerank) 覆盖增益，过覆盖闸再花钱答题。引擎与正式 schema 零改。"

## Context & Motivation *(informative)*

The rerank lever (DashScope `gte-rerank-v2`) was confirmed on a paid paired eval:
multi-hop answer accuracy 61.0% → 69.1% (+8.1pp, McNemar p=0.0025, above-noise). A
free coverage probe over that new baseline (`hybrid+rerank`) found large residual
headroom the pointwise reranker does not capture: at a fixed 18-fact budget, multi-hop
exact-turn recall is 0.642 at 12 chunk slots but 0.917 at 48 chunk slots (+27.4pp). The
same jump appears in the no-rerank arm, so it is a property of the base chunk ranking,
not a rerank artifact: multi-hop **bridge evidence** is weakly pointwise-relevant, so
neither fusion nor the pointwise reranker surfaces it into the top-12. Budget is ample —
multi-hop questions need mean 3.1 / median 3 gold turns, 100% ≤12 turns, and turns pack
into fewer chunks — so a 12-slot oracle selector ceiling ≈ recall@60 ≈ 0.92+ (+~28pp
multi-hop). This feature tests whether a **structure-aware span selector** can recover a
meaningful fraction of that ceiling as an **independent increment over `hybrid+rerank`**,
gated free-first on evidence coverage before any paid answer eval.

This is an evaluation-harness (adapter) feature living entirely in `cmd/locomo-bench`.
It adds **no** engine capability and changes **no** production schema.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Free coverage gate for the selector (Priority: P1)

As a memory-retrieval maintainer, I want to measure whether the PCIC-lite selector, at a
fixed 12-chunk budget, recovers gold evidence that the pointwise reranker leaves below
its top-12 — reported as an evidence-coverage delta against the correct `hybrid+rerank`
baseline and against an oracle upper bound — so that I can decide, **without spending any
answer/judge tokens**, whether the selector clears the coverage gate and earns a paid
answer eval.

**Why this priority**: This is the MVP. It is the cheapest decisive test: if the selector
cannot beat `hybrid+rerank` on evidence coverage, it cannot help answers, and it is
refuted for free. It also produces the oracle ceiling that bounds all downstream value.

**Independent Test**: Run the coverage-only bake-off with three arms
(`hybrid+rerank`, `hybrid+rerank+pcic`, oracle) over the LoCoMo stores and read the
per-category exact-turn recall matrix plus the gate metrics — no answer LLM calls.

**Acceptance Scenarios**:

1. **Given** the prebuilt LoCoMo stores, a reranker endpoint, and a precomputed
   `pcic_meta`, **When** the coverage bake-off runs the three arms, **Then** it reports
   exact-turn recall for each arm (overall + per category) and the metrics
   `selection_survival`, `complement_drop`, `anchor_violation`, with **zero** answer/judge
   tokens spent.
2. **Given** an arm suffixed `+pcic`, **When** retrieval runs, **Then** the selector picks
   12 chunks from the fixed top-60 reranked candidate window while the 18 fact slots and
   the per-query chunk token ceiling are unchanged from the `hybrid+rerank` baseline.
3. **Given** the oracle arm, **When** it selects, **Then** it uses gold turn labels to pick
   the coverage-maximising 12 chunks, establishing the realisable 12-slot ceiling.

---

### User Story 2 - Offline metadata annotation, query-time LLM-free (Priority: P1)

As a maintainer, I want the per-span typed claims (`entity/slot/value/polarity/
time_state/source_turn_ids`) produced by a **one-time offline** annotation pass and cached
to a sidecar file, so that the selector reasons over roles at query time **without any
query-time LLM call** and the cost is paid once and reused across every selector iteration.

**Why this priority**: The selector's asymmetric role logic is impossible without typed
claims, and the "free-first" promise only holds if annotation is a one-time cached cost,
never per-query.

**Independent Test**: Run the annotation subcommand once over the dataset; confirm it
writes a `pcic_meta` sidecar keyed by span; re-run the coverage bake-off twice and confirm
the second run performs no annotation and no query-time LLM calls.

**Acceptance Scenarios**:

1. **Given** raw dialogue, **When** the annotation pass runs, **Then** it writes a sidecar
   file of per-span typed claims and does not modify any engine store or production schema.
2. **Given** an existing `pcic_meta` sidecar, **When** the coverage bake-off runs, **Then**
   it loads the sidecar and issues **no** LLM calls during retrieval/selection.
3. **Given** a missing sidecar or a span with no annotation, **When** the selector runs,
   **Then** that chunk's roles are treated as "unknown", it is neither promoted nor
   penalised, and selection degrades to rerank-score order (never fails whole).

---

### User Story 3 - Paid paired answer eval, only after the gate (Priority: P2)

As a maintainer, once the selector clears the free coverage gate, I want a paired
`hybrid+rerank` vs `hybrid+rerank+pcic` answer eval (multi-hop pilot first) with McNemar
significance, so that I learn whether the coverage gain converts to answer accuracy — the
necessary-but-not-sufficient check — before adopting the selector.

**Why this priority**: Coverage is necessary but not sufficient; only a paid answer eval
confirms real gain. It is P2 because it is explicitly gated behind US1 and must not run if
the coverage gate fails.

**Independent Test**: With the gate passed, run the paired answer eval on the multi-hop
slice and read `paired.json` (McNemar p, flips, verdict).

**Acceptance Scenarios**:

1. **Given** the coverage gate passed, **When** the paired answer eval runs, **Then** it
   emits per-arm accuracy and a McNemar verdict for `hybrid+rerank` vs `hybrid+rerank+pcic`.
2. **Given** the coverage gate **failed**, **When** the maintainer reviews results, **Then**
   the recorded decision is to stop with zero answer tokens spent.

---

### Edge Cases

- **State conflict vs duplicate**: two candidates share entity+slot but differ in
  value/time_state (a fact that changed over time). The selector MUST NOT collapse them as
  duplicates — both are kept as a conflicting-state pair.
- **Complementary evidence for a bridge**: a multi-hop question needs two spans that are
  individually weak but jointly necessary. Dropping either must be counted as a
  `complement_drop`; the gate caps this.
- **Over-eager lure suppression**: an ambiguous same-entity candidate is uncertain between
  lure and genuine bridge. The conservative gate MUST leave it unpenalised rather than risk
  dropping a genuine bridge (the failure mode that dragged associative retrieval negative).
- **Fewer than 60 candidates / fewer than 12 chunks**: the selector fills what is available
  and never exceeds the budget or the token ceiling.
- **Non-multi-hop categories**: single-hop/temporal/open-domain must not regress; the
  anchor protection and conservative gate exist to bound this.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The harness MUST expose a `+pcic` per-arm mechanism composable with the
  existing `+rerank` mechanism, so that `hybrid+rerank` and `hybrid+rerank+pcic` run as a
  paired contrast in one process; arms without `+pcic` MUST be byte-for-byte unchanged.
- **FR-002**: The selector MUST operate between the rerank stage and the chunk-quota
  truncation, selecting up to 12 chunks from a fixed top-60 reranked chunk candidate window
  while leaving the 18 fact slots and the per-query chunk token ceiling unchanged.
- **FR-003**: The selector MUST determine each candidate chunk's roles by aggregating the
  typed claims of the turns it covers (via the existing chunk→dialogue-id mapping), relative
  to the query's demand atoms and the already-selected set.
- **FR-004**: The query's demand atoms MUST be derived without a query-time LLM call (reusing
  the engine's deterministic query-entity extraction).
- **FR-005**: The selector MUST apply the following priority, in order: (1) protect the top-2
  pointwise-rerank anchors; (2) collapse exact duplicates (same entity+slot+value+time_state)
  to the highest-scored instance; (3) never collapse a state conflict (same entity+slot,
  differing value/time_state); (4) prioritise candidates covering an unmet demand atom;
  (5) suppress only high-confidence lures (same entity, wrong slot, no unmet demand);
  (6) fill remaining slots by rerank score; (7) enforce selected ≤ 12 and token ≤ the
  baseline 12-chunk token count for that query.
- **FR-006**: Any uncertain role classification MUST be treated as non-penalising — the
  selector defaults to rerank-score behaviour rather than dropping a possibly-genuine span.
- **FR-007**: Per-span typed claims (`entity/slot/value/polarity/time_state/source_turn_ids`)
  MUST be produced by a one-time offline annotation pass and cached to a reusable sidecar
  file; the coverage/answer runs MUST consume the cache with no query-time LLM call.
- **FR-008**: The harness MUST provide an oracle selector arm that uses gold turn labels to
  maximise 12-chunk evidence coverage, reported as the realisable ceiling.
- **FR-009**: The coverage-only mode MUST report, per arm, exact-turn recall (overall and by
  category) plus `selection_survival`, `complement_drop`, and `anchor_violation`, with zero
  answer/judge tokens spent.
- **FR-010**: The feature MUST NOT modify any file under `memory/ embedding/ provider/
  store/ internal/`, and MUST NOT alter the production extraction schema; `pcic_meta` is a
  bench sidecar that is never written to the store, retrieved, or embedded.
- **FR-011**: All signals MUST degrade per-signal: a missing sidecar, missing reranker, or
  unannotated span MUST reduce the selector to graceful rerank-order behaviour, never a
  whole-run failure.
- **FR-012**: The paid paired answer eval MUST be reachable only after the coverage gate is
  cleared, and MUST produce a McNemar verdict for the `+pcic` contrast.

### Key Entities *(include if feature involves data)*

- **pcic_meta (span annotation)**: one record per raw span, keyed by span id; attributes
  `entity`, `slot`, `value`, `polarity`, `time_state`, `source_turn_ids`. A bench sidecar
  artifact; not part of the engine store or retrieval index.
- **Candidate window**: the fixed top-60 reranked chunk candidates a query's selector
  chooses from (18 fact slots handled unchanged).
- **Demand atom**: an entity/slot requirement extracted deterministically from the query;
  drives complement coverage.
- **Chunk role**: the aggregated classification of a candidate (anchor / duplicate /
  state-conflict / complement / lure / unknown) used by the lexicographic priority.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001** (coverage gate): On the correct `hybrid+rerank` baseline, the selector lifts
  exact-turn recall by at least **+2pp overall OR +4pp multi-hop** at the fixed 12-chunk
  budget.
- **SC-002** (no collateral): No major question category regresses by more than **1pp** in
  exact-turn recall versus `hybrid+rerank`.
- **SC-003** (complement safety): Complement recall stays **≥95%** (`complement_drop` ≤5%).
- **SC-004** (anchor safety): `anchor_violation` is exactly **0** — the top-2 pointwise
  anchors are always retained.
- **SC-005** (budget neutrality): The selected chunk token count does not exceed the
  `hybrid+rerank` baseline for any query; the fact budget is unchanged.
- **SC-006** (cost discipline): The free coverage gate spends **zero** answer/judge tokens;
  annotation is a one-time cached cost; a paid answer eval runs only after SC-001 passes.
- **SC-007** (conversion, post-gate): On the paid multi-hop pilot, the `+pcic` arm shows a
  non-negative answer-accuracy delta with a reported McNemar verdict (adoption requires an
  above-noise positive; a null or negative result stops adoption).

## Assumptions

- The correct baseline for judging the selector is `hybrid+rerank` (not the legacy plain
  `hybrid`), per the confirmed rerank win.
- The prebuilt LoCoMo stores (bge-embedded, gitignored) and the DashScope reranker sidecar
  from the rerank work are available and reused; the selector adds no new store build.
- Offline annotation uses the frozen relay model (`gpt-5.6-luna`) for comparability and is a
  one-time cost of roughly ¥5–15 for the 10-conversation dataset.
- The oracle ceiling (~+28pp multi-hop coverage) is an upper bound; the realistic selector
  recovers only a fraction, and coverage need not convert 1:1 to answer accuracy — the honest
  answer prior is multi-hop +1~3pp, overall +0.3~1.2pp.
- Evidence-recall provenance (chunk→dialogue-id mapping, exact-turn grading) from the
  existing coverage tooling is reused unchanged.
- Scope is the LoCoMo bench only; the full PCIC schema (immutable spans, typed claims in the
  production store, provenance closure) is explicitly out of scope for this MVP and deferred
  until this selector clears both gates.
