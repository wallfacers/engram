# Tasks: engram skill 隐式记忆触发 + 三工具数据飞轮（048）

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [dataset protocol](contracts/dataset-protocol.md), [runner CLI contract](contracts/runner-cli.md), [score contract](contracts/scoring-report.md), and [quickstart.md](quickstart.md)

**Generated**: 2026-09-01; regenerated after cross-artifact remediation and execution-order audit.

**Tests**: Required. The specification, constitution, and contracts require deterministic unit/contract coverage before each behavior change. Write each listed test first, verify that it fails for the missing behavior, then implement.

**Scope guard**: This list supersedes the stale 2026-08-29 checked-off task list. No task inherits a completed state. Do not modify Go files below memory/, embedding/, provider/, store/, or internal/, and do not modify cmd/locomo-bench/.

**Privacy guard**: Real holdout plaintext, raw holdout receipts, and protected roots never enter this checkout. Repository fixtures are fictional only; exported holdout reports contain only plaintext-free aggregate/seal data.

## Phase 1: Setup

**Purpose**: Record the protected-execution contract and fictional test-fixture location before implementation; the exact-child execution receipt is created later by formal `series prepare`.

- [X] T001 Record active-feature/worktree evidence, the operator-provided protected-execution mechanism, planned per-worker evaluated-child identities/capacity, disposable per-case core/holdout state allocators with controller-only retirement policy, disjoint author-review versus formal state roots, the protected access-probe matrix/controller-target-proof contract, and the engine/LoCoMo zero-scope baseline in specs/048-implicit-memory-flywheel/receipts/setup-scope.md
- [X] T002 Create fictional v2 schema, receipt, and invalid-input fixtures with a no-real-holdout notice in cmd/skill-eval/testdata/v2/README.md

---

## Phase 2: Foundational contracts and safety

**Purpose**: Build the shared v2 dataset, sealing, provenance, frozen DevFamilyIndex, containment, protected-execution, and seed-rendering foundation that blocks every story.

**Checkpoint**: All contract tests below must pass, and the CLI-reviewed DevFamilyIndex must be frozen, before modifying the skill or producing a holdout candidate.

- [X] T003 [P] Add v2-loader tests for core172 exact module/language policy, a 32-case `case-id → expected behavior/should_trigger` golden, manifest-authoritative IDs, append-only extension separation, legacy evals.json exclusion, and explicit pre-index versus family-aware validation behavior in cmd/skill-eval/dataset_test.go
- [X] T004 [P] Add LF-normalized digest, case-only dataset-payload digest, completed-manifest-after-payload digest, freeze-before-digest, self-reference regression, seal-tamper, and immutable-manifest tests in cmd/skill-eval/manifest_test.go
- [X] T005 [P] Add exact Claude settings, Codex `-c model_provider=aq -c model=qwen3.8-flash --yolo`, OpenCode free-model argv/template assertions plus ToolProvenance allowlist/redaction, stable-field-only `tool_identity_digest` (excluding `captured_at`/artifact IDs), every-attempt author/reviewer host-model stability/distinctness, unsafe-path, and secret-like input rejection tests in cmd/skill-eval/provenance_test.go
- [X] T006 [P] Add reproducible dev-family-index-v2 exact, mirror-review (wording-variant join, topic-divergent/empty-slug refusal), disagreement, connected-component, bounded `max_in_flight ≤ concurrency`, and `concurrency > 1` overlap tests in cmd/skill-eval/family_test.go and freeze the reviewed prompt contract in skills/engram/evals/prompts/dev-family-index-review-v1.md
- [X] T007 [P] Add SeedMemory event_date receipt-honesty plus minimum dev-only diagnostic-mode, unique-root, explicit `--concurrency`, bounded `max_in_flight ≤ concurrency`, actual overlap when `concurrency > 1`, and non-score-eligibility tests in cmd/skill-eval/runner_test.go
- [X] T008 [P] Add failing contract tests for the sole `skill-eval package validate` producer: full sorted recursive inventory, source/staging/materialized snapshot byte equality, immutable anchor, 020 validator argv/result binding, no source-dir/symlink substitute, and primary rejection after any snapshot-byte/file-list/package-digest drift in cmd/skill-eval/package_validation_test.go. Add fixed-suite `GreenTestReceipt` tests in cmd/skill-eval/green_test_test.go for fixed argv-only suites, sanitized output evidence, `holdout-pipeline`/`formal-tooling`/`series-prepare`/`pre-holdout` scope rules, missing/failed/wrong-suite/post-hoc/drift rejection, and no real holdout fixture use. Add pre-T018 CLI dispatch/usage/argv tests for `package validate` and `green-test create`, including required arguments, the fixed suite allowlist and rejection of arbitrary commands, in cmd/skill-eval/main_test.go.
- [X] T009 Implement immutable `FrozenSkillPackageSnapshot`, the sole `package validate` receipt producer, fixed-suite `green-test create`/verification, safe snapshot/anchor rehash helpers, and the complete `package validate` / `green-test create` command routing and usage in cmd/skill-eval/package_validation.go, cmd/skill-eval/green_test.go, cmd/skill-eval/manifest.go, and cmd/skill-eval/main.go. The implementation must be callable before T018, must not mutate the source package, may leave no passing artifact after failure, and must expose only sanitized test/validator output digests.
- [X] T010 Implement TriggerCaseV2, split/membership-aware loading, core-manifest case selection, and v1 compatibility in cmd/skill-eval/dataset.go
- [X] T011 Implement canonical blind/case-only/manifest digest helpers, dataset/run/series seals, immutable manifest helpers, and containment primitives in cmd/skill-eval/manifest.go
- [X] T012 Implement sanitized provenance capture, secret filtering, path validation, and resolved-model checks in cmd/skill-eval/provenance.go
- [X] T013 Implement the contract-frozen DevFamilyIndex derivation plus a bounded three-lane mirror-review worker pool honoring frozen concurrency in cmd/skill-eval/holdout.go
- [X] T014 Implement safe workspace materialization, `[event_date=YYYY-MM-DD]` seed-content rendering, and the minimum dev-only diagnostic runner with unique roots plus a bounded worker pool honoring explicit concurrency and formal-score eligibility fixed false in cmd/skill-eval/runner.go
- [X] T015 Add split-aware `pre-index`/`family-aware` validate, named-manifest loading, family-index build with frozen `--concurrency` and observed max-in-flight provenance, and run --mode diagnostic routing with explicit concurrency in cmd/skill-eval/main.go
- [X] T016 Freeze the core172 manifest plus initial empty extension payload/manifest in skills/engram/evals/dev-regression-core.manifest.json, skills/engram/evals/dev-extension.json, and skills/engram/evals/dev-extension.manifest.json; then execute and seal the real core172 `pre-index` validation receipt, failing before family-index build on any mismatch
- [X] T017 Generate and freeze the CLI-reviewed core family metadata with the WSL2 detached `setsid` pattern, session-scratchpad log, exit-file polling, prompt/provenance/decision receipts, and final digest in skills/engram/evals/dev-family-index.json and specs/048-implicit-memory-flywheel/receipts/family-index-freeze.md; then execute/seal family-aware core172 validation against that exact index

