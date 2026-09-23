# Quickstart: 048 Dual-Score Trigger Benchmark

This guide validates the feature after implementation. It deliberately distinguishes safe local development work from a protected formal series. Do not place run artifacts, models, holdout plaintext, settings, tokens or databases in the repository root.

See [data-model.md](data-model.md), [dataset protocol](contracts/dataset-protocol.md), [runner CLI contract](contracts/runner-cli.md), and [score contract](contracts/scoring-report.md) for the normative rules.

## 1. Preconditions

- Work from the `048-implicit-memory-flywheel` checkout only; inspect `git status` and `git worktree list` first.
- Build local public binaries with CGO disabled.
- Use a unique session scratchpad/run root outside the tracked repository.
- Claude Code uses the maintainer-provided `~/.claude/settings.json.aly_qwen_w`; Codex uses provider `aq` + model `qwen3.8-flash`; OpenCode2 names an explicitly confirmed free model for family review, holdout generation/review and formal evaluation.
- Keep all credentials in environment/config locations. Never copy them into a command transcript, manifest, source file or report.
- Keep holdout plaintext and every plaintext-bearing holdout run receipt in a protected private checkout/user/container, separate from the skill-tuning checkout and from the filesystem view of evaluated `--yolo` agents. Only aggregate report/seal summaries without case text may leave that boundary. Before a formal series, the exact-child matrix must deny protected-root traversal/list/read, author/review audit/state reads and active-sibling workspace reads while allowing the worker's own workspace; every concurrent worker needs an independent equivalent boundary and formal HOME/XDG/cache/session/container allocators must be separate from author/review roots. Every primary case receives a disposable state root, must fail to read controller-confirmed prior-case state and retired workspace, and is torn down afterward; core and holdout allocators are disjoint.
- Do not run any holdout command until its required fixed-suite `GreenTestReceipt` verifies against the current runner/judge/validator/snapshot/series digests. Implementation tests use fictional fixtures; never generate real holdout content merely to test a command.

```bash
CGO_ENABLED=0 go build ./cmd/engram ./cmd/engram-mcp ./cmd/skill-eval
CGO_ENABLED=0 go test -count=1 ./cmd/skill-eval
```

## 2. Validate the retained dev/regression split

The retained 172 cases must pass `pre-index` validation before any flywheel change. Legacy core payloads are not required to carry `family_id` until the separate frozen index exists.

```bash
skill-eval validate \
  --dataset skills/engram/evals \
  --split dev-regression \
  --phase pre-index
```

Expected result: exactly 172 cases, retained IDs/020 semantics, unique case IDs, deterministic machine rules, and all pre-index structural gates pass. This validates the official development/regression split; it does not make it a blind generalization test.

Build the frozen machine-derived family index before any holdout candidate is generated:

```bash
skill-eval family-index build \
  --dataset skills/engram/evals \
  --core-manifest skills/engram/evals/dev-regression-core.manifest.json \
  --review-prompt skills/engram/evals/prompts/dev-family-index-review-v1.md \
  --out skills/engram/evals/dev-family-index.json \
  --tool claude,codex,opencode \
  --claude-settings ~/.claude/settings.json.aly_qwen_w \
  --codex-provider aq --codex-model qwen3.8-flash \
  --opencode-model '<confirmed-free-provider/model#variant>' \
  --concurrency 3
```

Expected result: deterministic exact groups plus only unanimously confirmed cross-language mirror groups, with prompt/tool provenance and an immutable output digest; no human mappings.

Then validate family coverage and cross-split collision rules against the exact frozen index:

```bash
skill-eval validate \
  --dataset skills/engram/evals \
  --split dev-regression \
  --phase family-aware \
  --dev-family-index skills/engram/evals/dev-family-index.json
```

## 3. Generate, review and seal the private holdout

Run these only from the protected holdout environment after the implementation exposes the commands. `<private-root>` is outside the repository and unavailable to tuning/evaluated CLI processes except for one materialized case at a time.

First create the fixed `holdout-pipeline` attestation from fictional fixtures. It must predate the first author/reviewer process and remain digest-current through sealing.

