# Implementation Plan: Strike 3 — Abstention Gate for Adversarial Questions

**Branch**: `006-strike3-abstention-gate` | **Date**: 2026-07-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/006-strike3-abstention-gate/spec.md`

## Summary

Open abstention as a distinct operating point in `cmd/locomo-bench` to win LoCoMo's 446 category-5 adversarial (false-premise) questions — a slice currently excluded from the ≈67.6% answerable baseline. The approach borrows Synthius-Mem's thesis ("structured typed-claim store; empty lookup ⇒ premise absent ⇒ decline") but adapts it to engram, whose hybrid retrieval never returns empty: the abstention signal is synthesized offline from (a) a **typed-claim match** of the query's demand entity(+slot) against retrieved candidates' `pcic_meta` claims, and (b) a **retrieval-confidence** score. A zero-cost offline probe measures how well each signal (and their combination) separates adversarial from answerable questions and emits a single GO/NO-GO verdict against a pre-declared gate (SC-003). Only on GO **and** explicit cost authorization does a paired, same-process answering evaluation measure the trade-off frontier (force-answer / prompt-only / hard-gate / soft-hint). Engine packages and the shipped schema are untouched; all logic lives in the bench adapter, reusing feature 005's `pcic_meta` sidecar and the existing adversarial-gold + abstain-prompt scaffolding.

## Technical Context

**Language/Version**: Go 1.25.0 (no CGO — pure-Go, cross-compilable; engine invariant)

**Primary Dependencies**: existing `cmd/locomo-bench` harness only — `memory` engine public API (`EntryStore.EntitySignalsForQuery`, `EntitiesByEntry`, `EntityNorm`), `pcic_meta.go` sidecar (feature 005), the reranker wiring already used in 003 (DashScope `gte-rerank-v2` via `EMBED_RERANK_MODEL`), and the local fastembed/bge sidecar for offline query vectors. No new third-party dependency.

**Storage**: reuse the persisted 10-conversation LoCoMo store (`--store-dir`) + `pcic_meta.json` sidecar. No new tables, no migration. SQLite via `modernc.org/sqlite` unchanged.

**Testing**: `CGO_ENABLED=0 go test` — Go unit tests in `cmd/locomo-bench/*_test.go`. Probe/ROC logic tested offline on synthetic labeled sets; hard-gate no-LLM behavior tested with a stub caller; zero-token and zero-engine-state assertions mirror the existing `TestCoverage...UsesNoAnswerOrJudgeLLM` and pcic tests.

**Target Platform**: Linux (WSL2 dev); offline-capable eval harness.

**Project Type**: single project — CLI eval harness (adapter over the engine). No new binary; new flags on `cmd/locomo-bench`.

**Performance Goals**: US1 probe runs to a verdict on the existing store with zero incremental model cost and no rebuild (SC-002). Per-question signal computation is offline arithmetic over already-retrieved candidates — negligible vs retrieval.

**Constraints**: engine + shipped schema unchanged (FR-010/SC-006); US1 consumes zero answer/judge tokens (FR-004/SC-001); paid US2 gated behind GO + explicit authorization (FR-012).

**Scale/Scope**: 1986 LoCoMo questions across 10 conversations (446 adversarial + 1540 answerable). Single-user eval scale — within the honest ~100k-entry class.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1 design.*

- **I. Local-first, offline by default** — ✅ US1 probe is fully offline (local store + local query vectors + offline signal arithmetic), zero network answer/judge calls. Reranker/relay are the same opt-in online enhancements already used in 003, only in the paid US2 phase. No new required hosted service.
- **II. Engine/adapter separation** — ✅ All logic in `cmd/locomo-bench` (adapter). Engine consumed only through existing public API. FR-010/SC-006 make the empty `git diff` over `memory embedding provider store internal` a hard gate. No engine contract change.
- **III. Contract-first & namespace isolation** — ✅ Outward contract = new bench flags + the probe artifact schema + the frontier-table schema, frozen here in plan/contracts before implementation. No cross-namespace access (single-store eval). No engine schema change.
- **IV. Evaluation regression gate (NON-NEGOTIABLE)** — ✅ This feature *is* an eval capability. Adversarial accuracy is declared a **new baseline** with rationale (FR-011/SC-007); eval-result commits are separate from mechanism-code commits. Answerable regression from abstention is measured explicitly on the frontier (the whole point of operating point C), never hidden. The bench harness itself is adapter code that only calls unchanged engine paths — engine invariance proven by the empty diff + green engine tests, not a re-run.
- **V. Graceful degradation & honest scale** — ✅ Signals degrade per-signal (no `pcic_meta` ⇒ confidence-only; no reranker ⇒ embedding cosine) per FR-006. Adversarial-accuracy claims are bounded to the 10-conversation LoCoMo slice; the free gate exists precisely to avoid over-claiming before evidence.

**Verdict: PASS.** No violations; Complexity Tracking empty.

## Project Structure

### Documentation (this feature)

```text
specs/006-strike3-abstention-gate/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (flag + artifact + frontier-table contracts)
├── checklists/
│   └── requirements.md  # (from /speckit-specify)
├── spec.md
└── tasks.md             # /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── abstain.go           # NEW — abstention signal computation (claim-match + confidence) + decision rule
├── abstain_probe.go     # NEW — offline probe: label questions, sweep threshold, ROC/AUC, GO/NO-GO verdict
├── abstain_test.go      # NEW — TDD: signal grades, ROC/AUC on synthetic sets, gate verdict, degradation
├── main.go              # MODIFIED — new flags (--abstain-probe, abstain operating-point arms); wire probe branch
├── runner.go            # MODIFIED — hard-gate (skip answer LLM on flag) + soft-hint (inject low-confidence hint) arms; reuse abstain prompt scaffold
├── coverage.go          # MODIFIED (if needed) — probe reuses the coverage-only retrieval path (zero answer/judge)
├── pcic.go / pcic_meta.go   # REUSED read-only — SpanKey scoping, claim lookup, EntitySignalsForQuery
└── stats.go             # REUSED/EXTENDED — adversarial vs answerable accounting, McNemar per arm

# UNCHANGED (hard gate): memory/ embedding/ provider/ store/ internal/
#   verify: git diff --name-only master...006-strike3-abstention-gate -- memory embedding provider store internal  → EMPTY
```

**Structure Decision**: Single-project CLI eval-harness feature. New logic is two focused files (`abstain.go` signal core, `abstain_probe.go` offline gate) plus surgical edits to `main.go`/`runner.go` for the paid operating-point arms. This mirrors feature 005's layout (`pcic.go` + `pcic_meta.go` + arm wiring) and keeps each unit independently testable. The engine tree is off-limits.

## Complexity Tracking

> No Constitution violations — table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none)    | —          | —                                    |