---

## Phase 3: User Story 1 — Implicit write (Priority: P1) MVP

**Goal**: A durable fact disclosed naturally is written exactly once and acknowledged in the same turn; it is written directly without requesting confirmation, while transient, refusal, secret, generic-discussion, and third-party-misattribution cases remain non-writes.

**Independent Test**: Package-contract validation and deterministic write-side judge fixtures prove write count, store content, update semantics, no-confirmation behavior, correct attribution, and same-turn acknowledgement without any formal score.

- [X] T018 [US1] Create a current passing `formal-tooling` GreenTestReceipt, use the sole `skill-eval package validate` producer to freeze/anchor the immutable pre-revision skill snapshot and exact package-validation receipt, then use that snapshot with the foundational dev-only runner under the WSL2 detached `setsid` pattern to produce an exploratory complete diagnostic baseline in specs/048-implicit-memory-flywheel/receipts/exploratory-baseline.md; mark it ineligible for SC-5 and official scoring
- [X] T019 [P] [US1] Add package-contract assertions for direct write without confirmation, implicit durable writes, update semantics, transient/refusal/secret exclusions, generic-discussion non-writes, third-party attribution, and same-turn acknowledgement in scripts/validate-agent-skill.test.mjs
- [X] T020 [P] [US1] Add deterministic write-side judge fixtures for exactly-one-write, no-confirmation, acknowledgement, supersession, multi-fact, refusal, secret, generic-discussion, third-party attribution, and false-negative/false-positive/wrong-op/wrong-report mappings in cmd/skill-eval/judge_write_test.go
- [X] T021 [US1] After T018 freezes the pre-revision snapshot, implement the spec-directed implicit-write activation and durable-fact boundaries as the draft candidate in skills/engram/SKILL.md without claiming SC-5 improvement
- [X] T022 [US1] Synchronize the draft candidate's write intent/version metadata in skills/engram/references/contract.json and run the existing 020 validator diagnostically; do not emit or claim a formal package-validation receipt for the mutable draft

**Checkpoint**: The draft candidate package and write-side judge agree on every write/non-write fixture; SC-5 attribution waits for the final-runner baseline and T055 refinement.

---

## Phase 4: User Story 2 — Implicit read (Priority: P1)

**Goal**: A request that depends on durable user/project facts searches memory before answering or acting, uses the retrieved evidence, and leaves unrelated work free of memory calls.

**Independent Test**: Deterministic read-side fixtures prove search ordering, evidence-grounded output, honest empty results, supersession safety, and no-search boundaries without a formal score.

