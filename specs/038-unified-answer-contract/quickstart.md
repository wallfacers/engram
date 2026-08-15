# Quickstart: Unified Answer Contract Experiment

## Offline verification

```bash
export SPECIFY_FEATURE_DIRECTORY=specs/038-unified-answer-contract
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
git diff --name-only -- memory embedding provider store internal
```

The final command must print nothing.

The unified prompt contamination check is also offline:

```bash
rg -n 'LoCoMo|LongMemEval|answerable evaluation|gold answer|The information provided is not enough|Project Atlas|Project Beacon|vintage films|vintage cameras' \
  cmd/locomo-bench/runner.go specs/038-unified-answer-contract/contracts/answer-contract.md
```

Occurrences in historical control constants are expected in `runner.go`; none
may occur inside `unifiedAnswerContractPrompt` or the frozen contract block.
`TestUnifiedAnswerContractHasNoBenchmarkOrGoldText` enforces that scope without
false positives from the retained controls.

## Endpoint preflight

Load answer and judge configuration from the protected environment files named
in the operations runbook. Configure the local BGE-large endpoint. Check only
HTTP status/model identity; do not print tokens or env-file contents.

If the answer endpoint is unavailable, stop before score generation and mark
the verdict `BLOCKED`. Historical result files cannot score a new prompt.

## Development behavior smoke probe

The checked-in `behavior-cases.json` has 17 cases written alongside the
contract. Run it as a paired development smoke check, not as held-out evidence.
The dedicated mode compares the generic historical prompt with the unified
prompt using identical rendered request/evidence bytes and writes prompt,
fixture, and judge digests plus model metadata to the report.

It makes many model calls, so detach it on WSL2:

```bash
mkdir -p <scratch-artifact-dir>
CGO_ENABLED=0 go build -o <scratch-artifact-dir>/locomo-bench ./cmd/locomo-bench
setsid bash -c '
  <scratch-artifact-dir>/locomo-bench \
    --unified-answer-probe specs/038-unified-answer-contract/fixtures/behavior-cases.json \
    --unified-answer-probe-out <scratch-artifact-dir>/behavior-smoke-report.json \
    --unified-answer-probe-repeats 3 \
    --max-tokens 8000 --concurrency 1 \
    > <scratch-artifact-dir>/behavior-smoke.log 2>&1
  echo $? > <scratch-artifact-dir>/behavior-smoke.exit
' </dev/null >/dev/null 2>&1 & disown
```

Poll with one instant check:

```bash
test -f <scratch-artifact-dir>/behavior-smoke.exit \
  && cat <scratch-artifact-dir>/behavior-smoke.exit \
  || tail -1 <scratch-artifact-dir>/behavior-smoke.log
```

The CLI reads answer configuration from `LOCOMO_*` and judge configuration
from `JUDGE_*` (with the documented fallback), and persists no credentials.
Set explicit `LOCOMO_MODEL_REVISION` and `JUDGE_MODEL_REVISION` values; an
unset revision is recorded as `unverified:<model>` and cannot support a
promotion claim. The report contains raw model output and is therefore written
mode `0600`; keep it in the protected scratch area and treat it as sensitive.

Before interpreting behavior counts, require `.valid == true`,
`.complete == true`, `.run_status == "complete"`, and
`.operational_failures == 0`. Transport, empty-answer, or parser failures make
the artifact operationally invalid and are not counted as behavioral failures.
Inspect every genuinely judged failed case; the model judge is a smoke oracle
only. Passing all 17 cases cannot establish generalization or a <=2%
false-abstention rate.

Before promotion, create a separate, pre-registered held-out fixture not
derived from these cases, benchmark examples, mined errors, or treatment
outputs. Freeze its rubric before model calls and obtain human labels blinded
to arm. Use the same dedicated CLI only as an execution/reporting aid. The
directly-supported slice must be sized so its one-sided exact 95% upper bound
is <=2% (at least 149 cases when zero false abstentions are observed).

## Paired LoCoMo pilot

Build a reviewed binary and create a fresh run directory under the session
scratch area. Freeze a label-independent pilot whitelist before any model call;
the example below assumes that file already exists. The initial isolated pilot
does not use force-answer, category top-k/quota, temporal scaffold, typed
prompts, counter-refine, trace mediation, IDK retrieval retries, or reranking.

```bash
mkdir -p <scratch-artifact-dir> <fresh-warmup-dir> <fresh-run-dir>
CGO_ENABLED=0 go build -o <scratch-artifact-dir>/locomo-bench ./cmd/locomo-bench
sha256sum <scratch-artifact-dir>/locomo-bench \
  testdata/locomo/locomo.json \
  specs/038-unified-answer-contract/contracts/answer-contract.md \
  specs/038-unified-answer-contract/fixtures/behavior-cases.json \
  <frozen-pilot-whitelist> > <scratch-artifact-dir>/input-sha256.txt
find <frozen-private-store> -maxdepth 1 -type f -print0 | sort -z | xargs -0 sha256sum \
  > <scratch-artifact-dir>/store-sha256.txt
```

Run one discarded warm-up question into a separate fresh directory first. It
checks the call path only and is never combined with pilot results:

```bash
setsid bash -c '
  <scratch-artifact-dir>/locomo-bench \
    --data testdata/locomo/locomo.json \
    --store-dir <private-copy-of-frozen-store> \
    --run-dir <fresh-warmup-dir> \
    --chunks --top-k 30 --chunk-quota 12 \
    --retrieval "hybrid,hybrid+unified" \
    --judge-mem0-aligned --no-idk-retry --trace-mediation=false \
    --only-questions <frozen-one-question-warmup-list> \
    --repeats 1 --concurrency 1 \
    > <fresh-warmup-dir>/run.log 2>&1
  echo $? > <fresh-warmup-dir>/run.exit
' </dev/null >/dev/null 2>&1 & disown
```

