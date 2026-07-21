# Tasks: Strike 3 — Abstention Gate for Adversarial Questions

**Feature**: `006-strike3-abstention-gate` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

**Design inputs**: [research.md](./research.md) · [data-model.md](./data-model.md) · [contracts/cli-and-artifacts.md](./contracts/cli-and-artifacts.md) · [quickstart.md](./quickstart.md)

## STATUS (2026-07-21): feature CLOSED — free gate NO-GO

- T001–T025 delivered by external agent (uncommitted working tree); reviewed + committed by maintainer at `07563ce` (mechanism) — full suite green, engine untouched.
- **T016 (free probe): the external agent's run was INVALID** (wrong store from another repo + no pcic_meta → claim signal never ran, confidence degenerate). Maintainer re-ran on the correct engram 10-conv store + 005 pcic_meta: **NO-GO** (confidence signal real, AUC 0.84, but the strict SC-003 corner missed by ~1pp; claim signal dead). Recorded in `specs/003-bio-retrieval-locomo/eval-log.md` (separate eval commit, Constitution IV).
- **T026 (paid frontier): NOT RUN** — free gate not cleared; stays permanently blocked for this feature.
- T027 done (eval-log verdict). T028 engine-gate EMPTY ✓. T029 full suite green ✓. T030 review done (this pass).
- Mechanism code retained (lazy, default-off): `--abstain-*` arms don't touch existing arms; `--abstain-probe` is a standalone offline tool.

## Delegation & guardrails (read before starting)

- **Execution model**: implementation is delegated to an external AI agent; the maintainer reviews, closes out, and backstops. Each task is self-contained — do not invent scope beyond it.
- **TDD is mandatory** (Constitution IV + plan): for every behavior task, write the failing test FIRST, watch it fail for the right reason, then implement to green. No test = not done.
- **Engine is off-limits (hard gate)**: NO edits under `memory/ embedding/ provider/ store/ internal/`. Verify after every commit: `git diff --name-only master...006-strike3-abstention-gate -- memory embedding provider store internal` MUST be empty.
- **Post-edit**: `CGO_ENABLED=0 go build ./...` → zero errors; `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` for touched tests.
- **COST FIREWALL**: Phase 4 (US2) is PAID and MUST NOT run without (a) a recorded **GO** verdict from Phase 3's probe AND (b) explicit maintainer authorization. Building the US2 code is fine; *running the paid eval* is gated. T029 is the only paid task and is marked BLOCKED-BY-AUTH.

---

## Phase 1: Setup

- [ ] T001 Confirm build baseline on branch `006-strike3-abstention-gate`: run `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` — both green before any change. Record the baseline in the PR description.
- [ ] T002 Create empty `cmd/locomo-bench/abstain.go`, `cmd/locomo-bench/abstain_probe.go`, `cmd/locomo-bench/abstain_test.go` with `package main` headers so subsequent tasks have their target files.

---

## Phase 2: Foundational — abstention signal core (BLOCKS US1 and US2)

**Goal**: the per-question `AbstainSignal` (claim-match + confidence) and the threshold-separated `AbstainDecision`. Both stories depend on this.

- [ ] T003 [P] Write failing tests in `cmd/locomo-bench/abstain_test.go` for claim-match grading: given synthetic candidates + a `pcic_meta` map (via `SpanKey`), assert the grade is `no-match` / `entity-only` / `entity+slot` for constructed cases (entity absent; entity present but slot absent — the "Caroline charity-race" trap; entity+slot both present). See [data-model.md](./data-model.md) `AbstainSignal.ClaimMatch`.
- [ ] T004 [P] Write failing tests in `cmd/locomo-bench/abstain_test.go` for confidence normalization + `ConfidenceSource` selection: rerank score used when present, cosine fallback otherwise, value normalized to [0,1].
- [ ] T005 [P] Write failing test in `cmd/locomo-bench/abstain_test.go` for degradation: with no `pcic_meta`, `ClaimSignalPresent == false` and the signal still yields a confidence value (Constitution V, FR-006).
- [ ] T006 Implement `AbstainSignal` computation in `cmd/locomo-bench/abstain.go`: derive demand entity(+slot) via existing `derivePCICSignals`/`EntitySignalsForQuery` (no LLM), grade claim-match against `SpanKey`-scoped `pcic_meta` claims over the candidates' turns, and compute normalized confidence from candidate relevance scores. Make T003–T005 pass. (R1, R2, R3)
- [ ] T007 Implement `AbstainDecision` in `cmd/locomo-bench/abstain.go` as a pure function of `(AbstainSignal, thresholdConfig)` → `{Abstain bool, Rule string}`, kept separate from signal computation so the probe can sweep τ. Deterministic tie rule `>= τ ⇒ abstain`.