- [X] T023 [P] [US2] Extend package-contract assertions for implicit read, evidence-grounded answers/actions, empty-result honesty, and memory-independent no-search boundaries in scripts/validate-agent-skill.test.mjs after T019 is complete
- [X] T024 [P] [US2] Add deterministic read-side judge fixtures for search-before-answer, evidence-grounded output, not-found, stale/superseded facts, enumeration, read-negative cases, and closed failure-class mapping in cmd/skill-eval/judge_read_test.go after T020 is complete
- [X] T025 [US2] Extend the draft candidate with implicit-read activation, query discipline, and no-search boundaries in skills/engram/SKILL.md without claiming SC-5 improvement
- [X] T026 [US2] Synchronize the draft candidate's search-intent wording and version metadata in skills/engram/references/contract.json and rerun the existing 020 validator diagnostically; the mutable draft remains ineligible for a formal receipt or score

**Checkpoint**: Read-side package checks and judge fixtures pass while normal technical questions remain free of memory calls.

---

## Phase 5: User Story 3 — Versioned tricky dataset and sealed holdout (Priority: P1)

**Goal**: Preserve core172, create an append-only dev extension, and generate a 96-case external holdout through non-human three-lane authoring and anonymous dual review.

**Independent Test**: Holdout validation proves the exact 96 matrix, two anonymous agreeing non-author reviews, no family collision, model provenance, protected-root placement, frozen author/review state-root provenance, and seal integrity. Exact-child formal isolation remains a separate `series prepare` gate in US4.

- [X] T027 [P] [US3] Add exact 96-case module/author/language quota, eight scenario-bucket `12 / 4-per-author / 6-6-language / 10-implicit+2-trap` coverage, and core172 matrix tests in cmd/skill-eval/holdout_quota_test.go; add bounded author/review `max_in_flight ≤ N`, `N > 1` overlap, per-attempt stage-isolation capacity, own-input readability, controller target-existence proof, and private/audit/receipt/prior-review/active-sibling denial tests in cmd/skill-eval/holdout_concurrency_test.go
- [X] T028 [P] [US3] Add label-blind review-envelope serialization tests that reject author, author-specific quota slot, batch/source, ordinal, provenance, private candidate digest, prior-review, all author-proposed `expect/module/lang/scenario/category/machine-rule` fields, nested aliases/extensions, duplicate JSON keys and unknown recursive fields; require canonical `BlindCandidateV1` digest equality for identical blind subsets with different private labels/rules/slots, and require digest-matched materialized source-bound family-summary payloads in cmd/skill-eval/holdout_review_test.go
- [X] T029 [P] [US3] Add dataset-seal tests for protected-root-only plaintext, stable pairwise-distinct author/reviewer resolved models across every attempt, OpenCode free billing, prompt consistency, append-only `AttemptStarted`/`AttemptTerminal` ledger start-before-launch/one-terminal/count/reason integrity (including launch failure), complete launched-attempt AuthorReviewIsolationReceipt/controller-proof aggregation, frozen author/review state-root digests, explicit `payload_files` union/file-digest/canonical-json/anchor-preimage verification, and explicit exclusion of future FormalSeriesManifest, HoldoutBindingReceipt, and ProtectedExecutionReceipt fields from the dataset seal in cmd/skill-eval/holdout_seal_test.go
- [X] T030 [P] [US3] Add novelty tests for materialized anonymous dev/accepted-holdout family-summary payloads, source-state/count/root one-to-one re-projection and source-family deletion detection, dev/accepted-holdout family collisions, controller-only family identity, translations, reviewer disagreement, complete inferred category/expect label mismatch, stale accepted-family CAS under concurrent admission, AdmissionReceipt commit/stale chain replay, fresh-session re-review, same-slot regeneration, and invalid nearest-family references in cmd/skill-eval/holdout_novelty_test.go
- [X] T031 [P] [US3] Add frozen holdout authoring and review prompts in skills/engram/evals/prompts/holdout-authoring-v1.md and skills/engram/evals/prompts/holdout-review-v1.md
- [X] T032 [US3] Implement exact author × module × language × scenario quota scheduling, recursively closed strict-JSON private candidate parsing, required CLI identities, bounded authoring workers, append-before-launch `AttemptStarted` and exactly-one `AttemptTerminal` events, per-attempt ephemeral state/input workspaces, controller target proofs, and AuthorReviewIsolationReceipt probes in cmd/skill-eval/holdout.go
- [X] T033 [US3] Implement anonymous label-blind two-non-author review with canonical closed `BlindCandidateV1` digest, materialized label-free source-bound family-summary payloads, a bounded isolated reviewer worker pool, own-envelope-only visibility, complete independent inferred label/expect output, controller-side private candidate/four-dimensional slot comparison, controller-generated family identity, append-only `AdmissionReceipt` CAS/re-review chain, isolation/model-identity failure rejection, and same-slot regeneration in cmd/skill-eval/holdout.go
- [X] T034 [US3] Implement private manifest assembly, stable host-stable non-`unavailable` author/reviewer model seal checks across the complete all-attempt event ledger, scenario/source coverage and non-gating bias/funnel receipts, all launched AuthorReviewIsolationReceipt/controller-proof aggregation, frozen author/review state-root provenance, `payload_files`/canonical manifest/verified DatasetAnchorV1 handling, admission-chain/final-state replay, dataset-seal versus formal-series boundary validation in cmd/skill-eval/holdout.go
- [X] T035 [US3] Add holdout generate, holdout review, holdout seal, and holdout validation command routing in cmd/skill-eval/main.go
- [X] T036 [US3] Only after T009, T017, and T032–T035 pass their focused fictional-fixture tests, create a current passing `holdout-pipeline` GreenTestReceipt, then generate, dual-review, validate, and seal holdout96 only under the operator-provided protected root and stage-execution boundaries using the WSL2 detached `setsid` pattern; freeze AuthorReviewIsolationReceipt/state-root/event/admission-chain digests for later disjointness checks and record only plaintext-free dataset-seal receipts in specs/048-implicit-memory-flywheel/receipts/holdout-seal.md
- [X] T037 [US3] Before the final candidate snapshot, document core/extension/holdout membership, core172 module and `zh=72/en=68/regression-unclassified=32` language policy, legacy evals.json exclusion, residual authoring-bias limits, fixed scenario-bucket/source-slice design, label-blind/CAS review, per-stage/per-worker/per-case isolation, the no-plaintext rule, separate official-score language, and the explicit non-model-unseen claim boundary in skills/engram/evals/DATASET_CARD.md

