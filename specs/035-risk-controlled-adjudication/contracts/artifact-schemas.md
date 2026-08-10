# Artifact Contracts: Risk-Controlled Adjudication Audit v1

Canonical JSON/JSONL and SHA-256 rules inherit the frozen 034 encoding. JSONL sets sort by numeric `(conv,q)`; view
terminal sets additionally sort by fixed view order. Unknown fields fail strict decoding.

## `audit-manifest.json`

Schema `035.adjudication.audit.manifest.v1`.

Required fields bind:

```text
protocol_hash
parent protocol/packet-set/decision-set/prompt digests
parent manifest/packets/calls/decisions/seal raw digests
normalizer, queue_rule, view_seed_digest
entailment_prompt_digest, falsification_prompt_digest, resolver_digest
question_count=1540, risk_count=477
override_count=424, fallback_count=53, retain_count=1063
view_count=954, planned_calls=954
audit_packet_set_digest, resolver_map_set_digest
```

The protocol hash excludes itself and output set digests. No path, timestamp, provider config, hidden field, parent
selected slot, or control marker is allowed.

## `audit-packets.jsonl`

477 numeric-order records, schema `035.adjudication.audit.packet.v1`:

```text
protocol_hash, packet_id, packet_digest
conv, q, question_id, category, question, context_parity
evidence[E01..E30]
views[entailment,falsification]
  view_id, view_digest
  candidates[A1..An]{slot, representative_answer}
```

`n` is two or three unique normalized-answer groups covering all three parent candidates. Provider-visible data hides
group/member digests and multiplicity. The falsification order is a one-position rotation of the entailment order.
Forbidden recursively: `gold`, `correct`, `verdict`, `selected`, `current`, `control`, `fallback_reason`, parent C-slot,
source/run ID, group multiplicity, and provider identity.

The implementation may keep group/view mapping in non-serialized in-memory validation state; the persisted
`resolver-map.jsonl` is the canonical resolver mapping.

## `resolver-map.jsonl`

1540 label-free records, schema `035.adjudication.audit.resolver.v1`:

```text
parent packet/decision digests
parent selected slot/answer/group digests
text-control group digest
groups[group_digest, normalized_digest, member_answer_digests,
       representative_parent_slot, representative_answer_digest]
risk, risk_packet_id?, view slot-to-group maps?
```

Risk rows bind exactly two view maps. Non-risk rows have none and retain the parent answer. The file is opened by local
validation/resolution but never rendered to the provider. It contains no hidden verdict/source identity.

## Provider response

Exact JSON:

```json
{"assessments":[
  {
    "slot":"A1",
    "support":{"value":"yes","evidence_ids":["E03"]},
    "contradiction":{"value":"no","evidence_ids":[]}
  }
]}
```

There must be exactly one assessment for every view slot. `value` is `yes|no|unclear`. `yes` requires non-empty unique
present evidence IDs; `no|unclear` require `[]`. Direct recommendations, extra keys, code fences, trailing tokens, free
answers, duplicate slots/citations, and missing assessments are invalid.

## `audit-calls.jsonl`

Append-only schema `035.adjudication.audit.call.v1`, keyed by `(packet_id,view_id)`:

```text
STARTED: protocol/parent/packet/view identities, attempt=1, input_digest
COMPLETED: matching identities, structured assessments, usage, terminal_digest
FAILED: matching identities, closed reason, usage, terminal_digest
```

No raw provider output/error is stored. A terminal must follow exactly one matching STARTED. Orphan STARTED, duplicate
state, changed input identity, unknown view, or malformed assessment invalidates the run directory. FAILED is terminal
and valid for seal completeness.

## `second-pass-decisions.jsonl`

1540 numeric-order records, schema `035.adjudication.audit.decision.v1`:

```text
parent packet/decision digests
final parent slot/answer/group digests
audit_terminal_digests[0|2]
resolution, resolution_reason
provider_attempts[0|2], input_tokens, output_tokens
decision_digest
```

Non-risk rows have zero attempts and retain exactly. Risk rows have two terminal digests. Validator re-runs the resolver;
`switched_dual_convergence` is valid only for the exact dual-support/contradiction condition. Same-group exact-slot
changes are forbidden.

## `audit-seal.json`

Schema `035.adjudication.audit.seal.v1` includes:

```text
seal_digest
protocol and complete parent identity
audit packet/resolver/canonical-call-state/final-decision set digests
two prompt digests, resolver digest
provider, endpoint digest, model, revision, max tokens, binary digest
questions=1540, risk=477, views/planned/started/terminal/attempts=954
completed/failed, retries=0, retained/switched/reason counts
usage, pricing status/unit prices/estimated CNY
valid=true
```

The canonical call-state set hashes sorted terminal records, not raw concurrent journal order. All digests/counts and no
orphan/duplicate must validate before hidden input access.

## `audit-stage0-score.json`

Schema `035.adjudication.audit.stage0-score.v1`, result kind fixed to `historical_verdict_mapping`. Required results:

- frozen 034 baseline 1378/1540 and triggered mixed 61/88;
- new point mapping and judge-instability total/mixed best/worst bounds;
- 1540/477/954, 13/5, context-parity, audit completion/failure/change counts;
- new-only/old-only, overall exact two-sided McNemar;
- per-category deltas/exact p/Holm and temporal net;
- integrity/frozen diagnostics, usage/pricing, gate reasons, `GO|NO_GO`.

GO requires every FR-012 gate. A different parent digest requires a new protocol and thresholds.
