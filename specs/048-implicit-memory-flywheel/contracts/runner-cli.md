# Contract: skill-eval Formal Runner CLI

**Feature**: `048-implicit-memory-flywheel`

**Contract version**: 4

**Date**: 2026-09-01

## 1. Command surface

The current `validate` and `run` surface evolves to these stable commands:

```text
skill-eval validate --dataset <dir> --split dev-regression|holdout
  [--phase pre-index|family-aware] [--dev-family-index <dev-family-index.json>]

skill-eval green-test create
  --suite holdout-pipeline|formal-tooling|series-prepare|pre-holdout
  --repository-root <repo> --bin-dir <dir>
  --validator <scripts/validate-agent-skill.mjs>
  [--skill-snapshot <immutable-dir>]
  [--skill-package-validation <validation-receipt.json>]
  [--series-root <prepared-series-root>]
  --out <green-test-receipt.json>

skill-eval package validate --skill-dir <source-dir>
  --repository-root <repo> --snapshot-root <new-immutable-dir>
  --green-test-receipt <formal-tooling-green.json>
  --out <validation-receipt.json>

skill-eval family-index build --dataset <dev-dir>
  --core-manifest <file> --review-prompt <file> --out <dev-family-index.json>
  --tool claude,codex,opencode
  --claude-settings <path> --codex-provider aq --codex-model qwen3.8-flash --opencode-model <free-model>
  --concurrency <n>

skill-eval holdout generate --protocol <file> --private-root <dir>
  --dev-family-index <dev-family-index.json>
  --author-tools claude,codex,opencode
  --claude-settings <path> --codex-provider aq --codex-model qwen3.8-flash --opencode-model <provider/model#variant>
  --stage-exec <private-stage-boundary-config>
  --green-test-receipt <holdout-pipeline-green.json>
  --concurrency <n>

skill-eval holdout review --private-root <dir>
  --stage-exec <private-stage-boundary-config>
  --green-test-receipt <holdout-pipeline-green.json> --concurrency <n>
skill-eval holdout seal --private-root <dir> --anchor <git-tag|signature|immutable-object>
  --green-test-receipt <holdout-pipeline-green.json>

skill-eval core-plan create --out <core-execution-plan.json>
  --dev-dataset <dir> --bin-dir <dir>
  --tool claude,codex,opencode
  --claude-settings <path> --codex-provider aq --codex-model qwen3.8-flash --opencode-model <free-model>
  --core-exec <core-child-boundary-config>
  --timeout <seconds> --concurrency <n>

skill-eval series prepare --series <id> --series-root <dir>
  --purpose official-dual|dev-comparison
  --core-execution-plan <core-execution-plan.json>
  --dev-dataset <dir> [--holdout-dataset <protected-dir>]
  --skill-snapshot <immutable-dir> --bin-dir <dir>
  --skill-package-validation <validation-receipt.json>
  --green-test-receipt <series-prepare-green.json>
  --tool claude,codex,opencode
  --claude-settings <path> --codex-provider aq --codex-model qwen3.8-flash --opencode-model <free-model>
  --core-exec <core-child-boundary-config>
  --timeout <seconds> --concurrency <n>
  [--protected-exec <private-boundary-config>] # required for official-dual

skill-eval run --mode primary --series <id> --split dev-regression|holdout
  --run-ordinal 1|2|3 --tool claude|codex|opencode
  --dataset <dataset-dir> # sealed for holdout; frozen manifest/commit receipt for dev
  --out <unique-series-root> --scratch <unique-run-scratch>
  --bin-dir <dir> --timeout <seconds> --concurrency <n>
  [--green-test-receipt <pre-holdout-green.json>] # required for holdout ordinal 1

skill-eval run --mode diagnostic --split dev-regression
  --dataset <dev-dataset-dir>
  --concurrency <n>
  [--include-extension] [--only <case-id,...>] [--sample <n>] [--limit <n>] ...

skill-eval failure-archive --series-root <dev-comparison-root>
  --out <failure-archive.json>

skill-eval compare --baseline-series-root <dev-comparison-root>
  --candidate-series-root <official-dual-root>
  --failure-archive <failure-archive.json>
  [--extension-receipt <dev-diagnostic-receipt>]
  --out <flywheel-comparison.json>

skill-eval score --series-root <dir> # accepts official-dual only
```