**Checkpoint**: No real holdout plaintext is tracked, and a malformed, duplicated, author-revealing, model-ambiguous, provenance-incomplete, or unsealed candidate cannot pass dataset validation. This checkpoint does not authorize a formal run without the US4 execution receipt.

---

## Phase 6: User Story 4 — Formal three-host runner, receipts, and scoring (Priority: P1)

**Goal**: Run three complete ordinals per host/split with immutable receipts, deterministic rejudging, bounded concurrency, host parity, protected execution, and two separately gated official score families.

**Independent Test**: Synthetic sealed 3 × host × split × ordinal fixtures produce deterministic separate scores; every selector, retry, missing receipt, provenance drift, bad binding, concurrency violation, or protected-execution violation fails closed.

- [X] T038 [P] [US4] Add command-surface tests for `core-plan create`, `official-dual` versus `dev-comparison` purpose, required/forbidden datasets, required shared core-plan import, missing/failed/wrong-skill package-validation receipt rejection, missing/failed/wrong-suite/digest-drifted `series-prepare` and holdout-ordinal-1 `pre-holdout` receipt/argv rejection, primary/diagnostic split and explicit-concurrency rules, selector rejection, `failure-archive`/`compare` no-official-score and holdout-input rejection, trace-unobservable INVALID handling, and no silent downgrade in cmd/skill-eval/main_test.go
- [X] T039 [P] [US4] Add primary-runner tests for unique roots, no overwrite, exact coverage, one agent attempt, unavailable handling, diagnostic isolation, `max_in_flight ≤ concurrency`, actual overlap when concurrency exceeds one, rejection when isolated-worker capacity is insufficient, disposable per-case state roots, prior-case/retired-workspace denial, core/holdout allocator separation, and active sibling-workspace unreadability in cmd/skill-eval/runner_primary_test.go
- [X] T040 [P] [US4] Add series tests for `CoreExecutionPlanReceipt` stable-field digest semantics, shared baseline/candidate plan import, exact-skill package-validation receipt binding/rejection, `official-dual` versus core-only `dev-comparison` purpose, frozen timeout/concurrency/case-order/worker-boundary/disposable-state execution template, all three ordinals, digest drift, exact question counts, mandatory null ProtectedExecutionReceipt for `dev-comparison`, fresh candidate-specific ProtectedExecutionReceipt creation by `official-dual series prepare` with matching core-plan digest, automatic host × worker-slot workspace-canary receipt creation/mismatch rejection, core-invalid-before-holdout recovery, and binding-after-INVALID recovery that requires the same stable `CandidateBindingV1` digest (not a reused manifest, runtime or `pre-holdout` receipt), a new series/manifest/fresh roots, complete core172, then a fresh exact-series `pre-holdout` attestation bound to its new manifest + stable digest + complete core-leg receipt-set digest before complete holdout96 × three-host × three-ordinal execution; reject every reused old run/split/ordinal/`pre-holdout` receipt, assert all pre-seal prerequisite receipts (package validation, matching `series-prepare` GreenTestReceipt, protected execution, workspace canaries) precede the series seal, and verify the fresh exact-series `pre-holdout` attestation separately after complete core-leg completion and before holdout ordinal 1 in cmd/skill-eval/manifest_test.go
- [X] T041 [P] [US4] Add receipt/rejudge tests for redacted raw events, normalized events, store dumps, workspace digests, per-case state-isolation/reset receipts that bind a valid prepared worker slot/probe and reject unknown slot or identity/template/boundary drift, closed error codes, and protected holdout artifacts in cmd/skill-eval/receipt_test.go
- [X] T042 [P] [US4] Add holdout-binding tests for no binding/consumption when the official core leg invalidates before holdout ordinal 1; fresh matching `pre-holdout` receipt plus atomic binding-only at holdout ordinal 1; bound-but-unconsumed state when holdout becomes INVALID before all three ordinals complete; append-only recovery-ledger evidence; recovery only with the same stable `CandidateBindingV1` digest (which excludes old manifest/`pre-holdout`/runtime receipt identities), a new series that reruns the full core172 matrix, then creates a fresh `pre-holdout` attestation bound to its new `series_manifest_digest` + same `candidate_binding_digest` + new `core_leg_completion_digest`, atomically associates that exact attempt and only then runs the full holdout96 matrix; changed-stable-digest and every cross-series run/split/GreenTest receipt-reuse rejection; consumption only after one complete recovery/original holdout series; and FailureArchive/compare holdout-input rejection in cmd/skill-eval/binding_test.go
- [X] T043 [P] [US4] Add exact Claude/Codex/OpenCode argv/profile tests, automatic host × worker-slot staged-file canaries after final skill/template freeze, Codex cmd.Dir plus `codex exec -C` parity, and exact-child probe-matrix tests proving controller-confirmed own-workspace readability plus protected-root traversal/list/read, audit/state-root, prior-case-state, retired-workspace, and concurrent sibling-workspace denial; any canary or formal child slot/identity/template/boundary mismatch must invalidate in cmd/skill-eval/preflight_test.go
- [X] T044 [P] [US4] Add score tests for exact integer 90%/10% implicit boundaries, 8/8 and 0/8 trap gates, dev-only 020 versus holdout-only trap routing, three-ordinal medians, `dev-comparison` score rejection, per-host gates, no merged headline, conservative negative runner-error treatment, and table-driven eligibility failures for missing host/split/ordinal/case receipt, bad package/protected/workspace-canary/worker-slot receipt, missing/failed/wrong-suite/post-hoc/digest-drifted `series-prepare` or `pre-holdout` GreenTestReceipt, recovery-series GreenTest mismatch, any other digest mismatch, any invalid-series scoring receipt, or any cross-series splice; require a recovered report to reference only one complete recovery series while retaining prior invalid IDs solely as non-scoring binding-ledger evidence, plus OfficialScoreReport security and both GreenTest receipt digests and non-gating diagnostic cells with numerator/denominator/independent-case-count/low-N in cmd/skill-eval/report_test.go
- [X] T045 [US4] Implement sealed CoreExecutionPlanReceipt creation/import validation, exact-skill package-validation and matching `series-prepare` GreenTestReceipt binding, purpose-aware FormalSeriesManifest/PrimaryRunManifest, ProtectedExecutionReceipt plus automatic host × worker-slot WorkspaceCanaryReceipt maps with split-disjoint state allocators, HoldoutBindingReceipt and per-series-attempt `pre-holdout` attestation binding, freeze-before-digest sealing, core-invalid-before-holdout recovery rules, and timeout/concurrency mismatch rejection in cmd/skill-eval/manifest.go
- [X] T046 [US4] Implement deterministic rejudging, all four closed judge failure classes, unknown-class fail-closed behavior, and terminal runner-error classification in cmd/skill-eval/judge.go
- [X] T047 [US4] Implement primary execution with bounded workers honoring the sealed concurrency, harden the foundational diagnostic mode with the same bounded worker implementation, add unique series/run roots, full-case ordering, disposable per-case state allocation/teardown, prior/retired-state probes, and retry boundaries in cmd/skill-eval/runner.go
- [X] T048 [US4] Persist secret-filtered raw/normalized/store/workspace/attempt/state-isolation receipts, bind each primary case to its prepared host × worker slot/probe with exact child identity/template/boundary matching, and enforce primary receipt/state-root completeness in cmd/skill-eval/runner.go
- [X] T049 [US4] Implement required Claude/Codex/OpenCode invocation parity, event-date seed receipts, automatic series-prepare host × worker-slot workspace canary execution, disjoint core/holdout formal state allocators, per-worker isolation, controller target proofs, and the full root/audit/state/own-workspace/prior-case/retired-workspace/active-sibling protected access-probe matrix through the exact evaluated-child execution boundary in cmd/skill-eval/runner.go
- [X] T050 [US4] Implement two official score families, exact gate routing, per-host median gates, scorer eligibility fail-closed matrix, OfficialScoreReport series/core-plan/package/protected/canary plus exact `series-prepare`/`pre-holdout` GreenTest digest bindings, recovery-series attestation validation, supplemental-only aggregates, non-gating scenario/author-lane/self-author/funnel cells with independent-case low-N semantics, and invalid/fail verdicts in cmd/skill-eval/report.go
- [X] T051 [US4] Wire `core-plan create`, purpose-aware series prepare with exact package/`series-prepare` receipt and automatic canaries, primary holdout ordinal-1 with fresh exact-series `pre-holdout` receipt, final diagnostic concurrency/selector/holdout-rejection rules, and official-dual-only score commands after their implementations in cmd/skill-eval/main.go; reject missing/wrong-suite/drifted/recovery-series-mismatched GreenTest CLI inputs before any run or score action

