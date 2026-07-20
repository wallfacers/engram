# Contract: `+pcic` Arm Mechanism

**Surface**: `cmd/locomo-bench` `--retrieval` arm parsing (`supportedArmMechanisms`,
`parseArm`, `optionsForArm`, retriever construction).

## Frozen contract

- A new mechanism suffix `pcic` is accepted, composable with `rerank`:
  - `hybrid+rerank+pcic` — reranked candidates, then the PCIC-lite selector picks 12 chunks.
  - Any arm **without** the `pcic` suffix is byte-for-byte unchanged (parity invariant).
- `pcic` requires a hybrid backend and is only meaningful with `rerank` present; if used on
  a non-rerank hybrid arm it still runs (over fused candidates) but the intended contrast is
  `hybrid+rerank` vs `hybrid+rerank+pcic`.
- A global `--pcic` flag mirrors `--rerank` for single-arm runs (backward-compat with the
  bake-off recipe). `optionsForArm` sets `options.pcic` only under the `+pcic` suffix; the
  paired baseline never inherits it.
- An `oracle` selection mode is exposed for the ceiling arm via a distinct arm token
  (e.g. `hybrid+rerank+oracle`), gated to coverage-only runs (it consumes gold labels and
  MUST NOT be used in a scored answer arm).

## Degradation

- `+pcic` with no `pcic_meta` sidecar, no reranker, or unannotated spans → the selector
  returns rerank-order top-12 (identical to `hybrid+rerank`). Never errors the run.

## Tests (contract)

- `TestPCICArmMechanismGatesSelector`: `optionsForArm` sets `pcic` only for `+pcic`; paired
  baseline `hybrid+rerank` has `pcic=false`.
- Parity: `hybrid` and `hybrid+rerank` results are identical whether or not a `+pcic` sibling
  arm is present in the same run.
