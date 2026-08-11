# Tasks: Risk-Controlled Second-Pass Adjudication

**Input**: Design documents from `specs/035-risk-controlled-adjudication/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`

**Tests**: Required. This feature changes benchmark decision behavior, so each story starts with failing offline contract/unit tests before implementation.

**Organization**: Tasks are grouped by user story. Hosted calls are a final conditional execution phase and are forbidden until every offline integrity gate passes.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel because it touches a different file and does not depend on incomplete work
- **[Story]**: Maps the task to a user story from `spec.md`
- Every task names the exact repository path it reads or changes

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm isolation and inventory the frozen 034 seams before any implementation edit.

- [X] T001 Verify the 035 worktree/base/active-feature pointer and record that no sibling or engine changes overlap `specs/035-risk-controlled-adjudication/plan.md`
- [X] T002 Inventory reusable frozen 034 validators, provider wiring, canonical JSON, usage, statistics, and hidden-loader seams in `cmd/locomo-bench/answer_adjudication*.go` and `cmd/locomo-bench/main.go`
- [X] T003 Confirm the parent Stage-0 artifact directory has the frozen manifest/packet/call/decision/seal receipts without opening hidden score/custody/candidate files, using the operator scratch path documented in `specs/035-risk-controlled-adjudication/quickstart.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Freeze additive file boundaries and the global mode contract before user-story behavior.

**⚠️ CRITICAL**: No production behavior is added in this phase; story tests must fail before their implementations.

- [X] T004 Add compile-only 035 test scaffolding and injectable caller/hidden-loader seams in `cmd/locomo-bench/answer_adjudication_audit_test.go` and `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T005 Write failing table tests for all eight 034/035 modes being mutually exclusive and for mode-owned flag rejection in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`

**Checkpoint**: Test harness is ready and the first contract tests fail for missing 035 behavior.

---

## Phase 3: User Story 1 — Freeze the Label-Blind Risk Queue (Priority: P1) 🎯 MVP

**Goal**: Validate the frozen 034 public chain and deterministically build/validate 477 provider-safe risk packets plus 1540 resolver rows with zero model calls.

**Independent Test**: Build twice with reordered source reads and altered/unavailable hidden files; both public outputs are byte-identical, counts are 424 + 53 + 1063, and tampered parent/public inputs fail before any provider or hidden access.

### Tests for User Story 1

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T006 [US1] Write failing strict-schema, canonical-digest, tamper, unknown-field, and forbidden-field tests for manifest/packets/resolver artifacts in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T007 [US1] Write failing grouping tests for exact three-candidate coverage, normalized duplicate collapse, label-blind representative selection, and 2/3-group invariants in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T008 [US1] Write failing view tests for seeded determinism, two-view derangement, stable evidence order, view-local slots, and hidden current/control/source/multiplicity absence in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T009 [US1] Write failing parent-replay and queue tests for 1540/771/718/822, 424 override, 53 triggered fallback, 31 normalized-same exclusions, and hidden-loader zero reads in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T010 [US1] Write failing CLI build/validate tests for destination refusal, zero provider access, seed requirements, exact receipts, and byte-stable rebuilds in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`

### Implementation for User Story 1

- [X] T011 [US1] Define strict v1 manifest, packet, view, answer-group, parent-receipt, and resolver-map schemas plus canonical digest/I/O helpers in `cmd/locomo-bench/answer_adjudication_audit_artifact.go`
- [X] T012 [US1] Implement normalized answer grouping, label-blind representative selection, seeded entailment ordering, rotate-one falsification ordering, packet construction, and recursive provider-field safety checks in `cmd/locomo-bench/answer_adjudication_audit.go`
- [X] T013 [US1] Implement full frozen 034 manifest/packet/call/decision/seal replay validation and exact parent receipt binding without hidden inputs in `cmd/locomo-bench/answer_adjudication_audit_cli.go`
- [X] T014 [US1] Implement the 424/53 label-blind queue rule, 1063 non-risk resolver rows, atomic build writes, and strict offline build-artifact validation in `cmd/locomo-bench/answer_adjudication_audit_cli.go`
- [X] T015 [US1] Add the four audit modes, source/seed/paid/max-token flags, unified eight-mode exclusivity, mode-owned argument checks, and pre-`--data` build/validate dispatch in `cmd/locomo-bench/main.go`
- [X] T016 [US1] Run and fix the focused US1 tests plus `CGO_ENABLED=0 go build ./...` for `cmd/locomo-bench/answer_adjudication_audit*_test.go` and the repository build graph