**Checkpoint**: A primary run cannot overwrite, partially score, retry a case, consume diagnostic material, serialize a configured-parallel batch, let a failed negative case improve a gate, or use an isolation-unproven holdout environment.

---

## Phase 7: User Story 5 — Dev-only flywheel and official execution (Priority: P2)

**Goal**: Demonstrate a complete dev flywheel while preventing all holdout content and outcomes from guiding a skill revision.

**Independent Test**: The complete baseline → archive → revision → append-only backfill → full rerun → comparison chain exists, at least one frozen-baseline failure becomes a pass, every regression is counted, and no holdout receipt can enter revision input.

- [X] T052 [US5] Add append-only extension, sealed dev-only `failure-archive`/`compare` command output, closed failure-class/root-cause validation, holdout-plaintext rejection, and before/after tests that require a three-ordinal core-only `dev-comparison` baseline plus the exact same CoreExecutionPlanReceipt (runner/judge/core/host `tool_identity_digest`/timeout/concurrency/case-order/disposable-state template), the skill-package digest as the intentional variable, binary per-case median comparison, at least one frozen-baseline failure becoming a pass, every regression counted, and a one-to-one fail-to-pass → new extension ID backfill with valid source/supersession relation and manifest membership; extension results remain separate in cmd/skill-eval/flywheel_test.go
- [X] T053 [US5] Implement dev-only FailureArchive construction, core-only before/after comparison receipt sealing, holdout-path/official-score rejection, and the callable `failure-archive`/`compare` command handlers in cmd/skill-eval/report.go and cmd/skill-eval/main.go
- [ ] T054 [US5] After T051 and T053, independently rehash and verify T018's immutable pre-revision snapshot, anchor, and exact `skill-eval package validate` receipt; do not create a post-hoc replacement receipt. Create/seal one CoreExecutionPlanReceipt, create a fresh passing `series-prepare` GreenTestReceipt bound to that snapshot/receipt and the final runner/judge/tooling, prepare a core-only `dev-comparison` series, and run all three ordinals for every host with the WSL2 detached `setsid` pattern. Invoke `failure-archive`, freeze comparable binary per-case medians, and write the classified dev archive in specs/048-implicit-memory-flywheel/failbook.md plus specs/048-implicit-memory-flywheel/receipts/flywheel-baseline.md **（2026-09-04：4/9 腿完成——claude o1 + codex o1/o2/o3 全量零 error，binary per-case 中位数与分类归档已落 failbook T054 节 + receipts/flywheel-baseline.md；opencode 被维护者裁定停跑，3-host 矩阵不完整使 `failure-archive`/`compare` 封盘构造不出，机器 seal 版归 host-matrix 决策（2-host 重规划 vs pin 补跑）后收口）**
- [X] T055 [US5] After T054, finalize one failure-driven revision on top of the spec-directed draft candidate using only the comparable dev FailureArchive; synchronize SKILL.md description/body, references/contract.json and version metadata, create a fresh passing `formal-tooling` GreenTestReceipt, then use the sole `skill-eval package validate` producer to atomically materialize/anchor the final immutable snapshot and seal its exact `SkillPackageValidationReceipt`. Direct validator output or mutable `skills/engram` is never the formal package receipt/evaluated package **（2026-09-04 v0.2.8→v0.2.9：靶子取自 T054 全量 50 失败点解剖（24 retry-after-error[27 pinned 字符串化+7 trigger 超长+1 冗余字段] / 20 upsert-refine / 6 multi-fact 分写）——SKILL.md §0 write-once 重写 + mcp.md strict-types 契约 + contract.json implicit_write 同步；green `t055-formal-tooling-green-test.json` + 快照 `snap-b7112404425b39f609b442e7`（~/.engram-eval/skill-snapshot-t055/）+ PV `t055-package-validation.json`，skill_digest `bdda82f9…bfa04`）**
- [ ] T056 [US5] Create a fresh passing `series-prepare` GreenTestReceipt bound to T055's immutable snapshot/receipt, then prepare one `official-dual` series that imports T054's exact CoreExecutionPlanReceipt; this candidate-specific `series prepare` must rehash the snapshot/anchor, create a fresh exact-child ProtectedExecutionReceipt plus automatic host × worker-slot workspace-canary receipts from the frozen candidate invocation/boundary, verify normalized core identity/boundary/template matches the imported plan, and reject any package/green-test/canary/protected mismatch. Run **only** its core172 primary leg. If core invalidates before holdout ordinal 1, retain it and loop here with a new series ID, the same candidate binding and same CoreExecutionPlanReceipt until a complete non-INVALID core leg exists; never create/modify a HoldoutBindingReceipt in this recovery. Invoke `compare` over sealed core paths, require the shared-plan runner/judge/core/host `tool_identity_digest`/timeout/concurrency/case-order/worker-boundary/disposable-state template to match with only the skill-package digest intentionally changed, compare binary per-case medians, require at least one comparable-baseline fail-to-pass improvement or record SC-5 FAIL, emit the exact required backfill set, and write the core comparison portion of specs/048-implicit-memory-flywheel/receipts/flywheel-post.md
- [ ] T057 [US5] Append every T056 eligible fail-to-pass exactly once as a new ID in the mutable skills/engram/evals/dev-extension.json, record valid source/supersession lineage, update skills/engram/evals/dev-extension.manifest.json, and mechanically verify exact one-to-one membership with the sealed comparison set. Treat these post-snapshot package-source edits as an explicitly unevaluated dev-data revision: they may not alter, replace, or inherit the score of T055's snapshot. Then run the complete post-change core-plus-extension diagnostic regression with explicit bounded concurrency and the WSL2 detached `setsid` pattern in independent CLI/HOME/XDG/cache/session roots while reserving all formal holdout roots untouched; do not reprepare or mutate the evaluated snapshot/series. Revalidate the 32-case 020 semantic golden, independently rehash T055's unchanged snapshot/anchor/receipt rather than comparing its receipt to mutable source, write separate non-score extension/diagnostic receipts, and complete specs/048-implicit-memory-flywheel/receipts/flywheel-post.md
- [ ] T058 [US5] Begin the protected holdout leg only after T057 completes and the prepared `official-dual` core leg remains complete/non-INVALID. Create a fresh passing `pre-holdout` GreenTestReceipt that rehashes the exact prepared series/snapshot/runner/judge/validator/tool binding; at holdout ordinal 1, verify it and atomically append only the binding receipt. Run all three holdout ordinals for every host in fresh never-reused roots isolated from author/review and core state. If the series becomes INVALID after binding, keep the version bound and unconsumed, append recovery-ledger evidence, reject any changed stable `CandidateBindingV1` digest, and prepare a new series with the same stable digest (which expressly excludes the old manifest, `pre-holdout` and runtime receipts). That new series reruns **from zero** complete core172 for all three hosts and ordinals in fresh roots, creates a fresh recovery-series `pre-holdout` receipt bound to its exact new `series_manifest_digest` + the same `candidate_binding_digest` + its new `core_leg_completion_digest`, atomically associates those three values as a new ledger attempt, and only then reruns complete holdout96 for all three hosts and ordinals in fresh roots. No old successful run/split/ordinal/GreenTest receipt may be reused, and the final report may reference only the complete recovery series. After one complete sealed series, mark holdout96 consumed immediately whether PASS or FAIL, then run the official dual scorer and write only plaintext-free, strictly separate dev/regression and generalization summaries with all report security digests and low-N bias cells, labeled as untuned/session-isolated synthetic holdout evidence rather than model-unseen evidence in specs/048-implicit-memory-flywheel/receipts/holdout-formal.md

