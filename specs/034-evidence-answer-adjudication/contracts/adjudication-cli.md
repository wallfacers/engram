# CLI Contract: Answer Adjudication

All adjudication modes are additive, mutually exclusive, and dispatched before the ordinary `--data` requirement. With
none selected, `locomo-bench` behavior is unchanged.

## Shared flags

| Flag | Contract |
|---|---|
| `--adjudication-build DIR` | Offline materialization mode; DIR must not contain a prior seal/journal |
| `--adjudication-validate DIR` | Offline public packet/manifest validation; never opens hidden inputs or a provider |
| `--adjudication-run DIR` | Label-free verifier execution + seal mode |
| `--adjudication-score DIR` | Post-seal historical verdict join mode; never calls a provider |
| `--adjudication-candidate PATH` | Repeat exactly three times for build and score; forbidden for run/validate |
| `--adjudication-trace PATH` | Required only for build |
| `--store-dir DIR` | Required only for build; must contain `conv0.db` … `conv9.db` and no WAL/SHM sidecars |
| `--adjudication-seed STRING` | Required only for build; non-empty frozen permutation seed |
| `--concurrency N` | Run-only in this workflow; minimum 1, expected Stage-0 value 32 |
| `--adjudication-max-tokens N` | Run output cap; default 512, minimum 1 |
| `--adjudication-allow-paid` | Required for run; explicit acknowledgement that the verifier can incur cost |

Supplying arguments from another mode is an error. Build and score sort the three candidate sources by their sanitized
digest, so CLI order and path names do not affect packet or selection bytes.

## Build

```text
locomo-bench --adjudication-build DIR \
  --adjudication-candidate R1 --adjudication-candidate R2 --adjudication-candidate R3 \
  --adjudication-trace TRACE --store-dir STORE --adjudication-seed SEED
```

### Preconditions

- Exactly 1540 unique, matching questions in each strict candidate journal.
- Non-empty answer, question, regime, and retrieval receipts; identical question/category/regime semantics across runs.
- Exactly one trace per question, exactly 30 dense ranked unique hit names, every name resolvable in the matching store.
- Ten immutable source DBs with no WAL/SHM sidecars; source digest/sidecar state unchanged after reading.
- No existing execution journal or seal in DIR.

### Effects

Writes `manifest.json`, `packets.jsonl`, `slot-map.jsonl`, and `custody.json` atomically. Makes zero provider calls. A
failure leaves no valid manifest and returns non-zero.

## Validate

```text
locomo-bench --adjudication-validate DIR
```

Reads only `manifest.json` and `packets.jsonl`. It verifies schema, protocol/packet/set digests, uniqueness, exact counts,
trigger recomputation, candidate/evidence bounds, and a structural forbidden-field scan. It does not open `slot-map`,
`custody`, candidate journals, trace, store, environment API key, or network.

## Run and seal

```text
ADJUDICATOR_PROVIDER=... ADJUDICATOR_BASE_URL=... ADJUDICATOR_MODEL=... \
ADJUDICATOR_MODEL_REVISION=... ADJUDICATOR_API_KEY=... \
locomo-bench --adjudication-run DIR --adjudication-allow-paid --concurrency 32
```

Required environment:

| Variable | Persistence rule |
|---|---|
| `ADJUDICATOR_PROVIDER` | Persist provider name |
| `ADJUDICATOR_BASE_URL` | Persist only a SHA-256 fingerprint; reject userinfo/query/fragment |
| `ADJUDICATOR_MODEL` | Persist model ID |
| `ADJUDICATOR_MODEL_REVISION` | Persist explicit revision |
| `ADJUDICATOR_API_KEY` | Never persist, print, log, or return |

Optional pricing variables `ADJUDICATOR_INPUT_CNY_PER_MILLION` and
`ADJUDICATOR_OUTPUT_CNY_PER_MILLION` must both be valid non-negative decimals to produce a priced estimate. If either is
absent, the seal says `pricing_status: "unpriced"` and has no numeric cost claim.

The runner reads only public manifest/packets. It writes `calls.jsonl` as fsynced STARTED→terminal records. One triggered
packet permits one provider attempt; non-triggered packets permit none. A terminal provider/parse/confidence failure is a
valid deterministic fallback. An orphan STARTED, malformed prior line, identity drift, duplicate terminal, or changed
packet makes resume fail closed and prevents sealing.

When all 1540 decisions are terminal, the runner writes numeric `(conv,q)` sorted `sealed-decisions.jsonl` and
`seal.json`. Provider errors are stored only as a closed error code, never raw upstream text.

## Score

```text
locomo-bench --adjudication-score DIR \
  --adjudication-candidate R1 --adjudication-candidate R2 --adjudication-candidate R3
```

The scorer validates the public artifacts and seal first. Only after that succeeds may it open `slot-map.jsonl`,
`custody.json`, and the three label-bearing candidate journals. Raw custody digest mismatch, incomplete/extra labels,
slot-map mismatch, or any seal defect returns non-zero and writes no valid score.

On success it writes `stage0-score.json`, labelled `historical_verdict_mapping`. `GO` requires all of:

- selected historical mapping ≥1387/1540;
- selected-correct ≥69/88 on the post-seal triggered mixed-verdict stratum;
- no Holm-corrected significant negative category;
- integrity, contamination, coverage, and receipt gates all true.

The output is never a formal LoCoMo result.
