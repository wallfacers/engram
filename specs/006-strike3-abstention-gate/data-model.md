# Phase 1 Data Model: Strike 3 — Abstention Gate

All types are internal to `cmd/locomo-bench` (adapter). None touch the engine or the SQLite schema. Field names are indicative; the frozen contract is the JSON artifact shape in `contracts/`.

## AbstainSignal (per question, offline)

The raw discriminators computed for one question, before any threshold is applied.

| Field | Type | Meaning |
|-------|------|---------|
| `QuestionID` | string | conversation-scoped question identifier |
| `Category` | int | LoCoMo category (5 = adversarial) |
| `Adversarial` | bool | `Category == 5` (label) |
| `ClaimMatch` | enum `{no-match, entity-only, entity+slot}` | best grade of demand entity(+slot) against retrieved candidates' `pcic_meta` claims |
| `Confidence` | float64 | normalized top-1/top-k relevance; source recorded in `ConfidenceSource` |
| `ConfidenceSource` | enum `{rerank, cosine}` | which score fed `Confidence` (degradation transparency) |
| `ClaimSignalPresent` | bool | false when no `pcic_meta` available → claim signal drops out (Constitution V) |

**Derivation**: entity(+slot) via `EntitySignalsForQuery`/`derivePCICSignals` (no LLM); claims via `pcic_meta` `SpanKey`-scoped lookup over the retrieved candidate turns; confidence from candidate relevance scores already present after retrieval/rerank.

**Invariants**: computing an `AbstainSignal` makes zero answer/judge LLM calls and mutates no engine state.

## AbstainDecision (signal + threshold → action)

Pure function of an `AbstainSignal` and a threshold configuration; kept separate so the probe can sweep.

| Field | Type | Meaning |
|-------|------|---------|
| `Abstain` | bool | true ⇒ decline (skip/deprioritize answering) |
| `Rule` | string | which signal(s) fired (`confidence<τ`, `claim=no-match`, `combined`) |

**Correctness (definitional, offline-scorable)**: adversarial + `Abstain` ⇒ correct; answerable + `Abstain` ⇒ incorrect (via `adversarialGold` convention).

## ROCPoint & SignalROC (probe output)

| ROCPoint field | Type | Meaning |
|----------------|------|---------|
| `Threshold` | float64 | τ at this point |
| `AdversarialRecall` | float64 | flagged adversarial / 446 |
| `AnswerableFalseAbstain` | float64 | flagged answerable / 1540 |
| `NetQuestions` | int | 446·recall − 1540·falseabstain·0.676 |

| SignalROC field | Type | Meaning |
|-----------------|------|---------|
| `Signal` | enum `{claim, confidence, combined}` | which discriminator |
| `Points` | []ROCPoint | swept curve |
| `AUC` | float64 | trapezoidal area |
| `BestPoint` | ROCPoint | point meeting the gate, else closest |
| `MeetsGate` | bool | BestPoint satisfies SC-003 |

## ProbeReport (US1 sole deliverable → `abstain-probe.json`)

| Field | Type | Meaning |
|-------|------|---------|
| `Store` | string | store dir / fingerprint used |
| `Counts` | `{adversarial:int, answerable:int, total:int}` | population sizes (expect 446 / 1540 / 1986) |
| `Signals` | []SignalROC | per-signal curves + AUC + best point |
| `Gate` | `{minAdvRecall:0.40, maxFalseAbstain:0.05, minNet:100}` | the declared SC-003 gate |
| `Verdict` | enum `{GO, NO-GO}` | overall decision |
| `WinningSignal` | string \| null | signal+threshold that cleared the gate on GO |
| `ZeroLLM` | bool | asserted true (no answer/judge calls) |

## OperatingPoint & Frontier (US2 paid deliverable)

| OperatingPoint field | Type | Meaning |
|----------------------|------|---------|
| `Name` | enum `{force-answer, prompt-only, hard-gate, soft-hint}` | OP0 / OP1 / OP2a / OP2b |
| `AdversarialAcc` | float64 | accuracy on the 446 (declining = correct) |
| `AnswerableAcc` | float64 | accuracy on the answerable sample |
| `Combined` | float64 | combined figure over the evaluated set |
| `AnswerCalls` | int | answer-model calls (hard-gate: 0 on flagged questions) |
| `McNemar` | `{p:float64, verdict:string}` \| null | vs the paired baseline arm |

`Frontier` = ordered set of `OperatingPoint` + the declared new-baseline note (Constitution IV).

## Reused entities (read-only, unchanged)

- **`pcic_meta` / SpanClaim** (feature 005): `{Entity, Slot, Value, Polarity, TimeState, SourceTurnIDs}`, conversation-scoped `SpanKey`. Consumed read-only.
- **Adversarial question / `adversarialGold`** (existing bench): category-5 item; correct response declines; `adversarial_answer` is the trap.
- **`memory.Result`** candidates + their relevance scores (engine public type).