```bash
skill-eval green-test create \
  --suite holdout-pipeline \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --out <private-root>/receipts/holdout-pipeline-green.json

skill-eval holdout generate \
  --protocol <private-root>/holdout-protocol.json \
  --private-root <private-root> \
  --dev-family-index skills/engram/evals/dev-family-index.json \
  --author-tools claude,codex,opencode \
  --claude-settings ~/.claude/settings.json.aly_qwen_w \
  --codex-provider aq --codex-model qwen3.8-flash \
  --opencode-model '<confirmed-free-provider/model#variant>' \
  --stage-exec <private-stage-boundary-config> \
  --green-test-receipt <private-root>/receipts/holdout-pipeline-green.json \
  --concurrency 3

skill-eval holdout review \
  --private-root <private-root> \
  --stage-exec <private-stage-boundary-config> \
  --green-test-receipt <private-root>/receipts/holdout-pipeline-green.json \
  --concurrency 3

skill-eval holdout seal \
  --private-root <private-root> \
  --anchor '<immutable-tag-or-signature-id>' \
  --green-test-receipt <private-root>/receipts/holdout-pipeline-green.json

skill-eval validate \
  --dataset <private-root>/sealed-holdout \
  --split holdout
```

Expected result: 96 accepted cases, four implicit modules of 20 each, trap `8/4/4`, 48 zh / 48 en, 32 cases per authoring host, and eight closed scenario buckets of 12 (each author 4, zh/en 6/6, 10 implicit + 2 trap). Each case has exactly two agreeing non-author reviews that independently inferred the label/rules from a label-blind envelope; reviewer-visible candidate digest covers only the canonical de-labeled projection, and reviewers receive actual anonymous label-free dev/accepted family-summary payloads whose digests/revisions match the envelope. Controller then performs the private slot and accepted-family CAS checks. The private seal covers an append-only complete author/reviewer attempt ledger and every launched stage-isolation receipt, including rejected/stale/failed attempts. All author/reviewer model identities are host-stable and non-`unavailable` across that complete ledger; the three host harnesses are distinct while the unified underlying model may repeat (2026-09-01 maintainer decision). Every author/reviewer child can read only its own input workspace and cannot read private/audit/receipt/prior-review/sibling material. Rejected, disagreed, stale-family or isolation-invalid candidates are regenerated/reviewed automatically; there is no human review or tie-break. This seal does not yet prove a future evaluated CLI's filesystem isolation; `series prepare` creates that separate runtime receipt from final invocation templates.

## 4. Development diagnostics and dev flywheel

Use diagnostics only on `dev-regression`; it may select cases to investigate or run the append-only extension, but its results cannot become formal scores. The 172-case core denominator remains immutable.

```bash
skill-eval run --mode diagnostic \
  --split dev-regression \
  --dataset skills/engram/evals \
  --tool claude \
  --only iw-pos-001 \
  --sample 12 \
  --out <run-root>/diagnostics/after-change \
  --scratch <unique-scratch-root> \
  --bin-dir <bin-dir> \
  --concurrency 3
```

The early diagnostic run is exploratory only. After the final runner/judge command surface is available, produce a passing `formal-tooling` GreenTestReceipt, then use `package validate` to materialize an immutable pre-revision snapshot and bind the existing 020 validator to it. Create one sealed `CoreExecutionPlanReceipt`, then prepare and run that snapshot as a core-only `dev-comparison` primary series for all three hosts and all three ordinals; it cannot enter `score` or a headline. Use its binary per-case median states to make the sealed comparable FailureArchive, then apply the failure-driven skill revision, rerun formal-tooling, and materialize/validate an independently anchored final snapshot. The core172 before/after rows must reference the identical core plan: runner, judge, stable per-host `tool_identity_digest`, core dataset, timeout, concurrency, case-order seeds, normalized worker identity/boundary and disposable-state execution/isolation-template are fixed; the two frozen package snapshots are the only intentional difference. `ToolProvenance.source_revision` identifies only the runner subtree/binary, never the skill or docs. First run the candidate core leg and compare it to emit the exact extension-backfill set; append that set one-to-one, then run the complete post-change core-plus-extension **diagnostic** regression in independent CLI/HOME/cache/session roots without repreparing the candidate series; report extension results separately.

```bash
skill-eval green-test create \
  --suite formal-tooling \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --out <formal-receipts>/formal-tooling-green.json

skill-eval package validate \
  --skill-dir <pre-revision-mutable-skill-source> \
  --repository-root <repo-root> \
  --snapshot-root <protected-snapshots>/pre-revision \
  --green-test-receipt <formal-receipts>/formal-tooling-green.json \
  --out <formal-receipts>/pre-revision-skill-package-validation.json

skill-eval green-test create \
  --suite series-prepare \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --skill-snapshot <protected-snapshots>/pre-revision \
  --skill-package-validation <formal-receipts>/pre-revision-skill-package-validation.json \
  --out <formal-receipts>/pre-revision-series-prepare-green.json
```

