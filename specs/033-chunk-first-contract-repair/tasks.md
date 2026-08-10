# Tasks: Multi-hop Chunk-First Contract Repair

**Input**: Design documents from `specs/033-chunk-first-contract-repair/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: TDD is mandatory. Red tests are recorded before implementation; every source edit is followed by
`CGO_ENABLED=0 go build ./...` and touched-package tests.

## Phase 1: Setup and Frozen Evidence

**Purpose**: Preserve the pre-change state and all no-leak evaluation inputs.

- [X] T001 Record HEAD/worktrees, dirty-state, dataset/store receipt, cohort counts and SHA-256 values in `specs/033-chunk-first-contract-repair/diagnosis/preflight.md`
- [X] T002 Run the unchanged `cmd/locomo-bench` assembly tests and record exact baseline output plus the known missing multi-hop contract coverage in `specs/033-chunk-first-contract-repair/diagnosis/preflight.md`

---

## Phase 2: Foundational Experiment Boundaries

**Purpose**: Make runtime and evaluation boundaries executable before behavior changes.

- [X] T003 Validate `target-32.txt`, `guard-32.txt`, and `probe-64.txt` counts, uniqueness, concatenation, and hashes in `specs/033-chunk-first-contract-repair/diagnosis/cohort-manifest.md`
- [X] T004 [P] Add a no-gold offline order/closure analyzer for legacy vs kind-layered assembly JSONL in `specs/033-chunk-first-contract-repair/tools/offline_order_analyze.py`
- [X] T005 Add deterministic fixture tests for the offline analyzer in `specs/033-chunk-first-contract-repair/tools/test_offline_order_analyze.py`

**Checkpoint**: Cohorts and mechanism-only metrics are frozen before implementation or model calls.

---

## Phase 3: User Story 1 — 原文证据始终优先 (Priority: P1) 🎯 MVP

**Goal**: Multi-hop assembly has one canonical chunk→fact sequence, preserves entity grouping, and renders the
same evidence-line order that `EvidenceAssembly.Units` records.

**Independent Test**: Mixed-kind, multi-entity, tied-score, ungrouped, single-kind, empty and exact-cap fixtures
all pass offline; tests inspect both Units and rendered prompt without any model caller.

### Tests for User Story 1 — write and observe RED first

- [X] T006 [P] [US1] Add failing canonical order, stable tie-break, candidate multiset, cross-kind entity, degenerate-input and private-label poison non-interference tests in `cmd/locomo-bench/entity_grouping_test.go`
- [X] T007 [P] [US1] Add failing Units↔prompt evidence-line order and canonical-prefix cap tests in `cmd/locomo-bench/assembly_flow_test.go`
- [X] T008 [US1] Run only the new tests before implementation and record the expected failures in `specs/033-chunk-first-contract-repair/diagnosis/tdd-red.md`

### Implementation for User Story 1

- [X] T009 [US1] Implement kind-layered, coverage-first, deterministic canonical ordering in `cmd/locomo-bench/entity_grouping.go`
- [X] T010 [US1] Replace multi-hop renderer re-partitioning with a streaming renderer over the canonical sequence in `cmd/locomo-bench/entity_grouping.go`
- [X] T011 [US1] Route multi-hop assembly and every cap iteration through the canonical order/renderer pair in `cmd/locomo-bench/assembly.go`
- [X] T012 [US1] After each T009–T011 edit run `CGO_ENABLED=0 go build ./...` and then run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Test(MultiHop|Assemble)'`

**Checkpoint**: The repaired assembly is independently usable under the existing `--evidence-assembly` gate.

---

## Phase 4: User Story 2 — 修复不改变检索与其他类别 (Priority: P1)

**Goal**: Add an auditable legacy-order control and prove the repair is isolated to enabled multi-hop assembly.

**Independent Test**: Legacy and treatment share the same input closure, different modes cannot resume into one
run dir, non-multi categories and assembly-off remain byte-identical, and both modes work offline.

### Tests for User Story 2 — write and observe RED first

- [X] T013 [P] [US2] Add failing flag validation, default-off legacy control and answer-regime fingerprint tests in `cmd/locomo-bench/bench_test.go`
- [X] T014 [P] [US2] Add failing legacy-order parity, `entity_order` audit and cat 2/3/4 byte-parity tests in `cmd/locomo-bench/assembly_flow_test.go`

### Implementation for User Story 2

- [X] T015 [US2] Add `--assembly-legacy-entity-order`, validation and regime fingerprinting in `cmd/locomo-bench/main.go`
- [X] T016 [US2] Add the auditable `entity_order` field and mode routing to `assemblyConfig`/`EvidenceAssembly` in `cmd/locomo-bench/assembly.go` and `cmd/locomo-bench/assembly_diagnose.go`
- [X] T017 [US2] Preserve the exact pre-repair group-major sorter/renderer as the benchmark-only legacy path in `cmd/locomo-bench/entity_grouping.go`
- [X] T018 [US2] After each T015–T017 edit run `CGO_ENABLED=0 go build ./...`; then run all `cmd/locomo-bench` tests and `go test -race` for the new contracts
- [X] T019 [US2] Run both retrieval-only 64-question diagnostics and write candidate-closure, chunk-before-fact, rank-band, prompt-order and zero-call results to `specs/033-chunk-first-contract-repair/diagnosis/offline-verdict.md`

**Checkpoint**: Mechanism correctness, isolation and rollback are proven without answer/judge calls.

---

## Phase 5: User Story 3 — 用预注册配对门验证突破 (Priority: P2)

**Goal**: Apply the frozen three-arm pilot, stop on a failed gate, and only then attempt a comparable full-set
strict >90 result.