**Checkpoint**: `go test ./cmd/locomo-bench -run Abstain` green; signal + decision usable by the probe.

---

## Phase 3 (US1, P1): Free offline probe → GO/NO-GO gate — MVP

**Goal**: zero-cost probe that labels all questions, sweeps thresholds, emits per-signal ROC/AUC + a single GO/NO-GO verdict and the `abstain-probe.json` artifact. **This is the MVP and the cost firewall.**

**Independent test**: run `--abstain-probe` on the persisted store; verify zero answer/judge calls, correct counts, ROC/AUC per signal, and a printed verdict (quickstart US1).

- [ ] T008 [P] [US1] Write failing test in `cmd/locomo-bench/abstain_test.go` for ROC/AUC: on a synthetic separable labeled set AUC → 1.0; on an inseparable set AUC → ~0.5; ROC points monotone in τ. (R4)
- [ ] T009 [P] [US1] Write failing test for the gate verdict in `cmd/locomo-bench/abstain_test.go`: `GO` iff some signal has a point with adv-recall ≥ 0.40 AND false-abstain ≤ 0.05 (net ≥ +100); else `NO-GO`. Cover a just-passing and a just-failing fixture. (SC-003)
- [ ] T010 [P] [US1] Write failing test asserting the probe makes ZERO answer/judge calls: inject a stub caller that fails the test if invoked, run the probe path, assert no invocation (mirror `TestCoverage...UsesNoAnswerOrJudgeLLM`). (FR-004, SC-001)
- [ ] T011 [P] [US1] Write failing test asserting the probe writes no engine state and the artifact contains no secret (mirror the pcic no-secret / no-engine-state tests). (FR-004)
- [ ] T012 [P] [US1] Write failing test asserting population counts from LoCoMo labels: adversarial = 446, answerable = 1540, total = 1986 (category-5 = adversarial). (spec Context, contract invariant)
- [ ] T013 [US1] Implement the probe in `cmd/locomo-bench/abstain_probe.go`: reuse the coverage-only retrieval path to get candidates per question, compute `AbstainSignal`, label adversarial vs answerable, sweep τ per signal (`claim`, `confidence`, `combined` via the R5 OR/grid), compute ROC points + trapezoidal AUC + best point + `MeetsGate`, and the overall verdict. Make T008–T012 pass. (R4, R5)
- [ ] T014 [US1] Implement flags in `cmd/locomo-bench/main.go`: `--abstain-probe` (terminal branch, no answer/judge), `--abstain-probe-out` (default `<store|run-dir>/abstain-probe.json`), `--abstain-gate` (override thresholds; default = declared SC-003 gate). Wire the branch after store/pcic-meta resolution. (contracts §1)
- [ ] T015 [US1] Implement `abstain-probe.json` serialization in `cmd/locomo-bench/abstain_probe.go` exactly per [contracts/cli-and-artifacts.md](./contracts/cli-and-artifacts.md) §2, including `zero_llm: true`, `confidence_source`, and the `gate` block. Assert contract invariants (`total == adv+ans`; `verdict==GO` iff any `meets_gate`).
- [ ] T016 [US1] Run the probe on the persisted 10-conv store (+ `pcic_meta`) per quickstart US1; capture `abstain-probe.json` to scratchpad and read the verdict. **Zero cost.** This executes the free gate.

**Checkpoint (US1 done)**: probe produces a decisive GO/NO-GO with full ROC diagnostic, zero model cost. If **NO-GO** → go to T027 (record verdict) and STOP; Phase 4 does not run.

---

## Phase 4 (US2, P2): Paid paired frontier evaluation — GATED

**Goal**: measure the trade-off frontier (force-answer / prompt-only / hard-gate / soft-hint). Build unconditionally; RUN only on GO + authorization.

**Independent test**: paired run on adversarial + answerable sample produces the frontier table; hard-gate issues 0 answer calls on flagged questions (quickstart US2).