The command spelling may retain backwards-compatible aliases, but all artifacts and validation semantics above are mandatory.

### 1.1 Frozen package validation

`package validate` is the only producer of a formal `SkillPackageValidationReceipt` and `FrozenSkillPackageSnapshot`. It must:

1. reject an existing/non-empty `--snapshot-root`, any internal symlink, special file, path escape or empty package;
2. recursively enumerate the complete source package in bytewise-sorted relative-path order, record each exact-byte file digest and the existing normalized `engram-package-sha256-v1` package digest, and copy the files into a fresh staging directory;
3. invoke the existing 020 validator against that staged package and bind the exact validator revision/digest, argv, closed result and sanitized output digest;
4. recalculate the sorted file list, every file digest and package digest from the staged copy, atomically materialize it at the new snapshot root, and create a controller-held immutable anchor over `{snapshot_id, package_digest, file_records, validator_digest}`;
5. emit a passing receipt only after the materialized snapshot and anchor verify. Failure leaves no reusable formal snapshot or passing receipt.

The receipt contains the complete sorted `relative_path/file_digest/size` list, package and snapshot digests, snapshot anchor, validator revision/digest, checks, UTC time and receipt digest. The evaluated package is the snapshot, not the mutable source directory. `series prepare` and every primary child rehash the snapshot, verify its anchor and mount/materialize only that snapshot into the host's prepared skill-discovery path. The final-template canary must observe exactly one engram skill and its package digest must equal the snapshot receipt. A primary command must reject `skills/engram`, a symlink to it, a duplicate mutable/global copy, or any other mutable source as a substitute. A later edit anywhere under the source package creates a different unevaluated revision; it cannot retroactively replace the snapshot named by a score.

### 1.2 Green test attestations

`green-test create` runs a fixed, versioned suite selected by `--suite`; it does not accept an arbitrary shell command. `GreenTestReceipt` binds the exact argv vectors, exit codes, sanitized stdout/stderr digests, suite-manifest digest, runner source/binary digest, judge digest, 020 validator revision/digest and every applicable skill snapshot/package-validation/series-manifest digest. A `series-prepare` receipt additionally derives `stable_identity_digest` from the suite manifest, fixed argv set and stable runner/judge/validator/snapshot/package bindings, excluding output/time/receipt digest. That identity is the CandidateBindingV1 input and must remain equal across a bound recovery; the exact passing receipt may be newly produced. It records `passed=true` only when every required command exits zero and every bound digest is reverified.

- `holdout-pipeline` covers the generation/review/seal schema, isolation and seal fixtures and must pass before the first real holdout author/reviewer process starts; the same receipt remains usable through seal only while all bound implementation digests remain unchanged.
- `formal-tooling` covers `cmd/skill-eval` plus `node --test scripts/validate-agent-skill.test.mjs` and must pass before either pre-revision or final `package validate` runs.
- `series-prepare` additionally binds the selected frozen snapshot and passing package receipt. `series prepare` rejects a missing, failed, wrong-suite or digest-mismatched receipt.
- `pre-holdout` is created only after the complete core leg and immediately before holdout ordinal 1. It reruns the fixed formal suite, rehashes the prepared series/snapshot/runner/judge/validator/tool bindings, records the exact stable `candidate_binding_digest` and complete core-leg receipt-set digest, and rejects any drift. It is one-series-only: its exact `series_manifest_digest` and receipt digest cannot authorize another series. Holdout ordinal 1 and `HoldoutBindingReceipt` creation/attempt association reject a missing, failed or mismatched receipt.

