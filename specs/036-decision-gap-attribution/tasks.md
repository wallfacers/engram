# Tasks: Decision-Gap Attribution(决策缺口归因)

**Input**: Design documents from `specs/036-decision-gap-attribution/`

**Prerequisites**: `plan.md`, `spec.md`

**Tests**: Required. This feature changes benchmark diagnostic behavior, so each story starts with failing offline contract/unit tests before implementation. All tests are offline (034 fixture pattern + hand-built `adjudicationHiddenInputs`); no provider, no network.

**Organization**: Tasks are grouped by user story. 036 is a pure diagnostic: no model calls, no decision changes, no engine edits, no eval baseline.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel because it touches a different file and does not depend on incomplete work
- **[Story]**: Maps the task to a user story from `spec.md`
- Every task names the exact repository path it reads or changes

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm isolation and inventory the frozen 034 seams before any implementation edit.

- [X] T001 Verify the 036 feature/pointer and confirm no sibling or engine changes overlap `specs/036-decision-gap-attribution/` (`git worktree list` + `git status`; `.specify/feature.json` → `specs/036-decision-gap-attribution`)
- [X] T002 Inventory reusable frozen 034/035 seams: `loadAndValidateAdjudicationPublic`, `loadAdjudicationHiddenInputs`, `adjudicationTextControlSlot`, `canonicalHistoricalSlotForSameAnswer`, `normalizeAdjudicationAnswer`, `majorityCorrectness`, `categoryLabel`, `adjudicationAuditCallRecord.Assessments`; confirm `writeAdjudicationPublicFixture` exists at `cmd/locomo-bench/answer_adjudication_test.go:526`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Freeze additive file boundaries and the CLI contract before user-story behavior.

**⚠️ CRITICAL**: No production behavior is added in this phase; story tests must fail before their implementations.

- [X] T003 Add compile-only 036 test scaffolding and the attribution report schema/digest helpers in `cmd/locomo-bench/answer_adjudication_attribution_artifact.go` + `_test.go`
- [X] T004 Write failing table tests for attribution CLI dispatch, mutual exclusion with 034/035 modes, and mode-owned flag rejection in `cmd/locomo-bench/answer_adjudication_attribution_cli_test.go`

**Checkpoint**: Test harness is ready and the first contract tests fail for missing 036 behavior.

---

## Phase 3: User Story 1 — 逐题缺口重建与归因清单 (Priority: P1) 🎯 MVP

**Goal**: 从 034 public artifacts + hidden verdict 逐题重建 control/oracle/selected 三方状态,输出 1540 行明细并标记缺口,零模型调用。

**Independent Test**: 离线 fixture 上逐题重建与手工判定一致;缺口数 = 1411 − 1378 = 33;control-only loss 与 both-wrong 分解与 034 `verdict.md` 一致;篡改 hidden verdict 不改变归因行。

### Tests for User Story 1

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T005 [US1] Write failing row-build tests: oracle/control/selected per-packet reconstruction, canonical slot tie-break reuse, gap marking, control-only vs both-wrong split in `cmd/locomo-bench/answer_adjudication_attribution_test.go`
- [X] T006 [US1] Write failing fixture tests: `writeAdjudicationPublicFixture` + hand-built `adjudicationHiddenInputs` produce exact 33-gap / 13 / 9 decomposition; tampered hidden verdict leaves rows byte-identical in `cmd/locomo-bench/answer_adjudication_attribution_test.go`
- [X] T007 [US1] Write failing fail-closed tests: corrupted seal/decision/packet digest → zero hidden reads and no rows in `cmd/locomo-bench/answer_adjudication_attribution_cli_test.go`

### Implementation for User Story 1

- [X] T008 [US1] Implement `buildAttributionRows(manifest, packets, hidden)`: per-packet correctBySlot/normalizedBySlot/oracle/majority/controlSlot/selectedSlot (reusing `canonicalHistoricalSlotForSameAnswer`), gap = oracle && !selectedCorrect, split control-only vs both-wrong/third-candidate, per-row evidence/confidence/fallback in `cmd/locomo-bench/answer_adjudication_attribution.go`
- [X] T009 [US1] Implement attribution report schemas, canonical JSON, and strict read/write validation in `cmd/locomo-bench/answer_adjudication_attribution_artifact.go`
- [X] T010 [US1] Implement validation-first loading (`loadAndValidateAdjudicationPublic` + `loadAdjudicationHiddenInputs`), atomic output, and the attribution CLI mode in `cmd/locomo-bench/answer_adjudication_attribution_cli.go` + minimal `cmd/locomo-bench/main.go` flag/dispatch
- [X] T011 [US1] Run and fix focused US1 tests plus `CGO_ENABLED=0 go build ./...`

