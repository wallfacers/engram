# Contract: `pcic_meta` Sidecar + Annotation Subcommand

**Surface**: `cmd/locomo-bench` new `--pcic-annotate` subcommand and the sidecar file it
produces/consumes.

## Frozen contract

- **Subcommand**: `locomo-bench --pcic-annotate --data <locomo.json> --store-dir <dir>
  [--pcic-meta <path>]` runs the one-time offline annotation and writes the sidecar. It is
  idempotent: re-running with a matching header (annotate model + dataset fingerprint) is a
  no-op / cache hit.
- **File**: JSON object
  ```json
  {
    "header": { "annotate_model": "<model>", "dataset_fingerprint": "<hash>", "count": <n> },
    "spans": { "<span_id>": { "entity","slot","value","polarity","time_state","source_turn_ids" } }
  }
  ```
  Location defaults under the store/run dir; gitignored. Never written to the engine store.
- **Consumption**: coverage and answer runs load the sidecar read-only via `--pcic-meta`
  (defaulting to the conventional path). No LLM call happens at query time.
- **Secrets**: the annotate model API key flows env → provider only; never logged or written
  into the sidecar.

## Degradation / validation

- Missing file → selector runs in "unknown role" mode (see arm-mechanism contract).
- Header mismatch (different model or dataset) → the run warns and treats the sidecar as
  absent rather than silently mixing regimes; re-annotation is explicit.
- A span present in dialogue but absent from `spans` → that turn contributes no typed claim
  (role unknown), never an error.

## Tests (contract)

- `TestPCICMetaRoundTrip`: save then load reproduces the same span map + header.
- `TestPCICMetaHeaderMismatchDegrades`: a mismatched header is treated as absent (warn), not
  fatal, and selection degrades to rerank order.
- `TestPCICAnnotateWritesNoEngineState`: after annotation, `git diff` over engine dirs is
  empty and no store row is added.
