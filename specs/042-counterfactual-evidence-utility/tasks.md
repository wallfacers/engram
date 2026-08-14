# Tasks: Counterfactual Evidence Utility Gate

**Input**: Design documents from `specs/042-counterfactual-evidence-utility/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`

**Tests**: Required. This feature changes evaluation-harness behavior, and the project requires test-first development. Every test task below must fail for the intended reason before its paired implementation task begins.

**Organization**: Tasks are grouped by user story. The protocol is intentionally gated: US2 requires the US1 label-constructor receipt, and US3 is authorized only by a US2 diagnostic GO.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel because it touches different files and has no dependency on an incomplete task.
- **[Story]**: Maps the task to User Story 1, 2, or 3 in `spec.md`.
- All production changes remain under `cmd/locomo-bench/` plus the opt-in flag wiring in `cmd/locomo-bench/main.go`.

---

## Phase 1: Setup and External Preconditions

**Purpose**: Prove the historical inputs and implementation baseline are the intended ones before creating code or spending model budget.

- [ ] T001 On AutoDL, execute the read-only historical-root checks in `specs/042-counterfactual-evidence-utility/quickstart.md` and stop unless `/root/autodl-tmp/032-think3/keep` and `/root/autodl-tmp/topk-full/tk150-full3` each contain exactly `run-1/2/3`, one recognizable hybrid results JSONL per repetition, and comparable question/denominator/judge identities (FR-002, SC-001)
- [ ] T002 Verify `HEAD` contains `3cff168` and `ac5c66c`, confirm the 040/041 files remain absent from `cmd/locomo-bench/`, and record that `git diff --name-only -- memory embedding provider store internal` is empty before implementation, following `specs/042-counterfactual-evidence-utility/plan.md` (FR-015–FR-017, SC-008)

**Checkpoint**: Historical layout is real and the worktree is on the post-cleanup baseline. If T001 fails, stop before Phase 2.

---

## Phase 2: Foundational Protocol Infrastructure

**Purpose**: Establish the default-off CLI boundary, canonical schema, append-only retry journal, and tamper-resistant source chain shared by all stories.

**⚠️ CRITICAL**: No user-story implementation begins until this phase is green.

### Tests (write and observe failure first)

- [ ] T003 [P] Add failing CLI tests for the five-stage closed enum, auxiliary-flag rejection when disabled, mutually exclusive modes, required mem0-aligned/clean-final regime, early offline dispatch, and ordinary-run byte parity in `cmd/locomo-bench/counterfactual_utility_cli_test.go` (FR-013, FR-015–FR-017, SC-008)
- [ ] T004 [P] Add failing artifact-contract tests for manifest immutability, endpoint/config digests, `paired_deep` arm semantics, public/hidden custody, canonical digest/seal validation, bounded retry state transitions, conservative failed-attempt token charges, orphan/tamper rejection, and secret/raw-response exclusion in `cmd/locomo-bench/counterfactual_utility_artifact_test.go` (FR-002, FR-011–FR-013, FR-018, FR-024)

### Implementation

