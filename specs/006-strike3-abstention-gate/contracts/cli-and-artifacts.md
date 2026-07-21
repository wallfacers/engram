# Contracts: Strike 3 — Abstention Gate

The outward contract for this feature is (1) new `cmd/locomo-bench` flags, (2) the `abstain-probe.json` artifact schema (US1), and (3) the frontier-table artifact schema (US2). Frozen here before implementation (Constitution III). No engine API or SQLite schema change.

## 1. CLI flags (`cmd/locomo-bench`)

| Flag | Type | Default | Phase | Meaning |
|------|------|---------|-------|---------|
| `--abstain-probe` | bool | false | US1 | Run the zero-cost offline abstention probe and exit. Reuses the coverage-only retrieval path; makes **no** answer/judge calls. Requires a built store (+ `pcic_meta` for the claim signal). |
| `--abstain-probe-out` | string | `<store\|run-dir>/abstain-probe.json` | US1 | Path for the probe artifact. |
| `--abstain-gate` | string | `advrecall=0.40,falseabstain=0.05,net=100` | US1 | Override the SC-003 gate thresholds (for diagnostics; the declared gate is the default). |
| `--retrieval` arms | string | — | US2 | Extends the existing arm list with `+abstain-hard` and `+abstain-soft` operating points (paired with a baseline arm). |
| `--abstain-prompt` | bool | false | US2 | (existing) enable the abstain-oriented answer prompt for the prompt-only / soft-hint points. |

**Behavioral contract**:
- `--abstain-probe` is terminal (prints verdict, writes artifact, exits) and MUST NOT invoke any answer/judge model. Violation is a test failure (SC-001).
- Absent `pcic_meta`: the claim signal is omitted from `Signals` (not an error); the confidence signal still reports (FR-006).
- Absent reranker: `ConfidenceSource = cosine` recorded in the artifact.
- The `+abstain-*` arms MUST NOT run unless invoked explicitly; they are never the default retrieval stack (FR-012 spend gate is enforced by requiring explicit flag + authorization outside the tool).

## 2. `abstain-probe.json` (US1 artifact)

```json
{
  "store": "string",
  "counts": { "adversarial": 446, "answerable": 1540, "total": 1986 },
  "signals": [
    {
      "signal": "claim | confidence | combined",
      "auc": 0.0,
      "meets_gate": false,
      "best_point": {
        "threshold": 0.0,
        "adversarial_recall": 0.0,
        "answerable_false_abstain": 0.0,
        "net_questions": 0
      },
      "points": [
        { "threshold": 0.0, "adversarial_recall": 0.0, "answerable_false_abstain": 0.0, "net_questions": 0 }
      ]
    }
  ],
  "gate": { "min_adv_recall": 0.40, "max_false_abstain": 0.05, "min_net": 100 },
  "verdict": "GO | NO-GO",
  "winning_signal": "string | null",
  "confidence_source": "rerank | cosine",
  "zero_llm": true
}
```

**Contract invariants**: `counts.total == adversarial + answerable`; every `signal.best_point` is drawn from its `points`; `verdict == GO` iff at least one `signal.meets_gate == true`; `zero_llm` MUST be `true`.

## 3. Frontier-table artifact (US2, only on GO + authorization)

```json
{
  "baseline_note": "adversarial accuracy is a NEW declared baseline (Constitution IV)",
  "evaluated": { "adversarial": 446, "answerable_sample": 0, "repeats": 1 },
  "operating_points": [
    {
      "name": "force-answer | prompt-only | hard-gate | soft-hint",
      "adversarial_acc": 0.0,
      "answerable_acc": 0.0,
      "combined": 0.0,
      "answer_calls": 0,
      "mcnemar": { "p": 0.0, "verdict": "above-noise | within-noise" }
    }
  ]
}
```

**Contract invariants**: the `hard-gate` point MUST report `answer_calls == 0` for flagged questions (SC-005); `force-answer` is the reference point (`mcnemar` may be null); the eval-result artifact/commit is separate from mechanism-code commits (SC-007).

## 4. Engine-untouched contract (hard gate)

```
git diff --name-only master...006-strike3-abstention-gate -- memory embedding provider store internal
# MUST be empty for every mechanism commit (FR-010 / SC-006)
```
