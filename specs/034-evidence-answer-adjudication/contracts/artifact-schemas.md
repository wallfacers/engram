# Artifact Contracts: Answer Adjudication v1

Canonical JSON uses Go `encoding/json` field order from the frozen structs and a trailing newline for JSON documents.
JSONL digests are computed over numeric `(conv,q)` sorted canonical records, each followed by `\n`. Every digest string
is `sha256:<lowercase-hex>`.

## `manifest.json`

Schema: `034.adjudication.manifest.v1`.

Required fields:

```text
schema, protocol_id, protocol_hash, normalizer, permutation_seed_digest,
sanitized_candidate_source_digests[3], sanitized_trace_digest,
store_semantic_digest, question_ids_digest, prompt_digest,
question_count, triggered_count, context_parity_count,
triggered_context_parity_count, packet_set_digest
```

Frozen Stage-0 counts are 1540 questions, 771 triggered, 1532 context-parity, and 766 triggered context-parity. The
manifest contains no timestamp, path, raw source hash, run ID, gold/verdict field, model configuration, or secret, so
rebuilding from hidden-label-mutated sources produces identical execution bytes.

`protocol_hash` is the digest of the manifest with `protocol_hash` and `packet_set_digest` empty. `packet_set_digest` is
the digest of sorted packet records.

## `custody.json`

Schema: `034.adjudication.custody.v1`. Score-only receipt containing raw candidate/trace/store size + SHA-256, DB
inventory/semantic snapshot digests, record counts, git commit/dirty state, and build binary digest. It may change when a
hidden label is mutated and is deliberately excluded from execution protocol/permutation/packet digests. It contains no
raw path or credential. Historical answer model/revision fields, when present, are explicitly tagged
`provenance_status:"legacy_operator_claim"`; this schema cannot mark them verified.

## `packets.jsonl`

Each `034.adjudication.packet.v1` record contains:

```text
schema, protocol_hash, packet_id, packet_digest,
conv, q, question_id, category, question,
triggered, context_parity,
evidence[{evidence_id, rank, content, content_digest}],
candidates[{slot, answer, answer_digest}]
```

Rules:

- exactly 1540 unique packets, numeric `(conv,q)` sorted;
- exactly 30 dense `E01..E30` evidence items and exactly three `C1..C3` candidates;
- `triggered` is recomputed from candidate text; `context_parity` is informational and never changes the trigger;
- duplicate answer text is allowed; source identity and memory entry name are absent;
- `packet_digest` hashes the record with `packet_digest` empty;
- only the listed keys are allowed recursively. Unknown keys, including `gold`, `correct`, `verdict`, `covers_gold`,
  `mapped_gold_turns`, `run_id`, and `source_id`, invalidate the set before calls.

## `slot-map.jsonl`

Score-only label-free records keyed by packet ID. Each `C1..C3` maps to a canonical source digest, source-local candidate
identity, exact answer digest, and normalized answer digest. The file contains no verdict/gold field and is not opened by
validate/run. Its set digest is recorded only in custody and seal-private linkage, not in public packets.

## `calls.jsonl`

Append-only records, schema `034.adjudication.call.v1`:

```text
protocol_hash, packet_id, packet_digest, state,
attempt, input_digest,
terminal_decision?, terminal_decision_digest?
```

`state` is `started`, `completed`, or `failed`. `started` has no decision; terminal records must follow exactly one
matching `started`, use `attempt:1`, and carry a valid decision. `failed` still carries the deterministic fallback
decision and a closed fallback/error code. An incomplete started record permanently invalidates that directory.

## `sealed-decisions.jsonl`

Each `034.adjudication.decision.v1` record contains:

```text
protocol_hash, packet_id, packet_digest, decision_digest,
conv, q, question_id, triggered,
state, selected_slot, selected_answer_digest,
evidence_ids, confidence, fallback_reason,
provider_attempts, input_tokens, output_tokens
```

Allowed `state`: `selected`, `fallback`. A selected decision requires one of `C1..C3`, non-empty valid evidence IDs,
`confidence:"high"`, no fallback reason, and one attempt. A fallback requires a deterministic control slot,
`confidence:"fallback"`, a closed fallback reason, and zero attempts only for non-triggered packets; triggered failures
have one attempt. Every decision digest excludes only itself.

When two slots have byte-identical answer text, the runtime answer is identical. The post-seal historical join resolves
their potentially conflicting legacy verdicts by canonical sanitized source digest, never by favorable verdict or
blinded slot position, and separately reports the full judge-instability sensitivity bounds.

## `seal.json`

Schema: `034.adjudication.seal.v1`. Required receipts:

```text
protocol_hash, packet_set_digest, decision_set_digest,
prompt_digest, provider, base_url_digest, model, model_revision,
max_tokens, binary_digest, planned_calls, started_calls, completed_calls, failed_calls,
provider_attempts, retries, input_tokens, output_tokens,
fallback_counts, pricing_status, input/output unit prices?, estimated_cny?,
question_count, decision_count, valid
```

For the frozen set, `planned_calls=771`, `retries=0`, `question_count=decision_count=1540`, and
`started_calls=provider_attempts=completed_calls+failed_calls`. `valid` also requires no orphan/duplicate journal state.
The journal `input_digest` binds provider, endpoint digest, model, revision, output cap, binary, prompt, and packet input;
resume refuses any identity drift before another call.

## Verifier response

The provider response is strict JSON with no surrounding text and no unknown keys:

```json
{"selected_slot":"C2","evidence_ids":["E03"],"confidence":"high"}
```

Only `C1..C3`, present evidence IDs, at least one citation, and literal `high` are accepted. A different confidence,
missing/extra field, duplicate/out-of-range citation, code fence, trailing token, or free-form answer triggers fallback.

## `stage0-score.json`

Schema: `034.adjudication.stage0-score.v1`; `result_kind` is fixed to `historical_verdict_mapping`. It contains custody
validation, integrity gates, 1540/771/766/5 cohort results, 1371 historical verdict-majority, 1368 executable text
control, 1411 candidate oracle, 96/88 mixed-verdict strata, 13/5 judge-instability strata, selected score and sensitivity
bounds, paired flips, category comparisons with exact McNemar + Holm, fallbacks/usage/cost receipts, and `GO|NO_GO`.

The scorer must not emit `GO` if any denominator or frozen diagnostic count differs from the manifest-specific expected
values. A new input digest requires a new protocol and thresholds rather than silently reusing these constants.