- [ ] T005 Define the v1 constants, closed enums, identities, manifests, call/answer/signal/judge/label/decision/rule/report/seal structs, finite-number validators, and cost fields from `data-model.md` in `cmd/locomo-bench/counterfactual_utility.go` (FR-001, FR-005, FR-011–FR-014, FR-018–FR-024)
- [ ] T006 Implement canonical JSON digests, immutable manifest creation, public/hidden layouts, atomic report/seal writes, append-only logical-call/provider-attempt journal validation, retry resume rules, and per-arm reported/conservative usage aggregation in `cmd/locomo-bench/counterfactual_utility_artifact.go` (FR-002, FR-011–FR-013, FR-018, FR-024)
- [ ] T007 Implement utility option parsing, explicit-flag tracking, stage-specific allowlists/required inputs, ordinary-mode auxiliary rejection, offline-stage early dispatch, and minimal default-off wiring in `cmd/locomo-bench/counterfactual_utility_cli.go` and `cmd/locomo-bench/main.go` (FR-009, FR-013, FR-015–FR-017, FR-021–FR-022)
- [ ] T008 Run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` and `CGO_ENABLED=0 go build ./...` after T005–T007; fix only files under `cmd/locomo-bench/` until the foundational tests in `cmd/locomo-bench/counterfactual_utility_cli_test.go` and `cmd/locomo-bench/counterfactual_utility_artifact_test.go` pass

**Checkpoint**: A default-off, schema-valid utility mode exists, but no label, signal, calibration, or policy behavior is implemented yet.

---

## Phase 3: User Story 1 — Establish Counterfactual Utility Truth Set (Priority: P1) 🎯 MVP

**Goal**: Load strictly comparable historical shallow/deep results and deterministically derive exactly one BENEFIT/NEUTRAL/HARM label per question without any model call.

**Independent Test**: Run the `label` stage twice over frozen three-repetition fixtures; both artifacts are semantically identical, malformed layouts fail closed, and the authorized historical roots reproduce 56 BENEFIT / 31 HARM / 1453 NEUTRAL.

### Tests (write and observe failure first)

- [ ] T009 [US1] Add failing truth-table, three-repetition majority, exact `run-1/2/3` discovery, duplicate/missing/identity/judge-regime rejection, historical-provenance warning, deterministic replay, and 56/31 fixture tests in `cmd/locomo-bench/counterfactual_utility_test.go` (FR-001–FR-003, SC-001, SC-009)

### Implementation

- [ ] T010 [US1] Implement the boolean utility truth table, question-majority pairing, historical root loader, canonical identity/provenance comparison, and deterministic label records in `cmd/locomo-bench/counterfactual_utility.go` and `cmd/locomo-bench/counterfactual_utility_eval.go` (FR-001–FR-003, SC-001)
- [ ] T011 [US1] Implement the zero-model-call `label` stage, label report/seal, `label_constructor_regression_only` claim, 56/31/1453 gate, and CLI dispatch in `cmd/locomo-bench/counterfactual_utility_eval.go`, `cmd/locomo-bench/counterfactual_utility_artifact.go`, and `cmd/locomo-bench/counterfactual_utility_cli.go` (FR-002, FR-009, FR-018, SC-001, SC-009)
- [ ] T012 [US1] Run the focused US1 tests in `cmd/locomo-bench/counterfactual_utility_test.go` plus the package test/build gates; verify the `label` stage does not read provider or judge environment variables
- [ ] T013 [US1] On AutoDL, execute Stage `label` from `specs/042-counterfactual-evidence-utility/quickstart.md`; require the sealed 56/31/1453 GO receipt before authorizing US2 collection, otherwise record valid NO-GO/INVALID and stop (FR-002, FR-009, SC-001, SC-004, SC-009)

**Checkpoint**: The label constructor is independently useful and audited; no historical row is accepted as a probability-signal training or held-out row.

---

## Phase 4: User Story 2 — Diagnose Whether Probability Signals Predict Utility (Priority: P1)

**Goal**: Collect fresh k30/k150 paired repetitions with true final-answer logprob features, then perform leakage-free LOCO calibration and apply the strict diagnostic gate.

**Independent Test**: A fully local fixture produces a sealed fresh collect artifact and ten deterministic cross-fit fold decisions with zero conversation overlap; the report exposes availability, B/N/H outcomes, `D`, `required=max(25,D)`, charged-token cost, undefined fold metrics, and GO/NO-GO/INVALID precedence without held-out label access during rule construction.

### Tests (write and observe failure first)

- [ ] T014 [P] [US2] Add failing non-stream HTTP caller and strict span-mapper tests covering omitted `temperature`, `logprobs=true`, top-2 alternatives, reasoning/content suffix variants, UTF-8 bytes, token-boundary failures, short answers, unavailable reasons, 64 MiB limit, retry classification, and raw-response privacy in `cmd/locomo-bench/counterfactual_utility_http_test.go` (FR-004–FR-005, FR-008, FR-012, FR-024)
- [ ] T015 [P] [US2] Add failing collect/preflight integration tests covering explicit answer/embed/judge identities, store model/dimension fingerprint, two-call embedding determinism, `max_num_seqs=1`, mem0-aligned clean judge input, counting embedder degradation, shallow+`paired_deep` coverage, retry accounting, and label derivation in `cmd/locomo-bench/counterfactual_utility_eval_test.go` (FR-001–FR-008, FR-011–FR-013, FR-017–FR-018, FR-024, SC-002, SC-007)
- [ ] T016 [P] [US2] Add failing deterministic scaler/ridge/threshold tests for zero-variance features, fixed lambda, strict `score > threshold`, never/always sentinels, total charged-token feasibility, lexicographic tie-breaks, class-absence warnings, and numerical failure in `cmd/locomo-bench/counterfactual_utility_calibration_test.go` (FR-003, FR-006–FR-008, FR-023, SC-002, SC-011)
- [ ] T017 [US2] Add failing diagnose tests with a spy label loader for train-only access, full held-out union, cross-fit decision replay, sparse-fold `null + reason`, INVALID precedence, independent +25 and same-batch-deep gates, 90%/60%/category/Holm gates, and quarantined global rule behavior in `cmd/locomo-bench/counterfactual_utility_eval_test.go` (FR-005–FR-009, FR-012–FR-014, FR-018–FR-023, SC-002–SC-004, SC-009, SC-011)

### Implementation

- [ ] T018 [US2] Implement the harness-only non-stream Chat Completions caller, 64 MiB bounded decoder, closed failure taxonomy, maximum-three-attempt executor, sanitized numeric token trace, strict final-suffix mapper, three frozen features, and thinking-only diagnostics in `cmd/locomo-bench/counterfactual_utility_http.go` (FR-004–FR-005, FR-008, FR-012, FR-017, FR-024)
- [ ] T019 [US2] Implement answer/embed/judge provenance freezing, read-only store embedding fingerprint validation, deterministic embedding probe, counting embedding-client wrapper, logprob capability preflight, clean mem0-aligned judge seam, and preflight receipts in `cmd/locomo-bench/counterfactual_utility_eval.go` and `cmd/locomo-bench/counterfactual_utility_cli.go` (FR-004, FR-008, FR-011–FR-013, FR-017–FR-018, SC-002, SC-007)
- [ ] T020 [US2] Implement fresh `collect` orchestration for 1540 questions × 3 repetitions, k30 shallow signal answers, k150 `paired_deep` answers, hidden judge outcomes/utility labels, per-attempt/per-arm cost journals, crash-safe resume, and COMPLETE seal coverage in `cmd/locomo-bench/counterfactual_utility_eval.go` (FR-001–FR-008, FR-011–FR-013, FR-018, FR-024, SC-001–SC-002, SC-007)
- [ ] T021 [US2] Implement training-only feature scaling, fixed three-variable ridge solve, threshold enumeration, majority/cost simulation, deterministic tie-breaks, fold warnings/undefined metrics, and global-transfer-rule quarantine in `cmd/locomo-bench/counterfactual_utility_calibration.go` (FR-003, FR-005–FR-008, FR-012, FR-023, SC-002, SC-011)
- [ ] T022 [US2] Implement the offline `diagnose` public/hidden two-phase loader, ten LOCO rules/decisions, source and leakage validators, fresh `policy_net >= max(25,D)` gate, quality/accuracy/cost/category/statistical gates, diagnostic report/seal, and immediate NO-GO/INVALID stop semantics in `cmd/locomo-bench/counterfactual_utility_eval.go`, `cmd/locomo-bench/counterfactual_utility.go`, and `cmd/locomo-bench/counterfactual_utility_artifact.go` (FR-005–FR-009, FR-012–FR-014, FR-018–FR-023, SC-002–SC-004, SC-009, SC-011)
- [ ] T023 [US2] Run all five counterfactual-utility test files, `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`, and `CGO_ENABLED=0 go build ./...`; verify repeated fixture diagnose artifacts have identical semantic digests and no protected-directory diff
- [ ] T024 [US2] On AutoDL, run fresh detached Stage `collect` exactly as frozen in `specs/042-counterfactual-evidence-utility/quickstart.md`, with run-dir under `/root/autodl-tmp/`; require complete 1540/4620 coverage, embedding/answer/judge preflight receipts, and a valid seal before continuing (FR-001–FR-008, FR-011–FR-013, FR-017–FR-018, FR-024, SC-001–SC-002, SC-007)
- [ ] T025 [US2] Run offline Stage `diagnose` over the sealed fresh collect artifact and enforce the frozen verdict: if it is not GO, record the valid NO-GO/INVALID boundary and mark all of Phase 5 (T026–T036) `not_authorized` without creating US3 tests/code or making model calls; if GO, preserve the ten fold rules and quarantined global rule digests and authorize Phase 5 (FR-009, FR-018–FR-023, SC-003–SC-004, SC-009, SC-011)

**Checkpoint**: US2 either ends the feature honestly at diagnostic NO-GO or provides the only authorization accepted by US3. No feature/model/threshold may be changed after viewing held-out results.

---

## Phase 5: User Story 3 — Gate Shallow→Deep Retrieval and Test Transfer (Priority: P2)

**Goal**: Use frozen fold rules in a fresh LoCoMo conditional run, compare with an interleaved fixed-k150 control, and—only after LoCoMo GO—test the unchanged global rule on LongMemEval.

**Independent Test**: Local fixtures prove one shallow logical call plus at most one policy-deep call, unavailable→forced-deep, sealed label-blind decisions before scoring, full charged-cost accounting, fresh-source authorization, strict LoCoMo gates, and zero-retune LME non-regression.

**Authorization gate**: This entire phase, including tests and implementation T026–T034, MUST NOT begin unless T025 produced a valid diagnostic GO. A diagnostic NO-GO/INVALID closes T026–T036 as `not_authorized`; it does not permit speculative US3 code.

### Tests (write and observe failure first)

- [ ] T026 [P] [US3] Add failing decision/gate/cost tests for keep/deepen/forced-deep actions, `shallow_attempt_id` signal join, one/two logical-call limits, retry attempts, conservative charges, majority aggregation, exact McNemar/paired CI/category/Holm results, and strict quality/90%/60% verdict conjunction in `cmd/locomo-bench/counterfactual_utility_test.go` (FR-008, FR-010–FR-014, FR-018–FR-020, FR-024, SC-005–SC-007)
- [ ] T027 [P] [US3] Add failing fresh-confirm integration tests for fold-rule selection, no collect-answer reuse, interleaved caller parity, decision-before-judge custody, unavailable fallback, source digest checks, conditional-deep limits, and GO/NO-GO seals in `cmd/locomo-bench/counterfactual_utility_eval_test.go` (FR-005, FR-008–FR-020, FR-024, SC-005–SC-009)
- [ ] T028 [P] [US3] Add failing transfer/CLI tests for LoCoMo-GO authorization, global-rule-only loading, no LME scaling/refit/threshold/type routing, LME label blindness, non-regression claims, and portability/production flags in `cmd/locomo-bench/counterfactual_utility_cli_test.go` (FR-015–FR-022, SC-008–SC-010)

### Implementation

- [ ] T029 [US3] Implement label-blind runtime scoring/actions, shallow/deep answer receipt linkage, full per-decision retrieval/embedding/logical-call/provider-attempt/token/latency aggregation, majority quality/flip/statistical summaries, and hard gate evaluation in `cmd/locomo-bench/counterfactual_utility.go` (FR-005, FR-008, FR-010–FR-014, FR-018–FR-020, FR-024, SC-005–SC-007)
- [ ] T030 [US3] Implement fresh LoCoMo `confirm` orchestration with per-conversation fold rules, one shallow plus conditional/forced policy-deep execution, interleaved fixed-k150 control, decision sealing before hidden judge join, source-chain validation, and confirmation reports/seals in `cmd/locomo-bench/counterfactual_utility_eval.go` (FR-005, FR-008–FR-020, FR-024, SC-005–SC-009)
- [ ] T031 [US3] Implement zero-retune LongMemEval `transfer` orchestration using only the LoCoMo global rule and confirmation-GO authorization, with label-blind decisions, fixed-deep control, non-regression verdict, portability claim boundary, and no production authorization in `cmd/locomo-bench/counterfactual_utility_eval.go` (FR-005, FR-008, FR-010–FR-022, FR-024, SC-005–SC-010)
- [ ] T032 [US3] Extend report/seal validators for fresh-run non-reuse, policy/control arm coverage, decision replay, logical-call and retry limits, query-embedding counters, judge separation, LoCoMo mechanism claims, and transfer claims in `cmd/locomo-bench/counterfactual_utility_artifact.go` (FR-011–FR-013, FR-018–FR-022, FR-024, SC-005–SC-010)
- [ ] T033 [US3] Complete `confirm`/`transfer` required-input, source-authorization, dataset-format, no-retune, and early-dispatch wiring in `cmd/locomo-bench/counterfactual_utility_cli.go` and `cmd/locomo-bench/main.go` (FR-009, FR-013, FR-015–FR-022, SC-008–SC-010)
- [ ] T034 [US3] Run all counterfactual-utility tests, the complete `cmd/locomo-bench` package tests, and the full build; verify default-off requests/results are byte-identical and protected directories remain untouched
- [ ] T035 [US3] Only if T025 produced diagnostic GO, run fresh detached LoCoMo Stage `confirm` from `specs/042-counterfactual-evidence-utility/quickstart.md`; otherwise record `not_authorized_by_diagnostic` without model calls. Require policy correct ≥ same-batch k150, accuracy ≥90%, charged-token ratio ≤0.60, and category gates for LoCoMo GO (FR-009–FR-020, SC-004–SC-009)
- [ ] T036 [US3] Only if T035 produced LoCoMo GO, run detached LongMemEval Stage `transfer` with the unchanged global LoCoMo rule from `specs/042-counterfactual-evidence-utility/quickstart.md`; otherwise record `not_authorized_by_locomo`. Require policy correct ≥ same-batch fixed-deep for portable GO and never alter the LoCoMo mechanism verdict (FR-021–FR-022, SC-010)

**Checkpoint**: The feature has an honest LoCoMo mechanism verdict and, if authorized, a separate transfer verdict. Neither result authorizes production integration.

---

## Phase 6: Polish and Cross-Cutting Validation

**Purpose**: Close privacy, reproducibility, default-parity, build, and evaluation-audit gates across all stories.

- [ ] T037 [P] Add adversarial regression cases for credential/endpoint/raw-body leakage, digest tampering, duplicate identities, non-finite numerics, post-completion retries, label-before-decision access, and ordinary-mode artifact absence across `cmd/locomo-bench/counterfactual_utility_artifact_test.go`, `cmd/locomo-bench/counterfactual_utility_eval_test.go`, and `cmd/locomo-bench/counterfactual_utility_cli_test.go` (FR-002, FR-005, FR-011–FR-018, FR-024, SC-008–SC-009)
- [ ] T038 Run `gofmt` on `cmd/locomo-bench/counterfactual_utility*.go` and `cmd/locomo-bench/main.go`, then run `git diff --check`
- [ ] T039 Run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` and preserve the exact failure output if any test fails
- [ ] T040 Run `CGO_ENABLED=0 go test -count=1 ./...`, `CGO_ENABLED=0 go build ./...`, and `CGO_ENABLED=0 go vet ./...`; no skipped failure may be reported as verified
- [ ] T041 Verify `git diff --name-only -- memory embedding provider store internal` is empty and review the complete `cmd/locomo-bench/main.go` diff for opt-in-only behavior (Constitution II/IV, FR-015–FR-017, SC-008)
- [ ] T042 Replay Stage `diagnose` into a fresh directory using `specs/042-counterfactual-evidence-utility/quickstart.md` and require identical fold-rule, crossfit-decision, report semantic, and source-chain digests (SC-001–SC-002, SC-009, SC-011)
- [ ] T043 Audit the final label/collect/diagnose/confirm/transfer reports and seals against `specs/042-counterfactual-evidence-utility/contracts/utility-artifacts.md`; state which conditional stages were not authorized, separate historical/fresh/transfer claims, confirm `production_authorized=false`, and stop the metered GPU when model work is complete (FR-018–FR-022, SC-003–SC-010)

