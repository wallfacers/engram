# Tasks: Unified Evidence-Grounded Answer Contract

**Input**: Design documents from `specs/038-unified-answer-contract/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`,
`contracts/`, `quickstart.md`

**Tests**: Required. Prompt behavior and routing changes are test-first; no
model-backed score may be claimed without comparable artifacts.

## Phase 1: Setup and Evidence Freeze

**Purpose**: Preserve the dirty shared worktree and freeze the audit basis.

- [x] T001 Record the pre-edit dirty paths, current revision, active worktrees, and engine-zero-diff result in `specs/038-unified-answer-contract/research.md` without reverting any existing work
- [x] T002 [P] [US2] Write the complete LoCoMo/LME prompt audit and corpus-contamination evidence in `docs/evaluation/reports/unified-answer-contract-verdict-2026-08-13.md`
- [x] T003 [P] Confirm the frozen contract and isolation rules in `specs/038-unified-answer-contract/contracts/answer-contract.md` match the spec before code changes

---

## Phase 2: Foundational Test-First Contract

**Purpose**: Establish failing tests for the single prompt hook and provenance.

**⚠️ CRITICAL**: Capture the expected failures before implementation.

- [x] T004 [US1] Add failing tests in `cmd/locomo-bench/bench_test.go` proving unified mode returns byte-identical system prompt text for category IDs 1–12, both dataset formats, and with/without question date
- [x] T005 [P] [US1] Add failing prompt-content tests in `cmd/locomo-bench/bench_test.go` for zero benchmark names, category/gold/evaluation wording, few-shot markers, known LoCoMo/LME phrases, and fixed refusal text
- [x] T006 [P] [US1] Add failing tests in `cmd/locomo-bench/bench_test.go` for default-off legacy byte identity, `hybrid+unified` arm selection, and global/per-arm option behavior
- [x] T007 [P] [US1] Add failing validation tests in `cmd/locomo-bench/bench_test.go` for force/abstain/temporal/typed/counter-refine/hard-soft/scaffold/trace/category-budget conflicts and required `--no-idk-retry`
- [x] T008 [P] [US1] Add failing ordinary/formal prompt-digest tests in `cmd/locomo-bench/bench_test.go`, `cmd/locomo-bench/eval_protocol_test.go`, and `cmd/locomo-bench/eval_fixed_gold_oracle_test.go`

**Checkpoint**: The new contract tests fail for missing implementation, while
unrelated existing tests remain green.

---

## Phase 3: User Story 1 — One Dataset-Independent Contract (Priority: P1) 🎯 MVP

**Goal**: One opt-in system prompt serves every answer question without dataset
or category routing.

**Independent Test**: The same `locomoQA` request receives the frozen prompt for
all categories and dates; disabling the mode returns every legacy prompt byte.

- [x] T009 [US1] Replace the unshipped LME entity prompt family with the frozen `unifiedAnswerContract` and category-free selection in `cmd/locomo-bench/runner.go`
- [x] T010 [US1] Add `--unified-answer-contract` and `+unified` paired-arm selection, default-off behavior, and fail-fast isolation in `cmd/locomo-bench/main.go`
- [x] T011 [US1] Ensure ordinary, B0/B1, compiler, replay, source-validation, and fixed-gold answer calls all use `answerSystemPromptForEval` in `cmd/locomo-bench/main.go`, `cmd/locomo-bench/eval_runner.go`, `cmd/locomo-bench/eval_compile_bridge.go`, `cmd/locomo-bench/eval_source_validate.go`, and `cmd/locomo-bench/eval_fixed_gold_oracle.go`
- [x] T012 [US1] Bind effective prompt bytes to normal journal fingerprints and formal protocol digests in `cmd/locomo-bench/main.go` and `cmd/locomo-bench/eval_runner.go`
- [x] T013 [US1] Run `gofmt` on touched Go files and make all Phase-2 tests pass without changing historical control prompt constants

**Checkpoint**: Unified mode is usable and isolated; legacy mode is byte-stable.

---

## Phase 4: User Story 2 — Audit and Prevent Benchmark Leakage (Priority: P2)

**Goal**: Make specialization and measurement limits explicit and mechanically
prevent their return to the unified prompt.

**Independent Test**: Static prompt tests detect benchmark/category/gold/example
terms, and the report maps every historical prompt to keep/rewrite/control/remove.

- [x] T014 [US2] Remove stale implementation claims from `docs/evaluation/reports/lme-entity-verify-verdict-2026-08-13.md` and mark the LME-only prompt as abandoned/superseded
- [x] T015 [US2] Complete the historical prompt disposition table, judge-leakage evidence, and development-set warning in `docs/evaluation/reports/unified-answer-contract-verdict-2026-08-13.md`
- [x] T016 [US2] Verify `rg` finds no unified-contract copy of known LoCoMo/LME questions, gold phrases, entity examples, or benchmark labels and record the exact offline check in `specs/038-unified-answer-contract/quickstart.md`

**Checkpoint**: The product-oriented prompt has no test-set examples or scorer
contract; historical scoring prompts remain clearly labelled harness-only.