These receipts are preconditions, not post-hoc evidence: a receipt created after the irreversible action cannot authorize that action.

`validate --split dev-regression --phase pre-index` is the only valid check before `DevFamilyIndex` exists: it validates retained core IDs/counts, 020 semantics, machine rules and paths but does not demand legacy `family_id`. `family-index build` implements `dev-family-index-v2` (supersedes v1: same-family joins now require three-lane `same_family=true` plus pairwise dash-token overlap of the topic slugs instead of byte-equal slugs): deterministic normalization/pair construction followed by fresh-session three-lane unanimous mirror review through a bounded worker pool. It freezes `--concurrency`, records observed max-in-flight, must exhibit overlap in the validation run when concurrency is greater than one, refuses an existing output file, writes full prompt/provenance/decision receipts, and never accepts a hand-edited override. `validate --phase family-aware --dev-family-index <...>` then requires complete one-to-one core family coverage and cross-split/mirror safety. `holdout` validation always performs family-aware validation against the frozen index.

`holdout generate/review` require `--stage-exec`. For every exact author/reviewer child, the harness creates a fresh state/input workspace and proves own input readable while private-root traversal/list/read, generation-audit read, author-receipt read, prior-review read and active-sibling-workspace read are denied. The private `AuthorReviewIsolationReceipt` binds safe child/template/config/state/probe digests into the dataset manifest; anonymous JSON without this process boundary cannot be sealed.

`holdout generate`, `holdout review` and `holdout seal` also verify the same passing `holdout-pipeline` GreenTestReceipt against the current implementation digests before launching or sealing anything. A code/binary/judge/validator change after attestation requires a new receipt; real holdout plaintext must never be used as a test fixture.

`core-plan create` captures the final runner/judge identities, frozen core172 manifest, three ordinal seeds, timeout/concurrency, per-host stable `tool_identity_digest` values, normalized host × worker identity set, boundary policy and core child execution/isolation template. `--core-exec` supplies the nonsecret user/container/mount/ACL-equivalent template used by both baseline and candidate core runs; unique UID/container/root IDs are normalized away, but runtime image, process/network policy and visibility semantics are retained. The receipt excludes the evaluated skill, purpose, series ID, unique artifact roots and all holdout-only data. A comparable baseline and candidate series must import the exact same receipt rather than independently reconstructing these values.

`run --mode diagnostic` is dev-only and must use a bounded worker pool honoring its explicit `--concurrency`; its receipt records configured concurrency and observed max-in-flight. Selector/retry affordances do not relax that requirement and the resulting artifact remains permanently score-ineligible.

## 2. Mode rules

| Capability | primary | diagnostic |
|---|---|---|
| Split | dev-regression core or sealed holdout96 | dev-regression core, optionally append-only extension |
| `--only`, `--sample`, `--limit` | reject | allowed |
| case-level agent retry | reject | allowed, every attempt retained |
| result may enter `score` | official-dual only; dev-comparison no | no |
| output root | `primary/` only | `diagnostics/` only |
| holdout revision guidance | never | N/A; holdout diagnostics rejected |

`--mode primary` rejects an unset/mismatched seal, incomplete 3-ordinal series configuration, mutable/overlapping output roots, or tool invocation provenance that differs from the series manifest. It never silently downgrades to diagnostic.

`--purpose official-dual` requires both datasets. `--purpose dev-comparison` requires only core172, still requires three ordinals for every host, and is permanently ineligible for `score` or headline publication; it exists only for the SC-5 before/after comparison and must reject a holdout dataset. Both purposes require `--core-execution-plan` and `--core-exec`; their current runner/judge/core/tool identity/timeout/concurrency/seed/worker-identity/boundary/template values must match the same plan before a series can seal. `official-dual --protected-exec` may add holdout-root protections but cannot change the normalized core child identity/boundary semantics.