---

## Dependencies and Execution Order

### Phase Dependencies

- **Phase 1**: Starts immediately, but T001 requires live AutoDL access. A failed historical-root check stops the workflow.
- **Phase 2**: Depends on Phase 1 and blocks all user stories.
- **US1 / Phase 3**: Depends on Phase 2; T013 is the only label receipt accepted by collect.
- **US2 / Phase 4**: Depends on US1 GO. Tests T014–T016 can be authored in parallel; T017 follows the shared eval-test changes. Implementation runs T018 → T019 → T020 and T021, then T022.
- **US3 / Phase 5**: The entire phase T026–T036 starts only after a valid diagnostic GO at T025. T036 additionally requires LoCoMo GO at T035. Diagnostic NO-GO/INVALID means no US3 test/code files or model calls are created.
- **Phase 6**: Depends on all locally applicable story implementation tasks and records any protocol-authorized early stop.

### User Story Dependencies

- **US1 (P1)**: Independent zero-model MVP after foundation.
- **US2 (P1)**: Requires the sealed US1 constructor-audit GO receipt; historical labels are provenance only, never training rows.
- **US3 (P2)**: Requires US2 diagnostic GO before any US3 tests, code, or LoCoMo execution; LME additionally requires LoCoMo confirmation GO.

### Within Each User Story

