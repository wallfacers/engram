# CLI Contract: Risk-Controlled Adjudication Audit

All 035 modes are additive and mutually exclusive with each other and all four 034 adjudication modes. Dispatch happens
before the ordinary `--data` requirement. With no 034/035 mode, benchmark behavior is unchanged.

## Shared flags

| Flag | Contract |
|---|---|
| `--adjudication-audit-build DIR` | Offline build into a new directory |
| `--adjudication-audit-validate DIR` | Offline validation; no provider or hidden inputs |
| `--adjudication-audit-run DIR` | Explicit hosted dual-view run and seal |
| `--adjudication-audit-score DIR` | Offline post-seal hidden join |
| `--adjudication-source DIR` | Frozen parent 034 directory; build and score only |
| `--adjudication-audit-seed STRING` | Required build-only view permutation seed |
| `--adjudication-audit-allow-paid` | Required run-only paid acknowledgement |
| `--adjudication-audit-max-tokens N` | Run output cap; default 768, minimum 1 |
| `--concurrency N` | Run worker bound; Stage-0 value 32 |
| `--adjudication-candidate PATH` | Existing repeatable flag; exactly three paths for score only |

Arguments owned by another mode are rejected. Supplying any 034 and 035 mode together is an error before file or
provider access.

## Build

```text
locomo-bench --adjudication-audit-build NEW_DIR \
  --adjudication-source PARENT_034_DIR \
  --adjudication-audit-seed 035-stage0-v1
```

### Preconditions

- The destination has no prior 035 manifest/journal/decision/seal/score.
- Parent public artifacts, label-free calls, decisions, and seal pass full 034 validation.
- Frozen parent digests/counts match the paid 034 Stage-0 receipt.
- Build can run with score, slot-map, custody, and raw candidate files absent or unreadable.

### Effects

Writes `audit-manifest.json`, `audit-packets.jsonl`, and `resolver-map.jsonl` atomically. It creates exactly 477 packets,
954 views/planned calls, and 1540 resolver rows. It makes zero provider calls and does not write source paths.

## Validate

```text
locomo-bench --adjudication-audit-validate DIR
```

Reads only the three build artifacts. Recomputes strict schemas, protocol/packet/resolver digests, candidate grouping,
view derangement, forbidden-field scans, queue counts, and exact 1540/477/424/53/1063/954 receipts. It does not read
environment provider configuration or network.

## Run and seal

```text
ADJUDICATOR_PROVIDER=... ADJUDICATOR_BASE_URL=... ADJUDICATOR_MODEL=... \
ADJUDICATOR_MODEL_REVISION=... ADJUDICATOR_API_KEY=... \
locomo-bench --adjudication-audit-run DIR \
  --adjudication-audit-allow-paid --concurrency 32 \
  --adjudication-audit-max-tokens 768
```

The complete `ADJUDICATOR_*` provider contract from 034 applies. No fallback to `LOCOMO_*` or `JUDGE_*` is permitted.
Only the endpoint SHA-256, never the raw URL/key, is persisted. Optional input/output CNY price variables must be both
present or both absent.

The run schedules both views of all 477 packets regardless of the first result: exactly 954 attempts for a valid seal.
Every `(packet,view)` writes fsynced STARTED then terminal COMPLETED/FAILED. There are zero retries. Provider/parse/usage
failure is terminal and causes retain. An orphan STARTED prevents resume and seal; a terminal sibling plus unstarted view
resumes only the unstarted unit.

On complete terminal state, run deterministically resolves 1540 decisions and writes `second-pass-decisions.jsonl` plus
`audit-seal.json`. Existing decisions/seal/score are refused unless the crash-reconstruction contract proves byte
identity. Provider raw responses/errors are never written.

## Score

```text
locomo-bench --adjudication-audit-score DIR \
  --adjudication-source PARENT_034_DIR \
  --adjudication-candidate R1 --adjudication-candidate R2 --adjudication-candidate R3
```

Validation order is mandatory:

1. Revalidate the parent label-free protocol/journal/seal.
2. Validate 035 build artifacts.
3. Validate all 954 terminal call states.
4. Validate all 1540 final decisions and the audit seal.
5. Only then open parent slot map/custody and the three label-bearing candidate journals.

The old `stage0-score.json` is never read. Score recomputes 034 baseline and 035 mapping from custody-matching hidden
inputs, then writes `audit-stage0-score.json`. Invalid pre-seal state invokes the hidden loader zero times and writes no
valid score.

## Error and secret semantics

- Structural/schema/digest/count/identity errors return non-zero and fail closed.
- Terminal provider or strict-response failure is not a structural run failure; it is audited and retains parent answer.
- Raw provider output/error, API key, raw endpoint, source path, gold, correct, and verdict never enter 035 artifacts.
- The result kind is always `historical_verdict_mapping`; GO never prints a formal LoCoMo claim.