**Checkpoint**: Formal core172 and holdout scores remain separate; the holdout version is never retuned, rebinding is blocked, diagnostics do not alter either score, and a no-improvement flywheel cannot be reported as SC-5 PASS.

---

## Phase 8: User Story 6 — Installation canonical path and smoke tests (Priority: P2)

**Goal**: Install exactly one shared engram skill per tool, use a Claude symlink only where required, and document the current discovery behavior honestly.

**Independent Test**: Every tool discovers one approved shared/symlink path; all three execute an implicit-write smoke, at least one completes it, and every outcome is recorded without credentials.

- [ ] T059 [P] [US6] Add standard-directory, symlink, no-private-duplicate, three-tools-all-executed, and at-least-one-implicit-smoke-pass assertions in scripts/test-agent-skill-install.mjs
- [X] T060 [US6] Probe current Claude/Codex/OpenCode discovery behavior and record sanitized version/path evidence in specs/048-implicit-memory-flywheel/receipts/installation-discovery.md
- [X] T061 [P] [US6] After T060, update canonical standard-directory, Claude symlink, and no-private-copy instructions in mutable skills/engram/references/install.md. If it completes before T055, `package validate` must include those exact bytes in the final snapshot; if it completes afterward, record it as an explicitly post-snapshot unevaluated documentation revision that cannot replace or inherit the official snapshot's scores
- [X] T062 [P] [US6] After T060, synchronize installation guidance in README.md and README.zh-CN.md
- [ ] T063 [US6] After T058, T061, and T062, run one-skill discovery and one implicit-write smoke on all three tools, require at least one complete smoke pass, and record every result without credentials in specs/048-implicit-memory-flywheel/receipts/installation-smoke.md. Bind the observed package digest and explicitly set `scoring_equivalent=false` whenever mutable source differs from T055's evaluated snapshot