- Write each test task and observe the expected failure before its paired implementation.
- Complete identity/schema primitives before runners, runners before reports/seals, and public decision sealing before hidden scoring.
- A valid NO-GO completes the authorized scope; every downstream task—not only model-run tasks—is closed as `not_authorized`, not executed or pre-implemented.
- INVALID never becomes NO-GO and never authorizes a downstream stage.

### Parallel Opportunities

- T003 and T004 touch separate test files.
- T014, T015, and T016 touch separate test files; T017 follows T015 because both modify the eval test file.
- T026, T027, and T028 touch separate test files.
- T037 spans multiple files and is parallel only with documentation/artifact audit work that does not touch those tests.

---

## Parallel Examples

### User Story 2

```text
Task T014: HTTP request/response, retry, size-limit, and span-mapping tests
Task T015: collect/preflight/provenance/cost integration tests
Task T016: fixed ridge and threshold-selection tests
```

### User Story 3

```text
Task T026: decision, gate, statistics, and cost tests
Task T027: fresh confirmation runner tests
Task T028: transfer authorization and no-retune CLI tests
```

---

## Implementation Strategy

### MVP First

1. Complete Phase 1 and Phase 2.
2. Complete US1 and obtain the historical 56/31/1453 constructor receipt.
3. Stop and validate the zero-model label artifact independently.

### Incremental Delivery

1. Foundation → immutable protocol and default-off boundary.
2. US1 → deterministic label constructor and historical audit.
3. US2 → fresh probability-signal corpus and cross-fit diagnostic; stop on NO-GO.
4. US3 → fresh LoCoMo mechanism confirmation; run LME only after LoCoMo GO.
5. Cross-cutting replay/build/privacy audit and explicit claim boundary.

### Cost Discipline

- All long AutoDL commands use detached execution and data-disk run directories from `quickstart.md`.
- No hosted reranker/recall model is introduced.
- A failed gate prevents the next metered stage; it never triggers feature expansion or post-hoc retuning.

---

## Notes

- Every task follows the required checkbox/ID/story/path format.
- `[P]` is used only for different files without an incomplete dependency.
- Formal fixture tests may use small denominators but must set `fixture=true` and can never produce GO.
- Mark tasks complete only after the stated test/gate is actually satisfied; preserve exact failure output for any stopped task.