For any series containing holdout, `series prepare` requires `--protected-exec`. After the final tool templates, state roots, concurrency and worker identities are frozen, it launches a noninteractive helper through every host × worker-slot exact evaluated-child boundary. The probe matrix must deny protected-root traversal/list/read, author/review audit/state reads and every concurrently active sibling-workspace read, while allowing the worker's own materialized workspace. The resulting private `ProtectedExecutionReceipt` binds the boundary/config/template/identity/root-policy digests, isolated capacity and closed matrix outcomes to the series manifest. A repository-external path, dataset seal or separately chmod'd sentinel without this receipt is rejected.

For every purpose, `series prepare` verifies `--skill-package-validation` was produced by `package validate`, is passing, and names the exact anchored `--skill-snapshot` file list/package digest. It independently rehashes the snapshot and verifies the passing `series-prepare` GreenTestReceipt against that snapshot and the current runner/judge/validator digests. After that and before sealing the series manifest, it automatically runs the workspace canary whenever a bound dataset contains staged workspace files. The canary uses the frozen snapshot, final command/cwd/materialization template and every prepared child slot that can execute those cases; it writes one private `WorkspaceCanaryReceipt` per host × worker slot and binds the complete receipt map into the manifest. There is no separate `preflight workspace` command. Missing, failed or mismatched snapshot/anchor/green-test/canary/package receipts reject preparation.

If primary `run` repeats `--timeout` or `--concurrency`, both values must exactly equal the sealed series manifest; omission means use the sealed value. A mismatch is a usage error before any case starts.

## 3. Formal series protocol

`skill-eval series prepare` first validates the passing exact-snapshot `SkillPackageValidationReceipt`, immutable snapshot anchor, `series-prepare` GreenTestReceipt, sealed `CoreExecutionPlanReceipt` and every dataset manifest bound by its purpose. It recaptures each host's ToolProvenance and compares only the stable `tool_identity_digest` to the plan; differing `captured_at` values are expected and never weaken the identity check. `ToolProvenance.source_revision` is the revision/digest of the `cmd/skill-eval` runner source subtree and/or built runner binary only; its preimage must exclude `skills/engram/**`, datasets, specs/docs and run artifacts, so the skill snapshot remains SC-5's sole intentional variable. It also launches the final core child wrappers sufficiently to prove normalized worker identity, boundary and execution templates match the plan. For `official-dual`, it then creates the successful `ProtectedExecutionReceipt`, proves its actual core identities/boundary/templates normalize to the same plan digests, derives the normalized protected-execution policy and stable `CandidateBindingV1` digest, and only afterward creates and seals `series-manifest.json`. The stable digest includes the snapshot/anchor/package receipt, runner/judge/validator, exact dataset identities, core plan, tool/config/template policy and the `series-prepare` `stable_identity_digest`; it excludes `series_id`, manifest digest, exact per-series green/runtime receipt digests, roots/times and future `pre-holdout` receipt. Whenever staged files exist, it automatically creates workspace-canary receipts for every host × prepared worker slot from these exact final child wrappers before that seal. The series manifest binds its purpose, core plan, selected dataset manifests, all three hosts, three ordinals, tool templates, frozen skill snapshot/anchor, runner/judge identities, package-validation and green-test receipts, timeout/concurrency, exact question counts and (when present) protected-execution and workspace-canary receipt digests before computing its own digest. The holdout dataset seal itself never contains these runtime receipts. A `run --mode primary` without a valid prepared series fails.

For every `host × split × ordinal`:

