# Evaluation Protocol: Unified Answer Contract

## Hypothesis

One category-independent system prompt can preserve answerable-question quality
while reducing unsupported answers and avoiding harmful false abstention. A
benchmark score increase is neither required nor sufficient for promotion.

## Frozen axes

Control and treatment MUST share dataset bytes, selected questions, private
copies of the same store snapshot, retrieved evidence, answer/embed/judge
provider and model revisions, thinking mode, generation limits, retry policy,
judge prompt, concurrency policy, and repetition count. The sole intended
variable is the answer system-prompt digest.

Every run manifest records:

- source revision plus dirty-diff digest and binary digest;
- dataset and store-manifest digests;
- answer contract and effective prompt-set digests;
- answerer/embedder/judge provider, model, revision, and relevant generation
  settings without secrets;
- arm order, warm-up disposal, timestamps, failures, usage, and cost.

Operational validity is separate from behavior correctness. Any transport
failure, empty final answer, malformed judge response, incomplete row set, or
missing provenance invalidates the run and is excluded from behavioral
pass/fail denominators. All configured repetitions must validate before any
accuracy or uplift is displayed or published.

## Cohorts

1. The checked-in 17-case behavior suite is a development smoke set only. It
   was authored alongside the contract and cannot support a generalization or
   promotion claim, even if every case passes.
2. A separately authored held-out behavior cohort for promotion. Requests and
   evidence must not be derived from the 17 smoke cases, benchmark examples or
   mined benchmark errors. Expected/prohibited behavior is frozen before model
   calls and judged by reviewers blinded to arm; an LLM judge may assist but
   cannot be the sole promotion label.
3. LoCoMo category 1–4 full regression, plus category-5 adversarial when
   reporting false-answer behavior.
4. LongMemEval-S as explicitly post-hoc compatibility diagnostic.

Previously mined error cohorts may be smoke tests only, never promotion data.

## Execution

1. Health-check all three endpoint roles without printing credentials.
2. Build and hash a fresh binary from the reviewed source.
3. Freeze a fresh manifest of the current canonical store; never rely on an
   older receipt with a different manifest.
4. Run the 17-case development smoke probe. Treat failures as a development
   stop, and never treat passes as held-out evidence.
5. Run and discard a warm-up.
6. Pilot the same frozen question cohort with `hybrid` control and `hybrid+unified`
   treatment in one run where possible. Confirm evidence/context parity and
   zero failed model calls from the fail-closed per-row provider-call audit and
   per-repeat validation receipt. Context parity means equal SHA-256 digests of
   the actual provider-facing answer-user bytes, not inferred equality from
   retrieval settings.
7. If pilot gates pass, run three repetitions on the full cohort in the same
   operational window. Use fresh run directories; never resume a legacy journal.
8. Before any promotion decision, run the separately frozen held-out behavior
   cohort and obtain blinded human labels for both arms.
9. Compute per-question majority, control-only-correct/treatment-only-correct
   flips, exact McNemar, per-slice metrics, usage, latency, and cost.

## Gates

- LoCoMo answerable majority accuracy delta >= -0.5pp and no statistically
  significant regression.
- Unsupported/wrong-entity false-answer rate does not exceed control.
- Held-out directly-supported false-abstention <=2%; relative increase over
  control <=1pp; and the one-sided exact 95% upper confidence bound is <=2%.
  With zero observed false abstentions this requires at least 149 independent
  directly-supported cases; any observed event requires a larger sample.
- No critical slice (alias, partial evidence, preference/action, temporal,
  update, injection, sensitive inference) has a material unexplained regression.
- Zero category-dependent prompt branches and zero benchmark/gold/example text
  in the unified system prompt.

The 17 development fixtures do not satisfy any held-out promotion gate. Any
failed measured gate is `NO-GO`. Missing endpoint, missing blinded human labels,
or an undersized/incomplete comparable cohort is `BLOCKED`, not `GO`.
LongMemEval alone cannot promote the feature.
