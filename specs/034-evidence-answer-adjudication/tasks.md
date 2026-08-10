# Tasks: Evidence-Grounded Answer Adjudication

**Input**: Design documents from `specs/034-evidence-answer-adjudication/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`

**Tests**: Required by the feature's offline contract scenarios and repository TDD rules. For every test task below,
write the test first, run the focused package test, and record the expected failure before implementing its paired code.

**Organization**: Tasks are grouped by user story. US1 freezes label-blind inputs; US2 selects/falls back and seals; US3
joins hidden verdicts only after seal validation.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel because it touches a different file and has no dependency on an incomplete task
- **[Story]**: User story from `spec.md`

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create isolated source seams without changing the ordinary benchmark path.

- [X] T001 Create compile-only adjudication source/test files declared in the plan at `cmd/locomo-bench/answer_adjudication.go`, `cmd/locomo-bench/answer_adjudication_artifact.go`, `cmd/locomo-bench/answer_adjudication_cli.go`, `cmd/locomo-bench/answer_adjudication_test.go`, and `cmd/locomo-bench/answer_adjudication_cli_test.go`
- [X] T002 Add additive adjudication option fields, mutually exclusive flags, and pre-`--data` dispatch stubs in `cmd/locomo-bench/main.go` while proving no-mode option parsing remains unchanged in `cmd/locomo-bench/bench_test.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Freeze shared schemas, canonical encoding, strict I/O, and error semantics used by all stories.

**⚠️ CRITICAL**: No story implementation starts until these contracts compile and their focused tests pass.

- [X] T003 [P] Write failing canonical digest, numeric `(conv,q)` ordering, strict JSON/JSONL, atomic-write, and duplicate-record tests in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T004 Implement v1 artifact structs, canonical digests, numeric ordering, strict scanners, atomic writers, and closed error codes in `cmd/locomo-bench/answer_adjudication_artifact.go` to pass T003
- [X] T005 Implement shared CLI mode/argument validation from `contracts/adjudication-cli.md` in `cmd/locomo-bench/answer_adjudication_cli.go` and add table tests in `cmd/locomo-bench/answer_adjudication_cli_test.go`

**Checkpoint**: Shared artifact and CLI foundations are deterministic and offline.

---

## Phase 3: User Story 1 — Freeze Label-Blind Adjudication Inputs (Priority: P1) 🎯 MVP

**Goal**: Materialize and validate 1540 deterministic packets from sanitized candidate/trace/store inputs without
loading hidden labels into execution artifacts or making model calls.

**Independent Test**: Build twice from a small fixture, then mutate gold/correct/verdict and reorder/rename candidate
sources. `manifest.json`, `packets.jsonl`, trigger cohort, slot permutation, and planned calls remain byte-identical;
malformed/missing/duplicate evidence fails closed.

### Tests for User Story 1 — write and fail first

- [X] T006 [P] [US1] Write failing tests for ASCII normalization, 769/405/366 candidate-shape behavior, 2:1 support visibility, three-way lexical fallback, sanitized-source canonicalization, deterministic permutation, and hidden-label/source-order mutation invariance in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T007 [P] [US1] Write failing strict candidate/trace tests that reject malformed lines, duplicate/missing questions, metadata drift, non-dense ranks, duplicate hits, empty answers, and forbidden packet fields in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T008 [P] [US1] Write failing immutable-store integration tests covering `mode=ro&immutable=1`, public `memory.EntryStore` lookup, event/recorded rendering, missing entry rejection, WAL/SHM rejection, and unchanged source digest in `cmd/locomo-bench/answer_adjudication_cli_test.go`
- [X] T009 [US1] Write a failing end-to-end build/validate CLI test that asserts deterministic public bytes, zero provider access, private/public separation, context-parity receipts, and count recomputation in `cmd/locomo-bench/answer_adjudication_cli_test.go`

### Implementation for User Story 1

- [X] T010 [US1] Implement sanitized candidate parsing, normalization, canonical source/candidate identities, label-blind trigger, deterministic text control, and blinded permutation in `cmd/locomo-bench/answer_adjudication.go` to pass T006–T007
- [X] T011 [US1] Implement sanitized attribution parsing plus immutable public-entry-store evidence resolution and semantic/store receipts in `cmd/locomo-bench/answer_adjudication_cli.go` to pass T008
- [X] T012 [US1] Implement packet, label-free score-only slot map, custody, execution manifest, forbidden-field scan, and exact-set validation in `cmd/locomo-bench/answer_adjudication_artifact.go`
- [X] T013 [US1] Implement `--adjudication-build` and `--adjudication-validate` orchestration in `cmd/locomo-bench/answer_adjudication_cli.go` and wire the early dispatch in `cmd/locomo-bench/main.go` to pass T009
- [X] T014 [US1] Run the offline historical materialization into session scratch, confirm only public build facts (1540/771, 1532/766 context parity, 46,200 resolved hits, zero forbidden fields/calls, unchanged stores), and record non-secret digests/counts in `specs/034-evidence-answer-adjudication/diagnosis/build-receipt.md`; leave 13/5 hidden-label recomputation to US3

**Checkpoint**: US1 independently produces validated provider-ready packets with zero hidden execution fields and zero
network calls.

---

## Phase 4: User Story 2 — Evidence-Based Selection and Safe Fallback (Priority: P1)

**Goal**: Run a verifier over triggered public packets only, accept only cited high-confidence existing-slot selections,
fallback deterministically for every invalid path, and create a complete crash-auditable seal.

**Independent Test**: An offline injected stub covers valid selection, candidate permutation, invalid/free answer, bad or
empty citation, low confidence, provider error/timeout, non-trigger zero-call behavior, concurrency 32, resume, and orphan
STARTED rejection; all 1540 outputs seal deterministically without network.

### Tests for User Story 2 — write and fail first

- [X] T015 [P] [US2] Write failing strict verifier-response and prompt tests for exact JSON, closed keys, valid `C1..C3`, present `E01..E30`, non-empty citations, literal `high`, no fourth answer, and deterministic fallback in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T016 [P] [US2] Write failing call-journal tests for fsynced STARTED→COMPLETED/FAILED, one attempt, terminal fallback, duplicate/reordered records, orphan STARTED refusal, identity drift, and resume semantics in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T017 [P] [US2] Write failing runner tests with an injected stub for zero non-trigger calls, valid/error/timeout fallbacks, maximum in-flight 32, schedule-independent sealed order/digest, usage totals, and unpriced-vs-priced cost receipts in `cmd/locomo-bench/answer_adjudication_cli_test.go`
- [X] T018 [US2] Write failing CLI security tests that require `--adjudication-allow-paid`, complete `ADJUDICATOR_*` configuration, safe base URL shape, and prove API keys/raw provider errors/private slot map never enter output in `cmd/locomo-bench/answer_adjudication_cli_test.go`

### Implementation for User Story 2

- [X] T019 [US2] Implement verifier prompt construction, strict response decoding, citation/slot/confidence validation, and packet-only deterministic fallback in `cmd/locomo-bench/answer_adjudication.go` to pass T015
- [X] T020 [US2] Implement append-only call journal, terminal decision validation, resume refusal rules, sorted sealed decision set, and seal receipts in `cmd/locomo-bench/answer_adjudication_artifact.go` to pass T016
- [X] T021 [US2] Implement provider-independent concurrent adjudication runner with one attempt per trigger, usage/cost accounting, and all fallback paths in `cmd/locomo-bench/answer_adjudication_cli.go` to pass T017
- [X] T022 [US2] Implement dedicated `ADJUDICATOR_*` provider wiring, secret-safe configuration, explicit paid acknowledgement, and `--adjudication-run` dispatch in `cmd/locomo-bench/answer_adjudication_cli.go` and `cmd/locomo-bench/main.go` to pass T018
- [X] T023 [US2] Execute an all-offline 1540-packet stub run at concurrency 32, validate exactly 771 attempts and a deterministic seal, and record the non-secret receipt in `specs/034-evidence-answer-adjudication/diagnosis/offline-seal-receipt.md`

**Checkpoint**: US2 independently seals a complete selection set; hosted execution remains explicit opt-in and has not
been used merely by running tests/build/validate.

---

## Phase 5: User Story 3 — Post-Seal Historical Score and Promotion Gate (Priority: P2)

**Goal**: Prove hidden verdicts are loaded only after seal validation, reproduce all frozen diagnostics, report
judge-instability sensitivity, and issue a strict diagnostic GO/NO-GO without calling the mapping a formal score.

**Independent Test**: A spy hidden loader remains untouched for every invalid seal. A valid fixture reproduces historical
majority, executable control, oracle, mixed/instability strata, paired/category statistics, and exact promotion gates;
missing/extra/duplicate/tampered data fails closed.

### Tests for User Story 3 — write and fail first

- [X] T024 [P] [US3] Write failing seal-first tests with a spy hidden loader for missing/extra/duplicate decisions, packet/decision/protocol digest drift, orphan calls, invalid counts, custody drift, and zero hidden reads before validation in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T025 [P] [US3] Write failing scoring tests for 1371 verdict-majority, 1368 deterministic text control, 1411 oracle, 96/88 mixed strata, 13/5 instability, 771/766/5 trigger strata, selected sensitivity bounds, paired flips, exact McNemar, and Holm category results in `cmd/locomo-bench/answer_adjudication_test.go`
- [X] T026 [US3] Write failing gate tests for ≥1387/1540, ≥69/88, category non-regression, contamination/integrity completeness, digest-specific denominator refusal, and fixed `historical_verdict_mapping` labelling in `cmd/locomo-bench/answer_adjudication_test.go`

### Implementation for User Story 3

- [X] T027 [US3] Implement seal-first validation and strict hidden candidate/verdict loading with custody verification in `cmd/locomo-bench/answer_adjudication_artifact.go` to pass T024
- [X] T028 [US3] Implement historical majority/control/oracle, mixed/context-parity/instability strata, sensitivity bounds, paired/category metrics, and reusable exact statistics in `cmd/locomo-bench/answer_adjudication.go` to pass T025
- [X] T029 [US3] Implement Stage-0 validity/promotion gates plus `stage0-score.json` emission in `cmd/locomo-bench/answer_adjudication.go` to pass T026
- [X] T030 [US3] Implement `--adjudication-score` orchestration and post-seal-only slot-map/raw-journal access in `cmd/locomo-bench/answer_adjudication_cli.go` and wire its early dispatch in `cmd/locomo-bench/main.go`

**Checkpoint**: All three stories work offline; the workflow is ready for an explicitly authorized hosted Stage-0 run.

---

## Phase 6: Operational Stage-0 and Cross-Cutting Verification

**Purpose**: Verify repository safety, then execute paid work only with a newly rotated environment credential.

- [X] T031 Run `gofmt` on all touched Go files, then `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`; record exact outputs in `specs/034-evidence-answer-adjudication/diagnosis/verification.md`
- [X] T032 [P] Verify `git diff --name-only -- memory embedding provider store internal` is empty, ordinary no-mode CLI tests pass, source DB digests/sidecars are unchanged, and secret scans find no credential values; record results in `specs/034-evidence-answer-adjudication/diagnosis/verification.md`
- [X] T033 [P] Update `specs/034-evidence-answer-adjudication/quickstart.md` against the implemented flags/artifacts and run every offline command from a session scratch directory
- [X] T034 With a newly rotated `ADJUDICATOR_API_KEY` supplied only in the operator environment, revalidate the sealed protocol, review the 771-call manifest, and execute `--adjudication-run` detached at concurrency 32; never reuse the chat-exposed key and record only non-secret usage/cost receipts in `specs/034-evidence-answer-adjudication/diagnosis/paid-run-receipt.md`
- [X] T035 Run `--adjudication-score` only after T034 seals, record GO/NO-GO plus full 771/766/5 and 13/5 sensitivity in `specs/034-evidence-answer-adjudication/diagnosis/verdict.md`, and stop without formal rejudge on NO-GO
- [X] T036 N/A by its frozen condition: T035 is NO-GO, so no formal paired-rejudge spec/protocol was created; the stop decision and any future label-blind hypothesis are recorded in `specs/034-evidence-answer-adjudication/diagnosis/verdict.md`

---

## Dependencies & Execution Order

### Phase dependencies

- Phase 1 → Phase 2 → US1 are strictly sequential foundations.
- US2 depends on US1 packets but remains independently testable with fixture packets.
- US3 depends on the seal contract but remains independently testable with fixture decisions and a spy hidden loader.
- T034 depends on every offline gate T001–T033 and a newly rotated environment credential.
- T035 depends on a valid T034 seal; T036 is conditional on T035 GO.

### User story dependency graph

```text
Setup → Foundation → US1 packet freeze → US2 run/seal → US3 post-seal score
                                      └──────────────→ US3 fixture tests