1. Materialize a unique root, per-case workspace/store directory, and disposable HOME/XDG/cache/session/container state root. No formal state root may be reused by another case, ordinal or split.
2. Verify the matching dataset seal and exact case-ID set.
3. Capture sanitized CLI/config/model provenance before execution.
4. Generate the pre-registered deterministic order for that ordinal.
5. Seed every case through the existing public CLI path. A non-null `SeedMemory.event_date` is rendered as `[event_date=YYYY-MM-DD]` in content and verified in the seed receipt; it is never represented as structured engine EventDate.
6. Assign the case to one prepared worker slot. Before the child starts, controller-confirm a prior-case state target and retired workspace target when any exist; run exact-child denial probes against both, then run the case once. The `CaseStateIsolationReceipt` records that slot and, for official-dual, the exact `ProtectedWorkerProbe` digest; actual child identity/template/access-boundary digests must match the prepared slot. `cmd.Dir`, per-case `ENGRAM_DATA_DIR`, MCP config and any CLI cwd flag point to the same case directory.
7. Destroy the child session/container, quarantine its state root and workspace under a controller-only retirement boundary long enough for the next child's existence-proven denial probes (or perform a controller-verified final deletion when no later case exists), and persist `CaseStateIsolationReceipt`, all normalized/raw-event/store receipts and a terminal `CaseRunReceipt`.
8. Validate exact coverage, uniqueness, provenance, state-root non-reuse and receipt completeness.
9. Write `run-manifest.json`, then `run-seal.json`; no field may change after this point.

Primary ordinals 1, 2 and 3 are independent full runs, not retries. An incomplete or unavailable ordinal makes that host × split series invalid; it does not create a partial score. Starting again creates a distinct series ID and preserves the failed series intact.

For a holdout split, ordinal 1 first verifies a fresh passing `pre-holdout` GreenTestReceipt created after this series' complete core leg. The receipt must name the exact prepared manifest, its stable `CandidateBindingV1` digest and the complete core-leg receipt set. Controller then atomically creates the first protected `HoldoutBindingReceipt`, or appends/associates a recovery attempt on the existing receipt, recording that manifest and receipt digests before any holdout child starts. The immutable series manifest does not point back to this later append-only receipt. A replacement series after INVALID is allowed only when its **stable** candidate binding digest is identical; its series manifest and fresh `pre-holdout` receipt are deliberately new. A changed CandidateBindingV1 input requires a newly generated and sealed holdout version.

An `official-dual` series may start its holdout leg only after every core172 host × ordinal run is complete and the series is not `INVALID`. If the core leg becomes invalid before holdout ordinal 1 starts, preserve that failed series and prepare a new series ID using the same candidate snapshot binding and the exact same `CoreExecutionPlanReceipt`, then rerun the complete core leg. This recovery path must not create or mutate a `HoldoutBindingReceipt`, consume the holdout, or use a different snapshot/tool/runner/judge/config.

If an `official-dual` series becomes `INVALID` after a holdout binding exists, recovery is never a continuation or partial reuse. Preserve every failed artifact and append a recovery event to the protected binding ledger. Prepare a new series ID whose independently sealed manifest recomputes the same **stable** `CandidateBindingV1` digest; do not copy the old manifest, runtime receipts or `pre-holdout` receipt. Rerun the **complete** core172 matrix first, all three hosts and all three ordinals, in fresh run/case/state roots. Only then create a new `pre-holdout` receipt bound to that new manifest and core-leg completion, append/associate that attempt on the existing binding ledger, and run the complete holdout96 matrix with the same fresh-root rule. No successful ordinal, split receipt or green-test receipt from the invalid series may enter the recovery series. The final `OfficialScoreReport` references only the one complete recovery series; prior invalid series IDs/digests remain non-scoring binding-ledger evidence. A different stable binding digest requires a new holdout version, and abandoning recovery does not release the bound version for another candidate.

### 3.1 Dev flywheel receipts

`failure-archive` accepts only a complete sealed `dev-comparison` series. It derives the three-ordinal binary median for every host × core172 case, emits the closed dev failure taxonomy, and writes an immutable archive plus seal. It rejects any series/dataset/run receipt with `split=holdout`, any path that requires holdout traversal, and any request for an official score/headline.