```bash
skill-eval core-plan create \
  --out <core-execution-plan.json> \
  --dev-dataset skills/engram/evals \
  --bin-dir <bin-dir> \
  --tool claude,codex,opencode \
  --claude-settings ~/.claude/settings.json.aly_qwen_w \
  --codex-provider aq --codex-model qwen3.8-flash \
  --opencode-model '<confirmed-free-provider/model#variant>' \
  --core-exec <core-child-boundary-config> \
  --timeout 240 \
  --concurrency 3
```

```bash
skill-eval series prepare \
  --series <pre-revision-comparison-id> \
  --series-root <comparison-series-root> \
  --purpose dev-comparison \
  --core-execution-plan <core-execution-plan.json> \
  --dev-dataset skills/engram/evals \
  --skill-snapshot <protected-snapshots>/pre-revision \
  --skill-package-validation <formal-receipts>/pre-revision-skill-package-validation.json \
  --green-test-receipt <formal-receipts>/pre-revision-series-prepare-green.json \
  --bin-dir <bin-dir> \
  --tool claude,codex,opencode \
  --claude-settings ~/.claude/settings.json.aly_qwen_w \
  --codex-provider aq --codex-model qwen3.8-flash \
  --opencode-model '<confirmed-free-provider/model#variant>' \
  --core-exec <core-child-boundary-config> \
  --timeout 240 \
  --concurrency 3

# after all three hosts × three primary core ordinals complete
skill-eval failure-archive \
  --series-root <comparison-series-root> \
  --out <failure-archive.json>
```

## 5. Prepare and produce the two official score families

Freeze the skill/runner/judge/dataset/tool configuration before ordinal 1. Allocate unique output and scratch roots for every run. Because the series includes holdout, `<series-root>` and every child receipt must live inside the protected holdout boundary; export only the plaintext-free aggregate report/seal. Repeat for each host, each split and each ordinal 1–3.

After the final skill revision, create a new passing formal-tooling receipt, then materialize the final snapshot and its validator receipt. Never pass mutable `skills/engram` to `series prepare` or a primary run.

```bash
skill-eval green-test create \
  --suite formal-tooling \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --out <formal-receipts>/final-formal-tooling-green.json

skill-eval package validate \
  --skill-dir skills/engram \
  --repository-root <repo-root> \
  --snapshot-root <protected-snapshots>/final-candidate \
  --green-test-receipt <formal-receipts>/final-formal-tooling-green.json \
  --out <formal-receipts>/final-skill-package-validation.json

skill-eval green-test create \
  --suite series-prepare \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --skill-snapshot <protected-snapshots>/final-candidate \
  --skill-package-validation <formal-receipts>/final-skill-package-validation.json \
  --out <formal-receipts>/series-prepare-green.json
```

```bash
skill-eval series prepare \
  --series <series-id> \
  --series-root <series-root> \
  --purpose official-dual \
  --core-execution-plan <core-execution-plan.json> \
  --dev-dataset skills/engram/evals \
  --holdout-dataset <private-root>/sealed-holdout \
  --skill-snapshot <protected-snapshots>/final-candidate \
  --skill-package-validation <formal-receipts>/final-skill-package-validation.json \
  --green-test-receipt <formal-receipts>/series-prepare-green.json \
  --bin-dir <bin-dir> \
  --tool claude,codex,opencode \
  --claude-settings ~/.claude/settings.json.aly_qwen_w \
  --codex-provider aq --codex-model qwen3.8-flash \
  --opencode-model '<confirmed-free-provider/model#variant>' \
  --core-exec <core-child-boundary-config> \
  --timeout 240 \
  --concurrency 3 \
  --protected-exec <private-boundary-config>

for split in dev-regression; do
  for ordinal in 1 2 3; do
    skill-eval run --mode primary \
      --series <series-id> \
      --split "$split" \
      --run-ordinal "$ordinal" \
      --tool claude \
      --dataset <sealed-dataset-for-$split> \
      --out <series-root> \
      --scratch <unique-scratch-root> \
      --bin-dir <bin-dir> \
      --timeout 240 \
      --concurrency 3
  done
done

# Run this only after every candidate core receipt is complete and the series is valid.
skill-eval compare \
  --baseline-series-root <comparison-series-root> \
  --candidate-series-root <series-root> \
  --failure-archive <failure-archive.json> \
  --extension-receipt <independent-core-extension-diagnostic-receipt> \
  --out <flywheel-comparison.json>

# This fixed suite has no holdout plaintext. It must immediately precede ordinal 1.
skill-eval green-test create \
  --suite pre-holdout \
  --repository-root <repo-root> \
  --bin-dir <bin-dir> \
  --validator scripts/validate-agent-skill.mjs \
  --skill-snapshot <protected-snapshots>/final-candidate \
  --skill-package-validation <formal-receipts>/final-skill-package-validation.json \
  --series-root <series-root> \
  --out <series-root>/preflight/pre-holdout-green-test.json

for ordinal in 1 2 3; do
  skill-eval run --mode primary \
    --series <series-id> \
    --split holdout \
    --run-ordinal "$ordinal" \
    --tool claude \
    --dataset <private-root>/sealed-holdout \
    --out <series-root> \
    --scratch <unique-scratch-root> \
    --bin-dir <bin-dir> \
    --timeout 240 \
    --concurrency 3 \
    --green-test-receipt <series-root>/preflight/pre-holdout-green-test.json
done

skill-eval score --series-root <series-root>
```