```

### Parallel opportunities

- T003 and the documentation review can proceed independently, but T004 owns the shared artifact file.
- Within US1, T006–T008 are parallel failing-test slices before T010–T012 implementation.
- Within US2, T015–T017 are parallel failing-test slices before T019–T021 implementation.
- Within US3, T024 and T025 are parallel; T026 follows their metric/validity contract.
- T032 and T033 can run in parallel after T031.

## Parallel Examples

### User Story 1

```text
Task T006: normalization/canonicalization/mutation-invariance tests
Task T007: strict candidate/trace and forbidden-field tests
Task T008: immutable store/evidence integration tests
```

### User Story 2

```text
Task T015: verifier response/prompt contract tests
Task T016: crash journal/resume tests
Task T017: concurrency/seal/accounting tests
```

### User Story 3

```text
Task T024: seal-first hidden-loader tests
Task T025: frozen metric and instability tests
```

## Implementation Strategy

### MVP first — US1 only

1. Complete T001–T005.
2. Complete T006–T014 test-first.
3. Stop and validate that 1540/771 deterministic packets exist and no model/network call occurred.

### Incremental delivery

1. US1 freezes clean packets and receipts.
2. US2 adds a fully offline stub runner and deterministic seal before any hosted use.
3. US3 proves seal-first scoring and all gates using fixtures.
4. Repository verification completes before the paid Stage-0 run.
5. A NO-GO ends 034; a GO authorizes a separate formal paired-rejudge feature only.

## Notes

- Do not mark a test-first task complete unless its expected pre-implementation failure was observed.
- Never include raw API keys, provider error bodies, source paths, or chat-exposed credentials in tracked artifacts.
- A hosted answer verifier is benchmark/SaaS opt-in; it is not a local engine score or a reranker/recall lever.
- Do not modify or overwrite 031/033 worktree files.
