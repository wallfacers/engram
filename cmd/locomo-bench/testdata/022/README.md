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
