---
description: "Task list for PCIC-lite Span Selector"
---

# Tasks: PCIC-lite Span Selector

**Input**: Design documents from `specs/005-pcic-lite-selector/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/
**Tests**: INCLUDED — the project mandates test-first for behavior changes (CLAUDE.md) and
the contracts specify contract tests. Every implementation task is preceded by a failing test.

**Hard invariants for EVERY task** (a task that breaks one is not done):

- **Engine untouchable**: no edits under `memory/ embedding/ provider/ store/ internal/`.
  Verify `git diff --name-only -- memory embedding provider store internal` is empty.
- **Build/test gate**: after each task, `CGO_ENABLED=0 go build ./...` clean; touched-package
  tests green.
- **Parity**: `hybrid` and `hybrid+rerank` arms stay byte-for-byte identical when a `+pcic`
  sibling is present.
- **Cost**: coverage-path tasks spend zero answer/judge tokens; annotation is one-time.

## Path Conventions

Single-package adapter feature under `cmd/locomo-bench/`. New files: `pcic.go`,
`pcic_meta.go`, `pcic_oracle.go`, `pcic_test.go`, `pcic_meta_test.go`. Additive edits:
`main.go`, `chunks.go`, `coverage.go`, `coverage_test.go`.

---

## Phase 1: Setup

- [ ] T001 [P] Extend `.gitignore` so `pcic_meta*.json` and PCIC run/store artifacts
  (`cov-pcic*`, `ans-pcic*`) are never tracked (sidecar + secrets discipline).
- [ ] T002 Record the green baseline: run `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go
  test -count=1 ./cmd/locomo-bench/` and confirm `git diff --name-only -- memory embedding
  provider store internal` is empty; note the pre-change state in the PR description.

---

## Phase 2: Foundational (blocking prerequisites for all user stories)

- [ ] T003 Write failing `TestPCICArmMechanismGatesSelector` in
  `cmd/locomo-bench/pcic_test.go`: `optionsForArm` sets `options.pcic` only under a `+pcic`
  suffix; paired baseline `hybrid+rerank` has `pcic=false`; the `oracle` mechanism is
  recognized. (Contract: `contracts/pcic-arm-mechanism.md`.)
- [ ] T004 Add `pcic` and `oracle` to `supportedArmMechanisms`; add `options.pcic bool` and a
  `--pcic` global flag; wire `optionsForArm`/`optionsForRun` in `cmd/locomo-bench/main.go` so
  T003 passes and non-`pcic` arms are untouched.
- [ ] T005 Write failing parity test in `cmd/locomo-bench/pcic_test.go`: running
  `hybrid+rerank` alone vs alongside a `hybrid+rerank+pcic` sibling yields identical
  `hybrid+rerank` results (roles/selection must not leak into other arms).
- [ ] T006 Define the `pcic_meta` sidecar types (`SpanClaim`, header) and `loadPCICMeta`/
  `savePCICMeta` (JSON map + header) in `cmd/locomo-bench/pcic_meta.go`; missing file or
  header mismatch degrades to "absent" (warn, not fatal). Make failing
  `TestPCICMetaRoundTrip` + `TestPCICMetaHeaderMismatchDegrades` in
  `cmd/locomo-bench/pcic_meta_test.go` pass. (Contract: `contracts/pcic-meta-sidecar.md`.)
- [ ] T007 Add the selector seam to `retrieveWithQuotaDiagnostics` in
  `cmd/locomo-bench/chunks.go`: an optional selector func receives the top-60 chunk
  candidates (partitioned from the reranked `wide` list) before `applyChunkQuota`; a nil
  selector leaves the path byte-identical (keeps T005 green).

**Checkpoint**: arm mechanism, sidecar I/O, and the selector seam exist; no selector logic
yet. Parity holds.

---

## Phase 3: User Story 1 — Free coverage gate (Priority: P1) 🎯 MVP

**Goal**: Measure `hybrid+rerank` vs `hybrid+rerank+pcic` vs `hybrid+rerank+oracle` on
evidence coverage + safety metrics, zero answer tokens.

**Independent test**: `--coverage-only` three-arm run over the stores with a synthetic
`pcic_meta` fixture emits per-category turn recall + `selection_survival`/`complement_drop`/
`anchor_violation`; oracle bounds the selector; no LLM calls.

### Tests (write first, must fail)

- [ ] T008 [P] [US1] Failing selector unit tests in `cmd/locomo-bench/pcic_test.go`: anchor
  protection (top-2 always kept), duplicate collapse, state-conflict never-collapse,
  complement priority for unmet demand atoms, conservative lure (only high-confidence),
  budget ≤12 and token ≤ baseline ceiling.
- [ ] T009 [P] [US1] Failing demand-atom derivation test in `cmd/locomo-bench/pcic_test.go`:
  atoms come from `memory.EntityQueryTokens` + `EntitySignalsForQuery`; candidate entities
  from `EntitiesByEntry` — no query-time LLM, no engine edit.
- [ ] T010 [P] [US1] Failing oracle test in `cmd/locomo-bench/pcic_test.go`: greedy
  max-coverage picks the 12 chunks maximizing gold-turn coverage; equals full candidate-pool
  coverage when gold turns ≤12.
- [ ] T011 [P] [US1] Failing coverage-metric tests in `cmd/locomo-bench/coverage_test.go`:
  `selection_survival`, `complement_drop`, `anchor_violation` match hand-computed values on
  crafted selections. (Contract: `contracts/coverage-report-metrics.md`.)

### Implementation

- [ ] T012 [US1] Implement demand-atom derivation in `cmd/locomo-bench/pcic.go` (query→atoms
  via public engine API; candidate chunk entities via `EntitiesByEntry`/`EntityNorm`) — green
  T009.
- [ ] T013 [US1] Implement chunk-role classification in `cmd/locomo-bench/pcic.go` by
  aggregating a chunk's covered turns' `SpanClaim`s (via existing `chunkTurns` map) relative
  to demand atoms + already-selected set — green the role portions of T008.
- [ ] T014 [US1] Implement `pcicSelect` in `cmd/locomo-bench/pcic.go`: the 7-step
  lexicographic priority (anchor → duplicate → state-conflict → complement → lure → rerank
  fill → budget/token cap), conservative gate (unknown role never penalizes) — green T008.
- [ ] T015 [US1] Implement the oracle greedy selector in `cmd/locomo-bench/pcic_oracle.go` —
  green T010.
- [ ] T016 [US1] Emit `selection_survival`/`complement_drop`/`anchor_violation` per arm in
  `cmd/locomo-bench/coverage.go` (all arms on one schema; non-selector arms report the
  degenerate values per contract) — green T011.
- [ ] T017 [US1] Wire the selector + oracle into the T007 seam in
  `cmd/locomo-bench/chunks.go`/`main.go`, gated by `options.pcic` / the `oracle` arm; load
  the sidecar via `--pcic-meta`; degrade to rerank order when meta/reranker absent (FR-011).
- [ ] T018 [US1] Offline coverage integration test in `cmd/locomo-bench/coverage_test.go`
  with a synthetic `pcic_meta` fixture + stubbed candidates: the three arms produce turn
  recall + the three metrics with zero LLM calls.

**Checkpoint**: US1 delivers the free coverage gate independently (synthetic meta); this is a
shippable MVP that answers "does the selector clear SC-001?" for free.

---

## Phase 4: User Story 2 — Offline metadata annotation (Priority: P1)

**Goal**: Produce real `pcic_meta` from a one-time offline LLM pass; query-time stays
LLM-free.

**Independent test**: run `--pcic-annotate` once → sidecar written, no engine state; re-run
coverage twice → second run does no annotation and no query-time LLM.

### Tests (write first, must fail)

- [X] T019 [P] [US2] Failing tests in `cmd/locomo-bench/pcic_meta_test.go`:
  `TestPCICAnnotateWritesNoEngineState` (post-annotate engine diff empty, no store rows) and
  an annotation-shape test (each annotated span yields a well-formed `SpanClaim`).

### Implementation

- [X] T020 [US2] Implement a bench-local annotation prompt template + per-turn typed-claim
  extraction (`entity/slot/value/polarity/time_state/source_turn_ids`) via `provider.Provider`
  in `cmd/locomo-bench/pcic_meta.go` (prompt lives in the bench, NOT `memory/prompt/`).
- [X] T021 [US2] Implement the `--pcic-annotate` subcommand entry in `cmd/locomo-bench/main.go`:
  iterate raw dialogue, call the provider (frozen relay model), write the sidecar with header
  (annotate model + dataset fingerprint); idempotent cache-hit on matching header — green T019.
- [X] T022 [US2] Assert secret discipline: annotate key flows env→provider only; add a test
  that the sidecar and logs contain no API key.

**Checkpoint**: real annotations feed US1's selector; the coverage gate can now run on true
`pcic_meta`.

---

## Phase 5: User Story 3 — Gated paid paired answer eval (Priority: P2)

**Goal**: After the coverage gate passes, contrast `hybrid+rerank` vs `hybrid+rerank+pcic`
on answers with McNemar.

**Independent test**: the `+pcic` arm flows through the existing paired answer path producing
`paired.json`; the paid run is executed only when SC-001 passed.

- [ ] T023 [US3] Test in `cmd/locomo-bench/pcic_test.go` (offline stub) that
  `--retrieval hybrid+rerank,hybrid+rerank+pcic` plumbs the `+pcic` selection into the answer
  path and yields a two-arm paired report structure (no new eval code beyond the arm).
- [~] T024 [US3] GATED — **NOT RUN (correctly)**: the free coverage gate ran on real
  `pcic_meta` (2026-07-20) and **SC-001 FAILED** (+pcic overall −0.1pp, multi-hop −0.4pp; see
  eval-log.md "PCIC-lite 覆盖闸判决：NO-GO"). Per the gate, the paid McNemar eval is not run.

---

## Phase 6: Polish & Cross-Cutting

- [X] T025 [P] Record the coverage-gate result and the go/no-go decision in
  `specs/003-bio-retrieval-locomo/eval-log.md` (PCIC-lite section); if T024 ran, add the
  McNemar verdict. Eval-result commit is SEPARATE from algorithm commits (Constitution IV).
- [ ] T026 [P] Run the full suite `CGO_ENABLED=0 go test -count=1 ./...` and re-verify the
  engine-untouched diff gate; run `go vet ./cmd/locomo-bench/`.
- [ ] T027 Final integration review: confirm all contracts satisfied, parity test asserts
  (not a tautology), degradation paths exercised, and `quickstart.md` runs end-to-end on a
  synthetic fixture.

---

## Dependencies & Execution Order

- **Setup (T001–T002)** → **Foundational (T003–T007)** → user stories.
- **Foundational blocks everything**: arm mechanism (T003–T004), parity (T005), sidecar I/O
  (T006), selector seam (T007) are prerequisites for US1/US2/US3.
- **US1 (T008–T018)** is the MVP and depends only on Foundational (uses a synthetic
  `pcic_meta` fixture — independent of US2).
- **US2 (T019–T022)** depends on Foundational (T006 sidecar types); independent of US1 but
  produces the real meta US1's gate ultimately consumes.
- **US3 (T023–T024)** depends on Foundational (arm) + US1 (coverage gate must pass before the
  paid T024).
- **Polish (T025–T027)** last.

## Parallel Opportunities

- Setup: T001 ∥ (T002 is a check).
- US1 tests T008 ∥ T009 ∥ T010 ∥ T011 (distinct test funcs/files) — write all failing first.
- US1 impl: T012/T013/T014 are same-file (`pcic.go`) → sequential; T015 (`pcic_oracle.go`) ∥
  T016 (`coverage.go`) can proceed in parallel with the `pcic.go` chain.
- US1 and US2 can proceed in parallel once Foundational is done (different files, T006 shared
  type already landed).
- Polish T025 ∥ T026.

## Implementation Strategy (MVP first)

1. Land Setup + Foundational. Confirm parity + engine-clean.
2. Deliver **US1** to a green coverage gate on a **synthetic** `pcic_meta` — this alone
   decides, for free, whether the selector clears SC-001. If it does not, STOP (no annotation
   spend, no answer spend) and record the null result.
3. If US1's synthetic-meta signal is promising, land **US2** (real annotation, one-time cost)
   and re-run the coverage gate on true meta.
4. Only if the gate passes on real meta, run **US3** T024 (paid, authorized) for the McNemar
   conversion check.

**Suggested MVP scope**: Phases 1–3 (Setup + Foundational + US1). That is the free,
decisive increment.