**Independent Test**: A reproducible script and analyzer regenerate target/guard majority flips, isolated legacy
vs treatment flips, absolute/category metrics, call counts, costs and protocol receipts from fresh run dirs.

### Tests and tooling for User Story 3

- [X] T020 [P] [US3] Add fixture-tested target/guard majority, paired-flip, exact McNemar, Holm category gate, 19-question per-repeat context/cap/truncation/gold-admission audit, and post-result `chunk-gold && rank>=19` stratifier in `specs/033-chunk-first-contract-repair/tools/probe_analyze.py` and `specs/033-chunk-first-contract-repair/tools/test_probe_analyze.py`; reject missing/misaligned audits and never pass gold-derived cohorts/maps to execution
- [X] T021 [P] [US3] Add a benchmark-only runtime assembly-audit switch plus tests, then add a credential-free, scratch-dir-parameterized, WSL-detachable driver that runs A/C on 64 questions and B on the 18 multi-hop questions in `specs/033-chunk-first-contract-repair/run033.sh`; C/B audits must come from the same real answer passes, not a retrieval-only surrogate
- [X] T022 [US3] Preflight the driver with `--estimate`, verify 438 planned primary answer/judge decisions plus separately accounted retry provider calls, freeze `LOCOMO_NO_THINKING=0`, legacy IDK retry, model/store/data/cohort receipts, trace digest, 19/16 derived cohort digests, exact-vs-estimated counter status, and record them in `specs/033-chunk-first-contract-repair/diagnosis/probe-receipt.md`

### Paid evaluation gates — require explicit budget authorization

- [X] T023 [US3] After explicit authorization, run A/C on the 64-question probe and B on its 18 multi-hop questions with `setsid`, interleave arms in the same time window, poll without foreground sleep loops, and preserve all logs/results under the session scratchpad
- [X] T024 [US3] Analyze the probe, report every chunk-gold question's A/C provider context plus C cap/admission/truncation, and report C−A flips for `chunk-gold && rank>=19` versus remainder in `specs/033-chunk-first-contract-repair/diagnosis/verdict.md`; if C−A target net <8 or guard loss >1, record NO-GO and stop all full-set work
- [X] T025 [US3] NOT APPLICABLE: T024 is NO-GO, so the pre-registered stop rule forbids full-set A/C calls
- [X] T026 [US3] NOT APPLICABLE: no full-set result exists to register after the T024 NO-GO

**Checkpoint**: Promotion exists only if treatment ≥1387/1540 and every receipt/non-regression gate is satisfied.

---

## Phase 6: Polish & Cross-Cutting Verification

**Purpose**: Prove repository-wide safety and prepare independent review.

- [X] T027 Run `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test -count=1 ./...`, targeted race tests, and `go vet ./...`; paste any failure faithfully into `specs/033-chunk-first-contract-repair/diagnosis/verification.md`
- [X] T028 Verify `git diff --name-only -- memory embedding provider store internal` is empty, `git diff --check` is clean, no secret is present, and all temporary artifacts remain outside the repository; record in `specs/033-chunk-first-contract-repair/diagnosis/verification.md`
- [X] T029 [P] Re-run every command in `specs/033-chunk-first-contract-repair/quickstart.md` that does not require ungranted spend and reconcile documentation drift in `specs/033-chunk-first-contract-repair/quickstart.md`
- [X] T030 Complete independent code/spec/evaluation review in `specs/033-chunk-first-contract-repair/diagnosis/review.md`; terminal resolution is NO-GO, not merge-ready, do not merge, with every finding preserved as a mandatory prerequisite for any successor experiment

---

## Dependencies & Execution Order

### Phase Dependencies

- Phase 1 → Phase 2: pre-change evidence must be frozen first.
- Phase 2 → US1: cohorts and no-gold analyzer must be fixed before behavior changes.
- US1 → US2: legacy control is defined against the completed canonical path.
- US2 → US3: paid probes require the same-binary control, audit mode and fingerprint gates.
- US3 probe GO → full-set: T025/T026 are forbidden if T024 is NO-GO.
- Phase 6 follows all implementation work; paid-dependent checks remain pending until authorized.

### User Story Dependencies

- **US1**: starts after Phase 2; delivers the pure offline repair.
- **US2**: depends on US1's canonical sequence but independently proves non-interference and rollback.
- **US3**: depends on US1+US2 and is the only phase allowed to claim benchmark uplift.

### Parallel Opportunities

- T004 defines the analyzer contract; T005 follows it with deterministic fixtures.
- T006/T007 can be authored in parallel before the RED run.
- T013/T014 can be authored in parallel before control-mode implementation.
- T020/T021 can proceed in parallel after receipt fields are frozen.
- T029 can run alongside independent review preparation after implementation tests are green.

## Parallel Example: User Story 1

```text
Task T006: entity grouping order/tie/multiset/degenerate tests in entity_grouping_test.go
Task T007: Units↔prompt and exact-cap prefix tests in assembly_flow_test.go
Then T008: run both test sets and preserve RED evidence before implementation.
```

## Implementation Strategy

### MVP First

1. Freeze setup and no-gold diagnostics (T001–T005).
2. Complete US1 with observable RED→GREEN (T006–T012).
3. Stop and independently verify canonical order/prompt/cap before adding evaluation control.

### Incremental Delivery

1. US1: contract-correct offline assembly.
2. US2: same-binary legacy control, receipts and zero-call 64-question verdict.
3. US3: paid pilot under budget gate; full-set only after GO.
4. Polish: full repository and constitutional audit.

## Notes

- No task may use gold/judge fields in runtime ordering or treatment selection.
- No paid cloud reranker/recall is permitted under any phase.
- A failed paid or offline gate is a valid result and must be recorded; it is not permission to add another lever.
- Every task uses exact repository paths and the required checklist format.