**Checkpoint**: No instruction recommends a copied private Codex/OpenCode skill; any unsupported discovery behavior is reported rather than guessed.

---

## Phase 9: Polish and cross-cutting verification

**Purpose**: Publish the protocol truth, verify all gates, and document why no engine/LoCoMo regression run applies.

- [ ] T064 [P] Verify that T037's pre-snapshot skills/engram/evals/DATASET_CARD.md bytes already cover split identity, core language/count policy, legacy evals.json exclusion, fixed scenario/source bias diagnostics, per-worker/stage/per-case privacy boundary, separate-score language, and the non-model-unseen claim limit. Do not edit the evaluated snapshot; any required correction is a new unevaluated package revision and invalidates equivalence unless a new snapshot and applicable formal evaluation are completed
- [ ] T065 [P] Update primary/diagnostic protocol, WSL detach, protected artifacts, and no-retry guidance in docs/guides/skill-eval.md
- [ ] T066 Run focused Go and package/install test suites covering cmd/skill-eval, scripts/validate-agent-skill.test.mjs, and scripts/test-agent-skill-install.mjs
- [ ] T067 Run CGO_ENABLED=0 go build ./..., CGO_ENABLED=0 go test -count=1 ./..., and go vet ./... from the active 048 repository root (`./`)
- [ ] T068 Verify zero engine-path changes, no LoCoMo-path change, no paid reranker/recall dependency, and no secret/plaintext-holdout leakage; record results in specs/048-implicit-memory-flywheel/receipts/delivery-verification.md
- [ ] T069 Recheck active worktrees, git diff --check, and quickstart command/document consistency in specs/048-implicit-memory-flywheel/quickstart.md
- [ ] T070 Aggregate already sealed sanitized setup, family-index, exploratory/comparable baseline, package-validation, holdout seal/formal, flywheel, installation, and delivery receipts into the final provenance, dual-score, consumed-holdout, and no-LoCoMo verdict in specs/048-implicit-memory-flywheel/validation-report.md; do not recalculate or first emit either official score here. Report every post-snapshot `skills/engram/**` divergence as unevaluated and bind official scores only to T055's immutable snapshot digest