---

## Phase 5: User Story 3 — Comparable Evaluation Verdict (Priority: P3)

**Goal**: Produce a score only from a frozen single-variable paired run and
report reliability metrics separately from total accuracy.

**Independent Test**: Control and unified rows align one-to-one, evidence and all
non-prompt axes match, and the report contains majority/McNemar/flips/error
types/cost.

- [x] T017 [US3] Freeze the 17-case non-benchmark development smoke fixtures and review rubric under `specs/038-unified-answer-contract/fixtures/`; label them explicitly as non-held-out and non-promotional
- [x] T018 [US3] Add a model-backed behavior-probe runner and offline parser tests in `cmd/locomo-bench/unified_answer_contract_probe.go` and `cmd/locomo-bench/unified_answer_contract_probe_test.go`
- [x] T018A [US3] Add fail-closed per-row provider-call audits and per-repeat validation receipts for the exact `hybrid,hybrid+unified` pair in `cmd/locomo-bench/unified_answer_contract_eval.go`; validate actual provider-facing answer-user byte parity before printing scores
- [ ] T019 [US3] Freeze fresh source/binary/data/store/prompt/model manifests in the session scratch run directory; reject the mismatched historical store receipt
- [x] T020 [US3] Health-check answer/embed/judge endpoints without exposing secrets; record `BLOCKED` and stop if any required role is unavailable
- [ ] T021 [US3] Run the 17-case development smoke probe, a discarded warm-up, and a fresh paired LoCoMo top-k-30 pilot with `hybrid` versus `hybrid+unified`, no reranker or category-specific mechanism, using the detached recipes in `specs/038-unified-answer-contract/quickstart.md`
- [ ] T022 [US3] If development smoke and pilot gates pass, separately author and pre-register a held-out human-labelled behavior cohort sized for the declared confidence bound, then run it and three full paired LoCoMo repetitions including adversarial questions; otherwise record `NO-GO` for a measured failure or `BLOCKED` for missing evidence, without prompt retuning
- [ ] T023 [US3] Compute majority, exact McNemar, per-category/slice flips, false-answer, false-abstention, partial-answer, failure, token, latency, and cost metrics in `docs/evaluation/reports/unified-answer-contract-verdict-2026-08-13.md`
- [ ] T024 [US3] Run LongMemEval-S only as a labelled post-hoc compatibility diagnostic after the LoCoMo/behavior decision; do not use it for promotion

**Checkpoint**: Verdict is `GO`, `NO-GO`, or honestly `BLOCKED`; no historical
prediction is presented as a new prompt score.

---

## Phase 6: User Story 4 — Integration Handoff (Priority: P4)

**Goal**: Publish a reusable product contract only if promotion evidence passes.

**Independent Test**: A host can use the contract without benchmark metadata;
if gates fail/block, documentation does not recommend enabling it.

- [ ] T025 [US4] Update `specs/038-unified-answer-contract/contracts/answer-contract.md` with the final frozen digest, validated runtime-context inputs, and measured boundaries
- [x] T026 [US4] If and only if the promotion verdict is `GO`, add a product-facing integration link from the appropriate guide; otherwise leave the mode experimental/default-off and document the missing evidence in `docs/evaluation/reports/unified-answer-contract-verdict-2026-08-13.md`

---

## Phase 7: Verification and Handoff

- [ ] T027 Run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`
- [x] T028 Run `CGO_ENABLED=0 go build ./...`
- [ ] T029 Run `CGO_ENABLED=0 go test -count=1 ./...`
- [x] T030 Verify `git diff --name-only -- memory embedding provider store internal` is empty and review all unrelated pre-existing dirty changes for preservation
- [ ] T031 Re-run the commands in `specs/038-unified-answer-contract/quickstart.md`, update task checkboxes truthfully, and summarize completed versus endpoint-blocked work in the verdict

---

## Dependencies & Execution Order

- Phase 1 precedes edits and preserves concurrent work.
- Phase 2 tests precede Phase 3 implementation (TDD).
- Phase 4 depends on the final prompt bytes from Phase 3.
- Phase 5 depends on offline verification and healthy endpoints; T022/T024 are
  conditional on earlier gates.
- Phase 6 promotion is conditional on a measured `GO`.
- Phase 7 always runs for implemented code even if online evaluation is blocked.

## Parallel Opportunities

- T002 and T003 touch separate documentation files.
- T005–T008 can be prepared independently before `runner.go`/`main.go` changes.
- T014–T016 can run after the prompt freezes while code verification proceeds.
- Endpoint preflight and manifest preparation can run in parallel only after a
  current binary exists; no model call starts before both finish.
- T017's development fixtures and their model-backed results never satisfy
  T022's separately authored held-out requirement.

## Implementation Strategy

The MVP is Phases 1–4: one safe, default-off prompt with complete routing,
provenance, and audit. Phase 5 supplies the requested score when infrastructure
exists. A blocked endpoint does not relax any test, reuse rule, or promotion
gate. Product documentation is strictly conditional on Phase 5 `GO`.
