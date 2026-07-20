# Implementation Plan: PCIC-lite Span Selector

**Branch**: `005-pcic-lite-selector` | **Date**: 2026-07-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/005-pcic-lite-selector/spec.md`

## Summary

Insert a deterministic, conservative lexicographic span selector into the LoCoMo bench
retrieval path — between the `gte-rerank` stage and the chunk-quota truncation — that picks
12 chunks from a fixed top-60 reranked candidate window using one-time offline-annotated
per-span typed claims (`pcic_meta`). The goal is to recover the multi-hop **bridge** gold
the pointwise reranker leaves below its top-12 (free probe: multi-hop exact-turn recall
0.642 @12 → 0.917 @48; oracle 12-slot ceiling ~+28pp). Delivery is free-first: a
coverage-only bake-off contrasts `hybrid+rerank` vs `hybrid+rerank+pcic` vs an oracle arm
on evidence coverage and three safety metrics with zero answer/judge tokens; a paid
multi-hop McNemar eval runs only after the coverage gate (SC-001) passes. All work lives in
`cmd/locomo-bench`; the engine and production schema are untouched.

## Technical Context

**Language/Version**: Go 1.25.0, `CGO_ENABLED=0` (pure-Go, cross-compilable) — hard gate.

**Primary Dependencies**: existing engine **public** API only — `memory.Retriever`
(`SearchWithDiagnostics`), `memory.Result`, the `embedding.Reranker` already wired via
`buildBenchReranker`, and the `provider.Provider` used for offline annotation. No new
third-party deps.

**Storage**: `pcic_meta` JSON sidecar under the bench run/store dir (gitignored), keyed by
span id; reuses the prebuilt LoCoMo SQLite stores (`--store-dir`, bge-embedded). Never
written to the engine store, retrieval index, or embedding space.

**Testing**: `go test` with `CGO_ENABLED=0`, TDD (red→green). Offline selector unit tests
(deterministic, stubbed candidates + synthetic `pcic_meta`), coverage-grader tests reusing
the existing exact-turn provenance, and a parity test that `hybrid`/`hybrid+rerank` arms are
byte-for-byte unchanged when `+pcic` is absent.

**Target Platform**: Linux (WSL2 dev; CI runs `CGO=0` build+test+vet on Go 1.25).

**Project Type**: evaluation-harness adapter — a single package under `cmd/locomo-bench`.

**Performance Goals**: selection latency ≤ **1.10×** the `hybrid+rerank` baseline per query
(SC-005 budget-neutral); offline annotation is a one-time ~¥5–15 cost, cached and reused.

**Constraints**: engine + production schema **zero-change** (`git diff --name-only --
memory embedding provider store internal` empty); coverage gate spends **zero** answer/judge
tokens; conservative gate must not regress any major category > 1pp; per-signal graceful
degradation; no query-time LLM.

**Scale/Scope**: LoCoMo 10 conversations / 1532 questions (multi-hop N=282); top-60 chunk
candidate window; 12 chunk + 18 fact budget.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-first, offline by default** | PASS | Coverage gate runs fully offline (local bge embeddings + reranker sidecar + deterministic selector). Offline annotation is opt-in, one-time, cached; default paths need no network. |
| **II. Engine/adapter separation** | PASS (with a hard research gate) | All code in `cmd/locomo-bench`; engine consumed via public API only (FR-010). **Open item**: FR-004 needs query demand atoms with no query-time LLM. If the engine does not already expose deterministic query-entity extraction publicly, the bench derives its own (candidate-entity ↔ query-token match) — it MUST NOT modify engine internals. Resolved in Phase 0 (R1). |
| **III. Contract-first & namespace isolation** | PASS | The outward contract is frozen before code: the `+pcic` arm-suffix mechanism, the `pcic_meta` sidecar schema, the annotate subcommand, and the coverage report metric fields (see `contracts/`). Namespaces N/A (bench uses one store per conversation). |
| **IV. Evaluation regression gate (NON-NEGOTIABLE)** | PASS (by construction) | The selector affects only the `+pcic` arm; `hybrid`/`hybrid+rerank` arms are byte-for-byte unchanged (FR-001), proven by a parity test — baseline is invariant, so no regression is possible on it. This feature *is* the eval; the coverage bake-off is the comparable-metric run, and any paid answer eval is gated behind it. Eval-config commits stay separate from selector-algorithm commits (attribution). |
| **V. Graceful degradation & honest scale** | PASS | FR-011: missing sidecar / reranker / unannotated span → selector degrades to rerank-order, never fails whole. Oracle arm reports an honest ceiling; the spec states an honest, deflated answer prior (multi-hop +1~3pp). |

**Gate result**: PASS. One hard research item (R1: query demand atoms without engine change).
No constitution violation requires a Complexity Tracking entry.

## Project Structure

### Documentation (this feature)

```text
specs/005-pcic-lite-selector/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── pcic-arm-mechanism.md
│   ├── pcic-meta-sidecar.md
│   └── coverage-report-metrics.md
├── checklists/
│   └── requirements.md  # spec quality checklist (already present)
└── tasks.md             # /speckit-tasks output (NOT created here)
```

### Source Code (repository root)

All changes are confined to the bench adapter package. No engine file is touched.

```text
cmd/locomo-bench/
├── main.go              # EDIT: add "pcic" to supportedArmMechanisms; options.pcic +
│                        #   --pcic flag; wire selector into retriever/quota path; add the
│                        #   `--pcic-annotate` one-time subcommand entry
├── chunks.go            # EDIT: retrieveWithQuotaDiagnostics gains an optional selector
│                        #   hook applied to the top-60 chunk candidates before quota
├── pcic.go              # NEW: pcicSelect() lexicographic selector + role classification;
│                        #   demand-atom derivation; loadPCICMeta(); degradation shims
├── pcic_meta.go         # NEW: pcic_meta sidecar type + one-time offline annotation pass
│                        #   (provider-backed) + JSON load/save (cache-reuse)
├── pcic_oracle.go       # NEW: oracle 12-chunk max-coverage selector (gold-label ceiling)
├── coverage.go          # EDIT: emit selection_survival / complement_drop / anchor_violation
│                        #   per arm alongside the existing turn/session recall
├── pcic_test.go         # NEW: selector unit tests (roles, conservative gate, budget cap)
├── pcic_meta_test.go    # NEW: sidecar load/save + degradation tests
└── coverage_test.go     # EDIT: metric-emission tests

# UNCHANGED (hard gate): memory/ embedding/ provider/ store/ internal/
```

**Structure Decision**: Single-package adapter feature. The selector, its metadata, and the
oracle are new sibling files in `cmd/locomo-bench` so each has one clear purpose and is unit-
testable in isolation; `main.go`/`chunks.go`/`coverage.go` receive minimal, additive edits at
the existing seams (arm mechanism table, `retrieveWithQuotaDiagnostics`, coverage reporter).
The engine stays a black box consumed through its public API.

## Complexity Tracking

No constitution violations — table intentionally empty.