- [ ] T017 [P] [US2] Write failing test in `cmd/locomo-bench/abstain_test.go`: the `+abstain-hard` arm, on a flagged question, does NOT call the answer model (stub caller asserts 0 calls) and emits the canonical decline. (FR-008, SC-005)
- [ ] T018 [P] [US2] Write failing test: the `+abstain-soft` arm injects the low-confidence hint into the abstain prompt when the signal is low, and leaves it out otherwise. (R6)
- [ ] T019 [P] [US2] Write failing test: the `+abstain-*` arms are OFF by default and only activate when explicitly listed in `--retrieval` (arm-gating, mirror `TestRerankArmMechanismGatesReranker`). (FR-012)
- [ ] T020 [US2] Implement the `+abstain-hard` operating point in `cmd/locomo-bench/runner.go`: on `AbstainDecision.Abstain`, short-circuit to the canonical decline and skip the answer model; else answer normally. Reuse `adversarialGold` scoring. Make T017 pass.
- [ ] T021 [US2] Implement the `+abstain-soft` operating point in `cmd/locomo-bench/runner.go`/`main.go`: surface the low-confidence hint into the existing `--abstain-prompt` scaffold; the model makes the final call. Make T018 pass.
- [ ] T022 [US2] Wire both arms into the arm list in `cmd/locomo-bench/main.go` with default-off gating. Make T019 pass. (contracts §1)
- [ ] T023 [US2] Implement adversarial/answerable accounting + per-arm McNemar vs the paired baseline in `cmd/locomo-bench/stats.go` (reuse existing McNemar); emit the frontier-table artifact per [contracts/cli-and-artifacts.md](./contracts/cli-and-artifacts.md) §3, including OP0/OP1/OP2a/OP2b rows and the `baseline_note`.
- [ ] T024 [P] [US2] Write failing test asserting the frontier-table artifact matches the contract schema (all four operating-point names present; `hard-gate.answer_calls == 0` on flagged; `force-answer.mcnemar` may be null). Make it pass alongside T023.
- [ ] T025 [US2] Produce the cost `--estimate` for the paired frontier run (adversarial 446 + answerable sample N, repeats=1) per quickstart US2. Present the estimate to the maintainer. **Do not run the paid eval.**
- [ ] T026 [US2] 🔒 BLOCKED-BY-AUTH + BLOCKED-BY-GO: run the paired frontier eval only after a recorded GO verdict (T016) AND explicit maintainer cost authorization. Capture results to scratchpad. **The external agent MUST STOP at T025 and wait.**

---

## Phase 5: Polish & Cross-Cutting

- [ ] T027 Record the outcome in `specs/003-bio-retrieval-locomo/eval-log.md` under a new "Strike 3: Abstention gate" section: the probe verdict + ROC/AUC diagnostic (always), and — if T026 ran — the frontier table with the declared new adversarial baseline + rationale. **Eval-result commit MUST be separate from mechanism-code commits** (Constitution IV, FR-011/SC-007).
- [ ] T028 Engine-untouched gate: `git diff --name-only master...006-strike3-abstention-gate -- memory embedding provider store internal` → assert EMPTY. (FR-010, SC-006)
- [ ] T029 Full suite: `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test -count=1 ./...` → all green. Confirm probe tests assert real behavior (not self-comparing tautologies).
- [ ] T030 Final integration review (maintainer): re-verify SC-001..SC-007, confirm no secret in any artifact, confirm arms default-off, update `tasks.md` checkboxes + a one-line status in the PR.

---

## Dependencies & execution order

- **Phase 1 → Phase 2 → Phase 3** is the critical path to the MVP + free gate.
- **Phase 2 (T003–T007)** blocks both US1 and US2 (shared signal core).
- **US1 (Phase 3)** is independently shippable and yields the decisive verdict at zero cost. If NO-GO, skip Phase 4 entirely (T027 records, then done).
- **US2 (Phase 4)** depends on Phase 2 for code and on **T016 GO verdict + authorization** for the *run* (T026). US2 *code* (T017–T025) can be built regardless.
- **Phase 5** closes out; T027 (eval commit) is separate from all mechanism commits.

## Parallel opportunities

- Phase 2 tests: **T003, T004, T005** in parallel (same file, distinct test funcs — coordinate to avoid edit races; or write sequentially if one agent).
- US1 tests **T008–T012** are mutually independent (parallelizable across agents).
- US2 tests **T017, T018, T019, T024** are independent.
- Implementation tasks that share a file (`abstain.go`: T006/T007; `main.go`: T014/T022; `runner.go`: T020/T021) are **not** parallel — serialize per file.

## Implementation strategy (MVP first)

1. **MVP = Phase 1 + Phase 2 + Phase 3 (US1).** Delivers the free GO/NO-GO gate and full ROC diagnostic at zero model cost. This alone is a complete, valuable increment — it can end the feature with a NO-GO (money saved), exactly as PCIC-lite did.
2. Build **US2 code** (T017–T025) so the paid eval is ready to run *the instant* a GO + authorization lands — but STOP at T025.
3. Run **T026** only on GO + explicit authorization. Then **Phase 5** closes out with a separate eval-result commit.

## Task count

- Total: **30**
- Setup: 2 (T001–T002) · Foundational: 5 (T003–T007) · US1: 9 (T008–T016) · US2: 10 (T017–T026) · Polish: 4 (T027–T030)
- Test tasks (TDD): T003–T005, T008–T012, T017–T019, T024 = **12 failing-test-first tasks**
- Paid tasks: **1** (T026, gated). Zero-cost gate execution: T016.
