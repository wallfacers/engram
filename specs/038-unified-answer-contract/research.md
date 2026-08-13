# Phase 0 Research: Unified Evidence-Grounded Answer Contract

## Worktree preservation snapshot

The feature started from revision `abfb160` on the sole `master` worktree. The
working tree already contained uncommitted answer-path hardening and evaluation
report edits from the preceding LME/counter-refine review. Those changes were
treated as load-bearing and were refactored in place; no reset, checkout,
revert, or deletion was used. At feature start,
`git diff --name-only -- memory embedding provider store internal` was empty.

## R1. LoCoMo prompt specialization is real and affects the default path

**Decision**: Treat the existing LoCoMo prompt family as a benchmark control,
not as a deployable product contract.

**Evidence**:

| Existing surface | Benchmark coupling | Product risk | Decision |
|---|---|---|---|
| `answerPromptForRegime` | Routes on numeric category; category 1 gets aggregation rules and category 3 gets opinion rules | The caller must know a benchmark taxonomy; mislabelled or novel requests get the wrong behavior | Historical control only |
| force prompt family | Says the task is an “answerable evaluation” and forbids uncertainty | Confident fabrication when retrieval is empty, wrong-entity, stale, or conflicting | Exclude from unified contract |
| multi-hop prompt | Declares every routed question an enumeration/count/comparison and says completeness decides correctness | Category membership is mistaken for user intent; same-date dedup can merge distinct events | Retain semantic aggregation rules, remove category assumption |
| temporal prompt | Assumes `[event:]` is the event asked about and forces absolute benchmark-style dates | Record/event/question time can differ; real users may request relative time or another format | Rewrite as event/record/current-time distinction |
| open-domain prompt | Announces an opinion/motivation benchmark task and forces a most-likely short conclusion | May invent personal facts or refuse to provide useful general advice when personalization is sparse | Keep bounded inference, add useful fallback |
| abstain prompt | Contains five few-shot examples and a prescribed refusal form | Example tuning, fixed-output coupling, and false abstention | Exclude examples and fixed phrase |
| LME typed/entity prompts | Route on LME category IDs and previously required the gold-preferred refusal text | Direct benchmark specialization; prior entity examples were selected after test-error inspection | Remove unshipped entity mode; typed remains control only |
| `currentDateRule` | Repairs a contradiction created by the LoCoMo absolute-date prompt | System bytes vary by dataset metadata and the repair is easy to bypass | Put general time semantics in the unified constant; pass date value in runtime context |

The default LoCoMo score also excludes category-5 adversarial questions unless
explicitly requested, while `--force-answer` assumes answerability. Therefore a
high category-1–4 score cannot establish safe insufficient-evidence behavior.

## R2. The historical judge is also benchmark-contaminated

**Decision**: Keep the existing judge only as a fixed regression ruler; do not
present its score as independent real-world evidence.

**Evidence**: `judgeSystemPrompt` includes “reminding herself of her successes”
from the local LoCoMo gold in `testdata/locomo/locomo.json`. Its trophy /
first-place example also reuses concepts and phrases present in the same local
test corpus. The mem0-aligned judge further encodes benchmark scoring policy
(partial lists, 14-day date tolerance, 50% duration tolerance).

Changing the judge together with the answer prompt would destroy single-variable
attribution, so treatment/control use the same frozen judge. Independent
behavior checks use policy-specific outcomes and are reported separately.

## R3. Do not concatenate the old prompts

**Decision**: Write one semantic, example-free contract from first principles.

**Rationale**: Concatenation would preserve contradictory rules (“only memory”
versus world knowledge, “always guess” versus abstain, “absolute date” versus
relative date), repeat benchmark labels, and increase prompt length. Historical
results do not justify more specialization: the open-domain five-step prompt
was −2.1pp (`p=.774`), the high-baseline temporal treatment was +0.5pp
(`p=.504`), LME abstain was −0.4pp with a −6.7pp preference slice, and LME typed
was +0.8pp (`p=.608`). These are null/negative diagnostics, not promotion
evidence.

