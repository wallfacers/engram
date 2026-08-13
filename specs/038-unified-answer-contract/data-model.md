# Phase 1 Data Model: Unified Evidence-Grounded Answer Contract

This feature adds no database schema. Its entities are evaluation and answer
protocol objects.

## UnifiedAnswerContract

| Field | Meaning | Invariant |
|---|---|---|
| text | Complete system prompt | One constant; no examples, dataset names, category IDs, gold phrases, or scorer instructions |
| digest | SHA-256-derived protocol digest | Changes whenever effective prompt bytes change |
| enabled | Experimental selection | Default false |
| runtime context | Trusted current date/time and other host metadata | Values are user-side context; they do not select or mutate the system prompt |

The contract contains six policy groups: grounding, identity, aggregation and
state, time, reasoning/action, sufficiency/output.

## PromptRegime

| Regime | Selection | Role |
|---|---|---|
| historical control | all unified selectors false | Existing category-routed behavior, byte-preserved |
| unified treatment | global flag or `+unified` arm | Same base prompt for every dataset/category |

Invalid prompt regimes are rejected before any model call. A unified treatment
cannot also select another answer-policy prompt or post-answer rewrite.

## PairedEvaluationRecord

Existing result rows are reused. The effective `AnswerRegime` gains the prompt
digest, allowing every row to prove which contract produced it. Pairing key is
the existing question ID; control and treatment share question, evidence,
answerer/judge configuration, repetition, and generation settings.

For the exact `hybrid,hybrid+unified` experiment, each row also contains a
`unified_pair_audit` receipt (`unified-prompt-pair-call-audit/v1`) with digests
of the actual provider-facing system/user/output bytes, call status, usage, and
latency. Each repeat directory contains `unified-pair-validation.json`
(`unified-prompt-pair-validation/v1`). Scoring is fail-closed unless that
receipt proves complete one-to-one rows, one successful answer and judge call
per row, matching non-prompt fingerprints, and identical control/treatment
digests for the actual provider-facing answer-user bytes. It also binds the
dataset digest captured immediately after load, prompt/model revisions,
generation limits, one-attempt policy, and the concurrent arm-scheduling
policy. No per-repeat accuracy is exposed until all configured repeats have
valid receipts.

## BehaviorProbeReport

The paired development probe report explicitly separates operational validity
from judged behavior. `valid && complete && run_status == "complete"` is
required before reading `passed`, `failed`, or arm summaries. Transport errors,
empty final answers, malformed judge JSON, and input-encoding failures use
stable status codes, increment `operational_failures`, suppress behavior
summaries, and cause a non-zero CLI exit. Raw answer/judge output is retained
only for smoke debugging; the atomic report is mode `0600` and is a sensitive
artifact. Fixture and report paths must not name the same file or inode.

## BehaviorCase

Logical fields shared by development smoke fixtures and a future held-out
cohort:

| Field | Description |
|---|---|
| id | Stable, non-benchmark identifier |
| request | User request with no benchmark label |
| memory evidence | Independent facts, dates, duplicates, conflicts, or injected instructions |
| runtime context | Optional trusted current time/timezone/locale |
| expected behavior | Required facts/actions/uncertainty boundary |
| prohibited behavior | Wrong-entity transfer, invented fact, sensitive inference, full refusal, or instruction following |
| slice | direct, alias, entity-mismatch, aggregation, temporal, update, partial, advice, sensitive, injection |

The checked-in JSON contains 17 development smoke cases authored alongside the
contract. These fixtures are separate from the system prompt and are never
copied into it as few-shot examples, but that separation does not make them
independent validation data.

## BehaviorCohort

| Field | Description |
|---|---|
| role | `development-smoke` or `promotion-held-out` |
| provenance | Author/reviewer roles and confirmation that cases were not derived from benchmark errors, current smoke cases, or treatment outputs |
| fixture digest | Digest frozen before any model call |
| label protocol | Expected/prohibited behavior plus blinded human-review procedure |
| slice counts | Counts by direct support, unsupported, wrong entity, and other declared slices |
| sample-size rule | Predeclared confidence-bound requirement for each quantitative promotion gate |

A promotion-held-out cohort is a separate artifact; it is not currently
checked in. For the 2% false-abstention gate, its directly-supported slice must
be large enough for a one-sided exact 95% upper bound of at most 2% (at least
149 cases if zero false abstentions are observed).

## PromotionVerdict

Inputs are paired benchmark results, held-out behavior results with blinded
human labels, prompt and model provenance, cost, and failure logs. Development
smoke results are diagnostics only. State is `GO`, `NO-GO`, or `BLOCKED`:

- `GO`: every accuracy and reliability gate passes on frozen evidence;
- `NO-GO`: a measured gate fails;
- `BLOCKED`: a required model endpoint, held-out cohort, human labels, or other
  comparable evidence is unavailable, so no score claim is made and unified
  mode remains default-off.