`compare` validates the sealed archive, baseline and candidate manifests, requires both series to reference the exact same `CoreExecutionPlanReceipt`, and requires every observed per-host `tool_identity_digest` to match that plan. It opens only the candidate's exact core172 run paths from the manifest; it must not recursively scan the `official-dual` root or read holdout case/receipt paths. It writes a separately sealed `FlywheelComparisonReceipt` with fail-to-pass/regression counts, a sorted/deduplicated required extension-backfill source-ID set and digest, and optional extension diagnostics explicitly marked non-comparable/non-gating. The extension verification command/path must require exactly one manifest-contained successor in `extension_lineage` for every emitted source ID and reject extra/missing/duplicate or wrong-source mappings. It never writes either official score family; only `score` can do that.

## 4. Host invocation requirements

### Claude Code

- Requires an explicit `--settings ~/.claude/settings.json.aly_qwen_w` (or an equivalent pre-sealed path/digest), never fallback to ordinary `settings.json`.
- Uses print/stream JSON, an ephemeral/no-persistence setting when supported, a fresh case cwd and only the required MCP/CLI permissions.
- The report names the host `claude`; it names a resolved model only if runtime evidence provides one.

### Codex

- Uses the maintainer-required `-c model_provider=aq -c model=qwen3.8-flash --yolo` invocation identity together with noninteractive JSON output.
- Must set both the process working directory and `codex exec -C <caseDir>`; use `--ephemeral` when supported.
- A file-visibility canary must pass before formal file-backed trap scoring. Failure invalidates rather than downgrades Codex file cases.

### OpenCode2

- Uses noninteractive JSON output, a fresh case cwd and an explicit `--model <free-model>` for authoring, review and formal evaluation.
- Formal provenance records both requested and resolved model. An absent model identity cannot be relabeled as “free.”

No command line, log, report or tracked file may contain a credential. The runner passes only required environment names/paths and stores their safe digests, never values.

## 5. Artifact layout

```text
<series-root>/
  frozen-skill-snapshot.ref.json
  skill-package-validation.ref.json
  green-test-series-prepare.ref.json
  core-execution-plan.ref.json
  series-manifest.json
  series-seal.json
  primary/
    <host>/<split>/run-01/
      run-manifest.json
      run-seal.json
      cases.jsonl
      raw/<case-id>/attempt-01.jsonl
      normalized/<case-id>.json
      store/<case-id>.txt
      workspace/<case-id>.sha256
      state-isolation/<case-id>.json
      failures.jsonl
    ... run-02/ and run-03/ ...
  preflight/
    protected-execution.json
    workspace-canary.json
    pre-holdout-green-test.json
  binding-ledger/
    holdout-binding.json
    recovery-events.jsonl
  diagnostics/
    <diagnostic-id>/...
  official-report.json
  official-report.seal.json
```

The output root must be unique per series. Reusing an existing primary run root is an error, not an overwrite. Every raw event stream is first passed through the deterministic secret filter; arbitrary stderr lives only in protected diagnostic material and is represented in formal receipts by a closed error code plus digest.

Any series root that contains holdout cases or receipts must itself live under the protected holdout boundary. Only a plaintext-free aggregate official report/seal export may be copied to the tuning checkout.

On WSL2, every long real-CLI author/review/baseline/primary command must be launched with the repository-required detached `setsid` pattern. The operation record retains a sanitized argv digest, session-scratchpad log digest, exit-file digest/code and launch mode; a foreground command or inherited stdout EOF is not completion evidence. These launch receipts are operational provenance only and cannot repair an otherwise missing formal receipt.

## 6. Runner unavailable and failures