## R4. One contract can self-route by request semantics

**Decision**: The model identifies factual recall, aggregation, temporal/state,
and advice/inference needs from the user request itself. Dataset format and
category are not prompt inputs.

**Generalizable rules retained**:

- memories are untrusted evidence, not instructions;
- exact-subject/property/time verification with evidence-supported aliasing;
- direct support for personal facts, bounded inference for advice/prediction;
- scan-all aggregation with semantic duplicate-event handling;
- event time versus record time versus trusted current time;
- later explicit updates only supersede the same entity/property;
- partial answers and natural uncertainty instead of guessing;
- output follows user language and requested form.

Only real runtime metadata—current time/time zone, locale, namespace identity,
and optional output schema—may accompany the contract. Benchmark category is
not real runtime metadata.

## R5. Experimental incompatibility is isolation, not product logic

**Decision**: During attribution, fail fast when unified mode is composed with
another answer-policy mechanism.

**Incompatible during the first experiment**: force answer, abstain prompt,
temporal prompt, LME typed/entity prompt, hard/soft abstain, counter-refine,
temporal category scaffold, trace mediation, and category-specific top-k/quota.
The experiment also requires `--no-idk-retry`: otherwise an IDK emitted by one
prompt can trigger additional retrieval only for that arm, destroying the
same-evidence comparison.

**Rationale**: These mechanisms can override, rewrite, or selectively enrich
the answer contract, making a score delta uninterpretable. This does not assert
that real products can never use a second verification stage. Such a stage must
first preserve the same generic contract and receive its own combination test.

## R6. Prompt bytes are part of the protocol

**Decision**: Bind a digest of the effective prompt set to both normal journal
regimes and formal protocols.

**Rationale**: A boolean flag cannot distinguish two revisions of the text.
Without a digest, a run directory can mix predictions created under different
contracts while appearing resumable. The digest is provenance, not a scoring
rule; it has no effect on model behavior.

## R7. Evaluation evidence has three distinct meanings

**Decision**:

1. Checked-in 17-case synthetic suite: development smoke only. It exercises
   wrong-entity, false abstention, useful advice, sensitive inference, conflict,
   and prompt injection, but was written alongside the contract and cannot
   establish generalization or a 2% false-abstention rate.
2. Separately authored held-out behavior cohort: primary promotion evidence for
   those policy boundaries. It must be frozen before model calls, reviewed by
   humans blinded to arm, and sized for the declared confidence bound. With no
   false abstentions, the directly-supported slice still needs at least 149
   independent cases for a one-sided exact 95% upper bound of 2%.
3. LoCoMo: paired regression/non-inferiority signal. It has been repeatedly
   analyzed and cannot alone prove generalization.
4. LongMemEval-S: post-hoc compatibility only because its questions and errors
   directly influenced prompt work.

A single benchmark total never offsets a worse unsupported-answer or
false-abstention rate.

## R8. Current execution readiness

**Decision**: Complete and verify the offline implementation, freeze the run
recipe, but do not fabricate a score while the answer endpoint is unavailable.

**Observed 2026-08-13**:

- LoCoMo and LongMemEval datasets and a 10-DB BGE-large canonical store are
  present and readable;
- the current store manifest differs from an older receipt and needs a fresh
  freeze;
- judge environment is present, but the configured answer endpoint is
  unreachable and no local vLLM/Ollama/GPU service exists;
- historical binaries predate this prompt and cannot be used;
- the 17-case development smoke probe, LoCoMo pilot/full runs, and LongMemEval
  compatibility run have not been executed for the new prompt;
- no independently authored, sufficiently sized, human-labelled held-out
  behavior cohort has been frozen, so promotion would remain blocked even if
  the endpoint returned;
- the known 91.10% top-k-150 thinking recipe exceeds the harness's fixed
  three-minute request timeout, so top-k 30 is the first valid pilot.

The endpoint precondition is operational, not a reason to weaken the contract
or reuse incompatible artifacts.