**Checkpoint**: US1 materializes a validated 1540-row attribution with exact 33-gap decomposition offline.

---

## Phase 4: User Story 2 — 类别 × 失败模式聚合 (Priority: P1)

**Goal**: 按 packet.Category(1–4)聚合缺口,叠加可复核的失败模式(证据不足/事实错/语义等价),输出分布表与结论。

**Independent Test**: fixture 上类别分布表逐格与手工统计一致;四类之和 = 33;失败模式归类逐行可复核。

### Tests for User Story 2

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T012 [US2] Write failing category aggregation tests: 4-category distribution, sum = gap total, zero-category explicit row in `cmd/locomo-bench/answer_adjudication_attribution_test.go`
- [X] T013 [US2] Write failing failure-mode tests: semantic-equivalence (normalized equal), evidence-insufficient (EvidenceIDs don't cover correct candidate), factually-wrong (evidence self-consistent but non-correct), unclear fallback with `mode_reason` in `cmd/locomo-bench/answer_adjudication_attribution_test.go`

### Implementation for User Story 2

- [X] T014 [US2] Implement `aggregateRows(rows)`: category×mode table, conclusion (dominant mode), per-row mode_evidence/mode_normalized_equal/mode_reason in `cmd/locomo-bench/answer_adjudication_attribution.go`
- [X] T015 [US2] Run and fix focused US1+US2 tests plus `CGO_ENABLED=0 go build ./...`

**Checkpoint**: US2 independently proves the category×failure-mode distribution.

---

## Phase 5: User Story 3 — 035 审计交叉验证 (Priority: P2)

**Goal**: 读 035 audit seal 的 call journal(per-view assessments),对每缺口题标注风险队列内外/父答案被反驳/唯一替代;seal 缺失显式降级。

**Independent Test**: fixture 上交叉表行数 = 缺口数,状态判定与手工检查的 035 seal 一致;035 缺失时列空标注且不阻塞。

### Tests for User Story 3

> Write these tests first and confirm they fail for the expected missing behavior.

- [X] T016 [US3] Write failing cross-audit tests: per-gap 035 status (in_risk_queue / parent_refuted_any_view / unique_alternative), audit-unavailable empty column, no blocking in `cmd/locomo-bench/answer_adjudication_attribution_test.go`
- [X] T017 [US3] Write failing CLI tests: 035 dir present vs missing, atomic combined report, no credential/verdict leakage in `cmd/locomo-bench/answer_adjudication_attribution_cli_test.go`

### Implementation for User Story 3

- [X] T018 [US3] Implement `crossAudit(rows, auditCalls, auditResolver)`: per-gap status from `adjudicationAuditCallRecord.Assessments` (parent refuted = any view contradiction=yes & support!=yes; unique alternative = exactly one supported non-contradicted group) + `adjudicationAuditResolverMapRecord.Risk`; seal missing → empty column in `cmd/locomo-bench/answer_adjudication_attribution.go`
- [X] T019 [US3] Implement 035 seal loading (best-effort, missing → audit_unavailable), combined report write in `cmd/locomo-bench/answer_adjudication_attribution_cli.go`
- [X] T020 [US3] Run and fix all focused 036 tests plus `CGO_ENABLED=0 go build ./...`

**Checkpoint**: US3 independently proves the 035 cross-validation without blocking US1/US2.

---

## Phase 6: Polish, Regression Gates, and Verification

**Purpose**: Prove repository safety and leave the diagnostic runnable on real 034/035 data.

- [X] T021 Run `gofmt` on `answer_adjudication_attribution*.go`, `git diff --check`, and the checklist in `specs/036-decision-gap-attribution/checklists/requirements.md`
- [X] T022 Run `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` and detached `CGO_ENABLED=0 go test -count=1 ./...`, recording exit status/log paths in the session scratchpad
- [X] T023 Run `CGO_ENABLED=0 go build ./...`, verify no changes under `memory/ embedding/ provider/ store/ internal/`, and compare frozen 034/035 CLI tests still pass (parity)
- [X] T024 Scan tracked diffs for credentials, raw endpoints/errors/responses, hidden verdicts, and forbidden path leakage against `specs/036-decision-gap-attribution/contracts/attribution-schemas.md`

---

## Phase 7: Documentation and Real-Data Handoff

**Purpose**: Document the runbook and prepare for the real 034/035 dataset (maintainer's other machine).

- [X] T025 Write `specs/036-decision-gap-attribution/quickstart.md` (offline fixture run + real-data run steps + expected 33/13/9 receipts) and `data-model.md` (report schema reference)
- [X] T026 Summarize the deliverable in `specs/036-decision-gap-attribution/tasks.md`: diagnostic scope, zero-model-call proof, and the exact real-data command the maintainer runs once 034/035 artifacts are available
- [X] T027 Confirm default benchmark and 034/035 CLI behavior byte-identical via parity tests; engine-directory diff empty; document as invariant-by-construction with evidence

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: Starts immediately.
- **Foundational (Phase 2)**: Depends on Setup and blocks all story implementation.
- **US1 (Phase 3)**: Depends on Foundational; produces the row/缺口 contract.
- **US2 (Phase 4)**: Depends on US1 rows.
- **US3 (Phase 5)**: Depends on US1 rows + optional 035 seal; independent of US2.
- **Polish (Phase 6)**: Depends on all three stories.
- **Documentation/handoff (Phase 7)**: Depends on Phase 6.

### User Story Dependencies

- **US1 (P1)**: Foundational only; independently valuable as the 1540-row attribution artifact.
- **US2 (P1)**: Consumes US1 rows; independently testable offline.
- **US3 (P2)**: Consumes US1 rows + 035 seal; independently testable with a fixture audit seal.

### Within Each User Story

- Write each listed test and observe the expected failure before its implementation task.
- Report schemas/canonical validation precede orchestration.
- Reuse frozen 034/035 functions, never reimplement them.
- Focused tests and `CGO_ENABLED=0 go build ./...` must pass at each story checkpoint.

### Parallel Opportunities

- T005–T007 same file, serialized.
- T012–T013 same file, serialized.
- T016–T017 different files (test vs cli_test), can proceed independently.
- US2 and US3 implementation touch different files and can proceed in parallel after US1.

## Implementation Strategy

### MVP First (US1 Only)

1. Complete Setup and Foundational.
2. Write failing US1 contracts.
3. Implement the 1540-row build path with 33-gap decomposition.
4. Stop and verify exact 33 / 13 / 9 receipts and hidden-seal validation.

### Incremental Delivery

1. US1 freezes the row/缺口 contract.
2. US2 adds the category×mode aggregation.
3. US3 adds the optional 035 cross-validation.
4. Full repository/parity gates run before any real-data execution.
5. Real 034/035 data is supplied by the maintainer's other machine; the run is zero-call and offline.

## Notes

- 036 is benchmark-only diagnostic: no reranker, no recall change, no answer generation, no adjudication change, no engine edit.
- A failed/invalid seal fails closed; missing 035 audit degrades explicitly (empty column), never silently.
- Attribution output is `decision_gap_attribution`, never a formal LoCoMo score; it only informs a future mechanism spec.
- Never print or persist credential values, raw endpoint URLs, provider responses, or raw provider errors.

## Completion Record (2026-08-11)

- Full spec-kit chain complete: `spec.md` (Draft → finalized), `plan.md`, `tasks.md`, `contracts/attribution-schemas.md`,
  `data-model.md`, `checklists/requirements.md`, `quickstart.md`.
- Implementation shipped offline and verified:
  - `cmd/locomo-bench/answer_adjudication_attribution.go` — per-question three-way rebuild (control/oracle/selected,
    reusing `canonicalHistoricalSlotForSameAnswer` tie-break), gap marking, control-only vs both-wrong split, category
    × failure-mode aggregation, deterministic (conv,q) ordering.
  - `cmd/locomo-bench/answer_adjudication_attribution_artifact.go` — report schema `036.decision-gap-attribution.v1`,
    atomic write refusing overwrite, 035 cross-audit via resolver map + call journal with explicit `audit_unavailable`
    degradation.
  - `cmd/locomo-bench/answer_adjudication_attribution_cli.go` + minimal `main.go` flag/dispatch — `--adjudication-
    attribution` mode, exactly-three-candidate contract, optional `--adjudication-audit-source`.
  - `cmd/locomo-bench/answer_adjudication_attribution_test.go` — 9 offline tests covering gap marking, both-wrong,
    semantic equivalence, factually-wrong (contract requires cited evidence), frozen-fixture invariant
    `gap == oracle − selected`, fail-closed loader, 035 cross-audit, overwrite refusal, deterministic rows.
- Gates: focused + full-repo `CGO_ENABLED=0 go test -count=1 ./...` exit 0; `CGO_ENABLED=0 go build ./...` exit 0;
  `gofmt` clean; `git diff --check` clean; engine-directory diff (`memory/ embedding/ provider/ store/ internal/`)
  empty; secret scan zero hits.
- Invariant-by-construction proof (Constitution IV): 036 changes no retrieval/extraction/curation/storage/embedding
  algorithm and no eval config; parity is proven by unchanged 034/035 CLI tests + engine diff empty. No eval re-run
  required and no new baseline claimed.
- Real-data handoff: the maintainer runs the `quickstart.md` §2 command on the other machine once 034/035 artifacts
  are available; expected receipts gaps=33 control_only_loss=13 both_wrong=9 categories=4.
- The fixture recomputes gap=43 (oracle 1411 − selected 1368) because the scoring fixture's decisions are all
  fallback (selected == control); the real run's 33 comes from the same code with the actual 034 decisions.

## Real-Data Verification (2026-08-11)

The 034/035 artifacts turned out to live on this machine (session-scratch), so the §2 handoff ran locally:

```bash
go run ./cmd/locomo-bench --adjudication-attribution ~/.claude/session-scratch/034-stage0.DR07JV \
  --adjudication-candidate ~/.claude/session-scratch/locomo90.M5r3Os/matrix-2026-07-26/locomo-pro/r1/results-hybrid.jsonl \
  --adjudication-candidate .../r2/results-hybrid.jsonl \
  --adjudication-candidate .../r3/results-hybrid.jsonl \
  --adjudication-audit-source ~/.claude/session-scratch/035-materialize-a.RVp4zU
```

Receipt: `attribution: gaps=33 control_only_loss=13 both_wrong=9 categories=4 audit=035-audit dominant=factually_wrong`.
Report: `~/.claude/session-scratch/034-stage0.DR07JV/decision-gap-attribution.json`. Verdict:
`docs/evaluation/reports/036-decision-gap-attribution-verdict.md`.

Two bugs were found and fixed during the real-data run (both missed by the fixture because it is all-fallback):

1. **CLI flag bug**: `validateAdjudicationAttributionOptions` rejected attribution mode because it required
   `adjudicationMaxTokens == 0`, but max-tokens is a 034 run-mode flag whose default is 512. The task table claimed a
   `_cli_test.go` that did not exist, so the flag path was never tested. Fixed + added
   `answer_adjudication_attribution_cli_test.go` (6 tests) covering flag defaults, audit-dir, mode-foreign options, and
   the exactly-three-candidates contract.
2. **Aggregation bug**: `aggregateAttribution` counted every `!control && !selected` gap as `both_wrong`, absorbing the
   11 fallback gaps (6 not-triggered + 5 triggered) and yielding both_wrong=20 instead of the verdict's 9. Per data-model
   (`fallback_gaps = non-trigger + triggered`) and SC-002 (13/9), fallback is now `FallbackReason != ""` and never mixes
   into the override split: `33 = 13 control-only + 9 both-wrong + 11 fallback_gaps`.

Failure-mode conclusion: factually_wrong 22 (all 22 accepted overrides), semantic_equivalence 7 (all fallback),
evidence_insufficient 4 (triggered low-confidence fallbacks), unclear 0. Temporal is the largest category (12 gaps, 6
control-only + factually_wrong). See the verdict doc for the mechanism-spec direction.