- `runner-unavailable`: CLI missing, preflight fails, required settings/profile/model mismatch, model endpoint cannot start, or three consecutive infrastructure failures before coverage. It is distinct from a case verdict and makes the corresponding primary series invalid.
- `runner-error`: after a host passed preflight, one scheduled case timed out or exited unsuccessfully in its only primary agent attempt. Its terminal receipt remains in the denominator; in a positive module it is a non-pass, and in a negative module the scorer must count it conservatively in the official false-positive numerator while reporting it separately. No selective retry is allowed. If the runner stops before producing one terminal receipt for every expected case, the run is incomplete and INVALID.
- `false-negative`, `false-positive`, `wrong-op`, `wrong-report`: deterministic judge failure classes.

No process failure is repaired by reissuing only that case in primary mode. Diagnostic retries are permitted only in a separate dev artifact and are never read by `score`.

## 7. File-system safety

Candidate `id`, `workspace_files.path`, dataset root, output root and scratch root are validated before use. All joins must resolve underneath their declared parent after clean/abs/symlink checks. `RemoveAll` may only receive an already validated per-case directory under a unique run scratch root. The runner must never clean a shared checkout, a user home directory, or an unvalidated prefix.

## 8. File-backed and protected-execution preflight

`series prepare` automatically performs this gate before it seals a series containing staged-file cases:

1. Create one fresh fictional nonsecret canary workspace per host with an expected file and expected digest.
2. Use an already prepared worker slot and run the host-specific visibility prompt through the exact same skill, wrapper, cwd/session/materialization configuration as formal cases.
3. Verify observable result, cwd digest, staged-file digest, child identity/template and boundary digests against the prepared slot contract.
4. Write private `preflight/workspace-canary.json` receipts for every host × prepared worker slot and bind the complete digest map in the series manifest.

Every host × prepared worker slot that can run staged-file cases must pass for cross-host trap comparison. A missing or failed receipt rejects `series prepare` and the runner may not replace environment-backed cases with empty-workspace semantics.

### 8.2 Protected execution

Before `series prepare` seals a holdout series:

1. The operator provides a private boundary configuration describing separate users, containers, mount namespaces, ACLs, or equivalent independently isolated worker slots. Raw configuration, identities and paths are never copied into a tracked file or public report.
2. The harness validates that isolated worker capacity is at least the requested concurrency, that formal HOME/XDG/cache/session/container allocators are fresh and disjoint from frozen author/review roots, that core and holdout allocator sets are disjoint, that the holdout set has not been used by core work, and that each concurrently usable slot has an independently isolated effective child identity.
3. The harness creates nonsecret probe targets underneath the actual protected root, author/review audit root, author/review state roots and worker workspaces. For every target it records a controller-side existence/content nonce digest and actual parent access-policy digest immediately before the exact child launch. A separately chmod'd target, missing controller proof or mismatched policy digest invalidates the preflight.
4. For every host × worker slot, the harness launches the exact formal wrapper/identity/template and records closed probes for protected-root traversal, directory listing and file read; author/review audit/state read; own-workspace read; and, when concurrency is greater than one, reads of every simultaneously active sibling workspace. The per-case runner repeats the same evidence pattern for prior-case state and retired-case workspace denial before every child starts, records the slot/probe reference, and rejects any actual child identity/template/boundary that differs from the prepared slot.
5. Forbidden operations pass only with `permission-denied`, or with `not-found` when the controller proof establishes that the target existed immediately before launch; own-workspace read passes only with `readable`. Successful forbidden access, failed own access, missing pairwise sibling/prior-state/retired-workspace coverage, root reuse, identity/template/policy mismatch, unavailable probe or any other result makes `series prepare` or the affected run fail before scoring.
6. The harness writes the private `ProtectedExecutionReceipt`, then seals its digest into `FormalSeriesManifest`. The dataset manifest/seal remains unchanged.

This preflight proves the configured process-level access boundary for the prepared series; it does not prove that the underlying models never generated or reviewed the synthetic cases. The report may claim only untuned/session-isolated synthetic holdout evidence. The runner still materializes only the current case workspace for the evaluated CLI.