**Checkpoint**: US1 independently materializes and validates the exact frozen queue offline with no hidden/provider access.

---

## Phase 4: User Story 2 — Dual-View Evidence Audit and Conservative Resolution (Priority: P1)

**Goal**: Execute exactly two one-attempt blind assessments per risk packet and switch only on strict dual convergence; otherwise retain the parent answer.

**Independent Test**: An offline stub run schedules 954 attempts at bounded concurrency, produces deterministic terminal/decision/seal sets under shuffled completion, and only the exact dual-support/contradiction pattern switches an existing answer group.

### Tests for User Story 2

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T017 [US2] Write failing prompt and strict-response tests for every-slot coverage, separate support/contradiction fields, evidence-ID rules, extra/trailing/free-text rejection, and provider-field leakage in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T018 [US2] Write failing resolver tables for dual convergence, disagreement, current support, conflicting evidence, multiple supported alternatives, invalid/failed views, duplicate groups, and exact parent-answer retention in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T019 [US2] Write failing journal tests for STARTED-before-call, one terminal, `(packet,view)` identity, orphan/duplicate rejection, terminal-only resume, raw output/error exclusion, and canonical schedule-independent terminal digest in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T020 [US2] Write failing runner tests for 477 x 2 scheduling, no adaptive short-circuit, one attempt/zero retry, concurrency bound 32, provider/parse/usage terminal failures, and crash behavior in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T021 [US2] Write failing decision/seal tests for 1540 ordered decisions, 954 starts/terminals/attempts, zero retry, resolver recomputation, immutable existing outputs, provider identity digests, usage/pricing, and deterministic seal bytes in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`

### Implementation for User Story 2

- [X] T022 [US2] Implement role-separated provider prompts and strict closed-JSON assessment parsing/validation in `cmd/locomo-bench/answer_adjudication_audit.go`
- [X] T023 [US2] Implement the default-retain dual-view resolver, closed resolution reasons, representative existing-answer switching, and deterministic decision construction in `cmd/locomo-bench/answer_adjudication_audit.go`
- [X] T024 [US2] Implement additive audit call records, fsynced append-only journal, resume/orphan validation, canonical terminal-state hashing, second-pass decision schemas, and seal validation in `cmd/locomo-bench/answer_adjudication_audit_artifact.go`
- [X] T025 [US2] Implement explicit-paid dedicated `ADJUDICATOR_*` configuration, safe identity/price parsing, fixed 954-unit worker scheduling, one-attempt provider calls, terminal failure closure, deterministic decisions, and sealing in `cmd/locomo-bench/answer_adjudication_audit_cli.go`
- [X] T026 [US2] Add run dispatch before ordinary benchmark validation while preserving all 034/default behavior in `cmd/locomo-bench/main.go`
- [X] T027 [US2] Run and fix the focused US1+US2 tests plus `CGO_ENABLED=0 go build ./...` for `cmd/locomo-bench/answer_adjudication_audit*_test.go` and the repository build graph

**Checkpoint**: US2 independently proves the complete fixed-cost run/resolution/seal path with offline callers.

---

## Phase 5: User Story 3 — Post-Seal Scoring and Strict Stop-Loss (Priority: P2)

**Goal**: Validate both artifact chains before hidden access, recompute the 034 baseline, and issue only a strict historical-mapping GO/NO-GO result.

**Independent Test**: Invalid/incomplete/tampered seals cause zero hidden-loader reads; a valid fixture exactly recomputes 1378/1540 and 61/88, paired/category/instability metrics, and every FR-012 gate boundary.

### Tests for User Story 3

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T028 [US3] Write failing score-boundary tests proving parent + 035 validation precede hidden loading and invalid seal/journal/decision/custody states produce zero hidden reads and no score in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T029 [US3] Write failing metric fixtures for recomputed parent 1378/1540, mixed 61/88, point/best/worst 13/5 instability bounds, paired flips, exact two-sided McNemar, category tests/Holm, and temporal delta in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T030 [US3] Write failing GO/NO-GO boundary tables for 1387/1540, 69/88, paired net 9, p<0.05, temporal non-regression, Holm-negative-category rejection, integrity gates, and fixed `historical_verdict_mapping` in `cmd/locomo-bench/answer_adjudication_audit_test.go`
- [X] T031 [US3] Write failing score CLI tests for exactly three candidate journals, parent custody matching, refusal to read old Stage-0 score, atomic output, and no formal-score claim in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`

