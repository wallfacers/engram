# Quickstart: Risk-Controlled Second-Pass Adjudication

This guide proves offline contracts before any hosted audit. It contains no credential.

## 1. Build and test offline

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
CGO_ENABLED=0 go build ./...
```

Expected: both exit 0 without network; ordinary CLI and frozen 034 modes retain parity.

## 2. Build the frozen risk queue

Use a new session-scratch directory. The parent source is the complete, immutable 034 Stage-0 directory.

```bash
export AUDIT_STAGE_DIR=/path/to/session-scratch/035-stage0
export ADJUDICATION_SOURCE=/path/to/session-scratch/034-stage0

go run ./cmd/locomo-bench \
  --adjudication-audit-build "$AUDIT_STAGE_DIR" \
  --adjudication-source "$ADJUDICATION_SOURCE" \
  --adjudication-audit-seed 035-stage0-v1
```

Expected: parent label-free journal/seal validation passes; build makes zero calls and writes exactly 477 audit packets,
954 views, and 1540 resolver rows. Queue split is 424 accepted semantic overrides + 53 triggered fallbacks; 1063 rows
are zero-call retains.

## 3. Validate provider-safe artifacts

```bash
go run ./cmd/locomo-bench --adjudication-audit-validate "$AUDIT_STAGE_DIR"
```

Expected: strict schemas/digests/counts, two-or-three normalized groups, different view order, and forbidden-field scan
pass. Validation does not open parent score/slot-map/custody/raw candidates, provider environment, or network.

## 4. Offline stub seal before hosted use

The implementation test suite injects callers for valid dual convergence, conflicting views, invalid output, provider
failure, resume, orphan rejection, 32-concurrency bounding, and schedule-independent seals. No external command needs a
provider for this step.

Expected fixture receipt: 477 packets x 2 views = 954 attempts, zero retries, 1540 decisions, and an exact reproducible
seal. Failed view terminals retain the parent answer.

## 5. Optional paid Stage-0

Proceed only after offline verification, manual review of `planned_calls=954`, and a newly rotated credential supplied
through the operator environment. Never place a key in chat, a script, a tracked file, or a log.

```bash
export ADJUDICATOR_PROVIDER=openai
export ADJUDICATOR_BASE_URL=https://provider.example/v1
export ADJUDICATOR_MODEL=provider-model-id
export ADJUDICATOR_MODEL_REVISION=provider-model-revision
export ADJUDICATOR_API_KEY='set-privately-in-the-operator-environment'

setsid bash -c 'CGO_ENABLED=0 go run ./cmd/locomo-bench \
  --adjudication-audit-run "$AUDIT_STAGE_DIR" \
  --adjudication-audit-allow-paid \
  --concurrency 32 \
  --adjudication-audit-max-tokens 768 \
  >"$AUDIT_STAGE_DIR/run.log" 2>&1
echo $? >"$AUDIT_STAGE_DIR/run.exit"' </dev/null >/dev/null 2>&1 & disown
```

Poll with one instant check, never a foreground sleep loop:

```bash
[ -f "$AUDIT_STAGE_DIR/run.exit" ] && cat "$AUDIT_STAGE_DIR/run.exit" || tail -n 20 "$AUDIT_STAGE_DIR/run.log"
```

Valid completion requires exactly 954 STARTED + terminal pairs, zero retry/orphan/duplicate, 1540 decisions, and a valid
audit seal. One view never short-circuits the other.

## 6. Post-seal historical Stage-0 score

```bash
go run ./cmd/locomo-bench \
  --adjudication-audit-score "$AUDIT_STAGE_DIR" \
  --adjudication-source "$ADJUDICATION_SOURCE" \
  --adjudication-candidate /path/to/r1/results-hybrid.jsonl \
  --adjudication-candidate /path/to/r2/results-hybrid.jsonl \
  --adjudication-candidate /path/to/r3/results-hybrid.jsonl
```

Expected: scorer validates parent + 035 seals before hidden reads and recomputes the 034 baseline rather than reading its
old score. GO requires point and judge-instability worst bounds >=1387/1540, triggered-mixed point/worst >=69/88,
paired net >=9, overall exact McNemar p<0.05, non-negative temporal net, no Holm-significant negative category, and all
integrity gates. Output remains `historical_verdict_mapping`; NO-GO stops without formal rejudge.

## 7. Measured Stage-0 result (2026-08-10)

The approved V4-Pro run was executed once from the frozen manifest at concurrency 32. Artifacts and logs remain outside
the repository under the session scratchpad run `035-materialize-a.RVp4zU`; the parent receipt is
`034-stage0.DR07JV`. No formal rejudge was initiated.

### Paid-run receipt

| Field | Measured value |
|---|---:|
| Questions / risk questions | 1540 / 477 |
| Planned / started / terminal calls | 954 / 954 / 954 |
| Completed / invalid-response terminals | 940 / 14 |
| Provider attempts / retries | 954 / 0 |
| Decisions / retained / switched | 1540 / 1539 / 1 |
| Input / output tokens | 5,202,033 / 166,006 |
| Pricing | unpriced; no rate was supplied, so no cost estimate is claimed |
| Seal | valid |

The 14 invalid responses affected 13 packets and conservatively retained the parent answer. They were not the dominant
failure mode: another 463 risk decisions completed both views but did not satisfy dual convergence. Across completed
views, the parent answer was strictly refuted in only 10/473 entailment views and 16/467 falsification views. A view
both strictly refuted the parent and exposed exactly one supported, non-contradicted alternative in only 4 and 5 cases,
respectively. Only one packet satisfied those conditions for the same alternative in both views.

### Historical score and gates

| Gate | Required | Measured | Result |
|---|---:|---:|---|
| New historical mapping | >=1387/1540 | 1378/1540 (89.48%) | fail |
| Judge-instability lower bound | >=1387/1540 | 1375/1540 | fail |
| Triggered mixed mapping | >=69/88 | 61/88 | fail |
| Triggered mixed lower bound | >=69/88 | 60/88 | fail |
| Paired net | >=+9 | 0 (new-only 0, parent-only 0) | fail |
| Exact McNemar | p<0.05 | p=1.0 | fail |
| Temporal net | >=0 | 0 | pass |
| Holm-significant negative category | none | none; all category deltas 0 | pass |
| Integrity / frozen diagnostics | valid | valid | pass |

Verdict: **NO_GO**. The only switch was correctness-neutral under the frozen historical mapping, so the score and every
category remained identical to the 034 parent.

### Failure analysis

The mechanism behaved as designed, but its safety contract and the observed model labels are mismatched for score
recovery. A switch requires both independent views to mark the selected answer contradicted and unsupported, find
exactly one supported and non-contradicted alternative, and converge on the same normalized group. The strong parent
answer was rarely strictly refuted, while multiple candidate answers were often supportable or semantically equivalent.
The resolver therefore filtered 476/477 risk questions back to the parent answer. Relaxing this rule after observing
hidden outcomes would be post-hoc label adaptation and is not allowed by the frozen experiment.

This result closes the 035 direction as historical-only evidence. A future attempt needs a newly specified mechanism
that improves error targeting or answer generation before adjudication, plus a fresh label-blind gate; it must not reuse
this paid run to tune thresholds and must not present hosted adjudication as a shipped or default retrieval lever.