After the warm-up exits successfully, run the frozen pilot cohort:

```bash
setsid bash -c '
  <scratch-artifact-dir>/locomo-bench \
    --data testdata/locomo/locomo.json \
    --store-dir <frozen-private-store> \
    --run-dir <fresh-run-dir> \
    --chunks --top-k 30 --chunk-quota 12 \
    --retrieval "hybrid,hybrid+unified" \
    --judge-mem0-aligned --no-idk-retry --trace-mediation=false \
    --only-questions <frozen-pilot-whitelist> \
    --repeats 3 --concurrency <safe-limit> \
    > <fresh-run-dir>/run.log 2>&1
  echo $? > <fresh-run-dir>/run.exit
' </dev/null >/dev/null 2>&1 & disown
```

Poll with a single instant check; do not attach a foreground sleep loop:

```bash
test -f <fresh-run-dir>/run.exit && cat <fresh-run-dir>/run.exit || tail -1 <fresh-run-dir>/run.log
```

Before reading any score, require every repeat validation receipt to pass:

```bash
jq -e '
  .schema == "unified-prompt-pair-validation/v1" and
  .valid == true and
  (.dataset_digest | startswith("sha256:")) and
  .question_count > 0 and
  .context_parity_method == "sha256_of_actual_provider_answer_user_bytes" and
  .provider_attempt_policy == "one_provider_attempt_per_answer_and_judge_call"
' <fresh-run-dir>/run-{1,2,3}/unified-pair-validation.json
```

Each result row also carries `unified_pair_audit` with schema
`unified-prompt-pair-call-audit/v1`. The validation receipt is written after
strict row-set, configuration, prompt, exactly-one-answer/judge-call,
call-success, and actual provider-facing answer-user-byte parity checks; it is
valid only when all checks pass. A missing or invalid receipt invalidates the
run; do not report its score. Per-repeat logs intentionally say
`score=pending-all-repeat-validation`; score artifacts and summaries are
produced only after every configured repeat validates.

The receipt is a call/context integrity proof, not the complete promotion
manifest. The separately written `input-sha256.txt` and `store-sha256.txt`,
explicit non-`unverified:` model revisions, endpoint/model identities, source
dirty-diff record, and binary digest must all be reviewed together. Until that
external provenance bundle is complete, any emitted `paired.json` or
`stats-*.json` is development evidence only and cannot satisfy SC-006.

The `+unified` arm is the only treatment. Never omit the frozen pilot whitelist
or replace it with an error-mined cohort: doing so turns this command into an
expensive full run and invalidates the predeclared pilot.

## Standalone unified runs (configurable, non-contrast)

The unified contract is independently runnable without a paired control arm.
A single `hybrid+unified` arm runs the contract standalone; the frozen
paired-protocol validations (exact two-arm layout, odd repeats, isolation) do
not apply because there is no control to contrast:

```bash
setsid bash -c '
  locomo-bench --dataset-format longmemeval --data "$LME" --store-dir "$STORE" \
    --run-dir "$RUN" --chunks --retrieval "hybrid+unified" \
    --top-k 150 --chunk-quota 12 \
    --judge-mem0-aligned --no-idk-retry --concurrency 32 --repeats 3 \
    --trace-mediation=false > run.log 2>&1
' </dev/null >/dev/null 2>&1 & disown
```

Equivalently, `--unified-answer-contract` with a single arm selects the same
standalone path. The paired mode (`--retrieval "hybrid,hybrid+unified"`, odd
repeats, context-parity fail-closed) remains the only score-bearing contrast
protocol and is unchanged. Standalone unified runs are development evidence
for the contract itself, not a replacement for paired verification where a
delta claim is required. (2026-08-15 config flexibility iteration; outside the
frozen 038 paired scope.)

## Combining the unified contract with LoCoMo typed prompts (opt-in)

`--unified-typed-prompts` (requires `--unified-answer-contract`, default off)
combines the unified contract with the two LoCoMo-validated typed contracts:
category 1 (multi-hop) and category 3 (open-domain) fall back to their legacy
typed prompt bytes — including the current-date rule, byte-identical to the
historical control arm for those categories — while every other category keeps
the frozen unified bytes. LongMemEval pseudo-categories are 6-12 and never
collide with 1/3, so LongMemEval runs (top-k 30 / top-k 150 baselines) are
unchanged by construction; `TestUnifiedTypedPromptsCombineLocomoContractsOnly`
asserts this byte-for-byte. The flag is rejected inside the frozen paired
protocol (it would contaminate the single-variable prompt isolation) and is
journal/formal-digest bound when enabled. (2026-08-15 eval-config change; new
prompt combinations still require their own fresh run + verdict.)

## Result interpretation

Verify the validation receipts, prompt digest, model/store/data provenance,
row counts, zero failed calls, actual provider-facing answer user-context
parity, and cost
before reading accuracy. Report answerable
accuracy, adversarial/unsupported false answers, false abstention, partial
answers, preference/action behavior, flips, McNemar, and all repetitions.

Until the separately authored held-out behavior gate and LoCoMo non-inferiority
gate both pass, leave the feature default-off and do not recommend the prompt
for product use. The 17-case smoke report cannot satisfy the held-out gate.