### Implementation for User Story 3

- [X] T032 [US3] Implement audit score schemas, paired/category/instability aggregation, exact McNemar/Holm reuse, temporal safeguard, and strict gate evaluation in `cmd/locomo-bench/answer_adjudication_audit.go`
- [X] T033 [US3] Implement validation-first hidden loading, frozen parent mapping recomputation, candidate custody join, atomic `audit-stage0-score.json`, and fail-closed score orchestration in `cmd/locomo-bench/answer_adjudication_audit_cli.go`
- [X] T034 [US3] Add score dispatch and score-only candidate/source argument validation without changing default/034 dispatch in `cmd/locomo-bench/main.go`
- [X] T035 [US3] Run and fix all focused audit tests plus `CGO_ENABLED=0 go build ./...` for `cmd/locomo-bench/answer_adjudication_audit*_test.go` and the repository build graph

**Checkpoint**: All three stories work offline and hidden inputs remain cryptographically downstream of a valid audit seal.

---

## Phase 6: Polish, Regression Gates, and Offline Materialization

**Purpose**: Prove repository safety and materialize the real 035 queue before any paid execution.

- [X] T036 Run `gofmt` on `cmd/locomo-bench/answer_adjudication_audit*.go` and validate docs/tasks formatting with `git diff --check` plus the checklist in `specs/035-risk-controlled-adjudication/checklists/requirements.md`
- [X] T037 Run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` and detached `CGO_ENABLED=0 go test -count=1 ./...`, recording only exit status/log paths in the session scratchpad described by `specs/035-risk-controlled-adjudication/quickstart.md`
- [X] T038 Run `CGO_ENABLED=0 go build ./...`, verify no changes under `memory/ embedding/ provider/ store/ internal/`, and compare frozen 034/default CLI tests in `cmd/locomo-bench/answer_adjudication_audit_cli_test.go`
- [X] T039 Build and validate the real 035 artifacts from the frozen 034 directory into a new session-scratch directory, then confirm exact 1540/477/424/53/1063/954 receipts and byte-stable rebuilds following `specs/035-risk-controlled-adjudication/quickstart.md`
- [X] T040 Scan tracked diffs and real public artifacts for credentials, raw endpoints/errors/responses, hidden labels, provider-facing current/control/source/multiplicity fields, and forbidden path leakage against `specs/035-risk-controlled-adjudication/contracts/artifact-schemas.md`

---

## Phase 7: Conditional Paid Stage-0 and Documentation

**Purpose**: Spend only after T036–T040 pass and the operator approves the frozen 954-call manifest.

- [X] T041 Confirm `planned_calls=954`, concurrency 32, valid offline build, adequate scratch storage, and a newly rotated dedicated `ADJUDICATOR_*` credential is set without printing any value, then record approval status in `specs/035-risk-controlled-adjudication/tasks.md`
- [X] T042 If and only if T041 passes, launch the 954-call run detached, poll without blocking, validate zero retry/orphan/duplicate and 1540 sealed decisions, and keep all logs/artifacts in the session-scratch directory documented by `specs/035-risk-controlled-adjudication/quickstart.md`
- [X] T043 If and only if T042 yields a valid seal, run post-seal scoring, record the exact GO/NO-GO gates and failure analysis in `specs/035-risk-controlled-adjudication/quickstart.md`, and do not initiate a formal rejudge from this feature
- [X] T044 Re-run focused/full tests, build, diff/secret/engine gates, mark completed tasks faithfully, and summarize the historical-only result in `specs/035-risk-controlled-adjudication/tasks.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: Starts immediately.
- **Foundational (Phase 2)**: Depends on Setup and blocks all story implementation.
- **US1 (Phase 3)**: Depends on Foundational; produces the immutable queue/build contract.
- **US2 (Phase 4)**: Depends on validated US1 build artifacts and mappings.
- **US3 (Phase 5)**: Depends on US1 artifacts and US2 decisions/seal, but its metric tests can use sealed fixtures.
- **Polish/offline materialization (Phase 6)**: Depends on all three stories.
- **Paid Stage-0 (Phase 7)**: Depends on every offline task T036–T040 and explicit T041 approval; T043 depends on a valid T042 seal.

