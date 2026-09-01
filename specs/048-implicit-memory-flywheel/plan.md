# Implementation Plan: engram skill 隐式记忆触发 + 三工具数据飞轮（048）

**Branch**: `048-implicit-memory-flywheel` | **Date**: 2026-09-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/048-implicit-memory-flywheel/spec.md`

## Summary

Keep the existing 172-case trigger set as an immutable official `dev/regression core` score, place future flywheel backfills in an append-only non-headline dev extension, and add an independently generated, dual-reviewed, sealed 96-case holdout as the official `generalization score`. Both official manifests are run independently three times for each of Claude Code, Codex and OpenCode2; each host's per-metric median is official. No extension, cross-split or cross-host average can silently change or mask the two formal scores.

The implementation extends the adapter-side `cmd/skill-eval` harness and the engram skill/dataset documentation only. It adds a no-human CLI authoring/review/sealing workflow, first-class formal-run artifacts, reproducible provenance, deterministic rejudging, per-case cwd parity for Codex, and a strict diagnostic-versus-primary boundary. The engine remains untouched.

## Technical Context

**Language/Version**: Go 1.25, Markdown, versioned JSON; `CGO_ENABLED=0`.

**Primary Dependencies**: Go standard library (`os/exec`, `encoding/json`, `crypto/sha256`, worker-pool primitives); existing public `engram` / `engram-mcp` binaries; Claude Code, Codex CLI and OpenCode2 only as evaluated/authoring CLI processes.

**Storage**: Existing 172 JSON cases in `skills/engram/evals/`; private sealed holdout directory outside the tuning checkout; JSONL/JSON artifact receipts under a unique run root; per-case local SQLite stores only through public CLI/MCP surfaces.

**Testing**: `CGO_ENABLED=0 go test -count=1 ./cmd/skill-eval`; deterministic unit/contract fixtures; build/test/vet repository gate; real CLI smoke/preflight and formal run protocol outside unit tests.

**Target Platform**: Local Linux/WSL2 developer environment. Long CLI evaluations detach with `setsid` and write into a session scratchpad/run root.

**Project Type**: Adapter CLI + skill package + dataset protocol/documentation.

**Performance Goals**: All new model-calling author/review/eval paths use a bounded worker pool that honors `--concurrency`; tests must prove `max_in_flight ≤ concurrency` and prove actual overlap when `concurrency > 1`. Holdout permits `concurrency > 1` only when the protected-execution configuration provides at least that many independently isolated workers and proves each worker cannot read an active sibling workspace; otherwise preparation fails instead of sharing a visible workspace tree. Support the 2,412 official primary case executions (`3 hosts × 3 ordinals × (172 + 96)`) plus the 1,548-case pre-revision core comparison series (`3 × 3 × 172`) without serialized case dispatch when sufficient workers are configured; the comparison series never changes either official denominator.

**Constraints**:

- The frozen core172 and holdout96 are separately official; no extension or merged headline score changes their denominators.
- Holdout: 96 exactly; each authoring CLI contributes 32 accepted cases, 16 zh + 16 en; eight closed scenario buckets each contain 12 with 4 per author, zh/en 6/6 and frozen module coverage; no human authors/reviewers/tie-breakers.
- Each candidate is label-blind reviewed by exactly the other two hosts; reviewers independently derive module/language/scenario/expect, agreement plus private-slot comparison and accepted-family CAS are required.
- Reviewer-visible candidate digests cover only the canonical de-labeled projection, and novelty reviewers receive the actual anonymous label-free family-summary payloads bound by digest/revision; a digest-only summary is invalid.
- A holdout seal requires three host-stable, non-`unavailable` resolved models across all author/reviewer attempts; the three host harnesses must be distinct, while the underlying model may be shared by explicit maintainer decision (2026-09-01 unification on qwen3.8-flash). A host name never substitutes for model evidence.
- The holdout seal covers an append-only all-attempt ledger and every launched author/reviewer isolation receipt, including rejected/stale/failed attempts; case payload digest and completed-manifest digest are separate and non-self-referential.
- Every host × split runs three complete independent primary ordinals; median only; targeted retries are diagnostic-only.
- `skill-eval package validate` is the sole formal package-validation producer: it copies the complete package into an anchored immutable `FrozenSkillPackageSnapshot`, binds the existing 020 validator to that exact sorted file list/digest, and primary children use the snapshot rather than mutable `skills/engram/**`.
- A fixed-suite `GreenTestReceipt` gates each irreversible action: holdout author/review/seal, package validation, series preparation and holdout ordinal 1 all reject a missing/failed/digest-drifted receipt. `series prepare` automatically runs staged-workspace canaries for every usable host × worker slot after final templates freeze, and every case receipt binds the prepared slot/probe actually used.
- Holdout content and every plaintext-bearing formal receipt are not available to skill tuning or evaluated CLI filesystem traversal. An operator-provided isolated execution boundary (separate user/container/mount namespace/ACL or equivalent) must prove, using controller-confirmed targets governed by the same effective access boundary, that the exact evaluated child cannot traverse/list/read the holdout root or audit/state roots, cannot read a simultaneously active sibling/prior-case/retired workspace, and can read only its own materialized workspace. Every primary case uses a disposable HOME/XDG/cache/session/container root; core and holdout allocator sets are disjoint and holdout roots remain unused before its leg. Seal integrity, an unproven `not-found`, a repository-external path, or a separately chmod'd sentinel alone is insufficient.
- Author/reviewer CLI children also run in per-attempt ephemeral boundaries: they may read only their own prompt/quota or label-blind review envelope and cannot traverse/list/read the private root, generation audit, author receipt, prior review or active sibling workspace. Every denied probe has controller existence/content/policy proof. The dataset seal binds this author/review stage-isolation evidence separately from the later formal execution receipt.
- No credentials, raw settings, endpoint URLs, arbitrary stderr or real user memory enters a tracked file or formal report.
- No changes below `memory/`, `embedding/`, `provider/`, `store/` or `internal/`; no paid hosted reranker/recall scoring lever.

**Scale/Scope**: 268 official cases across two splits; a single-user local evaluation harness. This plan does not add a production memory engine capability, modify LoCoMo/LongMemEval, or promise model-agnostic performance beyond the three named host configurations.

**Current local CLI preflight (2026-08-31)**: Claude Code `2.1.251`, Codex CLI `0.149.1`, OpenCode2 `v0.0.0-next-16927`. These are captured at execution time as provenance rather than treated as stable hardcoded values. Claude's `aly_qwen_w` settings and Codex `aq` provider (qwen3.8-flash; maintainer 2026-09-01 unified both lanes onto Aliyun Bailian qwen3.8-flash) identify a requested configuration, not a verified underlying model identity.

## Constitution Check — Pre-Design

| Constitution gate | Design response | Status |
|---|---|---|
| I. Local-first/offline by default | engram store/MCP/judge paths are local and do not require embedding/LLM service. Agent inference uses only maintainer-authorized existing endpoints; docs must not call the entire agent inference chain offline. | PASS |
| II. Engine/adapter separation | Changes stay in `cmd/skill-eval`, `skills/engram`, docs and specs. Per-case data dirs use public CLI/MCP; no engine API change. | PASS |
| III. Contract-first/namespace isolation | Dataset, sealing, runner and report contracts are frozen in this plan before tasks. Case isolation uses a new workspace/store per host/run/case. | PASS |
| IV. Evaluation regression gate | No retrieval/extraction/curation/storage/embedding implementation changes and no `cmd/locomo-bench` changes. Prove invariance with engine tests/parity and empty engine diff; no LoCoMo rerun is implied. | PASS |
| V. Graceful degradation/honest scale | Report `runner-unavailable`/`INVALID` separately; do not turn missing host results into favorable denominators or infer actual model identity. | PASS |
| No paid-cloud reranker/recall death rule | No reranker/recall model is introduced. Named CLI endpoints are existing authorized inference lanes, with OpenCode author/review/formal evaluation constrained to an explicitly confirmed free model. | PASS |

## Research Decisions

The implementation must follow [research.md](research.md):

1. Freeze the 172 existing cases as an immutable `dev-regression core`; append later flywheel cases as a separately reported extension and do not retrospectively call either unseen generalization evidence.
2. Build a 96-case holdout with four 20-case implicit modules, trap `8/4/4`, and eight fixed scenario buckets, using the frozen author × module × language × scenario constraints; retain 96 for this version and use non-gating source/scenario slices before considering a future version.
3. Use the three supplied CLI identities as authors; two non-author CLI lanes label-blind review every candidate from a canonical blind digest plus actual anonymous family-summary payloads, accepted-family admission uses CAS, and all author/reviewer model identities are host-stable/pairwise distinct across the complete attempt ledger; no human admission or arbitration.
4. Digest versioned author/review prompts, case-only payloads and completed manifests with separate non-self-referential algorithms; provenance records real CLI/model/config evidence without secrets. `ToolProvenance.source_revision` is strictly the `cmd/skill-eval` source subtree and/or runner binary revision: it excludes skill packages, datasets, docs/specs and artifacts, so SC-5 has no accidental second variable.
5. Keep holdout plaintext outside the tuning checkout, prove root/audit/state/sibling/prior-case/retired-workspace isolation with controller target proofs under the exact evaluated-child identities, use disposable case state plus disjoint core/holdout allocators, keep author/review and formal state roots disjoint, and consume it after its three primary ordinals. Describe the result as untuned/session-isolated synthetic generalization evidence, not as proof that an underlying model never processed the cases.
6. Add `primary` versus `diagnostic` runner modes; both honor explicit bounded concurrency, while primary rejects partial selectors/retries and creates immutable, non-overwriting receipts. Once a holdout binding exists, an INVALID `official-dual` recovery uses a new same-binding series and replays the full core172+holdout96 matrix; it never carries forward prior successful ordinals.
7. Require Codex `-C <caseDir>` plus process cwd parity and automatic final-template host × worker-slot workspace canaries during `series prepare` before scoring staged-file traps.
8. Apply the user-selected A policy: every host separately must meet every applicable threshold on both score families; supplemental aggregate cannot decide PASS.

## Project Structure

### Documentation and contracts

```text
specs/048-implicit-memory-flywheel/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── dataset-protocol.md
│   ├── runner-cli.md
│   └── scoring-report.md
├── receipts/                    # sanitized, plaintext-free execution receipts; T070 aggregates them
└── tasks.md                    # regenerated on 2026-09-01; execute only after analysis

docs/guides/skill-eval.md       # formal/diagnostic runbook and hygiene updates
skills/engram/evals/            # retained dev dataset, protocol files, generic fixtures
├── dev-regression-core.manifest.json  # frozen 172 case IDs/counts/digests
├── dev-extension.json                 # append-only non-headline successor payload
├── dev-extension.manifest.json        # append-only non-headline flywheel cases
├── dev-family-index.json              # machine-derived core family mapping
└── prompts/                            # versioned author/review/family-index prompts
skills/engram/evals/DATASET_CARD.md  # split/provenance/reporting publication truth
```

### Source code

```text
cmd/skill-eval/
├── main.go                      # validate/green-test/package-validate/family-index/holdout/core-plan/series/run/archive/compare/score routing
├── dataset.go                   # v1 compatibility + v2 split/provenance validation
├── holdout.go                   # CLI authoring, blind review, seal lifecycle
├── provenance.go                # allowlisted capture/redaction/digests
├── manifest.go                  # freeze-before-digest, snapshots and artifact seals
├── package_validation.go         # immutable skill snapshot + 020 validator receipt producer
├── green_test.go                 # fixed-suite pre-irreversible-action attestations
├── runner.go                    # case isolation, host invocation, primary/diagnostic modes
├── judge.go                     # deterministic judge; rejudgable receipts
├── report.go                    # FailureArchive/comparison receipts + medians/two score families
├── testdata/                    # fictional v2/seal/event fixtures only; no real holdout
└── *_test.go                    # unit/contract/preflight fixtures

skills/engram/
├── SKILL.md                     # behavioral contract only; holdout does not feed tuning
├── references/contract.json
└── evals/                       # retained 172 + dataset protocol assets, not plaintext holdout

scripts/
└── *.test.mjs                    # package/install contract tests; not a production implementation package
```

**Structure Decision**: `cmd/skill-eval` is the only new Go production implementation package; listed `scripts/*.test.mjs` files remain test-contract surfaces. It may call public `engram`/`engram-mcp` binaries but must not import or alter engine internals. Neither current public write surface accepts structured `event_date`; therefore a non-null v2 `SeedMemory.event_date` is rendered deterministically into seeded content as `[event_date=YYYY-MM-DD]` before the existing `engram add` call. The runner must verify that marker in its seed receipt and must not claim that `memory.Entry.EventDate` was populated. Private holdout content is intentionally absent from the tuning source tree; generic test fixtures prove parsing/sealing without leaking scored cases.

## Implementation Phases

### Phase 0 — Contract and safety foundations

1. Regenerate `tasks.md` from this plan and run cross-artifact analysis before implementation.
2. Add v2 schema types, split loader and two-stage core validation while preserving the legacy 172 loader/IDs/**per-case expected semantics**: run/receipt a pre-index validation without requiring legacy `family_id`, create the frozen `dev-family-index-review-v1` prompt, build/freeze the versioned deterministic-plus-CLI-reviewed `DevFamilyIndex`, then run/receipt family-aware validation before any holdout candidate exists. Its three-lane mirror review uses a bounded worker pool, freezes `--concurrency`, records observed max-in-flight, and proves overlap when concurrency is greater than one. Render every non-null v2 `SeedMemory.event_date` into the frozen content marker before existing CLI seeding and test that it is neither silently dropped nor misreported as engine metadata.
3. Add safe relative-path/case-ID containment checks, secrets filtering, closed scenario-bucket scheduling, label-blind review/CAS admission, and the schema/configuration contract for an operator-provided protected-execution boundary before any generated candidate may write files. Author/reviewer model identities must be host-stable and pairwise distinct across all three lanes. The actual formal exact-child probe matrix is a formal-series preparation artifact, not part of the dataset seal: it must bind the final child identities and access policy, deny protected-root traversal/list/read plus author/review state and concurrent sibling-workspace reads, and allow only the worker's own materialized workspace. Every denied probe needs controller target-existence/content/policy proof; stage isolation receipts remain a distinct, allowed dataset-seal input.
4. Add deterministic canonical blind digest, case-only dataset payload digest and completed-manifest digest primitives with self-reference and freeze-before-digest tests.
5. Add fixed-suite `GreenTestReceipt` production/verification before any irreversible flow: holdout-pipeline before author/review/seal, formal-tooling before package snapshot validation, series-prepare before sealing a series, and pre-holdout before binding ordinal 1. Tests must reject post-hoc, wrong-suite, failed and digest-drifted receipts; no real holdout payload may serve as a test fixture.
6. Preserve a minimum dev-only diagnostic path with explicit bounded concurrency, unique roots and non-score eligibility so an exploratory pre-revision run and immutable pre-revision skill snapshot can be captured before the first flywheel skill change. This early run may guide diagnosis but cannot support the SC-5 comparison.

### Phase 1 — Holdout creation and sealing

1. Add the versioned holdout author/review prompt assets and the frozen 96-case quota scheduler. Require a current passing `holdout-pipeline` GreenTestReceipt before the first real authoring child and reject any changed implementation digest thereafter.
2. Implement CLI authoring commands using the required Claude/Codex/OpenCode identities, strict JSON output, bounded concurrency and an observable max-in-flight test.
3. Implement anonymous **label-blind** dual-review orchestration with its own bounded worker pool and reject/regenerate lifecycle; reviewers receive only the canonical blind candidate plus actual digest-bound anonymous family summaries, independently infer module/language/scenario/expect, then controller compares their unanimous digest to the private author/slot after submission. Verify author ≠ reviewers, envelopes contain neither author-identifying slot/source data nor proposed labels/rules/private digests, accepted-family CAS prevents stale concurrent admission, the accepted-family index detects dev/holdout collisions, and exact author/reviewer children cannot read private audit/receipt/sibling material outside their own ephemeral input workspaces.
4. Build a private-only holdout manifest, append-before-launch all-attempt ledger, every-attempt host-model-stability and pairwise-distinctness check, immutable anchor, author/review state-root provenance and complete launched-attempt `AuthorReviewIsolationReceipt` aggregate; reject omitted rejected attempts, plaintext-in-repo, unsafe/exposed author-review artifacts, mixed prompt/model digests and malformed/consumed datasets. The dataset seal proves admission plus author/review-stage isolation and case-only payload integrity only; it neither contains nor substitutes for a future formal `ProtectedExecutionReceipt`, `HoldoutBindingReceipt`, formal worker-capacity proof, or future execution-context check.
5. Validate the exact matrix: 96, 20×4 implicit, trap 8/4/4, 48/48 language, 32 per author, and eight scenario buckets each with 12/4-per-author/6-6-language/10-implicit-plus-2-trap coverage. Emit non-gating source/scenario slices and rejection funnel with numerator, denominator, independent-case count and low-N markers.

### Phase 2 — Formal runner integrity

1. Add the formal primary mode, first-class series purpose/split/ordinal fields, reusable sealed `CoreExecutionPlanReceipt`, `FrozenSkillPackageSnapshot`, exact-snapshot `SkillPackageValidationReceipt`, fixed-suite `GreenTestReceipt`, bounded eval workers that demonstrably honor `--concurrency`, per-worker isolation when holdout concurrency exceeds one, and final diagnostic selector/holdout-rejection rules on top of the foundational dev-only diagnostic path. `package validate` must recursively copy and anchor the full evaluated package, run/bind the existing 020 validator and emit the sole receipt producer; `series prepare`/primary children must rehash and use only that snapshot. The plan freezes core172, runner/judge, ordinal seeds, timeout/concurrency, normalized core child template and each host's stable `tool_identity_digest`, while deliberately excluding skill/purpose/unique roots. `official-dual` binds both score splits and runs the exact-child protected access-probe matrix before sealing; core-only `dev-comparison` uses the same plan and three-host/three-ordinal primary integrity but rejects holdout and can never enter the official scorer.
2. Primary mode rejects `--only`, `--sample`, `--limit`, automatic agent retries and holdout diagnostics; diagnostic mode is dev-only and isolated.
3. Persist redacted raw events, normalized events, post-turn store dump, workspace digest, attempt receipt, `CaseStateIsolationReceipt` and closed error codes so every verdict can be rejudged; each case receipt binds its prepared host × worker slot/probe and rejects actual identity/template/boundary drift. Keep every holdout plaintext-bearing receipt under the protected root, run the exact-execution access-probe matrix before holdout primary work, verify active sibling isolation under configured concurrency, disposable per-case state roots, prior-case/retired-workspace denial and core/holdout allocator separation, and mechanically reject holdout input to the dev failure archive.
4. Capture sanitized provenance, define `source_revision` as runner-subtree/binary-only, validate/bind the exact frozen skill snapshot/package receipt and `series-prepare` GreenTestReceipt, and freeze skill/dataset/runner/judge/tool configuration before ordinal 1. Immediately before holdout ordinal 1, rerun the fixed `pre-holdout` suite and reject any changed binding digest.
5. Make Codex use both `cmd.Dir=caseDir` and `-C caseDir`; after final templates/slots freeze, have `series prepare` automatically run and bind file-visibility canaries for every usable host × worker slot.

### Phase 3 — Official scoring and flywheel

1. Implement the three independent ordinals per host/split and immutable run/series seals.
2. Implement score calculation: per-run numerator/denominator, per-module median, exact integer 90%/10% boundaries, dev-only 020 versus holdout-only trap routing, host-specific gates, two named score families and non-gating supplemental summary; count a terminal negative-case `runner-error` conservatively in the negative gate numerator while reporting it separately. The scorer fails closed on any missing matrix/receipt and the public report binds series/core-plan/package/protected/canary digests plus low-N bias-cell counts.
3. Run the dev flywheel only. After a passing `formal-tooling` GreenTestReceipt, freeze and package-validate the pre-revision skill snapshot before preparing the spec-directed draft candidate. After the final runner/judge/CLI surface is wired, create and seal one `CoreExecutionPlanReceipt`, then execute that frozen snapshot as a core-only `dev-comparison` series across all three hosts and ordinals using that plan. Generate its sealed dev-only `FailureArchive`; only then use the archive to finalize the draft candidate, synchronize all three package faces/version, rerun `formal-tooling`, materialize/validate/anchor the exact final snapshot and seal the receipt. Prepare one `official-dual` series that imports the same core plan, run only its core172 primary leg and build the sealed core-only before/after `compare` receipt; it reads no holdout artifact and emits no official score. If the core leg is invalid before holdout ordinal 1, retain it and prepare a new series ID with the same candidate snapshot binding and same core plan until a complete core leg exists; do not create/consume/rebind the holdout. The comparison emits the exact fail-to-pass set; append each member exactly once as a new extension ID with source/supersession lineage and manifest membership, then run the complete post-change core+extension **diagnostic** regression in explicitly independent roots without changing/repreparing the candidate or touching reserved holdout roots. Each host × core-case before/after state is the binary median across its three ordinals. The common plan makes runner, judge, core dataset, timeout, concurrency, case-order seeds, normalized evaluated-child execution/isolation template and per-host stable `tool_identity_digest` identical; `source_revision` is runner-only, and `captured_at`, purpose and unique artifact IDs are not identity inputs. The two anchored skill snapshot digests are the sole intentional variables, and extension results are reported separately. SC-5 passes only if at least one comparable-baseline median failure becomes a pass and every median regression is counted; otherwise record FAIL. Do not feed any holdout item or result into the revision.
4. Only after the candidate core leg, one-to-one backfill and isolated core+extension diagnostic are complete may that exact prepared series create a passing `pre-holdout` GreenTestReceipt and execute the 96-case holdout three times per host in fresh formal contexts that cannot read author/review state. Ordinal 1 atomically binds the version to its digests. If a bound series becomes INVALID, append its binding-ledger evidence and prepare a new series ID whose manifest **recomputes the same stable `CandidateBindingV1` digest** (which excludes the old manifest, runtime and `pre-holdout` receipts); it reruns core172 from zero for all three hosts and ordinals in fresh roots, creates a fresh `pre-holdout` attestation bound to the new manifest + stable digest + new core-leg completion, associates that attempt with the existing binding ledger, and only then reruns the complete holdout96 three-host × three-ordinal matrix in fresh roots; no success, manifest or `pre-holdout` receipt from the invalid series may be reused. A complete holdout series is sealed and then marked consumed immediately whether PASS or FAIL; the scorer reads that immutable state afterward and does not decide consumption. Reports call this an untuned/session-isolated synthetic holdout and never a model-unseen corpus.

### Phase 4 — Documentation, validation and delivery

1. Align `skills/engram/evals/DATASET_CARD.md`, `docs/guides/skill-eval.md`, README installation guidance and skill references with split identity, provenance, privacy and official score rules. Any in-package content required to be part of the evaluated/released candidate must be finalized **before** the final `package validate` snapshot. A post-snapshot edit under `skills/engram/**` is a new, explicitly unevaluated documentation/data revision and cannot replace the official snapshot or inherit its score; either keep post-score reporting outside the package or create a new snapshot and repeat the applicable formal evaluation before claiming equivalence.
2. Run focused tests, then `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test -count=1 ./...` and `go vet ./...`.
3. Verify `git diff --name-only -- memory embedding provider store internal` is empty and parity/namespace tests remain green.
4. Record that no LoCoMo rerun was required because the engine/LoCoMo path stayed unchanged; do not claim that the CLI inference endpoints are offline.
5. Before tasks/implementation and again before merge, inspect active worktrees. Current sibling 042 operates on `cmd/locomo-bench`; if either feature begins touching a common file, stop and reconcile before edits.
6. Execute every long real-CLI family-index, holdout, baseline and formal-series command on WSL2 with the repository-mandated detached `setsid` pattern, session-scratchpad log, exit-file receipt and instant polling; a foreground inherited stdout pipe is not completion evidence.

## Constitution Check — Post-Design

| Gate | Post-design result |
|---|---|
| Local-first/offline | PASS — local engram/judge path remains offline; remote inference is explicitly opt-in/existing-host configuration and not misrepresented. |
| Engine/adapter separation | PASS — all proposed source paths are adapter/skill/docs paths; engine diff is a hard empty check. |
| Contract-first/isolation | PASS — explicit schema, sealed manifests, private holdout boundary, case-level dirs and report contracts precede code. |
| Evaluation regression | PASS — no relevant engine algorithm path changes; validation plan requires parity/tests and an explicit no-LoCoMo rationale. |
| Graceful degradation/honest scale | PASS — incomplete/unavailable hosts become invalid; no partial score or inflated portability claim. |

## Complexity Tracking

No constitution violation requires justification. The extra manifest/sealing artifacts are necessary to preserve the explicitly requested independent holdout and three-run official-score contract; a one-off script or mutable JSON report cannot enforce those guarantees.