---

## Dependencies and execution order

### Phase dependencies

- Setup → Foundational contracts and safety.
- Foundational contracts and safety, including the frozen DevFamilyIndex → US1, US3, US4 test work, and US6.
- US1 → US2 because both synchronize the same skill package; T023/T024 may run in parallel only after T019/T020 are complete.
- US3 → US4 formal integration because official holdout runs require a sealed holdout manifest.
- US1 + US2 + US3 + US4 → US5.
- US6 test work and post-probe documentation can run beside US3/US4 after the foundation. If T061 is not completed before T055, it is explicitly post-snapshot and unevaluated; T063 waits for the completed protected official series plus synchronized installation docs so it cannot perturb CLI discovery/HOME/cache state between formal core and holdout legs.
- US5 + US6 → Polish.
- Runtime tasks write separate immutable receipts; only T070 aggregates them into `validation-report.md`.

### User-story dependency graph

    Setup
      └── Foundation (incl. frozen DevFamilyIndex)
          ├── US1 ──> US2
          ├── US3 ──> US4
          └── US6
    US1 + US2 + US3 + US4 ──> US5
    US5 + US6 ──> Polish

### Parallel opportunities

- Foundation: T003 through T008 are test-first parallel opportunities; T009 follows T008 and may proceed beside the implementations of T003–T007 once their own failing tests exist.
- US1: T019 and T020 can run in parallel.
- US2: T023 and T024 can run in parallel only after US1's shared-file tests are complete.
- US3: T027 through T031 can run in parallel; T032 through T037 are ordered.
- US4: T038 through T044 can run in parallel; T045 through T051 are ordered.
- US5: T052 precedes T053; T054 waits for both T051 and T053; T055 through T058 are ordered.
- US6: T059 can run after the foundation; after T060, T061 and T062 can run in parallel. T063 waits for T058, T061 and T062.
- Polish: T064 and T065 can run in parallel.

---

## Implementation strategy

### MVP first

1. Complete Setup and Foundational contracts/safety, including the frozen core family index.
2. Complete US1 and validate package/judge behavior independently.
3. This MVP proves the write-side product behavior, but it does not authorize a formal benchmark or generalization claim.

### Incremental delivery

1. Add US2 for symmetric implicit reading.
2. Add US3 and US4 before any official-score claim.
3. Complete US5 to demonstrate one full dev-only flywheel, formal core172 scoring, and one protected holdout evaluation.
4. Complete US6 and Polish for installation/documentation and repository-wide verification.

### Mandatory formal-score boundary

Only T058 may first generate a `generalization score`, and only after T017, T036, T045–T057 plus every gate inside T058 are complete. T070 may only aggregate the already sealed T058 result. The 172-case dev/regression score remains official but does not replace the sealed holdout score.
