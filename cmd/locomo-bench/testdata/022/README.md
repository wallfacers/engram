# 022 Evaluation Artifact Fixtures

This directory is reserved for deterministic, synthetic fixtures for the
`022.v1` evaluation protocol. It is intentionally small enough to commit and
contains neither a benchmark dataset nor any provider credential.

Add fixtures here only when an offline unit or integration test needs to cover
one of the contracts in
[`specs/022-benchmark-parity-memory-architecture/contracts/evaluation-artifacts.md`](../../../../specs/022-benchmark-parity-memory-architecture/contracts/evaluation-artifacts.md):

- protocol canonicalization, hashing, and dirty/resume refusal;
- ranked-anchor and rendered-candidate byte replay;
- source-coverage strata and candidate-set digests;
- compiler trace, bundle, classification, and paired-summary validation.

Each fixture must use invented question, candidate, and source IDs; use short
synthetic text; declare the expected digest or verdict next to the fixture; and
be readable with no network, model endpoint, or local dataset. Do not copy
LoCoMo/LongMemEval prompts, conversations, answers, API keys, endpoints, or
runtime logs here.

The first tests may create a minimal fixture such as:

```text
protocol-valid.json
candidates-one-question.jsonl
```

Those names are illustrative, not an alternate runtime artifact location.
Real benchmark artifacts remain in the session scratchpad/run directory and
are referenced only by digest and question ID in tracked reports.

## judge-audit fixture

`judge-audit/run-{1,2,3}/results-{control,treatment}.jsonl` is a deterministic
three-repetition answer journal over three invented questions (`q-1`, `q-2`,
`q-3`). The control arm is judge-correct on every repetition; the treatment
arm is judge-incorrect on every repetition, so the frozen selection rules pick
all three questions as discordant and the blinded packet set covers both arms.
It drives `TestJudgeAuditCLIWorkflow` offline:

```text
judge-audit/run-1/results-control.jsonl   control arm, repetition 1
judge-audit/run-1/results-treatment.jsonl treatment arm, repetition 1
judge-audit/run-2/results-control.jsonl   control arm, repetition 2
judge-audit/run-2/results-treatment.jsonl treatment arm, repetition 2
judge-audit/run-3/results-control.jsonl   control arm, repetition 3
judge-audit/run-3/results-treatment.jsonl treatment arm, repetition 3
```

Expected offline behavior (asserted by the test): prepare emits blinded
packets + a separate private key; finalize with two agreeing reviewers binds
the run protocol/artifact hashes and reports a HOLD→GO verdict change at the
0.9 accuracy gate (raw accuracy 0.5, corrected 1.0).