### User Story Dependencies

- **US1 (P1)**: Foundational only; independently valuable as a zero-call frozen queue builder/validator.
- **US2 (P1)**: Consumes US1 packet/resolver contracts; independently testable with offline callers and no hidden labels.
- **US3 (P2)**: Consumes validated US1+US2 seals; independently testable with sealed fixtures and a spy hidden loader.

### Within Each User Story

- Write each listed test and observe the expected failure before its implementation task.
- Artifact schemas/canonical validation precede orchestration.
- Core algorithms precede CLI dispatch.
- Focused tests and `CGO_ENABLED=0 go build ./...` must pass at each story checkpoint.
- Existing 034 source remains frozen; new logic stays in additive 035 files except minimal `main.go` flags/dispatch.

### Parallel Opportunities

- T006–T008 are test cases in the same file and should be serialized when one agent owns that file; T009–T010 can proceed independently in the CLI test file.
- T011 artifact schemas and T012 pure grouping/view logic touch different production files after their tests are frozen.
- T017–T018 pure prompt/resolver tests can proceed while T019–T021 journal/runner/seal tests are authored in the CLI test file.
- T022/T023 pure logic and T024 artifact/journal implementation touch different files after contracts are stable.
- T029–T030 metric/gate tests can proceed independently of T028/T031 CLI boundary tests.
- No paid call task is parallelizable with implementation, validation, or manifest review.

## Parallel Example: User Story 2

```text
Task T017/T018 owner: cmd/locomo-bench/answer_adjudication_audit_test.go
Task T019/T020/T021 owner: cmd/locomo-bench/answer_adjudication_audit_cli_test.go

After tests are frozen:
Task T022/T023 owner: cmd/locomo-bench/answer_adjudication_audit.go
Task T024 owner:      cmd/locomo-bench/answer_adjudication_audit_artifact.go
```

## Implementation Strategy

### MVP First (US1 Only)

1. Complete Setup and Foundational.
2. Write failing US1 contracts.
3. Implement the zero-call build/validate path.
4. Stop and verify exact 477/954 queue receipts and hidden/provider zero access.

### Incremental Delivery

1. US1 freezes provider-safe inputs.
2. US2 adds only the offline-testable runner/resolver/seal path.
3. US3 adds only validation-first historical scoring.
4. Full repository/offline materialization gates run before any spend.
5. The paid Stage-0 runs once, with no retries or adaptive selection, only after explicit manifest approval.

## Notes

- Hosted answer-side adjudication is benchmark-only and default-off; it is not a reranker or shipped scoring lever.
- A failed/invalid audit is a terminal retain, while structural artifact corruption fails the run closed.
- GO remains exploratory historical mapping and only authorizes a separately specified formal paired-rejudge.
- Never print or persist credential values, raw endpoint URLs, provider responses, or raw provider errors.

## Completion Record (2026-08-10)

- The single approved paid run sealed 954/954 attempts with zero retries: 940 completed, 14 invalid responses, 1540
  decisions, 1539 retains, and one dual-convergence switch. The seal and frozen diagnostics are valid.
- Historical scoring is `NO_GO`: parent and new are both 1378/1540, instability lower bound is 1375, triggered mixed is
  61/88 with lower bound 60, new-only/parent-only are 0/0, exact McNemar p=1.0, and temporal/category deltas are zero.
  No formal rejudge was started.
- Final gates passed: focused `cmd/locomo-bench` and full-repository tests exit 0, `CGO_ENABLED=0 go build ./...` and
  `go vet ./cmd/locomo-bench` exit 0, `git diff --check` is clean, engine-directory changes are zero, and tracked/public
  artifact credential plus forbidden-field scans report zero hits. Detached test logs are retained under the session
  scratchpad run `035-merge-gate.OkuQeN`.
- Documentation lifecycle closed as **NO-GO** on 2026-08-11. All 44 tasks remain complete; no deferred implementation
  is carried inside 035. The next diagnostic scope is independently specified by
  [036 decision-gap attribution](../036-decision-gap-attribution/spec.md).