Repeat the primary loop for `codex` and `opencode`. The actual orchestrator may parallelize independent cases/hosts when the endpoints allow it, but it must preserve unique roots and all receipt identities.

`series prepare` first rehashes the immutable `<protected-snapshots>/final-candidate` file list/anchor and verifies that `<formal-receipts>/final-skill-package-validation.json` was produced for that exact snapshot by the existing 020 validator. It also requires a current passing `series-prepare` GreenTestReceipt. It then validates the shared core plan and creates the private `ProtectedExecutionReceipt`: it exercises every host × worker-slot exact child, checks root/audit/state denial, own-workspace readability and, at concurrency above one, pairwise active-sibling denial. If any bound case uses staged workspace files, the same command automatically executes a fictional workspace canary for every usable host × worker slot using the frozen snapshot, wrapper, cwd and materialization template; these receipts are sealed into the series manifest. Codex must use both process cwd and `codex exec -C <caseDir>`. Controller-side proofs establish every denied target existed under the real parent policy. Each case receipt must name its prepared worker slot/probe and match its identity/template/access boundary. The run then enforces disposable per-case state with prior-state/retired-workspace probes; `not-found` without a controller proof is not success. A repository-external path, a dataset seal, or a separately chmod'd probe file cannot substitute for these receipts. For `official-dual`, its manifest also carries a stable `CandidateBindingV1` digest: it preserves the frozen snapshot/anchor/package receipt, runner/judge/validator, exact dataset identities, core plan and stable tool/config/execution-policy plus the `series-prepare` `stable_identity_digest`, but excludes this series ID/manifest, exact per-series green/runtime roots/receipts and the later `pre-holdout` receipt.

Expected result:

- two separately named outputs: `dev/regression score` and `generalization score`;
- each host/split has exactly three complete full-run receipts and a median for every module metric;
- every host independently meets the applicable ≥90% positive / ≤10% negative gates, including the dev 020 regression gate and the holdout trap gate;
- any incomplete/unavailable host series is `INVALID`, not replaced with a partial or combined score;
- `diagnostic_artifacts_used` is false.

## 6. Verify artifacts and repository boundaries

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
go vet ./...
git diff --name-only -- memory embedding provider store internal
git diff --check
```

Expected result: all commands succeed; the final `git diff --name-only` output is empty. The delivery report explicitly says that LoCoMo was not rerun because neither engine behavior nor the LoCoMo path changed.

For long runs on WSL2, detach using `setsid` and poll the run’s log/exit receipt; do not rely on inherited stdout EOF as completion. For example, launch from a session scratchpad as `setsid bash -c '<command> >run.log 2>&1; echo $? >run.exit' </dev/null >/dev/null 2>&1 & disown`, then perform an instant exit-file/log check. Record the sanitized argv digest, log digest, exit-file digest/code and detached launch mode in the operation receipt. A failed primary ordinal is retained and makes its series invalid — start a fresh series rather than overwriting or selectively retrying it. If an `official-dual` core leg fails before holdout ordinal 1, do not start holdout: retain the failure and prepare a fresh series with the same stable `CandidateBindingV1` inputs and `CoreExecutionPlanReceipt`, then rerun core. Holdout ordinal 1 requires the fresh `pre-holdout` GreenTestReceipt created after that exact series' complete core leg; controller records the manifest, stable candidate digest, core-completion and receipt as one binding attempt before any holdout child starts. The version becomes consumed immediately after its complete sealed three-ordinal holdout series, before `score` reads it. If it becomes INVALID after binding but before completion, it stays bound and unconsumed: record a recovery ledger event, prepare a new series whose **stable** candidate digest is identical but whose manifest/runtime receipts are new, rerun all core172 host × ordinal work in fresh roots, then create a new manifest-bound `pre-holdout` receipt and associate it with the existing binding before rerunning all holdout96 host × ordinal work. No run, manifest or `pre-holdout` receipt from the invalid series may contribute to the recovery report; a changed stable binding input requires a new holdout version. Any valid holdout report remains **untuned/session-isolated synthetic holdout evidence**, not a model-unseen claim.
