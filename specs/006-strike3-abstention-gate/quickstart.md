# Quickstart: Strike 3 — Abstention Gate

Validation guide proving the feature works end-to-end. See [contracts/cli-and-artifacts.md](./contracts/cli-and-artifacts.md) for flag/artifact detail and [data-model.md](./data-model.md) for types.

## Prerequisites

- A persisted 10-conversation LoCoMo store (reuse the 003/005 build; `--store-dir`).
- The `pcic_meta.json` sidecar from feature 005 US2 (for the claim signal; optional — probe degrades to confidence-only without it).
- Local fastembed/bge sidecar for offline query vectors. Reranker (DashScope `gte-rerank-v2`) optional — enables the `rerank` confidence source, else `cosine`.
- No answer/judge LLM needed for US1.

## US1 — Free offline probe (zero cost, the MVP + gate)

```bash
CGO_ENABLED=0 go build ./cmd/locomo-bench
./locomo-bench --abstain-probe \
  --data testdata/locomo/locomo10.json \
  --store-dir <persisted-store> \
  --pcic-meta <persisted-store>/pcic_meta.json \
  --abstain-probe-out ./scratch/abstain-probe.json
```

**Expected**:
- Prints per-signal AUC + best operating point for `claim`, `confidence`, `combined`.
- Prints a single `Verdict: GO` or `Verdict: NO-GO` against the declared gate (adv-recall ≥ 0.40 @ false-abstain ≤ 0.05; net ≥ +100).
- Writes `abstain-probe.json` matching the contract; `zero_llm: true`.
- `counts` = `{adversarial:446, answerable:1540, total:1986}`.
- Completes with **zero answer/judge tokens** and no store mutation.

**Decision**: NO-GO ⇒ record the verdict + ROC diagnostic in the eval log; **stop, no paid answering**. GO ⇒ proceed to US2 only after explicit cost authorization.

## US1 test-level validation (offline, CI)

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Abstain'
```

Asserts: signal grading (`no-match`/`entity-only`/`entity+slot`); ROC/AUC on a synthetic separable set (AUC→1.0) and inseparable set (AUC→0.5); gate verdict logic; degradation when `pcic_meta` absent; **no answer/judge caller invoked** during the probe; no engine state written; no secret in the artifact.

## US2 — Paid paired frontier (only on GO + authorization)

```bash
# estimate first (Constitution IV cost discipline)
./locomo-bench --retrieval 'hybrid+rerank,hybrid+rerank+abstain-hard,hybrid+rerank+abstain-soft' \
  --only-category 5 --answerable-sample <N> --repeats 1 \
  --abstain-prompt --store-dir <persisted-store> --estimate

# run after authorization
./locomo-bench ... (same, without --estimate)
```

**Expected**: frontier table with OP0/OP1/OP2a/OP2b rows (adversarial acc, answerable acc, combined), McNemar per paired arm, and `answer_calls == 0` on hard-gate-flagged questions. Adversarial result recorded as a **declared new baseline**; eval-result commit separate from mechanism code.

## Engine-untouched gate (every commit)

```bash
git diff --name-only master...006-strike3-abstention-gate -- memory embedding provider store internal   # → empty
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./...
```
